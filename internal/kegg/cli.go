package kegg

import (
	"biofetch/internal/shared/cliopt"
	"biofetch/internal/shared/sets"
	"bufio"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

var pathwayAssetNamesSupported = []string{"list", "entry", "kgml", "conf", "image"}

const (
	ruleOrderAsc   = "asc"
	ruleOrderDesc  = "desc"
	ruleOrderInput = "input"
)

type pathwayConfig struct {
	dirOut                  string
	dirLogs                 string
	version                 string
	versionToken            string
	sourceRelease           string
	sourceReleaseStart      string
	sourceReleaseEnd        string
	sourceLastUpdate        string
	sourceLastUpdateStart   string
	sourceLastUpdateEnd     string
	assetNames              []string
	ruleOrder               string
	organismCode            string
	organismCodes           []string
	pathwayIDs              []string
	shouldFetchReference    bool
	shouldDownloadAll       bool
	ruleExisting            string
	shouldOverwriteExisting bool
	retryMax                int
	retryWait               time.Duration
	requestInterval         time.Duration
	shouldAllowInsecureTLS  bool
	shouldDryRun            bool
	scopeType               string
	scopeValue              string
}

type briteConfig struct {
	dirOut                  string
	dirLogs                 string
	version                 string
	versionToken            string
	sourceRelease           string
	sourceReleaseStart      string
	sourceReleaseEnd        string
	sourceLastUpdate        string
	sourceLastUpdateStart   string
	sourceLastUpdateEnd     string
	catalogCode             string
	ruleOrder               string
	organismCodes           []string
	shouldDownloadAll       bool
	briteIDs                []string
	shouldDownloadRootOnly  bool
	ruleExisting            string
	shouldOverwriteExisting bool
	retryMax                int
	retryWait               time.Duration
	requestInterval         time.Duration
	shouldAllowInsecureTLS  bool
	shouldDryRun            bool
	scopeType               string
	scopeValue              string
}

func RunCLI(args []string) error {
	commandRoot := NewCommand()
	commandRoot.SetArgs(args)
	return commandRoot.Execute()
}

func NewCommand() *cobra.Command {
	commandRoot := &cobra.Command{
		Use:           "kegg",
		Short:         "Manage KEGG raw assets and manifest.lock",
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	commandRoot.AddCommand(createCatalogCommand())
	commandRoot.AddCommand(createMappingCommand())
	commandRoot.AddCommand(createPathwayCommand())
	commandRoot.AddCommand(createBriteCommand())
	return commandRoot
}

func createMappingCommand() *cobra.Command {
	commandMapping := &cobra.Command{
		Use:           "mapping",
		Short:         "Manage KEGG organism and gene mapping raw assets",
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	commandMapping.AddCommand(createMappingFetchCommand())
	commandMapping.AddCommand(createMappingLockCommand())
	commandMapping.AddCommand(createMappingRestoreCommand())
	return commandMapping
}

func createMappingFetchCommand() *cobra.Command {
	cfg := createDefaultMappingConfig()
	commandFetch := &cobra.Command{
		Use:           "fetch",
		Short:         "Fetch KEGG mapping raw assets and update manifest.lock",
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := validateMappingFetchConfig(&cfg); err != nil {
				return err
			}
			return runFetchMapping(&cfg)
		},
	}

	commandFetch.Example = strings.Join([]string{
		"biofetch kegg mapping fetch --output /data/kegg --organisms hsa --assets conv_uniprot,gene_ko,gene_pathway",
		"biofetch kegg mapping fetch --output /data/kegg --organisms hsa,mmu --assets organism,conv_uniprot,conv_ncbi_geneid,gene_list,gene_ko,gene_pathway,ko_pathway",
		"biofetch kegg mapping fetch --output /data/kegg --all-organisms --assets conv_uniprot,gene_ko --dry-run",
	}, "\n")

	flags := commandFetch.Flags()
	flags.SortFlags = false
	flags.StringVarP(&cfg.dirOut, "output", "o", cfg.dirOut, "KEGG asset root directory")
	flags.StringVar(&cfg.versionToken, "version", cfg.versionToken, "KEGG local snapshot key (YYYY-MM), e.g. 2026-04")
	flags.StringSliceVar(&cfg.assetNames, "assets", nil, "Mapping assets: all|organism|conv_uniprot|conv_ncbi_geneid|gene_list|gene_ko|gene_pathway|ko_pathway; omit or pass all to fetch all supported assets")
	cliopt.BindListFileFlag(flags, &cfg.assetNames, "assets")
	flags.StringSliceVar(&cfg.organismCodes, "organisms", nil, "KEGG organism codes; pass inline values, repeat the flag,")
	cliopt.BindListFileFlag(flags, &cfg.organismCodes, "organisms")
	flags.BoolVar(&cfg.shouldDownloadAll, "all-organisms", false, "Fetch organism-scoped mapping assets for all KEGG organisms")
	flags.StringVar(&cfg.ruleExisting, "on-existing", cfg.ruleExisting, "Rule for existing files: skip|overwrite")
	flags.IntVar(&cfg.retryMax, "max-attempts", cfg.retryMax, "Max retry attempts on download failures")
	flags.DurationVar(&cfg.retryWait, "retry-wait", cfg.retryWait, "Wait between download attempts")
	flags.IntVar(&cfg.workersMax, "workers", cfg.workersMax, "Max concurrent download workers")
	flags.DurationVar(&cfg.requestInterval, "request-interval", cfg.requestInterval, "Delay between KEGG API requests")
	flags.BoolVar(&cfg.shouldAllowInsecureTLS, "insecure", false, "Disable TLS certificate verification")
	flags.BoolVar(&cfg.shouldDryRun, "dry-run", false, "Print actions only; do not download")
	flags.BoolVar(&cfg.shouldDisableProgress, "no-progress", false, "Disable download progress display")
	flags.StringVar(&cfg.dirLogs, "log-dir", cfg.dirLogs, "Directory for run log files; default is <version>/logs/")
	return commandFetch
}

func createMappingLockCommand() *cobra.Command {
	cfg := mappingLockConfig{}

	commandLock := &cobra.Command{
		Use:           "lock SNAPSHOT",
		Short:         "Rebuild KEGG mapping manifest.lock from the current version directory",
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg.dirSnapshot = args[0]
			if err := validateMappingLockConfig(&cfg); err != nil {
				return err
			}
			return runLockMapping(&cfg)
		},
	}

	flags := commandLock.Flags()
	flags.SortFlags = false
	cliopt.BindLockWorkersFlag(flags, &cfg.workersMax)
	flags.BoolVar(&cfg.shouldDryRun, "dry-run", false, "Print actions only; do not write manifest")
	flags.StringVar(&cfg.dirLogs, "log-dir", cfg.dirLogs, "Directory for run log files; default is <version>/logs/")
	return commandLock
}

func createMappingRestoreCommand() *cobra.Command {
	cfg := mappingRestoreConfig{}
	cfg.ruleExisting = "skip"
	cfg.retryMax = defaultKEGGRetryMax
	cfg.retryWait = defaultKEGGRetryWait
	cfg.workersMax = 1
	commandRestore := &cobra.Command{
		Use:           "restore SNAPSHOT",
		Short:         "Restore KEGG mapping files from manifest.lock and refresh manifest",
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			var err error
			cfg.dirOut, cfg.versionToken, err = cliopt.SnapshotRootVersion(args[0])
			if err != nil {
				return err
			}
			if err := validateMappingRestoreConfig(&cfg); err != nil {
				return err
			}
			return runRestoreMapping(&cfg)
		},
	}

	flags := commandRestore.Flags()
	flags.SortFlags = false
	flags.StringVar(&cfg.ruleExisting, "on-existing", cfg.ruleExisting, "Rule for existing files: skip|overwrite")
	flags.IntVar(&cfg.retryMax, "max-attempts", cfg.retryMax, "Max retry attempts on download failures")
	flags.DurationVar(&cfg.retryWait, "retry-wait", cfg.retryWait, "Wait between download attempts")
	flags.IntVar(&cfg.workersMax, "workers", cfg.workersMax, "Max concurrent download workers")
	flags.DurationVar(&cfg.requestInterval, "request-interval", cfg.requestInterval, "Delay between KEGG API requests")
	flags.BoolVar(&cfg.shouldAllowInsecureTLS, "insecure", false, "Disable TLS certificate verification")
	flags.BoolVar(&cfg.shouldDryRun, "dry-run", false, "Print actions only; do not download")
	flags.BoolVar(&cfg.shouldDisableProgress, "no-progress", false, "Disable download progress display")
	flags.StringVar(&cfg.dirLogs, "log-dir", cfg.dirLogs, "Directory for run log files; default is <version>/logs/")
	return commandRestore
}

