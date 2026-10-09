# D12 B02：完整 Human canonical 内容与文档树服务

状态（2026-10-09）：**按已接受 rev1 契约开始 B02 实现，尚未验收。** 独立树 `ai/knowledge-service` 从正式 `f1c94ee5` 开工；B01 的 12 个纯契约文件、C1 `cursor.Text`、C2 Knowledge Audit 闭集、C3 精确 ReferenceCleanup 已在基线中存在。§4 的 29 个服务／测试路径无可恢复旧实现，本轮由唯一作者实现；首批服务构造、调用登记／取消与 Drain、严格 canonical 行读取和当前事实口已可构建，完整 Documents／发布／树变更尚未闭合；本域迁移独占 `00025_knowledge.sql`，`00024` 为另一树 ProjectVariables、`00026` 为 Runner control，不能按本树当前最高编号重排。

依据：[D12 主卡](d12-knowledge-documents.md)、[D12 规格](d12-knowledge-documents-design.md) §3–7 及本卡已接受 rev1。旧 `/tmp` 候选／报告路径仅是历史定位，不是本轮必需输入；当前实际 B01 源码与正式规格是恢复依据。代码、SQL 与受控依赖测试可先行；完整业务接受仍需真实 Owner／Object／Audit／Outbox 组合、并发恢复与资源退出证据，不能把测试替身当生产授权。Object runtime join 等原停止项保持，当前不改 Object 实现、App root 或公共 fixture。

### 当前集成接缝

- 经 root 授权，B01 `contract/events.go`／`events_test.go` 仅补向后兼容的 `KnowledgeEvents.Valid() bool`：零值 false、真实 Catalog 注册后 true，封存 Catalog 不使已注册类型失效。它只支持构造拒绝缺失依赖，不是事件事实或权限。未改编码／闭集，已获未参与者有限独审接受。

- C1/C2/C3 已有实际源码，保留只读，不重复实现。D05 的 `NewProjectAuditAuthority` 已存在；它的存在不等于 Project 已接入 Object producer，也不证明本轮 Object runtime 组合已接受。
- Project 外域 Audit 路由实际位于 `project/audit_facts.go`，原基线仅接納 Secret、Object 明确 `DEPENDENCY_UNBOUND`；root 已授 §3 C4 五技术路径，现开始补 Knowledge，公开接口不变。Variables 并行树在同两个既有文件另增独立分支，最终由 root 按限定差异合并，保留两域与全部旧路由，禁止整文件覆盖。
- Project Outbox 原 `project/events.go` 已接 Model／Work 专口，本轮只增精确 Knowledge gate，不新造任意 Human producer 的通用放行。历史 `outbox_authority.go` 候选不是现存输入。B02 自有 producer 独立证明真实命令/当前事实；gate 仅证明当前 Project 权限，两者消费同一 Tx。
- 本域 `Documents`、树 mutation、canonical read/fact 与对象所有权／清理适配器按 B01 端口实现。D13、HTTP、受控下载 URL、全 Project participant 仍属后继；本轮不以它们未就绪阻止本域代码开工，也不把它们称作已绑定。


## 1. 推荐结果与完成边界

推荐交付一个完整库：真实实现 `kc.Documents`、`kc.TreeMutationPlanning`、`kc.AtomicTreeMutations`、`kc.CanonicalReads`、`kc.CanonicalFacts`，以及 Knowledge 的 Object ownership/current-object/source/cleanup、typed Event 和危险删除 Audit 事实适配器。Human 可创建文本/文件、读取 current 内容、更新、移动、分页查树、确认并删除完整子树；不是只能读取或移动预置数据的生产服务。

不采纳永久 B02-R（只有查询/Move/Preview）作为主结果。其规则/SQL可成为同一 B02 的中间开发检查点，不新增缩减版 Documents 公共接口，不据 fake 种行宣布业务闭环。新领域代码和纯共同口可以先行；完整 B02 业务验收必须等 §2 的真实依赖、正式迁移和独占资源到位。

本块不含 Knowledge HTTP/UI、签发下载 URL、D13 parser/index/retrieval、Agent destructive 或 Project 全域 participant。`OpenCanonical` 是真实受控对象读，不等于受控下载 URL/provider 已实现。PDF/DOCX 原文件可由该受控流读取，结构化 `ReadDocument` 返回既有 unavailable，不伪造文本；text 按正式有界 UTF-8 读取。D13 未启用仍原子存 current pending 与 typed invalidation event，不注册成功空 handler。启用真实生产 Knowledge 和 Project archive/delete 完整联动，仍需后续 B03 participant 与下载/HTTP 各自验收。

## 2. 真依赖、当前所有权及集成门槛

| 项目 | 已有正式口/实际情况 | 本卡采取的最小处理 |
| --- | --- | --- |
| Human 当前身份/Owner | 已验 `pc.ProjectAuthority.RequireOwnerInTx`，同活 Tx 核 User/Project 锁、当前 Session/Owner、初始化及 gate | 直接消费；禁止查 account/project 私表，禁止用纯 `CheckOwnerGate` 代权限。正常 Read/Mutate 无 D09 前置。 |
| Session 活动 | 真实 account Authority 已提供 `TouchActivityInTx` | 自有窄结构接口，只有首次成功变更同 Tx 调用，预先 User EX；完成重放不 Touch。 |
| D05 对象与源 | `oc.Objects`/`Uploads`/`SourceResolver`/`SourceReads` 形状已验；Knowledge 所属适配器缺失 | 新 Knowledge 实现真实所属事实；D05 继续拥有 MinIO、upload/reader/lease/writer。源 resolver 前工作登记/stop 的实际增强由 A 实施且未验，集成必须绑定其已验版本。 |
| ReferenceCleanup | 现有 C3 已接受 Avatar／Knowledge 精确分支 | C3 已交付，消费现有精确 Knowledge cause 分支；本轮不改 Object 实现。 |
| Object Audit 事实 | D08 §9.2/C0 `audit.ProjectFactAuthority` 与 D05 `NewProjectAuditAuthority` 均有实际源；Project 路由仍未绑定 | 必须消费 D05 自己的同 Tx 真实见证与 upload/attempt/cleanup事实。Knowledge command/cause 不能证明 D05 成功/失败；不得实现 allow checker。属于 D08/A 后段实现依赖，不是本卡偷偷扩大 D05 源码。 |
| Knowledge 删除 Audit | C2 闭集及 `AuthorityDependencies.AuditFacts` 已存在；实际注册/外域分派位于 `project/audit_facts.go` | 按已授 C4 只增加 KnowledgeProducer，当前 Owner Mutate 后原样委托本域私有见证 checker。缺 provider 仍 Unbound；不改 Audit service/公共接口。 |
| Knowledge Outbox | B01 typed events 与正式 Outbox Project/Producer ports 已存在；Project 基线已接 Model/Work | `project/events.go` 增精确 Knowledge 分派，新 `knowledge_event_authority.go` 绑定完整 Actor/Session/summary、私有 issuer/双 stage、User/Project SH。CurrentAccess=Read、NewFact=Mutate；另由本域 ProducerAuthority 核真实事实。默认生产未安装本域 producer，仍未绑定。 |
| 分页 | cursor.Text 已交付，与原 instant/uuid/integer 共存 | 直接消费 C1；不改签名格式、不以伪 integer 或全量内存分页替代。 |
| 迁移/资源 | 00001–00023 已正式存在；本卡独占 00025，00024／00026 属并行树 | 旧临时 DDL 不作现存输入；按 §6 实现 00025 并核前序 Audit CHECK 增量。真实迁移等待连续前序就绪和独占 PG／MinIO 窗口。 |
| Project生命周期 | D08 P、A stop与Knowledge participant未联合验收 | 本库具备真实本域工作登记/取消/join/恢复；后续 B03 绑定 exact lifecycle cause、required manifest、source_project_id join和删除顺序。不能把 B02 通过写成项目全域停止/清理通过，生产根继续未绑定。 |

