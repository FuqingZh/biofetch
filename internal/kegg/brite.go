package kegg

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	toml "github.com/pelletier/go-toml/v2"
)

type briteRecord struct {
	BriteID string
	Asset   string
	PathRel string
	SHA256  string
	Bytes   int64
	URL     string
}

type briteManifestFile struct {
	Database     string                 `toml:"database"`
	Asset        string                 `toml:"asset"`
	Version      string                 `toml:"version"`
	VersionToken string                 `toml:"version_token"`
	DownloadedAt string                 `toml:"downloaded_at"`
	Scope        manifestScope          `toml:"scope"`
	Brites       []briteManifestEntry   `toml:"brites"`
	Files        []briteManifestFileRef `toml:"files"`
}

type briteManifestEntry struct {
	ID    string   `toml:"id"`
	Files []string `toml:"files"`
}

type briteManifestFileRef struct {
	BriteID string `toml:"brite_id"`
	Asset   string `toml:"asset"`
	Path    string `toml:"path"`
	SHA256  string `toml:"sha256"`
	Bytes   int64  `toml:"bytes"`
	URL     string `toml:"url"`
}

func runFetchBrite(cfg *briteConfig) error {
	clientHTTP := createHTTPClient(cfg.shouldAllowInsecureTLS)
	clientKegg := createKEGGClient(clientHTTP, cfg.requestInterval)

	version, versionToken, err := resolveKEGGVersion(clientKegg, "brite")
	if err != nil {
		return err
	}
	cfg.version = version
	cfg.versionToken = versionToken

	dirVersion := filepath.Join(cfg.dirOut, "brite", cfg.versionToken)
	fileManifest := filepath.Join(dirVersion, "manifest.lock")

	if cfg.shouldDryRun {
		logf("[dry-run] version dir: %s", dirVersion)
		logf("[dry-run] manifest: %s", fileManifest)
	} else {
		if err := os.MkdirAll(dirVersion, 0o755); err != nil {
			return fmt.Errorf("create version dir: %w", err)
		}
	}

	records := make([]briteRecord, 0)
	countBrites := 0
	catalogs := []string{cfg.catalogCode}
	if cfg.shouldDownloadAllOrganisms {
		organismCodes, err := resolveKEGGOrganismCodes(clientKegg)
		if err != nil {
			return err
		}
		catalogs = organismCodes
	}

	for _, catalogCode := range catalogs {
		recordsCatalog, briteCount, err := fetchBriteCatalog(clientKegg, cfg, dirVersion, catalogCode)
		if err != nil {
			return err
		}
		records = append(records, recordsCatalog...)
		countBrites += briteCount
	}

	if cfg.shouldDryRun {
		logf("[dry-run] done (catalogs=%d)", len(catalogs))
		return nil
	}

	sort.Slice(records, func(i, j int) bool {
		if records[i].BriteID != records[j].BriteID {
			return records[i].BriteID < records[j].BriteID
		}
		return records[i].Asset < records[j].Asset
	})

	if err := writeBriteManifest(fileManifest, cfg, records, time.Now()); err != nil {
		return err
	}

	logf("done (files=%d, brites=%d, catalogs=%d)", len(records), countBrites, len(catalogs))
	logf("manifest written: %s", fileManifest)
	return nil
}

