# Biofetch documentation

This directory contains the maintained contracts and historical design record
for `biofetch`.

## Current authority

Read these documents in order:

1. [Root README](../README.md) for the product boundary and common commands.
2. [Resource and manifest contract](architecture/resource-manifest-contract.md)
   for snapshot layout, lock semantics, aggregate manifests, and path rules.
3. [Development and release contract](development/20261020-v1.0-development-and-release-contract.md)
   for direct development, proportional checks, exact-head integration and
   publication. It supersedes historical AO adoption and first-release
   pending statements.
4. [KEGG PATHWAY sharded and bounded download implementation plan](implementation-plan/20260820-v1.0-kegg-pathway-sharded-download-implementation-plan.md)
   for organism-prefix selection, request timeouts, bounded organism workers,
   shared request limiting, and controller-owned manifest checkpoints.
5. [Aggregate manifest v2 and build performance implementation plan](implementation-plan/20260810-v1.0-aggregate-manifest-build-performance-implementation-plan.md)
   for dataset-qualified snapshot identity, snapshot-aware discovery, bounded
   child-lock validation, v1 consumer migration, and deterministic performance
   gates.
6. [Dual-omics resource acquisition design](architecture/20260724-v1.0-dual-omics-resource-acquisition-design.md)
   for prioritized database additions, licensing gates, and the boundary
   between raw acquisition and normalized derived data.
7. [Reactome hardening and OmniPath full-evidence implementation plan](implementation-plan/20260731-v1.0-reactome-omnipath-full-evidence-implementation-plan.md)
   for dual-license interaction query identity, bounded evidence acquisition,
   sidecar-based lock/restore, and current-release retry behavior.
8. [HMDB browser acquisition and lock implementation plan](implementation-plan/20260727-v1.0-hmdb-browser-acquisition-lock-implementation-plan.md)
   for the active fix to browser-authorized acquisition, first-time lock
   metadata, archive validation, and authentication-failure behavior.
9. [dbCAN database acquisition implementation plan](implementation-plan/20260803-v1.0-dbcan-database-acquisition-implementation-plan.md)
   for the implemented pinned S3 CAZyme collection, large-download safety
   hardening, exact lock/restore contract, and remaining restore/publication
   acceptance.
10. [Test contract](testing/20260717-v1.0-test-contract.md) for local, CLI,
   concurrency, and real-resource validation requirements.
11. [CephFS lock and manifest benchmark](benchmarks/20260717-v1.0-cephfs-lock-and-manifest-benchmark.md)
   for the measured storage baseline and aggregate v2 candidate.
12. [Aggregate manifest v1-to-v2 migration](compatibility/aggregate-manifest-v1-to-v2.md)
    for consumer compatibility and dataset-qualified identity.
13. [Upstream data terms](compatibility/upstream-data-terms.md)
    for the non-redistribution boundary and source-specific access caveats.
14. [Complex Portal acquisition contract](architecture/20261020-v1.0-complexportal-acquisition-contract.md)
    for dated complete ComplexTab/XML snapshots, predicted data, offline
    locks and immutable restore.

The architecture and test contracts above describe the maintained
InterProScan and dbCAN snapshot, checksum, verifier-failure, and offline-test
behavior; they are the current authority for delivered behavior. The current
dbCAN full-size lock has been byte/SHA verified, but the snapshot is not called
production-ready and must not be published or redistributed until its
separately authorized exact-restore and release acceptance passes.

## History

Superseded plans, smoke notes, verifications, and closeouts live under
[archive/](archive/README.md). They retain decision history but may describe
commands, ownership boundaries, or layouts that no longer exist. Archived
documents are never an implementation authority.

The completed CLI v2, InterProScan, and AO adoption work is recorded in its
[archived implementation plan](archive/20260723-v1.0-cli-v2-interpro-scan-ao-adoption-implementation-plan.md)
and [closeout](archive/20260724-v1.0-cli-v2-interpro-scan-ao-adoption-closeout.md).
AO adoption is no longer the development policy. The
[publication readiness plan](implementation-plan/20260813-v1.0-github-publication-readiness-implementation-plan.md)
retains first-release provenance; current publication rules are above.
