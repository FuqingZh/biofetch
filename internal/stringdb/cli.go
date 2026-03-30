package stringdb

import (
	"biofetch/internal/shared/confirm"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

type config struct {
	dirOut                  string
	versionToken            string
	taxIDs                  []string
	fileTaxIDs              string
	shouldDownloadAll       bool
	ruleExisting            string
	shouldOverwriteExisting bool
	retryMax                int
	retryWait               time.Duration
	shouldAllowInsecureTLS  bool
	shouldDryRun            bool
}

func NewCommand() *cobra.Command {
	commandString := &cobra.Command{
		Use:           "string",
		Short:         "Manage STRING raw assets and manifest.lock",
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	commandString.AddCommand(createCatalogCommand())
	commandString.AddCommand(createFetchCommand())
	commandString.AddCommand(createLockCommand())
	commandString.AddCommand(createSyncCommand())
	return commandString
}

func createCatalogCommand() *cobra.Command {
	commandCatalog := &cobra.Command{
		Use:           "catalog",
		Short:         "Manage STRING shared catalog assets",
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	commandCatalog.AddCommand(createCatalogFetchCommand())
	commandCatalog.AddCommand(createCatalogLockCommand())
	commandCatalog.AddCommand(createCatalogSyncCommand())
	return commandCatalog
}

func createFetchCommand() *cobra.Command {
	cfg := createDefaultConfig()
	retryWaitSec := 3

	commandFetch := &cobra.Command{
		Use:           "fetch",
		Short:         "Fetch STRING raw assets and update manifest.lock",
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg.retryWait = time.Duration(retryWaitSec) * time.Second
			if err := validateConfig(cfg); err != nil {
				return err
			}
			if cfg.shouldDownloadAll {
				if err := confirmAllSpeciesDownload(cmd.InOrStdin(), cmd.ErrOrStderr()); err != nil {
					return err
				}
			}
			return runFetch(cfg)
		},
	}

	commandFetch.Example = strings.Join([]string{
		"biofetch string fetch --dir_out /data/string --taxids 7070 --should_dry_run",
		"biofetch string fetch --dir_out /data/string --taxids 7070 --taxids 9606",
		"biofetch string fetch --dir_out /data/string --file_taxids taxids.txt --version v12.0",
	}, "\n")

	flags := commandFetch.Flags()
	flags.SortFlags = false
	flags.StringVar(&cfg.dirOut, "dir_out", cfg.dirOut, "STRING asset root directory")
	flags.StringVar(&cfg.versionToken, "version", cfg.versionToken, "STRING release version token")
	flags.StringSliceVar(&cfg.taxIDs, "taxids", nil, "Taxids; repeat the flag or use commas, e.g. --taxids 7070 --taxids 9606")
	flags.StringVar(&cfg.fileTaxIDs, "file_taxids", "", "File with one taxid per line")
	flags.BoolVar(
		&cfg.shouldDownloadAll,
		"should_download_all",
		false,
		"Download all species listed by STRING",
	)
	flags.StringVar(&cfg.ruleExisting, "rule_existing", cfg.ruleExisting, "Rule for existing files: skip|overwrite")
	flags.IntVar(&cfg.retryMax, "retry_max", cfg.retryMax, "Max retry attempts on download failures")
	flags.IntVar(&retryWaitSec, "retry_wait_sec", retryWaitSec, "Wait seconds between retries")
	flags.BoolVar(
		&cfg.shouldAllowInsecureTLS,
		"should_allow_insecure_tls",
		false,
		"Disable TLS certificate verification",
	)
	flags.BoolVar(&cfg.shouldDryRun, "should_dry_run", false, "Print actions only; do not download")

	return commandFetch
}

func createLockCommand() *cobra.Command {
	cfg := lockConfig{}

	commandLock := &cobra.Command{
		Use:           "lock",
		Short:         "Rebuild STRING manifest.lock from the current version directory",
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if strings.TrimSpace(cfg.dirOut) == "" {
				return fmt.Errorf("dir_out is required")
			}
			if strings.TrimSpace(cfg.versionToken) == "" {
				return fmt.Errorf("version is required")
			}
			return runLock(&cfg)
		},
	}

	flags := commandLock.Flags()
	flags.SortFlags = false
	flags.StringVar(&cfg.dirOut, "dir_out", "", "STRING asset root directory")
	flags.StringVar(&cfg.versionToken, "version", "", "STRING release version token")
	flags.BoolVar(&cfg.shouldDryRun, "should_dry_run", false, "Print actions only; do not write manifest")
	return commandLock
}

func createSyncCommand() *cobra.Command {
	cfg := syncConfig{}
	cfg.retryMax = 5
	cfg.retryWait = 3 * time.Second
	cfg.ruleExisting = "skip"
	retryWaitSec := 3

	commandSync := &cobra.Command{
		Use:           "sync",
		Short:         "Sync STRING files from manifest.lock and refresh manifest",
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg.retryWait = time.Duration(retryWaitSec) * time.Second
			if strings.TrimSpace(cfg.dirOut) == "" {
				return fmt.Errorf("dir_out is required")
			}
			if strings.TrimSpace(cfg.versionToken) == "" {
				return fmt.Errorf("version is required")
			}
			if cfg.retryMax < 1 {
				return fmt.Errorf("retry_max must be >= 1")
			}
			if cfg.ruleExisting != "skip" && cfg.ruleExisting != "overwrite" {
				return fmt.Errorf("rule_existing must be one of: skip, overwrite")
			}
			cfg.shouldOverwriteExisting = cfg.ruleExisting == "overwrite"
			if cfg.retryWait < 0 {
				return fmt.Errorf("retry_wait_sec must be >= 0")
			}
			return runSync(&cfg)
		},
	}

	flags := commandSync.Flags()
	flags.SortFlags = false
	flags.StringVar(&cfg.dirOut, "dir_out", "", "STRING asset root directory")
	flags.StringVar(&cfg.versionToken, "version", "", "STRING release version token")
	flags.StringVar(&cfg.ruleExisting, "rule_existing", cfg.ruleExisting, "Rule for existing files: skip|overwrite")
	flags.IntVar(&cfg.retryMax, "retry_max", cfg.retryMax, "Max retry attempts on download failures")
	flags.IntVar(&retryWaitSec, "retry_wait_sec", retryWaitSec, "Wait seconds between retries")
	flags.BoolVar(&cfg.shouldAllowInsecureTLS, "should_allow_insecure_tls", false, "Disable TLS certificate verification")
	flags.BoolVar(&cfg.shouldDryRun, "should_dry_run", false, "Print actions only; do not download")
	return commandSync
}

