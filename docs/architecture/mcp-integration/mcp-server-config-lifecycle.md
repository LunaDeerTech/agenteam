# MCP Server Config / Connection Lifecycle 详细设计

> 状态：初版设计稿
>
> 上层架构：
> - [MCP 集成架构](./index.md)
>
> 相关详细设计：
> - [MCP Protocol Runtime](./mcp-protocol-runtime.md)
> - [MCP Tool Discovery](./mcp-tool-discovery.md)
> - [MCP Tool Execution](./mcp-tool-execution.md)
> - [MCP Resource Adapter](./mcp-resource-adapter.md)

本文定义 MCP Config、Project MCP Connection、Credential Binding 及其控制面生命周期。

## 1. 核心对象

MCP 集成固定区分：

~~~text
MCP Config
= 可复用的服务连接定义 / Catalog object

MCP Connection
= 某个 Project 对一个 MCP Config 建立的实际使用关系

Credential Binding
= 该 Project Connection 使用的认证材料引用

Protocol Runtime State
= 临时网络 / subscription / legacy session 等运行时状态
~~~

这些对象不能合并成一个 `MCP Server` 数据结构。

## 2. MCP Config

概念模型：

~~~text
McpConfig
├── id
├── scope
│   ├── system
│   └── project
├── project_id?          # scope=project
├── name
├── description?
├── endpoint
├── auth_profile
├── enabled?             # Catalog 层是否允许新连接
├── created_at
├── updated_at
└── revision
~~~

### 2.1 System Config

System MCP Config：

- 由系统管理员维护；
- 对所有 Project 可见；
- 不持有 Project Credential；
- Project 必须显式 Connect 才能建立 MCP Connection。

### 2.2 Project Config

Project MCP Config：

- 只属于一个 Project；
- 只对所属 Project 可见；
- 同样需要显式 Connect；
- Config 与 Connection 仍然保持分离。

## 3. Endpoint

MCP Integration 只支持 Central 后台可通过网络访问的 Streamable HTTP MCP，因此 MCP Config 不再保留 transport discriminator 或 transport-specific config。

MCP Config 直接保存：

~~~text
endpoint
~~~

`endpoint` 表示该 MCP Server 的 Streamable HTTP endpoint。

MCP Config 不提供任意 `custom_headers` 配置。需要通过自定义 Header 发送静态 Credential 时，由 Authentication Profile 的 `custom_header` scheme 明确定义，避免把任意请求 Header 注入能力暴露为普通网络配置。

## 4. Authentication Profile

MCP Config 描述“这个服务支持 / 要求什么认证方式”，但不保存某个 Project 的 credential。

固定抽象：

~~~text
AuthProfile
├── none
├── static_credential
│   └── scheme
│       ├── bearer
│       ├── basic
│       └── custom_header
│           └── header_name
└── oauth
~~~

具体 Credential 绑定属于 MCP Connection。

### 4.1 Static Credential

Project Connect 时选择当前 Project 可访问的 Secret / Credential source。

Static Credential 用于用户持有的 Access Token / API Key 等长期 Credential。

MCP Config 只描述 Credential 的发送方式，不保存 plaintext：

~~~text
bearer
-> Authorization: Bearer <credential>

basic
-> Authorization: Basic <credential>

custom_header
-> <header_name>: <credential>
~~~

其中：

- `bearer`：使用标准 Bearer Authorization Header；
- `basic`：使用标准 Basic Authorization Header；
- `custom_header`：由 MCP Config 显式定义 `header_name`，Credential 作为该 Header 的 value 发送；
- Header name / scheme 属于 Config；
- 实际 Credential 通过 Project MCP Connection 的 Credential Binding 保存和解析。

第一阶段不把 Basic 进一步拆成 username / password 两个独立业务字段；它只表示按 HTTP Basic 形式发送当前绑定的静态 Credential。

### 4.2 OAuth

