# Session Handoff Closeout

Version: v1.0
Date: 2026-06-15
Status: current

## Purpose

This handoff summarizes the important decisions from the long `biofetch`
database-download session so a new session can continue without relying on the
old chat window.

It is not a raw transcript. The detailed source contracts and PR plans remain
in the linked documents below; this file records the cross-topic decisions,
current state, and continuation order.

## Document Map

Read these first:

- `docs/refactor/2026-05-26-static_database_downloads-architecture.md`
- `docs/refactor/2026-05-26-static_database_downloads-pr_plan.md`
- `docs/implementation-plan/20260531-v1.0-diamond-uniprot-annotation-resources-pr-roadmap.md`
- `docs/refactor/2026-06-06-download_unification-pr_plan.md`
- `docs/refactor/2026-06-10-run_logs-and-kegg_batching-plan.md`
- `docs/refactor/2026-06-15-run_logs-and-kegg_batching-closeout.md`

Supporting plans:

- `docs/refactor/2026-06-05-staticasset_chunked_downloads-plan.md`
- `docs/refactor/2026-06-06-kegg_pathway_local_reuse_performance-plan.md`
- `docs/refactor/2026-06-06-kegg_pathway_local_reuse_performance-verification.md`
- `docs/refactor/2026-05-26-static_database_downloads-smoke.md`
- `docs/refactor/2026-05-26-static_database_downloads-verification.md`

## Tool Boundary Decision

The user raised whether `biofetch`, `biotidy`, and `bioextract` should later be
merged into one larger toolkit.

Current working position:

- keep repository and command boundaries for now
- `biofetch` owns downloading, source provenance, versioned raw assets, and
  `manifest.lock`
- `biotidy` owns normalization / canonical tidy conversion
- `bioextract` owns extraction from project or analysis-specific inputs
- a future umbrella CLI can be considered, but should compose these tools
  rather than erase source, tidy, and extraction boundaries prematurely

Reason: the current download work is already source-contract heavy. Merging the
tools now would blur responsibility while the database asset contracts are
still being stabilized.

## Static Database Source Decisions

### GO Slim

- Source: official GO subset files under GO release/current directories
- Command namespace: `biofetch go slim`
- The implementation should use the shared static asset kernel
- `fetch --should_dry_run` may still query upstream to resolve source metadata

### WikiPathways

- Source: official GMT files from WikiPathways
- `current` is a moving directory and must not be silently treated as a fixed
  historical release
- GMT is the right first artifact for pathway gene sets, but its identifier
  type may not be UniProt; downstream mapping/build must handle ID conversion

### Reactome

- Source: official Reactome download mapping assets, especially
  UniProt-to-Reactome mappings
- Reactome mapping fetch is global; species filtering is a build/downstream
  concern, unlike KEGG/STRING/OmniPath paths that are naturally organism-scoped
- Omitted `--version` and `--version current` should resolve the real Reactome
  release token such as `v96`
- Do not write `reactome/mapping/current/`
- Do not infer the next release by local archive version plus one
- `lock` and `sync` require a fixed version and reject `current`

### Subcell

- The current server legacy file
  `proteomics_db/DATABASE/emmapper_anno/<species>/<species>.SubCell` is a
  protein-to-location annotation table with columns:

```text
Protein    Subcellular location
```

- That legacy table is not HPA, COMPARTMENTS, or UniProt `subcell.txt`
- It looks closer to UniProtKB protein annotation extraction, but it is a
  legacy fallback and should not define the new source model
- New first source: `biofetch subcell uniprot`
- Source meaning: UniProtKB protein-to-subcellular-location annotation
- Do not expose a vague `subcell fetch` command
- HPA, COMPARTMENTS, and PSORTdb remain future source-specific subcommands
  that can write the same normalized protein-location contract
- UniProt `subcell.txt` is a controlled vocabulary, not protein-to-location
  mapping, so it is not the first annotation source

Subcell output state:

- raw/tidy `protein_location.tsv` from UniProt REST can be a useful cache
- direct free-text location strings are not clean enough as final ORA terms
- canonical normalization is a later `biotidy` or build-stage concern
- expected future canonical outputs:

