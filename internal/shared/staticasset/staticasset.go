package staticasset

import (
	"biofetch/internal/shared/cliopt"
	"biofetch/internal/shared/filehash"
	"biofetch/internal/shared/httpx"
	"biofetch/internal/shared/parallel"
	"biofetch/internal/shared/tomlx"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

type Asset struct {
	Name                  string
	Path                  string
	URL                   string
	RecoverDownloadError  func(string, error) (bool, error)                     `toml:"-"`
	VerifyDownloadedFile  func(string) error                                    `toml:"-"`
	HashForLock           func(string, HashProgressFunc) (string, int64, error) `toml:"-"`
	ReuseVerifiedExisting bool                                                  `toml:"-"`
}

type HashProgressFunc = httpx.DownloadProgressFunc

type Source struct {
	Database               string
	Asset                  string
	Source                 string
	DirName                string
	Version                string
	VersionToken           string
	Scope                  Scope
	Assets                 []Asset
	LockOnlyDeclaredAssets bool
}

type Scope struct {
	Type  string `toml:"type,omitempty"`
	Value string `toml:"value,omitempty"`
}

type Options struct {
	DirOut                 string
	RuleExisting           string
	RetryMax               int
	RetryWait              time.Duration
	WorkersMax             int
	RequestInterval        time.Duration
	ShouldAllowInsecureTLS bool
	ShouldDryRun           bool
	ShouldDisableProgress  bool
	ProgressWriter         io.Writer
}

type FileRecord struct {
	Asset  string
	Path   string
	SHA256 string
	Bytes  int64
	URL    string
}

type Manifest struct {
	Database     string               `toml:"database"`
	Asset        string               `toml:"asset"`
	Source       string               `toml:"source,omitempty"`
	Version      string               `toml:"version"`
	VersionToken string               `toml:"version_token"`
	DownloadedAt string               `toml:"downloaded_at"`
	Scope        Scope                `toml:"scope,omitempty"`
	Files        []ManifestFileRecord `toml:"files"`
}

type ManifestFileRecord struct {
	Asset  string `toml:"asset"`
	Path   string `toml:"path"`
	SHA256 string `toml:"sha256"`
	Bytes  int64  `toml:"bytes"`
	URL    string `toml:"url"`
}

type TraceEvent struct {
	Event        string
	Database     string
	Asset        string
	VersionToken string
	Path         string
	URL          string
	Bytes        int64
	SHA256       string
	Status       string
}

type TraceSink interface {
	EmitStaticAssetTrace(event TraceEvent)
}

type downloadTask struct {
	asset Asset
}

type scanTask struct {
	filePath    string
	pathRel     string
	asset       string
	url         string
	bytes       int64
	hashForLock func(string, HashProgressFunc) (string, int64, error)
	verifyFile  func(string) error
}

const manifestFlushInterval = 5 * time.Second

type progressReporter struct {
	writer        io.Writer
	label         string
	timeStarted   time.Time
	timeLastDraw  time.Time
	totalFiles    int
	doneFiles     int
	downloadFiles int
	totalBytes    int64
	doneBytes     int64
	knownBytes    bool
	fileBytes     map[string]int64
	fileTotals    map[string]int64
	currentPath   string
	currentBytes  int64
	currentTotal  int64
	lastLineWidth int
	mutex         sync.Mutex
}

type incrementalManifestWriter struct {
	mutex         sync.Mutex
	fileManifest  string
	source        Source
	dirVersion    string
	recordsByPath map[string]FileRecord
	flushInterval time.Duration
	timeLastFlush time.Time
	isDirty       bool
}

func Fetch(source Source, options Options, trace TraceSink) error {
	if err := validateSource(source); err != nil {
		return err
	}
	if err := validateOptions(options); err != nil {
		return err
	}
	emit(trace, source, TraceEvent{Event: "resolve_source", Status: "ok"})
	emit(trace, source, TraceEvent{Event: "resolve_assets", Status: fmt.Sprintf("count=%d", len(source.Assets))})

	dirVersion := buildVersionDir(options.DirOut, source)
	fileManifest := filepath.Join(dirVersion, "manifest.lock")
	if options.ShouldDryRun {
		emit(trace, source, TraceEvent{Event: "plan_fetch", Path: dirVersion, Status: "dry_run"})
		return nil
	}
	if err := ensureVersionDirs(dirVersion); err != nil {
		return err
	}

	recordsExistingByPath, err := readRecordIndex(fileManifest)
	if err != nil {
		return err
	}

	recordsReused, tasksDownload, err := planFetch(source.Assets, dirVersion, shouldOverwrite(options), recordsExistingByPath, trace, source)
	if err != nil {
		return err
	}
	emit(trace, source, TraceEvent{Event: "plan_fetch", Status: fmt.Sprintf("reuse=%d download=%d", len(recordsReused), len(tasksDownload))})

	clientHTTP := httpx.NewClient(options.ShouldAllowInsecureTLS)
	limiterRequest := httpx.NewRequestLimiter(options.RequestInterval)
	progress := newProgressReporter(source, options, len(source.Assets), len(recordsReused))
	progress.plan(len(recordsReused), len(tasksDownload))
	writerManifest, err := newIncrementalManifestWriter(fileManifest, source, dirVersion)
	if err != nil {
		progress.finish(false)
		return err
	}
	recordsDownloaded, err := runDownloadTasks(clientHTTP, source, dirVersion, tasksDownload, options, limiterRequest, trace, progress, writerManifest)
	if err != nil {
		_ = writerManifest.flush()
		progress.finish(false)
		return err
	}
	if err := writerManifest.flush(); err != nil {
		progress.finish(false)
		return err
	}
	progress.finish(true)

	records := append([]FileRecord{}, recordsReused...)
	records = append(records, recordsDownloaded...)
	recordsComplete, err := buildCompleteRecords(fileManifest, dirVersion, records)
	if err != nil {
		return err
	}
	if err := writeManifest(fileManifest, source, recordsComplete, time.Now()); err != nil {
		return err
	}
	emit(trace, source, TraceEvent{Event: "write_manifest", Path: fileManifest, Status: fmt.Sprintf("files=%d", len(recordsComplete))})
	return nil
}

func DeriveVersionDir(dirOut string, source Source) string {
	return buildVersionDir(dirOut, source)
}

func Lock(source Source, dirVersion string, options Options, trace TraceSink) error {
	if err := validateSourceIdentity(source); err != nil {
		return err
	}
	if strings.TrimSpace(dirVersion) == "" {
		return fmt.Errorf("snapshot is required")
	}
	if err := cliopt.NormalizeLockWorkersMax(&options.WorkersMax); err != nil {
		return err
	}
	if err := validateLockOptions(options); err != nil {
		return err
	}
	fileManifest := filepath.Join(dirVersion, "manifest.lock")
	assetsByPath, err := buildLockAssetMap(fileManifest, source.Assets)
	if err != nil {
		return err
	}
	progress := newProgressReporter(source, options, 0, 0)
	records, err := scanRecords(dirVersion, assetsByPath, source.LockOnlyDeclaredAssets, progress, options.WorkersMax)
	if err != nil {
		progress.finish(false)
		return err
	}
	progress.finish(true)
	emit(trace, source, TraceEvent{Event: "scan_files", Path: filepath.Join(dirVersion, "raw"), Status: fmt.Sprintf("files=%d", len(records))})
	if options.ShouldDryRun {
		emit(trace, source, TraceEvent{Event: "lock_rebuild", Path: fileManifest, Status: "dry_run"})
		return nil
	}
	if err := writeManifest(fileManifest, source, records, time.Now()); err != nil {
		return err
	}
	emit(trace, source, TraceEvent{Event: "write_manifest", Path: fileManifest, Status: fmt.Sprintf("files=%d", len(records))})
	return nil
}

func validateLockOptions(options Options) error {
	if options.RetryMax < 1 {
		return fmt.Errorf("max-attempts must be >= 1")
	}
	return nil
}

func Sync(source Source, options Options, trace TraceSink) error {
	if err := validateSourceIdentity(source); err != nil {
		return err
	}
	if err := validateOptions(options); err != nil {
		return err
	}
	dirVersion := buildVersionDir(options.DirOut, source)
	fileManifest := filepath.Join(dirVersion, "manifest.lock")
	manifest, ok, err := ReadManifest(fileManifest)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("manifest is empty or missing: %s", fileManifest)
	}
	if manifest.Database != source.Database || manifest.Asset != source.Asset ||
		manifest.VersionToken != source.VersionToken {
		return fmt.Errorf(
			"manifest identity mismatch: got database=%q asset=%q version_token=%q, want database=%q asset=%q version_token=%q",
			manifest.Database,
			manifest.Asset,
			manifest.VersionToken,
			source.Database,
			source.Asset,
			source.VersionToken,
		)
	}
	recordsManifest, err := readRecords(fileManifest)
	if err != nil {
		return err
	}
	if len(recordsManifest) == 0 {
		return fmt.Errorf("manifest is empty or missing: %s", fileManifest)
	}
	emit(trace, source, TraceEvent{Event: "read_manifest", Path: fileManifest, Status: fmt.Sprintf("files=%d", len(recordsManifest))})
	if options.ShouldDryRun {
		emit(trace, source, TraceEvent{Event: "plan_sync", Status: "dry_run"})
		return nil
	}
	if err := ensureVersionDirs(dirVersion); err != nil {
		return err
	}

	recordsReused, tasksDownload, err := planSync(recordsManifest, dirVersion, shouldOverwrite(options), trace, source)
	if err != nil {
		return err
	}
	emit(trace, source, TraceEvent{Event: "plan_sync", Status: fmt.Sprintf("reuse=%d download=%d", len(recordsReused), len(tasksDownload))})

	clientHTTP := httpx.NewClient(options.ShouldAllowInsecureTLS)
	limiterRequest := httpx.NewRequestLimiter(options.RequestInterval)
	progress := newProgressReporter(source, options, len(recordsManifest), len(recordsReused))
	progress.plan(len(recordsReused), len(tasksDownload))
	writerManifest, err := newIncrementalManifestWriter(fileManifest, source, dirVersion)
	if err != nil {
		progress.finish(false)
		return err
	}
	recordsDownloaded, err := runDownloadTasks(clientHTTP, source, dirVersion, tasksDownload, options, limiterRequest, trace, progress, writerManifest)
	if err != nil {
		_ = writerManifest.flush()
		progress.finish(false)
		return err
	}
	if err := writerManifest.flush(); err != nil {
		progress.finish(false)
		return err
	}
	progress.finish(true)
	records := append([]FileRecord{}, recordsReused...)
	records = append(records, recordsDownloaded...)
	recordsComplete, err := buildCompleteRecords(fileManifest, dirVersion, records)
	if err != nil {
		return err
	}
	if err := writeManifest(fileManifest, source, recordsComplete, time.Now()); err != nil {
		return err
	}
	emit(trace, source, TraceEvent{Event: "write_manifest", Path: fileManifest, Status: fmt.Sprintf("files=%d", len(recordsComplete))})
	return nil
}

