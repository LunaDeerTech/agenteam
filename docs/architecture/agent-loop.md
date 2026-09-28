# Agent Loop 架构

## 1. 定位

Agent Loop 是 Agent Execution 内部负责模型调用、Tool Calling、结果回填和循环控制的核心执行机制。

它不是独立服务，也不拥有自己的持久化生命周期。

关系如下：

```text
Agent Executor
  -> Agent Execution
      ├── Execution Context
      ├── Agent Loop
      ├── Model / Tools
      ├── Runtime View
      └── Logs / Usage
```

Agent Executor 负责创建 Agent Execution 并准备 AgentExecutionContext；Agent Loop 只消费已经准备好的 Execution Context，完成实际的 Agent 推理与工具交互。

## 2. Agent Loop 职责

Agent Loop 负责：

- 接收 AgentExecutionContext；
- 将 Execution Context 组装为模型输入；
- 调用 Model Adapter；
- 接收和解析模型输出；
- 处理 tool calls；
- 通过统一 Tool System 执行工具；
- 将 Tool Result 回填到消息上下文；
- 重复执行模型 / Tool 循环；
- 维护当前执行中的消息上下文；
- 管理 context window；
- 处理模型和 Tool 错误；
- 支持 retry；
- 响应 cancel；
- 实现单轮 Model generation watchdog；
- 维护可 checkpoint 的 runtime state；
- 记录 usage 和内部运行证据；
- 产生 Runtime Item semantic update；
- 向 Agent Executor 报告正常 completion 或不可恢复错误。

Agent Loop 不负责：

- Task 调度；
- Meeting turn 调度；
- 选择 Agent；
- 创建 Agent Execution；
- 读取 Task / Sprint / Milestone 业务数据；
- 读取 Meeting 业务数据；
- 解析 Agent 持久化配置；
- 选择 Model；
- 计算长期 Tool Capability；
- 决定 Task 的业务状态；
- 持久化 Agent Execution lifecycle。

## 3. 输入：AgentExecutionContext

Agent Loop 的唯一启动输入是已经准备好的 AgentExecutionContext。

概念结构：

```text
AgentExecutionContext
├── execution_id
├── platform_prompt
├── agent
│   ├── description
│   ├── instructions
│   ├── inject_agents_md
│   └── capability_snapshot
├── base_context
├── trigger_context
├── environment_context
│   ├── runner / mount metadata
│   └── project_variables
├── model
├── tools
├── execution_policy
└── metadata
```

Agent Loop 不根据 trigger 类型重新查询业务数据库补充启动上下文。

Task、Meeting 等业务信息由 Trigger Context Provider 在 Agent Executor 阶段准备。Trigger Context 中还可以包含对应场景的 Prompt Template / instructions，例如 Task 的状态流转与收尾规则，或 Meeting 的发言、决策与审批规则。

## 4. Context Assembly

Agent Loop 启动时首先将 AgentExecutionContext 组织成真正发送给 Model Adapter 的模型上下文。

在这个阶段才进行最终的 **Prompt Assembly**。此前 AgentExecutionContext 中保存的只是不同来源的 Prompt components，不应分别称为 System Prompt。

主要 Prompt components 包括：

- Platform Prompt；
- Agent `instructions`；
- Trigger-specific Prompt / instructions；
- 可选项目 `AGENTS.md` 中需要作为系统指令表达的内容；
- environment / execution policy 中需要作为系统指令表达的约束；
- 当前 Agent 可用的 Project Environment Variables。

Agent Loop 将这些 Prompt components 按稳定规则组合后，形成最终 **System Prompt**，再与业务上下文、消息历史和 ToolSpec 一起交给 Model Adapter。

```text
Platform Prompt
      +
Agent instructions
      +
Trigger Prompt / instructions
      +
Project / AGENTS.md instructions
      +
Project Environment Variables
      +
Environment / Execution constraints
      ↓
Prompt Assembly
      ↓
Final System Prompt
      ↓
Model Adapter
```

