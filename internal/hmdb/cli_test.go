package hmdb

import (
	"archive/zip"
	"biofetch/internal/shared/httpx"
	"biofetch/internal/shared/staticasset"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeZIP(t *testing.T, path, member string) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	z := zip.NewWriter(f)
	w, err := z.Create(member)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = w.Write([]byte("x"))
	if err := z.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestLockRequiresHMDBCoreAssets(t *testing.T) {
	dir := t.TempDir()
	raw := filepath.Join(dir, "hmdb", "database", "5.0", "raw")
	if err := os.MkdirAll(raw, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, item := range []struct{ file, member string }{{"hmdb_metabolites.zip", "hmdb_metabolites.xml"}, {"hmdb_proteins.zip", "hmdb_proteins.xml"}, {"structures.zip", "structures.sdf"}} {
		writeZIP(t, filepath.Join(raw, item.file), item.member)
	}
	cmd := NewCommand()
	cmd.SetArgs([]string{"database", "lock", filepath.Dir(raw)})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	manifest, ok, err := staticasset.ReadManifest(filepath.Join(filepath.Dir(raw), "manifest.lock"))
	if err != nil || !ok {
		t.Fatalf("ReadManifest = %v, %v", err, ok)
	}
	if manifest.Database != "hmdb" || manifest.Asset != "database" || manifest.VersionToken != "5.0" {
		t.Fatalf("identity = %#v", manifest)
	}
	if len(manifest.Files) != 3 {
		t.Fatalf("files = %#v", manifest.Files)
	}
	want := map[string]string{"raw/hmdb_metabolites.zip": baseURL + "/hmdb_metabolites.zip", "raw/hmdb_proteins.zip": baseURL + "/hmdb_proteins.zip", "raw/structures.zip": baseURL + "/structures.zip"}
	for _, file := range manifest.Files {
		if want[file.Path] != file.URL || file.Bytes <= 0 || file.SHA256 == "" {
			t.Fatalf("file = %#v", file)
		}
	}
	if err := os.Remove(filepath.Join(raw, "structures.zip")); err != nil {
		t.Fatal(err)
	}
	missing := NewCommand()
	missing.SetArgs([]string{"database", "lock", filepath.Dir(raw), "--dry-run"})
	if err := missing.Execute(); err == nil || !strings.Contains(err.Error(), "required asset") {
		t.Fatalf("missing core error = %v", err)
	}
}

func TestRecoverAuthorizationFailureOptionalAsset(t *testing.T) {
	_, err := recoverAuthorizationFailure("/tmp/hmdb/database/5.0/raw/fasta_proteins.zip", httpx.UnexpectedStatusError{Code: http.StatusForbidden, Status: "403 Forbidden"})
	if err == nil || !strings.Contains(err.Error(), "browser") || !strings.Contains(err.Error(), "lock") {
		t.Fatalf("error = %v", err)
	}
}
