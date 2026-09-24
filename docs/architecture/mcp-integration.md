# MCP 集成架构

## 1. 定位

MCP 是 agenteam Tool System 的一种外部 Tool 来源。

agenteam 不把 MCP 作为 Agent Loop 的唯一工具协议，也不让模型通过一个通用 `call_mcp` Tool 间接访问所有 MCP Server。

MCP Server 暴露的 Tool 由 **MCP Bridge** 发现并映射为统一 `ToolSpec`，随后与 Builtin Tool、Runner Tool 一起进入 Tool Registry。

~~~mermaid
flowchart LR
    Catalog["MCP Config Catalog<br/>System / Project"]
    Connection["Project MCP Connection<br/>Connect · Authentication · Enabled State"]
    Bridge["MCP Integration Runtime<br/>MCP Client / Bridge"]
    Tools["Unified Tool Runtime"]
    Execution["Agent Execution<br/>Agent Loop"]
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

因此：

> Agent 看到的是具体 Tool，而不是 MCP 协议本身。

## 2. MCP Config 与 MCP Connection

MCP 集成明确区分 **MCP Config** 与 **MCP Connection**。

`MCP Config` 是长期配置和目录对象，描述：

- 这个 MCP 服务是什么；
- 如何连接；
- 使用什么 transport；
- endpoint / command 等 transport-specific config；
- 支持什么身份验证方式。

`MCP Config` 本身不代表已经与远端 MCP Server 建立真实连接，也不保存某个 Project 的认证状态。

`MCP Connection` 是 Project 对某个 MCP Config 建立的实际连接关系，描述：

- 所属 Project；
- 引用的 MCP Config；
- 当前连接 / enabled 状态；
- 当前认证状态；
- Credential Binding；
- discovery 状态；
- 最近一次成功连接 / discovery 信息。

概念关系为：

~~~mermaid
flowchart TB
    SystemConfig["System MCP Config<br/>平台公共目录"]
    ProjectConfig["Project MCP Config<br/>仅所属 Project 可见"]

    ProjectA["Project A"]
    ProjectB["Project B"]

    ASystemConnection["MCP Connection<br/>Project A → System Config"]
    BSystemConnection["MCP Connection<br/>Project B → System Config"]
    AProjectConnection["MCP Connection<br/>Project A → Project Config"]

    SystemConfig -->|"所有 Project 可见"| ProjectA
    SystemConfig -->|"所有 Project 可见"| ProjectB
    ProjectConfig -->|"仅所属 Project"| ProjectA

    ProjectA -->|"Connect / Authenticate"| ASystemConnection
    ProjectB -->|"Connect / Authenticate"| BSystemConnection
    ProjectA -->|"Connect / Authenticate"| AProjectConnection
~~~

因此：

> MCP Config 不等于一个已经连接的 MCP Server。真正参与 Project 运行时的是 MCP Connection。

### 2.1 System-scoped MCP Config

System MCP Config 由系统管理员创建，作为平台公共 MCP 目录。

所有 System MCP Config 对所有 Project 可见，但仅“可见”不代表已经连接或可以直接用于 Agent Execution。

Project 想使用某个 System MCP Config 时，必须显式执行 Connect，完成该 Project 自己的登录 / 身份认证并建立 `MCP Connection`。只有连接成功后，该 MCP 才能成为当前 Project 的实际 Tool 来源。

System MCP Config 本身不建立真实服务连接，也不持有 Project Credential。

### 2.2 Project-scoped MCP Config

Project MCP Config 由 Project 自己创建，只对所属 Project 可见。

创建 Project MCP Config 后同样不会自动建立真实连接。Project 仍需要显式 Connect，完成认证并建立对应的 `MCP Connection`，之后才能使用该 MCP 提供的 Tool。

System / Project 两种 Config 的主要差异是配置可见范围；连接、认证、discovery 与 Tool 接入机制统一基于 `MCP Connection`。

### 2.3 Connect 与 Credential

