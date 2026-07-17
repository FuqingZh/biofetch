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
	commandAnnotation.AddCommand(createAnnotationSyncCommand())
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
	commandOntology.AddCommand(createOntologySyncCommand())
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
	commandSlim.AddCommand(createSlimSyncCommand())
	return commandSlim
}

func createAnnotationFetchCommand() *cobra.Command {
	cfg := createDefaultAnnotationConfig()
	retryWaitSec := 3
	requestIntervalMs := 0

	commandFetch := &cobra.Command{
		Use:           "fetch",
		Short:         "Fetch GO annotation raw assets and update manifest.lock",
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg.RetryWait = time.Duration(retryWaitSec) * time.Second
			cfg.RequestInterval = time.Duration(requestIntervalMs) * time.Millisecond
			if err := validateAnnotationConfig(&cfg); err != nil {
				return err
			}
			return runFetchAnnotation(&cfg)
		},
	}

	commandFetch.Example = strings.Join([]string{
		"biofetch go annotation fetch --dir_out /data/go",
		"biofetch go annotation fetch --dir_out /data/go --datasets goa_human",
		"biofetch go annotation fetch --dir_out /data/go --datasets goa_human --formats gaf,gpad,gpi",
		"biofetch go annotation fetch --dir_out /data/go --version 2026-01-23 --datasets mgi,sgd --formats gaf",
	}, "\n")

	flags := commandFetch.Flags()
	flags.SortFlags = false
	cliopt.BindDirOutFlag(flags, &cfg.DirOutConfig, "GO asset root directory")
	cliopt.BindVersionFlag(flags, &cfg.VersionConfig, "GO release date in YYYY-MM-DD; omit to fetch the latest release")
	flags.StringSliceVar(&cfg.datasetNames, "datasets", nil, "GO annotation dataset file stems, e.g. goa_human or mgi; omit to discover all datasets for selected formats")
	flags.StringSliceVar(&cfg.formatNames, "formats", nil, "GO annotation formats: gaf|gpad|gpi; omit for gaf, repeat the flag, use commas, or use @file")
	cliopt.BindRuleExistingFlag(flags, &cfg.ExistingRuleConfig, "Rule for existing files: skip|overwrite")
	cliopt.BindRetryFlags(flags, &cfg.RetryConfig, &retryWaitSec)
	cliopt.BindDownloadControlFlags(flags, &cfg.DownloadControlConfig, &requestIntervalMs)
	cliopt.BindInsecureTLSFlag(flags, &cfg.InsecureTLSConfig, "Disable TLS certificate verification")
	cliopt.BindDryRunFlag(flags, &cfg.DryRunConfig, "Print actions only; do not download")
	cliopt.BindLogDirFlag(flags, &cfg.LogConfig, "Directory for run log files; default is <version>/logs/")
	cliopt.BindProgressFlag(flags, &cfg.ProgressConfig, "Disable download progress display")
	return commandFetch
}

func createAnnotationLockCommand() *cobra.Command {
	cfg := annotationLockConfig{}

	commandLock := &cobra.Command{
		Use:           "lock",
		Short:         "Rebuild GO annotation manifest.lock from the current version directory",
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runLockAnnotation(&cfg)
		},
	}

	flags := commandLock.Flags()
	flags.SortFlags = false
	cliopt.BindDirSnapshotFlag(flags, &cfg.DirSnapshotConfig)
	cliopt.BindDryRunFlag(flags, &cfg.DryRunConfig, "Print actions only; do not write manifest")
	cliopt.BindLogDirFlag(flags, &cfg.LogConfig, "Directory for run log files; default is <version>/logs/")
	return commandLock
}

func createAnnotationSyncCommand() *cobra.Command {
	cfg := createDefaultAnnotationSyncConfig()
	retryWaitSec := 3
	requestIntervalMs := 0

	commandSync := &cobra.Command{
		Use:           "sync",
		Short:         "Sync GO annotation files from manifest.lock and refresh manifest",
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
			return runSyncAnnotation(&cfg)
		},
	}

	flags := commandSync.Flags()
	flags.SortFlags = false
	cliopt.BindDirOutFlag(flags, &cfg.DirOutConfig, "GO asset root directory")
	cliopt.BindVersionFlag(flags, &cfg.VersionConfig, "GO annotation version token")
	cliopt.BindRuleExistingFlag(flags, &cfg.ExistingRuleConfig, "Rule for existing files: skip|overwrite")
	cliopt.BindRetryFlags(flags, &cfg.RetryConfig, &retryWaitSec)
	cliopt.BindDownloadControlFlags(flags, &cfg.DownloadControlConfig, &requestIntervalMs)
	cliopt.BindInsecureTLSFlag(flags, &cfg.InsecureTLSConfig, "Disable TLS certificate verification")
	cliopt.BindDryRunFlag(flags, &cfg.DryRunConfig, "Print actions only; do not download")
	cliopt.BindLogDirFlag(flags, &cfg.LogConfig, "Directory for run log files; default is <version>/logs/")
	cliopt.BindProgressFlag(flags, &cfg.ProgressConfig, "Disable download progress display")
	return commandSync
}

