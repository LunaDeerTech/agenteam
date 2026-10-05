# D12 Knowledge 文档与当前文档树实施规格

状态：S01 rev2 已由独立验证者通过并获主线程采纳；B01 纯契约已独立验收并提交推送 `914fd84`。[B02 正式卡](d12-b02-knowledge-service.md) 已采纳，当前仅其 C1 cursor.Text 完成作者局部自测、等待独立验收，真实业务依赖与迁移尚未验收，未分配迁移号。主卡见 [D12 工作项](d12-knowledge-documents.md)。固定输入为 `16595ad1e78e5283dfe85fb812095acde382edd1`；不消费活动 D07、D08 P、D05 stop 或 D09 源码。本规格不改变产品架构。

## 1. 范围与可立即完成的结果

依据 D01 `resources-skills.md` 的“Knowledge 树与内容端口”、`foundation.md` 的 Cursor/业务幂等/锁序、`domain-lifecycle.md` 的删除矩阵，以及 `knowledge-document-domain.md` §2/6/8/9，落实 **Human 当前 Owner** 的稳定 Document ID、canonical 内容、树查询/移动和确认范围删除。D12 不实现 parser、embedding、query-doc、模型选择、完整版本历史或 D13 索引器。

已完成的独立结果是 **`knowledge/contract` 的完整类型、严格编解码、纯树规则、命令摘要、删除范围与签名确认凭据、typed canonical 事件**。不是数据库/权限实现，也不提供成功空服务。领域服务代码及测试准备可随后独立推进；真实 PostgreSQL/MinIO/生命周期门槛逐项满足后才验收对应块，不因缺 D09/D13 整体等待。

固定基线已经具备：

| 能力 | 真实固定口与结论 |
| --- | --- |
| 当前 Human/Owner | `project.Authority.RequireOwnerInTx(ctx, tx, actor, project, intent)` 已实际核 User/Project 锁、当前 Session、真实 Owner、初始化与 gate。直接消费；不读取 account/project 外域表，不以 `CheckOwnerGate` 纯函数代替授权。AgentRun 在该实现返回 `DEPENDENCY_UNBOUND`。 |
| 树锁 | `foundation.KnowledgeTreeLock(project.String())` 已有，序位 3；不新增 document aggregate kind 或自造锁。 |
| 对象写入/源读取 | `oc.Objects`、`oc.Uploads`、`oc.SourceResolver`、`oc.SourceReads` 已有正式形状及 D05 实现；Knowledge 的真实归属、prospective command、current-object、source/download provider 仍由 D12 实现。 |
| 生命周期 | `pc.ProjectLifecycleParticipant`/`ProjectAuthority.ValidateLifecycleInTx` 和 D08 C0 已冻结；固定基线没有已验 Knowledge participant，也不能把未绑定 LifecycleFacts 称为实现。 |
| 事件 | D06 typed catalog/Outbox 已有；D01 已定 `knowledge.content_changed/deleted`。D12 提供本域 producer 事实与 Outbox 计划适配，不能只注册字符串即视为可发布。 |
| Cursor | 此基线 `cursor.Scalar` 只有 instant/uuid/integer，尚不能编码 `(title,id)`。需要正式 text scalar 窄补口；禁止冒用其他 scalar 或另造分页签名。它不阻断纯树/命令/确认规则。 |
| 对象清理/下载 | `CleanupReleaseAccess` 明确仅 Avatar，`ReferenceCleanup` 的 SQL 也仅撤 Avatar 引用；不能当作 Knowledge 通用口。普通 `ReleaseObjectInTx` 可作为正式候选，必须真实证明撤引用、publication gate 与 pending upload 收敛。下载还需要 Knowledge typed Audit/provider，当前均未绑定。 |

## 2. 关键 Go 类型与公共端口

以下位于 `internal/central/knowledge/contract`；`f`、`id`、`oc`、`pc`、`event`、`ob` 分别是现有 foundation、identity/contract、object/contract、project/contract、event/contract、outbox/contract。B01 已实现类型与纯规则；下列服务接口仅声明正式边界，不代表运行时绑定。所有 ID 使用 foundation UUIDv7，版本/计数沿用其精确标量。

