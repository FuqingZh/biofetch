package uniprot

import (
	"biofetch/internal/shared/tomlx"
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type dmndRegisterConfig struct {
	dirOut         string
	versionToken   string
	fileDMND       string
	fastaVersion   string
	fastaPolicy    string
	headerFormat   string
	diamondVersion string
	buildCommand   string
	shouldDryRun   bool
}

type dmndManifest struct {
	Database     string           `toml:"database"`
	Asset        string           `toml:"asset"`
	Source       string           `toml:"source"`
	Version      string           `toml:"version"`
	VersionToken string           `toml:"version_token"`
	RegisteredAt string           `toml:"registered_at"`
	SourceFASTA  dmndSourceFASTA  `toml:"source_fasta"`
	Diamond      dmndDiamondBuild `toml:"diamond"`
	Files        []dmndFileRecord `toml:"files"`
}

type dmndSourceFASTA struct {
	Version      string `toml:"version"`
	Policy       string `toml:"policy"`
	HeaderFormat string `toml:"header_format"`
}

type dmndDiamondBuild struct {
	Version string `toml:"version"`
	Command string `toml:"command"`
}

type dmndFileRecord struct {
	Asset  string `toml:"asset"`
	Path   string `toml:"path"`
	SHA256 string `toml:"sha256"`
	Bytes  int64  `toml:"bytes"`
}

func runRegisterDMND(cfg *dmndRegisterConfig) error {
	if err := validateDMNDRegisterConfig(cfg); err != nil {
		return err
	}
	fileDMNDAbs, err := filepath.Abs(cfg.fileDMND)
	if err != nil {
		return fmt.Errorf("resolve dmnd path: %w", err)
	}
	record, err := buildDMNDFileRecord(fileDMNDAbs)
	if err != nil {
		return err
	}
	dirVersion := filepath.Join(cfg.dirOut, "dmnd", cfg.versionToken)
	fileManifest := filepath.Join(dirVersion, "manifest.lock")
	if cfg.shouldDryRun {
		return nil
	}
	if err := os.MkdirAll(dirVersion, 0o755); err != nil {
		return fmt.Errorf("create dmnd version dir: %w", err)
	}
	manifest := dmndManifest{
		Database:     "uniprot",
		Asset:        "dmnd",
		Source:       "registered",
		Version:      cfg.versionToken,
		VersionToken: cfg.versionToken,
		RegisteredAt: time.Now().Format(time.RFC3339),
		SourceFASTA: dmndSourceFASTA{
			Version:      cfg.fastaVersion,
			Policy:       cfg.fastaPolicy,
			HeaderFormat: cfg.headerFormat,
		},
		Diamond: dmndDiamondBuild{
			Version: cfg.diamondVersion,
			Command: cfg.buildCommand,
		},
		Files: []dmndFileRecord{record},
	}
	return tomlx.WriteFileAtomic(fileManifest, manifest)
}

func validateDMNDRegisterConfig(cfg *dmndRegisterConfig) error {
	if strings.TrimSpace(cfg.dirOut) == "" {
		return fmt.Errorf("dir_out is required")
	}
	if strings.TrimSpace(cfg.versionToken) == "" {
		return fmt.Errorf("version is required")
	}
	if strings.EqualFold(strings.TrimSpace(cfg.versionToken), uniprotCurrentVersionToken) {
		return fmt.Errorf("UniProt DMND version must be a fixed local token, not current")
	}
	if strings.TrimSpace(cfg.fileDMND) == "" {
		return fmt.Errorf("file_dmnd is required")
	}
	if strings.TrimSpace(cfg.fastaVersion) == "" {
		return fmt.Errorf("fasta_version is required")
	}
	if strings.TrimSpace(cfg.fastaPolicy) == "" {
		return fmt.Errorf("fasta_policy is required")
	}
	if strings.TrimSpace(cfg.headerFormat) == "" {
		return fmt.Errorf("header_format is required")
	}
	if strings.TrimSpace(cfg.diamondVersion) == "" {
		return fmt.Errorf("diamond_version is required")
	}
	if strings.TrimSpace(cfg.buildCommand) == "" {
		return fmt.Errorf("build_command is required")
	}
	return nil
}

func buildDMNDFileRecord(filePath string) (dmndFileRecord, error) {
	infoFile, err := os.Stat(filePath)
	if err != nil {
		return dmndFileRecord{}, fmt.Errorf("stat dmnd file: %w", err)
	}
	if infoFile.IsDir() {
		return dmndFileRecord{}, fmt.Errorf("dmnd path is a directory: %s", filePath)
	}
	if infoFile.Size() <= 0 {
		return dmndFileRecord{}, fmt.Errorf("dmnd file is empty: %s", filePath)
	}
	sha256File, err := calculateSHA256ForPath(filePath)
	if err != nil {
		return dmndFileRecord{}, err
	}
	return dmndFileRecord{
		Asset:  "uniprot.dmnd",
		Path:   filePath,
		SHA256: sha256File,
		Bytes:  infoFile.Size(),
	}, nil
}

func calculateSHA256ForPath(filePath string) (string, error) {
	fileIn, err := os.Open(filePath)
	if err != nil {
		return "", fmt.Errorf("open %s for sha256: %w", filePath, err)
	}
	defer fileIn.Close()
	hashSHA256 := sha256.New()
	if _, err := io.Copy(hashSHA256, fileIn); err != nil {
		return "", fmt.Errorf("hash %s: %w", filePath, err)
	}
	return fmt.Sprintf("%x", hashSHA256.Sum(nil)), nil
}
