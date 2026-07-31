package omnipath

import (
	"biofetch/internal/shared/logx"
	"biofetch/internal/shared/parallel"
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	fullEvidenceSchema   = "full-evidence-v1"
	maxBatchEdges        = 500
	maxBatchTargets      = 100
	maxBatchEncodedURL   = 6 * 1024
	interactionTransport = "target-batches-v1"
)

var fullEvidenceFields = []string{
	"source", "target", "source_genesymbol", "target_genesymbol",
	"is_directed", "is_stimulation", "is_inhibition",
	"consensus_direction", "consensus_stimulation", "consensus_inhibition",
	"sources", "references", "dorothea_curated", "dorothea_chipseq",
	"dorothea_tfbs", "dorothea_coexp", "dorothea_level", "type",
	"curation_effort", "extra_attrs", "evidences",
	"ncbi_tax_id_source", "ncbi_tax_id_target",
}

var fullEvidenceQueryFields = []string{
	"curation_effort", "dorothea_level", "dorothea_methods", "evidences",
	"extra_attrs", "ncbi_tax_id", "references", "sources", "type",
}

var interactionSleep = time.Sleep
var interactionNow = time.Now

var inventoryFields = []string{
	"source", "target", "is_directed", "is_stimulation", "is_inhibition",
	"consensus_direction", "consensus_stimulation", "consensus_inhibition",
}

type interactionQuery struct {
	Schema       string   `json:"schema"`
	Dataset      string   `json:"dataset"`
	License      string   `json:"license"`
	Organisms    []string `json:"organisms"`
	Fields       []string `json:"fields"`
	OutputFields []string `json:"output_fields"`
	Levels       []string `json:"levels"`
}

type interactionInventory struct {
	SHA256 string `json:"sha256"`
	Edges  int    `json:"edges"`
}

type interactionLeaf struct {
	Organism      string   `json:"organism"`
	Targets       []string `json:"targets"`
	ExpectedEdges int      `json:"expected_edges"`
	URL           string   `json:"url"`
}

type interactionQueryMeta struct {
	Schema        string               `json:"schema"`
	AcquiredAtUTC string               `json:"acquired_at_utc"`
	Fingerprint   string               `json:"fingerprint"`
	Query         interactionQuery     `json:"query"`
	Transport     string               `json:"transport"`
	Start         interactionInventory `json:"start_inventory"`
	End           interactionInventory `json:"end_inventory"`
	LeafBatches   []interactionLeaf    `json:"leaf_batches"`
}

type tsvTable struct {
	header []string
	rows   [][]string
	index  map[string]int
}

type stagedInteractionLeaf struct {
	meta interactionLeaf
	path string
}

