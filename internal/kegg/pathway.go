package kegg

import (
	"bufio"
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

const baseURL = "https://rest.kegg.jp"

type pathwayRecord struct {
	PathwayID string
	Asset     string
	PathRel   string
	SHA256    string
	Bytes     int64
	URL       string
}

type manifestFile struct {
	Database     string            `toml:"database"`
	Asset        string            `toml:"asset"`
	Version      string            `toml:"version"`
	VersionToken string            `toml:"version_token"`
	DownloadedAt string            `toml:"downloaded_at"`
	Scope        manifestScope     `toml:"scope"`
	Pathways     []manifestPathway `toml:"pathways"`
	Files        []manifestAsset   `toml:"files"`
}

type manifestScope struct {
	Type  string `toml:"type"`
	Value string `toml:"value"`
}

type manifestPathway struct {
	ID    string   `toml:"id"`
	Files []string `toml:"files"`
}

type manifestAsset struct {
	PathwayID string `toml:"pathway_id"`
	Asset     string `toml:"asset"`
	Path      string `toml:"path"`
	SHA256    string `toml:"sha256"`
	Bytes     int64  `toml:"bytes"`
	URL       string `toml:"url"`
}

func runFetchPathway(cfg *pathwayConfig) error {
	clientHTTP := createHTTPClient(cfg.shouldAllowInsecureTLS)
	clientKegg := createKEGGClient(clientHTTP, cfg.requestInterval)

	version, versionToken, err := resolveKEGGVersion(clientKegg, "pathway")
	if err != nil {
		return err
	}
	cfg.version = version
	cfg.versionToken = versionToken

	scopeKey := deriveScopeKey(cfg)
	dirVersion := filepath.Join(cfg.dirOut, "pathway", cfg.versionToken)
	dirRawScope := filepath.Join(dirVersion, "raw", scopeKey)
	dirTidyScope := filepath.Join(dirVersion, "tidy", scopeKey)
	fileManifest := filepath.Join(dirVersion, "manifest.lock")

	if cfg.shouldDryRun {
		logf("[dry-run] version dir: %s", dirVersion)
		logf("[dry-run] manifest: %s", fileManifest)
	} else {
		if err := os.MkdirAll(dirRawScope, 0o755); err != nil {
			return fmt.Errorf("create raw dir: %w", err)
		}
		if err := os.MkdirAll(dirTidyScope, 0o755); err != nil {
			return fmt.Errorf("create tidy dir: %w", err)
		}
	}

	pathwayIDs, listContent, listURL, err := resolvePathwayIDs(clientKegg, cfg)
	if err != nil {
		return err
	}

	records := make([]pathwayRecord, 0, 1+2*len(pathwayIDs))
	fileList := filepath.Join(dirRawScope, "pathway.list.tsv")
	pathRelList := filepath.ToSlash(filepath.Join("raw", scopeKey, "pathway.list.tsv"))

	if cfg.shouldDryRun {
		logf("[dry-run] %s -> %s", listURL, fileList)
	} else {
		recordList, err := writeDownloadedFile(
			fileList,
			pathRelList,
			"",
			"pathway.list",
			listURL,
			listContent,
		)
		if err != nil {
			return err
		}
		records = append(records, recordList)
	}

	for _, pathwayID := range pathwayIDs {
		fileEntry := filepath.Join(dirRawScope, pathwayID+".txt")
		pathRelEntry := filepath.ToSlash(filepath.Join("raw", scopeKey, pathwayID+".txt"))
		urlEntry := baseURL + "/get/" + pathwayID

		fileKGML := filepath.Join(dirRawScope, pathwayID+".kgml")
		pathRelKGML := filepath.ToSlash(filepath.Join("raw", scopeKey, pathwayID+".kgml"))
		urlKGML := baseURL + "/get/" + pathwayID + "/kgml"

		if cfg.shouldDryRun {
			logf("[dry-run] %s -> %s", urlEntry, fileEntry)
			logf("[dry-run] %s -> %s", urlKGML, fileKGML)
			continue
		}

		recordEntry, err := fetchPathwayAsset(
			clientKegg,
			cfg.shouldOverwriteExisting,
			fileEntry,
			pathRelEntry,
			pathwayID,
			"pathway.entry",
			urlEntry,
		)
		if err != nil {
			return err
		}
		records = append(records, recordEntry)

		recordKGML, err := fetchPathwayAsset(
			clientKegg,
			cfg.shouldOverwriteExisting,
			fileKGML,
			pathRelKGML,
			pathwayID,
			"pathway.kgml",
			urlKGML,
		)
		if err != nil {
			return err
		}
		records = append(records, recordKGML)
	}

	if cfg.shouldDryRun {
		logf("[dry-run] done (pathways=%d)", len(pathwayIDs))
		return nil
	}

	sort.Slice(records, func(i, j int) bool {
		if records[i].PathwayID != records[j].PathwayID {
			return records[i].PathwayID < records[j].PathwayID
		}
		return records[i].Asset < records[j].Asset
	})

	if err := writeManifest(fileManifest, cfg, records, time.Now()); err != nil {
		return err
	}

	logf("done (files=%d, pathways=%d)", len(records), len(pathwayIDs))
	logf("manifest written: %s", fileManifest)
	return nil
}

func deriveScopeKey(cfg *pathwayConfig) string {
	if cfg.shouldFetchReference {
		return "reference"
	}
	return cfg.organismCode
}

func resolvePathwayIDs(
	clientKegg *keggClient,
	cfg *pathwayConfig,
) ([]string, []byte, string, error) {
	listURL := derivePathwayListURL(cfg)
	listContent, err := clientKegg.download(listURL)
	if err != nil {
		return nil, nil, "", err
	}

	if strings.TrimSpace(cfg.pathwayIDsCSV) != "" {
		pathwayIDs, err := parsePathwayIDsCSV(cfg.pathwayIDsCSV)
		if err != nil {
			return nil, nil, "", err
		}
		return pathwayIDs, listContent, listURL, nil
	}
	if cfg.filePathwayIDs != "" {
		pathwayIDs, err := readPathwayIDsFromFile(cfg.filePathwayIDs)
		if err != nil {
			return nil, nil, "", err
		}
		return pathwayIDs, listContent, listURL, nil
	}

	pathwayIDs, err := parsePathwayIDsFromList(listContent)
	if err != nil {
		return nil, nil, "", err
	}
	return pathwayIDs, listContent, listURL, nil
}

func derivePathwayListURL(cfg *pathwayConfig) string {
	if cfg.shouldFetchReference {
		return baseURL + "/list/pathway"
	}
	return baseURL + "/list/pathway/" + cfg.organismCode
}

func parsePathwayIDsCSV(textCSV string) ([]string, error) {
	setPathwayIDs := make(map[string]struct{})
	for _, token := range strings.Split(textCSV, ",") {
		pathwayID := strings.TrimSpace(token)
		if pathwayID == "" {
			continue
		}
		if !isValidPathwayID(pathwayID) {
			return nil, fmt.Errorf("invalid pathway id: %s", pathwayID)
		}
		setPathwayIDs[pathwayID] = struct{}{}
	}
	return selectSortedKeys(setPathwayIDs), nil
}

func readPathwayIDsFromFile(filePathwayIDs string) ([]string, error) {
	fileIn, err := os.Open(filePathwayIDs)
	if err != nil {
		return nil, fmt.Errorf("open pathway ids file: %w", err)
	}
	defer fileIn.Close()

	setPathwayIDs := make(map[string]struct{})
	scanner := bufio.NewScanner(fileIn)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if !isValidPathwayID(line) {
			return nil, fmt.Errorf("invalid pathway id in %s: %s", filePathwayIDs, line)
		}
		setPathwayIDs[line] = struct{}{}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read pathway ids file: %w", err)
	}

	return selectSortedKeys(setPathwayIDs), nil
}

