# 全局协调索引

2026-10-11。main 的本文件由 root 指定 variables_review 唯一维护正文；root 负责 Git、集成和资源排期。功能分支 current 不覆盖本索引。正式交付边界见[任务台账](../docs/development/agent-team/tasks.md)，分段接受不等于当前 main 单次全量通过。

## 当前开发

| 任务 / 分支 | 负责人 | 状态与下一步 |
| --- | --- | --- |
| Runner directory 适配器 — `ai/runner-directory-adapter-20261011` | recovery_lead 交付，runner_read_file 子域 | 以已交 main `65fc8687` 为基线，list/find 正在独立树实现，尚未验收；不依赖已退役 read-file 树，上游授权与生产默认 nil executor/旧 STOP 保留。 |
| Knowledge 子树删除 UI — `ai/knowledge-subtree-delete-ui-20261011` | skills_milestone_review 交付，fixture 子域 | 基线 `a15d8bbd`；普通 Owner 的 preview/delete/原 Lookup 有限 UI 与真实夹具实现、基础检查进行中，尚未验收，原 STOP 保留。 |
| Knowledge Move UI — `ai/knowledge-move-ui-20261011` | skills_milestone_review 实施，skill_package_review 独审 | Move05 业务与视觉有限通过，whole 因 TCP 尾 FAIL 已停；原 FAIL 和完整退出门保留，未形成正式交付。 |

## 保留与清理

- 系统 Runner 管理 UI（main `f86f4215`）、Agent Secret 白名单 UI（`e263fc99`）、Runner read-file（`65fc8687`）均已正式交付并推送；三棵原树、本地分支与远端引用已全部清理，必要原件与失败输入已保全。
- SkillHTTP 树保留活跃热 Go cache，SkillsMilestoneResume 树保留共享只读 modules 与既有运行依赖；Go writer 和 native 窗口均按 root 明确交接。
- ProjectVariablesResume 保留未完恢复；ObjectSafe 保留原件与失败。Move 活跃树保留，未核其他树不自动清理。
- 已完成清理共 23 个 WorkTree、24 个本地分支、51 个远端引用；其中 25 个额外远端引用为严格 main 祖先，没有对应本地分支或树，另有已删除的本地 work296。root 已确认三棵最新退役树不存在、对应 local/tracking 为空，精确 lease 删除、ls-remote 空结果及 main+ai prune 均实际成功。另有 D16 filesystem、D18 NameProjection、SearchBenchmark 三个完整 scope 已交且无独占依赖的旧远端引用，经 fresh tip 核对、精确 lease 删除、成功空查询及 prune 后完成清理，没有新增本地分支或树；原 154 个待核引用现剩 151 个，1 个显式保留历史引用仍未动，其他活动分支按 owner 保留。

跨设备必要差额与各自核对范围见 [Sprint](sprint-edit-ui/README.md)、[Title](knowledge-title-search-ui/README.md)、[TaskHistory](task-history-filters/README.md)、[SecretAudit](secret-audit-stop-adapters/README.md)、[Outbox](outbox-stop-adapter/README.md)、[AgentRunner 后端](agent-runner-mounts/README.md)、[AgentRunner UI](agent-runner-mounts-ui/README.md)、[ExecutionMountCapture](execution-mount-capture/README.md)、[Project Usage UI](project-usage-read-ui/README.md)、[ExecutionMountDirectText](execution-mount-directtext/README.md)、[SystemRunner UI](system-runner-management-ui/recovery/README.md)、[Agent Secret 白名单 UI](agent-secret-allowlist-ui/README.md)、[Runner read-file](runner-read-file-adapter/README.md)、[SearchBenchmark 原 v1](search-benchmark/README.md)。近批完整原件位于 main `output/ai/retired-20261011/` 对应任务目录；read-file 的 `output/ai/runner-read-file-adapter/main-integration01.json` 另留原位。更早原件按各自保全说明定位。原 FAIL、绝对路径及验收边界保持；早期 UI source map、Usage 局部输入，以及 SystemRunner/Secret 未提交失败输入缺少原字节或逐项 SHA，以及 Search 原日志与误设夹具缺失的限制均不补造。AgentDisplay、Find、Runner Workspace 无需额外源码差额；不复制整树、日志或依赖入 Git。

完整 Project initializer、ready、生产 participant/Control 绑定及既有 STOP 按正式记录保持；本索引不批准恢复旧网络故障 probe 或其他停项。
