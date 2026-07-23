# Biofetch resource and manifest contract

Version: v1.2
Date: 2026-07-23
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
source URL. A verification failure retains `.part` but publishes neither a
final archive nor a false archive record. Biofetch does not extract or install
the distribution.

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

InterProScan lock additionally requires the official archive and `.md5` pair
and verifies their MD5 relationship before publishing the rebuilt lock.
InterProScan restore uses only the exact snapshot identity, recorded source
URLs, and recorded SHA256 values; it performs no mutable latest-release
lookup.

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
- optional `version` records an upstream source version when it differs;
- `downloaded_at` and database-specific source metadata preserve provenance;
- `[[files]]` records each `raw/` path, SHA256, byte count, and source URL when
  available.

Database-specific compound records, such as STRING species and KEGG pathways
or BRITE entries, may remain in the snapshot lock.

### Aggregate manifest

`biofetch manifest build RESOURCE-ROOT` recursively discovers files named exactly
`manifest.lock`. It validates only the common envelope, aggregates snapshot and
file counts, and hashes the lock files themselves. It does not open, stat, or
rehash the large files referenced by `[[files]]`; their byte totals come from
the validated lock records.

The aggregate identity `(database, asset, version)` is unique, where
`version` is always the child `version_token`. A differing child `version` is
retained as `source_version`; equal values are omitted. Snapshots are sorted by
`database`, `asset`, `version`, and `path`.

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
