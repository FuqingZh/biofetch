# Static Database Downloads Verification

## Verification Goals

The feature is accepted only if it proves four properties:

- correctness: selected assets map to the intended official URLs
- reproducibility: `manifest.lock` can recreate the same snapshot
- compatibility: existing commands and manifests are not changed
- operability: logs explain source resolution, planning, reuse, download, and
  manifest writes

## Unit Test Matrix

| Area | Test focus | Evidence |
| --- | --- | --- |
| asset registry | valid declarations and duplicate rejection | `go test ./internal/shared/staticasset` |
| asset selection | inline, comma, repeated flag, `@file`, unknown names | parser tests |
| version resolver | source version and fallback token behavior | resolver tests |
| fetch planner | hash-based reuse, stale local file, missing file, overwrite | planner tests |
| downloader | retry, timeout, HTTP status errors, SHA256, bytes | `httptest.Server` tests |
| manifest | TOML shape, stable ordering, scope fields | golden or decoded TOML tests |
| lock | recursive rebuild, preserve URLs from previous manifest | lock tests |
| sync | rehydrate missing files from manifest URLs | sync tests |
| dry-run | no directories or files created | filesystem tests |
| CLI | command flags and validation errors | command execution tests |
| path safety | reject absolute paths, `..`, empty paths, duplicates | validation tests |

## Dataset-Specific Tests

### GO Slim

- parse supported format values: `obo`, `owl`, `json`, `tsv`
- build official subset URLs under `ontology/subsets/`
- build archive subset URLs under
  `release.geneontology.org/<YYYY-MM-DD>/ontology/subsets/`
- reject unsupported subset IDs
- write one manifest file per subset-format output set

### Reactome

- restrict `mapping` command to mapping assets
- reject graph and SQL dump assets from the first mapping command
- record Reactome source URL exactly
- discover size metadata when available
- require explicit large-file confirmation above the configured threshold

### WikiPathways

- parse HTML directory listing
- ignore parent-directory and non-GMT entries
- extract release date from filenames
- select species deterministically
- require confirmation for all-species mode
- reject unsupported historical archive requests before download

### Subcell UniProt

- build deterministic UniProt query or raw asset URL from one explicit scope:
  species, taxid, or proteome
- parse protein-to-location annotations from fixture records
- normalize output to `protein_location` with at least `protein_id`,
  `location`, `source`, and `source_version`
- preserve UniProt-like free-text locations such as `Nucleus.` and
  `Endoplasmic reticulum membrane.`
- reject ambiguous mixed scope requests before network access

### Future Subcell Sources

- HPA and COMPARTMENTS should be added as source-specific commands that write
  the same `protein_location` asset contract
- source-specific evidence and confidence fields may be optional columns
- source-specific raw files and manifest `source` values must remain distinct

### UniProt Vocabulary

- keep UniProt `subcell.txt` out of the first protein-location annotation
  command set
- if added later, expose it as a controlled-vocabulary asset, not as a
  protein-to-location annotation database

## Log Trace Contract

Tests should assert structured trace events. CLI logs can remain human-readable,
but each logged action should be backed by an event with stable fields.

Minimum fields:

```text
event
database
asset
version_token
path
url
bytes
sha256
status
content_length
etag
last_modified
```

Required event order for `fetch`:

```text
resolve_source
resolve_assets
plan_fetch
reuse_file/download_file
write_manifest
```

Required event order for `sync`:

```text
read_manifest
plan_sync
reuse_file/download_file
write_manifest
```

Required event order for `lock`:

```text
scan_files
lock_rebuild
write_manifest
```

## A/B Test Design

The A/B tests compare existing or direct behavior with the shared static asset
kernel.

### A Variant

Use one of:

- current GO ontology code path when comparing generic fixed-asset behavior
- direct fixture download with explicit expected URLs
- hand-built expected asset list from a fixture directory index

### B Variant

Use the new command or the new static asset kernel.

### Required Fixtures

Fixtures should be local and deterministic:

- fake GO Slim files
- fake Reactome mapping files
- fake WikiPathways GMT directory listing and files
- fake UniProt protein annotation payload with subcellular location records

Do not depend on live external HTTP in unit or A/B tests.

### Metrics

| Metric | Expected |
| --- | --- |
| selected asset count | A equals B |
| downloaded file count | A equals B on first run |
| second-run download count | B equals 0 with `skip` |
| raw bytes | A equals B |
| SHA256 | A equals B |
| same-size changed content | B downloads or fails hash validation |
| relative path | A equals B where the same layout is intended |
| manifest URL | B equals expected official URL |
| unsupported request network calls | 0 |

## Smoke Test Commands

Use offline-safe smoke tests first:

```bash
go test ./...

go run ./cmd/biofetch go slim fetch --help

go run ./cmd/biofetch reactome mapping fetch \
  --dir_out /tmp/biofetch-smoke/reactome \
  --assets ReactomePathways.txt \
  --should_dry_run

go run ./cmd/biofetch wikipathways gmt fetch --help

go run ./cmd/biofetch subcell uniprot fetch \
  --dir_out /tmp/biofetch-smoke/subcell \
  --taxids 9606 \
  --should_dry_run
```

Live-download smoke tests should be opt-in because upstream files can be large
or rate-limited. Keep them outside default unit tests.

GO Slim and WikiPathways dry-run currently resolve source metadata from live
upstream endpoints before planning, so the default offline smoke uses `--help`
for those command surfaces and relies on fixture tests for source parsing and
download behavior.

## PR Evidence Checklist

Every implementation PR must include:

- commands run
- unit test result
- fixture A/B result when applicable
- representative log trace
- manifest excerpt
- compatibility statement for existing commands

## Failure Handling

Reject the PR or keep it draft if any of these are true:

- a source-specific command duplicates generic download/manifest code instead
  of using the shared kernel
- an unsupported asset reaches network access before validation fails
- second run with `--rule_existing skip` downloads unchanged files
- second run with `--rule_existing skip` reuses a changed file only because byte
  size matches
- manifest omits URL, bytes, or SHA256
- existing package tests regress
- a large Reactome file can be downloaded without dry-run visibility or explicit
  confirmation
- a historical WikiPathways version silently resolves to `current`
- a nested `raw/` file is ignored by `lock`

## Failure Mode Matrix

| Failure | Expected handling |
| --- | --- |
| unknown asset or species | fail before network access with available choices or count |
| unsupported archive version | fail explicitly; do not use `current` |
| 404 from manifest URL during sync | report snapshot source unavailable |
| 429 or 503 | retry with request interval and trace attempts |
| timeout | fail with URL, timeout duration, and target path |
| partial download | keep or remove only `.part`; never write manifest for it |
| same-size content drift | detect with SHA256 and redownload or fail |
| large file without confirmation | fail before download |
| path escapes version dir | reject declaration during validation |
