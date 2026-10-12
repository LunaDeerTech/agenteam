# Scheduler project stop validation

The [work item](../../docs/development/work-items/d08-scheduler-project-stop.md) defines this slice. These methods are statically prepared; no basic, controls, compilation or native result is claimed. The source baseline is `e4cf974eacd303c46e763cf9524db8ad61cec1b1`. A later checkpoint must bind the actual frozen sources and methods before execution.

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
