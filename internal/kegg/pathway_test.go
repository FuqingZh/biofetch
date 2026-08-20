package kegg

import (
	"bytes"
	"errors"
	"fmt"
	"github.com/FuqingZh/biofetch/internal/shared/httpx"
	"io"
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

	values, err := resolveKEGGOrganismInputs([]string{"tca,hsa", "mmu,hsa"}, ruleOrderInput)
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

	_, err := resolvePathwayIDInputs([]string{"map00010,bad"}, ruleOrderAsc)
	if err == nil || !strings.Contains(err.Error(), "invalid pathway id") {
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

func TestFetchPathwaySideAssetExhaustsTransientRetriesAndContinues(t *testing.T) {
	tests := []struct {
		name      string
		assetName string
		fileName  string
		pathRel   string
		err       error
	}{
		{name: "kgml", assetName: "pathway.kgml", fileName: "sxo00460.kgml", pathRel: "raw/sxo/sxo00460.kgml", err: io.ErrUnexpectedEOF},
		{name: "conf", assetName: "pathway.conf", fileName: "sxo00460.conf", pathRel: "raw/sxo/sxo00460.conf", err: io.ErrUnexpectedEOF},
		{name: "image", assetName: "pathway.image", fileName: "sxo00460.png", pathRel: "raw/sxo/sxo00460.png", err: io.ErrUnexpectedEOF},
		{name: "image bad record mac", assetName: "pathway.image", fileName: "sxo00460.png", pathRel: "raw/sxo/sxo00460.png", err: errors.New("local error: tls: bad record MAC")},
		{name: "image tls record version", assetName: "pathway.image", fileName: "sxo00514.png", pathRel: "raw/sxo/sxo00514.png", err: errors.New("write raw/sxo/sxo00514.png.part: tls: received record with version 1002 when expecting version 303")},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var countGET atomic.Int32
			clientHTTP := &http.Client{
				Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
					if request.Method == http.MethodHead {
						return nil, io.EOF
					}
					countGET.Add(1)
					return &http.Response{
						StatusCode: http.StatusOK,
						Status:     "200 OK",
						Body: &flakyReadCloser{
							data: []byte("partial"),
							err:  tc.err,
						},
					}, nil
				}),
			}

			dirOut := t.TempDir()
			fileOut := filepath.Join(dirOut, tc.fileName)
			clientKegg := createKEGGClient(clientHTTP, 0, 3, 0)
			_, ok, err := fetchPathwayAsset(
				clientKegg,
				false,
				fileOut,
				tc.pathRel,
				"sxo00460",
				tc.assetName,
				"https://example.test/get/sxo00460",
			)
			if err != nil {
				t.Fatalf("fetchPathwayAsset returned error: %v", err)
			}
			if ok {
				t.Fatal("ok = true")
			}
			if countGET.Load() != 3 {
				t.Fatalf("countGET = %d, want 3", countGET.Load())
			}
			if _, err := os.Stat(fileOut); !os.IsNotExist(err) {
				t.Fatalf("completed file exists or stat failed unexpectedly: %v", err)
			}
		})
	}
}

func TestFetchPathwayEntryExhaustsTransientRetriesAndContinues(t *testing.T) {
	var countGET atomic.Int32
	clientHTTP := &http.Client{
		Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
			if request.Method == http.MethodHead {
				return nil, io.EOF
			}
			countGET.Add(1)
			return &http.Response{
				StatusCode: http.StatusOK,
				Status:     "200 OK",
				Body: &flakyReadCloser{
					data: []byte("partial"),
					err:  io.ErrUnexpectedEOF,
				},
			}, nil
		}),
	}

	fileOut := filepath.Join(t.TempDir(), "sxo00460.txt")
	clientKegg := createKEGGClient(clientHTTP, 0, 3, 0)
	_, ok, err := fetchPathwayAsset(
		clientKegg,
		false,
		fileOut,
		"raw/sxo/sxo00460.txt",
		"sxo00460",
		"pathway.entry",
		"https://example.test/get/sxo00460",
	)
	if err != nil {
		t.Fatalf("fetchPathwayAsset returned error: %v", err)
	}
	if ok {
		t.Fatal("ok = true")
	}
	if countGET.Load() != 3 {
		t.Fatalf("countGET = %d, want 3", countGET.Load())
	}
}