func fetchBriteCatalog(
	clientKegg *keggClient,
	cfg *briteConfig,
	dirVersion string,
	catalogCode string,
) ([]briteRecord, int, error) {
	cfgCatalog := *cfg
	cfgCatalog.catalogCode = catalogCode

	dirRawCatalog := filepath.Join(append([]string{dirVersion, "raw"}, deriveBriteScopeDir(&cfgCatalog)...)...)
	dirTidyCatalog := filepath.Join(append([]string{dirVersion, "tidy"}, deriveBriteScopeDir(&cfgCatalog)...)...)
	if !cfg.shouldDryRun {
		if err := os.MkdirAll(dirRawCatalog, 0o755); err != nil {
			return nil, 0, fmt.Errorf("create raw dir: %w", err)
		}
		if err := os.MkdirAll(dirTidyCatalog, 0o755); err != nil {
			return nil, 0, fmt.Errorf("create tidy dir: %w", err)
		}
	}

	briteIDs, listContent, listURL, err := resolveBriteIDs(clientKegg, &cfgCatalog)
	if err != nil {
		return nil, 0, err
	}

	records := make([]briteRecord, 0, 1+2*len(briteIDs))
	pathRelRoot := filepath.ToSlash(filepath.Join(append([]string{"raw"}, deriveBriteScopeDir(&cfgCatalog)...)...))
	fileList := filepath.Join(dirRawCatalog, "brite.list.tsv")
	pathRelList := filepath.ToSlash(filepath.Join(pathRelRoot, "brite.list.tsv"))
	if cfg.shouldDryRun {
		logf("[dry-run] %s -> %s", listURL, fileList)
	} else {
		recordList, err := writeBriteFile(fileList, pathRelList, "", "brite.list", listURL, listContent)
		if err != nil {
			return nil, 0, err
		}
		records = append(records, recordList)
	}

	for _, briteID := range briteIDs {
		fileEntry := filepath.Join(dirRawCatalog, briteID+".txt")
		pathRelEntry := filepath.ToSlash(filepath.Join(pathRelRoot, briteID+".txt"))
		urlEntry := baseURL + "/get/br:" + briteID

		fileJSON := filepath.Join(dirRawCatalog, briteID+".json")
		pathRelJSON := filepath.ToSlash(filepath.Join(pathRelRoot, briteID+".json"))
		urlJSON := baseURL + "/get/br:" + briteID + "/json"

		if cfg.shouldDryRun {
			logf("[dry-run] %s -> %s", urlEntry, fileEntry)
			logf("[dry-run] %s -> %s", urlJSON, fileJSON)
			continue
		}

		recordEntry, err := fetchBriteAsset(clientKegg, cfg.shouldOverwriteExisting, fileEntry, pathRelEntry, briteID, "brite.entry", urlEntry)
		if err != nil {
			return nil, 0, err
		}
		records = append(records, recordEntry)

		recordJSON, err := fetchBriteAsset(clientKegg, cfg.shouldOverwriteExisting, fileJSON, pathRelJSON, briteID, "brite.json", urlJSON)
		if err != nil {
			return nil, 0, err
		}
		records = append(records, recordJSON)
	}

	return records, len(briteIDs), nil
}

func resolveBriteIDs(
	clientKegg *keggClient,
	cfg *briteConfig,
) ([]string, []byte, string, error) {
	listURL := baseURL + "/list/brite/" + cfg.catalogCode
	listContent, err := clientKegg.download(listURL)
	if err != nil {
		return nil, nil, "", err
	}

	if strings.TrimSpace(cfg.briteIDsCSV) != "" {
		briteIDs, err := parseBriteIDsCSV(cfg.briteIDsCSV)
		if err != nil {
			return nil, nil, "", err
		}
		return briteIDs, listContent, listURL, nil
	}
	if cfg.fileBriteIDs != "" {
		briteIDs, err := readBriteIDsFromFile(cfg.fileBriteIDs)
		if err != nil {
			return nil, nil, "", err
		}
		return briteIDs, listContent, listURL, nil
	}

	briteIDs, err := parseBriteIDsFromList(listContent)
	if err != nil {
		return nil, nil, "", err
	}
	return briteIDs, listContent, listURL, nil
}

func parseBriteIDsCSV(textCSV string) ([]string, error) {
	setBriteIDs := make(map[string]struct{})
	for _, token := range strings.Split(textCSV, ",") {
		briteID := normalizeBriteID(token)
		if briteID == "" {
			continue
		}
		if !isValidBriteID(briteID) {
			return nil, fmt.Errorf("invalid BRITE id: %s", token)
		}
		setBriteIDs[briteID] = struct{}{}
	}
	return selectSortedKeys(setBriteIDs), nil
}