func parsePathwayIDsFromList(data []byte) ([]string, error) {
	setPathwayIDs := make(map[string]struct{})
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		fields := strings.Split(line, "\t")
		if len(fields) == 0 {
			continue
		}
		pathwayID := strings.TrimSpace(fields[0])
		if !isValidPathwayID(pathwayID) {
			return nil, fmt.Errorf("invalid pathway id in KEGG list output: %s", pathwayID)
		}
		setPathwayIDs[pathwayID] = struct{}{}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan pathway list: %w", err)
	}
	return selectSortedKeys(setPathwayIDs), nil
}

func fetchPathwayAsset(
	clientKegg *keggClient,
	shouldOverwriteExisting bool,
	fileOut string,
	pathRel string,
	pathwayID string,
	assetName string,
	urlFile string,
) (pathwayRecord, error) {
	if !shouldOverwriteExisting {
		recordExisting, ok, err := inspectExistingFile(fileOut, pathRel, pathwayID, assetName, urlFile)
		if err != nil {
			return pathwayRecord{}, err
		}
		if ok {
			logf("using existing %s", filepath.Base(fileOut))
			return recordExisting, nil
		}
	}

	logf("downloading %s", filepath.Base(fileOut))
	data, err := clientKegg.download(urlFile)
	if err != nil {
		return pathwayRecord{}, err
	}
	return writeDownloadedFile(fileOut, pathRel, pathwayID, assetName, urlFile, data)
}

