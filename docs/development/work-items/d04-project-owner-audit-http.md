# D04：Project Owner Audit 列表与详情读取 HTTP

持久归档（2026-10-07）：rev2完整规格及正式末件STATIC接受已提交 `9eb167e4` 并推送核远端一致；[三轮独审、B1修订与静态证据](../agent-team/project-owner-audit-http-spec-verification.md)已归档。以下§1–8技术原字节保持，仍仅规格接受，实施须满足既定配置完整接受与根移交门槛。

修订：rev2；状态：完整规格独立 STATIC 已通过（rev1 完整审＋rev2 B1 差量），root 已采纳，现正式归位；**尚未授权本卡14技术实施、2文档末件或任何 Go／资源执行**。固定产品基线 `e4b1b89197da0e9027018fdb41f6ab3e04050b9d`（Project 模型凭据完整接受），固定来源与验收指纹见 [§9](#9-固定来源与规格验收定位)。Project 模型配置写入 rev2 完整产品接受后，须由 root 正式移交 account.go 与共同冻结输入再下发实施；这是调度与共享编译输入门槛，不是 Audit 查询功能缺失。

## 1. 完整结果、既定范围与依赖

交付当前 Project Owner 从默认 Central 根通过两条 GET／HEAD 路由读取所属 Project 的统一 Audit 列表和详情；当前 Session／Owner／Project Read gate 与实际有界 SQL 观察处于同一事务。返回既有类型化安全事实，不返回业务正文、不新增 Audit 写入、生命周期、清理、导出、全文搜索、前端或管理员越权读取。库新能力、HTTP 闭集、默认根、真实身份／PG／native 与旧兼容一起验收；单独新增路由不算完整结果。

本范围已经由 [Audit 业务规则](../../architecture/security-governance/audit.md) §3.1、§14–17、§20 确定：当前 Owner 可见本 Project 统一 Audit，Agent 入口只是过滤视图，不另建表；列表显示时间／执行主体／操作／资源／结果／安全摘要，详情显示类型化 metadata 和关联 ID；排序 `(created_at DESC,id DESC)`，不是正文全文检索。§2、§9 禁止完整参数、命令正文、stdout/stderr、Tool 结果、模型提示词／响应、Secret 明文；§13 保留归档 Project Audit，删除后的既有清理责任不变。没有新增自动过期或保留期配置。系统管理员没有因此取得别人的 Project Audit 权限；普通 Agent Tool 和 Service 不成为本 HTTP 查询主体。无需重新询问 Summary 模型归属：系统管理员统一设置（含初始标题）已决。

已具备：Audit typed Entry／AppendInTx／扫描与游标，Account 正式 Session，Project 同 Tx Owner authority／Read gate，同 Store 的根 Audit、真实 Secret／Model／Project producer，System Audit 已验 HTTP、原完整 20 包 fixture。尚需本卡实现：公开 Project 同事务窄 facade、完整 Project 安全 DTO／schema、两路由与根绑定及本卡验收。旧 `audit.Service.List/Get` 先 Tx 外授权的实现不能冒充新 facade；System 专用窄投影也不覆盖 Project 的 31 action。

配置写入完成后只消费其最终接受快照／实际图与已有根行为；不读取其活动 #1–14 作稳定输入。`app/account.go` 唯一写者由 root 正式移交；共享 model/app/tests/model 编译闭包冻结后才 Go 检查，真实资源始终独占。新 facade／wire 可在相应稳定依赖冻结后先离线交付，但整卡根验收不能绕过上述调度。无迁移、全局编号、依赖升级、旧公开 API 签名或旧游标协议改动。

Object runtime join、OpenAI tools 独立验收、SPA 并发发布三停止保持；生产 Resolution／Invocations、D24 真实 Meeting／Provider 调用、Project 创建 HTTP／Skills 初始化与生命周期根等未绑定边界保持。结构上能读 Object／Artifact／Knowledge 历史合法事件，不等于启动这些未绑定运行链。D04／D08／D09 整体及 E01 不因本卡完成而完成；默认 readyz 仍按既有未绑定事实工作。

## 2. 新公开窄服务能力及事务边界

在 `internal/central/audit/project_query.go` 增加以下 **公开窄服务能力**；它们不是私有实现接缝。`contract.Reader`、原 List/Get/ListSystem/GetSystem、原 System Unknown 表现均不改。`c` 为 Audit contract，`identity` 为 identity contract。

```go
func (s *Service) ListProject(ctx context.Context, actor identity.Actor,
    projectID identity.ProjectID, filter c.Filter, page foundation.PageRequest,
) (foundation.Page[c.SafeRecord], error)
func (s *Service) GetProject(ctx context.Context, actor identity.Actor,
    projectID identity.ProjectID, auditID c.ID,
) (c.SafeRecord, error)
func ProjectReadUnknownAttempt(err error) (foundation.CommitResult, bool)
```

内部事务／投影检查是私有 helper，不新增 Store、authority 或通用 Reader 端口。nil ctx／非法 ProjectID 为 InvalidArgument；无效／非 Human actor 为 Forbidden；nil Service／Store／Sessions／Projects 为 DependencyUnbound。新 facade 自入口派生最多 3s 的子 ctx，继承更早 parent deadline；不 WithoutCancel、不重试、不开另一确认事务。以新的 UUID read-run 标识和 `NewRecoveryCause("audit.project-read", runID, "")` 使用现有 `Store.WithinTx`。不创建 Command、receipt、幂等 key 或 Audit 查询事件。

同一 callback、同一实际 Tx 顺序：检查 ctx → User SH → Project SH → `authorizeHuman(ctx,tx,actor,projectScope,Read)`（真实当前 Session 与 `Projects.AuthorizeProject`、grant binding）→ `Store.InTx(tx)` → 有界 Get/List → 实际 Rows.Close 后 Rows.Err → 检查 ctx → 标记 callback 完整。正式 Project authority 要求 User／Project 已持锁并重新读取 Owner／gate，不接 caller boolean、旧 grant 或缓存 Owner。User 在 Project 之前，不能 Tx 外预检查代替。Read 允许 initialized 的 active／archiving／archived，拒绝 initializing／deleting；未知 Project 与非 Owner 按原 Project 隐匿规则 NotFound，合法 Session 的管理员亦不例外。Session 无效按原 AuthenticationRequired。锁与权限错误保持既定 Fault 映射，不为测试改成统一 403／503。

复用旧 `getRecord/listRecords` 的 SQL、`scanRecord`、filterDigest、Keyring 和非 nil check 回调；扫描本身以真实完整 Actor 重建 `NewEntry`，新 check 至少验证 Project scope、Get ID、页内严格降序／无重复和请求过滤绑定，包含额外哨兵。不能仅给旧 List/Get 外包一层 ctx。Get 的 SQL 必须仍是 scope+scope_key+id 隔离，再核返回 Project；List 还保留 project_id 隔离。UUID 列与 text scope_key 使用既有独立参数，不混用一个未定类型参数。所有匹配字段都基于完整 typed row；损坏 scope／actor／metadata／resource／关联、过量行或 Close 后 Err 均整次零候选，不能跳过坏行或截取前 limit 掩盖坏哨兵。

只有 `WithinTx` 实际返回 Committed、callback 完整且子 ctx 尚未取消／过期，才发布候选及 cursor。NotCommitted 返回原 result.Fault（nil fault 用 InternalError/NotCommitted）；Committed 但晚取消／缺 callback 完整则零候选 DependencyUnavailable，不伪造原事务 NotCommitted。Unknown 必须零 record／零 items／零 cursor，并以新私有 error wrapper 保存 **原 CommitResult 的 State、Cause、AttemptID**：公开 `ProjectReadUnknownAttempt` 可通过 errors.As 跨包装取回原值；其 foundation Fault 为 CommitUnknown/Unknown，原有效 AttemptID 作为内部 CauseID，安全 RetryHint 沿既有 lookup；UnknownAttempt 仅对本新 wrapper 返回 true，其他错误返回零值/false。不得用 `portError` 吞掉终局分类，不得把 cause/attempt 换成新事务或请求 ID。

HTTP 仍使用既有 Problem 的 `code/commit_state/request_id/retry_hint`；没有 `cause_id` JSON／响应头，也不公开原事务 cause/attempt。既有 CommitUnknown 公共 hint 不代表本卡提供查证端点；本卡无自动 lookup／私自重跑读事务。保留原 System read 的已接受表现，不借本卡修其未保留原 attempt 的差异。

## 3. HTTP 路由、过滤、分页与安全 DTO

公开构造和路由所有权如下，构造拒绝 nil，纯构造不 I/O。内部可有私有 reader interface 供受控测试，默认根只传真实 `*audit.Service`。

```go
func NewProjectHTTPHandler(auditor *audit.Service, boundary *account.HTTPBoundary) (http.Handler, error)
func HandlesProjectHTTPPath(path string) bool
```

包为 `internal/central/audit/http`（audithttp）。路径仅 `/api/v1/projects/{project_id}/audit` 与 `/api/v1/projects/{project_id}/audit/{audit_id}`，只 GET／HEAD；其他 method 返回 405，Allow 精确 `GET, HEAD`。拥有该 audit 子树的异常尾部在既有 Account CheckRequest 后返回 404；近似前缀继续原 fallback，不能拦 `/usage`、`/models`、凭据／配置写入、Project 更新或 System Audit。原 Account pathClean 会先拒绝 trailing slash／不规范 RawPath 为 400，不把它误测成未知 canonical 路由 404，也不增加 redirect。

入口先安装 §5 实际 I/O／cleanup 所有权，再沿一次原 RequestID／Recover／AccessLog 链执行 `CheckRequest` → route/method → `RequireHuman` → canonical UUIDv7 path ID／query／空 Body → 唯一 facade。不得提前用查询参数判断存在性绕当前身份。GET／HEAD 请求 Body 必须实际 EOF；非空拒绝，不忽略 ContentLength=0 的真实字节，阻塞 Body 受 3s／更早 parent 和实际 Close 管理。成功 Content-Type 为 application/json、Cache-Control:no-store，Account 安全头沿原边界，实际 Content-Length 等于完整编码字节。HEAD 完成相同授权／SQL／完整验证／编码，成功 Content-Length 等于 GET 字节，所有成功和错误不写实体；不把 HEAD 变成跳过 DB 的快速路径。

列表复用固定 `query` 的 canonical-v1 语义：RawQuery≤32KiB；拒绝裸 `?`、解析错误、未知键、重复键（含重复空值）；允许的 16 键恰为 `limit,cursor,from,to,actor_kind,actor_id,action,outcome,resource_kind,resource_id,tool_id,execution_id,operation_id,approval_id,runner_id,agent_id`。limit 缺省50，规范十进制1–200（非0、负数、+、前导0、指数），空 limit 非法；空可选 filter／cursor 沿旧“未过滤／首页”含义，from/to 非空必须标准 Instant，from<to，[from,to)。详情不接受任何 query。actor_kind 为 human/agent_run/service，service 不配 actor_id；ID 均 canonical UUIDv7；agent_id 为 AgentRun actor 或 agent resource 的既有 OR 条件。

**filter 合法集合不等于结果集合**：action 接受固定通用 Action.Valid 的全部 53 值，resource_kind 同通用合法集合；合法 System-only action（例如 account.login 或 model.selection.update）在 Project 查询返回空页，不应 400；未注册字符串才 400。输出仅下述 31 action/14 resource。不得从未来写入端是否已绑定反推删掉已有类型合法历史。

cursor 使用旧签名协议、Project scope+canonical filter digest+AuditOrder，排序二元 `(created_at,id)` DESC；页大小刻意不参与 digest，改变 limit 仍可续页。scope/System/其他 Project、任一 filter／排序、签名／key／shape改变均 CursorInvalid。raw token≤8192B，签名后 token 只允许既有安全 ASCII 表示。查询 limit+1，**第201行也完整扫描／验证／Close 后 Err**，只有前200参与响应，next_cursor 由最后返回行产生；末页为 null，空 items 为 []；HTTP也拒绝多于请求limit、少于limit却有next_cursor、空页有cursor、重复／乱序／跨过滤候选；不得分页重复／丢同时间 ID、把不存在的哨兵称更多页。当前授权必须在 cursor 及资源存在观察之前重新确认。

record 精确11个必有键：`audit_id,created_at,scope,project_id,actor,action,outcome,resource,metadata,associations,summary`，scope 为字面 project，project_id 等于路径。顶层详情为该 record；列表恰 `{items:[],next_cursor:null|string}`。不直接 JSON marshal SafeRecord／opaque Metadata／Entry。scalar 使用 common 的 UUIDv7、微秒 UTC Instant、规范十进制字符串：Version≥1／Progress≥0，上界 MaxInt64，无 JSON number 精度损失；Action、Outcome、所有 enum 闭集。未知／null／多余／跨分支属性拒绝；optional 只有缺省，不能以显式空／null 绕过。日期合法范围沿 foundation 与 common 的0000–9999年，不能用只支持1–9999年的测试运行库收窄；format检查按该日历语义扩展日期checker，生产仍用既有ParseInstant。schema 必须标准 draft2020-12／OpenAPI3.1、additionalProperties:false、action 分支 oneOf 和 required/not 条件，普通通用 Problem 只引用原 common，不改公共错误契约。

Actor 三个严格分支：Human 恰 `{kind:"human",id:<UserID>}`，没有 Session；AgentRun 恰 `{kind:"agent_run",id:<AgentID>,project_id,execution_id}`，两 Project 相等、associations.execution_id 存在且相等；Service 恰 `{kind:"service",service,cause_ref,project_id}`，Project 相等、cause_ref 为 canonical UUIDv7 或 sha256 digest。service 为固定13项 `secret,secret-maintenance,outbound,object,object-maintenance,project-lifecycle,outbox-delivery,account-bootstrap,account-auth,account-maintenance,account-mail,model-runtime,project-initialization`，还受 action 约束；其他字段不得保留。查询者 Human 与历史 Actor 三分支不同，不用假 Session 重建 Human Entry。

Resource 输出闭集14项：`secret,outbound_policy,agent,stored_object,object_transfer,artifact,artifact_collection,outbox_delivery,project,project_operation,project_creation,model_provider,model_config,knowledge_document`；outbound_policy 恰 `{kind}` 无 id，其余 canonical id 必有，并满足 action binding。associations 恰以下9个可选 UUIDv7：tool_id/execution_id/tool_call_id/operation_id/request_id/approval_id/runner_id/correlation_id/http_trace_id，另受 action 子集限制，不公开旧 Session、command key、任意 HTTP header、URL、材料或正文。

summary 只能由 action 重新选择既有安全常量：secret.create→`Secret created`，secret.update→`Secret updated`，secret.delete→`Secret deleted`，secret.resolve→`Secret use recorded`，outbound.access.deny→`Outbound access denied`，其余26→`Audit event`。候选 Summary 必须与该常量一致，否则 DependencyUnavailable；不透传用户 name/description、模型值、Secret摘要／哈希、SQL、错误字符串或全文。损坏上游 SafeRecord 即使 err=nil 也必须零候选，不能以过滤／晚查询覆盖。

## 4. Project 合法 typed metadata 全分支与最大页

以下31 action 来自固定 NewEntry／Metadata 构造器，HTTP 与 schema 必须覆盖全部，不复制 System 缩减表。先用 Metadata.Validate(action)、canonical JSON→DecodeMetadata→canonical JSON 验证，按动作检查 Actor／resource／scope／关联关系；不因 Metadata 是 opaque 就信任外部可变 SafeRecord 的其他字段。所有 metadata 对象拒绝未列属性、null、重复键／尾部 JSON；canonical输出≤4096B。Knowledge 工厂没有显式4096检查，但其5个定长 scalar 远小于该界；此新投影上限不拒绝任何既有合法 Knowledge 值。

|组／精确 action（数量）|metadata 形状与关键约束|Actor／resource／outcome／关联|
|---|---|---|
|secret.create / secret.update / secret.delete（3）|version≥1；前两者 changed_fields 为 purpose/value 的非空排序去重集合；delete 禁 changed_fields|secret ID；既有 Human/AgentRun/Service、4种 Outcome 均按原合法结构保留；一般9关联及 Agent execution 规则|
|secret.resolve（1）|lease_id；consumer 固定6项 model/mcp/runner/smtp/object_storage/system；reason 可选且仅原27枚举|secret ID；Actor／Outcome／一般关联同上；无材料、lease capability 或解密值|
|outbound.access.deny（1）|consumer、reason（必有）、version；reason 闭集同上|Denied；outbound_policy 无ID或 agent/secret 有ID；三 Actor 及一般关联仍按原契约|
|object.upload.complete / object.upload.failed / object.delete / object.transfer.issue / object.transfer.complete / object.transfer.revoke（6）|object_id、initiator_kind(Human/AgentRun/Service)、initiator_id、media_type、byte_size/sent_bytes；AgentRun initiator 才且必须 initiator_execution_id；后三者且仅后三者有 transfer_id；phase 依次 published/failed/deleted/issued/sent/revoked；upload.failed 必有 reason，revoke 可选，其余禁；sent≤byte，transfer.complete 必相等|Service object/object-maintenance，Project一致；前三resource stored_object.id=object_id，后三object_transfer.id=transfer_id；upload.failed Failed或Unknown，其余Success；一般9关联。upload.complete 不新增 sent==byte 的旧契约外限制|
|artifact.create / artifact.read / artifact.download / artifact.list（4）|list 恰 phase=listed/count≥0；其余 artifact_id/object_id/media_type/byte_size/sent_bytes/phase，sent≤byte；create phase=published/sent=0、source_kind必有；read phase=read；download phase issued/started/sent/failed，reason 当且仅当 failed；read/download source_kind 可缺；空/inline 禁source_id/revision，uploaded_object/artifact_file/knowledge_file/execution_file 必source_id，可有revision≥1|Human/AgentRun；前三artifact.id=artifact_id，list artifact_collection.id=ProjectID；download failed为Failed，其余Success；一般9关联及 Agent execution。不得新加download sent==byte限制|
|outbox.delivery.requeue（1）|delivery_id/event_id；handler_id≤128稳定 ASCII [a-z][a-z0-9_.-]*；from_state failed/dead_letter；redrive_cycle≥1；reason_code operator_retry/schema_available/dependency_restored|Human/Success/outbox_delivery.id=delivery_id；一般关联，不强加原契约未禁止的关联限制|
|project.create.accepted / project.create.completed / project.update / project.archive.accepted / project.archive.completed / project.restore / project.delete.accepted / project.lifecycle.retry（8）|共同 project_id/initiator_id/project_version；creation两种且仅两种有creation_id/creation_version，project_version=1；update version≥2，changed_fields为name/description非空排序无重集合且无transition；archive/delete/retry当且仅当有operation_id/operation_version；archive.accepted active→archiving/archive；archive.completed archiving→archived/archive；restore archived→active/restore且无operation；delete.accepted active或archived→deleting/delete；retry无from/to，仅action archive/delete；其他可选键禁止|Project全一致／Success；create resource project_creation.id，archive/delete/retry project_operation.id，其余project.id；create.completed Service project-initialization/cause_ref=creationID，archive.completed Service project-lifecycle/cause_ref=operationID，其余Human.id=initiatorID；只可request/runner/correlation/http_trace，operation_id若有必须等metadata.operation_id；禁tool/execution/tool_call/approval|
|provider.create / provider.update / provider.delete / model.create / model.update / model.delete（6）|共同provider_id/version/changed_fields；model3还必须model_id；create字段恰created，delete恰deleted（model.delete有replacement时恰deleted,replacement）；provider.update非空排序无重子集name/enabled/base_url/credential_ref/provider_options；model.update子集name/enabled/model_id/parameters/request_overwrite/header_overwrite/capabilities；model.delete必affected_count≥0，可replacement_id；禁止selection_id/selector_kind，其他动作禁止replacement/affected_count|Human/Success；resource model_provider.id=provider_id 或 model_config.id=model_id；只correlation_id/http_trace_id可有，其他7关联禁止；metadata只列改变字段名，不含配置正文、credential身份或值|
|knowledge.delete_subtree（1）|恰project_id/root_id/initiator_id/scope_digest/deleted_count，canonical sha256，count>0|Human.id=initiatorID／Success／knowledge_document.id=rootID／Project相等，9关联全禁；无文档标题、正文、token或节点清单|

reason 的27项固定为 permission_denied, scope_mismatch, lease_invalid, decrypt_failed, key_unavailable, ciphertext_invalid, rotation_failed, invalid_target, address_forbidden, private_not_allowed, port_denied, http_denied, tls_failed, dns_failed, redirect_denied, policy_unavailable, binding_invalid, consumer_denied, credential_denied, origin_denied, response_limit, timeout, cancelled, internal_error, storage_unavailable, payload_missing, integrity_mismatch。MIME 沿原 canonicalMediaType：非空≤256B，标准 mime.ParseMediaType/FormatMediaType 往返相等，只允许 charset=utf-8/us-ascii 参数；没有 filename/name/任意扩展。不能只用任意字符串或“含斜线”正则替代原合法集合。读取合法类型不证明对应生产运行时已绑定。

成功响应在写 Header 前完成完整验证和有界编码，选定预算 **1,048,576B（1MiB）**，不返回部分JSON／部分页／截断字段。此数不是未经证明沿用 System：[静态核算原件与指纹](#9-固定来源与规格验收定位) 静态保守笛卡尔包络为每record固定壳1049B＋canonical metadata4096B＝5145B，200项＋199逗号＋含8192B安全ASCII cursor的页壳8221B＝**1,037,420B**。壳已含全部11顶层键、9关联、最长service/cause/action/resource/outcome/summary、36B UUID与27B Instant、所有引号/逗号/花括号/冒号；数字最大19位在metadata4096内。壳内string限ASCII稳定字母，不会额外转义；metadata先由原Go JSON canonical编码（包括 `<>&`／Unicode转义），以合法 RawMessage 嵌入，不能再作为JSON字符串二次编码。若实现额外换行，必须计入且仍≤预算。8192B cursor只是安全表示上界，不能伪称真实 signer 会产生8192B token。该上界不是内存RSS／并发总量承诺。

静态提出一个合法结构候选：object.transfer.revoke、AgentRun initiator、全部关联、最大计数、256个 `&` 的 canonical MIME（当前标准库 token 可接受，HTML转义后1536B）；metadata1922B、record2949B、200项+8192shape cursor598220B。**尚未运行 Go，也未证明它是31分支中真实最大**。实施验收必须用正式构造器分别构造31动作所有最大长度／允许字段组合，记录实际最大分支，真实 DTO 编码／反解200条+完整第201哨兵；另用真实Keyring签名页token，逐字段/条数/precision/无截断验证。若Go发现更大合法分支仍在上述保守界内应更新实测记录；若推翻包络或合法极值超预算，停报规格修订，不能压缩合法数据、静默400或放宽预算过测。JSON Schema标准解析同一实际成功bytes；越预算只对损坏依赖/受控异常证明安全拒绝。

## 5. 3s 实际 I/O、panic 与默认根所有权

HTTP从入口开始最多3s发布／实际I/O预算，继承更早parent deadline，覆盖鉴权、Body EOF、SQL、投影编码、WriteHeader/Write、Flush；facade自身3s是上限，不重新获得完整时间。ctx取消后无候选／无继续Write／无第二Problem。同步服务／Body.Close／deadline callback 实际退役可跨预算，必须等待真实返回；不声称全handler一定3s返回，不用逃逸goroutine造超时成功收尾。自然3s及earlier-parent都要动态检查，不能只看ctx字段。

新project handler的私有能力解析先安装budget/defer/Body所有权，再在最多64层Unwrap中分别查首个ReadDeadline／WriteDeadline／Flush接收者，FlushError优先Flusher；允许能力位于不同层，nil／环／缺能力在业务调用前安全abort，不能提前Flush/WriteHeader/Write。panic Unwrap可保留已解析receiver供finish；read/write setter 各自 safe-call，任一panic转受控错误仍尝试另一setter、Close一次（调用前标记）、stop/callback实际join；AfterFunc内部panic不得杀进程或跳过done，finish清零setterpanic仍必须完成其他尾部后abort。Body.Read/Close、Write/Flushpanic保持原response提交状态、ctx和一次cleanup，禁止在committed后再写Problem。

仅 ResponseController 的调用使用固定receiver adapter；业务 Write/Problem 始终走原 tracked ResponseWriter，或明确透明Unwrap的等价链。不能用无Unwrap adapter遮住 httpapi.stateOf，必须验证日志Problem.code、Status/Bytes及committed防二写。失败回调测试必须先注册cancel/release/actualdone Cleanup再做fatal断言；不能只关闭channel当goroutine已join。恶意Unwrap环测试在正式RequestID安装后注入，不冒称旧httpapi外层任意恶意环已修复。

成功顺序完整Body退役／ctx检查→一次Header+完整bytes（HEAD不写）→实际Flush→stop或等待callback实际done→安全清零deadline后才允许keepalive复用；取消race／short-write／Flush失败／clear失败时ErrAbort并保原committed状态，无额外私因响应。不得只验证缓冲Writer就声称实际socket结束。日志、native trace、原件只能包含安全方法/path/status/request_id/字节数与syscall端点，禁止正文/材料/SQL args/cookie/authorization/raw frame。

默认根 `app/project_audit.go` 只用原 `createSecurity` 返回的 **同一个**真实 Audit Service 和原HTTPBoundary构造本路由，`account.go`在原一次中间件链中加入精确dispatch；不重建Audit／Store／ProjectAuthority，不另造System审批，不移动Secret checker→唯一ProjectAuthority→Audit/Secret/Model的已验顺序，不重复Initialize，不新增fake initializer或background work。全局关停沿原HTTP实际停收／取消／Drain与已验work顺序；新读handler／SQL事务／deadline callback必须确实退役。root正常关停和合法短耗尽保持真实返回与内部join的区别，独立proxy的backend退役不能倒填root内部joined。

## 6. 精确候选路径与兼容交接

实施须root另授；当前仅正式规格已接受。技术14＋文档2，共16候选路径如下。13新增已在固定基线核不存在；唯一旧技术源为account.go，两个旧文档最后授。不修改旧query/service/SystemHTTP、contract、common schema、shared fixture、resources.go、迁移、go.mod/sum、脚本或生产停止链；确需扩大必须先报root。

|#|路径|职责／所有权|
|---|---|---|
|1|internal/central/audit/project_query.go|新公开ListProject/GetProject/UnknownAttempt及私有Tx/绑定|
|2|internal/central/audit/project_query_test.go|新facade受控测试|
|3|internal/central/audit/http/project.go|新构造/路由/3s实际IO|
|4|internal/central/audit/http/project_wire.go|完整Project投影/闭集/预算|
|5|internal/central/audit/http/project_test.go|受控授权/IO/cleanup|
|6|internal/central/audit/http/project_wire_test.go|31分支/最坏页/标准schema|
|7|internal/central/audit/http/project_native_test.go|三gated真socket测试|
|8|api/openapi/project-audit.json|新闭集OpenAPI及localref|
|9|internal/central/app/project_audit.go|同实例根构造/dispatch|
|10|internal/central/app/project_audit_test.go|纯根接线测试|
|11|internal/central/app/account.go|旧源仅精确路由接入；root正式移交|
|12|tests/model/project_audit_http_fixture_test.go|本卡私有正式身份/PG/物理终局/安全body辅助|
|13|tests/model/project_audit_http_test.go|两真实投影/权限终局top|
|14|tests/model/project_audit_http_root_test.go|两默认根/实际关闭top|
|15|docs/development/backend/audit.md|技术PASS后局部当前边界/新公开口文档|
|16|docs/development/backend/README.md|同阶段末件，精确接受范围及未绑定限制|

原API／签名／查询canonical-v1、System DTO/路由、Secret Mutate-before-receipt、Model写入原Prepare/Execute与查证、Project更新30s、现有Owner read2s、Summary系统统一、Usage只读投影不变。等待配置最终接受后，Account/root helper实际hash、各route委派与旧selector由最终manifest差量重绑，不把本e4快照覆盖已接受配置写入。此计划不要求他人移交正在写的配置文件；无路径冲突的静态工作可并行，但共享包实际图不可混入活动源。

## 7. 有界验收、反例和证据组合

本卡不得以“源码有测试”称通过。先作者冻结技术14及准确正常/race/fixture/runtime读取闭包，独立验收者审固定源；源码/hash变化只复核受影响组合，保留原失败。Go/Node/native/PG执行均需root按资源与固定输入另授；下面是可执行计划，不是已运行结果。纯组不得全app run。

### 7.1 离线及标准schema

固定无网环境、已锁Go1.27.1/modulecache/工具hash，`GOTOOLCHAIN=local GOPROXY=off GOSUMDB=off GOFLAGS=-mod=readonly`、`-p=1 -count=1`，正常／race实际 `go list -deps -test`、GoFiles/CgoFiles/embed／generated TestMain／标准库variant均闭合，编译与binary运行分开，每命令45s wrapper真实subreaper/direct/adoptedwait/两次owned为空，testbody最多40s。无native环境变量、不socket/DB；integration只compile/精确-list。新增测试名必须如下（regexp均锚定，不能前缀意外选全包）：

- audit：`^(TestProjectAuditReadTransactionAndAuthority|TestProjectAuditReadTerminalAndPage)$`，正常/race；核User→Project SH、同Tx授权/Query、nil/错scope/错ID/全过滤/严格降序/limit+1及额外第202行、RowsClose→Err、提前父取消/晚Committed/NotCommitted/Unknown保存原CauseAttempt/零候选及完整callback。
- audithttp：`^(TestProjectAuditHTTPPureWireAndMaximumPage|TestProjectAuditHTTPPureDispatchAndProjection|TestProjectAuditHTTPPureActualTail)$`，正常/race；31action×三合法Actor分支（按动作约束）及全部不合法交叉、general合法System filter空结果、strictpresence/null/未知/重复、19位数/同时间分页、真实200最大页编码反解、3s自然/earlier parent、所有setter/Close/Unwrap/Flush panic、callback/服务真实tail、tracked日志/committed防二写。长自然例若总时会触40s，按这三个top各自分命令，不拉长budget。
- app：`^TestProjectAuditRootPureCompositionAndRoutes$` 正常/race；同实例真实构造依赖检查/路由委派/一次middleware/旧链保持，不运行无tag全包网络测试。对三受影响包race vet；cmd/agenteam、cmd/agenteam-runner按实际fixture固定CGO变体build但不启动。
- schema：新增wire spec用标准Python jsonschema Draft202012Validator加format checker/已有scalar严格pattern、固定本地引用common；核每个分支正例及action/actor/resource不匹配、foreignscope/null/missing/unknown/重复列表/精度/摘要/MIME参数/数字界/误接受System-only输出。schema不能只检查required列表，不能依自写近似parser宣称标准通过。冻结Python模块与jsonschema_specifications实际读取的20个官方schema JSON。实际Go编码200页bytes与保存的真实HTTPbytes都要给同标准解析器；schema对无法表示的跨字段等式由生产投影验证且受控反例独立证实，不冒schema单独检查全部等式。

### 7.2 三真实native（唯一loopback窗口）

精确独立top：`TestProjectAuditHTTPNativeEOFAndKeepAlive`、`TestProjectAuditHTTPNativeSlowBodyBudgets`、`TestProjectAuditHTTPNativeWriteAndFlushAbort`，分别执行，不把gated skip作PASS。`-race -p=1 -count=1` 编译binary，运行每top `-test.timeout=40s`、外层45s执行限额；owned进程清理15s、完整TCP含TIME_WAIT观察75s仅供actualwait/已记录资源退休，不延长45s执行或40s测试预算。使用真实port0/listener/client：完整与分段/无EOF Body、自然3s与更早parent、慢Write/Flush、HEAD、keepalive下一请求deadline确实清除。产品writer不能被仅受控模拟替代。

native冻结driver与实际argv、无TestMain资源副作用、精确PID/starttime/创建端口/FD/inode/完整TCP状态；执行前空间≥2GiB/live baseline。trace只准 `socket,bind,listen,accept,accept4,getsockname,getpeername,connect,shutdown,close`，不 `%network`、read/write/send/recv/iovec/正文buffer/syscall args材料。daemon tracer必须确切owned并实际wait。测试实际结束后两次本任务PID与全部TCP端口为空（TIME_WAIT单列直到真实消失），无法回收或parse失效先FAIL再只读补证，不能倒填原时点清零。超时TERM→有限KILL只针对任务owned PID/starttime；失败不自动重跑。

### 7.3 四真实PG/root新top

复用原完整20包postgres fixture，不增第21包；测试放现tests/model，主链正式bootstrap/admin+普通Owner注册/验证/login、正式Project持久化初始化fixture（沿已接受test Skills能力，明确不称生产Skills绑定），真实Project/Secret/Model producer写入Audit，不SQL种正常Audit事实／alwaysallow Owner。辅助测试允许私有受控canonical状态/损坏行/SQLtrace、只用于指定反例并标注，不能替代主链正式producer。4精确top按顺序分轮，任何FAIL实际清尾停后组：

1. `TestModelProjectAuditHTTPProjectionAndPaging`：真实两个Owner／两个Project，Project更新、凭据create/rotate/delete与配置写入已接受producer生成typed事实；全部一库，列表/详情/HEAD与筛选/分页只见目标Project；31分支安全完整投影由§7.1覆盖，真实PG再用合法typed Entry+正式Append authority可行的历史Actor代表（不能伪造未绑定Object执行）。至少201条由正式producer产生的合法Audit事实覆盖limit1/200与第201哨兵；相同timestamp不同ID可在隔离测试事务中仅调整已产生行的时间作排序辅助，明确不伪称producer精确同钟。另核最后页/空页/坏token/filter/scope/改变limit；归档Read允许、deleting/init拒绝辅助场景清楚。返回原bytes与source sidecar。
2. `TestModelProjectAuditHTTPAuthorityAndTerminal`：当前Session/Owner同Tx User/Project SH：先Logout提交→不查询；先读持SH→Logout真实排队→放行/实际join；Project EX holder使Read等待，取消仍零候选，holder释放并wait；错Owner/管理员/Agent/Service拒绝、外Projectcursor/详情不泄；真正同一Audit读取事务的物理COMMIT响应丢失（见下段）503/CommitUnknown/零候选，原CauseAttempt内部保持；控制Rows坏sentinel/Close后Err/合法页后晚cancel在离线补，不用结果装饰冒物理终局。
3. `TestModelProjectAuditHTTPDefaultRoot`：默认 `app.Run`，不注入handler/Store/authority/mock成功响应，正式HTTP更新/凭据/已接受配置写入生成事实再读；GET/HEAD/admin非Owner/diagnostics/request-id、安全头和一次日志，旧SystemAudit/ProjectUsage/Ownerread/update/models/credentials/config写入路由仍可达；无重复Initialize/新后台任务。readyz按既有Problem而非错误的ready字段/Account安全头假设，diagnostics才看ready:false。
4. `TestModelProjectAuditHTTPShutdown`：默认根在真实读取/锁等待中关闭，正常stop取消/事务/handler/回调实际退出后root返回；合法短耗尽保原error与实际未join边界，fixture最终释放自己的holder/proxy/backend并wait，不能将后者清零倒填root已join，也不解禁旧Object runtime缺口。无新公共Drain/Force接口。

Unknown proxy必须只武装一次**本次原Audit read**：固定Project/RecoveryCause owner audit.project-read/原attempt/该callback真实backend PID、UserSH+ProjectSH及实际查询事实绑定，歧义fail，Account/background不可消费；不得基于只有SQL关键词的泛拦截。至少一例观察同backend服务器 `C(COMMIT)` 与 `Z(I)` 后再丢回客户端ACK，保存安全phase/PID/终态bool，HTTP仍Unknown；不保存rawframe/SQL参数/材料。pending writer收到但尚未转发COMMIT→后来终局的旧helper只能作另一时序，不能冒terminal-first；Committed装饰成Unknown只作受控边界。读取没有持久化command receipt，不能套Secret nonce/Project写入lookup。proxy/holder的fail cleanup先注册，所有后台goroutine/command都实际join。

原资源要求完整继承：每top120s包含t.Cleanup，包6m，driver流式精确RUN→PASS/FAIL watchdog，预算不能仅defer事后断言。每轮fresh≥5GiB、live PID/starttime/daemon集合与7资源baseline（4容器+3网络），空Docker config/精确本地两digest与MinIO binary核验，不临时拉镜像/修改依赖。固定PG17 `pgvector/pgvector@sha256:99a149d3c84cfb0f32d8da7d72737e4643468787220af2223418730f8e9e9cdc`，PG16 `pgvector/pgvector@sha256:16e62164a405447dca191079a924ee5b8a9dbf04fe53128701ffbea857b37782`；MinIO RELEASE.2025-10-15T17-29-55Z binary SHA256 `dc5298474f0bc87a068f0b1135c583bb1278c17c11c512212ed7644a238c89c8`。原完整fixture graph、动态两cmd/三helper、CGO0变体、配置/SQL/schema读取必须实际闭合，按最小delta继承已验图，不全树复制。

subreaper direct/adopted实际wait，主结束后owned残留有限TERM/KILL退役；发生强制尾使该轮FAIL，不隐瞒为纯PASS；原7资源精确label/canonicalMount/owned链两次为空、前后sourcehash同后才交窗。Docker daemon侧PID1 shim差集按新实际集合报告，非本任务可wait，不操作PID1、不声称全机清零；环境重启不能沿用旧PID基线。版本组合PASS须保原首红、修改指针及实际affected重跑，不要求无关已PASS重复。

实际body保存到显式scratch `AGENTEAM_PROJECT_AUDIT_BODY_DIR`，0600原bytes不parse/stringify重编码，sidecar仅method/path/status/Content-Type/Content-Length/request-id、目标Project/AuditID、source_run/producer_test、candidate/input/schemahash与body SHA；无Cookie/CSRF/auth/密码/DSN。标准parser固定 `AGENTEAM_PROJECT_AUDIT_SCHEMA_PYTHON` 指向已冻解释器，旧选中Usage helper仍需原 `AGENTEAM_USAGE_SCHEMA_PYTHON`；最终至少真实list/detail/HEAD来源与最大页bytes的同输入标准schema通过。HEAD空body单验，不能拿合成payload冒实际HTTP。

### 7.4 旧兼容精确组与独立代表

旧实现无变化的结论可引用原固定验收；受影响包如下精确组必须在本卡最终共同冻结输入下运行／真实复核，不能只-list宣称通过。普通/race纯组每包45s预算可按top拆，禁止全app：

- audit旧6：`^(TestSystemAuditReadTransactionPublication|TestSystemAuditReadFailureHasZeroCandidate|TestSystemAuditPageScansSentinelAndClosesBeforePublication|TestSystemAuditReadDeadlineAndActualTail|TestSystemAuditLegacyQueryScopeAndValidationEquivalence|TestSystemAuditCursorBindingAndInvalidActor)$`。
- audithttp旧9：`^(TestAuditHTTPPureMIMEClosedSchema|TestAuditHTTPPureTypedProjection|TestAuditHTTPPureQueryClosure|TestAuditHTTPPureEncodedBudget|TestAuditHTTPPureRoutesAndAuthorizationOrder|TestAuditHTTPPureBodyWriteAndCapabilities|TestAuditHTTPPurePreauthenticationBudgetAndCallbackJoin|TestAuditHTTPPureBodyCloseJoinAndSafeUnknown|TestAuditHTTPPureOpenAPIClosedContract)$`。
- app旧6：`^(TestAuditRootPureRoutePreservationAndSingleMiddleware|TestAuditRootPureSameInstanceConstructionGraph|TestProjectCredentialsRootPureRouteOwnership|TestProjectReadRootPureRouteOwnership|TestProjectModelsRootPureRouteOwnership|TestProjectUsageRootPureRoutesPreserveOriginalChain)$`；另配置写入接受后的精确 `TestProjectModelConfigurationHTTPComposition` 是未来已定卡名，最终须实-list与accepted源确认，不在当前e4中冒存在。

真实旧组分包/分轮精确锚定：tests/account两top `^(TestSystemAuditHTTPQueryAndProjection|TestSystemAuditHTTPTransactionAndBudget)$`；app一个 `^TestSystemAuditHTTPRootProducerBinding$`；tests/security一个 `^TestAuditPaginationBindingsFiltersAndRevocation$`；tests/model四top `^(TestModelProjectCredentialHTTPDefaultRoot|TestModelProjectOwnerUpdateHTTPRootBinding|TestModelProjectUsageHTTPRootBinding|TestModelProjectConfigurationWriteHTTPDefaultRoot)$`。最后配置write top同样待接受manifest精确确认；每top120s/包6m，不把4个top预算叠到6m外，不足则预先按top分轮。原Owner/Model read、Summary root其余既有正确输入结果可复用，§7.3根委派测试不得删这些路径。旧System native实现不变可引用已接受完整证据，本卡三native验证新增writer，若改共享链则重评受影响范围。

独立验收至少两互补真实代表：A 当前Owner同Tx权限/锁与坏哨兵过滤绑定，B 同读事务terminal-first ACKloss／原CauseAttempt/零候选及一份默认根原body同schema；独立可复用正式身份与资源工具，但必须说明复用辅助能力与自己的新事实，不能机械镜像作者所有矩阵。若作者已完整承担上述物理终局，独立B可选择真实权限撤销相反次序和根tail，通过风险说明重组，不降低该物理终局必须有实际证据的门槛。

## 8. 接受门槛与当前声明

实施接受同时需要：技术14固定全STATIC、真实普通/race/vet/标准schema、31合法分支与最大200真实编码／全哨兵、三native、四新PG+上述旧兼容、独立互补验证、实际source前后/资源尾部闭合，随后两文档停写末件独审。若公开接口/合法投影/范围/预算/依赖与此卡冲突先报root修订；产品含义未变的测试/helper错误仅修自有scope并保首红，不为过测改权限/HTTP原语义。不得把accepted schema或库单测等同默认根/资源证明。

本rev2技术含义与已接受候选相同；正式归位只更新状态、文档链接与自包含固定索引。独立 STATIC 与 root 采纳均是规格结论，尚未执行或验收本卡产品。原建议、rev1、B1首问题与rev2差量原件全部保留，见 [§9](#9-固定来源与规格验收定位)。后续实施仍以页首调度与另行授权为前置，不将文档归位视为实施或资源授权。

## 9. 固定来源与规格验收定位

持久证据与可逆修订定位见[规格验收归档](../agent-team/project-owner-audit-http-spec-verification.md)及[来源映射](../agent-team/project-owner-audit-http-spec-verification-evidence/source-map.json)；下列原scratch指纹和77项固定来源表保持原样。

本卡 §1–8 是完整工程约束，不依赖外部 scratch 才能确定接口、字段、范围、预算与选择器。以下原件绝对路径用于证据定位；若运行环境被回收，以固定提交、逐源 Git blob／SHA-256 与本卡正文重建来源，不能改读活动工作树当已接受输入。scratch 不是产品代码或已运行测试的替代品。

规格作者为 `usage_verification`，独立审查者为 `next_frontier`；rev1 仅 B1 两个可执行包入口路径需更正，rev2 已以固定 `cmd/agenteam/main.go`、`cmd/agenteam-runner/main.go` 关闭，其余技术语义与14＋2范围不变。root 已采纳完整版本组合 STATIC PASS。

| 原件 | 绝对定位 | SHA-256 |
|---|---|---|
| 接受的 rev2 候选 | `/workspace/scratch/project-owner-audit-http-spec/draft-rev2.md` | `3eae6ce5d129e8125aba157cb4687656c31cd49fe654e3c583a386ffe843a809` |
| 原 rev1 候选 | `/workspace/scratch/project-owner-audit-http-spec/draft-rev1.md` | `95db31411d7e41db7966cab30b894c11d4f905696ff6a4f6572b89b47cd9c02f` |
| rev1 → rev2 差量 | `/workspace/scratch/project-owner-audit-http-spec/rev1-to-rev2.diff` | `3785380536f80244ea028e50150defdfa1b0de84e016e6dfb77088d1cc7a8906` |
| rev2 作者冻结 | `/workspace/scratch/project-owner-audit-http-spec/freeze-rev2.json` | `9a0bdc7df2fcbfbdf00ece64e40621929a97ffe4e53b10b25a61fb4ae76496a3` |
| 77项固定来源索引 | `/workspace/scratch/project-owner-audit-http-spec/inputs-rev2.json` | `a4bbed783e205ab4b9274e38c20cc6033442070552e9f4f0ea93fa338eaf7727` |
| 16路径范围 | `/workspace/scratch/project-owner-audit-http-spec/scope16.json` | `4daf7cc824b6526b99349f772f97319cf313e28be0ec3a06ccd6b657741d790e` |
| 精确选择器来源 | `/workspace/scratch/project-owner-audit-http-spec/compatibility-selectors.json` | `85ed926277c007af8dfcfe206ba0a169e6effa436f5420d9f05cc281c3bb1d9b` |
| 静态核算脚本 | `/workspace/scratch/project-owner-audit-http-spec/budget-static.py` | `13525ab6724947c0ebafc367f56561a29893a4bf757f11aa9fa1566900bf2e3c` |
| 静态核算结果 | `/workspace/scratch/project-owner-audit-http-spec/budget-static.json` | `a0c02c2db3247c48e05fe5b5bbdd1f997f54370b165f6dc8b9f1c4109a063ef6` |
| rev1 完整独审 | `/workspace/scratch/project-owner-audit-http-spec-verification/rev1/result.json` | `2e1b24ea418798aa1bc67bab134cf0ce4182ce493f5a2de71194224604044fb1` |
| rev2 独立差量／完整组合结论 | `/workspace/scratch/project-owner-audit-http-spec-verification/rev2/result.json` | `e2103767a9e8a280a3a8161608d235b052a2d587abf30bd6e245a78a0dcc3064` |
| rev2 独立报告 | `/workspace/scratch/project-owner-audit-http-spec-verification/rev2/review.md` | `a0d98f7b18215c90efba5c75628b97db8ec6d2dbe975c83508d4b10d23a6f642` |

### 9.1 固定已接受来源

以下77项均从产品提交 `e4b1b89197da0e9027018fdb41f6ab3e04050b9d` 读取。前40项继承已接受前沿分析，随后35项是本卡必要定点依赖，最后2项是B1路径更正的入口证明；未读取配置写入活动14源。当前链接只提供导航，不把其未来活动字节当作本表接受版本。治理随后如有正式修订遵守现行治理，技术复现仍使用表中固定内容。

| # | 仓库路径 | 固定 Git blob | SHA-256 |
|---|---|---|---|
| 1 | [AGENTS.md](../../../AGENTS.md) | `764823cf10ed88d24b96f9e94bdabc3702e3aa6a` | `cdfc9d803deceee06968567236735370af0e7da0f663668c63803ae33b54eccc` |
| 2 | [.agents/skills/agenteam-design/SKILL.md](../../../.agents/skills/agenteam-design/SKILL.md) | `dc06833821245cc578b46febc741c5740b3ec7f0` | `81834a981f226cc663c42b9dcdfd9b56001d2cfcd649107064bf865af4d61b81` |
| 3 | [docs/development/agent-team/README.md](../agent-team/README.md) | `7a31918bdc048515046dedcac20ce0e636106f14` | `9d8bad3c85cf2a34fd0d9360bbc9eb27ea5c76082222a174984f78d599701c40` |
| 4 | [docs/architecture/README.md](../../architecture/README.md) | `1696d14033ae4a236f95f797528ed908179bb87f` | `ebbdb68cf1facc0786eae34771d08b9d27c8876fcf89376d91c20f54d9492cf9` |
| 5 | [docs/development/development-plan.md](../development-plan.md) | `8cd99530bd66d7a272422f430dc2cf39c2e371b2` | `e3f5abdad1151912610c497b8efa3b09266da83203a8e6115f1c35df0457d44c` |
| 6 | [docs/development/agent-team/tasks.md](../agent-team/tasks.md) | `c24b625d12d7875ed86e69945280d482847e0a0d` | `1ca6646746e573cdc24189bc93560b9c4e3a22c99de1886069b7ab6b29605ab0` |
| 7 | [docs/development/backend/README.md](../backend/README.md) | `5d63e41153c3da79a4e72b7b1e83188ad569e481` | `b5b4444d61af796a9ecd5843b4de9cfd55981324e21ba1e6ef4b0c991bac0e5e` |
| 8 | [docs/development/backend/audit.md](../backend/audit.md) | `d88a90cfb492d5bdb7a2b36e5278d4b08ec376bf` | `6c0447804f5bd92c6963d56984848130cddebeff5142e27f7f338c8f426274ca` |
| 9 | [docs/architecture/security-governance/audit.md](../../architecture/security-governance/audit.md) | `cd378e556d3b59628f5c945e5cfcd46a5fc572ec` | `c5be8ea4041cebffe6eb4ee994147fcf6c8b7f75d324898a4c90bbd0962e9929` |
| 10 | [docs/architecture/project-work-management/project-environment-variables.md](../../architecture/project-work-management/project-environment-variables.md) | `4480e86b3bdbaae8ec30d3d32f9a15f772025382` | `3d473b08c7257c4cf73fd89ea975efc1e3bfe5c8313814cae146846af21d4aad` |
| 11 | [docs/development/work-items/d09-project-model-configuration-write-http.md](d09-project-model-configuration-write-http.md) | `e94bb63dea0f49100a4a3a896f65859bd8380553` | `cce364e54c513a04b7fd45298b98e1e22a2e5154340619c7d973f63f4dda5235` |
| 12 | [docs/development/work-items/d04-system-audit-management-http.md](d04-system-audit-management-http.md) | `1e7efd2497185928349b80de443dcea79cb6e34a` | `989046372da9679a5760bf0857e4e64b1dc6e7499ef49af676b2974e0c61e7d1` |
| 13 | [docs/development/work-items/d10-skills-initialization.md](d10-skills-initialization.md) | `14788e94d8ba24a42939737a9dc5d55c32da76f0` | `87033b3a37d03568fa47441084d9153c3ce3d2bebf34c5949dd52150d4ea2f6b` |
| 14 | [docs/development/work-items/d12-b02-knowledge-service.md](d12-b02-knowledge-service.md) | `230c7980351b52d6047ec369d79ac65242c6add4` | `f1cc70efc15e6f7c9024b1db578cfe7333f4722c2ce8be50603c881c7ed7fba3` |
| 15 | [docs/development/work-items/recovery-d09-current-model-resolution.md](recovery-d09-current-model-resolution.md) | `f7dc08f528199e0415d8b67378f9b327bfb2d659` | `2a698720f2879eea62384f570ea5086d50aaebc578c8a6fe2ce113c93e7e57f6` |
| 16 | [docs/development/work-items/recovery-d09-invocation-usage-ledger.md](recovery-d09-invocation-usage-ledger.md) | `f245822045261a1138f23903fa8002808014fb8f` | `5c6f555d163309089fc451c28987643f72361ea18416765144b43673953574b6` |
| 17 | [docs/development/agent-team/d09-next-wire-dependencies-2026-10-07.md](../agent-team/d09-next-wire-dependencies-2026-10-07.md) | `dedcb8bdfc410d5b8e5e7ecc3a0328bdd600c15b` | `6ee5b8275379c72d16bd527c190d37b2b86e70033a1bf8ddd9c5f29ae63ccc6c` |
| 18 | [docs/development/agent-team/system-audit-management-http-verification.md](../agent-team/system-audit-management-http-verification.md) | `a8cbabdba4da723469f7cf603b5f68f53ed00dad` | `61107f3ec3a5b51eca843e2b1ab316d0c008f85e8118899de4298d99ea989883` |
| 19 | [docs/development/agent-team/d09-project-configuration-verification.md](../agent-team/d09-project-configuration-verification.md) | `194a346b154d98b06f2ef7059156473955392fea` | `15abc5df4ee29054bf6045e6b5ca316a8b9280e0205c26bf23ae9c72297d7291` |
| 20 | [docs/development/agent-team/secret-project-audit-verification.md](../agent-team/secret-project-audit-verification.md) | `c9740ddbd2f60bad0ed6970fb196a632889ebb20` | `a63800cc04315d1fe7916fcbb53acd6f84cd1b6747c8b3e247ff2a2a1c36d733` |
| 21 | [docs/development/agent-team/d10-p1-recovery-verification.md](../agent-team/d10-p1-recovery-verification.md) | `c9d184274cb7b067ba73a6fd8122aea3023871ee` | `e0fff9717e5ace0ec3c300206073d29312fc823a2d90b39cd779803e1fc40960` |
| 22 | [internal/central/audit/query.go](../../../internal/central/audit/query.go) | `8a24e10fac01fd4dd779384e551f88a223760c97` | `3713a95d285b19300e0038cb6243f9932754bb22a030afb820012b8f9e8968f3` |
| 23 | [internal/central/audit/system_query.go](../../../internal/central/audit/system_query.go) | `15124c702afbdef16caa30e495d998aaa499c8dc` | `b84646b38e6810d02e3c6b259c1ee1e88b26aa79af406ebb5c504b58abd2014f` |
| 24 | [internal/central/audit/service.go](../../../internal/central/audit/service.go) | `3ba349c7c316dfc09752185e46427104923a5602` | `0e9efa8c36c0538617a4e499d9224894ebd22f61ec7de49f54ba8cbfce98b689` |
| 25 | [internal/central/audit/contract/access.go](../../../internal/central/audit/contract/access.go) | `d550c158c6e3043fb24dece099c1ed3397c9237e` | `775a838c03690fef4b709c37975fd23ccfc24e506569d4c3e8684ac32f8abfd1` |
| 26 | [internal/central/audit/contract/types.go](../../../internal/central/audit/contract/types.go) | `295170e6f48788f772e03b61142afca5734393ce` | `e39c14959dc0e705e07b5848b420e48b06f3756c3c083c9ca1a471a049a9679d` |
| 27 | [internal/central/audit/contract/metadata.go](../../../internal/central/audit/contract/metadata.go) | `13538ff6a994ed740b2f92b021297f8345fa701b` | `bdffe42f77fa2525dbbc03a3d3749af7b7665cb6cf2f995d62b82863a2080646` |
| 28 | [internal/central/audit/contract/project.go](../../../internal/central/audit/contract/project.go) | `aa18033cf7bf34725fb4251372c221cde186ba61` | `526e8c86d72f45b2584572c159586b59a360e1029269b4304d68ffa55f26083b` |
| 29 | [internal/central/audit/http/handler.go](../../../internal/central/audit/http/handler.go) | `8899a76bf5c49731d68434e8bbe315bf1eb9296e` | `b13e6a0885c155ff4eff737790be0fb4f06191abb0d0409701f510cd6441a039` |
| 30 | [internal/central/audit/http/wire.go](../../../internal/central/audit/http/wire.go) | `49e8187e117cec265f548ecd798faa128069ee92` | `4a42524d07301277350ed57d0a15901e2dfa6a541e16e14bbd145563adb593b4` |
| 31 | [internal/central/project/authority.go](../../../internal/central/project/authority.go) | `c8af518a1a99084f07f56633d1ec3e3afe774af4` | `a9e7d5768a18c23abf9a1cf86a396bbe6901d348c70b16c48b32fa559fa95321` |
| 32 | [internal/central/project/audit_authority.go](../../../internal/central/project/audit_authority.go) | `fa2ad4a2cfb1bcca1d0432fde0c10679b25b6ec6` | `3736a441bb6d49b48384069a1c0c014bd4df9278dd3b9b1713f2772fa4066a9b` |
| 33 | [internal/central/project/contract/lifecycle.go](../../../internal/central/project/contract/lifecycle.go) | `f7f0829a87bde8cfda7ed5c59f50117bd2f6fe79` | `5b74f0a959bbcffde811b11d207f9ed6928bded9d51c965772d34763520ddea6` |
| 34 | [internal/central/app/security.go](../../../internal/central/app/security.go) | `edf5a88ee28c9fb0d3c0c8cc3de34f741a511dfa` | `0804ec5ab4c3b8566d3f933881415779bf354eaa89a1bb85e0fb4fb09bd0a7a4` |
| 35 | [internal/central/app/account.go](../../../internal/central/app/account.go) | `3837dfa2dfc5758b729f7280b78527880ac4edb4` | `89d01abf651ca247515ae8226c739469b86ed01ac6cb4a717cdd3091633e79e7` |
| 36 | [internal/central/app/project_usage.go](../../../internal/central/app/project_usage.go) | `3463ae43544a68548924b7faf513ab0cfffe490a` | `13abb6bbdc4d38d40b1ba627aa989aec503ffe55c7a310dd42d11acc33d81275` |
| 37 | [internal/central/app/project_models.go](../../../internal/central/app/project_models.go) | `788747bfbffcbcb41fe51c25b96eeac3b219b3c4` | `9cc03c1c29c004e456e6e7151f55de40deb23378cb15f2ba47f7722d414ca771` |
| 38 | [internal/central/app/project_credentials.go](../../../internal/central/app/project_credentials.go) | `9e020f31af5d7b4bb7acdb889d0879978a846c03` | `1d155adeaef744b05c75071a828ee6aee3002ab4cba3848f92a14dda0b2a8dd5` |
| 39 | [tests/account/audit_http_transaction_test.go](../../../tests/account/audit_http_transaction_test.go) | `cada61e9861b92cb8a33ca737afda727fa972472` | `56f5b38e2a35117fc29e5b634a51bd4f613fa1969c848119f8b1f9f4c236c25b` |
| 40 | [tests/model/project_credentials_http_fixture_test.go](../../../tests/model/project_credentials_http_fixture_test.go) | `f91fba666d428cdeea663db3bc6858cbece7a610` | `cfdc4107891cf0be9a6ba2d963ca90550932a577825a6e2a5743774c537ba38b` |
| 41 | [internal/central/audit/contract/model.go](../../../internal/central/audit/contract/model.go) | `e8a764e3c1b8ef0846b7cf73a67051924a014c24` | `c94372d74737ed0775b004a6e636927c0e93df2bb1716dc5310ae64cb36d44d6` |
| 42 | [internal/central/audit/contract/knowledge.go](../../../internal/central/audit/contract/knowledge.go) | `8caceef17717d77ecb73b588b4779e03bfb52ed1` | `270e1943c9539dda9dc2ae9bb1013a0a48bf2309578b2da8d36c1a432a4d8372` |
| 43 | [internal/central/audit/contract/account.go](../../../internal/central/audit/contract/account.go) | `25a8f62fc1e10295cc98032ce756ebf8cd81b0ba` | `ef2b5bc2cca4435161d9b2f543c62b868324e54813581da894bfbfb70e42bfed` |
| 44 | [internal/central/audit/error.go](../../../internal/central/audit/error.go) | `58ab74d590762d4cd731512cd3469aa8c42f6ef8` | `ad980308d48166a978891ad999adbf4309dee28fe201bf40e3629192712a58d8` |
| 45 | [internal/central/foundation/tx.go](../../../internal/central/foundation/tx.go) | `3988c8d4ab53261e784fcd175f27b88395b5087d` | `b94efd2af7b01a3d857017cd4a4ffc5f577801044a203daff25a8746d80aec4f` |
| 46 | [internal/central/foundation/fault.go](../../../internal/central/foundation/fault.go) | `114e98b7b68d536be0dc9aad22221447b098991b` | `94a339976fb8f1e1173256aa840dd1273644cafd37dc6a3e96e33d1cb8ff09de` |
| 47 | [internal/central/foundation/cause.go](../../../internal/central/foundation/cause.go) | `4027e70d18735dd54d1f59b23382a0699022bee4` | `1af9a1fe3dcb8bb3b8aabdc55eea4a70ed78da3f6abf9dbf7c2629ef05f934c7` |
| 48 | [internal/central/foundation/lock.go](../../../internal/central/foundation/lock.go) | `f907283e5fc67482c99eb5c14add91923788ad2a` | `5f328d35a0bf313f589654c9e52b5b6b0b9f8f3f3b8502e9d1f612b588220384` |
| 49 | [internal/central/foundation/instant.go](../../../internal/central/foundation/instant.go) | `b6113cd662e75bd8b1505af6ea39fc36e819782b` | `95a37fb81d71f90626a92bd3318d675d69b7750204c34bd54cc614332a951d47` |
| 50 | [internal/central/foundation/page.go](../../../internal/central/foundation/page.go) | `83cbeea722ddedbc907f23ac64a0a2bee702b067` | `3be782a4c61729a5a2b13e195e5653ad1070e883765035b5cbb1f4f9e85c3c52` |
| 51 | [internal/central/identity/contract/identity.go](../../../internal/central/identity/contract/identity.go) | `fd13667713bf1248111aa1f18687296be9c3cde0` | `35403f72515c25c7950e0c199e73ce5b18e0f81f3363bb9dfb200f795ac8e8b0` |
| 52 | [internal/central/account/http_boundary.go](../../../internal/central/account/http_boundary.go) | `2c96df37be1f070ef440f21b4372eb66cb958d89` | `39e87840acced340197c533f9db25b64a42f52643f99d95ee2bff8040333c572` |
| 53 | [internal/central/httpapi/response_writer.go](../../../internal/central/httpapi/response_writer.go) | `6f3ca6b0cb76752adccde7cac5be47e3ca333776` | `360d0992159938812cfdad706a4b8b53de1e7673859462100aad4bd44bd361bc` |
| 54 | [internal/central/httpapi/problem.go](../../../internal/central/httpapi/problem.go) | `f489d6b96621bc8dfdac29a63575d2b6ceba9dd5` | `613140e4513630376fcd4ff77725cd3812e3ce4508edef823a550f40459e3cde` |
| 55 | [internal/central/project/service.go](../../../internal/central/project/service.go) | `68d495375017a1562e2f712e55ce2d96bd77a6b1` | `ee0b6294d901829c0d89f4bdcfb0869031dfbea968ded2d07add070c6282591f` |
| 56 | [internal/central/project/reader.go](../../../internal/central/project/reader.go) | `8edc23d6235cdf316a62a878aab910f0cd7b0fed` | `37cd934431d601bf052719eae9ad7f91274a1bc583a13c1425d9d14082555ed5` |
| 57 | [internal/central/cursor/cursor.go](../../../internal/central/cursor/cursor.go) | `f12c75aa6d76a3792b038c0bfdc04fe99da175e4` | `c9f13d1cc4f40966cf545d0ea6dcc0f5c5ffc821982e54f3e680e1a604150e5f` |
| 58 | [internal/central/audit/system_query_test.go](../../../internal/central/audit/system_query_test.go) | `e88a015570530532563fbc6998a417c7073c0140` | `411baa52add891a82ba720643981eae1584c3b7371a8a21a8e21ff56591aef6d` |
| 59 | [internal/central/audit/http/handler_test.go](../../../internal/central/audit/http/handler_test.go) | `eacef5de8f5e3c759aa37fbd78cc53e79a68956d` | `0844862bbaa0ac8f7f90dbd574e73534333c3d31b1bba10e2d3dd22cff5c0764` |
| 60 | [internal/central/app/audit_http_test.go](../../../internal/central/app/audit_http_test.go) | `8e23f848a90787dcbc68fc38b88fea4a5db12ab0` | `c3bef11fb2c72a62d4c89e44a4921ff98e508ba00326b16fe5bdb5bfe6de8467` |
| 61 | [internal/central/app/audit_http_process_test.go](../../../internal/central/app/audit_http_process_test.go) | `cc15eb98044d916807159cf8ca1c94698bf1d2fd` | `b44be5136db527ebcb39da30da760b49bc794937c32db3f44d02c07e01f9d1ba` |
| 62 | [internal/central/app/project_credentials_test.go](../../../internal/central/app/project_credentials_test.go) | `99386e6d2f3072cb1df2c74eca9a8e976d49b7dd` | `139525a6ccf4decf256e61c0ebc7090dd7f98d9f71adc21161a74b237613a5af` |
| 63 | [internal/central/app/project_read_test.go](../../../internal/central/app/project_read_test.go) | `70db50693971fc23fca852c391fb978ac8338030` | `9722e3da78abdd38a9cc9dcc019094264c1227b391c2feabb06a9f7aaae62e3a` |
| 64 | [internal/central/app/project_models_test.go](../../../internal/central/app/project_models_test.go) | `c3415dd5d9635a8a0fdd7adee942f8bfbbebfda9` | `44eb1fbaac79118703e5bb4cc4ef2d29dde5c60b556c7a688d4226e1e7e85f46` |
| 65 | [internal/central/app/project_usage_test.go](../../../internal/central/app/project_usage_test.go) | `ff015cbe6ced14b028bf1085919044b4f9a73db8` | `5321f1119dc32d075b8e332cf132c03ad5864e700bf25f9ba6bff4cd2ad272cf` |
| 66 | [internal/central/model/http_project_credentials.go](../../../internal/central/model/http_project_credentials.go) | `55910fb50864f2ec01b0c53082df4c92126bb8e6` | `af55eb294e131d779bc7cfc1fc892c522f07da86666e088b73af51ac4513a878` |
| 67 | [tests/model/project_credentials_http_root_test.go](../../../tests/model/project_credentials_http_root_test.go) | `df304d974298605f41ec705ed50b667f698d9ff6` | `0c11d5e3bf701bc0525841c3428196e91c53003e4e4c57122b7ee110ff3d7d6d` |
| 68 | [tests/model/project_owner_read_http_fixture_test.go](../../../tests/model/project_owner_read_http_fixture_test.go) | `b38ea67a393089a56588bb0f1d3474b4756e070a` | `56adb3a44e0fb3d4f5d50559a2f31cfcfd2a1e194723b0d44627d7a826d5ac39` |
| 69 | [tests/model/project_owner_update_http_root_test.go](../../../tests/model/project_owner_update_http_root_test.go) | `059458a945ba93152145909fdee2cac3f06903df` | `352cfa5d35c39109fd0759cc8879ebe65e53df2a686e564b5c5fa9fd388b0103` |
| 70 | [tests/model/project_usage_http_root_test.go](../../../tests/model/project_usage_http_root_test.go) | `88fe91b1218ab1bfc31610c810c37aadc5069a12` | `1d63defd90cbf1637b290adf1eba9b3e2c9c449453d63d17b1204e6ae3f67f8c` |
| 71 | [tests/account/audit_http_test.go](../../../tests/account/audit_http_test.go) | `53b2bc3ddcc09c2982efc65fc58f20b23f8a8e32` | `788fcc081557181115f17202964e77a99e04b140924cfc7c33df49095c17cff4` |
| 72 | [tests/security/audit_query_test.go](../../../tests/security/audit_query_test.go) | `3cb4b18df03376e61f40b0166f6b82355a537428` | `4307432031c51718d98397fa5edb77313773ca9f2d805adbcde07b64241485cd` |
| 73 | [api/openapi/common.json](../../../api/openapi/common.json) | `22f7d8e5f560f05380ca9c8031260f2d941ad273` | `d80bdf91970272768c74aa89cf7c3b1faed8187c78297b3ef1ecb69780aab212` |
| 74 | [go.mod](../../../go.mod) | `2db97d5624d9575b72eb8e345b064bc4185388dc` | `294a95594aa10b67474d8c82b01a6def9d64ca132fca53a1afff081736fe8758` |
| 75 | [go.sum](../../../go.sum) | `7f96b9efaf394930b1df6f095e0879e33cd95403` | `6bc1fd93b203eefb8360ed556980575429b0a35f999f5c8c984f611203633245` |
| 76 | [cmd/agenteam/main.go](../../../cmd/agenteam/main.go) | `1345651a64a24833421f22b21bdb80a2b6df559c` | `4412cc0e4d838b076d1447ea3ec9137f1eb0dffc01f95d8ff54c5756e305198a` |
| 77 | [cmd/agenteam-runner/main.go](../../../cmd/agenteam-runner/main.go) | `afb614f13f0148147586b0c431aa912187fa2ffc` | `e4341598318b791e6bd03b2cff75a1a6280adea2b4efa68736bb323f3e70aa66` |

### 9.2 静态工具来源与资源复用边界

静态 MIME 推导读取 Go1.27.1 的两个标准库源文件，未执行 Go。源码定位与SHA如下；实际实施需重新冻结真实工具与所有执行输入。

| 定位 | SHA-256 |
|---|---|
| `/workspace/toolchains/go1.27.1/src/mime/mediatype.go` | `2179fd195097a382a2ac3cdcbcbc6961f2770dbd61adfabfc60535c1acc5faa0` |
| `/workspace/toolchains/go1.27.1/src/mime/grammar.go` | `85d1052199201e6ad52e27db50104842295dbf10e87f3914c803c6ce533772a6` |

以下只作为已接受凭据实际图／fixture工具的最小复用入口。不能将原物理资源、PID或旧环境baseline复用于新轮；新卡有效图、实际新输入、候选、命令、空间、镜像、PID/starttime、daemon及7资源基线必须重新闭合。

| 原件定位 | SHA-256 |
|---|---|
| `/workspace/scratch/project-model-credentials-http-author/actual-graph-full02.json` | `a670f98b64bec39e931662e6c096ec33c9b0ef0c7f4e6b0e8a1d120d5eee1504` |
| `/workspace/scratch/project-model-credentials-http-author/pg-driver-v02/closure.json` | `c710918462af79b1fb08f734403f8a3ba8221ec991884b00fd53bb6aa0de30b3` |
