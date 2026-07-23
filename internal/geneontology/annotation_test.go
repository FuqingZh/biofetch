package geneontology

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"biofetch/internal/shared/httpx"
	"biofetch/internal/shared/staticasset"
)

func TestResolveAnnotationDatasetsAllowsOmittedValues(t *testing.T) {
	datasets, err := resolveAnnotationDatasets(nil)
	if err != nil {
		t.Fatalf("resolveAnnotationDatasets returned error: %v", err)
	}
	if datasets != nil {
		t.Fatalf("datasets = %#v, want nil", datasets)
	}
}

func TestResolveAnnotationDatasetsSupportsAtFileAndComma(t *testing.T) {
	fileDatasets := filepath.Join(t.TempDir(), "datasets.txt")
	if err := os.WriteFile(fileDatasets, []byte("# comment\nmgi\n"), 0o644); err != nil {
		t.Fatalf("os.WriteFile returned error: %v", err)
	}
	datasets, err := resolveAnnotationDatasets([]string{"goa_human,sgd", "mgi"})
	if err != nil {
		t.Fatalf("resolveAnnotationDatasets returned error: %v", err)
	}
	expected := []string{"goa_human", "mgi", "sgd"}
	if !reflect.DeepEqual(datasets, expected) {
		t.Fatalf("datasets = %#v, want %#v", datasets, expected)
	}
}

func TestResolveAnnotationDatasetsRejectsFormattedFilename(t *testing.T) {
	_, err := resolveAnnotationDatasets([]string{"goa_human.gaf.gz"})
	if err == nil {
		t.Fatal("resolveAnnotationDatasets returned nil error")
	}
}

func TestResolveAnnotationFormatsDefaultsToGAF(t *testing.T) {
	formats, err := resolveAnnotationFormats(nil)
	if err != nil {
		t.Fatalf("resolveAnnotationFormats returned error: %v", err)
	}
	expected := []string{"gaf"}
	if !reflect.DeepEqual(formats, expected) {
		t.Fatalf("formats = %#v, want %#v", formats, expected)
	}
}

func TestResolveAnnotationFormatsSupportsAtFileAndComma(t *testing.T) {
	fileFormats := filepath.Join(t.TempDir(), "formats.txt")
	if err := os.WriteFile(fileFormats, []byte("# comment\ngpi\n"), 0o644); err != nil {
		t.Fatalf("os.WriteFile returned error: %v", err)
	}
	formats, err := resolveAnnotationFormats([]string{"gaf,gpad", "gpi"})
	if err != nil {
		t.Fatalf("resolveAnnotationFormats returned error: %v", err)
	}
	expected := []string{"gaf", "gpad", "gpi"}
	if !reflect.DeepEqual(formats, expected) {
		t.Fatalf("formats = %#v, want %#v", formats, expected)
	}
}

func TestResolveAnnotationFormatsRejectsUnknown(t *testing.T) {
	_, err := resolveAnnotationFormats([]string{"obo"})
	if err == nil {
		t.Fatal("resolveAnnotationFormats returned nil error")
	}
}

func TestBuildAnnotationAssetsUsesAnnotationBaseURL(t *testing.T) {
	assets := buildAnnotationAssets(annotationCurrentBaseURL, []string{"goa_human"}, []string{"gaf", "gpi"})
	expected := []staticasset.Asset{
		{
			Name: "goa_human.gaf.gz",
			Path: "raw/goa_human.gaf.gz",
			URL:  "https://current.geneontology.org/annotations/goa_human.gaf.gz",
		},
		{
			Name: "goa_human.gpi.gz",
			Path: "raw/goa_human.gpi.gz",
			URL:  "https://current.geneontology.org/annotations/goa_human.gpi.gz",
		},
	}
	if !reflect.DeepEqual(assets, expected) {
		t.Fatalf("assets = %#v, want %#v", assets, expected)
	}
}

func TestBuildAnnotationReleaseBaseURL(t *testing.T) {
	got := buildAnnotationReleaseBaseURL("2026-01-23")
	want := "https://release.geneontology.org/2026-01-23/annotations/"
	if got != want {
		t.Fatalf("buildAnnotationReleaseBaseURL = %q, want %q", got, want)
	}
}

