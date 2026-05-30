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

const kbLabel = "UniProtKB"

var kbAssetFiles = map[string]string{
	"sprot":    "uniprot_sprot.fasta.gz",
	"trembl":   "uniprot_trembl.fasta.gz",
	"varsplic": "uniprot_sprot_varsplic.fasta.gz",
}

type kbConfig struct {
	cliopt.DirOutConfig
	cliopt.VersionConfig
	cliopt.ExistingRuleConfig
	cliopt.RetryConfig
	cliopt.DownloadControlConfig
	cliopt.InsecureTLSConfig
	cliopt.DryRunConfig
	cliopt.ProgressConfig
	assetNames               []string
	baseURLCurrentRelease    string
	shouldAllowLargeDownload bool
}

type kbLockConfig struct {
	cliopt.DirOutConfig
	cliopt.VersionConfig
	cliopt.DryRunConfig
}

type kbSyncConfig struct {
	cliopt.DirOutConfig
	cliopt.VersionConfig
	cliopt.ExistingRuleConfig
	cliopt.RetryConfig
	cliopt.DownloadControlConfig
	cliopt.InsecureTLSConfig
	cliopt.DryRunConfig
	cliopt.ProgressConfig
}

func runFetchKB(cfg *kbConfig) error {
	assets, err := resolveKBAssets(cfg.assetNames)
	if err != nil {
		return err
	}
	if hasLargeKBAsset(assets) && !cfg.shouldAllowLargeDownload {
		return fmt.Errorf("UniProtKB TrEMBL FASTA is a large download; pass --should_allow_large_download to fetch")
	}
	clientHTTP := httpx.NewClient(cfg.ShouldAllowInsecureTLS)
	versionToken, err := resolveKBFetchVersionToken(clientHTTP, cfg.VersionToken, cfg.baseURLCurrentRelease)
	if err != nil {
		return err
	}
	source := buildKBSource(versionToken, buildKBStaticAssets(cfg.baseURLCurrentRelease, assets))
	return staticasset.Fetch(source, buildIDMappingOptions(cfg.DirOut, cfg.ExistingRuleConfig, cfg.RetryConfig, cfg.DownloadControlConfig, cfg.InsecureTLSConfig, cfg.DryRunConfig, cfg.ProgressConfig), nil)
}

func runLockKB(cfg *kbLockConfig) error {
	versionToken, err := normalizeKBFixedVersionToken(cfg.VersionToken)
	if err != nil {
		return err
	}
	return staticasset.Lock(buildKBSource(versionToken, nil), staticasset.Options{
		DirOut:       cfg.DirOut,
		RuleExisting: "skip",
		RetryMax:     1,
		WorkersMax:   1,
		ShouldDryRun: cfg.ShouldDryRun,
	}, nil)
}

func runSyncKB(cfg *kbSyncConfig) error {
	versionToken, err := normalizeKBFixedVersionToken(cfg.VersionToken)
	if err != nil {
		return err
	}
	return staticasset.Sync(buildKBSource(versionToken, nil), buildIDMappingOptions(cfg.DirOut, cfg.ExistingRuleConfig, cfg.RetryConfig, cfg.DownloadControlConfig, cfg.InsecureTLSConfig, cfg.DryRunConfig, cfg.ProgressConfig), nil)
}

func resolveKBAssets(values []string) ([]string, error) {
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
		if _, ok := kbAssetFiles[asset]; !ok {
			unknown = append(unknown, asset)
			continue
		}
		assets = append(assets, asset)
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
		if asset == "trembl" {
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
