# Agent retirement preparation validation

This slice reads a complete Agent configuration for the original active Project
Delete stop round. The [work item](../../docs/development/work-items/d08-agent-retirement-preparation.md)
is the contract. These methods have not yet run; static preparation does not
establish a basic or PG PASS.

The basic selector in [run-basic-checks.py](run-basic-checks.py) is exactly
9 top tests / 41 subcases: 6 new / 39 subcases plus 3 related old / 2 subcases.
Project and Agent are the two race-test and vet packages.
It covers the new Project and Agent controls and three related original
StopDriver tests: construction/original step, cancellation waiting for the
original step return, and unconfirmed start making no provider call. No other
legacy matrix or loader suite is added. Exact top and subcase execution comes
from the original Go JSON result, not static declaration counts.

The [PG fixture](../../tests/projectvariable/agent_retirement_preparation_test.go)
is exactly `TestAgentRetirementPreparation`, with three subcases:

- `committed-stop-round-config-snapshot`
- `same-transaction-and-callback-boundaries`
- `original-loan-return-before-driver-drain`

Only accepted Delete is a controlled SQL precondition. The real bound guard and
StopDriver create the stopping operation and running claim, then the original
step reads under the private scope, exact caller transaction and required locks.
Other participants remain required. The snapshot does not mutate Agent state,
references or history and does not establish retirement, participant completion,
production BeginDelete or a complete LifecycleRegistry.

The third subcase holds the actual admitted synchronous loan callback return
after the read. It checks that original Run/Drain waits for that return; it does
not claim a held kernel SQL operation or ownership of the outer Store return.

The methods reuse the delivered Work single-binary algorithms. Both shared
harnesses add only this selector; removing it must recover the full baseline
AST at `db80a9c4d0f2863c6246adfea8949229086aa858`. The recorder remains byte-for-byte
SHA256 `56a32b0d45a633c3c85306d098730318327c02f4ee9c5267c8d712e9880d4f20`.
Original seven resources, Go 6m, supervisor 540+60, actual child Wait, input gates
and two empty TCP observations within 75s remain unchanged. Capacity is an
actual preparation/startup observation, not a peak guarantee.

Run only in the root-assigned exclusive validation window, from this worktree.
Use fresh output directories; any actual failure stops the chain and preserves
its original result. The following commands are the fixed interfaces, not a
record of execution:

```sh
python3 .agent-state/agent-retirement-preparation/run-basic-checks.py --output output/ai/agent-retirement-preparation/basic01
python3 .agent-state/agent-retirement-preparation/profile-controls.py
python3 .agent-state/agent-retirement-preparation/run-pg-checks.py compile --output output/ai/agent-retirement-preparation/compile01
python3 .agent-state/agent-retirement-preparation/run-pg-checks.py prepare --compile output/ai/agent-retirement-preparation/compile01 --output output/ai/agent-retirement-preparation/native01 --namespace /tmp/arpr01
```

Compile binds the actual original metadata input scope, all same-package Go
fixtures and all migration embeds, then records the single race binary and sole
exact test listing. Prepare does not start Go or resources. It emits runtime
inputs and the four recorder identities for a fresh namespace. Native runs only
when the root grants its resource window, by invoking the generated
`native01/record-native.py`; it is not an implicit effect of prepare.

Preserve original tool termination and supervisor/driver/Go/resource tails.
Any copied tool observation must identify itself as assistant-observed rather
than a provider export. Relevant input drift stops execution; unchanged accepted
results may be reused. No old worktree source or binary path is borrowed.
