package geneontology

import (
	"bytes"
	"reflect"
	"strings"
	"testing"
	"time"

	toml "github.com/pelletier/go-toml/v2"
)

func TestParseOntologyAssetNames(t *testing.T) {
	assets, err := parseOntologyAssetNames([]string{"go-basic.obo", "go.obo,go-basic.obo"})
	if err != nil {
		t.Fatalf("parseOntologyAssetNames returned error: %v", err)
	}

	expected := []string{"go-basic.obo", "go.obo"}
	if !reflect.DeepEqual(assets, expected) {
		t.Fatalf("parseOntologyAssetNames = %#v, want %#v", assets, expected)
	}
}

func TestParseOntologyAssetsFromIndex(t *testing.T) {
	data := []byte(`
<html>
  <body>
    <a href="../">Parent</a>
    <a href="extensions/">extensions</a>
    <a href="go-base.owl">go-base.owl</a>
    <a href="go-basic.json">go-basic.json</a>
    <a href="go-basic.obo">go-basic.obo</a>
    <a href="go.obo">go.obo</a>
  </body>
</html>
`)

	assets, err := parseOntologyAssetsFromIndex(data)
	if err != nil {
		t.Fatalf("parseOntologyAssetsFromIndex returned error: %v", err)
	}

	names := make([]string, 0, len(assets))
	for _, asset := range assets {
		names = append(names, asset.name)
	}

	expected := []string{"go-base.owl", "go-basic.json", "go-basic.obo", "go.obo"}
	if !reflect.DeepEqual(names, expected) {
		t.Fatalf("parseOntologyAssetsFromIndex = %#v, want %#v", names, expected)
	}
}

func TestParseOntologyAssetsFromIndexUsesLinkTextFallback(t *testing.T) {
	data := []byte(`
<html>
  <body>
    <a href='go-base.owl'>go-base.owl</a>
    <a>go-basic.json.gz</a>
    <a href='subsets/'>subsets</a>
  </body>
</html>
`)

	assets, err := parseOntologyAssetsFromIndex(data)
	if err != nil {
		t.Fatalf("parseOntologyAssetsFromIndex returned error: %v", err)
	}

	names := make([]string, 0, len(assets))
	for _, asset := range assets {
		names = append(names, asset.name)
	}

	expected := []string{"go-base.owl", "go-basic.json.gz"}
	if !reflect.DeepEqual(names, expected) {
		t.Fatalf("parseOntologyAssetsFromIndex = %#v, want %#v", names, expected)
	}
}

func TestResolveOntologyAssetsRejectsUnknownAsset(t *testing.T) {
	assetsAvailable := []ontologyAsset{
		{name: "go-basic.obo", url: "https://current.geneontology.org/ontology/go-basic.obo"},
	}

	_, err := resolveOntologyAssets(assetsAvailable, []string{"go-basic.obo", "unknown.obo"}, false)
	if err == nil {
		t.Fatal("resolveOntologyAssets returned nil error for unknown asset")
	}
}

func TestBuildOntologyManifestFile(t *testing.T) {
	cfg := ontologyConfig{
		version:      "2026-03-11",
		versionToken: "2026-03-11",
	}
	records := []ontologyRecord{
		{
			Asset:   "go-basic.obo",
			PathRel: "raw/go-basic.obo",
			SHA256:  "sha-basic",
			Bytes:   11,
			URL:     "https://current.geneontology.org/ontology/go-basic.obo",
		},
		{
			Asset:   "go-basic.json",
			PathRel: "raw/go-basic.json",
			SHA256:  "sha-json",
			Bytes:   22,
			URL:     "https://current.geneontology.org/ontology/go-basic.json",
		},
	}

	manifest := buildOntologyManifestFile(
		&cfg,
		records,
		time.Date(2026, time.March, 11, 12, 0, 0, 0, time.FixedZone("UTC+8", 8*3600)),
	)
	if manifest.Database != "go" || manifest.Asset != "ontology" {
		t.Fatalf("manifest = %#v", manifest)
	}
	if len(manifest.Files) != 2 {
		t.Fatalf("len(manifest.Files) = %d, want 2", len(manifest.Files))
	}

	var buffer bytes.Buffer
	if err := toml.NewEncoder(&buffer).Encode(manifest); err != nil {
		t.Fatalf("toml encode returned error: %v", err)
	}
	if buffer.Len() == 0 {
		t.Fatal("encoded manifest is empty")
	}
}

func TestParseOntologyVersionFromOBO(t *testing.T) {
	data := []byte("format-version: 1.2\ndata-version: releases/2026-03-11\nsubsetdef: goslim_generic \"Generic GO slim\"\n")
	value, err := parseOntologyVersionFromOBO(data)
	if err != nil {
		t.Fatalf("parseOntologyVersionFromOBO returned error: %v", err)
	}
	if value != "2026-03-11" {
		t.Fatalf("parseOntologyVersionFromOBO = %q, want %q", value, "2026-03-11")
	}
}

func TestConfirmAllOntologyDownload(t *testing.T) {
	var buffer bytes.Buffer
	err := confirmAllOntologyDownload(strings.NewReader("should_download_all\n"), &buffer)
	if err != nil {
		t.Fatalf("confirmAllOntologyDownload returned error: %v", err)
	}

	assertContains(t, buffer.String(), "Full ontology download may fetch a large number of files")
	assertContains(t, buffer.String(), `Type "should_download_all" to continue.`)
	assertContains(t, buffer.String(), "> ")
}

func TestConfirmAllOntologyDownloadRejectsWrongInput(t *testing.T) {
	err := confirmAllOntologyDownload(strings.NewReader("yes\n"), &bytes.Buffer{})
	if err == nil {
		t.Fatal("confirmAllOntologyDownload returned nil error for wrong confirmation text")
	}

	assertContains(t, err.Error(), `expected "should_download_all"`)
}

func assertContains(t *testing.T, text string, expected string) {
	t.Helper()
	if !strings.Contains(text, expected) {
		t.Fatalf("expected %q in text:\n%s", expected, text)
	}
}
