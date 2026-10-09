# D12 B02：完整 Human canonical 内容与文档树服务

状态（2026-10-09）：**B02有限Human Service库已实现，作者矩阵与独立风险补集闭合；待主线装配及共享Project路由组合核验。** 结果包括真实canonical内容、文档树、原子变更、来源复制、本域恢复和Object/Audit/Outbox事实适配器，迁移固定为`00025_knowledge.sql`。作者10top35sub及ProcessRecovery独立单top各按实际固定输入接受；未参与实现者的五个未变风险子组与修正末子82746完整PASS构成六组有限补集。原FAIL与环境终态缺口不回填。本结果不包含Knowledge HTTP/UI、下载URL、D13、B03完整Project lifecycle或生产root绑定；Object runtime join等停止项保持。

依据：[D12 主卡](d12-knowledge-documents.md)、[D12 规格](d12-knowledge-documents-design.md) §3–7 及本卡已接受 rev1。旧 `/tmp` 候选／报告路径仅是历史定位，不是本轮必需输入；当前实际 B01 源码与正式规格是恢复依据。代码、SQL 与受控依赖测试可先行；完整业务接受仍需真实 Owner／Object／Audit／Outbox 组合、并发恢复与资源退出证据，不能把测试替身当生产授权。Object runtime join 等原停止项保持，当前不改 Object 实现、App root 或公共 fixture。

### 当前集成接缝

- 经 root 授权，B01 `contract/events.go`／`events_test.go` 仅补向后兼容的 `KnowledgeEvents.Valid() bool`：零值 false、真实 Catalog 注册后 true，封存 Catalog 不使已注册类型失效。它只支持构造拒绝缺失依赖，不是事件事实或权限。未改编码／闭集，已获未参与者有限独审接受。

- C1/C2/C3 已有实际源码，保留只读，不重复实现。D05 的 `NewProjectAuditAuthority` 已存在；它的存在不等于 Project 已接入 Object producer，也不证明本轮 Object runtime 组合已接受。
- Project 外域 Audit 路由实际位于 `project/audit_facts.go`；本分支已实现 Knowledge/Object 精确注册和委托，公开接口不变。主线现有 Variables/Secret 等分支由装配者保留，按 §4 合并局部增量，禁止整文件覆盖。
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

当前交付范围以实际文件为准。生产15源均在 `internal/central/knowledge/`：`service.go`、`repository.go`、`commands.go`、`planner.go`、`publication.go`、`recovery.go`、`runtime.go`、`object_authority.go`、`source.go`、`cleanup.go`、`read.go`、`query.go`、`tree.go`、`events.go`、`audit_authority.go`。相邻9个测试为`service_test.go`、`commands_test.go`、`planner_test.go`、`publication_test.go`、`recovery_test.go`、`runtime_test.go`、`source_test.go`、`tree_test.go`、`query_test.go`。B01只另取`contract/events.go`及`events_test.go`的向后兼容Valid增量；其余既有契约不覆盖。

作者集成10文件均在`tests/knowledge/`：`b02_fixture_test.go`、`b02_owner_tree_test.go`、`b02_publication_test.go`、`b02_recovery_test.go`、`b02_migration_test.go`、`b02_audit_event_test.go`、`b02_concurrency_test.go`、`b02_runtime_test.go`、`b02_unknown_test.go`、`b02_process_test.go`。独立树另有`b02_independent_content_test.go`、`b02_independent_tree_reference_test.go`，由root随其实际监督/精确子例发现复验资产一并装配，不能误称它们已在作者树。正式SQL只有`db/migrations/00025_knowledge.sql`。

Project共享域只合`audit_facts.go`与`events.go`的Knowledge/Object闭集增量，以及新增`knowledge_event_authority.go`、`object_audit_facts.go`和`knowledge_audit_test.go`、`knowledge_event_authority_test.go`、`object_audit_facts_test.go`；`audit_facts_test.go`仅取对应注册期望。必须保留主线ProjectVariable、Secret、Model、Work与初始化全部分支及测试，不能从本旧基线整文件覆盖。主线Secret Audit新合同/HTTP/schema/client、app/config、Go依赖、D05后继bounded provider均不在本次装配范围。

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

### 当前有限Service库验收与装配边界

作者固定输入累计10top35sub＋Process50756单top已通过，含修后Runtime57974和公平Cleanup的实际组合；独立P1/P2源码风险审及15866定向复核已接受。独立风险补集采用89530中五个未变且实际通过子组＋82746修正末子，后者父2.45s/子0.62s、Go与driver实际Wait0、outer c5f341 exit0、19cd10确认7ID双退役/private/runtime/desc/TCP/input全齐；原件位于独立树`output/ai/knowledge-independent/pg/pg-9712da3407794d34aca1f45aa5bd6132.log`。覆盖DOCX与D05真实raw length/SHA拒绝、最终current authority、伪Audit/Event事实、正文/Move交错、preview成员变化、真实旧upload引用撤销与错误cleanup cause。89530整体FAIL、62452发现前置FAIL及作者旧失败保持原结论；不把分组合接受改成某一旧整轮PASS。

本域库验收门槛已闭合。剩余是root基于当前主线按§4装配、未参与者核必要共享路由组合及相关回归；不扩出新普通case矩阵。真实上游Account/Project规范seed与同Store Object/Audit/Outbox fixture的边界不变，不能外推为真实Login/Create、HTTP/UI、D13、全Project生命周期或生产root已绑定。

