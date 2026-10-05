# D26 本人资料、主题与修改密码

修订：rev1.2（2026-10-05 验后行政接续），技术正文保持规格冻结版本。独立差量静审及 §2 最终前置核查均通过并获采纳；被审卡 SHA-256 `65ff66b024de34b32f6b0fbc000f5d3e77a3e40ba7bf1b334045ee2d9b031631`。rev1.1 已独立静审通过并获采纳，提交 `ac653cfd099f959ae9bec2c3489e499351b0fdfd`（被审卡 SHA-256 `3c190ab7e068397a50edfe8061916409aa2a47ad0a3ef6e036054417cc9ead8d`，独立报告 SHA-256 `2593182a692eec16d986ff7ca6a378ed560345f7f34bca6d9342b2762e9e0161`）。rev1.2 据冻结认证源码明确实际接缝及必要范围，21路径增至24，产品规则不变。**本卡精确24源已最终独立验收通过并获主线程采纳，源码提交推送 `c54f73f3324caa11608d84e5d207141985eb6074`；最终报告 SHA-256 `ada59561329eb0c5d8b9b28720e6a548601a09b27966e04df4ea40897ae92b9c`。** 实施固定业务基线为已验认证 `9a710f272026b41ef69852bbeb41cb7670b500a8`；规格后端与设计来源仍为 `ccf498d61152178c5b44994d4b9e8b8f4eb6813b`。实施者 `d08_registry_backend`、独立验收者 `skill_verification` 的动态执行均已结束，资源清零并交回主线程。正文保留规格冻结时的授权条件，当前状态与证据边界以 §10 接续及[持久验收记录](../agent-team/personal-settings-verification.md)为准。无新包、后端 API 或迁移。

依据为 [D07 已验 Account](d07-account-session-smtp.md)、[认证卡 rev1.2](d26-account-authentication.md)、[个人设置](../../frontend-design/layouts/personal-settings.md)、[通用设置框架](../../frontend-design/layouts/settings-shell.md)、[账号生命周期](../../architecture/platform-infrastructure/authentication/account-lifecycle.md)及固定可行性报告 `/workspace/agenteam-personal-settings-feasibility-d0bt0_z1/report.md`（SHA-256 `8d7839fcd20da4a8457b6483f72a9b86c3a10b02d2155e1f59db625372cc1e2c`）。报告只有静态分析。必读 [设计技能](../../../.agents/skills/agenteam-design/SKILL.md)、[Vue 开发](../../../.agents/skills/agenteam-vue-development/SKILL.md)、[Vue 测试](../../../.agents/skills/vue-testing-best-practices/SKILL.md)、[前端基础](../frontend/README.md)、[组件接口](../frontend/components.md)及[样式规范](../../frontend-design/styles/README.md)；浏览器阶段另读可用 Playwright 技能。

## 1. 完整结果与范围

交付连接真实 Account HTTP 的本人基本资料（含头像）、主题预览/持久保存、修改密码及既有当前 Session 注销。三者共用设置框架、User version、当前身份显示和离页保护，作为一张完整结果卡；基本字段、头像、主题、密码各有独立保存动作，不造不存在的跨分区原子保存。

路由固定为 `/settings/profile`、`/settings/appearance`、`/settings/password`；`/settings` 进入 profile。均为当前本人受保护的系统页，无项目导航。右上显示名改为直达 profile 的真实链接；首页初始密码建议直达 password，仍可继续使用系统，不是强制改密关卡。普通入口每次首项，指定叶子直接打开。只有这三项加入认证返回目标闭集，不接受任意 URL、query/hash 目标或 Project 文字路径。

邀请、找回/重置、管理员用户/System 设置、语言/字号/通知偏好、设备管理、邮箱修改和二次验证不在本卡。尚无页面的邀请/恢复不放死链接；退出沿已验后的原协调器，不声称停止 Agent 执行。默认首页仍只有既定内容，不借个人设置补 Dashboard、搜索或 Inbox。

## 2. 依赖与实施前硬门槛

| 依赖 | 固定事实与限制 |
| --- | --- |
| D07 资料/头像/偏好/改密 HTTP | 已有真实端口；`account/http.go:77–85` 注册，`app/account.go:324–345,382–386` 装配真实 core、AvatarAuthority、ProfileService、Runtime 和 handler；普通用户/管理员同样只访问本人 |
| D26 认证前端 | 已最终验收并提交推送 `9a710f272026b41ef69852bbeb41cb7670b500a8`；本卡依赖的同源传输、User/Problem 解析、Session/CSRF、单一 Cookie owner、generation、登录/注销/受保护路由及同源 dist fixture，已按下述最终独立结论核定；本卡新增门面仍待实施 |
| 前端基础 | 复用既有 Ui 控件、tokens、useTheme、浮层/焦点；不复制 Debug 数据，不加 Pinia、上传/表单/UI 库或包锁改动 |
| 后端运行 | 复用真实 Account root/PG/Secret/Audit/Outbox/MinIO；无 Project/D25/未决 Summary 前置，也不新增生产 root 能力 |

**业务开工所需的独立依赖差量门槛已关闭，实施授权仍未授予。** 最终报告 `/workspace/agenteam-personal-settings-prerequisite-final-v-0wgurqub/final-report.md`（SHA-256 `9a0450e4c1f799dcbe110f511fcf7d54ab1fe04fe37bd0986339107d76aba23b`，`final-index.json` SHA-256 `e889576c24f6c970e0d704b0c4e5bf562792ebdc33c77d846412bbf9ea1b53fb`）已获主线程采纳：固定认证提交21源与预审输入相同、五项Git依赖及相关后端/helper闭包无影响漂移，实际导出/owner/身份主题/路由/fixture与本卡24路径映射成立。§3–9 技术规则保持，条件性范围仍须主线程另授；其中最终认证提交后的末核已由此结论履行。后续若基线、公共行为或路径有影响差量，仍须先核受影响部分并修订规格，不因本次通过而自行扩权。