因此可以先派同一完整 B02 作者写新 Knowledge 主体、纯测试和集成准备；共同口按下表先冻结再消费。未满足的真实上游是后段验收门槛，不是停止所有新文件开发的全局门槛。依赖版本在真实验证前固定；不从 A/B/P 活动源码拼编译输入。

## 3. 最小共享补口：分别授权、保留闭集

**C1 Cursor，旧1+新1。** `internal/central/cursor/cursor.go` 增 `func Text(value string) (Scalar, error)` 与 scalarValid 的 `text` 分支；新 `internal/central/cursor/text_test.go`。只拒非法 UTF-8，保留原字节（空串合法，领域标题规则另验）；沿原8KiB token限制、canonical-v1及签名版本，旧三 scalar 字节不变。测不同 Unicode 正规形、空串、非法UTF-8、混合title/UUID position、旧token兼容和跨过滤条件拒绝。

**C2 Knowledge 删除 Audit，旧2+新2。** 旧 `internal/central/audit/contract/types.go`、`metadata.go` 仅增加正式闭集分派；新 `internal/central/audit/contract/knowledge.go`、`knowledge_test.go`。精确新增：

```go
const KnowledgeDeleteSubtree Action = "knowledge.delete_subtree"
const KnowledgeDocumentResource ResourceKind = "knowledge_document"
const KnowledgeProducer Producer = "knowledge"
type KnowledgeMetadataFields struct {
    ProjectID, RootID, InitiatorID string // UUIDv7，分别匹配scope/resource/当前Human
    ScopeDigest foundation.Digest
    DeletedCount foundation.Progress // >0；JSON沿精确整数的字符串协议
}
func KnowledgeAction(Action) bool
func KnowledgeMetadata(Action, KnowledgeMetadataFields) (Metadata, error)
func (Metadata) KnowledgeFields() (KnowledgeMetadataFields, error)
```

唯一合法 entry 是 Project/Human/success、resource=root、ordinal=0、精确五字段，无body/title/ObjectID/token/节点清单；字段名 `project_id,root_id,initiator_id,scope_digest,deleted_count`。cause 为已有 CommandIdentity 的域隔离 SHA，不暴露 raw key。metadata构造只证明形状；真实 `KnowledgeFactAuthority` 校验同Tx原命令、scope digest/count、exact tombstone集合及该次新提交，不能靠Actor/UUID字符串放行。`audit/service.go` 当前一般Human分派已调用 Project authority，预计无需改；它归 B，若冻结版改变该事实需独立报精确diff。新增 SQL closed branch及既有四个闭集CHECK的合并纳入本域最终迁移；不能覆盖B的新Action/Producer/资源约束。

**C3 Knowledge irreversibly release，旧3+新2。** 旧 `internal/central/object/contract/access.go` 的 CleanupReleaseAccess：保留 Avatar+ReplacedObject/CancelledUpload 原分支；仅另加 Knowledge（必须含Project）+ReplacedObject/CancelledUpload/OwnerDeleted。旧 `internal/central/object/reference_cleanup.go` 将硬编码 Avatar owner/partition 改为经验证 cause 的精确 kind/id/partition 参数；保留真实 upload/object/owner/cause检查、canonical/reserved撤引用、upload disposition=revoked、gateObject及幂等原因匹配。旧 `internal/central/object/contract/reference_cleanup.go` 只同步 Avatar-only 注释为明确两个闭集，签名不改。新 `internal/central/object/contract/knowledge_cleanup_test.go` 和 `tests/objects/knowledge_cleanup_test.go`。无新D05表、无扩大其他owner，不能只删 Avatar 检查或用普通 ReleaseObjectInTx 代替 publication gate。

**C4 Project 接续，旧2+新3，已获 root 精确授权。** 只改 `internal/central/project/audit_facts.go` 与 `events.go`；新增同目录 `knowledge_event_authority.go`、`knowledge_audit_test.go`、`knowledge_event_authority_test.go`。公开 `AuthorityDependencies.AuditFacts`、`NewAuthority`、Audit/Outbox port 均不变，不动 Audit/Outbox service、app、Object 或公共 fixture。

Audit 注册闭集追加 KnowledgeProducer，拒 nil/typed-nil 并复制选择 map；Project scope/ProducerFor 精确匹配后，只接纳 KnowledgeDeleteSubtree/Human/success/knowledge_document/ordinal0/全空 associations。先由真实 `RequireOwnerInTx(..., Mutate)` 校验当前身份、Owner、初始化/生命周期及已有 User/Project SH，再将原 ctx/Tx/Entry/AppendKey 交给 Knowledge provider；保留私有 witness 和原 Fault/Unknown。Project SH 与 lifecycle 所需 Project EX 同一锁序串行；不新造名义 Audit 屏障，也不把此口冒称完整 Audit cleanup。缺 provider 仍 DependencyUnbound，Object 既有未绑定不改变。

Outbox 仅接 `knowledge.content_changed|knowledge.deleted`、producer=knowledge、schema1、aggregate=knowledge_document、有效 version 且无 sequence、Human/匹配 Project 的闭集。无 I/O 的 Discover 只接受 CurrentAccess，返回 User SH/Project SH 与 Project 私有 issuer/purpose；绑定完整 Actor（含 Session）、完整 summary/payload digest、Project 及固定双 stage。Validate 在原 Store 活 Tx 核 exact binding/purpose/锁集与实际 held locks，再分别使用 Read/Mutate；不新开事务或晚取锁。原 Outbox 还必须调用本域 ProducerAuthority，不能以 Project gate 代替 Knowledge 命令/当前事实。构造不证明任意 opaque provider 的 Store 身份；生产和真实验收须显式同 Store 组装，双方对同一活 Tx 分别拒 foreign/ended Tx，本域再核私有 witness/issuer。默认 root 尚未绑定 Knowledge provider，当前 C4 纯控/静审不称真实组合通过。

Variables 作者在独立树拥有同两文件的相邻增量；早期 provider/consumer 对齐已确认无冲突，稳定后交叉独审。最终由 root 精确合并保留两域分派，不能复制整文件覆盖另一域。

**D1 验收接线，旧1，等共享fixture交接。** `tests/testsupport/postgres/cmd/fixture/main.go` 只在既有package参数追加 `./tests/knowledge/...`；不改fixture安全/生命周期/cleanup逻辑，不改公共测试库。正式迁移是另一待编号新路径；除本轮 C1 外，上述都需root明确授权，不在新领域29路径里自动取得写权。B当前可能消费固定driver，交接后固定新输入再跑本块，不能漂移其运行中的组。

## 4. 新领域完整白名单与构造