func createCatalogCommand() *cobra.Command {
	commandCatalog := &cobra.Command{
		Use:           "catalog",
		Short:         "Manage KEGG shared catalog assets",
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	commandCatalog.AddCommand(createCatalogFetchCommand())
	commandCatalog.AddCommand(createCatalogLockCommand())
	commandCatalog.AddCommand(createCatalogSyncCommand())
	return commandCatalog
}

func createPathwayCommand() *cobra.Command {
	commandPathway := &cobra.Command{
		Use:           "pathway",
		Short:         "Manage KEGG PATHWAY raw assets",
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	commandPathway.AddCommand(createPathwayFetchCommand())
	commandPathway.AddCommand(createPathwayLockCommand())
	commandPathway.AddCommand(createPathwayRestoreCommand())
	return commandPathway
}

func createPathwayFetchCommand() *cobra.Command {
	cfg := createDefaultPathwayConfig()
	commandPathway := &cobra.Command{
		Use:           "fetch",
		Short:         "Fetch KEGG PATHWAY raw assets and update manifest.lock",
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := validatePathwayConfig(&cfg); err != nil {
				return err
			}
			return runFetchPathway(&cfg)
		},
	}

	commandPathway.Example = strings.Join([]string{
		"biofetch kegg pathway fetch --output /data/kegg --organisms hsa --dry-run",
		"biofetch kegg pathway fetch --output /data/kegg --version 2026-04 --organisms hsa",
		"biofetch kegg pathway fetch --output /data/kegg --organisms hsa",
		"biofetch kegg pathway fetch --output /data/kegg --organisms @organisms.txt --order input",
		"biofetch kegg pathway fetch --output /data/kegg --organisms hsa --organisms tca",
		"biofetch kegg pathway fetch --output /data/kegg --include-reference --pathway-ids @pathway_ids.txt",
		"biofetch kegg pathway fetch --output /data/kegg --organisms hsa --assets entry --assets kgml --assets image",
	}, "\n")

	flags := commandPathway.Flags()
	flags.SortFlags = false
	flags.StringVarP(&cfg.dirOut, "output", "o", cfg.dirOut, "KEGG asset root directory")
	flags.StringVar(&cfg.versionToken, "version", cfg.versionToken, "KEGG local snapshot key (YYYY-MM), e.g. 2026-04")
	flags.StringSliceVar(&cfg.assetNames, "assets", nil, "PATHWAY assets: all|list|entry|kgml|conf|image; omit or pass all to fetch all supported assets within the selected scope")
	cliopt.BindListFileFlag(flags, &cfg.assetNames, "assets")
	flags.StringSliceVar(&cfg.organismCodes, "organisms", nil, "KEGG organism codes; pass inline values, repeat the flag,")
	cliopt.BindListFileFlag(flags, &cfg.organismCodes, "organisms")
	flags.StringSliceVar(&cfg.pathwayIDs, "pathway-ids", nil, "Pathway IDs; pass inline values, repeat the flag,")
	cliopt.BindListFileFlag(flags, &cfg.pathwayIDs, "pathway-ids")
	flags.BoolVar(
		&cfg.shouldFetchReference,
		"include-reference",
		false,
		"Fetch reference pathways from /list/pathway",
	)
	flags.BoolVar(
		&cfg.shouldDownloadAll,
		"all-organisms",
		false,
		"Fetch PATHWAY assets for all KEGG organisms",
	)
	flags.StringVar(&cfg.ruleExisting, "on-existing", cfg.ruleExisting, "Rule for existing files: skip|overwrite")
	flags.StringVar(&cfg.ruleOrder, "order", cfg.ruleOrder, "Traversal order for organisms and pathway IDs: asc|desc|input (input preserves first-seen order)")
	flags.IntVar(&cfg.retryMax, "max-attempts", cfg.retryMax, "Max retry attempts on download failures")
	flags.DurationVar(&cfg.retryWait, "retry-wait", cfg.retryWait, "Wait between download attempts")
	flags.DurationVar(&cfg.requestInterval, "request-interval", cfg.requestInterval, "Delay between KEGG API requests")
	flags.BoolVar(
		&cfg.shouldAllowInsecureTLS,
		"insecure",
		false,
		"Disable TLS certificate verification",
	)
	flags.BoolVar(&cfg.shouldDryRun, "dry-run", false, "Print actions only; do not download")
	flags.StringVar(&cfg.dirLogs, "log-dir", cfg.dirLogs, "Directory for run log files; default is <version>/logs/")

	return commandPathway
}

func createPathwayLockCommand() *cobra.Command {
	cfg := keggLockConfig{}

	commandLock := &cobra.Command{
		Use:           "lock SNAPSHOT",
		Short:         "Rebuild KEGG PATHWAY manifest.lock from the current version directory",
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg.dirSnapshot = args[0]
			versionToken, err := cliopt.SnapshotVersionToken(cfg.dirSnapshot)
			if err != nil {
				return err
			}
			if !isValidKEGGSnapshotVersionToken(versionToken) {
				return fmt.Errorf("version must be a local snapshot key like 2026-04")
			}
			return runLockPathway(&cfg)
		},
	}

	flags := commandLock.Flags()
	flags.SortFlags = false
	cliopt.BindLockWorkersFlag(flags, &cfg.workersMax)
	flags.BoolVar(&cfg.shouldDryRun, "dry-run", false, "Print actions only; do not write manifest")
	flags.StringVar(&cfg.dirLogs, "log-dir", cfg.dirLogs, "Directory for run log files; default is <version>/logs/")
	return commandLock
}

