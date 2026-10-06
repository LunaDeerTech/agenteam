# Invocation / Usage core-freeze-01 independent review

Conclusion: one required SQL correction, U-R01. This is a static core review, not ledger acceptance or real PostgreSQL validation.

Input is author manifest c9f0c583ae690c40b8b59e0fb7e8b10a42a1ff7a1dea67cc7b8d5b105871f251, baseline e87c6ed66b07d58f103b0e4b5c9239fb4db5617a, all 14 frozen paths. The author's message used d09e8ef; the manifest's e87c6ed is authoritative and differs only in recovery documentation. Every frozen SHA and every one of the 18 files recorded by pure-01 matches the frozen core plus baseline Git objects. Original core-freeze-01 remains unchanged.

## Required correction

U-R01: db/migrations/00018_model_invocation_usage.sql lines 55-56 do not reject a missing terminal_version when the remaining terminal fields are populated. With non-null final_status/finished_at/final_digest but terminal_version=NULL, the no-final branch is false and the final branch evaluates to NULL; PostgreSQL CHECK accepts NULL. The independent loader rejects such a row, but this does not satisfy the required SQL terminal-group constraint. Add terminal_version IS NOT NULL (or an equivalent complete IS TRUE condition) and a real 23514 counterexample. Reported immediately to root and author; the author states it is corrected in the working candidate. That replacement is not part of the reviewed core01 bytes and remains to be matched and tested.

## Reviewed safeguards

- Construction checks typed nil (including channels), required reader ports, comparable identical Store and a valid cursor keyring; zero services fail safely. Identity changes only add the ModelRuntime registration.
- Request binding includes the complete Actor, including Session, and complete invocation identity/access/action/sequence. The service issuer cannot be substituted by a caller-issued plan. Discovery includes local call/invocation/project/summary locks and the entire provider lock set; InTx verifies the same Store, exact plan/union and held locks before the provider is consumed. Reserve with live links additionally requires held Provider/Model locks.
- Current reserve/authorized authority and exact technical observe/finalize/confirm actors are separated. Provider authorization/mapping/witness correctness remains a real-fixture validation obligation; the provider is trusted infrastructure, not a bearer DTO.
- New observations construct receipts and validate summary deltas before one data-modifying CTE updates the invocation, exact observation and execution summary. Numeric delta/version rejection occurs before SQL. Actual swallowed-error rollback/commit behavior still requires the original PG transaction tests.
- Old Action/Sequence receipts are read from observations, compared with current authorized canonical facts and returned without overwriting the latest event; live FK projection is refreshed without changing the stored digest. A same final at a continuous new sequence adds a receipt without incrementing the summary. The original canonical writer lock comes from the provider and must be demonstrated by the strict fixture.
- List/Aggregate/Get/Rebuild apply current Human Session and exact Project Read grants. Cursors bind stable user/filter/group/order, carry six signed scalars, preserve first upper bound/default time range and use keyset pagination. Aggregate bounds before grouping; every query uses the two-second caller-bounded context and returns zero result on transaction/error. Real nonempty coverage, SQL plans and timing remain unverified.
- Nullable contributions distinguish zero from unknown, use numeric/big integer intermediates and reject overflow. Rebuild holds the execution summary lock while validating canonical rows and aggregating; unchanged summaries retain their version.

## Evidence and limits

Reused author pure-01: actual go test -count=1 ./internal/central/usage/... ./internal/central/identity/contract, exit 0, three packages. These tests are narrow construction/contract/arithmetic checks and do not prove provider authority or database behavior.

Independent dependency check: Go 1.27.1/local, offline/readonly, go list -deps -test -json on a minimal 87-file frozen Git/author closure, exit 0. All local package directories are inside the verifier-owned source directory. It includes model/contract and excludes internal/central/model implementation and every active management-read path. Commands/raw metadata are under logs/; dependency-proof.json and input.json record the result. No product tests or fixture were launched by this verifier for this review. Final source fingerprints and empty private TMPDIR are recorded in final-checks.json.

The six integration sources are still being authored and were not reviewed. Unit race/vet, integration compilation, both command builds, all eight real TestUsage groups, current-authority/technical-finalization, swallowed-error atomicity and Unknown historical confirmation still need their applicable evidence. Preserve the old wire03/schema02 administrative facts without claiming the missing raw archives were verified. Production Runtime/Provider orchestration/lease/lifecycle/HTTP/root remain outside this library result.
