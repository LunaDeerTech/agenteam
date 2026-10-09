# D11 Human Owner 任务规划界面

状态：rev1，2026-10-09，工程选择已由负责人采纳并经独立 SPEC 审查接受；进入实施，尚无产品验证通过结论。正式前置为 main `f1c94ee5` 的 [Work Owner HTTP/root](d11-work-owner-http.md)。本卡不以活动 Model 工作树内容作为已接受依赖。

## 1. 完整结果与边界

已有 initialized Project 的当前 Human Owner 可从明确的“任务规划”入口进入 Explore：按 Milestone → Sprint → Task 浏览真实分页结构，创建/编辑 Milestone、Sprint，创建未指派 backlog Task，编辑普通字段与 Plan、调整同组顺序，并新增、分页查看、解除 `rely_on` 和无外域引用的 `waiting_for_human` Blocker。响应丢失后，通过本界面在首次发送前保存的原意图查证，必要时由用户明确同义重放；历史回执与当前对象分别展示。

消费已接受的 [Structure](d11-work-structure.md)、[Task Planning](d11-task-planning.md)、[Blocker 持久服务](d11-task-blocker-service.md)、[Owner 工作区](d27-project-owner-workspace-ui.md)及 Work HTTP 的全部 21 项能力。领域字段、权限、版本、幂等、排序及事件语义仍以上游为唯一来源；[正式 OpenAPI](../../../api/openapi/work-planning.json)是客户端编解码依据，不另设 HTTP 契约。

本卡是[任务布局](../../frontend-design/layouts/tasks.md)的限定 Explore 规划消费者。普通 `/tasks` 的 Kanban/Current Sprint 默认入口尚未实现，本卡不注册该路径、不把它重定向到 Explore、不添加假七列或失效的 Kanban 切换按钮。既定[项目工作台](../../frontend-design/layouts/project-workspace.md)普通任务入口规则保持；此次导航明确标作“任务规划”。

不包含 Project 创建/初始化 HTTP、Agent 目录/指派、Task 状态流转/跨列拖拽/跨 Sprint 移动/删除、Sprint start/complete/rollover/delete、完整 Timeline/评论、Execution、Scheduler、实时订阅或生产 SPA 发布。无新迁移或后端修改。`ready=false`/503 与 Object join、OpenAI tools、SPA publication、Jina/Image 停止项保持。fixture 的正式 Project service + test Skills 准备不证明真实 Skills/创建 HTTP 已绑定。

## 2. 文件域、隔离与接口分工

产品实施仅在 root 已创建的 `/workspace/agenteam-work-ui`、`ai/work-owner-planning-ui` 进行；本卡已从活动树冻结初稿单向同步到此隔离树，后续仅维护本树版本。root 负责 Git，禁止同时修改两树同名源。以下是 SPEC 接受后的拟定实施域，不构成本轮产品写入授权。

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

Work 页提供显式“刷新项目信息”，在当前 Work/Owner 读取与身份门禁允许时调用已有 Owner `readCurrent()`；它按已绑定稳定 ID 真正 Get，再沿既有 canonicalize 更新名称地址，保留同 ID Work 草稿。点击不表示放弃编辑，也不生成写命令；读取失败/失权沿 Owner 原错误界面处理，不能继续用旧资格发 Work 请求。读取被导航/身份退役取消后，迟到结果不得恢复旧项目。Session checking 恢复本身不等于 Owner 已重读，不增加隐式刷新、自动重试或共享 Owner 默认行为；浏览器改名验收必须通过此公开动作。

## 4. 页面行为与可写范围

桌面为规划树与详情；窄屏树用覆盖侧栏，选择后收起，保留项目身份和可返回的树按钮。只展示已绑定操作，状态文案为中文，canonical enum 仅是 wire。共用 [Ui 接口](../frontend/components.md)及[主题/交互规范](../../frontend-design/styles/README.md)，不导入 Debug。

| 对象 | 真实显示/操作 |
| --- | --- |
| Milestone | title/description/id/version/时间；创建、编辑、同 Project Milestone 组内排序，按需 Sprint 列表 |
| Sprint | title/description/父 Milestone/state/正式起止元信息/version；创建 planned Sprint、编辑；同 Milestone 内排序，按需 Task 列表；不提供跨 Milestone 迁移 |
| Task | title/description/type/priority/state/Plan、正式 parent/version/时间；创建未指派 backlog，编辑普通字段/Plan、同 Sprint/state/priority 组排序；不提供 state/assignee/parent 修改 |
| Blocker | type/description/typed metadata/创建与解决事实；仅两类 add/resolve，分页 status 切换；依赖项以真实 Task 标题/ID/状态及详情跳转呈现；候选页含三者，已有记录可显式逐条Get当前详情，尚未读/失败须明示，不批量扫描全图或猜测状态 |

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

