package omnipath

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
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
	client := createClient(false, 2, time.Hour)
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
	if _, err := lockInteractionQuery(path, nil, "dorothea", "token"); err == nil {
		t.Fatal("malformed JSON was downgraded to legacy-basic")
	}
}
