# D11 Task 人工阻塞新增与解除

状态：限定实现、前端检查与真实正常链已通过；本批最终文档待本地合入，GitHub 认证失效，尚未远端保存。

沿[Task 布局](../../frontend-design/layouts/tasks.md#6-操作与业务限制)、[规划界面 §4](d11-work-owner-planning-ui.md#4-页面行为与可写范围)、[Blocker 服务](d11-task-blocker-service.md)和[正式 Owner HTTP](d11-work-owner-http.md)，为 active Project 中当前 Human Owner 提供未指派 backlog Task 的 `waiting_for_human` 新增与解除。父 Sprint 必须为 planned/current。其它 Task 与已完成 Sprint 保留只读信息；本片不增加 rely_on 目标选择、technical 解除、状态流转、指派或排序。

“添加人工阻塞”只接受原始说明及固定 `type=waiting_for_human, metadata={}`。说明允许空或空白，最多 1024 UTF-8 字节，沿正式文本控制字符与 Unicode 校验，不 trim/NFC。“解除人工阻塞”只出现在未解除的人工阻塞记录上；解除说明恰为空字符串时显式发送 null，非空纯空白拒绝，其余非空原文最多 1024 UTF-8 字节。

复用 `POST /api/v1/projects/{P}/tasks/{T}/blockers`、`/blockers/resolve` 和 `/blocker-commands/lookup`。API 捕获不可变输入，校验 Task/Blocker scope、类型、原 ID/说明/解除备注、Task version 恰加一、未指派 backlog、时间关联与单事件回执。Lookup 使用 `{status,receipt}`；Task ID 留在路径，不伪造 Structure Lookup 外壳或 target_id。

`auth.workTaskBlockers` 复用原 Work Review 唯一 Session intent/request owner，新增 ID 与 key 仅在首次提交生成；保留原 expected_version、完整输入、身份及请求实际退出所有权。Unknown 仅显式查询原结果；not_observed/in_progress 不授权重发。当前对象拒绝访问清除对应人工阻塞草稿和原意图，另一对象拒绝不清旧操作。真实身份切换失效原结果，同身份 checking 不伪造已确认会话。页面按 Project/Task/身份与请求代际隔离迟到结果。

成功或原 Lookup 确认之后，独立重读 Task、现有 `status=all` 阻塞列表、时间线和原条件七列；丢弃旧 Task version 绑定的阻塞 cursor、时间线和任务后续页链，不用历史 receipt 替代当前事实。解除记录继续显示，Task 状态/指派/排序保持，Ready 仍需另一明确操作。版本拒绝保留说明及原版本，重新读取不自动采用新版本，取消后可重新填写。共享 UiField/UiTextarea/UiButton 与主题 tokens；打开聚焦说明，取消回原触发按钮，确认并完成当前回读后回新增入口。未保存输入沿原离开确认，Unknown 隐藏写入表单。

产品范围为 `web/src/api/client.ts`、`work-review.ts`、新 `work-task-blockers.ts`、`composables/useSession.ts`、`useProjectTasks.ts`、`components/work/TaskReviewDetail.vue` 与新 `TaskHumanBlockerForm.vue`。定向测试为 `web/src/tests/task-human-blocker-{client,session,page}.spec.ts` 和小型共用 fixture，覆盖正式 transport、原命令恢复、备注、权限/状态、冲突、迟到隔离、当前回读及回焦。源码及定向修正已交非作者审查接受。

[frontend-basic01](../../../output/ai/task-human-blocker-ui-delivery/frontend-basic01/result.json) 的 8 个文件共 58 项测试中 56 项通过、两项新页面 fixture 失败；完整类型检查同时指出测试误调用私有 finishLeave。仅修该 spec：使用正式公开 confirm，并用合法非空事件页建立时间线 cursor，保留并加强三类游标前后断言。[frontend-basic02](../../../output/ai/task-human-blocker-ui-delivery/frontend-basic02/result.json) 的完整类型检查及原失败两项通过，另八项为筛除；复用原 56 项，合计 58 个不同用例通过，不累加成 60 项。生产代码未因这些 fixture 问题修改。[首次构建](../../../output/ai/task-human-blocker-ui-delivery/build01/result.json)通过，产出 84 个 dist 文件。

受测业务源码 `659bc3eb`，运行入口按 `61e28abd` 固定。`TestTaskHumanBlockerWeb` 的 [native02](../../../output/ai/task-human-blocker-ui-delivery/native02/result.json) whole PASS：1 top/2 sub 共 79.72s（宽屏 31.09s、窄屏 48.63s），两个原 Node 调用均通过；外层原 `23728/959342` Wait 0、170.475s。真实 Work HTTP shell 中，两组各完成 Add→Resolve 恰两次写入、Task version 1→2→3；明确取消解除零写，原说明与空备注 null 分别验证，已解除记录留在 status=all 列表。Ready 表单在阻塞未解除时禁用，解除后启用由 Playwright 断言验证；未实际提交 Ready，零 Transfer，不据此宣称后端拒绝或调度执行已验。完整原 HTTP/公开调用投影、回执与持久历史核对，Task 状态、指派、排序及另一 Project 事实不变，见[原链日志](../../../output/ai/task-human-blocker-ui-delivery/native02/chain.log)。

首轮 [native01](../../../output/ai/task-human-blocker-ui-delivery/native01/result.json) 两组在 Add 已提交后因共享观测器缺少 receipt.blocker 的正式公开投影而失败。仅修对应 helper 投影并独审后复用同一 Go 二进制；原 FAIL、原 Wait 和退出记录保留，不回填首轮通过，也不把 harness 问题写成产品失败。

[native02 退出核验](../../../output/ai/task-human-blocker-ui-delivery/native02/post.json)确认原 Wait、14 个 native PID、六个自有资源两轮 absence、三个私有目录及 runtime、descendants 和 Host TCP 双尾闭合，输入未漂移。[四图审阅](../../../output/ai/task-human-blocker-ui-delivery/native02/visual-review.json)覆盖 1440×960 浅色与 390×844 深色的阻塞/解除后状态；运行者已逐张实际查看，文本、焦点、阻塞状态及窄屏无整页横溢。解除后的 available 图中确认按钮位于抽屉截图滚动区域之外，按钮启用由自动断言支持，不宣称截图可见。只接受这些实际视口、主题和状态，不扩大为完整响应式或无障碍矩阵。

[唯一二进制处分](../../../output/ai/task-human-blocker-ui-delivery/binary-disposition.json)记录编译及两轮消费者原调用退出后，按原路径、104,973,861 B 大小与 SHA 核实并精确删除，路径已不存在；原日志和四图保留。完整摘要见[summary](../../../output/ai/task-human-blocker-ui-delivery/summary.json)。

本片真实浏览器范围为 Work HTTP shell，未验证默认 App、Project 创建/完整生命周期、真实 Unknown/丢响应或撤权竞争矩阵。Unknown、版本冲突、planned/completed 与身份边界沿定向受控测试接受；不完成全部 Blocker 两类型/状态筛选、D11/D27。旧 FAIL 与 STOP 保持。

Worktree 按 sparse-first 物化小源，共享锁定的不可变依赖；数据库、浏览器 profile、缓存、dist 与输出任务私有，所有检查由统一运行者执行，不复制大归档、node_modules 或其它树的测试二进制。
