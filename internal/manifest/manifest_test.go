package manifest

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/pelletier/go-toml/v2"
)

const validSHA256 = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

func TestBuildWritesConsistentDeterministicFormats(t *testing.T) {
	root := t.TempDir()
	writeLock(t, root, "go/ontology/2026-01-23", `
database = "go"
asset = "ontology"
version = "2026-01-23"
version_token = "2026-01-23"
downloaded_at = "2026-01-24T00:00:00Z"

[[files]]
path = "raw/go-basic.obo"
sha256 = "`+validSHA256+`"
bytes = 7
`)
	writeLock(t, root, "string/network/v12.0", `
database = "string"
asset = "network"
version = "12.0"
version_token = "v12.0"

[[species]]
id = "9606"

[[files]]
path = "raw/9606/protein.links.txt.gz"
sha256 = "`+validSHA256+`"
bytes = 11
`)

	dirOutput := filepath.Join(root, "meta")
	if err := os.MkdirAll(dirOutput, 0o755); err != nil {
		t.Fatal(err)
	}
	result, err := Build(root, dirOutput, []string{"json,tsv", "toml", "json"})
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}
	if result.DatabaseCount != 2 || result.SnapshotCount != 2 || result.FileCount != 2 || result.TotalBytes != 18 {
		t.Fatalf("result = %#v", result)
	}

	dataTOML := readFile(t, filepath.Join(dirOutput, "manifest.toml"))
	var modelTOML aggregateManifest
	if err := toml.Unmarshal(dataTOML, &modelTOML); err != nil {
		t.Fatalf("decode TOML: %v", err)
	}
	var modelJSON aggregateManifest
	if err := json.Unmarshal(readFile(t, filepath.Join(dirOutput, "manifest.json")), &modelJSON); err != nil {
		t.Fatalf("decode JSON: %v", err)
	}
	if !reflect.DeepEqual(modelTOML, modelJSON) {
		t.Fatalf("TOML and JSON models differ:\nTOML=%#v\nJSON=%#v", modelTOML, modelJSON)
	}
	if modelTOML.ResourceRoot != ".." {
		t.Fatalf("resource_root = %q, want ..", modelTOML.ResourceRoot)
	}
	if modelTOML.SchemaVersion != "biofetch-manifest-v2" {
		t.Fatalf("schema version = %q", modelTOML.SchemaVersion)
	}
	if strings.Contains(string(dataTOML), "dataset") || strings.Contains(string(readFile(t, filepath.Join(dirOutput, "manifest.json"))), "dataset") {
		t.Fatal("dataset unexpectedly emitted for dataset-free snapshots")
	}
	if modelTOML.Snapshots[0].SourceVersion != "" {
		t.Fatalf("equal source_version was emitted: %#v", modelTOML.Snapshots[0])
	}
	if modelTOML.Snapshots[1].SourceVersion != "12.0" {
		t.Fatalf("different source_version missing: %#v", modelTOML.Snapshots[1])
	}
	if modelTOML.Snapshots[1].RecordKind != "species" || modelTOML.Snapshots[1].RecordCount != 1 {
		t.Fatalf("STRING record metadata = %#v", modelTOML.Snapshots[1])
	}

	reader := csv.NewReader(strings.NewReader(string(readFile(t, filepath.Join(dirOutput, "manifest.tsv")))))
	reader.Comma = '\t'
	records, err := reader.ReadAll()
	if err != nil {
		t.Fatalf("read TSV: %v", err)
	}
	if len(records) != 3 || records[0][0] != "SchemaVersion" || records[1][0] != schemaVersion {
		t.Fatalf("TSV records = %#v", records)
	}
	if records[0][4] != "Dataset" || records[0][5] != "Version" || records[1][4] != "" {
		t.Fatalf("TSV dataset columns = %#v", records[0:2])
	}

	first := map[string][]byte{}
	for _, extension := range []string{"toml", "tsv", "json"} {
		first[extension] = readFile(t, filepath.Join(dirOutput, "manifest."+extension))
	}
	if _, err := Build(root, dirOutput, []string{"toml", "tsv", "json"}); err != nil {
		t.Fatalf("second Build returned error: %v", err)
	}
	for extension, expected := range first {
		if actual := readFile(t, filepath.Join(dirOutput, "manifest."+extension)); !reflect.DeepEqual(actual, expected) {
			t.Fatalf("%s output changed across identical builds", extension)
		}
	}
}

