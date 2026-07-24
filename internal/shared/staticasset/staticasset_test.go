package staticasset

import (
	"biofetch/internal/shared/filehash"
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	toml "github.com/pelletier/go-toml/v2"
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

func TestFetchVerifiesCompletedPartBeforeRenameAndManifest(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		_, _ = writer.Write([]byte("verified"))
	}))
	defer server.Close()

	dirOut := t.TempDir()
	fileOut := filepath.Join(dirOut, "fixed", "v1", "raw", "archive.tar.gz")
	called := false
	source := Source{
		Database: "testdb", Asset: "fixed", Version: "v1", VersionToken: "v1",
		Assets: []Asset{{
			Name: "archive", Path: "raw/archive.tar.gz", URL: server.URL + "/archive.tar.gz",
			VerifyDownloadedFile: func(path string) error {
				called = true
				if path != fileOut+".part" {
					t.Fatalf("verifier path = %q, want %q", path, fileOut+".part")
				}
				if _, err := os.Stat(fileOut); !os.IsNotExist(err) {
					t.Fatalf("final file existed during verification: %v", err)
				}
				data, err := os.ReadFile(path)
				if err != nil {
					return err
				}
				if string(data) != "verified" {
					t.Fatalf("part content = %q", data)
				}
				return nil
			},
		}},
	}
	if err := Fetch(source, Options{DirOut: dirOut, RuleExisting: "skip", RetryMax: 1, WorkersMax: 1}, nil); err != nil {
		t.Fatalf("Fetch returned error: %v", err)
	}
	if !called {
		t.Fatal("verifier was not called")
	}
	manifest, ok, err := ReadManifest(filepath.Join(dirOut, "fixed", "v1", "manifest.lock"))
	if err != nil || !ok || len(manifest.Files) != 1 {
		t.Fatalf("manifest = %#v, ok = %v, err = %v", manifest, ok, err)
	}
}

func TestFetchVerifierFailureRemovesPartAndNextInvocationRedownloads(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method == http.MethodGet {
			requests++
			if got := request.Header.Get("Range"); got != "" {
				t.Fatalf("Range = %q, want clean download", got)
			}
		}
		_, _ = writer.Write([]byte("corrupt"))
	}))
	defer server.Close()

	dirOut := t.TempDir()
	verifications := 0
	source := Source{
		Database: "testdb", Asset: "fixed", Version: "v1", VersionToken: "v1",
		Assets: []Asset{{
			Name: "archive", Path: "raw/archive.tar.gz", URL: server.URL + "/archive.tar.gz",
			VerifyDownloadedFile: func(string) error {
				verifications++
				if verifications == 1 {
					return fmt.Errorf("checksum mismatch")
				}
				return nil
			},
		}},
	}
	err := Fetch(source, Options{DirOut: dirOut, RuleExisting: "skip", RetryMax: 3, WorkersMax: 1}, nil)
	if err == nil || !strings.Contains(err.Error(), `asset "archive" failed downloaded-file verification`) {
		t.Fatalf("Fetch error = %v", err)
	}
	fileOut := filepath.Join(dirOut, "fixed", "v1", "raw", "archive.tar.gz")
	if _, err := os.Stat(fileOut); !os.IsNotExist(err) {
		t.Fatalf("final file exists or stat failed: %v", err)
	}
	if _, err := os.Stat(fileOut + ".part"); !os.IsNotExist(err) {
		t.Fatalf("failed part exists or stat failed: %v", err)
	}
	manifest, ok, readErr := ReadManifest(filepath.Join(dirOut, "fixed", "v1", "manifest.lock"))
	if readErr != nil || (ok && len(manifest.Files) != 0) {
		t.Fatalf("manifest = %#v, ok = %v, err = %v", manifest, ok, readErr)
	}
	if err := Fetch(source, Options{DirOut: dirOut, RuleExisting: "skip", RetryMax: 1, WorkersMax: 1}, nil); err != nil {
		t.Fatalf("second Fetch returned error: %v", err)
	}
	if requests != 2 || verifications != 2 {
		t.Fatalf("requests = %d, verifications = %d, want 2 each", requests, verifications)
	}
	if data, err := os.ReadFile(fileOut); err != nil || string(data) != "corrupt" {
		t.Fatalf("final file = %q, err = %v", data, err)
	}
}

