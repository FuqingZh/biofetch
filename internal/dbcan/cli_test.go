package dbcan

import (
	"github.com/FuqingZh/biofetch/internal/shared/bulkasset"
	"github.com/FuqingZh/biofetch/internal/shared/staticasset"
	"github.com/FuqingZh/biofetch/internal/shared/tomlx"
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/spf13/cobra"
)

var fixtureBodies = map[string][]byte{
	"CAZy.dmnd":                 []byte("cazy-db"),
	"dbCAN.hmm":                 []byte("family-hmm"),
	"dbCAN_sub.hmm":             []byte("subfamily-hmm"),
	"fam-substrate-mapping.tsv": []byte("family\tsubstrate\n"),
}

func TestPinnedReleaseInventory(t *testing.T) {
	spec := newSpec(baseURL, pinnedSizes)
	if spec.Database != "dbcan" || spec.Asset != "database" || spec.Source != SourceName || spec.FixedVersion != VersionToken || spec.SourceVersion != SourceVersion {
		t.Fatalf("spec identity = %#v", spec)
	}
	if !spec.RequireCompleteAssets || !spec.RejectUndeclaredAssets || !spec.LockOnlyDeclaredAssets || !spec.DisableAssetSelection || spec.DefaultWorkers != 1 {
		t.Fatalf("spec safety = %#v", spec)
	}
	if len(spec.Assets) != 4 || TotalBytes != 7_439_565_906 {
		t.Fatalf("assets=%d total=%d", len(spec.Assets), TotalBytes)
	}
	want := []struct {
		name, path, remote string
		bytes              int64
	}{
		{"cazy-diamond", "CAZy.dmnd", "CAZy.dmnd", bytesCAZyDiamond},
		{"dbcan-hmm", "dbCAN.hmm", "dbCAN.hmm", bytesDBCANHMM},
		{"dbcan-sub-hmm", "dbCAN-sub.hmm", "dbCAN_sub.hmm", bytesDBCANSubHMM},
		{"family-substrate-mapping", "fam-substrate-mapping.tsv", "fam-substrate-mapping.tsv", bytesFamilySubstrateMapping},
	}
	for index, expected := range want {
		asset := spec.Assets[index]
		if asset.Name != expected.name || asset.Path != expected.path || asset.URL != baseURL+"/"+expected.remote || asset.ExpectedBytes != expected.bytes || !asset.Default || !asset.Large {
			t.Fatalf("asset[%d] = %#v", index, asset)
		}
	}
}

func TestFetchWritesCompletePinnedManifest(t *testing.T) {
	bodies := cloneBodies()
	server, _ := newFixtureServer(t, bodies, "")
	spec := fixtureSpec(server.URL, bodies)
	dirOut := t.TempDir()
	command := newCommandWithOutput(spec)
	command.SetArgs([]string{"database", "fetch", "--output", dirOut, "--allow-large-downloads", "--no-progress", "--max-attempts", "1"})
	if err := command.Execute(); err != nil {
		t.Fatalf("fetch: %v", err)
	}

	manifest, ok, err := staticasset.ReadManifest(filepath.Join(dirOut, "database", VersionToken, "manifest.lock"))
	if err != nil || !ok {
		t.Fatalf("manifest ok=%v err=%v", ok, err)
	}
	if manifest.Database != "dbcan" || manifest.Asset != "database" || manifest.Source != SourceName || manifest.Version != SourceVersion || manifest.VersionToken != VersionToken || len(manifest.Files) != 4 {
		t.Fatalf("manifest = %#v", manifest)
	}
	for _, file := range manifest.Files {
		if file.SHA256 == "" || file.Bytes <= 0 || !strings.HasPrefix(file.Path, "raw/") || !strings.HasPrefix(file.URL, server.URL+"/") {
			t.Fatalf("file = %#v", file)
		}
	}
	assertRawNames(t, filepath.Join(dirOut, "database", VersionToken, "raw"))
}