func createSlimFetchCommand() *cobra.Command {
	cfg := createDefaultSlimConfig()
	retryWaitSec := 3
	requestIntervalMs := 0

	commandSlim := &cobra.Command{
		Use:           "fetch",
		Short:         "Fetch GO Slim subset raw assets and update manifest.lock",
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg.RetryWait = time.Duration(retryWaitSec) * time.Second
			cfg.RequestInterval = time.Duration(requestIntervalMs) * time.Millisecond
			if err := validateSlimConfig(&cfg); err != nil {
				return err
			}
			return runFetchSlim(&cfg)
		},
	}

	commandSlim.Example = strings.Join([]string{
		"biofetch go slim fetch --dir_out /data/go --should_dry_run",
		"biofetch go slim fetch --dir_out /data/go --subsets goslim_generic --formats obo,tsv",
		"biofetch go slim fetch --dir_out /data/go --version 2026-01-23 --subsets goslim_plant --formats obo",
	}, "\n")

	flags := commandSlim.Flags()
	flags.SortFlags = false
	cliopt.BindDirOutFlag(flags, &cfg.DirOutConfig, "GO asset root directory")
	cliopt.BindVersionFlag(flags, &cfg.VersionConfig, "GO release date in YYYY-MM-DD; omit to fetch the latest release")
	flags.StringSliceVar(&cfg.subsetNames, "subsets", nil, "GO Slim subset IDs; omit for goslim_generic, repeat the flag, use commas, or use @file")
	flags.StringSliceVar(&cfg.formatNames, "formats", nil, "GO Slim formats: obo|owl|json|tsv; omit for obo, repeat the flag, use commas, or use @file")
	cliopt.BindRuleExistingFlag(flags, &cfg.ExistingRuleConfig, "Rule for existing files: skip|overwrite")
	cliopt.BindRetryFlags(flags, &cfg.RetryConfig, &retryWaitSec)
	cliopt.BindDownloadControlFlags(flags, &cfg.DownloadControlConfig, &requestIntervalMs)
	cliopt.BindInsecureTLSFlag(flags, &cfg.InsecureTLSConfig, "Disable TLS certificate verification")
	cliopt.BindDryRunFlag(flags, &cfg.DryRunConfig, "Print actions only; do not download")
	cliopt.BindLogDirFlag(flags, &cfg.LogConfig, "Directory for run log files; default is <version>/logs/")
	cliopt.BindProgressFlag(flags, &cfg.ProgressConfig, "Disable download progress display")
	return commandSlim
}

func createSlimLockCommand() *cobra.Command {
	cfg := slimLockConfig{}

	commandLock := &cobra.Command{
		Use:           "lock",
		Short:         "Rebuild GO Slim manifest.lock from the current version directory",
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runLockSlim(&cfg)
		},
	}

	flags := commandLock.Flags()
	flags.SortFlags = false
	cliopt.BindDirSnapshotFlag(flags, &cfg.DirSnapshotConfig)
	cliopt.BindDryRunFlag(flags, &cfg.DryRunConfig, "Print actions only; do not write manifest")
	cliopt.BindLogDirFlag(flags, &cfg.LogConfig, "Directory for run log files; default is <version>/logs/")
	return commandLock
}

func createSlimSyncCommand() *cobra.Command {
	cfg := slimSyncConfig{}
	cfg.RetryMax = 5
	cfg.RetryWait = 3 * time.Second
	cfg.RuleExisting = "skip"
	cfg.WorkersMax = 1
	retryWaitSec := 3
	requestIntervalMs := 0

	commandSync := &cobra.Command{
		Use:           "sync",
		Short:         "Sync GO Slim files from manifest.lock and refresh manifest",
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
			return runSyncSlim(&cfg)
		},
	}

	flags := commandSync.Flags()
	flags.SortFlags = false
	cliopt.BindDirOutFlag(flags, &cfg.DirOutConfig, "GO asset root directory")
	cliopt.BindVersionFlag(flags, &cfg.VersionConfig, "GO Slim version token")
	cliopt.BindRuleExistingFlag(flags, &cfg.ExistingRuleConfig, "Rule for existing files: skip|overwrite")
	cliopt.BindRetryFlags(flags, &cfg.RetryConfig, &retryWaitSec)
	cliopt.BindDownloadControlFlags(flags, &cfg.DownloadControlConfig, &requestIntervalMs)
	cliopt.BindInsecureTLSFlag(flags, &cfg.InsecureTLSConfig, "Disable TLS certificate verification")
	cliopt.BindDryRunFlag(flags, &cfg.DryRunConfig, "Print actions only; do not download")
	cliopt.BindLogDirFlag(flags, &cfg.LogConfig, "Directory for run log files; default is <version>/logs/")
	cliopt.BindProgressFlag(flags, &cfg.ProgressConfig, "Disable download progress display")
	return commandSync
}

