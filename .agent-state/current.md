# 全局协调索引

2026-10-11。main 的本文件由 root 指定 recovery_lead 唯一维护正文；root 负责 Git、集成和资源排期。功能分支 current 不覆盖本索引。正式交付边界见[任务台账](../docs/development/agent-team/tasks.md)，分段接受不等于当前 main 单次全量通过。

## 当前开发

| 任务 / 分支 | 负责人 | 状态与下一步 |
| --- | --- | --- |
| Runner read-file 可构造适配器 — `ai/runner-read-file-adapter-20261011` | recovery_lead 交付，runner_read_file 子域 | 当前 Mount PG 授权解析、单次 dispatch、真实认证 WSS 与 Manager.Read 正在实现，未验收；上游 Operation 授权仍需严格提供者，生产默认 nil executor 与旧 STOP 保留。 |
| Agent Secret Variable 白名单 UI — `ai/agent-secret-allowlist-ui-20261011` | variables_review 交付，secret_allowlist_fixture 方法 | 基线 9bc9e13a；普通 Owner 的安全 metadata 选择与 Agent 创建/编辑、原 Unknown 恢复正在独立实现；未验收，不接 Secret 值或运行注入。 |
| Knowledge Move UI — `ai/knowledge-move-ui-20261011` | skills_milestone_review 实施，skill_package_review 独审 | Move05 业务与视觉有限通过，whole 因 TCP 尾 FAIL 已停；原 FAIL 和完整退出门保留，未形成正式交付。 |

## 保留与清理

- SkillHTTP 树保留活跃热 Go cache，SkillsMilestoneResume 树保留共享只读 modules 与既有运行依赖；Go writer 和 native 窗口均按 root 明确交接。
- ProjectVariablesResume 保留未完恢复；ObjectSafe 保留原件与失败。Move 活跃树保留，未核其他树不自动清理。
- Find、Sprint、Title、AgentDisplay、TaskHistoryFilters、SecretAudit、Runner Workspace、Outbox、Secrets、KnowledgeTextUI、Milestone、KnowledgeHTTP、SchedulerHTTP、SchedulerUI、SkillUI、AgentRunner 后端、AgentRunner UI、ExecutionMountCapture、Project Usage UI、ExecutionMountDirectText 二十组 tree/local/remote 已全部清理。近期五树已普通 remove，远端引用按精确 lease 删除成功；对应本地引用仅因 squash 非祖先被普通 `-d` 拒绝，在完整交付、材料保全与无依赖核实后逐个窄 `-D` 成功。root 已确认对应 worktree、本地及 tracking 引用均不存在，成功的精确 ls-remote 为空，main+ai fetch/prune 实际成功。完整原件迁入 main `output/ai/` 各任务目录，依赖与日志映射见各自保全说明。另 25 个严格 main 祖先旧远端引用已按逐 OID lease 原子删除，精确查询为空、fetch/prune 成功；本地 work296 已普通删除。合计清理 45 个远端引用、21 个本地分支和 20 个 WorkTree；额外 25 个引用没有对应本地分支或树。154 个未证明包含于 main 的引用与 1 个显式保留历史引用仍未动，其他活动分支按 owner 保留。

跨设备必要差额见 [Sprint](sprint-edit-ui/README.md)、[Title](knowledge-title-search-ui/README.md)、[TaskHistory](task-history-filters/README.md)、[SecretAudit](secret-audit-stop-adapters/README.md)、[Outbox](outbox-stop-adapter/README.md)、[AgentRunner 后端](agent-runner-mounts/README.md)、[AgentRunner UI](agent-runner-mounts-ui/README.md)、[ExecutionMountCapture](execution-mount-capture/README.md)、[Project Usage UI](project-usage-read-ui/README.md)、[ExecutionMountDirectText](execution-mount-directtext/README.md) 的历史 main 基准与最小补丁，均已实际应用并逐字核已记录的必要输入。最近五套原件位于 `output/ai/retired-20261011/`；原 FAIL、早期 UI 未记录完整 source map，以及 Usage 局部 SHA map 对应的部分未提交源码字节缺失限制均保留。Outbox 补丁仅恢复已复用的历史 PG 输入，当前安全修后代码从 main 恢复；AgentDisplay、Find、Runner Workspace 无需额外源码差额。原 FAIL、绝对路径和验收范围不改写，不复制整树、日志或依赖入 Git。

完整 Project initializer、ready、生产 participant/Control 绑定及既有 STOP 按正式记录保持；本索引不批准恢复旧网络故障 probe 或其他停项。
