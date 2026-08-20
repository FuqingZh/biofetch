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
	"strconv"
	"strings"
	"sync"
	"time"
)

type DownloadProgressFunc func(bytesDone int64, bytesTotal int64)

type DownloadOptions struct {
	Limiter              *RequestLimiter
	PropagateProbeErrors bool
	SkipMetadataProbe    bool
}

var chunkedDownloadMinBytes int64 = 1 << 30
var chunkSizeBytes int64 = 256 << 20
var chunkWorkersMax = 4

type UnexpectedStatusError struct {
	URL         string
	Status      string
	Code        int
	Server      string
	CFMitigated string
	RetryAfter  time.Duration
}

type RangeIgnoredError struct {
	URL string
}

func (err RangeIgnoredError) Error() string {
	return fmt.Sprintf("request %s: server ignored Range; stale partial removed", err.URL)
}

func unexpectedStatus(response *http.Response, urlFile string) UnexpectedStatusError {
	return UnexpectedStatusError{
		URL:         urlFile,
		Status:      response.Status,
		Code:        response.StatusCode,
		Server:      response.Header.Get("Server"),
		CFMitigated: response.Header.Get("Cf-Mitigated"),
		RetryAfter:  retryAfterDuration(response.Header.Get("Retry-After"), time.Now()),
	}
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

func RetryAfter(err error) time.Duration {
	var statusErr UnexpectedStatusError
	if !errors.As(err, &statusErr) {
		return 0
	}
	return statusErr.RetryAfter
}

func retryAfterDuration(value string, now time.Time) time.Duration {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0
	}
	if seconds, err := strconv.ParseInt(value, 10, 64); err == nil {
		if seconds <= 0 {
			return 0
		}
		return time.Duration(seconds) * time.Second
	}
	instant, err := http.ParseTime(value)
	if err != nil || !instant.After(now) {
		return 0
	}
	return instant.Sub(now)
}

