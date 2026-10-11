# 全局协调索引

2026-10-11。main 的本文件由 root 指定 variables_review 唯一维护正文；root 负责 Git、集成和资源排期。功能分支 current 不覆盖本索引。正式交付边界见[任务台账](../docs/development/agent-team/tasks.md)，分段接受不等于当前 main 单次全量通过。

## 当前开发

| 任务 / 分支 | 负责人 | 状态与下一步 |
| --- | --- | --- |
| Knowledge Move UI — `ai/knowledge-move-ui-20261011` | skills_milestone_review 实施，skill_package_review 独审 | Move05 业务与视觉有限通过，whole 因 TCP 尾 FAIL 已停；原 FAIL 和完整退出门保留，未形成正式交付。 |
| Agent Runner 配置 — 后端与独立 UI 分支 | runtime_recovery / variables_review 实施，指定作者编写 Go/PW 方法 | 后端 PG02 已正式交付并推送 main（d319a041）；[UI](../docs/development/work-items/d10-agent-runner-mounts-ui.md)已完成有限验收，主线集成待本次提交。产品2ff3cddd、34项受影响控制与最终type/build通过，native02真实runtime-off默认App两子例、五图及完整退出尾独验接受。普通Owner可选择系统Runner与资源目录名创建、编辑、清空；offline/incompatible均可配置，无额外Admin门。原编译、PG01及UI首焦点FAIL保留，不外推物理目录、executor或完整D10。 |
| Execution Mount Capture — `ai/execution-mount-capture-20261011` | runtime_recovery 主实施，Mount / Runner 子作者分域 | 独立树 `/workspace/agenteam-execution-mount-capture`，当前806e0a68，源码WIP未验收。范围止于真实PreparationDriver的非空preparing input与不可变Mount material/ref；不开放Snapshot/running、物理目录或Runner执行。 |

Project Usage 只读 UI 已正式交付并推送 main（066bf），native03 有限业务、视觉及完整退出尾独验接受；原 native01/02 FAIL 保留，不外推完整 D09。

## 保留与清理

- SkillHTTP 树保留活跃热 Go cache，SkillsMilestoneResume 树保留共享只读 modules 与既有运行依赖；Go writer 和 native 窗口均按 root 明确交接。
- ProjectVariablesResume 保留未完恢复；ObjectSafe 保留原件与失败。Move、ExecutionMountCapture 活跃树保留；AgentRunner 后端/UI 与 Usage 原树及原件等待独立清理核验，未核其他树不自动清理。
- Find、Sprint、Title、AgentDisplay、TaskHistoryFilters、SecretAudit、Runner Workspace、Outbox、Secrets、KnowledgeTextUI、Milestone、KnowledgeHTTP、SchedulerHTTP、SchedulerUI、SkillUI 十五组 tree/local/remote 已全部清理，root 已确认远端删除、ls-remote 空与 fetch-prune 实际成功。完整原件迁入 main `output/ai/` 各任务目录，依赖与日志映射见各自保全说明。另 25 个严格 main 祖先旧远端引用已按逐 OID lease 原子删除，精确查询为空、fetch/prune 成功；本地 work296 已普通删除。合计清理 40 个远端引用、16 个本地分支和 15 个 WorkTree；额外 25 个引用没有对应本地分支或树。154 个未证明包含于 main 的引用、1 个显式保留历史引用与 8 个活动分支均未动。

跨设备必要差额见 [Sprint](sprint-edit-ui/README.md)、[Title](knowledge-title-search-ui/README.md)、[TaskHistory](task-history-filters/README.md)、[SecretAudit](secret-audit-stop-adapters/README.md)、[Outbox](outbox-stop-adapter/README.md) 的历史 main 基准与最小补丁，均已实际应用并逐字核原输入。Outbox 补丁仅恢复已复用的历史 PG 输入，当前安全修后代码从 main 恢复；AgentDisplay、Find、Runner 无需额外源码差额。原 FAIL、绝对路径和验收范围不改写，不复制整树、日志或依赖入 Git。

完整 Project initializer、ready、生产 participant/Control 绑定及既有 STOP 按正式记录保持；本索引不批准恢复旧网络故障 probe 或其他停项。
