package omnipath

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/ulikunitz/xz"
)

type archiveSnapshot struct {
	version      string
	versionToken string
	urlFile      string
}

type archiveQueryMeta struct {
	Mode         string   `json:"mode"`
	Asset        string   `json:"asset"`
	Dataset      string   `json:"dataset,omitempty"`
	Version      string   `json:"version"`
	VersionToken string   `json:"version_token"`
	ArchiveURL   string   `json:"archive_url"`
	Organisms    []string `json:"organisms"`
	License      string   `json:"license,omitempty"`
}

type archiveMaterializeInput struct {
	asset        string
	dataset      string
	version      string
	versionToken string
	taxIDs       []string
	urlArchive   string
	dirVersion   string
	ruleLicense  string
	shouldDryRun bool
}

func validateOptionalVersionToken(versionToken string) error {
	value := strings.TrimSpace(versionToken)
	if value == "" {
		return nil
	}
	if _, err := time.Parse("2006-01-02", value); err != nil {
		return fmt.Errorf(
			"version must be an OmniPath archive date in YYYY-MM-DD, e.g. 2025-08-13; see %s",
			archiveURL,
		)
	}
	return nil
}

func resolveArchiveSnapshot(client *omnipathClient, asset string, versionToken string) (archiveSnapshot, error) {
	dataIndex, err := client.download(archiveURL)
	if err != nil {
		return archiveSnapshot{}, err
	}
	return extractArchiveSnapshotFromIndex(dataIndex, asset, versionToken)
}

func extractArchiveSnapshotFromIndex(data []byte, asset string, versionToken string) (archiveSnapshot, error) {
	matches, err := parseArchiveMatches(data, asset)
	if err != nil {
		return archiveSnapshot{}, err
	}
	if strings.TrimSpace(versionToken) == "" {
		match := matches[len(matches)-1]
		return archiveSnapshot{
			version:      match.version,
			versionToken: sanitizeVersionToken(match.version),
			urlFile:      archiveURL + match.fileName,
		}, nil
	}

	for _, match := range matches {
		if match.version == versionToken {
			return archiveSnapshot{
				version:      match.version,
				versionToken: sanitizeVersionToken(match.version),
				urlFile:      archiveURL + match.fileName,
			}, nil
		}
	}
	return archiveSnapshot{}, fmt.Errorf(
		"OmniPath archive version %q not found for asset %s; see %s",
		versionToken,
		asset,
		archiveURL,
	)
}

type archiveMatch struct {
	fileName string
	version  string
}

func parseArchiveMatches(data []byte, asset string) ([]archiveMatch, error) {
	text := string(data)
	pattern := fmt.Sprintf(`(omnipath_webservice_%s__([0-9]{8})-([0-9]{8})\.tsv\.(?:xz|gz))`, regexp.QuoteMeta(asset))
	re := regexp.MustCompile(pattern)
	matchesAll := re.FindAllStringSubmatch(text, -1)
	if len(matchesAll) == 0 {
		return nil, fmt.Errorf("no archive version found for asset %s", asset)
	}

	matches := make([]archiveMatch, 0, len(matchesAll))
	for _, match := range matchesAll {
		if len(match) < 4 {
			return nil, fmt.Errorf("archive version parse failed for asset %s", asset)
		}
		version, err := formatArchiveDate(match[3])
		if err != nil {
			return nil, err
		}
		matches = append(matches, archiveMatch{
			fileName: match[1],
			version:  version,
		})
	}
	return matches, nil
}