func createPathwayRestoreCommand() *cobra.Command {
	cfg := keggRestoreConfig{}
	cfg.ruleExisting = "skip"
	commandRestore := &cobra.Command{
		Use:           "restore SNAPSHOT",
		Short:         "Restore KEGG PATHWAY files from manifest.lock and refresh manifest",
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			var err error
			cfg.dirOut, cfg.versionToken, err = cliopt.SnapshotRootVersion(args[0])
			if err != nil {
				return err
			}
			if strings.TrimSpace(cfg.dirOut) == "" {
				return fmt.Errorf("output is required")
			}
			if strings.TrimSpace(cfg.versionToken) == "" {
				return fmt.Errorf("version is required")
			}
			if !isValidKEGGSnapshotVersionToken(cfg.versionToken) {
				return fmt.Errorf("version must be a local snapshot key like 2026-04")
			}
			if cfg.ruleExisting != "skip" && cfg.ruleExisting != "overwrite" {
				return fmt.Errorf("on-existing must be one of: skip, overwrite")
			}
			cfg.shouldOverwriteExisting = cfg.ruleExisting == "overwrite"
			return runRestorePathway(&cfg)
		},
	}

	flags := commandRestore.Flags()
	flags.SortFlags = false
	flags.StringVar(&cfg.ruleExisting, "on-existing", cfg.ruleExisting, "Rule for existing files: skip|overwrite")
	flags.DurationVar(&cfg.requestInterval, "request-interval", cfg.requestInterval, "Delay between KEGG API requests")
	flags.BoolVar(&cfg.shouldAllowInsecureTLS, "insecure", false, "Disable TLS certificate verification")
	flags.BoolVar(&cfg.shouldDryRun, "dry-run", false, "Print actions only; do not download")
	flags.StringVar(&cfg.dirLogs, "log-dir", cfg.dirLogs, "Directory for run log files; default is <version>/logs/")
	return commandRestore
}

