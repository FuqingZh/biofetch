package omnipath

import (
	"biofetch/internal/shared/cliopt"
	"biofetch/internal/shared/logx"
	"biofetch/internal/shared/parallel"
	"biofetch/internal/shared/tomlx"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"time"
)

type lockConfig struct {
	dirSnapshot  string
	dirLogs      string
	workersMax   int
	shouldDryRun bool
}

type restoreConfig struct {
	dirOut                  string
	dirLogs                 string
	versionToken            string
	dataset                 string
	ruleExisting            string
	shouldOverwriteExisting bool
	retryMax                int
	retryWait               time.Duration
	shouldAllowInsecureTLS  bool
	shouldDryRun            bool
}

func runLockEnzSub(cfg *lockConfig) error {
	return runLockCommon(cfg, "enz_sub", "")
}

func runLockInteractions(cfg *lockConfig) error {
	dataset := filepath.Base(filepath.Dir(filepath.Clean(cfg.dirSnapshot)))
	return runLockCommon(cfg, "interactions", dataset)
}

func runLockCommon(cfg *lockConfig, asset string, dataset string) error {
	if err := cliopt.NormalizeLockWorkersMax(&cfg.workersMax); err != nil {
		return err
	}
	versionToken, err := cliopt.SnapshotVersionToken(cfg.dirSnapshot)
	if err != nil {
		return err
	}
	dirVersion := cfg.dirSnapshot
	fileManifest := filepath.Join(dirVersion, "manifest.lock")
	_, closeRun, err := logx.StartVersionedRun("biofetch omnipath", "lock", cfg.dirLogs, dirVersion)
	if err != nil {
		return err
	}
	defer closeRun()
	manifestExisting, _ := readExistingManifest(fileManifest)
	records, err := scanOmniPathRecords(dirVersion, asset, dataset, manifestExisting, cfg.workersMax)
	if err != nil {
		return err
	}

	manifest := manifestFile{
		Database:     "omnipath",
		Asset:        asset,
		Dataset:      dataset,
		Version:      versionToken,
		VersionToken: versionToken,
		DownloadedAt: time.Now().Format(time.RFC3339),
		Scope: func() manifestScope {
			scopeType, scopeValue := deriveOmniPathManifestScope(records)
			return manifestScope{Type: scopeType, Value: scopeValue}
		}(),
		RequestURL: deriveOmniPathRequestURL(records),
		QueryURL:   deriveOmniPathQueryURL(records, ""),
		Files:      records,
	}
	if asset == "interactions" {
		query, err := lockInteractionQuery(filepath.Join(dirVersion, "raw", "query_meta.json"), records, dataset, versionToken)
		if err != nil {
			return err
		}
		manifest.Query = query
		if query.Schema == fullEvidenceSchema {
			manifest.Version = query.AcquiredAt
			manifest.DownloadedAt = query.AcquiredAt
		}
	}

	if cfg.shouldDryRun {
		logf("[dry-run] lock version dir: %s", dirVersion)
		logf("[dry-run] manifest: %s", fileManifest)
		logf("[dry-run] files=%d", len(records))
		return nil
	}
	return writeManifest(fileManifest, manifest)
}

func runRestoreEnzSub(cfg *restoreConfig) error {
	dirVersion := filepath.Join(cfg.dirOut, "enz_sub", cfg.versionToken)
	return runRestoreCommon(cfg, dirVersion, "enz_sub")
}

func runRestoreInteractions(cfg *restoreConfig) error {
	dirVersion := filepath.Join(cfg.dirOut, "interactions", cfg.dataset, cfg.versionToken)
	return runRestoreCommon(cfg, dirVersion, "interactions")
}

