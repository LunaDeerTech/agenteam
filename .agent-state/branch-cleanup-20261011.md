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


## 2026-10-12 Knowledge Rename：历史恢复未闭合，KEEP

本次另行只读核对本地已fetch引用，未联网、未运行测试、未修改引用或原材料；不改写以上批次的历史计数。候选 `ai/knowledge-rename-integration` 的精确tip为 `50d52bb52be24673d99d8412bbe41222e5561028`，整tree `df07ead13d9b001a404a5c2dd9d3c18e7894e234` 与main祖先 `153b4f9fca5fcb2d0e6d1d7d8e5b0ec7fea476ab` 相同，祖先检查实际0。这只证明最终文件树相同，不代表全部中间源码可恢复。

逐一核对9个独有提交的25条变更路径，包含原preimage共55个不同path/blob版本；在main完整同路径历史中找到38个，另17个尚未证明可从main或已保全材料恢复，具体如下。未将“同路径未找到”扩大成对象必然不在其它引用或材料中；缺少替代恢复证明即保持该ref。

| 历史路径 | 未证恢复的blob |
| --- | --- |
| `web/src/api/client.ts` | `58cd1f4e6dfdfe0574a73427c8309a9d53d3aefe` |
| `web/src/composables/useSession.ts` | `894d1222327896cc2fdb2561365e75f4a6f3cc17` |
| `web/src/router/auth.ts` | `8461024ee811e439700df33fa133752db0fe6a41` |
| `web/src/tests/knowledge-command-client.spec.ts` | `e05cf21c5549b6771ad0355e1a010ef84cf31b26` |
| `web/src/tests/knowledge-command-state.spec.ts` | `b9a9e06b17022e6f08de6b089815849a057648e0` |
| `.agent-state/knowledge-owner-rename/native-controls.cjs` | `d705ae2314c782c2091a12e01665922c387c449f`、`38db8a7a89d1310222b072dfd2898570bb421074`、`b3b68eb00feec0ebc81035c0e3d3bc65ef713c12` |
| `internal/central/app/knowledge_owner_rename_web_test.go` | `8048ed336f092e8747845e35ef71ccffaac7c086` |
| `tests/account-captcha-web/e2e/knowledge-owner-rename.native.ts` | `9b707f23ad6c3f683a0a3f047ac8b6f6c5359aff`、`2211b72bfd334ee167697dbcbf6cb7322da9dadc`、`bbd0b4da4cd34ab5bf65c2da10026402e546dd29` |
| `tests/account-captcha-web/e2e/knowledge-owner-rename.spec.ts` | `9268d7d1fe22810dd8c84d2d46e6d30b0ec7e7c6`、`ecb2e99745558313dcc9657a3b016a889e56af4c`、`7e84fa21bfdbc1c07bab3e3f27ffd3b49e2a4801`、`67d100f46d213ef0c3ea8435b20582145d287908`、`a501072a6425b2e6b402c064e58d8182b0894678` |

[Rename原恢复说明](knowledge-owner-rename/README.md)保留native01联证FAIL、native03焦点门FAIL及native02过渡图，原source/observer/fixture/spec跨 `8c5af1b5`、`f64dcaae`、`15f28d57` 等版本；例如15f28d57的spec为上表 `a501072a`，不能用最终native04成功输入替代。最终功能已交付与历史FAIL恢复是两项不同条件，本次未闭合后者。

当前worktree清单与本地heads无此同名项。只检查 `/workspace`、`/tmp`、main `output/ai`及两个retired层的直接子目录，未发现同名Rename树/output；原说明指向的 `/workspace/agenteam-skills-owner-ui`、`/workspace/agenteam-skill-install-runtime-source`、对应main output同名根、main `output/ai/knowledge-owner-rename` 和 `/tmp/krn04/ui-2055baa1eebc48dc.log` 当时均不存在。未遍历全部输出、压缩归档或其它旧引用，**不据此声称原件毁损或不存在替代副本**，也没有证明全部旧helper/FAIL材料与owner依赖已解除。

结论为KEEP，不新增删除计数、不新增到原显式keep1。另两个更大metadata候选 `agent-sprint-configuration-integration`（12个独有提交）与 `sprint-start-integration`（15个）未深查，不因整tree等价列为可删除；D13、sprint-focus、branch-cleanup-workflow及旧secret原KEEP不在本轮重审范围。

## 2026-10-12 Write / Command实际清理

以下仅记录root已亲收的实际结果。Write完整历史随main `fff1f829` 合入，Command随后随 `6a27bc3db548caaf0a04e02fbc1bc70933cd619f` 完整合入；main推送原1815 actual0（0d33eb）。两原树均已普通worktree remove，本地分支普通删除：

| 分支 | 删除前精确tip | 原树 | 普通树/本地删除 |
| --- | --- | --- | --- |
| `ai/runner-write-file-operation-20261012` | `98083527258e0478f83694c90f228f7f9c36b208` | `/workspace/agenteam-runner-write-file-operation` | bfcaa3 actual0 |
| `ai/runner-command-owner-20261012` | `5920a949ce99fa5b36d47b45878f8bcdbd231035` | `/tmp/agenteam-runner-command-owner` | 3da004 actual0 |

两ref fresh查询cb992a actual0，精确tip与上表相同；atomic exact-lease删除原11874 actual0（d6485e），后继fresh远端空、fetch/prune和tracking空原16888 actual0（6e2531）。本批增加2树/2本地/2远端，累计 **37 WorkTrees / 38本地分支 / 72远端引用**；legacy141待核、原显式keep1不变。新Command WSS树实施中，未计作清理。

Write的95个原件及tmp副本已同FS保全，完整历史main可达，见[Write恢复说明](runner-write-file-operation/README.md)。Command的52文件/1,996,308B/5目录已dev34→27无覆盖复制，源前后及目标SHA/size/mode/uid/gid/nsmtime一致，新dev/inode身份不能恢复原inode；原 `/tmp` output现随普通树清理删除，main retired完整副本保留。Command合成main的44项仓库输入逐字等原记录，44个历史版本main可达，三份1111输入图不变，原验证复用无需重测；独立补证明见 `output/ai/retired-20261012/runner-command-owner-preservation/main-integration-6a27bc3d{,-completion}.json`，详情见[Command恢复说明](runner-command-owner/README.md)。原件、旧FAIL/退出证据、外部受保护cache/modules及所有STOP不改写；本批不扩大整体功能完成范围。