MCP Credential 属于 Project 的 MCP Connection，而不是 System MCP Config。

Connect 流程概念上为：

~~~text
MCP Config
  -> Project clicks Connect
      -> authentication / login
          -> Credential Binding
              -> MCP Connection
                  -> discovery
                      -> MCP Tools available to this Project
~~~

Credential 使用 Secret Reference 或对应认证机制的安全存储，不在 MCP Config 中保存认证明文。

Agent、Agent Execution Context 和 Model 不直接获得 MCP Credential 明文。

MCP Backend 使用某个 Project Secret Variable，不代表该 Secret 会自动加入 Agent 的 `allowed_secret_variables`。MCP Credential Binding 与 Agent 可直接使用的环境变量白名单是两个独立关系。

### 2.4 Enable / Disable / Disconnect / Delete

MCP Connection 的生命周期如下：

~~~mermaid
stateDiagram-v2
    [*] --> Unconnected

    Unconnected --> Connecting: Connect
    Connecting --> Enabled: Authentication success
    Connecting --> Unconnected: Failure / cancel

    Enabled --> Disabled: Disable
    Disabled --> Enabled: Enable

    Enabled --> Unconnected: Disconnect
    Disabled --> Unconnected: Disconnect

    Unconnected --> [*]: Config deleted
    Enabled --> [*]: Config deleted
    Disabled --> [*]: Config deleted
~~~

MCP Connection 的 enable / disable 属于 Project 运行时控制。

Disable 已连接的 MCP Connection：

- 阻止新的 Agent Execution 使用该 Connection；
- 不主动影响已经开始运行的 Agent Execution；
- 不改变 MCP Config 本身。

Disconnect / Delete Connection：

- 删除当前 Project 对该 MCP Config 的认证连接；
- 阻止新的 Agent Execution 使用该 Connection；
- 不删除 MCP Config；
- 不主动中断已经运行的 Agent Execution。

Delete MCP Config 是独立的配置管理操作：

- 删除 MCP Config 本身；
- 自动删除基于该 Config 建立的 MCP Connections；
- 删除后不能再创建新的 Agent Execution 使用该 MCP；
- 已经运行中的 Agent Execution 不被主动中断。

因此，disable、disconnect 与 config delete 对**新 Execution** 的效果一致：对应 MCP Tool 不再进入新的 Execution Tool Set；已经运行的 Execution 按其既有运行时快照继续，不因为控制面配置变化而被主动撤销。

如何在连接或配置删除后安全保留运行中 Execution 所需的临时运行时引用，以及何时进行物理 Credential / Connection 清理，属于 MCP Connection Lifecycle 的详细设计问题。

## 3. MCP Bridge

MCP Bridge 负责 MCP 协议接入与 capability integration。

MCP Bridge 不等同于 MCP Tool Adapter。它提供统一的 MCP Protocol Runtime，并通过 capability-specific Adapter 把 MCP 的不同能力映射到 agenteam 内部对应子系统。

第一阶段只实现 MCP Tool Adapter；未来支持 Resources、Prompts、Elicitation 等能力时，应在同一个 MCP Bridge 中增加对应 Adapter，而不是让各业务模块分别实现 MCP Client。

### 3.1 内部架构

~~~mermaid
flowchart LR
    Server["External MCP Server"]

    subgraph Bridge["MCP Bridge"]
        direction LR

        subgraph Protocol["MCP Protocol Runtime"]
            Client["MCP Client"]
            Session["Connection / Session<br/>Transport · Authentication<br/>Protocol Negotiation"]
            Router["Capability / Message Router"]
        end

        subgraph Adapters["Capability Adapters"]
            ToolAdapter["MCP Tool Adapter<br/>Phase 1"]
            FutureAdapter["MCP Resource / Prompt / ... Adapter<br/>Future"]
        end

        Client --> Session
        Session --> Router

        Router --> ToolAdapter
        Router -.-> FutureAdapter
    end

    ToolRuntime["Unified Tool Runtime"]
    FutureSubsystem["Other agenteam Subsystem<br/>Future"]

    Server <--> Client

    ToolAdapter --> ToolRuntime
    FutureAdapter -.-> FutureSubsystem
