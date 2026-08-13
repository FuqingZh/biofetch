package omnipath

import (
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/FuqingZh/biofetch/internal/shared/cliopt"
	"github.com/FuqingZh/biofetch/internal/shared/logx"
	"github.com/FuqingZh/biofetch/internal/shared/parallel"
	"github.com/FuqingZh/biofetch/internal/shared/tomlx"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type lockConfig struct {
	dirSnapshot  string
	dirLogs      string
	workersMax   int
	shouldDryRun bool
}

type restoreConfig struct {
	dirOut                  string
	dirLogs                 string
	versionToken            string
	dataset                 string
	ruleExisting            string
	shouldOverwriteExisting bool
	retryMax                int
	retryWait               time.Duration
	shouldAllowInsecureTLS  bool
	shouldDryRun            bool
}

func runLockEnzSub(cfg *lockConfig) error {
	return runLockCommon(cfg, "enz_sub", "")
}

func runLockInteractions(cfg *lockConfig) error {
	dataset := filepath.Base(filepath.Dir(filepath.Clean(cfg.dirSnapshot)))
	return runLockCommon(cfg, "interactions", dataset)
}

func runLockCommon(cfg *lockConfig, asset string, dataset string) error {
	if err := cliopt.NormalizeLockWorkersMax(&cfg.workersMax); err != nil {
		return err
	}
	versionToken, err := cliopt.SnapshotVersionToken(cfg.dirSnapshot)
	if err != nil {
		return err
	}
	dirVersion := cfg.dirSnapshot
	fileManifest := filepath.Join(dirVersion, "manifest.lock")
	logDir := cfg.dirLogs
	if logDir == "" && asset == "interactions" {
		logDir = fullEvidenceLogDir(dirVersion, dataset)
	}
	_, closeRun, err := logx.StartVersionedRun("biofetch omnipath", "lock", logDir, dirVersion)
	if err != nil {
		return err
	}
	defer closeRun()
	manifestExisting, _ := readExistingManifest(fileManifest)
	records, err := scanOmniPathRecords(dirVersion, asset, dataset, manifestExisting, cfg.workersMax)
	if err != nil {
		return err
	}

	manifest := manifestFile{
		Database:     "omnipath",
		Asset:        asset,
		Dataset:      dataset,
		Version:      versionToken,
		VersionToken: versionToken,
		DownloadedAt: time.Now().Format(time.RFC3339),
		Scope: func() manifestScope {
			scopeType, scopeValue := deriveOmniPathManifestScope(records)
			return manifestScope{Type: scopeType, Value: scopeValue}
		}(),
		RequestURL: deriveOmniPathRequestURL(records),
		QueryURL:   deriveOmniPathQueryURL(records, ""),
		Files:      records,
	}
	if asset == "interactions" {
		query, err := lockInteractionQuery(dirVersion, filepath.Join(dirVersion, "raw", "query_meta.json"), records, dataset, versionToken)
		if err != nil {
			return err
		}
		manifest.Query = query
		if query.Schema == fullEvidenceSchema {
			manifest.Version = query.AcquiredAt
			manifest.DownloadedAt = query.AcquiredAt
			manifest.RequestURL = ""
			manifest.QueryURL = queryInteractionsURL
			if err := validateFullEvidenceLayout(dirVersion, query.Organisms, false); err != nil {
				return err
			}
			if err := validateLockedQuery(manifest); err != nil {
				return err
			}
		}
	}

	if cfg.shouldDryRun {
		logf("[dry-run] lock version dir: %s", dirVersion)
		logf("[dry-run] manifest: %s", fileManifest)
		logf("[dry-run] files=%d", len(records))
		return nil
	}
	return writeManifest(fileManifest, manifest)
}

func runRestoreEnzSub(cfg *restoreConfig) error {
	dirVersion := filepath.Join(cfg.dirOut, "enz_sub", cfg.versionToken)
	return runRestoreCommon(cfg, dirVersion, "enz_sub")
}

func runRestoreInteractions(cfg *restoreConfig) error {
	dirVersion := filepath.Join(cfg.dirOut, "interactions", cfg.dataset, cfg.versionToken)
	return runRestoreCommon(cfg, dirVersion, "interactions")
}

func runRestoreCommon(cfg *restoreConfig, dirVersion string, asset string) error {
	fileManifest := filepath.Join(dirVersion, "manifest.lock")
	manifestExisting, err := readExistingManifest(fileManifest)
	if err != nil {
		return err
	}
	if len(manifestExisting.Files) == 0 {
		return fmt.Errorf("manifest is empty or missing: %s", fileManifest)
	}
	if err := validateRestoreRecordPaths(dirVersion, manifestExisting.Files); err != nil {
		return err
	}
	logDir := cfg.dirLogs
	if logDir == "" && asset == "interactions" && manifestExisting.Query.Schema == fullEvidenceSchema {
		logDir = fullEvidenceLogDir(dirVersion, manifestExisting.Dataset)
	}
	_, closeRun, err := logx.StartVersionedRun("biofetch omnipath", "restore", logDir, dirVersion)
	if err != nil {
		return err
	}
	defer closeRun()
	if strings.HasPrefix(strings.TrimSpace(manifestExisting.QueryURL), archiveURL) {
		return runRestoreArchive(cfg, dirVersion, manifestExisting, asset)
	}
	if asset == "interactions" && manifestExisting.Query.Schema == fullEvidenceSchema {
		return runRestoreFullEvidence(cfg, dirVersion, manifestExisting)
	}

	client := createClient(cfg.shouldAllowInsecureTLS, cfg.retryMax, cfg.retryWait)
	recordsCurrent := make([]recordFile, 0, len(manifestExisting.Files))
	for _, record := range manifestExisting.Files {
		filePath := filepath.Join(dirVersion, filepath.FromSlash(record.Path))
		if cfg.shouldDryRun {
			logf("[dry-run] restore %s -> %s", record.URL, filePath)
			continue
		}

		if err := os.MkdirAll(filepath.Dir(filePath), 0o755); err != nil {
			return fmt.Errorf("create restore dir: %w", err)
		}

		shouldDownload := cfg.shouldOverwriteExisting
		if !shouldDownload {
			recordCurrent, ok, err := inspectExisting(filePath, record.Path, record.URL, record.Asset)
			if err != nil {
				return err
			}
			if ok && recordCurrent.SHA256 == record.SHA256 {
				recordsCurrent = append(recordsCurrent, recordCurrent)
				continue
			}
			shouldDownload = true
		}

		if shouldDownload {
			logf("restore downloading %s", filepath.Base(filePath))
			data, err := client.download(record.URL)
			if err != nil {
				return err
			}
			if err := os.WriteFile(filePath, data, 0o644); err != nil {
				return fmt.Errorf("write %s: %w", filePath, err)
			}
		}

		recordCurrent, err := buildRecord(filePath, record.Path, record.URL, record.Asset)
		if err != nil {
			return err
		}
		recordsCurrent = append(recordsCurrent, recordCurrent)
	}

	if cfg.shouldDryRun {
		logf("[dry-run] restore done (files=%d)", len(manifestExisting.Files))
		return nil
	}

	recordsComplete, err := buildCompleteOmniPathRecords(fileManifest, dirVersion, recordsCurrent)
	if err != nil {
		return err
	}
	manifestExisting.DownloadedAt = time.Now().Format(time.RFC3339)
	manifestExisting.Scope = func() manifestScope {
		scopeType, scopeValue := deriveOmniPathManifestScope(recordsComplete)
		return manifestScope{Type: scopeType, Value: scopeValue}
	}()
	manifestExisting.RequestURL = deriveOmniPathRequestURL(recordsComplete)
	manifestExisting.QueryURL = deriveOmniPathQueryURL(recordsComplete, manifestExisting.QueryURL)
	manifestExisting.Files = recordsComplete
	if err := writeManifest(fileManifest, manifestExisting); err != nil {
		return err
	}

	logf("restore done (files=%d)", len(recordsComplete))
	logf("manifest written: %s", fileManifest)
	return nil
}

func validateRestoreRecordPaths(dirVersion string, records []recordFile) error {
	root, err := filepath.Abs(filepath.Clean(dirVersion))
	if err != nil {
		return fmt.Errorf("resolve snapshot root: %w", err)
	}
	seen := map[string]struct{}{}
	for _, record := range records {
		pathRaw := strings.TrimSpace(record.Path)
		pathClean := filepath.ToSlash(filepath.Clean(filepath.FromSlash(pathRaw)))
		if pathRaw == "" || pathRaw != record.Path || pathClean != pathRaw ||
			filepath.IsAbs(filepath.FromSlash(pathRaw)) ||
			!strings.HasPrefix(pathClean, "raw/") {
			return fmt.Errorf("unsafe OmniPath manifest record path %q", record.Path)
		}
		parts := strings.Split(pathClean, "/")
		for _, part := range parts {
			if part == "" || part == "." || part == ".." {
				return fmt.Errorf("unsafe OmniPath manifest record path %q", record.Path)
			}
		}
		if _, duplicate := seen[pathClean]; duplicate {
			return fmt.Errorf("duplicate OmniPath manifest record path %q", record.Path)
		}
		seen[pathClean] = struct{}{}
		candidate := filepath.Join(root, filepath.FromSlash(pathClean))
		relative, err := filepath.Rel(root, candidate)
		if err != nil || relative == "." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) || relative == ".." {
			return fmt.Errorf("OmniPath manifest record path escapes snapshot: %q", record.Path)
		}
		current := root
		for index, part := range parts {
			current = filepath.Join(current, part)
			info, err := os.Lstat(current)
			if os.IsNotExist(err) {
				break
			}
			if err != nil {
				return fmt.Errorf("inspect OmniPath restore path %s: %w", current, err)
			}
			if info.Mode()&os.ModeSymlink != 0 {
				return fmt.Errorf("OmniPath restore path must not traverse a symlink: %s", current)
			}
			if index < len(parts)-1 && !info.IsDir() {
				return fmt.Errorf("OmniPath restore parent is not a directory: %s", current)
			}
			if index == len(parts)-1 && !info.Mode().IsRegular() {
				return fmt.Errorf("OmniPath restore destination is not a regular file: %s", current)
			}
		}
	}
	return nil
}

