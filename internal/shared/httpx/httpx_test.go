package httpx

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
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