func TestParseAnnotationIndexAssetsFiltersFormats(t *testing.T) {
	assets := parseAnnotationIndexAssets("https://example.test/annotations/", `
		<a href="goa_human.gaf.gz">goa_human.gaf.gz</a>
		<a href="/annotations/mgi.gpi.gz?download=1">mgi.gpi.gz</a>
		<a href="sgd.gpad.gz">sgd.gpad.gz</a>
		<a href="README.txt">README</a>
	`, []string{"gaf", "gpi"})
	expected := []staticasset.Asset{
		{
			Name: "goa_human.gaf.gz",
			Path: "raw/goa_human.gaf.gz",
			URL:  "https://example.test/annotations/goa_human.gaf.gz",
		},
		{
			Name: "mgi.gpi.gz",
			Path: "raw/mgi.gpi.gz",
			URL:  "https://example.test/annotations/mgi.gpi.gz",
		},
	}
	if !reflect.DeepEqual(assets, expected) {
		t.Fatalf("assets = %#v, want %#v", assets, expected)
	}
}

func TestResolveAnnotationSourceCurrent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/ontology/go-basic.obo" {
			t.Fatalf("unexpected path: %s", request.URL.Path)
		}
		_, _ = writer.Write([]byte("format-version: 1.2\ndata-version: releases/2026-05-01\n"))
	}))
	defer server.Close()

	originalCurrent := ontologyCurrentBaseURL
	originalAnnotation := annotationCurrentBaseURL
	t.Cleanup(func() {
		ontologyCurrentBaseURL = originalCurrent
		annotationCurrentBaseURL = originalAnnotation
	})
	ontologyCurrentBaseURL = server.URL + "/ontology/"
	annotationCurrentBaseURL = server.URL + "/annotations/"

	source, err := resolveAnnotationSource(httpx.NewClient(false), "", httpx.NewRequestLimiter(0))
	if err != nil {
		t.Fatalf("resolveAnnotationSource returned error: %v", err)
	}
	if source.versionToken != "2026-05-01" {
		t.Fatalf("versionToken = %q", source.versionToken)
	}
	if source.baseURL != server.URL+"/annotations/" {
		t.Fatalf("baseURL = %q", source.baseURL)
	}
}

func TestRunFetchAnnotationDownloadsManifest(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/ontology/go-basic.obo":
			_, _ = writer.Write([]byte("format-version: 1.2\ndata-version: releases/2026-05-01\n"))
		case "/annotations/goa_human.gaf.gz":
			_, _ = writer.Write([]byte("!gaf-version: 2.2\n"))
		default:
			t.Fatalf("unexpected path: %s", request.URL.Path)
		}
	}))
	defer server.Close()

	originalCurrent := ontologyCurrentBaseURL
	originalAnnotation := annotationCurrentBaseURL
	t.Cleanup(func() {
		ontologyCurrentBaseURL = originalCurrent
		annotationCurrentBaseURL = originalAnnotation
	})
	ontologyCurrentBaseURL = server.URL + "/ontology/"
	annotationCurrentBaseURL = server.URL + "/annotations/"

	cfg := createDefaultAnnotationConfig()
	cfg.DirOut = t.TempDir()
	cfg.RuleExisting = "skip"
	cfg.RetryMax = 1
	cfg.RetryWait = time.Millisecond
	cfg.WorkersMax = 1
	cfg.datasetNames = []string{"goa_human"}
	cfg.formatNames = []string{"gaf"}

	if err := runFetchAnnotation(&cfg); err != nil {
		t.Fatalf("runFetchAnnotation returned error: %v", err)
	}
	fileManifest := filepath.Join(cfg.DirOut, "annotation", "2026-05-01", "manifest.lock")
	manifest, ok, err := staticasset.ReadManifest(fileManifest)
	if err != nil {
		t.Fatalf("ReadManifest returned error: %v", err)
	}
	if !ok {
		t.Fatal("manifest was not written")
	}
	if manifest.Database != "go" || manifest.Asset != "annotation" || manifest.Source != "geneontology" {
		t.Fatalf("manifest identity = %#v", manifest)
	}
	if len(manifest.Files) != 1 || manifest.Files[0].Path != "raw/goa_human.gaf.gz" {
		t.Fatalf("manifest files = %#v", manifest.Files)
	}
	if manifest.Scope.Type != "datasets_formats" || manifest.Scope.Value != "goa_human|gaf" {
		t.Fatalf("manifest scope = %#v", manifest.Scope)
	}
}

