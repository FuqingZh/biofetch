package kegg

import (
	"os"
	"path/filepath"
	"testing"
)

func TestScanPathwayRecords(t *testing.T) {
	dirVersion := t.TempDir()
	dirScope := filepath.Join(dirVersion, "raw", "reference")
	if err := os.MkdirAll(dirScope, 0o755); err != nil {
		t.Fatalf("os.MkdirAll returned error: %v", err)
	}

	fileList := filepath.Join(dirScope, "pathway.list.tsv")
	fileEntry := filepath.Join(dirScope, "map00010.txt")
	if err := os.WriteFile(fileList, []byte("map00010\tName\n"), 0o644); err != nil {
		t.Fatalf("os.WriteFile returned error: %v", err)
	}
	if err := os.WriteFile(fileEntry, []byte("ENTRY"), 0o644); err != nil {
		t.Fatalf("os.WriteFile returned error: %v", err)
	}

	records, err := scanPathwayRecords(dirVersion, 2)
	if err != nil {
		t.Fatalf("scanPathwayRecords returned error: %v", err)
	}
	if len(records) != 2 {
		t.Fatalf("len(records) = %d, want 2", len(records))
	}
	if records[0].Asset != "pathway.list" || records[1].Asset != "pathway.entry" {
		t.Fatalf("records = %#v", records)
	}
}
