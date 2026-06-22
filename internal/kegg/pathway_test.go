package kegg

import (
	"biofetch/internal/shared/httpx"
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	toml "github.com/pelletier/go-toml/v2"
)

func TestParsePathwayIDsFromList(t *testing.T) {
	data := []byte("map00010\tGlycolysis / Gluconeogenesis\nmap00020\tCitrate cycle (TCA cycle)\n")
	values, err := parsePathwayIDsFromList(data)
	if err != nil {
		t.Fatalf("parsePathwayIDsFromList returned error: %v", err)
	}

	expected := []string{"map00010", "map00020"}
	if !reflect.DeepEqual(values, expected) {
		t.Fatalf("parsePathwayIDsFromList = %#v, want %#v", values, expected)
	}
}

func TestParsePathwayAssetNames(t *testing.T) {
	values, err := parsePathwayAssetNames([]string{"entry,kgml", "image", "entry"})
	if err != nil {
		t.Fatalf("parsePathwayAssetNames returned error: %v", err)
	}

	expected := []string{"entry", "image", "kgml"}
	if !reflect.DeepEqual(values, expected) {
		t.Fatalf("parsePathwayAssetNames = %#v, want %#v", values, expected)
	}
}

func TestResolvePathwayAssetNamesReturnsAllWhenAssetsOmitted(t *testing.T) {
	values, err := resolvePathwayAssetNames(nil)
	if err != nil {
		t.Fatalf("resolvePathwayAssetNames returned error: %v", err)
	}

	expected := []string{"list", "entry", "kgml", "conf", "image"}
	if !reflect.DeepEqual(values, expected) {
		t.Fatalf("resolvePathwayAssetNames = %#v, want %#v", values, expected)
	}
}

func TestResolveKEGGOrganismInputsSupportsAtFileAndInputOrder(t *testing.T) {
	fileOrganisms := filepath.Join(t.TempDir(), "organisms.txt")
	if err := os.WriteFile(fileOrganisms, []byte("# comment\nmmu\nhsa\n"), 0o644); err != nil {
		t.Fatalf("os.WriteFile returned error: %v", err)
	}

	values, err := resolveKEGGOrganismInputs([]string{"tca,hsa", "@" + fileOrganisms}, ruleOrderInput)
	if err != nil {
		t.Fatalf("resolveKEGGOrganismInputs returned error: %v", err)
	}

	expected := []string{"tca", "hsa", "mmu"}
	if !reflect.DeepEqual(values, expected) {
		t.Fatalf("resolveKEGGOrganismInputs = %#v, want %#v", values, expected)
	}
}

func TestResolvePathwayIDInputsSupportsDescOrder(t *testing.T) {
	values, err := resolvePathwayIDInputs([]string{"map00020,map00010", "map00030"}, ruleOrderDesc)
	if err != nil {
		t.Fatalf("resolvePathwayIDInputs returned error: %v", err)
	}

	expected := []string{"map00030", "map00020", "map00010"}
	if !reflect.DeepEqual(values, expected) {
		t.Fatalf("resolvePathwayIDInputs = %#v, want %#v", values, expected)
	}
}

func TestResolvePathwayIDInputsRejectsInvalidAtFile(t *testing.T) {
	filePathwayIDs := filepath.Join(t.TempDir(), "pathway_ids.txt")
	if err := os.WriteFile(filePathwayIDs, []byte("map00010\nbad\n"), 0o644); err != nil {
		t.Fatalf("os.WriteFile returned error: %v", err)
	}

	_, err := resolvePathwayIDInputs([]string{"@" + filePathwayIDs}, ruleOrderAsc)
	if err == nil || !strings.Contains(err.Error(), "invalid pathway id in") {
		t.Fatalf("resolvePathwayIDInputs expected invalid file error, got: %v", err)
	}
}