func runRestoreCommon(cfg *restoreConfig, dirVersion string, asset string) error {
	fileManifest := filepath.Join(dirVersion, "manifest.lock")
	_, closeRun, err := logx.StartVersionedRun("biofetch omnipath", "restore", cfg.dirLogs, dirVersion)
	if err != nil {
		return err
	}
	defer closeRun()
	manifestExisting, err := readExistingManifest(fileManifest)
	if err != nil {
		return err
	}
	if len(manifestExisting.Files) == 0 {
		return fmt.Errorf("manifest is empty or missing: %s", fileManifest)
	}
	if strings.HasPrefix(strings.TrimSpace(manifestExisting.QueryURL), archiveURL) {
		return runRestoreArchive(cfg, dirVersion, manifestExisting, asset)
	}
	if asset == "interactions" && manifestExisting.Query.Schema == fullEvidenceSchema {
		return runRestoreFullEvidence(cfg, dirVersion, manifestExisting)
	}

	client := createClient(cfg.shouldAllowInsecureTLS, cfg.retryMax, cfg.retryWait)
	recordsCurrent := make([]recordFile, 0, len(manifestExisting.Files))
	for _, record := range manifestExisting.Files {
		filePath := filepath.Join(dirVersion, filepath.FromSlash(record.Path))
		if cfg.shouldDryRun {
			logf("[dry-run] restore %s -> %s", record.URL, filePath)
			continue
		}

		if err := os.MkdirAll(filepath.Dir(filePath), 0o755); err != nil {
			return fmt.Errorf("create restore dir: %w", err)
		}

		shouldDownload := cfg.shouldOverwriteExisting
		if !shouldDownload {
			recordCurrent, ok, err := inspectExisting(filePath, record.Path, record.URL, record.Asset)
			if err != nil {
				return err
			}
			if ok && recordCurrent.SHA256 == record.SHA256 {
				recordsCurrent = append(recordsCurrent, recordCurrent)
				continue
			}
			shouldDownload = true
		}

		if shouldDownload {
			logf("restore downloading %s", filepath.Base(filePath))
			data, err := client.download(record.URL)
			if err != nil {
				return err
			}
			if err := os.WriteFile(filePath, data, 0o644); err != nil {
				return fmt.Errorf("write %s: %w", filePath, err)
			}
		}

		recordCurrent, err := buildRecord(filePath, record.Path, record.URL, record.Asset)
		if err != nil {
			return err
		}
		recordsCurrent = append(recordsCurrent, recordCurrent)
	}

	if cfg.shouldDryRun {
		logf("[dry-run] restore done (files=%d)", len(manifestExisting.Files))
		return nil
	}

	recordsComplete, err := buildCompleteOmniPathRecords(fileManifest, dirVersion, recordsCurrent)
	if err != nil {
		return err
	}
	manifestExisting.DownloadedAt = time.Now().Format(time.RFC3339)
	manifestExisting.Scope = func() manifestScope {
		scopeType, scopeValue := deriveOmniPathManifestScope(recordsComplete)
		return manifestScope{Type: scopeType, Value: scopeValue}
	}()
	manifestExisting.RequestURL = deriveOmniPathRequestURL(recordsComplete)
	manifestExisting.QueryURL = deriveOmniPathQueryURL(recordsComplete, manifestExisting.QueryURL)
	manifestExisting.Files = recordsComplete
	if err := writeManifest(fileManifest, manifestExisting); err != nil {
		return err
	}

	logf("restore done (files=%d)", len(recordsComplete))
	logf("manifest written: %s", fileManifest)
	return nil
}