新生产源精确15路径，均 `internal/central/knowledge/`：`service.go`、`repository.go`、`commands.go`、`planner.go`、`publication.go`、`recovery.go`、`runtime.go`、`object_authority.go`、`source.go`、`cleanup.go`、`read.go`、`query.go`、`tree.go`、`events.go`、`audit_authority.go`。相邻测试精确8：`service_test.go`、`commands_test.go`、`planner_test.go`、`publication_test.go`、`runtime_test.go`、`source_test.go`、`tree_test.go`、`query_test.go`。新集成6路径均 `tests/knowledge/`：`b02_fixture_test.go`、`b02_owner_tree_test.go`、`b02_publication_test.go`、`b02_recovery_test.go`、`b02_migration_test.go`、`b02_audit_event_test.go`。新领域共29路径；每个落笔前检查不存在，出现已有文件即报所有权冲突。12个 B01 源保持只读；Go module/dependency、app/config、公共测试fixture、D05活跃旧域均不在白名单。

```go
// 新knowledge/service.go；Store的完整签名与已验project.Store相同。
type Store interface {
    postgres.SQLExecutor
    WithinTx(context.Context, foundation.TransactionCause,
        func(context.Context, foundation.Tx) error) foundation.CommitResult
    InTx(foundation.Tx) (postgres.SQLExecutor, error)
    AcquireAll(context.Context, foundation.Tx, []foundation.LockRequest) error
    RequireHeldLocks(context.Context, foundation.Tx, []foundation.LockRequest) error
}
type ActivityAuthority interface {
    TouchActivityInTx(context.Context, foundation.Tx, identity.Actor) error
}
type Dependencies struct {
    Projects pc.ProjectAuthority
    Activity ActivityAuthority
    Objects oc.Objects
    Uploads oc.Uploads
    Sources oc.SourceResolver
    SourceReads oc.SourceReads
    ReferenceCleanup oc.ReferenceCleanup
    ObjectCleanup oc.Cleaner
    Audit audit.Appender
    Outbox ob.Appender
    Events kc.KnowledgeEvents
    Processes ob.ProcessAuthority
    Cursors cursor.Keyring
    Confirmations kc.ConfirmationKeys
}
func New(store Store, deps Dependencies) (*Service, error)
// Authority只依赖Store+Projects，无Service/Object构造环。
func NewAuthority(store Store, projects pc.ProjectAuthority) (*Authority, error)
// Audit checker只依赖本域Store；Project授权由调用它的真实P authority先核。
func NewProjectAuditAuthority(store Store) (*ProjectAuditAuthority, error)
// resolver绑定真实Objects读取元数据，不能跨域SELECT object表。
func NewSourceResolver(store Store, authority *Authority, objects oc.Objects) (*SourceResolver, error)
```

`Authority` 实现 `oc.AccessPlanner/ResourceAuthority/ObjectReadAuthority/ProjectGate/CleanupAuthority` 和本域 `ob.ProducerAuthority`；独立 `ProjectAuditAuthority` 实现 `audit.ProjectFactAuthority`。构造顺序是Knowledge/D05本域Audit checker→P配置AuditFacts→Knowledge Authority→D05 Objects→SourceResolver/Outbox→Knowledge Service，不让P与Knowledge Authority互相等待构造。各接收者仅读/写 Knowledge 自己事实，并使用活Tx/held-lock正式口。纯构造不授读取，public计划不能被当作held/auth。`New` 对当前实现所需依赖拒 nil/typed-nil；D13/HTTP/执行文件 provider不作为伪必需依赖，未知源variant明确 DependencyUnbound。

固定 D05 `object.New` 本身不依赖SourceResolver；构造 Objects 后可创建 Knowledge resolver与真实source分派，随后调用现有 `object.NewSourceReads(objects,resolver)`，最后构造Knowledge Service，无需事后可变setter或空provider打破环。测试组合只分派到真实provider，若生产需要公共router另授权，不提前修改app。实现 KnowledgeFile 全路径；UploadedObject 必须消费D05正式receipt，ArtifactFile仅委托已绑定真实Artifact provider，ExecutionFile无真实provider则明确拒绝，不捏造执行文件。每个缺依赖在新工作登记/I/O前失败，不将未绑定分支称已验。

## 5. 必须守住的算法与持久事实

1. **权限/重放次序**：校验输入形状→同活Tx当前Session/Owner及结果可见性→同key receipt及摘要→若新工作才Mutate gate→version/parent/confirmation→写入；完成重放不查旧source/version/token密钥、不发Put/Touch/Event。AgentRun/Service普通CRUD拒绝；foreignProject/target不透存在。当前初始化未完成或deleting gate不被重放旁路。所有InTx正式口验证私有issuer、exact请求、actualTx、完整held union与外域mapping，无内部新事务或I/O。
2. **一次完整union**：所有命令锁 namespace=knowledge/OwnerIDs=[project]，Command EX/User EX/Project SH/KnowledgeTree EX，加D05source/owner/Object、D06event、末尾record锁；查询User SH/Project SH/tree SH。发现放Tx外，Acquire恰一次；跨Project source将两边真实User/Project/tree锁统一排序，漂移回滚并重发现，绝不晚补早序锁。Create额外加入 `RecordLock(ReferenceRecordLock,"knowledge:document:"+targetID)` EX，锁下查本域全局document/create-command占用，防跨Project同ID竞态；其他Project探到占用返回404，同Project另key RESOURCE_BUSY，tombstone不复活，DB全局唯一仍保留。
3. **意图/内容发布**：Create/Update先持久原command+target+source精确Details+publication cause，prospective不进目录；在任何resolver/spool/源读取前登记本域真实Process/attempt/fence与目标/来源Project。D05 measured preparation→同Tx reserve→确证→事务外send/verify→最终同Tx publish/canonical reference+Document current/version+safe receipt+typedEvent。Source在计划捕获、开流、最终发布按D05正式lease/version重验；源变更不自动latest；最终completed重放不再访问已删源。title-only沿同一command receipt/event事实，不新造Object。
4. **更新/移动**：Update必须原ExpectedVersion，final再验，SQL只改显式title/source及内容版本/updatedAt/indexing，绝不回写parent；Move只改parent/updatedAt，不改version、不发content event。持tree EX重验当前expected parent、target完整祖先链，同Projectactive/无环；A→B与B→A竞争不得都成功。真实无变化写no-op receipt，不重复副作用；如内容替换实际量测SHA/MIME/bytes与current等同，以真实对象元数据判定，清理新临时upload，不单凭客户端声明相同。
5. **事件发现可执行**：所有变更先有本域持久plan，固定event ID/header/payload（delete每node一个，Move无event），供Tx外 `DiscoverAppend`；producer校验原command/current-vs-new stage，最终同Tx校验实际current/version/status+exact payload。不先写Outbox而后补事实。tree snapshot或source变动使计划失效；complete不存在任何业务事件重放。Delete第一次可在独立短Tx保存通过签名的命令计划，但final仍完整重验签名/时效和scope，未提交tombstone不算accepted-delete成功。
6. **删除**：preview在tree SH完整递归，不静默截断；final tree EX下当前身份→receipt→token签名User/Project/root/期限→完整scope digest，一次事务将全部active节点变成最小tombstone，撤业务current-object映射，保存每Object/upload的exact cleanup cause，写一个root危险Audit+每node删除Event+receipt。任一失败全回滚；resource budget不足明确失败，不删已加载前缀。已提交same key/token即使token过期/旧kid移除仍安全receipt；换token同key异义，Unknown不能刷新token或换ID。
7. **延迟清理**：tombstone/version提交立即阻断旧payload新读及新source；撤引用可同Tx完成或可靠任务留存，但不许只普通Release后仍保持旧upload可再次附着。active document内部保存发布时真实receipt/attempt给出的current_upload_id，不对外投影；替换/删除同Tx把旧ObjectID+UploadID移入cleanup后清掉旧current指针，不能事后查D05私表猜UploadID。`ReferenceCleanup` exactcause撤canonical/reserved并revokes upload+gates；未附着pending upload调用真实CancelUpload，之后真实DeleteUnreferenced/Inspect，pending/Unknown保持待收敛。新指针提交后清理故障不回滚/恢复旧可读权限；真实旧reader尚未join不得物理完成。doc tombstone留ID/project/version/deletedAt，cleanup保exact IDs直到真实完成；Project永久删除由B03有序清这些技术事实。
8. **Converge精准边界**：普通Owner grant只Read/Mutate，`RequireOwnerInTx(...,Converge)`现不支持且本卡不扩。ReferenceCleanup/DeleteUnreferenced只用本域真实持久cleanup cause；D05维护Cancel所需ProjectGate只准注册ObjectMaintenance+exact原cleanup operation/cause/project/owner/upload/object，核原原因和阶段/held锁，不从OwnerRead升权，不接受任意服务cause。ProjectLifecycle类请求暂拒未绑定，后续B03必须消费D08真实ValidateLifecycleInTx；不能借Document cleanup cause进入项目删除。全Project stop/fence仍由D08/D05已验组合限制新I/O。
9. **Unknown按阶段**：保留原CommitResult/CauseID、CommandIdentity、Document/Object/upload IDs；在原command+project+tree完整串行后，确证canonical receipt→committed；对应事务确证缺行且原写者已终局才能not_committed。已有planned publication、原Process活着、租约/外部send未知、锁超时/查询失败都不是rollback。final未提交但plan仍在只证明final not_committed，继续同plan，不能把accepted计划抹掉。reserve不确证不send，publish不确证不宣称active。
10. **本域runtime**：加入真实call登记/取消/等待和durable work claim；唯一ProcessGuard来自注入Processes，不再另造全局guard。恢复旧claim要求真实join或 `ConfirmStopped(exactProcessID)`，不TTL/heartbeat推断；旧fence迟到不能publish。本卡先证明进程关闭/恢复与source关闭责任，正式D08participant后续把本域work与D05 source_project_id联结；不把本域无活跃goroutine当全域已stop。
11. **查询**：短Tx tree SH当前一致；nonroot parent必须同Projectactive。SQL title用原UTF-8、`COLLATE "C" ASC,id ASC`，标题字面子串（转义 `%/_/escape`）且无唯一。cursor.Text+UUID，绑stable user/project/querykind/parent/filter/order，不绑limit、不新建treegeneration，默认50最大200；每页再验当前身份，不承诺跨页snapshot。ReadAncestors/Search完整当前路径，删除head最小，旧receipt metadata不是旧Object读取许可。

