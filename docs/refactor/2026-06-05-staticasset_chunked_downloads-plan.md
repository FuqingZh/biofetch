# Static Asset Chunked Downloads Plan

## Goal

Improve large-file download resilience for all databases that use
`internal/shared/staticasset` by adding automatic chunked HTTP Range downloads.

The user-facing CLI should not change. `biofetch` should choose the download
strategy internally:

- use the existing single-connection resumable download for normal files
- use chunked resumable download for large files when the server supports HTTP
  byte ranges
- fall back to single-connection resumable download when chunking is not safe

This is mainly for large assets such as InterPro `protein2ipr.dat.gz`, UniProtKB
TrEMBL FASTA/DAT, UniProt ID mapping, and other multi-GB files.

## Current Baseline

Current shared download behavior:

- files download to `<target>.part`
- if `<target>.part` exists, `httpx.DownloadFileWithResume` sends
  `Range: bytes=<part_size>-`
- if the server returns `206 Partial Content`, the response is appended
- if the server returns `200 OK`, the partial file is overwritten and the file
  restarts from byte 0
- only a fully downloaded file is renamed to the final target
- only final files are hashed and recorded in `manifest.lock`
- `manifest.lock` is flushed every 5 seconds when new successful records exist,
  and flushed on success or failure exit

This avoids restarting from zero after a clean single-stream interruption when
the server supports Range, but it still has one large partial file. If the
single stream repeatedly fails near the end of a multi-GB file, the retry still
depends on continuing that one stream.

## Proposed Behavior

No new public flags.

`staticasset` keeps calling one generic HTTP download function. The strategy is
selected inside `httpx`.

Decision rule:

```text
HEAD/metadata succeeds
AND server supports byte ranges
AND Content-Length >= chunkedDownloadMinBytes
  -> chunked resumable download
otherwise
  -> single resumable download
```

Initial internal constants:

```go
const chunkedDownloadMinBytes = 1 << 30  // 1 GiB
const chunkSizeBytes = 256 << 20         // 256 MiB
const chunkWorkersMax = 4
```

These should stay internal constants in the first version. Do not expose CLI
parameters until there is evidence that users need to tune them.

## Temporary File Layout

For target:

```text
raw/.../protein2ipr.dat.gz
```

single resumable mode keeps the current file:

```text
raw/.../protein2ipr.dat.gz.part
```

chunked mode writes:

```text
raw/.../protein2ipr.dat.gz.parts/
  state.json
  000000.part
  000001.part
  000002.part
  ...
```

After all chunks are complete and validated by size:

```text
raw/.../protein2ipr.dat.gz.parts/* -> merge -> protein2ipr.dat.gz.part
protein2ipr.dat.gz.part -> rename -> protein2ipr.dat.gz
```

The final hash and manifest behavior remains unchanged.

## State File

`state.json` is an internal recovery file, not a public manifest. It can change
without compatibility promises.

Suggested fields:

```json
{
  "url": "https://...",
  "content_length": 16965120833,
  "chunk_size": 268435456,
  "chunks": [
    {"index": 0, "start": 0, "end": 268435455, "size": 268435456, "done": true},
    {"index": 1, "start": 268435456, "end": 536870911, "size": 268435456, "done": false}
  ]
}
```

Reuse rules:

- reuse the state only when URL, content length, and chunk size match
- discard and rebuild the state if any of those fields differ
- consider a chunk done only when its part file exists and has the exact
  expected byte size
- do not trust `done=true` without checking the file size

## Failure Modes

### Server does not support `HEAD`

Fall back to single resumable download.

### Server does not advertise range support

Fall back to single resumable download.

Useful signals:

- `Accept-Ranges: bytes`
- a probe request such as `Range: bytes=0-0` returning `206 Partial Content`

Prefer a small Range probe over trusting `Accept-Ranges` alone if server behavior
is inconsistent.

### Chunk request returns `200 OK`

Treat the server as not supporting reliable Range for this asset and fall back
to single resumable download. Do not append a `200 OK` response into a chunk.

