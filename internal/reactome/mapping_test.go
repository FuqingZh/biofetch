package reactome

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/FuqingZh/biofetch/internal/shared/staticasset"
)

type reactomeRoundTripFunc func(*http.Request) (*http.Response, error)

func (fn reactomeRoundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return fn(request)
}

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

	versionToken, err := resolveMappingFetchVersionToken(server.Client(), "", 1, 0)
	if err != nil {
		t.Fatalf("resolveMappingFetchVersionToken returned error: %v", err)
	}
	if versionToken != "v96" {
		t.Fatalf("versionToken = %q, want v96", versionToken)
	}
}

func TestResolveMappingCurrentVersionRetries521WithRetryAfter(t *testing.T) {
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		attempts++
		if attempts == 1 {
			writer.Header().Set("Retry-After", "120")
			writer.WriteHeader(521)
			return
		}
		_, _ = writer.Write([]byte("96"))
	}))
	defer server.Close()
	originalURL, originalSleep := mappingCurrentVersionURL, reactomeSleep
	t.Cleanup(func() { mappingCurrentVersionURL, reactomeSleep = originalURL, originalSleep })
	mappingCurrentVersionURL = server.URL
	var waits []time.Duration
	reactomeSleep = func(wait time.Duration) { waits = append(waits, wait) }
	version, err := resolveMappingFetchVersionToken(server.Client(), "", 2, time.Second)
	if err != nil || version != "v96" || attempts != 2 {
		t.Fatalf("version=%q attempts=%d err=%v", version, attempts, err)
	}
	if !reflect.DeepEqual(waits, []time.Duration{2 * time.Minute}) {
		t.Fatalf("waits = %#v", waits)
	}
}

func TestResolveMappingCurrentVersionExhaustionReportsIdentity(t *testing.T) {
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		attempts++
		writer.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()
	originalURL, originalSleep := mappingCurrentVersionURL, reactomeSleep
	t.Cleanup(func() { mappingCurrentVersionURL, reactomeSleep = originalURL, originalSleep })
	mappingCurrentVersionURL = server.URL
	var waits []time.Duration
	reactomeSleep = func(wait time.Duration) { waits = append(waits, wait) }
	_, err := resolveMappingFetchVersionToken(server.Client(), "", 3, time.Hour)
	if err == nil || !strings.Contains(err.Error(), server.URL) ||
		!strings.Contains(err.Error(), "503") || !strings.Contains(err.Error(), "attempts=3") {
		t.Fatalf("error = %v", err)
	}
	if attempts != 3 || !reflect.DeepEqual(waits, []time.Duration{time.Hour, time.Hour}) {
		t.Fatalf("attempts=%d waits=%#v", attempts, waits)
	}
}

func TestResolveMappingCurrentVersionRetries429AndDoesNotLeakRetryAfter(t *testing.T) {
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		attempts++
		switch attempts {
		case 1:
			writer.Header().Set("Retry-After", "120")
			writer.WriteHeader(http.StatusTooManyRequests)
		case 2:
			writer.WriteHeader(http.StatusServiceUnavailable)
		default:
			_, _ = writer.Write([]byte("96"))
		}
	}))
	defer server.Close()
	originalURL, originalSleep := mappingCurrentVersionURL, reactomeSleep
	t.Cleanup(func() { mappingCurrentVersionURL, reactomeSleep = originalURL, originalSleep })
	mappingCurrentVersionURL = server.URL
	var waits []time.Duration
	reactomeSleep = func(wait time.Duration) { waits = append(waits, wait) }
	version, err := resolveMappingFetchVersionToken(server.Client(), "", 3, time.Second)
	if err != nil || version != "v96" {
		t.Fatalf("version=%q err=%v", version, err)
	}
	if !reflect.DeepEqual(waits, []time.Duration{2 * time.Minute, time.Second}) {
		t.Fatalf("Retry-After leaked across attempts: %#v", waits)
	}
}

func TestResolveMappingCurrentVersionFinalTransportErrorReportsStatus(t *testing.T) {
	attempts := 0
	client := &http.Client{Transport: reactomeRoundTripFunc(func(*http.Request) (*http.Response, error) {
		attempts++
		if attempts == 1 {
			return &http.Response{
				StatusCode: 521,
				Status:     "521 Web Server Is Down",
				Header:     http.Header{},
				Body:       io.NopCloser(strings.NewReader("")),
			}, nil
		}
		return nil, errors.New("network unavailable")
	})}
	originalSleep := reactomeSleep
	t.Cleanup(func() { reactomeSleep = originalSleep })
	reactomeSleep = func(time.Duration) {}
	_, err := resolveMappingFetchVersionToken(client, "", 2, 0)
	if err == nil || !strings.Contains(err.Error(), "status=transport-error") ||
		!strings.Contains(err.Error(), "attempts=2") {
		t.Fatalf("error = %v", err)
	}
}

func TestResolveMappingExplicitVersionBypassesCurrentProbe(t *testing.T) {
	probes := 0
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		probes++
		writer.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()
	version, err := resolveMappingFetchVersionToken(server.Client(), "v96", 1, 0)
	if err != nil || version != "v96" || probes != 0 {
		t.Fatalf("version=%q probes=%d err=%v", version, probes, err)
	}
	assets := buildMappingStaticAssets(
		fmt.Sprintf(mappingReleaseBaseURL, strings.TrimPrefix(version, "v")),
		[]string{"ReactomePathways.txt"},
	)
	if len(assets) != 1 || assets[0].URL != "https://download.reactome.org/96/ReactomePathways.txt" {
		t.Fatalf("explicit immutable URL = %#v", assets)
	}
}

