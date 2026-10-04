# D06 Transactional Outbox 工程规格

- 修订：4；对应[主卡](d06-transactional-outbox.md)，原设计基线 `main@671d95c`，本次增量基线 `main@d866d5c`。B01 及修订 3 公共载体已独立验收；本轮仅补未尝试 delivery 的 Project 停止终态、00009 窄迁移及 RequeueTarget，不声明 B02 或 D06 整体已验收。
- 依据：[事件架构](../../architecture/platform-infrastructure/internal-domain-events.md)、[D01 事件与注册屏障](d01-contracts/runtime-events.md)、[事务与锁](d01-contracts/foundation.md)、[生命周期](d01-contracts/domain-lifecycle.md)、[W40](d01-contracts/walkthroughs.md)、[开发计划 D06](../development-plan.md#d06-outbox-与事件投递)。
- 复用 Go 1.27.1/local、现有 pgx/Goose、D03 事务、D04 身份/Audit/cursor 和 D05 真实 ProcessGuard；不增加依赖、broker、业务表、全历史 replay、匿名运维 HTTP 或成功 stub。

## 1. 结果分块与所有权

| 结果 | 完整交付 | 独占实现范围及前提 |
| --- | --- | --- |
| B01 持久事件与可组合事务 | typed catalog/envelope、正式权限端口、持久注册屏障、原子 append/delivery、marker 原语、00008、D03 持锁验证；真实并发/rollback/晚提交测试 | 新 `internal/central/event/contract/{event,catalog,codec}.go`、`internal/central/outbox/contract/{append,authority,handler,delivery,lifecycle,diagnostics}.go`、`internal/central/outbox/{service,catalog,registration,append,repository,transaction}.go` 及同目录测试；新增 `db/migrations/00008_transactional_outbox.sql`；下表 D03/Audit 最小增量；新 `tests/outbox/` 的本块测试 |
| B02 可运行投递与全生命周期 | 有界 dispatcher、独立重试/unknown/恢复、当前授权重投及 Audit、诊断、Project 清理、Central 初始化/健康/停机；完整故障与兼容验证 | B01 冻结后新增 `outbox/{dispatcher,attempt,recovery,requeue,cleanup,diagnostics,runtime}.go` 及测试，必要时修改 B01 新包内部文件；下表 app/fixture/docs 增量；不改已验 D05 核心 |

两块均应可独立构建和验证；B01 不装配半成品 worker，B02 才接 Central。各块单作者、顺序移交；root 维护主卡、计划、台账。实现者可在以上新包内按职责调整文件拆分，不扩大公共接口或旧文件范围。

| 既有文件 | 精确必要增量、归属与兼容 |
| --- | --- |
| `internal/central/postgres/transaction.go`、`error.go` 及对应测试 | B01 增加 §4 `RequireHeldLocks` 与闭集 `LOCK_NOT_HELD` 内部错误；不改 Tx、AcquireAll 或提交语义 |
| `internal/central/identity/contract/identity.go` 及测试 | B01 仅增加注册职责 `outbox-delivery`；由真实 claim 构造限定 Service cause，不授予 Session/Owner/Admin |
| `internal/central/audit/contract/{types,metadata}.go` 及测试 | B01 增加 §8 唯一重投 Action/Resource/Producer/typed metadata；旧 action/metadata 含义不变，00008 扩展既有 CHECK |
| `internal/central/audit/service.go` 及测试 | 仅在闭集校验确有需要时接入新 Producer；重投沿既有 Human 当前 Session/Owner/System + Mutate + `CheckAppendInTx`，不增加 Service 写 Audit 豁免 |
| `internal/central/outbox/contract/authority.go`、`diagnostics.go`；新 `contract/lifecycle_step_test.go`、`contract/diagnostics_test.go` | 修订 3 已交付的 B02 公共补口：§3 LifecycleStep/variant 校验及完整依赖 binding，§8 typed Summary/数值与空值校验。此次不再改 diagnostics.go；真实统计与 provider 场景仍在 B02 原授权测试范围实现 |
| `internal/central/outbox/contract/authority.go`；新 `contract/requeue_target_test.go`；必要时 `contract/contract_test.go` 的既有 Requeue 样例 | 修订 4 仅新增 §3 RequeueTarget/字段与 variant 校验、完整 target binding；旧测试仅机械适配 Requeue 构造。未交付 B02 Requeue 调用同步适配；其他 variant 断言、`delivery.go` 的 DeliveryIdentity.Valid/CauseRef、Stage 和全部现有服务方法签名保持不变 |
| 新 `db/migrations/00009_outbox_unattempted_stop.sql`、`tests/database/outbox_unattempted_stop_migration_test.go` | B02 独占 00009：§5 事务型 Up-only 窄 ALTER；fresh/8→9 升级、非法分支和 DDL 回滚验证。停止/恢复/人工重投/诊断的真实行为测试补入 B02 原授权范围，不改旧迁移或新增状态表 |
| `internal/central/app/{app,object,resources,health,diagnostics}.go`、新 `app/outbox.go` 及测试 | B02 实施 §10 私有 assembly、真实初始化/检查/关闭；保留 process/service/guard/runtime，覆盖失败和晚到资源所有权 |
| `internal/platform/logging/security.go` 及测试 | B02 仅增加 outbox 初始化/可用/不可用的固定安全 phase；中立包不 import Central |
| `tests/testsupport/postgres/cmd/fixture/main.go`、新 `tests/database/outbox_migration_test.go`、新 `tests/process/outbox_test.go` | B01 增加迁移兼容测试、将 `tests/outbox/...` 和 `internal/central/outbox/...` 纳入既有真实 PG 测试命令；B02 增加进程场景。现有三条 integration scripts/MinIO 和 outbound fixture 链直接复用，无需修改 shell 脚本或另起基础设施 |
| `docs/development/backend/README.md`、`AGENTS.md` | B02 局部同步实际入口、测试和仍未绑定的权限/领域边界；root 另行移交写权 |

`00001–00008`、`go.mod/go.sum`、D05 object/Artifact/transfer 实现及 Runner 代码冻结。Outbox 不 import object；共享 ProcessGuard 只在 app 组合映射。00008 属于已验 B01，不能改任何字节；修订 3 载体补口不涉及迁移，修订 4 仅用新编号 00009 修正 §9 所需状态约束，不增加表、字段或索引。两份迁移均为事务型 Up-only，无非事务 DDL。

## 2. Typed 事件、注册目录与边界

按 [D01 单向分层](d01-contracts/README.md#代码依赖与运行时依赖)，中立的 Header/Event/EventType、codec 和纯 typed schema catalog 放在新 `event/contract`，除标准库外仅依赖 foundation，不 import identity 或任何实现。Header 的 Scope/Project 标识在此用本包中立 DTO 与 foundation 标量定义，不引用 identity.Scope/ProjectID。需要 Actor/授权的 Append/Producer/Project/Handler、delivery/lifecycle/diagnostics 端口放上层 `outbox/contract`，只直接依赖 foundation、event/contract、identity/contract；领域实现/组合根适配标识，不使基础层反向依赖。

payload 类型由生产领域定义；D06 不预建 D10–D25 的业务类型。中立 catalog 只登记稳定 producer/schema 名称、codec/validator，不接身份或权限 provider；受信 producer/handler 的授权绑定由 Outbox 上层组合根持有，外部请求不能提供执行函数、任意类型名或 SQL。

```text
Header {event_id: UUIDv7, event_type: StableName, schema_version: uint32,
        occurred_at: Instant, scope: System|Project(project_id),
        aggregate_type: StableName, aggregate_id: UUID,
        aggregate_version?: Version, aggregate_sequence?: foundation.Sequence}
EventType[T] = catalog 签发的 typed producer/schema capability
Event = 不可变 Header + 已校验 canonical payload bytes + producer identity
DefineEvent[T](catalog, definition{type,version,aggregate_type,codec,validator}) -> EventType[T]
NewEvent[T](EventType[T], Header, T) -> Event
PrepareAppend(ctx, Actor, Event) -> AppendPlan
AppendLocks(AppendPlan) -> []LockRequest
AppendEventInTx(ctx, Tx, Actor, Event, AppendPlan) -> AppendReceipt{event_id,outbox_sequence}
```

`DefineEvent/NewEvent` 为可实现的泛型函数，不在 Go interface 上声明泛型方法；typed wrapper 内部擦除类型，handler 解码仍回到所属领域的明确类型。codec 对该 schema 编解码并校验，拒绝未知字段/重复 key/尾随 JSON、非有限数、缺必填；不能将未校验 `map[string]any` 或原始 JSON 当正式 payload。canonical encoding 固定同 schema 的结构/字段及数值规则，byte 长度上限 64 KiB；不得含 Secret、完整正文、HTTP Authorization 或签名 URL。

Header 至少提供 version/sequence 之一且大于 0；aggregate_sequence 复用现有 foundation.Sequence，范围为 1..MaxInt64，JSON 为规范十进制字符串，缺省显式 optional，不用 0 表示缺省。同 version 可有多个不同 EventID。内部 outbox_sequence 不代表业务版本、浏览器 cursor 或全局业务顺序；允许 0 的注册初始水位另用 Progress 表示，不放入 aggregate_sequence。occurred_at 也不排序业务事实。名称为稳定小写标识（不使用 Go 类型全名），最长 128 字节；schema_version 初始 1，未知版本不解成零值。

Event/AppendPlan 的通用格式化、JSON、slog 只输出固定安全标签；显式安全投影可取 IDs/type/version，不含 payload。PrepareAppend 在 Tx 外发现所需真实 Actor/Project/来源 gate，不授予权限；其私有 issuer 绑定当前 Service/catalog、Actor 稳定主体、完整 Event digest、父身份与锁集合。Human 主体用 user_id，排除 session_id/HTTP trace；Agent 用 project/agent/execution；Service 用注册职责和稳定 cause。

正式 `ProducerAuthority` 提供 `DiscoverAppend(ctx,actor,eventSummary) -> Dependencies`、`ValidateAppendInTx(ctx,tx,actor,eventSummary,dependencies,stage) error`；stage 闭集为 current_access/new_fact。取得完整锁后，顺序固定为当前主体/路径可见权限 → 本域不可逆 Project 删除 gate → EventID/digest 幂等 → 首次 new_fact 的当前事实/gate 校验。cleaning/completed 即使旧 Event 尚未删除也拒绝旧 Append，不返回原 receipt；stopping 仅允许 §9 的正式停止事实路径。Dependencies 仅持不可变父身份/锁和供本 provider 验证的 opaque 数据，不能缓存授权成功。Project scope 还经 §3 ProjectAuthority 校验相同阶段。缺 provider 返回 `DEPENDENCY_UNBOUND`；锁后父映射改变则整 Tx 回滚 `RESOURCE_BUSY`，不补锁、不自动替换来源。业务事实允许原操作的判断仍属于原 Service，事件发布不是第二套业务 veto 流程。

EventID、occurred_at 和已定 payload 在同一业务命令尝试/unknown 核实期间固定。存储 digest 包括 producer、稳定 Actor、完整 Header 和 canonical payload，排除传输字段；通过上述当前权限及不可逆删除 gate 后，同 EventID 同义返回原 receipt，异义 `IDEMPOTENCY_KEY_REUSED`。首次 source fact/command 与 Event 同 Tx；unknown 先查原命令/原 Event，不能换 EventID 宣称重做安全。已明确回滚的尝试可重新准备。

## 3. 正式授权和 handler 组合端口

```text
ProjectAuthority:
  Discover(ctx, ProjectRequest) -> ProjectDependencies
  ValidateInTx(ctx, Tx, ProjectRequest, ProjectDependencies) -> error
ProcessAuthority:
  CurrentProcess() -> ProcessID
  ConfirmStopped(ctx, exact ProcessID) -> error
HandlerDefinition {stable_handler_id, event_types + accepted_schema_versions,
                   effect: canonical_converge|domain_ingress, ordering_policy, handler}
Handler:
  Prepare(ctx, Event) -> HandlerPlan
  ValidateInTx(ctx, Tx, Event, HandlerPlan) -> error
  HandleInTx(ctx, Tx, Event, HandlerPlan) -> Ack|Retry(SafeReason)|DeadLetter(SafeReason)
RegisterHandler(ctx, HandlerDefinition) -> Registration
```

ProjectRequest 是闭集 `append|deliver|inspect|requeue|lifecycle`，绑定 ProjectID、真实 Actor/限定 delivery Service cause、EventID/DeliveryID/AttemptID/fence、handler effect 或 LifecycleCause，不能用 caller bool 表示 owner/archived/已停止。append/requeue 明分 current_access 与首次 apply 阶段；Append 在幂等前额外执行 §2 本域不可逆删除 gate，首次业务 gate/version 仍在幂等之后，完成 receipt 不跳过当前权限。delivery cause_ref 为上述完整稳定身份的规范化 SHA-256，provider 仍须核当前持久 claim，hash 本身不授予权限。D08 适配器负责真实 Project/Owner/gate/cause；D07 Session/SystemAuthority 保持当前校验。System scope 的人工查询/重投只允许当前 System Admin；Project 只允许当前 Owner，Admin 不绕过 Owner。内部技术恢复只核本域已持久事实，不以后台 Service 冒充原 Actor 重新执行业务。

在 `authority.go` 新增 `RequeueTarget{EventID,DeliveryID,HandlerID,Scope,Effect}`（沿既有 ID/StableName/Scope/HandlerEffect 类型），Valid 校验这些必需身份，不含 AttemptID/fence；`ProjectRequestDetails.Requeue` 使用此类型。RequeueProject 要求 Human、合法 Stage、有效且属于相同 Project 的 RequeueTarget，旧 Delivery 字段必须为空，Event/Lifecycle/LifecycleStep 沿原空值规则；其他所有 variant 必须 Requeue 为空。无论目标是否曾经 claim，人工重投都走该 target；正式 callback 仍用原 DeliveryIdentity，AttemptID/fence 必须真实且大于 0，绝不以 optional/假 ID 放宽 Valid/CauseRef。

Requeue 的依赖 binding 必须覆盖 Kind、ProjectID、稳定 Actor 和完整 RequeueTarget，现有 current_access/new_fact 的分阶段验证保持；不能对 ProjectRequest 的安全标签 JSON 求摘要。锁后重读目标的 Event/handler/scope/effect 和当前权限，目标映射不符整 Tx 回滚，不替换目标或补锁。该结构只定位真实 delivery，不代表当前状态/version、授权成功或正式 claim；命令幂等及 expected_version 仍按 §8 验证。

在 `authority.go` 新增 `LifecycleStep` 字符串闭集 `stop|inspect|cleanup` 及 `ProjectRequestDetails.LifecycleStep`。`LifecycleProject` 必须带合法 step 且 `Stage` 仍为空；其余所有 variant 的 LifecycleStep 必须为空，原 Stage 规则不变，不把 step 塞入通用 Stage。RequestStop/InspectStop/Cleanup 分别固定构造 stop/inspect/cleanup，外部输入不能替换方法对应 step。

lifecycle 的完整请求/Dependencies binding 显式包含 Kind、ProjectID、稳定 Actor（含注册职责/Project/cause_ref）、LifecycleStep 和 `Lifecycle.Details()` 的 operation_id/action/project_version；不能对只输出安全标签的 ProjectRequest/LifecycleCause JSON 求摘要。改 step、Actor 或任一 cause 字段后旧计划均不匹配，锁后仍须重验。actor 必须为同 Project 的真实 project-lifecycle Service，D08 在当前 Project 锁下验证其 cause_ref 确实对应该 operation/版本/动作和当前 gate；类型有效或哈希相等不替代授权。只有 cleanup 额外核其他参与者已收束；stop/inspect 只核本次操作合法及 Outbox 自身停止事实，不要求其他参与者先完成，不形成停止顺序循环。D08 未绑定时仍拒绝。

HandlerPlan 是受信注册 handler 签发、绑定 EventID/handler/当前父身份的不可变计划；暴露完整业务锁集合，私有数据由该 handler 锁后重验。不接受运行时请求提供 handler/plan。`HandleInTx` 只能调用领域 InTx 端口；域内结果、processed marker 和成功 delivery 同一事务。调用普通 wrapper 嵌套 Tx、网络/模型/工具/SMTP I/O、返回 ack 后异步补业务写均禁止；需要外部工作的领域先在该 Tx 持久化自己的作业/输入，由正式执行模块负责。

`canonical_converge` 只校正已提交事实的投影/marker；归档中/已归档仍须限定 Service cause、当前来源和幂等，不复活来源、不启动业务。`domain_ingress` 必须通过来源当前业务 gate，不能借 Converge 放宽 Launch/Resume 等权限；D08 archive stop 会关闭此类在途/待投业务入口。领域模块负责其具体 Validate 规则，D06 不默认授权。删除的 stopping gate 停止新业务回调，但保留 §9 真实停止事实的受限 Append；cleaning/completed 同时拒绝回调和 Append。

ordering_policy 必须明确为 `version_guarded` 或 `canonical_reconcile`；handler 在自己的投影/结果中保存可比较版本，或锁内重读 canonical 并带版本保护写入。先到 v43、迟到 v42 不能回退；有连续性缺口则重读来源，不凭 payload 猜历史。去重仅用 `(event_id,handler_id)`，不能以 aggregate version 吞掉 sibling。领域需要 sibling 先后时必须提供 sequence；D06 不定义跨 handler/aggregate 的完成顺序。

## 4. 一次完整锁集合和 D03 窄补口

```go
func (*postgres.Store) RequireHeldLocks(
    context.Context, foundation.Tx, []foundation.LockRequest,
) error
```

实现经现有 find 验证 owner/live，进入同一 Tx 的 enter/leave 槽；逐项校验 key/mode 与私有 held。EX 满足 SH/EX，SH 不满足 EX；缺失、非法或弱模式 poison 整 Tx，调用者忽略错误也不能提交。该方法不执行 SQL、不改 highest、不 Acquire、不升级、不接受 caller token；错 Store/失活/并发使用沿原拒绝语义。只在 D06 所需 Store interface 增此方法，现有 D05 结构接口不受影响。

完整 union 在开启业务 Tx 前预收集，进入后只调用一次现有 AcquireAll（包含对象时使用其 `AcquireAccessPlansInTx(plans, extra)` 一次取得完整 union）。按 D03 排序并合并最强模式：command rank0 → 注册/System/User rank1 → Project rank2 → 真实 Agent/来源 aggregates → records rank6。Append/handler/Audit 的 InTx 仅 RequireHeldLocks/验证，不追加锁；同一 key 预知需 EX 就从开始取 EX，不做 SH 升级。

固定注册屏障为 `SystemConfigLock("outbox-registration")`。每个可能 Append 的 Tx 初次 union 包含 SH；含 handler 的业务 Tx 也预带 SH，允许其计划中显式声明的后继事件。AppendPlan 必需锁还包括真实 Actor/Project/来源 dependencies 和 `OutboxRecordLock("event:"+event_id)` EX。可能发出的 EventID/计划须在锁前确定；回调发现新的低序 gate 或未声明事件时回滚后重新准备。

Delivery 各阶段使用同一 `OutboxRecordLock("delivery:"+delivery_id)` EX；业务 Apply 额外用 hash 压缩的 `(handler,scope,aggregate_type,aggregate_id)` stream record EX、handler/来源完整 locks。技术 claim/marker 核实不读取领域业务表，只取该阶段真实所需 Project/Delivery 锁。不能先取得 delivery record 再去做对象/领域完整锁规划；发现批次只读、行锁和 UPDATE 均在 AcquireAll 后。所有 namespaced record key 长度不超过既有 IdempotencyKey 限额。

## 5. 持久模型、注册屏障和原子 append

00008 新建 `agenteam_outbox`，所有外键/唯一键和枚举 CHECK 显式命名；不跨未来业务表设 FK。各表都有固定 schema/version 校验，时间以 PG UTC timestamptz，业务 ID 用 UUID，fence/counters 用非负 bigint 并检查溢出。

| 表 | 必需事实、约束与索引 |
| --- | --- |
| `events` | EventID PK、唯一 sequence、producer、Header、scope/project、canonical payload、semantic digest、created_at；version/sequence 至少其一；Project/sequence、aggregate 索引；已提交字段不可改 |
| `handlers` / `subscriptions` | 稳定 handler PK、effect/ordering、声明 digest；每 `(handler,event_type)` 唯一、accepted versions、首次 accepted_after_sequence、注册时间；无停止历史回放模式 |
| `deliveries` | DeliveryID PK、`UNIQUE(event_id,handler_id)`、FK Event/handler、scope、phase、row version、redrive_cycle、cycle/lifetime attempts、next_attempt_at、current AttemptID/fence、safe reason/last_at；due、scope、handler/phase 索引 |
| `attempts` | AttemptID PK、Delivery FK、ProcessID、fence、cycle/number、started_at、deadline、结束/实际 join 事实、commit_unknown/checkpoint、safe reason；`UNIQUE(delivery_id,fence)`；保留已失败尝试，不覆盖历史 |
| `processed` | `PRIMARY KEY(event_id,handler_id)`、Delivery/Attempt/fence、processed_at、最小结果摘要；仅与 handler 副作用同 Tx 插入；不保存业务正文 |
| `requeue_commands` | 命令身份 hash 唯一、稳定 Actor/digest、DeliveryID、原状态/版本、新 cycle、receipt；与重投和 Audit 原子提交 |
| `project_lifecycle` | ProjectID PK、精确 operation/action/project_version、停止/清理 checkpoint、公平扫描位置、最小完成 receipt；archive checkpoint 只描述该旧 operation，delete 的 stopping/cleaning/completed 不可逆；完成后不保事件、handler payload 或其他项目业务内容 |

00008 的 `outbox_deliveries_attempt_check` 不允许从未 claim 的 delivery 直接进入终态，与 §9 的停止要求不相容。新 00009 使用既定 `-- agenteam:transaction tx` / `-- +goose Up` 头，仅在同一事务执行以下 ALTER；旧两个分支保留，新分支只允许 Project 的零尝试 project_stopped 终态：

```sql
ALTER TABLE agenteam_outbox.deliveries
  DROP CONSTRAINT outbox_deliveries_attempt_check;
ALTER TABLE agenteam_outbox.deliveries
  ADD CONSTRAINT outbox_deliveries_attempt_check CHECK (
    (current_attempt_id IS NULL AND fence=0 AND phase='pending')
    OR (current_attempt_id IS NOT NULL AND fence>0)
    OR (scope='project' AND project_id IS NOT NULL
        AND current_attempt_id IS NULL AND fence=0
        AND cycle_attempts=0 AND lifetime_attempts=0
        AND phase='dead_letter'
        AND safe_reason IS NOT DISTINCT FROM 'project_stopped')
  );
```

NULL reason 明确不命中新分支，不能由 CHECK 的 UNKNOWN 放行；不扩大到 System、其他 reason/phase 或非零尝试计数。redrive_cycle 不要求为 0，允许显式重投后尚未首 claim 再遇合法停止。旧合法 pending/claimed 数据继续满足原分支，失败整体回滚 DDL/迁移进度；不改 FK、其他 CHECK 或伪造历史 attempt。CHECK 不跨表证明授权或 handler effect，实际写入仍由 §9 在完整锁后验证当前 provider/注册事实。

内部 `outbox_sequence` 使用 PostgreSQL bigint sequence，`CACHE 1 NO CYCLE`；Initialize 核实配置，禁止预缓存尚未使用的小序号破坏注册边界。回滚空洞合法，不将缺号当事件丢失；边界取 sequence 的 last_value/is_called（从未分配为 0），不用会随删除下降的 MAX(events)。

RegisterHandler 使用独立短 Tx，一次获取注册 EX 和 handler record EX。EX 等此前 SH writer 实际 commit/rollback 终局；之后保存 handler/subscriptions 与当前边界，并提交后才发布到进程目录。注册 Tx 不读业务表、不调用 handler、不执行 canonical rebuild。Append 在 nextval 前 RequireHeldLocks(SH)，同 Tx 插入 Event **和所有当时已注册匹配 type 的 delivery**，省去第二个易漏的 fan-out checkpoint；任一写失败整体回滚。

订阅按 event_type 命中，不能因 schema 不认识而不建 delivery；执行时未知 schema 明确 dead_letter。新 handler 只接受边界以后的事件；旧已提交事件不回补。重启相同声明返回原 Registration/边界，不能重置为新高水位；新增 type 在同 EX 协议取得该 type 新边界，新增受支持 schema 不改变原边界。删除订阅/改变 effect、ordering 或不兼容替换 handler 身份首版拒绝，须另行显式迁移。

注册与生产同时发生只有两种结果：writer 先拿 SH，则其 event 完整落在旧集合/边界之前；register 先拿 EX，则后续 writer 一定看到新 handler 且 sequence 更大。即使先 nextval 后长时间晚 commit，也不能穿过 EX 留漏投窗口。发布内存目录前必须确认注册 commit；unknown 取得同注册/handler 锁后查精确声明，未确认不得声称注册成功。

进程 catalog 最多 128 handlers、每 event 最多 128 subscriptions，初始化验证界限，避免单业务 Tx 无界 fan-out；不是用户配置的优先级。没有生产定义时 catalog 可以真实为空，生产路径未注册/未授权仍报错，不安装 ack-all consumer。

## 6. Claim、handler、marker 与 commit unknown

```text
pending -> processing -> succeeded
                  \-> retry_wait -> processing
                  \-> failed | dead_letter
failed/dead_letter --已授权人工重投--> pending（新 cycle，同 DeliveryID/Event）
pending（从未 claim）--已授权 Project stop--> dead_letter/project_stopped（无 attempt）
```

`retry_wait` 是持久调度细节，对 D01 Delivery 投影为 pending + next_attempt_at；processing 包含运行中或提交待核实，不能因超时伪装 failed。failed 为本 cycle 自动次数耗尽；dead_letter 为未知 schema、固定契约/来源终局等不可自动重试原因。上述状态不取消已提交事实，也不支持修改原 payload。

无 attempt 的 project_stopped 是已确认停止的 delivery 终态，不进入 due 调度或 processing/unknown 恢复，不需要虚构 Process/死亡证明，不增加 active callback/attempt 计数，也不插入 processed。若已经有真实 claim，则仍按原取消/join/marker/unknown 流程收敛，不能套用零尝试捷径。

claim 先只读发现候选，再 Tx 取得完整 scope/Delivery locks，重读 phase/due/local cleanup gate、当前 handler 绑定，写 ProcessID、新 AttemptID/递增 fence、deadline 和 processing。commit 确认为 committed 后才进入回调；unknown 先核实相同 claim，绝不先运行。事件与 delivery 仅从已提交表可见，未提交 producer 无调用机会。

一次执行依次为：Tx 外 Decode/Prepare → 新 `WithinTx(NewDeliveryCause(eventID,handlerID))` → 一次完整 AcquireAll → RequireHeldLocks、重读 Event/claim/fence/local gate/processed → 当前 Project/handler Validate → HandleInTx → 同 Tx 写 processed、attempt 成功、delivery succeeded。已有 marker 只核精确身份并收敛成功，不再调业务。handler 返回 Retry/DeadLetter/panic 时以内部回滚信号结束整个 Tx，不能提交部分副作用后另报失败。

回调真实返回且原 Tx 明确 not_committed 后，另开短 Tx，在同 Delivery EX/fence 下写 retry/failed/dead_letter checkpoint。SafeReason 闭集为 `handler_retry,unavailable,deadline,handler_panic,unsupported_schema,invalid_event,source_terminal,authorization_changed,project_stopped,unbound_handler,commit_unknown,process_unconfirmed,shutdown`；Decode/Prepare/Handle panic 均安全收敛，原 panic/error/SQL/payload 不进数据库、日志或诊断。被取消但仍在运行的 callback 不释放并发槽、不写“已 join”、不允许新 fence 越过它。

所有 WithinTx 先判断 CommitResult.State；unknown 的 Fault 可以 nil，不能用 `err == nil` 判成功。Apply commit unknown 时保留 processing/原 AttemptID，不再次调用 handler。核实 Tx 必须先拿原 writer 的同 Delivery EX（按完整低序 gate 在前），等旧 DB Tx 真正终局后查 marker/claim/fence：marker 存在即完成；marker 不存在且原 callback 已 join/精确进程已死，才可记未提交并按策略重试；拿锁超时/DB 不可用继续 unknown。

这利用本域 marker/claim，不新增通用结果仓库。核实 Tx 自己的 commit unknown 也不能宣称 checkpoint 完成，继续按同事实恢复。回调不得在返回后留下 goroutine 写业务；本地 admitted registry 直到 callback、Tx 清理和 checkpoint 尝试都已 join 才减少。DB 中的 handler_returned/terminal 记录不能冒充整个本地 goroutine 已 join。旧 fence 的所有写入都条件校验 current AttemptID/fence/Project gate，失败整体回滚。

## 7. 有界调度、恢复和公平进展

首版固定工程默认值：全局 4 个回调、每 handler 最多 2 个、每批发现 64 条、空轮 poll 1s、单 callback 上限 30s（更短 parent 优先），每 cycle 最多 8 次实际 claim。失败等待 1/2/4/8/16/32/60s；停机取消不将尚未核实的尝试计为已失败。可在包内 Options 向下收紧测试预算，生产无无界值/自动扩大环境开关。

due 查询先按 next_attempt_at/sequence，再公平轮转 handler；同 handler+aggregate 的执行由 stream EX 串行化。被锁/外部活实例阻挡的项有独立下次检查时间/持久扫描位置，不能令最早 64 条长期占满候选；实际递增 fence 仍须锁内重新验。失败 handler 不阻塞其他 handler，也不要求在旧 delivery failed 后停止同 aggregate 的 canonical 收敛；相对版本保护始终执行。

Recover 对 processing/unknown、retry 和 Project cleanup 分独立批次推进；每项 checkpoint 结束再处理下一项。可复用持久 recovery_pass + ID 轮转，轮到受保护项也推进扫描位置；单个 `RESOURCE_BUSY`/未绑定死亡证明不能令整轮提前 return，必须让本实例可收敛项继续。发现结构损坏记组件 unavailable，而不是静默跳过当作成功。

deadline/heartbeat/TTL 只触发取消和检查，永不证明死亡、偷 claim 或释放 fence。当前实例用服务私有 admitted registry/join channel 证明结束；其他实例必须 `ProcessAuthority.ConfirmStopped(exact owner)` 正向通过，再取得原 Delivery EX 等待数据库事务终局。失败/跨 host/未知部署或仍活实例保持 processing/unknown。确认 OS 本地进程死只允许收敛本域 DB 回调，不证明 Runner/外部请求终止。

重启恢复同一已登记 handler，不能把旧 processing 一律 pending 或增加新 AttemptID；先核 marker，后确认上一执行终局，最后才重试。未绑定 handler/Project provider 不执行业务；技术核实可以只读取/收敛本域 marker/attempt，但不得凭此新建业务结果或释放来源资源。孤立测试用真实 fixture authority，Central 不注入默认允许实现。

## 8. 人工重投、Audit 与安全诊断

```text
Requeue(ctx, Human, CommandMeta, DeliveryID, expected_version, RequeueReason)
    -> Result<RequeueReceipt{delivery_id,cycle,version}>
InspectDelivery(ctx, Human, Scope, DeliveryID) -> SafeDelivery
QueryDiagnostics(ctx, Human, Scope, DiagnosticsFilter, PageRequest) -> DiagnosticsPage
```

Requeue 先当前 Session/Owner 或 SystemAdmin，随后查原稳定命令，再查 expected_version 与 failed/dead_letter；完整命令 lock、User/Project/Delivery/Audit 所需 locks 一次取得。摘要绑定 scope、稳定 user_id、DeliveryID、期望版本、闭集 reason，不含 session/HTTP trace。同义重试返回原 receipt，异义冲突；原命令成功响应丢失后不再受旧 expected_version 阻断，但当前权限仍必须通过。processed 已存在禁止重做业务；禁止对 pending/processing/succeeded 重投、替换 payload、换 handler 或越过删除 gate。

首次人工重投是新的管理命令，沿当前 Mutate gate；历史事件/失败状态不赋予归档项目新的业务写权限，已完成命令仍可在当前可见权限下读原 receipt。已经合法登记的 canonical_converge 自动重试/恢复仍按 §3 处理。RequeueReason 闭集为 `operator_retry|schema_available|dependency_restored`，不接受自由文本。没有人工权限 provider，或新重投所需 handler 当前未绑定时返回 `DEPENDENCY_UNBOUND`，不提供未鉴权 HTTP 路由。

无 attempt 的 dead_letter/project_stopped 同样可成为显式 Human 重投目标；archived 中首次重投仍被 Mutate gate 拒绝，Restore 只恢复当前准入，不自动重开它。恢复 active 后，新的合法重投与 Audit/命令 receipt 同 Tx：redrive_cycle/version 递增、phase=pending、cycle_attempts/lifetime_attempts 保持 0、current_attempt_id=NULL/fence=0，不创建 attempt。之后首个实际 claim 才生成 AttemptID、fence=1、cycle/lifetime attempts=1；响应丢失沿原命令核实，不另建 cycle/attempt。该操作仅重新安排通知，不绕过 handler 当前来源校验或复活旧 Execution。

公共失败复用既有 `INVALID_ARGUMENT/PAYLOAD_TOO_LARGE/UNAUTHENTICATED/SESSION_REVOKED/FORBIDDEN/NOT_FOUND/RESOURCE_DELETED/PROJECT_NOT_ACTIVE/VERSION_CONFLICT/IDEMPOTENCY_KEY_REUSED/INVALID_STATE/RESOURCE_BUSY/SCHEMA_UNSUPPORTED/DEPENDENCY_UNBOUND/DEPENDENCY_UNAVAILABLE/COMMIT_UNKNOWN/SHUTTING_DOWN` 及 cursor 错误，附真实 CommitState；不新增 HTTP 业务错误码。目录不兼容、元数据损坏或安全检查失败不是可返回成功的空队列。

同 Tx 写 Audit：Action `outbox.delivery.requeue`，Resource `outbox_delivery` + 真实 DeliveryID，Producer `outbox`，Outcome success；typed metadata 为 `delivery_id,event_id,handler_id,from_state,redrive_cycle,reason_code`。AppendKey 使用既有 `CommandAppendKey(outbox,command,0)`；不接调用方 AuditID/time，不用虚构 Service actor。失败不能绕过 Audit 宣称重投成功；unknown 依原 command/marker 同锁核实。00008 扩展 `audit_records_action_check/resource_kind_check/producer_check/check1/check2` 的对应闭集，新增 outbox 专属 CHECK 限定三者对应、Human、success 和 typed metadata；旧数据与旧闭集分派保持兼容，新 ServiceName 不获得 Audit 服务身份豁免。

查询逐次当前授权并绑定 Scope/主体；安全投影含 IDs/type/schema/handler/state/attempt/next time/闭集 reason，无 payload、SQL、原错误或身份凭据。cursor 复用 D04 已验证 HMAC keyring，绑定 query kind、scope、稳定 user_id、规范化筛选、排序及水位，不能跨 Project/换筛选复用；匹配摘要不绑定 page size/limit，同一 cursor 可改变 limit，每次仍须为 1..100。详情查询不能用可猜 DeliveryID 绕过 scope。

`diagnostics.go` 仅给既有 DiagnosticsPage 增加必有的 typed Summary，QueryDiagnostics 方法签名不变。最小载体如下，字段是公共安全投影，不含 payload/原错误；各数值必须通过既有标量校验。

```text
DiagnosticsPage {Items: []SafeDelivery, NextCursor: string, Summary: DiagnosticsSummary}
DiagnosticsSummary {
  Window: DiagnosticsWindow, AsOf/WindowStart: foundation.Instant,
  WindowMS: foundation.DurationMS,
  PendingEvents/RetryWaitDeliveries/FailedDeliveries/DeadLetterDeliveries: foundation.Progress,
  OldestPendingAgeMS: *foundation.DurationMS,
  HandlerLatency: []HandlerLatencySummary, EventThroughput: []EventTypeThroughputSummary,
  HandlerLatencyTruncated/EventThroughputTruncated: bool,
  RecentErrors: []DiagnosticSafeError
}
HandlerLatencySummary {HandlerID, Count/Unknown: foundation.Progress,
                       SumMS: foundation.DurationMS, MaxMS: *foundation.DurationMS}
EventTypeThroughputSummary {EventType, ProducedEvents/SucceededDeliveries: foundation.Progress}
DiagnosticSafeError {At: foundation.Instant, DeliveryID, AttemptID?: AttemptID,
                     HandlerID, EventType, Reason: SafeReason}
```

HandlerID/EventType 用既有 event.StableName，ID 沿现有专用类型。Window 仅为 `5m|1h|24h`，省略时规范化为 5m 后参与 cursor 筛选摘要；其他值拒绝。Summary 的 AsOf 来自本次 DB 读取时间，WindowStart=AsOf−Window，WindowMS 固定为 300000/3600000/86400000，区间为 `[WindowStart,AsOf)`。各 Summary 分量从同一个 DB statement 的 MVCC snapshot 取得；每页重新读取并返回真实 AsOf，不持有跨页事务/持久 snapshot。Items 的既定 cursor 水位独立于本次 Summary，不用新 AsOf 改写旧 cursor，也不从当页 Items 推算总量。

Scope、HandlerID、EventType 精确筛选同时作用于 Items 和全部 Summary；Project 只算该 Project，System 只算 System，不把 Admin 查询解释为全租户。`DiagnosticsFilter.Phase` 只筛 Items，Summary 始终展示匹配 scope/handler/type 的各状态；PageRequest.limit/游标位置不裁剪 Summary。Window 规定 Summary 活动统计区间，当前 backlog 不受窗口起点裁掉；Items 仍按原筛选/水位分页。无 handler 筛选时，产生 Event 数包括无订阅的 Event；指定 handler 时，只统计存在该 handler delivery 的 Event，并按 EventID 去重。

| Summary 字段 | 00008 既有事实与计量口径 |
| --- | --- |
| PendingEvents / OldestPendingAgeMS | 当前至少有一条匹配 delivery 为 pending/processing/retry_wait 的不同 EventID 数；age 是 AsOf 减最早这类 Event 的 created_at，非 next_attempt_at，也不因多个 handler 重复计数。无 pending 时 age 为 null；旧积压即使早于 WindowStart 仍计入 |
| RetryWaitDeliveries / FailedDeliveries / DeadLetterDeliveries | 当前匹配 delivery 各 phase 的行数，不是重试 attempt 累计次数，也不包含其他 phase |
| HandlerLatency | 对 started_at 在窗口内的匹配 attempts 分 handler；有真实 handler_returned_at 且时间有效的行计 Count，逐行 `handler_returned_at−started_at` 向下取整毫秒后求 SumMS/MaxMS。它是 claim 至 callback 返回延迟，不伪称纯 handler CPU 时间；没有实际返回时间（含运行中/崩溃无法还原）的行计 Unknown，不进入 Count/sum/max；commit unknown 不抹掉已经持久的真实返回时间 |
| EventThroughput | 每 event_type 的 ProducedEvents 为窗口内 events.created_at 的不同 EventID 数；SucceededDeliveries 为窗口内 processed.processed_at 的不同 DeliveryID 数，跨 handler 可大于 Event 数，重投/重复核实不重复计 marker。只对当前仍保留且匹配 scope/handler/type 的事实统计，不重建已清理项目历史 |
| RecentErrors | 匹配 deliveries 中当前持久 safe_reason 非空、last_at 在窗口内的最近 20 条，按 last_at DESC/DeliveryID DESC；At=last_at，AttemptID 仅有真实 current_attempt_id 时给出。表示每 delivery 最近持久安全理由，不宣称完整错误历史；Reason 只能是既有 SafeReason 闭集 |

零尝试 project_stopped 计入 DeadLetterDeliveries，且自身不贡献 PendingEvents；同 Event 的其他 pending delivery 仍按原规则计入。它没有 attempt，因此 latency 的 Count、Unknown、Sum/Max 均不因它增加，也不产生成功吞吐；最近安全理由可显示 project_stopped，但 AttemptID 必须为空。显式重投后仅当前 phase 统计改变，直到真实 claim 才出现 attempt/latency 事实，不修改已验 DiagnosticsSummary 载体。

Count/Unknown/各总数用 foundation.Progress，所有时长用 foundation.DurationMS，JSON 均为规范十进制字符串，不能转浮点计数。无样本时 Count=0、SumMS=0、MaxMS=null；Unknown 明列，不能把未知时长放成 0 样本。已知不足 1ms 的样本可为真实取整 0。平均值分母为 Count，Count=0 无平均值；吞吐分母为明确的正 WindowMS，不用进程 uptime/页长或返回 float/NaN。COUNT/SUM 转换前检查 MaxInt64，负时差/结构损坏/溢出拒绝，不截断或伪空成功。

HandlerLatency/EventThroughput 分别按稳定名称排序、最多 128 行；超过时对应 Truncated=true，行内统计仍完整、总计不裁剪，指定单 handler/type 可查询该项。空集合返回 `[]`，空数据库是已知零而非 unknown；RecentErrors 的 20 条上限不受 PageRequest.limit 影响。统计沿现有索引和同一总计 2s 上限（更短 parent 优先）执行；超时/DB 失败不返回部分 Summary 或成功零值，不扫描/返回 payload，真实 fixture 验证计划与预算。不加统计表/迁移或跨页缓存；匿名 `/diagnostics` 只增加组件可用状态，HTTP 租户诊断绑定仍留 D07/D27。

## 9. Project 停止、显式清理与 bootstrap

实现 D01 `ProjectLifecycleParticipant` 的 Outbox 适配器：Name 固定 `outbox`，Project scope 的 RequestStop/InspectStop/Cleanup 使用真实 D08 LifecycleCause（operation_id/action/project_version）和注册 project-lifecycle actor，分别携带 §3 的 stop/inspect/cleanup step。调用在 Tx 外预收集依赖，事务内当前校验并按 Project EX → Outbox records 锁序；不从可见名称找 Project，不接受 caller 声称已删除。Meeting scope 未绑定相应来源规划则明确拒绝，不假报全 Meeting 已清。

archive：阻止/取消 domain_ingress 的新业务入口，实际在途需 join 后才报告所需 business stop=stopped；从未 claim 的该类 delivery 直接置 dead_letter/project_stopped，不造 attempt。Project EX 与该批 Delivery EX 下重读当前正式 cause、注册 handler 的 domain_ingress effect、原 pending/current_attempt_id=NULL/fence=0 和两尝试计数为 0，才更新 phase/reason/version/last_at；新 CHECK 不替代这些跨表校验。claim 若先取得 gate 并提交，停止方必须处理真实 attempt 并等终局，不能按未开始覆盖。已有真实 attempt 的未运行待投项沿原终局规则保留 attempt/fence，不把历史改成零尝试或启动旧业务。canonical_converge 仅按正式 gate 收敛已提交事实，可在 archived 后继续；不将这些投影写等同执行复活。Restore 只恢复未来合法准入，不能自动重开已终局 delivery/旧 execution；另行显式 Human 重投遵循 §8。

archive checkpoint 仅对应其精确旧 operation/project_version，不是永久关闭标志。Append/claim 在同一 Project 锁下由正式 provider 确认当前 active 且版本更新后，允许新 Event 和新 delivery；不因旧 archive checkpoint 仍在就继续拒绝。迟到旧 archive cause 不匹配当前 operation/版本，不能重新关门；Restore 不自动重开旧 terminal delivery/旧 execution。无需新增 Restore 公共口。此规则绝不重置 delete 的 stopping/cleaning/completed：即便请求声称 active/新版本，本域删除 gate 仍不可逆；当前 cause 和 checkpoint 均锁后重查。

delete 分两阶段。RequestStop 在 Project EX 下验证 D08 已持久 deleting gate，写本域 `stopping`，停止新 claim/requeue/业务回调；其他参与者仍可在当前 D08 Lifecycle/Converge 授权下把真实停止/失效事实与 Event 原子 Append，积累为待清理 delivery。普通 mutation 不能借此进入，也不因 Outbox 已 stopped 就声称所有事件生产者已停止。停止本实例回调时不持 DB Tx 等待；外实例仍须 exact Process 死亡证据 + 同 Delivery EX 终局核实，不能按超时强行删行。

InspectStop 只在该 Project 所有已 admitted 回调已 join 或精确旧实例已死且各原事务终局已核实后返回 stopped。跨实例未知不能给成功报告；不影响其他 Project 的恢复。D08 负责在其他 Project 事件生产者的停止/清理阶段收束后，最后调用 Outbox Cleanup；`ProjectAuthority.ValidateInTx` 在 Kind=lifecycle、LifecycleStep=cleanup 时必须读取当前 operation/participant 进度验证这一前提，不能信任旧 cause 或 caller bool。stop/inspect 不加此其他参与者前置条件，否则 Outbox 自身停止会等待尚需产生停止事件的参与者而循环；它们仍逐次验证当前 Actor/cause、Project gate 和本域事实。

Cleanup 一次 Project EX 下重查当前 cause、阶段和终局事实，再将本域置 `cleaning`，原子封闭所有 Append；此前持 SH 的 writer 先真正 commit/rollback，新旧 producer 此后都不能补事件。每批最多 100 个 Event，在 Project gate 与全部该批 record locks 下删除 attempts/processed/deliveries/requeue_commands/events 并持久 checkpoint；公平轮转受阻批次，不能因前缀受保护永久跳不到后续对象。pending/failed 可清；processing/unknown 未终局不删 marker，不得允许迟到业务写入。

最终同 Project EX 再查本域所有 payload/attempt/delivery/marker/command 行为 0，才给 completed；仅留 ProjectID、operation/action/version、终态及完成时间组成的技术 receipt/拒复活 gate。重试仍在锁后核当前正式 cause/receipt/阶段；Project 主行已删时由 D08 验证其最小完成 receipt，旧请求自带 cause 不作授权。它不保留名称、Actor、事件正文、Audit 副本，不占项目名称；D08 的原 Owner 当前 Session 结果查询仍走其正式删除 receipt。迟到旧 Append/claim 在看到原 Project 已删或本域 gate 后拒绝，不能通过 upsert 重建项目数据。System scope 和其他 Project 不清理，无任何自动 TTL。

其他域各清自己数据；Outbox 不直接删 Timeline/Inbox/知识/业务表，不用事件历史恢复 canonical。D24/D25 晚注册获得 Registration 的 per-type 边界/是否首次建立，然后在**注册 Tx 之后**建立新 generation、扫描 canonical；durable 新 delivery 在此期间继续保留/处理为 dirty source IDs，包含来源删除。完成基线后逐个重读 dirty、版本保护写入并原子切换；具体 generation/dirty/切换 Tx 由该投影模块所有。

回调启动前投影模块必须准备好可持久接收 dirty 的阶段；没有 generation 时可保持 delivery pending/retry，不能 ack 丢事件。D06 不将 Register 成功报告为 projection ready，也不提供回放旧 event payload 覆盖 canonical 的 API。W40 的注册晚提交、扫描期间删除、v43/v42 和 siblings 分别验证，不用单个顺序样例替代。

## 10. Central 启动、健康和真实停机

```text
Outbox.Initialize(ctx) / Check(ctx) -> error
Start(ctx) -> error                 // 单 worker owner；重复启动拒绝
StopClaims()                       // 停新 handler 准入，不截断在途业务 Tx 的 Append
Drain(ctx) -> error                 // 等本实例全部 admitted callbacks/Tx/worker 真 join
Force(ctx) -> error                 // cancel + 同 deadline 有界 join，未知不报 drained
Joined() -> bool                   // 私有状态事实的安全投影，非 caller 声明
```

DB Initialize 后沿既有**共享 30s** security startup budget（更短 parent 优先），先完成 D04 和 D05 Runtime.Initialize/guard.bind，再初始化 Outbox；不为 D06 重开 30s。核实 00008 及 00009/schema/sequence 配置、真实 PG 查询、持久注册声明和必要恢复事实，再注册本进程 catalog，最后启动 dispatcher，检查成功后才 bind HTTP。零 catalog/空持久数据是合法技术空态；已存 handler 与当前绑定不兼容/缺 provider 的待执行数据必须明确 unavailable/pending，不静默 ack 或忽略必要恢复。

Initialize/Recover 至少真实检查所有既有 processing/unknown 的归属与 checkpoint（分页且共原预算）；预算内不能完成必要安全检查则初始化失败。外实例仍活/无法证明死可保留明确 unknown 并禁该项新执行，不以队列非空伪失败；结构损坏、缺必要绑定、不能核实 DB 则 unavailable。catalog 没有业务消费者不代表产品 ready，`/readyz` 仍 503。

app/object.go 的 initializeObjects 改返回 app 私有 assembly，保留既有 ProcessID、Service、ProcessGuard、Runtime，而不是只返回静态 runtime 接口；assembly 实现现有 objectStorage，并向 app/outbox.go 映射 D06 ProcessAuthority。须在 guard 真 bind 后才允许 ConfirmStopped。无 D05 新公开补口、无第二 Process 死亡规则；精确本地 host/deployment/spool/file/flock/boot 证据沿已验实现。

assembly 同时成为共享 guard 的唯一 cleanup owner，并关联本实例 Outbox join 状态。初始化 D05 失败、Outbox 构造/注册/启动失败、startup context 取消、resources.addObjects/addOutbox 晚到和 force 已开始的路径，都先登记/保留该 owner，再处理返回错误。Outbox 构造不自行启动；资源登记之后才能准入回调，注册/启动与 StopClaims 在同服务状态锁下线性化。不能在某条早退路径把仍可有 handler 的 guard 交给裸 Runtime.Force。

Start 的 worker 用独立 serving context，不继承随后 cancel 的短启动 context；worker、回调和 recovery checkpoint 全部登记在同本地 admitted/join 体系。每个 Service 只启动一个调度 owner，Start/恢复入口不能制造未登记后台 goroutine。停止接新 handler 与关闭 Append 分开：首信号 StopClaims，HTTP 停新连接并允许已接业务 Tx drain；这些 Tx 的 Append 仍合法，提交后留 pending 给下次启动。

正常 drain 共用现有首停 deadline：HTTP/producer 与 Outbox 在途先结束，**Outbox 真 join 后**才允许 Object Runtime.Drain 释放 ProcessGuard；对象/Secret/网络自己的 drain 沿现有顺序，DB 最后。不要求把全库 pending 投递完，不在第一信号取消所有 HTTP BaseContext 假装 graceful。

第二信号/超时沿既有**共享额外 1s** force：先取消并尝试 join Outbox；若未真 join，assembly 只调用现有 Object **Service.Force(ctx)** 关闭双 Transport/本地 I/O，禁止 Runtime.Drain/Force/guard.Close，保留 guard FD 至真 join 或真实 OS 退出。Outbox 已 join 才可 Runtime.Force 完成 guard。所有资源共享同 ctx，不串行各续 1s；DB ForceClose 最后真实发起，ctx 已过期也不跳过。API 返回 timeout 不等于 handler 已停，健康/日志不可记录 drained=true。

健康采样与 DB/object 在同**2s** round 内完成，周期沿既有 10s，超过 **20s** 样本陈旧即 unavailable；Check 真实核 PG/control/worker（应运行阶段），不用 pending=0 判健康、不按读取缓存时刻刷新采样时间。匿名健康只给组件状态和固定 reason；业务诊断沿 §8 当前授权。没有新增必填 env、自动重放开关或跳过 DB/MinIO 的开发成功分支。

## 11. 可执行验收与未绑定责任

验收由实现者与独立 V 在授权后执行；本设计阶段不运行 Go/数据库/Docker。普通测试不启动外部资源，真实组用既有 nonce/labels/精确 ID 验证的 owned PG17.8/vector0.8.1、PG16.12 负例和固定源码 MinIO；不能连接现有库、生产凭据或跳过必需 fixture。

| 场景 | 必须观察到的结果 |
| --- | --- |
| T01 原子发布 | 核 event/contract 仅 foundation、上层 outbox/contract 仅允许的契约依赖；真实业务 fixture 修改+多个事件+对应 delivery 同 Tx，rollback 全无/commit 全有，未 commit 不调用 handler；同 ID 同义/异义及 §2 gate 优先级 |
| T02 注册晚提交 | A 已 SH/nextval 未 commit，B register EX 真阻塞；A commit/rollback 后 B 保存正确边界；B 后 producer 必建 delivery；sequence 回滚空洞/删除历史不使边界倒退；重启注册不换边界 |
| T03 一次完整锁 | 缺 barrier/Actor/Project、弱模式、错 Store/失活/并发 Tx 均拒绝且 poison；验证口不取锁；D05 object plan + Outbox extra 合并真实通过，无逆序补锁/SH 升级 |
| T04 独立与顺序 | foundation.Sequence optional/十进制字符串、1/MaxInt64 边界，0/负数/溢出/JSON number 拒绝；两 handler 一成功一失败互不拖住；v43 先于 v42、同 version 两 event、缺 sequence 重读 canonical；同 event 再投只一个领域结果和 marker |
| T05 回滚与 unknown | handler 写后 Retry/DeadLetter/panic 全回滚，再独立 checkpoint；真实代理丢 COMMIT 响应覆盖 committed/未提交/仍挂起，旧 writer 锁未终局不能因 SELECT 空而重做；Fault=nil unknown 同样处理 |
| T06 claim 与终局 | claim commit unknown 不执行；Apply unknown marker 核实不二次调用；超时但 callback 尚活不释放 slot/fence；SIGKILL 后 exact guard+同 Delivery 锁核实，跨 host/未知/活实例不能偷 claim |
| T07 恢复公平 | 前 64 项长期被保护，第 65 项仍推进；一个 handler 持续失败其他正常；foreign live/unbound 项不阻断本实例可收敛项；次数/退避/dead-letter 无忙循环 |
| T08 重投/Audit | 当前 Session/Owner/Admin 边界、归档 gate、同 key 异义/旧 version 成功重放、Audit 失败整体回滚；archive→Restore 后旧 checkpoint 仍在，新 Event/delivery 正常，旧 archive cause 不能再关门，旧 terminal 不自动重开。无 attempt 的合法 Human 重投只增 cycle/version，首个真实 claim 才有 AttemptID/fence1；Audit/commit unknown 不增伪 attempt。RequeueTarget 跨 scope/Actor/目标 binding 拒绝，Requeue 的旧 Delivery/其他 variant 的 Requeue 必须为空，callback 零 AttemptID/fence 仍拒绝；succeeded/processed 不重做，旧 Audit 兼容 |
| T09 Project 删除 | lifecycle step 必填/闭集，其他 variant 不准带 step；跨 step/Actor/cause 的 Dependencies 复用拒绝。fixture provider 允许其他参与者未完成时合法 stop/inspect，仅 cleanup 因未收束拒绝；错误 Actor/cause 和未绑定 D08 均拒绝。stopping 中真实停止事实仍原子 Append、普通 mutation 被拒；其他生产者收束后 cleaning 在 Project EX 封闭 Append，旧 Event 尚存时同 ID 同义 Append 仍拒绝；active/新 version 不能重置删除 gate；与 writer/handler commit 竞争、旧实例/unknown、分页前缀阻挡、旧 cause/receipt 重试、迟到回调均安全，最终清零且不复活，其他 Project/System 无变动 |
| T10 canonical bootstrap | 晚注册扫描期间新事件/来源删除均进入 dirty，旧 baseline/v42 不覆盖新 canonical；无 generation 时不能 ack；不从历史 payload 重建已删源 |
| T11 真实 Central | 必需迁移/存储/绑定失败不 listen；空 catalog 机制可用但 ready503；30s 共享耗尽/2s 采样/20s 陈旧、首停在途 append、第二信号与共享 1s、DB 最后关闭 |
| T12 guard 全路径 | 初始化对象失败、Outbox 注册失败、启动取消/晚返回；未 join handler 时强停只 Service.Force、另一进程 ConfirmStopped 仍拒绝；真 join/实际退出后才可恢复；无多 worker/泄漏 |
| T13 安全诊断 | 多 handler 同 Event 去重、窗口外旧 pending age、retry/failed/dead_letter、已知与 unknown latency、无订阅 Event/多成功 delivery 的吞吐、最近安全理由均与真实事实匹配；空库/null/零、毫秒与窗口边界、计数超过 2^53/溢出拒绝、128 行截断和 2s 超时不伪成功。scope/handler/type 同时限定 Summary，其他租户绝不混入；Phase 只筛 Items，limit/页位置不裁剪总计，本次 AsOf 更新而旧 cursor 水位不改。cursor 跨 Scope/主体/筛选失败，同 cursor 改 limit 可继续且逐次限 1..100；payload/原 panic/SQL/凭据不出日志和 HTTP |
| T14 零尝试停止与升级 | fresh/00008→00009 的真实升级保留旧 pending/claimed 数据且 00008 SHA 不变；新合法分支通过，NULL/错误 reason、System、非零计数和其他无 attempt phase 拒绝；故障注入证明两个 ALTER/迁移进度原子回滚。真实 archive 与首 claim 竞争按锁胜者收敛；未 claim 的 domain_ingress 终态无 attempt/processed、无未知 latency 样本、无自动恢复/Restore 重开，canonical_converge 不误终止；显式重投后未 claim 再停止仍合法，正式 cleanup 可清除且无迟到复活 |

预期命令（实现授权后执行并记录真实结果）：

```sh
AGENTEAM_GO=/workspace/toolchains/go1.27.1/bin/go sh scripts/check-go.sh
GOTOOLCHAIN=local /workspace/toolchains/go1.27.1/bin/go test -count=1 ./internal/central/event/... ./internal/central/outbox/... ./internal/central/postgres/... ./internal/central/audit/... ./internal/central/app/...
AGENTEAM_GO=/workspace/toolchains/go1.27.1/bin/go sh scripts/test-postgres.sh
AGENTEAM_GO=/workspace/toolchains/go1.27.1/bin/go sh scripts/test-security.sh
AGENTEAM_GO=/workspace/toolchains/go1.27.1/bin/go AGENTEAM_MINIO_BINARY=/owned/cache/minio sh scripts/test-objects.sh
```

`/owned/cache/minio` 必须替换为本次核过固定 checksum 的 owned binary；fixture scripts 纳入 `tests/outbox`，实际测试名称由实现确定并在交付列出，不能以命令示例宣称通过。B01 先跑其普通/真实 Tx+注册组；B02 跑受影响 lifecycle/故障组和无过滤兼容总组，独立 V 保留真实 fault/barrier 时序。

D07 绑定当前 Session/System，D08 绑定 Project/Owner/生命周期；D10–D24 各拥有 typed payload、ProducerAuthority、handler canonical/业务锁和事务验证；D24/D25 拥有 generation/dirty/bootstrap 与展示；D27 绑定鉴权 HTTP。D06 当前 Central 不注册未来领域事件/消费者、不读未来表；端口未绑定明确拒绝对应能力。单独完成 Outbox Project 清理不等于全 Project 删除验收。
