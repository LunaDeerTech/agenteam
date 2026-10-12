# Model successor pure contract

The schema-3 codec, unchanged Execution request wrapper and single-call auto response validator passed the complete limited pure scope at `a9e69b7c8eb67318cf7c69d3bb7686d85e780d39`: **11 tops / 83 subs and three package vets** in `basic02`. The original `basic01` whole FAIL remains intact. This records candidate validation, not a main integration, push, retirement or cleanup claim. This is a pure data slice: no Model Begin/Result/Close, credential read, Runner, invocation/usage write, default binding, new ledger, PG or native profile. The existing capture-only Runtime rejection and real tools HTTP STOP remain. The stored 25,182,208-byte budget does not enlarge the 16 MiB wire limit. See the [task card](../../docs/development/work-items/d22-model-successor-contract.md).

## Independent legacy golden

Before either new codec or response implementation was written, the unchanged delivered `448f6adea22d62dcb71255a3c834dbb8f1b95d84` builder/codec captured three deterministic synthetic fixtures: Read with Control, 512 KiB Write, and Command with two escaped 64 KiB streams and exit 23. This was not a real PG/Model/Runner execution.

The original tool `65068` / chunk `c40ba4` returned 0; dependency Go PID `1056100` and capture Go PID `1056147` each actually returned 0. The recorder bound 628 inputs, with the 625 pre-existing repository inputs matching the old source. The [independent check](../../output/ai/model-successor-contract/golden01/independent-check.json) confirms the exact top/three subs, original bytes, semantic substring, both SHA-256 digests and original process returns. The [tool observation](../../output/ai/model-successor-contract/golden01/tool-observation.json) retains the tool-return provenance.

Seven original files total 3,787,213 bytes. The [manifest](../../internal/central/execution/contract/testdata/successor_legacy/manifest.json) SHA-256 is `6f559fe198c72b86e05aba51bca77ab5495cd93f63385df8766f5f82357b0781`; its three request/semantic pairs were copied byte-for-byte from `output/ai/model-successor-contract/golden01/vectors/` into the adjacent testdata directory. The compatibility test pins the manifest SHA and compares all six original files and both digests. The new helper never generates expected values.

The temporary capture top has been removed from the final test package. Its unchanged [test source](legacy-capture-test.go.txt) (SHA `cb387a89d1419a323016f667197291bec224a10999c8adb8fe25114f76317975`) and [capture method](capture-legacy.py) remain historical evidence. Do not rerun that capture against the new implementation or recreate golden files with the new algorithm. Original records under `golden01/` are not new product-test results.

## Recorded results and retained failure

- **basic01 whole FAIL:** original tool `51218` / completion chunk `120b05` returned 1. Race PID `1067056` actually returned 1 in 86.682s: ten tops / 79 subs passed, while `TestSuccessorResponsePreservesCompleteTextAndSingleCall` and its four subs failed; vet did not run. The [original post-check](../../output/ai/model-successor-contract/basic01-outer/post-check.json) retains the initial unassigned diagnosis, all four unchanged 655-input maps and seven golden hashes. Its [tool observation](../../output/ai/model-successor-contract/basic01-outer/tool-observation.json) is explicitly assistant-observed, not a provider-exported receipt.
- **Test-only correction:** later source review identified a `reflect.DeepEqual` comparison of function-backed opaque request values, which cannot establish their semantic equality. Commit `a9e69b7c8eb67318cf7c69d3bb7686d85e780d39` checks the opaque Actor/lease/reference identities through their real equality methods, then compares the remaining complete request copies. Only `internal/central/agentloop/successor_response_test.go` changed between the two runs; production code, golden bytes, budgets and assertions of complete identity remained in scope. This later diagnosis does not rewrite the original failed observation.
- **basic02 whole PASS:** original tool `61850` / completion chunk `66a3f9` returned 0. [Outer terminal](../../output/ai/model-successor-contract/basic02-outer/terminal.json) records outer PID `1072022`, child `1072047`, actual Wait 0 / 52.985s. [Race](../../output/ai/model-successor-contract/basic02/unit01-result.json) PID `1072160` returned 0 / 18.300s; [selection](../../output/ai/model-successor-contract/basic02/unit01-selection.json) has exactly 11 tops / 83 subs, each run/pass once, no fail or skip. [Vet](../../output/ai/model-successor-contract/basic02/vet01-result.json) PID `1072406` returned 0 / 32.199s for all three packages.

