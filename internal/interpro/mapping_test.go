package interpro

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

func TestParseReleaseNotes(t *testing.T) {
	versionToken, err := parseReleaseNotes([]byte("Release Notes\n\nRelease 108.0, 29th January 2026\n"))
	if err != nil {
		t.Fatalf("parseReleaseNotes returned error: %v", err)
	}
	if versionToken != "108.0" {
		t.Fatalf("versionToken = %q, want 108.0", versionToken)
	}
}

func TestParseReleaseNotesRejectsMalformed(t *testing.T) {
	_, err := parseReleaseNotes([]byte("release unavailable"))
	if err == nil {
		t.Fatal("parseReleaseNotes returned nil error")
	}
}

func TestResolveMappingAssets(t *testing.T) {
	assets, err := resolveMappingAssets([]string{"protein2ipr,entries"})
	if err != nil {
		t.Fatalf("resolveMappingAssets returned error: %v", err)
	}
	expected := []string{"entries", "protein2ipr"}
	if !reflect.DeepEqual(assets, expected) {
		t.Fatalf("assets = %#v, want %#v", assets, expected)
	}
}

func TestResolveMappingAssetsRejectsUnknown(t *testing.T) {
	_, err := resolveMappingAssets([]string{"match_complete"})
	if err == nil {
		t.Fatal("resolveMappingAssets returned nil error")
	}
	if !strings.Contains(err.Error(), "supported") {
		t.Fatalf("error = %q", err.Error())
	}
}

func TestBuildMappingStaticAssets(t *testing.T) {
	assets := buildMappingStaticAssets("https://example.test/current_release/", []string{"entries"})
	expected := []staticasset.Asset{{
		Name: "entries",
		Path: "raw/interpro.xml.gz",
		URL:  "https://example.test/current_release/interpro.xml.gz",
	}}
	if !reflect.DeepEqual(assets, expected) {
		t.Fatalf("assets = %#v, want %#v", assets, expected)
	}
}

func TestResolveMappingFetchVersionTokenCurrent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/release_notes.txt" {
			t.Fatalf("path = %s", request.URL.Path)
		}
		_, _ = writer.Write([]byte("Release 108.0, 29th January 2026\n"))
	}))
	defer server.Close()

	versionToken, err := resolveMappingFetchVersionToken(server.Client(), "", server.URL)
	if err != nil {
		t.Fatalf("resolveMappingFetchVersionToken returned error: %v", err)
	}
	if versionToken != "108.0" {
		t.Fatalf("versionToken = %q, want 108.0", versionToken)
	}
}

func TestRunFetchMappingRequiresLargeDownloadFlag(t *testing.T) {
	cfg := createDefaultMappingConfig()
	cfg.DirOut = t.TempDir()
	cfg.assetNames = []string{"protein2ipr"}
	err := runFetchMapping(&cfg)
	if err == nil {
		t.Fatal("runFetchMapping returned nil error")
	}
	if !strings.Contains(err.Error(), "should_allow_large_download") {
		t.Fatalf("error = %q", err.Error())
	}
	if _, statErr := os.Stat(filepath.Join(cfg.DirOut, "mapping")); !os.IsNotExist(statErr) {
		t.Fatalf("mapping directory exists or stat failed unexpectedly: %v", statErr)
	}
}

func TestRunFetchMappingDownloadsAndReuses(t *testing.T) {
	countGetEntries := 0
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/release_notes.txt":
			_, _ = writer.Write([]byte("Release 108.0, 29th January 2026\n"))
		case "/interpro.xml.gz":
			countGetEntries++
			_, _ = writer.Write([]byte("<interprodb></interprodb>"))
		default:
			t.Fatalf("path = %s", request.URL.Path)
		}
	}))
	defer server.Close()

	cfg := createDefaultMappingConfig()
	cfg.DirOut = t.TempDir()
	cfg.RetryMax = 1
	cfg.WorkersMax = 1
	cfg.assetNames = []string{"entries"}
	cfg.baseURLCurrentRelease = server.URL
	if err := runFetchMapping(&cfg); err != nil {
		t.Fatalf("runFetchMapping first run returned error: %v", err)
	}
	if err := runFetchMapping(&cfg); err != nil {
		t.Fatalf("runFetchMapping second run returned error: %v", err)
	}
	if countGetEntries != 1 {
		t.Fatalf("countGetEntries = %d, want 1", countGetEntries)
	}

	fileOut := filepath.Join(cfg.DirOut, "mapping", "108.0", "raw", "interpro.xml.gz")
	if _, err := os.Stat(fileOut); err != nil {
		t.Fatalf("fileOut missing: %v", err)
	}
	manifest, ok, err := staticasset.ReadManifest(filepath.Join(cfg.DirOut, "mapping", "108.0", "manifest.lock"))
	if err != nil {
		t.Fatalf("ReadManifest returned error: %v", err)
	}
	if !ok {
		t.Fatal("manifest was not written")
	}
	if manifest.Database != "interpro" || manifest.Asset != "mapping" || manifest.Source != "ftp" {
		t.Fatalf("manifest identity = %#v", manifest)
	}
	if manifest.Version != "108.0" || manifest.VersionToken != "108.0" {
		t.Fatalf("manifest version = %#v", manifest)
	}
}

func TestRunFetchMappingFailsWhenCurrentVersionCannotResolve(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/release_notes.txt" {
			http.Error(writer, "missing", http.StatusNotFound)
			return
		}
		_, _ = writer.Write([]byte("entries"))
	}))
	defer server.Close()

	cfg := createDefaultMappingConfig()
	cfg.DirOut = t.TempDir()
	cfg.RetryMax = 1
	cfg.WorkersMax = 1
	cfg.assetNames = []string{"entries"}
	cfg.baseURLCurrentRelease = server.URL
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
