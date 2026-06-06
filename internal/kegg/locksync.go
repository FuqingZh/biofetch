package kegg

import (
	"biofetch/internal/shared/parallel"
	"biofetch/internal/shared/tomlx"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type keggLockConfig struct {
	dirOut          string
	versionToken    string
	requestInterval time.Duration
	shouldDryRun    bool
}

type keggSyncConfig struct {
	dirOut                  string
	versionToken            string
	ruleExisting            string
	shouldOverwriteExisting bool
	requestInterval         time.Duration
	shouldAllowInsecureTLS  bool
	shouldDryRun            bool
}

func runLockPathway(cfg *keggLockConfig) error {
	dirVersion := filepath.Join(cfg.dirOut, "pathway", cfg.versionToken)
	fileManifest := filepath.Join(dirVersion, "manifest.lock")

	records, err := scanPathwayRecords(dirVersion)
	if err != nil {
		return err
	}

	manifestExisting, _ := readExistingPathwayManifest(fileManifest)
	cfgManifest := pathwayConfig{
		version:            firstNonEmpty(manifestExisting.Version, cfg.versionToken),
		versionToken:       firstNonEmpty(manifestExisting.VersionToken, cfg.versionToken),
		sourceRelease:      manifestExisting.SourceRelease,
		sourceReleaseStart: manifestExisting.SourceReleaseStart,
		sourceReleaseEnd:   manifestExisting.SourceReleaseEnd,
	}

	if cfg.shouldDryRun {
		logf("[dry-run] lock version dir: %s", dirVersion)
		logf("[dry-run] manifest: %s", fileManifest)
		logf("[dry-run] files=%d", len(records))
		return nil
	}

	return writeManifest(fileManifest, &cfgManifest, records, time.Now())
}

func runSyncPathway(cfg *keggSyncConfig) error {
	dirVersion := filepath.Join(cfg.dirOut, "pathway", cfg.versionToken)
	fileManifest := filepath.Join(dirVersion, "manifest.lock")

	manifestExisting, err := readExistingPathwayManifest(fileManifest)
	if err != nil {
		return err
	}
	if len(manifestExisting.Files) == 0 {
		return fmt.Errorf("manifest is empty or missing: %s", fileManifest)
	}

	clientHTTP := createHTTPClient(cfg.shouldAllowInsecureTLS)
	clientKegg := createKEGGClient(clientHTTP, cfg.requestInterval, defaultKEGGRetryMax, defaultKEGGRetryWait)
	recordsCurrent := make([]pathwayRecord, 0, len(manifestExisting.Files))

	for _, item := range manifestExisting.Files {
		record := pathwayRecord{
			PathwayID: item.PathwayID,
			Asset:     item.Asset,
			PathRel:   item.Path,
			SHA256:    item.SHA256,
			Bytes:     item.Bytes,
			URL:       item.URL,
		}
		filePath := filepath.Join(dirVersion, filepath.FromSlash(record.PathRel))
		if cfg.shouldDryRun {
			logf("[dry-run] sync %s -> %s", record.URL, filePath)
			continue
		}

		if err := os.MkdirAll(filepath.Dir(filePath), 0o755); err != nil {
			return fmt.Errorf("create sync dir: %w", err)
		}

		shouldDownload := cfg.shouldOverwriteExisting
		if !shouldDownload {
			recordCurrent, ok, err := inspectExistingFile(filePath, record.PathRel, record.PathwayID, record.Asset, record.URL)
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
			logf("sync downloading %s", filepath.Base(filePath))
			if err := clientKegg.downloadFile(record.URL, filePath); err != nil {
				return err
			}
			recordCurrent, err := buildPathwayRecord(filePath, record.PathRel, record.PathwayID, record.Asset, record.URL)
			if err != nil {
				return err
			}
			recordsCurrent = append(recordsCurrent, recordCurrent)
			continue
		}
	}

	if cfg.shouldDryRun {
		logf("[dry-run] sync done (files=%d)", len(manifestExisting.Files))
		return nil
	}

	recordsComplete, err := buildCompletePathwayRecords(fileManifest, dirVersion, recordsCurrent)
	if err != nil {
		return err
	}

	cfgManifest := pathwayConfig{
		version:            manifestExisting.Version,
		versionToken:       manifestExisting.VersionToken,
		sourceRelease:      manifestExisting.SourceRelease,
		sourceReleaseStart: manifestExisting.SourceReleaseStart,
		sourceReleaseEnd:   manifestExisting.SourceReleaseEnd,
	}
	if err := writeManifest(fileManifest, &cfgManifest, recordsComplete, time.Now()); err != nil {
		return err
	}

	logf("sync done (files=%d)", len(recordsComplete))
	logf("manifest written: %s", fileManifest)
	return nil
}

func runLockBrite(cfg *keggLockConfig) error {
	dirVersion := filepath.Join(cfg.dirOut, "brite", cfg.versionToken)
	fileManifest := filepath.Join(dirVersion, "manifest.lock")

	records, err := scanBriteRecords(dirVersion)
	if err != nil {
		return err
	}

	manifestExisting, _ := readExistingBriteManifest(fileManifest)
	cfgManifest := briteConfig{
		version:            firstNonEmpty(manifestExisting.Version, cfg.versionToken),
		versionToken:       firstNonEmpty(manifestExisting.VersionToken, cfg.versionToken),
		sourceRelease:      manifestExisting.SourceRelease,
		sourceReleaseStart: manifestExisting.SourceReleaseStart,
		sourceReleaseEnd:   manifestExisting.SourceReleaseEnd,
	}

	if cfg.shouldDryRun {
		logf("[dry-run] lock version dir: %s", dirVersion)
		logf("[dry-run] manifest: %s", fileManifest)
		logf("[dry-run] files=%d", len(records))
		return nil
	}

	return writeBriteManifest(fileManifest, &cfgManifest, records, time.Now())
}

func runSyncBrite(cfg *keggSyncConfig) error {
	dirVersion := filepath.Join(cfg.dirOut, "brite", cfg.versionToken)
	fileManifest := filepath.Join(dirVersion, "manifest.lock")

	manifestExisting, err := readExistingBriteManifest(fileManifest)
	if err != nil {
		return err
	}
	if len(manifestExisting.Files) == 0 {
		return fmt.Errorf("manifest is empty or missing: %s", fileManifest)
	}

	clientHTTP := createHTTPClient(cfg.shouldAllowInsecureTLS)
	clientKegg := createKEGGClient(clientHTTP, cfg.requestInterval, defaultKEGGRetryMax, defaultKEGGRetryWait)
	recordsCurrent := make([]briteRecord, 0, len(manifestExisting.Files))

	for _, item := range manifestExisting.Files {
		record := briteRecord{
			BriteID: item.BriteID,
			Asset:   item.Asset,
			PathRel: item.Path,
			SHA256:  item.SHA256,
			Bytes:   item.Bytes,
			URL:     item.URL,
		}
		filePath := filepath.Join(dirVersion, filepath.FromSlash(record.PathRel))
		if cfg.shouldDryRun {
			logf("[dry-run] sync %s -> %s", record.URL, filePath)
			continue
		}

		if err := os.MkdirAll(filepath.Dir(filePath), 0o755); err != nil {
			return fmt.Errorf("create sync dir: %w", err)
		}

		shouldDownload := cfg.shouldOverwriteExisting
		if !shouldDownload {
			recordCurrent, ok, err := inspectBriteFile(filePath, record.PathRel, record.BriteID, record.Asset, record.URL)
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
			logf("sync downloading %s", filepath.Base(filePath))
			if err := clientKegg.downloadFile(record.URL, filePath); err != nil {
				return err
			}
			recordCurrent, err := buildBriteRecord(filePath, record.PathRel, record.BriteID, record.Asset, record.URL)
			if err != nil {
				return err
			}
			recordsCurrent = append(recordsCurrent, recordCurrent)
			continue
		}
	}

	if cfg.shouldDryRun {
		logf("[dry-run] sync done (files=%d)", len(manifestExisting.Files))
		return nil
	}

	recordsComplete, err := buildCompleteBriteRecords(fileManifest, dirVersion, recordsCurrent)
	if err != nil {
		return err
	}

	cfgManifest := briteConfig{
		version:            manifestExisting.Version,
		versionToken:       manifestExisting.VersionToken,
		sourceRelease:      manifestExisting.SourceRelease,
		sourceReleaseStart: manifestExisting.SourceReleaseStart,
		sourceReleaseEnd:   manifestExisting.SourceReleaseEnd,
	}
	if err := writeBriteManifest(fileManifest, &cfgManifest, recordsComplete, time.Now()); err != nil {
		return err
	}

	logf("sync done (files=%d)", len(recordsComplete))
	logf("manifest written: %s", fileManifest)
	return nil
}

func scanPathwayRecords(dirVersion string) ([]pathwayRecord, error) {
	type taskPathwayRecord struct {
		filePath string
		pathRel  string
		scopeKey string
		fileName string
	}

	dirRaw := filepath.Join(dirVersion, "raw")
	entriesScopes, err := os.ReadDir(dirRaw)
	if err != nil {
		return nil, fmt.Errorf("read raw dir: %w", err)
	}

	tasks := make([]taskPathwayRecord, 0)
	for _, entryScope := range entriesScopes {
		if !entryScope.IsDir() {
			continue
		}
		scopeKey := entryScope.Name()
		dirScope := filepath.Join(dirRaw, scopeKey)
		entriesFiles, err := os.ReadDir(dirScope)
		if err != nil {
			return nil, fmt.Errorf("read scope dir %s: %w", dirScope, err)
		}

		for _, entryFile := range entriesFiles {
			if entryFile.IsDir() {
				continue
			}
			fileName := entryFile.Name()
			filePath := filepath.Join(dirScope, fileName)
			pathRel := filepath.ToSlash(filepath.Join("raw", scopeKey, fileName))
			tasks = append(tasks, taskPathwayRecord{
				filePath: filePath,
				pathRel:  pathRel,
				scopeKey: scopeKey,
				fileName: fileName,
			})
		}
	}

	records, err := parallel.MapOrdered(tasks, func(task taskPathwayRecord) (pathwayRecord, error) {
		return buildScannedPathwayRecord(task.filePath, task.pathRel, task.scopeKey, task.fileName)
	})
	if err != nil {
		return nil, err
	}

	sort.Slice(records, func(i, j int) bool {
		if records[i].PathwayID != records[j].PathwayID {
			return records[i].PathwayID < records[j].PathwayID
		}
		return records[i].Asset < records[j].Asset
	})
	return records, nil
}

func buildScannedPathwayRecord(filePath string, pathRel string, scopeKey string, fileName string) (pathwayRecord, error) {
	switch {
	case fileName == "pathway.list.tsv":
		urlFile := baseURL + "/list/pathway/" + scopeKey
		if scopeKey == "reference" {
			urlFile = baseURL + "/list/pathway"
		}
		return buildPathwayRecord(filePath, pathRel, "", "pathway.list", urlFile)
	case strings.HasSuffix(fileName, ".txt"):
		pathwayID := strings.TrimSuffix(fileName, ".txt")
		return buildPathwayRecord(filePath, pathRel, pathwayID, "pathway.entry", baseURL+"/get/"+pathwayID)
	case strings.HasSuffix(fileName, ".kgml"):
		pathwayID := strings.TrimSuffix(fileName, ".kgml")
		return buildPathwayRecord(filePath, pathRel, pathwayID, "pathway.kgml", baseURL+"/get/"+pathwayID+"/kgml")
	case strings.HasSuffix(fileName, ".conf"):
		pathwayID := strings.TrimSuffix(fileName, ".conf")
		return buildPathwayRecord(filePath, pathRel, pathwayID, "pathway.conf", baseURL+"/get/"+pathwayID+"/conf")
	case strings.HasSuffix(fileName, ".png"):
		pathwayID := strings.TrimSuffix(fileName, ".png")
		return buildPathwayRecord(filePath, pathRel, pathwayID, "pathway.image", baseURL+"/get/"+pathwayID+"/image")
	default:
		return pathwayRecord{}, fmt.Errorf("unexpected KEGG pathway filename: %s", fileName)
	}
}

func scanBriteRecords(dirVersion string) ([]briteRecord, error) {
	type taskBriteRecord struct {
		filePath string
		pathRel  string
		scopeKey string
		fileName string
	}

	dirRaw := filepath.Join(dirVersion, "raw")
	entriesScopes, err := os.ReadDir(dirRaw)
	if err != nil {
		return nil, fmt.Errorf("read raw dir: %w", err)
	}

	tasks := make([]taskBriteRecord, 0)
	for _, entryScope := range entriesScopes {
		if !entryScope.IsDir() {
			continue
		}
		scopeKey := entryScope.Name()
		dirScope := filepath.Join(dirRaw, scopeKey)
		entriesFiles, err := os.ReadDir(dirScope)
		if err != nil {
			return nil, fmt.Errorf("read scope dir %s: %w", dirScope, err)
		}

		for _, entryFile := range entriesFiles {
			if entryFile.IsDir() {
				continue
			}
			fileName := entryFile.Name()
			filePath := filepath.Join(dirScope, fileName)
			pathRel := filepath.ToSlash(filepath.Join("raw", scopeKey, fileName))
			tasks = append(tasks, taskBriteRecord{
				filePath: filePath,
				pathRel:  pathRel,
				scopeKey: scopeKey,
				fileName: fileName,
			})
		}
	}

	records, err := parallel.MapOrdered(tasks, func(task taskBriteRecord) (briteRecord, error) {
		return buildScannedBriteRecord(task.filePath, task.pathRel, task.scopeKey, task.fileName)
	})
	if err != nil {
		return nil, err
	}

	sort.Slice(records, func(i, j int) bool {
		if records[i].BriteID != records[j].BriteID {
			return records[i].BriteID < records[j].BriteID
		}
		return records[i].Asset < records[j].Asset
	})
	return records, nil
}

func buildScannedBriteRecord(filePath string, pathRel string, scopeKey string, fileName string) (briteRecord, error) {
	switch {
	case fileName == "brite.list.tsv":
		return buildBriteRecord(filePath, pathRel, "", "brite.list", baseURL+"/list/brite/"+scopeKey)
	case strings.HasSuffix(fileName, ".txt"):
		briteID := strings.TrimSuffix(fileName, ".txt")
		return buildBriteRecord(filePath, pathRel, briteID, "brite.entry", baseURL+"/get/br:"+briteID)
	case strings.HasSuffix(fileName, ".json"):
		briteID := strings.TrimSuffix(fileName, ".json")
		return buildBriteRecord(filePath, pathRel, briteID, "brite.json", baseURL+"/get/br:"+briteID+"/json")
	default:
		return briteRecord{}, fmt.Errorf("unexpected KEGG brite filename: %s", fileName)
	}
}

func readExistingPathwayManifest(fileManifest string) (manifestFile, error) {
	var manifest manifestFile
	ok, err := tomlx.ReadFileIfExists(fileManifest, &manifest)
	if err != nil {
		return manifestFile{}, err
	}
	if !ok {
		return manifestFile{}, nil
	}
	manifest.SourceRelease, manifest.SourceReleaseStart, manifest.SourceReleaseEnd = deriveKEGGReleaseFields(
		manifest.SourceRelease,
		manifest.SourceReleaseStart,
		manifest.SourceReleaseEnd,
	)
	return manifest, nil
}

func readExistingBriteManifest(fileManifest string) (briteManifestFile, error) {
	var manifest briteManifestFile
	ok, err := tomlx.ReadFileIfExists(fileManifest, &manifest)
	if err != nil {
		return briteManifestFile{}, err
	}
	if !ok {
		return briteManifestFile{}, nil
	}
	manifest.SourceRelease, manifest.SourceReleaseStart, manifest.SourceReleaseEnd = deriveKEGGReleaseFields(
		manifest.SourceRelease,
		manifest.SourceReleaseStart,
		manifest.SourceReleaseEnd,
	)
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
