# Unified Tool Runtime 详细设计

> 状态：设计稿
>
> 上层架构：
> - [统一工具系统架构](./index.md)
> - [MCP 集成架构](../mcp-integration/index.md)
>
> 相关架构：
> - [Agent Executor 架构](../agent-executor/README.md)
> - [Agent Loop 架构](../agent-loop.md)
> - [Runner 架构](../runner.md)
> - [安全与治理架构](../security-governance/index.md)
>
> 相关详细设计：
> - [Approval Scope 详细设计](../security-governance/approval-scope.md)
> - [One-time Approval、Tool Retry 与 Idempotency 详细设计](../security-governance/one-time-approval-retry-idempotency.md)
> - [MCP Tool 默认启用策略详细设计](../mcp-integration/mcp-tool-default-enable.md)

## 1. 设计范围

Unified Tool Runtime 是 Agent Loop 与各种 Tool Backend 之间的统一运行时边界。

它负责把 Builtin、Runner、MCP 等不同来源的能力统一成稳定的 Tool 定义、调用、结果和错误模型，并提供一致的：

- Tool Registry；
- Execution Tool Set；
- model-visible Tool projection；
- Tool Call 解析与路由；
- Tool Authorization 接入；
- operation / attempt 生命周期；
- timeout / cancellation；
- 结果标准化；
- 错误标准化；
- 并发控制边界；
- execution snapshot 与可追溯性。

本文不重新定义：

- Agent Capability / Execution Policy / Approval 的安全规则；
- Tool Backend 自己的业务合法性；
- Runner RPC 协议；
- MCP Protocol；
- Model Provider 的 tool schema 格式；
- Agent Loop 的整体循环控制。

这些模块只通过本文定义的统一 Tool Contract 与 Tool Runtime 交互。

## 2. 设计参考与核心取舍

成熟 Agent Runtime 通常会把以下概念分开：

~~~text
模型能否产生多个 Tool Call
!=
Runtime 是否并行执行 Tool Call
~~~

同时也会区分：

~~~text
Tool 稳定身份
!=
模型本轮看到的 Tool 名称
!=
Backend 实际调用地址
~~~

错误处理也应区分：

~~~text
Tool 可恢复错误
-> 作为 Tool Result 返回给模型
-> Agent 可以继续推理

Runtime / Integrity Error
-> 当前 Tool Operation 无法形成可信结果
-> 由 Agent Loop / Agent Execution 决定是否终止
~~~

本文采用以下核心原则：

1. Agent Loop 只依赖 Unified Tool Runtime，不理解 Builtin / Runner / MCP 的具体执行协议。
2. Tool 的长期身份使用 stable tool id；模型可见名称只是当前 Execution 的 projection。
3. ToolSpec 只描述 Tool 本身的稳定语义定义；ToolBinding 描述如何执行，ToolRuntimeState 描述当前运行状态，三者组合为 Registry 中的 RegisteredTool；ExecutionTool 是当前 Execution 的不可变运行时 projection。
4. 每个模型产生的 Tool Call 对应一个逻辑 Tool Operation。
5. 一个 Tool Operation 可以有多个底层 Attempt，但不能通过 retry 变成新的逻辑操作。
6. Approval 绑定 Operation；retry safety 由 Backend 的执行语义决定。
7. Tool Result 必须能表达结构化结果、文本结果、Artifact 引用和错误，而不是只返回一个字符串。
8. Tool Backend 的内部异常不能直接泄漏给模型；模型只接收安全、结构化的 Runtime Result。
9. Tool concurrency 是 Runtime 决策，不能仅根据模型是否支持 parallel tool calls 自动并行。
10. Execution 启动后的 Tool identity / schema / routing snapshot 应保持可追溯，不被后续配置变更静默改写。

## 3. 总体结构

~~~mermaid
flowchart LR
    Provider["Tool Providers<br/>Builtin / Runner / MCP"]
    Registry["Tool Registry"]
    Resolver["Execution Tool Resolver"]
    ExecSet["Execution Tool Set"]
    Projection["Model Tool Projection"]
    Model["Model Adapter"]
    Loop["Agent Loop"]
    Runtime["Tool Runtime"]
    Auth["Tool Authorization"]
    Dispatcher["Tool Dispatcher"]
    Backend["Builtin / Runner / MCP Backend"]

    Provider --> Registry
    Registry --> Resolver
    Resolver --> ExecSet
    ExecSet --> Projection
    Projection --> Model
    Model --> Loop

    Loop -->|"Unified Tool Call"| Runtime
    Runtime --> Auth
    Auth --> Dispatcher
    Dispatcher --> Backend
    Backend -->|"Backend Result"| Dispatcher
    Dispatcher --> Runtime
    Runtime -->|"Unified Tool Result"| Loop
