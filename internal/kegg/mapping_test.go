package kegg

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"biofetch/internal/shared/staticasset"
)

func TestResolveMappingAssetNamesDefaultsToAll(t *testing.T) {
	assets, err := resolveMappingAssetNames(nil)
	if err != nil {
		t.Fatalf("resolveMappingAssetNames returned error: %v", err)
	}
	if !reflect.DeepEqual(assets, mappingAssetNamesSupported) {
		t.Fatalf("assets = %#v, want %#v", assets, mappingAssetNamesSupported)
	}
}

func TestResolveMappingAssetNamesRejectsUnknown(t *testing.T) {
	_, err := resolveMappingAssetNames([]string{"pathway_class"})
	if err == nil {
		t.Fatal("resolveMappingAssetNames returned nil error")
	}
}

func TestBuildMappingStaticAssets(t *testing.T) {
	originalBaseURL := keggMappingBaseURL
	t.Cleanup(func() { keggMappingBaseURL = originalBaseURL })
	keggMappingBaseURL = "https://example.test"

	assets := buildMappingStaticAssets([]string{"organism", "conv_uniprot", "gene_ko", "ko_pathway"}, []string{"hsa"})
	expected := []staticasset.Asset{
		{Name: "organism", Path: "raw/organism/list_organism.tsv", URL: "https://example.test/list/organism"},
		{Name: "hsa.conv_uniprot", Path: "raw/hsa/conv_uniprot.tsv", URL: "https://example.test/conv/hsa/uniprot"},
		{Name: "hsa.gene_ko", Path: "raw/hsa/gene_ko.tsv", URL: "https://example.test/link/ko/hsa"},
		{Name: "ko_pathway", Path: "raw/ko/ko_pathway.tsv", URL: "https://example.test/link/pathway/ko"},
	}
	if !reflect.DeepEqual(assets, expected) {
		t.Fatalf("assets = %#v, want %#v", assets, expected)
	}
}

func TestValidateMappingFetchConfigRequiresOrganismScope(t *testing.T) {
	cfg := createDefaultMappingConfig()
	cfg.dirOut = t.TempDir()
	cfg.assetNames = []string{"conv_uniprot"}
	err := validateMappingFetchConfig(&cfg)
	if err == nil {
		t.Fatal("validateMappingFetchConfig returned nil error")
	}
	if !strings.Contains(err.Error(), "organism-scoped") {
		t.Fatalf("error = %q", err.Error())
	}
}

func TestValidateMappingFetchConfigAllowsGlobalOnly(t *testing.T) {
	cfg := createDefaultMappingConfig()
	cfg.dirOut = t.TempDir()
	cfg.assetNames = []string{"organism,ko_pathway"}
	if err := validateMappingFetchConfig(&cfg); err != nil {
		t.Fatalf("validateMappingFetchConfig returned error: %v", err)
	}
}

func TestRunFetchMappingDownloadsAndReuses(t *testing.T) {
	countConv := 0
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/list/organism":
			_, _ = writer.Write([]byte("T01001\thsa\tHomo sapiens\tEukaryotes;Animals\n"))
		case "/conv/hsa/uniprot":
			countConv++
			_, _ = writer.Write([]byte("hsa:10458\tup:P12345\n"))
		case "/link/pathway/ko":
			_, _ = writer.Write([]byte("ko:K00001\tpath:map00010\n"))
		default:
			t.Fatalf("path = %s", request.URL.Path)
		}
	}))
	defer server.Close()

	originalBaseURL := keggMappingBaseURL
	t.Cleanup(func() { keggMappingBaseURL = originalBaseURL })
	keggMappingBaseURL = server.URL

	cfg := createDefaultMappingConfig()
	cfg.dirOut = t.TempDir()
	cfg.versionToken = "2026-05"
	cfg.retryMax = 1
	cfg.workersMax = 1
	cfg.requestInterval = 0
	cfg.assetNames = []string{"conv_uniprot,ko_pathway"}
	cfg.organismCodes = []string{"hsa"}
	if err := runFetchMapping(&cfg); err != nil {
		t.Fatalf("runFetchMapping first run returned error: %v", err)
	}
	if err := runFetchMapping(&cfg); err != nil {
		t.Fatalf("runFetchMapping second run returned error: %v", err)
	}
	if countConv != 1 {
		t.Fatalf("countConv = %d, want 1", countConv)
	}

	fileOut := filepath.Join(cfg.dirOut, "mapping", "2026-05", "raw", "hsa", "conv_uniprot.tsv")
	if _, err := os.Stat(fileOut); err != nil {
		t.Fatalf("fileOut missing: %v", err)
	}
	manifest, ok, err := staticasset.ReadManifest(filepath.Join(cfg.dirOut, "mapping", "2026-05", "manifest.lock"))
	if err != nil {
		t.Fatalf("ReadManifest returned error: %v", err)
	}
	if !ok {
		t.Fatal("manifest was not written")
	}
	if manifest.Database != "kegg" || manifest.Asset != "mapping" || manifest.Source != "rest" {
		t.Fatalf("manifest identity = %#v", manifest)
	}
	if manifest.Scope.Type != "organism" || manifest.Scope.Value != "hsa" {
		t.Fatalf("manifest scope = %#v", manifest.Scope)
	}
	if len(manifest.Files) != 2 {
		t.Fatalf("manifest files = %#v", manifest.Files)
	}
}

func TestRunFetchMappingDownloadAllResolvesOrganisms(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/list/organism":
			_, _ = writer.Write([]byte("T01001\thsa\tHomo sapiens\tEukaryotes;Animals\nT01002\tmmu\tMus musculus\tEukaryotes;Animals\n"))
		case "/link/ko/hsa", "/link/ko/mmu":
			_, _ = writer.Write([]byte("hsa:1\tko:K00001\n"))
		default:
			t.Fatalf("path = %s", request.URL.Path)
		}
	}))
	defer server.Close()

	originalBaseURL := keggMappingBaseURL
	t.Cleanup(func() { keggMappingBaseURL = originalBaseURL })
	keggMappingBaseURL = server.URL

	cfg := createDefaultMappingConfig()
	cfg.dirOut = t.TempDir()
	cfg.versionToken = "2026-05"
	cfg.retryMax = 1
	cfg.workersMax = 1
	cfg.requestInterval = 0
	cfg.assetNames = []string{"gene_ko"}
	cfg.shouldDownloadAll = true
	if err := runFetchMapping(&cfg); err != nil {
		t.Fatalf("runFetchMapping returned error: %v", err)
	}
	if _, err := os.Stat(filepath.Join(cfg.dirOut, "mapping", "2026-05", "raw", "hsa", "gene_ko.tsv")); err != nil {
		t.Fatalf("hsa gene_ko missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(cfg.dirOut, "mapping", "2026-05", "raw", "mmu", "gene_ko.tsv")); err != nil {
		t.Fatalf("mmu gene_ko missing: %v", err)
	}
}

func TestRunLockMappingRejectsMissingVersion(t *testing.T) {
	cfg := mappingLockConfig{dirOut: t.TempDir()}
	err := runLockMapping(&cfg)
	if err == nil {
		t.Fatal("runLockMapping returned nil error")
	}
}

func TestRunSyncMappingRejectsInvalidVersion(t *testing.T) {
	cfg := mappingSyncConfig{dirOut: t.TempDir(), versionToken: "current", ruleExisting: "skip", retryMax: 1, workersMax: 1}
	err := runSyncMapping(&cfg)
	if err == nil {
		t.Fatal("runSyncMapping returned nil error")
	}
}
