package archiveverify

import (
	"archive/zip"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestZIPRequiredMember(t *testing.T) {
	valid := filepath.Join(t.TempDir(), "valid.zip")
	file, err := os.Create(valid)
	if err != nil {
		t.Fatal(err)
	}
	writer := zip.NewWriter(file)
	member, err := writer.Create("required.xml")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := member.Write([]byte("content")); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	if err := ZIPRequiredMember("required.xml")(valid); err != nil {
		t.Fatalf("valid ZIP rejected: %v", err)
	}

	for name, content := range map[string][]byte{
		"html.zip":      []byte("<html>challenge</html>"),
		"truncated.zip": []byte("PK\x03\x04"),
	} {
		path := filepath.Join(t.TempDir(), name)
		if err := os.WriteFile(path, content, 0o644); err != nil {
			t.Fatal(err)
		}
		if err := ZIPRequiredMember("required.xml")(path); err == nil {
			t.Fatalf("%s unexpectedly passed", name)
		}
	}
}

func TestZIPRequiredMemberRejectsWrongMemberAndSymlink(t *testing.T) {
	path := filepath.Join(t.TempDir(), "wrong.zip")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	writer := zip.NewWriter(file)
	if _, err := writer.Create("wrong.xml"); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	if err := ZIPRequiredMember("required.xml")(path); err == nil || !strings.Contains(err.Error(), "missing required member") {
		t.Fatalf("wrong-member error = %v", err)
	}
	link := filepath.Join(t.TempDir(), "link.zip")
	if err := os.Symlink(path, link); err != nil {
		t.Fatal(err)
	}
	if err := ZIPRequiredMember("required.xml")(link); err == nil || !strings.Contains(err.Error(), "not a regular file") {
		t.Fatalf("symlink error = %v", err)
	}
}
