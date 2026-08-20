package httpx

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
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

func TestDownloadFileWithResumeCanPropagateProbeError(t *testing.T) {
	methods := make([]string, 0, 1)
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		methods = append(methods, request.Method)
		if request.Method == http.MethodHead {
			return nil, context.DeadlineExceeded
		}
		return &http.Response{StatusCode: http.StatusOK, Status: "200 OK", Body: io.NopCloser(strings.NewReader("unexpected")), Request: request}, nil
	})}
	err := DownloadFileWithResumeOptions(client, "https://example.test/asset", filepath.Join(t.TempDir(), "asset.part"), nil, DownloadOptions{PropagateProbeErrors: true})
	if err == nil || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("strict probe error = %v, want deadline exceeded", err)
	}
	if want := []string{http.MethodHead}; !slices.Equal(methods, want) {
		t.Fatalf("request methods = %#v, want %#v", methods, want)
	}
}

func TestDownloadFileWithResumeMetadataProbePolicy(t *testing.T) {
	for _, test := range []struct {
		name    string
		options DownloadOptions
		want    []string
	}{
		{name: "default probes", want: []string{http.MethodHead, http.MethodGet}},
		{name: "skip probe", options: DownloadOptions{SkipMetadataProbe: true}, want: []string{http.MethodGet}},
	} {
		t.Run(test.name, func(t *testing.T) {
			methods := make([]string, 0, 2)
			client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
				methods = append(methods, request.Method)
				return &http.Response{
					StatusCode: http.StatusOK,
					Status:     "200 OK",
					Header:     make(http.Header),
					Body:       io.NopCloser(strings.NewReader("ok")),
					Request:    request,
				}, nil
			})}
			err := DownloadFileWithResumeOptions(client, "https://example.test/asset", filepath.Join(t.TempDir(), "asset.part"), nil, test.options)
			if err != nil {
				t.Fatalf("DownloadFileWithResumeOptions returned error: %v", err)
			}
			if !slices.Equal(methods, test.want) {
				t.Fatalf("request methods = %#v, want %#v", methods, test.want)
			}
		})
	}
}

func TestDownloadFileWithResumeAppendsPartialContent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method == http.MethodHead {
			writer.Header().Set("Content-Length", "10")
			writer.Header().Set("ETag", `"generation-1"`)
			return
		}
		if got := request.Header.Get("Range"); got != "bytes=5-" {
			t.Fatalf("Range = %q, want bytes=5-", got)
		}
		if got := request.Header.Get("If-Range"); got != `"generation-1"` {
			t.Fatalf("If-Range = %q, want generation ETag", got)
		}
		writer.Header().Set("Content-Range", "bytes 5-9/10")
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

func TestDownloadFileWithResumeProbeAuthFailsOnce(t *testing.T) {
	for _, challenge := range []string{"", "challenge"} {
		calls := 0
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			calls++
			if challenge != "" {
				w.Header().Set("Cf-Mitigated", challenge)
			}
			w.WriteHeader(http.StatusForbidden)
		}))
		file := filepath.Join(t.TempDir(), "asset")
		err := DownloadFileWithResume(server.Client(), server.URL, file, nil)
		server.Close()
		if err == nil {
			t.Fatal("expected authorization error")
		}
		if calls != 1 {
			t.Fatalf("calls = %d, want 1", calls)
		}
		var status UnexpectedStatusError
		if !errors.As(err, &status) || status.Code != http.StatusForbidden {
			t.Fatalf("error = %v", err)
		}
	}
}

func TestDownloadFileWithResumeProbeFallbacks(t *testing.T) {
	for _, headStatus := range []int{http.StatusMethodNotAllowed, http.StatusInternalServerError} {
		calls := 0
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			calls++
			if r.Method == http.MethodHead {
				w.WriteHeader(headStatus)
				return
			}
			_, _ = w.Write([]byte("ok"))
		}))
		file := filepath.Join(t.TempDir(), "asset")
		if err := DownloadFileWithResume(server.Client(), server.URL, file, nil); err != nil {
			t.Fatalf("HEAD %d: %v", headStatus, err)
		}
		server.Close()
		if calls != 2 {
			t.Fatalf("HEAD %d calls = %d, want 2", headStatus, calls)
		}
	}
}

