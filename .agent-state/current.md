# 单 read-file Operation / Attempt

2026-10-11。本文件属于 `/workspace/agenteam-runner-read-file-operation` 的 `ai/runner-read-file-operation-20261011`；root 负责 Git、集成和资源排期。本分支只保留 main `95c092bc` 的已交付及清理事实，不覆盖 main 全局索引。正式交付边界见[任务台账](../docs/development/agent-team/tasks.md)，分段接受不等于当前 main 单次全量通过。

## 当前开发

| 任务 / 分支 | 负责人 | 状态与下一步 |
| --- | --- | --- |
| Execution ToolCall 事实 — `ai/execution-tool-call-facts-20261011` | recovery_lead 交付；Tool 域 runner_read_file；runtime_recovery 独审 | 本次集成正式交付有限片：原句柄实际 Joined 后同事务写保护参数 / ToolCall / Transcript 与 running/tool-pending；34 + 26 top 分段 race、适用 vet、native03 原两场景 wholePASS 已独立接受。原 unit / Model 控制与 native01/02 FAIL 保留；仅 Model JSONCaller 受控，不含 ToolOperation / Runner 派发或生产 tools。00063/00064 随本片，完整分支历史保留；原树 / output / tmp 等正式保存后保全清理。 |
| 单次 read-file Operation/Attempt 基础 | root 统筹并行实施 | 两组基础与compile01/02及精确list通过；native01/02均wholeFAIL。02清Mount子场景PASS，完整结果子场景err=nil未达原COMMIT_UNKNOWN预期；codec静态矛盾正在定向验证，未native03、未验收整链。 |
| Knowledge Move UI — `ai/knowledge-move-ui-20261011` | skills_milestone_review 实施，skill_package_review 独审 | Move05 业务与视觉有限通过，whole 因 TCP 尾 FAIL 已停；原 FAIL 和完整退出门保留，未形成正式交付。 |

## 本片恢复与下一步

- 主目标：真实 ToolCall → 单次 read-file Operation/Attempt → 完整持久结果，不包含下一轮 Model/Transcript 或自动重试。正式写域见 [D18/D22 工作卡](../docs/development/work-items/d18-runner-read-file-operation.md)。Tool lead 为 skill_package_review，Execution 由 skills_milestone_review、Mount/Runner 由 knowledge_delete_fixture 实现，variables_review 写最小 PG 方法，runtime_recovery 独审。
- Tool 基础 `d3dc0b18` 已保存，迁移65两变量改原列 `%TYPE` 的 `86c298c9` 已有限静审；原install编码/摘要未改，read-file完整payload沿原512KiB协议上限。Runner固定RID接缝 `6f87bfff`、Execution live callback `4b73635e`、Mount metadata-only目标投影 `0eeeea0e` 已保存。三接口19top race/三包vet与Tool16top34sub race/两包vet分段通过；666/509输入各自前后相同，不计真实执行链已通。
- 上游最初 `6e0b925c` 作为未验收开发依赖合入，现已由 main `95c092bc` 正式有限交付 ToolFacts，原native01/02 FAIL及修后03实际wholePASS均保留；该验收不包含本片ToolOperation/Runner。此合并带入原PG聚合SQL修正，不复制未提交WIP。
- producer两处静态must-fix已修并经适用基础控制：最终schema/依赖错误区分；Lookup独立实际返回登记与停止门。原consumer contract保持冻结。compile01 race编译与精确list实际0，1827输入前后相同，binary `89e84ed1…a8ec78f`；唯一PG top的两个sub未增加场景。
- native01原工具32554实际1/140.340s，**wholeFAIL**：65536结果场景尚未进入Operation便被旧canonical64断言拒绝；清Mount场景到达新业务后验后，被旧joined尾的Operation0断言拒绝。Go/driver实际Wait1、七资源14absence/private/runtime/desc双尾与1828输入不变齐；原75s HOST_TCP仍余1条TIME_WAIT差额，不能称退出门全过。原 `output/ai/runner-read-file-operation/{compile01,native01}/` 与 `/tmp/rfo01/pg-8c62651ea7e34711ac66556710e69e16.log` 保留，窗口已实际释放。
- 2eadbbde/872ebdb3仅修两fixture：原facts默认64/0不变；新PG显式场景canonical与阶段Operation0/1，复用全部旧强断言，再核exact原Operation/Attempt/结果/wire及实际Join。差额独审接受；compile02/list均实际0，1827输入前后相同，对01仅这两夹具变化，binary `f934ff93…13b475f2` 保留，basic未重复。独审确认harness STOP仅限原轮；native01 TIME_WAIT归属仍未明。
- native02原63642亲收actual1/supervisor709384实际1/120.850s，**wholeFAIL**：clear子场景PASS15.80s，原零发送/身份/退休与Join后验成立；happy FAIL14.56s，Attempt barrier后Execute err=nil而要求COMMIT_UNKNOWN，未记录安全Status/phase，不能补造原分支。Go/driver实际Wait1，七资源14absence/private/runtime/desc/TCP两delta_empty及1828inputs不变齐。原件 `output/ai/runner-read-file-operation/{compile02,native02}/` 与 `/tmp/rfo02/pg-b0e94d0c377f47909e923553ed067107.log` 保留；本轮TCP通过不解释01差额。
- 静态发现Message嵌套JSON键重排与Tool成功条件逐字比较相冲突；真实codec反例已固定d40fc527，由Tool作者另窗验证/修复，未预记结果，也不倒填native02动态首因。当前没有native03；原PG强断言、预算和退出门保持。Model JSON仍唯一端口替身，生产默认nil及其他STOP不变。

