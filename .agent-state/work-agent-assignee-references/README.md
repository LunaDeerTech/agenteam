# Work Agent assignee reference checks

This profile validates the narrow same-transaction Work fact port in [the task card](../../docs/development/work-items/d08-work-agent-assignee-references.md). Its immutable method baseline is delivered main `987ca6c42fb2c316fe8ce6564711f2afdedfbd75`. Earlier App/core results are dependencies, not results for this profile. After the preserved basic01 failure and a test-only fixture correction, basic02, controls, compile/list/prepare and native01 completed with their original actual Wait results at tested source `f9548b17e33c8fdab474f9dc6148ff713e2dd8aa`. Final documentation `3bb16f8e6c6380cabef70f8b986183688a04ec7a` was normally merged into and pushed as main `60d7e7487b3b64b593cddf5d32ec5e19642557d5`; evidence preservation and ordinary cleanup are recorded below.

## Basic scope

The selection is exactly **five new tops / 32 subtests**, race for `./internal/central/work` and vet for that package plus `./internal/central/work/contract`. The [dedicated controls](../../internal/central/work/agent_assignee_references_test.go) are:

- `TestWorkAgentAssigneeReferenceInputs` — 10 subtests.
- `TestWorkAgentAssigneeReferenceOriginalTransactionAndLocks` — 10 subtests.
- `TestWorkAgentAssigneeReferenceOwnerReadBeforeTask` — seven subtests.
- `TestWorkAgentAssigneeReferencePredicateAndFailure` — three subtests.
- `TestWorkAgentAssigneeReferenceWaitsForOriginalReturn` — two subtests.

No old Task, Scheduler, Execution, migration-loader or App suite is added. Existing pure identity/error helpers in `structure_test.go` remain part of the actual same-package compiler closure. The original basic algorithm retains race/plain compiler dependency discovery, complete input-map comparison, exact selected top/subtest run/pass counts and each original Popen/Wait. Controlled Store/Project/Scan ports prove only those controlled boundaries, not real PostgreSQL locks, COMMIT or lifecycle completion. The actual basic02 JSON confirms this exact selection; its results are recorded below.

## Native scope

The fixed selector is `^TestWorkAgentAssigneeReferences$`, one top with three subtests:

- `same-transaction-current-and-terminal-references`
- `authority-and-transaction-boundaries`
- `caller-lock-release-before-original-return`

The [new PG fixture](../../tests/projectvariable/work_agent_assignee_references_test.go) uses actual nonempty Project, Agent and Task setup through the original providers. The existing Work Authority borrows the caller's live same-Store transaction and verifies held User SH, Project SH, ProjectSchedule EX and Agent EX before original Owner Read and the complete current assignee predicate. Assigned terminal Tasks also count. Done/cancelled coverage uses exact owned-transaction SQL state fixtures, preserving the assignee and full lock union, followed by a fixed sentinel rollback and original Task/history/claim equality. It is not a production terminal transition, cancellation or Execution completion result.

An unassigned Agent is a real positive case. Invalid/unsupported actor, ownership, Project and original transaction boundaries must reject. Work-scoped nil does not establish canonical Agent existence, same-Project membership or deletion authority; a caller needing those facts first uses the real Agent current port in the same transaction. The Work query does not read Agent tables or issue a reusable grant.

The lock case uses two readonly callers with different Agent EX locks and the same Schedule EX. Real backend PIDs, `pg_locks` and blocking relationships establish that B waits in its own `AcquireAll` before entering the Work port while A's original transaction remains open. A's physical COMMIT then permits B to acquire the lock and finish even while A's original Store return is still held. Finally A's original return is released and collected. Callback completion, physical COMMIT and original wrapper return are distinct observations. This is not a Task writer, Scheduler/Execution owner, foreign-death proof or global Joined result; all original calls and connections still require actual cleanup.

## Methods

Only root schedules actual Go and native windows. These are recipes, not execution evidence; use fresh output and namespace paths and preserve every original FAIL and Wait record.

```sh
python3 -B .agent-state/work-agent-assignee-references/profile-controls.py
python3 -B .agent-state/work-agent-assignee-references/run-basic-checks.py --output output/ai/work-agent-assignee-references/basic01
python3 -B .agent-state/work-agent-assignee-references/run-pg-checks.py compile --output output/ai/work-agent-assignee-references/compile01
python3 -B .agent-state/work-agent-assignee-references/run-pg-checks.py prepare --compile output/ai/work-agent-assignee-references/compile01 --output output/ai/work-agent-assignee-references/native01 --namespace /tmp/wagr01
```