func createDefaultPathwayConfig() pathwayConfig {
	cfg := pathwayConfig{}
	cfg.retryMax = defaultKEGGRetryMax
	cfg.retryWait = defaultKEGGRetryWait
	cfg.requestInterval = 350 * time.Millisecond
	cfg.ruleExisting = "skip"
	cfg.ruleOrder = ruleOrderAsc
	return cfg
}

func validatePathwayConfig(cfg *pathwayConfig) error {
	if cfg.retryMax < 1 {
		return fmt.Errorf("max-attempts must be >= 1")
	}
	if cfg.retryWait < 0 {
		return fmt.Errorf("retry-wait must be >= 0")
	}
	if cfg.requestInterval < 0 {
		return fmt.Errorf("request-interval must be >= 0")
	}
	if strings.TrimSpace(cfg.dirOut) == "" {
		return fmt.Errorf("output is required")
	}
	if strings.TrimSpace(cfg.versionToken) != "" && !isValidKEGGSnapshotVersionToken(cfg.versionToken) {
		return fmt.Errorf("version must be a local snapshot key like 2026-04")
	}
	if cfg.ruleExisting != "skip" && cfg.ruleExisting != "overwrite" {
		return fmt.Errorf("on-existing must be one of: skip, overwrite")
	}
	if strings.TrimSpace(cfg.ruleOrder) == "" {
		cfg.ruleOrder = ruleOrderAsc
	}
	if err := validateRuleOrder(cfg.ruleOrder); err != nil {
		return err
	}
	cfg.shouldOverwriteExisting = cfg.ruleExisting == "overwrite"
	assetNames, err := resolvePathwayAssetNames(cfg.assetNames)
	if err != nil {
		return err
	}
	cfg.assetNames = assetNames
	if len(cfg.organismCodes) > 0 {
		organismCodes, err := resolveKEGGOrganismInputs(cfg.organismCodes, cfg.ruleOrder)
		if err != nil {
			return err
		}
		cfg.organismCodes = organismCodes
	}
	if len(cfg.pathwayIDs) > 0 {
		pathwayIDs, err := resolvePathwayIDInputs(cfg.pathwayIDs, cfg.ruleOrder)
		if err != nil {
			return err
		}
		cfg.pathwayIDs = pathwayIDs
	}

	countScope := 0
	if len(cfg.organismCodes) > 0 {
		countScope++
	}
	if cfg.shouldFetchReference {
		countScope++
	}
	if cfg.shouldDownloadAll {
		countScope++
	}
	if countScope != 1 {
		return fmt.Errorf(
			"choose exactly one scope: --organisms | --all-organisms | --include-reference",
		)
	}
	if cfg.shouldDownloadAll {
		if len(cfg.pathwayIDs) > 0 {
			return fmt.Errorf("pathway_ids is not allowed with multi-organism download")
		}
	}

	return nil
}

