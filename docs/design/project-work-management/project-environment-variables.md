# 项目变量与 Secret 详细设计

> 状态：设计稿
>
> 上层架构：[项目与工作管理架构](../../architecture/project-work-management.md)
>
> 相关架构：
> - [Agent 管理架构](../../architecture/agent-management.md)
> - [Agent Executor 架构](../../architecture/agent-executor.md)
> - [Runner 架构](../../architecture/runner.md)
> - [平台基础设施架构](../../architecture/platform-infrastructure.md)
> - [安全与治理架构](../../architecture/security-governance.md)

## 1. 设计目标

Project Variables 是 Project 的长期配置对象，用于向 Agent 和执行环境提供项目级变量。

分为两类：

~~~text
Project Variables
├── Variable
│   └── 普通变量
└── Secret
    └── 敏感变量
~~~

设计目标：

- 普通 Variable 对当前 Project 的所有 Agent 可见；
- Secret 必须由 Project Owner 显式授权给具体 Agent；
- Variable / Secret 都包含 description，让 Agent 知道变量用途；
- 当前 Agent 可用的变量信息进入 AgentExecutionContext 和 Prompt；
- Secret value 永远不进入 Agent Model；
- Agent 在命令中通过环境变量名引用 Secret；
- Central 是 Project Variable / Secret 的 Source of Truth；
- 远端 Runner 不同步、不缓存整个 Project 的变量表；
- 在真正启动远端进程时，由 Central 解析当前 Agent 可用变量，并通过 Runner Protocol 临时下发给 Runner；
- Runner 只把这些值注入目标 child process 的 environment。

核心链路：

~~~text
Project Variables
      ↓
Agent Variable Access
      ↓
AgentExecutionContext
      ├── Model-visible projection
      │    └── Prompt
      │
      └── Tool Call
           ↓
      Central Runtime Resolution
           ↓
      RunnerRequest.environment
           ↓ WSS
      Remote Runner
           ↓
      Child Process Environment
~~~

## 2. 数据模型

概念结构：

~~~text
ProjectEnvironmentVariable
├── id
├── project_id
├── name
├── description
├── type
│   ├── variable
│   └── secret
├── value / encrypted_value
├── created_at
└── updated_at
~~~

其中：

- `id`：稳定变量 ID；
- `project_id`：所属 Project；
- `name`：实际环境变量名；
- `description`：变量用途说明；
- `type`：`variable | secret`；
- `value`：普通变量值；
- `encrypted_value`：Secret 的加密值。

变量名第一阶段按常规环境变量名称规则处理。

平台自身保留的内部环境变量命名空间，例如 `AGENTEAM_*`，不能被 Project Variable 覆盖。

同一个 Project 内，变量名必须唯一。

## 3. 普通 Variable

普通 Variable 用于不敏感的项目配置，例如：

~~~text
name = API_BASE_URL
description = Backend API base URL used by local development and tests.
type = variable
value = https://api.example.test
~~~

普通 Variable：

- Project Owner 可以读取 value；
- Project Owner 可以创建、修改和删除；
- Project 内所有 Agent 自动可见；
- 不需要逐 Agent 授权；
- name / description / value 可以进入 AgentExecutionContext；
- name / description / value 可以进入 Prompt；
- Runner 启动 command / process 时自动注入当前 Agent 的执行环境。

普通 Variable 的安全模型是：

> 它属于当前 Project 的普通上下文，不作为 Secret 保护。

因此不应把敏感 Credential 放入普通 Variable。

## 4. Secret

Secret 用于 Token、Credential、Private Key 等敏感值，例如：

~~~text
name = GITHUB_TOKEN
description = GitHub token used for repository API operations.
type = secret
value = ********
~~~

Secret：

- value 加密存储；
- 创建或覆盖更新时接收明文；
- 保存后普通读取接口不返回明文；
- UI 只显示变量存在、name、description 和 masked 状态；
- Project Owner 可以覆盖更新或删除；
- Secret value 不进入 Model Context；
- Secret value 不进入 Task / Meeting / 普通 Execution Log / Audit metadata；
- 只有被当前 Agent 显式授权的 Secret 才能被该 Agent 的执行环境使用。

Project Owner 的配置体验类似：

~~~text
Name            Type       Description
API_BASE_URL    Variable   Backend API base URL
GITHUB_TOKEN    Secret     GitHub repository API token
NPM_TOKEN       Secret     npm registry token
~~~

## 5. Agent Variable Access

### 5.1 普通 Variable

所有普通 Variable 自动属于 Project 内所有 Agent：

