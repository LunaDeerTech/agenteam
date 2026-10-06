# D07：系统邮件任务管理事实读口

状态：rev1.2，2026-10-06 **已独立静审通过（STATIC PASS），主线程已采纳规格**；被审全文 SHA256 `06cf9691217466c2245538dace4033d146ba033e5e9872a444e100445dea334d`，技术 §1–6 SHA256 `137e6a302a1f1a2b49076d6406ded7ff61a3c3ffccc939d4fb909640d8f176bc` 保持。窄测试修权与真实资源窗口由主线程另行授权。原 rev1.1 已独立静审通过并采纳，接受副本固定于 Git `ec5a3b9`（被审全文 SHA256 `9a966815ccbe5ecc41a0c0b8708824ca223b0398e22897424fed092c3a44d2ff`，原技术 §1–6 SHA256 `36b0a57fc4a06c6efa23600a572b83bba8072ab950c1997e7ef000d4319360c2`）。本次依据保留的 `new-attempt01` 首红及固定源码，仅修 §6 撤销先产生 cancelled、随后 worker 拒绝 ResourceBusy 的验收前提；§1–5、八条路径、写服务语义和预算不变。固定输入为 `3c79fd4069837c4cee0b1ed37e00480ea8f1909f`，未消费活动账号安全或共享焦点候选。本修订不新增产品或资源授权，既有实施及资源窗口以主线程授权为准；规格通过不代表实现、独立验收或后继 SMTP 页面接受。

## 1. 完整结果与已验前置

目标：为后继 SMTP 测试与投递管理页面提供当前管理员可分页读取的任务类型、真实 current attempt 渠道和终局结果。保持旧 MailJob HTTP 的闭合形状、字段含义及所有投递写语义；不新增发送能力、历史收件人、谱系浏览或“可重试”授权投影。无未决产品决定，本卡只补已存在的安全管理事实。

