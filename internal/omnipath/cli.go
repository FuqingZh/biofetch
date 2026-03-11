package omnipath

import (
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

type configEnzSub struct {
	dirOut                  string
	organism                string
	ruleLicense             string
	shouldOverwriteExisting bool
	retryMax                int
	retryWait               time.Duration
	shouldAllowInsecureTLS  bool
	shouldDryRun            bool
}

type configInteractions struct {
	dirOut                  string
	organism                string
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
		Short:         "Fetch OmniPath raw assets and write manifest.lock",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	commandRoot.AddCommand(createEnzSubCommand())
	commandRoot.AddCommand(createInteractionsCommand())
	return commandRoot
}

func createEnzSubCommand() *cobra.Command {
	cfg := configEnzSub{}
	cfg.organism = "human"
	cfg.retryMax = 5
	cfg.retryWait = 3 * time.Second
	retryWaitSec := 3

	command := &cobra.Command{
		Use:           "enz_sub",
		Short:         "Fetch OmniPath enzyme-PTM relationships",
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg.retryWait = time.Duration(retryWaitSec) * time.Second
			if err := validateEnzSubConfig(cfg); err != nil {
				return err
			}
			return runFetchEnzSub(&cfg)
		},
	}

	flags := command.Flags()
	flags.SortFlags = false
	flags.StringVar(&cfg.dirOut, "dir_out", cfg.dirOut, "OmniPath asset root directory")
	flags.StringVar(&cfg.organism, "organism", cfg.organism, "Organism: human|mouse|rat or taxid 9606|10090|10116")
	flags.StringVar(&cfg.ruleLicense, "rule_license", "", "License mode: academic|commercial")
	flags.BoolVar(&cfg.shouldOverwriteExisting, "should_overwrite_existing", false, "Re-download existing files")
	flags.IntVar(&cfg.retryMax, "retry_max", cfg.retryMax, "Max retry attempts on download failures")
	flags.IntVar(&retryWaitSec, "retry_wait_sec", retryWaitSec, "Wait seconds between retries")
	flags.BoolVar(&cfg.shouldAllowInsecureTLS, "should_allow_insecure_tls", false, "Disable TLS certificate verification")
	flags.BoolVar(&cfg.shouldDryRun, "should_dry_run", false, "Print actions only; do not download")

	return command
}

func createInteractionsCommand() *cobra.Command {
	cfg := configInteractions{}
	cfg.organism = "human"
	cfg.dataset = "kinaseextra"
	cfg.retryMax = 5
	cfg.retryWait = 3 * time.Second
	retryWaitSec := 3

	command := &cobra.Command{
		Use:           "interactions",
		Short:         "Fetch OmniPath interactions datasets",
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg.retryWait = time.Duration(retryWaitSec) * time.Second
			if err := validateInteractionsConfig(cfg); err != nil {
				return err
			}
			return runFetchInteractions(&cfg)
		},
	}

	flags := command.Flags()
	flags.SortFlags = false
	flags.StringVar(&cfg.dirOut, "dir_out", cfg.dirOut, "OmniPath asset root directory")
	flags.StringVar(&cfg.organism, "organism", cfg.organism, "Organism: human|mouse|rat or taxid 9606|10090|10116")
	flags.StringVar(&cfg.dataset, "dataset", cfg.dataset, "Interactions dataset (v1 supports only kinaseextra)")
	flags.StringVar(&cfg.ruleLicense, "rule_license", "", "License mode: academic|commercial")
	flags.BoolVar(&cfg.shouldOverwriteExisting, "should_overwrite_existing", false, "Re-download existing files")
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
	if _, err := normalizeOrganism(cfg.organism); err != nil {
		return err
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
	if _, err := normalizeOrganism(cfg.organism); err != nil {
		return err
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
	value = strings.TrimSpace(strings.ToLower(value))
	mapOrganism := map[string]string{
		"human": "9606",
		"mouse": "10090",
		"rat":   "10116",
		"9606":  "9606",
		"10090": "10090",
		"10116": "10116",
	}
	taxID, ok := mapOrganism[value]
	if !ok {
		return "", fmt.Errorf("organism must be one of: human, mouse, rat, 9606, 10090, 10116")
	}
	return taxID, nil
}