func ReadManifest(fileManifest string) (Manifest, bool, error) {
	var manifest Manifest
	ok, err := tomlx.ReadFileIfExists(fileManifest, &manifest)
	return manifest, ok, err
}

func validateSource(source Source) error {
	if err := validateSourceIdentity(source); err != nil {
		return err
	}
	if len(source.Assets) == 0 {
		return fmt.Errorf("assets must not be empty")
	}
	pathsSeen := make(map[string]struct{}, len(source.Assets))
	namesSeen := make(map[string]struct{}, len(source.Assets))
	for _, asset := range source.Assets {
		if strings.TrimSpace(asset.Name) == "" {
			return fmt.Errorf("asset name must not be empty")
		}
		if strings.TrimSpace(asset.URL) == "" {
			return fmt.Errorf("asset %q url must not be empty", asset.Name)
		}
		pathClean, err := cleanRelativePath(asset.Path)
		if err != nil {
			return fmt.Errorf("asset %q path: %w", asset.Name, err)
		}
		if !strings.HasPrefix(pathClean, "raw/") {
			return fmt.Errorf("asset %q path must start with raw/: %s", asset.Name, pathClean)
		}
		if _, ok := pathsSeen[pathClean]; ok {
			return fmt.Errorf("duplicate asset path: %s", pathClean)
		}
		pathsSeen[pathClean] = struct{}{}
		if _, ok := namesSeen[asset.Name]; ok {
			return fmt.Errorf("duplicate asset name: %s", asset.Name)
		}
		namesSeen[asset.Name] = struct{}{}
	}
	return nil
}

