package omnipath

import (
	"biofetch/internal/shared/confirm"
	"biofetch/internal/shared/sets"
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

type configEnzSub struct {
	dirOut                  string
	organisms               []string
	fileOrganisms           string
	shouldDownloadAll       bool
	ruleLicense             string
	shouldOverwriteExisting bool
	retryMax                int
	retryWait               time.Duration
	shouldAllowInsecureTLS  bool
	shouldDryRun            bool
}

type configInteractions struct {
	dirOut                  string
	organisms               []string
	fileOrganisms           string
	shouldDownloadAll       bool
	dataset                 string
	ruleLicense             string
	shouldOverwriteExisting bool
	retryMax                int
	retryWait               time.Duration
	shouldAllowInsecureTLS  bool
	shouldDryRun            bool
}

func NewCommand() *cobra.Command {
	commandRoot := &cobra.Command{
		Use:           "omnipath",
		Short:         "Manage OmniPath raw assets and manifest.lock",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	commandRoot.AddCommand(createEnzSubCommand())
	commandRoot.AddCommand(createInteractionsCommand())
	return commandRoot
}

func createEnzSubCommand() *cobra.Command {
	command := &cobra.Command{
		Use:           "enz_sub",
		Short:         "Manage OmniPath enzyme-PTM relationships",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	command.AddCommand(createEnzSubFetchCommand())
	command.AddCommand(createEnzSubLockCommand())
	command.AddCommand(createEnzSubSyncCommand())
	return command
}

func createEnzSubFetchCommand() *cobra.Command {
	cfg := configEnzSub{}
	cfg.retryMax = 5
	cfg.retryWait = 3 * time.Second
	retryWaitSec := 3

	command := &cobra.Command{
		Use:           "fetch",
		Short:         "Fetch OmniPath enzyme-PTM relationships and update manifest.lock",
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg.retryWait = time.Duration(retryWaitSec) * time.Second
			if err := validateEnzSubConfig(cfg); err != nil {
				return err
			}
			if cfg.shouldDownloadAll {
				if err := confirmAllOrganismsDownload(cmd.InOrStdin(), cmd.ErrOrStderr()); err != nil {
					return err
				}
			}
			return runFetchEnzSub(&cfg)
		},
	}

	flags := command.Flags()
	flags.SortFlags = false
	flags.StringVar(&cfg.dirOut, "dir_out", cfg.dirOut, "OmniPath asset root directory")
	flags.StringSliceVar(&cfg.organisms, "organisms", nil, "Organism taxids; repeat the flag or use commas, e.g. 9606,10090,10116")
	flags.StringVar(&cfg.fileOrganisms, "file_organisms", "", "File with one organism per line")
	flags.BoolVar(&cfg.shouldDownloadAll, "should_download_all", false, "Fetch all supported organisms")
	flags.StringVar(&cfg.ruleLicense, "rule_license", "", "License mode: academic|commercial")
	flags.BoolVar(&cfg.shouldOverwriteExisting, "should_overwrite_existing", false, "Re-download existing files")
	flags.IntVar(&cfg.retryMax, "retry_max", cfg.retryMax, "Max retry attempts on download failures")
	flags.IntVar(&retryWaitSec, "retry_wait_sec", retryWaitSec, "Wait seconds between retries")
	flags.BoolVar(&cfg.shouldAllowInsecureTLS, "should_allow_insecure_tls", false, "Disable TLS certificate verification")
	flags.BoolVar(&cfg.shouldDryRun, "should_dry_run", false, "Print actions only; do not download")

	return command
}

func createInteractionsCommand() *cobra.Command {
	command := &cobra.Command{
		Use:           "interactions",
		Short:         "Manage OmniPath interactions datasets",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	command.AddCommand(createInteractionsFetchCommand())
	command.AddCommand(createInteractionsLockCommand())
	command.AddCommand(createInteractionsSyncCommand())
	return command
}

func createInteractionsFetchCommand() *cobra.Command {
	cfg := configInteractions{}
	cfg.dataset = "kinaseextra"
	cfg.retryMax = 5
	cfg.retryWait = 3 * time.Second
	retryWaitSec := 3

	command := &cobra.Command{
		Use:           "fetch",
		Short:         "Fetch OmniPath interactions datasets and update manifest.lock",
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg.retryWait = time.Duration(retryWaitSec) * time.Second
			if err := validateInteractionsConfig(cfg); err != nil {
				return err
			}
			if cfg.shouldDownloadAll {
				if err := confirmAllOrganismsDownload(cmd.InOrStdin(), cmd.ErrOrStderr()); err != nil {
					return err
				}
			}
			return runFetchInteractions(&cfg)
		},
	}

	flags := command.Flags()
	flags.SortFlags = false
	flags.StringVar(&cfg.dirOut, "dir_out", cfg.dirOut, "OmniPath asset root directory")
	flags.StringSliceVar(&cfg.organisms, "organisms", nil, "Organism taxids; repeat the flag or use commas, e.g. 9606,10090,10116")
	flags.StringVar(&cfg.fileOrganisms, "file_organisms", "", "File with one organism per line")
	flags.BoolVar(&cfg.shouldDownloadAll, "should_download_all", false, "Fetch all supported organisms")
	flags.StringVar(&cfg.dataset, "dataset", cfg.dataset, "Interactions dataset (v1 supports only kinaseextra)")
	flags.StringVar(&cfg.ruleLicense, "rule_license", "", "License mode: academic|commercial")
	flags.BoolVar(&cfg.shouldOverwriteExisting, "should_overwrite_existing", false, "Re-download existing files")
	flags.IntVar(&cfg.retryMax, "retry_max", cfg.retryMax, "Max retry attempts on download failures")
	flags.IntVar(&retryWaitSec, "retry_wait_sec", retryWaitSec, "Wait seconds between retries")
	flags.BoolVar(&cfg.shouldAllowInsecureTLS, "should_allow_insecure_tls", false, "Disable TLS certificate verification")
	flags.BoolVar(&cfg.shouldDryRun, "should_dry_run", false, "Print actions only; do not download")

	return command
}

func createEnzSubLockCommand() *cobra.Command {
	cfg := lockConfig{}

	command := &cobra.Command{
		Use:           "lock",
		Short:         "Rebuild OmniPath enz_sub manifest.lock from the current version directory",
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
			return runLockEnzSub(&cfg)
		},
	}

	flags := command.Flags()
	flags.SortFlags = false
	flags.StringVar(&cfg.dirOut, "dir_out", "", "OmniPath asset root directory")
	flags.StringVar(&cfg.versionToken, "version", "", "OmniPath version token")
	flags.BoolVar(&cfg.shouldDryRun, "should_dry_run", false, "Print actions only; do not write manifest")
	return command
}

func createEnzSubSyncCommand() *cobra.Command {
	cfg := syncConfig{}
	cfg.retryMax = 5
	cfg.retryWait = 3 * time.Second
	retryWaitSec := 3

	command := &cobra.Command{
		Use:           "sync",
		Short:         "Sync OmniPath enz_sub files from manifest.lock and refresh manifest",
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
			return runSyncEnzSub(&cfg)
		},
	}

	flags := command.Flags()
	flags.SortFlags = false
	flags.StringVar(&cfg.dirOut, "dir_out", "", "OmniPath asset root directory")
	flags.StringVar(&cfg.versionToken, "version", "", "OmniPath version token")
	flags.BoolVar(&cfg.shouldOverwriteExisting, "should_overwrite_existing", false, "Re-download files even if they already exist")
	flags.IntVar(&cfg.retryMax, "retry_max", cfg.retryMax, "Max retry attempts on download failures")
	flags.IntVar(&retryWaitSec, "retry_wait_sec", retryWaitSec, "Wait seconds between retries")
	flags.BoolVar(&cfg.shouldAllowInsecureTLS, "should_allow_insecure_tls", false, "Disable TLS certificate verification")
	flags.BoolVar(&cfg.shouldDryRun, "should_dry_run", false, "Print actions only; do not download")
	return command
}

