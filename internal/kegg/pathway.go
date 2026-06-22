package kegg

import (
	"biofetch/internal/shared/httpx"
	"biofetch/internal/shared/logx"
	"biofetch/internal/shared/parallel"
	"biofetch/internal/shared/sets"
	"biofetch/internal/shared/tomlx"
	"bufio"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"
)

const (
	defaultKEGGRetryMax       = 5
	defaultKEGGRetryWait      = 3 * time.Second
	pathwayInspectWorkersMax  = 8
	keggPathwayScopeBatchSize = 32
)

var baseURL = "https://rest.kegg.jp"

type keggInfoMetadata struct {
	sourceRelease    string
	sourceLastUpdate string
}

type pathwayRecord struct {
	PathwayID string
	Asset     string
	PathRel   string
	SHA256    string
	Bytes     int64
	URL       string
}

type pathwayFileInfo struct {
	size  int64
	isDir bool
}

type pathwayLocalPlanningStats struct {
	expectedAssets       int
	reusedManifestAssets int
	rebuiltHashAssets    int
	downloadAssets       int
	skippedEntryAssets   int
	elapsed              time.Duration
}

type pathwayAssetInspectTask struct {
	pathwayID string
	spec      pathwayAssetSpec
}

type pathwayAssetInspectResult struct {
	record         pathwayRecord
	task           pathwayAssetInspectTask
	shouldDownload bool
	wasManifest    bool
	wasHash        bool
}

func (stats *pathwayLocalPlanningStats) add(other pathwayLocalPlanningStats) {
	stats.expectedAssets += other.expectedAssets
	stats.reusedManifestAssets += other.reusedManifestAssets
	stats.rebuiltHashAssets += other.rebuiltHashAssets
	stats.downloadAssets += other.downloadAssets
	stats.skippedEntryAssets += other.skippedEntryAssets
	stats.elapsed += other.elapsed
}

type manifestFile struct {
	Database              string            `toml:"database"`
	Asset                 string            `toml:"asset"`
	Version               string            `toml:"version"`
	VersionToken          string            `toml:"version_token"`
	SourceRelease         string            `toml:"source_release"`
	SourceReleaseStart    string            `toml:"source_release_start,omitempty"`
	SourceReleaseEnd      string            `toml:"source_release_end,omitempty"`
	SourceLastUpdate      string            `toml:"source_last_update,omitempty"`
	SourceLastUpdateStart string            `toml:"source_last_update_start,omitempty"`
	SourceLastUpdateEnd   string            `toml:"source_last_update_end,omitempty"`
	DownloadedAt          string            `toml:"downloaded_at"`
	Scope                 manifestScope     `toml:"scope"`
	Pathways              []manifestPathway `toml:"pathways"`
	Files                 []manifestAsset   `toml:"files"`
}

type manifestScope struct {
	Type  string `toml:"type"`
	Value string `toml:"value"`
}

type manifestPathway struct {
	ID    string   `toml:"id"`
	Files []string `toml:"files"`
}

type manifestAsset struct {
	PathwayID string `toml:"pathway_id"`
	Asset     string `toml:"asset"`
	Path      string `toml:"path"`
	SHA256    string `toml:"sha256"`
	Bytes     int64  `toml:"bytes"`
	URL       string `toml:"url"`
}

func runFetchPathway(cfg *pathwayConfig) error {
	timeStarted := time.Now()
	clientHTTP := createHTTPClient(cfg.shouldAllowInsecureTLS)
	clientKegg := createKEGGClient(clientHTTP, cfg.requestInterval, cfg.retryMax, cfg.retryWait)

	if strings.TrimSpace(cfg.versionToken) == "" {
		cfg.versionToken = deriveKEGGSnapshotVersionToken(timeStarted)
	}
	cfg.version = cfg.versionToken
	metadataStart, err := resolveKEGGInfoMetadata(clientKegg, "pathway")
	if err != nil {
		logf("warning: KEGG pathway info metadata unavailable: %v", err)
	} else {
		cfg.applyKEGGInfoMetadataStart(metadataStart)
	}
	_, closeRun, err := logx.StartVersionedRun("biofetch kegg", "fetch", cfg.dirLogs, filepath.Join(cfg.dirOut, "pathway", cfg.versionToken))
	if err != nil {
		return err
	}
	defer closeRun()

	scopeKeys, err := resolvePathwayScopeKeys(clientKegg, cfg)
	if err != nil {
		return err
	}
	dirVersion := filepath.Join(cfg.dirOut, "pathway", cfg.versionToken)
	fileManifest := filepath.Join(dirVersion, "manifest.lock")

	if cfg.shouldDryRun {
		logf("[dry-run] version dir: %s", dirVersion)
		logf("[dry-run] manifest: %s", fileManifest)
	} else {
		if err := os.MkdirAll(dirVersion, 0o755); err != nil {
			return fmt.Errorf("create version dir: %w", err)
		}
	}

	reuseIndex := map[string]pathwayRecord{}
	if !cfg.shouldDryRun {
		var err error
		reuseIndex, err = buildPathwayManifestReuseIndex(fileManifest)
		if err != nil {
			return err
		}
	}

	records := make([]pathwayRecord, 0)
	statsPlanning := pathwayLocalPlanningStats{}
	countPathways := 0
	batchesScope := chunkStrings(scopeKeys, keggPathwayScopeBatchSize)
	for indexBatch, batchScopeKeys := range batchesScope {
		logf("batch %d/%d: scopes=%d", indexBatch+1, len(batchesScope), len(batchScopeKeys))
		for _, scopeKey := range batchScopeKeys {
			recordsScope, countScopePathways, statsScope, err := fetchPathwayScope(clientKegg, cfg, dirVersion, scopeKey, reuseIndex)
			if err != nil {
				return err
			}
			records = append(records, recordsScope...)
			statsPlanning.add(statsScope)
			countPathways += countScopePathways
		}
	}

	if cfg.shouldDryRun {
		logf("[dry-run] done (scopes=%d)", len(scopeKeys))
		return nil
	}

	sort.Slice(records, func(i, j int) bool {
		if records[i].PathwayID != records[j].PathwayID {
			return records[i].PathwayID < records[j].PathwayID
		}
		return records[i].Asset < records[j].Asset
	})

	recordsComplete, err := buildCompletePathwayRecords(fileManifest, dirVersion, records)
	if err != nil {
		return err
	}
	metadataEnd, err := resolveKEGGInfoMetadata(clientKegg, "pathway")
	if err != nil {
		logf("warning: KEGG pathway info metadata unavailable after download: %v", err)
	} else {
		cfg.applyKEGGInfoMetadataEnd(metadataEnd)
	}

	if err := writeManifest(fileManifest, cfg, recordsComplete, time.Now()); err != nil {
		return err
	}

	logf("done (files=%d, pathways=%d, scopes=%d)", len(recordsComplete), countPathways, len(scopeKeys))
	if statsPlanning.expectedAssets > 0 {
		logf(
			"local planning: expected=%d reused_manifest=%d rebuilt_hash=%d scheduled_download=%d skipped_entry=%d elapsed=%s",
			statsPlanning.expectedAssets,
			statsPlanning.reusedManifestAssets,
			statsPlanning.rebuiltHashAssets,
			statsPlanning.downloadAssets,
			statsPlanning.skippedEntryAssets,
			statsPlanning.elapsed.Round(time.Millisecond),
		)
	}
	logf("manifest written: %s", fileManifest)
	return nil
}