func validateSourceIdentity(source Source) error {
	if strings.TrimSpace(source.Database) == "" {
		return fmt.Errorf("database must not be empty")
	}
	if strings.TrimSpace(source.Asset) == "" {
		return fmt.Errorf("asset must not be empty")
	}
	if strings.TrimSpace(source.VersionToken) == "" {
		return fmt.Errorf("version_token must not be empty")
	}
	return nil
}

func validateOptions(options Options) error {
	if strings.TrimSpace(options.DirOut) == "" {
		return fmt.Errorf("output is required")
	}
	if options.RetryMax < 1 {
		return fmt.Errorf("max-attempts must be >= 1")
	}
	if options.RetryWait < 0 {
		return fmt.Errorf("retry-wait must be >= 0")
	}
	if options.WorkersMax < 1 {
		return fmt.Errorf("workers must be >= 1")
	}
	if options.RequestInterval < 0 {
		return fmt.Errorf("request-interval must be >= 0")
	}
	if options.RuleExisting != "skip" && options.RuleExisting != "overwrite" {
		return fmt.Errorf("on-existing must be one of: skip, overwrite")
	}
	return nil
}

func planFetch(
	assets []Asset,
	dirVersion string,
	overwrite bool,
	recordsExistingByPath map[string]FileRecord,
	trace TraceSink,
	source Source,
) ([]FileRecord, []downloadTask, error) {
	recordsReused := make([]FileRecord, 0, len(assets))
	tasksDownload := make([]downloadTask, 0, len(assets))
	for _, asset := range assets {
		recordExisting, ok := recordsExistingByPath[asset.Path]
		filePath := filepath.Join(dirVersion, filepath.FromSlash(asset.Path))
		reusable := ok && shouldReuseRecord(filePath, recordExisting)
		if reusable && asset.VerifyDownloadedFile != nil {
			if err := asset.VerifyDownloadedFile(filePath); err != nil {
				reusable = false
			}
		}
		if reusable && (!overwrite || asset.ReuseVerifiedExisting) {
			recordsReused = append(recordsReused, recordExisting)
			emit(trace, source, TraceEvent{Event: "reuse_file", Asset: recordExisting.Asset, Path: recordExisting.Path, URL: recordExisting.URL, Bytes: recordExisting.Bytes, SHA256: recordExisting.SHA256, Status: "sha256_match"})
			continue
		}
		tasksDownload = append(tasksDownload, downloadTask{asset: asset})
	}
	return recordsReused, tasksDownload, nil
}

func planSync(
	records []FileRecord,
	dirVersion string,
	overwrite bool,
	trace TraceSink,
	source Source,
) ([]FileRecord, []downloadTask, error) {
	recoverersByPath := buildRecoverersByPath(source.Assets)
	verifiersByPath := buildVerifiersByPath(source.Assets)
	recordsReused := make([]FileRecord, 0, len(records))
	tasksDownload := make([]downloadTask, 0, len(records))
	for _, record := range records {
		asset := Asset{Name: record.Asset, Path: record.Path, URL: record.URL}
		if recoverer := recoverersByPath[record.Path]; recoverer != nil {
			asset.RecoverDownloadError = recoverer
		}
		asset.VerifyDownloadedFile = composeVerifiers(
			verifiersByPath[record.Path],
			SHA256Verifier(record.SHA256),
		)
		if err := validateSyncAsset(asset); err != nil {
			return nil, nil, err
		}
		filePath := filepath.Join(dirVersion, filepath.FromSlash(record.Path))
		if !overwrite && shouldReuseRecord(filePath, record) {
			recordsReused = append(recordsReused, record)
			emit(trace, source, TraceEvent{Event: "reuse_file", Asset: record.Asset, Path: record.Path, URL: record.URL, Bytes: record.Bytes, SHA256: record.SHA256, Status: "sha256_match"})
			continue
		}
		tasksDownload = append(tasksDownload, downloadTask{asset: asset})
	}
	return recordsReused, tasksDownload, nil
}

func buildVerifiersByPath(assets []Asset) map[string]func(string) error {
	verifiers := make(map[string]func(string) error, len(assets))
	for _, asset := range assets {
		if asset.VerifyDownloadedFile != nil {
			verifiers[asset.Path] = asset.VerifyDownloadedFile
		}
	}
	return verifiers
}

