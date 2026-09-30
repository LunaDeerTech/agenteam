# Tool Execution 详细设计

> 状态：设计稿
>
> 上层架构：
> - [统一工具系统架构](./README.md)
> > 总体设计：
> > - [Unified Tool Runtime 详细设计](./tool-runtime.md)
> > 相关详细设计：
> > - [Approval Scope 详细设计](../security-governance/approval-scope.md)
> > - [One-time Approval、Tool Retry 与 Idempotency 详细设计](../security-governance/one-time-approval-retry-idempotency.md)

本分册描述单次 Tool Call 从进入 Runtime 到形成最终执行状态的生命周期，包括 Operation / Attempt、Authorization、Dispatcher、timeout、retry、并发与持久化。

## 1. Tool Call

模型输出经过 Model Adapter 后形成统一 Tool Call：

~~~text
ToolCall
├── tool_call_id
├── model_visible_tool_name
├── arguments
└── provider_metadata?
~~~

其中：

- tool_call_id：模型侧当前 Tool Call identity；
- model_visible_tool_name：用于当前 Execution 映射；
- arguments：解析后的 JSON value；
- provider_metadata：仅保留必要的 Provider transport metadata。

Tool Runtime 首先执行：

~~~text
model_visible_tool_name
-> Execution Tool Set
-> stable_tool_id
~~~

无法解析时属于 model/runtime mismatch，不直接发送到任意 Backend。

## 2. Arguments Validation

在 Authorization 与 Backend execution 之前，Runtime 应对 Tool arguments 执行统一 validation：

~~~text
1. Tool 存在于 Execution Tool Set
2. arguments 是合法 JSON value
3. 根据 input_schema validation
4. normalization（如平台明确允许）
5. 形成 canonical arguments
~~~

input schema validation 失败时，不调用 Backend。

默认应形成可安全返回给模型的 Tool Error，使 Agent 有机会修正参数后再次调用。

新的修正调用是新的 Tool Call / Operation。

Runtime 不应自动猜测并改写模型参数来“修复”不符合 schema 的调用。

## 3. Tool Operation

每一个模型实际产生的 Tool Call，在通过基本解析后创建一个 Tool Operation。

~~~text
ToolOperation
├── operation_id
├── agent_execution_id
├── tool_call_id
├── stable_tool_id
├── spec_revision
├── canonical_arguments
├── operation_fingerprint
├── state
├── approval_id?
├── created_at
├── started_at?
└── completed_at?
~~~

推荐状态：

~~~text
created
authorizing
waiting_for_approval
ready
running
succeeded
failed
cancelled
~~~

是否需要把所有中间状态持久化成数据库枚举可以在实现时简化，但语义上必须区分：

- 尚未开始 Backend；
- 等待审批；
- Backend 正在执行；
- 已形成最终结果。

### 3.1 operation_fingerprint

fingerprint 至少覆盖：

- stable tool id；
- ToolSpec spec_revision；
- canonical arguments；
- 必要的 backend binding identity。

它用于：

- One-time Approval 绑定；
- retry integrity；
- Audit correlation。

fingerprint 不能用来自动把两个独立 Model Tool Call 合并成同一个 Operation。

即使两个调用 arguments 完全一致，只要是模型新产生的 Tool Call，就是新 operation_id。

## 4. Tool Attempt

Backend 的每一次实际执行尝试都创建独立 Attempt：

~~~text
ToolAttempt
├── attempt_id
├── operation_id
├── attempt_number
├── backend_request_id?
├── state
├── started_at
├── completed_at?
├── outcome
├── error_category?
└── retry_metadata?
~~~

同一个 operation：

~~~text
operation_id = constant
attempt_id = changes per retry
~~~

这与 Security / Governance 中已经确认的 One-time Approval 语义一致。

Runtime 不因为技术 retry 再创建新的 Approval Request。

### 4.1 Persistence Model

`ToolOperation` 与 `ToolAttempt` 第一阶段作为两个独立持久化实体，不把 attempts 作为 JSON 数组内嵌在 ToolOperation 中。

~~~text
ToolOperation 1
    ↓
ToolAttempt 1..N
~~~

对应关系固定为：

- 一个 ToolOperation 表示一次逻辑 Tool 调用；
- 一个 ToolAttempt 表示一次实际 Backend 执行尝试；
- `ToolAttempt.operation_id` 引用所属 ToolOperation；
- technical retry 只新增 ToolAttempt，不新增 ToolOperation。

