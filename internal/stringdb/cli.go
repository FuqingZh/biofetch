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
	dirOut                   string
	versionToken             string
	taxIDsCSV                string
	fileTaxIDs               string
	shouldDownloadAllSpecies bool
	shouldOverwriteExisting  bool
	retryMax                 int
	retryWait                time.Duration
	shouldAllowInsecureTLS   bool
	shouldDryRun             bool
}

func NewCommand() *cobra.Command {
	cfg := createDefaultConfig()
	retryWaitSec := 3

	commandString := &cobra.Command{
		Use:           "string",
		Short:         "Fetch STRING raw assets and write manifest.lock",
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg.retryWait = time.Duration(retryWaitSec) * time.Second
			if err := validateConfig(*cfg); err != nil {
				return err
			}
			if cfg.shouldDownloadAllSpecies {
				if err := confirmAllSpeciesDownload(cmd.InOrStdin(), cmd.ErrOrStderr()); err != nil {
					return err
				}
			}
			return runFetch(cfg)
		},
	}

	commandString.Example = strings.Join([]string{
		"biofetch string --dir_out /data/string --taxids 7070 --should_dry_run",
		"biofetch string --dir_out /data/string --file_taxids taxids.txt --version v12.0",
	}, "\n")

	flags := commandString.Flags()
	flags.SortFlags = false
	flags.StringVar(&cfg.dirOut, "dir_out", cfg.dirOut, "STRING asset root directory")
	flags.StringVar(&cfg.versionToken, "version", cfg.versionToken, "STRING release version token")
	flags.StringVar(&cfg.taxIDsCSV, "taxids", "", "Comma-separated taxids, e.g. 7070,9606,10090")
	flags.StringVar(&cfg.fileTaxIDs, "file_taxids", "", "File with one taxid per line")
	flags.BoolVar(
		&cfg.shouldDownloadAllSpecies,
		"should_download_all_species",
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

	return commandString
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
	if cfg.shouldDownloadAllSpecies {
		countSources++
	}
	if strings.TrimSpace(cfg.taxIDsCSV) != "" {
		countSources++
	}
	if strings.TrimSpace(cfg.fileTaxIDs) != "" {
		countSources++
	}
	if countSources != 1 {
		return fmt.Errorf(
			"choose exactly one source: --taxids | --file_taxids | --should_download_all_species",
		)
	}
	if cfg.fileTaxIDs != "" {
		if _, err := os.Stat(cfg.fileTaxIDs); err != nil {
			return fmt.Errorf("taxids file not found: %w", err)
		}
	}

	return nil
}

func confirmAllSpeciesDownload(reader io.Reader, writer io.Writer) error {
	const textConfirm = "should_download_all_species"

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
