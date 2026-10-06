# D26 邀请兑换与找回、重置密码公开入口

修订：rev5，2026-10-06，仅增加 §14 的同一改密接口两个400观察点补遗，仍为25路径。原rev1–4正文和失败记录保留；只修测试正文采样，字段错误、后续业务断言、生产输入及预算不变。精确候选仍先经独立核验及主线程审阅，再定向复验。

修订：rev4，2026-10-06，仅增加 §13 的第二个既败旧响应观察点补遗，路径总数仍为25。原rev1–3正文与记录保留；新增范围只涉及旧密码拒绝登录的单次401 JSON采样，原产品规则、业务断言、预算和资源门槛保持。补遗及精确候选仍先交主线程审阅。

修订：rev3，2026-10-06，仅增加 §12 的旧主题回归响应采样补遗：在rev2的24路径上加一条旧浏览器测试，共25路径。下列rev1/rev2正文与冻结记录保留；本次只允许单个已失败409观察点的测试采样修复，不改产品、业务断言或预算。本补遗及候选差量先交主线程审阅，再归位受影响测试。

修订：rev2，2026-10-06，仅增加 §11 的测试接缝补遗：原23路径加一条旧测试fixture的私有日志路径元数据，共24路径。下文 rev1 授权与 §1–10 保留原冻结记录；当前窄例外以 §11 为准，产品规则与既有验收门槛不变。本补遗及精确候选差量先交主线程审阅，再归位新测试输入；不宣告页面或整卡验收完成。

修订：rev1，2026-10-05，已独立规格静审通过并获主线程采纳；被审原卡 SHA-256 `3f3e3a9624af7b73ed8a5082d74e85c6a7f3631286892352e209c2e0b05008d2`。设计者 `d08_recovery_design`；规格已采纳提交推送 `e5a5ccf5343fe9f17aced6e9ee0d633fbfe3c5aa`，远端一致由主线程确认，原件见[规格持久记录](../agent-team/public-account-entry-spec-verification.md)。主线程现已正式授权 `d08_registry_backend` 按 §8 精确23路径实施，`skill_verification` 独立验收；作者仅获私有 offline/pure/type/build 权，未获 Docker/browser 运行权。固定业务基线为已验个人设置提交 `c54f73f3324caa11608d84e5d207141985eb6074`；认证前置为 `9a710f272026b41ef69852bbeb41cb7670b500a8`。已采纳精确23条实施范围，无后端生产、SQL、包依赖或迁移。本次仅归位规格归档与正式实施授权，不新增资源授权；§1–9 技术正文逐字保持，其中候选/待另授表述保留规格冻结时含义，当前行政状态见本段及 §10。

依据为 [D07 Account](d07-account-session-smtp.md)、[D07 工程规格](d07-account-session-smtp-design.md)、[认证卡](d26-account-authentication.md)、[个人设置卡](d26-personal-settings.md)、[账号入口布局](../../frontend-design/layouts/account-entry.md)、[账号生命周期](../../architecture/platform-infrastructure/authentication/account-lifecycle.md)及 [SMTP Delivery](../../architecture/platform-infrastructure/authentication/smtp-delivery.md)。主线程已采纳静态可行性报告 `/workspace/agenteam-public-account-entry-feasibility-06zo4kp2/report.md`，SHA-256 `814f5a368c802567d3eb9571120284493a7a0aad10fe941050b2a704e33da829`；本卡已用个人设置最终源码核定其待定接缝，不沿用估算范围。固定31项 Git 输入定位见 `/workspace/agenteam-public-account-entry-spec-shkumzgp/inputs.json`，SHA-256 `913cc43664f17c151f7a6fa4c58837858ef412494b17875eea3491f18d8a6c6f`，仅为静态输入。

必读 [设计技能](../../../.agents/skills/agenteam-design/SKILL.md)、[Vue 开发](../../../.agents/skills/agenteam-vue-development/SKILL.md)、[Vue 测试](../../../.agents/skills/vue-testing-best-practices/SKILL.md)、[前端基础](../frontend/README.md)、[组件接口](../frontend/components.md)及[样式规范](../../frontend-design/styles/README.md)；后续真实浏览器阶段另读可用 Playwright 技能。

## 1. 完整结果与路由

交付实际生产 dist 连接真实 Account HTTP 的三条公开流程：邀请链接校验、填写账号及兑换；找回申请及统一渠道说明；重置链接校验、设置密码及返回登录。三者共用匿名 Browser Cookie、原写入意图和既有 Cookie 请求所有者，合为一张完整结果卡，不拆成协议或表单 helper 结果。

路由固定为 `/invite`、`/forgot-password`、`/reset-password`，均为独立居中面板，不显示 AppShell/已登录导航。前后两条链接路径由 D07 正式链接规定；找回路径为本卡工程选取。登录页新增真实“找回密码”入口；没有公开注册入口、管理员创建/撤销邀请 UI、投递诊断或管理员重设特例。找回成功保留邮箱，允许用户明确再次请求；不自动轮询或重试。

保留现有登录返回闭集 `/`、`/settings/profile`、`/settings/appearance`、`/settings/password`，不新增公开表单、Project、query/hash 或任意外部 URL 为 return 目标。普通 `/login` 在当前有效 Session 下仍沿既有行为回到净化目标。仅新增 §6 的显式 `/login?switch=1` 身份选择状态；`switch` 必须是单一字符串 `1`，不是认证、注销或允许跳转的凭证。

不涉及 Project/D25、未决 Summary、Model/Invocation、管理员用户设置或生产 SPA 托管。已知 Object Runtime join 缺陷及暂停修复、Artifact/领域绑定阻断、ready503 及完整 D26/D27 未完成的边界保持；本卡普通 Account root/browser 退出不证明共享 guard 缺陷已修。

## 2. 已验前置与实际接缝

