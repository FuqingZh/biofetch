package bulkasset

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
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

type AssetSpec struct {
	Name                 string
	Path                 string
	URL                  string
	Default              bool
	Large                bool
	RecoverDownloadError func(string, error) (bool, error)
	VerifyDownloadedFile func(string) error
	ExpectedBytes        int64
}

type Spec struct {
	Database                   string
	Asset                      string
	Source                     string
	DatabaseDescription        string
	AssetDescription           string
	VersionDescription         string
	Assets                     []AssetSpec
	ResolveCurrent             func(*http.Client) (string, error)
	ExpandAssets               func(*http.Client, []AssetSpec) ([]AssetSpec, error)
	SupportsFixedVersion       bool
	DefaultWorkers             int
	DefaultRequestWait         time.Duration
	LockOnlyDeclaredAssets     bool
	RequireDefaultAssetsOnLock bool
	RequireCompleteAssets      bool
	RejectUndeclaredAssets     bool
	DisableAssetSelection      bool
	FixedVersion               string
	SourceVersion              string
}

type config struct {
	cliopt.DirOutConfig
	cliopt.VersionConfig
	cliopt.ExistingRuleConfig
	cliopt.RetryConfig
	cliopt.DownloadControlConfig
	cliopt.InsecureTLSConfig
	cliopt.DryRunConfig
	cliopt.LogConfig
	cliopt.ProgressConfig
	assetNames []string
	allowLarge bool
}

type lockConfig struct {
	cliopt.DirSnapshotConfig
	cliopt.DryRunConfig
	cliopt.LogConfig
	workersMax int
}

type restoreConfig struct {
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

func NewCommand(spec Spec) *cobra.Command {
	commandRoot := &cobra.Command{
		Use:           spec.Database,
		Short:         spec.DatabaseDescription,
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          cobra.NoArgs,
	}
	commandRoot.AddCommand(NewAssetCommand(spec))
	return commandRoot
}

func NewAssetCommand(spec Spec) *cobra.Command {
	commandAsset := &cobra.Command{
		Use:           spec.Asset,
		Short:         spec.AssetDescription,
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          cobra.NoArgs,
	}
	commandAsset.AddCommand(createFetchCommand(spec))
	commandAsset.AddCommand(createLockCommand(spec))
	commandAsset.AddCommand(createRestoreCommand(spec))
	return commandAsset
}

func createFetchCommand(spec Spec) *cobra.Command {
	cfg := config{}
	cfg.RuleExisting = "skip"
	cfg.RetryMax = 5
	cfg.RetryWait = 3 * time.Second
	cfg.WorkersMax = spec.DefaultWorkers
	if cfg.WorkersMax == 0 {
		cfg.WorkersMax = 1
	}
	cfg.RequestInterval = spec.DefaultRequestWait
	cfg.VersionToken = spec.FixedVersion

	command := &cobra.Command{
		Use:           "fetch",
		Short:         fmt.Sprintf("Fetch %s raw assets and update manifest.lock", spec.Database),
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runFetch(spec, &cfg)
		},
	}
	flags := command.Flags()
	flags.SortFlags = false
	cliopt.BindDirOutFlag(flags, &cfg.DirOutConfig, spec.Database+" asset root directory")
	cliopt.BindVersionFlag(flags, &cfg.VersionConfig, spec.VersionDescription)
	if !spec.DisableAssetSelection {
		cliopt.BindStringListFlags(flags, &cfg.assetNames, "assets", "Assets to fetch; omit for the maintained default set; repeat the flag,")
	}
	if hasLargeAssets(spec.Assets) {
		flags.BoolVar(&cfg.allowLarge, "allow-large-downloads", false, "Allow explicitly selected large assets")
	}
	cliopt.BindRuleExistingFlag(flags, &cfg.ExistingRuleConfig, "Rule for existing files: skip|overwrite")
	cliopt.BindRetryFlags(flags, &cfg.RetryConfig)
	cliopt.BindDownloadControlFlags(flags, &cfg.DownloadControlConfig)
	cliopt.BindInsecureTLSFlag(flags, &cfg.InsecureTLSConfig, "Disable TLS certificate verification")
	cliopt.BindDryRunFlag(flags, &cfg.DryRunConfig, "Print actions only; do not download")
	cliopt.BindLogDirFlag(flags, &cfg.LogConfig, "Directory for run log files; default is <version>/logs/")
	cliopt.BindProgressFlag(flags, &cfg.ProgressConfig, "Disable download progress display")
	return command
}

