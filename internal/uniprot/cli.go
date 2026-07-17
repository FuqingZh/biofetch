package uniprot

import (
	"biofetch/internal/shared/cliopt"
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

func NewCommand() *cobra.Command {
	commandRoot := &cobra.Command{
		Use:           "uniprot",
		Short:         "Manage UniProt raw assets and manifest.lock",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(command *cobra.Command, args []string) error {
			if len(args) > 0 {
				return fmt.Errorf("unknown command %q for %q", args[0], command.CommandPath())
			}
			return command.Help()
		},
	}
	commandRoot.AddCommand(createKBCommand())
	commandRoot.AddCommand(createUniRefCommand())
	commandRoot.AddCommand(createIDMappingCommand())
	return commandRoot
}

func createKBCommand() *cobra.Command {
	commandKB := &cobra.Command{
		Use:           "kb",
		Short:         "Manage UniProtKB raw assets",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	commandKB.AddCommand(createKBFetchCommand())
	commandKB.AddCommand(createKBLockCommand())
	commandKB.AddCommand(createKBSyncCommand())
	return commandKB
}

func createKBFetchCommand() *cobra.Command {
	cfg := createDefaultKBConfig()
	retryWaitSec := 3
	requestIntervalMs := 0

	commandFetch := &cobra.Command{
		Use:           "fetch",
		Short:         "Fetch UniProtKB raw assets and update manifest.lock",
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg.RetryWait = time.Duration(retryWaitSec) * time.Second
			cfg.RequestInterval = time.Duration(requestIntervalMs) * time.Millisecond
			if err := validateKBConfig(&cfg); err != nil {
				return err
			}
			return runFetchKB(&cfg)
		},
	}
	commandFetch.Example = strings.Join([]string{
		"biofetch uniprot kb fetch --dir_out /data/uniprot --assets sprot",
		"biofetch uniprot kb fetch --dir_out /data/uniprot --assets sprot,trembl --should_allow_large_assets",
		"biofetch uniprot kb fetch --dir_out /data/uniprot --assets varsplic",
		"biofetch uniprot kb fetch --dir_out /data/uniprot --assets sprot_dat,trembl_dat --should_allow_large_assets",
	}, "\n")

	flags := commandFetch.Flags()
	flags.SortFlags = false
	cliopt.BindDirOutFlag(flags, &cfg.DirOutConfig, "UniProt asset root directory")
	cliopt.BindVersionFlag(flags, &cfg.VersionConfig, "UniProt release token; omit for current")
	flags.StringSliceVar(&cfg.assetNames, "assets", nil, "UniProtKB assets: all|sprot|trembl|varsplic|sprot_dat|trembl_dat; omit or pass all to fetch all supported assets")
	flags.BoolVar(&cfg.shouldAllowLargeAssets, "should_allow_large_assets", false, "Allow large UniProtKB assets")
	flags.StringVar(&cfg.baseURLCurrentRelease, "base_url_current_release", cfg.baseURLCurrentRelease, "UniProt current_release base URL, including mirror URLs")
	cliopt.BindRuleExistingFlag(flags, &cfg.ExistingRuleConfig, "Rule for existing files: skip|overwrite")
	cliopt.BindRetryFlags(flags, &cfg.RetryConfig, &retryWaitSec)
	cliopt.BindDownloadControlFlags(flags, &cfg.DownloadControlConfig, &requestIntervalMs)
	cliopt.BindInsecureTLSFlag(flags, &cfg.InsecureTLSConfig, "Disable TLS certificate verification")
	cliopt.BindDryRunFlag(flags, &cfg.DryRunConfig, "Print actions only; do not download")
	cliopt.BindLogDirFlag(flags, &cfg.LogConfig, "Directory for run log files; default is <version>/logs/")
	cliopt.BindProgressFlag(flags, &cfg.ProgressConfig, "Disable download progress display")
	return commandFetch
}

func createKBLockCommand() *cobra.Command {
	cfg := kbLockConfig{}
	commandLock := &cobra.Command{
		Use:           "lock",
		Short:         "Rebuild UniProtKB manifest.lock from the current version directory",
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runLockKB(&cfg)
		},
	}
	flags := commandLock.Flags()
	flags.SortFlags = false
	cliopt.BindDirSnapshotFlag(flags, &cfg.DirSnapshotConfig)
	cliopt.BindLockWorkersFlag(flags, &cfg.workersMax)
	cliopt.BindDryRunFlag(flags, &cfg.DryRunConfig, "Print actions only; do not write manifest")
	cliopt.BindLogDirFlag(flags, &cfg.LogConfig, "Directory for run log files; default is <version>/logs/")
	return commandLock
}