func createInteractionsLockCommand() *cobra.Command {
	cfg := lockConfig{}
	cfg.dataset = "kinaseextra"

	command := &cobra.Command{
		Use:           "lock",
		Short:         "Rebuild OmniPath interactions manifest.lock from the current version directory",
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
			return runLockInteractions(&cfg)
		},
	}

	flags := command.Flags()
	flags.SortFlags = false
	flags.StringVar(&cfg.dirOut, "dir_out", "", "OmniPath asset root directory")
	flags.StringVar(&cfg.versionToken, "version", "", "OmniPath version token")
	flags.StringVar(&cfg.dataset, "dataset", cfg.dataset, "Interactions dataset (v1 supports only kinaseextra)")
	flags.BoolVar(&cfg.shouldDryRun, "should_dry_run", false, "Print actions only; do not write manifest")
	return command
}

func createInteractionsSyncCommand() *cobra.Command {
	cfg := syncConfig{}
	cfg.dataset = "kinaseextra"
	cfg.retryMax = 5
	cfg.retryWait = 3 * time.Second
	retryWaitSec := 3

	command := &cobra.Command{
		Use:           "sync",
		Short:         "Sync OmniPath interactions files from manifest.lock and refresh manifest",
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
			if strings.TrimSpace(strings.ToLower(cfg.dataset)) != "kinaseextra" {
				return fmt.Errorf("dataset must be kinaseextra in v1")
			}
			return runSyncInteractions(&cfg)
		},
	}

	flags := command.Flags()
	flags.SortFlags = false
	flags.StringVar(&cfg.dirOut, "dir_out", "", "OmniPath asset root directory")
	flags.StringVar(&cfg.versionToken, "version", "", "OmniPath version token")
	flags.StringVar(&cfg.dataset, "dataset", cfg.dataset, "Interactions dataset (v1 supports only kinaseextra)")
	flags.BoolVar(&cfg.shouldOverwriteExisting, "should_overwrite_existing", false, "Re-download files even if they already exist")
	flags.IntVar(&cfg.retryMax, "retry_max", cfg.retryMax, "Max retry attempts on download failures")
	flags.IntVar(&retryWaitSec, "retry_wait_sec", retryWaitSec, "Wait seconds between retries")
	flags.BoolVar(&cfg.shouldAllowInsecureTLS, "should_allow_insecure_tls", false, "Disable TLS certificate verification")
	flags.BoolVar(&cfg.shouldDryRun, "should_dry_run", false, "Print actions only; do not download")
	return command
}

