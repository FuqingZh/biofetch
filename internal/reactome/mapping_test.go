package reactome

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

func TestResolveMappingAssets(t *testing.T) {
	assets, err := resolveMappingAssets([]string{"ReactomePathways.txt,UniProt2Reactome_All_Levels.txt"})
	if err != nil {
		t.Fatalf("resolveMappingAssets returned error: %v", err)
	}
	expected := []string{"ReactomePathways.txt", "UniProt2Reactome_All_Levels.txt"}
	if !reflect.DeepEqual(assets, expected) {
		t.Fatalf("assets = %#v, want %#v", assets, expected)
	}
}

func TestResolveMappingAssetsRejectsUnknown(t *testing.T) {
	_, err := resolveMappingAssets([]string{"reactome.graphdb.dump"})
	if err == nil {
		t.Fatal("resolveMappingAssets returned nil error for unknown asset")
	}
	if !strings.Contains(err.Error(), "supported") {
		t.Fatalf("error = %q", err.Error())
	}
}

func TestBuildMappingStaticAssets(t *testing.T) {
	assets := buildMappingStaticAssets("https://reactome.org/download/current/", []string{"ReactomePathways.txt"})
	expected := []staticasset.Asset{{
		Name: "ReactomePathways.txt",
		Path: "raw/ReactomePathways.txt",
		URL:  "https://reactome.org/download/current/ReactomePathways.txt",
	}}
	if !reflect.DeepEqual(assets, expected) {
		t.Fatalf("assets = %#v, want %#v", assets, expected)
	}
}

func TestValidateMappingDownloadSizesRejectsLargeFile(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodHead {
			t.Fatalf("method = %s, want HEAD", request.Method)
		}
		writer.Header().Set("Content-Length", "11")
	}))
	defer server.Close()

	err := validateMappingDownloadSizes(server.Client(), []staticasset.Asset{{
		Name: "large.txt",
		URL:  server.URL + "/large.txt",
	}}, 10)
	if err == nil {
		t.Fatal("validateMappingDownloadSizes returned nil error")
	}
}

func TestRunFetchMappingDownloadsAndReuses(t *testing.T) {
	countGet := 0
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.Method {
		case http.MethodHead:
			writer.Header().Set("Content-Length", "8")
		case http.MethodGet:
			countGet++
			_, _ = writer.Write([]byte("pathways"))
		default:
			t.Fatalf("method = %s", request.Method)
		}
	}))
	defer server.Close()

	originalBaseURL := mappingCurrentBaseURL
	t.Cleanup(func() { mappingCurrentBaseURL = originalBaseURL })
	mappingCurrentBaseURL = server.URL + "/"

	cfg := createDefaultMappingConfig()
	cfg.DirOut = t.TempDir()
	cfg.RetryMax = 1
	cfg.WorkersMax = 1
	cfg.assetNames = []string{"ReactomePathways.txt"}

	if err := runFetchMapping(&cfg); err != nil {
		t.Fatalf("runFetchMapping first run returned error: %v", err)
	}
	if err := runFetchMapping(&cfg); err != nil {
		t.Fatalf("runFetchMapping second run returned error: %v", err)
	}
	if countGet != 1 {
		t.Fatalf("countGet = %d, want 1", countGet)
	}

	fileManifest := filepath.Join(cfg.DirOut, "mapping", "current", "manifest.lock")
	manifest, ok, err := staticasset.ReadManifest(fileManifest)
	if err != nil {
		t.Fatalf("ReadManifest returned error: %v", err)
	}
	if !ok {
		t.Fatal("manifest was not written")
	}
	if manifest.Database != "reactome" || manifest.Asset != "mapping" {
		t.Fatalf("manifest identity = %#v", manifest)
	}
	if len(manifest.Files) != 1 || manifest.Files[0].SHA256 == "" || manifest.Files[0].Bytes != 8 {
		t.Fatalf("manifest files = %#v", manifest.Files)
	}
}

func TestRunSyncMappingRehydratesManifest(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.Method {
		case http.MethodHead:
			writer.Header().Set("Content-Length", "8")
		case http.MethodGet:
			_, _ = writer.Write([]byte("pathways"))
		}
	}))
	defer server.Close()

	originalBaseURL := mappingCurrentBaseURL
	t.Cleanup(func() { mappingCurrentBaseURL = originalBaseURL })
	mappingCurrentBaseURL = server.URL + "/"

	cfg := createDefaultMappingConfig()
	cfg.DirOut = t.TempDir()
	cfg.RetryMax = 1
	cfg.WorkersMax = 1
	cfg.assetNames = []string{"ReactomePathways.txt"}
	if err := runFetchMapping(&cfg); err != nil {
		t.Fatalf("runFetchMapping returned error: %v", err)
	}
	fileOut := filepath.Join(cfg.DirOut, "mapping", "current", "raw", "ReactomePathways.txt")
	if err := os.Remove(fileOut); err != nil {
		t.Fatalf("os.Remove returned error: %v", err)
	}
	syncCfg := mappingSyncConfig{}
	syncCfg.DirOut = cfg.DirOut
	syncCfg.VersionToken = "current"
	syncCfg.RuleExisting = "skip"
	syncCfg.RetryMax = 1
	syncCfg.WorkersMax = 1
	if err := runSyncMapping(&syncCfg); err != nil {
		t.Fatalf("runSyncMapping returned error: %v", err)
	}
	if _, err := os.Stat(fileOut); err != nil {
		t.Fatalf("synced file missing: %v", err)
	}
}
