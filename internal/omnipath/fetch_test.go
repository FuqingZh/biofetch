package omnipath

import "testing"

func TestNormalizeOrganism(t *testing.T) {
	value, err := normalizeOrganism("human")
	if err != nil {
		t.Fatalf("normalize organism: %v", err)
	}
	if value != "9606" {
		t.Fatalf("unexpected taxid: %s", value)
	}
}

func TestExtractVersionFromMetadata(t *testing.T) {
	text := []byte(`{"version":"2025-01-17"}`)
	version, err := extractVersionFromMetadata(text)
	if err != nil {
		t.Fatalf("extract version: %v", err)
	}
	if version != "2025-01-17" {
		t.Fatalf("unexpected version: %s", version)
	}
}

func TestSanitizeVersionToken(t *testing.T) {
	if got := sanitizeVersionToken("v1/2025:03"); got != "v1_2025_03" {
		t.Fatalf("unexpected token: %s", got)
	}
}