func validateEnzSubConfig(cfg configEnzSub) error {
	if strings.TrimSpace(cfg.dirOut) == "" {
		return fmt.Errorf("dir_out is required")
	}
	countSources := 0
	if len(cfg.organisms) > 0 {
		countSources++
	}
	if strings.TrimSpace(cfg.fileOrganisms) != "" {
		countSources++
	}
	if cfg.shouldDownloadAll {
		countSources++
	}
	if countSources != 1 {
		return fmt.Errorf("choose exactly one source: --organisms | --file_organisms | --should_download_all")
	}
	if len(cfg.organisms) > 0 {
		if _, err := parseOrganisms(cfg.organisms); err != nil {
			return err
		}
	}
	if strings.TrimSpace(cfg.fileOrganisms) != "" {
		if _, err := readOrganismsFromFile(cfg.fileOrganisms); err != nil {
			return err
		}
	}
	if err := validateRuleLicense(cfg.ruleLicense); err != nil {
		return err
	}
	if cfg.retryMax < 1 {
		return fmt.Errorf("retry_max must be >= 1")
	}
	if cfg.retryWait < 0 {
		return fmt.Errorf("retry_wait_sec must be >= 0")
	}
	return nil
}

func validateInteractionsConfig(cfg configInteractions) error {
	if strings.TrimSpace(cfg.dirOut) == "" {
		return fmt.Errorf("dir_out is required")
	}
	countSources := 0
	if len(cfg.organisms) > 0 {
		countSources++
	}
	if strings.TrimSpace(cfg.fileOrganisms) != "" {
		countSources++
	}
	if cfg.shouldDownloadAll {
		countSources++
	}
	if countSources != 1 {
		return fmt.Errorf("choose exactly one source: --organisms | --file_organisms | --should_download_all")
	}
	if len(cfg.organisms) > 0 {
		if _, err := parseOrganisms(cfg.organisms); err != nil {
			return err
		}
	}
	if strings.TrimSpace(cfg.fileOrganisms) != "" {
		if _, err := readOrganismsFromFile(cfg.fileOrganisms); err != nil {
			return err
		}
	}
	if err := validateRuleLicense(cfg.ruleLicense); err != nil {
		return err
	}
	if strings.TrimSpace(strings.ToLower(cfg.dataset)) != "kinaseextra" {
		return fmt.Errorf("dataset must be kinaseextra in v1")
	}
	if cfg.retryMax < 1 {
		return fmt.Errorf("retry_max must be >= 1")
	}
	if cfg.retryWait < 0 {
		return fmt.Errorf("retry_wait_sec must be >= 0")
	}
	return nil
}

