# Repository agent guide

Use [docs/README.md](docs/README.md) as the map to current documentation
authority. Set up Go from [go.mod](go.mod), then run the canonical local gate:

```bash
GOCACHE=/tmp/biofetch-test go test ./...
GOCACHE=/tmp/biofetch-test go vet ./...
GOCACHE=/tmp/biofetch-test go build -o /tmp/biofetch ./cmd/biofetch
```

The public CLI contract tests are in
[`internal/biofetch/cli_test.go`](internal/biofetch/cli_test.go). Default tests
must use temporary resources and must not contact live upstream services or
write to CephFS. Treat real resource trees as read-only; CephFS writes require
explicit task scope. See the
[test contract](docs/testing/20260717-v1.0-test-contract.md) for the maintained
validation and real-resource boundaries.

## AO delivery

This repository has opted into the accepted user-level AO service as
`biofetch`. For conversation-authorized implementation intended to cross a
pull-request boundary, verify AO health and start a task-specific worker before
creating the implementation branch or PR. If a PR already exists, mark it ready
for review if it is a draft, then restore its owning worker or claim it with
`--no-takeover`. Ready-for-review is only an AO claim prerequisite. If AO is
unavailable, use an isolated worktree and report that fallback. Merge,
auto-merge, and risk-acceptance decisions remain with the user.