ProcessRecovery修正候选50756完整PASS：基于d7ae94a7测试＋f3ce0c7a记录、race-c73496/c3af32的 `knowledge-process-plan-fixed-race.test`，原root-chain与唯一 `^TestKnowledgeB02ProcessRecovery$` 未改；首同进程statvfs5716013056B。实际单top2.40s通过（无t.Run子例），覆盖原活Guard拒绝、精确owned SIGKILL/actualWait、原ID真实死亡证明、新attempt/fence和唯一canonical/Audit/Event及replay尾。Go1079008实际Wait0、driver1077003实际Wait0；本人726572外层actualexit0，53297e核7ID两轮14absent、private/runtime/desc双清、exact_tops/actual_test_wait=True、HOST_TCP双delta_empty、inputs_unchanged=True，supervisor terminal0/92.535s且无STOP。终态statvfs5713051648B，日志 `output/ai/knowledge/pg/pg-d776f88b697049b1853a3f24616ca380.log`。本任务资源完整退役，无命令在途；旧91700FAIL及当时未采counts/未到后半不回填。该作者阶段已通过原10top35sub外加Process单top；后继六组有限独立补集已按本节当前结论闭合，仍待主线装配。

Process375定向tests-only修正已落 `tests/knowledge/b02_process_test.go`：两个Prepare前/活guard拒绝后计数均要求commands=1、command_events=0，失败只输出两个安全计数；新processEventPlan独立查询原Project/key/document的planned且无receipt行，以正式严格ev.DecodeHeader核固定header的有效ID/时间、ContentChanged/schema1、Project/Document/version1，并比较live拒绝前后完整持久header不变。产品/fixture/harness均未改；94b3e6实际逆去这两处断言与新增header核验后逐字4b3caaf9，原live Guard、SIGKILL/actualWait、精确死证、新attempt/fence和canonical/Audit/Event/replay断言不变，gofmt解析/格式及diffcheck0。Work原审者e9d8c9只读逆验证actual0后独立有限接受：原Guard及Lookup后SIGKILL/actualWait/death/recovery/replay尾逐字4b3caaf9；未动态。基于d7ae94a7的独立候选 `output/ai/knowledge/knowledge-process-plan-fixed-race.test` 已race-c73496／c3af32 actual0，87497e精确发现恰TestKnowledgeB02ProcessRecovery，36526696B；旧77598 binary保留36519881B、含原失败测试，不冒充修正版。首1632d7因编译命令漏integration tag而build constraints排除全部文件，setupFAIL保留；补唯一必需tag后编译成功。该编译阶段只证明离线ready，未重跑旧六top；后继50756实际结果见最新记录，原91700未采具体两个计数及未到后半保持。

五top组合91700整体FAIL，窗口完整释放：精确selector `^TestKnowledgeB02(Cleanup|CommitUnknown|Concurrency|CleanupCommitUnknown|ProcessRecovery)$`，使用89b970d3两harness与原77598 candidate，组合映射经Model038551独立有限接受（两全文逆差异、8错selector拒、41observer控制；没有实测业务外推）。启动statvfs5702197248B；Concurrency3 PASS6.68s、修后Cleanup3 PASS10.67s（两Project公平子3.91s）、PublicationUnknown6 PASS16.45s、CleanupUnknown2 PASS5.46s。ProcessRecovery在2.40s于b02_process_test.go:375计划计数断言FAIL，尚未到same-key活guard拒绝／SIGKILL后恢复，不能称Process通过。Go1028097／driver1026096实际Wait1，outer a6b3b4 actualexit1／supervisor134.495s terminal1；e2bffa核7ID两轮14absent、private/runtime/desc双尾、TCP双delta_empty、exact_tops／actual_test_wait、inputs_unchanged全齐且无STOP；终态statvfs5699268608B。日志 `output/ai/knowledge/pg/pg-04554d791ca64ac680bc6498b1ccc816.log`。四组14子仅按各自真实断言有限接受，不改组合FAIL；P2当前SQL及公平清理已有真实证据，作者去重累计10top35sub，Process仍未接受。

Process首fail窄查：x.count第二值是command_events，prepareContentIntent在Prepare前只保存commands.plan固定header与planned publication；command_events到finishContentPublication阶段才形成。测试两处误expect第二值=1，拟改为0并核原plan/header不变及安全计数诊断。原91700未打印具体两个计数，不能回填当时值；此前确切planned/active work、零九项publication事实和Activity不变断言已通过。该首fail分析时尚未改产品/测试；后继测试修正/独审/新候选见本页最新事实，Process后半仍待freshgrant，不重跑无关已过四组。

修后Runtime57974已完整PASS：当前2287eca0产品/测试＋race-c77598 `knowledge-recovery-fixed-race.test`，TCP仅移植34206ea4诊断增量且经Skills f8fe7d有限独审，gate/预算不变。首statvfs5867839488B；三子7.82s（2.73／2.68／2.42s），Go1017066／driver1015085实际Wait0，outer c34712实际exit0，supervisor104.995s／terminal0。7ID/private/runtime/desc双尾、exact_tops／actual_test_wait、HOST_TCP双delta_empty、inputs_unchanged全部齐，29ade3复核无STOP；日志 `output/ai/knowledge/pg/pg-0a7b9115f922479997dc166ee227af3b.log`。窗口已释放，只接受修后Knowledge Runtime三子，不扩为Object全域停止或整个B02。

修后Runtime54818已有三子业务PASS6.90s和Go998510／driver996487实际Wait0，原日志7ID/private/runtime/desc双尾已清、精确top/Wait匹配；但环境切换后工具session消失，`pg-62f545ac57a847159cb2d1996080fa98.log`止于exact_tops，缺HOST_TCP双观察、输入不变和supervisor/外层终态，**不记完整PASS**。恢复只读复核7精确ID/PID/runtime双清及三private目录不存在，root确认当前窗口释放，不回填失去的baseline或Wait。产品/测试/新binary保持冻结；后续57974同输入新一轮完整通过不回填54818旧终态，原56777失败与TCP失败继续保留，公平Cleanup仍未实际运行。