本次预备接缝审计固定于 `/workspace/agenteam-personal-settings-seams-v-gkyz_151/report.md`（SHA-256 `1fd8419f21a5ea43cd769aa0ba8e822d10f138425671cb41e7b4b4777d44592f`，index `fe83ce424280a17dadf099ae4d668101568343bb534656ea83b7591d3e3bee98`）。输入为认证 author-final manifest `598b10dcbc7e65c7ec197947900cf0e28248ac5a83c7fbcbfb4523d7a9fc40c0` 引用的 `fixture-input-05` 原件21源（manifest `2e0d63e1fabeaf4ba1ba808f2aa5e4a795b903516f6501b08d25517f16b37966`），不是活动树。以下行号均对应该冻结副本；该预审只提供可行性依据，最终认证提交后的门槛由上列最终独立报告关闭。

| 实际接缝 | 本卡必要窄扩与保留责任 |
| --- | --- |
| `api/client.ts:150` 的 `accountTransport`、`api/account.ts:169,239` 的 `createAccountAPI` / `AccountAPI=ReturnType` | 原闭集6方法只发送POST JSON；在原工厂扩8方法、PATCH/PUT/DELETE JSON和raw File/有界图片，保留实际body清理。`User` 已导出，私有 `user()` 可在同文件复用。扩展后两份旧纯测试 mock 需完整类型适配，不把生产方法改成optional或空成功 |
| `useSession.ts:73,216–254,547–566` | 现有 `createSessionController` / `SessionController` / `useSession` 没有通用业务调用口；私有 `run` 是 restore/login/logout 的 `Promise<void>` owner，忙时可能空返回。§4 新增 typed 个人门面必须由同一 owner 承接，明确DTO/错误；可见30s超时不释放实际请求尾部。原认证方法签名与单飞语义保持 |
| `useSession.ts:87–98,108–145,263–297`、`App.vue:12–15,60` | 现 `generation` 每次请求递增；restore 先清身份/设system，publish 清意图/设主题，checking会卸载 RouterView。不能直接将其当稳定身份代际或真实离页；按 §4 分离并保留同身份草稿/预览，仍隐藏检查中的受保护展示 |
| `router/auth.ts:5–21`、`LoginView.vue:95–103` | `safeReturnTarget` 和 guard 的 return 目前只有 `/`，登录成功也硬编码 `/`。两处共同接闭集三叶子；未认证跳转保存净化目标，登录完成消费同一净化目标。挑战/焦点/密码wire不改，dirty确认先于破坏草稿的导航及logout |
| `authentication.spec.ts:50–79,128`、`session.spec.ts` | 类型注入目前6方法；补8成员的测试setup，意外调用应显式拒绝。仅更新“无settings链接”的过时断言，改验允许的profile/password目标；其余认证断言、Project链接禁用、原预算均保留 |
| `newAuthenticationWebFixture(t,ctx)`、`authentication_web_fixture_test.go:177–187` | 已有真实root/随机同源/dist builder，但 `old.record` 仅局部存活。只保留私有record绑定/必要fixture引用给同包新helper；普通用户沿 `f.origin` 的正式邀请/inspect/redeem创建。原 `.browser()` 固定认证config，个人设置在新两Go文件接专属launcher，不改旧launcher/断言，不复制root |

独立审计已核 `ccf498d` 至认证候选基线 `457b1979c9d6563740543b2011eedc06cce34c71` 及至 `ac653cf` 的 Account/app、两OpenAPI、`tests/account`、`tests/testsupport/accountenv`、`useTheme.ts` 均零差量。最终独立报告已核认证 `9a710f2` 的相同21源SHA及实际导出；相对 `ccf498d`，Account/app、两OpenAPI、accountenv和useTheme无差量，tests/account仅新增认证两fixture，旧helper不变。本预审结论据此复用，24路径范围已采纳，具体唯一写权仍待另授。§3/§4 的新增导出是本卡待实现提案，不能当作认证已交付能力；不另造第二 Session store、Cookie 队列、通用 HTTP 工厂或全局可变 CSRF header。

已知 Object Runtime join 缺陷及暂停修复保持；头像只消费 D07 已验本人 HTTP，不恢复暂停的方法或证明共享 guard 异常退出已修。Artifact/领域绑定阻断、ready503、无 Project/D25 绑定、Summary 待决、D28 生产 SPA 托管及完整 D26/D27 未完成的边界均保持。

## 3. 新增 typed HTTP 调用

唯一 wire 来源为固定 [account.json](../../../api/openapi/account.json)、[common.json](../../../api/openapi/common.json)与同提交 `account/http.go`、`http_profile.go`、`http_auth.go`、`http_wire.go`。新增八方法；下列接口只说明调用签名，不要求新建第二客户端实例。`User` 复用冻结 `api/account.ts` 已导出类型及同文件私有 `user()` 完整解析，不复制另一个宽松解析器。Version/Progress/Digest 沿 common：Version 为正 int64 十进制字符串，Progress 为非负 int64 十进制字符串，Digest 为 `sha256:` 加64位小写十六进制。比较版本使用精确字符串/BigInt，不经 number；只对已确认不超过5MiB的头像长度转换安全 number。

