# M3 retrieval selection

Owner: `m3_retrieval_selection`; base `a1ce6f1b`. This directory owns the minimal candidate harness and recoverable inputs. It does not change production retrieval, fixed data, scorer, dependency locks or global migrations.

Use the existing `scripts/search-benchmark.py export` on unchanged `lexical-v2` before candidate execution. Runners receive only its answer-free export. Reuse the documented native FTS result; never manufacture its missing historical run file. PGroonga is frozen to 4.1.0 / Groonga 16.1.3, TokenBigram / NormalizerAuto, `&@~`, `pgroonga_score(tableoid,ctid)`, `enable_seqscan=off`, source ID ASCII tie, top20. Its EXPLAIN must show the created index before scoring. pg_search is frozen to 0.26.1; its one analyzer/query configuration must be recorded before querying the test inputs.

All resources must be new nonce-owned containers/networks on the existing digest `pgvector/pgvector@sha256:99a149d3c84cfb0f32d8da7d72737e4643468787220af2223418730f8e9e9cdc`. Read the actual installed/server version and vector version. Never connect to dev infrastructure. Candidate installation is confined to those containers; shared downloads are outside worktrees.

2026-10-14 actual default-proxy/TLS GETs returned HTTP200 for all three exact official releases and the Groonga bookworm Packages index. Exact release artifacts were downloaded and SHA256 recorded in `sources.json`. Bookworm does not advertise Groonga16.1.3/PGroonga4.1.0; a build on the existing base is needed. The pg_search bookworm amd64 package declares PostgreSQL17 and libopenblas0 dependencies; its extension control also needs inspection. This describes the initial checkpoint before candidate scoring; the completed phase is recorded below.

Knowledge requires SQL filtering before ranking/topK: scope/project, active document and current document_version, ready generation and project serving profile; optional document/media/source_kind filters. Memory adds agent namespace and active canonical revision/type/tags. Both consume the same embedding snapshot/dimension. The fixed real Model snapshot and vectors are now available; completed fusion results are recorded below. API availability probes alone do not constitute product-chain acceptance.

Raw repeatable HTTP responses and logs live in ignored `output/ai/m3-retrieval-selection`. Required frozen configuration, harness source and final irreproducible sanitized results belong here. Native raw output was not found in the current main `output` directory: rely on the accepted D13 historical metrics without claiming to have recovered those original files.

## Reproduce the completed lexical phase

The two frozen candidate runs are complete; `results/*-run.json` contains actual rankings and `results/*-source.json` the exact export hashes, SQL and plans. Original scorer replay uses these run files unchanged. PGroonga returns no hits; pg_search Recall@20=0.9036458333333334 and MRR@20=0.8544338474025974; 8/8 no-answer queries produce 20 candidates. See the D13 card for limits.

```sh
python3 scripts/search-benchmark.py export --data tests/search-benchmark/data --output output/ai/m3-retrieval-selection/export-new
python3 .agent-state/m3-retrieval-selection/prepare-fixture.py --cache /workspace/shared/downloads/m3-retrieval-selection --output output/ai/m3-retrieval-selection/fixture-new
python3 .agent-state/m3-retrieval-selection/run-candidate.py --owned output/ai/m3-retrieval-selection/fixture-new/owned.json --export output/ai/m3-retrieval-selection/export-new --backend pg_search --output output/ai/m3-retrieval-selection/pg-search-new
python3 .agent-state/m3-retrieval-selection/run-candidate.py --owned output/ai/m3-retrieval-selection/fixture-new/owned.json --export output/ai/m3-retrieval-selection/export-new --backend pgroonga --output output/ai/m3-retrieval-selection/pgroonga-new
python3 .agent-state/m3-retrieval-selection/cleanup-fixture.py --owned output/ai/m3-retrieval-selection/fixture-new/owned.json
python3 scripts/search-benchmark.py score --data tests/search-benchmark/data --run .agent-state/m3-retrieval-selection/results/pg-search-run.json --output output/ai/m3-retrieval-selection/score-replay-new
```

Run only with the integration owner's available fixture slot. Preparation is the consolidated recipe of the actual component commands, syntax checked; it was not redundantly run as another complete fixture. The initial actual build failure from PGroonga's disabled xxhash option is preserved in the D13 card; the final recipe enables system xxhash. Use the exact packages, no upgrade command, inherited proxy variables and trusted CA/TLS. If preparation fails, preserve its exact owned IDs, stop outstanding owned work and remove those resources; do not claim cleanup from a missing record. The original container sleep PID1 was killed by Docker stop (137), after PG graceful stop0; exact resources were then removed.

