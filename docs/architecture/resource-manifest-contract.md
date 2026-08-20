# Biofetch resource and manifest contract

Version: v1.3
Date: 2026-08-10
Status: current

## Ownership boundary

`biofetch` owns versioned downloads, source metadata, unmodified or
acquisition-shaped files under `raw/`, and provenance locks. It does not own
parsing, normalization, evidence removal, canonical tables, search-index
construction, or other derived data.

`bioextract` owns derived `tidy/` assets. `biofetch` neither creates empty
`tidy/` directories nor deletes existing `tidy/`, subcellular-location, or
DMND data. UniProt subcellular-location extraction and DIAMOND database
construction are downstream build concerns, not `biofetch` commands.

## Version directories

Database second-level commands use the upstream resource or product's own
stable name. A local family name is invented only when upstream provides no
stable name. This keeps CLI families and snapshot paths aligned with upstream
terminology; for example, JASPAR publishes “JASPAR data”, so its maintained
family and path are `jaspar data` and `jaspar/data/<release>/`.

A snapshot is the directory containing one exact `manifest.lock`. The common
layout is:

```text
<resource-root>/<database>/<asset>/<version_token>/
  raw/
  manifest.lock
```

Nested asset layouts remain valid, but the snapshot directory containing
`manifest.lock` always ends in `version_token`, for example
`omnipath/interactions/kinaseextra/2025-08-13/manifest.lock`. File records use
canonical forward-slash paths below `raw/`; absolute paths, `..`, and paths
outside `raw/` are invalid.

STRING uses explicit sibling assets: `string/network/<version_token>` for
protein network data plus its supporting aliases and protein metadata, and
`string/catalog/<version_token>` for the upstream species catalog.

InterPro uses explicit sibling assets: `interpro/mapping/<version_token>` for
release mapping data and `interpro/scan/<version_token>` for the InterProScan
software distribution. InterProScan accepts only fixed tokens shaped like
`5.77-108.0`. Its raw snapshot contains the release archive and its upstream
`.md5`; fetch verifies the completed archive `.part` against that MD5 before
atomic rename, while the lock continues to record SHA256, bytes, and final
source URL. A verification failure removes `.part` to force a clean subsequent
download and publishes neither a final archive nor a false archive record.
Biofetch does not extract or install the distribution.

The checksum file is itself parsed and filename-bound before it is renamed or
recorded. Once validated for a fetch, it is reused unchanged while the archive
is downloaded, including under `--on-existing overwrite`. Restore requires
exactly the official archive and checksum records; a checksum-only partial
manifest is not restorable.

A published version token is immutable. If upstream content changes, publish a
new version token, rebuild its lock, and regenerate every aggregate manifest
that references the resource tree. Do not silently replace files behind a
published lock.

### Rebuilding a snapshot lock

Every database-specific `lock` command accepts one required snapshot operand
pointing directly at the existing directory that contains
`raw/` and `manifest.lock`. The final directory segment is the authoritative
`version_token`; database-specific token formats are validated after deriving
the value from the path. `lock` does not accept `--output` or `--version` and
does not discover a latest snapshot.

Existing source metadata may be preserved while rebuilding, but an existing
lock must never override the directory-derived `version` or `version_token`.
Fetch retains `-o` / `--output` and its version selector. `restore` accepts the
same exact snapshot operand as `lock`, reads its identity and source URLs from
`manifest.lock`, and does not expose output or version selectors. Nested
snapshots such as OmniPath interactions remain exact rather than being reduced
to a fixed number of parent directories.

Live OmniPath interaction snapshots use the `full-evidence-v1` contract.
`raw/query_meta.json` is locally generated canonical JSON, not an upstream
query response. It records the explicit license, dataset, organisms, supported
query-field tokens, distinct fixed output header, DoRothEA levels, start/end
inventory digests, transport, and actual leaf plan. The typed `[query]`
manifest section cross-checks that sidecar. Lock never reconstructs these
values from filenames or guessed URLs. Full-evidence JSON must be the exact
canonical serialization that restore can reproduce; unknown fields or merely
equivalent non-canonical bytes are rejected. The old two-column capability TSV
is `legacy-basic`; arbitrary text, malformed JSON, and unknown JSON are
rejected.

Full-evidence restore replays locked leaf URLs into staging and validates
headers, target partitions, JSON evidence fields, confidence levels, byte
counts, and every original SHA256 before replacing an invalid destination.
All manifest record paths are confined below `raw/` and existing symlink
components are rejected before restore. Upstream drift leaves the lock and
existing valid files unchanged. Published
snapshots contain only `raw/query_meta.json`, one
`raw/<taxid>/interactions.tsv` per organism, and `manifest.lock`.

Reactome mapping resolves `current` through the version endpoint with bounded
429/5xx retry, including status 521 and `Retry-After`. Both resolved-current
and explicit versions download from the immutable numeric release directory;
explicit versions do not contact the current-version endpoint.

InterProScan lock additionally requires the official archive and `.md5` pair
and verifies their MD5 relationship before publishing the rebuilt lock. A
fresh lock assigns the fixed versioned EBI HTTPS URLs to both records; it does
not require an older manifest or contact the source. Lock records only those
two declared files and ignores download scratch paths or other undeclared
content under `raw/`.
InterProScan restore uses only the exact snapshot identity, recorded source
URLs, and recorded SHA256 values; it performs no mutable latest-release
lookup.

