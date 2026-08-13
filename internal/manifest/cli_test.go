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
	command.SetArgs([]string{root, "--output", root, "--formats-file", fileFormats})
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

func TestBuildCommandFormatsFileOverridesDefault(t *testing.T) {
	resourceRoot := t.TempDir()
	outputRoot := t.TempDir()
	fileFormats := filepath.Join(t.TempDir(), "formats.txt")
	if err := os.WriteFile(fileFormats, []byte("json\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	command := newBuildCommand()
	command.SetArgs([]string{resourceRoot, "--output", outputRoot, "--formats-file", fileFormats})
	if err := command.Execute(); err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if _, err := os.Stat(filepath.Join(outputRoot, "manifest.json")); err != nil {
		t.Fatalf("JSON output missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(outputRoot, "manifest.toml")); !os.IsNotExist(err) {
		t.Fatalf("unexpected TOML output stat error = %v", err)
	}
}

func TestBuildCommandWorkersContract(t *testing.T) {
	command := newBuildCommand()
	flag := command.Flags().Lookup("workers")
	if flag == nil {
		t.Fatal("workers flag missing")
	}
	if flag.DefValue != "4" {
		t.Fatalf("workers default = %q, want 4", flag.DefValue)
	}
	if !strings.Contains(flag.Usage, "reading and validating child manifests") {
		t.Fatalf("workers usage = %q", flag.Usage)
	}

	root := t.TempDir()
	command = newBuildCommand()
	command.SetArgs([]string{root, "--output", root, "--workers", "0"})
	if err := command.Execute(); err == nil || !strings.Contains(err.Error(), "workers must be >= 1") {
		t.Fatalf("workers=0 error = %v", err)
	}

	command = newBuildCommand()
	command.SetArgs([]string{root, "--output", root, "--workers", "65"})
	if err := command.Execute(); err == nil || !strings.Contains(err.Error(), "workers must be <= 64") {
		t.Fatalf("workers=65 error = %v", err)
	}
}
