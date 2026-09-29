# Runner 架构

## 1. 定位

Runner 是 agenteam 的远程资源执行节点。

Central / Control Plane 负责：

- Project / Task / Meeting 等业务状态；
- Scheduler；
- Agent Executor / Agent Execution；
- Agent Loop；
- Model 调用；
- Unified Tool Runtime；
- Authorization / Approval / Audit。

Runner 负责把远端设备能力提供给 Agent，包括：

- Filesystem；
- Command；
- Managed Process；
- Transfer；
- Desktop；
- Tunnel。

Runner 不运行完整的 agenteam 协作逻辑，也不维护一套独立的业务权限系统。

## 2. 总体架构

Runner 通过 Agent Mount 为 Agent 提供设备上的 workspace 与执行能力。

~~~mermaid
flowchart LR
    subgraph Central["agenteam Central"]
        ToolRuntime["Unified Tool Runtime"]
        RunnerBackend["Runner Backend / Dispatcher"]
        Connection["Runner Connection Registry"]
        Identity["Runner Enrollment / Identity"]
    end

    subgraph Runner["agenteam-runner"]
        Protocol["Runner Protocol Client"]
        Capabilities["Runtime Capabilities"]
        Workspace["Agent Workspace Resolver"]
        Execution["Execution Runtime"]
        Data["Data Channel"]
        Desktop["Desktop / Tunnel"]
    end

    Agent["Agent / Agent Execution"]

    Agent --> ToolRuntime
    ToolRuntime --> RunnerBackend
    RunnerBackend --> Connection
    Identity --> Connection

    Connection <-->|"WSS Control Channel"| Protocol
    Protocol --> Capabilities
    Protocol --> Workspace
    Protocol --> Execution
    Protocol --> Desktop
    Protocol <-->|"On-demand Data Channel"| Data
~~~

Runner 与 Central 之间使用 agenteam 自定义的 Runner Protocol，不要求使用 MCP。

模型和 Agent Loop 不感知 Runner Protocol。Runner 能力在 Central 中被适配成统一 Runner Tools。

## 3. Runner 与 Agent 的关系

Runner 是系统级设备资源，但不是“服务于 Project”的资源。

Runner 通过 Agent Mount 为 Agent 提供运行环境：

~~~text
System Runner
     ↓
Agent Mount
     ↓
Agent Workspace
     ↓
Runner Device Capability
~~~

Project 只参与 workspace 的资源归属与路径命名。

Agent 可以：

- 挂载多个 Runner；
- 在同一个 Runner 上挂载多个 workspace；
- 根据不同任务使用不同设备能力。

完整设计见 [Agent Workspace 详细设计](./agent-workspace.md)。

## 4. Agent Workspace

每个 Runner 创建时配置一个固定 `root-path`。

Agent Mount 不允许填写任意绝对路径，只配置逻辑 workspace 名。

实际路径由系统统一映射：

~~~text
<runner-root>/<project-id>/<agent-id>/<workspace>
~~~

例如：

~~~text
runner root-path = /workspace
project-id = project-A
agent-id = agent-X
workspace = source

-> /workspace/project-A/agent-X/source
~~~

Filesystem Tool 必须始终限制在当前 workspace 范围内。

第一阶段 `run-command` 直接使用 Runner 宿主机当前运行用户执行，只保证 working directory 位于 workspace 内，不承诺 shell command 无法主动访问 workspace 外路径，也不引入容器或 sandbox。

## 5. Runner Capability

Runner Capability 由 Runner 实际运行环境探测得到。

第一阶段统一 Tool System 已确定的 Runner Capability 包括：

### Filesystem

- `read-file`
- `write-file`
- `edit-file`
- `list-directory`
- `grep-files`
- `find-files`

### Command

- `run-command`

### Managed Process

- `start-process`
- `get-process` / `read-process-output`
- `stop-process`

### Transfer

- `file-upload`
- `file-download`
- `file-transfer`

### Desktop

在设备具备桌面环境时可以提供：

- `screenshot`
- `automation`
- `browser-use`
- `computer-use`

### Tunnel

- `expose-port`

第一阶段不实现 LSP / Code Navigation Capability。

## 6. Connection Topology