func TestDownloadFileWithResumeRestartsWhenRangeUnsupported(t *testing.T) {
	getCalls := 0
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method == http.MethodHead {
			writer.Header().Set("Content-Length", "11")
			return
		}
		getCalls++
		if getCalls == 1 {
			if got := request.Header.Get("Range"); got != "bytes=5-" {
				t.Fatalf("Range = %q, want bytes=5-", got)
			}
			_, _ = writer.Write([]byte("ignored-range-body"))
			return
		}
		if got := request.Header.Get("Range"); got != "" {
			t.Fatalf("clean restart Range = %q, want empty", got)
		}
		_, _ = writer.Write([]byte("replacement"))
	}))
	defer server.Close()

	fileOut := filepath.Join(t.TempDir(), "asset.part")
	if err := os.WriteFile(fileOut, []byte("alpha"), 0o644); err != nil {
		t.Fatalf("os.WriteFile returned error: %v", err)
	}
	err := DownloadFileWithResume(server.Client(), server.URL+"/asset", fileOut, nil)
	var ignored RangeIgnoredError
	if !errors.As(err, &ignored) {
		t.Fatalf("first DownloadFileWithResume error = %v, want RangeIgnoredError", err)
	}
	if err := DownloadFileWithResume(server.Client(), server.URL+"/asset", fileOut, nil); err != nil {
		t.Fatalf("clean DownloadFileWithResume returned error: %v", err)
	}
	data, err := os.ReadFile(fileOut)
	if err != nil {
		t.Fatalf("os.ReadFile returned error: %v", err)
	}
	if string(data) != "replacement" {
		t.Fatalf("file content = %q, want replacement", string(data))
	}
	if getCalls != 2 {
		t.Fatalf("GET calls = %d, want ignored Range plus clean restart", getCalls)
	}
}

func TestDownloadFileWithResumeClosesIgnoredRangeWithoutReadingBody(t *testing.T) {
	ignored := &countingReadCloser{Reader: bytes.NewReader(bytes.Repeat([]byte("x"), 1<<20))}
	calls := 0
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		calls++
		response := &http.Response{StatusCode: http.StatusOK, Status: "200 OK", Header: make(http.Header), Request: request}
		switch calls {
		case 1:
			response.Header.Set("Content-Length", "11")
			response.ContentLength = 11
			response.Body = io.NopCloser(bytes.NewReader(nil))
		case 2:
			if request.Header.Get("Range") != "bytes=5-" {
				t.Fatalf("Range = %q", request.Header.Get("Range"))
			}
			response.Body = ignored
		case 3:
			response.Header.Set("Content-Length", "11")
			response.ContentLength = 11
			response.Body = io.NopCloser(bytes.NewReader(nil))
		case 4:
			if request.Header.Get("Range") != "" {
				t.Fatalf("restart Range = %q", request.Header.Get("Range"))
			}
			response.ContentLength = 11
			response.Body = io.NopCloser(strings.NewReader("replacement"))
		default:
			t.Fatalf("unexpected request %d", calls)
		}
		return response, nil
	})}

	fileOut := filepath.Join(t.TempDir(), "asset.part")
	if err := os.WriteFile(fileOut, []byte("alpha"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := DownloadFileWithResume(client, "https://example.test/asset", fileOut, nil)
	var rangeIgnored RangeIgnoredError
	if !errors.As(err, &rangeIgnored) {
		t.Fatalf("first error = %v, want RangeIgnoredError", err)
	}
	if err := DownloadFileWithResume(client, "https://example.test/asset", fileOut, nil); err != nil {
		t.Fatalf("clean retry: %v", err)
	}
	if ignored.bytesRead != 0 || !ignored.closed {
		t.Fatalf("ignored body bytesRead=%d closed=%v, want 0/true", ignored.bytesRead, ignored.closed)
	}
}

func TestDownloadFileWithResumeRejectsInvalidContentRangeBeforeWrite(t *testing.T) {
	for _, value := range []string{"", "bytes 4-9/10", "bytes 5-8/10", "bytes 5-9/11", "items 5-9/10"} {
		t.Run(strings.ReplaceAll(value, "/", "_"), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				if request.Method == http.MethodHead {
					writer.Header().Set("Content-Length", "10")
					return
				}
				writer.Header().Set("Content-Range", value)
				writer.WriteHeader(http.StatusPartialContent)
				_, _ = writer.Write([]byte("bravo"))
			}))
			defer server.Close()

			fileOut := filepath.Join(t.TempDir(), "asset.part")
			if err := os.WriteFile(fileOut, []byte("alpha"), 0o644); err != nil {
				t.Fatal(err)
			}
			err := DownloadFileWithResume(server.Client(), server.URL, fileOut, nil)
			if err == nil || !strings.Contains(err.Error(), "Content-Range") {
				t.Fatalf("error = %v", err)
			}
			data, readErr := os.ReadFile(fileOut)
			if readErr != nil || string(data) != "alpha" {
				t.Fatalf("partial changed: %q, %v", data, readErr)
			}
		})
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
			writer.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, len(body)))
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
			writer.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, len(body)))
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

