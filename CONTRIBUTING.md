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

Use the Go toolchain declared by `go.mod` (currently `go1.26.9`).
Follow the [development and release contract](docs/development/20261020-v1.0-development-and-release-contract.md).
For ordinary changes, run tests and vet for the affected package and direct
consumers; use scoped race checks when concurrency or shared download/hash
behavior changes. Documentation-only work needs link, command and authority
checks, without unrelated Go regression.

Before publishing a release candidate, run the complete local gate once:

```bash
GOCACHE=/tmp/biofetch-test go test ./...
GOCACHE=/tmp/biofetch-test go vet ./...
GOCACHE=/tmp/biofetch-race go test -race ./...
GOCACHE=/tmp/biofetch-test go build -trimpath -o /tmp/biofetch ./cmd/biofetch
go mod tidy -diff
go mod verify
python3 scripts/generate_third_party_notices.py --check
go run honnef.co/go/tools/cmd/staticcheck@2025.1.1 ./...
go run golang.org/x/vuln/cmd/govulncheck@v1.4.0 ./...
```

Tests use temporary resources and must not contact live providers or write
CephFS. Add deterministic fixtures for source definitions, HTTP failures,
manifest identity, and lock/restore behavior. A large-download or live smoke
test belongs in a bounded workflow, not pull-request tests.

## Pull requests

Keep one concern per pull request, describe compatibility impact, link source
terms, and report the exact commands run. Do not include database archives,
credentials, cookies, or internal shared-storage paths. The required check is
`validate-biofetch`; resolve review threads and verify the required remote
checks against the current head immediately before merging. New code or
failures require relevant local revalidation; unchanged source does not require
another full local suite just because the PR, tag, or release state changed.

Development is direct; AO sessions and claims are not prerequisites.
Merge, release and shared-resource writes follow the user's explicit scope.
Public releases contain software, notices, checksums, SBOM and provenance;
database acquisition happens separately against official providers.