func TestFetchFailureLeavesNoPartialManifest(t *testing.T) {
	bodies := cloneBodies()
	server, _ := newFixtureServer(t, bodies, "fam-substrate-mapping.tsv")
	dirOut := t.TempDir()
	command := newCommandWithOutput(fixtureSpec(server.URL, bodies))
	command.SetArgs([]string{"database", "fetch", "--output", dirOut, "--allow-large-downloads", "--no-progress", "--max-attempts", "1"})
	if err := command.Execute(); err == nil {
		t.Fatal("fetch unexpectedly succeeded")
	}
	snapshot := filepath.Join(dirOut, "database", VersionToken)
	if _, err := os.Stat(filepath.Join(snapshot, "manifest.lock")); !os.IsNotExist(err) {
		t.Fatalf("partial manifest exists: %v", err)
	}
	if _, err := os.Stat(filepath.Join(snapshot, "raw", "CAZy.dmnd")); err != nil {
		t.Fatalf("verified earlier asset missing: %v", err)
	}
}

func TestFetchRejectsVersionAliasesBeforeRequestsOrDirectories(t *testing.T) {
	bodies := cloneBodies()
	server, requests := newFixtureServer(t, bodies, "")
	for _, version := range []string{"current", "latest", "db_current", "other"} {
		t.Run(version, func(t *testing.T) {
			dirOut := filepath.Join(t.TempDir(), "not-created")
			command := newCommandWithOutput(fixtureSpec(server.URL, bodies))
			command.SetArgs([]string{"database", "fetch", "--output", dirOut, "--version", version, "--allow-large-downloads", "--dry-run"})
			err := command.Execute()
			if err == nil || !strings.Contains(err.Error(), VersionToken) {
				t.Fatalf("version %q error = %v", version, err)
			}
			if _, statErr := os.Stat(dirOut); !os.IsNotExist(statErr) {
				t.Fatalf("output created: %v", statErr)
			}
		})
	}
	if requests.Load() != 0 {
		t.Fatalf("requests = %d, want 0", requests.Load())
	}
}

