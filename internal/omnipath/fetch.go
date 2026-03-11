package omnipath

import (
	"crypto/sha256"
	"crypto/tls"
	"encoding/json"
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

	toml "github.com/pelletier/go-toml/v2"
)

const baseURL = "https://omnipathdb.org"

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
	taxID, _ := normalizeOrganism(cfg.organism)
	params := url.Values{}
	params.Set("format", "tsv")
	params.Set("organisms", taxID)
	if cfg.ruleLicense != "" {
		params.Set("license", strings.ToLower(strings.TrimSpace(cfg.ruleLicense)))
	}

	urlData := baseURL + "/enzsub?" + params.Encode()
	urlQuery := baseURL + "/queries/enzsub"
	return runFetchCommon(fetchInput{
		asset:                   "enz_sub",
		dataset:                 "",
		taxID:                   taxID,
		urlData:                 urlData,
		urlQuery:                urlQuery,
		dirOut:                  cfg.dirOut,
		shouldOverwriteExisting: cfg.shouldOverwriteExisting,
		shouldAllowInsecureTLS:  cfg.shouldAllowInsecureTLS,
		retryMax:                cfg.retryMax,
		retryWait:               cfg.retryWait,
		shouldDryRun:            cfg.shouldDryRun,
	})
}

func runFetchInteractions(cfg *configInteractions) error {
	taxID, _ := normalizeOrganism(cfg.organism)
	params := url.Values{}
	params.Set("format", "tsv")
	params.Set("datasets", strings.ToLower(strings.TrimSpace(cfg.dataset)))
	params.Set("organisms", taxID)
	if cfg.ruleLicense != "" {
		params.Set("license", strings.ToLower(strings.TrimSpace(cfg.ruleLicense)))
	}

	urlData := baseURL + "/interactions?" + params.Encode()
	urlQuery := baseURL + "/queries/interactions"
	return runFetchCommon(fetchInput{
		asset:                   "interactions",
		dataset:                 "kinaseextra",
		taxID:                   taxID,
		urlData:                 urlData,
		urlQuery:                urlQuery,
		dirOut:                  cfg.dirOut,
		shouldOverwriteExisting: cfg.shouldOverwriteExisting,
		shouldAllowInsecureTLS:  cfg.shouldAllowInsecureTLS,
		retryMax:                cfg.retryMax,
		retryWait:               cfg.retryWait,
		shouldDryRun:            cfg.shouldDryRun,
	})
}

type fetchInput struct {
	asset                   string
	dataset                 string
	taxID                   string
	urlData                 string
	urlQuery                string
	dirOut                  string
	shouldOverwriteExisting bool
	shouldAllowInsecureTLS  bool
	retryMax                int
	retryWait               time.Duration
	shouldDryRun            bool
}

