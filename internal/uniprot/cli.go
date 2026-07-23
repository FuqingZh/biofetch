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
	commandKB.AddCommand(createKBRestoreCommand())
	return commandKB
}

func createKBFetchCommand() *cobra.Command {
	cfg := createDefaultKBConfig()

	commandFetch := &cobra.Command{
		Use:           "fetch",
		Short:         "Fetch UniProtKB raw assets and update manifest.lock",
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := validateKBConfig(&cfg); err != nil {
				return err
			}
			return runFetchKB(&cfg)
		},
	}
	commandFetch.Example = strings.Join([]string{
		"biofetch uniprot kb fetch --output /data/uniprot --assets sprot",
		"biofetch uniprot kb fetch --output /data/uniprot --assets sprot,trembl --allow-large-downloads",
		"biofetch uniprot kb fetch --output /data/uniprot --assets varsplic",
		"biofetch uniprot kb fetch --output /data/uniprot --assets sprot_dat,trembl_dat --allow-large-downloads",
	}, "\n")

	flags := commandFetch.Flags()
	flags.SortFlags = false
	cliopt.BindDirOutFlag(flags, &cfg.DirOutConfig, "UniProt asset root directory")
	cliopt.BindVersionFlag(flags, &cfg.VersionConfig, "UniProt release token; omit for current")
	flags.StringSliceVar(&cfg.assetNames, "assets", nil, "UniProtKB assets: all|sprot|trembl|varsplic|sprot_dat|trembl_dat; omit or pass all to fetch all supported assets")
	cliopt.BindListFileFlag(flags, &cfg.assetNames, "assets")
	flags.BoolVar(&cfg.shouldAllowLargeAssets, "allow-large-downloads", false, "Allow large UniProtKB assets")
	flags.StringVar(&cfg.baseURLCurrentRelease, "current-release-url", cfg.baseURLCurrentRelease, "UniProt current_release base URL, including mirror URLs")
	cliopt.BindRuleExistingFlag(flags, &cfg.ExistingRuleConfig, "Rule for existing files: skip|overwrite")
	cliopt.BindRetryFlags(flags, &cfg.RetryConfig)
	cliopt.BindDownloadControlFlags(flags, &cfg.DownloadControlConfig)
	cliopt.BindInsecureTLSFlag(flags, &cfg.InsecureTLSConfig, "Disable TLS certificate verification")
	cliopt.BindDryRunFlag(flags, &cfg.DryRunConfig, "Print actions only; do not download")
	cliopt.BindLogDirFlag(flags, &cfg.LogConfig, "Directory for run log files; default is <version>/logs/")
	cliopt.BindProgressFlag(flags, &cfg.ProgressConfig, "Disable download progress display")
	return commandFetch
}

func createKBLockCommand() *cobra.Command {
	cfg := kbLockConfig{}
	commandLock := &cobra.Command{
		Use:           "lock SNAPSHOT",
		Short:         "Rebuild UniProtKB manifest.lock from the current version directory",
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg.DirSnapshot = args[0]
			return runLockKB(&cfg)
		},
	}
	flags := commandLock.Flags()
	flags.SortFlags = false
	cliopt.BindLockWorkersFlag(flags, &cfg.workersMax)
	cliopt.BindDryRunFlag(flags, &cfg.DryRunConfig, "Print actions only; do not write manifest")
	cliopt.BindLogDirFlag(flags, &cfg.LogConfig, "Directory for run log files; default is <version>/logs/")
	return commandLock
}