## 6. DDL与保留规则

草案包括 `documents`、`commands`、`command_events`、`publications`、`work_claims`、`object_cleanup` 六张本域表，没有D13 job或DocumentVersion/parent历史表。commands/events/publications/work只是事务和恢复技术事实，receipt只存正式安全metadata，正文和原upload reader从不写入JSON。pending publication临时请求metadata在canonical终局/安全取消后清除；保留command identity/安全receipt及完成event校验所需既有事实。单Document删除保minimal tombstone和原command replay能力；Project永久删除须B03清所有本域表，不留文档历史。

正式迁移为 `00025_knowledge.sql`，在真实 00001–00024 前序上保留旧 Audit CHECK 表达式并增量接纳本域闭集；Variables 的 00024 由原作者提供，Runner 后继 00026 必须保留 Knowledge 00025 增量。作者 fresh/populated 升级/末端失败回滚与约束三子已实际通过，见 §8；仍需独立风险验证及完整领域业务 SQL 验收，不把迁移通过扩写为 B02 完成。

## 7. 有意义的验收与交付

先纯测试/race/vet限定新Knowledge和获批C1/C2/C3契约；复用B01既有30主/18子及V3风险probe，不机械重跑全库。fake只测plan/strictshape，不证明权限、Tx、join或真实对象。自有PG/MinIO fixture必须组真实Account→Project Owner、P通用gate、D05 Object fact checker、typedAudit/Outbox与Knowledge adapters，不以假allow充当新用户/项目权限。D10未绑定可用已验Project创建测试口的真实initializer fixture满足已验证初始化契约，明确不称产品D10已实现。

最小真实组：

- 当前Owner/撤销Session/foreign/archived read与newwrite/初始化门禁；wrongissuer、弱锁、foreignTx、extra锁删除、旧source/Object/version拒绝；safe receipt重放前仍当前身份。
- Create文本与PDF/DOCX测量发布，KnowledgeFile精确copy；raw上传SHA/length错、typedsource版本变、同target跨Project及同key两Session并发；不消费没有绑定的Execution源。
- Update source/title与Move确定性barrier竞争，A→B/B→A防环；delete preview后增节点/移入移出/重命名/version变化失效；整个subtree Audit/Event/SQL任一步错全回滚，不半删；大量节点资源预算失败不截断成功。
- 同标题Unicode/C排序、字面 `%/_`，游标wrongUser/parent/filter、当前Session撤销；旧token兼容、rename/move跨页不假snapshot。
- 对象旧引用revoked而不能再Attach、pending上传取消、仍活reader真实保护、故障/Unknown保cleanup cause；D05 Avatar既有路径回归和跨Project/owner/object/upload/cause负例。
- 原生Postgres COMMIT ACK丢失覆盖plan/reserve/canonical final/cleanup checkpoint，原cause/ID不变；活旧Process不可接管、真实guard/SIGKILL才恢复，迟到worker被fence拒绝；退出/读流Close失败不得假join。
- 同Tx实际canonical receipt/tombstone证明Knowledge Audit和event，伪cause/错count/错payload/错producer/跨Project/屏障后拒绝；D05 Object completed/failed/delete使用D05自身真实证据，禁止Knowledge代证。
- fresh schema、前序有数据升级、DDL失败全回滚；global ID/parent FK/tombstone/闭集约束，旧Account/Project/Object/Artifact与B新增AuditCHECK保留。

作者实际完整组及独立风险probe分列执行者/断言来源，保原红及固定输入；阶段准备或compile-only不能称B02通过。无HTTP/root、D13、Agent destructive、全Project lifecycle验收声明。交付固定版本、实际命令/原日志、结论/限制与精确提交路径即可，不复制全树/大索引。候选已完成独立静态审查并获采纳，报告 `/tmp/agenteam-d12-b02-review-acect8x7/report.md` SHA `df2fdea872d0e755449c8b966abdbe8962285d1b76e781f3ea7dd422bf2a86a8`；这不替代实现与真实业务验收。正式卡的路径和状态归位不更改未编号 DDL，原草案 SHA `fe3ed8d6084dced33618e2a7d565492424c1f061bbc5cb54f40d93c5331ca217`。

## 8. 本轮实际实施与验证

