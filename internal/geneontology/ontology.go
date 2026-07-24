package geneontology

import (
	"biofetch/internal/shared/cliopt"
	"biofetch/internal/shared/httpx"
	"biofetch/internal/shared/logx"
	"biofetch/internal/shared/parallel"
	"biofetch/internal/shared/sets"
	"biofetch/internal/shared/tomlx"
	"bytes"
	"crypto/sha256"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"golang.org/x/net/html"
)

type ontologyAsset struct {
	name string
	url  string
}

type ontologyRecord struct {
	Asset   string
	PathRel string
	SHA256  string
	Bytes   int64
	URL     string
}

type ontologyFileState struct {
	Bytes int64
}

type ontologyDownloadTask struct {
	asset      ontologyAsset
	fileOut    string
	pathRel    string
	textAction string
}

type ontologySource struct {
	version      string
	versionToken string
	baseURL      string
}

type ontologyManifestFile struct {
	Database     string                     `toml:"database"`
	Asset        string                     `toml:"asset"`
	Version      string                     `toml:"version"`
	VersionToken string                     `toml:"version_token"`
	DownloadedAt string                     `toml:"downloaded_at"`
	Files        []ontologyManifestFileItem `toml:"files"`
}

type ontologyManifestFileItem struct {
	Asset  string `toml:"asset"`
	Path   string `toml:"path"`
	SHA256 string `toml:"sha256"`
	Bytes  int64  `toml:"bytes"`
	URL    string `toml:"url"`
}

const ontologyArchiveRootURL = "https://release.geneontology.org/"
const ontologyArchiveDocsURL = "https://geneontology.org/docs/download-ontology/"
const ontologyVersionAssetName = "go-basic.obo"

var ontologyCurrentBaseURL = "https://current.geneontology.org/ontology/"

func runFetchOntology(cfg *ontologyConfig, readerConfirm io.Reader, writerConfirm io.Writer) error {
	clientHTTP := httpx.NewClient(cfg.ShouldAllowInsecureTLS)
	limiterRequest := httpx.NewRequestLimiter(cfg.RequestInterval)
	source, err := resolveOntologySource(clientHTTP, cfg.VersionToken, limiterRequest)
	if err != nil {
		return err
	}
	assetsAvailable, err := discoverOntologyAssets(clientHTTP, source.baseURL, limiterRequest)
	if err != nil {
		return err
	}
	assets, err := resolveOntologyAssets(assetsAvailable, cfg.assetNames)
	if err != nil {
		return err
	}
	cfg.version = source.version
	cfg.VersionToken = source.versionToken

	dirVersion := filepath.Join(cfg.DirOut, "ontology", cfg.VersionToken)
	_, closeRun, err := logx.StartVersionedRun("biofetch go", "fetch", cfg.DirLogs, dirVersion)
	if err != nil {
		return err
	}
	defer closeRun()
	dirRaw := filepath.Join(dirVersion, "raw")
	fileManifest := filepath.Join(dirVersion, "manifest.lock")

	if cfg.ShouldDryRun {
		logf("[dry-run] version dir: %s", dirVersion)
		logf("[dry-run] manifest: %s", fileManifest)
	} else {
		if err := os.MkdirAll(dirRaw, 0o755); err != nil {
			return fmt.Errorf("create raw dir: %w", err)
		}
	}

	recordsManifestByPath := map[string]ontologyRecord{}
	filesCurrentByPath := map[string]ontologyFileState{}
	if !cfg.ShouldOverwriteExisting {
		var err error
		recordsManifestByPath, err = buildOntologyRecordIndex(fileManifest)
		if err != nil {
			return err
		}
		filesCurrentByPath, err = scanOntologyRawFileStateIndex(dirRaw)
		if err != nil {
			return err
		}
	}

	if cfg.ShouldDryRun {
		for _, asset := range assets {
			fileOut := filepath.Join(dirRaw, asset.name)
			logf("[dry-run] %s -> %s", asset.url, fileOut)
		}
		logf("[dry-run] done (assets=%d)", len(assets))
		return nil
	}

	recordsReused, tasksDownload, err := planFetchOntologyTasks(
		assets,
		dirRaw,
		cfg.ShouldOverwriteExisting,
		recordsManifestByPath,
		filesCurrentByPath,
	)
	if err != nil {
		return err
	}
	for _, record := range recordsReused {
		logf("using existing %s", record.Asset)
	}

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

	records := make([]ontologyRecord, 0, len(recordsReused)+len(recordsDownloaded))
	records = append(records, recordsReused...)
	records = append(records, recordsDownloaded...)

	sort.Slice(records, func(i, j int) bool {
		return records[i].Asset < records[j].Asset
	})

	recordsComplete, err := buildCompleteOntologyRecords(fileManifest, dirVersion, records)
	if err != nil {
		return err
	}

	if err := tomlx.WriteFileAtomic(fileManifest, buildOntologyManifestFile(cfg, recordsComplete, time.Now())); err != nil {
		return err
	}

	logf("done (files=%d)", len(recordsComplete))
	logf("manifest written: %s", fileManifest)
	return nil
}

