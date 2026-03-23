package kegg

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

type pathwayConfig struct {
	dirOut                  string
	version                 string
	versionToken            string
	sourceRelease           string
	assetNames              []string
	organismCode            string
	organismCodes           []string
	fileOrganismCodes       string
	pathwayIDsCSV           string
	filePathwayIDs          string
	shouldFetchReference    bool
	shouldDownloadAll       bool
	ruleExisting            string
	shouldOverwriteExisting bool
	retryMax                int
	retryWait               time.Duration
	requestInterval         time.Duration
	shouldAllowInsecureTLS  bool
	shouldDryRun            bool
	scopeType               string
	scopeValue              string
}

type briteConfig struct {
	dirOut                  string
	version                 string
	versionToken            string
	sourceRelease           string
	catalogCode             string
	organismCodes           []string
	fileOrganismCodes       string
	shouldDownloadAll       bool
	briteIDsCSV             string
	fileBriteIDs            string
	shouldDownloadRootOnly  bool
	ruleExisting            string
	shouldOverwriteExisting bool
	retryMax                int
	retryWait               time.Duration
	requestInterval         time.Duration
	shouldAllowInsecureTLS  bool
	shouldDryRun            bool
	scopeType               string
	scopeValue              string
}

func RunCLI(args []string) error {
	commandRoot := NewCommand()
	commandRoot.SetArgs(args)
	return commandRoot.Execute()
}

