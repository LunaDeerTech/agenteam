# Explicit pg_search deployment

This optional database image adds the unmodified `pg_search 0.26.1` extension to
the pinned PostgreSQL 17.8 / pgvector 0.8.1 image. Ordinary application migrations
remain usable without this extension. The application only selects it when
`AGENTEAM_CENTRAL_RETRIEVAL_RUNTIME` explicitly uses `"backend":"pg_search"`.

Obtain the exact official
[Debian package](https://github.com/paradedb/paradedb/releases/download/v0.26.1/postgresql-17-pg-search_0.26.1-1PARADEDB-bookworm_amd64.deb)
and put it in a separate build input directory. Its filename is
`postgresql-17-pg-search_0.26.1-1PARADEDB-bookworm_amd64.deb`; its SHA-256 is
`79ea1fb115e3e136f6086b82d8a91e2cf86d2ba21f58a58477d4f372d73384ce`.
The installer verifies the package, base versions, OpenBLAS versions and notices.
It retains package licenses even though the base image excludes documentation.
Keep proxy settings and TLS trust enabled.

```sh
export AGENTEAM_PG_SEARCH_PACKAGE_DIR=/absolute/package-directory
docker compose -f docker-compose.dev.yaml -f docker-compose.retrieval-pg-search.yaml build database
```

APT uses HTTPS. If the build network needs an additional trusted CA, supply the
complete approved CA bundle as a BuildKit secret; do not disable certificate
verification or copy that bundle into an image layer. For example, with the
host's configured trust bundle and the same explicit package directory:

```sh
docker buildx build --load \
  --build-context "pg_search_package=$AGENTEAM_PG_SEARCH_PACKAGE_DIR" \
  --secret id=apt_ca,src=/etc/ssl/certs/ca-certificates.crt \
  --tag agenteam-pg-search:0.26.1-pg17.8-vector0.8.1 \
  deploy/retrieval
```

The optional secret affects only these APT commands. Standard Docker proxy
configuration and runtime database/application TLS settings remain in force.
Task fixtures pin the resulting local immutable image ID and verify the actual
versions and preload; a tag or a successful image build does not establish
database bootstrap or App hybrid readiness.

Building does not change the existing database service. Plan its restart and the
index creation window explicitly. The overlay enables the required preload;
it does not install extensions inside a database or change Central's settings.
After the ordinary Knowledge and Memory migrations exist in the intended
application database, run the exact bootstrap there with its normal deployment
database authority. For the development Compose database:

```sh
docker compose -f docker-compose.dev.yaml -f docker-compose.retrieval-pg-search.yaml exec -T database \
  psql -X -v ON_ERROR_STOP=1 -U agenteam -d agenteam < deploy/retrieval/bootstrap-pg-search.sql
```

Bootstrap creates the extension and two exact BM25 indexes in one transaction.
It refuses existing conflicting indexes and incorrect versions. Ordinary index
creation blocks writes, so use the deployment's maintenance window. Then set the
existing Central runtime configuration explicitly, for example
`{"backend":"pg_search","dimensions":4096}`; keep the actual embedding profile's
dimension and the existing execution runtime configuration. The database-only
overlay does not silently enable retrieval in the app service.

Central checks the same application database before constructing domain services:
PostgreSQL/vector/pg_search versions, preload, both valid/live indexes and their
exact tokenizer expressions. A missing or mismatched dependency fails startup.
Runtime lexical failures retain the existing explicit degradation policy; this
does not change authorization or the original 64/64 candidate and 8-result limits.
Existing Knowledge profiles and Memory generations still change through their
normal rebuild and publication lifecycle.

The isolated SQL/bootstrap test passed both domain joins and readiness checks.
This image recipe and the App hybrid retrieval chains require their own validation;
that SQL result does not establish App relevance or throughput.

## Notices and redistribution

The extension is the upstream unmodified AGPL release (Debian copyright says
AGPL-3.0-or-later; Cargo metadata says AGPL-3.0). Exact source commit:
`529c77e9dffe4bd86b6b7971c019bb26b8eadbfa`. The installer preserves the actual
package LICENSE/copyright plus the supplied Lindera dictionary and OpenBLAS
notices under `/usr/share/licenses/pg_search`.

The current scope is internal use. Before conveying this image externally,
the release owner must supply the applicable Corresponding Source, actual shipped
dependency source/license/NOTICE closure and build/install scripts. Modified
covered software used remotely also needs the applicable AGPL source offer.
The small notice bundle here is not a complete external release SBOM or source
distribution. Do not infer that unused tokenizer choices remove bundled material.