```go
type Document struct{}
type DocumentID = f.ID[Document]
type SourceKind string      // text | file
type DocumentStatus string  // active | deleted
type IndexingStatus string  // pending | processing | ready | failed
type CommandName string     // create | update | move | delete-subtree
type CreatorRef struct { /* private Human{user_id} | AgentRun{project_id,agent_id,execution_id} */ }
type CreatorDetails struct {
    Kind id.ActorKind
    UserID id.UserID
    ProjectID id.ProjectID
    AgentID id.AgentID
    ExecutionID id.ExecutionID
}
func NewCreatorRef(CreatorDetails) (CreatorRef, error)
func (c CreatorRef) Validate() error
func (c CreatorRef) Details() CreatorDetails

// 无 folder、rank、tree_version、历史 parent。
type DocumentRef struct {
    ID DocumentID
    ProjectID id.ProjectID
    ParentDocumentID *DocumentID
    Title string
    ContentVersion f.Version
    SourceKind SourceKind
    MediaType string
    ObjectID oc.ObjectID
    Status DocumentStatus
    IndexingStatus IndexingStatus
    CreatedBy CreatorRef // 本阶段生产创建只准 Human；能表达 Agent 不代表授予权限
    CreatedAt, UpdatedAt f.Instant
}
type DocumentTombstone struct {
    ID DocumentID
    ProjectID id.ProjectID
    ContentVersion f.Version
    DeletedAt f.Instant
}

// 私有闭集，不提供 RawObjectID/bucket/key/URL 分支。
type SourceInput struct { /* private, text | upload | business_file */ }
type InputKind string // text | upload | business_file；与文档 SourceKind 不混用
type UploadInputDetails struct { Length f.Progress; ExpectedSHA256 f.Digest }
type SourceInputDetails struct {
    Kind InputKind
    MediaType string // text/upload 必填；business_file 从真实 resolver 获取
    Text *string
    Upload *UploadInputDetails
    BusinessFile *oc.BusinessFileRef
}
func NewTextSource(mediaType, text string) (SourceInput, error)
func NewUploadSource(mediaType string, length f.Progress,
    expectedSHA256 f.Digest, body io.ReadCloser) (SourceInput, error)
func NewBusinessSource(ref oc.BusinessFileRef) (SourceInput, error)
func (s SourceInput) Validate() error
func (s SourceInput) Details() (SourceInputDetails, error) // 无 reader/closer 字段
func (s SourceInput) TakeUploadBody() (io.ReadCloser, error) // 单次转出受控包装流
func (s SourceInput) Close() error

type CreateRequest struct {
    ProjectID id.ProjectID
    DocumentID DocumentID // 本次意图固定；重试不能换 target
    ParentDocumentID *DocumentID
    Title string
}
type UpdateRequest struct {
    Title *string // nil 是未提供；wire null 拒绝
    ReplaceSource bool // source 参数非 nil 当且仅当 true；显式替换可变 text/file
}
type MoveRequest struct {
    ExpectedParentID *DocumentID // wire 字段必须出现，null 明确表示根
    TargetParentID *DocumentID   // wire 字段必须出现，null 明确表示根
}
type MoveResult struct {
    Document DocumentRef
    Changed bool
}
type ScopeNode struct {
    ID DocumentID
    ProjectID id.ProjectID
    ParentID *DocumentID
    ContentVersion f.Version
    Status DocumentStatus
}
type ConfirmationToken struct { /* private, no generic logging */ }
type DeletePreview struct {
    Root DocumentID
    Nodes []DocumentRef // 完整 active 子树，不能静默截断
    ScopeDigest f.Digest
    Confirmation ConfirmationToken
    ExpiresAt f.Instant
}
type DeleteResult struct {
    Root DocumentID
    DeletedIDs []DocumentID // 唯一、按 ID 排序
    CleanupPending bool // 首次提交只证明 tombstone；物理完成另由清理事实证明
}
type ListFilter struct {
    TitleQuery string
    SourceKind *SourceKind
    MediaType *string
    IndexingStatus *IndexingStatus
}
type TitleHit struct {
    Document DocumentRef
    Ancestors []DocumentRef // root 到 parent，不包含自身
}
type DocumentHead struct { // 当前读取闭集，删除态不携 title/object/path
    Active *DocumentRef
    Deleted *DocumentTombstone
}
type ReadRequest struct { ByteOffset f.Progress; MaxBytes int }
type TextContent struct { Text string; NextByteOffset f.Progress; Truncated bool }
type ReadableUnavailable string // processing | failed | dependency_unbound
type DocumentContent struct { // 恰一分支；binary 不进入 Text
    Document DocumentRef
    Text *TextContent
    File *oc.BusinessFileRef
    Unavailable *ReadableUnavailable
}
type LookupRequest struct {
    ProjectID id.ProjectID
    Command CommandName
    Key f.IdempotencyKey
    SemanticDigest f.Digest
}
type MutationReceipt struct { // 原 metadata 结果，不保存正文或旧对象读取许可
    Command CommandName
    Document *DocumentRef     // create/update/move；delete 不保存此分支
    RootID *DocumentID        // delete-subtree 才有
    Changed bool
    DeletedIDs []DocumentID
    CleanupPending bool
}
type CommandLookup struct { // 严格 result union
    State string // committed | in_progress | not_observed
    Receipt *MutationReceipt // 仅 committed 必填
}

type Documents interface {
    CreateDocument(context.Context, id.Actor, f.CommandMeta, CreateRequest, SourceInput) (DocumentRef, error)
    UpdateDocument(context.Context, id.Actor, f.CommandMeta, id.ProjectID, DocumentID, UpdateRequest, *SourceInput) (DocumentRef, error)
    GetDocument(context.Context, id.Actor, id.ProjectID, DocumentID) (DocumentHead, error)
    ReadDocument(context.Context, id.Actor, id.ProjectID, DocumentID, ReadRequest) (DocumentContent, error)
    ListDocuments(context.Context, id.Actor, id.ProjectID, ListFilter, f.PageRequest) (f.Page[DocumentRef], error)
    ListChildren(context.Context, id.Actor, id.ProjectID, *DocumentID, ListFilter, f.PageRequest) (f.Page[DocumentRef], error)
    ReadAncestors(context.Context, id.Actor, id.ProjectID, DocumentID) ([]DocumentRef, error)
    SearchTitles(context.Context, id.Actor, id.ProjectID, string, f.PageRequest) (f.Page[TitleHit], error)
    MoveDocument(context.Context, id.Actor, f.CommandMeta, id.ProjectID, DocumentID, MoveRequest) (MoveResult, error)
    PrepareDeleteSubtree(context.Context, id.Actor, id.ProjectID, DocumentID) (DeletePreview, error)
    DeleteSubtree(context.Context, id.Actor, f.CommandMeta, id.ProjectID, DocumentID, ConfirmationToken) (DeleteResult, error)
    LookupCommand(context.Context, id.Actor, LookupRequest) (CommandLookup, error)
}

// InTx 真正消费外层已取得的完整计划；此句柄私有绑定本服务/请求/Tx。
type TreeMutation struct { /* private Move{actor,meta,project,document,request} | Delete{actor,meta,project,root,token} */ }
type TreeMutationDetails struct {
    Command CommandName // 仅 move/delete-subtree
    Actor id.Actor
    Meta f.CommandMeta
    ProjectID id.ProjectID
    DocumentID DocumentID
    Move *MoveRequest
    Delete *ConfirmationToken
}
func (r TreeMutation) Validate() error
func (r TreeMutation) Details() TreeMutationDetails
func (r TreeMutation) Binding() f.Digest
func (r TreeMutation) Equal(TreeMutation) bool
type MutationIssuer struct { /* private per-service identity; never exported by a plan */ }
func NewMutationIssuer() MutationIssuer
func (i MutationIssuer) Validate() error
type MutationPlan struct { /* private issuer + exact TreeMutation + mappings + D05/D06 plans + lock union */ }
type MutationPlanDetails struct {
    Request TreeMutation
    DomainMapping f.Digest
    ObjectPlans []oc.AccessLockPlan
    EventPlans []ob.AppendPlan
    Locks []f.LockRequest // 完整规范化 union，覆盖上面各 provider 的全部锁
}
func NewMutationPlan(MutationIssuer, MutationPlanDetails) (MutationPlan, error)
func (p MutationPlan) Validate() error
func (p MutationPlan) Details() MutationPlanDetails
func (p MutationPlan) IssuedBy(MutationIssuer) bool
func (p MutationPlan) Matches(MutationIssuer, TreeMutation) bool
func (p MutationPlan) Locks() []f.LockRequest
type LockedMutation struct { /* private */ }
type LockedMutationDetails struct {
    Tx f.Tx
    Plan MutationPlan
    ExtraLocks []f.LockRequest
    ObjectAccess oc.LockedAccess // ObjectPlans 非空时由实际 D05 Acquire 返回
}
func NewLockedMutation(MutationIssuer, LockedMutationDetails) (LockedMutation, error)
func (l LockedMutation) Validate() error
func (l LockedMutation) Details() LockedMutationDetails
func (l LockedMutation) Matches(MutationIssuer, f.Tx, MutationPlan, TreeMutation) bool
func (l LockedMutation) Locks() []f.LockRequest
func NewMoveMutation(id.Actor, f.CommandMeta, id.ProjectID, DocumentID, MoveRequest) (TreeMutation, error)
func NewDeleteMutation(id.Actor, f.CommandMeta, id.ProjectID, DocumentID, ConfirmationToken) (TreeMutation, error)
type TreeMutationPlanning interface {
    DiscoverMutation(context.Context, TreeMutation) (MutationPlan, error)
    AcquireMutationInTx(context.Context, f.Tx, MutationPlan, []f.LockRequest) (LockedMutation, error)
}
type AtomicTreeMutations interface {
    MoveDocumentInTx(context.Context, f.Tx, id.Actor, f.CommandMeta,
        id.ProjectID, DocumentID, MoveRequest, LockedMutation) (MoveResult, error)
    DeleteSubtreeInTx(context.Context, f.Tx, id.Actor, f.CommandMeta,
        id.ProjectID, DocumentID, ConfirmationToken, LockedMutation) (DeleteResult, error)
}
```

