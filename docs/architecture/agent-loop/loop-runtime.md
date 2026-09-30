# Agent Loop Runtime 详细设计

> 上级架构：[Agent Loop](./README.md)
>
> 相关设计：
> - [Transcript 与 Model Context](./transcript-context.md)
> - [Context Window 与 Compaction](./context-compaction.md)
> - [Execution Lifecycle](../agent-executor/execution-lifecycle.md)
> - [Runtime View](../agent-executor/runtime-view.md)
> - [Chat Model Runtime](../platform-infrastructure/model-system/chat-model-runtime.md)
> - [Tool Execution](../tool-system/tool-execution.md)

## 1. 目标

本文定义 Agent Loop 的运行时控制：

- AgentLoopController；
- TurnProcessor；
- Turn 边界；
- Model invocation；
- Tool Batch；
- invalid / truncated Tool Call；
- waiting / resume control input；
- watchdog；
- doom-loop guard；
- cancel；
- completion / failure。

本文不定义：

- Agent Execution 主生命周期持久化；
- Model Provider Adapter；
- Tool Authorization / Approval / retry；
- Runtime View schema；
- context compaction 算法。

## 2. Runtime 组件

~~~text
AgentLoopController
├── LoopState
├── TurnSequencer
├── ModelContextProjector
├── TurnProcessor
├── DoomLoopGuard
├── Watchdog
├── ControlInputQueue
└── CancellationSignal

TurnProcessor
├── ModelInvoker
├── ModelStreamConsumer
├── AssistantResponseFinalizer
├── ToolCallValidator
├── ToolBatchExecutor
└── TranscriptAppender
~~~

AgentLoopController 生命周期与 Agent Execution 绑定。

TurnProcessor 是无独立业务生命周期的单 Turn 执行器。

## 3. Turn 定义

Turn 是 Agent Loop 内部的稳定推进单位。

定义：

> 一次 Model Response，以及由该 Response 产生的完整 Tool Batch 和对应 Tool Results，共同构成一个 Turn。

因此：

~~~text
Turn N
├── Model Invocation
├── Assistant Response
│   ├── text?
│   ├── reasoning / continuation metadata?
│   └── tool_calls[]
├── Tool Batch?
│   └── tool_results[]
└── stable boundary
~~~

一个 Turn 不等于：

- Agent Execution；
- Model Invocation；
- Tool Operation；
- UI Runtime Item。

Turn identity 只服务 Agent Loop runtime / Transcript / checkpoint correlation。

## 4. Turn 稳定边界

只有以下条件全部满足，当前 Turn 才进入 stable boundary：

1. Model Response 已结束或明确被 watchdog / provider error 终止；
2. Assistant Response 已完成规范化；
3. 需要执行的 Tool Batch 已全部 settle；
4. 每个 model-visible Tool Call 都有对应 model-visible Tool Result；
5. 稳定 Transcript entries 已写入；
6. 没有 outcome unknown 且无法安全表示为稳定结果的进行中副作用操作。

Compaction、checkpoint 和 next-turn decision 默认只在稳定边界执行。

## 5. AgentLoopController 主循环

概念伪代码：

~~~text
loop:
    assert execution is active
    consume pending typed control input

    projection = build_model_context()

    if projection.requires_compaction:
        compact()
        continue

    result = TurnProcessor.run(projection)

    switch result.outcome:
        continue:
            apply_loop_guards()
            continue

        waiting:
            hand waiting reference to Agent Executor
            suspend

        completed:
            report normal completion
            stop

        failed:
            report unrecoverable failure
            stop
~~~

Controller 不直接调用 Provider-native API 或 Backend-specific Tool executor。

## 6. TurnProcessor 输出

概念结构：

~~~text
TurnResult
├── turn_id
├── outcome
│   ├── continue
│   ├── waiting
│   ├── completed
│   └── failed
├── assistant_entry?
├── tool_result_entries[]
├── waiting_request?
├── error?
└── metadata
~~~

其中：

- continue：本 Turn 已稳定结束，需要下一次 Model Turn；
- waiting：当前 Tool / interaction 需要 Approval / Decision；
- completed：模型未产生需要继续执行的 Tool Call，且本 Turn 正常完成；
- failed：Loop invariant 或不可恢复运行错误。

Compaction 不是 TurnProcessor 的业务结果。

Compaction 由 AgentLoopController 在 Turn 之间的 context budget check 决定。

## 7. Model Request

TurnProcessor 通过 Model Context Projector 获得本 Turn 的 Unified Model Messages。

请求概念：

~~~text
Unified Chat Model Request
├── resolved_model = Execution Snapshot
├── final_system_prompt
├── messages = current Model Context Projection
├── tools = Execution Tool Set Snapshot
├── tool_choice / response_format
├── execution / turn metadata
└── cancellation
~~~

