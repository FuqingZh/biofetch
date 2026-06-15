package logx

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestBuildRunLogFileName(t *testing.T) {
	name := buildRunLogFileName("fetch", time.Date(2026, time.June, 10, 1, 5, 33, 0, time.UTC), "a1b2c3d4")
	if name != "fetch-20260610T010533Z-a1b2c3d4.log" {
		t.Fatalf("name = %q", name)
	}
}

func TestResolveLogDirDefaultsToVersionLogs(t *testing.T) {
	dirVersion := "/tmp/example/2026-04"
	dirLogs := ResolveLogDir("", dirVersion)
	if dirLogs != filepath.Join(dirVersion, "logs") {
		t.Fatalf("dirLogs = %q", dirLogs)
	}
}

func TestStartRunCreatesFile(t *testing.T) {
	dirLogs := t.TempDir()
	filePath, err := StartRun("fetch", dirLogs)
	if err != nil {
		t.Fatalf("StartRun returned error: %v", err)
	}
	defer func() { _ = CloseRun() }()

	if !strings.HasPrefix(filepath.Base(filePath), "fetch-") {
		t.Fatalf("filePath = %q", filePath)
	}
	if _, err := os.Stat(filePath); err != nil {
		t.Fatalf("os.Stat returned error: %v", err)
	}
}
