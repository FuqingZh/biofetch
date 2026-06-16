# GO Annotation Download Agent Chain

Version: v1.0
Date: 2026-06-15
Status: proposed

## Goal

Add a simple GO annotation download feature that fits the existing
`biofetch go` command family and reuses the shared `staticasset` download
kernel.

Preferred command shape:

```text
biofetch go annotation fetch
biofetch go annotation lock
biofetch go annotation sync
```

This should not be a new top-level `biofetch goa` command. GOA is a source /
content family under GO annotations, while the repository already owns GO
assets under `internal/geneontology`.

## Design Boundary

Keep the first implementation narrow:

- download raw official annotation files only
- write `manifest.lock`
- support `fetch`, `lock`, and `sync`
- support run logs via `--dir_logs`
- do not parse GAF, GPAD, or GPI contents
- do not normalize gene-product identifiers
- do not build enrichment-ready tidy tables
- do not add species-name search in the first pass

Downstream parsing and canonical tables belong in a later `biotidy` or build
stage.

## Source Contract

Initial source:

```text
https://current.geneontology.org/annotations/
https://release.geneontology.org/<version>/annotations/
```

Asset files use this shape:

```text
<dataset>.<format>.gz
```

Examples:

```text
goa_human.gaf.gz
goa_human.gpad.gz
goa_human.gpi.gz
filtered_goa_uniprot_all.gaf.gz
filtered_goa_uniprot_all_noiea.gaf.gz
mgi.gaf.gz
sgd.gaf.gz
```

Supported formats in the first pass:

```text
gaf,gpad,gpi
```

Default policy:

- default formats: `gaf`
- default datasets: none, require `--datasets`
- allow `@file` input for `--datasets` and `--formats`
- validate dataset names as file stems, not as free-form URLs
- use GO release date tokens such as `2026-01-23`
- omitted `--version` resolves current by reading the current GO ontology
  release, matching `go ontology` and `go slim`

Requiring `--datasets` avoids accidentally downloading very large global
annotation files.

## Manifest Contract

Use `staticasset.Source`:

```go
staticasset.Source{
    Database: "go",
    Asset: "annotation",
    Source: "geneontology",
    Version: source.version,
    VersionToken: source.versionToken,
    Scope: staticasset.Scope{
        Type: "datasets_formats",
        Value: strings.Join(datasets, ",") + "|" + strings.Join(formats, ","),
    },
    Assets: assets,
}
```

Output layout:

```text
<dir_out>/annotation/<version>/raw/<dataset>.<format>.gz
<dir_out>/annotation/<version>/manifest.lock
<dir_out>/annotation/<version>/logs/<action>-<timestamp>-<runid>.log
```

Keep paths flat under `raw/` in the first pass. Do not invent species
directories until there is a real lookup contract.

## Files To Touch

Expected implementation files:

```text
internal/geneontology/cli.go
internal/geneontology/annotation.go
internal/geneontology/annotation_test.go
```

Likely no changes needed:

```text
internal/shared/staticasset/staticasset.go
internal/shared/logx/logx.go
internal/biofetch/cli.go
```

The root CLI already registers `geneontology.NewCommand()`. The new command
should be registered inside `internal/geneontology.NewCommand()`.

## Goal Mode Prompt

Use this as the top-level goal:

```text
Implement GO annotation raw asset downloads in /home/being/projects/biofetch.

Add `biofetch go annotation fetch|lock|sync` under the existing
`internal/geneontology` command family. Reuse `staticasset` and the existing
GO ontology version-resolution pattern. Keep the first pass narrow: raw file
download, manifest.lock, lock/sync, run logs, tests, and CLI examples. Do not
parse GAF/GPAD/GPI content or add species lookup.

Acceptance:
- `fetch` supports `--dir_out`, `--version`, `--datasets`, `--formats`,
  `--rule_existing`, retry/download/TLS/dry-run/progress flags, and
  `--dir_logs`.
- `lock` and `sync` work for fixed versions.
- omitted `--version` resolves the current GO release date through the
  existing ontology version method.
- files are stored as `annotation/<version>/raw/<dataset>.<format>.gz`.
- manifest identity is `database=go`, `asset=annotation`,
  `source=geneontology`.
- tests cover dataset/format resolution, URL building, current source
  resolution, fetch manifest writing, and CLI help.
- run `/usr/bin/env GOCACHE=/tmp/biofetch-go-build /usr/local/go/bin/go test ./...`.
```

