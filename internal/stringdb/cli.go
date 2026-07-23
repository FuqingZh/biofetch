package stringdb

import (
	"biofetch/internal/shared/cliopt"
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

type config struct {
	dirOut                  string
	dirLogs                 string
	versionToken            string
	taxIDs                  []string
	shouldDownloadAll       bool
	ruleExisting            string
	shouldOverwriteExisting bool
	retryMax                int
	retryWait               time.Duration
	cliopt.DownloadControlConfig
	shouldAllowInsecureTLS bool
	shouldDryRun           bool
}

func NewCommand() *cobra.Command {
	commandString := &cobra.Command{
		Use:           "string",
		Short:         "Manage STRING raw assets and manifest.lock",
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}

	commandString.AddCommand(createCatalogCommand())
	commandString.AddCommand(createNetworkCommand())
	return commandString
}

func createNetworkCommand() *cobra.Command {
	commandNetwork := &cobra.Command{
		Use:           "network",
		Short:         "Manage STRING protein network assets",
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}

	commandNetwork.AddCommand(createFetchCommand())
	commandNetwork.AddCommand(createLockCommand())
	commandNetwork.AddCommand(createSyncCommand())
	return commandNetwork
}

func createCatalogCommand() *cobra.Command {
	commandCatalog := &cobra.Command{
		Use:           "catalog",
		Short:         "Manage STRING shared catalog assets",
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}

	commandCatalog.AddCommand(createCatalogFetchCommand())
	commandCatalog.AddCommand(createCatalogLockCommand())
	commandCatalog.AddCommand(createCatalogSyncCommand())
	return commandCatalog
}

func createFetchCommand() *cobra.Command {
	cfg := createDefaultConfig()

	commandFetch := &cobra.Command{
		Use:           "fetch",
		Short:         "Fetch STRING raw assets and update manifest.lock",
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := validateConfig(cfg); err != nil {
				return err
			}
			return runFetch(cfg)
		},
	}

	commandFetch.Example = strings.Join([]string{
		"biofetch string network fetch --output /data/string --taxon-ids 7070 --dry-run",
		"biofetch string network fetch --output /data/string --taxon-ids 7070 --taxon-ids 9606",
		"biofetch string network fetch --output /data/string --taxon-ids @taxids.txt --version v12.0",
	}, "\n")

	flags := commandFetch.Flags()
	flags.SortFlags = false
	flags.StringVarP(&cfg.dirOut, "output", "o", cfg.dirOut, "STRING asset root directory")
	flags.StringVar(&cfg.versionToken, "version", cfg.versionToken, "STRING release version token")
	cliopt.BindStringListFlags(flags, &cfg.taxIDs, "taxon-ids", "Taxids; pass inline values, repeat the flag,")
	flags.BoolVar(
		&cfg.shouldDownloadAll,
		"all-organisms",
		false,
		"Download all species listed by STRING",
	)
	flags.StringVar(&cfg.ruleExisting, "on-existing", cfg.ruleExisting, "Rule for existing files: skip|overwrite")
	flags.IntVar(&cfg.retryMax, "max-attempts", cfg.retryMax, "Max retry attempts on download failures")
	flags.DurationVar(&cfg.retryWait, "retry-wait", cfg.retryWait, "Wait between download attempts")
	cliopt.BindDownloadControlFlags(flags, &cfg.DownloadControlConfig)
	flags.BoolVar(
		&cfg.shouldAllowInsecureTLS,
		"insecure",
		false,
		"Disable TLS certificate verification",
	)
	flags.BoolVar(&cfg.shouldDryRun, "dry-run", false, "Print actions only; do not download")
	flags.StringVar(&cfg.dirLogs, "log-dir", cfg.dirLogs, "Directory for run log files; default is <version>/logs/")

	return commandFetch
}

func createLockCommand() *cobra.Command {
	cfg := lockConfig{}

	commandLock := &cobra.Command{
		Use:           "lock SNAPSHOT",
		Short:         "Rebuild STRING manifest.lock from the current version directory",
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg.dirSnapshot = args[0]
			return runLock(&cfg)
		},
	}

	flags := commandLock.Flags()
	flags.SortFlags = false
	cliopt.BindLockWorkersFlag(flags, &cfg.workersMax)
	flags.BoolVar(&cfg.shouldDryRun, "dry-run", false, "Print actions only; do not write manifest")
	flags.StringVar(&cfg.dirLogs, "log-dir", cfg.dirLogs, "Directory for run log files; default is <version>/logs/")
	return commandLock
}

