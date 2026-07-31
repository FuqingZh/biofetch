package omnipath

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"
)

func TestInteractionURLSeparatesQueryFieldsFromOutputHeader(t *testing.T) {
	query := interactionQuery{
		Dataset: "dorothea", License: "academic", Organisms: []string{"9606"},
		Fields: fullEvidenceQueryFields, OutputFields: fullEvidenceFields, Levels: []string{"A", "B", "C", "D"},
	}
	parsed, err := url.Parse(interactionURL(query, "9606", query.Fields, []string{"P1", "P2"}))
	if err != nil {
		t.Fatal(err)
	}
	values := parsed.Query()
	if values.Get("fields") != strings.Join(fullEvidenceQueryFields, ",") ||
		values.Get("genesymbols") != "1" || values.Get("evidences") != "1" ||
		values.Get("extra_attrs") != "1" {
		t.Fatalf("query = %v", values)
	}
	tokens := map[string]struct{}{}
	for _, token := range strings.Split(values.Get("fields"), ",") {
		tokens[token] = struct{}{}
	}
	for _, unsupported := range []string{"source", "target", "is_inhibition"} {
		if _, ok := tokens[unsupported]; ok {
			t.Fatalf("fields contains output-only token %q", unsupported)
		}
	}
	inventory, _ := url.Parse(interactionURL(query, "9606", nil, nil))
	if inventory.Query().Has("fields") || inventory.Query().Has("genesymbols") {
		t.Fatalf("inventory query is not lightweight: %v", inventory.Query())
	}
}

