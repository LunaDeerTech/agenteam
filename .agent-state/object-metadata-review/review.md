# D05 first cleanup pair: limited independent review

Stable source: `/workspace/agenteam-object-metadata-cleanup`, `6c120e42`.
No must-fix found in the two new exact mappings and the selected business tests.
The previously reviewed production implementation was not re-reviewed in full.

```sh
python3 .agent-state/object-metadata-review/mapping-controls.py
```

Actual `227b4b`, exit0: removing the single added selector line from each tool
reproduces `b44f46cd` byte-for-byte. Actual configuration has one positive and six
nonexact selector rejections; actual observer has seven positive/negative cases,
with 14 explicit resource-observation substitutes and both private/runtime tails
per case. It never launches a driver, fixture or business binary. Initial
`24576d` was this proof's `selectors.py` shadowing Python's standard module;
renaming to `mapping-controls.py` fixed the setup failure, without author edits.

Original budgets remain nonroot123/3, root540/60 (+ original3 SIGKILL join),
Go6m/TCP75/seven resources. The earlier [123,1] pure trace describes the original
nonroot TERM wait `min(1, grace/3)`; direct/adopted reaping shares the same total3.
It is not a new tool budget. No actual-tail claim comes from the resource doubles.

Static business findings:

- `metadata_cleanup_test.go` produces 65 histories through actual ReadObject,
  EOF and Close, then original Stop/Release/2s physical cleanup and one native
  ObjectDelete Audit. No native joined/cleaned/io_closed success fact is seeded.
- Each real transaction's observed cross-table row reduction must be 1..32;
  reuse of the live union is rejected and rolls back the first batch. Historical
  batches retain four anchors. The separate final transaction is deliberately
  rolled back once, proving all four anchors plus the explicit fixture parent
  remain, before final commit removes both. All-empty does not authorize a new
  standalone purge. These are source assertions, not observed SQL results.
- `metadata_cleanup_unknown_test.go` arms the existing PostgreSQL frame proxy
  only after final native and fixture-parent writes. It selects real COMMIT
  not-forwarded versus server COMMIT-completed/response-lost. The original
  transaction must actually return Unknown with valid attempt and original typed
  cause. An independent direct Store checks all-four-retained or all-empty;
  the unforwarded branch rediscovers and resumes with original cause, retaining
  one native Audit. No CommitResult is substituted.
- Fixture authority explicitly represents Project/Skill owner-domain facts.
  It validates live same-Store Tx/locks/current mapping but is not production
  Skills CleanupAuthority or its final-five-core-row transaction. Those remain
  separate integration work. Existing proxy actual copier/connection joins and
  original fixture cleanup remain in the test chain.

Not run: either business top, PG/MinIO/socket, migration28, SQLSTATE/rollback,
1001-history case, query plans or Skills final transaction. Author race/list
preparation remains preparation; this review grants no real-resource execution.
