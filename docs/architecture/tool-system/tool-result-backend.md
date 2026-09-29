# Tool Result & Backend 详细设计

> 状态：设计稿
>
> 上层架构：
> - [统一工具系统架构](./index.md)
> > 总体设计：
> > - [Unified Tool Runtime 详细设计](./tool-runtime.md)
> > 相关详细设计：
> > - [Object Storage 详细设计](../platform-infrastructure/object-storage.md)
> > - [Artifact Builtin Tools 详细设计](./artifact-tools.md)
> > - [MCP Tool 默认启用策略详细设计](../mcp-integration/mcp-tool-default-enable.md)

本分册描述 Tool 执行结果、错误与 Runtime Failure 的统一表示，以及 Builtin、Runner、MCP Backend 如何映射到统一 Backend Contract。

## 1. Unified Tool Result

Tool Result 需要同时满足：

- 给 Agent Loop / Model 使用；
- 给 UI / Execution Log 使用；
- 支持结构化结果；
- 支持大体积 Artifact；
- 不强迫所有 Backend 都返回纯文本。

建议：

~~~text
ToolResult
├── operation_id
├── status
│   ├── success
│   ├── error
│   └── cancelled
├── content[]
├── structured_data?
├── artifacts[]
├── error?
├── metadata?
└── backend_metadata?
~~~

### 1.1 content

content 是可回填 Model Context 的内容块。

第一阶段固定只支持：

~~~text
text
image_ref
file_ref
~~~

统一语义：

- `text`：直接回填模型的文本内容；
- `image_ref`：图片对象引用；
- `file_ref`：通用文件对象引用，可承载 document、audio、binary 等非图片文件。

Runtime 不要求把所有 Backend 原生 content type 原样暴露给模型。Backend-specific 类型统一在 Executor / Adapter 层映射：

~~~text
Backend text
-> text

Backend image
-> image_ref

Backend audio / document / binary resource
-> file_ref

Backend structured content
-> ToolResult.structured_data

Backend large / persistent resource
-> ToolArtifactRef / StoredObject
~~~

因此第一阶段不单独增加 `audio_ref`、`document_ref`、`resource` 等 content variant；这类差异通过 `file_ref + media_type`、`structured_data` 或统一 Object Storage 表达。

如果未来出现无法合理映射的新模型输入类型，再扩展新的 content part。

### 1.2 structured_data

用于机器可读结果。

如果 ExecutionTool 声明 output_schema，则 structured_data 应符合对应 snapshot schema。

Model Adapter 是否把 structured_data 直接发送给 Provider，取决于统一 Model Contract 的 projection；Tool Runtime 不依赖某个 Model Provider 的 Tool Result 格式。

### 1.3 artifacts

大体积或需要持久化的输出不应直接塞进 Model Context。

~~~text
ToolArtifactRef
├── artifact_id
├── stored_object_id
├── kind
├── name?
└── model_summary?
~~~

例如：

- 生成文件；
- 大型 command output；
- 图片；
- 下载内容；
- 大型 MCP resource result。

Tool Artifact 保留自己的业务语义，但实际 payload 统一保存到平台 `StoredObject`，并通过 `stored_object_id` 引用。

~~~text
ToolArtifactRef
-> stored_object_id
-> StoredObject
-> ObjectStorageService
-> MinIO
~~~

Tool Runtime 不直接访问 MinIO，也不保存 bucket / object key。media type、size、checksum 等底层对象 metadata 由 StoredObject 提供。完整对象存储设计见 [Object Storage 详细设计](../platform-infrastructure/object-storage.md)。

模型只接收必要摘要和可进一步读取的 Artifact / StoredObject 引用。

Agent 后续需要列举、读取或把已有对象整理成明确命名 Artifact 时，通过 Artifact Builtin Tools 完成：

~~~text
list-artifacts
create-artifact
read-artifact
~~~

ToolResult 中的 `file_ref / image_ref` 不是 Signed URL。

用户侧预览 / 下载时，由 UI / API 根据 Artifact / 业务引用重新执行权限校验，再通过 ObjectStorageService 生成短期 Signed URL。

### 1.4 metadata

metadata 是平台内部、可安全持久化的通用执行 metadata，例如：

- duration；
- backend latency；
- truncation info；
- result size。

不直接向模型暴露所有 metadata。

### 1.5 backend_metadata

用于保留必要的 Backend-specific 调试信息，例如：

- MCP request id；
- Runner request id；
- remote status code。

必须经过安全过滤。

Backend metadata 不应成为 Agent Loop 的业务判断依赖。

## 2. Tool Error

Tool Error 需要标准化。

建议第一阶段至少包括：

~~~text
ToolError
├── category
├── code?
├── safe_message
├── retryable
├── outcome_known
├── details?
└── backend_code?
~~~

建议统一 category：

~~~text
invalid_arguments
tool_not_found
authorization_denied
approval_required
business_rule_violation
capability_unsupported
not_found
conflict
rate_limited
timeout
cancelled
network
backend_unavailable
backend_protocol_error
backend_contract_violation
unknown_outcome
internal_error
unknown
~~~

### 2.1 safe_message

safe_message 是允许：

- 写入普通 Execution Log；
- 返回给 Agent Model；

的安全文本。

Backend 原始 exception / stderr / HTTP body 不自动成为 safe_message。

### 2.2 retryable