P1/P2五源修复已获原独立审查者Skills有限接受，无剩余mustfix。实际源overlay15866 race0／1.044s（4top7sub）覆盖真实D05取消与release-error的Close/monitor、阻塞release仍不得Drain，65项分32+32+1及高低新项下一轮，Hard/Unknown及ResourceBusy+Unknown的原cause/attempt，扫描并发Busy无SQL且实际join。独验首15786为probe对opaque closure用DeepEqual的误判，按正式Equal/Details修probe后过，产品未改。此独审结论本身不含PG；修后Runtime已由57974完整通过，新分页SQL/Cleanup仍待实际，56777原FAIL与TCP缺尾不改。

修后新整包独立binary已race-c77598／六精确top发现dd5fce／domain与integration vet53340实际0，未覆盖旧产物。首全包race59862因新测试误假定UUIDv7分配单调而FAIL；只修受控ID前置，真实新子也改为持两reader后读取实际cleanup排序选择保留项，产品不改。修后全Knowledge/... race49004实际0；原Cleanup两子逆差异da8223逐字89613dc5。这些构建/纯控制本身不是业务证明；P1/P2另经Skills15866有限独验、Runtime另经57974真实通过，下一安排Cleanup受影响组及原未验异常组。

针对P2只在既有Cleanup top追加两Project真实子例：首精确live reader不阻塞后项物理删除，连续恢复保持一次Object Audit，Close后首项才完成；原两子及selector不改。该增量已编入77598修后独立binary，仍未真实运行，也不增加泛化harness入口。

两项本域返修的技术边界：typed canonical ObjectReader 的同步 Close 返回是本域流调用实际结束边界，返回的原读取/取消/释放错误仍原样上交；不将本域注销当 D05 lease 已释放或全域 join，也不改变 generic SourceInput/publication 的失败退休要求。RecoverCleanup 每次最多32项，固定一轮的最高持久ID与内存轮转位置，Pending不阻止同批独立项，hard/Unknown保持原结果并停原项，轮末重开新的有限上界；不新增worker、TTL或预算，内存游标不是完成证明。五源定向作者race13078实际0（9top11sub）；Skills15866已有限独审接受，Runtime57974已实际补验，新SQL/Cleanup仍待；旧预编输入不覆盖。

整体独审曾发现两项 mustfix，均由未参与实现的 Skills 作者用实际源码 offline overlay 复现（61981／99769 race0），不把复现 PASS 当产品通过：canonical `trackedRead.Close` 在真实 D05 Close 已实际 join 但保留取消错误时永不注销本域 call；`RecoverCleanup` 每次从首条开始且首 Pending 立即返回，使其他 Project 长期饥饿。root 已授权本域定向修复；Object runtime join 停项及全域停止责任不变。独立原件在 Skills 树 `.agent-state/knowledge-b02-review/`，源/契约/限制俱全。

原未修 Runtime 三子随后实际56777整体FAIL（Go14.81s）：canonical Stop/Close 子已观察精确reader lease released和Close原context.Canceled，最后Knowledge Drain超时；后两子实际Close barrier和取消后源lease后继退休通过。Go/driver实际Wait1、7ID/private/runtime/desc双尾及input gate齐，但hostTCP75s尾仍2行，无双delta_empty，外层exit1；不记完整窗口通过。终态后当前ESTABLISHED行归codex PID848，原baseline未持久，不能回填原delta归属或改写TCP失败。原binary与三子输入保留，不把未来修后源冒作本轮通过。

B02可单独交付的是§1的完整Human canonical／树服务库及真实适配器；当前原十个作者top／三十五子与Process单top（无t.Run子例）均已按各自固定输入真实通过，包含P2修后Cleanup三子及50756真实Guard恢复；91700组合原始FAIL仍保留。后继按下列有限闭包收敛，不继续追加无已知缺口的作者测试组：

1. **Process真实闭环已补齐**：四组91700有限通过及50756修正Process单top完整通过分别保留各自固定输入证据。Process原前置断言FAIL不回填；后继新候选已实际验证活guard、精确SIGKILL/Wait、same-command恢复及幂等终态。独立者审原生证据和关键判据，不无因重跑已通过组。
2. **完整库独立风险审查及有限补集**：Skills已核十五生产源／00025／四Project适配器并发现P1/P2，修复已获有限复核接受；整体有限独立结论现已结合当前真实组和六组风险补集闭合；下述为所执行的有限风险范围。由未参与实现者核当前权限→原command→完整锁→真实外域事实→最终同Tx→资源退休链，复用已有迁移、B01／分页及作者固定输入证据。独立真实补集按两类组织，避免重做全矩阵：内容／权限／事实类补DOCX与raw length/SHA拒绝的正式D05组合、最终当前授权变化及伪Knowledge Audit/Event事实拒绝；树／引用类补正文Update与Move交错不覆parent、preview后成员移入/移出导致旧scope拒绝、旧upload revoked后不能重新Attach及错cleanup cause拒绝。已有Activity终点失败已证明整Tx的Audit/Event回滚，不为每条SQL复制同类故障。Unknown与真实Process接管由独立者审查完整原生实测及关键判据，发现未覆盖风险才给精确最小反例，不预设新增泛化harness或更多生产功能。
3. **同一最终输入整合**：root保留Project `audit_facts.go/events.go`中的Variables／其它已交付相邻分派，以正式00024→00025及后继全局迁移顺序整合；不能整文件覆盖，也不改写已执行迁移。合并后只对实际改变的共享分派／构造及其直接回归运行检查；没有相关差异的已通过binary／场景继续复用，若生产闭包改变则明确重编和受影响复验。最终整理完整库原子交付与简短台账，不能以分支checkpoint当main已交付。

