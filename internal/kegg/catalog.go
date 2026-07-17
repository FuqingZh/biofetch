package kegg

import (
	"biofetch/internal/shared/cliopt"
	"biofetch/internal/shared/logx"
	"biofetch/internal/shared/parallel"
	"biofetch/internal/shared/tomlx"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

const (
	keggCatalogAsset    = "organism"
	keggCatalogFileName = "organism.list.tsv"
)

type catalogConfig struct {
	dirOut                 string
	dirLogs                string
	version                string
	versionToken           string
	sourceRelease          string
	sourceReleaseStart     string
	sourceReleaseEnd       string
	sourceLastUpdate       string
	sourceLastUpdateStart  string
	sourceLastUpdateEnd    string
	shouldAllowInsecureTLS bool
	shouldDryRun           bool
}

type catalogLockConfig struct {
	dirSnapshot  string
	dirLogs      string
	shouldDryRun bool
}

type catalogSyncConfig struct {
	dirOut                 string
	dirLogs                string
	versionToken           string
	shouldAllowInsecureTLS bool
	shouldDryRun           bool
}

type catalogRecord struct {
	Asset   string
	PathRel string
	SHA256  string
	Bytes   int64
	URL     string
}

type catalogManifestFile struct {
	Database              string                    `toml:"database"`
	Asset                 string                    `toml:"asset"`
	Catalog               string                    `toml:"catalog"`
	Version               string                    `toml:"version"`
	VersionToken          string                    `toml:"version_token"`
	SourceRelease         string                    `toml:"source_release"`
	SourceReleaseStart    string                    `toml:"source_release_start,omitempty"`
	SourceReleaseEnd      string                    `toml:"source_release_end,omitempty"`
	SourceLastUpdate      string                    `toml:"source_last_update,omitempty"`
	SourceLastUpdateStart string                    `toml:"source_last_update_start,omitempty"`
	SourceLastUpdateEnd   string                    `toml:"source_last_update_end,omitempty"`
	DownloadedAt          string                    `toml:"downloaded_at"`
	Files                 []catalogManifestFileItem `toml:"files"`
}

type catalogManifestFileItem struct {
	Asset  string `toml:"asset"`
	Path   string `toml:"path"`
	SHA256 string `toml:"sha256"`
	Bytes  int64  `toml:"bytes"`
	URL    string `toml:"url"`
}

func createCatalogFetchCommand() *cobra.Command {
	cfg := catalogConfig{}

	commandFetch := &cobra.Command{
		Use:           "fetch",
		Short:         "Fetch KEGG organism catalog and update manifest.lock",
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := validateCatalogFetchConfig(&cfg); err != nil {
				return err
			}
			return runFetchCatalog(&cfg)
		},
	}

	flags := commandFetch.Flags()
	flags.SortFlags = false
	flags.StringVar(&cfg.dirOut, "dir_out", cfg.dirOut, "KEGG asset root directory")
	flags.StringVar(&cfg.versionToken, "version", cfg.versionToken, "KEGG local snapshot key (YYYY-MM), e.g. 2026-04")
	flags.BoolVar(
		&cfg.shouldAllowInsecureTLS,
		"should_allow_insecure_tls",
		false,
		"Disable TLS certificate verification",
	)
	flags.BoolVar(&cfg.shouldDryRun, "should_dry_run", false, "Print actions only; do not download")
	flags.StringVar(&cfg.dirLogs, "dir_logs", cfg.dirLogs, "Directory for run log files; default is <version>/logs/")
	return commandFetch
}

func createCatalogLockCommand() *cobra.Command {
	cfg := catalogLockConfig{}

	commandLock := &cobra.Command{
		Use:           "lock",
		Short:         "Rebuild KEGG catalog manifest.lock from the current version directory",
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := validateCatalogLockConfig(&cfg); err != nil {
				return err
			}
			return runLockCatalog(&cfg)
		},
	}

	flags := commandLock.Flags()
	flags.SortFlags = false
	flags.StringVar(&cfg.dirSnapshot, "dir_snapshot", "", "Existing snapshot directory containing raw/ and manifest.lock")
	flags.BoolVar(&cfg.shouldDryRun, "should_dry_run", false, "Print actions only; do not write manifest")
	flags.StringVar(&cfg.dirLogs, "dir_logs", cfg.dirLogs, "Directory for run log files; default is <version>/logs/")
	return commandLock
}

