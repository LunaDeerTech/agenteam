# 当前状态：F0 样板已独立接受，F1—F8 未开始

2026-10-15，[F0 完整页面样板与视觉系统定稿](../docs/frontend-design/redesign/implementation-plan.md#4-f0完整页面样板与视觉系统定稿)已获独立接受。11个必需页面、Knowledge 四格式/PDF及视觉值已定稿；已知独审阻塞均经定向返修和复验关闭，包括 Knowledge 原生 200% 缩放下的长正文阅读。**F1—F8 尚未开始，正式产品未迁移**；本轮未修改产品 `web/src/` 或后端，也未重验真实业务。

## 结果与恢复入口

- [样板说明](../docs/frontend-design/redesign/prototype/README.md)：启动、参数、完整页面、现有 API 映射、合成边界及 F1 接续；[目标设计 §8](../docs/frontend-design/redesign/target-design.md#8-f0-可实现视觉值与组件使用决定)保存可实施的视觉值。
- [作者检查](../docs/frontend-design/redesign/prototype/test-README.md)与[独立审查](../docs/frontend-design/redesign/prototype/review/README.md)：实际输入、前后对照、连续旅程、失败与复验、最小截图及资源退出限制。作者矩阵和定向复验按各自输入组合复用，不宣称一次全套运行；fixture 不代真实服务、权限、持久写入或恢复验收。
- 样板、harness、必要说明及最小审查图随正式提交保存。可再生原始截图、trace与日志位于忽略的 `output/ai/frontend-f0/`、`output/ai/frontend-f0-review/`；这些本机输出不通过 Git 恢复，跨设备按说明重建依赖与复现。浏览器与原生缩放/PDF观察限制沿上述检查入口，不照搬旧机器路径。

## 交付与下一步

本轮实施基线为 `d55bf044`，任务分支为 `ai/frontend-f0-20261015`，实现检查点已远端保存。唯一写会话为 `/root`；root 负责全部 Git，设计、作者测试、独立审查与状态文档分别由本轮各 owner 负责。此结果随正式提交交付 main；main 推送确认后 root 清退本轮临时分支和工作树。当前不预记推送、输出迁移或清理已完成，具体执行按[状态与恢复说明](README.md)。

下一阶段是 F1：在正式 Vue 共享基础和项目列表落实已接受设计，复用原 API/composable 并完成受影响真实链路；需后续阶段指令，不自动启动 F1 或全站迁移。

## 保留的既有恢复入口

M3 明确范围已正式交付 main；接受范围、原失败和未覆盖边界继续见 [M3 卡](../docs/development/work-items/m3-knowledge-memory-collaboration.md)、[独审报告](../docs/development/work-items/m3-core-independent-review.md)及[任务台账](../docs/development/agent-team/tasks.md)。本轮设计工作不扩大其接受范围，也不解除原 STOP。

- Model 原测试和 helper 的恢复说明见 [Model harness](m3-model-runtime/reproduction/README.md)与[原工作卡](../docs/development/work-items/m3-model-runtime.md)。既有 compile-only 结果不等于本轮执行测试。
- 固定检索选型、排名、来源和 bootstrap 见[检索恢复入口](m3-retrieval-selection/README.md)；公开向量沿用 [runtime-embeddings-v2](m3-model-runtime/runtime-embeddings-v2/manifest.json) 的 gzip。Meeting SQL 测试和已审截图的位置沿上述正式工作卡恢复。
- 远端 `origin/ai/task-comments-20261013` 继续保留，保存尚未合入 main 的“不改变任务状态的独立追加评论”API、迁移、服务、UI 与测试，完整任务未交付；本轮不接管、不清理，也不改写其旧失败或验收事实。恢复该任务须另核分支实际代码、差异与会话归属，不能把其旧 `current.md` 当作本轮状态。

M3 后继缺口、完整 Lifecycle/全平台 Runner、生产 SPA/部署恢复和完整 D24/D25 token 流仍按原卡评估；`update-meeting`、`comment-meeting` 的可写语义仍未定义、未注册、未接受。不自动进入 M4、E01 或生产发布。
