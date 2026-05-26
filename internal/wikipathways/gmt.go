package wikipathways

import (
	"biofetch/internal/shared/cliopt"
	"biofetch/internal/shared/confirm"
	"biofetch/internal/shared/httpx"
	"biofetch/internal/shared/sets"
	"biofetch/internal/shared/staticasset"
	"bytes"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"golang.org/x/net/html"
)

var gmtCurrentBaseURL = "https://data.wikipathways.org/current/gmt/"

var patternGMTFile = regexp.MustCompile(`^wikipathways-([0-9]{8})-gmt-([A-Za-z0-9_]+)\.gmt$`)

type gmtConfig struct {
	cliopt.DirOutConfig
	cliopt.VersionConfig
	cliopt.ExistingRuleConfig
	cliopt.RetryConfig
	cliopt.DownloadControlConfig
	cliopt.InsecureTLSConfig
	cliopt.DryRunConfig
	speciesNames      []string
	shouldDownloadAll bool
}

type gmtLockConfig struct {
	cliopt.DirOutConfig
	cliopt.VersionConfig
	cliopt.DryRunConfig
}

type gmtSyncConfig struct {
	cliopt.DirOutConfig
	cliopt.VersionConfig
	cliopt.ExistingRuleConfig
	cliopt.RetryConfig
	cliopt.DownloadControlConfig
	cliopt.InsecureTLSConfig
	cliopt.DryRunConfig
}

type gmtAsset struct {
	species      string
	fileName     string
	versionToken string
	url          string
}

func runFetchGMT(cfg *gmtConfig, readerConfirm io.Reader, writerConfirm io.Writer) error {
	if err := validateGMTVersionToken(cfg.VersionToken); err != nil {
		return err
	}
	clientHTTP := httpx.NewClient(cfg.ShouldAllowInsecureTLS)
	limiterRequest := httpx.NewRequestLimiter(cfg.RequestInterval)
	assetsAvailable, err := discoverGMTAssets(clientHTTP, gmtCurrentBaseURL, limiterRequest)
	if err != nil {
		return err
	}
	assetsSelected, err := resolveGMTAssets(assetsAvailable, cfg.speciesNames, cfg.shouldDownloadAll)
	if err != nil {
		return err
	}
	if cfg.shouldDownloadAll {
		if err := confirmAllGMTDownload(readerConfirm, writerConfirm); err != nil {
			return err
		}
	}
	versionToken := deriveGMTVersionToken(assetsSelected)
	source := buildGMTSource(versionToken, assetsSelected)
	return staticasset.Fetch(source, buildGMTOptions(cfg.DirOut, cfg.ExistingRuleConfig, cfg.RetryConfig, cfg.DownloadControlConfig, cfg.InsecureTLSConfig, cfg.DryRunConfig), nil)
}

func runLockGMT(cfg *gmtLockConfig) error {
	return staticasset.Lock(buildGMTSource(cfg.VersionToken, nil), staticasset.Options{
		DirOut:       cfg.DirOut,
		RuleExisting: "skip",
		RetryMax:     1,
		WorkersMax:   1,
		ShouldDryRun: cfg.ShouldDryRun,
	}, nil)
}

func runSyncGMT(cfg *gmtSyncConfig) error {
	return staticasset.Sync(buildGMTSource(cfg.VersionToken, nil), buildGMTOptions(cfg.DirOut, cfg.ExistingRuleConfig, cfg.RetryConfig, cfg.DownloadControlConfig, cfg.InsecureTLSConfig, cfg.DryRunConfig), nil)
}

func discoverGMTAssets(clientHTTP *http.Client, baseURL string, limiterRequest *httpx.RequestLimiter) ([]gmtAsset, error) {
	data, err := downloadText(clientHTTP, buildGMTIndexURL(baseURL), limiterRequest)
	if err != nil {
		return nil, err
	}
	return parseGMTAssetsFromIndex(data, baseURL)
}

func parseGMTAssetsFromIndex(data []byte, baseURL string) ([]gmtAsset, error) {
	document, err := html.Parse(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("parse WikiPathways GMT index html: %w", err)
	}
	assetsBySpecies := map[string]gmtAsset{}
	for _, target := range extractAnchorTargets(document) {
		fileName := filepath.Base(strings.TrimSpace(target))
		match := patternGMTFile.FindStringSubmatch(fileName)
		if match == nil {
			continue
		}
		versionToken := formatGMTVersionToken(match[1])
		species := match[2]
		assetsBySpecies[species] = gmtAsset{
			species:      species,
			fileName:     fileName,
			versionToken: versionToken,
			url:          strings.TrimRight(baseURL, "/") + "/" + fileName,
		}
	}
	if len(assetsBySpecies) == 0 {
		return nil, fmt.Errorf("no WikiPathways GMT assets found at %s", buildGMTIndexURL(baseURL))
	}
	species := make([]string, 0, len(assetsBySpecies))
	for item := range assetsBySpecies {
		species = append(species, item)
	}
	sort.Strings(species)
	assets := make([]gmtAsset, 0, len(species))
	for _, item := range species {
		assets = append(assets, assetsBySpecies[item])
	}
	return assets, nil
}