`DocumentRef` 是服务间 metadata，不是给模型或 UI 暴露 Object ID 的通用响应。metadata 的 HTTP/Tool 安全投影不输出 object identity、正文、签名 token 或内部错误；正文只由已授权的 ReadDocument 内容响应提供。GetDocument 查询 current-or-deleted；ReadDocument 消费下节的受保护 canonical 流并给有界当前内容/业务 file ref/不可用状态，不把 `io.Reader` 装进 JSON。ReadRequest 默认 offset=0、64 KiB，最大1 MiB；UTF-8 offset必须落字符边界，输出不得截断字符，NextByteOffset与实际字节一致。PDF/DOCX的parser locator由D13后续提供，当前不得假造解析结果。

所有 enum 使用命名类型并实现严格 Validate/JSON；上文 `CommandLookup.State` 在实际纯契约中也必须独立命名闭集。拒绝未知/重复 JSON 字段、无效 UTF-8、枚举未知值、零/错误类型 ID、数值精度丢失、missing 与 null 混淆；解码失败不污染既有值。opaque 授权/流/签名材料不接受通用 JSON 反序列化。

CreatorDetails 是可持久化的来源事实投影，不含 Session/权限：Human 仅 UserID 非零；AgentRun 仅 ProjectID/AgentID/ExecutionID 非零且 ProjectID 必须与 Document 相同；拒 Service 与混合分支。构造器不证明当前身份；运行时仍取当前已验 Actor。DocumentRef 的显式 DTO 编解码经此 typed 投影/构造器，不把 CreatorRef 当授权令牌或伪造 Session。零 CreatorRef.Validate 失败，零 Details 返回全零诊断值，不能据此接受来源。

SourceInputDetails 恰好一个有效分支；构造和 Details 深复制自身 pointer/元数据，BusinessFileRef 保留 D05 的 opaque 业务引用，不能用安全 JSON 标签替代其真实 Details 计算摘要。Details 不返回原 body，且在流关闭后仍可读取不变元数据做原命令核对。NewUploadSource 拒 nil/typed-nil body；构造失败由调用者关闭原 body，成功后关闭责任转给 SourceInput。所有 SourceInput 值副本共享同一流状态：TakeUploadBody 仅 upload 分支且最多成功一次，并发争取至多一个成功，返回包装流而非原句柄；包装流 Close 与 SourceInput.Close 汇入同一次真实底层 Close 并重复返回原关闭结果。未取流也可 Close，取流后调用者须 defer Close，服务提前返回亦须关闭尚持有的 SourceInput；非 upload 的有效 Close 无 I/O。零值的 Validate/Details/Take/Close 均报错，非 upload Take、二次 Take 和已关闭流 Read 拒绝；关闭错误不能投影成资源已清零或实际 join。文本/body 只经显式消费口获取，fmt/JSON/log 仅固定安全标签，通用反序列化拒绝。