func createCatalogSyncCommand() *cobra.Command {
	cfg := catalogSyncConfig{}

	commandSync := &cobra.Command{
		Use:           "sync",
		Short:         "Sync KEGG catalog files from manifest.lock and refresh manifest",
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := validateCatalogSyncConfig(&cfg); err != nil {
				return err
			}
			return runSyncCatalog(&cfg)
		},
	}

	flags := commandSync.Flags()
	flags.SortFlags = false
	flags.StringVar(&cfg.dirOut, "dir_out", "", "KEGG asset root directory")
	flags.StringVar(&cfg.versionToken, "version", "", "KEGG local snapshot key (YYYY-MM)")
	flags.BoolVar(
		&cfg.shouldAllowInsecureTLS,
		"should_allow_insecure_tls",
		false,
		"Disable TLS certificate verification",
	)
	flags.BoolVar(&cfg.shouldDryRun, "should_dry_run", false, "Print actions only; do not download")
	flags.StringVar(&cfg.dirLogs, "dir_logs", cfg.dirLogs, "Directory for run log files; default is <version>/logs/")
	return commandSync
}

func validateCatalogFetchConfig(cfg *catalogConfig) error {
	if strings.TrimSpace(cfg.dirOut) == "" {
		return fmt.Errorf("dir_out is required")
	}
	if strings.TrimSpace(cfg.versionToken) != "" && !isValidKEGGSnapshotVersionToken(cfg.versionToken) {
		return fmt.Errorf("version must be a local snapshot key like 2026-04")
	}
	return nil
}

func validateCatalogLockConfig(cfg *catalogLockConfig) error {
	versionToken, err := cliopt.SnapshotVersionToken(cfg.dirSnapshot)
	if err != nil {
		return err
	}
	if !isValidKEGGSnapshotVersionToken(versionToken) {
		return fmt.Errorf("version must be a local snapshot key like 2026-04")
	}
	return nil
}

func validateCatalogSyncConfig(cfg *catalogSyncConfig) error {
	if strings.TrimSpace(cfg.dirOut) == "" {
		return fmt.Errorf("dir_out is required")
	}
	if strings.TrimSpace(cfg.versionToken) == "" {
		return fmt.Errorf("version is required")
	}
	if !isValidKEGGSnapshotVersionToken(cfg.versionToken) {
		return fmt.Errorf("version must be a local snapshot key like 2026-04")
	}
	return nil
}

func runFetchCatalog(cfg *catalogConfig) error {
	timeStarted := time.Now()
	clientHTTP := createHTTPClient(cfg.shouldAllowInsecureTLS)
	clientKegg := createKEGGClient(clientHTTP, 350*time.Millisecond, defaultKEGGRetryMax, defaultKEGGRetryWait)

	if strings.TrimSpace(cfg.versionToken) == "" {
		cfg.versionToken = deriveKEGGSnapshotVersionToken(timeStarted)
	}
	cfg.version = cfg.versionToken
	metadataStart, err := resolveKEGGInfoMetadata(clientKegg, "kegg")
	if err != nil {
		logf("warning: KEGG catalog info metadata unavailable: %v", err)
	} else {
		cfg.applyKEGGInfoMetadataStart(metadataStart)
	}
	dirVersion := filepath.Join(cfg.dirOut, "catalog", cfg.versionToken)
	_, closeRun, err := logx.StartVersionedRun("biofetch kegg", "fetch", cfg.dirLogs, dirVersion)
	if err != nil {
		return err
	}
	defer closeRun()

	dirRaw := filepath.Join(dirVersion, "raw")
	fileManifest := filepath.Join(dirVersion, "manifest.lock")
	fileOut := filepath.Join(dirRaw, keggCatalogFileName)
	pathRel := filepath.ToSlash(filepath.Join("raw", keggCatalogFileName))
	urlCatalog := deriveKEGGCatalogURL()

	if cfg.shouldDryRun {
		logf("[dry-run] version dir: %s", dirVersion)
		logf("[dry-run] manifest: %s", fileManifest)
		logf("[dry-run] %s -> %s", urlCatalog, fileOut)
		return nil
	}

	if err := os.MkdirAll(dirRaw, 0o755); err != nil {
		return fmt.Errorf("create raw dir: %w", err)
	}

	record, ok, err := inspectCatalogFile(fileOut, pathRel, keggCatalogAsset, urlCatalog)
	if err != nil {
		return err
	}
	if !ok {
		logf("downloading %s", keggCatalogFileName)
		if err := clientKegg.downloadFile(urlCatalog, fileOut); err != nil {
			return err
		}
		record, err = buildCatalogRecord(fileOut, pathRel, keggCatalogAsset, urlCatalog)
		if err != nil {
			return err
		}
	} else {
		logf("using existing %s", keggCatalogFileName)
	}

	recordsComplete, err := buildCompleteCatalogRecords(fileManifest, dirVersion, []catalogRecord{record})
	if err != nil {
		return err
	}
	metadataEnd, err := resolveKEGGInfoMetadata(clientKegg, "kegg")
	if err != nil {
		logf("warning: KEGG catalog info metadata unavailable after download: %v", err)
	} else {
		cfg.applyKEGGInfoMetadataEnd(metadataEnd)
	}
	if err := writeCatalogManifest(fileManifest, cfg, recordsComplete, time.Now()); err != nil {
		return err
	}

	logf("done (files=%d)", len(recordsComplete))
	logf("manifest written: %s", fileManifest)
	return nil
}

