# D27：System SMTP 配置与私有凭据 UI

状态：rev1，2026-10-06 已获独立静审通过（STATIC PASS）并由主线程采纳；被审全文 SHA256 `c1fd5785c1be9796e872c1d21b38106a51ccf1bfa1954e68e0e5178a9a4f72d8`，技术§1–7 SHA256 `896a1c38496be34f50171decdfd452726797eba23b6c1dc1c7e0adae3769b62e`。本次仅同步页首采纳状态，技术正文保持被审原字节；architecture_worker仅获本卡页首唯一写权。账号安全产品尚未完整接受，下列29条仍是候选路径，产品实施、真实资源与后继投递页面均未授权。

固定后端/产品基线为 `870ebbb986f56bb62be34ccdfa2c77819ce995a6`，沿已接受迁移1–19。[账号安全 rev1](d27-system-account-security-ui.md)已获独立静审/采纳，技术§1–7 SHA256 `e83033c44b4eff79987913dc9baf9f2a82fe4a5d77a5e175e37663d22c7dbc1a`；其产品尚未完整接受。本卡仅依该已定契约规划第八依赖、七叶子/三组、第十一return与共同确认生命周期，不消费其活动源码。实施前必须等待账号安全完整接受，固定实际提交并复核共享签名、菜单断言和候选取舍，再由主线程另授唯一作者与资源窗口。

## 1. 完整结果与真实依赖

系统管理员在“平台配置 → SMTP”读取、保存 SMTP singleton，安全保持/替换/移除私有认证密码，显式停用并清除传输配置，维护有限自动重试策略，并处理冲突、未确认结果与原请求重放。仅接 GET/PUT/unconfigure 三口；保存成功只确认配置，不证明连接、认证、发送或收件成功。

