# D11 同 Milestone 内的 Sprint 排序

状态：限定验收通过，本批文档待本地合入；GitHub 认证失效，尚未远端保存。

沿[任务布局 §3](../../frontend-design/layouts/tasks.md#3-kanban-栏目与字段)、[规划界面 §4](d11-work-owner-planning-ui.md#4-页面行为与可写范围)和[Structure 排序合同](d11-work-structure.md#2-数据编码与有限排序)，在现有任务页面为选中的 planned/current Sprint 提供“调整 Sprint 顺序”。用户明确选择同 Milestone 内的真实候选之前或组尾，不推测跨页相邻位置；completed Sprint 不开放新排序，不移动 Sprint 的父级或 Task，不启动、完成迭代或改变 Current 指针。

复用正式 `POST /api/v1/projects/{P}/sprints/{S}/reorder` 和 Structure Lookup。提交保留字符串 `expected_version` 与原 `milestone_id`，`before_id` 省略表示组尾，不发送 null 或 caller rank。客户端冻结输入，校验完整历史回执的 command、Project、Sprint、原父、版本、生命周期和 event。Session 在发送前保存原 key、完整输入和身份；Unknown 只查询原意图，not_observed/in_progress 不授权再次排序。确认回执之后独立 Get/List 当前 Sprint 与顺序，回读失败只重试读取。

候选来自原 Milestone 的独立 50 项分页，保持服务端顺序和 opaque cursor。换页清除旧锚点，失效 cursor 只允许明确从首页重读；候选链不与侧栏混用。版本冲突保留原版本和选择，重新读取后由用户明确采用当前版本并重新选择；当前或父对象拒绝访问清除对应草稿/原意图，不以另一个 Project/Sprint 读取拒绝清除旧操作。导航、身份与迟到响应沿现有代际隔离。

复用 UiField、UiSelect、UiButton 与主题 tokens。打开聚焦表单标题，取消回原入口，completed 时回 Sprint 标题；确认并完成当前回读后回 Sprint 标题。与现有规划写操作互斥，保留其它 Sprint 的查看与原操作返回入口。

产品范围为 `web/src/api/client.ts`、`work-sprint-planning.ts`、`composables/useSession.ts`、`useProjectTasks.ts`、`views/projects/ProjectTasksView.vue` 与新 `components/work/SprintReorderForm.vue`。定向单元为 `web/src/tests/sprint-reorder-{client,session,page,recovery}.spec.ts`，覆盖真实 codec/Session/Workspace、当前与历史事实分离、同父分页、版本冲突、Unknown 原查询、权限与身份失效、迟到结果及共享控件回焦。真实浏览器使用默认 App 的正式 HTTP，原准备 owner 完成交接，public Run 与原 TLS handler 均实际返回；Runtime 保持 disabled，不代完整平台或生命周期验收。

[frontend-basic01](../../../output/ai/sprint-reorder-ui-delivery/frontend-basic01/result.json) 的 10 个测试文件共 73 项全部通过，其中本片新增 29 项；同时完整类型检查因新 Session 测试给 Sprint Start 的空 request 误加 milestone_id 而报 TS2322。只将这一个测试参数修正为正式空对象，保留 busy 互斥断言，生产代码未变。[frontend-basic02](../../../output/ai/sprint-reorder-ui-delivery/frontend-basic02/result.json) 的完整类型检查和唯一受影响用例通过，另外三项为筛除，原 73 项结果复用，不累加成 74 项；原 FAIL 保留。[首次构建](../../../output/ai/sprint-reorder-ui-delivery/build01/result.json)通过，产出 84 个 dist 文件。

受测源码 `c7c1bb14` 的 `TestSprintReorderWeb` [native01](../../../output/ai/sprint-reorder-ui-delivery/native01/result.json) whole PASS：1 top/2 sub 共 67.34s（planned 33.87s、current 33.47s），两个原 Node 调用均成功返回；外层原 `71094/69304f` Wait 0、157.975s。两组各有 53 个其它同父 Sprint 候选，实际选择第二页锚点、移到组尾及组尾 no-op；各恰三次排序、两次 changed、零浏览器 Lookup。原 Current 指针、另一 Milestone 的非空 Task 及其历史、其它组与 Project B 事实保持，见[原链日志](../../../output/ai/sprint-reorder-ui-delivery/native01/chain.log)。

[退出核验](../../../output/ai/sprint-reorder-ui-delivery/native01/post.json)闭合原 Wait、14 个 native PID、六个自有资源的两轮 absence、三个私有目录及 runtime、descendants、Host TCP 双尾，受测输入未漂移。[四张实际截图审阅](../../../output/ai/sprint-reorder-ui-delivery/native01/visual-review.json)仅覆盖 1440×960 浅色 planned 与 390×844 深色 current 各自的 before/confirmed-noop 状态：第二页候选、确认和标题回焦可见，窄屏表单无整页横溢；图片为实际滚动位置，不称完整响应式或无障碍矩阵。Unknown、completed 拒绝、失权及冲突恢复沿受控单元范围接受，本轮未注入真实浏览器故障矩阵。

唯一测试二进制在编译及原消费者共 17 个 PID 均退出后，按原路径、大小与 SHA 核实并精确删除；[处分凭证](../../../output/ai/sprint-reorder-ui-delivery/compile01/binary-disposition.json)确认 103,752,260 B 文件路径已不存在，原日志、FAIL 和四图保留。

本片不扩大为跨 Milestone 移动、Task 写入、完整 Explore、完整 D11/D27 或默认生命周期验收；既有 STOP 与历史失败保留。共享工具/锁定依赖复用，候选输出及测试可写状态保持任务私有，不复制大归档或其它 worktree 的测试二进制。
