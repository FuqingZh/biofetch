package stringdb

import (
	"bytes"
	"compress/gzip"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	toml "github.com/pelletier/go-toml/v2"
)

func TestParseTaxIDs(t *testing.T) {
	values, err := parseTaxIDs([]string{"9606", "7070,9606"})
	if err != nil {
		t.Fatalf("parseTaxIDs returned error: %v", err)
	}

	expected := []string{"7070", "9606"}
	if !reflect.DeepEqual(values, expected) {
		t.Fatalf("parseTaxIDs = %#v, want %#v", values, expected)
	}
}

func TestParseTaxIDsSupportsAtFile(t *testing.T) {
	fileTaxIDs := filepath.Join(t.TempDir(), "taxids.txt")
	content := "# comment\n7070\n\n9606\n"
	if err := os.WriteFile(fileTaxIDs, []byte(content), 0o644); err != nil {
		t.Fatalf("os.WriteFile returned error: %v", err)
	}

	values, err := parseTaxIDs([]string{"@" + fileTaxIDs})
	if err != nil {
		t.Fatalf("parseTaxIDs returned error: %v", err)
	}

	expected := []string{"7070", "9606"}
	if !reflect.DeepEqual(values, expected) {
		t.Fatalf("parseTaxIDs = %#v, want %#v", values, expected)
	}
}

func TestReadTaxIDsFromFile(t *testing.T) {
	dirTemp := t.TempDir()
	fileTaxIDs := filepath.Join(dirTemp, "taxids.txt")
	content := "# comment\n7070\n\n9606\n7070\n"
	if err := os.WriteFile(fileTaxIDs, []byte(content), 0o644); err != nil {
		t.Fatalf("os.WriteFile returned error: %v", err)
	}

	values, err := readTaxIDsFromFile(fileTaxIDs)
	if err != nil {
		t.Fatalf("readTaxIDsFromFile returned error: %v", err)
	}

	expected := []string{"7070", "9606"}
	if !reflect.DeepEqual(values, expected) {
		t.Fatalf("readTaxIDsFromFile = %#v, want %#v", values, expected)
	}
}

func TestBuildManifestFile(t *testing.T) {
	records := []fileRecord{
		{
			speciesID: "7070",
			assetName: "protein.aliases",
			pathRel:   "raw/7070/7070.protein.aliases.v12.0.txt.gz",
			sha256:    "sha-aliases",
			bytes:     11,
			url:       "https://example.org/aliases",
		},
		{
			speciesID: "7070",
			assetName: "protein.links",
			pathRel:   "raw/7070/7070.protein.links.v12.0.txt.gz",
			sha256:    "sha-links",
			bytes:     22,
			url:       "https://example.org/links",
		},
	}

	manifest := buildManifestFile(
		"v12.0",
		records,
		time.Date(2026, time.March, 10, 12, 0, 0, 0, time.FixedZone("UTC+8", 8*3600)),
	)

	var buffer bytes.Buffer
	if err := toml.NewEncoder(&buffer).Encode(manifest); err != nil {
		t.Fatalf("toml.NewEncoder returned error: %v", err)
	}

	var decoded manifestFile
	if err := toml.Unmarshal(buffer.Bytes(), &decoded); err != nil {
		t.Fatalf("toml.Unmarshal returned error: %v", err)
	}
	if decoded.Database != "string" || decoded.Asset != "network" || decoded.Version != "12.0" {
		t.Fatalf("decoded = %#v", decoded)
	}
	if len(decoded.Species) != 1 || decoded.Species[0].ID != "7070" {
		t.Fatalf("decoded.Species = %#v", decoded.Species)
	}
	if len(decoded.Files) != 2 {
		t.Fatalf("len(decoded.Files) = %d, want 2", len(decoded.Files))
	}
}

