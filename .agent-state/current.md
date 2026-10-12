# 全局协调索引

2026-10-11。Execution RoundInput（68）已随完整开发历史 main `bf7c882a` 有限交付并推送；recovery_lead 维护本次保全记录，root 负责 Git、清理和资源排期。正式交付边界见[任务台账](../docs/development/agent-team/tasks.md)与[68 本卡](../docs/development/work-items/d22-execution-round-input.md)。main `53ac76fe` 的全局保全、清理和保护记录保持；68 原件保全及树/本地分支清理已实际完成并计入下列累计，分段接受不等于 main 单次全量通过。

## 当前开发

| 任务 / 分支 | 负责人 | 状态与下一步 |
| --- | --- | --- |
| NextRoundProjection 独立树 | root 统筹分域实施；variables_review 编译/运行 | basic01 的 8 top / 42 sub 与 3 包 vet 已获独立接受；native02 原工具 73449 实际 1，安全诊断明确 ENOSPC，仍在目录前置阶段失败、未进入投影业务，全部 Wait/资源/TCP/输入尾已闭合。当前容量 hold，未自动重试；native01 原首因未定及两轮 FAIL 保留，不计真实 PG 通过或下一 Model 已执行。 |
| Knowledge Move UI — `ai/knowledge-move-ui-20261011` | skills_milestone_review 实施，skill_package_review 独审 | Move05 业务与视觉有限通过，whole 因 TCP 尾 FAIL 已停；原 FAIL 和完整退出门保留，未形成正式交付。 |

主目标仍是完成[开发计划](../docs/development/development-plan.md)中的D01–D28/E01，目前尚未完成；任何单片有限验收不代表整体目标或原STOP解除。

## 保留与清理

- Runner write-file WSS 已随完整历史 main `570da7a2` 有限交付并推送；保全说明 `58a1b1e1` 原推送68131实际0。basic01原31350的12top37sub（10新+2旧）/3vet、compile01原18090与native01原96046有限wholePASS已唯一主审接受；Go812704/supervisor812703原Wait0，所有实际资源、desc/private、TCP双差额空与1481输入尾齐。80文件/22,975,545B及7目录已无覆盖同FS保全至 `output/ai/retired-20261011/runner-write-file-wss`，5份tmp逐字副本齐、原tmp保留；63个仓库输入和静态Close修前完整历史main可恢复，见[恢复说明](runner-write-file-wss/README.md)。root核定最终tip `25840576` 为main祖先后，普通worktree remove及branch -d均实际0（chunk `095069`）；fresh ls-remote `f5f55c`实际0且空，此分支从未推送，仅增加树与本地分支计数。范围仍仅trustedTLS/WSS boundSession→显式Handler→ManagerWrite/readback/Conflict及原退出，不含Central授权/Operation/enrollment/完整或默认Client.Run/生产接线，peer.Close为静态发现而非动态FAIL。

- Execution RoundInput（68）已随完整历史 main `bf7c882a` 有限交付并推送；保全说明 `c34ba3b5` 原推送 50299 实际 0。基础 9 top / 52 sub 分段及 4 包 vet、compile01 与 native01 有限 wholePASS 保持，basic01 原 FAIL 完整保留。81 个 output 文件及三份 tmp 副本已无覆盖保全至 `output/ai/retired-20261011/execution-round-input`，详见[本卡](../docs/development/work-items/d22-execution-round-input.md)及[恢复说明](execution-round-input/README.md)。root 确认最终 `f1fe5484` 为 main 祖先后，普通 worktree remove 与 branch -d 均实际 0（工具 chunk `9dc6b3`）；fresh 远端查询 37217 实际 0 且为空，分支从未推送，故只增加树与本地分支计数。交付止于一次非终态后继 RoundInput，Determine 新增 Model/dispatch 为 0，下一 Model、production 与 tools HTTP 等 STOP 保持。

