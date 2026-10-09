# D12 B02：完整 Human canonical 内容与文档树服务

状态（2026-10-09）：**按已接受 rev1 契约开始 B02 实现，尚未验收。** 独立树 `ai/knowledge-service` 从正式 `f1c94ee5` 开工；B01 的 12 个纯契约文件、C1 `cursor.Text`、C2 Knowledge Audit 闭集、C3 精确 ReferenceCleanup 已在基线中存在。§4 的 29 个服务／测试路径无可恢复旧实现，本轮由唯一作者实现；首批服务构造、调用登记／取消与 Drain、严格 canonical 行读取和当前事实口已可构建，完整 Documents／发布／树变更尚未闭合；本域迁移独占 `00025_knowledge.sql`，`00024` 为另一树 ProjectVariables、`00026` 为 Runner control，不能按本树当前最高编号重排。

依据：[D12 主卡](d12-knowledge-documents.md)、[D12 规格](d12-knowledge-documents-design.md) §3–7 及本卡已接受 rev1。旧 `/tmp` 候选／报告路径仅是历史定位，不是本轮必需输入；当前实际 B01 源码与正式规格是恢复依据。代码、SQL 与受控依赖测试可先行；完整业务接受仍需真实 Owner／Object／Audit／Outbox 组合、并发恢复与资源退出证据，不能把测试替身当生产授权。Object runtime join 等原停止项保持，当前不改 Object 实现、App root 或公共 fixture。

### 当前集成接缝

- 经 root 授权，B01 `contract/events.go`／`events_test.go` 仅补向后兼容的 `KnowledgeEvents.Valid() bool`：零值 false、真实 Catalog 注册后 true，封存 Catalog 不使已注册类型失效。它只支持构造拒绝缺失依赖，不是事件事实或权限。未改编码／闭集，待未参与者窄审。

- C1/C2/C3 已有实际源码，保留只读，不重复实现。D05 的 `NewProjectAuditAuthority` 已存在；它的存在不等于 Project 已接入 Object producer，也不证明本轮 Object runtime 组合已接受。
- Project 外域 Audit 路由实际位于 `project/audit_facts.go`，当前仅接纳 Secret、Object 仍明确 `DEPENDENCY_UNBOUND`；Knowledge 的 C4 路由尚缺。§3 C4 中的旧路径推定须在真正接线前更正并另取共享写域，不能依历史文件名直接修改。
- Project Outbox 现有 `project/events.go` 已接 Model／Work 专口，但没有 Knowledge 或通用 Human gate；历史 `outbox_authority.go` 候选不是现存输入。B02 自有 producer 依正式 `outbox/contract` 实现，生产组合前补齐真实 Project gate；不安装 allow adapter。
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
| Knowledge 删除 Audit | C2 action/resource/producer/metadata 闭集已在基线；Project AuditFacts 仅 Secret 可注册 | §3 闭集+Knowledge 自有事实 checker；P冻结后 root 交接 `project/authority.go` 与 `project/audit_authority.go`，限定接纳 KnowledgeProducer。B 当前拥有 Audit types/metadata/service，必须等其冻结，不能并写。 |
| Knowledge Outbox | B01 typed events 已验；固定 `project/events.go:272/293` 仅接受 producer=project | 依赖 P 正在实现的 `project/outbox_authority.go` 通用 Human gate 独立验收：完整 summary/stage/private issuer、CurrentAccess=Read/NewFact=Mutate、User/Project union。不新建临时 Project adapter；Knowledge只实现自己的 `ob.ProducerAuthority`。 |
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

**C4 P接续，旧2+新1，必须等P冻结并另授权。** P已定 `AuthorityDependencies.AuditFacts map[audit.Producer]audit.ProjectFactAuthority` 与 `NewAuthority(store Store,d AuthorityDependencies)`。旧 `internal/central/project/authority.go` 仅把允许注册的闭集增加 KnowledgeProducer并拒nil；旧 `project/audit_authority.go` 的外域路由增 KnowledgeDeleteSubtree/Human当前Owner/当前Mutate/ProjectAudit屏障校验，再调用配置的Knowledge checker；新 `project/knowledge_audit_test.go`。已有 Secret/Object 路由、服务权限、Audit清理屏障不放宽。generic Human Outbox gate沿P已定工作，不另改旧 events.go/外域表。V审卡确认此闭口，执行前仍核P最终文件是否正是这两个，不能猜新增修改范围。

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

Audit草案只冻结新五字段闭集和需要合并的既有约束名；不生成覆盖A/B未来DDL的最终ALTER列表。正式迁移在A00014与B00015状态确定后重放合并真实前序，fresh/populated升级/失败回滚和旧Audit闭集兼容都必须验收。当前SQL不带migration ordinal、不归位、不执行，不能称已验证schema。

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

首批 `service.go`、`repository.go`、`runtime.go`、`read.go` 与两项相邻测试已落盘；`GetDocument`／`ReadAncestors`／`ReadCurrentInTx` 通过同 Store 活 Tx、完整已有锁和真实 Project port 后才查询本域，取消不能代替实际 Drain。Unknown 保留原物理 attempt／cause，安全格式不泄露 command key。当前只是完整 B02 的中间片段，没有生产 stub、完整 Documents 实现、迁移或真实业务接受。

