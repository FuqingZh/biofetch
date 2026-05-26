package reactome

import (
	"biofetch/internal/shared/cliopt"
	"biofetch/internal/shared/httpx"
	"biofetch/internal/shared/sets"
	"biofetch/internal/shared/staticasset"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

const mappingDefaultVersionToken = "current"
const mappingLargeDownloadThresholdBytes = 100 * 1024 * 1024

var mappingCurrentBaseURL = "https://reactome.org/download/current/"
var mappingCurrentVersionURL = "https://reactome.org/ContentService/data/database/version"
var patternMappingVersion = regexp.MustCompile(`(?i)^v?([0-9]+)$`)

var mappingAssetsSupported = []string{
	"ChEBI2Reactome.txt",
	"ChEBI2ReactomeReactions.txt",
	"ChEBI2Reactome_All_Levels.txt",
	"Complex_2_Pathway_human.txt",
	"Ewas2Pathway_human.txt",
	"GtoP2Reactome.txt",
	"GtoP2ReactomeReactions.txt",
	"GtoP2Reactome_All_Levels.txt",
	"NCBI2Reactome.txt",
	"NCBI2ReactomeReactions.txt",
	"NCBI2Reactome_All_Levels.txt",
	"ReactomePathways.gmt.zip",
	"ReactomePathways.txt",
	"ReactomePathwaysRelation.txt",
	"UniProt2Reactome.txt",
	"UniProt2ReactomeReactions.txt",
	"UniProt2Reactome_All_Levels.txt",
}

type mappingConfig struct {
	cliopt.DirOutConfig
	cliopt.VersionConfig
	cliopt.ExistingRuleConfig
	cliopt.RetryConfig
	cliopt.DownloadControlConfig
	cliopt.InsecureTLSConfig
	cliopt.DryRunConfig
	cliopt.ProgressConfig
	assetNames               []string
	shouldAllowLargeDownload bool
}

type mappingLockConfig struct {
	cliopt.DirOutConfig
	cliopt.VersionConfig
	cliopt.DryRunConfig
}

type mappingSyncConfig struct {
	cliopt.DirOutConfig
	cliopt.VersionConfig
	cliopt.ExistingRuleConfig
	cliopt.RetryConfig
	cliopt.DownloadControlConfig
	cliopt.InsecureTLSConfig
	cliopt.DryRunConfig
	cliopt.ProgressConfig
}

func runFetchMapping(cfg *mappingConfig) error {
	assets, err := resolveMappingAssets(cfg.assetNames)
	if err != nil {
		return err
	}
	clientHTTP := httpx.NewClient(cfg.ShouldAllowInsecureTLS)
	versionToken, err := resolveMappingFetchVersionToken(clientHTTP, cfg.VersionToken)
	if err != nil {
		return err
	}
	assetsStatic := buildMappingStaticAssets(mappingCurrentBaseURL, assets)
	if !cfg.ShouldDryRun && !cfg.shouldAllowLargeDownload {
		if err := validateMappingDownloadSizes(clientHTTP, assetsStatic, mappingLargeDownloadThresholdBytes); err != nil {
			return err
		}
	}
	source := buildMappingSource(versionToken, assetsStatic)
	return staticasset.Fetch(source, buildMappingOptions(cfg.DirOut, cfg.ExistingRuleConfig, cfg.RetryConfig, cfg.DownloadControlConfig, cfg.InsecureTLSConfig, cfg.DryRunConfig, cfg.ProgressConfig), nil)
}

func runLockMapping(cfg *mappingLockConfig) error {
	versionToken, err := normalizeMappingFixedVersionToken(cfg.VersionToken)
	if err != nil {
		return err
	}
	return staticasset.Lock(buildMappingSource(versionToken, nil), staticasset.Options{
		DirOut:       cfg.DirOut,
		RuleExisting: "skip",
		RetryMax:     1,
		WorkersMax:   1,
		ShouldDryRun: cfg.ShouldDryRun,
	}, nil)
}

func runSyncMapping(cfg *mappingSyncConfig) error {
	versionToken, err := normalizeMappingFixedVersionToken(cfg.VersionToken)
	if err != nil {
		return err
	}
	return staticasset.Sync(buildMappingSource(versionToken, nil), buildMappingOptions(cfg.DirOut, cfg.ExistingRuleConfig, cfg.RetryConfig, cfg.DownloadControlConfig, cfg.InsecureTLSConfig, cfg.DryRunConfig, cfg.ProgressConfig), nil)
}

func resolveMappingAssets(values []string) ([]string, error) {
	if len(values) == 0 {
		return nil, fmt.Errorf("assets must not be empty")
	}
	valuesResolved, err := cliopt.ExpandAtFileTokens(values, "assets")
	if err != nil {
		return nil, err
	}
	supported := stringSet(mappingAssetsSupported)
	selected := make([]string, 0, len(valuesResolved))
	unknown := make([]string, 0)
	for _, value := range valuesResolved {
		asset := strings.TrimSpace(value)
		if asset == "" {
			continue
		}
		if _, ok := supported[asset]; !ok {
			unknown = append(unknown, asset)
			continue
		}
		selected = append(selected, asset)
	}
	if len(unknown) > 0 {
		return nil, fmt.Errorf("unknown Reactome mapping asset(s): %s; supported: %s", strings.Join(unknown, ", "), strings.Join(mappingAssetsSupported, ", "))
	}
	if len(selected) == 0 {
		return nil, fmt.Errorf("assets must not be empty")
	}
	return sets.SortedKeys(stringSet(selected)), nil
}

func buildMappingStaticAssets(baseURL string, assets []string) []staticasset.Asset {
	result := make([]staticasset.Asset, 0, len(assets))
	for _, asset := range assets {
		result = append(result, staticasset.Asset{
			Name: asset,
			Path: filepath.ToSlash(filepath.Join("raw", asset)),
			URL:  strings.TrimRight(baseURL, "/") + "/" + asset,
		})
	}
	return result
}

func validateMappingDownloadSizes(clientHTTP *http.Client, assets []staticasset.Asset, thresholdBytes int64) error {
	for _, asset := range assets {
		bytes, ok, err := resolveContentLength(clientHTTP, asset.URL)
		if err != nil {
			return err
		}
		if ok && bytes > thresholdBytes {
			return fmt.Errorf("Reactome mapping asset %s is %d bytes, above threshold %d; pass --should_allow_large_download to fetch", asset.Name, bytes, thresholdBytes)
		}
	}
	return nil
}

func resolveContentLength(clientHTTP *http.Client, urlFile string) (int64, bool, error) {
	request, err := http.NewRequest(http.MethodHead, urlFile, nil)
	if err != nil {
		return 0, false, fmt.Errorf("build HEAD request %s: %w", urlFile, err)
	}
	response, err := clientHTTP.Do(request)
	if err != nil {
		return 0, false, fmt.Errorf("request content length %s: %w", urlFile, err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return 0, false, fmt.Errorf("request content length %s: unexpected status %s", urlFile, response.Status)
	}
	if response.ContentLength < 0 {
		return 0, false, nil
	}
	return response.ContentLength, true, nil
}

func buildMappingSource(versionToken string, assets []staticasset.Asset) staticasset.Source {
	return staticasset.Source{
		Database:     "reactome",
		Asset:        "mapping",
		Version:      versionToken,
		VersionToken: versionToken,
		Assets:       assets,
	}
}

func resolveMappingFetchVersionToken(clientHTTP *http.Client, value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		value = mappingDefaultVersionToken
	}
	if strings.EqualFold(value, mappingDefaultVersionToken) {
		return resolveMappingCurrentVersionToken(clientHTTP)
	}
	return normalizeMappingFixedVersionToken(value)
}

func normalizeMappingFixedVersionToken(value string) (string, error) {
	value = strings.TrimSpace(value)
	if strings.EqualFold(value, mappingDefaultVersionToken) {
		return "", fmt.Errorf("Reactome mapping version must be a fixed release token for this operation, not current")
	}
	matches := patternMappingVersion.FindStringSubmatch(value)
	if len(matches) != 2 {
		return "", fmt.Errorf("Reactome mapping version must look like v96 or 96: %s", value)
	}
	return "v" + matches[1], nil
}

func resolveMappingCurrentVersionToken(clientHTTP *http.Client) (string, error) {
	request, err := http.NewRequest(http.MethodGet, mappingCurrentVersionURL, nil)
	if err != nil {
		return "", fmt.Errorf("build Reactome current version request: %w", err)
	}
	request.Header.Set("Accept", "text/plain")
	response, err := clientHTTP.Do(request)
	if err != nil {
		return "", fmt.Errorf("resolve Reactome current release version: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return "", fmt.Errorf("resolve Reactome current release version: unexpected status %s", response.Status)
	}
	data, err := io.ReadAll(response.Body)
	if err != nil {
		return "", fmt.Errorf("read Reactome current release version: %w", err)
	}
	versionToken, err := normalizeMappingFixedVersionToken(string(data))
	if err != nil {
		return "", fmt.Errorf("parse Reactome current release version %q: %w", strings.TrimSpace(string(data)), err)
	}
	return versionToken, nil
}

func createDefaultMappingConfig() mappingConfig {
	cfg := mappingConfig{}
	cfg.RetryMax = 5
	cfg.RetryWait = 3 * time.Second
	cfg.WorkersMax = 1
	cfg.RuleExisting = "skip"
	return cfg
}

func validateMappingConfig(cfg *mappingConfig) error {
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

func buildMappingOptions(
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
