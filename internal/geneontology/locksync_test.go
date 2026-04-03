package geneontology

import (
	"os"
	"path/filepath"
	"testing"
)

func TestScanOntologyRecords(t *testing.T) {
	dirVersion := t.TempDir()
	dirRaw := filepath.Join(dirVersion, "raw")
	if err := os.MkdirAll(dirRaw, 0o755); err != nil {
		t.Fatalf("os.MkdirAll returned error: %v", err)
	}

	fileA := filepath.Join(dirRaw, "go.obo")
	fileB := filepath.Join(dirRaw, "go-basic.obo")
	if err := os.WriteFile(fileA, []byte("a"), 0o644); err != nil {
		t.Fatalf("os.WriteFile returned error: %v", err)
	}
	if err := os.WriteFile(fileB, []byte("b"), 0o644); err != nil {
		t.Fatalf("os.WriteFile returned error: %v", err)
	}

	records, err := scanOntologyRecords(dirVersion, "2026-01-23", nil)
	if err != nil {
		t.Fatalf("scanOntologyRecords returned error: %v", err)
	}
	if len(records) != 2 {
		t.Fatalf("len(records) = %d, want 2", len(records))
	}
	if records[0].Asset != "go-basic.obo" || records[1].Asset != "go.obo" {
		t.Fatalf("records = %#v", records)
	}
	if records[0].URL != "https://release.geneontology.org/2026-01-23/ontology/go-basic.obo" {
		t.Fatalf("records[0].URL = %q", records[0].URL)
	}
}

func TestScanOntologyRecordsPreservesExistingManifestURLs(t *testing.T) {
	dirVersion := t.TempDir()
	dirRaw := filepath.Join(dirVersion, "raw")
	if err := os.MkdirAll(dirRaw, 0o755); err != nil {
		t.Fatalf("os.MkdirAll returned error: %v", err)
	}

	filePath := filepath.Join(dirRaw, "go-basic.obo")
	if err := os.WriteFile(filePath, []byte("content"), 0o644); err != nil {
		t.Fatalf("os.WriteFile returned error: %v", err)
	}

	records, err := scanOntologyRecords(dirVersion, "2026-01-23", map[string]string{
		"raw/go-basic.obo": ontologyCurrentBaseURL + "go-basic.obo",
	})
	if err != nil {
		t.Fatalf("scanOntologyRecords returned error: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("len(records) = %d, want 1", len(records))
	}
	if records[0].URL != ontologyCurrentBaseURL+"go-basic.obo" {
		t.Fatalf("records[0].URL = %q, want %q", records[0].URL, ontologyCurrentBaseURL+"go-basic.obo")
	}
}

func TestPlanSyncOntologyTasksPlansReuseAndDownloads(t *testing.T) {
	dirVersion := t.TempDir()
	dirRaw := filepath.Join(dirVersion, "raw")
	if err := os.MkdirAll(dirRaw, 0o755); err != nil {
		t.Fatalf("os.MkdirAll returned error: %v", err)
	}

	if err := os.WriteFile(filepath.Join(dirRaw, "go-basic.obo"), []byte("cached"), 0o644); err != nil {
		t.Fatalf("os.WriteFile returned error: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dirRaw, "go-stale.obo"), []byte("short"), 0o644); err != nil {
		t.Fatalf("os.WriteFile returned error: %v", err)
	}

	filesCurrentByPath, err := scanOntologyRawFileStateIndex(dirRaw)
	if err != nil {
		t.Fatalf("scanOntologyRawFileStateIndex returned error: %v", err)
	}

	recordsManifest := []ontologyRecord{
		{
			Asset:   "go-basic.obo",
			PathRel: "raw/go-basic.obo",
			SHA256:  "sha-basic",
			Bytes:   int64(len("cached")),
			URL:     ontologyCurrentBaseURL + "go-basic.obo",
		},
		{
			Asset:   "go-stale.obo",
			PathRel: "raw/go-stale.obo",
			SHA256:  "sha-stale",
			Bytes:   999,
			URL:     ontologyCurrentBaseURL + "go-stale.obo",
		},
		{
			Asset:   "go-missing.obo",
			PathRel: "raw/go-missing.obo",
			SHA256:  "sha-missing",
			Bytes:   10,
			URL:     ontologyCurrentBaseURL + "go-missing.obo",
		},
	}

	recordsReused, tasksDownload := planSyncOntologyTasks(dirVersion, recordsManifest, false, filesCurrentByPath)
	if len(recordsReused) != 1 || recordsReused[0].Asset != "go-basic.obo" {
		t.Fatalf("recordsReused = %#v", recordsReused)
	}
	if len(tasksDownload) != 2 {
		t.Fatalf("len(tasksDownload) = %d, want 2", len(tasksDownload))
	}
	if tasksDownload[0].asset.name != "go-stale.obo" || tasksDownload[1].asset.name != "go-missing.obo" {
		t.Fatalf("tasksDownload = %#v", tasksDownload)
	}
}