复用 [Owner fixture](../../../tests/account/project_owner_web_fixture_test.go)、[Owner顶层](../../../tests/account/project_owner_web_test.go)的同包实际能力及 `scripts/test-objects.sh → outbound → postgres` 原链；真实默认 Central 根通过测试进程内 no-tag `app.Run(...)` 启动，结合正式 Account bootstrap/invitation/redeem/login 与实际 Session/CSRF，不手种Account/Session，不宣称本卡由独立cmd子进程托管。浏览器访问任务私有同源静态入口，反向代理正式 API；复用既有静态托管，对§3四route实际直接GET、浏览器地址导航及刷新验证，包含带点号Project名称。正例完整经过Session、Owner Resolve/Get和目标读取；非法Work后缀/ID/编码路径通过正式Vue原始路由校验拒绝，零Work读取；API与缺失asset沿原handler处理，不降为SPA。SPA内部点击不能替代直接访问与刷新检查。该历史路径托管仅属fixture，不是生产SPA发布。Work事实只能由正式HTTP/service产生；明确生命周期负向fixture输入不冒完整archive/cleanup。Activity刺激只对正式登录取得且匹配本fixture Owner的Session，在真实User EX事务锁内回拨issued/last_activity时间以跨过既有60秒节流；之后真实UI命令必须推进last_activity，不制造身份/业务成功或放宽case时限。

| 精确作者 top / case | 必须区分的结果 |
| --- | --- |
| `TestAccountProjectWorkPlanningWebReadAndNavigation` / `read` | 实际Owner Get门槛、50/51分页、父子与typed深链/页外对象；四route（含点号Project名）实际直接GET/浏览器导航与刷新，非法Work路径由Vue拒绝且零Work读取，资产/API不降为SPA；真实无Current与planned显式选择、筛选/失效cursor、无权限/pending/deleting与原路径保留 |
| `TestAccountProjectWorkPlanningWebStructureAndTasks` / `planning` | 六Structure与三Task mutation、七读取的真实消费者；字段/清空/Plan/同组排序、priority组改变、错误父/版本冲突，持久对象/版本/必要历史/Outbox/Activity一致且无额外命令 |
| `TestAccountProjectWorkPlanningWebBlockers` / `blockers` | 两类add/resolve、真实依赖环拒绝、status分页/Taskversion使cursor失效、resolved历史、错误目标与不擅自变Task状态 |
| `TestAccountProjectWorkPlanningWebOriginalRecovery` / `recovery` | 三域各实际写后响应丢失，保存原意图Lookup/显式同义重放、历史receipt与新当前值、改义冲突、无第二事实；in_progress/not_observed不可假成功，归档读/历史恢复vs新写拒绝 |
| `TestAccountProjectWorkPlanningWebIdentityAndOwnership` / `identity` | 正式Logout/撤销/过期、其它Owner与管理员、checking/新Session、迟到尾与Cookie owner实际join、跨Project/旧名复用、Owner/Model/Work既有确认链互不清理 |
| `TestAccountProjectWorkPlanningWebLayouts` / `layouts` | 浅深两主题×四宽度8组合，键盘/焦点/窄屏树/长文/readonly/empty/error，reduced motion及生产无Debug |

Current Sprint 非空真实正例暂不可由正式生产接口形成：[Work Structure](d11-work-structure.md)明确排除 start/complete/rollover，现有 planned Sprint 创建不设置 Project current pointer。本卡不以直接 SQL 伪造 Work 事实；本轮真实覆盖无Current与planned typed选择。§4自动读取非null指针及保留用户选择的规则保留，并有合法投影纯控制；非null真实正例是D11 Sprint生命周期接通后的待集成gate，未验证，不作为本卡规划能力已验证范围。

每个 Playwright case 原45s、expect5s、workers1/retries0；每Go top沿原120s含全部Cleanup/join，包6m。不通过加时/批次重试掩盖失败；若单case完整矩阵实际无法有界完成，先拆同目标的精确case与资源归属，回报负责人修规格，不延长产品期限或跳清理。

响应观察绑定同一原请求 method/path/query/身份/状态及完整body；schema与client验证同一实际body，不能另GET一份代第一次回复。断连区分真实已提交/未提交/Unknown，持久后验须有同原key事实及无重复写。只在fixture拥有的响应/帧边界注入故障，不fake Storeterminal冒真实COMMIT；复用已验故障设施，不复制Model私有harness或重新造监督器。

`in_progress` 的唯一额外刺激限定本轮私有 PG：对精确 Project/target/原 command/key 的最终 Outbox 写入施加 owned SQL 故障。原命令必须由默认根自己提交真实 planned，最终事务实际回滚；以 SQL 和正式 Lookup 双核 planned/in_progress 及零新增业务/History/Outbox事实，不种或改这些结果行。移除故障后，私有真实客户端以同 User/原 key/原 body 外部续写，UI 只再次明确 Lookup；界面在 in_progress 时仍禁重放、不自动轮询。若实际服务收敛到别的状态，本刺激失败，不伪造 planned。`not_observed` 仅对预先精确绑定的原请求在 owned proxy 未转发前断连，并核同 key 不存在；改义反例沿正式 HTTP 返回409，核原 receipt/业务及计数不变。二者均不豁免普通成功 finished/同体门槛，不扩大生产接口、资源链或预算。