func fullEvidenceLogDir(dirVersion, dataset string) string {
	dirInteractions := filepath.Dir(filepath.Dir(filepath.Clean(dirVersion)))
	dirRoot := filepath.Dir(dirInteractions)
	return filepath.Join(dirRoot, "logs", "omnipath", "interactions", dataset)
}

func lockInteractionQuery(dirVersion, path string, records []recordFile, dataset, versionToken string) (manifestQuery, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return manifestQuery{}, fmt.Errorf("read query sidecar: %w", err)
	}
	var meta interactionQueryMeta
	if !json.Valid(data) {
		if strings.HasPrefix(strings.TrimSpace(string(data)), "{") || strings.HasPrefix(strings.TrimSpace(string(data)), "[") {
			return manifestQuery{}, fmt.Errorf("query sidecar contains malformed JSON")
		}
		table, err := parseTSV(data, []string{"argument", "values"})
		if err != nil || len(table.rows) == 0 {
			if err == nil {
				err = fmt.Errorf("capability TSV has no rows")
			}
			return manifestQuery{}, fmt.Errorf("legacy query sidecar is not the expected capability TSV: %w", err)
		}
		for _, row := range table.rows {
			if strings.TrimSpace(row[0]) == "" {
				return manifestQuery{}, fmt.Errorf("legacy query sidecar contains an empty capability argument")
			}
		}
		return manifestQuery{Schema: "legacy-basic", SidecarPath: "raw/query_meta.json",
			SidecarSHA: recordSHA(records, "raw/query_meta.json")}, nil
	}
	var envelope struct {
		Schema string `json:"schema"`
		Mode   string `json:"mode"`
	}
	if err := json.Unmarshal(data, &envelope); err != nil {
		return manifestQuery{}, err
	}
	if envelope.Schema != fullEvidenceSchema {
		if envelope.Mode == "archive" {
			return manifestQuery{Schema: "legacy-basic", SidecarPath: "raw/query_meta.json",
				SidecarSHA: recordSHA(records, "raw/query_meta.json")}, nil
		}
		return manifestQuery{}, fmt.Errorf("unsupported JSON query sidecar schema %q", envelope.Schema)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&meta); err != nil {
		return manifestQuery{}, fmt.Errorf("decode full-evidence query sidecar: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return manifestQuery{}, fmt.Errorf("query sidecar contains trailing JSON values")
	}
	canonical, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return manifestQuery{}, err
	}
	canonical = append(canonical, '\n')
	if !bytes.Equal(data, canonical) {
		return manifestQuery{}, fmt.Errorf("full-evidence query sidecar is not canonical deterministic JSON")
	}
	if err := validateInteractionQueryMeta(meta, dataset, versionToken); err != nil {
		return manifestQuery{}, err
	}
	if !slicesEqual(meta.Query.Organisms, deriveOmniPathTaxIDsFromRecords(records)) {
		return manifestQuery{}, fmt.Errorf("query sidecar organisms do not match snapshot files")
	}
	if recordSHA(records, "raw/query_meta.json") == "" {
		return manifestQuery{}, fmt.Errorf("query sidecar is missing from snapshot records")
	}
	if err := validateFullEvidenceSnapshot(dirVersion, meta, records); err != nil {
		return manifestQuery{}, err
	}
	leaves := make([]manifestLeaf, len(meta.LeafBatches))
	for i, leaf := range meta.LeafBatches {
		leaves[i] = manifestLeaf(leaf)
	}
	return manifestQuery{
		Schema: meta.Schema, Fingerprint: meta.Fingerprint, License: meta.Query.License,
		Fields: meta.Query.Fields, OutputFields: meta.Query.OutputFields, Levels: meta.Query.Levels, SidecarPath: "raw/query_meta.json",
		SidecarSHA: recordSHA(records, "raw/query_meta.json"), AcquiredAt: meta.AcquiredAtUTC,
		Transport: meta.Transport, Organisms: meta.Query.Organisms,
		StartSHA: meta.Start.SHA256, StartEdges: meta.Start.Edges,
		EndSHA: meta.End.SHA256, EndEdges: meta.End.Edges, LeafBatches: leaves,
	}, nil
}

