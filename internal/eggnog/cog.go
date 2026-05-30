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
	defaultCOGVersion = "COG2024"
	defaultCOGBaseURL = "https://ftp.ncbi.nih.gov/pub/COG"
)

var patternCOGVersion = regexp.MustCompile(`^COG[0-9]{4}$`)

var cogAssetFiles = map[string]string{
	"category_definition": "cog-24.fun.tab",
	"definition":          "cog-24.def.tab",
	"readme":              "Readme.COG2024.txt",
}

type cogConfig struct {
	cliopt.DirOutConfig
	cliopt.VersionConfig
	cliopt.ExistingRuleConfig
	cliopt.RetryConfig
	cliopt.DownloadControlConfig
	cliopt.InsecureTLSConfig
	cliopt.DryRunConfig
	cliopt.ProgressConfig
	assetNames []string
	baseURL    string
}

type cogLockConfig struct {
	cliopt.DirOutConfig
	cliopt.VersionConfig
	cliopt.DryRunConfig
}

type cogSyncConfig struct {
	cliopt.DirOutConfig
	cliopt.VersionConfig
	cliopt.ExistingRuleConfig
	cliopt.RetryConfig
	cliopt.DownloadControlConfig
	cliopt.InsecureTLSConfig
	cliopt.DryRunConfig
	cliopt.ProgressConfig
}

func runFetchCOG(cfg *cogConfig) error {
	assets, err := resolveCOGAssets(cfg.assetNames)
	if err != nil {
		return err
	}
	versionToken, err := normalizeCOGVersionToken(firstNonEmpty(cfg.VersionToken, defaultCOGVersion))
	if err != nil {
		return err
	}
	source := buildCOGSource(versionToken, buildCOGStaticAssets(cfg.baseURL, versionToken, assets))
	return staticasset.Fetch(source, buildMapperOptions(cfg.DirOut, cfg.ExistingRuleConfig, cfg.RetryConfig, cfg.DownloadControlConfig, cfg.InsecureTLSConfig, cfg.DryRunConfig, cfg.ProgressConfig), nil)
}

func runLockCOG(cfg *cogLockConfig) error {
	versionToken, err := normalizeCOGVersionToken(cfg.VersionToken)
	if err != nil {
		return err
	}
	return staticasset.Lock(buildCOGSource(versionToken, nil), staticasset.Options{
		DirOut:       cfg.DirOut,
		RuleExisting: "skip",
		RetryMax:     1,
		WorkersMax:   1,
		ShouldDryRun: cfg.ShouldDryRun,
	}, nil)
}

func runSyncCOG(cfg *cogSyncConfig) error {
	versionToken, err := normalizeCOGVersionToken(cfg.VersionToken)
	if err != nil {
		return err
	}
	return staticasset.Sync(buildCOGSource(versionToken, nil), buildMapperOptions(cfg.DirOut, cfg.ExistingRuleConfig, cfg.RetryConfig, cfg.DownloadControlConfig, cfg.InsecureTLSConfig, cfg.DryRunConfig, cfg.ProgressConfig), nil)
}

func resolveCOGAssets(values []string) ([]string, error) {
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
		if _, ok := cogAssetFiles[asset]; !ok {
			unknown = append(unknown, asset)
			continue
		}
		assets = append(assets, asset)
	}
	if len(unknown) > 0 {
		return nil, fmt.Errorf("unknown COG asset(s): %s; supported: %s", strings.Join(unknown, ", "), strings.Join(sortedCOGAssetNames(), ", "))
	}
	if len(assets) == 0 {
		return nil, fmt.Errorf("assets must not be empty")
	}
	return sets.SortedKeys(stringSet(assets)), nil
}

func sortedCOGAssetNames() []string {
	names := make([]string, 0, len(cogAssetFiles))
	for name := range cogAssetFiles {
		names = append(names, name)
	}
	return sets.SortedKeys(stringSet(names))
}

func normalizeCOGVersionToken(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", fmt.Errorf("version is required")
	}
	if strings.EqualFold(value, "current") {
		return "", fmt.Errorf("COG version must be a fixed release token, not current")
	}
	if !strings.HasPrefix(strings.ToUpper(value), "COG") {
		value = "COG" + value
	}
	value = strings.ToUpper(value)
	if !patternCOGVersion.MatchString(value) {
		return "", fmt.Errorf("COG version must look like COG2024 or 2024: %s", value)
	}
	return value, nil
}

func buildCOGStaticAssets(baseURL string, versionToken string, assets []string) []staticasset.Asset {
	result := make([]staticasset.Asset, 0, len(assets))
	for _, asset := range assets {
		fileName := cogAssetFiles[asset]
		result = append(result, staticasset.Asset{
			Name: asset,
			Path: filepath.ToSlash(filepath.Join("raw", fileName)),
			URL:  strings.TrimRight(baseURL, "/") + "/" + versionToken + "/data/" + fileName,
		})
	}
	return result
}

func buildCOGSource(versionToken string, assets []staticasset.Asset) staticasset.Source {
	return staticasset.Source{
		Database:     "eggnog",
		Asset:        "cog",
		Source:       "ncbi",
		Version:      versionToken,
		VersionToken: versionToken,
		Assets:       assets,
	}
}

func createDefaultCOGConfig() cogConfig {
	cfg := cogConfig{}
	cfg.VersionToken = defaultCOGVersion
	cfg.RetryMax = 5
	cfg.RetryWait = 3 * time.Second
	cfg.WorkersMax = 1
	cfg.RuleExisting = "skip"
	cfg.baseURL = defaultCOGBaseURL
	return cfg
}

func validateCOGConfig(cfg *cogConfig) error {
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
	_, err := normalizeCOGVersionToken(firstNonEmpty(cfg.VersionToken, defaultCOGVersion))
	return err
}