func runFetchInteractionsLive(client *omnipathClient, cfg *configInteractions, taxIDs []string, scopeType, scopeValue string) error {
	acquired := time.Now().UTC()
	query := interactionQuery{
		Schema: fullEvidenceSchema, Dataset: cfg.dataset, License: normalizeLicense(cfg.ruleLicense),
		Organisms: append([]string(nil), taxIDs...), Fields: append([]string(nil), fullEvidenceQueryFields...),
		OutputFields: append([]string(nil), fullEvidenceFields...),
		Levels:       append([]string(nil), cfg.dorotheaLevels...),
	}
	canonical, err := json.Marshal(query)
	if err != nil {
		return err
	}
	fingerprint := fmt.Sprintf("%x", sha256.Sum256(canonical))
	versionToken := acquired.Format("20060102T150405.000000000Z") + "-" + fingerprint[:12]
	dirVersion := filepath.Join(cfg.dirOut, "interactions", cfg.dataset, versionToken)
	if cfg.shouldDryRun {
		logf("[dry-run] version dir: %s", dirVersion)
		logf("[dry-run] canonical query: %s", canonical)
		return nil
	}
	dirWork := dirVersion + ".part"
	if err := os.MkdirAll(filepath.Join(dirWork, "raw"), 0o755); err != nil {
		return fmt.Errorf("create raw dir: %w", err)
	}
	defer os.RemoveAll(dirWork)
	logDir := cfg.dirLogs
	if logDir == "" {
		logDir = filepath.Join(cfg.dirOut, "logs", "omnipath", "interactions", cfg.dataset)
	}
	_, closeRun, err := logx.StartVersionedRun("biofetch omnipath", "fetch", logDir, dirVersion)
	if err != nil {
		return err
	}
	defer closeRun()
	if err := validateInteractionCapabilities(client, query); err != nil {
		return err
	}

	startTables, startInv, err := fetchInventories(client, query)
	if err != nil {
		return err
	}
	leaves := make([]interactionLeaf, 0)
	records := make([]recordFile, 0, len(taxIDs)+1)
	for _, taxID := range taxIDs {
		table := startTables[taxID]
		batches, err := planInteractionBatches(query, taxID, table)
		if err != nil {
			return err
		}
		counts := inventoryTargetCounts(table)
		type batchTask struct {
			index   int
			targets []string
		}
		tasks := make([]batchTask, len(batches))
		for index, batch := range batches {
			tasks[index] = batchTask{index: index, targets: batch}
		}
		results, err := parallel.MapOrderedWithWorkers(tasks, 2, func(task batchTask) ([]stagedInteractionLeaf, error) {
			leafPath := filepath.Join(dirWork, fmt.Sprintf(".leaf-%s-%06d", taxID, task.index))
			return fetchInteractionBatch(client, query, taxID, task.targets, counts, leafPath)
		})
		if err != nil {
			return err
		}
		staged := make([]stagedInteractionLeaf, 0, len(batches))
		for _, actual := range results {
			staged = append(staged, actual...)
			for _, leaf := range actual {
				leaves = append(leaves, leaf.meta)
			}
		}
		dirRaw := filepath.Join(dirWork, "raw", taxID)
		if err := os.MkdirAll(dirRaw, 0o755); err != nil {
			return err
		}
		fileFinal := filepath.Join(dirRaw, "interactions.tsv")
		if err := assembleInteractionLeaves(fileFinal, staged, table, query.Levels); err != nil {
			return err
		}
		for _, leaf := range staged {
			if err := os.Remove(leaf.path); err != nil {
				return fmt.Errorf("remove staged leaf: %w", err)
			}
		}
		record, err := buildRecord(fileFinal, filepath.ToSlash(filepath.Join("raw", taxID, "interactions.tsv")), "", "interactions")
		if err != nil {
			return err
		}
		records = append(records, record)
	}
	_, endInv, err := fetchInventories(client, query)
	if err != nil {
		return err
	}
	if startInv != endInv {
		return fmt.Errorf("OmniPath inventory drifted during acquisition: start=%s/%d end=%s/%d", startInv.SHA256, startInv.Edges, endInv.SHA256, endInv.Edges)
	}
	meta := interactionQueryMeta{
		Schema: fullEvidenceSchema, AcquiredAtUTC: acquired.Format(time.RFC3339Nano),
		Fingerprint: fingerprint, Query: query, Transport: interactionTransport,
		Start: startInv, End: endInv, LeafBatches: leaves,
	}
	fileMeta := filepath.Join(dirWork, "raw", "query_meta.json")
	if err := writeJSONAtomic(fileMeta, meta); err != nil {
		return err
	}
	metaRecord, err := buildRecord(fileMeta, "raw/query_meta.json", "", "query_meta")
	if err != nil {
		return err
	}
	records = append(records, metaRecord)
	sort.Slice(records, func(i, j int) bool { return records[i].Path < records[j].Path })
	manifestLeaves := make([]manifestLeaf, len(leaves))
	for i, leaf := range leaves {
		manifestLeaves[i] = manifestLeaf(leaf)
	}
	manifest := manifestFile{
		Database: "omnipath", Asset: "interactions", Dataset: cfg.dataset,
		Version: meta.AcquiredAtUTC, VersionToken: versionToken,
		DownloadedAt: meta.AcquiredAtUTC, Scope: manifestScope{Type: scopeType, Value: scopeValue},
		QueryURL: queryInteractionsURL,
		Query: manifestQuery{Schema: fullEvidenceSchema, Fingerprint: fingerprint, License: query.License,
			Fields: query.Fields, OutputFields: query.OutputFields, Levels: query.Levels, SidecarPath: "raw/query_meta.json", SidecarSHA: metaRecord.SHA256},
		Files: records,
	}
	manifest.Query.AcquiredAt = meta.AcquiredAtUTC
	manifest.Query.Transport = meta.Transport
	manifest.Query.Organisms = query.Organisms
	manifest.Query.StartSHA, manifest.Query.StartEdges = meta.Start.SHA256, meta.Start.Edges
	manifest.Query.EndSHA, manifest.Query.EndEdges = meta.End.SHA256, meta.End.Edges
	manifest.Query.LeafBatches = manifestLeaves
	if err := writeManifest(filepath.Join(dirWork, "manifest.lock"), manifest); err != nil {
		return err
	}
	if err := os.Rename(dirWork, dirVersion); err != nil {
		return fmt.Errorf("publish snapshot: %w", err)
	}
	logf("done (files=%d, leaf_batches=%d)", len(records), len(leaves))
	return nil
}

