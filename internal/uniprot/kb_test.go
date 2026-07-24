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

func TestResolveKBAssets(t *testing.T) {
	assets, err := resolveKBAssets([]string{"trembl,sprot", "varsplic"})
	if err != nil {
		t.Fatalf("resolveKBAssets returned error: %v", err)
	}
	expected := []string{"sprot", "trembl", "varsplic"}
	if !reflect.DeepEqual(assets, expected) {
		t.Fatalf("assets = %#v, want %#v", assets, expected)
	}
}

func TestResolveKBAssetsAll(t *testing.T) {
	for _, values := range [][]string{nil, []string{"all"}} {
		assets, err := resolveKBAssets(values)
		if err != nil {
			t.Fatalf("resolveKBAssets(%#v) returned error: %v", values, err)
		}
		expected := []string{"sprot", "sprot_dat", "trembl", "trembl_dat", "varsplic"}
		if !reflect.DeepEqual(assets, expected) {
			t.Fatalf("assets = %#v, want %#v", assets, expected)
		}
	}
}

func TestResolveKBAssetsRejectsUnknown(t *testing.T) {
	_, err := resolveKBAssets([]string{"uniref90"})
	if err == nil {
		t.Fatal("resolveKBAssets returned nil error")
	}
	if !strings.Contains(err.Error(), "supported") {
		t.Fatalf("error = %q", err.Error())
	}
}

func TestBuildKBStaticAssets(t *testing.T) {
	assets := buildKBStaticAssets("https://example.test/current_release/", []string{"sprot", "sprot_dat", "trembl_dat"})
	expected := []staticasset.Asset{
		{
			Name: "sprot",
			Path: "raw/knowledgebase/complete/uniprot_sprot.fasta.gz",
			URL:  "https://example.test/current_release/knowledgebase/complete/uniprot_sprot.fasta.gz",
		},
		{
			Name: "sprot_dat",
			Path: "raw/knowledgebase/complete/uniprot_sprot.dat.gz",
			URL:  "https://example.test/current_release/knowledgebase/complete/uniprot_sprot.dat.gz",
		},
		{
			Name: "trembl_dat",
			Path: "raw/knowledgebase/complete/uniprot_trembl.dat.gz",
			URL:  "https://example.test/current_release/knowledgebase/complete/uniprot_trembl.dat.gz",
		},
	}
	if !reflect.DeepEqual(assets, expected) {
		t.Fatalf("assets = %#v, want %#v", assets, expected)
	}
}

func TestRunFetchKBRequiresLargeDownloadFlagForTrEMBL(t *testing.T) {
	cfg := createDefaultKBConfig()
	cfg.DirOut = t.TempDir()
	cfg.assetNames = []string{"trembl"}
	err := runFetchKB(&cfg)
	if err == nil {
		t.Fatal("runFetchKB returned nil error")
	}
	if !strings.Contains(err.Error(), "allow-large-downloads") {
		t.Fatalf("error = %q", err.Error())
	}
	if _, statErr := os.Stat(filepath.Join(cfg.DirOut, "kb")); !os.IsNotExist(statErr) {
		t.Fatalf("kb directory exists or stat failed unexpectedly: %v", statErr)
	}
}

func TestRunFetchKBRequiresLargeDownloadFlagForTrEMBLDAT(t *testing.T) {
	cfg := createDefaultKBConfig()
	cfg.DirOut = t.TempDir()
	cfg.assetNames = []string{"trembl_dat"}
	err := runFetchKB(&cfg)
	if err == nil {
		t.Fatal("runFetchKB returned nil error")
	}
	if !strings.Contains(err.Error(), "allow-large-downloads") {
		t.Fatalf("error = %q", err.Error())
	}
	if _, statErr := os.Stat(filepath.Join(cfg.DirOut, "kb")); !os.IsNotExist(statErr) {
		t.Fatalf("kb directory exists or stat failed unexpectedly: %v", statErr)
	}
}

