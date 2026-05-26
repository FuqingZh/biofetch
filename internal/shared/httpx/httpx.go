package httpx

import (
	"crypto/tls"
	"fmt"
	"io"
	"net/http"
	"os"
	"sync"
	"time"
)

type DownloadProgressFunc func(bytesDone int64, bytesTotal int64)

type RequestLimiter struct {
	interval        time.Duration
	mutexLimiter    sync.Mutex
	timeLastRequest time.Time
}

func NewClient(shouldAllowInsecureTLS bool) *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	if shouldAllowInsecureTLS {
		transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
	}

	return &http.Client{
		Timeout:   0,
		Transport: transport,
	}
}

func NewRequestLimiter(requestInterval time.Duration) *RequestLimiter {
	return &RequestLimiter{interval: requestInterval}
}

func (limiter *RequestLimiter) Wait() {
	if limiter == nil || limiter.interval <= 0 {
		return
	}

	limiter.mutexLimiter.Lock()
	defer limiter.mutexLimiter.Unlock()

	if !limiter.timeLastRequest.IsZero() {
		wait := limiter.interval - time.Since(limiter.timeLastRequest)
		if wait > 0 {
			time.Sleep(wait)
		}
	}
	limiter.timeLastRequest = time.Now()
}

func DownloadFile(clientHTTP *http.Client, urlFile string, fileOut string) error {
	return DownloadFileWithProgress(clientHTTP, urlFile, fileOut, nil)
}

func DownloadFileWithProgress(clientHTTP *http.Client, urlFile string, fileOut string, progress DownloadProgressFunc) error {
	response, err := clientHTTP.Get(urlFile)
	if err != nil {
		return fmt.Errorf("request %s: %w", urlFile, err)
	}
	defer response.Body.Close()

	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("request %s: unexpected status %s", urlFile, response.Status)
	}

	fileHandle, err := os.Create(fileOut)
	if err != nil {
		return fmt.Errorf("create %s: %w", fileOut, err)
	}

	var reader io.Reader = response.Body
	if progress != nil {
		reader = &progressReader{
			reader:     response.Body,
			bytesTotal: response.ContentLength,
			progress:   progress,
		}
		progress(0, response.ContentLength)
	}
	_, errCopy := io.Copy(fileHandle, reader)
	errClose := fileHandle.Close()
	if errCopy != nil {
		return fmt.Errorf("write %s: %w", fileOut, errCopy)
	}
	if errClose != nil {
		return fmt.Errorf("close %s: %w", fileOut, errClose)
	}
	return nil
}

type progressReader struct {
	reader     io.Reader
	bytesDone  int64
	bytesTotal int64
	progress   DownloadProgressFunc
}

func (reader *progressReader) Read(buffer []byte) (int, error) {
	count, err := reader.reader.Read(buffer)
	if count > 0 {
		reader.bytesDone += int64(count)
		reader.progress(reader.bytesDone, reader.bytesTotal)
	}
	return count, err
}