func TestInteractionQueryIdentitySeparatesLicensesAndDefaults(t *testing.T) {
	flag := createInteractionsFetchCommand().Flags().Lookup("license")
	if flag == nil || flag.DefValue != "academic" {
		t.Fatalf("license flag default = %#v, want academic", flag)
	}
	cfg := configInteractions{
		dirOut:       t.TempDir(),
		organisms:    []string{"9606"},
		dataset:      "collectri",
		ruleExisting: "skip",
		retryMax:     1,
	}
	if err := validateInteractionsConfig(&cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.ruleLicense != "academic" {
		t.Fatalf("default license = %q, want academic", cfg.ruleLicense)
	}
	base := interactionQuery{
		Schema: fullEvidenceSchema, Dataset: "collectri", License: "academic",
		Organisms: []string{"9606"}, Fields: fullEvidenceQueryFields, OutputFields: fullEvidenceFields,
	}
	academic, err := interactionQueryFingerprint(base)
	if err != nil {
		t.Fatal(err)
	}
	base.License = "commercial"
	commercial, err := interactionQueryFingerprint(base)
	if err != nil {
		t.Fatal(err)
	}
	if academic == commercial || academic[:12] == commercial[:12] {
		t.Fatalf("license-specific fingerprints collided: %s", academic)
	}
	acquired := time.Date(2026, 7, 31, 1, 2, 3, 0, time.UTC)
	emptyInventory := interactionInventory{SHA256: fmt.Sprintf("%x", sha256.Sum256(nil)), Edges: 0}
	tokens := map[string]string{}
	for _, license := range []string{"academic", "commercial"} {
		query := base
		query.License = license
		fingerprint, err := interactionQueryFingerprint(query)
		if err != nil {
			t.Fatal(err)
		}
		token := acquired.Format("20060102T150405.000000000Z") + "-" + fingerprint[:12]
		meta := interactionQueryMeta{
			Schema: fullEvidenceSchema, AcquiredAtUTC: acquired.Format(time.RFC3339Nano),
			Fingerprint: fingerprint, Query: query, Transport: interactionTransport,
			Start: emptyInventory, End: emptyInventory,
		}
		if err := validateInteractionQueryMeta(meta, "collectri", token); err != nil {
			t.Fatalf("%s query identity: %v", license, err)
		}
		tokens[license] = token
	}
	if tokens["academic"] == tokens["commercial"] {
		t.Fatalf("license-specific snapshot tokens collided: %#v", tokens)
	}
}

func TestNormalizeDorotheaLevelsDefaultsToADAndRejectsE(t *testing.T) {
	levels, err := normalizeDorotheaLevels(nil)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(levels, []string{"A", "B", "C", "D"}) {
		t.Fatalf("levels = %#v", levels)
	}
	if _, err := normalizeDorotheaLevels([]string{"E"}); err == nil {
		t.Fatal("accepted unavailable DoRothEA level E")
	}
}

func TestValidateLeafRowsRejectsTargetJSONAndLevelViolations(t *testing.T) {
	row := make([]string, len(fullEvidenceFields))
	index := map[string]int{}
	for i, field := range fullEvidenceFields {
		index[field] = i
	}
	row[index["target"]] = "outside"
	row[index["evidences"]] = "{}"
	row[index["extra_attrs"]] = "{}"
	row[index["dorothea_level"]] = "A"
	table := tsvTable{rows: [][]string{row}, index: index}
	if err := validateLeafRows(table, []string{"locked"}, []string{"A", "B", "C", "D"}); err == nil {
		t.Fatal("accepted target outside leaf")
	}
	row[index["target"]] = "locked"
	row[index["evidences"]] = "invalid"
	if err := validateLeafRows(table, []string{"locked"}, []string{"A", "B", "C", "D"}); err == nil {
		t.Fatal("accepted invalid evidence JSON")
	}
	row[index["evidences"]] = "{}"
	row[index["dorothea_level"]] = "A;D"
	if err := validateLeafRows(table, []string{"locked"}, []string{"A", "B", "C", "D"}); err != nil {
		t.Fatalf("rejected semicolon-delimited A-D levels: %v", err)
	}
	row[index["dorothea_level"]] = "E"
	if err := validateLeafRows(table, []string{"locked"}, []string{"A", "B", "C", "D"}); err == nil {
		t.Fatal("accepted DoRothEA E")
	}
}

func TestParseTSVPreservesBareAndLongJSONFields(t *testing.T) {
	header := []string{"source", "target", "extra_attrs", "evidences"}
	longValue := strings.Repeat("evidence", 20_000)
	data := strings.Join(header, "\t") + "\n" +
		"P1\tP2\t{\"kind\":\"bare quotes\"}\t{\"text\":\"" + longValue + "\"}\n"
	table, err := parseTSV([]byte(data), header)
	if err != nil {
		t.Fatalf("parseTSV returned error for OmniPath-style JSON: %v", err)
	}
	if len(table.rows) != 1 || table.rows[0][2] != `{"kind":"bare quotes"}` ||
		!strings.Contains(table.rows[0][3], longValue) {
		t.Fatalf("parsed row was not preserved")
	}
}

func TestDownloadBatchHonorsHTTPDateWithoutRealSleep(t *testing.T) {
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		attempts++
		if attempts == 1 {
			writer.Header().Set("Retry-After", "Wed, 21 Oct 2015 07:28:10 GMT")
			writer.WriteHeader(http.StatusTooManyRequests)
			return
		}
		_, _ = writer.Write([]byte("ok"))
	}))
	defer server.Close()
	originalSleep, originalNow := interactionSleep, interactionNow
	t.Cleanup(func() { interactionSleep, interactionNow = originalSleep, originalNow })
	var waited time.Duration
	interactionNow = func() time.Time { return time.Date(2015, 10, 21, 7, 28, 0, 0, time.UTC) }
	interactionSleep = func(value time.Duration) { waited = value }
	client := createClient(false, 2, time.Second)
	if _, _, err := client.downloadBatch(server.URL); err != nil {
		t.Fatal(err)
	}
	if attempts != 2 || waited != 10*time.Second {
		t.Fatalf("attempts=%d waited=%s", attempts, waited)
	}
}

func TestLockInteractionQueryRejectsMalformedJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "query_meta.json")
	if err := os.WriteFile(path, []byte(`{"schema":"full-evidence-v1"`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := lockInteractionQuery(filepath.Dir(path), path, nil, "dorothea", "token"); err == nil {
		t.Fatal("malformed JSON was downgraded to legacy-basic")
	}
}

func TestRunLockInvalidSidecarKeepsLogsOutsideSnapshot(t *testing.T) {
	for _, data := range []string{`{"schema":"full-evidence-v1"`, "not a capability table\n"} {
		t.Run(fmt.Sprintf("bytes-%d", len(data)), func(t *testing.T) {
			root := t.TempDir()
			dirVersion := filepath.Join(root, "interactions", "collectri", "broken")
			if err := os.MkdirAll(filepath.Join(dirVersion, "raw", "9606"), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dirVersion, "raw", "query_meta.json"), []byte(data), 0o644); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dirVersion, "raw", "9606", "interactions.tsv"),
				[]byte("source\ttarget\nS1\tT1\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			if err := runLockInteractions(&lockConfig{dirSnapshot: dirVersion, workersMax: 1}); err == nil {
				t.Fatal("lock accepted invalid query sidecar")
			}
			if _, err := os.Stat(filepath.Join(dirVersion, "logs")); !os.IsNotExist(err) {
				t.Fatalf("invalid lock wrote logs into snapshot: %v", err)
			}
		})
	}
}