作者公开 TitleContent98064 四子完整 PASS：Go2.44s、driver15.208s、outer74.861s actual0；Go751688／driver751026 实际 Wait0，精确 PG container/network 双退役、desc 两次为空、hostTCP 双 delta_empty、inputs_unchanged=True，owned 目录现场仅0600 owned.json，真实窗口已释放。日志 `output/ai/knowledge/pg/pg-0cbb0132059c4b24a93e0b8e507e0098.log`。本轮实证公开 title-only／真实 Account→Project→Knowledge producer→Outbox／固定 receipt与事件／final Activity 后全回滚／Move 阶段交错及撤销门禁；不替代完整正文发布、business-source lease、真实 D05／cleanup、SQL Unknown 或独立风险验收。

第十六片段修正精确 attempt 的本地 join 证明。纯控制73279发现原 claim 缺行后新物理 attempt 可仍为 fence1，旧 command-keyed map 会丢失该次真实退休；原 FAIL 保留，未执行真实 COMMIT Unknown。正式源改为 attempt-keyed 并继续核完整 command/project/process/source/fence；旧 finalizer 不覆盖新证明，durable checkpoint 只删除精确原 attempt。作者正式全域 race29773／vet0；未参与者实际源 overlay35619 三子 race0，覆盖同 fence 三 attempt 的逆序／重复 join、同 attempt 的身份漂移与旧 checkpoint 删除边界，有限接受。其首次 ignored 控制包 import 错误的 setup FAIL 保留，修控制后通过，生产源未改；仍不证明 SQL Unknown 恢复。

第十七片段 `source.go`／`source_test.go` 增加 business-source 解析和同 Tx lease 内部接线：先绑定原 intent／work／精确 BusinessFile reference，在 Tx 外 Resolve；完整 target/source 锁 union 一次获取后重核当前 Owner、原 work 和持久 source，再用原 Tx 的正式 SourceResolver／SourceReads 取得 lease。未确认事务不 Open/read；任何有效返回句柄均登记退休，包括 Unknown／错误同时返回，取消失败保留调用。十项实际控制15717 race0及全 Knowledge／contract race5907 actual0、当前 vet0，包含 reference／media／revision／Owner／work 负控、原物理 Unknown 与错误 cause。首25196因复用 fixture 漏投影 source Project FAIL，仅修测试行解码后通过。此为可构建内部阶段，未独审、未 SQL／D05 动态；公开 business-source 仍 Unbound，后续须实际 Open／字节校验／Close 与最终源 revalidation，不称完整 Documents。

公开 title-only 的最小真实 PG 小组已准备，`tests/knowledge/b02_audit_event_test.go` 的 `TestKnowledgeB02TitleContent` 四子覆盖变更／固定事件与 receipt／原命令及 no-op／归档重放，真实 Activity 成功写入后注入失败导致同 Tx canonical／Outbox／receipt／Activity 全回滚并用原事件重试，准备与 final 之间实际 Move 不丢 parent，以及初始 foreign／错版本与 final Session 撤销。Outbox 使用真实 Account、Project、Knowledge producer 和同 Store；没有 Object I/O、Audit 或 dispatcher 的假成功声明。`b02_owner_tree_test.go` 仅暴露原 Dependencies，原四项断言不改；本次是新的编译闭包，旧 OwnerTree32137 证据不升级为本版重验。首 race-c25575 的测试 Lookup／Creator 字段误写 FAIL 后已按实际公开接口修正，SQL 末端状态亦核为 completed；最终 race-c24829、精确 discovery／vet19054 actual0。作者实际98064完整结果见本节首段；精确命令及固定原预算见 current。本组只证明公开标题路径，不能替代业务来源、D05 内容发布／cleanup、真实 Unknown 或完整 B02。

Project Object Audit 五源随后获未参与者有限独审接受，无 mustfix；本人独立 overlay race19224 actual0（3 子）验证同 Tx 生命周期／初始化重读、真实 Object checker 对无私有 witness 的四个合法 action/ordinal 组合拒绝，以及错 cause/ordinal/Service/foreign/受控 ended Tx 不委托。原 ctx/key/Unknown 引用原因保留。该树没有 Variables 分派，不冒其回归；此为静审与受控实际源结果，不证明真实 D05 正向／PG／stale Owner 联合授权或 Object runtime join。

第十四片段 `commands.go`／`commands_test.go` 已组合最终发布：最终完整锁下重读当前 Owner／work／版本，写 canonical 后在原 Tx 消费 D05 Publish 私有 witness，再关闭旧引用、写精确 cleanup、Outbox、receipt 和 Activity；固定事件与清理 identity 可重放，旧 parent 不被内容更新覆盖。真实 Stat 的 MIME／长度／SHA 相同才允许复用原 Object，最终仍重核原指针／work／版本；纯标题变化保留 source，不变内容不增加版本／事件／Activity。全领域 race99838、vet2640／diffcheck 实际0。控制仅证明结果形状、绑定与拒绝，实际 final SQL／D05／Outbox 联合事务及恢复尚未执行，不称完整发布通过。

第十五片段 `service.go`／`runtime_test.go` 接通公开 Create／Update 的 direct text/upload 与 title-only 编排，并完成 `Documents` 接口编译。调用从准备到实际输入 Close／prepared Discard／本域 join 检查点返回始终登记；阻塞或失败的 Close 不被 cancel／Stop 冒充为退休。canonical 已确证提交后清理失败保持 Committed 语义。作者全领域 race3616 actual0，随后仅收紧测试无条件释放／实际 join 的 teardown，限定 race10017 actual0；当前 vet20406／diffcheck0。公开拒绝／shutdown 控制验证不读输入、准确 Close 次数、阻塞 Close 时 Drain 不提前结束；本阶段起初尚无公开成功 SQL；后续98064已限定验证 title-only，真实 D05 组合仍未验。business-source final revalidation 仍显式 Unbound，接口编译不代表完整 Documents；Object runtime join 停止项不变。

第十三内部片段四源 `object_authority.go`／`service_test.go`／`publication.go`／`publication_test.go` 已可构建。ExistingOwner 从原 Create command 读取真实 CreationCause，供同 Tx canonical 行先落下、再消费原 prospective upload 的 D05 授权校核，原当前 Owner／锁／Tx 门槛不变。reserve 使用 Knowledge command UUID 的私有 D05 key，当前完整 intent／work／测量后在原同 Tx 保留精确 attempt；confirmed 才能继续发送，真实 I/O 在 Tx 外，返回不同 attempt 不进入 SQL，uploaded 检查点 Unknown 保留原物理提交 cause。持久测量严格三个字段，未采或缺失长度不能冒成零字节结果；reserved/uploaded 必须有完整 object/upload/attempt 与测量。

新增实际源控制覆盖原 CreationCause／缺记录／损坏记录／当前 gate 拒绝、测量缺失／重复／null／额外字段、持久预留缺字段／错 Project／错文档、单次锁 union／同 Tx／私有 D05 key／Tx 外发送、stale-owner 受控拒绝、work/测量漂移、reserve Unknown、错误 attempt 及 checkpoint Unknown。纯65344通过；78484曾误要求经正式 CommitResult复制后 Fault 指针仍恒等，修为正式 code/cause 判据，随后全 Knowledge／contract race65165、当前 vet/diffcheck实际0。不修改 D03 错误语义，不冒真实权限／SQL／D05 组合；此片段未独审，最终原子发布、business-source及public Create/Update仍在实施。

