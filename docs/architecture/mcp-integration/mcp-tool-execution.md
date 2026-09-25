# MCP Tool Execution 详细设计

> 状态：初版设计稿
>
> 上层架构：
> - [MCP 集成架构](./index.md)
>
> 相关详细设计：
> - [MCP Protocol Runtime](./mcp-protocol-runtime.md)
> - [MCP Tool Discovery](./mcp-tool-discovery.md)
> - [MCP Resource Adapter](./mcp-resource-adapter.md)
> - [Tool Execution & Operation](../tool-system/tool-execution.md)
> - [Tool Result & Backend](../tool-system/tool-result-backend.md)

本文定义 Unified Tool Runtime 如何通过 MCP Tool Adapter / Backend 执行 `tools/call`，以及 MCP result / error 如何映射到统一 Backend Contract。

## 1. 设计边界

MCP Tool Execution 只处理 MCP 特有执行适配。

~~~text
Unified Tool Runtime
-> MCP Tool Backend
-> MCP Protocol Runtime
-> External MCP Server
~~~

它不重新定义：

- ToolOperation；
- ToolAttempt；
- Approval；
- timeout policy；
- retry policy；
- idempotency policy；
- ToolResult / ToolError 公共结构。

这些全部复用 Unified Tool Runtime。

## 2. Execution Binding

Execution 创建时已经固定 `ExecutionTool.binding_snapshot`。

MCP binding snapshot 至少需要逻辑上包含：

~~~text
McpExecutionBinding
├── project_id
├── mcp_connection_id / retained runtime binding ref
├── mcp_config_id
├── remote_tool_name
├── spec_revision
└── protocol/runtime binding metadata
~~~

Credential plaintext 不进入 snapshot。

Connection Config 后续 disable / disconnect / delete 不应让 Runtime 偷偷切换到另一个 Connection。

## 3. 调用链

~~~mermaid
sequenceDiagram
    participant L as Agent Loop
    participant R as Unified Tool Runtime
    participant G as Security / Governance
    participant M as MCP Tool Backend
    participant P as MCP Protocol Runtime
    participant S as MCP Server

    L->>R: Tool Call
    R->>R: resolve ExecutionTool + validate args
    R->>G: authorize operation
    G-->>R: allow
    R->>M: execute(operation, attempt)
    M->>P: tools/call(remote name, args)
    P->>S: MCP request
    S-->>P: MCP response
    P-->>M: protocol outcome
    M-->>R: BackendResult
    R-->>L: Unified ToolResult / Runtime Failure
~~~

MCP Tool Backend 不直接把 MCP payload 写回模型。

## 4. Request Mapping

输入来自 Unified Tool Runtime 已完成 canonical validation 的 arguments。

~~~text
ToolOperation.canonical_arguments
+ ExecutionTool binding
    ↓
MCP Tool Backend
    ↓
tools/call
{
  name: remote_tool_name,
  arguments: canonical_arguments
}
~~~

MCP Backend 不重新猜测或修复模型 arguments。

如果 negotiated protocol / Tool schema 存在 protocol-specific request metadata，由 Protocol Runtime / MCP SDK 注入。

### 4.1 x-mcp-header

如果当前 MCP revision 支持 schema 驱动的 MCP request header mirror：

- 优先由官方 SDK / Protocol Runtime 实现；
- MCP Tool Backend 不手工维护第二份 header projection；
- credential header 与 Tool argument header 必须保持安全边界；
- reserved MCP / auth header 不允许被 Tool argument 覆盖。

## 5. Request Correlation

每个 ToolAttempt 应能与 MCP request 关联。

建议 metadata：

~~~text
operation_id
attempt_id
mcp_connection_id
remote_tool_name
protocol_revision
mcp_request_id?
trace_id?
~~~

这些用于 diagnostics / Audit correlation，不作为业务结果返回模型。

## 6. Success Result Mapping

MCP `tools/call` 成功 result 需要映射为 Unified ToolResult。

核心字段：

~~~text
MCP CallToolResult
├── content[]
├── structuredContent?
├── isError?
└── metadata?
        ↓
MCP Tool Backend
        ↓
BackendResult
        ↓
Unified ToolResult
~~~

### 6.1 Text

MCP text content：

~~~text
MCP text
-> ToolResult.content.text
~~~

仍需要经过统一 size / sensitive data policy。

### 6.2 Image

MCP `ImageContent` 是直接内联在 MCP Tool Result 中的 base64 图片内容。

~~~text
MCP image bytes
-> decode base64
-> ObjectStorageService
-> StoredObject
-> image_ref
~~~

MCP binary content 不因为体积较小建立 inline 例外。图片 canonical payload 统一进入 Object Storage，Model Context 只接收 `image_ref` 与必要 metadata。

### 6.3 Audio / Binary / Embedded Resource

直接 audio / binary content：

~~~text
audio / binary
-> decode
-> ObjectStorageService
-> StoredObject
-> file_ref + media_type
~~~

