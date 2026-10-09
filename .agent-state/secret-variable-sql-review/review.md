# D04 migration 00029 limited review

Reviewed `17969f85`, only `db/migrations/00029_project_variable_receipts.sql`.
No must-fix found against the accepted D04 rev2 and D10 A storage contract.
The 66-line DDL is unchanged at review completion. Author SQL tests were only
preparation and were not used as evidence of PostgreSQL behavior.

Actual offline command:

```sh
python3 .agent-state/secret-variable-sql-review/check.py
```

`10291 → 072d86`, actual exit 0. The command verifies the frozen DDL, statement
identity to its accepted draft, unchanged migration00003, the additive table/FK
boundary and declared access paths. It invokes actual production
EmbeddedSource/NewSource through a read-only Go overlay: prefix28 and target29
are continuous transactional sources; six mutations (hole, duplicate, displaced
29, non_tx without recovery, Down, missing Up) are rejected. It never calls
Migrator.Migrate, constructs a database fixture, or parses/executes PostgreSQL
statements. No reviewed source or cache was written.

## Constraint and ownership findings

- Existing kind1 bounds and kind2 48-byte CHECK remain. Kind3 admits only Project
  scope and 48-byte ciphertext, inheriting the original non-null Project/scope,
  nonce, format, algorithm, wrapped-DEK, registry and positive-revision checks.
  The SQL length alone does not prove AEAD/AAD or 32-byte plaintext validity.
- Only canonical secrets gain `project_variable`, only in Project scope. Legacy
  receipt purpose, reference/lease consumers and Purpose.Valid stay closed;
  legacy metadata loading rejects the stored new purpose. This migration does
  not grant a new consumer or public plaintext path.
- The new same-schema UUIDv7 domain is additive. All identity columns are
  non-null/PK. The operation CHECK explicitly separates create's NULL expected
  from update/delete's non-null positive external expected. Create yields
  version1/not-deleted; replace/delete require internal version>=2; metadata
  none retains a positive internal version. No invalid equality between D10's
  external version and Credential version is introduced.
- `(project_id,command_digest)` and `digest_payload_id` uniqueness provide the
  declared idempotency and payload ownership keys. The deferred payload FK
  supports same-transaction insertion/deletion. There is no FK to current
  Credential, current Variable, User or D10 canonical rows and no CASCADE, so
  retained history can outlive credential deletion and current mappings.
- This FK only proves referenced payload existence. It does not prove kind3,
  receipt owner ID, Project/scope or reverse ownership. Existing native
  checkAuditPayload and kind3 reverse lookup compare the actual tuple; cleanup
  rejects a malformed bounded head before deleting receipt/payload pairs.
  There is no new trigger pretending to enforce these cross-row facts. Skills
  separately owns the Go implementation review; no duplicate Go acceptance is
  asserted here.
- PK serves exact receipt/reverse-owner lookup; the new Project/id index serves
  ordered bounded receipt cleanup; the two uniqueness indexes serve command
  lookup/payload FK checks. Original payload rotation and Project indexes remain.
  Their declaration matches current query prefixes, not an EXPLAIN result.
  Cleanup SQL remains LIMIT100 and tuple-qualified, without relying on a current
  Credential row; deleted-credential replay/rewrap remains representable.

## Finite real PostgreSQL work still required

1. Execute actual Migrator on a fresh database and a populated 28→29 upgrade;
   retain kind1/2 and legacy rows, inspect the actual named CHECK/FK constraints
   and journal/Goose facts, then repeat startup. Prefix28 itself has not been
   established by this review. A transactional failure must preserve the prior
   schema/data and normal migration recovery behavior.
2. Exercise UUID, null/presence, operation/effect/version/deleted, kind/scope and
   ciphertext bounds; both independent uniqueness constraints; deferred FK
   order and commit rejection of a missing payload. Check actual SQLSTATE, not
   a parser or SQL substitute. Keep external and internal versions deliberately
   different in a valid case.
3. With real Service/Store transactions, verify history after replace/delete,
   same original-key replay and changed-intent refusal, legacy purpose/consumer
   rejection, and malformed kind/owner/payload/Project tuples rejected by the
   native readers and maintenance. The DDL is not an authorization proof.
4. Run kind3 plus retained kind1/2 rotation/canary/retire and 100/101 receipt
   cleanup, including deleted Credential, missing-owner/live-payload failure,
   fully removed payload handling and commit-Unknown non-completion. Verify
   actual bounded rows/locks and relevant plans; retain original cleanup gates,
   budgets and full resource joins.

No SQLite, PostgreSQL, socket, browser or network command ran in this review.
