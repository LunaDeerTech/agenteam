# Shared ProcessGuard binding validation

This task uses the Object change shared by Model and Agent. It has not been run.
Run the method once from the Model worktree under the root-owned Go/cache window:

```sh
python3 -B .agent-state/process-guard-binding/run-basic-checks.py --output output/ai/process-guard-binding/basic01
```

The exact race selector includes `TestProcessGuardBindingRequiresOriginalLiveStore`
and `TestProcessGuardBindingWaitsAndRechecksState`: two new top tests and nine
subtests, followed by vet for `./internal/central/object`. These controlled Store
checks do not prove a real flock, PostgreSQL transaction, or foreign process death.

The method preserves the accepted basic algorithm: offline fixed Go, actual
race/plain dependency closure, source maps before and after each stage, original
Popen/Wait, exact JSON RUN/PASS selection, and immediate stop on failure or drift.
A thin outer recorder retains the actual method Wait and original tool observation.
No PG, native compilation, fixture start, old matrix, or retry is included.

Agent reuse requires the shared commit and actual tested repository inputs to
match; a matching filename alone is insufficient. Original failure evidence, if
any, remains unchanged. Root separately authorizes execution after source freeze.