B02当前不需要新增正式port：现有Account/Project、Object/SourceReads/ReferenceCleanup/Cleaner、Audit、Outbox、ProcessAuthority均已有注入口和测试组合。KnowledgeFile真实来源已验；UploadedObject／Artifact等只消费各自已绑定正式provider，未绑定分支保持明确拒绝，不把本域resolver包装成外域成功。HTTP／UI／下载URL、D13、生产root和B03全Project participant属于已排除的后继范围；其缺失不阻塞这个库交付，也不能在交付时声称已绑定。Object runtime join停项、共享guard至多域实际join／DB最后、Project lifecycle cleanup／source_project join留待B03及上游正式组合，不在B02中偷偷解停或造成功stub。

## 8. 本轮实际实施与验证

第二十九片段新增已授权 `tests/knowledge/b02_process_test.go`／单 top `TestKnowledgeB02ProcessRecovery`，补齐可选真实 Runtime fixture。每实例的 Account／Project／Knowledge／D05／Audit／Outbox 仍同一 Store，真实 `NewRuntime.Initialize`绑定同host／spool claim。旧 owned child 的 Knowledge work claim 确认提交后，在委托 Prepare 前阻塞；只经 D05 Service.Drain 释放 spool 目录锁，bound Runtime Guard仍持独立claim flock。新实例必须调用原Guard核旧ID并得到 ResourceBusy，planned work／当前Activity及零publication副作用保持；这一步不把Service.Drain或unbound Close视为死亡证明。随后只精确SIGKILL已启动child并收实际Wait／SIGKILL状态，Guard对原ID给真实停机证据后，原命令以新attempt／fence恢复一次canonical／Object Audit／Event，完成receipt重放不得增加Prepare、停机证明或事实。私有pipe／0600 config避免命令key与fixture凭据进入输出；没有产品源码／root修改，也不恢复Object runtime join停项。

本片段只完成离线准备：首编误选旧modcache setupFAIL，纠正到固定只读缓存后通过；最终race-c36066、精确top discovery与integration vet0eaf3f实际0，独立binary `output/ai/knowledge/knowledge-process-race.test`。fixture剥新Runtime分支后全文逆比较558303逐字c73054c3，既有Direct／Business测试体未改；原五个待真实binary未重编，各自原输入和结论保持。初次可构建阶段未配置exact入口、未独审或启动真实PG／Process。随后Work UI作者对96049336测试／e04a9e5可选fixture有限独审接受，无mustfix，结构控制c905f0实际0确认旧11函数含Direct/Business两top逐字不变、只有nil包装／唯一新top、无SQL伪造claim/stopped；复用作者race/list/vet。原两harness随后各+1精确selector／singleton，作者167b19离线全文逆差异、config1正4负、11target一致／原预算与observer1正4负实际0，Skills随后独立有限接受，无mustfix；其实际config1正4负、11target／原预算及observer1正5负（各14次资源观察）控制exit0，未跑真实资源。没有运行真实PG／Process，不称跨进程恢复、Object join或整个B02通过。

修正Cleanup恢复首启27247是环境前置SETUP FAIL，fixture编译缺固定GOMODCACHE、GOPROXY=off拒绝模块lookup；没有业务top、Go PID或7资源记录，不能报业务或资源退役通过。driver931240实际Wait1、desc双空、runtime双empty、TCP双delta_empty／inputs_unchanged齐，outer1.498s，现场run仅request.json与空runtime。完整含环境复现命令已写current；其后freshgrant99411沿原binary／预算完整PASS，结论见下，不回填本次失败。

修正Cleanup99411两子完整PASS：live reader／归档清理4.07s、final rollback／原key-token续接2.39s，Go6.46s；Go934577与driver932488实际Wait0，外层actualexit0／supervisor106.388s，7精确ID双退役、private双absent／runtime双empty、desc双空、exact_tops／actual_test_wait=True、hostTCP双delta_empty、inputs_unchanged=True齐。现场两PID不存在、run仅owned.json/request.json/空runtime，窗口已释放，原件 `output/ai/knowledge/pg/pg-54a84fc5001f41ab9ba1f25c4951c0ee.log`。本轮确核Delete在真实live reader下pending，实际Close后物理对象与Audit仅一次收敛；final失败全回滚但原独立准备计划仍durable planned／Lookup InProgress，原key/token恢复一次且重放无额外副作用。原10056整体FAIL、未采state及后半未执行事实仍保留。作者累计6top/20sub已真实通过，不扩写为Runtime、Unknown、Concurrency、跨进程恢复、独立风险验收或整个B02接受。

第二十八片段在原 `b02_unknown_test.go` 追加独立 `TestKnowledgeB02CleanupCommitUnknown` 两子（not_forwarded／committed_ack_lost），原发布Unknown六格及其旧binary不改。复用同一正式generic commitproxy、原同Store真实fixture和exact writer终局观察；仅匹配RecoveryCause owner=knowledge.cleanup／原cleanupID／空checkpointRef，原callback成功后实际SQL为completed且对应D05 Object已deleted才Arm。分别核真实checkpoint保留object或completed、原Store Unknown attempt/cause原样传到公开RecoverCleanup，后继恢复只一次Object删除Audit、无新Activity/Event、原Delete receipt不变。race-c88666、精确discovery恰1、integration vet60106 actual0，独立产物 `output/ai/knowledge/knowledge-cleanup-unknown-race.test`；原六格全文逆比较9355af通过（首控制多加一个换行setupFAIL后只修control）。两既有harness仅新selector +1/+1，作者335477实际0（两逆差异逐字9f9d3026、真实config1正4负、10target两表一致、无runtime创建），预算／7resources／实际Wait门槛不变。Skills独验5e23c6有限接受入口差异：全文逆差异、config1正4负及10target映射实际0；仅证明选择与原监督门槛保持，不替代两子业务真实验收。新top仍待真实窗口，不扩大旧六格。