retryable 只是 Backend / Runtime 对“技术上是否可能重试”的信号。

它不是最终 retry 决策。

最终自动 retry 仍必须结合：

- idempotency；
- outcome_known；
- operation 当前状态；
- Security / Governance 的 retry 约束。

### 2.3 outcome_known

这是 Tool Runtime 很重要的统一字段：

~~~text
outcome_known = true
-> Runtime 能确定 Backend 没有成功产生不可逆副作用
   或已经明确得到成功 / 失败结果

outcome_known = false
-> Backend 可能已经执行，但 Runtime 无法确认
~~~

典型：

~~~text
请求发送前连接失败
-> outcome_known = true

请求发送后连接断开
-> 可能 unknown outcome
~~~

对于：

~~~text
unknown_outcome + non-idempotent
~~~

不能自动 retry。

该原则与现有 One-time Approval / Retry 设计保持一致。

## 3. 哪些错误返回给模型

默认原则：

### 可作为 Tool Result 返回

例如：

- invalid_arguments；
- business_rule_violation；
- not_found；
- conflict；
- authorization_denied；
- backend_unavailable；
- 明确的 timeout；
- Backend 返回的安全业务错误。

这样 Agent 可以：

- 修正参数；
- 查询其他资源；
- 重新规划；
- 向用户解释。

### 默认不应伪装成普通业务结果

例如：

- backend_contract_violation；
- Runtime invariant violation；
- Tool identity 映射损坏；
- 数据完整性错误。

这些属于平台执行异常。

对于 `backend_contract_violation`，Tool Runtime 不构造普通 Tool Result 回填模型，而是返回结构化 Runtime Failure，明确表示：

> 当前 Backend 返回无法满足 Unified Tool Runtime contract，因此这次调用不能形成可信 Tool Result。

Tool Runtime 不在这一层直接决定终止整个 Agent Execution，也不把这种问题伪装成可由模型修正参数的普通 Tool Error。

后续是终止 Agent Execution、执行平台级 retry，还是进入其他 Runtime recovery 流程，由 Agent Loop / Executor 的错误策略决定。

## 4. Result Size 与 Truncation

Tool Runtime 必须避免把无限大结果直接加入模型上下文。

建议提供统一 result size policy：

~~~text
small result
-> inline content

large result
-> persist ToolArtifact + StoredObject
-> inline summary / preview + artifact_ref
~~~

截断必须显式记录：

~~~text
metadata.truncated = true
metadata.original_size = ...
~~~

不能静默截断后让模型误以为结果完整。

具体阈值可在实现阶段配置。

## 5. Sensitive Data

Tool Runtime 是 Secret 泄漏控制的一层，但不是唯一安全边界。

要求：

- Tool arguments 中不应包含由平台展开后的 Secret plaintext；
- Backend resolve 的 Secret 不写入普通 Tool Operation；
- safe_message 做脱敏；
- Tool Result 在进入 Model Context 前执行已知 Secret masking；
- backend_metadata 不保存 Authorization header；
- Artifact 如果包含敏感数据，需要按 Project 权限保护；
- Audit 只引用 Operation / Tool / outcome 等 metadata，不复制完整 Tool input/output。

具体 Secret 与 Audit 规则沿用 Security / Governance。

## 6. Backend Contract

每个 Tool Executor / Adapter 最终都需要实现等价于：

~~~text
ToolBackend.execute(
    execution_tool,
    operation,
    attempt,
    cancellation
) -> BackendResult
~~~

BackendResult 必须能够明确表达：

~~~text
success
error
cancelled
unknown_outcome
~~~

并提供足够信息让 Runtime 构造统一：

- ToolResult；
- ToolError；
- retry classification；
- backend metadata。

Backend 不直接把结果写回模型。

## 7. Builtin Backend

Builtin Tool 通常直接调用 agenteam Domain / Platform Service。

它仍然必须通过 Unified Runtime。

Backend 负责：

- 调用对应 service；
- 把 domain error 映射为 Tool Error；
- 把业务实体转换为 Tool Result；
- 避免泄漏内部 stack / database error。

Tool Runtime 不直接访问业务数据库绕过 Domain Service。

## 8. Runner Backend

Runner Tool Backend：

~~~text
Tool Runtime
-> Runner Executor
-> Runner RPC
-> Runner
~~~

Runner Executor 负责：

- 把 Unified Operation 转换为 Runner RPC；
- 保持 operation / attempt correlation；
- 传播 timeout / cancellation；
- 标准化 Runner offline / timeout / protocol error；
- 把大文件、stdout 等转换为 Tool Result / Artifact。

Runner 本地只执行协议完整性、Workspace / path containment 与实际 Capability 校验，不维护另一套 Tool Authorization。

## 9. MCP Backend

MCP Tool Backend：

~~~text
Tool Runtime
-> MCP Executor
-> MCP Client
-> MCP Server
~~~

MCP Executor 负责：

- 根据 ExecutionTool snapshot 找到 MCP backend binding；
- 把 canonical arguments 转换为 MCP tools/call；
- 映射 MCP content / structuredContent；
- 处理 MCP protocol error 与 Tool error；
- 传播 cancellation / timeout；
- 返回标准 BackendResult。

MCP Protocol revision、discovery、schema compatibility 和 transport lifecycle 不属于本文，放入 MCP detailed design。
