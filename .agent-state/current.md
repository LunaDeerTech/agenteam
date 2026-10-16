# 当前状态：F0 因用户实看反馈重开返修

2026-10-15，用户实看后未接受现有 F0 的信息结构与视觉品质：项目菜单出现额外项、项目设置结构和下拉样式不正确，层次关系不清，与 Linear 的整体完成度差距明显，动效和质感不足。**F0 已重开，视觉系统数值待重新校准；F1—F8 未开始，正式产品未迁移。** 既有技术与交互检查的通过事实按当时输入保留，不能据此继续称设计已最终接受或用户已认可。

## 本轮分支与职责

- 本轮补齐基线：`b52b8c43`；任务分支：`ai/frontend-f0-refinement-20261015`；当前工作树：`/workspace/agenteam-f0-refinement`。原 F0 工作树已移除，不能沿旧目录恢复。
- 唯一活动写会话为 `/root`，root 负责全部 Git 与集成。用户已授权在现有候选上补齐 Agent 设置的明确产品目标，并将 Reusable Approvals 独立为第4个“审计”Tab；最近已确认远端检查点为 `46805284`；其后的首版接入仍为 WIP，新结果尚未验收。本轮继续使用 `ai/frontend-f0-refinement-20261015`，暂不合并 main，main 仍为 `b6db5297`；恢复按[状态说明](README.md)核对分支最新提交与本地差异，不能把在制改动当已远端保存。
- `/root/agent_completion_designer` 负责基础配置、能力资源与样板集成；`/root/f0_structure_audit` 转为 Memory/审计模块实现；`/root/agent_completion_test` 负责本轮测试，`/root/f0_refinement_review` 独立审查；`/root/f0_docs` 独写本文件、任务台账顶部、`prototype/README.md`，以及 `page-blueprints.md`、`target-design.md`、`implementation-plan.md` 本轮对应规则。原型紧耦合，按文件唯一写者共用本工作树，Git 仍只由 root 执行。

## 当前断点与下一步