`EmbeddedResource` 具有正式 MCP Resource 语义，统一交给 MCP Resource Adapter：

~~~text
EmbeddedResource
-> MCP Resource Adapter
-> normalize / materialize
-> ObjectStorageService
-> StoredObject
-> file_ref / image_ref / text projection
~~~

即使 EmbeddedResource 是小型文本，它的 canonical Resource 内容也统一保存为 StoredObject；inline text 只作为 Agent / Model projection。

### 6.4 Resource Link

`ResourceLink` 是指向同一个 MCP Server Resource namespace 的 Resource 引用，不是普通 HTTP URL。

第一阶段固定通过一等 MCP Resource Adapter 解析：

~~~text
ResourceLink
-> MCP Resource Adapter
-> resources/read(link.uri)
-> Resource Materialization
-> ObjectStorageService / StoredObject
-> ToolResult file_ref / image_ref / text projection
~~~

ResourceLink 必须绑定到产生该 Tool Result 的同一个 MCP Connection。

Tool Adapter 不直接下载 Resource URI，也不自己实现 `resources/read`。

如果 Resource materialization 失败：

- 原始 `tools/call` 的 Tool-level outcome 仍按其真实 response 判定；
- 不伪造 file_ref / image_ref；
- ToolResult 返回安全的 Resource 读取失败说明；
- failure 记录到 Audit / diagnostics。

## 7. structuredContent / outputSchema

如果 MCP result 返回 `structuredContent`：

~~~text
structuredContent
-> ToolResult.structured_data
~~~

MCP 2026-07-28 允许 structured content 为任意 JSON value，不能假定一定是 object。

如果对应 ExecutionTool snapshot 声明 `output_schema`：

1. 使用 snapshot 的 output schema 校验；
2. validation success -> 正常 structured_data；
3. validation failure -> `backend_contract_violation`；
4. 不把不符合 schema 的结果伪装成 success。

如果没有 output_schema，structuredContent 仍可保留，但 Runtime 无法宣称完成 schema contract validation。

## 8. MCP isError

MCP `isError = true` 表示 Tool 调用形成了 Tool-level error result，而不是 JSON-RPC transport/protocol failure。

映射原则：

- `outcome_known = true`；
- 不把它归类为 network / protocol error；
- 从 content 中提取安全 error message；
- 映射为最合适的 Unified ToolError category；
- 无法通过结构化信息可靠判断业务类别时，统一 fallback 为 `unknown`；
- `retryable` 默认 false，除非存在明确、安全的错误分类依据。

不通过文本关键词猜测 retryability。

## 9. Protocol Error

JSON-RPC / MCP protocol error 与 Tool-level `isError` 分开。

例如：

~~~text
invalid request
method error
header mismatch
invalid response
protocol version failure
~~~

映射为：

~~~text
backend_protocol_error
backend_contract_violation
invalid_arguments
...
~~~

具体 category 取决于错误发生位置。

Protocol Runtime 提供原始安全分类，MCP Tool Backend 转成 Unified BackendResult。

## 10. HTTP / Transport Error

建议映射：

~~~text
request definitely not sent
-> network / backend_unavailable
-> outcome_known = true

request sent, no response / connection lost
-> network / timeout
-> outcome_known = false

explicit remote response says not executed
-> corresponding error
-> outcome_known = true
~~~

Backend 必须尽量使用 Protocol Runtime 提供的 delivery metadata，而不是仅根据 HTTP status 猜测。

## 11. Timeout

Unified Tool Runtime 产生 operation timeout / cancellation signal。

MCP Tool Backend：

1. 传给 Protocol Runtime；
2. 停止等待；
3. 尝试协议级 cancellation；
4. 根据 request delivery / server acknowledgement 构造 outcome。

Timeout 不自动意味着：

~~~text
outcome_known = true
~~~

远端 Tool 可能已经执行。

## 12. Cancellation

Cancellation 规则沿用统一 Runtime。

如果 MCP Server 明确确认请求在执行前取消，可以形成：

~~~text
cancelled
outcome_known = true
~~~

如果只知道本地停止等待：

~~~text
cancelled / unknown_outcome
outcome_known = false
~~~

不能因发送了 cancel message 就直接假定无副作用。

## 13. Progress

Progress event 不作为 Tool Result。

可以通过内部 event：

~~~text
MCP progress
-> Protocol Runtime
-> MCP Tool Backend
-> Tool Runtime progress channel
~~~

第一阶段：

- UI / log 可以消费；
- 不默认把每条 progress 加入 Model Context；
- progress 丢失不影响最终 result correctness。

## 14. Multi-round-trip

第一阶段不接入 Elicitation / Tasks 等 capability。

如果一次 `tools/call` 需要 unsupported multi-round-trip capability：

- MCP Tool Backend 不与 Agent Loop 建立私有旁路；
- 不伪造用户输入；
- 返回普通 Unified ToolError：
  - `category = capability_unsupported`；
  - `retryable = false`；
  - `outcome_known = false`；
