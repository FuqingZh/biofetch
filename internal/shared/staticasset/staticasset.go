package staticasset

import (
	"biofetch/internal/shared/httpx"
	"biofetch/internal/shared/parallel"
	"biofetch/internal/shared/tomlx"
	"crypto/sha256"
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
	Name      string
	Path      string
	URL       string
	Transform func(string) error `toml:"-"`
}

type Source struct {
	Database     string
	Asset        string
	Source       string
	DirName      string
	ScanDirs     []string
	Version      string
	VersionToken string
	Scope        Scope
	Assets       []Asset
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
	mutex         sync.Mutex
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
	recordsDownloaded, err := runDownloadTasks(clientHTTP, source, dirVersion, tasksDownload, options, limiterRequest, trace, progress)
	if err != nil {
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

func Lock(source Source, options Options, trace TraceSink) error {
	if err := validateSourceIdentity(source); err != nil {
		return err
	}
	if err := validateOptions(options); err != nil {
		return err
	}
	dirVersion := buildVersionDir(options.DirOut, source)
	fileManifest := filepath.Join(dirVersion, "manifest.lock")
	urlsExisting, err := buildExistingURLMap(fileManifest)
	if err != nil {
		return err
	}
	records, err := scanRecords(dirVersion, source.ScanDirs, urlsExisting)
	if err != nil {
		return err
	}
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

func Sync(source Source, options Options, trace TraceSink) error {
	if err := validateSourceIdentity(source); err != nil {
		return err
	}
	if err := validateOptions(options); err != nil {
		return err
	}
	dirVersion := buildVersionDir(options.DirOut, source)
	fileManifest := filepath.Join(dirVersion, "manifest.lock")
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
	recordsDownloaded, err := runDownloadTasks(clientHTTP, source, dirVersion, tasksDownload, options, limiterRequest, trace, progress)
	if err != nil {
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
		return fmt.Errorf("dir_out is required")
	}
	if options.RetryMax < 1 {
		return fmt.Errorf("retry_max must be >= 1")
	}
	if options.RetryWait < 0 {
		return fmt.Errorf("retry_wait_sec must be >= 0")
	}
	if options.WorkersMax < 1 {
		return fmt.Errorf("workers_max must be >= 1")
	}
	if options.RequestInterval < 0 {
		return fmt.Errorf("request_interval_ms must be >= 0")
	}
	if options.RuleExisting != "skip" && options.RuleExisting != "overwrite" {
		return fmt.Errorf("rule_existing must be one of: skip, overwrite")
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
		if !overwrite && ok && shouldReuseRecord(filePath, recordExisting) {
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
	transformsByPath := buildTransformsByPath(source.Assets)
	recordsReused := make([]FileRecord, 0, len(records))
	tasksDownload := make([]downloadTask, 0, len(records))
	for _, record := range records {
		asset := Asset{Name: record.Asset, Path: record.Path, URL: record.URL}
		if transform := transformsByPath[record.Path]; transform != nil {
			asset.Transform = transform
		}
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

func buildTransformsByPath(assets []Asset) map[string]func(string) error {
	transforms := make(map[string]func(string) error, len(assets))
	for _, asset := range assets {
		if asset.Transform != nil {
			transforms[asset.Path] = asset.Transform
		}
	}
	return transforms
}

func validateSyncAsset(asset Asset) error {
	if strings.TrimSpace(asset.Name) == "" {
		return fmt.Errorf("manifest asset name must not be empty")
	}
	if strings.TrimSpace(asset.URL) == "" {
		return fmt.Errorf("manifest asset %q url must not be empty", asset.Name)
	}
	if _, err := cleanRelativePath(asset.Path); err != nil {
		return fmt.Errorf("manifest asset %q path: %w", asset.Name, err)
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
	sha256File, err := calculateSHA256ForFile(filePath)
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
) ([]FileRecord, error) {
	return parallel.MapOrderedWithWorkers(tasks, options.WorkersMax, func(task downloadTask) (FileRecord, error) {
		fileOut := filepath.Join(dirVersion, filepath.FromSlash(task.asset.Path))
		if err := os.MkdirAll(filepath.Dir(fileOut), 0o755); err != nil {
			return FileRecord{}, fmt.Errorf("create dir for %s: %w", fileOut, err)
		}
		if err := downloadFileWithRetry(clientHTTP, task.asset, fileOut, options.RetryMax, options.RetryWait, limiterRequest, progress); err != nil {
			return FileRecord{}, err
		}
		if task.asset.Transform != nil {
			if err := task.asset.Transform(fileOut); err != nil {
				return FileRecord{}, fmt.Errorf("transform %s: %w", fileOut, err)
			}
		}
		record, err := buildRecord(fileOut, task.asset)
		if err != nil {
			return FileRecord{}, err
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
		if err := httpx.DownloadFileWithProgress(clientHTTP, asset.URL, filePart, progress.callbackForFile(asset)); err == nil {
			if err := os.Rename(filePart, fileOut); err != nil {
				return fmt.Errorf("rename %s -> %s: %w", filePart, fileOut, err)
			}
			progress.finishFile(asset, true)
			return nil
		} else {
			progress.finishFile(asset, false)
			errLast = err
		}
		if attempt < retryMax && retryWait > 0 {
			time.Sleep(retryWait)
		}
	}
	return fmt.Errorf("download failed after %d attempts for %s: %w", retryMax, asset.URL, errLast)
}

func buildRecord(filePath string, asset Asset) (FileRecord, error) {
	infoFile, err := os.Stat(filePath)
	if err != nil {
		return FileRecord{}, fmt.Errorf("stat file: %w", err)
	}
	sha256File, err := calculateSHA256ForFile(filePath)
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

func scanRecords(dirVersion string, scanDirs []string, urlsExisting map[string]string) ([]FileRecord, error) {
	if len(scanDirs) == 0 {
		scanDirs = []string{"raw"}
	}
	records := make([]FileRecord, 0)
	for _, dirName := range scanDirs {
		dirScan := filepath.Join(dirVersion, dirName)
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
			pathRel, err := filepath.Rel(dirVersion, path)
			if err != nil {
				return err
			}
			pathRel = filepath.ToSlash(pathRel)
			urlAsset := urlsExisting[pathRel]
			record, err := buildRecord(path, Asset{Name: filepath.Base(path), Path: pathRel, URL: urlAsset})
			if err != nil {
				return err
			}
			records = append(records, record)
			return nil
		}); err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, fmt.Errorf("scan files: %w", err)
		}
	}
	sortRecords(records)
	return records, nil
}

func buildCompleteRecords(fileManifest string, dirVersion string, recordsCurrent []FileRecord) ([]FileRecord, error) {
	recordsExisting, err := readRecords(fileManifest)
	if err != nil {
		return nil, err
	}
	recordsMerged := make(map[string]FileRecord, len(recordsExisting)+len(recordsCurrent))
	for _, record := range recordsExisting {
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

func buildExistingURLMap(fileManifest string) (map[string]string, error) {
	records, err := readRecords(fileManifest)
	if err != nil {
		return nil, err
	}
	urls := make(map[string]string, len(records))
	for _, record := range records {
		if record.Path != "" && record.URL != "" {
			urls[record.Path] = record.URL
		}
	}
	return urls, nil
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

func calculateSHA256ForFile(filePath string) (string, error) {
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

func buildVersionDir(dirOut string, source Source) string {
	dirName := strings.TrimSpace(source.DirName)
	if dirName == "" {
		dirName = source.Asset
	}
	return filepath.Join(dirOut, dirName, source.VersionToken)
}

func ensureVersionDirs(dirVersion string) error {
	if err := os.MkdirAll(filepath.Join(dirVersion, "raw"), 0o755); err != nil {
		return fmt.Errorf("create raw dir: %w", err)
	}
	if err := os.MkdirAll(filepath.Join(dirVersion, "tidy"), 0o755); err != nil {
		return fmt.Errorf("create tidy dir: %w", err)
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

func (progress *progressReporter) startFile(asset Asset) {
	if progress == nil {
		return
	}
	progress.mutex.Lock()
	defer progress.mutex.Unlock()
	progress.drawLocked("downloading "+asset.Path, false)
}

func (progress *progressReporter) callbackForFile(asset Asset) httpx.DownloadProgressFunc {
	if progress == nil {
		return nil
	}
	return func(bytesDone int64, bytesTotal int64) {
		progress.updateFile(asset, bytesDone, bytesTotal)
	}
}

func (progress *progressReporter) updateFile(asset Asset, bytesDone int64, bytesTotal int64) {
	if progress == nil {
		return
	}
	progress.mutex.Lock()
	defer progress.mutex.Unlock()
	key := asset.Path
	if bytesTotal > 0 && progress.downloadFiles <= 1 {
		if _, ok := progress.fileTotals[key]; !ok {
			progress.fileTotals[key] = bytesTotal
			progress.totalBytes += bytesTotal
			progress.knownBytes = true
		}
	}
	previous := progress.fileBytes[key]
	if bytesDone > previous {
		progress.doneBytes += bytesDone - previous
		progress.fileBytes[key] = bytesDone
	}
	progress.drawLocked("downloading "+asset.Path, false)
}

func (progress *progressReporter) finishFile(asset Asset, ok bool) {
	if progress == nil {
		return
	}
	progress.mutex.Lock()
	defer progress.mutex.Unlock()
	if ok {
		progress.doneFiles++
		progress.drawLocked("downloaded "+asset.Path, true)
		return
	}
	progress.drawLocked("retry "+asset.Path, true)
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
	if progress.knownBytes && progress.totalBytes > 0 {
		percent := float64(progress.doneBytes) / float64(progress.totalBytes)
		_, _ = fmt.Fprintf(progress.writer, "\r%s  %s %3.0f%%  %s/%s  %s/s  %s",
			progress.label,
			renderProgressBar(percent),
			percent*100,
			formatBytes(progress.doneBytes),
			formatBytes(progress.totalBytes),
			formatBytes(progress.speedBytesPerSecond(now)),
			status,
		)
		return
	}
	if progress.totalFiles > 0 {
		percent := float64(progress.doneFiles) / float64(progress.totalFiles)
		_, _ = fmt.Fprintf(progress.writer, "\r%s  %s %d/%d files  %s",
			progress.label,
			renderProgressBar(percent),
			progress.doneFiles,
			progress.totalFiles,
			status,
		)
		return
	}
	_, _ = fmt.Fprintf(progress.writer, "\r%s  [downloading] %s  %s/s  %s",
		progress.label,
		formatBytes(progress.doneBytes),
		formatBytes(progress.speedBytesPerSecond(now)),
		status,
	)
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