func TestBuildDatasetQualifiedIdentityAndFormats(t *testing.T) {
	root := t.TempDir()
	for _, dataset := range []string{"collectri", "dorothea", "kinaseextra"} {
		writeLock(t, root, "omnipath/interactions/"+dataset+"/2025-08-13", datasetLockContent(dataset))
	}
	dirOutput := filepath.Join(root, "meta")
	if err := os.MkdirAll(dirOutput, 0o755); err != nil {
		t.Fatal(err)
	}
	result, err := BuildWithWorkers(root, dirOutput, []string{"toml", "json", "tsv"}, 4)
	if err != nil {
		t.Fatalf("BuildWithWorkers returned error: %v", err)
	}
	if result.DatabaseCount != 1 || result.SnapshotCount != 3 || result.FileCount != 3 || result.TotalBytes != 3 {
		t.Fatalf("result = %#v", result)
	}

	var modelTOML aggregateManifest
	if err := toml.Unmarshal(readFile(t, filepath.Join(dirOutput, "manifest.toml")), &modelTOML); err != nil {
		t.Fatalf("decode TOML: %v", err)
	}
	var modelJSON aggregateManifest
	if err := json.Unmarshal(readFile(t, filepath.Join(dirOutput, "manifest.json")), &modelJSON); err != nil {
		t.Fatalf("decode JSON: %v", err)
	}
	if !reflect.DeepEqual(modelTOML, modelJSON) {
		t.Fatalf("TOML and JSON models differ:\nTOML=%#v\nJSON=%#v", modelTOML, modelJSON)
	}
	wantDatasets := []string{"collectri", "dorothea", "kinaseextra"}
	for index, want := range wantDatasets {
		if modelTOML.Snapshots[index].Dataset != want {
			t.Fatalf("snapshot[%d].dataset = %q, want %q", index, modelTOML.Snapshots[index].Dataset, want)
		}
	}

	reader := csv.NewReader(strings.NewReader(string(readFile(t, filepath.Join(dirOutput, "manifest.tsv")))))
	reader.Comma = '\t'
	records, err := reader.ReadAll()
	if err != nil {
		t.Fatalf("read TSV: %v", err)
	}
	if records[0][4] != "Dataset" || records[0][5] != "Version" {
		t.Fatalf("TSV header = %#v", records[0])
	}
	for index, want := range wantDatasets {
		if records[index+1][4] != want {
			t.Fatalf("TSV dataset[%d] = %q, want %q", index, records[index+1][4], want)
		}
	}
}

func TestBuildWorkerCountsPreserveOutputBytes(t *testing.T) {
	root := t.TempDir()
	for _, dataset := range []string{"collectri", "dorothea", "kinaseextra"} {
		writeLock(t, root, "omnipath/interactions/"+dataset+"/2025-08-13", datasetLockContent(dataset))
	}

	var want map[string][]byte
	for _, workers := range []int{1, 4, 8} {
		dirOutput := filepath.Join(root, fmt.Sprintf("meta-%d", workers))
		if err := os.MkdirAll(dirOutput, 0o755); err != nil {
			t.Fatal(err)
		}
		if _, err := BuildWithWorkers(root, dirOutput, []string{"toml", "json", "tsv"}, workers); err != nil {
			t.Fatalf("workers=%d: BuildWithWorkers returned error: %v", workers, err)
		}
		actual := make(map[string][]byte, 3)
		for _, extension := range []string{"toml", "json", "tsv"} {
			actual[extension] = readFile(t, filepath.Join(dirOutput, "manifest."+extension))
		}
		if want == nil {
			want = actual
			continue
		}
		for extension, expected := range want {
			if !reflect.DeepEqual(actual[extension], expected) {
				t.Fatalf("workers=%d changed %s output", workers, extension)
			}
		}
	}
}

func TestBuildRejectsMalformedDataset(t *testing.T) {
	tests := []string{"", " collectri", "collectri "}
	for _, dataset := range tests {
		t.Run(fmt.Sprintf("%q", dataset), func(t *testing.T) {
			root := t.TempDir()
			writeLock(t, root, "omnipath/interactions/collectri/2025-08-13", datasetLockContent(dataset))
			if _, err := Build(root, root, []string{"toml"}); err == nil || !strings.Contains(err.Error(), "dataset") {
				t.Fatalf("Build error = %v", err)
			}
		})
	}
}

func TestBuildPrunesNonAuthoritySubtreesAndStopsAtSnapshot(t *testing.T) {
	root := t.TempDir()
	writeLock(t, root, "go/ontology/v1", `
database = "go"
asset = "ontology"
version_token = "v1"
[[files]]
path = "raw/go.obo"
sha256 = "`+validSHA256+`"
bytes = 1
`)
	writeLock(t, root, "go/ontology/v1/raw/ignored/v2", `
database = "bad"
asset = "bad"
version_token = "wrong"
[[files]]
path = "not-raw"
sha256 = "bad"
bytes = 1
`)
	writeLock(t, root, "go/ontology/v2/raw/ignored/v2", `
database = "bad"
asset = "bad"
version_token = "wrong"
[[files]]
path = "not-raw"
sha256 = "bad"
bytes = 1
`)
	writeLock(t, root, "go/ontology/v1/tidy/ignored/v2", `
database = "bad"
asset = "bad"
version_token = "wrong"
[[files]]
path = "not-raw"
sha256 = "bad"
bytes = 1
`)

	result, err := BuildWithWorkers(root, root, []string{"toml"}, 1)
	if err != nil {
		t.Fatalf("BuildWithWorkers returned error: %v", err)
	}
	if result.SnapshotCount != 1 || result.FileCount != 1 {
		t.Fatalf("result = %#v", result)
	}
}