```ts
type Version = string
type Progress = string
type Digest = string
type Theme = 'system' | 'light' | 'dark'
type AvatarMedia = 'image/jpeg' | 'image/png' | 'image/webp'
type AvatarMetadata = Readonly<{
  media_type: AvatarMedia
  byte_size: Progress
  sha256: Digest
}>
type ProfileView = Readonly<{ user: User; avatar: AvatarMetadata | null }>
type PreferencesView = Readonly<{ version: Version; theme: Theme }>
type ProfileInput = Readonly<{ version: Version } & (
  { username: string; display_name?: string } |
  { username?: never; display_name: string }
)>
type PasswordInput = Readonly<{
  version: Version
  current_password: string
  new_password: string
  confirmation: string
}>
type WriteOptions = Readonly<{ csrfToken: string; key: string; signal?: AbortSignal }>
type AvatarDownload = Readonly<{ metadata: AvatarMetadata; blob: Blob }>
interface PersonalAccountMethods {
  getProfile(signal?: AbortSignal): Promise<ProfileView>
  updateProfile(input: ProfileInput, options: WriteOptions): Promise<ProfileView>
  getPreferences(signal?: AbortSignal): Promise<PreferencesView>
  setPreferences(input: PreferencesView, options: WriteOptions): Promise<PreferencesView>
  readAvatar(signal?: AbortSignal): Promise<AvatarDownload>
  putAvatar(input: Readonly<{ version: Version; file: File; mediaType: AvatarMedia }>,
            options: WriteOptions): Promise<ProfileView>
  deleteAvatar(input: Readonly<{ version: Version }>, options: WriteOptions): Promise<void>
  changePassword(input: PasswordInput, options: WriteOptions):
    Promise<Readonly<{ completed: true; next_path: '/' }>>
}
```

| 方法/相对路径 | 请求与严格成功响应 |
| --- | --- |
| GET `/api/v1/me` | 无 body/query；200 ProfileView |
| PATCH `/api/v1/me` | JSON ProfileInput；200 ProfileView；null 不代替缺省，`display_name:""` 才是显式清空 |
| GET `/api/v1/me/preferences` | 无 body/query；200 PreferencesView |
| PUT `/api/v1/me/preferences` | JSON PreferencesView；200 同 shape |
| GET `/api/v1/me/avatar` | 无 body/query/Range；200 实际图片，客户端不用 HEAD/206 路径 |
| PUT `/api/v1/me/avatar` | File 原 bytes，非 multipart/JSON；`Content-Type` 为所选允许媒体，`If-Match: "<version>"`；200 ProfileView |
| DELETE `/api/v1/me/avatar` | JSON `{version}`；204 无 body，不解析 JSON |
| POST `/api/v1/me/change-password` | JSON PasswordInput；200 `{completed:true,next_path:"/"}`，浏览器接收新 Session Cookie；响应不含新 CSRF |

所有写带原 `X-CSRF-Token`、`Idempotency-Key`；JSON body 沿16KiB上界，密码等敏感输入不另套截断。所有调用为固定相对同源 URL、credentials same-origin、cache no-store，拒绝重定向、SPA HTML 或不匹配的状态/Content-Type/shape；不设置 Cookie/Host/Origin/Content-Length/身份头，不接受 body User/Actor/role。每方法显式接收其 input/options，写 token/key 仅由协调器私有意图提供，组件不能自由组装身份。

Profile 用户必须验证认证卡完整字段，包括当前 user ID、role/theme、version 与初始密码建议；avatar 明确为 null 或完整三个字段。preferences 的 version 和 User version 是同一版本。JSON/畸形响应、transport、cancelled 与安全 Problem 分开，不制造 `not_committed`。Problem 保留原 code/field_errors/commit_state/retry_hint/request_id；未知字段/码只作安全区域错误，不显示 raw body 或请求值。

头像下载先验证200、精确允许媒体、1字节 ≤ Content-Length ≤ 5×1024×1024字节、带引号的有效 Digest ETag；有界读取，实际字节数必须等于声明，超限/截断/坏头拒绝，不盲目无界 `blob()`。只构造有界 Blob 与由响应头得到的 metadata，不把远端 URL 当图片。若它与当前 Profile.avatar 不一致，丢弃该下载，显示“头像已变化，请重新加载”；一次显式重新加载重新读 Profile 再取图片，不循环重试。只有当前 Profile 明确 avatar=null 才显示无头像；下载401/依赖失败不冒充 null。

## 4. 单一身份、版本与请求状态

`usePersonalSettings` 只拥有本页 saved/draft/命令状态与受控 Blob URL，不拥有第二份认证事实。当前 User/Session、CSRF、Cookie 请求 owner 及认证失败处理仍来自同一 `createSessionController`。新增稳定 `identity.epoch` 与现有每次run递增的请求generation分开；每个请求捕获 `{userID,sessionID,epoch,requestGeneration}`，写意图另冻结 `{method,path,key,expectedVersion,完整input或同一File}`。异步回包及finally全部核对身份/请求，旧结果不能改用户、主题、头像、dirty、busy、消息或导航；不把abort当作足够隔离。

在现有 controller 的返回对象新增 `personalContext`（只读 `{phase:'current'|'checking'|'invalid', identity:PersonalIdentity|null}`）和 `personal` 门面，下列名称为**本卡新增**，不是现成API。identity只含非敏感标识；checking保留的旧标识只供隔离/草稿生命周期判断，不授予显示或请求权限。CSRF、key、原重试意图仍为controller私有。

