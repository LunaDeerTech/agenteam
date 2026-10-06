# D27：System SMTP 测试与投递任务 UI

状态：rev1，2026-10-06，规格已通过独立 STATIC 审查并由主线程采纳，待主线程另授实施及独占真实资源。规格接受不等于产品实现或真实验证通过。

固定产品基线：`628612cdfc730cc1d89cad4e24a5d20f36cc6812`。SMTP 配置 29 路径已由主线程接受并提交推送；基线包含管理读 `819aba1b8f764328f1e2e67b53c274fad0db877d`、邮箱闭包 `f670cb1`、Account 安全/共享 Dialog 与 `a942779` 的已接受出站 HTTP 接缝。不从原 `f670cb1` 动态证据推称整棵 `628612c` 已动态重跑。

本次只调整标题与页首，§1–7 技术正文逐字保留已审 rev0.2。原私稿全文 SHA256 `f34d86d5720824c18b9fb162fc980e93d7454e753d98238c1ef6c3f5bf6746ce`；不变技术 SHA256 `bee4f7ddb2b480b8c57b7d1b6b81be36bfce9f726222ebc10c8b468b90a54cda`（第一节标题至最终 LF）。正文中的“私有候选/拟归位”等阶段措辞以本页首为准，未授实施和资源的边界不变。

CSRF 边界：在同完整 identity（userID/sessionID/epoch）仍保留时，原 CSRF 即当前合法值；token 变化会更换 epoch 并销毁旧 intent。不得把新 token 移植到旧 intent 继续恢复。CSRF 是当前浏览器写资格，不是后端命令幂等身份的组成部分。