追加的 Project Object Audit 接缝沿 D08 正式 §9.2／§9.2.1，复用 `audit.ProjectFactAuthority` 与 `object.NewProjectAuditAuthority(Store)`，不新增公开 port。授权精确五源为 Project `audit_facts.go`、`object_audit_facts.go`、`object_audit_facts_test.go`、`audit_facts_test.go`、`knowledge_audit_test.go`，真实组合测试仍用本卡 `b02_audit_event_test.go`。构造复制并选择 Object provider；只分派 ObjectService 的 upload complete／failed／delete，严格核 Project／cause／resource／outcome／ordinal／空 associations。先要求同 Store 活 Tx 与 Project SH 并重读已初始化项目；complete 还须当前 Active Mutate，failed／delete 只在真实 Object 私有同 Tx witness 证明已有事实后收敛。当前 Human Owner 不从 metadata 重建，而由真实 Knowledge→Project 授权及 D05 publish 前检查链证明；普通 Owner Converge、Transfer、初始化专用授权、Object runtime join 停止项和生产 root 均不变。缺 provider／私有 witness／精确 mapping 拒绝，原 ctx／Tx／Entry／Key／Fault／Unknown 原样委托。

该片段限定 race60546（前84176亦0）、Project vet 与 diffcheck 实际通过，包含旧 Secret／Knowledge 分派和 Initialization 控制、新状态矩阵、foreign／模拟 ended Tx／弱锁、原 Unknown、真实 D05 checker 无私有 witness 负控及 Transfer 仍 Unbound。首编 ordinal 类型／不可比较 LockKey／测试 Row 名错误和首次 opaque Actor 直接 DeepEqual 的测试误判均已修，保留原 FAIL，不改业务门槛。尚未独立接受或真实 D05／Account Owner／SQL 组合；本树没有 Variables 的相邻 Project 增量，最终合并需保留其精确分派并回归，当前不得称已验证 Variables 或真实 stale-owner 发布拒绝。

首批 `service.go`、`repository.go`、`runtime.go`、`read.go` 与两项相邻测试已落盘；`GetDocument`／`ReadAncestors`／`ReadCurrentInTx` 通过同 Store 活 Tx、完整已有锁和真实 Project port 后才查询本域，取消不能代替实际 Drain。Unknown 保留原物理 attempt／cause，安全格式不泄露 command key。当前只是完整 B02 的中间片段，没有生产 stub、完整 Documents 实现、迁移或真实业务接受。

Go 1.27.1、`GOPROXY=off GOSUMDB=off`、独占 GOCACHE、只读既有固定 modcache 下，`go test -p 2 ./internal/central/knowledge/...` 实际通过。首编曾因 Object marker 名误写失败，改为正式 `StoredObject`；随后测试 fixture 使用非法非 UUID owner 导致 `INVALID_TRANSACTION_CAUSE`，修为正式 typed UUID 后通过。当前 pure 只覆盖构造、真实调用返回前不能 Drain、多个调用退出、Unknown 私有因果与已有 B01；SQL／权限／并发和对象组合还未真实执行，不冒充已验。

第二片段补上数据库 title/UUID 分页、签名游标绑定与字面搜索、canonical reader 的实际 Close 跟踪、有界 UTF-8 读取、原 command Lookup 与本域六表／Knowledge Audit 增量迁移。分页及读取纯控通过，`go test -race -p 2 ./internal/central/knowledge/...` 实际通过；首批 race 97475 亦实际通过。新增代码曾误用 `InvalidCursor` 名称及旧 ObjectOwner 构造形状，均在编译检查暴露并修正为正式 API。迁移 00025 仅落盘，未执行 SQL 或升级；连续前序 00024 与完整服务、对象／树写入及真 PG 仍未闭合。返回流只有底层 Close 成功才退出服务调用登记；Close 失败保持未退役事实，不能把 cancel 或 wrapper 关闭标记当资源 join。

第三片段已实现完整 subtree 预览、10 分钟原签名确认、Move 的发现映射／私有 issuer／同 caller Tx 锁绑定及 standalone/atomic 入口。Move 重验当前 Owner、原命令 receipt 和新 Mutate gate；变更只写 parent/updated_at、不推进 content_version、不发 content event，首次真实变化才 TouchActivity。代码尚无真实 SQL 结论。新增实际源纯控覆盖 foreign issuer／Tx、锁取得一次、exact caller 与树映射漂移；限定 pure/race 15508 实际通过。测试前置遗漏 RequestID 及使用非法零 ScopeNode 曾导致两次 FAIL，补正式 typed 前置后通过；未改产品门槛。此前 vet 41924 实际通过，尚未覆盖本第三片段。Delete mutation、内容发布／恢复、Object/Audit/Event adapter 与真实集成仍待完成。

第四片段实现本域 Object Authority 与精确 cleanup checker、KnowledgeFile resolver：普通 Read/Mutate 核 same Store 活 Tx／已有锁／当前 Owner 的 exact Actor/Project 投影，prospective 仅从原用户的真实 planned create＋publication 发出；当前 Object 读取只认 canonical 指针。清理独立核持久 operation/project/document/reason/object/upload，不把普通 Owner Read 升为 Converge，不绑定整 Project lifecycle。来源只处理 KnowledgeFile 的精确 revision，其他来源交外层真实 provider；读取 Object 元数据后重验本域版本，InTx 消费原 Object plan/token，无内部新锁或 I/O。本片段仍没有提供清理执行循环、发布／恢复或完整 Documents。

新增纯控核 foreign Tx／缺锁／当前身份拒绝前不读本域、错 Session 投影拒绝、发现映射不等于授权、Project cleanup 不借文档 cause、resolver 同 Store 和旧 revision 拒绝。限定 race 14355 与当前 vet 实际通过（也覆盖第三片段）；测试使用受控 Store/Project，仅证明接线与拒绝顺序，未证明真实权限／SQL／Object 组合。adapter 首编曾误写私有 command marker 名，修成已存在 typed marker 后编译通过；不改变原业务门槛。后续实现持久 event/Audit、发布／恢复及 Delete，并安排阶段独审与真实 SQL。

第五片段补本域 ProjectAuditAuthority 和 Outbox ProducerAuthority，尚待接入实际执行链。危险删除 Audit 消费同 Store/Tx 私有见证，再核持久 completed receipt、完整原 scope digest/ID 集与实际最小 tombstone；原命令 key 使用与正式 Audit helper 相同的 canonical digest，调用者自报 count/公开 Entry 不能授权。事件发现读取本域固定 command_events，依 private issuer 绑定 exact Actor/Session、summary/command/锁；CurrentAccess 仍核当前 Owner，NewFact 另核 Mutate 及当前 canonical/tombstone payload，Move 不发事件。限定 race 4255、vet 与 diffcheck 实际通过，新增控制覆盖见证/issuer/cause/Session/原 payload；这不是 SQL 或真实 Audit/Outbox 接入验收。C4 Project 路由、内容发布、Delete 与恢复尚未闭合。

