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
