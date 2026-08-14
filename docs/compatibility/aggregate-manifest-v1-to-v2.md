# Aggregate manifest v1 to v2 migration

Version: v1.0
Date: 2026-08-13
Status: current

The aggregate builder now emits `biofetch-manifest-v2`. Child snapshot identity
is `(database, asset, dataset, version)`, where `dataset` is optional and empty
when absent. This distinguishes multiple live datasets that share an asset
namespace, such as OmniPath interactions.

Consumers must:

1. accept `schema_version = biofetch-manifest-v2`;
2. treat `dataset` as an optional string in TOML/JSON and as the `Dataset`
   column in TSV;
3. include `dataset` in uniqueness keys and cache paths when present; and
4. retain the child `version` field as the immutable `version_token`.

The builder does not write v1 and does not silently coalesce two datasets.
Historical v1 files remain readable as rollback artifacts until every consumer
has migrated. A v1 consumer must fail with an actionable schema message rather
than treating two datasets as duplicate snapshots.

`biofetch manifest build --workers N` controls bounded child-lock validation;
the default is four workers and the accepted range is 1-64. Discovery is
snapshot-aware and does not enter `raw/`, `tidy/`, `logs/`, or staging trees.