- Skill Assignment ingress 已随完整历史 main `5c4bdca9` 有限交付，保全说明 `ad023b69` 推送工具 14649 实际 0；native02 原91341 wholePASS 已唯一主审接受，原 basic01、compile01、native01 FAIL 全部保留。131 个完整 output 文件及六份 tmp 副本已保全至 `output/ai/retired-20261011/skill-assignment-ingress`，原 tmp 保留，详见[本卡](../docs/development/work-items/d10-skill-assignment-ingress.md)及[恢复说明](skill-assignment-ingress/README.md)。root 已实际完成普通 worktree remove 和 branch -d，均退出 0；远端首次 exact-lease 删除工具 49505 因无法读取 Git Username 实际 1，fresh 确认原 ref 未变后，同一 exact-lease 后继工具 2276 实际 0，fresh 查询为空、fetch/prune 0。三项已计入下列清理数。范围仍只到真实 Owner Assign/Remove、四 owner 与 typed Audit/Outbox/Ingress 同Tx、历史/回滚/Committed ack-loss 只 Lookup，created / Model0、显式图与 Project test Skills；不含 Preparing 全链、68、下一 Model 或默认绑定。

- Runner 本地 write-file 已随完整历史 main `66fe3ab0` 有限交付并推送；仅可信 Root/Handle 原子 Write→Read，不含 Tool/WSS/production。原 basic01 wholeFAIL 与旧 31 top PASS、修后新 10 top / 13 sub race 及两包 vet PASS 均保留。59 个原件 / 3,665,603 B 已无覆盖同文件系统迁至 `output/ai/retired-20261011/runner-write-file-adapter`，路径、SHA、size、dev、inode、mode 逐项相同；33 个仓库内历史输入每轮均与 main 可达提交逐字一致，外部 1,067 项仍指原工具链/modules。详见[恢复说明](runner-write-file-adapter/README.md)。root 已实际完成普通 worktree remove 和普通 branch -d，均退出 0、未强制删除；原 HEAD 6ce32282 为 main 祖先。fresh 远端精确查询成功且为空，本任务分支从未推送，故仅增加树和本地分支计数，不增加远端删除数。

- read-file 结果单次消费已随 main `c84a1257` 有限交付，`5905df69` 正式推送保完整 `f20f52b1` 开发历史与恢复说明；native01 wholePASS 已独立接受。原 75 个 output 文件、三份 tmp 必需副本及原基本 FAIL 均已无覆盖保全，见[恢复说明](execution-tool-result-consumption/README.md)。原树普通移除、本地 branch -d 实际成功；远端 fresh `0bf996ab` 精确 lease 删除工具 37215 实际 0，再 fresh 查询为空、prune 0，三项计入下列总数。交付只到一次非终态 seq3 消费，不含下一 Model、终态退休或默认生产绑定。

- read-file Operation 原 output 已同文件系统无覆盖迁至 main `output/ai/retired-20261011/runner-read-file-operation`：136 个文件、350,560,682 字节的路径 / SHA / size / device / inode / mode 全部保持，四套 compile / native 和 codec 原红均在；四个原 `/tmp` 的 16 个小文件另存逐字副本，原 `/tmp` 未删。23 个开发提交及 1,830 个核对过的历史 blob 已在 main 可达，未见源码恢复缺口；当前作者和其它树核对未发现旧绝对运行路径依赖，详见[恢复说明](runner-read-file-operation/README.md)。Operation 已正式推送并实际完成普通 worktree remove、本地分支删除；远端原 tip 精确 lease 删除工具 99189 实际 0，fresh 查询为空、prune 完成，三项均计入下列累计。native01 TIME_WAIT 归属未明、native02 未记录 Status、native03 未执行 Lookup / 正常收尾均不由 04 通过回填。

