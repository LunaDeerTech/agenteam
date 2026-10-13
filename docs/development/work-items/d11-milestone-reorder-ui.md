# D11 Milestone 排序界面

2026-10-13，实现候选，尚未完成独立审查、前端检查及真实浏览器验收。不作为完整 D11/D27 或 E01 完成证据。

当前 Human Owner 可在任务页面所选 Milestone 下打开“调整 Milestone 顺序”，明确选择同项目的某个 Milestone 之前或组尾，再确认操作。候选来自正式无筛选 Milestone 列表，每页 50 项，提供上一页和下一页；当前对象不作为锚点。不提供客户端 rank、上移/下移推测或跨项目排序，不修改 Task 筛选。

调用现有 `POST /api/v1/projects/{P}/milestones/{M}/reorder`，保留字符串 `expected_version`。指定锚点发送 `{request:{before_id}}`；组尾发送 `{request:{}}`，不发送 null。Session 在首次请求前冻结原 Project、目标、版本、request presence、key 和身份/CSRF。完整 Structure receipt 校验 command、目标、changed/version/event 后确认，随后独立 Get/List 当前事实；读取失败只恢复读取，不重发排序。

版本冲突保留原版本和排序选择，用户须明确重读当前对象、采用当前版本并重新选择位置，之后再次确认。候选分页失效清除旧链和锚点，提供“从首页重新读取”，不自动降回首屏。普通翻页不会隐含排序或在跨页选择中猜测邻居。

Unknown 沿已交 Milestone 编辑切片只提供用户触发的原 Structure Lookup，严格使用 `state/result`；in_progress/not_observed 保持待决，不自动轮询、换 key、换版本或重发。checking 隐藏页面并保留原意图；真实身份/CSRF 变化和绑定对象/Owner 明确失权清理私有材料。切对象或离开沿原 Work guard；确认离开不清除 Session 内未决原意图，返回原对象可继续查证。

复用正式表单、UiSelect、UiButton 与主题 tokens。打开后聚焦表单标题，取消回到原排序按钮，确认并读回后聚焦当前 Milestone 标题。前端受控测试覆盖 wire、原 Session 互斥与恢复、实际 Workspace/Tasks 恢复、用户表单、分页、冲突与回读；执行结果和真实浏览器范围待验收后填写。

正式来源：[规划 UI 卡](d11-work-owner-planning-ui.md#4-页面行为与可写范围)、[Work Structure](d11-work-structure.md)、[OpenAPI](../../../api/openapi/work-planning.json)、[任务布局](../../frontend-design/layouts/tasks.md)。本片无后端、迁移或新故障机制；既有 ToolsHTTP、Object 网络探针、SPA concurrent publication、KnowledgeMove 及固定外源 STOP 保持。
