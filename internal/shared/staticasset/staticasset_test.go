package staticasset

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

type traceBuffer struct {
	events []TraceEvent
}

func (trace *traceBuffer) EmitStaticAssetTrace(event TraceEvent) {
	trace.events = append(trace.events, event)
}

func TestFetchDownloadsAndReusesBySHA256(t *testing.T) {
	countRequests := 0
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method == http.MethodGet && request.Header.Get("Range") == "" {
			countRequests++
		}
		_, _ = writer.Write([]byte("alpha"))
	}))
	defer server.Close()

	dirOut := t.TempDir()
	source := Source{
		Database:     "testdb",
		Asset:        "fixed",
		Source:       "fixture",
		Version:      "v1",
		VersionToken: "v1",
		Assets: []Asset{{
			Name: "alpha",
			Path: "raw/alpha.txt",
			URL:  server.URL + "/alpha.txt",
		}},
	}
	options := Options{
		DirOut:       dirOut,
		RuleExisting: "skip",
		RetryMax:     1,
		WorkersMax:   1,
	}

	var trace traceBuffer
	if err := Fetch(source, options, &trace); err != nil {
		t.Fatalf("Fetch first run returned error: %v", err)
	}
	if err := Fetch(source, options, &trace); err != nil {
		t.Fatalf("Fetch second run returned error: %v", err)
	}
	if countRequests != 1 {
		t.Fatalf("countRequests = %d, want 1", countRequests)
	}

	fileManifest := filepath.Join(dirOut, "fixed", "v1", "manifest.lock")
	manifest, ok, err := ReadManifest(fileManifest)
	if err != nil {
		t.Fatalf("ReadManifest returned error: %v", err)
	}
	if !ok {
		t.Fatal("manifest was not written")
	}
	if manifest.Database != "testdb" || manifest.Asset != "fixed" || manifest.Source != "fixture" {
		t.Fatalf("manifest identity = %#v", manifest)
	}
	if len(manifest.Files) != 1 || manifest.Files[0].SHA256 == "" || manifest.Files[0].Bytes != 5 {
		t.Fatalf("manifest files = %#v", manifest.Files)
	}
	if _, err := os.Stat(filepath.Join(dirOut, "fixed", "v1", "tidy")); !os.IsNotExist(err) {
		t.Fatalf("fetch created tidy dir or stat returned unexpected error: %v", err)
	}
	if !hasEvent(trace.events, "reuse_file") {
		t.Fatalf("trace events do not include reuse_file: %#v", trace.events)
	}
}

func TestFetchWritesManifestAfterEachDownloadedFile(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/alpha.txt":
			_, _ = writer.Write([]byte("alpha"))
		case "/bravo.txt":
			http.Error(writer, "failed", http.StatusInternalServerError)
		default:
			t.Fatalf("path = %s", request.URL.Path)
		}
	}))
	defer server.Close()

	dirOut := t.TempDir()
	source := Source{
		Database:     "testdb",
		Asset:        "fixed",
		Source:       "fixture",
		Version:      "v1",
		VersionToken: "v1",
		Assets: []Asset{
			{Name: "alpha", Path: "raw/alpha.txt", URL: server.URL + "/alpha.txt"},
			{Name: "bravo", Path: "raw/bravo.txt", URL: server.URL + "/bravo.txt"},
		},
	}
	options := Options{DirOut: dirOut, RuleExisting: "skip", RetryMax: 1, WorkersMax: 1}

	err := Fetch(source, options, nil)
	if err == nil {
		t.Fatal("Fetch returned nil error")
	}
	manifest, ok, err := ReadManifest(filepath.Join(dirOut, "fixed", "v1", "manifest.lock"))
	if err != nil {
		t.Fatalf("ReadManifest returned error: %v", err)
	}
	if !ok {
		t.Fatal("manifest was not written")
	}
	if len(manifest.Files) != 1 || manifest.Files[0].Path != "raw/alpha.txt" || manifest.Files[0].Bytes != 5 {
		t.Fatalf("manifest files = %#v", manifest.Files)
	}
}

