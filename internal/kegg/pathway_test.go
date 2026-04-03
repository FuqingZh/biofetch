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

func TestResolvePathwayAssetNamesReturnsAllWhenAssetsOmitted(t *testing.T) {
	values, err := resolvePathwayAssetNames(nil)
	if err != nil {
		t.Fatalf("resolvePathwayAssetNames returned error: %v", err)
	}

	expected := []string{"list", "entry", "kgml", "conf", "image"}
	if !reflect.DeepEqual(values, expected) {
		t.Fatalf("resolvePathwayAssetNames = %#v, want %#v", values, expected)
	}
}

func TestResolveKEGGOrganismInputsSupportsAtFileAndInputOrder(t *testing.T) {
	fileOrganisms := filepath.Join(t.TempDir(), "organisms.txt")
	if err := os.WriteFile(fileOrganisms, []byte("# comment\nmmu\nhsa\n"), 0o644); err != nil {
		t.Fatalf("os.WriteFile returned error: %v", err)
	}

	values, err := resolveKEGGOrganismInputs([]string{"tca,hsa", "@" + fileOrganisms}, ruleOrderInput)
	if err != nil {
		t.Fatalf("resolveKEGGOrganismInputs returned error: %v", err)
	}

	expected := []string{"tca", "hsa", "mmu"}
	if !reflect.DeepEqual(values, expected) {
		t.Fatalf("resolveKEGGOrganismInputs = %#v, want %#v", values, expected)
	}
}

func TestResolvePathwayIDInputsSupportsDescOrder(t *testing.T) {
	values, err := resolvePathwayIDInputs([]string{"map00020,map00010", "map00030"}, ruleOrderDesc)
	if err != nil {
		t.Fatalf("resolvePathwayIDInputs returned error: %v", err)
	}

	expected := []string{"map00030", "map00020", "map00010"}
	if !reflect.DeepEqual(values, expected) {
		t.Fatalf("resolvePathwayIDInputs = %#v, want %#v", values, expected)
	}
}