func lockInteractionQuery(path string, records []recordFile, dataset, versionToken string) (manifestQuery, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return manifestQuery{}, fmt.Errorf("read query sidecar: %w", err)
	}
	var meta interactionQueryMeta
	if !json.Valid(data) {
		if strings.HasPrefix(strings.TrimSpace(string(data)), "{") || strings.HasPrefix(strings.TrimSpace(string(data)), "[") {
			return manifestQuery{}, fmt.Errorf("query sidecar contains malformed JSON")
		}
		return manifestQuery{Schema: "legacy-basic", SidecarPath: "raw/query_meta.json",
			SidecarSHA: recordSHA(records, "raw/query_meta.json")}, nil
	}
	if err := json.Unmarshal(data, &meta); err != nil {
		return manifestQuery{}, err
	}
	if meta.Schema != fullEvidenceSchema {
		var archive struct {
			Mode string `json:"mode"`
		}
		if err := json.Unmarshal(data, &archive); err == nil && archive.Mode == "archive" {
			return manifestQuery{Schema: "legacy-basic", SidecarPath: "raw/query_meta.json",
				SidecarSHA: recordSHA(records, "raw/query_meta.json")}, nil
		}
		return manifestQuery{}, fmt.Errorf("unsupported JSON query sidecar schema %q", meta.Schema)
	}
	if meta.Query.Schema != fullEvidenceSchema || meta.Transport != interactionTransport {
		return manifestQuery{}, fmt.Errorf("query sidecar schema/transport mismatch")
	}
	if meta.Query.Dataset != dataset || meta.Query.License != normalizeLicense(meta.Query.License) {
		return manifestQuery{}, fmt.Errorf("query sidecar dataset/license mismatch")
	}
	if !reflect.DeepEqual(meta.Query.Fields, fullEvidenceQueryFields) ||
		!reflect.DeepEqual(meta.Query.OutputFields, fullEvidenceFields) {
		return manifestQuery{}, fmt.Errorf("query sidecar fixed field profile mismatch")
	}
	if !reflect.DeepEqual(meta.Query.Organisms, deriveOmniPathTaxIDsFromRecords(records)) {
		return manifestQuery{}, fmt.Errorf("query sidecar organisms do not match snapshot files")
	}
	if len(meta.Query.Levels) > 0 {
		levels, err := normalizeDorotheaLevels(meta.Query.Levels)
		if err != nil || !reflect.DeepEqual(levels, meta.Query.Levels) {
			return manifestQuery{}, fmt.Errorf("query sidecar DoRothEA levels are not canonical A-D")
		}
	}
	if (dataset == "dorothea") != (len(meta.Query.Levels) > 0) {
		return manifestQuery{}, fmt.Errorf("query sidecar DoRothEA levels do not match dataset")
	}
	canonical, err := json.Marshal(meta.Query)
	if err != nil {
		return manifestQuery{}, err
	}
	fingerprint := fmt.Sprintf("%x", sha256.Sum256(canonical))
	if fingerprint != meta.Fingerprint {
		return manifestQuery{}, fmt.Errorf("query sidecar fingerprint mismatch: got %s want %s", meta.Fingerprint, fingerprint)
	}
	if !strings.HasSuffix(versionToken, "-"+fingerprint[:12]) {
		return manifestQuery{}, fmt.Errorf("query fingerprint does not match snapshot version token")
	}
	acquiredAt, err := time.Parse(time.RFC3339Nano, meta.AcquiredAtUTC)
	if err != nil || !strings.HasPrefix(versionToken, acquiredAt.UTC().Format("20060102T150405.000000000Z")+"-") {
		return manifestQuery{}, fmt.Errorf("query acquisition timestamp does not match snapshot version token")
	}
	if meta.Start != meta.End {
		return manifestQuery{}, fmt.Errorf("query sidecar inventory drift: start and end differ")
	}
	seenTargets := map[string]map[string]struct{}{}
	edges := 0
	for _, leaf := range meta.LeafBatches {
		if !containsString(meta.Query.Organisms, leaf.Organism) || len(leaf.Targets) == 0 || leaf.ExpectedEdges < 0 {
			return manifestQuery{}, fmt.Errorf("query sidecar contains invalid leaf identity")
		}
		values, err := url.Parse(leaf.URL)
		if err != nil {
			return manifestQuery{}, fmt.Errorf("query sidecar contains invalid leaf URL: %w", err)
		}
		params := values.Query()
		if params.Get("datasets") != dataset || params.Get("license") != meta.Query.License ||
			params.Get("organisms") != leaf.Organism ||
			params.Get("fields") != strings.Join(meta.Query.Fields, ",") ||
			params.Get("targets") != strings.Join(leaf.Targets, ",") {
			return manifestQuery{}, fmt.Errorf("query sidecar leaf URL does not match canonical query")
		}
		if seenTargets[leaf.Organism] == nil {
			seenTargets[leaf.Organism] = map[string]struct{}{}
		}
		for _, target := range leaf.Targets {
			if _, duplicate := seenTargets[leaf.Organism][target]; duplicate {
				return manifestQuery{}, fmt.Errorf("query sidecar target %q is duplicated", target)
			}
			seenTargets[leaf.Organism][target] = struct{}{}
		}
		edges += leaf.ExpectedEdges
	}
	if edges != meta.Start.Edges {
		return manifestQuery{}, fmt.Errorf("query sidecar leaf expected edges=%d inventory=%d", edges, meta.Start.Edges)
	}
	leaves := make([]manifestLeaf, len(meta.LeafBatches))
	for i, leaf := range meta.LeafBatches {
		leaves[i] = manifestLeaf(leaf)
	}
	return manifestQuery{
		Schema: meta.Schema, Fingerprint: meta.Fingerprint, License: meta.Query.License,
		Fields: meta.Query.Fields, OutputFields: meta.Query.OutputFields, Levels: meta.Query.Levels, SidecarPath: "raw/query_meta.json",
		SidecarSHA: recordSHA(records, "raw/query_meta.json"), AcquiredAt: meta.AcquiredAtUTC,
		Transport: meta.Transport, Organisms: meta.Query.Organisms,
		StartSHA: meta.Start.SHA256, StartEdges: meta.Start.Edges,
		EndSHA: meta.End.SHA256, EndEdges: meta.End.Edges, LeafBatches: leaves,
	}, nil
}

