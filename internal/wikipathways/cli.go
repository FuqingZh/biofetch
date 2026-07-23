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
	commandGMT.AddCommand(createGMTRestoreCommand())
	return commandGMT
}

func createGMTFetchCommand() *cobra.Command {
	cfg := createDefaultGMTConfig()

	commandFetch := &cobra.Command{
		Use:           "fetch",
		Short:         "Fetch WikiPathways GMT raw assets and update manifest.lock",
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := validateGMTConfig(&cfg); err != nil {
				return err
			}
			return runFetchGMT(&cfg, cmd.InOrStdin(), cmd.ErrOrStderr())
		},
	}
	commandFetch.Example = strings.Join([]string{
		"biofetch wikipathways gmt fetch --output /data/wikipathways --species Homo_sapiens --dry-run",
		"biofetch wikipathways gmt fetch --output /data/wikipathways --species @species.txt",
		"biofetch wikipathways gmt fetch --output /data/wikipathways --all-organisms",
	}, "\n")

	flags := commandFetch.Flags()
	flags.SortFlags = false
	cliopt.BindDirOutFlag(flags, &cfg.DirOutConfig, "WikiPathways asset root directory")
	cliopt.BindVersionFlag(flags, &cfg.VersionConfig, "WikiPathways release token; only current/empty is supported in this implementation")
	cliopt.BindStringListFlags(flags, &cfg.speciesNames, "species", "WikiPathways species labels; repeat the flag,")
	flags.BoolVar(&cfg.shouldDownloadAll, "all-organisms", false, "Fetch GMT files for all species in the current release")
	cliopt.BindRuleExistingFlag(flags, &cfg.ExistingRuleConfig, "Rule for existing files: skip|overwrite")
	cliopt.BindRetryFlags(flags, &cfg.RetryConfig)
	cliopt.BindDownloadControlFlags(flags, &cfg.DownloadControlConfig)
	cliopt.BindInsecureTLSFlag(flags, &cfg.InsecureTLSConfig, "Disable TLS certificate verification")
	cliopt.BindDryRunFlag(flags, &cfg.DryRunConfig, "Print actions only; do not download")
	cliopt.BindLogDirFlag(flags, &cfg.LogConfig, "Directory for run log files; default is <version>/logs/")
	cliopt.BindProgressFlag(flags, &cfg.ProgressConfig, "Disable download progress display")
	return commandFetch
}

func createGMTLockCommand() *cobra.Command {
	cfg := gmtLockConfig{}
	commandLock := &cobra.Command{
		Use:           "lock SNAPSHOT",
		Short:         "Rebuild WikiPathways GMT manifest.lock from the current version directory",
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg.DirSnapshot = args[0]
			return runLockGMT(&cfg)
		},
	}
	flags := commandLock.Flags()
	flags.SortFlags = false
	cliopt.BindLockWorkersFlag(flags, &cfg.workersMax)
	cliopt.BindDryRunFlag(flags, &cfg.DryRunConfig, "Print actions only; do not write manifest")
	cliopt.BindLogDirFlag(flags, &cfg.LogConfig, "Directory for run log files; default is <version>/logs/")
	return commandLock
}

func createGMTRestoreCommand() *cobra.Command {
	cfg := gmtRestoreConfig{}
	cfg.RetryMax = 5
	cfg.RetryWait = 3 * time.Second
	cfg.RuleExisting = "skip"
	cfg.WorkersMax = 1

	commandRestore := &cobra.Command{
		Use:           "restore SNAPSHOT",
		Short:         "Restore WikiPathways GMT files from manifest.lock and refresh manifest",
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := cliopt.ApplyFlatSnapshot(&cfg.DirOutConfig, &cfg.VersionConfig, args[0], "gmt"); err != nil {
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
			return runRestoreGMT(&cfg)
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
