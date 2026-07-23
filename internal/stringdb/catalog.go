package stringdb

import (
	"biofetch/internal/shared/cliopt"
	"biofetch/internal/shared/parallel"
	"biofetch/internal/shared/tomlx"
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

const stringCatalogAsset = "species"

type catalogConfig struct {
	dirOut                 string
	versionToken           string
	shouldAllowInsecureTLS bool
	shouldDryRun           bool
}

type catalogLockConfig struct {
	dirSnapshot  string
	workersMax   int
	shouldDryRun bool
}

type catalogRestoreConfig struct {
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
	Database     string                    `toml:"database"`
	Asset        string                    `toml:"asset"`
	Catalog      string                    `toml:"catalog"`
	Version      string                    `toml:"version"`
	VersionToken string                    `toml:"version_token"`
	DownloadedAt string                    `toml:"downloaded_at"`
	Files        []catalogManifestFileItem `toml:"files"`
}

type catalogManifestFileItem struct {
	Asset  string `toml:"asset"`
	Path   string `toml:"path"`
	SHA256 string `toml:"sha256"`
	Bytes  int64  `toml:"bytes"`
	URL    string `toml:"url"`
}

func createCatalogFetchCommand() *cobra.Command {
	cfg := createDefaultCatalogConfig()

	commandFetch := &cobra.Command{
		Use:           "fetch",
		Short:         "Fetch STRING species catalog and update manifest.lock",
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
	flags.StringVarP(&cfg.dirOut, "output", "o", cfg.dirOut, "STRING asset root directory")
	flags.StringVar(&cfg.versionToken, "version", cfg.versionToken, "STRING release version token")
	flags.BoolVar(
		&cfg.shouldAllowInsecureTLS,
		"insecure",
		false,
		"Disable TLS certificate verification",
	)
	flags.BoolVar(&cfg.shouldDryRun, "dry-run", false, "Print actions only; do not download")
	return commandFetch
}

func createCatalogLockCommand() *cobra.Command {
	cfg := catalogLockConfig{}

	commandLock := &cobra.Command{
		Use:           "lock SNAPSHOT",
		Short:         "Rebuild STRING catalog manifest.lock from the current version directory",
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg.dirSnapshot = args[0]
			if err := validateCatalogLockConfig(&cfg); err != nil {
				return err
			}
			return runLockCatalog(&cfg)
		},
	}

	flags := commandLock.Flags()
	flags.SortFlags = false
	cliopt.BindLockWorkersFlag(flags, &cfg.workersMax)
	flags.BoolVar(&cfg.shouldDryRun, "dry-run", false, "Print actions only; do not write manifest")
	return commandLock
}

func createCatalogSyncCommand() *cobra.Command {
	cfg := catalogRestoreConfig{}

	commandSync := &cobra.Command{
		Use:           "restore SNAPSHOT",
		Short:         "Restore STRING catalog files from manifest.lock and refresh manifest",
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			var err error
			cfg.dirOut, cfg.versionToken, err = cliopt.SnapshotRootVersion(args[0])
			if err != nil {
				return err
			}
			if err := validateCatalogRestoreConfig(&cfg); err != nil {
				return err
			}
			return runRestoreCatalog(&cfg)
		},
	}

	flags := commandSync.Flags()
	flags.SortFlags = false
	flags.BoolVar(
		&cfg.shouldAllowInsecureTLS,
		"insecure",
		false,
		"Disable TLS certificate verification",
	)
	flags.BoolVar(&cfg.shouldDryRun, "dry-run", false, "Print actions only; do not download")
	return commandSync
}

func createDefaultCatalogConfig() catalogConfig {
	return catalogConfig{
		versionToken: "v12.0",
	}
}

func validateCatalogFetchConfig(cfg *catalogConfig) error {
	if strings.TrimSpace(cfg.dirOut) == "" {
		return fmt.Errorf("output is required")
	}
	if strings.TrimSpace(cfg.versionToken) == "" {
		return fmt.Errorf("version is required")
	}
	return nil
}

func validateCatalogLockConfig(cfg *catalogLockConfig) error {
	_, err := cliopt.SnapshotVersionToken(cfg.dirSnapshot)
	return err
}

func validateCatalogRestoreConfig(cfg *catalogRestoreConfig) error {
	if strings.TrimSpace(cfg.dirOut) == "" {
		return fmt.Errorf("output is required")
	}
	if strings.TrimSpace(cfg.versionToken) == "" {
		return fmt.Errorf("version is required")
	}
	return nil
}

func runFetchCatalog(cfg *catalogConfig) error {
	dirVersion := filepath.Join(cfg.dirOut, "catalog", cfg.versionToken)
	dirRaw := filepath.Join(dirVersion, "raw")
	fileManifest := filepath.Join(dirVersion, "manifest.lock")
	fileName := deriveCatalogFileName(cfg.versionToken)
	fileOut := filepath.Join(dirRaw, fileName)
	pathRel := filepath.ToSlash(filepath.Join("raw", fileName))
	urlFile := deriveCatalogURL(cfg.versionToken)

	if cfg.shouldDryRun {
		logf("dry-run version dir: %s", dirVersion)
		logf("dry-run manifest: %s", fileManifest)
		logf("[dry-run] %s -> %s", urlFile, fileOut)
		return nil
	}

	if err := os.MkdirAll(dirRaw, 0o755); err != nil {
		return fmt.Errorf("create raw dir: %w", err)
	}

	record, ok, err := inspectCatalogFile(fileOut, pathRel, stringCatalogAsset, urlFile)
	if err != nil {
		return err
	}
	if !ok {
		logf("downloading %s", fileName)
		clientHTTP := createHTTPClient(cfg.shouldAllowInsecureTLS)
		if err := downloadFile(clientHTTP, urlFile, fileOut); err != nil {
			return err
		}
		record, err = buildCatalogRecord(fileOut, pathRel, stringCatalogAsset, urlFile)
		if err != nil {
			return err
		}
	} else {
		logf("using existing %s", fileName)
	}

	recordsComplete, err := buildCompleteCatalogRecords(fileManifest, dirVersion, []catalogRecord{record})
	if err != nil {
		return err
	}
	if err := writeCatalogManifest(fileManifest, cfg.versionToken, recordsComplete, time.Now()); err != nil {
		return err
	}

	logf("done (files=%d)", len(recordsComplete))
	logf("manifest written: %s", fileManifest)
	return nil
}

