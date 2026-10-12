# Agent Project call ownership validation

The bounded basic checks passed at `e752c0bda5d00f6f04260b811028932511f1c21a`. PG source and methods are prepared for separate review and authorization; **this slice has not compiled or run its PG profile**. The [work item](../../docs/development/work-items/d08-agent-project-call-ownership.md) defines the opt-in boundary.

## Original basic result

The original tool session `92042`, completion `8a4938`, returned 0. Method PID 1105863 actually returned 0 in 66.032 s; race-test PID 1105974 returned 0 in 39.299 s; vet PID 1106547 returned 0 in 25.104 s. Actual JSON records establish **9 top tests / 32 subtests and two vet packages**: eight new Agent tops with 32 subtests, plus the existing PostgreSQL `TestEmbeddedSourceLoadsAllMigrations` top with no subtests. Both selected packages used race instrumentation, then vet: `./internal/central/agent` and `./internal/central/postgres`.

All 378 actual compiler inputs and 20 outer method/source inputs matched before and after. The actual dependency maps excluded `tests/projectvariable/` and `tests/testsupport/agentconfiguration/` (zero intersection), allowing the separately owned PG sources to advance without changing the tested closure. Original evidence remains under `output/ai/agent-project-call-ownership/basic01/`; the original Wait and tool-completion record, rather than declaration counts, support this result.

The unchanged basic method retains offline Go 1.27.1, max1/race/count1/p1, actual race/plain dependency discovery, embedded inputs, exact JSON selection and actual Popen/Wait. The real loader accepted the continuous migration source including 73 and 74; it did not apply SQL or prove database constraints. Package-local Store and process-proof controls do not establish actual process death or original server transaction completion. The separately accepted shared Object controls are not promoted to a PG result.

## Pending PostgreSQL profile

The exact parent selector is `^TestAgentProjectCallOwnership$`, one top with two subtests:

| Subtest | Required real boundary |
| --- | --- |
| `local-original-return-and-terminal-fence` | Actual registration precedes the business entry; a live caller meets a deliberately terminal ledger row after acquiring its original lock and is rejected. Separately, physical checkpoint COMMIT cannot retire a still-held original return; Archive preserves target reads and foreign Project work. |
| `foreign-death-before-original-sql-terminal` | A real completed non-name, no-change Update is replayed under Project SH after Archive. Its fresh call is registered as service/mutation before the original business transaction. Actual Guard Busy, explicit child SIGKILL and the one original child Wait establish process death, while the real upstream COMMIT remains held. Exact holder/waiter backend identities and PostgreSQL locks show Project EX still blocked; original COMMIT/Ready and proxy HeldJoined precede retirement. |

The child uses a private environment branch of this same top, with its own closed stdout/pipe protocol and original `cmd.Wait`. It is not an additional helper top or a skipped test, and its pipe records are not counted as parent test results. Raw business Store Unknown, the held wrapper return, process death and actual upstream transaction completion remain distinct observations. Lifecycle acceptance and the separate live-caller terminal-row counterfixture are explicit prerequisites; neither fabricates foreign death. Real initialization, providers, guard, lifecycle authority and StopDriver remain in the graph.

Static review corrected the original foreign Create replay to a completed non-name Update because Create takes Project EX. The strict ShareLock observation was retained. Failure cleanup now gates both actual Guard and Object Runtime close callbacks on the original pair's real Joined state; a failed bounded drain retains the pair/guard for the rest of the native process lifetime. The parent stops after a failed local subtest. These are source-review corrections, not failed dynamic PG runs. Actual child/proxy/observer cleanup remains required, and a timeout does not mean Joined.

## Authorized execution method

The methods reuse this tree's delivered [local-stop method](../agent-project-local-stop/run-pg-checks.py). Shared harness changes add only this exact profile; removal of that key must leave both shared ASTs and existing effective maps equal to baseline `e752c0bda5d00f6f04260b811028932511f1c21a`. The profile control contains one positive and seven negative cases; **they have not yet run for this profile**. No resource or Wait algorithm was added.

After independent review and a root-authorized exclusive Go/cache window, the planned commands are:

```sh
python3 -B .agent-state/agent-project-call-ownership/profile-controls.py
python3 -B .agent-state/agent-project-call-ownership/run-pg-checks.py compile --output output/ai/agent-project-call-ownership/compile01
python3 -B .agent-state/agent-project-call-ownership/run-pg-checks.py prepare --compile output/ai/agent-project-call-ownership/compile01 --output output/ai/agent-project-call-ownership/native01 --namespace /tmp/apco01
```

The compile stage runs one offline race-c and an exact binary list, recording actual Wait and equal input maps. It binds all same-package Go sources, actual shared support, the original COMMIT proxy, conservative full-internal sources/assets and all embedded migrations through 74, explicitly requiring continuous migration 73. This inherited PG scope is broader than the basic stage's actual compiler closure. Runtime adds only the compiled binary to the compile input set. The 31 required profile paths select the new source and actual dependencies; they are not a substitute for the complete input map.

Preparation only consumes successful compile/list records and verifies original input and binary identities, then emits a fresh native request and a capacity snapshot. It does not start services or constitute a test result. Under a separate explicit native window, the emitted `native01/record-native.py` records the original supervisor's actual Wait. It remains byte-identical at SHA-256 `56a32b0d45a633c3c85306d098730318327c02f4ee9c5267c8d712e9880d4f20`.

The original seven-resource supervisor retains the 6-minute test timeout, 540+60-second driver budgets, exact resource observations, descendant/private/runtime tails and automatic HOST_TCP double delta-empty within 75 seconds. No filtering, manual TCP baseline, new cleanup authority or hidden retry is introduced. A nonzero result, input drift or incomplete tail stops the chain and preserves the original result; compile/list/prepare/native directories are never silently reused or overwritten. Existing cache/tool paths remain the authorized infrastructure; no retired feature worktree source, binary or output is an input.

## Product boundary

The explicit owned-pair factory supplies durable call/process ownership for these two opted-in instances. Default constructors and production app wiring retain their prior behavior. `RegisteredJoined` concerns the registered calls at the observation point; `LocalJoined` still concerns the two local instances. Neither completes a full Agent lifecycle participant or proves all system processes joined. This slice does not retire references, send remote cancellation, enable default Project creation, bind a partial LifecycleRegistry, or alter the Model/runtime STOP boundary.
