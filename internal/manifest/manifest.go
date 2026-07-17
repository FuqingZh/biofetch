package manifest

import (
	"bytes"
	"crypto/sha256"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

const schemaVersion = "biofetch-manifest-v1"

type buildConfig struct {
	dirIn       string
	fileStemOut string
	formats     []string
}

type BuildResult struct {
	DatabaseCount int
	SnapshotCount int
	FileCount     int64
	TotalBytes    int64
	Files         []string
}

type aggregateManifest struct {
	SchemaVersion string     `toml:"schema_version" json:"schema_version"`
	ResourceRoot  string     `toml:"resource_root" json:"resource_root"`
	Summary       summary    `toml:"summary" json:"summary"`
	Snapshots     []snapshot `toml:"snapshots" json:"snapshots"`
}

type summary struct {
	DatabaseCount int   `toml:"database_count" json:"database_count"`
	SnapshotCount int   `toml:"snapshot_count" json:"snapshot_count"`
	FileCount     int64 `toml:"file_count" json:"file_count"`
	TotalBytes    int64 `toml:"total_bytes" json:"total_bytes"`
}

type snapshot struct {
	Database              string         `toml:"database" json:"database"`
	Asset                 string         `toml:"asset" json:"asset"`
	Version               string         `toml:"version" json:"version"`
	SourceVersion         string         `toml:"source_version,omitempty" json:"source_version,omitempty"`
	Path                  string         `toml:"path" json:"path"`
	DownloadedAt          string         `toml:"downloaded_at,omitempty" json:"downloaded_at,omitempty"`
	FileCount             int64          `toml:"file_count" json:"file_count"`
	TotalBytes            int64          `toml:"total_bytes" json:"total_bytes"`
	SourceRelease         string         `toml:"source_release,omitempty" json:"source_release,omitempty"`
	SourceReleaseStart    string         `toml:"source_release_start,omitempty" json:"source_release_start,omitempty"`
	SourceReleaseEnd      string         `toml:"source_release_end,omitempty" json:"source_release_end,omitempty"`
	SourceLastUpdate      string         `toml:"source_last_update,omitempty" json:"source_last_update,omitempty"`
	SourceLastUpdateStart string         `toml:"source_last_update_start,omitempty" json:"source_last_update_start,omitempty"`
	SourceLastUpdateEnd   string         `toml:"source_last_update_end,omitempty" json:"source_last_update_end,omitempty"`
	RecordKind            string         `toml:"record_kind,omitempty" json:"record_kind,omitempty"`
	RecordCount           int64          `toml:"record_count,omitempty" json:"record_count,omitempty"`
	Manifest              manifestRecord `toml:"manifest" json:"manifest"`
}

type manifestRecord struct {
	Path   string `toml:"path" json:"path"`
	SHA256 string `toml:"sha256" json:"sha256"`
	Bytes  int64  `toml:"bytes" json:"bytes"`
}

type lockEnvelope struct {
	Database              string             `toml:"database"`
	Asset                 string             `toml:"asset"`
	Version               string             `toml:"version"`
	VersionToken          string             `toml:"version_token"`
	DownloadedAt          string             `toml:"downloaded_at"`
	SourceRelease         string             `toml:"source_release"`
	SourceReleaseStart    string             `toml:"source_release_start"`
	SourceReleaseEnd      string             `toml:"source_release_end"`
	SourceLastUpdate      string             `toml:"source_last_update"`
	SourceLastUpdateStart string             `toml:"source_last_update_start"`
	SourceLastUpdateEnd   string             `toml:"source_last_update_end"`
	Species               []compoundRecord   `toml:"species"`
	Pathways              []compoundRecord   `toml:"pathways"`
	Brites                []compoundRecord   `toml:"brites"`
	Files                 []lockFileEnvelope `toml:"files"`
}

type compoundRecord struct {
	ID string `toml:"id"`
}

type lockFileEnvelope struct {
	Path   string `toml:"path"`
	SHA256 string `toml:"sha256"`
	Bytes  *int64 `toml:"bytes"`
}

type outputFile struct {
	path string
	data []byte
}

func Build(dirIn string, fileStemOut string, formats []string) (BuildResult, error) {
	model, stemPhysical, formatsResolved, err := buildModel(buildConfig{
		dirIn:       dirIn,
		fileStemOut: fileStemOut,
		formats:     formats,
	})
	if err != nil {
		return BuildResult{}, err
	}
	outputs, err := renderOutputs(model, stemPhysical, formatsResolved)
	if err != nil {
		return BuildResult{}, err
	}
	if err := writeOutputsAtomically(outputs); err != nil {
		return BuildResult{}, err
	}
	files := make([]string, 0, len(outputs))
	for _, output := range outputs {
		files = append(files, output.path)
	}
	return BuildResult{
		DatabaseCount: model.Summary.DatabaseCount,
		SnapshotCount: model.Summary.SnapshotCount,
		FileCount:     model.Summary.FileCount,
		TotalBytes:    model.Summary.TotalBytes,
		Files:         files,
	}, nil
}

func buildModel(cfg buildConfig) (aggregateManifest, string, []string, error) {
	rootPhysical, err := canonicalDirectory(cfg.dirIn, "dir_in")
	if err != nil {
		return aggregateManifest{}, "", nil, err
	}
	stemPhysical, outputParent, err := canonicalOutputStem(cfg.fileStemOut)
	if err != nil {
		return aggregateManifest{}, "", nil, err
	}
	formats, err := normalizeFormats(cfg.formats)
	if err != nil {
		return aggregateManifest{}, "", nil, err
	}
	resourceRoot, err := filepath.Rel(outputParent, rootPhysical)
	if err != nil {
		return aggregateManifest{}, "", nil, fmt.Errorf("resolve resource_root: %w", err)
	}

	lockPaths, err := discoverLocks(rootPhysical)
	if err != nil {
		return aggregateManifest{}, "", nil, err
	}
	snapshots := make([]snapshot, 0, len(lockPaths))
	validationErrors := make([]error, 0)
	for _, fileLock := range lockPaths {
		item, err := readSnapshot(rootPhysical, fileLock)
		if err != nil {
			pathRel, relErr := filepath.Rel(rootPhysical, fileLock)
			if relErr != nil {
				pathRel = fileLock
			}
			validationErrors = append(validationErrors, fmt.Errorf("%s: %w", filepath.ToSlash(pathRel), err))
			continue
		}
		snapshots = append(snapshots, item)
	}
	sort.Slice(snapshots, func(i, j int) bool {
		left := snapshots[i]
		right := snapshots[j]
		if left.Database != right.Database {
			return left.Database < right.Database
		}
		if left.Asset != right.Asset {
			return left.Asset < right.Asset
		}
		if left.Version != right.Version {
			return left.Version < right.Version
		}
		return left.Path < right.Path
	})

	identities := make(map[string]string, len(snapshots))
	databases := make(map[string]struct{})
	var fileCount int64
	var totalBytes int64
	validatedSnapshots := make([]snapshot, 0, len(snapshots))
	for _, item := range snapshots {
		identity := item.Database + "\x00" + item.Asset + "\x00" + item.Version
		if previous, ok := identities[identity]; ok {
			validationErrors = append(validationErrors, fmt.Errorf(
				"duplicate snapshot identity (%s, %s, %s): %s and %s",
				item.Database,
				item.Asset,
				item.Version,
				previous,
				item.Path,
			))
			continue
		}
		identities[identity] = item.Path
		validatedSnapshots = append(validatedSnapshots, item)
		databases[item.Database] = struct{}{}
		if item.FileCount > math.MaxInt64-fileCount || item.TotalBytes > math.MaxInt64-totalBytes {
			return aggregateManifest{}, "", nil, fmt.Errorf("manifest summary exceeds int64 capacity")
		}
		fileCount += item.FileCount
		totalBytes += item.TotalBytes
	}

	model := aggregateManifest{
		SchemaVersion: schemaVersion,
		ResourceRoot:  filepath.ToSlash(resourceRoot),
		Summary: summary{
			DatabaseCount: len(databases),
			SnapshotCount: len(validatedSnapshots),
			FileCount:     fileCount,
			TotalBytes:    totalBytes,
		},
		Snapshots: validatedSnapshots,
	}
	if len(validationErrors) > 0 {
		return aggregateManifest{}, "", nil, fmt.Errorf(
			"manifest validation failed: compatible databases=%d snapshots=%d files=%d bytes=%d; incompatible=%d: %w",
			model.Summary.DatabaseCount,
			model.Summary.SnapshotCount,
			model.Summary.FileCount,
			model.Summary.TotalBytes,
			len(validationErrors),
			errors.Join(validationErrors...),
		)
	}
	return model, stemPhysical, formats, nil
}

func canonicalDirectory(value string, optionName string) (string, error) {
	if strings.TrimSpace(value) == "" {
		return "", fmt.Errorf("%s is required", optionName)
	}
	pathAbsolute, err := filepath.Abs(value)
	if err != nil {
		return "", fmt.Errorf("resolve %s: %w", optionName, err)
	}
	pathPhysical, err := filepath.EvalSymlinks(pathAbsolute)
	if err != nil {
		return "", fmt.Errorf("resolve physical %s: %w", optionName, err)
	}
	info, err := os.Stat(pathPhysical)
	if err != nil {
		return "", fmt.Errorf("stat %s: %w", optionName, err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("%s must be a directory: %s", optionName, value)
	}
	return filepath.Clean(pathPhysical), nil
}

func canonicalOutputStem(value string) (string, string, error) {
	if strings.TrimSpace(value) == "" {
		return "", "", fmt.Errorf("file_stem_out is required")
	}
	pathAbsolute, err := filepath.Abs(value)
	if err != nil {
		return "", "", fmt.Errorf("resolve file_stem_out: %w", err)
	}
	parentPhysical, err := canonicalDirectory(filepath.Dir(pathAbsolute), "file_stem_out parent")
	if err != nil {
		return "", "", err
	}
	stemPhysical := filepath.Join(parentPhysical, filepath.Base(pathAbsolute))
	return stemPhysical, parentPhysical, nil
}

func normalizeFormats(values []string) ([]string, error) {
	if len(values) == 0 {
		values = []string{"toml"}
	}
	set := make(map[string]struct{})
	for _, value := range values {
		for _, token := range strings.Split(value, ",") {
			format := strings.ToLower(strings.TrimSpace(token))
			if format == "" {
				continue
			}
			switch format {
			case "toml", "tsv", "json":
				set[format] = struct{}{}
			default:
				return nil, fmt.Errorf("formats must contain only toml, tsv, or json: %s", token)
			}
		}
	}
	if len(set) == 0 {
		return nil, fmt.Errorf("formats must not be empty")
	}
	order := []string{"toml", "tsv", "json"}
	formats := make([]string, 0, len(set))
	for _, format := range order {
		if _, ok := set[format]; ok {
			formats = append(formats, format)
		}
	}
	return formats, nil
}

func discoverLocks(root string) ([]string, error) {
	locks := make([]string, 0)
	err := filepath.WalkDir(root, func(filePath string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !entry.IsDir() && entry.Name() == "manifest.lock" {
			locks = append(locks, filePath)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("discover manifest.lock files: %w", err)
	}
	sort.Strings(locks)
	return locks, nil
}

func readSnapshot(root string, fileLock string) (snapshot, error) {
	data, err := os.ReadFile(fileLock)
	if err != nil {
		return snapshot{}, fmt.Errorf("read: %w", err)
	}
	var lock lockEnvelope
	if err := toml.Unmarshal(data, &lock); err != nil {
		return snapshot{}, fmt.Errorf("parse TOML: %w", err)
	}
	if strings.TrimSpace(lock.Database) == "" {
		return snapshot{}, fmt.Errorf("database is required")
	}
	if strings.TrimSpace(lock.Asset) == "" {
		return snapshot{}, fmt.Errorf("asset is required")
	}
	if strings.TrimSpace(lock.VersionToken) == "" {
		return snapshot{}, fmt.Errorf("version_token is required")
	}
	dirSnapshot := filepath.Dir(fileLock)
	pathSnapshot, err := filepath.Rel(root, dirSnapshot)
	if err != nil {
		return snapshot{}, fmt.Errorf("resolve snapshot path: %w", err)
	}
	if pathSnapshot == ".." || strings.HasPrefix(pathSnapshot, ".."+string(filepath.Separator)) {
		return snapshot{}, fmt.Errorf("snapshot is outside resource root")
	}
	if filepath.Base(dirSnapshot) != lock.VersionToken {
		return snapshot{}, fmt.Errorf(
			"version_token %q does not match snapshot directory %q",
			lock.VersionToken,
			filepath.ToSlash(pathSnapshot),
		)
	}
	if len(lock.Files) == 0 {
		return snapshot{}, fmt.Errorf("files must contain at least one record")
	}

	pathsSeen := make(map[string]struct{}, len(lock.Files))
	var totalBytes int64
	for index, file := range lock.Files {
		if err := validateLockFile(file); err != nil {
			return snapshot{}, fmt.Errorf("files[%d]: %w", index, err)
		}
		if _, ok := pathsSeen[file.Path]; ok {
			return snapshot{}, fmt.Errorf("files[%d]: duplicate path %q", index, file.Path)
		}
		pathsSeen[file.Path] = struct{}{}
		if *file.Bytes > math.MaxInt64-totalBytes {
			return snapshot{}, fmt.Errorf("file byte total exceeds int64 capacity")
		}
		totalBytes += *file.Bytes
	}

	hashLock := sha256.Sum256(data)
	item := snapshot{
		Database:              lock.Database,
		Asset:                 lock.Asset,
		Version:               lock.VersionToken,
		Path:                  filepath.ToSlash(pathSnapshot),
		DownloadedAt:          lock.DownloadedAt,
		FileCount:             int64(len(lock.Files)),
		TotalBytes:            totalBytes,
		SourceRelease:         lock.SourceRelease,
		SourceReleaseStart:    lock.SourceReleaseStart,
		SourceReleaseEnd:      lock.SourceReleaseEnd,
		SourceLastUpdate:      lock.SourceLastUpdate,
		SourceLastUpdateStart: lock.SourceLastUpdateStart,
		SourceLastUpdateEnd:   lock.SourceLastUpdateEnd,
		Manifest: manifestRecord{
			Path:   "manifest.lock",
			SHA256: fmt.Sprintf("%x", hashLock),
			Bytes:  int64(len(data)),
		},
	}
	if lock.Version != "" && lock.Version != lock.VersionToken {
		item.SourceVersion = lock.Version
	}
	switch {
	case len(lock.Species) > 0:
		item.RecordKind = "species"
		item.RecordCount = int64(len(lock.Species))
	case len(lock.Pathways) > 0:
		item.RecordKind = "pathways"
		item.RecordCount = int64(len(lock.Pathways))
	case len(lock.Brites) > 0:
		item.RecordKind = "brites"
		item.RecordCount = int64(len(lock.Brites))
	}
	return item, nil
}

func validateLockFile(file lockFileEnvelope) error {
	if strings.Contains(file.Path, "\\") {
		return fmt.Errorf("path must use forward slashes: %q", file.Path)
	}
	if file.Path == "" || path.IsAbs(file.Path) {
		return fmt.Errorf("path must be relative: %q", file.Path)
	}
	parts := strings.Split(file.Path, "/")
	for _, part := range parts {
		if part == ".." {
			return fmt.Errorf("path must not contain ..: %q", file.Path)
		}
	}
	if path.Clean(file.Path) != file.Path || !strings.HasPrefix(file.Path, "raw/") {
		return fmt.Errorf("path must be canonical and start with raw/: %q", file.Path)
	}
	decoded, err := hex.DecodeString(file.SHA256)
	if err != nil || len(decoded) != sha256.Size {
		return fmt.Errorf("sha256 must be 64 hexadecimal characters: %q", file.SHA256)
	}
	if file.Bytes == nil {
		return fmt.Errorf("bytes is required")
	}
	if *file.Bytes < 0 {
		return fmt.Errorf("bytes must be >= 0")
	}
	return nil
}

func renderOutputs(model aggregateManifest, stem string, formats []string) ([]outputFile, error) {
	outputs := make([]outputFile, 0, len(formats))
	for _, format := range formats {
		var data []byte
		var err error
		switch format {
		case "toml":
			data, err = toml.Marshal(model)
		case "json":
			data, err = json.MarshalIndent(model, "", "  ")
			data = append(data, '\n')
		case "tsv":
			data, err = renderTSV(model)
		}
		if err != nil {
			return nil, fmt.Errorf("render %s: %w", format, err)
		}
		outputs = append(outputs, outputFile{path: stem + "." + format, data: data})
	}
	return outputs, nil
}

func renderTSV(model aggregateManifest) ([]byte, error) {
	var buffer bytes.Buffer
	writer := csv.NewWriter(&buffer)
	writer.Comma = '\t'
	header := []string{
		"SchemaVersion", "ResourceRoot", "Database", "Asset", "Version", "SourceVersion", "Path",
		"DownloadedAt", "FileCount", "TotalBytes", "SourceRelease", "SourceReleaseStart", "SourceReleaseEnd",
		"SourceLastUpdate", "SourceLastUpdateStart", "SourceLastUpdateEnd", "RecordKind", "RecordCount",
		"ManifestPath", "ManifestSHA256", "ManifestBytes",
	}
	if err := writer.Write(header); err != nil {
		return nil, err
	}
	for _, item := range model.Snapshots {
		record := []string{
			model.SchemaVersion,
			model.ResourceRoot,
			item.Database,
			item.Asset,
			item.Version,
			item.SourceVersion,
			item.Path,
			item.DownloadedAt,
			strconv.FormatInt(item.FileCount, 10),
			strconv.FormatInt(item.TotalBytes, 10),
			item.SourceRelease,
			item.SourceReleaseStart,
			item.SourceReleaseEnd,
			item.SourceLastUpdate,
			item.SourceLastUpdateStart,
			item.SourceLastUpdateEnd,
			item.RecordKind,
			strconv.FormatInt(item.RecordCount, 10),
			item.Manifest.Path,
			item.Manifest.SHA256,
			strconv.FormatInt(item.Manifest.Bytes, 10),
		}
		if err := writer.Write(record); err != nil {
			return nil, err
		}
	}
	writer.Flush()
	if err := writer.Error(); err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}

func writeOutputsAtomically(outputs []outputFile) error {
	temporary := make([]string, 0, len(outputs))
	cleanup := func() {
		for _, filePath := range temporary {
			_ = os.Remove(filePath)
		}
	}
	for _, output := range outputs {
		fileTemp, err := os.CreateTemp(filepath.Dir(output.path), "."+filepath.Base(output.path)+".tmp-*")
		if err != nil {
			cleanup()
			return fmt.Errorf("create temporary output for %s: %w", output.path, err)
		}
		pathTemp := fileTemp.Name()
		temporary = append(temporary, pathTemp)
		if _, err := fileTemp.Write(output.data); err != nil {
			_ = fileTemp.Close()
			cleanup()
			return fmt.Errorf("write temporary output for %s: %w", output.path, err)
		}
		if err := fileTemp.Chmod(0o644); err != nil {
			_ = fileTemp.Close()
			cleanup()
			return fmt.Errorf("chmod temporary output for %s: %w", output.path, err)
		}
		if err := fileTemp.Sync(); err != nil {
			_ = fileTemp.Close()
			cleanup()
			return fmt.Errorf("sync temporary output for %s: %w", output.path, err)
		}
		if err := fileTemp.Close(); err != nil {
			cleanup()
			return fmt.Errorf("close temporary output for %s: %w", output.path, err)
		}
	}
	for index, output := range outputs {
		if err := os.Rename(temporary[index], output.path); err != nil {
			cleanup()
			return fmt.Errorf("replace output %s: %w", output.path, err)
		}
		temporary[index] = ""
	}
	return nil
}