除 Prompt 外，模型上下文还可以包含：

- Agent 配置快照；
- 项目基础信息；
- prepared trigger context；
- Runner / mount 环境信息；
- Project Environment Variables：
  - 普通变量提供 name、description 和 value；
  - 当前 Agent 被允许使用的 Secret 只提供 name、description 和 Secret 标记，不提供 value；
- execution metadata；
- conversation / execution messages；
- ToolSpec。

Task trigger 的 prepared context 可能包含：

- Task 必要信息；
- Milestone title / description；
- Sprint title / description；
- work / review purpose。

Meeting trigger 的 prepared context 可能包含：

- meeting topic；
- participants；
- rolling summary；
- Meeting References 的稳定 typed identity；
- 当前 Execution 可见、按 Meeting Timeline 顺序排列的 MeetingMessage history；
- Meeting policy。

Context Assembly 只负责组织已有输入，不负责业务数据检索。

这里的 **System Prompt** 专指 Prompt Assembly 完成后的最终模型系统级指令。Platform Prompt、Agent instructions、Task/Meeting Prompt 等在组装前都只是 Prompt component，不单独称为 System Prompt。

## 5. 不自动注入的内容

以下内容不作为固定 Execution Context 自动检索：

- Knowledge Retrieval；
- Agent Memory recall；
- 大量历史项目文档；
- 历史 Agent Execution logs；
- 与当前 trigger 无关的全量 Task / Meeting 历史。

Agent 应在 Loop 中根据需要主动调用 Tools 获取这些信息。

虽然 Knowledge Retrieval 和 Agent Memory 不会自动注入，但每个 Agent 都会通过 Platform Prompt 获知这些能力：需要项目背景时主动检索 Knowledge，遇到困难时可以 recall 自己的 Memory，形成可复用经验后可以 retain。具体 Tool 内容仍然只在 Agent 实际调用后进入当前 Execution Context。

这样可以避免：

- 每次执行无条件产生大规模检索；
- Context 被低相关信息占满；
- Agent 无法决定真正需要的信息；
- Knowledge / Memory 与 Agent Loop 强耦合。

## 6. 核心循环

Agent Loop 的基础流程：

```mermaid
flowchart TB
    Context["Prepared Model Context"]

    Context --> Model["Call Model"]
    Model --> Result{"Model Output"}

    Result -->|"Tool Calls"| ToolCalls["Validate / Execute Tool Calls"]
    ToolCalls --> ToolResults["Append Tool Results"]
    ToolResults --> Continue{"Continue?"}
    Continue -->|Yes| Model

    Result -->|"Final Response"| Output["Complete Current Loop"]

    Model -->|"Error"| Error["Error Handling"]
    ToolCalls -->|"Error"| Error

    Error --> Recover{"Recoverable?"}
    Recover -->|Yes| Model
    Recover -->|No| Failed["Execution Failed"]

    Continue -->|No| Output
    Output --> Success["Execution Succeeded"]
```

Agent Loop 必须允许多轮 Model → Tool → Model 循环，而不是把一次 Model API 调用等同于一次 Agent Execution。

## 7. Loop 状态

Agent Loop 内部可以维护类似以下逻辑状态：

```text
initializing
  -> calling_model
  -> executing_tools
  -> waiting_external_input
  -> calling_model
  -> ...
  -> completing
  -> completed

or

  -> failed
  -> cancelled
```

这些状态主要用于当前 Agent Execution 内部的运行控制和 observability。

它们不需要成为与 Agent Execution 并列的独立业务实体。

Agent Execution 对外只暴露自身生命周期状态，例如：

```text
created
preparing
running
waiting
succeeded
failed
cancelled
```

其中 `waiting_external_input` 用于 DecisionRequest、Approval Request 等 Human-in-the-loop 场景。它不会结束当前 Agent Loop；外部结果返回后继续原来的 Model → Tool → Model 循环。