func TestFetchDoesNotReuseSameSizeChangedContent(t *testing.T) {
	body := "bravo"
	countRequests := 0
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method == http.MethodGet && request.Header.Get("Range") == "" {
			countRequests++
		}
		_, _ = writer.Write([]byte(body))
	}))
	defer server.Close()

	dirOut := t.TempDir()
	source := Source{
		Database:     "testdb",
		Asset:        "fixed",
		Version:      "v1",
		VersionToken: "v1",
		Assets: []Asset{{
			Name: "alpha",
			Path: "raw/alpha.txt",
			URL:  server.URL + "/alpha.txt",
		}},
	}
	options := Options{DirOut: dirOut, RuleExisting: "skip", RetryMax: 1, WorkersMax: 1}

	if err := Fetch(source, options, nil); err != nil {
		t.Fatalf("Fetch first run returned error: %v", err)
	}
	fileOut := filepath.Join(dirOut, "fixed", "v1", "raw", "alpha.txt")
	if err := os.WriteFile(fileOut, []byte("xxxxx"), 0o644); err != nil {
		t.Fatalf("os.WriteFile returned error: %v", err)
	}
	if err := Fetch(source, options, nil); err != nil {
		t.Fatalf("Fetch second run returned error: %v", err)
	}
	if countRequests != 2 {
		t.Fatalf("countRequests = %d, want 2", countRequests)
	}
	data, err := os.ReadFile(fileOut)
	if err != nil {
		t.Fatalf("os.ReadFile returned error: %v", err)
	}
	if string(data) != body {
		t.Fatalf("file content = %q, want %q", string(data), body)
	}
}

func TestFetchWritesProgressByDefault(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Length", "5")
		_, _ = writer.Write([]byte("alpha"))
	}))
	defer server.Close()

	var progress bytes.Buffer
	source := Source{
		Database:     "testdb",
		Asset:        "fixed",
		Version:      "v1",
		VersionToken: "v1",
		Assets: []Asset{{
			Name: "alpha",
			Path: "raw/alpha.txt",
			URL:  server.URL + "/alpha.txt",
		}},
	}
	options := Options{
		DirOut:         t.TempDir(),
		RuleExisting:   "skip",
		RetryMax:       1,
		WorkersMax:     1,
		ProgressWriter: &progress,
	}
	if err := Fetch(source, options, nil); err != nil {
		t.Fatalf("Fetch returned error: %v", err)
	}
	text := progress.String()
	if !strings.Contains(text, "testdb fixed") || !strings.Contains(text, "100%") || !strings.Contains(text, "5 B/5 B") {
		t.Fatalf("progress = %q", text)
	}
}

func TestFetchWritesCurrentFileProgressForMultipleFiles(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Length", "5")
		_, _ = writer.Write([]byte("alpha"))
	}))
	defer server.Close()

	var progress bytes.Buffer
	source := Source{
		Database:     "testdb",
		Asset:        "fixed",
		Version:      "v1",
		VersionToken: "v1",
		Assets: []Asset{
			{
				Name: "alpha",
				Path: "raw/alpha.txt",
				URL:  server.URL + "/alpha.txt",
			},
			{
				Name: "bravo",
				Path: "raw/bravo.txt",
				URL:  server.URL + "/bravo.txt",
			},
		},
	}
	options := Options{
		DirOut:         t.TempDir(),
		RuleExisting:   "skip",
		RetryMax:       1,
		WorkersMax:     1,
		ProgressWriter: &progress,
	}
	if err := Fetch(source, options, nil); err != nil {
		t.Fatalf("Fetch returned error: %v", err)
	}
	text := progress.String()
	if !strings.Contains(text, "2/2 files") || !strings.Contains(text, "current") || !strings.Contains(text, "5 B/5 B") {
		t.Fatalf("progress = %q", text)
	}
}

func TestFetchCanDisableProgress(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		_, _ = writer.Write([]byte("alpha"))
	}))
	defer server.Close()

	var progress bytes.Buffer
	source := Source{
		Database:     "testdb",
		Asset:        "fixed",
		Version:      "v1",
		VersionToken: "v1",
		Assets: []Asset{{
			Name: "alpha",
			Path: "raw/alpha.txt",
			URL:  server.URL + "/alpha.txt",
		}},
	}
	options := Options{
		DirOut:                t.TempDir(),
		RuleExisting:          "skip",
		RetryMax:              1,
		WorkersMax:            1,
		ProgressWriter:        &progress,
		ShouldDisableProgress: true,
	}
	if err := Fetch(source, options, nil); err != nil {
		t.Fatalf("Fetch returned error: %v", err)
	}
	if progress.String() != "" {
		t.Fatalf("progress = %q, want empty", progress.String())
	}
}

