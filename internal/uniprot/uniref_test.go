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

func TestResolveUniRefAssets(t *testing.T) {
	for _, values := range [][]string{nil, []string{"all"}, []string{"uniref90"}} {
		assets, err := resolveUniRefAssets(values)
		if err != nil {
			t.Fatalf("resolveUniRefAssets(%#v) returned error: %v", values, err)
		}
		expected := []string{"uniref90"}
		if !reflect.DeepEqual(assets, expected) {
			t.Fatalf("assets = %#v, want %#v", assets, expected)
		}
	}
}

func TestResolveUniRefAssetsRejectsUnknown(t *testing.T) {
	_, err := resolveUniRefAssets([]string{"uniref100"})
	if err == nil {
		t.Fatal("resolveUniRefAssets returned nil error")
	}
	if !strings.Contains(err.Error(), "supported") {
		t.Fatalf("error = %q", err.Error())
	}
}

func TestBuildUniRefStaticAssets(t *testing.T) {
	assets := buildUniRefStaticAssets("https://example.test/current_release/", []string{"uniref90"})
	expected := []staticasset.Asset{{
		Name: "uniref90",
		Path: "raw/uniref/uniref90/uniref90.fasta.gz",
		URL:  "https://example.test/current_release/uniref/uniref90/uniref90.fasta.gz",
	}}
	if !reflect.DeepEqual(assets, expected) {
		t.Fatalf("assets = %#v, want %#v", assets, expected)
	}
}

func TestRunFetchUniRefRequiresLargeAssetsFlag(t *testing.T) {
	cfg := createDefaultUniRefConfig()
	cfg.DirOut = t.TempDir()
	err := runFetchUniRef(&cfg)
	if err == nil {
		t.Fatal("runFetchUniRef returned nil error")
	}
	if !strings.Contains(err.Error(), "should_allow_large_assets") {
		t.Fatalf("error = %q", err.Error())
	}
	if _, statErr := os.Stat(filepath.Join(cfg.DirOut, "uniref")); !os.IsNotExist(statErr) {
		t.Fatalf("uniref directory exists or stat failed unexpectedly: %v", statErr)
	}
}

func TestRunFetchUniRefDownloadsAndReuses(t *testing.T) {
	countGetUniRef90 := 0
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/relnotes.txt":
			_, _ = writer.Write([]byte("UniProt Release 2026_01\n"))
		case "/uniref/uniref90/uniref90.fasta.gz":
			countGetUniRef90++
			_, _ = writer.Write([]byte(">UniRef90_P12345 Cluster\nM\n"))
		default:
			t.Fatalf("path = %s", request.URL.Path)
		}
	}))
	defer server.Close()

	cfg := createDefaultUniRefConfig()
	cfg.DirOut = t.TempDir()
	cfg.RetryMax = 1
	cfg.WorkersMax = 1
	cfg.baseURLCurrentRelease = server.URL
	cfg.shouldAllowLargeAssets = true
	if err := runFetchUniRef(&cfg); err != nil {
		t.Fatalf("runFetchUniRef first run returned error: %v", err)
	}
	if err := runFetchUniRef(&cfg); err != nil {
		t.Fatalf("runFetchUniRef second run returned error: %v", err)
	}
	if countGetUniRef90 != 1 {
		t.Fatalf("countGetUniRef90 = %d, want 1", countGetUniRef90)
	}

	fileOut := filepath.Join(cfg.DirOut, "uniref", "2026_01", "raw", "uniref", "uniref90", "uniref90.fasta.gz")
	if _, err := os.Stat(fileOut); err != nil {
		t.Fatalf("fileOut missing: %v", err)
	}
	manifest, ok, err := staticasset.ReadManifest(filepath.Join(cfg.DirOut, "uniref", "2026_01", "manifest.lock"))
	if err != nil {
		t.Fatalf("ReadManifest returned error: %v", err)
	}
	if !ok {
		t.Fatal("manifest was not written")
	}
	if manifest.Database != "uniprot" || manifest.Asset != "uniref" || manifest.Source != "ftp" {
		t.Fatalf("manifest identity = %#v", manifest)
	}
	if manifest.Version != "2026_01" || manifest.VersionToken != "2026_01" {
		t.Fatalf("manifest version = %#v", manifest)
	}
	if len(manifest.Files) != 1 || manifest.Files[0].Path != "raw/uniref/uniref90/uniref90.fasta.gz" {
		t.Fatalf("manifest files = %#v", manifest.Files)
	}
}
