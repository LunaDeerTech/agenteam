# Secret Project Audit checker — author handoff

Status: implementation and author checks complete; independent acceptance pending. All eight source files frozen. No Git writes.

## Fixed input and scope

- Baseline: `d57ce0ba60150d0377082a2996b903353e867007`; isolated snapshot in `snapshot/` contains that fixed archive plus only the eight paths in `sources.txt`.
- Final source manifest: `final-source.sha256`; SHA-256 `73a003de920ad6a8461bb60fd5c8510b1c28be4aee803cb56f9127556b27c046`. It equals the pre-PG-2 manifest; no source changes during/after either actual PG group.
- Two old production files changed only at the actual pre-Append call sites: write after canonical + encrypted receipt persistence; resolve after actual lease/grant/metadata checks and successful AEAD. No schema, public contract, Project, Object, Audit service, or old test edits; 00017 remains unused.

## Delivered behavior

`secret.NewProjectAuditAuthority(Store)` implements the existing Audit `ProjectFactAuthority`. Construction uses no SQL and does not depend on either service, so the checker can precede Project → Audit → Secret assembly. Nil, typed-nil and zero checker are unbound. Uncomparable Store instances reject, including type-comparable wrappers whose interface fields dynamically contain slices.

Private, ephemeral witnesses carry exact Store/Tx, complete Entry (full actor/session, all nine associations, metadata), whole AppendKey and safe domain facts. They retain no prepared envelopes, plaintext, semantic value digest, SecretMaterial, Keyring or Service. The checker confirms the same live Tx and already-held locks; it never acquires locks, opens a transaction, decrypts, or writes.

Mutation rechecks the exact receipt, encrypted-payload ownership and canonical create/update/delete after-state, rebuilding safe changed_fields from actual prior purpose. Existing receipt replay remains the ordinary path and adds no Audit. Resolve witnesses only the successful actual grant/AEAD path and rechecks live lease, owner/ref/consumer and current metadata/payload ownership; the subject transformation and all associations remain exact. Current Project/Session/Usage authorization remains upstream of this fact checker.

## Verification and retained raw evidence

All commands, UTC starts, durations and exit codes are in `results.jsonl`; each raw log begins with its exact command and fixed snapshot cwd. Go is `/workspace/toolchains/go1.27.1/bin/go`, version 1.27.1, offline pinned module cache, own build cache/tmp. Relevant successful logs:

| Log | Actual result |
| --- | --- |
| `logs/unit-1.log` | Secret + Audit unit tests passed |
| `logs/comparable-fix-race.log` | Secret + Audit final production/unit version passed race |
| `logs/comparable-fix-vet.log` | Secret + Audit final production/unit version passed vet |
| `logs/integration-compile-3.log` | Final security integration sources compiled; no tests executed in this compile-only command |
| `logs/pg-new-1.log` | Official owned PG helper, `^TestSecretProjectAudit`, original race/count=1/6m, exit 0; security 43.901s, helper 79.427s |
| `logs/pg-regression-1.log` | Official owned PG helper, affected old Secret and three real Account combinations, original race/count=1/6m, exit 0; security 27.965s, account 31.373s, helper 44.930s |

The actual behavior groups use PG17 170008/vector 0.8.1. The helper also created and verified its PG16 160012/vector 0.8.1 unsupported-version fixture; this is not claimed as a second behavior-suite run. Both image digests appear in the raw logs.

New PG tests exercise real Secret + encryption + Audit + PostgreSQL with strict test-only Project/Usage ports, not an allow or fake appender. Seven top-level tests include mutation/replay, forgery rollback, lease/resolve denial, current session/owner/gate revocation, live-Tx/direct-Append rejection, and mutation/resolve COMMIT uncertainty. Each uncertainty group covers committed ACK loss, real rollback, and an original server transaction still pending while the client has Unknown. Mutation arms the wire proxy only after PrepareWrite nonce transactions. Actual original lock serialization is joined before recovery conclusions; resolve returns no readable material on Unknown, and a subsequent real read creates a new resolution.

Old selected tests cover prepare/receipt replay, System behavior, leases, references/cleanup, rotation, actual old-writer fences, child crash recovery and uncertainty. The three Account tests are BootstrapLoginReplayAndIndependentResponseLeases, InvitationAtomicSecretIntentEventAndRevocation and UnknownNeverExposesUnconfirmedLoginMaterial. Selection is recorded verbatim in the log. The newly passed ProjectAudit group was excluded from that follow-up to avoid redundant execution.

Earlier red logs remain intact: `integration-compile-1` exposed the different Secret/Audit cleanup-port cause signatures plus a missing test import; a private test wrapper fixed that separation. `vet-1` exposed three unkeyed test literals, now keyed. `comparable-regression-red` reproduced the unsafe type-only Store comparability acceptance; Value.Comparable rejects the actual unsafe instance. No PG failure occurred, no assertion/budget was weakened, and no unrelated source was changed.

Final gofmt output was empty; scoped diff --check passed; all eight files additionally checked UTF-8, LF, final newline and no trailing whitespace.

## Resource handoff and limitations

`final-resource-zero.json` records each of the four owned container IDs and two network IDs absent twice. The original two-container/four-network baseline is unchanged. Owned processes are zero and `tmp/` is empty. No MinIO was started. The exclusive fixture window was returned to root after this verification; no resource/test/source commands remain running.

This delivers the real Secret-domain fact checker and its actual call sites. Production Project AuditFacts routing and the true Secret CheckMutation delegate remain unbound and are expressly not manufactured here. Positive Service resolve tests use an actual persisted lease and strict fixture Usage binding; real Human/AgentRun consumer integration is not claimed. Object checker and lifecycle progression are separate successors.
