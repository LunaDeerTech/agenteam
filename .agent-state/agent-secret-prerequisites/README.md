# Secret Agent reference prerequisite

The frozen implementation is `internal/central/projectvariable/secret_references.go` with its adjacent test file (saved source `4e921083`). It uses the existing `00030` reference table and concrete same-Store `SecretDirectory`; no migration, public contract, Model source or default-root change is included. `NewSecretReferences` requires a bound real `SecretReferenceOwnerAuthority`, including for empty sets. The final participant uses the caller's original live Tx and complete held lock union, current Owner, and the owner's private canonical-writer witness before any write. It compares the complete Before index and owner version, then writes all After entries with the result owner version. It neither reads Agent SQL nor accesses Secret material.

The two sources received skills_http's limited independent source review with no remaining must-fix. The typed Human test argument was corrected before the first Go run; it was a static preparation finding, not an executed test failure.

On 2026-10-10 at 12:36:22–12:36:29 UTC, `references-check-01` completed wholePASS: exactly four new tops and 17 subs passed under race (package 1.066s), followed by vet for `projectvariable` and its `contract` package. Commands, from this tree, used `/workspace/toolchains/go1.27.1/bin/go`:

```sh
go test -mod=readonly -p=2 -race -count=1 -timeout=120s -json -run '^TestSecretReferences(CompleteSetAndOwnerVersion|OriginalCallerAndCanonicalGate|UnboundEmptyAndWholeBefore|UnavailableTargetAndSQLFailure)$' ./internal/central/projectvariable
go vet -mod=readonly -p=2 ./internal/central/projectvariable ./internal/central/projectvariable/contract
```

Session `68693` ended `d57f2b`/0; outer 724006, race 724009 and vet 724149 all actually returned 0. The two phases took 5.902s/1.371s; original process groups, descendant samples and runtime double samples were empty, with no adopted child. Each phase passed the same-process 5GiB fresh-space gate (5,658,664,960B then 5,643,378,688B). Private XDG telemetry-off configuration preceded Go; no telemetry override was inherited, module lookup was offline/read-only, and the lifecycle hot cache was released after the complete original tails. Regenerable commands/logs/result remain under `output/ai/agent-secret-prerequisites/references-check-01*`; execution reused the existing core-checks Wait/group/reap/runtime method and supervisor descendants function.

The tests use controlled Store/Owner ports. They do not establish real PostgreSQL, Agent canonical witness integration, Agent CRUD/F1 or a production binding. Existing Directory/metadata evidence and earlier failures are unchanged; no old test matrix or real resource ran. The shared Model task's `current.md` remains under its original writer.