```ts
type PersonalIdentity = Readonly<{ userID: string; sessionID: string; epoch: number }>
type PasswordConfirmation =
  | Readonly<{ commandConfirmed: true; sessionConfirmed: true }>
  | Readonly<{ commandConfirmed: true; sessionConfirmed: false; failure: AccountFailure }>
type PersonalMutationResult =
  | Readonly<{ kind: 'profile' | 'avatar-put'; value: ProfileView }>
  | Readonly<{ kind: 'preferences'; value: PreferencesView }>
  | Readonly<{ kind: 'avatar-delete' }>
  | Readonly<{ kind: 'password'; value: PasswordConfirmation }>
interface PersonalSessionMethods {
  readonly passwordProgress: Readonly<{
    identity: PersonalIdentity
    requestGeneration: number
    phase: 'confirming-session' | 'confirmed' | 'session-unconfirmed'
  }> | null
  getProfile(): Promise<ProfileView>
  updateProfile(input: ProfileInput): Promise<ProfileView>
  getPreferences(): Promise<PreferencesView>
  setPreferences(input: PreferencesView): Promise<PreferencesView>
  readAvatar(): Promise<AvatarDownload>
  putAvatar(input: Readonly<{ version: Version; file: File; mediaType: AvatarMedia }>):
    Promise<ProfileView>
  deleteAvatar(input: Readonly<{ version: Version }>): Promise<void>
  changePassword(input: PasswordInput): Promise<PasswordConfirmation>
  retryOriginal(): Promise<PersonalMutationResult>
  abandon(): void
  previewTheme(identity: PersonalIdentity, theme: Theme): void
  clearThemePreview(identity: PersonalIdentity): void
}
```

八个门面方法分别调用 §3 同名HTTP方法，不接受任意callback、token或自由User setter。忙时新个人调用在生成key/启动transport前以已有 `AccountFailure('busy')` 明确拒绝，不按泛称read合并不同端点、不将原run的void/no-op当DTO成功。实际请求失败保留原 `AccountFailure`；个人字段错误/Unknown只反馈个人状态，不直接复用只懂登录字段的 `failure()`。真正身份失效/CSRF上下文失效仍由原协调器处理。`retryOriginal` 只派发私有冻结意图并返回相应kind；缺意图或跨身份拒绝。`abandon` 只隔离页面责任，不能提前释放actual owner或声明服务器回滚。改密严格200时立即发布受原identity/requestGeneration约束的只读 `personal.passwordProgress` 为confirming-session，页面据此立即清密码；不等Session GET才通知确认，也不暴露key或材料。此投影非null即表示该写已确认，后续visible超时不能把它降回unknown；随后成功/失败分别转confirmed/session-unconfirmed。最终Promise以 `PasswordConfirmation` 保留同一事实，200前错误仍reject；逻辑离页不让旧投影重新打开页面或触发提示。

新 `usePersonalSettings` 的草稿owner由 `App.vue` 生命周期创建并向设置页提供同一实例。RouterView临时checking卸载不等同真实离页：受保护展示仍隐藏，尚未判定失效的同身份draft/preview不销毁；同身份重验可更新可信saved/version而不重基dirty。确认新User/Session或上下文失效才递增epoch并清旧意图/草稿；显式离页/放弃则按 §6 清理。改密由本owner确认的换发，只保留该次安全完成/会话确认反馈，不继承旧身份可重放意图。

共享 `useTheme` 保持只读；controller内部区分已确认theme与身份约束的临时preview。`previewTheme` / `clearThemePreview` 必须核当前identity，旧代清理不能撤销新代预览；同身份重验不闪回system或覆盖dirty预览，真正失效回system。组件不能把自填User/Preferences当可信saved；同步只消费controller自己成功解析、身份及版本匹配的响应。

**新增全部 Account 请求都进入原 Cookie 队列**，包括读资料/偏好/头像。`account/http.go:175–178` 的失效错误可清 Session Cookie，不能把 GET 或图片视作没有 Cookie 副作用。禁止 `<img src="/api/v1/me/avatar">` 绕队列；显示受控下载后的 Blob URL。Cookie 请求沿认证卡30s上界，逻辑离页不提前丢失实际 transport 责任；下一身份请求必须等上一请求真正结束，超时/取消不是服务器回滚证明。改密 POST 与随后的 Session 确认构成协调器内有序的身份转换，旧 Session 写与 logout 不得插入其间；由 `personal.changePassword` 在同一次owner内直接完成，不嵌套run/restore或第二队列；可见超时/取消与actual body尾部仍分别收束。

本页一次只提交一个 mutation。资料字段和头像为独立保存分区；主题和密码各自加载与保存。每份 draft 带产生时的 User version；后续已确认响应只更新非脏视图和当前已保存事实，不给脏 draft 自动换 expectedVersion。低版本迟到响应不回滚较新已确认资料；版本比较不是授权，仍受真实 Session/generation 约束。

| 状态/触发 | 页面与协调器行为 |
| --- | --- |
| 初次进入/指定叶子 | 先沿认证 guard 验当前 Session；再独立加载本叶资料或偏好。加载失败就地重试，无旧用户闪现。密码页也先取得当前 User version |
| ready → dirty | 只改本地 draft；主题可预览，头像选取可显示“尚未上传”的本地预览。没有隐式保存 |
| 提交 | 先冻结完整值和原版本，再生成一次不可预测 key；crypto 熵不可用则本地拒绝，不降级。禁重复点击/Enter和其它写，不在 await 后读取可变表单 |
| 200/204 严格成功 | 对应命令已确认；更新真实响应可证明的事实。资料/头像200是**当前** Profile，重放可能含后来版本；不得强求等于 originalVersion+1 或当历史快照。DELETE204后取当前Profile，后续读失败不改写已确认删除为失败 |
| 明确字段/版本拒绝 | 反馈就近、保留输入；VERSION_CONFLICT 不自动改version/key重发。提供显式“重新加载最新资料”，若会丢弃本分区草稿须确认；加载成功重新编辑，不做静默合并 |
| uncertain | mutation COMMIT_UNKNOWN、commit_state=unknown、transport/坏成功响应/已发出后的取消，或尚未收敛 RESOURCE_BUSY。禁改写原待重试快照；按 §6 显式检查/原请求重试/放弃，不假成功或换key |
| 当前身份失效 | 仅认证卡认可的 UNAUTHENTICATED/SESSION_REVOKED 触发原全局失效；先隔离旧代，再清页面敏感/预览/Blob引用，受保护内容消失。普通字段失败不注销用户 |
| 新用户/Session/context | 清上一身份 draft/反馈；旧意图不能跨身份重放。正常 Session revalidation 不替换同代正在编辑的 draft；发现服务端版本变化提示重新加载 |

