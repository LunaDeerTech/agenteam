# 全局协调索引

2026-10-11。main 的本文件由 root 指定 variables_review 唯一维护正文；root 负责 Git、集成和资源排期。功能分支 current 不覆盖本索引。正式交付边界见[任务台账](../docs/development/agent-team/tasks.md)，分段接受不等于当前 main 单次全量通过。

## 当前开发

| 任务 / 分支 | 负责人 | 状态与下一步 |
| --- | --- | --- |
| Project Usage 只读 UI — `ai/project-usage-read-ui-20261011` | agent_display_settings 实施，recovery_lead 独审 | native01 原 FAIL 已完整退出并保留，正在定向修复 Host 观测方法；后继输入和窗口须重新准入，不提前计浏览器交付。 |
| Knowledge Move UI — `ai/knowledge-move-ui-20261011` | skills_milestone_review 实施，skill_package_review 独审 | Move05 补核与独审通过，排下一窗口；启动时仍须满足 ≥5GiB。原 Move04 whole FAIL 和全部旧 FAIL 保留，不改固定输入、预算或退出门。 |
| Agent Runner 配置 — `ai/agent-runner-mounts-20261011` | runtime_recovery 及指定子作者实施，recovery_lead 提供契约 | 父/子作者已实际启动。系统管理 Runner；项目沿既有 Agent 配置权限，在创建/编辑 Agent 时选 Runner 并填写挂载目录 name。无额外系统 Admin 门、无独立 Mount 创建 UI；当前授权和生产执行接线仍须本片验证。 |

## 保留与清理

- SkillHTTP 树保留活跃热 Go cache，SkillsMilestoneResume 树保留共享只读 modules 与既有运行依赖；Go writer 和 native 窗口均按 root 明确交接。
- ProjectVariablesResume 保留未完恢复；ObjectSafe 保留原件与失败。Move、Usage、AgentRunner 活跃树及 Secrets 当前依赖阶段保留；未核其他树不自动清理。
- Find、Sprint、Title、AgentDisplay、TaskHistoryFilters、SecretAudit 六组 tree/local/remote 已由 root 实际确认全部清理。完整原件迁入 main `output/ai/` 各任务目录，依赖与日志映射见各自保全说明。
- Runner Workspace 与 Outbox 已正式交付 main，移出活跃开发。Runner 原 tree 已普通删除，local/remote 待 root 核删；Outbox 原件已迁移、tree 已普通删除，refs 须等本次恢复补丁远端保存后再清。

跨设备必要差额见 [Sprint](sprint-edit-ui/README.md)、[Title](knowledge-title-search-ui/README.md)、[TaskHistory](task-history-filters/README.md)、[SecretAudit](secret-audit-stop-adapters/README.md)、[Outbox](outbox-stop-adapter/README.md) 的历史 main 基准与最小补丁，均已实际应用并逐字核原输入。Outbox 补丁仅恢复已复用的历史 PG 输入，当前安全修后代码从 main 恢复；AgentDisplay、Find、Runner 无需额外源码差额。原 FAIL、绝对路径和验收范围不改写，不复制整树、日志或依赖入 Git。

完整 Project initializer、ready、生产 participant/Control 绑定及既有 STOP 按正式记录保持；本索引不批准恢复旧网络故障 probe 或其他停项。