技能与公开实践已归入[前端重设计技能](../.agents/skills/agenteam-frontend-redesign/SKILL.md#从一个完整页面建立方法)与[来源说明](../docs/development/agent-team/skill-sources.md#前端重设计方法的本地适配)。用户已实看 general 并要求去除名称/描述外层框，限定开放布局修正已完成；这不代表用户已认可整个示范。[目标设计 §8](../docs/frontend-design/redesign/target-design.md#8-f0-可实现视觉值与组件使用决定)仍保留待重校的原样板数值。

**Agent 四 Tab 首版 WIP 已接入，当前进入作者浏览器检查，尚未完成本轮验证。** 基础配置、列表、Preset、资源行为与新 catalog 已落盘，Memory/审计独立模块已接入；同一对象的审计入口为 `?page=agents&agent=agent-reviewer&view=audit`，此处仅确认路由契约，不代表浏览器通过。 基本配置独立手动保存/取消，资源按完整操作逐项保存，记忆逐条操作；审计查看当前 Agent 的可复用授权来源、Tool、Scope、active/revoked 与时间，并仅撤销有效规则，不是设置变更日志，不手工新建或编辑 Scope。切 Tab 保留基本草稿，资源、记忆和审计的成功结果不被基本配置取消撤销；创建仍为两个配置 Tab。

本轮补齐列表模型/派生忙碌摘要/筛选分页与时间、Preset 复制及初始化结果、模型候选与推理强度、Tools/Core、挂载四字段、Secret/Skills 完整浏览、Memory 必要字段和完整历史，以及操作结果恢复闭环。完整契约归[项目设置蓝图](../docs/frontend-design/redesign/page-blueprints.md#53-项目设置11-个叶子)，不以当前 Vue/API 的接入程度删减目标。Agent 删除生命周期与手工 Memory 纠正流程仍未定，不由本轮补造。共享对象页头、独立返回、列表位置/焦点恢复与独立保存边界继续保留；MCP 不扩对象结构。
此前 general 的正常/减少动效、窄屏、菜单与原生200%竞态修复等限定证据见[测试说明](../docs/frontend-design/redesign/prototype/test-README.md)与[独审入口](../docs/frontend-design/redesign/prototype/review/README.md)，只按未受影响的原输入复用。旧11类模板没有本轮整体验收；11个设置栏目中其余8叶仍无本轮业务样板，`task-pages.js` / `task.css` 仍未接入并保留 WIP。

下一步先调整 Memory/审计的正常浏览密度，再完成作者浏览器检查并交受影响测试与独审。首版分页仍属在制输入，最终容量与专用检查参数待实际落地核对，不提前写成可用或已验证；文档依据实际结果收口。此前三 Tab 的限定视觉、保存隔离和原生200%证据保留原输入含义，不能代证本轮四 Tab 或补齐项。入口与当前可运行边界见[样板说明](../docs/frontend-design/redesign/prototype/README.md#启动与参数)。Agent 使用依据产品字段构造的合成对象，不声称与旧正式 Vue 同输入对照。实现仍限隔离样板及必要文档/harness，未推进产品 `web/src/`、后端或 F1—F8；F0 全局未定稿，设计值未冻结，尚未取得用户整体认可。

## 材料与检查边界

- [样板说明](../docs/frontend-design/redesign/prototype/README.md)保存仓库内启动入口、参数、现有 API 映射与合成边界；需取得本任务分支，单独更新 main 没有本轮候选。
- [作者检查](../docs/frontend-design/redesign/prototype/test-README.md#三-tab-与独立保存的最终定向检查)与[独立审查](../docs/frontend-design/redesign/prototype/review/README.md)保留此前三 Tab 的实际输入、未受影响证据复用与定向复验；原错误预期和历史中断记录保留，异 scope 回调仅获静态核对，不冒充跨项目浏览器验收。没有重跑全站矩阵或产品业务测试。此前对应检查的自有服务、浏览器/context、临时 profile 与软链已按 owner 记录清理；本轮新增检查和资源退出尚待各 owner 报告，不预写通过或全部退出。
- Git 保存已有样板、harness、必要说明和最小审查图；原始截图、trace与日志在忽略的 `output/ai/` 下，不通过 Git 跨设备恢复。旧机器输出和绝对路径不作为当前可用资源证明，复现与资源退出以对应 owner 的实际记录为准。

## 保留的既有恢复入口

M3 明确范围已正式交付 main；接受范围、原失败和未覆盖边界继续见 [M3 卡](../docs/development/work-items/m3-knowledge-memory-collaboration.md)、[独审报告](../docs/development/work-items/m3-core-independent-review.md)及[任务台账](../docs/development/agent-team/tasks.md)。本轮设计工作不扩大其接受范围，也不解除原 STOP。

- Model 原测试和 helper 的恢复说明见 [Model harness](m3-model-runtime/reproduction/README.md)与[原工作卡](../docs/development/work-items/m3-model-runtime.md)。既有 compile-only 结果不等于本轮执行测试。
- 固定检索选型、排名、来源和 bootstrap 见[检索恢复入口](m3-retrieval-selection/README.md)；公开向量沿用 [runtime-embeddings-v2](m3-model-runtime/runtime-embeddings-v2/manifest.json) 的 gzip。Meeting SQL 测试和已审截图的位置沿上述正式工作卡恢复。
- 远端 `origin/ai/task-comments-20261013` 继续保留，保存尚未合入 main 的“不改变任务状态的独立追加评论”API、迁移、服务、UI 与测试，完整任务未交付；本轮不接管、不清理，也不改写其旧失败或验收事实。恢复该任务须另核分支实际代码、差异与会话归属，不能把其旧 `current.md` 当作本轮状态。

M3 后继缺口、完整 Lifecycle/全平台 Runner、生产 SPA/部署恢复和完整 D24/D25 token 流仍按原卡评估；`update-meeting`、`comment-meeting` 的可写语义仍未定义、未注册、未接受。不自动进入 M4、E01 或生产发布。
