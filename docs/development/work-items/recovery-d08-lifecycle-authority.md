# D08 恢复：生命周期事实授权与只读停止查询

修订：rev2，仅同步阶段状态，rev1冻结API与行为不变。R3十六源已独立验收通过并由主线程采纳、提交推送`3e399c3044fbd256994ad4bb6f184a0a26787ea0`，远端一致。下文规格与静审记录保留当时事实，正式验收及后继边界见文末。

## 输入、完整结果与边界

固定输入为 R2 已验提交 `73db0d45d673f804a37a7232a79a611a1ef447aa`，其 15 源与原冻结清单一致；前置 R1 为 `98262b49aa87c52cfb0c09586dd1b76f9aa1fd52`，D05 新停止规格为 `70006762bd2b24f30ae075b335c17e644b623f3f`。原 R2 清单 SHA-256 为 `d2133560cd18b1560da9b703f7c2057f31c512d8beaeb8dca6b33cb7e4a81d8a`；实施不依赖临时清单存续，使用已提交源码核对。D05 活动 00016 候选不是本卡输入。

本块交付可供真实下游消费的 Project 生命周期事实 Authority：从现有 00013 持久行、冻结 manifest 与当前 pointer 验证服务 actor、原接受版本和停止阶段，实现正式 Object/Artifact stop 授权与 Outbox stop/inspect/actor 解析。并修复 Outbox 的必要只读接缝，使授权只读 Inspect 不会变成取消或 checkpoint 写入。必须用真实 PG 及真实 Outbox Service 验证；仅新增接口、构造器或 allow fixture 不满足交付。

本卡不推进 `accepted → stopping`，不生成 stopped 证据，不完成 archive/delete，不实现 Restore/Retry、Cleaner/finalizer、普通 Outbox delivery/requeue、Object/Secret Audit checker、Project HTTP 或生产 root 绑定。缺真实 Object/Artifact stop 的项目仍不能完成归档；这些限制不是生命周期最终产品语义。

