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

func TestFixedVersionCollectionDisablesAssetSelectionAndRejectsAliases(t *testing.T) {
	const fixed = "db_v5-2-9_5-5-2026"
	spec := Spec{
		Database: "dbcan", Asset: "database", FixedVersion: fixed, SourceVersion: "5.2.9",
		DisableAssetSelection: true, RequireCompleteAssets: true,
		Assets: []AssetSpec{{Name: "core", Path: "core.hmm", URL: "https://example.test/" + fixed + "/core.hmm", Default: true, ExpectedBytes: 7}},
	}
	command := NewCommand(spec)
	fetch, _, err := command.Find([]string{"database", "fetch"})
	if err != nil {
		t.Fatal(err)
	}
	if fetch.Flags().Lookup("assets") != nil {
		t.Fatal("fixed complete collection exposes --assets")
	}
	versionFlag := fetch.Flags().Lookup("version")
	if versionFlag == nil || versionFlag.DefValue != fixed {
		t.Fatalf("version default = %#v, want %s", versionFlag, fixed)
	}
	for _, alias := range []string{"current", "latest", "db_current", "other"} {
		if _, err := resolveVersion(spec, http.DefaultClient, alias); err == nil || !strings.Contains(err.Error(), fixed) {
			t.Fatalf("resolveVersion(%q) error = %v", alias, err)
		}
	}
	if got, err := resolveVersion(spec, http.DefaultClient, ""); err != nil || got != fixed {
		t.Fatalf("resolveVersion(empty) = %q, %v", got, err)
	}
}

func TestBuildSourceCarriesCompleteCollectionContract(t *testing.T) {
	spec := Spec{
		Database: "dbcan", Asset: "database", Source: "run-dbcan-s3", SourceVersion: "5.2.9",
		RequireCompleteAssets: true, RejectUndeclaredAssets: true, LockOnlyDeclaredAssets: true,
		RequireDefaultAssetsOnLock: true,
		Assets:                     []AssetSpec{{Name: "core", Path: "core.hmm", URL: "https://example.test/{version}/core.hmm", Default: true, ExpectedBytes: 7}},
	}
	source := buildSource(spec, "fixed", spec.Assets)
	if source.Version != "5.2.9" || !source.RequireCompleteAssets || !source.RejectUndeclaredAssets {
		t.Fatalf("source contract = %#v", source)
	}
	if len(source.Assets) != 1 || source.Assets[0].ExpectedBytes != 7 || source.Assets[0].URL != "https://example.test/fixed/core.hmm" {
		t.Fatalf("source assets = %#v", source.Assets)
	}
}
