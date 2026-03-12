package kegg

import (
	"biofetch/internal/shared/httpx"
	"biofetch/internal/shared/logx"
	"biofetch/internal/shared/sets"
	"biofetch/internal/shared/tomlx"
	"bufio"
	"crypto/sha256"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
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
	Database      string            `toml:"database"`
	Asset         string            `toml:"asset"`
	Version       string            `toml:"version"`
	VersionToken  string            `toml:"version_token"`
	SourceRelease string            `toml:"source_release"`
	DownloadedAt  string            `toml:"downloaded_at"`
	Scope         manifestScope     `toml:"scope"`
	Pathways      []manifestPathway `toml:"pathways"`
	Files         []manifestAsset   `toml:"files"`
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

	sourceRelease, currentMajorVersion, err := resolveKEGGVersion(clientKegg, "pathway")
	if err != nil {
		return err
	}
	if strings.TrimSpace(cfg.versionToken) == "" {
		cfg.versionToken = currentMajorVersion
	} else if cfg.versionToken != currentMajorVersion {
		return fmt.Errorf("version %q does not match current KEGG pathway major version %q", cfg.versionToken, currentMajorVersion)
	}
	cfg.version = cfg.versionToken
	cfg.sourceRelease = sourceRelease

	scopeKeys, err := resolvePathwayScopeKeys(clientKegg, cfg)
	if err != nil {
		return err
	}
	dirVersion := filepath.Join(cfg.dirOut, "pathway", cfg.versionToken)
	fileManifest := filepath.Join(dirVersion, "manifest.lock")

	if cfg.shouldDryRun {
		logf("[dry-run] version dir: %s", dirVersion)
		logf("[dry-run] manifest: %s", fileManifest)
	} else {
		if err := os.MkdirAll(dirVersion, 0o755); err != nil {
			return fmt.Errorf("create version dir: %w", err)
		}
	}

	records := make([]pathwayRecord, 0)
	countPathways := 0
	for _, scopeKey := range scopeKeys {
		recordsScope, countScopePathways, err := fetchPathwayScope(clientKegg, cfg, dirVersion, scopeKey)
		if err != nil {
			return err
		}
		records = append(records, recordsScope...)
		countPathways += countScopePathways
	}

	if cfg.shouldDryRun {
		logf("[dry-run] done (scopes=%d)", len(scopeKeys))
		return nil
	}

	sort.Slice(records, func(i, j int) bool {
		if records[i].PathwayID != records[j].PathwayID {
			return records[i].PathwayID < records[j].PathwayID
		}
		return records[i].Asset < records[j].Asset
	})

	recordsComplete, err := buildCompletePathwayRecords(fileManifest, dirVersion, records)
	if err != nil {
		return err
	}

	if err := writeManifest(fileManifest, cfg, recordsComplete, time.Now()); err != nil {
		return err
	}

	logf("done (files=%d, pathways=%d, scopes=%d)", len(recordsComplete), countPathways, len(scopeKeys))
	logf("manifest written: %s", fileManifest)
	return nil
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

func resolvePathwayScopeKeys(clientKegg *keggClient, cfg *pathwayConfig) ([]string, error) {
	switch {
	case cfg.shouldFetchReference:
		cfg.scopeType = "reference"
		cfg.scopeValue = "pathway"
		return []string{"reference"}, nil
	case cfg.shouldDownloadAll:
		organismCodes, err := resolveKEGGOrganismCodes(clientKegg)
		if err != nil {
			return nil, err
		}
		cfg.scopeType = "organisms"
		cfg.scopeValue = "all"
		return organismCodes, nil
	case len(cfg.organismCodes) > 0:
		organismCodes, err := parseKEGGOrganismCodes(cfg.organismCodes)
		if err != nil {
			return nil, err
		}
		if len(organismCodes) == 1 {
			cfg.scopeType = "organism"
			cfg.scopeValue = organismCodes[0]
		} else {
			cfg.scopeType = "organisms"
			cfg.scopeValue = strings.Join(organismCodes, ",")
		}
		return organismCodes, nil
	case strings.TrimSpace(cfg.fileOrganismCodes) != "":
		organismCodes, err := readKEGGOrganismCodesFromFile(cfg.fileOrganismCodes)
		if err != nil {
			return nil, err
		}
		if len(organismCodes) == 1 {
			cfg.scopeType = "organism"
			cfg.scopeValue = organismCodes[0]
		} else {
			cfg.scopeType = "organisms"
			cfg.scopeValue = strings.Join(organismCodes, ",")
		}
		return organismCodes, nil
	default:
		return nil, fmt.Errorf("no pathway scope configured")
	}
}

func fetchPathwayScope(
	clientKegg *keggClient,
	cfg *pathwayConfig,
	dirVersion string,
	scopeKey string,
) ([]pathwayRecord, int, error) {
	cfgScope := *cfg
	cfgScope.organismCode = scopeKey
	cfgScope.shouldFetchReference = scopeKey == "reference"

	dirRawScope := filepath.Join(dirVersion, "raw", scopeKey)
	dirTidyScope := filepath.Join(dirVersion, "tidy", scopeKey)
	if !cfg.shouldDryRun {
		if err := os.MkdirAll(dirRawScope, 0o755); err != nil {
			return nil, 0, fmt.Errorf("create raw dir: %w", err)
		}
		if err := os.MkdirAll(dirTidyScope, 0o755); err != nil {
			return nil, 0, fmt.Errorf("create tidy dir: %w", err)
		}
	}

	pathwayIDs, listContent, listURL, err := resolvePathwayIDs(clientKegg, &cfgScope)
	if err != nil {
		return nil, 0, err
	}

	records := make([]pathwayRecord, 0, 1+2*len(pathwayIDs))
	fileList := filepath.Join(dirRawScope, "pathway.list.tsv")
	pathRelList := filepath.ToSlash(filepath.Join("raw", scopeKey, "pathway.list.tsv"))

	if cfg.shouldDryRun {
		logf("[dry-run] %s -> %s", listURL, fileList)
	} else {
		recordList, err := writeDownloadedFile(fileList, pathRelList, "", "pathway.list", listURL, listContent)
		if err != nil {
			return nil, 0, err
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

		recordEntry, err := fetchPathwayAsset(clientKegg, cfg.shouldOverwriteExisting, fileEntry, pathRelEntry, pathwayID, "pathway.entry", urlEntry)
		if err != nil {
			return nil, 0, err
		}
		records = append(records, recordEntry)

		recordKGML, err := fetchPathwayAsset(clientKegg, cfg.shouldOverwriteExisting, fileKGML, pathRelKGML, pathwayID, "pathway.kgml", urlKGML)
		if err != nil {
			return nil, 0, err
		}
		records = append(records, recordKGML)
	}

	return records, len(pathwayIDs), nil
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
	return sets.SortedKeys(setPathwayIDs), nil
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

	return sets.SortedKeys(setPathwayIDs), nil
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
	return sets.SortedKeys(setPathwayIDs), nil
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
	manifest := buildManifestFile(cfg, records, timeDownloaded)
	return tomlx.WriteFileAtomic(fileManifest, manifest)
}

func buildCompletePathwayRecords(
	fileManifest string,
	dirVersion string,
	recordsCurrent []pathwayRecord,
) ([]pathwayRecord, error) {
	recordsExisting, err := readExistingPathwayRecords(fileManifest)
	if err != nil {
		return nil, err
	}

	recordsMerged := make(map[string]pathwayRecord, len(recordsExisting)+len(recordsCurrent))
	for _, record := range recordsExisting {
		recordsMerged[record.PathRel] = record
	}
	for _, record := range recordsCurrent {
		recordsMerged[record.PathRel] = record
	}

	records := make([]pathwayRecord, 0, len(recordsMerged))
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
		if records[i].PathwayID != records[j].PathwayID {
			return records[i].PathwayID < records[j].PathwayID
		}
		return records[i].Asset < records[j].Asset
	})
	return records, nil
}