func TestSyncRehydratesFromManifest(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		_, _ = writer.Write([]byte("synced"))
	}))
	defer server.Close()

	dirOut := t.TempDir()
	source := Source{
		Database:     "testdb",
		Asset:        "fixed",
		Version:      "v1",
		VersionToken: "v1",
		Assets: []Asset{{
			Name: "sync",
			Path: "raw/sync.txt",
			URL:  server.URL + "/sync.txt",
		}},
	}
	options := Options{DirOut: dirOut, RuleExisting: "skip", RetryMax: 1, WorkersMax: 1}

	if err := Fetch(source, options, nil); err != nil {
		t.Fatalf("Fetch returned error: %v", err)
	}
	fileOut := filepath.Join(dirOut, "fixed", "v1", "raw", "sync.txt")
	if err := os.Remove(fileOut); err != nil {
		t.Fatalf("os.Remove returned error: %v", err)
	}
	if err := Sync(Source{Database: "testdb", Asset: "fixed", VersionToken: "v1", Version: "v1"}, options, nil); err != nil {
		t.Fatalf("Sync returned error: %v", err)
	}
	data, err := os.ReadFile(fileOut)
	if err != nil {
		t.Fatalf("os.ReadFile returned error: %v", err)
	}
	if string(data) != "synced" {
		t.Fatalf("file content = %q, want synced", string(data))
	}
}

func TestSyncRejectsManifestIdentityMismatch(t *testing.T) {
	dirOut := t.TempDir()
	dirVersion := filepath.Join(dirOut, "fixed", "v1")
	if err := os.MkdirAll(dirVersion, 0o755); err != nil {
		t.Fatalf("os.MkdirAll returned error: %v", err)
	}
	manifestSource := Source{Database: "otherdb", Asset: "fixed", Source: "fixture", Version: "v1", VersionToken: "v1"}
	records := []FileRecord{{Asset: "alpha", Path: "raw/alpha.txt", SHA256: "old", Bytes: 5, URL: "https://example.invalid/alpha.txt"}}
	if err := writeManifest(filepath.Join(dirVersion, "manifest.lock"), manifestSource, records, time.Now()); err != nil {
		t.Fatalf("writeManifest returned error: %v", err)
	}

	source := Source{Database: "testdb", Asset: "fixed", Version: "v1", VersionToken: "v1"}
	options := Options{DirOut: dirOut, RuleExisting: "skip", RetryMax: 1, WorkersMax: 1}
	err := Sync(source, options, nil)
	if err == nil || !strings.Contains(err.Error(), "manifest identity mismatch") {
		t.Fatalf("Sync error = %v, want manifest identity mismatch", err)
	}
}

func TestSyncWritesManifestAfterEachDownloadedFile(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/alpha.txt":
			_, _ = writer.Write([]byte("alpha"))
		case "/bravo.txt":
			http.Error(writer, "failed", http.StatusInternalServerError)
		default:
			t.Fatalf("path = %s", request.URL.Path)
		}
	}))
	defer server.Close()

	dirOut := t.TempDir()
	dirVersion := filepath.Join(dirOut, "fixed", "v1")
	if err := os.MkdirAll(dirVersion, 0o755); err != nil {
		t.Fatalf("os.MkdirAll returned error: %v", err)
	}
	source := Source{Database: "testdb", Asset: "fixed", Source: "fixture", Version: "v1", VersionToken: "v1"}
	records := []FileRecord{
		{Asset: "alpha", Path: "raw/alpha.txt", SHA256: "old", Bytes: 5, URL: server.URL + "/alpha.txt"},
		{Asset: "bravo", Path: "raw/bravo.txt", SHA256: "old", Bytes: 5, URL: server.URL + "/bravo.txt"},
	}
	if err := writeManifest(filepath.Join(dirVersion, "manifest.lock"), source, records, time.Now()); err != nil {
		t.Fatalf("writeManifest returned error: %v", err)
	}

	options := Options{DirOut: dirOut, RuleExisting: "skip", RetryMax: 1, WorkersMax: 1}
	err := Sync(source, options, nil)
	if err == nil {
		t.Fatal("Sync returned nil error")
	}
	manifest, ok, err := ReadManifest(filepath.Join(dirVersion, "manifest.lock"))
	if err != nil {
		t.Fatalf("ReadManifest returned error: %v", err)
	}
	if !ok {
		t.Fatal("manifest was not written")
	}
	if len(manifest.Files) != 1 || manifest.Files[0].Path != "raw/alpha.txt" || manifest.Files[0].SHA256 == "old" {
		t.Fatalf("manifest files = %#v", manifest.Files)
	}
}

