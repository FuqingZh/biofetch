package geneontology

import (
	"bytes"
	"reflect"
	"testing"
	"time"

	toml "github.com/pelletier/go-toml/v2"
)

func TestParseOntologyAssetNames(t *testing.T) {
	assets, err := parseOntologyAssetNames("go-plus.json,go-basic.obo,go-plus.json")
	if err != nil {
		t.Fatalf("parseOntologyAssetNames returned error: %v", err)
	}

	names := make([]string, 0, len(assets))
	for _, asset := range assets {
		names = append(names, asset.name)
	}

	expected := []string{"go-basic.obo", "go-plus.json"}
	if !reflect.DeepEqual(names, expected) {
		t.Fatalf("parseOntologyAssetNames = %#v, want %#v", names, expected)
	}
}

func TestParseOntologyAssetNamesRejectsUnknownAsset(t *testing.T) {
	_, err := parseOntologyAssetNames("go-basic.obo,unknown.obo")
	if err == nil {
		t.Fatal("parseOntologyAssetNames returned nil error for unknown asset")
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
			Asset:   "go-plus.json",
			PathRel: "raw/go-plus.json",
			SHA256:  "sha-plus",
			Bytes:   22,
			URL:     "https://current.geneontology.org/ontology/go-plus.json",
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
