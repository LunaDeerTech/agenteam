# Agent Loop 架构

> 详细设计：
> - [Agent Loop Runtime](./loop-runtime.md)
> - [Transcript 与 Model Context](./transcript-context.md)
> - [Context Window 与 Compaction](./context-compaction.md)
>
> 相关架构：
> - [Agent Executor](../agent-executor/README.md)
> - [Execution Context](../agent-executor/execution-context.md)
> - [Execution Lifecycle](../agent-executor/execution-lifecycle.md)
> - [Runtime View](../agent-executor/runtime-view.md)
> - [Model System](../platform-infrastructure/model-system/README.md)
> - [统一工具系统](../tool-system/README.md)
> - [Knowledge Base 与 Agent Memory](../knowledge-memory/README.md)

## 1. 定位

Agent Loop 是 Agent Execution 内部负责 Model → Tool → Model 迭代的核心执行机制。

它不是独立 Platform Service，也不拥有 Agent Execution 的持久化生命周期。

关系：

~~~text
Agent Executor
  -> Agent Execution
      ├── Immutable AgentExecutionContext
      ├── Agent Loop
      │   ├── AgentLoopController
      │   ├── TurnProcessor
      │   ├── Canonical Transcript
      │   └── Model Context Projector
      ├── Runtime View
      ├── Checkpoint
      └── Logs / Usage / Errors
~~~

Agent Executor 负责：

- 创建 Agent Execution；
- 在 preparing 阶段构造不可变 AgentExecutionContext；
- 管理 running / waiting / terminal lifecycle；
- 协调 checkpoint、resume、cancel 和 crash recovery。

Agent Loop 负责：

- 消费已经固化的 AgentExecutionContext；
- 组织最终模型上下文；
- 调用统一 Model Contract；
- 执行 Model Response 中的 Tool Calls；
- 回填 Tool Results；
- 按 Turn 推进；
- 管理 Canonical Transcript；
- 管理 context window 与 compaction；
- 向 Runtime View 产生 semantic update；
- 向 Agent Executor 报告 normal completion 或 unrecoverable failure。

## 2. 核心设计

Agent Loop 采用显式 Turn 模型，而不是把一次 Model API Request 等同于一次 Agent Execution。

一个 Turn 的基本语义是：

~~~text
Model Response
+
该 Response 产生的完整 Tool Batch
=
一个稳定 Turn
~~~

主循环：

~~~mermaid
flowchart TB
    Controller["AgentLoopController"]
    Context["Build Model Context"]
    Turn["TurnProcessor"]
    Model["Unified Chat Model"]
    Result{"Model Response"}
    Tools["Execute Tool Batch"]
    Stable["Stable Turn Boundary"]
    Decide{"Next Action"}
    Wait["Waiting"]
    Compact["Compaction"]
    Complete["Normal Completion"]
    Fail["Unrecoverable Failure"]

    Controller --> Context
    Context --> Turn
    Turn --> Model
    Model --> Result
    Result -->|"has tool calls"| Tools
    Tools --> Stable
    Result -->|"no tool calls"| Stable
    Stable --> Decide

    Decide -->|"continue"| Context
    Decide -->|"wait"| Wait
    Decide -->|"compact"| Compact
    Compact --> Context
    Decide -->|"complete"| Complete
    Decide -->|"fail"| Fail
~~~

每个 Turn 结束后的稳定边界统一判断：

- continue；
- wait；
- compact；
- complete；
- fail。

完整运行时设计见 [Agent Loop Runtime](./loop-runtime.md)。

## 3. AgentLoopController 与 TurnProcessor

Agent Loop 内部拆为两个主要控制层：

~~~text
AgentLoopController
    |
    ├── lifecycle-aware loop control
    ├── next-turn decision
    ├── context budget / compaction coordination
    ├── waiting / resume control input
    ├── watchdog / doom-loop guard
    └── cancel propagation
            |
            v
       TurnProcessor
            |
            ├── build one Model Request
            ├── consume Model stream
            ├── finalize Assistant response
            ├── validate Tool Calls
            ├── execute Tool Batch
            └── append stable results
~~~

TurnProcessor 只负责一个 Turn。

AgentLoopController 负责多个 Turn 之间的控制。

这样避免 Model stream、Tool execution、compaction、waiting 与 Execution lifecycle 全部堆在同一个大循环中。

## 4. 输入边界

Agent Loop 的唯一启动输入是已经准备完成的 AgentExecutionContext。

概念结构：

~~~text
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
~~~