- 保留 safe diagnostics。

这是 Tool capability 不受当前 agenteam 支持，而不是 Runtime invariant 损坏，因此不归类为 Runtime Failure。

如果 Server 在 agenteam 未声明相应 client capability 的情况下违反协议强行发起不被允许的交互，则按 `backend_protocol_error` 处理。

未来增加对应 Capability Adapter 后，Tool Execution 再接入明确的 continuation contract。

## 15. Retry

MCP Tool Backend 不自行重放 `tools/call`。

~~~text
Attempt fails
-> MCP BackendResult
-> Unified Tool Runtime
-> retry decision
-> new ToolAttempt
-> MCP Backend executes again
~~~

这样：

- operation_id 不变；
- attempt_id 变化；
- Approval 不重复申请；
- idempotency key / retry safety 由统一 Runtime 控制。

Protocol Runtime 可以重连 subscription 等协议维护流，但不得把 Tool business call 的重放藏在内部。

## 16. Idempotency

第一阶段明确映射 MCP Tool annotation：

~~~text
readOnlyHint === true
-> idempotency = inherent

idempotentHint === true
-> idempotency = inherent

otherwise
-> idempotency = unknown
~~~

`readOnlyHint` 与 `idempotentHint` 只作为 MCP Server 对该 Tool 的语义声明，不直接决定 Authorization / Approval。

第一阶段不从 MCP annotation 推导 `keyed` idempotency，因为 MCP annotation 没有提供 idempotency key contract。

`idempotentHint === false` 或 annotation 缺失也不强制映射为 `none`；统一保守使用 `unknown`。

自动 retry 仍由 Unified Tool Runtime 综合 `idempotency`、`outcome_known`、Attempt 状态和统一 retry policy 决定。

## 17. Content Size / Artifact

所有 MCP result 服从统一 Tool Result size policy。

~~~text
small safe text
-> inline

large text / image / audio / binary
-> StoredObject / Artifact
-> inline preview / summary + ref
~~~

MCP Backend 不把无限大 response 全部塞入 PostgreSQL 或 Model Context。

Protocol Runtime 层也应设置 transport response size / streaming safety limit。

## 18. Sensitive Data

MCP result 属于外部不可信数据。

进入 Unified ToolResult 前至少：

- Secret masking；
- unsafe backend metadata filtering；
- 不回传 response header 中的 credential；
- 不把 raw OAuth / auth failure payload直接送给模型；
- Artifact 按 Project scope 存储。

MCP Server 返回的 prompt-like text 不获得任何更高信任级别，它只是 Tool output。

## 19. BackendResult Mapping

MCP Backend 最终只返回统一 Backend Contract。

成功：

~~~text
BackendResult.success
├── content
├── structured_data?
├── artifacts
└── backend_metadata
~~~

失败：

~~~text
BackendResult.error
├── category
├── safe_message
├── retryable
├── outcome_known
├── backend_code?
└── backend_metadata
~~~

Runtime Failure：

- MCP response 无法满足已声明 output contract；
- identity / binding invariant 损坏；
- Adapter 自身 contract violation。

这类不能伪装成普通业务 Tool Error。

## 20. Binding / Connection 变化

Execution 使用 retained runtime binding。

调用时：

- Config 已 disable：已有 Execution 仍按 lease 尝试执行；
- Config 已 delete：已有 Execution 仍解析 retained binding；
- Credential 已物理不可用：执行失败；
- Server Tool 在新 discovery 中消失：旧 Execution 不自动换 Tool，调用是否成功由远端真实结果决定。

任何情况下都不能因为旧 binding 不可用而静默路由到同名其他 MCP Tool。

## 21. Audit / Observability

至少关联：

- operation_id；
- attempt_id；
- stable tool id；
- spec_revision；
- MCP config / connection id；
- remote tool name；
- protocol revision；
- duration；
- result status；
- Tool-level error / protocol error 分类；
- outcome_known。

Audit 不复制完整 arguments / result payload。

## 22. 第一阶段实现边界

第一阶段支持：

1. canonical arguments -> `tools/call`；
2. text / image / audio 基础映射；
3. EmbeddedResource -> MCP Resource Adapter；
4. ResourceLink -> MCP Resource Adapter -> `resources/read`；
5. binary content -> ObjectStorageService / StoredObject；
6. MCP annotation -> Unified idempotency 映射；
7. `capability_unsupported` ToolError；
8. structuredContent；
9. outputSchema validation；
10. `isError`；
11. protocol / transport error mapping；
12. timeout / cancellation propagation；
13. progress forwarding；
14. unknown outcome；
15. Artifact / StoredObject；
16. Unified retry / attempt contract。

第一阶段不实现：

- Elicitation continuation；
- Tasks extension；
- MCP-specific hidden retry；
- MCP-specific keyed idempotency。