func TestLockInteractionQueryRejectsArbitraryLegacyText(t *testing.T) {
	path := filepath.Join(t.TempDir(), "query_meta.json")
	if err := os.WriteFile(path, []byte("not a capability table\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := lockInteractionQuery(filepath.Dir(path), path, nil, "collectri", "token"); err == nil {
		t.Fatal("arbitrary text was downgraded to legacy-basic")
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return fn(request)
}

func testHTTPResponse(status int, body string, headers http.Header) *http.Response {
	if headers == nil {
		headers = http.Header{}
	}
	return &http.Response{
		StatusCode: status,
		Status:     fmt.Sprintf("%d %s", status, http.StatusText(status)),
		Header:     headers,
		Body:       io.NopCloser(bytes.NewBufferString(body)),
	}
}

func testInventoryTSV(pairs ...[2]string) string {
	var builder strings.Builder
	builder.WriteString(strings.Join(inventoryFields, "\t"))
	builder.WriteByte('\n')
	for _, pair := range pairs {
		builder.WriteString(strings.Join([]string{
			pair[0], pair[1], "1", "1", "0", "1", "1", "0",
		}, "\t"))
		builder.WriteByte('\n')
	}
	return builder.String()
}

func testFullEvidenceRow(source, target, level string) []string {
	row := make([]string, len(fullEvidenceFields))
	index := map[string]int{}
	for i, field := range fullEvidenceFields {
		index[field] = i
	}
	row[index["source"]] = source
	row[index["target"]] = target
	row[index["source_genesymbol"]] = "SOURCE"
	row[index["target_genesymbol"]] = "TARGET"
	row[index["is_directed"]] = "1"
	row[index["is_stimulation"]] = "1"
	row[index["is_inhibition"]] = "0"
	row[index["consensus_direction"]] = "1"
	row[index["consensus_stimulation"]] = "1"
	row[index["consensus_inhibition"]] = "0"
	row[index["sources"]] = "test"
	row[index["references"]] = "PMID:1"
	row[index["dorothea_level"]] = level
	row[index["type"]] = "transcriptional"
	row[index["curation_effort"]] = "1"
	row[index["extra_attrs"]] = `{"source":"test"}`
	row[index["evidences"]] = `{"evidence":"test"}`
	row[index["ncbi_tax_id_source"]] = "9606"
	row[index["ncbi_tax_id_target"]] = "9606"
	return row
}

func testFullEvidenceTSV(rows ...[]string) string {
	var builder strings.Builder
	builder.WriteString(strings.Join(fullEvidenceFields, "\t"))
	builder.WriteByte('\n')
	for _, row := range rows {
		builder.WriteString(strings.Join(row, "\t"))
		builder.WriteByte('\n')
	}
	return builder.String()
}

func testInteractionQuery(dataset, license string) interactionQuery {
	query := interactionQuery{
		Schema: fullEvidenceSchema, Dataset: dataset, License: license,
		Organisms: []string{"9606"}, Fields: append([]string(nil), fullEvidenceQueryFields...),
		OutputFields: append([]string(nil), fullEvidenceFields...),
	}
	if dataset == "dorothea" {
		query.Levels = []string{"A", "B", "C", "D"}
	}
	return query
}

func TestFetchInteractionBatchSplits502And503(t *testing.T) {
	for _, status := range []int{http.StatusBadGateway, http.StatusServiceUnavailable} {
		t.Run(fmt.Sprintf("status-%d", status), func(t *testing.T) {
			client := &omnipathClient{
				clientHTTP: &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
					targets := strings.Split(request.URL.Query().Get("targets"), ",")
					if len(targets) > 1 {
						return testHTTPResponse(status, "", nil), nil
					}
					return testHTTPResponse(http.StatusOK, testFullEvidenceTSV(
						testFullEvidenceRow("S-"+targets[0], targets[0], ""),
					), nil), nil
				})},
				retryMax: 1,
			}
			query := testInteractionQuery("collectri", "academic")
			pathBase := filepath.Join(t.TempDir(), "leaf")
			leaves, err := fetchInteractionBatch(client, query, "9606", []string{"T1", "T2"},
				map[string]int{"T1": 1, "T2": 1}, pathBase)
			if err != nil {
				t.Fatal(err)
			}
			if len(leaves) != 2 || !reflect.DeepEqual(leaves[0].meta.Targets, []string{"T1"}) ||
				!reflect.DeepEqual(leaves[1].meta.Targets, []string{"T2"}) {
				t.Fatalf("split leaves = %#v", leaves)
			}
		})
	}
}

