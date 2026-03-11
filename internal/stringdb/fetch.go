package stringdb

import (
	"bufio"
	"compress/gzip"
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

const baseURL = "https://stringdb-downloads.org/download"

var assetTypes = []string{
	"protein.links",
	"protein.aliases",
	"protein.info",
}

type fileRecord struct {
	speciesID string
	assetName string
	pathRel   string
	sha256    string
	bytes     int64
	url       string
}

type manifestFile struct {
	Database     string             `toml:"database"`
	Version      string             `toml:"version"`
	VersionToken string             `toml:"version_token"`
	DownloadedAt string             `toml:"downloaded_at"`
	Species      []manifestSpecies  `toml:"species"`
	Files        []manifestFileItem `toml:"files"`
}

type manifestSpecies struct {
	ID    string   `toml:"id"`
	Files []string `toml:"files"`
}

type manifestFileItem struct {
	SpeciesID string `toml:"species_id"`
	Asset     string `toml:"asset"`
	Path      string `toml:"path"`
	SHA256    string `toml:"sha256"`
	Bytes     int64  `toml:"bytes"`
	URL       string `toml:"url"`
}

func runFetch(cfg *config) error {
	taxIDs, err := resolveTaxIDs(cfg)
	if err != nil {
		return err
	}

	dirVersion := filepath.Join(cfg.dirOut, cfg.versionToken)
	dirRaw := filepath.Join(dirVersion, "raw")
	dirTidy := filepath.Join(dirVersion, "tidy")
	fileManifest := filepath.Join(dirVersion, "manifest.lock")

	if cfg.shouldDryRun {
		logf("dry-run version dir: %s", dirVersion)
		logf("dry-run manifest: %s", fileManifest)
	} else {
		if err := os.MkdirAll(dirRaw, 0o755); err != nil {
			return fmt.Errorf("create raw dir: %w", err)
		}
		if err := os.MkdirAll(dirTidy, 0o755); err != nil {
			return fmt.Errorf("create tidy dir: %w", err)
		}
	}

	clientHTTP := createHTTPClient(cfg.shouldAllowInsecureTLS)
	records := make([]fileRecord, 0, len(taxIDs)*len(assetTypes))

	for _, taxID := range taxIDs {
		dirSpeciesRaw := filepath.Join(dirRaw, taxID)
		if !cfg.shouldDryRun {
			if err := os.MkdirAll(dirSpeciesRaw, 0o755); err != nil {
				return fmt.Errorf("create species raw dir: %w", err)
			}
		}

		for _, assetName := range assetTypes {
			fileName := fmt.Sprintf("%s.%s.%s.txt.gz", taxID, assetName, cfg.versionToken)
			urlFile := fmt.Sprintf("%s/%s.%s/%s", baseURL, assetName, cfg.versionToken, fileName)
			fileOut := filepath.Join(dirSpeciesRaw, fileName)
			pathRel := filepath.ToSlash(filepath.Join("raw", taxID, fileName))

			if cfg.shouldDryRun {
				logf("[dry-run] %s -> %s", urlFile, fileOut)
				continue
			}

			if !cfg.shouldOverwriteExisting {
				recordExisting, ok, err := inspectExistingFile(
					fileOut,
					taxID,
					assetName,
					pathRel,
					urlFile,
				)
				if err != nil {
					return err
				}
				if ok {
					logf("using existing %s", fileName)
					records = append(records, recordExisting)
					continue
				}
			}

			logf("downloading %s", fileName)
			if err := downloadFileWithRetry(
				clientHTTP,
				urlFile,
				fileOut,
				cfg.retryMax,
				cfg.retryWait,
			); err != nil {
				return err
			}

			recordFile, err := buildFileRecord(fileOut, taxID, assetName, pathRel, urlFile)
			if err != nil {
				return err
			}
			records = append(records, recordFile)
		}
	}

	if cfg.shouldDryRun {
		logf("dry-run done (taxids=%d, assets_per_taxid=%d)", len(taxIDs), len(assetTypes))
		return nil
	}

	sort.Slice(records, func(i, j int) bool {
		if records[i].speciesID != records[j].speciesID {
			return records[i].speciesID < records[j].speciesID
		}
		return records[i].assetName < records[j].assetName
	})

	if err := writeManifest(fileManifest, cfg.versionToken, records, time.Now()); err != nil {
		return err
	}

	logf("done (files=%d, taxids=%d)", len(records), len(taxIDs))
	logf("manifest written: %s", fileManifest)
	return nil
}

func resolveTaxIDs(cfg *config) ([]string, error) {
	switch {
	case cfg.shouldDownloadAllSpecies:
		return fetchAllSpeciesTaxIDs(cfg)
	case strings.TrimSpace(cfg.taxIDsCSV) != "":
		return parseTaxIDsCSV(cfg.taxIDsCSV)
	default:
		return readTaxIDsFromFile(cfg.fileTaxIDs)
	}
}

func parseTaxIDsCSV(textCSV string) ([]string, error) {
	setTaxIDs := make(map[string]struct{})
	for _, token := range strings.Split(textCSV, ",") {
		taxID := strings.TrimSpace(token)
		if taxID == "" {
			continue
		}
		if !isDigits(taxID) {
			return nil, fmt.Errorf("invalid taxid: %s", taxID)
		}
		setTaxIDs[taxID] = struct{}{}
	}

	return selectSortedKeys(setTaxIDs), nil
}

func readTaxIDsFromFile(fileTaxIDs string) ([]string, error) {
	fileIn, err := os.Open(fileTaxIDs)
	if err != nil {
		return nil, fmt.Errorf("open taxids file: %w", err)
	}
	defer fileIn.Close()

	setTaxIDs := make(map[string]struct{})
	scanner := bufio.NewScanner(fileIn)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if !isDigits(line) {
			return nil, fmt.Errorf("invalid taxid in %s: %s", fileTaxIDs, line)
		}
		setTaxIDs[line] = struct{}{}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read taxids file: %w", err)
	}

	return selectSortedKeys(setTaxIDs), nil
}

