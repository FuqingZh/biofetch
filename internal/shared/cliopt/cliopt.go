package cliopt

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/spf13/pflag"
)

const (
	DefaultLockWorkersMax = 4
	LockWorkersMaxLimit   = 64
)

type DirOutConfig struct {
	DirOut string
}

type DirSnapshotConfig struct {
	DirSnapshot string
}

type VersionConfig struct {
	VersionToken string
}

type DryRunConfig struct {
	ShouldDryRun bool
}

type LogConfig struct {
	DirLogs string
}

type ProgressConfig struct {
	ShouldDisableProgress bool
}

type ExistingRuleConfig struct {
	RuleExisting            string
	ShouldOverwriteExisting bool
}

type RetryConfig struct {
	RetryMax  int
	RetryWait time.Duration
}

type DownloadControlConfig struct {
	WorkersMax      int
	RequestInterval time.Duration
}

type InsecureTLSConfig struct {
	ShouldAllowInsecureTLS bool
}

func BindDirOutFlag(flags *pflag.FlagSet, cfg *DirOutConfig, usage string) {
	flags.StringVar(&cfg.DirOut, "dir_out", cfg.DirOut, usage)
}

func BindDirSnapshotFlag(flags *pflag.FlagSet, cfg *DirSnapshotConfig) {
	flags.StringVar(&cfg.DirSnapshot, "dir_snapshot", cfg.DirSnapshot, "Existing snapshot directory containing raw/ and manifest.lock")
}

func BindLockWorkersFlag(flags *pflag.FlagSet, workersMax *int) {
	if *workersMax == 0 {
		*workersMax = DefaultLockWorkersMax
	}
	flags.IntVar(workersMax, "workers_max", *workersMax, "Max concurrent workers for hashing snapshot files (1-64)")
}

func ValidateLockWorkersMax(workersMax int) error {
	if workersMax < 1 {
		return fmt.Errorf("workers_max must be >= 1")
	}
	if workersMax > LockWorkersMaxLimit {
		return fmt.Errorf("workers_max must be <= %d", LockWorkersMaxLimit)
	}
	return nil
}

func NormalizeLockWorkersMax(workersMax *int) error {
	if *workersMax == 0 {
		*workersMax = DefaultLockWorkersMax
	}
	return ValidateLockWorkersMax(*workersMax)
}

func BindVersionFlag(flags *pflag.FlagSet, cfg *VersionConfig, usage string) {
	flags.StringVar(&cfg.VersionToken, "version", cfg.VersionToken, usage)
}

func BindDryRunFlag(flags *pflag.FlagSet, cfg *DryRunConfig, usage string) {
	flags.BoolVar(&cfg.ShouldDryRun, "should_dry_run", cfg.ShouldDryRun, usage)
}

func BindLogDirFlag(flags *pflag.FlagSet, cfg *LogConfig, usage string) {
	flags.StringVar(&cfg.DirLogs, "dir_logs", cfg.DirLogs, usage)
}

func BindProgressFlag(flags *pflag.FlagSet, cfg *ProgressConfig, usage string) {
	flags.BoolVar(&cfg.ShouldDisableProgress, "should_disable_progress", cfg.ShouldDisableProgress, usage)
}

func BindRuleExistingFlag(flags *pflag.FlagSet, cfg *ExistingRuleConfig, usage string) {
	flags.StringVar(&cfg.RuleExisting, "rule_existing", cfg.RuleExisting, usage)
}

func BindRetryFlags(flags *pflag.FlagSet, cfg *RetryConfig, retryWaitSec *int) {
	flags.IntVar(&cfg.RetryMax, "retry_max", cfg.RetryMax, "Max retry attempts on download failures")
	flags.IntVar(retryWaitSec, "retry_wait_sec", *retryWaitSec, "Wait seconds between retries")
}

func BindDownloadControlFlags(flags *pflag.FlagSet, cfg *DownloadControlConfig, requestIntervalMs *int) {
	flags.IntVar(&cfg.WorkersMax, "workers_max", cfg.WorkersMax, "Max concurrent download workers")
	flags.IntVar(
		requestIntervalMs,
		"request_interval_ms",
		*requestIntervalMs,
		"Minimum global delay between outbound requests in milliseconds",
	)
}

