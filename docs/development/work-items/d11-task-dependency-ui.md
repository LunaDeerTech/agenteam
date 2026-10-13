# D11 Task 依赖建立、解除与目标查看

状态：限定实现待独立审查与运行验证；尚无本片动态通过结论。

沿[Task 布局 §5–6](../../frontend-design/layouts/tasks.md#5-共享-task-详情)、[规划界面 §4](d11-work-owner-planning-ui.md#4-页面行为与可写范围)、[Blocker 服务](d11-task-blocker-service.md)和[正式 Owner HTTP](d11-work-owner-http.md)，补齐当前 Human Owner 对 active Project、planned/current Sprint 中未指派 backlog Task 的 `rely_on` 建立、解除和目标查看。该闭环为 [E01 协作验收](platform-game-acceptance.md)所需依赖事实提供用户入口，不代表真实 Execution、模型执行或 E01 已完成。

“添加任务依赖”显式选择当前 Project 的另一 Task。独立候选入口复用正式 `GET /api/v1/projects/{P}/tasks`，固定每页 50、可选 text 和独立 cursor；不发送 sprint_id/state，不放宽原看板七列的必需条件。完整页校验 Project、ID 唯一性和 `sprint_id → state.Order → priority → manual_rank/ID` 顺序，跨页重复/乱序整页拒绝。搜索输入只在明确查找时应用，继续读取使用已应用条件；重新查找清除选中值，续页保留明确选择。自身不作为选项，不在前端拉全图或自行判环。done/cancelled 目标仍可显式建立 unresolved 依赖，不自动满足或解除。

Add 使用原 `POST /tasks/{T}/blockers`，固定 `type=rely_on`、`metadata={related_task_id}`；Session 首次提交前生成 blocker ID 和 key，捕获原 expected_version、说明和目标。Resolve 使用原 `/blockers/resolve`，wire 仅 `blocker_id,resolution_comment`；原 related ID 私下随意图冻结，回执须仍为同一 rely_on/目标，不临时猜类型或向 wire 增添字段。空解除说明发送 null，其余保留原文并验证非空白与 1024 UTF-8 字节。Unknown 仅显式原 key/命令/完整输入 Lookup，使用正式 status/receipt；not_observed/in_progress 不授权重发。

“查看关联任务”先按同 Project Get 读取目标真实归属，再沿原脏输入/Unknown 离开确认进入 Task 路由，由原页面加载其真实 Milestone/Sprint。权限拒绝清除当前候选、草稿和对应意图；Project/Task/身份代际隔离迟到回包。已知拒绝保留原草稿与版本，重新读取不自动采用新版本。确认成功后重读 Task、status=all 阻塞、时间线和原条件七列，丢弃旧 cursor；不以历史回执替代当前 Get。取消回原触发器；拒绝后原入口不可用时回“重新读取任务”，成功且当前回读完成后回新增入口；搜索或续页回候选选择器。

产品范围为 `web/src/api/client.ts`、`work-review.ts`、新 `work-task-dependencies.ts`、`composables/useSession.ts`、`useProjectTasks.ts`、`components/work/TaskReviewDetail.vue` 与新 `TaskDependencyForm.vue`。必要纯前端验证为 `web/src/tests/task-dependency-{client,session,page}.spec.ts` 及小型 fixture，覆盖闭合 transport、原意图/Unknown、目录分页、冲突、权限、迟到隔离、真实父归属、刷新与焦点。真实浏览器由独立作者实现：同 Project 跨 Sprint A→B 建立、反向环拒绝不提交、明确解除；对照 Project 原事实保持。运行者统一执行检查，未运行前不将计划写成结果。

不新增后端/迁移/自动 done recovery，不接 technical 解除或跨 Project 依赖，不改既有 STOP。Worktree 使用 sparse-first 小源和共享只读依赖；缓存、dist、数据库和浏览器目录任务私有，不复制大归档、node_modules 或其它树二进制。