func NewCommand() *cobra.Command {
	commandRoot := &cobra.Command{
		Use:           "kegg",
		Short:         "Manage KEGG raw assets and manifest.lock",
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	commandRoot.AddCommand(createPathwayCommand())
	commandRoot.AddCommand(createBriteCommand())
	return commandRoot
}

func createPathwayCommand() *cobra.Command {
	commandPathway := &cobra.Command{
		Use:           "pathway",
		Short:         "Manage KEGG PATHWAY raw assets",
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	commandPathway.AddCommand(createPathwayFetchCommand())
	commandPathway.AddCommand(createPathwayLockCommand())
	commandPathway.AddCommand(createPathwaySyncCommand())
	return commandPathway
}

func createPathwayFetchCommand() *cobra.Command {
	cfg := createDefaultPathwayConfig()
	retryWaitSec := 3
	requestIntervalMs := 350

	commandPathway := &cobra.Command{
		Use:           "fetch",
		Short:         "Fetch KEGG PATHWAY raw assets and update manifest.lock",
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg.retryWait = time.Duration(retryWaitSec) * time.Second
			cfg.requestInterval = time.Duration(requestIntervalMs) * time.Millisecond
			if err := validatePathwayConfig(&cfg); err != nil {
				return err
			}
			if cfg.shouldDownloadAll {
				if err := confirmAllOrganismsDownload(cmd.InOrStdin(), cmd.ErrOrStderr()); err != nil {
					return err
				}
			}
			return runFetchPathway(&cfg)
		},
	}

	commandPathway.Example = strings.Join([]string{
		"biofetch kegg pathway fetch --dir_out /data/kegg --organisms hsa --should_dry_run",
		"biofetch kegg pathway fetch --dir_out /data/kegg --organisms hsa --organisms tca",
		"biofetch kegg pathway fetch --dir_out /data/kegg --should_fetch_reference --pathway_ids map00010,map00020",
		"biofetch kegg pathway fetch --dir_out /data/kegg --organisms hsa --assets entry --assets kgml --assets image",
	}, "\n")

	flags := commandPathway.Flags()
	flags.SortFlags = false
	flags.StringVar(&cfg.dirOut, "dir_out", cfg.dirOut, "KEGG asset root directory")
	flags.StringVar(&cfg.versionToken, "version", cfg.versionToken, "KEGG major version, e.g. 117.0")
	flags.StringSliceVar(&cfg.assetNames, "assets", nil, "PATHWAY assets: list|entry|kgml|conf|image; repeat the flag or use commas")
	flags.StringSliceVar(&cfg.organismCodes, "organisms", nil, "KEGG organism codes; repeat the flag or use commas")
	flags.StringVar(&cfg.fileOrganismCodes, "file_organisms", "", "File with one KEGG organism code per line")
	flags.StringVar(&cfg.pathwayIDsCSV, "pathway_ids", "", "Comma-separated pathway IDs")
	flags.StringVar(&cfg.filePathwayIDs, "file_pathway_ids", "", "File with one pathway ID per line")
	flags.BoolVar(
		&cfg.shouldFetchReference,
		"should_fetch_reference",
		false,
		"Fetch reference pathways from /list/pathway",
	)
	flags.BoolVar(
		&cfg.shouldDownloadAll,
		"should_download_all",
		false,
		"Fetch PATHWAY assets for all KEGG organisms",
	)
	flags.StringVar(&cfg.ruleExisting, "rule_existing", cfg.ruleExisting, "Rule for existing files: skip|overwrite")
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

func createPathwayLockCommand() *cobra.Command {
	cfg := keggLockConfig{}
	requestIntervalMs := 350

	commandLock := &cobra.Command{
		Use:           "lock",
		Short:         "Rebuild KEGG PATHWAY manifest.lock from the current version directory",
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg.requestInterval = time.Duration(requestIntervalMs) * time.Millisecond
			if strings.TrimSpace(cfg.dirOut) == "" {
				return fmt.Errorf("dir_out is required")
			}
			if strings.TrimSpace(cfg.versionToken) == "" {
				return fmt.Errorf("version is required")
			}
			return runLockPathway(&cfg)
		},
	}

	flags := commandLock.Flags()
	flags.SortFlags = false
	flags.StringVar(&cfg.dirOut, "dir_out", "", "KEGG asset root directory")
	flags.StringVar(&cfg.versionToken, "version", "", "KEGG version token")
	flags.IntVar(&requestIntervalMs, "request_interval_ms", requestIntervalMs, "Delay between KEGG API requests in milliseconds")
	flags.BoolVar(&cfg.shouldDryRun, "should_dry_run", false, "Print actions only; do not write manifest")
	return commandLock
}

func createPathwaySyncCommand() *cobra.Command {
	cfg := keggSyncConfig{}
	cfg.ruleExisting = "skip"
	requestIntervalMs := 350

	commandSync := &cobra.Command{
		Use:           "sync",
		Short:         "Sync KEGG PATHWAY files from manifest.lock and refresh manifest",
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg.requestInterval = time.Duration(requestIntervalMs) * time.Millisecond
			if strings.TrimSpace(cfg.dirOut) == "" {
				return fmt.Errorf("dir_out is required")
			}
			if strings.TrimSpace(cfg.versionToken) == "" {
				return fmt.Errorf("version is required")
			}
			if cfg.ruleExisting != "skip" && cfg.ruleExisting != "overwrite" {
				return fmt.Errorf("rule_existing must be one of: skip, overwrite")
			}
			cfg.shouldOverwriteExisting = cfg.ruleExisting == "overwrite"
			return runSyncPathway(&cfg)
		},
	}

	flags := commandSync.Flags()
	flags.SortFlags = false
	flags.StringVar(&cfg.dirOut, "dir_out", "", "KEGG asset root directory")
	flags.StringVar(&cfg.versionToken, "version", "", "KEGG version token")
	flags.StringVar(&cfg.ruleExisting, "rule_existing", cfg.ruleExisting, "Rule for existing files: skip|overwrite")
	flags.IntVar(&requestIntervalMs, "request_interval_ms", requestIntervalMs, "Delay between KEGG API requests in milliseconds")
	flags.BoolVar(&cfg.shouldAllowInsecureTLS, "should_allow_insecure_tls", false, "Disable TLS certificate verification")
	flags.BoolVar(&cfg.shouldDryRun, "should_dry_run", false, "Print actions only; do not download")
	return commandSync
}

func createDefaultPathwayConfig() pathwayConfig {
	cfg := pathwayConfig{}
	cfg.retryMax = defaultKEGGRetryMax
	cfg.retryWait = defaultKEGGRetryWait
	cfg.requestInterval = 350 * time.Millisecond
	cfg.ruleExisting = "skip"
	cfg.assetNames = []string{"list", "entry", "kgml"}
	return cfg
}