func createKBSyncCommand() *cobra.Command {
	cfg := kbSyncConfig{}
	cfg.RetryMax = 5
	cfg.RetryWait = 3 * time.Second
	cfg.RuleExisting = "skip"
	cfg.WorkersMax = 1
	retryWaitSec := 3
	requestIntervalMs := 0

	commandSync := &cobra.Command{
		Use:           "sync",
		Short:         "Sync UniProtKB files from manifest.lock and refresh manifest",
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg.RetryWait = time.Duration(retryWaitSec) * time.Second
			cfg.RequestInterval = time.Duration(requestIntervalMs) * time.Millisecond
			if err := cliopt.ValidateDirOutRequired(cfg.DirOut); err != nil {
				return err
			}
			if err := cliopt.ValidateVersionRequired(cfg.VersionToken); err != nil {
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
			return runSyncKB(&cfg)
		},
	}
	flags := commandSync.Flags()
	flags.SortFlags = false
	cliopt.BindDirOutFlag(flags, &cfg.DirOutConfig, "UniProt asset root directory")
	cliopt.BindVersionFlag(flags, &cfg.VersionConfig, "UniProtKB version token")
	cliopt.BindRuleExistingFlag(flags, &cfg.ExistingRuleConfig, "Rule for existing files: skip|overwrite")
	cliopt.BindRetryFlags(flags, &cfg.RetryConfig, &retryWaitSec)
	cliopt.BindDownloadControlFlags(flags, &cfg.DownloadControlConfig, &requestIntervalMs)
	cliopt.BindInsecureTLSFlag(flags, &cfg.InsecureTLSConfig, "Disable TLS certificate verification")
	cliopt.BindDryRunFlag(flags, &cfg.DryRunConfig, "Print actions only; do not download")
	cliopt.BindLogDirFlag(flags, &cfg.LogConfig, "Directory for run log files; default is <version>/logs/")
	cliopt.BindProgressFlag(flags, &cfg.ProgressConfig, "Disable download progress display")
	return commandSync
}

func createUniRefCommand() *cobra.Command {
	commandUniRef := &cobra.Command{
		Use:           "uniref",
		Short:         "Manage UniRef FASTA raw assets",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	commandUniRef.AddCommand(createUniRefFetchCommand())
	commandUniRef.AddCommand(createUniRefLockCommand())
	commandUniRef.AddCommand(createUniRefSyncCommand())
	return commandUniRef
}

func createUniRefFetchCommand() *cobra.Command {
	cfg := createDefaultUniRefConfig()
	retryWaitSec := 3
	requestIntervalMs := 0

	commandFetch := &cobra.Command{
		Use:           "fetch",
		Short:         "Fetch UniRef FASTA raw assets and update manifest.lock",
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg.RetryWait = time.Duration(retryWaitSec) * time.Second
			cfg.RequestInterval = time.Duration(requestIntervalMs) * time.Millisecond
			if err := validateUniRefConfig(&cfg); err != nil {
				return err
			}
			return runFetchUniRef(&cfg)
		},
	}
	commandFetch.Example = strings.Join([]string{
		"biofetch uniprot uniref fetch --dir_out /data/uniprot --assets uniref90 --should_allow_large_assets",
		"biofetch uniprot uniref fetch --dir_out /data/uniprot --should_allow_large_assets",
	}, "\n")

	flags := commandFetch.Flags()
	flags.SortFlags = false
	cliopt.BindDirOutFlag(flags, &cfg.DirOutConfig, "UniProt asset root directory")
	cliopt.BindVersionFlag(flags, &cfg.VersionConfig, "UniProt release token; omit for current")
	flags.StringSliceVar(&cfg.assetNames, "assets", nil, "UniRef FASTA assets: all|uniref90; omit or pass all to fetch all supported assets")
	flags.BoolVar(&cfg.shouldAllowLargeAssets, "should_allow_large_assets", false, "Allow large UniRef FASTA assets")
	flags.StringVar(&cfg.baseURLCurrentRelease, "base_url_current_release", cfg.baseURLCurrentRelease, "UniProt current_release base URL, including mirror URLs")
	cliopt.BindRuleExistingFlag(flags, &cfg.ExistingRuleConfig, "Rule for existing files: skip|overwrite")
	cliopt.BindRetryFlags(flags, &cfg.RetryConfig, &retryWaitSec)
	cliopt.BindDownloadControlFlags(flags, &cfg.DownloadControlConfig, &requestIntervalMs)
	cliopt.BindInsecureTLSFlag(flags, &cfg.InsecureTLSConfig, "Disable TLS certificate verification")
	cliopt.BindDryRunFlag(flags, &cfg.DryRunConfig, "Print actions only; do not download")
	cliopt.BindLogDirFlag(flags, &cfg.LogConfig, "Directory for run log files; default is <version>/logs/")
	cliopt.BindProgressFlag(flags, &cfg.ProgressConfig, "Disable download progress display")
	return commandFetch
}