## 5. 三类设置的具体规则

### 5.1 基本资料与头像

邮箱只读。username 3–32 ASCII 英文字母/数字/中间连字符，服务端规范小写、全站唯一及保留字；保持现存 `admin` 合法，不用新建账号保留字规则拒绝未改的旧值。更名前显示“本人项目路径将变化，旧链接失效且没有别名或自动跳转”；本卡不生成未实现的 Project 链接。display_name 最多80 Unicode码点/320 UTF-8 bytes、无控制字符；允许空值和空格，不 trim。空显示名导航用邮箱。提交只送实际拟变的字段，角色/邮箱不可写。

资料字段明确“保存资料/取消修改”。头像独立“选择图片→上传/替换头像”，选择不会保存；移除显示针对当前头像的确认，不暗中连带保存其它字段。输入只允许静态 JPG/PNG/WebP，展示实际5MiB、4096边长/16,777,216像素限制；服务端仍验证完整容器、禁止动画/SVG、缩放最长512并重编码去元数据。前端不自制裁剪/变换或把客户端解码当服务端授权；File.type/大小仅作早期反馈。

上传前保留当前已保存头像，候选预览明确未保存；失败不把候选提升为当前头像。确认成功后按返回 metadata 重新读取真实图片；读失败显示可重试状态，不能回到“上传未发生”。移除成功的204后显示已确认完成反馈并读取当前资料；并发新头像出现时显示当前事实，不谎称它必须为空。选取/取消/替换/离页/失效时 revokeObjectURL；读回与候选 URL 分开所有权，重复清理无泄漏。未知提交时同一 File 的原字节与媒体/版本/key 必须保留于私有意图，不能选择另一文件后冒称同请求重试。

### 5.2 主题

仅 system/light/dark。保存事实与临时预览分离；显式值优先，仅 system 响应系统配色变化，复用原 useTheme/tokens。首次未知偏好仍按系统，不新建 localStorage/sessionStorage 或 CSS token。

选项变更立即预览但不写接口；保存按原 version/key 调用 PUT。取消/确认放弃恢复**当前同一身份最新已确认**的服务端主题；退出/失效回匿名 system。来自 Session/偏好的新已确认值可更新 saved，但同身份 dirty 预览仍保持，显示版本变化且不自动重基；不能由迟到 Session 回包瞬间覆盖预览。保存确认后去掉临时覆盖；未知时保留“未确认”的预览标识，不能称偏好已持久化。刷新丢弃预览，从真实 Session/偏好重新加载。

### 5.3 修改密码与 Session 换发

表单为当前密码、新密码、确认新密码；使用 current-password/new-password 自动填充语义、不禁粘贴，不以 HTML maxlength 静默截断UTF-16。新密码/确认15–128 Unicode码点、最多512 UTF-8 bytes，允许中文/空格，精确保留首尾与正规化形式；客户端核两次一致，服务端核当前密码及弱密码。当前密码只验证必要存在/接口上界，不拿新密码规则提前拒绝历史密码。字段错误按 `/current_password`、`/new_password`、`/confirmation` 映射，其它为区域错误。现有 `password.go` 也会以 `/password` 报告校验失败，可能来自当前或新密码；该路径作为密码分区错误，不猜字段归属或改后端路径。

严格200才记录“改密命令已确认”，立即清空密码表单和不再需要的原值，进入 confirming-session；它本身不携新CSRF，不能继续用旧token。紧接一次真实 GET Session：必须同 User、不同于原 Session 的新ID且完整响应合法，取得其CSRF，再发布新认证状态、清初始建议并允许其它写。200后GET失败只说明新会话确认失败，不能改称密码未更新；原Session仍相同或User不同则不得沿旧上下文继续。

确认成功在密码分区就近说明“当前登录已更新，其他旧登录已失效”；保持该页，并提供返回首页。只接受 wire 的 `next_path:"/"` 作该安全目标，不用它开放任意重定向。首页初始建议据新User消失。D25 WebSocket 尚未绑定，本卡验证旧HTTP Session失效，不宣称已实现WS断连或停止Agent。

`password_change.go:19–21,145–150` 明确：已撤销的旧 Session 无法重放取回新 Cookie。若未拿到严格200，先有界检查当前 Session；看到可用身份只证明当前身份，不能证明原改密命令完成。旧Session仍当前且原意图完整时可显式原请求重试；旧Session失效/已换Session则不得把原key和密码移到新Session重放，新Cookie已丢失时须重新登录。不虚构命令lookup，不用新/旧密码自动尝试登录“探测”结果。已确认200但会话检查失败可显式再查一次或重新登录，不再提交改密。

## 6. Unknown、离页与敏感数据

资料/主题/头像成功重放沿当前权限与服务端receipt，不是公开命令查询。对 uncertain 只给以下有界动作：

- “检查当前资料/会话”：每次显式点击只做一次所需GET序列，显示当前事实；相同字段、当前版本、头像null或401均不能证明旧命令提交/未提交或原writer终局。检查失败不循环。
- “重试原请求”：仅同一User/Session/context仍经当前确认、私有原意图完整时，重用原method/path/key/version及全部值/File；未确认的改密另受 §5.3 限制。不能从正在编辑的字段重组，不能换key/version或重新分配新nonce来绕过冲突。
- “放弃本次页面操作”：明确此前提交仍可能生效，只放弃客户端确认/编辑责任，不能提示“服务器已取消”。用户随后显式重新加载并开始新意图才生成新key；不会把GET读到相同值当原receipt。

