# D09 Invocation / Usage recovery verification plan

Prepared 2026-10-06 against main d09e8ef294d8c51a8f82ec7d536fb573753ed42a. This is a plan and environment preparation record, not acceptance. Product sources are still owned by backend_recovery and will not be tested or reviewed as final input until frozen.

## Authority and input

- Apply the accepted ledger card, including the PG correction: PostgreSQL 17.x at least 17.8 for both fresh 1..18 and populated 1..17 to 18; PG16 is only an unsupported-version test. Preserve the historical plan's mistaken PG16 populated wording without editing old evidence.
- Accept a precise frozen revision for the card's 20 paths, its tests, required immutable source/assets, and the relevant module/fixture inputs. Record commit plus file SHA-256, actual command argv/environment/exit and raw output. Re-match final implementation before integration.
- Original wire03 (three failures/four passes, SQLSTATE 2201B) and schema02 are presently represented by persistent administrative statements only; their raw run logs and frozen source are not present in the inspected checkout or previous /tmp location. Request exact archive paths from the author. Do not claim those runs independently verified; rerun affected schema after SQL changes.

## Gates

1. Inspect frozen production changes for same Store/Tx, private issuer binding, complete lock union, provider mapping/current authority, historical exact receipt, loader validation, one-statement atomic three-table effects, nullable/numeric overflow and cursor/query budget.
2. Check frozen author provider and evidence for actual canonical rows/held locks and exact Exchange Decision/Joined/usage/request counts. Reject manufactured current authorization, fake material/join, and SQL fixtures presented as wire evidence.
3. Reuse applicable author results for all eight TestUsage groups only when command, input and environment match. Require corrected real PG schema tests, wire failures/fixes, nonempty pagination/aggregation, rebuild concurrency, malformed persistent data, SQL EXPLAIN and 2s total budget.
4. Run independent A plus B from the accepted plan: A current Session revocation versus original technical finalization; B1 intentional ignored entry error with exact outer state and unchanged three-table facts; B2 actual commit decorated Unknown, historical Action/Sequence, original writer-lock bounded blocking, exact receipt after release and zero resend. Freeze probes/selector/expected errors before running. Add C nonempty aggregate/rebuild only if author evidence leaves that gap.
5. Verify affected pure unit/race/vet, all integration source compilation, both command builds and relevant existing regression selectors; reuse unchanged adequate proof. Never count filtered no-tests packages as covered.

## Execution shape

Go must be /workspace/toolchains/go1.27.1/bin/go with GOTOOLCHAIN=local, GOENV=off, GOWORK=off, -mod=readonly, GOPROXY=off, GOSUMDB=off and a task-specific GOCACHE/TMPDIR. Dependency downloads happen only in the task resource preparation directory before test runs.

Real tests use the original scripts/test-objects.sh driver with explicit TestUsage selectors, GOFLAGS='-mod=readonly -p=1', -race -count=1 -timeout=6m. scripts/test-models.sh selects only ^TestModel and does not run the eight ledger groups. The driver starts four nonce-owned containers and three networks, including PG17/vector, unsupported PG16, MinIO and controlled outbound. No fixture or source test has yet been started by this verifier.

## Docker queue and cleanup

Root updated scheduling to ready-first, with ledger's first real run taking priority once ready. Each window has one explicit owner; a run is allowed only after its input freezes and the preceding owner reports actual command termination and cleanup. The current window and each handoff are recorded in the private docker-*.json files. Record baseline ID/name/labels, observe exact live nonce resources, then check each owned ID absent twice, baseline unchanged, runtime empty and own process count zero. Never connect to or mutate the existing onboarding PostgreSQL/MinIO containers.

## Limitations

This verifies only the library ledger and queries. Production Runtime Facts, call/provider orchestration, credential read/release routing, Process/lease/lifecycle and HTTP/root remain unbound. Commit Unknown decoration is not a network ACK-loss experiment. Object join/Artifact blockers, the undecided Summary product rule and ready=false/503 remain outside this result.