func TestLockRequiresExactDeclaredLayoutAndToken(t *testing.T) {
	bodies := cloneBodies()
	spec := fixtureSpec("https://example.test/fixed", bodies)
	for _, test := range []struct {
		name, token, omit, extra, want string
	}{
		{name: "wrong token", token: "other", want: "supports only fixed version"},
		{name: "missing", token: VersionToken, omit: "dbCAN.hmm", want: "required asset"},
		{name: "extra", token: VersionToken, extra: "pressed.h3f", want: "undeclared raw file"},
	} {
		t.Run(test.name, func(t *testing.T) {
			snapshot := filepath.Join(t.TempDir(), "database", test.token)
			raw := filepath.Join(snapshot, "raw")
			if err := os.MkdirAll(raw, 0o755); err != nil {
				t.Fatal(err)
			}
			for remote, body := range bodies {
				local := remote
				if remote == "dbCAN_sub.hmm" {
					local = "dbCAN-sub.hmm"
				}
				if local == test.omit {
					continue
				}
				if err := os.WriteFile(filepath.Join(raw, local), body, 0o644); err != nil {
					t.Fatal(err)
				}
			}
			if test.extra != "" {
				if err := os.WriteFile(filepath.Join(raw, test.extra), []byte("extra"), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			command := newCommandWithOutput(spec)
			command.SetArgs([]string{"database", "lock", snapshot})
			err := command.Execute()
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("lock error = %v, want %q", err, test.want)
			}
			if _, statErr := os.Stat(filepath.Join(snapshot, "manifest.lock")); !os.IsNotExist(statErr) {
				t.Fatalf("invalid lock exists: %v", statErr)
			}
		})
	}
}

func TestRestoreUsesExactManifestAndKeepsLockImmutable(t *testing.T) {
	bodies := cloneBodies()
	server, _ := newFixtureServer(t, bodies, "")
	spec := fixtureSpec(server.URL, bodies)
	dirOut := t.TempDir()
	command := newCommandWithOutput(spec)
	command.SetArgs([]string{"database", "fetch", "--output", dirOut, "--allow-large-downloads", "--no-progress", "--max-attempts", "1"})
	if err := command.Execute(); err != nil {
		t.Fatal(err)
	}
	snapshot := filepath.Join(dirOut, "database", VersionToken)
	lockPath := filepath.Join(snapshot, "manifest.lock")
	lockBefore, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(filepath.Join(snapshot, "raw")); err != nil {
		t.Fatal(err)
	}
	restore := newCommandWithOutput(spec)
	restore.SetArgs([]string{"database", "restore", snapshot, "--no-progress", "--max-attempts", "1"})
	if err := restore.Execute(); err != nil {
		t.Fatalf("restore: %v", err)
	}
	lockAfter, err := os.ReadFile(lockPath)
	if err != nil || !bytes.Equal(lockAfter, lockBefore) {
		t.Fatalf("lock changed after restore: err=%v", err)
	}

	bodies["dbCAN.hmm"] = []byte("changed!!!")
	if err := os.Remove(filepath.Join(snapshot, "raw", "dbCAN.hmm")); err != nil {
		t.Fatal(err)
	}
	drift := newCommandWithOutput(spec)
	drift.SetArgs([]string{"database", "restore", snapshot, "--no-progress", "--max-attempts", "1"})
	err = drift.Execute()
	if err == nil || !strings.Contains(err.Error(), "SHA256 mismatch") {
		t.Fatalf("drift restore error = %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(snapshot, "raw", "dbCAN.hmm")); !os.IsNotExist(statErr) {
		t.Fatalf("drifted final file exists: %v", statErr)
	}
	if _, statErr := os.Stat(filepath.Join(snapshot, "raw", "dbCAN.hmm.part")); !os.IsNotExist(statErr) {
		t.Fatalf("drifted part exists: %v", statErr)
	}
	lockAfter, _ = os.ReadFile(lockPath)
	if !bytes.Equal(lockAfter, lockBefore) {
		t.Fatal("lock changed after drift failure")
	}
}

func TestRestoreRejectsMalformedManifestBeforeNetworkOrRawMutation(t *testing.T) {
	bodies := cloneBodies()
	server, requests := newFixtureServer(t, bodies, "")
	spec := fixtureSpec(server.URL, bodies)
	seedOut := t.TempDir()
	seed := newCommandWithOutput(spec)
	seed.SetArgs([]string{"database", "fetch", "--output", seedOut, "--allow-large-downloads", "--no-progress", "--max-attempts", "1"})
	if err := seed.Execute(); err != nil {
		t.Fatal(err)
	}
	valid, ok, err := staticasset.ReadManifest(filepath.Join(seedOut, "database", VersionToken, "manifest.lock"))
	if err != nil || !ok {
		t.Fatalf("seed manifest ok=%v err=%v", ok, err)
	}

	cases := map[string]func(*staticasset.Manifest){
		"partial":                func(manifest *staticasset.Manifest) { manifest.Files = manifest.Files[:3] },
		"wrong URL":              func(manifest *staticasset.Manifest) { manifest.Files[0].URL = "https://example.invalid/drift" },
		"duplicate":              func(manifest *staticasset.Manifest) { manifest.Files = append(manifest.Files, manifest.Files[0]) },
		"unsafe path":            func(manifest *staticasset.Manifest) { manifest.Files[0].Path = "raw/../escape" },
		"invalid SHA":            func(manifest *staticasset.Manifest) { manifest.Files[0].SHA256 = "invalid" },
		"wrong source":           func(manifest *staticasset.Manifest) { manifest.Source = "other" },
		"wrong software version": func(manifest *staticasset.Manifest) { manifest.Version = "other" },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			manifest := valid
			manifest.Files = append([]staticasset.ManifestFileRecord(nil), valid.Files...)
			mutate(&manifest)
			snapshot := filepath.Join(t.TempDir(), "database", VersionToken)
			if err := os.MkdirAll(snapshot, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := tomlx.WriteFileAtomic(filepath.Join(snapshot, "manifest.lock"), manifest); err != nil {
				t.Fatal(err)
			}
			before := requests.Load()
			restore := newCommandWithOutput(spec)
			restore.SetArgs([]string{"database", "restore", snapshot, "--no-progress", "--max-attempts", "1"})
			if err := restore.Execute(); err == nil {
				t.Fatal("malformed manifest restore succeeded")
			}
			if requests.Load() != before {
				t.Fatalf("requests changed from %d to %d", before, requests.Load())
			}
			if _, statErr := os.Stat(filepath.Join(snapshot, "raw")); !os.IsNotExist(statErr) {
				t.Fatalf("raw mutated before validation: %v", statErr)
			}
		})
	}
}

func TestCommandContract(t *testing.T) {
	command := NewCommand()
	asset, _, err := command.Find([]string{"database"})
	if err != nil {
		t.Fatal(err)
	}
	commands := asset.Commands()
	if len(commands) != 3 || commands[0].Name() != "fetch" || commands[1].Name() != "lock" || commands[2].Name() != "restore" {
		t.Fatalf("database commands = %v, want fetch|lock|restore only", commands)
	}
	fetch, _, err := command.Find([]string{"database", "fetch"})
	if err != nil {
		t.Fatal(err)
	}
	if fetch.Flags().Lookup("assets") != nil || fetch.Flags().Lookup("cgc") != nil || fetch.Flags().Lookup("no-cgc") != nil {
		t.Fatal("dbCAN fetch exposes subset or CGC flags")
	}
	for flag, value := range map[string]string{"version": VersionToken, "workers": "1", "allow-large-downloads": "false"} {
		found := fetch.Flags().Lookup(flag)
		if found == nil || found.DefValue != value {
			t.Fatalf("flag %s = %#v, want default %s", flag, found, value)
		}
	}
	var output bytes.Buffer
	command.SetOut(&output)
	command.SetErr(&output)
	command.SetArgs([]string{"database", "fetch", "--help"})
	if err := command.Execute(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), VersionToken) {
		t.Fatalf("fetch help omits fixed token: %s", output.String())
	}
}

