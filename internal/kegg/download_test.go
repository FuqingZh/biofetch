package kegg

import (
	"context"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
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

func TestKEGGClientDefaultFileDownloadFallsBackAfterHEADTransportError(t *testing.T) {
	for _, test := range []struct {
		name string
		err  error
	}{
		{name: "EOF", err: io.EOF},
		{name: "timeout", err: context.DeadlineExceeded},
	} {
		t.Run(test.name, func(t *testing.T) {
			methods := make([]string, 0, 2)
			clientHTTP := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
				methods = append(methods, request.Method)
				if request.Method == http.MethodHead {
					return nil, test.err
				}
				return &http.Response{StatusCode: http.StatusOK, Status: "200 OK", Body: io.NopCloser(strings.NewReader("ok")), Request: request}, nil
			})}
			fileOut := filepath.Join(t.TempDir(), "asset.dat")
			client := createKEGGClient(clientHTTP, 0, 1, 0)
			if err := client.downloadFile("https://example.test/asset", fileOut); err != nil {
				t.Fatalf("default file download failed: %v", err)
			}
			if want := []string{http.MethodHead, http.MethodGet}; !reflect.DeepEqual(methods, want) {
				t.Fatalf("request methods = %#v, want %#v", methods, want)
			}
			data, err := os.ReadFile(fileOut)
			if err != nil || string(data) != "ok" {
				t.Fatalf("downloaded data = %q, err %v", data, err)
			}
		})
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
	tests := []struct {
		name   string
		status int
		text   string
	}{
		{name: "service unavailable", status: http.StatusServiceUnavailable, text: "503 Service Unavailable"},
		{name: "forbidden", status: http.StatusForbidden, text: "403 Forbidden"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			attempts := 0
			clientHTTP := &http.Client{
				Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
					attempts++
					if attempts == 1 {
						return &http.Response{
							StatusCode: tc.status,
							Status:     tc.text,
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
		})
	}
}
