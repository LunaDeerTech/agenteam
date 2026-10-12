# Variables project call ownership checks

These methods cover the explicit ordinary/Secret project business call owner in [the task card](../../docs/development/work-items/d08-variable-project-call-ownership.md). No method controls, Go checks, compile, or native run for this profile have been performed. Existing Agent results do not establish this profile's behavior.

The method baseline is `33feecbd6e50ec94fb32c258f980f3332e5c70ee`. The worktree was created from `a117e958c901a6146168c7837b3da0cc20377c2b`; its forthcoming normal main merge must preserve all existing profiles and any complete Runner descriptor helper/branches. The new profile is the only shared metadata addition. `profile-controls.py` must establish whole shared AST equality after removing only that new key against the final pinned baseline. The current pre-merge sources have not passed that baseline check.

## Scope

The basic scope is exactly **12 top-level tests / 38 subtests**, across `./internal/central/projectvariable` and `./internal/central/postgres`, with race enabled and vet for those same two packages:

- Ten new Variables ownership tops / 30 subtests from [the dedicated controls](../../internal/central/projectvariable/project_call_ownership_test.go).
- `TestSecretOwnerConfirmationUsesOriginalIntentAndPreservesUnknown` / eight subtests from [the existing Secret confirmation controls](../../internal/central/projectvariable/secret_confirmation_test.go). This preserves the original intent/receipt and Unknown classification checks alongside the new same-parent return controls.
- `TestEmbeddedSourceLoadsAllMigrations` / no subtests from [the real embedded source control](../../internal/central/postgres/embedded_source_test.go). It loads the complete actual migration embed, including 75, and proves loader acceptance only; SQL application is a separate native check.

The original basic algorithm retains actual race/plain compiler dependency discovery, complete input-map comparison, exact selected top/subtest results, and each original Popen/Wait. The scope adds no old Stop matrix. Store and process-proof ports in the new pure controls are controlled; they do not prove real PostgreSQL rollback, foreign process death, or D04 nonce completion.

The PG selector is `^TestVariableProjectCallOwnership$`, with exactly three parent subtests:

- `committed-lanes-registration-and-exact-row-fence`
- `lifecycle-selection-and-original-confirmation`
- `foreign-death-before-original-project-sql-terminal`

The three new PG sources use the same test binary. `AGENTEAM_VARIABLE_CALL_OWNERSHIP_CHILD` selects a private child branch of that same top-level test, with its own actual process Wait; it is neither an additional parent selector nor another compiled artifact. The fixture scope below describes the frozen source and remains unexecuted; source review does not establish PG PASS.

The first chain uses the original Account/Project/Skill/Secret graph, a real bound ProcessGuard and one same-Store owned pair. It covers nonempty ordinary and Secret six-entry lanes, physical registration COMMIT with controlled acknowledgment-loss masking and zero subsequent business/nonce, original tuple fencing, original confirmation/return/checkpoint ownership, Archive mutation/Delete read and other-project isolation. Legal STOPPING stage and exact-row terminal states are explicit SQL fixtures, not successful StopDriver or participant reports. Each lane also uses an original COMMIT proxy confirmation under the same parent. The foreign chain is an ordinary completed replay under the original Project **EX** lock. Actual child SIGKILL and original Wait, actual guard death proof, the observer EX waiter, original upstream COMMIT/Ready and proxy HeldJoined remain distinct facts.

A successful report covers registered **project business** calls at that observation point. Project EX does not prove that D04 global nonce SQL has physically completed. Local calls still own the original D04 provider/Prepare/Destroy and confirmation returns. This profile does not bind a complete participant, App defaults, reference retirement, or all legacy/global Secret providers.

## Commands and evidence

Only root may schedule each Go/native window. Commands here are recipes, not execution evidence. Use fresh output and namespace paths; preserve every failed attempt and its original Wait records.

```sh
python3 -B .agent-state/variable-project-call-ownership/profile-controls.py
python3 -B .agent-state/variable-project-call-ownership/run-basic-checks.py --output output/ai/variable-project-call-ownership/basic01
python3 -B .agent-state/variable-project-call-ownership/run-pg-checks.py compile --output output/ai/variable-project-call-ownership/compile01
python3 -B .agent-state/variable-project-call-ownership/run-pg-checks.py prepare --compile output/ai/variable-project-call-ownership/compile01 --output output/ai/variable-project-call-ownership/native01 --namespace /tmp/vpco01
```

`prepare` emits a request only. Native execution uses the generated `native01/record-native.py` after separate root authorization; invoke `python3 -B output/ai/variable-project-call-ownership/native01/record-native.py` without extra arguments. Its [original recorder](record-native.py) is copied unchanged. Do not infer native authorization or PASS from a successful prepare.

The [PG wrapper](run-pg-checks.py) keeps the original single-binary compile path and actual compiler/runtime input maps. Every same-package PG source, every embedded migration (including actual [migration 75](../../db/migrations/00075_project_variable_call_ownership.sql)), and the original driver closure are included. Compilation runs the original offline, readonly, single-worker race build, then exact test listing. Runtime inputs must equal the compile map plus the resulting binary; a stale source, incomplete compile, changed binary or missing list stops preparation.

The [profile controls](profile-controls.py) retain the original one positive and seven rejection cases. The original shared supervisor alone owns the seven fixture resources, Go 6-minute test deadline, 540+60-second outer budgets, original actual Wait/cleanup, descendants/private-runtime observations and automatic 75-second host-TCP baseline-delta gate. No filtering, second supervisor, budget refresh, or extra resource cleanup is introduced. A fixture failure records a safe error before retaining the graph while an original owner remains unjoined; it waits for actual return or the original Go/outer hard deadline. This is not successful bounded local Join. Normal PASS still requires every original owner and all resource tails to complete. The original recorder bytes and four-file identity binding algorithm remain unchanged; each new preparation records its own actual hashes.

The output records the immutable source checkpoint for each actual invocation, complete compile/list/runtime maps and the original process/Wait results. Future preservation can use those existing checkpoint mappings directly; no command in this method relocates or deletes source/evidence. The original Go toolchain, protected module cache and MinIO remain external inputs at their recorded paths.
