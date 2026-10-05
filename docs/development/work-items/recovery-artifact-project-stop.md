# 恢复：Artifact 项目范围停止与真实 join

修订：rev3，追加真实反例与来源 hint、selector 分批验证的工程澄清，已通过本次限定 delta 复核并由主线程采纳；API、16路径、迁移范围及原预算不变。Object S2已完整独立验收；文末原采纳/交接记录保留当时事实，本轮定向修复通过不代表 Artifact 完整停止行为已独立验收。selector 修复按本次澄清在原16路径内继续，仍须最终独立行为验收。

## 完整结果与固定输入

恢复 Artifact 自身创建/上传的持久登记、目标与来源项目双向停止、真实取消/join 和同 cause 恢复，并通过既有 `oc.ObjectProjectStop` 委托 Object。真实 Artifact、Object、PG、MinIO 与 ProcessGuard 组合须能把本卡覆盖的工作收敛到 stopped；接口可构造、空内存 registry、已发送 cancel 均不算结果。

固定源码输入为 `431fb5320b801802762ada2c5aaf647ada24a355`。实际 Artifact 只有同步 Service、commands/upload_intents 和 SourceLease 内存表；`CreateFromSource` 在新工作登记前调用 resolver，`UploadPayload` 在 PreparePayload 前仅一次普通授权，没有项目 stop/work 或 producer join。Object 仅消费已提交 [C0](../../../internal/central/object/contract/project_lifecycle.go)、[SourceReads](../../../internal/central/object/contract/source_lease.go)、[AccessPlanning](../../../internal/central/object/contract/access.go)及已采纳 [S2 规格](recovery-d05-object-stop.md)，不读取活动 Object 候选推断行为已验。

