# D11 未指派待规划任务同组排序

状态：本片实现、独立源码审查、必要基础验证与真实默认 App 浏览器验收已通过，待主线集成；不表示完整 D11 完成。

## 已定范围与调用

当前 Human Owner 在任务详情中调整未指派 backlog Task 的顺序。仅限同一 Sprint、state、priority 分组；Sprint 为 planned/current，Project 为 active。正式依据为 [Task 规划](d11-task-planning.md) §2、§4 和 [Work Owner HTTP](d11-work-owner-http.md)，界面沿 [任务布局](../../frontend-design/layouts/tasks.md)。

用户明确选择移动到真实候选之前或分组末尾。候选读取现有 Task List，固定 Sprint/backlog/priority，独立服务端分页，不继承看板的文本、类型或 Agent 筛选；目标自身不可选。前端不生成 rank、不移动跨组成员、不本地交换卡片。

通过现有 `POST /api/v1/projects/{project_id}/tasks/{task_id}/reorder` 提交原 `expected_version` 与 `{before_id?}`；省略锚点表示尾部。Lookup 使用现有 `task-commands/lookup`、完整 `work.task.reorder` 意图。Session 沿原 Work command owner 保存 key/body/身份与真实请求尾，界面不保存到 URL 或浏览器持久存储。

提交结果未知时仅允许显式查询原操作，不自动重发。明确版本/状态拒绝保留原选择，重新读取后须明确采用当前版本，再确认新排序。候选 cursor 失效要求从首页重新读取。确认成功或 no-op 后以真实 Get/List 刷新详情、历史和看板；刷新失败保留已确认事实，只重读。当前 Project、Task 或 Session 变化拒绝迟到响应。

## 实现与必要验证

- 专属 API：`web/src/api/work-task-reorder.ts`；复用 Session Work intent 的 `reorder` 分支。
- 页面：`useProjectTasks.ts`、真实 `TaskReviewDetail.vue` 与 `TaskReorderForm.vue`，复用 Ui 控件、现有路由离开确认和原操作查询入口。
- 基础测试源：`task-reorder-client.spec.ts`、`task-reorder-session.spec.ts`、`task-reorder-page.spec.ts`，覆盖封闭 wire、before/tail/no-op、单意图和 Unknown Lookup、版本采用、分页失效、权限/生命周期和迟到响应。
- 已执行：新 API/Form/3 测试源限定格式，新 14/14 项基础测试、受影响旧 Task 5 项直接回归（合计 19 项不同用例）、全前端类型检查与生产构建通过。浏览器新目标 TypeScript 源检查无诊断；已有其他 harness 诊断不在本片修复范围，不宣称全 harness 类型检查通过。
- 浏览器源码：`tests/projectvariable/task_reorder_web_test.go` 和 `tests/account-captcha-web/task-reorder.config.js`、`e2e/task-reorder.{helpers,spec}.ts`。正常场景验证前移、尾部、no-op；恢复场景先真实并发排序产生版本冲突，再显式采用新版、真实成功响应交付失败、仅 Lookup 原意图。复用原默认 App、原退出链和既有被动 reader/Session 观察。

## 真实浏览器结果与边界

`task-web-native03` 的 `TestTaskReorderWeb` 精确 1 个顶层、2 个子场景及 2 个 Node 浏览器进程通过。正常场景 3 次浏览器写请求、3 次已提交确认、2 次实际顺序变化，包含尾部 no-op；恢复场景 2 次浏览器写请求、1 次真实版本冲突、1 次显式 Lookup，连同独立并发排序共 2 次已提交、2 次实际顺序变化。正式 HTTP 读回与持久数据核验确认同组顺序和版本，其他项目数据保持不变。

恢复夹具在原排序事务成功、完整回执读取且原 TLS bridge 返回后受控截断下游交付，再由用户明确查询原 key 和完整意图。该场景证明 Unknown 恢复，不代表观察到了生产网络故障；没有自动重放写请求。逐请求 `handler_returned` 证据指原 TLS bridge 返回，原默认 App、TLS handler 和 ProcessGuard 的退出另由原夹具核验。完整执行实际 Wait 为 0；后验确认 14 个原进程、6 个精确所有资源及私有目录已退出或清理，受测源码无变化。

本轮实际查看 7 张截图：1440 浅色的第二页目标选择和键盘焦点清楚；390 深色的明确采用新版提示、排序表单、确认后按钮回焦可见，长 ID 换行，所见区域未发现页面横向溢出。`normal-390-dark-tail` 只显示表单标题，尾部选择控件不在视口；`recovery-390-dark-uncertain` 仅显示历史与重新读取，Unknown 提示和 Lookup 按钮不在视口。尾部选择与显式 Lookup 行为由实际 Playwright 断言验证，不把文件名当作对应内容的视觉证据。

保留前两轮真实失败事实：`task-web-native01` 的观察器错误地把正式 blockers 查询限定为只有 `limit=50`，修为同时要求 `status=all`；`task-web-native02` 的 Go 代理把 13 字段 Task List Summary 解码为 15 字段完整 Task，导致候选读取 502，修为严格 Summary 解码及对应持久比较。产品禁用候选和严格 wire 检查在失败时均按预期生效，修复没有放宽请求、响应、读取完成或退出门槛。引用既有后端验收，不重复无变化的全套领域矩阵。