func containsString(values []string, expected string) bool {
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
}

func recordSHA(records []recordFile, path string) string {
	for _, record := range records {
		if record.Path == path {
			return record.SHA256
		}
	}
	return ""
}

func runRestoreArchive(cfg *restoreConfig, dirVersion string, manifestExisting manifestFile, asset string) error {
	fileManifest := filepath.Join(dirVersion, "manifest.lock")
	recordsCurrent := make([]recordFile, 0, len(manifestExisting.Files))
	shouldDownload := cfg.shouldOverwriteExisting

	if !shouldDownload {
		allMatch := true
		for _, record := range manifestExisting.Files {
			filePath := filepath.Join(dirVersion, filepath.FromSlash(record.Path))
			recordCurrent, ok, err := inspectExisting(filePath, record.Path, record.URL, record.Asset)
			if err != nil {
				return err
			}
			if !ok || recordCurrent.SHA256 != record.SHA256 {
				allMatch = false
				break
			}
			recordsCurrent = append(recordsCurrent, recordCurrent)
		}
		if allMatch {
			recordsComplete, err := buildCompleteOmniPathRecords(fileManifest, dirVersion, recordsCurrent)
			if err != nil {
				return err
			}
			manifestExisting.DownloadedAt = time.Now().Format(time.RFC3339)
			manifestExisting.Files = recordsComplete
			if err := writeManifest(fileManifest, manifestExisting); err != nil {
				return err
			}
			logf("restore done (files=%d)", len(recordsComplete))
			logf("manifest written: %s", fileManifest)
			return nil
		}
	}

	if cfg.shouldDryRun {
		logf("[dry-run] restore archive %s -> %s", manifestExisting.QueryURL, dirVersion)
		logf("[dry-run] restore done (files=%d)", len(manifestExisting.Files))
		return nil
	}

	taxIDs := deriveOmniPathTaxIDsFromRecords(manifestExisting.Files)
	client := createClient(cfg.shouldAllowInsecureTLS, cfg.retryMax, cfg.retryWait)
	recordsArchive, err := materializeArchiveSnapshot(client, archiveMaterializeInput{
		asset:        asset,
		dataset:      manifestExisting.Dataset,
		version:      firstNonEmpty(manifestExisting.Version, manifestExisting.VersionToken),
		versionToken: manifestExisting.VersionToken,
		taxIDs:       taxIDs,
		urlArchive:   manifestExisting.QueryURL,
		dirVersion:   dirVersion,
	})
	if err != nil {
		return err
	}

	recordsComplete, err := buildCompleteOmniPathRecords(fileManifest, dirVersion, recordsArchive)
	if err != nil {
		return err
	}
	manifestExisting.DownloadedAt = time.Now().Format(time.RFC3339)
	manifestExisting.Scope = func() manifestScope {
		scopeType, scopeValue := deriveOmniPathManifestScope(recordsComplete)
		return manifestScope{Type: scopeType, Value: scopeValue}
	}()
	manifestExisting.RequestURL = firstNonEmpty(manifestExisting.RequestURL, manifestExisting.QueryURL)
	manifestExisting.QueryURL = firstNonEmpty(manifestExisting.QueryURL, manifestExisting.RequestURL)
	manifestExisting.Files = recordsComplete
	if err := writeManifest(fileManifest, manifestExisting); err != nil {
		return err
	}

	logf("restore done (files=%d)", len(recordsComplete))
	logf("manifest written: %s", fileManifest)
	return nil
}

