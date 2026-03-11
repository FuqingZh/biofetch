package geneontology

import (
	"crypto/sha256"
	"crypto/tls"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	toml "github.com/pelletier/go-toml/v2"
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

var ontologyAssets = []ontologyAsset{
	{
		name: "go-basic.obo",
		url:  "https://current.geneontology.org/ontology/go-basic.obo",
	},
	{
		name: "go.obo",
		url:  "https://current.geneontology.org/ontology/go.obo",
	},
	{
		name: "go-plus.json",
		url:  "https://current.geneontology.org/ontology/go-plus.json",
	},
}

var defaultOntologyAssetNames = []string{
	"go-basic.obo",
	"go.obo",
	"go-plus.json",
}

func runFetchOntology(cfg *ontologyConfig) error {
	assets, err := parseOntologyAssetNames(cfg.assetsCSV)
	if err != nil {
		return err
	}

	clientHTTP := createHTTPClient(cfg.shouldAllowInsecureTLS)
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

	if err := writeManifest(fileManifest, cfg, records, time.Now()); err != nil {
		return err
	}

	logf("done (files=%d)", len(records))
	logf("manifest written: %s", fileManifest)
	return nil
}

func parseOntologyAssetNames(textCSV string) ([]ontologyAsset, error) {
	setAssets := make(map[string]struct{})
	for _, token := range strings.Split(textCSV, ",") {
		name := strings.TrimSpace(token)
		if name == "" {
			continue
		}
		setAssets[name] = struct{}{}
	}
	if len(setAssets) == 0 {
		return nil, fmt.Errorf("assets must not be empty")
	}

	assets := make([]ontologyAsset, 0, len(setAssets))
	for _, asset := range ontologyAssets {
		if _, ok := setAssets[asset.name]; ok {
			assets = append(assets, asset)
			delete(setAssets, asset.name)
		}
	}
	if len(setAssets) > 0 {
		namesUnknown := make([]string, 0, len(setAssets))
		for name := range setAssets {
			namesUnknown = append(namesUnknown, name)
		}
		sort.Strings(namesUnknown)
		return nil, fmt.Errorf("unknown ontology asset(s): %s", strings.Join(namesUnknown, ", "))
	}
	return assets, nil
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
	response, err := clientHTTP.Get(urlFile)
	if err != nil {
		return fmt.Errorf("request %s: %w", urlFile, err)
	}
	defer response.Body.Close()

	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("request %s: unexpected status %s", urlFile, response.Status)
	}

	fileHandle, err := os.Create(fileOut)
	if err != nil {
		return fmt.Errorf("create %s: %w", fileOut, err)
	}

	_, errCopy := io.Copy(fileHandle, response.Body)
	errClose := fileHandle.Close()
	if errCopy != nil {
		return fmt.Errorf("write %s: %w", fileOut, errCopy)
	}
	if errClose != nil {
		return fmt.Errorf("close %s: %w", fileOut, errClose)
	}
	return nil
}

func writeManifest(
	fileManifest string,
	cfg *ontologyConfig,
	records []ontologyRecord,
	timeDownloaded time.Time,
) error {
	fileTemp := fileManifest + ".tmp"
	fileOut, err := os.Create(fileTemp)
	if err != nil {
		return fmt.Errorf("create manifest: %w", err)
	}

	manifest := buildOntologyManifestFile(cfg, records, timeDownloaded)
	encoder := toml.NewEncoder(fileOut)
	encoder.SetIndentTables(true)
	errEncode := encoder.Encode(manifest)
	errClose := fileOut.Close()
	if errEncode != nil {
		_ = os.Remove(fileTemp)
		return errEncode
	}
	if errClose != nil {
		_ = os.Remove(fileTemp)
		return fmt.Errorf("close manifest: %w", errClose)
	}
	if err := os.Rename(fileTemp, fileManifest); err != nil {
		return fmt.Errorf("rename manifest: %w", err)
	}
	return nil
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
	transport := http.DefaultTransport.(*http.Transport).Clone()
	if shouldAllowInsecureTLS {
		transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
	}
	return &http.Client{
		Timeout:   0,
		Transport: transport,
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

func logf(format string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, "[biofetch go] %s\n", fmt.Sprintf(format, args...))
}

func resolveOntologyVersion(clientHTTP *http.Client) (string, string, error) {
	data, err := downloadText(clientHTTP, "https://current.geneontology.org/ontology/go-basic.obo")
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
