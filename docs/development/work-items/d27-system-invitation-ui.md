# D27：系统待注册邀请管理页面

状态：rev3，2026-10-06 已获 `recovery_documentation`（verification_worker）独立静审通过（STATIC PASS），主线程已采纳；被审稿 SHA256 `7944834eedbc4a9be829e168bb40cfc452965fbb5a724aef367827a2a4355204`。本修订按正式 producer 历史与读口契约纠正 §2 的无 attempt 兼容表述，保留 cancelled/null 与 processing/unknown 的既定例外，不放宽后端 scanner 或新增产品规则。邀请读口已独立 PASS、接受并提交推送 `9b3201547f9b7b61fd9716a6ba6540084961496c`，迁移前缀1–19正式接受，所属真实资源已实际清零并交回；本卡上游依赖已满足。主线程现授权 `directory_frontend` 唯一实施 §6 二十二条产品路径，并授予唯一真实资源窗口；实际开工仍等待主线程正式下发，不由本次文档更新自行启动。本次仅更新页首和依赖表对应行，其余技术原字节及采纳语义保持；下文设计冻结时的候选/等待叙述以本页首和更新的依赖状态为准。规格采纳、上游接受及实施授权均不代表本页面产品通过。

## 1. 完整结果与依赖

管理员在现有系统设置中管理仍有效的待注册邀请：分页查看邮箱、创建/到期时间和最近一次已接受投递任务，创建邀请、重发有效邀请、人工重试投递、确认撤销，并能保留和恢复结果未确认的管理请求。只新增“用户与邀请 → 待注册邀请”这一完整叶子；普通系统入口仍默认“用户”。不提供链接/token 查看、复制或代设密码，不增加 SMTP 配置、全局邮件任务页、已注册/过期/撤销历史、批量操作、角色管理或模型入口。

固定已接受前端输入 `7ef3e30cf06b5df6d19516f24c34855a8308ef05`。该提交已有真实系统设置壳、用户目录和共享遮罩焦点修复；`useSession.system` 目前仅有 users GET，`App` 在会话 checking 时会卸载 RouterView。下述邀请写 owner 和 App 生命周期内的草稿是本卡实现内容，不声称现状已有。正在修改的邀请后端八路径不作为冻结源；接口设计按其已采纳规格引用，开工时再绑定实际接受提交与迁移组合。