func resolvePathwayAssetNames(valuesInput []string) ([]string, error) {
	if len(valuesInput) == 0 {
		return append([]string(nil), pathwayAssetNamesSupported...), nil
	}
	return parsePathwayAssetNames(valuesInput)
}

func parsePathwayAssetNames(valuesInput []string) ([]string, error) {
	setAssets := make(map[string]struct{})
	hasAll := false
	for _, valueInput := range valuesInput {
		for _, token := range strings.Split(valueInput, ",") {
			assetName := strings.ToLower(strings.TrimSpace(token))
			if assetName == "" {
				continue
			}
			if assetName == "all" {
				hasAll = true
				continue
			}
			if !isSupportedPathwayAssetName(assetName) {
				return nil, fmt.Errorf("invalid PATHWAY asset: %s", token)
			}
			setAssets[assetName] = struct{}{}
		}
	}
	if hasAll {
		if len(setAssets) > 0 {
			return nil, fmt.Errorf("assets=all cannot be combined with specific PATHWAY assets")
		}
		return append([]string(nil), pathwayAssetNamesSupported...), nil
	}
	if len(setAssets) == 0 {
		return nil, fmt.Errorf("assets must not be empty")
	}
	return sets.SortedKeys(setAssets), nil
}

func isSupportedPathwayAssetName(assetName string) bool {
	for _, value := range pathwayAssetNamesSupported {
		if value == assetName {
			return true
		}
	}
	return false
}

func createBriteCommand() *cobra.Command {
	commandBrite := &cobra.Command{
		Use:           "brite",
		Short:         "Manage KEGG BRITE raw assets",
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	commandBrite.AddCommand(createBriteFetchCommand())
	commandBrite.AddCommand(createBriteLockCommand())
	commandBrite.AddCommand(createBriteRestoreCommand())
	return commandBrite
}

func createBriteFetchCommand() *cobra.Command {
	cfg := createDefaultBriteConfig()
	commandBrite := &cobra.Command{
		Use:           "fetch",
		Short:         "Fetch KEGG BRITE raw assets and update manifest.lock",
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := validateBriteConfig(&cfg); err != nil {
				return err
			}
			return runFetchBrite(&cfg)
		},
	}

	commandBrite.Example = strings.Join([]string{
		"biofetch kegg brite fetch --output /data/kegg --catalog br --dry-run",
		"biofetch kegg brite fetch --output /data/kegg --version 2026-04 --catalog br",
		"biofetch kegg brite fetch --output /data/kegg --organisms hsa --brite-ids @brite_ids.txt",
		"biofetch kegg brite fetch --output /data/kegg --organisms @organisms.txt --order desc",
		"biofetch kegg brite fetch --output /data/kegg --organisms hsa --organisms tca",
		"biofetch kegg brite fetch --output /data/kegg --all-organisms",
	}, "\n")

	flags := commandBrite.Flags()
	flags.SortFlags = false
	flags.StringVarP(&cfg.dirOut, "output", "o", cfg.dirOut, "KEGG asset root directory")
	flags.StringVar(&cfg.versionToken, "version", cfg.versionToken, "KEGG local snapshot key (YYYY-MM), e.g. 2026-04")
	flags.StringVar(&cfg.catalogCode, "catalog", "", "Reference BRITE catalog")
	flags.StringSliceVar(&cfg.organismCodes, "organisms", nil, "KEGG organism codes; pass inline values, repeat the flag,")
	cliopt.BindListFileFlag(flags, &cfg.organismCodes, "organisms")
	flags.BoolVar(
		&cfg.shouldDownloadAll,
		"all-organisms",
		false,
		"Fetch BRITE assets for all KEGG organisms",
	)
	flags.StringSliceVar(&cfg.briteIDs, "brite-ids", nil, "BRITE IDs; pass inline values, repeat the flag,")
	cliopt.BindListFileFlag(flags, &cfg.briteIDs, "brite-ids")
	flags.BoolVar(&cfg.shouldDownloadRootOnly, "root-only", false, "Download only root BRITE hierarchy (*00001) per catalog")
	flags.StringVar(&cfg.ruleExisting, "on-existing", cfg.ruleExisting, "Rule for existing files: skip|overwrite")
	flags.StringVar(&cfg.ruleOrder, "order", cfg.ruleOrder, "Traversal order for organisms and BRITE IDs: asc|desc|input (input preserves first-seen order)")
	flags.IntVar(&cfg.retryMax, "max-attempts", cfg.retryMax, "Max retry attempts on download failures")
	flags.DurationVar(&cfg.retryWait, "retry-wait", cfg.retryWait, "Wait between download attempts")
	flags.DurationVar(&cfg.requestInterval, "request-interval", cfg.requestInterval, "Delay between KEGG API requests")
	flags.BoolVar(
		&cfg.shouldAllowInsecureTLS,
		"insecure",
		false,
		"Disable TLS certificate verification",
	)
	flags.BoolVar(&cfg.shouldDryRun, "dry-run", false, "Print actions only; do not download")
	flags.StringVar(&cfg.dirLogs, "log-dir", cfg.dirLogs, "Directory for run log files; default is <version>/logs/")

	return commandBrite
}

