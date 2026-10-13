# Task Execution 只读详情

当前片段把 Task 详情的「执行记录」接到 Execution Owner 的只读 List/Get。后端契约由同轮 `d22-task-execution-read.md` 与 `api/openapi/execution.json` 定义；此片不实现 Runtime Snapshot、RuntimeItem、WebSocket、resync 或执行命令。

## 页面与调用

现有 `/:username/:project_name/tasks/:task_id` → `ProjectTasksView` → `TaskReviewDetail` 保持正式入口。在详情中明确点击「查看执行记录」后，查询当前 Task 的 Execution；不从 TaskEvent、Usage 或 Task 状态推断执行记录。

- 每次 List/Get 均携带真实 Project/Task 身份。列表按服务端 `created_at DESC, id DESC` 展示，每页 50 项，显式加载较早记录；不在客户端重排。
- 选择条目后独立读取 Get，显示 canonical 状态、用途、Agent/Task/Project 引用、版本、时间、可空执行快照 ID 与安全终结原因。无持久安全原因时显示「未提供」。非终态且 `cancel_requested_at` 有值显示「正在取消」，不把请求取消表示成已取消。
- 执行详情只读；执行成功或失败都不修改或推断 Task 状态。刷新与失败重试均为用户明确触发，无自动轮询、重发或执行恢复命令。
- 加载详情立即隐藏之前的详情；权限/对象拒绝清除该区已读信息。分页失效提供从头重读；整页验证失败不追加部分数据，跨页重复或逆序不会发布。
- 返回列表恢复选中条目焦点；收起恢复入口焦点；详情错误导致旧控件移除时回到仍可见的分区标题。布局沿共享主题 tokens，窄屏属性单列并允许长 ID 换行。

## 请求与数据边界

`task-executions.ts` 仅接受正式闭集 DTO，校验 Project/Task/Execution 关联、UUIDv7、int64 字符串、固定精度时间、枚举、终态与可空字段。禁止额外 Prompt、正文、policy 或模型材料。`next_cursor` 明确为字符串或 null；opaque cursor 不进入 URL 导航或持久存储。

两个只读方法复用 Session 的 `work-review-read` 请求协调者。只有原网络/body/cancel 实际退出才能释放 Session 请求 owner；页面代次与当前 Session/Project/Task/路由隔离迟到结果。Task 导航与 Session 持有取消职责，新分区不会调用另一个读取的 abandon，也不改变原写命令意图、幂等键或 Unknown 恢复。

## 验证与当前限制

客户端、Session、实际 `TaskReviewDetail` 定向交互与兼容用例合计 12 项、完整类型检查及生产构建已通过；构建共 86 个文件，真实浏览器复用同一产物。覆盖闭集/作用域校验、请求尾、权限拒绝、分页与游标失效、独立详情读、显式重试、导航迟到响应和键盘焦点。首次页面夹具失败与窄修复验分别保留于 `output/ai/task-execution-read/ui-basic01`、`ui-basic02`，原客户端/Session/兼容和类型通过结果按未变输入复用。

真实默认 App 原生验收与 `read-web-native02` 浏览器整轮均已通过，详见[后端结果](d22-task-execution-read.md)。浏览器使用正式 Coordinator/Handoff 创建的 1 条 Execution，实际完成列表、独立详情、显式刷新、加载反馈、键盘与焦点恢复、非 Owner 404 拒绝、清除旧信息并恢复；目标 8 次 GET、页面阶段零写请求，26 个原响应连同辅助读取均有完整消费及 handler 返回证明。native、Node、CDP 与自有资源均实际退出。首轮 Web/纯 `--list` 的 afterEach 参数语法失败保留，最窄夹具修复后复用原后端二进制及前端构建。

已亲看本轮四张截图：1440 浅色任务抽屉及执行详情的双列属性、标题焦点环清楚；390 深色的详情操作可见，错误态展示「未能读取执行记录」及「刷新执行列表」按钮，无可见横向溢出或控件重叠。宽屏列表图只拍到执行区标题，窄屏详情图的多数属性位于当前视口下方，不据这些截图宣称所有行均入画；完整内容与列表行为由本轮真实 DOM/响应断言证明。截图位于 `output/ai/task-execution-read/read-web-native02`，未扩展为 Runtime 输出或整条 Agent 执行环验收。

Runtime 投影生产者及公开快照/订阅端点不在本片交付范围。本片的执行快照 ID 是启动输入的持久引用，不是 Runtime Snapshot，也不代表模型输出、工具过程、运行时事件流或完整 D22/D25 已完成。
