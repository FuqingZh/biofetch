package omnipath

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestNormalizeOrganism(t *testing.T) {
	value, err := normalizeOrganism("9606")
	if err != nil {
		t.Fatalf("normalize organism: %v", err)
	}
	if value != "9606" {
		t.Fatalf("unexpected taxid: %s", value)
	}
}

func TestNormalizeOrganismRejectsCommonName(t *testing.T) {
	_, err := normalizeOrganism("human")
	if err == nil {
		t.Fatal("normalizeOrganism returned nil error for common name")
	}
}

func TestExtractVersionFromArchiveIndex(t *testing.T) {
	text := []byte(`
<a href="omnipath_webservice_enz_sub__20230405-20250813.tsv.gz">omnipath_webservice_enz_sub__20230405-20250813.tsv.gz</a>
<a href="omnipath_webservice_interactions__20230728-20250813.tsv.gz">omnipath_webservice_interactions__20230728-20250813.tsv.gz</a>
`)
	version, err := extractVersionFromArchiveIndex(text, "enz_sub")
	if err != nil {
		t.Fatalf("extractVersionFromArchiveIndex: %v", err)
	}
	if version != "2025-08-13" {
		t.Fatalf("unexpected version: %s", version)
	}
}

func TestSanitizeVersionToken(t *testing.T) {
	if got := sanitizeVersionToken("v1/2025:03"); got != "v1_2025_03" {
		t.Fatalf("unexpected token: %s", got)
	}
}

func TestValidateOptionalVersionToken(t *testing.T) {
	if err := validateOptionalVersionToken(""); err != nil {
		t.Fatalf("validateOptionalVersionToken returned error for empty version: %v", err)
	}
	if err := validateOptionalVersionToken("2025-08-13"); err != nil {
		t.Fatalf("validateOptionalVersionToken returned error for valid version: %v", err)
	}
}