func composeVerifiers(verifiers ...func(string) error) func(string) error {
	return func(path string) error {
		for _, verify := range verifiers {
			if verify == nil {
				continue
			}
			if err := verify(path); err != nil {
				return err
			}
		}
		return nil
	}
}

func SHA256Verifier(expected string) func(string) error {
	return func(path string) error {
		actual, err := calculateSHA256ForFile(path, nil)
		if err != nil {
			return err
		}
		if actual != expected {
			return fmt.Errorf("SHA256 mismatch: got %s, want %s", actual, expected)
		}
		return nil
	}
}

func buildRecoverersByPath(assets []Asset) map[string]func(string, error) (bool, error) {
	recoverers := make(map[string]func(string, error) (bool, error), len(assets))
	for _, asset := range assets {
		if asset.RecoverDownloadError != nil {
			recoverers[asset.Path] = asset.RecoverDownloadError
		}
	}
	return recoverers
}

func validateSyncAsset(asset Asset) error {
	if strings.TrimSpace(asset.Name) == "" {
		return fmt.Errorf("manifest asset name must not be empty")
	}
	if strings.TrimSpace(asset.URL) == "" {
		return fmt.Errorf("manifest asset %q url must not be empty", asset.Name)
	}
	pathClean, err := cleanRelativePath(asset.Path)
	if err != nil {
		return fmt.Errorf("manifest asset %q path: %w", asset.Name, err)
	}
	if !strings.HasPrefix(pathClean, "raw/") {
		return fmt.Errorf("manifest asset %q path must start with raw/: %s", asset.Name, pathClean)
	}
	return nil
}

func shouldReuseRecord(filePath string, record FileRecord) bool {
	infoFile, err := os.Stat(filePath)
	if err != nil || infoFile.IsDir() || infoFile.Size() != record.Bytes {
		return false
	}
	if record.SHA256 == "" {
		return false
	}
	sha256File, err := calculateSHA256ForFile(filePath, nil)
	if err != nil {
		return false
	}
	return sha256File == record.SHA256
}

func runDownloadTasks(
	clientHTTP *http.Client,
	source Source,
	dirVersion string,
	tasks []downloadTask,
	options Options,
	limiterRequest *httpx.RequestLimiter,
	trace TraceSink,
	progress *progressReporter,
	writerManifest *incrementalManifestWriter,
) ([]FileRecord, error) {
	return parallel.MapOrderedWithWorkers(tasks, options.WorkersMax, func(task downloadTask) (FileRecord, error) {
		fileOut := filepath.Join(dirVersion, filepath.FromSlash(task.asset.Path))
		if err := os.MkdirAll(filepath.Dir(fileOut), 0o755); err != nil {
			return FileRecord{}, fmt.Errorf("create dir for %s: %w", fileOut, err)
		}
		if err := downloadFileWithRetry(clientHTTP, task.asset, fileOut, options.RetryMax, options.RetryWait, limiterRequest, progress); err != nil {
			return FileRecord{}, err
		}
		record, err := buildRecord(fileOut, task.asset)
		if err != nil {
			return FileRecord{}, err
		}
		if writerManifest != nil {
			if err := writerManifest.add(record); err != nil {
				return FileRecord{}, err
			}
		}
		emit(trace, source, TraceEvent{Event: "download_file", Asset: record.Asset, Path: record.Path, URL: record.URL, Bytes: record.Bytes, SHA256: record.SHA256, Status: "ok"})
		return record, nil
	})
}

func downloadFileWithRetry(
	clientHTTP *http.Client,
	asset Asset,
	fileOut string,
	retryMax int,
	retryWait time.Duration,
	limiterRequest *httpx.RequestLimiter,
	progress *progressReporter,
) error {
	filePart := fileOut + ".part"
	var errLast error
	for attempt := 1; attempt <= retryMax; attempt++ {
		limiterRequest.Wait()
		progress.startFile(asset)
		if err := httpx.DownloadFileWithResume(clientHTTP, asset.URL, filePart, progress.callbackForFile(asset)); err == nil {
			if asset.VerifyDownloadedFile != nil {
				if err := asset.VerifyDownloadedFile(filePart); err != nil {
					if errRemove := os.Remove(filePart); errRemove != nil && !os.IsNotExist(errRemove) {
						progress.finishFile(asset, false)
						return fmt.Errorf("asset %q failed downloaded-file verification (%v) and remove failed part: %w", asset.Name, err, errRemove)
					}
					progress.finishFile(asset, false)
					return fmt.Errorf("asset %q failed downloaded-file verification: %w", asset.Name, err)
				}
			}
			if err := os.Rename(filePart, fileOut); err != nil {
				return fmt.Errorf("rename %s -> %s: %w", filePart, fileOut, err)
			}
			progress.finishFile(asset, true)
			return nil
		} else {
			errLast = err
			if asset.RecoverDownloadError != nil {
				_ = os.Remove(filePart)
				recovered, errRecover := asset.RecoverDownloadError(fileOut, err)
				if errRecover != nil {
					progress.finishFile(asset, false)
					return errRecover
				}
				if recovered {
					progress.finishFile(asset, true)
					return nil
				}
			}
			progress.finishFile(asset, false)
		}
		if attempt < retryMax && retryWait > 0 {
			time.Sleep(retryWait)
		}
	}
	return fmt.Errorf("download failed after %d attempts for %s: %w", retryMax, asset.URL, errLast)
}

func buildRecord(filePath string, asset Asset) (FileRecord, error) {
	return buildRecordWithProgress(filePath, asset, nil)
}

