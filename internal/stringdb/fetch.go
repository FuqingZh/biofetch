package stringdb

import (
	"github.com/FuqingZh/biofetch/internal/shared/cliopt"
	"github.com/FuqingZh/biofetch/internal/shared/filehash"
	"github.com/FuqingZh/biofetch/internal/shared/httpx"
	"github.com/FuqingZh/biofetch/internal/shared/logx"
	"github.com/FuqingZh/biofetch/internal/shared/parallel"
	"github.com/FuqingZh/biofetch/internal/shared/sets"
	"github.com/FuqingZh/biofetch/internal/shared/tomlx"
	"bufio"
	"compress/gzip"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
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

type downloadTask struct {
	filePath   string
	speciesID  string
	assetName  string
	pathRel    string
	urlFile    string
	textAction string
}

type manifestFile struct {
	Database     string             `toml:"database"`
	Asset        string             `toml:"asset"`
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
	limiterRequest := httpx.NewRequestLimiter(cfg.RequestInterval)
	taxIDs, err := resolveTaxIDs(cfg, limiterRequest)
	if err != nil {
		return err
	}

	dirVersion := filepath.Join(cfg.dirOut, "network", cfg.versionToken)
	_, closeRun, err := logx.StartVersionedRun("biofetch string", "fetch", cfg.dirLogs, dirVersion)
	if err != nil {
		return err
	}
	defer closeRun()
	dirRaw := filepath.Join(dirVersion, "raw")
	fileManifest := filepath.Join(dirVersion, "manifest.lock")

	if cfg.shouldDryRun {
		logf("dry-run version dir: %s", dirVersion)
		logf("dry-run manifest: %s", fileManifest)
	} else {
		if err := os.MkdirAll(dirRaw, 0o755); err != nil {
			return fmt.Errorf("create raw dir: %w", err)
		}
	}

	if cfg.shouldDryRun {
		for _, taxID := range taxIDs {
			dirSpeciesRaw := filepath.Join(dirRaw, taxID)
			for _, assetName := range assetTypes {
				fileName := fmt.Sprintf("%s.%s.%s.txt.gz", taxID, assetName, cfg.versionToken)
				urlFile := fmt.Sprintf("%s/%s.%s/%s", baseURL, assetName, cfg.versionToken, fileName)
				fileOut := filepath.Join(dirSpeciesRaw, fileName)
				logf("[dry-run] %s -> %s", urlFile, fileOut)
			}
		}
		logf("dry-run done (taxids=%d, assets_per_taxid=%d)", len(taxIDs), len(assetTypes))
		return nil
	}

	recordsReused, tasksDownload, err := planFetchDownloadTasks(cfg, taxIDs, dirRaw)
	if err != nil {
		return err
	}

	clientHTTP := createHTTPClient(cfg.shouldAllowInsecureTLS)
	recordsDownloaded, err := runDownloadTasks(
		clientHTTP,
		tasksDownload,
		cfg.retryMax,
		cfg.retryWait,
		cfg.WorkersMax,
		limiterRequest,
	)
	if err != nil {
		return err
	}

	records := make([]fileRecord, 0, len(recordsReused)+len(recordsDownloaded))
	records = append(records, recordsReused...)
	records = append(records, recordsDownloaded...)

	sort.Slice(records, func(i, j int) bool {
		if records[i].speciesID != records[j].speciesID {
			return records[i].speciesID < records[j].speciesID
		}
		return records[i].assetName < records[j].assetName
	})

	recordsComplete, err := buildCompleteFileRecords(fileManifest, dirVersion, records)
	if err != nil {
		return err
	}

	if err := writeManifest(fileManifest, cfg.versionToken, recordsComplete, time.Now()); err != nil {
		return err
	}

	logf("done (files=%d, taxids=%d)", len(recordsComplete), len(taxIDs))
	logf("manifest written: %s", fileManifest)
	return nil
}

func resolveTaxIDs(cfg *config, limiterRequest *httpx.RequestLimiter) ([]string, error) {
	switch {
	case cfg.shouldDownloadAll:
		return fetchAllSpeciesTaxIDs(cfg, limiterRequest)
	case len(cfg.taxIDs) > 0:
		return parseTaxIDs(cfg.taxIDs)
	default:
		return nil, fmt.Errorf("no taxids configured")
	}
}

func parseTaxIDs(valuesInput []string) ([]string, error) {
	valuesResolved, err := cliopt.ExpandListTokens(valuesInput, "", "taxids")
	if err != nil {
		return nil, err
	}
	valuesValid := make([]string, 0, len(valuesResolved))
	for _, value := range valuesResolved {
		taxID := strings.TrimSpace(value)
		if taxID == "" {
			continue
		}
		if !isDigits(taxID) {
			return nil, fmt.Errorf("invalid taxid: %s", taxID)
		}
		valuesValid = append(valuesValid, taxID)
	}
	if len(valuesValid) == 0 {
		return nil, fmt.Errorf("taxids must not be empty")
	}
	return cliopt.SortedUniqueStrings(valuesValid), nil
}

func fetchAllSpeciesTaxIDs(cfg *config, limiterRequest *httpx.RequestLimiter) ([]string, error) {
	urlSpecies := fmt.Sprintf("%s/species.%s.txt", baseURL, cfg.versionToken)
	clientHTTP := createHTTPClient(cfg.shouldAllowInsecureTLS)

	limiterRequest.Wait()
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

	return sets.SortedKeys(setTaxIDs), nil
}

func createHTTPClient(shouldAllowInsecureTLS bool) *http.Client {
	return httpx.NewClient(shouldAllowInsecureTLS)
}

func planFetchDownloadTasks(cfg *config, taxIDs []string, dirRaw string) ([]fileRecord, []downloadTask, error) {
	recordsReused := make([]fileRecord, 0, len(taxIDs)*len(assetTypes))
	tasksDownload := make([]downloadTask, 0, len(taxIDs)*len(assetTypes))

	for _, taxID := range taxIDs {
		dirSpeciesRaw := filepath.Join(dirRaw, taxID)
		for _, assetName := range assetTypes {
			fileName := fmt.Sprintf("%s.%s.%s.txt.gz", taxID, assetName, cfg.versionToken)
			urlFile := fmt.Sprintf("%s/%s.%s/%s", baseURL, assetName, cfg.versionToken, fileName)
			fileOut := filepath.Join(dirSpeciesRaw, fileName)
			pathRel := filepath.ToSlash(filepath.Join("raw", taxID, fileName))

			if !cfg.shouldOverwriteExisting {
				recordExisting, ok, err := inspectExistingFile(
					fileOut,
					taxID,
					assetName,
					pathRel,
					urlFile,
				)
				if err != nil {
					return nil, nil, err
				}
				if ok {
					logf("using existing %s", fileName)
					recordsReused = append(recordsReused, recordExisting)
					continue
				}
			}

			tasksDownload = append(tasksDownload, downloadTask{
				filePath:   fileOut,
				speciesID:  taxID,
				assetName:  assetName,
				pathRel:    pathRel,
				urlFile:    urlFile,
				textAction: fmt.Sprintf("downloading %s", fileName),
			})
		}
	}

	return recordsReused, tasksDownload, nil
}

func planSyncDownloadTasks(
	dirVersion string,
	recordsManifest []fileRecord,
	shouldOverwriteExisting bool,
) ([]fileRecord, []downloadTask, error) {
	recordsReused := make([]fileRecord, 0, len(recordsManifest))
	tasksDownload := make([]downloadTask, 0, len(recordsManifest))

	for _, record := range recordsManifest {
		filePath := filepath.Join(dirVersion, filepath.FromSlash(record.pathRel))
		if !shouldOverwriteExisting {
			recordCurrent, ok, err := inspectExistingFile(
				filePath,
				record.speciesID,
				record.assetName,
				record.pathRel,
				record.url,
			)
			if err != nil {
				return nil, nil, err
			}
			if ok && recordCurrent.sha256 == record.sha256 {
				recordsReused = append(recordsReused, recordCurrent)
				continue
			}
		}

		tasksDownload = append(tasksDownload, downloadTask{
			filePath:   filePath,
			speciesID:  record.speciesID,
			assetName:  record.assetName,
			pathRel:    record.pathRel,
			urlFile:    record.url,
			textAction: fmt.Sprintf("restore downloading %s", filepath.Base(filePath)),
		})
	}

	return recordsReused, tasksDownload, nil
}

func runDownloadTasks(
	clientHTTP *http.Client,
	tasksDownload []downloadTask,
	retryMax int,
	retryWait time.Duration,
	workersMax int,
	limiterRequest *httpx.RequestLimiter,
) ([]fileRecord, error) {
	return parallel.MapOrderedWithWorkers(
		tasksDownload,
		workersMax,
		func(task downloadTask) (fileRecord, error) {
			logf("%s", task.textAction)
			if err := os.MkdirAll(filepath.Dir(task.filePath), 0o755); err != nil {
				return fileRecord{}, fmt.Errorf("create restore dir: %w", err)
			}
			if err := downloadFileWithRetry(
				clientHTTP,
				task.urlFile,
				task.filePath,
				retryMax,
				retryWait,
				limiterRequest,
			); err != nil {
				return fileRecord{}, err
			}
			return buildFileRecord(task.filePath, task.speciesID, task.assetName, task.pathRel, task.urlFile)
		},
	)
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
	limiterRequest *httpx.RequestLimiter,
) error {
	filePart := fileOut + ".part"
	var errLast error

	for attempt := 1; attempt <= retryMax; attempt++ {
		limiterRequest.Wait()
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
	return httpx.DownloadFileWithResume(clientHTTP, urlFile, fileOut, nil)
}

func calculateSHA256ForFile(filePath string) (string, error) {
	fileIn, err := os.Open(filePath)
	if err != nil {
		return "", fmt.Errorf("open %s for sha256: %w", filePath, err)
	}
	defer fileIn.Close()

	digest, err := filehash.SHA256(fileIn)
	if err != nil {
		return "", fmt.Errorf("hash %s: %w", filePath, err)
	}
	return digest, nil
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
	manifest := buildManifestFile(versionToken, records, timeDownloaded)
	return tomlx.WriteFileAtomic(fileManifest, manifest)
}

func buildCompleteFileRecords(fileManifest string, dirVersion string, recordsCurrent []fileRecord) ([]fileRecord, error) {
	recordsExisting, err := readExistingFileRecords(fileManifest)
	if err != nil {
		return nil, err
	}

	recordsMerged := make(map[string]fileRecord, len(recordsExisting)+len(recordsCurrent))
	for _, record := range recordsExisting {
		recordsMerged[record.pathRel] = record
	}
	for _, record := range recordsCurrent {
		recordsMerged[record.pathRel] = record
	}

	records := make([]fileRecord, 0, len(recordsMerged))
	for _, record := range recordsMerged {
		filePath := filepath.Join(dirVersion, filepath.FromSlash(record.pathRel))
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
		if records[i].speciesID != records[j].speciesID {
			return records[i].speciesID < records[j].speciesID
		}
		return records[i].assetName < records[j].assetName
	})
	return records, nil
}

func readExistingFileRecords(fileManifest string) ([]fileRecord, error) {
	var manifest manifestFile
	ok, err := tomlx.ReadFileIfExists(fileManifest, &manifest)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, nil
	}

	records := make([]fileRecord, 0, len(manifest.Files))
	for _, item := range manifest.Files {
		records = append(records, fileRecord{
			speciesID: item.SpeciesID,
			assetName: item.Asset,
			pathRel:   item.Path,
			sha256:    item.SHA256,
			bytes:     item.Bytes,
			url:       item.URL,
		})
	}
	return records, nil
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
		Asset:        "network",
		Version:      strings.TrimPrefix(versionToken, "v"),
		VersionToken: versionToken,
		DownloadedAt: timeDownloaded.Format(time.RFC3339),
		Species:      species,
		Files:        files,
	}
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
	logx.Logf("biofetch string", format, args...)
}