func BindInsecureTLSFlag(flags *pflag.FlagSet, cfg *InsecureTLSConfig, usage string) {
	flags.BoolVar(&cfg.ShouldAllowInsecureTLS, "should_allow_insecure_tls", cfg.ShouldAllowInsecureTLS, usage)
}

func ValidateDirOutRequired(dirOut string) error {
	if strings.TrimSpace(dirOut) == "" {
		return fmt.Errorf("dir_out is required")
	}
	return nil
}

func SnapshotVersionToken(dirSnapshot string) (string, error) {
	dirSnapshot = strings.TrimSpace(dirSnapshot)
	if dirSnapshot == "" {
		return "", fmt.Errorf("dir_snapshot is required")
	}
	dirClean := filepath.Clean(dirSnapshot)
	versionToken := filepath.Base(dirClean)
	if versionToken == "." || versionToken == string(filepath.Separator) || versionToken == "" {
		return "", fmt.Errorf("dir_snapshot must identify a version directory: %s", dirSnapshot)
	}
	return versionToken, nil
}

func ValidateVersionRequired(versionToken string) error {
	if strings.TrimSpace(versionToken) == "" {
		return fmt.Errorf("version is required")
	}
	return nil
}

func ValidateRetryConfig(cfg *RetryConfig) error {
	if cfg.RetryMax < 1 {
		return fmt.Errorf("retry_max must be >= 1")
	}
	if cfg.RetryWait < 0 {
		return fmt.Errorf("retry_wait_sec must be >= 0")
	}
	return nil
}

func ValidateDownloadControlConfig(cfg *DownloadControlConfig) error {
	if cfg.WorkersMax < 1 {
		return fmt.Errorf("workers_max must be >= 1")
	}
	if cfg.RequestInterval < 0 {
		return fmt.Errorf("request_interval_ms must be >= 0")
	}
	return nil
}

func ValidateRuleExisting(cfg *ExistingRuleConfig) error {
	if cfg.RuleExisting != "skip" && cfg.RuleExisting != "overwrite" {
		return fmt.Errorf("rule_existing must be one of: skip, overwrite")
	}
	cfg.ShouldOverwriteExisting = cfg.RuleExisting == "overwrite"
	return nil
}

func ExpandAtFileTokens(valuesInput []string, optionName string) ([]string, error) {
	valuesResolved := make([]string, 0)
	for _, valueInput := range valuesInput {
		for _, tokenRaw := range strings.Split(valueInput, ",") {
			token := strings.TrimSpace(tokenRaw)
			if token == "" {
				continue
			}
			if strings.HasPrefix(token, "@") {
				filePath := strings.TrimSpace(strings.TrimPrefix(token, "@"))
				if filePath == "" {
					return nil, fmt.Errorf("%s file path must not be empty", optionName)
				}
				valuesFile, err := readListInputFile(filePath, optionName)
				if err != nil {
					return nil, err
				}
				valuesResolved = append(valuesResolved, valuesFile...)
				continue
			}
			valuesResolved = append(valuesResolved, token)
		}
	}
	return valuesResolved, nil
}

func readListInputFile(filePath string, optionName string) ([]string, error) {
	fileIn, err := os.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("open %s file: %w", optionName, err)
	}
	defer fileIn.Close()

	values := make([]string, 0)
	scanner := bufio.NewScanner(fileIn)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		values = append(values, line)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read %s file: %w", optionName, err)
	}
	if len(values) == 0 {
		return nil, fmt.Errorf("%s file must not be empty: %s", optionName, filePath)
	}
	return values, nil
}

func SortedUniqueStrings(values []string) []string {
	setValues := make(map[string]struct{}, len(values))
	for _, value := range values {
		if value == "" {
			continue
		}
		setValues[value] = struct{}{}
	}
	valuesSorted := make([]string, 0, len(setValues))
	for value := range setValues {
		valuesSorted = append(valuesSorted, value)
	}
	sort.Strings(valuesSorted)
	return valuesSorted
}
