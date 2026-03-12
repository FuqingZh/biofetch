package geneontology

import (
	"biofetch/internal/shared/cliopt"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"
)

type ontologyLockConfig struct {
	cliopt.DirOutConfig
	cliopt.VersionConfig
	cliopt.DryRunConfig
}

type ontologySyncConfig struct {
	cliopt.DirOutConfig
	cliopt.VersionConfig
	cliopt.ExistingRuleConfig
	cliopt.RetryConfig
	cliopt.InsecureTLSConfig
	cliopt.DryRunConfig
}

func runLockOntology(cfg *ontologyLockConfig) error {
	dirVersion := filepath.Join(cfg.DirOut, "ontology", cfg.VersionToken)
	fileManifest := filepath.Join(dirVersion, "manifest.lock")

	records, err := scanOntologyRecords(dirVersion)
	if err != nil {
		return err
	}

	if cfg.ShouldDryRun {
		logf("dry-run lock version dir: %s", dirVersion)
		logf("dry-run manifest: %s", fileManifest)
		logf("dry-run files=%d", len(records))
		return nil
	}

	cfgManifest := ontologyConfig{
		version:       cfg.VersionToken,
		VersionConfig: cliopt.VersionConfig{VersionToken: cfg.VersionToken},
	}
	if err := writeManifest(fileManifest, &cfgManifest, records, time.Now()); err != nil {
		return err
	}

	logf("lock updated: %s", fileManifest)
	return nil
}

func runSyncOntology(cfg *ontologySyncConfig) error {
	dirVersion := filepath.Join(cfg.DirOut, "ontology", cfg.VersionToken)
	fileManifest := filepath.Join(dirVersion, "manifest.lock")

	recordsManifest, err := readExistingOntologyRecords(fileManifest)
	if err != nil {
		return err
	}
	if len(recordsManifest) == 0 {
		return fmt.Errorf("manifest is empty or missing: %s", fileManifest)
	}

	clientHTTP := createHTTPClient(cfg.ShouldAllowInsecureTLS)
	recordsCurrent := make([]ontologyRecord, 0, len(recordsManifest))
	for _, record := range recordsManifest {
		filePath := filepath.Join(dirVersion, filepath.FromSlash(record.PathRel))
		if cfg.ShouldDryRun {
			logf("[dry-run] sync %s -> %s", record.URL, filePath)
			continue
		}

		if err := os.MkdirAll(filepath.Dir(filePath), 0o755); err != nil {
			return fmt.Errorf("create sync dir: %w", err)
		}

		shouldDownload := cfg.ShouldOverwriteExisting
		if !shouldDownload {
			recordCurrent, ok, err := inspectExistingAsset(filePath, record.PathRel, ontologyAsset{name: record.Asset, url: record.URL})
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
			if err := downloadFileWithRetry(clientHTTP, record.URL, filePath, cfg.RetryMax, cfg.RetryWait); err != nil {
				return err
			}
		}

		recordCurrent, err := buildOntologyRecord(filePath, record.PathRel, ontologyAsset{name: record.Asset, url: record.URL})
		if err != nil {
			return err
		}
		recordsCurrent = append(recordsCurrent, recordCurrent)
	}

	if cfg.ShouldDryRun {
		logf("dry-run sync done (files=%d)", len(recordsManifest))
		return nil
	}

	recordsComplete, err := buildCompleteOntologyRecords(fileManifest, dirVersion, recordsCurrent)
	if err != nil {
		return err
	}
	cfgManifest := ontologyConfig{
		version:       cfg.VersionToken,
		VersionConfig: cliopt.VersionConfig{VersionToken: cfg.VersionToken},
	}
	if err := writeManifest(fileManifest, &cfgManifest, recordsComplete, time.Now()); err != nil {
		return err
	}

	logf("sync done (files=%d)", len(recordsComplete))
	logf("manifest written: %s", fileManifest)
	return nil
}

func scanOntologyRecords(dirVersion string) ([]ontologyRecord, error) {
	dirRaw := filepath.Join(dirVersion, "raw")
	entries, err := os.ReadDir(dirRaw)
	if err != nil {
		return nil, fmt.Errorf("read raw dir: %w", err)
	}

	records := make([]ontologyRecord, 0)
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		fileName := entry.Name()
		filePath := filepath.Join(dirRaw, fileName)
		pathRel := filepath.ToSlash(filepath.Join("raw", fileName))
		record, err := buildOntologyRecord(filePath, pathRel, ontologyAsset{name: fileName, url: ontologyBaseURL + fileName})
		if err != nil {
			return nil, err
		}
		records = append(records, record)
	}

	sort.Slice(records, func(i, j int) bool {
		return records[i].Asset < records[j].Asset
	})
	return records, nil
}