func discoverOntologyAssets(
	clientHTTP *http.Client,
	baseURL string,
	limiterRequest *httpx.RequestLimiter,
) ([]ontologyAsset, error) {
	data, err := downloadText(clientHTTP, buildOntologyIndexURL(baseURL), limiterRequest)
	if err != nil {
		return nil, err
	}
	return parseOntologyAssetsFromIndex(data, baseURL)
}

func parseOntologyAssetsFromIndex(data []byte, baseURL string) ([]ontologyAsset, error) {
	document, err := html.Parse(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("parse ontology index html: %w", err)
	}

	assetsByName := make(map[string]ontologyAsset)
	for _, name := range extractOntologyAnchorTargets(document) {
		if shouldIncludeOntologyAsset(name) {
			assetsByName[name] = ontologyAsset{
				name: name,
				url:  buildOntologyAssetURL(baseURL, name),
			}
		}
	}
	if len(assetsByName) == 0 {
		return nil, fmt.Errorf("no ontology assets found at %s", buildOntologyIndexURL(baseURL))
	}

	names := make([]string, 0, len(assetsByName))
	for name := range assetsByName {
		names = append(names, name)
	}
	sort.Strings(names)

	assets := make([]ontologyAsset, 0, len(names))
	for _, name := range names {
		assets = append(assets, assetsByName[name])
	}
	return assets, nil
}

func extractOntologyAnchorTargets(root *html.Node) []string {
	setTargets := make(map[string]struct{})
	visitOntologyAnchorNodes(root, setTargets)

	targets := make([]string, 0, len(setTargets))
	targets = append(targets, sets.SortedKeys(setTargets)...)
	return targets
}

func visitOntologyAnchorNodes(node *html.Node, setTargets map[string]struct{}) {
	if node == nil {
		return
	}
	if node.Type == html.ElementNode && node.Data == "a" {
		for _, target := range collectOntologyAnchorTargets(node) {
			setTargets[target] = struct{}{}
		}
	}
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		visitOntologyAnchorNodes(child, setTargets)
	}
}

func collectOntologyAnchorTargets(node *html.Node) []string {
	targets := make([]string, 0, 2)

	for _, attr := range node.Attr {
		if attr.Key == "href" {
			value := strings.TrimSpace(attr.Val)
			if value != "" {
				targets = append(targets, value)
			}
			break
		}
	}

	text := strings.TrimSpace(extractNodeText(node))
	if text != "" {
		targets = append(targets, text)
	}

	return targets
}

func extractNodeText(node *html.Node) string {
	if node == nil {
		return ""
	}
	if node.Type == html.TextNode {
		return node.Data
	}

	var builder strings.Builder
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		builder.WriteString(extractNodeText(child))
	}
	return builder.String()
}

func shouldIncludeOntologyAsset(name string) bool {
	if name == "" || name == ".." || name == "index.html" {
		return false
	}
	if strings.HasSuffix(name, "/") {
		return false
	}
	if strings.Contains(name, "/") {
		return false
	}
	return strings.Contains(name, ".")
}