AgentExecutionContext 一旦 preparing 完成并进入 running，即不可修改。

Agent Loop 不重新查询：

- Task；
- Meeting；
- Agent 长期配置；
- Model 配置；
- Project Environment Variables；
- AGENTS.md；
- Execution Tool Set。

这些内容全部以本次 Execution Snapshot 为准。

新增技能只通过正式持久化 Skill binding 在下一模型输入确定点进入运行目录，不重读全部 Agent 配置或扩展本次 Tool Set。初始绑定与已应用变更共同参与投影和恢复；当前请求/Tool Batch 不被改写，waiting 不因分配自动恢复，terminal 不复活。版本与移除的当前授权校验见 [Agent Skills](../agent-skills.md)。

运行过程中产生的：

- Model output；
- Tool Call / Tool Result；
- Approval / Decision result；
- compaction；
- control input；
- Runtime Item；
- checkpoint；

都属于 runtime state，不反向修改 AgentExecutionContext。

## 5. Prompt 与 Model Context

Execution Context 保存的是启动时的 Prompt components。

例如：

~~~text
Platform Prompt
+ Agent instructions
+ Trigger-specific instructions
+ AGENTS.md snapshot
+ Environment / execution constraints
~~~

Agent Loop 在模型调用边界统一组装 Final System Prompt。

但 Agent Loop 不把“最终发给模型的整个 Context”固化一次后永久 append。

每个 Turn 都从当前稳定 runtime state 生成本轮 Model Context：

~~~text
Immutable AgentExecutionContext
        +
Canonical Transcript
        +
Current Compaction Snapshot
        +
Persisted Skill bindings applied at the next model input boundary
        ↓
Model Context Projector
        ↓
Unified Model Messages
        +
Final System Prompt
        +
Execution Tool Set Snapshot
        ↓
Unified Chat Model Request
~~~

这里的“每 Turn 重新生成”只表示重新计算 model-visible projection。

它不表示每轮重新读取业务数据库或项目文件。

完整设计见 [Transcript 与 Model Context](./transcript-context.md)。

## 6. Canonical Transcript

Agent Loop 维护完整 Canonical Transcript。

Transcript 是：

> Agent Loop 为了正确构造下一轮 Model Context 而保存的稳定语义历史。

它不是：

- Runtime View；
- Audit Log；
- Tool Operation / Attempt log；
- Model Invocation log；
- Provider-native conversation object。

典型内容：

~~~text
TranscriptEntry
├── input
├── control
├── assistant
└── tool_result
~~~

Assistant entry 可以包含：

- text；
- reasoning / continuation metadata；
- tool_call。

Tool Result 必须稳定关联原 tool_call_id。

Canonical Transcript 保留完整历史；compaction 不删除或改写原始 Transcript。

完整 schema 与 Runtime View 边界见 [Transcript 与 Model Context](./transcript-context.md)。

## 7. Runtime View 是独立 Projection

Runtime View 面向用户，而 Canonical Transcript 面向 Agent Loop 和模型。

二者来自同一次运行事实，但目的不同：

~~~text
Model / Tool / Control Activity
            |
            v
      Turn Processor
        /       \
       /         \
      v           v
Canonical       Runtime View
Transcript      semantic update
    |                |
    v                v
Model Context       User UI
~~~

例如：

- Assistant streaming text 在 Transcript 中最终形成稳定 Assistant text part；
- 同一文本在 Runtime View 中可以通过多个 delta 更新同一个 TextRuntimeItem；
- Tool Call 与 Tool Result 在 Transcript 中是严格配对的 model-visible history；
- Runtime View 可以把它们合并成一个可折叠 ToolRuntimeItem；
- Tool retry evidence 不需要伪造成多条 Transcript message，也不需要全部显示在 Runtime View。

Runtime View 的 Source of Truth 和实时协议见 [Agent Executor Runtime View](../agent-executor/runtime-view.md)。

## 8. Model System 边界

Agent Loop 只依赖统一 Chat Model Contract。

它不直接理解：

- OpenAI Chat Completions；
- Anthropic Messages；
- Provider credential；
- Provider endpoint；
- Provider 原生 retry 细节。

Model System 负责：

- Unified Message → Provider schema；
- ToolSpec → Provider schema；
- streaming 标准化；
- finish reason；
- usage；
- Provider error；
- 同一 Agent 逻辑调用的 Model Request progressive timeout / retry，统一纳入 SDK/Adapter 的真实 attempts。

Agent Loop 负责：

