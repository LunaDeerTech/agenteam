Stage A author handoff — frozen, no running author commands

Five product files remain byte-identical to a02-current-five.json (SHA256 8d78d3b4de0fb8fcb8f0fb8af6ea4ff92fe03c40554d24014b0be422b0e8b7a9). They implement RequireHuman without modifying RequireSystem, strict typed queries, complete validated safe projections and bounded encoding, pure tests and the three-resource six-operation schema. No handler or production stub was added.

Passed on these exact files:
- a03 offline dependency discovery; a10 actual -race discovery.
- a05/a07 separate -race test compilation, actual exit0 (35.550s/3.636s).
- a06 Account HTTPBoundary: 5 top-level / 36 named subcases, race binary, actual exit0 (1.067s).
- a08 Usage wire: 11 top-level / 99 named subcases, race binary, actual exit0 (1.570s); includes 129 cases through jsonschema4.26.0 / referencing0.37.0 Draft202012, and Node24.19.0 ECMA 34 patterns / 40 scalar cases / 2 Unicode checks. Python format assertions remain the original server's typed validation.
- a11/a12 separate go vet -race -p=1, actual exit0 (6.606s/0.265s). No completed nonrace vet is claimed.
- gofmt and final git diff --check.

Measured maximum default-escaped bytes: invocation4564, list100464720, aggregate10093858, Project49672. All fit the card's conservative bounds; final 1MiB encoding gate remains.

Retained failures/limits: a01 missing fixed-module setup failed; root separately restored exact dependencies. a04 combined cold race build and a09 cold nonrace vet exceeded their unchanged 45s outer budgets before any test/assertion output. a04 directly waited its go process but left compiler PID22765/starttime81512 Z/PPID1 unjoined; this is not claimed cleaned/reaped. The scratch driver was then explicitly changed to subreaper. a09 actually reaped adopted compiler PID30312/status15 and has zero group members. All a05–a12 groups have zero members at final scan; no subsequent product edits or assertion failures occurred.

Evidence: every aNN directory has exact command/env, source hashes before/after, raw output and actual exit. a08 additionally retains actual standard-schema input/scripts/raw. a03-execution-input.json is the 377-package/2348-file nonrace baseline; stage-a-final-input.json records the actual race graph's 11 additional files, current driver and binary hashes, and explicitly identifies the sole changed baseline helper (the original driver is preserved in a04-pure/runner-original.py). Baseline source/tool/import bytes were rehashed at handoff; all but that intentional driver repair match. No source tree/cache copy was made.

Final input SHA256 ba247577940319f250b2a5d409848db6c569e38aa4e5d8bdaa5f82ab2ee4a3e0; final state SHA256 199222a69c75431bf9c596942f2e6c985fb9ca9fe009baed9c7c8aa0feedf7f1.

Unverified/unimplemented here: HTTP dispatch/HEAD execution, two-second preauthentication deadline and actual native I/O tails, PG/current Owner transactions/cursors, root construction/initialization/real binding, real fixtures and product integration. Stages B–D and README remain outside Stage A. The pure SQL-row Account fixture proves boundary composition only, not persisted authorization. Production Invocations/Facts remain unbound; no resources/listeners/containers/real database were used. No Git writes were performed.
