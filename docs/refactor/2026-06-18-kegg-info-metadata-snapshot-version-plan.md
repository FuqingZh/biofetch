# KEGG Info Metadata and Snapshot Version Plan

Version: v1.0
Date: 2026-06-18
Status: current

## Goal

Keep KEGG `--version` semantics aligned with the current CLI contract:
`--version` is a local snapshot key in `YYYY-MM` form, not a KEGG official
release selector.

KEGG live API downloads should not fail only because `/info/<database>` no
longer contains a `Release ...` line. The `/info` response is useful metadata,
but it is not the source of truth for local snapshot naming.

## Current Problem

`pathway`, `brite`, and `catalog` fetches call `resolveKEGGVersion` before
creating the version directory and again before writing the manifest. That
function requires `/info/<database>` to contain `Release ...`, then parses a
major release from it.

Current KEGG pathway info can instead contain:

```text
Last update 2026/06/12
```

With no `Release ...` line, explicit commands such as:

```bash
biofetch kegg pathway fetch --dir_out /data/kegg --version 2026-04 --organisms hsa
```

fail before the actual requested snapshot download can proceed.

## Contract

- Explicit `--version 2026-04` means use `2026-04` as the local snapshot
  directory and manifest `version_token`.
- Omitted `--version` means derive the local snapshot key from the command
  start time, e.g. `2026-06`.
- KEGG `/info/<database>` is optional provenance metadata.
- `Release ...` remains supported when KEGG returns it.
- `Last update YYYY/MM/DD` is parsed as upstream metadata when present.
- Missing or malformed `/info` metadata should warn and continue for fetch
  commands; it should not block an otherwise valid explicit snapshot request.

## Implementation Steps

1. Split KEGG info probing from local snapshot version selection.
2. Add parser support for `Last update YYYY/MM/DD`.
3. Preserve existing `source_release`, `source_release_start`, and
   `source_release_end` fields for older manifests and old KEGG info output.
4. Add optional `source_last_update`, `source_last_update_start`, and
   `source_last_update_end` manifest fields for the new info format.
5. Update `pathway`, `brite`, and `catalog` fetches to warn and continue when
   `/info` cannot provide metadata.
6. Add tests for old `Release ...`, new `Last update ...`, missing info
   metadata, and explicit-version fetch behavior under new info output.

## Acceptance

- `biofetch kegg pathway fetch --version 2026-04 ...` no longer depends on a
  KEGG `Release ...` line.
- The manifest still records source release metadata when available.
- The manifest records source last-update metadata when available.
- `go test ./internal/kegg` passes.
- `go test ./...` passes.