func readExistingPathwayRecords(fileManifest string) ([]pathwayRecord, error) {
	var manifest manifestFile
	ok, err := tomlx.ReadFileIfExists(fileManifest, &manifest)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, nil
	}

	records := make([]pathwayRecord, 0, len(manifest.Files))
	for _, item := range manifest.Files {
		records = append(records, pathwayRecord{
			PathwayID: item.PathwayID,
			Asset:     item.Asset,
			PathRel:   item.Path,
			SHA256:    item.SHA256,
			Bytes:     item.Bytes,
			URL:       item.URL,
		})
	}
	return records, nil
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

	scopeType, scopeValue := derivePathwayManifestScope(cfg, records)

	return manifestFile{
		Database:      "kegg",
		Asset:         "pathway",
		Version:       cfg.version,
		VersionToken:  cfg.versionToken,
		SourceRelease: cfg.sourceRelease,
		DownloadedAt:  timeDownloaded.Format(time.RFC3339),
		Scope: manifestScope{
			Type:  scopeType,
			Value: scopeValue,
		},
		Pathways: pathways,
		Files:    files,
	}
}

func derivePathwayManifestScope(cfg *pathwayConfig, records []pathwayRecord) (string, string) {
	setScopes := make(map[string]struct{})
	for _, record := range records {
		scopeKey := derivePathwayScopeFromPath(record.PathRel)
		if scopeKey != "" {
			setScopes[scopeKey] = struct{}{}
		}
	}

	scopeKeys := sets.SortedKeys(setScopes)
	switch len(scopeKeys) {
	case 0:
		if cfg.shouldFetchReference {
			return "reference", "pathway"
		}
		if cfg.scopeType != "" || cfg.scopeValue != "" {
			return cfg.scopeType, cfg.scopeValue
		}
		if cfg.organismCode != "" {
			return "organism", cfg.organismCode
		}
		return "organisms", ""
	case 1:
		if scopeKeys[0] == "reference" {
			return "reference", "pathway"
		}
		return "organism", scopeKeys[0]
	default:
		filtered := make([]string, 0, len(scopeKeys))
		for _, scopeKey := range scopeKeys {
			if scopeKey == "reference" {
				continue
			}
			filtered = append(filtered, scopeKey)
		}
		if len(filtered) == 0 {
			return "reference", "pathway"
		}
		return "organisms", strings.Join(filtered, ",")
	}
}

