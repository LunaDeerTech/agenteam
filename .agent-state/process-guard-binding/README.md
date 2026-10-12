# Shared ProcessGuard binding validation

This task uses the Object change shared by Model and Agent. Its single bounded basic run passed as recorded below.
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

## Actual basic01

At Model checkpoint `ea42346edf51af6846000a3e8eac796906a3b3f1`, original tool
`95684` completed with exit 0 (`cf527b`). The outer PID 1090384 personally waited
for method PID 1090409, which returned 0 in 39.059 seconds. Race PID 1090542
returned 0 in 24.165 seconds: exactly two top tests and nine subtests each ran
and passed once, with no fail or skip. Object vet PID 1090916 returned 0 in
9.608 seconds. Both actual dependency-list processes also returned 0.

All six actual closure maps contain the same 250 inputs; the thin outer's six
inputs also match before and after. Agent checkpoint
`d4f473031c4abedf9f8600325f20902774d2765a` and Model both contain shared commit
`8734a19a8a40bcc8836d99632f31060ea0c079e8`. All 248 tested repository input files
match Agent byte for byte. The only separately identified entries are this
Model-only execution method and the shared fixed Go executable. Agent did not
run a second copy of the checks.

Original records are under `output/ai/process-guard-binding/basic01` and
`basic01-outer`, including `post-check.json` and `tool-observation.json`; the
latter is explicitly an assistant observation of the original tool completion,
not a provider-exported artifact. The result covers controlled original Store
and guard state checks only; it does not establish real PostgreSQL binding,
flock ownership, or foreign process death. No PG/native resources were started.