`not_observed` 的断连屏障持续到公开原 Lookup、真实同 key SQL 零记录及当前业务/History/Outbox 不变三者核验完成；随后才解除，供用户明确按原请求重放。屏障只接收该 stage 的精确 Project/target/PATCH/command、原 key、原始 body 字节和原 CSRF；同路径改义拒绝，其他读取和 Lookup 保持原转发。允许至多四次由实际客户端产生的同义物理尝试，每次均复用既有 cut writer 写真实非成功 503、application/problem+json、Content-Length 4096 与单字节 `{`，实际 Flush/Hijack/Close 后形成截断响应；原 mutation 绝不转发，不提供完整 Problem、成功回执或合成 Lookup。fixture 不主动重试，至多四次上限不变；超限、身份或材料变化均令验收失败。先以实际生产 accountTransport/API/Session 的受控 stream 离线闭环证明读失败保留不确定、无自动请求，只有显式原意图 Lookup 才得到 not_observed，随后仍须真实 browser/PG 验证。Go 后验要求实际完整截断操作数等于未转发的服务响应空记录数（Status=0 仅表示没有上游服务响应，不表示下游零响应），仍恰一次非终态 Lookup、一次显式成功重放和一次 committed Lookup，不以多个物理尝试伪造多个浏览器 Request 或 expectedIncomplete。浏览器只有预先声明的 `unforwarded-milestone-update` 同一原 Request 可接纳固定 503/problem+json/CL4096/connection-close/无上游 Request ID 且无 Transfer-Encoding 的 owned 截断；仍必须实际 requestfailed，成功 finished、缺响应头、其他 Request 失败或仅页面关闭均拒绝。此回复单独计数，不冒上游完整 schema/body/RequestID；其他三域成功后断流和普通成功沿原观察链。原 proxy join 后仅保存 stage 闭集、是否绑定、尝试/实际关闭/完整截断操作计数、拒绝首因闭集及已核验释放标记，不落 key/body/CSRF/业务 ID；旧轮未采该信息不补写。

### 8.3 独立与终态

未参与实施者本人执行两个独立top：`TestIndependentProjectWorkPlanningWebRecovery`（三域真实原意图/历史与归档门禁）和 `TestIndependentProjectWorkPlanningWebAuthority`（身份/旧尾/切项目/聚合确认）。对应独立spec由独立作者编写，config仅接受这两个明确case及六作者case，不能任意执行目录。作者结果不冒独立动态。

旧浏览器回归最少选 Owner read/navigation、edit/rename、original recovery、identity/ownership及受共享Session/router影响的当前已接受Model/System代表；具体精确selector由负责人按最终diff定，不机械重跑无关全模块。使用本任务私有构建outDir/固定资产与独占真实窗口，禁止与Model的web/dist租约并发；新树共享的Docker/PG/outbound/MinIO/浏览器资源统一root调度。

实际测试、工具terminal、child/observer实际Wait、nonce/labels对应7资源及runtime/private目录、hostTCP双尾分别确认。业务通过但退出缺失不能记完整PASS，后来的current-clear不能回填原终态。监督及原预算沿已接受链，现有工具若不足只报精确缺口，不私自加第二监督器。普通输出放ignored output；必要可恢复harness源须进入本卡路径，失败事实保留，不生成重复闭包manifest/日志归档。

## 9. 实施与接受门槛

先冻结并独审本SPEC；按 §2 分离客户端/Session接入与页面/真实fixture任务，共享文件唯一writer，Model活动修改不带入新树。编译/纯测闭合片段可保存WIP，但未通过真实矩阵与独立审查不得称界面完成。最终一份原子结果包含产品、测试、必要README及限定台账；本卡完成仍不代表完整D11/D27、生产部署或创建Project链完成。

rev1 已经未参与产品实现者独立接受。API、Session恢复与controller已通过相应有限独立纯控制，页面组合已可构建；四fixture有限静审所见Lookup目标投影缺陷已修，并有8组实际观察器纯/race对照通过；新race编译闭合。完整前端check实际通过（64文件2776单元、类型、production build），六作者PW与同体schema/client源已落盘/严格TS通过，但尚无完整UI独审或真实浏览器通过结论；planning首轮有限审查所见no-op排序与成功后关闭已移除按钮已统一修正；同体schema响应$ref、真实清空/原文/七读取刺激同步修正，持久后验补同key回执/Outbox/TaskEvent/最终对象/Activity及无额外事实，修后窄复核与真实首轮仍待。两driver19独立纯控接受，不能代资源实际退出。其他矩阵和独立两case不冒完成。Current Sprint验收事实纠正经原独验确认：正式非null生产者尚缺，保留上文后续真实集成gate。后续结果最小更新，复用上游与本卡新证据分开，原失败保留。

