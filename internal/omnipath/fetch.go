package omnipath

import (
	"biofetch/internal/shared/httpx"
	"biofetch/internal/shared/logx"
	"biofetch/internal/shared/sets"
	"biofetch/internal/shared/tomlx"
	"crypto/sha256"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

const baseURL = "https://omnipathdb.org"
const archiveURL = "https://archive.omnipathdb.org/"
const queryEnzSubURL = baseURL + "/queries/enzsub"
const queryInteractionsURL = baseURL + "/queries/interactions"

type omnipathClient struct {
	clientHTTP *http.Client
	retryMax   int
	retryWait  time.Duration
}

type recordFile struct {
	Asset  string `toml:"asset"`
	Path   string `toml:"path"`
	SHA256 string `toml:"sha256"`
	Bytes  int64  `toml:"bytes"`
	URL    string `toml:"url"`
}

type manifestFile struct {
	Database     string        `toml:"database"`
	Asset        string        `toml:"asset"`
	Dataset      string        `toml:"dataset,omitempty"`
	Version      string        `toml:"version"`
	VersionToken string        `toml:"version_token"`
	DownloadedAt string        `toml:"downloaded_at"`
	Scope        manifestScope `toml:"scope"`
	RequestURL   string        `toml:"request_url"`
	QueryURL     string        `toml:"query_url"`
	Files        []recordFile  `toml:"files"`
}

type manifestScope struct {
	Type  string `toml:"type"`
	Value string `toml:"value"`
}

func runFetchEnzSub(cfg *configEnzSub) error {
	client := createClient(cfg.shouldAllowInsecureTLS, cfg.retryMax, cfg.retryWait)

	taxIDs, scopeType, scopeValue, err := resolveOmniPathTaxIDsEnzSub(client, cfg)
	if err != nil {
		return err
	}
	return runFetchCommon(fetchInput{
		asset:                   "enz_sub",
		dataset:                 "",
		taxIDs:                  taxIDs,
		urlQuery:                queryEnzSubURL,
		dirOut:                  cfg.dirOut,
		shouldOverwriteExisting: cfg.shouldOverwriteExisting,
		shouldAllowInsecureTLS:  cfg.shouldAllowInsecureTLS,
		retryMax:                cfg.retryMax,
		retryWait:               cfg.retryWait,
		shouldDryRun:            cfg.shouldDryRun,
		scopeType:               scopeType,
		scopeValue:              scopeValue,
		buildDataURL: func(taxID string) string {
			params := url.Values{}
			params.Set("format", "tsv")
			params.Set("organisms", taxID)
			if cfg.ruleLicense != "" {
				params.Set("license", strings.ToLower(strings.TrimSpace(cfg.ruleLicense)))
			}
			return baseURL + "/enzsub?" + params.Encode()
		},
	})
}

func runFetchInteractions(cfg *configInteractions) error {
	client := createClient(cfg.shouldAllowInsecureTLS, cfg.retryMax, cfg.retryWait)

	taxIDs, scopeType, scopeValue, err := resolveOmniPathTaxIDsInteractions(client, cfg)
	if err != nil {
		return err
	}
	return runFetchCommon(fetchInput{
		asset:                   "interactions",
		dataset:                 "kinaseextra",
		taxIDs:                  taxIDs,
		urlQuery:                queryInteractionsURL,
		dirOut:                  cfg.dirOut,
		shouldOverwriteExisting: cfg.shouldOverwriteExisting,
		shouldAllowInsecureTLS:  cfg.shouldAllowInsecureTLS,
		retryMax:                cfg.retryMax,
		retryWait:               cfg.retryWait,
		shouldDryRun:            cfg.shouldDryRun,
		scopeType:               scopeType,
		scopeValue:              scopeValue,
		buildDataURL: func(taxID string) string {
			params := url.Values{}
			params.Set("format", "tsv")
			params.Set("datasets", strings.ToLower(strings.TrimSpace(cfg.dataset)))
			params.Set("organisms", taxID)
			if cfg.ruleLicense != "" {
				params.Set("license", strings.ToLower(strings.TrimSpace(cfg.ruleLicense)))
			}
			return baseURL + "/interactions?" + params.Encode()
		},
	})
}

type fetchInput struct {
	asset                   string
	dataset                 string
	taxIDs                  []string
	urlQuery                string
	dirOut                  string
	shouldOverwriteExisting bool
	shouldAllowInsecureTLS  bool
	retryMax                int
	retryWait               time.Duration
	shouldDryRun            bool
	scopeType               string
	scopeValue              string
	buildDataURL            func(string) string
}

func runFetchCommon(in fetchInput) error {
	client := createClient(in.shouldAllowInsecureTLS, in.retryMax, in.retryWait)

	version, versionToken, err := resolveVersion(client, in.asset)
	if err != nil {
		return err
	}

	dirVersion := deriveVersionDir(in)
	dirVersion = filepath.Join(in.dirOut, dirVersion, versionToken)
	fileManifest := filepath.Join(dirVersion, "manifest.lock")
	fileQuery := filepath.Join(dirVersion, "raw", "query_meta.json")
	pathRelQuery := filepath.ToSlash(filepath.Join("raw", "query_meta.json"))

	if in.shouldDryRun {
		logf("[dry-run] query url: %s", in.urlQuery)
		logf("[dry-run] version dir: %s", dirVersion)
		for _, taxID := range in.taxIDs {
			logf("[dry-run] data url: %s", in.buildDataURL(taxID))
		}
		return nil
	}

	if err := os.MkdirAll(filepath.Join(dirVersion, "raw"), 0o755); err != nil {
		return fmt.Errorf("create raw dir: %w", err)
	}
	if err := os.MkdirAll(filepath.Join(dirVersion, "tidy"), 0o755); err != nil {
		return fmt.Errorf("create tidy dir: %w", err)
	}

	records := make([]recordFile, 0, 1+len(in.taxIDs))
	recordQuery, err := fetchAsset(client, fileQuery, pathRelQuery, in.urlQuery, "query_meta", in.shouldOverwriteExisting)
	if err != nil {
		return err
	}
	records = append(records, recordQuery)

	for _, taxID := range in.taxIDs {
		dirRaw := filepath.Join(dirVersion, "raw", taxID)
		dirTidy := filepath.Join(dirVersion, "tidy", taxID)
		if err := os.MkdirAll(dirRaw, 0o755); err != nil {
			return fmt.Errorf("create raw dir: %w", err)
		}
		if err := os.MkdirAll(dirTidy, 0o755); err != nil {
			return fmt.Errorf("create tidy dir: %w", err)
		}

		fileData := filepath.Join(dirRaw, in.asset+".tsv")
		pathRelData := filepath.ToSlash(filepath.Join("raw", taxID, in.asset+".tsv"))
		recordData, err := fetchAsset(client, fileData, pathRelData, in.buildDataURL(taxID), in.asset, in.shouldOverwriteExisting)
		if err != nil {
			return err
		}
		records = append(records, recordData)
	}

	records, err = buildCompleteOmniPathRecords(fileManifest, dirVersion, records)
	if err != nil {
		return err
	}

	sort.Slice(records, func(i, j int) bool {
		if records[i].Path != records[j].Path {
			return records[i].Path < records[j].Path
		}
		return records[i].Asset < records[j].Asset
	})

	scopeType, scopeValue := deriveOmniPathManifestScope(records)
	manifest := manifestFile{
		Database:     "omnipath",
		Asset:        in.asset,
		Dataset:      in.dataset,
		Version:      version,
		VersionToken: versionToken,
		DownloadedAt: time.Now().Format(time.RFC3339),
		Scope:        manifestScope{Type: scopeType, Value: scopeValue},
		RequestURL:   deriveOmniPathRequestURL(records),
		QueryURL:     deriveOmniPathQueryURL(records, in.urlQuery),
		Files:        records,
	}
	if err := writeManifest(fileManifest, manifest); err != nil {
		return err
	}

	logf("done (files=%d)", len(records))
	logf("manifest written: %s", fileManifest)
	return nil
}

func buildCompleteOmniPathRecords(
	fileManifest string,
	dirVersion string,
	recordsCurrent []recordFile,
) ([]recordFile, error) {
	recordsExisting, err := readExistingOmniPathRecords(fileManifest)
	if err != nil {
		return nil, err
	}

	recordsMerged := make(map[string]recordFile, len(recordsExisting)+len(recordsCurrent))
	for _, record := range recordsExisting {
		recordsMerged[record.Path] = record
	}
	for _, record := range recordsCurrent {
		recordsMerged[record.Path] = record
	}

	records := make([]recordFile, 0, len(recordsMerged))
	for _, record := range recordsMerged {
		filePath := filepath.Join(dirVersion, filepath.FromSlash(record.Path))
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

func readExistingOmniPathRecords(fileManifest string) ([]recordFile, error) {
	var manifest manifestFile
	ok, err := tomlx.ReadFileIfExists(fileManifest, &manifest)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, nil
	}
	return manifest.Files, nil
}

func deriveOmniPathManifestScope(records []recordFile) (string, string) {
	setTaxIDs := make(map[string]struct{})
	for _, record := range records {
		taxID := deriveOmniPathTaxIDFromPath(record.Path)
		if taxID != "" {
			setTaxIDs[taxID] = struct{}{}
		}
	}

	taxIDs := sets.SortedKeys(setTaxIDs)
	switch len(taxIDs) {
	case 0:
		return "organisms", ""
	case 1:
		return "organism", taxIDs[0]
	default:
		return "organisms", strings.Join(taxIDs, ",")
	}
}

func deriveOmniPathTaxIDFromPath(pathRel string) string {
	parts := strings.Split(pathRel, "/")
	if len(parts) < 3 || parts[0] != "raw" {
		return ""
	}
	if parts[1] == "query_meta.json" {
		return ""
	}
	return parts[1]
}

func deriveOmniPathRequestURL(records []recordFile) string {
	dataURLs := make([]string, 0, 1)
	for _, record := range records {
		if record.Asset == "query_meta" {
			continue
		}
		dataURLs = append(dataURLs, record.URL)
	}
	if len(dataURLs) != 1 {
		return ""
	}
	return dataURLs[0]
}

func deriveOmniPathQueryURL(records []recordFile, defaultURL string) string {
	for _, record := range records {
		if record.Asset == "query_meta" {
			return record.URL
		}
	}
	return defaultURL
}

func resolveOmniPathTaxIDsEnzSub(client *omnipathClient, cfg *configEnzSub) ([]string, string, string, error) {
	switch {
	case cfg.shouldDownloadAll:
		taxIDs, err := resolveAllOmniPathTaxIDs(client, queryEnzSubURL)
		if err != nil {
			return nil, "", "", err
		}
		return taxIDs, "organisms", "all", nil
	case len(cfg.organisms) > 0:
		taxIDs, err := parseOrganisms(cfg.organisms)
		if err != nil {
			return nil, "", "", err
		}
		if len(taxIDs) == 1 {
			return taxIDs, "organism", taxIDs[0], nil
		}
		return taxIDs, "organisms", strings.Join(taxIDs, ","), nil
	case strings.TrimSpace(cfg.fileOrganisms) != "":
		taxIDs, err := readOrganismsFromFile(cfg.fileOrganisms)
		if err != nil {
			return nil, "", "", err
		}
		if len(taxIDs) == 1 {
			return taxIDs, "organism", taxIDs[0], nil
		}
		return taxIDs, "organisms", strings.Join(taxIDs, ","), nil
	default:
		return nil, "", "", fmt.Errorf("no organisms configured")
	}
}

func resolveOmniPathTaxIDsInteractions(client *omnipathClient, cfg *configInteractions) ([]string, string, string, error) {
	switch {
	case cfg.shouldDownloadAll:
		taxIDs, err := resolveAllOmniPathTaxIDs(client, queryInteractionsURL)
		if err != nil {
			return nil, "", "", err
		}
		return taxIDs, "organisms", "all", nil
	case len(cfg.organisms) > 0:
		taxIDs, err := parseOrganisms(cfg.organisms)
		if err != nil {
			return nil, "", "", err
		}
		if len(taxIDs) == 1 {
			return taxIDs, "organism", taxIDs[0], nil
		}
		return taxIDs, "organisms", strings.Join(taxIDs, ","), nil
	case strings.TrimSpace(cfg.fileOrganisms) != "":
		taxIDs, err := readOrganismsFromFile(cfg.fileOrganisms)
		if err != nil {
			return nil, "", "", err
		}
		if len(taxIDs) == 1 {
			return taxIDs, "organism", taxIDs[0], nil
		}
		return taxIDs, "organisms", strings.Join(taxIDs, ","), nil
	default:
		return nil, "", "", fmt.Errorf("no organisms configured")
	}
}

func resolveAllOmniPathTaxIDs(client *omnipathClient, urlQuery string) ([]string, error) {
	dataQuery, err := client.download(urlQuery)
	if err != nil {
		return nil, fmt.Errorf("download query metadata %s: %w", urlQuery, err)
	}

	taxIDs, err := parseOrganismsFromQueryMetadata(dataQuery)
	if err != nil {
		return nil, fmt.Errorf("parse query metadata %s: %w", urlQuery, err)
	}
	return taxIDs, nil
}

func parseOrganismsFromQueryMetadata(data []byte) ([]string, error) {
	text := string(data)
	indexStart := strings.Index(text, "organisms ")
	if indexStart < 0 {
		return nil, fmt.Errorf("organisms field not found")
	}

	textAfter := text[indexStart+len("organisms "):]
	fields := strings.Fields(textAfter)
	if len(fields) == 0 {
		return nil, fmt.Errorf("organisms values not found")
	}

	valuesRaw := strings.ReplaceAll(fields[0], ";", ",")
	return parseOrganisms([]string{valuesRaw})
}

func deriveVersionDir(in fetchInput) string {
	if in.asset == "interactions" {
		return filepath.Join("interactions", in.dataset)
	}
	return in.asset
}

func fetchAsset(client *omnipathClient, pathFile string, pathRel string, urlFile string, asset string, shouldOverwrite bool) (recordFile, error) {
	if !shouldOverwrite {
		record, ok, err := inspectExisting(pathFile, pathRel, urlFile, asset)
		if err != nil {
			return recordFile{}, err
		}
		if ok {
			logf("using existing %s", filepath.Base(pathFile))
			return record, nil
		}
	}

	data, err := client.download(urlFile)
	if err != nil {
		return recordFile{}, err
	}
	if err := os.WriteFile(pathFile, data, 0o644); err != nil {
		return recordFile{}, fmt.Errorf("write %s: %w", pathFile, err)
	}
	return buildRecord(pathFile, pathRel, urlFile, asset)
}

func inspectExisting(pathFile string, pathRel string, urlFile string, asset string) (recordFile, bool, error) {
	infoFile, err := os.Stat(pathFile)
	if err != nil {
		if os.IsNotExist(err) {
			return recordFile{}, false, nil
		}
		return recordFile{}, false, fmt.Errorf("stat existing file: %w", err)
	}
	if infoFile.Size() <= 0 {
		return recordFile{}, false, nil
	}
	record, err := buildRecord(pathFile, pathRel, urlFile, asset)
	if err != nil {
		return recordFile{}, false, err
	}
	return record, true, nil
}

func buildRecord(pathFile string, pathRel string, urlFile string, asset string) (recordFile, error) {
	infoFile, err := os.Stat(pathFile)
	if err != nil {
		return recordFile{}, fmt.Errorf("stat %s: %w", pathFile, err)
	}
	hashSHA256, err := calculateSHA256(pathFile)
	if err != nil {
		return recordFile{}, err
	}
	return recordFile{Asset: asset, Path: pathRel, SHA256: hashSHA256, Bytes: infoFile.Size(), URL: urlFile}, nil
}

func calculateSHA256(pathFile string) (string, error) {
	fileIn, err := os.Open(pathFile)
	if err != nil {
		return "", fmt.Errorf("open %s for sha256: %w", pathFile, err)
	}
	defer fileIn.Close()

	hash := sha256.New()
	if _, err := io.Copy(hash, fileIn); err != nil {
		return "", fmt.Errorf("hash %s: %w", pathFile, err)
	}
	return fmt.Sprintf("%x", hash.Sum(nil)), nil
}

func writeManifest(fileManifest string, manifest manifestFile) error {
	return tomlx.WriteFileAtomic(fileManifest, manifest)
}

func createClient(shouldAllowInsecureTLS bool, retryMax int, retryWait time.Duration) *omnipathClient {
	return &omnipathClient{
		clientHTTP: httpx.NewClient(shouldAllowInsecureTLS),
		retryMax:   retryMax,
		retryWait:  retryWait,
	}
}

func (client *omnipathClient) download(urlFile string) ([]byte, error) {
	var errLast error
	for attempt := 1; attempt <= client.retryMax; attempt++ {
		resp, err := client.clientHTTP.Get(urlFile)
		if err != nil {
			errLast = err
		} else {
			data, errRead := io.ReadAll(resp.Body)
			resp.Body.Close()
			if errRead != nil {
				errLast = errRead
			} else if resp.StatusCode < 200 || resp.StatusCode >= 300 {
				errLast = fmt.Errorf("unexpected status %s", resp.Status)
			} else {
				return data, nil
			}
		}
		if attempt < client.retryMax {
			time.Sleep(client.retryWait)
		}
	}
	return nil, fmt.Errorf("request %s failed after %d attempts: %w", urlFile, client.retryMax, errLast)
}

func resolveVersion(client *omnipathClient, asset string) (string, string, error) {
	dataIndex, err := client.download(archiveURL)
	if err != nil {
		return "", "", err
	}
	version, err := extractVersionFromArchiveIndex(dataIndex, asset)
	if err != nil {
		return "", "", fmt.Errorf("resolve upstream version: %w", err)
	}
	return version, sanitizeVersionToken(version), nil
}

func extractVersionFromArchiveIndex(data []byte, asset string) (string, error) {
	text := string(data)
	pattern := fmt.Sprintf(`omnipath_webservice_%s__([0-9]{8})-([0-9]{8})[^"]*`, regexp.QuoteMeta(asset))
	re := regexp.MustCompile(pattern)
	matchesAll := re.FindAllStringSubmatch(text, -1)
	if len(matchesAll) == 0 {
		return "", fmt.Errorf("no archive version found for asset %s", asset)
	}
	matchLast := matchesAll[len(matchesAll)-1]
	if len(matchLast) < 3 {
		return "", fmt.Errorf("archive version parse failed for asset %s", asset)
	}
	return formatArchiveDate(matchLast[2])
}

func sanitizeVersionToken(version string) string {
	replacer := strings.NewReplacer("/", "_", " ", "_", ":", "_", "\\", "_")
	return replacer.Replace(strings.TrimSpace(version))
}

func formatArchiveDate(value string) (string, error) {
	if len(value) != 8 {
		return "", fmt.Errorf("invalid archive date: %s", value)
	}
	return value[0:4] + "-" + value[4:6] + "-" + value[6:8], nil
}

func logf(format string, args ...interface{}) {
	logx.Logf("biofetch omnipath", format, args...)
}