func buildRecordWithProgress(filePath string, asset Asset, progress *progressReporter) (FileRecord, error) {
	if asset.HashForLock != nil {
		sha256File, bytesFile, err := asset.HashForLock(filePath, progress.callbackForScanFile(asset))
		if err != nil {
			return FileRecord{}, err
		}
		return FileRecord{
			Asset:  asset.Name,
			Path:   asset.Path,
			SHA256: sha256File,
			Bytes:  bytesFile,
			URL:    asset.URL,
		}, nil
	}
	infoFile, err := os.Stat(filePath)
	if err != nil {
		return FileRecord{}, fmt.Errorf("stat file: %w", err)
	}
	sha256File, err := calculateSHA256ForFile(filePath, progress.callbackForScanFile(asset))
	if err != nil {
		return FileRecord{}, err
	}
	return FileRecord{
		Asset:  asset.Name,
		Path:   asset.Path,
		SHA256: sha256File,
		Bytes:  infoFile.Size(),
		URL:    asset.URL,
	}, nil
}

func scanRecords(dirVersion string, assetsByPath map[string]Asset, onlyDeclared bool, progress *progressReporter, workersMax int) ([]FileRecord, error) {
	tasks, err := planScanRecords(dirVersion, assetsByPath, onlyDeclared)
	if err != nil {
		return nil, err
	}
	progress.planScan(tasks)
	records, err := parallel.MapOrderedWithWorkers(tasks, workersMax, func(task scanTask) (FileRecord, error) {
		assetName := task.asset
		if assetName == "" {
			assetName = filepath.Base(task.filePath)
		}
		asset := Asset{Name: assetName, Path: task.pathRel, URL: task.url, HashForLock: task.hashForLock}
		progress.startScanFile(asset)
		if task.verifyFile != nil {
			if err := task.verifyFile(task.filePath); err != nil {
				progress.finishScanFile(asset, false)
				return FileRecord{}, fmt.Errorf("asset %q failed lock-file verification: %w", asset.Name, err)
			}
		}
		record, err := buildRecordWithProgress(task.filePath, asset, progress)
		if err != nil {
			progress.finishScanFile(asset, false)
			return FileRecord{}, err
		}
		progress.finishScanFile(asset, true)
		return record, nil
	})
	if err != nil {
		return nil, err
	}
	sortRecords(records)
	return records, nil
}

func planScanRecords(dirVersion string, assetsByPath map[string]Asset, onlyDeclared bool) ([]scanTask, error) {
	tasks := make([]scanTask, 0)
	dirScan := filepath.Join(dirVersion, "raw")
	if err := filepath.WalkDir(dirScan, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		if strings.HasSuffix(entry.Name(), ".part") {
			return nil
		}
		infoFile, err := entry.Info()
		if err != nil {
			return err
		}
		pathRel, err := filepath.Rel(dirVersion, path)
		if err != nil {
			return err
		}
		pathRel = filepath.ToSlash(pathRel)
		asset, declared := assetsByPath[pathRel]
		if onlyDeclared && !declared {
			return nil
		}
		tasks = append(tasks, scanTask{
			filePath:    path,
			pathRel:     pathRel,
			asset:       asset.Name,
			url:         asset.URL,
			bytes:       infoFile.Size(),
			hashForLock: asset.HashForLock,
			verifyFile:  asset.VerifyDownloadedFile,
		})
		return nil
	}); err != nil {
		if !os.IsNotExist(err) {
			return nil, fmt.Errorf("scan files: %w", err)
		}
	}
	sort.Slice(tasks, func(i, j int) bool {
		return tasks[i].pathRel < tasks[j].pathRel
	})
	return tasks, nil
}

func buildCompleteRecords(fileManifest string, dirVersion string, recordsCurrent []FileRecord) ([]FileRecord, error) {
	recordsExisting, err := readRecords(fileManifest)
	if err != nil {
		return nil, err
	}
	recordsMerged := make(map[string]FileRecord, len(recordsExisting)+len(recordsCurrent))
	for _, record := range recordsExisting {
		if !strings.HasPrefix(filepath.ToSlash(record.Path), "raw/") {
			return nil, fmt.Errorf("manifest file path must start with raw/: %s", record.Path)
		}
		recordsMerged[record.Path] = record
	}
	for _, record := range recordsCurrent {
		recordsMerged[record.Path] = record
	}
	records := make([]FileRecord, 0, len(recordsMerged))
	for _, record := range recordsMerged {
		filePath := filepath.Join(dirVersion, filepath.FromSlash(record.Path))
		infoFile, err := os.Stat(filePath)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, fmt.Errorf("stat manifest file %s: %w", filePath, err)
		}
		if !infoFile.IsDir() {
			records = append(records, record)
		}
	}
	sortRecords(records)
	return records, nil
}

func newIncrementalManifestWriter(fileManifest string, source Source, dirVersion string) (*incrementalManifestWriter, error) {
	recordsExisting, err := readRecords(fileManifest)
	if err != nil {
		return nil, err
	}
	recordsByPath := make(map[string]FileRecord, len(recordsExisting))
	for _, record := range recordsExisting {
		if strings.TrimSpace(record.Path) != "" {
			recordsByPath[record.Path] = record
		}
	}
	return &incrementalManifestWriter{
		fileManifest:  fileManifest,
		source:        source,
		dirVersion:    dirVersion,
		recordsByPath: recordsByPath,
		flushInterval: manifestFlushInterval,
	}, nil
}

func (writer *incrementalManifestWriter) add(record FileRecord) error {
	if writer == nil {
		return nil
	}
	writer.mutex.Lock()
	defer writer.mutex.Unlock()
	writer.recordsByPath[record.Path] = record
	writer.isDirty = true
	if !writer.shouldFlushLocked(time.Now()) {
		return nil
	}
	return writer.flushLocked(time.Now())
}

