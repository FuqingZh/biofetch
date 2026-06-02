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

func TestResolveMappingAssetsAll(t *testing.T) {
	for _, values := range [][]string{nil, []string{"all"}} {
		assets, err := resolveMappingAssets(values)
		if err != nil {
			t.Fatalf("resolveMappingAssets(%#v) returned error: %v", values, err)
		}
		if !reflect.DeepEqual(assets, mappingAssetsSupported) {
			t.Fatalf("assets = %#v, want %#v", assets, mappingAssetsSupported)
		}
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

func TestResolveMappingFetchVersionTokenCurrent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/ContentService/data/database/version" {
			t.Fatalf("path = %s", request.URL.Path)
		}
		_, _ = writer.Write([]byte("96"))
	}))
	defer server.Close()

	originalURL := mappingCurrentVersionURL
	t.Cleanup(func() { mappingCurrentVersionURL = originalURL })
	mappingCurrentVersionURL = server.URL + "/ContentService/data/database/version"

	versionToken, err := resolveMappingFetchVersionToken(server.Client(), "")
	if err != nil {
		t.Fatalf("resolveMappingFetchVersionToken returned error: %v", err)
	}
	if versionToken != "v96" {
		t.Fatalf("versionToken = %q, want v96", versionToken)
	}
}

func TestNormalizeMappingFixedVersionToken(t *testing.T) {
	for _, input := range []string{"96", "v96", "V96"} {
		versionToken, err := normalizeMappingFixedVersionToken(input)
		if err != nil {
			t.Fatalf("normalizeMappingFixedVersionToken(%q) returned error: %v", input, err)
		}
		if versionToken != "v96" {
			t.Fatalf("versionToken = %q, want v96", versionToken)
		}
	}
}

func TestNormalizeMappingFixedVersionTokenRejectsCurrent(t *testing.T) {
	_, err := normalizeMappingFixedVersionToken("current")
	if err == nil {
		t.Fatal("normalizeMappingFixedVersionToken returned nil error")
	}
}

func TestRunFetchMappingDownloadsAndReuses(t *testing.T) {
	countGet := 0
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/ContentService/data/database/version" {
			_, _ = writer.Write([]byte("96"))
			return
		}
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
	originalVersionURL := mappingCurrentVersionURL
	t.Cleanup(func() {
		mappingCurrentBaseURL = originalBaseURL
		mappingCurrentVersionURL = originalVersionURL
	})
	mappingCurrentBaseURL = server.URL + "/"
	mappingCurrentVersionURL = server.URL + "/ContentService/data/database/version"

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

	fileManifest := filepath.Join(cfg.DirOut, "mapping", "v96", "manifest.lock")
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
	if manifest.Version != "v96" || manifest.VersionToken != "v96" {
		t.Fatalf("manifest version = %#v", manifest)
	}
	if len(manifest.Files) != 1 || manifest.Files[0].SHA256 == "" || manifest.Files[0].Bytes != 8 {
		t.Fatalf("manifest files = %#v", manifest.Files)
	}
}

func TestRunFetchMappingFailsWhenCurrentVersionCannotResolve(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/ContentService/data/database/version" {
			http.Error(writer, "missing", http.StatusNotFound)
			return
		}
		_, _ = writer.Write([]byte("pathways"))
	}))
	defer server.Close()

	originalBaseURL := mappingCurrentBaseURL
	originalVersionURL := mappingCurrentVersionURL
	t.Cleanup(func() {
		mappingCurrentBaseURL = originalBaseURL
		mappingCurrentVersionURL = originalVersionURL
	})
	mappingCurrentBaseURL = server.URL + "/"
	mappingCurrentVersionURL = server.URL + "/ContentService/data/database/version"

	cfg := createDefaultMappingConfig()
	cfg.DirOut = t.TempDir()
	cfg.RetryMax = 1
	cfg.WorkersMax = 1
	cfg.assetNames = []string{"ReactomePathways.txt"}
	err := runFetchMapping(&cfg)
	if err == nil {
		t.Fatal("runFetchMapping returned nil error")
	}
	if _, statErr := os.Stat(filepath.Join(cfg.DirOut, "mapping")); !os.IsNotExist(statErr) {
		t.Fatalf("mapping directory exists or stat failed unexpectedly: %v", statErr)
	}
}

func TestRunLockMappingRejectsCurrentVersion(t *testing.T) {
	cfg := mappingLockConfig{}
	cfg.DirOut = t.TempDir()
	cfg.VersionToken = "current"
	err := runLockMapping(&cfg)
	if err == nil {
		t.Fatal("runLockMapping returned nil error")
	}
}

func TestRunSyncMappingRejectsCurrentVersion(t *testing.T) {
	cfg := mappingSyncConfig{}
	cfg.DirOut = t.TempDir()
	cfg.VersionToken = "current"
	cfg.RuleExisting = "skip"
	cfg.RetryMax = 1
	cfg.WorkersMax = 1
	err := runSyncMapping(&cfg)
	if err == nil {
		t.Fatal("runSyncMapping returned nil error")
	}
}

func TestRunSyncMappingRehydratesManifest(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/ContentService/data/database/version" {
			_, _ = writer.Write([]byte("96"))
			return
		}
		switch request.Method {
		case http.MethodHead:
			writer.Header().Set("Content-Length", "8")
		case http.MethodGet:
			_, _ = writer.Write([]byte("pathways"))
		}
	}))
	defer server.Close()

	originalBaseURL := mappingCurrentBaseURL
	originalVersionURL := mappingCurrentVersionURL
	t.Cleanup(func() {
		mappingCurrentBaseURL = originalBaseURL
		mappingCurrentVersionURL = originalVersionURL
	})
	mappingCurrentBaseURL = server.URL + "/"
	mappingCurrentVersionURL = server.URL + "/ContentService/data/database/version"

	cfg := createDefaultMappingConfig()
	cfg.DirOut = t.TempDir()
	cfg.RetryMax = 1
	cfg.WorkersMax = 1
	cfg.assetNames = []string{"ReactomePathways.txt"}
	if err := runFetchMapping(&cfg); err != nil {
		t.Fatalf("runFetchMapping returned error: %v", err)
	}
	fileOut := filepath.Join(cfg.DirOut, "mapping", "v96", "raw", "ReactomePathways.txt")
	if err := os.Remove(fileOut); err != nil {
		t.Fatalf("os.Remove returned error: %v", err)
	}
	syncCfg := mappingSyncConfig{}
	syncCfg.DirOut = cfg.DirOut
	syncCfg.VersionToken = "96"
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