~~~text
Project Variable
-> all Project Agents
~~~

Agent 不保存普通 Variable 白名单。

### 5.2 Secret 白名单

Secret 使用 Agent 白名单：

~~~text
Agent
└── allowed_secret_variables
    ├── secret_variable_id_A
    └── secret_variable_id_B
~~~

Agent 只保存 Secret ID 引用，不复制 Secret value。

因此：

~~~text
Project Secrets
      ↓
allowed_secret_variables
      ↓
Agent usable secrets
~~~

未在白名单中的 Secret：

- 不进入 AgentExecutionContext；
- 不进入 Prompt；
- 不注入 Runner；
- Agent 不需要知道它存在。

修改 `allowed_secret_variables` 会改变 Agent 的 Secret 使用边界，因此属于 security-sensitive Agent 配置。

### 5.3 Secret 更新

Agent 白名单引用稳定 Secret ID。

Secret value 被 Project Owner 覆盖更新后：

- Agent 白名单不需要重新配置；
- 后续新启动的 command / process 使用新值。

## 6. AgentExecutionContext 与 Prompt

AgentExecutionContext 的 `environment_context` 包含 Project Variables 的 model-visible projection：

~~~text
environment_context
├── runner / mount metadata
└── project_variables
    ├── variable
    │   ├── name
    │   ├── description
    │   └── value
    └── secret
        ├── name
        ├── description
        └── secret = true
~~~

Secret entry 永远没有 value。

构造逻辑：

~~~text
全部 Project Variable
        +
当前 Agent allowed_secret_variables 对应的 Secret metadata
        ↓
AgentExecutionContext.environment_context.project_variables
~~~

Agent Loop 在 Prompt Assembly 时把这部分作为当前执行环境信息提供给模型。

示例：

~~~text
Project environment variables available to this execution:

- API_BASE_URL
  Value: https://api.example.test
  Description: Backend API base URL used by local development and tests.

- GITHUB_TOKEN
  Secret: true
  Description: GitHub token used for repository API operations.
  Usage: reference it as $GITHUB_TOKEN when running commands.
~~~

因此 Agent Model 知道：

- 有哪些普通变量；
- 普通变量的值；
- 自己有哪些可用 Secret；
- 每个变量的用途；
- Secret 应使用哪个环境变量名。

Agent Model 不知道 Secret value。

## 7. Runtime Environment Resolution

AgentExecutionContext 中的变量信息用于“让 Model 知道有什么”。

真正启动执行后端时，Central 再计算“实际进程环境”。

对于 Runner command / process：

~~~text
Project ordinary Variables
        +
Agent allowed Secret Variables
        ↓
Central Runtime Environment Resolver
        ↓
Resolved Process Environment
~~~

例如：

~~~text
API_BASE_URL=https://api.example.test
GITHUB_TOKEN=<secret plaintext>
~~~

这里的 Secret plaintext 只存在于：

- Central 的 Secret resolution 临时内存；
- Runner Protocol 本次敏感 request payload；
- Runner 当前 request 的临时内存；
- 最终 child process environment。

不会写回 AgentExecutionContext。

### 7.1 解析时机

第一阶段在每次真正启动新进程前解析当前值：

- `run-command`：每次 command 启动前解析；
- `start-process`：Managed Process 创建前解析。

因此 Project Owner 修改 Variable / Secret 后：

- 已经运行中的进程不会被修改；
- 后续新启动的 command / process 使用最新值。

这与普通 OS environment 的行为一致。

### 7.2 Environment 合并

Runner 启动 child process 时：

~~~text
Runner base process environment
        +
Project resolved environment
        ↓
Child process environment
~~~

Project Variables 使用同名覆盖语义。

平台保留的 `AGENTEAM_*` 内部变量不允许被 Project Variable 覆盖。

## 8. 远端 Runner 如何获得 Project Variables

这是 Project Variables 跨设备使用的核心边界。

Runner 不直接访问 agenteam Database，也不直接访问 Project Secret Store。

Runner 也不在设备上持久同步：

~~~text
project variables
project secrets
agent secret whitelist
~~~

这些数据始终由 Central 管理。

远端执行链路：

~~~mermaid
sequenceDiagram
    participant A as Agent Loop
    participant T as Tool System
    participant C as Central Runtime Env Resolver
    participant S as Project Variable / Secret Store
    participant R as Remote Runner
    participant P as Child Process

    A->>T: run-command(command references $GITHUB_TOKEN)
    T->>T: Tool Authorization
    T->>C: resolve environment(execution, agent)
    C->>S: read ordinary Variables
    C->>S: resolve allowed Secret values
    S-->>C: resolved environment
    C->>R: RunnerRequest + sensitive environment over WSS
    R->>P: spawn process with environment
    P-->>R: stdout / stderr
    R->>R: mask known Secret values
    R-->>T: sanitized result
    T-->>A: Tool Result
