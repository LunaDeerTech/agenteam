# D27：待注册邀请的最近投递任务读口

状态：rev1，2026-10-06 已获 `recovery_documentation`（verification_worker）独立静审通过（STATIC PASS），主线程已采纳本规格与[系统设置布局 §2](../../frontend-design/layouts/system-settings.md#2-用户与邀请)的小澄清；被审稿 SHA256 `01a90463e8795b7d3de49a8a3d54ce51a085bead32eda25a37dcf84c7f46d063`。主线程已确定授权 `directory_backend` 唯一实施 §5 八路径，并独占全局迁移 `00019`；§4 规模计划及索引取舍规则保持。实际产品开工等待主线程正式下发，不由本次页首更新自行启动。本次仅更新页首与实施顺序，技术 §1–6 保持被审稿原字节；规格采纳与实施授权不代表本读口产品通过。

实施顺序：当前系统用户目录 UI 已获独立最终 PASS、主线程采纳，所属资源已实际清零并释放窗口；其20路径正在由主线程提交，本页不预填提交号。此前按已接受后端 `3affc01`、迁移前缀1–18完成 UI 验收的等待条件已满足。真实资源窗口现授予 `directory_backend`，开工后按本卡使用任务自有输入与资源；独立 verification 的资源窗口由主线程在作者冻结、实际结束与清理后另行移交。其他并行工作须固定已接受组合并由主线程安排输入和资源隔离，不自行切换验收基线。

## 1. 结果、基线与真实依赖

目标：当前管理员分页读取仍有效的待注册邀请时，同时得到该邀请**最近一次已接受投递任务**的当前状态与该任务当前尝试的真实渠道。页面无需遍历全局邮件任务，也不从邮箱、当前 SMTP 配置或最近成功记录猜关联。产品含义采用主线程已采纳的工程细化：最近任务按已接受事务事实选择；它可能尚未排入邮件队列、正在投递、等待自动重试或结果未知，不等于最近一次成功送达。本卡没有新增角色、邀请期限、重试或限流决定。

固定基线 `3affc0194214101cfa1e6fdc583afa5d60005db8`。实际 `HTTPInvitation`/`Invitation` 只有 id、email、version、created_at、expires_at；`MailJob` 虽有状态与渠道，却没有邀请关联。内部 `delivery_intents(kind,link_id)` 已保存稳定关联，`origin_intent_id` 只连接一个人工重试谱系。Create/Resend 每次产生新的 self-origin，故按单个 origin、收件邮箱或管理员 ID 查询都会漏选或错配。当前邀请列表也未排除到期但尚未清理的行，须落实[账号生命周期 §4](../../architecture/platform-infrastructure/authentication/account-lifecycle.md#4-邀请注册撤销与清理)的有效记录边界。

| 已验依赖 | 本卡消费与证据 |
| --- | --- |
| D07 Account/System 与真实根 | [D07 终局](d07-account-session-smtp.md#b04-终局采纳与文档关闭)，B37 `022dcea`、关闭 `0ed8085`；复用当前 Session/admin、邀请生命周期、正式投递意图、mail job/attempt、SMTP/受限日志及真实 fixture。 |
| D07 持久投递与人工重试 | [正式规格 §7–9](d07-account-session-smtp-design.md#7-初始化邀请和恢复)、[单跳 origin 补遗](d07-account-mail-retry-addendum.md#2-单跳-origin-事实与生命周期)，B03 `ffa65f0`；生产写路径已有最终事务完成时间和来源，不新增发送能力。 |
| D03/D04 事务、迁移与 cursor | 已随 D07 组合验收；复用 `httpRead`、当前授权及 invitations 的 signed cursor。迁移连续前缀 1–18 已接受，00018 证据见 [Usage 验收](../agent-team/invocation-usage-ledger-verification.md)。 |
| 系统用户目录补口 | 已接受并推送 `3affc01`，[独立结果](../agent-team/system-user-directory-read-verification-evidence/verification/verification-report.md.txt)；本卡保留其九字段、显式 users HEAD 与其他 Account 响应。 |

正在实施的[系统用户目录 UI](d27-system-user-directory-ui.md)不是依赖，不读其活动二十路径作为验收输入；本卡不提供页面。未验工具 wire、Anthropic、Summary 初值、Project/Artifact/新 Object 绑定均非前置；被筛查停止的 Object 原任务与 tools 独立原任务保持停止，不重建其探针。

## 2. 专用投影与 HTTP 契约

仅扩充 `SystemHTTPFacade.ListInvitations` 的列表结果和现有 `GET /api/v1/system/invitations`。外层仍为 `{items: [...], next_cursor?: string}`；每行保留原五字段，增加必需、非 null 的 `latest_delivery`。空列表为 `items: []`，没有 `next_cursor`。原 Create/Resend/Retry receipt、独立 `MailJob`、共享 User 及公开邀请检查/兑换形状保持。

`latest_delivery` 是闭合对象，字段如下；库层可用新的 `HTTPInvitationDelivery` 组合已有安全标量，不改 `account/contract`。

| 字段 | 事实与编码 |
| --- | --- |
| `job_id` | 被选 intent 的预分配 JobID；job 尚未建立时也存在。 |
| `accepted_at` | 对应 `commands.completed_at`，经 `foundation.NewInstant` 明确处理错误后输出 canonical UTC 微秒。它是最终接受事务中记录的完成时间，不承诺数据库提交序号或网络发送时间。 |
| `phase` | 与现有安全管理状态闭集相同：enqueue_pending / pending / claimed / sending / retry_wait / sent / failed / unknown / cancelled；旧 processing 仍投影 unknown。 |
| `attempts`、`version` | 当前 job 的尝试计数与版本，沿 foundation 十进制字符串；无 job 时分别为 `"0"`、`"1"`。attempts 是持久 claim 次数，不等于实际成功发送次数。 |
| `channel` | 必需、可为 null；只来自 `job.current_attempt_id` 对应的持久 attempt，smtp→smtp、log→backend_log。无 current attempt 时为 null，禁止用配置兜底。 |
| `attempt_result` | 必需、可为 null；只取同一 current attempt 的 result：sent / failed / unknown / cancelled。尚无终局结果或无 attempt 时为 null，不从 io_joined 或 phase=sending 推断结果。 |
| `reason` | 可省略；当前 job 的既有闭集安全 reason，沿现有管理读口校验。没有自由文本、SMTP 原始回复或凭据。 |

`attempt_result` 保留自动重试等待中的上一尝试未知结果：例如 phase=retry_wait、attempt_result=unknown，不能显示成明确未发送。新的自动 claim 换 current attempt 后，以该新尝试事实为准，不另造全历史摘要。无 attempt 的新任务显示尚未尝试；合法旧 processing/unknown 而无可报告 attempt 时，仍是结果未知、渠道未记录，不能据此断言从未发送。sent 只表示 SMTP 接受或受限日志持久写入，不保证收件箱到达。

实际尝试渠道与当前配置不同是合法历史。例如后台日志成功后配置 SMTP，该任务仍显示 backend_log；新任务尚未 claim 时 channel=null，claim 后才显示实际 smtp。配置失败不能当成未配置或自动切换日志。本卡不增加“下一次预计渠道”字段；后续若需要配置提示，必须清楚标为当前配置，不能覆盖实际尝试事实。

OpenAPI 只扩展当前列表专用 `Invitation`，追加 required `latest_delivery` 并新增闭合 `InvitationDelivery` schema；字段/null/enum 与实际 DTO 一致，不扩充 `MailJob` 或 `InvitationReceipt`。GET 的既有安全 headers、状态与分页参数保留。本卡没有新增 method；invitation HEAD 仍为原 405，users/avatar 的已验 HEAD 不变，不修改通用路由/认证分类。

## 3. 选择、事务与生命周期

1. 进入 `ListInvitations` 时建立 `min(parent deadline, 开始时间+3s)` 的局部读预算，覆盖本 facade 的事务内权限、锁、SQL 和扫描；不覆盖之前的 HTTP 预认证或之后的编码，也不修改共享 `httpRead`、Store 或其他 Account 读写预算。沿现有实际取消/事务结束规则回收；预算到期不是数据库调用已 join 的证明，不新增后台继续读取。
2. 复用 `httpRead`，一次 AcquireAll 包含 account-directory SH、account-mail SH 和已有当前 User SH，随后同 Tx `AuthorizeSystem(Read)`。HTTP 预授权、旧 cursor 或旧任务 ID 不能代替当前授权。邀请及投递投影用**同一 SQL statement snapshot**取得；不能先取列表再对每行调用另一个 facade/事务。
3. 先按 invitations 的 `(created_at DESC,id DESC)` 和已验 cursor 取至多 `limit+1` 行；本 statement 只捕获一次数据库 `clock_timestamp()` 作为截止时刻，条件为 `expires_at > cutoff`。不要在事务开始时捕获一个可能经过长锁等待的旧时间，也不在循环逐行取不同的时钟。继续默认25、最大100，下一 cursor 仍仅以邀请排序字段签发，投递状态变化不移动邀请分页边界。
4. 每行只从 `kind='invitation' AND link_id=该邀请ID` 的 intents 中选择最近已接受任务，跨全部 self-origin 与人工 retry 周期。`intent.id=command.id`，对应 command 必须 committed、`attempt_id=intent.job_id`；以该 command 的 `completed_at DESC, intent.job_id DESC` 稳定选择。不取 intent.created_at（复制 planned command 创建时间）、UUID 时间、job 创建时间、attempt 完成时间或 invitations.last_delivery_at（只服务 Create/Resend 的60秒限流）。
5. 同一 statement 将所选 intent 左连其 exact job、再连 current attempt；核 job.id/intent_id 与预分配映射，attempt 必须属于该 job 且 fence 等于 job 当前 fence。所选来源/root 的 invitation kind、link 与正式单跳关系须一致；仅固定一跳，不按 email 合并，不递归谱系，也不要求原发起者的历史 Session 仍有效。查询无 job 时得到 enqueue_pending；有效邀请没有可核实的首次已接受意图、所选映射缺失/矛盾或持久标量无效均503，不能伪造成功、把缺损称为空任务或回退到较旧成功记录。仅 SELECT 所需安全列，不读取 token/verifier/Secret材料、命令MAC、邮件正文或SMTP凭据，不触发外部投递。
6. 原始 ID/version/计数/phase/reason、邀请两个时间、accepted_at、channel/result 均严格校验；attempts 必须0–6，已记录 result 必须与 closed、terminal、io_joined 的正式终局事实相符，缺失指向的 current attempt 不能当成 null。时间显式调用 `NewInstant`，不复用忽略错误的 `instant`。校验覆盖 `limit+1` 哨兵行；任何扫描、rows.Err（含实际关闭后的错误）、事务结果或预算失败均返回空的库结果，不交付已积累 items/next_cursor。对当前允许的旧 processing/unknown 形状按上节明确投影，不用宽松默认值吞坏事实。

列表只读有效 live row。兑换/撤销事务先完成的邀请不再返回；读 statement 先建立 snapshot 的并发响应可以包含当时有效行，不承诺撤回已发响应。到期过滤不等后台清理，也不在 GET 删除记录、取消 jobs、释放 Secret 或补队列。相同邮箱到期后新建的是新 InvitationID，只关联新 ID；旧 intents/jobs 的安全历史继续由原机制保留。

Create/Resend 接受新 self-origin，人工 Retry 接受同根新 intent/job，自动 retry 在同 job 增加 attempt/fence；列表仅反映这些已有事实。不同 key/version、60秒限流、同根 active/未终局 unknown 拒绝、same-key receipt 重放、固定24小时和当前材料准入全部沿既有写服务。读取不授予重试权；job version 与 invitation version 分别保留，客户端不能互换。已开始 I/O 的旧周期可以晚于新周期结束，其完成不使旧任务重新成为“最近已接受任务”。

## 4. 查询成本与 00019 规划

已静态核对全量1–18迁移：intents 有 id 主键、job_id 唯一索引及 `(origin_intent_id,id)`；commands 有 id 主键/命令身份与 cleanup 索引；jobs 有 id 主键、intent_id 唯一索引；attempts 有 id 主键与 `(job_id,fence)` 唯一索引。现有索引没有按 invitation link 跨 root 查找的前缀，不能借 origin 索引声称已支持。原邀请页排序暂复用现状，不顺手优化其他管理列表。

00019 候选仅新增 `account_invitation_delivery_link`，位于 `agenteam_account.delivery_intents`，键为 `(link_id,id)`，固定 predicate `kind='invitation'`。不新增业务列、历史回填、触发器、最新任务缓存或 FK，不改1–18迁移字节。使用仓库正式事务型 Up 标记，幂等由 migrator journal 保证，不以 IF NOT EXISTS 掩盖不同定义；全局编号由主线程独占移交后实施。

实现先在任务自有 PostgreSQL17.x（最低17.8）记录现有 `pg_index` 实际定义，并用确定性规模 fixture 对**实际查询**执行 `ANALYZE` 与 `EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON)`，再比较00019后的同一数据/查询。规模至少含30个可分页邀请、100000个无关历史 intent 和单个邀请2048个不同接受时间的周期；这些直接播种只证明查询/迁移，不伪称真实邀请或发送。不得关闭 seqscan、强制 planner hint、减少数据或放宽预算以取得绿色结论。小表选择 Seq Scan 本身不是失败；规模证据须证明按 link 定位避免每个页面行全扫无关历史，并核正常页、后续页、无有效邀请、时间并列与倾斜单邀请。

`LIMIT 1` 只限制每个邀请的投影结果，不保证底层只读一条；completed_at 来自另一表，因此允许利用 link 索引取得本邀请周期、按 commands 主键取得接受时间后做 top-1。结果最多101邀请各一个任务，无 Go 端全历史 materialize、无 N+1 外部调用；该邀请自身历史扫描仍受局部3s取消预算约束，不宣称常数复杂度或无限规模 SLA。若真实规模计划证明原索引已经足够，先交主线程裁决省去00019并修卡；若建议额外索引、接受时间冗余列或写路径维护最新指针，同样先修订范围，不能自行扩展。

## 5. 精确候选路径与所有权

以下八条仅为后续实施候选；主线程采纳规格、明确实施者与资源窗口后才解冻。当前 architecture_worker 只写本卡及布局澄清，不写任何产品文件。

| 路径 | 限定用途 |
| --- | --- |
| `internal/central/account/http_facade.go` | 邀请专用投影、ListInvitations 单 statement/局部预算/扫描校验；共享 httpRead、users 和 mail-jobs 行为不变。 |
| `internal/central/account/http_system.go` | 邀请列表专用 DTO 与映射。 |
| `api/openapi/account.json` | Invitation/InvitationDelivery 及 GET 描述。 |
| `db/migrations/00019_account_invitation_delivery_read.sql`（新） | §4 一个候选 partial index；编号与落地条件由主线程控制。 |
| `internal/central/account/http_invitation_delivery_test.go`（新） | 严格投影/schema、坏行/哨兵/取消零候选、旧形状与路由兼容。 |
| `tests/account/http_invitation_delivery_test.go`（新） | 真实 HTTP/权限/列表/生命周期，复用 newHTTPFixture 及现有正式 Account 服务。 |
| `tests/account/invitation_delivery_query_test.go`（新） | 真实 facade/PG 的接受顺序、分页、并发与预算；00019 fresh/18升级/回滚及规模 EXPLAIN，复用现有迁移 helper。 |
| `tests/accountmail/invitation_directory_test.go`（新） | 真实受控 SMTP/受限日志的当前尝试渠道、等待/失败/unknown 与配置切换读口组合，复用已验 fixture/worker。 |

必读[Go 技能](../../../.agents/skills/agenteam-go-development/SKILL.md)、[验证技能](../../../.agents/skills/agenteam-verification/SKILL.md)、本卡与上列相关规则；局部文档遵循[文档技能](../../../.agents/skills/agenteam-documentation/SKILL.md)。不改现有 fixture/driver、锁文件、frontend二十路径、HTTP通用路由、账号写服务、mail worker/Secret/Outbound、旧测试断言。独立验收负责人只对冻结输入审查；任何需要新增路径、迁移变更、写语义或共享资源冲突先交主线程修卡。

## 6. 验收与交付

| 必需结果 | 可执行验证 |
| --- | --- |
| 真实列表与严格 wire | admin 创建有效邀请后，以数据库原值核五旧字段与最新 JobID/accepted_at；正式 facade 尚无 job 返回 enqueue_pending/null渠道，HTTP根正常推进后返回真实状态。空列表、limit/cursor、跨资源/篡改 cursor、相同邀请时间分页、状态改变不改变分页；原 User、InvitationReceipt、MailJob 形状及路由集合保持。 |
| 最近接受顺序 | 覆盖同邀请多 self-origin、原根及 retry-of-retry、不同邮箱/相同邮箱不同 ID、相同 completed_at 的 ID tie-break。用自有测试的受控事务顺序证明早 planned/晚接受者按 completed_at 胜出；旧任务晚完成不覆盖新任务。时间并列/历史量播种明确标为 fixture，不代替真实接受事务。 |
| 当前尝试事实 | 用实际 worker 与自有 SMTP/日志核 smtp、backend_log、未尝试 null、sent/failed/unknown、自动 retry_wait 保留 attempt_result、下一 claim 切换；配置改变不改旧 attempt channel，配置失败不被解释为日志。复用旧三种协议证据，只新增本投影需要的真实组合，不重做全部协议验收。 |
| 有效性与权限 | 未登录/撤销 Session、普通用户拒绝；事务内当前 admin 检查不能由预认证绕过。真实撤销、兑换后行消失；到期但清理未执行仍不返回。重发/人工 retry/消费并发只显示同一 statement 的完整旧或新事实，无半个 intent/job/attempt混合；不改变原限流、version与幂等结果。 |
| 边界与零候选 | 内部纯测试覆盖首行/中间/哨兵坏字段、非法时间/ID/计数/映射和 rows错误；真实 PG 对 owned坏时间、当前 attempt错绑定、锁等待及更早 parent取消核安全拒绝/零结果。局部预算不延期、不起新重试；Unknown 复用原 resultError 纪律，不新增 commit故障注入或 Object恢复探针。 |
| 迁移与成本 | 真PG fresh1–19、带账户/投递历史的18→19，核只新增精确索引且旧行/引用/版本/结果未改；自有迁移故障副本验证失败整体回滚、同字节恢复和journal事实。记录上述规模前后 EXPLAIN、行数/loops/buffers/时间及未达到的限制，不将纯SQL规模数据说成业务成功。 |

纯检查使用显式 Go1.27.1，`GOTOOLCHAIN=local GOPROXY=off GOSUMDB=off GOFLAGS=-mod=readonly`，缓存/临时目录归任务所有。执行 `go test -count=1 ./internal/central/account/...`、对应 race/vet，integration-tag 编译与 vet `./tests/account ./tests/accountmail`，以及 Central/Runner 构建。依赖网络准备与正式 readonly/off 检查分开，锁文件不漂移。

新真实顶层名称固定为 `TestAccountHTTPInvitationDeliveryDirectory`、`TestAccountInvitationDeliveryReadSelectionAndBudget`、`TestAccountInvitationDeliveryReadMigrationAndPlan`、`TestAccountMailInvitationDirectoryChannelsAndOutcomes`。主线程移交独占资源后，用既有 `scripts/test-security.sh -run '^(TestAccountHTTPInvitationDeliveryDirectory|TestAccountInvitationDeliveryReadSelectionAndBudget|TestAccountInvitationDeliveryReadMigrationAndPlan|TestAccountMailInvitationDirectoryChannelsAndOutcomes|TestAccountHTTPInvitationAndMailAdministration|TestAccountHTTPAdministratorPagesSettingsAndCurrentAuthority|TestAccountHTTPSystemUserDirectory)$'` 执行；保持原 `-race -count=1 -timeout=6m`、内部期限与nonce/exact-ID资源规则。按包真实列出命中结果，`[no tests to run]`不算通过。范围增长导致原预算不足时报告并按实际顶层分组，不加时或削断言。

作者冻结八路径及必要依赖，保留所有原失败、精确命令/env/退出与原始日志、EXPLAIN和迁移输入。验证者独立核最新接受排序、current attempt渠道/未知、有效邀请与当前权限，并在真实HTTP/PG/投递组合上补足风险；可复用未变上游证据。命令实际退出、自有资源exact-ID双次清零且基线不变后交回窗口。由主线程采纳并提交本完整读口；系统邀请页面、创建/重试/撤销UI和完整D27仍是后继结果。