func TestBuildCompleteFileRecordsMergesExistingManifestAndDropsMissing(t *testing.T) {
	dirVersion := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dirVersion, "raw", "7070"), 0o755); err != nil {
		t.Fatalf("os.MkdirAll returned error: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(dirVersion, "raw", "9606"), 0o755); err != nil {
		t.Fatalf("os.MkdirAll returned error: %v", err)
	}

	fileExisting := filepath.Join(dirVersion, "raw", "7070", "7070.protein.aliases.v12.0.txt.gz")
	fileCurrent := filepath.Join(dirVersion, "raw", "9606", "9606.protein.links.v12.0.txt.gz")
	if err := os.WriteFile(fileExisting, []byte("a"), 0o644); err != nil {
		t.Fatalf("os.WriteFile returned error: %v", err)
	}
	if err := os.WriteFile(fileCurrent, []byte("b"), 0o644); err != nil {
		t.Fatalf("os.WriteFile returned error: %v", err)
	}

	manifest := manifestFile{
		Database:     "string",
		Version:      "12.0",
		VersionToken: "v12.0",
		Files: []manifestFileItem{
			{
				SpeciesID: "7070",
				Asset:     "protein.aliases",
				Path:      "raw/7070/7070.protein.aliases.v12.0.txt.gz",
				SHA256:    "sha-existing",
				Bytes:     1,
				URL:       "https://example.org/existing",
			},
			{
				SpeciesID: "9999",
				Asset:     "protein.info",
				Path:      "raw/9999/9999.protein.info.v12.0.txt.gz",
				SHA256:    "sha-missing",
				Bytes:     1,
				URL:       "https://example.org/missing",
			},
		},
	}

	data, err := toml.Marshal(manifest)
	if err != nil {
		t.Fatalf("toml.Marshal returned error: %v", err)
	}
	fileManifest := filepath.Join(dirVersion, "manifest.lock")
	if err := os.WriteFile(fileManifest, data, 0o644); err != nil {
		t.Fatalf("os.WriteFile returned error: %v", err)
	}

	recordsCurrent := []fileRecord{
		{
			speciesID: "9606",
			assetName: "protein.links",
			pathRel:   "raw/9606/9606.protein.links.v12.0.txt.gz",
			sha256:    "sha-current",
			bytes:     1,
			url:       "https://example.org/current",
		},
	}

	records, err := buildCompleteFileRecords(fileManifest, dirVersion, recordsCurrent)
	if err != nil {
		t.Fatalf("buildCompleteFileRecords returned error: %v", err)
	}
	if len(records) != 2 {
		t.Fatalf("len(records) = %d, want 2", len(records))
	}
}

func TestPlanFetchDownloadTasksPlansReuseAndDownloads(t *testing.T) {
	dirRaw := t.TempDir()
	dirSpecies := filepath.Join(dirRaw, "7070")
	if err := os.MkdirAll(dirSpecies, 0o755); err != nil {
		t.Fatalf("os.MkdirAll returned error: %v", err)
	}

	fileExisting := filepath.Join(dirSpecies, "7070.protein.aliases.v12.0.txt.gz")
	if err := writeGzipFile(fileExisting, []byte("aliases")); err != nil {
		t.Fatalf("writeGzipFile returned error: %v", err)
	}

	cfg := createDefaultConfig()
	cfg.versionToken = "v12.0"
	cfg.taxIDs = []string{"7070"}

	recordsReused, tasksDownload, err := planFetchDownloadTasks(cfg, []string{"7070"}, dirRaw)
	if err != nil {
		t.Fatalf("planFetchDownloadTasks returned error: %v", err)
	}
	if len(recordsReused) != 1 {
		t.Fatalf("len(recordsReused) = %d, want 1", len(recordsReused))
	}
	if recordsReused[0].assetName != "protein.aliases" {
		t.Fatalf("recordsReused = %#v", recordsReused)
	}
	if len(tasksDownload) != 2 {
		t.Fatalf("len(tasksDownload) = %d, want 2", len(tasksDownload))
	}
	if tasksDownload[0].assetName != "protein.links" || tasksDownload[1].assetName != "protein.info" {
		t.Fatalf("tasksDownload = %#v", tasksDownload)
	}
}

