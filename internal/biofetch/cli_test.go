package biofetch

import (
	"strings"
	"testing"
)

var cliAssets = [][]string{
	{"eggnog", "cog"},
	{"eggnog", "mapper"},
	{"go", "annotation"},
	{"go", "ontology"},
	{"go", "slim"},
	{"interpro", "mapping"},
	{"interpro", "scan"},
	{"kegg", "brite"},
	{"kegg", "catalog"},
	{"kegg", "mapping"},
	{"kegg", "pathway"},
	{"omnipath", "enzyme-substrate"},
	{"omnipath", "interactions"},
	{"reactome", "mapping"},
	{"string", "network"},
	{"string", "catalog"},
	{"uniprot", "id-mapping"},
	{"uniprot", "kb"},
	{"uniprot", "uniref"},
	{"wikipathways", "gmt"},
}

func TestEveryMaintainedActionHelpPath(t *testing.T) {
	for _, asset := range cliAssets {
		for _, action := range []string{"fetch", "lock", "restore"} {
			args := append(append([]string{}, asset...), action, "--help")
			if err := RunCLI(args); err != nil {
				t.Fatalf("RunCLI(%q) error = %v", args, err)
			}
		}
	}
	if err := RunCLI([]string{"manifest", "build", "--help"}); err != nil {
		t.Fatal(err)
	}
}

func TestRootVersionAndCompletion(t *testing.T) {
	for _, args := range [][]string{{"--version"}, {"completion", "bash"}, {"completion", "zsh"}, {"completion", "fish"}, {"completion", "powershell"}} {
		if err := RunCLI(args); err != nil {
			t.Fatalf("RunCLI(%q) error = %v", args, err)
		}
	}
}

func TestLockAndRestoreRequireOneSnapshotOperand(t *testing.T) {
	for _, asset := range cliAssets {
		for _, action := range []string{"lock", "restore"} {
			base := append(append([]string{}, asset...), action)
			if err := RunCLI(base); err == nil {
				t.Fatalf("RunCLI(%q) accepted missing snapshot", base)
			}
			args := append(base, "one", "two")
			if err := RunCLI(args); err == nil {
				t.Fatalf("RunCLI(%q) accepted extra snapshot", args)
			}
		}
	}
}

func TestRemovedCommandsAndFlagsAreRejected(t *testing.T) {
	for _, args := range [][]string{
		{"string", "sync"},
		{"string", "network", "sync"},
		{"interpro", "scan", "sync"},
		{"interpro", "scan", "fetch", "--should_allow_large_assets"},
		{"omnipath", "enz_sub"},
		{"uniprot", "idmapping"},
		{"manifest", "build", "--dir_in", "/tmp"},
		{"go", "ontology", "fetch", "--dir_out", "/tmp"},
		{"go", "ontology", "fetch", "--retry_wait_sec", "3"},
		{"go", "ontology", "fetch", "--should_dry_run"},
		{"manifest", "build", "/tmp", "--output", "/tmp", "--formats", "@assets.txt"},
	} {
		err := RunCLI(args)
		if err == nil {
			t.Fatalf("RunCLI(%q) unexpectedly succeeded", args)
		}
		if strings.Contains(strings.Join(args, " "), "@assets.txt") && !strings.Contains(err.Error(), "@file syntax") {
			t.Fatalf("RunCLI(%q) error = %v", args, err)
		}
	}
}

func TestNativeDurationAndListFileFlagsParse(t *testing.T) {
	for _, args := range [][]string{
		{"go", "ontology", "fetch", "--retry-wait", "350ms", "--request-interval", "1m", "--help"},
	} {
		err := RunCLI(args)
		if strings.Contains(strings.ToLower(errString(err)), "invalid argument") {
			t.Fatalf("RunCLI(%q) duration/list flag parse error = %v", args, err)
		}
	}
}

func TestNestedOmniPathRestoreUsesExactSnapshot(t *testing.T) {
	snapshot := t.TempDir() + "/resources/omnipath/interactions/kinaseextra/2025-08-13"
	err := RunCLI([]string{"omnipath", "interactions", "restore", snapshot, "--dry-run"})
	if err == nil || !strings.Contains(err.Error(), snapshot+"/manifest.lock") {
		t.Fatalf("nested restore error = %v", err)
	}
}

func TestFlatRestoresRejectMismatchedAssetPath(t *testing.T) {
	for _, asset := range cliAssets {
		if asset[0] == "omnipath" && asset[1] == "interactions" {
			continue
		}
		snapshot := t.TempDir() + "/resources/wrong/v1"
		args := append(append([]string{}, asset...), "restore", snapshot, "--dry-run")
		err := RunCLI(args)
		if err == nil || !strings.Contains(err.Error(), "must identify asset") {
			t.Fatalf("RunCLI(%q) mismatched snapshot error = %v", args, err)
		}
	}
}

func TestNestedOmniPathRestoreRejectsMismatchedAssetAndDataset(t *testing.T) {
	root := t.TempDir() + "/resources"
	for _, test := range []struct {
		snapshot string
		want     string
	}{
		{snapshot: root + "/wrong/kinaseextra/v1", want: "must identify"},
		{snapshot: root + "/interactions/not-kinaseextra/v1", want: "dataset must be kinaseextra"},
	} {
		err := RunCLI([]string{"omnipath", "interactions", "restore", test.snapshot, "--dry-run"})
		if err == nil || !strings.Contains(err.Error(), test.want) {
			t.Fatalf("nested restore %q error = %v, want %q", test.snapshot, err, test.want)
		}
	}
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
