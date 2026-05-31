package eggnog

import (
	"biofetch/internal/shared/cliopt"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

func NewCommand() *cobra.Command {
	commandRoot := &cobra.Command{
		Use:           "eggnog",
		Short:         "Manage eggNOG raw assets and manifest.lock",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	commandRoot.AddCommand(createCOGCommand())
	commandRoot.AddCommand(createMapperCommand())
	return commandRoot
}

func createCOGCommand() *cobra.Command {
	commandCOG := &cobra.Command{
		Use:           "cog",
		Short:         "Manage NCBI COG raw definition assets",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	commandCOG.AddCommand(createCOGFetchCommand())
	commandCOG.AddCommand(createCOGLockCommand())
	commandCOG.AddCommand(createCOGSyncCommand())
	return commandCOG
}

func createCOGFetchCommand() *cobra.Command {
	cfg := createDefaultCOGConfig()
	retryWaitSec := 3
	requestIntervalMs := 0

	commandFetch := &cobra.Command{
		Use:           "fetch",
		Short:         "Fetch NCBI COG definition raw assets and update manifest.lock",
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg.RetryWait = time.Duration(retryWaitSec) * time.Second
			cfg.RequestInterval = time.Duration(requestIntervalMs) * time.Millisecond
			if err := validateCOGConfig(&cfg); err != nil {
				return err
			}
			return runFetchCOG(&cfg)
		},
	}
	commandFetch.Example = strings.Join([]string{
		"biofetch eggnog cog fetch --dir_out /data/eggnog --version COG2024 --assets category_definition",
		"biofetch eggnog cog fetch --dir_out /data/eggnog --version 2024 --assets category_definition,definition,readme",
	}, "\n")

	flags := commandFetch.Flags()
	flags.SortFlags = false
	cliopt.BindDirOutFlag(flags, &cfg.DirOutConfig, "eggNOG asset root directory")
	cliopt.BindVersionFlag(flags, &cfg.VersionConfig, "NCBI COG release token; default COG2024")
	flags.StringSliceVar(&cfg.assetNames, "assets", nil, "COG assets: category_definition|definition|readme; repeat the flag, use commas, or use @file")
	flags.StringVar(&cfg.baseURL, "base_url", cfg.baseURL, "NCBI COG base URL")
	cliopt.BindRuleExistingFlag(flags, &cfg.ExistingRuleConfig, "Rule for existing files: skip|overwrite")
	cliopt.BindRetryFlags(flags, &cfg.RetryConfig, &retryWaitSec)
	cliopt.BindDownloadControlFlags(flags, &cfg.DownloadControlConfig, &requestIntervalMs)
	cliopt.BindInsecureTLSFlag(flags, &cfg.InsecureTLSConfig, "Disable TLS certificate verification")
	cliopt.BindDryRunFlag(flags, &cfg.DryRunConfig, "Print actions only; do not download")
	cliopt.BindProgressFlag(flags, &cfg.ProgressConfig, "Disable download progress display")
	return commandFetch
}

func createCOGLockCommand() *cobra.Command {
	cfg := cogLockConfig{}
	commandLock := &cobra.Command{
		Use:           "lock",
		Short:         "Rebuild COG manifest.lock from the current version directory",
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
			return runLockCOG(&cfg)
		},
	}
	flags := commandLock.Flags()
	flags.SortFlags = false
	cliopt.BindDirOutFlag(flags, &cfg.DirOutConfig, "eggNOG asset root directory")
	cliopt.BindVersionFlag(flags, &cfg.VersionConfig, "NCBI COG release token")
	cliopt.BindDryRunFlag(flags, &cfg.DryRunConfig, "Print actions only; do not write manifest")
	return commandLock
}

func createCOGSyncCommand() *cobra.Command {
	cfg := cogSyncConfig{}
	cfg.RetryMax = 5
	cfg.RetryWait = 3 * time.Second
	cfg.RuleExisting = "skip"
	cfg.WorkersMax = 1
	retryWaitSec := 3
	requestIntervalMs := 0

	commandSync := &cobra.Command{
		Use:           "sync",
		Short:         "Sync COG files from manifest.lock and refresh manifest",
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
			return runSyncCOG(&cfg)
		},
	}
	flags := commandSync.Flags()
	flags.SortFlags = false
	cliopt.BindDirOutFlag(flags, &cfg.DirOutConfig, "eggNOG asset root directory")
	cliopt.BindVersionFlag(flags, &cfg.VersionConfig, "NCBI COG release token")
	cliopt.BindRuleExistingFlag(flags, &cfg.ExistingRuleConfig, "Rule for existing files: skip|overwrite")
	cliopt.BindRetryFlags(flags, &cfg.RetryConfig, &retryWaitSec)
	cliopt.BindDownloadControlFlags(flags, &cfg.DownloadControlConfig, &requestIntervalMs)
	cliopt.BindInsecureTLSFlag(flags, &cfg.InsecureTLSConfig, "Disable TLS certificate verification")
	cliopt.BindDryRunFlag(flags, &cfg.DryRunConfig, "Print actions only; do not download")
	cliopt.BindProgressFlag(flags, &cfg.ProgressConfig, "Disable download progress display")
	return commandSync
}

