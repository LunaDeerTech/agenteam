# Runtime View 详细设计

> 上层架构：[Agent Executor](./README.md)
>
> 相关详细设计：
> - [Agent Execution Domain Model](./execution-domain-model.md)
> - [Execution Lifecycle](./execution-lifecycle.md)
> - [Agent Loop](../agent-loop/README.md)
> - [Unified Tool Runtime](../tool-system/tool-runtime.md)

## 1. 设计目标

Runtime View 用于展示一次 Agent Execution 的实时结构化执行过程。

它不是传统纯文本日志，也不是底层 Event Store、Tool Operation、Model Invocation 或 Audit Log 的替代品。

它是一个 UI-facing execution projection：把 Agent 在运行过程中的用户可理解行为，组织成稳定、有顺序、可展开、可流式更新的 Runtime Item。

主要使用场景：

- Task / Review 的 Agent Execution Detail；
- Meeting Timeline 中展开某次 Agent Contribution；
- 独立 Agent Execution Detail；
- 未来其他引用 execution_id 的页面。

## 2. 核心模型

Runtime View 本质上由 Execution Summary 与按顺序排列的 Runtime Item 组成：

```text
AgentExecutionRuntimeView
├── execution
│   ├── id
│   ├── status
│   ├── phase
│   ├── started_at
│   ├── active_duration
│   └── waiting?
└── items[]
    └── AgentExecutionRuntimeItem
```

核心原则：

```text
RuntimeItem
= 一个用户可以直接理解和渲染的 Agent 行为块

RuntimeItemUpdate
= 某个 RuntimeItem 在实时执行过程中的增量变化
```

### 2.1 Execution View 效果示意

下面是一种典型 Execution View 的 ASCII 示意。重点是展示 Runtime Item 在 UI 中的排列、默认折叠状态，以及 Text / Reasoning 的流式更新方式。

```text
Backend Engineer · RUNNING · 03:18                                      [ Stop ]

  > 正在思考...                                                       [展开]

  > 调用 read_file                                                    [展开]

  > 思考了 4.2s                                                       [展开]

  > 调用 run_command                                                  [展开]

    我已经检查了当前项目结构，发现 Agent Executor 的生命周期设计中，
    checkpoint 与 waiting recovery 可以统一为同一套机制。下一步我会继续...
    |
    |  <- TextRuntimeItem 正在流式追加
    v

  > Waiting for Approval                                             [需要处理]
    run_command 需要用户批准
    [ Approve ]  [ Reject ]
```

默认交互规则：

- Text Item 默认展开，并实时追加内容；
- Reasoning Item 默认折叠，运行中显示“正在思考”，完成后显示持续时间；
- Tool Item 默认折叠，折叠状态只显示“调用 <tool_name>”；
- Tool Item 展开后再显示参数、执行过程与 Result；
- Interaction Item 默认展示需要用户处理的信息；
- Notice / Error Item 根据重要程度直接展示，不伪装成 Tool 或 Text；
- Runtime Item 的顺序只追加，不因为完成先后重新排序。

## 3. 不使用通用 AgentExecutionEvent

Runtime View 不建立通用 Execution Event Store。

如果把 token delta、reasoning delta、Tool Operation、checkpoint、heartbeat、lifecycle、audit、model invocation 全部塞进一种 Event，会形成过度泛化的模型，也会让 UI 反过来依赖内部 observability。

内部事实继续由各模块拥有：

```text
AgentExecution          -> lifecycle
ModelInvocation         -> model request
ToolOperation/Attempt   -> tool execution
Checkpoint              -> recovery
AuditRecord             -> audit
TokenUsage              -> usage
AgentExecutionError     -> execution failure
```

Runtime View 只维护 UI projection，不采用 Event Sourcing。

## 4. RuntimeItem Base Schema

概念结构：

```text
AgentExecutionRuntimeItem
├── id
├── execution_id
├── seq
├── type
├── status
├── started_at
├── updated_at
├── completed_at?
├── duration_ms?
├── source_reference?
└── data
```

`id` 是当前 Runtime Item 的稳定 identity；所有增量更新通过 item_id 指向同一个 Item。

