package geneontology

import (
	"bufio"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

type ontologyConfig struct {
	dirOut                  string
	version                 string
	versionToken            string
	assetNames              []string
	shouldDownloadAll       bool
	shouldOverwriteExisting bool
	retryMax                int
	retryWait               time.Duration
	shouldAllowInsecureTLS  bool
	shouldDryRun            bool
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
			cfg.retryWait = time.Duration(retryWaitSec) * time.Second
			if err := validateOntologyConfig(cfg); err != nil {
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
	flags.StringVar(&cfg.dirOut, "dir_out", cfg.dirOut, "GO asset root directory")
	flags.StringSliceVar(
		&cfg.assetNames,
		"assets",
		nil,
		"Ontology assets; repeat the flag or use commas, e.g. --assets go-basic.obo --assets go.obo",
	)
	flags.BoolVar(&cfg.shouldDownloadAll, "should_download_all", false, "Discover and download all ontology files")
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

func createOntologyLockCommand() *cobra.Command {
	cfg := ontologyLockConfig{}

	commandLock := &cobra.Command{
		Use:           "lock",
		Short:         "Rebuild GO ontology manifest.lock from the current version directory",
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
			return runLockOntology(&cfg)
		},
	}

	flags := commandLock.Flags()
	flags.SortFlags = false
	flags.StringVar(&cfg.dirOut, "dir_out", "", "GO asset root directory")
	flags.StringVar(&cfg.versionToken, "version", "", "GO ontology version token")
	flags.BoolVar(&cfg.shouldDryRun, "should_dry_run", false, "Print actions only; do not write manifest")
	return commandLock
}

func createOntologySyncCommand() *cobra.Command {
	cfg := ontologySyncConfig{}
	cfg.retryMax = 5
	cfg.retryWait = 3 * time.Second
	retryWaitSec := 3

	commandSync := &cobra.Command{
		Use:           "sync",
		Short:         "Sync GO ontology files from manifest.lock and refresh manifest",
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
			return runSyncOntology(&cfg)
		},
	}

	flags := commandSync.Flags()
	flags.SortFlags = false
	flags.StringVar(&cfg.dirOut, "dir_out", "", "GO asset root directory")
	flags.StringVar(&cfg.versionToken, "version", "", "GO ontology version token")
	flags.BoolVar(&cfg.shouldOverwriteExisting, "should_overwrite_existing", false, "Re-download files even if they already exist")
	flags.IntVar(&cfg.retryMax, "retry_max", cfg.retryMax, "Max retry attempts on download failures")
	flags.IntVar(&retryWaitSec, "retry_wait_sec", retryWaitSec, "Wait seconds between retries")
	flags.BoolVar(&cfg.shouldAllowInsecureTLS, "should_allow_insecure_tls", false, "Disable TLS certificate verification")
	flags.BoolVar(&cfg.shouldDryRun, "should_dry_run", false, "Print actions only; do not download")
	return commandSync
}

func createDefaultOntologyConfig() ontologyConfig {
	cfg := ontologyConfig{}
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
	const textConfirm = "should_download_all"

	if _, err := fmt.Fprintf(
		writer,
		"Full ontology download may fetch a large number of files and consume substantial disk, time, and bandwidth.\n",
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
