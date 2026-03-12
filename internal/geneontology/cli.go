package geneontology

import (
	"biofetch/internal/shared/cliopt"
	"biofetch/internal/shared/confirm"
	"fmt"
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
	cliopt.InsecureTLSConfig
	cliopt.DryRunConfig
	version           string
	assetNames        []string
	shouldDownloadAll bool
}

func NewCommand() *cobra.Command {
	commandGO := &cobra.Command{
		Use:           "go",
		Short:         "Manage Gene Ontology raw assets and manifest.lock",
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	commandGO.AddCommand(createOntologyCommand())
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

func createOntologyFetchCommand() *cobra.Command {
	cfg := createDefaultOntologyConfig()
	retryWaitSec := 3

	commandOntology := &cobra.Command{
		Use:           "fetch",
		Short:         "Fetch Gene Ontology ontology raw assets and update manifest.lock",
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg.RetryWait = time.Duration(retryWaitSec) * time.Second
			if err := validateOntologyConfig(&cfg); err != nil {
				return err
			}
			if cfg.shouldDownloadAll {
				if err := confirmAllOntologyDownload(cmd.InOrStdin(), cmd.ErrOrStderr()); err != nil {
					return err
				}
			}
			return runFetchOntology(&cfg)
		},
	}

	commandOntology.Example = strings.Join([]string{
		"biofetch go ontology fetch --dir_out /data/go --should_dry_run",
		"biofetch go ontology fetch --dir_out /data/go --assets go-basic.obo --assets go.obo",
		"biofetch go ontology fetch --dir_out /data/go --should_download_all",
	}, "\n")

	flags := commandOntology.Flags()
	flags.SortFlags = false
	cliopt.BindDirOutFlag(flags, &cfg.DirOutConfig, "GO asset root directory")
	flags.StringSliceVar(
		&cfg.assetNames,
		"assets",
		nil,
		"Ontology assets; repeat the flag or use commas, e.g. --assets go-basic.obo --assets go.obo",
	)
	flags.BoolVar(&cfg.shouldDownloadAll, "should_download_all", false, "Discover and download all ontology files")
	cliopt.BindRuleExistingFlag(flags, &cfg.ExistingRuleConfig, "Rule for existing files: skip|overwrite")
	cliopt.BindRetryFlags(flags, &cfg.RetryConfig, &retryWaitSec)
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
	retryWaitSec := 3

	commandSync := &cobra.Command{
		Use:           "sync",
		Short:         "Sync GO ontology files from manifest.lock and refresh manifest",
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg.RetryWait = time.Duration(retryWaitSec) * time.Second
			if err := cliopt.ValidateDirOutRequired(cfg.DirOut); err != nil {
				return err
			}
			if err := cliopt.ValidateVersionRequired(cfg.VersionToken); err != nil {
				return err
			}
			if err := cliopt.ValidateRetryConfig(&cfg.RetryConfig); err != nil {
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
	cliopt.BindRuleExistingFlag(flags, &cfg.ExistingRuleConfig, "Rule for existing files: skip|overwrite")
	cliopt.BindRetryFlags(flags, &cfg.RetryConfig, &retryWaitSec)
	cliopt.BindInsecureTLSFlag(flags, &cfg.InsecureTLSConfig, "Disable TLS certificate verification")
	cliopt.BindDryRunFlag(flags, &cfg.DryRunConfig, "Print actions only; do not download")
	return commandSync
}

func createDefaultOntologyConfig() ontologyConfig {
	cfg := ontologyConfig{}
	cfg.RetryMax = 5
	cfg.RetryWait = 3 * time.Second
	return cfg
}

func validateOntologyConfig(cfg *ontologyConfig) error {
	if err := cliopt.ValidateRetryConfig(&cfg.RetryConfig); err != nil {
		return err
	}
	if err := cliopt.ValidateDirOutRequired(cfg.DirOut); err != nil {
		return err
	}
	if err := cliopt.ValidateRuleExisting(&cfg.ExistingRuleConfig); err != nil {
		return err
	}
	countSources := 0
	if len(cfg.assetNames) > 0 {
		countSources++
	}
	if cfg.shouldDownloadAll {
		countSources++
	}
	if countSources != 1 {
		return fmt.Errorf("choose exactly one source: --assets | --should_download_all")
	}
	return nil
}

func confirmAllOntologyDownload(reader io.Reader, writer io.Writer) error {
	return confirm.RequireLiteral(
		reader,
		writer,
		"Full ontology download may fetch a large number of files and consume substantial disk, time, and bandwidth.",
		"should_download_all",
	)
}
