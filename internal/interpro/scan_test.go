package interpro

import (
	"biofetch/internal/shared/cliopt"
	"biofetch/internal/shared/staticasset"
	"crypto/md5"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
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

func TestScanFetchChecksumMismatchRemovesPartAndLeavesPartialManifest(t *testing.T) {
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
	if _, err := os.Stat(filepath.Join(snapshot, "raw", archiveName+".part")); !os.IsNotExist(err) {
		t.Fatalf("archive part exists or stat failed: %v", err)
	}
	manifest, ok, err := staticasset.ReadManifest(filepath.Join(snapshot, "manifest.lock"))
	if err != nil || !ok || len(manifest.Files) != 1 || manifest.Files[0].Asset != "archive.md5" {
		t.Fatalf("manifest = %#v, ok = %v, err = %v", manifest, ok, err)
	}
	restore := scanRestoreConfig{}
	restore.RetryMax, restore.WorkersMax, restore.RuleExisting = 1, 1, "skip"
	if err := runRestoreScan(&restore, snapshot); err == nil ||
		!strings.Contains(err.Error(), "must contain archive and archive.md5") {
		t.Fatalf("partial manifest restore error = %v", err)
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
	snapshot := filepath.Join(cfg.DirOut, "scan", scanTestVersion)
	if _, err := os.Stat(filepath.Join(snapshot, "raw", archiveName+".md5")); !os.IsNotExist(err) {
		t.Fatalf("malformed checksum exists or stat failed: %v", err)
	}
	manifest, ok, err := staticasset.ReadManifest(filepath.Join(snapshot, "manifest.lock"))
	if err != nil || (ok && len(manifest.Files) != 0) {
		t.Fatalf("manifest = %#v, ok = %v, err = %v", manifest, ok, err)
	}
}

func TestScanFetchRecoversWhenMalformedChecksumIsCorrected(t *testing.T) {
	archive := []byte("archive fixture")
	archiveName := scanArchiveName(scanTestVersion)
	checksumRequests := 0
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/" + scanTestVersion + "/" + archiveName + ".md5":
			if request.Method == http.MethodHead {
				return
			}
			checksumRequests++
			if checksumRequests == 1 {
				_, _ = writer.Write([]byte("malformed\n"))
			} else {
				_, _ = writer.Write([]byte(md5Line(archive, archiveName)))
			}
		case "/" + scanTestVersion + "/" + archiveName:
			_, _ = writer.Write(archive)
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()
	setScanBaseURL(t, server.URL)

	cfg := scanFetchConfig(t.TempDir())
	if err := runFetchScan(&cfg); err == nil {
		t.Fatal("first runFetchScan succeeded")
	}
	if err := runFetchScan(&cfg); err != nil {
		t.Fatalf("second runFetchScan error = %v", err)
	}
	if checksumRequests != 2 {
		t.Fatalf("checksum requests = %d, want 2", checksumRequests)
	}
}

func TestScanFetchOverwriteReusesValidatedChecksum(t *testing.T) {
	archive := []byte("archive fixture")
	archiveName := scanArchiveName(scanTestVersion)
	checksumRequests := 0
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/" + scanTestVersion + "/" + archiveName + ".md5":
			if request.Method == http.MethodHead {
				return
			}
			checksumRequests++
			if checksumRequests > 1 {
				_, _ = writer.Write([]byte("changed after validation\n"))
				return
			}
			_, _ = writer.Write([]byte(md5Line(archive, archiveName)))
		case "/" + scanTestVersion + "/" + archiveName:
			_, _ = writer.Write(archive)
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()
	setScanBaseURL(t, server.URL)

	cfg := scanFetchConfig(t.TempDir())
	cfg.RuleExisting = "overwrite"
	if err := runFetchScan(&cfg); err != nil {
		t.Fatalf("runFetchScan error = %v", err)
	}
	if checksumRequests != 1 {
		t.Fatalf("checksum requests = %d, want 1", checksumRequests)
	}
	data, err := os.ReadFile(filepath.Join(cfg.DirOut, "scan", scanTestVersion, "raw", archiveName+".md5"))
	if err != nil || string(data) != md5Line(archive, archiveName) {
		t.Fatalf("checksum = %q, err = %v", data, err)
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

func TestScanLockRequiresArchiveBeforeReplacingManifest(t *testing.T) {
	archiveName := scanArchiveName(scanTestVersion)
	snapshot := filepath.Join(t.TempDir(), "scan", scanTestVersion)
	rawDir := filepath.Join(snapshot, "raw")
	if err := os.MkdirAll(rawDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(rawDir, archiveName+".md5"), []byte(md5Line(nil, archiveName)), 0o644); err != nil {
		t.Fatal(err)
	}
	manifestPath := filepath.Join(snapshot, "manifest.lock")
	originalManifest := []byte("existing manifest must remain unchanged\n")
	if err := os.WriteFile(manifestPath, originalManifest, 0o644); err != nil {
		t.Fatal(err)
	}

	err := runLockScan(&scanLockConfig{
		DirSnapshotConfig: cliopt.DirSnapshotConfig{DirSnapshot: snapshot},
		workersMax:        1,
	})
	if err == nil || !strings.Contains(err.Error(), "InterProScan archive is required") {
		t.Fatalf("runLockScan error = %v", err)
	}
	data, readErr := os.ReadFile(manifestPath)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(data) != string(originalManifest) {
		t.Fatalf("manifest was replaced: %q", data)
	}
}

func TestScanFreshLockPublishesSourceURLsAndRestoresFromManifest(t *testing.T) {
	archive := []byte("official archive fixture")
	archiveName := scanArchiveName(scanTestVersion)
	requests := 0
	server := newScanServer(t, archive, md5Line(archive, archiveName), func(request *http.Request) {
		requests++
	})
	defer server.Close()
	setScanBaseURL(t, server.URL)

	snapshot := filepath.Join(t.TempDir(), "scan", scanTestVersion)
	rawDir := filepath.Join(snapshot, "raw")
	if err := os.MkdirAll(rawDir, 0o755); err != nil {
		t.Fatal(err)
	}
	checksum := []byte(md5Line(archive, archiveName))
	if err := os.WriteFile(filepath.Join(rawDir, archiveName), archive, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(rawDir, archiveName+".md5"), checksum, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(rawDir, archiveName+".part.parts"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(rawDir, archiveName+".part.parts", "state.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}

	archiveOpens := 0
	previousOpen := openScanArchive
	openScanArchive = func(path string) (*os.File, error) {
		archiveOpens++
		return os.Open(path)
	}
	t.Cleanup(func() { openScanArchive = previousOpen })
	if err := runLockScan(&scanLockConfig{
		DirSnapshotConfig: cliopt.DirSnapshotConfig{DirSnapshot: snapshot},
		workersMax:        2,
	}); err != nil {
		t.Fatalf("runLockScan error = %v", err)
	}
	if requests != 0 {
		t.Fatalf("lock made %d network requests, want 0", requests)
	}
	if archiveOpens != 1 {
		t.Fatalf("lock opened archive %d times, want 1", archiveOpens)
	}
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
	want := map[string]struct {
		asset  string
		data   []byte
		urlEnd string
	}{
		"raw/" + archiveName:          {asset: "archive", data: archive, urlEnd: "/" + archiveName},
		"raw/" + archiveName + ".md5": {asset: "archive.md5", data: checksum, urlEnd: "/" + archiveName + ".md5"},
	}
	for _, record := range manifest.Files {
		expected, exists := want[record.Path]
		if !exists {
			t.Fatalf("unexpected record = %#v", record)
		}
		sum := sha256.Sum256(expected.data)
		if record.Asset != expected.asset || record.URL != server.URL+"/"+scanTestVersion+expected.urlEnd ||
			record.SHA256 != hex.EncodeToString(sum[:]) || record.Bytes != int64(len(expected.data)) {
			t.Fatalf("record = %#v", record)
		}
	}

	if err := os.Remove(filepath.Join(rawDir, archiveName)); err != nil {
		t.Fatal(err)
	}
	scanBaseURL = "http://127.0.0.1:1/not-the-manifest-source"
	restore := scanRestoreConfig{}
	restore.RetryMax, restore.WorkersMax, restore.RuleExisting = 1, 1, "skip"
	if err := runRestoreScan(&restore, snapshot); err != nil {
		t.Fatalf("runRestoreScan error = %v", err)
	}
	if requests == 0 {
		t.Fatal("restore did not use the manifest URL")
	}
	data, err := os.ReadFile(filepath.Join(rawDir, archiveName))
	if err != nil || string(data) != string(archive) {
		t.Fatalf("restored archive = %q, err = %v", data, err)
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
			writer.Header().Set("Content-Length", fmt.Sprint(len(archive)))
			if request.Method == http.MethodHead {
				return
			}
			start := 0
			if request.Header.Get("Range") != "" {
				if request.Header.Get("Range") == "bytes=7-" {
					start = 7
					writer.Header().Set("Content-Length", fmt.Sprint(len(archive)-start))
					writer.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, len(archive)-1, len(archive)))
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