Agent Loop 不读取 Provider 配置。

Provider 转换、streaming transport、usage、finish reason 与底层 retry 由 Model System 负责。

## 8. Model Streaming

Model Adapter 标准化流至少可能产生：

- text delta；
- 允许展示的 reasoning delta / summary；
- Tool Call metadata / arguments delta；
- finish metadata；
- usage。

TurnProcessor 同时更新两类状态：

~~~text
Model Stream
    |
    ├── transient Assistant accumulator
    |
    └── Runtime View semantic updates
~~~

Canonical Transcript 不把每个 stream delta 保存成一条 TranscriptEntry。

Assistant Response 结束后，才形成稳定 assistant entry。

这样避免：

- Transcript 被大量 delta 污染；
- checkpoint 恢复依赖 Provider stream chunk；
- model-facing history 与 UI transport 耦合。

## 9. Assistant Response Finalization

Model Response 完成后，TurnProcessor 将统一 Model Response 固化为 Assistant semantic result。

Assistant entry 可包含：

- text；
- reasoning / provider continuation metadata；
- 一个或多个 tool_call。

如果 Response 同时包含 text 和 Tool Call：

> 只要存在需要执行的有效 Tool Call，该 text 就是当前 Turn 的中间输出，不代表 Agent Execution completion。

流程：

~~~text
Assistant text + Tool Calls
        ↓
persist stable Assistant entry
        ↓
execute complete Tool Batch
        ↓
append Tool Results
        ↓
next Turn
~~~

## 10. Completion 判断

正常 completion 的基本条件：

~~~text
Model Response completed
AND
no executable Tool Call
AND
no waiting interaction
AND
no unrecoverable error
~~~

此时 TurnResult.outcome = completed。

Agent Loop 向 Agent Executor 报告 normal completion。

Agent Loop 不解析文本来猜测：

- Task 是否 done；
- Meeting 是否结束；
- 业务是否成功。

这些业务事实必须通过对应 Domain Tool / Service 形成。

## 11. Tool Call 验证

Tool Call 在进入 Unified Tool Runtime 前至少检查：

- call_id 是否存在且在当前 Assistant Response 内唯一；
- model_visible_tool_name 是否可解析；
- arguments 是否能按统一 schema 解析；
- 当前 Response 是否完整；
- provider finish reason 是否允许安全执行。

Tool-specific authorization / approval / idempotency 不属于这里。

## 12. Unknown Tool

如果模型调用当前 Execution Tool Set 不存在的 Tool：

~~~text
tool_call
    ↓
tool_not_found
    ↓
structured error Tool Result
    ↓
append to Transcript
    ↓
next Turn
~~~

禁止：

- fuzzy name matching；
- 猜测最近 Tool；
- 路由到同名其他 Backend。

Unknown Tool 默认是 model-correctable error，不直接令 Agent Execution failed。

## 13. Invalid Arguments

arguments schema invalid 时：

- 不执行 Backend；
- 生成与原 tool_call_id 对应的结构化 error Tool Result；
- 返回 validation details 的 public-safe / model-safe projection；
- 进入下一 Turn 让模型修正。

如果错误来自 Agent Loop 自身无法保持 tool_call/result invariant，则属于内部错误，可以 failed。

## 14. 被 Output Limit 截断的 Tool Call

如果 Model Response 因：

- length；
- max_tokens；
- 等价 output limit finish reason；

被截断，并且 Response 中存在 Tool Call：

> 当前 Response 的所有 Tool Calls 都不执行。

即使某个 arguments 看起来已经可以解析，也不能假设参数完整。

处理：

~~~text
truncated Assistant Response
        ↓
persist Assistant entry
        ↓
for each tool_call:
    append structured error Tool Result
    category = truncated_model_output
        ↓
next Turn
~~~

下一轮 model-visible error 需要明确：

- 上一轮输出被截断；
- Tool 没有执行；
- 需要重新产生完整 Tool Call。

## 15. Tool Batch

同一个 Assistant Response 可以包含多个 Tool Calls。

它们组成一个 Tool Batch。

Agent Loop 将 Batch 交给 Unified Tool Runtime。

Tool Runtime 决定：

- read-only Tool 是否并行；
- 其他 Tool 是否串行；
- Approval；
- retry；
- Operation / Attempt；
- unknown outcome。

Agent Loop 只要求：

1. 每个 Tool Call 都必须得到一个最终 model-visible Tool Result；
2. Tool Result 通过 tool_call_id 精确关联；
3. 完整 Batch settle 后才进入 Turn stable boundary。

## 16. Tool Result

Tool Result 回填至少要保留语义类别，例如：

- success；
- business_error；
- validation_error；
- authorization_denied；
- approval_required / waiting；
- technical_error；
- cancelled；
- timeout；
- unknown_outcome。

