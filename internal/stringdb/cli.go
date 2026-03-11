package stringdb

import (
	"bufio"
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

	commandString.AddCommand(createFetchCommand())
	commandString.AddCommand(createLockCommand())
	commandString.AddCommand(createSyncCommand())
	return commandString
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
			if err := validateConfig(*cfg); err != nil {
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
	flags.BoolVar(&cfg.shouldOverwriteExisting, "should_overwrite_existing", false, "Re-download files even if they already exist")
	flags.IntVar(&cfg.retryMax, "retry_max", cfg.retryMax, "Max retry attempts on download failures")
	flags.IntVar(&retryWaitSec, "retry_wait_sec", retryWaitSec, "Wait seconds between retries")
	flags.BoolVar(&cfg.shouldAllowInsecureTLS, "should_allow_insecure_tls", false, "Disable TLS certificate verification")
	flags.BoolVar(&cfg.shouldDryRun, "should_dry_run", false, "Print actions only; do not download")
	return commandSync
}

func createDefaultConfig() *config {
	cfg := &config{}
	cfg.versionToken = "v12.0"
	cfg.retryMax = 5
	cfg.retryWait = 3 * time.Second
	return cfg
}

func validateConfig(cfg config) error {
	if cfg.retryMax < 1 {
		return fmt.Errorf("retry_max must be >= 1")
	}
	if cfg.retryWait < 0 {
		return fmt.Errorf("retry_wait_sec must be >= 0")
	}
	if strings.TrimSpace(cfg.dirOut) == "" {
		return fmt.Errorf("dir_out is required")
	}

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
	const textConfirm = "should_download_all"

	if _, err := fmt.Fprintf(
		writer,
		"Full-species download may fetch a large number of files and consume substantial disk, time, and bandwidth.\n",
	); err != nil {
		return fmt.Errorf("write confirmation prompt: %w", err)
	}
	if _, err := fmt.Fprintf(writer, "Type %q to continue.\n", textConfirm); err != nil {
		return fmt.Errorf("write confirmation prompt: %w", err)
	}
	if _, err := io.WriteString(writer, "> "); err != nil {
		return fmt.Errorf("write confirmation prompt: %w", err)
	}

	textInput, err := bufio.NewReader(reader).ReadString('\n')
	if err != nil && err != io.EOF {
		return fmt.Errorf("read confirmation input: %w", err)
	}
	if strings.TrimSpace(textInput) != textConfirm {
		return fmt.Errorf("confirmation failed; expected %q", textConfirm)
	}

	return nil
}