func writeDownloadedFile(
	fileOut string,
	pathRel string,
	pathwayID string,
	assetName string,
	urlFile string,
	data []byte,
) (pathwayRecord, error) {
	if err := os.WriteFile(fileOut, data, 0o644); err != nil {
		return pathwayRecord{}, fmt.Errorf("write %s: %w", fileOut, err)
	}
	return buildPathwayRecord(fileOut, pathRel, pathwayID, assetName, urlFile)
}

func inspectExistingFile(
	filePath string,
	pathRel string,
	pathwayID string,
	assetName string,
	urlFile string,
) (pathwayRecord, bool, error) {
	infoFile, err := os.Stat(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return pathwayRecord{}, false, nil
		}
		return pathwayRecord{}, false, fmt.Errorf("stat existing file: %w", err)
	}
	if infoFile.Size() <= 0 {
		return pathwayRecord{}, false, nil
	}

	record, err := buildPathwayRecord(filePath, pathRel, pathwayID, assetName, urlFile)
	if err != nil {
		return pathwayRecord{}, false, err
	}
	return record, true, nil
}

func buildPathwayRecord(
	filePath string,
	pathRel string,
	pathwayID string,
	assetName string,
	urlFile string,
) (pathwayRecord, error) {
	infoFile, err := os.Stat(filePath)
	if err != nil {
		return pathwayRecord{}, fmt.Errorf("stat file: %w", err)
	}
	sha256File, err := calculateSHA256ForFile(filePath)
	if err != nil {
		return pathwayRecord{}, err
	}

	return pathwayRecord{
		PathwayID: pathwayID,
		Asset:     assetName,
		PathRel:   pathRel,
		SHA256:    sha256File,
		Bytes:     infoFile.Size(),
		URL:       urlFile,
	}, nil
}

