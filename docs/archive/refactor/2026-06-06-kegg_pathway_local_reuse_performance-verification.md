# KEGG Pathway Local Reuse Performance Verification

Status: archived; not an active validation contract

## Scope

This note records how to verify the local planning optimization for repeated
`biofetch kegg pathway fetch` runs over a mostly complete local snapshot.

The optimization targets local reuse planning only:

- manifest-first reuse avoids re-hashing unchanged files
- directory indexes avoid per-missing-file `os.Stat`
- local inspect is parallel and deterministic
- KEGG REST downloads remain rate-limited by the existing client

## Measurement Template

Record the following for a realistic `pathway/<version>` directory:

```text
command:
version:
scope:
assets:
filesystem:
expected_assets:
reused_by_manifest:
rebuilt_by_hash:
scheduled_for_download:
local_planning_elapsed_before:
local_planning_elapsed_after:
download_elapsed:
notes:
```

## Expected Result

For a mostly complete snapshot, repeated fetch should report most existing
assets under `reused_by_manifest`, with only changed or manifest-missing files
falling back to hash rebuild.

For an incomplete snapshot, missing files should be scheduled for download
without one filesystem stat per expected missing file.

## Regression Checks

```bash
/usr/local/go/bin/go test ./internal/kegg/...
/usr/local/go/bin/go test ./...
```

## Failure Handling

- corrupt `manifest.lock`: fail while reading the manifest instead of silently
  trusting it
- missing `manifest.lock`: fall back to file inspection and hash rebuild
- size mismatch: fall back to hash rebuild
- partial `.part`: ignored by local reuse planning and handled by resumable
  download if the asset is scheduled
- upstream per-pathway 404: still skipped and not written to manifest