func validatePathwayConfig(cfg *pathwayConfig) error {
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
	if strings.TrimSpace(cfg.versionToken) != "" && !isValidKEGGMajorVersion(cfg.versionToken) {
		return fmt.Errorf("version must be a KEGG major version like 117.0")
	}
	if cfg.ruleExisting != "skip" && cfg.ruleExisting != "overwrite" {
		return fmt.Errorf("rule_existing must be one of: skip, overwrite")
	}
	cfg.shouldOverwriteExisting = cfg.ruleExisting == "overwrite"
	assetNames, err := parsePathwayAssetNames(cfg.assetNames)
	if err != nil {
		return err
	}
	cfg.assetNames = assetNames

	countScope := 0
	if len(cfg.organismCodes) > 0 {
		countScope++
	}
	if strings.TrimSpace(cfg.fileOrganismCodes) != "" {
		countScope++
	}
	if cfg.shouldFetchReference {
		countScope++
	}
	if cfg.shouldDownloadAll {
		countScope++
	}
	if countScope != 1 {
		return fmt.Errorf(
			"choose exactly one scope: --organisms | --file_organisms | --should_download_all | --should_fetch_reference",
		)
	}
	if cfg.fileOrganismCodes != "" {
		if _, err := os.Stat(cfg.fileOrganismCodes); err != nil {
			return fmt.Errorf("organisms file not found: %w", err)
		}
	}
	if cfg.shouldDownloadAll {
		if strings.TrimSpace(cfg.pathwayIDsCSV) != "" || strings.TrimSpace(cfg.filePathwayIDs) != "" {
			return fmt.Errorf("pathway_ids and file_pathway_ids are not allowed with multi-organism download")
		}
	}
	if strings.TrimSpace(cfg.fileOrganismCodes) != "" {
		if _, err := readKEGGOrganismCodesFromFile(cfg.fileOrganismCodes); err != nil {
			return err
		}
	}
	if len(cfg.organismCodes) > 0 {
		if _, err := parseKEGGOrganismCodes(cfg.organismCodes); err != nil {
			return err
		}
	}
	if cfg.filePathwayIDs != "" {
		if _, err := os.Stat(cfg.filePathwayIDs); err != nil {
			return fmt.Errorf("pathway ids file not found: %w", err)
		}
	}

	return nil
}

func parsePathwayAssetNames(valuesInput []string) ([]string, error) {
	setAssets := make(map[string]struct{})
	for _, valueInput := range valuesInput {
		for _, token := range strings.Split(valueInput, ",") {
			assetName := strings.ToLower(strings.TrimSpace(token))
			if assetName == "" {
				continue
			}
			switch assetName {
			case "list", "entry", "kgml", "conf", "image":
				setAssets[assetName] = struct{}{}
			default:
				return nil, fmt.Errorf("invalid PATHWAY asset: %s", token)
			}
		}
	}
	if len(setAssets) == 0 {
		return nil, fmt.Errorf("assets must not be empty")
	}
	return sets.SortedKeys(setAssets), nil
}

func createBriteCommand() *cobra.Command {
	commandBrite := &cobra.Command{
		Use:           "brite",
		Short:         "Manage KEGG BRITE raw assets",
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	commandBrite.AddCommand(createBriteFetchCommand())
	commandBrite.AddCommand(createBriteLockCommand())
	commandBrite.AddCommand(createBriteSyncCommand())
	return commandBrite
}

func createBriteFetchCommand() *cobra.Command {
	cfg := createDefaultBriteConfig()
	retryWaitSec := 3
	requestIntervalMs := 350

	commandBrite := &cobra.Command{
		Use:           "fetch",
		Short:         "Fetch KEGG BRITE raw assets and update manifest.lock",
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg.retryWait = time.Duration(retryWaitSec) * time.Second
			cfg.requestInterval = time.Duration(requestIntervalMs) * time.Millisecond
			if err := validateBriteConfig(&cfg); err != nil {
				return err
			}
			if cfg.shouldDownloadAll {
				if err := confirmAllOrganismsDownload(cmd.InOrStdin(), cmd.ErrOrStderr()); err != nil {
					return err
				}
			}
			return runFetchBrite(&cfg)
		},
	}

	commandBrite.Example = strings.Join([]string{
		"biofetch kegg brite fetch --dir_out /data/kegg --catalog br --should_dry_run",
		"biofetch kegg brite fetch --dir_out /data/kegg --organisms hsa --brite_ids hsa00001",
		"biofetch kegg brite fetch --dir_out /data/kegg --organisms hsa --organisms tca",
		"biofetch kegg brite fetch --dir_out /data/kegg --should_download_all",
	}, "\n")

	flags := commandBrite.Flags()
	flags.SortFlags = false
	flags.StringVar(&cfg.dirOut, "dir_out", cfg.dirOut, "KEGG asset root directory")
	flags.StringVar(&cfg.versionToken, "version", cfg.versionToken, "KEGG major version, e.g. 117.0")
	flags.StringVar(&cfg.catalogCode, "catalog", "", "Reference BRITE catalog; use br or ko")
	flags.StringSliceVar(&cfg.organismCodes, "organisms", nil, "KEGG organism codes; repeat the flag or use commas")
	flags.StringVar(&cfg.fileOrganismCodes, "file_organisms", "", "File with one KEGG organism code per line")
	flags.BoolVar(
		&cfg.shouldDownloadAll,
		"should_download_all",
		false,
		"Fetch BRITE assets for all KEGG organisms",
	)
	flags.StringVar(&cfg.briteIDsCSV, "brite_ids", "", "Comma-separated BRITE IDs, e.g. br08301,hsa00001")
	flags.StringVar(&cfg.fileBriteIDs, "file_brite_ids", "", "File with one BRITE ID per line")
	flags.BoolVar(&cfg.shouldDownloadRootOnly, "should_download_root_only", false, "Download only root BRITE hierarchy (*00001) per catalog")
	flags.StringVar(&cfg.ruleExisting, "rule_existing", cfg.ruleExisting, "Rule for existing files: skip|overwrite")
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

func createBriteLockCommand() *cobra.Command {
	cfg := keggLockConfig{}
	requestIntervalMs := 350

	commandLock := &cobra.Command{
		Use:           "lock",
		Short:         "Rebuild KEGG BRITE manifest.lock from the current version directory",
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg.requestInterval = time.Duration(requestIntervalMs) * time.Millisecond
			if strings.TrimSpace(cfg.dirOut) == "" {
				return fmt.Errorf("dir_out is required")
			}
			if strings.TrimSpace(cfg.versionToken) == "" {
				return fmt.Errorf("version is required")
			}
			return runLockBrite(&cfg)
		},
	}

	flags := commandLock.Flags()
	flags.SortFlags = false
	flags.StringVar(&cfg.dirOut, "dir_out", "", "KEGG asset root directory")
	flags.StringVar(&cfg.versionToken, "version", "", "KEGG version token")
	flags.IntVar(&requestIntervalMs, "request_interval_ms", requestIntervalMs, "Delay between KEGG API requests in milliseconds")
	flags.BoolVar(&cfg.shouldDryRun, "should_dry_run", false, "Print actions only; do not write manifest")
	return commandLock
}

