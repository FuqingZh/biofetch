package geneontology

import (
	"biofetch/internal/shared/cliopt"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

type ontologyConfig struct {
	cliopt.DirOutConfig
	cliopt.VersionConfig
	cliopt.ExistingRuleConfig
	cliopt.RetryConfig
	cliopt.DownloadControlConfig
	cliopt.InsecureTLSConfig
	cliopt.DryRunConfig
	cliopt.LogConfig
	version    string
	assetNames []string
}

func NewCommand() *cobra.Command {
	commandGO := &cobra.Command{
		Use:           "go",
		Short:         "Manage Gene Ontology raw assets and manifest.lock",
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	commandGO.AddCommand(createAnnotationCommand())
	commandGO.AddCommand(createOntologyCommand())
	commandGO.AddCommand(createSlimCommand())
	return commandGO
}

func createAnnotationCommand() *cobra.Command {
	commandAnnotation := &cobra.Command{
		Use:           "annotation",
		Short:         "Manage GO annotation raw assets",
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	commandAnnotation.AddCommand(createAnnotationFetchCommand())
	commandAnnotation.AddCommand(createAnnotationLockCommand())
	commandAnnotation.AddCommand(createAnnotationRestoreCommand())
	return commandAnnotation
}

func createOntologyCommand() *cobra.Command {
	commandOntology := &cobra.Command{
		Use:           "ontology",
		Short:         "Manage Gene Ontology ontology raw assets",
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	commandOntology.AddCommand(createOntologyFetchCommand())
	commandOntology.AddCommand(createOntologyLockCommand())
	commandOntology.AddCommand(createOntologyRestoreCommand())
	return commandOntology
}

func createSlimCommand() *cobra.Command {
	commandSlim := &cobra.Command{
		Use:           "slim",
		Short:         "Manage GO Slim subset raw assets",
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	commandSlim.AddCommand(createSlimFetchCommand())
	commandSlim.AddCommand(createSlimLockCommand())
	commandSlim.AddCommand(createSlimRestoreCommand())
	return commandSlim
}

func createAnnotationFetchCommand() *cobra.Command {
	cfg := createDefaultAnnotationConfig()

	commandFetch := &cobra.Command{
		Use:           "fetch",
		Short:         "Fetch GO annotation raw assets and update manifest.lock",
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := validateAnnotationConfig(&cfg); err != nil {
				return err
			}
			return runFetchAnnotation(&cfg)
		},
	}

	commandFetch.Example = strings.Join([]string{
		"biofetch go annotation fetch --output /data/go",
		"biofetch go annotation fetch --output /data/go --datasets goa_human",
		"biofetch go annotation fetch --output /data/go --datasets goa_human --formats gaf,gpad,gpi",
		"biofetch go annotation fetch --output /data/go --version 2026-01-23 --datasets mgi,sgd --formats gaf",
	}, "\n")

	flags := commandFetch.Flags()
	flags.SortFlags = false
	cliopt.BindDirOutFlag(flags, &cfg.DirOutConfig, "GO asset root directory")
	cliopt.BindVersionFlag(flags, &cfg.VersionConfig, "GO release date in YYYY-MM-DD; omit to fetch the latest release")
	cliopt.BindStringListFlags(flags, &cfg.datasetNames, "datasets", "GO annotation dataset file stems, e.g. goa_human or mgi; omit to discover all datasets for selected formats")
	cliopt.BindStringListFlags(flags, &cfg.formatNames, "formats", "GO annotation formats: gaf|gpad|gpi; omit for gaf, repeat the flag,")
	cliopt.BindRuleExistingFlag(flags, &cfg.ExistingRuleConfig, "Rule for existing files: skip|overwrite")
	cliopt.BindRetryFlags(flags, &cfg.RetryConfig)
	cliopt.BindDownloadControlFlags(flags, &cfg.DownloadControlConfig)
	cliopt.BindInsecureTLSFlag(flags, &cfg.InsecureTLSConfig, "Disable TLS certificate verification")
	cliopt.BindDryRunFlag(flags, &cfg.DryRunConfig, "Print actions only; do not download")
	cliopt.BindLogDirFlag(flags, &cfg.LogConfig, "Directory for run log files; default is <version>/logs/")
	cliopt.BindProgressFlag(flags, &cfg.ProgressConfig, "Disable download progress display")
	return commandFetch
}

func createAnnotationLockCommand() *cobra.Command {
	cfg := annotationLockConfig{}

	commandLock := &cobra.Command{
		Use:           "lock SNAPSHOT",
		Short:         "Rebuild GO annotation manifest.lock from the current version directory",
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg.DirSnapshot = args[0]
			return runLockAnnotation(&cfg)
		},
	}

	flags := commandLock.Flags()
	flags.SortFlags = false
	cliopt.BindLockWorkersFlag(flags, &cfg.workersMax)
	cliopt.BindDryRunFlag(flags, &cfg.DryRunConfig, "Print actions only; do not write manifest")
	cliopt.BindLogDirFlag(flags, &cfg.LogConfig, "Directory for run log files; default is <version>/logs/")
	return commandLock
}

func createAnnotationRestoreCommand() *cobra.Command {
	cfg := createDefaultAnnotationRestoreConfig()

	commandRestore := &cobra.Command{
		Use:           "restore SNAPSHOT",
		Short:         "Restore GO annotation files from manifest.lock and refresh manifest",
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := cliopt.ApplyFlatSnapshot(&cfg.DirOutConfig, &cfg.VersionConfig, args[0], "annotation"); err != nil {
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
			return runRestoreAnnotation(&cfg)
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

func createSlimFetchCommand() *cobra.Command {
	cfg := createDefaultSlimConfig()

	commandSlim := &cobra.Command{
		Use:           "fetch",
		Short:         "Fetch GO Slim subset raw assets and update manifest.lock",
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := validateSlimConfig(&cfg); err != nil {
				return err
			}
			return runFetchSlim(&cfg)
		},
	}

	commandSlim.Example = strings.Join([]string{
		"biofetch go slim fetch --output /data/go --dry-run",
		"biofetch go slim fetch --output /data/go --subsets goslim_generic --formats obo,tsv",
		"biofetch go slim fetch --output /data/go --version 2026-01-23 --subsets goslim_plant --formats obo",
	}, "\n")

	flags := commandSlim.Flags()
	flags.SortFlags = false
	cliopt.BindDirOutFlag(flags, &cfg.DirOutConfig, "GO asset root directory")
	cliopt.BindVersionFlag(flags, &cfg.VersionConfig, "GO release date in YYYY-MM-DD; omit to fetch the latest release")
	cliopt.BindStringListFlags(flags, &cfg.subsetNames, "subsets", "GO Slim subset IDs; omit for goslim_generic, repeat the flag,")
	cliopt.BindStringListFlags(flags, &cfg.formatNames, "formats", "GO Slim formats: obo|owl|json|tsv; omit for obo, repeat the flag,")
	cliopt.BindRuleExistingFlag(flags, &cfg.ExistingRuleConfig, "Rule for existing files: skip|overwrite")
	cliopt.BindRetryFlags(flags, &cfg.RetryConfig)
	cliopt.BindDownloadControlFlags(flags, &cfg.DownloadControlConfig)
	cliopt.BindInsecureTLSFlag(flags, &cfg.InsecureTLSConfig, "Disable TLS certificate verification")
	cliopt.BindDryRunFlag(flags, &cfg.DryRunConfig, "Print actions only; do not download")
	cliopt.BindLogDirFlag(flags, &cfg.LogConfig, "Directory for run log files; default is <version>/logs/")
	cliopt.BindProgressFlag(flags, &cfg.ProgressConfig, "Disable download progress display")
	return commandSlim
}

func createSlimLockCommand() *cobra.Command {
	cfg := slimLockConfig{}

	commandLock := &cobra.Command{
		Use:           "lock SNAPSHOT",
		Short:         "Rebuild GO Slim manifest.lock from the current version directory",
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg.DirSnapshot = args[0]
			return runLockSlim(&cfg)
		},
	}

	flags := commandLock.Flags()
	flags.SortFlags = false
	cliopt.BindLockWorkersFlag(flags, &cfg.workersMax)
	cliopt.BindDryRunFlag(flags, &cfg.DryRunConfig, "Print actions only; do not write manifest")
	cliopt.BindLogDirFlag(flags, &cfg.LogConfig, "Directory for run log files; default is <version>/logs/")
	return commandLock
}

func createSlimRestoreCommand() *cobra.Command {
	cfg := slimRestoreConfig{}
	cfg.RetryMax = 5
	cfg.RetryWait = 3 * time.Second
	cfg.RuleExisting = "skip"
	cfg.WorkersMax = 1

	commandRestore := &cobra.Command{
		Use:           "restore SNAPSHOT",
		Short:         "Restore GO Slim files from manifest.lock and refresh manifest",
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := cliopt.ApplyFlatSnapshot(&cfg.DirOutConfig, &cfg.VersionConfig, args[0], "slim"); err != nil {
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
			return runRestoreSlim(&cfg)
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

func createOntologyFetchCommand() *cobra.Command {
	cfg := createDefaultOntologyConfig()

	commandOntology := &cobra.Command{
		Use:           "fetch",
		Short:         "Fetch Gene Ontology ontology raw assets and update manifest.lock with cache-aware skip reuse",
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := validateOntologyConfig(&cfg); err != nil {
				return err
			}
			return runFetchOntology(&cfg, cmd.InOrStdin(), cmd.ErrOrStderr())
		},
	}

	commandOntology.Example = strings.Join([]string{
		"biofetch go ontology fetch --output /data/go --dry-run",
		"biofetch go ontology fetch --output /data/go",
		"biofetch go ontology fetch --output /data/go --version 2026-01-23 --assets go-basic.obo",
		"biofetch go ontology fetch --output /data/go --assets go-basic.obo --assets go.obo",
	}, "\n")

	flags := commandOntology.Flags()
	flags.SortFlags = false
	cliopt.BindDirOutFlag(flags, &cfg.DirOutConfig, "GO asset root directory")
	cliopt.BindVersionFlag(flags, &cfg.VersionConfig, "GO release date in YYYY-MM-DD; omit to fetch the latest release")
	cliopt.BindStringListFlags(
		flags,
		&cfg.assetNames,
		"assets",
		"Ontology assets; omit to fetch all discovered ontology files, or pass inline values or repeat the flag",
	)
	cliopt.BindRuleExistingFlag(flags, &cfg.ExistingRuleConfig, "Rule for existing files: skip|overwrite (skip reuses manifest/cache when size matches)")
	cliopt.BindRetryFlags(flags, &cfg.RetryConfig)
	cliopt.BindDownloadControlFlags(flags, &cfg.DownloadControlConfig)
	cliopt.BindInsecureTLSFlag(flags, &cfg.InsecureTLSConfig, "Disable TLS certificate verification")
	cliopt.BindDryRunFlag(flags, &cfg.DryRunConfig, "Print actions only; do not download")
	cliopt.BindLogDirFlag(flags, &cfg.LogConfig, "Directory for run log files; default is <version>/logs/")

	return commandOntology
}

func createOntologyLockCommand() *cobra.Command {
	cfg := ontologyLockConfig{}

	commandLock := &cobra.Command{
		Use:           "lock SNAPSHOT",
		Short:         "Rebuild GO ontology manifest.lock from the current version directory",
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg.DirSnapshot = args[0]
			return runLockOntology(&cfg)
		},
	}

	flags := commandLock.Flags()
	flags.SortFlags = false
	cliopt.BindLockWorkersFlag(flags, &cfg.workersMax)
	cliopt.BindDryRunFlag(flags, &cfg.DryRunConfig, "Print actions only; do not write manifest")
	cliopt.BindLogDirFlag(flags, &cfg.LogConfig, "Directory for run log files; default is <version>/logs/")
	return commandLock
}

func createOntologyRestoreCommand() *cobra.Command {
	cfg := ontologyRestoreConfig{}
	cfg.RetryMax = 5
	cfg.RetryWait = 3 * time.Second
	cfg.RuleExisting = "skip"
	cfg.WorkersMax = 1

	commandRestore := &cobra.Command{
		Use:           "restore SNAPSHOT",
		Short:         "Restore GO ontology files from manifest.lock and refresh manifest with cache-aware skip reuse",
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := cliopt.ApplyFlatSnapshot(&cfg.DirOutConfig, &cfg.VersionConfig, args[0], "ontology"); err != nil {
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
			return runRestoreOntology(&cfg)
		},
	}

	flags := commandRestore.Flags()
	flags.SortFlags = false
	cliopt.BindRuleExistingFlag(flags, &cfg.ExistingRuleConfig, "Rule for existing files: skip|overwrite (skip reuses manifest/cache when size matches)")
	cliopt.BindRetryFlags(flags, &cfg.RetryConfig)
	cliopt.BindDownloadControlFlags(flags, &cfg.DownloadControlConfig)
	cliopt.BindInsecureTLSFlag(flags, &cfg.InsecureTLSConfig, "Disable TLS certificate verification")
	cliopt.BindDryRunFlag(flags, &cfg.DryRunConfig, "Print actions only; do not download")
	cliopt.BindLogDirFlag(flags, &cfg.LogConfig, "Directory for run log files; default is <version>/logs/")
	return commandRestore
}

func createDefaultOntologyConfig() ontologyConfig {
	cfg := ontologyConfig{}
	cfg.RetryMax = 5
	cfg.RetryWait = 3 * time.Second
	cfg.WorkersMax = 1
	return cfg
}

func validateOntologyConfig(cfg *ontologyConfig) error {
	if err := cliopt.ValidateRetryConfig(&cfg.RetryConfig); err != nil {
		return err
	}
	if err := cliopt.ValidateDirOutRequired(cfg.DirOut); err != nil {
		return err
	}
	if err := cliopt.ValidateDownloadControlConfig(&cfg.DownloadControlConfig); err != nil {
		return err
	}
	if err := cliopt.ValidateRuleExisting(&cfg.ExistingRuleConfig); err != nil {
		return err
	}
	if err := validateOptionalOntologyVersionToken(cfg.VersionToken); err != nil {
		return err
	}
	return nil
}