func TestLockScansRawRecursivelyAndIgnoresPartFiles(t *testing.T) {
	dirOut := t.TempDir()
	dirRaw := filepath.Join(dirOut, "fixed", "v1", "raw", "human")
	if err := os.MkdirAll(dirRaw, 0o755); err != nil {
		t.Fatalf("os.MkdirAll returned error: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dirRaw, "asset.tsv"), []byte("content"), 0o644); err != nil {
		t.Fatalf("os.WriteFile returned error: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dirRaw, "asset.tsv.part"), []byte("partial"), 0o644); err != nil {
		t.Fatalf("os.WriteFile returned error: %v", err)
	}
	options := Options{DirOut: dirOut, RuleExisting: "skip", RetryMax: 1, WorkersMax: 1}
	source := Source{Database: "testdb", Asset: "fixed", Version: "v1", VersionToken: "v1"}
	if err := Lock(source, DeriveVersionDir(options.DirOut, source), options, nil); err != nil {
		t.Fatalf("Lock returned error: %v", err)
	}
	manifest, ok, err := ReadManifest(filepath.Join(dirOut, "fixed", "v1", "manifest.lock"))
	if err != nil {
		t.Fatalf("ReadManifest returned error: %v", err)
	}
	if !ok {
		t.Fatal("manifest was not written")
	}
	if len(manifest.Files) != 1 {
		t.Fatalf("len(manifest.Files) = %d, want 1", len(manifest.Files))
	}
	if manifest.Files[0].Path != "raw/human/asset.tsv" {
		t.Fatalf("manifest path = %q", manifest.Files[0].Path)
	}
}

func TestLockIgnoresFilesOutsideRaw(t *testing.T) {
	dirOut := t.TempDir()
	dirTidy := filepath.Join(dirOut, "fixed", "v1", "tidy")
	dirRaw := filepath.Join(dirOut, "fixed", "v1", "raw")
	if err := os.MkdirAll(dirTidy, 0o755); err != nil {
		t.Fatalf("os.MkdirAll returned error: %v", err)
	}
	if err := os.MkdirAll(dirRaw, 0o755); err != nil {
		t.Fatalf("os.MkdirAll returned error: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dirTidy, "asset.tsv"), []byte("content"), 0o644); err != nil {
		t.Fatalf("os.WriteFile returned error: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dirRaw, "asset.tsv"), []byte("raw"), 0o644); err != nil {
		t.Fatalf("os.WriteFile returned error: %v", err)
	}

	options := Options{DirOut: dirOut, RuleExisting: "skip", RetryMax: 1, WorkersMax: 1}
	source := Source{Database: "testdb", Asset: "fixed", Version: "v1", VersionToken: "v1"}
	if err := Lock(source, DeriveVersionDir(options.DirOut, source), options, nil); err != nil {
		t.Fatalf("Lock returned error: %v", err)
	}
	manifest, ok, err := ReadManifest(filepath.Join(dirOut, "fixed", "v1", "manifest.lock"))
	if err != nil {
		t.Fatalf("ReadManifest returned error: %v", err)
	}
	if !ok {
		t.Fatal("manifest was not written")
	}
	if len(manifest.Files) != 1 || manifest.Files[0].Path != "raw/asset.tsv" {
		t.Fatalf("manifest files = %#v", manifest.Files)
	}
}

func TestLockWritesHashProgress(t *testing.T) {
	dirOut := t.TempDir()
	dirRaw := filepath.Join(dirOut, "fixed", "v1", "raw")
	if err := os.MkdirAll(dirRaw, 0o755); err != nil {
		t.Fatalf("os.MkdirAll returned error: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dirRaw, "asset.tsv"), []byte("content"), 0o644); err != nil {
		t.Fatalf("os.WriteFile returned error: %v", err)
	}

	var progress bytes.Buffer
	options := Options{
		DirOut:         dirOut,
		RuleExisting:   "skip",
		RetryMax:       1,
		WorkersMax:     1,
		ProgressWriter: &progress,
	}
	source := Source{Database: "testdb", Asset: "fixed", Version: "v1", VersionToken: "v1"}
	if err := Lock(source, DeriveVersionDir(options.DirOut, source), options, nil); err != nil {
		t.Fatalf("Lock returned error: %v", err)
	}
	text := progress.String()
	if !strings.Contains(text, "testdb fixed") || !strings.Contains(text, "100%") || !strings.Contains(text, "7 B/7 B") {
		t.Fatalf("progress = %q", text)
	}
}

func TestLockWritesCurrentFileProgressForMultipleFiles(t *testing.T) {
	dirOut := t.TempDir()
	dirRaw := filepath.Join(dirOut, "fixed", "v1", "raw")
	if err := os.MkdirAll(dirRaw, 0o755); err != nil {
		t.Fatalf("os.MkdirAll returned error: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dirRaw, "alpha.tsv"), []byte("alpha"), 0o644); err != nil {
		t.Fatalf("os.WriteFile returned error: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dirRaw, "bravo.tsv"), []byte("bravo"), 0o644); err != nil {
		t.Fatalf("os.WriteFile returned error: %v", err)
	}

	var progress bytes.Buffer
	options := Options{
		DirOut:         dirOut,
		RuleExisting:   "skip",
		RetryMax:       1,
		WorkersMax:     1,
		ProgressWriter: &progress,
	}
	source := Source{Database: "testdb", Asset: "fixed", Version: "v1", VersionToken: "v1"}
	if err := Lock(source, DeriveVersionDir(options.DirOut, source), options, nil); err != nil {
		t.Fatalf("Lock returned error: %v", err)
	}
	text := progress.String()
	if !strings.Contains(text, "2/2 files") || !strings.Contains(text, "current") || !strings.Contains(text, "5 B/5 B") {
		t.Fatalf("progress = %q", text)
	}
}

func TestValidateSourceRejectsUnsafePaths(t *testing.T) {
	base := Source{
		Database:     "testdb",
		Asset:        "fixed",
		Version:      "v1",
		VersionToken: "v1",
		Assets: []Asset{{
			Name: "bad",
			Path: "raw/bad.txt",
			URL:  "https://example.test/bad.txt",
		}},
	}
	for _, path := range []string{"", "/abs.txt", "../escape.txt", "raw/../escape.txt", "tidy/asset.tsv"} {
		source := base
		source.Assets[0].Path = path
		err := validateSource(source)
		if err == nil {
			t.Fatalf("validateSource returned nil for path %q", path)
		}
	}
}

func TestFetchDryRunDoesNotCreateDirectories(t *testing.T) {
	dirOut := t.TempDir()
	source := Source{
		Database:     "testdb",
		Asset:        "fixed",
		Version:      "v1",
		VersionToken: "v1",
		Assets: []Asset{{
			Name: "alpha",
			Path: "raw/alpha.txt",
			URL:  "https://example.test/alpha.txt",
		}},
	}
	options := Options{DirOut: dirOut, RuleExisting: "skip", RetryMax: 1, WorkersMax: 1, ShouldDryRun: true}
	if err := Fetch(source, options, nil); err != nil {
		t.Fatalf("Fetch returned error: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dirOut, "fixed")); !os.IsNotExist(err) {
		t.Fatalf("dry-run created output dir or stat returned unexpected error: %v", err)
	}
}

func TestTraceEventOrder(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		_, _ = writer.Write([]byte("alpha"))
	}))
	defer server.Close()

	var trace traceBuffer
	err := Fetch(Source{
		Database:     "testdb",
		Asset:        "fixed",
		Version:      "v1",
		VersionToken: "v1",
		Assets: []Asset{{
			Name: "alpha",
			Path: "raw/alpha.txt",
			URL:  server.URL + "/alpha.txt",
		}},
	}, Options{DirOut: t.TempDir(), RuleExisting: "skip", RetryMax: 1, RetryWait: time.Millisecond, WorkersMax: 1}, &trace)
	if err != nil {
		t.Fatalf("Fetch returned error: %v", err)
	}
	got := eventNames(trace.events)
	want := []string{"resolve_source", "resolve_assets", "plan_fetch", "download_file", "write_manifest"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("events = %#v, want %#v", got, want)
	}
}

func hasEvent(events []TraceEvent, name string) bool {
	for _, event := range events {
		if event.Event == name {
			return true
		}
	}
	return false
}

func eventNames(events []TraceEvent) []string {
	names := make([]string, 0, len(events))
	for _, event := range events {
		if strings.TrimSpace(event.Event) != "" {
			names = append(names, event.Event)
		}
	}
	return names
}

func TestLockRequiresSnapshotWithPublicSpelling(t *testing.T) {
	err := Lock(Source{Database: "test", Asset: "asset", Version: "v1", VersionToken: "v1"}, "", Options{
		RetryMax:   1,
		WorkersMax: 1,
	}, nil)
	if err == nil || err.Error() != "snapshot is required" {
		t.Fatalf("Lock error = %v", err)
	}
}
