package geneontology

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/FuqingZh/biofetch/internal/shared/httpx"
	"github.com/FuqingZh/biofetch/internal/shared/staticasset"
)

func TestResolveSlimSubsetsDefaultsToGeneric(t *testing.T) {
	subsets, err := resolveSlimSubsets(nil)
	if err != nil {
		t.Fatalf("resolveSlimSubsets returned error: %v", err)
	}
	expected := []string{"goslim_generic"}
	if !reflect.DeepEqual(subsets, expected) {
		t.Fatalf("subsets = %#v, want %#v", subsets, expected)
	}
}

func TestResolveSlimFormatsSupportsAtFileAndComma(t *testing.T) {
	fileFormats := filepath.Join(t.TempDir(), "formats.txt")
	if err := os.WriteFile(fileFormats, []byte("# comment\njson\n"), 0o644); err != nil {
		t.Fatalf("os.WriteFile returned error: %v", err)
	}
	formats, err := resolveSlimFormats([]string{"obo,tsv", "json"})
	if err != nil {
		t.Fatalf("resolveSlimFormats returned error: %v", err)
	}
	expected := []string{"json", "obo", "tsv"}
	if !reflect.DeepEqual(formats, expected) {
		t.Fatalf("formats = %#v, want %#v", formats, expected)
	}
}

func TestResolveSlimFormatsRejectsUnknown(t *testing.T) {
	_, err := resolveSlimFormats([]string{"xml"})
	if err == nil {
		t.Fatal("resolveSlimFormats returned nil error for unknown format")
	}
}

func TestBuildSlimAssetsUsesSubsetBaseURL(t *testing.T) {
	assets := buildSlimAssets(slimCurrentBaseURL, []string{"goslim_generic"}, []string{"obo", "tsv"})
	expected := []staticasset.Asset{
		{
			Name: "goslim_generic.obo",
			Path: "raw/goslim_generic.obo",
			URL:  "https://current.geneontology.org/ontology/subsets/goslim_generic.obo",
		},
		{
			Name: "goslim_generic.tsv",
			Path: "raw/goslim_generic.tsv",
			URL:  "https://current.geneontology.org/ontology/subsets/goslim_generic.tsv",
		},
	}
	if !reflect.DeepEqual(assets, expected) {
		t.Fatalf("assets = %#v, want %#v", assets, expected)
	}
}

func TestBuildSlimReleaseBaseURL(t *testing.T) {
	got := buildSlimReleaseBaseURL("2026-01-23")
	want := "https://release.geneontology.org/2026-01-23/ontology/subsets/"
	if got != want {
		t.Fatalf("buildSlimReleaseBaseURL = %q, want %q", got, want)
	}
}

func TestResolveSlimSourceCurrent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/ontology/go-basic.obo" {
			t.Fatalf("unexpected path: %s", request.URL.Path)
		}
		_, _ = writer.Write([]byte("format-version: 1.2\ndata-version: releases/2026-05-01\n"))
	}))
	defer server.Close()

	originalCurrent := ontologyCurrentBaseURL
	originalSlim := slimCurrentBaseURL
	t.Cleanup(func() {
		ontologyCurrentBaseURL = originalCurrent
		slimCurrentBaseURL = originalSlim
	})
	ontologyCurrentBaseURL = server.URL + "/ontology/"
	slimCurrentBaseURL = server.URL + "/ontology/subsets/"

	source, err := resolveSlimSource(httpx.NewClient(false), "", httpx.NewRequestLimiter(0))
	if err != nil {
		t.Fatalf("resolveSlimSource returned error: %v", err)
	}
	if source.versionToken != "2026-05-01" {
		t.Fatalf("versionToken = %q", source.versionToken)
	}
	if source.baseURL != server.URL+"/ontology/subsets/" {
		t.Fatalf("baseURL = %q", source.baseURL)
	}
}

func TestRunFetchSlimDownloadsManifest(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/ontology/go-basic.obo":
			_, _ = writer.Write([]byte("format-version: 1.2\ndata-version: releases/2026-05-01\n"))
		case "/ontology/subsets/goslim_generic.obo":
			_, _ = writer.Write([]byte("id: GO:0000001\nsubset: goslim_generic\n"))
		default:
			t.Fatalf("unexpected path: %s", request.URL.Path)
		}
	}))
	defer server.Close()

	originalCurrent := ontologyCurrentBaseURL
	originalSlim := slimCurrentBaseURL
	t.Cleanup(func() {
		ontologyCurrentBaseURL = originalCurrent
		slimCurrentBaseURL = originalSlim
	})
	ontologyCurrentBaseURL = server.URL + "/ontology/"
	slimCurrentBaseURL = server.URL + "/ontology/subsets/"

	cfg := createDefaultSlimConfig()
	cfg.DirOut = t.TempDir()
	cfg.RuleExisting = "skip"
	cfg.RetryMax = 1
	cfg.RetryWait = time.Millisecond
	cfg.WorkersMax = 1
	cfg.subsetNames = []string{"goslim_generic"}
	cfg.formatNames = []string{"obo"}

	if err := runFetchSlim(&cfg); err != nil {
		t.Fatalf("runFetchSlim returned error: %v", err)
	}
	fileManifest := filepath.Join(cfg.DirOut, "slim", "2026-05-01", "manifest.lock")
	manifest, ok, err := staticasset.ReadManifest(fileManifest)
	if err != nil {
		t.Fatalf("ReadManifest returned error: %v", err)
	}
	if !ok {
		t.Fatal("manifest was not written")
	}
	if manifest.Database != "go" || manifest.Asset != "slim" || manifest.Source != "geneontology" {
		t.Fatalf("manifest identity = %#v", manifest)
	}
	if len(manifest.Files) != 1 || !strings.HasSuffix(manifest.Files[0].URL, "/ontology/subsets/goslim_generic.obo") {
		t.Fatalf("manifest files = %#v", manifest.Files)
	}
}
