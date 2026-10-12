# Agent Project local stop

The local provider and its bounded checks passed at `fe6beb28031caf5b80ee55feda4a476253d9db4b`. Product/controls were checkpointed at `d31e184c1846fc485161f138ccfccdb217255e4e`, and the real PG fixture plus optional assembly seam at `07f4061e3d7084941d1f7bb19a1640973fc4fb12`. Integration into main and worktree/branch cleanup are separate delivery steps. The [work item](../../docs/development/work-items/d08-agent-project-local-stop.md) defines the scope.

## Basic, compile and input identity

The original `sequence01` tool session `94116`, completion `d4d54c`, returned 0. Profile controls passed one positive and seven negative cases; 27 recorded method/source inputs remained unchanged.

`run-basic-checks.py --scope full` ran exactly eight new tops / 29 subtests plus `TestAgentStopWaitsForOriginalCall` and `TestAgentUnknownConfirmationOwnedUntilActualReturn`, which have no subtests: **10 tops / 29 subtests**, one race-test package and one vet package, both `./internal/central/agent`. Race Go PID 1062873 actually returned 0 in 23.589 s; vet PID 1063280 returned 0 in 15.509 s. All six maps of 285 inputs matched.

The recorder retains the accepted Command basic closure/Wait algorithm, fixed offline Go, max1/race/count1/p1 and original timeout. Actual race/plain `go list -deps -test -json` closures bind compiled sources, same-package tests, module and embedded files, the recorder and Go binary. Package/selectors and required sources are the bounded changes. Actual JSON selection and subtest counts, rather than declarations alone, support the result.

PG compile Go PID 1063608 actually returned 0 in 127.272 s; list PID 1065514 returned 0 in 1.056 s with the sole top `TestAgentProjectLocalStop`. The three compile/list maps of 2021 inputs matched; runtime added only the binary, giving 2022 inputs. Binary identity: SHA-256 `661ed81e1740d8b2f2993d82d0d627724a5747706eb6f0ae5bcfdfad3afe2638`, 91,081,460 bytes. Complete same-package sources and embedded migrations through 72 were bound; this slice added no migration.

## Capacity hold and fresh preparation

`native01` was **prepared only**. Its 2,785,267,712-byte capacity snapshot caused a hold before any native run; it was not a failed Agent test or native round.

Root then authorized an exact cache exception: unlink 145 inventoried, regenerable old Go package `-d` files, allocated size 2,163,302,400 bytes. Inventory `e729f2` and removal `f82161` each actually returned 0. The protected Agent 2022 inputs, Model 655 / 22 input sets (combined union 2680), 49 records and four identities matched before/after. This was a bounded removal, with no whole-cache sweep, no inference of non-use from mtime, and no claim that removed cache objects were permanently preserved.

`prepare02` used fresh namespace `/tmp/apls02`. Original tool `17869/6dfef3` returned 0; outer PID 1073524 and preparation child PID 1073549 returned 0, child elapsed 1.177 s. The 4,726,820,864 available bytes and 1,704,318 available inodes were a preparation snapshot, not runtime peak evidence. The previously compiled binary and protected inputs were reused.

## First actual native round

**native02 PASS** is the sole actual native round. Original tool session `5391`, completion `a0d6c9`, returned 0. Supervisor PID 1075262 actually returned 0 in 108.910 s (raw supervisor elapsed 107.793 s); Driver PID 1075263 and Go PID 1076914 each recorded actual Wait 0. The exact selector `^TestAgentProjectLocalStop$` ran **1 top / 2 subtests** in 17.94 s:

| Subtest | Actual duration |
| --- | --- |
| `archive-original-mutation-and-project-isolation` | 9.67 s |
| `delete-directory-and-original-return` | 8.27 s |

The real LifecycleStopDriver advanced accepted to stopping and called the actual same-Store Agent Service/DirectoryReader provider through the real LifecycleAuthority. Both physical COMMIT/Store-return barriers preceded cancellation. Business barriers held actual BEGIN transactions with PG backend identities; pending fell only after original calls returned. Archive selected target mutations while preserving reads and foreign Project work; Delete also selected both owners' target reads. Existing Agent, reference and event facts remained, and participant rows stayed required.

All seven exact resources had 14 absence observations. Descendant, private and runtime double-empty tails and automatic HOST_TCP double delta-empty completed; exact metadata checks passed. The final post-check matched all 2022 runtime input hashes and four identities, with tracked HEAD clean and no race report. The original 6-minute Go and 540+60-second driver budgets, seven-resource supervisor and 75-second TCP tail remained unchanged. The thin recorder stayed byte-identical at SHA-256 `56a32b0d45a633c3c85306d098730318327c02f4ee9c5267c8d712e9880d4f20`; no filtering, manual baseline or prewarm was added.

Original records remain under `output/ai/agent-project-local-stop/`: `sequence01/`, `basic01/`, `compile01/`, prepared-only `native01/`, and `native02/`. The [native terminal](../../output/ai/agent-project-local-stop/native02/terminal.json), [tool observation](../../output/ai/agent-project-local-stop/native02/tool-observation.json) and [post-check](../../output/ai/agent-project-local-stop/native02/post-check.json) retain the original result; `/tmp/apls02/pg-211993be9591401dbeb3e6ef0adeba9f.log` is the raw PG log. These are original records, not instructions to rerun or overwrite a used namespace.

## Remaining boundaries

The initial methods checkpoint attempt failed only its static EOF check because basic had one extra LF; that failure was retained and the single byte corrected before the tested checkpoint. A separate Model worktree test failure remains its own result. Neither changes the Agent behavior result or turns the prepared-only capacity hold into a native failure.

`LocalJoined` describes only the two bound local instances. Driver claim terminal and a local report do not prove foreign-process retirement or completion of the aggregate Agent/Skills/Variables participant. Durable call/process ownership, reference retirement and ordered cleanup remain separate work. This slice does not enable default Project creation, production app/initializer binding, a complete LifecycleRegistry or platform readiness, and does not change the existing Model/runtime STOP boundary.