func createOntologyFetchCommand() *cobra.Command {
	cfg := createDefaultOntologyConfig()
	retryWaitSec := 3
	requestIntervalMs := 0

	commandOntology := &cobra.Command{
		Use:           "fetch",
		Short:         "Fetch Gene Ontology ontology raw assets and update manifest.lock with cache-aware skip reuse",
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg.RetryWait = time.Duration(retryWaitSec) * time.Second
			cfg.RequestInterval = time.Duration(requestIntervalMs) * time.Millisecond
			if err := validateOntologyConfig(&cfg); err != nil {
				return err
			}
			return runFetchOntology(&cfg, cmd.InOrStdin(), cmd.ErrOrStderr())
		},
	}

	commandOntology.Example = strings.Join([]string{
		"biofetch go ontology fetch --dir_out /data/go --should_dry_run",
		"biofetch go ontology fetch --dir_out /data/go",
		"biofetch go ontology fetch --dir_out /data/go --version 2026-01-23 --assets go-basic.obo",
		"biofetch go ontology fetch --dir_out /data/go --assets go-basic.obo --assets go.obo",
	}, "\n")

	flags := commandOntology.Flags()
	flags.SortFlags = false
	cliopt.BindDirOutFlag(flags, &cfg.DirOutConfig, "GO asset root directory")
	cliopt.BindVersionFlag(flags, &cfg.VersionConfig, "GO release date in YYYY-MM-DD; omit to fetch the latest release")
	flags.StringSliceVar(
		&cfg.assetNames,
		"assets",
		nil,
		"Ontology assets; omit to fetch all discovered ontology files, or pass inline values, repeat the flag, or use @file with one asset per line (# comments and blank lines ignored)",
	)
	cliopt.BindRuleExistingFlag(flags, &cfg.ExistingRuleConfig, "Rule for existing files: skip|overwrite (skip reuses manifest/cache when size matches)")
	cliopt.BindRetryFlags(flags, &cfg.RetryConfig, &retryWaitSec)
	cliopt.BindDownloadControlFlags(flags, &cfg.DownloadControlConfig, &requestIntervalMs)
	cliopt.BindInsecureTLSFlag(flags, &cfg.InsecureTLSConfig, "Disable TLS certificate verification")
	cliopt.BindDryRunFlag(flags, &cfg.DryRunConfig, "Print actions only; do not download")
	cliopt.BindLogDirFlag(flags, &cfg.LogConfig, "Directory for run log files; default is <version>/logs/")

	return commandOntology
}

func createOntologyLockCommand() *cobra.Command {
	cfg := ontologyLockConfig{}

	commandLock := &cobra.Command{
		Use:           "lock",
		Short:         "Rebuild GO ontology manifest.lock from the current version directory",
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runLockOntology(&cfg)
		},
	}

	flags := commandLock.Flags()
	flags.SortFlags = false
	cliopt.BindDirSnapshotFlag(flags, &cfg.DirSnapshotConfig)
	cliopt.BindDryRunFlag(flags, &cfg.DryRunConfig, "Print actions only; do not write manifest")
	cliopt.BindLogDirFlag(flags, &cfg.LogConfig, "Directory for run log files; default is <version>/logs/")
	return commandLock
}

func createOntologySyncCommand() *cobra.Command {
	cfg := ontologySyncConfig{}
	cfg.RetryMax = 5
	cfg.RetryWait = 3 * time.Second
	cfg.RuleExisting = "skip"
	cfg.WorkersMax = 1
	retryWaitSec := 3
	requestIntervalMs := 0

	commandSync := &cobra.Command{
		Use:           "sync",
		Short:         "Sync GO ontology files from manifest.lock and refresh manifest with cache-aware skip reuse",
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
			return runSyncOntology(&cfg)
		},
	}

	flags := commandSync.Flags()
	flags.SortFlags = false
	cliopt.BindDirOutFlag(flags, &cfg.DirOutConfig, "GO asset root directory")
	cliopt.BindVersionFlag(flags, &cfg.VersionConfig, "GO ontology version token")
	cliopt.BindRuleExistingFlag(flags, &cfg.ExistingRuleConfig, "Rule for existing files: skip|overwrite (skip reuses manifest/cache when size matches)")
	cliopt.BindRetryFlags(flags, &cfg.RetryConfig, &retryWaitSec)
	cliopt.BindDownloadControlFlags(flags, &cfg.DownloadControlConfig, &requestIntervalMs)
	cliopt.BindInsecureTLSFlag(flags, &cfg.InsecureTLSConfig, "Disable TLS certificate verification")
	cliopt.BindDryRunFlag(flags, &cfg.DryRunConfig, "Print actions only; do not download")
	cliopt.BindLogDirFlag(flags, &cfg.LogConfig, "Directory for run log files; default is <version>/logs/")
	return commandSync
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