func fetchAllSpeciesTaxIDs(cfg *config) ([]string, error) {
	urlSpecies := fmt.Sprintf("%s/species.%s.txt", baseURL, cfg.versionToken)
	clientHTTP := createHTTPClient(cfg.shouldAllowInsecureTLS)

	response, err := clientHTTP.Get(urlSpecies)
	if err != nil {
		return nil, fmt.Errorf("download species list: %w", err)
	}
	defer response.Body.Close()

	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, fmt.Errorf("download species list: unexpected status %s", response.Status)
	}

	setTaxIDs := make(map[string]struct{})
	scanner := bufio.NewScanner(response.Body)
	shouldSkipHeader := true
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		if shouldSkipHeader {
			shouldSkipHeader = false
			if strings.HasPrefix(line, "taxon_id") {
				continue
			}
		}

		fields := strings.Split(line, "\t")
		if len(fields) == 0 {
			continue
		}

		taxID := strings.TrimSpace(fields[0])
		if isDigits(taxID) {
			setTaxIDs[taxID] = struct{}{}
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read species list: %w", err)
	}

	return selectSortedKeys(setTaxIDs), nil
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

func inspectExistingFile(
	filePath string,
	speciesID string,
	assetName string,
	pathRel string,
	urlFile string,
) (fileRecord, bool, error) {
	infoFile, err := os.Stat(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return fileRecord{}, false, nil
		}
		return fileRecord{}, false, fmt.Errorf("stat existing file: %w", err)
	}
	if infoFile.Size() <= 0 {
		return fileRecord{}, false, nil
	}

	if filepath.Ext(filePath) == ".gz" {
		if err := validateGzipFile(filePath); err != nil {
			logf("existing gzip failed integrity check; re-downloading %s", filepath.Base(filePath))
			return fileRecord{}, false, nil
		}
	}

	recordFile, err := buildFileRecord(filePath, speciesID, assetName, pathRel, urlFile)
	if err != nil {
		return fileRecord{}, false, err
	}
	return recordFile, true, nil
}