The [PG wrapper](run-pg-checks.py) retains the accepted single-PG-binary algorithm: offline readonly race compilation with `-p=1`, every actual same-package Go source and all existing migration embeds through 75, original Wait, exact selector listing, and complete compile/runtime maps. Runtime inputs equal the compile inputs plus that single original binary. No new child environment, Runner descriptor, second binary or build inside native is introduced. There is no new migration in this task.

`prepare` emits a request only. Native runs `python3 -B output/ai/work-agent-assignee-references/native01/record-native.py` without extra arguments in a separately authorized window. The [recorder](record-native.py) is byte-identical to the accepted original (`56a32b0d45a633c3c85306d098730318327c02f4ee9c5267c8d712e9880d4f20`). Preparation binds the actual invocation, plan, input map and recorder hashes; it is not native execution.

The [profile controls](profile-controls.py) add only this selector's closed key to each original shared map. Removing that key must leave the entire shared AST and all effective old maps equal to the baseline above, including all Model, Agent, Runner, Variables core and App profiles, the complete Runner descriptor helper and its two selector branches. The original one-positive/seven-rejection metadata controls remain. The supervisor remains the sole owner of seven fixture resources, the Go 6-minute deadline, 540+60-second outer budgets, actual Wait and cleanup, descendant/private-runtime observations and the automatic 75-second host-TCP baseline-delta gate. No filtering, extra supervisor, hidden retry or budget refresh is added.

Source checkpoints, input maps and actual process/tool returns remain separate evidence. The fixed toolchain, protected module/cache and MinIO paths are external inputs, not Git-recoverable source bytes. These methods do not move evidence or mutate Git. The completed checks establish this Work-owned fact port only: no Agent/Project deletion, reference retirement, new App/HTTP wiring, lifecycle participant/Initializer, scheduling stop, Model execution or other existing STOP capability follows.

## Actual validation

The original basic01 failure remains intact. At source `f92d787c1ada9dc79c876610ec7e9adc0d6b887a`, all five tops and 32 subtests failed with `INVALID_ARGUMENT` while constructing `newAssigneeRefFixture`, before entering the new Work port; vet, controls, compile and native had not started. Static diagnosis established that the two fixture name literals `assignee reference` violate the existing `NormalizeName` rule. The original log contains the failure, not an internal execution trace establishing that diagnosis. The sole source correction changed those two test literals to `assignee-reference`; product, methods, assertions and budgets did not change. This was a fixture input defect, not a demonstrated product defect. Basic02 used a fresh output directory and the corrected source above.

The following are original runs. `tool-observation.json` is an assistant-observed transcription of actual tool returns, not a provider-native export. Input equality refers to each original frozen run; final-document edits do not claim the current README retains its historical outer-map hash.

| Original run | Actual Wait and selection | Original evidence |
| --- | --- | --- |
| [basic01 FAIL](../../output/ai/retired-20261012/work-agent-assignee-references/basic01-outer/tool-observation.json) | `22945/cfb42d` actual 1; outer PID 1298902, method PID 1298927 Wait 1 / 55.770 s; race PID 1299038 Wait 1 / 53.833673 s. Five tops / 32 subtests ran and failed; none passed or skipped; vet did not run. | [Post-check](../../output/ai/retired-20261012/work-agent-assignee-references/basic01-outer/post-check.json): 459 closure inputs, four completed maps and 13 outer inputs unchanged; actual union 461; no race warning. No subsequent stage ran from this failed result. |
| [basic02 PASS](../../output/ai/retired-20261012/work-agent-assignee-references/basic02-outer/tool-observation.json) | `52911/263b16` actual 0; outer PID 1301796, method PID 1301821 Wait 0 / 67.498 s; race PID 1301933 Wait 0 / 28.298297 s; vet PID 1302224 Wait 0 / 35.834012 s. Exact five tops / 32 subtests pass once, Work race and two package vets. | [Post-check](../../output/ai/retired-20261012/work-agent-assignee-references/basic02-outer/post-check.json): all 459 inputs across six maps and 13 outer inputs unchanged, actual union 461; no fail, skip or race warning; basic01 FAIL retained. |
| [sequence01 PASS](../../output/ai/retired-20261012/work-agent-assignee-references/sequence01/tool-observation.json) | `38060/47fa54` actual 0; outer PID 1303221 / 152.261 s. Controls PID 1303268 Wait 0 / 0.538 s: one positive and seven rejections. Compile wrapper PID 1303335 Wait 0 / 150.368 s, actual Go PID 1303338 Wait 0 / 146.637 s; exact list PID 1305214 Wait 0 / 1.059 s; prepare PID 1305283 Wait 0 / 1.013 s. | [Post-check](../../output/ai/retired-20261012/work-agent-assignee-references/sequence01/post-check.json): 2,073 compile inputs across three maps; only the original binary adds the 2,074th runtime input; 33 outer inputs unchanged, all 75 migrations and 102 same-package Go sources included. This sequence prepared the request; native is the separate run below. |
| [native01 PASS](../../output/ai/retired-20261012/work-agent-assignee-references/native01/tool-observation.json) | `16513/a3e934` actual 0; supervisor PID 1305989 Wait 0 / 137.123 s, driver PID 1305990 and Go PID 1307604 Wait 0. Exact one top / three subtests pass in 40.28 s (12.75 / 14.03 / 13.50 s). | [Post-check](../../output/ai/retired-20261012/work-agent-assignee-references/native01/post-check.json): all 2,074 runtime inputs and the four input/invocation/plan/recorder identities unchanged at the tested HEAD; seven resource IDs absent in both observations (14 checks), descendants/private/runtime empty twice, both automatic TCP deltas empty and no race warning. |