func TestDerivePathwayAssetSpecs(t *testing.T) {
	specs := derivePathwayAssetSpecs("hsa", "hsa00010", []string{"entry", "conf", "image"}, "/tmp/raw/hsa")
	if len(specs) != 3 {
		t.Fatalf("derivePathwayAssetSpecs len = %d, want 3", len(specs))
	}
	if specs[0].assetName != "pathway.entry" || specs[0].url != "https://rest.kegg.jp/get/hsa00010" {
		t.Fatalf("entry spec = %#v", specs[0])
	}
	if specs[1].assetName != "pathway.conf" || specs[1].pathRel != "raw/hsa/hsa00010.conf" {
		t.Fatalf("conf spec = %#v", specs[1])
	}
	if specs[2].assetName != "pathway.image" || specs[2].fileOut != "/tmp/raw/hsa/hsa00010.png" {
		t.Fatalf("image spec = %#v", specs[2])
	}
}

func TestFetchPathwayKGMLSkipsStatus404(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		http.Error(writer, "missing", http.StatusNotFound)
	}))
	defer server.Close()

	fileOut := filepath.Join(t.TempDir(), "vmo00999.kgml")
	clientKegg := createKEGGClient(server.Client(), 0, 1, 0)
	record, ok, err := fetchPathwayAsset(
		clientKegg,
		false,
		fileOut,
		"raw/vmo/vmo00999.kgml",
		"vmo00999",
		"pathway.kgml",
		server.URL+"/get/vmo00999/kgml",
	)
	if err != nil {
		t.Fatalf("fetchPathwayAsset returned error: %v", err)
	}
	if ok {
		t.Fatalf("ok = true, record = %#v", record)
	}
	if _, err := os.Stat(fileOut); !os.IsNotExist(err) {
		t.Fatalf("file exists or stat failed unexpectedly: %v", err)
	}
}

func TestFetchPathwayEntryDoesNotSkipStatus404(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		http.Error(writer, "missing", http.StatusNotFound)
	}))
	defer server.Close()

	fileOut := filepath.Join(t.TempDir(), "vmo00999.txt")
	clientKegg := createKEGGClient(server.Client(), 0, 1, 0)
	_, ok, err := fetchPathwayAsset(
		clientKegg,
		false,
		fileOut,
		"raw/vmo/vmo00999.txt",
		"vmo00999",
		"pathway.entry",
		server.URL+"/get/vmo00999",
	)
	if err == nil {
		t.Fatal("fetchPathwayAsset returned nil error")
	}
	if ok {
		t.Fatal("ok = true")
	}
	if !httpx.IsUnexpectedStatus(err, http.StatusNotFound) {
		t.Fatalf("error = %v, want 404", err)
	}
}

func TestFetchPathwayKGMLSkipsStatus403(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		http.Error(writer, "forbidden", http.StatusForbidden)
	}))
	defer server.Close()

	fileOut := filepath.Join(t.TempDir(), "vibr00541.kgml")
	clientKegg := createKEGGClient(server.Client(), 0, 1, 0)
	record, ok, err := fetchPathwayAsset(
		clientKegg,
		false,
		fileOut,
		"raw/vibr/vibr00541.kgml",
		"vibr00541",
		"pathway.kgml",
		server.URL+"/get/vibr00541/kgml",
	)
	if err != nil {
		t.Fatalf("fetchPathwayAsset returned error: %v", err)
	}
	if ok {
		t.Fatalf("ok = true, record = %#v", record)
	}
	if _, err := os.Stat(fileOut); !os.IsNotExist(err) {
		t.Fatalf("file exists or stat failed unexpectedly: %v", err)
	}
}

func TestFetchPathwayConfSkipsStatus403(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		http.Error(writer, "forbidden", http.StatusForbidden)
	}))
	defer server.Close()

	fileOut := filepath.Join(t.TempDir(), "vibr00541.conf")
	clientKegg := createKEGGClient(server.Client(), 0, 1, 0)
	record, ok, err := fetchPathwayAsset(
		clientKegg,
		false,
		fileOut,
		"raw/vibr/vibr00541.conf",
		"vibr00541",
		"pathway.conf",
		server.URL+"/get/vibr00541/conf",
	)
	if err != nil {
		t.Fatalf("fetchPathwayAsset returned error: %v", err)
	}
	if ok {
		t.Fatalf("ok = true, record = %#v", record)
	}
	if _, err := os.Stat(fileOut); !os.IsNotExist(err) {
		t.Fatalf("file exists or stat failed unexpectedly: %v", err)
	}
}

