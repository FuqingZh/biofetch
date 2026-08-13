package eggnog

import (
	"github.com/FuqingZh/biofetch/internal/shared/cliopt"
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
	commandCOG.AddCommand(createCOGRestoreCommand())
	return commandCOG
}

func createCOGFetchCommand() *cobra.Command {
	cfg := createDefaultCOGConfig()

	commandFetch := &cobra.Command{
		Use:           "fetch",
		Short:         "Fetch NCBI COG definition raw assets and update manifest.lock",
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := validateCOGConfig(&cfg); err != nil {
				return err
			}
			return runFetchCOG(&cfg)
		},
	}
	commandFetch.Example = strings.Join([]string{
		"biofetch eggnog cog fetch --output /data/eggnog --version COG2024 --assets category_definition",
		"biofetch eggnog cog fetch --output /data/eggnog --version 2024",
	}, "\n")

	flags := commandFetch.Flags()
	flags.SortFlags = false
	cliopt.BindDirOutFlag(flags, &cfg.DirOutConfig, "eggNOG asset root directory")
	cliopt.BindVersionFlag(flags, &cfg.VersionConfig, "NCBI COG release token; default COG2024")
	cliopt.BindStringListFlags(flags, &cfg.assetNames, "assets", "COG assets: all|category_definition|definition|readme; omit or pass all to fetch all supported assets")
	flags.StringVar(&cfg.baseURL, "base-url", cfg.baseURL, "NCBI COG base URL")
	cliopt.BindRuleExistingFlag(flags, &cfg.ExistingRuleConfig, "Rule for existing files: skip|overwrite")
	cliopt.BindRetryFlags(flags, &cfg.RetryConfig)
	cliopt.BindDownloadControlFlags(flags, &cfg.DownloadControlConfig)
	cliopt.BindInsecureTLSFlag(flags, &cfg.InsecureTLSConfig, "Disable TLS certificate verification")
	cliopt.BindDryRunFlag(flags, &cfg.DryRunConfig, "Print actions only; do not download")
	cliopt.BindLogDirFlag(flags, &cfg.LogConfig, "Directory for run log files; default is <version>/logs/")
	cliopt.BindProgressFlag(flags, &cfg.ProgressConfig, "Disable download progress display")
	return commandFetch
}

func createCOGLockCommand() *cobra.Command {
	cfg := cogLockConfig{}
	commandLock := &cobra.Command{
		Use:           "lock SNAPSHOT",
		Short:         "Rebuild COG manifest.lock from the current version directory",
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg.DirSnapshot = args[0]
			return runLockCOG(&cfg)
		},
	}
	flags := commandLock.Flags()
	flags.SortFlags = false
	cliopt.BindLockWorkersFlag(flags, &cfg.workersMax)
	cliopt.BindDryRunFlag(flags, &cfg.DryRunConfig, "Print actions only; do not write manifest")
	cliopt.BindLogDirFlag(flags, &cfg.LogConfig, "Directory for run log files; default is <version>/logs/")
	return commandLock
}

func createCOGRestoreCommand() *cobra.Command {
	cfg := cogRestoreConfig{}
	cfg.RetryMax = 5
	cfg.RetryWait = 3 * time.Second
	cfg.RuleExisting = "skip"
	cfg.WorkersMax = 1

	commandRestore := &cobra.Command{
		Use:           "restore SNAPSHOT",
		Short:         "Restore COG files from manifest.lock and refresh manifest",
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := cliopt.ApplyFlatSnapshot(&cfg.DirOutConfig, &cfg.VersionConfig, args[0], "cog"); err != nil {
				return err
			}
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
			return runRestoreCOG(&cfg)
		},
	}
	flags := commandRestore.Flags()
	flags.SortFlags = false
	cliopt.BindRuleExistingFlag(flags, &cfg.ExistingRuleConfig, "Rule for existing files: skip|overwrite")
	cliopt.BindRetryFlags(flags, &cfg.RetryConfig)
	cliopt.BindDownloadControlFlags(flags, &cfg.DownloadControlConfig)
	cliopt.BindInsecureTLSFlag(flags, &cfg.InsecureTLSConfig, "Disable TLS certificate verification")
	cliopt.BindDryRunFlag(flags, &cfg.DryRunConfig, "Print actions only; do not download")
	cliopt.BindLogDirFlag(flags, &cfg.LogConfig, "Directory for run log files; default is <version>/logs/")
	cliopt.BindProgressFlag(flags, &cfg.ProgressConfig, "Disable download progress display")
	return commandRestore
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
	commandMapper.AddCommand(createMapperRestoreCommand())
	return commandMapper
}

