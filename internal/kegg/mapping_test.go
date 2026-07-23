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
	for _, values := range [][]string{nil, []string{"all"}} {
		assets, err := resolveMappingAssetNames(values)
		if err != nil {
			t.Fatalf("resolveMappingAssetNames(%#v) returned error: %v", values, err)
		}
		if !reflect.DeepEqual(assets, mappingAssetNamesSupported) {
			t.Fatalf("assets = %#v, want %#v", assets, mappingAssetNamesSupported)
		}
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
	type assetFields struct {
		name string
		path string
		url  string
	}
	got := make([]assetFields, 0, len(assets))
	for _, asset := range assets {
		got = append(got, assetFields{name: asset.Name, path: asset.Path, url: asset.URL})
	}
	expected := []assetFields{
		{name: "organism", path: "raw/organism/list_organism.tsv", url: "https://example.test/list/genome"},
		{name: "hsa.conv_uniprot", path: "raw/hsa/conv_uniprot.tsv", url: "https://example.test/conv/hsa/uniprot"},
		{name: "hsa.gene_ko", path: "raw/hsa/gene_ko.tsv", url: "https://example.test/link/ko/hsa"},
		{name: "ko_pathway", path: "raw/ko/ko_pathway.tsv", url: "https://example.test/link/pathway/ko"},
	}
	if !reflect.DeepEqual(got, expected) {
		t.Fatalf("assets = %#v, want %#v", got, expected)
	}
	if assets[1].RecoverDownloadError == nil {
		t.Fatal("conv_uniprot recoverer is nil")
	}
	if assets[2].RecoverDownloadError == nil {
		t.Fatal("gene_ko recoverer is nil")
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
		case "/list/genome":
			_, _ = writer.Write([]byte("T01001\thsa\tHomo sapiens\tEukaryotes;Animals\n"))
		case "/conv/hsa/uniprot":
			if request.Method == http.MethodGet {
				countConv++
			}
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
		case "/list/genome":
			_, _ = writer.Write([]byte("T01001\thsa; Homo sapiens (human)\nT01002\tmmu; Mus musculus (mouse)\n"))
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

func TestRunFetchMappingWritesEmptyConversionFileOnStatus400(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/conv/aacd/uniprot":
			http.Error(writer, "no conversion", http.StatusBadRequest)
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
	cfg.assetNames = []string{"conv_uniprot"}
	cfg.organismCodes = []string{"aacd"}
	if err := runFetchMapping(&cfg); err != nil {
		t.Fatalf("runFetchMapping returned error: %v", err)
	}

	fileOut := filepath.Join(cfg.dirOut, "mapping", "2026-05", "raw", "aacd", "conv_uniprot.tsv")
	info, err := os.Stat(fileOut)
	if err != nil {
		t.Fatalf("fileOut missing: %v", err)
	}
	if info.Size() != 0 {
		t.Fatalf("file size = %d, want 0", info.Size())
	}
	manifest, ok, err := staticasset.ReadManifest(filepath.Join(cfg.dirOut, "mapping", "2026-05", "manifest.lock"))
	if err != nil {
		t.Fatalf("ReadManifest returned error: %v", err)
	}
	if !ok {
		t.Fatal("manifest was not written")
	}
	if len(manifest.Files) != 1 || manifest.Files[0].Bytes != 0 {
		t.Fatalf("manifest files = %#v", manifest.Files)
	}
}

func TestRunFetchMappingWritesEmptyOrganismScopedFileOnStatus400(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/list/aacu":
			http.Error(writer, "bad organism", http.StatusBadRequest)
		case "/link/ko/aacd":
			http.Error(writer, "bad organism", http.StatusBadRequest)
		case "/link/pathway/aacp":
			http.Error(writer, "bad organism", http.StatusBadRequest)
		default:
			t.Fatalf("path = %s", request.URL.Path)
		}
	}))
	defer server.Close()

	originalBaseURL := keggMappingBaseURL
	t.Cleanup(func() { keggMappingBaseURL = originalBaseURL })
	keggMappingBaseURL = server.URL

	cases := []struct {
		asset        string
		organismCode string
		pathRel      string
	}{
		{asset: "gene_list", organismCode: "aacu", pathRel: filepath.Join("raw", "aacu", "gene_list.tsv")},
		{asset: "gene_ko", organismCode: "aacd", pathRel: filepath.Join("raw", "aacd", "gene_ko.tsv")},
		{asset: "gene_pathway", organismCode: "aacp", pathRel: filepath.Join("raw", "aacp", "gene_pathway.tsv")},
	}
	for _, tc := range cases {
		cfg := createDefaultMappingConfig()
		cfg.dirOut = t.TempDir()
		cfg.versionToken = "2026-05"
		cfg.retryMax = 1
		cfg.workersMax = 1
		cfg.requestInterval = 0
		cfg.assetNames = []string{tc.asset}
		cfg.organismCodes = []string{tc.organismCode}
		if err := runFetchMapping(&cfg); err != nil {
			t.Fatalf("runFetchMapping %s returned error: %v", tc.asset, err)
		}
		info, err := os.Stat(filepath.Join(cfg.dirOut, "mapping", "2026-05", tc.pathRel))
		if err != nil {
			t.Fatalf("%s missing: %v", tc.pathRel, err)
		}
		if info.Size() != 0 {
			t.Fatalf("%s size = %d, want 0", tc.pathRel, info.Size())
		}
	}
}

func TestRunLockMappingRejectsMissingVersion(t *testing.T) {
	cfg := mappingLockConfig{dirSnapshot: t.TempDir()}
	err := runLockMapping(&cfg)
	if err == nil {
		t.Fatal("runLockMapping returned nil error")
	}
}

func TestRunSyncMappingRejectsInvalidVersion(t *testing.T) {
	cfg := mappingRestoreConfig{dirOut: t.TempDir(), versionToken: "current", ruleExisting: "skip", retryMax: 1, workersMax: 1}
	err := runRestoreMapping(&cfg)
	if err == nil {
		t.Fatal("runRestoreMapping returned nil error")
	}
}