func resolvePathwayIDs(
	clientKegg *keggClient,
	cfg *pathwayConfig,
) ([]string, []byte, string, error) {
	listURL := derivePathwayListURL(cfg)
	listContent, err := clientKegg.download(listURL)
	if err != nil {
		return nil, nil, "", err
	}

	if len(cfg.pathwayIDs) > 0 {
		return append([]string(nil), cfg.pathwayIDs...), listContent, listURL, nil
	}

	pathwayIDs, err := parsePathwayIDsFromList(listContent)
	if err != nil {
		return nil, nil, "", err
	}
	pathwayIDs = applyTraversalOrder(pathwayIDs, cfg.ruleOrder)
	return pathwayIDs, listContent, listURL, nil
}

func derivePathwayListURL(cfg *pathwayConfig) string {
	if cfg.shouldFetchReference {
		return baseURL + "/list/pathway"
	}
	return baseURL + "/list/pathway/" + cfg.organismCode
}

func resolvePathwayScopeKeys(clientKegg *keggClient, cfg *pathwayConfig) ([]string, error) {
	switch {
	case cfg.shouldFetchReference:
		cfg.scopeType = "reference"
		cfg.scopeValue = "pathway"
		return []string{"reference"}, nil
	case cfg.shouldDownloadAll:
		organismCodes, err := resolveKEGGOrganismCodes(clientKegg, cfg.ruleOrder)
		if err != nil {
			return nil, err
		}
		cfg.scopeType = "organisms"
		cfg.scopeValue = "all"
		return organismCodes, nil
	case len(cfg.organismCodes) > 0:
		organismCodes := append([]string(nil), cfg.organismCodes...)
		if len(organismCodes) == 1 {
			cfg.scopeType = "organism"
			cfg.scopeValue = organismCodes[0]
		} else {
			cfg.scopeType = "organisms"
			cfg.scopeValue = strings.Join(organismCodes, ",")
		}
		return organismCodes, nil
	default:
		return nil, fmt.Errorf("no pathway scope configured")
	}
}

