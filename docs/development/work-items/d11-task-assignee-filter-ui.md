# D11 当前 Sprint 的 Agent 筛选

状态：实现中，尚未运行本片前端检查或真实浏览器验收。

沿[任务布局 §3](../../frontend-design/layouts/tasks.md#3-kanban-栏目与字段)与[Work Owner HTTP 查询合同](d11-work-owner-http.md#3-查询投影与有界分页)，在现有任务搜索中增加“筛选 Agent”。选项为全部 Agent、未指派及当前项目正式 Agent 目录；每批 50 项，可明确加载更多。标签沿目录 display_name/name，不从 Task 首屏推测可选 Agent。

筛选草稿在“搜索”或 Enter 后应用，与关键词、优先级、类型作服务端 AND 查询。全部省略 `assignee_agent_id`，未指派编码查询字面量 `null`，指定 Agent 编码其真实 ID；客户端验证每个返回摘要的 assignee 与已捕获筛选一致。七列重新从第一页读取，继续分页沿已应用条件；只改草稿不改变原 cursor 的条件。“清除筛选”同时清空四项。

Agent 目录读取不改变已应用筛选。第一页没有已选 Agent 不是删除证明；同项目保留草稿和已应用项的标签，真实身份、Project 或 Sprint 切换沿原代际清理，当前 Project Get 明确拒绝访问也清除已选材料。目录 cursor 失效清除旧目录链，只提供明确重读，不自动换条件或重试；普通传输失败保留旧 cursor。分页按钮消失或重读结束后，在原上下文且用户没有移走焦点时返回 Agent 选择器。

复用原 Session GET 协调、Project/Tasks controller、UiSelect/UiField 与主题 tokens；不新增 Task/Agent 写入、指派或执行行为，不新增后端或 API 契约。生产范围为 `client.ts`、`work-review.ts`、`useProjectTasks.ts`、`ProjectTasksView.vue`。单元沿 `web/src/tests` 检查三态 wire、错误响应、原 Session/Workspace 分页与身份失效、真实控件及焦点；浏览器独立作者复用原 Task shell 的真实 Agent/Work 准备和默认 App。

验证结果在实际执行后补充。受控前端检查不代真实服务权限、持久事实或浏览器验收；本片不完成完整 Explore、规划操作、运行调度、默认 Registry 或 E01，既有 STOP 保持。
