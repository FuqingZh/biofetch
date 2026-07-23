package omnipath

import (
	"biofetch/internal/shared/cliopt"
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

type configEnzSub struct {
	dirOut                  string
	dirLogs                 string
	versionToken            string
	organisms               []string
	shouldDownloadAll       bool
	ruleLicense             string
	ruleExisting            string
	shouldOverwriteExisting bool
	retryMax                int
	retryWait               time.Duration
	shouldAllowInsecureTLS  bool
	shouldDryRun            bool
}

type configInteractions struct {
	dirOut                  string
	dirLogs                 string
	versionToken            string
	organisms               []string
	shouldDownloadAll       bool
	dataset                 string
	ruleLicense             string
	ruleExisting            string
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
		Args:          cobra.NoArgs,
		RunE: func(command *cobra.Command, args []string) error {
			if len(args) > 0 {
				return fmt.Errorf("unknown command %q for %q", args[0], command.CommandPath())
			}
			return command.Help()
		},
	}
	commandRoot.AddCommand(createEnzSubCommand())
	commandRoot.AddCommand(createInteractionsCommand())
	return commandRoot
}

func createEnzSubCommand() *cobra.Command {
	command := &cobra.Command{
		Use:           "enzyme-substrate",
		Short:         "Manage OmniPath enzyme-PTM relationships",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	command.AddCommand(createEnzSubFetchCommand())
	command.AddCommand(createEnzSubLockCommand())
	command.AddCommand(createEnzSubRestoreCommand())
	return command
}

func createEnzSubFetchCommand() *cobra.Command {
	cfg := configEnzSub{}
	cfg.retryMax = 5
	cfg.retryWait = 3 * time.Second
	cfg.ruleExisting = "skip"

	command := &cobra.Command{
		Use:           "fetch",
		Short:         "Fetch OmniPath enzyme-PTM relationships and update manifest.lock",
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := validateEnzSubConfig(&cfg); err != nil {
				return err
			}
			return runFetchEnzSub(&cfg)
		},
	}

	flags := command.Flags()
	flags.SortFlags = false
	flags.StringVarP(&cfg.dirOut, "output", "o", cfg.dirOut, "OmniPath asset root directory")
	flags.StringVar(&cfg.versionToken, "version", cfg.versionToken, "OmniPath archive version date in YYYY-MM-DD; omit to fetch the latest data")
	flags.StringSliceVar(&cfg.organisms, "organisms", nil, "Organism taxids; pass inline values, repeat the flag,")
	cliopt.BindListFileFlag(flags, &cfg.organisms, "organisms")
	flags.BoolVar(&cfg.shouldDownloadAll, "all-organisms", false, "Fetch all supported organisms")
	flags.StringVar(&cfg.ruleLicense, "license", "", "License mode: academic|commercial")
	flags.StringVar(&cfg.ruleExisting, "on-existing", cfg.ruleExisting, "Rule for existing files: skip|overwrite")
	flags.IntVar(&cfg.retryMax, "max-attempts", cfg.retryMax, "Max retry attempts on download failures")
	flags.DurationVar(&cfg.retryWait, "retry-wait", cfg.retryWait, "Wait between download attempts")
	flags.BoolVar(&cfg.shouldAllowInsecureTLS, "insecure", false, "Disable TLS certificate verification")
	flags.BoolVar(&cfg.shouldDryRun, "dry-run", false, "Print actions only; do not download")
	flags.StringVar(&cfg.dirLogs, "log-dir", cfg.dirLogs, "Directory for run log files; default is <version>/logs/")

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
	command.AddCommand(createInteractionsRestoreCommand())
	return command
}

func createInteractionsFetchCommand() *cobra.Command {
	cfg := configInteractions{}
	cfg.dataset = "kinaseextra"
	cfg.retryMax = 5
	cfg.retryWait = 3 * time.Second
	cfg.ruleExisting = "skip"

	command := &cobra.Command{
		Use:           "fetch",
		Short:         "Fetch OmniPath interactions datasets and update manifest.lock",
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := validateInteractionsConfig(&cfg); err != nil {
				return err
			}
			return runFetchInteractions(&cfg)
		},
	}

	flags := command.Flags()
	flags.SortFlags = false
	flags.StringVarP(&cfg.dirOut, "output", "o", cfg.dirOut, "OmniPath asset root directory")
	flags.StringVar(&cfg.versionToken, "version", cfg.versionToken, "OmniPath archive version date in YYYY-MM-DD; omit to fetch the latest data")
	flags.StringSliceVar(&cfg.organisms, "organisms", nil, "Organism taxids; pass inline values, repeat the flag,")
	cliopt.BindListFileFlag(flags, &cfg.organisms, "organisms")
	flags.BoolVar(&cfg.shouldDownloadAll, "all-organisms", false, "Fetch all supported organisms")
	flags.StringVar(&cfg.dataset, "dataset", cfg.dataset, "Interactions dataset (v1 supports only kinaseextra)")
	flags.StringVar(&cfg.ruleLicense, "license", "", "License mode: academic|commercial")
	flags.StringVar(&cfg.ruleExisting, "on-existing", cfg.ruleExisting, "Rule for existing files: skip|overwrite")
	flags.IntVar(&cfg.retryMax, "max-attempts", cfg.retryMax, "Max retry attempts on download failures")
	flags.DurationVar(&cfg.retryWait, "retry-wait", cfg.retryWait, "Wait between download attempts")
	flags.BoolVar(&cfg.shouldAllowInsecureTLS, "insecure", false, "Disable TLS certificate verification")
	flags.BoolVar(&cfg.shouldDryRun, "dry-run", false, "Print actions only; do not download")
	flags.StringVar(&cfg.dirLogs, "log-dir", cfg.dirLogs, "Directory for run log files; default is <version>/logs/")

	return command
}

