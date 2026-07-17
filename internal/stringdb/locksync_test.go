package stringdb

import (
	"os"
	"path/filepath"
	"testing"
)

func TestScanVersionFileRecords(t *testing.T) {
	dirVersion := t.TempDir()
	dirSpeciesA := filepath.Join(dirVersion, "raw", "7070")
	dirSpeciesB := filepath.Join(dirVersion, "raw", "9606")
	if err := os.MkdirAll(dirSpeciesA, 0o755); err != nil {
		t.Fatalf("os.MkdirAll returned error: %v", err)
	}
	if err := os.MkdirAll(dirSpeciesB, 0o755); err != nil {
		t.Fatalf("os.MkdirAll returned error: %v", err)
	}

	fileA := filepath.Join(dirSpeciesA, "7070.protein.aliases."+filepath.Base(dirVersion)+".txt.gz")
	fileB := filepath.Join(dirSpeciesB, "9606.protein.links."+filepath.Base(dirVersion)+".txt.gz")
	if err := os.WriteFile(fileA, []byte("a"), 0o644); err != nil {
		t.Fatalf("os.WriteFile returned error: %v", err)
	}
	if err := os.WriteFile(fileB, []byte("b"), 0o644); err != nil {
		t.Fatalf("os.WriteFile returned error: %v", err)
	}

	records, err := scanVersionFileRecords(dirVersion, 2)
	if err != nil {
		t.Fatalf("scanVersionFileRecords returned error: %v", err)
	}
	if len(records) != 2 {
		t.Fatalf("len(records) = %d, want 2", len(records))
	}
	if records[0].speciesID != "7070" || records[1].speciesID != "9606" {
		t.Fatalf("records = %#v", records)
	}
}

func TestPlanSyncDownloadTasksPlansReuseAndDownloads(t *testing.T) {
	dirVersion := t.TempDir()
	fileExisting := filepath.Join(dirVersion, "raw", "7070", "7070.protein.aliases.v12.0.txt.gz")
	fileStale := filepath.Join(dirVersion, "raw", "7070", "7070.protein.links.v12.0.txt.gz")

	if err := writeGzipFile(fileExisting, []byte("aliases")); err != nil {
		t.Fatalf("writeGzipFile returned error: %v", err)
	}
	if err := writeGzipFile(fileStale, []byte("links")); err != nil {
		t.Fatalf("writeGzipFile returned error: %v", err)
	}

	recordExisting, err := buildFileRecord(
		fileExisting,
		"7070",
		"protein.aliases",
		"raw/7070/7070.protein.aliases.v12.0.txt.gz",
		"https://example.org/aliases",
	)
	if err != nil {
		t.Fatalf("buildFileRecord returned error: %v", err)
	}

	recordsManifest := []fileRecord{
		recordExisting,
		{
			speciesID: "7070",
			assetName: "protein.links",
			pathRel:   "raw/7070/7070.protein.links.v12.0.txt.gz",
			sha256:    "sha-stale",
			bytes:     1,
			url:       "https://example.org/links",
		},
		{
			speciesID: "7070",
			assetName: "protein.info",
			pathRel:   "raw/7070/7070.protein.info.v12.0.txt.gz",
			sha256:    "sha-missing",
			bytes:     1,
			url:       "https://example.org/info",
		},
	}

	recordsReused, tasksDownload, err := planSyncDownloadTasks(dirVersion, recordsManifest, false)
	if err != nil {
		t.Fatalf("planSyncDownloadTasks returned error: %v", err)
	}
	if len(recordsReused) != 1 || recordsReused[0].assetName != "protein.aliases" {
		t.Fatalf("recordsReused = %#v", recordsReused)
	}
	if len(tasksDownload) != 2 {
		t.Fatalf("len(tasksDownload) = %d, want 2", len(tasksDownload))
	}
	if tasksDownload[0].assetName != "protein.links" || tasksDownload[1].assetName != "protein.info" {
		t.Fatalf("tasksDownload = %#v", tasksDownload)
	}
}
