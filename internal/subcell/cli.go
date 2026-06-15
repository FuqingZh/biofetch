package subcell

import (
	"biofetch/internal/shared/cliopt"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

func NewCommand() *cobra.Command {
	commandRoot := &cobra.Command{
		Use:           "subcell",
		Short:         "Manage subcellular protein-location assets and manifest.lock",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	commandRoot.AddCommand(createUniProtCommand())
	return commandRoot
}

func createUniProtCommand() *cobra.Command {
	commandUniProt := &cobra.Command{
		Use:           "uniprot",
		Short:         "Manage UniProt-derived subcellular protein-location assets",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	commandUniProt.AddCommand(createUniProtFetchCommand())
	commandUniProt.AddCommand(createUniProtLockCommand())
	commandUniProt.AddCommand(createUniProtSyncCommand())
	return commandUniProt
}

func createUniProtFetchCommand() *cobra.Command {
	cfg := createDefaultUniProtConfig()
	retryWaitSec := 3
	requestIntervalMs := 0

	commandFetch := &cobra.Command{
		Use:           "fetch",
		Short:         "Fetch UniProt subcellular protein-location annotations and update manifest.lock",
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg.RetryWait = time.Duration(retryWaitSec) * time.Second
			cfg.RequestInterval = time.Duration(requestIntervalMs) * time.Millisecond
			if err := validateUniProtConfig(&cfg); err != nil {
				return err
			}
			return runFetchUniProt(&cfg)
		},
	}
	commandFetch.Example = strings.Join([]string{
		"biofetch subcell uniprot fetch --dir_out /data/subcell --taxids 9606 --should_dry_run",
		"biofetch subcell uniprot fetch --dir_out /data/subcell --species hsa",
		"biofetch subcell uniprot fetch --dir_out /data/subcell --proteome UP000005640",
	}, "\n")
	flags := commandFetch.Flags()
	flags.SortFlags = false
	cliopt.BindDirOutFlag(flags, &cfg.DirOutConfig, "Subcell asset root directory")
	cliopt.BindVersionFlag(flags, &cfg.VersionConfig, "UniProt release or local snapshot token; omit for current")
	flags.StringVar(&cfg.speciesCode, "species", "", "Species shortcut: hsa|mmu|rno|ath|sce")
	flags.StringVar(&cfg.taxID, "taxids", "", "NCBI taxonomy ID scope")
	flags.StringVar(&cfg.proteomeID, "proteome", "", "UniProt proteome ID scope")
	cliopt.BindRuleExistingFlag(flags, &cfg.ExistingRuleConfig, "Rule for existing files: skip|overwrite")
	cliopt.BindRetryFlags(flags, &cfg.RetryConfig, &retryWaitSec)
	cliopt.BindDownloadControlFlags(flags, &cfg.DownloadControlConfig, &requestIntervalMs)
	cliopt.BindInsecureTLSFlag(flags, &cfg.InsecureTLSConfig, "Disable TLS certificate verification")
	cliopt.BindDryRunFlag(flags, &cfg.DryRunConfig, "Print actions only; do not download")
	cliopt.BindLogDirFlag(flags, &cfg.LogConfig, "Directory for run log files; default is <version>/logs/")
	cliopt.BindProgressFlag(flags, &cfg.ProgressConfig, "Disable download progress display")
	return commandFetch
}

func createUniProtLockCommand() *cobra.Command {
	cfg := uniprotLockConfig{}
	commandLock := &cobra.Command{
		Use:           "lock",
		Short:         "Rebuild UniProt subcell manifest.lock from the current version directory",
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
			return runLockUniProt(&cfg)
		},
	}
	flags := commandLock.Flags()
	flags.SortFlags = false
	cliopt.BindDirOutFlag(flags, &cfg.DirOutConfig, "Subcell asset root directory")
	cliopt.BindVersionFlag(flags, &cfg.VersionConfig, "UniProt subcell version token")
	cliopt.BindDryRunFlag(flags, &cfg.DryRunConfig, "Print actions only; do not write manifest")
	cliopt.BindLogDirFlag(flags, &cfg.LogConfig, "Directory for run log files; default is <version>/logs/")
	return commandLock
}

func createUniProtSyncCommand() *cobra.Command {
	cfg := uniprotSyncConfig{}
	cfg.RetryMax = 5
	cfg.RetryWait = 3 * time.Second
	cfg.RuleExisting = "skip"
	cfg.WorkersMax = 1
	retryWaitSec := 3
	requestIntervalMs := 0

	commandSync := &cobra.Command{
		Use:           "sync",
		Short:         "Sync UniProt subcell files from manifest.lock and refresh manifest",
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
			return runSyncUniProt(&cfg)
		},
	}
	flags := commandSync.Flags()
	flags.SortFlags = false
	cliopt.BindDirOutFlag(flags, &cfg.DirOutConfig, "Subcell asset root directory")
	cliopt.BindVersionFlag(flags, &cfg.VersionConfig, "UniProt subcell version token")
	cliopt.BindRuleExistingFlag(flags, &cfg.ExistingRuleConfig, "Rule for existing files: skip|overwrite")
	cliopt.BindRetryFlags(flags, &cfg.RetryConfig, &retryWaitSec)
	cliopt.BindDownloadControlFlags(flags, &cfg.DownloadControlConfig, &requestIntervalMs)
	cliopt.BindInsecureTLSFlag(flags, &cfg.InsecureTLSConfig, "Disable TLS certificate verification")
	cliopt.BindDryRunFlag(flags, &cfg.DryRunConfig, "Print actions only; do not download")
	cliopt.BindLogDirFlag(flags, &cfg.LogConfig, "Directory for run log files; default is <version>/logs/")
	cliopt.BindProgressFlag(flags, &cfg.ProgressConfig, "Disable download progress display")
	return commandSync
}