个人设置24源已独立验收并提交推送 `c54f73f`。静态比较 `9a710f2..c54f73f` 的 `internal/central/account`、`internal/central/app`、两份 Account/common OpenAPI、`tests/testsupport/accountenv` 均零差量；后端五个公开入口及真实 Account/Secret/Audit/Outbox/Model 初始化与 root 不需增量。固定源码是依据，旧卡页首未归位的历史状态不把该已验提交降为活动候选。

| 固定实际接缝 | 本卡必要增量与保留责任 |
| --- | --- |
| `api/client.ts` 的闭集 endpoints / `accountTransport`，`api/account.ts` 的 `createAccountAPI`、`AccountAPI=ReturnType` | 在同一工厂增加五个 POST 和严格 DTO；沿同源、Problem、JSON 上界及实际 body 清理。原14方法不改签名/含义，无自由 URL 或通用身份 setter |
| `useSession.ts:109,317,661,1003` 的 controller、`run`、`runPersonal` 与返回对象 | 已有一个 `owner`；原 `run` 的 void/no-op 不能当新 DTO 成功。新增 typed `entry` 门面仍使用该 owner，保留30s可见上界和实际尾部。不能另建队列、创建第二 AccountAPI 实例或嵌套 run/restore |
| `useSession.ts:134–245,357–410` 的稳定 personal identity、`publish`、`bootstrap`、`restore` | `publish` 当前清匿名上下文，`bootstrap` 当前改成 anonymous；公开流程须在同一实例中使合法 Session 与匿名 CSRF 并存。只作本卡所需的私有分支，既有认证/个人设置行为与 stable epoch 保持；CSRF、key、原意图不进入公开状态 |
| `router/auth.ts` 的 `installPersonalNavigation`、`installAuthentication`，`App.vue:19–25` 的 logout/visibility | 先完成原设置 dirty 确认，再进入公开域；公开域的准备/离页由本卡 owner 处理，不能调用普通 restore 旋转正在使用的匿名上下文。App 持有页面 owner 与确认框，公开页不因全局 checking 临时消失 |
| `LoginView.vue` 的 authenticated watcher / unmount | 普通行为保持；显式 switch 状态显示身份选择，不能自动注销。离开登录去公开页不能由旧 unmount 的 `auth.leave()` 擦掉刚建立的公开上下文；非认证域离开仍清理 |
| 四份完整 mock | `session.spec.ts`、`authentication.spec.ts`、`personal-settings-state.spec.ts`、`personal-settings.spec.ts` 均向 controller 注入完整 AccountAPI；各补五成员，意外调用明确拒绝。只适配类型/新增可用入口，不能 optional 空成功或降低旧断言 |
| `newAuthenticationWebFixture` 与 `authenticationWebFixture.record` | 最终设置提交已保留绑定的私有 `old.record`；可在新同包 helper 复用真实 root/dist/随机 `f.origin`。旧 fixture 与 launcher 只读，不再申请其路径，不复制 root、不改表造身份 |

`usePersonalSettings`、`useTheme`、共享 UI、既有生产 root 和旧 Go/浏览器测试保持只读。若冻结输入、实际导出或实现必要范围出现影响差量，先报告证据并修卡，不自行扩权。当前前置已实现不代表本卡已实现；仍须独立规格审查及另行实施授权。

## 3. 五个 typed HTTP 方法

唯一 wire 来源为固定 [account.json](../../../api/openapi/account.json)、[common.json](../../../api/openapi/common.json)与 `account/http.go`、`http_auth.go`、`csrf.go`、`http_wire.go`。复用既有 `WriteOptions`、`AccountFailure` 与同文件的严格 email/Instant/CSRF/shape 解析；不复制另一宽松客户端。

```ts
type DeliveryChannel = 'smtp' | 'backend_log'
type LinkInput = Readonly<{ token: string }>
type BrowserReadOptions = Readonly<{ csrfToken: string; signal?: AbortSignal }>
type InvitationInspection = Readonly<{ email: string; expires_at: string }>
type InvitationRedeemed = Readonly<{ completed: true; login_required: true }>
type InvitationInput = Readonly<{
  token: string; username: string; display_name?: string
  password: string; confirmation: string
}>
type ResetAccepted = Readonly<{ accepted: true; delivery_channel: DeliveryChannel }>
type ResetInspection = Readonly<{ valid: true; expires_at: string }>
type ResetInput = Readonly<{
  token: string; new_password: string; confirmation: string
}>
interface PublicAccountMethods {
  inspectInvitation(input: LinkInput, options: BrowserReadOptions): Promise<InvitationInspection>
  redeemInvitation(input: InvitationInput, options: WriteOptions): Promise<InvitationRedeemed>
  requestPasswordReset(input: Readonly<{ email: string }>, options: WriteOptions): Promise<ResetAccepted>
  inspectPasswordReset(input: LinkInput, options: BrowserReadOptions): Promise<ResetInspection>
  completePasswordReset(input: ResetInput, options: WriteOptions): Promise<void>
}
```

| POST 相对路径 | 请求与严格成功 |
| --- | --- |
| `/api/v1/invitations/inspect` | LinkInput；200 InvitationInspection |
| `/api/v1/invitations/redeem` | InvitationInput；201 InvitationRedeemed，不含 User/Session/CSRF |
| `/api/v1/password-resets/request` | `{email}`；202 ResetAccepted，不含 job ID/存在性/投递结果 |
| `/api/v1/password-resets/inspect` | LinkInput；200 ResetInspection，不含邮箱/目标身份 |
| `/api/v1/password-resets/complete` | ResetInput；204 无 JSON body |

五条均为 `browser` authority：浏览器自动带现有 Cookie，客户端只用**匿名** CSRF header，不改用 Session CSRF。inspect 不发 Idempotency-Key；三个写只发本协调器原 key。Origin/Host/Cookie/Fetch-Site 由真实浏览器与服务端核验，前端不伪造。邀请 HTTP 在含 Session Cookie 时还真实核当前 Session；另一身份被拒绝，不能隐藏 Cookie/换 fetch credentials 绕过。

