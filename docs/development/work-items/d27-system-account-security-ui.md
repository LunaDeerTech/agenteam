# D27：System 账号安全配置 UI

状态：rev1，2026-10-06 已获独立静审通过（STATIC PASS）并由主线程采纳；被审全文 SHA256 `06602abcb845274af15f72e9f744a9487db59f156f8eda6ac397edb4f95125e5`，技术§1–7 SHA256 `e83033c44b4eff79987913dc9baf9f2a82fe4a5d77a5e175e37663d22c7dbc1a`。本次仅同步页首实施前置，技术正文保持被审原字节及采纳语义；architecture_worker仅获本规格页首唯一写权，产品实施和真实资源仍待主线程正式授予，不得自行启动。

[Selection rev2](d27-system-model-selection-ui.md)的26路径完整结果已获独立最终PASS、主线程采纳，提交推送 `870ebbb986f56bb62be34ccdfa2c77819ce995a6`，远端一致且所有资源已双清；本卡前置已满足，后继固定产品/前端基线采用该提交，沿已接受后端及迁移1–19。已只读核实该固定提交的六依赖签名（useSession第292–299行）、分域/CSRF/当前权限及实际尾部分类、公开WriteOptions/PersonalIdentity/SessionController和client严格纯工具、五叶子/九return、App期controller/Promise与View末尾确认宿主；不存在为接本卡第七依赖而新增导出或扩路径的缺口。Account后端、Account OpenAPI、迁移、共享Ui/SettingsShell/useLayer及account.ts相对原`a55e250`均无差量。

实际移交范围建议保留§6原27候选及原编号，无须增删；各新增源/测试、两固定endpoint、owner/controller/路由/菜单接入及末件README均有本卡唯一用途。固定`870ebbb`的旧纯测定位为：第13路径用户目录第210–215行五叶子；第14邀请第286行五叶子/第280行两组；第15 Provider第327/328行、第16 Model第449/450行、第17 Selection第401/402行分别为五叶子/两组，均只按本次六叶子/三组适配。第18个人设置第474行用例仍名为“nine exact return targets”，保留该路径仅精确更新十项说明及新合法/非法目标覆盖，原本人草稿/退出/失效断言保持。第23–26浏览器路径均确有必要：邀请第1528行、Provider第1192行、Model第1923及1991行、Selection第1864及1963行，共六处全部系统navigation/Drawer链接计数仅作`toHaveCount(5)`→`toHaveCount(6)`；这些浏览器位置没有分组数量适配需求，不新增其它改动用途，原其余动作/强断言/预算保持。第27路径仍在产品独立接受后最后完成。正文中旧基线、Selection待验和实施前复核表述保留原审稿状态，当前前置与实际范围以本页首为准；主线程尚未正式下发这27路径产品写权或资源窗口，规格与上游接受不代表本卡产品通过。

## 1. 完整结果与真实依赖

系统管理员在“平台配置 → 账号安全”读取并显式保存四项账号默认值，能够取消修改、处理版本冲突、确认原请求和恢复当前配置观察。只消费既有 Account 安全 singleton；不增加 SMTP、Session 管理、角色管理、邀请期限或加密/哈希参数开关，不撤销既有 Session/token。