func fetchPathwayScope(
	clientKegg *keggClient,
	cfg *pathwayConfig,
	dirVersion string,
	scopeKey string,
	reuseIndex map[string]pathwayRecord,
) ([]pathwayRecord, int, pathwayLocalPlanningStats, error) {
	cfgScope := *cfg
	cfgScope.organismCode = scopeKey
	cfgScope.shouldFetchReference = scopeKey == "reference"

	dirRawScope := filepath.Join(dirVersion, "raw", scopeKey)
	dirTidyScope := filepath.Join(dirVersion, "tidy", scopeKey)
	if !cfg.shouldDryRun {
		if err := os.MkdirAll(dirRawScope, 0o755); err != nil {
			return nil, 0, pathwayLocalPlanningStats{}, fmt.Errorf("create raw dir: %w", err)
		}
		if err := os.MkdirAll(dirTidyScope, 0o755); err != nil {
			return nil, 0, pathwayLocalPlanningStats{}, fmt.Errorf("create tidy dir: %w", err)
		}
	}

	pathwayIDs, listContent, listURL, err := resolvePathwayIDs(clientKegg, &cfgScope)
	if err != nil {
		return nil, 0, pathwayLocalPlanningStats{}, err
	}

	records := make([]pathwayRecord, 0, estimatePathwayRecordCapacity(cfg.assetNames, len(pathwayIDs)))
	fileList := filepath.Join(dirRawScope, "pathway.list.tsv")
	pathRelList := filepath.ToSlash(filepath.Join("raw", scopeKey, "pathway.list.tsv"))

	if shouldFetchPathwayAsset(cfg.assetNames, "list") {
		if cfg.shouldDryRun {
			logf("[dry-run] %s -> %s", listURL, fileList)
		} else {
			recordList, err := writeDownloadedFile(fileList, pathRelList, "", "pathway.list", listURL, listContent)
			if err != nil {
				return nil, 0, pathwayLocalPlanningStats{}, err
			}
			records = append(records, recordList)
		}
	}

	tasksInspect := make([]pathwayAssetInspectTask, 0, estimatePathwayRecordCapacity(cfg.assetNames, len(pathwayIDs)))
	for _, pathwayID := range pathwayIDs {
		for _, assetSpec := range derivePathwayAssetSpecs(scopeKey, pathwayID, cfg.assetNames, dirRawScope) {
			if cfg.shouldDryRun {
				logf("[dry-run] %s -> %s", assetSpec.url, assetSpec.fileOut)
				continue
			}
			tasksInspect = append(tasksInspect, pathwayAssetInspectTask{
				pathwayID: pathwayID,
				spec:      assetSpec,
			})
		}
	}

	statsPlanning := pathwayLocalPlanningStats{expectedAssets: len(tasksInspect)}
	if len(tasksInspect) == 0 {
		return records, len(pathwayIDs), statsPlanning, nil
	}

	timePlanningStart := time.Now()
	fileIndex, err := scanPathwayScopeFileIndex(dirRawScope)
	if err != nil {
		return nil, 0, pathwayLocalPlanningStats{}, err
	}
	resultsInspect, err := inspectPathwayAssetTasks(
		tasksInspect,
		cfg.shouldOverwriteExisting,
		reuseIndex,
		fileIndex,
	)
	if err != nil {
		return nil, 0, pathwayLocalPlanningStats{}, err
	}
	statsPlanning.elapsed = time.Since(timePlanningStart)

	for _, result := range resultsInspect {
		switch {
		case result.shouldDownload:
			statsPlanning.downloadAssets++
		case result.wasManifest:
			statsPlanning.reusedManifestAssets++
			records = append(records, result.record)
		case result.wasHash:
			statsPlanning.rebuiltHashAssets++
			records = append(records, result.record)
		}
	}

	for _, result := range resultsInspect {
		if !result.shouldDownload {
			continue
		}
		recordAsset, ok, err := downloadPathwayAsset(
			clientKegg,
			result.task.spec.fileOut,
			result.task.spec.pathRel,
			result.task.pathwayID,
			result.task.spec.assetName,
			result.task.spec.url,
		)
		if err != nil {
			return nil, 0, pathwayLocalPlanningStats{}, err
		}
		if !ok && result.task.spec.assetName == "pathway.entry" {
			statsPlanning.skippedEntryAssets++
		}
		if ok {
			records = append(records, recordAsset)
		}
	}

	return records, len(pathwayIDs), statsPlanning, nil
}

type pathwayAssetSpec struct {
	assetName string
	fileOut   string
	pathRel   string
	url       string
}

func shouldFetchPathwayAsset(assetNames []string, assetName string) bool {
	for _, value := range assetNames {
		if value == assetName {
			return true
		}
	}
	return false
}

func estimatePathwayRecordCapacity(assetNames []string, numPathways int) int {
	countPerPathway := 0
	for _, assetName := range assetNames {
		if assetName == "list" {
			continue
		}
		countPerPathway++
	}
	capacity := countPerPathway * numPathways
	if shouldFetchPathwayAsset(assetNames, "list") {
		capacity++
	}
	return capacity
}

func derivePathwayAssetSpecs(
	scopeKey string,
	pathwayID string,
	assetNames []string,
	dirRawScope string,
) []pathwayAssetSpec {
	pathRoot := filepath.ToSlash(filepath.Join("raw", scopeKey))
	specs := make([]pathwayAssetSpec, 0, len(assetNames))

	for _, assetName := range assetNames {
		switch assetName {
		case "entry":
			specs = append(specs, pathwayAssetSpec{
				assetName: "pathway.entry",
				fileOut:   filepath.Join(dirRawScope, pathwayID+".txt"),
				pathRel:   filepath.ToSlash(filepath.Join(pathRoot, pathwayID+".txt")),
				url:       baseURL + "/get/" + pathwayID,
			})
		case "kgml":
			specs = append(specs, pathwayAssetSpec{
				assetName: "pathway.kgml",
				fileOut:   filepath.Join(dirRawScope, pathwayID+".kgml"),
				pathRel:   filepath.ToSlash(filepath.Join(pathRoot, pathwayID+".kgml")),
				url:       baseURL + "/get/" + pathwayID + "/kgml",
			})
		case "conf":
			specs = append(specs, pathwayAssetSpec{
				assetName: "pathway.conf",
				fileOut:   filepath.Join(dirRawScope, pathwayID+".conf"),
				pathRel:   filepath.ToSlash(filepath.Join(pathRoot, pathwayID+".conf")),
				url:       baseURL + "/get/" + pathwayID + "/conf",
			})
		case "image":
			specs = append(specs, pathwayAssetSpec{
				assetName: "pathway.image",
				fileOut:   filepath.Join(dirRawScope, pathwayID+".png"),
				pathRel:   filepath.ToSlash(filepath.Join(pathRoot, pathwayID+".png")),
				url:       baseURL + "/get/" + pathwayID + "/image",
			})
		}
	}

	return specs
}

