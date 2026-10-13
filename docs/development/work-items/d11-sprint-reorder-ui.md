# D11 同 Milestone 内的 Sprint 排序

状态：实现与定向测试待独立审查、运行验证；尚未验收。

沿[任务布局 §3](../../frontend-design/layouts/tasks.md#3-kanban-栏目与字段)、[规划界面 §4](d11-work-owner-planning-ui.md#4-页面行为与可写范围)和[Structure 排序合同](d11-work-structure.md#2-数据编码与有限排序)，在现有任务页面为选中的 planned/current Sprint 提供“调整 Sprint 顺序”。用户明确选择同 Milestone 内的真实候选之前或组尾，不推测跨页相邻位置；completed Sprint 不开放新排序，不移动 Sprint 的父级或 Task，不启动、完成迭代或改变 Current 指针。

复用正式 `POST /api/v1/projects/{P}/sprints/{S}/reorder` 和 Structure Lookup。提交保留字符串 `expected_version` 与原 `milestone_id`，`before_id` 省略表示组尾，不发送 null 或 caller rank。客户端冻结输入，校验完整历史回执的 command、Project、Sprint、原父、版本、生命周期和 event。Session 在发送前保存原 key、完整输入和身份；Unknown 只查询原意图，not_observed/in_progress 不授权再次排序。确认回执之后独立 Get/List 当前 Sprint 与顺序，回读失败只重试读取。

候选来自原 Milestone 的独立 50 项分页，保持服务端顺序和 opaque cursor。换页清除旧锚点，失效 cursor 只允许明确从首页重读；候选链不与侧栏混用。版本冲突保留原版本和选择，重新读取后由用户明确采用当前版本并重新选择；当前或父对象拒绝访问清除对应草稿/原意图，不以另一个 Project/Sprint 读取拒绝清除旧操作。导航、身份与迟到响应沿现有代际隔离。

复用 UiField、UiSelect、UiButton 与主题 tokens。打开聚焦表单标题，取消回原入口，completed 时回 Sprint 标题；确认并完成当前回读后回 Sprint 标题。与现有规划写操作互斥，保留其它 Sprint 的查看与原操作返回入口。

产品范围为 `web/src/api/client.ts`、`work-sprint-planning.ts`、`composables/useSession.ts`、`useProjectTasks.ts`、`views/projects/ProjectTasksView.vue` 与新 `components/work/SprintReorderForm.vue`。定向单元为 `web/src/tests/sprint-reorder-{client,session,page,recovery}.spec.ts`，覆盖真实 codec/Session/Workspace、当前与历史事实分离、同父分页、版本冲突、Unknown 原查询、权限与身份失效、迟到结果及共享控件回焦。浏览器由独立作者沿现有正式服务/HTTP fixture 准备；具体运行结果、主题与视口范围待实际结果记录。

本片不扩大为跨 Milestone 移动、Task 写入、完整 Explore、完整 D11/D27 或默认生命周期验收；既有 STOP 与历史失败保留。共享工具/锁定依赖复用，候选输出及测试可写状态保持任务私有，不复制大归档或其它 worktree 的测试二进制。
