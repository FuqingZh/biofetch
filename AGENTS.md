# Repository agent guide

Use [docs/README.md](docs/README.md) as the map to current documentation
authority. Use the toolchain declared in [go.mod](go.mod). The current
[development and release contract](docs/development/20261020-v1.0-development-and-release-contract.md)
owns development mode, proportional verification, integration, and publication.
Start with the changed package and its consumers. Run the release gate once
for a release candidate; do not repeat full regression after unrelated prose,
polling, or delivery bookkeeping. Required remote checks remain mandatory.

The public CLI contract tests are in
[`internal/biofetch/cli_test.go`](internal/biofetch/cli_test.go). Default tests
must use temporary resources and must not contact live upstream services or
write to CephFS. Treat real resource trees as read-only; CephFS writes require
explicit task scope. See the
[test contract](docs/testing/20260717-v1.0-test-contract.md) for the maintained
validation and real-resource boundaries.

Public repository identity, release artifacts, upstream-data terms, and
security/contribution entrypoints are governed by
[the publication readiness plan](docs/implementation-plan/20260813-v1.0-github-publication-readiness-implementation-plan.md).
Release archives contain software and notices only; never add database
payloads or credentials.

## Direct development and delivery

Use direct Codex development. Do not start, restore, or claim AO workers or
orchestrators as a prerequisite for branches, PRs, CI, or releases. Historical
AO adoption records are provenance and do not override this decision.
Delegation requires an explicit user request.

Preserve existing edits and other owners' worktrees. Use a task branch and an
isolated worktree when the maintained checkout contains unrelated work.
An authorized takeover requires writer revocation and runtime verification;
session termination alone does not prove its children exited.

The current coordinator owns implementation, same-scope review fixes,
validation and delivery. Follow the user's existing authorization through
completion; implementation alone does not authorize a release, merge or
shared-storage write. When those actions are authorized, verify required
checks for the exact head and read back the published result.

Update current owning documentation. Keep historical plans and acceptance
records as provenance, and do not rewrite old pending statuses as current
facts. Report unverified boundaries explicitly.