## 10. 已裁定的工程选择

以下四项已由负责人明确采纳，用于闭合页面细节，不修改已确认的业务模型；无需另向用户请求决定，仍接受独立规格审查。

1. 四条路由采用 §3 的 `milestones/sprints/tasks/{id}` typed path，基础Explore读取Current指针但不替换普通Kanban入口；页外对象用真实详情+提示，不自动全量定位。
2. Task create 的type/priority初始为空、用户必须明确选择；避免无产品来源的 `task/medium` 默认。Plan首版纯文本阅读/textarea编辑，不新增Markdown renderer依赖。
3. 所有页固定50、仅opaque前后页；排序以明确“某对象之前/组尾”操作实现，暂不加拖拽或跨页“上移/置顶”推测。
4. 恢复只覆盖同完整identity的内存原意图；真正Session/CSRF改变或刷新销毁，不新增跨Session界面。归档历史恢复沿正式服务Read→receipt→新写门禁，不因Project非active隐藏历史操作。

### 首planning真实失败（保留）

首轮原45秒case/120秒Go/6分钟包预算不变，Go24.23秒失败、外层实际exit1/165.537秒。Milestone创建/编辑、前移/移尾及对应重读已执行；创建Sprint后原命令确认标题可见，但“查证原命令”仍disabled超过原5秒。服务端Sprint POST及随后Get有200同体，尚未定位持续禁用原因，不将前置响应称为完整UI或持久后验通过。case结束时观察器的response.finished另抛Test ended，需实际收束；原监督器因driver后4个descendant记STOP，之后4个实际Wait0。Node/proxy/root/driver实际退出、七资源双absent、desc/runtime/private/TCP双清及输入同一齐，原整体FAIL保留。后续只定位并修受影响边界，完整矩阵仍未接受。

随后定向纯控制复现页面availability缓存缺陷：当前读结束时实际Cookie owner已释放，页面computed仍保留此前canLookup=false。仅新Work getter显式依赖既有state.busy，继续检查owner/pending后红→绿，64个Work/Session兼容控制通过，真实复验待运行。观察器对原finished失败立即收束并仍拒绝验证；监督器仅UI分支先观测state及actual nonblocking Wait已退出子，非0拒绝，再保持原活descendant与资源门槛，11个控制通过；预回收初版无界循环经独审拒绝后改为入口descendant快照有限次数；修后实际owner/响应式、观察Promise与UI-only回收/default对照均获有限独立接受。完整前端2777单元、类型与build通过；真实同组复验仍待，不回填首轮。

### 修后planning第二轮（保留）

修后planning第二轮仍整体FAIL：冻结7354334e、原binary05/私有新dist及原预算，98126实际exit1/126.236秒、Go23.36秒。已越过原Sprint恢复按钮门槛并完成Sprint更新/两次排序；随后Task新建表单内精确“创建”按钮不存在，原5秒disabled断言失败，Go持久后验未到。实际模板标签为“创建 Task”，此轮尚无最终UI接受；原finished观察器没有再产生未处理拒绝。direct actualWait1，四个adopted枚举时均Z并实际nonblocking Wait0、未STOP；七资源双absent、desc/runtime/private/TCP双清及输入同一齐，窗口已释放。新回收仅证明本轮正确执行，首轮FAIL/STOP不回填。原log在output/ai/work-owner-planning-ui/pg/planning-02.log；下一先修精确测试标签并复核剩余同组刺激，不改产品或放宽期限。

第二轮定位修订仅两PW源：save显式区分structure/task并精确匹配对应form与“创建”/“创建 Task”；实际既有controller在提交时校验必选项而非预禁用，故测试改为未填提交→两字段错误且Task POST为0，仅填type→priority错误且POST仍0，最后显式priority后沿原创建/恢复链。未改产品、预算、EOF/schema或Go持久断言。严格TS69044及精确单planning发现43725均actual0；最初TS命令漏既有Node typeRoots/DOM.Iterable的setup失败保留。未参与本修订者核真实模板及6正反控制后有限接受；原binary05与私有dist不变，第三轮须新独立输出目录并等待root fresh grant。

第三轮planning仍完整FAIL：bea8ad29/binary05与原私有dist、原45秒case；首Milestone前移POST的originalBody停在原response.finished，直至case总界。74490实际exit1/167.494秒、Go58.81秒；Task标签修正与Go持久后验均未到。代理同原POST200完整上游body及随后Get200已记录，但缺浏览器reader/CL、PWfailed/CDP时序，不推根因或与Model同因。direct actualWait1、四现场Z→actualWait0、七资源/desc/runtime/private/TCP双尾与inputsame齐，窗口已释放；01/02原FAIL和首STOP保留。必要脱敏原件见 `.agent-state/work-owner-planning-ui/planning-third-failure.json`，普通原log在output/ai/work-owner-planning-ui/pg/planning-03.log。后继仅复用已接受观察模式做精确Work请求诊断设计，不放宽finished/EOF/身份或盲重跑。

