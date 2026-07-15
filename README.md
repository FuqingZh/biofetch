# biofetch

`biofetch` downloads versioned upstream bioinformatics assets. Its ownership
ends at source acquisition: raw files, source metadata, and per-snapshot
`manifest.lock` files.

Parsing, normalization, and canonical derived datasets belong to
`bioextract`, including every `tidy/` asset. New `biofetch` runs do not create
empty `tidy/` directories. Existing directories and files are never removed
automatically.

## Snapshot layout

Each managed snapshot has a stable version directory:

```text
<resource-root>/<database>/<asset>/<version_token>/
  raw/
  manifest.lock
```

Some established databases use a shorter or nested path, such as
`string/<version_token>/catalog/`. The lock's `version_token` must occur as a
complete directory segment in its resource-relative path. Every file recorded
by `manifest.lock` is relative to the lock's directory and lives under `raw/`.

## Common commands

Download a database asset and write its snapshot lock:

```bash
biofetch go ontology fetch --dir_out /database/bioinfo/resources/go
```

Build the aggregate manifest for an entire resource tree:

```bash
biofetch manifest build \
  --dir_in /database/bioinfo/resources \
  --file_stem_out /database/bioinfo/manifest \
  --formats toml
```

`--formats` accepts `toml`, `tsv`, and `json` as comma-separated values,
repeated options, or `@file` entries. TOML is the default canonical format.
The output stem is literal: a stem ending in `.toml` produces
`.toml.toml` for TOML output.

See [docs/README.md](docs/README.md) for the current architecture and
validation contract.