func resolveOntologyAssets(
	assetsAvailable []ontologyAsset,
	assetNames []string,
) ([]ontologyAsset, error) {
	if len(assetNames) == 0 {
		return assetsAvailable, nil
	}

	namesRequested, err := parseOntologyAssetNames(assetNames)
	if err != nil {
		return nil, err
	}

	assetsByName := make(map[string]ontologyAsset, len(assetsAvailable))
	for _, asset := range assetsAvailable {
		assetsByName[asset.name] = asset
	}

	assets := make([]ontologyAsset, 0, len(namesRequested))
	namesUnknown := make([]string, 0)
	for _, name := range namesRequested {
		asset, ok := assetsByName[name]
		if !ok {
			namesUnknown = append(namesUnknown, name)
			continue
		}
		assets = append(assets, asset)
	}
	if len(namesUnknown) > 0 {
		sort.Strings(namesUnknown)
		return nil, fmt.Errorf("unknown ontology asset(s): %s", strings.Join(namesUnknown, ", "))
	}
	return assets, nil
}

func parseOntologyAssetNames(values []string) ([]string, error) {
	valuesResolved, err := cliopt.ExpandListTokens(values, "", "assets")
	if err != nil {
		return nil, err
	}
	setAssets := make(map[string]struct{}, len(valuesResolved))
	for _, value := range valuesResolved {
		name := strings.TrimSpace(value)
		if name == "" {
			continue
		}
		setAssets[name] = struct{}{}
	}
	if len(setAssets) == 0 {
		return nil, fmt.Errorf("assets must not be empty")
	}

	return sets.SortedKeys(setAssets), nil
}

func resolveExistingOntologyFetchRecord(
	filePath string,
	pathRel string,
	asset ontologyAsset,
	recordsManifestByPath map[string]ontologyRecord,
	filesCurrentByPath map[string]ontologyFileState,
) (ontologyRecord, bool, error) {
	stateFile, ok := filesCurrentByPath[pathRel]
	if !ok || stateFile.Bytes <= 0 {
		return ontologyRecord{}, false, nil
	}

	recordManifest, ok := recordsManifestByPath[pathRel]
	if ok {
		if recordManifest.Bytes == stateFile.Bytes {
			return recordManifest, true, nil
		}
		return ontologyRecord{}, false, nil
	}

	record, err := buildOntologyRecord(filePath, pathRel, asset)
	if err != nil {
		return ontologyRecord{}, false, err
	}
	return record, true, nil
}

func planFetchOntologyTasks(
	assets []ontologyAsset,
	dirRaw string,
	shouldOverwriteExisting bool,
	recordsManifestByPath map[string]ontologyRecord,
	filesCurrentByPath map[string]ontologyFileState,
) ([]ontologyRecord, []ontologyDownloadTask, error) {
	recordsReused := make([]ontologyRecord, 0, len(assets))
	tasksDownload := make([]ontologyDownloadTask, 0, len(assets))

	for _, asset := range assets {
		fileOut := filepath.Join(dirRaw, asset.name)
		pathRel := filepath.ToSlash(filepath.Join("raw", asset.name))

		if !shouldOverwriteExisting {
			recordExisting, ok, err := resolveExistingOntologyFetchRecord(
				fileOut,
				pathRel,
				asset,
				recordsManifestByPath,
				filesCurrentByPath,
			)
			if err != nil {
				return nil, nil, err
			}
			if ok {
				recordsReused = append(recordsReused, recordExisting)
				continue
			}
		}

		tasksDownload = append(tasksDownload, ontologyDownloadTask{
			asset:      asset,
			fileOut:    fileOut,
			pathRel:    pathRel,
			textAction: fmt.Sprintf("downloading %s", asset.name),
		})
	}

	return recordsReused, tasksDownload, nil
}

