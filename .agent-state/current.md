# Skill assignment ingress 工作分支

2026-10-11。本树 `/workspace/agenteam-skill-assignment-ingress` 从 main `19df3fea` 建立，现合入正式 main `7c874adc`，分支 `ai/skill-assignment-ingress-20261011`。`skill_package_review` 负责本分支正文与 Skill/00067；Execution、Agent 与四引用域按[本卡](../docs/development/work-items/d10-skill-assignment-ingress.md)唯一写者并行。root 负责 Git、集成和资源排期。本片基础已分段通过，PG compile02/list 通过但 native01 wholeFAIL，Project producer 接线缺口待修复验证；下列 main 已交付、完整历史与清理索引原样保留。正式交付边界见[任务台账](../docs/development/agent-team/tasks.md)，分段接受不等于当前 main 单次全量通过。

## 当前开发

| 任务 / 分支 | 负责人 | 状态与下一步 |
| --- | --- | --- |
| Skill Assignment ingress — `ai/skill-assignment-ingress-20261011` | skill_package_review lead；recovery_lead Execution/运行；skill_assignment_agent Agent | 基础 33 top / 150 sub 分段与 11 包 vet 通过，原 basic01 FAIL 保留；profile01 一正七反通过。compile01 因 PG 两处类型接线 FAIL，`20fee0c4` 窄修后 compile02/list 实际 0；1868 输入跨轮仅 PG 文件变化。native01 原工具53013 wholeFAIL：两 sub 均 DEPENDENCY_UNBOUND，七资源/Wait/双尾/TCP/input门齐，窗口已释放。Project.Discover / ValidateInTx producer 闭集漏 Skill 已静态定位，待原作者修复和新验证，不记 assignment/回滚/Unknown 通过。详见[本卡](../docs/development/work-items/d10-skill-assignment-ingress.md)及[原件入口](skill-assignment-ingress/README.md)；不含 DetermineRoundInput、下一轮或 Transcript 应用。 |
| Knowledge Move UI — `ai/knowledge-move-ui-20261011` | skills_milestone_review 实施，skill_package_review 独审 | Move05 业务与视觉有限通过，whole 因 TCP 尾 FAIL 已停；原 FAIL 和完整退出门保留，未形成正式交付。 |

主目标仍是完成[开发计划](../docs/development/development-plan.md)中的D01–D28/E01，目前尚未完成；任何单片有限验收不代表整体目标或原STOP解除。

## 保留与清理

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
- 已完成清理共 30 个 WorkTree、31 个本地分支、62 个远端引用；其中 27 个额外远端引用为严格 main 祖先，没有对应本地分支或树，另有已删除的本地 work296。root 已确认前述三棵退役树不存在、对应 local/tracking 为空，精确 lease 删除、ls-remote 空结果及 main+ai prune 均实际成功。Directory 普通 worktree remove 成功，branch -d 因非祖先拒绝后仅对已交 squash 分支执行窄 -D；最新 tip 精确 lease 删除、成功空查询、prune 和 local/tracking 空结果均已闭合。另有 D16 filesystem、D18 NameProjection、SearchBenchmark 三个完整 scope 已交且无独占依赖的旧远端引用，经 fresh tip 核对、精确 lease 删除、成功空查询及 prune 后完成清理，没有新增本地分支或树。新增清理 `ai/work-review-runtime-delivery`：tip `f4190ad7` 全部历史经 main 可达，无本地分支、worktree 或活动依赖；root fresh tip 核对后精确 lease 删除成功，ls-remote 成功为空，prune 后远端跟踪引用为空，无需新增恢复材料。原 154 个待核引用现剩 147 个，1 个显式保留历史引用仍未动，其他活动分支按 owner 保留。`ai/skills-milestone-integration` 的全部历史已为 main 祖先、无 local/tree 或活动依赖；root 核定 tip `fc0fb130` 后精确 lease 删除工具 74385 实际 0，fresh 查询为空、prune 完成，已计入上述远端累计。


- 两条无 local/tree 或活动依赖的旧 remote 已完成 fresh tip 核对、双 exact-lease 原子删除、成功空查询及 prune：`ai/task-timeline-http`（`1e6b3db2809ce0e62de6c8c2042a0eaf3b9b67f5`）完整 6 路径及 6 个提交版本逐字保留于 main 祖先 `bfebd676`；`ai/agent-directory-http`（`235887d2f615ed6bd6863d9c8b4a1b1e548f32d2`）完整 14 路径等于 main 祖先 `2c59cf0d`，跨三提交的 15 个文件版本均可从 main 历史恢复，无需新增材料。
- `ai/secret-variables-owner`（`866a3fed`）继续保留：最终合同及独立控制已在 main，但 7 个历史源码版本尚未证明可从 main 或保全材料恢复，涉及 `7c2fb954` 的原名称 Schema/decoder 失败输入；不因最终源码相同而删除历史引用。

跨设备必要差额与各自核对范围见 [Sprint](sprint-edit-ui/README.md)、[Title](knowledge-title-search-ui/README.md)、[TaskHistory](task-history-filters/README.md)、[SecretAudit](secret-audit-stop-adapters/README.md)、[Outbox](outbox-stop-adapter/README.md)、[AgentRunner 后端](agent-runner-mounts/README.md)、[AgentRunner UI](agent-runner-mounts-ui/README.md)、[ExecutionMountCapture](execution-mount-capture/README.md)、[Project Usage UI](project-usage-read-ui/README.md)、[ExecutionMountDirectText](execution-mount-directtext/README.md)、[SystemRunner UI](system-runner-management-ui/recovery/README.md)、[Agent Secret 白名单 UI](agent-secret-allowlist-ui/README.md)、[Runner read-file](runner-read-file-adapter/README.md)、[SearchBenchmark 原 v1](search-benchmark/README.md)、[Knowledge 子树删除 UI](knowledge-subtree-delete-ui/README.md)、[Project Secret 写 UI](project-secrets-write-ui/README.md)。近批完整原件位于 main `output/ai/retired-20261011/` 对应任务目录；read-file 的 `output/ai/runner-read-file-adapter/main-integration01.json` 另留原位。更早原件按各自保全说明定位。原 FAIL、绝对路径及验收边界保持；早期 UI source map、Usage 局部输入，以及 SystemRunner/Secret 未提交失败输入缺少原字节或逐项 SHA，以及 Search 原日志与误设夹具缺失的限制均不补造。AgentDisplay、Find、Runner Workspace 无需额外源码差额；不复制整树、日志或依赖入 Git。

完整 Project initializer、ready、生产 participant/Control 绑定及既有 STOP 按正式记录保持；本索引不批准恢复旧网络故障 probe 或其他停项。