func TestPlanInteractionBatchesEnforcesAllBounds(t *testing.T) {
	query := testInteractionQuery("collectri", "academic")
	pairs := make([][2]string, 0, 101)
	for index := 0; index < 101; index++ {
		pairs = append(pairs, [2]string{fmt.Sprintf("S%03d", index), fmt.Sprintf("T%03d", index)})
	}
	inventory, err := parseTSV([]byte(testInventoryTSV(pairs...)), inventoryFields)
	if err != nil {
		t.Fatal(err)
	}
	batches, err := planInteractionBatches(query, "9606", inventory)
	if err != nil {
		t.Fatal(err)
	}
	if len(batches) != 2 || len(batches[0]) > maxBatchTargets || len(batches[1]) > maxBatchTargets {
		t.Fatalf("target-bounded batches = %#v", batches)
	}
	for _, batch := range batches {
		if len(interactionURL(query, "9606", query.Fields, batch)) > maxBatchEncodedURL {
			t.Fatal("planned an oversized URL")
		}
	}

	tooManyEdges := make([][2]string, 0, maxBatchEdges+1)
	for index := 0; index <= maxBatchEdges; index++ {
		tooManyEdges = append(tooManyEdges, [2]string{fmt.Sprintf("S%03d", index), "T1"})
	}
	inventory, err = parseTSV([]byte(testInventoryTSV(tooManyEdges...)), inventoryFields)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := planInteractionBatches(query, "9606", inventory); err == nil {
		t.Fatal("accepted a single target above the expected-edge bound")
	}

	inventory, err = parseTSV([]byte(testInventoryTSV([2]string{"S1", strings.Repeat("T", maxBatchEncodedURL)})), inventoryFields)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := planInteractionBatches(query, "9606", inventory); err == nil {
		t.Fatal("accepted a single target above the encoded-URL bound")
	}
}

func TestFetchInteractionBatchSingletonFailureAborts(t *testing.T) {
	client := &omnipathClient{
		clientHTTP: &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return testHTTPResponse(http.StatusServiceUnavailable, "", nil), nil
		})},
		retryMax: 1,
	}
	_, err := fetchInteractionBatch(client, testInteractionQuery("collectri", "academic"), "9606",
		[]string{"T1"}, map[string]int{"T1": 1}, filepath.Join(t.TempDir(), "leaf"))
	if err == nil || !strings.Contains(err.Error(), "targets=1") {
		t.Fatalf("singleton failure error = %v", err)
	}
}

func TestParseTSVRejectsBadHeaderAndRowWidth(t *testing.T) {
	if _, err := parseTSV([]byte("source\twrong\nS\tT\n"), inventoryFields); err == nil {
		t.Fatal("accepted bad inventory header")
	}
	if _, err := parseTSV([]byte(strings.Join(inventoryFields, "\t")+"\nS\tT\n"), inventoryFields); err == nil {
		t.Fatal("accepted short TSV row")
	}
}