func planSyncOntologyTasks(
	dirVersion string,
	recordsManifest []ontologyRecord,
	shouldOverwriteExisting bool,
	filesCurrentByPath map[string]ontologyFileState,
) ([]ontologyRecord, []ontologyDownloadTask) {
	recordsReused := make([]ontologyRecord, 0, len(recordsManifest))
	tasksDownload := make([]ontologyDownloadTask, 0, len(recordsManifest))

	for _, record := range recordsManifest {
		fileOut := filepath.Join(dirVersion, filepath.FromSlash(record.PathRel))
		if !shouldOverwriteExisting && shouldReuseOntologySyncRecord(record, filesCurrentByPath) {
			recordsReused = append(recordsReused, record)
			continue
		}

		tasksDownload = append(tasksDownload, ontologyDownloadTask{
			asset:      ontologyAsset{name: record.Asset, url: record.URL},
			fileOut:    fileOut,
			pathRel:    record.PathRel,
			textAction: fmt.Sprintf("restore downloading %s", filepath.Base(fileOut)),
		})
	}

	return recordsReused, tasksDownload
}

func shouldReuseOntologySyncRecord(
	record ontologyRecord,
	filesCurrentByPath map[string]ontologyFileState,
) bool {
	stateFile, ok := filesCurrentByPath[record.PathRel]
	if !ok || stateFile.Bytes <= 0 {
		return false
	}
	return stateFile.Bytes == record.Bytes
}

func runOntologyDownloadTasks(
	clientHTTP *http.Client,
	tasksDownload []ontologyDownloadTask,
	retryMax int,
	retryWait time.Duration,
	workersMax int,
	limiterRequest *httpx.RequestLimiter,
) ([]ontologyRecord, error) {
	return parallel.MapOrderedWithWorkers(
		tasksDownload,
		workersMax,
		func(task ontologyDownloadTask) (ontologyRecord, error) {
			logf("%s", task.textAction)
			if err := os.MkdirAll(filepath.Dir(task.fileOut), 0o755); err != nil {
				return ontologyRecord{}, fmt.Errorf("create dir for %s: %w", task.fileOut, err)
			}
			if err := downloadFileWithRetry(
				clientHTTP,
				task.asset.url,
				task.fileOut,
				retryMax,
				retryWait,
				limiterRequest,
			); err != nil {
				return ontologyRecord{}, err
			}
			return buildOntologyRecord(task.fileOut, task.pathRel, task.asset)
		},
	)
}

func buildOntologyRecord(
	filePath string,
	pathRel string,
	asset ontologyAsset,
) (ontologyRecord, error) {
	infoFile, err := os.Stat(filePath)
	if err != nil {
		return ontologyRecord{}, fmt.Errorf("stat file: %w", err)
	}
	sha256File, err := calculateSHA256ForFile(filePath)
	if err != nil {
		return ontologyRecord{}, err
	}
	return ontologyRecord{
		Asset:   asset.name,
		PathRel: pathRel,
		SHA256:  sha256File,
		Bytes:   infoFile.Size(),
		URL:     asset.url,
	}, nil
}

func downloadFileWithRetry(
	clientHTTP *http.Client,
	urlFile string,
	fileOut string,
	retryMax int,
	retryWait time.Duration,
	limiterRequest *httpx.RequestLimiter,
) error {
	filePart := fileOut + ".part"
	var errLast error

	for attempt := 1; attempt <= retryMax; attempt++ {
		limiterRequest.Wait()
		if err := httpx.DownloadFile(clientHTTP, urlFile, filePart); err == nil {
			if err := os.Rename(filePart, fileOut); err != nil {
				return fmt.Errorf("rename %s -> %s: %w", filePart, fileOut, err)
			}
			return nil
		} else {
			errLast = err
			logf("download failed (attempt %d/%d): %s", attempt, retryMax, err)
		}

		if attempt < retryMax && retryWait > 0 {
			time.Sleep(retryWait)
		}
	}

	return fmt.Errorf("download failed after %d attempts for %s: %w", retryMax, urlFile, errLast)
}