func parsePathwayIDsCSV(textCSV string) ([]string, error) {
	return resolvePathwayIDInputs([]string{textCSV}, ruleOrderAsc)
}

func readPathwayIDsFromFile(filePathwayIDs string) ([]string, error) {
	return resolvePathwayIDInputs([]string{"@" + filePathwayIDs}, ruleOrderAsc)
}

func parsePathwayIDsFromList(data []byte) ([]string, error) {
	setPathwayIDs := make(map[string]struct{})
	pathwayIDs := make([]string, 0)
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		fields := strings.Split(line, "\t")
		if len(fields) == 0 {
			continue
		}
		pathwayID := normalizePathwayID(fields[0])
		if !isValidPathwayID(pathwayID) {
			return nil, fmt.Errorf("invalid pathway id in KEGG list output: %s", pathwayID)
		}
		if _, ok := setPathwayIDs[pathwayID]; ok {
			continue
		}
		setPathwayIDs[pathwayID] = struct{}{}
		pathwayIDs = append(pathwayIDs, pathwayID)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan pathway list: %w", err)
	}
	return pathwayIDs, nil
}

func fetchPathwayAsset(
	clientKegg *keggClient,
	shouldOverwriteExisting bool,
	fileOut string,
	pathRel string,
	pathwayID string,
	assetName string,
	urlFile string,
) (pathwayRecord, bool, error) {
	if !shouldOverwriteExisting {
		result, err := inspectPathwayAssetTask(
			pathwayAssetInspectTask{
				pathwayID: pathwayID,
				spec: pathwayAssetSpec{
					assetName: assetName,
					fileOut:   fileOut,
					pathRel:   pathRel,
					url:       urlFile,
				},
			},
			false,
			nil,
			nil,
		)
		if err != nil {
			return pathwayRecord{}, false, err
		}
		if !result.shouldDownload {
			logf("using existing %s", filepath.Base(fileOut))
			return result.record, true, nil
		}
	}

	return downloadPathwayAsset(clientKegg, fileOut, pathRel, pathwayID, assetName, urlFile)
}

func downloadPathwayAsset(
	clientKegg *keggClient,
	fileOut string,
	pathRel string,
	pathwayID string,
	assetName string,
	urlFile string,
) (pathwayRecord, bool, error) {
	for attempt := 1; attempt <= clientKegg.retryMax; attempt++ {
		logf("downloading %s", filepath.Base(fileOut))
		shouldRetry, err := clientKegg.downloadFileOnce(urlFile, fileOut)
		if err != nil {
			if shouldSkipPathwayDownloadStatus(assetName, err) {
				logf("unavailable %s (%s), skipping", filepath.Base(fileOut), urlFile)
				return pathwayRecord{}, false, nil
			}
			if (shouldRetry || shouldRetryPathwayDownloadStatus(assetName, err)) && attempt < clientKegg.retryMax {
				if clientKegg.retryWait > 0 {
					logf(
						"request failed (%d/%d), retrying in %s: %v",
						attempt,
						clientKegg.retryMax,
						clientKegg.retryWait,
						err,
					)
					time.Sleep(clientKegg.retryWait)
				} else {
					logf("request failed (%d/%d), retrying: %v", attempt, clientKegg.retryMax, err)
				}
				continue
			}
			if shouldContinuePathwayDownloadStatus(assetName, err) {
				logx.Warnf(
					"biofetch kegg",
					"continuing after unavailable %s (%s): %v",
					filepath.Base(fileOut),
					urlFile,
					err,
				)
				return pathwayRecord{}, false, nil
			}
			return pathwayRecord{}, false, err
		}
		record, err := buildPathwayRecord(fileOut, pathRel, pathwayID, assetName, urlFile)
		return record, err == nil, err
	}
	return pathwayRecord{}, false, fmt.Errorf("request %s: exhausted retries", urlFile)
}

func shouldSkipPathwayDownloadStatus(assetName string, err error) bool {
	if assetName == "pathway.entry" {
		return false
	}
	if assetName != "pathway.kgml" && assetName != "pathway.conf" && assetName != "pathway.image" {
		return false
	}
	return httpx.IsUnexpectedStatus(err, http.StatusNotFound) ||
		httpx.IsUnexpectedStatus(err, http.StatusForbidden)
}

func shouldRetryPathwayDownloadStatus(assetName string, err error) bool {
	if assetName != "pathway.entry" {
		return false
	}
	return httpx.IsUnexpectedStatus(err, http.StatusForbidden)
}

func shouldContinuePathwayDownloadStatus(assetName string, err error) bool {
	if assetName != "pathway.entry" {
		return false
	}
	return httpx.IsUnexpectedStatus(err, http.StatusForbidden)
}

func inspectPathwayAssetTasks(
	tasks []pathwayAssetInspectTask,
	shouldOverwriteExisting bool,
	reuseIndex map[string]pathwayRecord,
	fileIndex map[string]pathwayFileInfo,
) ([]pathwayAssetInspectResult, error) {
	return parallel.MapOrderedWithWorkers(
		tasks,
		pathwayInspectWorkersMax,
		func(task pathwayAssetInspectTask) (pathwayAssetInspectResult, error) {
			return inspectPathwayAssetTask(task, shouldOverwriteExisting, reuseIndex, fileIndex)
		},
	)
}

