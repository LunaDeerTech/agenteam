# Agent Execution Domain Model 详细设计

> 上层架构：[Agent Executor](./README.md)
>
> 相关详细设计：
> - [Execution Context](./execution-context.md)
> - [Execution Lifecycle](./execution-lifecycle.md)
> - [Runtime View](./runtime-view.md)

## 1. 设计范围

本文定义 Agent Execution 的持久化领域模型与 Agent Executor 的核心 Service Contract。

主要覆盖：

- Agent Execution 主记录；
- Execution Snapshot；
- 幂等语义；
- 乐观并发；
- waiting / cancel 等生命周期字段；
- retry Execution 关联；
-结构化 Error；
- 大 Payload / Object Storage 边界；
- retention；
- Agent Executor 内部 Service API。

本文不定义：

- Agent Loop 内部消息结构；
- Tool Operation / Attempt；
- Model Invocation；
- Runtime View Item schema；
- Trigger Context 的具体内容；
- Task / Meeting 等业务领域状态。

## 2. 核心原则

Agent Execution Domain Model 遵循：

1. **Execution 主记录轻量**：高频 lifecycle / query 字段留在主记录；
2. **启动配置不可变**：本次实际使用的 Agent / Model / Tool / Context 以 Snapshot 固化；
3. **历史事实不可逆**：terminal Execution 不重新进入运行态；
4. **幂等创建**：重复 Launch 不重复创建 Execution；
5. **显式并发控制**：lifecycle mutation 使用 version；
6. **业务结果不归 Executor 所有**：真正业务状态由对应 Tool / Domain Service 持有；
7. **大 Payload 外部化**：大内容进入 Object Storage；
8. **内部模块不能直接修改 lifecycle persistence**。

## 3. AgentExecution

概念结构：

```text
AgentExecution
├── id
├── agent_id
├── trigger_type
├── trigger_reference
├── purpose
├── idempotency_scope
├── idempotency_key
├── status
├── waiting_reason?
├── waiting_reference?
├── cancel_requested_at?
├── retry_of_execution_id?
├── snapshot_id
├── version
├── created_at
├── preparing_at?
├── started_at?
├── waiting_at?
├── resumed_at?
├── finished_at?
├── active_duration_ms
├── error?
└── terminal_metadata?
```

### 3.1 Identity

`id` 是 Agent Execution 的稳定主键。

业务模块只能通过 `execution_id` 关联 Execution，不复制 Execution runtime 数据。

### 3.2 Trigger Identity

```text
trigger_type
trigger_reference
purpose
```

用于描述这次 Execution 为什么被创建。

示例：

```text
trigger_type = task
trigger_reference = task_id
purpose = work
```

或：

```text
trigger_type = meeting
trigger_reference = meeting_turn_id
purpose = response
```

这些字段只用于 identity / correlation。Agent Executor 不根据它们直接实现业务状态机。

### 3.3 Status

第一阶段：

```text
created
preparing
running
waiting
succeeded
failed
cancelled
```

不设置 Execution 级 `timed_out` 主状态。

Model timeout、Tool timeout、单轮 Loop watchdog 等属于更低层控制策略，不自动将整次 Execution 终止为 timeout。

### 3.4 Waiting Fields

仅当 `status = waiting` 时使用：

```text
waiting_reason
waiting_reference
waiting_at
```

第一阶段至少支持：

```text
waiting_reason = approval
waiting_reason = decision
```

`waiting_reference` 必须指向具体外部对象，例如：

- Approval Request ID；
- Decision Request ID。

不能仅依赖 Trigger Context 推测当前正在等待哪个对象。

### 3.5 Cancellation Fields

取消是一个两阶段过程。

至少记录：

```text
cancel_requested_at
finished_at
```

收到取消请求时不立即把 status 改成 `cancelled`。

只有 Agent Executor 确认当前 Execution 不再继续驱动 Model / Tool / Runner 操作后，才进入 terminal `cancelled`。

UI 可以根据：

```text
cancel_requested_at != null
AND status is non-terminal
```

展示 cancelling phase。

### 3.6 Active Duration

Execution 不使用自动总时长 timeout，但需要统计主动运行时间用于异常长时间运行警告。

`active_duration_ms` 只累计：

- preparing；
- running。

不累计：

- waiting。