func writeManifest(
	fileManifest string,
	cfg *pathwayConfig,
	records []pathwayRecord,
	timeDownloaded time.Time,
) error {
	fileTemp := fileManifest + ".tmp"
	fileOut, err := os.Create(fileTemp)
	if err != nil {
		return fmt.Errorf("create manifest: %w", err)
	}

	manifest := buildManifestFile(cfg, records, timeDownloaded)
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

func buildManifestFile(
	cfg *pathwayConfig,
	records []pathwayRecord,
	timeDownloaded time.Time,
) manifestFile {
	mapRecordsByPathway := make(map[string][]pathwayRecord)
	pathwayIDs := make([]string, 0)
	for _, record := range records {
		if record.PathwayID == "" {
			continue
		}
		if _, ok := mapRecordsByPathway[record.PathwayID]; !ok {
			pathwayIDs = append(pathwayIDs, record.PathwayID)
		}
		mapRecordsByPathway[record.PathwayID] = append(mapRecordsByPathway[record.PathwayID], record)
	}
	sort.Strings(pathwayIDs)

	pathways := make([]manifestPathway, 0, len(pathwayIDs))
	for _, pathwayID := range pathwayIDs {
		recordsPathway := mapRecordsByPathway[pathwayID]
		sort.Slice(recordsPathway, func(i, j int) bool {
			return recordsPathway[i].Asset < recordsPathway[j].Asset
		})

		paths := make([]string, 0, len(recordsPathway))
		for _, record := range recordsPathway {
			paths = append(paths, record.PathRel)
		}
		pathways = append(pathways, manifestPathway{
			ID:    pathwayID,
			Files: paths,
		})
	}

	files := make([]manifestAsset, 0, len(records))
	for _, record := range records {
		files = append(files, manifestAsset{
			PathwayID: record.PathwayID,
			Asset:     record.Asset,
			Path:      record.PathRel,
			SHA256:    record.SHA256,
			Bytes:     record.Bytes,
			URL:       record.URL,
		})
	}

	scopeType := "organism"
	scopeValue := cfg.organismCode
	if cfg.shouldFetchReference {
		scopeType = "reference"
		scopeValue = "pathway"
	}

	return manifestFile{
		Database:     "kegg",
		Asset:        "pathway",
		Version:      cfg.version,
		VersionToken: cfg.versionToken,
		DownloadedAt: timeDownloaded.Format(time.RFC3339),
		Scope: manifestScope{
			Type:  scopeType,
			Value: scopeValue,
		},
		Pathways: pathways,
		Files:    files,
	}
}

type keggClient struct {
	clientHTTP      *http.Client
	requestInterval time.Duration
	timeLastRequest time.Time
}

func createKEGGClient(clientHTTP *http.Client, requestInterval time.Duration) *keggClient {
	return &keggClient{
		clientHTTP:      clientHTTP,
		requestInterval: requestInterval,
	}
}

func (client *keggClient) download(urlFile string) ([]byte, error) {
	if client.requestInterval > 0 && !client.timeLastRequest.IsZero() {
		wait := client.requestInterval - time.Since(client.timeLastRequest)
		if wait > 0 {
			time.Sleep(wait)
		}
	}

	response, err := client.clientHTTP.Get(urlFile)
	client.timeLastRequest = time.Now()
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

func selectSortedKeys(setText map[string]struct{}) []string {
	values := make([]string, 0, len(setText))
	for key := range setText {
		values = append(values, key)
	}
	sort.Strings(values)
	return values
}

func isValidPathwayID(text string) bool {
	if len(text) < 7 {
		return false
	}
	prefix := text[:len(text)-5]
	suffix := text[len(text)-5:]
	if prefix == "" {
		return false
	}
	for _, char := range prefix {
		if !(char >= 'a' && char <= 'z') && !(char >= 'A' && char <= 'Z') {
			return false
		}
	}
	for _, char := range suffix {
		if char < '0' || char > '9' {
			return false
		}
	}
	return true
}

func logf(format string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, "[fetch_kegg] %s\n", fmt.Sprintf(format, args...))
}

func resolveKEGGVersion(clientKegg *keggClient, databaseName string) (string, string, error) {
	dataInfo, err := clientKegg.download(baseURL + "/info/" + databaseName)
	if err != nil {
		return "", "", err
	}
	version, err := parseKEGGReleaseFromInfo(dataInfo)
	if err != nil {
		return "", "", err
	}
	return version, sanitizeKEGGVersionToken(version), nil
}

func parseKEGGReleaseFromInfo(data []byte) (string, error) {
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		textPrefix := "Release "
		index := strings.Index(line, textPrefix)
		if index < 0 {
			continue
		}
		textAfter := strings.TrimSpace(line[index+len(textPrefix):])
		if textAfter == "" {
			continue
		}
		indexComma := strings.Index(textAfter, ",")
		if indexComma >= 0 {
			textAfter = strings.TrimSpace(textAfter[:indexComma])
		}
		if textAfter != "" {
			return textAfter, nil
		}
	}
	if err := scanner.Err(); err != nil {
		return "", fmt.Errorf("scan KEGG info output: %w", err)
	}
	return "", fmt.Errorf("KEGG release not found in info output")
}

func sanitizeKEGGVersionToken(version string) string {
	replacer := strings.NewReplacer("/", "_", " ", "_")
	return replacer.Replace(version)
}
