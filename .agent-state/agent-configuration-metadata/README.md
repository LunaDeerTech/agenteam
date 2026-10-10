# Agent configuration metadata: first real PG chain

This is one integration test, `^TestAgentConfigurationMetadata$`, in
`tests/projectvariable/agent_configuration_metadata_test.go`, with exactly three
subtests: `normal-metadata`, `current-and-stale`, and `caller-rollback`.

The integration tree combines main `728` with Model Selection's four Go files
from `171aee` (implementation first saved in `c5de6ad4`) and Secret Directory's
two Go files from `e591c5`. Their accepted unit checks are reused; this test does
not repeat the old Runtime suite or implement Agent reference persistence.

## Actual services and data

`newSecretOwnerFixture` supplies a migrated task-owned PostgreSQL database,
real Bootstrap/Invitation/Redeem/Login identities, current Session and Project
Owner checks, real Project creation, and formal Secret variable commands.
Its persistent test Skills initialization receipt is explicitly a fixture;
it does not bind the default production Project initializer. Unavailable
lifecycle participants remain unavailable.

The new test assembles Model Authority, Audit's Model authority, the real Model
Secret usage router, real Secret UsageOperations, Model events/Outbox and Model
Service on that same Store. System Provider/Model creation uses the real admin;
Project Provider/Model creation uses the real Owner. Selection always checks
the requesting Project Owner, including for System Models. No SQL seeds Model,
Secret, Agent or reference rows; no Provider invocation is made.

Each pair of Discover calls binds the same Human, Project and original
`project / [ProjectID] / agent.create / key` command. The test consumer
normalizes and acquires the complete union of both returned plans once, before
calling both final Require methods in its original live Store transaction.
It neither discovers inside the final transaction nor supplies late locks.

## Fixed checks

- `normal-metadata`: System and same-Project primary chat selection with its
  advertised effort, then same-Project approval selection with nil effort.
  Every selection is combined with five independently established Secret
  cases: valid, deleted, missing, foreign Project and ordinary variable. The
  formal statuses are `valid`, `removed` and `not_in_scope`; the last three
  cases must not return metadata. Exact Model identity/scope/version/capability
  facts and valid Secret metadata are checked.
- `current-and-stale`: the other Human and admin cannot discover the Owner's
  Project metadata. A freshly logged-in Owner discovers both plans, then formal
  Logout revokes that exact Session; both final ports reject in real caller
  transactions. Formal Model update and Secret metadata update independently
  invalidate the previously issued mapping; no new plan substitutes for the
  stale request under test.
- `caller-rollback`: both Require calls succeed in the original caller Tx,
  which inserts one task-private marker and intentionally returns an error.
  Actual NotCommitted plus absence of the marker proves the caller's physical
  rollback. This does not claim atomic Agent creation; metadata itself is read
  only. No Agent or reference is written.

Twelve actual counts before/after metadata operations cover references,
snapshots/bindings/invocations, Secret leases, Model catalog/commands, Secret
commands, Audit and Outbox. All remain unchanged across reads and rejected or
rolled-back consumers. Fixture setup and deliberate formal mutations are
outside their corresponding read-only comparison windows.

## Build and controlled execution

The candidate is
`output/ai/agent-configuration-metadata/metadata-race.test`, built once with
fixed Go 1.27.1 using `go test -p=2 -race -tags=integration -c -o <candidate>
./tests/projectvariable`, followed by exact `-test.list` discovery. Build and
real execution require separately granted windows, fresh space of at least
5 GiB, private XDG telemetry mode `off`, the three telemetry bypass variables
unset, offline module lookup, and the explicitly handed-over build cache.

The existing root-chain supervisor/driver is the only real execution entry:
`pg_only_supervisor.py --driver root_chain_driver.py --binary <candidate>
--run '^TestAgentConfigurationMetadata$' --output <fresh-output> --root-chain`.
The entry owner adds only this exact selector, mandatory source closure and
necessary native entry controls. Existing seven-resource fixture ownership,
Go timeout, supervisor budget, actual Wait, descendant adoption, runtime,
resource, input and host-TCP final checks remain unchanged. No new launcher,
resource retry, or alternate success path is introduced here.

Current status: test source is formatted; compilation and real PG execution
have not run. Source review and a successful test discovery do not count as
business acceptance. An original failure will be kept with its actual tails.
