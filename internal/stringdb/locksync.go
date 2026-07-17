package stringdb

import (
	"biofetch/internal/shared/cliopt"
	"biofetch/internal/shared/httpx"
	"biofetch/internal/shared/logx"
	"biofetch/internal/shared/parallel"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type lockConfig struct {
	dirSnapshot  string
	dirLogs      string
	shouldDryRun bool
}

type syncConfig struct {
	dirOut                  string
	dirLogs                 string
	versionToken            string
	ruleExisting            string
	shouldOverwriteExisting bool
	retryMax                int
	retryWait               time.Duration
	cliopt.DownloadControlConfig
	shouldAllowInsecureTLS bool
	shouldDryRun           bool
}

func runLock(cfg *lockConfig) error {
	versionToken, err := cliopt.SnapshotVersionToken(cfg.dirSnapshot)
	if err != nil {
		return err
	}
	dirVersion := cfg.dirSnapshot
	fileManifest := filepath.Join(dirVersion, "manifest.lock")
	_, closeRun, err := logx.StartVersionedRun("biofetch string", "lock", cfg.dirLogs, dirVersion)
	if err != nil {
		return err
	}
	defer closeRun()

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

	if err := writeManifest(fileManifest, versionToken, records, time.Now()); err != nil {
		return err
	}

	logf("lock updated: %s", fileManifest)
	return nil
}

func runSync(cfg *syncConfig) error {
	dirVersion := filepath.Join(cfg.dirOut, cfg.versionToken)
	fileManifest := filepath.Join(dirVersion, "manifest.lock")
	_, closeRun, err := logx.StartVersionedRun("biofetch string", "sync", cfg.dirLogs, dirVersion)
	if err != nil {
		return err
	}
	defer closeRun()

	recordsManifest, err := readExistingFileRecords(fileManifest)
	if err != nil {
		return err
	}
	if len(recordsManifest) == 0 {
		return fmt.Errorf("manifest is empty or missing: %s", fileManifest)
	}

	if cfg.shouldDryRun {
		for _, record := range recordsManifest {
			filePath := filepath.Join(dirVersion, filepath.FromSlash(record.pathRel))
			logf("[dry-run] sync %s -> %s", record.url, filePath)
		}
		logf("dry-run sync done (files=%d)", len(recordsManifest))
		return nil
	}

	recordsReused, tasksDownload, err := planSyncDownloadTasks(dirVersion, recordsManifest, cfg.shouldOverwriteExisting)
	if err != nil {
		return err
	}

	clientHTTP := createHTTPClient(cfg.shouldAllowInsecureTLS)
	limiterRequest := httpx.NewRequestLimiter(cfg.RequestInterval)
	recordsDownloaded, err := runDownloadTasks(
		clientHTTP,
		tasksDownload,
		cfg.retryMax,
		cfg.retryWait,
		cfg.WorkersMax,
		limiterRequest,
	)
	if err != nil {
		return err
	}

	recordsCurrent := make([]fileRecord, 0, len(recordsReused)+len(recordsDownloaded))
	recordsCurrent = append(recordsCurrent, recordsReused...)
	recordsCurrent = append(recordsCurrent, recordsDownloaded...)

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
	type taskFileRecord struct {
		filePath  string
		speciesID string
		assetName string
		pathRel   string
		urlFile   string
	}

	dirRaw := filepath.Join(dirVersion, "raw")
	entriesSpecies, err := os.ReadDir(dirRaw)
	if err != nil {
		return nil, fmt.Errorf("read raw dir: %w", err)
	}

	tasks := make([]taskFileRecord, 0)
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
			tasks = append(tasks, taskFileRecord{
				filePath:  filePath,
				speciesID: speciesID,
				assetName: assetName,
				pathRel:   pathRel,
				urlFile:   urlFile,
			})
		}
	}

	return parallel.MapOrdered(tasks, func(task taskFileRecord) (fileRecord, error) {
		return buildFileRecord(task.filePath, task.speciesID, task.assetName, task.pathRel, task.urlFile)
	})
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
