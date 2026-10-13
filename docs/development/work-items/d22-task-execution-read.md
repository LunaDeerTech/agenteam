# Task 的 Execution 历史读取

本片把 Task 详情中的「执行记录」接到真实默认 App。它读取已有 canonical Execution，不启动 Runtime、Model、Runner 或工具，也不把 Execution succeeded 解释为 Task done。实现状态以本片实际联调结果为准；Runtime 内容与工具闭环不在本片验收范围。

## 接口与授权

正式 wire 见 [`api/openapi/execution.json`](../../../api/openapi/execution.json)。两个入口均接受 GET/HEAD：

- `/api/v1/projects/{project_id}/tasks/{task_id}/executions`：`limit` 缺省 50、范围 1–100；可选原 opaque `cursor`。返回 `{items, next_cursor}`，末页 `next_cursor` 明确为 null。
- `/api/v1/projects/{project_id}/tasks/{task_id}/executions/{execution_id}`：不接受 query，返回 `{execution}`。

每次请求经过原 Account HTTP boundary、当前 Human Session、Project Owner Read 和 Work 同 Store/live Tx 的 Task 归属检查。管理员身份不替代 Owner。归档按原 Read 门可读；初始化未完成、archiving/deleting 等状态仍由原 Project 门裁定。跨 Project、Task 或 Execution 不能因已知 ID 绕过读取权限。当前 Task 不存在时，不通过 Execution 历史反向泄露其它 Task。请求体、未知/重复/空 query、非规范 limit、Range/If-Range 和 Content-Encoding 均拒绝。

列表按 `created_at DESC, id DESC`，只读 limit+1 行决定下一页。签名游标绑定 Project、Task、稳定 Human user 与该排序，不是授权或跨页快照；较新的 Execution 通过用户显式刷新查看。每条记录包含 ID、P/Task/Agent、purpose、canonical status/version、创建/开始/完成/请求取消时间、nullable snapshot_id 与 nullable safe reason。Version 是十进制字符串；时间沿 foundation.Instant 固定六位微秒 UTC。reason 只允许 completed/model_failed/incomplete_response/runtime_failed/cancelled；没有对应 canonical Round 终态事实时为 null。单条 SQL 联读 Execution 与终态标量，校验 status/version/finished_at 一致，不读取 Snapshot 内容、模型响应、Transcript 或私有错误。

非终态且 cancel_requested_at 非空时，UI 显示「正在取消」；终态展示 canonical status。本片不提供取消操作。失败/Unknown 不输出部分结果；HEAD 同样执行原权限与查询、计算同一表示后抑制 body。响应 no-store，入口 2 秒继承更早的 deadline，仍等待原实际 Store、技术关闭及 HTTP body/flush 返回。

## 原始调用归属与默认装配

Runtime enabled 时，TaskReader 在原 Execution 四服务 pair 的首次构造中生成，共用同一 Store、Guard、process/instance、owners、service lane 与 Project Stop。Runtime disabled 时只创建历史读取 owner，完全不创建四个 driver、Model 或 Scheduler，也不暴露 Launch/retirement 权限。App 在原 Object.Initialize 成功并绑定 Guard 后、Account.Start 之前择一装配并发布 HTTP。Registry 仍保持原 nil 边界。

`list_task_executions` / `get_task_execution` 是 service lane 的独立读 identity，不使用空 LaunchLookupKey，也不伪造 migration 84 的 created origin。读登记、业务事务及 technical close 的 Unknown 保留原 attempt/cause；原 Store 返回前调用仍被拥有，确认只处理原读技术事实，不重新发布数据或启动业务。原 Request/Inspect Project Stop 共用这些 owner；全局 Stop/Drain/Joined 与 Account Force 保留 Guard/Store 直到真实返回。Work 借用口只在预持 User/P/TaskSchedule/Task 锁的同一事务检查当前 Owner 与 Task，不另开事务或创建第二 Reader。

## 验证与后继边界

定向验证已通过 15 个顶层测试、41 个子测试及 5 个包的 vet，覆盖首次构造、同 pair、固定 P/Task/游标、safe 投影、登记/业务/关闭的原 Store 返回屏障与 Unknown、Project Stop、HTTP 输入与真实请求尾。首次 Work 夹具失败及其窄修复验保留在 `output/ai/task-execution-read/go-basic01`、`go-basic02`。

真实默认 App 的 `TestTaskExecutionReadDefaultApp/enabled-owner-and-disabled-history` 已在 `read-app-native04` 通过，原命令及全部自有资源实际退出。原 Coordinator/Handoff 正式创建 1 条 Execution；Runtime enabled 共用原 pair，原 manager、Guard 与数据库退出后，同数据库及部署密钥重启 Runtime disabled，只装配历史 reader。真实 Owner/Task scope、GET/HEAD、原 Store 返回屏障和读取技术关闭均通过；Execution、Task、完整 Audit 及 Snapshot/Preparation/Model/Tool 计数保持原样，Model 调用为零。真实 `limit=1` 验证的是这 1 条记录的末页，未宣称真实数据库多页；分页与游标边界由定向控制测试互补。

前三轮 native 的完整 Audit 比较失败记录仍保留。其新增记录来自夹具正式邀请兑换后遗留材料的 `secret.delete`；现在在只读基线前调用原 Account.Recover，并绑定原创建/兑换命令、cleanup tuple、删除回执与同事务 Audit，未放宽读取及重启前后的全量比较。浏览器夹具复用同一准备步骤。

`TestTaskExecutionReadWeb/history-detail-refresh-and-owner` 已在 `read-web-native02` 整轮通过：真实页面执行 3 次 List、5 次独立 Get，包含 1 次非 Owner 的 404 拒绝与恢复，页面阶段零写请求。连同正式页面辅助读取共 26 个原响应，全部对齐原 reader/consumer、完整响应与 handler 返回；原加载屏障、完整业务事实不变、Node 及自有进程/资源退出均通过。截图与结构化结果位于 `output/ai/task-execution-read/read-web-native02`，有限视觉结论见[前端结果](d22-task-execution-ui.md)。`read-web-native01` 的启动失败和 `browser-list01` 的 Playwright 参数语法失败保留；仅将新 spec 的 afterEach 首参改为合法对象解构后复验，未改产品或放宽判据。本片的历史 List/Get 纵向验收已闭合。

真正的后继模型工具回路仍需 [`d22-round-model-call-reservation.md`](d22-round-model-call-reservation.md) 定义的 reservation 后实际 Begin/Result/Close/Invoke、原 credential/attempt/fence 与响应 checkpoint；不能用 [`d22-model-successor-contract.md`](d22-model-successor-contract.md) 的纯 schema3 分类放开现 text/schema1 门。Runtime snapshot/stream/resync 的 producer、flush 覆盖和 progress generation/revision 仍按 [Runtime View §20–21](../../architecture/agent-executor/runtime-view.md) 另行设计；本片不以 TaskEvents 或 Usage 代替。既有 ToolsHTTP、Object runtime join 等 STOP 不因新增读接口恢复。
