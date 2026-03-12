package cliopt

import (
	"fmt"
	"strings"
	"time"

	"github.com/spf13/pflag"
)

type DirOutConfig struct {
	DirOut string
}

type VersionConfig struct {
	VersionToken string
}

type DryRunConfig struct {
	ShouldDryRun bool
}

type ExistingRuleConfig struct {
	RuleExisting            string
	ShouldOverwriteExisting bool
}

type RetryConfig struct {
	RetryMax  int
	RetryWait time.Duration
}

type InsecureTLSConfig struct {
	ShouldAllowInsecureTLS bool
}

func BindDirOutFlag(flags *pflag.FlagSet, cfg *DirOutConfig, usage string) {
	flags.StringVar(&cfg.DirOut, "dir_out", cfg.DirOut, usage)
}

func BindVersionFlag(flags *pflag.FlagSet, cfg *VersionConfig, usage string) {
	flags.StringVar(&cfg.VersionToken, "version", cfg.VersionToken, usage)
}

func BindDryRunFlag(flags *pflag.FlagSet, cfg *DryRunConfig, usage string) {
	flags.BoolVar(&cfg.ShouldDryRun, "should_dry_run", cfg.ShouldDryRun, usage)
}

func BindRuleExistingFlag(flags *pflag.FlagSet, cfg *ExistingRuleConfig, usage string) {
	flags.StringVar(&cfg.RuleExisting, "rule_existing", cfg.RuleExisting, usage)
}

func BindRetryFlags(flags *pflag.FlagSet, cfg *RetryConfig, retryWaitSec *int) {
	flags.IntVar(&cfg.RetryMax, "retry_max", cfg.RetryMax, "Max retry attempts on download failures")
	flags.IntVar(retryWaitSec, "retry_wait_sec", *retryWaitSec, "Wait seconds between retries")
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

func ValidateRuleExisting(cfg *ExistingRuleConfig) error {
	if cfg.RuleExisting != "skip" && cfg.RuleExisting != "overwrite" {
		return fmt.Errorf("rule_existing must be one of: skip, overwrite")
	}
	cfg.ShouldOverwriteExisting = cfg.RuleExisting == "overwrite"
	return nil
}
