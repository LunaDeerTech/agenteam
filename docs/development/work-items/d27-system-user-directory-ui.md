# D27：系统设置壳与用户目录只读页面

状态：rev1，2026-10-06 已由独立 verification_worker 静审通过（STATIC PASS）并获主线程采纳；被审稿 SHA256 `846591b9fea5f92f1a05305bebc83c3c54a2c401963cb4651f8d61484b986974`。本次仅更新页首，技术 §1–7 保持被审稿原字节。**注册时间后端补口仍未接受，UI 产品实施须等待该依赖独立验收并获主线程采纳；§6 的 20 个产品候选路径暂无实施权**。规格采纳不授予真实资源运行权，也不代表页面或产品验收通过。

## 1. 完整结果与依赖

管理员从系统导航进入真实系统设置，普通入口默认打开“用户与邀请 → 用户”，读取并分页查看邮箱、用户名、显示名、角色和注册时间。普通用户不见系统设置入口，直接链接显示无权限。只交付这一只读目录与所需设置壳，不添加邀请、模型、搜索、Inbox、用户详情/编辑或其他未实现栏目、按钮、占位页。

设计输入固定 `5fac116ab9d52d938ba3e3cb3626e5346264a6b8`；前端业务仍为已接受的公开入口组合，不读取后端作者活动候选作为已验依赖。实现开始时由主线程记录后端接受提交、冻结实际五路径及必要依赖差量；不能仅凭这张规格或候选 API 声明跳过门槛。

