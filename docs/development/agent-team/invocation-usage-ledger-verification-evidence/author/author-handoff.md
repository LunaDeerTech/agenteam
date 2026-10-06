# D09 Invocation/Usage ledger — author handoff

Status: implementation and applicable author/independent verification are complete. Independent final01 passed all 11 expected groups and the final committed-upstream combination passed all three expected groups; the verifier reports the ledger card acceptable for root integration. The final owned input is `input13/manifest.json`, SHA-256 `b0dbeefe78312c2b2ca81f7e6de80fc8cde6b4f3354cfd3d6c7497a847b3009e`. All 20 owned live paths match this input. Production bytes have not changed after input11. The repaired formal-Logout Reader test passed in the independent window; it is an author test executed by the independent verifier, not a third author fixture run. No author Git action was taken.

## Authorization and recovery

- Role: backend_worker; no delegation, Git mutations, shared ledgers, old helpers or out-of-scope product edits.
- Read skills: `.agents/skills/agenteam-design/SKILL.md`, `.agents/skills/agenteam-go-development/SKILL.md`.
- Read AGENTS/team workflow, development plan/tasks, ledger card through its implementation authorization and PG correction, recovery record through §32, D09 design and relevant C0/Model/Secret/Project/Account/D04 contracts and implementations.
- The old temporary implementation and raw wire03/schema02 logs were absent. Persistent specs authorized recovery of exactly 20 paths; old reported passes were not treated as execution evidence for recovered code.
- Full frozen inputs use Git baseline `d09e8ef` plus the owned overlay and necessary backend/test/script/runtime assets. There are 871 hashed source/runtime-asset paths, including the original `api/openapi/model-system.json`. Later snapshots hardlink unchanged bytes; none copies the full working tree or consumes other agents' dirty files.
- Earlier `core-freeze-01` records its actual baseline `e87c6ed66b07d58f103b0e4b5c9239fb4db5617a`; it is not rewritten. Its 14-path successor `core-freeze-02/manifest.json` SHA is `73c3859654608679eeb3b8839010fcd1a8c71b259d1077a6b64f56066ac8fa28`.

## Delivered behavior

- Trusted InvocationFacts, opaque issuer-bound plans, full Actor and immutable invocation identity, same Store/Tx and complete lock union, exact current-owner reader authorization.
- Reservation/observation/finalization with immutable headers, ordered events, stable historical receipts, semantic replay, atomic invocation/receipt/summary SQL and checked nullable counters.
- Current Human owner List/Aggregate/Get/Rebuild, signed bounded cursor positions, fixed default 30-day range, exact filters/eight groups and two-second budgets.
- Exact bounded Lookup for original Action/Sequence, preserving actual DB Unknown/commit identities independently of dispatch unknown and never authorizing resend.
- Migration 00018 has three tables, constraints, history/live-FK behavior and indexes. Safe token validation uses alphabet checks plus length checks instead of PostgreSQL ARE bounds above 255.
- Private test Runtime facts are canonical durable rows, reread through the same real Store/Tx/full held-lock proof. Real Account/Project/Model/Secret/Audit and D04 Exchange supply positive evidence. Not-sent uses an actual policy-denied, joined Do. Only dispatch unknown and explicitly marked statistical backfills are protocol/statistical fixtures.

## Preserved defects and fixes

