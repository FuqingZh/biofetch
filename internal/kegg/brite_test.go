package kegg

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

func TestResolveBriteIDInputsSupportsAtFileAndInputOrder(t *testing.T) {
	fileBriteIDs := filepath.Join(t.TempDir(), "brite_ids.txt")
	if err := os.WriteFile(fileBriteIDs, []byte("# comment\nbr08901\nbr08301\n"), 0o644); err != nil {
		t.Fatalf("os.WriteFile returned error: %v", err)
	}

	values, err := resolveBriteIDInputs([]string{"hsa00001", "br08901,br08301"}, ruleOrderInput)
	if err != nil {
		t.Fatalf("resolveBriteIDInputs returned error: %v", err)
	}

	expected := []string{"hsa00001", "br08901", "br08301"}
	if !reflect.DeepEqual(values, expected) {
		t.Fatalf("resolveBriteIDInputs = %#v, want %#v", values, expected)
	}
}

func TestBuildBriteManifest(t *testing.T) {
	cfg := briteConfig{
		version:            "2026-03",
		versionToken:       "2026-03",
		sourceRelease:      "117.0+/03-10",
		sourceReleaseStart: "117.0+/03-10",
		sourceReleaseEnd:   "117.0+/03-11",
		catalogCode:        "br",
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
	if manifest.VersionToken != "2026-03" || manifest.SourceReleaseStart != "117.0+/03-10" || manifest.SourceReleaseEnd != "117.0+/03-11" {
		t.Fatalf("manifest release fields = %#v", manifest)
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
	data := []byte("T01001\thsa\tHomo sapiens\nT01002\tmmu; Mus musculus (mouse)\n")
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
		versionToken:      "2026-04",
		shouldDownloadAll: true,
		retryMax:          1,
		ruleExisting:      "skip",
	}
	if err := validateBriteConfig(&cfg); err != nil {
		t.Fatalf("validateBriteConfig returned error: %v", err)
	}
	if cfg.ruleOrder != ruleOrderAsc {
		t.Fatalf("cfg.ruleOrder = %q, want %q", cfg.ruleOrder, ruleOrderAsc)
	}
}

func TestValidateBriteConfigAllOrganismsWithCatalogFails(t *testing.T) {
	cfg := briteConfig{
		dirOut:            "/tmp/kegg",
		versionToken:      "2026-04",
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
		versionToken:           "2026-04",
		catalogCode:            "hsa",
		shouldDownloadRootOnly: true,
		briteIDs:               []string{"hsa00001"},
		retryMax:               1,
		ruleExisting:           "skip",
	}
	err := validateBriteConfig(&cfg)
	if err == nil || !strings.Contains(err.Error(), "root-only") {
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

func TestValidateBriteConfigRejectsMajorVersionToken(t *testing.T) {
	cfg := briteConfig{
		dirOut:       "/tmp/kegg",
		versionToken: "117.0",
		catalogCode:  "br",
		retryMax:     1,
		ruleExisting: "skip",
	}
	err := validateBriteConfig(&cfg)
	if err == nil || !strings.Contains(err.Error(), "local snapshot key") {
		t.Fatalf("validateBriteConfig expected snapshot key error, got: %v", err)
	}
}

func TestValidateBriteConfigRejectsInvalidRuleOrder(t *testing.T) {
	cfg := briteConfig{
		dirOut:       "/tmp/kegg",
		versionToken: "2026-04",
		catalogCode:  "br",
		ruleOrder:    "reverse",
		retryMax:     1,
		ruleExisting: "skip",
	}
	err := validateBriteConfig(&cfg)
	if err == nil || !strings.Contains(err.Error(), "order") {
		t.Fatalf("validateBriteConfig expected order error, got: %v", err)
	}
}

func TestValidateBriteConfigResolvesAtFileInputs(t *testing.T) {
	fileOrganisms := filepath.Join(t.TempDir(), "organisms.txt")
	if err := os.WriteFile(fileOrganisms, []byte("tca\nhsa\n"), 0o644); err != nil {
		t.Fatalf("os.WriteFile returned error: %v", err)
	}
	fileBriteIDs := filepath.Join(t.TempDir(), "brite_ids.txt")
	if err := os.WriteFile(fileBriteIDs, []byte("hsa00001\nbr08301\n"), 0o644); err != nil {
		t.Fatalf("os.WriteFile returned error: %v", err)
	}

	cfg := briteConfig{
		dirOut:       "/tmp/kegg",
		versionToken: "2026-04",
		organismCodes: []string{
			"tca,hsa",
		},
		briteIDs:     []string{"hsa00001,br08301"},
		ruleOrder:    ruleOrderInput,
		retryMax:     1,
		ruleExisting: "skip",
	}
	if err := validateBriteConfig(&cfg); err != nil {
		t.Fatalf("validateBriteConfig returned error: %v", err)
	}
	expectedOrganisms := []string{"tca", "hsa"}
	if !reflect.DeepEqual(cfg.organismCodes, expectedOrganisms) {
		t.Fatalf("cfg.organismCodes = %#v, want %#v", cfg.organismCodes, expectedOrganisms)
	}
	expectedBriteIDs := []string{"hsa00001", "br08301"}
	if !reflect.DeepEqual(cfg.briteIDs, expectedBriteIDs) {
		t.Fatalf("cfg.briteIDs = %#v, want %#v", cfg.briteIDs, expectedBriteIDs)
	}
}

func TestReadExistingBriteManifestBackfillsReleaseRange(t *testing.T) {
	dirTemp := t.TempDir()
	fileManifest := filepath.Join(dirTemp, "manifest.lock")
	manifest := briteManifestFile{
		Database:      "kegg",
		Asset:         "brite",
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

	manifestRead, err := readExistingBriteManifest(fileManifest)
	if err != nil {
		t.Fatalf("readExistingBriteManifest returned error: %v", err)
	}
	if manifestRead.SourceReleaseStart != "118.0+/04-01" || manifestRead.SourceReleaseEnd != "118.0+/04-01" {
		t.Fatalf("manifestRead = %#v", manifestRead)
	}
}
