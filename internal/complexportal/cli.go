// Package complexportal acquires versioned, unmodified Complex Portal releases.
package complexportal

import (
	"fmt"
	"path/filepath"
	"time"

	"github.com/FuqingZh/biofetch/internal/shared/cliopt"
	"github.com/FuqingZh/biofetch/internal/shared/httpx"
	"github.com/FuqingZh/biofetch/internal/shared/logx"
	"github.com/FuqingZh/biofetch/internal/shared/staticasset"
	"github.com/spf13/cobra"
)

const baseURL = "https://ftp.ebi.ac.uk/pub/databases/intact/complex/"

type config struct {
	cliopt.DirOutConfig
	cliopt.VersionConfig
	cliopt.ExistingRuleConfig
	cliopt.RetryConfig
	cliopt.DownloadControlConfig
	cliopt.InsecureTLSConfig
	cliopt.DryRunConfig
	cliopt.LogConfig
	cliopt.ProgressConfig
}

func NewCommand() *cobra.Command { return newCommand(baseURL) }

func newCommand(base string) *cobra.Command {
	root := &cobra.Command{Use: "complexportal", Short: "Manage complete Complex Portal releases", Args: cobra.NoArgs}
	for _, asset := range []string{"complextab", "psi30"} {
		command := &cobra.Command{Use: asset, Short: "Manage versioned " + asset + " raw assets", Args: cobra.NoArgs}
		for _, action := range []string{"fetch", "lock", "restore"} {
			cfg := config{}
			cfg.RuleExisting, cfg.RetryMax, cfg.RetryWait, cfg.WorkersMax = "skip", 5, 3*time.Second, 2
			use, args := action+" SNAPSHOT", cobra.ExactArgs(1)
			if action == "fetch" {
				use, args = action, cobra.NoArgs
			}
			cmd := &cobra.Command{
				Use: use, Short: action + " the complete " + asset + " snapshot", Args: args,
				RunE: func(cmd *cobra.Command, args []string) error {
					snapshot := ""
					if action != "fetch" {
						snapshot = args[0]
						if err := cliopt.ApplyFlatSnapshot(&cfg.DirOutConfig, &cfg.VersionConfig, snapshot, asset); err != nil {
							return err
						}
					}
					return run(base, asset, action, snapshot, &cfg)
				},
			}
			flags := cmd.Flags()
			if action == "fetch" {
				cliopt.BindDirOutFlag(flags, &cfg.DirOutConfig, "Complex Portal root, e.g. /data/complexportal")
				cliopt.BindVersionFlag(flags, &cfg.VersionConfig, "Upstream YYYY-MM-DD release; omit or use current for the latest dated release")
			}
			if action == "lock" {
				cfg.WorkersMax = cliopt.DefaultLockWorkersMax
				cliopt.BindLockWorkersFlag(flags, &cfg.WorkersMax)
			} else {
				cliopt.BindRuleExistingFlag(flags, &cfg.ExistingRuleConfig, "Existing file policy: skip|overwrite")
				cliopt.BindRetryFlags(flags, &cfg.RetryConfig)
				cliopt.BindDownloadControlFlags(flags, &cfg.DownloadControlConfig)
				cliopt.BindInsecureTLSFlag(flags, &cfg.InsecureTLSConfig, "Disable TLS certificate verification")
				cliopt.BindProgressFlag(flags, &cfg.ProgressConfig, "Disable download progress")
			}
			cliopt.BindDryRunFlag(flags, &cfg.DryRunConfig, "Validate and print actions without writing snapshot files")
			cliopt.BindLogDirFlag(flags, &cfg.LogConfig, "Run log directory; default <version>/logs/")
			command.AddCommand(cmd)
		}
		root.AddCommand(command)
	}
	return root
}

func run(base, asset, action, snapshot string, cfg *config) error {
	if err := cliopt.ValidateDirOutRequired(cfg.DirOut); err != nil {
		return err
	}
	if err := cliopt.ValidateRuleExisting(&cfg.ExistingRuleConfig); err != nil {
		return err
	}
	if err := cliopt.ValidateRetryConfig(&cfg.RetryConfig); err != nil {
		return err
	}
	if err := cliopt.ValidateDownloadControlConfig(&cfg.DownloadControlConfig); err != nil {
		return err
	}
	if action == "lock" {
		if err := cliopt.ValidateLockWorkersMax(cfg.WorkersMax); err != nil {
			return err
		}
	}
	client := httpx.NewClient(cfg.ShouldAllowInsecureTLS)
	version := cfg.VersionToken
	var err error
	if action == "fetch" {
		version, err = resolveVersion(client, base, version)
	} else {
		err = validateVersion(version)
	}
	if err != nil {
		return err
	}
	if snapshot == "" {
		snapshot = filepath.Join(cfg.DirOut, asset, version)
	}
	if err := rejectSymlinks(snapshot); err != nil {
		return err
	}
	source, err := resolveSource(client, base, asset, version, action, snapshot)
	if err != nil {
		return err
	}
	options := staticasset.Options{
		DirOut: cfg.DirOut, RuleExisting: cfg.RuleExisting, RetryMax: cfg.RetryMax,
		RetryWait: cfg.RetryWait, WorkersMax: cfg.WorkersMax, RequestInterval: cfg.RequestInterval,
		ShouldAllowInsecureTLS: cfg.ShouldAllowInsecureTLS, ShouldDryRun: cfg.ShouldDryRun,
		ShouldDisableProgress: cfg.ShouldDisableProgress,
	}
	// An existing lock is authoritative. Fetch must not silently expand a
	// legacy curated-only snapshot or replace its provenance.
	hasManifest := false
	if action != "lock" {
		if _, exists, err := staticasset.ReadManifest(filepath.Join(snapshot, "manifest.lock")); err != nil {
			return err
		} else if exists {
			hasManifest = true
			check := options
			check.ShouldDryRun = true
			if err := staticasset.Sync(source, check, nil); err != nil {
				return fmt.Errorf("existing snapshot is incompatible; preserve it and use a separate output root: %w", err)
			}
		}
	}
	trace := staticasset.TraceSink(logx.NewStaticAssetTraceSink("biofetch complexportal"))
	if !cfg.ShouldDryRun {
		var closeRun func()
		trace, closeRun, err = logx.StartSourceRun("biofetch complexportal", action, cfg.DirLogs, cfg.DirOut, source)
		if err != nil {
			return err
		}
		defer closeRun()
	}
	switch action {
	case "lock":
		return staticasset.Lock(source, snapshot, options, trace)
	case "restore":
		return staticasset.Sync(source, options, trace)
	default:
		if hasManifest {
			return staticasset.Sync(source, options, trace)
		}
		return staticasset.Fetch(source, options, trace)
	}
}