func createMapperCommand() *cobra.Command {
	commandMapper := &cobra.Command{
		Use:           "mapper",
		Short:         "Manage eggNOG-mapper raw database assets",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	commandMapper.AddCommand(createMapperFetchCommand())
	commandMapper.AddCommand(createMapperLockCommand())
	commandMapper.AddCommand(createMapperSyncCommand())
	return commandMapper
}

func createMapperFetchCommand() *cobra.Command {
	cfg := createDefaultMapperConfig()
	retryWaitSec := 3
	requestIntervalMs := 0

	commandFetch := &cobra.Command{
		Use:           "fetch",
		Short:         "Fetch eggNOG-mapper raw database assets and update manifest.lock",
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg.RetryWait = time.Duration(retryWaitSec) * time.Second
			cfg.RequestInterval = time.Duration(requestIntervalMs) * time.Millisecond
			if err := validateMapperConfig(&cfg); err != nil {
				return err
			}
			return runFetchMapper(&cfg)
		},
	}
	commandFetch.Example = strings.Join([]string{
		"biofetch eggnog mapper fetch --dir_out /data/eggnog --version 5 --assets taxa --should_allow_large_download",
		"biofetch eggnog mapper fetch --dir_out /data/eggnog --version 5.0.2 --assets db,taxa,diamond --should_allow_large_download",
	}, "\n")

	flags := commandFetch.Flags()
	flags.SortFlags = false
	cliopt.BindDirOutFlag(flags, &cfg.DirOutConfig, "eggNOG asset root directory")
	cliopt.BindVersionFlag(flags, &cfg.VersionConfig, "eggNOG-mapper database version; default 5.0.2")
	flags.StringSliceVar(&cfg.assetNames, "assets", nil, "eggNOG-mapper assets: db|taxa|diamond; repeat the flag, use commas, or use @file")
	flags.BoolVar(&cfg.shouldAllowLargeDownload, "should_allow_large_download", false, "Allow large eggNOG-mapper database downloads")
	flags.StringVar(&cfg.baseURL, "base_url", cfg.baseURL, "eggNOG-mapper download base URL")
	cliopt.BindRuleExistingFlag(flags, &cfg.ExistingRuleConfig, "Rule for existing files: skip|overwrite")
	cliopt.BindRetryFlags(flags, &cfg.RetryConfig, &retryWaitSec)
	cliopt.BindDownloadControlFlags(flags, &cfg.DownloadControlConfig, &requestIntervalMs)
	cliopt.BindInsecureTLSFlag(flags, &cfg.InsecureTLSConfig, "Disable TLS certificate verification")
	cliopt.BindDryRunFlag(flags, &cfg.DryRunConfig, "Print actions only; do not download")
	cliopt.BindProgressFlag(flags, &cfg.ProgressConfig, "Disable download progress display")
	return commandFetch
}

func createMapperLockCommand() *cobra.Command {
	cfg := mapperLockConfig{}
	commandLock := &cobra.Command{
		Use:           "lock",
		Short:         "Rebuild eggNOG-mapper manifest.lock from the current version directory",
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
			return runLockMapper(&cfg)
		},
	}
	flags := commandLock.Flags()
	flags.SortFlags = false
	cliopt.BindDirOutFlag(flags, &cfg.DirOutConfig, "eggNOG asset root directory")
	cliopt.BindVersionFlag(flags, &cfg.VersionConfig, "eggNOG-mapper database version")
	cliopt.BindDryRunFlag(flags, &cfg.DryRunConfig, "Print actions only; do not write manifest")
	return commandLock
}

func createMapperSyncCommand() *cobra.Command {
	cfg := mapperSyncConfig{}
	cfg.RetryMax = 5
	cfg.RetryWait = 3 * time.Second
	cfg.RuleExisting = "skip"
	cfg.WorkersMax = 1
	retryWaitSec := 3
	requestIntervalMs := 0

	commandSync := &cobra.Command{
		Use:           "sync",
		Short:         "Sync eggNOG-mapper files from manifest.lock and refresh manifest",
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
			return runSyncMapper(&cfg)
		},
	}
	flags := commandSync.Flags()
	flags.SortFlags = false
	cliopt.BindDirOutFlag(flags, &cfg.DirOutConfig, "eggNOG asset root directory")
	cliopt.BindVersionFlag(flags, &cfg.VersionConfig, "eggNOG-mapper database version")
	cliopt.BindRuleExistingFlag(flags, &cfg.ExistingRuleConfig, "Rule for existing files: skip|overwrite")
	cliopt.BindRetryFlags(flags, &cfg.RetryConfig, &retryWaitSec)
	cliopt.BindDownloadControlFlags(flags, &cfg.DownloadControlConfig, &requestIntervalMs)
	cliopt.BindInsecureTLSFlag(flags, &cfg.InsecureTLSConfig, "Disable TLS certificate verification")
	cliopt.BindDryRunFlag(flags, &cfg.DryRunConfig, "Print actions only; do not download")
	cliopt.BindProgressFlag(flags, &cfg.ProgressConfig, "Disable download progress display")
	return commandSync
}