~~~

这样远端 Runner 即使位于另一台服务器或 Mac：

> 也不需要“读取 Project Secret Store”；它只需要在 Central 下发执行请求时接收本次进程所需的 environment。

## 9. Runner Protocol Environment Payload

对于会启动 OS process 的 Runner operation，RunnerRequest 增加可选敏感字段：

~~~text
RunnerRequest
├── project_id
├── mount_id
├── execution_id
├── effective_permissions
├── environment
│   ├── variables
│   │   └── NAME = value
│   └── secrets
│       └── NAME = plaintext
├── audit_correlation_id
└── operation_payload
~~~

其中：

- `variables` 是普通 Project Variable；
- `secrets` 只包含当前 Agent 白名单允许的 Secret；
- `environment` 只发送给需要 process environment 的 operation；
- Filesystem Tool 不需要携带环境变量。

`environment.secrets` 是 Runner Protocol 的敏感字段：

- Central request logging 不得记录其 plaintext；
- WebSocket frame debug logging 不得记录其 plaintext；
- Runner request logging 不得记录其 plaintext；
- Trace / Audit 只记录变量名、数量、关联 ID 等非敏感 metadata。

## 10. 跨设备传输安全

Central 与 Runner 的 Control Channel 已经使用：

~~~text
WSS
= WebSocket over TLS
~~~

并且 Runner 通过设备身份完成认证。

因此第一阶段：

> Project Secret 通过已认证的 WSS Control Channel 传输，不再叠加一套应用层 Secret 加密协议。

TLS 负责传输机密性和完整性。

如果未来 Runner Protocol 引入不具备同等安全保证的 Broker / Relay / Data Channel，则对应通道必须重新满足：

- authenticated endpoint；
- confidentiality；
- integrity；
- Secret payload 不被中间基础设施明文持久化。

## 11. Runner 端生命周期

### 11.1 run-command

Runner 收到 request 后：

1. 在当前 request 内存中读取 environment；
2. 合并 Runner base environment；
3. 启动 child process；
4. child process 继承 environment；
5. process 结束；
6. Runner 丢弃本次 request 的 environment；
7. 不把 Secret 写入磁盘配置。

### 11.2 start-process

Managed Process 的 environment 在创建时注入。

进程启动后：

- Secret 已经属于该 process 的运行环境；
- 后续 Secret 更新不会修改已经存在的进程；
- 如果要使用新 Secret value，需要重新启动 Managed Process。

Runner 的 Managed Process metadata 可以记录：

~~~text
environment variable names
secret variable ids / names
environment revision metadata
~~~

但不能记录 Secret plaintext。

### 11.3 Runner 重启

Runner 不持久保存 Project Secret plaintext。

因此如果 Runner 自身重启：

- 已结束的 command 不需要恢复 environment；
- 需要重新创建的 Managed Process 必须由 Central 再次下发 execution request，并重新解析当前 Secret；
- Runner 不依赖本地 Secret cache 恢复 Project 进程。

## 12. Runner 是 Secret 使用边界的一部分

当 Project Owner：

1. 把 Secret 加入 Agent 白名单；
2. 给 Agent 配置某个 Runner / Mount；
3. Agent 使用 `run-command` 或 `start-process`；

那么对应 Secret plaintext 会进入该 Runner 上的目标 process。

因此：

> 允许 Agent 在某个 Runner 上使用 Secret，就意味着信任该 Runner 设备和该设备上的目标执行环境处理这个 Secret。

Runner 主机管理员、root 权限进程或目标 child process 本身，都可能在 OS 层读取该 Secret。

这不是 Prompt masking 可以隔离的边界。

Secret 白名单控制“Agent 能不能使用 Secret”；Runner enrollment / mount 控制“Agent 能在哪个受信任设备上执行”。

## 13. 命令生成规则

Agent 应尽量让 Tool arguments 保留变量引用：

~~~text
gh api /repos/... --header "Authorization: Bearer $GITHUB_TOKEN"
~~~

而不是：

~~~text
gh api /repos/... --header "Authorization: Bearer ghp_xxx..."
~~~

因为 Model 不知道 Secret value，所以正常情况下也不会产生后一种 command。

普通 Variable 同样可以直接引用：