链接 token 只接受正式 UUIDv7 加 `.` 加43字符 RawURL 字符串，完整80字符；不把 query、百分号二次解码、额外片段或任意 URL 当 token。客户端格式检查不是验证有效/归属，仍须真实 inspect。email 沿 D07 裸 ASCII 地址、≤254bytes、无首尾空白；不静默 trim。新用户名3–32 ASCII 字母/数字/中间连字符，服务端负责规范小写、保留字及唯一性。display_name 可缺省或空，最多80码点/320bytes且无控制字符，不 trim。

两种设密表单均为15–128 Unicode码点、≤512 UTF-8 bytes、两次完全一致；允许中文/空格，不作正规化、字符组合规则或 UTF-16 maxlength 截断。提交事件同步冻结实际最终字段，覆盖自动填充、最后字符、点击和 Enter；禁在 await 后重新读取可变 draft。保留原字段值，弱密码/占用以服务端结果为准。JSON 仍为16KiB请求边界，成功 status/media/shape/字面 true 不符一律坏响应；不能把 HTML/重定向/缺字段当成功。

Problem 保留 code、commit_state、field_errors、retry_hint、request_id；显示安全固定文案及需要的请求编号，不输出 raw body/底层异常或 token/password。字段 `/username`、`/display_name`、`/email`、`/password`、`/new_password`、`/confirmation` 正确归属；重置底层 `/password` 表示新密码错误，不丢掉该反馈。未知字段/码为区域错误，不能猜成功。410 RESOURCE_DELETED 统一链接不可用，不区分存在/已消费；403 FORBIDDEN 与 CSRF_FAILED、401 当前 Session 失效分开。429 不造冷却倒计时；503依赖失败可显式重试，但不能掩盖已有 uncertain。

## 4. 链接捕获与材料生命周期

新增 `router/account-link.ts` 只负责固定 `/invite`、`/reset-password` capability 的初次/后续浏览器 fragment 捕获。`router/index.ts` **建立 createWebHistory 前**安装；不能只在 main 的 mount 前调用，因为静态 router import 更早执行。先移除 fragment，再解析/准备/发 POST；无效、超长也移除且不发送。保留路径及净化后的非敏感 return，丢弃其它公开入口 query，不把 token 换存 query、route params、history.state、localStorage/sessionStorage、Cookie、DOM属性或页面文本。

捕获材料只在模块/页面 owner 私有闭包中转交，组件不接收 token prop，不把它挂全局 reactive state。最多保留当前链接与一个待确认替换链接；新 fragment 覆盖待替换值时清旧引用，不能累计。为同标签页打开新链接、hashchange/popstate 与同路径替换安装同一窄监听，监听顺序早于 history 消费；清除 URL 后才通知页面 owner。重复事件不重复 consume/inspect；新链接不能在原写实际未结束或旧 dirty 未明确放弃时覆盖其意图。取消替换保留旧表单/意图，丢弃新链接；确认替换后按 §5/§6 收束旧 owner 才进入新链接。

保持非敏感 history 导航信息与前进/后退行为；任何新写入的 history state 不得包含 capability。输入地址中最初 fragment 是原外部链接，不声称浏览器从未接收过；要求应用接管后立即清除、刷新/回退不重新显示或再次发送已消费材料。刷新后无内存 token 显示“请重新打开原链接”，不提供恢复 API。URL 清除失败则拒绝 POST 并给安全错误，不继续带着敏感地址操作。

确认成功、明确放弃、实际离页/新链接替换、dispose 后清密码/token/draft与监听器；JavaScript 仅释放引用，不宣称可擦除引擎内存。Unknown 只在当前私有 owner 保留原材料供显式重试，页面显示为锁定待确认；不复制到日志、测试结果或错误对象。实际请求的已捕获输入及 body/cancel 尾部由原 owner 留到返回，清页面不假称后端回滚。

## 5. 同一 Cookie owner 的公开门面与状态

在 `createSessionController` 返回对象新增 `entry`，不改原 auth/personal 公共签名。anonymous context epoch 是本地不透明代际，并非读取 HttpOnly BrowserID；只有同一成功 bootstrap/CSRF上下文可重放该 Browser owner 的原写。Session 稳定身份沿已有 `PersonalIdentity`，与匿名代际和每次请求 generation 分层，不能拿 personal session identity 代替 reset 命令 owner。

```ts
type EntrySession =
  | Readonly<{ kind: 'anonymous' }>
  | Readonly<{ kind: 'authenticated'; identity: PersonalIdentity }>
type EntryPreparation = Readonly<{
  deliveryChannel: DeliveryChannel; session: EntrySession
}>
type ResetConfirmation =
  | Readonly<{ commandConfirmed: true; session: EntrySession }>
  | Readonly<{
      commandConfirmed: true; session: Readonly<{ kind: 'unconfirmed' }>
      failure: AccountFailure
    }>
type EntryMutationResult =
  | Readonly<{ kind: 'invitation'; value: InvitationRedeemed }>
  | Readonly<{ kind: 'reset-request'; value: ResetAccepted }>
  | Readonly<{ kind: 'reset-complete'; value: ResetConfirmation }>
interface PublicEntrySessionMethods {
  readonly progress: Readonly<{
    kind: 'invitation' | 'reset-request' | 'reset-complete'
    phase: 'submitting' | 'uncertain' | 'confirmed' | 'confirming-session' | 'session-unconfirmed'
    canRetryOriginal: boolean
    contextValid: boolean
  }> | null
  prepare(): Promise<EntryPreparation>
  inspectInvitation(input: LinkInput): Promise<InvitationInspection>
  redeemInvitation(input: InvitationInput): Promise<InvitationRedeemed>
  requestPasswordReset(input: Readonly<{ email: string }>): Promise<ResetAccepted>
  inspectPasswordReset(input: LinkInput): Promise<ResetInspection>
  completePasswordReset(input: ResetInput): Promise<ResetConfirmation>
  retryOriginal(): Promise<EntryMutationResult>
  abandon(): void
}
```

