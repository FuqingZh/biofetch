package interpro

import (
	"biofetch/internal/shared/cliopt"
	"biofetch/internal/shared/logx"
	"biofetch/internal/shared/staticasset"
	"crypto/md5"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

const (
	scanLabel          = "InterProScan"
	defaultScanBaseURL = "https://ftp.ebi.ac.uk/pub/software/unix/iprscan/5"
)

var patternScanVersion = regexp.MustCompile(`^[0-9]{1,4}\.[0-9]{1,4}-[0-9]{1,4}\.[0-9]{1,4}$`)
var patternMD5File = regexp.MustCompile(`^([0-9A-Fa-f]{32})[ \t]+\*?([^/\r\n]+)(?:\r?\n)?$`)
var scanBaseURL = defaultScanBaseURL
var openScanArchive = os.Open

type scanConfig struct {
	cliopt.DirOutConfig
	cliopt.VersionConfig
	cliopt.ExistingRuleConfig
	cliopt.RetryConfig
	cliopt.DownloadControlConfig
	cliopt.InsecureTLSConfig
	cliopt.DryRunConfig
	cliopt.LogConfig
	cliopt.ProgressConfig
	shouldAllowLargeDownloads bool
}

type scanLockConfig struct {
	cliopt.DirSnapshotConfig
	cliopt.DryRunConfig
	cliopt.LogConfig
	workersMax int
}

type scanRestoreConfig struct {
	cliopt.DirOutConfig
	cliopt.VersionConfig
	cliopt.ExistingRuleConfig
	cliopt.RetryConfig
	cliopt.DownloadControlConfig
	cliopt.InsecureTLSConfig
	cliopt.DryRunConfig
	cliopt.LogConfig
	cliopt.ProgressConfig
}

func runFetchScan(cfg *scanConfig) error {
	version, err := normalizeScanVersion(cfg.VersionToken)
	if err != nil {
		return err
	}
	if !cfg.shouldAllowLargeDownloads {
		return fmt.Errorf("%s fetch requires --allow-large-downloads", scanLabel)
	}
	if err := validateScanFetchConfig(cfg); err != nil {
		return err
	}
	sourceChecksum, sourceComplete := buildScanSources(version)
	options := buildScanOptions(cfg.DirOut, cfg.ExistingRuleConfig, cfg.RetryConfig, cfg.DownloadControlConfig, cfg.InsecureTLSConfig, cfg.DryRunConfig, cfg.ProgressConfig)
	if cfg.ShouldDryRun {
		return staticasset.Fetch(sourceComplete, options, nil)
	}
	trace, closeRun, err := logx.StartSourceRun("biofetch interpro scan", "fetch", cfg.DirLogs, cfg.DirOut, sourceComplete)
	if err != nil {
		return err
	}
	defer closeRun()
	if err := staticasset.Fetch(sourceChecksum, options, trace); err != nil {
		return err
	}
	dirVersion := staticasset.DeriveVersionDir(cfg.DirOut, sourceComplete)
	checksumPath := filepath.Join(dirVersion, filepath.FromSlash(sourceChecksum.Assets[0].Path))
	expectedMD5, err := readScanMD5(checksumPath, scanArchiveName(version))
	if err != nil {
		return err
	}
	sourceComplete.Assets[0].VerifyDownloadedFile = md5Verifier(expectedMD5)
	if err := staticasset.Fetch(sourceComplete, options, trace); err != nil {
		return err
	}
	return nil
}

func runLockScan(cfg *scanLockConfig) error {
	version, err := cliopt.SnapshotVersionToken(cfg.DirSnapshot)
	if err != nil {
		return err
	}
	version, err = normalizeScanVersion(version)
	if err != nil {
		return err
	}
	if _, _, err := cliopt.FlatSnapshotRootVersion(cfg.DirSnapshot, "scan"); err != nil {
		return err
	}
	archiveName := scanArchiveName(version)
	archivePath := filepath.Join(cfg.DirSnapshot, "raw", archiveName)
	archiveInfo, err := os.Stat(archivePath)
	if err != nil {
		return fmt.Errorf("InterProScan archive is required at %s: %w", archivePath, err)
	}
	if !archiveInfo.Mode().IsRegular() {
		return fmt.Errorf("InterProScan archive is required to be a regular file: %s", archivePath)
	}
	expectedMD5, err := readScanMD5(archivePath+".md5", archiveName)
	if err != nil {
		return err
	}
	_, source := buildScanSources(version)
	source.Assets[0].HashForLock = scanArchiveLockHasher(expectedMD5)
	if cfg.ShouldDryRun {
		return staticasset.Lock(source, cfg.DirSnapshot, staticasset.Options{RetryMax: 1, WorkersMax: cfg.workersMax, ShouldDryRun: true}, nil)
	}
	_, closeRun, err := logx.StartVersionedRun("biofetch interpro scan", "lock", cfg.DirLogs, cfg.DirSnapshot)
	if err != nil {
		return err
	}
	defer closeRun()
	return staticasset.Lock(source, cfg.DirSnapshot, staticasset.Options{
		RuleExisting: "skip", RetryMax: 1, WorkersMax: cfg.workersMax,
	}, logx.NewStaticAssetTraceSink("biofetch interpro scan"))
}

func runRestoreScan(cfg *scanRestoreConfig, snapshot string) error {
	dirRoot, version, err := cliopt.FlatSnapshotRootVersion(snapshot, "scan")
	if err != nil {
		return err
	}
	version, err = normalizeScanVersion(version)
	if err != nil {
		return err
	}
	manifest, ok, err := staticasset.ReadManifest(filepath.Join(snapshot, "manifest.lock"))
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("manifest is empty or missing: %s", filepath.Join(snapshot, "manifest.lock"))
	}
	if manifest.Database != "interpro" || manifest.Asset != "scan" || manifest.Source != "ftp" ||
		manifest.Version != version || manifest.VersionToken != version {
		return fmt.Errorf("manifest identity does not match InterProScan snapshot %s", snapshot)
	}
	if err := validateScanManifestFiles(manifest, version); err != nil {
		return err
	}
	cfg.DirOut = dirRoot
	cfg.VersionToken = version
	if err := validateScanRestoreConfig(cfg); err != nil {
		return err
	}
	source := buildScanSource(version, buildScanRestoreAssets(manifest))
	options := buildScanOptions(cfg.DirOut, cfg.ExistingRuleConfig, cfg.RetryConfig, cfg.DownloadControlConfig, cfg.InsecureTLSConfig, cfg.DryRunConfig, cfg.ProgressConfig)
	if cfg.ShouldDryRun {
		return staticasset.Sync(source, options, nil)
	}
	trace, closeRun, err := logx.StartSourceRun("biofetch interpro scan", "restore", cfg.DirLogs, cfg.DirOut, source)
	if err != nil {
		return err
	}
	defer closeRun()
	return staticasset.Sync(source, options, trace)
}

