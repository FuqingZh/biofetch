package subcell

import (
	"biofetch/internal/shared/cliopt"
	"biofetch/internal/shared/staticasset"
	"bytes"
	"encoding/csv"
	"fmt"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"
)

var uniprotStreamBaseURL = "https://rest.uniprot.org/uniprotkb/stream"

var speciesTaxIDs = map[string]string{
	"hsa": "9606",
	"mmu": "10090",
	"rno": "10116",
	"ath": "3702",
	"sce": "559292",
}

type uniprotConfig struct {
	cliopt.DirOutConfig
	cliopt.VersionConfig
	cliopt.ExistingRuleConfig
	cliopt.RetryConfig
	cliopt.DownloadControlConfig
	cliopt.InsecureTLSConfig
	cliopt.DryRunConfig
	speciesCode string
	taxID       string
	proteomeID  string
}

type uniprotLockConfig struct {
	cliopt.DirOutConfig
	cliopt.VersionConfig
	cliopt.DryRunConfig
}

type uniprotSyncConfig struct {
	cliopt.DirOutConfig
	cliopt.VersionConfig
	cliopt.ExistingRuleConfig
	cliopt.RetryConfig
	cliopt.DownloadControlConfig
	cliopt.InsecureTLSConfig
	cliopt.DryRunConfig
}

type uniprotScope struct {
	scopeType  string
	scopeValue string
	query      string
}

func runFetchUniProt(cfg *uniprotConfig) error {
	scope, err := resolveUniProtScope(cfg.speciesCode, cfg.taxID, cfg.proteomeID)
	if err != nil {
		return err
	}
	versionToken := strings.TrimSpace(cfg.VersionToken)
	if versionToken == "" {
		versionToken = "current"
	}
	source := buildUniProtSource(versionToken, scope)
	return staticasset.Fetch(source, buildUniProtOptions(cfg.DirOut, cfg.ExistingRuleConfig, cfg.RetryConfig, cfg.DownloadControlConfig, cfg.InsecureTLSConfig, cfg.DryRunConfig), nil)
}

func runLockUniProt(cfg *uniprotLockConfig) error {
	return staticasset.Lock(staticasset.Source{
		Database:     "subcell",
		Asset:        "protein_location",
		Source:       "uniprot",
		DirName:      "uniprot",
		Version:      cfg.VersionToken,
		VersionToken: cfg.VersionToken,
	}, staticasset.Options{
		DirOut:       cfg.DirOut,
		RuleExisting: "skip",
		RetryMax:     1,
		WorkersMax:   1,
		ShouldDryRun: cfg.ShouldDryRun,
	}, nil)
}

func runSyncUniProt(cfg *uniprotSyncConfig) error {
	return staticasset.Sync(buildUniProtSource(cfg.VersionToken, uniprotScope{}), buildUniProtOptions(cfg.DirOut, cfg.ExistingRuleConfig, cfg.RetryConfig, cfg.DownloadControlConfig, cfg.InsecureTLSConfig, cfg.DryRunConfig), nil)
}

func buildUniProtSource(versionToken string, scope uniprotScope) staticasset.Source {
	urlAsset := ""
	if scope.query != "" {
		urlAsset = buildUniProtStreamURL(scope.query)
	}
	return staticasset.Source{
		Database:     "subcell",
		Asset:        "protein_location",
		Source:       "uniprot",
		DirName:      "uniprot",
		Version:      versionToken,
		VersionToken: versionToken,
		Scope: staticasset.Scope{
			Type:  scope.scopeType,
			Value: scope.scopeValue,
		},
		Assets: []staticasset.Asset{{
			Name: "protein_location",
			Path: "tidy/protein_location.tsv",
			URL:  urlAsset,
			Transform: func(filePath string) error {
				return normalizeUniProtSubcellFile(filePath, versionToken)
			},
		}},
	}
}