## Subagent Chain

Use subagents for read-heavy work and review. Keep code writes single-owner.

### Agent 1: Source Contract Explorer

Purpose: confirm source URLs, file naming, and release/current behavior.

Prompt:

```text
Explore the GO annotation download source contract for this repo. Read the
existing `internal/geneontology` implementation and official GO annotation
download pages if network is available. Return only a concise contract:
current URL, release URL, file naming pattern, formats to support first, and
any risks. Do not edit files.
```

Expected output:

- source URLs
- examples of valid files
- version-resolution recommendation
- risk notes for large assets and current vs release

### Agent 2: Existing Pattern Explorer

Purpose: map the local implementation pattern before writing code.

Prompt:

```text
Read `internal/geneontology/cli.go`, `slim.go`, `ontology.go`, their tests,
and `internal/shared/staticasset/staticasset.go`. Summarize the exact pattern
to follow for a new `go annotation` staticasset command. Include config
structs, validation helpers, source building, lock/sync shape, and tests to
mirror. Do not edit files.
```

Expected output:

- file/function map
- reusable helpers
- places to register commands
- test patterns to copy

### Main Agent: Implementation Owner

Purpose: make all code edits, keep staging coherent, and run tests.

Steps:

1. Add annotation config structs and command registration in `cli.go`.
2. Add `annotation.go` with source resolution, dataset/format resolution,
   asset building, fetch/lock/sync, and options builder.
3. Add tests in `annotation_test.go`.
4. Run `gofmt`.
5. Run focused tests:

```bash
/usr/bin/env GOCACHE=/tmp/biofetch-go-build /usr/local/go/bin/go test ./internal/geneontology
```

6. Run full tests:

```bash
/usr/bin/env GOCACHE=/tmp/biofetch-go-build /usr/local/go/bin/go test ./...
```

### Agent 3: Reviewer

Purpose: review the final diff after implementation.

Prompt:

```text
Review the GO annotation implementation diff in /home/being/projects/biofetch.
Focus on correctness, CLI compatibility, manifest identity, source URL
semantics, missing tests, and accidental overreach into parsing or downstream
tidy behavior. Return findings with file references. Do not edit files.
```

Expected output:

- blocking issues first
- test gaps
- any source-contract ambiguity
- final go/no-go recommendation

## Commit Chain

If committing this work, use two small commits:

1. `Add GO annotation download commands`
   - `internal/geneontology/cli.go`
   - `internal/geneontology/annotation.go`
   - `internal/geneontology/annotation_test.go`

2. `Document GO annotation download plan`
   - this document, if it changes during execution

Before each commit:

```bash
git diff --cached --stat
```

## Acceptance Checklist

- `biofetch go annotation --help` lists `fetch`, `lock`, and `sync`.
- `fetch --should_dry_run` resolves current version without writing files.
- `fetch` writes one manifest for a test server fixture.
- `lock` rebuilds a manifest from `raw/`.
- `sync` reads `manifest.lock` and reuses/downloads files through
  `staticasset`.
- `--dir_logs` writes a run log under `<version>/logs/` by default.
- invalid dataset names are rejected.
- invalid formats are rejected.
- `@file` expansion works for datasets and formats.
- full test suite passes.

## Codex Subagent Note

Codex subagent workflows are enabled by default in current Codex releases and
only run when explicitly requested. They are best used for parallel exploration,
tests, triage, and review. Avoid parallel write-heavy edits for this task.

As of the current Codex manual fetched on 2026-06-15, subagent activity is
surfaced in the Codex app and CLI, while IDE extension visibility is listed as
coming soon. The VS Code extension uses the same agent and shared configuration
as the CLI, but do not assume the IDE UI can fully inspect and manage subagent
threads yet.
