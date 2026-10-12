# Model successor pure contract

The schema-3 codec, unchanged Execution request wrapper and single-call auto response validator have limited static acceptance. The new product controls have **not run**. This is a pure data slice: no Model Begin/Result/Close, credential read, Runner, invocation/usage write, default binding, new ledger, PG or native profile. The existing capture-only Runtime rejection and real tools HTTP STOP remain. The stored 25,182,208-byte budget does not enlarge the 16 MiB wire limit. See the [task card](../../docs/development/work-items/d22-model-successor-contract.md).

## Independent legacy golden

Before either new codec or response implementation was written, the unchanged delivered `448f6adea22d62dcb71255a3c834dbb8f1b95d84` builder/codec captured three deterministic synthetic fixtures: Read with Control, 512 KiB Write, and Command with two escaped 64 KiB streams and exit 23. This was not a real PG/Model/Runner execution.

The original tool `65068` / chunk `c40ba4` returned 0; dependency Go PID `1056100` and capture Go PID `1056147` each actually returned 0. The recorder bound 628 inputs, with the 625 pre-existing repository inputs matching the old source. The [independent check](../../output/ai/model-successor-contract/golden01/independent-check.json) confirms the exact top/three subs, original bytes, semantic substring, both SHA-256 digests and original process returns. The [tool observation](../../output/ai/model-successor-contract/golden01/tool-observation.json) retains the tool-return provenance.

Seven original files total 3,787,213 bytes. The [manifest](../../internal/central/execution/contract/testdata/successor_legacy/manifest.json) SHA-256 is `6f559fe198c72b86e05aba51bca77ab5495cd93f63385df8766f5f82357b0781`; its three request/semantic pairs were copied byte-for-byte from `output/ai/model-successor-contract/golden01/vectors/` into the adjacent testdata directory. The compatibility test pins the manifest SHA and compares all six original files and both digests. The new helper never generates expected values.

The temporary capture top has been removed from the final test package. Its unchanged [test source](legacy-capture-test.go.txt) (SHA `cb387a89d1419a323016f667197291bec224a10999c8adb8fe25114f76317975`) and [capture method](capture-legacy.py) remain historical evidence. Do not rerun that capture against the new implementation or recreate golden files with the new algorithm. Original records under `golden01/` are not new product-test results.

## Exact basic scope

`run-basic-checks.py` has one `full` scope. Static declarations are **9 new tops / 68 subs plus 2 old tops / 15 subs: 11 tops / 83 subs**, in three race test packages, followed by the same three packages' vet:

| Package under `internal/central` | New tops / subs | Related old tops / subs |
| --- | --- | --- |
| `model/contract` | 4 / 25 | 0 / 0 |
| `execution/contract` | 1 / 3 | 0 / 0 |
| `agentloop` | 4 / 40 | 2 / 15 |

The four Model controls exercise full semantic identity/digest, canonical rejection, protected copies/defaults and escaped size/context limits. The Execution control compares the independent old Read/Write/Command vectors through the real new decoder, constructor and Model validator. The four new Loop controls cover complete text/single-call, malformed/unsupported shapes, interrupted facts and profile/ownership separation.

The only reused old tops are `TestNextRoundRequestCodecRejectsIncompleteBinding` (its actual source contains 15 named subcases) and `TestNextRoundRequestCountsEscapedSizeBeforeEncoding` (no subcases). The new Loop tests declare 4 + 20 + 7 + 9 subs, Model 12 + 10 + 0 + 3, and golden compatibility 3. The method checks actual Go JSON top selection and actual new/old sub run/pass counts; these declarations alone are not a passing result. No previous complete Operation/WSS/Model matrix is repeated.

## Original recorder and bounded changes

The method derives from the accepted [Command result basic recorder](../run-command-result-consumption/run-basic-checks.py). Package/selectors and required sources are narrowed to this slice. Original offline fixed Go, read-only modules, hot cache location, max1/race/count1/p1, 10-minute timeout, `Popen`/actual `Wait`, fresh exclusive output files, complete `go list -deps -test -json` race/vet source union and before/after hashing remain. ROOT derives from this script's location; no retired tree is used as a running dependency.

The seven golden files are explicitly added to that input union: they are read by the compatibility test at runtime rather than embedded by Go. Their original pre-discovery SHA values are checked through discovery, unit execution and vet. The method does not replace the actual compiler closure with a hand-maintained source count. Any migrations that really enter the discovered closure are recorded; this pure scope does not claim to load or apply SQL. The other bounded addition records and checks the actual 68 new and 15 old subcases in the original selection result.

Only a separately authorized Go window may run:

```sh
python3 -B .agent-state/model-successor-contract/run-basic-checks.py --output output/ai/model-successor-contract/basic01
```

The output directory must be fresh. A failed stage stops the run and preserves its original records; no automatic retry, capture regeneration or native preparation follows. Successful pure checks still do not grant invocation permission, enable a Runtime adapter, change HTTP STOP, or establish a real successor Model execution.
