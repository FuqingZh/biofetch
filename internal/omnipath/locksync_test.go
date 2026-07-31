package omnipath

import (
	"os"
	"path/filepath"
	"testing"
)

func TestScanOmniPathRecords(t *testing.T) {
	dirVersion := t.TempDir()
	dirTaxID := filepath.Join(dirVersion, "raw", "9606")
	if err := os.MkdirAll(dirTaxID, 0o755); err != nil {
		t.Fatalf("os.MkdirAll returned error: %v", err)
	}

	fileQuery := filepath.Join(dirVersion, "raw", "query_meta.json")
	fileData := filepath.Join(dirTaxID, "enz_sub.tsv")
	if err := os.WriteFile(fileQuery, []byte("{}"), 0o644); err != nil {
		t.Fatalf("os.WriteFile returned error: %v", err)
	}
	if err := os.WriteFile(fileData, []byte("a"), 0o644); err != nil {
		t.Fatalf("os.WriteFile returned error: %v", err)
	}

	records, err := scanOmniPathRecords(dirVersion, "enz_sub", "", manifestFile{}, 2)
	if err != nil {
		t.Fatalf("scanOmniPathRecords returned error: %v", err)
	}
	if len(records) != 2 {
		t.Fatalf("len(records) = %d, want 2", len(records))
	}
	if records[0].Path != "raw/9606/enz_sub.tsv" || records[1].Path != "raw/query_meta.json" {
		t.Fatalf("records = %#v", records)
	}
}

func TestValidateRestoreRecordPathsRejectsEscapeDuplicateAndSymlink(t *testing.T) {
	root := t.TempDir()
	for _, path := range []string{
		"",
		"/tmp/escape.tsv",
		"../escape.tsv",
		"raw/../escape.tsv",
		"raw//9606/interactions.tsv",
		"other/9606/interactions.tsv",
	} {
		if err := validateRestoreRecordPaths(root, []recordFile{{Path: path}}); err == nil {
			t.Fatalf("accepted unsafe restore path %q", path)
		}
	}
	if err := validateRestoreRecordPaths(root, []recordFile{
		{Path: "raw/9606/interactions.tsv"},
		{Path: "raw/9606/interactions.tsv"},
	}); err == nil {
		t.Fatal("accepted duplicate restore paths")
	}
	if err := validateRestoreRecordPaths(root, []recordFile{{Path: "raw/9606/interactions.tsv"}}); err != nil {
		t.Fatalf("rejected safe missing restore path: %v", err)
	}

	outside := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "raw"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "raw", "9606")); err != nil {
		t.Fatal(err)
	}
	if err := validateRestoreRecordPaths(root, []recordFile{{Path: "raw/9606/interactions.tsv"}}); err == nil {
		t.Fatal("accepted restore path through symlink")
	}
}
