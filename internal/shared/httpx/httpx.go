package httpx

import (
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type DownloadProgressFunc func(bytesDone int64, bytesTotal int64)

var chunkedDownloadMinBytes int64 = 1 << 30
var chunkSizeBytes int64 = 256 << 20
var chunkWorkersMax = 4

type UnexpectedStatusError struct {
	URL    string
	Status string
	Code   int
}

func (err UnexpectedStatusError) Error() string {
	return fmt.Sprintf("request %s: unexpected status %s", err.URL, err.Status)
}

func IsUnexpectedStatus(err error, code int) bool {
	var statusErr UnexpectedStatusError
	if !errors.As(err, &statusErr) {
		return false
	}
	return statusErr.Code == code
}

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
		return UnexpectedStatusError{URL: urlFile, Status: response.Status, Code: response.StatusCode}
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

func DownloadFileWithResume(clientHTTP *http.Client, urlFile string, fileOut string, progress DownloadProgressFunc) error {
	if existingFileSize(fileOut) > 0 {
		return downloadFileSingleResume(clientHTTP, urlFile, fileOut, progress)
	}
	metadata, ok := probeDownloadMetadata(clientHTTP, urlFile)
	if ok && metadata.SupportsRange && metadata.ContentLength >= chunkedDownloadMinBytes {
		return downloadFileChunked(clientHTTP, urlFile, fileOut, metadata.ContentLength, progress)
	}
	return downloadFileSingleResume(clientHTTP, urlFile, fileOut, progress)
}

func downloadFileSingleResume(clientHTTP *http.Client, urlFile string, fileOut string, progress DownloadProgressFunc) error {
	bytesExisting := existingFileSize(fileOut)
	request, err := http.NewRequest(http.MethodGet, urlFile, nil)
	if err != nil {
		return fmt.Errorf("create request %s: %w", urlFile, err)
	}
	if bytesExisting > 0 {
		request.Header.Set("Range", fmt.Sprintf("bytes=%d-", bytesExisting))
	}

	response, err := clientHTTP.Do(request)
	if err != nil {
		return fmt.Errorf("request %s: %w", urlFile, err)
	}
	defer response.Body.Close()

	shouldAppend := false
	bytesDoneStart := int64(0)
	bytesTotal := response.ContentLength
	switch {
	case bytesExisting > 0 && response.StatusCode == http.StatusPartialContent:
		shouldAppend = true
		bytesDoneStart = bytesExisting
		if response.ContentLength >= 0 {
			bytesTotal = bytesExisting + response.ContentLength
		}
	case response.StatusCode >= 200 && response.StatusCode < 300:
		shouldAppend = false
	default:
		return UnexpectedStatusError{URL: urlFile, Status: response.Status, Code: response.StatusCode}
	}

	flagFile := os.O_CREATE | os.O_WRONLY
	if shouldAppend {
		flagFile |= os.O_APPEND
	} else {
		flagFile |= os.O_TRUNC
	}
	fileHandle, err := os.OpenFile(fileOut, flagFile, 0o644)
	if err != nil {
		return fmt.Errorf("open %s: %w", fileOut, err)
	}

	var reader io.Reader = response.Body
	if progress != nil {
		reader = &progressReader{
			reader:     response.Body,
			bytesDone:  bytesDoneStart,
			bytesTotal: bytesTotal,
			progress:   progress,
		}
		progress(bytesDoneStart, bytesTotal)
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

type downloadMetadata struct {
	ContentLength int64
	SupportsRange bool
}

func probeDownloadMetadata(clientHTTP *http.Client, urlFile string) (downloadMetadata, bool) {
	request, err := http.NewRequest(http.MethodHead, urlFile, nil)
	if err != nil {
		return downloadMetadata{}, false
	}
	response, err := clientHTTP.Do(request)
	if err != nil {
		return downloadMetadata{}, false
	}
	_ = response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 || response.ContentLength <= 0 {
		return downloadMetadata{}, false
	}
	metadata := downloadMetadata{
		ContentLength: response.ContentLength,
		SupportsRange: strings.EqualFold(strings.TrimSpace(response.Header.Get("Accept-Ranges")), "bytes"),
	}
	if metadata.SupportsRange {
		return metadata, true
	}
	if metadata.ContentLength < chunkedDownloadMinBytes {
		return metadata, true
	}
	metadata.SupportsRange = probeRangeSupport(clientHTTP, urlFile)
	return metadata, true
}

func probeRangeSupport(clientHTTP *http.Client, urlFile string) bool {
	request, err := http.NewRequest(http.MethodGet, urlFile, nil)
	if err != nil {
		return false
	}
	request.Header.Set("Range", "bytes=0-0")
	response, err := clientHTTP.Do(request)
	if err != nil {
		return false
	}
	_, _ = io.Copy(io.Discard, response.Body)
	_ = response.Body.Close()
	return response.StatusCode == http.StatusPartialContent
}

type chunkState struct {
	URL           string       `json:"url"`
	ContentLength int64        `json:"content_length"`
	ChunkSize     int64        `json:"chunk_size"`
	Chunks        []chunkRange `json:"chunks"`
}

type chunkRange struct {
	Index int   `json:"index"`
	Start int64 `json:"start"`
	End   int64 `json:"end"`
	Size  int64 `json:"size"`
	Done  bool  `json:"done"`
}

func downloadFileChunked(clientHTTP *http.Client, urlFile string, fileOut string, contentLength int64, progress DownloadProgressFunc) error {
	dirParts := fileOut + ".parts"
	if err := os.MkdirAll(dirParts, 0o755); err != nil {
		return fmt.Errorf("create chunk dir %s: %w", dirParts, err)
	}
	state, err := loadOrCreateChunkState(filepath.Join(dirParts, "state.json"), urlFile, contentLength, chunkSizeBytes)
	if err != nil {
		return err
	}
	if err := writeChunkState(filepath.Join(dirParts, "state.json"), state); err != nil {
		return err
	}

	progressChunk := newChunkProgress(state, dirParts, progress)
	if progress != nil {
		progress(progressChunk.bytesDone(), contentLength)
	}
	channelTasks := make(chan chunkRange)
	channelErrors := make(chan error, len(state.Chunks))
	var group sync.WaitGroup
	workers := chunkWorkersMax
	if workers > len(state.Chunks) {
		workers = len(state.Chunks)
	}
	if workers < 1 {
		workers = 1
	}
	for range workers {
		group.Add(1)
		go func() {
			defer group.Done()
			for chunk := range channelTasks {
				if isChunkComplete(dirParts, chunk) {
					progressChunk.markDone(chunk)
					continue
				}
				if err := downloadChunk(clientHTTP, urlFile, dirParts, chunk, progressChunk); err != nil {
					channelErrors <- err
					continue
				}
				progressChunk.markDone(chunk)
			}
		}()
	}
	for _, chunk := range state.Chunks {
		channelTasks <- chunk
	}
	close(channelTasks)
	group.Wait()
	close(channelErrors)
	for err := range channelErrors {
		if err != nil {
			return err
		}
	}
	for index := range state.Chunks {
		state.Chunks[index].Done = isChunkComplete(dirParts, state.Chunks[index])
		if !state.Chunks[index].Done {
			return fmt.Errorf("chunk %d incomplete for %s", state.Chunks[index].Index, urlFile)
		}
	}
	if err := writeChunkState(filepath.Join(dirParts, "state.json"), state); err != nil {
		return err
	}
	if err := mergeChunks(dirParts, fileOut, state.Chunks); err != nil {
		return err
	}
	if err := os.RemoveAll(dirParts); err != nil {
		_ = os.Remove(fileOut)
		return fmt.Errorf("remove completed chunk workspace %s: %w", dirParts, err)
	}
	return nil
}

func loadOrCreateChunkState(fileState string, urlFile string, contentLength int64, chunkSize int64) (chunkState, error) {
	data, err := os.ReadFile(fileState)
	if err == nil {
		var state chunkState
		if err := json.Unmarshal(data, &state); err != nil {
			return chunkState{}, fmt.Errorf("read chunk state %s: %w", fileState, err)
		}
		if state.URL == urlFile && state.ContentLength == contentLength && state.ChunkSize == chunkSize {
			return state, nil
		}
	}
	if err != nil && !os.IsNotExist(err) {
		return chunkState{}, fmt.Errorf("read chunk state %s: %w", fileState, err)
	}
	return chunkState{
		URL:           urlFile,
		ContentLength: contentLength,
		ChunkSize:     chunkSize,
		Chunks:        buildChunkRanges(contentLength, chunkSize),
	}, nil
}

func buildChunkRanges(contentLength int64, chunkSize int64) []chunkRange {
	chunks := make([]chunkRange, 0, int(contentLength/chunkSize)+1)
	for start := int64(0); start < contentLength; start += chunkSize {
		end := start + chunkSize - 1
		if end >= contentLength {
			end = contentLength - 1
		}
		chunks = append(chunks, chunkRange{
			Index: len(chunks),
			Start: start,
			End:   end,
			Size:  end - start + 1,
		})
	}
	return chunks
}

func writeChunkState(fileState string, state chunkState) error {
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("encode chunk state: %w", err)
	}
	if err := os.WriteFile(fileState, data, 0o644); err != nil {
		return fmt.Errorf("write chunk state %s: %w", fileState, err)
	}
	return nil
}

func downloadChunk(clientHTTP *http.Client, urlFile string, dirParts string, chunk chunkRange, progress *chunkProgress) error {
	fileChunk := chunkFilePath(dirParts, chunk)
	_ = os.Remove(fileChunk)
	request, err := http.NewRequest(http.MethodGet, urlFile, nil)
	if err != nil {
		return fmt.Errorf("create request %s: %w", urlFile, err)
	}
	request.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", chunk.Start, chunk.End))
	response, err := clientHTTP.Do(request)
	if err != nil {
		return fmt.Errorf("request %s: %w", urlFile, err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusPartialContent {
		return UnexpectedStatusError{URL: urlFile, Status: response.Status, Code: response.StatusCode}
	}
	fileHandle, err := os.OpenFile(fileChunk, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("open chunk %s: %w", fileChunk, err)
	}
	reader := &chunkProgressReader{reader: response.Body, chunk: chunk, progress: progress}
	_, errCopy := io.Copy(fileHandle, reader)
	errClose := fileHandle.Close()
	if errCopy != nil {
		return fmt.Errorf("write chunk %s: %w", fileChunk, errCopy)
	}
	if errClose != nil {
		return fmt.Errorf("close chunk %s: %w", fileChunk, errClose)
	}
	if !isChunkComplete(dirParts, chunk) {
		return fmt.Errorf("chunk %d size mismatch", chunk.Index)
	}
	return nil
}

func chunkFilePath(dirParts string, chunk chunkRange) string {
	return filepath.Join(dirParts, fmt.Sprintf("%06d.part", chunk.Index))
}

func isChunkComplete(dirParts string, chunk chunkRange) bool {
	infoFile, err := os.Stat(chunkFilePath(dirParts, chunk))
	return err == nil && !infoFile.IsDir() && infoFile.Size() == chunk.Size
}

func mergeChunks(dirParts string, fileOut string, chunks []chunkRange) error {
	fileHandle, err := os.OpenFile(fileOut, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("open merged part %s: %w", fileOut, err)
	}
	for _, chunk := range chunks {
		fileChunk, err := os.Open(chunkFilePath(dirParts, chunk))
		if err != nil {
			_ = fileHandle.Close()
			return fmt.Errorf("open chunk %d: %w", chunk.Index, err)
		}
		_, errCopy := io.Copy(fileHandle, fileChunk)
		errCloseChunk := fileChunk.Close()
		if errCopy != nil {
			_ = fileHandle.Close()
			return fmt.Errorf("merge chunk %d: %w", chunk.Index, errCopy)
		}
		if errCloseChunk != nil {
			_ = fileHandle.Close()
			return fmt.Errorf("close chunk %d: %w", chunk.Index, errCloseChunk)
		}
	}
	if err := fileHandle.Close(); err != nil {
		return fmt.Errorf("close merged part %s: %w", fileOut, err)
	}
	return nil
}

type chunkProgress struct {
	mutex        sync.Mutex
	bytesByChunk map[int]int64
	bytesTotal   int64
	progress     DownloadProgressFunc
}

func newChunkProgress(state chunkState, dirParts string, progress DownloadProgressFunc) *chunkProgress {
	tracker := &chunkProgress{
		bytesByChunk: make(map[int]int64, len(state.Chunks)),
		bytesTotal:   state.ContentLength,
		progress:     progress,
	}
	for _, chunk := range state.Chunks {
		if isChunkComplete(dirParts, chunk) {
			tracker.bytesByChunk[chunk.Index] = chunk.Size
		}
	}
	return tracker
}

func (progress *chunkProgress) update(chunk chunkRange, bytesDone int64) {
	if progress == nil || progress.progress == nil {
		return
	}
	progress.mutex.Lock()
	defer progress.mutex.Unlock()
	progress.bytesByChunk[chunk.Index] = bytesDone
	progress.progress(progress.bytesDoneLocked(), progress.bytesTotal)
}

func (progress *chunkProgress) markDone(chunk chunkRange) {
	if progress == nil {
		return
	}
	progress.update(chunk, chunk.Size)
}

func (progress *chunkProgress) bytesDone() int64 {
	progress.mutex.Lock()
	defer progress.mutex.Unlock()
	return progress.bytesDoneLocked()
}

func (progress *chunkProgress) bytesDoneLocked() int64 {
	var total int64
	for _, value := range progress.bytesByChunk {
		total += value
	}
	return total
}

type chunkProgressReader struct {
	reader    io.Reader
	chunk     chunkRange
	bytesDone int64
	progress  *chunkProgress
}

func (reader *chunkProgressReader) Read(buffer []byte) (int, error) {
	count, err := reader.reader.Read(buffer)
	if count > 0 {
		reader.bytesDone += int64(count)
		reader.progress.update(reader.chunk, reader.bytesDone)
	}
	return count, err
}

func existingFileSize(filePath string) int64 {
	infoFile, err := os.Stat(filePath)
	if err != nil || infoFile.IsDir() {
		return 0
	}
	return infoFile.Size()
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
