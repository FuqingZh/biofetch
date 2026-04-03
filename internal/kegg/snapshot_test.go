package kegg

import (
	"testing"
	"time"
)

func TestDeriveKEGGSnapshotVersionToken(t *testing.T) {
	value := deriveKEGGSnapshotVersionToken(
		time.Date(2026, time.April, 1, 12, 0, 0, 0, time.FixedZone("UTC+8", 8*3600)),
	)
	if value != "2026-04" {
		t.Fatalf("deriveKEGGSnapshotVersionToken = %q, want %q", value, "2026-04")
	}
}

func TestIsValidKEGGSnapshotVersionToken(t *testing.T) {
	if !isValidKEGGSnapshotVersionToken("2026-04") {
		t.Fatal("expected 2026-04 to be valid")
	}
	if isValidKEGGSnapshotVersionToken("117.0") {
		t.Fatal("expected 117.0 to be invalid")
	}
}

func TestDeriveKEGGReleaseFieldsCompatibility(t *testing.T) {
	valueCompat, valueStart, valueEnd := deriveKEGGReleaseFields("118.0+/04-01", "", "")
	if valueCompat != "118.0+/04-01" || valueStart != "118.0+/04-01" || valueEnd != "118.0+/04-01" {
		t.Fatalf("deriveKEGGReleaseFields = %q, %q, %q", valueCompat, valueStart, valueEnd)
	}
}