func validateContentRange(value string, expectedStart int64, expectedEnd int64, expectedTotal int64, requireTotal bool) error {
	value = strings.TrimSpace(value)
	if !strings.HasPrefix(value, "bytes ") {
		return fmt.Errorf("invalid Content-Range %q", value)
	}
	parts := strings.Split(strings.TrimPrefix(value, "bytes "), "/")
	if len(parts) != 2 {
		return fmt.Errorf("invalid Content-Range %q", value)
	}
	bounds := strings.Split(parts[0], "-")
	if len(bounds) != 2 {
		return fmt.Errorf("invalid Content-Range %q", value)
	}
	start, errStart := strconv.ParseInt(bounds[0], 10, 64)
	end, errEnd := strconv.ParseInt(bounds[1], 10, 64)
	total, errTotal := strconv.ParseInt(parts[1], 10, 64)
	if errStart != nil || errEnd != nil || errTotal != nil || start < 0 || end < start || total <= end {
		return fmt.Errorf("invalid Content-Range %q", value)
	}
	if start != expectedStart {
		return fmt.Errorf("Content-Range start = %d, want %d", start, expectedStart)
	}
	if expectedEnd >= 0 && end != expectedEnd {
		return fmt.Errorf("Content-Range end = %d, want %d", end, expectedEnd)
	}
	if requireTotal && expectedTotal > 0 && total != expectedTotal {
		return fmt.Errorf("Content-Range total = %d, want %d", total, expectedTotal)
	}
	return nil
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

func doRequest(clientHTTP *http.Client, request *http.Request, limiter *RequestLimiter) (*http.Response, error) {
	limiter.Wait()
	return clientHTTP.Do(request)
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
		return unexpectedStatus(response, urlFile)
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
	return DownloadFileWithResumeOptions(clientHTTP, urlFile, fileOut, progress, DownloadOptions{})
}

func DownloadFileWithResumeOptions(clientHTTP *http.Client, urlFile string, fileOut string, progress DownloadProgressFunc, options DownloadOptions) error {
	var metadata downloadMetadata
	var ok bool
	if !options.SkipMetadataProbe {
		var err error
		metadata, ok, err = probeDownloadMetadata(clientHTTP, urlFile, options.Limiter, options.PropagateProbeErrors)
		if err != nil {
			return err
		}
	}
	if existingFileSize(fileOut) > 0 {
		return downloadFileSingleResume(clientHTTP, urlFile, fileOut, progress, metadata, ok, options.Limiter)
	}
	if ok && metadata.SupportsRange && metadata.ContentLength >= chunkedDownloadMinBytes {
		return downloadFileChunked(clientHTTP, urlFile, fileOut, metadata, progress, options.Limiter)
	}
	return downloadFileSingleResume(clientHTTP, urlFile, fileOut, progress, metadata, ok, options.Limiter)
}

func downloadFileSingleResume(clientHTTP *http.Client, urlFile string, fileOut string, progress DownloadProgressFunc, metadata downloadMetadata, hasMetadata bool, limiter *RequestLimiter) error {
	bytesExisting := existingFileSize(fileOut)
	request, err := http.NewRequest(http.MethodGet, urlFile, nil)
	if err != nil {
		return fmt.Errorf("create request %s: %w", urlFile, err)
	}
	if bytesExisting > 0 {
		request.Header.Set("Range", fmt.Sprintf("bytes=%d-", bytesExisting))
		if validator := metadata.ifRangeValidator(); hasMetadata && validator != "" {
			request.Header.Set("If-Range", validator)
		}
	}

	response, err := doRequest(clientHTTP, request, limiter)
	if err != nil {
		return fmt.Errorf("request %s: %w", urlFile, err)
	}
	defer response.Body.Close()

	shouldAppend := false
	bytesDoneStart := int64(0)
	bytesTotal := response.ContentLength
	switch {
	case bytesExisting > 0 && response.StatusCode == http.StatusPartialContent:
		expectedEnd := int64(-1)
		if hasMetadata {
			expectedEnd = metadata.ContentLength - 1
		}
		if err := validateContentRange(response.Header.Get("Content-Range"), bytesExisting, expectedEnd, metadata.ContentLength, hasMetadata); err != nil {
			return fmt.Errorf("request %s: %w", urlFile, err)
		}
		shouldAppend = true
		bytesDoneStart = bytesExisting
		if response.ContentLength >= 0 {
			bytesTotal = bytesExisting + response.ContentLength
		}
	case bytesExisting > 0 && response.StatusCode >= 200 && response.StatusCode < 300:
		// The origin ignored Range or rejected If-Range. Close without consuming
		// the potentially multi-gigabyte body, discard the stale partial, and
		// issue one clean full request.
		if err := response.Body.Close(); err != nil {
			return fmt.Errorf("close ignored Range response for %s: %w", urlFile, err)
		}
		if err := os.Remove(fileOut); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("remove stale partial %s: %w", fileOut, err)
		}
		return RangeIgnoredError{URL: urlFile}
	case response.StatusCode >= 200 && response.StatusCode < 300:
		shouldAppend = false
	default:
		return unexpectedStatus(response, urlFile)
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
	FinalURL      string
	ETag          string
	LastModified  string
}

func (metadata downloadMetadata) ifRangeValidator() string {
	if etag := strings.TrimSpace(metadata.ETag); etag != "" && !strings.HasPrefix(strings.ToUpper(etag), "W/") {
		return etag
	}
	return strings.TrimSpace(metadata.LastModified)
}

func probeDownloadMetadata(clientHTTP *http.Client, urlFile string, limiter *RequestLimiter, propagateErrors bool) (downloadMetadata, bool, error) {
	request, err := http.NewRequest(http.MethodHead, urlFile, nil)
	if err != nil {
		if propagateErrors {
			return downloadMetadata{}, false, fmt.Errorf("create metadata request %s: %w", urlFile, err)
		}
		return downloadMetadata{}, false, nil
	}
	response, err := doRequest(clientHTTP, request, limiter)
	if err != nil {
		if propagateErrors {
			return downloadMetadata{}, false, fmt.Errorf("request metadata %s: %w", urlFile, err)
		}
		return downloadMetadata{}, false, nil
	}
	_ = response.Body.Close()
	if response.StatusCode == http.StatusMethodNotAllowed {
		return downloadMetadata{}, false, nil
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		if response.StatusCode == http.StatusUnauthorized || response.StatusCode == http.StatusForbidden || strings.EqualFold(strings.TrimSpace(response.Header.Get("Cf-Mitigated")), "challenge") {
			return downloadMetadata{}, false, unexpectedStatus(response, urlFile)
		}
		return downloadMetadata{}, false, nil
	}
	if response.ContentLength <= 0 {
		return downloadMetadata{}, false, nil
	}
	metadata := downloadMetadata{
		ContentLength: response.ContentLength,
		SupportsRange: strings.EqualFold(strings.TrimSpace(response.Header.Get("Accept-Ranges")), "bytes"),
		FinalURL:      response.Request.URL.String(),
		ETag:          strings.TrimSpace(response.Header.Get("ETag")),
		LastModified:  strings.TrimSpace(response.Header.Get("Last-Modified")),
	}
	if metadata.SupportsRange {
		return metadata, true, nil
	}
	if metadata.ContentLength < chunkedDownloadMinBytes {
		return metadata, true, nil
	}
	var probeErr error
	metadata.SupportsRange, probeErr = probeRangeSupport(clientHTTP, urlFile, metadata, limiter, propagateErrors)
	return metadata, true, probeErr
}

func probeRangeSupport(clientHTTP *http.Client, urlFile string, metadata downloadMetadata, limiter *RequestLimiter, propagateErrors bool) (bool, error) {
	request, err := http.NewRequest(http.MethodGet, urlFile, nil)
	if err != nil {
		return false, nil
	}
	request.Header.Set("Range", "bytes=0-0")
	response, err := doRequest(clientHTTP, request, limiter)
	if err != nil {
		if propagateErrors {
			return false, fmt.Errorf("probe Range support for %s: %w", urlFile, err)
		}
		return false, nil
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusUnauthorized || response.StatusCode == http.StatusForbidden || strings.EqualFold(strings.TrimSpace(response.Header.Get("Cf-Mitigated")), "challenge") {
		return false, unexpectedStatus(response, urlFile)
	}
	if response.StatusCode != http.StatusPartialContent {
		return false, nil
	}
	if err := validateContentRange(response.Header.Get("Content-Range"), 0, 0, metadata.ContentLength, true); err != nil {
		return false, fmt.Errorf("probe Range support for %s: %w", urlFile, err)
	}
	_, _ = io.CopyN(io.Discard, response.Body, 1)
	return true, nil
}

type chunkState struct {
	URL           string       `json:"url"`
	ContentLength int64        `json:"content_length"`
	ChunkSize     int64        `json:"chunk_size"`
	ETag          string       `json:"etag,omitempty"`
	LastModified  string       `json:"last_modified,omitempty"`
	Chunks        []chunkRange `json:"chunks"`
}

type chunkRange struct {
	Index int   `json:"index"`
	Start int64 `json:"start"`
	End   int64 `json:"end"`
	Size  int64 `json:"size"`
	Done  bool  `json:"done"`
}

func downloadFileChunked(clientHTTP *http.Client, urlFile string, fileOut string, metadata downloadMetadata, progress DownloadProgressFunc, limiter *RequestLimiter) error {
	contentLength := metadata.ContentLength
	dirParts := fileOut + ".parts"
	if err := os.MkdirAll(dirParts, 0o755); err != nil {
		return fmt.Errorf("create chunk dir %s: %w", dirParts, err)
	}
	state, err := loadOrCreateChunkState(filepath.Join(dirParts, "state.json"), metadata, chunkSizeBytes)
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
				if err := downloadChunk(clientHTTP, urlFile, dirParts, chunk, metadata, limiter, progressChunk); err != nil {
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

func loadOrCreateChunkState(fileState string, metadata downloadMetadata, chunkSize int64) (chunkState, error) {
	data, err := os.ReadFile(fileState)
	if err == nil {
		var state chunkState
		if err := json.Unmarshal(data, &state); err != nil {
			return chunkState{}, fmt.Errorf("read chunk state %s: %w", fileState, err)
		}
		if state.URL == metadata.FinalURL && state.ContentLength == metadata.ContentLength && state.ChunkSize == chunkSize &&
			state.ETag == metadata.ETag && state.LastModified == metadata.LastModified {
			return state, nil
		}
		if err := os.RemoveAll(filepath.Dir(fileState)); err != nil {
			return chunkState{}, fmt.Errorf("reset stale chunk state %s: %w", fileState, err)
		}
		if err := os.MkdirAll(filepath.Dir(fileState), 0o755); err != nil {
			return chunkState{}, fmt.Errorf("recreate chunk dir %s: %w", filepath.Dir(fileState), err)
		}
	}
	if err != nil && !os.IsNotExist(err) {
		return chunkState{}, fmt.Errorf("read chunk state %s: %w", fileState, err)
	}
	return chunkState{
		URL:           metadata.FinalURL,
		ContentLength: metadata.ContentLength,
		ChunkSize:     chunkSize,
		ETag:          metadata.ETag,
		LastModified:  metadata.LastModified,
		Chunks:        buildChunkRanges(metadata.ContentLength, chunkSize),
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

func downloadChunk(clientHTTP *http.Client, urlFile string, dirParts string, chunk chunkRange, metadata downloadMetadata, limiter *RequestLimiter, progress *chunkProgress) error {
	fileChunk := chunkFilePath(dirParts, chunk)
	_ = os.Remove(fileChunk)
	request, err := http.NewRequest(http.MethodGet, urlFile, nil)
	if err != nil {
		return fmt.Errorf("create request %s: %w", urlFile, err)
	}
	request.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", chunk.Start, chunk.End))
	if validator := metadata.ifRangeValidator(); validator != "" {
		request.Header.Set("If-Range", validator)
	}
	response, err := doRequest(clientHTTP, request, limiter)
	if err != nil {
		return fmt.Errorf("request %s: %w", urlFile, err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusPartialContent {
		return unexpectedStatus(response, urlFile)
	}
	if err := validateContentRange(response.Header.Get("Content-Range"), chunk.Start, chunk.End, metadata.ContentLength, true); err != nil {
		return fmt.Errorf("request chunk %d for %s: %w", chunk.Index, urlFile, err)
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