Shared reusable runtime artifact `/workspace/shared/downloads/m3-retrieval-selection/pgroonga-runtime.tar.gz` is 3,450,896 bytes, SHA256 `6a866999d3145328aaeb54bea5657a71596fedaa523fa240b021421698d8d507`; it contains built Groonga/PGroonga libraries and extension definitions, not all linked libraries. Runtime needs the recorded MessagePack/xxhash/system dependencies. This artifact is optional and reproducible from the pinned recipe; it is not a production image or deployment.

## Completed fixed dense/RRF comparison

The original Model Runtime session2079 returned 136 real 4096-dimensional vectors in three batches under one immutable v2 snapshot. Model checkpoint `784c6468` preserves `.agent-state/m3-model-runtime/runtime-embeddings-v2/vectors.json.gz`; its SHA256 is `948074800e4cc25bff2e88305f5edd70ea82f2c677565821e0d96f1a4953276d`. This directory references that single recoverable input instead of duplicating the 2.5 MB payload. `results/fusion/model-manifest.json` records its actual Runtime and cleanup evidence; configured identity is retained, provider response model alias is not.

```sh
python3 -B .agent-state/m3-retrieval-selection/test-fixed-fusion.py
python3 -B .agent-state/m3-retrieval-selection/fuse-fixed.py --vectors .agent-state/m3-model-runtime/runtime-embeddings-v2/vectors.json.gz --manifest .agent-state/m3-model-runtime/runtime-embeddings-v2/manifest.json --export output/ai/m3-retrieval-selection/export-new --output output/ai/m3-retrieval-selection/fusion-new
python3 scripts/search-benchmark.py score --data tests/search-benchmark/data --run output/ai/m3-retrieval-selection/fusion-new/pg_search-rrf-run.json --output output/ai/m3-retrieval-selection/fusion-new/pg_search-score
```

The export must reproduce the original two pinned SHA values; the vector paths are present after integration of the Model checkpoint. The fusion command accepts explicit paths when the Model evidence lives in its separate worktree. The original scorer can similarly consume `dense-run.json`, `native_pg_fts-rrf-run.json` and `pgroonga-rrf-run.json`. Scorer backend labels identify the lexical leg; config explicitly says dense-only or fusion, never a fresh lexical experiment. Stored `results/fusion/*-run.json.gz` are gzip-compressed exact generated runs and can be decompressed before scoring. `provenance.json` and `summary.json` retain all group metrics and per-query dense/pg_search differences.

Actual author controls: 4/4 hand-calculated tests; original fusion session9215 Wait0; four original scorer calls exit0. Dense nDCG@10=0.9868422792946057 versus pg_search RRF=0.8936382638653616; 34/64 queries regress, 2 improve and 28 tie. All four routes return 20 candidates for all eight no-answer queries. Native/PGroonga empty lexical legs leave dense order unchanged. No new Model request, SQL query or lexical run was made. New fusion code still requires non-author review.

The frozen comparison and independent numerical review are complete. The original first-phase hybrid contract remains in force; the unanswered dense-first scope question does not pause implementation. Select the exact pg_search 0.26.1 configuration for the lexical leg, preserving equal-weight RRF and all observed ranking regressions. This is an implementation choice, not evidence of an improvement over dense or a passed product quality gate. See [integration.md](integration.md) for the concrete installation, ownership and license boundaries. No benchmark rerun or test-set tuning is authorized by this closure.

A 4096-dimensional pgvector exact scan is possible, but pgvector0.8.1 vector/halfvec ANN limits of 2000/4000 do not cover this snapshot. The current App defaults (64/64/64 candidates, 8 results) differ from the frozen top20 comparison and are not assigned its quality numbers.

Unmodified internal use is allowed under AGPL section 2. Distribution of a database image/binary and modifications have additional source and notice obligations; separate containers are not an exemption. The workspace lock inventory is not the shipped PG17 static dependency closure. That closure and Corresponding Source package remain necessary before accepting external redistribution, rather than an automatic user-approval step before current internal integration.

## Recovery in main

Frozen candidate configuration, harness and rankings come from the selection tree.
`integration.md`, `licenses/integration-source-provenance.json`,
`check-bootstrap.py` and `results/bootstrap/` come from the later runtime tree,
which records both the original failed check and the corrected successful check.
Their historical source identifiers and manifest hashes describe those original
inputs; they are not hashes of the later path adaptation.

The four Lindera/OpenBLAS notices and the unchanged installer/bootstrap SQL are
reused from [`deploy/retrieval`](../../deploy/retrieval/) after byte comparison.
The check now mounts that formal recipe directory; the notice provenance paths
point there too. No benchmark, fixture, external request or product check was
rerun during this material transfer. Original repeatable runtime command logs
remain historical paths; the retained bootstrap results carry both original
outcomes and owned cleanup identities. This directory does not assert that
those ignored command-log directories are available on another machine.
