# pg_search 0.26.1 integration decision

The original M3 hybrid contract remains in force. Select the only useful lexical
candidate in the frozen comparison, pg_search 0.26.1, with the unchanged
`pdb.chinese_compatible` analyzer, disjunctive `paradedb.match`, BM25 ordering and
UUID ties. Keep equal-weight RRF and the original failure/degradation policy.
This implements the required capability; it does not establish better relevance
than dense-only. No extra user approval follows merely from installing an AGPL
service for the authorized internal deployment.

## Evidence and quality limits

The frozen 64-document/72-query comparison and its independent numerical review
are complete (selection `e1964240`, Model `784c6468`). Of 64 answerable queries,
pg_search lexical Recall@20/nDCG@10/MRR@20 are .903646/.762231/.854434; these are
overall metrics, not the Chinese-only group. Dense scores are 1/.986842/1 and
pg_search plus dense RRF scores are 1/.893638/.958333. Per-query nDCG worsens on
34, improves on 2, and ties on 28. Native FTS and PGroonga frozen lexical legs
are empty. All four dense/fused routes return 20 hits for all 8 no-answer queries.
The SPEC sets no numerical no-regression gate. We retain the observed regression
as a limitation, not a hidden new gate or a reason to tune against the test set.

App currently uses 64 lexical/64 dense/64 fused candidates and 8 results, with
RRF constant 60; benchmark used top20 throughout. Those App defaults have not
inherited the benchmark's measured quality. No new model requests, dataset,
reranker requirement, weight change or parameter tuning is part of this closure.
The fixed snapshot is 4096-dimensional: exact pgvector distance is supported,
while vector/halfvec ANN limits in 0.8.1 are 2000/4000. The observed pg_search
CustomScan plus explicit sort is not a throughput/capacity acceptance.

## Exact installation and deployment boundary

- Base: `pgvector/pgvector@sha256:99a149d3c84cfb0f32d8da7d72737e4643468787220af2223418730f8e9e9cdc`,
  observed PostgreSQL `17.8-1.pgdg12+1`, vector `0.8.1`, Debian bookworm, amd64.