`CSRF_FAILED` 沿原身份协调器重新建立上下文，不能混用匿名token、旧Sessiontoken或静默bootstrap后重发。`IDEMPOTENCY_KEY_REUSED` 保留原意图和错误；新意图须明确放弃后重新编辑。PAYLOAD_TOO_LARGE/UNSUPPORTED_MEDIA_TYPE、字段错误、依赖失败和未知Problem code安全就近显示；保留原commit_state，不能把所有非200统一称回滚。`retry_hint=lookup` 不产生不存在的 Account HTTP lookup 方法。

本页 dirty 包含未保存资料、候选头像、主题预览或任一密码输入；route update/leave、菜单、用户入口、Logo和退出操作均经过一次“继续编辑/放弃修改”确认，不能先调用logout再询问。版本冲突仍是dirty。继续编辑保留值并还原触发焦点；放弃重置当前草稿/预览后执行原导航，普通重新进入回首项。浏览器back同样受路由guard；真实刷新/关页仅使用原生beforeunload提示，不能承诺拦住强关、保存草稿或提供自定义系统提示文案。

请求在途时，放弃/离页只是逻辑取消并隔离generation，真实请求及必要快照由原协调器持有到transport结束，不能释放队列后立即发logout/新身份操作。可能改变Cookie的改密结果需先完成协调器当前Session检查，再开放受保护写；弃置页面的晚到成功不重新打开页面或弹成功。浏览器真实退出可能直接丢失内存，重新加载从真实Session开始，不声称原提交回滚。

密码、CSRF、key、File/待重试body只放必要内存和实际请求；不进URL/history state、storage、日志、错误对象、trace/video、Service Worker或跨标签广播。确认成功、明确放弃、离页、失效时清不再需要的引用；在途私有请求仍保留最低必要内容直到结束。JS字符串/浏览器副本不能承诺物理擦除。Blob URLs逐个释放；真实离页、失效及草稿owner终止不遗留监听器/主题覆盖/脏导航拦截；仅checking造成的视图临时卸载沿 §4 保留同身份owner。

## 7. 布局与可访问性

复用 SettingsShell 的两级悬浮菜单：个人资料→基本资料，界面偏好→主题，账号安全→修改密码/既有退出操作。分组只展开折叠，叶子才导航；退出是动作，不能冒充叶子GET路由。首项/指定项和选中祖先清楚；个人设置没有项目栏。桌面侧栏与内容留白、各自受控滚动；窄屏用既有UiDrawer覆盖侧栏，选择后收起、恢复合理焦点。

表单复用UiField/Input/Button/RadioGroup等及tokens，不复制公共控件CSS。错误就近持久、aria-invalid/describedby、alert/status正确；成功在对应分区，不底部toast。提交时读取最后实际输入，包括自动填充、Enter及鼠标点击转移焦点后的值；禁用控件导致blur不得丢字符或提交旧快照。脏导航确认复用UiDialog的焦点管理，不自制无语义overlay。

1440/1024/834/390px、浅/深/system、200%缩放、长邮箱/显示名、键盘与reduced-motion均须可用，无页面级横向溢出。侧栏键盘可展开/选择，打开/关闭/路由切换焦点有明确归宿；移除头像确认和取消保留触发焦点。未认证/失效不闪现旧资料；生产继续排除Debug。没有头像显示默认身份标识，不给失败图片伪造成功替代。

## 8. 条件性精确文件范围

以下为候选 **24代码/测试路径（14生产、10测试/fixture）**，须通过 §2 后再正式授权；当前没有任何业务写权。相对冻结认证成果，8生产、2纯测试与1旧fixture需窄扩，其余13路径新增。本次新增三条旧路径有 §2 精确来源；认证最终提交后仍须末核，不以复制实现硬守数量。

| 路径 | 责任 |
| --- | --- |
| `web/src/api/client.ts` | 原认证传输窄扩固定头像raw/有界bytes；保持原JSON/Problem/取消语义，无任意URL |
| `web/src/api/account.ts` | 8新增方法、完整DTO/标量/头像响应解析，复用User解析 |
| `web/src/composables/useSession.ts` | 原唯一owner承接typed个人门面、稳定identity epoch、改密双请求、可信资料/theme与预览；不扩公开敏感状态 |
| `web/src/router/auth.ts` | safeReturnTarget闭集窄增三叶子，保留净化后的登录返回目标及dirty前置确认 |
| `web/src/views/auth/LoginView.vue` | 登录成功以同一safeReturnTarget替代硬编码 `/`；其它登录/挑战/焦点规则不改 |
| `web/src/router/index.ts` | `/settings`及三叶子注册，既有DEV/NotFound保持 |
| `web/src/App.vue` | 右上本人入口、logout前dirty确认、同一设置草稿owner跨临时checking卸载；不保留失效受保护展示 |
| `web/src/views/HomeView.vue` | 初始改密建议直达，无其它Dashboard改动 |
| `web/src/composables/usePersonalSettings.ts` | 新；App生命周期中的saved/draft/version/命令状态/离页/Blob所有权，向子页提供同一owner；无第二认证store |
| `web/src/components/layout/SettingsShell.vue` | 新；本卡真实使用的两级栏目/窄屏侧栏，布局不含API/权限猜测 |
| `web/src/views/settings/PersonalSettingsView.vue` | 新；设置根、协调子页、加载/离页和焦点 |
| `web/src/views/settings/ProfileSettings.vue` | 新；资料与独立头像分区 |
| `web/src/views/settings/AppearanceSettings.vue` | 新；主题预览/保存/取消 |
| `web/src/views/settings/PasswordSettings.vue` | 新；改密/会话确认/安全反馈 |
| `web/src/tests/personal-settings-client.spec.ts` | 新；八方法wire/完整解析/有界图片与错误 |
| `web/src/tests/personal-settings-state.spec.ts` | 新；version、Unknown、dirty/主题/Blob责任与迟到barrier |
| `web/src/tests/personal-settings.spec.ts` | 新；真实Vue路由/组件组合、字段和键盘/焦点 |
| `web/src/tests/session.spec.ts` | 窄扩新请求/改密与原owner/identity组合，适配AccountAPI mock类型；旧行为断言不动 |
| `web/src/tests/authentication.spec.ts` | 仅适配typed mock并将旧“无settings链接”断言改为允许profile/password目标；保留其它认证/权限断言 |
| `tests/account-captcha-web/personal-settings.config.js` | 新；专属selector，沿已锁本地Playwright/Chromium，无新包或下载 |
| `tests/account-captcha-web/e2e/personal-settings.spec.ts` | 新；正式dist真实浏览器场景 |
| `tests/account/authentication_web_fixture_test.go` | 仅保留constructor的私有record绑定/必要fixture引用，供同包新helper；旧launcher/断言/清理保持 |
| `tests/account/personal_settings_web_fixture_test.go` | 新；复用newAuthenticationWebFixture，正式邀请/兑换与专属browser config/安全IPC/result/进程wait，不复制root |
| `tests/account/personal_settings_web_test.go` | 新；四个真实顶层、HTTP/DB后态与浏览器证据 |