func scanOmniPathRecords(dirVersion string, asset string, dataset string, manifestExisting manifestFile, workersMax int) ([]recordFile, error) {
	type taskRecordFile struct {
		filePath string
		pathRel  string
		urlFile  string
		asset    string
	}

	dirRaw := filepath.Join(dirVersion, "raw")
	fullEvidenceSidecar := false
	if asset == "interactions" {
		var meta interactionQueryMeta
		if data, err := os.ReadFile(filepath.Join(dirRaw, "query_meta.json")); err == nil &&
			json.Unmarshal(data, &meta) == nil && meta.Schema == fullEvidenceSchema {
			fullEvidenceSidecar = true
		}
	}
	urlsExisting := buildOmniPathExistingURLMap(manifestExisting)
	urlArchive := firstNonEmpty(
		strings.TrimSpace(manifestExisting.QueryURL),
		strings.TrimSpace(manifestExisting.RequestURL),
		deriveArchiveURLFromQueryMeta(filepath.Join(dirRaw, "query_meta.json")),
	)
	entries, err := os.ReadDir(dirRaw)
	if err != nil {
		return nil, fmt.Errorf("read raw dir: %w", err)
	}

	tasks := make([]taskRecordFile, 0)
	for _, entry := range entries {
		if entry.IsDir() {
			taxID := entry.Name()
			dirTaxID := filepath.Join(dirRaw, taxID)
			entriesFiles, err := os.ReadDir(dirTaxID)
			if err != nil {
				return nil, fmt.Errorf("read taxid dir %s: %w", dirTaxID, err)
			}
			for _, entryFile := range entriesFiles {
				if entryFile.IsDir() {
					continue
				}
				fileName := entryFile.Name()
				if fileName != asset+".tsv" {
					return nil, fmt.Errorf("unexpected OmniPath raw file: %s", filepath.Join(taxID, fileName))
				}
				filePath := filepath.Join(dirTaxID, fileName)
				pathRel := filepath.ToSlash(filepath.Join("raw", taxID, fileName))
				urlFile := urlsExisting[pathRel]
				if urlFile == "" && !fullEvidenceSidecar {
					if strings.HasPrefix(urlArchive, archiveURL) {
						urlFile = urlArchive
					} else {
						urlFile = deriveOmniPathDataURL(asset, dataset, taxID)
					}
				}
				tasks = append(tasks, taskRecordFile{
					filePath: filePath,
					pathRel:  pathRel,
					urlFile:  urlFile,
					asset:    asset,
				})
			}
			continue
		}

		if entry.Name() != "query_meta.json" {
			return nil, fmt.Errorf("unexpected OmniPath raw file: %s", entry.Name())
		}
		filePath := filepath.Join(dirRaw, entry.Name())
		pathRel := filepath.ToSlash(filepath.Join("raw", entry.Name()))
		urlQuery := urlsExisting[pathRel]
		if urlQuery == "" {
			if strings.HasPrefix(urlArchive, archiveURL) {
				urlQuery = urlArchive
			} else {
				urlQuery = queryEnzSubURL
				if asset == "interactions" {
					urlQuery = queryInteractionsURL
				}
			}
		}
		tasks = append(tasks, taskRecordFile{
			filePath: filePath,
			pathRel:  pathRel,
			urlFile:  urlQuery,
			asset:    "query_meta",
		})
	}

	records, err := parallel.MapOrderedWithWorkers(tasks, workersMax, func(task taskRecordFile) (recordFile, error) {
		return buildRecord(task.filePath, task.pathRel, task.urlFile, task.asset)
	})
	if err != nil {
		return nil, err
	}

	sort.Slice(records, func(i, j int) bool {
		if records[i].Path != records[j].Path {
			return records[i].Path < records[j].Path
		}
		return records[i].Asset < records[j].Asset
	})
	return records, nil
}

func deriveOmniPathDataURL(asset string, dataset string, taxID string) string {
	params := urlForTaxID(asset, dataset, taxID)
	if asset == "interactions" {
		return baseURL + "/interactions?" + params
	}
	return baseURL + "/enzsub?" + params
}

func urlForTaxID(asset string, dataset string, taxID string) string {
	if asset == "interactions" {
		return "datasets=" + dataset + "&format=tsv&organisms=" + taxID
	}
	return "format=tsv&organisms=" + taxID
}

func readExistingManifest(fileManifest string) (manifestFile, error) {
	var manifest manifestFile
	ok, err := tomlx.ReadFileIfExists(fileManifest, &manifest)
	if err != nil {
		return manifestFile{}, err
	}
	if !ok {
		return manifestFile{}, nil
	}
	return manifest, nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func buildOmniPathExistingURLMap(manifestExisting manifestFile) map[string]string {
	urls := make(map[string]string, len(manifestExisting.Files))
	for _, record := range manifestExisting.Files {
		if strings.TrimSpace(record.Path) == "" || strings.TrimSpace(record.URL) == "" {
			continue
		}
		urls[record.Path] = record.URL
	}
	return urls
}

func deriveOmniPathTaxIDsFromRecords(records []recordFile) []string {
	setTaxIDs := make(map[string]struct{})
	for _, record := range records {
		taxID := deriveOmniPathTaxIDFromPath(record.Path)
		if taxID == "" {
			continue
		}
		setTaxIDs[taxID] = struct{}{}
	}
	return sortedKeys(setTaxIDs)
}