`seq` 在每个 Execution 内单调递增，用于稳定排序、分页和 Snapshot 定位，不承担 Domain Event 语义。

第一阶段 type：

```text
text
reasoning
tool
interaction
notice
error
```

通用 status 可以包括：

```text
streaming
running
waiting
completed
failed
cancelled
```

并非所有 Item type 都使用所有 status。

## 5. TextRuntimeItem

用于 Agent 面向用户的普通文本输出。

```text
TextRuntimeItem
├── type = text
├── status = streaming | completed
└── data
    └── content
```

UI 规则：

- 默认展开；
- 收到 delta 时实时追加；
- 完成后保持原位置；
- 不额外生成一条“文本完成”日志。

## 6. ReasoningRuntimeItem

用于 Provider / Model Adapter 明确允许向上层展示的 reasoning / thinking 内容。

```text
ReasoningRuntimeItem
├── type = reasoning
├── status = streaming | completed
├── duration_ms?
└── data
    ├── content?
    ├── summary?
    └── representation
```

`representation` 可表达：

```text
provider_content
provider_summary
unavailable
```

Runtime View 不要求 Provider 必须提供 reasoning。

UI 默认折叠。运行中显示“正在思考”；完成后可以显示“思考了 8.4 秒”。只有展开后才显示 Model Adapter 明确允许展示的 reasoning content / summary。

Provider private reasoning、隐藏 CoT 或平台内部 scratchpad 不进入 Runtime View。

## 7. ToolRuntimeItem

一个 Tool Call 对应一个 Tool Runtime Item。

```text
ToolRuntimeItem
├── type = tool
├── status
├── source_reference
│   └── tool_operation_id
└── data
    ├── tool_id
    ├── tool_name
    ├── display_name?
    ├── arguments_projection?
    ├── result_projection?
    └── error_projection?
```

ToolRuntimeItem 不是 Tool Operation 的 Source of Truth。真正执行数据仍在 ToolOperation / ToolAttempt 中。

UI 默认折叠：

```text
正在调用 run_command…
调用 run_command
run_command 调用失败
```

展开后可以展示 Tool 名称、安全过滤后的参数、执行状态和 Result。

## 8. Parallel Tool Items

并行 Tool Call 按创建时 seq 稳定排列，而不是按完成时间重新排序。

```text
seq=12 tool A running
seq=13 tool B completed
seq=14 tool C running
```

每个 Item 独立更新状态。

## 9. InteractionRuntimeItem

用于 Agent Execution 中必须由用户处理的外部交互，第一阶段至少包括 Approval 与 Decision。

```text
InteractionRuntimeItem
├── type = interaction
├── status = waiting | completed
├── source_reference
└── data
    ├── interaction_type
    ├── title
    ├── summary
    └── action_reference
```

Interaction Item 通常不默认折叠，因为它需要用户操作。

真正提交 approve / reject / answer / skip 的 API 仍属于 Approval / Meeting Domain；Runtime View 只负责结构化呈现。

## 10. Notice / Error Item

Notice 用于重要但非错误的执行提示，例如从 checkpoint 恢复、上一轮 generation 被 watchdog 中止后继续、retry 等。

Error Item 用于用户可理解的错误展示：

```text
ErrorRuntimeItem
├── type = error
├── status = completed
├── source_reference?
└── data
    ├── code
    ├── message
    └── retryable?
```

详细 stack / diagnostics 不进入 Runtime Item；通过 source_reference 关联 AgentExecutionError、Tool Operation 或 Model Invocation。

## 11. Runtime Item Manager

Agent Executor 内部维护 Runtime Item Manager。

职责：

- 接收 Agent Loop / Model Adapter / Tool Runtime 的 semantic runtime update；
- 创建 Runtime Item；
- 分配 seq；
- 更新 Item Snapshot；
- 发布 RuntimeItemUpdate；
- 执行 public-safe projection；
- 维护当前 Execution Runtime View。

它不负责执行 Tool、调用 Model、决定 lifecycle、保存 Audit 或保存 checkpoint。

