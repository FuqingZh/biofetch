package hmdb

import (
	"archive/zip"
	"biofetch/internal/shared/httpx"
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
	cmd.SetArgs([]string{"database", "lock", filepath.Dir(raw), "--dry-run"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	os.Remove(filepath.Join(raw, "structures.zip"))
	if err := cmd.Execute(); err == nil || !strings.Contains(err.Error(), "required asset") {
		t.Fatalf("missing core error = %v", err)
	}
}

func TestRecoverAuthorizationFailureOptionalAsset(t *testing.T) {
	_, err := recoverAuthorizationFailure("/tmp/hmdb/database/5.0/raw/fasta_proteins.zip", httpx.UnexpectedStatusError{Code: http.StatusForbidden, Status: "403 Forbidden"})
	if err == nil || !strings.Contains(err.Error(), "browser") || !strings.Contains(err.Error(), "lock") {
		t.Fatalf("error = %v", err)
	}
}
