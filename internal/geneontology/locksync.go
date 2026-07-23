package geneontology

import (
	"biofetch/internal/shared/cliopt"
	"biofetch/internal/shared/httpx"
	"biofetch/internal/shared/logx"
	"biofetch/internal/shared/parallel"
	"biofetch/internal/shared/tomlx"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"
)

type ontologyLockConfig struct {
	cliopt.DirSnapshotConfig
	cliopt.DryRunConfig
	cliopt.LogConfig
	workersMax int
}

type ontologyRestoreConfig struct {
	cliopt.DirOutConfig
	cliopt.VersionConfig
	cliopt.ExistingRuleConfig
	cliopt.RetryConfig
	cliopt.DownloadControlConfig
	cliopt.InsecureTLSConfig
	cliopt.DryRunConfig
	cliopt.LogConfig
}

func runLockOntology(cfg *ontologyLockConfig) error {
	if err := cliopt.NormalizeLockWorkersMax(&cfg.workersMax); err != nil {
		return err
	}
	versionToken, err := cliopt.SnapshotVersionToken(cfg.DirSnapshot)
	if err != nil {
		return err
	}
	if err := validateOptionalOntologyVersionToken(versionToken); err != nil {
		return err
	}
	dirVersion := cfg.DirSnapshot
	fileManifest := filepath.Join(dirVersion, "manifest.lock")
	_, closeRun, err := logx.StartVersionedRun("biofetch go", "lock", cfg.DirLogs, dirVersion)
	if err != nil {
		return err
	}
	defer closeRun()
	urlsExisting, err := buildOntologyExistingURLMap(fileManifest)
	if err != nil {
		return err
	}

	records, err := scanOntologyRecords(dirVersion, versionToken, urlsExisting, cfg.workersMax)
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
		version:       versionToken,
		VersionConfig: cliopt.VersionConfig{VersionToken: versionToken},
	}
	if err := tomlx.WriteFileAtomic(fileManifest, buildOntologyManifestFile(&cfgManifest, records, time.Now())); err != nil {
		return err
	}

	logf("lock updated: %s", fileManifest)
	return nil
}

func runRestoreOntology(cfg *ontologyRestoreConfig) error {
	dirVersion := filepath.Join(cfg.DirOut, "ontology", cfg.VersionToken)
	fileManifest := filepath.Join(dirVersion, "manifest.lock")
	_, closeRun, err := logx.StartVersionedRun("biofetch go", "restore", cfg.DirLogs, dirVersion)
	if err != nil {
		return err
	}
	defer closeRun()

	recordsManifest, err := readExistingOntologyRecords(fileManifest)
	if err != nil {
		return err
	}
	if len(recordsManifest) == 0 {
		return fmt.Errorf("manifest is empty or missing: %s", fileManifest)
	}

	filesCurrentByPath := map[string]ontologyFileState{}
	if !cfg.ShouldOverwriteExisting {
		dirRaw := filepath.Join(dirVersion, "raw")
		filesCurrentByPath, err = scanOntologyRawFileStateIndex(dirRaw)
		if err != nil {
			return err
		}
	}

	if cfg.ShouldDryRun {
		for _, record := range recordsManifest {
			filePath := filepath.Join(dirVersion, filepath.FromSlash(record.PathRel))
			logf("[dry-run] sync %s -> %s", record.URL, filePath)
		}
		logf("dry-run restore done (files=%d)", len(recordsManifest))
		return nil
	}

	recordsReused, tasksDownload := planSyncOntologyTasks(
		dirVersion,
		recordsManifest,
		cfg.ShouldOverwriteExisting,
		filesCurrentByPath,
	)

	clientHTTP := httpx.NewClient(cfg.ShouldAllowInsecureTLS)
	limiterRequest := httpx.NewRequestLimiter(cfg.RequestInterval)
	recordsDownloaded, err := runOntologyDownloadTasks(
		clientHTTP,
		tasksDownload,
		cfg.RetryMax,
		cfg.RetryWait,
		cfg.WorkersMax,
		limiterRequest,
	)
	if err != nil {
		return err
	}

	recordsCurrent := make([]ontologyRecord, 0, len(recordsReused)+len(recordsDownloaded))
	recordsCurrent = append(recordsCurrent, recordsReused...)
	recordsCurrent = append(recordsCurrent, recordsDownloaded...)

	recordsComplete, err := buildCompleteOntologyRecords(fileManifest, dirVersion, recordsCurrent)
	if err != nil {
		return err
	}
	cfgManifest := ontologyConfig{
		version:       cfg.VersionToken,
		VersionConfig: cliopt.VersionConfig{VersionToken: cfg.VersionToken},
	}
	if err := tomlx.WriteFileAtomic(fileManifest, buildOntologyManifestFile(&cfgManifest, recordsComplete, time.Now())); err != nil {
		return err
	}

	logf("restore done (files=%d)", len(recordsComplete))
	logf("manifest written: %s", fileManifest)
	return nil
}

func scanOntologyRecords(
	dirVersion string,
	versionToken string,
	urlsExisting map[string]string,
	workersMax int,
) ([]ontologyRecord, error) {
	type taskOntologyRecord struct {
		filePath string
		pathRel  string
		asset    ontologyAsset
	}

	dirRaw := filepath.Join(dirVersion, "raw")
	entries, err := os.ReadDir(dirRaw)
	if err != nil {
		return nil, fmt.Errorf("read raw dir: %w", err)
	}

	tasks := make([]taskOntologyRecord, 0)
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		fileName := entry.Name()
		filePath := filepath.Join(dirRaw, fileName)
		pathRel := filepath.ToSlash(filepath.Join("raw", fileName))
		urlAsset := urlsExisting[pathRel]
		if urlAsset == "" {
			urlAsset = buildOntologyAssetURL(buildOntologyBaseURLForVersionToken(versionToken), fileName)
		}
		tasks = append(tasks, taskOntologyRecord{
			filePath: filePath,
			pathRel:  pathRel,
			asset:    ontologyAsset{name: fileName, url: urlAsset},
		})
	}

	records, err := parallel.MapOrderedWithWorkers(tasks, workersMax, func(task taskOntologyRecord) (ontologyRecord, error) {
		return buildOntologyRecord(task.filePath, task.pathRel, task.asset)
	})
	if err != nil {
		return nil, err
	}

	sort.Slice(records, func(i, j int) bool {
		return records[i].Asset < records[j].Asset
	})
	return records, nil
}

func buildOntologyExistingURLMap(fileManifest string) (map[string]string, error) {
	recordsExisting, err := readExistingOntologyRecords(fileManifest)
	if err != nil {
		return nil, err
	}

	urls := make(map[string]string, len(recordsExisting))
	for _, record := range recordsExisting {
		if record.PathRel == "" || record.URL == "" {
			continue
		}
		urls[record.PathRel] = record.URL
	}
	return urls, nil
}
