package interpro

import (
	"biofetch/internal/shared/cliopt"
	"biofetch/internal/shared/httpx"
	"biofetch/internal/shared/logx"
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

const (
	mappingLabel                 = "InterPro mapping"
	interproCurrentVersion       = "current"
	defaultCurrentReleaseBaseURL = "https://ftp.ebi.ac.uk/pub/databases/interpro/current_release/"
)

var patternInterProRelease = regexp.MustCompile(`(?m)^Release ([0-9]+(?:\.[0-9]+)?)\b`)
var patternInterProVersion = regexp.MustCompile(`^[0-9]+(?:\.[0-9]+)?$`)

var mappingAssetFiles = map[string]string{
	"entries":     "interpro.xml.gz",
	"protein2ipr": "protein2ipr.dat.gz",
}

type mappingConfig struct {
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

type mappingLockConfig struct {
	cliopt.DirSnapshotConfig
	cliopt.DryRunConfig
	cliopt.LogConfig
	workersMax int
}

type mappingRestoreConfig struct {
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

func runFetchMapping(cfg *mappingConfig) error {
	assets, err := resolveMappingAssets(cfg.assetNames)
	if err != nil {
		return err
	}
	if hasLargeMappingAsset(assets) && !cfg.shouldAllowLargeAssets {
		return fmt.Errorf("selected InterPro mapping assets include large files; pass --allow-large-downloads to fetch")
	}
	clientHTTP := httpx.NewClient(cfg.ShouldAllowInsecureTLS)
	versionToken, err := resolveMappingFetchVersionToken(clientHTTP, cfg.VersionToken, cfg.baseURLCurrentRelease)
	if err != nil {
		return err
	}
	source := buildMappingSource(versionToken, buildMappingStaticAssets(cfg.baseURLCurrentRelease, assets))
	trace, closeRun, err := logx.StartSourceRun("biofetch interpro", "fetch", cfg.DirLogs, cfg.DirOut, source)
	if err != nil {
		return err
	}
	defer closeRun()
	if err := staticasset.Fetch(source, buildMappingOptions(cfg.DirOut, cfg.ExistingRuleConfig, cfg.RetryConfig, cfg.DownloadControlConfig, cfg.InsecureTLSConfig, cfg.DryRunConfig, cfg.ProgressConfig), trace); err != nil {
		logx.Errorf("biofetch interpro", "fetch failed: %v", err)
		return err
	}
	return nil
}

func runLockMapping(cfg *mappingLockConfig) error {
	versionToken, err := cliopt.SnapshotVersionToken(cfg.DirSnapshot)
	if err != nil {
		return err
	}
	versionToken, err = normalizeMappingFixedVersionToken(versionToken)
	if err != nil {
		return err
	}
	source := buildMappingSource(versionToken, nil)
	_, closeRun, err := logx.StartVersionedRun("biofetch interpro", "lock", cfg.DirLogs, cfg.DirSnapshot)
	if err != nil {
		return err
	}
	defer closeRun()
	trace := logx.NewStaticAssetTraceSink("biofetch interpro")
	if err := staticasset.Lock(source, cfg.DirSnapshot, staticasset.Options{
		RuleExisting: "skip",
		RetryMax:     1,
		WorkersMax:   cfg.workersMax,
		ShouldDryRun: cfg.ShouldDryRun,
	}, trace); err != nil {
		logx.Errorf("biofetch interpro", "lock failed: %v", err)
		return err
	}
	return nil
}

func runRestoreMapping(cfg *mappingRestoreConfig) error {
	versionToken, err := normalizeMappingFixedVersionToken(cfg.VersionToken)
	if err != nil {
		return err
	}
	source := buildMappingSource(versionToken, nil)
	trace, closeRun, err := logx.StartSourceRun("biofetch interpro", "restore", cfg.DirLogs, cfg.DirOut, source)
	if err != nil {
		return err
	}
	defer closeRun()
	if err := staticasset.Sync(source, buildMappingOptions(cfg.DirOut, cfg.ExistingRuleConfig, cfg.RetryConfig, cfg.DownloadControlConfig, cfg.InsecureTLSConfig, cfg.DryRunConfig, cfg.ProgressConfig), trace); err != nil {
		logx.Errorf("biofetch interpro", "restore failed: %v", err)
		return err
	}
	return nil
}

func resolveMappingAssets(values []string) ([]string, error) {
	if len(values) == 0 {
		return sortedMappingAssetNames(), nil
	}
	valuesResolved, err := cliopt.ExpandListTokens(values, "", "assets")
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
		if _, ok := mappingAssetFiles[asset]; !ok {
			unknown = append(unknown, asset)
			continue
		}
		assets = append(assets, asset)
	}
	if hasAll {
		if len(assets) > 0 {
			return nil, fmt.Errorf("assets=all cannot be combined with specific InterPro mapping assets")
		}
		return sortedMappingAssetNames(), nil
	}
	if len(unknown) > 0 {
		return nil, fmt.Errorf("unknown InterPro mapping asset(s): %s; supported: %s", strings.Join(unknown, ", "), strings.Join(sortedMappingAssetNames(), ", "))
	}
	if len(assets) == 0 {
		return nil, fmt.Errorf("assets must not be empty")
	}
	return sets.SortedKeys(stringSet(assets)), nil
}

func sortedMappingAssetNames() []string {
	names := make([]string, 0, len(mappingAssetFiles))
	for name := range mappingAssetFiles {
		names = append(names, name)
	}
	return sets.SortedKeys(stringSet(names))
}

func hasLargeMappingAsset(assets []string) bool {
	for _, asset := range assets {
		if asset == "protein2ipr" {
			return true
		}
	}
	return false
}

func buildMappingStaticAssets(baseURLCurrentRelease string, assets []string) []staticasset.Asset {
	result := make([]staticasset.Asset, 0, len(assets))
	for _, asset := range assets {
		fileName := mappingAssetFiles[asset]
		result = append(result, staticasset.Asset{
			Name: asset,
			Path: filepath.ToSlash(filepath.Join("raw", fileName)),
			URL:  buildCurrentReleaseURL(baseURLCurrentRelease, fileName),
		})
	}
	return result
}

func buildMappingSource(versionToken string, assets []staticasset.Asset) staticasset.Source {
	return staticasset.Source{
		Database:     "interpro",
		Asset:        "mapping",
		Source:       "ftp",
		Version:      versionToken,
		VersionToken: versionToken,
		Assets:       assets,
	}
}

func resolveMappingFetchVersionToken(clientHTTP *http.Client, value string, baseURLCurrentRelease string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		value = interproCurrentVersion
	}
	if strings.EqualFold(value, interproCurrentVersion) {
		return resolveCurrentVersionToken(clientHTTP, baseURLCurrentRelease)
	}
	return normalizeMappingFixedVersionToken(value)
}

func normalizeMappingFixedVersionToken(value string) (string, error) {
	value = strings.TrimSpace(value)
	if strings.EqualFold(value, interproCurrentVersion) {
		return "", fmt.Errorf("%s version must be a fixed release token for this operation, not current", mappingLabel)
	}
	if !patternInterProVersion.MatchString(value) {
		return "", fmt.Errorf("%s version must look like 108.0: %s", mappingLabel, value)
	}
	return value, nil
}

func resolveCurrentVersionToken(clientHTTP *http.Client, baseURLCurrentRelease string) (string, error) {
	request, err := http.NewRequest(http.MethodGet, buildCurrentReleaseURL(baseURLCurrentRelease, "release_notes.txt"), nil)
	if err != nil {
		return "", fmt.Errorf("build InterPro current release request: %w", err)
	}
	request.Header.Set("Accept", "text/plain")
	response, err := clientHTTP.Do(request)
	if err != nil {
		return "", fmt.Errorf("resolve InterPro current release version: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return "", fmt.Errorf("resolve InterPro current release version: unexpected status %s", response.Status)
	}
	data, err := io.ReadAll(response.Body)
	if err != nil {
		return "", fmt.Errorf("read InterPro current release version: %w", err)
	}
	return parseReleaseNotes(data)
}

func parseReleaseNotes(data []byte) (string, error) {
	matches := patternInterProRelease.FindSubmatch(data)
	if len(matches) != 2 {
		return "", fmt.Errorf("parse InterPro current release version: release token not found")
	}
	return string(matches[1]), nil
}

func buildCurrentReleaseURL(baseURL string, pathParts ...string) string {
	url := strings.TrimRight(baseURL, "/")
	for _, part := range pathParts {
		part = strings.Trim(part, "/")
		if part == "" {
			continue
		}
		url += "/" + part
	}
	return url
}

func createDefaultMappingConfig() mappingConfig {
	cfg := mappingConfig{}
	cfg.RetryMax = 5
	cfg.RetryWait = 3 * time.Second
	cfg.WorkersMax = 1
	cfg.RuleExisting = "skip"
	cfg.baseURLCurrentRelease = defaultCurrentReleaseBaseURL
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
	result := make(map[string]struct{}, len(values))
	for _, value := range values {
		result[value] = struct{}{}
	}
	return result
}