func TestFetchPathwayImageSkipsStatus403(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		http.Error(writer, "forbidden", http.StatusForbidden)
	}))
	defer server.Close()

	fileOut := filepath.Join(t.TempDir(), "vibr00541.png")
	clientKegg := createKEGGClient(server.Client(), 0, 1, 0)
	record, ok, err := fetchPathwayAsset(
		clientKegg,
		false,
		fileOut,
		"raw/vibr/vibr00541.png",
		"vibr00541",
		"pathway.image",
		server.URL+"/get/vibr00541/image",
	)
	if err != nil {
		t.Fatalf("fetchPathwayAsset returned error: %v", err)
	}
	if ok {
		t.Fatalf("ok = true, record = %#v", record)
	}
	if _, err := os.Stat(fileOut); !os.IsNotExist(err) {
		t.Fatalf("file exists or stat failed unexpectedly: %v", err)
	}
}

func TestFetchPathwayEntryContinuesAfterSingleStatus403(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		http.Error(writer, "forbidden", http.StatusForbidden)
	}))
	defer server.Close()

	fileOut := filepath.Join(t.TempDir(), "vibr00541.txt")
	clientKegg := createKEGGClient(server.Client(), 0, 1, 0)
	_, ok, err := fetchPathwayAsset(
		clientKegg,
		false,
		fileOut,
		"raw/vibr/vibr00541.txt",
		"vibr00541",
		"pathway.entry",
		server.URL+"/get/vibr00541",
	)
	if err != nil {
		t.Fatalf("fetchPathwayAsset returned error: %v", err)
	}
	if ok {
		t.Fatal("ok = true")
	}
}

func TestFetchPathwayEntryRetriesStatus403ThenSucceeds(t *testing.T) {
	var countGET atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodGet {
			http.Error(writer, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if countGET.Add(1) < 3 {
			http.Error(writer, "forbidden", http.StatusForbidden)
			return
		}
		_, _ = writer.Write([]byte("ENTRY       hsa03460\n"))
	}))
	defer server.Close()

	fileOut := filepath.Join(t.TempDir(), "hsa03460.txt")
	clientKegg := createKEGGClient(server.Client(), 0, 3, 0)
	record, ok, err := fetchPathwayAsset(
		clientKegg,
		false,
		fileOut,
		"raw/hsa/hsa03460.txt",
		"hsa03460",
		"pathway.entry",
		server.URL+"/get/hsa03460",
	)
	if err != nil {
		t.Fatalf("fetchPathwayAsset returned error: %v", err)
	}
	if !ok {
		t.Fatalf("ok = false, record = %#v", record)
	}
	if countGET.Load() != 3 {
		t.Fatalf("countGET = %d, want 3", countGET.Load())
	}
}

func TestFetchPathwayEntryExhaustsStatus403RetriesAndContinues(t *testing.T) {
	var countGET atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodGet {
			http.Error(writer, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		countGET.Add(1)
		http.Error(writer, "forbidden", http.StatusForbidden)
	}))
	defer server.Close()

	fileOut := filepath.Join(t.TempDir(), "hsa03460.txt")
	clientKegg := createKEGGClient(server.Client(), 0, 2, 0)
	_, ok, err := fetchPathwayAsset(
		clientKegg,
		false,
		fileOut,
		"raw/hsa/hsa03460.txt",
		"hsa03460",
		"pathway.entry",
		server.URL+"/get/hsa03460",
	)
	if err != nil {
		t.Fatalf("fetchPathwayAsset returned error: %v", err)
	}
	if ok {
		t.Fatal("ok = true")
	}
	if countGET.Load() != 2 {
		t.Fatalf("countGET = %d, want 2", countGET.Load())
	}
}