| 依赖 | 证据与门槛 |
| --- | --- |
| Account 安全配置与正式 HTTP | **已满足**，[D07 当前范围](d07-account-session-smtp.md#当前进度)已独立接受，B04 `022dcea` 在固定基线内；消费真实根、当前管理员授权、设置版本、命令幂等及 Audit，同现有迁移1–19组合，不改后端。 |
| 系统壳、Cookie owner 与基础焦点 | **已满足**，用户目录/邀请/Provider/Model 的正式结果均在固定基线内；[Model 接受报告](../agent-team/system-models-ui-verification.md)、[共享模态恢复](d27-modal-focus-restoration.md)和[个人设置](d26-personal-settings.md)提供已验接缝。SettingsShell 支持受限两级分组，UiDialog/useLayer 不在本卡写入范围。 |
| Selection 的共享前端接缝 | **产品门槛未满足**，仅沿 [Selection §5–7](d27-system-model-selection-ui.md#5-分域owner分页与确认生命周期)约定的第六 API 依赖、独立域、五叶子/第九 return、App 期 controller 与 View 末尾确认宿主。其最终接受后核实际签名、清理分类及菜单断言，不能把约定或活动源码当验收证据。 |
| 既定产品与界面 | [账号生命周期 §5.1](../../architecture/platform-infrastructure/authentication/account-lifecycle.md#51-管理员可配置默认值)、[D07 参数及 HTTP](d07-account-session-smtp-design.md#2-工程参数与部署输入)、[系统设置 §6](../../frontend-design/layouts/system-settings.md#6-安全审计与平台配置)、[通用设置](../../frontend-design/layouts/settings-shell.md)、[样式](../../frontend-design/styles/README.md)、[组件接口](../frontend/components.md)、[D27 计划](../development-plan.md#d27-业务页面与设置)。无待用户决定的新产品含义。 |

静态接缝核对固定 `a55e250` 的 [HTTP facade](../../../internal/central/account/http_facade.go) `GetAccountSettings/UpdateAccountSettings`、[HTTP 序列化](../../../internal/central/account/http_system.go)、[命令核实](../../../internal/central/account/mutation.go)与 [Account OpenAPI](../../../api/openapi/account.json)。GET 在读取事务内重新授权；PUT 在当前授权后按原命令 key/语义核实，再裁决首次写的版本。该核对没有执行产品或数据库测试；沿用的 [既有真实用例](../../../tests/account/http_admin_test.go)已覆盖等字节重放、新 key 旧版本冲突、旧 Session 期限不变及权限撤销后禁止重放。

## 2. 页面、字段与编辑边界

仅新增懒加载 `/system/account-security`，继承 authentication/protected/systemAdmin。在既有两组之后增加稳定 key=`platform-configuration` 的“平台配置”组，仅含“账号安全”一叶子；按 Selection 契约形成六叶子、三个分组。`/system` 默认用户页不变，登录 return 闭集由九项增十项，只新增本精确路径；query/hash、数组、外部地址、动态子路径及未知叶子仍拒绝。普通用户无入口，直链不挂载本页、不派发管理 GET/PUT，不为未交付平台栏目造空页。

页面使用单个内联表单，显示当前配置版本、四个带标签/单位/合法范围的字段及保存、取消、读取当前配置。字段采用字符串输入和 `inputmode=numeric`，秒数明确标“秒”，可附可读时长说明但不隐藏舍入、截断或偷偷转换值；挑战阈值单位为“次”。不提供自动保存或“恢复默认”写入。初次 GET 成功前不以默认值填表或允许保存，读取失败/null/404不当成尚未初始化并尝试创建。

| 字段 | 默认值及前端/服务端共同边界 |
| --- | --- |
| `session_idle_seconds` | 604800；900–2592000，不大于 absolute。 |
| `session_absolute_seconds` | 2592000；3600–7776000，不小于 idle。 |
| `password_reset_seconds` | 1800；300–7200。 |
| `challenge_after_failures` | 5；1–20，不提供关闭挑战。 |

默认值只是规则说明，显示和编辑基线来自正式 GET。期限修改只影响随后签发的 Session/token，已签发对象保留原期限；挑战阈值在下一次尝试读取当前值；邀请固定24小时。保存反馈写“账号安全配置已保存”，不能写“所有登录期限已更新”或暗示已撤销 Session。

当前观察、编辑基线/四项草稿、原命令确认分别保存。无变化或非法输入零 PUT；进行中/未确认命令禁止开启另一写，保留原输入供恢复。显式取消或覆盖草稿的重载经过统一放弃确认，恢复已成功读取的当前值；没有有效当前观察时，必须重新读取成功才可开始新编辑，不用历史 PUT 结果冒充当前基线。读取失败保留同身份草稿与原确认，读取区显示失败而非伪造旧值为最新。

只在本页使用 UiField/UiInput/UiButton/UiState 和既有 tokens，成功按钮反馈沿2400ms并提供可访问状态，错误映射固定字段/安全区域文案，不显示原始服务端 detail。窄屏表单单列、长说明换行，无页面级横向溢出。页面末尾只有统一放弃确认 UiDialog，不增业务编辑弹窗；有效原触发仍在 DOM 时沿共享规则恢复焦点，已卸载节点不作为恢复目标，重挂后关闭确认仍应能用键盘访问当前页面控件。

## 3. 两项正式 API 与严格 Settings

新增 `web/src/api/system-account-security.ts`，导出只读 `AccountSecurityValues`（四字段）、`AccountSecuritySettings`（四字段加 id/version/lifetime_changes_apply_to）、`AccountSecurityUpdateInput`（四字段加 version）及 `SystemAccountSecurityAPI`。已有 `SystemAccountAPI` 用户目录接口、Account/User/Session shape 不变。

| 方法 | 唯一 wire 及成功 |
| --- | --- |
| `getSettings(signal: AbortSignal)` | GET `/api/v1/system/account-settings`，无 query/body/CSRF/key；200 `AccountSecuritySettings`，表示该次当前读取。 |
| `updateSettings(input: AccountSecurityUpdateInput, options: WriteOptions)` | PUT 同一路径，body 恰 `version/session_idle_seconds/session_absolute_seconds/password_reset_seconds/challenge_after_failures`，带原 key 和当前合法原 Session CSRF；200 `AccountSecuritySettings`，表示该命令的历史成功结果。 |

`WriteOptions` 沿既有 `csrfToken/key/signal`，只由 useSession 私有协调器构造。API 拒未知输入成员、非字符串数字、空串、前导零、符号、小数、指数、空白及超界值；交叉范围按§2。版本为1–9223372036854775807的 canonical 十进制字符串，比较及后继用 BigInt，不经 number。GET 可接受最大版本作为合法只读配置，新 PUT 的原 version 必须小于该上限；不静默溢出、钳制或更换版本。

响应恰七字段且全部必填：id 为 canonical UUIDv7；version 和四项值分别严格按上述编码/范围；`lifetime_changes_apply_to` 只能为 `newly_issued_sessions_and_tokens`。拒数组、null、缺失、未知字段、非法 id/常量或互相矛盾的期限，完整解析后一次发布不可变值。首次成功 GET 捕获真实 singleton ID；同身份后读与写结果必须匹配已捕获 ID，不能由客户端生成或从 URL 接收该身份。

PUT 成功还须逐一匹配私有原请求：id=捕获 singleton、version=原 version+1、四项值=原输入、scope 常量合法。API 完成通用 shape 与版本/值核对，useSession 在确认/发布前核 singleton 和当前请求身份；不把只满足 shape 的200当成功。历史结果允许早于后续 GET 版本，无需与当前观察相等。

client.ts 仅添加上述两固定 endpoint 的精确 method/status/options 分类；GET只能 signal，PUT只能 signal/body/csrf/key，不开放任意 URL/header/query。复用正式同源 Cookie、no-store、redirect:error、媒体/status/安全 Problem 解析及实际流清理。请求仍为 UTF-8 序列化 JSON ≤16KiB，校验与编码预算失败在创建 intent/派发前拒绝；成功及 Problem 响应继续600000B，仅原 listProviders 成功2MiB例外保持，不放大全局预算。页面/日志/错误对象/URL/history/storage 不保存 key、CSRF、完整请求或 raw response；配置值只在必要草稿与安全文本中使用。

## 4. 历史成功、当前观察与无 lookup 恢复

固定 facade 的命令身份为 `account.settings`、owner=当前 UserID、name=`settings-update`，语义摘要绑定原 version 与四项值。`lookupMutation` 每次在事务内重新核当前管理员和语义；已提交同 key 直接投影原 resource、expected+1、四项原值，核 committed/COMPLETED 和 actor/command/版本。首次写在同事务内核 singleton/version并持久命令、设置、typed Audit 和活动触碰；Unknown 由服务先查原命令，确认后返回同一投影。PUT 后没有另读当前配置。不同 key 的旧 version 冲突，旧 key/不同 body 为幂等语义冲突；本卡不改这些后端事实，也不添加事件或 Session 撤销。

**没有公开 Account command lookup。** `retry_hint=lookup` 不授权虚构 URL、调用 Model lookup 或将 GET 当命令查询。“检查当前会话与配置”只显式检查一次 Session；同完整 identity/原 CSRF/当前 admin 仍成立时，再最多 GET 一次当前配置。该观察即使字段相同、版本升高或发生冲突，也不证明原请求接受/未接受。没有后台轮询、自动重发、新 key 探测或跨刷新持久队列。

新增私有不可变 intent 捕获完整 identity、singleton ID、原 version/四项值、稳定 key、原 CSRF 与完整序列化 body。首次派发前完成所有输入校验；重复点击不增 key、不排队。恢复沿 [个人设置 §6](d26-personal-settings.md#6-unknown离页与敏感数据)与 [Selection §4](d27-system-model-selection-ui.md#4-保存版本竞态与原命令恢复)的同 owner 纪律，但确认载体仅使用本卡历史 Settings：

- 未派发的本地失败/busy，以及没有更早不确定尝试、且明确 not_started/not_committed 的 INVALID_ARGUMENT/字段错误/VERSION_CONFLICT 等业务拒绝，按已知失败显示并保留输入。只有明确拒绝才允许修正后开始新 intent，不把全部非200统称未知。
- 已派发后的 transport、超时/取消、截断/非法响应、5xx、RESOURCE_BUSY、COMMIT_UNKNOWN，或没有匹配成功结果的 unknown/committed Problem，保持“请求结果未确认”。原 intent 曾未确认时，后续拒绝或读取失败不消除历史不确定性。IDEMPOTENCY_KEY_REUSED 保留原 body/key并提示冲突，不能自动换 key。
- “重试原请求”只在同完整身份/原 CSRF 经当前确认且原 intent 完整时，显式再次执行相同 PUT/body/version/key。后读较新版本不得挡住历史重放，也不得用当前草稿或 GET version 改造原请求。后端会重新授权；UI 不借后端 UserID 级命令身份把旧 intent 移交同用户的新 Session。
- 严格匹配200立即记录“原请求已确认”，清除不再需要的恢复意图；随后至多一次 GET 刷新当前配置。历史 Settings 单独作为该次确认，不能覆盖更高版本的当前观察，也不能以其版本发起下一次新写。后读失败显示“保存已确认，当前配置读取失败”，保留确认，仅重读当前配置，不再提交该写。
- “放弃本次操作”明确此前提交仍可能生效，只停止客户端跟踪，不撤销服务器提交。放弃后仍等实际 owner 收尾，再显式 GET 成功、重新编辑才能生成新 key；不存在未确认命令的自动回滚或补偿。

明确版本冲突保留四项草稿和原基线，允许“读取最新配置”查看当前值而不修改草稿/原版本；用户核对后，显式采用最新值重新编辑才重建编辑基线，该丢弃动作仍经统一确认。不得自动合并、静默改 expected version 或重新保存。冲突期间再有并发保存，下一次新写仍由后端裁决；曾未确认的原 intent 必须先保留恢复或明确放弃，不能用冲突流程清除它。返回较新的当前观察只更新观察，不能复写正在编辑的草稿。

## 5. 共享 owner、身份与确认生命周期

Selection 接受的前六个依赖后追加第七个可选 `SystemAccountSecurityAPI`（默认正式工厂），保留原参数顺序和调用方。新增明确 `account-security-read/write` 分类、独立 revision/intent/progress；不得落入 personal、Provider/Model/Selection 默认分支。两 API、当前 Session 检查及其它域仍由唯一 Cookie owner 串行；controller/View 不直接 fetch，不增第二队列、认证缓存或任意 privileged callback。

原30秒可见预算不变。取消、放弃、超时或身份变化可结束可见等待并隔离代次，但 fetch/body read/cancel 的实际尾部与 finally 完成前不释放 owner，也不能用新身份越过旧尾部。初次加载遇 owner 忙且尚未派发，可延迟一次；失效/离页后不续发、不循环重试。各域 abandon 双向隔离，账号安全清理不能撤个人、用户目录、邀请、Provider、Model 或 Selection；反向亦同。App.dispose/auth.leave 的全局清理含义和原退出协议保持。

新增写纳入当前完整 identity+owner generation 的 CSRF_FAILED 失效护栏；当前/同身份晚到401沿已有 Cookie 清理纪律处理，旧身份/代次失败不得污染新账号。当前403清全部系统私有状态（含本域草稿/intent/观察），设置身份绑定的拒绝，不擅改 User.role或自动登出；再由成功 Session 重验恢复资格。清除本地状态不声称服务器回滚。真正换账号、同 User 换 Session/epoch、CSRF上下文失效、失权或注销销毁旧意图；同 Session 临时 checking 只隐藏受保护内容并保留 App 期状态。

App 创建/provide `createSystemAccountSecurity`，拥有观察、编辑基线/草稿、确认状态与 Promise。View 只承载 DOM，在本叶子且身份确认后 attach；dirty 包含字段变化、冲突草稿、进行中/未确认写。取消、显式覆盖重载、菜单/本人入口/退出和浏览器返回统一询问“继续编辑 / 放弃修改”，路由确认先于 Session 重验；beforeunload 沿原原生提示，不承诺强关保存。

放弃确认 UiDialog 固定在本 View 模板末尾，App 不增常驻确认宿主。checking 卸载 RouterView 时确认随 View 全部撤离，App Promise/草稿保留；检查失败时原 App 恢复按钮可用，无残留 overlay/inert/滚动锁。同完整身份/admin恢复后同序重挂，确认仍可见、顶层、可真实响应，零自动 PUT/答案。确认打开时 mounted 标题不抢焦点；任何 await 后 DOM 操作先核实例仍挂载与目标当前可用，关闭确认只消费已验共享恢复，不用全局选择器、手动 focus 或反复重挂修层序。真实离页/新身份/失权/dispose以false结算待决确认并清旧状态，旧 route/退出续体不得移交新身份，继续/放弃至多结算一次。

## 6. 唯一候选路径与实施移交

以下27条仅为规格候选。Selection 最终接受后，主线程先核两API接入、实际第六依赖与分域分类、五叶子/九return及下列旧测试断言；无必要的旧测试候选可剔除，不能为凑数修改。共享产品文件和测试/README与 Selection 串行，未通过的活动实现不作为本卡依赖。

| # | 路径 | 唯一用途 |
| --- | --- | --- |
| 1 | `web/src/api/client.ts` | 两固定 Account 安全 endpoint/精确 options；保持旧预算与实际清理。 |
| 2 | `web/src/api/system-account-security.ts`（新） | 两 API、闭合输入/Settings解析、历史成功匹配。 |
| 3 | `web/src/composables/useSession.ts` | 第七依赖、本域独立 owner分类/intent/CSRF与系统失权接入。 |
| 4 | `web/src/composables/useSystemAccountSecurity.ts`（新） | App期观察/草稿/历史确认、冲突复核、页面/身份与确认 Promise。 |
| 5 | `web/src/App.vue` | provide、既有导航/顶部退出 hook、dispose组合，不加确认Dialog宿主。 |
| 6 | `web/src/router/index.ts` | 账号安全正式叶子，原权限/默认页保持。 |
| 7 | `web/src/router/auth.ts` | 第十精确return及本域离页确认/完成hook，原退出协议保持。 |
| 8 | `web/src/views/system/SystemSettingsView.vue` | 新平台配置组和唯一账号安全叶子，原五叶子保持。 |
| 9 | `web/src/views/system/SystemAccountSecurityView.vue`（新） | 内联表单、当前/历史结果、安全反馈、末尾确认宿主与局部布局。 |
| 10 | `web/src/tests/system-account-security-client.spec.ts`（新） | 两 wire、严格范围/Settings/原成功核对、预算及传输尾部。 |
| 11 | `web/src/tests/system-account-security-state.spec.ts`（新） | 生产controller+受控transport，原请求/冲突/当前观察/双向owner与身份屏障。 |
| 12 | `web/src/tests/system-account-security.spec.ts`（新） | 真实App/router/controller、表单/确认重挂与菜单/return行为。 |
| 13 | `web/src/tests/system-user-directory.spec.ts` | 仅六叶子/第十return必要兼容，默认用户与权限强断言保持。 |
| 14 | `web/src/tests/system-invitations.spec.ts` | 仅菜单六叶子/分组2→3与新增导航合法目标适配；原写/恢复/cohost不改。 |
| 15 | `web/src/tests/system-providers.spec.ts` | 同上菜单/分组兼容，原两步凭据/恢复强断言保持。 |
| 16 | `web/src/tests/system-models.spec.ts` | 同上菜单/分组兼容，原CRUD/impact/替代/恢复强断言保持。 |
| 17 | `web/src/tests/system-model-selection.spec.ts` | 待上游接受后核：仅本次菜单/分组/合法目标兼容，原选择器引用和恢复断言保持。 |
| 18 | `web/src/tests/personal-settings.spec.ts` | 九项return增十项及新合法/非法目标，原本人草稿/退出保持。 |
| 19 | `tests/account/system_account_security_web_test.go`（新） | 下节真实顶层及命令/settings/Audit/签发期限旁证。 |
| 20 | `tests/account/system_account_security_web_fixture_test.go`（新） | 复用正式自有fixture、专用launcher及精确有界响应控制，不改旧fixture。 |
| 21 | `tests/account-captcha-web/system-account-security.config.js`（新） | 专用Playwright配置/私有输出，原预算不变。 |
| 22 | `tests/account-captcha-web/e2e/system-account-security.spec.ts`（新） | 生产dist+真实后端的配置/恢复/冲突/权限/导航和布局。 |
| 23 | `tests/account-captcha-web/e2e/system-invitations.spec.ts` | 暂候选：仅旧Navigation的全部Drawer链接5→6；固定a55e250:1528为4，Selection约定改5，须核接受源；其余断言保持。 |
| 24 | `tests/account-captcha-web/e2e/system-providers.spec.ts` | 暂候选：仅旧Navigation的全部系统链接5→6；固定a55e250:1192为4，Selection约定改5；其余断言保持。 |
| 25 | `tests/account-captcha-web/e2e/system-models.spec.ts` | 暂候选：仅旧Navigation在a55e250:1923/1991两处全系统/Drawer链接，Selection约定4→5后本卡5→6；其余断言保持。 |
| 26 | `tests/account-captcha-web/e2e/system-model-selection.spec.ts` | 待上游接受后核：仅实际存在的全系统/Drawer五叶子断言5→6及分组2→3；不存在则剔除，不新增其它用途。 |
| 27 | `docs/development/frontend/README.md` | 产品独立接受后最后同步账号安全页、十项return、实际命令/证据及未交付边界。 |

旧纯测在固定基线中已有精确菜单文本和 `.settings-group-toggle` 两组断言；新增真实平台分组必须适配，不能通过重复分组/CSS隐藏、削减链接或降低断言规避。实施前核第13–18及23–26的接受后准确位置/数量，超出上述用途或其它必要源改动先报主线程修卡。必读[Vue技能](../../../.agents/skills/agenteam-vue-development/SKILL.md)、[Vue测试技能](../../../.agents/skills/vue-testing-best-practices/SKILL.md)；独立验收用[验证技能](../../../.agents/skills/agenteam-verification/SKILL.md)和可用Playwright技能。后端/OpenAPI、迁移、SettingsShell/Ui/useLayer、全局CSS、旧API/controller/View、旧fixture/driver、脚本、锁和归档只读；零DDL/新依赖，不触台账/continuation或停止任务。

## 7. 高风险验收与资源门槛

实际开工先固定 Selection 完整接受提交、原27候选的最终取舍及精确依赖清单。作者运行 `npm run check --prefix web`，仅格式化授权路径；Go1.27.1/local，`GOPROXY=off GOSUMDB=off GOFLAGS=-mod=readonly` 下精确 integration-tag race 编译与适用 vet，依赖准备单列。纯测、静审和编译不冒充真实 HTTP/权限/事务或浏览器通过。

纯测覆盖七字段Settings/五字段PUT的闭集、所有边界与idle≤absolute、canonical UUID/整数字符串、最大版本只读和禁止新写、scope常量、匹配及不匹配id/version+1/四项值。核本地非法输入/无变化/重复点击零派发，16KiB/600000B及其他endpoint预算不变，非法/截断/超限响应的body/read/cancel实际收尾。原PUT成功是历史值，GET是当前值：先观察v3再接v2历史确认不得退回v2，确认后GET失败不重发，GET相同值不能确认未知写，业务拒绝与曾未确认分列，原key/body/version不可修改。

生产controller+受控transport设置显式异步屏障，分别阻塞fetch、body read与cancel；30秒可见结束/取消/放弃/身份变化后仍忙，迟到零发布，实际join才释放，后续GET/PUT/Session/其它域不能越过。双向核个人、users、邀请、Provider、Model、Selection的abandon隔离；覆盖当前和迟到401/403/CSRF、同User新Session/换账号、原请求恢复前Session检查及原CSRF不符销毁。冲突读取保留草稿，只有用户明确采用当前值后才可建立新意图，不自动重基。

页面纯测先打开dirty或未确认的放弃确认，再向真实App listener合成pageshow并保持Session GET待决；checking零本域overlay，失败后App恢复按钮可操作，同身份恢复确认仍顶层/可响应且零PUT。通过按钮完成继续/放弃各一次，覆盖待决路由/退出、新身份/失权/dispose的false终点、断开的旧View续体与标题不抢焦点。jsdom状态/DOM证据和真实浏览器焦点分列，不直接结算Promise替代交互。

| 新真实顶层 | 必须覆盖 |
| --- | --- |
| `TestAccountSystemAccountSecurityWebLifecycle` | 正式GET加载、四字段编辑保存、取消/无变化零PUT、刷新持久值、范围及交叉校验、明确生效说明。准备值经正式GET/PUT，沿真实fixture可能已将挑战阈值改1的事实，不把fixture准备当默认5证明；新库默认沿D07既有证据。真实组合核修改前Session期限不变、新正式登录Session取新期限；不等待长时间过期，不宣称撤销旧Session或重跑邮件投递。 |
| `TestAccountSystemAccountSecurityWebConcurrency` | 两个真实管理员会话读取同版本，各用独立key保存不同合法值，只有先提交成功，后者真实409且四项输入保留；显式读取/核对/采用当前值后重新编辑保存。旁证singleton、命令和Audit唯一/原子，拒绝时无部分字段修改；不直接SQL制造设置版本或值。 |
| `TestAccountSystemAccountSecurityWebOutcomeRecovery` | 自有同源服务器在真实PUT成功后有界丢失响应；原页保留未确认，通过另一正式会话再保存较新配置，检查Session/GET只显示当前观察。原key/body/version显式重放得到原历史Settings且不覆盖当前较新值，原命令和Audit仍唯一；确认后精确GET一次失败仍保留写确认。另核明确放弃不回滚、无lookup调用、未派发和可确认拒绝/未知分列，不注入DB提交故障。 |
| `TestAccountSystemAccountSecurityWebAuthorityAndIdentity` | 普通用户无入口/直链零管理请求；正式Session撤销、当前admin失权后真实GET/新PUT/原请求重放均拒绝并清旧状态，同User新Session/换账号无旧草稿/intent。沿既有自有精确身份的权限fixture纪律，不fulfill假403冒充授权；新增写CSRF与实际Cookie尾部具独立证据。 |
| `TestAccountSystemAccountSecurityWebNavigationAndLayouts` | 三组/六叶子、默认用户、第十return、平台组展开/active/键盘和dirty/未确认离页。确认已开时合成pageshow，精确Session GET一次失败、App恢复同Session后实际点击/键盘可响应确认，继续保留草稿、放弃按原导航且零新PUT。light/dark×1440/1024/834/390、长说明/字段错误、Drawer及确认的Tab/Escape/遮罩/焦点、reduced-motion与无溢出；合成事件不冒充真实BFCache。 |

全部账号、Session、设置与冲突准备使用已接受正式 HTTP/服务；除沿旧纪律对任务自有精确账号设置管理员资格/撤销资格的权限负例外，不写业务表。SQL仅旁证自有settings/commands/Audit/Session期限，不输出Cookie、CSRF、密码或语义MAC。复用已验完整根和隔离PG/MinIO fixture，正式dist同源；无新增SMTP实投、外部Provider、Runtime或停止任务依赖。五新组可按精确名称分组运行 `scripts/test-security.sh -run '^TestAccountSystemAccountSecurityWeb(Lifecycle|Concurrency|OutcomeRecovery|AuthorityAndIdentity|NavigationAndLayouts)$'`；Playwright每test45秒、Go顶层2分钟、workers=1/retries=0、race/count1/每包6分钟保持，按实际耗时分组，不放大预算/削断言或把no-tests当通过。

旧真实回归沿 Selection §7 已列的认证SessionLifecycle、个人ThemeAndNavigation、公开IdentityNavigation、用户AuthorityAndIdentity/NavigationAndLayouts、邀请OutcomeRecovery/NavigationAndLayouts、Provider CredentialReplacement/OutcomeRecovery/NavigationAndLayouts、Model DeletionAndReplacement/OutcomeRecovery/NavigationAndLayouts，再覆盖 Selection 最终接受的 OutcomeRecovery/AuthorityAndIdentity/NavigationAndLayouts。实施时核完整正式测试名，按语义和输入未变复用证据；旧browser只改上述真实菜单计数，Tab困陷、Escape/遮罩、焦点、cohost、权限/事务和时间强断言全部保持。

仅在 Selection 作者/独立验收命令实际停止、资源双清且完整接受后，由主线程另授本卡唯一作者与独占窗口。作者冻结精确源/dist/锁/环境，保留首红和实际argv/env/退出/安全原log，交未参与实现的verification_worker独立验收权限、原请求恢复、竞态与浏览器。每轮实际wait、server/browser与所属进程join、自有exact-ID双次absent及旧基线不变后交回；不占活动资源或归档。产品独立通过并主线程采纳后才写第27路径。SMTP及其它平台栏目、完整D26/D27、生产SPA/真实Vite代理、Runtime与ready503边界保持；Summary未答仍待定，Object/tools原停止任务不重建。