func TestFetchVerifierChecksResumedPart(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if got := request.Header.Get("Range"); got != "bytes=4-" {
			t.Fatalf("Range = %q, want bytes=4-", got)
		}
		writer.WriteHeader(http.StatusPartialContent)
		_, _ = writer.Write([]byte("ived"))
	}))
	defer server.Close()

	dirOut := t.TempDir()
	filePart := filepath.Join(dirOut, "fixed", "v1", "raw", "archive.tar.gz.part")
	if err := os.MkdirAll(filepath.Dir(filePart), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filePart, []byte("arch"), 0o644); err != nil {
		t.Fatal(err)
	}
	verified := false
	source := Source{
		Database: "testdb", Asset: "fixed", Version: "v1", VersionToken: "v1",
		Assets: []Asset{{
			Name: "archive", Path: "raw/archive.tar.gz", URL: server.URL + "/archive.tar.gz",
			VerifyDownloadedFile: func(path string) error {
				data, err := os.ReadFile(path)
				verified = err == nil && string(data) == "archived"
				return err
			},
		}},
	}
	if err := Fetch(source, Options{DirOut: dirOut, RuleExisting: "skip", RetryMax: 1, WorkersMax: 1}, nil); err != nil {
		t.Fatalf("Fetch returned error: %v", err)
	}
	if !verified {
		t.Fatal("resumed part was not verified")
	}
}