func recordSHA(records []recordFile, path string) string {
	for _, record := range records {
		if record.Path == path {
			return record.SHA256
		}
	}
	return ""
}

func runRestoreArchive(cfg *restoreConfig, dirVersion string, manifestExisting manifestFile, asset string) error {
	fileManifest := filepath.Join(dirVersion, "manifest.lock")
	recordsCurrent := make([]recordFile, 0, len(manifestExisting.Files))
	shouldDownload := cfg.shouldOverwriteExisting

	if !shouldDownload {
		allMatch := true
		for _, record := range manifestExisting.Files {
			filePath := filepath.Join(dirVersion, filepath.FromSlash(record.Path))
			recordCurrent, ok, err := inspectExisting(filePath, record.Path, record.URL, record.Asset)
			if err != nil {
				return err
			}
			if !ok || recordCurrent.SHA256 != record.SHA256 {
				allMatch = false
				break
			}
			recordsCurrent = append(recordsCurrent, recordCurrent)
		}
		if allMatch {
			recordsComplete, err := buildCompleteOmniPathRecords(fileManifest, dirVersion, recordsCurrent)
			if err != nil {
				return err
			}
			manifestExisting.DownloadedAt = time.Now().Format(time.RFC3339)
			manifestExisting.Files = recordsComplete
			if err := writeManifest(fileManifest, manifestExisting); err != nil {
				return err
			}
			logf("restore done (files=%d)", len(recordsComplete))
			logf("manifest written: %s", fileManifest)
			return nil
		}
	}

	if cfg.shouldDryRun {
		logf("[dry-run] restore archive %s -> %s", manifestExisting.QueryURL, dirVersion)
		logf("[dry-run] restore done (files=%d)", len(manifestExisting.Files))
		return nil
	}

	taxIDs := deriveOmniPathTaxIDsFromRecords(manifestExisting.Files)
	client := createClient(cfg.shouldAllowInsecureTLS, cfg.retryMax, cfg.retryWait)
	recordsArchive, err := materializeArchiveSnapshot(client, archiveMaterializeInput{
		asset:        asset,
		dataset:      manifestExisting.Dataset,
		version:      firstNonEmpty(manifestExisting.Version, manifestExisting.VersionToken),
		versionToken: manifestExisting.VersionToken,
		taxIDs:       taxIDs,
		urlArchive:   manifestExisting.QueryURL,
		dirVersion:   dirVersion,
	})
	if err != nil {
		return err
	}

	recordsComplete, err := buildCompleteOmniPathRecords(fileManifest, dirVersion, recordsArchive)
	if err != nil {
		return err
	}
	manifestExisting.DownloadedAt = time.Now().Format(time.RFC3339)
	manifestExisting.Scope = func() manifestScope {
		scopeType, scopeValue := deriveOmniPathManifestScope(recordsComplete)
		return manifestScope{Type: scopeType, Value: scopeValue}
	}()
	manifestExisting.RequestURL = firstNonEmpty(manifestExisting.RequestURL, manifestExisting.QueryURL)
	manifestExisting.QueryURL = firstNonEmpty(manifestExisting.QueryURL, manifestExisting.RequestURL)
	manifestExisting.Files = recordsComplete
	if err := writeManifest(fileManifest, manifestExisting); err != nil {
		return err
	}

	logf("restore done (files=%d)", len(recordsComplete))
	logf("manifest written: %s", fileManifest)
	return nil
}

