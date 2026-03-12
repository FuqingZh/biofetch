package kegg

import (
	"bytes"
	"reflect"
	"strings"
	"testing"
	"time"

	toml "github.com/pelletier/go-toml/v2"
)

func TestParseBriteIDsFromList(t *testing.T) {
	data := []byte("br:br08301\tCell growth and death\nbr:br08901\tMetabolism\n")
	values, err := parseBriteIDsFromList(data)
	if err != nil {
		t.Fatalf("parseBriteIDsFromList returned error: %v", err)
	}

	expected := []string{"br08301", "br08901"}
	if !reflect.DeepEqual(values, expected) {
		t.Fatalf("parseBriteIDsFromList = %#v, want %#v", values, expected)
	}
}

func TestParseBriteIDsCSVSupportsOrganismSpecificIDs(t *testing.T) {
	values, err := parseBriteIDsCSV("br08301,hsa00001,tcar00001")
	if err != nil {
		t.Fatalf("parseBriteIDsCSV returned error: %v", err)
	}

	expected := []string{"br08301", "hsa00001", "tcar00001"}
	if !reflect.DeepEqual(values, expected) {
		t.Fatalf("parseBriteIDsCSV = %#v, want %#v", values, expected)
	}
}

func TestBuildBriteManifest(t *testing.T) {
	cfg := briteConfig{
		version:       "117.0",
		versionToken:  "117.0",
		sourceRelease: "117.0+/03-10",
		catalogCode:   "br",
	}
	records := []briteRecord{
		{
			BriteID: "br08301",
			Asset:   "brite.entry",
			PathRel: "raw/br/br08301.txt",
			SHA256:  "sha-entry",
			Bytes:   11,
			URL:     "https://rest.kegg.jp/get/br:br08301",
		},
		{
			BriteID: "br08301",
			Asset:   "brite.json",
			PathRel: "raw/br/br08301.json",
			SHA256:  "sha-json",
			Bytes:   22,
			URL:     "https://rest.kegg.jp/get/br:br08301/json",
		},
	}

	manifest := buildBriteManifest(
		&cfg,
		records,
		time.Date(2026, time.March, 11, 12, 0, 0, 0, time.FixedZone("UTC+8", 8*3600)),
	)
	if manifest.Database != "kegg" || manifest.Asset != "brite" {
		t.Fatalf("manifest = %#v", manifest)
	}
	if manifest.Scope.Type != "reference" || manifest.Scope.Value != "br" {
		t.Fatalf("manifest.Scope = %#v", manifest.Scope)
	}
	if len(manifest.Brites) != 1 || manifest.Brites[0].ID != "br08301" {
		t.Fatalf("manifest.Brites = %#v", manifest.Brites)
	}

	var buffer bytes.Buffer
	if err := toml.NewEncoder(&buffer).Encode(manifest); err != nil {
		t.Fatalf("toml encode returned error: %v", err)
	}
	if buffer.Len() == 0 {
		t.Fatal("encoded manifest is empty")
	}
}

func TestParseKEGGReleaseFromInfo(t *testing.T) {
	data := []byte("pathway          KEGG Pathway Database\npathway          Release 117.0+/03-10, Mar 10\n")
	value, err := parseKEGGReleaseFromInfo(data)
	if err != nil {
		t.Fatalf("parseKEGGReleaseFromInfo returned error: %v", err)
	}
	if value != "117.0+/03-10" {
		t.Fatalf("parseKEGGReleaseFromInfo = %q, want %q", value, "117.0+/03-10")
	}
}

func TestParseKEGGOrganismCodesFromList(t *testing.T) {
	data := []byte("T01001\thsa\tHomo sapiens\nT01002\tmmu\tMus musculus\n")
	values, err := parseKEGGOrganismCodesFromList(data)
	if err != nil {
		t.Fatalf("parseKEGGOrganismCodesFromList returned error: %v", err)
	}

	expected := []string{"hsa", "mmu"}
	if !reflect.DeepEqual(values, expected) {
		t.Fatalf("parseKEGGOrganismCodesFromList = %#v, want %#v", values, expected)
	}
}

func TestValidateBriteConfigAllOrganisms(t *testing.T) {
	cfg := briteConfig{
		dirOut:            "/tmp/kegg",
		shouldDownloadAll: true,
		retryMax:          1,
		ruleExisting:      "skip",
	}
	if err := validateBriteConfig(&cfg); err != nil {
		t.Fatalf("validateBriteConfig returned error: %v", err)
	}
}

func TestValidateBriteConfigAllOrganismsWithCatalogFails(t *testing.T) {
	cfg := briteConfig{
		dirOut:            "/tmp/kegg",
		catalogCode:       "hsa",
		shouldDownloadAll: true,
		retryMax:          1,
		ruleExisting:      "skip",
	}
	err := validateBriteConfig(&cfg)
	if err == nil || !strings.Contains(err.Error(), "must not be set") {
		t.Fatalf("validateBriteConfig expected conflict error, got: %v", err)
	}
}

func TestFilterRootBriteIDs(t *testing.T) {
	values := filterRootBriteIDs([]string{"hsa00001", "hsa03010", "hsa05130"}, "hsa")
	expected := []string{"hsa00001"}
	if !reflect.DeepEqual(values, expected) {
		t.Fatalf("filterRootBriteIDs = %#v, want %#v", values, expected)
	}
}

func TestValidateBriteConfigRootOnlyWithIDsFails(t *testing.T) {
	cfg := briteConfig{
		dirOut:                 "/tmp/kegg",
		catalogCode:            "hsa",
		shouldDownloadRootOnly: true,
		briteIDsCSV:            "hsa00001",
		retryMax:               1,
		ruleExisting:           "skip",
	}
	err := validateBriteConfig(&cfg)
	if err == nil || !strings.Contains(err.Error(), "should_download_root_only") {
		t.Fatalf("validateBriteConfig expected root-only conflict error, got: %v", err)
	}
}

func TestDeriveBriteScopeValueAllOrganisms(t *testing.T) {
	cfg := briteConfig{scopeValue: "all"}
	if value := deriveBriteScopeValue(&cfg); value != "all" {
		t.Fatalf("deriveBriteScopeValue = %q, want %q", value, "all")
	}
}

func TestDeriveBriteManifestScopeFromRecords(t *testing.T) {
	cfg := briteConfig{}
	records := []briteRecord{
		{PathRel: "raw/hsa/hsa00001.txt"},
		{PathRel: "raw/tcar/tcar00001.txt"},
	}

	scopeType := deriveBriteManifestScopeType(&cfg, records)
	scopeValue := deriveBriteManifestScopeValue(&cfg, records)
	if scopeType != "organisms" || scopeValue != "hsa,tcar" {
		t.Fatalf("deriveBriteManifestScope = %q, %q", scopeType, scopeValue)
	}
}
