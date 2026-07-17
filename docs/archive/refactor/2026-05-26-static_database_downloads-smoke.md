# Static Database Downloads Smoke Evidence

Status: archived; not an active validation contract

## Scope

This file records PR 6 smoke evidence for the static database download series.
The default smoke path is offline-safe: it avoids live upstream access unless a
developer explicitly chooses a live-download run.

## Commands Run

Full test suite:

```bash
go test ./...
```

CLI command-surface smoke:

```bash
go run ./cmd/biofetch go slim fetch --help
go run ./cmd/biofetch wikipathways gmt fetch --help
go run ./cmd/biofetch reactome mapping fetch \
  --dir_out /tmp/biofetch-smoke/reactome \
  --assets ReactomePathways.txt \
  --should_dry_run
go run ./cmd/biofetch subcell uniprot fetch \
  --dir_out /tmp/biofetch-smoke/subcell \
  --taxids 9606 \
  --should_dry_run
```

Dry-run filesystem check:

```bash
find /tmp/biofetch-smoke -maxdepth 4 -type f -o -type d
```

Result: `/tmp/biofetch-smoke` was not created by the dry-run commands.

## Verified Command Surface

`biofetch go slim fetch --help` exposes:

- `--dir_out`
- `--version`
- `--subsets`
- `--formats`
- `--rule_existing`
- retry, worker, request interval, TLS, and dry-run flags

`biofetch wikipathways gmt fetch --help` exposes:

- `--dir_out`
- `--version`
- `--species`
- `--should_download_all_organisms`
- `--rule_existing`
- retry, worker, request interval, TLS, and dry-run flags

Reactome and Subcell dry-run commands returned success and did not create output
files.

## Fixture Evidence

The implementation uses local `httptest.Server` fixtures for network-dependent
behavior:

| Dataset | Fixture coverage |
| --- | --- |
| Reactome | registry validation, URL construction, HEAD content length, large-file guard, fetch, sync, second-run reuse |
| WikiPathways | current GMT index parsing, species selection, historical-version rejection, all-species confirmation, fetch, sync, second-run reuse |
| Subcell UniProt | scope validation, stream URL construction, protein-location parsing, normalization, fetch, sync |

## Deviations Recorded

The original smoke plan listed dry-run fetches for all new commands. In the
current implementation, GO Slim and WikiPathways dry-run still perform source
resolution before planning:

- GO Slim resolves the GO release version from the ontology OBO header.
- WikiPathways parses the current GMT index to know available species and the
  release token.

Because those actions require live upstream HTTP, PR 6 uses help smoke for those
two command surfaces and relies on local fixture tests for source resolution and
download behavior. This preserves offline repeatability of the default test
gate. Live smoke can be run separately when upstream access is intended.

## Live Smoke Commands

Run these only when live upstream HTTP is allowed:

```bash
go run ./cmd/biofetch go slim fetch \
  --dir_out /tmp/biofetch-live/go \
  --subsets goslim_generic \
  --formats obo \
  --should_dry_run

go run ./cmd/biofetch wikipathways gmt fetch \
  --dir_out /tmp/biofetch-live/wikipathways \
  --species Homo_sapiens \
  --should_dry_run
```

Reactome live downloads can be large. Use `--should_dry_run` first and pass
`--should_allow_large_assets` only when the requested files and disk impact
are intentional.