`prepare` 在同一次原 owner 中直接 GET Session，200按真实 view 更新稳定身份，认可的401清当前 Session事实；其它失败不假匿名。随后仅在未持有可用匿名上下文时调用 bootstrap，并保留已确认 Session/其CSRF。若已有可用匿名CSRF则复用，不为取渠道盲目旋转；保存该成功 bootstrap 的 channel 供准备说明，reset request 的真实202 channel才是该次正式回执。匿名 bootstrap 的成功不自动注销/覆盖仍有效 Session；全局 theme/个人草稿按真实身份规则处理。private publish 的本卡分支允许保留匿名上下文，原普通 login/restore 的清理含义不变。

五个新方法及 prepare 全部进入原 `owner`，忙/尚有另一不确定写时在新 key/transport 前明确 `AccountFailure('busy')`，不合并不同读，也不把旧 run 的 void/no-op 当结果。原登录/注销/改密确认中不插公开操作；公开原意图未放弃时 auth/personal/visibility 不得旋转、覆盖它。新门面错误用原 AccountFailure，不送进只懂登录字段的 `failure()`。原30s可见预算、actual fetch/body/cancel 单一终局、generation隔离保留；迟到结果/finally不能更改后继页面、主题、busy、字段或导航。

三个写首次提交冻结 `{kind,path,原key,匿名epoch/CSRF,完整input}`；邀请另捕获当时可知的 nullable Session identity 供授权变化与迟到隔离。reset request/complete 的原命令 owner 是匿名 Browser，不因目标用户 Session 被自身成功重置撤销就伪造新 owner/key。公开状态只含安全进度，不含key、CSRF、原body或材料。crypto熵不可用本地拒绝，不降级随机数。

| 事件 | 可见与内部结果 |
| --- | --- |
| 初次/显式准备 | 同 owner 确认 Session 与匿名上下文，成功后才 inspect/允许找回提交；失败局部重试，不能将其它依赖错误当401 |
| inspect200 | 显示对应表单/到期提示；只是当前有效观察，不保证稍后提交成功，最终由服务端判断 |
| 新写严格201/202 | 对应写已确认，释放原写重试材料；邀请清密码/token并提示登录，request保留邮箱及实际渠道说明。202不是发送成功/账号存在证明 |
| reset严格204 | 立即确认命令、清密码/token/原意图并发布 confirming-session；然后在**同一 owner**直接 GET Session，不能嵌套 restore。200可能是仍有效的另一用户Session，401可能是目标原Session已撤销；不猜目标身份。失败发布 session-unconfirmed，返回 ResetConfirmation，绝不把已确认改密降回Unknown/再发reset |
| 未确认写 | COMMIT_UNKNOWN、commit_state=unknown、transport、坏成功响应、已发出后的取消或尚未收敛ResourceBusy进入uncertain，锁原完整输入，只能显式原请求重试或明确放弃。首次明确本地/字段拒绝不冒充unknown |
| uncertain后的拒绝 | 后续明确拒绝、410或幂等冲突不能否定之前的写；保持历史uncertain。原context失效则禁重放并说明结果仍未知；不自动重bootstrap/换key，也不清空成“未提交” |
| CSRF_FAILED / Session401 | 区分匿名CSRF失效与真实Session失效。前者撤销本地匿名重放能力；后者隔离失效的Session/个人草稿。不能把普通FORBIDDEN当匿名身份或自动logout；原未知命令事实独立保留 |
| abandon/离页 | 丢弃页面重试资格与敏感引用，隔离代际；保留实际请求所有者直到终局。没有自动反向命令，没有服务器取消/未提交承诺 |

HTTP没有公开 Account command lookup。服务内部可能在底层Unknown后查到receipt并返回正常成功，UI以实际响应为准。`link_commands.go`/`reset_complete.go` 在原 Browser/key/full-value receipt 命中时可早于活链接校验返回；因此 `retryOriginal` **直接重放原mutation，不预先 inspect 或重prepare**。已消费链接410、GET Session200/401、时间到期或试登录都不是原写终局证明。邀请HTTP有合法Session时自身仍会重核inspect；若因此返回拒绝，前端保留原uncertain，不改后端或绕过当前权限。

重置204之后的 Session GET 即使页面放弃仍由原 actual owner/原有界预算收束；安全全局身份更新与页面反馈分开。其它请求/新身份不得在该两步之间插入。纯测试必须证明先204后GET失败仍只算一次已确认改密、表单立即清空、重试操作不能再发reset。

## 6. 页面 owner、导航及显式身份转换

新增 `createAccountEntry(auth)` / `accountEntryKey` / `useAccountEntry`，位于 `useAccountEntry.ts`。App创建并provide同一个页面 owner；其职责限三页 draft、已校验链接结果、局部错误/进度、dirty确认与导航，不拥有第二份认证事实/API/CSRF队列。页面模式闭集 invitation/forgot/reset；组件只提交本页字段，由owner私有链接组装typed input。初始化、显式重新检查、重新请求、原请求重试分别有动作，不把onMounted/watch/visibility作自动重提。

`router/auth.ts` 新增与既有 personalNavigation 同型的 `installAccountEntryNavigation(router, owner)`：owner提供 `confirmLeave(): Promise<boolean>`、`afterNavigation(to:string,from:string):void`，返回撤销安装函数。先询问来源页dirty，再作目标准备/授权；取消不会擦除草稿。新三路由 `meta.accountEntry` 为闭集模式，并保留认证域生命周期标记，guard对该分支不用通用 restore/已登录跳首页；App visibility/pageshow 不在公开域盲目调用 restore。初次准备/重查由公开 owner 明确调用 entry.prepare；正常可见性变化保留当前表单/原意图。

有未提交密码/资料或uncertain时，返回、菜单、路由back与新链接替换须显示“继续编辑/放弃并离开”确认；找回已202后的普通邮箱不是未确认命令，不阻止正常离页。确认前不得清原材料。公开请求仍占 actual owner 时，SPA导航不能凭普通restore的void/no-op放行保护页；留当前页显示等待/稍后重试导航，不释放owner或暗中发第二次请求。浏览器硬离开只能做本地清理，不能承诺服务器回滚。新链接替换、相同route变化与 App unmount 均解除自己的监听/确认回调，旧代清理不能影响新代。