这样可以直接按 attempt 查询、索引和统计 backend request、retry 次数、单次耗时与失败原因，而不依赖 JSONB 内部查询。

## 5. Tool Authorization 接入点

Tool Runtime 自己不定义权限规则。

Tool Runtime 在完成 Tool identity 解析、arguments validation 并创建 ToolOperation 后，把当前真实 Operation 交给 Security / Governance 的 Tool Authorization。

Tool Authorization 的内部规则由 Security / Governance 负责，Unified Tool Runtime 不感知其内部步骤。

因此 Runtime 与 Security / Governance 的契约是：

~~~text
Tool Runtime
-> Tool Authorization
    -> allow
    -> waiting_for_approval
    -> deny
~~~

其中 `waiting_for_approval` 表示当前 ToolOperation 已经进入统一 Approval Request / Human Inbox 流程。用户批准后继续同一个 ToolOperation；拒绝则形成最终 ToolError。

Authorization 必须基于：

- 当前 Agent Execution；
- stable tool id；
- canonical arguments；
- 当前真实 resource / scope。

不能只依赖 model-visible name。

## 6. Tool Dispatcher

Tool Dispatcher 根据 ExecutionTool 中固定的 execution adapter / backend binding 路由调用。

~~~text
stable tool id
+ ExecutionTool snapshot
    ↓
Tool Dispatcher
    ├── Builtin Executor
    ├── Runner Executor
    └── MCP Executor
~~~

Dispatcher 不重新查询 Agent Capability。

它收到的是已经完成 Runtime 基础校验与 Authorization 的 Operation。

Backend 仍然需要执行最后一层自己的业务或本地安全校验。

## 7. Timeout

Tool Operation 不使用统一的平台强制 timeout。

是否为某次 Tool Call 设置 timeout，以及设置多长，由 Agent 根据当前任务和 Tool 语义决定。

具体规则：

- timeout 是可选 Runtime 调用参数；
- Agent 可以针对具体 Tool Operation 指定 timeout；
- Agent 未指定时，不由 Agent Executor / Tool Runtime 自动补一个统一 Operation deadline；
- Project 不提供统一 timeout 配置，不要求人类为每个 Tool 预配置运行时间；
- timeout 不进入 ToolSpec，也不属于 Tool 的业务 arguments；
- Backend 自己为网络连接、RPC、进程管理设置的 transport / implementation timeout 属于 Backend 内部可靠性机制，不等同于 Agent 选择的 Tool Operation timeout。

概念：

~~~text
Agent decides optional operation timeout
        ↓
Tool Runtime
        ↓
Cancellation signal
        ↓
Tool Dispatcher
        ↓
Backend adapter
~~~

原则：

1. Tool 可以在没有 Agent-level timeout 的情况下持续运行。
2. Agent 明确设置 timeout 后，Tool Runtime 负责计时并在到达时触发 cancellation。
3. Backend 支持 cancellation 时应向下传播。
4. timeout 到达不代表 Backend 一定没有执行。
5. 如果取消后无法确定 Backend outcome，应标记 unknown_outcome。
6. timeout 是否可以 retry，仍取决于 idempotency 和 outcome。
7. Tool timeout 只影响当前 Tool Operation，不自动终止整个 Agent Execution。

Agent 在收到 timeout Tool Result 后，可以自行决定：

- 使用更长 timeout 发起新的 Tool Call；
- 改用其他方法；
- 继续推理；
- 放弃该操作。

## 8. Cancellation

Tool Operation cancellation 来源可能包括：

- Agent Execution 被取消；
- 用户取消当前 Execution；
- Agent 为当前 Tool Operation 设置的 timeout 到达；
- Agent Loop 主动停止剩余并发调用；
- Backend-specific cancel。

Cancellation 使用 Execution 级 context / signal 向下传播。

统一原则：

~~~text
cancel requested
-> Runtime 停止等待不必要的新工作
-> 尝试取消 active backend request
-> 收集可确认 outcome
-> 形成 cancelled 或 unknown_outcome
~~~

不能假设发送 cancel 就代表远端一定没有发生副作用。

## 9. Retry

Retry 不作为 Tool Runtime 的无条件默认行为。

