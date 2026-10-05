# D09 Invocation / Usage 事务账本恢复卡

修订：rev1 已独立规格静审通过并获主线程采纳，接受提交已推送 `9aad5f5d26bf5af066390a8bd35b91d57d6b8c8b`；被审卡 SHA-256 `d53835cb79618b5cea487e8d84161cdc2e4f3a9900c6d35a86a258f75f795cab`，固定已验业务 `4295df7d51c1f171df78ab3f0d9cef2fd241a505` 不变。独立报告原定位 `/workspace/agenteam-invocation-ledger-spec-review-_836hbd1/review.md`、SHA-256 `1fd29dfd4e7c9892440c8d6abbcb074f9e5f9964ec6b7535d3ba744f32576de0`，现见[持久规格档案](../agent-team/invocation-usage-ledger-spec-verification.md)。主线程已授权 `recovery_handoff`（backend_worker）实施 §9 精确 20 路径，独立验收负责人 `restore_test_dependencies`；含唯一 `00018_model_invocation_usage.sql` 的实施权，R4 不占 00018，但业务和迁移尚未验收。规格作者 `d08_recovery_design`（architecture_worker）；除末尾记录的 PG 版本验收口径更正外，§1–10 保持被审原文，其中候选/待审/未授权表述为冻结时状态，当前行政状态以本段及末尾移交说明为准。不再委派。

