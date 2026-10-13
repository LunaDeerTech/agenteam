# D11 Task 依赖建立、解除与目标查看

状态：产品有限独审与前端检查通过；按 `task-dependency-owned-v1` 接受 native04 的业务与自有资源退出范围。native04 原整轮 Wait 1、whole_pass=false 与全局 Host TCP 门失败保留，未产生新的整轮通过记录；本批结果待文档审查与本地合入。

沿[Task 布局 §5–6](../../frontend-design/layouts/tasks.md#5-共享-task-详情)、[规划界面 §4](d11-work-owner-planning-ui.md#4-页面行为与可写范围)、[Blocker 服务](d11-task-blocker-service.md)和[正式 Owner HTTP](d11-work-owner-http.md)，补齐当前 Human Owner 对 active Project、planned/current Sprint 中未指派 backlog Task 的 `rely_on` 建立、解除和目标查看。该闭环为 [E01 协作验收](platform-game-acceptance.md)所需依赖事实提供用户入口，不代表真实 Execution、模型执行或 E01 已完成。

“添加任务依赖”显式选择当前 Project 的另一 Task。独立候选入口复用正式 `GET /api/v1/projects/{P}/tasks`，固定每页 50、可选 text 和独立 cursor；不发送 sprint_id/state，不放宽原看板七列的必需条件。完整页校验 Project、ID 唯一性和 `sprint_id → state.Order → priority → manual_rank/ID` 顺序，跨页重复/乱序整页拒绝。搜索输入只在明确查找时应用，继续读取使用已应用条件；重新查找清除选中值，续页保留明确选择。自身不作为选项，不在前端拉全图或自行判环。done/cancelled 目标仍可显式建立 unresolved 依赖，不自动满足或解除。

Add 使用原 `POST /tasks/{T}/blockers`，固定 `type=rely_on`、`metadata={related_task_id}`；Session 首次提交前生成 blocker ID 和 key，捕获原 expected_version、说明和目标。Resolve 使用原 `/blockers/resolve`，wire 仅 `blocker_id,resolution_comment`；原 related ID 私下随意图冻结，回执须仍为同一 rely_on/目标，不临时猜类型或向 wire 增添字段。空解除说明发送 null，其余保留原文并验证非空白与 1024 UTF-8 字节。Unknown 仅显式原 key/命令/完整输入 Lookup，使用正式 status/receipt；not_observed/in_progress 不授权重发。

“查看关联任务”先按同 Project Get 读取目标真实归属，再沿原脏输入/Unknown 离开确认进入 Task 路由，由原页面加载其真实 Milestone/Sprint。权限拒绝清除当前候选、草稿和对应意图；Project/Task/身份代际隔离迟到回包。已知拒绝保留原草稿与版本，重新读取不自动采用新版本。确认成功后重读 Task、status=all 阻塞、时间线和原条件七列，丢弃旧 cursor；不以历史回执替代当前 Get。取消回原触发器；拒绝后原入口不可用时回“重新读取任务”，成功且当前回读完成后回新增入口；搜索或续页回候选选择器。

产品范围为 `web/src/api/client.ts`、`work-review.ts`、新 `work-task-dependencies.ts`、`composables/useSession.ts`、`useProjectTasks.ts`、`components/work/TaskReviewDetail.vue` 与新 `TaskDependencyForm.vue`。必要纯前端验证为 `web/src/tests/task-dependency-{client,session,page}.spec.ts` 及小型 fixture，覆盖闭合 transport、原意图/Unknown、目录分页、冲突、权限、迟到隔离、真实父归属、刷新与焦点。产品与测试经非作者有限审查接受，所有检查由统一运行者执行；浏览器由独立作者实现并经非作者审查。

[basic01](../../../output/ai/task-dependency-ui-delivery/basic01/result.json) 的 10 个 spec、75 个不同用例全部通过；同轮完整类型检查因新测试夹具的嵌套 union 缩小报 TS2345。仅修 fixture：按原 expected_version 和已缩小的 request 显式构造 resolve 参数，不改产品、断言或运行值。[basic02](../../../output/ai/task-dependency-ui-delivery/basic02/result.json) 完整类型检查与覆盖 Add/Resolve 的唯一必要用例通过，另四项筛除；复用原 75 项，不累计成 76 项。[首次构建](../../../output/ai/task-dependency-ui-delivery/build01/result.json)通过，84 个 dist 文件共 1,530,535 B。原类型失败记录保留。

受测浏览器组合 `4b2651be` 的 [native04](../../../output/ai/task-dependency-ui-delivery/native04/result.json) 业务 Go 1 top/1 sub 通过（51.67s/51.66s），原 Node 一次调用通过（23.5s）。真实 default App、正式 Owner HTTP 与公开 facade 中，53 个同 Project 有序候选跨两页，明确选择另一 Sprint 的 B；A→B 建立、查看 B 后反向建立被真实 `409 TASK_DEPENDENCY_CYCLE/not_committed` 拒绝、返回 A 明确解除。三个原 POST 中恰两次提交，A version 1→2→3；反向拒绝前后完整可变账本相同，历史依赖/Task 事件与原版本核对，解除后独立当前 Get 保持 version 3。取消、读取和被拒绝命令不产生 Transfer；Task 排序/内容、Project/Agent 及 B/Q 对照事实不变。原 HTTP body/EOF/Close、handler 返回和公开投影沿严格断言核验，见[原业务链](../../../output/ai/task-dependency-ui-delivery/native04/chain.log)。Project 由正式服务与持久测试 Skills receipt 准备，非 Project 创建 HTTP 或生产 Skills 验收；Runtime disabled，真实模型请求为零。

前三轮失败均保留：[native01](../../../output/ai/task-dependency-ui-delivery/native01/result.json) 的私有观测 helper 局部变量 original 遮蔽外层快照，触发 TDZ；仅重命名该局部绑定，原 Go 二进制与 dist 复用。[native02](../../../output/ai/task-dependency-ui-delivery/native02/result.json) 在循环拒绝的 Problem 投影处失败；[native03](../../../output/ai/task-dependency-ui-delivery/native03/result.json) 只增加有限失败诊断，实证 409、not_committed、request ID、响应完整退出和账本不变均满足，错误仅为测试误期待完整请求路径的 Problem.instance。经正式 Account HTTPBoundary 合同核对，Go/helper 窄修为精确 `/api/v1`，保留全部拒绝与零提交断言，再得到 native04 业务通过；不把前三轮改记为通过。

[native04 退出记录](../../../output/ai/task-dependency-ui-delivery/native04/post.json)仍为原 `19524/d42b49` Wait 1、181.394s，whole_pass=false。9 个原 PID、六个自有容器/网络的两轮 absence、三个私有目录、runtime 与两轮 descendants 均闭合，受测输入未变；全局 Host TCP 两轮门未通过，不能把业务 PASS 写成 whole PASS。后续只读 inode 核对证明其中 ESTABLISHED 行属于现有 Codex 控制进程；另一 TIME_WAIT 行已消失、无法补证归属，不以事后消失追认原门通过。原全局门结果不改写；没有重新执行这条业务链或删除未知行。

运行者另冻结仅针对精确 `^TestTaskDependencyWeb$` 的新方法 `task-dependency-owned-v1`，见[独立 scope assessment](../../../output/ai/task-dependency-ui-delivery/launcher-scope01/scope-assessment.json)。该方法保留原 75 秒全局 TCP 采样及全部行，将全局增量及未归属行列为独立诊断；业务结果、本轮进程/资源退出、输入一致性、读取异常与中断继续作为硬门。原方法在其他 selector 上的行为和历史验收卡契约保持。有限 Python 合成验证检查实际尾部与判定代码，覆盖业务失败、资源未退出、输入变化、中断及观察错误；没有新 Go/Node、真实业务重跑或新的原 Wait。新方法及 scope assessment 已获非作者独审接受：按 `task-dependency-owned-v1` 接受既有 native04 的业务/自有资源/输入范围。这是单独的范围结论，不追认原整轮 PASS，也不声称一次新运行通过。

[四图实看记录](../../../output/ai/task-dependency-ui-delivery/visual-review.json)覆盖 1440×960 浅色的依赖建立/循环拒绝，以及 390×844 深色的循环草稿/解除后状态。宽屏图显示关联操作、保留原因、明确循环错误与重读提示；窄屏循环图仅展示可滚动抽屉上部，错误文字在该截图视口之外，不据图宣称错误可见。解除图显示已解除标记、原说明、关联任务与解除备注。四个实际视图未见横向溢出或文本重叠，不扩为完整视口、主题、Unknown 或撤权竞争矩阵。完整事实见[summary-v2](../../../output/ai/task-dependency-ui-delivery/summary-v2.json)。

[最后一份测试二进制回收记录](../../../output/ai/task-dependency-ui-delivery/compile03/binary-disposition.json)确认 compile03 与 native04 原调用退出、12 个原 PID 均消失后，按路径、105,964,537 B 大小与 SHA 精确删除，路径已不存在。稳定归档 v2 增补已接受的范围评估、验证及回收记录；v1 原件保持，包含原失败和原整轮 Wait 1，没有第五轮业务执行。

不新增后端/迁移/自动 done recovery，不接 technical 解除或跨 Project 依赖，不改既有 STOP。Worktree 使用 sparse-first 小源和共享只读依赖；缓存、dist、数据库和浏览器目录任务私有，不复制大归档、node_modules 或其它树二进制。
