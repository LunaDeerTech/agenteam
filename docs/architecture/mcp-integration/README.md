# MCP 集成架构

## 1. 定位

MCP 是 agenteam Tool System 的一种外部 Tool 来源。

agenteam 不把 MCP 作为 Agent Loop 的唯一工具协议，也不让模型通过一个通用 `call_mcp` Tool 间接访问所有 MCP Server。

MCP Server 的 capability 由 **MCP Bridge** 接入，再通过 capability-specific Adapter 映射到 agenteam 内部子系统。第一阶段接入 MCP Tools 与 MCP Resources。

~~~mermaid
flowchart LR
    Catalog["MCP Config Catalog<br/>System / Project"]
    Connection["Project MCP Connection"]
    Bridge["MCP Bridge"]
    Tools["Unified Tool Runtime"]
    Execution["Agent Execution"]
    Server["External MCP Server"]
    Secrets["Secret / Credential Management"]
    Governance["Security / Governance"]

    Catalog --> Connection
    Connection --> Bridge
    Bridge <--> Server
    Bridge --> Tools
    Tools --> Execution

    Secrets -.-> Connection
    Governance -.-> Tools
~~~

核心原则：

> Agent 看到的是 agenteam 内部的具体 Tool，而不是 MCP 协议本身。

详细设计：

- [MCP Protocol Runtime](./mcp-protocol-runtime.md)
- [MCP Server Config / Connection Lifecycle](./mcp-server-config-lifecycle.md)
- [MCP Tool Discovery](./mcp-tool-discovery.md)
- [MCP Tool Execution](./mcp-tool-execution.md)
- [MCP Resource Adapter](./mcp-resource-adapter.md)
- [MCP Tool 默认启用策略](./mcp-tool-default-enable.md)

## 2. MCP Config 与 MCP Connection

MCP 集成明确区分 **MCP Config** 与 **MCP Connection**。

`MCP Config` 是长期配置 / Catalog 对象，描述：

- MCP 服务是什么；
- Streamable HTTP endpoint；
- authentication profile。

`MCP Connection` 是某个 Project 对 MCP Config 建立的实际使用关系，描述：

- 所属 Project；
- 引用的 MCP Config；
- enabled / authentication / discovery 状态；
- Credential Binding。

~~~mermaid
flowchart TB
    SystemConfig["System MCP Config"]
    ProjectConfig["Project MCP Config"]

    ProjectA["Project A"]
    ProjectB["Project B"]

    ASystem["MCP Connection<br/>Project A"]
    BSystem["MCP Connection<br/>Project B"]
    AProject["MCP Connection<br/>Project A"]

    SystemConfig -->|"visible"| ProjectA
    SystemConfig -->|"visible"| ProjectB
    ProjectConfig -->|"project only"| ProjectA

    ProjectA -->|"Connect"| ASystem
    ProjectB -->|"Connect"| BSystem
    ProjectA -->|"Connect"| AProject
~~~

因此：

> MCP Config 不等于已经连接的 MCP Server。真正参与 Project 运行时的是 MCP Connection。

同一个 Project 对同一个 MCP Config 最多只能存在一个 MCP Connection。多账户需求通过创建多个独立 MCP Config 解决，不在同一个 Config 下建立多个 Connection。

### 2.1 System-scoped MCP Config

System MCP Config：

- 由系统管理员创建；
- 对所有 Project 可见；
- 不保存 Project Credential；
- Project 必须显式 Connect 才能建立自己的 MCP Connection。

### 2.2 Project-scoped MCP Config

Project MCP Config：

- 由 Project 创建；
- 只对所属 Project 可见；
- 同样需要显式 Connect；
- Connection / Authentication / Discovery 机制与 System Config 相同。

System / Project Config 的主要差异是配置可见范围，而不是运行时模型。

### 2.3 Credential Ownership

MCP Credential 属于 Project MCP Connection，而不是 System MCP Config。

~~~text
MCP Config
-> Project MCP Connection
-> Credential Binding
-> MCP Protocol Runtime
-> MCP Server
~~~

Credential 使用 Secret Reference 或统一 Credential Provider 安全保存。

Agent、Model 和 Agent Execution Context 不直接获得 MCP Credential plaintext。

Config / Connection 的完整生命周期、删除语义与运行中 Execution binding 保留机制见：

> [MCP Server Config / Connection Lifecycle](./mcp-server-config-lifecycle.md)