OAuth 按 MCP Authorization 所采用的标准 OAuth 2.1 流程实现，不设计 agenteam 私有 OAuth 变体。

OAuth 的 authorization server discovery、authorization code / token exchange、refresh token、issuer validation、client metadata / registration compatibility 等通过官方 MCP SDK 与统一 Credential Provider 管理。

MCP Connection 只保存安全引用，不直接保存 token plaintext。

Protocol Runtime 只消费 Credential Provider 提供的有效 access credential，Project / Connection domain model 不直接依赖 MCP SDK 的 OAuth API。

## 5. MCP Connection

概念模型：

~~~text
McpConnection
├── id
├── project_id
├── mcp_config_id
├── config_revision_at_connect?
├── state
├── enabled
├── auth_state
├── credential_binding_id?
├── capability_state
├── last_connected_at?
├── last_error_code?
├── created_at
└── updated_at
~~~

同一个 Project 对同一个 MCP Config **最多只能存在一个 MCP Connection**。

这是长期约束，不只是第一阶段限制。数据库层应保证等价于：

~~~text
UNIQUE(project_id, mcp_config_id)
~~~

agenteam 不计划支持同一个 Project + MCP Config 下的多账户 Connection，因为这会让 Tool identity、Credential ownership、Agent Capability 与 Audit 都产生额外歧义。

如果用户需要为同一个远端 MCP 服务连接多个账户，应创建多个不同名称的 MCP Config，例如：

~~~text
GitHub MCP - Work
GitHub MCP - Personal
~~~

每个 Config 分别建立自己的唯一 Connection。

## 6. Connection State

建议把“是否存在连接”和“是否允许用于新 Execution”分开。

控制面状态：

~~~text
unconnected
connecting
connected
auth_required
error
~~~

运行启用状态：

~~~text
enabled = true / false
~~~

这样 authentication failure 不需要复用 `disabled` 表达。

概念：

~~~mermaid
stateDiagram-v2
    [*] --> Unconnected
    Unconnected --> Connecting: Connect
    Connecting --> Connected: authentication success
    Connecting --> AuthRequired: interactive auth required
    Connecting --> Error: connection / protocol failure
    AuthRequired --> Connecting: credential supplied
    Error --> Connecting: retry
    Connected --> Connecting: reconnect / credential change
    Connected --> Unconnected: Disconnect
~~~

`connected` 只表示当前 MCP Connection 的认证已经建立成功，不表示 Tool / Resource capability 已经同步成功，也不保证当前存在可用 Tool / Resource。

各 Capability Adapter 独立维护自己的同步状态；同步失败本身不进入 Connection error 状态，只记录 Audit / diagnostics。

`enabled` 只在 connected Connection 上决定该 Connection 是否有资格向当前 Tool Registry 注册 Tool。Backend 的临时在线 / 健康状态不进入统一 Tool availability 层；实际调用失败由 MCP Backend 在执行时返回。

## 7. Connect

Connect 是控制面操作，不等于建立一个长期 MCP protocol session。

推荐流程：

~~~text
Project chooses MCP Config
-> validate visibility / permission
-> create or reuse the unique MCP Connection
-> resolve authentication flow
-> authentication success
-> mark connection connected
-> Protocol Runtime compatibility check
-> initial Tool Discovery
-> initial Resource Catalog Discovery
~~~

`connected` 的判定只依赖认证成功，不等待 `tools/list` 成功。

Tool / Resource capability 是否曾成功同步由各 Adapter 的独立 metadata 表达；失败 attempt 不改变上一版成功状态。

### 7.1 Initial Capability Discovery Failure

如果认证已经成功，但 initial Tool / Resource discovery 某一项失败：

- Connection 保持 `connected`；
- 失败只影响对应 Capability Adapter；
- Tool Discovery 失败不影响 Resource Catalog；
- Resource Discovery 失败不影响 Tool Registry；
- 如果此前没有成功同步，则对应能力保持空状态；
- 如果此前已有成功同步，则继续保留上一版成功数据；
- 只记录本次 failure 的 Audit / diagnostics；
- 管理员可以 manual refresh。

