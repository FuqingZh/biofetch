package uniprot

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

const idMappingDefaultVersionToken = "current"

var idMappingCurrentBaseURL = "https://ftp.uniprot.org/pub/databases/uniprot/current_release/knowledgebase/idmapping/"
var idMappingCurrentReleaseNotesURL = "https://ftp.uniprot.org/pub/databases/uniprot/current_release/relnotes.txt"
var patternUniProtRelease = regexp.MustCompile(`(?m)^UniProt Release ([0-9]{4}_[0-9]{2})\b`)
var patternUniProtVersion = regexp.MustCompile(`^[0-9]{4}_[0-9]{2}$`)

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
	assetNames               []string
	shouldAllowLargeDownload bool
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
	if !cfg.shouldAllowLargeDownload {
		return fmt.Errorf("UniProt ID mapping global assets are multi-GB files; pass --should_allow_large_download to fetch")
	}
	clientHTTP := httpx.NewClient(cfg.ShouldAllowInsecureTLS)
	versionToken, err := resolveIDMappingFetchVersionToken(clientHTTP, cfg.VersionToken)
	if err != nil {
		return err
	}
	source := buildIDMappingSource(versionToken, buildIDMappingStaticAssets(idMappingCurrentBaseURL, assets))
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
		if _, ok := idMappingAssetFiles[asset]; !ok {
			unknown = append(unknown, asset)
			continue
		}
		assets = append(assets, asset)
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

func buildIDMappingStaticAssets(baseURL string, assets []string) []staticasset.Asset {
	result := make([]staticasset.Asset, 0, len(assets))
	for _, asset := range assets {
		fileName := idMappingAssetFiles[asset]
		result = append(result, staticasset.Asset{
			Name: asset,
			Path: filepath.ToSlash(filepath.Join("raw", fileName)),
			URL:  strings.TrimRight(baseURL, "/") + "/" + fileName,
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

func resolveIDMappingFetchVersionToken(clientHTTP *http.Client, value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		value = idMappingDefaultVersionToken
	}
	if strings.EqualFold(value, idMappingDefaultVersionToken) {
		return resolveIDMappingCurrentVersionToken(clientHTTP)
	}
	return normalizeIDMappingFixedVersionToken(value)
}

func normalizeIDMappingFixedVersionToken(value string) (string, error) {
	value = strings.TrimSpace(value)
	if strings.EqualFold(value, idMappingDefaultVersionToken) {
		return "", fmt.Errorf("UniProt ID mapping version must be a fixed release token for this operation, not current")
	}
	if !patternUniProtVersion.MatchString(value) {
		return "", fmt.Errorf("UniProt ID mapping version must look like 2026_01: %s", value)
	}
	return value, nil
}

func resolveIDMappingCurrentVersionToken(clientHTTP *http.Client) (string, error) {
	request, err := http.NewRequest(http.MethodGet, idMappingCurrentReleaseNotesURL, nil)
	if err != nil {
		return "", fmt.Errorf("build UniProt current release request: %w", err)
	}
	request.Header.Set("Accept", "text/plain")
	response, err := clientHTTP.Do(request)
	if err != nil {
		return "", fmt.Errorf("resolve UniProt current release version: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return "", fmt.Errorf("resolve UniProt current release version: unexpected status %s", response.Status)
	}
	data, err := io.ReadAll(response.Body)
	if err != nil {
		return "", fmt.Errorf("read UniProt current release version: %w", err)
	}
	versionToken, err := parseIDMappingReleaseNotes(data)
	if err != nil {
		return "", err
	}
	return versionToken, nil
}

func parseIDMappingReleaseNotes(data []byte) (string, error) {
	matches := patternUniProtRelease.FindSubmatch(data)
	if len(matches) != 2 {
		return "", fmt.Errorf("parse UniProt current release version: release token not found")
	}
	return string(matches[1]), nil
}

func createDefaultIDMappingConfig() idMappingConfig {
	cfg := idMappingConfig{}
	cfg.RetryMax = 5
	cfg.RetryWait = 3 * time.Second
	cfg.WorkersMax = 1
	cfg.RuleExisting = "skip"
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
