package complexportal

import (
	"archive/zip"
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/FuqingZh/biofetch/internal/shared/staticasset"
)

const testVersion = "2026-01-09"
const testTab = "#Complex ac\tRecommended name\tAliases for complex\tTaxonomy identifier\tIdentifiers (and stoichiometry) of molecules in complex\nCPX-1\tExample\t-\t9606\tP12345(1)|P67890(1)\n"
const testIndex = `<a href="?C=N;O=D">Sort</a><a href="../">Parent</a><a href="9606.tsv">Human</a><a href="9606_predicted.tsv">Predicted</a><a href="3702.tsv">Plant</a><a href="9606.tsv">Duplicate</a>`

type upstream struct {
	server *httptest.Server
	mu     sync.Mutex
	files  map[string][]byte
	hits   map[string]int
}

func fixture(t *testing.T) *upstream {
	t.Helper()
	f := &upstream{files: map[string][]byte{
		"/":                                []byte(`<a href="2025-12-01/">Older</a><a href="2026-01-09/">Latest</a><a href="2026-99-99/">Invalid</a><a href="current/">Moving</a>`),
		"/" + testVersion + "/complextab/": []byte(testIndex),
	}, hits: make(map[string]int)}
	for _, name := range []string{"9606.tsv", "9606_predicted.tsv", "3702.tsv"} {
		f.files["/"+testVersion+"/complextab/"+name] = []byte(testTab)
	}
	var archive bytes.Buffer
	writer := zip.NewWriter(&archive)
	entry, err := writer.Create("example.xml")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := entry.Write([]byte(`<entrySet><entry/></entrySet>`)); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	f.files["/"+testVersion+"/psi30/complexesMIF30.zip"] = archive.Bytes()
	f.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		f.hits[r.URL.Path]++
		data, ok := f.files[r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Length", fmt.Sprint(len(data)))
		if r.Method != http.MethodHead {
			_, _ = w.Write(data)
		}
	}))
	t.Cleanup(f.server.Close)
	return f
}

func (f *upstream) replace(path, content string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.files[path] = []byte(content)
}

func (f *upstream) count(path string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.hits[path]
}

func execute(base string, args ...string) error {
	command := newCommand(base)
	command.SilenceErrors, command.SilenceUsage = true, true
	command.SetArgs(args)
	return command.Execute()
}

func fetch(t *testing.T, f *upstream, root, asset string) string {
	t.Helper()
	if err := execute(f.server.URL+"/", asset, "fetch", "--output", root, "--version", testVersion, "--max-attempts", "1", "--no-progress"); err != nil {
		t.Fatal(err)
	}
	return filepath.Join(root, asset, testVersion)
}

func TestVersionResolutionAndPinnedDryRun(t *testing.T) {
	f := fixture(t)
	for _, value := range []string{"", "current"} {
		got, err := resolveVersion(f.server.Client(), f.server.URL+"/", value)
		if err != nil || got != testVersion {
			t.Fatalf("version = %q, %v", got, err)
		}
	}
	count := f.count("/")
	for _, invalid := range []string{"latest", "../2026-01-09", "2026-02-30", "2026-1-9", "2026-01-09/"} {
		if _, err := resolveVersion(f.server.Client(), f.server.URL+"/", invalid); err == nil {
			t.Fatalf("accepted version %q", invalid)
		}
	}
	root := filepath.Join(t.TempDir(), "not-created")
	if err := execute(f.server.URL+"/", "complextab", "fetch", "--output", root, "--version", testVersion, "--dry-run"); err != nil {
		t.Fatal(err)
	}
	if f.count("/") != count {
		t.Fatal("fixed version contacted latest discovery")
	}
	if _, err := os.Stat(root); !os.IsNotExist(err) {
		t.Fatalf("dry-run created snapshot: %v", err)
	}
	f.replace("/", `<a href="current/">Only moving release</a>`)
	if _, err := resolveVersion(f.server.Client(), f.server.URL+"/", "current"); err == nil {
		t.Fatal("accepted missing dated release")
	}
}