func validateInteractionCapabilities(client *omnipathClient, query interactionQuery) error {
	data, err := client.download(queryInteractionsURL)
	if err != nil {
		return fmt.Errorf("read OmniPath interaction capabilities: %w", err)
	}
	table := map[string]map[string]struct{}{}
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		parts := strings.SplitN(line, "\t", 2)
		if len(parts) != 2 || parts[0] == "argument" {
			continue
		}
		values := map[string]struct{}{}
		for _, value := range strings.Split(parts[1], ";") {
			values[strings.TrimSpace(value)] = struct{}{}
		}
		table[parts[0]] = values
	}
	check := func(argument string, values []string) error {
		advertised, ok := table[argument]
		if !ok {
			return fmt.Errorf("capability table lacks argument %s", argument)
		}
		for _, value := range values {
			if _, ok := advertised[value]; !ok {
				return fmt.Errorf("capability table does not advertise %s=%s", argument, value)
			}
		}
		return nil
	}
	if err := check("datasets", []string{query.Dataset}); err != nil {
		return err
	}
	if err := check("license", []string{query.License}); err != nil {
		return err
	}
	if err := check("organisms", query.Organisms); err != nil {
		return err
	}
	if err := check("fields", query.Fields); err != nil {
		return err
	}
	if len(query.Levels) > 0 {
		if err := check("dorothea_levels", query.Levels); err != nil {
			return err
		}
	}
	return nil
}

func interactionURL(query interactionQuery, taxID string, fields, targets []string) string {
	params := url.Values{}
	params.Set("datasets", query.Dataset)
	params.Set("format", "tsv")
	params.Set("license", query.License)
	params.Set("organisms", taxID)
	if len(fields) > 0 {
		params.Set("fields", strings.Join(fields, ","))
		params.Set("genesymbols", "1")
		params.Set("evidences", "1")
		params.Set("extra_attrs", "1")
	}
	if len(query.Levels) > 0 {
		params.Set("dorothea_levels", strings.Join(query.Levels, ","))
	}
	if len(targets) > 0 {
		params.Set("targets", strings.Join(targets, ","))
	}
	return baseURL + "/interactions?" + params.Encode()
}

func fetchInventories(client *omnipathClient, query interactionQuery) (map[string]tsvTable, interactionInventory, error) {
	tables := make(map[string]tsvTable, len(query.Organisms))
	keys := make([]string, 0)
	for _, taxID := range query.Organisms {
		data, err := client.download(interactionURL(query, taxID, nil, nil))
		if err != nil {
			return nil, interactionInventory{}, fmt.Errorf("fetch inventory for organism %s: %w", taxID, err)
		}
		table, err := parseTSV(data, inventoryFields)
		if err != nil {
			return nil, interactionInventory{}, err
		}
		tables[taxID] = table
		for _, row := range table.rows {
			keys = append(keys, taxID+"\x1f"+edgeKey(table, row))
		}
	}
	sort.Strings(keys)
	sum := sha256.Sum256([]byte(strings.Join(keys, "\n")))
	return tables, interactionInventory{SHA256: fmt.Sprintf("%x", sum), Edges: len(keys)}, nil
}