本候选工程取值：标题保留原 UTF-8，不做大小写合并或名称唯一约束；非空、至多 512 Unicode scalar，拒绝控制字符；可同名。允许四个规范 MIME，text 仅 Markdown/plain，file 仅 PDF/DOCX；显式 replacement 可跨 kind，不能只改元数据。上传长度不超过 D05 的 1 GiB，上传声明 SHA 必须与实际测量一致；plain/Markdown 验证 UTF-8，不 trim/换行归一正文。HTTP 可先 spool/hash 再构造上传输入，此处没有放宽 D05 完整性检查。

## 3. 纯规则与确认凭据

```go
type MoveFacts struct {
    Current ScopeNode
    TargetAncestors []ScopeNode // target-parent 到根的完整链；根目标时为空
}
type MoveDecision struct { From, To *DocumentID; Changed bool }
func CheckMove(project id.ProjectID, target DocumentID,
    request MoveRequest, facts MoveFacts) (MoveDecision, error)
func SubtreeDigest(project id.ProjectID, root DocumentID,
    nodes []ScopeNode) (f.Digest, error)
func CommandIdentity(project id.ProjectID, name CommandName,
    key f.IdempotencyKey) (f.CommandIdentity, error)
func CreateDigest(id.Actor, f.CommandMeta, CreateRequest, SourceInput) (f.Digest, error)
func UpdateDigest(id.Actor, f.CommandMeta, id.ProjectID, DocumentID, UpdateRequest, *SourceInput) (f.Digest, error)
func MoveDigest(id.Actor, f.CommandMeta, id.ProjectID, DocumentID, MoveRequest) (f.Digest, error)
func DeleteDigest(id.Actor, f.CommandMeta, id.ProjectID, DocumentID, ConfirmationToken) (f.Digest, error)

type DeleteConfirmationClaims struct {
    UserID id.UserID
    ProjectID id.ProjectID
    RootID DocumentID
    ScopeDigest f.Digest
    ExpiresAt f.Instant
}
type ConfirmationKeys struct { /* private HMAC keyring */ }
func ParseConfirmationToken(raw string) (ConfirmationToken, error) // 只核传输形状，不授确认权
func (t ConfirmationToken) ForHumanResponse() string // 仅在服务已授权的预览响应显式调用
func LoadConfirmationKeys(raw string) (ConfirmationKeys, error)
func (k ConfirmationKeys) Sign(DeleteConfirmationClaims) (ConfirmationToken, error)
func (k ConfirmationKeys) Verify(ConfirmationToken, f.Instant) (DeleteConfirmationClaims, error)
```

`CheckMove` 验证 Current 属于请求 Project/target 且 active，当前 parent 等于显式 expected_parent；target 链全部同 Project/active、链连续且最终到根、无重复 ID、不出现自身，错误范围不返回外项目节点。只据这些事实计算结果，**不证明 Session、数据库快照完整性或已持锁**。同 parent 且 expected 命中是成功 no-op；不会推进 content_version。后代不逐个重写 parent。

`SubtreeDigest` 对唯一 ID 集、正 content_version、同 Project、active、单根/连通/无环作纯验证；根可以有子树外父节点，其余每个 parent 必须在集合内。它不能证明 SQL 没漏 descendant，完整枚举由运行时在树锁下负责。摘要域 `agenteam.knowledge.subtree.v1\x00`，canonical-v1 编码 project/root 与按 ID 排序的 `(id,parent_id,content_version,status)`；无树版本，不加入索引状态、路径字符串或时间。标题影响 content_version，所以标题更新也失效确认。

确认 HMAC-SHA256 使用独立部署密钥、signed header `{signature_version:1,kid}`、上述 canonical claims；格式 `base64url(H).base64url(P).base64url(M)`，无 padding、固定算法，MAC 输入域 `agenteam.knowledge.delete-confirmation.v1\x00` 加原始 `H.P`，常量时间比较，最大 8 KiB。只用 current kid 签发，旧 kid 只验证，未知 kid/版本/重复字段/额外字段拒绝；密钥/签名不进入日志或 Audit。默认确认有效期 10 分钟，明确只用于未接受删除前校验，不给命令/业务增加 TTL。完整删除清单另返回，不塞 token。

首次 Delete：当前 Session/Owner → 同 key 已完成查询/摘要 → 如未完成，核签名/有效期/User/Project/root → 锁后重算完整子树 → 摘要相同才 tombstone。完整范围的加入、移出、删除、parent 改变、内容/标题更新均 `CONFIRMATION_STALE`；不部分提交，也不自动换成新范围。锁等待超时/查询失败不等价空树。

Delete 命令语义含**原确认 token 字节的 SHA-256**；新签确认材料用新意图 key，不自动改写已持久命令。已提交的同 key/同 token 重放先返回安全删除 receipt，不重验旧 token 期限/旧 content_version，不重删；原 token 对应已核验成功事实足够重放，旧验证 key 被移除也不应阻断该安全 receipt。若需要执行新删除，任何签名/过期例外均不成立。原 key Unknown 必须先查清，不能刷新 token/换 key 重做。

## 4. 事务、锁与幂等

默认 Read Committed 加正式 advisory locks 和数据库约束；所有 metadata 正常命令在完整 `Command EX → User → Project SH → KnowledgeTree → Object/其他 aggregate → record` union 下执行。Human 写入需同 Tx 的 Session 活动记录时提前要求 User EX，普通只读 User SH。所有 create/update/move/delete 使用 tree EX；目录查询/祖先/确认预览用 tree SH。没有单独 document aggregate；D05 所需 Object/来源 owner locks 及 D06 Outbox 屏障必须预先加入。

source/对象/事件计划在 Tx 外发现；一次 `AcquireAll` 或 D05 `AcquireAccessPlansInTx(..., completeExtras)` 取得规范化完整 union，再同 Tx 校验全部 mapping/current auth。`...InTx` 不嵌套事务、不逆序补锁、不发 MinIO/网络。新依赖/映射漂移使整个未提交事务退出重取计划；不能以“知识树已锁”跳过原来源 Project/对象的真实锁。只读 discovery 不授数据读取权。公开 `AccessDependencies` 只是 mapping+locks，不能冒称不可伪造 issuer。