Cleanup修后仅单测试新增planned SQL／InProgress且无completed receipt期待，保原全部回滚门槛，并用原key/token续接，核1删除Audit／2待清任务／2事件及committed receipt，再次原命令重放不增事实。race-c63832、exact list恰1、integration vet53137 actual0；`knowledge-cleanup-race.test` 现为修后版本，原31200／10056失败输入保留Git来源。未参与者Runner作者对7b2012d1→9f9d3026单源+29/-2有限静审接受，未PG，不回填原未采state。Concurrency两harness各+1已保存，Skills独验f40dec实际控制0有限接受（逐字逆差异、config1正4负、原observer尾5正负及9target一致）；原预算／Wait／7resources不变。首次作者harness自检AST误选另一expected变量setupFAIL后仅修控制，b483f9实际0。Cleanup修后、Runtime三子、CommitUnknown六子、Concurrency三子全部仍待真实验收。

首轮Cleanup10056完整FAIL：live-reader／归档物理清理子PASS3.30s，final_transaction_rollback子FAIL2.66s停原b02_recovery_test.go:176，公开Lookup调用err=nil但NotObserved／无receipt组合断言未满足；原输出未采具体state或receipt，不能补认。之前原错误身份、两文档与正文、Audit／cleanup／Outbox／Activity回滚断言已过，后面的canonical引用SQL未执行。Go5.96s／877621实际Wait1，driver875637实际Wait1，外层actualexit1／supervisor104.943s；7ID双absent、desc双空、private双absent／runtime双empty、exact_tops／actual_test_wait=True、TCP双delta_empty、inputs_unchanged=True。现场两PID不存在、run仅owned.json／request.json／空runtime，真实窗口已释放。原件 `output/ai/knowledge/pg/pg-9026831a61184f469bfae59f110db784.log`。静核发现DeleteSubtree先由DiscoverMutation独立Tx持久保存planned command及固定节点计划，再进入final Tx；原测试把final回滚等同整个命令NotObserved，与该正式持久准备流程错位。只准备最小测试期待修正及原key/token续接断言，生产未改、未修后复验。

第二十七片段新增已授权 `tests/knowledge/b02_concurrency_test.go`／`TestKnowledgeB02Concurrency` 三子，当前仅可构建：同key两Session在原Command EX实际阻塞后只允许一次publication及同用户receipt；两个不同Owner／Project争同全局DocumentID，在真实reference EX阻塞后要求foreign NotFound、Lookup not_observed及零目标副作用；两份公开Move plan原同Tx完整锁集竞争，观察最先冲突的User EX而不伪造tree-only锁，提交后重验祖先链拒绝相反Move成环。每格实际PID／key／mode／holder／waiter与pg_blocking_pids必须同时匹配；仅持原成功callback至真实COMMIT前，不替换权限／锁／CommitResult。race-c74688、exact list恰1、integration vet85320及diffcheck actual0，binary `output/ai/knowledge/knowledge-concurrency-race.test`；未配置新exact target、未运行PG，无并发通过结论。

第二十六片段把 Runtime 扩为三子：新增真实大正文 canonical Open，先证精确 reader lease active，Stop 后观察该 lease 实际 released，再核底层 Close 真实返回并保留取消错误，最后独立要求 Knowledge Drain 完成。lease 退役或非空 Close 单独均不当 join；未改 Knowledge/Object 产品，静态疑点尚未实证，不推 Object runtime 停止项。新 race-c21334、单 top discovery 与 integration vet96931 actual0，binary `output/ai/knowledge/knowledge-runtime-race.test` 已替换旧40889二子产物，真实运行待窗口。Cleanup／Runtime／CommitUnknown 三个 exact singleton 已在原两harness各增三行并保存；原独验 c43f90 的32个离线控制接受，8target与原预算／资源／Wait门槛不变，三业务组仍未实际运行。

第二十五片段新增 `tests/knowledge/b02_unknown_test.go`，直接import root从正式3cea6076精确导入的 `.agent-state/project-variables-independent/commitproxy`，没有复制作者private代理或另造协议实现。单top `TestKnowledgeB02CommitUnknown` 拟对plan／reserve／final各验两结果：只在本命令真实callback已成功、SQL阶段匹配后Arm实际backendPID，原Store返回原生Unknown与attempt；COMMIT帧保持未转发时，确核同owned DB／application的writer后终止并观察实际退出，随后Release及HeldJoined且不得见Committed；另一格Release后须真实COMMIT/ReadyForQuery与HeldJoined。原同Store Account／Project／D05／Audit／Outbox全装配不变，独立连接观测实际writer，Lookup按原持久结果区分not_observed／in_progress／committed，原命令续接只产生一次canonical／Object Audit／Outbox。回调、CommitResult与SQL结果均未替换；不声称cleanup checkpoint、跨进程恢复或任何动态PASS。race-c89980 actual0、exact list恰1、integration vet49164与diffcheck0；binary `output/ai/knowledge/knowledge-unknown-race.test`。该组尚未配置精确harness target／独审消费接线或获真实窗口；后继按阶段有限独审和原预算排期。

