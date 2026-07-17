package geneontology

import (
	"biofetch/internal/shared/cliopt"
	"biofetch/internal/shared/httpx"
	"biofetch/internal/shared/logx"
	"biofetch/internal/shared/sets"
	"biofetch/internal/shared/staticasset"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
	"time"
)

var slimFormatsSupported = []string{"json", "obo", "owl", "tsv"}

var slimCurrentBaseURL = "https://current.geneontology.org/ontology/subsets/"

type slimConfig struct {
	cliopt.DirOutConfig
	cliopt.VersionConfig
	cliopt.ExistingRuleConfig
	cliopt.RetryConfig
	cliopt.DownloadControlConfig
	cliopt.InsecureTLSConfig
	cliopt.DryRunConfig
	cliopt.LogConfig
	cliopt.ProgressConfig
	version     string
	subsetNames []string
	formatNames []string
}

type slimLockConfig struct {
	cliopt.DirSnapshotConfig
	cliopt.DryRunConfig
	cliopt.LogConfig
}

type slimSyncConfig struct {
	cliopt.DirOutConfig
	cliopt.VersionConfig
	cliopt.ExistingRuleConfig
	cliopt.RetryConfig
	cliopt.DownloadControlConfig
	cliopt.InsecureTLSConfig
	cliopt.DryRunConfig
	cliopt.LogConfig
	cliopt.ProgressConfig
}

func runFetchSlim(cfg *slimConfig) error {
	clientHTTP := httpx.NewClient(cfg.ShouldAllowInsecureTLS)
	limiterRequest := httpx.NewRequestLimiter(cfg.RequestInterval)
	source, err := resolveSlimSource(clientHTTP, cfg.VersionToken, limiterRequest)
	if err != nil {
		return err
	}
	subsets, err := resolveSlimSubsets(cfg.subsetNames)
	if err != nil {
		return err
	}
	formats, err := resolveSlimFormats(cfg.formatNames)
	if err != nil {
		return err
	}
	assets := buildSlimAssets(source.baseURL, subsets, formats)
	cfg.version = source.version
	cfg.VersionToken = source.versionToken

	sourceStatic := staticasset.Source{
		Database:     "go",
		Asset:        "slim",
		Source:       "geneontology",
		Version:      source.version,
		VersionToken: source.versionToken,
		Scope: staticasset.Scope{
			Type:  "subsets_formats",
			Value: strings.Join(subsets, ",") + "|" + strings.Join(formats, ","),
		},
		Assets: assets,
	}
	trace, closeRun, err := logx.StartSourceRun("biofetch go", "fetch", cfg.DirLogs, cfg.DirOut, sourceStatic)
	if err != nil {
		return err
	}
	defer closeRun()
	if err := staticasset.Fetch(sourceStatic, buildSlimOptions(cfg.DirOut, cfg.ExistingRuleConfig, cfg.RetryConfig, cfg.DownloadControlConfig, cfg.InsecureTLSConfig, cfg.DryRunConfig, cfg.ProgressConfig), trace); err != nil {
		logx.Errorf("biofetch go", "fetch failed: %v", err)
		return err
	}
	return nil
}

func runLockSlim(cfg *slimLockConfig) error {
	versionToken, err := cliopt.SnapshotVersionToken(cfg.DirSnapshot)
	if err != nil {
		return err
	}
	source := staticasset.Source{
		Database:     "go",
		Asset:        "slim",
		Source:       "geneontology",
		Version:      versionToken,
		VersionToken: versionToken,
	}
	_, closeRun, err := logx.StartVersionedRun("biofetch go", "lock", cfg.DirLogs, cfg.DirSnapshot)
	if err != nil {
		return err
	}
	defer closeRun()
	trace := logx.NewStaticAssetTraceSink("biofetch go")
	if err := staticasset.Lock(source, cfg.DirSnapshot, staticasset.Options{
		RuleExisting: "skip",
		RetryMax:     1,
		WorkersMax:   1,
		ShouldDryRun: cfg.ShouldDryRun,
	}, trace); err != nil {
		logx.Errorf("biofetch go", "lock failed: %v", err)
		return err
	}
	return nil
}

func runSyncSlim(cfg *slimSyncConfig) error {
	source := staticasset.Source{
		Database:     "go",
		Asset:        "slim",
		Source:       "geneontology",
		Version:      cfg.VersionToken,
		VersionToken: cfg.VersionToken,
	}
	trace, closeRun, err := logx.StartSourceRun("biofetch go", "sync", cfg.DirLogs, cfg.DirOut, source)
	if err != nil {
		return err
	}
	defer closeRun()
	if err := staticasset.Sync(source, buildSlimOptions(cfg.DirOut, cfg.ExistingRuleConfig, cfg.RetryConfig, cfg.DownloadControlConfig, cfg.InsecureTLSConfig, cfg.DryRunConfig, cfg.ProgressConfig), trace); err != nil {
		logx.Errorf("biofetch go", "sync failed: %v", err)
		return err
	}
	return nil
}

type slimSource struct {
	version      string
	versionToken string
	baseURL      string
}

