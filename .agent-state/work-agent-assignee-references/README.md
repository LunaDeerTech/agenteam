# Work Agent assignee reference checks

This profile validates the narrow same-transaction Work fact port in [the task card](../../docs/development/work-items/d08-work-agent-assignee-references.md). Its immutable method baseline is delivered main `987ca6c42fb2c316fe8ce6564711f2afdedfbd75`. Earlier App/core results are dependencies, not results for this profile. These methods have not run; source review and static checks do not establish a basic, compile, prepare or native PASS.

## Basic scope

The selection is exactly **five new tops / 32 subtests**, race for `./internal/central/work` and vet for that package plus `./internal/central/work/contract`. The [dedicated controls](../../internal/central/work/agent_assignee_references_test.go) are:

- `TestWorkAgentAssigneeReferenceInputs` — 10 subtests.
- `TestWorkAgentAssigneeReferenceOriginalTransactionAndLocks` — 10 subtests.
- `TestWorkAgentAssigneeReferenceOwnerReadBeforeTask` — seven subtests.
- `TestWorkAgentAssigneeReferencePredicateAndFailure` — three subtests.
- `TestWorkAgentAssigneeReferenceWaitsForOriginalReturn` — two subtests.

No old Task, Scheduler, Execution, migration-loader or App suite is added. Existing pure identity/error helpers in `structure_test.go` remain part of the actual same-package compiler closure. The original basic algorithm retains race/plain compiler dependency discovery, complete input-map comparison, exact selected top/subtest run/pass counts and each original Popen/Wait. Controlled Store/Project/Scan ports prove only those controlled boundaries, not real PostgreSQL locks, COMMIT or lifecycle completion. These counts describe the frozen source selection, not an executed result.

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
python3 -B .agent-state/work-agent-assignee-references/run-pg-checks.py prepare --compile output/ai/work-agent-assignee-references/compile01 --output output/ai/work-agent-assignee-references/native01 --namespace /tmp/waar01
```

The [PG wrapper](run-pg-checks.py) retains the accepted single-PG-binary algorithm: offline readonly race compilation with `-p=1`, every actual same-package Go source and all existing migration embeds through 75, original Wait, exact selector listing, and complete compile/runtime maps. Runtime inputs equal the compile inputs plus that single original binary. No new child environment, Runner descriptor, second binary or build inside native is introduced. There is no new migration in this task.

`prepare` emits a request only. Native runs `python3 -B output/ai/work-agent-assignee-references/native01/record-native.py` without extra arguments in a separately authorized window. The [recorder](record-native.py) is byte-identical to the accepted original (`56a32b0d45a633c3c85306d098730318327c02f4ee9c5267c8d712e9880d4f20`). Preparation binds the actual invocation, plan, input map and recorder hashes; it is not native execution.

The [profile controls](profile-controls.py) add only this selector's closed key to each original shared map. Removing that key must leave the entire shared AST and all effective old maps equal to the baseline above, including all Model, Agent, Runner, Variables core and App profiles, the complete Runner descriptor helper and its two selector branches. The original one-positive/seven-rejection metadata controls remain. The supervisor remains the sole owner of seven fixture resources, the Go 6-minute deadline, 540+60-second outer budgets, actual Wait and cleanup, descendant/private-runtime observations and the automatic 75-second host-TCP baseline-delta gate. No filtering, extra supervisor, hidden retry or budget refresh is added.

Source checkpoints, input maps and actual process/tool returns remain separate evidence. The fixed toolchain, protected module/cache and MinIO paths are external inputs, not Git-recoverable source bytes. These methods do not move evidence or mutate Git. Successful checks would establish this Work-owned fact port only: no Agent/Project deletion, reference retirement, new App/HTTP wiring, lifecycle participant/Initializer, scheduling stop, Model execution or other existing STOP capability follows.