不能因为某个 Capability discovery 失败把认证成功的 Connection 降回 unconnected，也不能因为 refresh failure 改写其他 Adapter 或上一版成功状态。

## 8. Enable / Disable

Disable：

- 保留 MCP Connection；
- 保留 Credential Binding；
- 保留各 Capability Adapter 最近一次成功同步数据；
- 从当前 Tool Registry 移除该 Connection 的 Tool registration；
- 阻止其 Tool 进入新的 Execution Tool Set；
- 不主动取消已有 Agent Execution；
- 可以停止非必要 subscription / refresh runtime resource。

Enable：

- 校验 Credential 仍可用；
- 触发 protocol check / Tool Discovery / Resource Catalog refresh；
- refresh 成功后把最新成功 Tool snapshot 注册回当前 Registry。

如果 enable refresh 失败，则不把该 Connection 的旧 Tool snapshot 重新注册到当前 Registry；旧 snapshot 只作为历史同步数据保留。

## 9. Disconnect

Disconnect 表示 Project 主动解除对 Config 的实际连接。

行为：

- Connection 不再参与新的 Execution；
- 停止 runtime subscription / connection resource；
- Credential Binding 与 Connection 的关联解除；
- 从当前 Tool Registry 移除对应 Project Connection 的 Tool registration；
- Agent Capability 的 stable tool reference 不删除；
- 已运行 Execution 不主动中断。

Credential 对象本身是否删除取决于它是否是专属于该 Connection 的 managed credential。

共享 Project Secret 不能因为 Disconnect 被删除。

## 10. Delete MCP Config

MCP Config 采用 **hard delete + retained runtime shadow**。

控制面删除时：

- 物理删除 MCP Config；
- 物理删除对应 Project MCP Connection；
- 阻止新的 Connect；
- 新 Execution 不再获得其 Tool；
- Agent Capability stable reference 保留；
- 从当前 Tool Registry 移除对应 Tool registration；
- stable identity、历史 ToolSpec revision 与 Agent Capability 引用保留；
- 已运行 Execution 不主动中断。

已经运行的 Execution 不依赖 live Config / Connection 继续解析，而是使用在 Execution 建立时保留的 runtime binding / shadow。

retained runtime shadow 只保存继续执行真正需要的运行时信息，例如：

- endpoint；
- remote tool name；
- protocol binding metadata；
- credential provider reference / lease；
- 其他必要 execution-scoped backend binding。

它不是一个新的长期 McpConfig / McpConnection 业务对象，也不保存 Credential plaintext。

最后一个引用该 shadow 的 Execution 结束后即可清理。

## 11. Runtime Lease

为了兑现“运行中 Execution 不被控制面变化主动撤销”，需要 runtime lease / retained binding。

概念：

~~~text
Execution starts
-> snapshot ExecutionTool
-> acquire MCP runtime binding lease

Config disable / disconnect
-> blocks new lease
-> existing lease remains valid until Execution ends

Config delete
-> live Config / Connection hard delete
-> existing Execution continues through retained runtime shadow
~~~

runtime binding 至少保证：

- ExecutionTool 能继续解析到正确 remote Tool；
- 必要 endpoint / protocol binding 不被提前物理删除；
- 必要 credential 可以在 lease 生命周期内安全使用。

Execution 结束后释放 lease，后台清理可以删除无引用的 Connection runtime material。

这不是要求保存 credential plaintext snapshot。

## 12. Config Update

Config 更新分两类。

### 12.1 Non-runtime metadata

例如：

- display name；
- description。

不影响 Connection。

### 12.2 Runtime-affecting change

例如：

- endpoint；
- auth profile。

这类更新：