func TestAssembleInteractionLeavesRejectsMissingAndDuplicateEdges(t *testing.T) {
	inventoryTwo, err := parseTSV([]byte(testInventoryTSV([2]string{"S1", "T1"}, [2]string{"S2", "T2"})), inventoryFields)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	missingPath := filepath.Join(dir, "missing.part")
	if err := os.WriteFile(missingPath, []byte(testFullEvidenceTSV(testFullEvidenceRow("S1", "T1", ""))), 0o644); err != nil {
		t.Fatal(err)
	}
	err = assembleInteractionLeaves(filepath.Join(dir, "missing.tsv"), []stagedInteractionLeaf{{
		meta: interactionLeaf{Organism: "9606", Targets: []string{"T1"}, ExpectedEdges: 1},
		path: missingPath,
	}}, inventoryTwo, nil)
	if err == nil || !strings.Contains(err.Error(), "edge count mismatch") {
		t.Fatalf("missing edge error = %v", err)
	}

	inventoryOne, err := parseTSV([]byte(testInventoryTSV([2]string{"S1", "T1"})), inventoryFields)
	if err != nil {
		t.Fatal(err)
	}
	duplicateA := filepath.Join(dir, "duplicate-a.part")
	duplicateB := filepath.Join(dir, "duplicate-b.part")
	data := []byte(testFullEvidenceTSV(testFullEvidenceRow("S1", "T1", "")))
	if err := os.WriteFile(duplicateA, data, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(duplicateB, data, 0o644); err != nil {
		t.Fatal(err)
	}
	err = assembleInteractionLeaves(filepath.Join(dir, "duplicate.tsv"), []stagedInteractionLeaf{
		{meta: interactionLeaf{Organism: "9606", Targets: []string{"T1"}, ExpectedEdges: 1}, path: duplicateA},
		{meta: interactionLeaf{Organism: "9606", Targets: []string{"T1"}, ExpectedEdges: 1}, path: duplicateB},
	}, inventoryOne, nil)
	if err == nil || !strings.Contains(err.Error(), "duplicate full-evidence edge") {
		t.Fatalf("duplicate edge error = %v", err)
	}
}

func testCapabilitiesTSV(query interactionQuery) string {
	return strings.Join([]string{
		"argument\tvalues",
		"datasets\tcollectri;dorothea;kinaseextra",
		"license\tacademic;commercial",
		"organisms\t9606",
		"fields\t" + strings.Join(query.Fields, ";"),
		"dorothea_levels\tA;B;C;D",
	}, "\n") + "\n"
}

func TestValidateInteractionCapabilitiesRejectsUnavailableContract(t *testing.T) {
	for _, test := range []struct {
		name  string
		query interactionQuery
		body  string
	}{
		{
			name:  "commercial-license",
			query: testInteractionQuery("collectri", "commercial"),
			body: strings.Replace(
				testCapabilitiesTSV(testInteractionQuery("collectri", "commercial")),
				"license\tacademic;commercial", "license\tacademic", 1,
			),
		},
		{
			name:  "fixed-evidence-field",
			query: testInteractionQuery("collectri", "academic"),
			body: strings.Replace(
				testCapabilitiesTSV(testInteractionQuery("collectri", "academic")),
				"fields\t"+strings.Join(fullEvidenceQueryFields, ";"),
				"fields\tcuration_effort", 1,
			),
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			client := &omnipathClient{
				clientHTTP: &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
					return testHTTPResponse(http.StatusOK, test.body, nil), nil
				})},
				retryMax: 1,
			}
			if err := validateInteractionCapabilities(client, test.query); err == nil {
				t.Fatal("accepted unavailable OmniPath capability contract")
			}
		})
	}
}

func TestRunFetchInteractionsLiveRejectsInventoryDrift(t *testing.T) {
	query := testInteractionQuery("collectri", "academic")
	inventoryCalls := 0
	client := &omnipathClient{
		clientHTTP: &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
			switch request.URL.Path {
			case "/queries/interactions":
				return testHTTPResponse(http.StatusOK, testCapabilitiesTSV(query), nil), nil
			case "/interactions":
				if request.URL.Query().Get("fields") != "" {
					return testHTTPResponse(http.StatusOK,
						testFullEvidenceTSV(testFullEvidenceRow("S1", "T1", "")), nil), nil
				}
				inventoryCalls++
				if inventoryCalls == 1 {
					return testHTTPResponse(http.StatusOK, testInventoryTSV([2]string{"S1", "T1"}), nil), nil
				}
				return testHTTPResponse(http.StatusOK, testInventoryTSV([2]string{"S2", "T2"}), nil), nil
			default:
				return testHTTPResponse(http.StatusNotFound, "", nil), nil
			}
		})},
		retryMax: 1,
	}
	cfg := &configInteractions{
		dirOut: t.TempDir(), dirLogs: t.TempDir(), dataset: "collectri", ruleLicense: "academic",
		retryMax: 1,
	}
	err := runFetchInteractionsLive(client, cfg, []string{"9606"}, "organism", "9606")
	if err == nil || !strings.Contains(err.Error(), "inventory drifted") {
		t.Fatalf("inventory drift error = %v", err)
	}
	entries, globErr := filepath.Glob(filepath.Join(cfg.dirOut, "interactions", "collectri", "*"))
	if globErr != nil {
		t.Fatal(globErr)
	}
	if len(entries) != 0 {
		t.Fatalf("inventory drift published snapshot artifacts: %#v", entries)
	}
}