第一阶段不提供 tool-specific retry profile，也不提供面向用户、Project 或 Agent 的 retry 配置页。

Runtime 使用统一的平台级 retry policy。该 policy 至少定义：

~~~text
default max attempts
default backoff
runtime-recognized retryable error categories
~~~

具体默认值属于平台内部 Runtime 配置，不进入 ToolSpec，也不要求用户配置。

Runtime 只有在明确满足安全条件时才自动 retry：

~~~text
retryable = true
AND
operation still active
AND
platform retry policy allows
AND
(
    outcome_known = true
    OR
    Backend supports verifiable idempotency
)
~~~

其中是否允许 retry 还必须结合 ToolSpec 的 idempotency 语义判断，统一 Runtime policy 不能绕过非幂等 / unknown outcome 的安全边界。

同一个 Operation retry：

- operation_id 不变；
- approval 不重新申请；
- attempt_id 改变；
- operation fingerprint 不变。

不同 Backend 可以提供 error classification、outcome information 和 idempotency capability，但第一阶段不能自行提供独立 retry profile 绕过统一 Runtime policy。

如果未来确实存在某类 Backend 需要不同的 max attempts / backoff / retry category，可以在 ToolBinding 增加平台内部 override；这属于后续扩展，不属于第一阶段 contract。

## 10. Idempotency

Unified Tool Runtime 不要求所有 Tool 都幂等。

Tool 可以具有：

~~~text
idempotency
├── inherently_idempotent
├── supports_idempotency_key
└── non_idempotent
~~~

如果 Backend 支持 idempotency key：

- key 应与 operation_id 或明确的 operation identity 绑定；
- technical retry 使用相同 key；
- 新 Tool Call / 新 Operation 使用新 key。

外部 MCP Tool 的 idempotency annotation 是否可信以及如何映射，由 MCP detailed design 单独定义。

## 11. 并发执行

Model Capability 中的 parallel_tool_calls 只表示：

> 模型可能在同一 turn 输出多个 Tool Call。

它不代表 Runtime 必须并行执行。

统一 Runtime 应支持：

~~~text
serial
parallel
~~~

第一阶段采用简单规则：

- 同一 model turn 返回多个 Tool Call 时，`read_only = true` 的 Tool Call 可以并行执行；
- 其他 Tool Call 一律串行执行；
- 第一阶段不设置额外的 Runtime 全局最大并发数；
- 同一 turn 多个 Tool Call 的最终结果仍按原 `tool_call_id` 精确对应回模型。

这里不依赖 MCP annotation 直接决定安全并发。

其中 `read_only` 使用 Unified ToolSpec 中经过平台归一化后的可信 annotation，而不是直接信任外部 Backend 原始声明。

## 12. Unknown Tool Call

如果模型调用一个当前 Execution Tool Set 中不存在的名称：

~~~text
model_visible_tool_name
-> no mapping
~~~

默认不路由到任意模糊匹配 Tool。

Runtime 返回结构化：

~~~text
tool_not_found
~~~

并允许 Agent Loop 把安全错误回填模型，让模型自行纠正。

不自动采用：

- fuzzy matching；
- 最近名字猜测；
- 同名其他来源 Tool。

## 13. Persistence 与 Execution Log

Tool Operation 是 Agent Execution 的运行记录。

至少需要持久化：

~~~text
operation_id
agent_execution_id
tool_call_id
stable_tool_id
spec_revision
arguments / safe arguments reference
operation fingerprint
state
approval_id?
timing
final status
error category
result summary / artifact refs
~~~

Attempt 至少需要记录：

~~~text
attempt_id
operation_id
attempt number
backend request id?
timing
outcome
retry metadata
~~~

数据库实现上，ToolOperation 与 ToolAttempt 分别使用独立表持久化，ToolAttempt 通过 `operation_id` 与 ToolOperation 建立一对多关系。

完整敏感 payload 是否持久化由 Execution logging policy 决定。

大段、文件性质的 Tool output / stdout / stderr / runtime log 不直接写入 PostgreSQL 大字段，而是通过 ObjectStorageService 写入 StoredObject；数据库中的 Execution / Tool runtime record 只保存结构化 metadata、索引和 `stored_object_id` 等引用。

Audit 不复制全部 Tool runtime record，而是引用对应 operation_id。