### Chunk request returns unexpected status

Retry according to the existing retry policy. If still failing, return an error.
Keep completed chunks for the next run.

### Partial chunk has wrong size

Delete that chunk and redownload it.

### Merge fails

Return an error. Keep chunk files and state so the next run can validate and
merge again.

### Final hash fails

Return an error. Keep final `.part` and chunk files. The next run may either
resume/validate chunks or restart single mode depending on metadata.

### Disk pressure

Chunked mode can temporarily require both chunk files and the merged `.part`.
This can approach roughly 2x target file size during merge. First version should
document this in code comments and keep chunked mode limited to large files only
when Range is supported. A later version can stream-merge and delete chunks as
they are copied.

## Progress Display

No new CLI output mode.

Map chunked progress into the existing progress callback:

```text
bytesDone = sum(completed chunk sizes + active chunk bytes)
bytesTotal = content length
```

The existing `staticasset` progress display should then continue to show current
file byte progress.

## Trace / Manifest

No manifest schema change in the first version.

Manifest should keep recording the final file only:

- asset name
- relative path
- URL
- bytes
- SHA256

Optional future trace events can include:

- `download_strategy=chunked`
- `chunks_total`
- `chunks_reused`
- `chunks_downloaded`

Do not expand the public manifest schema for this first implementation.

## Implementation Scope

### PR 1: HTTP Metadata and Strategy Selection

Files:

- `internal/shared/httpx/httpx.go`
- `internal/shared/httpx/httpx_test.go`

Add:

- metadata probe for content length and range support
- strategy selector
- tests for:
  - small file uses single mode
  - large file without Range uses single mode
  - large file with Range chooses chunked mode
  - `HEAD` failure falls back safely

No `staticasset` behavior change beyond calling the unified function if needed.

### PR 2: Chunk State and Chunk Download

Files:

- `internal/shared/httpx/httpx.go`
- `internal/shared/httpx/httpx_test.go`

Add:

- chunk plan builder
- state load/write
- chunk validation by exact size
- per-chunk Range download
- bounded chunk worker pool, internally capped at `chunkWorkersMax`
- tests for:
  - completed chunks are reused
  - incomplete chunk is redownloaded
  - 206 response writes only the requested range
  - 200 response from a chunk request falls back to single mode or errors before
    corrupting chunk files

### PR 3: Merge and Staticasset Integration

Files:

- `internal/shared/httpx/httpx.go`
- `internal/shared/httpx/httpx_test.go`
- `internal/shared/staticasset/staticasset.go`
- `internal/shared/staticasset/staticasset_test.go`

Add:

- merge completed chunks into the existing `.part` target
- keep final rename/hash/manifest logic unchanged in `staticasset`
- clean up chunk state only after final file is successfully renamed and hashed,
  or leave cleanup as a later explicit task if keeping chunks is safer
- tests for:
  - chunked download produces the exact final bytes
  - interrupted first run keeps completed chunks
  - second run completes missing chunks and merges
  - final `manifest.lock` records only the final asset

## Verification

Unit tests:

```bash
/usr/local/go/bin/go test ./internal/shared/httpx ./internal/shared/staticasset
/usr/local/go/bin/go test ./...
```

Manual smoke, after implementation:

```bash
./dist/biofetch interpro mapping fetch \
  --dir_out /tmp/biofetch-interpro-smoke \
  --should_allow_large_assets \
  --workers_max 1 \
  --should_allow_insecure_tls
```

For a non-destructive real large-file resume test, interrupt after chunks appear,
rerun the same command, and verify:

- existing chunk files are reused
- no full restart from byte 0 occurs when the server supports Range
- final file hash is written to `manifest.lock`

## Non-Goals

- no public CLI flags for chunk size or chunk workers in the first version
- no manifest schema changes
- no per-database special cases
- no multipart uploads or remote checksum validation
- no decompression or content-level validation
- no replacement of `--workers_max`; chunk workers are internally capped and
  separate from asset-level workers