~~~

这里的核心边界是：

> Capability Adapter 不直接连接 MCP Server。所有 MCP transport、session、authentication、protocol version、request / response、notification 等协议行为统一由 MCP Protocol Runtime 承担。

这样 MCP 协议演进被隔离在 Bridge 内部，不会让 Unified Tool Runtime 或未来其他内部子系统直接依赖 MCP 协议细节。

### 3.2 MCP Protocol Runtime

MCP Protocol Runtime 是各 Capability Adapter 共用的协议底座，负责：

1. 根据 MCP Config 与 Project MCP Connection 建立运行时连接；
2. MCP Client 与 transport；
3. Authentication / Credential Binding 接入；
4. connection / session lifecycle；
5. protocol version negotiation；
6. Server capability discovery；
7. MCP request / response；
8. notification / subscription routing；
9. cancellation / progress / protocol error 等通用协议行为。

Protocol Runtime 不负责把 MCP capability 映射成 agenteam 业务模型。

### 3.3 Capability Adapter

Capability Adapter 负责：

> MCP capability model <-> agenteam internal subsystem model

Adapter 只处理对应 capability 的语义转换，不重新实现 MCP transport、authentication、session 或版本兼容。

第一阶段：

~~~text
MCP Tool Adapter
  -> Unified Tool Runtime
~~~

未来可扩展：

~~~text
MCP Resource / Prompt / ... Adapter
  -> corresponding agenteam subsystem
~~~

这里只保留 capability adapter 的扩展点，不在 architecture 层提前枚举未来具体 Adapter 或其目标子系统。

### 3.4 MCP Tool Adapter

MCP Tool Adapter 是第一阶段唯一实现的 Capability Adapter。

它同时承担两条 Tool 集成路径：

~~~text
Definition / Discovery path

MCP tools/list
  -> MCP Tool Adapter
      -> Unified ToolSpec
          -> Tool Registry
~~~

~~~text
Execution path

Unified Tool Call
  -> MCP Tool Adapter
      -> MCP Protocol Runtime
          -> MCP tools/call

MCP result
  -> MCP Tool Adapter
      -> Unified ToolResult / ToolError
~~~

实现内部可以进一步区分 Tool Definition Adapter 与 Tool Backend Adapter，但对 MCP Bridge 外部仍表现为同一个 MCP Tool Adapter。

MCP Tool 从远端服务进入 agenteam Tool System 的 definition / discovery 链路为：

~~~mermaid
flowchart LR
    Server["External MCP Server<br/>MCP Tools"]
    Protocol["MCP Protocol Runtime"]
    Adapter["MCP Tool Adapter"]
    Spec["Unified ToolSpec"]
    Registry["Tool Registry"]
    Capability["Agent Capability"]
    ExecutionSet["Execution Tool Set"]

    Server -->|"tools/list"| Protocol
    Protocol --> Adapter
    Adapter -->|"stable identity + normalized schema"| Spec
    Spec -->|"spec_revision + availability"| Registry
    Registry -->|"stable tool id reference"| Capability
    Capability -->|"snapshot when Execution starts"| ExecutionSet
~~~

MCP Bridge 与 MCP Tool Adapter 都不负责 Agent 权限决策。

MCP Tool 仍必须经过统一的：

- Agent Capability；
- Execution Policy；
- Project / Resource Scope；
- 平台固定安全规则；
- Approval Match / Approval Policy；
- Tool Authorization；
- Audit。

## 4. MCP Tool 映射

MCP Server 发现出的每一个 Tool 都映射为一个独立的 Unified ToolSpec。

例如：

~~~text
github-mcp
  -> search-code
  -> create-issue
  -> create-pull-request
~~~

