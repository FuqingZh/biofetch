package uniprot

import (
	"biofetch/internal/shared/cliopt"
	"biofetch/internal/shared/httpx"
	"biofetch/internal/shared/sets"
	"biofetch/internal/shared/staticasset"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
	"time"
)

const idMappingLabel = "UniProt ID mapping"

var idMappingAssetFiles = map[string]string{
	"dat":      "idmapping.dat.gz",
	"selected": "idmapping_selected.tab.gz",
}

type idMappingConfig struct {
	cliopt.DirOutConfig
	cliopt.VersionConfig
	cliopt.ExistingRuleConfig
	cliopt.RetryConfig
	cliopt.DownloadControlConfig
	cliopt.InsecureTLSConfig
	cliopt.DryRunConfig
	cliopt.ProgressConfig
	assetNames             []string
	baseURLCurrentRelease  string
	shouldAllowLargeAssets bool
}

type idMappingLockConfig struct {
	cliopt.DirOutConfig
	cliopt.VersionConfig
	cliopt.DryRunConfig
}

type idMappingSyncConfig struct {
	cliopt.DirOutConfig
	cliopt.VersionConfig
	cliopt.ExistingRuleConfig
	cliopt.RetryConfig
	cliopt.DownloadControlConfig
	cliopt.InsecureTLSConfig
	cliopt.DryRunConfig
	cliopt.ProgressConfig
}

func runFetchIDMapping(cfg *idMappingConfig) error {
	assets, err := resolveIDMappingAssets(cfg.assetNames)
	if err != nil {
		return err
	}
	if !cfg.shouldAllowLargeAssets {
		return fmt.Errorf("selected UniProt ID mapping assets are multi-GB files; pass --should_allow_large_assets to fetch")
	}
	clientHTTP := httpx.NewClient(cfg.ShouldAllowInsecureTLS)
	versionToken, err := resolveIDMappingFetchVersionToken(clientHTTP, cfg.VersionToken, cfg.baseURLCurrentRelease)
	if err != nil {
		return err
	}
	source := buildIDMappingSource(versionToken, buildIDMappingStaticAssets(cfg.baseURLCurrentRelease, assets))
	return staticasset.Fetch(source, buildIDMappingOptions(cfg.DirOut, cfg.ExistingRuleConfig, cfg.RetryConfig, cfg.DownloadControlConfig, cfg.InsecureTLSConfig, cfg.DryRunConfig, cfg.ProgressConfig), nil)
}

func runLockIDMapping(cfg *idMappingLockConfig) error {
	versionToken, err := normalizeIDMappingFixedVersionToken(cfg.VersionToken)
	if err != nil {
		return err
	}
	return staticasset.Lock(buildIDMappingSource(versionToken, nil), staticasset.Options{
		DirOut:       cfg.DirOut,
		RuleExisting: "skip",
		RetryMax:     1,
		WorkersMax:   1,
		ShouldDryRun: cfg.ShouldDryRun,
	}, nil)
}

func runSyncIDMapping(cfg *idMappingSyncConfig) error {
	versionToken, err := normalizeIDMappingFixedVersionToken(cfg.VersionToken)
	if err != nil {
		return err
	}
	return staticasset.Sync(buildIDMappingSource(versionToken, nil), buildIDMappingOptions(cfg.DirOut, cfg.ExistingRuleConfig, cfg.RetryConfig, cfg.DownloadControlConfig, cfg.InsecureTLSConfig, cfg.DryRunConfig, cfg.ProgressConfig), nil)
}

func resolveIDMappingAssets(values []string) ([]string, error) {
	if len(values) == 0 {
		return sortedIDMappingAssetNames(), nil
	}
	valuesResolved, err := cliopt.ExpandAtFileTokens(values, "assets")
	if err != nil {
		return nil, err
	}
	assets := make([]string, 0, len(valuesResolved))
	unknown := make([]string, 0)
	hasAll := false
	for _, value := range valuesResolved {
		asset := strings.ToLower(strings.TrimSpace(value))
		if asset == "" {
			continue
		}
		if asset == "all" {
			hasAll = true
			continue
		}
		if _, ok := idMappingAssetFiles[asset]; !ok {
			unknown = append(unknown, asset)
			continue
		}
		assets = append(assets, asset)
	}
	if hasAll {
		if len(assets) > 0 {
			return nil, fmt.Errorf("assets=all cannot be combined with specific UniProt ID mapping assets")
		}
		return sortedIDMappingAssetNames(), nil
	}
	if len(unknown) > 0 {
		return nil, fmt.Errorf("unknown UniProt ID mapping asset(s): %s; supported: %s", strings.Join(unknown, ", "), strings.Join(sortedIDMappingAssetNames(), ", "))
	}
	if len(assets) == 0 {
		return nil, fmt.Errorf("assets must not be empty")
	}
	return sets.SortedKeys(stringSet(assets)), nil
}

func sortedIDMappingAssetNames() []string {
	names := make([]string, 0, len(idMappingAssetFiles))
	for name := range idMappingAssetFiles {
		names = append(names, name)
	}
	return sets.SortedKeys(stringSet(names))
}

func buildIDMappingStaticAssets(baseURLCurrentRelease string, assets []string) []staticasset.Asset {
	result := make([]staticasset.Asset, 0, len(assets))
	for _, asset := range assets {
		fileName := idMappingAssetFiles[asset]
		result = append(result, staticasset.Asset{
			Name: asset,
			Path: filepath.ToSlash(filepath.Join("raw", fileName)),
			URL:  buildUniProtCurrentReleaseURL(baseURLCurrentRelease, "knowledgebase", "idmapping", fileName),
		})
	}
	return result
}

func buildIDMappingSource(versionToken string, assets []staticasset.Asset) staticasset.Source {
	return staticasset.Source{
		Database:     "uniprot",
		Asset:        "idmapping",
		Source:       "ftp",
		Version:      versionToken,
		VersionToken: versionToken,
		Assets:       assets,
	}
}

func resolveIDMappingFetchVersionToken(clientHTTP *http.Client, value string, baseURLCurrentRelease string) (string, error) {
	return resolveUniProtFetchVersionToken(clientHTTP, value, baseURLCurrentRelease, idMappingLabel)
}

func normalizeIDMappingFixedVersionToken(value string) (string, error) {
	return normalizeUniProtFixedVersionToken(value, idMappingLabel)
}

func parseIDMappingReleaseNotes(data []byte) (string, error) {
	return parseUniProtReleaseNotes(data, idMappingLabel)
}

func createDefaultIDMappingConfig() idMappingConfig {
	cfg := idMappingConfig{}
	cfg.RetryMax = 5
	cfg.RetryWait = 3 * time.Second
	cfg.WorkersMax = 1
	cfg.RuleExisting = "skip"
	cfg.baseURLCurrentRelease = uniprotCurrentReleaseBaseURL
	return cfg
}

func validateIDMappingConfig(cfg *idMappingConfig) error {
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
	return nil
}

func buildIDMappingOptions(
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
	result := make(map[string]struct{}, len(values))
	for _, value := range values {
		result[value] = struct{}{}
	}
	return result
}
