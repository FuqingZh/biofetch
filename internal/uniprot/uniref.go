package uniprot

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

const unirefLabel = "UniRef"

var unirefAssetFiles = map[string]string{
	"uniref90": "uniref90.fasta.gz",
}

type unirefConfig struct {
	cliopt.DirOutConfig
	cliopt.VersionConfig
	cliopt.ExistingRuleConfig
	cliopt.RetryConfig
	cliopt.DownloadControlConfig
	cliopt.InsecureTLSConfig
	cliopt.DryRunConfig
	cliopt.LogConfig
	cliopt.ProgressConfig
	assetNames             []string
	baseURLCurrentRelease  string
	shouldAllowLargeAssets bool
}

type unirefLockConfig struct {
	cliopt.DirOutConfig
	cliopt.VersionConfig
	cliopt.DryRunConfig
	cliopt.LogConfig
}

type unirefSyncConfig struct {
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

func runFetchUniRef(cfg *unirefConfig) error {
	assets, err := resolveUniRefAssets(cfg.assetNames)
	if err != nil {
		return err
	}
	if !cfg.shouldAllowLargeAssets {
		return fmt.Errorf("selected UniRef assets are large files; pass --should_allow_large_assets to fetch")
	}
	clientHTTP := httpx.NewClient(cfg.ShouldAllowInsecureTLS)
	versionToken, err := resolveUniRefFetchVersionToken(clientHTTP, cfg.VersionToken, cfg.baseURLCurrentRelease)
	if err != nil {
		return err
	}
	source := buildUniRefSource(versionToken, buildUniRefStaticAssets(cfg.baseURLCurrentRelease, assets))
	trace, closeRun, err := logx.StartSourceRun("biofetch uniprot", "fetch", cfg.DirLogs, cfg.DirOut, source)
	if err != nil {
		return err
	}
	defer closeRun()
	if err := staticasset.Fetch(source, buildIDMappingOptions(cfg.DirOut, cfg.ExistingRuleConfig, cfg.RetryConfig, cfg.DownloadControlConfig, cfg.InsecureTLSConfig, cfg.DryRunConfig, cfg.ProgressConfig), trace); err != nil {
		logx.Errorf("biofetch uniprot", "fetch failed: %v", err)
		return err
	}
	return nil
}

func runLockUniRef(cfg *unirefLockConfig) error {
	versionToken, err := normalizeUniRefFixedVersionToken(cfg.VersionToken)
	if err != nil {
		return err
	}
	source := buildUniRefSource(versionToken, nil)
	trace, closeRun, err := logx.StartSourceRun("biofetch uniprot", "lock", cfg.DirLogs, cfg.DirOut, source)
	if err != nil {
		return err
	}
	defer closeRun()
	if err := staticasset.Lock(source, staticasset.Options{
		DirOut:       cfg.DirOut,
		RuleExisting: "skip",
		RetryMax:     1,
		WorkersMax:   1,
		ShouldDryRun: cfg.ShouldDryRun,
	}, trace); err != nil {
		logx.Errorf("biofetch uniprot", "lock failed: %v", err)
		return err
	}
	return nil
}

func runSyncUniRef(cfg *unirefSyncConfig) error {
	versionToken, err := normalizeUniRefFixedVersionToken(cfg.VersionToken)
	if err != nil {
		return err
	}
	source := buildUniRefSource(versionToken, nil)
	trace, closeRun, err := logx.StartSourceRun("biofetch uniprot", "sync", cfg.DirLogs, cfg.DirOut, source)
	if err != nil {
		return err
	}
	defer closeRun()
	if err := staticasset.Sync(source, buildIDMappingOptions(cfg.DirOut, cfg.ExistingRuleConfig, cfg.RetryConfig, cfg.DownloadControlConfig, cfg.InsecureTLSConfig, cfg.DryRunConfig, cfg.ProgressConfig), trace); err != nil {
		logx.Errorf("biofetch uniprot", "sync failed: %v", err)
		return err
	}
	return nil
}

func resolveUniRefAssets(values []string) ([]string, error) {
	if len(values) == 0 {
		return sortedUniRefAssetNames(), nil
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
		if _, ok := unirefAssetFiles[asset]; !ok {
			unknown = append(unknown, asset)
			continue
		}
		assets = append(assets, asset)
	}
	if hasAll {
		if len(assets) > 0 {
			return nil, fmt.Errorf("assets=all cannot be combined with specific UniRef assets")
		}
		return sortedUniRefAssetNames(), nil
	}
	if len(unknown) > 0 {
		return nil, fmt.Errorf("unknown UniRef asset(s): %s; supported: %s", strings.Join(unknown, ", "), strings.Join(sortedUniRefAssetNames(), ", "))
	}
	if len(assets) == 0 {
		return nil, fmt.Errorf("assets must not be empty")
	}
	return sets.SortedKeys(stringSet(assets)), nil
}

func sortedUniRefAssetNames() []string {
	names := make([]string, 0, len(unirefAssetFiles))
	for name := range unirefAssetFiles {
		names = append(names, name)
	}
	return sets.SortedKeys(stringSet(names))
}

func buildUniRefStaticAssets(baseURLCurrentRelease string, assets []string) []staticasset.Asset {
	result := make([]staticasset.Asset, 0, len(assets))
	for _, asset := range assets {
		fileName := unirefAssetFiles[asset]
		result = append(result, staticasset.Asset{
			Name: asset,
			Path: filepath.ToSlash(filepath.Join("raw", "uniref", asset, fileName)),
			URL:  buildUniProtCurrentReleaseURL(baseURLCurrentRelease, "uniref", asset, fileName),
		})
	}
	return result
}

func buildUniRefSource(versionToken string, assets []staticasset.Asset) staticasset.Source {
	return staticasset.Source{
		Database:     "uniprot",
		Asset:        "uniref",
		Source:       "ftp",
		Version:      versionToken,
		VersionToken: versionToken,
		Assets:       assets,
	}
}

func resolveUniRefFetchVersionToken(clientHTTP *http.Client, value string, baseURLCurrentRelease string) (string, error) {
	return resolveUniProtFetchVersionToken(clientHTTP, value, baseURLCurrentRelease, unirefLabel)
}

func normalizeUniRefFixedVersionToken(value string) (string, error) {
	return normalizeUniProtFixedVersionToken(value, unirefLabel)
}

func createDefaultUniRefConfig() unirefConfig {
	cfg := unirefConfig{}
	cfg.RetryMax = 5
	cfg.RetryWait = 3 * time.Second
	cfg.WorkersMax = 1
	cfg.RuleExisting = "skip"
	cfg.baseURLCurrentRelease = uniprotCurrentReleaseBaseURL
	return cfg
}

func validateUniRefConfig(cfg *unirefConfig) error {
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