func inspectPathwayAssetTask(
	task pathwayAssetInspectTask,
	shouldOverwriteExisting bool,
	reuseIndex map[string]pathwayRecord,
	fileIndex map[string]pathwayFileInfo,
) (pathwayAssetInspectResult, error) {
	result := pathwayAssetInspectResult{
		task:           task,
		shouldDownload: true,
	}
	if shouldOverwriteExisting {
		return result, nil
	}

	fileInfo, ok, err := lookupPathwayFileInfo(task.spec.fileOut, fileIndex)
	if err != nil {
		return pathwayAssetInspectResult{}, err
	}
	if !ok || fileInfo.isDir || fileInfo.size <= 0 {
		return result, nil
	}

	if record, ok := reusePathwayManifestRecord(task.spec.pathRel, fileInfo, reuseIndex); ok {
		result.record = record
		result.shouldDownload = false
		result.wasManifest = true
		return result, nil
	}

	record, ok, err := inspectExistingFile(
		task.spec.fileOut,
		task.spec.pathRel,
		task.pathwayID,
		task.spec.assetName,
		task.spec.url,
	)
	if err != nil {
		return pathwayAssetInspectResult{}, err
	}
	if ok {
		result.record = record
		result.shouldDownload = false
		result.wasHash = true
	}
	return result, nil
}

func reusePathwayManifestRecord(
	pathRel string,
	fileInfo pathwayFileInfo,
	reuseIndex map[string]pathwayRecord,
) (pathwayRecord, bool) {
	if len(reuseIndex) == 0 {
		return pathwayRecord{}, false
	}
	record, ok := reuseIndex[pathRel]
	if !ok {
		return pathwayRecord{}, false
	}
	if record.SHA256 == "" || record.Bytes <= 0 || record.Bytes != fileInfo.size {
		return pathwayRecord{}, false
	}
	return record, true
}

func lookupPathwayFileInfo(filePath string, fileIndex map[string]pathwayFileInfo) (pathwayFileInfo, bool, error) {
	if fileIndex != nil {
		info, ok := fileIndex[filepath.Base(filePath)]
		return info, ok, nil
	}
	infoFile, err := os.Stat(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return pathwayFileInfo{}, false, nil
		}
		return pathwayFileInfo{}, false, fmt.Errorf("stat existing file: %w", err)
	}
	return pathwayFileInfo{size: infoFile.Size(), isDir: infoFile.IsDir()}, true, nil
}

func scanPathwayScopeFileIndex(dirRawScope string) (map[string]pathwayFileInfo, error) {
	entries, err := os.ReadDir(dirRawScope)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]pathwayFileInfo{}, nil
		}
		return nil, fmt.Errorf("scan pathway scope dir %s: %w", dirRawScope, err)
	}

	index := make(map[string]pathwayFileInfo, len(entries))
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil {
			return nil, fmt.Errorf("stat pathway scope entry %s: %w", filepath.Join(dirRawScope, entry.Name()), err)
		}
		index[entry.Name()] = pathwayFileInfo{size: info.Size(), isDir: info.IsDir()}
	}
	return index, nil
}

func buildPathwayManifestReuseIndex(fileManifest string) (map[string]pathwayRecord, error) {
	records, err := readExistingPathwayRecords(fileManifest)
	if err != nil {
		return nil, err
	}
	index := make(map[string]pathwayRecord, len(records))
	for _, record := range records {
		if record.PathRel == "" {
			continue
		}
		index[record.PathRel] = record
	}
	return index, nil
}

func writeDownloadedFile(
	fileOut string,
	pathRel string,
	pathwayID string,
	assetName string,
	urlFile string,
	data []byte,
) (pathwayRecord, error) {
	if err := os.WriteFile(fileOut, data, 0o644); err != nil {
		return pathwayRecord{}, fmt.Errorf("write %s: %w", fileOut, err)
	}
	return buildPathwayRecord(fileOut, pathRel, pathwayID, assetName, urlFile)
}

func inspectExistingFile(
	filePath string,
	pathRel string,
	pathwayID string,
	assetName string,
	urlFile string,
) (pathwayRecord, bool, error) {
	infoFile, err := os.Stat(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return pathwayRecord{}, false, nil
		}
		return pathwayRecord{}, false, fmt.Errorf("stat existing file: %w", err)
	}
	if infoFile.Size() <= 0 {
		return pathwayRecord{}, false, nil
	}

	record, err := buildPathwayRecord(filePath, pathRel, pathwayID, assetName, urlFile)
	if err != nil {
		return pathwayRecord{}, false, err
	}
	return record, true, nil
}

func buildPathwayRecord(
	filePath string,
	pathRel string,
	pathwayID string,
	assetName string,
	urlFile string,
) (pathwayRecord, error) {
	infoFile, err := os.Stat(filePath)
	if err != nil {
		return pathwayRecord{}, fmt.Errorf("stat file: %w", err)
	}
	sha256File, err := calculateSHA256ForFile(filePath)
	if err != nil {
		return pathwayRecord{}, err
	}

	return pathwayRecord{
		PathwayID: pathwayID,
		Asset:     assetName,
		PathRel:   pathRel,
		SHA256:    sha256File,
		Bytes:     infoFile.Size(),
		URL:       urlFile,
	}, nil
}

func writeManifest(
	fileManifest string,
	cfg *pathwayConfig,
	records []pathwayRecord,
	timeDownloaded time.Time,
) error {
	manifest := buildManifestFile(cfg, records, timeDownloaded)
	return tomlx.WriteFileAtomic(fileManifest, manifest)
}