- 决定什么时候调用模型；
- 提供本轮 model-visible messages；
- 消费统一 Model Response；
- 根据 Response 决定 Tool Loop 或 completion。

Loop 消费标准化流、结果与最终错误，不根据 retryable 再重试相同请求。不同 attempt 的 partial output 必须隔离，不盲拼回答、执行半截 Tool Call 或重放已发生/unknown 的工具副作用；取消阻止下一自动 attempt。模型自我修复的新 Turn、压缩后新输入与用户 retry/Scheduler relaunch 不属于这一请求重试，参数仍按责任规格落实。

完整 Provider contract 见 [Chat Model Runtime](../platform-infrastructure/model-system/chat-model-runtime.md)。

## 9. Tool System 边界

Agent Loop 面向：

- Execution Tool Set；
- Unified Tool Call；
- normalized Tool Result。

Builtin、Runner、MCP 等 Backend 由 Unified Tool Runtime 处理。

Agent Loop 不自己决定：

- Tool Authorization；
- Approval；
- Backend routing；
- Tool technical retry；
- Tool Operation / Attempt persistence；
- idempotency；
- unknown outcome recovery；
- Tool 并发安全规则。

同一个 Model Response 中的 Tool Batch 由 Tool Runtime 按其既定规则执行：

- read-only Tool Call 可以并行；
- 其他 Tool Call 默认串行；
- 结果始终按 tool_call_id 精确回填。

Agent Loop 只保证：

> 一个 Turn 在完整 Tool Batch settle 后才进入稳定 Turn Boundary。

## 10. 无效 Tool Call 与被截断 Tool Call

Unknown tool、arguments schema invalid 等安全可恢复错误不直接令 Execution failed。

默认流程：

~~~text
invalid Tool Call
    ↓
structured error Tool Result
    ↓
append to Transcript
    ↓
next Turn
    ↓
model self-correction
~~~

只有 Agent Loop 自身 invariant 损坏等不可恢复错误才直接 failed。

如果 Model Response 因 length / max_tokens 等原因被截断，并且包含 Tool Call：

- 该 Response 中的 Tool Calls 全部不执行；
- 为每个 Tool Call 产生结构化 error Tool Result；
- 下一 Turn 明确要求模型重新发起完整调用。

这样避免执行可能被截断的副作用参数。

## 11. Waiting 与 Control Input

第一阶段不提供通用交互式 steering / follow-up queue。

运行中只允许有限 typed control input，例如：

- Approval result；
- Decision result；
- watchdog notice；
- resume notice；
- recovery notice；
- 经 Executor 验证、持久化并在输入边界应用的新增 Skill binding。

Approval / Decision 导致 Execution：

~~~text
running -> waiting -> running
~~~

Resume 必须先经过 Agent Executor 的 waiting_reference 校验与幂等处理。

Agent Loop 只消费已经由 Agent Executor 确认可注入的 typed control input；Approval/Decision 必须 resolved，Skill binding 只更新资源目录，不充当 resume。

完整 waiting / resume lifecycle 见 [Execution Lifecycle](../agent-executor/execution-lifecycle.md)。

## 12. Completion

Agent Loop 不返回统一业务 Agent Result。

正常结束只向 Agent Executor 报告：

~~~text
completion = normal
~~~

Agent Executor 据此进入 succeeded。

业务效果通过 Tool / Domain Service 形成事实，例如：

- Task state change；
- MeetingMessage；
- blocker；
- comment；
- artifact。

最后普通文本属于 Runtime View / Transcript，不复制成另一份通用 final_output 业务字段。

不可恢复错误返回结构化 failure，由 Agent Executor 进入 failed。

## 13. Context Window Management

Agent Loop 必须支持长期执行。

第一阶段采用：

- proactive compaction；
- Provider overflow 后 compact-and-retry 兜底；
- safe Turn boundary；
- rolling summary；
- recent Turns 保留；
- large Tool Result projection；
- approximate context-budget estimate；
- fixed-context-too-large guard；
- compaction progress guard。

Compaction 只改变下一轮 Model Context Projection，不删除 Canonical Transcript。

完整设计见 [Context Window 与 Compaction](./context-compaction.md)。

## 14. Doom Loop 与全局限制

Agent Execution 不设置固定 max turns，也不设置全局 wall-clock timeout。

长期任务可以持续运行。

防 runaway 依赖：

- 单轮 Model generation watchdog；
- 重复 Tool Call doom-loop guard；
- context budget guard；
- compaction progress guard；
- cancel；
- 明确不可恢复错误。

