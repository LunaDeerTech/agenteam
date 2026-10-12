# Agent Project local stop

The Agent product and controlled tests are fixed at `d31e184c1846fc485161f138ccfccdb217255e4e`; the real PG fixture and optional assembly seam are fixed at `07f4061e3d7084941d1f7bb19a1640973fc4fb12`. These checkpoints and static method checks are not execution results. No Agent basic, PG compile or native run has occurred for this slice.

## Bounded basic scope

`run-basic-checks.py --scope full` selects exactly eight new tops / 29 declared subtests from `agent/project_lifecycle_test.go`, plus `TestAgentStopWaitsForOriginalCall` and `TestAgentUnknownConfirmationOwnedUntilActualReturn`. The two old tops have no subtests: the planned total is ten tops / 29 subtests, one race-test package and one vet package, both `./internal/central/agent`.

The script reuses the accepted Command result-consumption basic algorithm. Only the explanatory docstring and `NEW_TESTS`, `COMPAT_TESTS`, `VET_PACKAGES`, `REQUIRED_SOURCES` change. Actual race and plain `go list -deps -test -json` closures bind the compiled sources, same-package tests, module files, embedded files, recorder and fixed Go binary. Required inputs include the seven changed Agent product files, new test and the two selected old test files. Each stage retains its actual Popen/Wait and input hashes. The exact top selector, original race timeout, offline/max1 environment and fail-stop behavior remain unchanged. The final reviewer also checks the raw JSON for actual subtest run/pass counts and any skip/fail; static declarations do not prove execution.

## Real PG scope

The only real selector is `^TestAgentProjectLocalStop$` in `tests/projectvariable`, with `archive-original-mutation-and-project-isolation` and `delete-directory-and-original-return`. It uses the real LifecycleStopDriver, Agent provider and database. Actual committed-return barriers precede cancellation; held original calls remain pending until their real returns. Archive isolates target mutations and preserves reads/foreign Project work; Delete also covers both owners' target reads. The fixture does not declare a participant or the whole Project stopped merely because the local provider joined.

The two shared harness changes add only the new profile. The profile controls compare the remaining full AST and effective mappings to the fixed baseline and retain one positive / seven negative log controls. PG compilation binds the complete actual source set, all same-package Go files and embedded migrations through the current migration 72; no new migration is introduced. Successful compile and an exact sole-top list are prerequisites to preparation. Preparation creates a fresh recipe and capacity snapshot, not a running resource or a guaranteed successful native round.

The original seven exact resources, actual Go/driver/supervisor Wait, descendant/private/runtime double tails and automatic HOST_TCP double-empty within 75 seconds are preserved. The Go 6m and driver 540+60s budgets are unchanged. The thin native recorder remains byte-identical to the accepted recorder, SHA `56a32b0d45a633c3c85306d098730318327c02f4ee9c5267c8d712e9880d4f20`. No manual baseline, filtering, prewarm or automatic retry is added.

## Planned sequence

These commands await root's execution window; they are not PASS evidence. The outer recording sequence must retain the methods' inputs, each actual child Wait and original final tool receipt. Continue only after the preceding stage actually returns zero with unchanged inputs. Any failure stops the sequence and preserves its original records.

```sh
python3 -B .agent-state/agent-project-local-stop/profile-controls.py
python3 -B .agent-state/agent-project-local-stop/run-basic-checks.py --scope full --output output/ai/agent-project-local-stop/basic01
python3 -B .agent-state/agent-project-local-stop/run-pg-checks.py compile --output output/ai/agent-project-local-stop/compile01
python3 -B .agent-state/agent-project-local-stop/run-pg-checks.py prepare --compile output/ai/agent-project-local-stop/compile01 --output output/ai/agent-project-local-stop/native01 --namespace /tmp/apls01
```

Native execution requires a separate root authorization after the new recipe, binary and actual input identities have been reviewed. Preserve failed rounds; a successor needs its own fresh namespace. This task does not establish global lifecycle completion, default production binding or platform readiness. The [work item](../../docs/development/work-items/d08-agent-project-local-stop.md) defines the product boundaries.
