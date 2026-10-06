# D09 Invocation/Usage independent verification

**Accepted for the authorized Invocation/Usage ledger library/schema card.** The fixed input13 eleven-group independent run and the final gate-only/current-upstream three-group composition both passed. No unresolved finding remains in this scope. This is not complete D09 or production Runtime/root integration.

## Fixed scope and provenance

- Reviewed card: `docs/development/work-items/recovery-d09-invocation-usage-ledger.md`, including the corrected PG17.x >=17.8 requirement for both fresh and populated migration success.
- Final ledger author input: `/workspace/scratch/agenteam-d09-ledger-author/input13/manifest.json`, SHA `b0dbeefe78312c2b2ca81f7e6de80fc8cde6b4f3354cfd3d6c7497a847b3009e`; baseline `d09e8ef`, 20 owned paths and 871 source/runtime-asset closure paths.
- Original core01 records `e87c6ed` as its actual baseline; it differs from d09e8ef only in recovery documentation. Its original manifest was not rewritten. The independent 87-path pure dependency proof excludes the concurrent management implementation.
- All real tests used immutable minimal source closures, not the active shared checkout. No product, shared task ledger or Git mutation was made by the verifier. Private probes were compiled and overlaid; they are not product changes.
- The vanished old wire03/schema02 raw evidence was not reused. Administrative historical records and the old mistaken PG16-populated wording remain historical evidence only.

## Findings and disposition

| Finding | Evidence and disposition |
| --- | --- |
| U-R01: nullable terminal_version escaped a SQL CHECK through UNKNOWN | Core01 static finding. SQL18 now explicitly requires non-NULL terminal_version in the terminal branch. Author real01 exercised the actual 23514 negative and both PG17 migration paths. |
| U-F01: SecretService identity confused LeaseOwner CauseRef and Invocation RequestID | Author fixture repair reviewed before dynamic runs. Exact owner ID and invocation request identity now consume original canonical rows and complete actual writer locks. Real01 and real02 authorization cases passed. |
| U-F02: not-sent lacked a real denied Do/Joined witness | Repaired test uses actual D04 policy denial, exact Decision/DoStarted, Joined and zero Provider requests. Author real01 passed. Own two-second derived-context exhaustion now follows actual authorized SQL and preserves zero DTO. |
| U-F03: overflow fixture also corrupted the final digest | Separated bad-digest rejection from a consistent numeric overflow fixture. Both safety cases passed. Earlier claim that digest validation necessarily preceded Aggregate numeric scanning was explicitly corrected. |
| Legacy Secret candidate lock probing could poison Tx | Author repaired before first dynamic run: inspect exact current advisory locks before full RequireHeldLocks, then reread canonical facts under the full lock union. Shared-owner two-call positive case passed. |
| U-R02: five read APIs published data after actual commit followed by caller cancellation | Independent original input07 failures retained in publication01 and perf-lookup01. Input11+ shared completedRead preserves original noncommitted/Unknown outcomes, checks ctx after Committed and returns zero DTO. The exact four-reader and Lookup probes passed on input13 in final01. |
| Author real01 index assertion ran before ANALYZE | Original exit1 retained. Normal statistics added; no planner forcing, smaller fixture or larger budget. Real02 complete 1201-row Aggregate passed at 1.361191577s; independent profile measured 1.378896329s. See performance-report.md. |
| Author real02 treated a deleted Session as revoked | Original exit1 retained: actual Unauthenticated and zero DTO. Input13 calls formal Account.Logout and verifies committed command, typed event and SessionRevoked, then old-cursor rejection/new-Session continuation. The full Reader top passed in independent final01. |

The verifier's original, unexecuted A probe also had the DELETE/SessionRevoked mismatch. Its old source/hash and compilation record are preserved in probe-archive. The executed A uses the existing complete administrative revocation fact fixture and real Account authority. Formal Logout is separately covered by the author Reader test. The final A/B probe also joins its writer-lock goroutine on cleanup.

## Actual independent runs

All invocations use the unmodified `sh scripts/test-objects.sh -run <fixed selector>`, Go1.27.1, race/count1/6m, readonly offline modules and a private cache/TMPDIR. Exact argv/env/input/probe hashes are in each run's command.json. No compile-only result is counted as runtime compatibility.

| Run | Input | Actual driver result | Meaning |
| --- | --- | --- | --- |
| publication01 | original input07 + private four-reader probe | exit1 | All four real committed-then-cancelled readers wrongly published a DTO. Preserved U-R02 counterexample. |
| perf-lookup01 | original input07 + private Lookup and timing probes | exit1 | Lookup original counterexample failed; complete Aggregate timing top passed. Profile and EXPLAIN are preserved separately. |
| final01 | input13 + three private probe files | **exit0, 11 exact top-level PASS, zero skips** | Four independent tops, six original compatibility tops, and one repaired author Reader top. Source/probe hashes unchanged. |
| combined01 | unchanged ledger20 + committed ecd7337 management13 + private gate probe | **exit0, 3 exact top-level PASS, zero skips** | Isolated current-gate rejection and two actual upstream compatibility seams. All 880 closure/probe hashes unchanged. |

final01 independent observations:

- Current admission: valid old reserve and authorized plans both reject revoked Session with NotCommitted, empty receipt and byte-equal invocation/observation/summary tables. An actually sent Exchange still records observation and final after revocation/archive, actually joins, preserves 9 input / 0 output tokens, and makes exactly one Provider request.
- Atomicity: exact inner INVALID_STATE from summary-version overflow is deliberately swallowed; the real outer transaction commits, while the receipt remains empty and all three ledger tables remain byte-equal.
- Historical Unknown: after a real wire request, the real observation transaction commits and its result is decorated Unknown with the original cause/attempt. Exact Lookup confirms the original sent receipt; after later final it still returns that Action/Sequence. An original writer lock blocks confirmation for about 91ms under an 80ms caller context, returns error/zero result, then actually joins; subsequent receipt and all tables are unchanged. No resend occurs. This is a commit-result decorator, not a claim of packet-level COMMIT ACK loss.
- Publication cancellation: actual committed List, Aggregate, GetExecutionSummary, RebuildExecutionSummary and LookupInvocation all receive caller and derived-context cancellation before publication, return DependencyUnavailable with the cancellation cause, and expose zero DTO. Rebuild's durable repair still committed. Shared pure checks preserve Committed state and earlier NotCommitted/Unknown identities.
- Author Reader test: formal Logout revokes only the old Session; old signed cursor is rejected, same User's different current Session continues at a different limit, and both signed cursors retain their original upper bound and effective 30-day range. All filters/eight groups, null groups, no-Execution summary exclusion, explicit 40-day historical statistical backfill and archive/deleting read gates pass. Historical backfill is marked synthetic; it is not a claim of a real send 40 days ago.

The six original compatibility tops actually passed: ModelCurrentResolutionAuthorization, ModelCurrentResolutionCanonicalLease, ModelProjectCRUDScopeAndCanonicalReceipts, SecretModelUsagePlannedReadAndAudit, ModelOpenAIChatWireHTTP, ModelOpenAIChatStructuredHTTP. Their full `Test` names and source definitions are in final-plan01.json. The repaired Reader is an author test executed by the independent worker; there was no separate third author fixture run.

Author matrix reused by semantic identity: input07 real01 covers wire, replay/atomicity and schema/history; input11 real02 covers current authorization, summary, query safety/budget and Unknown after the common read repair. Input13 differs from input11 only in the repaired Session fixture and Reader test, whose final result is above. Original failed driver exits are retained; passing child groups are individually attributed rather than labelling those invocations all-pass.

## Final composition checkpoint

Root requested one additional genuine upstream integration check because committed management reads change Secret.Metadata and Model HTTP relative to the author's fixed baseline. `combined-input01/manifest.json` SHA `8d97aef145a9582ac35444c9d7ba64688f0c74baec637f4bcbeba07e1ab30b23` combines unchanged input13 ledger20 with exactly 13 files read from Git commit `ecd733711caff5df46e423cadab52b32c34f785e`, matching accepted management input05 hashes. It contains 880 source/runtime-asset paths. Relevant committed deltas from d09e8ef to ecd7337 are exactly these 13 paths; none remains from ecd7337 to the recorded HEAD. No active tools-wire source was consumed.

compile07 passed with race/integration tags. combined-plan01 selected exactly TestIndependentUsageCurrentProjectGate, TestUsageInvocationWireLedger and TestSystemModelManagementMetadata; **all three actually passed with original driver exit0 and no skips**. The private gate probe keeps Session and canonical Runtime facts unchanged, accepts real BeginArchive/BeginDeleteProject gates, then tests both old reserve/authorized plans for ProjectNotActive, NotCommitted, empty receipt and unchanged three-table facts. Both archiving and deleting subcases passed. This closes the gap that combined Session+gate revocation cannot isolate. The real ledger Exchange chain and current management Metadata also passed against the changed Secret.Metadata/model dependencies. At final cleanup, all 20 live owned ledger paths still matched input13.

## Environment and cleanup

Verified Go1.27.1 binary, prescribed PG17 image (actual server170008, vector0.8.1), PG16 unsupported image (actual160012), and source-built MinIO exact SHA are indexed by ../environment.json and ../handoff.md. Real fixtures use task-owned nonce/labels and exact IDs, never existing infrastructure or external credentials.

publication01, perf-lookup01, final01 and combined01 each recorded four owned containers/three networks, continuous PID/starttime ownership, actual driver completion, two exact-ID absence checks, unchanged original 2-container/4-network ID/name/label baseline, zero owned processes and empty private runtime/TMPDIR. All monitor error lists are empty. Coordinator independently rechecked exact IDs before handoff. final01 returned at 2026-10-06T01:31:38.677039+00:00; its result-summary.json identifies all 11 actual passes. combined01 returned at 2026-10-06T01:36:14.310678+00:00, after the same double cleanup and coordinator recheck. Its result-summary.json identifies the three actual passes and unchanged live ledger20.

Author real01 predates the continuous process observer; its original records prove driver/Docker/tmp cleanup and a separate independent /proc check found no remaining author process. The later tree-monitor evidence is not retroactively attributed to that old run.

## Boundaries

This is a library/schema result for the authorized ledger card, not complete D09 or production deployment. Production InvocationFacts/Runtime orchestration, real Provider dispatch/retry scheduling, lease/Process/lifecycle/HTTP/default-root binding remain unbound. Runtime facts in tests are private durable canonical test records validated in the actual Store/Tx under full locks. Migration success proves task-owned PG17 fresh/populated rollback-and-same-bytes-retry behavior, not a deployment. Summary defaults remain undecided, ready remains false/503, and Object join/Artifact restrictions remain unchanged.
