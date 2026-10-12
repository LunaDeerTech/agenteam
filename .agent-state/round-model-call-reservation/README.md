# Round Model Call reservation validation

Status: methods prepared and sources statically reviewed; this task's basic and PG checks have not run.

Under the root-authorized exclusive Go/cache window, run once from this worktree:

```sh
python3 -B .agent-state/round-model-call-reservation/run-basic-checks.py --output output/ai/round-model-call-reservation/basic01
```

The exact basic selection is 6 new top tests / 28 declared subtests in
`./internal/central/execution`, plus the existing `TestEmbeddedSourceLoadsAllMigrations`
in `./internal/central/postgres` (one top, no subtests). Both selected packages run
with race instrumentation, then vet for those two packages. Thus the planned
selection is 7 top tests / 28 subtests / two vet packages; actual JSON RUN/PASS
records must establish the final counts. No other old test matrix is selected.

The embedded-source check uses the actual `db/migrations/*.sql` closure and the
real loader, including the continuous migration sequence. It loads source only;
it does not apply SQL or establish deferred constraints on PostgreSQL.

The script preserves the already accepted basic algorithm: fixed offline Go
1.27.1, `-p=1`, actual race/plain dependency discovery, input maps before and after
each stage, original Popen/Wait, exact JSON selection, and immediate stop on
nonzero exit or input drift. A thin outer recorder will retain the method's real
Wait and original tool-completion observation. No automatic retry is authorized.

Inputs are determined by the actual package closure, including same-package
tests, dependency sources and migration embeds. The required-source set binds
the new product/control files, the shared ProcessGuard entry and actual loader.
PG or assembly files still being edited may remain outside that closure only if
the discovered input map proves it; if any are included, freeze them before the
run. Do not infer independence merely from a file's directory or task owner.

These package-local Store and process-proof controls do not establish a real PostgreSQL reservation, physical COMMIT recovery, complete 68/72 historical reconstruction, or any Model invocation. The separate PG profile remains to be prepared and authorized.

The shared Object-only basic evidence is recorded separately under
`.agent-state/process-guard-binding/README.md`; it is not re-run or promoted to a
real PG result here. Subsequent PG compilation/list/prepare/native work requires
its own frozen profile and separate root authorization. These methods do not
start services, perform process-death experiments, or change runtime defaults.
