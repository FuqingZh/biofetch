package staticasset

import (
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
		countRequests++
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
	if !hasEvent(trace.events, "reuse_file") {
		t.Fatalf("trace events do not include reuse_file: %#v", trace.events)
	}
}

func TestFetchDoesNotReuseSameSizeChangedContent(t *testing.T) {
	body := "bravo"
	countRequests := 0
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		countRequests++
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
	if err := Lock(source, options, nil); err != nil {
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

func TestLockScansConfiguredDirectories(t *testing.T) {
	dirOut := t.TempDir()
	dirTidy := filepath.Join(dirOut, "fixed", "v1", "tidy")
	if err := os.MkdirAll(dirTidy, 0o755); err != nil {
		t.Fatalf("os.MkdirAll returned error: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dirTidy, "asset.tsv"), []byte("content"), 0o644); err != nil {
		t.Fatalf("os.WriteFile returned error: %v", err)
	}

	options := Options{DirOut: dirOut, RuleExisting: "skip", RetryMax: 1, WorkersMax: 1}
	source := Source{Database: "testdb", Asset: "fixed", Version: "v1", VersionToken: "v1", ScanDirs: []string{"tidy"}}
	if err := Lock(source, options, nil); err != nil {
		t.Fatalf("Lock returned error: %v", err)
	}
	manifest, ok, err := ReadManifest(filepath.Join(dirOut, "fixed", "v1", "manifest.lock"))
	if err != nil {
		t.Fatalf("ReadManifest returned error: %v", err)
	}
	if !ok {
		t.Fatal("manifest was not written")
	}
	if len(manifest.Files) != 1 || manifest.Files[0].Path != "tidy/asset.tsv" {
		t.Fatalf("manifest files = %#v", manifest.Files)
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
	for _, path := range []string{"", "/abs.txt", "../escape.txt", "raw/../escape.txt"} {
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
