package reactome

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
	"strconv"
	"strings"
	"time"
)

const mappingDefaultVersionToken = "current"
const mappingLargeDownloadThresholdBytes = 100 * 1024 * 1024

var mappingCurrentVersionURL = "https://reactome.org/ContentService/data/database/version"
var mappingReleaseBaseURL = "https://download.reactome.org/%s/"
var reactomeSleep = time.Sleep
var reactomeNow = time.Now
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
	cliopt.LogConfig
	cliopt.ProgressConfig
	assetNames             []string
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
	clientHTTP := httpx.NewClient(cfg.ShouldAllowInsecureTLS)
	versionToken, err := resolveMappingFetchVersionToken(clientHTTP, cfg.VersionToken, cfg.RetryMax, cfg.RetryWait)
	if err != nil {
		return err
	}
	releaseNumber := strings.TrimPrefix(versionToken, "v")
	assetsStatic := buildMappingStaticAssets(fmt.Sprintf(mappingReleaseBaseURL, releaseNumber), assets)
	if !cfg.ShouldDryRun && !cfg.shouldAllowLargeAssets {
		if err := validateMappingDownloadSizes(clientHTTP, assetsStatic, mappingLargeDownloadThresholdBytes); err != nil {
			return err
		}
	}
	source := buildMappingSource(versionToken, assetsStatic)
	trace, closeRun, err := logx.StartSourceRun("biofetch reactome", "fetch", cfg.DirLogs, cfg.DirOut, source)
	if err != nil {
		return err
	}
	defer closeRun()
	if err := staticasset.Fetch(source, buildMappingOptions(cfg.DirOut, cfg.ExistingRuleConfig, cfg.RetryConfig, cfg.DownloadControlConfig, cfg.InsecureTLSConfig, cfg.DryRunConfig, cfg.ProgressConfig), trace); err != nil {
		logx.Errorf("biofetch reactome", "fetch failed: %v", err)
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
	_, closeRun, err := logx.StartVersionedRun("biofetch reactome", "lock", cfg.DirLogs, cfg.DirSnapshot)
	if err != nil {
		return err
	}
	defer closeRun()
	trace := logx.NewStaticAssetTraceSink("biofetch reactome")
	if err := staticasset.Lock(source, cfg.DirSnapshot, staticasset.Options{
		RuleExisting: "skip",
		RetryMax:     1,
		WorkersMax:   cfg.workersMax,
		ShouldDryRun: cfg.ShouldDryRun,
	}, trace); err != nil {
		logx.Errorf("biofetch reactome", "lock failed: %v", err)
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
	trace, closeRun, err := logx.StartSourceRun("biofetch reactome", "restore", cfg.DirLogs, cfg.DirOut, source)
	if err != nil {
		return err
	}
	defer closeRun()
	if err := staticasset.Sync(source, buildMappingOptions(cfg.DirOut, cfg.ExistingRuleConfig, cfg.RetryConfig, cfg.DownloadControlConfig, cfg.InsecureTLSConfig, cfg.DryRunConfig, cfg.ProgressConfig), trace); err != nil {
		logx.Errorf("biofetch reactome", "restore failed: %v", err)
		return err
	}
	return nil
}

func resolveMappingAssets(values []string) ([]string, error) {
	if len(values) == 0 {
		return append([]string(nil), mappingAssetsSupported...), nil
	}
	valuesResolved, err := cliopt.ExpandListTokens(values, "", "assets")
	if err != nil {
		return nil, err
	}
	supported := stringSet(mappingAssetsSupported)
	selected := make([]string, 0, len(valuesResolved))
	unknown := make([]string, 0)
	hasAll := false
	for _, value := range valuesResolved {
		asset := strings.TrimSpace(value)
		if asset == "" {
			continue
		}
		if strings.EqualFold(asset, "all") {
			hasAll = true
			continue
		}
		if _, ok := supported[asset]; !ok {
			unknown = append(unknown, asset)
			continue
		}
		selected = append(selected, asset)
	}
	if hasAll {
		if len(selected) > 0 {
			return nil, fmt.Errorf("assets=all cannot be combined with specific Reactome mapping assets")
		}
		return append([]string(nil), mappingAssetsSupported...), nil
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
			return fmt.Errorf("Reactome mapping asset %s is %d bytes, above threshold %d; pass --allow-large-downloads to fetch", asset.Name, bytes, thresholdBytes)
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

func resolveMappingFetchVersionToken(clientHTTP *http.Client, value string, maxAttempts int, wait time.Duration) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		value = mappingDefaultVersionToken
	}
	if strings.EqualFold(value, mappingDefaultVersionToken) {
		return resolveMappingCurrentVersionToken(clientHTTP, maxAttempts, wait)
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

func resolveMappingCurrentVersionToken(clientHTTP *http.Client, maxAttempts int, retryWait time.Duration) (string, error) {
	if maxAttempts < 1 {
		return "", fmt.Errorf("resolve Reactome current release version endpoint=%s: max attempts must be >= 1", mappingCurrentVersionURL)
	}
	var lastErr error
	status := ""
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		status = "transport-error"
		wait := retryWait
		request, err := http.NewRequest(http.MethodGet, mappingCurrentVersionURL, nil)
		if err != nil {
			return "", fmt.Errorf("build Reactome current version request: %w", err)
		}
		request.Header.Set("Accept", "text/plain")
		response, err := clientHTTP.Do(request)
		if err != nil {
			lastErr = err
		} else {
			status = response.Status
			data, errRead := io.ReadAll(response.Body)
			_ = response.Body.Close()
			if errRead == nil && response.StatusCode >= 200 && response.StatusCode < 300 {
				versionToken, err := normalizeMappingFixedVersionToken(string(data))
				if err != nil {
					return "", fmt.Errorf("parse Reactome current release version %q: %w", strings.TrimSpace(string(data)), err)
				}
				return versionToken, nil
			}
			if errRead != nil {
				lastErr = errRead
			} else {
				lastErr = fmt.Errorf("unexpected status %s", response.Status)
			}
			retryable := response.StatusCode == http.StatusTooManyRequests || response.StatusCode >= 500
			if !retryable {
				return "", fmt.Errorf("resolve Reactome current release version endpoint=%s status=%s attempts=%d: %w",
					mappingCurrentVersionURL, status, attempt, lastErr)
			}
			if strings.TrimSpace(response.Header.Get("Retry-After")) != "" {
				wait = reactomeRetryAfter(response.Header.Get("Retry-After"), retryWait)
			}
		}
		if attempt < maxAttempts {
			reactomeSleep(wait)
		}
	}
	return "", fmt.Errorf("resolve Reactome current release version endpoint=%s status=%s attempts=%d: %w",
		mappingCurrentVersionURL, status, maxAttempts, lastErr)
}

func reactomeRetryAfter(value string, fallback time.Duration) time.Duration {
	if seconds, err := strconv.Atoi(strings.TrimSpace(value)); err == nil && seconds >= 0 {
		wait := time.Duration(seconds) * time.Second
		if wait > fallback {
			return wait
		}
		return fallback
	}
	if when, err := http.ParseTime(value); err == nil {
		if wait := when.Sub(reactomeNow()); wait > 0 {
			if wait > fallback {
				return wait
			}
		}
		return fallback
	}
	return fallback
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