func derivePathwayScopeFromPath(pathRel string) string {
	parts := strings.Split(pathRel, "/")
	if len(parts) < 3 || parts[0] != "raw" {
		return ""
	}
	return parts[1]
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
	logx.Logf("biofetch kegg", format, args...)
}

func resolveKEGGVersion(clientKegg *keggClient, databaseName string) (string, string, error) {
	dataInfo, err := clientKegg.download(baseURL + "/info/" + databaseName)
	if err != nil {
		return "", "", err
	}
	sourceRelease, err := parseKEGGReleaseFromInfo(dataInfo)
	if err != nil {
		return "", "", err
	}
	majorVersion, err := parseKEGGMajorVersion(sourceRelease)
	if err != nil {
		return "", "", err
	}
	return sourceRelease, majorVersion, nil
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

func parseKEGGMajorVersion(sourceRelease string) (string, error) {
	text := strings.TrimSpace(sourceRelease)
	if text == "" {
		return "", fmt.Errorf("KEGG source release is empty")
	}
	for i, ch := range text {
		if ch == '+' || ch == '/' || ch == ' ' || ch == ',' {
			text = strings.TrimSpace(text[:i])
			break
		}
	}
	if !isValidKEGGMajorVersion(text) {
		return "", fmt.Errorf("invalid KEGG major version in source release %q", sourceRelease)
	}
	return text, nil
}