func materializeArchiveSnapshot(client *omnipathClient, in archiveMaterializeInput) ([]recordFile, error) {
	dataArchive, err := client.download(in.urlArchive)
	if err != nil {
		return nil, err
	}

	if in.shouldDryRun {
		logf("[dry-run] archive url: %s", in.urlArchive)
		return nil, nil
	}

	if strings.TrimSpace(in.ruleLicense) != "" && strings.ToLower(strings.TrimSpace(in.ruleLicense)) != "academic" {
		return nil, fmt.Errorf(
			"historical OmniPath archive fetch supports only default/academic license mode; see %s",
			archiveURL,
		)
	}

	fileQuery := filepath.Join(in.dirVersion, "raw", "query_meta.json")
	pathRelQuery := filepath.ToSlash(filepath.Join("raw", "query_meta.json"))
	if err := os.MkdirAll(filepath.Join(in.dirVersion, "raw"), 0o755); err != nil {
		return nil, fmt.Errorf("create raw dir: %w", err)
	}

	taxIDsMaterialized, err := writeArchiveDataFiles(dataArchive, in)
	if err != nil {
		return nil, err
	}

	if len(in.taxIDs) == 0 {
		in.taxIDs = taxIDsMaterialized
	}
	queryMeta := archiveQueryMeta{
		Mode:         "archive",
		Asset:        in.asset,
		Dataset:      in.dataset,
		Version:      in.version,
		VersionToken: in.versionToken,
		ArchiveURL:   in.urlArchive,
		Organisms:    append([]string(nil), in.taxIDs...),
		License:      strings.ToLower(strings.TrimSpace(in.ruleLicense)),
	}
	dataQueryMeta, err := json.MarshalIndent(queryMeta, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal archive query metadata: %w", err)
	}
	dataQueryMeta = append(dataQueryMeta, '\n')
	if err := os.WriteFile(fileQuery, dataQueryMeta, 0o644); err != nil {
		return nil, fmt.Errorf("write %s: %w", fileQuery, err)
	}

	records := make([]recordFile, 0, 1+len(in.taxIDs))
	recordQuery, err := buildRecord(fileQuery, pathRelQuery, in.urlArchive, "query_meta")
	if err != nil {
		return nil, err
	}
	records = append(records, recordQuery)

	for _, taxID := range in.taxIDs {
		fileData := filepath.Join(in.dirVersion, "raw", taxID, in.asset+".tsv")
		pathRelData := filepath.ToSlash(filepath.Join("raw", taxID, in.asset+".tsv"))
		recordData, err := buildRecord(fileData, pathRelData, in.urlArchive, in.asset)
		if err != nil {
			return nil, err
		}
		records = append(records, recordData)
	}

	sort.Slice(records, func(i, j int) bool {
		if records[i].Path != records[j].Path {
			return records[i].Path < records[j].Path
		}
		return records[i].Asset < records[j].Asset
	})
	return records, nil
}