```text
canonical/protein_to_location.parquet
canonical/location_ref.parquet
```

### Subcell and GO CC

GO Cellular Component and subcellular localization are related but not
identical.

- GO CC is an ontology of cellular components
- subcell enrichment often uses coarser location categories derived from GO CC
  descendants, UniProt annotations, HPA, COMPARTMENTS, or project-specific
  mappings
- extracting subcell categories from GO CC means mapping selected GO CC terms
  and their descendants into a small, interpretable set such as nucleus,
  cytoplasm, membrane, mitochondrion, ER, Golgi, chloroplast, lysosome, and
  extracellular region
- that GO CC-derived mapping is not the same thing as UniProt protein-location
  annotations

## UniProt Decisions

### ID mapping

- Download raw official files, not REST job outputs, for the static database
  feature
- First raw assets:

```text
idmapping_selected.tab.gz
idmapping.dat.gz
```

- Keep official upstream layout under `raw/`; do not invent `9606/` species
  directories for these global files
- Omitted version and `current` resolve to real UniProt release tokens such as
  `2026_01`
- Large assets require `--should_allow_large_assets`

### KB FASTA and DAT

- `uniprot.dmnd` should be built from UniProtKB FASTA, not treated as an
  official UniProt downloadable asset
- Official FASTA assets:

```text
uniprot_sprot.fasta.gz
uniprot_trembl.fasta.gz
uniprot_sprot_varsplic.fasta.gz
```

- DAT assets should also be downloadable under `uniprot kb` because they belong
  to the UniProtKB knowledgebase release
- Preserve meaningful official layout such as:

```text
raw/knowledgebase/complete/...
```

### UniRef

- `uniref90.fasta.gz` belongs under a separate `uniprot uniref` command, not
  under `uniprot kb`
- `uniref90.dmnd` is not recommended as the default annotation database for
  traceable proteomics annotation because hits point to UniRef clusters rather
  than original UniProt proteins
- If UniRef is used later, annotation transfer requires an explicit
  UniRef-cluster-to-UniProt/member policy

### Mirrors

- UniProt commands should support a configurable current-release base URL
- DDBJ mirror example:

```text
https://ddbj.nig.ac.jp/public/mirror_database/ftp.uniprot.org/
```

- Mirror support should keep source paths stable and record final URLs in
  `manifest.lock`

## Diamond UniProt Annotation Resource Decisions

The `diamond-uniprot` path needs official or quasi-official raw resources after
query proteins are mapped to UniProt accessions.

Key gaps:

- `UniProt/GeneId -> KEGG gene -> KO/pathway`
- `UniProt/protein -> eggNOG OG -> COG_categories`
- `UniProt -> InterPro/Pfam`

Source choices:

- KEGG mapping: official KEGG REST `list`, `conv`, and `link` endpoints
- eggNOG mapper: official eggNOG-mapper database distribution
- COG category definition: NCBI COG2024 category table
- InterPro/Pfam: EBI InterPro `protein2ipr.dat.gz` and `interpro.xml.gz`
- Pfam should initially be represented through InterPro rather than a separate
  root command

## CLI and Safety Decisions

- `--should_download_all_organisms` means full organism scope
- `--should_allow_large_assets` means bypass large-asset guard
- Do not keep old deprecated aliases when flags are renamed or clarified
- Do not require users to type an extra literal confirmation after they already
  passed an explicit safety flag
- Omitting `--assets` can mean all supported assets for asset-scoped commands,
  but large downloads still require `--should_allow_large_assets`
- `lock` and `sync` generally require fixed versions; moving pointers such as
  `current` should be rejected

## Download Behavior Decisions

### Progress

- Shared progress belongs in `internal/shared/staticasset`
- Keep both aggregate progress and current-file progress
- Progress writes to stderr
- One simple progress UI is enough; do not add `auto|always|never` modes
- Keep `--should_disable_progress` for non-interactive logs or CI

### Run logs

- Use `--dir_logs`, not `--file_log`
- If omitted, default to `<version>/logs/`
- Each run writes one new log file
- File name format:

