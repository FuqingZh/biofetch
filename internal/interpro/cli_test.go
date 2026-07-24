package interpro

import (
	"bytes"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestScanSnapshotHelpExplainsExactLayout(t *testing.T) {
	for _, action := range []string{"lock", "restore"} {
		command := NewCommand()
		var output bytes.Buffer
		command.SetOut(&output)
		command.SetErr(&output)
		command.SetArgs([]string{"scan", action, "--help"})
		if err := command.Execute(); err != nil {
			t.Fatalf("%s help returned error: %v", action, err)
		}
		text := output.String()
		for _, expected := range []string{
			"SNAPSHOT is the exact version directory",
			"<root>/scan/<version>/",
			"biofetch interpro scan " + action + " /data/interpro/scan/5.77-108.0",
		} {
			if !strings.Contains(text, expected) {
				t.Fatalf("%s help missing %q:\n%s", action, expected, text)
			}
		}
	}
}

func TestScanSnapshotCompletionFiltersDirectories(t *testing.T) {
	for _, command := range []*cobra.Command{createScanLockCommand(), createScanRestoreCommand()} {
		_, directive := command.ValidArgsFunction(command, nil, "")
		if directive != cobra.ShellCompDirectiveFilterDirs {
			t.Fatalf("%s completion directive = %v", command.Name(), directive)
		}
	}
}