func scanOmniPathRecords(dirVersion string, asset string, dataset string, manifestExisting manifestFile, workersMax int) ([]recordFile, error) {
	type taskRecordFile struct {
		filePath string
		pathRel  string
		urlFile  string
		asset    string
	}

	dirRaw := filepath.Join(dirVersion, "raw")
	fullEvidenceSidecar := false
	if asset == "interactions" {
		var meta interactionQueryMeta
		if data, err := os.ReadFile(filepath.Join(dirRaw, "query_meta.json")); err == nil &&
			json.Unmarshal(data, &meta) == nil && meta.Schema == fullEvidenceSchema {
			fullEvidenceSidecar = true
		}
	}
	urlsExisting := buildOmniPathExistingURLMap(manifestExisting)
	urlArchive := firstNonEmpty(
		strings.TrimSpace(manifestExisting.QueryURL),
		strings.TrimSpace(manifestExisting.RequestURL),
		deriveArchiveURLFromQueryMeta(filepath.Join(dirRaw, "query_meta.json")),
	)
	entries, err := os.ReadDir(dirRaw)
	if err != nil {
		return nil, fmt.Errorf("read raw dir: %w", err)
	}

	tasks := make([]taskRecordFile, 0)
	for _, entry := range entries {
		if entry.IsDir() {
			taxID := entry.Name()
			dirTaxID := filepath.Join(dirRaw, taxID)
			entriesFiles, err := os.ReadDir(dirTaxID)
			if err != nil {
				return nil, fmt.Errorf("read taxid dir %s: %w", dirTaxID, err)
			}
			for _, entryFile := range entriesFiles {
				if entryFile.IsDir() {
					continue
				}
				fileName := entryFile.Name()
				if fileName != asset+".tsv" {
					return nil, fmt.Errorf("unexpected OmniPath raw file: %s", filepath.Join(taxID, fileName))
				}
				filePath := filepath.Join(dirTaxID, fileName)
				pathRel := filepath.ToSlash(filepath.Join("raw", taxID, fileName))
				urlFile := ""
				if !fullEvidenceSidecar {
					urlFile = urlsExisting[pathRel]
				}
				if urlFile == "" && !fullEvidenceSidecar {
					if strings.HasPrefix(urlArchive, archiveURL) {
						urlFile = urlArchive
					} else {
						urlFile = deriveOmniPathDataURL(asset, dataset, taxID)
					}
				}
				tasks = append(tasks, taskRecordFile{
					filePath: filePath,
					pathRel:  pathRel,
					urlFile:  urlFile,
					asset:    asset,
				})
			}
			continue
		}

		if entry.Name() != "query_meta.json" {
			return nil, fmt.Errorf("unexpected OmniPath raw file: %s", entry.Name())
		}
		filePath := filepath.Join(dirRaw, entry.Name())
		pathRel := filepath.ToSlash(filepath.Join("raw", entry.Name()))
		urlQuery := ""
		if !fullEvidenceSidecar {
			urlQuery = urlsExisting[pathRel]
		}
		if urlQuery == "" && !fullEvidenceSidecar {
			if strings.HasPrefix(urlArchive, archiveURL) {
				urlQuery = urlArchive
			} else {
				urlQuery = queryEnzSubURL
				if asset == "interactions" {
					urlQuery = queryInteractionsURL
				}
			}
		}
		tasks = append(tasks, taskRecordFile{
			filePath: filePath,
			pathRel:  pathRel,
			urlFile:  urlQuery,
			asset:    "query_meta",
		})
	}

	records, err := parallel.MapOrderedWithWorkers(tasks, workersMax, func(task taskRecordFile) (recordFile, error) {
		return buildRecord(task.filePath, task.pathRel, task.urlFile, task.asset)
	})
	if err != nil {
		return nil, err
	}

	sort.Slice(records, func(i, j int) bool {
		if records[i].Path != records[j].Path {
			return records[i].Path < records[j].Path
		}
		return records[i].Asset < records[j].Asset
	})
	return records, nil
}