- Execution ToolCall 持久事实已随 main `95c092bc` 有限交付并推送，native03 wholePASS 独立接受，原全部 FAIL 保留；完整开发历史在 main，126 个 output 原件及三轮 tmp 必需副本已无覆盖保全，见[恢复说明](execution-tool-call-facts/README.md)。原树普通移除、本地 branch -d 成功；远端精确 tip lease 删除实际 0，fresh 查询为空、explicit prune 0、旧树不在列表，三项清理已计入下列总数。不含真实 Model tools HTTP、ToolOperation / Runner 派发或默认生产绑定。

- Project Secret 写 UI 已随 main `22d0a0b6` 有限交付：普通 Owner 创建、元数据修改、明确替换与原身份 Lookup 恢复，native02 wholePASS 独立接受；原 FAIL 保留。完整证据已同文件系统无覆盖保全至 `output/ai/retired-20261011/project-secrets-write-ui/`，含组合 Knowledge 控制、dist 与两轮 tmp 原件副本；原树已普通移除，本地分支在 squash 非祖先拒绝后窄删；精确 tip lease 远端删除、成功空查询及 prune 均已核实，三项清理已计入下列总数。删除、运行注入、生产 Project initializer 与原 STOP 不在本片。
- Knowledge 子树删除 UI 已于 main `48fe9a37` 有限交付，native03 wholePASS 独立接受；原树、本地分支及远端引用均已实际清理并核为空。两输出根、dist、三轮 `/tmp` 原件及必要恢复差额已保全；原01/02 FAIL、02 Lookup502 未定因和 cleanup_pending 不代表物理 Object 回收的边界保持。
- 系统 Runner 管理 UI（main `f86f4215`）、Agent Secret 白名单 UI（`e263fc99`）、Runner read-file（`65fc8687`）均已正式交付并推送；三棵原树、本地分支与远端引用已全部清理，必要原件与失败输入已保全。
- SkillHTTP 树保留活跃热 Go cache，SkillsMilestoneResume 树保留共享只读 modules 与既有运行依赖；Go writer 和 native 窗口均按 root 明确交接。
- ProjectVariablesResume 已随 main `3e28f10a` 有限交付；`66d45ce1` 仅保留完整开发历史、文件树零变化。全部原 output 和三份 tmp 必需副本已无覆盖保全，119 个已提交历史版本 main 可达；早期未提交失败输入缺口如实保留，见[恢复说明](project-variables-resume/README.md)。原树已普通移除，本地 branch -d 实际成功；远端原 tip 精确 lease 删除工具 4661 实际 0，fresh 查询为空、prune 0，三项清理已计入下列总数。
- ObjectSafe 保留原件与失败。Move 活跃树保留，未核其他树不自动清理。
- Runner directory 已随 main `4e5e70ce` 有限交付并推送，28 个相关 race top、六包 vet 与真实 PG/WSS 整轮独立接受；原树、本地分支及远端引用均已清理。完整 output 已同文件系统无覆盖迁至 main `output/ai/retired-20261011/runner-directory-adapter/`，其中 `retention/` 保存原 `/tmp/rda01` log/owned/request 的逐字副本和路径映射；原 `/tmp` 保留。本片无失败输入需补恢复差额，上游严格授权替身、生产默认 nil executor/旧 STOP 保留。
- 已完成清理共 34 个 WorkTree、35 个本地分支、64 个远端引用；其中 27 个额外远端引用为严格 main 祖先，没有对应本地分支或树，另有已删除的本地 work296。root 已确认前述三棵退役树不存在、对应 local/tracking 为空，精确 lease 删除、ls-remote 空结果及 main+ai prune 均实际成功。Directory 普通 worktree remove 成功，branch -d 因非祖先拒绝后仅对已交 squash 分支执行窄 -D；最新 tip 精确 lease 删除、成功空查询、prune 和 local/tracking 空结果均已闭合。另有 D16 filesystem、D18 NameProjection、SearchBenchmark 三个完整 scope 已交且无独占依赖的旧远端引用，经 fresh tip 核对、精确 lease 删除、成功空查询及 prune 后完成清理，没有新增本地分支或树。新增清理 `ai/work-review-runtime-delivery`：tip `f4190ad7` 全部历史经 main 可达，无本地分支、worktree 或活动依赖；root fresh tip 核对后精确 lease 删除成功，ls-remote 成功为空，prune 后远端跟踪引用为空，无需新增恢复材料。原 154 个待核引用现剩 146 个，1 个显式保留历史引用仍未动，其他活动分支按 owner 保留。`ai/skills-milestone-integration` 的全部历史已为 main 祖先、无 local/tree 或活动依赖；root 核定 tip `fc0fb130` 后精确 lease 删除工具 74385 实际 0，fresh 查询为空、prune 完成，已计入上述远端累计。