The [basic02 post-check](../../output/ai/model-successor-contract/basic02-outer/post-check.json) confirms six identical 655-input maps, 22 unchanged outer inputs, seven unchanged original golden files, and exactly the one test-file hash change from basic01. The [tool observation](../../output/ai/model-successor-contract/basic02-outer/tool-observation.json) preserves the actual tool-return provenance separately from the child process record. The original unit/outer logs, maps and FAIL are preserved; neither a simulated response nor a codec result is represented as an actual Model invocation. There was no PG/native run.

## Exact basic scope

`run-basic-checks.py` has one `full` scope. The declared and actually passed `basic02` selection is **9 new tops / 68 subs plus 2 old tops / 15 subs: 11 tops / 83 subs**, in three race test packages, followed by the same three packages' vet:

| Package under `internal/central` | New tops / subs | Related old tops / subs |
| --- | --- | --- |
| `model/contract` | 4 / 25 | 0 / 0 |
| `execution/contract` | 1 / 3 | 0 / 0 |
| `agentloop` | 4 / 40 | 2 / 15 |

The four Model controls exercise full semantic identity/digest, canonical rejection, protected copies/defaults and escaped size/context limits. The Execution control compares the independent old Read/Write/Command vectors through the real new decoder, constructor and Model validator. The four new Loop controls cover complete text/single-call, malformed/unsupported shapes, interrupted facts and profile/ownership separation.

The only reused old tops are `TestNextRoundRequestCodecRejectsIncompleteBinding` (its actual source contains 15 named subcases) and `TestNextRoundRequestCountsEscapedSizeBeforeEncoding` (no subcases). The new Loop tests declare 4 + 20 + 7 + 9 subs, Model 12 + 10 + 0 + 3, and golden compatibility 3. The method checks actual Go JSON top selection and actual new/old sub run/pass counts; the complete `basic02` run/pass result below is the execution evidence, rather than those declarations alone. No previous complete Operation/WSS/Model matrix is repeated.

## Original recorder and bounded changes

The method derives from the accepted [Command result basic recorder](../run-command-result-consumption/run-basic-checks.py). Package/selectors and required sources are narrowed to this slice. Original offline fixed Go, read-only modules, hot cache location, max1/race/count1/p1, 10-minute timeout, `Popen`/actual `Wait`, fresh exclusive output files, complete `go list -deps -test -json` race/vet source union and before/after hashing remain. ROOT derives from this script's location; no retired tree is used as a running dependency.

The seven golden files are explicitly added to that input union: they are read by the compatibility test at runtime rather than embedded by Go. Their original pre-discovery SHA values are checked through discovery, unit execution and vet. The method does not replace the actual compiler closure with a hand-maintained source count. Any migrations that really enter the discovered closure are recorded; this pure scope does not claim to load or apply SQL. The other bounded addition records and checks the actual 68 new and 15 old subcases in the original selection result.

The accepted run used the following fresh output directory. This is an executed recipe for reference, not authorization to repeat or overwrite it:

```sh
python3 -B .agent-state/model-successor-contract/run-basic-checks.py --output output/ai/model-successor-contract/basic02
```

Any separately authorized future run must use a new output directory. A failed stage stops the run and preserves its original records; no automatic retry, capture regeneration or native preparation follows. Successful pure checks still do not grant invocation permission, enable a Runtime adapter, change HTTP STOP, or establish a real successor Model execution.