func (writer *incrementalManifestWriter) flush() error {
	if writer == nil {
		return nil
	}
	writer.mutex.Lock()
	defer writer.mutex.Unlock()
	if !writer.isDirty {
		return nil
	}
	return writer.flushLocked(time.Now())
}

func (writer *incrementalManifestWriter) shouldFlushLocked(now time.Time) bool {
	if writer.flushInterval <= 0 {
		return true
	}
	if writer.timeLastFlush.IsZero() {
		return true
	}
	return now.Sub(writer.timeLastFlush) >= writer.flushInterval
}

func (writer *incrementalManifestWriter) flushLocked(now time.Time) error {
	records, err := writer.buildRecordsLocked()
	if err != nil {
		return err
	}
	if err := writeManifest(writer.fileManifest, writer.source, records, now); err != nil {
		return err
	}
	writer.timeLastFlush = now
	writer.isDirty = false
	return nil
}

func (writer *incrementalManifestWriter) buildRecordsLocked() ([]FileRecord, error) {
	records := make([]FileRecord, 0, len(writer.recordsByPath))
	for pathRel, record := range writer.recordsByPath {
		filePath := filepath.Join(writer.dirVersion, filepath.FromSlash(pathRel))
		infoFile, err := os.Stat(filePath)
		if err != nil {
			if os.IsNotExist(err) {
				delete(writer.recordsByPath, pathRel)
				continue
			}
			return nil, fmt.Errorf("stat manifest file %s: %w", filePath, err)
		}
		if infoFile.IsDir() {
			delete(writer.recordsByPath, pathRel)
			continue
		}
		records = append(records, record)
	}
	sortRecords(records)
	return records, nil
}

func readRecordIndex(fileManifest string) (map[string]FileRecord, error) {
	records, err := readRecords(fileManifest)
	if err != nil {
		return nil, err
	}
	recordsByPath := make(map[string]FileRecord, len(records))
	for _, record := range records {
		if record.Path != "" {
			recordsByPath[record.Path] = record
		}
	}
	return recordsByPath, nil
}

func readRecords(fileManifest string) ([]FileRecord, error) {
	var manifest Manifest
	ok, err := tomlx.ReadFileIfExists(fileManifest, &manifest)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, nil
	}
	records := make([]FileRecord, 0, len(manifest.Files))
	for _, item := range manifest.Files {
		records = append(records, FileRecord{
			Asset:  item.Asset,
			Path:   item.Path,
			SHA256: item.SHA256,
			Bytes:  item.Bytes,
			URL:    item.URL,
		})
	}
	return records, nil
}

func buildLockAssetMap(fileManifest string, sourceAssets []Asset) (map[string]Asset, error) {
	records, err := readRecords(fileManifest)
	if err != nil {
		return nil, err
	}
	assets := make(map[string]Asset, len(records)+len(sourceAssets))
	for _, record := range records {
		if record.Path != "" {
			assets[record.Path] = Asset{Name: record.Asset, Path: record.Path, URL: record.URL}
		}
	}
	for _, asset := range sourceAssets {
		if asset.Path != "" {
			assets[asset.Path] = asset
		}
	}
	return assets, nil
}

func writeManifest(fileManifest string, source Source, records []FileRecord, timeDownloaded time.Time) error {
	files := make([]ManifestFileRecord, 0, len(records))
	for _, record := range records {
		files = append(files, ManifestFileRecord{
			Asset:  record.Asset,
			Path:   record.Path,
			SHA256: record.SHA256,
			Bytes:  record.Bytes,
			URL:    record.URL,
		})
	}
	return tomlx.WriteFileAtomic(fileManifest, Manifest{
		Database:     source.Database,
		Asset:        source.Asset,
		Source:       source.Source,
		Version:      source.Version,
		VersionToken: source.VersionToken,
		DownloadedAt: timeDownloaded.Format(time.RFC3339),
		Scope:        source.Scope,
		Files:        files,
	})
}

func calculateSHA256ForFile(filePath string, progress httpx.DownloadProgressFunc) (string, error) {
	fileIn, err := os.Open(filePath)
	if err != nil {
		return "", fmt.Errorf("open %s for sha256: %w", filePath, err)
	}
	defer fileIn.Close()
	var reader io.Reader = fileIn
	if progress != nil {
		infoFile, err := fileIn.Stat()
		if err != nil {
			return "", fmt.Errorf("stat %s for sha256: %w", filePath, err)
		}
		reader = &hashProgressReader{
			reader:     fileIn,
			bytesTotal: infoFile.Size(),
			progress:   progress,
		}
		progress(0, infoFile.Size())
	}
	digest, err := filehash.SHA256(reader)
	if err != nil {
		return "", fmt.Errorf("hash %s: %w", filePath, err)
	}
	return digest, nil
}

type hashProgressReader struct {
	reader     io.Reader
	bytesDone  int64
	bytesTotal int64
	progress   httpx.DownloadProgressFunc
}

func (reader *hashProgressReader) Read(buffer []byte) (int, error) {
	count, err := reader.reader.Read(buffer)
	if count > 0 {
		reader.bytesDone += int64(count)
		reader.progress(reader.bytesDone, reader.bytesTotal)
	}
	return count, err
}

func buildVersionDir(dirOut string, source Source) string {
	dirName := strings.TrimSpace(source.DirName)
	if dirName == "" {
		dirName = source.Asset
	}
	return filepath.Join(dirOut, dirName, source.VersionToken)
}

func ensureVersionDirs(dirVersion string) error {
	if err := os.MkdirAll(dirVersion, 0o755); err != nil {
		return fmt.Errorf("create version dir: %w", err)
	}
	return nil
}