| 已验前置 | 本卡消费与边界 |
| --- | --- |
| D07 Account/System、真实 HTTP 根及投递服务 | [D07 终局](d07-account-session-smtp.md#b04-终局采纳与文档关闭)，B37 `022dcea`、关闭 `0ed8085`；真实 Session/admin、HTTP 错误与安全头、PG 事务、Outbox enqueue、SMTP/受限恢复日志和任务自有 fixture 均已有验收。 |
| D07 B03 人工重试 | `ffa65f0`，[单跳 origin 补遗 §2–3](d07-account-mail-retry-addendum.md#2-单跳-origin-事实与生命周期)；原 TestSMTP、RetryMailJob、current attempt/fence 和实际 I/O join 均为已接受写能力，本卡不改裁决。 |
| 邀请最近投递读口 | `9b32015`、[正式验收](../agent-team/system-invitation-delivery-read-verification.md)和[读口 §2–3](d27-system-invitation-delivery-read.md#2-专用投影与-http-契约)；复用已验证的严格尝试映射、旧 processing/unknown 例外及失败零候选纪律，不改变邀请最近接受排序。 |
| D03/D04 事务、cursor 与迁移 | 随上述能力组合接受；固定迁移1–19，只用既有表和索引，不登记新全局迁移、不回填历史。 |

页面职责来自[系统设置 §6](../../frontend-design/layouts/system-settings.md#6-安全审计与平台配置)和[SMTP 架构](../../architecture/platform-infrastructure/authentication/smtp-delivery.md)。[SMTP 配置首卡](d27-system-smtp-settings-ui.md)仅规格已接受、产品未接受，负责 GET/PUT/unconfigure；测试、任务列表/详情、显式重试页面另立后继完整结果，须等待配置页面与本读口实际接受。本后端不依赖该 UI、账号安全返修或焦点修复。Summary 待决、停止的 Object runtime join / tools 原任务、Runtime/Project 等未绑定范围保持，不能借本卡重建其探针。

固定 Git 的消费者核对已覆盖 `internal`、`api`、`web`、`tests`、`scripts`、`cmd`：`HTTPMailJob` 仅在 `http_facade.go` / `http_system.go` 形成旧管理读响应；`tests/account/http_admin_test.go` 和 `http_fixture_test.go` 消费旧 GET。生产前端及邀请 browser 仅消费 POST retry receipt，没有 MailJob GET parser；内部/真实投递测试另消费 `contract.MailJobStatus`。然而旧 OpenAPI `MailJob` / `MailJobList` 为 `additionalProperties:false`，不能据仓内无严格前端消费者便向旧对象追加 required 字段并称兼容。以下选择独立 GET 投影，旧 DTO/schema、旧列表 cursor、服务状态及写 receipt 原字节保留。

## 2. 专用 API 与闭合响应

| 新 operation | HTTP / 输入 | 成功响应 |
| --- | --- | --- |
| `listMailJobManagement` | `GET /api/v1/system/mail-jobs/management`；仅既有 `limit` / `cursor` 查询语法 | 200，`MailJobManagementList`：`{items: MailJobManagement[], next_cursor?: string}`。默认25、1–100；空页 `items:[]`，无下一页时省略 cursor，不输出 null/空串。 |
| `getMailJobManagement` | `GET /api/v1/system/mail-jobs/{id}/management`；canonical UUIDv7 JobID，无查询参数 | 200，单个 `MailJobManagement`；合法 ID 无对应 intent 才404，坏 ID 400。 |

两口只接受当前管理员、无请求 body，不要求 CSRF 写令牌或 Idempotency-Key；沿原 Session/Origin/security headers/Problem 边界。显式 GET-only，HEAD/其他 method 仍405及精确 `Allow: GET`；不增加全 Account GET→HEAD。集合 literal `management` 必须由 ServeMux 精确路由选中，不落旧 `{id}` 的 UUID 解析；详情末尾 `/management` 与旧 `/retry` 不重叠。禁止改成 `/mail-jobs/management/{id}` 造成与 `{id}/retry` 的交叉匹配。旧两 GET 和 POST retry 的 method、响应、授权及查询规则不变。

库层新增 `SystemHTTPFacade.ListMailJobManagement(ctx, actor, HTTPListRequest) (HTTPMailJobManagementList, error)` 与 `GetMailJobManagement(ctx, actor, contract.JobID) (HTTPMailJobManagement, error)`。新投影可组合既有 `HTTPMailJob` 与 `contract.DeliveryKind`、nullable 尝试字段；不扩 `account/contract`、不改 `HTTPMailJob` 或 `MailJobStatus`。wire 使用单个平坦、`additionalProperties:false` 的新 schema，不用封闭旧 schema 的 allOf 拼接。

| 字段 | 必需/空值与事实来源 |
| --- | --- |
| `job_id` | 必需非 null，intent 预分配 JobID；尚无 job 也已存在，不返回 intent/attempt/root ID。 |
| `kind` | 必需非 null，只取同一 intent 的 `invitation` / `password_reset` / `test`，不从渠道、邮箱或当前配置猜测。 |
| `phase` | 必需非 null，旧闭集 enqueue_pending / pending / claimed / sending / retry_wait / sent / failed / unknown / cancelled；内部旧 processing 明确映射 unknown。 |
| `attempts`、`version` | 必需非 null，canonical 十进制字符串；attempts 为0–6的持久 claim 计数，version 为正 int64。无 job 时分别 `"0"` / `"1"`；不当作实际发送次数或命令 receipt。 |
| `created_at` | 必需非 null，仍是 `delivery_intents.created_at`，canonical UTC 微秒 Instant；不是命令最终接受时间、job 入队时间或发送时间。明确处理 `foundation.NewInstant` 错误，不用忽略错误的 `instant`。 |
| `reason` | 唯一可省略的行字段；沿旧安全闭集 sent / token_invalid / configuration_invalid / policy_rejected / network_failed / smtp_rejected / timeout / cancelled / unknown。空值省略，不输出 null、空串或自由文本。 |
| `channel` | 必需非 null，**旧兼容摘要**：有 current attempt 时取其 smtp / log→backend_log；否则仅按当前 singleton 的 configured 选择 smtp / backend_log，与 enabled 无关。保留原含义，不能称作实际/历史尝试或下一次发送保证。 |
| `attempt_channel` | 必需、可 null，**实际渠道的唯一字段**；只来自 exact current attempt 的 smtp / log→backend_log。无可报告 attempt 为 null；不回退当前配置或旧尝试。 |
| `attempt_result` | 必需、可 null，只来自同一 current attempt 的 sent / failed / unknown / cancelled；无 attempt 或尚无终局结果为 null。不是 job phase，不从 io_joined 单独推断成功。 |

新对象九字段必需，`reason` 可省略；列表只允许 items 与可省略 next_cursor，maxItems100、cursor 最大8192字节。缺失 required、未知 enum、非法 null、非 canonical 标量均不是成功候选。OpenAPI 明写 `channel` 的兼容摘要性质；后继 UI 必须使用 `attempt_channel` / `attempt_result` 解释真实尝试，不把两个渠道字段并列成两个历史事实。

无 attempt 的新 enqueue_pending/pending 是尚未尝试；合法旧 processing/unknown 无 attempt 仍为“结果未知、渠道未记录”，不能推断从未发送。cancelled 可以合法无 attempt；sent/failed 必须有真实 attempt。retry_wait 可以保留上一尝试的 `attempt_result=unknown`，下一自动 claim 换 current attempt 后 result 回 null；不能据 retry_wait 称未发送。sent 只证明 SMTP 接受或受限日志持久写入，不保证收件箱到达。

## 3. 精确映射、事务、预算与分页

1. 新 HTTP 两 GET 从各自 dispatch 进入、预认证之前建立 `min(parent deadline, 开始+3s)`，只按精确 method/path 选择，不扩大通用 dispatcher 模式。直接调用新 facade 也有最长3s；继承更早 deadline，嵌套不得延长。预算涵盖当前授权、锁、SQL/扫描与响应编码前的完成检查，返回/写出候选前再次查取消；已开始写出的响应不承诺可撤回。沿现真实数据库调用与事务尾部 join，不用超时 goroutine 先返回仍在读取的候选。
2. 复用 `httpRead`，一次 AcquireAll 使用 account-mail SH 和现有当前 User SH，在同一 Tx 执行 `AuthorizeSystem(Read)` 后读取。HTTP 预授权、旧 cursor 和 JobID 不替代当前授权；不取历史发起者 Session、不扩大锁到全目录。共享 httpRead/Store/Authority 语义原字节保持。
3. 列表先以 intents 的 `(created_at DESC, job_id DESC)`、cursor 和 `limit+1` 建立 materialized page，再在**同一 SQL statement snapshot**左连 exact job/current attempt 及兼容摘要所需 configured。详情按 intent.job_id 唯一索引精确读取。同 statement 核按 intent_id 找到的 job 与按预分配 job_id 找到的 job 为同一映射；只能二者都不存在才 enqueue_pending，错配/缺件不能降格为空任务。不得因 INNER JOIN 或 WHERE 过滤坏映射、current pointer 缺失行；坏事实须被扫描拒绝。
4. 已存在 job 的 phase、attempts、version、reason、fence 均严校；无 job 必须匹配初始 phase/0次/版本1/空 reason/无 current attempt。current pointer 非空时必须解析 canonical ID，实际 attempt ID 等于 pointer、attempt.job_id 等于本 JobID、attempt.fence 等于 job.fence 且为正，attempts≥1。只读必要安全列及校验布尔/ID，不读邮件正文、recipient、link/token/verifier、Secret/lease、command MAC 或 root 链。
5. current pointer 为空时，attempt ID/job/fence/protocol/channel/result/terminal/joined 必须全部为空或对应零值；claimed/sending/retry_wait/sent/failed 不允许此形状，旧 processing/unknown 与 cancelled 的例外沿已验邀请 scanner。pointer 非空但行不存在不是 null。protocol 只接受 negotiation/auth/envelope/data/awaiting_acceptance/closed；result 非 null 必须同时 closed、terminal、io_joined，且在四种终局闭集内。result 为 null 时不得 closed 或 terminal；允许既有实际收尾过程的 joined=true、尚非终局形状，不擅自强化成 joined 必等于 terminal。之后才映射 processing→unknown，与 `httpScanInvitation` 已接受规则一致，不重构该旧函数。
6. 独立 signed cursor 绑定 System scope、`cursor.AuditOrder` 与 canonical `{"resource":"mail-job-management","filter":"all"}` 的 QueryDigest，位置仅 Instant + UUID，无 order_generation。复用 cursor 库和 facade 既有 ring，专用帮助函数放新文件，不放宽旧 `httpListBinding` allowlist。旧 mail-jobs/users/invitations cursor、篡改/超长/非法位置均拒；签发仅用最后一个已返回项的排序值。状态/配置/新 attempt 不改排序，limit 可沿旧语法调整；跨页不承诺全程数据库快照，新接受项可出现在已读边界之前，不自动扫描补齐。
7. 校验包括 `limit+1` 哨兵及实际 Close 后的 rows.Err。任何标量/映射/扫描/关闭/游标签发/权限/事务或预算错误，都返回零值新库结果，HTTP 只发安全 Problem、无 items/next_cursor/单项残留。事务 `Unknown` 沿 `resultError` 返回 Unknown，不能因只读回调拿到数据而改报成功，也不另启确认写或重试。缺失 singleton 造成所需兼容摘要无法确认时不得用 false/日志渠道兜底。

每页 Go 端至多101条安全候选；不 N+1、不循环读全部 jobs/attempts、不读取完整谱系。现有 job_id / intent_id 唯一索引及 attempt 主键支持 exact joins；列表排序沿旧事实，**没有**假称现有 intents 已有 `(created_at,job_id)` 复合页索引。materialized page 限制后续关联量，不保证底层历史排序常数成本；旧历史扫描受3s预算约束。无需新 DDL；若真实查询显示必须新增索引或维护列，先报主线程修范围，不能自行占迁移号或调高预算。

## 4. 观察边界与原写语义

读取只报告该 statement 的完整事实。并发 claim/finish/人工 retry 可以使随后观察不同；不混合旧 job 与新 attempt。配置改变可改变无 attempt 时旧 `channel` 摘要，却不能改写 `attempt_channel`；已走日志的历史任务在配置 SMTP 后仍有真实 backend_log。过期、消费、撤销链接不删除这些既有安全任务历史，本查询不读链接当前内容、续期、清理、取消、enqueue 或触发外部 I/O。

不新增 retry_eligible、terminal/join、recipient、root、config_version 或 attempt-history 字段。phase、attempt_result 只能提供必要观察条件：旧 unknown 无 attempt/非终局结果不能据此放行；cancelled/null 不可仅因渠道空便推断禁止。最终人工重试仍由现有 POST 在同 Tx 核当前 admin、source version、源周期终局与实际 join、同根其他周期及当前材料/SMTP 配置。已有最大版本拒绝、同 key 重放与并发裁决保持，本读口不扩大或收紧写资格。

`POST /system/smtp/test` 202 仍只有 `{job_id}`，不含 DraftVersion，也不承诺按页面旧配置发送；POST retry 202 仍为 `{job_id,version}`，是新周期的当前版本，可能已大于1。两条原写路径接受后仍可能因安全后读失败未能回成功响应；GET 当前状态不是命令 receipt，GET 的失败/not_started 也不能证明原 POST 未接受。Account 没有公共命令 lookup，本卡不补造。后继 UI 保留原 key/body/上下文，通过用户明确重试原 Execute 确认；未知投递可能已经发出且再次投递有重复风险。这里只冻结接口消费边界，不实现 UI 恢复控制器或自动轮询。

## 5. 唯一候选路径与实施门槛

以下八路径仅为候选。主线程采纳、指定唯一 backend_worker 并移交资源后才实施；当前只写本卡。新增类型/方法不引入通用可变注册口，也不扩大原服务 interface。

| # | 路径 | 限定用途 |
| --- | --- | --- |
| 1 | `internal/central/account/http.go` | 仅两个精确 GET 路由、新集合查询分类与其 dispatch 前局部预算接入；原分类/method/security 行为不变。 |
| 2 | `internal/central/account/http_mail_job_management.go`（新） | 新 HTTP DTO、handler、精确预算选择与写出前检查。 |
| 3 | `internal/central/account/http_mail_job_management_reads.go`（新） | 新 facade 类型/两方法、单 statement、专用 cursor、严格 scanner 与失败零值。 |
| 4 | `api/openapi/account.json` | 只增两 operation、MailJobManagement / MailJobManagementList 闭合 schema；旧 schema/operation 保持。 |
| 5 | `internal/central/account/http_mail_job_management_test.go`（新） | wire/schema、方法/路由优先、严格行/哨兵/关闭/取消、cursor 分域与局部预算纯测。 |
| 6 | `tests/account/http_mail_job_management_test.go`（新） | 正式 HTTP 根的当前权限、兼容形状、列表/详情/分页与错误；复用 newHTTPFixture。 |
| 7 | `tests/account/mail_job_management_query_test.go`（新） | 正式 facade/PG 同 Tx 权限、快照、锁等待/取消和实际查询成本；复用已验 Account fixture。 |
| 8 | `tests/accountmail/mail_job_management_test.go`（新） | 正式 intent/worker、受控 SMTP/日志的 kind、真实 attempt 状态及配置切换组合；复用已验 fixture/端口。 |

必读[Go 技能](../../../.agents/skills/agenteam-go-development/SKILL.md)、[验证技能](../../../.agents/skills/agenteam-verification/SKILL.md)、本卡与 §1 依据；局部文档沿[文档技能](../../../.agents/skills/agenteam-documentation/SKILL.md)。不写全局迁移、go.mod/go.sum、UI/SMTP 首卡、归档台账、旧 fixture/driver、旧 http_test.go 或投递/认证/配置/重试函数。既有 OpenAPI route 双向断言自动覆盖新表，无需改计数。任何缺少正式能力、需新路径/字段/写裁决或资源冲突先报告主线程。

## 6. 验收与冻结交付

| 验收组 | 本结果必须证明的内容 |
| --- | --- |
| 纯闭集/失败边界 | 新 schema 九 required/可省 reason、明确 nullable、所有 enum/ID/时间/计数与 exact job/attempt/fence；无 job、cancelled/null、旧 processing/unknown/null、活跃及终局组合。首/中/哨兵坏行和实际 Close/rows.Err/ctx 错误全零候选；Unknown 保留原分类。旧 MailJob/InvitationDelivery/JobAccepted/RetryAccepted schema 与 DTO 原形状不变。受控 Store/Authority 纯测试证明两个新 GET 从 HTTP 预认证起的本地3s或更早 parent 期限，以及取消后实际调用/事务尾部 join；不冒充真实 PG 锁等待结果。纯替身只证明映射/边界，旧 processing 例外不冒充本次真实 worker 状态。 |
| HTTP/路由/权限 | 两 literal/参数路径由真实 handler 分派；管理集合不当 UUID，非法/不存在详情分开；重复/未知/空/超长 query、body、非法 method/HEAD 和 Allow 正确，旧 retry POST 与旧 GET/HEAD 不变。真实 admin 成功、匿名/普通用户/撤销 Session 拒绝，旧 GET 仍只有原字段；安全 headers/Problem 不泄露私有字段或内部错误。 |
| PG/分页/预算 | 正式创建的不同 kind intents 支持空页、limit1、连续页与稳定 JobID/created_at；真实列表和详情对同项一致。只读 DB 原值作断言，时间并列可用纯 cursor 行测试，不直接改业务时间冒充生产顺序。真实 facade 同 Tx 当前授权另验；固定 fixture 的 `LOCK_TIMEOUT=1s` 不改，account-mail 锁等待验证 `min(parent, 本地3s, DB锁等待1s)`、typed 安全错误/零候选及实际退出，不保证 `errors.Is(err, context.DeadlineExceeded)`，不把 DB1s先到期说成真实本地3s自然到期。沿固定 `tests/account/invitation_delivery_query_test.go:590–629` 的原生错误与 caller 期限分辨，保留 Query→scan→Close→Tx 尾部。记录现有索引及实际 SQL 的正常/后续/空页 EXPLAIN，说明真实数据量和历史排序限制，不以小表计划声称无限规模 SLA。 |
| 实际尝试事实 | 通过正式 CreateInvitation、密码恢复请求/协调器、TestSMTP 创建三 kind；ReconcileDeliveryIntents 与真实 worker 生成 enqueue_pending/pending、SMTP/日志 sent、受控 SMTP failed、unknown 终局及 retry_wait/unknown，暂停受控 I/O 读取真实活跃 attempt，再实际 join。下一自动 claim 必须切换 current attempt/result；配置切换前后旧 actual channel 不漂移，无 attempt 的兼容 channel 可以变、attempt_channel 始终 null。对尚未 claim、无 attempt 的 pending 邀请 job，正式 RevokeInvitation 在同 Tx 先将其置 cancelled（reason=token_invalid），读取证明 attempt_channel/attempt_result 仍 null；随后 worker Claim 因已非 pending/retry_wait 拒绝 ResourceBusy，不新增 attempt/发送，任务状态保持。不能 SQL 改 phase/result/attempt/fence/terminal/join 来制造状态。 |
| 与既有写口组合 | 正式 retry 接受的新 job 保留 kind、初始实际渠道 null；same-key 返回同 JobID，源/新当前版本各自准确，读取不增加 intent/event/Audit/attempt 或发送次数。活跃/未终局或同根忙的原 retry 拒绝保持；只补本读口必要组合，重试原子性、TLS 三模式、出站/Secret/恢复/迁移等未变底层证据直接复用。 |

新的真实顶层固定为 `TestAccountHTTPMailJobManagement`、`TestAccountMailJobManagementReadTransactionAndBudget`、`TestAccountMailJobManagementAttemptFacts`；可在这些顶层内按事实分子例，使用现有任务自有 PG17.x（最低17.8）、MinIO/正式根及受控 SMTP fixture。状态必须经正式事务与投递端口产生；不直改 phase，也不借真实样本需求修改 production classifier、当前权限或重试规则。无真实收件箱/外部凭据，不新建发送 stub；固定未知结果风险与日志保密边界保持。

纯检查采用 Go1.27.1、`GOTOOLCHAIN=local GOPROXY=off GOSUMDB=off GOFLAGS=-mod=readonly`，依赖准备与正式检查分离；运行 `go test -count=1 ./internal/central/account/...` 及相应 race/vet、两受影响 integration 包编译/vet与 Central/Runner 构建。旧 `TestB04HTTPOpenAPIRoutesAgree` 必须原样通过。

真实资源只在主线程独占移交后启动，按现有 `scripts/test-security.sh -run` 选择上述三顶层，另跑 `TestAccountHTTPInvitationAndMailAdministration`、`TestAccountHTTPAdministratorPagesSettingsAndCurrentAuthority` 作为旧 HTTP 回归；按包核确实命中，不把 `[no tests to run]` 计通过。保留现有内部期限和 `-race -count=1 -timeout=6m`，按实际顶层分组，不为失败扩大期限或机械重跑全部 D07。独立验证者针对当前权限、精确尝试/未知和预算零候选补真实组合，未变底层证据可复用。

作者冻结八路径及最小已提交依赖，交精确输入 SHA、实际命令/退出/原日志、首红、索引/查询限制和资源 exact-ID 双次清零；真实调用、worker、父子进程与 fixture 实际 join 后才交回窗口。主线程独立采纳后提交本读口。后继 SMTP 测试/投递 UI、配置首卡及完整 D27 未随本卡完成；当前仅静态规格工作，未运行任何产品、PG/SMTP 或浏览器资源。
