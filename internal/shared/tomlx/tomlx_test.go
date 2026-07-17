package tomlx

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type testDocument struct {
	Name  string `toml:"name"`
	Count int    `toml:"count"`
}

func TestReadFileIfExistsMissing(t *testing.T) {
	var document testDocument
	ok, err := ReadFileIfExists(filepath.Join(t.TempDir(), "missing.toml"), &document)
	if err != nil || ok {
		t.Fatalf("ReadFileIfExists = (%v, %v), want (false, nil)", ok, err)
	}
}

func TestWriteFileAtomicRoundTrip(t *testing.T) {
	filePath := filepath.Join(t.TempDir(), "manifest.lock")
	expected := testDocument{Name: "biofetch", Count: 2}
	if err := WriteFileAtomic(filePath, expected); err != nil {
		t.Fatalf("WriteFileAtomic returned error: %v", err)
	}
	if _, err := os.Stat(filePath + ".tmp"); !os.IsNotExist(err) {
		t.Fatalf("temporary file remains: %v", err)
	}

	var actual testDocument
	ok, err := ReadFileIfExists(filePath, &actual)
	if err != nil || !ok {
		t.Fatalf("ReadFileIfExists = (%v, %v)", ok, err)
	}
	if actual != expected {
		t.Fatalf("round trip = %#v, want %#v", actual, expected)
	}
}

func TestReadFileIfExistsRejectsInvalidTOML(t *testing.T) {
	filePath := filepath.Join(t.TempDir(), "invalid.toml")
	if err := os.WriteFile(filePath, []byte("name = [\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var document testDocument
	ok, err := ReadFileIfExists(filePath, &document)
	if ok || err == nil || !strings.Contains(err.Error(), "decode manifest") {
		t.Fatalf("ReadFileIfExists = (%v, %v), want decode error", ok, err)
	}
}
