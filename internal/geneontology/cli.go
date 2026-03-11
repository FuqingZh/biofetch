package geneontology

import (
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

type ontologyConfig struct {
	dirOut                  string
	version                 string
	versionToken            string
	assetsCSV               string
	shouldOverwriteExisting bool
	retryMax                int
	retryWait               time.Duration
	shouldAllowInsecureTLS  bool
	shouldDryRun            bool
}

func NewCommand() *cobra.Command {
	commandGO := &cobra.Command{
		Use:           "go",
		Short:         "Fetch Gene Ontology raw assets and write manifest.lock",
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	commandGO.AddCommand(createOntologyCommand())
	return commandGO
}

func createOntologyCommand() *cobra.Command {
	cfg := createDefaultOntologyConfig()
	retryWaitSec := 3

	commandOntology := &cobra.Command{
		Use:           "ontology",
		Short:         "Fetch Gene Ontology ontology raw assets",
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg.retryWait = time.Duration(retryWaitSec) * time.Second
			if err := validateOntologyConfig(cfg); err != nil {
				return err
			}
			return runFetchOntology(&cfg)
		},
	}

	commandOntology.Example = strings.Join([]string{
		"biofetch go ontology --dir_out /data/go --should_dry_run",
		"biofetch go ontology --dir_out /data/go --assets go-basic.obo,go-plus.json",
	}, "\n")

	flags := commandOntology.Flags()
	flags.SortFlags = false
	flags.StringVar(&cfg.dirOut, "dir_out", cfg.dirOut, "GO asset root directory")
	flags.StringVar(
		&cfg.assetsCSV,
		"assets",
		cfg.assetsCSV,
		"Comma-separated ontology assets, e.g. go-basic.obo,go.obo,go-plus.json",
	)
	flags.BoolVar(
		&cfg.shouldOverwriteExisting,
		"should_overwrite_existing",
		false,
		"Re-download existing files",
	)
	flags.IntVar(&cfg.retryMax, "retry_max", cfg.retryMax, "Max retry attempts on download failures")
	flags.IntVar(&retryWaitSec, "retry_wait_sec", retryWaitSec, "Wait seconds between retries")
	flags.BoolVar(
		&cfg.shouldAllowInsecureTLS,
		"should_allow_insecure_tls",
		false,
		"Disable TLS certificate verification",
	)
	flags.BoolVar(&cfg.shouldDryRun, "should_dry_run", false, "Print actions only; do not download")

	return commandOntology
}

func createDefaultOntologyConfig() ontologyConfig {
	cfg := ontologyConfig{}
	cfg.assetsCSV = strings.Join(defaultOntologyAssetNames, ",")
	cfg.retryMax = 5
	cfg.retryWait = 3 * time.Second
	return cfg
}

func validateOntologyConfig(cfg ontologyConfig) error {
	if cfg.retryMax < 1 {
		return fmt.Errorf("retry_max must be >= 1")
	}
	if cfg.retryWait < 0 {
		return fmt.Errorf("retry_wait_sec must be >= 0")
	}
	if strings.TrimSpace(cfg.dirOut) == "" {
		return fmt.Errorf("dir_out is required")
	}

	if _, err := parseOntologyAssetNames(cfg.assetsCSV); err != nil {
		return err
	}
	return nil
}
