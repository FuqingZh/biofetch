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

func TestNormalizeMapperVersionToken(t *testing.T) {
	versionToken, err := normalizeMapperVersionToken("7")
	if err != nil {
		t.Fatalf("normalizeMapperVersionToken returned error: %v", err)
	}
	if versionToken != "7.0.0" {
		t.Fatalf("versionToken = %q, want 7.0.0", versionToken)
	}
}

func TestNormalizeMapperVersionTokenRejectsCurrent(t *testing.T) {
	_, err := normalizeMapperVersionToken("current")
	if err == nil {
		t.Fatal("normalizeMapperVersionToken returned nil error")
	}
}

func TestResolveMapperAssets(t *testing.T) {
	assets, err := resolveMapperAssets([]string{"diamond,db", "taxa"})
	if err != nil {
		t.Fatalf("resolveMapperAssets returned error: %v", err)
	}
	expected := []string{"db", "diamond", "taxa"}
	if !reflect.DeepEqual(assets, expected) {
		t.Fatalf("assets = %#v, want %#v", assets, expected)
	}
}

func TestResolveMapperAssetsRejectsUnknown(t *testing.T) {
	_, err := resolveMapperAssets([]string{"cog"})
	if err == nil {
		t.Fatal("resolveMapperAssets returned nil error")
	}
	if !strings.Contains(err.Error(), "supported") {
		t.Fatalf("error = %q", err.Error())
	}
}

func TestBuildMapperStaticAssets(t *testing.T) {
	assets := buildMapperStaticAssets("https://example.test/emapper", "7.0.0", []string{"manifest"})
	expected := []staticasset.Asset{{
		Name: "manifest",
		Path: "raw/manifest.json",
		URL:  "https://example.test/emapper/emapperdb-7.0.0/manifest.json",
	}}
	if !reflect.DeepEqual(assets, expected) {
		t.Fatalf("assets = %#v, want %#v", assets, expected)
	}
}

func TestRunFetchMapperRequiresLargeDownloadFlag(t *testing.T) {
	cfg := createDefaultMapperConfig()
	cfg.DirOut = t.TempDir()
	cfg.assetNames = []string{"db"}
	err := runFetchMapper(&cfg)
	if err == nil {
		t.Fatal("runFetchMapper returned nil error")
	}
	if !strings.Contains(err.Error(), "should_allow_large_download") {
		t.Fatalf("error = %q", err.Error())
	}
	if _, statErr := os.Stat(filepath.Join(cfg.DirOut, "mapper")); !os.IsNotExist(statErr) {
		t.Fatalf("mapper directory exists or stat failed unexpectedly: %v", statErr)
	}
}

func TestRunFetchMapperDownloadsAndReuses(t *testing.T) {
	countManifest := 0
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/emapperdb-7.0.0/manifest.json":
			countManifest++
			_, _ = writer.Write([]byte(`{"schema_version":1}`))
		default:
			t.Fatalf("path = %s", request.URL.Path)
		}
	}))
	defer server.Close()

	cfg := createDefaultMapperConfig()
	cfg.DirOut = t.TempDir()
	cfg.RetryMax = 1
	cfg.WorkersMax = 1
	cfg.assetNames = []string{"manifest"}
	cfg.baseURL = server.URL
	if err := runFetchMapper(&cfg); err != nil {
		t.Fatalf("runFetchMapper first run returned error: %v", err)
	}
	if err := runFetchMapper(&cfg); err != nil {
		t.Fatalf("runFetchMapper second run returned error: %v", err)
	}
	if countManifest != 1 {
		t.Fatalf("countManifest = %d, want 1", countManifest)
	}

	fileOut := filepath.Join(cfg.DirOut, "mapper", "7.0.0", "raw", "manifest.json")
	if _, err := os.Stat(fileOut); err != nil {
		t.Fatalf("fileOut missing: %v", err)
	}
	manifest, ok, err := staticasset.ReadManifest(filepath.Join(cfg.DirOut, "mapper", "7.0.0", "manifest.lock"))
	if err != nil {
		t.Fatalf("ReadManifest returned error: %v", err)
	}
	if !ok {
		t.Fatal("manifest was not written")
	}
	if manifest.Database != "eggnog" || manifest.Asset != "mapper" || manifest.Source != "download" {
		t.Fatalf("manifest identity = %#v", manifest)
	}
}

func TestRunLockMapperRejectsCurrentVersion(t *testing.T) {
	cfg := mapperLockConfig{}
	cfg.DirOut = t.TempDir()
	cfg.VersionToken = "current"
	err := runLockMapper(&cfg)
	if err == nil {
		t.Fatal("runLockMapper returned nil error")
	}
}

func TestRunSyncMapperRejectsCurrentVersion(t *testing.T) {
	cfg := mapperSyncConfig{}
	cfg.DirOut = t.TempDir()
	cfg.VersionToken = "current"
	cfg.RuleExisting = "skip"
	cfg.RetryMax = 1
	cfg.WorkersMax = 1
	err := runSyncMapper(&cfg)
	if err == nil {
		t.Fatal("runSyncMapper returned nil error")
	}
}