| 当前事实/动作 | 安全导航 |
| --- | --- |
| 匿名打开邀请/重置 | 准备匿名CSRF后inspect；成功显示表单，无Session自动创建 |
| 当前有效Session打开公开页 | 保留真实Cookie/Session；不自动跳首页或注销。邀请仍以真实inspect判断；reset/forgot允许按其真实匿名端口操作，不把当前用户邮箱当链接目标 |
| 邀请FORBIDDEN | 显示当前账号不能兑换此邀请，不泄露未返回的邀请邮箱。提供“返回当前账号”和“切换账号”；后者是用户点击到 `/login?switch=1`，清本页材料并说明切换后重新打开原邀请。不能自动logout后inspect成功 |
| 邀请201/重置204 | 提示成功并清密码；“返回登录”是显式动作，无api.login。若浏览器仍有有效Session则进入switch身份选择；未确认Session可先明确检查，不能展示其为已登录目标账号 |
| `/login?switch=1` + 当前有效Session | LoginView显示当前身份、净化后的“返回当前账号”和“退出当前账号后登录”。不显示其它用户的填充密码，不自动注销；只有点击退出才调用既有auth.logout，确认成功后移除switch、进入普通登录准备 |
| 显式logout仍Unknown/失败 | 沿既有auth恢复/原请求重试反馈；不得因为Promise<void>返回就声称已退出或自动登录。当前Session检查只证明当前身份，不改写原logout终局 |
| 普通 `/login` 或无效switch | 保持原guard、挑战、焦点与登录成功返回；query只用于闭集导航，任意URL/数组/编码绕过不扩大return目标 |

登录页跳去找回时清登录密码/挑战材料；公开域跳回登录时清本页材料。协调原 `LoginView.onUnmounted`、`router.afterEach` 与 App dispose，不能旧页 `leave()` 擦掉目标页在guard已准备的context。原settings的dirty、主题preview、稳定epoch和改密换发两步保持；跨域后所有保护页仍沿真实Session重验，不拿公开准备或上一份User当永久授权。

## 7. 可见字段、错误与渠道

邀请先校验再显示邮箱只读、username、display_name、password、confirmation；固定邮箱不作为可改请求字段。用户名占用/保留字与弱密码就近反馈，字段拒绝保留其它输入。失效链接提示联系管理员；不在公开页提供发邀请功能。

找回只填邮箱。202统一提示“如果该邮箱可用，将按当前渠道提供恢复方式”，保留邮箱与明确再次申请按钮；smtp说明查看邮件，backend_log才说明请有权限访问受限后端日志的人协助转交。无账号存在性、token、恢复URL、jobID、投递日志/错误细节或送达承诺。客户端不能因smtp报错改成backend_log；重复请求不暗中延长有效期，由后端实际规则决定。

重置只填新密码和确认密码；inspect不返回目标email，不能从当前Session/先前找回输入猜目标。失效可显式转到找回页；成功提示重新登录。管理员与普通用户相同，没有管理员快捷重置。复用当前Ui控件、主题tokens、自动填充与键盘语义；密码允许粘贴。进入页标题聚焦；失败焦点移到合适字段/错误区，异步结果不抢走用户已移开的焦点。成功先清敏感输入再布局取证，长email/错误可换行，无横向溢出。

## 8. 精确实施路径与禁止范围

采纳后另授下列23条唯一写权；本次设计不实施。12条生产前端（7旧、5新），4条旧pure窄适配，7条新测试。没有额外“可选文件”；实际必要性超出须先报告。

| 路径 | 允许范围 |
| --- | --- |
| `web/src/api/client.ts` | 五个固定POST endpoint；原传输、限额及body生命周期兼容 |
| `web/src/api/account.ts` | §3类型、五方法与严格输入/响应解析；原14方法语义不变 |
| `web/src/composables/useSession.ts` | §5 entry门面、匿名/Session共存及同owner整合；原认证/personal兼容 |
| `web/src/router/auth.ts` | 公开域准备/离页接缝、switch闭集与原dirty顺序 |
| `web/src/router/index.ts` | 三路由/meta与history前捕获；不增加return目标 |
| `web/src/App.vue` | 单一公开页面owner/provide/确认框/visibility与dispose协调 |
| `web/src/views/auth/LoginView.vue` | 找回真实入口、显式switch身份选择及跨公开域unmount协调；挑战机制/焦点不重写 |
| `web/src/router/account-link.ts`（新） | §4有界私有fragment捕获与生命周期 |
| `web/src/composables/useAccountEntry.ts`（新） | §6三页draft/错误/进度/dirty导航owner |
| `web/src/views/auth/InvitationView.vue`（新） | 邀请校验/兑换与安全身份转向 |
| `web/src/views/auth/ForgotPasswordView.vue`（新） | 统一找回申请/渠道反馈 |
| `web/src/views/auth/ResetPasswordView.vue`（新） | 重置校验/改密/确认及返回登录 |
| `web/src/tests/session.spec.ts` | 补五方法mock，原断言与预算保持 |
| `web/src/tests/authentication.spec.ts` | 补五方法mock及真实找回/switch路由必要setup；原正常登录/挑战/保护断言保持 |
| `web/src/tests/personal-settings-state.spec.ts` | 补五方法mock；原身份/Unknown/actual-tail断言保持 |
| `web/src/tests/personal-settings.spec.ts` | 补五方法mock及必要App注入setup；原资料/主题/改密断言保持 |
| `web/src/tests/account-entry-client.spec.ts`（新） | wire/DTO/敏感材料/错误解析 |
| `web/src/tests/account-entry-state.spec.ts`（新） | 匿名与Session、原意图、取消/确认/实际owner |
| `web/src/tests/account-entry.spec.ts`（新） | 三页/fragment/router/switch/dirty/焦点 |
| `tests/account-captcha-web/account-entry.config.js`（新） | 专属固定浏览器selector/私有运行目录，不加依赖 |
| `tests/account-captcha-web/e2e/account-entry.spec.ts`（新） | 真实生产三页和既有身份组合 |
| `tests/account/account_entry_web_fixture_test.go`（新） | 复用旧root+record，同源正式前置、私有IPC/只读后态/专属launcher |
| `tests/account/account_entry_web_test.go`（新） | §9真实顶层及持久/Session事实断言 |