func createEnzSubLockCommand() *cobra.Command {
	cfg := lockConfig{}

	command := &cobra.Command{
		Use:           "lock SNAPSHOT",
		Short:         "Rebuild OmniPath enz_sub manifest.lock from the current version directory",
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg.dirSnapshot = args[0]
			return runLockEnzSub(&cfg)
		},
	}

	flags := command.Flags()
	flags.SortFlags = false
	cliopt.BindLockWorkersFlag(flags, &cfg.workersMax)
	flags.BoolVar(&cfg.shouldDryRun, "dry-run", false, "Print actions only; do not write manifest")
	flags.StringVar(&cfg.dirLogs, "log-dir", cfg.dirLogs, "Directory for run log files; default is <version>/logs/")
	return command
}

func createEnzSubRestoreCommand() *cobra.Command {
	cfg := restoreConfig{}
	cfg.retryMax = 5
	cfg.retryWait = 3 * time.Second
	cfg.ruleExisting = "skip"

	command := &cobra.Command{
		Use:           "restore SNAPSHOT",
		Short:         "Restore OmniPath enz_sub files from manifest.lock and refresh manifest",
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			var err error
			cfg.dirOut, cfg.versionToken, err = cliopt.FlatSnapshotRootVersion(args[0], "enz_sub")
			if err != nil {
				return err
			}
			if strings.TrimSpace(cfg.dirOut) == "" {
				return fmt.Errorf("output is required")
			}
			if strings.TrimSpace(cfg.versionToken) == "" {
				return fmt.Errorf("version is required")
			}
			if cfg.ruleExisting != "skip" && cfg.ruleExisting != "overwrite" {
				return fmt.Errorf("on-existing must be one of: skip, overwrite")
			}
			cfg.shouldOverwriteExisting = cfg.ruleExisting == "overwrite"
			return runRestoreEnzSub(&cfg)
		},
	}

	flags := command.Flags()
	flags.SortFlags = false
	flags.StringVar(&cfg.ruleExisting, "on-existing", cfg.ruleExisting, "Rule for existing files: skip|overwrite")
	flags.IntVar(&cfg.retryMax, "max-attempts", cfg.retryMax, "Max retry attempts on download failures")
	flags.DurationVar(&cfg.retryWait, "retry-wait", cfg.retryWait, "Wait between download attempts")
	flags.BoolVar(&cfg.shouldAllowInsecureTLS, "insecure", false, "Disable TLS certificate verification")
	flags.BoolVar(&cfg.shouldDryRun, "dry-run", false, "Print actions only; do not download")
	flags.StringVar(&cfg.dirLogs, "log-dir", cfg.dirLogs, "Directory for run log files; default is <version>/logs/")
	return command
}

func createInteractionsLockCommand() *cobra.Command {
	cfg := lockConfig{}

	command := &cobra.Command{
		Use:           "lock SNAPSHOT",
		Short:         "Rebuild OmniPath interactions manifest.lock from the current version directory",
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg.dirSnapshot = args[0]
			return runLockInteractions(&cfg)
		},
	}

	flags := command.Flags()
	flags.SortFlags = false
	cliopt.BindLockWorkersFlag(flags, &cfg.workersMax)
	flags.BoolVar(&cfg.shouldDryRun, "dry-run", false, "Print actions only; do not write manifest")
	flags.StringVar(&cfg.dirLogs, "log-dir", cfg.dirLogs, "Directory for run log files; default is <version>/logs/")
	return command
}

