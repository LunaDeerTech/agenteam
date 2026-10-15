# 当前状态：F0 因用户实看反馈重开返修

2026-10-15，用户实看后未接受现有 F0 的信息结构与视觉品质：项目菜单出现额外项、项目设置结构和下拉样式不正确，层次关系不清，与 Linear 的整体完成度差距明显，动效和质感不足。**F0 已重开，视觉系统数值待重新校准；F1—F8 未开始，正式产品未迁移。** 既有技术与交互检查的通过事实按当时输入保留，不能据此继续称设计已最终接受或用户已认可。

## 本轮分支与职责

- 基线：`b6db5297`；任务分支：`ai/frontend-f0-refinement-20261015`；当前工作树：`/workspace/agenteam-f0-refinement`。原 F0 工作树已移除，不能沿旧目录恢复。
- 唯一活动写会话为 `/root`，root 负责全部 Git 与集成。general 第二稿已远端保存；本轮限定产出继续保存在此任务分支，暂不合并 main，main 仍为 `b6db5297`。最终源码由第二稿、纯格式整理及共享菜单滚动竞态修复组成，连同本轮说明由 root 统一 checkpoint/push。恢复按[状态说明](README.md)核对分支最新提交与本地差异。
- `/root/f0_refinement_designer` 负责设计与样板实现、正式设计正文；`/root/f0_structure_audit` 只读核对结构；`/root/f0_refinement_review` 负责新的独立审查；`/root/f0_test` 更新作者侧 harness；`/root/f0_docs` 独写本文件、任务台账顶部及 `prototype/README.md`。

## 当前断点与下一步

按用户要求完成技能与公开实践调研后，已将可执行流程归入[前端重设计技能](../.agents/skills/agenteam-frontend-redesign/SKILL.md#从一个完整页面建立方法)与[来源说明](../docs/development/agent-team/skill-sources.md#前端重设计方法的本地适配)。本轮限定产出为方法落实与项目设置「基本信息」单页示范；[目标设计 §8](../docs/frontend-design/redesign/target-design.md#8-f0-可实现视觉值与组件使用决定)仍保留待重校的原样板数值。

**general 第二稿已完成限定内部视觉与交互检查，待用户实看反馈。** 后续从任务分支的最终 general 候选继续：保留 `f0138630` 第二稿布局，包含后继纯格式整理与 `core.js` 菜单滚动竞态修复；只有纯格式批次已证语义等价，最终源码须包含该修复。静态 HTTP 入口为 `index.html?page=settings&fixture=baseline&scenario=normal&theme=light`，启动见[样板说明](../docs/frontend-design/redesign/prototype/README.md#启动与参数)。使用正式 Vue 同一 Atlas fixture；11个真实设置栏目按源码顺序保留，只有 general 本轮深入实现，Agent 基础配置/Memory 与 MCP 沿既有样板可达，其余8项为静态栏目文字。旧11类模板未获得本轮整体验收，Provider 等仍待质量返修；`task-pages.js` / `task.css` 未接入并保留 WIP。

独审确认第二稿结构、浅深整页、菜单与改名确认的限定视觉结果通过，首稿3项视觉缺陷已关闭；独立交互检查覆盖 normal/reduce 下的描述取消/保存、改名取消/确认、草稿返回、菜单键盘回焦和390窄屏栏目/末尾记录可达。原生200%补查发现的迟到滚动通知误关菜单已修复，原链3次复验均通过；正常 Projects 菜单滚动关闭与键盘行为、独审窄范围复验也已通过。作者定向结果与原测试前提失败、修正输入后同断言通过的事实见[测试说明](../docs/frontend-design/redesign/prototype/test-README.md)，独审范围见[独审入口](../docs/frontend-design/redesign/prototype/review/README.md)。正式 Vue 的基线检查仍只证明同内容输入；这些内部结果不代表用户接受、全设置覆盖或完整 F0 已完成。

下一步供用户从任务分支本地预览此示范，结合实看反馈继续 F0 返修，再处理其他模板与视觉值；不自动推广或进入 F1。实现继续限隔离样板及必要文档/harness，不修改产品 `web/src/`、后端或业务契约。

后续分别报告“已修订内容”和实际内部检查结果；没有新的用户认可时，状态明确为“已修订/内部审查结果，待用户实看反馈”。内部检查通过不替代用户对信息结构与视觉品质的接受，不自动推进 F1。

## 材料与检查边界

- [样板说明](../docs/frontend-design/redesign/prototype/README.md)保存启动、参数、现有 API 映射与合成边界；页面与参数会随本轮实际实现同步。
- [作者检查](../docs/frontend-design/redesign/prototype/test-README.md)与[独立审查](../docs/frontend-design/redesign/prototype/review/README.md)保留旧输入的技术、交互、截图与失败复验事实，并分别记录本轮基线、新页与独审结果。本轮完成的是 general 限定内部检查；本轮未运行产品业务测试。实现、test 与独审均已停写；作者5207预览服务已主动终止并实际 join，test 自有服务/浏览器/profile、review5227及浏览器均已关闭，测试用 `web/node_modules` 软链已移除。
- Git 保存已有样板、harness、必要说明和最小审查图；原始截图、trace与日志在忽略的 `output/ai/` 下，不通过 Git 跨设备恢复。旧机器输出和绝对路径不作为当前可用资源证明，复现与资源退出以对应 owner 的实际记录为准。

## 保留的既有恢复入口

M3 明确范围已正式交付 main；接受范围、原失败和未覆盖边界继续见 [M3 卡](../docs/development/work-items/m3-knowledge-memory-collaboration.md)、[独审报告](../docs/development/work-items/m3-core-independent-review.md)及[任务台账](../docs/development/agent-team/tasks.md)。本轮设计工作不扩大其接受范围，也不解除原 STOP。

- Model 原测试和 helper 的恢复说明见 [Model harness](m3-model-runtime/reproduction/README.md)与[原工作卡](../docs/development/work-items/m3-model-runtime.md)。既有 compile-only 结果不等于本轮执行测试。
- 固定检索选型、排名、来源和 bootstrap 见[检索恢复入口](m3-retrieval-selection/README.md)；公开向量沿用 [runtime-embeddings-v2](m3-model-runtime/runtime-embeddings-v2/manifest.json) 的 gzip。Meeting SQL 测试和已审截图的位置沿上述正式工作卡恢复。
- 远端 `origin/ai/task-comments-20261013` 继续保留，保存尚未合入 main 的“不改变任务状态的独立追加评论”API、迁移、服务、UI 与测试，完整任务未交付；本轮不接管、不清理，也不改写其旧失败或验收事实。恢复该任务须另核分支实际代码、差异与会话归属，不能把其旧 `current.md` 当作本轮状态。

M3 后继缺口、完整 Lifecycle/全平台 Runner、生产 SPA/部署恢复和完整 D24/D25 token 流仍按原卡评估；`update-meeting`、`comment-meeting` 的可写语义仍未定义、未注册、未接受。不自动进入 M4、E01 或生产发布。
