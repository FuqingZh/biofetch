package interpro

import (
	"biofetch/internal/shared/cliopt"
	"biofetch/internal/shared/staticasset"
	"crypto/md5"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const scanTestVersion = "5.77-108.0"

func TestNormalizeScanVersion(t *testing.T) {
	if got, err := normalizeScanVersion(scanTestVersion); err != nil || got != scanTestVersion {
		t.Fatalf("normalizeScanVersion = %q, %v", got, err)
	}
	for _, value := range []string{"", "current", "5.77", "../5.77-108.0", "5.77-108.0/other", " 5.77-108.0", "5.77-108.0?x", "55555.77-108.0"} {
		if _, err := normalizeScanVersion(value); err == nil {
			t.Fatalf("normalizeScanVersion(%q) succeeded", value)
		}
	}
}

func TestBuildScanSourcesUsesFixedEBIHTTPSURLs(t *testing.T) {
	setScanBaseURL(t, defaultScanBaseURL)
	checksum, complete := buildScanSources(scanTestVersion)
	archiveName := scanArchiveName(scanTestVersion)
	wantBase := "https://ftp.ebi.ac.uk/pub/software/unix/iprscan/5/" + scanTestVersion + "/"
	if len(checksum.Assets) != 1 || checksum.Assets[0].URL != wantBase+archiveName+".md5" {
		t.Fatalf("checksum assets = %#v", checksum.Assets)
	}
	if len(complete.Assets) != 2 || complete.Assets[0].URL != wantBase+archiveName ||
		complete.Assets[1].URL != wantBase+archiveName+".md5" {
		t.Fatalf("complete assets = %#v", complete.Assets)
	}
}

func TestScanFetchRejectsUnsafeVersionBeforeSideEffects(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { requests++ }))
	defer server.Close()
	setScanBaseURL(t, server.URL)
	dirOut := filepath.Join(t.TempDir(), "not-created")
	cfg := createDefaultScanConfig()
	cfg.DirOut = dirOut
	cfg.VersionToken = "../unsafe"
	cfg.shouldAllowLargeDownloads = true
	if err := runFetchScan(&cfg); err == nil || !strings.Contains(err.Error(), "version must look like") {
		t.Fatalf("runFetchScan error = %v", err)
	}
	if requests != 0 {
		t.Fatalf("requests = %d, want 0", requests)
	}
	if _, err := os.Stat(dirOut); !os.IsNotExist(err) {
		t.Fatalf("output exists or stat failed: %v", err)
	}
}

func TestScanFetchRequiresLargeDownloadOptInBeforeSideEffects(t *testing.T) {
	dirOut := filepath.Join(t.TempDir(), "not-created")
	cfg := createDefaultScanConfig()
	cfg.DirOut = dirOut
	cfg.VersionToken = scanTestVersion
	if err := runFetchScan(&cfg); err == nil || !strings.Contains(err.Error(), "--allow-large-downloads") {
		t.Fatalf("runFetchScan error = %v", err)
	}
	if _, err := os.Stat(dirOut); !os.IsNotExist(err) {
		t.Fatalf("output exists or stat failed: %v", err)
	}
}

func TestParseScanMD5StrictFilenameBinding(t *testing.T) {
	sum := strings.Repeat("a", 32)
	filename := scanArchiveName(scanTestVersion)
	if got, err := parseScanMD5([]byte(sum+"  "+filename+"\n"), filename); err != nil || got != sum {
		t.Fatalf("parseScanMD5 = %q, %v", got, err)
	}
	for _, data := range []string{
		"", "not-a-checksum  " + filename, sum + "  wrong.tar.gz\n",
		sum + "  ../" + filename + "\n", sum + "  " + filename + "\nextra\n",
	} {
		if _, err := parseScanMD5([]byte(data), filename); err == nil {
			t.Fatalf("parseScanMD5(%q) succeeded", data)
		}
	}
}

