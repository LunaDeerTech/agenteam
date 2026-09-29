# MCP Protocol Runtime 详细设计

> 状态：初版设计稿
>
> 上层架构：
> - [MCP 集成架构](./index.md)
>
> 相关详细设计：
> - [MCP Server Config / Connection Lifecycle](./mcp-server-config-lifecycle.md)
> - [MCP Tool Discovery](./mcp-tool-discovery.md)
> - [MCP Tool Execution](./mcp-tool-execution.md)
> - [MCP Resource Adapter](./mcp-resource-adapter.md)
> - [Unified Tool Runtime](../tool-system/tool-runtime.md)

本文定义 MCP Bridge 内部的 Protocol Runtime。它负责 agenteam 与外部 MCP Server 之间的协议通信，不负责把 MCP capability 映射成 agenteam 业务模型。

## 1. 设计目标

MCP Protocol Runtime 的目标是把 MCP 协议细节隔离在 MCP Bridge 内部，使上层 Capability Adapter 只面对稳定的内部协议接口。

~~~text
Capability Adapter
    |
    | Protocol Runtime API
    v
MCP Protocol Runtime
    |
    | MCP protocol
    v
External MCP Server
~~~

Protocol Runtime 负责：

- protocol revision / compatibility；
- transport；
- authentication 注入；
- request / response；
- server capability discovery；
- notification / subscription；
- cancellation / progress；
- multi-round-trip protocol exchange 的基础承载；
- protocol error normalization；
- observability / correlation。

Protocol Runtime 不负责：

- Agent Capability；
- Tool Authorization / Approval；
- ToolSpec / ToolResult 业务映射；
- MCP Tool 是否默认启用；
- Project MCP Connection 的控制面生命周期；
- Tools / Resources / Prompts / Elicitation 等 capability 的业务映射。

## 2. 协议基线

第一阶段以 MCP `2026-07-28` 为主协议基线。

该 revision 的核心特征是：

- protocol core 为 stateless；
- 不再依赖旧的 `initialize / initialized` handshake；
- 不再把 `Mcp-Session-Id` 作为现代协议请求的基础；
- 每个请求携带 protocol version、client identity 与 client capabilities；
- 可通过 `server/discover` 获取 Server capability；
- list change 等通知通过显式 `subscriptions/listen` 流订阅；
- `tools/list` 等 list result 可以携带 cache metadata；
- Tool schema 使用完整 JSON Schema 2020-12 能力。

因此 agenteam 内部不能把“一个 MCP Connection”错误实现成“一个长期 MCP protocol session”。

### 2.1 Compatibility Profile

第一阶段固定：

| 项目 | 第一阶段 |
| --- | --- |
| MCP 2026-07-28 | 支持，主路径 |
| MCP 2025-11-25 | 支持，兼容路径 |
| 更早 revision | 不保证支持 |
| Streamable HTTP | 支持 |
| Legacy HTTP + SSE | 不主动支持 |
| tools | 支持 |
| resources | 支持 |
| prompts | 不接入 |
| elicitation | 不接入 |
| tasks extension | 不接入 |
| deprecated roots / sampling / logging | 不接入 |

这里的“兼容”指 Protocol Runtime 可以与对应旧版 Server 完成协议通信，不代表 agenteam 会接入旧版协议暴露的全部 capability。

### 2.2 Version Negotiation

Protocol Runtime 应把版本协商封装在内部。

固定策略：

1. 优先采用官方 Go MCP SDK 的 discovery / negotiation 能力；
2. 优先协商 `2026-07-28`；
3. Server 仅支持 `2025-11-25` 时，由 SDK / Protocol Runtime 自动回退到兼容 lifecycle；
4. Adapter 不感知最终 negotiated revision；
5. negotiated revision 记录在 runtime metadata / diagnostics；
6. 不兼容的 revision 返回明确 protocol compatibility error。

不得把 version branching 扩散到 MCP Tool Adapter 或 Unified Tool Runtime。

### 2.3 官方 Go MCP SDK

Protocol Runtime 第一阶段优先基于官方 Go MCP SDK 实现，而不是自行维护 MCP wire protocol。