func buildCompletePathwayRecords(
	fileManifest string,
	dirVersion string,
	recordsCurrent []pathwayRecord,
) ([]pathwayRecord, error) {
	recordsExisting, err := readExistingPathwayRecords(fileManifest)
	if err != nil {
		return nil, err
	}

	recordsMerged := make(map[string]pathwayRecord, len(recordsExisting)+len(recordsCurrent))
	for _, record := range recordsExisting {
		recordsMerged[record.PathRel] = record
	}
	for _, record := range recordsCurrent {
		recordsMerged[record.PathRel] = record
	}

	records := make([]pathwayRecord, 0, len(recordsMerged))
	for _, record := range recordsMerged {
		filePath := filepath.Join(dirVersion, filepath.FromSlash(record.PathRel))
		infoFile, err := os.Stat(filePath)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, fmt.Errorf("stat manifest file %s: %w", filePath, err)
		}
		if infoFile.IsDir() {
			continue
		}
		records = append(records, record)
	}

	sort.Slice(records, func(i, j int) bool {
		if records[i].PathwayID != records[j].PathwayID {
			return records[i].PathwayID < records[j].PathwayID
		}
		return records[i].Asset < records[j].Asset
	})
	return records, nil
}

func readExistingPathwayRecords(fileManifest string) ([]pathwayRecord, error) {
	var manifest manifestFile
	ok, err := tomlx.ReadFileIfExists(fileManifest, &manifest)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, nil
	}

	records := make([]pathwayRecord, 0, len(manifest.Files))
	for _, item := range manifest.Files {
		records = append(records, pathwayRecord{
			PathwayID: item.PathwayID,
			Asset:     item.Asset,
			PathRel:   item.Path,
			SHA256:    item.SHA256,
			Bytes:     item.Bytes,
			URL:       item.URL,
		})
	}
	return records, nil
}

func buildManifestFile(
	cfg *pathwayConfig,
	records []pathwayRecord,
	timeDownloaded time.Time,
) manifestFile {
	sourceRelease, sourceReleaseStart, sourceReleaseEnd := deriveKEGGReleaseFields(
		cfg.sourceRelease,
		cfg.sourceReleaseStart,
		cfg.sourceReleaseEnd,
	)
	sourceLastUpdate, sourceLastUpdateStart, sourceLastUpdateEnd := deriveKEGGReleaseFields(
		cfg.sourceLastUpdate,
		cfg.sourceLastUpdateStart,
		cfg.sourceLastUpdateEnd,
	)
	mapRecordsByPathway := make(map[string][]pathwayRecord)
	pathwayIDs := make([]string, 0)
	for _, record := range records {
		if record.PathwayID == "" {
			continue
		}
		if _, ok := mapRecordsByPathway[record.PathwayID]; !ok {
			pathwayIDs = append(pathwayIDs, record.PathwayID)
		}
		mapRecordsByPathway[record.PathwayID] = append(mapRecordsByPathway[record.PathwayID], record)
	}
	sort.Strings(pathwayIDs)

	pathways := make([]manifestPathway, 0, len(pathwayIDs))
	for _, pathwayID := range pathwayIDs {
		recordsPathway := mapRecordsByPathway[pathwayID]
		sort.Slice(recordsPathway, func(i, j int) bool {
			return recordsPathway[i].Asset < recordsPathway[j].Asset
		})

		paths := make([]string, 0, len(recordsPathway))
		for _, record := range recordsPathway {
			paths = append(paths, record.PathRel)
		}
		pathways = append(pathways, manifestPathway{
			ID:    pathwayID,
			Files: paths,
		})
	}

	files := make([]manifestAsset, 0, len(records))
	for _, record := range records {
		files = append(files, manifestAsset{
			PathwayID: record.PathwayID,
			Asset:     record.Asset,
			Path:      record.PathRel,
			SHA256:    record.SHA256,
			Bytes:     record.Bytes,
			URL:       record.URL,
		})
	}

	scopeType, scopeValue := derivePathwayManifestScope(cfg, records)

	return manifestFile{
		Database:              "kegg",
		Asset:                 "pathway",
		Version:               cfg.version,
		VersionToken:          cfg.versionToken,
		SourceRelease:         sourceRelease,
		SourceReleaseStart:    sourceReleaseStart,
		SourceReleaseEnd:      sourceReleaseEnd,
		SourceLastUpdate:      sourceLastUpdate,
		SourceLastUpdateStart: sourceLastUpdateStart,
		SourceLastUpdateEnd:   sourceLastUpdateEnd,
		DownloadedAt:          timeDownloaded.Format(time.RFC3339),
		Scope: manifestScope{
			Type:  scopeType,
			Value: scopeValue,
		},
		Pathways: pathways,
		Files:    files,
	}
}

func derivePathwayManifestScope(cfg *pathwayConfig, records []pathwayRecord) (string, string) {
	setScopes := make(map[string]struct{})
	for _, record := range records {
		scopeKey := derivePathwayScopeFromPath(record.PathRel)
		if scopeKey != "" {
			setScopes[scopeKey] = struct{}{}
		}
	}

	scopeKeys := sets.SortedKeys(setScopes)
	switch len(scopeKeys) {
	case 0:
		if cfg.shouldFetchReference {
			return "reference", "pathway"
		}
		if cfg.scopeType != "" || cfg.scopeValue != "" {
			return cfg.scopeType, cfg.scopeValue
		}
		if cfg.organismCode != "" {
			return "organism", cfg.organismCode
		}
		return "organisms", ""
	case 1:
		if scopeKeys[0] == "reference" {
			return "reference", "pathway"
		}
		return "organism", scopeKeys[0]
	default:
		filtered := make([]string, 0, len(scopeKeys))
		for _, scopeKey := range scopeKeys {
			if scopeKey == "reference" {
				continue
			}
			filtered = append(filtered, scopeKey)
		}
		if len(filtered) == 0 {
			return "reference", "pathway"
		}
		return "organisms", strings.Join(filtered, ",")
	}
}