func planInteractionBatches(query interactionQuery, taxID string, inventory tsvTable) ([][]string, error) {
	counts := inventoryTargetCounts(inventory)
	targets := make([]string, 0, len(counts))
	for target := range counts {
		targets = append(targets, target)
	}
	sort.Strings(targets)
	var batches [][]string
	var current []string
	edges := 0
	for _, target := range targets {
		if counts[target] > maxBatchEdges {
			return nil, fmt.Errorf("target %q has %d expected edges above batch limit %d", target, counts[target], maxBatchEdges)
		}
		candidate := append(append([]string(nil), current...), target)
		if len(current) > 0 && (edges+counts[target] > maxBatchEdges || len(candidate) > maxBatchTargets ||
			len(interactionURL(query, taxID, query.Fields, candidate)) > maxBatchEncodedURL) {
			batches = append(batches, current)
			current, edges = nil, 0
		}
		current = append(current, target)
		edges += counts[target]
		if len(interactionURL(query, taxID, query.Fields, current)) > maxBatchEncodedURL {
			return nil, fmt.Errorf("target %q produces encoded URL above %d bytes", target, maxBatchEncodedURL)
		}
	}
	if len(current) > 0 {
		batches = append(batches, current)
	}
	return batches, nil
}

func inventoryTargetCounts(inventory tsvTable) map[string]int {
	counts := map[string]int{}
	for _, row := range inventory.rows {
		counts[row[inventory.index["target"]]]++
	}
	return counts
}

func fetchInteractionBatch(client *omnipathClient, query interactionQuery, taxID string, targets []string, counts map[string]int, pathBase string) ([]stagedInteractionLeaf, error) {
	urlBatch := interactionURL(query, taxID, query.Fields, targets)
	data, status, err := client.downloadBatch(urlBatch)
	if err != nil {
		if (status == http.StatusBadGateway || status == http.StatusServiceUnavailable) && len(targets) > 1 {
			middle := len(targets) / 2
			leftLeaves, err := fetchInteractionBatch(client, query, taxID, targets[:middle], counts, pathBase+"-0")
			if err != nil {
				return nil, err
			}
			rightLeaves, err := fetchInteractionBatch(client, query, taxID, targets[middle:], counts, pathBase+"-1")
			return append(leftLeaves, rightLeaves...), err
		}
		return nil, fmt.Errorf("interaction batch organism=%s targets=%d: %w", taxID, len(targets), err)
	}
	table, err := parseTSV(data, fullEvidenceFields)
	if err != nil {
		return nil, err
	}
	expected := 0
	for _, target := range targets {
		expected += counts[target]
	}
	if len(table.rows) != expected {
		return nil, fmt.Errorf("leaf edge count mismatch: got %d want inventory %d", len(table.rows), expected)
	}
	if err := validateLeafRows(table, targets, query.Levels); err != nil {
		return nil, err
	}
	path := pathBase + ".part"
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return nil, err
	}
	return []stagedInteractionLeaf{{
		meta: interactionLeaf{Organism: taxID, Targets: append([]string(nil), targets...), ExpectedEdges: expected, URL: urlBatch},
		path: path,
	}}, nil
}

func validateLeafRows(table tsvTable, targets, levels []string) error {
	allowedTargets := map[string]struct{}{}
	for _, target := range targets {
		allowedTargets[target] = struct{}{}
	}
	allowedLevels := map[string]struct{}{}
	for _, level := range levels {
		allowedLevels[level] = struct{}{}
	}
	for _, row := range table.rows {
		if _, ok := allowedTargets[row[table.index["target"]]]; !ok {
			return fmt.Errorf("leaf returned target %q outside its partition", row[table.index["target"]])
		}
		for _, field := range []string{"evidences", "extra_attrs"} {
			if !json.Valid([]byte(row[table.index[field]])) {
				return fmt.Errorf("leaf %s is not JSON", field)
			}
		}
		if len(allowedLevels) > 0 {
			level := row[table.index["dorothea_level"]]
			if !dorotheaLevelCellAllowed(level, allowedLevels) {
				return fmt.Errorf("leaf DoRothEA level %q is outside locked levels", level)
			}
		}
	}
	return nil
}