func TestAssetVerifierIsNotSerialized(t *testing.T) {
	data, err := toml.Marshal(Asset{
		Name: "archive", Path: "raw/archive", URL: "https://example.test/archive",
		VerifyDownloadedFile: func(string) error { return nil },
	})
	if err != nil {
		t.Fatalf("Marshal returned error: %v", err)
	}
	if strings.Contains(string(data), "VerifyDownloadedFile") || strings.Contains(string(data), "verify") {
		t.Fatalf("serialized asset contains verifier: %s", data)
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

func TestSyncRejectsMovingURLContentThatDoesNotMatchManifest(t *testing.T) {
	contentLocked := []byte("locked")
	contentCurrent := []byte("changed")
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		_, _ = writer.Write(contentCurrent)
	}))
	defer server.Close()

	dirOut := t.TempDir()
	dirVersion := filepath.Join(dirOut, "fixed", "v1")
	if err := os.MkdirAll(dirVersion, 0o755); err != nil {
		t.Fatalf("os.MkdirAll returned error: %v", err)
	}
	hashLocked, err := filehash.SHA256(bytes.NewReader(contentLocked))
	if err != nil {
		t.Fatalf("filehash.SHA256 returned error: %v", err)
	}
	source := Source{Database: "testdb", Asset: "fixed", Source: "fixture", Version: "v1", VersionToken: "v1"}
	recordLocked := FileRecord{
		Asset: "moving", Path: "raw/moving.txt", SHA256: hashLocked,
		Bytes: int64(len(contentLocked)), URL: server.URL + "/moving.txt",
	}
	fileManifest := filepath.Join(dirVersion, "manifest.lock")
	if err := writeManifest(fileManifest, source, []FileRecord{recordLocked}, time.Now()); err != nil {
		t.Fatalf("writeManifest returned error: %v", err)
	}

	options := Options{DirOut: dirOut, RuleExisting: "skip", RetryMax: 1, WorkersMax: 1}
	err = Sync(source, options, nil)
	if err == nil || !strings.Contains(err.Error(), "SHA256 mismatch") {
		t.Fatalf("Sync error = %v, want SHA256 mismatch", err)
	}
	fileOut := filepath.Join(dirVersion, "raw", "moving.txt")
	if _, statErr := os.Stat(fileOut); !os.IsNotExist(statErr) {
		t.Fatalf("restored file exists or stat failed: %v", statErr)
	}
	if _, statErr := os.Stat(fileOut + ".part"); !os.IsNotExist(statErr) {
		t.Fatalf("failed part exists or stat failed: %v", statErr)
	}
	manifest, ok, readErr := ReadManifest(fileManifest)
	if readErr != nil || !ok {
		t.Fatalf("ReadManifest = ok %v, err %v", ok, readErr)
	}
	if len(manifest.Files) != 1 || manifest.Files[0].SHA256 != hashLocked {
		t.Fatalf("manifest files = %#v, want original locked record", manifest.Files)
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

func TestFetchKeepsVerifierRejectedFileWhenReplacementFails(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		http.Error(writer, "failed", http.StatusInternalServerError)
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
			Name: "archive",
			Path: "raw/archive.tar.gz",
			URL:  server.URL + "/archive.tar.gz",
			VerifyDownloadedFile: func(string) error {
				return fmt.Errorf("sidecar checksum mismatch")
			},
		}},
	}
	options := Options{DirOut: dirOut, RuleExisting: "skip", RetryMax: 1, WorkersMax: 1}
	dirVersion := DeriveVersionDir(dirOut, source)
	filePath := filepath.Join(dirVersion, "raw", "archive.tar.gz")
	if err := os.MkdirAll(filepath.Dir(filePath), 0o755); err != nil {
		t.Fatalf("os.MkdirAll returned error: %v", err)
	}
	content := []byte("old archive")
	if err := os.WriteFile(filePath, content, 0o644); err != nil {
		t.Fatalf("os.WriteFile returned error: %v", err)
	}
	file, err := os.Open(filePath)
	if err != nil {
		t.Fatalf("os.Open returned error: %v", err)
	}
	hash, err := filehash.SHA256(file)
	if closeErr := file.Close(); closeErr != nil {
		t.Fatalf("file.Close returned error: %v", closeErr)
	}
	if err != nil {
		t.Fatalf("filehash.SHA256 returned error: %v", err)
	}
	records := []FileRecord{{Asset: "archive", Path: "raw/archive.tar.gz", SHA256: hash, Bytes: int64(len(content)), URL: source.Assets[0].URL}}
	if err := writeManifest(filepath.Join(dirVersion, "manifest.lock"), source, records, time.Now()); err != nil {
		t.Fatalf("writeManifest returned error: %v", err)
	}

	err = Fetch(source, options, nil)
	if err == nil {
		t.Fatal("Fetch returned nil error")
	}
	got, readErr := os.ReadFile(filePath)
	if readErr != nil {
		t.Fatalf("os.ReadFile returned error: %v", readErr)
	}
	if !bytes.Equal(got, content) {
		t.Fatalf("file content = %q, want %q", got, content)
	}
	manifest, ok, readErr := ReadManifest(filepath.Join(dirVersion, "manifest.lock"))
	if readErr != nil || !ok {
		t.Fatalf("ReadManifest = ok %v, err %v", ok, readErr)
	}
	if len(manifest.Files) != 1 || manifest.Files[0].SHA256 != hash {
		t.Fatalf("manifest files = %#v", manifest.Files)
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
	hashAlpha, err := filehash.SHA256(strings.NewReader("alpha"))
	if err != nil {
		t.Fatalf("filehash.SHA256 returned error: %v", err)
	}
	records := []FileRecord{
		{Asset: "alpha", Path: "raw/alpha.txt", SHA256: hashAlpha, Bytes: 5, URL: server.URL + "/alpha.txt"},
		{Asset: "bravo", Path: "raw/bravo.txt", SHA256: "old", Bytes: 5, URL: server.URL + "/bravo.txt"},
	}
	if err := writeManifest(filepath.Join(dirVersion, "manifest.lock"), source, records, time.Now()); err != nil {
		t.Fatalf("writeManifest returned error: %v", err)
	}

	options := Options{DirOut: dirOut, RuleExisting: "skip", RetryMax: 1, WorkersMax: 1}
	err = Sync(source, options, nil)
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
	if len(manifest.Files) != 1 || manifest.Files[0].Path != "raw/alpha.txt" || manifest.Files[0].SHA256 != hashAlpha {
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
	for _, forbidden := range []string{"downloading", "downloaded", "retry"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("lock progress contains download vocabulary %q: %q", forbidden, text)
		}
	}
	if !strings.Contains(text, "hashing") || !strings.Contains(text, "hashed") {
		t.Fatalf("lock progress does not describe hashing: %q", text)
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

func TestProgressFinalRepaintClearsLongerPreviousLine(t *testing.T) {
	var output bytes.Buffer
	progress := &progressReporter{
		writer:      &output,
		label:       "testdb fixed",
		timeStarted: time.Now(),
		totalFiles:  1,
	}
	progress.drawLocked("hashing raw/a-very-long-file-name.txt", true)
	progress.finish(true)

	var visible string
	for _, character := range output.String() {
		switch character {
		case '\r':
			visible = ""
		case '\n':
			goto done
		default:
			visible += string(character)
		}
	}
done:
	if got := strings.TrimRight(visible, " "); !strings.HasSuffix(got, "completed") {
		t.Fatalf("visible final line = %q", visible)
	}
	if strings.Contains(visible, "file-name") {
		t.Fatalf("visible final line retained repaint residue: %q", visible)
	}
	if strings.Contains(output.String(), "\x1b") {
		t.Fatalf("captured progress contains ANSI escapes: %q", output.String())
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