~~~text
curl "$API_BASE_URL/health"
~~~

## 14. 日志与 Masking

Central 与 Runner 都知道本次 process 注入了哪些 Secret value。

因此 stdout、stderr、Tool Result 在：

- 返回 Agent Model 前；
- 写入普通 Execution Log 前；

都必须对已知 Secret value 做 masking：

~~~text
actual-secret-value
-> ***
~~~

典型场景：

~~~text
echo "$GITHUB_TOKEN"
~~~

返回：

~~~text
***
~~~

Masking 是防止意外泄漏的保护，不是严格 DLP。

例如 Agent 如果主动执行：

~~~text
base64 "$GITHUB_TOKEN"
~~~

输出已经不是原始 Secret value，简单字符串 masking 无法可靠识别。

因此安全语义保持：

> Secret 加入 Agent 白名单 = 允许该 Agent 的执行环境使用该 Secret。

不能把 masking 当成权限隔离机制。

## 15. MCP / Backend Credential

Project Secret 也可以作为 Project-scoped backend configuration 的 credential reference：

~~~text
MCP Server Config
  -> credential_ref
      -> Project Secret
          -> MCP Bridge resolve
~~~

这个关系与 Agent Secret 白名单不同：

~~~text
Agent allowed_secret_variables
= Agent 可以在自己的执行环境使用 Secret

MCP credential_ref
= MCP Backend 自己使用 Secret
~~~

MCP Backend 使用某个 Secret，不代表该 Secret 会进入 Agent Prompt 或 Runner environment。

如果 Agent 还需要在 Runner command 中直接使用同一个 Secret，则仍然需要把该 Secret 加入 Agent 白名单。

## 16. 创建、更新与删除

### 创建

Project Owner 创建 Variable / Secret。

Secret 明文只在写入请求和加密过程短暂存在。

### 更新普通 Variable

更新后：

- 新 Agent Execution Prompt 使用新值；
- 后续新 command / process 注入新值；
- 已运行 process 不变。

### 更新 Secret

覆盖 encrypted value。

Agent 白名单继续引用同一个 Secret ID，因此无需重新配置。

后续新 command / process 使用新值。

### 删除

删除 Variable / Secret 后：

- 新 Agent Execution 不再看到该变量；
- 后续 Runner process 不再注入；
- Agent 白名单中的失效 Secret reference 应被清理或标记 unavailable；
- 已经运行中的 OS process 不会因为 Central 删除变量而自动移除其已有 environment。

## 17. UI

Project 配置页面：

~~~text
Environment Variables

Name             Type       Description
API_BASE_URL     Variable   Backend API base URL
GITHUB_TOKEN     Secret     GitHub repository API token
NPM_TOKEN        Secret     npm registry token
~~~

普通 Variable：

- 可以查看 value；
- 可以编辑；
- 可以删除。

Secret：

- 保存后只显示 masked value；
- 不提供“读取明文”；
- 可以覆盖更新；
- 可以删除。

Agent 配置页面：

~~~text
Available Secrets

[x] GITHUB_TOKEN
[ ] NPM_TOKEN
[ ] DEPLOY_KEY
~~~

只需要选择 Secret。

普通 Variable 不需要 Agent 级配置。

## 18. 第一阶段实现边界

第一阶段确定：

1. Project 支持 Variable / Secret 两类 Environment Variable；
2. Variable / Secret 都有 name、description；
3. 普通 Variable 对 Project 内所有 Agent 自动可见；
4. Secret 使用 Agent `allowed_secret_variables` 白名单；
5. Secret value 加密存储，保存后不可通过普通 API 读取；
6. AgentExecutionContext 和 Prompt 包含普通 Variable 的 name / description / value；
7. AgentExecutionContext 和 Prompt 只包含 Agent 可用 Secret 的 name / description，不包含 value；
8. Runner `run-command / start-process` 在进程启动前由 Central 解析环境变量；
9. Central 通过已认证 WSS Runner Protocol 把本次 process environment 临时下发给远端 Runner；
10. Runner 不同步、不缓存整个 Project Secret Store；
11. Runner 不持久保存 Project Secret plaintext；
12. Managed Process 在创建时获得 environment，Secret 更新后需要重启进程才能使用新值；
13. Secret payload 不进入 request / frame / trace 普通日志；
14. stdout / stderr / Tool Result 对已知 Secret value 执行 masking；
15. Project-scoped backend 可以通过 credential_ref 引用 Project Secret；
16. Agent Secret 白名单不再细分 Runner / MCP / Tool usage scope。
