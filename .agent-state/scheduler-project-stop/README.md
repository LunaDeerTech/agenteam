# Scheduler project stop validation

The [work item](../../docs/development/work-items/d08-scheduler-project-stop.md) defines this slice. The selected basic, controls, compilation/list, preparation and native checks passed at `898ed8ebd91e0a272ad3eec07ca8d4721f133969`, following the preserved first basic build failure below. The source baseline is `e4cf974eacd303c46e763cf9524db8ad61cec1b1`. Acceptance covers Delete-only stopping within the original manager and its original call returns.

`run-basic-checks.py` uses the delivered input-discovery, race, vet, original process Wait and exact JSON-selection algorithm. It selects the new Project/Scheduler controls and five directly affected compatibility tests: the three original Scheduler manager lifecycle tests, plus the original Agent nested-loan and scope-close tests in Project. Actual counts are checked from the selected RUN/PASS JSON records; a nonzero child exit or changed input map stops the run. The frozen scope is 10 new top-level tests / 27 subtests plus 5 compatibility top-level tests / 7 subtests: 15 top-level tests and 34 subtests total, with Project and Scheduler race tests and one vet invocation covering those same two packages. The script contains the exact per-package selectors.

The PG selector is `^TestSchedulerProjectStop$`, with exactly these two subtests:

- `committed-stop-isolates-project-and-discards-stale-page`
- `driver-scope-and-authorization-unknown`

The fixture reuses the original directory, A/B runners, initialized Model/wire resources and ProcessGuard. Only accepted Delete is controlled setup; the real StopDriver produces stopping and the running claim. The Scheduler control transaction must execute its complete authorization callback and actually return Committed while the private step scope remains active before stopping A. A keeps its original call until return, B continues, and a stale directory page must be discarded before mutating entries or cursors. The held Store-return barriers follow physical COMMIT, so they do not claim that SQL locks remain held. Authorization Unknown is a controlled ackmask; the business-Unknown ordering checks remain pure tests. This slice sends no Model request and does not establish full participant completion, foreign termination, Agent retirement or App wiring.

The methods reuse the delivered single-binary PG algorithm. Only this task's profile key is added to the shared driver and supervisor. Removing that key must recover the complete baseline AST and effective old maps, including the Runner helper and both Runner branches. The thin recorder remains byte-identical (`56a32b0d45a633c3c85306d098730318327c02f4ee9c5267c8d712e9880d4f20`). The original seven resources, six-minute Go budget, 540+60-second supervisor budget, all actual-return/resource exit gates and automatic 75-second double-empty TCP delta remain unchanged.

Run from this worktree, only within the assigned execution window. Every output directory must be new; these examples are not execution authorization.

```sh
python3 -B .agent-state/scheduler-project-stop/run-basic-checks.py \
  --output output/ai/scheduler-project-stop/basic01
python3 -B .agent-state/scheduler-project-stop/profile-controls.py
python3 -B .agent-state/scheduler-project-stop/run-pg-checks.py compile \
  --output output/ai/scheduler-project-stop/compile01
python3 -B .agent-state/scheduler-project-stop/run-pg-checks.py prepare \
  --compile output/ai/scheduler-project-stop/compile01 \
  --output output/ai/scheduler-project-stop/native01 \
  --namespace /tmp/sps01
```

The offline profile controls require one positive case and rejection of seven negative cases. Compilation records the complete conservative driver input scope, all same-package Go fixtures and all embedded migrations through 75, then compiles one race-enabled integration binary and lists only the exact top-level test. All three compilation maps must agree. The runtime map adds only that actual binary and retains its measured bytes and digest.

Preparation checks those existing records and current inputs, creates the four recipe identities, and records available bytes/inodes. It neither starts native nor proves the runtime peak. A native run needs the assigned window and its actual capacity gate; the current root gate is at least 4,100,000,000 available bytes. Capacity HOLD and actual test FAIL are distinct outcomes. Preserve the original records and stop either way; do not change budgets, filter TCP, clean cache or retry automatically.

Only after the prepared recipe has been accepted for execution:

```sh
python3 -B output/ai/scheduler-project-stop/native01/record-native.py
```

The final result requires the original outer/supervisor, driver, Go and owned-child actual Wait results, seven resource identities with both absence observations, descendant/private/runtime tails, both TCP-empty observations, unchanged runtime inputs and all four recipe identities. Raw business PASS alone is not whole PASS. Tool observations are explicitly assistant-observed transcriptions, not provider-native exports.

The [PG wrapper](run-pg-checks.py) and [profile controls](profile-controls.py) bind current-tree inputs. No retired worktree source, output or binary path is used. The fixed Go, module cache, build cache and MinIO paths retain their existing protected roles; no dependency download or cache cleanup is part of these methods.

## Actual results

