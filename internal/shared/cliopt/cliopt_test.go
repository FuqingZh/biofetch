package cliopt

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/spf13/pflag"
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
	if err == nil || err.Error() != "workers must be >= 1" {
		t.Fatalf("ValidateDownloadControlConfig error = %v", err)
	}
}

func TestValidateDownloadControlConfigRejectsNegativeRequestInterval(t *testing.T) {
	cfg := DownloadControlConfig{WorkersMax: 1, RequestInterval: -1 * time.Millisecond}
	err := ValidateDownloadControlConfig(&cfg)
	if err == nil || err.Error() != "request-interval must be >= 0" {
		t.Fatalf("ValidateDownloadControlConfig error = %v", err)
	}
}

func TestExpandListTokens(t *testing.T) {
	fileValues := filepath.Join(t.TempDir(), "values.txt")
	if err := os.WriteFile(fileValues, []byte("# comment\nc\na\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	values, err := ExpandListTokens([]string{"b,a"}, fileValues, "values")
	if err != nil {
		t.Fatal(err)
	}
	expected := []string{"b", "a", "c", "a"}
	if !reflect.DeepEqual(values, expected) {
		t.Fatalf("values = %#v, want %#v", values, expected)
	}
}

func TestExpandListTokensRejectsAtSyntax(t *testing.T) {
	_, err := ExpandListTokens([]string{"@missing.txt"}, "", "values")
	if err == nil {
		t.Fatal("expected @ syntax rejection")
	}
}

func TestBindStringListFlagsPreservesArgumentOrder(t *testing.T) {
	fileValues := filepath.Join(t.TempDir(), "values.txt")
	if err := os.WriteFile(fileValues, []byte("from-file\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name string
		args []string
		want []string
	}{
		{
			name: "file before inline",
			args: []string{"--values-file", fileValues, "--values", "inline"},
			want: []string{"from-file", "inline"},
		},
		{
			name: "inline before file",
			args: []string{"--values", "inline", "--values-file", fileValues},
			want: []string{"inline", "from-file"},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			var values []string
			flags := pflag.NewFlagSet("test", pflag.ContinueOnError)
			BindStringListFlags(flags, &values, "values", "test values")
			if err := flags.Parse(test.args); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(values, test.want) {
				t.Fatalf("values = %#v, want %#v", values, test.want)
			}
		})
	}
}

func TestSortedUniqueStrings(t *testing.T) {
	values := SortedUniqueStrings([]string{"b", "a", "b", ""})
	expected := []string{"a", "b"}
	if !reflect.DeepEqual(values, expected) {
		t.Fatalf("SortedUniqueStrings = %#v, want %#v", values, expected)
	}
}
