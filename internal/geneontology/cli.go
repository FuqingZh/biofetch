package geneontology

import (
	"biofetch/internal/shared/cliopt"
	"biofetch/internal/shared/confirm"
	"io"
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

	commandGO.AddCommand(createOntologyCommand())
	commandGO.AddCommand(createSlimCommand())
	return commandGO
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
			if err := cliopt.ValidateDirOutRequired(cfg.DirOut); err != nil {
				return err
			}
			if err := cliopt.ValidateVersionRequired(cfg.VersionToken); err != nil {
				return err
			}
			return runLockSlim(&cfg)
		},
	}

	flags := commandLock.Flags()
	flags.SortFlags = false
	cliopt.BindDirOutFlag(flags, &cfg.DirOutConfig, "GO asset root directory")
	cliopt.BindVersionFlag(flags, &cfg.VersionConfig, "GO Slim version token")
	cliopt.BindDryRunFlag(flags, &cfg.DryRunConfig, "Print actions only; do not write manifest")
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
			if err := cliopt.ValidateDirOutRequired(cfg.DirOut); err != nil {
				return err
			}
			if err := cliopt.ValidateVersionRequired(cfg.VersionToken); err != nil {
				return err
			}
			return runLockOntology(&cfg)
		},
	}

	flags := commandLock.Flags()
	flags.SortFlags = false
	cliopt.BindDirOutFlag(flags, &cfg.DirOutConfig, "GO asset root directory")
	cliopt.BindVersionFlag(flags, &cfg.VersionConfig, "GO ontology version token")
	cliopt.BindDryRunFlag(flags, &cfg.DryRunConfig, "Print actions only; do not write manifest")
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

func confirmAllOntologyDownload(reader io.Reader, writer io.Writer) error {
	return confirm.RequireLiteral(
		reader,
		writer,
		"Full ontology download may fetch a large number of files and consume substantial disk, time, and bandwidth.",
		"all_assets",
	)
}
