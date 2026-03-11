package geneontology

import (
	"biofetch/internal/shared/httpx"
	"biofetch/internal/shared/logx"
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

const ontologyBaseURL = "https://current.geneontology.org/ontology/"
const ontologyIndexURL = ontologyBaseURL + "index.html"
const ontologyVersionAssetName = "go-basic.obo"

func runFetchOntology(cfg *ontologyConfig) error {
	clientHTTP := createHTTPClient(cfg.shouldAllowInsecureTLS)
	assetsAvailable, err := discoverOntologyAssets(clientHTTP)
	if err != nil {
		return err
	}
	assets, err := resolveOntologyAssets(assetsAvailable, cfg.assetNames, cfg.shouldDownloadAll)
	if err != nil {
		return err
	}
	version, versionToken, err := resolveOntologyVersion(clientHTTP)
	if err != nil {
		return err
	}
	cfg.version = version
	cfg.versionToken = versionToken

	dirVersion := filepath.Join(cfg.dirOut, "ontology", cfg.versionToken)
	dirRaw := filepath.Join(dirVersion, "raw")
	dirTidy := filepath.Join(dirVersion, "tidy")
	fileManifest := filepath.Join(dirVersion, "manifest.lock")

	if cfg.shouldDryRun {
		logf("[dry-run] version dir: %s", dirVersion)
		logf("[dry-run] manifest: %s", fileManifest)
	} else {
		if err := os.MkdirAll(dirRaw, 0o755); err != nil {
			return fmt.Errorf("create raw dir: %w", err)
		}
		if err := os.MkdirAll(dirTidy, 0o755); err != nil {
			return fmt.Errorf("create tidy dir: %w", err)
		}
	}

	records := make([]ontologyRecord, 0, len(assets))

	for _, asset := range assets {
		fileOut := filepath.Join(dirRaw, asset.name)
		pathRel := filepath.ToSlash(filepath.Join("raw", asset.name))

		if cfg.shouldDryRun {
			logf("[dry-run] %s -> %s", asset.url, fileOut)
			continue
		}

		record, err := fetchOntologyAsset(clientHTTP, cfg, asset, fileOut, pathRel)
		if err != nil {
			return err
		}
		records = append(records, record)
	}

	if cfg.shouldDryRun {
		logf("[dry-run] done (assets=%d)", len(assets))
		return nil
	}

	sort.Slice(records, func(i, j int) bool {
		return records[i].Asset < records[j].Asset
	})

	recordsComplete, err := buildCompleteOntologyRecords(fileManifest, dirVersion, records)
	if err != nil {
		return err
	}

	if err := writeManifest(fileManifest, cfg, recordsComplete, time.Now()); err != nil {
		return err
	}

	logf("done (files=%d)", len(recordsComplete))
	logf("manifest written: %s", fileManifest)
	return nil
}

func discoverOntologyAssets(clientHTTP *http.Client) ([]ontologyAsset, error) {
	data, err := downloadText(clientHTTP, ontologyIndexURL)
	if err != nil {
		return nil, err
	}
	return parseOntologyAssetsFromIndex(data)
}

func parseOntologyAssetsFromIndex(data []byte) ([]ontologyAsset, error) {
	document, err := html.Parse(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("parse ontology index html: %w", err)
	}

	assetsByName := make(map[string]ontologyAsset)
	for _, name := range extractOntologyAnchorTargets(document) {
		if shouldIncludeOntologyAsset(name) {
			assetsByName[name] = ontologyAsset{
				name: name,
				url:  ontologyBaseURL + name,
			}
		}
	}
	if len(assetsByName) == 0 {
		return nil, fmt.Errorf("no ontology assets found at %s", ontologyIndexURL)
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
	shouldDownloadAll bool,
) ([]ontologyAsset, error) {
	if shouldDownloadAll {
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
	setAssets := make(map[string]struct{})
	for _, value := range values {
		for _, token := range strings.Split(value, ",") {
			name := strings.TrimSpace(token)
			if name == "" {
				continue
			}
			setAssets[name] = struct{}{}
		}
	}
	if len(setAssets) == 0 {
		return nil, fmt.Errorf("assets must not be empty")
	}

	return sets.SortedKeys(setAssets), nil
}

func fetchOntologyAsset(
	clientHTTP *http.Client,
	cfg *ontologyConfig,
	asset ontologyAsset,
	fileOut string,
	pathRel string,
) (ontologyRecord, error) {
	if !cfg.shouldOverwriteExisting {
		recordExisting, ok, err := inspectExistingAsset(fileOut, pathRel, asset)
		if err != nil {
			return ontologyRecord{}, err
		}
		if ok {
			logf("using existing %s", asset.name)
			return recordExisting, nil
		}
	}

	logf("downloading %s", asset.name)
	if err := downloadFileWithRetry(clientHTTP, asset.url, fileOut, cfg.retryMax, cfg.retryWait); err != nil {
		return ontologyRecord{}, err
	}
	return buildOntologyRecord(fileOut, pathRel, asset)
}

func inspectExistingAsset(
	filePath string,
	pathRel string,
	asset ontologyAsset,
) (ontologyRecord, bool, error) {
	infoFile, err := os.Stat(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return ontologyRecord{}, false, nil
		}
		return ontologyRecord{}, false, fmt.Errorf("stat existing file: %w", err)
	}
	if infoFile.Size() <= 0 {
		return ontologyRecord{}, false, nil
	}

	record, err := buildOntologyRecord(filePath, pathRel, asset)
	if err != nil {
		return ontologyRecord{}, false, err
	}
	return record, true, nil
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
) error {
	filePart := fileOut + ".part"
	var errLast error

	for attempt := 1; attempt <= retryMax; attempt++ {
		if err := downloadFile(clientHTTP, urlFile, filePart); err == nil {
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

func downloadFile(clientHTTP *http.Client, urlFile string, fileOut string) error {
	return httpx.DownloadFile(clientHTTP, urlFile, fileOut)
}

func writeManifest(
	fileManifest string,
	cfg *ontologyConfig,
	records []ontologyRecord,
	timeDownloaded time.Time,
) error {
	manifest := buildOntologyManifestFile(cfg, records, timeDownloaded)
	return tomlx.WriteFileAtomic(fileManifest, manifest)
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
		VersionToken: cfg.versionToken,
		DownloadedAt: timeDownloaded.Format(time.RFC3339),
		Files:        files,
	}
}

func createHTTPClient(shouldAllowInsecureTLS bool) *http.Client {
	return httpx.NewClient(shouldAllowInsecureTLS)
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

func logf(format string, args ...interface{}) {
	logx.Logf("biofetch go", format, args...)
}

func resolveOntologyVersion(clientHTTP *http.Client) (string, string, error) {
	data, err := downloadText(clientHTTP, ontologyBaseURL+ontologyVersionAssetName)
	if err != nil {
		return "", "", err
	}
	version, err := parseOntologyVersionFromOBO(data)
	if err != nil {
		return "", "", err
	}
	return version, version, nil
}

func downloadText(clientHTTP *http.Client, urlFile string) ([]byte, error) {
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