func createBriteSyncCommand() *cobra.Command {
	cfg := keggSyncConfig{}
	cfg.ruleExisting = "skip"
	requestIntervalMs := 350

	commandSync := &cobra.Command{
		Use:           "sync",
		Short:         "Sync KEGG BRITE files from manifest.lock and refresh manifest",
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg.requestInterval = time.Duration(requestIntervalMs) * time.Millisecond
			if strings.TrimSpace(cfg.dirOut) == "" {
				return fmt.Errorf("dir_out is required")
			}
			if strings.TrimSpace(cfg.versionToken) == "" {
				return fmt.Errorf("version is required")
			}
			if cfg.ruleExisting != "skip" && cfg.ruleExisting != "overwrite" {
				return fmt.Errorf("rule_existing must be one of: skip, overwrite")
			}
			cfg.shouldOverwriteExisting = cfg.ruleExisting == "overwrite"
			return runSyncBrite(&cfg)
		},
	}

	flags := commandSync.Flags()
	flags.SortFlags = false
	flags.StringVar(&cfg.dirOut, "dir_out", "", "KEGG asset root directory")
	flags.StringVar(&cfg.versionToken, "version", "", "KEGG version token")
	flags.StringVar(&cfg.ruleExisting, "rule_existing", cfg.ruleExisting, "Rule for existing files: skip|overwrite")
	flags.IntVar(&requestIntervalMs, "request_interval_ms", requestIntervalMs, "Delay between KEGG API requests in milliseconds")
	flags.BoolVar(&cfg.shouldAllowInsecureTLS, "should_allow_insecure_tls", false, "Disable TLS certificate verification")
	flags.BoolVar(&cfg.shouldDryRun, "should_dry_run", false, "Print actions only; do not download")
	return commandSync
}

func createDefaultBriteConfig() briteConfig {
	cfg := briteConfig{}
	cfg.retryMax = defaultKEGGRetryMax
	cfg.retryWait = defaultKEGGRetryWait
	cfg.requestInterval = 350 * time.Millisecond
	cfg.ruleExisting = "skip"
	return cfg
}

