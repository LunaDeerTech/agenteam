# D07 B03 人工邮件重试：Audit 与原始投递绑定补遗（已采纳规格）

- 固定输入：Git `c07ebcc`；有效设计修订6及已冻结 SMTP changed-field 范围补遗。本补遗已采纳为工程规格，不代表 B03 实现或测试通过。
- 归入设计 §1/§3/§4/§8/T10 的窄增量；已定发送、Secret、实际 join、日志资格点、共享停机预算保持不变。
- 结论：新增一个 typed Audit action、一个命令名和 `delivery_intents.origin_intent_id`；无新表、无 Secret 接口、无实际 mail attempt 冒用。
- 核对入口：[有效修订6副本](d07-account-session-smtp-design.md)、[固定00010](../../../db/migrations/00010_account_session_smtp.sql)、[固定事件规划](../../../internal/central/account/events.go)、[固定Audit授权](../../../internal/central/account/audit_authority.go)。

## 1. 已证缺口与正式请求

固定 `audit_authority.go` 的 Human 分支没有人工 retry 动作；`SMTPTestRequest` 只认 test intent，`SMTPDelivery` 要求 AccountMail 服务及真实 attempt，均不能借用。`00010` 的 command_name 没有 retry。多个独立 `reset-request` 可以重发同一个尚有效的 ResetID，`commands.resource_id` 不唯一；不能按 LinkID 任挑一个 command，也不能沿 retry 链无界回溯。

沿已规划 B03 `account/contract/delivery.go` 定义并由 `account.Service` 实现：

```go
type MailJobRetry struct {
    Actor identity.Actor
    Key foundation.IdempotencyKey
    JobID JobID
    ExpectedVersion foundation.Version
}
func (s *Service) RetryMailJob(ctx context.Context, r contract.MailJobRetry) (contract.MailJobStatus, error)
```

返回新周期的 JobID/status；已接受但尚未 enqueue 时为既定 `enqueue_pending`。只接受当前 Human admin；不向匿名、一般用户或服务开放。相同 key 返回同一个新 JobID，不能把一次人工请求复用为实际发送 attempt。

新 `mail-retry` command 持久绑定如下：

| 原字段 | retry 含义 |
| --- | --- |
| `id` | 新 IntentID，也是本次 Audit cause |
| `resource_id` | 被重试的原 JobID |
| `attempt_id` | 预分配的新 JobID；这是历史命令结果槽，不是 `mail_attempts.id` |
| `expected_version` | 请求中的原 Job version |
| `actor_kind/user_id/session_id` | 真实 Human 发起者及首次操作 Session，不能存 reset 目标用户冒充 Actor |
| `namespace/owner_id/command_key/semantic_kid/mac` | `account.mail-retry`／稳定 Human UserID／业务 key／既有 ACCOUNT_KEYRING 摘要 |

语义 HMAC 固定包含命令名、稳定发起 UserID、原 JobID、ExpectedVersion；不含 HTTP trace 或 SessionID。首次分配的新 ID 与解析出的 origin 不要求调用方知道。相同 key 异义拒绝；当前 Session/admin 授权仍先于 receipt 重放。

## 2. 单跳 origin 事实与生命周期

增加 `delivery_intents.origin_intent_id uuid NOT NULL`，不设永久 DEFAULT；所有 INSERT 显式赋值。原始 invitation/reset/test intent 令 `origin_intent_id=id`；人工 retry 直接复制来源 intent 的根 ID。根必须自指，retry 不能指向另一个非根 retry；不建立递归读取路径。

一次固定查询/计划收集：原 Job→来源 intent→根 intent→根 command（`root.id=command.id`）。锁后核以下不可变关系，任一映射不同整 Tx 回滚为 `RESOURCE_BUSY`，不是自动补锁或挑另一个 root：

- 来源和新 intent 的 kind、link 与根一致；test 的 link 必须 NULL，私有 recipient 从根逐字继承，不能在 retry 中换收件地址。新 intent 的 initiator 是本次 admin，根的 initiator 保留原事实。
- 根 command 必须 committed；invitation 对应 `invite-create`、根 JobID=`command.attempt_id`、LinkID=`command.resource_id`、Human initiator=`command.user_id`。
- reset 对应 `reset-request`，根 JobID/LinkID 同上；根 initiator=`command.browser_id`，真正 target UserID/password_version 取根 command 的已捕获值。获取材料/准入前另核当前 password_resets 的 exact UserID/password_version 与当前 User 密码版本；绝不取 retry command 的 admin UserID。
- test 对应原始 `smtp-test` Human 命令及预分配 JobID/initiator；其 command.resource_id 是真实 SMTP singleton，expected_version 是原配置版本，不当作 LinkID。重试用当前合法配置，不能改写根的原配置事实。
- 新 retry intent 与新 command 绑定 `intent.id=command.id`、`intent.job_id=command.attempt_id`、`command.resource_id=原 JobID`；根不得是 `mail-retry` command。原命令/根关系用于出处核实，不要求原发起者的旧 Session 仍有效。