但 Agent Loop 不自己重新分类 Tool Runtime 已经给出的 normalized result。

model-visible projection 可以比内部 Tool Operation 记录更简化。

详细 Source of Truth 仍是 Tool Operation / Attempt。

## 17. Approval / Decision Waiting

如果 Tool 或 Meeting interaction 需要 Human input：

~~~text
Turn active
    ↓
Interaction created
    ↓
Agent Executor:
running -> waiting
    ↓
Agent Loop suspend
~~~

Agent Loop 不自己持久化 Execution lifecycle mutation。

Turn runtime 需要记录：

- waiting_reference；
- waiting type；
- 当前稳定 Transcript position；
- 可安全 resume 的位置。

## 18. Resume Control Input

外部 Approval / Decision resolve 后，必须经过 Agent Executor：

~~~text
resolve
-> validate waiting_reference
-> verify resolved object
-> idempotency check
-> waiting -> running
-> inject typed control input
~~~

Agent Loop 接收到的是已确认的 typed control input，而不是直接消费外部业务对象。

概念：

~~~text
ControlInput
├── type
│   ├── approval_result
│   ├── decision_result
│   ├── watchdog_notice
│   ├── resume_notice
│   └── recovery_notice
├── reference?
├── payload
└── source
~~~

Control Input 进入 Canonical Transcript，并在下一轮 Model Context Projection 中转换成 model-visible message。

## 19. 不提供通用 Steering Queue

第一阶段不实现：

- 用户在 Agent 正运行时任意插入新 prompt；
- steering queue；
- follow-up queue；
- 多级 interactive message queue。

原因：

- agenteam 的 Agent Execution 不是交互式 CLI Session；
- waiting / resume 已有明确 typed control contract；
- Task / Meeting 的新业务输入应由对应 Domain 决定。

以后出现明确产品需求时可以扩展，但不能绕过 Agent Executor lifecycle。

## 20. Model Request Error

Model error 的 Provider-specific retry 由 Model System 负责。

Model System最终返回给 Agent Loop 的错误至少应能区分：

- retry 已耗尽的 transient failure；
- invalid request；
- context overflow；
- cancellation；
- provider unavailable；
- 其他 normalized error。

Agent Loop 处理：

- context overflow：进入 compaction recovery；
- cancellation：按 cancel flow；
- 明确不可恢复 Model error：failed；
- 已由 Model System retry 成功：对 Loop 无特殊语义。

## 21. Tool Error

Tool technical retry 由 Tool Runtime 完成。

Agent Loop 不通过“Tool name + args 看起来一样”自行认定 retry。

同一个 Tool Operation 的 Attempt、One-time Approval、idempotency 与 unknown outcome 规则见 Tool System / Security Governance。

Agent Loop 只消费最终 normalized result。

## 22. 单轮 Model Generation Watchdog

Agent Execution 不设置全局 wall-clock timeout。

但每轮 Model generation 有 watchdog。

触发条件：

> 单轮 generation 持续时间超过配置阈值，或被判断为持续循环输出。

处理：

1. 向当前 Model Request 传播 cancel；
2. 停止接收后续 stream；
3. 保留已经允许稳定保存 / 展示的必要输出；
4. 结束当前 Model Invocation；
5. 向 Transcript 注入 typed watchdog notice；
6. 下一 Turn 告知模型上一轮被中止；
7. Agent Execution 继续 running。

watchdog 不自动 failed。

阈值属于 Agent Loop runtime 配置。

## 23. Watchdog 与截断 Assistant

watchdog 中止发生时，不能把半截 Tool Call 当作可执行 Tool Call。

原则与 output-limit truncation 相同：

- incomplete Tool Call 不执行；
- 若需要维持 model history pairing，则生成明确未执行 error result；
- Runtime View 可以保留已经展示的 text/reasoning；
- Transcript 只保存可恢复的稳定语义。

## 24. Doom Loop Guard

Doom Loop Guard 检测连续重复 Tool Call。

Fingerprint 概念：

~~~text
tool_loop_fingerprint
=
stable_tool_identity
+
normalized_arguments
~~~

它与 Tool Operation fingerprint 的用途不同：

- Tool Operation fingerprint 服务 retry / idempotency；
- doom-loop fingerprint 只服务 Loop runaway detection。

检测窗口只看 Agent 连续主动产生的重复业务调用，不把 Tool Runtime technical retry 计为新的 Agent Tool Call。

## 25. Doom Loop 两阶段处理

达到第一阈值：

~~~text
repeat threshold 1
    ↓
append model-visible loop warning
    ↓
allow next Turn
~~~

warning 告诉模型：

- 检测到连续重复调用；
- 先检查现有结果；
- 不要继续无变化地重复同一动作。

如果之后模型改变 Tool / arguments，则重复计数重置。

继续达到第二阈值：

~~~text
repeat threshold 2
    ↓
