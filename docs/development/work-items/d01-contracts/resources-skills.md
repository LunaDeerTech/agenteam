# 资源、Agent 与技能端口

依据：[Agent](../../../architecture/agent-management.md)、[Skills](../../../architecture/agent-skills.md)、[Object Storage](../../../architecture/platform-infrastructure/object-storage.md)、[Artifact](../../../architecture/tool-system/artifact-tools.md)、[Knowledge](../../../architecture/knowledge-memory/knowledge-document-domain.md)、[Memory Runtime](../../../architecture/knowledge-memory/agent-memory-runtime.md)、[Retrieval](../../../architecture/knowledge-memory/retrieval-runtime.md)。端口均待[责任模块](README.md#实现与绑定责任目录)实现。

## 对象与业务引用

```text
ObjectOwner {kind: avatar|artifact|knowledge|skill_revision|mcp_content|
                   execution_payload|meeting_file, owner_id, project_id?}
ObjectMeta {id, scope, media_type, byte_size, sha256, state, created_at}
PutObject(ctx, actor, owner, meta, media_type, length, expected_sha256?, Reader)
  -> Result<ObjectMeta>
AttachObjectInTx(ctx, tx, actor, owner, object_id) -> Result<ObjectReference>
ReleaseObjectInTx(ctx, tx, actor, owner, object_id) -> error
ReadObject(ctx, actor, owner, object_id, byte_range?) -> Result<ObjectReader>
InspectReferences(ctx, object_id) -> Result<{references[], active_leases[]}>
ObjectCleanupCause {operation_id, owner: ObjectOwner, reason: string}
DeleteUnreferenced(ctx, cleanup_cause: ObjectCleanupCause, object_id) -> Result<ObjectCleanupResult>
ObjectCleanupResult {state: completed|pending|failed, remaining_refs[], fault?}
CreateArtifact(ctx, actor, meta, project_id, title, source: BusinessFileRef)
  -> Result<ArtifactRef>
BusinessFileRef = UploadedObject {object_id, upload_receipt}
                | ArtifactFile {artifact_id, file_id}
                | KnowledgeFile {document_id, content_version}
                | ExecutionFile {execution_id, payload_id}
```

D05 保存对象 metadata 与显式 owner reference；业务模块确认 source 实际归属及读取权限，不能仅凭 object_id 授权。`ObjectReader` 是服务内流，不是把字节全部加载内存的 DTO；`ReadObject` 可用 streaming body 或短期签名下载的受控内部适配，模型只获得业务 ref。平台文件引用均按当前项目及 Actor 校验；Avatar 无 project_id，按当前用户授权。

上传先形成 pending metadata，写对象并验证长度/digest，再事务发布 available/reference；对象存在而事务失败留可追踪 orphan 待清理，metadata 成功但 payload 不可读返回 `OBJECT_PAYLOAD_MISSING`，不伪造成功。外部 put/delete 与 DB 不能成为同一原子事务，恢复按原 object identity。引用释放本身不删除共享 payload；删除前再次锁 metadata/引用确认无 protected reference/lease。签名材料、MinIO key、Secret 不进入普通业务 DTO、ToolResult 或 Transcript。

```text
TransferGrant {transfer_id, runner_id, operation_id, object_id, direction: get|put,
               expires_at, length, sha256, private_transport_material}
IssueTransfer(ctx, trusted_actor, object_ref, runner_id, operation_id, direction)
  -> Result<TransferGrant>
InspectTransfer(ctx, trusted_actor, transfer_id) -> Result<TransferStatus>
TransferStatus = pending|complete|failed|unknown
```

D05/D17 负责单对象、单操作、短期授权与完整性；grant 只经受信任 Runner 通道，Runner 直连部署声明的受保护存储 endpoint。不可达明确失败，不自动 Central 中转。grant 过期只限制传输资格，不把业务等待或 Operation 自动设为过期。

## Agent 配置与真实目录

```text
AgentConfig {id, project_id, name, normalized_name, display_name?, tag_color?,
             description, instructions, inject_agents_md: bool,
             model_ref: ID<ModelConfig>, reasoning_effort?: string,
             allowed_tool_ids[], allowed_mount_ids[], allowed_secret_variable_ids[],
             approval_policy: default|auto|allow, approval_model_ref?,
             version, created_at, updated_at}
ReadAgent(ctx, actor, project_id, agent_id) -> Result<AgentConfig>
ValidateAgentRefsInTx(ctx, tx, actor, proposed_config) -> Result<ValidatedAgentRefs>
UpdateAgentInTx(ctx, tx, actor, meta, agent_id, allowed_patch) -> Result<AgentConfig>
```

Skill assignments 是 D10 独立引用集合，不将完整包、Tool schema、Secret 值复制进 AgentConfig。name 使用英文字母/数字/`-`、Owner scope 为 Project、标准 URL 小写；D10 固定长度/保留字和约束，User/Agent 名称 namespace 分开，参与者始终携带 kind。Agent 保存 model_ref/reasoning_effort，不提供另一套任意模型参数或 system prompt。

```text
ModelCatalog.ValidateSelectionInTx(ctx, tx, project_id, model_id, purpose,
                                   reasoning_effort?) -> Result<ModelSelectionInfo>
ToolCatalog.ResolveRefs(ctx, actor, project_id, tool_ids[]) -> Result<ToolRefInfo[]>
MountCatalog.ResolveRefsInTx(ctx, tx, actor, project_id, agent_id, mount_ids[])
  -> Result<MountRefInfo[]>
MCPConnectionCatalog.Read(ctx, actor, project_id, connection_id)
  -> Result<{config_id, connection_id, enabled, tool_ids[], version}>
SecretVariableCatalog.ValidateRefsInTx(ctx, tx, actor, project_id, variable_ids[])
  -> Result<SecretVariableRef[]>
```

目录返回每一请求 ID 的 typed `valid/removed/disabled/not_in_scope` 结论；连接/存储读取失败返回错误，不当成无效条目或自动跳过。Preset 复制只跳过已确认失效推荐，保留用户明确禁用；临时 Runner offline 不改变合法引用。新 MCP Tool 不自动加入能力。D09/D15/D18/D20/D04/D10 各自提供以上端口，组合根转换 DTO，不能由 Agent 直接查表。

Model 引用新增/替换共用 `system-config:model-references` 锁（新增 shared、删除/替换 exclusive），然后按 Project、Agent 等既定顺序取得锁并重读。`ReplaceModelReferencesInTx(ctx, tx, system_or_project_actor, old_id, replacement_id, affected_versions)` 返回替换计数/安全 ID 结果；D09 通过 Agent/Project/平台 selector 正式端口完成全体校验和写入，任何不兼容全回滚。System chat 的替代必须是所有受影响项目可见的 enabled System chat；受控跨 Owner 配置替换不授予正文读取权。运行 Credential 用 D04 lease，live 配置删除不回收已获 lease 的材料。

Project variables 正常值和 Secret 引用分离：`ResolveEnvironment(ctx, AgentRun, variable_refs, mount_id, operation_id) -> Result<TrustedEnvironment>` 只供命令适配器在执行前读取；普通值可按既定 Context 投影，Secret 明文仅在已授权 Backend 请求生命周期内出现。MCP credential binding 属于 Project Connection，不属于系统 Config；共享变量删除须引用检查/替换，不能使无关 Connection 失去凭据。

## Skill 类型与统一安装

```text
Skill {id, project_id, name, normalized_name, description, protected: bool,
       current_revision: Revision, version: Version, deleted_at?}
SkillRevision {skill_id, revision, name, description, package_object_id, package_sha256,
               manifest: SkillFile[], entry_path: "SKILL.md", published_at,
               source: {kind, safe_label?, source_digest?}}
SkillFile {path: string, media_type, byte_size, sha256}
SkillBinding {skill_id, revision, assignment_id, assignment_sequence,
              name, description, package_sha256, entry_path}
SkillAssignment {id, project_id, agent_id, skill_id, enabled,
                 assignment_sequence, created_at, removed_at?}
PackageSource = ControlledFile {file: BusinessFileRef}
              | RunnerPackage {mount_id, workspace_relative_path}
              | TextFiles {files: [{path, utf8_text}]}
InstallSkill(ctx, actor, meta, project_id, source: PackageSource,
             mode: create | update {skill_id, expected_version})
  -> Result<{skill_id, revision, version}>
InitializeProjectSkills(ctx, service_actor, project_id, initialization_key)
  -> Result<{add_skills_id, revision}>
```

`TextFiles` 是无 Runner 的标准包输入路径，不执行脚本；服务端使用和受控上传相同的验证/打包/发布路径。D10 在开工规格固定首期压缩容器、frontmatter 子集及数量/体积/处理时间限制，并为所有输入使用相同验证器；D01 不授权任意第三方格式自动适配。路径采用 `/` 分隔的包内相对路径，拒绝空段、`.`/`..`、绝对路径、反斜杠歧义、Unicode/大小写规范化碰撞、links/设备文件/重复项，根入口必须恰为 `SKILL.md`。

Install 接受受控材料后，以原 key 建立可恢复发布记录；完整验证→D05 put→事务写 immutable revision/current pointer/引用/幂等结果。create 同名冲突；update 必须显式稳定 skill_id/expected_version，不能凭包内名称覆盖。失败保留原 current revision；对象成功不等于安装成功；重试/崩溃查询原发布记录，不重复 revision。发布不自动分配，安装成功而分配失败分别反馈。受保护 Add Skills 由初始化 key 唯一化，普通安装不得覆盖或单独删除；D08 创建 Project 必须等待 D10 正式初始化成功，不能用空 fixture 在生产完成。

## 分配、目录与读取

```text
AssignSkillInTx(ctx, tx, actor, meta, project_id, target_agent_id,
                skill_id, enabled: bool) -> Result<AssignmentMutation>
RemoveSkillInTx(ctx, tx, actor, meta, project_id, target_agent_id, assignment_id)
  -> Result<AssignmentMutation>
AssignmentMutation {agent_version, assignment_sequence, assignment?, change_id}
ListSkillCatalog(ctx, actor, project_id,
                 view: own_assignments|management, page) -> Result<Page<SkillCard>>
SkillCard {skill_id, name, description, current_revision}
ResolveInitialBindingsInTx(ctx, tx, project_id, agent_id)
  -> Result<{assignment_sequence, bindings: SkillBinding[]}>
ValidateAssignmentInTx(ctx, tx, project_id, agent_id, assignment_id,
                      skill_id, bound_revision) -> Result<valid|removed|disabled|deleted>
SkillExecutionBindings.Resolve(ctx, project_id, agent_id, execution_id,
                               input_binding_id, skill_id)
  -> Result<{assignment_id, bound_revision, round_id}>
ReadSkill(ctx, AgentRun, skill_id, relative_path, input_binding_id)
  -> Result<SkillContent>
SkillContent = text {skill_id, revision, path, text, truncated: bool, file_ref?}
             | file {skill_id, revision, path, file_ref}
```

Assign/Remove 锁 Project gate、目标 Agent gate，expected_version 是目标 Agent.config_version；先同 key replay，再版本校验；Assignment sequence 按 Agent 单调递增，每次新增分配有新 assignment_id。Agent 配置分配保存稳定 Skill 引用，不永久钉死包版本。分配时仅为运行中新增 change 固定此刻 current revision；新 Execution 的 ResolveInitialBindingsInTx 在 preparing 捕获当前合法分配所指 Skill 的当前 revision。已运行 Execution 的初始/新增 binding 不随库更新漂移。普通重复勾选已有 enabled assignment 返回当前分配，不构造无意义的新授权；Remove 后重新新增产生新 assignment identity，因此迟到旧 change 不能当成当前授权。

ValidateAssignment 仅验证 assignment identity 当前仍有效、Agent/Skill 同 Project、资源仍 active、bound_revision 确实属于此 Skill；不依赖 Execution 已有正式 binding，也不要求 bound_revision 等于 Skill.current_revision。Executor 另外验证自己的既有 binding 或 durable pending change 的 captured identity/受保护 revision 引用，再决定创建下一轮 binding；不能用“先已有 binding”作为新增 binding 的校验前提。库显式更新后，新 Execution 使用新版，旧 Execution 仍合法读取固定旧版；取消分配或删除资源才撤销后续读取/准备授权。revision 捕获与库发布通过同 Skill aggregate lock 排序，不能拿 metadata 的新版名称却读取旧版包；catalog metadata 随选中的 immutable revision 固定。

Owner 可管理本项目。Agent management view/分配写只在当前真实 Tool/Policy 具有管理权限时可用，可给同项目其他 Agent 分配且不要求调用者先拥有 Skill；management 只返回名称简介，不扩正文权限。`own_assignments` 由真实 AgentRun 绑定，不接收伪造目标身份。ReadSkill 先通过 skill contract 定义、D22 实现的 SkillExecutionBindings 窄查询核实本轮 input_binding_id 与真实 Execution，再检查当前 assignment/resource；不能仅按 skill_id 自动取最新，也不能由客户端传固定 revision 越权。该端口不返回 Executor 内部对象，缺失或失败明确拒绝。二进制/过大结果走业务 file ref，不将签名材料塞进模型。

分配事务通过 D10 的 `AssignmentRuntimeSink.RecordInTx(ctx, tx, change)` 窄出口交给 D22 `SkillAssignmentIngress`，记录新增变更与目标 active Execution，不能靠异步通知承诺下一轮。没有 active Execution 时也需可靠返回 `no_active_execution`，该结论由 D22 持同 Agent gate 查询。返回 error 整个分配事务回滚，返回提交 unknown 用原 key 查；未绑定端口不能默认为“没有活动执行”。启动/输入边界算法见[执行与编排](execution-orchestration.md#技能变更与下一轮输入确定点)。

## Runner 准备与删除保留

```text
PrepareSkill(ctx, AgentRun, meta, skill_id, input_binding_id, mount_id)
  -> Result<SkillPreparation>
SkillPreparation {skill_id, revision, mount_id, relative_directory,
                  manifest_digest, state: ready|pending|failed|unknown}
```

D21 工具适配先重验资源/分配/固定 revision、Mount 与 Execution Policy，再由 D17 签发对象传输，Runner D16 在平台托管版本目录临时下载、验证整个 manifest 后原子发布。返回完整包的工作区相对目录，不接受模型指定任意宿主路径。重复 `(mount_id, skill_id, revision, package_sha256)` 只有重新验证存在且完整才可复用。取消/断连/磁盘不足不能暴露半包 ready；未安装解释器返回真实失败，不自动装依赖。运行脚本仍用已有命令工具与审批。

移除/禁用只改变配置，不取消在跑脚本、清除已读内容或热改执行快照；下一次读取/准备校验当前授权失败，作为普通 ToolError。Skill 库删除 tombstone 当前资源，保留 immutable revisions 及对象引用以保护活动/历史 Execution；没有引用的 revision payload 可经明确清理删除，保留必要 metadata 与 digest 以解释历史，不能以无 payload 假装仍能读。缓存只清平台确认归属且无 active usage 的目录。Project 永久删除经正式 participant 清库、所有 revision 引用及托管文件，Runner unknown 清理不得成功。

## Knowledge 树与内容端口

```text
DocumentRef {id, project_id, parent_document_id?, title, content_version,
             source_kind: text|file, media_type, object_id, status: active|deleted}
ReadDocument(ctx, actor, project_id, document_id) -> Result<DocumentContent>
ListChildren(ctx, actor, project_id, parent_document_id?, title_query?, page)
  -> Result<Page<DocumentRef>>
ReadAncestors(ctx, actor, project_id, document_id) -> Result<DocumentRef[]>
MoveDocumentInTx(ctx, tx, actor, meta, document_id,
                 expected_parent_id?, target_parent_id?) -> Result<DocumentRef>
PrepareDeleteSubtree(ctx, Human, project_id, root_id)
  -> Result<{nodes: DocumentRef[], scope_digest, confirmation_token}>
DeleteSubtreeInTx(ctx, tx, Human, meta, root_id, confirmation_token)
  -> Result<{deleted_ids[], cleanup_pending: bool}>
```

D12 树只有当前 parent，无结构版本/结构历史/手工排序。Move 带显式 nullable `expected_parent_id` 前置条件并锁 `knowledge-tree:project_id`，重新校验同 Project active parent、防自环/后代环；只更新 parent，不改内容 version、不触发索引。正文更新不回写 parent。所有 create/move/delete 共用树锁，内容 update 同时取得该锁以保护删除确认范围；D12 可细化并发算法，但不能削弱断言。

删除确认 token 绑定 Actor/Project/root 和按 ID 排序的当前子树 `(id,parent_id,content_version,status)` 摘要；签名由服务端产生，携带明确失效时间只用于确认有效性。提交重算全范围，任何节点/归属/内容变化返回 `CONFIRMATION_STALE`，不静默扩大/部分删除。所有 tombstone 与 serving invalidation input/Outbox 同事务，payload/索引清理在提交后；Meeting 历史引用保留身份但不能读已删正文。

## 检索与 Memory

```text
RetrievalScope = Knowledge {project_id} | Memory {project_id, agent_id}
ServingRef {profile_id, generation_id, embedding_snapshot_id}
Retrieve(ctx, actor, scope, query, filters, limits) -> Result<RetrievalResult>
RetrievalResult {serving: ServingRef, items: [{source_id, source_revision,
                 snippet, score, provenance}], degradation?: string}
EnqueueIndexInTx(ctx, tx, source_ref, source_revision, command_key) -> error
InvalidateServingInTx(ctx, tx, source_ref, source_revision?) -> error
```

scope/filter 在召回前检查，不能先跨项目/Agent 召回再仅隐藏 UI。读取实际 serving generation 的 embedding snapshot；文档内容更新立即使旧 version 不可 serving，新版 ready 前可暂时查不到，不能返回旧 snippet。IndexProfile-only rebuild 可继续旧 serving generation，原子切换前验证完整性；查询参数与 index profile 参数由 D13 区分，管理员默认/上限不授予项目正文访问。D13 负责真实词法选型/评测；接口不假装某个库已支持中文/并发 rebuild。

```text
MemoryInput {memory_id, revision, status: active|deleted, text, tags[], provenance}
MemoryProposal {pass: 0|1, input_digest, input_refs: [{memory_id, revision}],
                actions: (Add {text,tags,provenance} |
                          Update {memory_id,expected_revision,text,tags,provenance} |
                          Delete {memory_id,expected_revision} | Noop)[]}
InspectSensitive(ctx, AgentRun, input) -> Result<allow|mask {safe_input}|reject {code}>
ReadConsolidationInputs(ctx, AgentRun, query) -> Result<MemoryInput[]>
CommitMemoryBatchInTx(ctx, tx, AgentRun, retain_operation_id, proposal)
  -> Result<{committed_revisions[], index_work_ids[]}>
LookupRetain(ctx, AgentRun, retain_operation_id, original_input_digest)
  -> Result<committed {result}|in_progress|not_observed>
```

D14 在任何 Memory Model/embedding/lexical 输入前执行 allow/mask/reject；tags/provenance 同样过滤，不另存长期完整 raw retain。namespace 强绑真实 AgentRun。proposal 只能 UPDATE/DELETE 本次服务端输入集中当前仍 active 的目标，schema/归属/状态/revision 任一失败整批回滚；模型输出只是 proposal。

明确 `MEMORY_INPUT_CONFLICT` 且 `commit_state=not_committed` 时重读合法当前数据并重新 consolidation 一次，使用 pass=1，保留 input_digest/Invocation evidence；再冲突返回普通 ToolError。取消/权限/生命周期失效不走此重算。原 retain operation/key/原输入摘要保持不变，内部 proposal attempt 按 `(retain_operation_id, pass, input_digest)` 留证，避免“同键不同 proposal”被错误当外部 key 重用。

成功批次、幂等结果、canonical revision 与索引更新意图同事务；提交 unknown 首先查原 retain，不重读重算第二批、不当回滚。索引刷新只能由 D13 正式端口投递，错误不静默吞掉。Memory 无自动 TTL；reflect 只返回推理结果，不隐式写长期 Memory，单次工具失败不直接终止整个 Execution。