The [single binary record](../../output/ai/retired-20261012/work-agent-assignee-references/compile01/binary.json) binds `compile01/projectvariable.test`, 92,792,811 bytes, SHA-256 `e7c6f5d33a711d13ddf17829367a37261244569cc4f43fc3a16a0ff5e1c935b3`. Original native raw output is `/tmp/wagr01/pg-fe6fd1323720481eadd51c25b161446b.log`; its same-nonce directory retains the original owned/request records. The supervisor log's 136.143 s and the recorder's 137.123 s measure separate layers of this one native attempt.

Actual PG markers establish two real Agents, formal Task assignment, an unassigned current Agent positive case, complete current-assignee rejection including done/cancelled query fixtures with exact rollback/history preservation, and the original authority/transaction boundaries. The third marker establishes the exact Schedule lock wait and both original returns, with B returning after A's physical COMMIT but before A's held Store return. Both callers are readonly; these observations do not prove a Task writer, Scheduler, Execution or foreign process has Joined. The target Project/Agents use `newTaskTransitionFixture` → `newAgentCreateFixture` and real Skill/P2 initialization. The inherited base uses a persistent test Skills receipt for the other Project in the cross-Project responsibility case. This is neither Project-creation HTTP nor a complete production Skills acceptance claim. Work-scoped nil remains neither Agent existence nor retirement authority; no existing STOP is lifted.


## Main delivery and retirement

The normal no-ff merge retains initial card `156ce126`, source checkpoint `fb97d34d`, failed tested source `f92d787c` / test SHA `c589c04d`, the two-literal test correction `f9548b17` and final documentation `3bb16f8e` as ancestors of main `60d7e7487b3b64b593cddf5d32ec5e19642557d5`. All delivered technical blobs equal tested `f9548b17`. Main push and fresh/tracking verification completed with original `88282/3e0e84` actual 0; the feature branch was never published for cleanup.

[Preservation](../../output/ai/retired-20261012/work-agent-assignee-references-preservation/result.json) completed with original `1837/de0e07` actual 0, outer PID 1315026 and child PID 1315029 Wait 0 / 3.699 s. Same-filesystem NOREPLACE retained 120 files / 99,747,633 bytes / 13 directories with identical path hashes and stat/inode fields. The [source history](../../output/ai/retired-20261012/work-agent-assignee-references-preservation/source-history.json) resolves 2076 repository versions from 31 input maps through eight main ancestors, including the original failing test and corrected source. The single PG binary, all original FAIL/PASS records, 14 Wait fields and four identities remain unchanged; no reconstructed-source exception applies. Four tmp files were copied exclusively (8232 bytes), with originals retained. Two external tools are protected by original path/hash/stat, not archived or claimed recoverable from Git. Original tool receipts remain assistant-observed transcriptions; known-PID/owner checks do not claim global FD visibility.

The [first read-only plan attempt](../../output/ai/variables-review-drafts/work-agent-assignee-delivery02/tool-observation.json), original `76122/15de0b` actual 1, stopped at an old App binder's absent `post.status` field before any preservation. Its original script and artifacts remain in `work-agent-assignee-delivery02`, with small failure copies in the preservation proof. [The subsequent schema adaptation](../../output/ai/retired-20261012/work-agent-assignee-references-preservation/schema-adaptation.json) changes only the exact Work result/Wait/four-identity field shapes; runtime accepted those semantics and concrete plan03 (`30026/6274af` actual 0). Neither result overwrites the original plan failure or product basic01 failure.

After independent actual post `9afec6` accepted preservation, [ordinary cleanup](../../output/ai/work-agent-assignee-references-delivery/cleanup01/result.json) completed with original `60325/28e6b6` actual 0: worktree remove, local branch `-d`, fresh remote absence before/after, explicit main/ai fetch-prune and empty local/tracking refs. No remote was created or deleted. All original scope limits, prerequisite failures and STOPs remain in force.