func createMapperFetchCommand() *cobra.Command {
	cfg := createDefaultMapperConfig()

	commandFetch := &cobra.Command{
		Use:           "fetch",
		Short:         "Fetch eggNOG-mapper raw database assets and update manifest.lock",
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := validateMapperConfig(&cfg); err != nil {
				return err
			}
			return runFetchMapper(&cfg)
		},
	}
	commandFetch.Example = strings.Join([]string{
		"biofetch eggnog mapper fetch --output /data/eggnog --version 5 --assets taxa --allow-large-downloads",
		"biofetch eggnog mapper fetch --output /data/eggnog --version 5.0.2 --allow-large-downloads",
	}, "\n")

	flags := commandFetch.Flags()
	flags.SortFlags = false
	cliopt.BindDirOutFlag(flags, &cfg.DirOutConfig, "eggNOG asset root directory")
	cliopt.BindVersionFlag(flags, &cfg.VersionConfig, "eggNOG-mapper database version; default 5.0.2")
	cliopt.BindStringListFlags(flags, &cfg.assetNames, "assets", "eggNOG-mapper assets: all|db|taxa|diamond; omit or pass all to fetch all supported assets")
	flags.BoolVar(&cfg.shouldAllowLargeAssets, "allow-large-downloads", false, "Allow large eggNOG-mapper assets")
	flags.StringVar(&cfg.baseURL, "base-url", cfg.baseURL, "eggNOG-mapper download base URL")
	cliopt.BindRuleExistingFlag(flags, &cfg.ExistingRuleConfig, "Rule for existing files: skip|overwrite")
	cliopt.BindRetryFlags(flags, &cfg.RetryConfig)
	cliopt.BindDownloadControlFlags(flags, &cfg.DownloadControlConfig)
	cliopt.BindInsecureTLSFlag(flags, &cfg.InsecureTLSConfig, "Disable TLS certificate verification")
	cliopt.BindDryRunFlag(flags, &cfg.DryRunConfig, "Print actions only; do not download")
	cliopt.BindLogDirFlag(flags, &cfg.LogConfig, "Directory for run log files; default is <version>/logs/")
	cliopt.BindProgressFlag(flags, &cfg.ProgressConfig, "Disable download progress display")
	return commandFetch
}

func createMapperLockCommand() *cobra.Command {
	cfg := mapperLockConfig{}
	commandLock := &cobra.Command{
		Use:           "lock SNAPSHOT",
		Short:         "Rebuild eggNOG-mapper manifest.lock from the current version directory",
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg.DirSnapshot = args[0]
			return runLockMapper(&cfg)
		},
	}
	flags := commandLock.Flags()
	flags.SortFlags = false
	cliopt.BindLockWorkersFlag(flags, &cfg.workersMax)
	cliopt.BindDryRunFlag(flags, &cfg.DryRunConfig, "Print actions only; do not write manifest")
	cliopt.BindLogDirFlag(flags, &cfg.LogConfig, "Directory for run log files; default is <version>/logs/")
	return commandLock
}

func createMapperRestoreCommand() *cobra.Command {
	cfg := mapperRestoreConfig{}
	cfg.RetryMax = 5
	cfg.RetryWait = 3 * time.Second
	cfg.RuleExisting = "skip"
	cfg.WorkersMax = 1

	commandRestore := &cobra.Command{
		Use:           "restore SNAPSHOT",
		Short:         "Restore eggNOG-mapper files from manifest.lock and refresh manifest",
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := cliopt.ApplyFlatSnapshot(&cfg.DirOutConfig, &cfg.VersionConfig, args[0], "mapper"); err != nil {
				return err
			}
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
			return runRestoreMapper(&cfg)
		},
	}
	flags := commandRestore.Flags()
	flags.SortFlags = false
	cliopt.BindRuleExistingFlag(flags, &cfg.ExistingRuleConfig, "Rule for existing files: skip|overwrite")
	cliopt.BindRetryFlags(flags, &cfg.RetryConfig)
	cliopt.BindDownloadControlFlags(flags, &cfg.DownloadControlConfig)
	cliopt.BindInsecureTLSFlag(flags, &cfg.InsecureTLSConfig, "Disable TLS certificate verification")
	cliopt.BindDryRunFlag(flags, &cfg.DryRunConfig, "Print actions only; do not download")
	cliopt.BindLogDirFlag(flags, &cfg.LogConfig, "Directory for run log files; default is <version>/logs/")
	cliopt.BindProgressFlag(flags, &cfg.ProgressConfig, "Disable download progress display")
	return commandRestore
}