1. bump Config revision；
2. 现有 Connection 标记 `needs_reconnect` 或等价状态；
3. 阻止旧 binding 进入新的 Execution；
4. 重新完成 protocol check / auth / capability discovery；
5. 成功后形成新的 runtime binding。

运行中的 Execution 继续使用已有 lease。

## 13. Credential Rotation

Credential rotation 不应要求重建 Agent Capability。

~~~text
Credential changed
-> Connection revalidate
-> Protocol Runtime uses new credential for future binding
-> Tool identity unchanged
~~~

如果 rotation 导致认证失败：

- Connection auth state 更新；
- 新 Execution 不再使用；
- 已运行 Execution 的处理取决于其 runtime lease 是否仍拥有有效 credential provider binding。

## 14. Capability Sync State

Connection 只保存 / 聚合各 Capability Adapter 的同步状态，不用一个 Tool-specific discovery 字段代表所有 MCP capability。

第一阶段至少包含：

~~~text
capability_state
├── tools
│   ├── state: never | refreshing | ready
│   ├── last_successful_at?
│   ├── discovered_tool_count
│   └── discovery_revision
└── resources
    ├── state: never | refreshing | ready
    ├── last_successful_at?
    ├── discovered_resource_count
    ├── discovered_template_count
    └── catalog_revision
~~~

任一 Adapter sync attempt 失败时：

- 对应 `ready` 保持 `ready`；
- 对应 `never` 保持 `never`；
- 不更新成功 sync metadata；
- 不影响其他 Capability Adapter；
- 失败信息只进入 Audit / diagnostics。

Tool 的具体 diff 规则见 [MCP Tool Discovery](./mcp-tool-discovery.md)。

Resource Catalog / Template / read 规则见 [MCP Resource Adapter](./mcp-resource-adapter.md)。

## 15. Background Reconciliation

Central 启动后应对 enabled MCP Connection 做 reconciliation，而不是假设数据库状态等于实时状态。

建议：

~~~text
load enabled MCP Connections
-> validate Config / credential binding
-> initialize protocol runtime binding
-> refresh Tool Discovery when needed
-> refresh Resource Catalog when needed
-> update connection diagnostics
~~~

reconciliation 失败只影响对应 Connection，不应阻塞整个 Central 启动。

## 16. Security / Network Validation

Project MCP endpoint 属于外部网络输入，必须统一经过平台 Outbound Network Policy。

MCP 模块只遵循该平台能力，不自行维护第二套 SSRF / private-network 规则。

对 MCP 来说固定要求是：

- Endpoint 必须通过统一 Outbound Network Policy；
- redirect 的每一个目标都重新执行同样的校验；
- Credential 不跨 origin 自动转发；
- response size、connect/read timeout、TLS validation 使用平台统一限制。

完整的 URL validation、DNS / IP classification、loopback / link-local / cloud metadata、private CIDR、redirect、TLS 与平台部署策略见 [Outbound Network Policy 详细设计](../platform-infrastructure/outbound-network-policy.md)。

## 17. Audit

控制面操作应记录 Audit：

- Config create / update / delete；
- Connect；
- authentication state change；
- enable / disable；
- Disconnect；
- credential binding change；
- manual refresh。

Audit 不记录 token plaintext。

## 18. 第一阶段建议

第一阶段实现：

1. System / Project Config；
2. `McpConfig.endpoint`；
3. 每个 Project / Config 唯一 MCP Connection；
4. none / static credential / OAuth 2.1；
5. Static Credential 支持 Bearer / Basic / Custom Header；
6. Streamable HTTP；
7. authentication 成功即进入 connected；
8. Capability Adapter 使用独立 sync state；
9. initial Tool Discovery + Resource Catalog Discovery；
10. enable / disable / disconnect；
11. Config runtime revision；
12. hard delete + retained runtime shadow；
13. runtime binding lease；
14. startup reconciliation；
15. 统一 Outbound Network Policy。
