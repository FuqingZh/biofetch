# Static Database Downloads Plan

## Goal

Add first-class download support for fixed public database assets used in
enrichment, pathway, and localization workflows:

- GO Slim subsets
- WikiPathways releases
- Reactome download assets
- UniProt ID mapping global raw assets
- Subcellular protein-location annotations, starting with UniProt-derived
  protein annotation data and leaving HPA / COMPARTMENTS as compatible future
  sources

The implementation should preserve the current `biofetch` contract: raw files
are downloaded under a versioned directory, `manifest.lock` records exact source
URLs and hashes, and `lock` / `sync` can rebuild or rehydrate a snapshot.

## Current Baseline

The existing command tree is one database package per root command:

- `internal/geneontology`
- `internal/stringdb`
- `internal/kegg`
- `internal/omnipath`

`internal/geneontology` is the closest current model for fixed-source assets.
It already provides:

- `fetch`, `lock`, and `sync`
- `--dir_out`, `--version`, `--assets`, `--rule_existing`
- `--retry_max`, `--retry_wait_sec`, `--workers_max`,
  `--request_interval_ms`
- `--should_allow_insecure_tls`, `--should_dry_run`
- cache-aware reuse through file size and existing manifest records
- `manifest.lock` with database, asset, version, version token, download time,
  file path, SHA256, bytes, and URL

## Proposed Architecture

Create a shared fixed-asset download kernel:

```text
internal/shared/staticasset/
  asset.go
  fetch.go
  locksync.go
  manifest.go
  trace.go
  *_test.go
```

The kernel owns only generic behavior:

- validate static asset declarations
- resolve requested assets
- plan reuse vs download
- run HTTP downloads with retry, workers, and request limiter
- calculate SHA256 and bytes
- write `manifest.lock`
- rebuild lock from local files
- sync files from a manifest
- render a concise download progress indicator to stderr by default
- emit deterministic trace records for tests and smoke evidence

The kernel must not own public Cobra command construction. Database packages
own CLI shape and source policy; the shared package may expose small flag
binding helpers only when they do not hide source-specific behavior.

### Download Progress V1

Progress is a shared-kernel concern, not a per-database implementation. Every
command that uses `staticasset.Fetch` or `staticasset.Sync` should get the same
minimal progress behavior.

Public behavior:

- show a concise loading/progress bar by default
- write progress to stderr, never stdout
- add one shared flag, `--should_disable_progress`, for non-interactive logs or
  CI
- do not add `auto|always|never` modes in v1
- do not add detailed log-file output in v1

Display rules:

- if total bytes are known, show aggregate byte progress, percentage, and
  transfer speed
- if total bytes are unknown but file count is known, show file-count progress
- if both byte total and meaningful file-count progress are weak, show a
  spinner/loading line with downloaded bytes and speed
- show reuse/download counts after planning, then update one progress line
  during downloads
- finish with a compact completed summary

Examples:

```text
uniprot idmapping  [======>.............] 34%  3.2 GiB/9.3 GiB  24.1 MiB/s
wikipathways gmt   [==========>.........] 18/43 files
subcell uniprot    [downloading] 812.4 MiB  12.8 MiB/s
```

Detailed event logs can be added later with a separate `--file_log` option that
serializes existing trace events, but that is outside the v1 progress scope.

Database packages own source-specific policy:

```text
internal/geneontology/
  slim.go
  slim_cli.go

internal/wikipathways/
  cli.go
  gmt.go

internal/reactome/
  cli.go
  mapping.go

internal/uniprot/
  cli.go
  idmapping.go

internal/subcell/
  cli.go
  uniprot.go
```

The root CLI adds the new command packages in `internal/biofetch/cli.go`.

## Source Contracts

### GO Slim

GO Slim should live under the existing GO namespace because GO subsets are part
of the GO ontology release surface.

Proposed command:

```bash
biofetch go slim fetch --dir_out /data/go --subset goslim_generic --formats obo,tsv
biofetch go slim fetch --dir_out /data/go --subset goslim_plant --formats obo,json
biofetch go slim lock --dir_out /data/go --version 2026-05-01
biofetch go slim sync --dir_out /data/go --version 2026-05-01
```

Directory:

```text
<dir_out>/slim/<version_token>/
  raw/
    goslim_generic.obo
    goslim_generic.tsv
  tidy/
  manifest.lock
```

Source:

- GO Slim guide: <https://geneontology.org/docs/go-subset-guide/>
- Current subset index:
  <https://current.geneontology.org/ontology/subsets/index.html>

Implementation note: GO Slim assets live below `ontology/subsets/`, not the
ontology root used by `biofetch go ontology`. Archive support should use
`https://release.geneontology.org/<YYYY-MM-DD>/ontology/subsets/`.

### WikiPathways

WikiPathways should start with GMT assets because the official current release
directory exposes organism-specific GMT files. The current directory is a
monthly release pointer; older releases are retained elsewhere and should not be
silently inferred from `current`.

Proposed command:

```bash
biofetch wikipathways gmt fetch --dir_out /data/wikipathways --species Homo_sapiens
biofetch wikipathways gmt fetch --dir_out /data/wikipathways --species @species.txt
biofetch wikipathways gmt fetch --dir_out /data/wikipathways --should_download_all
```

Directory:

```text
<dir_out>/gmt/<version_token>/
  raw/
    Homo_sapiens/<release-file>.gmt
  tidy/
  manifest.lock
```

Source: <https://data.wikipathways.org/current/gmt/>

Implementation note: first-pass `--version` support should either map to a
known archived monthly release URL or fail with an explicit "archive release not
implemented" error. Do not reinterpret a historical `--version` request as
`current`.

### Reactome

Reactome should start with mapping and GMT-like assets, not full graph or SQL
dumps. Some mapping files are still large, so size discovery and confirmation
are part of the first implementation, not a later enhancement.

Proposed command:

```bash
biofetch reactome mapping fetch --dir_out /data/reactome --assets UniProt2Reactome_All_Levels.txt
biofetch reactome mapping fetch --dir_out /data/reactome --assets @reactome_assets.txt
```

Directory:

```text
<dir_out>/mapping/<version_token>/
  raw/
    UniProt2Reactome_All_Levels.txt
  tidy/
  manifest.lock
```

Sources:

- Download page: <https://reactome.org/download-data>
- Current file index: <https://reactome.org/download/current/>

Implementation note: Reactome file size varies from small text files to
hundreds of MB or larger. Dry-run should show content length when available.
Downloads above a named threshold must require explicit confirmation or an
explicit large-download flag.

### UniProt ID Mapping

UniProt ID mapping should start as a raw mirror/cache for the two official
global mapping files. Do not add REST ID-mapping jobs in the first
implementation; those are request-specific conversion jobs, not static database
downloads.

Proposed command:

```bash
biofetch uniprot idmapping fetch --dir_out /data/uniprot --assets selected --should_allow_large_download
biofetch uniprot idmapping fetch --dir_out /data/uniprot --assets dat --should_allow_large_download
biofetch uniprot idmapping fetch --dir_out /data/uniprot --assets selected,dat --should_allow_large_download
```

Directory:

```text
<dir_out>/idmapping/<version_token>/
  raw/
    idmapping_selected.tab.gz
    idmapping.dat.gz
  manifest.lock
```

Sources:

- Current release notes:
  <https://ftp.uniprot.org/pub/databases/uniprot/current_release/relnotes.txt>
- Current ID mapping directory:
  <https://ftp.uniprot.org/pub/databases/uniprot/current_release/knowledgebase/idmapping/>
- ID mapping REST API:
  <https://www.uniprot.org/help/id_mapping_prog>

Implementation note: `raw/` must preserve the official file layout. Do not
rewrite global files into taxid directories. The first implementation downloads
only:

- `idmapping_selected.tab.gz` (`--assets selected`)
- `idmapping.dat.gz` (`--assets dat`)

Both files are multi-GB assets and must require `--should_allow_large_download`,
even when `Content-Length` is unavailable. `--version current` or an omitted
version must resolve the real UniProt release from `relnotes.txt`, such as
`2026_01`; if release parsing fails, fail the command and do not write a
`current` snapshot.

### Subcellular Sources

Do not expose a vague `subcell fetch` command that hides source meaning. Use
source-specific subcommands. The first source should be UniProt-derived
protein annotation data, producing a versioned protein-to-location annotation
asset.

The source is not UniProt `subcell.txt`. `subcell.txt` is a controlled
vocabulary and does not provide protein-to-location mappings. The first
implementation should derive or fetch records from UniProtKB protein annotation
data and materialize the normalized asset as `protein_location`.

HPA and COMPARTMENTS remain compatible future sources because they can write the
same normalized `protein_location` asset while keeping source-specific raw files
and manifest metadata.

Proposed commands:

```bash
biofetch subcell uniprot fetch --dir_out /data/subcell --species hsa
biofetch subcell uniprot fetch --dir_out /data/subcell --taxids 9606
biofetch subcell uniprot fetch --dir_out /data/subcell --proteome UP000005640
```

Directory:

```text
<dir_out>/uniprot/<version_token>/
  raw/
  tidy/
    protein_location.tsv
  manifest.lock
```

Source contract:

- Source kind: `uniprot_protein_subcell_annotation`
- Input source: UniProtKB protein annotation records
- Output asset: `protein_location`
- Output schema:
  - `protein_id`
  - `location`
  - `source`
  - `source_version`
  - `evidence_type` optional
  - `confidence` optional
- Term style: UniProt-like free-text subcellular location strings such as
  `Nucleus.`, `Cytoplasm.`, `Mitochondrion.`, `Endoplasmic reticulum membrane.`,
  and `Chloroplast.`

Future compatible sources:

- HPA: <https://www.proteinatlas.org/about/download>
- COMPARTMENTS: <https://compartments.jensenlab.org/Downloads>
- UniProt subcellular location vocabulary:
  <https://www.uniprot.org/help/subcellular_location>