## 3. MCP Bridge

MCP Bridge 负责 MCP 协议接入与 capability integration。

它由两层逻辑组成：

~~~mermaid
flowchart LR
    Server["External MCP Server"]

    subgraph Bridge["MCP Bridge"]
        direction LR

        Protocol["MCP Protocol Runtime"]

        subgraph Adapters["Capability Adapters"]
            ToolAdapter["MCP Tool Adapter<br/>Phase 1"]
            ResourceAdapter["MCP Resource Adapter<br/>Phase 1"]
            FutureAdapter["MCP Prompt / Elicitation / ... Adapter<br/>Future"]
        end

        Protocol --> ToolAdapter
        Protocol --> ResourceAdapter
        Protocol -.-> FutureAdapter
    end

    ToolRuntime["Unified Tool Runtime"]
    ResourceCatalog["MCP Resource Catalog"]
    ObjectStorage["ObjectStorageService"]
    FutureSubsystem["Other agenteam Subsystem<br/>Future"]

    Server <--> Protocol
    ToolAdapter --> ToolRuntime
    ResourceAdapter --> ResourceCatalog
    ResourceAdapter --> ObjectStorage
    FutureAdapter -.-> FutureSubsystem
~~~

核心边界：

> Capability Adapter 不直接实现 MCP transport、authentication、protocol revision、request / response、notification 等协议行为。

这些统一由 MCP Protocol Runtime 承担。

### 3.1 MCP Protocol Runtime

MCP Protocol Runtime 是各 Capability Adapter 共用的协议底座。

它负责：

- MCP Client / transport；
- Authentication / Credential Binding 接入；
- protocol revision / compatibility；
- protocol-level lifecycle；
- Server capability discovery；
- request / response；
- notification / subscription；
- cancellation / progress；
- protocol error normalization。

Protocol Runtime 不负责把 MCP capability 映射成 agenteam 业务模型。

协议 revision、Streamable HTTP、现代 stateless protocol 与旧版兼容、subscription 等具体规则见：

> [MCP Protocol Runtime 详细设计](./mcp-protocol-runtime.md)

### 3.2 Capability Adapter

Capability Adapter 负责：

> MCP capability model <-> agenteam internal subsystem model

第一阶段：

~~~text
MCP Tool Adapter
-> Unified Tool Runtime

MCP Resource Adapter
-> MCP Resource Catalog
-> ObjectStorageService
~~~

未来支持 Prompts、Elicitation 等能力时，在 MCP Bridge 中增加对应 Adapter，而不是让业务模块分别实现 MCP Client。

### 3.3 MCP Tool Adapter

MCP Tool Adapter 负责：

~~~text
Definition path
MCP tools/list
-> Unified ToolSpec
-> Tool Registry

Execution path
Unified Tool Call
-> MCP tools/call
-> Unified ToolResult / ToolError
~~~

Adapter 不负责 Agent 权限决策。

### 3.4 MCP Resource Adapter

MCP Resource Adapter 与 MCP Tool Adapter 平级，负责 MCP Resources capability。

第一阶段支持：

~~~text
resources/list
resources/templates/list
resources/read
~~~

它维护 Project / MCP Connection scoped 的 Resource Catalog / Template Catalog，并把实际读取到的 Resource 内容统一保存为 StoredObject。

Agent 不直接调用 MCP protocol。Resource Adapter 对 Agent 提供 agenteam 内部的 Resource Catalog / Read 能力；Tool Result 中的 ResourceLink / EmbeddedResource 也统一交给 Resource Adapter 处理。

完整设计见：

> [MCP Resource Adapter 详细设计](./mcp-resource-adapter.md)

## 4. MCP Tool 与 Unified Tool Runtime

每一个 MCP Tool 都映射为一个独立的 Unified Tool。

例如：

~~~text
github-mcp
  -> search-code
  -> create-issue
  -> create-pull-request
~~~

进入 Unified Tool Runtime 后，MCP Tool 与 Builtin Tool / Runner Tool 统一使用：

- ToolSpec；
- ToolBinding；
- Tool Registry；
- Agent Capability；
- Execution Tool Set；
- ToolOperation / ToolAttempt；
- ToolResult / ToolError。

MCP 不建立第二套 Tool Runtime。

统一 Tool 设计见：