第六片段已接入完整树计划与 Delete 的 standalone/同 caller Tx 入口：发现阶段保存原确认语义及固定每节点事件/cleanup IDs；最终重新核当前 Owner、原 receipt、签名期限和完整树/对象/upload 映射，再于同一事务撤 canonical 指针、写精确 cleanup、调用 D05 ReferenceCleanup、逐节点事件、单 root Audit、安全 receipt 与 Activity。完成重放先于旧 token key/过期/旧 scope 检查，不重复副作用；完成时清除临时 request/plan，不把原 parent 快照留作历史。`TreeMutationPlanning` 与 `AtomicTreeMutations` 编译断言已齐，物理清理仍以 `CleanupPending` 和持久任务留存，执行循环待后续接入。新增纯控覆盖部分树/parent/version/project/object/upload 漂移及持久计划 duplicate/额外字段拒绝；race 74599 实际通过，随后仅 SQL 补清除 plan（SQL 仍未验），当前 vet/diffcheck 通过。以上不等于真实删除、回滚、Audit/Outbox/Object 接入通过。

第七片段已接入 `RecoverCleanup`：分页读取本域精确任务，按 reference/object/completed 阶段恢复；取消未附着 upload 使用原 Knowledge command UUID 作为私有 D05 key，再经正式 ReferenceCleanup 关闭旧 gate。只有真实 Cleaner 返回匹配 operation 的 completed 且无残留引用/lease 才写完成检查点；pending、端口 Unknown、错误 operation 或仍有 live reader 保持未完成，事务 Unknown 保留实际 attempt cause。调用纳入原服务取消/Drain，尚未实现内容 publication work claim 恢复，也不宣称 Object runtime join 已验。纯控 82874、race 53848、当前 vet/diffcheck 实际通过；控制只证明失败/完成接线和原因果保留，SQL、物理对象清理与完整 Documents 仍未真实验收。

阶段独审接受 `KnowledgeEvents.Valid` 向后兼容补口，并发现 00025 Knowledge Audit CHECK 遗漏 `correlation_id`／`http_trace_id` 必须为空；已最小补齐，与正式 `Associations{}` 九字段一致。未参与实现者对两字段差异及 SQL 三值逻辑、全部九项关联字段负控复核接受，原前序四 CHECK 表达式与 Variables 独立约束保留。此为静态／受控结论，fresh/populated/rollback 的真实 SQL 仍未执行。root 已将 Variables 冻结的 `00024_project_variables.sql` 精确导入本树，逐字差异为零，供连续迁移验收；该前序仍归 Variables，正式交付由其先入 main，不属于 Knowledge 作者实现或独立服务交付范围。

第八片段 `publication.go`／`publication_test.go` 已实现精确持久来源描述和工作 claim 内部口：正文/上传 reader 不入 JSON，读取描述不触发 I/O，原 BusinessFile／receipt/revision 与 source Project 保留；claim 绑定 Process/attempt/fence，只有已 joined 或真实 `ConfirmStopped(exactProcess)` 证据才允许接管，最终持久 fence 必须重验。纯控／race 5290 与 vet 实际通过，包括 live process 拒绝、旧 proof 漂移拒绝及跨 Project 初始锁 union。该片段尚未接通 Create/Update、实际上传、发布与回收全部阶段，不构成完整 Documents 或真实恢复结论。

首个真实 SQL 输入已准备为 `tests/knowledge/b02_fixture_test.go`／`b02_migration_test.go` 的单 top `TestKnowledgeB02Migration`，只复用现 task-owned PG fixture。三项分别覆盖 fresh、含前序 Variables/Audit 真实行升级与旧约束保留、末端失败回滚及同 checksum 恢复；另核九关联字段拒绝、最小 tombstone、全局 ID 与跨 Project parent FK。当前只编译/discovery，未执行数据库。上游 00024 的 `project.%` 排他 guard 缺口已由 Variables 作者修复并获独审，root 精确同步其冻结版后，本树重编 embed；不把已知前序缺陷当本域首次 SQL 结果。所用既有 PG-only driver 与正式 D08 有界 Wait supervisor 的可恢复来源及原预算见本树 current，不另造 fixture。

第九片段 `commands.go`／`commands_test.go` 补持久 content intent 内部流程：使用 B01 原语义摘要、快照 caller 的 parent/title/expected version 指针；当前身份后先认完成 receipt，新工作才核 Mutate/版本/父节点/全局文档 ID 占用和来源 Project，再保留固定 event header 与无正文 source 描述。title-only 真 no-op 只落安全 receipt，不伪造活动或事件；没有将该内部流程冒充已接通的上传／完整 Documents。首编暴露 import 补丁位置与旧 Event API 名误用，改回实际 `EventIdentity`／`DecodeHeader` 形状后 pure 64236、新增实际源控制和 race 31012 实际通过；最终前片段 race/vet 44212 亦已结束通过。content SQL 与最终发布仍待后续实际集成。

首轮迁移真实验证由本域作者执行，单 top 的 fresh、populated_upgrade、rollback 三子均 PASS：外层 88917 actual exit0（84.216s），Go 12.97s、driver 25.586s。Go/driver 实际 Wait0，精确 PG container/network 双退役、两次后代为空、runtime 仅 owned.json、host TCP 两次 delta_empty、冻结输入一致全部满足，窗口已释放。沿用上述 race-c15644 与原 driver/supervisor，无门槛或预算变更；原始日志为 `output/ai/knowledge/pg/pg-fdeeabd18252457fbadd478f314a2caa.log`。该轮证明冻结 00024→00025 的 schema/约束、有数据升级及失败回滚／原 checksum 恢复；不证明本域 content/Move/Delete 实际 SQL、当前权限、Object/Audit/Outbox 联合事实、独立验收或完整 Documents。其余未验边界继续保留。

C4 五技术路径已可构建：Project Audit 的 Knowledge 闭集／原 witness 委托以及 Outbox 精确双事件／双 stage 当前 gate 已接入，未改公开接口或安装生产 root。新增作者控制覆盖缺失/typed-nil provider、map 拷贝、当前初始化/生命周期/撤销 Session、foreign Tx/缺锁、原 Unknown/cause/ctx 保留，及完整 Actor/summary/issuer/purpose/锁集、双事件与非法闭集。首轮 58386 中一个无版本 stimulus 已被公共 Header constructor 拒绝，测试误期待进入消费者，修正为承认该拒绝；同轮广泛 `project/...` 既有 HTTP schema 测试因未提供固定解释器失败，保留该环境前置结果。限定实际修改包 `go test -race -p 2 ./internal/central/project ./internal/central/project/contract` 36007 actual0，随后相同包 vet/diffcheck actual0；没有重试或扩大业务预算。以上是受控 Project 接线作者证据，不是 Knowledge 事实或真实同 Store 联合 PG，待 Variables 作者未参与的有限独审及后续业务集成。

C4 随后已获 Variables 作者有限独审接受，无 mustfix；其本人实际源 overlay 两项控制 63234 actual0，覆盖改锁 mode、另 issuer、有效请求及原 ctx/Tx/key 委托；首个非法 ordinal 刺激已被公共工厂拒绝的控制前置 FAIL 保留。此结论仅是 Project 共享接线，不替代 Knowledge 事实、迁移或联合 PG。

第十片段补 publication 资源退休内部机制：准备/发送等同步回调真正返回后，按逆序关闭已登记来源、lease、spool；任一真实 Close 失败保持调用登记，成功项不重复关闭。全部成功才保存绑定原 Process/command/source Project/attempt/fence 的同进程 joined 证据并退出调用；不能从 cancel 或当前 registry 空推断。后续同进程重试可消费该精确证明，旧进程仍需真实 ProcessAuthority；过期 finalizer 不覆盖更新 fence。原命令完整锁下可将已证明 joined 投影到本域 work row，错命令/attempt 拒绝，Unknown 保留物理 attempt/cause 及本地证明，只有确证持久 joined 才删除内存项。没有新增后台任务、TTL 或绕过真实关闭。

