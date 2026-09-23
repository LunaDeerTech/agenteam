# MCP 集成架构

## 1. 定位

MCP 是 agenteam Tool System 的一种外部 Tool 来源。

agenteam 不把 MCP 作为 Agent Loop 的唯一工具协议，也不让模型通过一个通用 `call_mcp` Tool 间接访问所有 MCP Server。

MCP Server 暴露的 Tool 由 **MCP Bridge** 发现并映射为统一 `ToolSpec`，随后与 Builtin Tool、Runner Tool 一起进入 Tool Registry。

~~~mermaid
flowchart LR
    Config["MCP Server Config"]
    Bridge["MCP Bridge"]
    Server["MCP Server"]
    Registry["Tool Registry<br/>Unified ToolSpec"]
    Capability["Agent Capability"]
    Execution["Agent Execution"]
    Loop["Agent Loop"]

    Config --> Bridge
    Bridge <--> Server
    Bridge -->|"tool discovery / mapping"| Registry
    Registry --> Capability
    Capability --> Execution
    Execution --> Loop
~~~

因此：

> Agent 看到的是具体 Tool，而不是 MCP 协议本身。

## 2. MCP Server 配置

MCP Server Config 是 MCP 集成的长期配置对象。

至少应包含：

- `id`；
- `name`；
- `description`；
- `scope`：system / project；
- `transport`；
- endpoint / command 等 transport-specific config；
- `credential_ref`；
- enabled 状态；
- connection / discovery 状态；
- 最近一次 discovery 信息。

### 2.1 System-scoped MCP

系统级 MCP 由系统管理员配置。

系统级 MCP 可以作为 Project 的可用 Tool 来源；具体 MCP Tool 是否提供给某个 Agent，仍由 Agent Capability 决定。

### 2.2 Project-scoped MCP

项目级 MCP 只属于当前 Project。

它发现出的 Tool 只能被当前 Project 的 Agent 配置和 Agent Execution 使用。

### 2.3 Credential

MCP Credential 使用 Secret Reference。

对于 Project-scoped MCP，`credential_ref` 可以引用当前 Project 的 Secret Variable；System-scoped MCP 则引用平台级 Secret。

Agent、Agent Execution Context 和 Model 不直接获得 Credential 明文。

调用链路：

~~~text
MCP Tool Call
  -> MCP Executor / Bridge
      -> MCP Server Config
          -> credential_ref
              -> Project Secret Variable / Platform Secret
                  -> Secret Resolver
                      -> MCP Client
~~~

MCP Backend 使用某个 Project Secret Variable，不代表该 Secret 会自动加入 Agent 的 `allowed_secret_variables`。MCP Credential Binding 与 Agent 可直接使用的环境变量白名单是两个独立关系。

## 3. MCP Bridge

MCP Bridge 负责把 MCP Server 接入统一 Tool System。

主要职责：

1. 根据 MCP Server Config 建立连接；
2. 获取 Server capabilities；
3. 发现 MCP Tools；
4. 将 MCP Tool schema 映射为 Unified ToolSpec；
5. 将映射后的 Tool 注册到 Tool Registry；
6. 调用时把统一 Tool Call 转换成 MCP 请求；
7. 将 MCP Result 转换成统一 Tool Result；
8. 维护 Tool availability / discovery 状态。

MCP Bridge 不负责 Agent 权限决策。

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
MCP Server Config
  -> MCP Bridge Discovery
      -> Unified ToolSpec
          -> Tool Registry
              -> Agent Capability references
                  -> Agent Execution Tool Set
~~~

MCP Server 是否存在、Tool 是否 available，与 Agent 是否拥有这个 Tool 的长期 Capability 是两个不同问题。

## 7. Discovery 与刷新

MCP Tool discovery 至少在以下场景发生：

- MCP Server Config 创建或启用；
- 配置发生变化；
- 服务启动后的连接初始化；
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
    participant T as Tool System
    participant M as MCP Executor / Bridge
    participant S as MCP Server

    A->>T: Unified Tool Call
    T->>T: Capability / Policy / Authorization

    alt allowed
        T->>M: execute(stable tool id, args)
        M->>M: resolve MCP config / credential
        M->>S: MCP tools/call
        S-->>M: MCP result
        M-->>T: normalized Tool Result
        T-->>A: Tool Result
    else denied / approval required
        T-->>A: structured security result
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

## 11. MCP 与 Project Secret Variable

MCP Server Config 只保存 `credential_ref`，不保存 Credential 明文。

Project-scoped MCP 的 `credential_ref` 可以引用当前 Project 的 Secret Variable。

Secret 的实际解析发生在 MCP Backend：

~~~text
Project Secret Variable
  -> MCP Server Config.credential_ref
      -> MCP Executor / Bridge
          -> resolve secret
              -> MCP Client
~~~

原则上：

- Agent 不读取 MCP Credential；
- MCP Tool input schema 不暴露 Credential 字段；
- Tool Result 和错误需要脱敏；
- Credential 不进入 Agent Execution 普通日志；
- Project-scoped MCP 不能引用其他 Project 的 Secret Variable；
- MCP Backend 的 credential binding 不要求把该 Secret Variable 加入 Agent 的 Secret 白名单。

如果某个 Agent 还需要在 Runner command 中直接使用同一个 Secret，则 Project Owner 需要另外把该 Secret Variable 加入该 Agent 的 `allowed_secret_variables`。

详细数据模型见 [项目变量与 Secret 详细设计](../design/project-work-management/project-environment-variables.md)。

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

MCP Bridge 只把这些 annotations 翻译为 Unified ToolSpec 的 `default_enabled`，用于 Agent Capability 的初始配置建议。

第一阶段固定采用：

~~~text
default_enabled = (readOnlyHint === true)
~~~

即只有 MCP Server 明确声明 `readOnlyHint = true` 的 Tool 默认启用；`false` 或缺失都默认关闭，其他 annotation 不参与该值计算。

因此：

~~~text
MCP annotations
  -> MCP Bridge
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

## 13. 第一阶段实现边界

第一阶段建议至少支持：

1. System / Project 两级 MCP Server Config；
2. Streamable HTTP MCP；
3. MCP Tool discovery；
4. MCP Tool -> Unified ToolSpec；
5. stable MCP Tool ID；
6. Tool Registry 注册；
7. Agent Capability 逐 Tool 授权；
8. Tool unavailable / refresh；
9. Credential Reference；
10. 统一 Tool Authorization 与 Audit。

第一阶段可以暂缓：

- 自动把整个 MCP Server 的所有 Tool 授权给 Agent；
- 复杂的 schema migration；
- Runner-hosted stdio MCP；
- MCP resources / prompts 的完整接入；
- 自动信任新 discovery 出来的 Tool。

## 14. 与其他架构的边界

### Agent Management

保存 Agent 对 MCP Tool 的 stable ID 引用，不保存 MCP schema 或连接信息。

### Tool System

提供 Unified ToolSpec、Tool Registry、Authorization 和 Dispatcher。

### Security / Governance

决定 MCP Tool 是否允许当前 Agent Execution 调用，以及是否需要审批。

### Project Environment Variables / Secret Management

Project-scoped MCP Credential 可以通过 Secret Reference 指向 Project Secret Variable，并由 MCP Executor 在后端解析；System-scoped MCP 使用平台级 Secret。

### Runner

如果未来支持 Runner-hosted stdio MCP，Runner 负责对应的进程宿主和本机安全边界。

### Platform Infrastructure

负责 MCP Config、连接状态等持久化与平台级运行基础设施。
