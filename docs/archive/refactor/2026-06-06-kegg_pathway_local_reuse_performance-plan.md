# KEGG Pathway Local Reuse Performance Plan

Status: archived; not an active implementation plan

## Goal

Speed up repeated `biofetch kegg pathway fetch` runs when the version directory
already contains many small files.

The main cost to remove is unnecessary local re-hashing. Current
`--rule_existing skip` behavior checks each existing file and rebuilds its
record, which recalculates `sha256` for every file. For KEGG PATHWAY this can
mean tens of thousands of small reads before discovering that most files can be
reused.

## Non-Goals

- Do not change KEGG request rate limiting.
- Do not parallelize KEGG REST downloads beyond existing command controls.
- Do not change the KEGG PATHWAY manifest schema or directory layout.
- Do not migrate KEGG PATHWAY to `staticasset.Manifest` in this plan.

## PR 1: Manifest-First Reuse

### Scope

Read `<dir_out>/pathway/<version>/manifest.lock` before planning downloads and
build a `pathRel -> pathwayRecord` reuse index.

For `--rule_existing skip`:

- if the file exists
- and the file size matches `manifest.bytes`
- and the manifest record has a non-empty `sha256`

then reuse the manifest record directly without recalculating the hash.

Fall back to the current full inspection path when:

- no manifest exists
- the path is missing from manifest
- file size differs from manifest
- manifest checksum is empty
- the file is empty or missing
- `--rule_existing overwrite` is set

### Acceptance

- repeated fetches avoid hashing files whose manifest record and size match
- missing or changed files are still downloaded or rebuilt correctly
- existing manifest merge behavior remains stable
- `go test ./internal/kegg/...` passes

## PR 2: Directory Index for Existence Checks

### Scope

Before inspecting assets for one pathway scope, scan `raw/<scope>` once with
`os.ReadDir` and build a filename index with size metadata.

Use this index to avoid one `os.Stat` call per expected missing file. This is
most useful when a scope is only partially downloaded or when a new asset type
is added.

Preserve fallback behavior:

- if directory indexing fails because the directory is absent, treat it as
  empty
- if indexing hits a real filesystem error, fail fast
- final record building still validates files that are selected for reuse

### Acceptance

- missing files can be detected from the directory index without per-file stat
- absent scope directories are handled as empty
- existing files still pass size and manifest checks before reuse
- `go test ./internal/kegg/...` passes

## PR 3: Parallel Local Inspect

### Scope

Split pathway fetch planning into two phases:

1. build expected asset tasks from `pathwayID x asset`
2. inspect local files concurrently to classify tasks as reused or missing

Use a small bounded worker pool for local inspect. Reuse existing worker
configuration if it fits the command semantics; otherwise add an internal
constant first and expose a CLI option only if measurement shows users need it.

Keep network behavior unchanged:

- local inspect may run concurrently
- KEGG downloads still go through existing retry and request limiter
- per-asset 404 skip behavior remains unchanged

### Acceptance

- output record order remains deterministic
- a local inspect error cancels queued inspect work and returns the error
- download request interval is still enforced
- `go test ./internal/kegg/...` and `go test ./...` pass

## PR 4: Measurement and Trace

### Scope

Add lightweight evidence for this optimization without adding noisy user output.

Record timing and counts in a trace or verification doc:

- number of expected assets
- number reused by manifest fast path
- number rebuilt by hash fallback
- number scheduled for download
- elapsed local planning time before and after the change

Prefer a project trace for raw measurements and promote only the stable design
notes into docs.

### Acceptance

- measurement uses a realistic KEGG PATHWAY directory with many files
- trace distinguishes local planning time from network download time
- final docs state the operational behavior and failure fallbacks
- no new default verbose CLI output is added

## Failure Modes

- **Manifest stale but size matches**: reuse can miss content drift if a file is
  replaced with same-size content. This is an accepted tradeoff only for
  files already recorded in `manifest.lock`; `lock` can still rebuild full
  checksums when stronger validation is needed.
- **Manifest missing or corrupt**: fall back to current file-based inspection or
  return the manifest parse error if the file exists but cannot be parsed.
- **Partial `.part` files**: ignore for reuse; resumable download logic handles
  them when the asset is scheduled for download.
- **Network filesystem pressure**: keep local inspect worker count bounded; do
  not hash all files concurrently by default.
- **KEGG asset missing upstream**: preserve current per-pathway 404 skip
  behavior and avoid writing missing assets into the manifest.

## Expected Effect

Repeated fetches over a mostly complete KEGG PATHWAY snapshot should spend far
less time in local planning because most existing files can be accepted from
manifest metadata after a size check. Parallel inspect then improves the
remaining stat/fallback work without changing network safety behavior.