func TestFetchPathwayEntryExhaustsStatus403ThenTransientErrorAndContinues(t *testing.T) {
	var countGET atomic.Int32
	clientHTTP := &http.Client{
		Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
			if request.Method == http.MethodHead {
				return nil, io.EOF
			}
			if countGET.Add(1) < 3 {
				return &http.Response{
					StatusCode: http.StatusForbidden,
					Status:     "403 Forbidden",
					Body:       io.NopCloser(strings.NewReader("forbidden")),
				}, nil
			}
			return nil, io.EOF
		}),
	}

	fileOut := filepath.Join(t.TempDir(), "sgh00120.txt")
	clientKegg := createKEGGClient(clientHTTP, 0, 3, 0)
	_, ok, err := fetchPathwayAsset(
		clientKegg,
		false,
		fileOut,
		"raw/sgh/sgh00120.txt",
		"sgh00120",
		"pathway.entry",
		"https://example.test/get/sgh00120",
	)
	if err != nil {
		t.Fatalf("fetchPathwayAsset returned error: %v", err)
	}
	if ok {
		t.Fatal("ok = true")
	}
	if countGET.Load() != 3 {
		t.Fatalf("countGET = %d, want 3", countGET.Load())
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

func TestRunFetchPathwaySkipsUnavailableOrganismListStatus403(t *testing.T) {
	oldBaseURL := baseURL
	defer func() {
		baseURL = oldBaseURL
	}()

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/info/pathway":
			_, _ = writer.Write([]byte("pathway\tKEGG pathway maps\npath\t586 entries\n\tLast update 2026/06/12\n"))
		case "/list/pathway/sro":
			http.Error(writer, "forbidden", http.StatusForbidden)
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
		assetNames:      []string{"list", "entry"},
		organismCodes:   []string{"sro"},
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
	if len(manifest.Files) != 0 {
		t.Fatalf("manifest.Files = %#v, want empty", manifest.Files)
	}
	if _, err := os.Stat(filepath.Join(dirOut, "pathway", "2026-04", "raw", "sro", "pathway.list.tsv")); !os.IsNotExist(err) {
		t.Fatalf("sro pathway list exists or stat failed unexpectedly: %v", err)
	}
}

func TestRunFetchPathwayContinuesAfterUnavailableOrganismListStatus403(t *testing.T) {
	oldBaseURL := baseURL
	defer func() {
		baseURL = oldBaseURL
	}()

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/info/pathway":
			_, _ = writer.Write([]byte("pathway\tKEGG pathway maps\npath\t586 entries\n\tLast update 2026/06/12\n"))
		case "/list/pathway/sro":
			http.Error(writer, "forbidden", http.StatusForbidden)
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
		assetNames:      []string{"list", "entry"},
		organismCodes:   []string{"sro", "hsa"},
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
	paths := make([]string, 0, len(manifest.Files))
	for _, file := range manifest.Files {
		paths = append(paths, file.Path)
	}
	expected := []string{"raw/hsa/pathway.list.tsv", "raw/hsa/hsa00010.txt"}
	if !reflect.DeepEqual(paths, expected) {
		t.Fatalf("manifest file paths = %#v, want %#v", paths, expected)
	}
}

func TestRunFetchPathwayReferenceListStatus403StillFails(t *testing.T) {
	oldBaseURL := baseURL
	defer func() {
		baseURL = oldBaseURL
	}()

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/info/pathway":
			_, _ = writer.Write([]byte("pathway\tKEGG pathway maps\npath\t586 entries\n\tLast update 2026/06/12\n"))
		case "/list/pathway":
			http.Error(writer, "forbidden", http.StatusForbidden)
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()
	baseURL = server.URL

	cfg := pathwayConfig{
		dirOut:               t.TempDir(),
		versionToken:         "2026-04",
		assetNames:           []string{"list"},
		shouldFetchReference: true,
		retryMax:             1,
		ruleExisting:         "skip",
		requestInterval:      0,
	}
	err := runFetchPathway(&cfg)
	if err == nil {
		t.Fatal("runFetchPathway returned nil error")
	}
	if !httpx.IsUnexpectedStatus(err, http.StatusForbidden) {
		t.Fatalf("runFetchPathway error = %v, want 403", err)
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

func TestShouldSkipPathwayScopeListError(t *testing.T) {
	errForbidden := httpx.UnexpectedStatusError{URL: "https://rest.kegg.jp/list/pathway/sro", Status: "403 Forbidden", Code: http.StatusForbidden}
	errMissing := httpx.UnexpectedStatusError{URL: "https://rest.kegg.jp/list/pathway/sro", Status: "404 Not Found", Code: http.StatusNotFound}
	errServer := httpx.UnexpectedStatusError{URL: "https://rest.kegg.jp/list/pathway/sro", Status: "503 Service Unavailable", Code: http.StatusServiceUnavailable}

	if !shouldSkipPathwayScopeListError(&pathwayConfig{organismCode: "sro"}, errForbidden) {
		t.Fatal("organism 403 should be skipped")
	}
	if !shouldSkipPathwayScopeListError(&pathwayConfig{organismCode: "sro"}, errMissing) {
		t.Fatal("organism 404 should be skipped")
	}
	if shouldSkipPathwayScopeListError(&pathwayConfig{shouldFetchReference: true}, errForbidden) {
		t.Fatal("reference 403 should not be skipped")
	}
	if !shouldSkipPathwayScopeListError(&pathwayConfig{organismCode: "sro"}, errServer) {
		t.Fatal("organism 503 after retry exhaustion should be skipped")
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
	if err == nil || !strings.Contains(err.Error(), "order") {
		t.Fatalf("validatePathwayConfig expected order error, got: %v", err)
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
			"mmu,hsa",
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
		pathwayIDs:           []string{"map00020,map00010"},
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

func TestNormalizeOrganismPrefixesPreservesFirstSeenOrder(t *testing.T) {
	got, err := normalizeOrganismPrefixes([]string{" B,a ", "b", "c,a"})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"b", "a", "c"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("prefixes = %#v, want %#v", got, want)
	}
}

func TestNormalizeOrganismPrefixesRejectsInvalidTokens(t *testing.T) {
	for _, value := range []string{"", "ab", "1", "é"} {
		if _, err := normalizeOrganismPrefixes([]string{value}); err == nil {
			t.Fatalf("normalizeOrganismPrefixes(%q) succeeded", value)
		}
	}
}

func TestPartitionOrganismsByPrefixPreservesPrefixPlan(t *testing.T) {
	groups, err := partitionOrganismsByPrefix([]string{"hsa", "bac", "bbu", "ath"}, []string{"b", "a"}, ruleOrderDesc)
	if err != nil {
		t.Fatal(err)
	}
	if len(groups) != 2 || groups[0].prefix != "b" || groups[1].prefix != "a" {
		t.Fatalf("groups = %#v", groups)
	}
	if want := []string{"bbu", "bac"}; !reflect.DeepEqual(groups[0].organismCodes, want) {
		t.Fatalf("b organisms = %#v, want %#v", groups[0].organismCodes, want)
	}
	if _, err := partitionOrganismsByPrefix([]string{"hsa"}, []string{"z"}, ruleOrderAsc); err == nil {
		t.Fatal("unmatched prefix succeeded")
	}
}

func TestKEGGClientTimeoutRetriesExactAttemptCount(t *testing.T) {
	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts.Add(1)
		time.Sleep(50 * time.Millisecond)
		_, _ = w.Write([]byte("late"))
	}))
	defer server.Close()
	clientHTTP := server.Client()
	clientHTTP.Timeout = 10 * time.Millisecond
	client := createKEGGClient(clientHTTP, 0, 3, 0)
	if _, err := client.download(server.URL); err == nil {
		t.Fatal("timed-out request succeeded")
	}
	if got := attempts.Load(); got != 3 {
		t.Fatalf("attempts = %d, want 3", got)
	}
}

func TestRunFetchPathwayPrefixUsesOneCatalogAndBoundedWorkers(t *testing.T) {
	oldBaseURL := baseURL
	defer func() { baseURL = oldBaseURL }()
	var catalogRequests atomic.Int32
	var active atomic.Int32
	var maximum atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/info/pathway":
			_, _ = w.Write([]byte("pathway Release 1\n"))
		case "/list/genome":
			catalogRequests.Add(1)
			_, _ = w.Write([]byte("T1\thsa\nT2\thpy\nT3\tbbu\n"))
		case "/list/pathway/hsa", "/list/pathway/hpy", "/list/pathway/bbu":
			n := active.Add(1)
			for old := maximum.Load(); n > old && !maximum.CompareAndSwap(old, n); old = maximum.Load() {
			}
			time.Sleep(20 * time.Millisecond)
			active.Add(-1)
			code := strings.TrimPrefix(r.URL.Path, "/list/pathway/")
			_, _ = fmt.Fprintf(w, "%s00010\tpathway\n", code)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	baseURL = server.URL
	cfg := createDefaultPathwayConfig()
	cfg.dirOut = t.TempDir()
	cfg.versionToken = "2026-04"
	cfg.assetNames = []string{"list"}
	cfg.organismPrefixes = []string{"h", "b"}
	cfg.workersMax = 2
	cfg.requestInterval = 0
	cfg.requestTimeout = time.Second
	if err := runFetchPathway(&cfg); err != nil {
		t.Fatal(err)
	}
	if catalogRequests.Load() != 1 {
		t.Fatalf("catalog requests = %d, want 1", catalogRequests.Load())
	}
	if maximum.Load() > 2 || maximum.Load() < 2 {
		t.Fatalf("max active = %d, want 2", maximum.Load())
	}
	manifest, err := readExistingPathwayManifest(filepath.Join(cfg.dirOut, "pathway", "2026-04", "manifest.lock"))
	if err != nil {
		t.Fatal(err)
	}
	if len(manifest.Files) != 3 {
		t.Fatalf("manifest files = %d, want 3", len(manifest.Files))
	}
}

func TestRunFetchPathwayCheckpointsCompletedBatchBeforeLaterFailure(t *testing.T) {
	oldBaseURL := baseURL
	defer func() { baseURL = oldBaseURL }()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/info/pathway":
			_, _ = w.Write([]byte("pathway Release 1\n"))
		case strings.HasPrefix(r.URL.Path, "/list/pathway/"):
			code := strings.TrimPrefix(r.URL.Path, "/list/pathway/")
			if code == "abg" {
				http.Error(w, "bad", http.StatusBadRequest)
				return
			}
			_, _ = fmt.Fprintf(w, "%s00010\tpathway\n", code)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	baseURL = server.URL
	codes := make([]string, 33)
	for i := range codes {
		codes[i] = fmt.Sprintf("a%c%c", 'a'+rune(i/26), 'a'+rune(i%26))
	}
	cfg := createDefaultPathwayConfig()
	cfg.dirOut, cfg.versionToken = t.TempDir(), "2026-04"
	cfg.assetNames, cfg.organismCodes = []string{"list"}, codes
	cfg.requestInterval, cfg.requestTimeout, cfg.retryMax, cfg.workersMax = 0, time.Second, 1, 4
	errRun := runFetchPathway(&cfg)
	if errRun == nil {
		t.Fatal("later batch failure succeeded")
	}
	manifest, err := readExistingPathwayManifest(filepath.Join(cfg.dirOut, "pathway", "2026-04", "manifest.lock"))
	if err != nil {
		t.Fatal(err)
	}
	if len(manifest.Files) != 32 {
		t.Fatalf("checkpoint files = %d, want 32 (run error: %v)", len(manifest.Files), errRun)
	}
}