func TestDownloadFileWithResumeResetsChunksWhenRemoteIdentityChanges(t *testing.T) {
	withTestChunkConfig(t, 10, 5, 1)
	body := []byte("abcdefghijklmnopqrstuvwxyz")
	fileOut := filepath.Join(t.TempDir(), "asset.part")
	dirParts := fileOut + ".parts"
	if err := os.MkdirAll(dirParts, 0o755); err != nil {
		t.Fatal(err)
	}
	old := chunkState{
		URL: "placeholder", ContentLength: int64(len(body)), ChunkSize: chunkSizeBytes, ETag: `"old"`,
		Chunks: buildChunkRanges(int64(len(body)), chunkSizeBytes),
	}
	if err := os.WriteFile(chunkFilePath(dirParts, old.Chunks[0]), []byte("stale"), 0o644); err != nil {
		t.Fatal(err)
	}

	gets := 0
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method == http.MethodHead {
			writer.Header().Set("Accept-Ranges", "bytes")
			writer.Header().Set("Content-Length", fmt.Sprint(len(body)))
			writer.Header().Set("ETag", `"new"`)
			return
		}
		gets++
		if request.Header.Get("If-Range") != `"new"` {
			t.Fatalf("If-Range = %q", request.Header.Get("If-Range"))
		}
		start, end := parseTestRange(t, request.Header.Get("Range"))
		writer.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, len(body)))
		writer.WriteHeader(http.StatusPartialContent)
		_, _ = writer.Write(body[start : end+1])
	}))
	defer server.Close()
	old.URL = server.URL
	if err := writeChunkState(filepath.Join(dirParts, "state.json"), old); err != nil {
		t.Fatal(err)
	}

	if err := DownloadFileWithResume(server.Client(), server.URL, fileOut, nil); err != nil {
		t.Fatal(err)
	}
	if gets != len(buildChunkRanges(int64(len(body)), chunkSizeBytes)) {
		t.Fatalf("GETs = %d, stale chunk was reused", gets)
	}
}

func TestDownloadFileWithResumeLimitsEveryChunkRequest(t *testing.T) {
	withTestChunkConfig(t, 10, 13, 1)
	body := []byte("abcdefghijklmnopqrstuvwxyz")
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method == http.MethodHead {
			writer.Header().Set("Accept-Ranges", "bytes")
			writer.Header().Set("Content-Length", fmt.Sprint(len(body)))
			return
		}
		start, end := parseTestRange(t, request.Header.Get("Range"))
		writer.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, len(body)))
		writer.WriteHeader(http.StatusPartialContent)
		_, _ = writer.Write(body[start : end+1])
	}))
	defer server.Close()

	started := time.Now()
	err := DownloadFileWithResumeOptions(server.Client(), server.URL, filepath.Join(t.TempDir(), "asset.part"), nil, DownloadOptions{
		Limiter: NewRequestLimiter(20 * time.Millisecond),
	})
	if err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(started); elapsed < 35*time.Millisecond {
		t.Fatalf("elapsed = %s, want limiter spacing across HEAD and two chunks", elapsed)
	}
}

func TestRetryAfterParsesDeltaAndHTTPDate(t *testing.T) {
	now := time.Date(2026, 8, 3, 0, 0, 0, 0, time.UTC)
	if got := retryAfterDuration("7", now); got != 7*time.Second {
		t.Fatalf("delta Retry-After = %s", got)
	}
	if got := retryAfterDuration(now.Add(9*time.Second).Format(http.TimeFormat), now); got != 9*time.Second {
		t.Fatalf("date Retry-After = %s", got)
	}
	if got := retryAfterDuration("invalid", now); got != 0 {
		t.Fatalf("invalid Retry-After = %s", got)
	}
}

func TestDownloadFileWithResumeKeepsExistingSinglePart(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.Method {
		case http.MethodHead:
			writer.Header().Set("Accept-Ranges", "bytes")
			writer.Header().Set("Content-Length", "10")
		case http.MethodGet:
			if got := request.Header.Get("Range"); got != "bytes=5-" {
				t.Fatalf("Range = %q, want bytes=5-", got)
			}
			writer.Header().Set("Content-Range", "bytes 5-9/10")
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

type roundTripFunc func(*http.Request) (*http.Response, error)

func (function roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}

type countingReadCloser struct {
	io.Reader
	bytesRead int
	closed    bool
}

func (body *countingReadCloser) Read(buffer []byte) (int, error) {
	count, err := body.Reader.Read(buffer)
	body.bytesRead += count
	return count, err
}

func (body *countingReadCloser) Close() error {
	body.closed = true
	return nil
}
