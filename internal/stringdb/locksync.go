package stringdb

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type lockConfig struct {
	dirOut       string
	versionToken string
	shouldDryRun bool
}

type syncConfig struct {
	dirOut                  string
	versionToken            string
	shouldOverwriteExisting bool
	retryMax                int
	retryWait               time.Duration
	shouldAllowInsecureTLS  bool
	shouldDryRun            bool
}

func runLock(cfg *lockConfig) error {
	dirVersion := filepath.Join(cfg.dirOut, cfg.versionToken)
	fileManifest := filepath.Join(dirVersion, "manifest.lock")

	records, err := scanVersionFileRecords(dirVersion)
	if err != nil {
		return err
	}

	if cfg.shouldDryRun {
		logf("dry-run lock version dir: %s", dirVersion)
		logf("dry-run manifest: %s", fileManifest)
		logf("dry-run files=%d", len(records))
		return nil
	}

	if err := writeManifest(fileManifest, cfg.versionToken, records, time.Now()); err != nil {
		return err
	}

	logf("lock updated: %s", fileManifest)
	return nil
}

func runSync(cfg *syncConfig) error {
	dirVersion := filepath.Join(cfg.dirOut, cfg.versionToken)
	fileManifest := filepath.Join(dirVersion, "manifest.lock")

	recordsManifest, err := readExistingFileRecords(fileManifest)
	if err != nil {
		return err
	}
	if len(recordsManifest) == 0 {
		return fmt.Errorf("manifest is empty or missing: %s", fileManifest)
	}

	clientHTTP := createHTTPClient(cfg.shouldAllowInsecureTLS)
	recordsCurrent := make([]fileRecord, 0, len(recordsManifest))

	for _, record := range recordsManifest {
		filePath := filepath.Join(dirVersion, filepath.FromSlash(record.pathRel))
		if cfg.shouldDryRun {
			logf("[dry-run] sync %s -> %s", record.url, filePath)
			continue
		}

		if err := os.MkdirAll(filepath.Dir(filePath), 0o755); err != nil {
			return fmt.Errorf("create sync dir: %w", err)
		}

		shouldDownload := cfg.shouldOverwriteExisting
		if !shouldDownload {
			recordCurrent, ok, err := inspectExistingFile(
				filePath,
				record.speciesID,
				record.assetName,
				record.pathRel,
				record.url,
			)
			if err != nil {
				return err
			}
			if ok && recordCurrent.sha256 == record.sha256 {
				recordsCurrent = append(recordsCurrent, recordCurrent)
				continue
			}
			shouldDownload = true
		}

		if shouldDownload {
			logf("sync downloading %s", filepath.Base(filePath))
			if err := downloadFileWithRetry(clientHTTP, record.url, filePath, cfg.retryMax, cfg.retryWait); err != nil {
				return err
			}
		}

		recordCurrent, err := buildFileRecord(filePath, record.speciesID, record.assetName, record.pathRel, record.url)
		if err != nil {
			return err
		}
		recordsCurrent = append(recordsCurrent, recordCurrent)
	}

	if cfg.shouldDryRun {
		logf("dry-run sync done (files=%d)", len(recordsManifest))
		return nil
	}

	recordsComplete, err := buildCompleteFileRecords(fileManifest, dirVersion, recordsCurrent)
	if err != nil {
		return err
	}
	if err := writeManifest(fileManifest, cfg.versionToken, recordsComplete, time.Now()); err != nil {
		return err
	}

	logf("sync done (files=%d)", len(recordsComplete))
	logf("manifest written: %s", fileManifest)
	return nil
}

func scanVersionFileRecords(dirVersion string) ([]fileRecord, error) {
	dirRaw := filepath.Join(dirVersion, "raw")
	entriesSpecies, err := os.ReadDir(dirRaw)
	if err != nil {
		return nil, fmt.Errorf("read raw dir: %w", err)
	}

	records := make([]fileRecord, 0)
	for _, entrySpecies := range entriesSpecies {
		if !entrySpecies.IsDir() {
			continue
		}

		speciesID := entrySpecies.Name()
		dirSpecies := filepath.Join(dirRaw, speciesID)
		entriesFiles, err := os.ReadDir(dirSpecies)
		if err != nil {
			return nil, fmt.Errorf("read species dir %s: %w", dirSpecies, err)
		}

		for _, entryFile := range entriesFiles {
			if entryFile.IsDir() {
				continue
			}
			fileName := entryFile.Name()
			assetName, err := parseAssetNameFromFileName(fileName, speciesID)
			if err != nil {
				return nil, err
			}

			filePath := filepath.Join(dirSpecies, fileName)
			pathRel := filepath.ToSlash(filepath.Join("raw", speciesID, fileName))
			urlFile := fmt.Sprintf("%s/%s.%s/%s", baseURL, assetName, filepath.Base(dirVersion), fileName)
			record, err := buildFileRecord(filePath, speciesID, assetName, pathRel, urlFile)
			if err != nil {
				return nil, err
			}
			records = append(records, record)
		}
	}

	return records, nil
}

func parseAssetNameFromFileName(fileName string, speciesID string) (string, error) {
	prefix := speciesID + "."
	suffix := ".txt.gz"
	if !strings.HasPrefix(fileName, prefix) || !strings.HasSuffix(fileName, suffix) {
		return "", fmt.Errorf("unexpected STRING asset filename: %s", fileName)
	}

	body := strings.TrimSuffix(strings.TrimPrefix(fileName, prefix), suffix)
	indexLastDot := strings.LastIndex(body, ".")
	if indexLastDot <= 0 {
		return "", fmt.Errorf("cannot parse STRING asset filename: %s", fileName)
	}

	assetName := body[:indexLastDot]
	if assetName == "" {
		return "", fmt.Errorf("cannot parse STRING asset filename: %s", fileName)
	}
	return assetName, nil
}