func dorotheaLevelCellAllowed(value string, allowed map[string]struct{}) bool {
	if strings.TrimSpace(value) == "" {
		return false
	}
	for _, token := range strings.Split(value, ";") {
		if _, ok := allowed[strings.TrimSpace(token)]; !ok {
			return false
		}
	}
	return true
}

func parseTSV(data []byte, expected []string) (tsvTable, error) {
	scanner := bufio.NewScanner(bytes.NewReader(data))
	scanner.Buffer(make([]byte, 64*1024), 32*1024*1024)
	rows := make([][]string, 0)
	for scanner.Scan() {
		line := strings.TrimSuffix(scanner.Text(), "\r")
		rows = append(rows, strings.Split(line, "\t"))
	}
	if err := scanner.Err(); err != nil {
		return tsvTable{}, fmt.Errorf("parse TSV with 32MiB line limit: %w", err)
	}
	if len(rows) == 0 {
		return tsvTable{}, fmt.Errorf("TSV is empty")
	}
	if strings.Join(rows[0], "\x00") != strings.Join(expected, "\x00") {
		return tsvTable{}, fmt.Errorf("unexpected TSV header: got %q want %q", rows[0], expected)
	}
	index := make(map[string]int, len(expected))
	for i, name := range expected {
		index[name] = i
	}
	return tsvTable{header: rows[0], rows: rows[1:], index: index}, nil
}

func edgeKey(table tsvTable, row []string) string {
	parts := make([]string, len(inventoryFields))
	for i, field := range inventoryFields {
		parts[i] = row[table.index[field]]
	}
	return strings.Join(parts, "\x1f")
}

func assembleInteractionLeaves(path string, leaves []stagedInteractionLeaf, inventory tsvTable, levels []string) error {
	part := path + ".part"
	out, err := os.Create(part)
	if err != nil {
		return err
	}
	writer := bufio.NewWriter(out)
	if _, err := writer.WriteString(strings.Join(fullEvidenceFields, "\t") + "\n"); err != nil {
		_ = out.Close()
		return err
	}
	expected := map[string]struct{}{}
	for _, row := range inventory.rows {
		key := edgeKey(inventory, row)
		if _, duplicate := expected[key]; duplicate {
			_ = out.Close()
			return fmt.Errorf("duplicate inventory edge %q", key)
		}
		expected[key] = struct{}{}
	}
	actual := map[string]struct{}{}
	index := make(map[string]int, len(fullEvidenceFields))
	for i, field := range fullEvidenceFields {
		index[field] = i
	}
	for _, leaf := range leaves {
		data, err := os.ReadFile(leaf.path)
		if err != nil {
			_ = out.Close()
			return err
		}
		table, err := parseTSV(data, fullEvidenceFields)
		if err != nil {
			_ = out.Close()
			return err
		}
		if err := validateLeafRows(table, leaf.meta.Targets, levels); err != nil {
			_ = out.Close()
			return err
		}
		for _, row := range table.rows {
			key := edgeKey(tsvTable{index: index}, row)
			if _, duplicate := actual[key]; duplicate {
				_ = out.Close()
				return fmt.Errorf("duplicate full-evidence edge %q", key)
			}
			actual[key] = struct{}{}
			if _, err := writer.WriteString(strings.Join(row, "\t") + "\n"); err != nil {
				_ = out.Close()
				return err
			}
		}
	}
	errWriter := writer.Flush()
	errClose := out.Close()
	if errWriter != nil {
		return errWriter
	}
	if errClose != nil {
		return errClose
	}
	if len(actual) != len(expected) {
		return fmt.Errorf("edge count mismatch: inventory=%d full-evidence=%d", len(expected), len(actual))
	}
	for key := range expected {
		if _, ok := actual[key]; !ok {
			return fmt.Errorf("missing full-evidence edge %q", key)
		}
	}
	for key := range actual {
		if _, ok := expected[key]; !ok {
			return fmt.Errorf("extra full-evidence edge %q", key)
		}
	}
	return os.Rename(part, path)
}

