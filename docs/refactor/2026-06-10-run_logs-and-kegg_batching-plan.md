# Run Logs and KEGG Batching Plan

## Goal

Add a shared run-log capability for database download commands and improve KEGG
PATHWAY full-organism execution so users get better traceability and less
front-loaded waiting.

This plan also updates KEGG PATHWAY entry failure policy:

- `pathway.entry` `403` should retry by `retry_max` / `retry_wait_sec`
- after retries are exhausted, log a warning and continue instead of failing
  the whole run
- `pathway.kgml`, `pathway.conf`, and `pathway.image` keep `403/404`
  unavailable-skip behavior

## Logging Contract

Use a directory option, not a file option:

- CLI flag: `--dir_logs`
- if omitted, default log directory is `<version>/logs/`
- each command run writes one new file

Filename template:

```text
<action>-<YYYYMMDD>T<HHMMSS>Z-<short_runid>.log
```

Examples:

```text
fetch-20260610T105033Z-a1b2c3d4.log
sync-20260610T105033Z-9f8e7d6c.log
lock-20260610T105033Z-12ab34cd.log
```

The version directory already carries database / asset context, so filenames do
not repeat `kegg-pathway-` or similar prefixes.

## PR 1: Shared Run Logger

### Scope

Extend `internal/shared/logx` to support one per-run file logger that can
mirror output to terminal and file.

Requirements:

- default terminal behavior stays unchanged when no run logger is configured
- command code can initialize a run logger after resolving its version
  directory
- file creation auto-creates the target log directory
- logger close is explicit and safe to call multiple times

### Acceptance

- shared `logx` supports terminal + file dual write
- default filename generation follows the template above
- default log directory can be derived from a version directory
- unit tests cover filename format and file creation

## PR 2: All Database Commands Support `--dir_logs`

### Scope

Add `--dir_logs` to database `fetch`, `lock`, and `sync` commands and wire them
to the shared run logger.

Databases in scope:

- GO ontology / slim
- WikiPathways GMT
- Reactome mapping
- InterPro mapping
- UniProt KB / UniRef / ID mapping
- eggNOG
- KEGG mapping / pathway / brite / catalog
- OmniPath
- STRING
- Subcell

Behavior:

- if `--dir_logs` is set, write logs there
- if not set, write to `<version>/logs/`
- terminal output remains visible

### Acceptance

- a normal fetch creates exactly one log file
- log file path is stable and discoverable
- `lock` and `sync` also write run logs
- no command loses existing terminal output

## PR 3: KEGG PATHWAY Progress and Soft Entry Failure

### Scope

Improve KEGG PATHWAY observability and resilience:

- keep the existing local planning summary
- add clearer scan/progress messages for batch execution
- if `pathway.entry` still returns `403` after retries, log a warning and
  continue instead of aborting the whole run
- skipped entry failures must not be written into the manifest as successful
  assets
- accumulate a failure summary in the log

### Acceptance

- exhausted entry `403` produces warning logs and run summary counts
- other unexpected entry failures still error
- side assets keep unavailable-skip behavior
- tests cover exhausted entry `403` continue behavior

## PR 4: KEGG PATHWAY Full-Organism Internal Batching

### Scope

Change full-organism fetch from one giant expand-then-download pass to internal
streaming batches.

Target flow:

1. resolve all organism codes
2. split into internal batches
3. for each batch:
   - fetch pathway lists
   - expand tasks only for that batch
   - inspect local files
   - download assets
   - flush progress and warning counts

Initial version should use a fixed internal batch size. Do not expose a new CLI
flag until measurements show a clear need.

### Acceptance

- full-organism runs start downloading earlier
- planning memory and task expansion are bounded by batch size
- manifest result remains equivalent to non-batched execution
- logs summarize batch counts and skipped entry failures

## Verification

Required checks:

```bash
/usr/local/go/bin/go test ./internal/shared/logx ./internal/kegg/...
/usr/local/go/bin/go test ./...
```

Smoke checks:

- one representative command from `staticasset` flow creates a log file
- one KEGG PATHWAY full-organism dry-run shows batched progress
- one KEGG PATHWAY run with forced entry `403` records warning + continue
