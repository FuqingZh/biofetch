package kegg

import (
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
	keggCatalogURL      = baseURL + "/list/organism"
)

type catalogConfig struct {
	dirOut                 string
	version                string
	versionToken           string
	sourceRelease          string
	shouldAllowInsecureTLS bool
	shouldDryRun           bool
}

type catalogLockConfig struct {
	dirOut       string
	versionToken string
	shouldDryRun bool
}

type catalogSyncConfig struct {
	dirOut                 string
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
	Database      string                    `toml:"database"`
	Asset         string                    `toml:"asset"`
	Catalog       string                    `toml:"catalog"`
	Version       string                    `toml:"version"`
	VersionToken  string                    `toml:"version_token"`
	SourceRelease string                    `toml:"source_release"`
	DownloadedAt  string                    `toml:"downloaded_at"`
	Files         []catalogManifestFileItem `toml:"files"`
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
	flags.StringVar(&cfg.versionToken, "version", cfg.versionToken, "KEGG major version, e.g. 117.0")
	flags.BoolVar(
		&cfg.shouldAllowInsecureTLS,
		"should_allow_insecure_tls",
		false,
		"Disable TLS certificate verification",
	)
	flags.BoolVar(&cfg.shouldDryRun, "should_dry_run", false, "Print actions only; do not download")
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
	flags.StringVar(&cfg.dirOut, "dir_out", "", "KEGG asset root directory")
	flags.StringVar(&cfg.versionToken, "version", "", "KEGG version token")
	flags.BoolVar(&cfg.shouldDryRun, "should_dry_run", false, "Print actions only; do not write manifest")
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
	flags.StringVar(&cfg.versionToken, "version", "", "KEGG version token")
	flags.BoolVar(
		&cfg.shouldAllowInsecureTLS,
		"should_allow_insecure_tls",
		false,
		"Disable TLS certificate verification",
	)
	flags.BoolVar(&cfg.shouldDryRun, "should_dry_run", false, "Print actions only; do not download")
	return commandSync
}

func validateCatalogFetchConfig(cfg *catalogConfig) error {
	if strings.TrimSpace(cfg.dirOut) == "" {
		return fmt.Errorf("dir_out is required")
	}
	if strings.TrimSpace(cfg.versionToken) != "" && !isValidKEGGMajorVersion(cfg.versionToken) {
		return fmt.Errorf("version must be a KEGG major version like 117.0")
	}
	return nil
}

func validateCatalogLockConfig(cfg *catalogLockConfig) error {
	if strings.TrimSpace(cfg.dirOut) == "" {
		return fmt.Errorf("dir_out is required")
	}
	if strings.TrimSpace(cfg.versionToken) == "" {
		return fmt.Errorf("version is required")
	}
	if !isValidKEGGMajorVersion(cfg.versionToken) {
		return fmt.Errorf("version must be a KEGG major version like 117.0")
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
	if !isValidKEGGMajorVersion(cfg.versionToken) {
		return fmt.Errorf("version must be a KEGG major version like 117.0")
	}
	return nil
}

func runFetchCatalog(cfg *catalogConfig) error {
	clientHTTP := createHTTPClient(cfg.shouldAllowInsecureTLS)
	clientKegg := createKEGGClient(clientHTTP, 350*time.Millisecond, defaultKEGGRetryMax, defaultKEGGRetryWait)

	sourceRelease, currentMajorVersion, err := resolveKEGGVersion(clientKegg, "kegg")
	if err != nil {
		return err
	}
	if strings.TrimSpace(cfg.versionToken) == "" {
		cfg.versionToken = currentMajorVersion
	} else if cfg.versionToken != currentMajorVersion {
		return fmt.Errorf("version %q does not match current KEGG catalog major version %q", cfg.versionToken, currentMajorVersion)
	}
	cfg.version = cfg.versionToken
	cfg.sourceRelease = sourceRelease

	dirVersion := filepath.Join(cfg.dirOut, "catalog", cfg.versionToken)
	dirRaw := filepath.Join(dirVersion, "raw")
	fileManifest := filepath.Join(dirVersion, "manifest.lock")
	fileOut := filepath.Join(dirRaw, keggCatalogFileName)
	pathRel := filepath.ToSlash(filepath.Join("raw", keggCatalogFileName))

	if cfg.shouldDryRun {
		logf("[dry-run] version dir: %s", dirVersion)
		logf("[dry-run] manifest: %s", fileManifest)
		logf("[dry-run] %s -> %s", keggCatalogURL, fileOut)
		return nil
	}

	if err := os.MkdirAll(dirRaw, 0o755); err != nil {
		return fmt.Errorf("create raw dir: %w", err)
	}

	record, ok, err := inspectCatalogFile(fileOut, pathRel, keggCatalogAsset, keggCatalogURL)
	if err != nil {
		return err
	}
	if !ok {
		logf("downloading %s", keggCatalogFileName)
		data, err := clientKegg.download(keggCatalogURL)
		if err != nil {
			return err
		}
		record, err = writeCatalogFile(fileOut, pathRel, keggCatalogAsset, keggCatalogURL, data)
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
	if err := writeCatalogManifest(fileManifest, cfg, recordsComplete, time.Now()); err != nil {
		return err
	}

	logf("done (files=%d)", len(recordsComplete))
	logf("manifest written: %s", fileManifest)
	return nil
}

func runLockCatalog(cfg *catalogLockConfig) error {
	dirVersion := filepath.Join(cfg.dirOut, "catalog", cfg.versionToken)
	fileManifest := filepath.Join(dirVersion, "manifest.lock")

	records, err := scanCatalogRecords(dirVersion)
	if err != nil {
		return err
	}

	manifestExisting, _ := readExistingCatalogManifest(fileManifest)
	cfgManifest := catalogConfig{
		version:       firstNonEmpty(manifestExisting.Version, cfg.versionToken),
		versionToken:  firstNonEmpty(manifestExisting.VersionToken, cfg.versionToken),
		sourceRelease: manifestExisting.SourceRelease,
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
			data, err := clientKegg.download(record.URL)
			if err != nil {
				return err
			}
			recordCurrent, err = writeCatalogFile(filePath, record.PathRel, record.Asset, record.URL, data)
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
		version:       manifestExisting.Version,
		versionToken:  manifestExisting.VersionToken,
		sourceRelease: manifestExisting.SourceRelease,
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
		Database:      "kegg",
		Asset:         "catalog",
		Catalog:       keggCatalogAsset,
		Version:       cfg.version,
		VersionToken:  cfg.versionToken,
		SourceRelease: cfg.sourceRelease,
		DownloadedAt:  timeDownloaded.Format(time.RFC3339),
		Files:         files,
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
	filePath := filepath.Join(dirVersion, "raw", keggCatalogFileName)
	record, err := buildCatalogRecord(filePath, filepath.ToSlash(filepath.Join("raw", keggCatalogFileName)), keggCatalogAsset, keggCatalogURL)
	if err != nil {
		return nil, err
	}
	return []catalogRecord{record}, nil
}