| Finding | Original evidence | Fix and current evidence |
| --- | --- | --- |
| U-R01: terminal_version NULL could satisfy CHECK as UNKNOWN | Independent static core-freeze-01 | Explicit non-NULL branch in SQL18; author real01 Schema group passed the real negative constraint case. |
| U-F01: SecretService CauseRef incorrectly used invocation ID | Independent input01 static review | CauseRef is exact LeaseOwner.ID; RequestID is invocation ID. Planner rereads canonical exact attempt/ref/lease/input/gate, with full real locks. Real01 and real02 authorization passed. |
| U-F02: boolean-only not-sent and insufficient service-budget evidence | Independent input01 static review | Actual D04 denied Do and Joined, zero server requests; real callback after authorized SQL observes service two-second deadline and zero DTO. Real01/real02 passed. |
| U-F03: overflow fixture retained stale final digest | Independent input01 static review | Overflow statistics retain a consistent recomputed digest; corrupt digest is an independent negative case. Real01/real02 safety cases passed. |
| Summary replay test compared per-read AsOf | Independent input05 review | Checks valid monotonic DB AsOf, normalizes it before comparing persisted projection/version semantics. Real01 replay passed. |
| Candidate-lock probing poisoned the transaction | Author static review before real01 | Uses both AdvisoryKey halves and exact backend/current database/advisory/ExclusiveLock/granted/objsubid=1 to locate a held original writer, then full RequireHeldLocks and canonical validation. Shared-lease two-call real negative/positive passed. |
| real01 expected a specific index before fixture ANALYZE | Author real01 raw exit1 | Added normal ANALYZE and measured API-only logging; no forced planner, reduced sample, larger budget or relaxed validation. real02 QueryBudget passed with project_page Index Only Scan. |
| U-R02: post-commit caller cancellation published nonzero candidates | Independent publication01: four real API reds on input07; perf-lookup01 later confirmed Lookup too | completedRead first retains actual CommitResult/Unknown identity, then checks the original derived ctx and returns Committed+DependencyUnavailable with cancellation cause and zero DTO. List/Aggregate/Get/Rebuild/Lookup all use it. Pure/race/vet/compile passed; independent final01 repeated all five actual-commit/cancel probes and passed. |
| real02: deleted Session expected SessionRevoked | Author real02 raw exit1, actual Unauthenticated and zero DTO | Input12 retained as the unexecuted error-code correction. Input13 invokes the real Account Logout command, verifies SessionRevoked plus committed command/typed event, and preserves old cursor rejection/new Session continuation/signed bound-range assertions. All passed in independent final01. |

All original frozen inputs, raw failures and metadata remain unchanged.

## Pure checks and compilation

Exact argv/env/start/end/exit/hash data and original stdout are paired under `logs/<label>.json` and `.log`.

| Label | Actual result | Scope/input |
| --- | --- | --- |
| pure-01 | exit0 | usage packages and identity contract; initial per-file hash metadata retained. |
| compile-01 | exit1 | Initial fixture decorator missing Acquire; original per-file inputs retained. |
| compile-02 | exit0 | All `./...` integration compilation on input01. |
| compile-03..09 | exit0 each | Affected model integration compilation after input deltas. |
| unit-race-01 | exit1 | usage/usage contract/identity/model adapter/model contract passed; old Model HTTP unit failed only because the minimal closure omitted its runtime OpenAPI file. |
| unit-race-02 | exit0 | Affected Model package after restoring exact baseline OpenAPI bytes in input04. |
| vet-01 | exit0 | usage, identity contract, model packages on input02. |
| build-01 | exit0 | Both command binaries on input02. |
| unit-race-03 | exit0 | usage packages after four-reader U-R02 repair in input10. |
| unit-race-04 / vet-02 / compile-11 | exit0 each | Final production input11 including Lookup repair. |
| compile-13 | exit0 | Input13 formal Logout reader fixture/test. |

The old unit failure is an input-closure asset failure, not a claimed business regression. Build/unchanged package checks are reused only where production/dependency semantics did not change. `git diff --check` passed for tracked owned paths; all owned Go is gofmt formatted.

## Actual author fixture runs

Both runs used the original `sh scripts/test-objects.sh -run <explicit selector>` driver, Go1.27.1, race/count1/timeout6m and `-p=1`, offline readonly modules, exact prescribed PG17.8/PG16.12 images and exact MinIO checksum. `scripts/test-models.sh` is unsuitable because its fixed `^TestModel` selector misses all eight Usage groups.

