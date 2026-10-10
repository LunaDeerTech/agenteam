# Secret HTTP independent risk probe

Status: implementation preparation only. No independent PG execution or acceptance
has occurred. Author HTTP PG/native/root results and their original failures remain
separate. The probe author did not implement Secret HTTP or its default root.
The six offline entry controls passed (tool `1f069c`, exit 0): this includes the
actual shared main's PG observer and both input passes with explicit OS doubles.
After checkpoint `7429c8e1`, the unchanged probe passed offline race compilation
and exact-list in session `87900` (final `2d140c`, actual exit 0). Original outer
332465, Go 332466 and list 339257 all exited; the list contained only the declared
top. Fresh space was 8,790,708,224 B at 2026-10-10T06:30:23.214238Z, elapsed 65.597s,
binary 37,029,513 B, private build runtime empty. The build used a new independent
cache and private telemetry-off environment. Original logs and result are in
`output/ai/secret-owner-http/independent-build-01/`. No source repair was needed.
Both SQL subscenarios remain NOT RUN; method review and a fresh real grant are
still required.
After that build, self-review tightened the same probe's Problem assertion to the
formal fixed title/detail/type for both tested codes. This excludes metadata in
otherwise legal diagnostic fields. No scenarios or product behavior changed;
the first binary does not yet verify this narrow assertion update.
The original `candidate-independent-01` artifact remains intact. The entry now
requires the separate `candidate-independent-02` artifact, pending one hot rebuild
and exact-list after this source update is checkpointed.

The exact selector is `^TestIndependentSecretHTTPCurrentSessionAndSafeErrors$`:

- `current-session-after-domain-begin`: successful GET/List controls with real
  Cookie authentication, then real BEGIN/backend PID, before-lock barrier, formal
  Logout, and the original callback. Callback `SessionRevoked/NotStarted`, physical
  transaction and HTTP `SessionRevoked/NotCommitted`, 401, cleared Cookie, no
  metadata, and six unchanged Secret facts must all agree. Normal Postgres release
  cancels its callback context: inspect that context before callback return, and
  the original caller context after physical transaction return. Cancellation or
  expiry of the original two-second request is a failure.
- `hostile-member-errors-are-safe`: unknown top/nested JSON member names and
  independent synthetic values must yield 400/NotStarted with no FieldErrors;
  declared value containing NUL must yield only `/request/value` with
  `INVALID_SECRET_VALUE`. Original middleware logs are bound to the response
  request ID. Body, headers and logs exclude raw/escaped/Base64/digest forms.
  Six facts stay unchanged and new target rows remain absent.

The observer is installed under the unique `hookStore` before both real fixture
assemblies, so all Account/Project/D04/Owner services share the same Store. It
delegates original contexts, transactions and CommitResults. The reused fixture
has explicitly controlled Skills initialization; this is not default-root,
Project lifecycle, native transport or Agent F1 acceptance. No public fixture or
production source is changed.

After source review/checkpoint, build with exact Go 1.27.1, read-only shared module
cache, a task-owned build cache and private telemetry mode `off`, clearing
`TEST_TELEMETRY_DIR`, `GO_TELEMETRY_CHILD`, `GO_TELEMETRY_CHILD_UPLOAD` before Go.
Set `GOTOOLCHAIN=local GOPROXY=off GOSUMDB=off`; require fresh 5 GiB first.
Compile `go test -race -tags=integration -c ./tests/projectvariable` to
`output/ai/secret-owner-http/candidate-independent-02/secret-http-independent.test`,
then exact-list the sole top. No author binary is claimed to contain this probe.

Offline orchestration controls (explicit OS doubles, no resources):

```sh
python3 .agent-state/secret-owner-http/independent-controls.py
```

Only after a fresh exclusive resource grant, from the repository root:

```sh
python3 .agent-state/secret-owner-http/independent.py \
  --driver "$PWD/output/ai/secret-owner-http/candidate-01/pg-only-driver" \
  --binary "$PWD/output/ai/secret-owner-http/candidate-independent-02/secret-http-independent.test" \
  --run '^TestIndependentSecretHTTPCurrentSessionAndSafeErrors$' \
  --output "$PWD/output/ai/secret-owner-http/independent-pg-01"
```

Use the pinned existing PG17/vector 0.8.1 image and an empty private Docker
configuration. The wrapper rejects all other selectors, aliases, modes and
artifact paths before loading the unchanged shared supervisor. Its private
namespace registers the exact one-top/two-sub PG case set and a complete input
enumerator; the actual shared `main`, observer and resource logic remain original.
Inputs include candidate/driver, source/dependency/fixture/migration closure,
formal schemas and all four independent sources, with no symlinks and initial/final
re-enumeration plus byte identity.

Budgets remain Go 6m, driver 105s plus cleanup 15s, supervisor 123s plus shared
TERM/KILL retirement 3s, and host TCP 75s with two consecutive empty observations.
One PG container and one network require exact ownership and double retirement;
private directory must contain only `owned.json`. Original Go/driver/supervisor/
outer Wait, double empty descendants and final inputs must all close. Business
PASS alone is insufficient. No automatic retry or retrospective repair of a run.