func createLockCommand(spec Spec) *cobra.Command {
	cfg := lockConfig{}
	command := &cobra.Command{
		Use:           "lock SNAPSHOT",
		Short:         fmt.Sprintf("Rebuild %s %s manifest.lock from SNAPSHOT", spec.Database, spec.Asset),
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          cobra.ExactArgs(1),
		Example:       fmt.Sprintf("biofetch %s %s lock /data/%s/%s/<version>", spec.Database, spec.Asset, spec.Database, spec.Asset),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg.DirSnapshot = args[0]
			return runLock(spec, &cfg)
		},
	}
	flags := command.Flags()
	flags.SortFlags = false
	cliopt.BindLockWorkersFlag(flags, &cfg.workersMax)
	cliopt.BindDryRunFlag(flags, &cfg.DryRunConfig, "Print actions only; do not write manifest")
	cliopt.BindLogDirFlag(flags, &cfg.LogConfig, "Directory for run log files; default is <version>/logs/")
	return command
}

func createRestoreCommand(spec Spec) *cobra.Command {
	cfg := restoreConfig{}
	cfg.RuleExisting = "skip"
	cfg.RetryMax = 5
	cfg.RetryWait = 3 * time.Second
	cfg.WorkersMax = 1
	cfg.RequestInterval = spec.DefaultRequestWait
	command := &cobra.Command{
		Use:           "restore SNAPSHOT",
		Short:         fmt.Sprintf("Restore %s %s files from SNAPSHOT manifest.lock", spec.Database, spec.Asset),
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          cobra.ExactArgs(1),
		Example:       fmt.Sprintf("biofetch %s %s restore /data/%s/%s/<version>", spec.Database, spec.Asset, spec.Database, spec.Asset),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := cliopt.ApplyFlatSnapshot(&cfg.DirOutConfig, &cfg.VersionConfig, args[0], spec.Asset); err != nil {
				return err
			}
			if spec.FixedVersion != "" && cfg.VersionToken != spec.FixedVersion {
				return fmt.Errorf("%s %s supports only fixed version %s", spec.Database, spec.Asset, spec.FixedVersion)
			}
			if err := validateCommon(&cfg.DirOutConfig, &cfg.ExistingRuleConfig, &cfg.RetryConfig, &cfg.DownloadControlConfig); err != nil {
				return err
			}
			return runRestore(spec, &cfg)
		},
	}
	flags := command.Flags()
	flags.SortFlags = false
	cliopt.BindRuleExistingFlag(flags, &cfg.ExistingRuleConfig, "Rule for existing files: skip|overwrite")
	cliopt.BindRetryFlags(flags, &cfg.RetryConfig)
	cliopt.BindDownloadControlFlags(flags, &cfg.DownloadControlConfig)
	cliopt.BindInsecureTLSFlag(flags, &cfg.InsecureTLSConfig, "Disable TLS certificate verification")
	cliopt.BindDryRunFlag(flags, &cfg.DryRunConfig, "Print actions only; do not download")
	cliopt.BindLogDirFlag(flags, &cfg.LogConfig, "Directory for run log files; default is <version>/logs/")
	cliopt.BindProgressFlag(flags, &cfg.ProgressConfig, "Disable download progress display")
	return command
}

func runFetch(spec Spec, cfg *config) error {
	if err := validateCommon(&cfg.DirOutConfig, &cfg.ExistingRuleConfig, &cfg.RetryConfig, &cfg.DownloadControlConfig); err != nil {
		return err
	}
	assets, err := resolveAssets(spec.Assets, cfg.assetNames)
	if err != nil {
		return err
	}
	if !cfg.allowLarge {
		for _, asset := range assets {
			if asset.Large {
				return fmt.Errorf("asset %s requires --allow-large-downloads", asset.Name)
			}
		}
	}
	clientHTTP := httpx.NewClient(cfg.ShouldAllowInsecureTLS)
	version, err := resolveVersion(spec, clientHTTP, cfg.VersionToken)
	if err != nil {
		return err
	}
	if spec.ExpandAssets != nil {
		assets, err = spec.ExpandAssets(clientHTTP, assets)
		if err != nil {
			return err
		}
	}
	source := buildSource(spec, version, assets)
	trace, closeRun, err := logx.StartSourceRun("biofetch "+spec.Database, "fetch", cfg.DirLogs, cfg.DirOut, source)
	if err != nil {
		return err
	}
	defer closeRun()
	return staticasset.Fetch(source, buildOptions(
		cfg.DirOut, cfg.ExistingRuleConfig, cfg.RetryConfig, cfg.DownloadControlConfig,
		cfg.InsecureTLSConfig, cfg.DryRunConfig, cfg.ProgressConfig,
	), trace)
}