- [basic01 original FAIL](../../output/ai/retired-20261012/scheduler-project-stop/basic01-outer/post-check.json), tool `86231/f2eb4b`, actual exit 1: method PID 1501019 returned 1 after 70.807s. Project's 7 top-level tests / 30 subtests passed, but Scheduler did not execute because its new test called `NewRecoveryCause` without the required third argument ([original build output](../../output/ai/retired-20261012/scheduler-project-stop/basic01-outer/failure-build-events.json)). Vet and all later stages did not start. The 573-entry closure and 14 outer inputs remained unchanged. Commit `898ed8ebd91e0a272ad3eec07ca8d4721f133969` only added the empty `checkpointRef` argument in that test; no product or method change is attributed to this failure.
- [basic02 PASS](../../output/ai/retired-20261012/scheduler-project-stop/basic02-outer/post-check.json), tool `59860/2c2a82`, actual exit 0: method PID 1503909 returned 0 after 77.549s; race PID 1504041 returned 0 after 12.830s and vet PID 1504224 after 61.185s. All 15 top-level tests / 34 subtests ran and passed exactly once across the two race packages; both packages passed vet. The six 573-entry maps and 14 outer inputs matched. The original basic01 failure remains intact.
- [sequence01 PASS](../../output/ai/retired-20261012/scheduler-project-stop/sequence01/post-check.json), tool `45610/a0ee44`, actual exit 0 after 155.956s: [profile controls](../../output/ai/retired-20261012/scheduler-project-stop/sequence01/controls01.log) passed 1 positive / 7 negative cases; original compile PID 1505621, sole-list PID 1507558 and prepare PID 1507627 returned 0 after 150.302s, 1.057s and 1.199s respectively. The 2,085 compile inputs matched across three maps; the runtime map added the one actual binary for 2,086 inputs, with 32 outer inputs unchanged. The [binary record](../../output/ai/retired-20261012/scheduler-project-stop/compile01/binary.json) records 93,110,074 bytes and SHA-256 `af628f61b89ca58381ad6ca29f498223570462a759047478ebed3330096bcf96`.
- [native01 whole PASS](../../output/ai/retired-20261012/scheduler-project-stop/native01/post-check.json), tool `5554/d9b30e`, actual exit 0: supervisor PID 1508414 returned 0 after 151.351s; original driver PID 1508435 and Go PID 1510238 also returned 0. The selected 1 top-level test / 2 subtests passed in 36.49s (17.74s / 18.75s). All 2,086 runtime inputs, four recipe identities and tested HEAD matched; seven resources had 14 absence observations, descendant/private/runtime tails were empty twice, and both TCP delta observations were empty. No race warning occurred. [Original tool observation](../../output/ai/retired-20261012/scheduler-project-stop/native01/tool-observation.json) retains the observed Wait provenance; raw log: `/tmp/sps01/pg-336e6c6bad924ae6ab713ed5a322e86a.log`.

The native markers establish A/B isolation, stale-page rejection and original returns through the real StopDriver/private scope. Accepted Delete remains controlled setup; authorization Unknown remains a controlled acknowledgement mask. Store-return barriers follow physical COMMIT and do not prove held SQL locks. Business Unknown ordering remains a pure-test result. This is neither foreign/global termination nor Task-assignee cleanup, Agent retirement, full Registry/participant completion or App integration; no Model/Launch/Advance call was made.

## Main delivery and retirement

Tested `898ed8ebd91e0a272ad3eec07ca8d4721f133969` and final docs `ecda1596d4061f097c50c9c95bef98e6fadd0789` were normally merged and pushed as main `1b1a1cb927465e572ff312d62b568a7c290b1415`; [delivery](../../output/ai/scheduler-project-stop-delivery/main01/result.json) confirms the technical tree matches the tested version. The [preserved evidence](../../output/ai/retired-20261012/scheduler-project-stop-preservation/result.json) contains all 122 files / 101,136,754 bytes / 13 directories with unchanged SHA/stat/inode, including basic01 FAIL, the test fix, single binary and original PASS records. It moved without replacement on the same filesystem; original tool `8921/c0f341` Wait 0 / 4.778s and independent check `0d2b21` accepted it. The 31 input maps identify 2,088 Git source versions recoverable from main history; four tmp copies total 7,250 bytes and the originals remain. Two external tools retain their original identity records and are not claimed as Git-restorable dependencies.

[Ordinary cleanup](../../output/ai/scheduler-project-stop-delivery/cleanup01/result.json), tool `65933/61eaed` actual exit 0, removed only the old worktree and local branch. The feature was never pushed; fresh remote checks were empty before and after, so no remote deletion is counted. Main plus the task-prefix fetch/prune left no residual reference. The commands above describe the original execution paths; restoration uses the preserved evidence and recorded source versions, without rewriting old records or rerunning in their output directories. This filesystem retirement adds no product retirement claim.
