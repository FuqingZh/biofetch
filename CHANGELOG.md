# Changelog

All notable changes to biofetch are documented here. The project follows
[Semantic Versioning](https://semver.org/).

## [Unreleased]

## [0.2.0] - 2026-10-20

- Add versioned Complex Portal acquisition: complete all-species ComplexTab
  including predicted complexes, and the official complete PSI-MI XML 3.0
  archive. Support latest dated and explicit releases, complete snapshot
  locks, immutable restore, integrity validation and offline fixtures.
- Add KEGG PATHWAY organism-prefix plans, per-request timeouts, bounded
  organism workers with a shared limiter, and 32-organism manifest checkpoints.
- Harden KEGG retries and final download attempt time bounds.
- Verify arm64 release binaries natively and support authorized release recovery.
- Replace historical AO prerequisites with direct development and proportional
  local verification; preserve mandatory exact-head CI and release checks.
- Update the pinned Go toolchain to 1.26.9 and networking dependencies to
  resolve the security findings blocking release.

## [0.1.0] - 2026-08-14

- First public release of the raw database acquisition CLI.
- Include InterProScan, dbCAN, chemical, pathway, interaction, and manifest
  snapshot commands documented by the current contracts.
