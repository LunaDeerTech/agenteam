# 当前状态：F0 因用户实看反馈重开返修

2026-10-15，用户实看后未接受现有 F0 的信息结构与视觉品质：项目菜单出现额外项、项目设置结构和下拉样式不正确，层次关系不清，与 Linear 的整体完成度差距明显，动效和质感不足。**F0 已重开，视觉系统数值待重新校准；F1—F8 未开始，正式产品未迁移。** 既有技术与交互检查的通过事实按当时输入保留，不能据此继续称设计已最终接受或用户已认可。

## 本轮分支与职责

- 基线：`b6db5297`；任务分支：`ai/frontend-f0-refinement-20261015`；当前工作树：`/workspace/agenteam-f0-refinement`。原 F0 工作树已移除，不能沿旧目录恢复。
- 唯一活动写会话为 `/root`，root 负责全部 Git 与集成。上一状态检查点 `d04c0bd6` 已推远端；下述中间源码仍待 root 保存，不能把旧检查点当作已包含这些改动。后续按[状态说明](README.md)核对实际保存结果。
- `/root/f0_refinement_designer` 负责设计与样板实现、正式设计正文；`/root/f0_structure_audit` 只读核对结构；`/root/f0_refinement_review` 负责新的独立审查；`/root/f0_test` 更新作者侧 harness；`/root/f0_docs` 独写本文件、任务台账顶部及 `prototype/README.md`。

## 当前断点与下一步

用户再次指出这是第三轮仍然粗糙的设计，要求先寻找合适技能、学习他人 AI/Codex 设计实践。root 已暂停样式实现，转为两路有界调研；设计者已停写。现有样板及[目标设计 §8](../docs/frontend-design/redesign/target-design.md#8-f0-可实现视觉值与组件使用决定)中的数值仍是待重校输入，不是已获用户认可的设计基准。

当前保留的是未完成 WIP：`prototype/app.js`、`base-pages.js`、`core.js`、`shared.css` 有中间修改；新增 `task-pages.js`、`task.css` 尚未接入；设置链接已指向 `settings`，对应页面尚未实现。当前仅做过 `node --check`，没有新的浏览器检查或设计通过结论，不能把此状态称为可用新版样板。

下一步先完成技能与公开实践调研，形成可执行的设计方法和参考，再验证一个完整页面的结构、视觉与交互；单页结果成立后再安排推广范围。实现继续限隔离样板及必要文档/harness，不修改产品 `web/src/`、后端或业务契约。

后续分别报告“已修订内容”和实际内部检查结果；没有新的用户认可时，状态明确为“已修订/内部审查结果，待用户实看反馈”。内部检查通过不替代用户对信息结构与视觉品质的接受，不自动推进 F1。

## 材料与检查边界

- [样板说明](../docs/frontend-design/redesign/prototype/README.md)保存启动、参数、现有 API 映射与合成边界；页面与参数会随本轮实际实现同步。
- [原作者检查](../docs/frontend-design/redesign/prototype/test-README.md)与[原独立审查](../docs/frontend-design/redesign/prototype/review/README.md)保留旧输入的技术、交互、截图与失败复验事实。本轮修改后仅复用未受影响证据，新审查另明确输入和实际结论；本轮尚未运行新的页面检查或产品测试。
- Git 保存已有样板、harness、必要说明和最小审查图；原始截图、trace与日志在忽略的 `output/ai/` 下，不通过 Git 跨设备恢复。旧机器输出和绝对路径不作为当前可用资源证明，复现与资源退出以对应 owner 的实际记录为准。

## 保留的既有恢复入口

M3 明确范围已正式交付 main；接受范围、原失败和未覆盖边界继续见 [M3 卡](../docs/development/work-items/m3-knowledge-memory-collaboration.md)、[独审报告](../docs/development/work-items/m3-core-independent-review.md)及[任务台账](../docs/development/agent-team/tasks.md)。本轮设计工作不扩大其接受范围，也不解除原 STOP。

- Model 原测试和 helper 的恢复说明见 [Model harness](m3-model-runtime/reproduction/README.md)与[原工作卡](../docs/development/work-items/m3-model-runtime.md)。既有 compile-only 结果不等于本轮执行测试。
- 固定检索选型、排名、来源和 bootstrap 见[检索恢复入口](m3-retrieval-selection/README.md)；公开向量沿用 [runtime-embeddings-v2](m3-model-runtime/runtime-embeddings-v2/manifest.json) 的 gzip。Meeting SQL 测试和已审截图的位置沿上述正式工作卡恢复。
- 远端 `origin/ai/task-comments-20261013` 继续保留，保存尚未合入 main 的“不改变任务状态的独立追加评论”API、迁移、服务、UI 与测试，完整任务未交付；本轮不接管、不清理，也不改写其旧失败或验收事实。恢复该任务须另核分支实际代码、差异与会话归属，不能把其旧 `current.md` 当作本轮状态。

M3 后继缺口、完整 Lifecycle/全平台 Runner、生产 SPA/部署恢复和完整 D24/D25 token 流仍按原卡评估；`update-meeting`、`comment-meeting` 的可写语义仍未定义、未注册、未接受。不自动进入 M4、E01 或生产发布。