原 invitation/reset 的所有固定 Git INSERT 都经过 `delivery_intent.go:insertDelivery`，只在该 helper 增加 self-origin 即可；不改 `invitation.go/reset.go`。新 test helper 同样显式写 self-origin。现 handler 使用显式列，新增列本身不破坏旧解码；B03 在现已授权 handler 中将 origin 映射加入 plan/current 重验，不能重新用 retry admin 作为 reset target。

根 intent、根 command、来源 intent/job 与 retry command receipt 在后继 intent/job/attempt/receipt 仍需引用时保留。B03 不增加其清除任务；清理 token/路由邮箱不等于删这些安全绑定。FK 不级联删后继；不借保留规则延长 token 有效期或保留可重发密文。恢复只读固定一跳，缺失/矛盾安全拒绝。

## 3. 同 Tx、规划、幂等与 Unknown

私有实现落在新 `account/delivery_retry.go`；复用现有 planned command→Tx 外 PrepareAppend→最终业务 Tx 结构。计划阶段可由原 Job/来源/root 推导未来新 intent，无须在 Discover 前虚构已提交新 intent。首次 command 持久预约 Unknown 先按原 command writer 锁确认，未确认不换 ID。

固定 `events.go:DiscoverAppend` 只带 retry command/admin/mail 锁，缺 root、原签发 command、link、reset target User。仅 delivery 分支增加委派，新私有 helper 提供：

```go
func (a *Authority) discoverRetryDelivery(ctx context.Context, actor identity.Actor, summary event.Summary) (outbox.Dependencies, error)
func (a *Authority) validateRetryDeliveryInTx(ctx context.Context, tx foundation.Tx, actor identity.Actor, summary event.Summary, deps outbox.Dependencies, stage outbox.Stage) error
```

Discover 在 Tx 外读取真实映射；完整绑定 Actor、command identity/HMAC、新 event header/payload、原/新 Job、来源/root intent、根 command、kind/link/target User/password_version。复制锁集合并入现有 command/System/User/Outbox 注册 SH/记录锁的一次 AcquireAll；包含原 Job、root/来源 intent、根 command、link 及 reset target User SH，新 intent/job 记录锁与现有容量锁。root intent 用 EX 串行化本来源周期；保留正式 provider 原要求的更强模式，不降级。完整绑定区分不变映射和本事务合法 phase/version 变化，不能在提交前将预期 version 自己更新后误判计划变化。

Validate 只 RequireHeld、重读和核 issuer/binding/当前权限；不 Discover、不补锁、不读外部网络。旧 session-revoked 与非 retry command 路径保持原规则；新 helper 从 retry 的真实原 job/root 推导 kind，不篡改 commandRecord 伪装成 reset-request/smtp-test。NewFact 阶段要求本 Tx 已 committed 的 retry command、新 intent 与真实出处完全一致，并重新核当前材料/配置。

最终唯一 Tx 顺序为当前 Session/admin→命令幂等→原 version/当前合法性→写新事实。首次须原 job 已结束本周期，当前 attempt 如有必须 terminal+io_joined；仍可自动 claim、正在 I/O 或终局未核实的 unknown 拒绝。按同 root EX 查询其他已接受周期：尚无 job 也算占用，pending/claimed/sending/retry_wait/processing 或未终局 unknown 均拒绝并行新周期；不能只检查当前 source 的内存 worker。

首次成功同 Tx：原 Job.version+1（溢出拒绝，保留原 phase/result/attempts）→新 intent→command committed receipt（result_version=新 job 初始版本1）→`account.delivery-requested` v1 与 typed Audit。实际 SQL 排列须满足正式 NewFact/provider 当前校验；事件 ID=新 command ID，payload 仍仅 `{intent_id,kind}`。不在此创建 `mail_attempts` 或做 SMTP/Secret I/O。现 handler/canonical 才收敛新 job，原 command/result 不能冒充已发送。

不同 key 同 ExpectedVersion 至多一成功；retry-of-retry 仍根锁下固定单跳。当前材料已消费/过期/普通改密失效时新请求拒绝；已完成 same-key receipt 只返回当时新 JobID 的当前安全状态，不续期、不重发，不要求旧 token 还可读。实际 claim 与 retry 也消费同来源串行条件；不能让旧周期在接受新周期后再次自动 claim。