规格来源：[SMTP Delivery](../../architecture/platform-infrastructure/authentication/smtp-delivery.md)、[系统设置布局 §6](../../frontend-design/layouts/system-settings.md#6-安全审计与平台配置)、[管理读 rev1.2](d07-system-mail-job-management-reads.md)、[邮箱闭包 §2](d07-email-canonical-roundtrip.md#2-唯一规范化契约)、[SMTP 配置首卡](d27-system-smtp-settings-ui.md)。独立静审原件：[STATIC PASS 报告](/workspace/scratch/agenteam-smtp-delivery-ui-spec-review-x6vw4xfs/review.md)，SHA256 `1f0a5faca177f46a769615011a6758d2f3f3b74a7a8e6a5adb7ea8cf1c653b65`；同目录 `review.json` SHA256 `0bc66903f7adec96143518822c498fa9a640c709af89761563b98cb987d3fba1`。只证明规格和固定接缝，没有实现或动态结论，不预置未来归档链接。

## 1. 完整结果、缺口与接缝

管理员在现有 `/system/smtp` 使用指定测试收件邮箱显式请求一次测试，分页查看三种投递任务、打开精确 JobID 的安全详情，并按已报告事实申请新重试周期；原请求未确认时能检查当前状态或明确重放原请求。配置保存、测试请求接受、任务当前状态、实际 attempt 结果分别反馈。不新增菜单/路由/return，仍为 SMTP 首卡约定的七叶子、三组、十一 return。

没有需要用户另选的产品含义，也未发现必须再扩后端的事实缺口。必要限制：没有历史 recipient/root/谱系/链接/配置版本/attempt 列表，故不提供这些详情、过滤、搜索或跨表推断；没有 retry_eligible，故按钮只表示具备申请的必要条件，当前权限、同 root 忙、材料有效性仍由 POST 裁决。DTO 没有 recipient 不妨碍按 JobID 管理；当前测试草稿或本次确认旁的邮箱只标“本次请求收件人”，不冒充任何任务的历史字段。

同叶子内用“配置 / 测试与任务”两个区块切换，默认配置；本稿固定这一工程组织：切换先通过活跃区块的统一放弃门禁，成功切换才 detach 原区块并 attach 新区块，避免配置密码草稿与投递未确认 intent 同时编辑。配置的三个旧写读口及凭据语义不改变；测试明确使用服务端已保存配置，无 DraftVersion、无先保存再发送组合，也不保证使用页面刚观察的配置版本。

固定 `createSessionController` 前八参数依次为 Account、SystemAccount、Invitation、Provider、Model、ModelSelection、AccountSecurity、SMTPSettings API；末尾追加第九个可选 `SystemSMTPDeliveryAPI = createSystemSMTPDeliveryAPI()`，旧参数/工厂/mocks 兼容。当前 `auth.system.smtp` 是配置域，本卡另设 `auth.system.smtpDelivery`，不复用其私有 intent 或请求代次。`installSMTPSettingsNavigation` 已接受仅含 `confirmLeave/afterNavigation` 的结构对象，App 可装入一个 SMTP 分区协调器，无须改 router。

固定配置 controller 已有 `confirmLeave/attach/detach/afterNavigation/dispose`：`confirmLeave()` 经统一确认清草稿与配置 intent；`detach()` 只停止读取并保留安全观察；checking 时卸载不会销毁未决确认。它的现有接口足够，本卡不修改该 controller。SMTP View 现有配置表单/停用确认/统一放弃确认和当前本地 `fallbackFocus` 沿用；新投递 controller 与分区状态同样由 App 持有，不能放入随 checking 卸载的 View。后端、迁移、通用 Ui、生产发送适配器/出站分类均不扩围。

## 2. 四口与严格事实

| 本卡 API | 正式 wire / 成功语义 |
| --- | --- |
| `testSMTP({recipient}, options)` | POST `/api/v1/system/smtp/test`；body 恰 recipient；202 恰 `{job_id}`。只确认测试意图已接受，不含 version/applied_version/phase/配置快照。 |
| `listJobs({cursor?}, signal)` | GET `/api/v1/system/mail-jobs/management?limit=25[&cursor=…]`；200 `{items,next_cursor?}`，无 body/key/CSRF。 |
| `getJob(jobID, signal)` | GET `/api/v1/system/mail-jobs/{jobID}/management`；200 单项，精确 canonical UUIDv7，零 query。 |
| `retryJob({job_id,version}, options)` | POST 原 `/api/v1/system/mail-jobs/{job_id}/retry`，body 恰 `{version}`；202 恰 `{job_id,version}`，返回 JobID 必须不同于源 ID，version 是新周期当前正 int64，允许大于1，不要求等于源 version+1。 |

复用 `accountTransport` 原 `retrySystemDelivery` 固定传输分类，其余三个增加 `testSMTP/listMailJobManagement/getMailJobManagement` 窄 endpoint/options；不调用邀请 controller、旧 MailJob GET 或任意 URL。两 POST 使用私有 owner 的稳定 key、合法原 CSRF/identity 和 signal。原输入先闭合校验；recipient 复用已接受 `system-smtp-settings.ts` 私有 `email()` 的完整资格，新增窄 `captureSMTPEmail(value: unknown): string` 导出仅以既有 input 错误映射调用原 helper，返回输入原文。接受集合严格为邮箱卡 `S ∪ lower(S)`，含旧合法 `A@[IPv6:2001:DB8::7]` 和全小写 canonical，但拒 `A@[ipv6:…]`/混合标记等非集合输入；不先整体 lowercase 后校验，不 trim、不用 host/DNS parser，不复制第二份邮箱解析器。配置 parser/保存行为不改，原 body 一经捕获不因 canonical 等价改写。非法/空输入、双击不产生新意图。请求 JSON UTF-8 总预算仍16KiB，响应/Problem 仍600000B；不扩大 SMTP 配置32KiB或 Provider既有例外。复用实际 read/cancel join、no-store/redirect:error及安全 Problem。

管理行恰九 required：`job_id,kind,phase,attempts,version,channel,created_at,attempt_channel,attempt_result`，只 `reason` 可省；各枚举/0–6 canonical attempts/正 int64 version/27字符 UTC 微秒 Instant、null/optional 严格沿读卡，整页完整解析才一次发布。kind 为 invitation/password_reset/test。一页至多25条，按完整 `(created_at,job_id)` 严格递减且 ID 不重复；next_cursor 仅可省或非空≤8192 bytes且满25项，拒 null/空串/未知字段/坏行。cursor 不透明且仅留本身份内存，不解析权限或放入 URL。详情必须返回所请求 ID。`version` 输入捕获用于新 retry 时不得取 int64 最大值（源版本必须能推进），202 的新周期 version 允许整个正 int64 范围。复用已导出的 `parseSystemInstant`，不新增时间工具或宽松 JSON passthrough。

phase 闭集为 `enqueue_pending/pending/claimed/sending/retry_wait/sent/failed/unknown/cancelled`；reason 若出现只能是 `sent/token_invalid/configuration_invalid/policy_rejected/network_failed/smtp_rejected/timeout/cancelled/unknown`，不接受空串/null。实际渠道为 `smtp/backend_log/null`，实际结果为 `sent/failed/unknown/cancelled/null`。安全文案由这些闭集映射，不显示服务端原始异常。

`attempt_channel` 为唯一实际当前尝试渠道；有值时必须与 `channel` 一致且 attempts≥1，无值则 attempt_result 必为 null，不能出现在 claimed/sending/retry_wait/sent/failed；enqueue_pending 保持0次/版本1/无 reason/无 attempt。其余跨字段约束不比正式读口更强，不擅自令 phase 等于 attempt_result。`channel` 保留为兼容摘要，仅在详情独立标注“兼容渠道摘要：不证明已尝试或下次使用渠道”，不能替代实际列。首次 pending/0/null 只说明尚无已记录尝试；旧 unknown/null 只能说渠道未记录、结果未知，cancelled/null 也不虚构历史。attempt_result 非 null 由正式读口保证 exact current attempt 已 terminal 且 joined，前端不发明 join 布尔或历史 attempt ID。

## 3. 页面、分页与新重试申请

测试区只输入收件邮箱并显式“请求测试发送”；新请求需要同完整身份下配置 controller 已完成的 `observation.phase === 'ready' && observation.value.configured === true`。App 只向新 controller 提供这一只读安全可用性投影；不得传配置写 callback/私有密码、调用配置 controller 代发请求，或为新投递 API 增设第五口。配置观察为空/失败/身份变化时入口不可用，并提供返回配置区明确读取的动作；已配置为 false 则说明测试不可用。切换前清理保留的安全观察可供此门禁，不把其版本写进 SMTPTest body。这个入口条件不是版本 fence；并发停用/改配置以服务裁决。历史未确认重放不受当前 configured 或列表缺行阻挡。测试失败不删除配置；202 后立即保存安全确认，再最多一次 GET 返回 JobID 的管理详情，后读失败只标观察失败，不重发已确认 POST。不造未读的列表行、初始 version 或发送状态。

任务列表有显式刷新/上一页/下一页与精确详情入口，三 kind 一起展示、不自动扫全量。详情内联返回原列表及 cursor；列表与详情独立错误/取消状态，取消时同步把本次失效且无活读取的 loading 转为可显式重读状态，保留已确认事实，按固定已接受 controller 的同步退休规则实现。首次/新页失败不提交候选 cursor；换页/离开清对应旧读取代次；收到更旧的同任务观察不覆盖已确认较新 version。无自动轮询或后台重发。写确认旁提供显式重新读取，不从时间、kind 或本地邮箱扫描推断原任务。

新周期重试使用详情捕获的源 JobID/version，经明确确认说明“使用当前合法配置和原业务材料；可能重复投递”，sent 也只能说明曾获 SMTP 接受/日志持久写入，不代表禁止原服务允许的再次投递。必要条件：phase 属 sent/failed/cancelled/unknown、version 有合法后继；有实际渠道必须有终局 attempt_result；无实际渠道仅允许正式合法 cancelled/null，unknown/null 禁用。其余阶段给可见禁用原因。满足这些也不显示“保证可重试”：源链接已过期/撤销/使用、重置密码版本变化、同 root 其它周期、当前 SMTP 与授权等均未投影，必须让服务原子决定。

确认捕获目标/版本后不随刷新更换。严格202确认新周期接受，保留源 ID 与新 JobID 分开，再至多一次读取新详情；这是当前操作的本地关联，不宣称全历史 root/链。409冲突/410材料失效等明确拒绝保留观察、提供显式重读及重新确认，不改原 expected version 自动提交。配置变更不改写旧实际渠道；已配置失败不切后台日志，后台日志从无查看链接入口。状态/原因全用安全闭集文案；长 ID/时间/错误局部换行、窄屏堆叠，避免表格全局 nowrap 溢出。

## 4. 原请求恢复与两种 unknown

正式测试命令为 `account.smtp / smtp-test`，语义 MAC 包含 NormalizeEmail 后的 canonical recipient（客户端仍冻结原 body）；重试为 `account.mail-retry / mail-retry`，含 actor、源 JobID/version。两服务已提交或命中历史命令后均还调用 GetMailJob；该后读可失败，所以 POST 的404/5xx/RESOURCE_BUSY/transport/取消/超时/非法成功响应，甚至其 Problem 为 not_started/not_committed，也不能证明原写未接受。Account 没有公开命令 lookup，不借 Model lookup、Session、列表或详情充当 receipt。

每域仅一份私有不可变 intent：完整 identity/原合法 CSRF、操作、固定 path、原 body/version、key。新域的结果分类明确如下，不机械照搬配置 owner 的 NOT_FOUND 分支：

| 本次结果 | 本域处理 |
| --- | --- |
| 没有派发的本地校验/busy/取消 | 无本次网络写；不建立未确认状态，也不覆盖原有未确认意图。 |
| 严格 Problem，`commit_state` 为 `not_started/not_committed`、status<500，code 为 `INVALID_ARGUMENT/VERSION_CONFLICT/INVALID_STATE/RATE_LIMITED/RESOURCE_DELETED` 之一，且此前从未 unknown | 本次确定业务拒绝；允许明确修改/重读再发起新操作，不自动重试。它不声称数据库没有 planned 命令或所有副作用为零。 |
| 派发后 `NOT_FOUND`/404、任意5xx、`RESOURCE_BUSY/COMMIT_UNKNOWN`、unknown/committed Problem、transport/超时/取消/非法或截断成功响应，或不在上述确定集合的其它业务错误 | 保留原 intent 为未确认。尤其404可来自写后或历史 GetMailJob，不显示原写未接受。 |
| `IDEMPOTENCY_KEY_REUSED` | 未确认且锁住原意图，不替换 key/body；提示明确放弃后重新读取。 |
| 当前身份的401/失效CSRF或当前403 | 沿§5失效/失权路径销毁恢复资格与私有材料；不声称原请求回滚、未接受或邮件撤回。旧身份/旧代次按既有隔离规则。 |

未知状态具有粘性：曾未确认后即使收到确定集合的拒绝，也不抹除更早历史不确定性；只有匹配的严格202确认或明确放弃/身份失效可终止客户端追踪。测试无 JobID 时“检查当前状态”先 Session 重验，再至多一页管理观察；重试已知源 ID 时至多精确源详情。这些都不确认新 JobID 或旧命令是否提交。Session 已恢复为同完整身份且原 CSRF 仍合法后，即使随后管理 GET 失败，显式重放资格仍可成立；必须等待该 GET 的实际尾部 join，不能因观察失败永久堵住原请求。Session 恢复与随后管理读沿既有顺序 owner 协议；每次实际请求保留原30秒可见预算，不并发、不因 body chunk 或再次观察而重置同次预算。

“重试原请求”仅在同完整 identity/原 CSRF 仍合法且当前 admin 时，以同 method/path/key/body/version 再次 Execute；不能从当前草稿/详情构造新请求。即使现在 SMTP 已停用、源已撤销、当前 version 已推进，仍不预先堵住合法历史确认。严格202才确认原请求；retry receipt 的 version 可以再次推进，但同 key 已确认过的 JobID 不能被换成另一 ID。

“任务结果未知”是正式 job/attempt 事实；“请求结果未确认”是客户端尚无匹配202。二者独立：一个已确认的请求可以对应 unknown 任务；一个未确认请求不能靠看到某个 sent 任务转成功。用户明确“放弃追踪本次操作”只销毁客户端 intent，不声称取消/回滚发送；actual owner 尚未 join 仍忙。放弃后成功显式重读、重新操作才可新 key，无自动补偿或持久队列。普通收件草稿也仅存本身份内存；URL/storage/console/异常/报告不包含原 body/key/CSRF或恢复链接。

## 5. 同页协调、owner与确认

第九依赖提供独立 `smtp-delivery-read/write`、list/detail revision、私有 intent和安全进度；沿同一个 Cookie owner 与原30秒可见预算。新增 API 不直接 fetch，也不借配置域/邀请域的 owner 方法绕行；列表/详情/两 POST 均经过本域 runAuthorized 分支和实际尾部。与 SMTP settings 第八域及个人/users/邀请/Provider/Model/Selection/账号安全的 abandon 双向隔离；可见取消不释放实际 fetch/body/cancel尾部，晚到零发布。两 POST 纳入当前 identity/generation 的 CSRF_FAILED；401/同身份迟到401、当前403全系统私有销毁、失权/同User新Session/注销/全局 dispose沿已验纪律，不能只清本域而遗漏配置密码，也不能以普通页清理中止别的域。

App期保存活动区块、两个 controller和确认 Promise；任何时刻仅活跃区块可保有编辑/未确认意图及其一个统一放弃确认。协调器类型/注入 key 可置于本卡新的 `useSystemSMTPDelivery.ts`，App 实例化一次并作为唯一对象传给现有 `installSMTPSettingsNavigation`。其 `confirmLeave()` 只调用当时活跃 controller 的同名公开方法；`afterNavigation(to,from)` 通知两 controller，真正离开 SMTP 后清状态并把下次默认区块归为配置；App.logout 使用同一协调器门禁，dispose 清两 controller/待决切换。无新 router/meta/return/菜单。

区块切换捕获活动区块、代次和完整 identity，await 原 controller.confirmLeave 后重核当前性，成功才 detach 原区块、改变 active 并 attach 新区块；取消、失权、身份变化、真离开或 App.dispose 均使待决切换失败，旧续体不能激活新身份页面。连续点击不能堆叠多个放弃 Promise。仅 mounted 且 active 的 controller 可启动读取；旧域实际尾部未 join 时，新区块仅显示等待，不能越过 Cookie owner。切换不会自动发送/重放。inactive 配置 controller 不触发 get，允许保留其安全观察；检查恢复时仍重挂原 active，不以默认配置覆盖投递草稿/确认。

原配置 controller 保持字节不变；通过其现有公开方法组合，所有新门禁/active refs 在新 controller、App、View 范围内。默认配置区保留原配置表单、操作名和焦点行为，且不得提前挂载投递 Panel、读取 mail-jobs 或请求测试；旧配置 browser 的“零发送/零mail-jobs”断言仍全部适用。

协调器拥有独立的切换代次与 `viewAttached`，不借配置 controller 的内部 revision 作为成功切换判据（confirmLeave 合法 discard 会推进它）。View 的 mounted/unmounted 只通知该协调器附着/脱离当前 active controller；新 Panel 仅渲染，不再第二次 attach。同一生命周期的 attach/detach 恰由这一处负责，确认通过后的顺序必须可测试：旧 detach → active 变更 → 若 View 仍在且身份有效才新 attach；actual owner 未终局则新读取等待，不把 owner 设空。

| 事件 | 必须保留或结算的状态 |
| --- | --- |
| 用户确认切区 | 仅原 active 的 confirmLeave 成功才改变 active；失败/继续编辑保留原 active/草稿/确认。旧配置 input DOM 随分区退出清除；后台私有材料已由原 confirmLeave 清理。 |
| 同身份 checking 导致 View 卸载 | 仅 detach 当前 active 与使在途读代次失效，active/未确认 intent/两个层次的未决 Promise 不变；不得当成用户切区、调用 confirmLeave 或自动答 true/false。 |
| 503 后同身份显式恢复 | 重挂原 active，继续同一 App/controller Promise。配置侧保留其原已接受 resume 规则；投递侧不会将被取消读重新标为 loading。已明确取消/失效的投递 list/detail 显示独立“读取已取消，请明确重新读取”，只能显式操作重读；首次从未启动的 active 初读可等 owner 终局后启动一次。任何旧读晚到均零发布，所有情况零自动 POST/新key。 |
| 真离开/身份失效/当前失权/App dispose | 两 controller 按既有规则清理，协调器使待决切换失效；所有旧确认以 false 结算，旧导航/切区续体不能激活后来身份。普通分区切换的清理只作用于原 active，不扩大成全局清理。 |

投递读退休必须同步处理可见终态：list/detail 分别提升代次、取消本域对应读，并把该次 loading 改为 error/cancelled 可重读状态；已确认且独立保存的 receipt/更高版本事实保留，不用旧 async finally 修补 UI。停止可见 loading 不意味着 actual body/cancel 已 join，统一 busy 仍由原 owner 释放；不能为“可点重读”提前释放 Cookie 所有权。

统一由现有 SMTP View 末尾承载活跃区块确认：配置停用或投递重试确认在前，该区块统一放弃确认在后；App无常驻 Dialog，两个放弃确认不并存。物理两层时只有顶部可访问，下层仍 inert/aria-hidden；关闭顶部恢复到下层合法范围。checking共同卸载DOM、保留App状态/未决Promise；失败时App恢复可用，同Session恢复重挂原区块和顶部确认，零新 POST/key/自动答案。标题不抢开着的确认，fallbackFocus 在正常close读取最新本地ref，unmount/旧实例/新身份不借页面手动focus恢复。失权/真离开/dispose以false结算旧Promise，旧路由续体不得转移身份。复用首卡与已验账号安全共同确认，不改共享layer算法/旧退出协议。

## 6. 唯一候选范围（17路径，尚未授实施）

原18候选经固定接缝复核，去掉原#5配置 controller 和原#17旧 SMTP browser；新增配置 API 窄邮箱校验导出。主线程已确认这一规格范围调整，不等于实现授权。所有候选必须由同一前端作者独占，README最后。

| # | 路径 | 限定用途 |
| --- | --- | --- |
| 1 | `web/src/api/client.ts` | 三新固定 endpoint/options；复用旧 retry 路径，预算不扩。 |
| 2 | `web/src/api/system-smtp-settings.ts` | 仅增加 `captureSMTPEmail` 窄 export，调用原 email/input；配置及 parser 语义不变。 |
| 3 | `web/src/api/system-smtp-delivery.ts`（新） | 四API、closed DTO、输入捕获与两类202解析；复用现有邮箱/时间/标量。 |
| 4 | `web/src/composables/useSession.ts` | 第九依赖、独立域/intent、CSRF与全部system清理、实际owner尾部。 |
| 5 | `web/src/composables/useSystemSMTPDelivery.ts`（新） | 测试/列表/详情/原恢复、重试确认及窄分区协调类型/注入。 |
| 6 | `web/src/App.vue` | 两controller/单协调器provide、原SMTP guard与logout/dispose组合，不宿主Dialog。 |
| 7 | `web/src/views/system/SystemSMTPSettingsView.vue` | 同叶子两区块、仅active attach/detach、局部布局和末尾确认宿主。 |
| 8 | `web/src/views/system/SystemSMTPDeliveryPanel.vue`（新） | 收件输入、任务页/内联详情与安全反馈，不自行fetch/保存密钥或宿主统一确认。 |
| 9 | `web/src/tests/system-smtp-delivery-client.spec.ts`（新） | 四wire/严格解析/邮箱复用/编码及实际流尾部。 |
| 10 | `web/src/tests/system-smtp-delivery-state.spec.ts`（新） | 生产factory+受控transport的原请求/owner/代次/取消恢复。 |
| 11 | `web/src/tests/system-smtp-delivery.spec.ts`（新） | 真App/router/controller、同页切换与公开确认恢复。 |
| 12 | `web/src/tests/system-smtp-settings.spec.ts` | 新增默认配置与分区兼容/提供协调上下文；原配置/密码/确认强断言保留。 |
| 13 | `tests/account/system_smtp_delivery_web_test.go`（新） | 五个新真实顶层、正式事务旁证与脱敏断言。 |
| 14 | `tests/account/system_smtp_delivery_web_fixture_test.go`（新） | 正式根/自有受控SMTP与响应控制，旧fixture只读复用。 |
| 15 | `tests/account-captcha-web/system-smtp-delivery.config.js`（新） | 专用有界浏览器配置与私有输出。 |
| 16 | `tests/account-captcha-web/e2e/system-smtp-delivery.spec.ts`（新） | 生产dist、真实API/SMTP/事务组合与布局。 |
| 17 | `docs/development/frontend/README.md` | 产品独立接受后最后记录能力/证据/仍未交付范围。 |

配置 controller、旧 SMTP browser、router/auth、SystemSettingsView、旧菜单计数、邀请重试 API/controller、后端/OpenAPI/迁移/共享 SMTP fixture、Ui/SettingsShell/全局CSS、锁及归档全部只读。旧设置 browser 无需定位兼容修改，默认区块继续通过原断言；若实施遇确证卡外缺口，先交主线程裁决，不能借“兼容”默增路径。正式卡拟归位 `docs/development/work-items/d27-system-smtp-delivery-ui.md`，当前只交私有候选。

## 7. 验收重点与未授权资源

纯测以正式factory+受控 fetch/native body/cancel 分层：四闭集/坏行/分页/旧 retry 兼容；job-only202、retry版本>1/新ID、post-read失败not_started仍原intent；已确认receipt后GET失败不退回未确认；GET/Session非receipt；原key/body/CSRF重放、当前停用仍可历史确认、404后读未知、明确五码拒绝与先未知再拒绝；owner实际tail及与各既有域隔离（以配置密码/Provider两步/Model或个人操作代表双向实测，类型和清理表静核全域）、当前403全系统清理；取消list/detail立即离开无活读取loading并可显式重读。不得用账户 API即时mock代替真实owner证明。

真实App纯测：配置dirty切换、测试draft/未确认切换与离页的单门禁；重试/放弃确认已开→生产pageshow listener→Session503→同Session恢复→继续/放弃，强核可操作焦点/层序/零新POST；身份失效/待决导航false终点/旧实例隔离。jsdom证据与真实browser分列，合成pageshow不称BFCache。共享focus消费固定已接受行为，不在业务补全局querySelector或反复重挂。

拟五个独立真实组：`TestAccountSystemSMTPDeliveryWebReadAndPagination`（三kind/两渠道/空与连续页/精确详情/兼容摘要和实际字段）；`TestAccountSystemSMTPDeliveryWebTestAndRetry`（正式测试→真实SMTP接受/明确失败/结果未知；新retry周期与同root忙/版本冲突/材料失效原拒绝）；`TestAccountSystemSMTPDeliveryWebOutcomeRecovery`（两个POST分别真实接受后有界丢响应，同key/body重放各只一个命令/周期，推进后的version>1及当前停用/源状态变化）；`TestAccountSystemSMTPDeliveryWebAuthorityAndIdentity`（真实普通用户/撤销/精确失权、GET/新写/历史重放拒绝与同身份尾部）；`TestAccountSystemSMTPDeliveryWebNavigationAndLayouts`（配置/投递双域切换、checking503恢复、全部关闭渠道/焦点/窄屏）。实际每顶层2分钟、Playwright每test45秒、workers1/retries0、race/count1/每包6分钟；按耗时拆执行组，不能延时、跳强断言或算no-tests通过。导航布局固定 light/dark × 390/768/1024/1440 的8图，真实 pointer/Escape/Tab、reduced-motion 和 Drawer 复用已接受门槛；截图前私有凭据/恢复材料不在DOM且测试收件草稿已清空、物理overlay为0、main滚动定位可观察归零，核主区正常滚动与无外层异常空白，不以测试PASS代替逐图检查。

使用已接受邀请 UI `tests/account/system_invitations_web_fixture_test.go` 的正式 app.Run、受控 private SMTP socket与自有隔离库准备模式，复用 `tests/testsupport/smtp`；出站只允许任务自有已核nonce/exactID/IP/端口，沿原fixture规则校验/种子边界，不放宽生产分类或触外部邮箱。SMTP配置、测试/邀请/恢复请求、重试/撤销均走正式服务/HTTP，状态经真实worker产生；不SQL改phase/result/current_attempt/fence/join/version，不制造收件人或谱系。必要SQL只作任务自有状态/commands/Audit/attempt/链接不续期旁证及沿已接受fixture的精确准备，具体允许准备仅限任务自有初始化/用户角色或Session撤销、上述窄出站种子、受控响应503/接受后截断与安全事实读取；响应控制只发生在正式服务返回之后，不伪造202/JobID/version。使用固定 fixture 的登录/挑战和已接受账户准备 helper，不新增跨二进制存量前提。旧 SMTP settings fixture 本身禁止 delivery endpoint，不复用它的 deny-filter 根；新根采用邀请真实 SMTP 根的组合模式，原文件不改。取消无attempt样本遵管理读rev1.2：先正式Revoke得cancelled/null，再ClaimBusy且零新增，不复用原错误验收前提。

真实协议最小使用已验none模式SMTP成功/失败/未知与显式retry组合，三加密模式、Secret、出站/自动重试底层未变证据复用D07和管理读接受报告，不机械重跑全D07；不称本UI另验了所有TLS/崩溃恢复或收件箱送达。新的列表/详情所需旧processing例外留纯测试，不假造真实历史producer。SMTP/后台日志/body/凭据不进公开报告；双方只输出安全关联ID/计数/脱敏断言。两 POST 结果丢失的唯一性旁证必须关联原 command 的 namespace/name/actor/target 与对应 intent/event/Audit，而非只看全库增量；原 body/key/合法CSRF仅在私有内存比较并输出相等布尔，不打印值或把hash当通用可公开材料。observeNative 必须返回原 fetch/read Promise 和原 Response/reader/cancel，不额外读/clone；只累加已读分片并闭合安全DTO，所有done/error/超限清文本，响应600000B。成功DTO要求原生EOF，text/plain503媒体拒绝走cancel不能伪造EOF。trace/video/自动失败截图/原始网络body采集关闭；登录/私有fill错误不得回显材料。

当前前置已接受，但本稿仍无任何资源授权；候选/离线检查冻结并获主线程独占窗口移交后才运行。旧回归固定五顶层：`TestAccountSystemSMTPSettingsWebOutcomeRecovery`、`TestAccountSystemSMTPSettingsWebAuthorityAndIdentity`、`TestAccountSystemSMTPSettingsWebNavigationAndLayouts`、`TestAccountSystemInvitationsWebDeliveryRetry`、`TestAccountSystemInvitationsWebOutcomeRecovery`，全部原源/原断言。前端完整 `npm run check --prefix web` 及适用 browser type/list、native观察器契约、Go integration race仅编译/vet、Central/Runner构建先于真实开窗；纯资源不能冒已启动真实组。

以固定628612c+候选16测试源重建实际必要编译/运行闭包及所serve的dist，逐路径前后绑定；不混后继出站UI或主树文档闭包，不复制大型依赖索引。冻结driver/命令env/原始失败/安全raw，唯一Playwright绝对CLI和配置spec必须同一锁定解析。每轮原PG17+16/MinIO/SMTP资源按拓扑明确7或9 exact IDs，自有nonce/端口和非自有基线先记录；启动子进程前subreaper、按PID/starttime保留实际wait和adopted wait，root/server/browser/socket/lease/worker全部实际join后精确ID与owned进程两次清零才移窗。不碰旧容器/网络或历史PPID1 Z，不用外层超时强杀冒清零；FAIL先保原件并清理，再归因/按受影响范围另授返修。完整D26/D27、生产SPA、Runtime/ready503、Summary待决与Object/tools停止任务均不随本结果解决。