func createDefaultConfig() *config {
	cfg := &config{}
	cfg.versionToken = "v12.0"
	cfg.ruleExisting = "skip"
	cfg.retryMax = 5
	cfg.retryWait = 3 * time.Second
	return cfg
}

func validateConfig(cfg *config) error {
	if cfg.retryMax < 1 {
		return fmt.Errorf("retry_max must be >= 1")
	}
	if cfg.retryWait < 0 {
		return fmt.Errorf("retry_wait_sec must be >= 0")
	}
	if strings.TrimSpace(cfg.dirOut) == "" {
		return fmt.Errorf("dir_out is required")
	}
	if cfg.ruleExisting != "skip" && cfg.ruleExisting != "overwrite" {
		return fmt.Errorf("rule_existing must be one of: skip, overwrite")
	}
	cfg.shouldOverwriteExisting = cfg.ruleExisting == "overwrite"

	countSources := 0
	if cfg.shouldDownloadAll {
		countSources++
	}
	if len(cfg.taxIDs) > 0 {
		countSources++
	}
	if strings.TrimSpace(cfg.fileTaxIDs) != "" {
		countSources++
	}
	if countSources != 1 {
		return fmt.Errorf(
			"choose exactly one source: --taxids | --file_taxids | --should_download_all",
		)
	}
	if cfg.fileTaxIDs != "" {
		if _, err := os.Stat(cfg.fileTaxIDs); err != nil {
			return fmt.Errorf("taxids file not found: %w", err)
		}
	}
	if len(cfg.taxIDs) > 0 {
		if _, err := parseTaxIDs(cfg.taxIDs); err != nil {
			return err
		}
	}

	return nil
}

func confirmAllSpeciesDownload(reader io.Reader, writer io.Writer) error {
	return confirm.RequireLiteral(
		reader,
		writer,
		"Full-species download may fetch a large number of files and consume substantial disk, time, and bandwidth.",
		"should_download_all",
	)
}
