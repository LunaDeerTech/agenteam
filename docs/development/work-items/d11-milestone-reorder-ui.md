# D11 Milestone 排序界面

2026-10-13，本切片已完成独立审查、前端检查及限定真实浏览器验收，受测源码 `8729aec9`。不作为完整 D11/D27 或 E01 完成证据。

当前 Human Owner 可在任务页面所选 Milestone 下打开“调整 Milestone 顺序”，明确选择同项目的某个 Milestone 之前或组尾，再确认操作。候选来自正式无筛选 Milestone 列表，每页 50 项，提供上一页和下一页；当前对象不作为锚点。不提供客户端 rank、上移/下移推测或跨项目排序，不修改 Task 筛选。

调用现有 `POST /api/v1/projects/{P}/milestones/{M}/reorder`，保留字符串 `expected_version`。指定锚点发送 `{request:{before_id}}`；组尾发送 `{request:{}}`，不发送 null。Session 在首次请求前冻结原 Project、目标、版本、request presence、key 和身份/CSRF。完整 Structure receipt 校验 command、目标、changed/version/event 后确认，随后独立 Get/List 当前事实；读取失败只恢复读取，不重发排序。

版本冲突保留原版本和排序选择，用户须明确重读当前对象、采用当前版本并重新选择位置，之后再次确认。候选分页失效清除旧链和锚点，提供“从首页重新读取”，不自动降回首屏。普通翻页不会隐含排序或在跨页选择中猜测邻居。

Unknown 沿已交 Milestone 编辑切片只提供用户触发的原 Structure Lookup，严格使用 `state/result`；in_progress/not_observed 保持待决，不自动轮询、换 key、换版本或重发。checking 隐藏页面并保留原意图；真实身份/CSRF 变化和绑定对象/Owner 明确失权清理私有材料。切对象或离开沿原 Work guard；确认离开不清除 Session 内未决原意图，返回原对象可继续查证。

复用正式表单、UiSelect、UiButton 与主题 tokens。打开后聚焦表单标题，取消回到原排序按钮，确认并读回后聚焦当前 Milestone 标题。前端受控测试覆盖 wire、原 Session 互斥与恢复、实际 Workspace/Tasks 恢复、用户表单、分页、冲突与回读；[14 个测试文件的 75 项测试及完整类型检查](../../../output/ai/milestone-reorder-ui-delivery/frontend-basic01/result.json)、[生产构建](../../../output/ai/milestone-reorder-ui-delivery/build01/result.json)均通过，浏览器复验复用这些未变输入的结果。

真实 `TestMilestoneReorderWeb` 的 [native02](../../../output/ai/milestone-reorder-ui-delivery/native02/result.json) 原 `77366/f6f2d3` whole PASS：1 top/2 sub 共 68.53s（normal 35.54s、responsive 32.98s），两个 Node 原调用成功返回，外层 Wait 0/156.693s。实际 normal 覆盖 53 个候选、跨页 before、无变化重排及组尾，三次排序写入中两次 changed，B 不变；responsive 零业务写入。[原日志](../../../output/ai/milestone-reorder-ui-delivery/native02/chain.log)与[退出核验](../../../output/ai/milestone-reorder-ui-delivery/native02/post.json)保留：14 个原 PID、六个自有资源、三个私有目录及 runtime/子进程/TCP 尾均已清退，受测输入未漂移。[九张截图审阅](../../../output/ai/milestone-reorder-ui-delivery/native02/visual-review01.json)覆盖 normal 与 390/834/1024/1440 四宽度的浅深主题，没有本轮 must-fix；不据此宣称完整无障碍验收。

[native01](../../../output/ai/milestone-reorder-ui-delivery/native01/result.json) 的 FAIL 保留：可用空间实际降至 0 B 后受控中止，normal 已成功，responsive 未完成；原 runtime 另遗留约 9.47 MB 缓存，不能倒填 whole PASS。本轮仅为此精确 profile 省去无消费者的 PG16，复用既有 current-only 六资源链，其他 profile 的原七资源/Wait 不变；浏览器可写缓存转入原测试私有目录并随原 Node 返回后清理。native02 启动可用空间 4,047,187,968 B，每秒采样最低 1,293,742,080 B，最大观测下降 2,753,445,888 B，不是连续精确峰值。两轮单 binary 的[首轮处置](../../../output/ai/milestone-reorder-ui-delivery/compile01/binary-disposition.json)与[复验处置](../../../output/ai/milestone-reorder-ui-delivery/compile02/binary-disposition.json)均保留。

本轮浏览器未新增 Unknown/丢响应场景；该恢复语义仍属上述受控前端测试范围。浏览器 App 的 Runtime 关闭，无外部 Invoke；Project 种子的 Skills receipt 为测试前置，不冒正式 Skills claim 或 Project 创建 HTTP。未绑定完整 default Registry，不扩大平台删除或旧 STOP 范围。

正式来源：[规划 UI 卡](d11-work-owner-planning-ui.md#4-页面行为与可写范围)、[Work Structure](d11-work-structure.md)、[OpenAPI](../../../api/openapi/work-planning.json)、[任务布局](../../frontend-design/layouts/tasks.md)。本片无后端、迁移或新故障机制；既有 ToolsHTTP、Object 网络探针、SPA concurrent publication、KnowledgeMove 及固定外源 STOP 保持。
