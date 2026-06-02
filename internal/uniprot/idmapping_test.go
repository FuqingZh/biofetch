package uniprot

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

func TestParseIDMappingReleaseNotes(t *testing.T) {
	versionToken, err := parseIDMappingReleaseNotes([]byte("UniProt Release 2026_01\n"))
	if err != nil {
		t.Fatalf("parseIDMappingReleaseNotes returned error: %v", err)
	}
	if versionToken != "2026_01" {
		t.Fatalf("versionToken = %q, want 2026_01", versionToken)
	}
}

func TestParseIDMappingReleaseNotesRejectsMalformed(t *testing.T) {
	_, err := parseIDMappingReleaseNotes([]byte("release unavailable"))
	if err == nil {
		t.Fatal("parseIDMappingReleaseNotes returned nil error")
	}
}

func TestResolveIDMappingAssets(t *testing.T) {
	assets, err := resolveIDMappingAssets([]string{"selected,dat"})
	if err != nil {
		t.Fatalf("resolveIDMappingAssets returned error: %v", err)
	}
	expected := []string{"dat", "selected"}
	if !reflect.DeepEqual(assets, expected) {
		t.Fatalf("assets = %#v, want %#v", assets, expected)
	}
}

func TestResolveIDMappingAssetsAll(t *testing.T) {
	for _, values := range [][]string{nil, []string{"all"}} {
		assets, err := resolveIDMappingAssets(values)
		if err != nil {
			t.Fatalf("resolveIDMappingAssets(%#v) returned error: %v", values, err)
		}
		expected := []string{"dat", "selected"}
		if !reflect.DeepEqual(assets, expected) {
			t.Fatalf("assets = %#v, want %#v", assets, expected)
		}
	}
}

func TestResolveIDMappingAssetsRejectsUnknown(t *testing.T) {
	_, err := resolveIDMappingAssets([]string{"by_organism"})
	if err == nil {
		t.Fatal("resolveIDMappingAssets returned nil error")
	}
	if !strings.Contains(err.Error(), "supported") {
		t.Fatalf("error = %q", err.Error())
	}
}

func TestBuildIDMappingStaticAssets(t *testing.T) {
	assets := buildIDMappingStaticAssets("https://example.test/current_release/", []string{"selected"})
	expected := []staticasset.Asset{{
		Name: "selected",
		Path: "raw/idmapping_selected.tab.gz",
		URL:  "https://example.test/current_release/knowledgebase/idmapping/idmapping_selected.tab.gz",
	}}
	if !reflect.DeepEqual(assets, expected) {
		t.Fatalf("assets = %#v, want %#v", assets, expected)
	}
}

func TestResolveIDMappingFetchVersionTokenCurrent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/relnotes.txt" {
			t.Fatalf("path = %s", request.URL.Path)
		}
		_, _ = writer.Write([]byte("UniProt Release 2026_01\n"))
	}))
	defer server.Close()

	versionToken, err := resolveIDMappingFetchVersionToken(server.Client(), "", server.URL)
	if err != nil {
		t.Fatalf("resolveIDMappingFetchVersionToken returned error: %v", err)
	}
	if versionToken != "2026_01" {
		t.Fatalf("versionToken = %q, want 2026_01", versionToken)
	}
}

func TestNormalizeIDMappingFixedVersionTokenRejectsCurrent(t *testing.T) {
	_, err := normalizeIDMappingFixedVersionToken("current")
	if err == nil {
		t.Fatal("normalizeIDMappingFixedVersionToken returned nil error")
	}
}

func TestRunFetchIDMappingRequiresLargeDownloadFlag(t *testing.T) {
	cfg := createDefaultIDMappingConfig()
	cfg.DirOut = t.TempDir()
	cfg.assetNames = []string{"selected"}
	err := runFetchIDMapping(&cfg)
	if err == nil {
		t.Fatal("runFetchIDMapping returned nil error")
	}
	if !strings.Contains(err.Error(), "should_allow_large_assets") {
		t.Fatalf("error = %q", err.Error())
	}
	if _, statErr := os.Stat(filepath.Join(cfg.DirOut, "idmapping")); !os.IsNotExist(statErr) {
		t.Fatalf("idmapping directory exists or stat failed unexpectedly: %v", statErr)
	}
}

