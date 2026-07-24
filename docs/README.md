# Biofetch documentation

This directory contains the maintained contracts and historical design record
for `biofetch`.

## Current authority

Read these documents in order:

1. [Root README](../README.md) for the product boundary and common commands.
2. [Resource and manifest contract](architecture/resource-manifest-contract.md)
   for snapshot layout, lock semantics, aggregate manifests, and path rules.
3. [Test contract](testing/20260717-v1.0-test-contract.md) for local, CLI,
   concurrency, and real-resource validation requirements.
4. [CephFS lock and manifest benchmark](benchmarks/20260717-v1.0-cephfs-lock-and-manifest-benchmark.md)
   for the current measured storage baseline.

The architecture and test contracts above describe the maintained
InterProScan snapshot, checksum, verifier-failure, and offline-test behavior;
they are the current authority for the delivered behavior.

## History

Superseded plans, smoke notes, verifications, and closeouts live under
[archive/](archive/README.md). They retain decision history but may describe
commands, ownership boundaries, or layouts that no longer exist. Archived
documents are never an implementation authority.

The completed CLI v2, InterProScan, and AO adoption work is recorded in its
[archived implementation plan](archive/20260723-v1.0-cli-v2-interpro-scan-ao-adoption-implementation-plan.md)
and [closeout](archive/20260724-v1.0-cli-v2-interpro-scan-ao-adoption-closeout.md).