后继失败观察限定在既有两PW源：原Request对象关联request/response/failed/finished及page-close的Node单调观测时间，合法公开ID、闭集endpoint和脱敏failure分类；记录原originalBody当前await阶段。最多保留256个请求，失败afterEach写出安全快照，成功不生成该产物；观察写失败不替换原测试失败。不新增finished调用、原生fetch或请求，不改变原Promise/gate/45秒预算；快照不是关闭后最终状态，也没有观察浏览器native EOF。实际源15正反纯控通过且0unhandled、严格TS通过，尚待有限独审/真实使用，不能据此解释第三轮或推同Model根因。

read补充输入仍仅测试：私有Skills替身对唯一固定Project保留pending，其创建/权限仍经正式Project服务，默认fixture不启用；浏览器应拒绝pending/deleting且零Work请求，末端SQL仅核真实Project状态和无Work事实。Task筛选核同原GET的完整body、精确query和公开树结果，跨Project对象必须404且无旧详情。三Go源已实际integration race编译binary06，原binary05不含此read输入；PW严格TS及精确read发现通过。这些是未独审、未真实运行的可构建验收源，不计入UI通过范围。

read有限独审发现原Go把用于真实cursor失效的IPC Milestone PATCH也误算为浏览器写入；已保留该静审FAIL并精确修正。仅唯一read/main/Milestone/固定文案IPC，按准备客户端独立真实Session的CSRF摘要、path/target/domain/command及完整返回receipt绑定实际观察中的原key/body；末端恰一条DeepEqual例外，其余非GET仍拒，pending/deleting零请求与零事实不变。两Go源码重新编译binary07（74093 actual0），未参与者窄复核接受，无剩余read静审mustfix。两PW失败诊断另获未参与者9项实际正反控制接受；这些仅限定输入就绪，不是浏览器PASS。read-01拟沿原工具/7资源链、45/120/6m/540+60使用binary07与原私有dist，须root fresh grant；binary06为返修前输入，不再用于本read验收。预期截断/取消场景的finished与incomplete观察边界仍待独立裁定，不借本read接受放行recovery/identity或完整UI。

read首轮17720完整FAIL（Go27.25秒、outer132.718秒），初始50/51翻页与回首页已过，外部title PATCH后期望cursor失效的5秒提示断言失败；实际下一页GET200/1项且原finished已返。正式Structure §2.2只在结构/排序变化推进generation，标题更新不推进，本次刺激前提错误，待改为真实排序；更早回首页GET另见同Request aborted/finished pending，尚无browser EOF或原因，不与本断言混同。filters/pending/deleting及Go持久后验未到；actualWait、七资源/runtime/private/TCP双尾与inputsame齐，原FAIL保留。必要脱敏事实见 `.agent-state/work-owner-planning-ui/read-first-failure.json`；原log在output/ai/work-owner-planning-ui/pg/read-01.log。仅安排测试刺激窄修，不改产品generation或原gate。

read刺激窄修已有限独审接受：仅read/main/一次且零额外参数的reorder IPC，正式前两条读→第二条移到第一条之前→changed真与实际重读反序；seed仍首页。唯一外部写豁免精确绑定该POST的准备Session CSRF摘要、目标、command、原key/body及完整receipt，末端整条DeepEqual恰一，其余写仍拒。PW绑定原opaque query并要求同原409/CURSOR_STALE完整body与失效提示，不改产品、成功finished/EOF或期限。race编译87055 actual0/binary08、strictTS91715 actual0、精确read list91030 actual0/恰1；首list48084误用环境变量名的setupFAIL保留。两源已冻结，read02使用新输出目录且须root fresh grant，尚无修后动态结论；expectedIncomplete仍为独立ignored准备。

read第二轮85803仍整体FAIL（51b46c8f/binary08，Go66.38秒、outer197.276秒）：真实外部reorder/重读反序、原cursor409与失效提示已过；第三次filterRead（bug/medium，期望0项）停原response.finished直到45秒。对应原Request在约6796ms已aborted、原finished在约44731ms失败快照仍pending，page-close未见；同原上游GET200/items0不证明浏览器EOF/解码/状态发布。前两筛选通过，后续筛选/Project门禁及Go持久后验未到。directWait1、四Z→actualWait0、七资源/desc/runtime/private双尾齐且inputsame，但hostTCP尾另有1行未知tuple差异FAIL；当前六已登记PID均absent/owned runtime空，只证明current-clear，不回填原TCP结果。必要原件 `.agent-state/work-owner-planning-ui/read-second-failure.json`，普通原log output/ai/work-owner-planning-ui/pg/read-02.log；不盲重跑，不放宽成功finished或改写旧FAIL。

