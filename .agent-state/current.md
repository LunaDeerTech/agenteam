# 全局协调索引

2026-10-11。main 的本文件当前由 root 指定 recovery_lead 唯一维护正文；root 负责 Git、集成和资源排期。功能分支 current 不覆盖本索引。正式交付边界见[任务台账](../docs/development/agent-team/tasks.md)，分段接受不等于当前 main 单次全量通过。

## 当前开发

| 任务 / 分支 | 负责人 | 状态与下一步 |
| --- | --- | --- |
| Knowledge 子树删除 UI — `ai/knowledge-subtree-delete-ui-20261011` | skills_milestone_review 交付，fixture 子域 | 普通 Owner preview/delete/原 Lookup 有限 UI；native02 wholeFAIL：normal 状态文字定位失败，lost Lookup 502 且无原 stage，正在分别诊断。两轮原 FAIL 均保留，尚未正式交付；原 STOP 保留。 |
| Project Secret 写 UI — `ai/project-secrets-write-ui-20261011` | variables_review 交付，agent_display_settings 独审 | 新树基线 `73dccca1`；首 API 片 `2d07a031` 已有限静审，首 build 进行中，真实方法并行实现，尚未验收。不含删除、注入或新 provider，原 STOP 保留。 |
| Knowledge Move UI — `ai/knowledge-move-ui-20261011` | skills_milestone_review 实施，skill_package_review 独审 | Move05 业务与视觉有限通过，whole 因 TCP 尾 FAIL 已停；原 FAIL 和完整退出门保留，未形成正式交付。 |

## 保留与清理

- 系统 Runner 管理 UI（main `f86f4215`）、Agent Secret 白名单 UI（`e263fc99`）、Runner read-file（`65fc8687`）均已正式交付并推送；三棵原树、本地分支与远端引用已全部清理，必要原件与失败输入已保全。
- SkillHTTP 树保留活跃热 Go cache，SkillsMilestoneResume 树保留共享只读 modules 与既有运行依赖；Go writer 和 native 窗口均按 root 明确交接。
- ProjectVariablesResume 保留未完恢复；ObjectSafe 保留原件与失败。Move 活跃树保留，未核其他树不自动清理。
- Runner directory 已随 main `4e5e70ce` 有限交付并推送，28 个相关 race top、六包 vet 与真实 PG/WSS 整轮独立接受；原树、本地分支及远端引用均已清理。完整 output 已同文件系统无覆盖迁至 main `output/ai/retired-20261011/runner-directory-adapter/`，其中 `retention/` 保存原 `/tmp/rda01` log/owned/request 的逐字副本和路径映射；原 `/tmp` 保留。本片无失败输入需补恢复差额，上游严格授权替身、生产默认 nil executor/旧 STOP 保留。
- 已完成清理共 24 个 WorkTree、25 个本地分支、52 个远端引用；其中 25 个额外远端引用为严格 main 祖先，没有对应本地分支或树，另有已删除的本地 work296。root 已确认前述三棵退役树不存在、对应 local/tracking 为空，精确 lease 删除、ls-remote 空结果及 main+ai prune 均实际成功。Directory 普通 worktree remove 成功，branch -d 因非祖先拒绝后仅对已交 squash 分支执行窄 -D；最新 tip 精确 lease 删除、成功空查询、prune 和 local/tracking 空结果均已闭合。另有 D16 filesystem、D18 NameProjection、SearchBenchmark 三个完整 scope 已交且无独占依赖的旧远端引用，经 fresh tip 核对、精确 lease 删除、成功空查询及 prune 后完成清理，没有新增本地分支或树；原 154 个待核引用现剩 151 个，1 个显式保留历史引用仍未动，其他活动分支按 owner 保留。

跨设备必要差额与各自核对范围见 [Sprint](sprint-edit-ui/README.md)、[Title](knowledge-title-search-ui/README.md)、[TaskHistory](task-history-filters/README.md)、[SecretAudit](secret-audit-stop-adapters/README.md)、[Outbox](outbox-stop-adapter/README.md)、[AgentRunner 后端](agent-runner-mounts/README.md)、[AgentRunner UI](agent-runner-mounts-ui/README.md)、[ExecutionMountCapture](execution-mount-capture/README.md)、[Project Usage UI](project-usage-read-ui/README.md)、[ExecutionMountDirectText](execution-mount-directtext/README.md)、[SystemRunner UI](system-runner-management-ui/recovery/README.md)、[Agent Secret 白名单 UI](agent-secret-allowlist-ui/README.md)、[Runner read-file](runner-read-file-adapter/README.md)、[SearchBenchmark 原 v1](search-benchmark/README.md)。近批完整原件位于 main `output/ai/retired-20261011/` 对应任务目录；read-file 的 `output/ai/runner-read-file-adapter/main-integration01.json` 另留原位。更早原件按各自保全说明定位。原 FAIL、绝对路径及验收边界保持；早期 UI source map、Usage 局部输入，以及 SystemRunner/Secret 未提交失败输入缺少原字节或逐项 SHA，以及 Search 原日志与误设夹具缺失的限制均不补造。AgentDisplay、Find、Runner Workspace 无需额外源码差额；不复制整树、日志或依赖入 Git。

完整 Project initializer、ready、生产 participant/Control 绑定及既有 STOP 按正式记录保持；本索引不批准恢复旧网络故障 probe 或其他停项。