func TestChunkStrings(t *testing.T) {
	batches := chunkStrings([]string{"a", "b", "c", "d", "e"}, 2)
	expected := [][]string{{"a", "b"}, {"c", "d"}, {"e"}}
	if !reflect.DeepEqual(batches, expected) {
		t.Fatalf("chunkStrings = %#v, want %#v", batches, expected)
	}
}

func TestInspectPathwayAssetTaskReusesManifestWhenSizeMatches(t *testing.T) {
	dirTemp := t.TempDir()
	fileOut := filepath.Join(dirTemp, "hsa00010.txt")
	if err := os.WriteFile(fileOut, []byte("new-content"), 0o644); err != nil {
		t.Fatalf("os.WriteFile returned error: %v", err)
	}

	pathRel := "raw/hsa/hsa00010.txt"
	recordManifest := pathwayRecord{
		PathwayID: "hsa00010",
		Asset:     "pathway.entry",
		PathRel:   pathRel,
		SHA256:    "manifest-sha",
		Bytes:     int64(len("new-content")),
		URL:       "https://rest.kegg.jp/get/hsa00010",
	}

	result, err := inspectPathwayAssetTask(
		pathwayAssetInspectTask{
			pathwayID: "hsa00010",
			spec: pathwayAssetSpec{
				assetName: "pathway.entry",
				fileOut:   fileOut,
				pathRel:   pathRel,
				url:       "https://rest.kegg.jp/get/hsa00010",
			},
		},
		false,
		map[string]pathwayRecord{pathRel: recordManifest},
		map[string]pathwayFileInfo{"hsa00010.txt": {size: int64(len("new-content"))}},
	)
	if err != nil {
		t.Fatalf("inspectPathwayAssetTask returned error: %v", err)
	}
	if result.shouldDownload || !result.wasManifest || result.wasHash {
		t.Fatalf("result = %#v", result)
	}
	if result.record.SHA256 != "manifest-sha" {
		t.Fatalf("result.record.SHA256 = %q, want manifest-sha", result.record.SHA256)
	}
}

func TestInspectPathwayAssetTaskFallsBackToHashWhenManifestSizeDiffers(t *testing.T) {
	dirTemp := t.TempDir()
	fileOut := filepath.Join(dirTemp, "hsa00010.txt")
	content := []byte("entry")
	if err := os.WriteFile(fileOut, content, 0o644); err != nil {
		t.Fatalf("os.WriteFile returned error: %v", err)
	}

	pathRel := "raw/hsa/hsa00010.txt"
	result, err := inspectPathwayAssetTask(
		pathwayAssetInspectTask{
			pathwayID: "hsa00010",
			spec: pathwayAssetSpec{
				assetName: "pathway.entry",
				fileOut:   fileOut,
				pathRel:   pathRel,
				url:       "https://rest.kegg.jp/get/hsa00010",
			},
		},
		false,
		map[string]pathwayRecord{
			pathRel: {
				PathwayID: "hsa00010",
				Asset:     "pathway.entry",
				PathRel:   pathRel,
				SHA256:    "manifest-sha",
				Bytes:     999,
				URL:       "https://rest.kegg.jp/get/hsa00010",
			},
		},
		map[string]pathwayFileInfo{"hsa00010.txt": {size: int64(len(content))}},
	)
	if err != nil {
		t.Fatalf("inspectPathwayAssetTask returned error: %v", err)
	}
	if result.shouldDownload || result.wasManifest || !result.wasHash {
		t.Fatalf("result = %#v", result)
	}
	if result.record.SHA256 == "manifest-sha" || result.record.SHA256 == "" {
		t.Fatalf("result.record.SHA256 = %q", result.record.SHA256)
	}
}

