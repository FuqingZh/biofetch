# biofetch

`biofetch` downloads versioned upstream bioinformatics assets. Its ownership
ends at source acquisition: raw files, source metadata, and per-snapshot
`manifest.lock` files.

The first public release targets Linux `amd64` and `arm64`, with `CGO_ENABLED=0`.
Install the latest tagged release from the public module with:

```bash
go install github.com/FuqingZh/biofetch/cmd/biofetch@latest
```

Prebuilt archives, checksums, SBOM, provenance, and completion files are
published on the [GitHub Releases](https://github.com/FuqingZh/biofetch/releases)
page. Source installation requires the Go version declared in `go.mod`.

The repository-owned code and documentation are licensed under
[Apache-2.0](LICENSE). Downloaded databases are not relicensed by this
repository; read [the upstream terms boundary](docs/compatibility/upstream-data-terms.md)
before acquiring or redistributing any source snapshot.

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
Verifier failures remove the completed `.part` so the next invocation restarts
cleanly; they do not create the final archive or a manifest record for it.

Fetch the maintained transcriptomics-metabolomics source snapshots:

```bash
biofetch chebi database fetch --output /database/bioinfo/resources/chebi
biofetch rhea database fetch --output /database/bioinfo/resources/rhea
biofetch jaspar data fetch --output /database/bioinfo/resources/jaspar
biofetch kegg metabolic fetch --output /database/bioinfo/resources/kegg
biofetch omnipath interactions fetch \
  --output /database/bioinfo/resources/omnipath \
  --dataset collectri \
  --license academic \
  --organisms 9606
```

Live OmniPath interaction snapshots use `academic` when `--license` is
omitted. DoRothEA defaults to confidence levels `A,B,C,D`; use
`--dorothea-levels` only with `--dataset dorothea`. Live acquisition verifies
the advertised API capabilities, inventories the edge set before and after
bounded target batches, and publishes only the generated query sidecar and one
full-evidence TSV per organism. Commercial mode is sent explicitly and never
falls back to academic data. Historical archives remain academic-only.

Reactome `current` is resolved with bounded retry and all mapping downloads
then use the immutable numeric release URL. An explicit `--version vNN`
bypasses the current-release endpoint.

P1 chemical resources use the same `fetch`, `lock`, and `restore` contract:

```bash
biofetch chemont ontology fetch --output /database/bioinfo/resources/chemont
biofetch lipidmaps lmsd fetch --output /database/bioinfo/resources/lipidmaps
biofetch hmdb database fetch --output /database/bioinfo/resources/hmdb
```

Fetch the pinned dbCAN CAZyme database collection:

```bash
biofetch dbcan database fetch \
  --output /database/bioinfo/resources/dbcan \
  --allow-large-downloads
```

dbCAN always uses `db_v5-2-9_5-5-2026`, downloads all four required files
(about 6.93 GiB) from the pinned S3 release, and defaults to one outer download
worker. It does not expose asset subsets or CGC flags. Lock and restore require
the exact `dbcan/database/db_v5-2-9_5-5-2026` snapshot.

`--assets` selects a maintained subset or `all`. Defaults avoid optional very
large files such as the HMDB all-spectra bundle; selecting a declared large
asset also requires `--allow-large-downloads`. KEGG metabolic entry records are
acquired in REST batches of at most ten IDs and the default request interval is
350 ms. The atomic dbCAN collection is the documented exception: it has no
`--assets` option and always requires the large-download opt-in.

Long KEGG PATHWAY acquisitions can be split by the first letter of the
organism code while retaining one snapshot and one controller-owned lock:

```bash
biofetch kegg pathway fetch --output /data/kegg --organism-prefix h --assets list,entry
biofetch kegg pathway fetch --output /data/kegg --organism-prefix a,b,c --workers 4
biofetch kegg pathway fetch --output /data/kegg --organism-prefix-file prefixes.txt --request-timeout 60s
```

Prefixes run in input order. Organisms use 1-8 bounded workers and share the
single process-wide request interval; the controller checkpoints
`manifest.lock` after each 32-organism batch. Multiple processes must not write
the same snapshot.

These commands preserve raw upstream files only. Compound crosswalks,
normalized reaction edges, motif tables, and other Parquet outputs remain
`bioextract` responsibilities.

Long options use lowercase kebab-case. Fetch output uses `-o` / `--output`;
durations accept native values such as `350ms`, `3s`, and `1m`. Use
`biofetch --version` for build information and `biofetch completion <shell>`
for Cobra-generated completion scripts.

See [docs/README.md](docs/README.md) for the current architecture and
validation contract. See [CONTRIBUTING.md](CONTRIBUTING.md) for the local gate,
[SECURITY.md](SECURITY.md) for private vulnerability reports, and
[CHANGELOG.md](CHANGELOG.md) for compatibility history.