func TestRunFetchAnnotationDiscoversDatasetsWhenOmitted(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/ontology/go-basic.obo":
			_, _ = writer.Write([]byte("format-version: 1.2\ndata-version: releases/2026-05-01\n"))
		case "/annotations/":
			_, _ = writer.Write([]byte(`
				<a href="goa_human.gaf.gz">goa_human.gaf.gz</a>
				<a href="mgi.gaf.gz">mgi.gaf.gz</a>
				<a href="goa_human.gpi.gz">goa_human.gpi.gz</a>
			`))
		case "/annotations/goa_human.gaf.gz", "/annotations/mgi.gaf.gz":
			_, _ = writer.Write([]byte("!gaf-version: 2.2\n"))
		default:
			t.Fatalf("unexpected path: %s", request.URL.Path)
		}
	}))
	defer server.Close()

	originalCurrent := ontologyCurrentBaseURL
	originalAnnotation := annotationCurrentBaseURL
	t.Cleanup(func() {
		ontologyCurrentBaseURL = originalCurrent
		annotationCurrentBaseURL = originalAnnotation
	})
	ontologyCurrentBaseURL = server.URL + "/ontology/"
	annotationCurrentBaseURL = server.URL + "/annotations/"

	cfg := createDefaultAnnotationConfig()
	cfg.DirOut = t.TempDir()
	cfg.RuleExisting = "skip"
	cfg.RetryMax = 1
	cfg.RetryWait = time.Millisecond
	cfg.WorkersMax = 1

	if err := runFetchAnnotation(&cfg); err != nil {
		t.Fatalf("runFetchAnnotation returned error: %v", err)
	}
	manifest, ok, err := staticasset.ReadManifest(filepath.Join(cfg.DirOut, "annotation", "2026-05-01", "manifest.lock"))
	if err != nil {
		t.Fatalf("ReadManifest returned error: %v", err)
	}
	if !ok {
		t.Fatal("manifest was not written")
	}
	paths := make([]string, 0, len(manifest.Files))
	for _, file := range manifest.Files {
		paths = append(paths, file.Path)
	}
	expectedPaths := []string{"raw/goa_human.gaf.gz", "raw/mgi.gaf.gz"}
	if !reflect.DeepEqual(paths, expectedPaths) {
		t.Fatalf("manifest paths = %#v, want %#v", paths, expectedPaths)
	}
	if manifest.Scope.Type != "datasets_formats" || manifest.Scope.Value != "goa_human,mgi|gaf" {
		t.Fatalf("manifest scope = %#v", manifest.Scope)
	}
}

func TestRunFetchAnnotationDiscoverNoMatchingFiles(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/ontology/go-basic.obo":
			_, _ = writer.Write([]byte("format-version: 1.2\ndata-version: releases/2026-05-01\n"))
		case "/annotations/":
			_, _ = writer.Write([]byte(`<a href="README.txt">README</a>`))
		default:
			t.Fatalf("unexpected path: %s", request.URL.Path)
		}
	}))
	defer server.Close()

	originalCurrent := ontologyCurrentBaseURL
	originalAnnotation := annotationCurrentBaseURL
	t.Cleanup(func() {
		ontologyCurrentBaseURL = originalCurrent
		annotationCurrentBaseURL = originalAnnotation
	})
	ontologyCurrentBaseURL = server.URL + "/ontology/"
	annotationCurrentBaseURL = server.URL + "/annotations/"

	cfg := createDefaultAnnotationConfig()
	cfg.DirOut = t.TempDir()
	cfg.RuleExisting = "skip"
	cfg.RetryMax = 1
	cfg.RetryWait = time.Millisecond
	cfg.WorkersMax = 1

	err := runFetchAnnotation(&cfg)
	if err == nil {
		t.Fatal("runFetchAnnotation returned nil error")
	}
	if !strings.Contains(err.Error(), "no GO annotation files with formats gaf were found") {
		t.Fatalf("error = %v", err)
	}
}