func TestRunFetchInteractionsLiveRejectsExistingStagingDirectory(t *testing.T) {
	query := testInteractionQuery("collectri", "academic")
	fingerprint, err := interactionQueryFingerprint(query)
	if err != nil {
		t.Fatal(err)
	}
	acquired := time.Date(2026, 7, 31, 1, 2, 3, 0, time.UTC)
	originalNow := interactionNow
	t.Cleanup(func() { interactionNow = originalNow })
	interactionNow = func() time.Time { return acquired }
	root := t.TempDir()
	versionToken := acquired.Format("20060102T150405.000000000Z") + "-" + fingerprint[:12]
	staging := filepath.Join(root, "interactions", "collectri", versionToken+".part")
	if err := os.MkdirAll(staging, 0o755); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(staging, "inspect-before-removal")
	if err := os.WriteFile(marker, []byte("partial"), 0o644); err != nil {
		t.Fatal(err)
	}
	client := &omnipathClient{
		clientHTTP: &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			t.Fatal("network was used before rejecting stale staging")
			return nil, nil
		})},
		retryMax: 1,
	}
	cfg := &configInteractions{
		dirOut: root, dirLogs: t.TempDir(), dataset: "collectri", ruleLicense: "academic", retryMax: 1,
	}
	err = runFetchInteractionsLive(client, cfg, []string{"9606"}, "organism", "9606")
	if err == nil || !strings.Contains(err.Error(), "staging directory already exists") {
		t.Fatalf("stale staging error = %v", err)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("stale staging was destructively removed: %v", err)
	}
}