`real-01`, input07 SHA `ec8a86c784d7ff64110ac774cb480de4c8a0492d72fdc0f4b974308f74729ff4`, exit1:

```text
^TestUsage(InvocationWireLedger|InvocationIdentityAndAuthority|InvocationReplayAndAtomicity|InvocationUnknownConfirmation|ReaderNonemptyPagination|ExecutionSummaryRebuild|SchemaAndHistory|QueryBudgetAndSafety)$
```

Seven top-level groups passed. Only QueryBudget failed, solely its index-choice assertion before ANALYZE. Its actual 1201-row aggregate completed correctly within the two-second API assertion; the printed 1.92 seconds is the containing subtest including EXPLAIN, not a separately recorded API duration. All shorter-caller/service-deadline, safe projection, bad JSON/digest and numeric overflow subcases passed. Model package duration 48.857s.

`real-02`, input11 SHA `f6b9847f0fb69ff9ad5400ccb3f75c0eb383d53821be2aebae865b11eae2c7eb`, exit1:

```text
^TestUsage(InvocationIdentityAndAuthority|ReaderNonemptyPagination|ExecutionSummaryRebuild|QueryBudgetAndSafety|InvocationUnknownConfirmation)$
```

Authority, Summary, QueryBudget and Unknown passed. Reader failed only at the newly added missing-Session error-code assertion; subsequent default signed-range/new-Session continuation assertions in that subcase did not run. Its other Reader subcases passed, including explicit historical range and no-Execution/no-summary. Model package duration 24.390s.

QueryBudget API-only measured duration: **1.361191577s**, 1201 confirmed invocations and input sum 1201, project_page Index Only Scan after ANALYZE. Service-deadline callbacks performed actual authorized SQL before expiring the original two-second derived context, returned zero DTO and actual NotCommitted. Observed wall time includes scheduling/return overhead (~2.008s), not a raised configured budget.

Independent perf-lookup01 on original production input07 plus its private ANALYZE fixture reports a separately measured API **1.378896329s**, SQL EXPLAIN execution roughly 4.12ms total, with most time in client transfer/strict JSON/digest checks under race instrumentation. This single profiled sample is not a guarantee for all workloads. No speculative production performance change was made.

## Independent final01 result

Verifier evidence is `/workspace/scratch/agenteam-d09-ledger-verification-y4jvpxmq/independent/final01/`: original `raw.log`, `command.json`, frozen `input.json`, `result-summary.json`, resource/process observations and two `cleanup.json` observations. The original driver exited **0**, all **11 expected top-level groups passed**, no failure/skip, input13 and private overlay06 hashes stayed fixed. The verifier confirmed seven exact IDs absent twice and again itself, unchanged baseline, owned PID0 and empty runtime, and returned the window at 01:31:38 UTC.

- Four independent groups: current-session rejection and sent technical closure; ignored-inner-error atomicity and real wire/commit-decorated-Unknown historical receipt/writer-lock behavior; Lookup post-commit cancellation; four Reader/Get/Rebuild post-commit cancellation. All five U-R02 APIs retained actual Committed and returned DependencyUnavailable plus zero DTO after cancellation.
- Six unchanged legacy groups: CurrentResolutionAuthorization, CurrentResolutionCanonicalLease, ProjectCRUDScopeAndCanonicalReceipts, SecretModelUsagePlannedReadAndAudit, OpenAIChatWireHTTP and OpenAIChatStructuredHTTP.
- Author `TestUsageReaderNonemptyPagination` passed all subcases, including formal Logout, existing-cursor rejection, same-user replacement Session continuation, signed fixed range, explicit history and no-Execution/no-summary. This closes the sole real02 failure.

The independent review identified an evidence distinction: SessionRevoked-first tests do not isolate gate-only current admission. The final combined run below closed that gap with current Sessions and real lifecycle acceptance.

## Final committed-upstream combination

