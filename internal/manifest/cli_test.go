package manifest

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuildCommandExpandsFormatsFile(t *testing.T) {
	root := t.TempDir()
	fileFormats := filepath.Join(root, "formats.txt")
	if err := os.WriteFile(fileFormats, []byte("# outputs\ntoml\njson\ntsv\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	stem := filepath.Join(root, "manifest")
	command := newBuildCommand()
	var output bytes.Buffer
	command.SetOut(&output)
	command.SetArgs([]string{"--dir_in", root, "--file_stem_out", stem, "--formats", "@" + fileFormats})
	if err := command.Execute(); err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	for _, extension := range []string{"toml", "json", "tsv"} {
		if _, err := os.Stat(stem + "." + extension); err != nil {
			t.Fatalf("%s output missing: %v", extension, err)
		}
	}
	if !strings.Contains(output.String(), "snapshots=0") {
		t.Fatalf("command output = %q", output.String())
	}
}