func TestValidateFullEvidenceLayoutRejectsUnexpectedFiles(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "raw", "9606"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{
		filepath.Join(root, "manifest.lock"),
		filepath.Join(root, "raw", "query_meta.json"),
		filepath.Join(root, "raw", "9606", "interactions.tsv"),
	} {
		if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	extra := filepath.Join(root, "raw", "leftover.part")
	if err := os.WriteFile(extra, []byte("partial"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := validateFullEvidenceLayout(root, []string{"9606"}, true); err == nil {
		t.Fatal("accepted unexpected full-evidence snapshot file")
	}
	if err := os.Remove(extra); err != nil {
		t.Fatal(err)
	}
	if err := validateFullEvidenceLayout(root, []string{"9606"}, true); err != nil {
		t.Fatalf("rejected exact full-evidence snapshot layout: %v", err)
	}
}

func createFullEvidenceSnapshot(t *testing.T, license string) (string, interactionQueryMeta) {
	t.Helper()
	query := testInteractionQuery("collectri", license)
	fingerprint, err := interactionQueryFingerprint(query)
	if err != nil {
		t.Fatal(err)
	}
	acquired := time.Date(2026, 7, 31, 1, 2, 3, 456789000, time.UTC)
	versionToken := acquired.Format("20060102T150405.000000000Z") + "-" + fingerprint[:12]
	root := t.TempDir()
	dirVersion := filepath.Join(root, "interactions", "collectri", versionToken)
	if err := os.MkdirAll(filepath.Join(dirVersion, "raw", "9606"), 0o755); err != nil {
		t.Fatal(err)
	}
	inventory, err := parseTSV([]byte(testInventoryTSV([2]string{"S1", "T1"})), inventoryFields)
	if err != nil {
		t.Fatal(err)
	}
	keys := []string{"9606\x1f" + edgeKey(inventory, inventory.rows[0])}
	sort.Strings(keys)
	digest := fmt.Sprintf("%x", sha256.Sum256([]byte(strings.Join(keys, "\n"))))
	leaf := interactionLeaf{
		Organism: "9606", Targets: []string{"T1"}, ExpectedEdges: 1,
		URL: interactionURL(query, "9606", query.Fields, []string{"T1"}),
	}
	meta := interactionQueryMeta{
		Schema: fullEvidenceSchema, AcquiredAtUTC: acquired.Format(time.RFC3339Nano),
		Fingerprint: fingerprint, Query: query, Transport: interactionTransport,
		Start:       interactionInventory{SHA256: digest, Edges: 1},
		End:         interactionInventory{SHA256: digest, Edges: 1},
		LeafBatches: []interactionLeaf{leaf},
	}
	if err := writeJSONAtomic(filepath.Join(dirVersion, "raw", "query_meta.json"), meta); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dirVersion, "raw", "9606", "interactions.tsv"),
		[]byte(testFullEvidenceTSV(testFullEvidenceRow("S1", "T1", ""))), 0o644); err != nil {
		t.Fatal(err)
	}
	return dirVersion, meta
}

func TestLockInteractionQueryRebuildsFullEvidenceManifestFromSidecar(t *testing.T) {
	dirVersion, meta := createFullEvidenceSnapshot(t, "commercial")
	cfg := &lockConfig{dirSnapshot: dirVersion, workersMax: 1}
	if err := runLockInteractions(cfg); err != nil {
		t.Fatal(err)
	}
	manifest, err := readExistingManifest(filepath.Join(dirVersion, "manifest.lock"))
	if err != nil {
		t.Fatal(err)
	}
	if err := validateLockedQuery(manifest); err != nil {
		t.Fatal(err)
	}
	if manifest.Query.License != "commercial" || manifest.Query.Fingerprint != meta.Fingerprint ||
		manifest.RequestURL != "" || manifest.QueryURL != queryInteractionsURL {
		t.Fatalf("rebuilt query = %#v", manifest.Query)
	}
	for _, record := range manifest.Files {
		if record.URL != "" {
			t.Fatalf("full-evidence record guessed URL: %#v", record)
		}
	}
	if _, err := os.Stat(filepath.Join(dirVersion, "logs")); !os.IsNotExist(err) {
		t.Fatalf("full-evidence lock created snapshot-local logs: %v", err)
	}
	before, err := os.ReadFile(filepath.Join(dirVersion, "manifest.lock"))
	if err != nil {
		t.Fatal(err)
	}
	if err := runLockInteractions(cfg); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(filepath.Join(dirVersion, "manifest.lock"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("sidecar-based lock rebuild was not deterministic")
	}
}

func TestLockInteractionQueryRejectsNonCanonicalOrUnknownSidecarJSON(t *testing.T) {
	for _, test := range []struct {
		name string
		data func(interactionQueryMeta) []byte
	}{
		{
			name: "minified",
			data: func(meta interactionQueryMeta) []byte {
				data, err := json.Marshal(meta)
				if err != nil {
					t.Fatal(err)
				}
				return data
			},
		},
		{
			name: "unknown-field",
			data: func(meta interactionQueryMeta) []byte {
				data, err := json.MarshalIndent(meta, "", "  ")
				if err != nil {
					t.Fatal(err)
				}
				text := strings.TrimSuffix(string(data), "\n")
				text = strings.TrimSuffix(text, "}") + ",\n  \"unknown\": true\n}\n"
				return []byte(text)
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			dirVersion, meta := createFullEvidenceSnapshot(t, "academic")
			if err := os.WriteFile(filepath.Join(dirVersion, "raw", "query_meta.json"), test.data(meta), 0o644); err != nil {
				t.Fatal(err)
			}
			if err := runLockInteractions(&lockConfig{
				dirSnapshot: dirVersion, dirLogs: t.TempDir(), workersMax: 1,
			}); err == nil {
				t.Fatal("lock accepted a sidecar that restore cannot reproduce byte-for-byte")
			}
		})
	}
}

func TestLockInteractionQueryValidatesFinalSnapshot(t *testing.T) {
	for _, test := range []struct {
		name string
		data string
	}{
		{name: "bad-header", data: "source\ttarget\nS1\tT1\n"},
		{name: "bad-json", data: func() string {
			row := testFullEvidenceRow("S1", "T1", "")
			for index, field := range fullEvidenceFields {
				if field == "evidences" {
					row[index] = "not-json"
				}
			}
			return testFullEvidenceTSV(row)
		}()},
		{name: "missing-edge", data: testFullEvidenceTSV()},
		{name: "duplicate-edge", data: testFullEvidenceTSV(
			testFullEvidenceRow("S1", "T1", ""),
			testFullEvidenceRow("S1", "T1", ""),
		)},
	} {
		t.Run(test.name, func(t *testing.T) {
			dirVersion, _ := createFullEvidenceSnapshot(t, "academic")
			path := filepath.Join(dirVersion, "raw", "9606", "interactions.tsv")
			if err := os.WriteFile(path, []byte(test.data), 0o644); err != nil {
				t.Fatal(err)
			}
			err := runLockInteractions(&lockConfig{dirSnapshot: dirVersion, dirLogs: t.TempDir(), workersMax: 1})
			if err == nil {
				t.Fatal("lock accepted corrupted full-evidence snapshot")
			}
		})
	}
}

func TestLockInteractionQueryRejectsExtraSnapshotFiles(t *testing.T) {
	dirVersion, _ := createFullEvidenceSnapshot(t, "academic")
	if err := os.WriteFile(filepath.Join(dirVersion, "unexpected.txt"), []byte("extra"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := runLockInteractions(&lockConfig{
		dirSnapshot: dirVersion, dirLogs: t.TempDir(), workersMax: 1,
	}); err == nil {
		t.Fatal("lock accepted an extra full-evidence snapshot file")
	}
}

func TestLockInteractionQueryKeepsLegacyBasicSidecar(t *testing.T) {
	root := t.TempDir()
	dirVersion := filepath.Join(root, "interactions", "collectri", "2025-08-13")
	if err := os.MkdirAll(filepath.Join(dirVersion, "raw", "9606"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dirVersion, "raw", "query_meta.json"),
		[]byte("argument\tvalues\nfields\tsource;target\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dirVersion, "raw", "9606", "interactions.tsv"),
		[]byte("source\ttarget\nS1\tT1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := runLockInteractions(&lockConfig{
		dirSnapshot: dirVersion, dirLogs: t.TempDir(), workersMax: 1,
	}); err != nil {
		t.Fatal(err)
	}
	manifest, err := readExistingManifest(filepath.Join(dirVersion, "manifest.lock"))
	if err != nil {
		t.Fatal(err)
	}
	if manifest.Query.Schema != "legacy-basic" {
		t.Fatalf("legacy sidecar schema = %q", manifest.Query.Schema)
	}
}

func TestRestoreFullEvidenceDriftLeavesManifestAndFilesUnchanged(t *testing.T) {
	dirVersion, _ := createFullEvidenceSnapshot(t, "academic")
	if err := runLockInteractions(&lockConfig{
		dirSnapshot: dirVersion, dirLogs: t.TempDir(), workersMax: 1,
	}); err != nil {
		t.Fatal(err)
	}
	manifestPath := filepath.Join(dirVersion, "manifest.lock")
	dataPath := filepath.Join(dirVersion, "raw", "9606", "interactions.tsv")
	manifestBefore, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	dataBefore, err := os.ReadFile(dataPath)
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := readExistingManifest(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	client := &omnipathClient{
		clientHTTP: &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return testHTTPResponse(http.StatusOK,
				testFullEvidenceTSV(testFullEvidenceRow("CHANGED", "T1", "")), nil), nil
		})},
		retryMax: 1,
	}
	err = restoreFullEvidenceWithClient(client, &restoreConfig{retryMax: 1}, dirVersion, manifest)
	if err == nil || !strings.Contains(err.Error(), "drift") {
		t.Fatalf("restore drift error = %v", err)
	}
	manifestAfter, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	dataAfter, err := os.ReadFile(dataPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(manifestBefore, manifestAfter) || !bytes.Equal(dataBefore, dataAfter) {
		t.Fatal("restore drift modified the locked snapshot")
	}
	if _, err := os.Stat(filepath.Join(dirVersion, ".restore.part")); !os.IsNotExist(err) {
		t.Fatalf("restore staging directory remains: %v", err)
	}
}