只读旧Go/浏览器fixture及测试，不改服务端、OpenAPI、SQL、root、脚本、共享UI/styles、theme、personal composable、包锁、状态页或其它卡。测试新增helper可复用同包既有函数或在新两Go文件封装真实f.origin HTTP；不得整份复制root/旧spec、加production fixture API，或借mock适配放宽业务。

## 9. 验收闭包与资源

| 组 | 必需观察 |
| --- | --- |
| 新client pure | 五个精确method/path/status/body/header，inspect无key、全用匿名CSRF；严格literal/shape/Instant/token/无204 JSON；Unicode/空格完整，坏HTML/重定向/失败body清理，不保留raw秘密 |
| 新state pure | 同一个owner互斥 auth/personal/entry；有效Session+匿名context并存，bootstrap不伪注销；未知原key/context/fullbody精确重放且不inspect/prepare，后续410/明确拒绝不抹历史Unknown；CSRF失效与Session失效区别；30s可见取消不放actual尾部、旧结果隔离 |
| reset确认 pure | 204先发布确认并清密码/token，持同owner再GET；200/401/错误三分支及用户离页；后续GET失败不再POST。目标重置不靠personal identity存活才能重放，当前另一Session不被假清除 |
| 新UI/router pure | history前捕获、同标签hash/popstate/同path新link、无效/超长/清除失败、刷新无材料；敏感值不进URL/history/storage/DOM；dirty取消保留原意图、新link不能覆盖；已登录invite FORBIDDEN零自动logout；显式switch仍需点击注销，普通login/四return闭集不变 |
| `TestAccountPublicEntryWebInvitation` | 真实管理员HTTP创建邀请→受限日志精确link→生产页校验→readonly邮箱/真实字段拒绝→兑换201，无自动Session；清材料后明确登录访问本人；实际命令/User/Audit后态匹配且无重复用户 |
| `TestAccountPublicEntryWebPasswordReset` | 正式创建普通用户并建立两个真实Session；公开页申请→真实Outbox/受限日志link→重置204；两旧Session实际失效、新密码经正常登录成功、旧密码拒绝；无自动login/Session替换声明，持久命令/Session撤销事实对应 |
| `TestAccountPublicEntryWebIdentityNavigation` | 当前A打开B邀请真实FORBIDDEN，A的Session仍有效且零logout；用户明确switch/logout后按提示重开原链接才兑换。重置B时浏览器仍持A，204后的当前Session仍是A且不自动改绑；正常login/return及settings dirty跨公开域保持 |
| `TestAccountPublicEntryWebPrivacyAndProduction` | 已注册/未注册邮箱相同公开反馈/shape；正式撤销/已消费链接不可用，URL尽早清除且无秘密证据；backend_log真实渠道、无Debug/API fallback；三页1440/1024/834/390、浅深/system、200%/键盘/reduced-motion/长email无溢出 |

真实正向必须是同一最终 dist→自有同源服务→真实 app.Run/PG/Secret/Audit/Outbox/MinIO。新helper只复用 `newAuthenticationWebFixture`，沿动态 `f.origin` 用正式管理员邀请与兑换/登录创建前置，利用已保留 `f.record` 读**本fixture**精确purpose/email/id记录。旧 `httpFixture.invite` 固定localhost:8080不能直接套用。找回后的动态link可经有界私有文件握手转交浏览器，0700目录/0600文件，固定purpose/邮箱/本次资源与有界序号；不新增产品API、SQL造身份/链接、route.fulfill或假Session。只读PG后态不能读取密码hash/Secret材料进输出；不把DOM文字单独当业务证据。

backend_log为本卡真实浏览器交付渠道。smtp UI分支在严格DTO/组件pure中覆盖，并复用D07已验“配置后失败不回退”的后端证据；不声称本卡新增SMTP真实投递/browser验收或真实邮件账户。邀请撤销可用既有正式管理员HTTP作前置；链接过期/错误形状/Unknown/迟到由纯受控依赖覆盖，区分真实使用后410与时间模拟，不能静改表造过期并称真实全流程。HTTP没有lookup；不新增网络/COMMIT/ROLLBACK故障注入。

纯门槛沿 `npm run check --prefix web`（格式/Vitest/type-check/build），只格式化授权文件，无新包/升级。新增Go只需固定Go1.27.1、offline readonly的integration race compile与适用vet；后端生产零改，不机械重跑两cmd/全域后端。旧认证四个真实顶层、个人设置四个真实顶层按共享controller/App/router影响回归，原断言/预算不改。已有稳定不变证据可按独立验收者明确范围复用，首失败保留，不以修改断言掩盖回归。

浏览器/Docker窗口仍由主线程另授。新四顶层按前两/后两冻结selector分批，旧两组各沿原两批；`scripts/test-objects.sh -run '<selector>'` 保持race/count1/每包6m，Go浏览器顶层2分钟、Playwright workers=1/retries=0/单例45s。不能因新增场景超时擅自加预算或只挑绿例；超出先以实际证据报告、调整有界分组需主线程确认。fixture只用自有随机端口/nonce-label资源、锁定浏览器，无existing server复用或外部账户。

记录冻结23源/依赖资产、实际argv/env名/exit/selector/原日志与首红；禁止trace/video/request-body日志及敏感截图，错误输出须避免URL/token/密码自动打印，必要截图在敏感字段清空后取得。每轮实际进程wait、私有runtime清空、资源exactID两遍absent且原基线不变再交窗。独立验收优先补“真实另一身份+显式转换”和“确认/未知与匿名owner”风险，纯模拟不能冒充PG Unknown或原writer终局。

## 10. 当前状态