func TestBuildUsesFixedOutputNames(t *testing.T) {
	root := t.TempDir()
	result, err := Build(root, root, nil)
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}
	fileTOML := filepath.Join(root, "manifest.toml")
	if !reflect.DeepEqual(result.Files, []string{fileTOML}) {
		t.Fatalf("result files = %#v, want only %s", result.Files, fileTOML)
	}
	if _, err := os.Stat(fileTOML); err != nil {
		t.Fatalf("fixed-name output missing: %v", err)
	}
	for _, fileName := range []string{"manifest.json", "manifest.tsv", "manifest.lock"} {
		if _, err := os.Stat(filepath.Join(root, fileName)); !os.IsNotExist(err) {
			t.Fatalf("unexpected default output %s: %v", fileName, err)
		}
	}
}

func TestBuildRejectsAssetDirectoryNestedBelowVersion(t *testing.T) {
	root := t.TempDir()
	writeLock(t, root, "string/v12.0/catalog", `
database = "string"
asset = "catalog"
version_token = "v12.0"
[[files]]
path = "raw/species.tsv"
sha256 = "`+validSHA256+`"
bytes = 1
`)
	if _, err := Build(root, root, []string{"toml"}); err == nil || !strings.Contains(err.Error(), "does not match snapshot directory") {
		t.Fatalf("Build error = %v", err)
	}
}

func TestBuildAggregatesLockValidationErrorsWithoutOutputs(t *testing.T) {
	root := t.TempDir()
	writeLock(t, root, "go/ontology/wrong-dir", `
database = "go"
asset = "ontology"
version_token = "2026-01-23"
[[files]]
path = "raw/go.obo"
sha256 = "`+validSHA256+`"
bytes = 1
`)
	writeLock(t, root, "string/network/v12.0", `
database = "string"
version_token = "v12.0"
[[files]]
path = "../escape"
sha256 = "bad"
bytes = 1
`)
	var wantError string
	for _, workers := range []int{1, 4, 8} {
		_, err := BuildWithWorkers(root, root, []string{"toml", "json"}, workers)
		if err == nil {
			t.Fatalf("workers=%d: Build returned nil error", workers)
		}
		text := err.Error()
		for _, expected := range []string{"go/ontology/wrong-dir/manifest.lock", "string/network/v12.0/manifest.lock"} {
			if !strings.Contains(text, expected) {
				t.Fatalf("workers=%d: error does not list %q: %v", workers, expected, err)
			}
		}
		if wantError == "" {
			wantError = text
		} else if text != wantError {
			t.Fatalf("workers=%d changed validation error order or text:\nwant=%s\n got=%s", workers, wantError, text)
		}
	}
	for _, extension := range []string{"toml", "json"} {
		if _, statErr := os.Stat(filepath.Join(root, "manifest."+extension)); !os.IsNotExist(statErr) {
			t.Fatalf("failed build left formal %s output: %v", extension, statErr)
		}
	}
}

func TestBuildRejectsDuplicateSnapshotIdentity(t *testing.T) {
	root := t.TempDir()
	content := `
database = "go"
asset = "ontology"
version_token = "v1"
[[files]]
path = "raw/a"
sha256 = "` + validSHA256 + `"
bytes = 1
`
	writeLock(t, root, "first/v1", content)
	writeLock(t, root, "second/v1", content)
	_, err := Build(root, root, []string{"toml"})
	if err == nil || !strings.Contains(err.Error(), "duplicate snapshot identity") {
		t.Fatalf("Build error = %v", err)
	}
}

func TestValidateLockFileRejectsUnsafeAndIncompleteRecords(t *testing.T) {
	bytesOne := int64(1)
	tests := []lockFileEnvelope{
		{Path: "/absolute", SHA256: validSHA256, Bytes: &bytesOne},
		{Path: "raw/../escape", SHA256: validSHA256, Bytes: &bytesOne},
		{Path: "tidy/data.tsv", SHA256: validSHA256, Bytes: &bytesOne},
		{Path: "raw/data.tsv", SHA256: "bad", Bytes: &bytesOne},
		{Path: "raw/data.tsv", SHA256: validSHA256, Bytes: nil},
	}
	for _, test := range tests {
		if err := validateLockFile(test); err == nil {
			t.Fatalf("validateLockFile(%#v) returned nil", test)
		}
	}
}

func writeLock(t *testing.T, root string, dirRelative string, content string) string {
	t.Helper()
	dir := filepath.Join(root, filepath.FromSlash(dirRelative))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	filePath := filepath.Join(dir, "manifest.lock")
	if err := os.WriteFile(filePath, []byte(strings.TrimSpace(content)+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return filePath
}

func datasetLockContent(dataset string) string {
	return fmt.Sprintf(`
database = "omnipath"
asset = "interactions"
dataset = "%s"
version_token = "2025-08-13"
[[files]]
path = "raw/interactions.tsv"
sha256 = "%s"
bytes = 1
`, dataset, validSHA256)
}

func readFile(t *testing.T, filePath string) []byte {
	t.Helper()
	data, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatal(err)
	}
	return data
}