新增实际源控制覆盖阻塞 Close 与 Stop/Drain、Close 失败后准确重试、source 指针快照、旧 fence 晚到、检查点 Unknown 与原命令匹配；race 80177、补检查点控制后 race 67327（1.080s）及当前 vet/diffcheck 实际通过。SQL 检查点使用受控 Store，仅验证调度/原因果，未跑此业务 SQL 或真实 Object/MinIO。该内部阶段尚未接入完整 Create/Update measured reserve/send/publish，不改变本卡未验收与 Object join 停止边界。

第十一片段补 title-only final 内部事务：固定事件先持久再发现 Outbox 完整锁，最终重读原命令／当前 Owner／内容版本，标题、content_version、pending 索引、事件、安全 receipt 与 Activity 同 Tx 提交；不重写可能由 Move 更新的 parent/source/Object 指针。完成 receipt 仍先于新工作 gate，final 失败不留下部分文档更新。新增实际源控制证明保留当前 parent／源／creator／时间、拒绝旧 version/错域/错误目标及 no-op 混入变更路径；race 52260 actual0、vet/diffcheck0。该函数仍为完整 Create/Update 的内部阶段，尚未公开接通或真实执行其 SQL，未宣称 Outbox 联合接受。下一最小 PG top 先验已闭合的真实 Owner/树读取/Move/Lookup，同步继续内容发布。

下一最小业务 PG 输入已冻结为 `tests/knowledge/b02_owner_tree_test.go` 的 `^TestKnowledgeB02OwnerTree$`，四子 current_owner、move_replay_and_cycle、title_pagination_and_paths、caller_tx_rollback_and_ended。真实 Account Authority→Project Authority 和 Activity 使用同一个 PostgreSQL Store；测试只在自有库预置 Account/Project/Document 事实，再由真实服务读写，不宣称 Project 创建 API、Skill 或 Object 内容发布已验。未消费的 Object/Source/Audit/Outbox 接口在测试中调用即 panic，Process stopped 口仅返回 Unbound，不能提供假授权或 join 证明。四子覆盖当前 Owner/foreign/撤销/初始化/归档、Move receipt/no-op/防环与稳定用户重放、标题 C/UUID 分页和字面搜索/完整路径、调用方真实回滚无树/receipt/Activity残留与 ended Tx 拒绝。

该 top 首次 race-c70973 actual0；静核测试前置后最终 race-c78547 actual0，精确 discovery 与 vet79200 actual0，尚未真实执行。binary 为 `output/ai/knowledge/knowledge-owner-tree-race.test`；精确命令和原资源预算见本树 current。该 binary 导入整个 Knowledge 领域，全部本域 Go 输入必须保持稳定至实际执行与完整资源尾；与早先不导入领域的 migration binary 边界不同。本轮只是作者 metadata/tree/Move SQL 验收准备，不代替独立风险组、完整 Create/Update/Delete 或 Object/Audit/Outbox 联合验收。

第十一 title-only 内部片段随后获未参与实现者有限独审接受：只读核两次完整 union、current Owner／receipt-first、最终当前 parent、固定 event 及最终同 Tx 副作用，独立 diffcheck0；复用作者 race52260/vet，不冒公开 Update、runtime 外层或真实 Outbox 组合。

首轮 `TestKnowledgeB02OwnerTree` 保留整体 FAIL：9063 actual exit1／77.036s，Go 2.88s、driver 15.918s。current_owner 在 archived current read、move_replay_and_cycle 在 archived safe lookup 返回 DEPENDENCY_UNAVAILABLE；后两项 title_pagination_and_paths、caller_tx_rollback_and_ended 实际 PASS。Go654203／driver653454 实际 Wait1，两精确 PG container/network 均双退役，desc 两次为空、host TCP 两次 delta_empty、inputs_unchanged=True；实际目录只剩 owned.json，真实窗口已释放。原件 `output/ai/knowledge/pg/pg-695d95ccfa5442f981d6be8c3658a387.log`，不将前两个已走过的子步骤补成完整子项 PASS。

只读定位到测试归档 helper 仅写 archived_at，未同步 updated_at；正式 ProjectRef.Validate 明确拒绝 ArchivedAt 晚于 UpdatedAt，Project scan 将此非法事实映成 DependencyUnavailable。限定修改仅本测试 helper：完成归档同时 version+1、archived_at／updated_at 取同一 statement_timestamp；产品和全部四组断言保持。正式 ProjectRef 的原组合负向／修后时间组合正向控制 84556 actual0（无 PG），修后 race-c83775 actual0、精确 discovery 通过。该修正尚未实际复验，原两个 FAIL 不回填。

该 helper 修正随后获未参与实现者有限独审接受；最终 discovery／integration vet39482 actual0。修后 OwnerTree32137 已完整 PASS，四子 current_owner、move_replay_and_cycle、title_pagination_and_paths、caller_tx_rollback_and_ended 全部通过；Go3.74s、driver16.367s、outer74.971s，Go666273／driver665634 actualWait0，精确 container/network 双退役、desc 双空、hostTCP 双 delta 空及 inputs_unchanged 全齐，runtime 现场仅 owned.json。日志 `output/ai/knowledge/pg/pg-2c6c60d0753f4f1aa06fa679990abe19.log`。原9063的两个失败与当轮事实原样保留。本结论仅为作者真实 Owner／metadata tree／Move／Lookup及caller Tx回滚验收，不代替独立风险组、完整内容发布／Delete或Object／Audit／Outbox联合验收；下一继续 Create／Update 发布链。

第十二片段在 `publication.go`／`publication_test.go` 接上内部工作 claim 编排及 text/upload 的测量准备。两个短事务各只获取一次完整原 union；在当前 Owner／来源 Project 与原 intent 后读取旧 claim，Tx 外核 exact Process 停机证明，第二事务重核原 attempt/fence 后登记新 work。Unknown 原物理 cause 与本次 work identity 保留给实际无 I/O 退休，未确认不得读取或发送。直接源描述须逐字同原 intent，流式 UTF-8 支持跨 Read 的多字节字符且保留原字节；实际长度、SHA 与 EOF 同正式 D05 prepared 元数据相符才可继续。Close 不阻断底层取消入口且保持原结果，provider 忽略的 Close 错误不会变成 join；返回句柄即登记 Discard，包括 provider 同时返回错误的情形。

作者控制覆盖分块合法／畸形／截断 UTF-8、文件二进制、长度错配、无 EOF／错误摘要或媒体的 provider、源所有权未提前消费、Close 真实中断与失败、原 Fault identity、claim live/stopped／证明后漂移／Unknown 原物理身份。另直接消费真实 D05 本地 `Spool.Prepare/Discard`，验证合法文本成功与畸形文本拒绝及 owned payload/manifest 真实清理；该测试只证明本地 spool 组合，不提供 Object Authority／PG／网络或 D05 全域 join 证据。首编因测试引用不存在的 `code`／`MustParseID` helper FAIL，修正为实际 `errors.As`／`ParseID` 后纯45694、claim纯87890通过；最终全领域 race／vet33144 actual0（中间24429、43082亦0），diffcheck0。本片段仍是内部接线，business-source lease、reserve/send/final 和 public Create/Update 尚待实现及真实业务验收。