## 保留与清理

- Project Secret 写 UI 已随 main `22d0a0b6` 有限交付：普通 Owner 创建、元数据修改、明确替换与原身份 Lookup 恢复，native02 wholePASS 独立接受；原 FAIL 保留。完整证据已同文件系统无覆盖保全至 `output/ai/retired-20261011/project-secrets-write-ui/`，含组合 Knowledge 控制、dist 与两轮 tmp 原件副本；原树已普通移除，本地分支在 squash 非祖先拒绝后窄删；精确 tip lease 远端删除、成功空查询及 prune 均已核实，三项清理已计入下列总数。删除、运行注入、生产 Project initializer 与原 STOP 不在本片。
- Knowledge 子树删除 UI 已于 main `48fe9a37` 有限交付，native03 wholePASS 独立接受；原树、本地分支及远端引用均已实际清理并核为空。两输出根、dist、三轮 `/tmp` 原件及必要恢复差额已保全；原01/02 FAIL、02 Lookup502 未定因和 cleanup_pending 不代表物理 Object 回收的边界保持。
- 系统 Runner 管理 UI（main `f86f4215`）、Agent Secret 白名单 UI（`e263fc99`）、Runner read-file（`65fc8687`）均已正式交付并推送；三棵原树、本地分支与远端引用已全部清理，必要原件与失败输入已保全。
- SkillHTTP 树保留活跃热 Go cache，SkillsMilestoneResume 树保留共享只读 modules 与既有运行依赖；Go writer 和 native 窗口均按 root 明确交接。
- ProjectVariablesResume 保留未完恢复；ObjectSafe 保留原件与失败。Move 活跃树保留，未核其他树不自动清理。
- Runner directory 已随 main `4e5e70ce` 有限交付并推送，28 个相关 race top、六包 vet 与真实 PG/WSS 整轮独立接受；原树、本地分支及远端引用均已清理。完整 output 已同文件系统无覆盖迁至 main `output/ai/retired-20261011/runner-directory-adapter/`，其中 `retention/` 保存原 `/tmp/rda01` log/owned/request 的逐字副本和路径映射；原 `/tmp` 保留。本片无失败输入需补恢复差额，上游严格授权替身、生产默认 nil executor/旧 STOP 保留。
- 已完成清理共 26 个 WorkTree、27 个本地分支、57 个远端引用；其中 26 个额外远端引用为严格 main 祖先，没有对应本地分支或树，另有已删除的本地 work296。root 已确认前述三棵退役树不存在、对应 local/tracking 为空，精确 lease 删除、ls-remote 空结果及 main+ai prune 均实际成功。Directory 普通 worktree remove 成功，branch -d 因非祖先拒绝后仅对已交 squash 分支执行窄 -D；最新 tip 精确 lease 删除、成功空查询、prune 和 local/tracking 空结果均已闭合。另有 D16 filesystem、D18 NameProjection、SearchBenchmark 三个完整 scope 已交且无独占依赖的旧远端引用，经 fresh tip 核对、精确 lease 删除、成功空查询及 prune 后完成清理，没有新增本地分支或树。新增清理 `ai/work-review-runtime-delivery`：tip `f4190ad7` 全部历史经 main 可达，无本地分支、worktree 或活动依赖；root fresh tip 核对后精确 lease 删除成功，ls-remote 成功为空，prune 后远端跟踪引用为空，无需新增恢复材料。原 154 个待核引用现剩 148 个，1 个显式保留历史引用仍未动，其他活动分支按 owner 保留。


