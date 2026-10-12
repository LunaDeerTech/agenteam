# Next Round Model Request (NRMR-1)

Source and method preparation only. No basic, PG compile, native or service run has been performed for this slice. The shared-harness comparison baseline is `93df5f41774262c706df8260ef1b3bf932095b52`; later method or documentation commits do not establish execution success.

The inherited Command result-consumption source tested at `1ab01c81352f254ee45123ed29a08f40b1c310c1` has its own limited `native03` whole PASS and is integrated into main `f1de19746a1997ddb0492c4073afdac07c7ae6a7`. That accepted prerequisite is distinct from this unrun NRMR profile; its old PASS is not reused as a passing NRMR method or business result.

This slice persists one complete versioned request for an already committed `(ExecutionID, RoundInputID)`, with a stable logical CallID, semantic input digest and complete request digest. The original Actor, Model snapshot, credential reference/lease and all historical messages and Tools remain bound. The explicit new request profile is auto/text/AgentRetry. A valid candidate or a committed request is data, not authority to invoke Model, read credentials or dispatch Runner. Existing tools HTTP STOP and default production bindings are unchanged.

## Frozen scopes

`run-basic-checks.py` selects exactly eight new behaviour tops / 45 declared subs, with no added old matrix:

- Execution: `TestExecutionRoundModelRequestStoredIdentityAndObservation`, `TestExecutionRoundModelRequestTransactionAndCurrentGates`, `TestExecutionRoundModelRequestUnknownAndStopWaitForOriginalReturn`, `TestExecutionRoundModelRequestReadWaitsForActualCommit` (four tops / 20 subs).
- AgentLoop: `TestNextRoundRequestPreservesProfilesAndStableIdentity`, `TestNextRoundRequestCodecRejectsIncompleteBinding`, `TestNextRoundRequestOwnsProtectedCopiesAndRejectsBadSources`, `TestNextRoundRequestCountsEscapedSizeBeforeEncoding` (four tops / 25 subs).

These are controlled codec/source/Store tests, not real PostgreSQL commitment or Model execution. Two race test packages and three vet packages (`execution`, `agentloop`, `execution/contract`) use the accepted offline/max1 discovery, complete actual compiled input union, original Popen/Wait and before/after identities. Every embedded migration, including `00072_execution_round_model_requests.sql`, is required in the actual closure. Read/Write/Command projection controls retain their existing budgets and bytes; no previous Runner/WSS matrix is repeated.

The real PG profile is exactly `^TestNextRoundModelRequest$`, in `tests/projectvariable`, with two subs:

- `committed-command-input-request-and-history`: the original real Command setup completes one exit23 command, formally revokes dispatch Tool/Mount grants, consumes its known report and commits Determine. The actual binder then uses the same transaction's complete historical loan to persist one request. Independent SQL checks every stored identity, canonical bytes, digests and stable CallID; repeated Prepare and original/new binder Lookup return that same request without altering the old Execution, Snapshot, pending Round, ToolCall, transcript or successor input.
- `request-rollback-and-commit-loss-lookup`: the same real predecessor chain is independently established. A callback-tail sentinel rolls the complete request candidate back, and independent SQL proves zero request rows. After known NotCommitted, one further Prepare physically commits; a transparent original-return barrier then hides only its acknowledgement. No success is published before that return. The retained original Unknown is resolved only by original/new binder Lookup, with no further Prepare, request rewrite or new CallID.

Each sub's prerequisite setup has **Model 1 / wire 1**. Request binding adds **Model 0 / Runner 0**; this does not mean the whole chain makes zero calls. The inherited Model JSON boundary and Project test-Skills boundary remain controlled. The new binder does not resolve Model, read credentials, create Invocation/Usage, append a transcript entry, launch a next Model round or retire Execution. A controlled COMMIT acknowledgement loss is not a claim about an unknown physical database outcome. Actual caller/Store/loan returns and the existing fixture owners' Stop/Drain/Joined cleanup remain mandatory.

## Methods and original gates

The two shared harness files add only this profile. `profile-controls.py` compares their complete AST and effective mappings, after removing the single new profile, to the fixed baseline. It specifies one accepted and seven rejected log shapes: missing sub, failed case, nonzero Wait, extra case, duplicate Wait, wrong selector and overall failure. Static syntax or declared counts do not prove those controls ran.

`run-pg-checks.py compile` captures the original full metadata input scope, all same-package Go files and all embedded SQL, requires actual migration 72, builds one race binary and lists only the exact top. It saves actual subprocess PIDs/Wait and source/binary identities; a failed compile/list stops the stage. `prepare` requires those successful original records and unchanged actual inputs, then creates a fresh recipe such as `/tmp/nrmr01`. It starts no Go, PG, container, service or TCP observation.

The copied thin `record-native.py` is byte-identical to the accepted recorder, SHA `56a32b0d45a633c3c85306d098730318327c02f4ee9c5267c8d712e9880d4f20`. Actual native execution requires a separate root window. The original seven exact resources, Go 6m, driver 540+60s, original actual Wait, descendant/private/runtime double tails and automatic HOST_TCP delta double-empty within 75s remain unchanged. No manual baseline, prewarm, TIME_WAIT filtering or automatic retry is added.

## Commands awaiting authorization

The following are planned invocations, not execution evidence. All output paths must be fresh; preserve any failed round and allocate a separately authorized successor rather than overwriting it.

```sh
python3 -B .agent-state/next-round-model-request/profile-controls.py
python3 -B .agent-state/next-round-model-request/run-basic-checks.py --scope full --output output/ai/next-round-model-request/basic01
python3 -B .agent-state/next-round-model-request/run-pg-checks.py compile --output output/ai/next-round-model-request/compile01
python3 -B .agent-state/next-round-model-request/run-pg-checks.py prepare --compile output/ai/next-round-model-request/compile01 --output output/ai/next-round-model-request/native01 --namespace /tmp/nrmr01
```

Only a separate native authorization permits running the generated `native01/record-native.py`. Preparation and capacity snapshots do not guarantee available runtime space or a passing test. The task [card](../../docs/development/work-items/d22-next-round-model-request.md) records product scope; actual results belong in the original fresh output records when the corresponding run is authorized.