func TestInventoryRejectsInvalidAndIgnoresForeignLinks(t *testing.T) {
	base := "https://example.org/2026-01-09/complextab/"
	data := testIndex + `<a href="https://foreign.org/999.tsv">Foreign</a><a href="../999.tsv">Parent</a><a href="%2e%2e/999.tsv">Encoded</a><a href="9606.tsv?download=yes">Query</a>`
	names, err := tabInventory([]byte(data), base)
	if err != nil || strings.Join(names, ",") != "3702.tsv,9606.tsv,9606_predicted.tsv" {
		t.Fatalf("inventory %v, %v", names, err)
	}
	for _, bad := range []string{"", "<html>Maintenance</html>", `<a href="unexpected.tsv">New format</a>`} {
		if _, err := tabInventory([]byte(bad), base); err == nil {
			t.Fatalf("accepted inventory %q", bad)
		}
	}
	if _, err := boundedIndex(strings.NewReader(strings.Repeat("x", maxIndexBytes+1))); err == nil {
		t.Fatal("accepted oversized metadata")
	}
}

func TestFetchLockRestoreAndReuse(t *testing.T) {
	for _, asset := range []string{"complextab", "psi30"} {
		t.Run(asset, func(t *testing.T) {
			f := fixture(t)
			root := t.TempDir()
			snapshot := fetch(t, f, root, asset)
			manifest, ok, err := staticasset.ReadManifest(filepath.Join(snapshot, "manifest.lock"))
			wantFiles := 4
			file := "9606_predicted.tsv"
			if asset == "psi30" {
				wantFiles, file = 1, "complexesMIF30.zip"
			}
			if err != nil || !ok || len(manifest.Files) != wantFiles || manifest.VersionToken != testVersion || manifest.Source != sourceName {
				t.Fatalf("manifest %+v, %v", manifest, err)
			}
			for _, item := range manifest.Files {
				if !strings.Contains(item.URL, "/"+testVersion+"/"+asset+"/") || len(item.SHA256) != 64 || item.Bytes == 0 {
					t.Fatalf("invalid file record %+v", item)
				}
			}
			locked, err := os.ReadFile(filepath.Join(snapshot, "manifest.lock"))
			if err != nil {
				t.Fatal(err)
			}
			requestPath := "/" + testVersion + "/" + asset + "/" + file
			before := f.count(requestPath)
			fetch(t, f, root, asset)
			if f.count(requestPath) != before {
				t.Fatal("reused payload contacted upstream")
			}
			actual, err := os.ReadFile(filepath.Join(snapshot, "manifest.lock"))
			if err != nil || !bytes.Equal(actual, locked) {
				t.Fatal("repeat fetch changed authoritative lock")
			}
			if err := os.Remove(filepath.Join(snapshot, "raw", file)); err != nil {
				t.Fatal(err)
			}
			if err := execute(f.server.URL+"/", asset, "lock", snapshot); err == nil {
				t.Fatal("locked missing payload")
			}
			if err := execute(f.server.URL+"/", asset, "restore", snapshot, "--max-attempts", "1", "--no-progress"); err != nil {
				t.Fatal(err)
			}
			if _, err := os.Stat(filepath.Join(snapshot, "raw", file)); err != nil {
				t.Fatal(err)
			}
			f.server.Close()
			// No mutable inventory/version network access for local lock/restore.
			for _, action := range []string{"restore", "lock"} {
				if err := execute(f.server.URL+"/", asset, action, snapshot); err != nil {
					t.Fatalf("offline %s: %v", action, err)
				}
			}
		})
	}
}