~~~text
MCP Protocol Runtime
-> official Go MCP SDK
-> negotiated MCP protocol
~~~

SDK 负责：

- `2026-07-28` / `2025-11-25` protocol negotiation；
- modern `server/discover` 与 legacy `initialize` lifecycle 差异；
- Streamable HTTP transport；
- MCP request / response codec；
- protocol-level cancellation / progress 等基础能力。

agenteam 在 SDK 外仍保留自己的 Protocol Runtime wrapper，用于：

- MCP Connection / Credential Binding 接入；
- Security / outbound network policy；
- observability；
- error normalization；
- capability Adapter 边界；
- 避免业务层直接依赖 SDK API。

## 3. Runtime 组件

逻辑组件建议为：

~~~mermaid
flowchart LR
    Adapter["Capability Adapter"]
    API["Protocol Runtime API"]
    Client["MCP Client"]
    Compat["Protocol Compatibility"]
    Transport["Transport"]
    Auth["Auth Injector"]
    Events["Subscription / Event<br/>Future"]
    Obs["Observability"]
    Server["External MCP Server"]

    Adapter --> API
    API --> Client
    Client --> Compat
    Compat --> Transport
    Auth --> Transport
    Events -.-> Client
    Obs -.-> Client
    Transport <--> Server
~~~

这些是逻辑职责，不要求拆成独立进程或独立 package。

## 4. Protocol Runtime API

Capability Adapter 不直接持有底层 HTTP client、session id 或 SDK transport 对象。

概念接口：

~~~text
ProtocolRuntime.discover(connection_ref)
ProtocolRuntime.request(connection_ref, method, params, options)
ProtocolRuntime.cancel(request_ref)
~~~

第一阶段 Tool / Resource Adapter 实际需要的最小能力：

~~~text
discover server capability
tools/list
tools/call
resources/list
resources/templates/list
resources/read
propagate cancellation
receive progress
~~~

未来接入 MCP change subscription 时，再扩展：

~~~text
ProtocolRuntime.subscribe(connection_ref, filter)
~~~

`connection_ref` 指向 Project MCP Connection 的运行时 binding，不等同于底层协议 session。

## 5. Streamable HTTP

MCP Integration 只处理 Central 可通过网络访问的 Streamable HTTP MCP。

Protocol Runtime 负责：

- 使用 MCP Config 中的 endpoint；
- required MCP headers；
- content negotiation；
- 根据 Authentication Profile 注入 MCP Credential；
- response parsing；
- streaming response；
- subscription stream；
- connection pooling。

底层 HTTP 请求必须使用平台统一的受控 Outbound HTTP Client / Transport：

~~~text
MCP Protocol Runtime
-> Outbound HTTP Client
-> Outbound Network Policy
-> MCP Server
~~~

URL / DNS / private network / redirect / TLS / timeout / response guardrail 由平台 Outbound Network Policy 统一处理，Protocol Runtime 不自行维护第二套网络安全逻辑。

HTTP connection pooling 属于普通网络资源复用，不具有 MCP 业务身份语义。

### 5.1 2026-07-28 请求

现代请求应由 SDK / Protocol Runtime 正确携带：

- protocol revision；
- MCP method；
- method-specific name / uri 等标准 header；
- client information；
- client capability；
- trace metadata。

这些字段不由 Tool Adapter 手工拼接。

### 5.2 旧版 session compatibility

如果兼容的 2025 Server 使用 session：

- session 建立与 session id 维护全部由 Protocol Runtime 负责；
- session id 不进入 MCP Connection 持久化业务模型；
- session 丢失时由 Protocol Runtime 执行兼容恢复；
- Adapter 只看到 request 成功或标准化失败。

## 6. Server Discovery 与 Capabilities

Protocol Runtime 可以执行 protocol-level server discovery，用于确认：

- negotiated revision；
- Server identity；
- Server capabilities；
- extensions；
- protocol-level optional behavior。

Protocol-level capability discovery 不等于 MCP Tool Discovery。

~~~text
server/discover
-> Server protocol capabilities

tools/list
-> concrete MCP Tool definitions
~~~

两者必须分开。

