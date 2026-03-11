package kegg

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

type pathwayConfig struct {
	dirOut                  string
	version                 string
	versionToken            string
	organismCode            string
	pathwayIDsCSV           string
	filePathwayIDs          string
	shouldFetchReference    bool
	shouldOverwriteExisting bool
	retryMax                int
	retryWait               time.Duration
	requestInterval         time.Duration
	shouldAllowInsecureTLS  bool
	shouldDryRun            bool
}

type briteConfig struct {
	dirOut                  string
	version                 string
	versionToken            string
	catalogCode             string
	briteIDsCSV             string
	fileBriteIDs            string
	shouldOverwriteExisting bool
	retryMax                int
	retryWait               time.Duration
	requestInterval         time.Duration
	shouldAllowInsecureTLS  bool
	shouldDryRun            bool
}

func RunCLI(args []string) error {
	commandRoot := NewCommand()
	commandRoot.SetArgs(args)
	return commandRoot.Execute()
}

func NewCommand() *cobra.Command {
	commandRoot := &cobra.Command{
		Use:           "kegg",
		Short:         "Fetch KEGG raw assets and write manifest.lock",
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	commandRoot.AddCommand(createPathwayCommand())
	commandRoot.AddCommand(createBriteCommand())
	return commandRoot
}

func createPathwayCommand() *cobra.Command {
	cfg := createDefaultPathwayConfig()
	retryWaitSec := 3
	requestIntervalMs := 350

	commandPathway := &cobra.Command{
		Use:           "pathway",
		Short:         "Fetch KEGG PATHWAY raw assets",
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg.retryWait = time.Duration(retryWaitSec) * time.Second
			cfg.requestInterval = time.Duration(requestIntervalMs) * time.Millisecond
			if err := validatePathwayConfig(cfg); err != nil {
				return err
			}
			return runFetchPathway(&cfg)
		},
	}

	commandPathway.Example = strings.Join([]string{
		"biofetch kegg pathway --dir_out /data/kegg --organism hsa --should_dry_run",
		"biofetch kegg pathway --dir_out /data/kegg --should_fetch_reference --pathway_ids map00010,map00020",
	}, "\n")

	flags := commandPathway.Flags()
	flags.SortFlags = false
	flags.StringVar(&cfg.dirOut, "dir_out", cfg.dirOut, "KEGG asset root directory")
	flags.StringVar(&cfg.organismCode, "organism", "", "KEGG organism code, e.g. hsa or tca")
	flags.StringVar(&cfg.pathwayIDsCSV, "pathway_ids", "", "Comma-separated pathway IDs")
	flags.StringVar(&cfg.filePathwayIDs, "file_pathway_ids", "", "File with one pathway ID per line")
	flags.BoolVar(
		&cfg.shouldFetchReference,
		"should_fetch_reference",
		false,
		"Fetch reference pathways from /list/pathway",
	)
	flags.BoolVar(
		&cfg.shouldOverwriteExisting,
		"should_overwrite_existing",
		false,
		"Re-download existing files",
	)
	flags.IntVar(&cfg.retryMax, "retry_max", cfg.retryMax, "Max retry attempts on download failures")
	flags.IntVar(&retryWaitSec, "retry_wait_sec", retryWaitSec, "Wait seconds between retries")
	flags.IntVar(
		&requestIntervalMs,
		"request_interval_ms",
		requestIntervalMs,
		"Delay between KEGG API requests in milliseconds",
	)
	flags.BoolVar(
		&cfg.shouldAllowInsecureTLS,
		"should_allow_insecure_tls",
		false,
		"Disable TLS certificate verification",
	)
	flags.BoolVar(&cfg.shouldDryRun, "should_dry_run", false, "Print actions only; do not download")

	return commandPathway
}

func createDefaultPathwayConfig() pathwayConfig {
	cfg := pathwayConfig{}
	cfg.retryMax = 5
	cfg.retryWait = 3 * time.Second
	cfg.requestInterval = 350 * time.Millisecond
	return cfg
}