func buildFileRecord(
	filePath string,
	speciesID string,
	assetName string,
	pathRel string,
	urlFile string,
) (fileRecord, error) {
	infoFile, err := os.Stat(filePath)
	if err != nil {
		return fileRecord{}, fmt.Errorf("stat file: %w", err)
	}

	sha256File, err := calculateSHA256ForFile(filePath)
	if err != nil {
		return fileRecord{}, err
	}

	return fileRecord{
		speciesID: speciesID,
		assetName: assetName,
		pathRel:   pathRel,
		sha256:    sha256File,
		bytes:     infoFile.Size(),
		url:       urlFile,
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

	return fmt.Errorf(
		"download failed after %d attempts for %s: %w",
		retryMax,
		urlFile,
		errLast,
	)
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

func validateGzipFile(filePath string) error {
	fileIn, err := os.Open(filePath)
	if err != nil {
		return fmt.Errorf("open gzip %s: %w", filePath, err)
	}
	defer fileIn.Close()

	readerGzip, err := gzip.NewReader(fileIn)
	if err != nil {
		return fmt.Errorf("open gzip reader %s: %w", filePath, err)
	}
	defer readerGzip.Close()

	if _, err := io.Copy(io.Discard, readerGzip); err != nil {
		return fmt.Errorf("read gzip %s: %w", filePath, err)
	}
	return nil
}

func writeManifest(
	fileManifest string,
	versionToken string,
	records []fileRecord,
	timeDownloaded time.Time,
) error {
	fileTemp := fileManifest + ".tmp"
	fileOut, err := os.Create(fileTemp)
	if err != nil {
		return fmt.Errorf("create manifest: %w", err)
	}

	manifest := buildManifestFile(versionToken, records, timeDownloaded)
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
	versionToken string,
	records []fileRecord,
	timeDownloaded time.Time,
) manifestFile {
	mapRecordsBySpecies := make(map[string][]fileRecord)
	speciesIDs := make([]string, 0)
	for _, record := range records {
		if _, ok := mapRecordsBySpecies[record.speciesID]; !ok {
			speciesIDs = append(speciesIDs, record.speciesID)
		}
		mapRecordsBySpecies[record.speciesID] = append(mapRecordsBySpecies[record.speciesID], record)
	}
	sort.Strings(speciesIDs)

	species := make([]manifestSpecies, 0, len(speciesIDs))
	for _, speciesID := range speciesIDs {
		recordsSpecies := mapRecordsBySpecies[speciesID]
		sort.Slice(recordsSpecies, func(i, j int) bool {
			return recordsSpecies[i].assetName < recordsSpecies[j].assetName
		})

		paths := make([]string, 0, len(recordsSpecies))
		for _, record := range recordsSpecies {
			paths = append(paths, record.pathRel)
		}

		species = append(species, manifestSpecies{
			ID:    speciesID,
			Files: paths,
		})
	}

	files := make([]manifestFileItem, 0, len(records))
	for _, record := range records {
		files = append(files, manifestFileItem{
			SpeciesID: record.speciesID,
			Asset:     record.assetName,
			Path:      record.pathRel,
			SHA256:    record.sha256,
			Bytes:     record.bytes,
			URL:       record.url,
		})
	}

	return manifestFile{
		Database:     "string",
		Version:      strings.TrimPrefix(versionToken, "v"),
		VersionToken: versionToken,
		DownloadedAt: timeDownloaded.Format(time.RFC3339),
		Species:      species,
		Files:        files,
	}
}

func selectSortedKeys(setText map[string]struct{}) []string {
	values := make([]string, 0, len(setText))
	for key := range setText {
		values = append(values, key)
	}
	sort.Strings(values)
	return values
}

func isDigits(text string) bool {
	if text == "" {
		return false
	}
	for _, char := range text {
		if char < '0' || char > '9' {
			return false
		}
	}
	return true
}

func logf(format string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, "[biofetch string] %s\n", fmt.Sprintf(format, args...))
}