| 依赖 | 证据与门槛 |
| --- | --- |
| 正式 Account/SMTP 服务 | **已满足**，[D07 当前范围](d07-account-session-smtp.md#当前进度)的真实根、当前管理员、SMTP singleton/版本、Secret、命令幂等、typed Audit 与迁移1–19已接受；B03/B04 的正式服务在固定基线内。本卡不新增后端、迁移、凭据端口或发送资源。 |
| 现有系统壳、Cookie owner、焦点 | **已满足**，固定基线含已接受用户、邀请、Provider、Model及Selection；沿[共享模态恢复](d27-modal-focus-restoration.md)、[个人设置](d26-personal-settings.md)与[Provider私有材料纪律](d27-system-provider-management-ui.md#3-固定-api严格解析与材料预算)。SettingsShell/Ui/useLayer只读。 |
| 账号安全共享前端接缝 | **产品门槛未满足**，仅沿[账号安全 §5–7](d27-system-account-security-ui.md#5-共享-owner身份与确认生命周期)的第七依赖、独立域、平台配置组/六叶子/十return、App期状态及View末尾确认宿主。接受后核实际导出/清理分类/旧测试，不把规格或活动源码当已验能力。 |
| 已定产品规则 | [SMTP Delivery](../../architecture/platform-infrastructure/authentication/smtp-delivery.md)、[D07 SMTP工程契约](d07-account-session-smtp-design.md#8-smtp-配置outbox-和真实投递状态机)、[系统设置 §6](../../frontend-design/layouts/system-settings.md#6-安全审计与平台配置)、[通用设置](../../frontend-design/layouts/settings-shell.md)、[样式](../../frontend-design/styles/README.md)、[组件接口](../frontend/components.md)及[D27计划](../development-plan.md#d27-业务页面与设置)。本卡范围没有待用户决定的新产品含义。 |

固定源码依据为 [SMTP服务](../../../internal/central/account/smtp_settings.go)、[HTTP三口及DTO](../../../internal/central/account/http_system.go)、[正式契约](../../../internal/central/account/contract/delivery.go)、[Account OpenAPI](../../../api/openapi/account.json)与[既有真实管理用例](../../../tests/account/http_admin_test.go)。静态核对不冒充本UI的动态验收；旧真实用例已覆盖保存/停用、凭据不回显、旧请求重放的历史applied_version与较新current settings、权限撤销后拒绝重放。

后继独立完整结果承担测试收件人/显式测试发送、投递任务读取/人工重试及真实SMTP协议组合；本卡不写其规格或实现，不提供测试/连接检查按钮、空投递区或假成功入口。其202/job版本、unknown与可能重复投递必须按正式D07另行验收；本卡接受不代表全SMTP页面、D27或Runtime已完成。

## 2. 页面、配置与凭据行为

仅新增懒加载 `/system/smtp`，继承 protected/systemAdmin；在账号安全建立的 `platform-configuration` 组追加“SMTP”，形成七叶子、三个分组。`/system`默认用户页、原六叶子和分组行为不变；return闭集十项增十一项，仅新增该精确路径，query/hash/数组/外部地址/动态后缀及未知项仍拒绝。普通用户无入口，直链不挂载本页、不派发管理请求。

初次正式GET成功前不填默认配置或允许保存；失败/404/null不能解释为未配置。页面分别显示当前configured/version、凭据“已配置/未配置”安全状态、编辑草稿与本次原请求确认。`configured=false`显示后台链接由授权运维获取/转交的说明，不读取或展示恢复日志、完整链接或token；已配置只显示配置事实，不以失败降级为后台日志，不提示已测试可用。

使用单个内联表单。已配置时编辑全部传输字段与重试策略；未配置时只编辑重试策略，保存通过unconfigure三字段请求，另有明确“配置 SMTP”进入完整配置草稿，成功通过PUT转为configured。进入完整配置不猜主机、端口、加密方式或认证信息，重试初值来自当前GET；取消未提交配置返回当前未配置状态。保存无变化/非法输入零写入，无自动保存、测试、默认值回写或自动重发。

| 字段/动作 | 界面及正式边界 |
| --- | --- |
| host / port / encryption | 主机是ASCII DNS A-label或合法IP，不含scheme/path/userinfo/query/fragment/zone；≤253字符，端口1–65535，UI字符串输入严格转为wire整数。明确选择 `tls/starttls/none`；none是正式选项，不添加“仅内网”规则、不绕过出站授权，加密失败不自动降级。 |
| username / sender_email / sender_name | username≤256 UTF-8 bytes；发件邮箱为≤254 bytes裸ASCII地址，无空白/显示名/注释；显示名≤80 codepoints且≤320 UTF-8 bytes，可为空。文本合法Unicode且无控制字符，保留合法空格；沿正式服务校验/规范化DNS/IP和小写邮箱，不由普通文本控件偷偷trim、补协议、端口或邮箱。 |
| password / credential_action | 仅写入的新密码1–2048 UTF-8 bytes；拒未配对surrogate、NUL、CR、LF，不trim或截断，合法空格不是空密码。空字符串/未输入保持旧凭据；非空新材料用 `credential_action:"keep"` 加password替换，**没有replace枚举**。显式“移除凭据”用remove且无非空password，必须同时将username设空；取消尚未提交的移除/清空新材料回到保持，不改变原保存值。 |
| 认证一致性 | 成功配置只能username空且无credential，或username非空且有credential。新建认证必须输入新材料；保持时只依据当前 `credential_present` 提示现有状态，不读取或填写旧密码/ref。已派发后原action/password不可因编辑输入框而改变。 |
| auto_retry_count / retry_interval_seconds | 字符串输入和canonical十进制wire，分别0–5、10–3600秒；默认3/60仅作说明。次数指首次之外的最大自动尝试，0不表示禁止首次投递；新策略影响随后attempt，不重置已有用量，不显示后台退避/jitter为可编辑字段。 |
| 停用并清除配置 | 仅当前configured且无进行中/未确认写时开放；有未提交草稿先经统一放弃确认。之后捕获当前观察的id/version和**已保存**重试两值，确认明确清除主机/认证/发件信息及凭据、保留这两值；取消零请求，确认使用unconfigure。并发变化由原version裁决，不偷取草稿/默认策略或声称撤回在途投递。 |

无认证时移除密码不会停用SMTP；unconfigure才使configured=false，并由服务安全释放旧引用。旧凭据/投递可能仍有既定使用或清理尾部，UI不承诺立即物理擦除、撤销已发邮件或取消在途副作用。配置正常保存反馈、未配置策略保存反馈及停用确认分别使用准确文案，不能统称邮件发送成功。

当前观察、编辑基线/普通草稿、私有新材料、原intent分离；读取失败保留同身份草稿/意图，观察区如实标失败。普通刷新/取消若会覆盖草稿须统一确认；明确冲突重读或检查原请求不得清草稿。重挂不回填私有材料，只显示“已保留新密码输入”及清除动作。使用UiField/UiInput/UiButton/UiState与既有tokens，成功反馈沿2400ms及可访问状态；固定字段/安全区域错误不显示raw detail。窄屏单列、长文换行，无页面级横向溢出。

## 3. 三项正式 API、严格 DTO 与私有材料

新增 `web/src/api/system-smtp-settings.ts`，导出不可变 `SMTPSettings`、`SMTPUpdateInput`、`SMTPUnconfigureInput`、`SMTPResult`、`SystemSMTPSettingsAPI`及正式工厂；共享纯工具/WriteOptions沿已接受导出，原Account/User/Session/API shape不变。敏感输入只由私有owner调用API，不通过公共响应式状态交付。

| 方法 | 唯一wire及成功 |
| --- | --- |
| `getSettings(signal: AbortSignal)` | GET `/api/v1/system/smtp`，无query/body/key/CSRF；200 `SMTPSettings`，为该次当前观察。 |
| `updateSettings(input: SMTPUpdateInput, options: WriteOptions)` | PUT同一路径；body恰version/host/port/encryption/username/sender_email/sender_name/credential_action/auto_retry_count/retry_interval_seconds，可有非空password；200 `SMTPResult`。保持时省略password，action始终显式keep/remove。 |
| `unconfigure(input: SMTPUnconfigureInput, options: WriteOptions)` | POST `/api/v1/system/smtp/unconfigure`；body恰 `version,auto_retry_count,retry_interval_seconds` 且均必填；200 `SMTPResult`，不是204。不发送configured、空传输字段或凭据。 |

两写口的WriteOptions仅私有协调器构造原key、原Session CSRF及signal。拒未知输入字段、错误方法/options/query、数组、null、数字编码混用和超界；版本是1–9223372036854775807的canonical十进制字符串，比较/后继用BigInt，GET可读最大值，新写禁止无合法后继的原version。重试两值同样是范围内canonical字符串，拒符号、空白、前导零、小数/指数/强转；端口是有界整数number，不能用重试字段的字符串编码代替。

Settings恰12个必填字段：`id,version,configured,host,port,encryption,username,sender_email,sender_name,credential_present,auto_retry_count,retry_interval_seconds`。id为canonical UUIDv7，configured/credential_present严格bool，其余遵上述边界。configured=true要求合法传输字段及username/credential_present一致；false要求host/encryption/username/sender_email/sender_name全空、port=0、credential_present=false，重试仍合法。拒丢字段/额外字段、坏行、无效组合，完整解析成功后一次发布不可变值；首次GET捕获singleton ID，同身份后读及写结果必须匹配，不生成客户端ID。

SMTPResult恰 `{applied_version,settings}`；applied_version必须等于私有原version+1，settings严格解析且id为捕获singleton，settings.version≥applied_version。**applied_version是原命令历史结果，settings是重新授权后的当前读取**，可能已被另一保存/停用推进；不能要求其配置/凭据/字段等于原草稿，不能把current version当applied_version。尤其旧PUT重放可返回当前未配置，旧unconfigure重放可返回当前已配置。结果只在完整匹配原请求身份/版本及严格DTO后确认，不能取缺失settings的半个200作成功。

client.ts仅加三固定endpoint的闭合分类；复用同源Cookie、no-store、redirect:error、媒体/status/安全Problem和实际流清理。**仅本卡PUT**按正式HTTP边界允许序列化JSON的UTF-8 body≤32KiB，unconfigure仍≤16KiB；每个原字段长度与完整JSON编码预算在创建intent/key和dispatch前分别校验。2048bytes材料不是2048个JS字符，合法控制字符的JSON转义计入32KiB，不扩大其他Account端点；响应/Problem继续600000B，原listProviders成功2MiB例外保持。流累计实际字节，超界/非法/截断零候选，read/cancel实际join后才释放owner。

材料只在输入DOM、App期私有非响应式内存及实际请求必要临时副本；公开reactive state/provide/props/安全receipt、Problem/异常、日志/console、URL/history/storage不得含password、完整body、key或CSRF。输入经受限提交/清除动作转交私有owner，公开仅是否有材料及安全阶段，无材料读取接口；不承诺JS全部字符串物理擦除。清DOM不等于清原intent：按§4仍需重放时保有精确原密码；已确认且实际尾部join后尽早清本方可达材料/body引用和可擦除buffer。明确放弃/失效立即禁止读取/重放，只为旧实际请求尾部保留最少必要引用，join后清除。

## 4. 历史 applied_version、当前观察与原请求恢复

正式命令身份为 `account.smtp`、owner=UserID、name=`smtp-settings-update`；PUT/unconfigure均由同服务执行，语义MAC包含原version、规范化传输字段、configured、action、重试策略及精确非空密码材料。当前管理员授权后查原key/MAC；已提交命令先于当前version/credential裁决返回历史receipt。首次写先计划命令/准备Secret，再在当前授权及版本/ref重验下原子提交设置、命令结果、Secret引用变化/清理与typed Audit。UI不复刻这些后端阶段，也不调用Model Credential或自行删除旧Secret。

**HTTP两写口均在服务写完成后再调用GetSMTPSettings，后读也可能失败。** 因此已派发写收到5xx/RESOURCE_BUSY/transport/取消/超时、非法响应或未知结果，哪怕Problem标not_started/not_committed，也不能由这个后读阶段标签断言原写未接受；须保留完整原intent和精确原密码，不能显示已保存、清材料再靠缺密码的新body重放。当前401/403/CSRF失效按§5销毁私有恢复资格，但仍不声明写已回滚。

没有公开Account command lookup。新增私有不可变intent绑定完整identity/原CSRF、singleton、原path/action/version、完整body/稳定key及材料；首次派发前全部校验，双击零额外key/请求。恢复沿[个人设置 §6](d26-personal-settings.md#6-unknown离页与敏感数据)和[账号安全 §4](d27-system-account-security-ui.md#4-历史成功当前观察与无-lookup-恢复)的同owner原则，但确认载体必须使用本卡SMTPResult，不能复用账号安全历史Settings的成功/后读次序：

- 本地未派发/busy，或没有更早未确认尝试且可明确定位为写入前的INVALID_ARGUMENT/字段错误、VERSION_CONFLICT等业务拒绝（合法not_started/not_committed），显示已知拒绝并保留输入；不要将所有非200误归未知。首次版本冲突保留草稿/私有新材料及原基线；显式重读只显示当前，用户核对并明确采用最新值后才可建立新编辑基线/新key，禁止自动合并或改原expected version。
- 已派发后没有严格匹配SMTPResult的其它不确定结果保持“请求结果未确认”；COMMIT_UNKNOWN、unknown/committed Problem亦如此。IDEMPOTENCY_KEY_REUSED锁住原key/body提示冲突，不自动换key。曾未确认时后续拒绝不抹去先前不确定性，不能用新版本/相同字段/current credential_present或configured判断原写是否提交。
- “检查当前会话与配置”只显式Session重验；同完整identity/原CSRF/当前admin成立后至多一次GET。该读取仅是当前观察，没有found/missing receipt含义，不触发后台轮询、自动重发或Model lookup。失败保留原intent和材料，不以当前已停用/凭据缺失挡住合法历史重放。
- “重试原请求”仅在原上下文仍合法、原intent完整时显式再次执行相同method/path/key/body/version；PUT继续带精确原password，不用当前草稿/GET值重建。返回严格SMTPResult才记录原操作/applied_version确认，并单独更新较新当前观察；不以较旧current读覆盖已观察的更高版本，不自动再次GET或提交。成功后清已完成恢复材料并用有效当前观察建立下一次编辑基线；若当前状态与原操作不同，明确显示“原操作已确认，当前配置已变化”。
- 主动“放弃本次操作”说明此前请求仍可能生效，仅停止客户端跟踪、清旧材料/intent；实际owner尾部未完仍忙。放弃后需显式GET成功、重新编辑才能新key，不自动补偿unconfigure/删除Secret。页面刷新丢失私有材料不代表服务回滚，不提供无原密码的虚假恢复。若未确认intent仍存在，不能借冲突/取消草稿流程悄悄替换它。

unconfigure保留捕获的两个重试值，重复重放不再次清除后来保存的配置；当前读取结果以真实Settings展示。停止配置不保证终止已开始的投递或清理，保存/取消/重放也不触发测试、邀请或重置。已严格确认后发生独立GET失败只标当前观察失败，保留原applied确认，不重发已确认操作。

## 5. 第八依赖、身份与共同确认生命周期

在账号安全接受的前七个依赖后追加第八个可选 `SystemSMTPSettingsAPI`（默认正式工厂），保持旧参数顺序/调用方。新增明确 `smtp-settings-read/write`、独立revision/私有intent/材料/progress；不落入personal、账号安全或其它system域的默认分类。三API及恢复Session检查都走唯一Cookie owner，无第二队列、认证缓存、直接fetch或任意特权回调。

沿原30秒可见预算；取消、超时、放弃、离页或身份失效可停止可见等待并隔离代次，但fetch/body read/cancel实际finally未join前不能释放owner或让新身份/其他域越过。初次未派发busy可延迟一次，离页/失效后不续发、不循环重试。各域abandon双向隔离，SMTP不可撤个人/users/邀请/Provider/Model/Selection/账号安全；反向同样成立。App.dispose/auth.leave保持全局清理和原退出协议。

两写口均纳入当前完整identity+owner generation的CSRF_FAILED护栏；当前及同身份迟到401沿既有Cookie规则，旧身份/旧代次失败不污染新账号。当前403清全部系统私有状态，新增本域观察/草稿/材料/intent也须清，发布原身份绑定拒绝而不改User.role或自动登出；成功Session重验才能恢复资格。真正换账号、同User换Session/epoch、CSRF失效、失权、注销/dispose销毁旧材料与恢复资格，但不越过旧实际尾部；临时checking仅隐藏受保护内容并保留同身份App期状态。

App创建/provide `createSystemSMTPSettings`，持有普通草稿/观察、安全材料状态、确认状态与Promise；敏感值/请求仍在私有owner。View只承载DOM，本叶子且身份确认才attach；dirty含字段/策略/配置模式变化、新材料、显式移除、冲突草稿、进行中/未确认写。取消、覆盖重载、菜单/本人入口/退出与浏览器返回统一放弃确认，路由确认先于Session重验，beforeunload沿原原生提示。停用确认独立于统一放弃确认，捕获目标及策略后不随下层输入变化；两者至多各有一个待决Promise。

View模板末尾依序放停用确认、统一放弃确认UiDialog；App不新增常驻Dialog。checking共同卸载全部本域模态，App的草稿/原密码/Promise保留，detach不能当真实离页销毁；检查失败时原App恢复按钮可用、无残留overlay/inert/滚动锁。同完整identity/admin恢复后同序重挂，确认仍顶层/可响应，零新写/key/自动答案，私有密码不回显DOM。确认打开时mounted标题不抢焦点；await后DOM续体核本实例仍挂载/节点当前可用，确认关闭只消费共享恢复，不用全局querySelector、手工focus或反复重挂修层序。真正离页/新身份/失权/dispose以false结算所有待决确认并清旧状态，旧导航/退出续体不能迁到新身份，继续/放弃/停用至多结算一次。

## 6. 唯一候选路径与实施移交

以下29条仅为规格候选。账号安全完整接受后先核第八依赖、公开类型/分域分类、平台组六叶子/十return与旧测试准确位置；无必要候选可剔除，新增路径/公共语义先报主线程。共享产品文件/测试及README与上游串行，不消费未验活动实现。

| # | 路径 | 唯一用途 |
| --- | --- | --- |
| 1 | `web/src/api/client.ts` | 三固定SMTP endpoint/精确options，PUT独占32KiB，其余预算与实际清理保持。 |
| 2 | `web/src/api/system-smtp-settings.ts`（新） | 三API、闭合输入/Settings/SMTPResult解析与原applied核对。 |
| 3 | `web/src/composables/useSession.ts` | 第八依赖、本域owner/私有材料及原intent、CSRF/系统失权与实际尾部。 |
| 4 | `web/src/composables/useSystemSMTPSettings.ts`（新） | App期普通草稿/观察/历史确认、冲突/恢复及两类确认Promise。 |
| 5 | `web/src/App.vue` | provide、导航/顶部退出hook与dispose组合，不增Dialog宿主。 |
| 6 | `web/src/router/index.ts` | SMTP正式叶子，原默认页/权限保持。 |
| 7 | `web/src/router/auth.ts` | 第十一精确return与本域离页/完成hook，旧退出保持。 |
| 8 | `web/src/views/system/SystemSettingsView.vue` | 既有平台配置组追加SMTP，原六叶子/三组保持。 |
| 9 | `web/src/views/system/SystemSMTPSettingsView.vue`（新） | 内联字段/私有输入、配置/策略保存、停用及末尾确认宿主/局部布局。 |
| 10 | `web/src/tests/system-smtp-settings-client.spec.ts`（新） | 三wire、严格DTO/编码预算、applied/current及传输尾部。 |
| 11 | `web/src/tests/system-smtp-settings-state.spec.ts`（新） | 生产controller+受控transport，材料/原重放/冲突/双向owner/身份屏障。 |
| 12 | `web/src/tests/system-smtp-settings.spec.ts`（新） | 真实App/router/controller的配置/策略/确认重挂与菜单/return。 |
| 13 | `web/src/tests/system-user-directory.spec.ts` | 仅七叶子/新合法目标兼容，默认用户/权限保持。 |
| 14 | `web/src/tests/system-invitations.spec.ts` | 仅菜单七叶子/新目标兼容，分组仍3，原写/恢复/cohost保持。 |
| 15 | `web/src/tests/system-providers.spec.ts` | 同上菜单兼容，原两步凭据/恢复断言保持。 |
| 16 | `web/src/tests/system-models.spec.ts` | 同上菜单兼容，原CRUD/impact/替代/恢复保持。 |
| 17 | `web/src/tests/system-model-selection.spec.ts` | 同上菜单兼容，原选择器引用/恢复保持。 |
| 18 | `web/src/tests/system-account-security.spec.ts` | 待接受核实：仅实际菜单/合法目标兼容，原配置/恢复/确认断言保持。 |
| 19 | `web/src/tests/personal-settings.spec.ts` | 十项return增十一项的说明/正反例，旧本人草稿/退出保持。 |
| 20 | `tests/account/system_smtp_settings_web_test.go`（新） | 下节真实顶层及自有设置/命令/Secret引用/Audit旁证。 |
| 21 | `tests/account/system_smtp_settings_web_fixture_test.go`（新） | 正式隔离fixture、专用launcher与有界响应控制，不增SMTP socket或改旧fixture。 |
| 22 | `tests/account-captcha-web/system-smtp-settings.config.js`（新） | 专用Playwright配置/私有输出，原预算保持。 |
| 23 | `tests/account-captcha-web/e2e/system-smtp-settings.spec.ts`（新） | 生产dist+真实后端的保存/凭据/恢复/权限/导航布局。 |
| 24 | `tests/account-captcha-web/e2e/system-invitations.spec.ts` | 暂候选：旧Drawer全链接仅6→7；870ebbb:1528为5、账号安全约定改6，待核接受源。 |
| 25 | `tests/account-captcha-web/e2e/system-providers.spec.ts` | 暂候选：旧全系统链接仅6→7；870ebbb:1192为5、账号安全约定改6。 |
| 26 | `tests/account-captcha-web/e2e/system-models.spec.ts` | 暂候选：旧系统/Drawer两处仅6→7；870ebbb:1923/1991为5、账号安全约定改6。 |
| 27 | `tests/account-captcha-web/e2e/system-model-selection.spec.ts` | 暂候选：旧系统/Drawer两处仅6→7；870ebbb:1864/1963为5、账号安全约定改6。 |
| 28 | `tests/account-captcha-web/e2e/system-account-security.spec.ts` | 待接受核实：仅实际全系统/Drawer六链接6→7，不存在则剔除；其它断言不改。 |
| 29 | `docs/development/frontend/README.md` | 产品独立接受后最后同步三口/七叶子/十一return、实际证据和后继未交付边界。 |

旧菜单变更不增加分组数量，不用重复组/CSS隐藏/削减链接规避计数；旧Tab困陷、Escape/遮罩、焦点、确认与身份断言保持。实施者必读[Vue技能](../../../.agents/skills/agenteam-vue-development/SKILL.md)、[Vue测试技能](../../../.agents/skills/vue-testing-best-practices/SKILL.md)，独立验收读[验证技能](../../../.agents/skills/agenteam-verification/SKILL.md)与可用Playwright技能。后端/OpenAPI/迁移、SettingsShell/Ui/useLayer/全局CSS、旧API/controller/View、旧fixture/driver/脚本、锁及归档只读；不新增依赖、DDL或改台账/continuation。

## 7. 高风险验收与资源门槛

开工固定账号安全完整接受提交、实际候选清单和接缝；作者运行 `npm run check --prefix web`，仅格式化授权路径。Go1.27.1/local，`GOPROXY=off GOSUMDB=off GOFLAGS=-mod=readonly`下精确integration-tag race编译/适用vet，准备阶段单列。纯测、静审、编译不冒充真实HTTP/事务/浏览器通过。

纯测覆盖12字段Settings/11种可能PUT成员/3字段unconfigure/2字段Result闭集，configured两分支、username/credential一致、host/邮箱/Unicode/整数字符串/port/最大version和密码UTF-8上下界；合法空格、空password保持、remove与替换互斥、缺retry拒绝。核32KiB/16KiB/600000B与旧端点预算不变，完整编码预算/无变化/非法输入/双击零intent或派发，坏媒体/状态/截断/非法DTO/超限read/cancel实际尾部。当前更高版本/相反configured状态仍可确认原applied，错singleton/version/缺settings拒绝；公开state/错误/DOM恢复/storage中无材料或原key/body。

生产controller+受控transport分别阻塞fetch、body read、cancel；30秒可见结束/取消/放弃/身份变化后owner仍忙，原实际join前所有GET/写/Session/其它域不得越过，晚到零发布。单列写后GET报错（含not_started）仍保留原password/body/key的纯机制例，与真实接受后丢失响应证据分开；不能把代理故障称为服务内真实读故障。覆盖明示写前业务拒绝/首次VERSION_CONFLICT/先未知再拒绝、原CSRF/同User新Session、当前及迟到401/403、全部既有域双向abandon及App总dispose，材料只在合法恢复阶段保留、终局后清引用。

真实App页面纯测分别打开停用确认和dirty/未确认放弃确认，再对生产listener合成pageshow并保持Session GET待决：checking无本域overlay，失败App恢复可用，同完整身份恢复仍可按钮/键盘响应、零新PUT/POST及材料回显。覆盖待决路由/退出、真实失权/换身份/dispose的false终点、旧实例await续体及标题不抢焦点；不直接结算Promise替代交互，jsdom层序证据与真实浏览器焦点分列。

| 新真实顶层 | 必须覆盖 |
| --- | --- |
| `TestAccountSystemSMTPSettingsWebLifecycle` | 正式GET/PUT配置三个明确加密选项的持久值、无认证配置、字段/策略边界、取消与无变化零写；未配置保存重试策略、完整配置、停用保留明确策略，刷新取实际值。保存不创建test/delivery intent或请求发送口，无连接成功暗示；这是配置验收，不重跑三模式SMTP协议。 |
| `TestAccountSystemSMTPSettingsWebCredentialLifecycle` | 正式保存新材料、空输入保持、替换、显式remove+空username、不匹配拒绝、停用清引用。只读旁证自有Secret引用切换/清理登记和typed Audit，不输出或解密材料、不宣称旧在途材料已物理销毁。确认已派发时禁止修改原password；页面/响应/安全日志无明文，checking重挂保留私有输入而不回显。 |
| `TestAccountSystemSMTPSettingsWebConcurrency` | 两个真实管理员同version，以不同key分别配置/替换或停用，只有先提交成功，后者真实409保留普通草稿/新材料。显式当前读取/人工核对后重新编辑，新写仍正式裁决；旁证设置、已提交命令、凭据引用和Audit原子且无拒绝后的部分配置，不SQL制造version/值。 |
| `TestAccountSystemSMTPSettingsWebOutcomeRecovery` | 自有同源代理在真实PUT接受后有界丢失响应，另一正式会话unconfigure/再配置推进当前；原页保持精确材料，Session/GET只观察，原key/body/password显式重放确认原applied而显示较新current，命令/Audit不重复。另以真实unconfigure接受后响应丢失、后续正式PUT复原配置，再原POST重放确认历史停用但当前仍配置；观察失败/明确放弃不回滚、不自动新key，不调用lookup/test/jobs。 |
| `TestAccountSystemSMTPSettingsWebAuthorityAndIdentity` | 普通用户入口/直链零管理请求；正式Session撤销/精确自有admin失权后真实GET/新写/原请求重放拒绝，清材料/草稿/intent。换账号/同User新Session无旧恢复，新增两写CSRF护栏/实际Cookie尾部独立证据；不以fulfill假403冒充权限。 |
| `TestAccountSystemSMTPSettingsWebNavigationAndLayouts` | 三组/七叶子、默认用户、第十一return、平台组active/展开/键盘、dirty/未确认离页。两类确认已开时合成pageshow，Session GET一次受限失败/App恢复同Session后实际继续/放弃/停用可响应且无自动写；light/dark×1440/1024/834/390，长字段/错误、Drawer和Dialog的Tab/Escape/遮罩/焦点、reduced-motion/无溢出。合成事件不冒充真实BFCache。 |

配置/策略/冲突全部使用正式HTTP/服务；只沿既有隔离fixture纪律对任务自有精确账号准备/撤销管理员资格，其他业务表不写，SQL只旁证自有settings/commands/Secret引用/Audit及必要的准备终局。普通用户/第二管理员在SMTP未配置时沿既有正式邀请→受限日志→redeem准备；等待这些精确自有日志投递及attempt实际终局、无待发送/在途工作后记录基线，不能要求历史任务表为空或直接改任务phase。沿正式根/隔离PG/MinIO/Secret与生产dist同源，不新建SMTP listener/CA/出站放行；浏览器SMTP配置操作期不调用test、邀请、reset或jobs，投递intent/job数量相对准备基线零新增。该前提下的零新发送不证明现实旧任务无I/O。密码/Cookie/CSRF/MAC/raw请求不进入证据日志，实际body等价可在自有进程内比较后只输出脱敏断言。

六新组按精确名称分组执行 `scripts/test-security.sh -run '^TestAccountSystemSMTPSettingsWeb(Lifecycle|CredentialLifecycle|Concurrency|OutcomeRecovery|AuthorityAndIdentity|NavigationAndLayouts)$'`。Playwright每test45秒、Go顶层2分钟、workers=1/retries=0、race/count1/每包6分钟保持；按实际耗时分组，不放大预算、削断言或将no-tests计PASS。旧真实回归沿账号安全§7已定集合，再含账号安全最终接受的OutcomeRecovery/AuthorityAndIdentity/NavigationAndLayouts；菜单仅上述精确6→7。语义/输入未变可复用证据，密码恢复/停用竞态/权限与共同确认必须有独立验证。

仅账号安全相关命令停止、资源双清及完整接受后，由主线程另授本卡唯一作者与独占窗口。作者冻结源/dist/锁/环境、保留首红及实际argv/env/退出/安全原log，交未参与实现的verification_worker；每轮实际wait、server/browser/所属进程join、自有exact-ID双次absent及基线不变后交回。产品独立通过并主线程采纳后才写第29路径。本卡没有运行真实资源；后继测试发送/投递管理、全SMTP页、完整D26/D27、生产SPA/真实Vite代理、Runtime/ready503仍未交付，Summary待定及Object/tools原停止保持。