func resolveUniProtScope(speciesCode string, taxID string, proteomeID string) (uniprotScope, error) {
	valuesSet := 0
	if strings.TrimSpace(speciesCode) != "" {
		valuesSet++
	}
	if strings.TrimSpace(taxID) != "" {
		valuesSet++
	}
	if strings.TrimSpace(proteomeID) != "" {
		valuesSet++
	}
	if valuesSet != 1 {
		return uniprotScope{}, fmt.Errorf("exactly one of species, taxids, or proteome is required")
	}
	if speciesCode = strings.TrimSpace(speciesCode); speciesCode != "" {
		taxIDMapped, ok := speciesTaxIDs[strings.ToLower(speciesCode)]
		if !ok {
			return uniprotScope{}, fmt.Errorf("unsupported species shortcut %q", speciesCode)
		}
		return buildTaxIDScope("species", speciesCode, taxIDMapped), nil
	}
	if taxID = strings.TrimSpace(taxID); taxID != "" {
		if !isDigits(taxID) {
			return uniprotScope{}, fmt.Errorf("taxids must be numeric: %s", taxID)
		}
		return buildTaxIDScope("taxid", taxID, taxID), nil
	}
	proteomeID = strings.TrimSpace(proteomeID)
	if !strings.HasPrefix(strings.ToUpper(proteomeID), "UP") {
		return uniprotScope{}, fmt.Errorf("proteome must look like a UniProt proteome ID: %s", proteomeID)
	}
	query := fmt.Sprintf("(proteome:%s) AND (cc_subcellular_location:*)", proteomeID)
	return uniprotScope{scopeType: "proteome", scopeValue: proteomeID, query: query}, nil
}

func buildTaxIDScope(scopeType string, scopeValue string, taxID string) uniprotScope {
	query := fmt.Sprintf("(organism_id:%s) AND (cc_subcellular_location:*)", taxID)
	return uniprotScope{scopeType: scopeType, scopeValue: scopeValue, query: query}
}

func buildUniProtStreamURL(query string) string {
	params := url.Values{}
	params.Set("compressed", "false")
	params.Set("fields", "accession,cc_subcellular_location")
	params.Set("format", "tsv")
	params.Set("query", query)
	return uniprotStreamBaseURL + "?" + params.Encode()
}

func normalizeUniProtSubcellFile(filePath string, sourceVersion string) error {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return err
	}
	records, err := parseUniProtProteinLocations(data, "uniprot", sourceVersion)
	if err != nil {
		return err
	}
	return writeProteinLocationTSV(filePath, records)
}

type proteinLocationRecord struct {
	proteinID     string
	location      string
	source        string
	sourceVersion string
}

func parseUniProtProteinLocations(data []byte, source string, sourceVersion string) ([]proteinLocationRecord, error) {
	reader := csv.NewReader(bytes.NewReader(data))
	reader.Comma = '\t'
	reader.FieldsPerRecord = -1
	rows, err := reader.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("read UniProt TSV: %w", err)
	}
	if len(rows) == 0 {
		return nil, fmt.Errorf("UniProt TSV is empty")
	}
	indexAccession := -1
	indexLocation := -1
	for index, name := range rows[0] {
		switch strings.TrimSpace(name) {
		case "Entry", "Entry Name", "accession", "Accession":
			if indexAccession < 0 {
				indexAccession = index
			}
		case "Subcellular location [CC]", "cc_subcellular_location":
			indexLocation = index
		}
	}
	if indexAccession < 0 || indexLocation < 0 {
		return nil, fmt.Errorf("UniProt TSV must include accession and subcellular location columns")
	}
	records := make([]proteinLocationRecord, 0)
	for _, row := range rows[1:] {
		if indexAccession >= len(row) || indexLocation >= len(row) {
			continue
		}
		proteinID := strings.TrimSpace(row[indexAccession])
		for _, location := range extractUniProtLocations(row[indexLocation]) {
			records = append(records, proteinLocationRecord{
				proteinID:     proteinID,
				location:      location,
				source:        source,
				sourceVersion: sourceVersion,
			})
		}
	}
	return records, nil
}