func readBriteIDsFromFile(fileBriteIDs string) ([]string, error) {
	fileIn, err := os.Open(fileBriteIDs)
	if err != nil {
		return nil, fmt.Errorf("open BRITE ids file: %w", err)
	}
	defer fileIn.Close()

	setBriteIDs := make(map[string]struct{})
	scanner := bufio.NewScanner(fileIn)
	for scanner.Scan() {
		line := normalizeBriteID(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if !isValidBriteID(line) {
			return nil, fmt.Errorf("invalid BRITE id in %s: %s", fileBriteIDs, line)
		}
		setBriteIDs[line] = struct{}{}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read BRITE ids file: %w", err)
	}
	return selectSortedKeys(setBriteIDs), nil
}

func parseBriteIDsFromList(data []byte) ([]string, error) {
	setBriteIDs := make(map[string]struct{})
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
		briteID := normalizeBriteID(fields[0])
		if !isValidBriteID(briteID) {
			return nil, fmt.Errorf("invalid BRITE id in KEGG list output: %s", fields[0])
		}
		setBriteIDs[briteID] = struct{}{}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan BRITE list: %w", err)
	}
	return selectSortedKeys(setBriteIDs), nil
}

func fetchBriteAsset(
	clientKegg *keggClient,
	shouldOverwriteExisting bool,
	fileOut string,
	pathRel string,
	briteID string,
	assetName string,
	urlFile string,
) (briteRecord, error) {
	if !shouldOverwriteExisting {
		recordExisting, ok, err := inspectBriteFile(fileOut, pathRel, briteID, assetName, urlFile)
		if err != nil {
			return briteRecord{}, err
		}
		if ok {
			logf("using existing %s", filepath.Base(fileOut))
			return recordExisting, nil
		}
	}

	logf("downloading %s", filepath.Base(fileOut))
	data, err := clientKegg.download(urlFile)
	if err != nil {
		return briteRecord{}, err
	}
	return writeBriteFile(fileOut, pathRel, briteID, assetName, urlFile, data)
}

func writeBriteFile(
	fileOut string,
	pathRel string,
	briteID string,
	assetName string,
	urlFile string,
	data []byte,
) (briteRecord, error) {
	if err := os.WriteFile(fileOut, data, 0o644); err != nil {
		return briteRecord{}, fmt.Errorf("write %s: %w", fileOut, err)
	}
	return buildBriteRecord(fileOut, pathRel, briteID, assetName, urlFile)
}

func inspectBriteFile(
	filePath string,
	pathRel string,
	briteID string,
	assetName string,
	urlFile string,
) (briteRecord, bool, error) {
	infoFile, err := os.Stat(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return briteRecord{}, false, nil
		}
		return briteRecord{}, false, fmt.Errorf("stat existing file: %w", err)
	}
	if infoFile.Size() <= 0 {
		return briteRecord{}, false, nil
	}
	record, err := buildBriteRecord(filePath, pathRel, briteID, assetName, urlFile)
	if err != nil {
		return briteRecord{}, false, err
	}
	return record, true, nil
}

func buildBriteRecord(
	filePath string,
	pathRel string,
	briteID string,
	assetName string,
	urlFile string,
) (briteRecord, error) {
	infoFile, err := os.Stat(filePath)
	if err != nil {
		return briteRecord{}, fmt.Errorf("stat file: %w", err)
	}
	sha256File, err := calculateSHA256ForFile(filePath)
	if err != nil {
		return briteRecord{}, err
	}
	return briteRecord{
		BriteID: briteID,
		Asset:   assetName,
		PathRel: pathRel,
		SHA256:  sha256File,
		Bytes:   infoFile.Size(),
		URL:     urlFile,
	}, nil
}