func createBriteLockCommand() *cobra.Command {
	cfg := keggLockConfig{}

	commandLock := &cobra.Command{
		Use:           "lock SNAPSHOT",
		Short:         "Rebuild KEGG BRITE manifest.lock from the current version directory",
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg.dirSnapshot = args[0]
			versionToken, err := cliopt.SnapshotVersionToken(cfg.dirSnapshot)
			if err != nil {
				return err
			}
			if !isValidKEGGSnapshotVersionToken(versionToken) {
				return fmt.Errorf("version must be a local snapshot key like 2026-04")
			}
			return runLockBrite(&cfg)
		},
	}

	flags := commandLock.Flags()
	flags.SortFlags = false
	cliopt.BindLockWorkersFlag(flags, &cfg.workersMax)
	flags.BoolVar(&cfg.shouldDryRun, "dry-run", false, "Print actions only; do not write manifest")
	flags.StringVar(&cfg.dirLogs, "log-dir", cfg.dirLogs, "Directory for run log files; default is <version>/logs/")
	return commandLock
}

func createBriteRestoreCommand() *cobra.Command {
	cfg := keggRestoreConfig{}
	cfg.ruleExisting = "skip"
	commandRestore := &cobra.Command{
		Use:           "restore SNAPSHOT",
		Short:         "Restore KEGG BRITE files from manifest.lock and refresh manifest",
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			var err error
			cfg.dirOut, cfg.versionToken, err = cliopt.SnapshotRootVersion(args[0])
			if err != nil {
				return err
			}
			if strings.TrimSpace(cfg.dirOut) == "" {
				return fmt.Errorf("output is required")
			}
			if strings.TrimSpace(cfg.versionToken) == "" {
				return fmt.Errorf("version is required")
			}
			if !isValidKEGGSnapshotVersionToken(cfg.versionToken) {
				return fmt.Errorf("version must be a local snapshot key like 2026-04")
			}
			if cfg.ruleExisting != "skip" && cfg.ruleExisting != "overwrite" {
				return fmt.Errorf("on-existing must be one of: skip, overwrite")
			}
			cfg.shouldOverwriteExisting = cfg.ruleExisting == "overwrite"
			return runRestoreBrite(&cfg)
		},
	}

	flags := commandRestore.Flags()
	flags.SortFlags = false
	flags.StringVar(&cfg.ruleExisting, "on-existing", cfg.ruleExisting, "Rule for existing files: skip|overwrite")
	flags.DurationVar(&cfg.requestInterval, "request-interval", cfg.requestInterval, "Delay between KEGG API requests")
	flags.BoolVar(&cfg.shouldAllowInsecureTLS, "insecure", false, "Disable TLS certificate verification")
	flags.BoolVar(&cfg.shouldDryRun, "dry-run", false, "Print actions only; do not download")
	flags.StringVar(&cfg.dirLogs, "log-dir", cfg.dirLogs, "Directory for run log files; default is <version>/logs/")
	return commandRestore
}

func createDefaultBriteConfig() briteConfig {
	cfg := briteConfig{}
	cfg.retryMax = defaultKEGGRetryMax
	cfg.retryWait = defaultKEGGRetryWait
	cfg.requestInterval = 350 * time.Millisecond
	cfg.ruleExisting = "skip"
	cfg.ruleOrder = ruleOrderAsc
	return cfg
}

