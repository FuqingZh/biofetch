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
	command := newBuildCommand()
	var output bytes.Buffer
	command.SetOut(&output)
	command.SetArgs([]string{"--dir_in", root, "--dir_out", root, "--formats", "@" + fileFormats})
	if err := command.Execute(); err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	for _, extension := range []string{"toml", "json", "tsv"} {
		if _, err := os.Stat(filepath.Join(root, "manifest."+extension)); err != nil {
			t.Fatalf("%s output missing: %v", extension, err)
		}
	}
	if !strings.Contains(output.String(), "snapshots=0") {
		t.Fatalf("command output = %q", output.String())
	}
}

func TestBuildCommandRejectsRemovedFileStemFlag(t *testing.T) {
	command := newBuildCommand()
	command.SetArgs([]string{"--file_stem_out", filepath.Join(t.TempDir(), "manifest")})
	if err := command.Execute(); err == nil || !strings.Contains(err.Error(), "unknown flag") {
		t.Fatalf("Execute error = %v", err)
	}
}
