package interpro

import (
	"biofetch/internal/shared/cliopt"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

func NewCommand() *cobra.Command {
	commandRoot := &cobra.Command{
		Use:           "interpro",
		Short:         "Manage InterPro raw assets and manifest.lock",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	commandRoot.AddCommand(createMappingCommand())
	return commandRoot
}

func createMappingCommand() *cobra.Command {
	commandMapping := &cobra.Command{
		Use:           "mapping",
		Short:         "Manage InterPro protein-domain mapping raw assets",
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
		Short:         "Fetch InterPro mapping raw assets and update manifest.lock",
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := validateMappingConfig(&cfg); err != nil {
				return err
			}
			return runFetchMapping(&cfg)
		},
	}
	commandFetch.Example = strings.Join([]string{
		"biofetch interpro mapping fetch --output /data/interpro --assets entries",
		"biofetch interpro mapping fetch --output /data/interpro --assets protein2ipr --allow-large-downloads",
		"biofetch interpro mapping fetch --output /data/interpro --allow-large-downloads",
	}, "\n")

	flags := commandFetch.Flags()
	flags.SortFlags = false
	cliopt.BindDirOutFlag(flags, &cfg.DirOutConfig, "InterPro asset root directory")
	cliopt.BindVersionFlag(flags, &cfg.VersionConfig, "InterPro release token; omit for current")
	cliopt.BindStringListFlags(flags, &cfg.assetNames, "assets", "InterPro mapping assets: all|protein2ipr|entries; omit or pass all to fetch all supported assets")
	flags.BoolVar(&cfg.shouldAllowLargeAssets, "allow-large-downloads", false, "Allow large InterPro mapping assets")
	flags.StringVar(&cfg.baseURLCurrentRelease, "current-release-url", cfg.baseURLCurrentRelease, "InterPro current_release base URL, including mirror URLs")
	cliopt.BindRuleExistingFlag(flags, &cfg.ExistingRuleConfig, "Rule for existing files: skip|overwrite")
	cliopt.BindRetryFlags(flags, &cfg.RetryConfig)
	cliopt.BindDownloadControlFlags(flags, &cfg.DownloadControlConfig)
	cliopt.BindInsecureTLSFlag(flags, &cfg.InsecureTLSConfig, "Disable TLS certificate verification")
	cliopt.BindDryRunFlag(flags, &cfg.DryRunConfig, "Print actions only; do not download")
	cliopt.BindLogDirFlag(flags, &cfg.LogConfig, "Directory for run log files; default is <version>/logs/")
	cliopt.BindProgressFlag(flags, &cfg.ProgressConfig, "Disable download progress display")
	return commandFetch
}

func createMappingLockCommand() *cobra.Command {
	cfg := mappingLockConfig{}
	commandLock := &cobra.Command{
		Use:           "lock SNAPSHOT",
		Short:         "Rebuild InterPro mapping manifest.lock from the current version directory",
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg.DirSnapshot = args[0]
			return runLockMapping(&cfg)
		},
	}
	flags := commandLock.Flags()
	flags.SortFlags = false
	cliopt.BindLockWorkersFlag(flags, &cfg.workersMax)
	cliopt.BindDryRunFlag(flags, &cfg.DryRunConfig, "Print actions only; do not write manifest")
	cliopt.BindLogDirFlag(flags, &cfg.LogConfig, "Directory for run log files; default is <version>/logs/")
	return commandLock
}

func createMappingRestoreCommand() *cobra.Command {
	cfg := mappingRestoreConfig{}
	cfg.RetryMax = 5
	cfg.RetryWait = 3 * time.Second
	cfg.RuleExisting = "skip"
	cfg.WorkersMax = 1

	commandRestore := &cobra.Command{
		Use:           "restore SNAPSHOT",
		Short:         "Restore InterPro mapping files from manifest.lock and refresh manifest",
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := cliopt.ApplyFlatSnapshot(&cfg.DirOutConfig, &cfg.VersionConfig, args[0], "mapping"); err != nil {
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
			return runRestoreMapping(&cfg)
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