func TestRunFetchIDMappingDownloadsAndReuses(t *testing.T) {
	countGetSelected := 0
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/relnotes.txt":
			_, _ = writer.Write([]byte("UniProt Release 2026_01\n"))
		case "/knowledgebase/idmapping/idmapping_selected.tab.gz":
			countGetSelected++
			_, _ = writer.Write([]byte("selected"))
		default:
			t.Fatalf("path = %s", request.URL.Path)
		}
	}))
	defer server.Close()

	cfg := createDefaultIDMappingConfig()
	cfg.DirOut = t.TempDir()
	cfg.RetryMax = 1
	cfg.WorkersMax = 1
	cfg.assetNames = []string{"selected"}
	cfg.baseURLCurrentRelease = server.URL
	cfg.shouldAllowLargeAssets = true
	if err := runFetchIDMapping(&cfg); err != nil {
		t.Fatalf("runFetchIDMapping first run returned error: %v", err)
	}
	if err := runFetchIDMapping(&cfg); err != nil {
		t.Fatalf("runFetchIDMapping second run returned error: %v", err)
	}
	if countGetSelected != 1 {
		t.Fatalf("countGetSelected = %d, want 1", countGetSelected)
	}

	fileOut := filepath.Join(cfg.DirOut, "idmapping", "2026_01", "raw", "idmapping_selected.tab.gz")
	if _, err := os.Stat(fileOut); err != nil {
		t.Fatalf("fileOut missing: %v", err)
	}
	manifest, ok, err := staticasset.ReadManifest(filepath.Join(cfg.DirOut, "idmapping", "2026_01", "manifest.lock"))
	if err != nil {
		t.Fatalf("ReadManifest returned error: %v", err)
	}
	if !ok {
		t.Fatal("manifest was not written")
	}
	if manifest.Database != "uniprot" || manifest.Asset != "idmapping" || manifest.Source != "ftp" {
		t.Fatalf("manifest identity = %#v", manifest)
	}
	if manifest.Version != "2026_01" || manifest.VersionToken != "2026_01" {
		t.Fatalf("manifest version = %#v", manifest)
	}
	if len(manifest.Files) != 1 || manifest.Files[0].Path != "raw/idmapping_selected.tab.gz" {
		t.Fatalf("manifest files = %#v", manifest.Files)
	}
}

func TestRunFetchIDMappingFailsWhenCurrentVersionCannotResolve(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/relnotes.txt" {
			http.Error(writer, "missing", http.StatusNotFound)
			return
		}
		_, _ = writer.Write([]byte("selected"))
	}))
	defer server.Close()

	cfg := createDefaultIDMappingConfig()
	cfg.DirOut = t.TempDir()
	cfg.RetryMax = 1
	cfg.WorkersMax = 1
	cfg.assetNames = []string{"selected"}
	cfg.baseURLCurrentRelease = server.URL
	cfg.shouldAllowLargeAssets = true
	err := runFetchIDMapping(&cfg)
	if err == nil {
		t.Fatal("runFetchIDMapping returned nil error")
	}
	if _, statErr := os.Stat(filepath.Join(cfg.DirOut, "idmapping")); !os.IsNotExist(statErr) {
		t.Fatalf("idmapping directory exists or stat failed unexpectedly: %v", statErr)
	}
}

func TestRunLockIDMappingRejectsCurrentVersion(t *testing.T) {
	cfg := idMappingLockConfig{}
	cfg.DirOut = t.TempDir()
	cfg.VersionToken = "current"
	err := runLockIDMapping(&cfg)
	if err == nil {
		t.Fatal("runLockIDMapping returned nil error")
	}
}

func TestRunSyncIDMappingRejectsCurrentVersion(t *testing.T) {
	cfg := idMappingSyncConfig{}
	cfg.DirOut = t.TempDir()
	cfg.VersionToken = "current"
	cfg.RuleExisting = "skip"
	cfg.RetryMax = 1
	cfg.WorkersMax = 1
	err := runSyncIDMapping(&cfg)
	if err == nil {
		t.Fatal("runSyncIDMapping returned nil error")
	}
}