func runLockCatalog(cfg *catalogLockConfig) error {
	versionToken, err := cliopt.SnapshotVersionToken(cfg.dirSnapshot)
	if err != nil {
		return err
	}
	dirVersion := cfg.dirSnapshot
	fileManifest := filepath.Join(dirVersion, "manifest.lock")
	_, closeRun, err := logx.StartVersionedRun("biofetch kegg", "lock", cfg.dirLogs, dirVersion)
	if err != nil {
		return err
	}
	defer closeRun()

	records, err := scanCatalogRecords(dirVersion)
	if err != nil {
		return err
	}

	manifestExisting, _ := readExistingCatalogManifest(fileManifest)
	cfgManifest := catalogConfig{
		version:               versionToken,
		versionToken:          versionToken,
		sourceRelease:         manifestExisting.SourceRelease,
		sourceReleaseStart:    manifestExisting.SourceReleaseStart,
		sourceReleaseEnd:      manifestExisting.SourceReleaseEnd,
		sourceLastUpdate:      manifestExisting.SourceLastUpdate,
		sourceLastUpdateStart: manifestExisting.SourceLastUpdateStart,
		sourceLastUpdateEnd:   manifestExisting.SourceLastUpdateEnd,
	}

	if cfg.shouldDryRun {
		logf("[dry-run] lock version dir: %s", dirVersion)
		logf("[dry-run] manifest: %s", fileManifest)
		logf("[dry-run] files=%d", len(records))
		return nil
	}

	if err := writeCatalogManifest(fileManifest, &cfgManifest, records, time.Now()); err != nil {
		return err
	}
	logf("lock updated: %s", fileManifest)
	return nil
}