该值可以采用持久化累计值 + 当前 active segment 的方式计算，不要求每秒更新数据库。

## 4. AgentExecutionSnapshot

AgentExecutionSnapshot 固化本次 Execution 真正使用的启动配置。

概念结构：

```text
AgentExecutionSnapshot
├── id
├── execution_id
├── launch_request
├── agent_snapshot
├── model_snapshot
├── execution_tool_set_snapshot
├── execution_policy
├── execution_context
├── environment_snapshot
├── created_at
└── object_references?
```

Snapshot 在 preparing 完成后成为不可变事实。

### 4.1 为什么与主记录分离

AgentExecution 主记录用于：

- 列表；
- 状态查询；
- cancel / resume；
- recovery；
-业务对象关联。

Snapshot 主要用于：

- Agent Loop 启动；
- crash recovery；
- 历史 Execution 解释；
-问题排查。

两者访问频率和体积不同，因此不放在同一记录中。

### 4.2 Snapshot 内容

至少固化：

- Agent description；
- Agent instructions；
- Agent capability；
- resolved Model config；
- reasoning effort；
- Execution Tool Set；
- Execution Policy；
- Trigger identity；
- typed Trigger Context；
- Project / `AGENTS.md` Snapshot；
- Runner / mount identity metadata；
- Project Environment Variable metadata；
-最终 AgentExecutionContext。

### 4.3 Secret

Snapshot 永远不保存 Secret value。

Secret 仅保存：

```text
name
description
secret = true
availability / permission metadata
```

真正 Secret value 只在具体执行后端需要时临时解析并注入。

## 5. Snapshot 与 Object Storage

不是所有 Snapshot Payload 都必须内嵌 PostgreSQL。

建议：

```text
small structured payload
    -> PostgreSQL JSON/JSONB

large payload / artifact
    -> Object Storage
    -> Snapshot 保存 StoredObject reference
```

适合 Object Storage 的内容包括：

- 大型 Project Context；
- 大型 `AGENTS.md`；
- 大型 Tool Result；
- 大型 Model Payload；
- generated artifact；
- 其他超过数据库合理内联大小的内容。

对象引用必须稳定且可权限校验。

## 6. Idempotency

Agent Launch Request 必须包含 `idempotency_key`。

唯一边界：

```text
idempotency_scope + idempotency_key
```

`idempotency_scope` 可以直接由调用来源推导，例如：

```text
task
meeting
manual
timer
webhook
```

或使用更明确的 caller scope。

### 6.1 重复请求

相同：

```text
idempotency_scope
+
idempotency_key
```

始终视为同一次 Launch。

重复请求直接返回第一次创建的 Agent Execution。

第一阶段不额外计算 request fingerprint，也不因为重复请求携带不同 payload 而产生 IdempotencyConflict。

因此调用方必须保证自身 key 生成逻辑正确。

### 6.2 持久化约束

数据库层应有唯一约束：

```text
UNIQUE(idempotency_scope, idempotency_key)
```

避免并发 Launch 重复创建。

## 7. Version 与并发控制

AgentExecution 使用递增 `version`。

关键 lifecycle mutation 必须基于当前 version 做 compare-and-swap。

概念：

```text
UPDATE agent_execution
SET
  status = ...,
  version = version + 1
WHERE
  id = ?
  AND version = ?
  AND status IN (...)
```

如果 affected rows = 0，则调用方重新读取状态并判断：

- stale request；
- duplicate resume；
- cancel 与 terminal transition 冲突；
-非法状态转换。

version 不用于 Runtime Item streaming。

Runtime Item 有自己的更新模型。

## 8. Retry Execution

terminal Execution 永不重新进入非终态。

需要重试时创建新 Execution：

```text
new_execution.retry_of_execution_id = old_execution.id
```

关系只表示“这是一次针对旧 Execution 的重新尝试”。

它不意味着：

- 自动复制所有业务状态；
- 自动重放 Tool Operation；
- 自动恢复未知副作用。

真正是否允许重试，由对应上层业务决定。

Agent Executor 不自动创建 replacement Execution。

## 9. AgentExecutionError

失败需要结构化 Error Contract。

概念结构：

```text
AgentExecutionError
├── code
├── category
├── stage
├── retryable
├── public_message
├── internal_detail?
├── cause_reference?
└── occurred_at
```

