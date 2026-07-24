package cliopt

import (
	"bufio"
	"encoding/csv"
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
	flags.StringVarP(&cfg.DirOut, "output", "o", cfg.DirOut, usage)
}

func BindLockWorkersFlag(flags *pflag.FlagSet, workersMax *int) {
	if *workersMax == 0 {
		*workersMax = DefaultLockWorkersMax
	}
	flags.IntVar(workersMax, "workers", *workersMax, "Max concurrent workers for hashing snapshot files (1-64)")
}

func ValidateLockWorkersMax(workersMax int) error {
	if workersMax < 1 {
		return fmt.Errorf("workers must be >= 1")
	}
	if workersMax > LockWorkersMaxLimit {
		return fmt.Errorf("workers must be <= %d", LockWorkersMaxLimit)
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
	flags.BoolVar(&cfg.ShouldDryRun, "dry-run", cfg.ShouldDryRun, usage)
}

func BindLogDirFlag(flags *pflag.FlagSet, cfg *LogConfig, usage string) {
	flags.StringVar(&cfg.DirLogs, "log-dir", cfg.DirLogs, usage)
}

func BindProgressFlag(flags *pflag.FlagSet, cfg *ProgressConfig, usage string) {
	flags.BoolVar(&cfg.ShouldDisableProgress, "no-progress", cfg.ShouldDisableProgress, usage)
}

func BindRuleExistingFlag(flags *pflag.FlagSet, cfg *ExistingRuleConfig, usage string) {
	flags.StringVar(&cfg.RuleExisting, "on-existing", cfg.RuleExisting, usage)
}

func BindRetryFlags(flags *pflag.FlagSet, cfg *RetryConfig) {
	flags.IntVar(&cfg.RetryMax, "max-attempts", cfg.RetryMax, "Max attempts on download failures")
	flags.DurationVar(&cfg.RetryWait, "retry-wait", cfg.RetryWait, "Wait between download attempts")
}

func BindDownloadControlFlags(flags *pflag.FlagSet, cfg *DownloadControlConfig) {
	flags.IntVar(&cfg.WorkersMax, "workers", cfg.WorkersMax, "Max concurrent download workers")
	flags.DurationVar(
		&cfg.RequestInterval,
		"request-interval",
		cfg.RequestInterval,
		"Minimum global delay between outbound requests",
	)
}

func BindInsecureTLSFlag(flags *pflag.FlagSet, cfg *InsecureTLSConfig, usage string) {
	flags.BoolVar(&cfg.ShouldAllowInsecureTLS, "insecure", cfg.ShouldAllowInsecureTLS, usage)
}

func ValidateDirOutRequired(dirOut string) error {
	if strings.TrimSpace(dirOut) == "" {
		return fmt.Errorf("output is required")
	}
	return nil
}

func SnapshotVersionToken(dirSnapshot string) (string, error) {
	dirSnapshot = strings.TrimSpace(dirSnapshot)
	if dirSnapshot == "" {
		return "", fmt.Errorf("snapshot is required")
	}
	dirClean := filepath.Clean(dirSnapshot)
	versionToken := filepath.Base(dirClean)
	if versionToken == "." || versionToken == string(filepath.Separator) || versionToken == "" {
		return "", fmt.Errorf("snapshot must identify a version directory: %s", dirSnapshot)
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
		return fmt.Errorf("max-attempts must be >= 1")
	}
	if cfg.RetryWait < 0 {
		return fmt.Errorf("retry-wait must be >= 0")
	}
	return nil
}

func ValidateDownloadControlConfig(cfg *DownloadControlConfig) error {
	if cfg.WorkersMax < 1 {
		return fmt.Errorf("workers must be >= 1")
	}
	if cfg.RequestInterval < 0 {
		return fmt.Errorf("request-interval must be >= 0")
	}
	return nil
}

func ValidateRuleExisting(cfg *ExistingRuleConfig) error {
	if cfg.RuleExisting != "skip" && cfg.RuleExisting != "overwrite" {
		return fmt.Errorf("on-existing must be one of: skip, overwrite")
	}
	cfg.ShouldOverwriteExisting = cfg.RuleExisting == "overwrite"
	return nil
}

func ExpandListTokens(valuesInput []string, filePath string, optionName string) ([]string, error) {
	valuesResolved := make([]string, 0)
	for _, valueInput := range valuesInput {
		for _, tokenRaw := range strings.Split(valueInput, ",") {
			token := strings.TrimSpace(tokenRaw)
			if token == "" {
				continue
			}
			if strings.HasPrefix(token, "@") {
				return nil, fmt.Errorf("@file syntax is not supported for --%s; use --%s-file", optionName, optionName)
			}
			valuesResolved = append(valuesResolved, token)
		}
	}
	if strings.TrimSpace(filePath) != "" {
		valuesFile, err := readListInputFile(filePath, optionName)
		if err != nil {
			return nil, err
		}
		valuesResolved = append(valuesResolved, valuesFile...)
	}
	return valuesResolved, nil
}

type listFlagState struct {
	values *[]string
	seen   bool
}

func (state *listFlagState) append(values ...string) {
	if !state.seen {
		*state.values = nil
		state.seen = true
	}
	*state.values = append(*state.values, values...)
}

type stringListValue struct {
	state *listFlagState
}

func (value *stringListValue) String() string { return strings.Join(*value.state.values, ",") }
func (value *stringListValue) Type() string   { return "strings" }
func (value *stringListValue) Set(input string) error {
	values, err := csv.NewReader(strings.NewReader(input)).Read()
	if err != nil {
		return fmt.Errorf("parse list value: %w", err)
	}
	value.state.append(values...)
	return nil
}

type listFileValue struct {
	state      *listFlagState
	optionName string
}

func (value *listFileValue) String() string { return "" }
func (value *listFileValue) Type() string   { return "file" }
func (value *listFileValue) Set(filePath string) error {
	valuesFile, err := readListInputFile(filePath, value.optionName)
	if err != nil {
		return err
	}
	value.state.append(valuesFile...)
	return nil
}

func BindStringListFlags(flags *pflag.FlagSet, values *[]string, optionName string, usage string) {
	state := &listFlagState{values: values}
	flags.Var(&stringListValue{state: state}, optionName, usage)
	flags.Var(&listFileValue{state: state, optionName: optionName}, optionName+"-file", "Read "+optionName+" from a file")
}

func ApplyFlatSnapshot(dirOut *DirOutConfig, version *VersionConfig, snapshot string, expectedAsset string) error {
	dirRoot, versionToken, err := FlatSnapshotRootVersion(snapshot, expectedAsset)
	if err != nil {
		return err
	}
	dirOut.DirOut = dirRoot
	version.VersionToken = versionToken
	return nil
}

func FlatSnapshotRootVersion(snapshot string, expectedAsset string) (string, string, error) {
	versionToken, err := SnapshotVersionToken(snapshot)
	if err != nil {
		return "", "", err
	}
	dirSnapshot := filepath.Clean(snapshot)
	dirAsset := filepath.Dir(dirSnapshot)
	dirRoot := filepath.Dir(dirAsset)
	if dirAsset == "." || filepath.Base(dirAsset) == "." {
		return "", "", fmt.Errorf("snapshot must identify <resource-root>/<asset>/<version>: %s", snapshot)
	}
	if filepath.Base(dirAsset) != expectedAsset {
		return "", "", fmt.Errorf("snapshot must identify asset %q: %s", expectedAsset, snapshot)
	}
	if filepath.Clean(filepath.Join(dirRoot, expectedAsset, versionToken)) != dirSnapshot {
		return "", "", fmt.Errorf("snapshot path does not match asset %q: %s", expectedAsset, snapshot)
	}
	return dirRoot, versionToken, nil
}

func NestedSnapshotRootDatasetVersion(snapshot string, expectedAsset string) (string, string, string, error) {
	versionToken, err := SnapshotVersionToken(snapshot)
	if err != nil {
		return "", "", "", err
	}
	dirSnapshot := filepath.Clean(snapshot)
	dirDataset := filepath.Dir(dirSnapshot)
	dataset := filepath.Base(dirDataset)
	dirAsset := filepath.Dir(dirDataset)
	dirRoot := filepath.Dir(dirAsset)
	if dataset == "." || dataset == "" || filepath.Base(dirAsset) != expectedAsset {
		return "", "", "", fmt.Errorf(
			"snapshot must identify <resource-root>/%s/<dataset>/<version>: %s",
			expectedAsset,
			snapshot,
		)
	}
	if filepath.Clean(filepath.Join(dirRoot, expectedAsset, dataset, versionToken)) != dirSnapshot {
		return "", "", "", fmt.Errorf("snapshot path does not match asset %q: %s", expectedAsset, snapshot)
	}
	return dirRoot, dataset, versionToken, nil
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
