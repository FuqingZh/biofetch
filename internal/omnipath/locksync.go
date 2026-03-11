package omnipath

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	toml "github.com/pelletier/go-toml/v2"
)

type lockConfig struct {
	dirOut       string
	versionToken string
	dataset      string
	shouldDryRun bool
}

type syncConfig struct {
	dirOut                  string
	versionToken            string
	dataset                 string
	shouldOverwriteExisting bool
	retryMax                int
	retryWait               time.Duration
	shouldAllowInsecureTLS  bool
	shouldDryRun            bool
}

func runLockEnzSub(cfg *lockConfig) error {
	dirVersion := filepath.Join(cfg.dirOut, "enz_sub", cfg.versionToken)
	return runLockCommon(cfg, dirVersion, "enz_sub")
}

func runLockInteractions(cfg *lockConfig) error {
	dirVersion := filepath.Join(cfg.dirOut, "interactions", cfg.dataset, cfg.versionToken)
	return runLockCommon(cfg, dirVersion, "interactions")
}

func runLockCommon(cfg *lockConfig, dirVersion string, asset string) error {
	fileManifest := filepath.Join(dirVersion, "manifest.lock")
	records, err := scanOmniPathRecords(dirVersion, asset)
	if err != nil {
		return err
	}

	manifestExisting, _ := readExistingManifest(fileManifest)
	manifest := manifestFile{
		Database:     "omnipath",
		Asset:        asset,
		Dataset:      firstNonEmpty(manifestExisting.Dataset, cfg.dataset),
		Version:      firstNonEmpty(manifestExisting.Version, cfg.versionToken),
		VersionToken: firstNonEmpty(manifestExisting.VersionToken, cfg.versionToken),
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
	manifestExisting, err := readExistingManifest(fileManifest)
	if err != nil {
		return err
	}
	if len(manifestExisting.Files) == 0 {
		return fmt.Errorf("manifest is empty or missing: %s", fileManifest)
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

func scanOmniPathRecords(dirVersion string, asset string) ([]recordFile, error) {
	dirRaw := filepath.Join(dirVersion, "raw")
	entries, err := os.ReadDir(dirRaw)
	if err != nil {
		return nil, fmt.Errorf("read raw dir: %w", err)
	}

	records := make([]recordFile, 0)
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
				urlFile := deriveOmniPathDataURL(asset, taxID)
				record, err := buildRecord(filePath, pathRel, urlFile, asset)
				if err != nil {
					return nil, err
				}
				records = append(records, record)
			}
			continue
		}

		if entry.Name() != "query_meta.json" {
			return nil, fmt.Errorf("unexpected OmniPath raw file: %s", entry.Name())
		}
		filePath := filepath.Join(dirRaw, entry.Name())
		pathRel := filepath.ToSlash(filepath.Join("raw", entry.Name()))
		urlQuery := queryEnzSubURL
		if asset == "interactions" {
			urlQuery = queryInteractionsURL
		}
		record, err := buildRecord(filePath, pathRel, urlQuery, "query_meta")
		if err != nil {
			return nil, err
		}
		records = append(records, record)
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
	data, err := os.ReadFile(fileManifest)
	if err != nil {
		if os.IsNotExist(err) {
			return manifestFile{}, nil
		}
		return manifestFile{}, fmt.Errorf("read manifest: %w", err)
	}

	var manifest manifestFile
	if err := toml.Unmarshal(data, &manifest); err != nil {
		return manifestFile{}, fmt.Errorf("decode manifest: %w", err)
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