func TestResolvePathwayIDInputsRejectsInvalidAtFile(t *testing.T) {
	filePathwayIDs := filepath.Join(t.TempDir(), "pathway_ids.txt")
	if err := os.WriteFile(filePathwayIDs, []byte("map00010\nbad\n"), 0o644); err != nil {
		t.Fatalf("os.WriteFile returned error: %v", err)
	}

	_, err := resolvePathwayIDInputs([]string{"@" + filePathwayIDs}, ruleOrderAsc)
	if err == nil || !strings.Contains(err.Error(), "invalid pathway id in") {
		t.Fatalf("resolvePathwayIDInputs expected invalid file error, got: %v", err)
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
		version:              "2026-03",
		versionToken:         "2026-03",
		sourceRelease:        "117.0+/03-11",
		sourceReleaseStart:   "117.0+/03-11",
		sourceReleaseEnd:     "117.0+/03-12",
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
	if manifest.VersionToken != "2026-03" || manifest.SourceReleaseStart != "117.0+/03-11" || manifest.SourceReleaseEnd != "117.0+/03-12" {
		t.Fatalf("manifest release fields = %#v", manifest)
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

func TestDerivePathwayListURLReferenceAndOrganism(t *testing.T) {
	if value := derivePathwayListURL(&pathwayConfig{shouldFetchReference: true}); value != "https://rest.kegg.jp/list/pathway" {
		t.Fatalf("derivePathwayListURL reference = %q", value)
	}
	if value := derivePathwayListURL(&pathwayConfig{organismCode: "hsa"}); value != "https://rest.kegg.jp/list/pathway/hsa" {
		t.Fatalf("derivePathwayListURL organism = %q", value)
	}
}

func TestValidatePathwayConfigAcceptsSnapshotVersion(t *testing.T) {
	cfg := pathwayConfig{
		dirOut:               "/tmp/kegg",
		versionToken:         "2026-04",
		shouldFetchReference: true,
		retryMax:             1,
		ruleExisting:         "skip",
	}
	if err := validatePathwayConfig(&cfg); err != nil {
		t.Fatalf("validatePathwayConfig returned error: %v", err)
	}
	expected := []string{"list", "entry", "kgml", "conf", "image"}
	if !reflect.DeepEqual(cfg.assetNames, expected) {
		t.Fatalf("cfg.assetNames = %#v, want %#v", cfg.assetNames, expected)
	}
	if cfg.ruleOrder != ruleOrderAsc {
		t.Fatalf("cfg.ruleOrder = %q, want %q", cfg.ruleOrder, ruleOrderAsc)
	}
}

func TestValidatePathwayConfigRejectsMajorVersionToken(t *testing.T) {
	cfg := pathwayConfig{
		dirOut:               "/tmp/kegg",
		versionToken:         "117.0",
		assetNames:           []string{"list"},
		shouldFetchReference: true,
		retryMax:             1,
		ruleExisting:         "skip",
	}
	err := validatePathwayConfig(&cfg)
	if err == nil || !strings.Contains(err.Error(), "local snapshot key") {
		t.Fatalf("validatePathwayConfig expected snapshot key error, got: %v", err)
	}
}

func TestValidatePathwayConfigKeepsExplicitAssetSubset(t *testing.T) {
	cfg := pathwayConfig{
		dirOut:               "/tmp/kegg",
		versionToken:         "2026-04",
		assetNames:           []string{"entry,image"},
		shouldFetchReference: true,
		retryMax:             1,
		ruleExisting:         "skip",
	}
	if err := validatePathwayConfig(&cfg); err != nil {
		t.Fatalf("validatePathwayConfig returned error: %v", err)
	}
	expected := []string{"entry", "image"}
	if !reflect.DeepEqual(cfg.assetNames, expected) {
		t.Fatalf("cfg.assetNames = %#v, want %#v", cfg.assetNames, expected)
	}
}

func TestValidatePathwayConfigRejectsInvalidRuleOrder(t *testing.T) {
	cfg := pathwayConfig{
		dirOut:               "/tmp/kegg",
		versionToken:         "2026-04",
		shouldFetchReference: true,
		ruleOrder:            "reverse",
		retryMax:             1,
		ruleExisting:         "skip",
	}
	err := validatePathwayConfig(&cfg)
	if err == nil || !strings.Contains(err.Error(), "rule_order") {
		t.Fatalf("validatePathwayConfig expected rule_order error, got: %v", err)
	}
}

func TestValidatePathwayConfigResolvesAtFileInputs(t *testing.T) {
	fileOrganisms := filepath.Join(t.TempDir(), "organisms.txt")
	if err := os.WriteFile(fileOrganisms, []byte("mmu\nhsa\n"), 0o644); err != nil {
		t.Fatalf("os.WriteFile returned error: %v", err)
	}

	cfg := pathwayConfig{
		dirOut:       "/tmp/kegg",
		versionToken: "2026-04",
		organismCodes: []string{
			"@" + fileOrganisms,
		},
		ruleOrder:    ruleOrderInput,
		retryMax:     1,
		ruleExisting: "skip",
	}
	if err := validatePathwayConfig(&cfg); err != nil {
		t.Fatalf("validatePathwayConfig returned error: %v", err)
	}
	expected := []string{"mmu", "hsa"}
	if !reflect.DeepEqual(cfg.organismCodes, expected) {
		t.Fatalf("cfg.organismCodes = %#v, want %#v", cfg.organismCodes, expected)
	}
}

func TestValidatePathwayConfigResolvesPathwayIDsAtFile(t *testing.T) {
	filePathwayIDs := filepath.Join(t.TempDir(), "pathway_ids.txt")
	if err := os.WriteFile(filePathwayIDs, []byte("map00020\nmap00010\n"), 0o644); err != nil {
		t.Fatalf("os.WriteFile returned error: %v", err)
	}

	cfg := pathwayConfig{
		dirOut:               "/tmp/kegg",
		versionToken:         "2026-04",
		shouldFetchReference: true,
		pathwayIDs:           []string{"@" + filePathwayIDs},
		ruleOrder:            ruleOrderInput,
		retryMax:             1,
		ruleExisting:         "skip",
	}
	if err := validatePathwayConfig(&cfg); err != nil {
		t.Fatalf("validatePathwayConfig returned error: %v", err)
	}
	expected := []string{"map00020", "map00010"}
	if !reflect.DeepEqual(cfg.pathwayIDs, expected) {
		t.Fatalf("cfg.pathwayIDs = %#v, want %#v", cfg.pathwayIDs, expected)
	}
}

func TestReadExistingPathwayManifestBackfillsReleaseRange(t *testing.T) {
	dirTemp := t.TempDir()
	fileManifest := filepath.Join(dirTemp, "manifest.lock")
	manifest := manifestFile{
		Database:      "kegg",
		Asset:         "pathway",
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

	manifestRead, err := readExistingPathwayManifest(fileManifest)
	if err != nil {
		t.Fatalf("readExistingPathwayManifest returned error: %v", err)
	}
	if manifestRead.SourceReleaseStart != "118.0+/04-01" || manifestRead.SourceReleaseEnd != "118.0+/04-01" {
		t.Fatalf("manifestRead = %#v", manifestRead)
	}
}