~~~

运行时逻辑分为五层：

~~~text
Definition
└── ToolSpec

Registration
└── RegisteredTool
    ├── ToolSpec
    ├── ToolBinding
    └── ToolRuntimeState

Execution Resolution
└── ExecutionTool

Invocation
├── ToolCall
├── ToolOperation
└── ToolAttempt

Result
├── ToolResult
└── ToolError
~~~

### 3.1 单次 Tool Call 的完整运行时序

一次 Tool Call 从 Agent Loop 进入 Unified Tool Runtime 后，由 Runtime 完成 Tool 解析、Operation 生命周期管理，并在进入实际 Backend 前调用 Security / Governance 进行统一授权。

这里不展开 Security / Governance 内部的 Capability、Execution Policy、Approval Match、Approval Policy、Human Inbox、Audit 等流程；Unified Tool Runtime 只把它视为一个独立的授权系统，并消费它返回的授权结果。

~~~mermaid
sequenceDiagram
    participant L as Agent Loop
    participant R as Tool Runtime
    participant E as Execution Tool Set
    participant S as Security / Governance
    participant D as Tool Dispatcher
    participant B as Tool Backend

    L->>R: execute Unified ToolCall

    R->>E: resolve model-visible tool name
    E-->>R: ExecutionTool snapshot

    R->>R: validate arguments
    R->>R: create ToolOperation

    R->>S: Tool Authorization(operation)

    alt allow
        S-->>R: allow
        R->>R: create ToolAttempt
        R->>D: dispatch ExecutionTool + Operation + Attempt
        D->>B: backend request
        B-->>D: backend result
        D-->>R: normalized BackendResult

        alt Backend 成功
            R->>R: validate / normalize ToolResult
            R-->>L: Unified ToolResult
        else Backend 失败或结果不确定
            R->>R: classify error / outcome / retry safety

            opt 满足安全 technical retry 条件
                R->>R: create next ToolAttempt
                R->>D: retry same ToolOperation
                D->>B: backend retry
                B-->>D: backend result
                D-->>R: normalized BackendResult
            end

            R-->>L: ToolError / Runtime Failure
        end

    else waiting_for_approval
        S-->>R: waiting_for_approval
        R->>R: ToolOperation -> waiting_for_approval
        Note over R,S: Security / Governance 内部处理审批流程

        S-->>R: authorization resolved

        alt approved
            R->>R: resume same ToolOperation
            R->>R: create ToolAttempt
            R->>D: dispatch ExecutionTool + Operation + Attempt
            D->>B: backend request
            B-->>D: backend result
            D-->>R: normalized BackendResult
            R-->>L: Unified ToolResult / ToolError
        else rejected
            R-->>L: ToolError
        end

    else deny
        S-->>R: deny
        R-->>L: ToolError
    end
~~~

这张图只表达 Unified Tool Runtime 自己需要关心的外部关系：

- Agent Loop 把 Unified ToolCall 交给 Tool Runtime；
- Tool Runtime 通过 Execution Tool Set 解析当前 Execution 固定的 Tool identity、schema 和 backend binding；
- Tool Runtime 创建并维护 ToolOperation / ToolAttempt；
- Security / Governance 作为独立系统负责 Tool Authorization，Runtime 只消费 `allow / waiting_for_approval / deny`；
- `waiting_for_approval` 期间保持同一个 ToolOperation，审批完成后继续或结束该 Operation；
- Tool Dispatcher 负责把已授权 Operation 路由到 Builtin / Runner / MCP Backend；
- Backend technical retry 仍属于同一个 ToolOperation，只创建新的 ToolAttempt；
- Tool Backend 返回的原始结果先由 Dispatcher / Runtime 标准化，再返回 Agent Loop。

Security / Governance 自身的授权、审批和审计内部时序由对应详细设计文档定义，不在 Unified Tool Runtime 中重复展开。

## 4. 详细设计分册

Unified Tool Runtime 的详细设计按职责拆分为以下分册：