func runSyncCatalog(cfg *catalogSyncConfig) error {
	dirVersion := filepath.Join(cfg.dirOut, "catalog", cfg.versionToken)
	fileManifest := filepath.Join(dirVersion, "manifest.lock")
	_, closeRun, err := logx.StartVersionedRun("biofetch kegg", "sync", cfg.dirLogs, dirVersion)
	if err != nil {
		return err
	}
	defer closeRun()

	manifestExisting, err := readExistingCatalogManifest(fileManifest)
	if err != nil {
		return err
	}
	if len(manifestExisting.Files) == 0 {
		return fmt.Errorf("manifest is empty or missing: %s", fileManifest)
	}

	if cfg.shouldDryRun {
		for _, item := range manifestExisting.Files {
			filePath := filepath.Join(dirVersion, filepath.FromSlash(item.Path))
			logf("[dry-run] sync %s -> %s", item.URL, filePath)
		}
		logf("[dry-run] sync done (files=%d)", len(manifestExisting.Files))
		return nil
	}

	clientHTTP := createHTTPClient(cfg.shouldAllowInsecureTLS)
	clientKegg := createKEGGClient(clientHTTP, 350*time.Millisecond, defaultKEGGRetryMax, defaultKEGGRetryWait)
	recordsCurrent := make([]catalogRecord, 0, len(manifestExisting.Files))
	for _, item := range manifestExisting.Files {
		record := catalogRecord{
			Asset:   item.Asset,
			PathRel: item.Path,
			SHA256:  item.SHA256,
			Bytes:   item.Bytes,
			URL:     item.URL,
		}
		filePath := filepath.Join(dirVersion, filepath.FromSlash(record.PathRel))
		if err := os.MkdirAll(filepath.Dir(filePath), 0o755); err != nil {
			return fmt.Errorf("create sync dir: %w", err)
		}

		recordCurrent, ok, err := inspectCatalogFile(filePath, record.PathRel, record.Asset, record.URL)
		if err != nil {
			return err
		}
		if !ok || recordCurrent.SHA256 != record.SHA256 {
			logf("sync downloading %s", filepath.Base(filePath))
			if err := clientKegg.downloadFile(record.URL, filePath); err != nil {
				return err
			}
			recordCurrent, err = buildCatalogRecord(filePath, record.PathRel, record.Asset, record.URL)
			if err != nil {
				return err
			}
		}
		recordsCurrent = append(recordsCurrent, recordCurrent)
	}

	recordsComplete, err := buildCompleteCatalogRecords(fileManifest, dirVersion, recordsCurrent)
	if err != nil {
		return err
	}

	cfgManifest := catalogConfig{
		version:               manifestExisting.Version,
		versionToken:          manifestExisting.VersionToken,
		sourceRelease:         manifestExisting.SourceRelease,
		sourceReleaseStart:    manifestExisting.SourceReleaseStart,
		sourceReleaseEnd:      manifestExisting.SourceReleaseEnd,
		sourceLastUpdate:      manifestExisting.SourceLastUpdate,
		sourceLastUpdateStart: manifestExisting.SourceLastUpdateStart,
		sourceLastUpdateEnd:   manifestExisting.SourceLastUpdateEnd,
	}
	if err := writeCatalogManifest(fileManifest, &cfgManifest, recordsComplete, time.Now()); err != nil {
		return err
	}

	logf("sync done (files=%d)", len(recordsComplete))
	logf("manifest written: %s", fileManifest)
	return nil
}

func inspectCatalogFile(
	filePath string,
	pathRel string,
	assetName string,
	urlFile string,
) (catalogRecord, bool, error) {
	infoFile, err := os.Stat(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return catalogRecord{}, false, nil
		}
		return catalogRecord{}, false, fmt.Errorf("stat existing file: %w", err)
	}
	if infoFile.Size() <= 0 {
		return catalogRecord{}, false, nil
	}

	record, err := buildCatalogRecord(filePath, pathRel, assetName, urlFile)
	if err != nil {
		return catalogRecord{}, false, err
	}
	return record, true, nil
}

func deriveKEGGCatalogURL() string {
	return deriveKEGGGenomeListURL()
}

func deriveKEGGGenomeListURL() string {
	return baseURL + "/list/genome"
}

func writeCatalogFile(
	filePath string,
	pathRel string,
	assetName string,
	urlFile string,
	data []byte,
) (catalogRecord, error) {
	if err := os.WriteFile(filePath, data, 0o644); err != nil {
		return catalogRecord{}, fmt.Errorf("write %s: %w", filePath, err)
	}
	return buildCatalogRecord(filePath, pathRel, assetName, urlFile)
}

func buildCatalogRecord(
	filePath string,
	pathRel string,
	assetName string,
	urlFile string,
) (catalogRecord, error) {
	infoFile, err := os.Stat(filePath)
	if err != nil {
		return catalogRecord{}, fmt.Errorf("stat file: %w", err)
	}
	sha256File, err := calculateSHA256ForFile(filePath)
	if err != nil {
		return catalogRecord{}, err
	}
	return catalogRecord{
		Asset:   assetName,
		PathRel: pathRel,
		SHA256:  sha256File,
		Bytes:   infoFile.Size(),
		URL:     urlFile,
	}, nil
}

