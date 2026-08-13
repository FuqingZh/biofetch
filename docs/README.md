# Biofetch documentation

This directory contains the maintained contracts and historical design record
for `biofetch`.

## Current authority

Read these documents in order:

1. [Root README](../README.md) for the product boundary and common commands.
2. [Resource and manifest contract](architecture/resource-manifest-contract.md)
   for snapshot layout, lock semantics, aggregate manifests, and path rules.
3. [GitHub publication readiness implementation plan](implementation-plan/20260813-v1.0-github-publication-readiness-implementation-plan.md)
   for module identity, licensing boundaries, CI/security, release artifacts,
   public contributor entrypoints, and the first tagged release.
4. [Aggregate manifest v2 and build performance implementation plan](implementation-plan/20260810-v1.0-aggregate-manifest-build-performance-implementation-plan.md)
   for dataset-qualified snapshot identity, snapshot-aware discovery, bounded
   child-lock validation, v1 consumer migration, and deterministic performance
   gates.
5. [Dual-omics resource acquisition design](architecture/20260724-v1.0-dual-omics-resource-acquisition-design.md)
   for prioritized database additions, licensing gates, and the boundary
   between raw acquisition and normalized derived data.
6. [Reactome hardening and OmniPath full-evidence implementation plan](implementation-plan/20260731-v1.0-reactome-omnipath-full-evidence-implementation-plan.md)
   for dual-license interaction query identity, bounded evidence acquisition,
   sidecar-based lock/restore, and current-release retry behavior.
7. [HMDB browser acquisition and lock implementation plan](implementation-plan/20260727-v1.0-hmdb-browser-acquisition-lock-implementation-plan.md)
   for the active fix to browser-authorized acquisition, first-time lock
   metadata, archive validation, and authentication-failure behavior.
8. [dbCAN database acquisition implementation plan](implementation-plan/20260803-v1.0-dbcan-database-acquisition-implementation-plan.md)
   for the implemented pinned S3 CAZyme collection, large-download safety
   hardening, exact lock/restore contract, and pending full release acceptance.
9. [Test contract](testing/20260717-v1.0-test-contract.md) for local, CLI,
   concurrency, and real-resource validation requirements.
10. [CephFS lock and manifest benchmark](benchmarks/20260717-v1.0-cephfs-lock-and-manifest-benchmark.md)
   for the measured storage baseline and aggregate v2 candidate.
11. [Aggregate manifest v1-to-v2 migration](compatibility/aggregate-manifest-v1-to-v2.md)
    for consumer compatibility and dataset-qualified identity.
12. [Upstream data terms](compatibility/upstream-data-terms.md)
    for the non-redistribution boundary and source-specific access caveats.

The architecture and test contracts above describe the maintained
InterProScan and dbCAN snapshot, checksum, verifier-failure, and offline-test
behavior; they are the current authority for delivered behavior. dbCAN is not
production-ready and must not be published to CephFS until its separately
authorized full-size release acceptance passes.

## History

Superseded plans, smoke notes, verifications, and closeouts live under
[archive/](archive/README.md). They retain decision history but may describe
commands, ownership boundaries, or layouts that no longer exist. Archived
documents are never an implementation authority.

The completed CLI v2, InterProScan, and AO adoption work is recorded in its
[archived implementation plan](archive/20260723-v1.0-cli-v2-interpro-scan-ao-adoption-implementation-plan.md)
and [closeout](archive/20260724-v1.0-cli-v2-interpro-scan-ao-adoption-closeout.md).
