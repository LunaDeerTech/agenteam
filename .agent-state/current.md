# 当前状态：F0 因用户实看反馈重开返修

2026-10-15，用户实看后未接受现有 F0 的信息结构与视觉品质：项目菜单出现额外项、项目设置结构和下拉样式不正确，层次关系不清，与 Linear 的整体完成度差距明显，动效和质感不足。**F0 已重开，视觉系统数值待重新校准；F1—F8 未开始，正式产品未迁移。** 既有技术与交互检查的通过事实按当时输入保留，不能据此继续称设计已最终接受或用户已认可。

## 本轮分支与职责

- 本轮调整基线：`c4c4c9d7`；任务分支：`ai/frontend-f0-refinement-20261015`；当前工作树：`/workspace/agenteam-f0-refinement`。原 F0 工作树已移除，不能沿旧目录恢复。
- 唯一活动写会话为 `/root`，root 负责全部 Git 与集成。Agent 返回导航独立上置、详情说明使用当前描述的小修已保存至 `80c1fd71`；当前转入三 Tab 调整，尚无新检查结论。本轮继续使用任务分支，暂不合并 main，main 仍为 `b6db5297`；恢复按[状态说明](README.md)核对分支最新提交与本地差异。
- `/root/f0_refinement_designer` 负责设计与样板实现，本轮不写正式设计正文；`/root/f0_structure_audit` 只读核对结构；`/root/f0_refinement_review` 负责独立审查；`/root/f0_test` 更新作者侧 harness；`/root/f0_docs` 独写本文件、任务台账顶部、`prototype/README.md`，以及 `page-blueprints.md`、`target-design.md`、`implementation-plan.md` 本轮对应规则。

## 当前断点与下一步

技能与公开实践已归入[前端重设计技能](../.agents/skills/agenteam-frontend-redesign/SKILL.md#从一个完整页面建立方法)与[来源说明](../docs/development/agent-team/skill-sources.md#前端重设计方法的本地适配)。用户已实看 general 并要求去除名称/描述外层框，限定开放布局修正已完成；这不代表用户已认可整个示范。[目标设计 §8](../docs/frontend-design/redesign/target-design.md#8-f0-可实现视觉值与组件使用决定)仍保留待重校的原样板数值。

**当前正在按用户新要求将 Agent 详情改为三个 Tab：基本配置、能力与资源、记忆。** 基本配置承载身份、描述、指令、模型与审批；取消独立“查看记忆”按钮，同一 Agent 切换 Tab 保留草稿。详情标题、当前描述与上方独立“← Agent”返回列表入口保持稳定；没有描述时不显示说明行，技术名留在基本信息。页头宽度与标题/正文横向起点仍一致，不要求不同层级标题纵向齐平。实际路由与返回契约待实现者交付，本轮尚未完成内部检查或取得用户认可；MCP 仅保留已做的页头调整，不扩对象结构。

此前 general 的正常/减少动效、窄屏、菜单与原生200%竞态修复等限定证据见[测试说明](../docs/frontend-design/redesign/prototype/test-README.md)与[独审入口](../docs/frontend-design/redesign/prototype/review/README.md)，只按未受影响的原输入复用。旧11类模板没有本轮整体验收；11个设置栏目中其余8叶仍无本轮业务样板，`task-pages.js` / `task.css` 仍未接入并保留 WIP。

下一步完成三 Tab 及同对象草稿/返回行为，冻结后按影响检查，再同步[样板说明](../docs/frontend-design/redesign/prototype/README.md#启动与参数)中的实际入口；现有说明与检查不得代证新 Tab 已完成。3个 Agent 是按真实字段构造的合成对象，不能与旧正式 Vue 的单 reviewer 声称同输入对照。实现仍限隔离样板及必要文档/harness，未修改产品 `web/src/`、后端或业务契约；F0 全局未定稿，设计值未冻结，F1—F8 未开始。

## 材料与检查边界

- [样板说明](../docs/frontend-design/redesign/prototype/README.md)保存仓库内启动入口、参数、现有 API 映射与合成边界；需取得本任务分支，单独更新 main 没有本轮候选。
- [作者检查](../docs/frontend-design/redesign/prototype/test-README.md)与[独立审查](../docs/frontend-design/redesign/prototype/review/README.md)分别保留此前 Agent 候选的限定通过、真实失败与复验记录，以及返回导航小修在用户转向后的实际停止边界；这些不代表新三 Tab 通过。没有重跑全站矩阵或产品业务测试。原 Agent 候选检查的自有资源已清理；本轮资源退出须由对应 owner 实际确认。
- Git 保存已有样板、harness、必要说明和最小审查图；原始截图、trace与日志在忽略的 `output/ai/` 下，不通过 Git 跨设备恢复。旧机器输出和绝对路径不作为当前可用资源证明，复现与资源退出以对应 owner 的实际记录为准。

## 保留的既有恢复入口

M3 明确范围已正式交付 main；接受范围、原失败和未覆盖边界继续见 [M3 卡](../docs/development/work-items/m3-knowledge-memory-collaboration.md)、[独审报告](../docs/development/work-items/m3-core-independent-review.md)及[任务台账](../docs/development/agent-team/tasks.md)。本轮设计工作不扩大其接受范围，也不解除原 STOP。

- Model 原测试和 helper 的恢复说明见 [Model harness](m3-model-runtime/reproduction/README.md)与[原工作卡](../docs/development/work-items/m3-model-runtime.md)。既有 compile-only 结果不等于本轮执行测试。
- 固定检索选型、排名、来源和 bootstrap 见[检索恢复入口](m3-retrieval-selection/README.md)；公开向量沿用 [runtime-embeddings-v2](m3-model-runtime/runtime-embeddings-v2/manifest.json) 的 gzip。Meeting SQL 测试和已审截图的位置沿上述正式工作卡恢复。
- 远端 `origin/ai/task-comments-20261013` 继续保留，保存尚未合入 main 的“不改变任务状态的独立追加评论”API、迁移、服务、UI 与测试，完整任务未交付；本轮不接管、不清理，也不改写其旧失败或验收事实。恢复该任务须另核分支实际代码、差异与会话归属，不能把其旧 `current.md` 当作本轮状态。

M3 后继缺口、完整 Lifecycle/全平台 Runner、生产 SPA/部署恢复和完整 D24/D25 token 流仍按原卡评估；`update-meeting`、`comment-meeting` 的可写语义仍未定义、未注册、未接受。不自动进入 M4、E01 或生产发布。
