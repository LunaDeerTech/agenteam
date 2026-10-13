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

新增客户端、Session、实际 `TaskReviewDetail` 交互用例覆盖闭集/作用域校验、请求尾、权限拒绝、分页与游标失效、独立详情读、显式重试、导航迟到响应和键盘焦点。源码首稿尚待统一 runtime 窗口执行类型/定向测试及真实默认 App 浏览器验证；未声称通过。

当前没有 Runtime 投影生产者或公开快照/订阅端点。本片的执行快照 ID 是启动输入的持久引用，不是 Runtime Snapshot，也不代表模型输出、工具过程、运行时事件流或完整 D22/D25 已完成。
