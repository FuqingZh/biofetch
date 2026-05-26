# Static Database Downloads PR Plan

## PR 1: Shared Static Asset Kernel

### Scope

- Add `internal/shared/staticasset`.
- Implement generic asset declarations, fetch planning, reuse detection,
  downloads, lock rebuild, sync, manifest writing, and trace emission.
- Add tests using `httptest.Server` and temporary directories.
- Do not wire new public CLI commands yet.
- Do not make the shared package own public Cobra command shape; database
  packages keep command construction.

### Public Behavior

None. This PR is internal-only.

### Unit Tests

- asset declaration validation rejects empty names, paths, URLs, and duplicate
  paths
- requested asset resolution supports inline values, comma-separated values,
  repeated flags, and `@file`
- unknown requested assets return an actionable error
- fetch planning reuses existing files when manifest bytes match local bytes
- fetch planning reuses existing files only when SHA256 matches if a manifest
  hash is available
- fetch planning downloads missing or stale files
- download writes SHA256 and byte size into records
- lock rebuild preserves known URLs when an existing manifest is available
- lock rebuild recursively scans nested `raw/` directories
- sync downloads from manifest URLs and respects `skip` / `overwrite`
- dry-run does not create files
- invalid relative paths, duplicate paths, absolute paths, and `..` paths are
  rejected

### Log Traces

Add a structured in-memory trace sink for tests and line-oriented stderr logs for
CLI use. Expected events:

- `resolve_source`
- `resolve_assets`
- `plan_fetch`
- `reuse_file`
- `download_file`
- `write_manifest`
- `read_manifest`
- `plan_sync`
- `sync_file`
- `lock_rebuild`

Test assertions should check event order and key fields, not full prose strings.

### A/B Test

Compare kernel behavior against the current GO ontology implementation using a
local `httptest.Server`.

Variant A:

- current `geneontology` fetch path
- same fake index and same fake assets

Variant B:

- `staticasset` kernel with equivalent asset declarations

Acceptance:

- same raw file bytes
- same relative paths
- same SHA256 values
- same skip-vs-download decisions on second run
- manifest fields match for database, asset, version token, files, path, bytes,
  SHA256, and URL after allowing known schema differences
- byte-size-only reuse is rejected in a negative fixture where content changes
  without a size change

## PR 2: GO Slim Command

### Scope

- Add `biofetch go slim fetch|lock|sync`.
- Reuse GO release resolution where practical.
- Support `--subset` and `--formats`.
- Default to a conservative subset/format, preferably `goslim_generic` and
  `obo`, unless product needs require all formats.
- Resolve assets from `ontology/subsets/`, not the GO ontology root.

### Public Behavior

Example:

```bash
biofetch go slim fetch --dir_out /data/go --subset goslim_generic --formats obo,tsv
```

Expected output directory:

```text
/data/go/slim/<version_token>/raw/
/data/go/slim/<version_token>/tidy/
/data/go/slim/<version_token>/manifest.lock
```

### Unit Tests

- subset name parser accepts known subset IDs
- format parser accepts `obo`, `owl`, `json`, `tsv`
- URL builder produces official current URLs
- archive URL builder produces
  `https://release.geneontology.org/<YYYY-MM-DD>/ontology/subsets/...`
- fetch writes one file per subset-format pair
- manifest scope records subset and format selection
- `lock` rebuilds the same files from disk
- `sync` rehydrates missing files from manifest

### Log Traces

Required trace evidence:

- selected subsets
- selected formats
- resolved version token
- planned file count
- reused/downloaded file count

### A/B Test

Compare `go slim fetch --subset goslim_generic --formats obo` against manual
fixed URL download through the shared kernel test fixture.

Acceptance:

- raw OBO bytes match fixture
- manifest URL is the expected official GO Slim URL
- second run logs `reuse_file` and performs zero downloads

## PR 3: Reactome Mapping Command

### Scope

- Add `biofetch reactome mapping fetch|lock|sync`.
- Start with curated mapping assets, not full graph or SQL dumps.
- Add an asset registry for names shown on the official Reactome download page.
- Add size discovery and large-file confirmation for mapping files.

### Public Behavior

Example:

```bash
biofetch reactome mapping fetch --dir_out /data/reactome --assets UniProt2Reactome_All_Levels.txt
```

### Unit Tests

- known mapping asset registry is stable
- unknown assets are rejected with available choices
- URL builder targets official Reactome download URLs
- size resolver captures `Content-Length` when available
- large mapping files require explicit confirmation or an explicit
  large-download flag
- fetch/lock/sync match static kernel contract
- large dump names are not accepted by the mapping command

### Log Traces

Required trace evidence:

- selected mapping assets
- source base URL
- resolved version token policy
- per-file download/reuse status