func fixtureSpec(url string, bodies map[string][]byte) bulkasset.Spec {
	return newSpec(url, releaseSizes{
		cazyDiamond: int64(len(bodies["CAZy.dmnd"])), dbcanHMM: int64(len(bodies["dbCAN.hmm"])),
		dbcanSubHMM: int64(len(bodies["dbCAN_sub.hmm"])), familySubstrateMapping: int64(len(bodies["fam-substrate-mapping.tsv"])),
	})
}

func newCommandWithOutput(spec bulkasset.Spec) *cobra.Command {
	command := bulkasset.NewCommand(spec)
	command.SetOut(&bytes.Buffer{})
	command.SetErr(&bytes.Buffer{})
	return command
}

func newFixtureServer(t *testing.T, bodies map[string][]byte, fail string) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	requests := &atomic.Int32{}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		requests.Add(1)
		name := strings.TrimPrefix(request.URL.Path, "/")
		body, ok := bodies[name]
		if !ok {
			http.NotFound(writer, request)
			return
		}
		if name == fail {
			http.Error(writer, "failed", http.StatusServiceUnavailable)
			return
		}
		writer.Header().Set("Content-Length", fmt.Sprint(len(body)))
		writer.Header().Set("ETag", `"`+name+`-fixture"`)
		if request.Method == http.MethodHead {
			return
		}
		_, _ = writer.Write(body)
	}))
	return server, requests
}

func cloneBodies() map[string][]byte {
	result := make(map[string][]byte, len(fixtureBodies))
	for name, body := range fixtureBodies {
		result[name] = append([]byte(nil), body...)
	}
	return result
}

func assertRawNames(t *testing.T, raw string) {
	t.Helper()
	entries, err := os.ReadDir(raw)
	if err != nil {
		t.Fatal(err)
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	want := []string{"CAZy.dmnd", "dbCAN-sub.hmm", "dbCAN.hmm", "fam-substrate-mapping.tsv"}
	if fmt.Sprint(names) != fmt.Sprint(want) {
		t.Fatalf("raw names = %v, want %v", names, want)
	}
}