| 依赖 | 状态、证据及本卡消费范围 |
| --- | --- |
| System 用户目录含 canonical 注册时间 | **未满足**。[读口 rev1](d27-system-user-directory-read.md)已静审采纳并提交 `c11b512`，五路径产品仍在实施。本卡消费专用 flat 九字段 SystemUser、既有 users cursor 与当前 admin 读取。 |
| Account/System HTTP 与真实权限 | D07 B37 `022dcea`、关闭 `0ed8085`，见 [D07 终局](d07-account-session-smtp.md#b04-终局采纳与文档关闭)。隐藏菜单不替代该权限。 |
| 当前认证及单一 Cookie 请求 owner | [认证](../agent-team/d26-authentication-verification.md)、[个人设置](../agent-team/personal-settings-verification.md)、[公开入口](../agent-team/public-account-entry-verification.md)已接受；公开入口最终源码 `787a5c7`。复用当前 `useSession`、恢复/注销、个人设置草稿与明确身份切换。 |
| 视觉、路由与测试基础 | [前端基础](../frontend/README.md)、[应用框架](../../frontend-design/layouts/application-shell.md)、[通用设置](../../frontend-design/layouts/settings-shell.md)、[系统设置](../../frontend-design/layouts/system-settings.md)、[样式](../../frontend-design/styles/README.md)与现有 Vue/Ui 组件、锁定 Vitest/Playwright。没有新增依赖。 |

Summary 初值、Project/Artifact、Provider 调用与 tools 均非本卡依赖。Object 原停止任务和 tools 原独立停止任务不恢复、不改派、不重建。本卡不改变完整 D26/D27、生产 SPA 托管、Vite 真实代理或平台 ready 的原边界。

## 2. 路由、导航与布局

| 入口 | 确定行为 |
| --- | --- |
| `/system` | 固定重定向 `/system/users`；不保存或恢复上次系统栏目。 |
| `/system/users` | 唯一系统设置叶子；继承 `authentication: true`、`protected: true`，新增系统管理 meta。组件懒加载，成功确认身份后才显示内容。 |
| 系统导航“系统设置” | 仅当前已确认 admin 且未收到当前身份的系统权限拒绝时展示；指向 `/system`。沿现有 `meta.navigation` 生成，不复制一套导航数组。 |
| 未登录直接链接 | 沿现有登录恢复，return 闭集只增加精确 `/system/users`。`/system`、任意 query/hash、数组、外部 URL、未知系统叶子仍不是 return 目标；原四项保持。 |
| 已登录非 admin / 权限已拒绝 | 留在原链接显示“无权访问系统设置”及返回首页、重新检查权限入口；不挂载用户目录、不发列表请求，不静默跳回首页或伪造空列表。 |

继续使用 App 的个人名称入口和经既有草稿确认的退出操作，不新增注销、登录或身份切换流程。由个人设置跳转系统设置仍先执行既有“继续编辑 / 放弃修改”确认；不能先触发 Session 重验导致草稿被卸载。身份检查失败仍使用 App 原恢复区域，管理员导航在 checking/anonymous/uncertain/unavailable 状态隐藏。

仅对现有 `SettingsShell` 做必要参数化：可传标题、两级菜单项及是否显示侧栏退出按钮，默认仍为原个人设置三组与退出行为。系统实例标题“系统设置”，只有一级“用户与邀请”和二级“用户”，默认展开且叶子高亮；一级仅展开/折叠。系统实例不显示侧栏退出按钮，继续使用 App 顶部退出，避免绕过个人草稿确认。不得把 scope 混成一组菜单，或改成多个近似副本。

桌面复用悬浮侧栏、留白、独立滚动和 tokens；窄屏沿既有 760px 侧栏切换，入口为“系统设置栏目”，使用 `UiDrawer` 的焦点限制、Escape/遮罩关闭和触发焦点恢复。选择叶子后关闭面板。用户列表桌面为有语义列头的表格，窄屏可堆叠为有明确字段标签的行；长邮箱/名称可换行，不产生页面级横向溢出。必要横向滚动只在列表容器。

目录标题“用户”；五个展示字段分开标注，role 映射为“管理员 / 普通用户”。显示名为空时沿账号规则回落邮箱，不把 username 当显示名。注册时间列标明 UTC，使用 `<time datetime="完整 canonical 值">`；可见值到秒，完整六位微秒 UTC 值须可访问。目录中每个其他用户只是文本，不提供跳转其项目或代改其资料的入口。theme、version、initial_password_suggestion 仅作严格 wire 校验，不因此新增可见设置项。

复用 `UiButton`、`UiState`、`UiTable`/现有表格语义和既定 tokens；不改颜色、动效或全局样式基线，不使用 eyebrow。初次内容聚焦标题，分页/刷新保留可达的操作焦点与状态播报；不以全屏成功提示反馈读取。

## 3. 受限客户端与严格 DTO

新增 `web/src/api/system-account.ts`，导出以下只读形状及 `createSystemAccountAPI(fetcher?)`。它复用原 `accountTransport` 的安全 Problem、600,000B JSON 上限、真实 body/cancel 结束和 same-origin Cookie 配置，不直接调用另一套 fetch 实现。

```ts
type SystemUser = Readonly<User & { created_at: string }>
type SystemUserPage = Readonly<{
  items: readonly SystemUser[]
  next_cursor?: string
}>
type SystemUserQuery = Readonly<{ cursor?: string }>
// 每次固定 limit=25；不新增用户可配的 page size。
interface SystemAccountAPI {
  listUsers(query: SystemUserQuery, signal: AbortSignal): Promise<SystemUserPage>
}
```

`client.ts` 仅增加固定 GET `/api/v1/system/users` endpoint 与该 endpoint 专用 cursor/limit 编码。路径、method、成功状态 200 固定；query 用 `URLSearchParams`，没有任意 URL、headers、method 或 query map 透传。请求无 body、CSRF 或 idempotency key；仍为 `credentials: same-origin`、`cache: no-store`、`redirect: error`。旧 endpoint 不能接新列表参数，旧 transport 分支及预算不变。

`account.ts` 只导出既有 User 解析器的明确别名供复用，不改变其八字段规则或 AccountAPI 成员。SystemUser 先校验恰九字段，再将原八字段投影给原 User parser；不得放宽原 User 接受未知字段。原 API/controller 测试 mock 因此无需添加未使用的系统方法。

列表外层只接受 `items` 与可选 `next_cursor`，拒绝缺项、null、未知字段；每页 0–25 项。逐行校验 UUIDv7、原 User 约束、正 decimal string version 上界和 canonical `created_at`。时间必须恰 `YYYY-MM-DDTHH:mm:ss.ffffffZ`、有效日历且在 foundation 年份范围；不能只用会接受日期归一化的宽松 `Date.parse` 作为校验。保留原字符串和微秒，不以 JS Number/毫秒值重建 cursor 或注册时间。

同页 ID 不重复，顺序沿 `(created_at,id)` 严格递减；比较完整 canonical 时间和 canonical ID，不能因截断到毫秒误判同秒记录。cursor 是不透明、非空、至多 8192 字符的字符串，由服务端签名，前端只原样保存并编码；不解码其权限或自行签名。存在 next_cursor 时必须有完整 25 项，空页不能附 cursor。解析完成才一次发布冻结的新数组/行，不发布部分有效行或用缺失时间占位。

失败对象不保存 raw response、完整请求、body 或底层异常；页面不把用户目录、cursor 或 Cookie/CSRF 写入 console、URL、history.state、localStorage/sessionStorage。列表用户绝不进入 `acceptUser`、当前 Session、主题或个人草稿；即使其中包含自己，也仅是目录数据。

## 4. 当前身份与同一请求 owner

`createSessionController` 增加第二个可选、默认真实的 `SystemAccountAPI` 参数供测试注入；原第一个 AccountAPI 和调用方式兼容。对页面仅开放 `auth.system.listUsers(query)`、`auth.system.abandon()` 与当前身份的只读系统权限拒绝状态，不开放任意函数/URL 的 privileged request 入口。

System GET 也可能以 401 清除 Session Cookie，**必须占用现有唯一 `owner`**。在 `useSession` 内对当前授权读取包装作最小复用，区分 `kind: system`；不得建立第二队列、第二 Cookie store 或单独 Session owner，也不得把 GET 接入登录写意图的 uncertain/replay 分支。保持既有 30 秒可见等待上限；超时/abort/离页可以结束页面等待，但 owner 只在实际 fetch、body read/cancel 全部返回后释放。未结束时登录、注销、改密、公开重置及另一目录请求均不能越过该 owner。

每次读取先确认 `state.phase === authenticated`、当前完整 identity（UserID + SessionID + epoch）、当前 admin 与未被拒绝状态；捕获 identity、请求 generation 和页面 generation。只有三者仍一致、角色仍为 admin、页面仍有效时才允许发布结果。busy 只能延迟一次尚未发出的初始加载，不创建自动重试循环；失败后须明确操作。

权限和清理规则：

- 401 / `SESSION_REVOKED` 沿现有身份失效处理清空当前认证与旧目录；不能仅把列表清空后继续显示管理员壳。接收错误不确认任何旧写命令。
- 当前身份的 403 `FORBIDDEN` 立即清 rows/cursor/分页历史、隐藏系统入口并显示无权限，但不自动注销或改写 User.role。拒绝状态绑定当前 identity；由后续成功的 Session 检查重新建立当前角色，或 identity 真实变化后清除，不靠另一次点击列表重置。
- 普通网络/503/Unknown/非法响应只是读取未确认，不生成命令 key、lookup 或“原写未提交”结论。明确重试是新 GET 观察。
- App 因 `restore` 转 checking 会暂时卸载 RouterView。目录 dispose 必须只取消自己的 system 操作，不能调用会放弃个人写意图的 `personal.abandon`，也不能误取消此刻已经占 owner 的 restore、password 或 entry 操作。
- 离开目录、logout/身份失效、角色降级、SessionID/epoch 变化时清除全部目录及分页材料，递增本页 generation，停止相关 watcher。迟到成功/错误不能复活旧行、隐藏新身份导航或污染后续用户；当前身份之外的 401/403 不能投影到新身份。

`useSystemUserDirectory` 是页面期 controller，只保存列表/分页/状态，无 Cookie/CSRF、密码或另一条请求通道。普通读取没有未保存草稿，不新增离页确认。原个人/公开入口的确认和 owner 语义保持。

## 5. 分页、取消与可恢复状态

只提供“刷新”“上一页”“下一页”，每次固定读取 25 项。初次进入、身份重新确认后或明确刷新从首项开始；每次离开再进入不恢复旧页。不增加客户端搜索/排序、总人数、总页数或自动抓取全目录。

分页仅在当前页面内存保存已成功使用的 cursor 历史及当前 next_cursor。下一页使用服务端原 cursor；上一页用先前该页的输入 cursor 重新读取当前事实，不展示缓存页作为新结果。成功后才推进/回退页位置；转向旧页后丢弃失效的后续历史。刷新重置历史。只显示“本页 N 位用户”，不宣称分页过程是全局固定快照。

请求开始隐藏旧 rows，显示局部加载；保留用于这次明确重试的目标 cursor 与动作，成功一次替换整页。加载/owner 未结束时禁用重复分页与刷新，不偷偷排队。失败显示区域错误且无旧 rows，保留同身份目标供明确重试；`CURSOR_INVALID` 不自动改读首项，而提供“返回首页重新加载”。取消或离页不弹出写入结果未知提示。

空响应呈现“暂无用户”，不附尚未实现的邀请/创建按钮；读取失败、无权限与空结果是三种不同状态。显示静态、与安全错误码匹配的说明，不直接插入原始错误正文或 HTML。管理员普通入口必须展示真实加载/结果，不用空页面冒充交付。

## 6. 候选唯一文件与所有权

下面 20 路径在依赖满足后才可由主线程整体授予 frontend_worker；当前全为候选。与后端补口五路径无共享写入文件，真实测试资源仍串行交接。本规格由 architecture_worker 独占，产品作者不得顺带改旧工作项或台账。

| # | 路径 | 允许范围 |
| --- | --- | --- |
| 1 | `web/src/api/client.ts` | 单一 users GET 与专用 query 编码；原边界复用。 |
| 2 | `web/src/api/account.ts` | 仅原八字段 User parser 的导出别名。 |
| 3 | `web/src/api/system-account.ts` | 新；目录类型、严格 parser、固定 API。 |
| 4 | `web/src/composables/useSession.ts` | 第二依赖参数、窄 system 读口与同 owner/身份/权限拒绝接缝。 |
| 5 | `web/src/composables/useSystemUserDirectory.ts` | 新；本页数据、状态、分页与清理。 |
| 6 | `web/src/components/layout/SettingsShell.vue` | 标题/菜单/侧栏退出可参数化，原默认行为不变。 |
| 7 | `web/src/components/layout/SystemNav.vue` | 基于 system meta 和当前身份过滤唯一入口。 |
| 8 | `web/src/views/system/SystemSettingsView.vue` | 新；系统设置壳及非 admin/拒绝状态。 |
| 9 | `web/src/views/system/SystemUsersView.vue` | 新；真实只读目录、状态、分页和响应布局。 |
| 10 | `web/src/router/index.ts` | 父/叶路由与 meta 类型。 |
| 11 | `web/src/router/auth.ts` | 精确增加 `/system/users` 返回目标；保留原确认/身份流程。 |
| 12 | `web/src/tests/system-account-client.spec.ts` | 新；受限请求、DTO、时间/排序/cursor 与错误。 |
| 13 | `web/src/tests/system-user-directory-state.spec.ts` | 新；identity、owner/实际结束、分页和异常。 |
| 14 | `web/src/tests/system-user-directory.spec.ts` | 新；组件、路由、真实 controller 与共享壳回归。 |
| 15 | `web/src/tests/personal-settings.spec.ts` | 仅四项返回闭集的旧描述改为五项，并补新增合法项；旧正/负例和草稿断言保持。 |
| 16 | `tests/account/system_user_directory_web_test.go` | 新；下节三个真实浏览器顶层及 canonical 核对。 |
| 17 | `tests/account/system_user_directory_web_fixture_test.go` | 新；复用原 authenticationWebFixture 的私有准备/runner，不能改其预算或生产行为。 |
| 18 | `tests/account-captcha-web/system-user-directory.config.js` | 新；固定三场景、同锁定 Playwright、独占私有输出。 |
| 19 | `tests/account-captcha-web/e2e/system-user-directory.spec.ts` | 新；生产 dist 与真实后端的页面行为。 |
| 20 | `docs/development/frontend/README.md` | 接受后准确同步新叶子、五项返回闭集及实际边界。 |

`App.vue`、旧个人/公开页面及 controller、公共 Ui 组件/tokens、所有后端生产源码/OpenAPI/迁移、旧浏览器 fixture、锁文件和生产部署配置均只读。必读[Vue 开发技能](../../../.agents/skills/agenteam-vue-development/SKILL.md)、[Vue 测试技能](../../../.agents/skills/vue-testing-best-practices/SKILL.md)；独立负责人另读[验证技能](../../../.agents/skills/agenteam-verification/SKILL.md)。浏览器使用环境可用的 Playwright 技能；不可用须记录，不能虚称使用。路径不足或公共语义需扩大时先报告主线程。

## 7. 验收与资源

作者 `npm run check --prefix web` 覆盖格式、全部既有及新增 Vitest、类型和生产构建；只格式化本卡路径。新增 Go fixture 按 Go1.27.1、readonly/offline 进行 integration race compile 与适用 vet，不因纯测试新增机械重跑后端全域或两 cmd。依赖准备可恢复锁定缓存，正式检查不得改变版本/锁；不新增 npm 包。

纯测试必须有明确延迟屏障，证明目录 GET 的 fetch/body cancel 忽略 abort、30秒可见超时后，其他 Cookie 操作仍不进入；实际尾部释放后才可继续。分别验证离页、同 Session 重验、跨账号、同 User 新 Session、403/401、畸形 DTO/非规范日期/微秒相同毫秒排序、cursor 错误、上一页重读和失败不推进页。用真实 controller 加受控 transport，不能只 mock 一个已成功 resolve 的 API 冒充 owner 验证。原认证/改密/公开重置与新 GET 互斥、旧八字段/主题/草稿隔离均为门槛。

组件测试按用户可见文字、角色和交互断言，不使用只比快照的验证；fake timers 与 promises 分别推进并恢复。覆盖 `/system` 默认叶子、严格 return、新普通用户拒绝、个人草稿确认、两个 SettingsShell scope 不混淆、页码操作键盘与抽屉焦点。

| 新真实顶层 | 完整场景 |
| --- | --- |
| `TestAccountSystemUserDirectoryWebReadAndPagination` | 冻结生产 dist 通过真实 Session/admin root 读目录；至少26个自有目录记录，逐页/返回/刷新与数据库 canonical `created_at` 一致，普通参数不被拼成任意URL；页内身份、版本和时间均来自真实响应。管理员及普通账号通过已验正式流程准备，其余批量只读样本如采用测试库 fixture，必须明确标记并核字段，不作为邀请注册证据。 |
| `TestAccountSystemUserDirectoryWebAuthorityAndIdentity` | 普通用户不见入口、直接链接拒绝；目录打开后真实撤销当前 Session、当前角色失权，页面清数据与 cursor；明确换账号后旧目录不复活。权限变化仅以自有库精确 fixture 或已有正式口制造，实际 GET/Session 必须经真实后端拒绝，不能 fulfill 假403代替。 |
| `TestAccountSystemUserDirectoryWebNavigationAndLayouts` | 管理员普通入口/登录返回、个人未保存修改的继续/放弃及顶部注销；light/dark ×1440/1024/834/390，长中英名称/邮箱、列表布局、键盘/焦点恢复、reduced-motion和页面无溢出。生产无 Debug、API/缺失资产不被 SPA fallback 吞掉。 |

复用当前 `newAuthenticationWebFixture` 的任务自有同源静态生产 dist 服务器、真实 Central/PG17.x（最低17.8）/MinIO，不将测试服务器当生产托管。单浏览器45秒、Go顶层2分钟、fixture原预算、workers=1/retries=0及脚本 race/count1/每包6m保持；可按明确顶层分批，不通过延长预算掩盖失败。CSS zoom 观察不标记为原生浏览器缩放。

共享 owner/router/SettingsShell 的必要旧真实回归为 `TestAccountAuthenticationWebSessionLifecycle`、`TestAccountPersonalSettingsWebThemeAndNavigation`、`TestAccountPublicEntryWebIdentityNavigation`；其他旧通过证据可据固定输入和未变语义复用，独立负责人按实际差量决定是否追加。新/旧组采用 `scripts/test-objects.sh -run '<上述确切顶层集合>'`，不带入 Object 原停止探针或 tools 测试。未取得独占窗口不得启动 Docker、浏览器或后台服务。

冻结待验路径与必要依赖、记录实际 argv/env/exit/raw、保留原失败和精确修复差量。私有链接/密码仍走原受限 fixture 材料，不进持久截图/日志。资源结束须实际 wait、所属进程退出、自有 exact-ID 双次 absent、既有基线不变并清空私有 runtime 后交回；body 的 JSON clone 观察不能代替原 reader/cancel 实际结束证据。

未参与实现的 verification_worker 独立审查 owner/当前身份、九字段解析与权限隔离，并对冻结 dist + 已接受后端实际验证目录注册时间、普通用户拒绝和身份变化清理；作者与独立结论分别记录。主线程核验后才采纳、更新文档及提交推送。本规格静审或前端纯检查通过均不能越过页首后端依赖门槛，也不意味着邀请、Model UI 或完整 D27 已交付。