映射后在 Tool Registry 中仍然是三个独立 Tool。

Agent Capability 可以分别授权：

~~~text
MCP / github-mcp

[x] search-code
[x] create-issue
[ ] create-pull-request
~~~

不会因为 Agent 获得了某一个 MCP Tool，就自动获得同一个 MCP Server 的全部 Tool。

## 5. Stable Tool Identity

Agent Capability 不能依赖 Tool 的展示名称保存权限。

MCP Tool 必须具有稳定身份。

概念上可以使用：

~~~text
mcp:<mcp_server_config_id>:<remote_tool_name>
~~~

或者等价的内部稳定 Tool ID。

Stable Tool ID 用于：

- Agent Capability 引用；
- Tool Registry；
- Agent Execution capability snapshot；
- Audit；
- Tool Call routing。

展示名称可以变化，但不能因此改变权限身份。

### 5.1 新增 Tool

如果 MCP Server 后续 discovery 出新的 Tool：

> 新 Tool 默认不自动加入已有 Agent Capability。

管理员需要显式为 Agent 开启该 Tool。

这样 MCP Server 升级不会隐式扩大 Agent 权限。

### 5.2 Tool 消失

如果某个已授权 Tool 在新的 discovery 中消失：

- Agent Capability 中的稳定引用可以保留；
- Tool 标记为 unavailable；
- Agent Execution 不应把 unavailable Tool 暴露给模型；
- Tool 恢复后可以重新变为 available。

不应因为一次连接失败就自动删除 Agent 的长期配置。

### 5.3 Schema 变化

如果远端 Tool 名称和稳定身份不变，但 schema 更新：

- Tool Registry 更新对应 ToolSpec；
- 新 Agent Execution 使用新的 ToolSpec；
- 旧 Agent Execution 保留自己的 Tool / capability snapshot 用于追溯。

如果变化无法安全兼容，应将 Tool 标记为需要重新确认，而不是静默改变已有权限语义。

## 6. Agent Capability 集成

MCP Bridge 映射出的 Tool 与 Builtin / Runner Tool 一样，属于 Agent Capability 的普通可配置 Tool。

Agent 配置界面应按来源展示，例如：

~~~text
Builtin
  ...

Runner
  ...

MCP
  github-mcp
    [x] search-code
    [x] create-issue
    [ ] create-pull-request

  jira-mcp
    [x] search-issues
    [ ] create-issue
~~~

Agent Capability 保存 Tool stable ID，而不是复制 MCP Tool schema。

因此关系是：

~~~text
MCP Config
  -> Project MCP Connection
      -> MCP Bridge Discovery
          -> Unified ToolSpec
          -> Tool Registry
              -> Agent Capability references
                  -> Agent Execution Tool Set
~~~

MCP Config / Connection 是否存在、Tool 是否 available，与 Agent 是否拥有这个 Tool 的长期 Capability 是不同问题。

## 7. Discovery 与刷新

MCP Tool discovery 至少在以下场景发生：

- Project 完成 Connect 并建立 MCP Connection；
- MCP Connection 重新启用；
- 影响连接或远端能力的配置发生变化；
- 服务启动后的已连接 MCP Connection 初始化；
- 管理员主动 refresh；
- MCP Server 明确通知 capabilities / tool list 发生变化（如果 transport / protocol 支持）。

Discovery 结果需要记录：

- connection status；
- discovery status；
- discovered tool count；
- last successful discovery；
- last error。

Tool Discovery 失败不应直接破坏已保存的 Agent Capability。

## 8. Tool 名称与冲突

不同 Tool Provider 可能出现相同展示名称，例如：

~~~text
Builtin: search
MCP A: search
MCP B: search
~~~

因此 Tool Registry 内部依赖 stable tool id，而不是 display name 保证唯一性。

模型侧 Tool 名称如果受 Provider schema 限制，需要由 Tool System 在当前 Execution Tool Set 中生成无冲突的模型可见名称，并保存与 stable tool id 的映射。