## 8. Model Adapter

Agent Loop 通过统一 Model Adapter 调用不同 Model Provider。

Model Adapter 负责：

- 将统一 message schema 转换为 Provider 原生格式；
- 将统一 ToolSpec 转换为 Provider function/tool schema；
- 发送模型请求；
- 处理 streaming；
- 解析文本输出；
- 解析 tool calls；
- 标准化 usage；
- 标准化 finish reason；
- 标准化模型错误。

Agent Loop 不应直接绑定某个具体 Provider 的协议。

第一阶段：

```text
Agent Loop
    -> Model Adapter
        -> OpenAI Chat Completions / OpenAI-compatible
        -> Anthropic Messages
```

后续可以继续增加其他 Provider / Protocol Adapter。Provider 差异应尽量被 Model Adapter 屏蔽。Provider / Model 配置、Resolved Model Snapshot、统一请求 / 响应、streaming、usage 与错误标准化的完整设计见 [Model System 详细设计](./platform-infrastructure/model-system.md)。

## 9. Tool Calling

模型产生 Tool Call 后，Agent Loop 通过统一 Tool System 执行。

```mermaid
sequenceDiagram
    participant L as Agent Loop
    participant T as Tool System
    participant E as Tool Backend

    L->>T: tool call
    T->>T: policy / authorization
    T->>E: execute
    E-->>T: result
    T-->>L: normalized Tool Result
    L->>L: append result to context
```

Agent Loop 只面对：

- Unified ToolSpec；
- normalized Tool Call；
- normalized Tool Result。

Builtin、Runner、MCP 等具体来源由 Tool System 处理。

## 10. Parallel Tool Calls

如果 Model Provider 支持一次返回多个独立 tool calls，Agent Loop 可以支持并行执行。

但并行执行必须考虑：

- Tool 是否声明为可并行；
- 是否存在写冲突；
- Tool 调用之间是否有隐式依赖；
- 是否共享同一 Runner / workspace；
- 是否涉及需要串行审批的操作。

第一阶段可以保守地：

- 读操作允许并行；
- 写操作默认串行；
- 未明确声明可并行的 Tool 默认串行。

具体并发策略可以在 Tool System 设计中继续细化。

## 11. Tool Result 回填

Tool Result 需要以统一结构回填给模型。

至少应区分：

- success；
- business error；
- authorization denied；
- approval required；
- technical error；
- cancelled；
- timeout。

不应把所有失败都转换成普通字符串。

Agent Loop 需要根据 Tool Result 类型决定：

- 继续交给模型判断；
- 自动 retry；
- 等待 approval；
- 终止执行；
- 向 Agent Executor 报告不可恢复错误。

## 12. 按需 Knowledge 与 Memory

Knowledge Base 与 Agent Memory 都作为 Tool 能力提供。

典型流程：

```mermaid
sequenceDiagram
    participant A as Agent Loop
    participant T as Tool System
    participant K as Knowledge Service
    participant M as Memory Service

    A->>T: query-doc(...)
    T->>K: search / retrieve
    K-->>T: relevant knowledge
    T-->>A: tool result

    A->>T: recall(...)
    T->>M: retrieve agent memory
    M-->>T: memories
    T-->>A: tool result

    opt useful experience discovered
        A->>T: retain(...)
        T->>M: store memory
    end
```

Knowledge 与 Memory 不是 Agent Loop 内部隐藏的自动注入步骤。

## 13. Context Window Management

长时间 Agent Execution 可能超过模型 context window。

Agent Loop 必须具备上下文窗口管理能力。

至少需要考虑：

- system / execution context 的保留优先级；
- 最近 Tool Calls / Tool Results；
- 历史消息压缩；
- 大型 Tool Result 外部化；
- artifact / file reference；
- rolling summary；
- token budget。

原则上：

- 核心 system / execution constraints 不应被压缩丢失；
- 大体积原始内容优先保存为 artifact，并在 Context 中保留 reference；
- 历史 Tool Result 可以按需要摘要；
- 不应因为 context compaction 改变业务事实。

