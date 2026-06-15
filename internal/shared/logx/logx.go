package logx

import (
	"biofetch/internal/shared/staticasset"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/log"
)

var styleError = lipgloss.NewStyle().
	Bold(true).
	Foreground(lipgloss.Color("9"))

type runState struct {
	mutex    sync.Mutex
	file     *os.File
	filePath string
}

type staticAssetTraceSink struct {
	prefix string
}

var stateRun runState

func StartRun(action string, dirLogs string) (string, error) {
	if action == "" {
		action = "run"
	}
	if dirLogs == "" {
		return "", fmt.Errorf("dir_logs must not be empty")
	}
	if err := os.MkdirAll(dirLogs, 0o755); err != nil {
		return "", fmt.Errorf("create log dir %s: %w", dirLogs, err)
	}
	filePath := filepath.Join(dirLogs, buildRunLogFileName(action, time.Now().UTC(), buildRunID()))
	fileLog, err := os.Create(filePath)
	if err != nil {
		return "", fmt.Errorf("create log file %s: %w", filePath, err)
	}

	stateRun.mutex.Lock()
	defer stateRun.mutex.Unlock()
	if stateRun.file != nil {
		_ = stateRun.file.Close()
	}
	stateRun.file = fileLog
	stateRun.filePath = filePath
	return filePath, nil
}

func CloseRun() error {
	stateRun.mutex.Lock()
	defer stateRun.mutex.Unlock()
	if stateRun.file == nil {
		stateRun.filePath = ""
		return nil
	}
	err := stateRun.file.Close()
	stateRun.file = nil
	stateRun.filePath = ""
	return err
}

func ResolveLogDir(dirLogs string, dirVersion string) string {
	if dirLogs != "" {
		return dirLogs
	}
	return filepath.Join(dirVersion, "logs")
}

func StartVersionedRun(prefix string, action string, dirLogs string, dirVersion string) (string, func(), error) {
	filePath, err := StartRun(action, ResolveLogDir(dirLogs, dirVersion))
	if err != nil {
		return "", nil, err
	}
	Logf(prefix, "run log: %s", filePath)
	return filePath, func() {
		_ = CloseRun()
	}, nil
}

func StartSourceRun(prefix string, action string, dirLogs string, dirOut string, source staticasset.Source) (staticasset.TraceSink, func(), error) {
	_, closeRun, err := StartVersionedRun(prefix, action, dirLogs, staticasset.DeriveVersionDir(dirOut, source))
	if err != nil {
		return nil, nil, err
	}
	return NewStaticAssetTraceSink(prefix), closeRun, nil
}

func CurrentRunPath() string {
	stateRun.mutex.Lock()
	defer stateRun.mutex.Unlock()
	return stateRun.filePath
}

func Logf(prefix string, format string, args ...interface{}) {
	logWithLevel(log.InfoLevel, prefix, format, args...)
}

func Warnf(prefix string, format string, args ...interface{}) {
	logWithLevel(log.WarnLevel, prefix, format, args...)
}

func Errorf(prefix string, format string, args ...interface{}) {
	logWithLevel(log.ErrorLevel, prefix, format, args...)
}

func Writer() io.Writer {
	stateRun.mutex.Lock()
	defer stateRun.mutex.Unlock()
	if stateRun.file == nil {
		return os.Stderr
	}
	return io.MultiWriter(os.Stderr, stateRun.file)
}

func NewStaticAssetTraceSink(prefix string) staticasset.TraceSink {
	return staticAssetTraceSink{prefix: prefix}
}

func (sink staticAssetTraceSink) EmitStaticAssetTrace(event staticasset.TraceEvent) {
	Logf(
		sink.prefix,
		"trace event=%s status=%s asset=%s path=%s url=%s bytes=%d",
		event.Event,
		event.Status,
		event.Asset,
		event.Path,
		event.URL,
		event.Bytes,
	)
}

func RenderErrorLabel() string {
	return styleError.Render("ERROR")
}

func buildRunLogFileName(action string, timeRun time.Time, runID string) string {
	return fmt.Sprintf("%s-%s-%s.log", action, timeRun.Format("20060102T150405Z"), runID)
}

func buildRunID() string {
	buffer := make([]byte, 4)
	if _, err := rand.Read(buffer); err != nil {
		return fmt.Sprintf("%08x", time.Now().UnixNano())
	}
	return hex.EncodeToString(buffer)
}

func logWithLevel(level log.Level, prefix string, format string, args ...interface{}) {
	logger := log.NewWithOptions(Writer(), log.Options{
		Prefix:          prefix,
		ReportTimestamp: false,
		ReportCaller:    false,
	})
	message := fmt.Sprintf(format, args...)
	switch level {
	case log.ErrorLevel:
		logger.Error(message)
	case log.WarnLevel:
		logger.Warn(message)
	default:
		logger.Info(message)
	}
}
