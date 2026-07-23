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
`omnipath/interactions/<dataset>/<version_token>/`. The lock's
`version_token` is the final snapshot-directory segment. Every file recorded by
`manifest.lock` is relative to the lock's directory and lives under `raw/`.

## Common commands

Download a database asset and write its snapshot lock:

```bash
biofetch go ontology fetch --output /database/bioinfo/resources/go
```

Rebuild a snapshot lock from an existing snapshot directory. The directory
name is the authoritative `version_token`; `lock` does not accept a separate
`--version`:

```bash
biofetch kegg catalog lock \
  /database/bioinfo/resources/kegg/catalog/2026-04 \
  --workers 4
```

Every `lock` performs a complete SHA256 pass over the snapshot's `raw/`
files. Hashing is parallel and deterministic; `--workers` defaults to 4
and can be tuned for the storage system. More workers can reduce performance
on shared filesystems, so increase it only after measurement.

Build the aggregate manifest for an entire resource tree:

```bash
biofetch manifest build \
  /database/bioinfo/resources \
  --output /database/bioinfo/meta \
  --formats toml
```

`--formats` accepts `toml`, `tsv`, and `json` as comma-separated values,
repeated options, or through `--formats-file`. TOML is the default canonical format.
Output names are fixed as `manifest.toml`, `manifest.json`, and
`manifest.tsv` within `--output`. The aggregate manifest is derived from
snapshot locks and is therefore never named `manifest.lock`.

Restore missing or invalid files from the URLs and SHA256 records in one exact
snapshot lock:

```bash
biofetch kegg catalog restore \
  /database/bioinfo/resources/kegg/catalog/2026-04 \
  --on-existing skip \
  --retry-wait 3s
```

Fetch a fixed InterProScan distribution without extracting or installing it:

```bash
biofetch interpro scan fetch \
  --output /database/bioinfo/resources/interpro \
  --version 5.77-108.0 \
  --allow-large-downloads
```

InterProScan fetch requires the explicit large-download opt-in. It preserves
the upstream archive and `.md5` file under
`interpro/scan/<version>/raw/`, verifies MD5 before publishing the archive,
and records repository-standard SHA256 and byte counts in `manifest.lock`.
Verifier failures retain the completed `.part` file for inspection or a later
resumed fetch; they do not create the final archive or a manifest record for
it.

Long options use lowercase kebab-case. Fetch output uses `-o` / `--output`;
durations accept native values such as `350ms`, `3s`, and `1m`. Use
`biofetch --version` for build information and `biofetch completion <shell>`
for Cobra-generated completion scripts.

See [docs/README.md](docs/README.md) for the current architecture and
validation contract.