func buildCompleteOntologyRecords(
	fileManifest string,
	dirVersion string,
	recordsCurrent []ontologyRecord,
) ([]ontologyRecord, error) {
	recordsExisting, err := readExistingOntologyRecords(fileManifest)
	if err != nil {
		return nil, err
	}

	recordsMerged := make(map[string]ontologyRecord, len(recordsExisting)+len(recordsCurrent))
	for _, record := range recordsExisting {
		recordsMerged[record.PathRel] = record
	}
	for _, record := range recordsCurrent {
		recordsMerged[record.PathRel] = record
	}

	records := make([]ontologyRecord, 0, len(recordsMerged))
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

	sort.Slice(records, func(i, j int) bool {
		return records[i].Asset < records[j].Asset
	})
	return records, nil
}

func readExistingOntologyRecords(fileManifest string) ([]ontologyRecord, error) {
	var manifest ontologyManifestFile
	ok, err := tomlx.ReadFileIfExists(fileManifest, &manifest)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, nil
	}

	records := make([]ontologyRecord, 0, len(manifest.Files))
	for _, item := range manifest.Files {
		records = append(records, ontologyRecord{
			Asset:   item.Asset,
			PathRel: item.Path,
			SHA256:  item.SHA256,
			Bytes:   item.Bytes,
			URL:     item.URL,
		})
	}
	return records, nil
}

func buildOntologyRecordIndex(fileManifest string) (map[string]ontologyRecord, error) {
	records, err := readExistingOntologyRecords(fileManifest)
	if err != nil {
		return nil, err
	}

	recordsByPath := make(map[string]ontologyRecord, len(records))
	for _, record := range records {
		if record.PathRel == "" {
			continue
		}
		recordsByPath[record.PathRel] = record
	}
	return recordsByPath, nil
}

func buildOntologyManifestFile(
	cfg *ontologyConfig,
	records []ontologyRecord,
	timeDownloaded time.Time,
) ontologyManifestFile {
	files := make([]ontologyManifestFileItem, 0, len(records))
	for _, record := range records {
		files = append(files, ontologyManifestFileItem{
			Asset:  record.Asset,
			Path:   record.PathRel,
			SHA256: record.SHA256,
			Bytes:  record.Bytes,
			URL:    record.URL,
		})
	}

	return ontologyManifestFile{
		Database:     "go",
		Asset:        "ontology",
		Version:      cfg.version,
		VersionToken: cfg.VersionToken,
		DownloadedAt: timeDownloaded.Format(time.RFC3339),
		Files:        files,
	}
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

func scanOntologyRawFileStateIndex(dirRaw string) (map[string]ontologyFileState, error) {
	entries, err := os.ReadDir(dirRaw)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]ontologyFileState{}, nil
		}
		return nil, fmt.Errorf("read raw dir: %w", err)
	}

	filesByPath := make(map[string]ontologyFileState, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		infoEntry, err := entry.Info()
		if err != nil {
			return nil, fmt.Errorf("stat raw file %s: %w", entry.Name(), err)
		}
		pathRel := filepath.ToSlash(filepath.Join("raw", entry.Name()))
		filesByPath[pathRel] = ontologyFileState{Bytes: infoEntry.Size()}
	}
	return filesByPath, nil
}

func logf(format string, args ...any) {
	logx.Logf("biofetch go", format, args...)
}

func resolveOntologySource(
	clientHTTP *http.Client,
	versionToken string,
	limiterRequest *httpx.RequestLimiter,
) (ontologySource, error) {

	if versionToken := strings.TrimSpace(versionToken); versionToken == "" {
		version, err := resolveOntologyVersion(clientHTTP, ontologyCurrentBaseURL, limiterRequest)
		if err != nil {
			return ontologySource{}, err
		}
		return ontologySource{
			version:      version,
			versionToken: version,
			baseURL:      ontologyCurrentBaseURL,
		}, nil
	}

	if err := validateOptionalOntologyVersionToken(versionToken); err != nil {
		return ontologySource{}, err
	}

	baseURL := buildOntologyReleaseBaseURL(versionToken)
	version, err := resolveOntologyVersion(clientHTTP, baseURL, limiterRequest)
	if err != nil {
		return ontologySource{}, fmt.Errorf(
			"GO release %q not found or unreadable at %s: %w (see %s)",
			versionToken,
			baseURL,
			err,
			ontologyArchiveRootURL,
		)
	}
	if version != versionToken {
		return ontologySource{}, fmt.Errorf(
			"GO release %q resolved to %q at %s (see %s)",
			versionToken,
			version,
			baseURL,
			ontologyArchiveRootURL,
		)
	}
	return ontologySource{
		version:      version,
		versionToken: versionToken,
		baseURL:      baseURL,
	}, nil
}