## 12. Semantic Update 来源

Agent Loop 产生 text / reasoning / loop notice / execution-level error projection。

Model Adapter 向上层提供标准化 text delta、允许展示的 reasoning delta / summary、Tool Call metadata 与 finish metadata。Runtime View 不直接依赖 Provider 原生 stream schema。

Tool Runtime 提供 Tool Operation created / running / waiting / completed / failed / cancelled 等语义变化，映射到同一个 Tool Item。

Agent Executor Lifecycle 可以产生 interaction waiting、recovery notice、cancel notice、terminal error 等 Runtime Item 更新。

## 13. RuntimeItemUpdate

RuntimeItemUpdate 是实时 transport protocol，不是 durable Event Store。

第一阶段概念事件：

```text
item.started
item.delta
item.updated
item.completed
item.failed
```

通用 envelope：

```text
RuntimeItemUpdate
├── execution_id
├── item_id
├── seq
├── kind
├── timestamp
└── payload
```

## 14. item.started / delta / updated

`item.started` 创建一个 Runtime Item。

`item.delta` 只用于适合追加式流式更新的内容，主要是 text 与 reasoning。

例如：

```text
item.started(type = reasoning)
item.delta("我需要先...")
item.delta("检查当前项目...")
item.completed(duration = 8.4s)
```

前端始终只有一个 Reasoning Item，不是四条日志。

`item.updated` 用于 patch Tool 状态、waiting 状态等结构化字段。

## 15. item.completed / failed

`item.completed` 将 Item 标记为完成并写最终 projection。

`item.failed` 表示当前 Item 自身失败。

必须注意：

```text
RuntimeItem failed
!=
AgentExecution failed
```

例如某次 Tool Call 失败后，Agent 仍可能继续下一轮推理。

## 16. Snapshot Persistence

持久化的是 Runtime Item 当前 / 最终 Snapshot，而不是每条 delta。

概念记录：

```text
agent_execution_runtime_item
├── id
├── execution_id
├── seq
├── type
├── status
├── data
├── source_reference
├── started_at
├── updated_at
└── completed_at
```

不逐条保存 text delta、reasoning delta、Tool argument delta。

## 17. Streaming Item 的 Snapshot Flush

长时间 streaming Item 不能只在 completed 时才持久化，否则页面刷新后会丢失当前显示内容。

采用批量 / 节流 Snapshot flush：

```text
in-memory deltas
    ↓
periodic / coalesced snapshot flush
    ↓
RuntimeItem Store
```

原则：

- realtime delta 优先低延迟推送；
- Snapshot 写入允许合并；
- 不允许每个 token 都写数据库；
- flush interval 属于实现参数。

## 18. 首次加载

打开 Execution Detail：

```text
get_runtime_view(execution_id)
    ↓
Execution summary
+
RuntimeItem[] ordered by seq
```

如果 Execution 已 terminal，只需要 Snapshot。

如果仍 non-terminal，加载 Snapshot 后再订阅 RuntimeItemUpdate Stream。

## 19. 断线重连

不重放全部 delta history。

```text
connection lost
    ↓
reconnect
    ↓
reload current Runtime View Snapshot
    ↓
subscribe new RuntimeItemUpdate
```

RuntimeItem.seq 保证重新加载后的顺序稳定。

## 20. Snapshot 与 Stream Race

首次加载 Snapshot 与订阅 stream 之间存在 race，必须有一致性策略。

可以使用 snapshot revision，或者先订阅并 buffer update、再加载 Snapshot 并应用较新的 update。

具体机制由 Platform Realtime Channel 设计决定，但必须保证：

- 不重复创建 Item；
- 不丢最终状态；
- 相同 item_id 更新幂等。

## 21. RuntimeItem Update 幂等

客户端按 `execution_id + item_id` 识别 Item。

重复 started 不重复插入；updated / delta / completed 只更新已有 Item。

如实现需要，可以增加 item_revision 防止旧 patch 覆盖新状态。

## 22. Tool Detail 加载

Tool Item 展开时优先展示 Snapshot 中已有的安全 projection。

