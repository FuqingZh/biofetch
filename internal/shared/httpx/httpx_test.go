package httpx

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestRequestLimiterWaitSpacesConcurrentCalls(t *testing.T) {
	limiter := NewRequestLimiter(20 * time.Millisecond)

	var group sync.WaitGroup
	group.Add(3)

	times := make([]time.Time, 0, 3)
	var mutexTimes sync.Mutex

	for range 3 {
		go func() {
			defer group.Done()
			limiter.Wait()
			mutexTimes.Lock()
			times = append(times, time.Now())
			mutexTimes.Unlock()
		}()
	}

	group.Wait()

	if len(times) != 3 {
		t.Fatalf("len(times) = %d, want 3", len(times))
	}
	slices.SortFunc(times, func(left, right time.Time) int {
		switch {
		case left.Before(right):
			return -1
		case right.Before(left):
			return 1
		default:
			return 0
		}
	})

	for index := 1; index < len(times); index++ {
		delta := times[index].Sub(times[index-1])
		if delta < 15*time.Millisecond {
			t.Fatalf("delta[%d] = %s, want >= 15ms", index, delta)
		}
	}
}

func TestDownloadFileWithResumeAppendsPartialContent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if got := request.Header.Get("Range"); got != "bytes=5-" {
			t.Fatalf("Range = %q, want bytes=5-", got)
		}
		writer.WriteHeader(http.StatusPartialContent)
		_, _ = writer.Write([]byte("bravo"))
	}))
	defer server.Close()

	fileOut := filepath.Join(t.TempDir(), "asset.part")
	if err := os.WriteFile(fileOut, []byte("alpha"), 0o644); err != nil {
		t.Fatalf("os.WriteFile returned error: %v", err)
	}
	var gotProgress []int64
	err := DownloadFileWithResume(server.Client(), server.URL+"/asset", fileOut, func(bytesDone int64, bytesTotal int64) {
		gotProgress = append(gotProgress, bytesDone)
	})
	if err != nil {
		t.Fatalf("DownloadFileWithResume returned error: %v", err)
	}
	data, err := os.ReadFile(fileOut)
	if err != nil {
		t.Fatalf("os.ReadFile returned error: %v", err)
	}
	if string(data) != "alphabravo" {
		t.Fatalf("file content = %q, want alphabravo", string(data))
	}
	if len(gotProgress) == 0 || gotProgress[0] != 5 {
		t.Fatalf("progress = %#v, want first value 5", gotProgress)
	}
}

func TestDownloadFileWithResumeRestartsWhenRangeUnsupported(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if got := request.Header.Get("Range"); got != "bytes=5-" {
			t.Fatalf("Range = %q, want bytes=5-", got)
		}
		_, _ = writer.Write([]byte("replacement"))
	}))
	defer server.Close()

	fileOut := filepath.Join(t.TempDir(), "asset.part")
	if err := os.WriteFile(fileOut, []byte("alpha"), 0o644); err != nil {
		t.Fatalf("os.WriteFile returned error: %v", err)
	}
	err := DownloadFileWithResume(server.Client(), server.URL+"/asset", fileOut, nil)
	if err != nil {
		t.Fatalf("DownloadFileWithResume returned error: %v", err)
	}
	data, err := os.ReadFile(fileOut)
	if err != nil {
		t.Fatalf("os.ReadFile returned error: %v", err)
	}
	if string(data) != "replacement" {
		t.Fatalf("file content = %q, want replacement", string(data))
	}
}

func TestDownloadFileWithResumeUsesChunkedForLargeRangeAsset(t *testing.T) {
	withTestChunkConfig(t, 10, 5, 2)
	body := []byte("abcdefghijklmnopqrstuvwxyz")
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.Method {
		case http.MethodHead:
			writer.Header().Set("Accept-Ranges", "bytes")
			writer.Header().Set("Content-Length", fmt.Sprintf("%d", len(body)))
			return
		case http.MethodGet:
			start, end := parseTestRange(t, request.Header.Get("Range"))
			if start < 0 || end < start {
				t.Fatalf("Range = %q", request.Header.Get("Range"))
			}
			writer.WriteHeader(http.StatusPartialContent)
			_, _ = writer.Write(body[start : end+1])
		default:
			t.Fatalf("method = %s", request.Method)
		}
	}))
	defer server.Close()

	fileOut := filepath.Join(t.TempDir(), "asset.part")
	err := DownloadFileWithResume(server.Client(), server.URL+"/asset", fileOut, nil)
	if err != nil {
		t.Fatalf("DownloadFileWithResume returned error: %v", err)
	}
	data, err := os.ReadFile(fileOut)
	if err != nil {
		t.Fatalf("os.ReadFile returned error: %v", err)
	}
	if string(data) != string(body) {
		t.Fatalf("file content = %q, want %q", string(data), string(body))
	}
	if _, err := os.Stat(fileOut + ".parts"); !os.IsNotExist(err) {
		t.Fatalf("completed chunk workspace exists or stat failed: %v", err)
	}
}