func TestValidateOptionalVersionTokenRejectsInvalidValue(t *testing.T) {
	err := validateOptionalVersionToken("2025-8-13")
	if err == nil {
		t.Fatal("validateOptionalVersionToken returned nil error for invalid version")
	}
	if !strings.Contains(err.Error(), archiveURL) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestExtractArchiveSnapshotFromIndexSelectsRequestedVersion(t *testing.T) {
	text := []byte(`
<a href="omnipath_webservice_enz_sub__20220114-20230405.tsv.xz">old</a>
<a href="omnipath_webservice_enz_sub__20230405-20250813.tsv.xz">new</a>
`)
	snapshot, err := extractArchiveSnapshotFromIndex(text, "enz_sub", "2025-08-13")
	if err != nil {
		t.Fatalf("extractArchiveSnapshotFromIndex returned error: %v", err)
	}
	if snapshot.version != "2025-08-13" {
		t.Fatalf("snapshot.version = %q", snapshot.version)
	}
	if snapshot.urlFile != archiveURL+"omnipath_webservice_enz_sub__20230405-20250813.tsv.xz" {
		t.Fatalf("snapshot.urlFile = %q", snapshot.urlFile)
	}
}

func TestMatchArchiveTaxIDsEnzSub(t *testing.T) {
	taxIDs, err := matchArchiveTaxIDs(
		"enz_sub",
		"",
		[]string{"enzyme", "ncbi_tax_id"},
		"P12345\t10090",
	)
	if err != nil {
		t.Fatalf("matchArchiveTaxIDs returned error: %v", err)
	}
	if len(taxIDs) != 1 || taxIDs[0] != "10090" {
		t.Fatalf("taxIDs = %#v", taxIDs)
	}
}

func TestMatchArchiveTaxIDsInteractions(t *testing.T) {
	taxIDs, err := matchArchiveTaxIDs(
		"interactions",
		"kinaseextra",
		[]string{"kinaseextra", "ncbi_tax_id_source", "ncbi_tax_id_target"},
		"True\t9606\t-1",
	)
	if err != nil {
		t.Fatalf("matchArchiveTaxIDs returned error: %v", err)
	}
	if len(taxIDs) != 1 || taxIDs[0] != "9606" {
		t.Fatalf("taxIDs = %#v", taxIDs)
	}
}

func TestParseOrganisms(t *testing.T) {
	values, err := parseOrganisms([]string{"9606", "10090,9606"})
	if err != nil {
		t.Fatalf("parseOrganisms: %v", err)
	}
	if len(values) != 2 || values[0] != "10090" || values[1] != "9606" {
		t.Fatalf("unexpected organisms: %#v", values)
	}
}

func TestParseOrganismsSupportsAtFile(t *testing.T) {
	fileOrganisms := filepath.Join(t.TempDir(), "organisms.txt")
	if err := os.WriteFile(fileOrganisms, []byte("# comment\n9606\n\n10090\n"), 0o644); err != nil {
		t.Fatalf("os.WriteFile returned error: %v", err)
	}

	values, err := parseOrganisms([]string{"10090,9606"})
	if err != nil {
		t.Fatalf("parseOrganisms returned error: %v", err)
	}
	expected := []string{"10090", "9606"}
	if !reflect.DeepEqual(values, expected) {
		t.Fatalf("parseOrganisms = %#v, want %#v", values, expected)
	}
}

func TestValidateEnzSubConfigResolvesAtFileOrganisms(t *testing.T) {
	fileOrganisms := filepath.Join(t.TempDir(), "organisms.txt")
	if err := os.WriteFile(fileOrganisms, []byte("9606\n10090\n"), 0o644); err != nil {
		t.Fatalf("os.WriteFile returned error: %v", err)
	}

	cfg := configEnzSub{
		dirOut:       "/tmp/omnipath",
		versionToken: "2025-08-13",
		organisms:    []string{"10090,9606"},
		ruleExisting: "skip",
		retryMax:     1,
	}
	if err := validateEnzSubConfig(&cfg); err != nil {
		t.Fatalf("validateEnzSubConfig returned error: %v", err)
	}
	expected := []string{"10090", "9606"}
	if !reflect.DeepEqual(cfg.organisms, expected) {
		t.Fatalf("cfg.organisms = %#v, want %#v", cfg.organisms, expected)
	}
}

func TestParseOrganismsFromQueryMetadata(t *testing.T) {
	data := []byte("fields datasets interactions organisms 9606;10090;10116 license academic;commercial")
	values, err := parseOrganismsFromQueryMetadata(data)
	if err != nil {
		t.Fatalf("parseOrganismsFromQueryMetadata: %v", err)
	}

	expected := []string{"10090", "10116", "9606"}
	if len(values) != len(expected) {
		t.Fatalf("unexpected values: %#v", values)
	}
	for index, expectedValue := range expected {
		if values[index] != expectedValue {
			t.Fatalf("unexpected values: %#v", values)
		}
	}
}

func TestDeriveOmniPathManifestScopeAndRequestURL(t *testing.T) {
	records := []recordFile{
		{Asset: "query_meta", Path: "raw/query_meta.json", URL: "https://omnipathdb.org/queries/interactions"},
		{Asset: "interactions", Path: "raw/9606/interactions.tsv", URL: "https://omnipathdb.org/interactions?organisms=9606"},
		{Asset: "interactions", Path: "raw/10090/interactions.tsv", URL: "https://omnipathdb.org/interactions?organisms=10090"},
	}

	scopeType, scopeValue := deriveOmniPathManifestScope(records)
	if scopeType != "organisms" || scopeValue != "10090,9606" {
		t.Fatalf("deriveOmniPathManifestScope = %q, %q", scopeType, scopeValue)
	}
	if value := deriveOmniPathRequestURL(records); value != "" {
		t.Fatalf("deriveOmniPathRequestURL = %q, want empty", value)
	}
	if value := deriveOmniPathQueryURL(records, ""); value != "https://omnipathdb.org/queries/interactions" {
		t.Fatalf("deriveOmniPathQueryURL = %q", value)
	}
}
