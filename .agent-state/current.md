# 当前状态：F0 因用户实看反馈重开返修

2026-10-15，用户实看后未接受现有 F0 的信息结构与视觉品质：项目菜单出现额外项、项目设置结构和下拉样式不正确，层次关系不清，与 Linear 的整体完成度差距明显，动效和质感不足。**F0 已重开，视觉系统数值待重新校准；F1—F8 未开始，正式产品未迁移。** 既有技术与交互检查的通过事实按当时输入保留，不能据此继续称设计已最终接受或用户已认可。

## 本轮分支与职责

- 本轮调整基线：`c4c4c9d7`；任务分支：`ai/frontend-f0-refinement-20261015`；当前工作树：`/workspace/agenteam-f0-refinement`。原 F0 工作树已移除，不能沿旧目录恢复。
- 唯一活动写会话为 `/root`，root 负责全部 Git 与集成。此前 general 与开放布局修正已远端保存；本轮继续使用此任务分支，暂不合并 main，main 仍为 `b6db5297`。新范围由 root 统一 checkpoint/push，恢复按[状态说明](README.md)核对分支最新提交与本地差异。
- `/root/f0_refinement_designer` 负责设计与样板实现，本轮不写正式设计正文；`/root/f0_structure_audit` 只读核对结构；`/root/f0_refinement_review` 负责独立审查；`/root/f0_test` 更新作者侧 harness；`/root/f0_docs` 独写本文件、任务台账顶部、`prototype/README.md`，以及 `page-blueprints.md`、`target-design.md`、`implementation-plan.md` 本轮对应规则。

## 当前断点与下一步

技能与公开实践已归入[前端重设计技能](../.agents/skills/agenteam-frontend-redesign/SKILL.md#从一个完整页面建立方法)与[来源说明](../docs/development/agent-team/skill-sources.md#前端重设计方法的本地适配)。用户已实看 general 并要求去除名称/描述外层框，限定开放布局修正已完成；这不代表用户已认可整个示范。[目标设计 §8](../docs/frontend-design/redesign/target-design.md#8-f0-可实现视觉值与组件使用决定)仍保留待重校的原样板数值。

**当前实施范围：统一已实现项目设置页的页头，并仅将 Agent 改为列表进入独立详情。** 已确认规则为：header、title、description 与正文起点统一，页头使用宽区域，较窄表单靠左对齐，不恢复表单外围框。Agent 首次进入只显示列表，不默认选中对象、不与详情并排；选择后进入独立详情，再返回列表。MCP 本轮只调整页头，列表结构待 Agent 模式获得用户确认后再扩散。以上是本轮已确认方向，尚无新实现完成或接受结论。

此前 general 的正常/减少动效、窄屏、菜单与原生200%竞态修复等限定证据见[测试说明](../docs/frontend-design/redesign/prototype/test-README.md)与[独审入口](../docs/frontend-design/redesign/prototype/review/README.md)，只按未受影响的原输入复用。旧11类模板没有本轮整体验收；11个设置栏目中其余8叶仍无本轮业务样板，`task-pages.js` / `task.css` 仍未接入并保留 WIP。

下一步由设计者完成上述范围并明确实际路由/返回行为，再按影响进行自查与独审；文档 owner 随实际结果一次归位正式规则及运行边界。当前仅更新恢复状态，不预写完成、用户认可或新预览参数。实现继续限隔离样板及必要文档/harness，不修改产品 `web/src/`、后端或业务契约；F0 全局未定稿，F1—F8 未开始。后续分别说明已修订内容、限定内部结果与用户反馈，不以内部检查替代用户认可。

## 材料与检查边界

- [样板说明](../docs/frontend-design/redesign/prototype/README.md)保存启动、参数、现有 API 映射与合成边界；页面与参数会随本轮实际实现同步。
- [作者检查](../docs/frontend-design/redesign/prototype/test-README.md)与[独立审查](../docs/frontend-design/redesign/prototype/review/README.md)保留旧输入的技术、交互、截图与失败复验事实，并分别记录本轮基线、新页与独审结果。此前完成的是 general 限定内部检查，本轮新范围尚未形成检查结论；未运行产品业务测试。此前批次的服务、浏览器及测试软链已退出/清理，本轮资源状态须由对应 owner 实际确认，不沿用旧批次的全部退出结论。
- Git 保存已有样板、harness、必要说明和最小审查图；原始截图、trace与日志在忽略的 `output/ai/` 下，不通过 Git 跨设备恢复。旧机器输出和绝对路径不作为当前可用资源证明，复现与资源退出以对应 owner 的实际记录为准。

## 保留的既有恢复入口

M3 明确范围已正式交付 main；接受范围、原失败和未覆盖边界继续见 [M3 卡](../docs/development/work-items/m3-knowledge-memory-collaboration.md)、[独审报告](../docs/development/work-items/m3-core-independent-review.md)及[任务台账](../docs/development/agent-team/tasks.md)。本轮设计工作不扩大其接受范围，也不解除原 STOP。

- Model 原测试和 helper 的恢复说明见 [Model harness](m3-model-runtime/reproduction/README.md)与[原工作卡](../docs/development/work-items/m3-model-runtime.md)。既有 compile-only 结果不等于本轮执行测试。
- 固定检索选型、排名、来源和 bootstrap 见[检索恢复入口](m3-retrieval-selection/README.md)；公开向量沿用 [runtime-embeddings-v2](m3-model-runtime/runtime-embeddings-v2/manifest.json) 的 gzip。Meeting SQL 测试和已审截图的位置沿上述正式工作卡恢复。
- 远端 `origin/ai/task-comments-20261013` 继续保留，保存尚未合入 main 的“不改变任务状态的独立追加评论”API、迁移、服务、UI 与测试，完整任务未交付；本轮不接管、不清理，也不改写其旧失败或验收事实。恢复该任务须另核分支实际代码、差异与会话归属，不能把其旧 `current.md` 当作本轮状态。

M3 后继缺口、完整 Lifecycle/全平台 Runner、生产 SPA/部署恢复和完整 D24/D25 token 流仍按原卡评估；`update-meeting`、`comment-meeting` 的可写语义仍未定义、未注册、未接受。不自动进入 M4、E01 或生产发布。
