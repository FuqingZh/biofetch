package eggnog

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

func TestNormalizeCOGVersionToken(t *testing.T) {
	versionToken, err := normalizeCOGVersionToken("2024")
	if err != nil {
		t.Fatalf("normalizeCOGVersionToken returned error: %v", err)
	}
	if versionToken != "COG2024" {
		t.Fatalf("versionToken = %q, want COG2024", versionToken)
	}
}

func TestNormalizeCOGVersionTokenRejectsCurrent(t *testing.T) {
	_, err := normalizeCOGVersionToken("current")
	if err == nil {
		t.Fatal("normalizeCOGVersionToken returned nil error")
	}
}

func TestResolveCOGAssets(t *testing.T) {
	assets, err := resolveCOGAssets([]string{"definition,category_definition"})
	if err != nil {
		t.Fatalf("resolveCOGAssets returned error: %v", err)
	}
	expected := []string{"category_definition", "definition"}
	if !reflect.DeepEqual(assets, expected) {
		t.Fatalf("assets = %#v, want %#v", assets, expected)
	}
}

func TestResolveCOGAssetsAll(t *testing.T) {
	for _, values := range [][]string{nil, []string{"all"}} {
		assets, err := resolveCOGAssets(values)
		if err != nil {
			t.Fatalf("resolveCOGAssets(%#v) returned error: %v", values, err)
		}
		expected := []string{"category_definition", "definition", "readme"}
		if !reflect.DeepEqual(assets, expected) {
			t.Fatalf("assets = %#v, want %#v", assets, expected)
		}
	}
}

func TestResolveCOGAssetsRejectsUnknown(t *testing.T) {
	_, err := resolveCOGAssets([]string{"mapping"})
	if err == nil {
		t.Fatal("resolveCOGAssets returned nil error")
	}
	if !strings.Contains(err.Error(), "supported") {
		t.Fatalf("error = %q", err.Error())
	}
}

func TestBuildCOGStaticAssets(t *testing.T) {
	assets := buildCOGStaticAssets("https://example.test/pub/COG", "COG2024", []string{"category_definition"})
	expected := []staticasset.Asset{{
		Name: "category_definition",
		Path: "raw/cog-24.fun.tab",
		URL:  "https://example.test/pub/COG/COG2024/data/cog-24.fun.tab",
	}}
	if !reflect.DeepEqual(assets, expected) {
		t.Fatalf("assets = %#v, want %#v", assets, expected)
	}
}

func TestRunFetchCOGDownloadsAndReuses(t *testing.T) {
	countCategory := 0
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/COG2024/data/cog-24.fun.tab":
			if request.Method == http.MethodGet {
				countCategory++
			}
			_, _ = writer.Write([]byte("J\tINFORMATION STORAGE AND PROCESSING\tTranslation, ribosomal structure and biogenesis\n"))
		default:
			t.Fatalf("path = %s", request.URL.Path)
		}
	}))
	defer server.Close()

	cfg := createDefaultCOGConfig()
	cfg.DirOut = t.TempDir()
	cfg.RetryMax = 1
	cfg.WorkersMax = 1
	cfg.assetNames = []string{"category_definition"}
	cfg.baseURL = server.URL
	if err := runFetchCOG(&cfg); err != nil {
		t.Fatalf("runFetchCOG first run returned error: %v", err)
	}
	if err := runFetchCOG(&cfg); err != nil {
		t.Fatalf("runFetchCOG second run returned error: %v", err)
	}
	if countCategory != 1 {
		t.Fatalf("countCategory = %d, want 1", countCategory)
	}

	fileOut := filepath.Join(cfg.DirOut, "cog", "COG2024", "raw", "cog-24.fun.tab")
	if _, err := os.Stat(fileOut); err != nil {
		t.Fatalf("fileOut missing: %v", err)
	}
	manifest, ok, err := staticasset.ReadManifest(filepath.Join(cfg.DirOut, "cog", "COG2024", "manifest.lock"))
	if err != nil {
		t.Fatalf("ReadManifest returned error: %v", err)
	}
	if !ok {
		t.Fatal("manifest was not written")
	}
	if manifest.Database != "eggnog" || manifest.Asset != "cog" || manifest.Source != "ncbi" {
		t.Fatalf("manifest identity = %#v", manifest)
	}
	if manifest.Version != "COG2024" || manifest.VersionToken != "COG2024" {
		t.Fatalf("manifest version = %#v", manifest)
	}
}

func TestRunLockCOGRejectsCurrentVersion(t *testing.T) {
	cfg := cogLockConfig{}
	cfg.DirOut = t.TempDir()
	cfg.VersionToken = "current"
	err := runLockCOG(&cfg)
	if err == nil {
		t.Fatal("runLockCOG returned nil error")
	}
}

func TestRunSyncCOGRejectsCurrentVersion(t *testing.T) {
	cfg := cogSyncConfig{}
	cfg.DirOut = t.TempDir()
	cfg.VersionToken = "current"
	cfg.RuleExisting = "skip"
	cfg.RetryMax = 1
	cfg.WorkersMax = 1
	err := runSyncCOG(&cfg)
	if err == nil {
		t.Fatal("runSyncCOG returned nil error")
	}
}