func TestRunDownloadTasksPreservesOrderWithWorkers(t *testing.T) {
	serverHTTP := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/slow":
			time.Sleep(20 * time.Millisecond)
			_, _ = writer.Write(makeGzipBytes([]byte("slow")))
		case "/fast":
			_, _ = writer.Write(makeGzipBytes([]byte("fast")))
		default:
			http.NotFound(writer, request)
		}
	}))
	defer serverHTTP.Close()

	dirRaw := t.TempDir()
	tasksDownload := []downloadTask{
		{
			filePath:   filepath.Join(dirRaw, "7070", "7070.protein.aliases.v12.0.txt.gz"),
			speciesID:  "7070",
			assetName:  "protein.aliases",
			pathRel:    "raw/7070/7070.protein.aliases.v12.0.txt.gz",
			urlFile:    serverHTTP.URL + "/slow",
			textAction: "downloading slow",
		},
		{
			filePath:   filepath.Join(dirRaw, "7070", "7070.protein.info.v12.0.txt.gz"),
			speciesID:  "7070",
			assetName:  "protein.info",
			pathRel:    "raw/7070/7070.protein.info.v12.0.txt.gz",
			urlFile:    serverHTTP.URL + "/fast",
			textAction: "downloading fast",
		},
	}

	recordsDownloaded, err := runDownloadTasks(serverHTTP.Client(), tasksDownload, 1, 0, 2, nil)
	if err != nil {
		t.Fatalf("runDownloadTasks returned error: %v", err)
	}
	if len(recordsDownloaded) != 2 {
		t.Fatalf("len(recordsDownloaded) = %d, want 2", len(recordsDownloaded))
	}
	if recordsDownloaded[0].assetName != "protein.aliases" || recordsDownloaded[1].assetName != "protein.info" {
		t.Fatalf("recordsDownloaded = %#v", recordsDownloaded)
	}
}

func TestDownloadFileWithRetryResumesPartialFile(t *testing.T) {
	var gotRange atomic.Value
	serverHTTP := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		gotRange.Store(request.Header.Get("Range"))
		writer.WriteHeader(http.StatusPartialContent)
		_, _ = writer.Write([]byte("bar"))
	}))
	defer serverHTTP.Close()

	dirTemp := t.TempDir()
	fileOut := filepath.Join(dirTemp, "asset.txt")
	filePart := fileOut + ".part"
	if err := os.WriteFile(filePart, []byte("foo"), 0o644); err != nil {
		t.Fatalf("os.WriteFile returned error: %v", err)
	}

	err := downloadFileWithRetry(
		serverHTTP.Client(),
		serverHTTP.URL+"/asset.txt",
		fileOut,
		1,
		0,
		nil,
	)
	if err != nil {
		t.Fatalf("downloadFileWithRetry returned error: %v", err)
	}

	data, err := os.ReadFile(fileOut)
	if err != nil {
		t.Fatalf("os.ReadFile returned error: %v", err)
	}
	if string(data) != "foobar" {
		t.Fatalf("file content = %q, want %q", string(data), "foobar")
	}
	if gotRange.Load() != "bytes=3-" {
		t.Fatalf("Range = %#v, want bytes=3-", gotRange.Load())
	}
	if _, err := os.Stat(filePart); !os.IsNotExist(err) {
		t.Fatalf("partial file still exists: %v", err)
	}
}

