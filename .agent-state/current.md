# 全局协调索引

2026-10-11。main 的本文件由 root 指定 variables_review 唯一维护正文；root 负责 Git、集成和资源排期。功能分支 current 不覆盖本索引。正式交付边界见[任务台账](../docs/development/agent-team/tasks.md)，分段接受不等于当前 main 单次全量通过。

## 当前开发

| 任务 / 分支 | 负责人 | 状态与下一步 |
| --- | --- | --- |
| Project Usage 只读 UI — `ai/project-usage-read-ui-20261011` | agent_display_settings 实施，recovery_lead 独审 | native01 原 FAIL 已完整退出并保留；Host 方法修复 f462 已接受，native02 已获独占窗口并做启动前检查，尚未计本轮通过。 |
| Knowledge Move UI — `ai/knowledge-move-ui-20261011` | skills_milestone_review 实施，skill_package_review 独审 | Move05 业务与视觉有限通过，whole 因 TCP 尾 FAIL 已停；原 FAIL 和完整退出门保留，未形成正式交付。 |
| Agent Runner 配置 — `ai/agent-runner-mounts-20261011` | runtime_recovery 及指定子作者实施，recovery_lead 提供契约 | 后端 6f609a4a 首编译与独审推进；variables_review 在独立 `ai/agent-runner-mounts-ui-20261011` 并行实现 UI。项目沿原 Agent 配置权限选择系统 Runner 与资源目录名，无额外 Admin 门或独立 Mount 创建 UI；不扩大生产执行接线。 |

## 保留与清理

- SkillHTTP 树保留活跃热 Go cache，SkillsMilestoneResume 树保留共享只读 modules 与既有运行依赖；Go writer 和 native 窗口均按 root 明确交接。
- ProjectVariablesResume 保留未完恢复；ObjectSafe 保留原件与失败。Move、Usage、AgentRunner 活跃树及 Secrets 当前依赖阶段保留；未核其他树不自动清理。
- Find、Sprint、Title、AgentDisplay、TaskHistoryFilters、SecretAudit、Runner Workspace、Outbox 八组 tree/local/remote 已全部清理，root 已用 ls-remote/fetch-prune 确认。完整原件迁入 main `output/ai/` 各任务目录，依赖与日志映射见各自保全说明。

跨设备必要差额见 [Sprint](sprint-edit-ui/README.md)、[Title](knowledge-title-search-ui/README.md)、[TaskHistory](task-history-filters/README.md)、[SecretAudit](secret-audit-stop-adapters/README.md)、[Outbox](outbox-stop-adapter/README.md) 的历史 main 基准与最小补丁，均已实际应用并逐字核原输入。Outbox 补丁仅恢复已复用的历史 PG 输入，当前安全修后代码从 main 恢复；AgentDisplay、Find、Runner 无需额外源码差额。原 FAIL、绝对路径和验收范围不改写，不复制整树、日志或依赖入 Git。

完整 Project initializer、ready、生产 participant/Control 绑定及既有 STOP 按正式记录保持；本索引不批准恢复旧网络故障 probe 或其他停项。
