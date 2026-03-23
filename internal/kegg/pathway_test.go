package kegg

import (
	"bytes"
	"reflect"
	"testing"
	"time"

	toml "github.com/pelletier/go-toml/v2"
)

func TestParsePathwayIDsFromList(t *testing.T) {
	data := []byte("map00010\tGlycolysis / Gluconeogenesis\nmap00020\tCitrate cycle (TCA cycle)\n")
	values, err := parsePathwayIDsFromList(data)
	if err != nil {
		t.Fatalf("parsePathwayIDsFromList returned error: %v", err)
	}

	expected := []string{"map00010", "map00020"}
	if !reflect.DeepEqual(values, expected) {
		t.Fatalf("parsePathwayIDsFromList = %#v, want %#v", values, expected)
	}
}

func TestParsePathwayAssetNames(t *testing.T) {
	values, err := parsePathwayAssetNames([]string{"entry,kgml", "image", "entry"})
	if err != nil {
		t.Fatalf("parsePathwayAssetNames returned error: %v", err)
	}

	expected := []string{"entry", "image", "kgml"}
	if !reflect.DeepEqual(values, expected) {
		t.Fatalf("parsePathwayAssetNames = %#v, want %#v", values, expected)
	}
}

func TestDerivePathwayAssetSpecs(t *testing.T) {
	specs := derivePathwayAssetSpecs("hsa", "hsa00010", []string{"entry", "conf", "image"}, "/tmp/raw/hsa")
	if len(specs) != 3 {
		t.Fatalf("derivePathwayAssetSpecs len = %d, want 3", len(specs))
	}
	if specs[0].assetName != "pathway.entry" || specs[0].url != "https://rest.kegg.jp/get/hsa00010" {
		t.Fatalf("entry spec = %#v", specs[0])
	}
	if specs[1].assetName != "pathway.conf" || specs[1].pathRel != "raw/hsa/hsa00010.conf" {
		t.Fatalf("conf spec = %#v", specs[1])
	}
	if specs[2].assetName != "pathway.image" || specs[2].fileOut != "/tmp/raw/hsa/hsa00010.png" {
		t.Fatalf("image spec = %#v", specs[2])
	}
}

func TestBuildManifestFile(t *testing.T) {
	cfg := pathwayConfig{
		version:              "117.0",
		versionToken:         "117.0",
		sourceRelease:        "117.0+/03-11",
		shouldFetchReference: true,
	}
	records := []pathwayRecord{
		{
			PathwayID: "map00010",
			Asset:     "pathway.entry",
			PathRel:   "raw/reference/map00010.txt",
			SHA256:    "sha-entry",
			Bytes:     11,
			URL:       "https://rest.kegg.jp/get/map00010",
		},
		{
			PathwayID: "map00010",
			Asset:     "pathway.kgml",
			PathRel:   "raw/reference/map00010.kgml",
			SHA256:    "sha-kgml",
			Bytes:     22,
			URL:       "https://rest.kegg.jp/get/map00010/kgml",
		},
	}

	manifest := buildManifestFile(
		&cfg,
		records,
		time.Date(2026, time.March, 11, 12, 0, 0, 0, time.FixedZone("UTC+8", 8*3600)),
	)
	if manifest.Database != "kegg" || manifest.Asset != "pathway" {
		t.Fatalf("manifest = %#v", manifest)
	}
	if len(manifest.Pathways) != 1 || manifest.Pathways[0].ID != "map00010" {
		t.Fatalf("manifest.Pathways = %#v", manifest.Pathways)
	}

	var buffer bytes.Buffer
	if err := toml.NewEncoder(&buffer).Encode(manifest); err != nil {
		t.Fatalf("toml encode returned error: %v", err)
	}
	if buffer.Len() == 0 {
		t.Fatal("encoded manifest is empty")
	}
}

func TestDerivePathwayManifestScopeFromRecords(t *testing.T) {
	cfg := pathwayConfig{}
	records := []pathwayRecord{
		{PathRel: "raw/hsa/map00010.txt"},
		{PathRel: "raw/mmu/map00020.txt"},
	}

	scopeType, scopeValue := derivePathwayManifestScope(&cfg, records)
	if scopeType != "organisms" || scopeValue != "hsa,mmu" {
		t.Fatalf("derivePathwayManifestScope = %q, %q", scopeType, scopeValue)
	}
}

func TestParseKEGGMajorVersion(t *testing.T) {
	value, err := parseKEGGMajorVersion("117.0+/03-10")
	if err != nil {
		t.Fatalf("parseKEGGMajorVersion returned error: %v", err)
	}
	if value != "117.0" {
		t.Fatalf("parseKEGGMajorVersion = %q, want %q", value, "117.0")
	}
}