个人设置与认证前置已接受，本卡已按最终提交 `c54f73f3324caa11608d84e5d207141985eb6074` 核定。独立规格报告 `/workspace/agenteam-public-account-entry-spec-v-spp78bu_/report.md`，SHA-256 `e69b4e4d70ba780b7deb7e10a41034690cf25da1eef63a93d34cd5966ae2c160`，结论为 STATIC PASS；主线程已完整读取并采纳 rev1 及精确23路径范围，无技术修订。此为规格可实施性结论，不是页面、pure、browser或完整D26验收。

规格已采纳提交推送 `e5a5ccf5343fe9f17aced6e9ee0d633fbfe3c5aa`；主线程现已正式授权 `d08_registry_backend` 在固定 `c54f73f` 与本卡被审技术正文上实施 §8 精确23路径，`skill_verification` 独立验收。作者仅可在私有环境进行 offline/pure/type/build；Docker/browser 窗口仍须另授，尚无本卡业务验收结论。[规格持久记录](../agent-team/public-account-entry-spec-verification.md)保存可行性、rev1、31项Git定位、独立STATIC PASS、原行政差量及[独立后续计划](../agent-team/evidence/public-account-entry-spec-verification/plan/plan.md)，计划不等于测试已执行。§1–9 技术正文、固定基线和23路径保持不变。完整D26、生产SPA托管及原模块阻断不因规格采纳或实施授权改变。

## 11. rev2 测试接缝补遗（2026-10-06）

本节依据本轮真实 `TestAccountPublicEntryWebPrivacyAndProduction` 首红与固定生产源码作窄修订。该次已取得已注册邮箱的严格202回执及成功页面反馈，随后新helper在自设4s等待内未见 `password_resets` 行；未注册邮箱请求尚未执行。正式 `account.Runtime.maintain` 每10s调用一次 `Recover`，单轮有2s预算，`processReset` 才创建reset、投递意图并追加事件；Accountmail的1s循环处理后续投递。202只确认申请受理，因此新helper的4s与Playwright默认5s IPC等待不能覆盖一个正常周期。首红数据库已由原trap销毁，缺当时相位快照，不能声称本次唯一根因已确定；原失败和原cleanup失败保持，后续定点双清另记。

主线程授权在 §8 原23路径之外，追加且仅追加下列第24路径：

| 路径 | 唯一新增允许范围 |
| --- | --- |
| `tests/account/authentication_web_fixture_test.go` | 为既有私有 `authenticationWebFixture` 增加 `recoveryLogPath string` 字段，并在已有构造中赋 `cfg.AccountRecoveryLog()`；仅传递本fixture已配置的精确日志路径元数据 |

该旧fixture的root、构造行为、`record`、既有断言、launcher和资源清理逻辑继续只读；不得复制root、搜索 `t.TempDir` 父目录、枚举邻近文件或猜日志位置。新 `account_entry_web_fixture_test.go` 仅通过该明确字段只读本fixture的精确受限日志。复用旧 `f.record` 的现有流程仍可保持；新的有界IPC读日志必须继续核对精确purpose、邮箱与本次reset ID，能力链接只经0700目录/0600文件私有转交，不进入普通日志或测试输出。

新动态恢复链接握手采用一个20s截止，沿既有 `record` 的20s尺度，覆盖PG后态等待、精确受限日志匹配及IPC应答。Playwright等待应答与Go helper截止须联动；不能PG等待结束后再重开一个20s日志等待，不能以重发HTTP、直接调用生产恢复方法、写表造链接或扩大后台周期改变被测链路。查询和等待须受该握手截止及原父级取消约束，失败后仍实际取消并join已启动的浏览器。原Playwright单例45s、workers=1/retries=0、Go顶层2分钟、driver race/count1/每包6m保持，§9的真实Session、CSRF、精确command/Audit与完整known/unknown回执断言不变。

允许在新helper的首次观察与成功/失败终局增加安全只读阶段快照，以同一精确成员及请求、command、reset、delivery因果关系核事实。输出仅为白名单bool/count/phase/pass与必要SQLSTATE；ID在私有进程内用于精确关联。不得输出邮箱原值、Browser/CSRF、幂等key、token、密码或其hash、Secret材料、URL，不能转储原始行或原始数据库错误。该快照用于区分尚未扫描、已扫描未完成与已转入投递的阶段，不将缺失首红快照的原因补写为确定事实。

新增候选先在私有冻结输入中形成24路径manifest与逐路径差量，由独立验收核上述元数据接缝、单截止、隐私和原断言，再交主线程审阅归位。只变测试观察或握手时，已通过且输入语义不变的业务证据可按 §9明确复用；受影响Privacy须定向复验。原23路径输入、首红原件及其动态结论保持可追溯，不以新的24路径清单改写旧输入。本补遗不增加产品、服务端、SQL、依赖或共享UI范围，资源执行继续使用主线程协调的独占窗口。

## 12. rev3 旧主题回归观察点补遗（2026-10-06）

本轮原 `TestAccountPersonalSettingsWebThemeAndNavigation` 在真实 `PUT /api/v1/me/preferences` 返回409后，旧浏览器测试执行 `conflict.json()` 时出现 CDP `Network.getResponseBody: No data found for resource with given identifier`。原409状态断言已通过，但后续草稿/主题行为与三次写入、三条Audit终態未走完，因此该原轮仍为失败；不据此声称产品响应体丢失或CDP唯一根因已确定。首红与资源证据保留，原 `ProfileAndAvatar` 通过事实不扩大到主题组。

主线程授权在 §8原23路径及 §11元数据路径之外，追加且仅追加第25路径 `tests/account-captcha-web/e2e/personal-settings.spec.ts`。只可替换上述冲突保存动作的一个409响应体采样点：在真实点击前安装限定同源、`PUT`、`/api/v1/me/preferences`及409的一次原响应clone观察，返回并保留同一个原生fetch Promise/Response，原请求只发一次；保留原Playwright响应事件与409状态断言，从该次实际响应读取JSON并继续断言 `code === 'VERSION_CONFLICT'`。读取、解析或清理失败须显式失败，finally实际join观察读取/cancel/release并恢复fetch与删除私有状态。无网络拦截、响应替换、重发、额外写入或错误时空成功。