func TestDownloadFileWithResumeReusesCompletedChunks(t *testing.T) {
	withTestChunkConfig(t, 10, 5, 2)
	body := []byte("abcdefghijklmnopqrstuvwxyz")
	dirTemp := t.TempDir()
	fileOut := filepath.Join(dirTemp, "asset.part")
	dirParts := fileOut + ".parts"
	if err := os.MkdirAll(dirParts, 0o755); err != nil {
		t.Fatalf("os.MkdirAll returned error: %v", err)
	}
	state := chunkState{
		URL:           "placeholder",
		ContentLength: int64(len(body)),
		ChunkSize:     chunkSizeBytes,
		Chunks: []chunkRange{
			{Index: 0, Start: 0, End: 4, Size: 5},
			{Index: 1, Start: 5, End: 25, Size: 21},
		},
	}
	if err := os.WriteFile(chunkFilePath(dirParts, state.Chunks[0]), body[:5], 0o644); err != nil {
		t.Fatalf("os.WriteFile returned error: %v", err)
	}

	countGet := 0
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.Method {
		case http.MethodHead:
			writer.Header().Set("Accept-Ranges", "bytes")
			writer.Header().Set("Content-Length", fmt.Sprintf("%d", len(body)))
		case http.MethodGet:
			countGet++
			start, end := parseTestRange(t, request.Header.Get("Range"))
			if start != 5 || end != 25 {
				t.Fatalf("Range = %q, want bytes=5-25", request.Header.Get("Range"))
			}
			writer.WriteHeader(http.StatusPartialContent)
			_, _ = writer.Write(body[start : end+1])
		default:
			t.Fatalf("method = %s", request.Method)
		}
	}))
	defer server.Close()
	state.URL = server.URL + "/asset"
	if err := writeChunkState(filepath.Join(dirParts, "state.json"), state); err != nil {
		t.Fatalf("writeChunkState returned error: %v", err)
	}

	err := DownloadFileWithResume(server.Client(), server.URL+"/asset", fileOut, nil)
	if err != nil {
		t.Fatalf("DownloadFileWithResume returned error: %v", err)
	}
	if countGet != 1 {
		t.Fatalf("countGet = %d, want 1", countGet)
	}
	infoFile, err := os.Stat(fileOut)
	if err != nil {
		t.Fatalf("fileOut missing: %v", err)
	}
	if infoFile.Size() != int64(len(body)) {
		t.Fatalf("file size = %d, want %d", infoFile.Size(), len(body))
	}
	if _, err := os.Stat(dirParts); !os.IsNotExist(err) {
		t.Fatalf("completed chunk workspace exists or stat failed: %v", err)
	}
}

func TestDownloadFileWithResumeKeepsExistingSinglePart(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.Method {
		case http.MethodHead:
			writer.Header().Set("Accept-Ranges", "bytes")
			writer.Header().Set("Content-Length", fmt.Sprintf("%d", chunkedDownloadMinBytes+1))
		case http.MethodGet:
			if got := request.Header.Get("Range"); got != "bytes=5-" {
				t.Fatalf("Range = %q, want bytes=5-", got)
			}
			writer.WriteHeader(http.StatusPartialContent)
			_, _ = writer.Write([]byte("bravo"))
		default:
			t.Fatalf("method = %s", request.Method)
		}
	}))
	defer server.Close()

	fileOut := filepath.Join(t.TempDir(), "asset.part")
	if err := os.WriteFile(fileOut, []byte("alpha"), 0o644); err != nil {
		t.Fatalf("os.WriteFile returned error: %v", err)
	}
	err := DownloadFileWithResume(server.Client(), server.URL+"/asset", fileOut, nil)
	if err != nil {
		t.Fatalf("DownloadFileWithResume returned error: %v", err)
	}
	data, err := os.ReadFile(fileOut)
	if err != nil {
		t.Fatalf("os.ReadFile returned error: %v", err)
	}
	if string(data) != "alphabravo" {
		t.Fatalf("file content = %q, want alphabravo", string(data))
	}
	if _, err := os.Stat(fileOut + ".parts"); !os.IsNotExist(err) {
		t.Fatalf("parts dir exists or stat returned unexpected error: %v", err)
	}
}

func parseTestRange(t *testing.T, value string) (int64, int64) {
	t.Helper()
	if !strings.HasPrefix(value, "bytes=") {
		t.Fatalf("Range = %q", value)
	}
	parts := strings.Split(strings.TrimPrefix(value, "bytes="), "-")
	if len(parts) != 2 {
		t.Fatalf("Range = %q", value)
	}
	start, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		t.Fatalf("parse start: %v", err)
	}
	end, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		t.Fatalf("parse end: %v", err)
	}
	if end >= int64(len("abcdefghijklmnopqrstuvwxyz")) {
		end = int64(len("abcdefghijklmnopqrstuvwxyz")) - 1
	}
	return start, end
}

func withTestChunkConfig(t *testing.T, minBytes int64, chunkBytes int64, workers int) {
	t.Helper()
	originalMinBytes := chunkedDownloadMinBytes
	originalChunkBytes := chunkSizeBytes
	originalWorkers := chunkWorkersMax
	chunkedDownloadMinBytes = minBytes
	chunkSizeBytes = chunkBytes
	chunkWorkersMax = workers
	t.Cleanup(func() {
		chunkedDownloadMinBytes = originalMinBytes
		chunkSizeBytes = originalChunkBytes
		chunkWorkersMax = originalWorkers
	})
}