func TestRunFetchKBDownloadsAndReuses(t *testing.T) {
	countGetSProt := 0
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/relnotes.txt":
			_, _ = writer.Write([]byte("UniProt Release 2026_01\n"))
		case "/knowledgebase/complete/uniprot_sprot.fasta.gz":
			if request.Method == http.MethodGet {
				countGetSProt++
			}
			_, _ = writer.Write([]byte(">sp|P12345|TEST\nM\n"))
		default:
			t.Fatalf("path = %s", request.URL.Path)
		}
	}))
	defer server.Close()

	cfg := createDefaultKBConfig()
	cfg.DirOut = t.TempDir()
	cfg.RetryMax = 1
	cfg.WorkersMax = 1
	cfg.assetNames = []string{"sprot"}
	cfg.baseURLCurrentRelease = server.URL
	if err := runFetchKB(&cfg); err != nil {
		t.Fatalf("runFetchKB first run returned error: %v", err)
	}
	if err := runFetchKB(&cfg); err != nil {
		t.Fatalf("runFetchKB second run returned error: %v", err)
	}
	if countGetSProt != 1 {
		t.Fatalf("countGetSProt = %d, want 1", countGetSProt)
	}

	fileOut := filepath.Join(cfg.DirOut, "kb", "2026_01", "raw", "knowledgebase", "complete", "uniprot_sprot.fasta.gz")
	if _, err := os.Stat(fileOut); err != nil {
		t.Fatalf("fileOut missing: %v", err)
	}
	manifest, ok, err := staticasset.ReadManifest(filepath.Join(cfg.DirOut, "kb", "2026_01", "manifest.lock"))
	if err != nil {
		t.Fatalf("ReadManifest returned error: %v", err)
	}
	if !ok {
		t.Fatal("manifest was not written")
	}
	if manifest.Database != "uniprot" || manifest.Asset != "kb" || manifest.Source != "ftp" {
		t.Fatalf("manifest identity = %#v", manifest)
	}
	if manifest.Version != "2026_01" || manifest.VersionToken != "2026_01" {
		t.Fatalf("manifest version = %#v", manifest)
	}
	if len(manifest.Files) != 1 || manifest.Files[0].Path != "raw/knowledgebase/complete/uniprot_sprot.fasta.gz" {
		t.Fatalf("manifest files = %#v", manifest.Files)
	}
}

func TestRunFetchKBDownloadsDAT(t *testing.T) {
	countGetDAT := 0
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/relnotes.txt":
			_, _ = writer.Write([]byte("UniProt Release 2026_01\n"))
		case "/knowledgebase/complete/uniprot_sprot.dat.gz":
			if request.Method == http.MethodGet {
				countGetDAT++
			}
			_, _ = writer.Write([]byte("ID   TEST_HUMAN\n"))
		default:
			t.Fatalf("path = %s", request.URL.Path)
		}
	}))
	defer server.Close()

	cfg := createDefaultKBConfig()
	cfg.DirOut = t.TempDir()
	cfg.RetryMax = 1
	cfg.WorkersMax = 1
	cfg.assetNames = []string{"sprot_dat"}
	cfg.baseURLCurrentRelease = server.URL
	if err := runFetchKB(&cfg); err != nil {
		t.Fatalf("runFetchKB returned error: %v", err)
	}
	if countGetDAT != 1 {
		t.Fatalf("countGetDAT = %d, want 1", countGetDAT)
	}
	fileOut := filepath.Join(cfg.DirOut, "kb", "2026_01", "raw", "knowledgebase", "complete", "uniprot_sprot.dat.gz")
	if _, err := os.Stat(fileOut); err != nil {
		t.Fatalf("fileOut missing: %v", err)
	}
	manifest, ok, err := staticasset.ReadManifest(filepath.Join(cfg.DirOut, "kb", "2026_01", "manifest.lock"))
	if err != nil {
		t.Fatalf("ReadManifest returned error: %v", err)
	}
	if !ok {
		t.Fatal("manifest was not written")
	}
	if len(manifest.Files) != 1 || manifest.Files[0].Path != "raw/knowledgebase/complete/uniprot_sprot.dat.gz" {
		t.Fatalf("manifest files = %#v", manifest.Files)
	}
}

func TestRunFetchKBFailsWhenCurrentVersionCannotResolve(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/relnotes.txt" {
			http.Error(writer, "missing", http.StatusNotFound)
			return
		}
		_, _ = writer.Write([]byte("sprot"))
	}))
	defer server.Close()

	cfg := createDefaultKBConfig()
	cfg.DirOut = t.TempDir()
	cfg.RetryMax = 1
	cfg.WorkersMax = 1
	cfg.assetNames = []string{"sprot"}
	cfg.baseURLCurrentRelease = server.URL
	err := runFetchKB(&cfg)
	if err == nil {
		t.Fatal("runFetchKB returned nil error")
	}
	if _, statErr := os.Stat(filepath.Join(cfg.DirOut, "kb")); !os.IsNotExist(statErr) {
		t.Fatalf("kb directory exists or stat failed unexpectedly: %v", statErr)
	}
}

func TestRunLockKBRejectsCurrentVersion(t *testing.T) {
	cfg := kbLockConfig{}
	cfg.DirSnapshot = filepath.Join(t.TempDir(), "current")
	err := runLockKB(&cfg)
	if err == nil {
		t.Fatal("runLockKB returned nil error")
	}
}

func TestRunSyncKBRejectsCurrentVersion(t *testing.T) {
	cfg := kbRestoreConfig{}
	cfg.DirOut = t.TempDir()
	cfg.VersionToken = "current"
	cfg.RuleExisting = "skip"
	cfg.RetryMax = 1
	cfg.WorkersMax = 1
	err := runRestoreKB(&cfg)
	if err == nil {
		t.Fatal("runRestoreKB returned nil error")
	}
}