func normalizeScanVersion(value string) (string, error) {
	if !patternScanVersion.MatchString(value) {
		return "", fmt.Errorf("%s version must look like 5.77-108.0: %s", scanLabel, value)
	}
	return value, nil
}

func scanArchiveName(version string) string {
	return "interproscan-" + version + "-64-bit.tar.gz"
}

func buildScanSources(version string) (staticasset.Source, staticasset.Source) {
	archiveName := scanArchiveName(version)
	baseURL := strings.TrimRight(scanBaseURL, "/") + "/" + version
	checksum := staticasset.Asset{
		Name: "archive.md5", Path: filepath.ToSlash(filepath.Join("raw", archiveName+".md5")),
		URL: baseURL + "/" + archiveName + ".md5", ReuseVerifiedExisting: true,
		VerifyDownloadedFile: scanMD5FormatVerifier(archiveName),
	}
	archive := staticasset.Asset{
		Name: "archive", Path: filepath.ToSlash(filepath.Join("raw", archiveName)),
		URL: baseURL + "/" + archiveName,
	}
	return buildScanSource(version, []staticasset.Asset{checksum}), buildScanSource(version, []staticasset.Asset{archive, checksum})
}

func buildScanSource(version string, assets []staticasset.Asset) staticasset.Source {
	return staticasset.Source{
		Database: "interpro", Asset: "scan", Source: "ftp",
		Version: version, VersionToken: version, Assets: assets,
		LockOnlyDeclaredAssets: true,
	}
}

func validateScanManifestFiles(manifest staticasset.Manifest, version string) error {
	archiveName := scanArchiveName(version)
	expected := map[string]string{
		filepath.ToSlash(filepath.Join("raw", archiveName)):        "archive",
		filepath.ToSlash(filepath.Join("raw", archiveName+".md5")): "archive.md5",
	}
	seen := make(map[string]struct{}, len(manifest.Files))
	for _, record := range manifest.Files {
		asset, ok := expected[record.Path]
		if !ok || record.Asset != asset {
			return fmt.Errorf("InterProScan manifest contains unexpected file record: %s", record.Path)
		}
		if _, duplicate := seen[record.Path]; duplicate {
			return fmt.Errorf("InterProScan manifest contains duplicate file record: %s", record.Path)
		}
		if strings.TrimSpace(record.URL) == "" {
			return fmt.Errorf("InterProScan manifest file URL is empty: %s", record.Path)
		}
		seen[record.Path] = struct{}{}
	}
	if len(seen) != len(expected) {
		return fmt.Errorf("InterProScan manifest must contain archive and archive.md5 records")
	}
	return nil
}

func buildScanRestoreAssets(manifest staticasset.Manifest) []staticasset.Asset {
	assets := make([]staticasset.Asset, 0, len(manifest.Files))
	for _, record := range manifest.Files {
		assets = append(assets, staticasset.Asset{
			Name: record.Asset, Path: record.Path,
			VerifyDownloadedFile: staticasset.SHA256Verifier(record.SHA256),
		})
	}
	return assets
}

