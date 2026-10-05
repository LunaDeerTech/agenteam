# D08 Project 与 Owner 实施规格

修订：rev2 / 已经独立静态审查并由主线程采纳，现正式归位。B01 契约/纯规则库已独立验收并提交推送 `199554b`；B02 存储与 Owner/创建/元数据服务、00013 和 fixture 接缝已独立验收并提交推送 `6319d03`。D08 整体仍未完成，B03 完整生命周期、B04 HTTP/app 和 D10 真实 Skill 初始化尚待实施和集成。设计输入固定为 `062ae2c050e2f6fa1549d2e67f924b4e5160d754`；49项输入及作者冻结证据位于 `/tmp/agenteam-d08-design-rev2-nm2kwP/`，审查证据见[工作项](d08-project-owner.md)。原设计与归位阶段未读取活动 B03/B04 业务源码；B01 修复闭环与 B02 的首轮三红、修后真实组合证据分别见[主卡 B01 记录](d08-project-owner.md#b01-独立验收与提交)和[B02 记录](d08-project-owner.md#b02-独立验收与提交)。

当前依赖进度补记：D07 B03/00011 `ffa65f0`、A1 CurrentUserRoute `59b38c8` 与 A2/00012 `da5caab` 均已验；B02 在固定 `da5caab` 上完成真实 PG/初始化消费/Owner/Audit/Outbox 组合，21 文件与 00013 提交推送 `6319d03`，路径已接入正式 Route。§1 表保留原固定设计输入的历史状态，当前开工以本段及主卡为准。B03 主体与后段可按真实依赖分阶段，§9.1/9.2 的 C0 精确 Go 口已获独立定向审查并由 root 采纳，四个新契约/测试文件作者实现及 unit/race/vet 已完成、待独立验收；旧 D05/Secret 文件、可能的新迁移与 Docker 仍由 root/验收负责人逐项交接。此状态补记不改变下述产品规则与正式契约，也不涉及 D09 未决创建模型字段。

用户最新授权允许按真实依赖并行；固定提交内旧“全局单模块/最多三个”规则不再决定本次排程。本文不把设计、纯契约库或测试替身等同于正式生产绑定。

业务依据：[Project 架构](../../architecture/project-work-management/README.md)、[D01 基础契约](d01-contracts/foundation.md)、[D01 生命周期](d01-contracts/domain-lifecycle.md)、[Skills 契约](d01-contracts/resources-skills.md)、[事件契约](d01-contracts/runtime-events.md)、[开发计划](../development-plan.md)。

## 1. 开工结论与范围

已验 D07 B01 的 `account.Authority.RequireCurrentSession`、`AuthorizeSystem` 和 `TouchActivityInTx` 足够支撑按稳定 Project ID 的 Owner 授权、事务内 Session 检查和成功人类命令活动记录。D08 不需要 SMTP、头像、个人偏好或完整 D07 HTTP 才能形成独立服务库；SystemAuthority 不能授予其他 Owner 的项目权限。

以下事实仍阻止宣称“D08 全部依赖已经满足”：

| 依赖 | 固定输入中的真实状态 | 本项处理与完成责任 |
| --- | --- | --- |
| Account 当前身份 | B01 库已验，非零 Tx 要求已持 User SH，Touch 要求 User EX | 直接复用；D08 不读写 account 表 |
| 当前本人 username 与 Project 同 Tx 检查 | 无公开 InTx 投影；GetSession 自开事务且需要 Cookie | §4 窄口由 D07 B04-A 的 account/profile.go 提供；已与该设计负责人核对，尚未实施/验收 |
| D03 连续迁移 | 固定输入最大 00010；B03 持有未验 00011；扫描拒绝缺号 | D08 迁移号由 root 预留，00011 已验后才可把后续连续 manifest 作为真实数据库验收输入；禁止占位/改号绕过 |
| D04 typed Audit | 有真实 Appender/Cleaner；尚无 Project action/resource/producer | D08 增加闭集及本域授权分派，不能借 account/secret 动作写假审计 |
| D05 Artifact/Object | 有真实 CleanupProject、上传、下载和权限消费口 | D08 绑定 Owner/Gate；§9 补正式项目范围停止/检查口，不能调用全局 StopAdmission 停全部租户，也不能用 Cleanup 代归档 |
| D06 Outbox | ProducerAuthority、ProjectAuthority、LifecycleParticipant/Resolver 已有真实库 | D08 按 exact cause/step/当前 gate 绑定；删除清理须排在所有事件生产者收束之后 |
| D10 Add Skills | D01 正式语义已定，实际服务未实现 | §7 正式初始化端口；缺绑定时 Create 明确 503/unbound，不能生产创建成功。D10 负责真实对象与技能初始化集成 |
| HTTP/进程身份装配 | B04 尚未验收 | D08 HTTP/app 卡等待 B04 稳定接口及相关文件所有权；不能覆盖活动装配 |
| 未来 Agent/Execution、订阅等 | D10–D25 未实现 | 逐域真实绑定；未知 AgentRun 或未绑定订阅端口明确拒绝，不凭项目 ID 返回成功 |

D08 拥有 Project 身份、名称空间、描述、初始化可用门禁、Owner 权限、生命周期进度与最小删除回执。各领域继续拥有自己的业务数据、停止证据、清理及引用。D08 不直接操作 account、Audit、Outbox、Object、Skill 或未来领域表，不建立跨域数据库级联。

## 2. 名称、路径与字段

`ProjectID` 使用 `identity.ProjectID`，不另造可混用的 ID 类型。所有 ID/版本/时间沿 D01；正版本从 1 开始，计数使用 Progress，JSON 整数用规范十进制字符串。

Create请求必填 `project_id`：请求发起方为一次创建意图生成一个合法UUIDv7，所有重试保留同一target ProjectID及key，不能在Unknown后换ID。它只是待创建资源的稳定身份，不是权限凭据；Owner始终由服务端当前Human绑定，不能随请求指定。服务端在同User+Project锁下核目标未被任何live/保留创建/最小删除receipt占用；曾永久删除的ID不可重用。创建时间取DB当前时间，不从客户端UUID时间位推断授权、年龄或created_at。此为root采纳的rev2工程边界，用已有最小delete receipt防止删除后的迟到Create复活，不新增创建历史墓碑。

项目名称为 1–64 个 ASCII 字符，仅 `[A-Za-z0-9._-]`；拒绝单段 `.`、`..`。此嵌套名称空间不额外保留 `admin`、`api`、`settings` 等用户名保留字。`name` 保留通过验证的输入大小写，`normalized_name` 为 ASCII 小写；不 trim、不 Unicode 兼容折叠、不进行百分号解码。大小写-only 改名属于可见字段修改，推进版本，路由保持同一小写段。不同 Owner 可同名。

`description` 为 UTF-8，最多 8192 bytes，允许换行/制表符，拒绝 NUL、除 LF/HT 外的 C0 控制字符和 DEL；不 trim、不重新格式化。创建省略时为空字符串，显式 null 拒绝。更新只写出现的 `name/description`，至少一项；未知字段及 owner/version 等不可写字段拒绝。

`ProjectRef`：`id,owner_user_id,name,normalized_name,description,lifecycle,version,current_sprint_id?,created_at,updated_at,archived_at?`。lifecycle 仅 `active|archiving|archived|deleting`，不增加 soft-deleted、purging 或初始化业务状态。`current_sprint_id` 首期为 null；D11 以后通过正式 Project InTx 口维护同项目约束，D08 不创建空 Sprint 表或接受客户端写入。

标准路径为无前导斜杠的 `canonical_username/normalized_name`；页面可加首个 `/`。路径 resolver 接收两个已经由 HTTP 框架单次解码的独立段；拒绝 `/`、反斜杠、百分号、空段及点段，不做第二次解码、自动跳转或历史别名。客户端大小写折成 ASCII 小写比较；不 trim。`admin` 是已有合法 bootstrap 用户名，路由读取不能套用禁止新建保留用户名的校验函数。

本人可见项目列表按 `(created_at DESC,id DESC)` keyset 排序；筛选 lifecycle 为闭集集合，省略表示全部四态。默认 50、范围 1–100，超限报错。cursor 使用已有签名库，scope 为 System 的编码容器但 digest 明确包含 `query=owned-projects-v1,user_id,lifecycle_filter,order`；这不授予 System 权限，不把 SessionID 或 limit 放入 digest。每页重新校验当前 Session，并在 SQL 中限定真实 Owner；跨用户、筛选、顺序复用 cursor 拒绝。遍历不承诺快照隔离。

## 3. 本域持久事实

新增 schema `agenteam_project`，使用一张由 root 分配的事务型 Up-only 新迁移，保留所有已提交旧 SQL 字节。索引/约束随同迁移，禁止通过运行时启动自动建表。

| 表 | 字段和约束 |
| --- | --- |
| `projects` | UUIDv7 id PK；owner_user_id NOT NULL（稳定外域 ID，不以跨域级联管理用户）；name/normalized_name/description；lifecycle CHECK；version>0；current_sprint_id nullable；created_at/updated_at；archived_at nullable；creation_id UNIQUE；initialized_at nullable；current_lifecycle_operation_id nullable；UNIQUE(owner_user_id,normalized_name) 覆盖所有行及阶段；CHECK normalized_name=ASCII lower(name) 及名称规则 |
| `creations` | id PK、project_id UNIQUE、owner_user_id、command_key、semantic_digest、request_name/description、state=`accepted\|initializing\|failed\|completed`、initialization_key、protected_skill_id/revision nullable、safe_reason nullable、version、created_at/updated_at；target ProjectID就是create命令scope，一个target至多一个creation，不建立跨Project的(owner,key)唯一性。完成后清原输入副本，只留关联和安全结果；Project 删除时清整条 |
| `commands` | id、project_id、actor_user_id、command_name、key、semantic_digest、state=`planned\|completed`、safe_result、event_ids、created/committed_at；UNIQUE(project_id,command_name,key)。允许的命令闭集 update/archive/restore/delete/retry-lifecycle；planned 只用于多阶段规划，不冒称业务成功 |
| `lifecycle_operations` | id PK、project_id、owner_user_id、action=`archive\|delete`、project_version（接受 gate 的固定版本）、completed_project_version nullable、state=`accepted\|stopping\|cleaning\|completed\|failed`、resume_phase、cleanup_stage=`domains\|outbox\|audit\|final`（仅 delete cleaning适用）、version、required_manifest、manifest_digest、safe_reason、retry_after、created/updated/completed_at；每 Project 至多一条未结束/失败待续 operation；archive 不得进入 cleaning |
| `lifecycle_participants` | (operation_id,participant_name) PK；contract_version、stop_state=`required\|pending\|stopped\|failed`、cleanup_state=`not_applicable\|required\|pending\|completed\|failed`、checkpoint_schema/checkpoint、safe_pending_refs、safe_reason、attempt_count、next_attempt_at、version；checkpoint 带原 ProjectID/OperationID，不接受用户回填 |
| `work_claims` | (work_kind,work_id) PK；process_id、attempt_id、fence>0、phase、actual_join/终局检查的持久关联；只用于初始化及生命周期 worker 的持久恢复。不能以 timestamp/TTL 推断工作已结束 |
| `deletion_receipts` | operation_id PK、deleted_project_id UNIQUE、original_owner_user_id、command_key_hash、request_digest、completed_at、status 固定 completed；无 live Project FK、无 name/path/description/Project Audit 副本；按 owner/operation 和 (project,owner,key_hash) 查 |

所有本域表可用受控本域 FK，删除顺序由 finalizer 显式确定。Owner 名称唯一索引是最终防线；User EX 序列化创建/改名以及对应账号变更。禁止按可复用路径保存外域清理目标。

`initialized_at IS NULL` 是创建工作尚未完成的内部可用门禁，不是第五个 Project lifecycle；这样的保留行只对 exact 初始化 cause 和原 Owner 的 creation status 可见，不出现在正常项目列表/ProjectRef/普通下载或业务权限中。接受创建后名称占用；失败保留可重试 creation，不伪造可用 Project，也不自动删除已经产生的技能对象。

事件 ID、OperationID、CreationID 在首次规划时生成并持久化；ProjectID沿首次请求的稳定target，重复请求不重新换任一ID。所有表无自动 TTL。删除最终清理除最小删除回执以外的本域项目事实；旧 create/update 返回值随 Project 永久删除失效，不能保存正文用来重放。迟到worker只能引用旧CreationID/ProjectID，找不到事实即拒绝；迟到HTTP Create同target即使creation已清，也在原Project锁下被最小deletion_receipt永久拒绝，不能按旧key/name重新建另一ID。新创建意图必须选择新target ProjectID，不复用旧ID。

## 4. 身份端口与锁纪律

### 4.1 账号窄口

在 `account/contract/profile.go` 定义，由 D07 的 `account/profile.go` 实现：

```go
type UserRoute struct {
    UserID identity.UserID
    Username string // 当前持久 canonical 小写值
    Version foundation.Version
}
type CurrentUserRoutes interface {
    CurrentUserRouteInTx(context.Context, foundation.Tx, identity.Actor) (UserRoute, error)
}
```

实现显式拒绝 zero/失效/异 Store Tx；要求已持 Human 的 User SH（EX 覆盖），同 Tx 检查当前 Session/User/expiry/keyring，返回真实本人 username/version。不开 Tx、不取锁、不升级、不 touch、不读取任意他人目录。D08 普通稳定 ID 接口不依赖此口；resolver/永久删除路径确认缺口返回 DEPENDENCY_UNBOUND。

### 4.2 Project Authority

`project.Authority` 只依赖 Project Store、已验 SessionAuthority、可选 CurrentUserRoutes、注册验证器；不依赖 Audit/Outbox Service，避免构造循环。外层 Service 才依赖 Audit/Events/Initializer/Participants。

```go
AuthorizeProject(ctx, tx, actor, projectID, intent) (identity.AccessGrant, error)
RequireOwnerInTx(ctx, tx, human, projectID, intent) (ProjectAccess, error)
ResolveProjectPath(ctx, human, username, projectName) (ProjectRef, error)
ValidateLifecycleInTx(ctx, tx, serviceActor, cause, participant, phase) error
ValidateInitializationInTx(ctx, tx, serviceActor, creationID, projectID, key) error
```

`ProjectAccess` 为内部不可 JSON 反序列化结果，只包含当前 ProjectRef、授权检查时间/版本；不能跨请求复用。zero Tx 的 AuthorizeProject 可自行创建短只读 Tx；非零 Tx 只使用调用方 Tx 并 RequireHeldLocks，不补锁。RequireOwnerInTx 必须非零 Tx。

Human 每次实际校验 Session，再读 Project.owner_user_id；管理员没有旁路。无对象/非 Owner 对外均 NOT_FOUND，内部允许安全诊断 Forbidden。SystemAuthority 仅在 System 分派中使用；不作为 Project 检查的前置捷径。

| 当前阶段 | Human 普通读取 | Human mutation/launch/resume | 生命周期/内部事实 |
| --- | --- | --- | --- |
| 未完成初始化 | 不给 ProjectRef；仅本人 creation status | 拒绝 | 仅 exact creation 初始化/确认 |
| active 且 initialized | Owner 可读 | Owner 通过本域规则；未来 AgentRun 另需真实执行授权 | 可 archive/delete |
| archiving | Owner 只读 | PROJECT_NOT_ACTIVE | 原 operation 停止/有限收敛；不能改名/restore/delete 抢占 |
| archived | Owner 只读，下载仍核当前资源权限 | PROJECT_NOT_ACTIVE | 可 restore/delete；限定已提交事实收敛 |
| deleting | 仅 Owner 安全生命周期状态，不开放正文/下载 | PROJECT_NOT_ACTIVE | exact stop/cleanup/收敛；不 restore |
| 已物理删除 | 仅原 Owner 当前 Session 的最小 receipt | NOT_FOUND | 已完成 exact cause 可核技术 receipt，不得写回任何项目数据 |

AgentRun 若无 D22 实际 Execution/当前授权 provider 返回 DEPENDENCY_UNBOUND；仅 ID 相等不是权限证明。通用 Service 的 `AuthorizeProject` 不给 Owner 权限；正式消费口按已注册 service、持久 cause、原 Project、participant、phase 和已持锁分别校验。任意 Service 自报 converge/lifecycle 拒绝。

### 4.3 一次完整锁集合

默认 Read Committed，沿既有顺序：command identity EX → User/System locks → Project gate → 子域锁 → record locks；调用前发现/准备所有参与者依赖，再一次 AcquireAll。InTx 端口只 RequireHeldLocks，缺锁/弱模式/错 Tx 必须使事务失败。

读/分页：User SH；读单项目同时 Project SH。元数据更新、创建接受、生命周期人类接受/恢复/重试：User EX（支持成功后 TouchActivityInTx），Project 元数据行或生命周期切换使用 EX；普通其他领域 mutation 仍 Project SH。项目本身改名/version更新与所有读取事务线性化，不把共享门禁当项目行互斥。后台 progress 更新仅 Project EX+对应记录/command 锁；不拿已经过期原 Session 冒充 Human。

创建同时锁请求的target ProjectID与当前Human User EX；unique index覆盖竞争，不能用其他Owner的对象或回执授权本次创建。resolver先在Tx外按(actor.UserID,normalized_name)仅预扫描候选ProjectID，再一次AcquireAll(User SH+该Project SH)；同Tx调用CurrentUserRouteInTx并重读本人/项目名称映射，请求username非当前本人则NOT_FOUND。预扫描结果不向用户返回、不给权限；锁后名称/映射变化则结束短Tx并重新规划，禁止逐步补锁或使用旧grant。无候选时仍短Tx核当前Session/本人route后返回NOT_FOUND。删除确认在同 User EX + Project EX 中读当前 username、Owner、project name、version。

Audit、Outbox PrepareAppend、对象计划与技能完成确认需要的锁全部在外层并集；不在持锁事务内调用新 Discover、外部初始化、MinIO、Runner 或等待 join。

## 5. 服务命令与查询

以下为精确职责签名；具体 Go 参数采用同名 typed request，禁止 map/任意 patch 或不带鉴权语义的 bool。所有方法接 context，返回 foundation Fault；Meta 为现有 CommandMeta。

```text
CreateProject(ctx, Human, Meta, {project_id, name, description?})
  -> CreationResult = ready {ProjectRef} | accepted {CreationOperation}
GetCreation(ctx, Human, creation_id) -> CreationOperation | ready {ProjectRef}
GetProject(ctx, Human, project_id) -> ProjectRef
ListOwnedProjects(ctx, Human, {lifecycle[]}, PageRequest) -> Page<ProjectListItem>
UpdateProject(ctx, Human, Meta, project_id, {name?, description?}) -> ProjectRef
ResolveProjectPath(ctx, Human, username, project_name) -> ProjectRef
BeginArchive(ctx, Human, Meta, project_id) -> LifecycleOperation
RestoreProject(ctx, Human, Meta, project_id) -> ProjectRef
BeginDeleteProject(ctx, Human, Meta, project_id,
                   {normalized_current_path, permanent:true})
  -> LifecycleOperation | ProjectDeletionReceipt
GetLifecycle(ctx, Human, project_id, operation_id)
  -> LifecycleOperation | ProjectDeletionReceipt
RetryLifecycle(ctx, Human, Meta, project_id, operation_id)
  -> LifecycleOperation | ProjectDeletionReceipt
LookupCommand(ctx, Human, CommandLookupRequest) -> committed | in_progress | not_observed
```

`CreationOperation` 含 id/project_id/state/version/固定 reason/created_at/updated_at，不含技能对象路径或私有输入。`LifecycleOperation` 沿 D01，参与者与 pending refs 为注册闭集和稳定安全 ID，最多 100 条，超过时 `pending_refs_truncated=true`；不能截断事实检查，仅截断 DTO。`ProjectListItem` 在 deleting 时只给 id/name/lifecycle/version/operation_id，不给 description。删除确认值按 §2 的两个独立段规则规范化为小写后比较，拒绝前后空白、前导/尾随斜杠、额外段和百分号；canonical路径进入语义摘要。

Create 禁 expected_version；Update/Archive/Restore/Delete 必填 Project expected_version；RetryLifecycle 的 expected_version 明确针对 operation.version，未完成响应同时给 Project.version。Retry 只把该 operation 的 failed/pending 步骤排入原 phase，不换 operation/cause，不撤门禁。其 Go 返回类型统一为已有 `LifecycleResult`（`Operation|Receipt`）：正常/归档路径返回 Operation；completed archive 的原 retry 拒绝；completed delete 仅在当前 Session、原 Owner 和精确 ProjectID/OperationID 下只读确认最小 Receipt，不声称已证明 retry key 同义，不恢复旧 operation、Project.version 或已删内容。输入仍须通过既有 Meta/key/正 expected_version 的语法校验，但不验证已删除的 retry 历史；不执行新 phase、Touch、Event/Audit 或任何写入，不增加保留字段。

普通已认证成功命令首次提交在同事务 TouchActivityInTx；读取、轮询、后台推进、幂等重放不刷新 idle。同 key 已接受的生命周期命令重放返回原 operation 的当前安全状态；不拿旧 expected_version 再拒绝已成功接受的事实。

## 6. 幂等、取消和 unknown

Create与普通Project命令均使用D01既有namespace `project/[target_project_id]`；Create的command为`create`，其余为§3闭集。主体为稳定UserID，不纳入SessionID。语义摘要用现有canonical-v1，包含命令、target、主体、原expected_version、所有语义字段及presence；名称校验后使用要保存的name和normalized_name，描述原样。重放Update同key但不同omitted/explicit字段语义不得碰巧合并。

Create冲突闭集：同target/同key/同义在当前Owner校验后重放或恢复原creation；同target/同key/异义为IDEMPOTENCY_KEY_REUSED；同target/异key且同Owner为RESOURCE_BUSY（`/project_id:TARGET_OCCUPIED`），不得接管原未决初始化或返回其历史内容。target已经属于另一Owner的live/保留创建/receipt均NOT_FOUND，不暴露该Owner或状态。两Owner并发抢同target依Project EX+唯一约束只一人接受，另一方同样NOT_FOUND。同target已删时原Owner当前Session得到RESOURCE_DELETED且不返回已删内容，其他Owner为NOT_FOUND，任意key都不能复用该ID。

同raw key但**不同target**属于不同Project scope，可独立接受；它是明确的新创建意图，不是原命令同key异义或Unknown重试，不承诺全账号跨Project永久key历史。这避免在删除后额外保留Create key；同scope的同key不同业务参数仍严格拒绝。服务/API调用方必须把target与key一起保存和重试；Lookup/恢复/内部job绝不自行生成另一target。

每次先当前身份及结果可见性，然后同 identity 锁下比较摘要；同 key 异义 IDEMPOTENCY_KEY_REUSED。已成功同义返回安全历史结果，不重新执行、不重验旧 version。删除后只认最小 delete receipt，不恢复旧 Project 内容；completed-delete Retry 按 §5 作只读状态确认，不适用已删除 retry 历史的同义重放声明；receipt 的 key hash 对 `project_id,original_owner_user_id,key` 使用固定 domain-separated SHA-256，输入是非秘密业务 key，HTTP 不输出 hash/digest。

普通 update/restore 的 Project、version、Audit、typed Event、command receipt 和首次 Touch 同事务。Archive/Delete 的 gate、operation、required participants、Audit、接受事件、command receipt 和首次 Touch 同事务；任何一个写失败全回滚，不返回已接受。

外层 ctx 取消只停止本次等待；已接受 creation/lifecycle 不因断连丢失。接受前明确回滚可沿原 key 同义重试。Commit Unknown 一律保留原 provenance 与 cause，外层用相同 command identity/Project gate 取得原 writer 排他序列后读 canonical receipt；确认事务超时/55P03/失败不是原事务未提交证据。只有确证串行后的 committed/absence 才收敛；否则 COMMIT_UNKNOWN，retry_hint=lookup，不生成新 ProjectID、OperationID、EventID 或 key。

后台初始化/参与者调用与 PG 不在同一个事务内：调用返回成功但 checkpoint 未知时，下一轮先检查原 exact cause/provider checkpoint。不得因当前无活动连接、HTTP 超时或单次 SELECT 缺行就重放外部副作用。提供受控 COMMIT 丢响应的 committed/rollback/仍挂起三类验收。

## 7. 项目创建与正式 Skill 初始化

D01 的 `InitializeProjectSkills(ctx, service_actor, project_id, initialization_key)` 保持业务含义。D08 新建消费契约位于 `project/contract/initialization.go`，D10 通过 adapter 实现，不使 D08 import D10 实现包：

```text
InitializationRequest {creation_id, project_id, initialization_key}
InitializationResult {state: pending|completed|failed,
                      creation_id, project_id, add_skills_id?, revision?, safe_reason?}
ProjectSkillInitializer.InitializeProjectSkills(ctx, service_actor, request)
  -> InitializationResult
ProjectSkillInitializer.InspectProjectSkills(ctx, service_actor, request)
  -> InitializationResult
ProjectSkillInitializer.DiscoverConfirmation(ctx, service_actor, request)
  -> InitializationConfirmationPlan
ProjectSkillInitializer.ConfirmInitializedInTx(ctx, tx, service_actor, request, plan)
  -> InitializationReceipt {creation_id,project_id,add_skills_id,revision}
```

Plan 是 provider 私有 issuer + 完整 request/actor mapping +复制 RequiredLocks 的 opaque 值；Discover 只规划，不授权；Confirm 必须同调用方 Tx/完整已持锁重验真实 Skill 所属 Project、protected Add Skills、已发布且可引用的 revision/对象，以及 exact 初始化记录，不接受 caller 标记 completed。无锁补取/嵌套 Tx/外部 I/O。D10 加入 Project manifest 的 `agent-skills-variables` 参与者之后才能启用该 adapter。

新增封闭服务职责 `identity.ProjectInitialization`（值 `project-initialization`），只由组合根注册。Actor ProjectID 为原保留 Project，CauseRef 为 CreationID；Project Authority 核实未完成 creation、原 ID/key、当前初始化门禁和该服务职责。不得复用 archive/delete cause 伪造初始化，也不给此服务通用读写项目能力。

创建流程：

1. 当前Human与输入基本校验后，先按§6在同identity/User/target Project完整锁下检查当前可见的已完成receipt、已有creation及最小delete receipt。同义已完成creation安全重放优先，不能因后来initializer unbound/容量变化拒绝它；已删target直接拒绝。只有新接受，或确需继续外部初始化的未完成creation，才检查initializer必要绑定/容量。新接受缺口返回DEPENDENCY_UNBOUND且零Project/name reservation；旧未完成creation缺口保留原ID/进度并明确失败，不能换target重建。
2. 以完整锁并集提交 creation+不可用 Project 保留行+接受命令/Audit。此阶段不是 ProjectRef 成功，发生未知先核原接受。
3. Tx 外以持久初始化 key 调 D10。pending/Unknown 保留该 creation/name，真实失败记录 failed/安全原因；不创建空 Add Skills，不返回 201，不把对象上传成功当技能发布成功。
4. provider completed 后 Tx 外 DiscoverConfirmation；同 Project EX 和其完整计划锁下 ConfirmInitializedInTx，检查 creation 尚在原身份/阶段，原子写 initialized_at、completed、固定 Add Skills reference、Project 创建 Event/Audit/最终 result。version 对可用 Project 初始仍为 1；初始化阶段用 creation.version，不伪造多次业务更新。
5. 首请求可在原 caller 总预算内得到 ready/201；持久 accepted 后未完成返回 202/status URL。明确失败返回安全 Fault 并可用 GetCreation 查询 failed；重试相同 Create key 恢复同 creation。后台恢复不重复 revision，重启和响应丢失都不产生第二个 Project。

没有 D10 adapter 时，D08 服务库的 Create 正向由隔离测试 initializer 验边界，生产仅 fail-closed。该事实写进台账；D10 真实创建及失败/恢复/清理组合通过前，不宣称用户已经能创建完整项目。

## 8. 生命周期编排

状态边保持 D01：active→archiving→archived→active；active/archived→deleting→物理删除。archiving 与 deleting 互斥；failed operation 保留原 gate/名称/完成进度，不自动恢复 active。用户先恢复/完成原归档操作再提出 delete；不额外发明取消归档处理中命令。

Registry 来自受信任组合根，条目固定 name、contract_version、enabled owner module、stop/cleanup 能力、清理先后关系。缺必要绑定是明确缺陷；不能按“当前没数据”“目录为空”“还没看到事件”自动删 required participant。初始 D08 已存在领域要求 `artifact-object,secret,outbox,audit`；启用 D10 初始化时再要求 `agent-skills-variables`。D28 完整集合沿 D01，各模块启用时登记，不能沿用 D08 小集合冒称全平台清理。

接受操作时把 required manifest 与版本/digest 同 Tx 持久化，之后不缩减。部署升级恢复旧 operation 必须保留兼容 adapter；移除或缺版本使该 operation pending/failed，不当作已完成。新模块注册不得在已有 deleting/archiving 项目上建立普通数据；当前 operation 的集合同步只能在保证没有遗漏既存数据的工程升级规格下增加，不能随进程重启默默替换。

```text
ProjectLifecycleParticipant.Name() -> ParticipantName
RequestStop(ctx, ServiceActor, LifecycleCause, ScopeRef) -> StopReport
InspectStop(ctx, ServiceActor, LifecycleCause, ScopeRef) -> StopReport
Cleanup(ctx, ServiceActor, LifecycleCause, ScopeRef, checkpoint?) -> CleanupReport
ScopeRef = Project {project_id} | Meeting {project_id,meeting_id}
LifecycleCause {operation_id,action,project_version}
StopReport {state: stopped|pending|failed, active_refs[],unknown_refs[],safe_reason?}
CleanupReport {state: completed|pending|failed,checkpoint?,remaining_refs[],safe_reason?}
```

D08 只编排 Project Scope；Meeting variant 保留正式 type，由 D24 绑定，未绑定拒绝。参与者每次从 Project 正式授权口验证当前 exact cause/phase；不得直接读取 Project 私有表。幂等身份固定 `(operation_id,participant,phase,resource_id)`，checkpoint 每次单独短 Tx 持久化。报表签名/类型不代替真实领域事实。

`LifecycleCause.project_version` 始终是接受 gate 时的固定版本，progress 更新只推进 operation.version。归档完成推进 Project.version 后把新值记入 completed_project_version，保留当前operation指针供有限终态检查；不改旧cause或重新执行stop。Restore或新delete接受后，旧operation不再是当前pointer，迟到旧cause不能取消/清理新工作。永久删除后 retry command/key/operation version 历史一并删除；RetryLifecycle 仅作 §5 的当前授权终态确认，使用不同合法 retry key 也只返回同一最小 Receipt，不能证明旧 retry 同义。原 Delete 重放仍严格匹配保留的 command_key_hash/request_digest；无法辨识的 LookupCommand(retry-lifecycle) 返回 RESOURCE_DELETED。最小删除receipt按D01不增加project_version字段；已删项目的下游终态检查仅凭匹配的operation/project/delete身份定位，各下游再核自身技术receipt里的原version，不能借这一只读分支执行新phase。

Audit库与Secret库本身没有自主Project执行/外部业务worker：它们的普通canonical写入均在同Project gate内，生命周期接受EX屏障已等此前短事务结束，后续普通写由当前gate拒绝。其archive Stop适配器因此在同gate重验exact cause及冻结能力声明后可报告stopped，不调用清理、不把Secret lease视作业务已经停止；lease的实际使用者仍由执行/对象等所有者的Stop及后续Secret Cleaner核实。若后续版本给这些库引入自主异步Project工作，必须同时升级自己的Stop实现和manifest版本，不能沿用此屏障证明。此特例不适用于已有真实在途写入的Object/Artifact或Outbox。

顺序如下：

1. Begin同事务取得Project EX、核当前Owner/version/删除路径、关闭新业务gate并保存operation。若既有SH持有者令EX在预算内未取得，返回真实not_committed有界失败，零已接受operation、零参与者cancel；不得先取消再补授权，也不把D06接受之后的预取消机制误当能越过此原子接受门槛。后台只对已确认提交的operation按有限轮询RequestStop；取消请求接受/Runner离线/工具超时只算pending。
2. 持续 InspectStop；要求必要业务工作 actual join 或 exact 旧实例真实死亡且原事务已终局。未知资源保留在 pending refs。必要只读对象流可在 archive 下继续，不属于业务执行复活；delete 的 reader/source/transfer 使用必须在物理清理前实际收束。
3. 全部 stop=stopped 后，archive 在 Project EX 下复核 operation/cause 与所有进度，原子变 archived、Project.version+1、archived_at、完成 Event/Audit、operation completed。不运行 Cleanup。Restore 在当前 Human/Owner/expected_version 下原子变 active、version+1、清 archived_at并发事件/Audit；不重开旧 execution/delivery，不移除历史进度来冒充新动作。
4. delete 全 stop 后持久进入 cleaning。清理依赖图按领域资源引用排序：业务资源拥有者释放引用/清本域 → Secret 与 Artifact/Object 清理 → Outbox → Audit → Project finalizer。清理中的内部 Audit/停止 Event 在各自最后清理屏障之前允许；其他生产者仍在清理时禁止 Outbox Cleanup。
5. Outbox Cleanup 仅当除 Outbox/Audit/Project finalizer 外所有必要清理 completed 且无尚可派生事件的本域工作时授权。由 D06 在 Project EX 关闭所有 Append 并确认库内队列/marker/attempt 清零。不能把 stop/inspect 也绑到这个条件而造成循环。
6. Audit Cleanup 仅在 Outbox 与所有会追加 Project Audit 的资源工作已 completed 后授权；进入 audit 清理阶段即形成不可逆的新 Project Audit 写入屏障，D08 `CheckAppendInTx` 从此拒绝新 Project Audit。该清理轮失败、Unknown 或尚未写 completed checkpoint 均不能后移或重开屏障。System Audit 不动，不另外抄 Project Audit 到 System。
7. 最终Project EX下复核exact operation及所有必要最终回执；除当前finalizer的exact(process,attempt,fence) claim之外，不得存在其他未actual joined/未获exact死亡+原writer终局证据的活claim。本次claim从进入finalize一直持有，不提前释放给另一worker；在写最小deletion receipt、删除本域commands/creation/participant/op/project的同一最终事务内原子退休/删除本次claim，释放unique name。最终事务不追加Project Event/Audit，不重新污染已清理领域。提交未知沿同原claim/Project锁和最小receipt核实，不重新claim再清一次；新同名项目有新ID，不被旧worker/cause影响。

屏障后的 RetryLifecycle 仅豁免默认的 Project Audit 追加要求，不豁免当前 Session/Owner、exact Project/operation、既有同 key 摘要/重放与版本规则：按原幂等优先级核已完成 receipt，新接受 Retry 才比较 operation expected_version；原 phase 恢复、command receipt 和既定首次 Touch 仍同 Tx。重复同 key 不重复 Touch。只省略会重新污染项目的 Audit append，不换 cause、不撤 gate、不跳清理、不产生 Project Event；Outbox 清理屏障后的 Event 禁写也不因恢复而重开。屏障前 Retry 的 Audit 必须原子成功，失败回滚 receipt/phase/Touch；Unknown 仍须按原命令身份串行确认，不能因省略 Audit 直接成功。最终真实清理和最小 deletion receipt 保留规则不变；已物理删除后的只读 Retry 仍按 §5，无本域或 Activity 写入。

停止/清理调用不持长 Tx。Worker 单进程公平有限并行，默认最多 4 个 work item，每 Project 同时一个，单 participant 调用预算 2s 或更短 parent；超时仍追踪真实任务完成，不释放 claim/fence谎称已 join。无法及时 join 的 item 保持 pending，有空闲槽时其他项目公平继续；四槽全部仍有真实活调用时报告容量受阻，不无界再开goroutine。退避 1s、2s、4s 至 30s capped，不按次数判 stopped。panic 记固定安全原因并保留当前 phase，不能跳过。失败需要同 operation 恢复，checkpoint 不由 UI 决定。

## 9. 既有依赖的真实适配与必需窄补

### 9.1 D05 项目停止能力

固定 D05 只有 CleanupProject，没有项目范围 RequestStop/InspectStop。下列 C0 已经独立定向审查并由 root 采纳，代码依据为 `6319d03`；typed 口实现与真实 D05 stop/inspect 分阶段交付，契约成立不代表停止能力已绑定。Project 不越域查表推断空闲。

D05 第一段已由 root 授权 `d02_backend` 按固定 `/tmp/agenteam-d08-b03-d05-scope-pvns68a1/proposal.md`（SHA `39a505d98307bedf832b3dea8e14b88f18e03742342a4847a01648824c0ebc00`）实施，唯一新迁移为 `00014_object_artifact_project_stop.sql`，两个域各自拥有 project_stops/project_work 四张技术表；具体旧文件权限见主卡，不扩本节公开口。delete 必须清掉所有旧 archive receipts、扫描游标及临时 work，只留 current delete 最小 stopped receipt；source_project_id 必须纳入源 Project delete 扫描和真实 join。此为实施授权，真实停止与迁移验收尚未完成；Secret/Audit checker 后段独立推进。

以下均位于既有 `object/contract` 包；依赖仅 `context`、既有 `foundation`、`identity/contract` 及标准库。不得 import Project 包。opaque 类型的内部字段由实现者选择，零值无效。

```go
type ProjectStopOperation struct{}
type ProjectStopOperationID = foundation.ID[ProjectStopOperation]
type ProjectStopAction string
const (
    ProjectStopArchive ProjectStopAction = "archive"
    ProjectStopDelete  ProjectStopAction = "delete"
)
type ProjectStopCauseDetails struct {
    ProjectID      identity.ProjectID
    OperationID    ProjectStopOperationID
    Action         ProjectStopAction
    ProjectVersion foundation.Version
}
type ProjectStopCause struct { /* private immutable data */ }
func NewProjectStopCause(ProjectStopCauseDetails) (ProjectStopCause, error)
func (ProjectStopCause) Validate() error
func (ProjectStopCause) Details() ProjectStopCauseDetails
func (ProjectStopCause) Equal(ProjectStopCause) bool

type ProjectStopComponent string
const (
    ObjectStopComponent   ProjectStopComponent = "object"
    ArtifactStopComponent ProjectStopComponent = "artifact"
)
type ProjectStopStep string
const (
    RequestProjectStopStep ProjectStopStep = "request_stop"
    InspectProjectStopStep ProjectStopStep = "inspect_stop"
)
type ProjectStopRequestDetails struct {
    Actor     identity.Actor
    Cause     ProjectStopCause
    Component ProjectStopComponent
    Step      ProjectStopStep
}
type ProjectStopRequest struct { /* private immutable data */ }
func NewProjectStopRequest(ProjectStopRequestDetails) (ProjectStopRequest, error)
func (ProjectStopRequest) Validate() error
func (ProjectStopRequest) Details() ProjectStopRequestDetails
func (ProjectStopRequest) Equal(ProjectStopRequest) bool
func ProjectStopBinding(ProjectStopRequest) (foundation.Digest, error)

type ProjectStopAuthorizationMode string
const (
    ContinueProjectStop ProjectStopAuthorizationMode = "continue"
    ReadProjectStop     ProjectStopAuthorizationMode = "read_only"
)
type ProjectStopAuthorization struct { /* private immutable data */ }
func NewProjectStopAuthorization(
    foundation.Tx, ProjectStopRequest, AccessDependencies,
    ProjectStopAuthorizationMode,
) (ProjectStopAuthorization, error)
func (ProjectStopAuthorization) Validate() error
func (ProjectStopAuthorization) Mode() ProjectStopAuthorizationMode
func (ProjectStopAuthorization) Matches(
    foundation.Tx, ProjectStopRequest, AccessDependencies,
) bool

type ProjectStopAuthority interface {
    DiscoverProjectStop(context.Context, ProjectStopRequest) (AccessDependencies, error)
    ValidateProjectStopInTx(
        context.Context, foundation.Tx, ProjectStopRequest, AccessDependencies,
    ) (ProjectStopAuthorization, error)
}

type ProjectStopState string
const (
    ProjectStopped     ProjectStopState = "stopped"
    ProjectStopPending ProjectStopState = "pending"
    ProjectStopFailed  ProjectStopState = "failed"
)
type ProjectStopReason string
const (
    ProjectStopDependencyUnbound     ProjectStopReason = "dependency_unbound"
    ProjectStopDependencyUnavailable ProjectStopReason = "dependency_unavailable"
    ProjectStopWorkPending           ProjectStopReason = "work_pending"
    ProjectStopOutcomeUnknown        ProjectStopReason = "outcome_unknown"
    ProjectStopOperationFailed       ProjectStopReason = "operation_failed"
)
type ProjectStopRefKind string
const (
    StopObjectPreparation ProjectStopRefKind = "object_preparation"
    StopObjectUpload      ProjectStopRefKind = "object_upload"
    StopObjectAttempt     ProjectStopRefKind = "object_attempt"
    StopObjectLease       ProjectStopRefKind = "object_lease"
    StopObjectTransfer    ProjectStopRefKind = "object_transfer"
    StopObjectDownload    ProjectStopRefKind = "object_download"
    StopArtifactUpload    ProjectStopRefKind = "artifact_upload"
    StopArtifactCreation  ProjectStopRefKind = "artifact_creation"
)
type ProjectStopResource struct{}
type ProjectStopResourceID = foundation.ID[ProjectStopResource]
type ProjectStopRef struct {
    Kind ProjectStopRefKind
    ID   ProjectStopResourceID
}
type ObjectStopDetails struct {
    State                   ProjectStopState
    ActiveRefs, UnknownRefs []ProjectStopRef
    SafeReason              ProjectStopReason
}
type ObjectStopReport struct { /* private immutable data */ }
func NewObjectStopReport(
    ProjectStopComponent, ProjectStopCause, ObjectStopDetails,
) (ObjectStopReport, error)
func (ObjectStopReport) Validate() error
func (ObjectStopReport) Matches(ProjectStopComponent, ProjectStopCause) bool
func (ObjectStopReport) Details() ObjectStopDetails

type ObjectProjectStop interface {
    RequestProjectStop(context.Context, identity.Actor, ProjectStopCause) (ObjectStopReport, error)
    InspectProjectStop(context.Context, identity.Actor, ProjectStopCause) (ObjectStopReport, error)
}
```

#### 9.1.1 纯类型规则

- 每个 enum 具有 `Validate() error`、校验后的 `MarshalJSON() ([]byte,error)` 与严格 `UnmarshalJSON([]byte) error`：拒绝未知值、null 和非字符串；失败不污染旧值。Restore 不属于 stop action。
- Cause 的四字段都必填并严格验证 UUIDv7/正版本；Project 与既有 `project.OperationID`、`CleanupID` 之间仅经 `ParseID[K](id.String())` 显式转换同 UUID，不生成新 ID。
- Request 构造只接受 `identity.Service`、`identity.ProjectLifecycle`，Actor 的 ProjectID/CauseRef 分别精确等于 Cause 的 ProjectID/OperationID；没有 User/Session/Execution 变体。Component 由 Object 或 Artifact 实现者固定，Step 由所调方法固定。语法成立不表示有权限。
- Binding 使用域分隔 `object.project-stop.request.v1`，明确编码完整 ActorDetails、Cause 四字段、Component 与 Step。不能 hash opaque 类型的固定安全 JSON 标签。项目、operation、版本、action、actor、component、step 任一变化均改变绑定。
- Authorization 绑定同一个非零 `foundation.Tx`、完整 Request 和完整 AccessDependencies；只接收配置的 Authority 在本次调用返回的结果。`ReadProjectStop` 只可用于 Inspect；它不含取消、写入 stop gate、推进 phase 或创建回执的许可。授权构造器与其它既有 typed grant 一样只供可信 adapter 使用，不能作为任意业务调用的权限入口。
- Report 绑定 Component 与完整 Cause；构造/Details 均复制 slices。Ref ID 必须 UUIDv7，kind 为闭集；同一 `(kind,id)` 不得重复或同时出现在 active/unknown。Object 报告不能含 Artifact ref，Artifact 报告允许包含其调用 Object 返回的 Object refs。组合适配做保守合并，unknown 优先，不把两个不同 kind 的 ID 混成一个资源。
- stopped 必须无 active/unknown、无 reason；failed 必须有固定安全 reason；pending reason 可空或来自闭集。Unknown 永远不能投影为 stopped/failed；failed + outcome_unknown 的组合必须拒绝。列表是有界轮次的诊断投影，空列表从来不单独证明 stopped；实现者必须有完整扫描/终局证据。只有接到真实 stopped 报告才允许 Project 记录 stop 完成。
- opaque Cause/Request/Authorization/Report 的 `Format`、`MarshalJSON`、`LogValue` 仅输出固定安全标签；`UnmarshalJSON` 一律拒绝。显式 Details 只供可信 adapter 使用。错误不得包含原 SQL、对象 key、payload、名称或未清洗异常。

#### 9.1.2 Project Authority 的实际授权

`DiscoverProjectStop` 只输出计划。当前生命周期 actor 无额外 User 锁；至少规划 Cause.ProjectID 的 Project SH，mapping 必须绑定完整 Request。Object/Artifact 在 Tx 外预扫描各自本域对象/command/attempt 等映射，将其所需锁与 authority 依赖组成一次完整 union。每个物理 Tx 恰好一次 AcquireAll，不调用旧 ProjectCleanupAccess 来替代 stop，不在拿到 Project 锁后逐步补 Object/Command 锁。

`ValidateProjectStopInTx` 消费调用者 Tx；Project 实现先 `RequireHeldLocks` 核对自己的依赖，再重读当前 Project、operation、已冻结 manifest/participant/原接受版本。不能从 actor 字符串构造权限。Object/Artifact 同 Tx 重读自己的资源映射并核完整 union；映射改变则整 Tx 回滚，Tx 外重新规划。私有 issuer 指 D05 自己的本域批次计划/handle，不是 AccessDependencies：后者只有 mapping+locks 和公开构造器，不是不可伪造授权。配置的 Authority 必须按完整 Request 与当前映射重算依赖、核等并核已持锁；D05 私有批次计划另核自己的 issuer/完整绑定，不能消费其它服务或调用的计划。

| Project 当前事实 | 可返回的授权 |
| --- | --- |
| 当前 exact operation 正在接受后的 stopping phase，manifest 含对应版本的 artifact-object | Request/Inspect 可 continue；Request 幂等创建/推进本域同 cause gate，Inspect 只查看或收敛已有本域 gate，不能凭 Inspect 创建新 cause |
| 当前 exact operation 已越过 stop，进入 cleaning/finalizing，或尚未 Retry 的失败状态 | 仅 Inspect 对已有本域 stop 事实只读；不重新 cancel 或开启 stop phase |
| archive 已 completed，仍为当前 operation pointer 且 Project.version 与 completed_project_version 对应 | 仅 Inspect read_only；下游必须已有原 cause 的终态事实 |
| Project 已删，最小 delete receipt 精确匹配 Project/operation/delete | 仅 Inspect read_only；Project 不臆造 receipt 没有的版本，下游自己核原技术 receipt 的 action/version |
| Restore、新 operation、错误 cause/版本/manifest、无当前事实 | 拒绝；无取消、无新 gate、无新 phase |

终态只读没有本域匹配回执时不能凭“表为空”成功，不创建补偿回执；返回不匹配/不可用并保持 Project 查询边界。D05 的具体错误码沿既有固定 Fault 闭集，缺 Authority 返回 DEPENDENCY_UNBOUND。

#### 9.1.3 实际停止协议与生命周期

1. 初始 Project Begin 仍是正式 §8 的 EX 原子接受。预算内拿不到 EX 就是未接受/未提交，零 cancel；本口不绕过该屏障。
2. 接受后，D05 用第一只读短 Tx 在 Project SH 与本域完整锁 union 下验当前 cause，捕获确切本地 handle、真实 Project 关联、process/attempt/fence；只有该 Tx 确认 committed 后才在锁外取消。Unknown 不足以启动取消。
3. 取消只作用于仍等于捕获对象的原 handle；不能按项目名、后来扫描的宽范围或全局 Force 取消。Registry 的 Project/读写类别来自已验证 owner/source/transfer 映射；同一因果流程涉及目标写入与另一 Project source 时应分别登记实际关系，不能只靠一个可覆盖的 Project tag。
4. 实际等待位于 Tx 外。后续每个 checkpoint Tx 再独立一次完整 union，核 exact cause、原 writer Tx 终局、本地真实 join 或 exact ProcessGuard 死亡。停止请求超时、进程连接消失或取消返回均只算 pending。不能持 EX 等持 SH 的 callback join。
5. archive 撤销未完成 publish/attach 资格并持久保留同 attempt 的撤销事实；保存已发布 canonical payload/reference/history，允许合法只读下载。delete 还必须真实收束本 Project reader/source/transfer I/O；未打开但已持有的 lease/handle 也不能在 stop 后开启新 I/O。
6. Restore 只允许新 command/key；旧已撤销 upload/attempt/Artifact command 不复活。既有 reserve/send/publish/consume/attach 的真实状态检查继续生效，本轮不得通过 archive 物理清理 published 内容来“证明”停止。
7. Artifact 先处理自身 prospective/source 操作，再调用配置的 ObjectProjectStop；只有自己的真实工作与 Object 都 stopped 才返回 artifact stopped。缺任一真实 port 明确 unbound/pending，不能以 fake 生产 adapter 完成归档。
8. 本口不替代 Cleaner；删除清理顺序和最终 receipt/claim 原子退休完全沿正式 §8。只读终态分支不能获得 CleanupProject、新 Audit/Event 或新资源写权限。

### 9.2 当前权限、对象与 Audit 分派

D08 提供以下精确适配：Audit/Secret `AuthorizeProject` 与生命周期校验；Object `ProjectGate` 和 `CheckProjectCleanupInTx`；Artifact `Authority.Discover/DiscoverInTx/AuthorizeInTx/CheckProjectCleanupInTx`；Outbox `ProjectAuthority`、`ProducerAuthority`、`LifecycleActorResolver`。计划私有 issuer、完整 request binding、复制锁与锁后重读沿各现有 contract，不自建通用授权 bool。

Human Artifact 路径必须验证当前 User/Owner，Project SH 与资源实际父 ID 一致；Artifact/对象本域仍负责真实 artifact→object/receipt/lease 绑定。Avatar 继续交 account provider；Skill/Knowledge/Transcript 等未来 ObjectOwner 交对应领域，未绑定返回 unbound。Project 不能把“拥有项目”直接变成任意 ObjectID/历史 revision 的读取许可。AgentRun/Execution 来源适配留 D22，不能伪造执行身份。

resolver 前工作登记的窄口：D05 追加封闭 `OwnerAccess` 操作 `PrepareReadAccess`（wire `prepare_read`），只含 Actor、ObjectOwner、Intent=Read；不接受业务 key、Command、ObjectID、Source、Range 或其他变体字段。它用于已知 business_ref 对应 owner、尚未 Resolve 出真实 ObjectID 时的前置只读 work 规划。当前正式 AccessPlanner 预收集真实 Actor/owner 父 gate，初始一次完整 AcquireAll 后 Validate，并在同 Tx 完成当前 ResourceAuthority Read 与 ProjectGate Read 授权；持久 work 确认提交后才调用外部 resolver。该 variant 不授权限、不替代后续精确 source/version/object 校验，archive 合法当前读不被误当 Mutate；旧 Prepare/Lookup/ObjectRead 规则不放宽。精确追加范围为 `object/contract/access.go` 与新 `object/contract/project_prepare_read_test.go`；`object/access.go` 只在既有 D05 作者范围加入 SH/Read 分派。本段是 root 已授权的规格补口，D05 实现与真实验收尚未完成。

```go
type ProjectFactAuthority interface {
    CheckProjectAuditInTx(context.Context, foundation.Tx, Entry, AppendKey) error
}
```

该 interface 复用现有 typed Entry/AppendKey，只有这一方法，不接受原始 map、bool 或通用 cause “证明”。C0 不扩 Audit Action/Producer/schema，也不改已验 Audit Service。

Project Authority 增加只允许 SecretProducer/ObjectProducer 的配置映射并复制；仍先按 Actor/AccessIntent 检查当前 Project gate，然后按 producer 分派。Human 路径当前 Session/Owner 不省略；Service 路径必须通过选中 producer 的真实事实校验，不能仅凭 ServiceName+UUID。缺 provider 拒绝，交叉 producer/action/scope/不匹配 cause 拒绝。Project 自有 action 继续由 Project 当前 Tx 内真实 command/creation/lifecycle 行检查；Artifact Human typed Audit 沿其正式权限/本域映射，不偷归 Secret/Object provider。

#### 9.2.1 构造关系与同 Tx 事实

Object/Secret 分别新增独立 checker：

```go
// 位于各自 object / secret 包；返回值实现上面的 audit contract。
type ProjectAuditAuthority struct { /* private store */ }
func NewProjectAuditAuthority(Store) (*ProjectAuditAuthority, error)
func (*ProjectAuditAuthority) CheckProjectAuditInTx(
    context.Context, foundation.Tx, audit.Entry, audit.AppendKey,
) error
```

先用 Store 构造两个 checker，再构造 Project Authority → Audit Service → Object/Secret Service；checker 不能依赖后构造的业务 Service，也不使用运行时可变全局注册表。它用本域 Store.InTx 验同一活 Tx，不打开嵌套 Tx，不重新 Acquire。当前 Object Store 没有 RequireHeldLocks；不能为方便新增旧 Store 方法。锁/本域阶段证据应由既有 validated access 与私有同 Tx 见证配合实际行检查完成。

| 现有动作 | 固定代码事实与必须校验内容 |
| --- | --- |
| Secret create/update/delete | `secret/write.go` 已在同 Tx 写入 secret_command_receipts；核 receipt/command digest、credential、scope/purpose/result version、typed metadata/key，而非仅服务名称 |
| Secret resolve | `secret/lease.go` 的 resolution UUID 只在本次调用生成，没有单独持久 resolution 行；必须在已授权真实 lease/metadata/解密步骤之后、实际 Append 前生成包内私有见证，绑定活 Tx、完整 Entry/Key、lease/ref/purpose/consumer，并由 checker 重验真实 lease/metadata |
| Object upload complete/failed/delete | `object/upload.go` 的集中 append helper；complete 的 Audit 在 objects available UPDATE 前，delete 的 Audit 在 deleted UPDATE 前，不能要求不存在的后态。核真实 upload/attempt/cleanup cause、typed metadata、对应已检查的阶段前置；包内私有同 Tx 见证证明本次真实调用点 |
| Object transfer issue/complete/revoke | `object/transfer.go` 集中 helper，核真实 transfer 行、ordinal/action/phase/object/owner/typed metadata，配合相同 Tx 的实际阶段见证 |

见证不是公开 API：只可由本包真实持久/授权步骤后的 helper 创建；不得导出 mint/register/WithWitness 给 caller。见证由私有 context key 携带到当前 Append 调用，绑定同一 Tx 及完整 Entry/Key；checker 的 Store.InTx 必须证明 Tx 归属。不能跨 Tx、跨 Store、跨 action/ordinal/Entry/producer 重用；不含 secret 明文或对象 payload，不延长其生命，不新增 resolution 留存表。此 Secret 临时事实实现属于 D05 provider 后段；它不阻塞 stop C0 的独立纯契约或 Project 主体开工。没有真实领域步骤的外部直接 Audit.Append 必须失败。

**已采纳的工程澄清：**本节的原因事实指“本域真实事实；有持久 cause 的检查原行，现有短事务临时 Audit cause 以包内同 Tx 见证和真实 lease/阶段事实共同校验”。这是适配现有 Secret resolve/写前 Object Audit 的工程口径；不改变用户产品行为，不扩大服务权限或数据留存。此后段实现不得退为任意 UUID allow。

### 9.3 Outbox

Project 自身 producer 为 `project`；事件计划与 command/operation/creation 中真实 event identity/digest/version 匹配。Discover 只规划；ValidateAppendInTx 在 CurrentAccess 先验当前可见/内部 cause，在 NewFact 验 gate、原 command/phase、同 Tx canonical变更与完整计划。计划变更全回滚重规划，不在锁内二次 PrepareAppend。

DeliverProject 逐次校验真实 DeliveryIdentity、effect 与原 Project gate：active 合法；archiving/archived 只允许 canonical_converge；deleting 停止后不再执行业务回调。RequeueProject 只有当前 Owner，CurrentAccess 可读原完成结果，NewFact 必须 active；系统管理员无旁路。exact生命周期 stop/inspect 不依赖其他 participant 清理完成，cleanup 才核 §8 前置。

LifecycleActorResolver 只从当前持久 operation 或最小完成 receipt 构造原 ProjectLifecycle Actor。完成后的 resolver 只允许既有子域 terminal receipt检查，不能授权新 cleanup/append；不能信任请求里的 OperationID/Version 自己构造权限。

## 10. typed Event 与 Audit

事件 producer/aggregate_type 均 `project`，schema_version=1，Project scope；aggregate_id 为稳定 ProjectID，aggregate_version 为提交后的 Project.version。事件不含 name/description/path、Cookie/Secret、原错误、participant checkpoint。消费者 D25 重读当前 canonical/Owner，不以事件 payload 重建已删除 Project；乱序按版本保护，同版本不同 EventID 不丢 sibling。

| type | payload | 生产事务 |
| --- | --- | --- |
| `project.created` | owner_user_id、creation_id | Skill正式确认+initialized完成事务 |
| `project.updated` | changed_fields 闭集 name/description | Update事务；无字段实际变化则返回同version且不写新Event/Audit，但保存首次命令receipt |
| `project.lifecycle_changed` | operation_id?、from、to、action=`archive\|restore\|delete` | archive/delete接受、archive完成、restore事务；restore无operation_id，归档接受/完成共原operation |

永久删除最后不发 `project.deleted` 项目事件；接受 deleting 已有事件且最终最小 receipt 是状态查询事实。D25 后续必须以门禁/生命周期查询关闭订阅与清本域，不能依赖清理后再发一个会残留的 Project Event。operation progress 用查询投影，不每轮制造事件风暴。

Audit 新 producer `project`、资源 `project`/`project_operation`/`project_creation`。动作闭集：`project.create.accepted,project.create.completed,project.update,project.archive.accepted,project.archive.completed,project.restore,project.delete.accepted,project.lifecycle.retry`。Metadata 只包括相应稳定 project/operation/creation/initiator User IDs、project_version、适用时的operation_version或creation_version、changed_fields、from/to/action、固定 reason；不含描述、名称、确认路径、对象 payload、密钥或未知 map。

Human 操作验当前 Session/Owner；完成动作只给 exact ProjectInitialization/ProjectLifecycle cause 且读同 Tx 本域事实。普通 audit.authorizeHuman 的 Mutate 门禁不能阻止合法 Lifecycle action，因此新增 Project action 专属分派到真实 ProjectAuthority；不放宽旧 account/secret/object 动作。SQL CHECK 同时匹配 producer/action/resource/actor/outcome/metadata；迁移保留全部旧分支并验证 populated升级。没有 `project.delete.completed` Project Audit 或 System 副本；最终只保 §3 receipt。

## 11. HTTP、错误与正式装配

均 `/api/v1`；沿 D07 正式 Cookie/Origin/CSRF 边界，Actor 不来自 JSON。普通 body 上限 16KiB，strict JSON：未知/重复 key、尾随内容、null替代值拒绝。请求-ID由服务端生成；GET no-store。Project ID参数均 UUIDv7，不接受可读路径作为 mutation target。

| 方法/路径 | 请求 | 成功 |
| --- | --- | --- |
| POST `/projects` | 稳定project_id、name/description；Idempotency-Key；不接owner | 201 ready 或已持久接受202+creation_id/status_url |
| GET `/project-creations/{id}` | 当前本人 | 200安全creation/ready；failed如实标失败 |
| GET `/projects` | lifecycle、cursor、limit | 200本人Page |
| GET `/projects/{id}` | 当前Owner | 200 ProjectRef；deleting只通过下述status读 |
| GET `/projects/resolve` | username、project_name两个query参数 | 200 ProjectRef；旧路径/跨Owner404；路由注册须优先于{id} |
| PATCH `/projects/{id}` | expected_version+name?/description?；key | 200 ProjectRef |
| POST `/projects/{id}/archive` | expected_version；key | 202 operation/status_url |
| POST `/projects/{id}/restore` | expected_version；key | 200 ProjectRef |
| POST `/projects/{id}/delete` | expected_version、normalized_current_path、permanent:true；key | 202 operation 或原终态200最小receipt |
| GET `/projects/{id}/lifecycle-operations/{operation_id}` | 当前Owner/原Owner | 200 operation/最小receipt；主记录已删仍可查 |
| POST `/projects/{id}/lifecycle-operations/{operation_id}/retry` | operation expected_version；key | 202原operation；completed delete当前授权只读确认200最小receipt |

LookupCommand 由 same-key 重放和以上稳定 status URL提供HTTP语义，不开放任意 namespace/key 枚举入口。错误沿 D01；名称竞争 RESOURCE_BUSY + `/name:NAME_TAKEN`，版本陈旧 VERSION_CONFLICT，当前路径不匹配 CONFIRMATION_STALE，非法状态 INVALID_STATE，非active PROJECT_NOT_ACTIVE，未绑定/不可用503，Unknown503+lookup。不同用户请求不得通过名称冲突、receipt或operation泄露私有数据。永久删除时先当前Owner/Session，再原幂等/receipt，再版本与路径，不让攻击者通过错误顺序枚举。

组合根在已验 B04 assembly基础上注册 Project Authority，再把真实 Session/System/Project 分派给 Audit/Secret/Artifact/Object/Outbox。Object Avatar与Project owner dispatch共享文件由单一整合卡负责；拒绝未绑定的其他 owner。B04共享HTTP认证薄口若未稳定，D08 HTTP卡等待，不复制Cookie或CSRF私有实现。

Project worker使用独立 serving context；constructor无I/O，资源在 Initialize/Start前登记。复用共享启动30s、健康round2s/10s/20s陈旧规则及相同停机deadline/force1s，不每模块续预算。StopAdmission停止新Project命令，已经提交生命周期留checkpoint；Drain/Force按真实work/participant join判定。Project/初始化/参与者尚有活工作时共享ProcessGuard不能释放；DB最终ForceClose仍沿既有顺序真实调用。D10 initializer未绑定单独显示项目创建能力unbound，不让空数据库技术健康冒充产品ready；未知遗留creation/operation及缺required adapter必须明确诊断并保护门禁。

## 12. 验收矩阵

纯契约测试可立即运行；真实数据库/对象/进程组只有相应冻结依赖已验、迁移连续且root移交owned资源后运行。隔离fixture只在tests，生产无allow/empty/stub。

| 编号 | 必须观察的行为 |
| --- | --- |
| T01 类型/名称 | UUIDv7/正version/UTC微秒、名称1/64/65与ASCII大小写、`.`/`..`/编码斜杠/双解码/非法UTF8、描述边界；bootstrap admin合法路由；case-only rename版本/路由语义 |
| T02 Owner | 普通User、自己的admin、另一admin、被撤销/到期Session；读/改/归档/删除/receipt/对象下载/Outbox诊断全部无越权；无AgentRun provider明确unbound；构造Actor不代替数据库授权 |
| T03 名称/目标竞争 | 同Owner并发Create/Create、Rename/Rename、Create/Rename只一个占名；不同Owner同名成功；同target跨Owner只一人接受且失败不泄露；异key同target不能接管live/未决初始化；同raw key不同target按不同Project scope各自幂等；归档/失败/deleting占名，最终删除后新ID可用，旧worker不影响新项目 |
| T04 事务与幂等 | Project/Audit/Event/receipt/Touch全有或全无；同key并发、同key异义、换有效Session同User重放、旧version成功重放；Audit或Outbox失败整Tx回滚，无重复EventID |
| T05 初始化 | 无D10口零新生产成功/零新保留行，但当前授权下既有completed同义receipt仍能重放；fixture成功真实调用确认口才ready；pending/错误/Unknown/崩溃后同target/creation/Skill revision；错误Project/creation/issuer/缺锁/保护标志/对象不可引用拒绝；D10真实技能对象组合另列待集成 |
| T06 路径确认 | 同一User锁下username改名与resolve/delete竞争；项目rename与delete竞争；旧路径立即404/confirmation stale；old name重新占用后稳定ID+Owner+version防误删；Route口失败不退化account SQL |
| T07 cursor | 篡改/跨user/筛选/未知kid拒绝；limit变化可续页仍限1..100；同timestamp按ID稳定；每页撤权拒绝；不从当前页推全局状态 |
| T08 生命周期 | Archive/Delete/Restore/Retry互斥、旧version拒绝；接受EX超时真实not_committed且零operation/cancel；全部必需stop之前不archived、不cleaning；failed保gate/name/已完进度；Restore不重启旧执行/attempt/delivery；缺participant/版本不空成功 |
| T09 真停止 | 真实在途Object写/Outbox callback，持SH时Stop不与EX等待构成死锁；cancel不是join；旧archive捕获目标不能取消Restore后新handle；unknown/foreign-live/离线不成功，其他Project仍可推进 |
| T10 删除全程 | 真实已绑定Secret、Artifact/Object/MinIO、download、Outbox、Audit组合；先stop后按依赖释放/物理清理，活lease不假成功；外域只经端口；System Audit/其他Project不变；name只在最后receipt+主行删的同Tx释放 |
| T11 COMMIT unknown | create接受/初始化完成、update、archive/delete接受、progress、archive完成/restore、final receipt各覆盖晚COMMIT/ROLLBACK/锁未终局；确认55P03继续Unknown；不重复Project/operation/event/cleanup；original cause保持 |
| T12 恢复/公平 | 在接受后/取消后checkpoint前/外部完成后checkpoint前/最终提交后响应前SIGKILL+Wait；exact ProcessGuard死后恢复同工作；活实例/未知不能偷claim；前缀被保护不饿死后项；没有无界goroutine/阻塞join泄漏 |
| T13 最终回执 | 主Project已删后原Owner新Session能查completed/replay原delete；其他User/admin404；同key异义拒绝；原Create请求即使creation/key行已清，旧target对原Owner410/他人404且零新Project；回执无名称/正文/Audit副本/额外Create key；finalizer本次claim与receipt原子退休，其他活claim阻止final；旧phase新副作用拒绝，不通过恢复upsert重建 |
| T14 服务/Audit安全 | Project action闭集与typed metadata；ProjectLifecycle/Initialization假cause、错误participant/phase/ProjectVersion、未知producer拒绝；SQL旧Audit分支兼容，Secret/Object cause必须真provider；日志无路径/正文/token/checkpoint |
| T15 HTTP/app | strict DTO/OpenAPI、Origin/CSRF、no-store、status_url及真实202/失败/503；启动/取消/晚resource登记、共享预算、force不假join、guard和DB顺序；B04真实HTTP身份复用 |
| T16 将来集成 | D10真实Add Skills初始化/清理；D11工作、D19审批、D22执行/等待、D23调度、D24会议、D25订阅/投影各真实participant；最后D28完整required集合。未绑定保留待集成，不能用T08 fixture替代 |

实施后的预期命令（本设计阶段全部未执行）：

```sh
GOTOOLCHAIN=local /workspace/toolchains/go1.27.1/bin/go test -count=1 ./internal/central/project/...
GOTOOLCHAIN=local /workspace/toolchains/go1.27.1/bin/go test -race -count=1 ./internal/central/project/...
AGENTEAM_GO=/workspace/toolchains/go1.27.1/bin/go sh scripts/check-go.sh
AGENTEAM_GO=/workspace/toolchains/go1.27.1/bin/go AGENTEAM_MINIO_BINARY=/owned/cache/minio sh scripts/test-projects.sh
```

新脚本只组合既有owned fixture路径，并通过单一授权修改固定fixture包列表纳入`tests/project`。精确顶层测试集合、source/dependency/runtime manifest、逐组原超时、输入SHA及失败记录在作者交付时冻结；不把增长套件强塞单包预算后加时，不重复跑到绿。既有未变检查复用证据；对象停止口/迁移/Audit/装配变化运行各自受影响真实组合并独立验证。

## 13. 子任务、文件与共享资源所有权

| 卡 | 完整结果及依赖 | 写入范围/共享约束 | 验收范围 |
| --- | --- | --- | --- |
| S01 规格 | rev2规格、依赖清单、卡片已获独立静态通过并由root采纳 | 两份新D08文档由同一作者独占归位；其他正式文档不改 | 固定输入、链接/格式及定向静态审查已完成；不冒称独立业务验收 |
| B01 正式Project契约与规则库 | 新typed domain/请求/结果、Name与路径校验、gate闭集、生命周期/初始化消费口、typed event codecs；只依赖已验foundation/identity/event | 新`project/contract/{types,commands,lifecycle,initialization,events,validation}.go`及测试；不写旧identity枚举/迁移/app/Audit/DTO生产入口。可立即独立完成 | T01及闭集/绑定/安全编码纯测试、race/vet；不宣称存储或Project服务已验 |
| B02 存储与Owner/创建/元数据服务 | B01；00011已验、root分配连续新迁移；D03/D04/D06已验口；D10仍按正式port隔离 | 新`project/{repository,authority,service,commands,creation,events,audit_authority,recovery}.go`及私有helper、`tests/project`；新连续迁移；`identity/contract/identity.go`仅ProjectInitialization；`audit/contract/{project,types,metadata}.go`与`audit/service.go`窄增量。共享旧文件须等B03冻结验收解除/明确交接 | T02–T05/T07/T11对应真实PG/Audit/Outbox，initializer fixture及unbound生产负例；独立验收 |
| B03 生命周期与已有领域真实绑定 | B02；D07 CurrentUserRoutes已验（删除确认）；D05/06固定接口 | 新`project/{lifecycle,participants,object_authority,outbox_authority,secret_authority}.go`；D05新`object/contract/project_lifecycle.go`及Object/Artifact私有停止实现，Secret/Object新typed Audit cause验证；必要旧入口只按冻结精确清单；不得与B04对象旧文件并写 | T06/T08–T14，真实MinIO/PG/ProcessGuard/Outbox；已有模块完整archive/delete，未来participant仍明列 |
| B04 HTTP与Central装配 | B03与已验D07 B04 HTTP/app；相关所有权已移交 | 新`project/http.go`、`api/openapi/project.json`、`app/project.go`及app/resources/health/config等最小分派、tests/process；script/fixture包列表由本卡独占整合 | T15及真实权限/下载组合；不提前UI |
| I10/I28 集成责任 | D10技能初始化后、各未来模块启用后 | 各事实owner的adapter/正式注册与自己的任务卡 | T16；D08库完成和平台完整Project流程分开登记 |

B01 可与 B03 最终验证和 B04规格准备独立并行；B02 在迁移前置未验时可做有界编码准备，但不能把未验00011作为“已验依赖”或宣布真实PG结果卡完成。已验B01身份使B02不必等待SMTP/头像/正式HTTP；共享文件冻结和Docker独占另是实际排程约束。

B02/B03涉及共享Audit、identity、Object、Secret、app文件，root逐文件给单一作者或顺序交接，不能仅凭目录不同宣布独立。迁移号由root统一分配；Go mod/lock默认不变。Docker/测试库/MinIO/端口只给一个明确owned资源使用者；设计及纯测试无需占用。

本稿不存在需要重问用户的产品选择。初始化状态、原子确认、停止补口和事件清理顺序是落实已确认“不空成功/不误删/未知保持”的工程细化；若实施发现这些正式接口与其他owner冻结契约冲突，先报告root修订受影响规格，不能在下游自行造旁路。