第一阶段只消费 Tool 集成所需 capability，其余 capability 可以记录用于 diagnostics，但不会自动映射到 agenteam 子系统。

## 7. Notification / Subscription

第一阶段不实现 MCP Tool / Resource change subscription。

Tool Discovery / Resource Catalog 的持续刷新第一阶段采用：

~~~text
Central startup refresh
manual refresh
~~~

Connect / Enable 等需要建立可用 Connection 的流程仍可执行必要的即时 discovery，但不维护长期 change subscription。

`subscriptions/listen` 及旧版 notification compatibility 作为后续扩展。

未来接入时仍应遵循：

- notification 只作为 refresh trigger；
- notification 本身不直接修改 Tool Registry / Resource Catalog；
- event 丢失不能破坏 Registry 正确性；
- subscription 失败不删除已有 ToolSpec / Resource Catalog。

## 8. Cache Metadata

现代 MCP list result 可以包含 TTL / cache scope。

Protocol Runtime 可以把这些 metadata 传给 Discovery 层，但不直接决定 Tool Registry 生命周期。

第一阶段原则：

- remote TTL 可以记录到 diagnostics / discovery metadata；
- 第一阶段不根据 TTL 自动调度 refresh；
- TTL 过期不自动从 Tool Registry 移除 Tool；
- agenteam 自己的持久化 discovery 状态仍是 Tool Registry 的事实来源。

后续如果增加自动 refresh，再考虑使用 remote TTL 作为调度提示。

## 9. Cancellation

Unified Tool Runtime 的 cancellation signal 可以传递到 MCP Protocol Runtime。

~~~text
Execution / Tool Runtime cancellation
-> MCP Tool Adapter
-> Protocol Runtime
-> cancel active request / stop waiting
~~~

Protocol Runtime 必须区分：

- 本地停止等待；
- 协议级取消已经发送；
- Server 已确认取消；
- outcome 无法确认。

发送 cancel 不代表远端一定没有产生副作用。

最终 `outcome_known` 与 retry 判断由 MCP Tool Execution 映射到 Unified BackendResult。

## 10. Progress

如果 Server 提供 progress：

- Protocol Runtime 负责接收；
- 以内部 progress event 向 Adapter / Tool Runtime 转发；
- progress 不是 Tool 最终结果；
- progress 中的任意文本仍按不可信外部内容处理；
- 第一阶段不要求把所有 progress 实时写入 Model Context。

UI / Execution Log 是否展示 progress，由 Unified Tool Runtime / Execution logging policy 决定。

## 11. Multi-round-trip 与 Server-originated Request

MCP 现代协议允许某些 capability 在一个请求处理中发生多轮交互。

Protocol Runtime 应具备协议层承载能力，但第一阶段只有已经接入的 Capability Adapter 才能响应对应语义。

第一阶段不接入：

- Elicitation；
- Sampling；
- Tasks extension；
- 其他需要业务层处理的 server-originated capability。

如果 Server 在 `tools/call` 中要求 agenteam 未声明 / 未实现的 capability：

- 不伪造响应；
- 不把请求直接透传给 Agent Loop；
- 返回明确 capability unsupported / protocol failure；
- 记录安全的 diagnostics。

未来支持相关 capability 时，应增加对应 Adapter，而不是把业务逻辑塞进 Protocol Runtime。

## 12. Authentication 接口

Protocol Runtime 只消费已经解析好的 Connection Credential Binding。

~~~text
MCP Connection
-> Credential Binding
-> secure resolve
-> Protocol Runtime Auth Injector
-> outbound MCP request
~~~

Protocol Runtime 不决定：

- 哪个 Project 可以绑定哪个 Secret；
- OAuth 登录 UI；
- Credential 的控制面创建 / 删除；
- Agent Tool Authorization。

这些属于 MCP Connection Lifecycle 与 Security / Governance。

第一阶段支持：

~~~text
No Authentication

Static Credential
└── scheme
    ├── bearer
    ├── basic
    └── custom_header

OAuth
~~~

Static Credential 用于 Access Token / API Key 等长期 Credential：

- `bearer` -> `Authorization: Bearer <credential>`；
- `basic` -> `Authorization: Basic <credential>`；
- `custom_header` -> 使用 MCP Config 中显式定义的 `header_name`。