```text
<action>-<YYYYMMDD>T<HHMMSS>Z-<short_runid>.log
```

### Resumable and chunked downloads

- Shared static downloads write to `.part` first
- Final files are renamed into place only after successful download
- Manifest records only final files
- Automatic chunked downloads should be selected internally for large files
  when HTTP Range is supported
- No public chunk-size or chunk-worker flags in the first version

### Manifest persistence

- `manifest.lock` is not append-streamed
- `Fetch` and `Sync` use throttled atomic rewrites
- Flush every 5 seconds only if new successful records made the manifest dirty
- Force a final flush on success or failure
- This applies to commands routed through `internal/shared/staticasset`

## KEGG Decisions

### Mapping

- KEGG mapping is snapshot-based because KEGG REST does not expose stable
  source release tokens for every endpoint
- Organism-scoped KEGG mapping endpoints can return `400` when that organism
  has no such mapping
- For organism-scoped mapping assets such as `conv_uniprot`,
  `conv_ncbi_geneid`, `gene_list`, `gene_ko`, and `gene_pathway`, recover by
  writing an empty file or skipping according to the implemented recovery path
- Global assets such as `organism` and `ko_pathway` still fail hard when
  invalid

### PATHWAY

- Local reuse planning should be manifest-first to avoid re-hashing huge
  existing pathway directories
- Directory scanning and local inspect can be concurrent
- KEGG REST downloads remain rate-limited
- Side assets `kgml`, `conf`, and `image` can be unavailable; `403/404` should
  skip and not write manifest entries
- `entry` `403` should retry, then warn and continue after retries are
  exhausted
- Full-organism fetch should be internally batched so downloading starts before
  the full global task expansion finishes

### KEGG upstream refusal

- KEGG `403`, TLS handshake timeout, and malformed `/info` output are upstream
  or network conditions, not local file-missing errors
- Do not try to bypass KEGG refusal by browser/IP tricks
- Engineering response should be retries, request intervals, logs, resumable
  downloads, batching, and clear error messages

Current clearer release parse error:

```text
failed to parse KEGG release from info response: upstream response was empty
failed to parse KEGG release from info response: upstream response did not contain a 'Release ...' field (...)
```

## Current Worktree State

As of this handoff, the repository has uncommitted implementation work for run
logs and KEGG PATHWAY batching.

Notable uncommitted areas:

- `internal/shared/logx`
- `internal/shared/cliopt`
- `internal/shared/staticasset`
- database command wiring for `--dir_logs`
- KEGG PATHWAY retry/continue and batching behavior
- docs:
  - `2026-06-10-run_logs-and-kegg_batching-plan.md`
  - `2026-06-15-run_logs-and-kegg_batching-closeout.md`
  - this handoff

Expected commit split:

1. shared run-log infrastructure and CLI flag helper
2. database command wiring for `--dir_logs`
3. KEGG PATHWAY retry/continue, clearer release parse errors, and batching
4. docs closeout / handoff

## Verification Notes

Known environment constraint:

- tests using `httptest.NewServer` may fail in this environment with:

```text
listen tcp6 [::1]:0: socket: operation not permitted
```

Use these checks when local listener tests are blocked:

```bash
/usr/local/go/bin/go test -run '^$' ./...
/usr/local/go/bin/go test -run 'TestParseKEGGReleaseFromInfoErrorMessageIsExplicit|TestParseKEGGMajorVersion' ./internal/kegg
```

Build static Linux binary:

```bash
CGO_ENABLED=0 /usr/local/go/bin/go build -o dist/biofetch ./cmd/biofetch
```

Static build is needed because the remote server can have an older GLIBC than
the local build environment.

## Next Session Order

Recommended next steps:

1. review the uncommitted diff for accidental broadening
2. run compile-only whole-repo check
3. split commits according to the expected commit split
4. rebuild `dist/biofetch`
5. smoke-test a small non-KEGG staticasset command for `--dir_logs`
6. smoke-test KEGG PATHWAY on a small organism or targeted `pathway_ids`
7. only then retry full-organism KEGG PATHWAY fetch on the server network
