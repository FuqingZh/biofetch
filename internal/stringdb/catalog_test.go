package stringdb

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	toml "github.com/pelletier/go-toml/v2"
)

func TestBuildCatalogManifestFile(t *testing.T) {
	records := []catalogRecord{
		{
			Asset:   stringCatalogAsset,
			PathRel: "raw/species.v12.0.txt",
			SHA256:  "sha-species",
			Bytes:   11,
			URL:     "https://stringdb-downloads.org/download/species.v12.0.txt",
		},
	}

	manifest := buildCatalogManifestFile(
		"v12.0",
		records,
		time.Date(2026, time.March, 26, 12, 0, 0, 0, time.FixedZone("UTC+8", 8*3600)),
	)

	var buffer bytes.Buffer
	if err := toml.NewEncoder(&buffer).Encode(manifest); err != nil {
		t.Fatalf("toml.NewEncoder returned error: %v", err)
	}

	var decoded catalogManifestFile
	if err := toml.Unmarshal(buffer.Bytes(), &decoded); err != nil {
		t.Fatalf("toml.Unmarshal returned error: %v", err)
	}
	if decoded.Database != "string" || decoded.Asset != "catalog" || decoded.Catalog != stringCatalogAsset {
		t.Fatalf("decoded = %#v", decoded)
	}
	if decoded.Version != "12.0" || decoded.VersionToken != "v12.0" {
		t.Fatalf("decoded version = %#v", decoded)
	}
	if len(decoded.Files) != 1 || decoded.Files[0].Path != "raw/species.v12.0.txt" {
		t.Fatalf("decoded.Files = %#v", decoded.Files)
	}
}

func TestScanCatalogRecords(t *testing.T) {
	dirVersion := t.TempDir()
	dirRaw := filepath.Join(dirVersion, "raw")
	if err := os.MkdirAll(dirRaw, 0o755); err != nil {
		t.Fatalf("os.MkdirAll returned error: %v", err)
	}

	fileCatalog := filepath.Join(dirRaw, "species.v12.0.txt")
	if err := os.WriteFile(fileCatalog, []byte("taxon_id\tname\n9606\tHuman\n"), 0o644); err != nil {
		t.Fatalf("os.WriteFile returned error: %v", err)
	}

	records, err := scanCatalogRecords(dirVersion, "v12.0", 2)
	if err != nil {
		t.Fatalf("scanCatalogRecords returned error: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("len(records) = %d, want 1", len(records))
	}
	if records[0].Asset != stringCatalogAsset || records[0].PathRel != "raw/species.v12.0.txt" {
		t.Fatalf("record = %#v", records[0])
	}
}

func TestValidateCatalogRestoreConfigRequiresVersion(t *testing.T) {
	cfg := catalogRestoreConfig{dirOut: "/tmp/string"}
	err := validateCatalogRestoreConfig(&cfg)
	if err == nil || !strings.Contains(err.Error(), "version is required") {
		t.Fatalf("validateCatalogRestoreConfig expected version error, got: %v", err)
	}
}