| 依赖 | 本卡消费与门槛 |
| --- | --- |
| 系统设置壳与用户目录 | **已满足**，`7ef3e30` 已独立最终 PASS、主线程采纳；[正式验收报告](../agent-team/system-user-directory-ui-verification.md)与证据已归档于 `d06afca8558a2579a646714f3fd8a0968d8827b1`，范围见[目录工作项](d27-system-user-directory-ui.md)。保留 `/system` 默认用户页、管理员导航、普通身份拒绝、signed cursor 与请求实际结束纪律。 |
| 认证、个人设置、公开入口 | **已满足**，见[认证](../agent-team/d26-authentication-verification.md)、[个人设置](../agent-team/personal-settings-verification.md)、[公开入口](../agent-team/public-account-entry-verification.md)验收。复用同一 Cookie owner、当前 Session/CSRF、明确身份切换、原 key 重试和草稿确认。 |
| D07 邀请与人工重试写服务 | **已满足**，B37 `022dcea`、终局 `0ed8085`，见 [D07 主卡](d07-account-session-smtp.md#b04-终局采纳与文档关闭)及[单跳 retry 契约](d07-account-mail-retry-addendum.md)。Create/Resend/Revoke/Retry 已有真实 HTTP、当前管理员、version、幂等和恢复能力；本卡不改这些服务。 |
| 邀请的最近投递任务读口 | **已满足**，[读口 rev1](d27-system-invitation-delivery-read.md)已独立 PASS、主线程采纳并提交推送 `9b3201547f9b7b61fd9716a6ba6540084961496c`，迁移前缀1–19正式接受。本卡固定消费该接受组合的 exact invitation 跨周期投影、有效期过滤、当前 attempt 渠道/结果、同事务权限及规模验证，不消费此前活动候选。 |
| 布局与组件 | [系统设置 §2](../../frontend-design/layouts/system-settings.md#2-用户与邀请)、[通用设置壳](../../frontend-design/layouts/settings-shell.md)、[交互样式](../../frontend-design/styles/README.md)、[公共组件](../frontend/components.md)。UiDialog/UiDrawer 遮罩关闭修复已接受 `b53895f`，包含在前端基线；不修改公共控件。 |

[账号生命周期](../../architecture/platform-infrastructure/authentication/account-lifecycle.md#3-管理员邀请)与 [SMTP 投递](../../architecture/platform-infrastructure/authentication/smtp-delivery.md#4-保存测试与重试)已确定业务含义：有效链接固定24小时，重发/重试不续期；创建相同有效邮箱复用原邀请；已注册邮箱拒绝；发送失败不回滚邀请；未知投递可能重复。本次未发现待用户决定的产品问题。Summary、Project/Artifact、Provider/tools 非前置；Object 原停止任务及 tools 原独立停止任务保持停止，不重建。本卡不改变完整 D26/D27、ready503、生产 SPA/Vite 真实代理的原边界。

## 2. 页面、操作与反馈

新增懒加载 `/system/invitations`，继承系统 authentication/protected/systemAdmin 元数据；系统设置菜单仅在原“用户”后增加“待注册邀请”。`/system` 仍重定向 `/system/users`，不记忆末次栏目。登录 return 闭集仅增加精确 `/system/invitations`；query/hash、数组、`/system`、未知叶子及外部 URL 仍拒绝。复用原系统导航权限条件和无权限区域，拒绝文案涵盖系统设置，不因列表失败造空页。普通用户直接链接不挂载邀请页、不发任何邀请读写请求。

SettingsShell 现有 `groups?: readonly {label,path,leaf}[]` 和每组唯一 RouterLink 是本卡需补齐的真实接缝。仅把 groups 元素扩为下列两种受限形状；旧调用、title/showLogout 默认及logout事件兼容，不引任意菜单slot、递归菜单或业务权限判断：

```ts
type SettingsGroup =
  | Readonly<{ label: string; path: string; leaf: string; key?: never; children?: never }>
  | Readonly<{
      key: string
      label: string
      children: readonly Readonly<{ label: string; path: string }>[]
      path?: never
      leaf?: never
    }>
```

新形状的key非空且同实例唯一，children非空、叶子path同实例唯一，由受信任的页面常量提供。内部将旧形状归一为一个叶子，旧组以path、新组以显式key形成互不冲突的稳定展开键，不能取首个叶子、文案或数组位置作组身份。SystemSettingsView传入一个 `key: 'users-invitations'` 的“用户与邀请”组，children按“用户”“待注册邀请”排序；只渲染一次组标题及一套共同展开/折叠控制，不复制同名分组或用CSS隐藏结构问题。

同组任一子项的精确route.path匹配均选中父组，仅该叶子带 `aria-current="page"`；组按钮只切换整个列表，不导航。`aria-controls`对应本实例稳定且唯一的列表id，不能以可重复的leaf文案拼接；不同shell实例不冲突。默认仍展开全部组，个人默认三组、三个原路径/叶子文案、侧栏退出和事件保持。groups允许通过props更新：同稳定键的展开/折叠状态及列表id保留，新增键默认展开，移除键清理展开状态；标题、顺序或children变化不重置其他组。选中态按当前props与路由重新计算，不缓存过期首叶子。保留原760px断点、叶子点击收起窄屏Drawer、Escape/遮罩关闭与触发焦点恢复，菜单仍以原生button/RouterLink支持键盘；这次不改变公共浮层或样式契约。

标题“待注册邀请”，操作区有“邀请用户”“刷新”“上一页”“下一页”；空列表显示“暂无待注册邀请”，保留真实创建入口。每行分开标注邮箱、创建时间（UTC）、到期时间（UTC）、最近一次投递任务及操作。任务区显示接受时间、当前阶段、尝试次数、真实尝试渠道及已有安全原因；若 `attempt_result` 有值，同时准确显示这次尝试的结果，不由 job 阶段覆盖它。完整 canonical 时间放入 `<time datetime>` 并可访问，展示可到秒，保留六位微秒原值。

| 操作 | 固定行为与成功含义 |
| --- | --- |
| 邀请用户 | UiDialog 仅输入邮箱，明确“发送邀请”，不输入密码、角色或期限。输入不偷偷 trim/改写；基础非空/长度校验及后端字段错误原位展示，最终邮箱格式与占用以现有服务为准。严格201只确认邀请/投递意图已接受，清空并关闭表单，显示“邀请请求已接受”，随后重新读取列表。不能称“邮件已送达”。 |
| 重发邀请 | 使用所选 InvitationID 与 invitation.version。可明确重发仍有效邀请，复用原链接及到期时间；后端仍执行每邀请60秒限流。不要从 latest_delivery.accepted_at 推算该限流，该值也可能属于人工 retry。202只确认新的重发意图。 |
| 重试投递 | 使用该行 latest_delivery.job_id 与其 job.version，不能传 invitation.version。这是新投递周期，沿原有效链接及当前合法配置。显示可能重复投递的说明，尤其不能把 unknown 解释为从未发送。202返回新 JobID及其当前 version，后者允许已大于1。 |
| 撤销邀请 | 必须先确认，明确邮箱与“链接将失效”；取消不发请求。确认后 POST 所选 InvitationID/version，严格204才显示“邀请已撤销”。不能提前移除行；失败保留原观察与安全错误，成功再刷新列表。 |

重发、重试与撤销各自使用点击时捕获的目标和版本，不因后台刷新自动换目标或版本。重试按钮的必要条件沿既有 retryEligible：enqueue_pending/pending/claimed/sending/retry_wait 不可重试；unknown 无已报告终局 attempt_result 时不可重试；有渠道而尚无终局结果时亦不声称已结束。cancelled 的合法无 attempt 记录不得仅因 channel=null 禁用；sent/failed 必须具有读口确认的合法真实 current attempt，旧 processing/unknown 的投影与例外严格沿读口契约。提供可见禁用原因；即使按钮可用，同 root 其他周期、当前材料有效性、version 和管理员资格仍由服务原子裁决，页面不授予重试权。未知投递的终局证据来自投影，不自行创造 io_joined。

阶段采用固定中文标签：等待入队、等待投递、已领取、投递中、等待自动重试、已完成投递、投递失败、结果未知、已取消。smtp 表示本次实际 SMTP 尝试，backend_log 表示后台恢复日志；null 显示“尚未记录尝试渠道”。特别是旧 unknown/null 不能写“从未发送”。sent 只表示 SMTP 接受或受限日志写入，不保证收件箱到达；配置改变不能改写历史渠道。backend_log 仅说明需由有权限的日志接收者协助转交，页面没有查看链接的入口。原因仅从读口闭集映射静态文案，不展示服务端自由文本或邮件正文。

成功反馈沿 UiButton 的2400ms规则并有可访问状态播报；**写确认与后续列表读取分开**。收到严格 receipt 后即保留确认，随后 GET 失败显示“操作已确认，列表读取失败”，只提供重读列表，不能重新进入该写的未确认状态。收到的 ID/version 不直接伪造列表行，也不覆盖后台更晚的任务。读取失败不能撤回已确认撤销或创建。

布局复用现有 SettingsShell/UiDrawer、UiDialog、UiField、UiInput、UiButton/UiMenu 和 tokens。长邮箱、时间及错误可换行；本页局部 `th,td` 覆盖全局 nowrap，列宽不能被邮箱占满。窄屏行按字段堆叠，操作可换行，无页面级横向溢出。创建/撤销/放弃确认的 Escape、遮罩、取消、关闭按钮均走同一业务关闭判断；有草稿或未确认提交时不得直接双向绑定 open 而绕过确认。正常关闭恢复触发焦点，行被删除时落到仍存在的创建/刷新控件或标题；保留键盘、层栈与 reduced-motion，不重写 useLayer。

## 3. 固定 API 与严格响应

新增 `web/src/api/system-invitations.ts`，导出只读 Invitation、InvitationDelivery、InvitationPage、InvitationQuery 与接口 `SystemInvitationAPI`。`SystemAccountAPI` 原 users 成员保持；只从 `system-account.ts` 明确导出已有严格 canonical Instant 解析器供两个目录复用，不放宽原八字段 User、九字段 SystemUser 或原日期校验。

| SystemInvitationAPI 方法 | 唯一路径、输入及成功 |
| --- | --- |
| `listInvitations({cursor?}, signal)` | GET `/api/v1/system/invitations?limit=25[&cursor=…]`，200 InvitationPage；无 body/CSRF/key。 |
| `createInvitation({email}, options)` | POST `/api/v1/system/invitations`，201 `{id,job_id,version}`。 |
| `resendInvitation({id,version}, options)` | POST `/api/v1/system/invitations/{id}/resend`，body仅 `{version}`，202 `{id,job_id,version}`；返回 id 必须与捕获目标一致。 |
| `revokeInvitation({id,version}, options)` | POST `/api/v1/system/invitations/{id}/revoke`，body仅 `{version}`，204实际空 body。 |
| `retryDelivery({job_id,version}, options)` | POST `/api/v1/system/mail-jobs/{job_id}/retry`，body仅 `{version}`，202 `{job_id,version}`；返回新 JobID，不是原 JobID。 |

options 沿现有 WriteOptions 由 useSession 提供 Session CSRF、稳定命令 key 和 AbortSignal。所有输入对象拒绝未知成员，UUIDv7、正 int64 十进制字符串版本先校验。邮箱输入保持原始值，可用 text + inputmode=email 避免原生 email 控件的隐式清理，前端不另造比既有 NormalizeEmail 更窄的业务语法；响应邮箱必须为 canonical ASCII 地址，不含显示名/首尾空白并满足既有长度约束。

复用 `accountTransport` 的同源 Cookie、no-store、redirect:error、600000B JSON上限、安全 Problem 与实际 body/cancel join。client.ts 仅增加上述五个固定 endpoint及它们的专用 options；动态段只能是预先校验的 UUIDv7，query 只允许 cursor并固定limit25，经 URLSearchParams 编码。不能提供任意 URL/method/header/query map 入口，旧 endpoint不能接新参数。撤销204须沿现有 readEmptyBody 核真实 EOF，不能忽略非空流或提前释放 owner；不顺手改变其他旧204接口。

列表严格对应[读口 §2](d27-system-invitation-delivery-read.md#2-专用投影与-http-契约)：外层恰 items及可选next_cursor；每行恰 id/email/version/created_at/expires_at/latest_delivery。投影 required/null/optional、phase/reason/attempt_result闭集逐项校验；attempts是0–6的十进制字符串，version是正int64字符串，channel仅smtp/backend_log/null。接受/创建/到期时间都用严格27字符UTC微秒与有效日历校验，不用宽松 Date.parse 归一化。拒绝缺投影、未知字段、非法null、数字代字符串或坏行，全部解析完成后才一次发布不可变列表，不兼容降级旧五字段成功。

一页0–25项，InvitationID不重复，按完整 `(created_at,id)` 严格递减；只保留原字符串，不截断微秒后排序。next_cursor若存在必须非空、≤8192字符且本页有25项；作为不透明签名值原样保存/编码，不解析权限、不自签、不放入路由。receipt均为闭合形状，只确认其命令，不把其中version冒充当前列表version。独立 MailJob GET 的渠道可来自配置兜底，本页不调用该接口补造历史渠道；也不抓取全局任务列表做邮箱关联。

请求、响应和异常均不进入console、URL/history.state、localStorage/sessionStorage；安全错误对象不持有 raw response/body、底层异常或完整命令。页面只展示白名单安全文本，邮箱作为文本渲染。列表其他身份不得进入 acceptUser、当前主题或个人草稿。

## 4. 同一 owner、写意图与身份恢复

`createSessionController` 增加第三个可选、默认真实的 SystemInvitationAPI 注入，保持前两个参数和原 mocks 调用兼容。在既有 system 下增加有类型的邀请读写/重试/取消能力及安全进度；不开放任意 privileged callback。仍只有当前唯一 owner，邀请 GET/POST、users GET、认证、个人设置和公开入口互斥，原30秒可见等待预算不变。可见超时、abort或明确放弃都不释放实际 owner，只有 fetch、body read/cancel 确实结束后才释放。

邀请操作在既有 owner 内标记用途并捕获自身代次；它与 users 读取的 abandon 必须各自限于自身操作。邀请读取消不能丢弃待确认写，users dispose不能取消邀请写，邀请清理不能取消 restore、改密或公开入口。可用私有 scope/代次完成，不建立第二队列或第二 Cookie store；不要全面重构原登录/个人/公开状态机。

每次读写均先核 authenticated、当前完整 identity（UserID+SessionID+epoch）、admin且未被system拒绝。只在 identity、请求代次、页面代次仍匹配时发布结果。新增写先完成输入校验，私有不可变 intent 保存命令种类、目标、完整原输入/version、key、原 Session CSRF及identity；页面只得安全进度，不持有key/CSRF。一次只保留一个邀请写意图，重复点击和新的邀请写不得越过在途或未确认意图，不自动排队、不自动换key重试。

失败分类及恢复：

- 本地校验/尚未派发失败、明确的 INVALID_ARGUMENT/字段错误、VERSION_CONFLICT、RESOURCE_DELETED、限流等既有业务拒绝，在没有更早未确认尝试且响应明确 not_started/not_committed 时按已知失败显示；保留输入或原观察，提供修正/重新加载后再执行，不把所有业务拒绝都称未确认。冲突不静默更新原expected version并重发。
- 已派发写的transport、超时/取消、非法响应、COMMIT_UNKNOWN、commit_state=unknown/committed但没有合法成功receipt，以及5xx/RESOURCE_BUSY，保留同一原intent为“请求结果未确认”。`RetryMailJob` 已提交后还会 GetMailJob 返回当前状态，后读失败可能是5xx/not_started；该字段不能证明原写未接受。后读返回version也可能已推进，不能硬断言为1。
- 原intent曾未确认时，后来一次非成功响应不自动消除历史不确定性。IDEMPOTENCY_KEY_REUSED提示标识/输入冲突并锁住原intent；不修改原body或偷偷换key。可重试原请求或明确放弃后重载。
- “检查当前状态”先沿原 restore 检查 Session，只有同一完整identity与原CSRF仍匹配且当前admin时才允许原请求重试；随后至多读取一页邀请现状。Session、列表出现/消失、任务进展都不是原command的receipt，观察后仍保留未确认标记，不循环扫描寻找“证明”。
- “重试原请求”使用相同kind/path/body/version/key和仍有效的原上下文。当前授权仍由服务重新检查；即使邀请已被兑换/撤销，合法same-key历史receipt也不被页面先以列表缺行挡掉。严格成功才确认原操作；重试不是新增重发或新retry周期。
- “放弃本次操作”明确确认“此前提交仍可能生效”；只停止页面追踪并销毁原intent，不声称取消服务端提交。仍保持实际owner至join，清除原草稿/目标后必须成功重新读取才可发起新的邀请写。无持久离线队列或跨刷新恢复key。

401/SESSION_REVOKED及写CSRF_FAILED沿当前身份失效纪律清除旧认证/邀请材料。当前identity的403 FORBIDDEN清列表、cursor、草稿和原intent，设置身份绑定的system拒绝，隐藏入口而不改User.role或自动注销；再次成功Session检查才可重建权限。晚到401对仍相同identity可能已清Cookie，仍处理失效；旧identity/旧代次403不得污染新账号。真实换账号、同User换Session/epoch、降权和注销销毁旧邀请状态；同一Session临时checking只隐藏，不丢弃草稿或原intent。

## 5. 页面生命周期、分页与取消

新增 `createSystemInvitations` 由 App 创建并provide，其草稿、确认与安全进度跨 RouterView 的checking卸载保留；Cookie/CSRF/key仍私有于useSession。页面进入/离开明确attach/detach，只在邀请叶子有效且当前身份确认后加载。App.dispose实际撤销本控制器watcher、beforeunload及反馈计时器，不能遗留后台轮询或请求。

创建邮箱草稿、进行中写和未确认写属于dirty。菜单、浏览器返回、跳其他系统叶子、顶部本人入口/退出、表单关闭/取消均先经“继续编辑 / 放弃修改”确认；确认须发生在router触发Session重验之前。App顶部退出组合既有个人草稿确认与邀请确认，不绕过任一现存owner。沿个人设置模式安装邀请navigation hook与App层确认Dialog；其余页面导航和公开入口确认保持。beforeunload按现有标准提示，有真实身份失效时立即销毁，不因dirty阻止清理。空白未提交表单和普通列表读取不额外要求放弃确认。

列表只提供固定25项的首项刷新、上一页、下一页，cursor历史只活在当前邀请页。下一页使用服务端cursor；上一页重新读取该页原输入cursor，后退后丢弃后续历史；请求成功才改变页位置。刷新、已确认写后的刷新、重新进入均从首项开始，不补抓所有页、不排序搜索、不伪造总数/固定快照。行到期/兑换/撤销由服务事实决定，不用浏览器时钟自行删除或续期；陈旧行操作被拒绝后明确重新加载。

普通GET开始隐藏旧行，保留这次目标cursor供明确重试，失败无部分候选、无自动重试。CURSOR_INVALID只提供回首项重新加载，不能偷偷改目标重读。等待其他owner时只可延迟一次尚未发出的初次加载。显式分页/刷新/新操作在owner未实际结束时禁用并说明等待；不建自动重发循环。分页和刷新不得让dirty表单/未确认意图悄悄消失；未确认状态的检查入口按§4保留原intent。新请求清单状态、写反馈和modal状态独立，读取错误不得改称写错误。

离开邀请页清列表/分页历史和目标，正常dirty离开经确认才销毁草稿；checking卸载不能冒充真正离页。页面和控制器各自递增有效代次，晚到列表、写成功和反馈计时器均不能复活已离开页面或旧身份。焦点更新只落到仍连接且可操作的元素，恢复等待真实owner释放后的可用控件。

## 6. 精确候选路径与所有权

以下22路径仅在上游读口产品接受、独立静审采纳及主线程明确移交后授予frontend_worker。当前architecture_worker只写本卡，无源码、测试、迁移或归档所有权；不修改正在实施的邀请后端八路径，不消费其活动副本作验收输入。

| # | 路径 | 限定用途 |
| --- | --- | --- |
| 1 | `web/src/api/client.ts` | 五个固定endpoint、受限ID/query options及撤销204真实空流校验，复用原transport。 |
| 2 | `web/src/api/system-account.ts` | 仅导出已验严格Instant解析器；users类型/请求/解析语义保持。 |
| 3 | `web/src/api/system-invitations.ts`（新） | 专用类型、严格输入/投影/receipt解析及固定API。 |
| 4 | `web/src/composables/useSession.ts` | 第三依赖参数、同owner邀请操作/私有intent、原请求重试及限域清理。 |
| 5 | `web/src/composables/useSystemInvitations.ts`（新） | App期草稿/未确认反馈、列表分页、确认和身份/页面生命周期。 |
| 6 | `web/src/App.vue` | provide/导航hook、邀请放弃Dialog、顶部退出确认及dispose；原个人/公开流程保持。 |
| 7 | `web/src/components/layout/SettingsShell.vue` | 兼容旧single-leaf与受限group key/children；归一化、展开键/props更新、选中及aria，原默认/窄屏保持。 |
| 8 | `web/src/router/index.ts` | 仅新增邀请叶子；系统默认用户页保持。 |
| 9 | `web/src/router/auth.ts` | 第六项精确return目标及邀请离页前确认/完成hook。 |
| 10 | `web/src/views/system/SystemSettingsView.vue` | 同一多叶子组提供用户及待注册邀请、通用系统权限拒绝文案。 |
| 11 | `web/src/views/system/SystemInvitationsView.vue`（新） | 列表、四操作、模态、状态及局部响应布局。 |
| 12 | `web/src/tests/system-invitations-client.spec.ts`（新） | 受限wire、严格DTO/receipt、时间/cursor/空流与输入反例。 |
| 13 | `web/src/tests/system-invitations-state.spec.ts`（新） | 真实controller+受控transport，实际join/身份/原key/错误分类/分页。 |
| 14 | `web/src/tests/system-invitations.spec.ts`（新） | 用户交互、真实router/controller、dirty/焦点/权限与App组合。 |
| 15 | `web/src/tests/settings-shell.spec.ts`（新） | 同组双叶子、折叠/选中/aria/键盘与props更新行为，旧single-leaf/个人默认及窄屏兼容。 |
| 16 | `web/src/tests/system-user-directory.spec.ts` | 原唯一叶子断言精确改为用户+待注册邀请；保留默认用户及其所有目录/权限断言。 |
| 17 | `web/src/tests/personal-settings.spec.ts` | 原五项return描述更新为六项并补新增合法项；原正反例/草稿断言保持。 |
| 18 | `tests/account/system_invitations_web_test.go`（新） | 下节真实顶层及数据库/command事实核对。 |
| 19 | `tests/account/system_invitations_web_fixture_test.go`（新） | 复用既有authenticationWebFixture及自有SMTP能力的窄准备/runner/响应丢失控制；不改旧fixture。 |
| 20 | `tests/account-captcha-web/system-invitations.config.js`（新） | 锁定Playwright、精确场景、独占私有输出。 |
| 21 | `tests/account-captcha-web/e2e/system-invitations.spec.ts`（新） | 冻结生产dist+真实后端的邀请行为/布局。 |
| 22 | `docs/development/frontend/README.md` | 接受后同步第二叶子、六项return、实际能力/命令与限制。 |

必读[Vue开发技能](../../../.agents/skills/agenteam-vue-development/SKILL.md)、[Vue测试技能](../../../.agents/skills/vue-testing-best-practices/SKILL.md)及相关组件/样式；验证负责人另读[验证技能](../../../.agents/skills/agenteam-verification/SKILL.md)，真实浏览器使用环境可用的Playwright技能，缺失如实记录。没有新增npm依赖。旧个人/公开controller、公共Ui/SystemNav、全局样式、后端生产源码/OpenAPI/迁移、旧浏览器fixture/driver、锁文件、当前归档路径均只读；SettingsShell仅授上述兼容补口，任何需扩大共享规则/路径的发现先交主线程修卡。

## 7. 验收与资源交接

先固定上游实际接受的后端提交/投影/OpenAPI/迁移前缀，再固定本卡22路径与dist输入。作者执行 `npm run check --prefix web`，覆盖原有及新增Vitest、格式、类型和生产构建；只格式化授权文件。新增Go fixture使用Go1.27.1、`GOTOOLCHAIN=local GOPROXY=off GOSUMDB=off GOFLAGS=-mod=readonly`进行integration-tag race编译及适用vet；网络依赖准备与正式检查分开，无锁漂移。纯测试与真实测试分别报告，不因读取规格或fixture编译声称行为通过。

纯测试须用明确屏障证明邀请读/写fetch、body/cancel忽略abort时，30秒可见结束后仍阻止users/登录/注销/改密/公开入口越过实际owner；只有原尾部实际结束才可继续。覆盖相反方向互斥、限域abandon、sameSession checking草稿保留、同User新Session及跨账号清理、当前/迟到401/403/CSRF、原key/body重放、retry后读5xx/not_started、明确业务拒绝与历史未确认区别、确认写后刷新失败、204非空流、版本不互换、closed/null/unknown投影、严格日期/分页、首次busy与无自动重试。测试实际生产controller+transport，不能只用立即resolve的API mock或响应clone证明实际尾部。

SettingsShell专用测试通过真实Router和用户可见交互核同组两个链接、一次折叠同时隐藏两项、组按钮不导航、两个精确叶子各自高亮且父组保持选中；两个shell实例的aria-controls对应各自唯一列表。以props替换验证同键折叠状态/id保留、新增组可见、删除组无残留菜单，不能仅断言内部Set。旧single-leaf调用、无props的个人三组/title/退出事件，以及窄屏叶子选择/键盘行为均为兼容门槛；真实焦点和遮罩仍由下述新Navigation及既有个人/用户目录浏览器组证明，不以jsdom模拟通过冒充浏览器验证。

| 新真实顶层 | 必需场景 |
| --- | --- |
| `TestAccountSystemInvitationsWebLifecycle` | 从已接受系统壳进入、真实创建邀请与邮箱错误/已注册拒绝；same有效邮箱/重发保持ID/token/到期，限流明确失败；撤销取消零POST、确认严格204及后端失效，实际兑换后刷新消失且用户目录可见。时间推进/限流准备若使用自有库精确fixture须标明，不把修改时钟列算真实等待。 |
| `TestAccountSystemInvitationsWebDeliveryRetry` | 正式邀请经自有受限日志显示backend_log；自有受控SMTP真实失败/终局unknown后，页面按真实投影与原job version人工retry，确认新JobID，改为成功后显示真实当前尝试；不延长链接、不自动日志降级。配置及协议基础可复用已验D07/读口证据，本卡至少补页面与真实投递/重试的一次组合。 |
| `TestAccountSystemInvitationsWebOutcomeRecovery` | 任务自有同源服务器在真实POST完成后有界丢弃/阻断返回，保留原失败证据；页面检查Session/列表仍未确认，显式同key重放后数据库只有同一接受事实。并测放弃后重载、严格成功后GET失败仍保留成功、真实版本竞争不静默改version重发。仅控制响应，不注入数据库commit故障或重建Object探针。 |
| `TestAccountSystemInvitationsWebReadAndPagination` | 至少26个有效邀请，每行由正式服务建立其accepted intent；逐页/返回/刷新核exact invitation和最新投影，投递推进不改变邀请分页顺序。空/到期/撤销/兑换边界与坏cursor、安全错误分别验证，不假装全局快照。纯规模播种不替代本组真实创建。 |
| `TestAccountSystemInvitationsWebAuthorityAndIdentity` | 普通用户无入口/direct-link不发邀请请求；实际撤销Session/当前admin失权由真实后端401/403拒绝读写，旧数据/草稿/intent清除；同账号新Session及明确换账号不继承原重试。失权准备限自有精确fixture，不能fulfill假403冒充真实授权。 |
| `TestAccountSystemInvitationsWebNavigationAndLayouts` | `/system`默认用户、邀请直链登录return、两个真实叶子；dirty表单与未确认写在菜单/顶部退出的继续/放弃、同Session复查保留；light/dark×1440/1024/834/390、长邮箱/状态、键盘、创建/撤销/放弃Dialog及窄屏Drawer的Escape/遮罩焦点、reduced-motion、无页面溢出。 |

真实新增顶层通过既有脚本按精确名称运行，可采用 `scripts/test-security.sh -run '^TestAccountSystemInvitationsWeb(Lifecycle|DeliveryRetry|OutcomeRecovery|ReadAndPagination|AuthorityAndIdentity|NavigationAndLayouts)$'`；只使用已验真实依赖与任务自有PG17.x（最低17.8）、MinIO、受控私网SMTP、受限日志和生产dist服务器。脚本/旧fixture原预算不变：浏览器45秒、Go顶层2分钟、workers=1/retries=0、race/count1/每包6分钟。按真实耗时分组运行，不加时、削断言、跳过缺资源或把no-tests算通过。浏览器日志/截图不得包含私有邀请链接或初始化密码，fixture材料仅留受限runtime。

共享App/router/owner的必要旧真实回归为 `TestAccountAuthenticationWebSessionLifecycle`、`TestAccountPersonalSettingsWebThemeAndNavigation`、`TestAccountPublicEntryWebIdentityNavigation`、`TestAccountSystemUserDirectoryWebAuthorityAndIdentity`、`TestAccountSystemUserDirectoryWebNavigationAndLayouts`；按实际固定差量复用其余旧证据。真实SMTP不可运行时明确保留本组未验证及阻塞，不能把合成投影/UI单测当成SMTP组合通过。没有生产托管、真实Vite代理或原生浏览器zoom的新接受声明。

独占资源由主线程在后端读口作者/独立验收实际结束清零后另行移交，本规格不占用窗口。保留原失败、固定输入、实际argv/env/退出/原始日志和安全数据库核对；命令实际wait、所属进程结束、自有exact-ID双次absent及旧基线不变后交回窗口。未参与实现的verification_worker独立审查权限、单owner/实际join和原请求恢复，并补真实浏览器/后端组合。主线程核关键证据后才采纳、同步文档并提交本完整页面；上游依赖接受或规格STATIC PASS均不代表本卡产品通过。
