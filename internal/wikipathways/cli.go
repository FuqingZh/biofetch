package wikipathways

import (
	"biofetch/internal/shared/cliopt"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

func NewCommand() *cobra.Command {
	commandRoot := &cobra.Command{
		Use:           "wikipathways",
		Short:         "Manage WikiPathways raw assets and manifest.lock",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	commandRoot.AddCommand(createGMTCommand())
	return commandRoot
}

func createGMTCommand() *cobra.Command {
	commandGMT := &cobra.Command{
		Use:           "gmt",
		Short:         "Manage WikiPathways GMT raw assets",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	commandGMT.AddCommand(createGMTFetchCommand())
	commandGMT.AddCommand(createGMTLockCommand())
	commandGMT.AddCommand(createGMTSyncCommand())
	return commandGMT
}

func createGMTFetchCommand() *cobra.Command {
	cfg := createDefaultGMTConfig()
	retryWaitSec := 3
	requestIntervalMs := 0

	commandFetch := &cobra.Command{
		Use:           "fetch",
		Short:         "Fetch WikiPathways GMT raw assets and update manifest.lock",
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg.RetryWait = time.Duration(retryWaitSec) * time.Second
			cfg.RequestInterval = time.Duration(requestIntervalMs) * time.Millisecond
			if err := validateGMTConfig(&cfg); err != nil {
				return err
			}
			return runFetchGMT(&cfg, cmd.InOrStdin(), cmd.ErrOrStderr())
		},
	}
	commandFetch.Example = strings.Join([]string{
		"biofetch wikipathways gmt fetch --dir_out /data/wikipathways --species Homo_sapiens --should_dry_run",
		"biofetch wikipathways gmt fetch --dir_out /data/wikipathways --species @species.txt",
		"biofetch wikipathways gmt fetch --dir_out /data/wikipathways --should_download_all_organisms",
	}, "\n")

	flags := commandFetch.Flags()
	flags.SortFlags = false
	cliopt.BindDirOutFlag(flags, &cfg.DirOutConfig, "WikiPathways asset root directory")
	cliopt.BindVersionFlag(flags, &cfg.VersionConfig, "WikiPathways release token; only current/empty is supported in this implementation")
	flags.StringSliceVar(&cfg.speciesNames, "species", nil, "WikiPathways species labels; repeat the flag, use commas, or use @file")
	flags.BoolVar(&cfg.shouldDownloadAll, "should_download_all_organisms", false, "Fetch GMT files for all species in the current release")
	cliopt.BindRuleExistingFlag(flags, &cfg.ExistingRuleConfig, "Rule for existing files: skip|overwrite")
	cliopt.BindRetryFlags(flags, &cfg.RetryConfig, &retryWaitSec)
	cliopt.BindDownloadControlFlags(flags, &cfg.DownloadControlConfig, &requestIntervalMs)
	cliopt.BindInsecureTLSFlag(flags, &cfg.InsecureTLSConfig, "Disable TLS certificate verification")
	cliopt.BindDryRunFlag(flags, &cfg.DryRunConfig, "Print actions only; do not download")
	cliopt.BindLogDirFlag(flags, &cfg.LogConfig, "Directory for run log files; default is <version>/logs/")
	cliopt.BindProgressFlag(flags, &cfg.ProgressConfig, "Disable download progress display")
	return commandFetch
}

func createGMTLockCommand() *cobra.Command {
	cfg := gmtLockConfig{}
	commandLock := &cobra.Command{
		Use:           "lock",
		Short:         "Rebuild WikiPathways GMT manifest.lock from the current version directory",
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
			return runLockGMT(&cfg)
		},
	}
	flags := commandLock.Flags()
	flags.SortFlags = false
	cliopt.BindDirOutFlag(flags, &cfg.DirOutConfig, "WikiPathways asset root directory")
	cliopt.BindVersionFlag(flags, &cfg.VersionConfig, "WikiPathways GMT version token")
	cliopt.BindDryRunFlag(flags, &cfg.DryRunConfig, "Print actions only; do not write manifest")
	cliopt.BindLogDirFlag(flags, &cfg.LogConfig, "Directory for run log files; default is <version>/logs/")
	return commandLock
}

func createGMTSyncCommand() *cobra.Command {
	cfg := gmtSyncConfig{}
	cfg.RetryMax = 5
	cfg.RetryWait = 3 * time.Second
	cfg.RuleExisting = "skip"
	cfg.WorkersMax = 1
	retryWaitSec := 3
	requestIntervalMs := 0

	commandSync := &cobra.Command{
		Use:           "sync",
		Short:         "Sync WikiPathways GMT files from manifest.lock and refresh manifest",
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
			return runSyncGMT(&cfg)
		},
	}
	flags := commandSync.Flags()
	flags.SortFlags = false
	cliopt.BindDirOutFlag(flags, &cfg.DirOutConfig, "WikiPathways asset root directory")
	cliopt.BindVersionFlag(flags, &cfg.VersionConfig, "WikiPathways GMT version token")
	cliopt.BindRuleExistingFlag(flags, &cfg.ExistingRuleConfig, "Rule for existing files: skip|overwrite")
	cliopt.BindRetryFlags(flags, &cfg.RetryConfig, &retryWaitSec)
	cliopt.BindDownloadControlFlags(flags, &cfg.DownloadControlConfig, &requestIntervalMs)
	cliopt.BindInsecureTLSFlag(flags, &cfg.InsecureTLSConfig, "Disable TLS certificate verification")
	cliopt.BindDryRunFlag(flags, &cfg.DryRunConfig, "Print actions only; do not download")
	cliopt.BindLogDirFlag(flags, &cfg.LogConfig, "Directory for run log files; default is <version>/logs/")
	cliopt.BindProgressFlag(flags, &cfg.ProgressConfig, "Disable download progress display")
	return commandSync
}
