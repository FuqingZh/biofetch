package geneontology

import (
	"biofetch/internal/shared/cliopt"
	"bytes"
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

func TestParseOntologyAssetNames(t *testing.T) {
	assets, err := parseOntologyAssetNames([]string{"go-basic.obo", "go.obo,go-basic.obo"})
	if err != nil {
		t.Fatalf("parseOntologyAssetNames returned error: %v", err)
	}

	expected := []string{"go-basic.obo", "go.obo"}
	if !reflect.DeepEqual(assets, expected) {
		t.Fatalf("parseOntologyAssetNames = %#v, want %#v", assets, expected)
	}
}

func TestParseOntologyAssetNamesSupportsAtFile(t *testing.T) {
	fileAssets := filepath.Join(t.TempDir(), "assets.txt")
	if err := os.WriteFile(fileAssets, []byte("# comment\ngo-basic.obo\n\ngo.obo\n"), 0o644); err != nil {
		t.Fatalf("os.WriteFile returned error: %v", err)
	}

	assets, err := parseOntologyAssetNames([]string{"@" + fileAssets})
	if err != nil {
		t.Fatalf("parseOntologyAssetNames returned error: %v", err)
	}

	expected := []string{"go-basic.obo", "go.obo"}
	if !reflect.DeepEqual(assets, expected) {
		t.Fatalf("parseOntologyAssetNames = %#v, want %#v", assets, expected)
	}
}

func TestParseOntologyAssetsFromIndex(t *testing.T) {
	data := []byte(`
<html>
  <body>
    <a href="../">Parent</a>
    <a href="extensions/">extensions</a>
    <a href="go-base.owl">go-base.owl</a>
    <a href="go-basic.json">go-basic.json</a>
    <a href="go-basic.obo">go-basic.obo</a>
    <a href="go.obo">go.obo</a>
  </body>
</html>
`)

	assets, err := parseOntologyAssetsFromIndex(data, ontologyCurrentBaseURL)
	if err != nil {
		t.Fatalf("parseOntologyAssetsFromIndex returned error: %v", err)
	}

	names := make([]string, 0, len(assets))
	for _, asset := range assets {
		names = append(names, asset.name)
	}

	expected := []string{"go-base.owl", "go-basic.json", "go-basic.obo", "go.obo"}
	if !reflect.DeepEqual(names, expected) {
		t.Fatalf("parseOntologyAssetsFromIndex = %#v, want %#v", names, expected)
	}
}

func TestParseOntologyAssetsFromIndexUsesLinkTextFallback(t *testing.T) {
	data := []byte(`
<html>
  <body>
    <a href='go-base.owl'>go-base.owl</a>
    <a>go-basic.json.gz</a>
    <a href='subsets/'>subsets</a>
  </body>
</html>
`)

	assets, err := parseOntologyAssetsFromIndex(data, ontologyCurrentBaseURL)
	if err != nil {
		t.Fatalf("parseOntologyAssetsFromIndex returned error: %v", err)
	}

	names := make([]string, 0, len(assets))
	for _, asset := range assets {
		names = append(names, asset.name)
	}

	expected := []string{"go-base.owl", "go-basic.json.gz"}
	if !reflect.DeepEqual(names, expected) {
		t.Fatalf("parseOntologyAssetsFromIndex = %#v, want %#v", names, expected)
	}
}

func TestResolveOntologyAssetsRejectsUnknownAsset(t *testing.T) {
	assetsAvailable := []ontologyAsset{
		{name: "go-basic.obo", url: ontologyCurrentBaseURL + "go-basic.obo"},
	}

	_, err := resolveOntologyAssets(assetsAvailable, []string{"go-basic.obo", "unknown.obo"})
	if err == nil {
		t.Fatal("resolveOntologyAssets returned nil error for unknown asset")
	}
}

func TestResolveOntologyAssetsReturnsAllWhenAssetsOmitted(t *testing.T) {
	assetsAvailable := []ontologyAsset{
		{name: "go-basic.obo", url: ontologyCurrentBaseURL + "go-basic.obo"},
		{name: "go.obo", url: ontologyCurrentBaseURL + "go.obo"},
	}

	assets, err := resolveOntologyAssets(assetsAvailable, nil)
	if err != nil {
		t.Fatalf("resolveOntologyAssets returned error: %v", err)
	}
	if !reflect.DeepEqual(assets, assetsAvailable) {
		t.Fatalf("resolveOntologyAssets = %#v, want %#v", assets, assetsAvailable)
	}
}

func TestBuildOntologyManifestFile(t *testing.T) {
	cfg := ontologyConfig{
		version:       "2026-03-11",
		VersionConfig: cliopt.VersionConfig{VersionToken: "2026-03-11"},
	}
	records := []ontologyRecord{
		{
			Asset:   "go-basic.obo",
			PathRel: "raw/go-basic.obo",
			SHA256:  "sha-basic",
			Bytes:   11,
			URL:     ontologyCurrentBaseURL + "go-basic.obo",
		},
		{
			Asset:   "go-basic.json",
			PathRel: "raw/go-basic.json",
			SHA256:  "sha-json",
			Bytes:   22,
			URL:     ontologyCurrentBaseURL + "go-basic.json",
		},
	}

	manifest := buildOntologyManifestFile(
		&cfg,
		records,
		time.Date(2026, time.March, 11, 12, 0, 0, 0, time.FixedZone("UTC+8", 8*3600)),
	)
	if manifest.Database != "go" || manifest.Asset != "ontology" {
		t.Fatalf("manifest = %#v", manifest)
	}
	if len(manifest.Files) != 2 {
		t.Fatalf("len(manifest.Files) = %d, want 2", len(manifest.Files))
	}

	var buffer bytes.Buffer
	if err := toml.NewEncoder(&buffer).Encode(manifest); err != nil {
		t.Fatalf("toml encode returned error: %v", err)
	}
	if buffer.Len() == 0 {
		t.Fatal("encoded manifest is empty")
	}
}

func TestParseOntologyVersionFromOBO(t *testing.T) {
	data := []byte("format-version: 1.2\ndata-version: releases/2026-03-11\nsubsetdef: goslim_generic \"Generic GO slim\"\n")
	value, err := parseOntologyVersionFromOBO(data)
	if err != nil {
		t.Fatalf("parseOntologyVersionFromOBO returned error: %v", err)
	}
	if value != "2026-03-11" {
		t.Fatalf("parseOntologyVersionFromOBO = %q, want %q", value, "2026-03-11")
	}
}

func TestValidateOptionalOntologyVersionToken(t *testing.T) {
	if err := validateOptionalOntologyVersionToken(""); err != nil {
		t.Fatalf("validateOptionalOntologyVersionToken returned error for empty version: %v", err)
	}
	if err := validateOptionalOntologyVersionToken("2026-01-23"); err != nil {
		t.Fatalf("validateOptionalOntologyVersionToken returned error for valid version: %v", err)
	}
}

func TestValidateOptionalOntologyVersionTokenRejectsInvalidValue(t *testing.T) {
	err := validateOptionalOntologyVersionToken("2026-1-23")
	if err == nil {
		t.Fatal("validateOptionalOntologyVersionToken returned nil error for invalid version")
	}

	assertContains(t, err.Error(), "YYYY-MM-DD")
	assertContains(t, err.Error(), ontologyArchiveRootURL)
}

func TestBuildOntologyBaseURLForVersionToken(t *testing.T) {
	if got := buildOntologyBaseURLForVersionToken("2026-01-23"); got != "https://release.geneontology.org/2026-01-23/ontology/" {
		t.Fatalf("buildOntologyBaseURLForVersionToken(valid) = %q", got)
	}
	if got := buildOntologyBaseURLForVersionToken("not-a-date"); got != ontologyCurrentBaseURL {
		t.Fatalf("buildOntologyBaseURLForVersionToken(invalid) = %q, want %q", got, ontologyCurrentBaseURL)
	}
}

func TestResolveExistingOntologyFetchRecordUsesManifestMatch(t *testing.T) {
	dirRaw := t.TempDir()
	filePath := filepath.Join(dirRaw, "go-basic.obo")
	if err := os.WriteFile(filePath, []byte("altered!"), 0o644); err != nil {
		t.Fatalf("os.WriteFile returned error: %v", err)
	}

	filesCurrentByPath, err := scanOntologyRawFileStateIndex(dirRaw)
	if err != nil {
		t.Fatalf("scanOntologyRawFileStateIndex returned error: %v", err)
	}

	recordExpected := ontologyRecord{
		Asset:   "go-basic.obo",
		PathRel: "raw/go-basic.obo",
		SHA256:  "sha-from-manifest",
		Bytes:   int64(len("altered!")),
		URL:     ontologyCurrentBaseURL + "go-basic.obo",
	}
	recordCurrent, ok, err := resolveExistingOntologyFetchRecord(
		filePath,
		recordExpected.PathRel,
		ontologyAsset{name: "go-basic.obo", url: recordExpected.URL},
		map[string]ontologyRecord{recordExpected.PathRel: recordExpected},
		filesCurrentByPath,
	)
	if err != nil {
		t.Fatalf("resolveExistingOntologyFetchRecord returned error: %v", err)
	}
	if !ok {
		t.Fatal("resolveExistingOntologyFetchRecord returned ok=false")
	}
	if !reflect.DeepEqual(recordCurrent, recordExpected) {
		t.Fatalf("recordCurrent = %#v, want %#v", recordCurrent, recordExpected)
	}
}

func TestResolveExistingOntologyFetchRecordBuildsLocalRecordWithoutManifest(t *testing.T) {
	dirRaw := t.TempDir()
	filePath := filepath.Join(dirRaw, "go-basic.obo")
	if err := os.WriteFile(filePath, []byte("local-content"), 0o644); err != nil {
		t.Fatalf("os.WriteFile returned error: %v", err)
	}

	filesCurrentByPath, err := scanOntologyRawFileStateIndex(dirRaw)
	if err != nil {
		t.Fatalf("scanOntologyRawFileStateIndex returned error: %v", err)
	}

	recordCurrent, ok, err := resolveExistingOntologyFetchRecord(
		filePath,
		"raw/go-basic.obo",
		ontologyAsset{name: "go-basic.obo", url: ontologyCurrentBaseURL + "go-basic.obo"},
		nil,
		filesCurrentByPath,
	)
	if err != nil {
		t.Fatalf("resolveExistingOntologyFetchRecord returned error: %v", err)
	}
	if !ok {
		t.Fatal("resolveExistingOntologyFetchRecord returned ok=false")
	}
	if recordCurrent.SHA256 == "" {
		t.Fatal("recordCurrent.SHA256 is empty")
	}
	if recordCurrent.Bytes != int64(len("local-content")) {
		t.Fatalf("recordCurrent.Bytes = %d, want %d", recordCurrent.Bytes, len("local-content"))
	}
}

func TestResolveExistingOntologyFetchRecordRejectsManifestSizeMismatch(t *testing.T) {
	dirRaw := t.TempDir()
	filePath := filepath.Join(dirRaw, "go-basic.obo")
	if err := os.WriteFile(filePath, []byte("size-now"), 0o644); err != nil {
		t.Fatalf("os.WriteFile returned error: %v", err)
	}

	filesCurrentByPath, err := scanOntologyRawFileStateIndex(dirRaw)
	if err != nil {
		t.Fatalf("scanOntologyRawFileStateIndex returned error: %v", err)
	}

	_, ok, err := resolveExistingOntologyFetchRecord(
		filePath,
		"raw/go-basic.obo",
		ontologyAsset{name: "go-basic.obo", url: ontologyCurrentBaseURL + "go-basic.obo"},
		map[string]ontologyRecord{
			"raw/go-basic.obo": {
				Asset:   "go-basic.obo",
				PathRel: "raw/go-basic.obo",
				SHA256:  "sha-from-manifest",
				Bytes:   999,
				URL:     ontologyCurrentBaseURL + "go-basic.obo",
			},
		},
		filesCurrentByPath,
	)
	if err != nil {
		t.Fatalf("resolveExistingOntologyFetchRecord returned error: %v", err)
	}
	if ok {
		t.Fatal("resolveExistingOntologyFetchRecord returned ok=true for manifest size mismatch")
	}
}

func TestShouldReuseOntologySyncRecord(t *testing.T) {
	record := ontologyRecord{
		Asset:   "go-basic.obo",
		PathRel: "raw/go-basic.obo",
		SHA256:  "sha-from-manifest",
		Bytes:   12,
		URL:     ontologyCurrentBaseURL + "go-basic.obo",
	}
	if !shouldReuseOntologySyncRecord(record, map[string]ontologyFileState{
		record.PathRel: {Bytes: 12},
	}) {
		t.Fatal("shouldReuseOntologySyncRecord returned false for matching size")
	}
	if shouldReuseOntologySyncRecord(record, map[string]ontologyFileState{
		record.PathRel: {Bytes: 11},
	}) {
		t.Fatal("shouldReuseOntologySyncRecord returned true for mismatched size")
	}
	if shouldReuseOntologySyncRecord(record, nil) {
		t.Fatal("shouldReuseOntologySyncRecord returned true for missing file state")
	}
}

func TestPlanFetchOntologyTasksPlansReuseAndDownloads(t *testing.T) {
	dirRaw := t.TempDir()
	if err := os.WriteFile(filepath.Join(dirRaw, "go-basic.obo"), []byte("cached"), 0o644); err != nil {
		t.Fatalf("os.WriteFile returned error: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dirRaw, "go.obo"), []byte("local"), 0o644); err != nil {
		t.Fatalf("os.WriteFile returned error: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dirRaw, "go-plus.obo"), []byte("stale"), 0o644); err != nil {
		t.Fatalf("os.WriteFile returned error: %v", err)
	}

	filesCurrentByPath, err := scanOntologyRawFileStateIndex(dirRaw)
	if err != nil {
		t.Fatalf("scanOntologyRawFileStateIndex returned error: %v", err)
	}

	assets := []ontologyAsset{
		{name: "go-basic.obo", url: ontologyCurrentBaseURL + "go-basic.obo"},
		{name: "go.obo", url: ontologyCurrentBaseURL + "go.obo"},
		{name: "go-plus.obo", url: ontologyCurrentBaseURL + "go-plus.obo"},
	}
	recordsReused, tasksDownload, err := planFetchOntologyTasks(
		assets,
		dirRaw,
		false,
		map[string]ontologyRecord{
			"raw/go-basic.obo": {
				Asset:   "go-basic.obo",
				PathRel: "raw/go-basic.obo",
				SHA256:  "sha-manifest",
				Bytes:   int64(len("cached")),
				URL:     ontologyCurrentBaseURL + "go-basic.obo",
			},
			"raw/go-plus.obo": {
				Asset:   "go-plus.obo",
				PathRel: "raw/go-plus.obo",
				SHA256:  "sha-stale",
				Bytes:   999,
				URL:     ontologyCurrentBaseURL + "go-plus.obo",
			},
		},
		filesCurrentByPath,
	)
	if err != nil {
		t.Fatalf("planFetchOntologyTasks returned error: %v", err)
	}
	if len(recordsReused) != 2 {
		t.Fatalf("len(recordsReused) = %d, want 2", len(recordsReused))
	}
	if recordsReused[0].Asset != "go-basic.obo" || recordsReused[1].Asset != "go.obo" {
		t.Fatalf("recordsReused = %#v", recordsReused)
	}
	if recordsReused[1].SHA256 == "" {
		t.Fatal("recordsReused[1].SHA256 is empty")
	}
	if len(tasksDownload) != 1 || tasksDownload[0].asset.name != "go-plus.obo" {
		t.Fatalf("tasksDownload = %#v", tasksDownload)
	}
}

func TestRunOntologyDownloadTasksPreservesOrderWithWorkers(t *testing.T) {
	serverHTTP := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/slow.obo":
			time.Sleep(20 * time.Millisecond)
			_, _ = writer.Write([]byte("slow"))
		case "/fast.obo":
			_, _ = writer.Write([]byte("fast"))
		default:
			http.NotFound(writer, request)
		}
	}))
	defer serverHTTP.Close()

	dirRaw := t.TempDir()
	tasksDownload := []ontologyDownloadTask{
		{
			asset:      ontologyAsset{name: "slow.obo", url: serverHTTP.URL + "/slow.obo"},
			fileOut:    filepath.Join(dirRaw, "slow.obo"),
			pathRel:    "raw/slow.obo",
			textAction: "downloading slow.obo",
		},
		{
			asset:      ontologyAsset{name: "fast.obo", url: serverHTTP.URL + "/fast.obo"},
			fileOut:    filepath.Join(dirRaw, "fast.obo"),
			pathRel:    "raw/fast.obo",
			textAction: "downloading fast.obo",
		},
	}

	recordsDownloaded, err := runOntologyDownloadTasks(
		serverHTTP.Client(),
		tasksDownload,
		1,
		0,
		2,
		nil,
	)
	if err != nil {
		t.Fatalf("runOntologyDownloadTasks returned error: %v", err)
	}
	if len(recordsDownloaded) != 2 {
		t.Fatalf("len(recordsDownloaded) = %d, want 2", len(recordsDownloaded))
	}
	if recordsDownloaded[0].Asset != "slow.obo" || recordsDownloaded[1].Asset != "fast.obo" {
		t.Fatalf("recordsDownloaded = %#v", recordsDownloaded)
	}
}