func runLock(spec Spec, cfg *lockConfig) error {
	version, err := cliopt.SnapshotVersionToken(cfg.DirSnapshot)
	if err != nil {
		return err
	}
	if spec.FixedVersion != "" && version != spec.FixedVersion {
		return fmt.Errorf("%s %s supports only fixed version %s", spec.Database, spec.Asset, spec.FixedVersion)
	}
	source := buildSource(spec, version, spec.Assets)
	_, closeRun, err := logx.StartVersionedRun("biofetch "+spec.Database, "lock", cfg.DirLogs, cfg.DirSnapshot)
	if err != nil {
		return err
	}
	defer closeRun()
	return staticasset.Lock(source, cfg.DirSnapshot, staticasset.Options{
		RuleExisting: "skip",
		RetryMax:     1,
		WorkersMax:   cfg.workersMax,
		ShouldDryRun: cfg.ShouldDryRun,
	}, logx.NewStaticAssetTraceSink("biofetch "+spec.Database))
}

func runRestore(spec Spec, cfg *restoreConfig) error {
	source := buildSource(spec, cfg.VersionToken, spec.Assets)
	trace, closeRun, err := logx.StartSourceRun("biofetch "+spec.Database, "restore", cfg.DirLogs, cfg.DirOut, source)
	if err != nil {
		return err
	}
	defer closeRun()
	return staticasset.Sync(source, buildOptions(
		cfg.DirOut, cfg.ExistingRuleConfig, cfg.RetryConfig, cfg.DownloadControlConfig,
		cfg.InsecureTLSConfig, cfg.DryRunConfig, cfg.ProgressConfig,
	), trace)
}

func validateCommon(dirOut *cliopt.DirOutConfig, existing *cliopt.ExistingRuleConfig, retry *cliopt.RetryConfig, download *cliopt.DownloadControlConfig) error {
	if err := cliopt.ValidateDirOutRequired(dirOut.DirOut); err != nil {
		return err
	}
	if err := cliopt.ValidateRuleExisting(existing); err != nil {
		return err
	}
	if err := cliopt.ValidateRetryConfig(retry); err != nil {
		return err
	}
	return cliopt.ValidateDownloadControlConfig(download)
}

func resolveVersion(spec Spec, clientHTTP *http.Client, requested string) (string, error) {
	requested = strings.TrimSpace(requested)
	if spec.FixedVersion != "" {
		if requested == "" {
			return spec.FixedVersion, nil
		}
		if requested != spec.FixedVersion {
			return "", fmt.Errorf("%s %s supports only fixed version %s", spec.Database, spec.Asset, spec.FixedVersion)
		}
		return requested, nil
	}
	if requested != "" && requested != "current" {
		if !spec.SupportsFixedVersion {
			return "", fmt.Errorf("%s %s fetch supports only current/empty version; use restore for an existing fixed snapshot", spec.Database, spec.Asset)
		}
		return requested, nil
	}
	if spec.ResolveCurrent == nil {
		return "", fmt.Errorf("%s %s does not define current version resolution", spec.Database, spec.Asset)
	}
	return spec.ResolveCurrent(clientHTTP)
}

func resolveAssets(available []AssetSpec, requested []string) ([]AssetSpec, error) {
	byName := make(map[string]AssetSpec, len(available))
	defaults := make([]AssetSpec, 0)
	for _, asset := range available {
		byName[asset.Name] = asset
		if asset.Default {
			defaults = append(defaults, asset)
		}
	}
	if len(requested) == 0 {
		return defaults, nil
	}
	values, err := cliopt.ExpandListTokens(requested, "", "assets")
	if err != nil {
		return nil, err
	}
	names := make(map[string]struct{})
	for _, value := range values {
		if value == "all" {
			for name := range byName {
				names[name] = struct{}{}
			}
			continue
		}
		if _, ok := byName[value]; !ok {
			return nil, fmt.Errorf("unknown asset %q; supported: %s", value, strings.Join(sets.SortedKeys(keySet(byName)), ", "))
		}
		names[value] = struct{}{}
	}
	result := make([]AssetSpec, 0, len(names))
	for _, name := range sets.SortedKeys(names) {
		result = append(result, byName[name])
	}
	if len(result) == 0 {
		return nil, fmt.Errorf("assets must not be empty")
	}
	return result, nil
}