func TestRunFetchAnnotationDryRunResolvesVersionAndWritesRunLog(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/ontology/go-basic.obo":
			_, _ = writer.Write([]byte("format-version: 1.2\ndata-version: releases/2026-05-01\n"))
		case "/annotations/":
			_, _ = writer.Write([]byte(`<a href="goa_human.gaf.gz">goa_human.gaf.gz</a>`))
		default:
			t.Fatalf("unexpected path: %s", request.URL.Path)
		}
	}))
	defer server.Close()

	originalCurrent := ontologyCurrentBaseURL
	originalAnnotation := annotationCurrentBaseURL
	t.Cleanup(func() {
		ontologyCurrentBaseURL = originalCurrent
		annotationCurrentBaseURL = originalAnnotation
	})
	ontologyCurrentBaseURL = server.URL + "/ontology/"
	annotationCurrentBaseURL = server.URL + "/annotations/"

	cfg := createDefaultAnnotationConfig()
	cfg.DirOut = t.TempDir()
	cfg.RuleExisting = "skip"
	cfg.RetryMax = 1
	cfg.RetryWait = time.Millisecond
	cfg.WorkersMax = 1
	cfg.ShouldDryRun = true

	if err := runFetchAnnotation(&cfg); err != nil {
		t.Fatalf("runFetchAnnotation returned error: %v", err)
	}
	if cfg.VersionToken != "2026-05-01" {
		t.Fatalf("VersionToken = %q, want 2026-05-01", cfg.VersionToken)
	}
	fileManifest := filepath.Join(cfg.DirOut, "annotation", "2026-05-01", "manifest.lock")
	if _, err := os.Stat(fileManifest); !os.IsNotExist(err) {
		t.Fatalf("manifest exists or stat failed unexpectedly: %v", err)
	}
	entries, err := os.ReadDir(filepath.Join(cfg.DirOut, "annotation", "2026-05-01", "logs"))
	if err != nil {
		t.Fatalf("os.ReadDir logs returned error: %v", err)
	}
	if len(entries) != 1 || !strings.HasPrefix(entries[0].Name(), "fetch-") {
		t.Fatalf("log entries = %#v", entries)
	}
}

func TestRunLockAnnotationPreservesManifestURL(t *testing.T) {
	dirOut := t.TempDir()
	source := staticasset.Source{
		Database:     "go",
		Asset:        "annotation",
		Source:       "geneontology",
		Version:      "2026-05-01",
		VersionToken: "2026-05-01",
		Scope: staticasset.Scope{
			Type:  "datasets_formats",
			Value: "goa_human|gaf",
		},
		Assets: []staticasset.Asset{{
			Name: "goa_human.gaf.gz",
			Path: "raw/goa_human.gaf.gz",
			URL:  "https://example.test/goa_human.gaf.gz",
		}},
	}
	options := staticasset.Options{DirOut: dirOut, RuleExisting: "skip", RetryMax: 1, WorkersMax: 1}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		_, _ = writer.Write([]byte("annotation"))
	}))
	defer server.Close()
	source.Assets[0].URL = server.URL + "/goa_human.gaf.gz"
	if err := staticasset.Fetch(source, options, nil); err != nil {
		t.Fatalf("Fetch returned error: %v", err)
	}

	cfg := annotationLockConfig{}
	cfg.DirSnapshot = filepath.Join(dirOut, "annotation", "2026-05-01")
	if err := runLockAnnotation(&cfg); err != nil {
		t.Fatalf("runLockAnnotation returned error: %v", err)
	}
	manifest, ok, err := staticasset.ReadManifest(filepath.Join(dirOut, "annotation", "2026-05-01", "manifest.lock"))
	if err != nil {
		t.Fatalf("ReadManifest returned error: %v", err)
	}
	if !ok {
		t.Fatal("manifest was not written")
	}
	if len(manifest.Files) != 1 || manifest.Files[0].URL != server.URL+"/goa_human.gaf.gz" {
		t.Fatalf("manifest files = %#v", manifest.Files)
	}
	if manifest.Scope.Type != "datasets_formats" || manifest.Scope.Value != "goa_human|gaf" {
		t.Fatalf("manifest scope = %#v", manifest.Scope)
	}
}