所有Go生产、Account/OpenAPI、迁移、脚本/Go module、共享Ui/tokens/useTheme、package/lock及其它旧测试均只读。特别不改认证活动文件、Object暂停修复、Artifact或Model/Resolver实现。必要范围外差量先给具体来源/触发再修规格，不能生产stub或放宽安全断言；仅上表已列的过时settings导航断言随新能力更新。验后文档仅本卡、`docs/development/frontend/README.md`、`docs/development/frontend/verification.md`，由主线程另授；状态页/AGENTS不随业务授权。

## 9. 验收与证据边界

规格静审只证明固定来源与实现可行性。先满足 §2 并正式授权，才在最终固定前端/后端闭包上实施；不能以当前 `ccf498d` 缺认证源码的树直接冒充完整新基线。

| 检查 | 必须覆盖 |
| --- | --- |
| typed client纯测 | 八方法精确method/path/header/body/status；原字节File/密码、optional缺省/null/显式空；User完整shape、大int64、Digest与Progress、204无解析；头像超限/截断/错误媒体/ETag及非200；Problem/transport/取消不伪造提交状态 |
| 状态与组件纯测 | 同一owner的typed DTO/错误，busy零transport/零新key且非void成功；visible超时后actual尾部仍占owner，忽略abort晚到barrier含头像/资料401；改密POST→GET同owner、200立即清密码、确认后超时不退回unknown、成功/失败/身份失配；稳定epoch区别每run generation，临时checking隐藏内容但同身份draft/preview保留、真实失效清理；Unknown重试/版本/分区冲突；safeReturnTarget三叶子经登录不丢且非法目标回 `/`；dirty route/back/logout与Blob/监听器清理 |
| `TestAccountPersonalSettingsWebProfileAndAvatar` | 真实本人资料修改/刷新、空显示名/username规范与重复冲突；静态JPG/PNG/WebP真实上传→服务端metadata/bytes→替换/移除，默认头像；至少一个正常小尺寸头像在服务端编码后实际小于1MiB，仍成功读回；SVG/动画/超限真实拒绝与旧头像保持。保存时最终字符/自动填充/点击与Enter快照正确；User/命令/Audit后态对应 |
| `TestAccountPersonalSettingsWebThemeAndNavigation` | 真实主题预览零写、取消、保存后刷新/重登保持；两个当前窗口造成真实version冲突且保留输入；指定叶子/首项及未登录三叶子→真实登录返回、dirty菜单/back/logout确认、同身份重验保留草稿/预览、system变化和saved/draft区别 |
| `TestAccountPersonalSettingsWebPasswordRotation` | 两个真实浏览器context同用户：当前密码错/确认不一致/弱密码反馈；成功换当前Session、GET新CSRF后资料再写成功，另一旧Session真实401；新密码登录成功/旧密码拒绝、初始建议清除，敏感表单及时清空 |
| `TestAccountPersonalSettingsWebAuthorityAndProduction` | 实际普通用户与管理员各走本人页面；失效隐藏旧内容、同源/CSRF真实生效，无body越权；生产无Debug/API不被fallback；1440/1024/834/390、浅深/system、200%/键盘/焦点/reduced motion/长文本和无溢出 |

普通用户前置必须沿 `newAuthenticationWebFixture` 的随机 `f.origin` 调用正式D07邀请/inspect/兑换HTTP。新同包helper通过旧constructor保留的私有 `old.record` 绑定读取本fixture精确邀请恢复记录；旧 `httpFixture.invite` 固定localhost origin不可直接套用。专属browser launcher位于新两Go文件，沿原预算/安全IPC/result/实际进程wait，不导入执行整份旧TS spec、不复制root或修改旧launcher。这里只做测试setup，不交付邀请前端、不用SQL造用户或角色，也不暴露生产fixture接口。正向场景真实dist→同源fixture→Account root/PG/MinIO，不用 route.fulfill、假Session/假数据或fixture业务API。关键持久后态由自有fixture精确只读核对；不把DOM成功文字当后端通过。主题preview断言同一持久User version/theme未变，改密验证真实新旧Session和后续CSRF行为，不读取密码hash或Cookie原值进报告。