- [Unified Tool Runtime](../tool-system/tool-runtime.md)
- [Tool Definition & Registry](../tool-system/tool-definition-registry.md)
- [Tool Execution & Operation](../tool-system/tool-execution.md)
- [Tool Result & Backend](../tool-system/tool-result-backend.md)

## 5. Stable Tool Identity

Agent Capability 不能依赖 Tool 展示名称保存权限。

MCP Tool 使用稳定身份，概念上：

~~~text
mcp:<mcp_server_config_id>:<remote_tool_name>
~~~

或者等价的内部稳定 Tool ID。

Stable Tool ID 用于：

- Agent Capability；
- Tool Registry；
- Execution Tool Set；
- Approval / Audit；
- Tool Call routing。

Tool definition 变化通过 ToolSpec `spec_revision` 表达，不因 description / schema 更新自动改变 stable identity。

remote tool rename 第一阶段按 old removed + new added 处理。

## 6. Agent Capability 集成

MCP Tool 与其他 Tool 一样逐 Tool 授权。

~~~text
MCP
  github-mcp
    [x] search-code
    [x] create-issue
    [ ] create-pull-request
~~~

Agent Capability 保存 stable Tool ID，而不是复制 MCP schema 或 Connection 信息。

关系：

~~~text
MCP Config
-> MCP Connection
-> MCP Tool Discovery
-> Tool Registry
-> Agent Capability
-> Execution Tool Set
~~~

MCP Connection / Tool 当前是否仍在 Registry 中注册，与 Agent 的长期 Capability 引用是不同状态。Backend 暂时网络不可达不会改写 Registry，而是在实际调用时返回 Backend error。

## 7. Discovery 原则

MCP Tool Discovery 的长期原则：

- 以 Project MCP Connection 为实际 discovery source；
- `tools/list` 结果映射为 canonical Unified ToolSpec；
- 新 Tool 不自动加入已有 Agent Capability；
- 一次完整成功 discovery 确认 Tool 消失时，从当前 Registry 移除，但保留 stable identity、历史 ToolSpec 与 Agent Capability 引用；
- definition 变化形成新的 immutable ToolSpec revision；
- definition 变化不要求用户 reconfirmation；
- refresh failure 不修改上一版成功 Tool definition / Registry registration，只记录 Audit / diagnostics；
- 单个 invalid Tool 不影响同次 discovery 中其他有效 Tool；
- notification 只触发 discovery refresh，不直接旁路修改 Registry；
- 已运行 Execution 保留自己的 ToolSpec / binding snapshot。

完整 refresh、diff、atomic commit 与 schema 规则见：

> [MCP Tool Discovery 详细设计](./mcp-tool-discovery.md)

## 8. Tool 名称与冲突

Tool Registry 依赖 stable Tool ID 保证身份，不依赖 display name。

不同来源可以出现同名 Tool：

~~~text
Builtin: search
MCP A: search
MCP B: search
~~~

模型侧名称由 Unified Tool Runtime 在 Execution Tool Set 中生成无冲突 projection，并保存：

~~~text
model_visible_name
<-> stable_tool_id
~~~

Agent Capability 与 Audit 始终使用 stable Tool ID。

## 9. MCP Tool 执行

统一执行链：

~~~mermaid
sequenceDiagram
    participant A as Agent Loop
    participant R as Unified Tool Runtime
    participant G as Security / Governance
    participant B as MCP Tool Adapter
    participant P as MCP Protocol Runtime
    participant S as MCP Server

    A->>R: Unified Tool Call
    R->>G: authorize(operation)
    G-->>R: allow / approval / deny

    alt allowed
        R->>B: execute(operation, attempt)
        B->>P: tools/call
        P->>S: MCP request
        S-->>P: MCP response
        P-->>B: protocol outcome
        B-->>R: BackendResult
        R-->>A: Unified ToolResult / Failure
    end
~~~

MCP Server 不能直接绕过 Unified Tool Runtime 被 Agent Loop 调用。

MCP content、structuredContent、`isError`、transport error、timeout、cancellation、unknown outcome 等映射规则见：

> [MCP Tool Execution 详细设计](./mcp-tool-execution.md)

## 10. MCP Authentication 与 Tool Authorization

MCP Authentication 与 Tool Authorization 是两套独立机制。

~~~text
MCP Authentication
= Platform / Project -> MCP Server

Tool Authorization
= Agent / Execution -> MCP Tool
~~~

Connection authentication success 只表示当前 Project 可以访问对应 MCP Server，不代表任意 Agent 自动获得其 Tool。