func cleanRelativePath(value string) (string, error) {
	pathValue := filepath.ToSlash(strings.TrimSpace(value))
	if pathValue == "" {
		return "", fmt.Errorf("must not be empty")
	}
	if strings.HasPrefix(pathValue, "/") || filepath.IsAbs(pathValue) {
		return "", fmt.Errorf("must be relative")
	}
	for _, part := range strings.Split(pathValue, "/") {
		if part == ".." {
			return "", fmt.Errorf("must not contain ..")
		}
	}
	pathClean := filepath.ToSlash(filepath.Clean(pathValue))
	if pathClean == "." || pathClean == "" {
		return "", fmt.Errorf("must not be empty")
	}
	return pathClean, nil
}

func shouldOverwrite(options Options) bool {
	return options.RuleExisting == "overwrite"
}

func sortRecords(records []FileRecord) {
	sort.Slice(records, func(i, j int) bool {
		if records[i].Path != records[j].Path {
			return records[i].Path < records[j].Path
		}
		return records[i].Asset < records[j].Asset
	})
}

func newProgressReporter(source Source, options Options, totalFiles int, reusedFiles int) *progressReporter {
	if options.ShouldDisableProgress {
		return nil
	}
	writer := options.ProgressWriter
	if writer == nil {
		writer = os.Stderr
	}
	return &progressReporter{
		writer:      writer,
		label:       strings.TrimSpace(source.Database + " " + source.Asset),
		timeStarted: time.Now(),
		totalFiles:  totalFiles,
		doneFiles:   reusedFiles,
		fileBytes:   map[string]int64{},
		fileTotals:  map[string]int64{},
	}
}

func (progress *progressReporter) plan(reusedFiles int, downloadFiles int) {
	if progress == nil {
		return
	}
	progress.mutex.Lock()
	defer progress.mutex.Unlock()
	progress.downloadFiles = downloadFiles
	progress.drawLocked(fmt.Sprintf("reuse=%d download=%d", reusedFiles, downloadFiles), true)
}

func (progress *progressReporter) planScan(tasks []scanTask) {
	if progress == nil {
		return
	}
	progress.mutex.Lock()
	defer progress.mutex.Unlock()
	progress.totalFiles = len(tasks)
	progress.doneFiles = 0
	progress.downloadFiles = len(tasks)
	progress.totalBytes = 0
	progress.doneBytes = 0
	progress.knownBytes = false
	progress.fileBytes = map[string]int64{}
	progress.fileTotals = map[string]int64{}
	progress.currentPath = ""
	progress.currentBytes = 0
	progress.currentTotal = 0
	for _, task := range tasks {
		if task.bytes > 0 {
			progress.totalBytes += task.bytes
			progress.fileTotals[task.pathRel] = task.bytes
		}
	}
	progress.knownBytes = progress.totalBytes > 0
	progress.drawLocked(fmt.Sprintf("scanning %d files", len(tasks)), true)
}

func (progress *progressReporter) startFile(asset Asset) {
	if progress == nil {
		return
	}
	progress.mutex.Lock()
	defer progress.mutex.Unlock()
	progress.currentPath = asset.Path
	progress.currentBytes = 0
	progress.currentTotal = progress.fileTotals[asset.Path]
	progress.drawLocked("downloading "+asset.Path, false)
}

func (progress *progressReporter) startScanFile(asset Asset) {
	if progress == nil {
		return
	}
	progress.mutex.Lock()
	defer progress.mutex.Unlock()
	progress.currentPath = asset.Path
	progress.currentBytes = 0
	progress.currentTotal = progress.fileTotals[asset.Path]
	progress.drawLocked("hashing "+asset.Path, true)
}

func (progress *progressReporter) callbackForFile(asset Asset) httpx.DownloadProgressFunc {
	if progress == nil {
		return nil
	}
	return func(bytesDone int64, bytesTotal int64) {
		progress.updateFile(asset, bytesDone, bytesTotal)
	}
}

func (progress *progressReporter) callbackForScanFile(asset Asset) HashProgressFunc {
	if progress == nil {
		return nil
	}
	return func(bytesDone int64, bytesTotal int64) {
		progress.updateScanFile(asset, bytesDone, bytesTotal)
	}
}

func (progress *progressReporter) updateFile(asset Asset, bytesDone int64, bytesTotal int64) {
	if progress == nil {
		return
	}
	progress.mutex.Lock()
	defer progress.mutex.Unlock()
	progress.updateFileStateLocked(asset, bytesDone, bytesTotal)
	progress.drawLocked("downloading "+asset.Path, false)
}

func (progress *progressReporter) updateScanFile(asset Asset, bytesDone int64, bytesTotal int64) {
	if progress == nil {
		return
	}
	progress.mutex.Lock()
	defer progress.mutex.Unlock()
	progress.updateFileStateLocked(asset, bytesDone, bytesTotal)
	progress.drawLocked("hashing "+asset.Path, false)
}

func (progress *progressReporter) updateFileStateLocked(asset Asset, bytesDone int64, bytesTotal int64) {
	key := asset.Path
	if bytesTotal > 0 && progress.downloadFiles <= 1 {
		if _, ok := progress.fileTotals[key]; !ok {
			progress.fileTotals[key] = bytesTotal
			progress.totalBytes += bytesTotal
			progress.knownBytes = true
		}
	}
	if bytesTotal > 0 {
		progress.fileTotals[key] = bytesTotal
	}
	progress.currentPath = key
	progress.currentBytes = bytesDone
	progress.currentTotal = bytesTotal
	if progress.currentTotal == 0 {
		progress.currentTotal = progress.fileTotals[key]
	}
	previous := progress.fileBytes[key]
	if bytesDone > previous {
		progress.doneBytes += bytesDone - previous
		progress.fileBytes[key] = bytesDone
	}
}