clone会分出新的body读取分支，属于测试instrumentation；该观察只证明上述真实409及Problem code，不能证明未观测原始stream或共享owner的实际尾部时序。原后续radio/预览保留、取消预览、重新保存、系统浅深色、dirty导航和保护页返回行为，以及Go侧原精确三次持久写入、三条Audit与用户/版本/主题事实全部保持。不得改成仅检查409或UI文字，不删原code字面断言，不修改其它旧响应观察点、旧fixture、生产client或controller。

候选在私有冻结输入中提供25路径manifest及该旧spec精确差量，先由独立验收核范围、原请求与断言保持、隐私和实际清理，再交主线程审阅归位。受影响 `TestAccountPersonalSettingsWebThemeAndNavigation` 按原selector定向复验；其余输入语义不变的已通过证据按 §9明确复用。原单例45s、workers=1/retries=0、Go顶层2分钟、driver race/count1/每包6m及独占资源双清门槛保持，§11单20s恢复链接握手不变。禁止trace/video/request-body日志与秘密输出，本补遗不增加产品、后端、SQL、依赖或其它旧测试路径范围。

## 13. rev4 旧密码拒绝观察点补遗（2026-10-06）

原 `TestAccountPersonalSettingsWebPasswordRotation` 复验已在正常修改密码、替换Session及新密码登录后，以旧密码执行真实 `POST /api/v1/sessions/login` 并得到401。旧 `personal-settings.spec.ts` 的 `refused.json()` 再次出现CDP取body失败，原 `code === 'UNAUTHENTICATED'` 及该点之后的浏览器/Go后态断言未完成，故原轮仍为失败。原 `AuthorityAndProduction` 已通过，按不变范围复用；首红日志及双清记录保留，不将其原因写成产品body丢失或已确定的CDP根因。

主线程仅扩展 §12 已授权的第25路径，增加上述第二个实际失败观察点。可将 §12 单次clone helper参数化为两个闭集目标：`PUT /api/v1/me/preferences` 的409与 `POST /api/v1/sessions/login` 的401；不得成为自由URL/任意状态采样器，也不修改其它旧 `.json()` 观察点。两者均限定同源、原真实用户动作的一次fetch与对应响应，保留原Playwright状态断言，从原响应的clone读JSON；旧密码拒绝继续精确断言 `UNAUTHENTICATED`。沿 §12 同一原生Promise/Response、bounded读取、错误显式失败、实际join/cancel/release/restore/delete及tee边界，不重发或替换请求，不读取/记录密码请求体。

该点之后原密码输入清空、初始密码提示清除、敏感材料缺席、最终User/Session/version和两次写入结果保持；Go侧精确两次命令/Audit、两条原Session以 `password_changed` 撤销、替换Session仍有效及其后CSRF写入事实全部保持。不得以仅401状态或UI错误文字替代原Problem code，不放宽Session或持久状态断言。

新候选保留原input11及失败原件，提供25路径manifest与旧spec相对input11的精确差量，并完成受影响type/Go检查。独立核闭集两个观察点及未改断言后，由主线程审阅归位，定向复验受影响 `PasswordRotation`；§12的Theme及 §11的Privacy仍须完成各自受影响验证。允许沿主线程核准的原selector合并分组，原45s/2分钟/每包6m、race/count1、workers=1/retries=0及独占双清不变。此补遗不增加第26路径，不改变已验组合生产输入，也不新增产品、后端、SQL或依赖范围。

## 14. rev5 改密字段错误观察点补遗（2026-10-06）

input12 的三顶层定向复验中，`PrivacyAndProduction` 与 `ThemeAndNavigation` 已通过；`PasswordRotation` 在错误当前密码触发真实 `POST /api/v1/me/change-password` 返回400后，旧 `wrong.json()` 发生CDP正文取回失败。该点的 `field_errors` 及其后改密/Session/PG断言未完成，因此本轮Password仍为失败。相邻弱密码400的 `weak.json()` 使用同一采样方式，本轮尚未到达；不能将它记为已观测失败，也不等待其再次随机失败才修正同一观察方式。保留本轮首红、实际退出与原清理事实；不宣称产品body丢失或已确定CDP根因。

主线程仅授权在第25路径的现有helper闭集中增加 `POST /api/v1/me/change-password`、400这一目标，并替换上述错误当前密码和相邻弱密码两个正文观察点。每次仍限定同源、一次原真实点击、一次原fetch及其400响应；保持原Playwright响应事件、状态和请求次数约束。两个 `field_errors` 断言逐字保留，分别含 `{ path: '/current_password', code: 'INVALID' }` 与 `{ path: '/password', code: 'PASSWORD_WEAK' }`，不以仅400、UI文字或任一字段错误替代。

沿 §12–13 的同一原生Promise/Response、完整且有界JSON读取、失败显式、finally实际join/cancel/release/restore/delete及tee边界。helper仍只接受三个闭集接口/方法/状态目标；不重发、不替换响应、不扩大超时、不读取或记录密码请求体。此补遗不扩展已通过的Profile重复字段观察点、页面内原生 `response.json()` 或其它旧测试。

两个400之后的字段可编辑、严格成功换发Session、另一真实上下文撤销、原新密码登录及旧密码401、密码清空、初始提示清除、敏感材料缺席和最终结果全部保持；Go侧原精确两次命令/Audit、原Session撤销与替换Session/CSRF写入等断言不变。input13保留input12及其失败原件，提供唯一旧spec差量、25路径manifest和受影响type检查；Go源码不变可明确复用input12编译/vet。独立静核及主线程采纳后，只定向重跑原 `TestAccountPersonalSettingsWebPasswordRotation`，其余已通过顶层及独立组合按不变范围复用。原45s/2分钟/每包6m、race/count1、workers=1/retries=0和独占资源双清门槛不变；不增加第26路径或产品、后端、SQL、依赖范围。