func resolveSlimSource(
	clientHTTP *http.Client,
	versionToken string,
	limiterRequest *httpx.RequestLimiter,
) (slimSource, error) {
	if versionToken := strings.TrimSpace(versionToken); versionToken == "" {
		version, err := resolveOntologyVersion(clientHTTP, ontologyCurrentBaseURL, limiterRequest)
		if err != nil {
			return slimSource{}, err
		}
		return slimSource{
			version:      version,
			versionToken: version,
			baseURL:      slimCurrentBaseURL,
		}, nil
	}

	if err := validateOptionalOntologyVersionToken(versionToken); err != nil {
		return slimSource{}, err
	}

	baseOntologyURL := buildOntologyReleaseBaseURL(versionToken)
	version, err := resolveOntologyVersion(clientHTTP, baseOntologyURL, limiterRequest)
	if err != nil {
		return slimSource{}, fmt.Errorf(
			"GO release %q not found or unreadable at %s: %w (see %s)",
			versionToken,
			baseOntologyURL,
			err,
			ontologyArchiveRootURL,
		)
	}
	if version != versionToken {
		return slimSource{}, fmt.Errorf(
			"GO release %q resolved to %q at %s (see %s)",
			versionToken,
			version,
			baseOntologyURL,
			ontologyArchiveRootURL,
		)
	}
	return slimSource{
		version:      version,
		versionToken: versionToken,
		baseURL:      buildSlimReleaseBaseURL(versionToken),
	}, nil
}

func resolveSlimSubsets(values []string) ([]string, error) {
	if len(values) == 0 {
		return []string{"goslim_generic"}, nil
	}
	valuesResolved, err := cliopt.ExpandAtFileTokens(values, "subsets")
	if err != nil {
		return nil, err
	}
	subsets := make([]string, 0, len(valuesResolved))
	for _, value := range valuesResolved {
		subset := strings.TrimSpace(value)
		if subset == "" {
			continue
		}
		if !isValidSlimSubsetName(subset) {
			return nil, fmt.Errorf("invalid GO Slim subset: %s", subset)
		}
		subsets = append(subsets, subset)
	}
	if len(subsets) == 0 {
		return nil, fmt.Errorf("subsets must not be empty")
	}
	return sets.SortedKeys(stringSet(subsets)), nil
}

func resolveSlimFormats(values []string) ([]string, error) {
	if len(values) == 0 {
		return []string{"obo"}, nil
	}
	valuesResolved, err := cliopt.ExpandAtFileTokens(values, "formats")
	if err != nil {
		return nil, err
	}
	formatsSupported := stringSet(slimFormatsSupported)
	formats := make([]string, 0, len(valuesResolved))
	for _, value := range valuesResolved {
		format := strings.ToLower(strings.TrimSpace(value))
		if format == "" {
			continue
		}
		if _, ok := formatsSupported[format]; !ok {
			return nil, fmt.Errorf("unsupported GO Slim format %q; supported: %s", format, strings.Join(slimFormatsSupported, ", "))
		}
		formats = append(formats, format)
	}
	if len(formats) == 0 {
		return nil, fmt.Errorf("formats must not be empty")
	}
	return sets.SortedKeys(stringSet(formats)), nil
}

func buildSlimAssets(baseURL string, subsets []string, formats []string) []staticasset.Asset {
	assets := make([]staticasset.Asset, 0, len(subsets)*len(formats))
	for _, subset := range subsets {
		for _, format := range formats {
			fileName := subset + "." + format
			assets = append(assets, staticasset.Asset{
				Name: fileName,
				Path: filepath.ToSlash(filepath.Join("raw", fileName)),
				URL:  buildSlimAssetURL(baseURL, fileName),
			})
		}
	}
	return assets
}

func buildSlimAssetURL(baseURL string, fileName string) string {
	return strings.TrimRight(baseURL, "/") + "/" + fileName
}

func buildSlimReleaseBaseURL(versionToken string) string {
	return ontologyArchiveRootURL + versionToken + "/ontology/subsets/"
}

func isValidSlimSubsetName(value string) bool {
	if !strings.HasPrefix(value, "goslim_") {
		return false
	}
	for _, char := range value {
		if char >= 'a' && char <= 'z' {
			continue
		}
		if char >= '0' && char <= '9' {
			continue
		}
		if char == '_' {
			continue
		}
		return false
	}
	return true
}

func createDefaultSlimConfig() slimConfig {
	cfg := slimConfig{}
	cfg.RetryMax = 5
	cfg.RetryWait = 3 * time.Second
	cfg.WorkersMax = 1
	cfg.RuleExisting = "skip"
	return cfg
}

func validateSlimConfig(cfg *slimConfig) error {
	if err := cliopt.ValidateDirOutRequired(cfg.DirOut); err != nil {
		return err
	}
	if err := validateOptionalOntologyVersionToken(cfg.VersionToken); err != nil {
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
	return nil
}

func buildSlimOptions(
	dirOut string,
	cfgExisting cliopt.ExistingRuleConfig,
	cfgRetry cliopt.RetryConfig,
	cfgDownload cliopt.DownloadControlConfig,
	cfgTLS cliopt.InsecureTLSConfig,
	cfgDryRun cliopt.DryRunConfig,
	cfgProgress cliopt.ProgressConfig,
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
		ShouldDisableProgress:  cfgProgress.ShouldDisableProgress,
	}
}

func stringSet(values []string) map[string]struct{} {
	set := make(map[string]struct{}, len(values))
	for _, value := range values {
		set[value] = struct{}{}
	}
	return set
}