- 两条无 local/tree 或活动依赖的旧 remote 已完成 fresh tip 核对、双 exact-lease 原子删除、成功空查询及 prune：`ai/task-timeline-http`（`1e6b3db2809ce0e62de6c8c2042a0eaf3b9b67f5`）完整 6 路径及 6 个提交版本逐字保留于 main 祖先 `bfebd676`；`ai/agent-directory-http`（`235887d2f615ed6bd6863d9c8b4a1b1e548f32d2`）完整 14 路径等于 main 祖先 `2c59cf0d`，跨三提交的 15 个文件版本均可从 main 历史恢复，无需新增材料。
- `ai/secret-variables-owner`（`866a3fed`）继续保留：最终合同及独立控制已在 main，但 7 个历史源码版本尚未证明可从 main 或保全材料恢复，涉及 `7c2fb954` 的原名称 Schema/decoder 失败输入；不因最终源码相同而删除历史引用。

跨设备必要差额与各自核对范围见 [Sprint](sprint-edit-ui/README.md)、[Title](knowledge-title-search-ui/README.md)、[TaskHistory](task-history-filters/README.md)、[SecretAudit](secret-audit-stop-adapters/README.md)、[Outbox](outbox-stop-adapter/README.md)、[AgentRunner 后端](agent-runner-mounts/README.md)、[AgentRunner UI](agent-runner-mounts-ui/README.md)、[ExecutionMountCapture](execution-mount-capture/README.md)、[Project Usage UI](project-usage-read-ui/README.md)、[ExecutionMountDirectText](execution-mount-directtext/README.md)、[SystemRunner UI](system-runner-management-ui/recovery/README.md)、[Agent Secret 白名单 UI](agent-secret-allowlist-ui/README.md)、[Runner read-file](runner-read-file-adapter/README.md)、[SearchBenchmark 原 v1](search-benchmark/README.md)、[Knowledge 子树删除 UI](knowledge-subtree-delete-ui/README.md)、[Project Secret 写 UI](project-secrets-write-ui/README.md)。近批完整原件位于 main `output/ai/retired-20261011/` 对应任务目录；read-file 的 `output/ai/runner-read-file-adapter/main-integration01.json` 另留原位。更早原件按各自保全说明定位。原 FAIL、绝对路径及验收边界保持；早期 UI source map、Usage 局部输入，以及 SystemRunner/Secret 未提交失败输入缺少原字节或逐项 SHA，以及 Search 原日志与误设夹具缺失的限制均不补造。AgentDisplay、Find、Runner Workspace 无需额外源码差额；不复制整树、日志或依赖入 Git。

完整 Project initializer、ready、生产 participant/Control 绑定及既有 STOP 按正式记录保持；本索引不批准恢复旧网络故障 probe 或其他停项。
