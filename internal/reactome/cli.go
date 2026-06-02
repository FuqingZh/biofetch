package reactome

import (
	"biofetch/internal/shared/cliopt"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

func NewCommand() *cobra.Command {
	commandRoot := &cobra.Command{
		Use:           "reactome",
		Short:         "Manage Reactome raw assets and manifest.lock",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	commandRoot.AddCommand(createMappingCommand())
	return commandRoot
}

func createMappingCommand() *cobra.Command {
	commandMapping := &cobra.Command{
		Use:           "mapping",
		Short:         "Manage Reactome mapping raw assets",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	commandMapping.AddCommand(createMappingFetchCommand())
	commandMapping.AddCommand(createMappingLockCommand())
	commandMapping.AddCommand(createMappingSyncCommand())
	return commandMapping
}

func createMappingFetchCommand() *cobra.Command {
	cfg := createDefaultMappingConfig()
	retryWaitSec := 3
	requestIntervalMs := 0

	commandFetch := &cobra.Command{
		Use:           "fetch",
		Short:         "Fetch Reactome mapping raw assets and update manifest.lock",
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg.RetryWait = time.Duration(retryWaitSec) * time.Second
			cfg.RequestInterval = time.Duration(requestIntervalMs) * time.Millisecond
			if err := validateMappingConfig(&cfg); err != nil {
				return err
			}
			return runFetchMapping(&cfg)
		},
	}
	commandFetch.Example = strings.Join([]string{
		"biofetch reactome mapping fetch --dir_out /data/reactome --assets UniProt2Reactome_All_Levels.txt --should_dry_run",
		"biofetch reactome mapping fetch --dir_out /data/reactome --assets ReactomePathways.txt",
		"biofetch reactome mapping fetch --dir_out /data/reactome --should_allow_large_assets",
	}, "\n")

	flags := commandFetch.Flags()
	flags.SortFlags = false
	cliopt.BindDirOutFlag(flags, &cfg.DirOutConfig, "Reactome asset root directory")
	cliopt.BindVersionFlag(flags, &cfg.VersionConfig, "Reactome snapshot token; omit for current")
	flags.StringSliceVar(&cfg.assetNames, "assets", nil, "Reactome mapping assets; omit or pass all to fetch all supported assets; repeat the flag, use commas, or use @file")
	flags.BoolVar(&cfg.shouldAllowLargeAssets, "should_allow_large_assets", false, "Allow Reactome mapping assets larger than the configured safety threshold")
	cliopt.BindRuleExistingFlag(flags, &cfg.ExistingRuleConfig, "Rule for existing files: skip|overwrite")
	cliopt.BindRetryFlags(flags, &cfg.RetryConfig, &retryWaitSec)
	cliopt.BindDownloadControlFlags(flags, &cfg.DownloadControlConfig, &requestIntervalMs)
	cliopt.BindInsecureTLSFlag(flags, &cfg.InsecureTLSConfig, "Disable TLS certificate verification")
	cliopt.BindDryRunFlag(flags, &cfg.DryRunConfig, "Print actions only; do not download")
	cliopt.BindProgressFlag(flags, &cfg.ProgressConfig, "Disable download progress display")
	return commandFetch
}

func createMappingLockCommand() *cobra.Command {
	cfg := mappingLockConfig{}
	commandLock := &cobra.Command{
		Use:           "lock",
		Short:         "Rebuild Reactome mapping manifest.lock from the current version directory",
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := cliopt.ValidateDirOutRequired(cfg.DirOut); err != nil {
				return err
			}
			if err := cliopt.ValidateVersionRequired(cfg.VersionToken); err != nil {
				return err
			}
			return runLockMapping(&cfg)
		},
	}
	flags := commandLock.Flags()
	flags.SortFlags = false
	cliopt.BindDirOutFlag(flags, &cfg.DirOutConfig, "Reactome asset root directory")
	cliopt.BindVersionFlag(flags, &cfg.VersionConfig, "Reactome mapping version token")
	cliopt.BindDryRunFlag(flags, &cfg.DryRunConfig, "Print actions only; do not write manifest")
	return commandLock
}

func createMappingSyncCommand() *cobra.Command {
	cfg := mappingSyncConfig{}
	cfg.RetryMax = 5
	cfg.RetryWait = 3 * time.Second
	cfg.RuleExisting = "skip"
	cfg.WorkersMax = 1
	retryWaitSec := 3
	requestIntervalMs := 0

	commandSync := &cobra.Command{
		Use:           "sync",
		Short:         "Sync Reactome mapping files from manifest.lock and refresh manifest",
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
			return runSyncMapping(&cfg)
		},
	}
	flags := commandSync.Flags()
	flags.SortFlags = false
	cliopt.BindDirOutFlag(flags, &cfg.DirOutConfig, "Reactome asset root directory")
	cliopt.BindVersionFlag(flags, &cfg.VersionConfig, "Reactome mapping version token")
	cliopt.BindRuleExistingFlag(flags, &cfg.ExistingRuleConfig, "Rule for existing files: skip|overwrite")
	cliopt.BindRetryFlags(flags, &cfg.RetryConfig, &retryWaitSec)
	cliopt.BindDownloadControlFlags(flags, &cfg.DownloadControlConfig, &requestIntervalMs)
	cliopt.BindInsecureTLSFlag(flags, &cfg.InsecureTLSConfig, "Disable TLS certificate verification")
	cliopt.BindDryRunFlag(flags, &cfg.DryRunConfig, "Print actions only; do not download")
	cliopt.BindProgressFlag(flags, &cfg.ProgressConfig, "Disable download progress display")
	return commandSync
}