- 两条无 local/tree 或活动依赖的旧 remote 已完成 fresh tip 核对、双 exact-lease 原子删除、成功空查询及 prune：`ai/task-timeline-http`（`1e6b3db2809ce0e62de6c8c2042a0eaf3b9b67f5`）完整 6 路径及 6 个提交版本逐字保留于 main 祖先 `bfebd676`；`ai/agent-directory-http`（`235887d2f615ed6bd6863d9c8b4a1b1e548f32d2`）完整 14 路径等于 main 祖先 `2c59cf0d`，跨三提交的 15 个文件版本均可从 main 历史恢复，无需新增材料。
- `ai/skills-catalog-headers-fix` 已实际清理：fresh 远端 tip 为 `403db88709f8689a001413e9586e94ec21c30037`（原查询 `9a6474` 实际 0），exact-lease 删除工具 29092 实际 0；随后 fresh 查询成功为空、main/ai fetch/prune 及 local/tracking 空核验由原工具 83899 实际 0 闭合，仅增加远端计数。该分支无 local/tree 或活动 owner/ref 依赖，只有一个 helper 提交；与 main 祖先 `c30d2e495841ec6f667912017a4b52f41b1a6ffd` 同父 `b2e244fe`、整个 tree 同 `7b17aea667c8664e60248d12f780b625bb5f594e`，唯一文件修改前 blob `ae30df57` 与修改后 `2310a2f7` 均 main 可达，全部历史源码可恢复，不需新增归档。另候选 `ai/sprint-create-focus-fix`（`5ed36a9f`）实际涉及 10 个独有提交、30 个文件及旧浏览器材料，超出本次小范围审计且未证明完整历史恢复，继续保留待核；不计作已清理或新增原显式 keep1。
- `ai/secret-variables-owner`（`866a3fed`）继续保留：最终合同及独立控制已在 main，但 7 个历史源码版本尚未证明可从 main 或保全材料恢复，涉及 `7c2fb954` 的原名称 Schema/decoder 失败输入；不因最终源码相同而删除历史引用。

以下三条旧远端引用已由 skill_package_review 完成有界只读核验，状态均为**候选核验通过、待 root 新鲜 tip 核对及远端删除**，尚未执行清理。累计仍为 34 个 WorkTree / 35 个本地分支 / 64 个远端引用，146 个待核引用及 keep1 不变。基于本地 main `b6f158c3` 的 1,000 个历史 commit/tree 与 151 条现有 `origin/ai/*` 元数据对照（包括活动和保留引用），发现 9 个符合排除条件的整树等价项；只深查下列三条，其余六条不视为已准清。

