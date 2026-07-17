package cliopt

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestSnapshotVersionToken(t *testing.T) {
	got, err := SnapshotVersionToken(filepath.Join("data", "kegg", "catalog", "2026-04"))
	if err != nil {
		t.Fatalf("SnapshotVersionToken returned error: %v", err)
	}
	if got != "2026-04" {
		t.Fatalf("SnapshotVersionToken = %q", got)
	}
}

func TestSnapshotVersionTokenRequiresDirectory(t *testing.T) {
	if _, err := SnapshotVersionToken(""); err == nil {
		t.Fatal("SnapshotVersionToken returned nil error")
	}
}

func TestNormalizeLockWorkersMaxAppliesDefault(t *testing.T) {
	workersMax := 0
	if err := NormalizeLockWorkersMax(&workersMax); err != nil {
		t.Fatalf("NormalizeLockWorkersMax returned error: %v", err)
	}
	if workersMax != DefaultLockWorkersMax {
		t.Fatalf("workersMax = %d, want %d", workersMax, DefaultLockWorkersMax)
	}
}

func TestNormalizeLockWorkersMaxRejectsNegative(t *testing.T) {
	workersMax := -1
	if err := NormalizeLockWorkersMax(&workersMax); err == nil {
		t.Fatal("NormalizeLockWorkersMax returned nil error")
	}
}

func TestNormalizeLockWorkersMaxRejectsAboveLimit(t *testing.T) {
	workersMax := LockWorkersMaxLimit + 1
	if err := NormalizeLockWorkersMax(&workersMax); err == nil {
		t.Fatal("NormalizeLockWorkersMax returned nil error")
	}
}

func TestValidateDownloadControlConfig(t *testing.T) {
	cfg := DownloadControlConfig{WorkersMax: 1, RequestInterval: 0}
	if err := ValidateDownloadControlConfig(&cfg); err != nil {
		t.Fatalf("ValidateDownloadControlConfig returned error: %v", err)
	}
}

func TestValidateDownloadControlConfigRejectsInvalidWorkersMax(t *testing.T) {
	cfg := DownloadControlConfig{WorkersMax: 0}
	err := ValidateDownloadControlConfig(&cfg)
	if err == nil || err.Error() != "workers_max must be >= 1" {
		t.Fatalf("ValidateDownloadControlConfig error = %v", err)
	}
}

func TestValidateDownloadControlConfigRejectsNegativeRequestInterval(t *testing.T) {
	cfg := DownloadControlConfig{WorkersMax: 1, RequestInterval: -1 * time.Millisecond}
	err := ValidateDownloadControlConfig(&cfg)
	if err == nil || err.Error() != "request_interval_ms must be >= 0" {
		t.Fatalf("ValidateDownloadControlConfig error = %v", err)
	}
}

func TestExpandAtFileTokens(t *testing.T) {
	fileValues := filepath.Join(t.TempDir(), "values.txt")
	if err := os.WriteFile(fileValues, []byte("# comment\nc\n\na\n"), 0o644); err != nil {
		t.Fatalf("os.WriteFile returned error: %v", err)
	}

	values, err := ExpandAtFileTokens([]string{"b,a", "@" + fileValues}, "values")
	if err != nil {
		t.Fatalf("ExpandAtFileTokens returned error: %v", err)
	}

	expected := []string{"b", "a", "c", "a"}
	if !reflect.DeepEqual(values, expected) {
		t.Fatalf("ExpandAtFileTokens = %#v, want %#v", values, expected)
	}
}

func TestExpandAtFileTokensRejectsMissingFile(t *testing.T) {
	_, err := ExpandAtFileTokens([]string{"@missing.txt"}, "values")
	if err == nil {
		t.Fatal("ExpandAtFileTokens returned nil error for missing file")
	}
}

func TestSortedUniqueStrings(t *testing.T) {
	values := SortedUniqueStrings([]string{"b", "a", "b", ""})
	expected := []string{"a", "b"}
	if !reflect.DeepEqual(values, expected) {
		t.Fatalf("SortedUniqueStrings = %#v, want %#v", values, expected)
	}
}
