# D11 Task 人工阻塞新增与解除

状态：实现与定向测试已写，待独立源码审查、前端检查和真实浏览器验收；未声明通过。

沿[Task 布局](../../frontend-design/layouts/tasks.md#6-操作与业务限制)、[规划界面 §4](d11-work-owner-planning-ui.md#4-页面行为与可写范围)、[Blocker 服务](d11-task-blocker-service.md)和[正式 Owner HTTP](d11-work-owner-http.md)，为 active Project 中当前 Human Owner 提供未指派 backlog Task 的 `waiting_for_human` 新增与解除。父 Sprint 必须为 planned/current。其它 Task 与已完成 Sprint 保留只读信息；本片不增加 rely_on 目标选择、technical 解除、状态流转、指派或排序。

“添加人工阻塞”只接受原始说明及固定 `type=waiting_for_human, metadata={}`。说明允许空或空白，最多 1024 UTF-8 字节，沿正式文本控制字符与 Unicode 校验，不 trim/NFC。“解除人工阻塞”只出现在未解除的人工阻塞记录上；解除说明恰为空字符串时显式发送 null，非空纯空白拒绝，其余非空原文最多 1024 UTF-8 字节。

复用 `POST /api/v1/projects/{P}/tasks/{T}/blockers`、`/blockers/resolve` 和 `/blocker-commands/lookup`。API 捕获不可变输入，校验 Task/Blocker scope、类型、原 ID/说明/解除备注、Task version 恰加一、未指派 backlog、时间关联与单事件回执。Lookup 使用 `{status,receipt}`；Task ID 留在路径，不伪造 Structure Lookup 外壳或 target_id。

`auth.workTaskBlockers` 复用原 Work Review 唯一 Session intent/request owner，新增 ID 与 key 仅在首次提交生成；保留原 expected_version、完整输入、身份及请求实际退出所有权。Unknown 仅显式查询原结果；not_observed/in_progress 不授权重发。当前对象拒绝访问清除对应人工阻塞草稿和原意图，另一对象拒绝不清旧操作。真实身份切换失效原结果，同身份 checking 不伪造已确认会话。页面按 Project/Task/身份与请求代际隔离迟到结果。

成功或原 Lookup 确认之后，独立重读 Task、现有 `status=all` 阻塞列表、时间线和原条件七列；丢弃旧 Task version 绑定的阻塞 cursor、时间线和任务后续页链，不用历史 receipt 替代当前事实。解除记录继续显示，Task 状态/指派/排序保持，Ready 仍需另一明确操作。版本拒绝保留说明及原版本，重新读取不自动采用新版本，取消后可重新填写。共享 UiField/UiTextarea/UiButton 与主题 tokens；打开聚焦说明，取消回原触发按钮，确认并完成当前回读后回新增入口。未保存输入沿原离开确认，Unknown 隐藏写入表单。

产品范围为 `web/src/api/client.ts`、`work-review.ts`、新 `work-task-blockers.ts`、`composables/useSession.ts`、`useProjectTasks.ts`、`components/work/TaskReviewDetail.vue` 与新 `TaskHumanBlockerForm.vue`。定向测试为 `web/src/tests/task-human-blocker-{client,session,page}.spec.ts` 和小型共用 fixture，覆盖正式 transport、原命令恢复、备注、权限/状态、冲突、迟到隔离、当前回读及回焦；测试编写不等于已运行。

独立浏览器作者沿既有真实 Work HTTP shell 准备宽屏浅色与窄屏深色两个正常闭环：新增人工阻塞、原 Ready 提交被阻止、取消解除零写、显式解除；核两次 Blocker 命令、Task version 加二、历史事实、零自动状态流转及另一 Project 不变。该 shell 不冒称默认 App、Project 创建/全生命周期或真实 Unknown 故障矩阵。实际结果待运行后填写，旧 FAIL 与 STOP 保留。

Worktree 按 sparse-first 物化小源，共享锁定的不可变依赖；数据库、浏览器 profile、缓存、dist 与输出任务私有，所有检查由统一运行者执行，不复制大归档、node_modules 或其它树的测试二进制。
