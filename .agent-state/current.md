# 当前状态：从零重建 Vue 前端，框架与路由骨架进行中

2026-10-16，用户明确启动新的 Vue 工程：旧 `web/` 归档为 `web-crape/`，新 `web/` 从零建立。新工程不复用旧 API 客户端、Session、权限或业务实现，必要后端接口随后续具体页面任务重新设计；当前不代表后端已重建或认证授权已经具备。路线已由 HTML 样板阶段转为 Vue 重建，不再等待全站 F0 完成。

## 本轮分支与职责

- 基线：`746ddcb3`；任务分支：`ai/frontend-vue-rebuild-20261016`；当前工作树：`/workspace/agenteam-vue-rebuild`。`main` 仍为 `b6db5297`；旧 `ai/frontend-f0-refinement-20261015` 在 `746ddcb3` 保留为设计对照，尚未合入 main，不再作为本轮主工作树。
- 唯一活动协调会话为 `/root`，负责全部 Git、工作树、保存推送与集成。root 已执行 `web/` → `web-crape/` 重命名并暂存，当前等待保存该里程碑；新 `web/` 正在建立，尚未确认独立启动或验收通过。恢复须按[状态说明](README.md)核对本分支最新提交和未提交差异，不改旧树的 `current.md`。
- `/root/agent_completion_designer` 负责新工程、router/layouts/views 与集成；`/root/vue_foundation_ui` 负责共享 UI/styles/theme；`/root/f0_structure_audit` 负责根工具兼容与只读契约核对；`/root/agent_completion_test` 负责验证；`/root/f0_refinement_review` 独立审查；`/root/f0_docs` 独写本轮获授权文档及恢复状态。框架紧耦合，各源文件保持唯一写者，共用当前新工作树。

## 当前范围与下一步

- 先建立共享 UI、AppShell、单一 SettingsShell 和全部已定路由骨架，先从系统设置推进。未讨论的页面正文只保留标题与空内容区域，不自行填充旧表单、假数据或业务动作。
- `web-crape/` 只作归档与历史参照，不修改其内部，也不将旧客户端、会话、权限或业务代码移回新工程。新页面的产品规则与接口需逐项确认，旧接口名称不是新合同。
- Agent HTML 样板保留对照，不继续扩功能；本轮不迁入完整 Agent 或项目列表正文。原 F0 内部测试与视觉结果保留其旧输入含义，不代证新 Vue 工程。
- 下一步先由 root 保存重命名与本状态，再按实现者给出的组件/布局/路由契约归位文档，验证新工程独立启动、路由骨架和共享 UI，最后记录实际测试与独审结论。当前尚无新工程通过、系统设置正文完成或用户接受结论。

## 文档与材料边界

当前正式入口见[前端开发说明](../docs/development/frontend/README.md)、[重设计入口](../docs/frontend-design/redesign/README.md)、[开发计划](../docs/development/development-plan.md#当前开发目标与里程碑)与[任务台账](../docs/development/agent-team/tasks.md)。这些入口将按本轮新路线更新；旧“仅重构展示、保留旧客户端、F1—F8 一律未开始”的当前态文字不再决定本轮实施方式。

历史样板与检查继续留在[原型说明](../docs/frontend-design/redesign/prototype/README.md)、[测试记录](../docs/frontend-design/redesign/prototype/test-README.md)和[独审记录](../docs/frontend-design/redesign/prototype/review/README.md)，不重写历史事实。原始输出仍在忽略的 `output/ai/`，Git 不保存进程或本机输出；本轮启动命令、实际运行与资源退出待各 owner 报告后归位。

## 保留的既有恢复入口

M3 明确范围已正式交付 main；接受范围、原失败和未覆盖边界继续见 [M3 卡](../docs/development/work-items/m3-knowledge-memory-collaboration.md)、[独审报告](../docs/development/work-items/m3-core-independent-review.md)及[任务台账](../docs/development/agent-team/tasks.md)。本轮设计工作不扩大其接受范围，也不解除原 STOP。

- Model 原测试和 helper 的恢复说明见 [Model harness](m3-model-runtime/reproduction/README.md)与[原工作卡](../docs/development/work-items/m3-model-runtime.md)。既有 compile-only 结果不等于本轮执行测试。
- 固定检索选型、排名、来源和 bootstrap 见[检索恢复入口](m3-retrieval-selection/README.md)；公开向量沿用 [runtime-embeddings-v2](m3-model-runtime/runtime-embeddings-v2/manifest.json) 的 gzip。Meeting SQL 测试和已审截图的位置沿上述正式工作卡恢复。
- 远端 `origin/ai/task-comments-20261013` 继续保留，保存尚未合入 main 的“不改变任务状态的独立追加评论”API、迁移、服务、UI 与测试，完整任务未交付；本轮不接管、不清理，也不改写其旧失败或验收事实。恢复该任务须另核分支实际代码、差异与会话归属，不能把其旧 `current.md` 当作本轮状态。

M3 后继缺口、完整 Lifecycle/全平台 Runner、生产 SPA/部署恢复和完整 D24/D25 token 流仍按原卡评估；`update-meeting`、`comment-meeting` 的可写语义仍未定义、未注册、未接受。不自动进入 M4、E01 或生产发布。
