# 全局协调索引

2026-10-11。当前协调位置为 main 的本文件，由 root 指定 variables_review 唯一维护正文；root 负责全部 Git、集成与资源排期。功能分支的 current 只记录本片状态，合入时不覆盖本索引。正式产品状态见[任务台账](../docs/development/agent-team/tasks.md)，下表只保留当前任务和下一步；固定输入的分段接受不等于当前 main 单次全量通过。

## 当前五线

| 任务 / 分支 | 负责人及依赖 | 当前状态与下一步 |
| --- | --- | --- |
| Outbox stop adapter — `ai/outbox-stop-adapter-20261011` | runtime_recovery 实施，skill_package_review 独审；同 Store 生命周期端口 | 限定真实 PG native01 已最终独立接受，原完整退出门与 FAIL 保留，正式文档随本次提交合入。普通 delivery、Cleanup、生产 registry/initializer 和旧整卡 STOP 不变；旧树待后继材料/依赖核清再清理。 |
| Project Usage 只读 UI — `ai/project-usage-read-ui-20261011` | agent_display_settings 实施，recovery_lead 独审；基于 Secrets 主线 `7e0a123e` | 产品基础控/build 与浏览器方法已推进，等待 native 的输入准入和原资源窗口；不提前计 browser、真实焦点或完整 Usage 交付。依赖已私有复制。 |
| Runner Workspace manager — `ai/runner-workspace-manager-20261011` | variables_review 实施，skill_package_review 最终独审；recovery_lead 仅契约咨询 | 技术 `20672baa` 与文档 `de87b8d9` 已由 main `3c7f820d` 正式交付并推送；11 top/11 sub race 与 vet、原 FAIL/窄修保留。仅本地库，原树待 cleanup，后继 Mount/Control/executor 接线独立。 |
| Knowledge Move UI — `ai/knowledge-move-ui-20261011` | skills_milestone_review 实施，skill_package_review 独审 | Move04 业务有限接受但 whole 因 TCP delta FAIL 未交付；Move05 固定输入复验仍受容量门阻塞，未启动。保留全部旧 FAIL，不降低原输入、预算或退出门。 |
| Agent Runner 配置原子链 — `ai/agent-runner-mounts-20261011` | runtime_recovery 即将实施，recovery_lead 提供契约；新树基线 `3c7f820d` | 树 `/workspace/agenteam-agent-runner-mounts` 已建、即将启动。按用户确认：系统管理 Runner，项目内沿既有 Agent 配置权限在创建/编辑 Agent 选择 Runner 并填写挂载目录 name；无额外系统 Admin 门、无独立 Mount 创建 UI。不以本地 Workspace 库代当前授权或生产执行接线。 |

## 必须保留的依赖和材料

- `/workspace/agenteam-skill-package-http` 保留活跃热 Go cache；`/workspace/agenteam-skills-milestone-resume` 保留共享只读 module cache 及已明确运行依赖。任何 Go writer 必须遵守 root 的缓存交接。
- `/workspace/agenteam-project-variables-resume` 保留未完恢复上下文；`/workspace/agenteam-object-runtime-join-safe` 保留原件和既有失败。不能按旧任务名称或完成标题自动清理。
- Move、Outbox、Usage、Agent Runner mounts 树保留，已交 Runner manager 树待清理；Secrets 树暂保留到 Usage 依赖交接阶段核清。其他树只按 owner 停写、当前引用、实际交付和材料保全逐个判断。
- TaskHistoryFilters、SecretAudit 原件已保全，root 已普通删除两工作树；两组 local/remote refs 暂保留，待四份必要源码差额补丁随本次提交推送后再删除。AgentDisplay 的 tree/local/remote 已由 root 确认全部删除。

## 已完成树的材料位置

root 已确认清理 Find、Sprint、Title、AgentDisplay 的工作树和本地/远端任务分支，它们不再属于活动任务；TaskHistoryFilters、SecretAudit 两工作树也已删除但引用按上节暂保留，共六棵旧树已删、四组引用已清。必要原件分别保存在 main 的 `output/ai/workspace-find/`、`output/ai/sprint-edit-ui/`、`output/ai/knowledge-title-search-ui/`；各自保全说明给出历史 main 恢复基准、必要差异源码、依赖及原日志位置。迁移没有改写原 FAIL、输入绝对路径或产生新的测试通过结论。AgentDisplay 完整原件在 `output/ai/agent-display-settings/` 与 `output/ai/agent-edit-web/`，TaskHistory 在 `output/ai/task-history-filters/` 与 `output/ai/task-timeline-web/`，SecretAudit 在 `output/ai/secret-audit-stop-adapters/`；均保留原 FAIL 和监督日志。后续清理只在实际结果成立后同步本节，不另建永久台账。

跨设备恢复仅保留必要文本差额：[Sprint](sprint-edit-ui/README.md)、[Title](knowledge-title-search-ui/README.md)、[TaskHistory](task-history-filters/README.md)、[SecretAudit](secret-audit-stop-adapters/README.md) 各有基于指定历史 main 的 `retired-inputs.patch`，已在私有临时目录实际应用并逐字等原验收输入；没有复制整树、依赖或日志入 Git。AgentDisplay/Find 的受控源码均可直接由其历史 main 恢复。

完整 Project initializer、ready、生产 participant/Control 绑定及所有既有 STOP 均按正式任务记录保持；本索引不批准恢复旧网络故障 probe 或其他停项。