func TestRestoreMissingInventoryAndDrift(t *testing.T) {
	f := fixture(t)
	snapshot := fetch(t, f, t.TempDir(), "complextab")
	path := filepath.Join(snapshot, "raw", "index.html")
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := execute(f.server.URL+"/", "complextab", "restore", snapshot, "--max-attempts", "1", "--no-progress"); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(snapshot, "raw", "9606.tsv")
	before, _ := os.ReadFile(target)
	lockBefore, _ := os.ReadFile(filepath.Join(snapshot, "manifest.lock"))
	f.replace("/"+testVersion+"/complextab/9606.tsv", strings.ReplaceAll(testTab, "Example", "Changed"))
	for _, action := range []string{"restore", "fetch"} {
		args := []string{"complextab", action}
		if action == "restore" {
			args = append(args, snapshot)
		} else {
			args = append(args, "--output", filepath.Dir(filepath.Dir(snapshot)), "--version", testVersion)
		}
		args = append(args, "--on-existing", "overwrite", "--max-attempts", "1", "--no-progress", "--workers", "1")
		if err := execute(f.server.URL+"/", args...); err == nil {
			t.Fatalf("%s accepted changed payload", action)
		}
		after, _ := os.ReadFile(target)
		lockAfter, _ := os.ReadFile(filepath.Join(snapshot, "manifest.lock"))
		if !bytes.Equal(before, after) || !bytes.Equal(lockBefore, lockAfter) {
			t.Fatal("drift modified original payload or lock")
		}
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	f.replace("/"+testVersion+"/complextab/", testIndex+"<!-- changed -->")
	if err := execute(f.server.URL+"/", "complextab", "restore", snapshot); err == nil {
		t.Fatal("accepted changed inventory")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("published changed inventory")
	}
}

func TestInvalidPayloadPublishesNoLock(t *testing.T) {
	for _, asset := range []string{"complextab", "psi30"} {
		t.Run(asset, func(t *testing.T) {
			f := fixture(t)
			name := "9606_predicted.tsv"
			if asset == "psi30" {
				name = "complexesMIF30.zip"
			}
			f.replace("/"+testVersion+"/"+asset+"/"+name, "<html>Service unavailable</html>")
			root := t.TempDir()
			err := execute(f.server.URL+"/", asset, "fetch", "--output", root, "--version", testVersion, "--max-attempts", "1", "--no-progress")
			if err == nil {
				t.Fatal("accepted HTML payload")
			}
			if _, err := os.Stat(filepath.Join(root, asset, testVersion, "manifest.lock")); !os.IsNotExist(err) {
				t.Fatalf("incomplete snapshot published lock: %v", err)
			}
		})
	}
}

func TestLegacyIncompleteAndTamperedLocksRejected(t *testing.T) {
	f := fixture(t)
	for _, mutation := range []string{"omit-predicted", "foreign-url", "wrong-version", "bad-digest", "duplicate"} {
		t.Run(mutation, func(t *testing.T) {
			snapshot := fetch(t, f, t.TempDir(), "complextab")
			path := filepath.Join(snapshot, "manifest.lock")
			data, _ := os.ReadFile(path)
			text := string(data)
			switch mutation {
			case "omit-predicted":
				blocks := strings.Split(text, "[[files]]")
				var keep []string
				for _, block := range blocks {
					if !strings.Contains(block, "9606_predicted") {
						keep = append(keep, block)
					}
				}
				text = strings.Join(keep, "[[files]]")
			case "foreign-url":
				text = strings.ReplaceAll(text, f.server.URL+"/"+testVersion+"/complextab/9606.tsv", "https://foreign.invalid/9606.tsv")
			case "wrong-version":
				text = strings.Replace(text, "version = '"+testVersion+"'", "version = '2020-01-01'", 1)
			case "bad-digest":
				text = strings.Replace(text, "sha256 = '", "sha256 = 'z", 1)
			case "duplicate":
				blocks := strings.Split(text, "[[files]]")
				text += "[[files]]" + blocks[len(blocks)-1]
			}
			if text == string(data) {
				t.Fatal("counterexample did not mutate lock")
			}
			if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := execute(f.server.URL+"/", "complextab", "restore", snapshot, "--dry-run"); err == nil {
				t.Fatalf("accepted %s", mutation)
			}
		})
	}
}

func TestSnapshotSymlinkRejected(t *testing.T) {
	root := t.TempDir()
	snapshot := filepath.Join(root, "complextab", testVersion)
	if err := os.MkdirAll(snapshot, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(t.TempDir(), filepath.Join(snapshot, "raw")); err != nil {
		t.Fatal(err)
	}
	err := execute(baseURL, "complextab", "lock", snapshot)
	if err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("symlink error: %v", err)
	}
}
