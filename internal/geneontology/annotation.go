package geneontology

import (
	"biofetch/internal/shared/cliopt"
	"biofetch/internal/shared/httpx"
	"biofetch/internal/shared/logx"
	"biofetch/internal/shared/sets"
	"biofetch/internal/shared/staticasset"
	"biofetch/internal/shared/tomlx"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

var annotationFormatsSupported = []string{"gaf", "gpad", "gpi"}

var annotationCurrentBaseURL = "https://current.geneontology.org/annotations/"

var annotationHrefPattern = regexp.MustCompile(`(?i)\bhref\s*=\s*["']([^"']+)["']`)

const annotationIndexReadLimitBytes int64 = 4 << 20

type annotationConfig struct {
	cliopt.DirOutConfig
	cliopt.VersionConfig
	cliopt.ExistingRuleConfig
	cliopt.RetryConfig
	cliopt.DownloadControlConfig
	cliopt.InsecureTLSConfig
	cliopt.DryRunConfig
	cliopt.LogConfig
	cliopt.ProgressConfig
	version      string
	datasetNames []string
	formatNames  []string
}

type annotationLockConfig struct {
	cliopt.DirSnapshotConfig
	cliopt.DryRunConfig
	cliopt.LogConfig
	workersMax int
}

type annotationRestoreConfig struct {
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

type annotationSource struct {
	version      string
	versionToken string
	baseURL      string
}

func runFetchAnnotation(cfg *annotationConfig) error {
	clientHTTP := httpx.NewClient(cfg.ShouldAllowInsecureTLS)
	limiterRequest := httpx.NewRequestLimiter(cfg.RequestInterval)
	source, err := resolveAnnotationSource(clientHTTP, cfg.VersionToken, limiterRequest)
	if err != nil {
		return err
	}
	formats, err := resolveAnnotationFormats(cfg.formatNames)
	if err != nil {
		return err
	}
	datasets, formatsResolved, assets, err := resolveAnnotationAssetSelection(clientHTTP, source, cfg.datasetNames, formats, limiterRequest)
	if err != nil {
		return err
	}
	cfg.version = source.version
	cfg.VersionToken = source.versionToken

	sourceStatic := buildAnnotationStaticSource(source, datasets, formatsResolved, assets)
	trace, closeRun, err := logx.StartSourceRun("biofetch go", "fetch", cfg.DirLogs, cfg.DirOut, sourceStatic)
	if err != nil {
		return err
	}
	defer closeRun()
	if err := staticasset.Fetch(sourceStatic, buildAnnotationOptions(cfg.DirOut, cfg.ExistingRuleConfig, cfg.RetryConfig, cfg.DownloadControlConfig, cfg.InsecureTLSConfig, cfg.DryRunConfig, cfg.ProgressConfig), trace); err != nil {
		logx.Errorf("biofetch go", "fetch failed: %v", err)
		return err
	}
	return nil
}

func runLockAnnotation(cfg *annotationLockConfig) error {
	versionToken, err := cliopt.SnapshotVersionToken(cfg.DirSnapshot)
	if err != nil {
		return err
	}
	dirVersion := cfg.DirSnapshot
	if !cfg.ShouldDryRun {
		if err := validateAnnotationRawFiles(dirVersion); err != nil {
			return err
		}
	}
	source := staticasset.Source{
		Database:     "go",
		Asset:        "annotation",
		Source:       "geneontology",
		Version:      versionToken,
		VersionToken: versionToken,
	}
	_, closeRun, err := logx.StartVersionedRun("biofetch go", "lock", cfg.DirLogs, dirVersion)
	if err != nil {
		return err
	}
	defer closeRun()
	trace := logx.NewStaticAssetTraceSink("biofetch go")
	if err := staticasset.Lock(source, dirVersion, staticasset.Options{
		RuleExisting: "skip",
		RetryMax:     1,
		WorkersMax:   cfg.workersMax,
		ShouldDryRun: cfg.ShouldDryRun,
	}, trace); err != nil {
		logx.Errorf("biofetch go", "lock failed: %v", err)
		return err
	}
	if !cfg.ShouldDryRun {
		if err := finalizeAnnotationLockManifest(filepath.Join(dirVersion, "manifest.lock"), versionToken); err != nil {
			return err
		}
	}
	return nil
}

func runRestoreAnnotation(cfg *annotationRestoreConfig) error {
	manifestExisting, ok, err := staticasset.ReadManifest(filepath.Join(cfg.DirOut, "annotation", cfg.VersionToken, "manifest.lock"))
	if err != nil {
		return err
	}
	source := staticasset.Source{
		Database:     "go",
		Asset:        "annotation",
		Source:       "geneontology",
		Version:      cfg.VersionToken,
		VersionToken: cfg.VersionToken,
	}
	if ok {
		if manifestExisting.Version != "" {
			source.Version = manifestExisting.Version
		}
		source.Scope = manifestExisting.Scope
		if source.Scope.Type == "" && source.Scope.Value == "" {
			source.Scope = deriveAnnotationScopeFromManifest(manifestExisting)
		}
	}
	trace, closeRun, err := logx.StartSourceRun("biofetch go", "restore", cfg.DirLogs, cfg.DirOut, source)
	if err != nil {
		return err
	}
	defer closeRun()
	if err := staticasset.Sync(source, buildAnnotationOptions(cfg.DirOut, cfg.ExistingRuleConfig, cfg.RetryConfig, cfg.DownloadControlConfig, cfg.InsecureTLSConfig, cfg.DryRunConfig, cfg.ProgressConfig), trace); err != nil {
		logx.Errorf("biofetch go", "restore failed: %v", err)
		return err
	}
	return nil
}

func resolveAnnotationSource(
	clientHTTP *http.Client,
	versionToken string,
	limiterRequest *httpx.RequestLimiter,
) (annotationSource, error) {
	if versionToken := strings.TrimSpace(versionToken); versionToken == "" {
		version, err := resolveOntologyVersion(clientHTTP, ontologyCurrentBaseURL, limiterRequest)
		if err != nil {
			return annotationSource{}, err
		}
		return annotationSource{
			version:      version,
			versionToken: version,
			baseURL:      annotationCurrentBaseURL,
		}, nil
	}

	if err := validateOptionalOntologyVersionToken(versionToken); err != nil {
		return annotationSource{}, err
	}

	baseOntologyURL := buildOntologyReleaseBaseURL(versionToken)
	version, err := resolveOntologyVersion(clientHTTP, baseOntologyURL, limiterRequest)
	if err != nil {
		return annotationSource{}, fmt.Errorf(
			"GO release %q not found or unreadable at %s: %w (see %s)",
			versionToken,
			baseOntologyURL,
			err,
			ontologyArchiveRootURL,
		)
	}
	if version != versionToken {
		return annotationSource{}, fmt.Errorf(
			"GO release %q resolved to %q at %s (see %s)",
			versionToken,
			version,
			baseOntologyURL,
			ontologyArchiveRootURL,
		)
	}
	return annotationSource{
		version:      version,
		versionToken: versionToken,
		baseURL:      buildAnnotationReleaseBaseURL(versionToken),
	}, nil
}

func resolveAnnotationDatasets(values []string) ([]string, error) {
	if len(values) == 0 {
		return nil, nil
	}
	valuesResolved, err := cliopt.ExpandListTokens(values, "", "datasets")
	if err != nil {
		return nil, err
	}
	datasets := make([]string, 0, len(valuesResolved))
	for _, value := range valuesResolved {
		dataset := strings.TrimSpace(value)
		if dataset == "" {
			continue
		}
		if !isValidAnnotationDatasetName(dataset) {
			return nil, fmt.Errorf("invalid GO annotation dataset %q", dataset)
		}
		datasets = append(datasets, dataset)
	}
	if len(datasets) == 0 {
		return nil, fmt.Errorf("datasets must not be empty")
	}
	return sets.SortedKeys(stringSet(datasets)), nil
}

func resolveAnnotationAssetSelection(
	clientHTTP *http.Client,
	source annotationSource,
	datasetNames []string,
	formats []string,
	limiterRequest *httpx.RequestLimiter,
) ([]string, []string, []staticasset.Asset, error) {
	datasets, err := resolveAnnotationDatasets(datasetNames)
	if err != nil {
		return nil, nil, nil, err
	}
	if len(datasets) > 0 {
		return datasets, formats, buildAnnotationAssets(source.baseURL, datasets, formats), nil
	}

	assets, err := discoverAnnotationAssets(clientHTTP, source.baseURL, formats, limiterRequest)
	if err != nil {
		return nil, nil, nil, err
	}
	datasetsDiscovered, formatsDiscovered := summarizeAnnotationAssets(assets)
	return datasetsDiscovered, formatsDiscovered, assets, nil
}

func resolveAnnotationFormats(values []string) ([]string, error) {
	if len(values) == 0 {
		return []string{"gaf"}, nil
	}
	valuesResolved, err := cliopt.ExpandListTokens(values, "", "formats")
	if err != nil {
		return nil, err
	}
	formatsSupported := stringSet(annotationFormatsSupported)
	formats := make([]string, 0, len(valuesResolved))
	for _, value := range valuesResolved {
		format := strings.ToLower(strings.TrimSpace(value))
		if format == "" {
			continue
		}
		if _, ok := formatsSupported[format]; !ok {
			return nil, fmt.Errorf("unsupported GO annotation format %q; supported: %s", format, strings.Join(annotationFormatsSupported, ", "))
		}
		formats = append(formats, format)
	}
	if len(formats) == 0 {
		return nil, fmt.Errorf("formats must not be empty")
	}
	return sets.SortedKeys(stringSet(formats)), nil
}

func buildAnnotationAssets(baseURL string, datasets []string, formats []string) []staticasset.Asset {
	assets := make([]staticasset.Asset, 0, len(datasets)*len(formats))
	for _, dataset := range datasets {
		for _, format := range formats {
			fileName := dataset + "." + format + ".gz"
			assets = append(assets, staticasset.Asset{
				Name: fileName,
				Path: filepath.ToSlash(filepath.Join("raw", fileName)),
				URL:  buildAnnotationAssetURL(baseURL, fileName),
			})
		}
	}
	return assets
}

func discoverAnnotationAssets(
	clientHTTP *http.Client,
	baseURL string,
	formats []string,
	limiterRequest *httpx.RequestLimiter,
) ([]staticasset.Asset, error) {
	limiterRequest.Wait()
	response, err := clientHTTP.Get(baseURL)
	if err != nil {
		return nil, fmt.Errorf("request GO annotation index %s: %w", baseURL, err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, httpx.UnexpectedStatusError{URL: baseURL, Status: response.Status, Code: response.StatusCode}
	}

	body, err := io.ReadAll(io.LimitReader(response.Body, annotationIndexReadLimitBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read GO annotation index %s: %w", baseURL, err)
	}
	if int64(len(body)) > annotationIndexReadLimitBytes {
		return nil, fmt.Errorf("GO annotation index %s exceeds %d bytes", baseURL, annotationIndexReadLimitBytes)
	}

	assets := parseAnnotationIndexAssets(baseURL, string(body), formats)
	if len(assets) == 0 {
		return nil, fmt.Errorf("no GO annotation files with formats %s were found at %s", strings.Join(formats, ","), baseURL)
	}
	return assets, nil
}

func parseAnnotationIndexAssets(baseURL string, indexHTML string, formats []string) []staticasset.Asset {
	formatsRequested := stringSet(formats)
	assetByName := make(map[string]staticasset.Asset)
	namesSet := make(map[string]struct{})
	for _, match := range annotationHrefPattern.FindAllStringSubmatch(indexHTML, -1) {
		fileName := extractAnnotationHrefFileName(match[1])
		if fileName == "" {
			continue
		}
		_, format, err := parseAnnotationFileName(fileName)
		if err != nil {
			continue
		}
		if _, ok := formatsRequested[format]; !ok {
			continue
		}
		assetByName[fileName] = staticasset.Asset{
			Name: fileName,
			Path: filepath.ToSlash(filepath.Join("raw", fileName)),
			URL:  buildAnnotationAssetURL(baseURL, fileName),
		}
		namesSet[fileName] = struct{}{}
	}

	names := sets.SortedKeys(namesSet)
	assets := make([]staticasset.Asset, 0, len(names))
	for _, name := range names {
		assets = append(assets, assetByName[name])
	}
	return assets
}

func extractAnnotationHrefFileName(value string) string {
	text := strings.TrimSpace(html.UnescapeString(value))
	if text == "" {
		return ""
	}
	if parsed, err := url.Parse(text); err == nil {
		text = parsed.Path
	} else {
		if index := strings.IndexAny(text, "?#"); index >= 0 {
			text = text[:index]
		}
	}
	if textUnescaped, err := url.PathUnescape(text); err == nil {
		text = textUnescaped
	}
	text = strings.TrimRight(text, "/")
	if index := strings.LastIndex(text, "/"); index >= 0 {
		text = text[index+1:]
	}
	return strings.TrimSpace(text)
}

func summarizeAnnotationAssets(assets []staticasset.Asset) ([]string, []string) {
	datasets := make([]string, 0, len(assets))
	formats := make([]string, 0, len(assets))
	for _, asset := range assets {
		dataset, format, err := parseAnnotationFileName(asset.Name)
		if err != nil {
			continue
		}
		datasets = append(datasets, dataset)
		formats = append(formats, format)
	}
	return sets.SortedKeys(stringSet(datasets)), sets.SortedKeys(stringSet(formats))
}

func buildAnnotationAssetURL(baseURL string, fileName string) string {
	return strings.TrimRight(baseURL, "/") + "/" + fileName
}

func buildAnnotationReleaseBaseURL(versionToken string) string {
	return ontologyArchiveRootURL + versionToken + "/annotations/"
}

func buildAnnotationStaticSource(
	source annotationSource,
	datasets []string,
	formats []string,
	assets []staticasset.Asset,
) staticasset.Source {
	return staticasset.Source{
		Database:     "go",
		Asset:        "annotation",
		Source:       "geneontology",
		Version:      source.version,
		VersionToken: source.versionToken,
		Scope: staticasset.Scope{
			Type:  "datasets_formats",
			Value: strings.Join(datasets, ",") + "|" + strings.Join(formats, ","),
		},
		Assets: assets,
	}
}

func validateAnnotationRawFiles(dirVersion string) error {
	dirRaw := filepath.Join(dirVersion, "raw")
	err := filepath.WalkDir(dirRaw, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || strings.HasSuffix(entry.Name(), ".part") {
			return nil
		}
		if _, _, err := parseAnnotationFileName(entry.Name()); err != nil {
			return err
		}
		return nil
	})
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("validate annotation raw files: %w", err)
	}
	return nil
}

func finalizeAnnotationLockManifest(fileManifest string, versionToken string) error {
	manifest, ok, err := staticasset.ReadManifest(fileManifest)
	if err != nil {
		return err
	}
	if !ok {
		return nil
	}
	manifest.Scope = deriveAnnotationScopeFromManifest(manifest)
	baseURL := buildAnnotationReleaseBaseURL(versionToken)
	for index := range manifest.Files {
		if _, _, err := parseAnnotationFileName(manifest.Files[index].Asset); err != nil {
			return err
		}
		if strings.TrimSpace(manifest.Files[index].URL) == "" {
			manifest.Files[index].URL = buildAnnotationAssetURL(baseURL, manifest.Files[index].Asset)
		}
	}
	return tomlx.WriteFileAtomic(fileManifest, manifest)
}

func deriveAnnotationScopeFromManifest(manifest staticasset.Manifest) staticasset.Scope {
	datasets := make([]string, 0, len(manifest.Files))
	formats := make([]string, 0, len(manifest.Files))
	for _, file := range manifest.Files {
		dataset, format, err := parseAnnotationFileName(file.Asset)
		if err != nil {
			continue
		}
		datasets = append(datasets, dataset)
		formats = append(formats, format)
	}
	if len(datasets) == 0 || len(formats) == 0 {
		return staticasset.Scope{}
	}
	return staticasset.Scope{
		Type:  "datasets_formats",
		Value: strings.Join(sets.SortedKeys(stringSet(datasets)), ",") + "|" + strings.Join(sets.SortedKeys(stringSet(formats)), ","),
	}
}

func parseAnnotationFileName(fileName string) (string, string, error) {
	text := strings.TrimSpace(fileName)
	if !strings.HasSuffix(text, ".gz") {
		return "", "", fmt.Errorf("invalid GO annotation filename %q: expected <dataset>.<format>.gz", fileName)
	}
	text = strings.TrimSuffix(text, ".gz")
	indexDot := strings.LastIndex(text, ".")
	if indexDot <= 0 || indexDot == len(text)-1 {
		return "", "", fmt.Errorf("invalid GO annotation filename %q: expected <dataset>.<format>.gz", fileName)
	}
	dataset := text[:indexDot]
	format := text[indexDot+1:]
	if !isValidAnnotationDatasetName(dataset) {
		return "", "", fmt.Errorf("invalid GO annotation dataset %q in filename %q", dataset, fileName)
	}
	if _, ok := stringSet(annotationFormatsSupported)[format]; !ok {
		return "", "", fmt.Errorf("unsupported GO annotation format %q in filename %q", format, fileName)
	}
	return dataset, format, nil
}

func isValidAnnotationDatasetName(value string) bool {
	if value == "" {
		return false
	}
	for _, char := range value {
		if char >= 'a' && char <= 'z' {
			continue
		}
		if char >= 'A' && char <= 'Z' {
			continue
		}
		if char >= '0' && char <= '9' {
			continue
		}
		if char == '_' || char == '-' {
			continue
		}
		return false
	}
	return true
}

func createDefaultAnnotationConfig() annotationConfig {
	cfg := annotationConfig{}
	cfg.RetryMax = 5
	cfg.RetryWait = 3 * time.Second
	cfg.WorkersMax = 1
	cfg.RuleExisting = "skip"
	return cfg
}

func createDefaultAnnotationRestoreConfig() annotationRestoreConfig {
	cfg := annotationRestoreConfig{}
	cfg.RetryMax = 5
	cfg.RetryWait = 3 * time.Second
	cfg.WorkersMax = 1
	cfg.RuleExisting = "skip"
	return cfg
}

func validateAnnotationConfig(cfg *annotationConfig) error {
	if err := cliopt.ValidateDirOutRequired(cfg.DirOut); err != nil {
		return err
	}
	if err := validateOptionalOntologyVersionToken(cfg.VersionToken); err != nil {
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
	if _, err := resolveAnnotationDatasets(cfg.datasetNames); err != nil {
		return err
	}
	if _, err := resolveAnnotationFormats(cfg.formatNames); err != nil {
		return err
	}
	return nil
}

func buildAnnotationOptions(
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
