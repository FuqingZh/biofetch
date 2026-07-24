package kegg

import (
	"reflect"
	"strings"
	"testing"
)

func TestParseMetabolicVersion(t *testing.T) {
	version, err := parseMetabolicVersion([]byte("pathway          KEGG Pathway Database\npathway          Release 116.0+/07-23, Jul 26\n                 Last update 2026/07/23\n"))
	if err != nil {
		t.Fatal(err)
	}
	if version != "2026-07" {
		t.Fatalf("version = %q, want 2026-07", version)
	}
}

func TestMetabolicRestoreKeepsKEGGRequestIntervalDefault(t *testing.T) {
	command := createMetabolicCommand()
	restore, _, err := command.Find([]string{"restore"})
	if err != nil {
		t.Fatal(err)
	}
	flag := restore.Flags().Lookup("request-interval")
	if flag == nil || flag.DefValue != defaultKEGGRequestInterval.String() {
		t.Fatalf("restore request-interval default = %v", flag)
	}
}

func TestParseMetabolicVersionRejectsMissingUpdate(t *testing.T) {
	if _, err := parseMetabolicVersion([]byte("pathway\n")); err == nil {
		t.Fatal("parseMetabolicVersion accepted missing update")
	}
}

func TestBuildMetabolicEntryBatchesUsesKEGGLimit(t *testing.T) {
	ids := []string{
		"cpd:C00001", "cpd:C00002", "cpd:C00003", "cpd:C00004", "cpd:C00005",
		"cpd:C00006", "cpd:C00007", "cpd:C00008", "cpd:C00009", "cpd:C00010",
		"cpd:C00011",
	}
	assets := buildMetabolicEntryBatches("compound", ids, "https://example.test")
	if len(assets) != 2 {
		t.Fatalf("len(assets) = %d, want 2", len(assets))
	}
	if count := strings.Count(assets[0].URL, "+") + 1; count != 10 {
		t.Fatalf("first batch count = %d, want 10; url=%s", count, assets[0].URL)
	}
	if assets[1].Path != "compound/entries/000002.keg" {
		t.Fatalf("second path = %q", assets[1].Path)
	}
	if !reflect.DeepEqual([]string{assets[0].Name, assets[1].Name}, []string{"compound-entry-000001", "compound-entry-000002"}) {
		t.Fatalf("asset names = %#v", assets)
	}
}
