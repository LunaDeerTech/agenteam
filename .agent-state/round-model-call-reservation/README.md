# Round Model Call reservation validation

Status: bounded basic checks passed at `6c843253ef8366a4080f3f7d1a2da8420db66fba`; the first real PG round, `native01`, passed as a whole at technical HEAD `4f76ef40eb0b8129940a21e2e36c55d0e03eaa83`. This verifies reservation and original commit-receipt recovery, not a successor Model invocation. Integration into main and branch/worktree cleanup are separate delivery steps. The [work item](../../docs/development/work-items/d22-round-model-call-reservation.md) defines the reservation-only scope.

The completed basic invocation ran once from this worktree in the root-authorized exclusive Go/cache window:

```sh
python3 -B .agent-state/round-model-call-reservation/run-basic-checks.py --output output/ai/round-model-call-reservation/basic01
```

The exact basic selection is 6 new top tests / 28 declared subtests in
`./internal/central/execution`, plus the existing `TestEmbeddedSourceLoadsAllMigrations`
in `./internal/central/postgres` (one top, no subtests). Both selected packages run
with race instrumentation, then vet for those two packages. Thus the completed
selection is 7 top tests / 28 subtests / two vet packages. Actual JSON RUN/PASS
records confirmed those counts, without failure or skip. No other old test matrix was selected.

The embedded-source check uses the actual `db/migrations/*.sql` closure and the
real loader, including the continuous migration sequence. It loads source only;
it does not apply SQL or establish deferred constraints on PostgreSQL.

The script preserves the already accepted basic algorithm: fixed offline Go
1.27.1, `-p=1`, actual race/plain dependency discovery, input maps before and after
each stage, original Popen/Wait, exact JSON selection, and immediate stop on
nonzero exit or input drift. A thin outer recorder retained the method's real
Wait and original tool-completion observation. No automatic retry is authorized.

Inputs are determined by the actual package closure, including same-package
tests, dependency sources and migration embeds. The required-source set binds
the new product/control files, the shared ProcessGuard entry and actual loader.
PG or assembly files still being edited may remain outside that closure only if
the discovered input map proves it; if any are included, freeze them before the
run. Do not infer independence merely from a file's directory or task owner.

Original basic tool session `56248`, completion `3eeb89`, returned 0. Outer PID
1103519 recorded method PID 1103545 actually returning 0 in 141.022 s. Race Go
PID 1103654 returned 0 in 84.153 s; vet PID 1104831 returned 0 in 54.614 s. Both
dependency discoveries returned 0. All six maps of 707 actual compiler inputs
matched, as did the 15 outer inputs; independent post-check `282000` returned 0.
Original records remain under `output/ai/round-model-call-reservation/basic01/`
and `basic01-outer/`. `tool-observation.json` is an assistant-observed transcript
of the original tool completion, not a provider-owned output file.

These package-local Store and process-proof controls do not establish a real PostgreSQL reservation, physical COMMIT recovery, complete 68/72 historical reconstruction, or any Model invocation.

The shared Object-only basic evidence is recorded separately under
`.agent-state/process-guard-binding/README.md`; it is not re-run or promoted to a
real PG result here. The PG compilation/list/prepare/native work below used
its own frozen profile and separate root authorization. These methods do not
perform process-death experiments or change runtime defaults.

## PG compilation and preparation

Original `sequence01` tool session `98671`, completion `abd9ec`, returned 0.
Outer PID 1111637 finished in 87.965 s; all 31 recorded method/source inputs
remained unchanged. Offline profile controls passed one positive and seven
negative cases. Compile wrapper PID 1111752 actually returned 0 in 86.128 s;
its Go PID 1111754 returned 0 in 82.785 s. List PID 1112976 returned 0 in
1.050 s with the sole top `TestRoundModelCallReservation`. Preparation PID
1113045 returned 0 in 1.039 s and started no native resources.

The three compile/list maps of 2042 inputs matched. Runtime added only the
binary, giving 2043 inputs. All 92 same-package Go files and the complete
migration sequence through 73 were bound. The binary is 91,414,828 bytes,
SHA-256 `9937265d4b193f21648b25a0d9a30875bbe3561596c30baf730f814ba36d7bff`.
Fresh preparation used `/tmp/rmcr01`; its 3,989,389,312 available bytes and
1,702,140 available inodes were a capacity snapshot, not runtime peak evidence.
The pre-start capacity fluctuation was reviewed before authorizing this single
native attempt; it was not a failed native round or a retry.

## First actual native round

Original tool session `11528`, completion `728984`, returned 0. Supervisor PID
1115121 actually returned 0 in 141.678 s; driver PID 1115126 and Go PID 1116761
each recorded actual Wait 0. The exact selector ran **1 top / 2 subtests** in
38.47 s:

| Subtest | Actual duration |
| --- | --- |
| `committed-request-reservation-and-history` | 20.24 s |
| `reservation-rollback-and-commit-loss-lookup` | 18.23 s |