Connection 的 `connected` 状态只表示认证成功；Tool Discovery 使用独立状态，不要求 `tools/list` 成功后才算 connected。

第一阶段 MCP Connection 支持：

- 无认证；
- Static Credential：
  - Bearer；
  - Basic；
  - Custom Header；
- 标准 OAuth 2.1。

MCP Tool 仍统一经过：

- Agent Capability；
- Execution Policy；
- Approval；
- Tool Authorization；
- Audit。

MCP Server 返回的 annotation 不直接成为 Security / Governance policy。

## 11. MCP Annotation

MCP annotation 必须经过 MCP Tool Adapter 的显式 normalization 才能进入 Unified ToolSpec。

其中 `default_enabled` 第一阶段采用已经确定的独立规则：

~~~text
default_enabled = (readOnlyHint === true)
~~~

该值只用于 capability 初始配置建议：

- 不等于 Agent 已授权；
- 不自动修改已有 Agent Capability；
- 不绕过 Approval / Authorization。

具体规则见：

> [MCP Tool 默认启用策略](./mcp-tool-default-enable.md)

其他 annotation 对 Unified ToolSpec 的映射由 MCP Tool Discovery / Execution detailed design 约束。

## 12. Transport

agenteam 的 MCP Integration 是 Central 后台对远程 MCP Server 的服务端集成。

因此 MCP Bridge 只支持 Central 可通过网络访问的 Streamable HTTP MCP，不支持 stdio 等本地进程型 transport。

Endpoint 的网络访问统一受平台 Outbound Network Policy 约束，具体规则见：

> [MCP Server Config / Connection Lifecycle](./mcp-server-config-lifecycle.md)
>
> [Outbound Network Policy](../platform-infrastructure/outbound-network-policy.md)

## 13. MCP Capability Integration 原则

agenteam 不以“完整透传 MCP 协议”为目标。

总体关系：

~~~text
MCP Server
-> MCP Protocol Runtime
-> capability-specific Adapter
-> agenteam subsystem
~~~

第一阶段接入：

- MCP Tools；
- MCP Resources。

MCP Prompts、Elicitation、Tasks 等 capability 第一阶段不接入。

未来支持时：

- 通过 MCP Bridge 增加对应 Adapter；
- 明确目标内部子系统；
- 单独设计其 lifecycle / security / context semantics；
- 不因为远端 Server 声明 capability 就直接暴露给 Agent Loop。

## 14. 第一阶段实现边界

第一阶段包含：

1. System / Project MCP Config；
2. Project-scoped MCP Connection；
3. 显式 Connect / Authentication；
4. Connection enable / disable / disconnect；
5. Streamable HTTP；
6. MCP Protocol Runtime；
7. MCP Tool Discovery；
8. MCP Tool -> Unified ToolSpec；
9. stable MCP Tool identity；
10. Tool Registry 注册；
11. Agent Capability 逐 Tool 授权；
12. MCP Tool Execution；
13. MCP Resource Adapter；
14. Resource Catalog / Template Catalog；
15. `resources/list` / `resources/templates/list` / `resources/read`；
16. ResourceLink / EmbeddedResource materialization；
17. Resource content -> ObjectStorageService / StoredObject；
18. Credential Binding；
19. Unified Tool Authorization / Approval / Audit。

第一阶段不包含：

- MCP Prompts；
- MCP Elicitation；
- MCP Tasks extension；
- 自动把新 discovery Tool 加入已有 Agent Capability。

## 15. 与其他架构的边界

### Unified Tool Runtime

定义 ToolSpec、ToolBinding、Registry、Execution Tool Set、Operation / Attempt、Result / Error、retry / idempotency 等统一语义。

MCP 只实现 Tool source / backend adapter。

### Agent Management

保存 Agent 对 MCP Tool 的 stable ID 引用，不保存 MCP schema、Credential 或 protocol state。

### Security / Governance

决定 MCP Tool 是否允许当前 Agent Execution 调用以及是否需要 Approval，不负责 MCP Server 登录认证。

### Secret / Credential Management

为 MCP Connection 保存安全 Credential Binding；plaintext 不进入 Agent Context。

### Platform Infrastructure

负责 MCP Config、Project MCP Connection、authentication / discovery state 等持久化基础设施，以及 MCP Endpoint 使用的统一 Outbound Network Policy。