func TestRunOntologyDownloadTasksCancelsQueuedWorkAfterError(t *testing.T) {
	var countThird atomic.Int32

	serverHTTP := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/one.obo":
			_, _ = writer.Write([]byte("one"))
		case "/two.obo":
			http.Error(writer, "bad gateway", http.StatusBadGateway)
		case "/three.obo":
			countThird.Add(1)
			_, _ = writer.Write([]byte("three"))
		default:
			http.NotFound(writer, request)
		}
	}))
	defer serverHTTP.Close()

	dirRaw := t.TempDir()
	tasksDownload := []ontologyDownloadTask{
		{
			asset:      ontologyAsset{name: "one.obo", url: serverHTTP.URL + "/one.obo"},
			fileOut:    filepath.Join(dirRaw, "one.obo"),
			pathRel:    "raw/one.obo",
			textAction: "downloading one.obo",
		},
		{
			asset:      ontologyAsset{name: "two.obo", url: serverHTTP.URL + "/two.obo"},
			fileOut:    filepath.Join(dirRaw, "two.obo"),
			pathRel:    "raw/two.obo",
			textAction: "downloading two.obo",
		},
		{
			asset:      ontologyAsset{name: "three.obo", url: serverHTTP.URL + "/three.obo"},
			fileOut:    filepath.Join(dirRaw, "three.obo"),
			pathRel:    "raw/three.obo",
			textAction: "downloading three.obo",
		},
	}

	_, err := runOntologyDownloadTasks(
		serverHTTP.Client(),
		tasksDownload,
		1,
		0,
		1,
		nil,
	)
	if err == nil {
		t.Fatal("runOntologyDownloadTasks returned nil error")
	}
	if countThird.Load() != 0 {
		t.Fatalf("countThird = %d, want 0", countThird.Load())
	}
}

