package subcell

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"biofetch/internal/shared/staticasset"
)

func TestResolveUniProtScopeTaxID(t *testing.T) {
	scope, err := resolveUniProtScope("", "9606", "")
	if err != nil {
		t.Fatalf("resolveUniProtScope returned error: %v", err)
	}
	if scope.scopeType != "taxid" || scope.scopeValue != "9606" {
		t.Fatalf("scope = %#v", scope)
	}
	if !strings.Contains(scope.query, "organism_id:9606") || !strings.Contains(scope.query, "cc_subcellular_location:*") {
		t.Fatalf("query = %q", scope.query)
	}
}

func TestResolveUniProtScopeRejectsAmbiguous(t *testing.T) {
	_, err := resolveUniProtScope("hsa", "9606", "")
	if err == nil {
		t.Fatal("resolveUniProtScope returned nil error")
	}
}

func TestBuildUniProtStreamURL(t *testing.T) {
	urlBuilt := buildUniProtStreamURL("(organism_id:9606) AND (cc_subcellular_location:*)")
	if !strings.HasPrefix(urlBuilt, "https://rest.uniprot.org/uniprotkb/stream?") {
		t.Fatalf("url = %q", urlBuilt)
	}
	for _, part := range []string{"format=tsv", "fields=accession%2Ccc_subcellular_location", "compressed=false"} {
		if !strings.Contains(urlBuilt, part) {
			t.Fatalf("url %q does not contain %s", urlBuilt, part)
		}
	}
}

func TestExtractUniProtLocations(t *testing.T) {
	got := extractUniProtLocations("SUBCELLULAR LOCATION: Nucleus {ECO:0000269}; Cytoplasm. Note=Shuttles.")
	want := []string{"Nucleus.", "Cytoplasm."}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("locations = %#v, want %#v", got, want)
	}
}

func TestParseUniProtProteinLocations(t *testing.T) {
	data := []byte("Entry\tSubcellular location [CC]\nP12345\tSUBCELLULAR LOCATION: Nucleus {ECO:1}; Cytoplasm.\nQ99999\tSUBCELLULAR LOCATION: Mitochondrion.\n")
	records, err := parseUniProtProteinLocations(data, "uniprot", "2026_03")
	if err != nil {
		t.Fatalf("parseUniProtProteinLocations returned error: %v", err)
	}
	if len(records) != 3 {
		t.Fatalf("len(records) = %d, want 3: %#v", len(records), records)
	}
	if records[0].proteinID != "P12345" || records[0].location != "Nucleus." || records[0].sourceVersion != "2026_03" {
		t.Fatalf("records[0] = %#v", records[0])
	}
}

func TestRunFetchUniProtDownloadsNormalizedAsset(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/uniprotkb/stream" {
			t.Fatalf("unexpected path: %s", request.URL.Path)
		}
		_, _ = writer.Write([]byte("Entry\tSubcellular location [CC]\nP12345\tSUBCELLULAR LOCATION: Nucleus {ECO:1}; Cytoplasm.\n"))
	}))
	defer server.Close()

	originalURL := uniprotStreamBaseURL
	t.Cleanup(func() { uniprotStreamBaseURL = originalURL })
	uniprotStreamBaseURL = server.URL + "/uniprotkb/stream"

	cfg := createDefaultUniProtConfig()
	cfg.DirOut = t.TempDir()
	cfg.RetryMax = 1
	cfg.WorkersMax = 1
	cfg.taxID = "9606"
	cfg.VersionToken = "2026_03"
	if err := runFetchUniProt(&cfg); err != nil {
		t.Fatalf("runFetchUniProt returned error: %v", err)
	}

	fileOut := filepath.Join(cfg.DirOut, "uniprot", "2026_03", "tidy", "protein_location.tsv")
	data, err := os.ReadFile(fileOut)
	if err != nil {
		t.Fatalf("os.ReadFile returned error: %v", err)
	}
	text := string(data)
	if !strings.Contains(text, "protein_id\tlocation\tsource\tsource_version") || !strings.Contains(text, "P12345\tNucleus.\tuniprot\t2026_03") {
		t.Fatalf("normalized output = %q", text)
	}

	manifest, ok, err := staticasset.ReadManifest(filepath.Join(cfg.DirOut, "uniprot", "2026_03", "manifest.lock"))
	if err != nil {
		t.Fatalf("ReadManifest returned error: %v", err)
	}
	if !ok {
		t.Fatal("manifest missing")
	}
	if manifest.Database != "subcell" || manifest.Asset != "protein_location" || manifest.Source != "uniprot" {
		t.Fatalf("manifest identity = %#v", manifest)
	}
}

func TestRunSyncUniProtRehydratesManifest(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		_, _ = writer.Write([]byte("Entry\tSubcellular location [CC]\nP12345\tSUBCELLULAR LOCATION: Nucleus.\n"))
	}))
	defer server.Close()

	originalURL := uniprotStreamBaseURL
	t.Cleanup(func() { uniprotStreamBaseURL = originalURL })
	uniprotStreamBaseURL = server.URL + "/uniprotkb/stream"

	cfg := createDefaultUniProtConfig()
	cfg.DirOut = t.TempDir()
	cfg.RetryMax = 1
	cfg.WorkersMax = 1
	cfg.taxID = "9606"
	cfg.VersionToken = "2026_03"
	if err := runFetchUniProt(&cfg); err != nil {
		t.Fatalf("runFetchUniProt returned error: %v", err)
	}
	fileOut := filepath.Join(cfg.DirOut, "uniprot", "2026_03", "tidy", "protein_location.tsv")
	if err := os.Remove(fileOut); err != nil {
		t.Fatalf("os.Remove returned error: %v", err)
	}
	syncCfg := uniprotSyncConfig{}
	syncCfg.DirOut = cfg.DirOut
	syncCfg.VersionToken = "2026_03"
	syncCfg.RuleExisting = "skip"
	syncCfg.RetryMax = 1
	syncCfg.WorkersMax = 1
	if err := runSyncUniProt(&syncCfg); err != nil {
		t.Fatalf("runSyncUniProt returned error: %v", err)
	}
	if _, err := os.Stat(fileOut); err != nil {
		t.Fatalf("synced file missing: %v", err)
	}
}

func TestRunLockUniProtScansTidyAsset(t *testing.T) {
	dirOut := t.TempDir()
	dirTidy := filepath.Join(dirOut, "uniprot", "2026_03", "tidy")
	if err := os.MkdirAll(dirTidy, 0o755); err != nil {
		t.Fatalf("os.MkdirAll returned error: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dirTidy, "protein_location.tsv"), []byte("protein_id\tlocation\tsource\tsource_version\nP12345\tNucleus.\tuniprot\t2026_03\n"), 0o644); err != nil {
		t.Fatalf("os.WriteFile returned error: %v", err)
	}
	cfg := uniprotLockConfig{}
	cfg.DirOut = dirOut
	cfg.VersionToken = "2026_03"
	if err := runLockUniProt(&cfg); err != nil {
		t.Fatalf("runLockUniProt returned error: %v", err)
	}
	manifest, ok, err := staticasset.ReadManifest(filepath.Join(dirOut, "uniprot", "2026_03", "manifest.lock"))
	if err != nil {
		t.Fatalf("ReadManifest returned error: %v", err)
	}
	if !ok {
		t.Fatal("manifest missing")
	}
	if len(manifest.Files) != 1 || manifest.Files[0].Path != "tidy/protein_location.tsv" {
		t.Fatalf("manifest files = %#v", manifest.Files)
	}
}