func TestScanPathwayScopeFileIndexTreatsMissingDirAsEmpty(t *testing.T) {
	index, err := scanPathwayScopeFileIndex(filepath.Join(t.TempDir(), "missing"))
	if err != nil {
		t.Fatalf("scanPathwayScopeFileIndex returned error: %v", err)
	}
	if len(index) != 0 {
		t.Fatalf("len(index) = %d, want 0", len(index))
	}
}

func TestInspectPathwayAssetTasksPreservesOrder(t *testing.T) {
	dirTemp := t.TempDir()
	fileA := filepath.Join(dirTemp, "hsa00010.txt")
	fileB := filepath.Join(dirTemp, "hsa00020.txt")
	if err := os.WriteFile(fileA, []byte("a"), 0o644); err != nil {
		t.Fatalf("os.WriteFile returned error: %v", err)
	}
	if err := os.WriteFile(fileB, []byte("bb"), 0o644); err != nil {
		t.Fatalf("os.WriteFile returned error: %v", err)
	}

	results, err := inspectPathwayAssetTasks(
		[]pathwayAssetInspectTask{
			{
				pathwayID: "hsa00010",
				spec: pathwayAssetSpec{
					assetName: "pathway.entry",
					fileOut:   fileA,
					pathRel:   "raw/hsa/hsa00010.txt",
					url:       "https://rest.kegg.jp/get/hsa00010",
				},
			},
			{
				pathwayID: "hsa00020",
				spec: pathwayAssetSpec{
					assetName: "pathway.entry",
					fileOut:   fileB,
					pathRel:   "raw/hsa/hsa00020.txt",
					url:       "https://rest.kegg.jp/get/hsa00020",
				},
			},
		},
		false,
		nil,
		map[string]pathwayFileInfo{
			"hsa00010.txt": {size: 1},
			"hsa00020.txt": {size: 2},
		},
	)
	if err != nil {
		t.Fatalf("inspectPathwayAssetTasks returned error: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("len(results) = %d, want 2", len(results))
	}
	if results[0].record.PathwayID != "hsa00010" || results[1].record.PathwayID != "hsa00020" {
		t.Fatalf("results order = %#v", results)
	}
}

func TestBuildManifestFile(t *testing.T) {
	cfg := pathwayConfig{
		version:              "2026-03",
		versionToken:         "2026-03",
		sourceRelease:        "117.0+/03-11",
		sourceReleaseStart:   "117.0+/03-11",
		sourceReleaseEnd:     "117.0+/03-12",
		shouldFetchReference: true,
	}
	records := []pathwayRecord{
		{
			PathwayID: "map00010",
			Asset:     "pathway.entry",
			PathRel:   "raw/reference/map00010.txt",
			SHA256:    "sha-entry",
			Bytes:     11,
			URL:       "https://rest.kegg.jp/get/map00010",
		},
		{
			PathwayID: "map00010",
			Asset:     "pathway.kgml",
			PathRel:   "raw/reference/map00010.kgml",
			SHA256:    "sha-kgml",
			Bytes:     22,
			URL:       "https://rest.kegg.jp/get/map00010/kgml",
		},
	}

	manifest := buildManifestFile(
		&cfg,
		records,
		time.Date(2026, time.March, 11, 12, 0, 0, 0, time.FixedZone("UTC+8", 8*3600)),
	)
	if manifest.Database != "kegg" || manifest.Asset != "pathway" {
		t.Fatalf("manifest = %#v", manifest)
	}
	if manifest.VersionToken != "2026-03" || manifest.SourceReleaseStart != "117.0+/03-11" || manifest.SourceReleaseEnd != "117.0+/03-12" {
		t.Fatalf("manifest release fields = %#v", manifest)
	}
	if len(manifest.Pathways) != 1 || manifest.Pathways[0].ID != "map00010" {
		t.Fatalf("manifest.Pathways = %#v", manifest.Pathways)
	}

	var buffer bytes.Buffer
	if err := toml.NewEncoder(&buffer).Encode(manifest); err != nil {
		t.Fatalf("toml encode returned error: %v", err)
	}
	if buffer.Len() == 0 {
		t.Fatal("encoded manifest is empty")
	}
}

func TestDerivePathwayManifestScopeFromRecords(t *testing.T) {
	cfg := pathwayConfig{}
	records := []pathwayRecord{
		{PathRel: "raw/hsa/map00010.txt"},
		{PathRel: "raw/mmu/map00020.txt"},
	}

	scopeType, scopeValue := derivePathwayManifestScope(&cfg, records)
	if scopeType != "organisms" || scopeValue != "hsa,mmu" {
		t.Fatalf("derivePathwayManifestScope = %q, %q", scopeType, scopeValue)
	}
}

func TestParseKEGGMajorVersion(t *testing.T) {
	value, err := parseKEGGMajorVersion("117.0+/03-10")
	if err != nil {
		t.Fatalf("parseKEGGMajorVersion returned error: %v", err)
	}
	if value != "117.0" {
		t.Fatalf("parseKEGGMajorVersion = %q, want %q", value, "117.0")
	}
}

func TestParseKEGGReleaseFromInfoErrorMessageIsExplicit(t *testing.T) {
	_, err := parseKEGGReleaseFromInfo([]byte("KEGG Database\nno release field here\n"))
	if err == nil {
		t.Fatal("parseKEGGReleaseFromInfo returned nil error")
	}
	text := err.Error()
	if !strings.Contains(text, "failed to parse KEGG release from info response") {
		t.Fatalf("error = %q", text)
	}
	if !strings.Contains(text, "did not contain a 'Release ...' field") {
		t.Fatalf("error = %q", text)
	}
}

func TestParseKEGGInfoMetadataSupportsLastUpdate(t *testing.T) {
	metadata, err := parseKEGGInfoMetadata([]byte("pathway\tKEGG pathway maps\npath\t586 entries\n\tLast update 2026/06/12\n"))
	if err != nil {
		t.Fatalf("parseKEGGInfoMetadata returned error: %v", err)
	}
	if metadata.sourceRelease != "" {
		t.Fatalf("sourceRelease = %q, want empty", metadata.sourceRelease)
	}
	if metadata.sourceLastUpdate != "2026-06-12" {
		t.Fatalf("sourceLastUpdate = %q, want 2026-06-12", metadata.sourceLastUpdate)
	}
}

func TestParseKEGGInfoMetadataSupportsRelease(t *testing.T) {
	metadata, err := parseKEGGInfoMetadata([]byte("pathway          KEGG Pathway Database\npathway          Release 117.0+/03-10, Mar 10\n"))
	if err != nil {
		t.Fatalf("parseKEGGInfoMetadata returned error: %v", err)
	}
	if metadata.sourceRelease != "117.0+/03-10" {
		t.Fatalf("sourceRelease = %q, want 117.0+/03-10", metadata.sourceRelease)
	}
	if metadata.sourceLastUpdate != "" {
		t.Fatalf("sourceLastUpdate = %q, want empty", metadata.sourceLastUpdate)
	}
}

func TestRunFetchPathwayExplicitSnapshotVersionAllowsLastUpdateInfo(t *testing.T) {
	oldBaseURL := baseURL
	defer func() {
		baseURL = oldBaseURL
	}()

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/info/pathway":
			_, _ = writer.Write([]byte("pathway\tKEGG pathway maps\npath\t586 entries\n\tLast update 2026/06/12\n"))
		case "/list/pathway/hsa":
			_, _ = writer.Write([]byte("hsa00010\tGlycolysis / Gluconeogenesis\n"))
		case "/get/hsa00010":
			_, _ = writer.Write([]byte("ENTRY       hsa00010 Pathway\nNAME        Glycolysis / Gluconeogenesis\n"))
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()
	baseURL = server.URL

	dirOut := t.TempDir()
	cfg := pathwayConfig{
		dirOut:          dirOut,
		versionToken:    "2026-04",
		assetNames:      []string{"entry"},
		organismCodes:   []string{"hsa"},
		retryMax:        1,
		ruleExisting:    "skip",
		requestInterval: 0,
	}
	if err := runFetchPathway(&cfg); err != nil {
		t.Fatalf("runFetchPathway returned error: %v", err)
	}

	manifest, err := readExistingPathwayManifest(filepath.Join(dirOut, "pathway", "2026-04", "manifest.lock"))
	if err != nil {
		t.Fatalf("readExistingPathwayManifest returned error: %v", err)
	}
	if manifest.VersionToken != "2026-04" {
		t.Fatalf("VersionToken = %q, want 2026-04", manifest.VersionToken)
	}
	if manifest.SourceRelease != "" {
		t.Fatalf("SourceRelease = %q, want empty", manifest.SourceRelease)
	}
	if manifest.SourceLastUpdate != "2026-06-12" || manifest.SourceLastUpdateStart != "2026-06-12" || manifest.SourceLastUpdateEnd != "2026-06-12" {
		t.Fatalf("last update fields = %q, %q, %q", manifest.SourceLastUpdate, manifest.SourceLastUpdateStart, manifest.SourceLastUpdateEnd)
	}
	if len(manifest.Files) != 1 || manifest.Files[0].Path != "raw/hsa/hsa00010.txt" {
		t.Fatalf("manifest.Files = %#v", manifest.Files)
	}
}

func TestDerivePathwayListURLReferenceAndOrganism(t *testing.T) {
	if value := derivePathwayListURL(&pathwayConfig{shouldFetchReference: true}); value != "https://rest.kegg.jp/list/pathway" {
		t.Fatalf("derivePathwayListURL reference = %q", value)
	}
	if value := derivePathwayListURL(&pathwayConfig{organismCode: "hsa"}); value != "https://rest.kegg.jp/list/pathway/hsa" {
		t.Fatalf("derivePathwayListURL organism = %q", value)
	}
}

func TestValidatePathwayConfigAcceptsSnapshotVersion(t *testing.T) {
	cfg := pathwayConfig{
		dirOut:               "/tmp/kegg",
		versionToken:         "2026-04",
		shouldFetchReference: true,
		retryMax:             1,
		ruleExisting:         "skip",
	}
	if err := validatePathwayConfig(&cfg); err != nil {
		t.Fatalf("validatePathwayConfig returned error: %v", err)
	}
	expected := []string{"list", "entry", "kgml", "conf", "image"}
	if !reflect.DeepEqual(cfg.assetNames, expected) {
		t.Fatalf("cfg.assetNames = %#v, want %#v", cfg.assetNames, expected)
	}
	if cfg.ruleOrder != ruleOrderAsc {
		t.Fatalf("cfg.ruleOrder = %q, want %q", cfg.ruleOrder, ruleOrderAsc)
	}
}

func TestValidatePathwayConfigRejectsMajorVersionToken(t *testing.T) {
	cfg := pathwayConfig{
		dirOut:               "/tmp/kegg",
		versionToken:         "117.0",
		assetNames:           []string{"list"},
		shouldFetchReference: true,
		retryMax:             1,
		ruleExisting:         "skip",
	}
	err := validatePathwayConfig(&cfg)
	if err == nil || !strings.Contains(err.Error(), "local snapshot key") {
		t.Fatalf("validatePathwayConfig expected snapshot key error, got: %v", err)
	}
}

func TestValidatePathwayConfigKeepsExplicitAssetSubset(t *testing.T) {
	cfg := pathwayConfig{
		dirOut:               "/tmp/kegg",
		versionToken:         "2026-04",
		assetNames:           []string{"entry,image"},
		shouldFetchReference: true,
		retryMax:             1,
		ruleExisting:         "skip",
	}
	if err := validatePathwayConfig(&cfg); err != nil {
		t.Fatalf("validatePathwayConfig returned error: %v", err)
	}
	expected := []string{"entry", "image"}
	if !reflect.DeepEqual(cfg.assetNames, expected) {
		t.Fatalf("cfg.assetNames = %#v, want %#v", cfg.assetNames, expected)
	}
}

func TestValidatePathwayConfigRejectsInvalidRuleOrder(t *testing.T) {
	cfg := pathwayConfig{
		dirOut:               "/tmp/kegg",
		versionToken:         "2026-04",
		shouldFetchReference: true,
		ruleOrder:            "reverse",
		retryMax:             1,
		ruleExisting:         "skip",
	}
	err := validatePathwayConfig(&cfg)
	if err == nil || !strings.Contains(err.Error(), "rule_order") {
		t.Fatalf("validatePathwayConfig expected rule_order error, got: %v", err)
	}
}

func TestValidatePathwayConfigResolvesAtFileInputs(t *testing.T) {
	fileOrganisms := filepath.Join(t.TempDir(), "organisms.txt")
	if err := os.WriteFile(fileOrganisms, []byte("mmu\nhsa\n"), 0o644); err != nil {
		t.Fatalf("os.WriteFile returned error: %v", err)
	}

	cfg := pathwayConfig{
		dirOut:       "/tmp/kegg",
		versionToken: "2026-04",
		organismCodes: []string{
			"@" + fileOrganisms,
		},
		ruleOrder:    ruleOrderInput,
		retryMax:     1,
		ruleExisting: "skip",
	}
	if err := validatePathwayConfig(&cfg); err != nil {
		t.Fatalf("validatePathwayConfig returned error: %v", err)
	}
	expected := []string{"mmu", "hsa"}
	if !reflect.DeepEqual(cfg.organismCodes, expected) {
		t.Fatalf("cfg.organismCodes = %#v, want %#v", cfg.organismCodes, expected)
	}
}

func TestValidatePathwayConfigResolvesPathwayIDsAtFile(t *testing.T) {
	filePathwayIDs := filepath.Join(t.TempDir(), "pathway_ids.txt")
	if err := os.WriteFile(filePathwayIDs, []byte("map00020\nmap00010\n"), 0o644); err != nil {
		t.Fatalf("os.WriteFile returned error: %v", err)
	}

	cfg := pathwayConfig{
		dirOut:               "/tmp/kegg",
		versionToken:         "2026-04",
		shouldFetchReference: true,
		pathwayIDs:           []string{"@" + filePathwayIDs},
		ruleOrder:            ruleOrderInput,
		retryMax:             1,
		ruleExisting:         "skip",
	}
	if err := validatePathwayConfig(&cfg); err != nil {
		t.Fatalf("validatePathwayConfig returned error: %v", err)
	}
	expected := []string{"map00020", "map00010"}
	if !reflect.DeepEqual(cfg.pathwayIDs, expected) {
		t.Fatalf("cfg.pathwayIDs = %#v, want %#v", cfg.pathwayIDs, expected)
	}
}

func TestReadExistingPathwayManifestBackfillsReleaseRange(t *testing.T) {
	dirTemp := t.TempDir()
	fileManifest := filepath.Join(dirTemp, "manifest.lock")
	manifest := manifestFile{
		Database:      "kegg",
		Asset:         "pathway",
		Version:       "2026-04",
		VersionToken:  "2026-04",
		SourceRelease: "118.0+/04-01",
	}
	data, err := toml.Marshal(manifest)
	if err != nil {
		t.Fatalf("toml.Marshal returned error: %v", err)
	}
	if err := os.WriteFile(fileManifest, data, 0o644); err != nil {
		t.Fatalf("os.WriteFile returned error: %v", err)
	}

	manifestRead, err := readExistingPathwayManifest(fileManifest)
	if err != nil {
		t.Fatalf("readExistingPathwayManifest returned error: %v", err)
	}
	if manifestRead.SourceReleaseStart != "118.0+/04-01" || manifestRead.SourceReleaseEnd != "118.0+/04-01" {
		t.Fatalf("manifestRead = %#v", manifestRead)
	}
}