func createInteractionsRestoreCommand() *cobra.Command {
	cfg := restoreConfig{}
	cfg.dataset = "kinaseextra"
	cfg.retryMax = 5
	cfg.retryWait = 3 * time.Second
	cfg.ruleExisting = "skip"

	command := &cobra.Command{
		Use:           "restore SNAPSHOT",
		Short:         "Restore OmniPath interactions files from manifest.lock and refresh manifest",
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			var err error
			cfg.dirOut, cfg.dataset, cfg.versionToken, err = cliopt.NestedSnapshotRootDatasetVersion(args[0], "interactions")
			if err != nil {
				return err
			}
			if strings.TrimSpace(cfg.dirOut) == "" {
				return fmt.Errorf("output is required")
			}
			if strings.TrimSpace(cfg.versionToken) == "" {
				return fmt.Errorf("version is required")
			}
			if cfg.ruleExisting != "skip" && cfg.ruleExisting != "overwrite" {
				return fmt.Errorf("on-existing must be one of: skip, overwrite")
			}
			cfg.shouldOverwriteExisting = cfg.ruleExisting == "overwrite"
			if strings.TrimSpace(strings.ToLower(cfg.dataset)) != "kinaseextra" {
				return fmt.Errorf("dataset must be kinaseextra in v1")
			}
			return runRestoreInteractions(&cfg)
		},
	}

	flags := command.Flags()
	flags.SortFlags = false
	flags.StringVar(&cfg.dataset, "dataset", cfg.dataset, "Interactions dataset (v1 supports only kinaseextra)")
	flags.StringVar(&cfg.ruleExisting, "on-existing", cfg.ruleExisting, "Rule for existing files: skip|overwrite")
	flags.IntVar(&cfg.retryMax, "max-attempts", cfg.retryMax, "Max retry attempts on download failures")
	flags.DurationVar(&cfg.retryWait, "retry-wait", cfg.retryWait, "Wait between download attempts")
	flags.BoolVar(&cfg.shouldAllowInsecureTLS, "insecure", false, "Disable TLS certificate verification")
	flags.BoolVar(&cfg.shouldDryRun, "dry-run", false, "Print actions only; do not download")
	flags.StringVar(&cfg.dirLogs, "log-dir", cfg.dirLogs, "Directory for run log files; default is <version>/logs/")
	return command
}

func validateEnzSubConfig(cfg *configEnzSub) error {
	if strings.TrimSpace(cfg.dirOut) == "" {
		return fmt.Errorf("output is required")
	}
	if cfg.ruleExisting != "skip" && cfg.ruleExisting != "overwrite" {
		return fmt.Errorf("on-existing must be one of: skip, overwrite")
	}
	if err := validateOptionalVersionToken(cfg.versionToken); err != nil {
		return err
	}
	cfg.shouldOverwriteExisting = cfg.ruleExisting == "overwrite"
	countSources := 0
	if len(cfg.organisms) > 0 {
		countSources++
	}
	if cfg.shouldDownloadAll {
		countSources++
	}
	if countSources != 1 {
		return fmt.Errorf("choose exactly one source: --organisms | --all-organisms")
	}
	if len(cfg.organisms) > 0 {
		organisms, err := parseOrganisms(cfg.organisms)
		if err != nil {
			return err
		}
		cfg.organisms = organisms
	}
	if err := validateRuleLicense(cfg.ruleLicense); err != nil {
		return err
	}
	if cfg.retryMax < 1 {
		return fmt.Errorf("max-attempts must be >= 1")
	}
	if cfg.retryWait < 0 {
		return fmt.Errorf("retry-wait must be >= 0")
	}
	return nil
}

func validateInteractionsConfig(cfg *configInteractions) error {
	if strings.TrimSpace(cfg.dirOut) == "" {
		return fmt.Errorf("output is required")
	}
	if cfg.ruleExisting != "skip" && cfg.ruleExisting != "overwrite" {
		return fmt.Errorf("on-existing must be one of: skip, overwrite")
	}
	if err := validateOptionalVersionToken(cfg.versionToken); err != nil {
		return err
	}
	cfg.shouldOverwriteExisting = cfg.ruleExisting == "overwrite"
	countSources := 0
	if len(cfg.organisms) > 0 {
		countSources++
	}
	if cfg.shouldDownloadAll {
		countSources++
	}
	if countSources != 1 {
		return fmt.Errorf("choose exactly one source: --organisms | --all-organisms")
	}
	if len(cfg.organisms) > 0 {
		organisms, err := parseOrganisms(cfg.organisms)
		if err != nil {
			return err
		}
		cfg.organisms = organisms
	}
	if err := validateRuleLicense(cfg.ruleLicense); err != nil {
		return err
	}
	if strings.TrimSpace(strings.ToLower(cfg.dataset)) != "kinaseextra" {
		return fmt.Errorf("dataset must be kinaseextra in v1")
	}
	if cfg.retryMax < 1 {
		return fmt.Errorf("max-attempts must be >= 1")
	}
	if cfg.retryWait < 0 {
		return fmt.Errorf("retry-wait must be >= 0")
	}
	return nil
}

func parseOrganisms(valuesInput []string) ([]string, error) {
	valuesResolved, err := cliopt.ExpandListTokens(valuesInput, "", "organisms")
	if err != nil {
		return nil, err
	}
	valuesValid := make([]string, 0, len(valuesResolved))
	for _, value := range valuesResolved {
		taxID, err := normalizeOrganism(value)
		if err != nil {
			return nil, fmt.Errorf("invalid organism: %w", err)
		}
		if taxID != "" {
			valuesValid = append(valuesValid, taxID)
		}
	}
	if len(valuesValid) == 0 {
		return nil, fmt.Errorf("organisms must not be empty")
	}
	return cliopt.SortedUniqueStrings(valuesValid), nil
}

func readOrganismsFromFile(filePath string) ([]string, error) {
	return parseOrganisms([]string{"@" + filePath})
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