func TestRunDownloadTasksCancelsQueuedWorkAfterError(t *testing.T) {
	var countThird atomic.Int32

	serverHTTP := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/one":
			_, _ = writer.Write(makeGzipBytes([]byte("one")))
		case "/two":
			http.Error(writer, "bad gateway", http.StatusBadGateway)
		case "/three":
			countThird.Add(1)
			_, _ = writer.Write(makeGzipBytes([]byte("three")))
		default:
			http.NotFound(writer, request)
		}
	}))
	defer serverHTTP.Close()

	dirRaw := t.TempDir()
	tasksDownload := []downloadTask{
		{
			filePath:   filepath.Join(dirRaw, "7070", "7070.protein.aliases.v12.0.txt.gz"),
			speciesID:  "7070",
			assetName:  "protein.aliases",
			pathRel:    "raw/7070/7070.protein.aliases.v12.0.txt.gz",
			urlFile:    serverHTTP.URL + "/one",
			textAction: "downloading one",
		},
		{
			filePath:   filepath.Join(dirRaw, "7070", "7070.protein.links.v12.0.txt.gz"),
			speciesID:  "7070",
			assetName:  "protein.links",
			pathRel:    "raw/7070/7070.protein.links.v12.0.txt.gz",
			urlFile:    serverHTTP.URL + "/two",
			textAction: "downloading two",
		},
		{
			filePath:   filepath.Join(dirRaw, "7070", "7070.protein.info.v12.0.txt.gz"),
			speciesID:  "7070",
			assetName:  "protein.info",
			pathRel:    "raw/7070/7070.protein.info.v12.0.txt.gz",
			urlFile:    serverHTTP.URL + "/three",
			textAction: "downloading three",
		},
	}

	_, err := runDownloadTasks(serverHTTP.Client(), tasksDownload, 1, 0, 1, nil)
	if err == nil {
		t.Fatal("runDownloadTasks returned nil error")
	}
	if countThird.Load() != 0 {
		t.Fatalf("countThird = %d, want 0", countThird.Load())
	}
}

func TestValidateConfigRejectsInvalidWorkersMax(t *testing.T) {
	cfg := createDefaultConfig()
	cfg.dirOut = "/tmp/string"
	cfg.taxIDs = []string{"7070"}
	cfg.WorkersMax = 0

	err := validateConfig(cfg)
	if err == nil || err.Error() != "workers_max must be >= 1" {
		t.Fatalf("validateConfig error = %v", err)
	}
}

func TestValidateConfigRejectsNegativeRequestInterval(t *testing.T) {
	cfg := createDefaultConfig()
	cfg.dirOut = "/tmp/string"
	cfg.taxIDs = []string{"7070"}
	cfg.RequestInterval = -1 * time.Millisecond

	err := validateConfig(cfg)
	if err == nil || err.Error() != "request_interval_ms must be >= 0" {
		t.Fatalf("validateConfig error = %v", err)
	}
}

func TestValidateConfigResolvesAtFileTaxIDs(t *testing.T) {
	fileTaxIDs := filepath.Join(t.TempDir(), "taxids.txt")
	if err := os.WriteFile(fileTaxIDs, []byte("9606\n7070\n"), 0o644); err != nil {
		t.Fatalf("os.WriteFile returned error: %v", err)
	}

	cfg := createDefaultConfig()
	cfg.dirOut = "/tmp/string"
	cfg.taxIDs = []string{"@" + fileTaxIDs}

	err := validateConfig(cfg)
	if err != nil {
		t.Fatalf("validateConfig error = %v", err)
	}
	expected := []string{"7070", "9606"}
	if !reflect.DeepEqual(cfg.taxIDs, expected) {
		t.Fatalf("cfg.taxIDs = %#v, want %#v", cfg.taxIDs, expected)
	}
}

func writeGzipFile(filePath string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(filePath), 0o755); err != nil {
		return err
	}
	return os.WriteFile(filePath, makeGzipBytes(data), 0o644)
}

func makeGzipBytes(data []byte) []byte {
	var buffer bytes.Buffer
	writerGzip := gzip.NewWriter(&buffer)
	_, _ = writerGzip.Write(data)
	_ = writerGzip.Close()
	return buffer.Bytes()
}

func assertContains(t *testing.T, text string, expected string) {
	t.Helper()
	if !strings.Contains(text, expected) {
		t.Fatalf("expected %q in text:\n%s", expected, text)
	}
}