var patternSubcellPrefix = regexp.MustCompile(`(?i)^SUBCELLULAR LOCATION:\s*`)
var patternEvidence = regexp.MustCompile(`\s*\{[^}]+\}`)

func extractUniProtLocations(value string) []string {
	value = patternSubcellPrefix.ReplaceAllString(strings.TrimSpace(value), "")
	value = patternEvidence.ReplaceAllString(value, "")
	parts := strings.Split(value, ";")
	locations := make([]string, 0, len(parts))
	seen := map[string]struct{}{}
	for _, part := range parts {
		location := strings.TrimSpace(part)
		if indexNote := strings.Index(strings.ToLower(location), "note="); indexNote >= 0 {
			location = strings.TrimSpace(location[:indexNote])
		}
		if location == "" {
			continue
		}
		if !strings.HasSuffix(location, ".") {
			location += "."
		}
		if _, ok := seen[location]; ok {
			continue
		}
		seen[location] = struct{}{}
		locations = append(locations, location)
	}
	return locations
}

func writeProteinLocationTSV(filePath string, records []proteinLocationRecord) error {
	var buffer bytes.Buffer
	writer := csv.NewWriter(&buffer)
	writer.Comma = '\t'
	if err := writer.Write([]string{"protein_id", "location", "source", "source_version"}); err != nil {
		return err
	}
	for _, record := range records {
		if err := writer.Write([]string{record.proteinID, record.location, record.source, record.sourceVersion}); err != nil {
			return err
		}
	}
	writer.Flush()
	if err := writer.Error(); err != nil {
		return err
	}
	return os.WriteFile(filePath, buffer.Bytes(), 0o644)
}

func createDefaultUniProtConfig() uniprotConfig {
	cfg := uniprotConfig{}
	cfg.RetryMax = 5
	cfg.RetryWait = 3 * time.Second
	cfg.WorkersMax = 1
	cfg.RuleExisting = "skip"
	return cfg
}

func validateUniProtConfig(cfg *uniprotConfig) error {
	if err := cliopt.ValidateDirOutRequired(cfg.DirOut); err != nil {
		return err
	}
	if err := cliopt.ValidateRetryConfig(&cfg.RetryConfig); err != nil {
		return err
	}
	if err := cliopt.ValidateDownloadControlConfig(&cfg.DownloadControlConfig); err != nil {
		return err
	}
	if err := cliopt.ValidateRuleExisting(&cfg.ExistingRuleConfig); err != nil {
		return err
	}
	_, err := resolveUniProtScope(cfg.speciesCode, cfg.taxID, cfg.proteomeID)
	return err
}

func buildUniProtOptions(
	dirOut string,
	cfgExisting cliopt.ExistingRuleConfig,
	cfgRetry cliopt.RetryConfig,
	cfgDownload cliopt.DownloadControlConfig,
	cfgTLS cliopt.InsecureTLSConfig,
	cfgDryRun cliopt.DryRunConfig,
) staticasset.Options {
	return staticasset.Options{
		DirOut:                 dirOut,
		RuleExisting:           cfgExisting.RuleExisting,
		RetryMax:               cfgRetry.RetryMax,
		RetryWait:              cfgRetry.RetryWait,
		WorkersMax:             cfgDownload.WorkersMax,
		RequestInterval:        cfgDownload.RequestInterval,
		ShouldAllowInsecureTLS: cfgTLS.ShouldAllowInsecureTLS,
		ShouldDryRun:           cfgDryRun.ShouldDryRun,
	}
}

func isDigits(value string) bool {
	for _, char := range value {
		if char < '0' || char > '9' {
			return false
		}
	}
	return value != ""
}
