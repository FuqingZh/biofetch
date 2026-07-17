package omnipath

import (
	"biofetch/internal/shared/cliopt"
	"biofetch/internal/shared/logx"
	"biofetch/internal/shared/parallel"
	"biofetch/internal/shared/tomlx"
	"fmt"
	"os"
	"path/filepath"
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

type syncConfig struct {
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
	records, err := scanOmniPathRecords(dirVersion, asset, manifestExisting, cfg.workersMax)
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

	if cfg.shouldDryRun {
		logf("[dry-run] lock version dir: %s", dirVersion)
		logf("[dry-run] manifest: %s", fileManifest)
		logf("[dry-run] files=%d", len(records))
		return nil
	}
	return writeManifest(fileManifest, manifest)
}

func runSyncEnzSub(cfg *syncConfig) error {
	dirVersion := filepath.Join(cfg.dirOut, "enz_sub", cfg.versionToken)
	return runSyncCommon(cfg, dirVersion, "enz_sub")
}

func runSyncInteractions(cfg *syncConfig) error {
	dirVersion := filepath.Join(cfg.dirOut, "interactions", cfg.dataset, cfg.versionToken)
	return runSyncCommon(cfg, dirVersion, "interactions")
}

func runSyncCommon(cfg *syncConfig, dirVersion string, asset string) error {
	fileManifest := filepath.Join(dirVersion, "manifest.lock")
	_, closeRun, err := logx.StartVersionedRun("biofetch omnipath", "sync", cfg.dirLogs, dirVersion)
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
		return runSyncArchive(cfg, dirVersion, manifestExisting, asset)
	}

	client := createClient(cfg.shouldAllowInsecureTLS, cfg.retryMax, cfg.retryWait)
	recordsCurrent := make([]recordFile, 0, len(manifestExisting.Files))
	for _, record := range manifestExisting.Files {
		filePath := filepath.Join(dirVersion, filepath.FromSlash(record.Path))
		if cfg.shouldDryRun {
			logf("[dry-run] sync %s -> %s", record.URL, filePath)
			continue
		}

		if err := os.MkdirAll(filepath.Dir(filePath), 0o755); err != nil {
			return fmt.Errorf("create sync dir: %w", err)
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
			logf("sync downloading %s", filepath.Base(filePath))
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
		logf("[dry-run] sync done (files=%d)", len(manifestExisting.Files))
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

	logf("sync done (files=%d)", len(recordsComplete))
	logf("manifest written: %s", fileManifest)
	return nil
}

func runSyncArchive(cfg *syncConfig, dirVersion string, manifestExisting manifestFile, asset string) error {
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
			logf("sync done (files=%d)", len(recordsComplete))
			logf("manifest written: %s", fileManifest)
			return nil
		}
	}

	if cfg.shouldDryRun {
		logf("[dry-run] sync archive %s -> %s", manifestExisting.QueryURL, dirVersion)
		logf("[dry-run] sync done (files=%d)", len(manifestExisting.Files))
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

	logf("sync done (files=%d)", len(recordsComplete))
	logf("manifest written: %s", fileManifest)
	return nil
}

func scanOmniPathRecords(dirVersion string, asset string, manifestExisting manifestFile, workersMax int) ([]recordFile, error) {
	type taskRecordFile struct {
		filePath string
		pathRel  string
		urlFile  string
		asset    string
	}

	dirRaw := filepath.Join(dirVersion, "raw")
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
				filePath := filepath.Join(dirTaxID, fileName)
				pathRel := filepath.ToSlash(filepath.Join("raw", taxID, fileName))
				urlFile := urlsExisting[pathRel]
				if urlFile == "" {
					if strings.HasPrefix(urlArchive, archiveURL) {
						urlFile = urlArchive
					} else {
						urlFile = deriveOmniPathDataURL(asset, taxID)
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

func deriveOmniPathDataURL(asset string, taxID string) string {
	params := urlForTaxID(asset, taxID)
	if asset == "interactions" {
		return baseURL + "/interactions?" + params
	}
	return baseURL + "/enzsub?" + params
}

func urlForTaxID(asset string, taxID string) string {
	if asset == "interactions" {
		return "datasets=kinaseextra&format=tsv&organisms=" + taxID
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