func derivePathwayScopeFromPath(pathRel string) string {
	parts := strings.Split(pathRel, "/")
	if len(parts) < 3 || parts[0] != "raw" {
		return ""
	}
	return parts[1]
}

type keggClient struct {
	clientHTTP      *http.Client
	requestInterval time.Duration
	retryMax        int
	retryWait       time.Duration
	timeLastRequest time.Time
}

func createKEGGClient(
	clientHTTP *http.Client,
	requestInterval time.Duration,
	retryMax int,
	retryWait time.Duration,
) *keggClient {
	if retryMax < 1 {
		retryMax = 1
	}
	if retryWait < 0 {
		retryWait = 0
	}
	return &keggClient{
		clientHTTP:      clientHTTP,
		requestInterval: requestInterval,
		retryMax:        retryMax,
		retryWait:       retryWait,
	}
}

func (client *keggClient) download(urlFile string) ([]byte, error) {
	for attempt := 1; attempt <= client.retryMax; attempt++ {
		data, shouldRetry, err := client.downloadOnce(urlFile)
		if err == nil {
			return data, nil
		}
		if !shouldRetry || attempt == client.retryMax {
			return nil, err
		}
		if client.retryWait > 0 {
			logf(
				"request failed (%d/%d), retrying in %s: %v",
				attempt,
				client.retryMax,
				client.retryWait,
				err,
			)
			time.Sleep(client.retryWait)
			continue
		}
		logf("request failed (%d/%d), retrying: %v", attempt, client.retryMax, err)
	}
	return nil, fmt.Errorf("request %s: exhausted retries", urlFile)
}

func (client *keggClient) downloadFile(urlFile string, fileOut string) error {
	for attempt := 1; attempt <= client.retryMax; attempt++ {
		shouldRetry, err := client.downloadFileOnce(urlFile, fileOut)
		if err == nil {
			return nil
		}
		if !shouldRetry || attempt == client.retryMax {
			return err
		}
		if client.retryWait > 0 {
			logf(
				"request failed (%d/%d), retrying in %s: %v",
				attempt,
				client.retryMax,
				client.retryWait,
				err,
			)
			time.Sleep(client.retryWait)
			continue
		}
		logf("request failed (%d/%d), retrying: %v", attempt, client.retryMax, err)
	}
	return fmt.Errorf("request %s: exhausted retries", urlFile)
}

func (client *keggClient) downloadFileOnce(urlFile string, fileOut string) (bool, error) {
	if client.requestInterval > 0 && !client.timeLastRequest.IsZero() {
		wait := client.requestInterval - time.Since(client.timeLastRequest)
		if wait > 0 {
			time.Sleep(wait)
		}
	}
	filePart := fileOut + ".part"
	err := httpx.DownloadFileWithResume(client.clientHTTP, urlFile, filePart, nil)
	client.timeLastRequest = time.Now()
	if err != nil {
		var statusErr httpx.UnexpectedStatusError
		if errors.As(err, &statusErr) {
			return isRetryableKEGGStatus(statusErr.Code), err
		}
		return isRetryableKEGGError(err), err
	}
	if err := os.Rename(filePart, fileOut); err != nil {
		return false, fmt.Errorf("rename %s -> %s: %w", filePart, fileOut, err)
	}
	return false, nil
}

func (client *keggClient) downloadOnce(urlFile string) ([]byte, bool, error) {
	if client.requestInterval > 0 && !client.timeLastRequest.IsZero() {
		wait := client.requestInterval - time.Since(client.timeLastRequest)
		if wait > 0 {
			time.Sleep(wait)
		}
	}

	response, err := client.clientHTTP.Get(urlFile)
	client.timeLastRequest = time.Now()
	if err != nil {
		return nil, isRetryableKEGGError(err), fmt.Errorf("request %s: %w", urlFile, err)
	}
	defer response.Body.Close()

	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, isRetryableKEGGStatus(response.StatusCode), httpx.UnexpectedStatusError{URL: urlFile, Status: response.Status, Code: response.StatusCode}
	}

	data, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, isRetryableKEGGError(err), fmt.Errorf("read %s: %w", urlFile, err)
	}
	return data, false, nil
}

func createHTTPClient(shouldAllowInsecureTLS bool) *http.Client {
	return httpx.NewClient(shouldAllowInsecureTLS)
}

func isRetryableKEGGStatus(statusCode int) bool {
	switch statusCode {
	case http.StatusForbidden, http.StatusRequestTimeout, http.StatusTooEarly, http.StatusTooManyRequests:
		return true
	}
	return statusCode >= 500
}

func isRetryableKEGGError(err error) bool {
	if err == nil {
		return false
	}

	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return true
	}

	type temporary interface {
		Temporary() bool
	}
	var errTemporary temporary
	if errors.As(err, &errTemporary) && errTemporary.Temporary() {
		return true
	}

	return errors.Is(err, io.EOF) ||
		errors.Is(err, io.ErrUnexpectedEOF) ||
		errors.Is(err, net.ErrClosed) ||
		errors.Is(err, syscall.ECONNRESET) ||
		errors.Is(err, syscall.ECONNABORTED) ||
		errors.Is(err, syscall.EPIPE)
}