func resolveOntologyVersion(
	clientHTTP *http.Client,
	baseURL string,
	limiterRequest *httpx.RequestLimiter,
) (string, error) {
	data, err := downloadText(
		clientHTTP,
		buildOntologyAssetURL(baseURL, ontologyVersionAssetName),
		limiterRequest,
	)
	if err != nil {
		return "", err
	}
	version, err := parseOntologyVersionFromOBO(data)
	if err != nil {
		return "", err
	}
	return version, nil
}

func downloadText(
	clientHTTP *http.Client,
	urlFile string,
	limiterRequest *httpx.RequestLimiter,
) ([]byte, error) {
	limiterRequest.Wait()
	response, err := clientHTTP.Get(urlFile)
	if err != nil {
		return nil, fmt.Errorf("request %s: %w", urlFile, err)
	}
	defer response.Body.Close()

	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, fmt.Errorf("request %s: unexpected status %s", urlFile, response.Status)
	}

	data, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", urlFile, err)
	}
	return data, nil
}

func parseOntologyVersionFromOBO(data []byte) (string, error) {
	scanner := strings.NewReader(string(data))
	for {
		line, err := readNextLine(scanner)
		if err != nil {
			if err == io.EOF {
				break
			}
			return "", err
		}
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "data-version:") {
			continue
		}
		textVersion := strings.TrimSpace(strings.TrimPrefix(line, "data-version:"))
		if textVersion == "" {
			break
		}
		textDate := extractDateToken(textVersion)
		if textDate != "" {
			return textDate, nil
		}
		return textVersion, nil
	}
	return "", fmt.Errorf("GO ontology version not found in OBO header")
}

func readNextLine(reader *strings.Reader) (string, error) {
	var builder strings.Builder
	for {
		char, _, err := reader.ReadRune()
		if err != nil {
			if err == io.EOF && builder.Len() > 0 {
				return builder.String(), nil
			}
			return "", err
		}
		if char == '\n' {
			return builder.String(), nil
		}
		if char != '\r' {
			builder.WriteRune(char)
		}
	}
}

func extractDateToken(text string) string {
	for index := 0; index+10 <= len(text); index++ {
		candidate := text[index : index+10]
		if isDateToken(candidate) {
			return candidate
		}
	}
	return ""
}

func isDateToken(text string) bool {
	if len(text) != 10 {
		return false
	}
	for index, char := range text {
		if index == 4 || index == 7 {
			if char != '-' {
				return false
			}
			continue
		}
		if char < '0' || char > '9' {
			return false
		}
	}
	return true
}

func validateOptionalOntologyVersionToken(versionToken string) error {
	versionToken = strings.TrimSpace(versionToken)
	if versionToken == "" {
		return nil
	}
	if !isDateToken(versionToken) {
		return fmt.Errorf(
			"version must be a GO release date in YYYY-MM-DD, e.g. 2026-01-23; see %s and %s",
			ontologyArchiveRootURL,
			ontologyArchiveDocsURL,
		)
	}
	return nil
}

func buildOntologyIndexURL(baseURL string) string {
	return baseURL + "index.html"
}

func buildOntologyReleaseBaseURL(versionToken string) string {
	return ontologyArchiveRootURL + versionToken + "/ontology/"
}

func buildOntologyBaseURLForVersionToken(versionToken string) string {
	if isDateToken(strings.TrimSpace(versionToken)) {
		return buildOntologyReleaseBaseURL(strings.TrimSpace(versionToken))
	}
	return ontologyCurrentBaseURL
}

func buildOntologyAssetURL(baseURL string, assetName string) string {
	return baseURL + assetName
}