### 9.1 Category

第一阶段可以包括：

```text
invalid_configuration
context_build_failed
model_error
tool_error
runner_error
infrastructure_error
state_corruption
recovery_failed
cancel_propagation_failed
unknown
```

局部 timeout 可作为具体 code / cause，例如：

```text
model_timeout
tool_timeout
loop_watchdog_triggered
```

但不自动映射成 Execution-level `timed_out`。

### 9.2 Stage

建议：

```text
launch
preparing
running
waiting
resuming
cancelling
recovery
```

### 9.3 Public / Internal

`public_message` 可以用于 UI。

`internal_detail` 用于诊断，但必须确保：

- 不包含 Secret；
- 不包含 Provider private reasoning；
- 不复制不必要的敏感 Tool payload。

`cause_reference` 优先引用：

- Model Invocation；
- Tool Operation / Attempt；
- Runner Request；
- Checkpoint；
- Audit record。

## 10. Completion 语义

Agent Executor 不定义统一业务 `ExecutionResult`。

Execution 正常结束只需要记录：

```text
status = succeeded
finished_at
terminal_metadata
```

Agent 实际业务效果通过 Tool / Domain Service 写入：

- Task；
- Meeting；
- Knowledge；
- Object Storage；
- 其他领域。

Executor 不从 final text 推断业务状态，也不保存一份 trigger-specific business result。

`terminal_metadata` 只保存执行层必要信息，例如：

- completion reason；
-最后一个 runtime item reference；
- diagnostics correlation。

## 11. Retention

第一阶段：

- AgentExecution 不自动过期；
- AgentExecutionSnapshot 不自动过期；
- Runtime Item history 不自动过期；
- Execution Error 不自动过期。

后续若引入 retention，应统一考虑：

- Audit；
- Token Usage；
- Tool Operation；
- Model Invocation；
- Object Storage；
- Runtime View。

不要让 Agent Executor 单独使用与其他运行证据不一致的 TTL。

## 12. Agent Executor Service Boundary

业务模块不能直接修改 AgentExecution persistence。

Agent Executor 至少提供：

```text
launch(request)
get(execution_id)
cancel(execution_id)
resume(execution_id, waiting_reference)
get_runtime_view(execution_id)
subscribe_runtime(execution_id)
```

### 12.1 launch

- 做基础 request validation；
-基于 idempotency 创建或返回 Execution；
-返回 `execution_id`；
-异步进入 preparing。

### 12.2 get

返回生命周期主记录，不默认返回完整重型 Snapshot。

### 12.3 cancel

记录 cancellation request，并协调 Agent Loop / Model / Tool / Runner 停止。

### 12.4 resume

验证：

- Execution 当前为 waiting；
- `waiting_reference` 匹配；
-对应外部对象已 resolved；
-该 waiting condition 尚未被消费。

成功后恢复同一个 Execution。

Resume 必须幂等。

### 12.5 get_runtime_view / subscribe_runtime

只提供 Runtime View projection 与 RuntimeItemUpdate Stream。

内部 Tool / Model / Audit 诊断数据仍通过各自模块查询。

## 13. 数据关系概览

```text
AgentExecution
├── 1:1 AgentExecutionSnapshot
├── 1:N AgentExecutionRuntimeItem
├── 0:N ModelInvocation
├── 0:N ToolOperation
├── 0:N TokenUsage
├── 0:N Checkpoint
├── 0:N AuditRecord
├── 0:1 AgentExecutionError (terminal primary error)
└── 0:1 retry_of -> AgentExecution
```

这些关联不意味着 AgentExecution 是所有子对象的唯一 Source of Truth。

各模块仍拥有自己的领域记录。

## 14. 不在本文定义的内容

以下内容由其他文档定义：

- Snapshot 如何构造：[Execution Context](./execution-context.md)
-状态转换 / waiting / checkpoint / recovery：[Execution Lifecycle](./execution-lifecycle.md)
- Runtime Item / Stream：[Runtime View](./runtime-view.md)
- Model request / retry：[Model System](../platform-infrastructure/model-system.md)
- Tool Operation / Attempt：[Unified Tool Runtime](../tool-system/tool-runtime.md)
- Agent Loop 控制：[Agent Loop](../agent-loop/README.md)
