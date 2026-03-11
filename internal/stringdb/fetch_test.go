package stringdb

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	toml "github.com/pelletier/go-toml/v2"
)

func TestParseTaxIDsCSV(t *testing.T) {
	values, err := parseTaxIDsCSV("9606,7070,9606")
	if err != nil {
		t.Fatalf("parseTaxIDsCSV returned error: %v", err)
	}

	expected := []string{"7070", "9606"}
	if !reflect.DeepEqual(values, expected) {
		t.Fatalf("parseTaxIDsCSV = %#v, want %#v", values, expected)
	}
}

func TestReadTaxIDsFromFile(t *testing.T) {
	dirTemp := t.TempDir()
	fileTaxIDs := filepath.Join(dirTemp, "taxids.txt")
	content := "# comment\n7070\n\n9606\n7070\n"
	if err := os.WriteFile(fileTaxIDs, []byte(content), 0o644); err != nil {
		t.Fatalf("os.WriteFile returned error: %v", err)
	}

	values, err := readTaxIDsFromFile(fileTaxIDs)
	if err != nil {
		t.Fatalf("readTaxIDsFromFile returned error: %v", err)
	}

	expected := []string{"7070", "9606"}
	if !reflect.DeepEqual(values, expected) {
		t.Fatalf("readTaxIDsFromFile = %#v, want %#v", values, expected)
	}
}

func TestBuildManifestFile(t *testing.T) {
	records := []fileRecord{
		{
			speciesID: "7070",
			assetName: "protein.aliases",
			pathRel:   "raw/7070/7070.protein.aliases.v12.0.txt.gz",
			sha256:    "sha-aliases",
			bytes:     11,
			url:       "https://example.org/aliases",
		},
		{
			speciesID: "7070",
			assetName: "protein.links",
			pathRel:   "raw/7070/7070.protein.links.v12.0.txt.gz",
			sha256:    "sha-links",
			bytes:     22,
			url:       "https://example.org/links",
		},
	}

	manifest := buildManifestFile(
		"v12.0",
		records,
		time.Date(2026, time.March, 10, 12, 0, 0, 0, time.FixedZone("UTC+8", 8*3600)),
	)

	var buffer bytes.Buffer
	if err := toml.NewEncoder(&buffer).Encode(manifest); err != nil {
		t.Fatalf("toml.NewEncoder returned error: %v", err)
	}

	var decoded manifestFile
	if err := toml.Unmarshal(buffer.Bytes(), &decoded); err != nil {
		t.Fatalf("toml.Unmarshal returned error: %v", err)
	}
	if decoded.Database != "string" || decoded.Version != "12.0" {
		t.Fatalf("decoded = %#v", decoded)
	}
	if len(decoded.Species) != 1 || decoded.Species[0].ID != "7070" {
		t.Fatalf("decoded.Species = %#v", decoded.Species)
	}
	if len(decoded.Files) != 2 {
		t.Fatalf("len(decoded.Files) = %d, want 2", len(decoded.Files))
	}
}

func TestConfirmAllSpeciesDownload(t *testing.T) {
	var buffer bytes.Buffer
	err := confirmAllSpeciesDownload(
		strings.NewReader("should_download_all_species\n"),
		&buffer,
	)
	if err != nil {
		t.Fatalf("confirmAllSpeciesDownload returned error: %v", err)
	}

	assertContains(t, buffer.String(), "Full-species download may fetch a large number of files")
	assertContains(t, buffer.String(), `Type "should_download_all_species" to continue.`)
	assertContains(t, buffer.String(), "> ")
}

func TestConfirmAllSpeciesDownloadRejectsWrongInput(t *testing.T) {
	err := confirmAllSpeciesDownload(
		strings.NewReader("yes\n"),
		&bytes.Buffer{},
	)
	if err == nil {
		t.Fatal("confirmAllSpeciesDownload returned nil error for wrong confirmation text")
	}

	assertContains(t, err.Error(), `expected "should_download_all_species"`)
}

func assertContains(t *testing.T, text string, expected string) {
	t.Helper()
	if !strings.Contains(text, expected) {
		t.Fatalf("expected %q in text:\n%s", expected, text)
	}
}