dbCAN uses the upstream `database` product name and the fixed snapshot
`dbcan/database/db_v5-2-9_5-5-2026`. Its raw layout contains exactly
`CAZy.dmnd`, `dbCAN.hmm`, `dbCAN-sub.hmm`, and
`fam-substrate-mapping.tsv`; the local `dbCAN-sub.hmm` record preserves the
exact remote `dbCAN_sub.hmm` S3 URL. All four files are required, total
7,439,565,906 bytes, and every fetch requires `--allow-large-downloads`.
Neither a subset nor moving `current`, `latest`, or `db_current` identity is a
valid snapshot.

dbCAN fetch publishes `manifest.lock` only after the complete declared set has
passed expected-byte and local SHA256 checks. Lock rejects missing and
undeclared raw files, including pressed-HMM sidecars. Restore validates the
complete identity, `source = "run-dbcan-s3"`, associated run_dbCAN software
`version = "5.2.9"`, exact URLs, byte counts, canonical paths, and SHA256
records before network or raw-file mutation. It never rewrites the authoritative
lock and fails if the pinned URL serves different bytes. The upstream core set
has no SHA/MD5 sidecar; multipart S3 ETags are remote-generation validators,
not content hashes.

KEGG PATHWAY fetch supports concurrency only within one controller process.
Each worker owns one `raw/<organism>/` directory and returns records to the
controller; workers never write `manifest.lock`. The controller merges current
and existing still-present records and atomically checkpoints the unchanged
manifest schema after every completed 32-organism batch. A completed final file
not yet checkpointed is hashed and adopted on the next run. Independent
processes must not write the same PATHWAY snapshot.

Locking always computes SHA256 from the current file bytes; size or mtime is
never accepted as a hash substitute. File hashing uses deterministic ordered
parallelism controlled by `--workers`, with a conservative default of 4.
The option limits concurrent file readers, not directory discovery. Operators
must benchmark higher values against the actual storage system because shared
filesystems can lose throughput under excessive concurrency.

The current production-storage measurement is recorded in the
[CephFS lock and manifest benchmark](../benchmarks/20260717-v1.0-cephfs-lock-and-manifest-benchmark.md).

## Two manifest layers

### Snapshot `manifest.lock`

Each snapshot lock is database-specific but provides a common envelope:

- `database`, `asset`, and `version_token` identify the snapshot;
- optional `dataset` qualifies an asset when the upstream source defines
  multiple datasets under one asset;
- optional `version` records an upstream source version when it differs;
- `downloaded_at` and database-specific source metadata preserve provenance;
- `[[files]]` records each `raw/` path, SHA256, byte count, and source URL when
  available.

Database-specific compound records, such as STRING species and KEGG pathways
or BRITE entries, may remain in the snapshot lock.

### Aggregate manifest

`biofetch manifest build RESOURCE-ROOT` discovers files named exactly
`manifest.lock` using snapshot-aware traversal. A directory containing a
`manifest.lock` is an authoritative, terminal snapshot; `raw/`, `tidy/`,
`logs/`, and directories ending in `.part` or `.part.parts` are non-authority
subtrees and are not traversed. This keeps incomplete payload and staging trees
out of the aggregate scan while retaining arbitrary namespace depth.

The aggregate schema is `biofetch-manifest-v2`. It validates only the common
envelope, aggregates snapshot and file counts, and hashes the lock files
themselves. It does not open, stat, or rehash the large files referenced by
`[[files]]`; their byte totals come from the validated lock records. Child-lock
validation uses bounded ordered workers controlled by `--workers` (default 4,
range 1-64); directory discovery remains deterministic and serial.

The aggregate identity `(database, asset, dataset, version)` is unique, where
`dataset` is the optional typed child-lock qualifier (empty when absent) and
`version` is always the child `version_token`. A differing child `version` is
retained as `source_version`; equal values are omitted. Snapshots are sorted by
`database`, `asset`, `dataset`, `version`, and `path`. Dataset is emitted in
TOML/JSON when present and appears as the `Dataset` column between `Asset` and
`Version` in TSV.

The builder emits v2 only; it does not provide a schema-selection flag or a
parallel v1 writer. Previously published v1 aggregates remain readable as
historical rollback artifacts until every consumer has passed the v2 contract
and a separately authorized publication replaces them.

TOML is the canonical default. JSON and the flattened TSV view are rendered
from the same in-memory model. No generation timestamp is stored, so the same
resource tree and output location produce byte-identical output.

The caller selects an existing output directory with `--output`; generated
names are fixed as `manifest.toml`, `manifest.json`, and `manifest.tsv`.
`manifest.lock` is reserved for authoritative snapshot locks and is never used
for the derived aggregate manifest.

## Path resolution

Before generation, the input root and existing output directory are resolved
to physical paths. `resource_root` is relative to the final manifest's directory.
Each `snapshots.path` is relative to `resource_root`, and
`snapshots.manifest.path` is relative to the snapshot directory.

Moving an aggregate manifest invalidates `resource_root`; regenerate it at the
new location. Changing the output location can therefore change both manifest
content and its SHA256.

## Build and publication flow

1. Fetch or restore all intended snapshots and finish each `manifest.lock`.
2. Confirm every lock has the common envelope, raw-only canonical paths,
   valid SHA256 values, and a final snapshot directory matching
   `version_token`.
3. Run `biofetch manifest build` for all publication formats. The builder
   validates every discovered lock before creating temporary output files.
4. Validate TOML/JSON/TSV agreement and repeat the build to confirm byte
   determinism.
5. Publish the snapshot tree and aggregate manifests together. Multi-format
   replacement is not a filesystem transaction: each completed temporary file
   is atomically renamed individually.
6. Treat cleanup of legacy `tidy/`, subcell, or DMND assets as a separate,
   caller-approved migration after consumer audit.

The maintained local and real-resource release gates are defined in the
[test contract](../testing/20260717-v1.0-test-contract.md).