- Official binary: [release asset](https://github.com/paradedb/paradedb/releases/download/v0.26.1/postgresql-17-pg-search_0.26.1-1PARADEDB-bookworm_amd64.deb),
  53,629,552 bytes, SHA-256
  `79ea1fb115e3e136f6086b82d8a91e2cf86d2ba21f58a58477d4f372d73384ce`.
  Package metadata version is `0.26.1` despite its longer filename.
- Source: commit `529c77e9dffe4bd86b6b7971c019bb26b8eadbfa`, exact v0.26.1
  Cargo.lock SHA-256 `f288169943d7c7eee593f4718379e849e3b62ebb6187216f170c8f82d0e17e8a`.
- Required OpenBLAS packages: `libopenblas0` and `libopenblas0-pthread`
  `0.3.21+ds-4`. Their runtime dependencies are also required; the original
  installed-package/ldd evidence is retained in `results/`. No full upgrade.
- Preserve default proxy variables and TLS verification. A missing trusted CA
  is fixed by adding trust in the owned build, never by disabling verification.
- The upstream v0.26.1 `Dockerfile.official-17` downloads pg_search 0.26.0,
  vector 0.8.6 and an unpinned PostgreSQL image. Do not use it as this recipe.

The runtime implementation branch `ai/m3-retrieval-runtime` owns
`install-pg-search.sh` and `bootstrap-pg-search.sql` in the formal
[`deploy/retrieval`](../../deploy/retrieval/) directory. The
installer consumes the already checksum-verified exact artifact; it does not
download, upgrade, connect to a database or change preloads. It restores the
actual package LICENSE/copyright omitted by the base dpkg documentation filter
and preserves the known embedded dictionary notices.

Bootstrap is explicit deployment work after the normal Knowledge/Memory schemas
exist. It requires the accurate versions and `shared_preload_libraries=pg_search`
before creating the two indexes. It is not an unconditional global migration:
old databases using native FTS or PGroonga remain upgradeable. The reserved 134
number was released. No conditional migration may silently skip indexes and
claim readiness. Index creation uses ordinary transactional CREATE INDEX; plan
the deployment window for its write blocking, and keep the old serving profile
until the domain's normal rebuild/publication completes.

## Integration owners and concrete contract

| Owner | Exact responsibility |
|---|---|
| Retrieval selection | `internal/central/retrieval/lexical/sql.go`, `sql_test.go`; owned install/bootstrap/verification inputs; accurate version and index definition evidence; exported `lexical.PGSearchReadinessSQL` |
| Integration | existing config/App assembly and Compose; explicit `backend=pg_search`; same-DB readiness of version, preload and both valid BM25 indexes; visible failure before ready; original fallback semantics |
| Knowledge | existing immutable IndexProfile and worker/rebuild/publication; scoped SQL joins; document/current-version eligibility and citations |
| Memory | existing generation/snapshot and canonical revision/namespace eligibility; type/tags filters and source revision |

The shared SQL branch uses `c.id @@@ paradedb.match('index_text',$6,conjunction_mode=>false)`
and `paradedb.score(c.id)` for Knowledge, `p.memory_id` with the existing `$4`
query binding for Memory. The other parameter positions and domain joins remain
unchanged. It never embeds user query text in SQL or performs global LIMIT before
authorization filters. UUID value ordering agrees with canonical UUID ASCII
ordering, independent of text locale.

The exact upstream score implementation identifies a relation Var, rather than
looking up rows by its value; `key_field` became a deprecated no-op in 0.26.0.
Thus Memory's repeated `memory_id` across generations does not require an
invented surrogate UUID solely for this API. This source fact does not replace
the new real UUID-domain/index/filtered-query check.

Knowledge must retain project, active document, current document version, ready
generation, serving profile, same snapshot/dimension and document/media/source
filters before LIMIT. Memory must retain project + agent namespace, serving
generation, active canonical revision and type/tags before LIMIT. The SQL-level
check does not prove authenticated App/Tool chains; those remain the domain
owners' integration gates.

Integration must execute the exported `lexical.PGSearchReadinessSQL` on the same application database when `options.Backend()==lexical.PGSearch`, scan its one boolean result, and refuse readiness for false or SQL error. This single source checks PG17.8/vector0.8.1/pg_search0.26.1, preload, both exact table/index identities, valid/ready/live flags, key/expression arity, tokenizer type/expression and frozen index options. It performs no DDL. The source has been written; its real catalog behavior remains to be checked in the allocated fixture.

Startup with explicitly selected pg_search and an absent/wrong extension or
index must fail visibly, not report a hybrid service ready. During operation,
only the existing classified lexical-unavailable policy may produce an explicitly
reported `dense_only` response; authorization, namespace, invalid input or stale
generation errors must never be relabeled as lexical unavailability. There is no
new configuration system, persistent readiness ledger or silent backend switch.

## License obligations at the actual trigger

No project-root LICENSE or external binary/container publishing workflow was
found in the bounded repository inspection. This does not grant a public license
to project code. README and Dockerfile.dev already build a fixed unmodified AGPL
MinIO locally, preserve its LICENSE and identify its source/version.

AGPL section 2 explicitly permits running the unmodified program and internal
non-conveyed use. Section 0 says network interaction without transfer of a copy
is not conveying; section 13 addresses a modified program offered over a network.
The present implementation installs the upstream unmodified extension in PG,
uses SQL from Go, and does not copy/link its implementation into Go. This is a
sufficient engineering basis to proceed with current internal integration. It
does not decide whether a later combined or modified product is a covered work;
process/container separation is not an automatic exemption.

The actual Debian copyright says AGPL-3.0-or-later; Cargo metadata says AGPL-3.0.
Preserve both, comply with AGPLv3, and promise no additional alternative license.
Before conveying a database binary/image to another party, the release owner
must pair it with the applicable section 6 Corresponding Source access: exact
extension source, actual static dependency source/license/NOTICE closure,
required build/install scripts, and any local modifications. A homepage link or
workspace package count is insufficient. Modified remotely used covered code
must also satisfy section 13's prominent source-offer requirement. If those
actual obligations cannot be met for a proposed external release, resolve that
concrete release boundary or obtain a suitable license before that release;
do not block current unmodified internal use speculatively.

The workspace lock has 791 packages including development/benchmark entries; it
is not the resolved shipped Linux/PG17 closure. This release work remains open.
Known embedded Lindera 6.2.0 dictionary notices are now preserved with hashes:
CC-CEDICT (CC BY-SA 4.0 attribution/share-alike for applicable dictionary changes),
IPADIC (NAIST/ICOT conditions), ko-dic (Apache 2.0), plus original pg_search and
OpenBLAS notices. Choosing chinese_compatible does not prove those embedded
materials are absent from the binary. The small notice bundle is not a complete
SBOM or a completed external redistribution acceptance.

## Verification state

The old relevance benchmark remains frozen and independently checked. The new
SQL/installation check used product source `b24ba485`, with harness-only stdout/
stderr correction `0f288f14`. Original session 51248 exited 1 because a planner
warning was incorrectly parsed as a data row; the exact four Knowledge IDs were
present. Original session 67620 exited 0 and passed the complete bounded gate.
Both runs gracefully stopped PG (0), stopped container sleep PID1 (137), removed
their exact nonce container/network, and confirmed absence. These are separate
facts; the first failure is not relabeled as a pass.

The actual install, two UUID-domain indexes and exported readiness predicate
passed. Readiness returned false for missing extension/index, true after the
accurate bootstrap, false for a missing Memory index and a BM25 index using the
wrong tokenizer, then true after exact restoration. A repeated bootstrap failed
explicitly without silently replacing the existing indexes. Both domain SQLs
retained the original join predicates and passed the public eligibility fixture,
including current version, namespace/type/tags, repeated Memory IDs across
revisions/generations and UUID ties. These are isolated SQL checks, not domain
`ValidateServing` authorization/admission, complete global migrations, or actual
App/Tool lifecycle acceptance.

Both actual EXPLAIN plans, with default enable_seqscan, use a ParadeDB Base Scan
below ordinary canonical joins, then Sort and final Limit. Project/namespace/
generation predicates appear in its heap_filter. The actual warning says
JoinScan is not used because the other canonical relations have no ParadeDB
indexes; this does not mean no lexical index was used. Do not add unrelated
indexes merely to suppress it or claim all joins/TopK were pushed into the
extension. No latency/throughput/capacity conclusion follows.

[Recoverable results](results/bootstrap/manifest.json) retain exact source hashes,
plans, index definitions, extracted product queries, both original outcomes and
owned cleanup identities. Full repeatable logs remain in the ignored output
paths recorded there. Ordinary lexical/config checks passed with Go1.27.1, readonly/p1/GOMAXPROCS2,
original session22718 Wait0 (lexical0.005s/config0.064s), recorded separately in
`go-validation.json`. Integration still owns the
App/Compose readiness call and deployment binding, which this component gate
does not implement on its behalf.
