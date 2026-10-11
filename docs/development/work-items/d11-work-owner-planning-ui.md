# D11 Human Owner 任务规划界面

状态：rev1 草稿，2026-10-09，§10 工程选择已由负责人采纳，待独立 SPEC 审查；未实施、未运行产品验证。正式前置为 main `f1c94ee5` 的 [Work Owner HTTP/root](d11-work-owner-http.md)。本卡不以活动 Model 工作树内容作为已接受依赖。

> §1–10 保留原 Explore 规划界面的规格与当时状态；2026-10-11 新增的任务评审切片及实际验证边界见 [§11](#11-任务评审界面限定切片2026-10-11)。整张规划界面卡尚未交付，后续局部结果不回填旧轮次。

## 1. 完整结果与边界

已有 initialized Project 的当前 Human Owner 可从明确的“任务规划”入口进入 Explore：按 Milestone → Sprint → Task 浏览真实分页结构，创建/编辑 Milestone、Sprint，创建未指派 backlog Task，编辑普通字段与 Plan、调整同组顺序，并新增、分页查看、解除 `rely_on` 和无外域引用的 `waiting_for_human` Blocker。响应丢失后，通过本界面在首次发送前保存的原意图查证，必要时由用户明确同义重放；历史回执与当前对象分别展示。

消费已接受的 [Structure](d11-work-structure.md)、[Task Planning](d11-task-planning.md)、[Blocker 持久服务](d11-task-blocker-service.md)、[Owner 工作区](d27-project-owner-workspace-ui.md)及 Work HTTP 的全部 21 项能力。领域字段、权限、版本、幂等、排序及事件语义仍以上游为唯一来源；[正式 OpenAPI](../../../api/openapi/work-planning.json)是客户端编解码依据，不另设 HTTP 契约。

本卡是[任务布局](../../frontend-design/layouts/tasks.md)的限定 Explore 规划消费者。普通 `/tasks` 的 Kanban/Current Sprint 默认入口尚未实现，本卡不注册该路径、不把它重定向到 Explore、不添加假七列或失效的 Kanban 切换按钮。既定[项目工作台](../../frontend-design/layouts/project-workspace.md)普通任务入口规则保持；此次导航明确标作“任务规划”。

不包含 Project 创建/初始化 HTTP、Agent 目录/指派、Task 状态流转/跨列拖拽/跨 Sprint 移动/删除、Sprint start/complete/rollover/delete、完整 Timeline/评论、Execution、Scheduler、实时订阅或生产 SPA 发布。无新迁移或后端修改。`ready=false`/503 与 Object join、OpenAI tools、SPA publication、Jina/Image 停止项保持。fixture 的正式 Project service + test Skills 准备不证明真实 Skills/创建 HTTP 已绑定。

## 2. 文件域、隔离与接口分工

产品实施仅在 root 已创建的 `/workspace/agenteam-work-ui`、`ai/work-owner-planning-ui` 进行；本草稿当前只在活动树归位，由负责人安排单向同步。root 负责 Git，禁止同时修改两树同名源。以下是 SPEC 接受后的拟定实施域，不构成本轮产品写入授权。

### 2.1 恰六个共享产品文件

| 文件 | 必要接缝 |
| --- | --- |
| `web/src/api/client.ts` | Work 闭集端点/options/path/query 与请求、成功响应 cap；复用原 transport/EOF/cancel，不重构旧域 |
| `web/src/composables/useSession.ts` | Work 专用 facade/action revision/私有 intent，接入同一 Cookie owner、CSRF、身份失效与实际 I/O 尾部 |
| `web/src/router/auth.ts` | Explore 原始路径闭集、safeReturnTarget、Work 离开/导航通知；保留 Owner → Model 原顺序，在后追加 Work |
| `web/src/router/index.ts` | 四条受保护 Project 子路由与懒加载 View |
| `web/src/App.vue` | 持有/provide Work controller，初始导航、dispose、退出确认及 Work 对话框；checking 卸载 RouterView 时状态仍被持有 |
| `web/src/components/layout/ProjectNav.vue` | 从既有 `home` prop 派生“任务规划”链接及选中态；不新增 prop，不改变设置分组选中规则 |

`useProjectWorkspace.ts`、`ProjectWorkspaceView.vue`、`api/project-owner.ts` 只读复用：`currentReadContext`、`detail`、`readCurrent`、`paths.home` 与 `current_sprint_id` 已满足需要。工作区现有 RouterView 保留；同 Project 后缀切换与 canonicalize 已支持保留后缀，无需为本卡扩 Owner API。Model 专属源码、共享 Ui 组件、锁文件、schema、后端、迁移及公共监督器不在写域。

另有一个已授权的共享测试接缝：仅在 UI 隔离树修改 `tests/account/project_owner_web_fixture_test.go`，为新 Work fixture 增加最小可选观察/配置/IPC参数，复用原进程内 no-tag `app.Run(...)` 默认根、Account/Project准备、原反向代理、静态托管和实际join。该参数仅由Work fixture启用，不新增Go四route分类器。原默认Owner页面分类、观察/IPC/config、120s/45s预算、原7资源及退出链保持；独审必须覆盖默认未注入行为及新任务注入边界，不复制千行fixture或新增监督器。

### 2.2 新增专属源与必要文档

| 精确路径 | 职责 |
| --- | --- |
| `web/src/api/work-planning.ts` | 21 API 的 typed DTO/严格解析与输入捕获、资源 UUIDv7 生成；无页面状态/授权旁路 |
| `web/src/composables/useProjectWorkPlanning.ts` | Project/route generation、分页树、选择、编辑草稿、冲突、恢复反馈、离开保护 |
| `web/src/views/projects/ProjectWorkPlanningView.vue` | 页面布局、选择详情与子组件组合 |
| `web/src/views/projects/ProjectWorkPlanningTree.vue` | 分页层级、独立展开/选择、窄屏覆盖入口 |
| `web/src/views/projects/ProjectWorkStructureEditor.vue` | Milestone/Sprint 创建、普通字段与同组排序 |
| `web/src/views/projects/ProjectWorkTaskEditor.vue` | Task 创建、普通字段/Plan 与同组排序 |
| `web/src/views/projects/ProjectWorkBlockers.vue` | 两类 Blocker 表单、状态分页、解除与依赖目标选择 |
| `web/src/views/projects/ProjectWorkRecovery.vue` | 三域原意图进度、历史回执、查证/明确重放/放弃本地追踪 |
| `web/src/tests/work-planning-client.spec.ts` | schema/client、输入/投影、cap、完整 EOF/cancel |
| `web/src/tests/work-planning-session.spec.ts` | 唯一 owner、身份、原意图与恢复 |
| `web/src/tests/work-planning-state.spec.ts` | 树/当前值/草稿/版本/分页/迟到结果 |
| `web/src/tests/work-planning.spec.ts` | 正式 router/App、组件交互、导航/Logout/焦点 |
| `tests/account/project_work_planning_web_fixture_test.go` | 复用同包已验 Owner/native root 能力的任务私有真实 fixture |
| `tests/account/project_work_planning_web_test.go` | 作者精确真实浏览器顶层入口 |
| `tests/account-captcha-web/project-work-planning.config.js` | 精确 owned origin/private paths/case，锁定已有 Playwright |
| `tests/account-captcha-web/e2e/project-work-planning.spec.ts` | 作者真实用户流程与同请求响应/事实断言 |
| `tests/account/project_work_planning_web_independent_test.go` | 未参与实现者本人执行的独立入口 |
| `tests/account-captcha-web/e2e/project-work-planning-independent.spec.ts` | 独立恢复/身份/跨域竞争场景，独立作者持有 |
| `docs/development/frontend/README.md` | 验收后同步限定能力、精确命令与托管边界 |
| `docs/development/work-items/d11-work-owner-planning-ui.md` | 本规格及最小接受/未完成事实 |

新增辅助文件确有必要时先向负责人交精确路径；不借此建立通用 Session/表单/监督框架。现有 `authentication.spec.ts`、`session.spec.ts`、`account-client.spec.ts`、`project-owner-client.spec.ts`、`project-workspace.spec.ts`、`project-workspace-state.spec.ts` 优先原样回归，未预先授予改期待的写权。

### 2.3 可并行实施的稳定接口

`createWorkPlanningAPI(fetcher?)` 返回 `WorkPlanningAPI`：`listMilestones/getMilestone/createMilestone/updateMilestone/reorderMilestone`、对应五个 Sprint 方法、对应五个 Task 方法、`listTaskBlockers/addTaskBlocker/resolveTaskBlocker`、`lookupStructureCommand/lookupTaskCommand/lookupTaskBlockerCommand`，恰 21 方法。所有调用必须带 signal；写/Lookup 的 key、CSRF 只由 Session facade 供给。类型名可沿 Go/OpenAPI 增加 Work 前缀消歧，wire 不改名。

`createSessionController` 追加可选 Work API 依赖，不插入既有位置参数；暴露唯一 `auth.workPlanning`。为 API/Session 与页面并行固定如下消费者接口，不新造通用框架：

| 边界 | 固定内容 |
| --- | --- |
| `WorkPlanningCommand` | `domain: structure/task/blocker` 与原完整 `command` 为判别联合；共同 `projectID`，Structure/Task 非create有 `targetID`，Blocker有 `taskID`；其余恰原 mutation 的 `expected_version` presence 与 typed `request`，禁止任意字符串command或宽泛Record代替联合 |
| `captureWorkPlanningCommand(value)` | 校验并深复制/冻结上述输入；create request自带首次生成的UUIDv7。返回值不含key/CSRF/digest，不从当前Get补字段 |
| `WorkPlanningReceipt` | `domain` 判别联合，`value` 分别为正式StructureMutation/TaskMutation/TaskBlockerMutation；仅客户端内包装，wire不改 |
| `auth.workPlanning` 七项读 | 与WorkPlanningAPI同名的list/get方法，参数为稳定Project/对象ID和typed查询；signal由唯一owner内部提供，页面不另建fetch |
| `start(command)` | 接受捕获后的闭合WorkPlanningCommand，首次生成并私有保存key/CSRF/原body，执行对应十一种mutation之一，返回WorkPlanningReceipt |
| `checkOriginal()` | 仅以私有原意图选三Lookup之一，返回 `domain` 加对应正式Lookup；不得由页面传入另一份原意图/key |
| `retryOriginal()` / `abandonRead()` / `abandon()` | 分别明确重放私有原意图、退役本域读、清本域追踪；不释放尚未返回的实际Cookie owner，不修改其它域 |
| 只读 `progress` | null或 `{domain,command,projectID,targetID,phase,observation,receipt,failure,contextValid,canLookup,canReplay}`；phase恰submitting/uncertain/rejected/confirmed，observation恰none/committed/in_progress/not_observed/failed；receipt为上述联合或null，failure仅安全AccountFailure或null；无原body/key/CSRF/可变引用 |

此处 `targetID` 在progress总是本命令作用的稳定对象ID（Blocker为TaskID）；所有创建资源动作（含Blocker add）在明确提交时通过 `newWorkPlanningID()` 生成新对象ID，再捕获command。`canLookup/canReplay` 为本域上下文/实际owner/原材料/冲突条件的当前投影，不能作为服务端授权；in_progress不提供自动执行，显式继续策略遵循§5。页面不得传入任意 endpoint/command namespace/摘要/key/CSRF。一次最多一个 Work 待跟踪 mutation，不为三个域另开 Cookie 队列。API/client/useSession及client/session两测试由同一实施者持有；controller/views/App/router/Nav及state/交互测试由另一实施者持有。

`createProjectWorkPlanning(auth, workspace, replaceRoute)` 由 App 创建并以 `projectWorkPlanningKey` provide，页面通过 `useProjectWorkPlanning()` 消费。controller 公开当前阶段、树/选择/表单/分页/恢复状态与明确用户动作，并提供 `confirmLeave(target?)`、`afterNavigation(to, from)`、`dispose()` 及本域 confirmation。子 View 只通过该 controller 或显式 props/emits 操作；不用组件临时实例保存唯一原意图。

## 3. 路由、授权与选择

以下 `B` 为合法规范 `/{username}/{project_name}`，所有对象 ID 为 canonical 小写 UUIDv7。恰注册四条路径：

| 路径 | 选择 |
| --- | --- |
| `B/tasks/explore` | Explore 入口；按下面默认规则选中或保持待选 |
| `B/tasks/explore/milestones/{milestone_id}` | 指定 Milestone |
| `B/tasks/explore/sprints/{sprint_id}` | 指定 Sprint，真实读取所属 Milestone |
| `B/tasks/explore/tasks/{task_id}` | 指定 Task，真实读取所属 Sprint/Milestone |

`projectRoute` 与 `safeReturnTarget` 共同扩这个闭集：沿既有合法 Owner 名称及保留根规则，拒绝编码路径、大小写后缀替代、反斜线、空/附加段、尾斜线、query/hash/裸 `?`、非 canonical ID。登录 return 只保存合法目标路径，不夹带筛选、cursor、正文或原意图。旧静态路由优先，未知 `/tasks` 仍未实现，不做静默别名。

Project Resolve 只是候选 ID；沿现 Owner Get 成功后发布的 `currentReadContext` 才读 Work。每个异步请求/结果绑定完整 Session identity（User/Session/epoch）、稳定 ProjectID、workspace generation/readGeneration、本页 route/selection/request generation；首次列表行和 Resolve 不提供授权。后台实际 HTTP 每次重验当前 Owner，管理员无旁路。

`currentReadContext` 不包含 lifecycle，非null不等于可写。新写UI门禁必须同时读取当前匹配 `workspace.detail.project.lifecycle === 'active'`；archiving/archived保留读取及§5历史恢复，pending initialization/deleting拒绝进入。该判断不替代每次HTTP当前授权。

Explore 基础入口沿正式布局：首次当前 Get 有 `current_sprint_id` 时读取该 Sprint 及祖先；没有时保留规划结构和“请选择 Sprint”，不自动选第一个 planned/completed。用户已选择后不会被后来 current pointer 变化强制跳转。显式 typed 链接优先。默认选中采用 replace，用户切换采用正常导航记录；浏览器返回恢复该记录对象，不借旧内存跳到另一 Project。

展开只加载子列表、不改变选择；选择只读取目标/必要祖先，不暗中展开全部项目。深链接对象未在已加载页时，详情/面包屑仍可由各正式 Get 显示，树提示“所选对象不在当前已加载页”；不得把它插入未验证的 rank 位置、无限抓页寻找、伪造父子关系或总数。可明确从相应父列表首页重新浏览。Task Get 与 Sprint/Milestone Get 的 Project/parent 必须匹配；竞争导致矛盾时整组选择不发布，显示显式重读，不拼出不存在的层级。

同 Project 的对象切换也要保护本域未保存草稿；切 Milestone 清原 Sprint/Task 选择，切 Sprint 清原 Task。选中对象不可见/已失效时清敏感详情并给父级/重新选择入口。Project 改名/username 变化沿现 stable-ID Get/canonicalize；仅在当前真实同 ID/同对象的规范路径替换时保留本域状态，不把新名称 Resolve 到的新 ID 当旧对象。Project Get 失败/失权后不沿历史导航继续发子请求。

## 4. 页面行为与可写范围

桌面为规划树与详情；窄屏树用覆盖侧栏，选择后收起，保留项目身份和可返回的树按钮。只展示已绑定操作，状态文案为中文，canonical enum 仅是 wire。共用 [Ui 接口](../frontend/components.md)及[主题/交互规范](../../frontend-design/styles/README.md)，不导入 Debug。

| 对象 | 真实显示/操作 |
| --- | --- |
| Milestone | title/description/id/version/时间；创建、编辑、同 Project Milestone 组内排序，按需 Sprint 列表 |
| Sprint | title/description/父 Milestone/state/正式起止元信息/version；创建 planned Sprint、编辑；同 Milestone 内排序，按需 Task 列表；不提供跨 Milestone 迁移 |
| Task | title/description/type/priority/state/Plan、正式 parent/version/时间；创建未指派 backlog，编辑普通字段/Plan、同 Sprint/state/priority 组排序；不提供 state/assignee/parent 修改 |
| Blocker | type/description/typed metadata/创建与解决事实；仅两类 add/resolve，分页 status 切换；依赖项以真实 Task 标题/ID/状态及详情跳转呈现 |

规划 Task 树默认查询 `state=backlog&assignee_agent_id=null`，可明确筛选 text/type/priority，筛选改变回首页。直接链接到其它合法 Task 仍按正式详情只读显示，并说明不在本卡可写范围；不解析 Agent 名称、不造不存在的 Agent 目录或执行状态。Sprint completed 禁止新 Task 与结构编辑，非 active Project 禁止新 mutation；这些仅是 UI 可用性，服务端结果始终是最终判据。

title 1–256 Unicode scalar、≤1024 UTF-8 bytes、至少一非空白且拒绝 Cc；description/Task plan 各 ≤32768 UTF-8 bytes，允许 TAB/LF/CR，拒绝其它 Cc。保留空格、换行、大小写与 Unicode，不 trim/NFC，不用 JS UTF-16 length 代替 scalar/UTF-8 cap。description/plan 可明确清空；patch 只发送明确修改字段，至少一项存在，null 不代替省略。Task type/priority 均为必选，不凭后端假定默认值；编辑时来自当前值。Plan 首版以保留原文的 textarea 编辑、转义纯文本阅读，不执行 HTML/Markdown，不使用会 trim 内容的 UiComposer。

排序用“移到……之前”与“移到组尾”的明确操作，支持键盘。候选只来自真实同组、按服务端顺序加载的页面；`before_id` 缺省为组尾、不能发 null。Sprint reorder 固化当前 milestone_id；Task reorder 不接受 caller rank。过滤视图不得把相邻过滤结果伪装成完整组相邻；需要排序时打开独立的无 text/type 筛选同组分页选择。不会以“置顶/上移/下移”暗算未知跨页邻居。Task priority 编辑本身可能改变组及位置，完全按正式 receipt + 当前重读处理，不额外偷偷 reorder。

Blocker add 显式选择 `rely_on` 或 `waiting_for_human`；后者 metadata 恰 `{}`，前者恰合法 related_task_id。目标选择用本 Project 正式 Task List/Get，分页读取，可查看非 backlog 依赖目标但不向其开放写功能；排除自身的 UI 候选不替代服务端环/存在性检查，不拉全图、不自行判满足。description ≤1024 UTF-8 bytes，沿契约允许空/空白。resolve 必发 `blocker_id,resolution_comment`；只有表单值恰空字符串时代表明确无备注并发 null；非空但全空白输入报非法，不静默转null。其它非空输入必须含非空白、≤1024 UTF-8 bytes且保持原文；不把空字符串当合法 wire 备注。不得因 resolve 自行把 Task 改为 todo/恢复其它状态。

## 5. 唯一 Session owner 与原意图

所有 Work 请求经 `auth.workPlanning` 进入原 `runAuthorized` 的唯一 Cookie owner，不能在 composable 直接 fetch、另建并发队列或导出 sessionCSRF。新增 Work action 必须走 Human/Project 分支，不落入 System admin 授权。Read/Lookup/mutation 都继承现请求可见 30 秒界限；可见取消不释放实际 fetch、body reader、cancel promise 的所有权。禁止晚响应、晚 401/403、旧 finally 清新 owner 或发布旧页。

同 identity 的 Session checking 隐藏且禁用受保护内容，但 App 持有的草稿、选择与私有原意图保留；成功确认同 identity 后须以 currentReadContext 重新建立当前 Project 读取资格。真实 Logout、新 User/Session/CSRF/epoch 清除对应私有意图和页面草稿，取消旧读并等待其实际尾部；不承诺 UI 跨新 Session 或刷新恢复。后端允许同 User 新 Session 查历史的能力仍有效，本卡不把它变为浏览器存储方案。

创建资源在首次发送前生成并冻结正确 UUIDv7（48-bit Unix 毫秒、version7/variant、加密随机尾、canonical 小写），无可用 CSPRNG 时零网络拒绝；`crypto.randomUUID()` 的 v4 仅可作满足 Foundation 规则的幂等 key，不可作 Work 资源 ID。同一意图重试不重生 ID/key。保存 typed input 的深冻结快照及精确原序列化正文、字段 presence、command、稳定 Project/target、expected_version、identity/原 CSRF；用户继续编辑不会改已派发意图，不把 key/body 放 DOM、日志、storage、URL 或 history。

| 原 mutation | 捕获与 Lookup 正文 |
| --- | --- |
| Structure/Task create | mutation `{request}`；Lookup 恰 `{command,request}`，request 自带原新 ID，无 expected_version |
| Structure/Task update/reorder | mutation `{expected_version,request}`；Lookup 恰 `{command,target_id,expected_version,request}` |
| Blocker add/resolve | TaskID 在原 path；mutation `{expected_version,request}`；Lookup 恰 `{command,expected_version,request}` |

三域 Lookup 使用原 Idempotency-Key 与原 command 稳定名，当前 RequestID 只是新传输信息。客户端不计算 Go digest，不用当前 Get 的字段/version/rank 补原意图。version 始终 canonical 十进制字符串 `1..9223372036854775807`，需算术时用 BigInt，不经 Number；历史重放/no-op 不因 MaxInt64 被客户端一律拒绝。

mutation 2xx 必须收到完整合法同 Project/target 的该域 receipt 后才确认：Structure 使用 `command/changed/milestone/sprint/event_id` 严格 union，Task 使用 `task/changed/task_event_id/event_ids`，Blocker 使用正式 TaskBlockerMutation。创建资源、command、事件条件、version/输入字段可比较部分按各原契约校验；不能仅看到标题相同、任意2xx、当前 Get 或 Event ID 就成功。

Structure Lookup 恰 `{state,result}`；Task/Blocker 恰 `{status,receipt}`。committed 必有合法原 receipt；in_progress/not_observed 必须明确 null，不接受字段省略或额外键。历史回执显示“原命令已确认”，随后独立读取当前详情/受影响页；当前读失败显示“已确认，当前内容读取失败”，不撤销确认、不再自动 mutation。

首次发出后的 transport、截断、无效响应、取消、COMMIT_UNKNOWN、身份恢复失败等不能推定未提交。仅此前从未不确定、且正式 Problem 是 not_started/not_committed 的明确输入/版本/状态/容量/权限拒绝可作为首次拒绝；保留表单及安全字段错误。只凭状态码/RESOURCE_BUSY/503 不推断结果。既有不确定具有粘性；一次拒绝、not_observed 或当前读都不能清除它。IDEMPOTENCY_KEY_REUSED 保留说明并禁用原重放，不自动换 key。

恢复操作只由用户触发：“查证原命令”“按原请求重放”“放弃本地追踪”。in_progress 只表示持久准备，保留材料并允许稍后明确查证，不自动轮询/续写；not_observed 只表示本次锁定观察未见，仍不确定，可明确同义重放。有当前有效 Human Owner 读取资格、同完整 identity/CSRF、原材料齐全且非 key conflict，并且当前没有执行中的 Cookie 操作，才可重放；不额外要求 Project active。不得偷偷更新 version、补新字段或自动改新命令。用户本地放弃有确认，明确不撤销服务端副作用。

归档/归档中仍可读取、Lookup，并按原服务执行已完成原意图 replay；禁止新 mutation，不因按钮只读就隐去历史恢复。不提供“继续未完成 planned”旁路：归档后若显式原请求得到 ProjectNotActive/其它拒绝，仍按先前不确定语义保留，不将其说成回滚。pending initialization/deleting 沿 Project gate 禁入；他人 Owner/admin 不显示其内容。

## 6. 有界客户端与分页一致性

`client.ts` 增加 Work 专属 endpoint family，保持旧域默认预算。Work request 1 MiB；详情/mutation/Lookup 成功 JSON 1 MiB；Milestone/Sprint/Task 摘要与 Blocker 四种成功页 5 MiB。Problem 沿现安全解析及原 cap。成功页只含上游规定的摘要字段，详情才带 description/plan，不消费库内 64 MiB/全量列表。任一元素错误拒绝整页，绝不截断输出或过滤坏元素冒合法页。

复用当前同源 Cookie、Origin/CSRF、安全 Problem、redirect:error/no-store transport；按实际流字节而非 Content-Length 检查 cap，fatal UTF-8 完整 EOF 后才 JSON/typed parse，检查失败/取消后 await 同一个 reader.cancel 并 releaseLock，外层响应 body.cancel 也实际返回后才完成请求。不得修改已接受 reader/EOF gate 或仅凭 headers/Playwright response event 确认成功。正式 schema 的四种摘要/三域 receipt/Lookup、UUID/version/微秒时间/nullable/presence/enum/parent 关系均须严格校验；未知字段/非法值零发布。

界面页长固定50。每个父节点/筛选/Blocker status 保持独立 opaque cursor 历史与当前页，顺序完全由服务端返回；不解码/拼接/跨上下文复用 cursor。前后页为 UiButton + 当前页号，不使用要求已知总页数的 UiPagination，不捏造 total。只在已有连续合法相邻页可按原次序累积；换父/筛选/identity、新查询重启或 cursor stale/invalid 时废弃该链，显式“从首页重新读取”，不悄悄降为首屏。

Milestone/Sprint 按正式 manual_rank 顺序，Task 按正式状态/优先级/组内rank顺序；分页边界与本地筛选不得改变服务端顺序。新 mutation 确认后使受影响列表旧 cursor 链失效，再独立重读；失败保留原页面并标明陈旧，不能乐观修改业务 version/rank/事件事实。

Blocker status 默认为 unresolved，可选 resolved/all；顺序 `(created_at,id)`。服务端已把当前 Task.version 绑定到 cursor，Task 真实更新/排序、add/resolve 会使旧 cursor 失效。**HTTP 页只有 items/next_cursor，没有 Task.version 字段**：前端不得编造版本、解签游标，或声称独立 Task Get 与 Blocker 页是同事务快照。Task 编辑使用一次真实当前 Get 的 version；每次成功本地 Task/Blocker mutation或观察到新的 Task version，清该 Task 的旧 Blocker 分页链。外部并发变化由续页 CursorStale 拒绝后明确重启；显示独立读取状态，不跨失效链混页。

## 7. 离开、焦点与异常反馈

保留原 Owner → Model 导航 guard 与 App Logout 顺序，其后追加 Work。Work 只询问/清理本域 dirty、冲突和未决意图，不清 Owner/Model/System 草稿，不给其它 mutator 自动同意。各域按原确认链分别确认；任一取消停止该次导航/Logout，恢复当前合法触发焦点，不宣称多域已确认放弃能被后来取消整体回滚。切对象、切 Project、系统入口、浏览器返回都走同链；明确只读无修改的展开/翻页不触发离开。

模态用正式 UiDialog；用户关闭/取消、Escape/遮罩按同一未保存规则，旧触发已失效时可用本页真实标题 fallbackFocus。checking/失权卸载不会延迟抢新页焦点。窄屏层叠使用既有 layer manager，不改 z-index 基础设施。beforeunload 仅本域有dirty/未决时沿浏览器约束提示，不写恢复材料到持久存储。

树、详情、Blocker列表分别有 loading/empty/error/stale/unavailable，错误就地明确重读，401/CSRF与Owner失效沿Session/Project语义处理，不能把普通403全部当退出。版本冲突保原草稿/原version，当前重读单独展示并允许用户明确重新编辑后发新命令；不自动 merge 或覆写。空树逐层提供已授权的真实创建入口；无筛选结果提供清筛选；未知总数不显示“全部已加载”。

浅/深色、1440/1024/834/390宽度、reduced motion、键盘树导航/独立展开选择、Tab/Enter/Escape、长标题/最大文本换行与局部溢出均验收。readonly/background inert 清晰；截图等待实际模态/侧栏退出及布局稳定，不拿过渡中截图作最终布局接受。

## 8. 必要验收与真实资源

### 8.1 纯客户端、状态与组件

新四个 Vitest 文件覆盖：21 endpoint method/path/body/header及正式 schema 正反样本；三Lookup闭合union与不同原意图；合法最大 escaping/Unicode/字节cap、过限/非法UTF8/截断/晚取消与reader尾；UUIDv7正确性/无随机源/重放不换ID；sameidentity checking保留与真正identity/CSRF改变销毁；迟到读/401/finally隔离与唯一Cookie owner；历史receipt/current分离、未知粘性及禁止自动副作用；树展开/选择/typed深链/页外目标、rank筛选/跨页/Blocker cursor；生产router/safeReturn/Owner→Model→Work链与可取消Logout。

旧回归按影响选择前述六文件及现正式 Model/System恢复、personal/auth 相关 App/Session覆盖，不为新增路由放宽旧拒绝集。精确format、全部适用Vitest、严格TS与生产构建，最终 `npm run check --prefix web` 均须实际完成；新树依赖/固定资产与活动 Model 分开，不使用活动未验收产物。

### 8.2 作者真实浏览器矩阵

复用 [Owner fixture](../../../tests/account/project_owner_web_fixture_test.go)、[Owner顶层](../../../tests/account/project_owner_web_test.go)的同包实际能力及 `scripts/test-objects.sh → outbound → postgres` 原链；真实默认 Central 根通过测试进程内 no-tag `app.Run(...)` 启动，结合正式 Account bootstrap/invitation/redeem/login 与实际 Session/CSRF，不手种Account/Session，不宣称本卡由独立cmd子进程托管。浏览器访问任务私有同源静态入口，反向代理正式 API；复用既有静态托管，对§3四route实际直接GET、浏览器地址导航及刷新验证，包含带点号Project名称。正例完整经过Session、Owner Resolve/Get和目标读取；非法Work后缀/ID/编码路径通过正式Vue原始路由校验拒绝，零Work读取；API与缺失asset沿原handler处理，不降为SPA。SPA内部点击不能替代直接访问与刷新检查。该历史路径托管仅属fixture，不是生产SPA发布。Work事实只能由正式HTTP/service产生；明确生命周期负向fixture输入不冒完整archive/cleanup。

| 精确作者 top / case | 必须区分的结果 |
| --- | --- |
| `TestAccountProjectWorkPlanningWebReadAndNavigation` / `read` | 实际Owner Get门槛、50/51分页、父子与typed深链/页外对象；四route（含点号Project名）实际直接GET/浏览器导航与刷新，非法Work路径由Vue拒绝且零Work读取，资产/API不降为SPA；默认Current/无Current、筛选/失效cursor、无权限/pending/deleting与原路径保留 |
| `TestAccountProjectWorkPlanningWebStructureAndTasks` / `planning` | 六Structure与三Task mutation、七读取的真实消费者；字段/清空/Plan/同组排序、priority组改变、错误父/版本冲突，持久对象/版本/必要历史/Outbox/Activity一致且无额外命令 |
| `TestAccountProjectWorkPlanningWebBlockers` / `blockers` | 两类add/resolve、真实依赖环拒绝、status分页/Taskversion使cursor失效、resolved历史、错误目标与不擅自变Task状态 |
| `TestAccountProjectWorkPlanningWebOriginalRecovery` / `recovery` | 三域各实际写后响应丢失，保存原意图Lookup/显式同义重放、历史receipt与新当前值、改义冲突、无第二事实；in_progress/not_observed不可假成功，归档读/历史恢复vs新写拒绝 |
| `TestAccountProjectWorkPlanningWebIdentityAndOwnership` / `identity` | 正式Logout/撤销/过期、其它Owner与管理员、checking/新Session、迟到尾与Cookie owner实际join、跨Project/旧名复用、Owner/Model/Work既有确认链互不清理 |
| `TestAccountProjectWorkPlanningWebLayouts` / `layouts` | 浅深两主题×四宽度8组合，键盘/焦点/窄屏树/长文/readonly/empty/error，reduced motion及生产无Debug |

每个 Playwright case 原45s、expect5s、workers1/retries0；每Go top沿原120s含全部Cleanup/join，包6m。不通过加时/批次重试掩盖失败；若单case完整矩阵实际无法有界完成，先拆同目标的精确case与资源归属，回报负责人修规格，不延长产品期限或跳清理。

响应观察绑定同一原请求 method/path/query/身份/状态及完整body；schema与client验证同一实际body，不能另GET一份代第一次回复。断连区分真实已提交/未提交/Unknown，持久后验须有同原key事实及无重复写。只在fixture拥有的响应/帧边界注入故障，不fake Storeterminal冒真实COMMIT；复用已验故障设施，不复制Model私有harness或重新造监督器。

### 8.3 独立与终态

未参与实施者本人执行两个独立top：`TestIndependentProjectWorkPlanningWebRecovery`（三域真实原意图/历史与归档门禁）和 `TestIndependentProjectWorkPlanningWebAuthority`（身份/旧尾/切项目/聚合确认）。对应独立spec由独立作者编写，config仅接受这两个明确case及六作者case，不能任意执行目录。作者结果不冒独立动态。

旧浏览器回归最少选 Owner read/navigation、edit/rename、original recovery、identity/ownership及受共享Session/router影响的当前已接受Model/System代表；具体精确selector由负责人按最终diff定，不机械重跑无关全模块。使用本任务私有构建outDir/固定资产与独占真实窗口，禁止与Model的web/dist租约并发；新树共享的Docker/PG/outbound/MinIO/浏览器资源统一root调度。

实际测试、工具terminal、child/observer实际Wait、nonce/labels对应7资源及runtime/private目录、hostTCP双尾分别确认。业务通过但退出缺失不能记完整PASS，后来的current-clear不能回填原终态。监督及原预算沿已接受链，现有工具若不足只报精确缺口，不私自加第二监督器。普通输出放ignored output；必要可恢复harness源须进入本卡路径，失败事实保留，不生成重复闭包manifest/日志归档。

## 9. 实施与接受门槛

先冻结并独审本SPEC；按 §2 分离客户端/Session接入与页面/真实fixture任务，共享文件唯一writer，Model活动修改不带入新树。编译/纯测闭合片段可保存WIP，但未通过真实矩阵与独立审查不得称界面完成。最终一份原子结果包含产品、测试、必要README及限定台账；本卡完成仍不代表完整D11/D27、生产部署或创建Project链完成。

当前实际状态仅为 rev1 规格草稿落盘；没有本卡产品实现、纯测、构建、PG或浏览器PASS。后续接受结果在本节最小更新，复用的上游证据与本卡新证据分开，不覆盖原失败。

## 10. 已裁定的工程选择

以下四项已由负责人明确采纳，用于闭合页面细节，不修改已确认的业务模型；无需另向用户请求决定，仍接受独立规格审查。

1. 四条路由采用 §3 的 `milestones/sprints/tasks/{id}` typed path，基础Explore读取Current指针但不替换普通Kanban入口；页外对象用真实详情+提示，不自动全量定位。
2. Task create 的type/priority初始为空、用户必须明确选择；避免无产品来源的 `task/medium` 默认。Plan首版纯文本阅读/textarea编辑，不新增Markdown renderer依赖。
3. 所有页固定50、仅opaque前后页；排序以明确“某对象之前/组尾”操作实现，暂不加拖拽或跨页“上移/置顶”推测。
4. 恢复只覆盖同完整identity的内存原意图；真正Session/CSRF改变或刷新销毁，不新增跨Session界面。归档历史恢复沿正式服务Read→receipt→新写门禁，不因Project非active隐藏历史操作。


## 11. 任务评审界面限定切片（2026-10-11）

本批在 main `6a6da44e` 已有 Human Transfer/Lookup、三条评审边和[真实 Agent 安全目录](d10-agent-configuration.md)之上新增前端消费者，无后端、DDL 或依赖锁变更。组合源 `53bd25cb` 的限定任务评审界面已通过定向前端检查和 `TestTaskReviewWeb` 两个真实浏览器子场景；原调用及资源退出均已闭合，独立有限源码与最终原件审查已接受。该结果交付本节切片，不代表整张规划界面卡完成。

正式导航为“任务”，路由恰 `/{owner}/{project}/tasks`、`tasks/sprints/{SprintID}`、`tasks/{TaskID}`。默认从当前 Owner Project Get 的 `current_sprint_id` 读取真实 Sprint/Milestone；无 Current 时保留结构选择，不自动选择或启动 planned/completed Sprint。七列按各自 `state+sprint_id` 读取服务端分页与顺序；Task 深链校验真实父链，详情抽屉关闭回所属 Sprint。手选 Milestone 回任务入口并清 Sprint/Task，当前 App 内刷新保留手选；整页重载重新遵守入口默认，身份或 Project 改变清理原选择。

详情呈现 Task 当前状态、原字符串版本、说明、Plan、关联结构和 Blocker。`in_progress→in_review` 与 `in_review→todo` 显式选择真实 Agent 并填写说明；`in_review→done` 填写说明、省略 Agent 以保留 reviewer。只有退回 todo 要求零 unresolved Blocker，界面不将该条件加到提交评审或接受完成。选择项和负责人来自 Agent List/Get，显示 `display_name`，未配置时回退 `name`；目录不提供 busy 或运行授权。最终 Owner、版本、活动执行和业务资格仍由原 Transfer 服务判定。

所有读写经原 Session 单一请求所有者；App 持有页面状态，临时 checking 隐藏保护内容但保留同身份草稿。首次提交前私有冻结原 Project/Task、version、request、key 和 CSRF；结果不确定仅提供原键 Lookup，`in_progress/not_observed` 保持未决，不重发 Transfer、不用当前 GET 代替原回执。真正身份失效清保护数据和原意图，迟到响应不得发布到新身份或对象；可见取消不提前释放仍在返回的 body/cancel 尾。

共享客户端区分实际 EOF 与提前中止：JSON、空正文和头像读取在原 `reader.read()` 返回 `done=true` 后只释放 reader，不再调用 `reader.cancel()` 或外层 `body.cancel()`；即使随后 JSON/DTO 校验拒绝，也不重复取消已完成正文。成功 `204` 的 logout、deleteAvatar、completePasswordReset、revokeSystemInvitation 四口统一等待空正文完成，兼容无 body，拒绝非空正文。EOF 前异常或 abort 仍等待原一次 cancel Promise，释放锁后才确认该 body 已收尾；外层仅对未被 reader 收尾的早期拒绝响应执行并等待清理。容量、严格解码、身份门及 Session 原调用退出要求不变。

`auth.restore()` 在原严格 Session 响应已发布、原调用实际返回且身份仍有效时，返回冻结的安全 `{user,session}` 副本，不含 CSRF；同一次在途恢复复用原 Promise，失败、超时或身份失效返回 null。既有忽略返回值的调用兼容，不增加授权入口，也不使迟到结果恢复旧身份。

定向验证按实际受影响输入组合，不将重复执行的用例相加为唯一总数：原路由 62 项和页面状态 3 项通过；`frontend06` 的客户端、Session、页面及正文生命周期等 31 项通过；安全恢复返回变更后的 `frontend07` 为 Session 20 项加 Work Review Session 4 项通过；统一 `204` 后的 `frontend08` 为正文生命周期 9 项加旧端点代表 5 项通过。三个后续前端轮次的改动文件格式检查、完整类型检查和生产构建均通过，未运行的旧平台矩阵不计入结果。

`native06` 使用 `frontend08/dist` 与已通过编译的原候选，真实 Account/当前 Owner、已初始化 Project、Agent 目录及已终态 Work 执行素材经正式服务装配。`review-complete` 在无 active Execution 的 Task 上实际选择另一 reviewer，完成 `in_progress→in_review→done`（43.42 秒）；`lost-confirmation-lookup-and-session-revocation` 实际执行 `in_review→todo`，在提交持久事实后的原回执截断下保持 Unknown，禁止第二次 Transfer，只按原键 Lookup 恢复，再完成正式 Logout、保护数据清理及后续 Session 拒绝（43.55 秒）。原数据库核对版本、负责人、历史和 Outbox，未用 SQL 种成功状态。整 top 86.97 秒，两原 Node 调用成功，Go/driver/supervisor/outer 均退出 0；7 个资源的两次缺席检查及进程、TCP 尾均闭合。

真实 HTTP 观察保留原 `requestfailed` 事件；仅精确 GET 200 的 `ERR_ABORTED` 可由同一原请求的服务端完整正文长度/摘要、EOF/关闭/handler 返回、浏览器原正文收尾与原公开消费者的当前有效安全投影共同证明完成，不以 DOM 或补发 GET 替代。预声明的 Transfer 回执截断单独验证已提交事实；Logout POST 没有失败豁免。两场景覆盖桌面与 390 窄屏、键盘操作及恢复提示；四张截图的有限视觉检查只接受实际详情可见区域，完整七列整板、深色主题和其它宽度仍未视觉验收。原结果位于 `output/ai/agent-system-integration/task-review-web-06-control/result.json`。

旧 Planning06、Recovery04 以及本片 native01–05 的 whole FAIL 均保留，后继成功不回填旧轮次。当前切片不恢复旧 Planning/Recovery 全矩阵，不交付 Explore/规划写入、跨列拖拽、Timeline 或 Execution 新读口、完整筛选、生产 SPA 发布及生产 runtime/ready；任务状态也不由 Model 结果在前端推导。原 STOP 与整卡未完成边界不变。