Loop failed
    ↓
error.category = loop_runaway
~~~

阈值是 runtime 配置，不在架构中写死。

## 26. 不设置 Max Turns

第一阶段不配置固定 max turns / max steps。

因此不能出现：

~~~text
turn_count >= N
-> fail
~~~

长期 Task 可以持续执行。

运行安全依赖：

- watchdog；
- doom-loop guard；
- context budget；
- compaction guard；
- cancel；
- 不可恢复错误。

## 27. Cancel

Agent Executor 发出 cancel / shutdown 后：

1. AgentLoopController 不再创建新 Turn；
2. 向当前 Model Request 传播 cancellation；
3. 向 Tool Runtime 请求取消可取消 Tool Operation；
4. 等待必要的稳定状态 / 持久化；
5. 停止 Loop；
6. 向 Agent Executor 报告 stopped。

Agent Loop 不自行把 Execution 主状态写成 cancelled。

主状态由 Agent Executor Lifecycle 控制。

## 28. Cancel 与 Tool Side Effect

如果正在执行的 Tool Operation outcome unknown：

- 不能把 cancel 当作“没有执行”；
- 不能为了恢复或下一 Turn 自动重放；
- 由 Tool Runtime 的 outcome / idempotency contract 决定可否 retry；
- checkpoint 不能伪造 completed Tool Result。

安全原则：

> 宁可让 Execution recovery failed，也不盲目重复副作用。

## 29. Loop Runtime Phase

Agent Loop 内部可以维护轻量 phase：

~~~text
initializing
projecting_context
calling_model
executing_tools
waiting_external_input
between_turns
compacting
completing
stopped
~~~

这些 phase：

- 用于 runtime control；
- 用于 checkpoint；
- 用于 diagnostics。

它们不是 Agent Execution 对外 lifecycle state。

Execution 对外仍只有：

~~~text
created
preparing
running
waiting
succeeded
failed
cancelled
~~~

## 30. Checkpoint Safe Point

Loop 可以在稳定边界向 Agent Executor 暴露 checkpointable state。

优先 checkpoint 点：

- Turn stable boundary；
- waiting 已建立且 waiting_reference 已持久化；
- compaction 完成并持久化 Compaction Snapshot 后；
- cancel 前最后一个可确认稳定位置。

默认不把以下状态当作安全恢复点：

- 半个 Model stream；
- Tool Batch 部分完成但仍有无法确认 outcome 的副作用操作；
- 正在生成但未完成的 compaction summary。

## 31. Runtime Checkpoint State

Loop 对 checkpoint 提供的状态至少包含：

~~~text
AgentLoopCheckpointState
├── phase
├── last_stable_turn_id?
├── transcript_position
├── compaction_snapshot_id?
├── pending_control_state?
├── waiting_state?
├── model / tool correlation?
└── next_action
~~~

完整 Checkpoint 存储、state hash、restart scan 由 Agent Executor 定义。

## 32. Runtime View 更新

TurnProcessor / Model Adapter / Tool Runtime 可以向 Runtime Item Manager 发 semantic update。

Agent Loop 负责的典型 update：

- text / reasoning lifecycle；
- loop warning；
- watchdog notice；
- execution-level loop error。

Tool Item 的 Operation state 由 Tool Runtime 投影。

Agent Loop 不将 TranscriptEntry 机械一一映射成 RuntimeItem。

## 33. Failure 分类

Agent Loop 自己产生的不可恢复错误可以至少包括：

~~~text
loop_internal_error
loop_state_corrupted
loop_runaway
invalid_model_protocol_state
context_compaction_failed
context_fixed_input_too_large
~~~

Provider / Tool 原始错误不需要重新包装成新的平行 taxonomy；尽量保留 source_reference。

## 34. 与 Execution Lifecycle 的边界

Agent Loop 可以请求或报告：

- waiting；
- normal completion；
- unrecoverable failure；
- stopped after cancel；
- checkpointable state。

Agent Executor 负责：

- 实际 lifecycle mutation；
- waiting_reference 幂等；
- terminal state；
- checkpoint persistence；
- restart recovery；
- cancel request ownership。

## 35. 运行时原则

1. Turn 必须在完整 Tool Batch settle 后才稳定；
2. streaming delta 不直接成为 Canonical Transcript entry；
3. text + Tool Call 的 Assistant Response 不提前 completion；
4. invalid Tool Call 优先回填模型自修正；
5. truncated Tool Call 一律不执行；
6. Tool Runtime 拥有 Tool technical retry；
7. 通用 steering 第一阶段不进入 Agent Loop；
8. 无固定 max turns；
9. watchdog 只终止当前 generation；
10. doom-loop guard 防止无变化重复调用；
11. compaction / checkpoint 默认只在稳定边界执行；
12. lifecycle mutation 始终由 Agent Executor 拥有。