正式规则引用 [D08 §8](d08-project-owner-design.md#8-生命周期编排)、[§9.1](d08-project-owner-design.md#91-d05-项目停止能力)、[§9.2](d08-project-owner-design.md#92-当前权限对象与-audit-分派)、[§9.3](d08-project-owner-design.md#93-outbox)、[R1](recovery-d08-b03.md)、[R2](recovery-d08-b03-acceptance.md)和 [D05 Object 停止卡](recovery-d05-object-stop.md)。不重定义公共 contract，不修改既有 SQL，不分配迁移号。

首次必读 [AGENTS.md](../../../AGENTS.md)、[团队流程](../agent-team/README.md)、[开发计划](../development-plan.md)、[设计技能](../../../.agents/skills/agenteam-design/SKILL.md)和 [Go 技能](../../../.agents/skills/agenteam-go-development/SKILL.md)；独立验收者另读[验证技能](../../../.agents/skills/agenteam-verification/SKILL.md)。工具及真实 fixture 沿[依赖恢复记录](../agent-team/dependency-recovery-2026-10-05.md)，资源须由主线程交接，不抢占 D05 验收。

## 构造 API 与已有接缝

以下新增 API 位于 `internal/central/project` 实现包；`pc`、`obj`、`ob` 分别指现有 Project、Object、Outbox contract。既有 `LifecycleFacts`、`AuthorityDependencies.Lifecycle`、`NewAuthority` 和 Object 的正式 `ProjectStopAuthority` 签名不变。

```go
type LifecycleAuthority struct { /* private immutable state */ }

func NewLifecycleAuthority(store Store, current pc.RequiredManifest,
    compatible []pc.ParticipantRegistration) (*LifecycleAuthority, error)

func (*LifecycleAuthority) ValidateLifecycleInTx(context.Context, foundation.Tx,
    identity.Actor, pc.LifecycleCause, pc.ParticipantName, pc.OperationPhase) error
func (*LifecycleAuthority) DiscoverProjectStop(context.Context,
    obj.ProjectStopRequest) (obj.AccessDependencies, error)
func (*LifecycleAuthority) ValidateProjectStopInTx(context.Context, foundation.Tx,
    obj.ProjectStopRequest, obj.AccessDependencies) (obj.ProjectStopAuthorization, error)
func (*LifecycleAuthority) Discover(context.Context,
    ob.ProjectRequest) (ob.Dependencies, error)
func (*LifecycleAuthority) ValidateInTx(context.Context, foundation.Tx,
    ob.ProjectRequest, ob.Dependencies) error
func (*LifecycleAuthority) ResolveLifecycleActor(context.Context,
    ob.LifecycleCause) (identity.Actor, error)
func (*Authority) ResolveLifecycleActor(context.Context,
    ob.LifecycleCause) (identity.Actor, error)

var _ LifecycleFacts = (*LifecycleAuthority)(nil)
var _ obj.ProjectStopAuthority = (*LifecycleAuthority)(nil)
var _ ob.ProjectAuthority = (*LifecycleAuthority)(nil)
var _ ob.LifecycleActorResolver = (*LifecycleAuthority)(nil)
var _ ob.LifecycleActorResolver = (*Authority)(nil)
```

构造只消费 Store 和受信任装配的版本声明，不调用参与者、不查数据库、不接收后构造的 Audit/Object/Outbox Service 或 `LifecycleRegistry`。`current.Require()` 及其 Entries 提供当前声明；compatible 只补保留的旧 `(name, contract_version)` 声明。同 name 不同 version 可共存，同 key 重复拒绝；复制所有 slices，使用已有 `normalizeLifecycleRegistration`，不复制 `RequiredManifest` 的完整图验证或 R1 的 adapter 解析。零值/typed-nil Store 明确 unbound；零值 Authority 不产生 grant。

每个 live operation 的整份冻结 manifest 先由 R2 loader 验 digest/图和全部 participant 行，再逐项与构造时元数据核对 name/version、OwnerModule、ReferenceKinds 集合及 CleanupAfter 集合。缺版本 unbound，元数据不一致 unavailable，不补当前版本、不缩减旧集合。构造输入不是实际参与者绑定；后续真实 R1 Registry 仍须独立 Resolve 同一 manifest，缺真实 adapter 不能推进。

装配顺序固定为：版本声明与 Store → LifecycleAuthority → `NewAuthority(... Lifecycle: facts)` → Audit/Outbox 等业务 Service；D05 S2 的 `object.Authorizations.ProjectStop` 直接绑定同一 facts；真实领域 Service 与 participant adapters 全部可用后再构造 R1 Registry 和 Project Service。无需可变全局 registry、后置 setter 或代理 allow。此顺序只解开生命周期 stop 环；§9.2 的 Object/Secret Audit checker 仍是后继依赖。

`NewAuthority` 对本构造器产出的具体 LifecycleAuthority 核同一 Store（复用 `sameStore`）；不接受零值或跨 Store 的具体实例。既有 nil Lifecycle 和其它既有 `LifecycleFacts` 注入兼容，原 `ValidateLifecycleInTx` 分派保留；其它实例不会自动获得新 Outbox 能力。`Authority.Discover/ValidateInTx` 只为 `LifecycleProject` 委派给同 Store 的具体 facts，已有 Project producer Append 路径保持；缺此绑定明确 unbound。新的 resolver 同样只委派该具体绑定。

## 当前事实、状态矩阵与错误

所有 live 许可均核：初始化已完成；Project 当前 operation pointer 精确相等；operation.project_id/owner 与 Project 相等；请求 action/operation/固定接受版本相等；manifest、全部 participant 行及对应 participant/version 完整有效。泛型 `pc.LifecycleCause` 没有 ProjectID，必须从已验证 ProjectLifecycle actor 的 ProjectID 解析并核真实映射。Object 两个 Component 都映射冻结的 `artifact-object` 条目；Outbox 映射 `outbox`，不能从请求选择任意 participant。

接受后的 archive gate 为 archiving、delete 为 deleting，Project.version 必须仍等于 operation.project_version。进度只改变 operation.version，不以它替代 cause 的固定版本。archive 完成要求 archived、原 pointer 保留、Project.version 精确等于 completed_project_version 且为接受版本加一。live Project 与最小 delete receipt 同时存在、字段/phase/gate 自相矛盾按不可用处理，不任选一份放行。

| 当前持久事实 | 泛型 `ValidateLifecycleInTx(StopPhase)` | Object Request / Inspect | Outbox Stop / Inspect |
| --- | --- | --- | --- |
| exact current accepted | 拒绝 | 均拒绝 | 均拒绝；接受不等于停止授权 |
| exact current stopping，所选 stop 行非 failed | 成功，仅证明当前 stop phase | continue / continue；Inspect 不创建新 cause | Stop 允许；Inspect 的非终态继续须额外 Stop 同 Tx 验证 |
| exact current cleaning（含 final stage）或尚未 Retry 的 failed | 拒绝 | Request 拒绝；Inspect read_only | Stop 拒绝；Inspect 只允许精确本域 terminal receipt，非 terminal 拒绝且零取消/零写 |
| archive completed 且 pointer/完成版本匹配 | 拒绝 | Request 拒绝；Inspect read_only | 同上，只允许原 cause 的本域 terminal receipt |
| Project 已物理删除，最小 receipt 精确匹配 project/operation/delete | 拒绝 | Request 拒绝；Inspect read_only | Stop 拒绝；Inspect 只允许精确本域 terminal receipt |
| Restore 已清 pointer、新 operation、错项目/actor/cause/version、无当前事实 | 拒绝 | 均拒绝 | 均拒绝 |

本卡泛型 CleanupPhase 和 Outbox LifecycleCleanup 明确 unbound，是尚未交付的清理授权；不得以表空、stop 完成或终态 Inspect 推导清理许可。后继清理块须实现 §8 的依赖图、Outbox/Audit 不可逆屏障和 finalizer，不能把该 unbound 当永久语义。

删除 receipt 没有 project_version/manifest，不新增字段也不猜测版本。Project 只认证已删 project/operation/delete 身份并授只读定位；Object/Outbox 必须自行核本域原技术 receipt 的 action/version。无本域匹配事实、错版本或非 terminal 不能成功或补建回执。已删普通 Get、Owner、活动刷新及结果可见性继续沿 R2，不因 resolver/Inspect 暴露 Project 内容。

`ResolveLifecycleActor` 在自己的短只读 Tx、Project SH 下重新核 current operation 或最小 receipt，确认事务 committed 才返回固定 `identity.ProjectLifecycle` actor。accepted 可以解析当前 actor，但没有 stop 许可；live 解析仍核冻结版本声明。terminal actor 只用于上述有限 Inspect。任意输入 UUID、语法合法 actor 或 Registry 条目都不构成当前事实。Unknown 返回原有 Commit Unknown，不返回 actor 或授予取消/写入资格。

错误沿现有 Fault 闭集：无配置/缺兼容声明/未交付 cleanup 为 DEPENDENCY_UNBOUND；非法输入为 INVALID_ARGUMENT；错误服务职责、项目/actor/cause 或伪造计划为 FORBIDDEN；真实当前阶段不允许为 INVALID_STATE；损坏行、manifest/元数据矛盾或 Store/锁校验失败保守拒绝。数据库不可用不变成不存在，错误中不含 SQL、名称、对象 key、payload 或未清洗原因。无需为此新增错误码。

## 事务、计划与身份边界

- Validate 全部消费调用者同一活 Tx，以本 Store.InTx 确认归属；先 RequireHeldLocks，再重读本域事实。不 Acquire、不嵌套 Tx、不创建 command/operation、不做外部 I/O，也不取消 handle。Project SH 足够；已有 EX 可覆盖。真实领域调用者在 Tx 外汇总 Project 依赖和本域资源锁，每个物理 Tx 恰好一次 AcquireAll。
- Object Discover 使用 `ProjectStopBinding(request)` 和唯一 Project SH 构造完整 AccessDependencies；这些可公开重建的数据不是授权。Validate 重算同 request 的预期 mapping/规范 locks、完整核等，再核已持锁与当前事实，最后返回绑定同 Tx/request/deps 的 `ProjectStopAuthorization`。额外乱塞锁、少锁、换 Component/Step/版本不能复用计划；消费者的完整 union 可包含其它域锁。
- Outbox Discover 只处理 LifecycleProject，使用现有 `LifecycleBinding`、本实例私有 PlanIssuer、唯一 Project SH，Opaque 为空。Validate 同时核 issuer、完整 binding、预期规范 locks/空 Opaque 和当前事实；不得把别处构造的 Dependencies 当可信计划。两类 Discover 均只做规划，不缓存一次成功授权，不要求 Request 的 phase 在规划时已经获准；最终权限只由锁后 Validate 决定。
- 普通 Human 继续当前 Session、真实 Owner、User SH 与 Project gate；系统管理员没有旁路。生命周期 Service 不冒充 Owner，不需要 User 锁，不调用 Touch，不得到普通 AuthorizeProject 的 Read/Mutate grant。错误 ServiceName、Human、AgentRun 或跨 Project actor 不能进入生命周期分支。
- 版本、pointer 或阶段在规划后改变，下一 Validate 立即拒绝不再适用的权限；不能持旧 discover 结果继续。旧 cause 即使 UUID/manifest 完整也不能取消 Restore 后的新工作。

## Outbox 只读 Inspect 的必要窄修

固定输入的 [cleanup.go](../../../internal/central/outbox/cleanup.go) 中，`startLifecycle` 会让非 terminal Inspect 捕获并取消 callback，`progressLifecycleStop` 和 `stopReport` 会写 checkpoint/置 stopped。现有 contract 只有 error，没有 read_only grant。仅将 Project 的 terminal 矩阵返回 nil 会错授修改能力，因此本卡同时实现下列本域分流，不扩公共 contract。

1. `stopProject` 对 Inspect 先调用新包内 helper：以原 Inspect 计划，在一次 Project SH/完整依赖 union 的只读 Tx 验权限并读 exact 本域 lifecycle receipt。没有行或 cause/action/version 不匹配直接拒绝；stopped/completed 在确认提交后只读返回，零捕获、零取消、零扫描推进、零写，也不为这一终态分支额外 Discover Stop。
2. 原 Inspect Tx 证明存在非 terminal 原 cause 后，Tx 外才追加规划同 actor/cause 的 LifecycleStop 请求。私有组合计划保留两份完整 Dependencies；接下来的事务一次 AcquireAll 取两份依赖及本域锁的规范 union。不得在已拿锁的 Tx 内 Discover 或追加 Acquire。
3. `startLifecycle` 的 preflight、后续 gate Tx、`progressLifecycleStop`、`stopReport` 每次先 Validate Inspect、重读 exact 本域 receipt；若另一实例已使其 terminal，走零写短路。否则同一 Tx 还必须 Validate Stop，之后才能捕获 callback、继续 stop 或写入。Inspect 永不创建新 cause/gate。Project 仅给 Inspect、Stop 被拒绝时，本域非 terminal 直接失败，不能把空待办或零 count 投影为 stopped。
4. callback cancel 仍只在 preflight 确认 committed 后于锁外执行，仍只针对原捕获 handle/attempt/fence。preflight Unknown、Stop 规划失败、任一 Validate 失败均零 cancel/零写。禁止用背景重试替调用方补过被拒绝的授权。
5. Inspect 的后续报告事务也要重新检查 terminal 或双权限；不能先在 SH 事务看见 terminal，再在另一个事务无条件写扫描游标。原 RequestStop 的资格/取消/Unknown、actual join、精确进程死亡和原 writer 终局要求保持；Cleanup 的原控制流及权限不借此扩大。

新 helper 与组合计划是 Outbox 私有实现，不接收 caller 的 readonly bool，不消费 Project 表。Project 阶段变动发生在 Stop 规划后、首次继续授权前时，必须证实零 side effect；已经获准并提交的精确捕获仍遵守原取消协议，不能把规划本身当提交证据。

## 唯一待下发文件范围

主线程采纳且独立静审通过后，单一实现者拥有以下范围；本卡作者此时不拥有代码写权。未列文件保持只读，若真实实现必须扩大先报主线程。

| 范围 | 精确文件与限定 |
| --- | --- |
| Project 旧接缝 2 个 | `internal/central/project/authority.go`：仅 NewAuthority 对具体 facts 的同 Store/有效性校验；既有依赖/interface/Owner 行为不改。`internal/central/project/events.go`：仅 Authority.Discover/ValidateInTx 的 LifecycleProject 分派及相应注释/import，不改已有 append/Audit 语义 |
| Project 新源码 3 个 | `internal/central/project/lifecycle_authority.go`、`internal/central/project/object_stop_authority.go`、`internal/central/project/outbox_lifecycle_authority.go`；分别承载共同持久事实/构造、Object typed grant、Outbox 计划/resolver/Authority 委派 |
| Project 新单测 3 个 | `internal/central/project/lifecycle_authority_test.go`、`internal/central/project/object_stop_authority_test.go`、`internal/central/project/outbox_lifecycle_authority_test.go` |
| Project 新真实集成 3 个 | `tests/project/b03_r3_fixture_test.go`、`tests/project/b03_r3_authority_test.go`、`tests/project/b03_r3_object_stop_authority_test.go` |
| Outbox 旧接缝 1 个 | `internal/central/outbox/cleanup.go`：仅 lifecyclePlan、planLifecycle、lifecyclePlan.locks、startLifecycle、stopProject、progressLifecycleStop、stopReport，以及它们必要的 import/私有 helper 调用。`validateLifecycle` 仅允许复用其原验证，不改变其它调用者语义；不改 Cleanup、terminalProof、capture/cancel 的匹配规则、普通 delivery/runtime、SQL schema |
| Outbox 必要新 helper 1 个 | `internal/central/outbox/lifecycle_inspection.go`：前置只读 receipt、双计划 union/继续授权与 terminal 短路辅助；不改公共方法签名 |
| Outbox 新验收文件 3 个 | `internal/central/outbox/lifecycle_inspection_test.go`、`tests/outbox/lifecycle_inspection_continuation_test.go`、`tests/project/b03_r3_outbox_inspection_test.go` |

只读复用 R1 `participants.go`、R2 `lifecycle_store.go` 的 loadLifecycleOperation/loadDeletionReceipt、repository.go 的 loadProject/projectLock/readCause、service.go 的 sameStore 及既有 fixture。R2 commands/receipt/事件计划、Audit checker、Object/Artifact/Secret 实现和 D05 00016/S2 范围不改。若 loader 的真实缺陷影响授权，报告精确失败输入并申请窄授权，不能复制宽松 loader 绕开。

## 验收门槛

以下测试名为新增顶层选择器；每项都要断言错误/事务与副作用，不能仅核 helper 返回值。授权阶段 fixture 可在测试拥有的 PG 数据里布置停止/完成事实以验完整矩阵，必须标注为 Authority 输入，不能称实际 runtime 已把操作推进或领域已停止。

- `TestLifecycleAuthorityConstructionAndCompatibility`：无 Service/Registry 也可构造；nil/typed nil/零值/跨 Store 拒绝；构造输入和返回锁 slices 的修改不改内部事实；当前与保留旧版本、无升级回退、相同版本元数据集合顺序等价而删减拒绝。旧 nil Lifecycle、B01/B02/R2 行为兼容。
- `TestLifecycleAuthorityPersistedCauseMatrix`：通过真实 R2 BeginArchive/Delete 得到 accepted 后所有 stop 拒绝；随后测试拥有的 phase fixture 覆盖表中每行、初始化未完成、current pointer、owner/action/固定版本、completed_project_version、缺/额外 participant、损坏 digest/manifest；unsupported 版本不能因表空通过。泛型 StopPhase 只在匹配当前 stopping 成功，CleanupPhase 明确 unbound。
- `TestLifecycleAuthorityLocksAndActors`：真实 PG 验漏锁、错 Project、其它 Store/已结束 Tx、规划后 pointer/phase 变化；Validate 不调用 AcquireAll/WithinTx；Service 无 User 锁可走正式生命周期口，但不能获得普通 Owner grant；Human/admin/AgentRun/错误 ServiceName 无旁路。Resolver 从真实行构造、Unknown 不返回 actor，任意 UUID 或已 Restore 的 cause 被拒绝。
- `TestProjectStopAuthorityCurrentAndTerminal`：Object 与 Artifact Component 的完整绑定、公开伪造 mapping/锁、换 Request/Inspect、跨 Tx/版本重放拒绝；stopping 返回 Continue，failed/cleaning/completed 返回 Read，仅 Inspect；最小删除 receipt 不伪造版本。返回 grant 必须 Matches 原 Tx/request/deps。此测试只验正式 Project 端口，不宣称 D05 Service 实际停止。
- `TestOutboxLifecycleInspectionAuthorityBoundary`：真实 Project Authority + 真实 Outbox Service + PG，覆盖 stopping 正常 RequestStop/Inspect；failed、cleaning、completed archive、删除最小 receipt 下 exact 本域 stopped/completed 只读重放。每种 readonly Project 状态同时测试本域缺行、错 action/version、仍 stopping，全部拒绝且零 cancel、零 row/version/cursor/attempt/claim 变化，不能由空 deliveries 推出成功。
- `TestOutboxLifecycleInspectionContinuationRechecks`：在 Stop Discover 后、首次双 Validate 前撤销 phase/pointer，严格零 side effect；两份计划完整 union 一次 AcquireAll；错 issuer、不同 request、漏一份锁或跨 Tx 拒绝。另一实例在初始只读扫描后完成时，允许 terminal 短路而不强索 Stop；在 progress/stopReport 前完成时同样零写。正常 Inspect 继续必须保持 real callback join，不以取消返回/空表/TTL 判停止。
- `TestOutboxLifecycleInspectionUnknownAndJoin`：真实 PG/受控 commit 边界和真实 callback，前置只读与双权限 preflight 的 committed/rollback/仍未决 Unknown 均沿原结果处理；Unknown 零取消，重试重新验证原 cause。原 RequestStop 有界等待、捕获精确原 handle、实际 join/原 writer、不同 Project 与 archive canonical_converge 兼容不回归。测试替身只用于正式边界的失败注入，不取代真实 Project/Outbox 正向组合。

实现者先运行新增单测、相关 `internal/central/project`/`internal/central/outbox` 单测、race、vet 与编译；真实 fixture 经资源交接后运行 `tests/project` 的 R3 选择器及 B02/R2 回归、`tests/outbox` 的生命周期/terminal/cancel/recovery 与新增选择器。按失败和实际改动影响扩展，不重复无关 MinIO 全树。Go 精确版本、fixture 所有权、命令/原始日志/固定源 hash 均交独立验收；不借本次通过宣称整个 D08/D05 完成。

## 后继的真实依赖

1. D05 S2 消费本卡真实 ProjectStopAuthority，将 Object 的登记、Request/Inspect、actual join 与恢复接到正式 facts；Artifact 仍须自己的真实 prospective/source 工作及 Object 组合。两者缺一不得报告 artifact-object stopped。00016 仅 schema 验收不能替代 S2。
2. 后续生命周期推进完整块消费 R1/R2/本卡与真实 participant adapters，交付持久 claim/fence、accepted→stopping、Request/Inspect checkpoint、Unknown 恢复及 archive 完成的原子 Event/Audit；Secret/Audit 的屏障型 Stop 只沿 §8 既定事实，不宣称停止 lease 消费者。未齐绑定时只可证明明确保留 pending/unbound，不能接受假成功适配器。
3. Restore 与 Retry 应随可恢复的真实生命周期推进结果成块交付：Restore 必须基于已完成 archive 和新版本，Retry 原 cause/phase 与原幂等规则。删除 cleaning/依赖图、Outbox/Audit cleanup barrier、finalizer/最小 receipt 作为后续完整删除结果，届时补本卡显式未交付的 cleanup 授权。生产装配和端到端终态需要全部真实绑定及独立组合验收。

当前检查仅规格静态检查；本卡不记录 Go/PG/MinIO 行为已通过。

## 独立静审与采纳

2026-10-05，未参与本卡编写的 `skill_verification` 对冻结 SHA-256 `b10c340a922dbe09d59f2ee224fc18974e02138e34509719f43209e23bd0f766` 完成独立静审，通过且无确定硬阻断。API 与不可变声明装配无构造循环；当前 cause、完整 manifest、版本和 Object read_only 矩阵可落地；Outbox 私有双计划完整 union、每 Tx terminal 短路及双验证可在授权窄范围内实现，无需修改公共契约、普通 delivery 或 Audit。15 个链接/fragment、输入指纹及空白检查通过，未运行产品测试。报告为 `/tmp/agenteam-d08-r3-static-krsf3jml/review.md`，SHA-256 `9fb1b7a0e6130b6ad898725bf70743dfcd575f8e4e3a8f1ada86781cbc5f01f9`；此处持久保留结论，不依赖临时文件存续。

实施须按本卡保留取消的准确边界：未获 committed preflight 前拒绝/Unknown 为零 cancel；已授权提交后的精确捕获仍沿原取消协议，不要求后续 gate 失败撤回此前合法取消。主线程采纳只更新状态与本记录，原行为要求不变；实现和独立真实验收仍待完成。

## R3 独立验收与后继交接

R3十六源已独立验收并由主线程采纳、提交推送`3e399c3`，精确匹配作者清单SHA-256 `d0e5fd1c03cf6b9a22c74ae299b324d983a537a90bab8dd329d79f572d8b25d9`。[正式报告](../agent-team/d08-r3-verification.md)复用作者pure race/vet/compile及修后Project 38顶层、Outbox 21顶层及包内2顶层通过，保留原始编译/fixture失败与修复；独立真实PG race 3顶层15子例通过（11.057s），两cmd构建及探针compile/vet通过，4容器3网络及所属进程清零。

本次通过范围为事实Authority、Object typed port与Outbox只读Inspect；callback实际join复用作者真实Outbox协议证据，本次不覆盖实际Object stop、生命周期推进、cleanup、普通delivery、HTTP或root。[Secret checker](recovery-secret-project-audit.md) `d57ce0b`作者8源已冻结待独立验收，真实新/旧组完成且4容器2网络清零；唯一fixture已由主线程交S2作者，Secret验收者先静审、等待窗口。Project+Secret绑定卡由`restore_test_dependencies`准备、未实施；后继按正式已验输入另行下发。