func createUniRefLockCommand() *cobra.Command {
	cfg := unirefLockConfig{}
	commandLock := &cobra.Command{
		Use:           "lock",
		Short:         "Rebuild UniRef manifest.lock from the current version directory",
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runLockUniRef(&cfg)
		},
	}
	flags := commandLock.Flags()
	flags.SortFlags = false
	cliopt.BindDirSnapshotFlag(flags, &cfg.DirSnapshotConfig)
	cliopt.BindLockWorkersFlag(flags, &cfg.workersMax)
	cliopt.BindDryRunFlag(flags, &cfg.DryRunConfig, "Print actions only; do not write manifest")
	cliopt.BindLogDirFlag(flags, &cfg.LogConfig, "Directory for run log files; default is <version>/logs/")
	return commandLock
}

func createUniRefSyncCommand() *cobra.Command {
	cfg := unirefSyncConfig{}
	cfg.RetryMax = 5
	cfg.RetryWait = 3 * time.Second
	cfg.RuleExisting = "skip"
	cfg.WorkersMax = 1
	retryWaitSec := 3
	requestIntervalMs := 0

	commandSync := &cobra.Command{
		Use:           "sync",
		Short:         "Sync UniRef files from manifest.lock and refresh manifest",
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg.RetryWait = time.Duration(retryWaitSec) * time.Second
			cfg.RequestInterval = time.Duration(requestIntervalMs) * time.Millisecond
			if err := cliopt.ValidateDirOutRequired(cfg.DirOut); err != nil {
				return err
			}
			if err := cliopt.ValidateVersionRequired(cfg.VersionToken); err != nil {
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
			return runSyncUniRef(&cfg)
		},
	}
	flags := commandSync.Flags()
	flags.SortFlags = false
	cliopt.BindDirOutFlag(flags, &cfg.DirOutConfig, "UniProt asset root directory")
	cliopt.BindVersionFlag(flags, &cfg.VersionConfig, "UniRef version token")
	cliopt.BindRuleExistingFlag(flags, &cfg.ExistingRuleConfig, "Rule for existing files: skip|overwrite")
	cliopt.BindRetryFlags(flags, &cfg.RetryConfig, &retryWaitSec)
	cliopt.BindDownloadControlFlags(flags, &cfg.DownloadControlConfig, &requestIntervalMs)
	cliopt.BindInsecureTLSFlag(flags, &cfg.InsecureTLSConfig, "Disable TLS certificate verification")
	cliopt.BindDryRunFlag(flags, &cfg.DryRunConfig, "Print actions only; do not download")
	cliopt.BindLogDirFlag(flags, &cfg.LogConfig, "Directory for run log files; default is <version>/logs/")
	cliopt.BindProgressFlag(flags, &cfg.ProgressConfig, "Disable download progress display")
	return commandSync
}