`LockedMutation` 对外层组合不是空能力：上述 `TreeMutationPlanning` 计划完整绑定命令/输入/本域发现与外部 D05/D06 plans，令牌绑定本服务私有 issuer 和 actual tx；额外锁只能增加完整 union，不能替换或删掉计划锁。B02 构造依赖必须真实注入 ProjectAuthority、Store、Objects/Uploads、Outbox 及所属事实适配器，所有 InTx 方法重验 exact request/token/tx。B01 只冻结类型、构造校验和接口，不实现或宣称这些服务授权方法。公开 InTx 方法缺该实现即未绑定，不允许调用方自造 bool 放行。

TreeMutation 的构造/Details 深复制 Meta.ExpectedVersion、Move 的两个 nullable parent 与全部自身指针；仅允许与 command 相符的一个分支。Binding 用域 `agenteam.knowledge.tree-request.v1\x00` 加 primitive canonical-v1 投影，覆盖完整 ActorDetails（含当前 Session）、完整 CommandMeta（含 RequestID）、Project/Document、两个显式 nullable parent 或原 token SHA；不能散列 opaque 的安全格式化标签。Equal 核完整绑定；该调用绑定与排除 Session/RequestID 的业务幂等摘要不同。

MutationIssuer 由每个实际 Knowledge 服务独立创建并私有持有；公开 New 只能创建另一个 issuer，Plan/Locked 的任何 Details 均不泄出 issuer。NewMutationPlan 为本次计划生成私有 plan identity，校验 request/DomainMapping、各 typed D05/D06 plan 形状与完整锁覆盖，包括本阶段 Human tree mutation 必要的 command EX、User EX、Project SH、KnowledgeTree EX；锁规范化使用既有全局顺序，重复 key 取较强模式。构造/Details/Locks 复制自身 slices/pointers及锁快照，typed provider handle 按其正式契约消费，不用 any/字符串替代；runtime 对 provider 的当前完整投影重验，不能把发现时 mapping 当永久事实。

NewLockedMutation 只校验非零 Tx、同 issuer 的精确 plan identity 和规范化额外锁，捕获 plan+extra 的完整 union及实际 D05 ObjectAccess；Matches 比较服务 issuer、同一 actual Tx、原 plan identity、完整 TreeMutation 与锁快照，而非只比 target/key。它与 NewMutationPlan 都是 **纯载体构造器，不证明 held locks 或权限**。真实 AcquireMutationInTx 在 Store 确认活 Tx 后，若有 ObjectPlans，唯一调用 D05 AcquireAccessPlansInTx，并把本域/D06/extra 全部锁传作 extras，保留返回的 ObjectAccess；否则唯一调用 Store.AcquireAll。实际取得完整 union 后才签发本服务 LockedMutation；各 InTx 再验证 Store 中同一活 Tx、当前权限/mapping，并把原 plan+ObjectAccess 交 D05 Validate/操作口、把原 AppendPlan 交 D06。纯构造器不得伪造成功 Acquire；调用方自给锁列表、用自己的 issuer、foreign/stale Tx 或另一个相同参数 plan 都不能通过服务消费。零值 Validate/Matches 失败；零 Details/Locks 只返回零值/空投影；opaque plan/token/issuer 的反序列化拒绝、隐式输出固定安全标签。

CommandIdentity 必须调用 `foundation.NewCommandIdentity("knowledge", []string{project.String()}, string(command), key)`：Namespace 是字面 `knowledge`，OwnerIDs 是有序的单元素 `[project_id]`，command 为四闭集。这是构造器实参，不是把展示路径 `knowledge/[project_id]` 传进 namespace；不修改 foundation 的 stableName 规则。摘要 domain `agenteam.knowledge.command.canonical-v1`，包含当前 Human 稳定 user_id（不含 session）、Project/target、expected_version 和全部语义参数；正文用按原字节测量的 length+SHA，BusinessFileRef 保留精确 kind/业务 ID/revision，Move 包含两个显式 nullable parent，Delete 包含确认 token digest。Source 流句柄、trace、连接、超时不参与。每个 command 的 typed digest 函数在 B01 实现，不提供任意 map 入参。

当前身份与结果可见性先于 receipt；命中同 key异义为 `IDEMPOTENCY_KEY_REUSED`；已完成同义不重验旧版本/来源/父位置、不重 Put/Touch/Event。无成功结果才检查当前 Mutate gate、不变量与版本。Create/Move/Delete 的 meta.ExpectedVersion 必须 nil；Update 必须正版本且匹配 current。Update 只更新明确 title/source 字段，绝不写回旧 parent；Move 只更新 parent，不带正文/标题。有效 title/source 改变推进 content_version、pending 与 updated_at；真正无变化可记安全 no-op receipt，不推进版本/重复 event。Move 改变 updated_at 但不改内容版本。

Create 只用一次意图固定 UUIDv7 target；同 Project 同 target 被其他 key 使用即 `RESOURCE_BUSY`，foreign target不透露信息，同 ID tombstone 不可复活。DB 全局 Document PK 及本域 command identity 唯一约束保留；锁下先查 expected collision，不能依赖 23505 在已 poisoned Tx 内转换安全业务错误。create/update 预先保存原 target/source/upload/发布 cause 的恢复事实，但未发布的 prospective 文档不进入 active 目录。

