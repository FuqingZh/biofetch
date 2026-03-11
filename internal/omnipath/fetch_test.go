package omnipath

import "testing"

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

func TestParseOrganisms(t *testing.T) {
	values, err := parseOrganisms([]string{"9606", "10090,9606"})
	if err != nil {
		t.Fatalf("parseOrganisms: %v", err)
	}
	if len(values) != 2 || values[0] != "10090" || values[1] != "9606" {
		t.Fatalf("unexpected organisms: %#v", values)
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