expectedIncomplete独立增量已获有限独审接受，尚未真实运行：只预声明三类原意图截断写与一条held Task GET，绑定同一原Request/目标/版本/精确语义、原key/body；只有其真实requestfailed可收束预期不完整，缺终态/未声明/错意图/重复/仅page-close均拒。普通成功仍原finished/null，originalBody/schema/decoder逐源未变。原Owner截断设施仅增加nil默认无副作用回调，实际一字节Write/Flush/Hijack/Close返回逐项留在Work私有观察；Go核三域同key历史receipt/Lookup/显式重放、原operation历史/Outbox、真实seed计数增量与后继当前值分离，held读要求真实context取消及finish。实际helper/consumer 27纯控制通过且0unhandled，Go两纯top的15子对照race通过（9918 actual0），strictTS89349 actual0；最终race binary10已编译，原binary08仅证明read02输入。仅加强已有committed-loss与held取消场景，in_progress/not_observed/改义恢复及完整身份矩阵仍未闭合；未参与本轮实现者本人复核33项Node控制（含6新增负控、0unhandled）及Go两top15子race通过，无mustfix；不把源码或纯控写成真实case通过，下一真实运行仍须root新窗口。

身份矩阵补充已获有限独审接受，原可构建状态如下：新增封闭IPC仅接受本fixture真实浏览器Owner Session，核注销并在User EX锁内按精确前像回拨过期时间；Project改名与旧名新ID复用都走正式服务，不种Work事实。公开UI核同identity checking、同ID改名保草稿、Owner→Work分段取消、Model原返回导航确认及零写、他人Owner/admin零Work请求、真实过期后清草稿/新Session。strictTS94089与精确identity发现98996均actual0；Go闭集输入纯/race18662 actual0（1.020秒）、binary11 race-c18697 actual0。首次编译46465因SessionID已是string却调用String失败，原日志保留，修正后通过。默认成功finished及原held-read唯一取消刺激不变；恢复真实planned/not_observed/改义另按§8.2最小接缝补齐，未预写动态结论。

未参与实施的Knowledge作者已有限接受身份增量，新增真实requalify提取6纯控制actual0，原TS/Go控制及编译复用；不冒真实状态或预算可行性。

identity首轮65559完整FAIL（3a1b2da9/binary11，Go15.57秒、outer140.395秒）：初始Work选择、取消Logout及同identity checking保草稿已过，随后直接注销HTTP的status===204布尔断言失败；原实际状态码/body未采，不回填。后继撤销SQL、新Session、held取消、权限/改名/确认链/过期及Go末端后验未到。directWait1、四Z→actualWait0、Node/proxy/preparation/root join、七资源/desc/runtime/private/TCP双尾与inputsame齐，资源已释放；必要事实见 `.agent-state/work-owner-planning-ui/identity-first-failure.json`。离线对照正式Account wire确认该测试漏Idempotency-Key和必需JSON空对象；仅修测试刺激及安全整数状态断言，原FAIL保留，不推产品故障或放宽门槛。

logout测试刺激已按正式wire最小修正（单spec +4/-1）：一次私有UUID幂等key与data:{}，保留原CSRF/Origin/Session；直接断言实际整数204，不采错误体。作者实际源4控通过（旧缺key/body反例及400/503拒绝），strictTS87600、精确identity发现5656均actual0/恰1；Go输入/binary11不变，不重编。未参与者核正式wire及实际源3项正反控制后有限接受；原65559实际状态码仍未知。

identity第二轮39077仍完整FAIL（3cdb9df2/binary11，Go32.33秒、outer143.723秒）：正式logout204与撤销SQL、新Session、held读跨Project导航、other Owner/admin拒绝已顺序通过；同ID真实Project改名后pageshow Session200/同Session已返，但规范URL的原5秒断言仍见旧名owner-main。Owner当前项目发布/该时DOM未采，暂不推产品或测试原因。后续Owner/Work/Model护栏、旧名复用、expiry、最终observer及Go持久后验未到。原Work安全观察另保留一条普通Task GET aborted/finished未返，与已声明held取消分开，不借expectedIncomplete放行。directWait1、四Z→actualWait0无STOP、各服务join及七资源/desc/runtime/private/TCP双尾与inputsame齐，窗口已释放；必要事实见 `.agent-state/work-owner-planning-ui/identity-second-failure.json`，普通原log在output/ai/work-owner-planning-ui/pg/identity-02.log。首轮未知注销状态与其它旧FAIL均保留；不盲重跑或放宽期限。