func runLockCatalog(cfg *catalogLockConfig) error {
	if err := cliopt.NormalizeLockWorkersMax(&cfg.workersMax); err != nil {
		return err
	}
	versionToken, err := cliopt.SnapshotVersionToken(cfg.dirSnapshot)
	if err != nil {
		return err
	}
	dirVersion := cfg.dirSnapshot
	fileManifest := filepath.Join(dirVersion, "manifest.lock")

	records, err := scanCatalogRecords(dirVersion, versionToken, cfg.workersMax)
	if err != nil {
		return err
	}

	if cfg.shouldDryRun {
		logf("dry-run lock version dir: %s", dirVersion)
		logf("dry-run manifest: %s", fileManifest)
		logf("dry-run files=%d", len(records))
		return nil
	}

	if err := writeCatalogManifest(fileManifest, versionToken, records, time.Now()); err != nil {
		return err
	}

	logf("lock updated: %s", fileManifest)
	return nil
}

func runRestoreCatalog(cfg *catalogRestoreConfig) error {
	dirVersion := filepath.Join(cfg.dirOut, "catalog", cfg.versionToken)
	fileManifest := filepath.Join(dirVersion, "manifest.lock")

	recordsManifest, err := readExistingCatalogRecords(fileManifest)
	if err != nil {
		return err
	}
	if len(recordsManifest) == 0 {
		return fmt.Errorf("manifest is empty or missing: %s", fileManifest)
	}

	if cfg.shouldDryRun {
		for _, record := range recordsManifest {
			filePath := filepath.Join(dirVersion, filepath.FromSlash(record.PathRel))
			logf("[dry-run] sync %s -> %s", record.URL, filePath)
		}
		logf("dry-run restore done (files=%d)", len(recordsManifest))
		return nil
	}

	clientHTTP := createHTTPClient(cfg.shouldAllowInsecureTLS)
	recordsCurrent := make([]catalogRecord, 0, len(recordsManifest))
	for _, record := range recordsManifest {
		filePath := filepath.Join(dirVersion, filepath.FromSlash(record.PathRel))
		if err := os.MkdirAll(filepath.Dir(filePath), 0o755); err != nil {
			return fmt.Errorf("create restore dir: %w", err)
		}

		recordCurrent, ok, err := inspectCatalogFile(filePath, record.PathRel, record.Asset, record.URL)
		if err != nil {
			return err
		}
		if !ok || recordCurrent.SHA256 != record.SHA256 {
			logf("restore downloading %s", filepath.Base(filePath))
			if err := downloadFile(clientHTTP, record.URL, filePath); err != nil {
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
	if err := writeCatalogManifest(fileManifest, cfg.versionToken, recordsComplete, time.Now()); err != nil {
		return err
	}

	logf("restore done (files=%d)", len(recordsComplete))
	logf("manifest written: %s", fileManifest)
	return nil
}

func deriveCatalogFileName(versionToken string) string {
	return fmt.Sprintf("species.%s.txt", versionToken)
}

func deriveCatalogURL(versionToken string) string {
	return fmt.Sprintf("%s/species.%s.txt", baseURL, versionToken)
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

	sha256File, err := calculateCatalogSHA256ForFile(filePath)
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

func calculateCatalogSHA256ForFile(filePath string) (string, error) {
	fileIn, err := os.Open(filePath)
	if err != nil {
		return "", fmt.Errorf("open %s for sha256: %w", filePath, err)
	}
	defer fileIn.Close()

	hashSHA256 := sha256.New()
	if _, err := io.Copy(hashSHA256, fileIn); err != nil {
		return "", fmt.Errorf("hash %s: %w", filePath, err)
	}
	return fmt.Sprintf("%x", hashSHA256.Sum(nil)), nil
}

func writeCatalogManifest(
	fileManifest string,
	versionToken string,
	records []catalogRecord,
	timeDownloaded time.Time,
) error {
	manifest := buildCatalogManifestFile(versionToken, records, timeDownloaded)
	return tomlx.WriteFileAtomic(fileManifest, manifest)
}

func buildCatalogManifestFile(
	versionToken string,
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
		Database:     "string",
		Asset:        "catalog",
		Catalog:      stringCatalogAsset,
		Version:      strings.TrimPrefix(versionToken, "v"),
		VersionToken: versionToken,
		DownloadedAt: timeDownloaded.Format(time.RFC3339),
		Files:        files,
	}
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
	var manifest catalogManifestFile
	ok, err := tomlx.ReadFileIfExists(fileManifest, &manifest)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, nil
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

func scanCatalogRecords(dirVersion string, versionToken string, workersMax int) ([]catalogRecord, error) {
	fileName := deriveCatalogFileName(versionToken)
	filePath := filepath.Join(dirVersion, "raw", fileName)
	return parallel.MapOrderedWithWorkers([]string{filePath}, workersMax, func(path string) (catalogRecord, error) {
		return buildCatalogRecord(path, filepath.ToSlash(filepath.Join("raw", fileName)), stringCatalogAsset, deriveCatalogURL(versionToken))
	})
}
