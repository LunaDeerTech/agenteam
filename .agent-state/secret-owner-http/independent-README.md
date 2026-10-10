# Secret HTTP independent risk probe

Status: implementation preparation only. No independent PG execution or acceptance
has occurred. Author HTTP PG/native/root results and their original failures remain
separate. The probe author did not implement Secret HTTP or its default root.
The six offline entry controls passed (tool `1f069c`, exit 0): this includes the
actual shared main's PG observer and both input passes with explicit OS doubles.
Go compilation/list and real SQL scenarios are still not run.

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
`output/ai/secret-owner-http/candidate-independent-01/secret-http-independent.test`,
then exact-list the sole top. No author binary is claimed to contain this probe.

Offline orchestration controls (explicit OS doubles, no resources):

```sh
python3 .agent-state/secret-owner-http/independent-controls.py
```

Only after a fresh exclusive resource grant, from the repository root:

```sh
python3 .agent-state/secret-owner-http/independent.py \
  --driver "$PWD/output/ai/secret-owner-http/candidate-01/pg-only-driver" \
  --binary "$PWD/output/ai/secret-owner-http/candidate-independent-01/secret-http-independent.test" \
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