func createKBRestoreCommand() *cobra.Command {
	cfg := kbRestoreConfig{}
	cfg.RetryMax = 5
	cfg.RetryWait = 3 * time.Second
	cfg.RuleExisting = "skip"
	cfg.WorkersMax = 1

	commandRestore := &cobra.Command{
		Use:           "restore SNAPSHOT",
		Short:         "Restore UniProtKB files from manifest.lock and refresh manifest",
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := cliopt.ApplyFlatSnapshot(&cfg.DirOutConfig, &cfg.VersionConfig, args[0]); err != nil {
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
			return runRestoreKB(&cfg)
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

func createUniRefCommand() *cobra.Command {
	commandUniRef := &cobra.Command{
		Use:           "uniref",
		Short:         "Manage UniRef FASTA raw assets",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	commandUniRef.AddCommand(createUniRefFetchCommand())
	commandUniRef.AddCommand(createUniRefLockCommand())
	commandUniRef.AddCommand(createUniRefRestoreCommand())
	return commandUniRef
}

func createUniRefFetchCommand() *cobra.Command {
	cfg := createDefaultUniRefConfig()

	commandFetch := &cobra.Command{
		Use:           "fetch",
		Short:         "Fetch UniRef FASTA raw assets and update manifest.lock",
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := validateUniRefConfig(&cfg); err != nil {
				return err
			}
			return runFetchUniRef(&cfg)
		},
	}
	commandFetch.Example = strings.Join([]string{
		"biofetch uniprot uniref fetch --output /data/uniprot --assets uniref90 --allow-large-downloads",
		"biofetch uniprot uniref fetch --output /data/uniprot --allow-large-downloads",
	}, "\n")

	flags := commandFetch.Flags()
	flags.SortFlags = false
	cliopt.BindDirOutFlag(flags, &cfg.DirOutConfig, "UniProt asset root directory")
	cliopt.BindVersionFlag(flags, &cfg.VersionConfig, "UniProt release token; omit for current")
	flags.StringSliceVar(&cfg.assetNames, "assets", nil, "UniRef FASTA assets: all|uniref90; omit or pass all to fetch all supported assets")
	cliopt.BindListFileFlag(flags, &cfg.assetNames, "assets")
	flags.BoolVar(&cfg.shouldAllowLargeAssets, "allow-large-downloads", false, "Allow large UniRef FASTA assets")
	flags.StringVar(&cfg.baseURLCurrentRelease, "current-release-url", cfg.baseURLCurrentRelease, "UniProt current_release base URL, including mirror URLs")
	cliopt.BindRuleExistingFlag(flags, &cfg.ExistingRuleConfig, "Rule for existing files: skip|overwrite")
	cliopt.BindRetryFlags(flags, &cfg.RetryConfig)
	cliopt.BindDownloadControlFlags(flags, &cfg.DownloadControlConfig)
	cliopt.BindInsecureTLSFlag(flags, &cfg.InsecureTLSConfig, "Disable TLS certificate verification")
	cliopt.BindDryRunFlag(flags, &cfg.DryRunConfig, "Print actions only; do not download")
	cliopt.BindLogDirFlag(flags, &cfg.LogConfig, "Directory for run log files; default is <version>/logs/")
	cliopt.BindProgressFlag(flags, &cfg.ProgressConfig, "Disable download progress display")
	return commandFetch
}

func createUniRefLockCommand() *cobra.Command {
	cfg := unirefLockConfig{}
	commandLock := &cobra.Command{
		Use:           "lock SNAPSHOT",
		Short:         "Rebuild UniRef manifest.lock from the current version directory",
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg.DirSnapshot = args[0]
			return runLockUniRef(&cfg)
		},
	}
	flags := commandLock.Flags()
	flags.SortFlags = false
	cliopt.BindLockWorkersFlag(flags, &cfg.workersMax)
	cliopt.BindDryRunFlag(flags, &cfg.DryRunConfig, "Print actions only; do not write manifest")
	cliopt.BindLogDirFlag(flags, &cfg.LogConfig, "Directory for run log files; default is <version>/logs/")
	return commandLock
}

func createUniRefRestoreCommand() *cobra.Command {
	cfg := unirefRestoreConfig{}
	cfg.RetryMax = 5
	cfg.RetryWait = 3 * time.Second
	cfg.RuleExisting = "skip"
	cfg.WorkersMax = 1

	commandRestore := &cobra.Command{
		Use:           "restore SNAPSHOT",
		Short:         "Restore UniRef files from manifest.lock and refresh manifest",
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := cliopt.ApplyFlatSnapshot(&cfg.DirOutConfig, &cfg.VersionConfig, args[0]); err != nil {
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
			return runRestoreUniRef(&cfg)
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

func createIDMappingCommand() *cobra.Command {
	commandIDMapping := &cobra.Command{
		Use:           "id-mapping",
		Short:         "Manage UniProt ID mapping raw assets",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	commandIDMapping.AddCommand(createIDMappingFetchCommand())
	commandIDMapping.AddCommand(createIDMappingLockCommand())
	commandIDMapping.AddCommand(createIDMappingRestoreCommand())
	return commandIDMapping
}

func createIDMappingFetchCommand() *cobra.Command {
	cfg := createDefaultIDMappingConfig()

	commandFetch := &cobra.Command{
		Use:           "fetch",
		Short:         "Fetch UniProt ID mapping global raw assets and update manifest.lock",
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := validateIDMappingConfig(&cfg); err != nil {
				return err
			}
			return runFetchIDMapping(&cfg)
		},
	}
	commandFetch.Example = strings.Join([]string{
		"biofetch uniprot id-mapping fetch --output /data/uniprot --allow-large-downloads",
		"biofetch uniprot id-mapping fetch --output /data/uniprot --assets selected --allow-large-downloads",
		"biofetch uniprot id-mapping fetch --output /data/uniprot --assets dat --allow-large-downloads",
	}, "\n")

	flags := commandFetch.Flags()
	flags.SortFlags = false
	cliopt.BindDirOutFlag(flags, &cfg.DirOutConfig, "UniProt asset root directory")
	cliopt.BindVersionFlag(flags, &cfg.VersionConfig, "UniProt release token; omit for current")
	flags.StringSliceVar(&cfg.assetNames, "assets", nil, "UniProt ID mapping assets: all|selected|dat; omit or pass all to fetch all supported assets")
	cliopt.BindListFileFlag(flags, &cfg.assetNames, "assets")
	flags.BoolVar(&cfg.shouldAllowLargeAssets, "allow-large-downloads", false, "Allow multi-GB UniProt ID mapping assets")
	flags.StringVar(&cfg.baseURLCurrentRelease, "current-release-url", cfg.baseURLCurrentRelease, "UniProt current_release base URL, including mirror URLs")
	cliopt.BindRuleExistingFlag(flags, &cfg.ExistingRuleConfig, "Rule for existing files: skip|overwrite")
	cliopt.BindRetryFlags(flags, &cfg.RetryConfig)
	cliopt.BindDownloadControlFlags(flags, &cfg.DownloadControlConfig)
	cliopt.BindInsecureTLSFlag(flags, &cfg.InsecureTLSConfig, "Disable TLS certificate verification")
	cliopt.BindDryRunFlag(flags, &cfg.DryRunConfig, "Print actions only; do not download")
	cliopt.BindLogDirFlag(flags, &cfg.LogConfig, "Directory for run log files; default is <version>/logs/")
	cliopt.BindProgressFlag(flags, &cfg.ProgressConfig, "Disable download progress display")
	return commandFetch
}

func createIDMappingLockCommand() *cobra.Command {
	cfg := idMappingLockConfig{}
	commandLock := &cobra.Command{
		Use:           "lock SNAPSHOT",
		Short:         "Rebuild UniProt ID mapping manifest.lock from the current version directory",
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg.DirSnapshot = args[0]
			return runLockIDMapping(&cfg)
		},
	}
	flags := commandLock.Flags()
	flags.SortFlags = false
	cliopt.BindLockWorkersFlag(flags, &cfg.workersMax)
	cliopt.BindDryRunFlag(flags, &cfg.DryRunConfig, "Print actions only; do not write manifest")
	cliopt.BindLogDirFlag(flags, &cfg.LogConfig, "Directory for run log files; default is <version>/logs/")
	return commandLock
}

func createIDMappingRestoreCommand() *cobra.Command {
	cfg := idMappingRestoreConfig{}
	cfg.RetryMax = 5
	cfg.RetryWait = 3 * time.Second
	cfg.RuleExisting = "skip"
	cfg.WorkersMax = 1

	commandRestore := &cobra.Command{
		Use:           "restore SNAPSHOT",
		Short:         "Restore UniProt ID mapping files from manifest.lock and refresh manifest",
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := cliopt.ApplyFlatSnapshot(&cfg.DirOutConfig, &cfg.VersionConfig, args[0]); err != nil {
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
			return runRestoreIDMapping(&cfg)
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