Agent Capability 和 Audit 始终使用 stable tool id。

## 9. MCP Tool 执行链路

~~~mermaid
sequenceDiagram
    participant A as Agent Loop
    participant D as Tool Dispatcher
    participant G as Security / Governance
    participant B as MCP Tool Adapter
    participant C as MCP Protocol Runtime
    participant S as MCP Server

    A->>D: Unified Tool Call
    D->>G: authorize(tool, execution, args)

    alt denied / approval required
        G-->>D: denied / approval_required
        D-->>A: structured Tool Error
    else allowed
        G-->>D: allowed
        D->>B: execute(stable tool id, args)
        B->>C: tools/call request
        C->>S: MCP tools/call
        S-->>C: MCP result
        C-->>B: MCP protocol result
        B-->>D: normalized ToolResult / ToolError
        D-->>A: Tool Result
    end
~~~

MCP Server 不能直接绕过 Tool System 被 Agent Loop 调用。

## 10. Transport 与执行位置

MCP Bridge 需要支持 MCP 的实际 transport。

至少需要考虑：

- Streamable HTTP；
- stdio；
- 后续协议支持的其他 transport。

### Streamable HTTP

对于 Central 网络可访问的 MCP Server，可以由 Central 的 MCP Bridge 直接建立 Client 连接。

### stdio

stdio MCP 需要本地进程宿主，因此必须明确运行位置。

可能的运行方式包括：

~~~text
Central-hosted stdio MCP
或
Runner-hosted stdio MCP
~~~

这两种方式的权限、进程生命周期、文件访问和 Secret 注入边界不同。

当前先保留明确的架构扩展点，不假定所有 stdio MCP 都能直接运行在 Central。Runner-hosted stdio MCP 的具体协议衔接需要与 Runner 架构进一步确认。

## 11. MCP Authentication 与 Tool Authorization

MCP Authentication 与 Tool Authorization 是两套独立机制。

### 11.1 MCP Authentication

MCP Authentication 解决的是：

> agenteam / 当前 Project 如何向远端 MCP Server 证明自己的身份并建立 MCP Connection。

认证状态与 Credential Binding 属于 Project 的 MCP Connection。

如果认证机制使用 Project Secret Variable，其实际解析发生在 MCP Backend：

~~~text
Project MCP Connection
  -> credential binding
      -> Project Secret Variable / secure credential storage
          -> MCP Protocol Runtime
              -> resolve credential
                  -> MCP Client
~~~

原则上：

- Agent 不读取 MCP Credential；
- MCP Tool input schema 不暴露 Credential 字段；
- Tool Result 和错误需要脱敏；
- Credential 不进入 Agent Execution 普通日志；
- MCP Connection 不能引用其他 Project 的 Secret Variable；
- MCP Backend 的 credential binding 不要求把该 Secret Variable 加入 Agent 的 Secret 白名单。

如果某个 Agent 还需要在 Runner command 中直接使用同一个 Secret，则 Project Owner 需要另外把该 Secret Variable 加入该 Agent 的 `allowed_secret_variables`。

### 11.2 Tool Authorization

Tool Authorization 解决的是：

> 当前 Agent / Agent Execution 是否允许调用某个已经通过 MCP Connection 接入的 MCP Tool。

MCP Connection 认证成功只表示当前 Project 可以访问对应 MCP Server，不代表任意 Agent 自动获得该 Server 的 Tool。

MCP Tool 仍然统一经过 Agent Capability、Execution Policy、Approval、Tool Authorization 与 Audit。

因此：

~~~text
MCP Authentication
  = Platform / Project -> MCP Server

Tool Authorization
  = Agent / Execution -> MCP Tool
~~~

## 12. MCP 与安全治理

MCP Tool 和其他 Tool 一样受 Security / Governance 约束。

至少包括：

- Project Scope；
- Agent Capability；
- Execution Policy；
- Approval；
- Secret Binding；
- Audit。