最小持久化形状拟为本域 `documents`、`commands/publications`、`object_cleanup` 三类事实，不在本次指定 SQL 文件或迁移号。Document 全局 ID 主键，另有 `(project_id,id)` 唯一键及同项目 parent 复合外键、`id != parent_id`/正版本/闭集状态约束；active parent 与无环由树锁下 SQL 重验，不能假称 CHECK 能检查跨行树。索引至少覆盖 active `(project_id,parent_document_id,title COLLATE C,id)`。普通 title 非唯一；非 active 不进入目录索引。tombstone 将 payload/current-object读取映射撤销，清理所需精确对象ID转入受控cleanup事实；Meeting可查询的已删投影只保稳定ID/version/时间。publication及清理是技术恢复事实，不新增用户可见DocumentVersion或结构历史。索引pending/current-version和Outbox事实足以让D13以后发现工作，不提前创建D13 job表。

Unknown 分阶段查**原 cause + key + target**：command/Project/tree 锁确证原 metadata 事务已结束后，canonical receipt 可收敛 committed；只对相应阶段的确证缺行收敛 not_committed。已有 publication plan、活动旧 Process/lease、外部对象结果未知、查询故障/锁预算耗尽均不能当 rollback；未确认 reserve 不 PUT，未确认 publish 不宣布 Document active，不能换 object/document ID。Lookup 的 `not_observed` 本身不证明全部旧工作终止。

## 5. 内容、对象引用与 D13 边界

```go
// Domain 门面先绑定当前 Document；业务流不可 JSON/日志展开。
type CanonicalRead struct { /* private: fixed DocumentRef + *oc.ObjectReader */ }
func NewCanonicalRead(DocumentRef, *oc.ObjectReader) (CanonicalRead, error)
func (r CanonicalRead) Validate() error
func (r CanonicalRead) Document() DocumentRef
func (r CanonicalRead) Range() *oc.ResolvedRange
func (r CanonicalRead) Read([]byte) (int, error)
func (r CanonicalRead) Close() error
type CanonicalReads interface {
    OpenCanonical(context.Context, id.Actor, id.ProjectID, DocumentID,
        *oc.ByteRange) (CanonicalRead, error)
}
type CurrentDocumentFact struct {
    ProjectID id.ProjectID
    DocumentID DocumentID
    ContentVersion f.Version
    Status DocumentStatus
    ObjectID *oc.ObjectID // active 才有；删除投影无对象访问能力
}
type CanonicalFacts interface {
    ReadCurrentInTx(context.Context, f.Tx, id.Actor, id.ProjectID,
        DocumentID) (CurrentDocumentFact, error)
}
type ContentChange string // created | title | source；created不能与另两项并存
type ContentChangedPayload struct {
    DocumentID DocumentID
    ContentVersion f.Version
    Changes []ContentChange // 非空唯一，固定顺序
    ObjectID oc.ObjectID
}
type DeletedPayload struct { DocumentID DocumentID; ContentVersion f.Version }
type KnowledgeEvents struct { /* private typed event handles */ }
func RegisterKnowledgeEvents(*event.Catalog) (KnowledgeEvents, error)
func (e KnowledgeEvents) ContentChanged(event.Header, ContentChangedPayload) (event.Event, error)
func (e KnowledgeEvents) Deleted(event.Header, DeletedPayload) (event.Event, error)
```

NewCanonicalRead 校验非零 active DocumentRef、非 nil/有效 D05 reader、available ObjectMeta，且 object ID、Project scope、media_type 与固定 Document 完全匹配，range 服从该对象真实长度；只建立受控载体，不证明调用者权限。构造失败由调用者 Close 原 reader；成功后将 reader 关闭责任转给 CanonicalRead，Document/Range 返回深复制投影，不暴露底层 reader。其值副本共享关闭状态，Read 只委托该固定 reader，Close 汇入一次真实 reader.Close 并重复返回原结果，关闭后 Read 拒绝；调用者必须 Close，包括提前返回/读取失败/EOF。实际取消、join、lease 释放仍由 D05真实reader完成；包装器关闭标记或返回error不能伪称已join/已释放。零值 Validate/Read/Close 失败，Document/Range 仅零投影；fmt/JSON/log 固定安全标签，通用反序列化拒绝，不能靠客户端构造载体获得权限。

Human 正常调用每次经 D08 current Owner；Source resolver 映射 `KnowledgeFile{document_id,content_version}` 必须精确匹配当前 active 版本与 Object ID，不能自动 latest 或读旧版。D12 组合提供 `oc.AccessPlanner`、`ResourceAuthority`、`ObjectReadAuthority`、`SourceResolver`、`DownloadProvider` 对 Knowledge owner 的分派；其他 kind 转真实所属 provider，未知/缺失明确拒绝。`ProjectGate` 的普通读/写委托同 Tx ProjectAuthority；Converge/Lifecycle 另核本域实际持久 cause，不能把 Owner Read grant 提升成任意 Converge。

新对象归属为 `ObjectOwner{knowledge,document_id,project_id}`。prospective grant 只来源于当前已授权的固定 publication command，不是客户端声称“document 不存在”。文本、上传文件、受控业务 source 均走 D05 measured preparation → 短 Tx reserve → 确证后 I/O → 校验后短 Tx publish/bind；外部 Source 用真实 resolver/SourceReads 在命令捕获同 Tx 保住精确版本，开流/发布再验，copy 到本 Document 独立 canonical 对象，不复用外域 owner grant。完成重放先查目标 receipt，不重读已删除来源。

发布 Tx 原子写：新的 current metadata/version、D05 新 canonical reference、旧 canonical reference 的合法释放或持久释放任务、canonical mutation receipt、本域 indexing invalidation 输入及 D06 typed Outbox。新版本指针一旦提交，旧 payload 从业务读取即失效；清理未结束不会恢复旧版本权限。旧 payload 无历史 API；既有 reader/lease 的实际关闭与引用保护仍真实记录，不能宣称已召回发出的字节或强行物理删除。