| 待清远端引用 | 精确 tip | 整树 hash | main 可达的整树等价提交 | 完整历史恢复范围 |
| --- | --- | --- | --- | --- |
| `ai/project-runtime-audit-composition` | `c43c88ad6e4d5145d9d99017ef9bc320af717123` | `b3aef4aa36f0b5b0020fe0d928322561c06ac392` | `a818837e943df13fad02caf9f117c8a415b3671a` | 唯一独有提交的 2 个新增文件版本：`internal/central/project/runtime_initialization_audit.go` 及同名 `_test.go`，逐项 blob 等于该 main 提交；两提交同父 `f4190ad7`。 |
| `ai/project-scheduler-discovery` | `5bd7e928b615a3d937bb834a669f18e507a6013b` | `85f928c66af191836d82ec2a7f58ef461afe6f26` | `74bd4d74b148a4cb0bc8435ed42712f12faa2186` | 独有 `1c0721f4` / `5bd7e928` 两提交的 4 个文件版本：`internal/central/project/contract/scheduler_projects.go`、`internal/central/project/scheduler_projects.go` 及各自 `_test.go`，全部逐项 blob 等于该 main 提交。 |
| `ai/task-retry-exhaustion` | `a6f491d7d8e33594a1aaf3ac8d94ee5c45e9266b` | `5de923b15554b4d4c1fdd5fb7a979bc1751f3661` | `a5b61d568aca4a96fb0e7f9b5fb8cd29c59f9ff4` | 独有 `4ddcae0f` / `a6f491d7` 两提交的 3 个文件版本：`internal/central/work/contract/task_launch_failure.go`、`task_launch_failure_reason_test.go` 及 `internal/central/work/task_retry_exhaustion_test.go`，全部逐项 blob 等于该 main 提交。 |

三条候选的原分叉基线及上述等价提交均为 main 祖先，祖先检查实际 0；最终 scope 与全部中间源码版本无恢复缺口，无需新增恢复 patch。当前 worktree/local 分支清单均无对应项；当前索引、方法文档和三条 tracked scope 未见旧 ref、tip 或绝对运行路径依赖。仅核了 `/workspace`、`/tmp`、main `output/ai` 及其 `retired-20261011` 的直接子目录，未发现三项同名旧树/output，未遍历全部输出。当前 Tool runtime 作者 recovery_lead、Projection/Execution/Loop 作者 variables_review 及 Mount/CentralRunner 作者 skill_package_review 分别确认本任务无这些旧 ref/tree/output/bin/cache 的运行依赖；main 正常源码继承不算旧引用依赖。本批只考虑删除远端 ref，不删除或迁移任何原输出、失败材料、工具链或保护缓存；旧保留项与其他活动分支不受影响。

跨设备必要差额与各自核对范围见 [Sprint](sprint-edit-ui/README.md)、[Title](knowledge-title-search-ui/README.md)、[TaskHistory](task-history-filters/README.md)、[SecretAudit](secret-audit-stop-adapters/README.md)、[Outbox](outbox-stop-adapter/README.md)、[AgentRunner 后端](agent-runner-mounts/README.md)、[AgentRunner UI](agent-runner-mounts-ui/README.md)、[ExecutionMountCapture](execution-mount-capture/README.md)、[Project Usage UI](project-usage-read-ui/README.md)、[ExecutionMountDirectText](execution-mount-directtext/README.md)、[SystemRunner UI](system-runner-management-ui/recovery/README.md)、[Agent Secret 白名单 UI](agent-secret-allowlist-ui/README.md)、[Runner read-file](runner-read-file-adapter/README.md)、[SearchBenchmark 原 v1](search-benchmark/README.md)、[Knowledge 子树删除 UI](knowledge-subtree-delete-ui/README.md)、[Project Secret 写 UI](project-secrets-write-ui/README.md)。近批完整原件位于 main `output/ai/retired-20261011/` 对应任务目录；read-file 的 `output/ai/runner-read-file-adapter/main-integration01.json` 另留原位。更早原件按各自保全说明定位。原 FAIL、绝对路径及验收边界保持；早期 UI source map、Usage 局部输入，以及 SystemRunner/Secret 未提交失败输入缺少原字节或逐项 SHA，以及 Search 原日志与误设夹具缺失的限制均不补造。AgentDisplay、Find、Runner Workspace 无需额外源码差额；不复制整树、日志或依赖入 Git。

完整 Project initializer、ready、生产 participant/Control 绑定及既有 STOP 按正式记录保持；本索引不批准恢复旧网络故障 probe 或其他停项。