func validateBriteConfig(cfg *briteConfig) error {
	if cfg.retryMax < 1 {
		return fmt.Errorf("max-attempts must be >= 1")
	}
	if cfg.retryWait < 0 {
		return fmt.Errorf("retry-wait must be >= 0")
	}
	if cfg.requestInterval < 0 {
		return fmt.Errorf("request-interval must be >= 0")
	}
	if strings.TrimSpace(cfg.dirOut) == "" {
		return fmt.Errorf("output is required")
	}
	if strings.TrimSpace(cfg.versionToken) != "" && !isValidKEGGSnapshotVersionToken(cfg.versionToken) {
		return fmt.Errorf("version must be a local snapshot key like 2026-04")
	}
	if cfg.ruleExisting != "skip" && cfg.ruleExisting != "overwrite" {
		return fmt.Errorf("on-existing must be one of: skip, overwrite")
	}
	if strings.TrimSpace(cfg.ruleOrder) == "" {
		cfg.ruleOrder = ruleOrderAsc
	}
	if err := validateRuleOrder(cfg.ruleOrder); err != nil {
		return err
	}
	cfg.shouldOverwriteExisting = cfg.ruleExisting == "overwrite"
	if len(cfg.organismCodes) > 0 {
		organismCodes, err := resolveKEGGOrganismInputs(cfg.organismCodes, cfg.ruleOrder)
		if err != nil {
			return err
		}
		cfg.organismCodes = organismCodes
	}
	if len(cfg.briteIDs) > 0 {
		briteIDs, err := resolveBriteIDInputs(cfg.briteIDs, cfg.ruleOrder)
		if err != nil {
			return err
		}
		cfg.briteIDs = briteIDs
	}

	if cfg.shouldDownloadAll {
		if strings.TrimSpace(cfg.catalogCode) != "" {
			return fmt.Errorf("catalog must not be set with --all-organisms")
		}
		if len(cfg.organismCodes) > 0 {
			return fmt.Errorf("organisms must not be set with --all-organisms")
		}
		if len(cfg.briteIDs) > 0 {
			return fmt.Errorf("brite_ids is not allowed with --all-organisms")
		}
	} else {
		countSources := 0
		if strings.TrimSpace(cfg.catalogCode) != "" {
			countSources++
		}
		if len(cfg.organismCodes) > 0 {
			countSources++
		}
		if countSources != 1 {
			return fmt.Errorf("choose exactly one source: --catalog | --organisms | --all-organisms")
		}
	}

	if cfg.shouldDownloadRootOnly {
		if len(cfg.briteIDs) > 0 {
			return fmt.Errorf("brite_ids is not allowed with --root-only")
		}
	}
	if strings.TrimSpace(cfg.catalogCode) != "" && cfg.catalogCode != "br" && cfg.catalogCode != "ko" {
		return fmt.Errorf("invalid catalog: %s", cfg.catalogCode)
	}
	return nil
}

type orderedListInputSpec struct {
	nameOption  string
	nameValue   string
	fnNormalize func(string) string
	fnValidate  func(string) bool
}

func validateRuleOrder(ruleOrder string) error {
	switch ruleOrder {
	case ruleOrderAsc, ruleOrderDesc, ruleOrderInput:
		return nil
	default:
		return fmt.Errorf("order must be one of: asc, desc, input")
	}
}

func resolveKEGGOrganismInputs(valuesInput []string, ruleOrder string) ([]string, error) {
	return resolveOrderedListInputs(valuesInput, ruleOrder, orderedListInputSpec{
		nameOption:  "organisms",
		nameValue:   "KEGG organism code",
		fnNormalize: normalizeKEGGOrganismCode,
		fnValidate:  isValidKEGGOrganismCode,
	})
}

func resolvePathwayIDInputs(valuesInput []string, ruleOrder string) ([]string, error) {
	return resolveOrderedListInputs(valuesInput, ruleOrder, orderedListInputSpec{
		nameOption:  "pathway-ids",
		nameValue:   "pathway id",
		fnNormalize: normalizePathwayID,
		fnValidate:  isValidPathwayID,
	})
}

func resolveBriteIDInputs(valuesInput []string, ruleOrder string) ([]string, error) {
	return resolveOrderedListInputs(valuesInput, ruleOrder, orderedListInputSpec{
		nameOption:  "brite-ids",
		nameValue:   "BRITE id",
		fnNormalize: normalizeBriteID,
		fnValidate:  isValidBriteID,
	})
}

