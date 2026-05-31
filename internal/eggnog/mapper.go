package eggnog

import (
	"biofetch/internal/shared/cliopt"
	"biofetch/internal/shared/sets"
	"biofetch/internal/shared/staticasset"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

const (
	defaultMapperVersion = "5.0.2"
	defaultMapperBaseURL = "https://downloads.eggnogdb.org/emapper"
)

var patternMapperVersionMajor = regexp.MustCompile(`^[0-9]+$`)
var patternMapperVersionFull = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+$`)

var mapperAssetFiles = map[string]string{
	"db":      "eggnog.db.gz",
	"diamond": "eggnog_proteins.dmnd.gz",
	"taxa":    "eggnog.taxa.tar.gz",
}

type mapperConfig struct {
	cliopt.DirOutConfig
	cliopt.VersionConfig
	cliopt.ExistingRuleConfig
	cliopt.RetryConfig
	cliopt.DownloadControlConfig
	cliopt.InsecureTLSConfig
	cliopt.DryRunConfig
	cliopt.ProgressConfig
	assetNames               []string
	baseURL                  string
	shouldAllowLargeDownload bool
}

type mapperLockConfig struct {
	cliopt.DirOutConfig
	cliopt.VersionConfig
	cliopt.DryRunConfig
}

type mapperSyncConfig struct {
	cliopt.DirOutConfig
	cliopt.VersionConfig
	cliopt.ExistingRuleConfig
	cliopt.RetryConfig
	cliopt.DownloadControlConfig
	cliopt.InsecureTLSConfig
	cliopt.DryRunConfig
	cliopt.ProgressConfig
}

func runFetchMapper(cfg *mapperConfig) error {
	assets, err := resolveMapperAssets(cfg.assetNames)
	if err != nil {
		return err
	}
	if hasLargeMapperAsset(assets) && !cfg.shouldAllowLargeDownload {
		return fmt.Errorf("eggNOG mapper database assets are large downloads; pass --should_allow_large_download to fetch")
	}
	versionToken, err := normalizeMapperVersionToken(firstNonEmpty(cfg.VersionToken, defaultMapperVersion))
	if err != nil {
		return err
	}
	source := buildMapperSource(versionToken, buildMapperStaticAssets(cfg.baseURL, versionToken, assets))
	return staticasset.Fetch(source, buildMapperOptions(cfg.DirOut, cfg.ExistingRuleConfig, cfg.RetryConfig, cfg.DownloadControlConfig, cfg.InsecureTLSConfig, cfg.DryRunConfig, cfg.ProgressConfig), nil)
}

func runLockMapper(cfg *mapperLockConfig) error {
	versionToken, err := normalizeMapperVersionToken(cfg.VersionToken)
	if err != nil {
		return err
	}
	return staticasset.Lock(buildMapperSource(versionToken, nil), staticasset.Options{
		DirOut:       cfg.DirOut,
		RuleExisting: "skip",
		RetryMax:     1,
		WorkersMax:   1,
		ShouldDryRun: cfg.ShouldDryRun,
	}, nil)
}

func runSyncMapper(cfg *mapperSyncConfig) error {
	versionToken, err := normalizeMapperVersionToken(cfg.VersionToken)
	if err != nil {
		return err
	}
	return staticasset.Sync(buildMapperSource(versionToken, nil), buildMapperOptions(cfg.DirOut, cfg.ExistingRuleConfig, cfg.RetryConfig, cfg.DownloadControlConfig, cfg.InsecureTLSConfig, cfg.DryRunConfig, cfg.ProgressConfig), nil)
}

func resolveMapperAssets(values []string) ([]string, error) {
	if len(values) == 0 {
		return nil, fmt.Errorf("assets must not be empty")
	}
	valuesResolved, err := cliopt.ExpandAtFileTokens(values, "assets")
	if err != nil {
		return nil, err
	}
	assets := make([]string, 0, len(valuesResolved))
	unknown := make([]string, 0)
	for _, value := range valuesResolved {
		asset := strings.ToLower(strings.TrimSpace(value))
		if asset == "" {
			continue
		}
		if _, ok := mapperAssetFiles[asset]; !ok {
			unknown = append(unknown, asset)
			continue
		}
		assets = append(assets, asset)
	}
	if len(unknown) > 0 {
		return nil, fmt.Errorf("unknown eggNOG mapper asset(s): %s; supported: %s", strings.Join(unknown, ", "), strings.Join(sortedMapperAssetNames(), ", "))
	}
	if len(assets) == 0 {
		return nil, fmt.Errorf("assets must not be empty")
	}
	return sets.SortedKeys(stringSet(assets)), nil
}

func sortedMapperAssetNames() []string {
	names := make([]string, 0, len(mapperAssetFiles))
	for name := range mapperAssetFiles {
		names = append(names, name)
	}
	return sets.SortedKeys(stringSet(names))
}

func hasLargeMapperAsset(assets []string) bool {
	return len(assets) > 0
}

func normalizeMapperVersionToken(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", fmt.Errorf("version is required")
	}
	if strings.EqualFold(value, "current") {
		return "", fmt.Errorf("eggNOG mapper version must be a fixed database version, not current")
	}
	if patternMapperVersionMajor.MatchString(value) {
		if value == "5" {
			return defaultMapperVersion, nil
		}
		return value + ".0.0", nil
	}
	if !patternMapperVersionFull.MatchString(value) {
		return "", fmt.Errorf("eggNOG mapper version must look like 7 or 7.0.0: %s", value)
	}
	return value, nil
}

func buildMapperStaticAssets(baseURL string, versionToken string, assets []string) []staticasset.Asset {
	result := make([]staticasset.Asset, 0, len(assets))
	for _, asset := range assets {
		fileName := mapperAssetFiles[asset]
		result = append(result, staticasset.Asset{
			Name: asset,
			Path: filepath.ToSlash(filepath.Join("raw", fileName)),
			URL:  strings.TrimRight(baseURL, "/") + "/emapperdb-" + versionToken + "/" + fileName,
		})
	}
	return result
}

func buildMapperSource(versionToken string, assets []staticasset.Asset) staticasset.Source {
	return staticasset.Source{
		Database:     "eggnog",
		Asset:        "mapper",
		Source:       "download",
		Version:      versionToken,
		VersionToken: versionToken,
		Assets:       assets,
	}
}

func createDefaultMapperConfig() mapperConfig {
	cfg := mapperConfig{}
	cfg.VersionToken = defaultMapperVersion
	cfg.RetryMax = 5
	cfg.RetryWait = 3 * time.Second
	cfg.WorkersMax = 1
	cfg.RuleExisting = "skip"
	cfg.baseURL = defaultMapperBaseURL
	return cfg
}

func validateMapperConfig(cfg *mapperConfig) error {
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
	_, err := normalizeMapperVersionToken(firstNonEmpty(cfg.VersionToken, defaultMapperVersion))
	return err
}

func buildMapperOptions(
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

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func stringSet(values []string) map[string]struct{} {
	result := make(map[string]struct{}, len(values))
	for _, value := range values {
		result[value] = struct{}{}
	}
	return result
}
