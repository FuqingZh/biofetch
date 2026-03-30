package kegg

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	toml "github.com/pelletier/go-toml/v2"
)

func TestBuildCatalogManifest(t *testing.T) {
	cfg := catalogConfig{
		version:       "117.0",
		versionToken:  "117.0",
		sourceRelease: "117.0+/03-26",
	}
	records := []catalogRecord{
		{
			Asset:   keggCatalogAsset,
			PathRel: "raw/organism.list.tsv",
			SHA256:  "sha-organism",
			Bytes:   11,
			URL:     keggCatalogURL,
		},
	}

	manifest := buildCatalogManifest(
		&cfg,
		records,
		time.Date(2026, time.March, 26, 12, 0, 0, 0, time.FixedZone("UTC+8", 8*3600)),
	)
	if manifest.Database != "kegg" || manifest.Asset != "catalog" || manifest.Catalog != keggCatalogAsset {
		t.Fatalf("manifest = %#v", manifest)
	}

	var buffer bytes.Buffer
	if err := toml.NewEncoder(&buffer).Encode(manifest); err != nil {
		t.Fatalf("toml encode returned error: %v", err)
	}

	var decoded catalogManifestFile
	if err := toml.Unmarshal(buffer.Bytes(), &decoded); err != nil {
		t.Fatalf("toml.Unmarshal returned error: %v", err)
	}
	if decoded.VersionToken != "117.0" || decoded.SourceRelease != "117.0+/03-26" {
		t.Fatalf("decoded = %#v", decoded)
	}
	if len(decoded.Files) != 1 || decoded.Files[0].Path != "raw/organism.list.tsv" {
		t.Fatalf("decoded.Files = %#v", decoded.Files)
	}
}

func TestScanCatalogRecords(t *testing.T) {
	dirVersion := t.TempDir()
	dirRaw := filepath.Join(dirVersion, "raw")
	if err := os.MkdirAll(dirRaw, 0o755); err != nil {
		t.Fatalf("os.MkdirAll returned error: %v", err)
	}

	fileCatalog := filepath.Join(dirRaw, keggCatalogFileName)
	if err := os.WriteFile(fileCatalog, []byte("T01001\thsa\tHomo sapiens\n"), 0o644); err != nil {
		t.Fatalf("os.WriteFile returned error: %v", err)
	}

	records, err := scanCatalogRecords(dirVersion)
	if err != nil {
		t.Fatalf("scanCatalogRecords returned error: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("len(records) = %d, want 1", len(records))
	}
	if records[0].Asset != keggCatalogAsset || records[0].PathRel != "raw/organism.list.tsv" {
		t.Fatalf("record = %#v", records[0])
	}
}

func TestValidateCatalogFetchConfigRejectsInvalidVersion(t *testing.T) {
	cfg := catalogConfig{
		dirOut:       "/tmp/kegg",
		versionToken: "current",
	}
	err := validateCatalogFetchConfig(&cfg)
	if err == nil || !strings.Contains(err.Error(), "KEGG major version") {
		t.Fatalf("validateCatalogFetchConfig expected version error, got: %v", err)
	}
}