Audit/Event/业务任一失败整 Tx 回滚。Commit Unknown 先在同 command/root/source writer 锁下核原 command、exact intent、原 Job version 推进与事件/Audit 的原提交事实；查询自身 Unknown 仍 Unknown。未确认不响应新成功、不新建第二周期；确认 committed 后沿原 receipt 返回，缺失/回滚才在原 ID 下重规划。无需通用结果仓库或第二个发送尝试。

## 4. 唯一新增 Audit 动作

`SMTPDeliveryRetry Action = "smtp.delivery.retry"`，加入 AccountAction；ProducerFor 现规则自然返回 `AccountProducer`，无需改通用 Producer 分派。唯一合法组合：System scope、当前 Human、Success、`MailJobResource(原 JobID)`；typed AccountMetadata 只允许并要求 `JobID=原 JobID`、`InitiatorID=当前 UserID`、`Version=请求原 version`、`Phase=AccountAccepted`。所有其他 ID、AttemptID、Channel、Reason、ChangedFields 均空；不含 recipient、host、token 或自由文本。

AppendKey 固定 `AccountProducer/causeRef=新 retry command ID/ordinal=0`，将同一请求的新旧两边连接。metadata.Version 是被接受的 expected version；最终 Tx 内原 job 当前值为 expected+1，后续变化不能改写这条摘要。不新增 metadata 新 JobID 字段：新 JobID 已由 exact command.attempt_id 及新 intent 持久绑定。

`account/audit_authority.go` Human switch 只新增当前 `AuthorizeSystem(Mutate)` 后的专用委派；新私有 `account/mail_retry_audit.go`：

```go
func (a *Authority) checkMailRetryAuditInTx(ctx context.Context, tx foundation.Tx, entry audit.Entry, key audit.AppendKey) error
```

该 helper RequireHeld 全部相关锁，按 key.CauseRef 读取 exact `mail-retry` committed command，核当前 Human UserID、首次新事实的 command Session、完整 HMAC/预留计划、原 JobID/expected version、新 IntentID/JobID/root 绑定及本 Tx 已写推进事实；不接受只因“某 admin 有一个 test intent”便授权。same-key业务重放不重 append、不拿后来 Session 改原事件；Audit 自身 receipt 核实仍当前授权。服务 Actor、其他 admin 的命令、改 metadata/resource/key/ordinal、planned/失败 command、缺 intent 全拒绝。旧 Human/Service actions、SMTPTestRequest、SMTPDelivery 的严格条件不改。

## 5. 00011 的确切追加范围

仍为尚待 B03 实施的事务型 Up-only `00011_account_mail_delivery.sql`；在 rev6 已定增量上仅追加以下内容，已提交 `00010` 字节不变：

1. 替换 `commands_command_name_check`：原允许集逐项保留，仅加 `mail-retry`。
2. 替换 `audit_records_action_check`：原允许集仅加 `smtp.delivery.retry`。替换 `audit_records_account_contract` 时保留旧表达式含义，仅 OR 一条 `scope='system' AND actor_kind='human' AND producer='account' AND action='smtp.delivery.retry' AND resource_kind='mail_job' AND resource_id IS NOT NULL` 分支；typed Go 校验承担 metadata/真实授权，不拓宽旧分支。producer/resource/Service 枚举不用改。
3. 给 `delivery_intents` 加暂可 NULL 的 `origin_intent_id uuid`，在同迁移事务中完成下列验证/回填，再加 NOT NULL、命名自表 FK（引用 id，ON DELETE RESTRICT，无 CASCADE）及 `(origin_intent_id,id)` 索引；无 DEFAULT。索引用于同 root 周期和保留事实的有界访问，不新增谱系查询服务。

10→11 回填只令已证明原始 intent 的 origin=id，不从当前 link、任意同 LinkID command、时间近似或 initiator 猜出处。每行须同 ID committed command、command.attempt_id=job_id、created_at 一致；invitation 要 invite-create/Human/initiator=user/link=resource；reset 要 reset-request/browser/initiator=browser/link=resource，并有非空捕获 target UserID、正 password_version。匹配用明确 IS NOT NULL/IS NOT DISTINCT FROM，不让 SQL NULL/UNKNOWN 放行。任何不合格行使整轮迁移失败并只给固定安全错误。