必读 [设计技能](../../../.agents/skills/agenteam-design/SKILL.md)、[Go 技能](../../../.agents/skills/agenteam-go-development/SKILL.md)、[D09 工程规格 §5–8](d09-model-system-token-usage-design.md#5-调用reservation-与用量原子事实)、[Model / Usage 架构](../../architecture/platform-infrastructure/model-token-usage.md)、[Runtime 架构](../../architecture/platform-infrastructure/model-system/chat-model-runtime.md)及 [C0 Usage](../../../internal/central/usage/contract/types.go)。本卡精化一个完整库级结果，不复制 Runtime 的调用、恢复或产品规则。

## 1. 完整结果、前置与明确未交付

交付受信 Runtime 事实端口下的 Invocation 持久账本：reservation、发送状态/可靠用量观察、终态与 Execution 用量摘要同 Tx；精确历史回执确认、并发幂等和摘要重建；实际有数据的当前 Human Owner List / Aggregate / Execution 摘要读取。Execution 摘要仅是调用统计投影，**不是**未决 Project Summary 模型配置或内容摘要。

| 固定事实 | 可消费与限制 |
| --- | --- |
| current Resolution `4295df7` | 已验 current_selection 的 direct / platform.memory、不可变 snapshot/binding、planned Secret Acquire；[正式卡](recovery-d09-current-model-resolution.md)与 [00017](../../../db/migrations/00017_model_current_resolution.sql)可定位。它不是 call 许可，没有 Invocation 表，也不读取材料或发送请求 |
| Secret planned usage `8ad6759` | [正式 ReadCredentialForUsage](../../../internal/central/secret/contract/model.go)及同 Tx grant / AEAD / Audit 已验；Model 的生产 read / release 分派仍未绑定，不能把 Acquire witness 借给 read |
| text / structured wire | text `9c72190`、structured `be0bd07` 已验；[Exchange](../../../internal/central/model/adapter/transport.go)确有 Observe / Result / Next / Close / Joined，提供实际 D04 Decision 和规范 Usage。它没有本卡的 call 事实、账本或生产消费方 |
| Account / Project / D03 / cursor | 当前 Session、Project Owner Read、归档/删除 gate、同 Store Tx、全局锁序及签名 cursor 均可复用。System 管理员不拥有其他 Owner 的项目；Cursor 不授权限 |
| Usage C0 | [types.go](../../../internal/central/usage/contract/types.go)、[query.go](../../../internal/central/usage/contract/query.go)仅定义 carrier / Reader。保持现有类型及 Reader 两签名不变，不把 Validate 当授权 |

新 `InvocationFacts` 的生产 owner 是后继 **Model Runtime**。本卡允许用严格持久、同 Store 的测试 provider 隔离这个尚未绑定的 owner，和真实 PG / Account / Project / Model / Resolution / Secret / Audit / wire 组合验收账本职责；不得把该测试 provider 装入生产。没有 provider 时写入/技术确认明确 `DEPENDENCY_UNBOUND`，不能返回成功占位。

本卡不创建 logical call、输入原文仓库、call 许可、send gate、worker、重试调度、Process 注册/接管/死亡判定、跨进程 join、Secret lease release、Provider 调用编排、Audit 新动作、Outbox 事件、HTTP 或 root。它不提供工程稿的 LookupCall 或正文重放。真实 consumer、Model credential read/release、D08 OutboundService 委派、Runtime 唯一 writer 及其生命周期由后继完整结果绑定。已知 [Object join 缺陷](../agent-team/object-runtime-join-regression.md)与 Artifact 最终共享 guard 阻断保留；不恢复暂停任务、不造第二套 Process 死亡机制。Summary 初值仍待用户决定；`ready=false` / 503 不变。

## 2. 正式事实接口、DTO 与装配

以下是本卡新增公共口；`f`、`id`、`mc`、`uc` 分别指 foundation、identity/contract、model/contract、usage/contract。它们不改 C0 Model 或 Secret 的签名。工程稿 §5 的写入草图在本卡具体化为这些签名，返回 InTx 回执仍不代表提交。

```go
// usage/contract/invocations.go
type InvocationAction string // reserve | observe | finalize
type InvocationAccess string // apply | confirm
type InvocationIdentity struct {
    Attempt    mc.AttemptIdentity
    Consumer   mc.Consumer
    SnapshotID mc.SnapshotID
    Input      mc.InputIdentity
}
type InvocationRequest struct {
    Actor    id.Actor
    Access   InvocationAccess
    Action   InvocationAction
    Identity InvocationIdentity
    Sequence f.Sequence
}
type InvocationFact struct {
    Identity  InvocationIdentity
    Initiator id.Actor
    Sequence  f.Sequence
    Value     Invocation
}
type InvocationReceipt struct {
    Action   InvocationAction
    Sequence f.Sequence
    Digest   f.Digest
    Value    Invocation
}
type InvocationLookup struct {
    Observed bool
    Receipt  *InvocationReceipt
}
type ExecutionSummary struct {
    ProjectID   id.ProjectID
    ExecutionID id.ExecutionID
    Version     f.Version
    Summary     Summary
}

// usage/contract/authority.go
type InvocationFacts interface {
    Discover(context.Context, InvocationRequest) (InvocationDependencies, error)
    ValidateInTx(context.Context, f.Tx, InvocationRequest, InvocationDependencies) (InvocationFact, error)
}
type Writer interface {
    DiscoverInvocation(context.Context, InvocationRequest) (InvocationPlan, error)
    ReserveInvocationInTx(context.Context, f.Tx, InvocationRequest, InvocationPlan) (InvocationReceipt, error)
    ObserveInvocationInTx(context.Context, f.Tx, InvocationRequest, InvocationPlan) (InvocationReceipt, error)
    FinalizeInvocationInTx(context.Context, f.Tx, InvocationRequest, InvocationPlan) (InvocationReceipt, error)
    LookupInvocation(context.Context, InvocationRequest) (InvocationLookup, error)
}
```

`*usage.Service` 实现 Writer、原 Reader，并提供 `GetExecutionSummary(context.Context, id.Actor, id.ProjectID, id.ExecutionID) (uc.ExecutionSummary, error)` 和同参数/结果的 `RebuildExecutionSummary`。三个 InTx 方法只接受 `Access=apply` 和各自 Action；Lookup 只接受 `Access=confirm`，Action / Sequence 指原写入步骤，不是“读最近一次”。所有字段复制、Validate / Clone、Format / LogValue 的安全行为在新 contract 中实现。请求没有 Sent、token 数或 Joined 参数；调用方不能直接提交统计事实。

Identity 完整复用 `AttemptIdentity.Validate`、`Consumer.Validate`、`InputIdentity.ValidateFor`；Value 的 ID / CallID / ordinal / Process / fence / Consumer / Snapshot 必须逐项一致，`Invocation.Validate` 和安全历史 ModelIdentity 必须成立。Initiator 是 canonical call 的原发起者，只用于核稳定主体；它不凭自身结构获得准入。持久主体按已有规则投影：Human 保留 UserID、不保 Session；AgentRun 保留 Project/Agent/Execution；Service 保留名字、CauseRef 和 scope。当前完整 Actor（包括 Session）始终进入**本次** plan binding；新 Session 必须重新规划并当前授权，不能复用旧 plan。

Facts 是受信基础设施边界，不是 `mc.ConsumerAuthority` 的别名。Discover 仅返回候选/锁；ValidateInTx 必须由 provider 在其真实 Store、原 Tx、全锁下重新读取 canonical call / attempt / input / snapshot / observation，校验本次访问类别、完整请求、映射版本、不可变事实以及 §3 的当前准入或技术权限，再返回 Fact。端口不能仅核 Actor/owner 字符串、调用者 digest、技术角色或 DTO.Validate；也不能用 Project Owner 代替 consumer / input / attempt。调用输入摘要必须由 canonical owner 对实际输入重算，本账本只保存其受信摘要和 schema，不读取其他域私表或保存输入内容。

### 2.1 Opaque carrier

新增本包 `PlanIssuer`、`NewPlanIssuer`、`InvocationDependencies`、`InvocationPlan`。issuer 是实例内不可伪造身份；依赖与 plan 用私有闭包持有复制值，拒绝 JSON 反序列化，普通 JSON/格式化/log 只输出固定安全 marker。公开构造只建立结构，不授权限：

- `InvocationDependencyDetails{Binding, Mapping f.Digest; Locks []f.LockRequest}`；`NewInvocationDependencies(PlanIssuer, InvocationDependencyDetails) (InvocationDependencies,error)`，并有 `Validate`、`Details`、`RequiredLocks`、`Matches(PlanIssuer, f.Digest, f.Digest) bool`。Mapping 由 provider 绑定候选 canonical 事件的完整内容/版本，不公开历史 Fact；只有 provider 的锁后 Validate 才返回可消费投影。
- `InvocationPlanDetails{Binding, Mapping f.Digest; Locks []f.LockRequest; Facts InvocationDependencies; Cause f.TransactionCause}`；`NewInvocationPlan(PlanIssuer, InvocationPlanDetails) (InvocationPlan,error)`，同样有上述四方法，另有 `Cause() f.TransactionCause`。本服务只接受自己的 issuer；把任意自行签发的 plan 传入必须失败。
- `InvocationBinding(InvocationRequest) (f.Digest,error)` 绑定 Access / Action / Sequence、完整 Identity 与 ActorDetails。plan Mapping 分开绑定 provider mapping 和完整锁集；provider 的 mapping 必须覆盖候选 Fact 的 canonical 摘要。安全历史事实摘要排除临时 issuer、Session、事后可空 live FK、查询 AsOf；不得因配置变化改写已提交事实。
- 构造与 Details / RequiredLocks 均深复制 Consumer / Input / Invocation / Final / Usage / 指针/锁切片，规范排序、去重及 EX 覆盖 SH；plan 锁必须覆盖 provider 的全锁，不能删除不认识的锁。零 issuer / 零 plan / 跨实例 plan 不可用。

### 2.2 同 Store、无环构造

新 `usage.Store` 精确复用已验 Store 能力：嵌入 `postgres.SQLExecutor`，具有 `InTx(f.Tx) (postgres.SQLExecutor,error)`、`WithinTx(context.Context,f.TransactionCause,func(context.Context,f.Tx)error) f.CommitResult`、`AcquireAll` 和 `RequireHeldLocks`（后两者参数均为 `context.Context,f.Tx,[]f.LockRequest`，返回 error）。

新 `usage.ProjectAuthority` 精确为现有 `AuthorizeProject(context.Context,f.Tx,id.Actor,id.ProjectID,id.AccessIntent) (id.AccessGrant,error)`；`Authorizations{Sessions id.SessionAuthority; Projects ProjectAuthority; Invocations uc.InvocationFacts}`。固定构造 `NewAuthority(Store, Authorizations) (*Authority,error)`，`Dependencies{Cursors cursor.Keyring}`，`New(Store,*Authority,Dependencies) (*Service,error)`。Sessions / Projects 必须绑定；Invocations 真 nil 表示仅 Reader，可构造但所有 Writer 入口 Unbound；非 nil 的 typed-nil（包括具名 nil Chan）拒绝。Service 与 Authority 的 Store 必须同一可比较实例，零 Service / Authority 安全失败；opaque 外部 Facts 不反射内部 Store，必须在实际 InTx 验证中拒绝 foreign / expired Tx。

构造零 SQL、零角色注册、零 goroutine；依赖按值复制，不提供 setter / 全局注册表。`Initialize(context.Context) error` 只检查本卡三表和必需列可用，不造 calls、summary 默认行或 worker。装配方向是 Store → Account/Project 与未来 Runtime Facts → Usage Authority → Usage Service；Usage 生产只依赖其他域 contract、D03、cursor，不导入 Model/Secret/Project 实现或 adapter。后继 Runtime 消费 Usage，不要求 Usage 反持 Runtime Service，避免构造环。

`identity/contract` 窄增闭集 `ModelRuntime ServiceName = "model-runtime"` 及 RegisterService 识别；注册由未来受信组合者持有。它只是技术角色，不新增 System/Owner/业务权限，不改其他角色或 Actor / Scope 语义。Observe / Finalize / confirm 的该 Actor 必须 Project scope 精确相同且 CauseRef=InvocationID；注册成功、任意技术 Actor 或持有 Service 对象均不证明 invocation 存在或发出。

## 3. 事实所有权、当前准入与已发生事实终局

Runtime 是 call / attempt / 发送资格 / wire 观察的唯一 canonical owner；Usage 是本卡三表的唯一应用写者。调用方只有先取得可信事实再规划写账的能力，不能通过写账反向取得调用、credential read、D04、重试或新 fence 权限。生产 Facts provider 及消费者尚未绑定，必须明确交给后继 Runtime 卡。

| 操作 / 来源 | 必须在原 Tx 持锁复核的事实与权限 |
| --- | --- |
| reserve，Sequence=1 | 原完整 Actor 的当前 consumer/input/gate 准入、canonical call/snapshot/binding、exact 本次 Process/fence/ordinal 和先前 attempt 的真实终局。Fact 只能是 reserved、无 Final/DispatchedAt、UnknownUsage。该步骤不创建 call 或发送资格；原 reservation 未确认时不得因账本 callback 值调用 wire |
| observe → authorized | 当前原 Actor 及 consumer/input/gate；canonical owner 已持久确认的**原**发送资格。必须仍是本次未发送 attempt。账本只记录授权状态，不签发许可、不延长预算。不能以旧一次 reserve 授权绕过这里的当前检查 |
| observe → sent / 后续可靠 Usage | ModelRuntime exact 技术 Actor；provider 证明该 exact Exchange 的实际 `Observe().Decision.Sent=true`，持久安全观察序列和本地确认时间，可靠 Usage 来自已验 profile 的规范输出。可以发生在尚未 join 的合法流中，但不因此 Finalize / 退槽 |
| observe → not_sent | 同一技术 Actor和原 attempt；实际 Do / 本地 I/O 已终局且不可能再写，真实 Decision=false，或 canonical owner 已证实际从未交付 I/O 的终局。ctx 返回、deadline、空 Registry、缺 Decision 不能作为证明 |
| observe → dispatch unknown | owner 已有 canonical 授权但无法确定发送的持久技术事实。账本不推断死亡；后继真实 owner 需其 Process/原 writer/join 协议。本卡仅用严格持久协议 fixture 验这一状态的存取/统计，不声称验证过进程崩溃或网络未知窗口 |
| finalize | exact 技术 Actor、原 call/attempt/input/snapshot、同一 wire/local operation 的 actual join 和 canonical 安全结果；不得把 Close 返回、取消或槽已释放当 join。原 Human Session 撤销、项目归档/删除 gate 关闭不能抹掉已发生事实；provider 验原因果链和当前仍合法的技术终局权，不重新授新业务调用 |
| confirm | exact 技术 Actor 与原身份，持原 call/Invocation writer 屏障并核 canonical owner 的收尾/回执读取权限；允许确认旧已发生事实，不授新准入。不按“当前最新 invocation”替代请求中的 exact 身份/Sequence |

reserve 和 authorized 所用 Actor 的结构沿 C0 合法 Human / AgentRun / Service；provider 必须真的支持对应当前业务授权，否则 Unbound，不能泛化 allow。Observe 的 authorized 分支仍需原准入 Actor，其他发送/用量分支及 Finalize/confirm 使用上述 ModelRuntime。plan 须把候选状态和完整 Actor 一起冻结，不能在两类权限之间换 Fact 后借用旧 plan。

Facts provider 负责把观察与 exact Exchange 单一所有权连接：reservation/发送资格确认先于 Start；从该句柄实际读取 Decision / Usage；Finalize 前实际 Joined；观察序列、完整 attempt、snapshot revision、输入摘要和安全结果作为不可变 canonical 事件持久化。此处 join 指 wire 的 reader/transport/parser/material 使用或确定未交付 I/O 的本地阶段，不能要求包含本次数据库 finalizer 的整个 Runtime handle 先 joined 而形成环；完整 Runtime 终局还须等待账本最终 Tx 确认及其余真实尾部，属于后继职责。`InvocationFact` 是此端口的返回投影，绝不接受未经 Validate 的调用方 Fact。opaque dependencies 或公开 DTO 构造不是 wire witness。Usage 不验证供应商账号，也不以一段文本响应自行计算用量。

后继 Runtime 可在同一 Tx 写其 canonical 事件并写账，但必须事先完成完整规划，provider 的候选版本和锁后验证须支持这个明确的 staged 事件，不在账本里设置通用“正在写所以允许”开关。本卡正例可先固定测试 owner 的事件再应用账本；两个阶段间延迟是待确认状态，不是已发布消息/Usage。

## 4. 三表、身份与锁

唯一新迁移 `db/migrations/00018_model_invocation_usage.sql`，沿 `-- agenteam:transaction tx` / Goose Up。不改旧 SQL、Audit CHECK、Project/Secret/Execution 表或跨域 FK，不建 `model.calls` 占位。全部 UUID 复用 `agenteam_model.safe_id`，序号/版本 positive bigint，计数 nonnegative bigint，时间 timestamptz(6)，摘要固定 `sha256:<64 lowercase hex>`。

| 表 | 必需列、约束与保留规则 |
| --- | --- |
| `agenteam_model.invocations` | `id` PK；`call_id, attempt_index, project_id, snapshot_id, process_id, fence` 非空；安全 Consumer / Input / stable initiator / historical ModelIdentity 的私有规范 JSON 和 immutable header_digest；供筛选的 `consumer_kind,purpose,agent_id,execution_id,meeting_id,operation_id,historical_provider_id,historical_model_id` 与 JSON 一致；`live_provider_id,live_model_id` 可空；`dispatch,started_at,dispatched_at,last_sequence,updated_at`；可空 `final_status,terminal_version,finished_at,error_data,final_digest`；六个可空 token bigint、`usage_source`、受限 `provider_request_id`。UNIQUE(call_id,attempt_index)、UNIQUE(id,project_id)，同 call 至多一行 `finished_at IS NULL` 的部分唯一索引。首次 ordinal=1，后续连续且原 attempt 已终局由应用和 Facts 双重校验，不能靠这个索引证明 join |
| `agenteam_model.invocation_observations` | `(invocation_id,sequence)` PK、`project_id,action,fact_digest,receipt_data,recorded_at`；同域复合 FK `(invocation_id,project_id)` → invocations。保存每次已接受观察的安全原回执，以区分旧 sequence 的同义重放/异义冲突，不只存 latest digest。sequence positive，action 闭集，receipt 严格 format_version=1、最大 16 KiB；没有 endpoint、ref、材料、响应正文或原输入。随所属 Invocation 的未来正式删除可同域 CASCADE；当前无删除/TTL入口 |
| `agenteam_model.execution_usage_summaries` | `(project_id,execution_id)` PK、正 `version`、`updated_at`、ConfirmedInvocations / DispatchUnknown / 四终态计数、六字段的 nullable sum / known_count / unknown_count。每字段 known+unknown=confirmed，sum NULL 当且仅当 confirmed>0 且 known=0；confirmed=0 时 sum=0。终态和≤confirmed+dispatch_unknown；非负与 int64 上限受 DB / loader 双重约束。不含 Project Summary 默认模型、文本或 settings |

invocations 的 `(snapshot_id,project_id)` 同域 FK → 00017 snapshots `(id,project_id)`，`ON DELETE RESTRICT`；两个 live FK → providers/models `ON DELETE SET NULL`。历史 Provider/Model ID/name/protocol/profile/type/revision 永不随删除清空；不从 deleted live 表补历史名。call、Process、Agent、Execution、Meeting、operation、Project 均无跨域 FK，存在/归属/准入由正式 Facts / Project 端口证明；未来 call owner 不能把裸 CallID 或此表存在视为许可。正式 Project 永久删除后继须先删 invocation / observation / summary 再删 snapshot；本卡无清理权限，不把级联存在当成已绑定生命周期。

SQL CHECK 至少覆盖 C0 Consumer 分支与 nullable 组合、Input 的 execution/round 或 operation 分支、UUID/长度/枚举/JSON对象及版本、历史 ID 与安全投影一致、Live ID 若非 NULL 等于历史 ID、dispatch 与 dispatched_at 的 iff、非 sent 只能 UnknownUsage+六 NULL、ProviderUsage 至少一 known、时间顺序、终态组全空或 status/version/time/digest 全有、success 只许 sent 且无 error、not_sent error 不得 Dispatched=true。应用 loader 再完整严格反序列化并 Validate / 摘要 / 跨字段核对；未知字段、版本、坏事实返回安全 InternalError，不宽松补默认。

Consumer / Input / initiator / identity 每个 JSON 至多 16 KiB，error_data 至多 4 KiB，仅 `mc.ModelError` 安全字段；不会保存不定长正文。运行产生的原始结构必须先规范成私有标量 DTO，不能反序列化 C0 opaque Actor 来签发权限。标准 SQL `sum(bigint)` 以 numeric 中间值处理，转换到 C0 int64 前显式判溢出，不依赖 Go 加法回绕；CHECK 自身涉及多项和也转 numeric。

必需查询索引：`(project_id,started_at DESC,id DESC)`；同项目按 execution / agent / meeting / historical_provider / historical_model / purpose / consumer_kind / final_status 后接 started_at,id 的有界分页索引（nullable维度可 partial）；`(project_id,execution_id,id)` 供摘要扫描；observations 的主键供 exact receipt。无以 usage 数量动态排序或任意 SQL 标识符。§10 以实际数据和 EXPLAIN 核验，索引不是性能已验声明。

### 4.1 稳定写入与完整 union

本域 call 串行锁由 `NewCommandIdentity("model.usage", []string{ProjectID}, "call-ledger", CallID)` → `CommandLock` EX 生成，Action / Sequence / Session 不分裂此锁。plan.Cause 是该稳定 identity 的 `NewCommandsCause`；它只是本域事务因果，不是业务 call 记录或准入。Invocation 用 `RecordLock(CommandRecordLock,"model-invocation:"+InvocationID)` EX；Execution 摘要用 `RecordLock(ProjectionRecordLock,"model-usage:"+ProjectID+":"+ExecutionID)` EX，均满足既有 128 字节 key 限制。

每次写入与 confirm 的 union 包括上述本域 call / Invocation 锁、Project SH、适用摘要锁，以及 Facts 给出的**全部原 canonical call/attempt/consumer/input writer 和授权锁**；不能以新建本域锁冒充 Runtime 原 writer 屏障。reserve 涉及非空 live FK 时，Facts.Discover 的锁必须包含 ProviderAggregate / ModelConfigAggregate SH，锁后返回事实时账本另核这些锁确实 held；已存在回执的历史匹配不依赖 live FK 是否已空。完整 union 按 `CompareLockKeys` 一次排序去重后 AcquireAll，不在 InTx Discover / 开新事务 / 补更低锁。Discover 只返回 opaque 计划，不返回历史 Value 或回执；provider 不得在当前身份验证前以可区分资源错误泄漏 foreign call 的存在。

InTx 先验 Store/Tx 活性、本服务 issuer、完整 request binding、plan coverage；RequireHeldLocks 对全 union 生效，再 Facts.ValidateInTx。返回 Fact 的完整 identity/sequence、稳定发起者、候选 mapping / digest 和 action 必须匹配；任何规划后变化整体拒绝 `RESOURCE_BUSY` 并在 Tx 外重规划。所有账本及摘要写入只能在这些检查之后。header 已存在时逐项核完整语义，不能仅比哈希或 ID；同 call 其他 attempts 的 Consumer / Snapshot / Input / stable initiator 必须一致。新 attempt 还须 provider 证明前驱实际终局，数据库 finished_at 不自行授重试。

## 5. 状态、观察、回执与 Unknown

首次 reserve 建立 reserved 行、sequence=1 原回执；有 Execution 时同时建立版本1的零计数摘要。StartedAt 来自 canonical reservation 的持久 DB 时间。观察的 DispatchedAt 是实际 Decision=true 的本地首次确认时间，不声称 socket 首字节时间；FinishAt 是 canonical actual-join 之后的固定终局时间。再次重放不取新的时钟覆盖这些事实。

| 当前状态 | 允许应用的新事实 |
| --- | --- |
| reserved | authorized；或有真实本地终局证明的 not_sent。不能跳到 sent/成功来绕过原发送资格 |
| authorized | sent、not_sent 或 canonical dispatch unknown；授权本身不计真实调用 |
| sent | 同一首次 DispatchedAt 的后续可靠累计 Usage / 安全 request ID；或者在实际 join 后 Final。不能退回 authorized / not_sent / dispatch unknown |
| not_sent / dispatch unknown | 仅对应 terminal 或同义重放；dispatch unknown 不能凭后来的 DB COMMIT 确认改为 sent / not_sent。本轮没有事后重分类接口 |
| 已 terminal | 不可重写终态、用量、时间或原错误；相同最终语义可确认/重放，异义拒绝 |

Reserve / Observe / Finalize 都从锁后 Fact 生成新的规范快照，不应用调用者 delta。Observe 不含 Final；Finalize 必须保留先前发送事实并含 Final。Final Version 首次固定1；sent 可按真实安全结果取 C0 四终态，not_sent 只能 failed/cancelled，dispatch unknown 固定 terminal unknown；成功无 usage 仍可 succeeded + UnknownUsage。失败/取消不能把先前已知字段清成 NULL，不能以“最后没有 usage chunk”覆盖可靠用量。累计/增量合并属于已验 adapter/profile；账本接收完整规范累计值，不逐 chunk 盲加、不推算 Total、不把细分字段再加一次。

每个 Invocation 的 Sequence 是可信 owner 分配的正观察序列，first=1、后续新事实要求 last+1。相同 sequence 先当前技术/准入验证，再比保存的 action+完整安全事实摘要；同义返回原 receipt，异义 `IDEMPOTENCY_KEY_REUSED`。较旧且已有回执的 sequence 可同义重放，但不得倒写 latest；缺回执的旧号、跳号返回 `RESOURCE_BUSY`，不能假设“已处理”。Final digest 绑定完整 immutable header、发送/usage/安全error/status/version/固定时间，排除 Session、live FK、Sequence及存储时刻；同一最终语义的后续 Finalize 可存新 sequence 的同义回执，但不改变 final body/summary，非连续新号仍拒绝。任何异义 Finalize 冲突，不能拿更高 version 覆盖历史。

账本行、观察回执及执行摘要的 canonical 前后 delta 在**同一原 Tx**完成；任一步拒绝、约束错误、ctx 取消或 Audit 等外层组合失败均由外层实际结果决定是否提交，不得单独提交统计。InTx error 被恶意忽略也不得留下该入口的半写入：在写前完成可判定校验和完整回执构造，三表变化通过单条 data-modifying CTE 等原子 SQL 完成，约束/算术错误使该语句整体失败；不能先改行再用 Go 验证后续摘要，把“caller应该回滚”当成部分写入可接受。不得新增/修改 D03 通用事务语义；若已有 Store 无法支持所选写法，先报告具体接缝而非扩范围。

`InvocationReceipt` 只含安全状态/ID/历史identity/用量，返回后仍为 provisional。唯一成功边界是调用者持有实际外层 `CommitResult.State()==Committed`；`Unknown` 保留原 AttemptID/Cause，无 receipt 当作已确认成功，不发布 usage_update / message_end，不重发 Provider。DB Unknown 与 `DispatchUnknown` 是两个独立维度，绝不能互相赋值。

Lookup 内部 Discover(Access=confirm) → 自开有界 Tx → 一次完整 union → Validate → exact observation receipt。Observed=true 只表示当前真实读取到匹配原身份/Action/Sequence 的已提交回执，Value 是当时的安全历史投影，非“当前最新结果”或下一次准入；Observed=false 只表示本次未观察到该回执，不是 NotCommitted、不证明原 writer 终局、更不授重试或 no-send。因持原 writer 锁无法完成、授权失败或自身读 Tx Unknown，返回对应错误，不能改成 Observed=false。即使 Ledger 回执已确认，调用方也不能用它代替 Secret material read 或发送资格的独立确认。

Lookup 同义校验忽略事后 live FK 空化，安全 Value 只保历史身份；若原 receipt 存过 live ID，返回时与当前 invocation 的 live FK 投影一致，不把旧 pointer 当“资源仍存在”。原 bytes / fact digest 不被更改，明确区分历史回执与可变 live 链接。

## 6. 摘要、当前 Owner 查询与游标

只有 `dispatch=sent` 增加 ConfirmedInvocations 和六字段的 known/unknown 分母；dispatch unknown 单列。四 terminal 计数仅计 sent 或 dispatch unknown，遵守原 C0 `statuses <= confirmed+dispatch_unknown`；not_sent/reserved/authorized 可在 List 看到但不假计已发生调用。已知0保留0，全未知且有 confirmed 时 sum=NULL，没有 confirmed 时 sum=0，混合只累加已知项；拒绝任何 count/sum/version int64 溢出，零部分结果。

每次新 canonical event 持正式摘要 EX 锁计算前后贡献差；重复旧回执零 delta。没有 Execution 的 consumer 不造摘要行，仍进入正常 List / Aggregate。GetExecutionSummary 读取本项目真实摘要并核 C0 约束；没有该 Execution 的本域 Invocation 则 NotFound，不能把随机/他项目 ID 变成假存在的空执行。Rebuild 在同一摘要锁下从本域全部 canonical Invocation 重算，修复缺失或错误投影，与并发 Observe / Finalize 不丢增量；保持安全正版本，发生修正递增，内容相同不因重复重建改版本。首次摘要版本1，此后真正新 event 递增，重复 receipt 不变；无 token delta 的新 event 也可推进版本。Get/Rebuild 返回 Summary.AsOf 为本次真正统计/读取的 DB 时间；持久 updated_at 不是跨页 snapshot 承诺。

List / Aggregate / Get / Rebuild 只允许 Human，必须每次在同一 Tx 持 User SH + Project SH，调用真实 `RequireCurrentSession` 和 `AuthorizeProject(...,Read)`，并核返回 AccessGrant 精确 Matches。Rebuild 是技术统计投影重算，不更改业务输入、Project Touch或外域数据，但仍要求当前 Owner Read。System 管理员、其他 Owner、Service 或过期 Session 不豁免。归档可读，删除 gate 沿正式 Project 权限拒绝；先权限后返回可区分的资源/游标/无数据结果，不直接 SQL 查 Project/Account 私表。

原 Reader / Filter / Page / AggregatePage 字段保持：已有 C0 Limit=1..100，调用层缺省50，本服务不把 Limit=0 改成合法。From/To 均空时第一页取 DBNow 的最近30日 `[now-30d,now)`；显式范围按原值。所有页当前授权。过滤 Consumer / Agent / Execution / Meeting / purpose / 历史 Provider/Model / final status / started_at；当前配置已删也按历史 ID 统计，UTC 日分组。

Cursor 使用真实 `cursor.Keyring.Sign/Verify`；binding Scope=Project、digest=版本化规范请求（稳定 Human UserID、全部原始 Filter、list/aggregate、GroupBy与固定排序），不含 Session/limit/trace。默认时间范围的实际 From/To 放入已签名 Position，后页不能重新取“最近30日”。List 按 started_at DESC,id DESC，Position 至多6个标量：第一页上界 `(started_at,id)`、最后返回项 `(started_at,id)`、有效 From/To；空首结果没有 next。所有页都限制首上界并使用 strict keyset，不用 offset。第一页取同一 SQL snapshot 的最大符合行及 page，不允许先查上界后又用不同集合生成 page。

Aggregate GroupBy 只用 C0 八项闭集 switch。按规范 group key 升序、允许的 NULL group 独立且固定排最前；Position 在同一最大8标量限制内保存首 Invocation 上界、有效 From/To、上一组 NULL 标志和 key，binding 已含 GroupBy。限制原 Invocation 集合后计算每个完整组，再分页组；不得先 limit 明细后聚合、截断总数或只合计已取页。C0 AsOf 为**本页实际统计时间**且每个 item 相同。两种 cursor 固定的是首上界与筛选，不是跨事务 MVCC snapshot；并发观察可改变 usage/status，迟到提交的历史行也可能进入后页，不声称跨页强一致总数。

每次 List/Aggregate/Get/Rebuild 的整个调用（包括授权/锁/SQL/扫描）≤2s且服从更短 caller deadline；超时明确错误、零部分 Page/Summary。不得为大筛选静默丢数据、抬高预算或自动切样本。SQL 参数化；安全 projections 不含 endpoint、overwrite、credential/ref/lease、输入摘要/正文、response正文或原诊断 error。`Input` 与 stable initiator 是私有写账关联，原 C0 Invocation 对外并不增加这些字段。

## 7. 错误与取消

沿 [foundation.Code](../../../internal/central/foundation/fault.go)已有闭集，不新造字符串错码。坏 request、Action/Access/方法不配、Actor结构、zero ctx、非法cursor载体为 InvalidArgument；缺端口/零 Service/typed-nil 为 DependencyUnbound；非法实例/issuer/完整Actor或身份绑定为 Forbidden；权限/Session/Project gate 保留正式错误。签名/请求绑定/位置不合法的 cursor 为 CursorInvalid；schema/loader坏事实为 InternalError，未知受支持格式版本可 SchemaUnsupported。当前 plan/mapping/事实版本变化、跳观察号为 ResourceBusy；immutable header/同号/终态异义为 IdempotencyKeyReused；非法状态迁移为 InvalidState。锁/DB错误保留可 errors.Is/As 的原诊断 cause，公共格式化不泄漏 SQL/输入。溢出用 InvalidState 明确失败，不回绕为0或改 NULL。

Discovery / 外层主动调用被取消时停止新的规划/写入，InTx 在适用校验边界复核 ctx；取消不是发送或终局证据。已经发生的技术收尾可由 Runtime 提供新的有界收尾 ctx 和 exact 技术 Actor重新规划，但本卡不自行 background 重试或 `context.Background()`逃逸。Store callback 的错误及真实底层 commit state 如实传递；普通内层错误可能被 Store 包装成 InternalError/NotCommitted，不能在测试只比较原预期码而忽略真实错误链。

## 8. 真实 fixture 与独立验收边界

新 fixture 复用固定 [current_resolution_fixture_test.go](../../../tests/model/current_resolution_fixture_test.go)、[Project 配置 fixture](../../../tests/model/project_configuration_fixture_test.go)与 [wire fixture](../../../tests/model/openai_chat_wire_fixture_test.go)的真实 Account / Project / Model / Secret / Audit / D04 设施，不修改旧 helper 或放宽旧断言。只在测试 schema 中建立 Runtime-owned call/attempt/不可变观察表及严格事实 provider；每次 Validate 都使用原 Store.InTx、RequireHeldLocks、重读 canonical full身份/版本，不从可变内存 bool 授权。

正例链必须实际发生：真实 current Resolve 得到 snapshot/lease → 测试 owner 的合法 reservation 事实 → 原 Tx 写账且真实确认 → 真实 Secret ReadCredentialForUsage（如使用 credential）→ 已确认原发送资格 → 真实 adapter.Start 与受控服务器请求 → 从该 Exchange 取 Decision/Usage → 实际 Joined → owner 固定 canonical 观察/终局 → 正式 Usage 方法写账及摘要 → 当前 Human 非空查询核对。Secret 的测试 usage planner 只隔离未绑定的 Model invocation 分派，必须重读 exact attempt/lease/ref/owner/purpose/current输入/gate，不能返回常量 allow、手造 SecretMaterial 或只借执行 owner 字符串。生产 Model router / Outbound 委派仍未绑定，不用此 fixture 夸成生产 Runtime。

sent / usage / terminal 正向证据来自真正 wire；至少覆盖一个实际 success、有可靠 usage 的已发失败/流终止、成功但无 usage、显式0与NULL。not_sent 正例必须有真实已结束的本地/Do事实；dispatch unknown 仅严格持久协议 fixture，用来验保留未知与分组，不冒称网络故障或 Process 恢复已验。多组不同安全维度可重用已验证的 wire观察形状，但每个新 Invocation 的 sent 正例须来自自己 exact Exchange；性能大数据只限测试直接生成明确标记的统计 fixture，不拿它证明真实发送。

DB Unknown 仅用实际 PG 事务结果的受控 Store 装饰器及普通 mutex/writer 锁竞争；准确记录底层 Committed / NotCommitted与对外 Unknown，保留原 cause。禁止新增网络、COMMIT/ROLLBACK 代理或恢复暂停 Object 探针。装饰后的 Unknown 不应被报告成真实网络 ACK 丢失；观察到回执才能确认。对缺行/锁被阻/已提交三分支，零第二 Provider 请求；单独的 DB Unknown 不更改 dispatch/usage。

## 9. 精确候选实施范围

以下 **20 路径**是提交规格审查的候选实施所有权，当前不授写入。无新包依赖、Go module/lockfile、旧迁移/旧跨模块测试修改。9个生产源（仅identity一旧）、1个新迁移、10个测试源：

| # | 路径 | 唯一职责 |
| --- | --- | --- |
| 1 | `internal/central/usage/contract/invocations.go`（新） | 新 DTO、写端/回执、校验/复制/安全格式化 |
| 2 | `internal/central/usage/contract/authority.go`（新） | InvocationFacts、opaque依赖/plan及绑定 |
| 3 | `internal/central/usage/service.go`（新） | 构造/初始化、接口实现外壳、依赖完整性 |
| 4 | `internal/central/usage/store.go`（新） | Store、私有规范编码/严格loader、SQL公共核 |
| 5 | `internal/central/usage/authority.go`（新） | 当前Reader授权、Discovery/full union/实例/Tx验证 |
| 6 | `internal/central/usage/invocations.go`（新） | 状态/回执原子应用及exact Lookup |
| 7 | `internal/central/usage/summary.go`（新） | canonical贡献、同Tx delta、Get/Rebuild |
| 8 | `internal/central/usage/query.go`（新） | List/Aggregate/filter/cursor/2s预算 |
| 9 | `internal/central/identity/contract/identity.go`（旧） | 仅 ModelRuntime 闭集注册，不改旧安全语义 |
| 10 | `db/migrations/00018_model_invocation_usage.sql`（新） | §4 三表/约束/索引，无旧 SQL 变动 |
| 11 | `internal/central/identity/contract/identity_test.go`（旧） | 新角色与旧闭集兼容断言，旧反例不弱化 |
| 12 | `internal/central/usage/contract/invocations_test.go`（新） | DTO/opaque/绑定/不可变/安全投影纯测试 |
| 13 | `internal/central/usage/authority_test.go`（新） | nil/typed-nil/同Store/完整锁/当前授权纯测试 |
| 14 | `internal/central/usage/summary_test.go`（新） | 六字段nullable/前后贡献/零值/溢出纯测试 |
| 15 | `tests/model/usage_fixture_test.go`（新） | 严格持久Runtime port及真实既有服务组合 |
| 16 | `tests/model/usage_ledger_test.go`（新） | 真实wire→账本/终态/summary，幂等与并发 |
| 17 | `tests/model/usage_authorization_test.go`（新） | 当前准入、技术收尾、错身份/锁/Store反例 |
| 18 | `tests/model/usage_query_test.go`（新） | 非空查询、分组/cursor、摘要重建及性能预算 |
| 19 | `tests/model/usage_unknown_test.go`（新） | 精确事务Unknown/确认/原writer普通锁/零重发 |
| 20 | `tests/model/usage_schema_test.go`（新） | fresh/populated/约束/历史FK/原子失败与前向边界 |

既有 C0 `types.go/query.go`、Model/Secret/Project/adapter/Outbox/Audit/app/fixture生产和旧测试其余路径全部只读。必须改范围外 helper、Store、公共契约或新增另一个迁移才能闭合时，先报告具体事实/最小范围；不得复制放宽的旧服务来维持“20路径”。Schema和contract可先冻结供独立静审，但不把纯类型/建表作为本卡完整验收。

## 10. 验收门槛与交接

纯检查固定 Go1.27.1、离线/readonly依赖、私有可复用cache/TMPDIR，最小源码与必要 embed/运行资产闭包；精确 argv/env/exit/input manifest 与原日志保留。新包及受影响 identity/model/usage contract 的 unit / race / vet；全部 integration compile 与 Central、Runner 两 cmd build。缺资产或首红按事实保留，修后只补受影响检查，不把空日志补写成原始exit记录。

真实 fixture 只在主线程明确交独占窗口后执行，沿现有 driver 的 race / count=1 / 6m，不擅自提高预算。至少以下 **8 个有名顶层新组**，每组列出实际子例，独立验收按风险补最小未覆盖探针；不能只跑空Reader或用“框架通过”代替数据结果：

1. `TestUsageInvocationWireLedger`：上述真实链；成功、已发可靠usage后失败/取消、成功无usage、0/NULL、actual Joined 前拒Finalize且摘要/receipt零变化；各 exact Exchange 实际请求计数，无隐式发送重试。不得把仅本地假Exchange的Joined当D04实证。
2. `TestUsageInvocationIdentityAndAuthority`：同Owner他Project、错call/ordinal/input/schema/snapshot/process/fence/原始Actor、技术Actor不能reserve或获得当前准入；规划后 Session/gate/事实版本改变，reserve/authorized零effects；真实已发后撤销Session/gate，原技术终局仍能记录可靠usage而不能新发。跨instance/Store/过期Tx/缺一把原writer锁失败，结构合法反例不得先被DTO拒绝后冒领授权拒绝。
3. `TestUsageInvocationReplayAndAtomicity`：同call并发reservation、不同sequence竞争、旧回执同义/异义、final同义与异义、attempt前驱未终局拒绝、保持immutable header；SQL/统计溢出/外层拒绝回滚时 invocation+observation+summary 全部无部分变化；故意忽略入口错误后外层提交也不留该入口半写入。
4. `TestUsageInvocationUnknownConfirmation`：实际提交/回滚后装饰Unknown、锁竞争有界失败与之后真实读取，exact回执/缺行区别；原state/cause/attempt、DB Unknown与dispatch_unknown分离、仍仅原请求的真实发送，禁止自动重发；取消不当作原writer终局。
5. `TestUsageReaderNonemptyPagination`：实际非空多页、全部Filter/八GroupBy、NULL组、0/NULL/混合分母、status只按C0计数、默认30日固定cursor/显式历史范围/不同limit；同User新合法Session可续但旧Session不可，换Actor/project/filter/group/order或篡改拒绝；archived读与deleting拒沿真实gate，非本卡Archive实现。
6. `TestUsageExecutionSummaryRebuild`：多个真实调用相同Execution的汇总、无Execution不造行、错Project/未知Execution、缺失/有效结构但错误摘要修复、重复重建、与Observe/Finalize普通并发互斥；canonical聚合与摘要逐六字段/计数/版本精确比较，不只断言count大于0。
7. `TestUsageSchemaAndHistory`：PG17 fresh prefix1..18、PG17 populated prefix1..17→18（均沿 D03 支持的 PostgreSQL 17.x、最低17.8）；PG16仅为既有不支持版本反例，不作为升级成功环境；原字节失败重试/约束反例、nullableFK/安全历史保留；通过正式Model配置删除方法删除无真实引用的Provider/Model后，历史Usage仍可读、live FK为空、回执可确认。不得直接改外域表伪造合法删除，也不解除未来Agent/project_summary引用缺adapter的拒绝。
8. `TestUsageQueryBudgetAndSafety`：确定的大数据统计fixture/实际EXPLAIN，2s含锁等待、无部分或截断总数、nullable和numeric溢出；cursor最大8标量与8KiB token边界，坏持久JSON/摘要/版本安全失败；错误/日志/回执/读DTO无材料/输入/endpoint/响应正文。性能fixture与真实发送证据分开标记。

适用旧回归包括 C0 model/usage、current_resolution、Secret Model planned read、Project配置/Owner/currentSession、text/structured wire；选择受接口/schema/fixture影响的实际顶层并记录，不机械全跑无关模块。Default root 与未启用本包的System功能须仍编译/行为兼容；不要求在root装配Usage以过测。

迁移只 Up，fresh / populated 保留旧数据，失败靠真正事务回滚及同字节重试或另审前向修复；不得添加 Down、删已确认ledger或调整既有00017。库级验收不表示生产迁移已部署。运行前核预期资源exact ID/name/labels，活动fixture观察nonce/labels，结束两次exact-ID absent、原基线不变、所属进程0/runtime空；及时交回窗口后整理报告，不拷全树/cache/binary/凭据。

最终冻结20源/SQL及完整输入，交独立验收：静态权限/事务/查询审查 + 原失败与修后证据 + 必要真实补证。主线程采纳后才按精确路径提交。报告明确已实现账本和查询职责，以及未绑定 InvocationFacts生产owner、call/Provider编排、lease/Process/lifecycle/HTTP/root；不称完整D09，不将严格fixture端口写成生产allow。当前仍只是候选规格，无Go/SQL/Docker/Provider运行或业务改动。

## 11. 规格采纳与实施移交

主线程已采纳独立 STATIC PASS，精确提交推送本卡 `9aad5f5d26bf5af066390a8bd35b91d57d6b8c8b`，随后正式授权 `recovery_handoff` 按固定业务 `4295df7` 实施 §9 的 20 路径，含唯一 00018 迁移；`restore_test_dependencies` 负责独立验收。实施授权不等于业务、SQL、性能或真实运行通过，本次文档归位不授任何额外路径或共享资源窗口。

[规格档案与原证据](../agent-team/invocation-usage-ledger-spec-verification.md)保留被审 rev1、19 输入 Git 定位、13 接缝指纹、作者及独立检查、采纳行政差量。除下述 §10 第7项 PG 版本验收口径更正外，§1–10 技术正文逐字保持，后续验收仍须满足其中全部门槛。生产 Facts/Runtime 绑定、DB Unknown/dispatch unknown 区分、暂停 Object/Artifact、Summary 与 ready503 等限制不变；尚无完整 D09 或生产装配交付结论。

### PG 版本验收口径更正（2026-10-05）

已接受 rev1 的 §10 第7项曾写成“PG16 populated1..17升级”，该口径错误；原规格及原静审事实仍保留于上列持久档案，不追改为当时已经正确。本次仅将正向迁移验收更正为 **PG17 fresh prefix1..18，以及 PG17 populated prefix1..17→18**；PG16仅沿既有不支持版本反例。

依据为固定业务 `4295df7d51c1f171df78ab3f0d9cef2fd241a505` 的 [store.go](../../../internal/central/postgres/store.go) `checkCompatibility` 第254–255行与 [journal.go](../../../internal/central/postgres/journal.go) `checkSQLCompatibility` 第219–220行：两处均要求 `version/10000 == 17 && version%10000 >= 8`，否则返回 `VersionUnsupported`。固定 [fixture.go](../../../tests/testsupport/postgres/fixture.go) 的 `UnsupportedImage` / `UnsupportedEnv` 对应PG16，既有 [security_test.go](../../../tests/database/security_test.go) 第113–139行核Store及迁移器拒绝该版本。这里的17.8下界限于17.x，不包含其它major。

这是规格验收口径更正，**不是生产兼容范围变更**，不修改 D03、fixture、SQL、实施20路径或其它业务/权限门槛，不证明新迁移已通过。本次没有运行 Go、PG、Docker 或 fixture。