The verifier's `independent/combined-input01/manifest.json`, SHA **8d97aef145a9582ac35444c9d7ba64688f0c74baec637f4bcbeba07e1ab30b23**, combines unchanged input13 ledger20 with exactly 13 accepted management files from Git `ecd733711caff5df46e423cadab52b32c34f785e`. The closure contains 880 source/runtime assets; no active tools14 source was consumed. The private gate probe and overlay07 hashes are recorded in `independent/combined01/command.json`.

Independent compile07 passed with `go test -c -race -tags=integration ./tests/model`. The original driver then ran:

```text
^(TestIndependentUsageCurrentProjectGate|TestUsageInvocationWireLedger|TestSystemModelManagementMetadata)$
```

The driver exited **0**, all **three top-level groups passed**, no failures or skips. Raw log, command, fixed input, process/resource observations, two cleanup passes and final result are under `independent/combined01/` in the verifier root.

- Gate-only: actual BeginArchive/BeginDeleteProject accepts archiving/deleting gates while Session remains current. Both old reserve and authorized plans return ProjectNotActive/NotCommitted with empty receipts and unchanged Runtime/three ledger tables.
- UsageWireLedger: the complete author wire group passes on the final upstream composition, including reliable usage, explicit zero/NULL, real failure after usage, actual pre-join refusal and zero-send denied Do versus protocol unknown.
- SystemModelManagementMetadata: current metadata/authorization/history behavior passes with the new ledger schema and committed Secret.Metadata implementation.

All input/probe hashes and live20 remained unchanged. Seven exact resource IDs were absent in both cleanup observations and coordinator recheck; baseline stayed unchanged, owned processes/runtime were empty. The window returned at **2026-10-06T01:36:14.310678Z**. No additional production correction was required.

## Resource evidence

- Author real01 and real02 each observed exactly four task containers and three task networks, then confirmed all seven exact IDs absent twice and unchanged pre-existing two-container/four-network baseline (names/labels included).
- Both finished their original driver, left TMPDIR empty, and preserved all frozen closure hashes.
- Real02 additionally records continuous PID/PPID/starttime/cwd/exe observations and descendant trees in `logs/real-02.processes.json`, with `owned_processes_remaining=[]` on both cleanup observations.
- Real01 predates this process observer. Independent verification separately checked the driver gone and no process cwd/exe under the author root; do not imply the original real01 metadata contained the later tree monitor.
- ledger-author-01 and ledger-author-02 were explicitly returned after actual cleanup. No task Docker resource is currently held by the author.

## Delivery boundaries

- Author work is stopped: 20 paths frozen, no author fixture/command/resource remains. Root owns final exact-path staging, commit and push under the existing authorization.
- The final combination's input is separately identified above; final01 is not relabeled as having included the new management source.
- Production InvocationFacts owner, call scheduling/Provider dispatch/retry, lease/Process/lifecycle/HTTP/default-root binding remain unbound. No production migration deployment, complete D09, Summary-default decision, ready503 change, or repair of the known Object join defect is claimed.

## Owned paths

The authoritative 20-path SHA mapping is `input13/manifest.json.owned`:

```text
internal/central/usage/contract/invocations.go
internal/central/usage/contract/authority.go
internal/central/usage/service.go
internal/central/usage/store.go
internal/central/usage/authority.go
internal/central/usage/invocations.go
internal/central/usage/summary.go
internal/central/usage/query.go
internal/central/identity/contract/identity.go
db/migrations/00018_model_invocation_usage.sql
internal/central/identity/contract/identity_test.go
internal/central/usage/contract/invocations_test.go
internal/central/usage/authority_test.go
internal/central/usage/summary_test.go
tests/model/usage_fixture_test.go
tests/model/usage_ledger_test.go
tests/model/usage_authorization_test.go
tests/model/usage_query_test.go
tests/model/usage_unknown_test.go
tests/model/usage_schema_test.go
```