func buildSource(spec Spec, version string, assets []AssetSpec) staticasset.Source {
	result := make([]staticasset.Asset, 0, len(assets))
	required := make([]string, 0)
	for _, asset := range assets {
		url := strings.ReplaceAll(asset.URL, "{version}", version)
		path := strings.ReplaceAll(asset.Path, "{version}", version)
		result = append(result, staticasset.Asset{
			Name:                 asset.Name,
			Path:                 filepath.ToSlash(filepath.Join("raw", path)),
			URL:                  url,
			RecoverDownloadError: asset.RecoverDownloadError,
			VerifyDownloadedFile: asset.VerifyDownloadedFile,
			ExpectedBytes:        asset.ExpectedBytes,
		})
	}
	if spec.RequireDefaultAssetsOnLock {
		for _, asset := range spec.Assets {
			if asset.Default {
				required = append(required, asset.Name)
			}
		}
	}
	sourceVersion := version
	if spec.SourceVersion != "" {
		sourceVersion = spec.SourceVersion
	}
	return staticasset.Source{
		Database:               spec.Database,
		Asset:                  spec.Asset,
		Source:                 spec.Source,
		Version:                sourceVersion,
		VersionToken:           version,
		Assets:                 result,
		LockOnlyDeclaredAssets: spec.LockOnlyDeclaredAssets,
		RequiredAssets:         required,
		RequireCompleteAssets:  spec.RequireCompleteAssets,
		RejectUndeclaredAssets: spec.RejectUndeclaredAssets,
	}
}

func buildOptions(dirOut string, existing cliopt.ExistingRuleConfig, retry cliopt.RetryConfig, download cliopt.DownloadControlConfig, insecure cliopt.InsecureTLSConfig, dryRun cliopt.DryRunConfig, progress cliopt.ProgressConfig) staticasset.Options {
	return staticasset.Options{
		DirOut:                 dirOut,
		RuleExisting:           existing.RuleExisting,
		RetryMax:               retry.RetryMax,
		RetryWait:              retry.RetryWait,
		WorkersMax:             download.WorkersMax,
		RequestInterval:        download.RequestInterval,
		ShouldAllowInsecureTLS: insecure.ShouldAllowInsecureTLS,
		ShouldDryRun:           dryRun.ShouldDryRun,
		ShouldDisableProgress:  progress.ShouldDisableProgress,
	}
}

func ResolveVersionFromLastModified(url string) func(*http.Client) (string, error) {
	return func(clientHTTP *http.Client) (string, error) {
		request, err := http.NewRequest(http.MethodHead, url, nil)
		if err != nil {
			return "", fmt.Errorf("build release metadata request: %w", err)
		}
		response, err := clientHTTP.Do(request)
		if err != nil {
			return "", fmt.Errorf("resolve release metadata: %w", err)
		}
		defer response.Body.Close()
		if response.StatusCode < 200 || response.StatusCode >= 400 {
			return "", fmt.Errorf("resolve release metadata: unexpected status %s", response.Status)
		}
		modified := strings.TrimSpace(response.Header.Get("Last-Modified"))
		if modified == "" {
			return "", fmt.Errorf("resolve release metadata: Last-Modified header is missing")
		}
		instant, err := http.ParseTime(modified)
		if err != nil {
			return "", fmt.Errorf("parse release Last-Modified %q: %w", modified, err)
		}
		return instant.UTC().Format("2006-01-02"), nil
	}
}

func ResolveVersionFromPage(url string, pattern *regexp.Regexp) func(*http.Client) (string, error) {
	return func(clientHTTP *http.Client) (string, error) {
		response, err := clientHTTP.Get(url)
		if err != nil {
			return "", fmt.Errorf("resolve current release: %w", err)
		}
		defer response.Body.Close()
		if response.StatusCode != http.StatusOK {
			return "", fmt.Errorf("resolve current release: unexpected status %s", response.Status)
		}
		data, err := io.ReadAll(io.LimitReader(response.Body, 2*1024*1024))
		if err != nil {
			return "", fmt.Errorf("read current release page: %w", err)
		}
		matches := pattern.FindAllStringSubmatch(string(data), -1)
		if len(matches) == 0 {
			return "", fmt.Errorf("current release token not found at %s", url)
		}
		versions := make([]string, 0, len(matches))
		for _, match := range matches {
			versions = append(versions, match[1])
		}
		sort.Strings(versions)
		return versions[len(versions)-1], nil
	}
}

func keySet(values map[string]AssetSpec) map[string]struct{} {
	result := make(map[string]struct{}, len(values))
	for key := range values {
		result[key] = struct{}{}
	}
	return result
}

func hasLargeAssets(assets []AssetSpec) bool {
	for _, asset := range assets {
		if asset.Large {
			return true
		}
	}
	return false
}