正式规则沿 [D08 §9.1](d08-project-owner-design.md#91-d05-项目停止能力)、[D05 源与对象组合](d05-object-storage-design.md)、[00014](../../../db/migrations/00014_object_artifact_project_stop.sql)和 [00016](../../../db/migrations/00016_object_project_work_maintenance.sql)。00014 的 Artifact work 已能表达本卡 upload/creation、精确 process、目标撤销和来源撤销；00016 只扩 Object 维护工作。本卡不新增表/迁移，不占 00017，不改 C0、Artifact contract 或 foundation 锁框架。

必读 [AGENTS](../../../AGENTS.md)、[团队流程](../agent-team/README.md)、[设计技能](../../../.agents/skills/agenteam-design/SKILL.md)；实施者另读 [Go 技能](../../../.agents/skills/agenteam-go-development/SKILL.md)。真实资源沿 [依赖恢复记录](../agent-team/dependency-recovery-2026-10-05.md)，由主线程交接唯一窗口。

| 前置/后继 | 本卡准确边界 |
| --- | --- |
| 已提交 Artifact、C0、00014/16、ProcessGuard | 复用真实本域 command/source/receipt 与 flock 机制，不恢复丢失候选 |
| Object S2 | 实施前须固定完整验收提交并核正式 port 的 delta，尤其 SourceLease unopened/force、DiscardPrepared 后 verify/finishWriter 的整次 actual join、PrepareReadAccess。不能绕过未闭合缺陷 |
| Project R3 生命周期 Authority | 正式 `ProjectStopAuthority` 已有事实校验能力；本卡消费其接口。普通 Artifact Owner/Session/Agent/Project 路由和生产装配仍由后继 Project/app 块绑定 |
| Object Audit checker/Project AuditFacts | 保持 S2 正式 stop/Audit 调用；生产组合依赖真实 Object checker 和 Project 映射，不能在 Artifact 加宽松 allow。本卡不实现这些主体 |
| Cleaner、Project 生命周期推进/Restore/Retry | 本卡交付真实 Stop 库，不推进 Project phase，不执行 Cleaner，不替代最终删除。后继必须消费本卡及 S2 已验版本 |

## 构造与现有接口

API 位于 `internal/central/artifact`，保留原九参 `New`；新增可选构造及本域 producer 生命周期：

```go
type ProcessAuthority interface {
    oc.ProcessAuthority
    CurrentProcess() oc.ProcessID
}
type SourceAuthority interface {
    oc.ResourceAuthority
    oc.ProjectGate
}
type ProjectStopDependencies struct {
    Authority oc.ProjectStopAuthority
    Objects   oc.ObjectProjectStop
    Processes ProcessAuthority
    Sources   SourceAuthority
}
func NewWithProjectStop(
    Store, *OwnerProvider, oc.Objects, oc.Uploads, oc.Cleaner,
    oc.SourceReads, oc.SourceResolver, au.Appender, cursor.Keyring,
    ProjectStopDependencies,
) (*Service, error)
func (*Service) RequestProjectStop(context.Context, identity.Actor, oc.ProjectStopCause) (oc.ObjectStopReport, error)
func (*Service) InspectProjectStop(context.Context, identity.Actor, oc.ProjectStopCause) (oc.ObjectStopReport, error)
func (*Service) StopAdmission()
func (*Service) Drain(context.Context) error
func (*Service) Force(context.Context) error
func (*Service) Joined() bool
var _ oc.ObjectProjectStop = (*Service)(nil)
```

`au` 为 Audit contract。构造零 SQL/外部 I/O，固定依赖与 provider 选择，不增加可变 setter/router。原必需依赖及 SourceReads/Resolver 的可选性不变；新可选字段 nil 有下述语义，接口非 nil 而动态值 typed-nil 则 INVALID_ARGUMENT。零值 Service 的新增入口安全拒绝，不能 panic；错误沿现有固定 Fault，不带 SQL、payload 或未清洗错误。

- `Authority` 或 `Objects` 未绑定时 stop 返回 DEPENDENCY_UNBOUND，仍必须登记普通工作。配置 Object port 必须是所用真实 Objects 的同一装配，不能任意另一库的 stopped 报告；组合验收核实际对象/因果匹配，Artifact 不窥读 Object 私表。
- `Processes` 已绑定时构造只调用一次 CurrentProcess，Validate 后捕获不可变 ID；不另收可错配的第二个 ID，不随每次调用重取动态身份。CurrentProcess+ConfirmStopped 沿已提交 [Outbox 端口](../../../internal/central/outbox/contract/authority.go)的职责，Artifact 不 import Object/Outbox 实现。
- 原 `New` 等同于零 stop/process 配置。每个 Service 产生唯一的本地 instance ID，照常持久登记工作；该随机 ID 只是精确本地身份，不是 ProcessGuard 死亡证据。仅本 Service 持有的实际 join 可确认，另一 Service/历史 work 无证据则 pending；不能因无 provider 禁止旧合法普通操作，也不能推断旧进程已死。
- `Sources` 已绑定时复用其正式 ResourceAuthority+ProjectGate；nil 时仅实际 Artifact owner 交给已有 OwnerProvider 的这两个接口。其它 owner 缺前置权限依赖返回 DEPENDENCY_UNBOUND，不能假称 Service 原本就禁止所有非 Artifact source。

共享 guard 不会因为配置了 Processes 自动 hold。已提交 Object ProcessGuard 没有跨域 hold API；[组合根](../../../internal/central/app/resources.go)现有 producersJoined 等待 Account/Outbox 后才允许 Object Runtime 释放 guard。本卡暴露实际 Artifact producer join，后继 app 必须把它纳入同一个联合屏障；当前不改 app，不建 Artifact 独立 Runtime/claim/常驻 worker。组合测试使用真实 guard，并按同一规则先 Artifact join、再 Object runtime 关闭；不能让 fixture 提前释放 guard 后把活调用“恢复”为已死。

## 工作身份、准入与来源

1. `CreateFromContent`、`CreateFromSource`、`UploadPayload` 从入口、第一次 DB/外部调用之前登记本地 control，覆盖持久登记前窗口。真正新工作在任何 resolver/body Read/spool/GET/PUT/verify 前完成当前权限下的持久 work 提交确认；Unknown/NotCommitted 不开始这些 I/O。`BeginUpload`、`CreateFromUpload` 的本地 control、原 command/intent 短 Tx 与消费门禁也纳入生命周期；不为纯 DB 命令伪造外部 work。
2. work 每次物理调用生成独立 UUIDv7；`kind` 仅 upload/creation，`process_id` 固定本 Service 身份，`command_hash` 是原 CommandIdentity canonical 的 SHA-256。`resource_id` 为该调用真实已存或预分配的 ArtifactRef.ArtifactID，后续创建沿同一个 ref；不得先写虚假 source/sha 的 commands 行来绕过 resolver 前登记。并发同 key 的不同调用不能覆写彼此 work/process，原 native command 锁和语义冲突规则继续生效。
3. target Project 来自当前 Invocation/Owner 的真实授权。每次准入在同 Project SH 下读本域最后 stop epoch，保存其 version/operation，无旧 stop 则 `(0,NULL)`；它只负责技术 fencing，不替代当前 Mutate gate。work 的 id/project/process/kind/resource/command/epoch/source-project 一经登记不得改指向；相同 ID 重放须全身份匹配，PK 不能被当作 UPDATE 不变性的证明。
4. Source 在 resolver 前从合法 BusinessFileRef 映射正式 owner：ArtifactID、Knowledge DocumentID、Execution PayloadID 或实际 receipt.Owner；映射只用于发现。用 `PrepareReadAccess` 规划源的完整依赖，与目标 Mutate、原 Command、Artifact/work record 锁组成一次 union；Acquire、Validate 后，在同 Tx 用原 actor/owner/Read 同时调用 SourceAuthority.AuthorizeOwnerInTx 与 CheckInTx。必须核真实资源/父映射，不能只信 ref.ProjectID、目标 actor.ProjectID 或 plan.Validate。实际源 Project 才写 `source_project_id`，源 archive 仍允许 Read，deleting 拒绝。
5. resolver 返回后仍核完整 reference/owner/source/version/object，随后按原 `AcquireSourceAccess` 与目标权限，在同 Tx 保存 commands 原 source、真实 SourceLease 和对应 work.source_lease_id。lease 仅允许 NULL→本次真实 lease ID 或同 ID 重放，不覆盖旧调用；提交未知保留原 handle/Tx 身份且零 GET。OpenLeasedSource、Reserve、Publish/Consume 每阶段继续消费正式 plans/current permissions；同 Tx 再核本 work 与两个撤销字段，Restore 后也不得借旧 handle 开流/发布。
6. 原已完成 command 仍先当前目标可见性，再 target-only 重放；不得再 Resolve、读取已删源、创建新 work 或重复 I/O/Audit。未完成旧 command/UploadTarget 被撤销后，即使 work 已 joined 也不能恢复，原 key 不复活；resolver 前尚无 native command 时，按 command_hash 保留 revoked work 的 tombstone 并在后续新准入原 command 锁内检查。新 key 仍按当前 gate 正常处理。
7. 固定 store 没有 RequireHeldLocks，不扩 Store。包内私有计划/handle 绑定本 Service、活 Tx、完整身份/锁集合；任何原 writer Tx 与随后 checkpoint 必须共享稳定的 `RecordLock(ReferenceRecordLock,"artifact-work:<work-id>")` EX，native command 已存在时另带原 Command EX 与 `artifact:<id>` record。pre-resolver work 只有 command_hash，不能从 hash 伪造 CommandIdentity；其 work record 必须从第一登记 Tx 起就是原 writer 终局屏障。跨 Service/Tx、漏锁/映射变化拒绝，整 Tx 回滚后外部重新规划，绝不补锁或内层重开 Tx。

当前 Sources 的真实实现只覆盖 ArtifactFile 和 Artifact owner 的 UploadedObject；Service 的注入 SourceResolver 形状本来允许其它合法 ref。新增 Sources 是解除缺前置授权的正式接缝，不是永久收窄 source 种类。[D12 B02](d12-b02-knowledge-service.md)已规定 Knowledge Authority 实现 ResourceAuthority/ProjectGate/AccessPlanner，并在 Objects 后构造独立 SourceResolver；其 C1 cursor、C2 删除 Audit、C3 irreversible release 都不替代此授权。后继 D12 及组合 router 须配对绑定真实 Authority+Resolver+Object planner，D22 负责 Execution 来源；缺任一部分在 resolver 前失败。当前只验真实 Artifact 来源和通用 port 的严格拒绝/分派，不宣称 Knowledge/Execution 已实现。非 Project source 也不能凭空填 source_project_id/lease，需后继明确其真实来源保护接缝，不伪装成目标项目。

## Stop、双向关系与真实终局

Request 固定 ArtifactStopComponent，actor/cause、Continue/Read-only 矩阵完整复用 C0/D08，不另造权限。每个物理 Tx 恰一次 AcquireAll（有对象组合计划时走 AcquireAccessPlansInTx）；先收齐 Authority 依赖、本域 stop/work/native 行、目标及来源 Project、原 command/record 等完整 union。同 Tx 校验 Authorization.Matches、mode 和本域映射，不能借 Owner grant、ProjectCleanupAccess 或直接查 Project 私表授权 stop。

- 第一阶段只读短 Tx 核 exact 当前 cause 并捕获匹配的原本地 handle；只有 confirmed committed 后才可能进行后续动作。若需新建 stop gate/撤销，则另一个完整授权 Tx 先持久保存同 cause gate 与限定行的撤销；该 Tx Unknown 同样零 cancel、零调用可能继续取消的 Object stop。每个本地 cancel 只针对捕获且身份仍相同的 child handle，不能用全局 Force、按项目名重新宽扫或取消跨项目父控制轮次。
- archive 扫描目标 Project 的所有 upload/creation work、未完成 commands 和未消费 upload_intents。持久撤销未来 publish/attach/consume；保留 completed command、canonical Artifact/Object/reference/history 及合法读/下载。不可调用 CancelCreation/CancelUpload 的物理清理路径来替代项目 stop，不写 cleanup cause。已完成发布但仍在 source release/Discard/尾部 checkpoint 的调用仍须真实 join。
- delete 在上述目标侧之外，扫描 `source_project_id=Cause.ProjectID` 的入向 creation，并从旧 commands 的严格 source 解码补查遗漏 native 关系。锁 union 包含实际另一侧目标 Project/command/work，技术撤销只针对真实依赖该源的那次调用；不能给来源项目的 lifecycle actor 授予另一项目业务 Mutate/Read。源 archive 不撤销另一目标的合法 Read/复制；源 delete 才写 source_revoked_by。目标与源相同去重，多个源调用/相同 command 的不同物理 work 不相互覆盖。
- 等待均在 Tx 外。joined_at 覆盖整个目标调用；source_joined_at 保守覆盖该 creation 的全部源使用、最终 source Validate、SourceLease release 和整体退出，不能读完 bytes 就提前写。两侧分别保存原 cause，可被两个项目独立发现；一侧 join 不删除另一侧未终局关系，不擅自清除其它项目的 command。
- 实际 join 包含 resolver 返回、真实 body Read/Close、PreparePayload/DiscardPrepared、UploadPrepared 内 PUT+verify+finishWriter、source cancel/release、最终 publish/Audit Tx 与取消后的 DB 尾部。cancel 返回、wrapper.Close、PreparePayload 释放、HTTP 返回、ctx 到期或本地查不到 handle 均非证明。未知原 Tx 须通过原 writer 的共同锁确认终局；所有未终局 control/cleanup callback 仍阻止 Joined，不能在 public 调用返回时提前注销。
- 每轮先推进本域精确撤销/有限等待，再在 Tx 外调用同 cause 的 Objects.RequestProjectStop/InspectProjectStop；本域仍 pending 时也可调用它使 Object 收敛，不能互相等待才开始。Inspect 不创建新 cause；read-only 只调用 Object Inspect、零 cancel/新 gate/写入。Artifact 本域完整终局且 Object 报告匹配 ObjectStopComponent+完整 cause 并 stopped，才可写/报告 Artifact stopped。合并 refs 按 kind+ID 去重，Unknown 优先，不能以空列表、只有本域成功或缺 port 成功。
- cleaning/finalizing/failed/completed/archive 或删后最小 receipt 的 read-only Inspect，必须有本域完全匹配的 stopped receipt，并得到 Object 的匹配只读终态；缺行/非终态/版本不符返回不匹配或不可用，零 side effect。Restore、新 operation、错误 manifest/cause 在任何取消前拒绝；规划后撤权/换 cause 的竞态也必须零 cancel。

`project_stops.scan_kind` 的四类游标分给目标 work、入向 source work、native commands、native upload_intents，`scan_after` 使用各自真实 UUID（commands 用唯一 artifact_id）。每轮每批有固定上限，越过仍 busy/unknown 的低 ID，尾部重新检查未决项；游标推进不等于这些项已完成。停止成功必须完成所有适用集合及最后锁下反查，不能 LIMIT 前缀空/只有当前页为空就成功；stopped 清零游标。新物理 work、无 work 的未开 UploadTarget、只有技术行的实例均不能漏扫。

恢复只由调用者再次 Request/Inspect 驱动有界进展，本块不新增后台 worker。对旧精确 process，必须有本 Service 原实际 join 或注入 ProcessAuthority 的真实 exact ProcessGuard 死亡，加原 writer Tx 终局；无证据保持 pending/unknown，不 TTL、心跳、连接丢失或重造 UUID。复用 SourceReads.CancelSourceLease 原 handle；历史源 lease 的存在/退休只经 Cleaner.InspectReferences 与 Object 既有恢复判断，不构造新的 SourceLease、不直接改 Object 表，也不把“lease 不见”单独当 Artifact 整次 join。

旧 native commands 没 process，不能回填一个方便的 process 后声称已恢复。未开过 I/O 的 UploadTarget 在原 command/record 锁下撤销即可封闭未来入口；旧 incomplete creation 若缺实际原终局证据则保守 pending。当前生产 Artifact 未绑定，后继装配必须从 Service 构造开始覆盖登记，不能同库并跑旧未登记 producer。验收须区分确证已退出的历史 native 事实与仍未知旧调用，后者不可空成功。

## 全局 producer 退出与 Cleaner 后继

StopAdmission 只停止新的本卡 producer/control 准入，已准入调用仍可做必需 checkpoint/join；Drain 用调用者剩余预算等待真实已登记 producer，不立即 cancel。Force 停准入、取消原已登记调用并触发其必要 Close，再用同一预算等待；不合作的 body/resolver/原 writer 仍返回 busy，Joined=false，不另加每资源 timeout，不遗留脱离 registry 的关闭 goroutine。入口登记与 StopAdmission 原子互斥，覆盖尚未到 durable INSERT 的调用；正常尾部与 Unknown 原 writer 的控制记录也必须计入。共享 guard 释放的最终许可仍归后继组合根，普通读/下载继续由真实 Object/S2 producer 与外层调用所有者承担，不宣称新增了 Artifact 全域独立 runtime。

Stop 完成不是删除清理完成。本卡不改现有 cleanup.go，不删除 work/command/source facts、旧 archive receipt 或 Project 行，不调用 Cleaner.CleanupProject/Purge。后继 Artifact/Object Cleaner 必须在全部双侧实际 join 与既定清理顺序之后，清掉本域临时 work/扫描记录和旧 archive receipts，只留 current-delete 最小 stopped receipt；处理指向被删来源项目的入向关系时，不删除其它项目仍合法的 canonical/command。只读删后 Inspect 依赖该最小 receipt，但本卡不宣称已经实现真实删后终局清理。

## 候选实施范围：16 个路径

当前设计者只新增本卡；下表是静审采纳后由主线程另行授予实施者的唯一源文件范围。

| 类型 | 精确路径与范围 |
| --- | --- |
| 旧源 1 | `internal/central/artifact/service.go`：兼容构造、固定依赖/control 状态、within 的 work 锁/校验接缝；completed target-only lookup 保持 |
| 旧源 2 | `internal/central/artifact/create.go`：两创建入口整次 lifetime、resolver 前登记、预分配 ref、源 lease 与 work 同 Tx 绑定、所有 defer/尾部 join |
| 旧源 3 | `internal/central/artifact/upload.go`：BeginUpload/UploadPayload/CreateFromUpload 的准入、持久撤销与原 receipt/consume 检查；上传整次 lifetime |
| 旧源 4 | `internal/central/artifact/publish.go`：reserve/publish 原 Tx 的 exact work/cause fencing，不改原对象/Audit 原子顺序 |
| 旧源 5 | `internal/central/artifact/commands.go`：复用严格 native row 解码/原 CommandIdentity 构造供有界扫描，不改请求摘要、字段保留或完成重放语义 |
| 新生产 3 | `internal/central/artifact/project_work.go`、`internal/central/artifact/project_lifecycle.go`、`internal/central/artifact/lifecycle.go` |
| 新单测 3 | `internal/central/artifact/project_work_test.go`、`internal/central/artifact/project_lifecycle_test.go`、`internal/central/artifact/lifecycle_test.go` |
| 新真实集成 5 | `tests/objects/artifact_project_stop_fixture_test.go`、`tests/objects/artifact_project_stop_test.go`、`tests/objects/artifact_project_stop_source_test.go`、`tests/objects/artifact_project_stop_recovery_test.go`、`tests/objects/artifact_project_stop_lifecycle_test.go` |

owner.go、sources.go、read.go、query.go、cleanup.go、store.go、source_wire.go、旧测试、全部 Object/Project/Knowledge/app/Audit 实现、contract、迁移、脚本均只读。没有与 Object S2/Object checker 同源写入，但其实际 port/join 行为是硬前置；S2 通过后固定接缝差异，若需上述范围外修改先报具体证据，不能旁路权限或缩弱验收来守名单。

## 可执行验收与交付门槛

真实组统一 `TestArtifactProjectStop` 前缀，生产 Artifact+OwnerProvider+Sources+已验 Object/S2+SourceReads+Audit+PG/MinIO；测试专用上游只隔离尚未装配的 Project/Session/Agent 权限，须按正式接口真实 SQL 当前状态、完整 actor/cause/manifest、活 Tx 与已持锁校验，拒绝恒定 allow。已有 fixture 仅读复用，在本卡新 fixture 中组合严格新 port；不修改旧断言。实际进程死亡使用真实 ProcessGuard 与 kill+Wait，不用模拟 bool 证明 join。

| 组/测试文件 | 必须覆盖的反例与正常结果 |
| --- | --- |
| `artifact_project_stop_test.go` | Archive 正常 pending→stopped，保留 canonical/历史/合法 read/download，未开 UploadTarget 与上传 receipt 不再 consume；Delete 正常收束目标工作及 Object 真实 reader/source/unopened lease；Object pending/failed/unknown/unbound、错误 component/cause 不可 Artifact stopped；Continue/Read-only 全矩阵及无本域 receipt、非终态 receipt、删后最小 receipt；不存在本卡 Cleaner side effect |
| `artifact_project_stop_source_test.go` | resolver 前阻塞时 work 已确认且可被精确取消；登记 Unknown 零 Resolve/Read/spool/GET/PUT；真实来源 Owner/Session/Project 撤权和计划后映射变化拒绝；源 archive 允许跨项目复制，源 delete 停其精确 child、另一项目无关调用继续；双侧并发停止、同项目去重、多调用不覆盖；未 open source lease fencing；旧 key/旧 handle 在 Restore 后不复活，completed replay 不访已删源，新 key 当前授权正常 |
| `artifact_project_stop_recovery_test.go` | 真 COMMIT ACK 丢失分别命中 work INSERT、AcquireSource+command、最终 publish/Audit、stop gate/撤销及 join checkpoint；保留原 committed/rolled-back/仍未决三态和原 writer 锁，不将 admission Unknown 冒充 final Unknown；新 Service 对活 process/无 provider 保持 pending，真实 kill+Wait+flock 后同 cause 恢复；只有技术行、多页/低 ID 阻塞公平性、无 work 原生事实与未知旧调用；进程/资源/command/来源身份篡改拒绝 |
| `artifact_project_stop_lifecycle_test.go` | resolver/上传体 Read 阻塞、Close 返回但 verify/finishWriter 未退、SourceLease Cancel 未退、最终/取消后 DB 尾部及原 Tx 未终局时均不得提前 joined；StopAdmission 拒新但旧可收尾，Drain 不提前取消，Force 预算到期不假成功；真实共享 guard 在 Artifact 未 join 时仍 held；精确 stop 不 cancel 另一 Project；原 New/nil Processes 普通三路径/本地 join 正常、跨 Service 不假死 |
| 三个 unit 文件 | 可选构造 nil/typed-nil/零值、CurrentProcess 一次捕获、固定 Sources 分派与缺依赖零 I/O；原句柄/私有 issuer/完整 Tx/规范 union/不可变身份及 source lease 单赋值；report conservative merge/有界游标/control race。纯测试不替代 PG/MinIO/join 证明 |

接受 EX 未成功、preflight Unknown、stop gate Unknown、规划后撤销当前授权、旧 cause/Restore 的用例均须观察零 cancel、零 Object Continue 调用及零新 stop 副作用；不能仅断言错误码。final publication Unknown 必须命中含真实 Artifact/Object/Audit 写的原最后 Tx，并观察重复请求无重复 I/O/metadata/Audit。真实阻塞用确定性 barrier，保留原始红日志和触发位置。

兼容至少复跑旧 `TestArtifactThreeCreationPathsAndCompletedSourceReplay`、`TestArtifactSourceCurrentRevocationBlocksPublishAndRecoveryKeepsOriginal`、`TestArtifactSourceCommandUnknownAndOriginalFactRecovery`、`TestArtifactProspectiveCancelAndProjectCleanupDoNotRevive`、`TestArtifactPendingCreationCancelPreventsLaterPublication`、`TestArtifactAgentExecutionCurrentGateAndProvenance`，及全部受影响 Artifact read/list/download/cleanup/purge、Object SourceLease/Force/Runtime 和 S2 actual-join 场景。按固定输入与接缝冻结精确顶层集合，保留 Go 1.27.1、race/count=1/原每包 6m 等预算，可有依据分组，不能延时、弱断言或把 no-tests 当通过。

交付先完成相关单元/race/vet/编译，再在唯一资源窗口运行真实组及必要回归；冻结源清单、完整命令/原始日志/实际主子测试数，确认进程、容器、网络及 runtime 目录实际清零，交未参与实现者独立验收。生产 Project/app 路由与共享 guard 屏障、未来来源绑定、Cleaner 最终退休和 Project 全程推进仍是后继，不将本卡通过称作完整 D05/D08 已交付。

本次设计仅固定输入阅读与 Markdown 自查，没有 Go、PG、MinIO、Docker 或 Artifact stop 行为通过声明。

## 规格采纳记录

2026-10-05，独立 verification_worker 对原候选 SHA-256 `31d8a52df71ba4020a2dbc8a963b55c11239d55f6111e488a1ad13cf9a5c6d77` 完成静审，未发现阻止采纳的硬缺陷；49 项固定输入、16 个本地链接/fragment 及 16 个实施路径核对通过。主线程已复核构造兼容、来源权限、双侧 join、共享 guard 与 Cleaner 后继边界并采纳。来源权限实现须消费 `OwnerAuthorization.Matches(originalActor, sourceOwner, Read)`，错配 grant 必须在 resolver 前拒绝；这是正文精确权限要求的实现检查，不扩接口或文件范围。

审查报告 `/tmp/agenteam-artifact-stop-spec-review-t_nc4xj8/review.md` SHA-256 `fe4925a6fbbf2e14f84c023bce4a7113de4177afed2e2db63ca0b18c5e1deaeb`，检查结果 `checks.json` SHA-256 `5a956645340c60be7c8ef523d187ffbca27dc42c422439a8896b495dc147516b`；临时文件仅为本轮过程证据，本卡持久记录结论与门槛。没有 Go/PG/MinIO 行为验收声明；S2 完整独立验收及最终固定输入的接缝 delta 仍是开工前置，00017 未分配。

## 已验 S2 输入与实施授权

S2最终28源已独立25顶层43子例全部通过，提交推送 `6658a6cb1f29299521773bc8dc86b2f607b8c809`，主线程确认远端同SHA。实现者基于该固定提交核对原规格接缝，报告 `/tmp/agenteam-artifact-stop-author-y597q6vl/preparation.md` SHA-256 `df1e7ae8331e94da800203b4331a30b4cdb10047daed8f99b57d4efbe79cdf20`；主线程已阅读并采纳。原431fb53到该提交的Artifact包、Object contract与00014均未变，16文件范围仍成立，无新接口或迁移需求。

S2已提供同Tx SourceLease/work、confirmed Open、真实release/join和完整writer引用。Artifact必须继续跟踪整次control、失败release和未知原writer；不能以既有defer调用已返回替代真实终局。service.go内为PrepareReadAccess补Read分派，并同Tx核SourceAuthority返回grant.Matches与ProjectGate，属于原授权接缝，不改Object端口。共享guard联合屏障仍由后继app绑定。

唯一实施者`d08_registry_backend`仅可修改本卡16个源/测试路径；固定6658a6c加自己16源自测，不消费Object Audit或D09活动稿。当前无真实fixture权限，由主线程另行交接；无00017、无旧测试/contract/app扩权。自测冻结后须独立验收，不因S2通过宣称Artifact或完整D08完成。

## rev3：已确认反例与有界验证澄清

本节只补齐原“真实 join、完整身份、一次完整锁集合与有界扫描”的工程约束。rev2 固定卡为 `fcb83fcf35cf97dcd4cdc158192f37f5afafe875`，SHA-256 `51b7fee82b9a402fffa6fe5b93ef1b7b86c567267585f280aaabd6ec251e38d4`；此前 API/16路径/四持久游标/迁移与原请求、每包测试预算均保持。本节复用作者已保存的真实证据及独立验收者的限定协议核对，后者记录 `/tmp/agenteam-artifact-selector-review-7ko80vwr/review-plan.md` SHA-256 `6749f5ee9106c20933506fb28d9f7cc3595056f184301df17568ead7ec53c1ae`。本次文档修订没有运行 Go、PG/MinIO 或 Docker，不把尚未实施的 selector 方案写成已通过。

### 原失败及已到达的修复边界

- F2：本地 `settleProducer/recoverArtifactWork` 的真实 join checkpoint COMMIT Unknown 曾被吞为普通 WorkPending。修复须逐 work 保留 UnknownRefs/OutcomeUnknown，该轮不得据此写 stopped；Object 返回错误时仍保留原 Unknown Fault。现有定向通过只覆盖本地 settle，不能写成 dead-process 恢复分支也已通过。
- F3 页内：只按 row.id 捕获本地 handle，未交叉核原 handle 的完整身份和 lease；DB 中 process/resource/command hash/source/epoch/lease 的合法 typed 篡改仍可能触发 cancel。修复在捕获阶段精确等式校验，拒绝错配。F2/F3 修后两个真实顶层 `TestArtifactProjectStopUnknownJoinCheckpointRemainsUnknown`、`TestArtifactProjectStopLocalWorkIdentityTamperingRejectsBeforeCancel` 通过，objects `11.061s`，exit 0；这不是完整 Artifact 验收结论。
- source planning：在 `PrepareReadAccess` 调用正式 `base.Discover` 之前设确定性 barrier，尚未有来源授权，work=0、Resolve=0、公共调用尚未返回时，Source Delete 曾提前报告 stopped；放行后 gate 拒绝后续工作。本反例证明来源规划窗口漏计 join，未证明发生了未授权源 I/O。
- 页外 selector：已确认的本地 S→T work，其 DB `source_project_id` 被改为 U 后移出 S 的扫描集合。S Delete 未 cancel 原 context，但仍产生 gate=1、Object Continue=1；仅核当前 SQL 页中已选出的行不足以发现被移出 selector 的原本地关系。该原红保留，修复须消费本节的完整本地快照与分批前置，不能只重复页内 F3 校验。

原始输入/日志保存在 `/tmp/agenteam-artifact-stop-author-y597q6vl`：F2/F3 `red-input-join-identity-01/manifest.json` SHA `c2a5599b0fe5763cedbb047c1a355409498875d52d027c0af0d1f96ab443fd9c`、`logs/join-identity-red-01.log` SHA `f492ab3603d2c2bf15f97ee8cc653a3413cf14fb39b4ab1ccb7b465aae78031d`；修后输入 `input-join-identity-fix-01.json`、日志 `logs/join-identity-fix-01.log`，production-review-03 delta SHA `b8e15d6348264a213f1a6fa6c5233201c7e4f6bfd552a32d21ce803ad092941c`。source planning 原红输入清单 SHA `7940d118d2050610e97d2c3564b977e9a2c2bc424e79666497cfb457bf26ac92`，对应 `source-planning-red-input-01/manifest.json` 与 `logs/source-planning-red-01.log`；selector 原红输入清单 SHA `c10093d84f3e57b450cacafb4fe0bfe3e81d4c228d778168978e2a0fc9657217`，对应 `selector-red-input-01/manifest.json` 与 `logs/selector-red-01.log`。本节持久记录已确认事实，完整交付仍须按原门槛归档准确源、日志与重跑材料，不能依赖临时目录永久存在。

### 来源规划 hint 的有限含义

completed command 仍先当前目标可见性、完成 target-only replay；只有未从该路径返回、实际进入 source planning 的调用才登记本地 source hint。hint 来自本次合法语法输入的候选关系，只为保守识别尚在来源规划中的整次调用；不代表已授权源、不写 `source_project_id`、不构造 SourceLease、不凭 hint 取消任何来源侧 handle，也不给另一 Project 的 Read/Mutate 权限。

本次停止请求的正式 Authority 已 confirmed 后，正常 gate/Object Continue 可以推进；hint 只令尚未真实退出的相关 whole call 保持 pending，不能提前报告 stopped/Joined。该调用完成前不能以 work=0、Resolve=0 或当前 selector 无行抹掉 hint；完成/失败仍须按原 producer 与 writer 终局规则退出。completed replay 不登记新 hint、不重新读取已删源。此处“hint 保守 pending”与下述“selector 验证未通过零 gate/Continue”是两个不同条件。

### 同一请求内的完整快照与 ≤64 批验证

1. 多批读取前登记本次唯一的 stop 请求 control，覆盖所有预检/最终 gate/确认事务及尾部，使用稳定 control EX 作为共同串行屏障。它是本请求的所有权，不是新增持久 work、来源事实或待自行 cancel 的业务 handle；其未终局仍阻止全局 producer Joined。相关业务本地集合在 registry 互斥下取快照：原 member pointer、私有 lifetime ID、完整 work tuple（id/kind/process/target/resource/command hash/登记 epoch/source）、当前 lease、hint、admitting/returned 状态、原 control Tx/锁 union 身份及可识别 ABA 的单调 revision。快照不能只记 work ID 或数量。
2. 已确认的本地 S→T 关系必须按快照里的原 work/native identity 逐项读回匹配，不能先用当前 DB `source_project_id=S` 等可变 selector 把它过滤掉。合法 typed 值仍须与原 handle 的完整事实相等；失配拒绝，不取消、不修回数据库来掩盖原红。尚无 work 的 admitting/hint 成员仍保持其真实未确认状态，不能为满足核对补造持久 source。
3. 每批最多 64 个相关本地成员，只作业务只读验证，仍使用本请求原 context/剩余预算；每个物理 Tx 一次取得当前完整 Authority 依赖、原 cause Project EX、该批原 work/native/command/相关另一侧 Project/control 等完整 union，重验 exact cause、mode、映射和完整 tuple。原业务 writer 及最终 gate 的完整 union 不得因新增预检而缩减；不在 Tx 内补锁或改用假的授权。
4. 每个 batch 都包含同一请求的稳定 control EX，且只有本次 Tx confirmed 才进入下一批。若 AcquireAll 失败前尚未证实取得 control EX，不能只重取该 control 锁就推断原 writer 终局；须保留该失败/未确认批至多64项的原 union，并按原可能持锁的 EX 确认规则收束。已 confirmed 的历史 batch 不累计锁集合；不能在末尾构造所有批锁的无界 union。确认事务自身也属于原 control，NotCommitted 且可能残留域锁时同样不能提前注销。
5. 只有全部批在原请求预算内 confirmed，相关完整快照及单调 revision仍一致，才允许进入原 gate 写事务。缺批、Unknown、NotCommitted 未证 writer 终局、预算耗尽，或成员/tuple/lease/hint/admitting/returned及原 control Tx/union 变化（包括增删后还原的 ABA），均不尝试新 gate/撤销语句，零 cancel、零 Object Continue；必要 owned control 保留到真实终局。公开 NotCommitted 保持原业务值，不改为业务 Unknown，不通过 fresh context 重开预算。
6. 最终 gate 的线性化点在已持原 cause Project EX、当前 Authority 完整校验之后：按 registry 互斥核完整快照与 revision，并作 gate 决策；本次持久扫描页仍最多64项，原页及实际待 cancel 的原 handles在同一 Tx 以原完整 union 再核。不能把早前只读 batch 当永久授权，也不要求持 registry mutex跨所有批的 PG I/O。提交 confirmed 后仅取消仍精确匹配的原 handle。晚于该线性化点的新入场不追溯声称属于旧 snapshot，仍须纳入后续 whole-call join，不能因此空成功。
7. 上述“零 gate”指只读预检未满足时不执行 gate 写；若原最终 gate 已尝试 COMMIT 而返回 Unknown，数据库可能已经提交，仍沿既有原 writer/完整事实确认，确认前零 cancel/Object Continue，不能声称 gate 必定不存在。final gate/后续确认的未知或未证回滚终局仍归原 control。
8. 跨批成立依赖原正式协议：已登记 work 身份不可被正式 UPDATE 改指向；lease 单赋值、登记时 epoch 确定及其它合法本地 lease/epoch/hint/admitting/returned/成员、原 control Tx与union变化都在 registry 互斥下同步更新单调 revision，原 pointer/私有 ID也参与等式；所有相关正式 writer继续持原共同 Project 锁及本域稳定锁。这里不授权修改 immutable tuple，也不声称能对绕开协议、在批间任意直接 SQL 篡改全库提供单事务原子性。
9. 相关 locals 超过64时按稳定快照分批完成，不能永久以“>64”拒绝。本轮未完成就安全返回，由下次请求重新取全快照/重验，不保留跨轮私有“已验证”状态。四个既有持久扫描游标的定义、字段与进度语义不扩展，不把本地分批游标写进它们。

### 必须补验的真实反例

在原16路径中的真实测试文件补齐，可增加必要顶层，不改旧断言或延长原预算：

- formal Discover 前的来源规划 barrier：尚未授权、work/Resolve均为0而公共调用未返时保持 pending；放行并实际收尾后才 stopped。completed target-only replay无 hint/新源访问；hint不触发来源 cancel或冒充 durable source。
- 页外 selector 原红，以及 >64个相关 local成员的实际多批读取；超过64仍能在真实确认后进展，不能只用mock计数证明分页或把永久busy当通过。按原 handle直接比对移出selector的记录，零gate/Continue。
- 首批后、最终 gate 决策前的真实成员增删/ABA、lease/epoch/hint/admitting合法变化与 tuple错配：快照不稳定时零gate/Continue，不复用上一轮私有验证结果。线性化点之后的新调用计入后续join，并核不误取消新句柄。
- batch/最终 gate/确认事务分别命中真实COMMIT丢ACK、ROLLBACK和原writer仍持锁；read-only batch未全confirmed不尝试gate，final gate Unknown不作不存在断言；精确backend/共享锁证明终局后才退control。含NotCommitted但实际ROLLBACK终局未证、AcquireAll部分失败尚未证controlEX的反例，验证保留当前≤64原union且没有累积历史批或绕开原预算。

F2/F3、本节 hint/selector 与真实 join增量全部须在最终冻结源上按原独立验收门槛验证；定向作者通过、只读协议核对或规格采纳都不替代该结论。API、16个源/测试路径、四持久游标、ProcessGuard装配边界、Cleaner后继及“不占00017”保持。

主线程采纳记录：2026-10-05，独立验收者对冻结候选 `65f744a2866b70e5c35d71478a3d31ad15c9426a987b56ba6cdac4752c6755bb` 的限定差量完成核对，原API/16路径/预算不扩。仅将hint段的Authority明确为本次停止请求的Authority，避免与Source Read授权混淆。主线程采纳工程澄清，继续原范围修复；本次没有新增动态通过声明。
