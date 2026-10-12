# Variables project call ownership App checks

This profile validates the default App wiring described in [the task card](../../docs/development/work-items/d08-variable-project-call-app.md). The immutable method baseline is `9293ace4604b11529293ac6b26cb4c266ea65b2f`, the delivered Variables core main. Core results and earlier Agent App results are dependencies, not results for this profile. No method controls, Go check, compile, prepare or native run for this profile has been performed.

## Basic scope

The basic scope is exactly **six top-level tests / seven subtests**, with race and vet for `./internal/central/app` only:

- Four new tops / seven subtests in [the dedicated App controls](../../internal/central/app/variable_project_call_ownership_test.go): `TestVariableProjectCallAppConstructionAndLegacyViews` (five), `TestVariableProjectCallAppBothLanesHoldOriginalReturn` (zero), `TestVariableProjectCallAppUnknownCheckpointKeepsOriginalOwner` (two), and `TestVariableProjectCallAppLateInstallUsesOriginalForce` (zero).
- `TestProjectVariablesPureConstructionAndActualReadLookupJoin` from [the original ordinary controls](../../internal/central/app/project_variables_test.go) and `TestProjectSecretVariablesPureConstructionAndActualJoin` from [the original Secret controls](../../internal/central/app/project_secret_variables_test.go), each with no subtests. These exercise both independent legacy assemblies through actual controlled Store returns, Force and Join; the new controls' legacy construction checks do not replace those branches.

No migration-loader, core ownership, old HTTP or full App matrix is added. The exact JSON selection checks require every selected top/subtest to run and pass once; the source counts are not execution results.

The original basic algorithm retains actual race/plain compiler dependency discovery, complete input-map comparison, exact selected top/subtest results, and each original Popen/Wait. Controlled Store-return/Unknown cases do not prove real database COMMIT loss. The concrete unbound guard is used only to reject registration; it does not fabricate a successful guard or process-death proof.

## Native scope

The fixed PG selector is `^TestVariableProjectCallApp$`, one top with two subtests:

- `default-http-shared-variable-owner`
- `secret-checkpoint-drains-before-guard`

The [new PG source](../../tests/projectvariable/variable_project_call_app_test.go) reuses the [original Agent App handoff](../../tests/projectvariable/agent_project_call_app_test.go), public `app.Run`, validated TLS, Session/CSRF and Owner HTTP. All setup owners and their guard must actually return before App starts. Ordinary and Secret Create/Get/Lookup make six distinct calls, with two mutation and four read rows closed under one process/instance; original command/D04 receipt metadata and safe read projections remain meaningful. A rejected owner creates no registration. This is six HTTP entries, not a second twelve-entry core matrix.

The tail case uses a real Secret Get. An independent Project SH holder permits the original read but blocks the original retirement EX statement. Exactly one `pg_cancel_backend` is guarded by the original PID, transaction start, application name, lock tuple and blocker. Actual rollback within the original HTTP budget leaves the known HTTP 200 result and the same running CallID. SIGTERM then makes the original App Drain wait on that same CallID's new EX checkpoint while the real guard flock remains held. Releasing the original blocker within the original graceful budget permits retirement, actual `app.Run` return and guard/process-claim release. This is not COMMIT Unknown, foreign death, Force PG, a StopDriver or complete participant result. It does not establish D04 global nonce physical termination or reference retirement.

A failure records its safe stage immediately and retains the fixture while the original App/guard/HTTP/SQL remains unjoined. It waits only for actual release or the original whole-run hard deadline; it neither resets a budget nor enters the next subtest. This process-lifetime retention is not successful local bounded Join. Normal PASS requires the original HTTP/SQL/App returns, original CallID `business_closed`, actual guard release and all outer resource tails.

## Methods

Only root schedules the actual Go and native windows. The following are recipes, not execution evidence; allocate fresh output/namespace paths and keep every original FAIL/Wait record.

```sh
python3 -B .agent-state/variable-project-call-app/profile-controls.py
python3 -B .agent-state/variable-project-call-app/run-basic-checks.py --output output/ai/variable-project-call-app/basic01
python3 -B .agent-state/variable-project-call-app/run-pg-checks.py compile --output output/ai/variable-project-call-app/compile01
python3 -B .agent-state/variable-project-call-app/run-pg-checks.py prepare --compile output/ai/variable-project-call-app/compile01 --output output/ai/variable-project-call-app/native01 --namespace /tmp/vpca01
```

The [PG wrapper](run-pg-checks.py) preserves the accepted single-PG-binary algorithm: offline readonly race compilation with `-p=1`, every actual same-package Go source and all migration embeds through existing migration 75, original Wait, exact selector listing and a complete compile/runtime map. Runtime inputs equal the compile inputs plus that single original binary. No new child environment, Runner descriptor, second binary or build inside native is introduced.

`prepare` emits a request only. Native uses `python3 -B output/ai/variable-project-call-app/native01/record-native.py` without extra arguments in a separately authorized window. The [recorder](record-native.py) is byte-identical to the accepted original (`56a32b0d45a633c3c85306d098730318327c02f4ee9c5267c8d712e9880d4f20`). Preparation binds its actual invocation, plan, input map and recorder hashes; it is not a native PASS.

The [profile controls](profile-controls.py) add one closed selector to each original shared map. Removing only that new key must leave the entire shared AST and all effective old maps equal to the exact baseline above, including all Model/Agent/App/Runner/Variables keys and the Runner descriptor helper and both selector branches. The one-positive/seven-rejection metadata controls are unchanged. The original supervisor remains the sole owner of seven fixture resources, Go 6-minute deadline, 540+60-second outer budgets, actual Wait and cleanup, descendants/private-runtime observations and the automatic 75-second host-TCP baseline-delta gate. No filtering, second supervisor, extra cleanup or budget refresh is added.

Original source checkpoints, compile/list/runtime maps and tool/process returns remain separate evidence. Toolchain, protected module/cache and MinIO paths remain external inputs; Git does not contain their bytes. This method does not move/delete evidence or perform Git mutations. App coupling does not add Lifecycle Registry/Initializer, ObserveStop HTTP, reference cleanup, Model execution or any other existing STOP capability.
