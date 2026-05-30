package uniprot

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"biofetch/internal/shared/tomlx"
)

func TestRunRegisterDMNDWritesManifest(t *testing.T) {
	dirTemp := t.TempDir()
	fileDMND := filepath.Join(dirTemp, "uniprot.dmnd")
	if err := os.WriteFile(fileDMND, []byte("diamond-db"), 0o644); err != nil {
		t.Fatalf("os.WriteFile returned error: %v", err)
	}

	cfg := dmndRegisterConfig{
		dirOut:         filepath.Join(dirTemp, "assets"),
		versionToken:   "2026_01-full",
		fileDMND:       fileDMND,
		fastaVersion:   "2026_01",
		fastaPolicy:    "full",
		headerFormat:   "uniprot",
		diamondVersion: "2.1.11",
		buildCommand:   "diamond makedb --in uniprot.fasta -d uniprot",
	}
	if err := runRegisterDMND(&cfg); err != nil {
		t.Fatalf("runRegisterDMND returned error: %v", err)
	}

	fileManifest := filepath.Join(cfg.dirOut, "dmnd", "2026_01-full", "manifest.lock")
	var manifest dmndManifest
	ok, err := tomlx.ReadFileIfExists(fileManifest, &manifest)
	if err != nil {
		t.Fatalf("ReadFileIfExists returned error: %v", err)
	}
	if !ok {
		t.Fatal("manifest was not written")
	}
	if manifest.Database != "uniprot" || manifest.Asset != "dmnd" || manifest.Source != "registered" {
		t.Fatalf("manifest identity = %#v", manifest)
	}
	if manifest.SourceFASTA.Version != "2026_01" || manifest.SourceFASTA.Policy != "full" {
		t.Fatalf("source fasta = %#v", manifest.SourceFASTA)
	}
	if manifest.Diamond.Version != "2.1.11" || manifest.Diamond.Command == "" {
		t.Fatalf("diamond = %#v", manifest.Diamond)
	}
	if len(manifest.Files) != 1 {
		t.Fatalf("files = %#v", manifest.Files)
	}
	fileDMNDAbs, err := filepath.Abs(fileDMND)
	if err != nil {
		t.Fatalf("filepath.Abs returned error: %v", err)
	}
	if manifest.Files[0].Path != fileDMNDAbs || manifest.Files[0].Bytes != int64(len("diamond-db")) {
		t.Fatalf("file record = %#v", manifest.Files[0])
	}
	if manifest.Files[0].SHA256 == "" {
		t.Fatal("sha256 was empty")
	}
}

func TestRunRegisterDMNDRejectsCurrentVersion(t *testing.T) {
	cfg := dmndRegisterConfig{
		dirOut:         t.TempDir(),
		versionToken:   "current",
		fileDMND:       "uniprot.dmnd",
		fastaVersion:   "2026_01",
		fastaPolicy:    "full",
		headerFormat:   "uniprot",
		diamondVersion: "2.1.11",
		buildCommand:   "diamond makedb",
	}
	err := runRegisterDMND(&cfg)
	if err == nil {
		t.Fatal("runRegisterDMND returned nil error")
	}
}

func TestRunRegisterDMNDRequiresProvenance(t *testing.T) {
	cfg := dmndRegisterConfig{
		dirOut:       t.TempDir(),
		versionToken: "2026_01-full",
		fileDMND:     "uniprot.dmnd",
	}
	err := runRegisterDMND(&cfg)
	if err == nil {
		t.Fatal("runRegisterDMND returned nil error")
	}
	if !strings.Contains(err.Error(), "fasta_version") {
		t.Fatalf("error = %q", err.Error())
	}
}

func TestRunRegisterDMNDDryRunDoesNotWriteManifest(t *testing.T) {
	dirTemp := t.TempDir()
	fileDMND := filepath.Join(dirTemp, "uniprot.dmnd")
	if err := os.WriteFile(fileDMND, []byte("diamond-db"), 0o644); err != nil {
		t.Fatalf("os.WriteFile returned error: %v", err)
	}
	cfg := dmndRegisterConfig{
		dirOut:         filepath.Join(dirTemp, "assets"),
		versionToken:   "2026_01-full",
		fileDMND:       fileDMND,
		fastaVersion:   "2026_01",
		fastaPolicy:    "full",
		headerFormat:   "uniprot",
		diamondVersion: "2.1.11",
		buildCommand:   "diamond makedb --in uniprot.fasta -d uniprot",
		shouldDryRun:   true,
	}
	if err := runRegisterDMND(&cfg); err != nil {
		t.Fatalf("runRegisterDMND returned error: %v", err)
	}
	if _, err := os.Stat(filepath.Join(cfg.dirOut, "dmnd")); !os.IsNotExist(err) {
		t.Fatalf("dmnd dir exists or stat failed unexpectedly: %v", err)
	}
}
