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

const kbLabel = "UniProtKB"

var kbAssetFiles = map[string]string{
	"sprot":      "uniprot_sprot.fasta.gz",
	"sprot_dat":  "uniprot_sprot.dat.gz",
	"trembl":     "uniprot_trembl.fasta.gz",
	"trembl_dat": "uniprot_trembl.dat.gz",
	"varsplic":   "uniprot_sprot_varsplic.fasta.gz",
}

type kbConfig struct {
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

type kbLockConfig struct {
	cliopt.DirSnapshotConfig
	cliopt.DryRunConfig
	cliopt.LogConfig
	workersMax int
}

type kbRestoreConfig struct {
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

func runFetchKB(cfg *kbConfig) error {
	assets, err := resolveKBAssets(cfg.assetNames)
	if err != nil {
		return err
	}
	if hasLargeKBAsset(assets) && !cfg.shouldAllowLargeAssets {
		return fmt.Errorf("selected UniProtKB assets include large files; pass --allow-large-downloads to fetch")
	}
	clientHTTP := httpx.NewClient(cfg.ShouldAllowInsecureTLS)
	versionToken, err := resolveKBFetchVersionToken(clientHTTP, cfg.VersionToken, cfg.baseURLCurrentRelease)
	if err != nil {
		return err
	}
	source := buildKBSource(versionToken, buildKBStaticAssets(cfg.baseURLCurrentRelease, assets))
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

func runLockKB(cfg *kbLockConfig) error {
	versionToken, err := cliopt.SnapshotVersionToken(cfg.DirSnapshot)
	if err != nil {
		return err
	}
	versionToken, err = normalizeKBFixedVersionToken(versionToken)
	if err != nil {
		return err
	}
	source := buildKBSource(versionToken, nil)
	_, closeRun, err := logx.StartVersionedRun("biofetch uniprot", "lock", cfg.DirLogs, cfg.DirSnapshot)
	if err != nil {
		return err
	}
	defer closeRun()
	trace := logx.NewStaticAssetTraceSink("biofetch uniprot")
	if err := staticasset.Lock(source, cfg.DirSnapshot, staticasset.Options{
		RuleExisting: "skip",
		RetryMax:     1,
		WorkersMax:   cfg.workersMax,
		ShouldDryRun: cfg.ShouldDryRun,
	}, trace); err != nil {
		logx.Errorf("biofetch uniprot", "lock failed: %v", err)
		return err
	}
	return nil
}

func runRestoreKB(cfg *kbRestoreConfig) error {
	versionToken, err := normalizeKBFixedVersionToken(cfg.VersionToken)
	if err != nil {
		return err
	}
	source := buildKBSource(versionToken, nil)
	trace, closeRun, err := logx.StartSourceRun("biofetch uniprot", "restore", cfg.DirLogs, cfg.DirOut, source)
	if err != nil {
		return err
	}
	defer closeRun()
	if err := staticasset.Sync(source, buildIDMappingOptions(cfg.DirOut, cfg.ExistingRuleConfig, cfg.RetryConfig, cfg.DownloadControlConfig, cfg.InsecureTLSConfig, cfg.DryRunConfig, cfg.ProgressConfig), trace); err != nil {
		logx.Errorf("biofetch uniprot", "restore failed: %v", err)
		return err
	}
	return nil
}

func resolveKBAssets(values []string) ([]string, error) {
	if len(values) == 0 {
		return sortedKBAssetNames(), nil
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
		if _, ok := kbAssetFiles[asset]; !ok {
			unknown = append(unknown, asset)
			continue
		}
		assets = append(assets, asset)
	}
	if hasAll {
		if len(assets) > 0 {
			return nil, fmt.Errorf("assets=all cannot be combined with specific UniProtKB assets")
		}
		return sortedKBAssetNames(), nil
	}
	if len(unknown) > 0 {
		return nil, fmt.Errorf("unknown UniProtKB asset(s): %s; supported: %s", strings.Join(unknown, ", "), strings.Join(sortedKBAssetNames(), ", "))
	}
	if len(assets) == 0 {
		return nil, fmt.Errorf("assets must not be empty")
	}
	return sets.SortedKeys(stringSet(assets)), nil
}

func sortedKBAssetNames() []string {
	names := make([]string, 0, len(kbAssetFiles))
	for name := range kbAssetFiles {
		names = append(names, name)
	}
	return sets.SortedKeys(stringSet(names))
}

func hasLargeKBAsset(assets []string) bool {
	for _, asset := range assets {
		if asset == "trembl" || asset == "trembl_dat" {
			return true
		}
	}
	return false
}

func buildKBStaticAssets(baseURLCurrentRelease string, assets []string) []staticasset.Asset {
	result := make([]staticasset.Asset, 0, len(assets))
	for _, asset := range assets {
		fileName := kbAssetFiles[asset]
		result = append(result, staticasset.Asset{
			Name: asset,
			Path: filepath.ToSlash(filepath.Join("raw", "knowledgebase", "complete", fileName)),
			URL:  buildUniProtCurrentReleaseURL(baseURLCurrentRelease, "knowledgebase", "complete", fileName),
		})
	}
	return result
}

func buildKBSource(versionToken string, assets []staticasset.Asset) staticasset.Source {
	return staticasset.Source{
		Database:     "uniprot",
		Asset:        "kb",
		Source:       "ftp",
		Version:      versionToken,
		VersionToken: versionToken,
		Assets:       assets,
	}
}

func resolveKBFetchVersionToken(clientHTTP *http.Client, value string, baseURLCurrentRelease string) (string, error) {
	return resolveUniProtFetchVersionToken(clientHTTP, value, baseURLCurrentRelease, kbLabel)
}

func normalizeKBFixedVersionToken(value string) (string, error) {
	return normalizeUniProtFixedVersionToken(value, kbLabel)
}

func createDefaultKBConfig() kbConfig {
	cfg := kbConfig{}
	cfg.RetryMax = 5
	cfg.RetryWait = 3 * time.Second
	cfg.WorkersMax = 1
	cfg.RuleExisting = "skip"
	cfg.baseURLCurrentRelease = uniprotCurrentReleaseBaseURL
	return cfg
}

func validateKBConfig(cfg *kbConfig) error {
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