MCP Server 返回的 Tool annotations 不直接参与 agenteam 的运行时授权或 Approval 判断。

MCP Tool Adapter 只把这些 annotations 翻译为 Unified ToolSpec 的 `default_enabled`，用于 Agent Capability 的初始配置建议。

第一阶段固定采用：

~~~text
default_enabled = (readOnlyHint === true)
~~~

即只有 MCP Server 明确声明 `readOnlyHint = true` 的 Tool 默认启用；`false` 或缺失都默认关闭，其他 annotation 不参与该值计算。

因此：

~~~text
MCP annotations
  -> MCP Tool Adapter
      -> Unified ToolSpec.default_enabled
~~~

而运行时仍然统一走：

~~~text
Agent Capability
  -> Execution Policy
      -> Approval
          -> Tool Authorization
~~~

`default_enabled` 不等于 Agent 已经拥有该 Tool。新 discovery 出来的 Tool 也不能因为默认建议为 enabled 就自动加入已有 Agent Capability。

具体转换规则见 [MCP Tool 默认启用策略详细设计](../design/mcp-integration/mcp-tool-default-enable.md)。

## 13. MCP Capability Integration 原则

agenteam 不以“完整透传 MCP 协议”为目标。

MCP 的不同 capability 必须通过 MCP Bridge 内各自明确的 Capability Adapter 映射到 agenteam 内部对应的子系统，而不是把 MCP 协议对象直接泄漏到 Agent Loop。

总体关系为：

~~~text
MCP Server
  -> MCP Protocol Runtime
      -> capability-specific Adapter
          -> agenteam subsystem
~~~

第一阶段只接入 MCP Tools：

~~~text
MCP Tools
  -> MCP Protocol Runtime
      -> MCP Tool Adapter
          -> Unified Tool Runtime
~~~

MCP Resources、Prompts、Elicitation、Tasks 等 capability 第一阶段不接入。未来如需支持，应在 MCP Bridge 中分别增加对应 Adapter，并单独设计其内部目标子系统和生命周期语义，而不是因为 MCP Server 暴露该 capability 就自动向 Agent Loop 透传。

## 14. 第一阶段实现边界

第一阶段建议至少支持：

1. System / Project 两级 MCP Config；
2. Project-scoped MCP Connection；
3. 显式 Connect / Authentication；
4. Connection enable / disable / disconnect；
5. Streamable HTTP MCP；
6. MCP Tool discovery；
7. MCP Tool -> Unified ToolSpec；
8. stable MCP Tool ID；
9. Tool Registry 注册；
10. Agent Capability 逐 Tool 授权；
11. Tool unavailable / refresh；
12. Credential Binding / Reference；
13. 统一 Tool Authorization 与 Audit。

第一阶段可以暂缓：

- 自动把整个 MCP Server 的所有 Tool 授权给 Agent；
- 复杂的 schema migration；
- Runner-hosted stdio MCP；
- MCP resources / prompts / elicitation / tasks 的接入；
- 自动信任新 discovery 出来的 Tool。

## 15. 与其他架构的边界

### Agent Management

保存 Agent 对 MCP Tool 的 stable ID 引用，不保存 MCP schema 或连接信息。

### Tool System

提供 Unified ToolSpec、Tool Registry、Authorization 和 Dispatcher。

### Security / Governance

决定 MCP Tool 是否允许当前 Agent Execution 调用，以及是否需要审批；不负责 MCP Server 登录认证。

### Project Environment Variables / Secret Management

Project MCP Connection 的 Credential Binding 可以通过 Secret Reference 指向当前 Project Secret Variable，并由 MCP Executor 在后端解析。System MCP Config 本身不持有 Project Credential。

### Runner

如果未来支持 Runner-hosted stdio MCP，Runner 负责对应的进程宿主和本机安全边界。

### Platform Infrastructure

负责 MCP Config、Project MCP Connection、认证状态、连接状态等持久化与平台级运行基础设施。