func TestRunLockAnnotationDerivesURLAndScopeFromRawFiles(t *testing.T) {
	dirOut := t.TempDir()
	dirRaw := filepath.Join(dirOut, "annotation", "2026-05-01", "raw")
	if err := os.MkdirAll(dirRaw, 0o755); err != nil {
		t.Fatalf("os.MkdirAll returned error: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dirRaw, "goa_human.gaf.gz"), []byte("annotation"), 0o644); err != nil {
		t.Fatalf("os.WriteFile returned error: %v", err)
	}

	cfg := annotationLockConfig{}
	cfg.DirSnapshot = filepath.Join(dirOut, "annotation", "2026-05-01")
	if err := runLockAnnotation(&cfg); err != nil {
		t.Fatalf("runLockAnnotation returned error: %v", err)
	}
	manifest, ok, err := staticasset.ReadManifest(filepath.Join(dirOut, "annotation", "2026-05-01", "manifest.lock"))
	if err != nil {
		t.Fatalf("ReadManifest returned error: %v", err)
	}
	if !ok {
		t.Fatal("manifest was not written")
	}
	if len(manifest.Files) != 1 {
		t.Fatalf("manifest files = %#v", manifest.Files)
	}
	wantURL := "https://release.geneontology.org/2026-05-01/annotations/goa_human.gaf.gz"
	if manifest.Files[0].URL != wantURL {
		t.Fatalf("manifest URL = %q, want %q", manifest.Files[0].URL, wantURL)
	}
	if manifest.Scope.Type != "datasets_formats" || manifest.Scope.Value != "goa_human|gaf" {
		t.Fatalf("manifest scope = %#v", manifest.Scope)
	}
}

func TestRunLockAnnotationRejectsInvalidRawFileName(t *testing.T) {
	dirOut := t.TempDir()
	dirRaw := filepath.Join(dirOut, "annotation", "2026-05-01", "raw")
	if err := os.MkdirAll(dirRaw, 0o755); err != nil {
		t.Fatalf("os.MkdirAll returned error: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dirRaw, "README.txt"), []byte("not an annotation asset"), 0o644); err != nil {
		t.Fatalf("os.WriteFile returned error: %v", err)
	}

	cfg := annotationLockConfig{}
	cfg.DirSnapshot = filepath.Join(dirOut, "annotation", "2026-05-01")
	err := runLockAnnotation(&cfg)
	if err == nil {
		t.Fatal("runLockAnnotation returned nil error")
	}
	if !strings.Contains(err.Error(), "invalid GO annotation filename") {
		t.Fatalf("error = %v", err)
	}
}

func TestRunSyncAnnotationDownloadsFromManifest(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		_, _ = writer.Write([]byte("annotation"))
	}))
	defer server.Close()

	dirOut := t.TempDir()
	source := staticasset.Source{
		Database:     "go",
		Asset:        "annotation",
		Source:       "geneontology",
		Version:      "2026-05-01",
		VersionToken: "2026-05-01",
		Scope: staticasset.Scope{
			Type:  "datasets_formats",
			Value: "goa_human|gaf",
		},
		Assets: []staticasset.Asset{{
			Name: "goa_human.gaf.gz",
			Path: "raw/goa_human.gaf.gz",
			URL:  server.URL + "/goa_human.gaf.gz",
		}},
	}
	options := staticasset.Options{DirOut: dirOut, RuleExisting: "skip", RetryMax: 1, WorkersMax: 1}
	if err := staticasset.Fetch(source, options, nil); err != nil {
		t.Fatalf("Fetch returned error: %v", err)
	}
	fileOut := filepath.Join(dirOut, "annotation", "2026-05-01", "raw", "goa_human.gaf.gz")
	if err := os.Remove(fileOut); err != nil {
		t.Fatalf("os.Remove returned error: %v", err)
	}

	cfg := createDefaultAnnotationRestoreConfig()
	cfg.DirOut = dirOut
	cfg.VersionToken = "2026-05-01"
	cfg.RuleExisting = "skip"
	cfg.RetryMax = 1
	cfg.RetryWait = time.Millisecond
	cfg.WorkersMax = 1
	if err := runRestoreAnnotation(&cfg); err != nil {
		t.Fatalf("runRestoreAnnotation returned error: %v", err)
	}
	data, err := os.ReadFile(fileOut)
	if err != nil {
		t.Fatalf("os.ReadFile returned error: %v", err)
	}
	if string(data) != "annotation" {
		t.Fatalf("file content = %q", data)
	}
	manifest, ok, err := staticasset.ReadManifest(filepath.Join(dirOut, "annotation", "2026-05-01", "manifest.lock"))
	if err != nil {
		t.Fatalf("ReadManifest returned error: %v", err)
	}
	if !ok {
		t.Fatal("manifest was not written")
	}
	if manifest.Scope.Type != "datasets_formats" || manifest.Scope.Value != "goa_human|gaf" {
		t.Fatalf("manifest scope = %#v", manifest.Scope)
	}
}

