# Biofetch documentation

This directory contains the maintained contracts and historical design record
for `biofetch`.

## Current authority

Read these documents in order:

1. [Root README](../README.md) for the product boundary and common commands.
2. [Resource and manifest contract](architecture/resource-manifest-contract.md)
   for snapshot layout, lock semantics, aggregate manifests, and path rules.
3. [Dual-omics resource acquisition design](architecture/20260724-v1.0-dual-omics-resource-acquisition-design.md)
   for prioritized database additions, licensing gates, and the boundary
   between raw acquisition and normalized derived data.
4. [Reactome hardening and OmniPath full-evidence implementation plan](implementation-plan/20260731-v1.0-reactome-omnipath-full-evidence-implementation-plan.md)
   for dual-license interaction query identity, bounded evidence acquisition,
   sidecar-based lock/restore, and current-release retry behavior.
5. [HMDB browser acquisition and lock implementation plan](implementation-plan/20260727-v1.0-hmdb-browser-acquisition-lock-implementation-plan.md)
   for the active fix to browser-authorized acquisition, first-time lock
   metadata, archive validation, and authentication-failure behavior.
6. [dbCAN database acquisition implementation plan](implementation-plan/20260803-v1.0-dbcan-database-acquisition-implementation-plan.md)
   for the implemented pinned S3 CAZyme collection, large-download safety
   hardening, exact lock/restore contract, and pending full release acceptance.
7. [Test contract](testing/20260717-v1.0-test-contract.md) for local, CLI,
   concurrency, and real-resource validation requirements.
8. [CephFS lock and manifest benchmark](benchmarks/20260717-v1.0-cephfs-lock-and-manifest-benchmark.md)
   for the current measured storage baseline.

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
