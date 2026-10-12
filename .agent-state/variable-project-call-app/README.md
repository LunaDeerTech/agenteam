# Variables project call ownership App checks

This profile validates the default App wiring described in [the task card](../../docs/development/work-items/d08-variable-project-call-app.md). The immutable method baseline is `9293ace4604b11529293ac6b26cb4c266ea65b2f`, the delivered Variables core main. Core results and earlier Agent App results are dependencies, not results for this profile. Basic01, controls, compile/list/prepare and the first native01 have now completed with their original actual Wait 0 results at source `156da848e9f9b91bba1ad2fc88a9085d0f7d2da2`. Final documentation and main delivery are separate steps; no move or cleanup is claimed here.

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

## Actual validation

These are original run results. The `tool-observation.json` records are assistant-observed transcriptions of the original tool returns, not provider-native exports. Input equality refers to each original frozen run; subsequent final-document edits do not claim the current README still has its historical outer-map hash. No App attempt failed or was retried; the upstream core basic01 failure remains recorded in its own unchanged results.

| Original run | Actual Wait and selection | Input and tail evidence |
| --- | --- | --- |
| [basic01](../../output/ai/variable-project-call-app/basic01-outer/tool-observation.json) | Tool `51459/1c50c2` actual 0; outer PID 1273358, method PID 1273383 Wait 0 / 287.275 s; race PID 1273494 Wait 0 / 156.829 s; vet PID 1275599 Wait 0 / 128.105 s. Exactly six tops / seven subtests and one App package vet passed, with no fail or skip. | [Post-check](../../output/ai/variable-project-call-app/basic01-outer/post-check.json): six 1138-input maps equal, 21 outer inputs unchanged and current source equal at that observation point. |
| [sequence01](../../output/ai/variable-project-call-app/sequence01/tool-observation.json) | Tool `37348/0da0ce` actual 0; outer PID 1278026 / 46.737 s. Controls PID 1278073 Wait 0 / 0.421 s: one positive / seven rejections. Compile wrapper PID 1278120 Wait 0 / 45.030 s; actual Go compiler PID 1278122 Wait 0 / 41.514 s; exact-list PID 1278696 Wait 0 / 1.086 s; prepare PID 1278765 Wait 0 / 0.997 s. | [Post-check](../../output/ai/variable-project-call-app/sequence01/post-check.json): three equal 2069-input compile maps, 2070 runtime inputs including the original single binary, 43 outer inputs unchanged, all migration embeds and 101 same-package Go sources included. Preparation remained `prepared_ready_not_run`. |
| [native01](../../output/ai/variable-project-call-app/native01/tool-observation.json) | Tool `53840/56d7d0` actual 0; supervisor PID 1279902 Wait 0 / 112.131 s; driver PID 1279924 and Go PID 1281535 Wait 0. One top / two subtests passed in 21.56 s (10.62 / 10.93 s). | [Post-check](../../output/ai/variable-project-call-app/native01/post-check.json): all 2070 actual input hashes and [four recorder identities](../../output/ai/variable-project-call-app/native01/recorder-identity.json) unchanged, original tested HEAD clean, seven resource IDs absent in both observations (14 checks), descendants/private/runtime empty twice, both automatic TCP deltas empty, and no race warning. |

The original [binary record](../../output/ai/variable-project-call-app/compile01/binary.json) binds `compile01/projectvariable.test`, 92,656,152 bytes, SHA-256 `14320ece7545a1d7edccf8135e59d97419abc2056a51eb3f55b12b5f2579ccc5`. Original native raw output is `/tmp/vpca01/pg-cfdc7919d62642d0af96113381f803d2.log`; its same-nonce directory retains the owned/request records. The recorder's supervisor elapsed time above and the supervisor log's 111.114 s measure distinct layers, not separate native attempts.

Actual native markers establish public `app.Run`, three ordinary and three Secret HTTP calls, six distinct closed calls with the same process/instance, a real D04 receipt and zero denied-owner registration. The tail marker binds the original Secret HTTP return, a genuinely NotCommitted checkpoint after precise statement cancellation, the same CallID later closed by shutdown Drain, and the guard held until that checkpoint completed. The PG graph uses real bound guard/Store; controlled Unknown and unbound-guard rejection remain pure-test evidence only. Setup uses Project service and a persistent test Skills receipt, not Project-creation HTTP or proof of a production Skills implementation.

This passes the stated default double-lane App integration and graceful Secret-checkpoint chain. It does not establish all twelve core entries again, physical COMMIT loss, foreign death, Force PG, a full Lifecycle participant/Initializer, reference retirement or global D04 technical SQL Join. The root's original Force policy may still terminate DB sockets through Store.ForceClose; an unjoined pair is not erased by that event and the existing joint guard gate remains. No promise of permanently available Store after Force follows from this result.