func runFetchCommon(in fetchInput) error {
	client := createClient(in.shouldAllowInsecureTLS, in.retryMax, in.retryWait)

	version, versionToken, err := resolveVersion(client, in.urlQuery)
	if err != nil {
		return err
	}

	dirVersion := deriveVersionDir(in)
	dirVersion = filepath.Join(in.dirOut, dirVersion, versionToken)
	dirRaw := filepath.Join(dirVersion, "raw", in.taxID)
	dirTidy := filepath.Join(dirVersion, "tidy", in.taxID)
	fileManifest := filepath.Join(dirVersion, "manifest.lock")

	fileData := filepath.Join(dirRaw, in.asset+".tsv")
	fileQuery := filepath.Join(dirRaw, "query_meta.json")
	pathRelData := filepath.ToSlash(filepath.Join("raw", in.taxID, in.asset+".tsv"))
	pathRelQuery := filepath.ToSlash(filepath.Join("raw", in.taxID, "query_meta.json"))

	if in.shouldDryRun {
		logf("[dry-run] data url: %s", in.urlData)
		logf("[dry-run] query url: %s", in.urlQuery)
		logf("[dry-run] version dir: %s", dirVersion)
		return nil
	}

	if err := os.MkdirAll(dirRaw, 0o755); err != nil {
		return fmt.Errorf("create raw dir: %w", err)
	}
	if err := os.MkdirAll(dirTidy, 0o755); err != nil {
		return fmt.Errorf("create tidy dir: %w", err)
	}

	records := make([]recordFile, 0, 2)
	for _, item := range []struct {
		asset string
		path  string
		rel   string
		url   string
	}{
		{asset: in.asset, path: fileData, rel: pathRelData, url: in.urlData},
		{asset: "query_meta", path: fileQuery, rel: pathRelQuery, url: in.urlQuery},
	} {
		record, err := fetchAsset(client, item.path, item.rel, item.url, item.asset, in.shouldOverwriteExisting)
		if err != nil {
			return err
		}
		records = append(records, record)
	}

	sort.Slice(records, func(i, j int) bool { return records[i].Asset < records[j].Asset })

	manifest := manifestFile{
		Database:     "omnipath",
		Asset:        in.asset,
		Dataset:      in.dataset,
		Version:      version,
		VersionToken: versionToken,
		DownloadedAt: time.Now().Format(time.RFC3339),
		Scope:        manifestScope{Type: "organism", Value: in.taxID},
		RequestURL:   in.urlData,
		QueryURL:     in.urlQuery,
		Files:        records,
	}
	if err := writeManifest(fileManifest, manifest); err != nil {
		return err
	}

	logf("done (files=%d)", len(records))
	logf("manifest written: %s", fileManifest)
	return nil
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
	data, err := toml.Marshal(manifest)
	if err != nil {
		return fmt.Errorf("encode manifest: %w", err)
	}
	if err := os.WriteFile(fileManifest, data, 0o644); err != nil {
		return fmt.Errorf("write manifest: %w", err)
	}
	return nil
}

func createClient(shouldAllowInsecureTLS bool, retryMax int, retryWait time.Duration) *omnipathClient {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	if shouldAllowInsecureTLS {
		transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
	}
	return &omnipathClient{
		clientHTTP: &http.Client{
			Transport: transport,
			Timeout:   0,
		},
		retryMax:  retryMax,
		retryWait: retryWait,
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

func resolveVersion(client *omnipathClient, urlQuery string) (string, string, error) {
	dataMeta, err := client.download(urlQuery)
	if err != nil {
		return "", "", err
	}
	version, err := extractVersionFromMetadata(dataMeta)
	if err != nil {
		return "", "", fmt.Errorf("resolve upstream version: %w", err)
	}
	return version, sanitizeVersionToken(version), nil
}

func extractVersionFromMetadata(data []byte) (string, error) {
	var object map[string]interface{}
	if err := json.Unmarshal(data, &object); err == nil {
		keys := []string{"version", "release", "updated", "last_updated", "date"}
		for _, key := range keys {
			value, ok := object[key]
			if !ok {
				continue
			}
			text := strings.TrimSpace(fmt.Sprintf("%v", value))
			if text != "" {
				return text, nil
			}
		}
	}

	text := string(data)
	re := regexp.MustCompile(`(?i)(version|release|updated|last[_ ]updated|date)[^0-9]*([0-9]{4}[-_/][0-9]{2}[-_/][0-9]{2}|[0-9]+\.[0-9]+(?:\.[0-9]+)?)`)
	matches := re.FindStringSubmatch(text)
	if len(matches) >= 3 {
		return matches[2], nil
	}
	return "", fmt.Errorf("no version metadata found")
}

func sanitizeVersionToken(version string) string {
	replacer := strings.NewReplacer("/", "_", " ", "_", ":", "_", "\\", "_")
	return replacer.Replace(strings.TrimSpace(version))
}

func logf(format string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, "[fetch_omnipath] %s\n", fmt.Sprintf(format, args...))
}
