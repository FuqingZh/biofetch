package uniprot

import (
	"biofetch/internal/shared/cliopt"
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
	}
	commandRoot.AddCommand(createIDMappingCommand())
	return commandRoot
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
		"biofetch uniprot idmapping fetch --dir_out /data/uniprot --assets selected --should_allow_large_download",
		"biofetch uniprot idmapping fetch --dir_out /data/uniprot --assets dat --should_allow_large_download",
		"biofetch uniprot idmapping fetch --dir_out /data/uniprot --assets selected,dat --should_allow_large_download",
	}, "\n")

	flags := commandFetch.Flags()
	flags.SortFlags = false
	cliopt.BindDirOutFlag(flags, &cfg.DirOutConfig, "UniProt asset root directory")
	cliopt.BindVersionFlag(flags, &cfg.VersionConfig, "UniProt release token; omit for current")
	flags.StringSliceVar(&cfg.assetNames, "assets", nil, "UniProt ID mapping assets: selected|dat; repeat the flag, use commas, or use @file")
	flags.BoolVar(&cfg.shouldAllowLargeDownload, "should_allow_large_download", false, "Allow multi-GB UniProt ID mapping downloads")
	cliopt.BindRuleExistingFlag(flags, &cfg.ExistingRuleConfig, "Rule for existing files: skip|overwrite")
	cliopt.BindRetryFlags(flags, &cfg.RetryConfig, &retryWaitSec)
	cliopt.BindDownloadControlFlags(flags, &cfg.DownloadControlConfig, &requestIntervalMs)
	cliopt.BindInsecureTLSFlag(flags, &cfg.InsecureTLSConfig, "Disable TLS certificate verification")
	cliopt.BindDryRunFlag(flags, &cfg.DryRunConfig, "Print actions only; do not download")
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
			if err := cliopt.ValidateDirOutRequired(cfg.DirOut); err != nil {
				return err
			}
			if err := cliopt.ValidateVersionRequired(cfg.VersionToken); err != nil {
				return err
			}
			return runLockIDMapping(&cfg)
		},
	}
	flags := commandLock.Flags()
	flags.SortFlags = false
	cliopt.BindDirOutFlag(flags, &cfg.DirOutConfig, "UniProt asset root directory")
	cliopt.BindVersionFlag(flags, &cfg.VersionConfig, "UniProt ID mapping version token")
	cliopt.BindDryRunFlag(flags, &cfg.DryRunConfig, "Print actions only; do not write manifest")
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
	cliopt.BindProgressFlag(flags, &cfg.ProgressConfig, "Disable download progress display")
	return commandSync
}