func TestConfirmAllOntologyDownload(t *testing.T) {
	var buffer bytes.Buffer
	err := confirmAllOntologyDownload(strings.NewReader("all_assets\n"), &buffer)
	if err != nil {
		t.Fatalf("confirmAllOntologyDownload returned error: %v", err)
	}

	assertContains(t, buffer.String(), "Full ontology download may fetch a large number of files")
	assertContains(t, buffer.String(), `Type "all_assets" to continue.`)
	assertContains(t, buffer.String(), "> ")
}

func TestConfirmAllOntologyDownloadRejectsWrongInput(t *testing.T) {
	err := confirmAllOntologyDownload(strings.NewReader("yes\n"), &bytes.Buffer{})
	if err == nil {
		t.Fatal("confirmAllOntologyDownload returned nil error for wrong confirmation text")
	}

	assertContains(t, err.Error(), `expected "all_assets"`)
}

func TestValidateOntologyConfigRejectsInvalidVersionToken(t *testing.T) {
	cfg := ontologyConfig{
		DirOutConfig:       cliopt.DirOutConfig{DirOut: "/tmp/go"},
		ExistingRuleConfig: cliopt.ExistingRuleConfig{RuleExisting: "skip"},
		RetryConfig:        cliopt.RetryConfig{RetryMax: 1},
		DownloadControlConfig: cliopt.DownloadControlConfig{
			WorkersMax: 1,
		},
		VersionConfig: cliopt.VersionConfig{VersionToken: "2026-1-23"},
		assetNames:    []string{"go-basic.obo"},
	}

	err := validateOntologyConfig(&cfg)
	if err == nil {
		t.Fatal("validateOntologyConfig returned nil error for invalid version token")
	}
	assertContains(t, err.Error(), "YYYY-MM-DD")
}

