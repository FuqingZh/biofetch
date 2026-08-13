package eggnog

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/FuqingZh/biofetch/internal/shared/staticasset"
)

func TestNormalizeMapperVersionToken(t *testing.T) {
	versionToken, err := normalizeMapperVersionToken("5")
	if err != nil {
		t.Fatalf("normalizeMapperVersionToken returned error: %v", err)
	}
	if versionToken != "5.0.2" {
		t.Fatalf("versionToken = %q, want 5.0.2", versionToken)
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

func TestResolveMapperAssetsAll(t *testing.T) {
	for _, values := range [][]string{nil, []string{"all"}} {
		assets, err := resolveMapperAssets(values)
		if err != nil {
			t.Fatalf("resolveMapperAssets(%#v) returned error: %v", values, err)
		}
		expected := []string{"db", "diamond", "taxa"}
		if !reflect.DeepEqual(assets, expected) {
			t.Fatalf("assets = %#v, want %#v", assets, expected)
		}
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
	assets := buildMapperStaticAssets("https://example.test/emapper", "5.0.2", []string{"taxa"})
	expected := []staticasset.Asset{{
		Name: "taxa",
		Path: "raw/eggnog.taxa.tar.gz",
		URL:  "https://example.test/emapper/emapperdb-5.0.2/eggnog.taxa.tar.gz",
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
	if !strings.Contains(err.Error(), "allow-large-downloads") {
		t.Fatalf("error = %q", err.Error())
	}
	if _, statErr := os.Stat(filepath.Join(cfg.DirOut, "mapper")); !os.IsNotExist(statErr) {
		t.Fatalf("mapper directory exists or stat failed unexpectedly: %v", statErr)
	}
}

func TestRunFetchMapperDownloadsAndReuses(t *testing.T) {
	countTaxa := 0
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/emapperdb-5.0.2/eggnog.taxa.tar.gz":
			if request.Method == http.MethodGet {
				countTaxa++
			}
			_, _ = writer.Write([]byte("taxa"))
		default:
			t.Fatalf("path = %s", request.URL.Path)
		}
	}))
	defer server.Close()

	cfg := createDefaultMapperConfig()
	cfg.DirOut = t.TempDir()
	cfg.RetryMax = 1
	cfg.WorkersMax = 1
	cfg.assetNames = []string{"taxa"}
	cfg.shouldAllowLargeAssets = true
	cfg.baseURL = server.URL
	if err := runFetchMapper(&cfg); err != nil {
		t.Fatalf("runFetchMapper first run returned error: %v", err)
	}
	if err := runFetchMapper(&cfg); err != nil {
		t.Fatalf("runFetchMapper second run returned error: %v", err)
	}
	if countTaxa != 1 {
		t.Fatalf("countTaxa = %d, want 1", countTaxa)
	}

	fileOut := filepath.Join(cfg.DirOut, "mapper", "5.0.2", "raw", "eggnog.taxa.tar.gz")
	if _, err := os.Stat(fileOut); err != nil {
		t.Fatalf("fileOut missing: %v", err)
	}
	manifest, ok, err := staticasset.ReadManifest(filepath.Join(cfg.DirOut, "mapper", "5.0.2", "manifest.lock"))
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
	cfg.DirSnapshot = filepath.Join(t.TempDir(), "current")
	err := runLockMapper(&cfg)
	if err == nil {
		t.Fatal("runLockMapper returned nil error")
	}
}

func TestRunSyncMapperRejectsCurrentVersion(t *testing.T) {
	cfg := mapperRestoreConfig{}
	cfg.DirOut = t.TempDir()
	cfg.VersionToken = "current"
	cfg.RuleExisting = "skip"
	cfg.RetryMax = 1
	cfg.WorkersMax = 1
	err := runRestoreMapper(&cfg)
	if err == nil {
		t.Fatal("runRestoreMapper returned nil error")
	}
}