func TestScanFetchChecksumMismatchLeavesPartAndPartialManifest(t *testing.T) {
	archive := []byte("archive fixture")
	archiveName := scanArchiveName(scanTestVersion)
	server := newScanServer(t, archive, strings.Repeat("0", 32)+"  "+archiveName+"\n", nil)
	defer server.Close()
	setScanBaseURL(t, server.URL)

	cfg := scanFetchConfig(t.TempDir())
	err := runFetchScan(&cfg)
	if err == nil || !strings.Contains(err.Error(), `asset "archive" failed downloaded-file verification`) {
		t.Fatalf("runFetchScan error = %v", err)
	}
	snapshot := filepath.Join(cfg.DirOut, "scan", scanTestVersion)
	if _, err := os.Stat(filepath.Join(snapshot, "raw", archiveName)); !os.IsNotExist(err) {
		t.Fatalf("final archive exists or stat failed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(snapshot, "raw", archiveName+".part")); err != nil {
		t.Fatalf("archive part not retained: %v", err)
	}
	manifest, ok, err := staticasset.ReadManifest(filepath.Join(snapshot, "manifest.lock"))
	if err != nil || !ok || len(manifest.Files) != 1 || manifest.Files[0].Asset != "archive.md5" {
		t.Fatalf("manifest = %#v, ok = %v, err = %v", manifest, ok, err)
	}
}

func TestScanFetchRejectsMalformedMD5BeforeArchiveRequest(t *testing.T) {
	archiveRequests := 0
	archiveName := scanArchiveName(scanTestVersion)
	server := newScanServer(t, []byte("archive"), "malformed\n", func(request *http.Request) {
		if request.URL.Path == "/"+scanTestVersion+"/"+archiveName {
			archiveRequests++
		}
	})
	defer server.Close()
	setScanBaseURL(t, server.URL)

	cfg := scanFetchConfig(t.TempDir())
	if err := runFetchScan(&cfg); err == nil || !strings.Contains(err.Error(), "malformed InterProScan MD5") {
		t.Fatalf("runFetchScan error = %v", err)
	}
	if archiveRequests != 0 {
		t.Fatalf("archive requests = %d, want 0", archiveRequests)
	}
}

func TestScanFetchManifestIdentityURLsAndReuse(t *testing.T) {
	archive := []byte("archive fixture")
	archiveName := scanArchiveName(scanTestVersion)
	requests := map[string]int{}
	server := newScanServer(t, archive, md5Line(archive, archiveName), func(request *http.Request) {
		if request.Method == http.MethodGet && request.Header.Get("Range") == "" {
			requests[request.URL.Path]++
		}
	})
	defer server.Close()
	setScanBaseURL(t, server.URL)

	cfg := scanFetchConfig(t.TempDir())
	if err := runFetchScan(&cfg); err != nil {
		t.Fatalf("first runFetchScan error = %v", err)
	}
	if err := runFetchScan(&cfg); err != nil {
		t.Fatalf("second runFetchScan error = %v", err)
	}
	snapshot := filepath.Join(cfg.DirOut, "scan", scanTestVersion)
	manifest, ok, err := staticasset.ReadManifest(filepath.Join(snapshot, "manifest.lock"))
	if err != nil || !ok {
		t.Fatalf("ReadManifest ok = %v, err = %v", ok, err)
	}
	if manifest.Database != "interpro" || manifest.Asset != "scan" || manifest.Source != "ftp" ||
		manifest.Version != scanTestVersion || manifest.VersionToken != scanTestVersion {
		t.Fatalf("manifest identity = %#v", manifest)
	}
	if len(manifest.Files) != 2 {
		t.Fatalf("manifest files = %#v", manifest.Files)
	}
	for _, record := range manifest.Files {
		if !strings.HasPrefix(record.Path, "raw/") || !strings.HasPrefix(record.URL, server.URL+"/"+scanTestVersion+"/") ||
			record.SHA256 == "" || record.Bytes <= 0 {
			t.Fatalf("manifest record = %#v", record)
		}
	}
	if requests["/"+scanTestVersion+"/"+archiveName] != 1 || requests["/"+scanTestVersion+"/"+archiveName+".md5"] != 1 {
		t.Fatalf("GET requests = %#v", requests)
	}
}

func TestScanFetchResumesInterruptedArchive(t *testing.T) {
	archive := []byte("archive fixture")
	archiveName := scanArchiveName(scanTestVersion)
	rangeSeen := ""
	server := newScanServer(t, archive, md5Line(archive, archiveName), func(request *http.Request) {
		if request.URL.Path == "/"+scanTestVersion+"/"+archiveName {
			rangeSeen = request.Header.Get("Range")
		}
	})
	defer server.Close()
	setScanBaseURL(t, server.URL)

	cfg := scanFetchConfig(t.TempDir())
	part := filepath.Join(cfg.DirOut, "scan", scanTestVersion, "raw", archiveName+".part")
	if err := os.MkdirAll(filepath.Dir(part), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(part, archive[:7], 0o644); err != nil {
		t.Fatal(err)
	}
	if err := runFetchScan(&cfg); err != nil {
		t.Fatalf("runFetchScan error = %v", err)
	}
	if rangeSeen != "bytes=7-" {
		t.Fatalf("Range = %q, want bytes=7-", rangeSeen)
	}
}

func TestScanLockVerifiesMD5AndRestoreUsesManifestURLs(t *testing.T) {
	archive := []byte("archive fixture")
	archiveName := scanArchiveName(scanTestVersion)
	server := newScanServer(t, archive, md5Line(archive, archiveName), nil)
	defer server.Close()
	setScanBaseURL(t, server.URL)

	cfg := scanFetchConfig(t.TempDir())
	if err := runFetchScan(&cfg); err != nil {
		t.Fatalf("runFetchScan error = %v", err)
	}
	snapshot := filepath.Join(cfg.DirOut, "scan", scanTestVersion)
	if err := runLockScan(&scanLockConfig{DirSnapshotConfig: cliopt.DirSnapshotConfig{DirSnapshot: snapshot}, workersMax: 1}); err != nil {
		t.Fatalf("runLockScan error = %v", err)
	}
	if err := os.Remove(filepath.Join(snapshot, "raw", archiveName)); err != nil {
		t.Fatal(err)
	}
	restore := scanRestoreConfig{}
	restore.RetryMax, restore.WorkersMax, restore.RuleExisting = 1, 1, "skip"
	if err := runRestoreScan(&restore, snapshot); err != nil {
		t.Fatalf("runRestoreScan error = %v", err)
	}
	data, err := os.ReadFile(filepath.Join(snapshot, "raw", archiveName))
	if err != nil || string(data) != string(archive) {
		t.Fatalf("restored archive = %q, err = %v", data, err)
	}

	if err := os.WriteFile(filepath.Join(snapshot, "raw", archiveName), []byte("bad"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := runLockScan(&scanLockConfig{DirSnapshotConfig: cliopt.DirSnapshotConfig{DirSnapshot: snapshot}, workersMax: 1}); err == nil ||
		!strings.Contains(err.Error(), "failed MD5 verification") {
		t.Fatalf("mismatched runLockScan error = %v", err)
	}
}

func TestScanDryRunHasNoNetworkOrFilesystemWrites(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { requests++ }))
	defer server.Close()
	setScanBaseURL(t, server.URL)
	cfg := scanFetchConfig(filepath.Join(t.TempDir(), "not-created"))
	cfg.ShouldDryRun = true
	if err := runFetchScan(&cfg); err != nil {
		t.Fatalf("runFetchScan error = %v", err)
	}
	if requests != 0 {
		t.Fatalf("requests = %d, want 0", requests)
	}
	if _, err := os.Stat(cfg.DirOut); !os.IsNotExist(err) {
		t.Fatalf("output exists or stat failed: %v", err)
	}
}

func scanFetchConfig(dirOut string) scanConfig {
	cfg := createDefaultScanConfig()
	cfg.DirOut = dirOut
	cfg.VersionToken = scanTestVersion
	cfg.shouldAllowLargeDownloads = true
	cfg.ShouldDisableProgress = true
	return cfg
}

func md5Line(data []byte, filename string) string {
	sum := md5.Sum(data)
	return hex.EncodeToString(sum[:]) + "  " + filename + "\n"
}

func setScanBaseURL(t *testing.T, value string) {
	t.Helper()
	previous := scanBaseURL
	scanBaseURL = value
	t.Cleanup(func() { scanBaseURL = previous })
}

func newScanServer(t *testing.T, archive []byte, checksum string, observe func(*http.Request)) *httptest.Server {
	t.Helper()
	archiveName := scanArchiveName(scanTestVersion)
	return httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if observe != nil {
			observe(request)
		}
		switch request.URL.Path {
		case "/" + scanTestVersion + "/" + archiveName + ".md5":
			_, _ = writer.Write([]byte(checksum))
		case "/" + scanTestVersion + "/" + archiveName:
			start := 0
			if request.Header.Get("Range") != "" {
				if request.Header.Get("Range") == "bytes=7-" {
					start = 7
					writer.WriteHeader(http.StatusPartialContent)
				} else {
					t.Fatalf("unexpected Range: %s", request.Header.Get("Range"))
				}
			}
			_, _ = writer.Write(archive[start:])
		default:
			http.NotFound(writer, request)
		}
	}))
}
