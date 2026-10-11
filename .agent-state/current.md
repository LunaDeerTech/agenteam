# 全局协调索引

2026-10-11。当前协调位置为 main 的本文件，由 root 指定 variables_review 唯一维护正文；root 负责全部 Git、集成与资源排期。功能分支的 current 只记录本片状态，合入时不覆盖本索引。正式产品状态见[任务台账](../docs/development/agent-team/tasks.md)，下表只保留当前任务和下一步；固定输入的分段接受不等于当前 main 单次全量通过。

## 当前五线

| 任务 / 分支 | 负责人及依赖 | 当前状态与下一步 |
| --- | --- | --- |
| Outbox stop adapter — `ai/outbox-stop-adapter-20261011` | runtime_recovery 实施，skill_package_review 独审；同 Store 生命周期端口 | 正在收真实 PG native01 结果与最终独审、有限文档。普通 delivery、Cleanup、生产 registry/initializer 和旧整卡 STOP 不变；以本树原件和 root 窗口交接为准。 |
| Project Usage 只读 UI — `ai/project-usage-read-ui-20261011` | agent_display_settings 实施，recovery_lead 独审；基于 Secrets 主线 `7e0a123e` | 产品基础控/build 已完成有限验证，浏览器方法待最终组合独审及原资源窗口；不提前计 browser、真实焦点或完整 Usage 交付。依赖已私有复制。 |
| Runner Workspace manager — `ai/runner-workspace-manager-20261011` | variables_review 实施，skill_package_review 最终独审；recovery_lead 仅契约咨询 | 技术 `20672baa` 与文档 `de87b8d9` 有限接受，本次主线合入；11 top/11 sub race 与 vet、原 FAIL/窄修保留。仅本地 Ensure→Read/List/Find，后继 Mount/Control/executor 接线独立。 |
| Knowledge Move UI — `ai/knowledge-move-ui-20261011` | skills_milestone_review 实施，skill_package_review 独审 | Move04 业务有限接受但 whole 因 TCP delta FAIL 未交付；Move05 固定输入复验仍受容量门阻塞，未启动。保留全部旧 FAIL，不降低原输入、预算或退出门。 |
| Agent Runner 配置原子链 — 分支待 root 指派 | recovery_lead 提供契约，实施 owner 待排 | 待启动。按用户确认：系统管理 Runner，项目内沿既有 Agent 配置权限在创建/编辑 Agent 选择 Runner 并填写挂载目录 name；无额外系统 Admin 门、无独立 Mount 创建 UI。不以本地 Workspace 库代当前授权或生产执行接线。 |

## 必须保留的依赖和材料

- `/workspace/agenteam-skill-package-http` 保留活跃热 Go cache；`/workspace/agenteam-skills-milestone-resume` 保留共享只读 module cache 及已明确运行依赖。任何 Go writer 必须遵守 root 的缓存交接。
- `/workspace/agenteam-project-variables-resume` 保留未完恢复上下文；`/workspace/agenteam-object-runtime-join-safe` 保留原件和既有失败。不能按旧任务名称或完成标题自动清理。
- Move、Outbox、Usage、新 Runner 活跃树保留；Secrets 树暂保留到 Usage 依赖交接阶段核清。其他树只按 owner 停写、当前引用、实际交付和材料保全逐个判断。
- AgentDisplay、TaskHistoryFilters、SecretAudit 三树正在逐个只读评估，尚未记为已删除；原件迁移及 root 实际 tree/local/remote 删除确认后再移出保留范围。

## 已完成树的材料位置

root 已清理 Find、Sprint、Title 的工作树和本地/远端任务分支，它们不再属于活动任务。必要原件分别保存在 main 的 `output/ai/workspace-find/`、`output/ai/sprint-edit-ui/`、`output/ai/knowledge-title-search-ui/`；各自保全说明给出历史 main 恢复基准、必要差异源码、依赖及原日志位置。迁移没有改写原 FAIL、输入绝对路径或产生新的测试通过结论。后续清理只在实际结果成立后同步本节，不另建永久台账。

完整 Project initializer、ready、生产 participant/Control 绑定及所有既有 STOP 均按正式任务记录保持；本索引不批准恢复旧网络故障 probe 或其他停项。