func writeJSONAtomic(path string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	part := path + ".part"
	if err := os.WriteFile(part, data, 0o644); err != nil {
		return err
	}
	return os.Rename(part, path)
}

func (client *omnipathClient) downloadBatch(urlFile string) ([]byte, int, error) {
	var last error
	var status int
	for attempt := 1; attempt <= client.retryMax; attempt++ {
		response, err := client.clientHTTP.Get(urlFile)
		if err != nil {
			last = err
		} else {
			status = response.StatusCode
			data := new(bytes.Buffer)
			_, readErr := data.ReadFrom(response.Body)
			_ = response.Body.Close()
			if readErr == nil && status >= 200 && status < 300 {
				return data.Bytes(), status, nil
			}
			if readErr != nil {
				last = readErr
			} else {
				last = fmt.Errorf("unexpected status %s", response.Status)
			}
			if status != http.StatusTooManyRequests && status != http.StatusBadGateway && status != http.StatusServiceUnavailable {
				break
			}
			if attempt < client.retryMax {
				wait := client.retryWait
				if status == http.StatusTooManyRequests {
					if seconds, err := strconv.Atoi(strings.TrimSpace(response.Header.Get("Retry-After"))); err == nil && seconds >= 0 {
						wait = time.Duration(seconds) * time.Second
					} else if when, err := http.ParseTime(strings.TrimSpace(response.Header.Get("Retry-After"))); err == nil {
						wait = when.Sub(interactionNow())
						if wait < 0 {
							wait = 0
						}
					}
				}
				interactionSleep(wait)
				continue
			}
		}
		if attempt < client.retryMax {
			interactionSleep(client.retryWait)
		}
	}
	return nil, status, fmt.Errorf("request %s failed after %d attempts: %w", urlFile, client.retryMax, last)
}