func parseScanMD5(data []byte, expectedFilename string) (string, error) {
	matches := patternMD5File.FindSubmatch(data)
	if len(matches) != 3 {
		return "", fmt.Errorf("malformed InterProScan MD5 file: expected exactly one MD5 and filename pair")
	}
	filename := string(matches[2])
	if filename != expectedFilename {
		return "", fmt.Errorf("InterProScan MD5 filename %q does not match archive %q", filename, expectedFilename)
	}
	return strings.ToLower(string(matches[1])), nil
}

func scanMD5FormatVerifier(expectedFilename string) func(string) error {
	return func(path string) error {
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		_, err = parseScanMD5(data, expectedFilename)
		return err
	}
}

func readScanMD5(path string, expectedFilename string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read InterProScan MD5 file %s: %w", path, err)
	}
	return parseScanMD5(data, expectedFilename)
}

func calculateMD5(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("open %s for MD5: %w", path, err)
	}
	defer file.Close()
	hash := md5.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", fmt.Errorf("calculate MD5 for %s: %w", path, err)
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func md5Verifier(expected string) func(string) error {
	return func(path string) error {
		actual, err := calculateMD5(path)
		if err != nil {
			return err
		}
		if actual != expected {
			return fmt.Errorf("MD5 mismatch: got %s, want %s", actual, expected)
		}
		return nil
	}
}

func scanArchiveLockHasher(expectedMD5 string) func(string, staticasset.HashProgressFunc) (string, int64, error) {
	return func(path string, progress staticasset.HashProgressFunc) (string, int64, error) {
		file, err := openScanArchive(path)
		if err != nil {
			return "", 0, fmt.Errorf("open %s for checksums: %w", path, err)
		}
		defer file.Close()
		info, err := file.Stat()
		if err != nil {
			return "", 0, fmt.Errorf("stat %s for checksums: %w", path, err)
		}
		hashMD5 := md5.New()
		hashSHA256 := sha256.New()
		writer := io.MultiWriter(hashMD5, hashSHA256)
		if progress != nil {
			progress(0, info.Size())
			writer = io.MultiWriter(hashMD5, hashSHA256, &scanHashProgressWriter{
				progress: progress,
				total:    info.Size(),
			})
		}
		bytesRead, err := io.Copy(writer, file)
		if err != nil {
			return "", 0, fmt.Errorf("hash %s: %w", path, err)
		}
		actualMD5 := hex.EncodeToString(hashMD5.Sum(nil))
		if actualMD5 != expectedMD5 {
			return "", 0, fmt.Errorf("asset %q failed MD5 verification: MD5 mismatch: got %s, want %s", "archive", actualMD5, expectedMD5)
		}
		return hex.EncodeToString(hashSHA256.Sum(nil)), bytesRead, nil
	}
}

type scanHashProgressWriter struct {
	progress staticasset.HashProgressFunc
	done     int64
	total    int64
}

func (writer *scanHashProgressWriter) Write(buffer []byte) (int, error) {
	writer.done += int64(len(buffer))
	writer.progress(writer.done, writer.total)
	return len(buffer), nil
}

func createDefaultScanConfig() scanConfig {
	cfg := scanConfig{}
	cfg.RetryMax = 5
	cfg.RetryWait = 3 * time.Second
	cfg.WorkersMax = 1
	cfg.RuleExisting = "skip"
	return cfg
}

func validateScanFetchConfig(cfg *scanConfig) error {
	if err := cliopt.ValidateDirOutRequired(cfg.DirOut); err != nil {
		return err
	}
	if err := cliopt.ValidateRetryConfig(&cfg.RetryConfig); err != nil {
		return err
	}
	if err := cliopt.ValidateDownloadControlConfig(&cfg.DownloadControlConfig); err != nil {
		return err
	}
	return cliopt.ValidateRuleExisting(&cfg.ExistingRuleConfig)
}

func validateScanRestoreConfig(cfg *scanRestoreConfig) error {
	if err := cliopt.ValidateRetryConfig(&cfg.RetryConfig); err != nil {
		return err
	}
	if err := cliopt.ValidateDownloadControlConfig(&cfg.DownloadControlConfig); err != nil {
		return err
	}
	return cliopt.ValidateRuleExisting(&cfg.ExistingRuleConfig)
}

func buildScanOptions(
	dirOut string,
	existing cliopt.ExistingRuleConfig,
	retry cliopt.RetryConfig,
	download cliopt.DownloadControlConfig,
	tls cliopt.InsecureTLSConfig,
	dryRun cliopt.DryRunConfig,
	progress cliopt.ProgressConfig,
) staticasset.Options {
	return staticasset.Options{
		DirOut: dirOut, RuleExisting: existing.RuleExisting,
		RetryMax: retry.RetryMax, RetryWait: retry.RetryWait,
		WorkersMax: download.WorkersMax, RequestInterval: download.RequestInterval,
		ShouldAllowInsecureTLS: tls.ShouldAllowInsecureTLS,
		ShouldDryRun:           dryRun.ShouldDryRun, ShouldDisableProgress: progress.ShouldDisableProgress,
	}
}