- [Tool Definition & Registry 详细设计](./tool-definition-registry.md)：Stable Tool Identity、ToolSpec、ToolBinding、ToolRuntimeState、RegisteredTool、Registry、Availability、spec_revision、Execution Tool Set 与 model-visible projection；
- [Tool Execution 详细设计](./tool-execution.md)：ToolCall、参数校验、ToolOperation / ToolAttempt、Authorization 接入、Dispatcher、timeout / cancellation、retry / idempotency、并发、Unknown Tool Call 与运行记录持久化；
- [Tool Result & Backend 详细设计](./tool-result-backend.md)：Unified Tool Result、Tool Error、Runtime Failure、result size / truncation、Sensitive Data、Artifact / StoredObject、Backend Contract 以及 Builtin / Runner / MCP Backend。

本文件继续作为 Unified Tool Runtime 的总设计入口，描述整体职责、完整调用时序以及与 Agent Loop、Model Adapter、Security / Governance 的边界。

## 5. Tool 与 Model Adapter 的边界

Tool Runtime 与 Model Adapter 边界固定为：

~~~text
Tool Runtime
-> ExecutionTool / ToolSpec projection
-> Model Adapter
-> Provider-native tool definition

Provider-native Tool Call
-> Model Adapter
-> Unified ToolCall
-> Tool Runtime

Unified ToolResult
-> Model Adapter
-> Provider-native tool result
~~~

因此：

- Tool Runtime 不知道 OpenAI function call JSON；
- Tool Runtime 不知道 Anthropic tool_use block；
- MCP Bridge 不直接生成 OpenAI / Anthropic tool schema；
- Model Adapter 不执行 Tool Authorization；
- Model Adapter 不调用 Tool Backend。

## 6. Tool Runtime 与 Agent Loop 的边界

Agent Loop 负责：

- 把 Execution Tool Set 交给 Model Contract；
- 接收一个或多个 Unified ToolCall；
- 调用 Tool Runtime；
- 根据 Tool Result 继续下一轮模型调用；
- 根据 Runtime failure / cancellation 决定 Execution 后续行为。

Tool Runtime 负责：

- 每个 Tool Call 的完整执行生命周期；
- Authorization 接入；
- Backend routing；
- retry safety；
- result / error normalization。

Tool Runtime 不决定 Agent 的业务目标，也不决定 Task 是否完成。

## 7. 第一阶段数据对象建议

第一阶段至少需要形成以下明确对象：

~~~text
ToolSpec
ToolBinding
ToolRuntimeState
RegisteredTool
ExecutionTool
ToolCall
ToolOperation
ToolAttempt
ToolResult
ToolError
ToolArtifactRef
~~~

不要求这些对象全部一一对应独立数据库表。

建议：

- ToolSpec：保存或生成稳定 Tool 定义；
- ToolBinding：保存或生成 Backend 执行绑定；
- ToolRuntimeState：保存或计算当前动态状态；
- RegisteredTool：作为 Registry 中的组合对象，不要求独立持久化；
- ExecutionTool：作为 Agent Execution context / snapshot 的一部分持久化；
- ToolOperation：持久化；
- ToolAttempt：持久化或作为 Operation 子记录；
- ToolResult：主体写 Execution Log；Tool Artifact 业务 metadata 与 StoredObject 引用持久化，实际 payload 由 ObjectStorageService 保存；
- ToolError：随 Operation / Attempt 保存标准化 metadata。

## 8. 第一阶段实现边界

第一阶段建议实现：

1. stable tool id；
2. Unified ToolSpec；
3. ToolBinding；
4. ToolRuntimeState；
5. RegisteredTool / Tool Registry；
6. Tool availability；
7. ToolSpec spec_revision；
8. Execution Tool Set snapshot；
9. model-visible name 映射；
10. input schema validation；
11. Tool Operation / Attempt；
12. 统一 Authorization 接入；
13. Builtin / Runner / MCP Dispatcher；
14. Unified ToolResult；
15. Unified ToolError；
16. timeout / cancellation propagation；
17. retry classification 与 unknown outcome；
18. 同一 model turn 中 read-only Tool 的并行执行能力；
19. Tool Artifact -> StoredObject 统一对象引用机制；
20. Execution Log / Audit correlation。

第一阶段不要求：

- 自动 dependency graph 调度多个 Tool Call；
- speculative Tool execution；
- fuzzy Tool name resolution；
- 自动学习 retry / idempotency；
- Tool result semantic cache；
- 通用 workflow engine；
- 把所有 Backend 自己的 metadata 暴露给 Agent Model。