如果需要完整详情，通过 `tool_operation_id` 请求 Tool Runtime Detail API。

这样避免 Runtime Item 复制完整 Tool Operation、大型 Result 或敏感数据。

## 23. Reasoning 安全边界

允许展示的 reasoning 必须来自 Model Adapter 的显式可展示字段，例如 provider-visible reasoning 或 provider reasoning summary。

没有可展示 reasoning 时，可以只显示“正在思考 / 思考了 xx 秒”，但不能伪造 reasoning content。

Provider private reasoning、隐藏 CoT、平台内部 scratchpad 不因 Runtime View 暴露。

## 24. Secret / Sensitive Projection

RuntimeItem 本身就是 UI-facing projection，因此写入前必须过滤：

- Secret value；
- Credential / Token；
- Tool 参数中不可公开字段；
- Provider private metadata；
- 仅供内部诊断的 Error detail。

Runtime Item 不增加 public / internal / sensitive 多级 visibility enum。

内部数据继续留在各自 Source of Truth。

## 25. Runtime View 与 Logs

```text
Runtime View
= 用户理解 Agent 做了什么

Execution Log
= 内部运行与诊断记录
```

checkpoint tick、Model request id、完整 ToolAttempt technical retry 等默认不进入 Runtime View。

只有对用户理解执行过程有意义时，才投影成 Notice / Tool status。

## 26. Meeting 集成

Meeting Contribution 默认折叠时不订阅 Runtime View。

用户展开后：

```text
Meeting Contribution
    ↓ execution_id
Runtime View Snapshot
    +
RuntimeItemUpdate subscription
```

Contribution 外层仍可以使用前端生成的趣味折叠文案；展开后才显示真实 Agent 行为流。

Meeting 不自己实现 Model / Tool stream。

## 27. Task / Review 集成

Task Execution Detail 直接复用相同 Runtime View。

业务页面只负责选择 execution_id、外层 Task / Review 布局和业务操作按钮，Runtime Item renderer 可以统一复用。

## 28. Interaction 与业务卡片

Approval / Decision 的领域对象仍由对应模块拥有。

Runtime View 可以包含 Interaction Item，但真正提交 approve / reject / answer / skip 的 API 仍属于对应领域。

Interaction 完成后：

- Runtime Item 更新 completed；
- Agent Executor resume 同一个 Execution；
- Agent Loop 继续。

## 29. Completion / Failure / Cancel

Execution succeeded 后，所有 streaming Item 应完成或明确关闭，Runtime View Snapshot 保持可历史读取。

Executor 不需要创建统一 final output result Item；如果 Agent 最后一段普通文本存在，它就是普通 TextRuntimeItem。

Execution failed 时，可以增加 ErrorRuntimeItem；结构化 AgentExecutionError 仍是失败 Source of Truth。

Cancel 时可以先显示 cancelling Notice，再根据实际停止结果更新当前 Tool / Text / Reasoning Item，不能在 cancel request 刚到时伪造所有 Item 已完成。

## 30. Recovery

Central restart 后：

- Agent Executor 从 checkpoint 恢复 Execution；
- Runtime Item Snapshot 从 Store 重新加载；
- 继续运行时使用已有 Item 或创建新 Item；
- 必要时增加 Recovery Notice。

Runtime View 不依赖 RuntimeItemUpdate replay 恢复 Agent Loop。

```text
Checkpoint
= execution recovery

RuntimeItem Snapshot
= UI recovery
```

## 31. API 概念

Snapshot：

```text
get_runtime_view(execution_id)
```

Realtime：

```text
subscribe_runtime(execution_id)
```

Realtime 统一通过 Platform Realtime Channel 承载 RuntimeItemUpdate；具体 HTTP / WebSocket route 命名留到 API 设计阶段。

## 32. 不在本文定义的内容

- Tool Operation 完整 schema；
- Model Invocation 完整 schema；
- Audit；
- Token Usage；
- checkpoint storage；
- Platform Realtime transport implementation；
- Meeting Timeline 自身 realtime；
- Approval / Decision 领域 schema。

这些能力只通过 reference / semantic update 与 Runtime View 集成。