重复相同 Tool + arguments 达到第一阈值时：

- 向模型注入一次 model-visible warning；
- 允许模型自行修正。

继续重复达到第二阈值时：

- 结束 Agent Loop；
- 向 Agent Executor 报告 unrecoverable failure。

具体运行规则见 [Agent Loop Runtime](./loop-runtime.md)。

## 15. Checkpoint 与恢复

Agent Loop runtime state 必须可 checkpoint。

Checkpoint 至少能恢复：

- 当前 loop phase；
- Canonical Transcript position；
- 当前 Compaction Snapshot；
- 最近稳定 Tool Call / Tool Result 边界；
- waiting / control state；
- 初始 Skill 固定版本与已应用绑定变更位置；
- 必要 Model / Tool correlation；
- 下一步安全继续位置。

Agent Executor 拥有 checkpoint 调度、持久化协调与 restart recovery policy。

Agent Loop 只负责：

> 提供可恢复且只指向稳定边界的 runtime state。

不能为了恢复而盲目重放 outcome unknown 的非幂等 Tool Operation。

完整恢复规则见 [Execution Lifecycle](../agent-executor/execution-lifecycle.md)。

## 16. Knowledge 与 Memory

Knowledge Base 与 Agent Memory 不自动注入每次 Model Context。

Agent 通过普通 Tool 按需：

- query / read Knowledge；
- recall Memory；
- retain Memory；
- reflect Memory。

其中：

- recall / reflect 都是只读能力；
- retain 才产生长期 Memory mutation；
- reflect 基于当前 Agent namespace 中检索出的 Memory 做综合推理，不隐式写入新的 Memory。

Tool Result 进入 Canonical Transcript 后，再受正常 Model Context Projection 与 compaction 规则管理。

这样避免 Knowledge / Memory 与 Agent Loop 形成隐藏耦合。

## 17. Observability

Agent Loop 同时参与三条不同记录路径：

~~~text
Model / Tool / Loop activity
        |
        ├── Canonical Transcript
        |   -> model continuity
        |
        ├── Runtime View semantic updates
        |   -> user understanding
        |
        └── Internal execution evidence
            -> diagnostics / audit / usage
~~~

Source of Truth 分工：

- Transcript：Agent Loop model-facing history；
- Runtime View：用户执行视图；
- Model Invocation：真实 Provider 调用证据；
- Tool Operation / Attempt：Tool 执行证据；
- Token Usage：Model Token Usage；
- Audit Log：安全与治理事实。

这些对象可以通过 execution_id / model invocation / tool_call_id 等相关联，但不互相复制成为另一套事件存储。

## 18. 架构原则

Agent Loop 保持以下原则：

1. **Turn-based runtime**：一次 Model Response + 完整 Tool Batch 构成稳定 Turn；
2. **Controller / Processor 分离**：跨 Turn 控制与单 Turn 执行分离；
3. **Immutable startup context**：运行过程中不重新解释或修改 AgentExecutionContext；
4. **Canonical history 与 request projection 分离**：完整 Transcript 不等于本轮 Model Context；
5. **Model-neutral**：只依赖 Unified Chat Model Contract；
6. **Tool-neutral**：只依赖 Unified Tool Runtime；
7. **Stable tool pairing**：任何 model-visible Tool Call 必须与正确 Tool Result 配对；
8. **Compaction 不破坏事实**：Summary 只影响 model-visible projection，不删除原始 Transcript；
9. **UI projection 分离**：Runtime View 不是 Loop history Source of Truth；
10. **无隐式全局终止限制**：不以固定 turn count 或 Execution wall-clock duration 截断长期任务；
11. **失败优先安全**：恢复、截断 Tool Call、unknown side effect 等场景不盲目执行；
12. **按需扩展**：Knowledge / Memory 等能力通过 Tools 接入，不成为隐藏 Loop 阶段。

## 19. Harness 参考

Agent Loop 由 agenteam 自行实现，不集成完整第三方 Harness 作为核心运行时。

设计主要参考成熟 Harness 中已经验证的机制：

- pi：AgentMessage 与 LLM Message 分层、Turn loop、Tool Batch、context transform、compaction；
- OpenCode：Session Message / Part、Session Processor、compaction、doom-loop guard；
- Codex CLI：内部 history 与 per-request prompt normalization 分离；
- Gemini CLI：history management、Tool output masking、fresh turn history。

参考项目用于验证设计模式，不成为 agenteam 的运行时依赖。