func TestValidateOntologyConfigRejectsInvalidWorkersMax(t *testing.T) {
	cfg := createDefaultOntologyConfig()
	cfg.DirOut = "/tmp/go"
	cfg.RuleExisting = "skip"
	cfg.assetNames = []string{"go-basic.obo"}
	cfg.WorkersMax = 0

	err := validateOntologyConfig(&cfg)
	if err == nil || err.Error() != "workers_max must be >= 1" {
		t.Fatalf("validateOntologyConfig error = %v", err)
	}
}

func TestValidateOntologyConfigRejectsNegativeRequestInterval(t *testing.T) {
	cfg := createDefaultOntologyConfig()
	cfg.DirOut = "/tmp/go"
	cfg.RuleExisting = "skip"
	cfg.assetNames = []string{"go-basic.obo"}
	cfg.RequestInterval = -1 * time.Millisecond

	err := validateOntologyConfig(&cfg)
	if err == nil || err.Error() != "request_interval_ms must be >= 0" {
		t.Fatalf("validateOntologyConfig error = %v", err)
	}
}

func TestValidateOntologyConfigAllowsAssetsOmitted(t *testing.T) {
	cfg := createDefaultOntologyConfig()
	cfg.DirOut = "/tmp/go"
	cfg.RuleExisting = "skip"

	err := validateOntologyConfig(&cfg)
	if err != nil {
		t.Fatalf("validateOntologyConfig returned error: %v", err)
	}
}

func TestShouldConfirmAllOntologyDownload(t *testing.T) {
	assetsAvailable := []ontologyAsset{
		{name: "go-basic.obo", url: ontologyCurrentBaseURL + "go-basic.obo"},
		{name: "go.obo", url: ontologyCurrentBaseURL + "go.obo"},
	}
	if !shouldConfirmAllOntologyDownload(assetsAvailable, assetsAvailable) {
		t.Fatal("shouldConfirmAllOntologyDownload returned false for full asset set")
	}
	if shouldConfirmAllOntologyDownload(assetsAvailable, assetsAvailable[:1]) {
		t.Fatal("shouldConfirmAllOntologyDownload returned true for partial asset set")
	}
}

func assertContains(t *testing.T, text string, expected string) {
	t.Helper()
	if !strings.Contains(text, expected) {
		t.Fatalf("expected %q in text:\n%s", expected, text)
	}
}
