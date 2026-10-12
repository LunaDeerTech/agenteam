# Agent Project call App validation

The [App work item](../../docs/development/work-items/d08-agent-project-call-app.md) has passed its selected controlled basic check, profile controls, compile/list and first real native run. Basic used `763576b88b4ed6f819b3a067ac8007c7bfd3352f`; after merging the accepted upstream fixture correction, compile/native used `7e202fcb1c471c13f812e6880500e2d7bff3345b`. The original basic inputs were verified unchanged and reused. The shared profile baseline remains `d64a8f8cc03167b4582a9ea64aa277dfa48da9ac`; existing Agent ownership and Model reservation profiles remain intact. This finite App result is ready for integration; merge, push, preservation and tree cleanup are not claimed here.

## Minimal basic scope

The only selected package is `./internal/central/app`, with race instrumentation and the same package's vet. Its five new top tests contain six subtests:

- `TestAgentProjectCallAppConstructionKeepsExplicitOwnership` (four subtests)
- `TestAgentProjectCallAppBothOriginalEntriesBlockProviderForce`
- `TestAgentProjectCallAppRegistryUnknownWaitsForOriginalReturn`
- `TestAgentProjectCallAppUnknownKeepsOriginalOwnerAfterStop` (two subtests)
- `TestAgentProjectCallAppLateInstallRetainsOriginalBudget`

These [App controls](../../internal/central/app/agent_project_call_ownership_test.go) use controlled Store and provider boundaries. They check one owned pair, actual local return/Unknown retention, provider ordering, and the original budget. They do not establish PostgreSQL commit, process death, or HTTP results. No existing Agent or migration-loader test matrix is selected again.

The [basic wrapper](run-basic-checks.py) preserves offline Go 1.27.1, max1/race/count1/p1, actual race/plain dependency discovery, embedded inputs, exact JSON selection and original Popen/Wait. Twelve required source paths anchor the changed assembly, real ownership dependency and existing migration embeds; the actual compiler closure determines the complete input set. This slice adds no migration.

## Recorded basic result

The original basic method PID `1169230` returned Wait `0` in `303.568s`; its race test PID `1169361` returned `0` in `164.018s`, and App vet PID `1171621` returned `0` in `136.935s`. Both original dependency-discovery processes also returned `0`. The [exact selection](../../output/ai/agent-project-call-app/basic01/unit01-selection.json) and [post-check](../../output/ai/agent-project-call-app/basic01-outer/post-check.json) confirm five top tests and six subtests, each RUN/PASS exactly once, with no FAIL, SKIP or race warning. All six maps contain the same 1,133 compiler/runtime inputs; the actual post-run hashes match, all 16 outer inputs match, and the tested HEAD was clean. This proves the selected App controls and one App vet only; `tests/projectvariable` and `tests/testsupport/agentconfiguration` had no intersection with that compiler closure.

The original [terminal](../../output/ai/agent-project-call-app/basic01-outer/terminal.json) records that no native run started. Tool session `96017`, final chunk `5b813a`, returned `0`; [tool-observation](../../output/ai/agent-project-call-app/basic01-outer/tool-observation.json) is an assistant-observed transcription, not a provider-native export. The post-check completed with `0` (`4cdc39`). These are the original results, not a rerun or expanded selection.

Upstream Agent ownership native02 and native04 remain separate recorded failures; native03 was a capacity hold without native execution. App native used the merged full Skill/Object fixture correction. This App slice had no failed basic/compile/native run or retry; its success neither rewrites those upstream records nor establishes their separate foreign-process scope.

## Recorded real PostgreSQL and App profile

The exact selector is `^TestAgentProjectCallApp$`, one top with two subtests in the independently owned [PG file](../../tests/projectvariable/agent_project_call_app_test.go):

| Subtest | Required real boundary |
| --- | --- |
| `default-http-shared-call-owner` | After every setup owner and its guard actually stop, public `app.Run` serves a real TLS bridge with Session, CSRF and Owner authorization. Create, configuration Get/Lookup, Runner Mount Get and directory Get/List use one process and one owned-pair instance, with distinct call IDs and joined records. Rejected calls add no registration. |
| `directory-return-checkpoint-drains-before-guard` | A real directory call finishes its business read but its original Project EX retirement waits behind an independently held Project SH. One bounded `pg_cancel_backend` targets the exact owned backend PID, transaction start, lock tuple, blocker and application-name statement within the unchanged HTTP/finish budgets. Actual rollback yields HTTP 200 while the original call remains running. SIGTERM starts retirement of that same call; its real lock wait retains the flock guard. Releasing the holder within the original graceful budget permits actual retirement, `app.Run` return and guard release. |

