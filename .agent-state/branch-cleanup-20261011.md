# 2026-10-11 分支清理交接

本记录保存本次已执行清理的范围与来源，不授权删除其它分支或恢复既有 STOP。Git 操作全部由 root 执行；本记录作者只复用此前只读审查与 root 的实际退出交接，未重新联网、运行 Git 或扩大审计。

## 依据与实际结果

只读审查的最终输入为 root 原工具 `17848` actual0 完成显式 main＋ai/* fetch/prune 后的快照。main 与 origin/main 均为 `8f5b57598e4a3fdc2b1fd739fc5194c3b8288304`；下列 25 个 tip 逐一通过 `merge-base --is-ancestor <OID> <main>`，无同名 worktree 占用，且未发现当前保护索引中的名称依赖。快照读取前后 refs/worktree 不变。精确清单复用 main ignored `output/ai/branch-cleanup-audit/post-fetch-01/first-pass-candidates.tsv`；原审查、前后引用及保留项仍在该输出目录。

以下执行事实来自 **root 亲收的原工具结果与本次交接**，并非记录作者重跑所得：

- AgentRunner native02 原 `58727` actual0、完整退出尾释放后，root 对下列 25 名重新执行 `ls-remote`，逐项实际 OID 与已审清单相等。
- root 使用每 ref 显式 expected-OID 的 `--force-with-lease` 与 `--atomic` 删除；原 `25507` actual0，25 项均报告 deleted。本次只删除已核定引用，没有改写提交历史。
- 随后精确查询同 25 名，`ls-remote` actual0 且全部为空；显式 main＋ai/* fetch/prune 原 `29686` actual0。
- 本地 `work` @ `29666627a5685c59ad10205571ebf7f1ced447ef` 已通过普通 `branch -d` 删除。此前已确认其在 main 祖先中、0 独有提交、无 worktree/upstream，限定规则检索未发现 work 专门保护规则；提交仍由 main 可达。
- root 确认清理后 main 已到 `d319a041` 且远端相等。该值是此次交接基准，不要求本记录保存后的新提交仍使用它。

## 本次额外删除的 25 个远端引用

| 分支 | 删除前已核 OID |
| --- | --- |
| `ai/agent-edit-current-app` | `79b70067cb65357ac0bb62f542345860fdb3ec6f` |
| `ai/agent-runtime-delivery` | `c413550cf4ddce16578b45cdc9170c664c421dbc` |
| `ai/agent-sprint-configuration-delivery` | `29666627a5685c59ad10205571ebf7f1ced447ef` |
| `ai/capture-provider-delivery` | `6fef88e8f40aa5a5ddee1e2729457f20199b6443` |
| `ai/execution-context-delivery` | `561a24439b1de6f5612dc431348c89b120486e8f` |
| `ai/execution-direct-text-delivery` | `dc876501d06cecacfcbd4495828fc1b2ba9d1f87` |
| `ai/knowledge-rename-delivery` | `153b4f9fca5fcb2d0e6d1d7d8e5b0ec7fea476ab` |
| `ai/model-environment-capture-delivery` | `41aa77663bf95e7c32cc54966a6f2ada48764782` |
| `ai/project-runtime-audit` | `a3eb45b1e3744e8dd6d0f4e2e00489e5baa6c7d9` |
| `ai/project-runtime-support-delivery` | `6a6da44e01e6f51d934a6684b9071778313405b4` |
| `ai/review-dispatch-delivery` | `65ec9395615282b4c18ad89e3126d04a9697b1b5` |
| `ai/scheduler-execution-delivery` | `af1c8acdb2020a39d00de8e8d042e9b300a0f1d2` |
| `ai/scheduler-relaunch-delivery` | `5b0a3eed54b8f0cb007b4450472bb24da1102a74` |
| `ai/scheduler-retry-config` | `f58da0ad51d9a23c0ee1f61a746fc5fc22207f54` |
| `ai/scheduler-retry-delivery` | `6fa3e147be637dc9c5b11ba198b232cbecba7349` |
| `ai/scheduler-retry-integration` | `fd9390992f5a6d193a45674f6568c67a085cc3ff` |
| `ai/scheduler-runner-delivery` | `bbf8b60fb107ee4e1c904c12ee07386bcb55220e` |
| `ai/sprint-start-delivery` | `1f98aa985ee708c05470e4b017110c85fa3d1a8f` |
| `ai/task-agent-web-delivery` | `44ecbc981690a1e3e6961e555719cc9dafda451f` |
| `ai/task-intake-runtime-delivery` | `bce5ba7002e5568c07e82964c700921dd567e415` |
| `ai/task-intake-write-observation` | `b4f15dd07bc06ec514cd6262b9ef901663e67961` |
| `ai/task-timeline-current-delivery` | `95bc31184ba7faaa24d0767052712182250f7224` |
| `ai/task-timeline-current-reader` | `50658f4385e2edea73fd1e554f92c32b4d1dd4bd` |
| `ai/task-unblock-integration` | `031b5df9266dfb2a54a5b2c8c329c435c50d0291` |
| `ai/work-review-delivery` | `d4a8626162b13c5eff9b98fcf63a5eabd956ba42` |

## 前批保全与合计

此前 Find、Sprint、Title、AgentDisplay、TaskHistoryFilters、SecretAudit、Runner Workspace、Outbox、Secrets、KnowledgeTextUI、Milestone、KnowledgeHTTP、SchedulerHTTP、SchedulerUI、SkillUI **15 组 tree/local/remote** 已由 root 清理并确认远端为空。原件迁入 main `output/ai/` 各任务目录；原 FAIL、绝对路径及有限验收结论不改写。需要跨设备恢复的历史 main 基准与最小源码差额见 [Sprint](sprint-edit-ui/README.md)、[Title](knowledge-title-search-ui/README.md)、[TaskHistory](task-history-filters/README.md)、[SecretAudit](secret-audit-stop-adapters/README.md)、[Outbox](outbox-stop-adapter/README.md)。Outbox 补丁仅恢复历史 PG 输入，当前安全修后代码从 main 恢复；其它材料位置沿各任务保全说明。

两批合计：**40 个远端引用、16 个本地分支、15 个 worktree**。额外 25 个远端引用无对应本地分支或树；本地额外一项为 work。ignored 输出没有复制进本记录；通用恢复与清理规则见 [README](README.md#清理操作与跨环境接续)。持久 fetchspec 仍只取 main，后续刷新 ai/* 须使用 README 中显式 refspec，不能把普通 main-only prune 当作全部任务 tracking 已清理。

## 明确保留

- **1 个祖先引用**：`ai/work-review-runtime-delivery` @ `f4190ad7f6a2efdd7dc7b8235f519de0c109d71f`。虽在 main 祖先中且无 worktree，[历史协调说明](agent-prerequisites/coordination.md)仍记原 refs/材料保留；本轮未解决其材料归属，因此未删，不将历史文字解释为永久保护规则。
- **154 个其它引用**：本轮没有 tip 包含于 main 的证明；仅做有限图关系与 tip 摘要核对，没有用旧名称、close/delivery 标题或可能的 squash/cherry-pick 推定可删。精确 OID 沿 `post-fetch-01/nonancestor-hold.tsv`，后续有实际需要再定向核对。
- **8 个活动或受保护分支及其树**：AgentRunner 后端、AgentRunner UI、Move、Usage、VariablesResume、SkillHTTP、SkillsResume、ObjectSafe；加 main 共 9 个 worktree 未动。保护覆盖未完工作、原 FAIL/恢复材料，以及 SkillHTTP 热 Go cache、SkillsResume 只读 modules/MinIO 等依赖，不以旧名称或局部交付推定可清理。

本记录完成后作者停写，由 root 保存；原 `output/ai/branch-cleanup-audit/` 材料保持。
