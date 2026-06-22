package kegg

import (
	"biofetch/internal/shared/httpx"
	"biofetch/internal/shared/logx"
	"biofetch/internal/shared/sets"
	"biofetch/internal/shared/staticasset"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

var keggMappingBaseURL = baseURL

var mappingAssetNamesSupported = []string{
	"organism",
	"conv_uniprot",
	"conv_ncbi_geneid",
	"gene_list",
	"gene_ko",
	"gene_pathway",
	"ko_pathway",
}

type mappingConfig struct {
	dirOut                  string
	version                 string
	versionToken            string
	assetNames              []string
	organismCodes           []string
	shouldDownloadAll       bool
	ruleExisting            string
	shouldOverwriteExisting bool
	retryMax                int
	retryWait               time.Duration
	workersMax              int
	requestInterval         time.Duration
	shouldAllowInsecureTLS  bool
	shouldDryRun            bool
	shouldDisableProgress   bool
	dirLogs                 string
	scopeType               string
	scopeValue              string
}

type mappingLockConfig struct {
	dirOut       string
	versionToken string
	shouldDryRun bool
	dirLogs      string
}

type mappingSyncConfig struct {
	dirOut                 string
	versionToken           string
	ruleExisting           string
	retryMax               int
	retryWait              time.Duration
	workersMax             int
	requestInterval        time.Duration
	shouldAllowInsecureTLS bool
	shouldDryRun           bool
	shouldDisableProgress  bool
	dirLogs                string
}

func createDefaultMappingConfig() mappingConfig {
	cfg := mappingConfig{}
	cfg.retryMax = defaultKEGGRetryMax
	cfg.retryWait = defaultKEGGRetryWait
	cfg.workersMax = 1
	cfg.requestInterval = 350 * time.Millisecond
	cfg.ruleExisting = "skip"
	return cfg
}

func runFetchMapping(cfg *mappingConfig) error {
	timeStarted := time.Now()
	if strings.TrimSpace(cfg.versionToken) == "" {
		cfg.versionToken = deriveKEGGSnapshotVersionToken(timeStarted)
	}
	cfg.version = cfg.versionToken

	assets, err := resolveMappingAssetNames(cfg.assetNames)
	if err != nil {
		return err
	}
	organismCodes, err := resolveMappingOrganismCodes(cfg)
	if err != nil {
		return err
	}
	source := buildMappingSource(cfg.versionToken, buildMappingStaticAssets(assets, organismCodes), deriveMappingScope(cfg, organismCodes))
	trace, closeRun, err := logx.StartSourceRun("biofetch kegg", "fetch", cfg.dirLogs, cfg.dirOut, source)
	if err != nil {
		return err
	}
	defer closeRun()
	if err := staticasset.Fetch(source, buildMappingOptions(cfg), trace); err != nil {
		logx.Errorf("biofetch kegg", "fetch failed: %v", err)
		return err
	}
	return nil
}

func runLockMapping(cfg *mappingLockConfig) error {
	if err := validateMappingFixedVersion(cfg.versionToken); err != nil {
		return err
	}
	source := buildMappingSource(cfg.versionToken, nil, staticasset.Scope{})
	trace, closeRun, err := logx.StartSourceRun("biofetch kegg", "lock", cfg.dirLogs, cfg.dirOut, source)
	if err != nil {
		return err
	}
	defer closeRun()
	if err := staticasset.Lock(source, staticasset.Options{
		DirOut:       cfg.dirOut,
		RuleExisting: "skip",
		RetryMax:     1,
		WorkersMax:   1,
		ShouldDryRun: cfg.shouldDryRun,
	}, trace); err != nil {
		logx.Errorf("biofetch kegg", "lock failed: %v", err)
		return err
	}
	return nil
}

func runSyncMapping(cfg *mappingSyncConfig) error {
	if err := validateMappingFixedVersion(cfg.versionToken); err != nil {
		return err
	}
	options := staticasset.Options{
		DirOut:                 cfg.dirOut,
		RuleExisting:           cfg.ruleExisting,
		RetryMax:               cfg.retryMax,
		RetryWait:              cfg.retryWait,
		WorkersMax:             cfg.workersMax,
		RequestInterval:        cfg.requestInterval,
		ShouldAllowInsecureTLS: cfg.shouldAllowInsecureTLS,
		ShouldDryRun:           cfg.shouldDryRun,
		ShouldDisableProgress:  cfg.shouldDisableProgress,
	}
	source := buildMappingSource(cfg.versionToken, nil, staticasset.Scope{})
	trace, closeRun, err := logx.StartSourceRun("biofetch kegg", "sync", cfg.dirLogs, cfg.dirOut, source)
	if err != nil {
		return err
	}
	defer closeRun()
	if err := staticasset.Sync(source, options, trace); err != nil {
		logx.Errorf("biofetch kegg", "sync failed: %v", err)
		return err
	}
	return nil
}

func validateMappingFetchConfig(cfg *mappingConfig) error {
	if strings.TrimSpace(cfg.dirOut) == "" {
		return fmt.Errorf("dir_out is required")
	}
	if strings.TrimSpace(cfg.versionToken) != "" && !isValidKEGGSnapshotVersionToken(cfg.versionToken) {
		return fmt.Errorf("version must be a local snapshot key like 2026-04")
	}
	if cfg.ruleExisting != "skip" && cfg.ruleExisting != "overwrite" {
		return fmt.Errorf("rule_existing must be one of: skip, overwrite")
	}
	cfg.shouldOverwriteExisting = cfg.ruleExisting == "overwrite"
	if cfg.retryMax < 1 {
		return fmt.Errorf("retry_max must be >= 1")
	}
	if cfg.retryWait < 0 {
		return fmt.Errorf("retry_wait_sec must be >= 0")
	}
	if cfg.workersMax < 1 {
		return fmt.Errorf("workers_max must be >= 1")
	}
	if cfg.requestInterval < 0 {
		return fmt.Errorf("request_interval_ms must be >= 0")
	}
	if cfg.shouldDownloadAll && len(cfg.organismCodes) > 0 {
		return fmt.Errorf("choose either --organisms or --should_download_all_organisms, not both")
	}
	assets, err := resolveMappingAssetNames(cfg.assetNames)
	if err != nil {
		return err
	}
	if hasOrganismScopedMappingAsset(assets) && !cfg.shouldDownloadAll && len(cfg.organismCodes) == 0 {
		return fmt.Errorf("organism-scoped mapping assets require --organisms or --should_download_all_organisms")
	}
	return nil
}

func validateMappingLockConfig(cfg *mappingLockConfig) error {
	if strings.TrimSpace(cfg.dirOut) == "" {
		return fmt.Errorf("dir_out is required")
	}
	return validateMappingFixedVersion(cfg.versionToken)
}

func validateMappingSyncConfig(cfg *mappingSyncConfig) error {
	if strings.TrimSpace(cfg.dirOut) == "" {
		return fmt.Errorf("dir_out is required")
	}
	if err := validateMappingFixedVersion(cfg.versionToken); err != nil {
		return err
	}
	if cfg.ruleExisting != "skip" && cfg.ruleExisting != "overwrite" {
		return fmt.Errorf("rule_existing must be one of: skip, overwrite")
	}
	if cfg.retryMax < 1 {
		return fmt.Errorf("retry_max must be >= 1")
	}
	if cfg.retryWait < 0 {
		return fmt.Errorf("retry_wait_sec must be >= 0")
	}
	if cfg.workersMax < 1 {
		return fmt.Errorf("workers_max must be >= 1")
	}
	if cfg.requestInterval < 0 {
		return fmt.Errorf("request_interval_ms must be >= 0")
	}
	return nil
}

func validateMappingFixedVersion(versionToken string) error {
	if strings.TrimSpace(versionToken) == "" {
		return fmt.Errorf("version is required")
	}
	if !isValidKEGGSnapshotVersionToken(versionToken) {
		return fmt.Errorf("version must be a local snapshot key like 2026-04")
	}
	return nil
}

func resolveMappingAssetNames(valuesInput []string) ([]string, error) {
	if len(valuesInput) == 0 {
		return append([]string(nil), mappingAssetNamesSupported...), nil
	}
	setAssets := make(map[string]struct{})
	hasAll := false
	for _, valueInput := range valuesInput {
		for _, token := range strings.Split(valueInput, ",") {
			assetName := strings.ToLower(strings.TrimSpace(token))
			if assetName == "" {
				continue
			}
			if assetName == "all" {
				hasAll = true
				continue
			}
			if !isSupportedMappingAssetName(assetName) {
				return nil, fmt.Errorf("invalid KEGG mapping asset: %s", token)
			}
			setAssets[assetName] = struct{}{}
		}
	}
	if hasAll {
		if len(setAssets) > 0 {
			return nil, fmt.Errorf("assets=all cannot be combined with specific KEGG mapping assets")
		}
		return append([]string(nil), mappingAssetNamesSupported...), nil
	}
	if len(setAssets) == 0 {
		return nil, fmt.Errorf("assets must not be empty")
	}
	return sets.SortedKeys(setAssets), nil
}

func isSupportedMappingAssetName(assetName string) bool {
	for _, value := range mappingAssetNamesSupported {
		if value == assetName {
			return true
		}
	}
	return false
}

func hasOrganismScopedMappingAsset(assets []string) bool {
	for _, asset := range assets {
		switch asset {
		case "conv_uniprot", "conv_ncbi_geneid", "gene_list", "gene_ko", "gene_pathway":
			return true
		}
	}
	return false
}

func resolveMappingOrganismCodes(cfg *mappingConfig) ([]string, error) {
	assets, err := resolveMappingAssetNames(cfg.assetNames)
	if err != nil {
		return nil, err
	}
	if !hasOrganismScopedMappingAsset(assets) && len(cfg.organismCodes) == 0 && !cfg.shouldDownloadAll {
		return nil, nil
	}
	if cfg.shouldDownloadAll {
		clientHTTP := createHTTPClient(cfg.shouldAllowInsecureTLS)
		clientKegg := createKEGGClient(clientHTTP, cfg.requestInterval, cfg.retryMax, cfg.retryWait)
		data, err := clientKegg.download(deriveMappingGenomeListURL())
		if err != nil {
			return nil, err
		}
		organismCodes, err := parseKEGGOrganismCodesFromList(data)
		if err != nil {
			return nil, err
		}
		return applyTraversalOrder(organismCodes, ruleOrderAsc), nil
	}
	return parseKEGGOrganismCodes(cfg.organismCodes)
}

func deriveMappingScope(cfg *mappingConfig, organismCodes []string) staticasset.Scope {
	switch {
	case cfg.shouldDownloadAll:
		return staticasset.Scope{Type: "organisms", Value: "all"}
	case len(organismCodes) == 1:
		return staticasset.Scope{Type: "organism", Value: organismCodes[0]}
	case len(organismCodes) > 1:
		return staticasset.Scope{Type: "organisms", Value: strings.Join(organismCodes, ",")}
	default:
		return staticasset.Scope{Type: "global", Value: "kegg"}
	}
}

func buildMappingStaticAssets(assets []string, organismCodes []string) []staticasset.Asset {
	result := make([]staticasset.Asset, 0, len(assets)*(len(organismCodes)+1))
	for _, asset := range assets {
		switch asset {
		case "organism":
			result = append(result, staticasset.Asset{
				Name: "organism",
				Path: filepath.ToSlash(filepath.Join("raw", "organism", "list_organism.tsv")),
				URL:  deriveMappingGenomeListURL(),
			})
		case "ko_pathway":
			result = append(result, staticasset.Asset{
				Name: "ko_pathway",
				Path: filepath.ToSlash(filepath.Join("raw", "ko", "ko_pathway.tsv")),
				URL:  keggMappingBaseURL + "/link/pathway/ko",
			})
		case "conv_uniprot", "conv_ncbi_geneid", "gene_list", "gene_ko", "gene_pathway":
			for _, organismCode := range organismCodes {
				result = append(result, buildOrganismMappingAsset(asset, organismCode))
			}
		}
	}
	return result
}

func deriveMappingGenomeListURL() string {
	return keggMappingBaseURL + "/list/genome"
}

func buildOrganismMappingAsset(asset string, organismCode string) staticasset.Asset {
	switch asset {
	case "conv_uniprot":
		return staticasset.Asset{
			Name:                 organismCode + ".conv_uniprot",
			Path:                 filepath.ToSlash(filepath.Join("raw", organismCode, "conv_uniprot.tsv")),
			URL:                  keggMappingBaseURL + "/conv/" + organismCode + "/uniprot",
			RecoverDownloadError: recoverMissingKEGGOrganismScopedMapping,
		}
	case "conv_ncbi_geneid":
		return staticasset.Asset{
			Name:                 organismCode + ".conv_ncbi_geneid",
			Path:                 filepath.ToSlash(filepath.Join("raw", organismCode, "conv_ncbi_geneid.tsv")),
			URL:                  keggMappingBaseURL + "/conv/" + organismCode + "/ncbi-geneid",
			RecoverDownloadError: recoverMissingKEGGOrganismScopedMapping,
		}
	case "gene_list":
		return staticasset.Asset{
			Name:                 organismCode + ".gene_list",
			Path:                 filepath.ToSlash(filepath.Join("raw", organismCode, "gene_list.tsv")),
			URL:                  keggMappingBaseURL + "/list/" + organismCode,
			RecoverDownloadError: recoverMissingKEGGOrganismScopedMapping,
		}
	case "gene_ko":
		return staticasset.Asset{
			Name:                 organismCode + ".gene_ko",
			Path:                 filepath.ToSlash(filepath.Join("raw", organismCode, "gene_ko.tsv")),
			URL:                  keggMappingBaseURL + "/link/ko/" + organismCode,
			RecoverDownloadError: recoverMissingKEGGOrganismScopedMapping,
		}
	case "gene_pathway":
		return staticasset.Asset{
			Name:                 organismCode + ".gene_pathway",
			Path:                 filepath.ToSlash(filepath.Join("raw", organismCode, "gene_pathway.tsv")),
			URL:                  keggMappingBaseURL + "/link/pathway/" + organismCode,
			RecoverDownloadError: recoverMissingKEGGOrganismScopedMapping,
		}
	default:
		return staticasset.Asset{}
	}
}

func recoverMissingKEGGOrganismScopedMapping(fileOut string, err error) (bool, error) {
	if !httpx.IsUnexpectedStatus(err, 400) {
		return false, nil
	}
	if err := os.WriteFile(fileOut, nil, 0o644); err != nil {
		return false, fmt.Errorf("write empty KEGG organism-scoped mapping %s: %w", fileOut, err)
	}
	return true, nil
}

func buildMappingSource(versionToken string, assets []staticasset.Asset, scope staticasset.Scope) staticasset.Source {
	return staticasset.Source{
		Database:     "kegg",
		Asset:        "mapping",
		Source:       "rest",
		Version:      versionToken,
		VersionToken: versionToken,
		Scope:        scope,
		Assets:       assets,
	}
}

func buildMappingOptions(cfg *mappingConfig) staticasset.Options {
	return staticasset.Options{
		DirOut:                 cfg.dirOut,
		RuleExisting:           cfg.ruleExisting,
		RetryMax:               cfg.retryMax,
		RetryWait:              cfg.retryWait,
		WorkersMax:             cfg.workersMax,
		RequestInterval:        cfg.requestInterval,
		ShouldAllowInsecureTLS: cfg.shouldAllowInsecureTLS,
		ShouldDryRun:           cfg.shouldDryRun,
		ShouldDisableProgress:  cfg.shouldDisableProgress,
	}
}
