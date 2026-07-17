# Biofetch resource and manifest contract

Version: v1.1
Date: 2026-07-17
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

A published version token is immutable. If upstream content changes, publish a
new version token, rebuild its lock, and regenerate every aggregate manifest
that references the resource tree. Do not silently replace files behind a
published lock.

### Rebuilding a snapshot lock

Every database-specific `lock` command accepts one required
`--dir_snapshot`, pointing directly at the existing directory that contains
`raw/` and `manifest.lock`. The final directory segment is the authoritative
`version_token`; database-specific token formats are validated after deriving
the value from the path. `lock` does not accept `--dir_out` or `--version` and
does not discover a latest snapshot.

Existing source metadata may be preserved while rebuilding, but an existing
lock must never override the directory-derived `version` or `version_token`.
Fetch and sync retain their root-directory and version selectors because they
create or restore a snapshot rather than rebuild one already identified by its
directory.

Locking always computes SHA256 from the current file bytes; size or mtime is
never accepted as a hash substitute. File hashing uses deterministic ordered
parallelism controlled by `--workers_max`, with a conservative default of 4.
The option limits concurrent file readers, not directory discovery. Operators
must benchmark higher values against the actual storage system because shared
filesystems can lose throughput under excessive concurrency.

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

`biofetch manifest build` recursively discovers files named exactly
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

## Path resolution

Before generation, the input root and output parent directory are resolved to
physical paths. `resource_root` is relative to the final manifest's directory.
Each `snapshots.path` is relative to `resource_root`, and
`snapshots.manifest.path` is relative to the snapshot directory.

Moving an aggregate manifest invalidates `resource_root`; regenerate it at the
new location. Changing the output location can therefore change both manifest
content and its SHA256.

## Build and publication flow

1. Fetch or sync all intended snapshots and finish each `manifest.lock`.
2. Confirm every lock has the common envelope, raw-only canonical paths,
   valid SHA256 values, and a resource path containing `version_token` as a
   complete directory segment.
3. Run `biofetch manifest build` for all publication formats. The builder
   validates every discovered lock before creating temporary output files.
4. Validate TOML/JSON/TSV agreement and repeat the build to confirm byte
   determinism.
5. Publish the snapshot tree and aggregate manifests together. Multi-format
   replacement is not a filesystem transaction: each completed temporary file
   is atomically renamed individually.
6. Treat cleanup of legacy `tidy/`, subcell, or DMND assets as a separate,
   caller-approved migration after consumer audit.

The release gate is `go test ./...`, `go vet ./...`, a built-binary CLI smoke,
and a read-only aggregate build against the real resource tree with outputs in
temporary storage.