Real PostgreSQL applied the migration source including 73. The tests passed
against the original committed 68/72 history and same-Store ProcessGuard. The
rollback left zero reservation rows; the physical COMMIT/held-return barrier,
Stop/Drain and acknowledgement-loss case recovered only its original complete
candidate. Each case retained one immutable reservation, unchanged original
request/Execution/transcript and Invocation/Usage facts, and the setup's one
controlled Model call plus one real Command WSS request. Reservation added zero
Model or Runner calls.

All seven exact resources had 14 absence observations. Descendant, private and
runtime double-empty tails, automatic HOST_TCP double delta-empty observations,
and exact metadata RUN/PASS/Wait checks completed. The independent post-check
matched all 2043 runtime inputs and all four recorder identities; the tested
technical HEAD remained clean. This is whole-run PASS, including actual process
returns and resource tails.

Original records remain under `output/ai/round-model-call-reservation/` in
`basic01/`, `basic01-outer/`, `sequence01/`, `compile01/` and `native01/`.
The [sequence post-check](../../output/ai/round-model-call-reservation/sequence01/post-check.json),
[native terminal](../../output/ai/round-model-call-reservation/native01/terminal.json),
[tool observation](../../output/ai/round-model-call-reservation/native01/tool-observation.json)
and [native post-check](../../output/ai/round-model-call-reservation/native01/post-check.json)
retain the original results. The raw log is
`/tmp/rmcr01/pg-45c857cdfa174c6bb9a9882f3f54c058.log`.
Tool observations are assistant-received transcripts of the original tool
completion; they are not provider-exported files. These records and used
namespaces are not instructions to rerun or overwrite them.

## Frozen PG profile and completed commands

The exact selector `^TestRoundModelCallReservation$` requires one top and two
subtests: `committed-request-reservation-and-history` and
`reservation-rollback-and-commit-loss-lookup`. The new PG file reuses the actual
Command → formal dispatch revocation → 66 → 68 → 72 fixture, its original Store
and Runtime-bound ProcessGuard. Each setup uses the controlled Model JSONCaller
once and real Command WSS once; reservation adds no Model or Runner call.

The first case checks the complete immutable reservation, same-instance replay
and new-instance Lookup without Claim ownership. The second observes a complete
insert followed by actual rollback, then a physical COMMIT with original Store
return held and its acknowledgement masked. Stop and a short Drain cannot join
that original call; after actual return, only the original full-candidate Lookup
settles its retained Unknown. Independent SQL checks all reservation fields and
preserves original 72 bytes/digests, 56/66/68, transcript, Execution version and
Invocation/Usage facts. Cleanup waits for the original call and all owners before
the inherited fixture releases its actual guard. No foreign death or takeover
is claimed.

Only the new profile key is added to the shared driver and supervisor. The
offline controls compare both files to `6c843253ef8366a4080f3f7d1a2da8420db66fba`
after stripping that key, including existing effective maps, and exercise one
positive plus seven rejected metadata-result cases:

```sh
python3 -B .agent-state/round-model-call-reservation/profile-controls.py
```

The completed compile/list invocation ran once in the authorized exclusive
Go/cache window and started no services:

```sh
python3 -B .agent-state/round-model-call-reservation/run-pg-checks.py compile --output output/ai/round-model-call-reservation/compile01
```

After actual successful compile/list returns, this invocation prepared the
fresh native request:

```sh
python3 -B .agent-state/round-model-call-reservation/run-pg-checks.py prepare --compile output/ai/round-model-call-reservation/compile01 --output output/ai/round-model-call-reservation/native01 --namespace /tmp/rmcr01
```

Preparation runs no Go or service. It requires unchanged compile/list/runtime
input maps and exact binary identity. The profile has 21 explicit required paths;
the unchanged driver includes every same-package source, all embedded migrations
through actual migration 73, shared support, conservative internal sources and
runtime tools. The actual input counts are recorded above.
Capacity at preparation is a snapshot, not a guarantee of runtime peak space.

The separately authorized native window used the copied thin recorder:

```sh
python3 -B output/ai/round-model-call-reservation/native01/record-native.py
```

The recorder remains byte-identical to the accepted original at SHA-256
`56a32b0d45a633c3c85306d098730318327c02f4ee9c5267c8d712e9880d4f20`.
The original supervisor owns the seven exact resources, 540 + 60 second budgets,
6-minute Go timeout, actual driver/Go Wait, exact metadata selection, descendant
and private/runtime double-empty tails, and two automatic HOST_TCP delta-empty
observations within 75 seconds. No filtering, manual baseline, prewarm, retry or
alternate cleanup is added. Existing namespace or output records must not be
overwritten. A business pass alone does not establish whole-run success.

This work does not invoke a successor Model, read credentials, dispatch HTTP,
write a second response checkpoint, change the capture-only runtime/default
assembly, or lift the existing tools HTTP STOP.