固定 c07ebcc 的正式 test 路径尚 unbound、无合法旧 test intent 创建入口；因此 10→11 遇不能由该基线解释的旧 test 行安全失败，不猜测新 B03 的 SMTP singleton/recipient 绑定。新 B03 test 在升级后经正式 helper 显式 self-origin。迁移也不要求当前 link 尚存在、原 Session 尚有效、当前 User 密码版本仍等于原值或 mail_job 已形成：被消费/到期的历史根、尚未 enqueue 的 committed intent 都是合法旧事实。邀请的 command 在插 intent 前同 Tx committed；reset 在公开接受时已 committed，不存在必须放宽 planned 的合法旧 intent。

所有旧行 ID/ref/phase/version/receipt 保留；无发送、无 token 延期、不补造 attempt。新增根/子 intent INSERT 由服务强制一跳自指根与 exact kind/link/recipient，SQL 自表 FK 只保证存在性，不伪称跨表 CHECK 可以授权。旧 handler 显式列及新 helper 的显式 origin 写入必须一起升级；不允许新代码漏列依赖 DEFAULT。

## 6. 最小授权范围与必须验收

| 文件 | 仅本补遗新增责任 |
| --- | --- |
| `audit/contract/account.go`、`account_test.go` | 一个 action、metadata/Entry 闭集、ProducerFor 与序列化反例测试；既有三项 SMTP changed-field 补遗保留 |
| `account/audit_authority.go`、新 `mail_retry_audit.go` 及相邻新测试 | Human admin 的 exact retry Audit 分派/私有校验；旧分支不放宽 |
| `account/events.go` | 仅 delivery retry 分支接新私有 origin 规划；session-revoked/原命令路径保持 |
| 已授权 `account/delivery_intent.go`、`delivery_handler.go` | root 显式 origin、retry event/current provider 绑定、handler/canonical origin 计划；邀请/reset原调用点不改 |
| 已规划新 `account/contract/delivery.go`、新 `account/delivery_retry.go`、必要新 origin helper/窄测试 | 上述请求/返回、同 Tx receipt、单跳 resolver/锁；复用新 B03 delivery/repository/usage 的正式组合 |
| 已规划新 `db/migrations/00011_account_mail_delivery.sql`、`tests/accountmail/` | 上述 CHECK/origin 增量、真实升级/迁移回滚和事务竞争验收 |

路径均相对 `internal/central/`，表内 `db/`、`tests/` 为仓库根路径。不改 `service.go/invitation.go/reset.go/recovery.go`、旧 SQL、Secret/Outbox 公共 contract、通用 Audit `types.go/metadata.go`；新动作通过现 AccountAction 分派生效并测试。若实现需要超出这些冻结旧文件，先报告具体缺口。

- **A01 闭集**：合法 retry metadata/Entry/producer 精确往返；Service、非 admin、失效 Session、伪原/新 Job、根链/跨kind/link、wrong cause/ordinal/Initiator/version、额外 AttemptID/channel/正文全拒；旧 SMTPTestRequest/SMTPDelivery 正反例不变。
- **A02 原子/重放**：首次产生唯一新 intent/event/Audit，源 version 仅+1，无实际 mail attempt；Audit/Outbox 故障全回滚；丢 COMMIT 回复先 canonical 确认，同 key 换当前合法 Session仍返回同新 job，异义拒绝，撤销当前权限不泄漏 receipt。
- **A03 竞争**：两 key 同 version、不同来源子 job 同 root、尚未enqueue、自动 claim/人工retry、token消费/到期/普通改密竞争；无并行新周期或迟到重新发送，Unknown 未证 join 不放行；完整 AcquireAll/低序缺锁与映射变化拒绝。
- **A04 根事实**：同 ResetID 两个独立 reset-request根、retry-of-retry 均精确一跳；当前 admin 与 target User 不同也取真正原 User/password_version；根旧 Session失效不伪授权也不破坏合法当前 retry；token已消费时旧receipt可当前授权重放，新请求拒绝。
- **A05 升级**：真实空库1→11、带00010 invitation/reset committed intent（含未enqueue、已消费link、后变password_version）10→11 保事实；缺 command/planned/错job/initiator/link/NULL目标、不可解释test行阻止迁移并整轮回滚；验证 origin NOT NULL/no-default/FK/index、旧CHECK允许集不回归、新action只接受限定SQL组合。
- **A06 恢复/计量**：handler/canonical形成唯一新job；首轮真实 attempt数和 latency 不被 command.attempt_id 伪增；同 root 原事实不被清理提前删；正文无邮箱/token日志。沿现完整 accountmail fixture，不改变发送/日志真实 join 与共享预算门槛。

本补遗只做静态可实施性设计；未运行产品测试、DB/Docker/网络或读取活动 B03 源码。当前无未决产品问题；具体 SQL 与真实升级/竞争结果必须由后续授权实现及独立 V 给证据。