MCP Config 不支持普通用途的任意 `custom_headers`。Credential plaintext 通过 Connection Credential Binding 在请求时解析。

OAuth 按 MCP Authorization 所采用的标准 OAuth 2.1 流程实现，不设计 agenteam 私有 OAuth 变体。OAuth client lifecycle、authorization server discovery、authorization code / token exchange、refresh token、issuer validation 等由 Credential Provider 与官方 SDK 能力承载。

即使采用官方 Go MCP SDK，OAuth 仍通过 agenteam 自己的 Credential Provider abstraction 与业务模型隔离，避免 MCP SDK API 直接进入 Project / Connection domain model。

要求：

- credential plaintext 不进入普通 Execution Log；
- Authorization / Cookie 等敏感 header 不进入 backend metadata；
- redirect 时不得无条件跨 origin 转发 credential；
- refresh token / access token rotation 必须通过 credential provider abstraction 完成。

## 13. Error Model

Protocol Runtime 把底层错误归类为内部 protocol outcome，供 Capability Adapter 进一步映射。

至少区分：

~~~text
transport_error
authentication_error
protocol_version_unsupported
protocol_error
invalid_response
server_unavailable
rate_limited
timeout
cancelled
subscription_error
capability_unsupported
~~~

Protocol Runtime 不直接生成 Unified ToolError，因为它也会被未来非 Tool Adapter 使用。

### 13.1 Request Delivery 与 outcome

Runtime 尽量保留以下事实：

- request 是否确定未发送；
- request 是否已经发送；
- 是否收到明确 MCP response；
- transport 是否在 response 前中断。

这使 MCP Tool Adapter 能判断 Tool Operation 的 `outcome_known`。

## 14. Timeout 与 Retry

Protocol Runtime 自身不实现独立的 Tool retry policy。

- network / protocol retry classification 可以由 Runtime 提供；
- 是否真的 retry 由 Unified Tool Runtime 决定；
- Protocol Runtime 可以对纯协议维护行为进行内部恢复；
- `tools/call` 不因为网络异常被 Protocol Runtime 私自重放。

这样可以避免绕过 Unified Tool Runtime 的 idempotency / approval / attempt 语义。

## 15. Observability

至少记录安全 metadata：

- MCP connection id；
- config id；
- negotiated revision；
- method；
- request correlation id；
- duration；
- HTTP status / protocol error code；
- retry / reconnect diagnostics；

不得默认记录：

- Authorization header；
- credential plaintext；
- 完整 Tool arguments；
- 完整 Tool Result；
- OAuth token。

完整 payload 是否持久化由对应业务层 logging policy 决定。

## 16. Runtime 生命周期

MCP Protocol Runtime 自身是 Central 内的共享运行时服务。

对 2026-07-28：

- 不要求为每个 MCP Connection 常驻一个 protocol session；
- 普通 request 可以按需发送并复用 HTTP transport；
- 只有未来 subscription 等确实需要长连接的能力才维护长生命周期 runtime resource。

对兼容旧版 Server：

- 可以内部维护 session；
- session 生命周期不改变 MCP Connection 的持久化生命周期。

因此：

~~~text
MCP Connection lifetime
!= HTTP connection lifetime
!= future subscription lifetime
!= legacy MCP session lifetime
~~~

## 17. 第一阶段实现边界

第一阶段实现：

1. MCP 2026-07-28 主路径；
2. MCP 2025-11-25 兼容路径；
3. Streamable HTTP；
4. protocol/server discovery；
5. tools/list / tools/call；
6. resources/list / resources/templates/list / resources/read；
7. startup / manual discovery refresh 所需协议能力；
8. cancellation / progress 基础承载；
9. Access Token / API Key credential injection；
10. 标准 OAuth；
11. protocol error classification；
12. trace / request correlation。

第一阶段不实现：

- legacy HTTP+SSE；
- Tool / Resource change subscription / compatible notification；
- Prompts Adapter；
- Elicitation Adapter；
- MCP Tasks extension；
- deprecated Roots / Sampling / Logging capability。