- PSORTdb can be added later as `biofetch subcell psortdb ...` because it is
  bacterial/archaeal and has larger archive assets:
  <https://db.psort.org/downloads>

## Version Token Policy

The static asset kernel should accept a source resolver that returns:

- `version`: human-facing version or release label
- `version_token`: filesystem-safe snapshot key
- `base_url` or per-asset URLs

Recommended first-pass policy:

- GO Slim: reuse GO release date from the source OBO header when available;
  fallback to user-supplied `--version`.
- WikiPathways: parse release date from filename when using current directory;
  accept explicit `--version` for archived releases after archive support lands.
- Reactome: use Reactome release number if discoverable from source metadata;
  otherwise use a current-date snapshot token only when the exact URLs are
  captured in `manifest.lock`.
- UniProt ID mapping: use the UniProt release parsed from current release
  notes, such as `2026_01`; fail rather than writing a `current` snapshot when
  the release cannot be resolved.
- UniProt subcell: use the UniProt release when available; otherwise use a
  deterministic snapshot token only when the exact query URL and output hashes
  are captured in `manifest.lock`.
- HPA and COMPARTMENTS future sources: use source-native version metadata when
  available; otherwise use a deterministic snapshot token plus exact URLs and
  hashes.

Every manifest must record the final resolved URL, SHA256, and byte size. That
is the reproducibility contract when upstream version labels are weak.

When upstream provides `Content-Length`, `Last-Modified`, or `ETag`, store them
in the manifest if the shared schema supports optional fields. These values are
secondary metadata; SHA256 remains the file identity check.

## Manifest Contract

The shared manifest should keep existing fields and add only fields that help
static datasets:

```toml
database = "subcell"
asset = "protein_location"
source = "uniprot"
version = "2026_03"
version_token = "2026_03"
downloaded_at = "2026-05-26T12:00:00+08:00"

[scope]
type = "taxid"
value = "9606"

[[files]]
asset = "protein_location"
path = "tidy/protein_location.tsv"
sha256 = "..."
bytes = 12345
url = "https://..."
```

Compatibility rule: existing manifests for GO ontology, STRING, KEGG, and
OmniPath must continue to read and write exactly as they do now.

Reuse rule: the new static asset kernel must validate existing files by SHA256
when a manifest hash is available. Byte size can be used as a fast pre-check,
but matching byte size alone is not enough to skip a download.

## Expected Outcome

After the PR series lands:

- users can fetch common fixed enrichment/pathway/localization source files
  without hand-maintaining URLs
- each dataset snapshot is reproducible from `manifest.lock`
- skip/overwrite behavior is consistent with existing `biofetch` commands
- large or ambiguous sources are guarded by explicit command names and download
  confirmation where needed
- adding a new fixed asset becomes mostly a registry declaration plus parser
  tests, not a new copy of download/manifest plumbing

## Non-Goals

- No tidy conversion in the first series.
- No enrichment analysis or gene ID conversion.
- No full Reactome graph dump in the default command path.
- No generic `subcell` source that hides whether the source is UniProt, HPA,
  COMPARTMENTS, PSORTdb, or a controlled vocabulary.
- No UniProt `subcell.txt` support in the first implementation; if needed
  later, add it as a separate controlled-vocabulary asset rather than the
  `protein_location` annotation source.
- No breaking changes to existing command names, flags, or manifest files.

## Failure Modes

### Source Resolution Failures

- Unknown asset, subset, species, channel, or format should fail before any
  network request when the value can be validated from a registry.
- Directory index parse failures should include the source URL and the expected
  filename pattern.
- Historical `--version` values without implemented archive support should fail
  explicitly instead of falling back to `current`.

### Network and Server Failures

- HTTP 404 means the requested snapshot cannot be resolved; do not silently
  substitute a newer current file.
- HTTP 429, 500, 502, 503, and transient transport errors should use retry with
  request interval and trace events.
- The shared HTTP client should use a finite timeout. Existing `httpx.NewClient`
  has no timeout, which is acceptable for current behavior but should not be
  copied into the new kernel unchanged.

### File Integrity Failures

- Downloads write to `.part` files and rename only after a successful copy and
  hash calculation.
- Failed partial files should be removed or left with an explicit `.part`
  suffix that is ignored by `lock`.
- `sync` must use manifest URLs and hashes. If a manifest URL fails, report the
  snapshot as unavailable rather than resolving a different current URL.

### Large Download Failures

- Dry-run should report planned file count and known content length.
- Large assets require explicit confirmation independent of
  `--should_download_all`.
- Progress display is informational only; if size detection fails, downloads
  should continue with file-count or spinner progress rather than failing.
- Disk-space checks are optional for first implementation, but error messages
  should include target path and partial file path on write failure.

### Path Safety Failures

- Registry paths must be relative, clean paths.
- Reject empty paths, absolute paths, `..`, duplicate paths, and paths that
  escape the version directory after normalization.
- `lock` must support recursive `raw/` scanning because WikiPathways and future
  source-specific downloads can use nested species or taxon directories.