Go 1.27.1、`GOPROXY=off GOSUMDB=off`、独占 GOCACHE、只读既有固定 modcache 下，`go test -p 2 ./internal/central/knowledge/...` 实际通过。首编曾因 Object marker 名误写失败，改为正式 `StoredObject`；随后测试 fixture 使用非法非 UUID owner 导致 `INVALID_TRANSACTION_CAUSE`，修为正式 typed UUID 后通过。当前 pure 只覆盖构造、真实调用返回前不能 Drain、多个调用退出、Unknown 私有因果与已有 B01；SQL／权限／并发和对象组合还未真实执行，不冒充已验。

第二片段补上数据库 title/UUID 分页、签名游标绑定与字面搜索、canonical reader 的实际 Close 跟踪、有界 UTF-8 读取、原 command Lookup 与本域六表／Knowledge Audit 增量迁移。分页及读取纯控通过，`go test -race -p 2 ./internal/central/knowledge/...` 实际通过；首批 race 97475 亦实际通过。新增代码曾误用 `InvalidCursor` 名称及旧 ObjectOwner 构造形状，均在编译检查暴露并修正为正式 API。迁移 00025 仅落盘，未执行 SQL 或升级；连续前序 00024 与完整服务、对象／树写入及真 PG 仍未闭合。返回流只有底层 Close 成功才退出服务调用登记；Close 失败保持未退役事实，不能把 cancel 或 wrapper 关闭标记当资源 join。

第三片段已实现完整 subtree 预览、10 分钟原签名确认、Move 的发现映射／私有 issuer／同 caller Tx 锁绑定及 standalone/atomic 入口。Move 重验当前 Owner、原命令 receipt 和新 Mutate gate；变更只写 parent/updated_at、不推进 content_version、不发 content event，首次真实变化才 TouchActivity。代码尚无真实 SQL 结论。新增实际源纯控覆盖 foreign issuer／Tx、锁取得一次、exact caller 与树映射漂移；限定 pure/race 15508 实际通过。测试前置遗漏 RequestID 及使用非法零 ScopeNode 曾导致两次 FAIL，补正式 typed 前置后通过；未改产品门槛。此前 vet 41924 实际通过，尚未覆盖本第三片段。Delete mutation、内容发布／恢复、Object/Audit/Event adapter 与真实集成仍待完成。

第四片段实现本域 Object Authority 与精确 cleanup checker、KnowledgeFile resolver：普通 Read/Mutate 核 same Store 活 Tx／已有锁／当前 Owner 的 exact Actor/Project 投影，prospective 仅从原用户的真实 planned create＋publication 发出；当前 Object 读取只认 canonical 指针。清理独立核持久 operation/project/document/reason/object/upload，不把普通 Owner Read 升为 Converge，不绑定整 Project lifecycle。来源只处理 KnowledgeFile 的精确 revision，其他来源交外层真实 provider；读取 Object 元数据后重验本域版本，InTx 消费原 Object plan/token，无内部新锁或 I/O。本片段仍没有提供清理执行循环、发布／恢复或完整 Documents。

新增纯控核 foreign Tx／缺锁／当前身份拒绝前不读本域、错 Session 投影拒绝、发现映射不等于授权、Project cleanup 不借文档 cause、resolver 同 Store 和旧 revision 拒绝。限定 race 14355 与当前 vet 实际通过（也覆盖第三片段）；测试使用受控 Store/Project，仅证明接线与拒绝顺序，未证明真实权限／SQL／Object 组合。adapter 首编曾误写私有 command marker 名，修成已存在 typed marker 后编译通过；不改变原业务门槛。后续实现持久 event/Audit、发布／恢复及 Delete，并安排阶段独审与真实 SQL。

第五片段补本域 ProjectAuditAuthority 和 Outbox ProducerAuthority，尚待接入实际执行链。危险删除 Audit 消费同 Store/Tx 私有见证，再核持久 completed receipt、完整原 scope digest/ID 集与实际最小 tombstone；原命令 key 使用与正式 Audit helper 相同的 canonical digest，调用者自报 count/公开 Entry 不能授权。事件发现读取本域固定 command_events，依 private issuer 绑定 exact Actor/Session、summary/command/锁；CurrentAccess 仍核当前 Owner，NewFact 另核 Mutate 及当前 canonical/tombstone payload，Move 不发事件。限定 race 4255、vet 与 diffcheck 实际通过，新增控制覆盖见证/issuer/cause/Session/原 payload；这不是 SQL 或真实 Audit/Outbox 接入验收。C4 Project 路由、内容发布、Delete 与恢复尚未闭合。

第六片段已接入完整树计划与 Delete 的 standalone/同 caller Tx 入口：发现阶段保存原确认语义及固定每节点事件/cleanup IDs；最终重新核当前 Owner、原 receipt、签名期限和完整树/对象/upload 映射，再于同一事务撤 canonical 指针、写精确 cleanup、调用 D05 ReferenceCleanup、逐节点事件、单 root Audit、安全 receipt 与 Activity。完成重放先于旧 token key/过期/旧 scope 检查，不重复副作用；完成时清除临时 request/plan，不把原 parent 快照留作历史。`TreeMutationPlanning` 与 `AtomicTreeMutations` 编译断言已齐，物理清理仍以 `CleanupPending` 和持久任务留存，执行循环待后续接入。新增纯控覆盖部分树/parent/version/project/object/upload 漂移及持久计划 duplicate/额外字段拒绝；race 74599 实际通过，随后仅 SQL 补清除 plan（SQL 仍未验），当前 vet/diffcheck 通过。以上不等于真实删除、回滚、Audit/Outbox/Object 接入通过。