func TestRunFetchMappingExplicitVersionBypassesProbe(t *testing.T) {
	probes := 0
	downloads := 0
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/ContentService/data/database/version" {
			probes++
			writer.WriteHeader(http.StatusInternalServerError)
			return
		}
		if request.URL.Path != "/96/ReactomePathways.txt" {
			t.Fatalf("path = %s", request.URL.Path)
		}
		if request.Method == http.MethodGet {
			downloads++
		}
		_, _ = writer.Write([]byte("pathways"))
	}))
	defer server.Close()
	originalReleaseURL, originalVersionURL := mappingReleaseBaseURL, mappingCurrentVersionURL
	t.Cleanup(func() {
		mappingReleaseBaseURL = originalReleaseURL
		mappingCurrentVersionURL = originalVersionURL
	})
	mappingReleaseBaseURL = server.URL + "/%s/"
	mappingCurrentVersionURL = server.URL + "/ContentService/data/database/version"
	cfg := createDefaultMappingConfig()
	cfg.DirOut = t.TempDir()
	cfg.VersionToken = "v96"
	cfg.RetryMax = 1
	cfg.WorkersMax = 1
	cfg.shouldAllowLargeAssets = true
	cfg.assetNames = []string{"ReactomePathways.txt"}
	if err := runFetchMapping(&cfg); err != nil {
		t.Fatal(err)
	}
	if probes != 0 || downloads != 1 {
		t.Fatalf("probes=%d downloads=%d", probes, downloads)
	}
}

func TestReactomeRetryAfterHTTPDateUsesInjectedClock(t *testing.T) {
	originalNow := reactomeNow
	t.Cleanup(func() { reactomeNow = originalNow })
	reactomeNow = func() time.Time { return time.Date(2015, 10, 21, 7, 28, 0, 0, time.UTC) }
	if wait := reactomeRetryAfter("Wed, 21 Oct 2015 07:28:10 GMT", time.Second); wait != 10*time.Second {
		t.Fatalf("wait = %s", wait)
	}
}

func TestReactomeRetryAfterNeverShortensConfiguredWait(t *testing.T) {
	originalNow := reactomeNow
	t.Cleanup(func() { reactomeNow = originalNow })
	reactomeNow = func() time.Time { return time.Date(2015, 10, 21, 7, 28, 20, 0, time.UTC) }
	for _, value := range []string{"0", "invalid", "Wed, 21 Oct 2015 07:28:10 GMT"} {
		if wait := reactomeRetryAfter(value, time.Minute); wait != time.Minute {
			t.Fatalf("Retry-After %q shortened wait to %s", value, wait)
		}
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

	originalReleaseURL := mappingReleaseBaseURL
	originalVersionURL := mappingCurrentVersionURL
	t.Cleanup(func() {
		mappingReleaseBaseURL = originalReleaseURL
		mappingCurrentVersionURL = originalVersionURL
	})
	mappingReleaseBaseURL = server.URL + "/%s/"
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
	if manifest.Files[0].URL != server.URL+"/96/ReactomePathways.txt" {
		t.Fatalf("immutable release URL = %q", manifest.Files[0].URL)
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

	originalReleaseURL := mappingReleaseBaseURL
	originalVersionURL := mappingCurrentVersionURL
	t.Cleanup(func() {
		mappingReleaseBaseURL = originalReleaseURL
		mappingCurrentVersionURL = originalVersionURL
	})
	mappingReleaseBaseURL = server.URL + "/%s/"
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
	cfg.DirSnapshot = filepath.Join(t.TempDir(), "current")
	err := runLockMapping(&cfg)
	if err == nil {
		t.Fatal("runLockMapping returned nil error")
	}
}

func TestRunSyncMappingRejectsCurrentVersion(t *testing.T) {
	cfg := mappingRestoreConfig{}
	cfg.DirOut = t.TempDir()
	cfg.VersionToken = "current"
	cfg.RuleExisting = "skip"
	cfg.RetryMax = 1
	cfg.WorkersMax = 1
	err := runRestoreMapping(&cfg)
	if err == nil {
		t.Fatal("runRestoreMapping returned nil error")
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

	originalReleaseURL := mappingReleaseBaseURL
	originalVersionURL := mappingCurrentVersionURL
	t.Cleanup(func() {
		mappingReleaseBaseURL = originalReleaseURL
		mappingCurrentVersionURL = originalVersionURL
	})
	mappingReleaseBaseURL = server.URL + "/%s/"
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
	syncCfg := mappingRestoreConfig{}
	syncCfg.DirOut = cfg.DirOut
	syncCfg.VersionToken = "96"
	syncCfg.RuleExisting = "skip"
	syncCfg.RetryMax = 1
	syncCfg.WorkersMax = 1
	if err := runRestoreMapping(&syncCfg); err != nil {
		t.Fatalf("runRestoreMapping returned error: %v", err)
	}
	if _, err := os.Stat(fileOut); err != nil {
		t.Fatalf("synced file missing: %v", err)
	}
}
