package kegg

import (
	"io"
	"net/http"
	"strings"
	"syscall"
	"testing"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return fn(request)
}

type flakyReadCloser struct {
	data   []byte
	err    error
	served bool
}

func (reader *flakyReadCloser) Read(buffer []byte) (int, error) {
	if !reader.served {
		reader.served = true
		if len(reader.data) == 0 {
			return 0, reader.err
		}
		return copy(buffer, reader.data), nil
	}
	if reader.err != nil {
		return 0, reader.err
	}
	return 0, io.EOF
}

func (reader *flakyReadCloser) Close() error {
	return nil
}

func TestKEGGClientDownloadRetriesRequestError(t *testing.T) {
	attempts := 0
	clientHTTP := &http.Client{
		Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
			attempts++
			if attempts == 1 {
				return nil, syscall.ECONNRESET
			}
			return &http.Response{
				StatusCode: http.StatusOK,
				Status:     "200 OK",
				Body:       io.NopCloser(strings.NewReader("ok")),
			}, nil
		}),
	}

	clientKegg := createKEGGClient(clientHTTP, 0, 2, 0)
	data, err := clientKegg.download("https://example.test/request")
	if err != nil {
		t.Fatalf("download returned error: %v", err)
	}
	if string(data) != "ok" {
		t.Fatalf("download data = %q, want %q", string(data), "ok")
	}
	if attempts != 2 {
		t.Fatalf("download attempts = %d, want %d", attempts, 2)
	}
}

func TestKEGGClientDownloadRetriesReadError(t *testing.T) {
	attempts := 0
	clientHTTP := &http.Client{
		Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
			attempts++
			if attempts == 1 {
				return &http.Response{
					StatusCode: http.StatusOK,
					Status:     "200 OK",
					Body: &flakyReadCloser{
						data: []byte("partial"),
						err:  syscall.ECONNRESET,
					},
				}, nil
			}
			return &http.Response{
				StatusCode: http.StatusOK,
				Status:     "200 OK",
				Body:       io.NopCloser(strings.NewReader("complete")),
			}, nil
		}),
	}

	clientKegg := createKEGGClient(clientHTTP, 0, 2, 0)
	data, err := clientKegg.download("https://example.test/read")
	if err != nil {
		t.Fatalf("download returned error: %v", err)
	}
	if string(data) != "complete" {
		t.Fatalf("download data = %q, want %q", string(data), "complete")
	}
	if attempts != 2 {
		t.Fatalf("download attempts = %d, want %d", attempts, 2)
	}
}

func TestKEGGClientDownloadRetriesRetryableStatus(t *testing.T) {
	attempts := 0
	clientHTTP := &http.Client{
		Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
			attempts++
			if attempts == 1 {
				return &http.Response{
					StatusCode: http.StatusServiceUnavailable,
					Status:     "503 Service Unavailable",
					Body:       io.NopCloser(strings.NewReader("busy")),
				}, nil
			}
			return &http.Response{
				StatusCode: http.StatusOK,
				Status:     "200 OK",
				Body:       io.NopCloser(strings.NewReader("ok")),
			}, nil
		}),
	}

	clientKegg := createKEGGClient(clientHTTP, 0, 2, 0)
	data, err := clientKegg.download("https://example.test/status")
	if err != nil {
		t.Fatalf("download returned error: %v", err)
	}
	if string(data) != "ok" {
		t.Fatalf("download data = %q, want %q", string(data), "ok")
	}
	if attempts != 2 {
		t.Fatalf("download attempts = %d, want %d", attempts, 2)
	}
}