func writeBriteManifest(
	fileManifest string,
	cfg *briteConfig,
	records []briteRecord,
	timeDownloaded time.Time,
) error {
	fileTemp := fileManifest + ".tmp"
	fileOut, err := os.Create(fileTemp)
	if err != nil {
		return fmt.Errorf("create manifest: %w", err)
	}

	manifest := buildBriteManifest(cfg, records, timeDownloaded)
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

func buildBriteManifest(
	cfg *briteConfig,
	records []briteRecord,
	timeDownloaded time.Time,
) briteManifestFile {
	mapRecordsByBrite := make(map[string][]briteRecord)
	briteIDs := make([]string, 0)
	for _, record := range records {
		if record.BriteID == "" {
			continue
		}
		if _, ok := mapRecordsByBrite[record.BriteID]; !ok {
			briteIDs = append(briteIDs, record.BriteID)
		}
		mapRecordsByBrite[record.BriteID] = append(mapRecordsByBrite[record.BriteID], record)
	}
	sort.Strings(briteIDs)

	brites := make([]briteManifestEntry, 0, len(briteIDs))
	for _, briteID := range briteIDs {
		recordsBrite := mapRecordsByBrite[briteID]
		sort.Slice(recordsBrite, func(i, j int) bool {
			return recordsBrite[i].Asset < recordsBrite[j].Asset
		})
		paths := make([]string, 0, len(recordsBrite))
		for _, record := range recordsBrite {
			paths = append(paths, record.PathRel)
		}
		brites = append(brites, briteManifestEntry{
			ID:    briteID,
			Files: paths,
		})
	}

	files := make([]briteManifestFileRef, 0, len(records))
	for _, record := range records {
		files = append(files, briteManifestFileRef{
			BriteID: record.BriteID,
			Asset:   record.Asset,
			Path:    record.PathRel,
			SHA256:  record.SHA256,
			Bytes:   record.Bytes,
			URL:     record.URL,
		})
	}

	return briteManifestFile{
		Database:     "kegg",
		Asset:        "brite",
		Version:      cfg.version,
		VersionToken: cfg.versionToken,
		DownloadedAt: timeDownloaded.Format(time.RFC3339),
		Scope: manifestScope{
			Type:  deriveBriteScopeType(cfg),
			Value: deriveBriteScopeValue(cfg),
		},
		Brites: brites,
		Files:  files,
	}
}

func deriveBriteScopeValue(cfg *briteConfig) string {
	if cfg.shouldDownloadAllOrganisms {
		return "all"
	}
	return cfg.catalogCode
}

func normalizeBriteID(text string) string {
	value := strings.TrimSpace(text)
	value = strings.TrimPrefix(value, "br:")
	value = strings.TrimPrefix(value, "BR:")
	return value
}

func resolveKEGGOrganismCodes(clientKegg *keggClient) ([]string, error) {
	data, err := clientKegg.download(baseURL + "/list/organism")
	if err != nil {
		return nil, err
	}
	return parseKEGGOrganismCodesFromList(data)
}

func parseKEGGOrganismCodesFromList(data []byte) ([]string, error) {
	setCodes := make(map[string]struct{})
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		fields := strings.Split(line, "\t")
		if len(fields) < 2 {
			continue
		}
		code := strings.TrimSpace(fields[1])
		if code == "" {
			continue
		}
		setCodes[code] = struct{}{}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan KEGG organism list: %w", err)
	}
	return selectSortedKeys(setCodes), nil
}

func isValidBriteID(text string) bool {
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

func deriveBriteScopeType(cfg *briteConfig) string {
	if cfg.shouldDownloadAllOrganisms {
		return "organism_all"
	}
	if cfg.catalogCode == "br" || cfg.catalogCode == "ko" {
		return "reference"
	}
	return "organism"
}

func deriveBriteScopeDir(cfg *briteConfig) []string {
	if deriveBriteScopeType(cfg) == "reference" {
		return []string{"reference", cfg.catalogCode}
	}
	return []string{"organism", cfg.catalogCode}
}
