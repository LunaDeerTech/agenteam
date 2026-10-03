# Model、Tool、授权与远端执行

依据：[Chat Runtime](../../../architecture/platform-infrastructure/model-system/chat-model-runtime.md)、[Model Resolution](../../../architecture/platform-infrastructure/model-system/model-resolution.md)、[Usage](../../../architecture/platform-infrastructure/model-token-usage.md)、[Tool 定义](../../../architecture/tool-system/tool-definition-registry.md)、[Tool Execution](../../../architecture/tool-system/tool-execution.md)、[Tool Result](../../../architecture/tool-system/tool-result-backend.md)、[治理](../../../architecture/security-governance/README.md)、[一次性批准](../../../architecture/security-governance/one-time-approval-retry-idempotency.md)。类型使用[基础约定](foundation.md)，状态枚举见[领域状态表](domain-lifecycle.md#状态与原子转换所有者)。

## Model 解析与调用

```text
ModelPurpose = agent_generation | agent_compaction | approval_auto |
               meeting_summary_initial | meeting_summary_update |
               knowledge_embedding | memory_embedding | memory_extraction |
               memory_consolidation | memory_reflection | rerank | image_generation
ModelConsumer {kind: agent|knowledge|memory|tool|meeting,
               project_id, agent_id?, execution_id?, meeting_id?, operation_id?, purpose}
ResolvedModel {snapshot_id, provider_id_snapshot, model_config_id_snapshot,
               protocol, adapter_revision, endpoint_ref, model_id,
               parameters: JSON, capabilities: ModelCapabilities,
               credential_ref_snapshot, credential_lease_ref, selection_version}
ModelCapabilities {tool_calls: bool, parallel_tool_calls: bool,
                   input_modalities[], output_modalities[],
                   reasoning_efforts[], context_window?, structured_output_modes[]}
CredentialLeaseOwner {kind: execution|model_call, id} // secret contract 自有类型
ModelLeaseOwner // model contract 对 CredentialLeaseOwner 的引用
ResolveModel(ctx, actor, project_id, model_ref, purpose, reasoning_effort?,
             lease_owner: ModelLeaseOwner)
  -> Result<ResolvedModel>
ResolveModelInTx(ctx, tx, actor, project_id, model_ref, purpose,
                 reasoning_effort?, lease_owner: ModelLeaseOwner)
  -> Result<ResolvedModel>
AcquireCredentialLeaseInTx(ctx, tx, service_actor, credential_ref,
                           lease_owner: CredentialLeaseOwner)
  -> Result<{lease_id, credential_ref}>
ReadCredentialForRequest(ctx, service_actor, lease_id) -> Result<SecretMaterial>
ReleaseCredentialLeaseInTx(ctx, tx, service_actor, lease_id) -> error
ModelRequest {logical_call_id, consumer, resolved_model, input_ref,
              messages: ModelMessage[], tools: ModelTool[],
              tool_choice: auto|none|required|named {name},
              response_format: text|json_schema {name,schema},
              retry_class: agent|bounded_consumer, cancellation}
ModelMessage {role: system|user|assistant|tool, parts: ModelPart[]}
ModelPart = text {text} | image {file_ref, media_type} | file {file_ref, media_type}
          | tool_call {call_id, name, arguments: JSON}
          | tool_result {call_id, content: ModelResultPart[], is_error: bool}
          | provider_metadata {adapter_id, version, opaque_ref}
ModelResultPart = text {text} | image {file_ref, media_type} | file {file_ref, media_type}
ModelTool {model_visible_name, description, input_schema, output_schema?}
Chat(ctx, ModelRequest) -> Result<ModelResponse>
Stream(ctx, ModelRequest) -> Result<ModelStream>
ModelResponse {logical_call_id, invocation_id, assistant_message, finish_reason,
               usage: Usage?, provider_request_id?}
```

`input_ref{execution_id?, round_id?, input_digest, input_schema_version}` 指向调用者已固定输入，Agent 路径的 round 由 D22 持久化；没有 Execution 的 consumer 使用自己的稳定请求记录 ID。Logical call 同一次输入及重试身份不变；语义新输入/压缩/用户 retry 则新 logical_call_id。`cancellation` 是服务内信号，不能序列化成模型权限；ResolvedModel 的配置/参数/稳定 credential_ref 不因 live config 改动更换。

ResolveModelInTx 只读取 D09 canonical metadata、验证选择并经 D04 secret contract 的 AcquireCredentialLeaseInTx 保存引用保护，与 preparation input 共用外层 Tx；不访问 Provider、解密/发送 Credential 或建 SDK 连接。普通 ResolveModel wrapper 只供事务外调用并自行建立短 Tx；InTx 路径不得调用 wrapper 另开 Tx。lease_owner 的稳定 ID 参与去重，preparing 重试不重复泄漏 lease。

Model lease 保护 credential_ref 的存在及删除保留，不固定明文或 Secret value 版本。实际每次请求在事务外经 ReadCredentialForRequest 取该稳定引用的当前值；同一 Secret 轮换后，运行中 Execution 后续模型 turn 使用新值，已发出的请求不热换。SecretMaterial 仅临时内存供 Provider 适配器使用，不入 Snapshot/Transcript/日志。MCP 的 credential provider binding 与 retained shadow 继续按本文件末节的自身 lease/rotation 规则处理，不能套用 Model lease 推导其凭据行为。

ModelMessage/ModelResultPart/ModelTool 属于 model contract；D22 适配器将 ToolContent 转换成它们，Model 不导入更高层 Tool contract。双方都只引用 object contract 中的受控业务 file identity，不把签名 URL 当模型内容。

`ModelConsumer` 由受信任调用者构造：Agent 在会议发言仍 kind=agent，可带 meeting_id；会议辅助摘要是 kind=meeting、execution_id 为空。`approval_auto` 使用 kind=tool 关联 operation/execution，不重复记录为 Agent generation。四类非 chat 模型通过明确能力端口：

```text
Embed(ctx, ModelConsumer, ResolvedModel, logical_call_id, texts[]) -> Result<Embeddings>
Rerank(ctx, ModelConsumer, ResolvedModel, logical_call_id, query, candidates[])
  -> Result<RankedItems>
GenerateImage(ctx, ModelConsumer, ResolvedModel, logical_call_id, image_request)
  -> Result<GeneratedFiles>
```

embedding/reranker/image_generation 只用 System selection；Memory 用已定系统 chat selector，不能把任意 project chat 配成这些能力。D09 开工规格补齐各 native profile 的参数/schema/限制和 provider conformance，不能让一个万能 JSON 方法绕过模型能力校验。

## Model Stream、重试与计量

```text
ModelFrame {logical_call_id, invocation_id, attempt_index: int,
            sequence: Sequence, kind, payload}
kind/payload = attempt_started {started_at}
             | message_start {message_id}
             | text_delta {part_index, offset_utf8, text}
             | reasoning_delta {part_index, safe_projection_delta}
             | tool_call_start {call_id, name}
             | tool_call_delta {call_id, fragment}
             | tool_call_end {call_id, complete_arguments: JSON}
             | usage_update {Usage}
             | attempt_aborted {ModelError, partial: bool}
             | message_end {ModelResponse}
             | call_failed {ModelError}
             | call_cancelled {reason}
ModelError {category, code?, safe_message, retryable,
            provider_request_id?, dispatched: bool, partial_output: bool}
Usage {input_tokens?, output_tokens?, total_tokens?, cached_input_tokens?,
       cache_write_tokens?, reasoning_tokens?, source: provider|unknown}
```

frame sequence 只在一个 invocation 内递增，attempt_index 从 1 开始；每个真实 Provider attempt 有新的 invocation_id。logical stream 以 message_end/call_failed/call_cancelled 恰一终结；重试前发 attempt_aborted，新的 attempt_started 建立新内容基线。Provider TCP/HTTP stream EOF 没有正常协议结束标记时是 error/unknown，不伪造 message_end。

完整 Tool Call 只有获当前 attempt 的成功 terminal response 后才交给 Runtime 执行；tool_call_end 用于组装/显示，不提前派发。partial 参数不执行；abort 后不会把该 attempt 的文字/工具片段与下一 attempt 拼成一个完整回答。D22 可保留安全的中断投影与 notice；Canonical Transcript 明确 attempt/abort 归属，不把未完成工具调用组成可重放副作用。

D09 Model System 是同一 Agent logical call 的唯一自动 Provider 请求 retry owner。SDK 隐式重试必须关闭或纳入同一 attempt observer，不得形成不可见第二层。Agent 路径采用渐进单请求 timeout，达到上限后仍按既定 active/cancel 条件重试，不引入统一固定次数/Execution 总期限。认证、配置、不支持等非 retryable 错误直接返回；Loop 不根据 retryable 再复制本次调用。每轮 watchdog 由 Loop 取消本 logical call、保留安全中断证据并用新输入进入下一轮，不由 Adapter 偷换成新 round。

ModelError.category 固定为 `invalid_request,authentication,permission,model_not_found,rate_limited,context_too_large,provider_unavailable,timeout,network,cancelled,content_filter,unsupported_feature,provider_error,unknown`。finish_reason 固定 `stop,tool_calls,length,content_filter,cancelled,error,unknown`；未知值不能默认 stop。

其他 consumer 的 finite policy 由对应模块明确预算；自动审批不叠加应用层 retry/备用模型，有限请求期限内允许 Adapter 透明网络 retry，失败回人工等待。Meeting 摘要有限生成/校验重试耗尽仍 finalizing。取消优先于下一次 attempt，已经发送但结果无法确认则 Invocation status=unknown，不假装未发出。

```text
ModelInvocation {invocation_id, logical_call_id, attempt_index, consumer,
                 provider_id?, model_config_id?, resolved_model_identity_snapshot,
                 provider_request_id?, dispatched_at?, finished_at?,
                 status: succeeded|failed|cancelled|unknown, usage: Usage}
RecordInvocation(ctx, invocation) -> error
FinalizeInvocation(ctx, invocation_id, final_status, provider_usage?) -> error
```

每次实际发出请求前保留 attempt reservation，实际发出后记录 dispatched_at；未发出 reservation 不伪计成真实调用。失败/取消 attempt 仍计 Provider 报告的可靠 usage；无报告为 null/unknown，不估算 0，total 不与包含的 cached/reasoning 子字段重复相加。finalize 按 invocation_id 幂等，不为 Meeting Agent 发言重复写两条 usage。Provider/Model 删除只置空 live 引用，保留快照；Project 永久删除按项目数据矩阵清理。

## ToolSpec、Binding 与可信调用

```text
ToolSpec {tool_id, stable_key, name, description, spec_revision,
          input_schema: JSONSchema, output_schema?: JSONSchema,
          annotations?: {read_only: bool, destructive: bool,
                         idempotency: inherent|keyed|none|unknown}}
ToolBinding = builtin {handler_id, contract_revision}
            | runner {operation_name, operation_revision, protocol_major}
            | mcp {config_id, connection_id, remote_tool_name,
                   negotiated_protocol, shadow_lease_id}
RegisteredTool {spec_ref: {tool_id,spec_revision}, binding,
                scope_resolver_id, risk_classifier_id}
ExecutionTool {tool_id, spec_revision, model_visible_name, binding_snapshot}
ToolSnapshotSelection {allowed_tool_ids[], denied_tool_ids[], scope_constraints[]}
ResolveExecutionToolsInTx(ctx, tx, project_id, agent_id, execution_id,
                          selection: ToolSnapshotSelection) -> Result<ExecutionTool[]>
ToolCall {logical_call_id, invocation_id, call_id, model_visible_name, arguments: JSON}
TrustedToolContext {project_id, agent_id, execution_id, round_id, snapshot_id,
                    input_binding_id, actor: AgentRun, cancellation}
ExecuteTool(ctx, TrustedToolContext, ToolCall) -> Result<ToolOutcome>
ToolOutcome = complete {result: ToolResult}
            | suspended {operation_id, waiting_reference: WaitRef}
WaitRef {kind: approval|decision, request_id, operation_id, round_id}
```

WaitRef 是 tool contract 中的中立持久交互引用，只有稳定 ID/判别字段；execution contract 复用此类型并定义登记/消费接口。Tool 不 import Executor、Governance 或 Meeting 实现；来源判定由注册端口完成。

ResolveExecutionToolsInTx 读取 D18 的 Registry/immutable ToolSpec metadata 和正式 binding 引用，固定模型名称映射、所选 revision 与对应 lease；MCP 通过下文 AcquireRuntimeBindingInTx 窄端口取得已持久的 binding/shadow，不在此发起 connect/discovery。selection 由 D22 将可信 Capability/Policy 转换为 Tool 自有 DTO，包含既定 Core Tool 规则，不 import ExecutionPolicy。所有 InTx 方法只操作本域 metadata/ref 并使用同一外层 Tx，普通事务外解析 wrapper 不得从此路径调用。

ToolSpec 只含定义/三个 annotation，不塞 backend 地址、credential、在线状态、retry profile、统一 timeout。immutable `(tool_id,spec_revision)` 历史保留；模型可见名称在 Execution 中一一固定，反解失败 `tool_not_found`，不任意透传到 Backend。`stable_key` 使用 `builtin:<name>`、`runner:<name>`、`mcp:<config_id>:<remote_name>`，DB identity 仍 UUIDv7；remote name 按 MCP 标识原样编码，不靠大小写合并身份。

Registry/Projection 将 canonical JSON Schema 转成 Provider 支持子集；不能靠删 required/放松约束伪造兼容。不支持的 schema/ref 返回 `SCHEMA_UNSUPPORTED`，D09/D18/D20 联合 conformance；Provider 投影和 Runtime 原 schema 都校验。参数不合法不调用 Backend，修正参数是新的 Model Tool Call/Operation。

一次已成功模型响应中的 `(execution_id, logical_call_id, invocation_id, call_id)` 唯一对应 Operation；checkpoint 恢复同一输入不重复建 Operation，模型新调用即使参数完全一致仍新 Operation。fingerprint 采用 canonical-v1，覆盖 tool_id/spec_revision/canonical_arguments/binding identity/解析后的真实目标资源与可信 project/agent/execution；不含 attempt_id、临时连接或 credential 明文。

## Operation、Attempt 与结果

```text
ToolOperation {operation_id, execution_id, round_id, invocation_id, call_id,
               tool_id, spec_revision, binding_snapshot, canonical_arguments,
               fingerprint, state, approval_request_id?, suspension_ref?: WaitRef,
               native_deadline_at?, cancellation_requested_at?, result_ref?, version}
ToolAttempt {attempt_id, operation_id, attempt_number, backend_request_id?,
             state, started_at?, completed_at?, outcome: success|error|cancelled|unknown,
             error?, backend_idempotency_key?}
ToolResult {operation_id, status: success|error|cancelled,
            content: ToolContent[], structured_data?: JSON, artifacts: ArtifactRef[],
            error?: ToolError, truncated: bool, safe_metadata?}
ToolContent = text {text} | image_ref {file_ref, media_type} | file_ref {file_ref, media_type}
ToolError {category, code?, safe_message, retryable: bool, outcome_known: bool,
           safe_details?: JSON}
BackendExecute(ctx, execution_tool, trusted_context, operation, attempt)
  -> Result<BackendResult>
BackendResult = success {content, structured_data?, artifacts[], control?}
              | error {ToolError} | cancelled {ToolError?} | unknown {ToolError}
```

Operation/Attempt 分别独立持久化，不将 attempts 存 JSON 数组。`control=wait {WaitRef}` 只允许正式注册的内置交互适配器产生；MCP/Runner 任意 JSON 不可控制 Executor。Decision 产生成功的 Backend 创建事实后 Operation 保持 running 并持久 suspension_ref，未有最终 ToolResult；审批等待使用 waiting_for_approval。恢复决议后完成同 Operation、向 Transcript 注入一次结果。

unknown 映射 ToolResult.status=error、category=unknown_outcome、outcome_known=false；failed Operation 的未知副作用仍明确展示。`ToolError.category` 为 `invalid_arguments,tool_not_found,authorization_denied,approval_required,business_rule_violation,capability_unsupported,not_found,conflict,rate_limited,timeout,cancelled,network,backend_unavailable,backend_protocol_error,backend_contract_violation,unknown_outcome,internal_error,unknown`。处于真正 waiting 的 Operation 使用 suspended，不先提交 error 终态再尝试复活。

输出声明 output_schema 时必须验证 structured_data，违反为 backend_contract_violation，不能当 success。大输出用 D05 外部化后返回业务 file ref/截断标记；safe_message 不能直接等于原始 stderr/HTTP body/异常堆栈。

只有 Runtime 统一 retry policy 同时满足技术可重试、Operation active、权限仍有效、fingerprint 不变，以及已知安全 outcome 或可验证幂等能力时才新增 Attempt。backend key 固定由 operation_id 派生；one-time Approval 沿用本 Operation，但不证明 retry 安全。unknown+non-idempotent 禁止自动重发；Runner 协议不能在重连时私自 replay。

## 授权、人工等待与取消

```text
OperationAuthInput {trusted_context, operation_id, fingerprint, tool_id,
                    spec_revision, canonical_arguments, resolved_target}
AuthorizeTool(ctx, OperationAuthInput) -> Result<AuthorizationDecision>
AuthorizationDecision = allow {grant_ref?}
                      | deny {ToolError}
                      | waiting {approval_request_id, waiting_reference: WaitRef}
ScopeResolver.Resolve(ctx, OperationAuthInput) -> Result<ResolvedScope>
ResolvedScope {kind, schema_version, data: JSON, supports_reusable: bool,
               safe_description}
ScopeResolver.Matches(saved_scope, current_scope) -> Result<bool>
DecideApproval(ctx, Human, meta, approval_request_id,
               decision: approve_once|approve_reusable|reject)
  -> Result<ApprovalDecisionFact>
RevokeApproval(ctx, Human, meta, approval_id) -> Result<ApprovalGrant>
```

Authorize 内部使用短 Tx 校验基础权限/Project/真实目标，读取匹配 grant 或执行固定 policy；需要模型策略时结束 Tx 后调用 Model，再开短 Tx 重验输入/fingerprint/生命周期后持久化结论。不能持数据库事务等待用户/Provider。并发同 Operation 请求唯一化，返回同 ApprovalRequest，不能重复建待办。

批准/拒绝锁 Project、Agent、Execution、Operation、Approval；Owner/expected_version/source/fingerprint 当前仍有效才提交 request 决议、grant、Outbox。刚批准的当前 Operation 直接消费该决议，不重新跑 Approval Match/Policy；仍做执行前当前基础权限/生命周期与 Backend 最终校验。Reusable scope 必须 ScopeResolver 从真实调用生成，模型/客户端不能扩大；不支持 reusable 时拒绝该动作。

取消原因固定 `user_stop,execution_cancelled,project_archiving,project_deleting,meeting_deleting,source_unavailable,resource_invalid`。显式停止映射 request.cancelled，来源/资源失效映射 invalidated，并保留 reason；没有自动 expired。已决议历史不改造成取消，终态 Execution 只禁止迟到决议被消费；失效来源的 pending request 与 Inbox 通过 canonical 状态收敛。resolved-before-waiting 与重复通知见[等待契约](execution-orchestration.md#等待登记与恢复)。

Tool 原生可选 timeout 继续留在它自己的 arguments 中。D16/D18 首期命令/进程等待适配可声明 `timeout_ms?: integer [1,2147483647]`，null/0/负数均 invalid_arguments；省略表示未选择该工具期限。具体支持工具在正式 ToolSpec 明列，MCP 原生同名字段完全保留第三方含义，不增加全工具 wrapper/`__runtime` 字段。

支持期限的适配器在首次 Backend 开始之前持久化 native_deadline_at，审批等待不计时；技术 retry 只使用剩余时间，不刷新期限，yield/wait-output 不等于重置。Runner request deadline 为原期限与适配器本次内部 request bound 的较早者，内部 timeout 不能伪装成业务已停止。cancel/timeout 尽力向下传播并查询实际结果；unknown 继续按副作用安全规则处理。单 Tool timeout 不直接结束整个 Execution。

## Runner 与 MCP 通信边界

依据：[Runner Control](../../../architecture/runner/control-protocol.md)、[Data Channel](../../../architecture/runner/data-channel.md)、[Tunnel](../../../architecture/runner/desktop-tunnel.md)、[MCP Lifecycle](../../../architecture/mcp-integration/mcp-server-config-lifecycle.md)。

```text
RunnerEnvelope {protocol_version: {major: 1, minor: 0}, type,
                message_id, request_id?, operation_id?, timestamp, payload}
RunnerRequest {request_id, operation_id, execution_id, project_id, agent_id,
               mount: {mount_id, workspace_id}, operation_name, operation_revision,
               deadline?: Instant, environment?: map<string,string>,
               idempotency_key?, payload}
RunnerResponse {request_id, operation_id, outcome: success|failure|cancelled|unknown,
                code?, safe_message?, payload?}
RunnerCancel {request_id, operation_id, reason}
```

Runner 主动出站 WSS、设备 enrollment/Ed25519 身份按 D15；major 不一致拒绝连接，minor/feature 在 hello 协商，未知必须字段/operation 拒绝。operation schema 与控制 envelope 独立版本。D15 固定帧大小/心跳/backpressure/挑战签名字节，D17 固定二进制帧和传输参数；这些不进入 Central 业务契约。

runnerprotocol 的 ID/时间为自身 wire string DTO，独立验证 UUIDv7/RFC 3339 格式；Central adapter 显式转换 foundation/领域类型。environment 是协议自有敏感字符串映射，不导入 Central TrustedEnvironment/Actor/Scope 类型；Runner 不依赖任何 Central 包。

每实际 RPC attempt 新 request_id，同 Operation 保留 operation_id；message_id 每消息独立，不混用 HTTP trace ID。Runner 只用已注册 Mount/workspace logical ID 解析物理路径，不接 Central 任意宿主绝对路径。环境 Secret 只在受信请求内存使用，不写普通日志/持久配置。cancel ack 不是 terminal response，断连后的已发出未返回请求为 unknown；新连接不自动 resend 旧 request。

`RunnerDirectory.ResolveMount(ctx, actor, project_id, agent_id, mount_id)` 返回合法身份/能力与独立 online 信息；offline 是调用错误，不删配置。`TunnelAuthorize(ctx, Human, tunnel_id, request_kind: http|websocket_upgrade)` 由 D17 逐请求调用 Session/Owner/Project/Tunnel 生命周期端口。对外 URL/路由由 Central 管理，借 Runner 反向通道代理；失效 Session/项目门禁关闭既有长连接，不由 Runner 自行发布永久 URL。

```text
MCPRuntimeBinding {config_id, connection_id, binding_revision, negotiated_protocol,
                   endpoint_ref, credential_provider_ref, shadow_lease_id}
AcquireRuntimeBindingInTx(ctx, tx, project_id, connection_id, execution_id,
                          expected_binding_revision) -> Result<MCPRuntimeBinding>
AcquireRuntimeBinding(ctx, project_id, connection_id, execution_id,
                      expected_binding_revision) -> Result<MCPRuntimeBinding>
InvokeMCP(ctx, lease, remote_name, args, cancellation) -> Result<BackendResult>
```

MCP Config 为配置目录，Project Connection 拥有认证、发现和凭据。上述 binding DTO/窄端口由 tool contract 定义、D20 适配实现，防止 Tool 与 MCP 实现包互引。AcquireRuntimeBindingInTx 只校验本域已持久的协议/Connection/binding revision、创建或复用 shadow/credential provider lease 与引用；不做网络握手、认证请求、discovery 或 SDK 连接。普通 wrapper 在事务外开短 Tx 并委托 InTx，不能在 preparation Tx 中另开事务。外部连接/环境准备在捕获提交后执行。

暂时离线不移除已固定 Tool；成功 discovery 不兼容变化不得静默替换旧 revision。disable/disconnect/delete 阻止新 lease，旧 Execution 用 retained runtime shadow，release 后再清材料。MCP Credential rotation 由 Connection revalidate，为 future binding 使用新凭据；旧 Execution 能否继续由其已有 credential provider binding 的有效性决定，不由 Model 的按稳定引用取当前值规则代替。Core resource list/read 仍逐调用校验项目/资源且转换为受控对象，不向模型暴露 transport credential。
