package wikipathways

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

func TestParseGMTAssetsFromIndex(t *testing.T) {
	data := []byte(`
<html><body>
<a href="../">Parent</a>
<a href="readme.txt">readme.txt</a>
<a href="wikipathways-20260510-gmt-Homo_sapiens.gmt">Homo</a>
<a href="wikipathways-20260510-gmt-Mus_musculus.gmt">Mouse</a>
</body></html>
`)
	assets, err := parseGMTAssetsFromIndex(data, "https://data.wikipathways.org/current/gmt/")
	if err != nil {
		t.Fatalf("parseGMTAssetsFromIndex returned error: %v", err)
	}
	if len(assets) != 2 {
		t.Fatalf("len(assets) = %d, want 2", len(assets))
	}
	if assets[0].species != "Homo_sapiens" || assets[0].versionToken != "2026-05-10" {
		t.Fatalf("assets[0] = %#v", assets[0])
	}
}

func TestResolveGMTAssets(t *testing.T) {
	assetsAvailable := []gmtAsset{
		{species: "Homo_sapiens", fileName: "hsa.gmt"},
		{species: "Mus_musculus", fileName: "mmu.gmt"},
	}
	assets, err := resolveGMTAssets(assetsAvailable, []string{"Mus_musculus,Homo_sapiens"}, false)
	if err != nil {
		t.Fatalf("resolveGMTAssets returned error: %v", err)
	}
	if !reflect.DeepEqual([]string{assets[0].species, assets[1].species}, []string{"Homo_sapiens", "Mus_musculus"}) {
		t.Fatalf("assets = %#v", assets)
	}
}

func TestResolveGMTAssetsRejectsUnknown(t *testing.T) {
	_, err := resolveGMTAssets([]gmtAsset{{species: "Homo_sapiens"}}, []string{"Unknown_species"}, false)
	if err == nil {
		t.Fatal("resolveGMTAssets returned nil error")
	}
	if !strings.Contains(err.Error(), "available_species=1") {
		t.Fatalf("error = %q", err.Error())
	}
}

func TestValidateGMTVersionTokenRejectsHistorical(t *testing.T) {
	err := validateGMTVersionToken("2026-05-10")
	if err == nil {
		t.Fatal("validateGMTVersionToken returned nil error")
	}
	if !strings.Contains(err.Error(), "not implemented") {
		t.Fatalf("error = %q", err.Error())
	}
}

func TestBuildGMTStaticAssets(t *testing.T) {
	assets := buildGMTStaticAssets([]gmtAsset{{
		species:  "Homo_sapiens",
		fileName: "wikipathways-20260510-gmt-Homo_sapiens.gmt",
		url:      "https://example.test/wikipathways-20260510-gmt-Homo_sapiens.gmt",
	}})
	expected := []staticasset.Asset{{
		Name: "Homo_sapiens",
		Path: "raw/Homo_sapiens/wikipathways-20260510-gmt-Homo_sapiens.gmt",
		URL:  "https://example.test/wikipathways-20260510-gmt-Homo_sapiens.gmt",
	}}
	if !reflect.DeepEqual(assets, expected) {
		t.Fatalf("assets = %#v, want %#v", assets, expected)
	}
}