func createSyncCommand() *cobra.Command {
	cfg := restoreConfig{}
	cfg.retryMax = 5
	cfg.retryWait = 3 * time.Second
	cfg.ruleExisting = "skip"
	cfg.WorkersMax = 1

	commandRestore := &cobra.Command{
		Use:           "restore SNAPSHOT",
		Short:         "Restore STRING files from manifest.lock and refresh manifest",
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			var err error
			cfg.dirOut, cfg.versionToken, err = cliopt.FlatSnapshotRootVersion(args[0], "network")
			if err != nil {
				return err
			}
			if strings.TrimSpace(cfg.dirOut) == "" {
				return fmt.Errorf("output is required")
			}
			if strings.TrimSpace(cfg.versionToken) == "" {
				return fmt.Errorf("version is required")
			}
			if cfg.retryMax < 1 {
				return fmt.Errorf("max-attempts must be >= 1")
			}
			if err := cliopt.ValidateDownloadControlConfig(&cfg.DownloadControlConfig); err != nil {
				return err
			}
			if cfg.ruleExisting != "skip" && cfg.ruleExisting != "overwrite" {
				return fmt.Errorf("on-existing must be one of: skip, overwrite")
			}
			cfg.shouldOverwriteExisting = cfg.ruleExisting == "overwrite"
			if cfg.retryWait < 0 {
				return fmt.Errorf("retry-wait must be >= 0")
			}
			return runRestore(&cfg)
		},
	}

	flags := commandRestore.Flags()
	flags.SortFlags = false
	flags.StringVar(&cfg.ruleExisting, "on-existing", cfg.ruleExisting, "Rule for existing files: skip|overwrite")
	flags.IntVar(&cfg.retryMax, "max-attempts", cfg.retryMax, "Max retry attempts on download failures")
	flags.DurationVar(&cfg.retryWait, "retry-wait", cfg.retryWait, "Wait between download attempts")
	cliopt.BindDownloadControlFlags(flags, &cfg.DownloadControlConfig)
	flags.BoolVar(&cfg.shouldAllowInsecureTLS, "insecure", false, "Disable TLS certificate verification")
	flags.BoolVar(&cfg.shouldDryRun, "dry-run", false, "Print actions only; do not download")
	flags.StringVar(&cfg.dirLogs, "log-dir", cfg.dirLogs, "Directory for run log files; default is <version>/logs/")
	return commandRestore
}

func createDefaultConfig() *config {
	cfg := &config{}
	cfg.versionToken = "v12.0"
	cfg.ruleExisting = "skip"
	cfg.retryMax = 5
	cfg.retryWait = 3 * time.Second
	cfg.WorkersMax = 1
	return cfg
}

func validateConfig(cfg *config) error {
	if cfg.retryMax < 1 {
		return fmt.Errorf("max-attempts must be >= 1")
	}
	if cfg.retryWait < 0 {
		return fmt.Errorf("retry-wait must be >= 0")
	}
	if err := cliopt.ValidateDownloadControlConfig(&cfg.DownloadControlConfig); err != nil {
		return err
	}
	if strings.TrimSpace(cfg.dirOut) == "" {
		return fmt.Errorf("output is required")
	}
	if cfg.ruleExisting != "skip" && cfg.ruleExisting != "overwrite" {
		return fmt.Errorf("on-existing must be one of: skip, overwrite")
	}
	cfg.shouldOverwriteExisting = cfg.ruleExisting == "overwrite"

	countSources := 0
	if cfg.shouldDownloadAll {
		countSources++
	}
	if len(cfg.taxIDs) > 0 {
		countSources++
	}
	if countSources != 1 {
		return fmt.Errorf(
			"choose exactly one source: --taxon-ids | --all-organisms",
		)
	}
	if len(cfg.taxIDs) > 0 {
		taxIDs, err := parseTaxIDs(cfg.taxIDs)
		if err != nil {
			return err
		}
		cfg.taxIDs = taxIDs
	}

	return nil
}