替换/删除持久化 `(原 command/cause, DocumentID, ObjectID, UploadID, reason, phase)` 清理事实；payload/索引删除在事务外推进，失败/Unknown留 checkpoint，不能成为 canonical rollback。普通 `ReleaseObjectInTx` 只删 canonical ref，不关闭所有 upload/publication 状态；是否能在确证 owner cause 下完整满足 D12，须真实 D05 合作验收。若确需扩 `ReferenceCleanup`，**单独提交精确 D05 窄补口审查并取得文件权**；不能删除其 Avatar 检查、自行查/改 object 表或令清理默认成功。单个 Document tombstone 保稳定 identity/已删状态；Project 永久删除才清这些本域 metadata/command/cleanup事实。

B01 typed schema 固定 producer=`knowledge`、aggregate_type=`knowledge_document`、schema_version=1；下列同事务可靠发布仍属后续服务。

D12 在同 canonical Tx 保存当前版本/status、pending invalidation 输入并可靠写 `knowledge.content_changed`（create/title/source change）或每个 tombstone 的 `knowledge.deleted`；typed payload 只含 ID/version/change/current object ref（删除不含对象），无正文/标题全量副本。Move 不发送 content_changed、不索引；若后续需要目录 realtime，另冻结最小父关系事件，不伪造内容变更。

**D13 未启用**：无索引可 serving，canonical 写入仍成功且 pending；D12 没有空 `EnqueueIndexInTx` 成功实现。后续 D13 注册 handler 后按 D06 canonical rebuild+dirty 协议补齐旧文档，不能假设注册前 events 有 delivery。**D13 已启用**：从候选召回前就以同 Project 下 current ID/version/status 排除旧版本/已删文档；D12 对这些 eligibility 原事实的更新与失效输入同 Tx。D13 若需要本域派生标记同步，按 D01 `InvalidateServingInTx` 接 caller Tx 和完整计划，失败回滚整个 mutation，不能延迟到异步队列才撤旧 serving。pending/processing/ready/failed 的真实构建状态和合法回调由 D13 自己证实，D12不凭“已入队”写 ready。

PDF/DOCX 无 parser 时 metadata/download 可用，结构化 read/locator 返回 processing/unavailable，不回退旧版、不把二进制塞进模型。文本全文/分段按原 current 字节、明确截断/locator结果；D13 承担解析 locator，不能因此阻塞 D12 原文件下载。受控 preview/download 必须走 D05 provider 的每次当前 Session/Owner/版本校验与真实 Audit（issued/started/sent/failed）；URL只面向 Human，沿既有 no-store/短期/token/header规则。D05 基线对 PDF/Office 默认 attachment，不因 D12 `preview` 参数改变 active-content 安全策略；D27 安全 viewer/派生预览后续接入。

## 6. 查询、删除与生命周期边界

每次查询先当前授权。根以显式 null parent；非根 parent 必须是同 Project active 文档，不把 deleted/foreign parent 当“空列表”。`ListChildren` 只该层，`ListDocuments` 全 Project平面筛选，`SearchTitles` 在服务器所有 active 文档搜索并返回当前完整祖先路径；不依赖浏览器已加载节点、不检索正文。

排序固定 `title COLLATE C ASC,id ASC`，同 title 可存在；title_query 为原 UTF-8 的字面子串匹配，`%/_` 不作 wildcard，不引入分词/语义搜索。默认 limit=foundation 50、上限200。cursor 绑定 stable user/project、查询类型、parent/filter/order和 `(title,id)` position；每页重验 current Session/Owner。limit 不绑摘要；不增加 tree generation，不承诺跨页快照。rename/move 后可能跳过/重复，客户端按 DocumentID 合并并刷新相关层/祖先；不能用陈旧 metadata PUT 整行回写。cursor.Text 补口完成前该分页运行时不能宣称完成。

单次预览/祖先/查询在短 Tx tree SH 内获得一致关系；Delete/Move 在 tree EX 内重读，跨 Tx “先查防环再写”不成立。确认范围完整查询失败/资源预算不够即失败并提示重新获取，不能只删已装入内存的部分；本候选不设静默截断或持久结构历史。全部 tombstone、serving invalidation 输入、删除 receipt/event 在一个事务一致提交，物理清理异步。`CleanupPending=false` 只可来自真实已完成清理事实，不因队列接收成功。

历史引用查询允许返回最小 `DocumentTombstone`，普通 read body / preview / new source 使用返回 `RESOURCE_DELETED`；外项目仍不透漏存在性。旧更新/创建 receipt 只返回原安全 metadata 结果，不返回历史正文或重新可读 URL；其内部 ObjectID/旧版本不构成读取许可，HTTP/Tool 不投影该 ObjectID。仅按原 command key 可重放 metadata，不提供历史版本或树历史浏览；同 key replay 不是读取旧 content version。Meeting 继续保存 document identity，由 D24 投影 deleted，不由 D12 重写外域历史。

启用真实 Knowledge 之前，注册自己的 `ProjectLifecycleParticipant`（建议名 `knowledge`，由 D08 装配显式纳入持久 required manifest）：archive 阻止新写，停止本域 publication/cleanup写工作并保已存内容，archived 可读；delete 停止本域 publication/source/reader，再清本域所有 active+tombstone/commands/invalidation/cleanup，不只默认 active 列表。D05 stop/inspect 的实际 work/source_project_id 事实与 D12 work 必须合并，不以 cancel/TTL代 join，不漏历史 upload 或正在源复制的工作。

Cleanup 的依赖顺序由 D08 manifest 明列：Knowledge 自有 refs与publication事实先于 Artifact/Object 的最终物理清理，若 D13已启用其读取/派生资源也必须真实停止清理，Outbox/Audit仍按后置屏障执行。保留必要临时 cleanup cause直到D05确认对象处理完成，不能先抹掉其授权证据形成循环。Archive不物理删除正文，Restore不重启旧 attempt。D08 P/D05 stop真实接入未验和D13未绑定分别记账，不假称项目全域停止/清理已验。

## 7. 先行块、所有权与验收