func resolveGMTAssets(assetsAvailable []gmtAsset, speciesNames []string, shouldDownloadAll bool) ([]gmtAsset, error) {
	if shouldDownloadAll {
		if len(speciesNames) > 0 {
			return nil, fmt.Errorf("species cannot be combined with should_download_all")
		}
		return assetsAvailable, nil
	}
	speciesRequested, err := parseGMTSpecies(speciesNames)
	if err != nil {
		return nil, err
	}
	assetsBySpecies := make(map[string]gmtAsset, len(assetsAvailable))
	for _, asset := range assetsAvailable {
		assetsBySpecies[asset.species] = asset
	}
	assets := make([]gmtAsset, 0, len(speciesRequested))
	unknown := make([]string, 0)
	for _, species := range speciesRequested {
		asset, ok := assetsBySpecies[species]
		if !ok {
			unknown = append(unknown, species)
			continue
		}
		assets = append(assets, asset)
	}
	if len(unknown) > 0 {
		return nil, fmt.Errorf("unknown WikiPathways species: %s; available_species=%d", strings.Join(unknown, ", "), len(assetsAvailable))
	}
	return assets, nil
}

func parseGMTSpecies(values []string) ([]string, error) {
	if len(values) == 0 {
		return nil, fmt.Errorf("species must be provided unless should_download_all is set")
	}
	valuesResolved, err := cliopt.ExpandAtFileTokens(values, "species")
	if err != nil {
		return nil, err
	}
	species := make([]string, 0, len(valuesResolved))
	for _, value := range valuesResolved {
		item := strings.TrimSpace(value)
		if item != "" {
			species = append(species, item)
		}
	}
	if len(species) == 0 {
		return nil, fmt.Errorf("species must not be empty")
	}
	return sets.SortedKeys(stringSet(species)), nil
}

func buildGMTSource(versionToken string, assets []gmtAsset) staticasset.Source {
	return staticasset.Source{
		Database:     "wikipathways",
		Asset:        "gmt",
		Version:      versionToken,
		VersionToken: versionToken,
		Assets:       buildGMTStaticAssets(assets),
	}
}

func buildGMTStaticAssets(assets []gmtAsset) []staticasset.Asset {
	result := make([]staticasset.Asset, 0, len(assets))
	for _, asset := range assets {
		result = append(result, staticasset.Asset{
			Name: asset.species,
			Path: filepath.ToSlash(filepath.Join("raw", asset.species, asset.fileName)),
			URL:  asset.url,
		})
	}
	return result
}

func deriveGMTVersionToken(assets []gmtAsset) string {
	setVersions := make(map[string]struct{}, len(assets))
	for _, asset := range assets {
		setVersions[asset.versionToken] = struct{}{}
	}
	versions := sets.SortedKeys(setVersions)
	if len(versions) == 1 {
		return versions[0]
	}
	return strings.Join(versions, "_")
}

func validateGMTVersionToken(versionToken string) error {
	versionToken = strings.TrimSpace(versionToken)
	if versionToken == "" || versionToken == "current" {
		return nil
	}
	return fmt.Errorf("historical WikiPathways GMT version %q is not implemented; use current/empty version", versionToken)
}

func formatGMTVersionToken(value string) string {
	if len(value) != 8 {
		return value
	}
	return value[0:4] + "-" + value[4:6] + "-" + value[6:8]
}

func buildGMTIndexURL(baseURL string) string {
	return strings.TrimRight(baseURL, "/") + "/"
}

func extractAnchorTargets(root *html.Node) []string {
	setTargets := make(map[string]struct{})
	visitAnchorNodes(root, setTargets)
	return sets.SortedKeys(setTargets)
}

func visitAnchorNodes(node *html.Node, setTargets map[string]struct{}) {
	if node == nil {
		return
	}
	if node.Type == html.ElementNode && node.Data == "a" {
		for _, attr := range node.Attr {
			if attr.Key == "href" && strings.TrimSpace(attr.Val) != "" {
				setTargets[strings.TrimSpace(attr.Val)] = struct{}{}
			}
		}
	}
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		visitAnchorNodes(child, setTargets)
	}
}

func downloadText(clientHTTP *http.Client, urlFile string, limiterRequest *httpx.RequestLimiter) ([]byte, error) {
	limiterRequest.Wait()
	response, err := clientHTTP.Get(urlFile)
	if err != nil {
		return nil, fmt.Errorf("request %s: %w", urlFile, err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, fmt.Errorf("request %s: unexpected status %s", urlFile, response.Status)
	}
	data, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", urlFile, err)
	}
	return data, nil
}

func confirmAllGMTDownload(reader io.Reader, writer io.Writer) error {
	return confirm.RequireLiteral(
		reader,
		writer,
		"Full WikiPathways GMT download may fetch all species in the current release.",
		"all_species",
	)
}

func createDefaultGMTConfig() gmtConfig {
	cfg := gmtConfig{}
	cfg.RetryMax = 5
	cfg.RetryWait = 3 * time.Second
	cfg.WorkersMax = 1
	cfg.RuleExisting = "skip"
	return cfg
}

func validateGMTConfig(cfg *gmtConfig) error {
	if err := cliopt.ValidateDirOutRequired(cfg.DirOut); err != nil {
		return err
	}
	if err := validateGMTVersionToken(cfg.VersionToken); err != nil {
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

func buildGMTOptions(
	dirOut string,
	cfgExisting cliopt.ExistingRuleConfig,
	cfgRetry cliopt.RetryConfig,
	cfgDownload cliopt.DownloadControlConfig,
	cfgTLS cliopt.InsecureTLSConfig,
	cfgDryRun cliopt.DryRunConfig,
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
	}
}

func stringSet(values []string) map[string]struct{} {
	set := make(map[string]struct{}, len(values))
	for _, value := range values {
		set[value] = struct{}{}
	}
	return set
}

func sortGMTAssets(assets []gmtAsset) {
	sort.Slice(assets, func(i, j int) bool {
		return assets[i].species < assets[j].species
	})
}