func TestRunSyncAnnotationDerivesMissingManifestScope(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		_, _ = writer.Write([]byte("annotation"))
	}))
	defer server.Close()

	dirOut := t.TempDir()
	source := staticasset.Source{
		Database:     "go",
		Asset:        "annotation",
		Source:       "geneontology",
		Version:      "2026-05-01",
		VersionToken: "2026-05-01",
		Assets: []staticasset.Asset{{
			Name: "goa_human.gaf.gz",
			Path: "raw/goa_human.gaf.gz",
			URL:  server.URL + "/goa_human.gaf.gz",
		}},
	}
	options := staticasset.Options{DirOut: dirOut, RuleExisting: "skip", RetryMax: 1, WorkersMax: 1}
	if err := staticasset.Fetch(source, options, nil); err != nil {
		t.Fatalf("Fetch returned error: %v", err)
	}
	fileOut := filepath.Join(dirOut, "annotation", "2026-05-01", "raw", "goa_human.gaf.gz")
	if err := os.Remove(fileOut); err != nil {
		t.Fatalf("os.Remove returned error: %v", err)
	}

	cfg := createDefaultAnnotationRestoreConfig()
	cfg.DirOut = dirOut
	cfg.VersionToken = "2026-05-01"
	cfg.RuleExisting = "skip"
	cfg.RetryMax = 1
	cfg.RetryWait = time.Millisecond
	cfg.WorkersMax = 1
	if err := runRestoreAnnotation(&cfg); err != nil {
		t.Fatalf("runRestoreAnnotation returned error: %v", err)
	}
	manifest, ok, err := staticasset.ReadManifest(filepath.Join(dirOut, "annotation", "2026-05-01", "manifest.lock"))
	if err != nil {
		t.Fatalf("ReadManifest returned error: %v", err)
	}
	if !ok {
		t.Fatal("manifest was not written")
	}
	if manifest.Scope.Type != "datasets_formats" || manifest.Scope.Value != "goa_human|gaf" {
		t.Fatalf("manifest scope = %#v", manifest.Scope)
	}
}

func TestAnnotationCommandHelpListsActions(t *testing.T) {
	command := NewCommand()
	var out bytes.Buffer
	command.SetOut(&out)
	command.SetErr(&out)
	command.SetArgs([]string{"annotation", "--help"})
	if err := command.Execute(); err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	text := out.String()
	for _, want := range []string{"fetch", "lock", "restore"} {
		if !strings.Contains(text, want) {
			t.Fatalf("help output missing %q: %s", want, text)
		}
	}
}