The setup and the injected PostgreSQL cancellation remain explicit fixture boundaries. This profile does not claim a StopDriver chain, foreign-process death, physical COMMIT-Unknown injection or Force behavior in PG; those are separate upstream or controlled-test scopes. It does not complete a lifecycle participant, retire references, establish mixed-version global joining, or enable the stopped Model HTTP path. If the original App or guard has not actually released, failure cleanup first records a safe failure and keeps the process and fixture alive, waiting only for the original release or the unchanged Go 6-minute / outer 540+60-second hard budget; this is not successful local bounded Drain/Join and neither refreshes a budget nor retries. A normal PASS still requires the original app.Run to return nil, the original migration-74 calls to be joined, guard release and every original resource tail gate. A deadline or cancellation is not evidence of Joined.

## Recorded compile and native result

The [sequence terminal](../../output/ai/agent-project-call-app/sequence01/terminal.json) and [post-check](../../output/ai/agent-project-call-app/sequence01/post-check.json) record original tool `94694` / final `baf93c` actual `0`, outer `1191601`, `39.506s`. Profile controls `1191648` passed one positive and seven negative cases. Compile Go `1191716` returned `0` in `34.602s`; exact sole-list `1192260` returned `0` in `1.053s`; prepare `1192329` returned `0` in `1.021s` without starting native. All 2,052 compile inputs agree before/after/list, including 96 same-package Go files and every migration through 74; the 38 sequence inputs agree. Runtime adds the actual 91,949,919-byte binary, SHA-256 `97f6c8a934034c2f72f9e93754ec3531a92eb7bf71cbf5fc9d30ee6c4f8918ac`, for 2,053 inputs. Preparation and startup capacity values are historical snapshots, not a peak-memory or capacity guarantee.

The first native original tool `69540` / final `731bb9` returned `0`. The [terminal](../../output/ai/agent-project-call-app/native01/terminal.json) records supervisor `1192901` Wait `0` in `117.740s`; driver `1192921` and Go `1194521` also actually returned `0`. The exact top and two subtests passed in `24.32s` (`12.99s` / `11.33s`). The [native post-check](../../output/ai/agent-project-call-app/native01/post-check.json) confirms six distinct joined public App calls on the same pair, zero registration for the denied request, and the original directory-call retirement/guard ordering. The original raw log is `/tmp/apca01/pg-c866eada816341688c9a09da88b683b3.log`, with `owned.json` and `request.json` in its same-name directory without `.log`.

All seven exact resources have two absence observations (14 total); descendants, private directories and runtime have both empty tails, and both automatic host TCP deltas are empty. All 2,053 runtime hashes and the [four recorder identities](../../output/ai/agent-project-call-app/native01/recorder-identity.json) remain equal; tested HEAD was clean, with no race warning, source change or retry. Post-check `abf250` returned `0`. The [native tool observation](../../output/ai/agent-project-call-app/native01/tool-observation.json), like the sequence and basic observations, is an assistant-observed transcription of the actual tool completion, not a provider-native export. Later document edits do not retroactively claim that historical outer document hashes are still current.

## Commands and unchanged resource method

The commands below describe the completed sequence. Do not reuse these completed output directories or rerun them without a new assigned window; any successor must have fresh output and explicit Go/cache/native allocation:

```sh
python3 -B .agent-state/agent-project-call-app/profile-controls.py
python3 -B .agent-state/agent-project-call-app/run-basic-checks.py --output output/ai/agent-project-call-app/basic01
python3 -B .agent-state/agent-project-call-app/run-pg-checks.py compile --output output/ai/agent-project-call-app/compile01
python3 -B .agent-state/agent-project-call-app/run-pg-checks.py prepare --compile output/ai/agent-project-call-app/compile01 --output output/ai/agent-project-call-app/native01 --namespace /tmp/apca01
python3 -B output/ai/agent-project-call-app/native01/record-native.py
```

The [profile controls](profile-controls.py) passed one positive and seven negative cases. Removing only the App key restores both complete shared harness ASTs and effective maps at the fixed base. [Run-pg](run-pg-checks.py) retains the original Agent ownership compile/list/prepare algorithm: all same-package PG Go files, shared support, conservative full-internal sources/assets, the original COMMIT proxy and every embedded migration are bound, explicitly including existing 73 and 74. The 28 profile-required paths are anchors, not a reduced closure. Runtime adds only the compiled binary; actual source, binary and four request identities must remain equal.

Preparation consumes successful compile/list records and emits a fresh request, input map and capacity snapshot; it starts no Go or service. Native execution needs its own resource window. The [thin recorder](record-native.py) is byte-identical to the accepted original, SHA-256 `56a32b0d45a633c3c85306d098730318327c02f4ee9c5267c8d712e9880d4f20`, and records the original supervisor Wait.

The original seven-resource supervisor retains the 6-minute test limit, 540+60-second driver budgets, exact IDs, descendant/private/runtime tails and automatic HOST_TCP double delta-empty within 75 seconds. A nonzero result, input drift or incomplete tail stops the chain and preserves the failure. There is no TCP filtering, manual baseline, hidden retry, resource-gate relaxation or output reuse. Actual closure and runtime bindings use this tree and the existing protected tool/cache infrastructure, not a removed Model worktree.