显式项目刷新与完整恢复刺激已可构建并获有限独审接受，真实验收未完成。§4新增公开“刷新项目信息”只复用既有Owner readCurrent，同ID规范化保Work草稿；共享Owner/Session无改动。实际App/路由组合原3红例缺入口，修后4文件116控制通过（58369）；首404刺激漏正式X-Request-ID的纯控FAIL单列保留。前端类型/build71743实际0、私有dist已更新，identity只经新公开按钮取得真实项目后再要求规范URL。recovery归档观察也使用这个动作，不再把pageshow误作OwnerGet。

§8.2的recovery两非终态与三域改义现已落测试源：原请求未转发前真实close、final Outbox精确故障/真实planned与回滚/移除后同User原请求私有续写均保留实际SQL和公开Lookup门槛；闭集IPC、私有与浏览器Session区分、原key/完整语义、503错误形态及四个精确单次操作后验拒绝放宽。新增测试对象只在原私有DB生命周期内创建，失败仍由原七资源链退休；没有业务/command/history/Outbox结果造数。原三域成功截断与历史receipt/归档同义重放继续验证，并只允许各一次精确改义409反例。纯/race七top5329实际0/1.031秒；PW strictTS74271、identity发现25259/recovery发现47387各1项均实际0；binary12完整race编译20975实际0，两真实top发现齐。纯测试与编译不证明真实planned/回滚、截断或新UI通过，有限审查接受后仍须逐个freshgrant运行，原预算不变。

该七技术源随后获未参与者有限独审接受，无mustfix；11项SQL谓词正反实际通过（SQLite共同子集，非真实PG），三个实际App/router用例70752通过，其余未选用例不计入本次结果。复用既有116及原纯控制，不外推真实planned/回滚或完整UI。

identity第三轮54638仍完整FAIL（4caa6a44/binary12，Go59.13秒、outer173.143秒）：初次Work草稿及取消Logout后，首次pageshow Session整数200已返，但原response.finished一直未返直到45秒case总界。此处尚在正式Logout之前，不是新Session阶段；同Session正文断言、撤销/新Session、rename及新公开刷新均未到。Work安全观察另有Milestone列表与Sprint详情两条普通GET aborted/finished未返，未采本次Session的failed/finished事件，不混为同请求或借expectedIncomplete通过；仍缺浏览器EOF与生产decode/publish证据。directWait1、四Z→actualWait0、各服务join及七资源/desc/runtime/private/TCP双尾与inputsame齐，资源已释放。必要原件 `.agent-state/work-owner-planning-ui/identity-third-failure.json`，普通原log output/ai/work-owner-planning-ui/pg/identity-03.log；原FAIL全保留，不盲重跑。

recovery首轮77556完整FAIL（4caa6a44/binary12，Go23.06秒、outer121.896秒）：首not_observed阶段实际原PATCH200/finished，receipt.changed真/version2及后继Get同值，而“不确定”标题未出现，原5秒断言失败。公开Lookup/同key零事实及in_progress/三域历史/最终Go后验均未到；未持久采原wire尝试数或hook命中，不把源码可能放行再次请求写成已证透明重试。directWait1、四Z→actualWait0、各服务join及七资源/desc/runtime/private/TCP双尾与inputsame齐。另有独立前置缺口：首执行未采启动前fresh可用磁盘；运行中4.134GiB不能倒推启动前是否≥5GiB，不计完整环境接受。必要原件 `.agent-state/work-owner-planning-ui/recovery-first-failure.json`，普通原log output/ai/work-owner-planning-ui/pg/recovery-01.log。下一仅在原fixture修精确stage屏障/安全计数并做原handler离线红绿控制，不改产品、5秒或造not_observed事实；旧FAIL均保留。

not_observed刺激窄修已有离线红→绿：原Owner.observeRequest→Work路径85611实际失败，已绑定原请求再次到达会被放行；修后按§8.2持续屏障与安全计数，10个相关纯top及子控race31166实际0/1.052秒，包含原key/raw body/CSRF/target/command/query变更拒绝、4次边界、关闭失败及公开读取默认转发。仅fixture两Go源变化，产品/Playwright/private dist和原预算不变；binary13 race-c95423与精确top发现41597均actual0；未参与者有限独审已接受（追加并发Close/Rejected两实际源race控制通过），当时真实修后结果未有，不回填首轮透明重试或not_observed事实。

recovery第二轮83659仍完整FAIL（eec02e58/binary13，Go18.55秒、outer120.552秒）：not_observed屏障实际wire4/close4且Rejected=true，拒绝是次数上限还是原材料变化未采，不能改大上限或推产品错。公开原Lookup已真实200/{state:not_observed,result:null}，IPC原请求绑定通过；同key计数SQL已返回但数值未留，随后因组合判据失败退出，尚未核完current/事实不变或解除屏障，in_progress和末端后验未到。首exec前fresh磁盘6362923008≥5368709120 bytes已采PASS；direct Go/driver Wait1、六Z→actualWait（五0、一status9，原因不推）、各服务join及七资源/desc/runtime/private/TCP双清、inputsame齐。原组合报错不等于已证forward或落事实；必要原件 `.agent-state/work-owner-planning-ui/recovery-second-failure.json`、原log output/ai/work-owner-planning-ui/pg/recovery-02.log。下一仅补闭集拒绝原因及真实handler离线判别，不改产品、原45/120/6m/540+60预算或旧FAIL。

