# Download Unification PR Plan

Status: archived; not an active implementation plan

## Goal

Make database downloads behave consistently across `biofetch`:

- explicit safety flags are enough; do not require extra literal input
- old custom download paths should reuse the shared resumable HTTP downloader
- existing directory and manifest contracts should remain stable
- large-download safety flags should stay in place

This plan intentionally separates "download transport unification" from a full
manifest rewrite. KEGG PATHWAY / BRITE / catalog still have richer custom
manifests, so the first pass should update their file transfer behavior without
forcing them into `staticasset.Manifest`.

## Current Split

Already using `internal/shared/staticasset`:

- GO Slim
- WikiPathways GMT
- Reactome mapping
- InterPro mapping
- UniProt KB / UniRef / ID mapping
- eggNOG COG / mapper
- KEGG mapping
- Subcell UniProt

Still using custom download logic:

- KEGG PATHWAY
- KEGG BRITE
- KEGG catalog
- STRING
- OmniPath
- GO ontology

## PR 1: Remove Literal Confirmation

### Scope

Remove interactive literal confirmation for explicit safety flags.

Keep these flags:

- `--should_download_all_organisms`
- `--should_allow_large_assets`

Remove only the extra prompt that asks users to type the flag name again.

### Affected Commands

- `kegg mapping fetch --should_download_all_organisms`
- `kegg pathway fetch --should_download_all_organisms`
- `kegg brite fetch --should_download_all_organisms`
- `wikipathways gmt fetch --should_download_all_organisms`
- `stringdb fetch --should_download_all_organisms`
- `omnipath ... --should_download_all_organisms`
- GO ontology all-assets confirmation

### Acceptance

- commands with explicit safety flags run non-interactively
- validation still rejects ambiguous source combinations
- large asset flags still protect multi-GB downloads
- tests no longer assert literal prompt behavior

## PR 2: KEGG PATHWAY Download Transport

### Scope

Keep the KEGG PATHWAY manifest schema and directory layout. Replace direct
in-memory file downloads for pathway entry assets with resumable file downloads.

Use shared transport:

```go
httpx.DownloadFileWithResume(...)
```

Preserve current behavior:

- list content can still be downloaded into memory because it is small and also
  used for ID parsing
- per-pathway `entry`, `kgml`, `conf`, and `image` files use resumable `.part`
  download
- 404 for a per-pathway file is skipped, not written to manifest
- existing file reuse by `--rule_existing skip` stays unchanged

### Acceptance

- interrupted `.part` files can resume
- chunked mode can be selected automatically for large files when applicable
- missing per-pathway 404 assets do not fail the whole fetch
- `go test ./internal/kegg/...` passes

## PR 3: KEGG BRITE and Catalog Download Transport

### Scope

Keep custom manifests and layouts, but use shared resumable transport for raw
file writes:

- BRITE entry and JSON assets
- KEGG catalog raw file
- catalog / pathway / brite sync rehydration where practical

Small list files may remain in memory when they are parsed immediately.

### Acceptance

- raw file downloads use `.part` + resumable/automatic chunked transport
- existing file reuse still works
- no public CLI change beyond removed confirmations
- `go test ./internal/kegg/...` and `go test ./...` pass

## PR 4: STRING Download Transport

### Scope

Keep the STRING manifest schema and directory layout. Replace direct raw file
downloads with shared resumable downloads.

Use shared transport:

```go
httpx.DownloadFileWithResume(...)
```

Preserve current behavior:

- species catalog resolution remains in memory because it is small and parsed
  immediately
- `protein.links`, `protein.aliases`, and `protein.info` raw files download to
  `.part` first, then atomically rename into place
- existing file reuse by `--rule_existing skip` stays unchanged
- `sync` uses the same task runner and therefore receives the same resumable
  transport

### Acceptance

- interrupted `.part` files can resume
- chunked mode can be selected automatically for large files when applicable
- existing manifest schema and paths remain unchanged
- `go test ./internal/stringdb/...` and `go test ./...` pass

## Deferred

The following are intentionally left for later PRs:

- full migration of KEGG PATHWAY / BRITE / catalog to `staticasset.Manifest`
- OmniPath transport unification
- GO ontology transport unification
- cleanup of the now-unused `internal/shared/confirm` package after all
  consumers are removed
