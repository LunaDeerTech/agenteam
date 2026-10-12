# Agent retirement preparation validation

This slice reads a complete Agent configuration for the original active Project
Delete stop round. The [work item](../../docs/development/work-items/d08-agent-retirement-preparation.md)
is the contract. The scoped basic and real PG checks passed at tested revision
`0c8532d881ef8688ca9e3b711ea5e387690f10c7`; the results below do not establish
Agent retirement or a complete lifecycle integration.

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

## Actual validation

- **Basic:** original tool `58407/0d7f8c` exited 0. The method returned after 117.372s; Project and Agent race tests ran exactly 9 top tests / 41 subcases, all passed without failures or skips, and both package vets passed. Race PID1474267 and vet PID1475194 actually returned 0. The 535-input six maps and 13 outer inputs were unchanged. See the [original observation](../../output/ai/agent-retirement-preparation/basic01-outer/tool-observation.json) and [post-check](../../output/ai/agent-retirement-preparation/basic01-outer/post-check.json).
- **Controls and preparation:** original `98657/6f2eff` exited 0 after 145.951s. The [controls log](../../output/ai/agent-retirement-preparation/sequence01/controls01.log) records 1 positive / 7 rejections and preserved prior harness profiles. Actual compiler PID1476494, exact-list PID1478292 and prepare PID1478361 all returned 0. Compile bound 2,080 inputs; runtime added only the same race binary, for 2,081 inputs, with 29 outer inputs unchanged. The [binary record](../../output/ai/agent-retirement-preparation/compile01/binary.json) identifies 92,953,158B, SHA256 `361b8499a8f6dcfe47b20e41a2d055f5124a8211a5c103adf734200643f29018`; see [sequence post-check](../../output/ai/agent-retirement-preparation/sequence01/post-check.json).
- **Native:** original `71336/d2c944` exited 0; supervisor PID1478997 actually returned 0 after 138.852s, and driver PID1478998 / Go PID1480740 also returned 0. The one top test and three subcases passed in 35.24s (12.42 / 12.13 / 10.69s). The [original observation](../../output/ai/agent-retirement-preparation/native01/tool-observation.json) and [post-check](../../output/ai/agent-retirement-preparation/native01/post-check.json) bind all 2,081 runtime inputs, four recorder identities, unchanged tested HEAD, seven resource IDs observed absent twice, two empty descendant/private/runtime tails and two empty TCP observations within the original 75s gate. No race was reported. Raw log: `/tmp/arpr01/pg-4c3d35d68e6c46578e729d7410d8a9fa.log`.

The accepted Delete SQL precondition is controlled; stopping/running claim issuance, complete read-only configuration and exact callback-return ownership use the real StopDriver and reader. Canonical Agent configuration and references remain unchanged. The held callback follows the completed read: its actual return gates Run/Drain and attempt checkpoint, not a kernel SQL hold or an outer Store return tail. Fixture setup is not acceptance of Project creation HTTP or complete production Skills. No Agent retirement, participant completion, full registry or existing STOP is unlocked. Tool-observation files are assistant-observed transcriptions, not provider exports; all original execution and resource tails completed before these documentation changes.