func runRestoreFullEvidence(cfg *restoreConfig, dirVersion string, manifest manifestFile) error {
	client := createClient(cfg.shouldAllowInsecureTLS, cfg.retryMax, cfg.retryWait)
	if cfg.shouldDryRun {
		for _, leaf := range manifest.Query.LeafBatches {
			logf("[dry-run] restore leaf %s", leaf.URL)
		}
		return nil
	}
	if err := validateLockedQuery(manifest); err != nil {
		return err
	}
	meta := interactionQueryMeta{
		Schema: manifest.Query.Schema, AcquiredAtUTC: manifest.Query.AcquiredAt,
		Fingerprint: manifest.Query.Fingerprint,
		Query: interactionQuery{Schema: manifest.Query.Schema, Dataset: manifest.Dataset,
			License: manifest.Query.License, Organisms: manifest.Query.Organisms,
			Fields: manifest.Query.Fields, OutputFields: manifest.Query.OutputFields, Levels: manifest.Query.Levels},
		Transport: manifest.Query.Transport,
		Start:     interactionInventory{SHA256: manifest.Query.StartSHA, Edges: manifest.Query.StartEdges},
		End:       interactionInventory{SHA256: manifest.Query.EndSHA, Edges: manifest.Query.EndEdges},
	}
	meta.LeafBatches = make([]interactionLeaf, len(manifest.Query.LeafBatches))
	for i, leaf := range manifest.Query.LeafBatches {
		meta.LeafBatches[i] = interactionLeaf(leaf)
	}
	stage := filepath.Join(dirVersion, ".restore.part")
	if err := os.RemoveAll(stage); err != nil {
		return err
	}
	defer os.RemoveAll(stage)
	if err := os.MkdirAll(stage, 0o755); err != nil {
		return err
	}
	metaPath := filepath.Join(stage, filepath.FromSlash(manifest.Query.SidecarPath))
	if err := os.MkdirAll(filepath.Dir(metaPath), 0o755); err != nil {
		return err
	}
	if err := writeJSONAtomic(metaPath, meta); err != nil {
		return err
	}
	stagedByOrganism := map[string][]stagedInteractionLeaf{}
	seenTargets := map[string]map[string]struct{}{}
	for indexLeaf, leaf := range manifest.Query.LeafBatches {
		data, _, err := client.downloadBatch(leaf.URL)
		if err != nil {
			return err
		}
		table, err := parseTSV(data, manifest.Query.OutputFields)
		if err != nil {
			return err
		}
		if len(table.rows) != leaf.ExpectedEdges {
			return fmt.Errorf("restore leaf edge drift for organism %s: got %d want %d", leaf.Organism, len(table.rows), leaf.ExpectedEdges)
		}
		allowed := map[string]struct{}{}
		for _, target := range leaf.Targets {
			allowed[target] = struct{}{}
			if seenTargets[leaf.Organism] == nil {
				seenTargets[leaf.Organism] = map[string]struct{}{}
			}
			if _, duplicate := seenTargets[leaf.Organism][target]; duplicate {
				return fmt.Errorf("locked target %q appears in multiple leaf batches", target)
			}
			seenTargets[leaf.Organism][target] = struct{}{}
		}
		for _, row := range table.rows {
			if _, ok := allowed[row[table.index["target"]]]; !ok {
				return fmt.Errorf("leaf returned target outside locked partition")
			}
		}
		if err := validateLeafRows(table, leaf.Targets, manifest.Query.Levels); err != nil {
			return err
		}
		pathLeaf := filepath.Join(stage, fmt.Sprintf(".leaf-%06d.part", indexLeaf))
		if err := os.WriteFile(pathLeaf, data, 0o644); err != nil {
			return err
		}
		stagedByOrganism[leaf.Organism] = append(stagedByOrganism[leaf.Organism], stagedInteractionLeaf{
			meta: interactionLeaf(leaf), path: pathLeaf,
		})
	}
	for _, organism := range manifest.Query.Organisms {
		record, ok := findManifestRecord(manifest.Files, filepath.ToSlash(filepath.Join("raw", organism, "interactions.tsv")))
		if !ok {
			return fmt.Errorf("manifest lacks interactions record for organism %s", organism)
		}
		path := filepath.Join(stage, filepath.FromSlash(record.Path))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return err
		}
		if err := assembleRestoredLeaves(path, stagedByOrganism[organism], manifest.Query.OutputFields, manifest.Query.Levels); err != nil {
			return err
		}
	}
	for _, record := range manifest.Files {
		pathStage := filepath.Join(stage, filepath.FromSlash(record.Path))
		info, err := os.Stat(pathStage)
		if err != nil {
			return fmt.Errorf("staged restore lacks %s: %w", record.Path, err)
		}
		hash, err := calculateSHA256(pathStage)
		if err != nil {
			return err
		}
		if info.Size() != record.Bytes || hash != record.SHA256 {
			return fmt.Errorf("restored %s drift: bytes=%d/%d sha256=%s/%s; manifest and existing files unchanged",
				record.Path, info.Size(), record.Bytes, hash, record.SHA256)
		}
	}
	for _, record := range manifest.Files {
		pathStage := filepath.Join(stage, filepath.FromSlash(record.Path))
		pathFinal := filepath.Join(dirVersion, filepath.FromSlash(record.Path))
		if current, err := calculateSHA256(pathFinal); err == nil && current == record.SHA256 {
			continue
		}
		if err := os.MkdirAll(filepath.Dir(pathFinal), 0o755); err != nil {
			return err
		}
		if err := os.Rename(pathStage, pathFinal); err != nil {
			return fmt.Errorf("publish restored %s: %w", record.Path, err)
		}
	}
	return nil
}

func findManifestRecord(records []recordFile, path string) (recordFile, bool) {
	for _, record := range records {
		if record.Path == path {
			return record, true
		}
	}
	return recordFile{}, false
}

