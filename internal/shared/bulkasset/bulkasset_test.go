package bulkasset

import (
	"biofetch/internal/shared/staticasset"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestFetchWritesVersionedManifestForDefaultAssets(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Last-Modified", time.Date(2026, 7, 24, 0, 0, 0, 0, time.UTC).Format(http.TimeFormat))
		if request.Method == http.MethodHead {
			return
		}
		_, _ = writer.Write([]byte("payload"))
	}))
	defer server.Close()

	spec := Spec{
		Database:            "exampledb",
		Asset:               "database",
		Source:              "test",
		DatabaseDescription: "Manage example assets",
		AssetDescription:    "Manage example database",
		VersionDescription:  "Example release date",
		ResolveCurrent:      ResolveVersionFromLastModified(server.URL + "/default.tsv"),
		Assets: []AssetSpec{
			{Name: "default", Path: "default.tsv", URL: server.URL + "/default.tsv", Default: true},
			{Name: "optional", Path: "optional.tsv", URL: server.URL + "/optional.tsv"},
		},
	}
	dirOut := t.TempDir()
	command := NewCommand(spec)
	command.SetArgs([]string{"database", "fetch", "--output", dirOut, "--no-progress"})
	if err := command.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	manifest, ok, err := staticasset.ReadManifest(filepath.Join(dirOut, "database", "2026-07-24", "manifest.lock"))
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("manifest was not written")
	}
	if manifest.Database != "exampledb" || manifest.Asset != "database" || manifest.VersionToken != "2026-07-24" {
		t.Fatalf("manifest identity = %#v", manifest)
	}
	if len(manifest.Files) != 1 || manifest.Files[0].Path != "raw/default.tsv" {
		t.Fatalf("manifest files = %#v", manifest.Files)
	}
}

func TestResolveAssetsRejectsUnknownAndSupportsAll(t *testing.T) {
	available := []AssetSpec{
		{Name: "a", Default: true},
		{Name: "b"},
	}
	assets, err := resolveAssets(available, []string{"all"})
	if err != nil {
		t.Fatal(err)
	}
	if len(assets) != 2 {
		t.Fatalf("len(assets) = %d, want 2", len(assets))
	}
	if _, err := resolveAssets(available, []string{"missing"}); err == nil || !strings.Contains(err.Error(), "unknown asset") {
		t.Fatalf("unknown asset error = %v", err)
	}
}

func TestRequiredDefaultAssetsOnLockIsOptIn(t *testing.T) {
	source := buildSource(Spec{Database: "db", Asset: "asset", Assets: []AssetSpec{{Name: "core", Default: true}}}, "v1", nil)
	if len(source.RequiredAssets) != 0 {
		t.Fatalf("RequiredAssets = %#v, want empty", source.RequiredAssets)
	}
	source = buildSource(Spec{Database: "db", Asset: "asset", RequireDefaultAssetsOnLock: true, Assets: []AssetSpec{{Name: "core", Default: true}}}, "v1", nil)
	if len(source.RequiredAssets) != 1 || source.RequiredAssets[0] != "core" {
		t.Fatalf("RequiredAssets = %#v", source.RequiredAssets)
	}
}

func TestFixedVersionIsRejectedForCurrentOnlySource(t *testing.T) {
	_, err := resolveVersion(Spec{Database: "db", Asset: "asset"}, http.DefaultClient, "2026-01-01")
	if err == nil || !strings.Contains(err.Error(), "supports only current") {
		t.Fatalf("resolveVersion error = %v", err)
	}
}

func TestFetchRequiresOptInForLargeAsset(t *testing.T) {
	spec := Spec{
		Database:            "exampledb",
		Asset:               "database",
		DatabaseDescription: "Manage example assets",
		AssetDescription:    "Manage example database",
		VersionDescription:  "Example release",
		ResolveCurrent: func(*http.Client) (string, error) {
			return "v1", nil
		},
		Assets: []AssetSpec{{Name: "huge", Path: "huge.zip", URL: "https://example.test/huge.zip", Large: true}},
	}
	command := NewCommand(spec)
	command.SetArgs([]string{"database", "fetch", "--output", t.TempDir(), "--assets", "huge", "--dry-run"})
	err := command.Execute()
	if err == nil || !strings.Contains(err.Error(), "--allow-large-downloads") {
		t.Fatalf("Execute error = %v", err)
	}
}
