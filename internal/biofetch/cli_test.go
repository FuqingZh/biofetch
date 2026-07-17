package biofetch

import (
	"strings"
	"testing"
)

func TestRemovedDerivedAssetCommandsAreUnavailable(t *testing.T) {
	for _, args := range [][]string{{"subcell"}, {"uniprot", "dmnd"}} {
		err := RunCLI(args)
		if err == nil || !strings.Contains(err.Error(), "unknown command") {
			t.Fatalf("RunCLI(%q) error = %v", args, err)
		}
	}
}

func TestManifestBuildHelpIsAvailable(t *testing.T) {
	if err := RunCLI([]string{"manifest", "build", "--help"}); err != nil {
		t.Fatalf("RunCLI manifest build help returned error: %v", err)
	}
}

func TestLockCommandsUseSnapshotDirectoryFlag(t *testing.T) {
	commands := [][]string{
		{"eggnog", "cog", "lock"},
		{"eggnog", "mapper", "lock"},
		{"go", "annotation", "lock"},
		{"go", "ontology", "lock"},
		{"go", "slim", "lock"},
		{"interpro", "mapping", "lock"},
		{"kegg", "brite", "lock"},
		{"kegg", "catalog", "lock"},
		{"kegg", "mapping", "lock"},
		{"kegg", "pathway", "lock"},
		{"omnipath", "enz_sub", "lock"},
		{"omnipath", "interactions", "lock"},
		{"reactome", "mapping", "lock"},
		{"string", "lock"},
		{"string", "catalog", "lock"},
		{"uniprot", "idmapping", "lock"},
		{"uniprot", "kb", "lock"},
		{"uniprot", "uniref", "lock"},
		{"wikipathways", "gmt", "lock"},
	}
	for _, command := range commands {
		argsHelp := append(append([]string{}, command...), "--dir_snapshot", "/tmp/snapshot", "--help")
		if err := RunCLI(argsHelp); err != nil {
			t.Fatalf("RunCLI(%q) error = %v", argsHelp, err)
		}
		for _, oldFlag := range []string{"--dir_out", "--version"} {
			argsOld := append(append([]string{}, command...), oldFlag, "value")
			err := RunCLI(argsOld)
			if err == nil || !strings.Contains(err.Error(), "unknown flag") {
				t.Fatalf("RunCLI(%q) error = %v", argsOld, err)
			}
		}
	}
}