具体 compaction 算法可在实现阶段继续设计。

## 14. Streaming 与 Runtime Item

Agent Loop 应支持模型 streaming。

Model Adapter 可以标准化产生：

- text delta；
- 可向上层展示的 reasoning / thinking delta 或 summary；
- tool call metadata；
- usage update；
- finish metadata。

Agent Loop 不把这些原始增量直接当作持久化 Execution Event。

它们被转换为 Agent Executor Runtime View 使用的 semantic update：

```text
Model / Tool streaming
        ↓
Agent Loop semantic update
        ↓
Runtime Item Manager
        ↓
RuntimeItem Snapshot + RuntimeItemUpdate Stream
```

Text 与 Reasoning 可以通过 RuntimeItemUpdate 流式更新同一个 Runtime Item；Tool Call 也持续更新同一个 Tool Runtime Item，而不是产生大量日志行。

Task、Meeting、独立 Execution Detail 等消费者复用同一个 Runtime View contract。

完整设计见 [Agent Executor Runtime View](./agent-executor/runtime-view.md)。

Meeting 中最终公开消息仍由 Meeting Domain 在对应业务流程中写入，不能把 Runtime View 中的 Text Item 直接等同于 MeetingMessage。

## 15. Error Handling

错误至少需要分类：

### Model Error

例如：

- rate limit；
- provider unavailable；
- invalid request；
- context too large；
- model timeout。

### Tool Error

例如：

- business validation error；
- authorization denied；
- Runner unavailable；
- MCP failure；
- command failure。

### Loop Error

例如：

- invalid tool call format；
- repeated invalid calls；
- tool loop runaway；
- internal state corruption。

错误类型不同，恢复策略也应不同。

## 16. Retry

Agent Loop 可以对明确可恢复的底层错误进行 retry，但 Model retry 与 Tool retry 使用不同策略：

- Model timeout / network / provider unavailable 等由 Model System 的 progressive timeout + retry policy 处理；
- provider rate limit 等 retryable Model Error 可以结合 backoff；
- Tool System 明确标记为 retryable 的 transient Tool transport error 按 Tool Operation / Attempt 规则处理。

Model Request timeout 达到最大单次 timeout 后仍可继续使用该上限重试，只要 Execution 仍 active 且未被 cancel；它不受统一的“最大重试次数”规则约束。

对于 Tool retry，Agent Loop 不自行根据“Tool 名称和 arguments 看起来相同”判断是否属于 retry。

Tool System 负责维护：

~~~text
Tool Call
  -> Tool Operation (operation_id)
      -> Attempt 1
      -> Attempt 2
~~~

同一个 Tool Operation 的 technical retry 保持 operation_id 不变，只创建新的 Attempt。

如果当前 Operation 已经通过 One-time Approval，安全的 technical retry 不重新请求 Approval。

但 Approval 不代表 retry 安全；Tool System 仍必须根据 Tool / Backend 的 idempotency 与 outcome 决定是否允许 retry。

尤其：

~~~text
unknown + non-idempotent
-> 不允许自动 retry
~~~

不应自动 retry：

- 业务校验失败；
- 权限拒绝；
- Human Approval required；
- Agent 明确产生了新的错误业务动作；
- Tool System 未明确允许 retry 的 unknown outcome；
- 无限重复的相同 Tool Operation。

Tool technical retry 必须：

- 有明确次数上限；
- 支持 backoff；
- 保持同一 operation_id 与 operation fingerprint；
- 为每次实际执行创建新的 attempt / backend request id；
- 写入 Agent Execution log；
- 计入 usage / duration。

Model retry 的次数与 progressive timeout 规则不复用 Tool retry 上限，具体见 Model System。

完整设计见 [One-time Approval、Tool Retry 与 Idempotency 详细设计](./security-governance/one-time-approval-retry-idempotency.md)。

