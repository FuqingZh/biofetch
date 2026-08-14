# Contributing to biofetch

## Scope

Changes should preserve the boundary documented in
[`docs/architecture/resource-manifest-contract.md`](docs/architecture/resource-manifest-contract.md):
biofetch acquires and locks raw upstream snapshots; normalization and derived
tables belong elsewhere.

For a new source, include the official URL, release/version semantics, size,
authentication behavior, license/terms link, citation, retry constraints, and
the intended `manifest.lock` layout. Do not add a browser workaround that
bypasses authentication or redistribute upstream bytes.

## Local setup and gate

Use the Go version declared by `go.mod`. Before opening a pull request, run:

```bash
GOCACHE=/tmp/biofetch-test go test ./...
GOCACHE=/tmp/biofetch-test go vet ./...
GOCACHE=/tmp/biofetch-race go test -race ./...
GOCACHE=/tmp/biofetch-test go build -trimpath -o /tmp/biofetch ./cmd/biofetch
go mod tidy -diff
go mod verify
python3 scripts/generate_third_party_notices.py --check
```

Tests use temporary resources and must not contact live providers or write
CephFS. Add deterministic fixtures for source definitions, HTTP failures,
manifest identity, and lock/restore behavior. A large-download or live smoke
test belongs in a bounded workflow, not pull-request tests.

## Pull requests

Keep one concern per pull request, describe compatibility impact, link source
terms, and report the exact commands run. Do not include database archives,
credentials, cookies, or internal shared-storage paths. The required check is
`validate-biofetch`; resolve review threads and re-run the current-head gate
after every change.