func TestRunFetchGMTDownloadsAndReuses(t *testing.T) {
	countGMT := 0
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/gmt/":
			_, _ = writer.Write([]byte(`<a href="wikipathways-20260510-gmt-Homo_sapiens.gmt">hsa</a>`))
		case "/gmt/wikipathways-20260510-gmt-Homo_sapiens.gmt":
			if request.Method == http.MethodGet {
				countGMT++
			}
			_, _ = writer.Write([]byte("WP1\tPathway\tA\tB\n"))
		default:
			t.Fatalf("unexpected path: %s", request.URL.Path)
		}
	}))
	defer server.Close()

	originalBaseURL := gmtCurrentBaseURL
	t.Cleanup(func() { gmtCurrentBaseURL = originalBaseURL })
	gmtCurrentBaseURL = server.URL + "/gmt/"

	cfg := createDefaultGMTConfig()
	cfg.DirOut = t.TempDir()
	cfg.RetryMax = 1
	cfg.WorkersMax = 1
	cfg.speciesNames = []string{"Homo_sapiens"}

	if err := runFetchGMT(&cfg, strings.NewReader(""), ioDiscard{}); err != nil {
		t.Fatalf("runFetchGMT first run returned error: %v", err)
	}
	if err := runFetchGMT(&cfg, strings.NewReader(""), ioDiscard{}); err != nil {
		t.Fatalf("runFetchGMT second run returned error: %v", err)
	}
	if countGMT != 1 {
		t.Fatalf("countGMT = %d, want 1", countGMT)
	}
	manifest, ok, err := staticasset.ReadManifest(filepath.Join(cfg.DirOut, "gmt", "2026-05-10", "manifest.lock"))
	if err != nil {
		t.Fatalf("ReadManifest returned error: %v", err)
	}
	if !ok {
		t.Fatal("manifest missing")
	}
	if manifest.Database != "wikipathways" || manifest.Asset != "gmt" {
		t.Fatalf("manifest identity = %#v", manifest)
	}
}

func TestRunSyncGMTRehydratesManifest(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/gmt/":
			_, _ = writer.Write([]byte(`<a href="wikipathways-20260510-gmt-Homo_sapiens.gmt">hsa</a>`))
		case "/gmt/wikipathways-20260510-gmt-Homo_sapiens.gmt":
			_, _ = writer.Write([]byte("WP1\tPathway\tA\tB\n"))
		}
	}))
	defer server.Close()

	originalBaseURL := gmtCurrentBaseURL
	t.Cleanup(func() { gmtCurrentBaseURL = originalBaseURL })
	gmtCurrentBaseURL = server.URL + "/gmt/"

	cfg := createDefaultGMTConfig()
	cfg.DirOut = t.TempDir()
	cfg.RetryMax = 1
	cfg.WorkersMax = 1
	cfg.speciesNames = []string{"Homo_sapiens"}
	if err := runFetchGMT(&cfg, strings.NewReader(""), ioDiscard{}); err != nil {
		t.Fatalf("runFetchGMT returned error: %v", err)
	}
	fileOut := filepath.Join(cfg.DirOut, "gmt", "2026-05-10", "raw", "Homo_sapiens", "wikipathways-20260510-gmt-Homo_sapiens.gmt")
	if err := os.Remove(fileOut); err != nil {
		t.Fatalf("os.Remove returned error: %v", err)
	}
	syncCfg := gmtSyncConfig{}
	syncCfg.DirOut = cfg.DirOut
	syncCfg.VersionToken = "2026-05-10"
	syncCfg.RuleExisting = "skip"
	syncCfg.RetryMax = 1
	syncCfg.WorkersMax = 1
	if err := runSyncGMT(&syncCfg); err != nil {
		t.Fatalf("runSyncGMT returned error: %v", err)
	}
	if _, err := os.Stat(fileOut); err != nil {
		t.Fatalf("synced file missing: %v", err)
	}
}

func TestRunFetchGMTAllSpeciesRequiresConfirmation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		_, _ = writer.Write([]byte(`<a href="wikipathways-20260510-gmt-Homo_sapiens.gmt">hsa</a>`))
	}))
	defer server.Close()

	originalBaseURL := gmtCurrentBaseURL
	t.Cleanup(func() { gmtCurrentBaseURL = originalBaseURL })
	gmtCurrentBaseURL = server.URL + "/"

	cfg := createDefaultGMTConfig()
	cfg.DirOut = t.TempDir()
	cfg.RetryMax = 1
	cfg.WorkersMax = 1
	cfg.ShouldDryRun = true
	cfg.shouldDownloadAll = true
	err := runFetchGMT(&cfg, strings.NewReader("no\n"), ioDiscard{})
	if err == nil {
		t.Fatal("runFetchGMT returned nil error without confirmation")
	}
}

type ioDiscard struct{}

func (ioDiscard) Write(p []byte) (int, error) {
	return len(p), nil
}