func validateLockedQuery(manifest manifestFile) error {
	query := manifest.Query
	if query.Schema != fullEvidenceSchema || query.Transport != interactionTransport ||
		len(query.Fingerprint) < 12 ||
		query.License != normalizeLicense(query.License) ||
		!slicesEqual(query.Fields, fullEvidenceQueryFields) ||
		!slicesEqual(query.OutputFields, fullEvidenceFields) ||
		query.StartSHA != query.EndSHA || query.StartEdges != query.EndEdges {
		return fmt.Errorf("locked full-evidence query is inconsistent")
	}
	if query.SidecarPath != "raw/query_meta.json" || recordSHA(manifest.Files, query.SidecarPath) != query.SidecarSHA ||
		!strings.HasSuffix(manifest.VersionToken, "-"+query.Fingerprint[:12]) {
		return fmt.Errorf("locked query does not match snapshot identity")
	}
	if (manifest.Dataset == "dorothea") != (len(query.Levels) > 0) {
		return fmt.Errorf("locked DoRothEA levels do not match dataset")
	}
	canonical, err := json.Marshal(interactionQuery{
		Schema: query.Schema, Dataset: manifest.Dataset, License: query.License,
		Organisms: query.Organisms, Fields: query.Fields, OutputFields: query.OutputFields, Levels: query.Levels,
	})
	if err != nil {
		return err
	}
	if fmt.Sprintf("%x", sha256.Sum256(canonical)) != query.Fingerprint {
		return fmt.Errorf("locked query fingerprint mismatch")
	}
	seen := map[string]map[string]struct{}{}
	edges := 0
	for _, leaf := range query.LeafBatches {
		if !containsString(query.Organisms, leaf.Organism) {
			return fmt.Errorf("locked leaf organism is outside query")
		}
		parsed, err := url.Parse(leaf.URL)
		if err != nil {
			return err
		}
		values := parsed.Query()
		if values.Get("datasets") != manifest.Dataset || values.Get("license") != query.License ||
			values.Get("organisms") != leaf.Organism || values.Get("fields") != strings.Join(query.Fields, ",") ||
			values.Get("targets") != strings.Join(leaf.Targets, ",") {
			return fmt.Errorf("locked leaf URL does not match query")
		}
		if seen[leaf.Organism] == nil {
			seen[leaf.Organism] = map[string]struct{}{}
		}
		for _, target := range leaf.Targets {
			if _, duplicate := seen[leaf.Organism][target]; duplicate {
				return fmt.Errorf("locked target appears in multiple leaves")
			}
			seen[leaf.Organism][target] = struct{}{}
		}
		edges += leaf.ExpectedEdges
	}
	if edges != query.StartEdges {
		return fmt.Errorf("locked leaf edge total does not match inventory")
	}
	return nil
}

func slicesEqual(left, right []string) bool {
	return strings.Join(left, "\x00") == strings.Join(right, "\x00")
}

func assembleRestoredLeaves(path string, leaves []stagedInteractionLeaf, header, levels []string) error {
	part := path + ".part"
	out, err := os.Create(part)
	if err != nil {
		return err
	}
	writer := bufio.NewWriter(out)
	if _, err := writer.WriteString(strings.Join(header, "\t") + "\n"); err != nil {
		_ = out.Close()
		return err
	}
	seen := map[string]struct{}{}
	for _, leaf := range leaves {
		data, err := os.ReadFile(leaf.path)
		if err != nil {
			_ = out.Close()
			return err
		}
		table, err := parseTSV(data, header)
		if err != nil {
			_ = out.Close()
			return err
		}
		if err := validateLeafRows(table, leaf.meta.Targets, levels); err != nil {
			_ = out.Close()
			return err
		}
		for _, row := range table.rows {
			key := edgeKey(table, row)
			if _, duplicate := seen[key]; duplicate {
				_ = out.Close()
				return fmt.Errorf("duplicate restored edge %q", key)
			}
			seen[key] = struct{}{}
			if _, err := writer.WriteString(strings.Join(row, "\t") + "\n"); err != nil {
				_ = out.Close()
				return err
			}
		}
	}
	errWriter, errClose := writer.Flush(), out.Close()
	if errWriter != nil {
		return errWriter
	}
	if errClose != nil {
		return errClose
	}
	return os.Rename(part, path)
}