func calculateSHA256ForFile(filePath string) (string, error) {
	fileIn, err := os.Open(filePath)
	if err != nil {
		return "", fmt.Errorf("open %s for sha256: %w", filePath, err)
	}
	defer fileIn.Close()

	hashSHA256 := sha256.New()
	if _, err := io.Copy(hashSHA256, fileIn); err != nil {
		return "", fmt.Errorf("hash %s: %w", filePath, err)
	}
	return fmt.Sprintf("%x", hashSHA256.Sum(nil)), nil
}

func isValidPathwayID(text string) bool {
	if len(text) < 7 {
		return false
	}
	prefix := text[:len(text)-5]
	suffix := text[len(text)-5:]
	if prefix == "" {
		return false
	}
	for _, char := range prefix {
		if !(char >= 'a' && char <= 'z') && !(char >= 'A' && char <= 'Z') {
			return false
		}
	}
	for _, char := range suffix {
		if char < '0' || char > '9' {
			return false
		}
	}
	return true
}

func normalizePathwayID(text string) string {
	return strings.TrimSpace(text)
}

func logf(format string, args ...interface{}) {
	logx.Logf("biofetch kegg", format, args...)
}

func resolveKEGGInfoMetadata(clientKegg *keggClient, databaseName string) (keggInfoMetadata, error) {
	dataInfo, err := clientKegg.download(baseURL + "/info/" + databaseName)
	if err != nil {
		return keggInfoMetadata{}, err
	}
	metadata, err := parseKEGGInfoMetadata(dataInfo)
	if err != nil {
		return keggInfoMetadata{}, err
	}
	return metadata, nil
}

func parseKEGGReleaseFromInfo(data []byte) (string, error) {
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		if value := parseKEGGInfoLineValue(line, "Release "); value != "" {
			return value, nil
		}
	}
	if err := scanner.Err(); err != nil {
		return "", fmt.Errorf("scan KEGG info output: %w", err)
	}
	return "", newKEGGReleaseMissingError(data)
}

func parseKEGGInfoMetadata(data []byte) (keggInfoMetadata, error) {
	metadata := keggInfoMetadata{}
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		if value := parseKEGGInfoLineValue(line, "Release "); value != "" {
			metadata.sourceRelease = value
		}
		if value := parseKEGGInfoLineValue(line, "Last update "); value != "" {
			metadata.sourceLastUpdate = normalizeKEGGLastUpdate(value)
		}
		if metadata.sourceRelease != "" && metadata.sourceLastUpdate != "" {
			break
		}
	}
	if err := scanner.Err(); err != nil {
		return keggInfoMetadata{}, fmt.Errorf("scan KEGG info output: %w", err)
	}
	if metadata.sourceRelease == "" && metadata.sourceLastUpdate == "" {
		return keggInfoMetadata{}, newKEGGInfoMetadataMissingError(data)
	}
	return metadata, nil
}

func parseKEGGInfoLineValue(line string, textPrefix string) string {
	index := strings.Index(line, textPrefix)
	if index < 0 {
		return ""
	}
	textAfter := strings.TrimSpace(line[index+len(textPrefix):])
	if textAfter == "" {
		return ""
	}
	indexComma := strings.Index(textAfter, ",")
	if indexComma >= 0 {
		textAfter = strings.TrimSpace(textAfter[:indexComma])
	}
	return textAfter
}

func normalizeKEGGLastUpdate(value string) string {
	text := strings.TrimSpace(value)
	if timeParsed, err := time.Parse("2006/01/02", text); err == nil {
		return timeParsed.Format("2006-01-02")
	}
	return text
}

func newKEGGInfoMetadataMissingError(data []byte) error {
	textPreview := strings.TrimSpace(string(data))
	if textPreview == "" {
		return fmt.Errorf("failed to parse KEGG info metadata from info response: upstream response was empty")
	}
	if len(textPreview) > 160 {
		textPreview = textPreview[:160]
	}
	return fmt.Errorf(
		"failed to parse KEGG info metadata from info response: upstream response did not contain a 'Release ...' or 'Last update ...' field (preview=%q)",
		textPreview,
	)
}

func newKEGGReleaseMissingError(data []byte) error {
	textPreview := strings.TrimSpace(string(data))
	if textPreview == "" {
		return fmt.Errorf("failed to parse KEGG release from info response: upstream response was empty")
	}
	if len(textPreview) > 160 {
		textPreview = textPreview[:160]
	}
	return fmt.Errorf(
		"failed to parse KEGG release from info response: upstream response did not contain a 'Release ...' field (preview=%q)",
		textPreview,
	)
}

func parseKEGGMajorVersion(sourceRelease string) (string, error) {
	text := strings.TrimSpace(sourceRelease)
	if text == "" {
		return "", fmt.Errorf("KEGG source release is empty")
	}
	for i, ch := range text {
		if ch == '+' || ch == '/' || ch == ' ' || ch == ',' {
			text = strings.TrimSpace(text[:i])
			break
		}
	}
	if !isValidKEGGMajorVersion(text) {
		return "", fmt.Errorf("invalid KEGG major version in source release %q", sourceRelease)
	}
	return text, nil
}

func chunkStrings(values []string, sizeChunk int) [][]string {
	if len(values) == 0 {
		return nil
	}
	if sizeChunk < 1 {
		sizeChunk = 1
	}
	batches := make([][]string, 0, (len(values)+sizeChunk-1)/sizeChunk)
	for start := 0; start < len(values); start += sizeChunk {
		end := start + sizeChunk
		if end > len(values) {
			end = len(values)
		}
		batches = append(batches, values[start:end])
	}
	return batches
}
