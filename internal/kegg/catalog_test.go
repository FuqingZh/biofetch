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
		version:            "2026-03",
		versionToken:       "2026-03",
		sourceRelease:      "117.0+/03-26",
		sourceReleaseStart: "117.0+/03-26",
		sourceReleaseEnd:   "117.0+/03-27",
	}
	records := []catalogRecord{
		{
			Asset:   keggCatalogAsset,
			PathRel: "raw/organism.list.tsv",
			SHA256:  "sha-organism",
			Bytes:   11,
			URL:     deriveKEGGCatalogURL(),
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
	if decoded.VersionToken != "2026-03" || decoded.SourceRelease != "117.0+/03-26" {
		t.Fatalf("decoded = %#v", decoded)
	}
	if decoded.SourceReleaseStart != "117.0+/03-26" || decoded.SourceReleaseEnd != "117.0+/03-27" {
		t.Fatalf("decoded release range = %#v", decoded)
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

	records, err := scanCatalogRecords(dirVersion, 2)
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
	if err == nil || !strings.Contains(err.Error(), "local snapshot key") {
		t.Fatalf("validateCatalogFetchConfig expected version error, got: %v", err)
	}
}

func TestValidateCatalogFetchConfigAcceptsSnapshotVersion(t *testing.T) {
	cfg := catalogConfig{
		dirOut:       "/tmp/kegg",
		versionToken: "2026-04",
	}
	if err := validateCatalogFetchConfig(&cfg); err != nil {
		t.Fatalf("validateCatalogFetchConfig returned error: %v", err)
	}
}

func TestReadExistingCatalogManifestBackfillsReleaseRange(t *testing.T) {
	dirTemp := t.TempDir()
	fileManifest := filepath.Join(dirTemp, "manifest.lock")
	manifest := catalogManifestFile{
		Database:      "kegg",
		Asset:         "catalog",
		Catalog:       keggCatalogAsset,
		Version:       "2026-04",
		VersionToken:  "2026-04",
		SourceRelease: "118.0+/04-01",
	}
	data, err := toml.Marshal(manifest)
	if err != nil {
		t.Fatalf("toml.Marshal returned error: %v", err)
	}
	if err := os.WriteFile(fileManifest, data, 0o644); err != nil {
		t.Fatalf("os.WriteFile returned error: %v", err)
	}

	manifestRead, err := readExistingCatalogManifest(fileManifest)
	if err != nil {
		t.Fatalf("readExistingCatalogManifest returned error: %v", err)
	}
	if manifestRead.SourceReleaseStart != "118.0+/04-01" || manifestRead.SourceReleaseEnd != "118.0+/04-01" {
		t.Fatalf("manifestRead = %#v", manifestRead)
	}
}

func TestRunLockCatalogUsesDirectoryVersionIdentity(t *testing.T) {
	dirSnapshot := filepath.Join(t.TempDir(), "catalog", "2026-04")
	dirRaw := filepath.Join(dirSnapshot, "raw")
	if err := os.MkdirAll(dirRaw, 0o755); err != nil {
		t.Fatalf("os.MkdirAll returned error: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dirRaw, keggCatalogFileName), []byte("T01001\thsa\tHomo sapiens\n"), 0o644); err != nil {
		t.Fatalf("os.WriteFile raw returned error: %v", err)
	}
	dataOld, err := toml.Marshal(catalogManifestFile{
		Database:      "kegg",
		Asset:         "catalog",
		Catalog:       keggCatalogAsset,
		Version:       "118.0",
		VersionToken:  "118.0",
		SourceRelease: "118.0+/04-01",
	})
	if err != nil {
		t.Fatalf("toml.Marshal returned error: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dirSnapshot, "manifest.lock"), dataOld, 0o644); err != nil {
		t.Fatalf("os.WriteFile manifest returned error: %v", err)
	}

	if err := runLockCatalog(&catalogLockConfig{dirSnapshot: dirSnapshot}); err != nil {
		t.Fatalf("runLockCatalog returned error: %v", err)
	}
	manifest, err := readExistingCatalogManifest(filepath.Join(dirSnapshot, "manifest.lock"))
	if err != nil {
		t.Fatalf("readExistingCatalogManifest returned error: %v", err)
	}
	if manifest.Version != "2026-04" || manifest.VersionToken != "2026-04" {
		t.Fatalf("manifest identity = version %q, version_token %q", manifest.Version, manifest.VersionToken)
	}
	if manifest.SourceRelease != "118.0+/04-01" {
		t.Fatalf("SourceRelease = %q", manifest.SourceRelease)
	}
}