作者首轮 BusinessPublication21607 完整 PASS，四子 exact_revision_copy_and_replay／reuse_title_and_replacement／source_revision_rechecked_in_final／source_owner_and_archived_read 全通过；Go5.82s、supervisor103.328s、外层actualexit0。Go848330与driver846451实际Wait0，7个精确ID双退役、desc双空、private双absent／runtime双empty、exact_tops／actual_test_wait=True、hostTCP双delta_empty、inputs_unchanged=True；现场两PID不存在、run目录仅owned.json／request.json／空runtime，窗口已释放。原件 `output/ai/knowledge/pg/pg-1769e4413d8248219aba63a1b282cdfc.log`。消费原40693 binary／第22生产源与已审Business精确target；实证公开KnowledgeFile copy、真实source lease／GET／Close、原revision与receipt重放、no-op／同内容标题／不同内容替换的原opaque cleanup计划、final来源变更拒绝和当前Owner／归档读取。不升级为独立验收、COMMIT Unknown、Delete／物理cleanup、本域Runtime或Object停止项通过。

Business完整尾后仅测试前置继续准备：Cleanup原短正文在D05 Open时可能已预读至EOF并释放lease，故改为2*StreamBufferSize+1字节，且删除前独立查询精确Object的active reader lease必须为1；原pending／Close／归档收敛断言不动。该组从未真实运行，此为前置修正，不捏造产品或测试动态FAIL。按root授权从原b02_owner_tree_test.go及b02_publication_test.go各提取一个内部constructor，原入口仍按原路径组装同Store；两项实际逆差异控制证明移除提取后全文逐字16354ead，未复制Account/Object fixture或替换权限。新Cleanup race-c31200 actual0，精确list恰1、integration vet17397 actual0；Business旧binary未重编，原已验版本不冒新fixture复验。Unknown后继可在新入口绑定真实COMMIT代理，目前未实现/运行；Cleanup/Runtime精确harness入口仍待。

第二十四片段新增 `tests/knowledge/b02_runtime_test.go` 的 `TestKnowledgeB02Runtime` 两子，只复用冻结真实D05组合。stop_waits_for_actual_delegated_close 仅在真实ObjectReader的Close前设置可释放barrier，核Stop不使原公开调用返回、取消预算的Drain不得成功，实际Close返回后调用和lease真正退役且不发布target；D05可在已验证EOF时自行释放lease，故不声称lease必定等wrapper Close才释放。cancelled_request_retains_unopened_lease_for_live_retry 在真实lease取得后、GET前注入原失败并取消请求，要求原错误不被cleanup替换、原lease仍active，后继live Drain用自己的context实际释放，再same-command以新真实lease恢复一次发布。所有权限／Acquire／Cancel／GET原委托，无成功授权替身；不证明SQL COMMIT Unknown、Process死亡或全域Object join。初编64996及核对D05 partition_id列后的最终race-c40889 actual0，exact list恰1、integration vet21350与diffcheck0；binary `output/ai/knowledge/knowledge-runtime-race.test`。未配置Runtime精确harness target、未获真实窗口或执行，该组不改变Business40693的冻结生产/测试binary。

第二十三片段新增 `tests/knowledge/b02_recovery_test.go` 的单top `TestKnowledgeB02Cleanup`，仅准备真实同进程Delete/cleanup两子，不修改冻结Business生产源或binary。deleted_subtree_waits_for_actual_reader 使用公开真实创建／过期scope拒绝／精确子树删除与tombstone，持有真实OpenCanonical reader时必须保持pending，实际Close后再恢复并核两Object删除Audit及持久completed；项目归档后仅既有cleanup收敛／固定receipt重放，树外文档保持可读。delete_final_transaction_rollback 在真实Activity调用后注入失败，要求文档／canonical引用／Audit／Outbox／cleanup／receipt／Activity全回滚。没有以missing process代替stopped、没有SQL COMMIT Unknown或跨进程join结论。race-c38705及gofmt后最终race-c51008 actual0，精确list与integration vet30594 actual0；binary `output/ai/knowledge/knowledge-cleanup-race.test`。该新组尚未配置精确harness入口、未获真实窗口、未执行SQL／D05，不能标cleanup通过。

第二十二片段修正公开 business 替换组合的计划选择：`commands.go` 的完整 union 原顺序为 publish、可选 source、cleanup，旧 `plans[1]` 在带来源时误选 source；现保存 DiscoverAccess 原始 opaque cleanupPlan 并原样传入 ReleaseForCleanupInTx，不重新构造计划、不改变锁集合或 Tx。此缺口是在新分支尚未真实运行前静核发现，不捏造历史业务 FAIL。实际源选择表达式投影配真实 D05 opaque Plan／LockedAccess，旧66688 actual1在 source=true 失配，修后79711 race0／1.021s；两种来源模式都保原cleanup，错purpose、同字段重新mint和foreign issuer均拒。首两次62871／25435控制误用Access构造器setupFAIL保留，只更正ignored刺激，不计产品反例；该有限投影没有执行完整final SQL。

新增 `TestKnowledgeB02BusinessPublication` 四子与原 DirectPublication 三子共用 `tests/knowledge/b02_publication_test.go` 的真实同Store D05 fixture，原三子保持：exact_revision_copy_and_replay 验跨Project同Owner精确副本／源真实升版后固定receipt／新旧revision拒；reuse_title_and_replacement 验完整 no-op／同Object改标题及不同内容Update通过source+cleanup两计划；source_revision_rechecked_in_final 用真实来源Update在原Outbox计划后交错，目标final必须拒绝而无canonical事实；source_owner_and_archived_read 验foreign来源无lease及归档Owner读取允许。源lease要求真实存在过、无active残留且目标持久lease checkpoint已清，未用成功替身或假Reader。