func confirmAllOrganismsDownload(reader io.Reader, writer io.Writer) error {
	return confirm.RequireLiteral(
		reader,
		writer,
		"Multi-organism download may fetch a large number of files and consume substantial disk, time, and bandwidth.",
		"should_download_all",
	)
}

func parseOrganisms(valuesInput []string) ([]string, error) {
	setTaxIDs := make(map[string]struct{})
	for _, valueInput := range valuesInput {
		for _, token := range strings.Split(valueInput, ",") {
			taxID, err := normalizeOrganism(token)
			if err != nil {
				return nil, err
			}
			if taxID != "" {
				setTaxIDs[taxID] = struct{}{}
			}
		}
	}
	if len(setTaxIDs) == 0 {
		return nil, fmt.Errorf("organisms must not be empty")
	}
	return sets.SortedKeys(setTaxIDs), nil
}

func readOrganismsFromFile(filePath string) ([]string, error) {
	fileIn, err := os.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("open organisms file: %w", err)
	}
	defer fileIn.Close()

	setTaxIDs := make(map[string]struct{})
	scanner := bufio.NewScanner(fileIn)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		taxID, err := normalizeOrganism(line)
		if err != nil {
			return nil, fmt.Errorf("invalid organism in %s: %w", filePath, err)
		}
		setTaxIDs[taxID] = struct{}{}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read organisms file: %w", err)
	}
	if len(setTaxIDs) == 0 {
		return nil, fmt.Errorf("organisms file must not be empty: %s", filePath)
	}
	return sets.SortedKeys(setTaxIDs), nil
}

func validateRuleLicense(ruleLicense string) error {
	if strings.TrimSpace(ruleLicense) == "" {
		return nil
	}
	value := strings.ToLower(strings.TrimSpace(ruleLicense))
	if value == "academic" || value == "commercial" {
		return nil
	}
	return fmt.Errorf("rule_license must be one of: academic, commercial")
}

func normalizeOrganism(value string) (string, error) {
	taxID := strings.TrimSpace(value)
	if taxID == "" {
		return "", nil
	}
	for _, char := range taxID {
		if char < '0' || char > '9' {
			return "", fmt.Errorf("organism must be a numeric taxid, e.g. 9606")
		}
	}
	return taxID, nil
}