func validatePathwayConfig(cfg pathwayConfig) error {
	if cfg.retryMax < 1 {
		return fmt.Errorf("retry_max must be >= 1")
	}
	if cfg.retryWait < 0 {
		return fmt.Errorf("retry_wait_sec must be >= 0")
	}
	if cfg.requestInterval < 0 {
		return fmt.Errorf("request_interval_ms must be >= 0")
	}
	if strings.TrimSpace(cfg.dirOut) == "" {
		return fmt.Errorf("dir_out is required")
	}

	countScope := 0
	if strings.TrimSpace(cfg.organismCode) != "" {
		countScope++
	}
	if cfg.shouldFetchReference {
		countScope++
	}
	if countScope != 1 {
		return fmt.Errorf("choose exactly one scope: --organism | --should_fetch_reference")
	}
	if cfg.filePathwayIDs != "" {
		if _, err := os.Stat(cfg.filePathwayIDs); err != nil {
			return fmt.Errorf("pathway ids file not found: %w", err)
		}
	}

	return nil
}

func createBriteCommand() *cobra.Command {
	cfg := createDefaultBriteConfig()
	retryWaitSec := 3
	requestIntervalMs := 350

	commandBrite := &cobra.Command{
		Use:           "brite",
		Short:         "Fetch KEGG BRITE raw assets",
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg.retryWait = time.Duration(retryWaitSec) * time.Second
			cfg.requestInterval = time.Duration(requestIntervalMs) * time.Millisecond
			if err := validateBriteConfig(cfg); err != nil {
				return err
			}
			return runFetchBrite(&cfg)
		},
	}

	commandBrite.Example = strings.Join([]string{
		"biofetch kegg brite --dir_out /data/kegg --catalog br --should_dry_run",
		"biofetch kegg brite --dir_out /data/kegg --catalog hsa --brite_ids hsa00001",
	}, "\n")

	flags := commandBrite.Flags()
	flags.SortFlags = false
	flags.StringVar(&cfg.dirOut, "dir_out", cfg.dirOut, "KEGG asset root directory")
	flags.StringVar(&cfg.catalogCode, "catalog", "", "BRITE collection code; use br or ko for reference, or an organism code such as hsa or tcar")
	flags.StringVar(&cfg.briteIDsCSV, "brite_ids", "", "Comma-separated BRITE IDs, e.g. br08301,hsa00001")
	flags.StringVar(&cfg.fileBriteIDs, "file_brite_ids", "", "File with one BRITE ID per line")
	flags.BoolVar(
		&cfg.shouldOverwriteExisting,
		"should_overwrite_existing",
		false,
		"Re-download existing files",
	)
	flags.IntVar(&cfg.retryMax, "retry_max", cfg.retryMax, "Max retry attempts on download failures")
	flags.IntVar(&retryWaitSec, "retry_wait_sec", retryWaitSec, "Wait seconds between retries")
	flags.IntVar(
		&requestIntervalMs,
		"request_interval_ms",
		requestIntervalMs,
		"Delay between KEGG API requests in milliseconds",
	)
	flags.BoolVar(
		&cfg.shouldAllowInsecureTLS,
		"should_allow_insecure_tls",
		false,
		"Disable TLS certificate verification",
	)
	flags.BoolVar(&cfg.shouldDryRun, "should_dry_run", false, "Print actions only; do not download")

	return commandBrite
}

func createDefaultBriteConfig() briteConfig {
	cfg := briteConfig{}
	cfg.retryMax = 5
	cfg.retryWait = 3 * time.Second
	cfg.requestInterval = 350 * time.Millisecond
	return cfg
}

func validateBriteConfig(cfg briteConfig) error {
	if cfg.retryMax < 1 {
		return fmt.Errorf("retry_max must be >= 1")
	}
	if cfg.retryWait < 0 {
		return fmt.Errorf("retry_wait_sec must be >= 0")
	}
	if cfg.requestInterval < 0 {
		return fmt.Errorf("request_interval_ms must be >= 0")
	}
	if strings.TrimSpace(cfg.dirOut) == "" {
		return fmt.Errorf("dir_out is required")
	}
	if strings.TrimSpace(cfg.catalogCode) == "" {
		return fmt.Errorf("catalog is required")
	}
	if cfg.fileBriteIDs != "" {
		if _, err := os.Stat(cfg.fileBriteIDs); err != nil {
			return fmt.Errorf("brite ids file not found: %w", err)
		}
	}
	return nil
}