func validateBriteConfig(cfg *briteConfig) error {
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
	if strings.TrimSpace(cfg.versionToken) != "" && !isValidKEGGMajorVersion(cfg.versionToken) {
		return fmt.Errorf("version must be a KEGG major version like 117.0")
	}
	if cfg.ruleExisting != "skip" && cfg.ruleExisting != "overwrite" {
		return fmt.Errorf("rule_existing must be one of: skip, overwrite")
	}
	cfg.shouldOverwriteExisting = cfg.ruleExisting == "overwrite"

	if cfg.shouldDownloadAll {
		if strings.TrimSpace(cfg.catalogCode) != "" {
			return fmt.Errorf("catalog must not be set with --should_download_all")
		}
		if len(cfg.organismCodes) > 0 || strings.TrimSpace(cfg.fileOrganismCodes) != "" {
			return fmt.Errorf("organisms and file_organisms must not be set with --should_download_all")
		}
		if strings.TrimSpace(cfg.briteIDsCSV) != "" || strings.TrimSpace(cfg.fileBriteIDs) != "" {
			return fmt.Errorf("brite_ids and file_brite_ids are not allowed with --should_download_all")
		}
	} else {
		countSources := 0
		if strings.TrimSpace(cfg.catalogCode) != "" {
			countSources++
		}
		if len(cfg.organismCodes) > 0 {
			countSources++
		}
		if strings.TrimSpace(cfg.fileOrganismCodes) != "" {
			countSources++
		}
		if countSources != 1 {
			return fmt.Errorf("choose exactly one source: --catalog | --organisms | --file_organisms | --should_download_all")
		}
	}

	if cfg.shouldDownloadRootOnly {
		if strings.TrimSpace(cfg.briteIDsCSV) != "" || strings.TrimSpace(cfg.fileBriteIDs) != "" {
			return fmt.Errorf("brite_ids and file_brite_ids are not allowed with --should_download_root_only")
		}
	}
	if cfg.fileBriteIDs != "" {
		if _, err := os.Stat(cfg.fileBriteIDs); err != nil {
			return fmt.Errorf("brite ids file not found: %w", err)
		}
	}
	if cfg.fileOrganismCodes != "" {
		if _, err := os.Stat(cfg.fileOrganismCodes); err != nil {
			return fmt.Errorf("organisms file not found: %w", err)
		}
	}
	if strings.TrimSpace(cfg.fileOrganismCodes) != "" {
		if _, err := readKEGGOrganismCodesFromFile(cfg.fileOrganismCodes); err != nil {
			return err
		}
	}
	if len(cfg.organismCodes) > 0 {
		if _, err := parseKEGGOrganismCodes(cfg.organismCodes); err != nil {
			return err
		}
	}
	if strings.TrimSpace(cfg.catalogCode) != "" && cfg.catalogCode != "br" && cfg.catalogCode != "ko" {
		return fmt.Errorf("invalid catalog: %s", cfg.catalogCode)
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

func parseKEGGOrganismCodes(valuesInput []string) ([]string, error) {
	setCodes := make(map[string]struct{})
	for _, valueInput := range valuesInput {
		for _, token := range strings.Split(valueInput, ",") {
			code := normalizeKEGGOrganismCode(token)
			if code == "" {
				continue
			}
			if !isValidKEGGOrganismCode(code) {
				return nil, fmt.Errorf("invalid KEGG organism code: %s", token)
			}
			setCodes[code] = struct{}{}
		}
	}
	if len(setCodes) == 0 {
		return nil, fmt.Errorf("organisms must not be empty")
	}
	return sets.SortedKeys(setCodes), nil
}

func readKEGGOrganismCodesFromFile(filePath string) ([]string, error) {
	fileIn, err := os.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("open organisms file: %w", err)
	}
	defer fileIn.Close()

	setCodes := make(map[string]struct{})
	scanner := bufio.NewScanner(fileIn)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		code := normalizeKEGGOrganismCode(line)
		if !isValidKEGGOrganismCode(code) {
			return nil, fmt.Errorf("invalid KEGG organism code in %s: %s", filePath, line)
		}
		setCodes[code] = struct{}{}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read organisms file: %w", err)
	}
	if len(setCodes) == 0 {
		return nil, fmt.Errorf("organisms file must not be empty: %s", filePath)
	}
	return sets.SortedKeys(setCodes), nil
}

func normalizeKEGGOrganismCode(text string) string {
	return strings.ToLower(strings.TrimSpace(text))
}

func isValidKEGGOrganismCode(text string) bool {
	text = normalizeKEGGOrganismCode(text)
	if text == "" {
		return false
	}
	for _, char := range text {
		if (char < 'a' || char > 'z') && (char < '0' || char > '9') {
			return false
		}
	}
	return true
}

func isValidKEGGMajorVersion(value string) bool {
	text := strings.TrimSpace(value)
	if text == "" {
		return false
	}
	parts := strings.Split(text, ".")
	if len(parts) != 2 {
		return false
	}
	if parts[0] == "" || parts[1] == "" {
		return false
	}
	for _, ch := range parts[0] {
		if ch < '0' || ch > '9' {
			return false
		}
	}
	for _, ch := range parts[1] {
		if ch < '0' || ch > '9' {
			return false
		}
	}
	return true
}