最终业务binary `output/ai/knowledge/knowledge-business-publication-race.test` race-c40693实际0，精确发现恰单top，全 Knowledge／contract race2294实际0（领域1.219s）、integration+domain vet7979／diffcheck0。新增controller+cleanup已获未参与实现的Variables作者有限独审接受：实际核receipt-first、confirmed lease后Open、实际Close和精确checkpoint先于final、原错误/Unknown及最终source重验；两项逆差异控制actual0，cleanup继续传原Discover返回的opaque计划。此结论不代替Business真实PG。两harness经root授权各仅增加Business精确selector一行，独验7958ca actual0有限接受（逐字逆差异、真实configuration与四种非法selector拒绝）；原Direct/Work、预算、资源与未知输入门槛不变，未来合main须保留Variables新增targets。 未启动该组真实资源，不得把编译／有限控制称为business或cleanup完成。

第二十一片段 `service.go` 将已审内部业务来源阶段接入公开 Create/Update：仍先 current Owner／固定 receipt 和原 claim；精确 Resolve 后只在同 Tx lease 确认成功才 Open/Prepare，实际 reader.Close／CancelSourceLease 与精确 lease 检查点成功后才复用或 reserve/send。最终发布与同内容改标题／完全 no-op 均传递同一 resolved source witness，保持原 final 同 Tx重验证；各阶段错误/Unknown仍原样停止，重放不读取已失效来源。direct/text/title-only沿原逻辑且不新增resolver调用；未绑定的外域来源仍由真实 provider fail-closed，不补allow。全 Knowledge／contract race60692 actual0（领域1.270s）、vet29312 actual0；这是公开编排的可构建接线，尚未获本新增单源有限独审／真实 KnowledgeFile lease/GET/final验证，不等同business-source已接受。下一复用本轮真实 D05 fixture准备单独业务来源组，不改原DirectPublication三子结论或Object停止项。

作者首轮 DirectPublication12869 已完整 PASS：三个子项 create_read_replay_noop、replace_and_current_owner、final_transaction_rollback 均通过，Go2.94s，supervisor108.615s／外层 actualexit0；Go815307 与原链 driver813448 实际 Wait0，7个精确资源 ID 双次退役，desc 双空、私有目录双 absent／runtime 双 empty，exact_tops／actual_test_wait=True，hostTCP 双 delta_empty、inputs_unchanged=True。现场已核两PID不存在、runtime空，run目录仅 owned.json／request.json／runtime，窗口已释放。原件 `output/ai/knowledge/pg/pg-4826df79aa8d4497b3238c83ddd3ee14.log`。本轮在57fcf5d7生产闭包上实证公开文本创建／真实GET与Close／重放和no-op、PDF上传替换与旧引用退休／Owner和归档门禁，以及最终Activity注入失败后canonical、D05发布、Audit、Outbox、receipt同Tx回滚；不是独立验收、业务来源、SQL Unknown、物理Object清理或D05 runtime join接受。消费闭包本轮已解除冻结，后继source变更不冒同输入重验；继续business-source公开组合。

第二十片段准备单 top `^TestKnowledgeB02DirectPublication$`，新源 `tests/knowledge/b02_publication_test.go`。只预置自有 Account/Project 上游事实；Knowledge、真实 D05 backend/spool、Project 的 Knowledge/Object 私有 Audit checker、真实 Audit 与 Outbox 共用同一 Store，未使用成功授权替身。三子 create_read_replay_noop、replace_and_current_owner、final_transaction_rollback，目标为文本创建／实际 GET/Close／重放与 no-op、PDF 替换与旧引用退休／当前 Owner/归档门禁，以及末端真实 Activity 后注入错误证明 canonical/Object 发布/Audit/Outbox/receipt 全回滚。Process death 口仍 Unbound，未消费 SourceReads 不代替 business-source 真实验收；不声称 SQL Unknown、D05 runtime join 或物理清理完成。

原两 harness 经 root 追加授权：`.agent-state/work-owner-http/root_chain_driver.py` 和 `.agent-state/task-planning-recovery/pg_only_supervisor.py` 各只增一个精确 selector 的 cwd／expected singleton；已保存 7a3e453b，有限独审65885d实际12控接受，原缺本树MinIO的94a7bb setupFAIL保留。原 Work 默认与未知输入拒绝、6m test／540+60 supervisor／75TCP／5GiB／原7资源链不变；未来主干合并必须同时保留 Variables 后继 targets，不整文件覆盖。

DirectPublication 首编62670 actual0；核对正式 D05 列名/bytea编码/available、committed/attached 与 canonical 引用后，最终 race-c80125 actual0，精确 list 为唯一该 top，integration vet50722 actual0；只构建／静态验证，尚未真实执行。固定 MinIO 从已核只读 cache 精确复制到本树 `output/ai/deps-minio/bin/minio`，实际 SHA 为 `dc5298474f0bc87a068f0b1135c583bb1278c17c11c512212ed7644a238c89c8`，未执行或读取私有descriptor。实际 adapter --check 已通过 cwd／binary／固定SHA／6m／7resources；当前别树真实7资源未退出前的磁盘不足5GiB现场不解释为最终准入，必须全尾后重新核门槛再申请 freshgrant。全部当前生产 Go、三子与 compiled binary 保持冻结；精确启动命令见本树 current。