func createIDMappingCommand() *cobra.Command {
	commandIDMapping := &cobra.Command{
		Use:           "idmapping",
		Short:         "Manage UniProt ID mapping raw assets",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	commandIDMapping.AddCommand(createIDMappingFetchCommand())
	commandIDMapping.AddCommand(createIDMappingLockCommand())
	commandIDMapping.AddCommand(createIDMappingSyncCommand())
	return commandIDMapping
}

func createIDMappingFetchCommand() *cobra.Command {
	cfg := createDefaultIDMappingConfig()
	retryWaitSec := 3
	requestIntervalMs := 0

	commandFetch := &cobra.Command{
		Use:           "fetch",
		Short:         "Fetch UniProt ID mapping global raw assets and update manifest.lock",
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg.RetryWait = time.Duration(retryWaitSec) * time.Second
			cfg.RequestInterval = time.Duration(requestIntervalMs) * time.Millisecond
			if err := validateIDMappingConfig(&cfg); err != nil {
				return err
			}
			return runFetchIDMapping(&cfg)
		},
	}
	commandFetch.Example = strings.Join([]string{
		"biofetch uniprot idmapping fetch --dir_out /data/uniprot --should_allow_large_assets",
		"biofetch uniprot idmapping fetch --dir_out /data/uniprot --assets selected --should_allow_large_assets",
		"biofetch uniprot idmapping fetch --dir_out /data/uniprot --assets dat --should_allow_large_assets",
	}, "\n")

	flags := commandFetch.Flags()
	flags.SortFlags = false
	cliopt.BindDirOutFlag(flags, &cfg.DirOutConfig, "UniProt asset root directory")
	cliopt.BindVersionFlag(flags, &cfg.VersionConfig, "UniProt release token; omit for current")
	flags.StringSliceVar(&cfg.assetNames, "assets", nil, "UniProt ID mapping assets: all|selected|dat; omit or pass all to fetch all supported assets")
	flags.BoolVar(&cfg.shouldAllowLargeAssets, "should_allow_large_assets", false, "Allow multi-GB UniProt ID mapping assets")
	flags.StringVar(&cfg.baseURLCurrentRelease, "base_url_current_release", cfg.baseURLCurrentRelease, "UniProt current_release base URL, including mirror URLs")
	cliopt.BindRuleExistingFlag(flags, &cfg.ExistingRuleConfig, "Rule for existing files: skip|overwrite")
	cliopt.BindRetryFlags(flags, &cfg.RetryConfig, &retryWaitSec)
	cliopt.BindDownloadControlFlags(flags, &cfg.DownloadControlConfig, &requestIntervalMs)
	cliopt.BindInsecureTLSFlag(flags, &cfg.InsecureTLSConfig, "Disable TLS certificate verification")
	cliopt.BindDryRunFlag(flags, &cfg.DryRunConfig, "Print actions only; do not download")
	cliopt.BindLogDirFlag(flags, &cfg.LogConfig, "Directory for run log files; default is <version>/logs/")
	cliopt.BindProgressFlag(flags, &cfg.ProgressConfig, "Disable download progress display")
	return commandFetch
}

func createIDMappingLockCommand() *cobra.Command {
	cfg := idMappingLockConfig{}
	commandLock := &cobra.Command{
		Use:           "lock",
		Short:         "Rebuild UniProt ID mapping manifest.lock from the current version directory",
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runLockIDMapping(&cfg)
		},
	}
	flags := commandLock.Flags()
	flags.SortFlags = false
	cliopt.BindDirSnapshotFlag(flags, &cfg.DirSnapshotConfig)
	cliopt.BindLockWorkersFlag(flags, &cfg.workersMax)
	cliopt.BindDryRunFlag(flags, &cfg.DryRunConfig, "Print actions only; do not write manifest")
	cliopt.BindLogDirFlag(flags, &cfg.LogConfig, "Directory for run log files; default is <version>/logs/")
	return commandLock
}

func createIDMappingSyncCommand() *cobra.Command {
	cfg := idMappingSyncConfig{}
	cfg.RetryMax = 5
	cfg.RetryWait = 3 * time.Second
	cfg.RuleExisting = "skip"
	cfg.WorkersMax = 1
	retryWaitSec := 3
	requestIntervalMs := 0

	commandSync := &cobra.Command{
		Use:           "sync",
		Short:         "Sync UniProt ID mapping files from manifest.lock and refresh manifest",
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg.RetryWait = time.Duration(retryWaitSec) * time.Second
			cfg.RequestInterval = time.Duration(requestIntervalMs) * time.Millisecond
			if err := cliopt.ValidateDirOutRequired(cfg.DirOut); err != nil {
				return err
			}
			if err := cliopt.ValidateVersionRequired(cfg.VersionToken); err != nil {
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
			return runSyncIDMapping(&cfg)
		},
	}
	flags := commandSync.Flags()
	flags.SortFlags = false
	cliopt.BindDirOutFlag(flags, &cfg.DirOutConfig, "UniProt asset root directory")
	cliopt.BindVersionFlag(flags, &cfg.VersionConfig, "UniProt ID mapping version token")
	cliopt.BindRuleExistingFlag(flags, &cfg.ExistingRuleConfig, "Rule for existing files: skip|overwrite")
	cliopt.BindRetryFlags(flags, &cfg.RetryConfig, &retryWaitSec)
	cliopt.BindDownloadControlFlags(flags, &cfg.DownloadControlConfig, &requestIntervalMs)
	cliopt.BindInsecureTLSFlag(flags, &cfg.InsecureTLSConfig, "Disable TLS certificate verification")
	cliopt.BindDryRunFlag(flags, &cfg.DryRunConfig, "Print actions only; do not download")
	cliopt.BindLogDirFlag(flags, &cfg.LogConfig, "Directory for run log files; default is <version>/logs/")
	cliopt.BindProgressFlag(flags, &cfg.ProgressConfig, "Disable download progress display")
	return commandSync
}