recovery03输入已就绪、未真实运行：`c9951551`的原请求零转发503截断与精确`unforwarded-milestone-update` observer按§8.2有限独审接受；独立Go三race控制54256与helper六正反控制173e91通过，无mustfix。作者Go七组race、client三控/类型、helper42与PW类型均实际通过，binary14已race编译且精确发现唯一OriginalRecovery。cap4、普通成功finished/同体/schema及全部预算保持。旧recovery01/02 FAIL与缺失拒绝原因不回填；下一轮先在同进程实采≥5GiB，再于root明确fresh grant下执行原七资源链。

recovery第三轮84156仍完整FAIL（feac9440文档/c9951551技术、binary14；Go25.30秒、outer123.348秒）。两非终态局部链已走：not_observed实际wire1/close1/truncate1且未拒绝、原Lookup与SQL/current/事实校验后解除并显式重放；in_progress实际final-Outbox fault_hits1、局部SQL/Lookup及原请求续写完成。随后首三域断流声明被WORK_INCOMPLETE_DECLARATION_REJECTED拒绝，三域历史/归档及最终observer/Go后验未到。首执行磁盘6916890624 bytes通过；Go/driver actualWait1、四Z→actualWait0无STOP，各服务join及七资源/desc/runtime/private/TCP双尾与inputsame齐，已释放窗口。必要事实见`.agent-state/work-owner-planning-ui/recovery-third-failure.json`，普通log在output/ai/work-owner-planning-ui/pg/recovery-03.log。另两普通请求aborted/finished未返独立保留，不借预期截断例外放行。

随后离线源及实际crypto.randomUUID红控确认observer把正式幂等key误作资源UUIDv7；只将key改按Foundation 1..128 ASCII标量校验，Project/target/blocker的UUIDv7与原Request/headers/failed/finished/cap4门槛不变。红控d04cb8实际1，修后12664实际0/45控/0unhandled，strictTS4423实际0；formatter首check仅新增CJS换行失败，已格式化。两技术源已冻结交未参与者独审，未重编Go或运行新真实窗。该修复不回填本轮slot私有状态或另两普通请求根因；旧FAIL保持。

幂等key窄修随后获Model未参与者有限接受，无mustfix：原45控及新增3控实际通过，共48控/0unhandled，涵盖ASCII单字节全集、第二Request/关闭后failed拒绝及精确headers+原failed退休slot；不提升recovery03动态结论。

recovery04（37095，ae101b00技术/binary14）仍整体FAIL：Go10.69秒、outer111.417秒，在Node启动前失败，尚未检验key窄修。原因是本轮执行者显式PATH遗漏实际Node目录，离线4892b7复现旧PATH无法解析node，保留继承PATH并前置Go可执行Node v24.19.0/actual0。首执行/exec前磁盘均过5GiB；Go955958与driver953888实际Wait1、无浏览器adopted子或STOP，proxy/body/preparation/root join及七资源/desc/runtime/private/TCP双尾与inputsame齐。必要事实在`.agent-state/work-owner-planning-ui/recovery-fourth-failure.json`，原log output/ai/work-owner-planning-ui/pg/recovery-04.log；原FAIL保留，源/门槛/预算不改，仅纠正下一次执行环境，复跑须新fresh grant。

recovery05（63642，b48c1a7e文档/ae101b00技术、binary14）仍整体FAIL：Go59.82秒、outer155.707秒，45秒总界止最终seen.verify（observerErrors期望0实际9）。已越过key声明阻塞、两非终态局部SQL/Lookup及三域历史/改义拒绝/归档Lookup与同义重放；最终observer/schema/client、complete报告与Go持久后验未到。安全快照71请求中62有原finished返回，4声明截断content-length-mismatch及5普通aborted均未返finished；后者为Milestone PATCH、Task Lookup/GET及两个Sprint GET。同原请求上游完整200不证明浏览器EOF、CL完整或生产decode/publish，快照约44846ms尚未page-close/observer rejection与超时后errors9不混成同一边界。首执行保继承PATH、Node实核及5852405760 bytes磁盘通过；Go/driver actualWait1、四Z→actualWait0无STOP、各服务join及七资源/desc/runtime/private/TCP双尾与inputsame齐，窗口已释放。必要事实见`.agent-state/work-owner-planning-ui/recovery-fifth-failure.json`，原log output/ai/work-owner-planning-ui/pg/recovery-05.log；下一先定位普通failed证据，不放宽finished、扩大预期截断或同输入盲重跑。