Unknown/迟到transport可用纯可控promise证明前端状态机；**本卡不新增真实网络/COMMIT/ROLLBACK注入**，不得将纯失败模拟写成服务端Unknown或原writer终局证明。正常头像关闭与fixture退出不外推Object已知guard缺陷已修。D25未实现的WS行为不纳入通过声明。

纯门槛沿原 `npm run check --prefix web`（格式/Vitest/类型/build），只格式化授权文件，无安装新包/自动升级。新增Go测试用固定Go1.27.1、offline readonly做integration race compile和适用vet；后端生产不变不机械重跑无关两cmd/完整旧域。认证四个真实顶层须按受影响组合回归，原断言/预算不变；既有无关前端基线保持。

真实窗口由主线程另授。沿既有 `scripts/test-objects.sh -run '<冻结selector>'`，race/count1/包6m；新四顶层前两/后两分批，认证四顶层另按原两批，不把全部串成超预算后再过滤。单Go浏览器顶层原2分钟、Playwright workers=1/retries=0/单例45s；不自动重跑或加预算。专属fixture只用自有随机同源端口、nonce/label资源、锁定浏览器，无existing server reuse或外部账户。

截图只取清空敏感输入后的布局；trace/video/request-body日志关闭，不归档凭据、密码、CSRF、Cookie或原文件私有材料。每轮记录实际argv/env名/exit、固定selector/源码/资产/依赖指纹与首红，资源exactID两遍absent、基线不变、子进程wait/runtime清空后交窗。独立验收优先补Cookie/改密、头像当前事实和主题dirty竞争，稳定有效证据可复用，不机械重复全部场景。

## 10. 当前交付状态

本卡精确24路径的本人资料/头像、主题和修改密码已独立验收通过并获主线程采纳，源码提交推送 `c54f73f3324caa11608d84e5d207141985eb6074`，远端一致。最终候选 `fixture-input-01` SHA-256 为 `20d8fc5e3ec3ad3f5eb477345fd76d002eec057b4a0a2d0fad875e354c1a99fd`；实际来源、逐源指纹、原日志及独立结论见[个人设置验收记录](../agent-team/personal-settings-verification.md)。§1–9 技术正文、产品规则、24路径及原验收预算保持不变。

### 授权与验收接续

rev1.1 已独立规格通过并提交 `ac653cf`；rev1.2 的实际接缝、24路径和必要回归已独立静审通过，规格提交推送 `e2ec65d4bb2bf220b67efb7a88d76bf4b2ceb901`。§2 最终核查以已验认证 `9a710f272026b41ef69852bbeb41cb7670b500a8` 关闭；主线程随后将24源授予 `d08_registry_backend`、独立验收授予 `skill_verification`。实施只消费该固定业务基线和本卡源，未混入活动 Resolver/Artifact。原修订与预审/最终前置记录见[规格与前置验收记录](../agent-team/personal-settings-spec-verification.md)；其中“待授权/待认证提交”是历史状态，不代替本次已执行验收。

作者实际通过完整工程检查的9文件110个纯测试、格式、类型及正式构建，以及离线 integration race compile/vet、浏览器 spec 类型和配置检查。新增4个完整Go顶层/4个真实浏览器case均通过，覆盖 §9 的资料/头像、主题/导航、改密和权限/生产布局。原认证4个顶层名称/6个浏览器case采用分项组合：lifecycle/desktop先通过，同轮keyboard在旧 `Response.json()` 发生CDP读取body失败；原父组与driver保留FAIL。原日志无该响应status/body，原因未知，不补推401或ChallengeRequired。主线程在限定归因后只授权一次原样keyboard子例，实际通过；随后revocation/expiry/layouts原组通过，不能称为原父组一次全绿或该首红已被产品修复。

独立真实增量 `TestIndependentPersonalSettingsLiveOwnerRevocation` 一顶层/一case通过：同Session重验保留资料/File/Blob，另一窗口实际改密后用新CSRF写入，原窗口真实401清除旧草稿/头像且不被离页确认阻挡，同文档重新登录不复活旧稿；PG核对真实命令、Audit和Session后态。独立最终报告 SHA-256 为 `ada59561329eb0c5d8b9b28720e6a548601a09b27966e04df4ea40897ae92b9c`，主线程已读并采纳固定24范围。

Core F1/F2、UI-F1/UI-F2的原红、修复与分版本复用均保留；原类型、测试选择器/清理时点及TS命令准备失败不与产品缺陷混称。Core F2中间源与首fixture TS源为事后按SHA精确重建。独立真实首轮因私有runtime路径长度前置失败，Node/browser未启动；清零后仅换短路径，同probe及原预算一次通过，不能称产品修复。作者首组缺Node PID/starttime采样，仅有launcher实际 `cmd.Wait` 返回0，不能用后续捕获反填。五轮作者、两轮独立的自有4容器/3网络均实际双清，原2容器/4网络不变，输入前后同SHA，所有动态命令已停止并交回资源窗口。

### 完成边界

三个真实叶子为 `/settings/profile`、`/settings/appearance`、`/settings/password`；`/settings`进入profile，右上本人入口及首页初始改密建议均指向已实现页面。资料、头像、主题和密码独立保存，仍复用唯一Session/request owner。Unknown和迟到transport只沿受控纯测试证据，不声称真实服务端COMMIT、ROLLBACK或网络故障验收。

本次只关闭本卡个人设置完整结果；邀请/恢复等后继页面、生产SPA托管与真实Vite开发代理浏览器验收、Object既有Runtime/guard缺陷及Artifact阻断、Summary待决和完整D26/D27未完成边界保持。正常头像操作、Blob URL撤销与fixture退出不能证明Object异常join已修。验后只归位本卡及两份前端指南，状态页和证据主档由主线程另行安排。