### A/B Test

Compare two runs against the same local fixture:

- A: direct static URL download helper
- B: `reactome mapping fetch`

Acceptance:

- identical bytes and SHA256
- B writes manifest with `database = "reactome"` and `asset = "mapping"`
- B performs zero downloads on the second run with `--rule_existing skip`

## PR 4: WikiPathways GMT Command

### Scope

- Add `biofetch wikipathways gmt fetch|lock|sync`.
- Parse current GMT directory listings.
- Support organism selection by species label.
- Add confirmation for `--should_download_all`.
- Do not silently map unsupported historical `--version` values to `current`.

### Public Behavior

Example:

```bash
biofetch wikipathways gmt fetch --dir_out /data/wikipathways --species Homo_sapiens
```

### Unit Tests

- index parser extracts GMT filenames from HTML listings
- release token parser extracts release date from filenames
- archive version requests fail explicitly until archive URL support is
  implemented
- species selector handles inline, repeated, comma-separated, and `@file`
- unknown species reports available choices or a compact count
- all-species mode requires confirmation
- fetch/lock/sync match static kernel contract

### Log Traces

Required trace evidence:

- parsed release date
- selected species count
- selected file count
- confirmation result for all-species mode
- per-file download/reuse status

### A/B Test

Fixture:

- fake directory listing with at least two species and one non-GMT file
- fake GMT file bodies

Acceptance:

- A direct parser expected list equals B command selected assets
- B downloads only selected species
- B preserves deterministic ordering
- second B run reuses all files

## PR 5: Subcellular Sources

### Scope

- Add `biofetch subcell uniprot fetch|lock|sync`.
- Produce the normalized `protein_location` asset from UniProtKB protein
  annotation data.
- Keep source-specific commands separate so HPA and COMPARTMENTS can be added
  later without changing the asset contract.
- Treat UniProt `subcell.txt` as a future controlled-vocabulary asset, not as
  the protein-to-location annotation source.

### Public Behavior

Examples:

```bash
biofetch subcell uniprot fetch --dir_out /data/subcell --species hsa
biofetch subcell uniprot fetch --dir_out /data/subcell --taxids 9606
biofetch subcell uniprot fetch --dir_out /data/subcell --proteome UP000005640
```

### Unit Tests

- UniProt subcell source resolver builds deterministic query URLs from species,
  taxid, or proteome scope
- parser extracts protein identifiers and UniProt-like subcellular location
  strings from fixture annotation records
- normalized `protein_location` output contains `protein_id`, `location`,
  `source`, and `source_version`
- optional evidence and confidence fields are preserved when the source exposes
  them
- unsupported or ambiguous scope combinations are rejected before network access
- UniProt vocabulary requests are rejected with a clear "not implemented as a
  protein-location annotation source" error unless a separate vocabulary command
  is added
- manifests record `database = "subcell"`, `asset = "protein_location"`, and
  `source = "uniprot"`
- fetch/lock/sync match static kernel contract

### Log Traces

Required trace evidence:

- source name
- selected scope type and value
- selected query URL or raw asset URL
- resolved version token
- normalized row count
- per-file download/reuse status

### A/B Test

Fixture:

- fake UniProt annotation payload with several proteins and subcellular
  location terms

Acceptance:

- normalized rows preserve protein-to-location mappings
- manifest preserves `source = "uniprot"` and `asset = "protein_location"`
- second run reuses all files
- unsupported scope combinations fail before network access

### Future Sources

HPA and COMPARTMENTS should be added as later PRs that write the same
`protein_location` asset contract while preserving source-specific raw data,
evidence fields, and manifest `source` values.

## PR 6: End-to-End Smoke and Docs

### Scope

- Add README or docs examples for the new commands.
- Add smoke scripts or documented commands that exercise dry-run and local
  fixture fetches.
- Confirm existing commands still pass tests.

### Public Behavior

No new behavior beyond examples and verification.

### Unit Tests

Run full package tests:

```bash
go test ./...
```

### Log Traces

Capture smoke logs for:

- GO Slim one-file fetch
- Reactome one-file fetch
- WikiPathways one-species fetch
- UniProt subcell one-scope fetch

### A/B Test

Run the static-kernel A/B suite and include a compact result table in the PR
description:

| Dataset | A bytes | B bytes | SHA256 match | Second-run downloads |
| --- | ---: | ---: | --- | ---: |
| GO Slim | n | n | yes | 0 |
| Reactome | n | n | yes | 0 |
| WikiPathways | n | n | yes | 0 |
| Subcell UniProt | n | n | yes | 0 |

## Review Order

Review in PR order. Do not start source-specific PRs before PR 1 is merged or
rebased cleanly, because the main risk is divergent download/manifest behavior.