func deriveOmniPathDataURL(asset string, dataset string, taxID string) string {
	params := urlForTaxID(asset, dataset, taxID)
	if asset == "interactions" {
		return baseURL + "/interactions?" + params
	}
	return baseURL + "/enzsub?" + params
}

func urlForTaxID(asset string, dataset string, taxID string) string {
	if asset == "interactions" {
		return "datasets=" + dataset + "&format=tsv&organisms=" + taxID
	}
	return "format=tsv&organisms=" + taxID
}

func readExistingManifest(fileManifest string) (manifestFile, error) {
	var manifest manifestFile
	ok, err := tomlx.ReadFileIfExists(fileManifest, &manifest)
	if err != nil {
		return manifestFile{}, err
	}
	if !ok {
		return manifestFile{}, nil
	}
	return manifest, nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func buildOmniPathExistingURLMap(manifestExisting manifestFile) map[string]string {
	urls := make(map[string]string, len(manifestExisting.Files))
	for _, record := range manifestExisting.Files {
		if strings.TrimSpace(record.Path) == "" || strings.TrimSpace(record.URL) == "" {
			continue
		}
		urls[record.Path] = record.URL
	}
	return urls
}

func deriveOmniPathTaxIDsFromRecords(records []recordFile) []string {
	setTaxIDs := make(map[string]struct{})
	for _, record := range records {
		taxID := deriveOmniPathTaxIDFromPath(record.Path)
		if taxID == "" {
			continue
		}
		setTaxIDs[taxID] = struct{}{}
	}
	return sortedKeys(setTaxIDs)
}