func writeArchiveDataFiles(dataArchive []byte, in archiveMaterializeInput) ([]string, error) {
	readerArchive, err := openArchiveReader(dataArchive, in.urlArchive)
	if err != nil {
		return nil, err
	}
	defer readerArchive.Close()

	scanner := bufio.NewScanner(readerArchive)
	scanner.Buffer(make([]byte, 1024*1024), 16*1024*1024)
	if !scanner.Scan() {
		if err := scanner.Err(); err != nil {
			return nil, fmt.Errorf("read archive header: %w", err)
		}
		return nil, fmt.Errorf("empty archive: %s", in.urlArchive)
	}
	lineHeader := scanner.Text()
	headerFields := strings.Split(lineHeader, "\t")

	writers := make(map[string]*os.File)
	taxIDsSeen := make(map[string]struct{})
	writeLine := func(taxID string, line string) error {
		fileOut, ok := writers[taxID]
		if !ok {
			dirRaw := filepath.Join(in.dirVersion, "raw", taxID)
			if err := os.MkdirAll(dirRaw, 0o755); err != nil {
				return fmt.Errorf("create raw dir: %w", err)
			}
			filePath := filepath.Join(dirRaw, in.asset+".tsv")
			fileHandle, err := os.Create(filePath)
			if err != nil {
				return fmt.Errorf("create %s: %w", filePath, err)
			}
			if _, err := io.WriteString(fileHandle, lineHeader+"\n"); err != nil {
				fileHandle.Close()
				return fmt.Errorf("write header %s: %w", filePath, err)
			}
			writers[taxID] = fileHandle
			fileOut = fileHandle
		}
		if line == "" {
			taxIDsSeen[taxID] = struct{}{}
			return nil
		}
		if _, err := io.WriteString(fileOut, line+"\n"); err != nil {
			return fmt.Errorf("write archive line for taxid %s: %w", taxID, err)
		}
		taxIDsSeen[taxID] = struct{}{}
		return nil
	}

	var requested map[string]struct{}
	if len(in.taxIDs) > 0 {
		requested = make(map[string]struct{}, len(in.taxIDs))
		for _, taxID := range in.taxIDs {
			requested[taxID] = struct{}{}
		}
	}

	for scanner.Scan() {
		line := scanner.Text()
		matchedTaxIDs, err := matchArchiveTaxIDs(in.asset, in.dataset, headerFields, line)
		if err != nil {
			return nil, err
		}
		for _, taxID := range matchedTaxIDs {
			if requested != nil {
				if _, ok := requested[taxID]; !ok {
					continue
				}
			}
			if err := writeLine(taxID, line); err != nil {
				return nil, err
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan archive %s: %w", in.urlArchive, err)
	}

	if requested != nil {
		for _, taxID := range in.taxIDs {
			if _, ok := writers[taxID]; ok {
				continue
			}
			if err := writeLine(taxID, ""); err != nil {
				return nil, err
			}
		}
	}

	for taxID, fileOut := range writers {
		if err := fileOut.Close(); err != nil {
			return nil, fmt.Errorf("close archive output for taxid %s: %w", taxID, err)
		}
	}

	taxIDs := sortedKeys(taxIDsSeen)
	if len(in.taxIDs) > 0 {
		return append([]string(nil), in.taxIDs...), nil
	}
	return taxIDs, nil
}

func openArchiveReader(dataArchive []byte, urlArchive string) (io.ReadCloser, error) {
	readerData := bytes.NewReader(dataArchive)
	switch {
	case strings.HasSuffix(urlArchive, ".xz"):
		readerXZ, err := xz.NewReader(readerData)
		if err != nil {
			return nil, fmt.Errorf("open xz archive %s: %w", urlArchive, err)
		}
		return io.NopCloser(readerXZ), nil
	case strings.HasSuffix(urlArchive, ".gz"):
		readerGzip, err := gzip.NewReader(readerData)
		if err != nil {
			return nil, fmt.Errorf("open gzip archive %s: %w", urlArchive, err)
		}
		return readerGzip, nil
	default:
		return io.NopCloser(readerData), nil
	}
}

func matchArchiveTaxIDs(asset string, dataset string, headerFields []string, line string) ([]string, error) {
	fields := strings.Split(line, "\t")
	indexByName := make(map[string]int, len(headerFields))
	for index, name := range headerFields {
		indexByName[name] = index
	}

	switch asset {
	case "enz_sub":
		indexTaxID, ok := indexByName["ncbi_tax_id"]
		if !ok {
			return nil, fmt.Errorf("archive missing ncbi_tax_id column for %s", asset)
		}
		if indexTaxID >= len(fields) {
			return nil, nil
		}
		taxID := strings.TrimSpace(fields[indexTaxID])
		if !isArchiveTaxID(taxID) {
			return nil, nil
		}
		return []string{taxID}, nil
	case "interactions":
		indexDataset, ok := indexByName[dataset]
		if !ok {
			return nil, fmt.Errorf("archive missing dataset column %s", dataset)
		}
		if indexDataset >= len(fields) || fields[indexDataset] != "True" {
			return nil, nil
		}
		indexSource, okSource := indexByName["ncbi_tax_id_source"]
		indexTarget, okTarget := indexByName["ncbi_tax_id_target"]
		if !okSource || !okTarget {
			return nil, fmt.Errorf("archive missing interaction taxid columns")
		}
		if indexSource >= len(fields) || indexTarget >= len(fields) {
			return nil, nil
		}
		return deriveInteractionTaxIDs(fields[indexSource], fields[indexTarget]), nil
	default:
		return nil, fmt.Errorf("unsupported archive asset: %s", asset)
	}
}

func deriveInteractionTaxIDs(valueSource string, valueTarget string) []string {
	source := strings.TrimSpace(valueSource)
	target := strings.TrimSpace(valueTarget)
	switch {
	case source == "" && target == "":
		return nil
	case source == target && isArchiveTaxID(source):
		return []string{source}
	case source == "-1" && isArchiveTaxID(target):
		return []string{target}
	case target == "-1" && isArchiveTaxID(source):
		return []string{source}
	case isArchiveTaxID(source) && isArchiveTaxID(target):
		values := make(map[string]struct{}, 2)
		values[source] = struct{}{}
		values[target] = struct{}{}
		return sortedKeys(values)
	default:
		return nil
	}
}

func isArchiveTaxID(value string) bool {
	if strings.TrimSpace(value) == "" || strings.TrimSpace(value) == "-1" {
		return false
	}
	for _, char := range value {
		if char < '0' || char > '9' {
			return false
		}
	}
	return true
}

func sortedKeys(setValues map[string]struct{}) []string {
	values := make([]string, 0, len(setValues))
	for value := range setValues {
		if strings.TrimSpace(value) == "" {
			continue
		}
		values = append(values, value)
	}
	sort.Strings(values)
	return values
}

func deriveArchiveURLFromQueryMeta(fileQuery string) string {
	data, err := os.ReadFile(fileQuery)
	if err != nil {
		return ""
	}
	var meta archiveQueryMeta
	if err := json.Unmarshal(data, &meta); err != nil {
		return ""
	}
	return strings.TrimSpace(meta.ArchiveURL)
}