func (progress *progressReporter) finishFile(asset Asset, ok bool) {
	if progress == nil {
		return
	}
	progress.mutex.Lock()
	defer progress.mutex.Unlock()
	progress.currentPath = asset.Path
	if total := progress.fileTotals[asset.Path]; total > 0 {
		progress.currentBytes = total
		progress.currentTotal = total
	}
	if ok {
		progress.doneFiles++
		progress.drawLocked("downloaded "+asset.Path, true)
		return
	}
	progress.drawLocked("retry "+asset.Path, true)
}

func (progress *progressReporter) finishScanFile(asset Asset, ok bool) {
	if progress == nil {
		return
	}
	progress.mutex.Lock()
	defer progress.mutex.Unlock()
	progress.currentPath = asset.Path
	if total := progress.fileTotals[asset.Path]; total > 0 {
		progress.currentBytes = total
		progress.currentTotal = total
	}
	if ok {
		progress.doneFiles++
		progress.drawLocked("hashed "+asset.Path, true)
		return
	}
	progress.drawLocked("hash failed "+asset.Path, true)
}

func (progress *progressReporter) finish(ok bool) {
	if progress == nil {
		return
	}
	progress.mutex.Lock()
	defer progress.mutex.Unlock()
	status := "completed"
	if !ok {
		status = "failed"
	}
	progress.drawLocked(status, true)
	_, _ = fmt.Fprintln(progress.writer)
}

func (progress *progressReporter) drawLocked(status string, force bool) {
	now := time.Now()
	if !force && !progress.timeLastDraw.IsZero() && now.Sub(progress.timeLastDraw) < 200*time.Millisecond {
		return
	}
	progress.timeLastDraw = now
	if progress.totalFiles > 1 && progress.currentPath != "" {
		progress.drawMultiFileLocked(status, now)
		return
	}
	if progress.knownBytes && progress.totalBytes > 0 {
		percent := float64(progress.doneBytes) / float64(progress.totalBytes)
		progress.writeLineLocked(fmt.Sprintf("%s  %s %3.0f%%  %s/%s  %s/s  %s",
			progress.label,
			renderProgressBar(percent),
			percent*100,
			formatBytes(progress.doneBytes),
			formatBytes(progress.totalBytes),
			formatBytes(progress.speedBytesPerSecond(now)),
			status,
		))
		return
	}
	if progress.totalFiles > 0 {
		percent := float64(progress.doneFiles) / float64(progress.totalFiles)
		progress.writeLineLocked(fmt.Sprintf("%s  %s %d/%d files  %s",
			progress.label,
			renderProgressBar(percent),
			progress.doneFiles,
			progress.totalFiles,
			status,
		))
		return
	}
	progress.writeLineLocked(fmt.Sprintf("%s  [downloading] %s  %s/s  %s",
		progress.label,
		formatBytes(progress.doneBytes),
		formatBytes(progress.speedBytesPerSecond(now)),
		status,
	))
}

func (progress *progressReporter) drawMultiFileLocked(status string, now time.Time) {
	percentFiles := float64(progress.doneFiles) / float64(progress.totalFiles)
	if progress.currentTotal > 0 {
		percentCurrent := float64(progress.currentBytes) / float64(progress.currentTotal)
		progress.writeLineLocked(fmt.Sprintf("%s  %s %d/%d files  current %s %3.0f%%  %s/%s  %s/s  %s",
			progress.label,
			renderProgressBar(percentFiles),
			progress.doneFiles,
			progress.totalFiles,
			renderProgressBar(percentCurrent),
			percentCurrent*100,
			formatBytes(progress.currentBytes),
			formatBytes(progress.currentTotal),
			formatBytes(progress.speedBytesPerSecond(now)),
			status,
		))
		return
	}
	progress.writeLineLocked(fmt.Sprintf("%s  %s %d/%d files  current %s  %s/s  %s",
		progress.label,
		renderProgressBar(percentFiles),
		progress.doneFiles,
		progress.totalFiles,
		formatBytes(progress.currentBytes),
		formatBytes(progress.speedBytesPerSecond(now)),
		status,
	))
}

func (progress *progressReporter) writeLineLocked(line string) {
	padding := progress.lastLineWidth - len(line)
	if padding < 0 {
		padding = 0
	}
	_, _ = fmt.Fprintf(progress.writer, "\r%s%s", line, strings.Repeat(" ", padding))
	progress.lastLineWidth = len(line)
}

func (progress *progressReporter) speedBytesPerSecond(now time.Time) int64 {
	elapsed := now.Sub(progress.timeStarted).Seconds()
	if elapsed <= 0 {
		return 0
	}
	return int64(float64(progress.doneBytes) / elapsed)
}

func renderProgressBar(percent float64) string {
	if percent < 0 {
		percent = 0
	}
	if percent > 1 {
		percent = 1
	}
	const width = 20
	filled := int(percent * width)
	if filled > width {
		filled = width
	}
	if filled == width {
		return "[" + strings.Repeat("=", width) + "]"
	}
	return "[" + strings.Repeat("=", filled) + ">" + strings.Repeat(".", width-filled-1) + "]"
}

func formatBytes(bytes int64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	value := float64(bytes)
	units := []string{"KiB", "MiB", "GiB", "TiB"}
	for _, unitName := range units {
		value /= unit
		if value < unit {
			return fmt.Sprintf("%.1f %s", value, unitName)
		}
	}
	return fmt.Sprintf("%.1f PiB", value/unit)
}

func emit(trace TraceSink, source Source, event TraceEvent) {
	if trace == nil {
		return
	}
	if event.Database == "" {
		event.Database = source.Database
	}
	if event.VersionToken == "" {
		event.VersionToken = source.VersionToken
	}
	trace.EmitStaticAssetTrace(event)
}