func writeCatalogManifest(
	fileManifest string,
	cfg *catalogConfig,
	records []catalogRecord,
	timeDownloaded time.Time,
) error {
	manifest := buildCatalogManifest(cfg, records, timeDownloaded)
	return tomlx.WriteFileAtomic(fileManifest, manifest)
}

func buildCatalogManifest(
	cfg *catalogConfig,
	records []catalogRecord,
	timeDownloaded time.Time,
) catalogManifestFile {
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
	files := make([]catalogManifestFileItem, 0, len(records))
	for _, record := range records {
		files = append(files, catalogManifestFileItem{
			Asset:  record.Asset,
			Path:   record.PathRel,
			SHA256: record.SHA256,
			Bytes:  record.Bytes,
			URL:    record.URL,
		})
	}

	return catalogManifestFile{
		Database:              "kegg",
		Asset:                 "catalog",
		Catalog:               keggCatalogAsset,
		Version:               cfg.version,
		VersionToken:          cfg.versionToken,
		SourceRelease:         sourceRelease,
		SourceReleaseStart:    sourceReleaseStart,
		SourceReleaseEnd:      sourceReleaseEnd,
		SourceLastUpdate:      sourceLastUpdate,
		SourceLastUpdateStart: sourceLastUpdateStart,
		SourceLastUpdateEnd:   sourceLastUpdateEnd,
		DownloadedAt:          timeDownloaded.Format(time.RFC3339),
		Files:                 files,
	}
}

func readExistingCatalogManifest(fileManifest string) (catalogManifestFile, error) {
	var manifest catalogManifestFile
	ok, err := tomlx.ReadFileIfExists(fileManifest, &manifest)
	if err != nil {
		return catalogManifestFile{}, err
	}
	if !ok {
		return catalogManifestFile{}, nil
	}
	manifest.SourceRelease, manifest.SourceReleaseStart, manifest.SourceReleaseEnd = deriveKEGGReleaseFields(
		manifest.SourceRelease,
		manifest.SourceReleaseStart,
		manifest.SourceReleaseEnd,
	)
	manifest.SourceLastUpdate, manifest.SourceLastUpdateStart, manifest.SourceLastUpdateEnd = deriveKEGGReleaseFields(
		manifest.SourceLastUpdate,
		manifest.SourceLastUpdateStart,
		manifest.SourceLastUpdateEnd,
	)
	return manifest, nil
}

func buildCompleteCatalogRecords(
	fileManifest string,
	dirVersion string,
	recordsCurrent []catalogRecord,
) ([]catalogRecord, error) {
	recordsExisting, err := readExistingCatalogRecords(fileManifest)
	if err != nil {
		return nil, err
	}

	recordsMerged := make(map[string]catalogRecord, len(recordsExisting)+len(recordsCurrent))
	for _, record := range recordsExisting {
		recordsMerged[record.PathRel] = record
	}
	for _, record := range recordsCurrent {
		recordsMerged[record.PathRel] = record
	}

	records := make([]catalogRecord, 0, len(recordsMerged))
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
	return records, nil
}

func readExistingCatalogRecords(fileManifest string) ([]catalogRecord, error) {
	manifest, err := readExistingCatalogManifest(fileManifest)
	if err != nil {
		return nil, err
	}
	records := make([]catalogRecord, 0, len(manifest.Files))
	for _, item := range manifest.Files {
		records = append(records, catalogRecord{
			Asset:   item.Asset,
			PathRel: item.Path,
			SHA256:  item.SHA256,
			Bytes:   item.Bytes,
			URL:     item.URL,
		})
	}
	return records, nil
}

func scanCatalogRecords(dirVersion string) ([]catalogRecord, error) {
	type taskCatalogRecord struct {
		filePath string
		pathRel  string
		asset    string
		urlFile  string
	}

	return parallel.MapOrdered([]taskCatalogRecord{
		{
			filePath: filepath.Join(dirVersion, "raw", keggCatalogFileName),
			pathRel:  filepath.ToSlash(filepath.Join("raw", keggCatalogFileName)),
			asset:    keggCatalogAsset,
			urlFile:  deriveKEGGCatalogURL(),
		},
	}, func(task taskCatalogRecord) (catalogRecord, error) {
		return buildCatalogRecord(task.filePath, task.pathRel, task.asset, task.urlFile)
	})
}