作者公开 TitleContent98064 四子完整 PASS：Go2.44s、driver15.208s、outer74.861s actual0；Go751688／driver751026 实际 Wait0，精确 PG container/network 双退役、desc 两次为空、hostTCP 双 delta_empty、inputs_unchanged=True，owned 目录现场仅0600 owned.json，真实窗口已释放。日志 `output/ai/knowledge/pg/pg-0cbb0132059c4b24a93e0b8e507e0098.log`。本轮实证公开 title-only／真实 Account→Project→Knowledge producer→Outbox／固定 receipt与事件／final Activity 后全回滚／Move 阶段交错及撤销门禁；不替代完整正文发布、business-source lease、真实 D05／cleanup、SQL Unknown 或独立风险验收。

第十六片段修正精确 attempt 的本地 join 证明。纯控制73279发现原 claim 缺行后新物理 attempt 可仍为 fence1，旧 command-keyed map 会丢失该次真实退休；原 FAIL 保留，未执行真实 COMMIT Unknown。正式源改为 attempt-keyed 并继续核完整 command/project/process/source/fence；旧 finalizer 不覆盖新证明，durable checkpoint 只删除精确原 attempt。作者正式全域 race29773／vet0；未参与者实际源 overlay35619 三子 race0，覆盖同 fence 三 attempt 的逆序／重复 join、同 attempt 的身份漂移与旧 checkpoint 删除边界，有限接受。其首次 ignored 控制包 import 错误的 setup FAIL 保留，修控制后通过，生产源未改；仍不证明 SQL Unknown 恢复。

第十七片段 `source.go`／`source_test.go` 增加 business-source 解析和同 Tx lease 内部接线：先绑定原 intent／work／精确 BusinessFile reference，在 Tx 外 Resolve；完整 target/source 锁 union 一次获取后重核当前 Owner、原 work 和持久 source，再用原 Tx 的正式 SourceResolver／SourceReads 取得 lease。未确认事务不 Open/read；任何有效返回句柄均登记退休，包括 Unknown／错误同时返回，取消失败保留调用。十项实际控制15717 race0及全 Knowledge／contract race5907 actual0、当前 vet0，包含 reference／media／revision／Owner／work 负控、原物理 Unknown 与错误 cause。首25196因复用 fixture 漏投影 source Project FAIL，仅修测试行解码后通过。此为可构建内部阶段，未独审、未 SQL／D05 动态；公开 business-source 仍 Unbound，后续须实际 Open／字节校验／Close 与最终源 revalidation，不称完整 Documents。

第十八片段 `publication.go`／`publication_test.go` 增加业务源租约开流与实际测量内部口：只调用正式 OpenLeasedSource，原 actor／opaque lease 不变；返回 reader 即登记 Close，含 error-with-reader。完整 Object meta 与无范围读在触字节前校核，真实 D05 本地 spool 消费原字节，UTF-8／长度／SHA／EOF 全满足且实际 Close 成功才返回 prepared；错误返回的 prepared 仍 Discard，Close 失败不冒 join。十四项限定 race33349 actual0、全 Knowledge／contract race36422 actual0、当前 vet/diffcheck0，含 PDF 二进制正例及错对象／scope／version／digest／range／不读到EOF／错误带句柄／Close失败／未开始拒绝。仅使用本地 spool 与受控 SourceReads，不证明真实租约 commit／外部 GET 或 final 重验证；public business-source 仍 Unbound，下一闭合源租约清理检查点与 final/reuse 的同 Tx source gate。

第十九片段 `source.go`／`source_test.go`／`commands.go` 闭合租约清理检查点与最终 source gate。实际 CancelSourceLease 成功后，在原完整 command/work 锁下只清该 command 的精确 lease UUID；取消失败、当前门禁／work 漂移或测量不符不清行，Unknown 保留原物理 attempt。final publish、同内容但改标题及完全 no-op 三条最终事务都把原 source plan 合入一次锁 union，并在 canonical／receipt 变更前重验原 resolver，不隐式解析 latest；direct/title-only 保持原无 resolver 路径。新增有限十五项8379 race0含清理 Unknown/拒绝及原 Tx/source gate 正反，全领域45265 race0与当前 vet/diffcheck0。尚未公开接通 business-source，也未验这三条最终 SQL 分支或真实 D05 联合事务；组合阶段待未参与者独审，之后接公开编排并准备真实组合。

第17–19组合有限独审发现一项 mustfix：source lease 的退休闭包捕获原请求 ctx；有效 lease＋Unknown 后请求取消，真实 D05 CancelSourceLease 会在 ctx.Err 门禁先拒，原实现没有本地后继清理责任。独立实际源控制53101 FAIL保留，不能由先前忽略ctx的纯端口结论抵消。现最小返修把 context-sensitive 资源保留于精确 attempt 的待退休项，仍登记原调用；原 defer 或后继同command恢复／Drain 只用各自实际 ctx，没有 Background／新 timeout／延长原预算。取消失败不join，精确清理成功才保存原 attempt proof并退出原调用；并发第二清理不阻塞在另一 Close mutex，也不重复调用底层。原 Unknown/cause 不改，租约持久检查点不因本地退休补成成功。作者取消敏感控制22990、同组加精确attempt与并发责任控制16605实际race0，全 Knowledge／contract57356 race0、vet97397/diffcheck0；旧 `.join` 相邻测试只显式传原控制context，原断言保持。原独审者随后以实际源 overlay86295 race0／1.029s 有限接受返修：旧取消ctx失败保句柄、新ctx原样传入、成功Close不再调用、重复登记不替换原done、错command无动作、实际完成才闭合原call/map/proof。仍未SQL／D05联合验收、public business-source未启用，Object runtime join停止不变。

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