## 17. 单轮 Model Generation Watchdog

Agent Execution 不设置全局 wall-clock deadline，也不因为运行时间长自动停止。

为了防止某些模型在单次 generation 中陷入持续循环输出，每一轮 Model generation 需要有 watchdog。

当单轮持续时间超过配置阈值：

1. 中止当前 generation；
2. 保留已经允许上层使用的必要输出；
3. 向当前 Agent conversation 插入系统提示，说明上一轮因持续时间过长 / 疑似循环输出被中止；
4. 开始下一轮 Agent Loop。

watchdog 只终止当前轮，不结束 Agent Execution。

阈值属于 Agent Loop 配置，不是 Agent Execution 总时长限制。

## 18. Cancel 与局部 Timeout 边界

Agent Loop 必须响应 Agent Executor 发出的 cancel / shutdown。

收到 cancel 后：

1. 不再发起新的 Model / Tool 工作；
2. 尽可能取消正在进行的 Model Request；
3. 尽可能取消可取消的 Tool Call；
4. 释放执行资源；
5. 向 Agent Executor 报告已经停止。

Agent Execution 本身没有自动 timeout 终止。

不同局部 timeout 分别处理：

- Tool timeout：由 Agent / Tool Contract 决定，作为 Tool Result 交回 Agent Loop；
- Model timeout：由 Model Adapter 的渐进 timeout / retry 策略处理；
- 单轮 generation timeout：由上一节 watchdog 处理，只进入下一轮。

Agent Loop 本身不创建新的 Agent Execution 进行重试。

## 19. Completion

Agent Loop 不返回统一的业务 `Agent Execution Result`。

正常结束时只向 Agent Executor 报告：

```text
completion = normal
```

Agent Executor 据此将 Execution 标记为 succeeded。

Agent 的实际业务结果通过 Tool / Domain Service 写入相应领域。例如 Task 的 `in-review / done / blocked` 必须通过 Task Tool 和 Domain 状态机形成事实。

如果 Agent 最后产生普通文本，它作为 Runtime View 的 TextRuntimeItem 保存，而不是额外复制成 `final_output` 业务结果。

不可恢复错误则返回结构化失败原因，由 Agent Executor 进入 failed。

## 20. Logs、Runtime View 与 Observability

Agent Loop 同时产生两类不同信息：

```text
Runtime semantic updates
-> Runtime View
-> 用户理解 Agent 做了什么

Internal execution evidence
-> Model Invocation / Tool Operation / Token Usage / Log
-> 诊断与审计
```

Runtime View 只保存 text、reasoning、tool、interaction、notice/error 等 UI-facing Runtime Item。

checkpoint tick、Provider request id、完整 ToolAttempt、内部 retry detail 等不应为了 UI 全部转成 Runtime Item。

Task Event、MeetingMessage 和 Audit Log 仍保存各自领域事实，并可引用 Agent Execution ID。

每次真实 Provider invocation 的 Token Usage Source of Truth 见 [Model Token Usage 详细设计](./platform-infrastructure/model-token-usage.md)。

Runtime View 完整设计见 [Agent Executor Runtime View](./agent-executor/runtime-view.md)。

## 21. Harness 参考与实现策略

当前架构基线是：**agenteam 自行实现 Agent Loop，不直接集成或封装完整第三方 Agent Harness 作为核心执行基础。**

可以研究成熟 Agent Harness，例如：

- pi；
- opencode。

重点借鉴：

- Agent Loop 控制流程；
- provider abstraction；
- tool/function calling；
- streaming；
- cancellation；
- retry / error recovery；
- context compaction；
- usage tracking；
- logging / tracing。

这些项目是设计和实现参考，不是 agenteam 的既定运行时依赖。

未来如果局部组件具有明确复用价值，可以通过适配层引入，但不改变：

```text
Agent Executor owns Agent Execution lifecycle
Agent Execution owns execution context / logs / result
Agent Loop performs model-tool iteration
```
