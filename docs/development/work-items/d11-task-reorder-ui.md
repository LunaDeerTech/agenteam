# D11 未指派待规划任务同组排序

状态：实现候选，新增 14 项基础测试与全前端类型检查已通过；独立审查、直接旧回归及真实浏览器验收尚未完成，不表示完整 D11 完成。

## 已定范围与调用

当前 Human Owner 在任务详情中调整未指派 backlog Task 的顺序。仅限同一 Sprint、state、priority 分组；Sprint 为 planned/current，Project 为 active。正式依据为 [Task 规划](d11-task-planning.md) §2、§4 和 [Work Owner HTTP](d11-work-owner-http.md)，界面沿 [任务布局](../../frontend-design/layouts/tasks.md)。

用户明确选择移动到真实候选之前或分组末尾。候选读取现有 Task List，固定 Sprint/backlog/priority，独立服务端分页，不继承看板的文本、类型或 Agent 筛选；目标自身不可选。前端不生成 rank、不移动跨组成员、不本地交换卡片。

通过现有 `POST /api/v1/projects/{project_id}/tasks/{task_id}/reorder` 提交原 `expected_version` 与 `{before_id?}`；省略锚点表示尾部。Lookup 使用现有 `task-commands/lookup`、完整 `work.task.reorder` 意图。Session 沿原 Work command owner 保存 key/body/身份与真实请求尾，界面不保存到 URL 或浏览器持久存储。

提交结果未知时仅允许显式查询原操作，不自动重发。明确版本/状态拒绝保留原选择，重新读取后须明确采用当前版本，再确认新排序。候选 cursor 失效要求从首页重新读取。确认成功或 no-op 后以真实 Get/List 刷新详情、历史和看板；刷新失败保留已确认事实，只重读。当前 Project、Task 或 Session 变化拒绝迟到响应。

## 实现与必要验证

- 专属 API：`web/src/api/work-task-reorder.ts`；复用 Session Work intent 的 `reorder` 分支。
- 页面：`useProjectTasks.ts`、真实 `TaskReviewDetail.vue` 与 `TaskReorderForm.vue`，复用 Ui 控件、现有路由离开确认和原操作查询入口。
- 基础测试源：`task-reorder-client.spec.ts`、`task-reorder-session.spec.ts`、`task-reorder-page.spec.ts`，覆盖封闭 wire、before/tail/no-op、单意图和 Unknown Lookup、版本采用、分页失效、权限/生命周期和迟到响应。
- 已执行：新 API/Form/3 测试源限定格式，新 14/14 项基础测试、全前端类型检查通过。
- 尚未执行：受影响旧 Task 回归、构建、真实浏览器和持久顺序读回。运行由共享资源 owner 安排；基础通过不替代真实系统完成。
- 浏览器源码：`tests/projectvariable/task_reorder_web_test.go` 和 `tests/account-captcha-web/task-reorder.config.js`、`e2e/task-reorder.{helpers,spec}.ts`。正常场景验证前移、尾部、no-op；恢复场景先真实并发排序产生版本冲突，再显式采用新版、真实成功响应交付失败、仅 Lookup 原意图。复用原默认 App、原退出链和既有被动 reader/Session 观察；源码尚未运行。

真实浏览器须验证现有 Work 后端与正式页面共同工作，包括前移及尾部、真实服务端顺序/版本读回、明确查询恢复、浅深色/窄屏、键盘选择和焦点恢复。引用既有后端验收，不重复无变化的全套领域矩阵。