func resolveOrderedListInputs(
	valuesInput []string,
	ruleOrder string,
	spec orderedListInputSpec,
) ([]string, error) {
	valuesResolved := make([]string, 0)
	setSeen := make(map[string]struct{})

	for _, valueInput := range valuesInput {
		for _, tokenRaw := range strings.Split(valueInput, ",") {
			token := strings.TrimSpace(tokenRaw)
			if token == "" {
				continue
			}
			if strings.HasPrefix(token, "@") {
				filePath := strings.TrimSpace(strings.TrimPrefix(token, "@"))
				if filePath == "" {
					return nil, fmt.Errorf("%s file path must not be empty", spec.nameOption)
				}
				valuesFile, err := readOrderedListInputFile(filePath, spec)
				if err != nil {
					return nil, err
				}
				for _, value := range valuesFile {
					if _, ok := setSeen[value]; ok {
						continue
					}
					setSeen[value] = struct{}{}
					valuesResolved = append(valuesResolved, value)
				}
				continue
			}

			valueNormalized := spec.fnNormalize(token)
			if !spec.fnValidate(valueNormalized) {
				return nil, fmt.Errorf("invalid %s: %s", spec.nameValue, token)
			}
			if _, ok := setSeen[valueNormalized]; ok {
				continue
			}
			setSeen[valueNormalized] = struct{}{}
			valuesResolved = append(valuesResolved, valueNormalized)
		}
	}

	if len(valuesResolved) == 0 {
		return nil, fmt.Errorf("%s must not be empty", spec.nameOption)
	}
	return applyTraversalOrder(valuesResolved, ruleOrder), nil
}

func readOrderedListInputFile(filePath string, spec orderedListInputSpec) ([]string, error) {
	fileIn, err := os.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("open %s file: %w", spec.nameOption, err)
	}
	defer fileIn.Close()

	valuesResolved := make([]string, 0)
	setSeen := make(map[string]struct{})
	scanner := bufio.NewScanner(fileIn)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		valueNormalized := spec.fnNormalize(line)
		if !spec.fnValidate(valueNormalized) {
			return nil, fmt.Errorf("invalid %s in %s: %s", spec.nameValue, filePath, line)
		}
		if _, ok := setSeen[valueNormalized]; ok {
			continue
		}
		setSeen[valueNormalized] = struct{}{}
		valuesResolved = append(valuesResolved, valueNormalized)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read %s file: %w", spec.nameOption, err)
	}
	if len(valuesResolved) == 0 {
		return nil, fmt.Errorf("%s file must not be empty: %s", spec.nameOption, filePath)
	}
	return valuesResolved, nil
}

func applyTraversalOrder(values []string, ruleOrder string) []string {
	valuesOrdered := append([]string(nil), values...)
	switch ruleOrder {
	case ruleOrderInput:
		return valuesOrdered
	case ruleOrderDesc:
		sort.Strings(valuesOrdered)
		reverseStrings(valuesOrdered)
		return valuesOrdered
	default:
		sort.Strings(valuesOrdered)
		return valuesOrdered
	}
}

func reverseStrings(values []string) {
	for idxLeft, idxRight := 0, len(values)-1; idxLeft < idxRight; idxLeft, idxRight = idxLeft+1, idxRight-1 {
		values[idxLeft], values[idxRight] = values[idxRight], values[idxLeft]
	}
}

func parseKEGGOrganismCodes(valuesInput []string) ([]string, error) {
	return resolveKEGGOrganismInputs(valuesInput, ruleOrderAsc)
}

func readKEGGOrganismCodesFromFile(filePath string) ([]string, error) {
	return resolveKEGGOrganismInputs([]string{"@" + filePath}, ruleOrderAsc)
}

func normalizeKEGGOrganismCode(text string) string {
	return strings.ToLower(strings.TrimSpace(text))
}

func isValidKEGGOrganismCode(text string) bool {
	text = normalizeKEGGOrganismCode(text)
	if text == "" {
		return false
	}
	for _, char := range text {
		if (char < 'a' || char > 'z') && (char < '0' || char > '9') {
			return false
		}
	}
	return true
}

func isValidKEGGMajorVersion(value string) bool {
	text := strings.TrimSpace(value)
	if text == "" {
		return false
	}
	parts := strings.Split(text, ".")
	if len(parts) != 2 {
		return false
	}
	if parts[0] == "" || parts[1] == "" {
		return false
	}
	for _, ch := range parts[0] {
		if ch < '0' || ch > '9' {
			return false
		}
	}
	for _, ch := range parts[1] {
		if ch < '0' || ch > '9' {
			return false
		}
	}
	return true
}