Runner 始终主动向 Central 建立出站连接。

~~~text
Enrollment / Challenge
= HTTPS

Control Channel
= WebSocket over TLS (WSS)
= JSON Envelope

Data Channel
= Runner 主动建立的按需独立连接
= binary / large-file transfer
~~~

Central 不要求能够直接访问 Runner，因此 Runner 可以部署在：

- NAT；
- 家庭网络；
- 企业内网；
- 无公网监听端口的服务器或桌面设备。

Runner 身份通过 Enrollment + Ed25519 Device Identity 建立。

完整设备生命周期见 [Runner Management 详细设计](./runner-management.md)。

## 7. Control Channel 与 Data Channel

Control Channel 负责：

- hello；
- heartbeat；
- status；
- capability synchronization；
- request / response；
- cancellation；
- stdout / stderr stream event；
- Data Channel 协商。

Data Channel 负责 Runner 与 Central 之间的大文件和 binary payload。

二者职责固定分离：

~~~text
Control Channel
= structured control plane

Data Channel
= bulk data plane
~~~

Object Storage 是另一条独立的数据路径。Agent 通过 Artifact / Object Storage 能力把对象传入或传出 Runner 时，由 Control Channel 协商 metadata，然后 Runner 直接与 Object Storage 进行数据传输。

协议设计见：

- [Control Protocol 详细设计](./control-protocol.md)
- [Data Channel 详细设计](./data-channel.md)

## 8. Execution Runtime

Runner Execution Runtime 负责：

- workspace 内 Filesystem 操作；
- `run-command`；
- stdout / stderr streaming；
- command cancellation；
- Managed Process；
- environment injection；
- Secret masking。

Runner 不维护 Project Secret Store。

Secret 由 Central 在具体 operation 执行前解析，并通过当前 RPC 的 environment payload 临时下发。

完整设计见 [Execution Runtime 详细设计](./execution-runtime.md)。

## 9. Desktop 与 Tunnel

Desktop / Browser / Computer Use 能力直接实现在同一个 Runner 工程中，不拆成独立 Runner 插件。

是否可用由设备实际环境决定。

Tunnel 作为第一阶段正式 Capability，用于把 Runner 本地服务临时暴露给用户或其他调用方。

完整设计见 [Desktop & Tunnel 详细设计](./desktop-tunnel.md)。

## 10. 安全边界

Runner 不实现一套与 Central 重复的权限系统。

权限决策 Source of Truth 在 Central：

~~~text
Agent Capability
+ Execution Policy
+ Project / Resource Scope
+ Approval / Authorization
            ↓
       Runner Request
~~~

Runner 只执行协议与运行时完整性校验，例如：

- 当前连接已认证；
- request schema 合法；
- workspace identity / path 映射合法；
- filesystem path 不能逃逸当前 workspace；
- requested operation 属于当前实际 Capability；
- environment / Secret 不写入普通日志。

对于 shell command，第一阶段采用 trusted-host 语义：

> 把 Agent 挂载到某个 Runner，意味着允许该 Agent 通过该 Runner 当前宿主机用户权限执行命令。

## 11. Runner 状态

第一阶段 Runner 顶层状态只包含：

~~~text
online
offline
incompatible
~~~

Capability 的单项可用性不通过 Runner 顶层状态表达，而通过 capability metadata 表达。

## 12. 详细设计文档

Runner 详细设计按以下边界拆分：

- [Runner Management](./runner-management.md)：配置、Enrollment、Device Identity、版本兼容、状态与设备生命周期；
- [Agent Workspace](./agent-workspace.md)：Agent Mount、workspace path、Filesystem 边界与 trusted-host 语义；
- [Control Protocol](./control-protocol.md)：WSS、Envelope、RPC、streaming、heartbeat、reconnect、unknown outcome；
- [Data Channel](./data-channel.md)：binary / large-file transfer、channel lifecycle、Transfer Capability 与 Object Storage 边界；
- [Execution Runtime](./execution-runtime.md)：Filesystem、Command、Managed Process、environment、Secret、cancellation；
- [Desktop & Tunnel](./desktop-tunnel.md)：Desktop / Browser / Computer Use 与 expose-port。