| 结果块 | 可写范围与实际前置 | 可认定的完成/阻塞边界 |
| --- | --- | --- |
| B01 纯契约/规则，已验收并推送914fd84 | 仅新 `internal/central/knowledge/contract/{types,source,commands,tree,confirmation,events}.go` 及六个对应 `_test.go`，共12文件；固定165上的 foundation、identity/contract、object/contract、project/contract、event/contract、outbox/contract 和 cursor.CanonicalJSON 只读。opaque 补口仍放 types/source/tree 六源既有分工，不增加第13文件。目录若已有文件先停止冲突，不能覆盖。 | 完整 strict DTO/enum、树校验、语义摘要、token签发验签/过期、typed events及负例；无 service/schema/Audit/identity/cursor 旧文件写。纯 unit/race/vet 及独立风险验收已完成，实际证据见主卡 B01 记录；仅此纯块完成。 |
| B02 Human canonical+树服务库及测试准备 | 按[B02正式卡](d12-b02-knowledge-service.md)的29新领域与共享6新/9旧闭包推进；当前仅 C1获授权，其他路径按所有权接续。真实D05 Object Audit/stop、P通用Human Outbox gate/Knowledge Audit路由与正式迁移仍为未验集成门槛。 | fake/repository纯测不等真实业务通过。正式 schema必须唯一迁移号/资源排程；本候选不抢00014及活动旧D05。Owner/currentSession/once-union/receipt/真实对象发布/Unknown均须真实PG+MinIO后验。 |
| 窄共同口 | cursor text scalar；Knowledge download/delete Audit 的 action/resource/producer/metadata及真实CheckAppend；必要D05清理能力。每项先核既有已验成果，单独授权旧路径与兼容迁移，不交B01随意改。 | download Audit闭集现不存在；D08基线Audit authority只认ProjectAction，不是任意Knowledge记录的授权。只对危险删除与D05要求的受控下载补必要Audit，普通低风险命令不机械复制运行内容。 |
| B03 participant与实际装配 | 等D08 P/D05 stop对应稳定接口和实现验收交接；D12 own事实源与上述清理口齐备后，唯一runtime/fixture资源。 | 真实archive/delete竞争、reader/writer/source join、exact cause、恢复/迟到结果不得复活；D08整体未验的接口不能以候选或C0替代。 |
| D13/Tool/HTTP后续 | D13提供索引状态/解析/serving、D18/19/21/22提供真正Agent/Tool调用和权限，D27 HTTP/UI。 | 不阻断Human纯树/内容设计与独立模块代码；无生产空allow/no-op索引、无Agent伪装Human、无提前根装配。 |

最小验收集按边界执行，不机械整库重跑：

1. B01：missing/null/重复字段/未知枚举、token篡改/kid/版本/跨User/Project/root/过期/时间边界、输入slice/pointer防别名；同义摘要/异义冲突、不 trim正文、换Session不换主体；无环/自环/后代环、跨Project、expected parent陈旧与根null；子树摘要对输入排列不敏感，对任一节点增删/移动/version/status变化敏感；content vs move事件严格分离。
   rev2 增补仅对应载体：另一个消费包经正式构造/Details/Matches完成 plan与流的类型闭包；零值/typed-nil/错issuer/另plan/错Tx/弱锁和篡改自身投影拒绝；SourceInput值副本并发Take仅一次、Close转交/重复/真实错误，CanonicalRead错object/scope/MIME/range及关闭后Read。纯 fake reader 只验证包装责任，不声明真实 I/O join或数据库持锁。
2. B02真实竞争：A→B与B→A同时移动只能一个合法；Move与正文更新互不覆盖；preview后新增/移出/更新/重命名/删除使确认失效，scope检查和写之间阻止phantom；整子树tombstone/Audit/Event任一点失败全rollback；相同title不冲突，cursor跨parent/filter/用户不可用。
3. 权限/对象：撤销Session、foreignProject、archived读/写、prospective伪cause、任意旧ObjectID/旧version、source发布前变化、完成后source删除再重放；D05 prepare/reserve/send/publish真实故障及真实COMMIT响应丢失；无双Document/双version/误读旧payload，无provider不给URL/字节，Audit失败前不输出。
4. 运行时：body替换立即排旧serving，索引失败不丢canonical；全Project删除包含tombstone与pending upload/source，旧Process未死亡不得接管，物理清理Unknown不completed，旧command/事件/回调不复活。D13未启用时只证明当前eligibility事实/事件，不宣称已跑检索。

## 8. 已有规则交叉与当前裁定边界

**Agent delete-doc 的后续适配未闭口，不阻断本次 Human 结果。** 精确依据：

- `resources-skills.md:160–163` 的 `PrepareDeleteSubtree(ctx, Human,...)` 与 `DeleteSubtreeInTx(ctx,tx,Human,...)`；同文168行 token绑定Actor/Project/root与当前范围。
- `knowledge-document-domain.md` §12 把 delete-doc列为可配置Builtin，§12.3要求统一安全/治理；`default-approval-policy.md:149–173` 明确 delete-doc默认 destructive、需要Approval，且Approval不能绕过领域约束。
- D01 `model-tool.md:195–205` 的Human决议/真实Operation fingerprint与执行前校验，不包含将Approval转成Knowledge Human confirmation token、替换Actor或免除子树确认的许可。

因此已能确认“默认Approval”框架，不新增产品问题或立即请求用户选择；当前候选只签发/消费真实当前Owner的Human确认。D18/D19/D21开工适配时必须精确绑定实际操作与同一范围，若确需把D01 Human-only端口改为Agent destructive端口则先回主线程审明文变更，不能猜测默认自动批准、冒充Owner或只删root而忽略子树。普通Human树CRUD继续。

其余本候选采用的是已有明文规则和待审工程参数，没有发现必须立刻改变产品架构才能完成B01的冲突。本文不把未来缺口标作已完成，也不要求先等待D09/D13整体结束。
