# Internal Domain Events 详细设计

> 状态：设计稿
>
> 上层架构：[平台基础设施与部署架构](./README.md)
>
> 相关设计：
> - [Realtime](./realtime.md)
> - [Human Inbox](./human-inbox.md)
> - [Task Event Timeline](../project-work-management/task-event-timeline.md)
> - [Meeting Timeline & Realtime](../meeting/meeting-timeline-realtime.md)

## 1. 定位

Internal Domain Events 是 agenteam Central 内部的模块解耦机制。

它的目标是让一个业务模块表达：

> 某个已经完成提交的业务事实发生了。

而不要求事件生产方直接知道所有后续消费者。

典型 Event：

- TaskCreated；
- TaskStateChanged；
- AgentExecutionStarted；
- AgentExecutionSucceeded；
- AgentExecutionFailed；
- AgentExecutionCancelled；
- ApprovalRequested；
- ApprovalResolved；
- MeetingProposed；
- DecisionAnswered；
- DocumentUpdated。

整体模型借鉴成熟的进程内 Event + Listener / Handler 机制：

~~~text
Typed Domain Event
    ↓
Event Bus
    ↓
Registered Handlers
~~~

但 agenteam 的 Domain Event 不是可取消的运行时 Hook。

本文明确：

~~~text
Domain Event
= immutable
= non-cancellable
= after-the-fact business fact
~~~

## 2. 非目标

Internal Domain Events 不负责：

- 代替业务 Service / Command；
- 在 Event Handler 中决定原业务操作是否允许；
- 跨模块分布式事务；
- Event Sourcing；
- 从全部历史 Event 重建业务实体；
- 全局严格事件顺序；
- 浏览器 Realtime transport；
- Scheduler correctness；
- Kafka / RabbitMQ 等独立 broker。

agenteam Central 当前是单后端、单 Central 实例。

因此 Event Bus 本身运行在 Central 进程内；PostgreSQL Outbox 提供 durable delivery。

## 3. 总体结构

~~~mermaid
flowchart LR
    Domain["Domain Operation"]
    PG["PostgreSQL Transaction"]
    State["Business State"]
    Outbox["DomainEventOutbox"]

    Dispatcher["Domain Event Dispatcher"]
    Bus["In-process Event Bus"]

    H1["Handler A"]
    H2["Handler B"]
    H3["Handler C"]

    Domain --> PG
    PG --> State
    PG --> Outbox

    Outbox --> Dispatcher
    Dispatcher --> Bus

    Bus --> H1
    Bus --> H2
    Bus --> H3
~~~

核心原则：

1. 所有 Domain Event 都写 PostgreSQL Outbox。
2. Outbox row 与对应业务状态变更在同一个 PostgreSQL transaction 中写入。
3. Commit 后 Dispatcher 才允许投递 Event。
4. Delivery 语义为 at-least-once。
5. Handler 必须幂等。
6. Handler 失败相互独立。
7. 只保证同一 aggregate 内的相对顺序。
8. 不提供全局 Event Priority。
9. 不做通用 Event Replay Framework。

## 4. Typed Event Model

代码侧使用明确的 Event type。

概念示例：

~~~text
DomainEvent

TaskCreatedEvent
TaskStateChangedEvent

AgentExecutionStartedEvent
AgentExecutionSucceededEvent
AgentExecutionFailedEvent
AgentExecutionCancelledEvent

ApprovalRequestedEvent
ApprovalResolvedEvent

MeetingProposedEvent
MeetingActivatedEvent
~~~

目标是让 Handler 订阅语义稳定的业务事实，而不是到处处理任意 map / string payload。

例如：

~~~text
Handle(TaskStateChangedEvent)
Handle(ApprovalRequestedEvent)
~~~

具体 Go interface / generic API 属于实现设计，但必须保留 typed event 的语义。

## 5. Immutable / Non-cancellable

Event 表示业务操作已经完成提交。

因此不支持：

~~~text
event.Cancel()
event.SetResult(...)
event.TaskState = ...
~~~

如果一个操作必须在发生之前校验：

- Authorization；
- Approval；
- Validation；
- Policy；
- Conflict；

应在原 Service / Command / Governance path 完成。

正确关系：

~~~text
Command
  ↓ validation / authorization / approval
Business Transaction
  ↓ commit
Domain Event
~~~

而不是：

~~~text
Command
  ↓
Event
  ↓ listener cancels business operation
~~~

## 6. DomainEventEnvelope

代码中的 typed Event 持久化到 Outbox 时使用统一 envelope。

第一阶段至少包含：

~~~text
DomainEventEnvelope
├── event_id
├── event_type
├── schema_version
├── occurred_at
├── project_id?
├── aggregate_type?
├── aggregate_id?
├── aggregate_version?
└── payload
~~~

### event_id

每次 Domain Event 的全局唯一 identity。

用于：

- delivery tracking；
- handler idempotency；
- diagnostics；
- retry / failed delivery。

### event_type

稳定逻辑名称，例如：

~~~text
task.state_changed
agent_execution.finished
approval.requested
meeting.proposed
~~~

Event type 名称不能依赖 Go package path、struct rename 等实现细节。

### schema_version

Event payload schema version。

Consumer 不能假定所有历史 row 永远都是当前 payload schema。

### occurred_at

表示业务事实发生时间。

不作为跨 aggregate 的严格顺序保证。

### project_id

Project-scoped Event 建议直接写入 envelope，便于：

- projection filter；
- diagnostics；
- downstream routing。

系统级 Event 可以为空。

### aggregate_type / aggregate_id / aggregate_version

用于表达同一业务 aggregate 的相对顺序。

例如：

~~~text
aggregate_type = task
aggregate_id = <task_id>
aggregate_version = 42
~~~

## 7. Payload Ownership

payload 由产生 Event 的 Domain 所有。

例如 Task Domain 定义：

~~~text
TaskStateChangedPayload
├── task_id
├── from_state
├── to_state
└── ...
~~~

平台 Event System 不把所有 Domain payload 强行标准化成一个超级 schema。

平台只统一：

- envelope；
- durable delivery；
- handler registration；
- ordering contract；
- retry / failure；
- diagnostics。

## 8. Transactional Outbox

所有 Domain Event 都写 Outbox。

核心 transaction：

~~~text
BEGIN

update business state
insert related business timeline/event if needed
insert DomainEventOutbox row

COMMIT
~~~

如果业务状态提交失败：

- Outbox Event 不存在。

如果 Outbox 写入失败：

- 同一 transaction rollback；
- 业务状态也不能提交。

因此不会出现：

~~~text
business state committed
but Domain Event permanently missing
~~~

## 9. DomainEventOutbox

建议至少包含：

~~~text
DomainEventOutbox
├── event_id
├── event_type
├── schema_version
├── occurred_at
├── project_id?
├── aggregate_type?
├── aggregate_id?
├── aggregate_version?
├── payload
├── dispatch_state
├── created_at
└── dispatched_at?
~~~

Outbox 负责保存：

> 这个 Event 必须被 Event System 投递。

它不等于 Event Handler delivery result。

因为同一个 Event 可能有多个 Handler，每个 Handler 可以独立成功或失败。

## 10. Dispatcher

Domain Event Dispatcher 是 Central 内部 worker。

职责：

1. 查询尚未完成 dispatch 的 Outbox Event；
2. 按允许的 ordering contract 取出 Event；
3. 反序列化 typed payload；
4. 交给 in-process Event Bus；
5. 记录 dispatch / handler delivery 结果；
6. 对失败 Handler 安排 retry。

Dispatcher 不创建新的业务事实。

如果 Handler 需要修改业务状态，应调用对应 Domain Service / Command。

## 11. In-process Event Bus

Event Bus 提供类似 Event + Listener / Handler 的模块解耦能力。

概念接口：

~~~text
register(event_type, handler)

publish(domain_event)
~~~

Handler registration 在 Central 启动时完成。

业务模块可以新增 Handler，而不要求修改 Event producer。

例如：

~~~text
TaskStateChanged
├── HumanInboxProjector
├── RealtimePublisher
└── FutureAnalyticsHandler
~~~

Task Domain 不需要知道这些 Handler 的存在。

## 12. Handler Identity

每个 durable consumer / handler 必须有稳定 handler_id。

示例：

~~~text
human_inbox.projector
meeting_timeline.projector
realtime.publisher
~~~

handler_id 用于：

- delivery state；
- retry；
- failed delivery；
- diagnostics；
- 幂等。

不能使用匿名函数地址或启动时随机值作为 durable handler identity。

## 13. Delivery 语义

确认采用：

~~~text
at-least-once
~~~

即：

- Event 不应因为进程 crash 永久丢失；
- 同一个 Handler 可能收到同一个 Event 多次；
- Consumer 必须幂等。

不承诺 exactly-once。

业务正确性不能依赖“某个 Handler 绝不会执行第二次”。

## 14. Handler Delivery State

建议持久化每个 Event / Handler 的 delivery state。

概念：

~~~text
DomainEventDelivery
├── event_id
├── handler_id
├── status
├── attempts
├── next_retry_at?
├── last_error_code?
├── last_error_message?
├── first_attempt_at?
└── completed_at?
~~~

status 至少表达：

~~~text
pending
processing
retry_wait
succeeded
failed
~~~

具体表结构可以与 Outbox 合并优化，但语义必须等价。

## 15. Handler 独立失败

同一个 Event 的不同 Handler 相互独立。

例如：

~~~text
TaskStateChanged

HumanInboxProjector -> succeeded
RealtimePublisher   -> succeeded
FutureHandler       -> failed
~~~

不能因为 FutureHandler 失败：

- 回滚已经提交的 Task；
- 回滚已成功 Human Inbox projection；
- 阻止其他 Handler 永久前进。

失败只影响：

~~~text
(event_id, handler_id)
~~~

这条 delivery。

## 16. Retry

失败 Handler 使用退避重试。

本文不固定：

- 最大 attempts；
- base delay；
- max delay；

这些属于部署 / implementation config。

但必须满足：

- 失败不是无限紧密 hot loop；
- retry state 持久化；
- Central restart 后仍能继续；
- error diagnostics 可查询。

## 17. Failed / Dead-letter

达到 retry 阈值后：

~~~text
delivery.status = failed
~~~

failed delivery 保留：

- event_id；
- handler_id；
- attempts；
- last error；
- payload / envelope reference；
- timestamps。

系统应支持人工 / 运维重新触发：

~~~text
retry failed delivery
~~~

第一阶段不要求独立 Dead Letter Queue 基础设施。

PostgreSQL 中的 failed delivery state 即可承担该职责。

## 18. Ordering

不提供全局 Event 顺序。

只保证：

> 同一个 aggregate 内，Handler 能够识别并按 aggregate_version / sequence 处理相对顺序。

例如：

~~~text
Task #123

version 41 -> state_changed
version 42 -> blocker_added
version 43 -> state_changed
~~~

必须避免 version 43 被 version 42 的迟到处理覆盖。

不同 aggregate：

~~~text
Task #123
Task #456
Meeting #789
~~~

不存在平台级业务总顺序。

## 19. Listener Priority

第一阶段不提供类似：

~~~text
LOW
NORMAL
HIGH
MONITOR
~~~

这样的 Event Handler Priority。

原因是 Priority 容易建立隐藏依赖：

~~~text
Handler B 必须在 Handler A 后运行
~~~

这会让 Event Bus 从解耦机制变成隐式 workflow engine。

如果业务存在明确先后依赖，应使用：

- 同一 Service transaction；
- explicit workflow / orchestration；
- 或 Handler 完成业务变化后产生新的 Domain Event。

## 20. Consumer 幂等

每个 durable Handler 必须显式满足幂等。

常见策略：

### processed event identity

~~~text
UNIQUE(handler_id, event_id)
~~~

### projection version

~~~text
if incoming.aggregate_version <= projection.source_version
-> no-op
~~~

### domain command idempotency

Handler 调用下游 Command 时使用稳定 request_id。

不同 Handler 可以选择不同策略，但平台不能假定自然幂等。

## 21. Event 与 TaskEvent

TaskEvent 与 Internal Domain Event 不同。

~~~text
TaskEvent
= 用户可见 Task 业务 Timeline

DomainEvent
= 后端模块集成机制
~~~

一次 Task transaction 可以同时写：

~~~text
Task row
TaskEvent
DomainEventOutbox
~~~

它们：

- schema 不同；
- audience 不同；
- query 语义不同；
- retention / consumer 不同；
- 不互为 Source of Truth。

## 22. Event 与 Realtime

Domain Event 与 Realtime Event 是两套 contract。

~~~text
Domain Event
= durable backend business fact
= PostgreSQL Outbox
= at-least-once

Realtime Event
= transient UI delivery / invalidation
= WebSocket
= can be lost
~~~

浏览器不直接订阅 Domain Event Bus。

需要 UI 更新时：

~~~text
Domain Event
-> RealtimePublisher
-> Realtime Event
-> WebSocket Gateway
~~~

或者业务模块在状态提交后根据自己的 UI contract 发布 Realtime Event。

完整设计见 [Realtime](./realtime.md)。

## 23. Event 与 Scheduler

Scheduler correctness 不依赖 Domain Event delivery。

Scheduler 继续根据：

- PostgreSQL Task state；
- SchedulerDispatch；
- 固定 tick traversal；

工作。

第一阶段 Scheduler 不作为 Domain Event Bus consumer。

未来即使增加 Event wake-up，也只能作为降低调度延迟的优化，不能变成唯一调度触发源。

## 24. Event Replay

第一阶段不提供通用 Event Replay Framework。

Outbox 不是 Event Store。

业务对象当前状态仍然来自：

- Task；
- Meeting；
- AgentExecution；
- Approval；
- Knowledge；
- 其他 canonical tables。

Projection 重建使用：

~~~text
Source of Truth
-> module-specific rebuild
~~~

而不是：

~~~text
replay every historical event
-> rebuild whole system
~~~

failed delivery 可以 retry / redelivery，但这不等于通用历史 replay。

## 25. Retention

第一阶段不自动清理 Outbox / delivery 历史的具体 retention 规则。

如果未来事件规模要求归档 / cleanup，应单独增加 retention policy。

在引入 retention 前，必须确认：

- failed delivery diagnostics；
- audit / troubleshooting 需求；
- projection rebuild 不依赖完整 event history。

## 26. Observability

至少需要可查询：

- pending Event 数；
- oldest pending age；
- retry_wait 数；
- failed delivery 数；
- per handler latency；
- per event type throughput；
- last error。

日志中可以记录：

- event_id；
- event_type；
- handler_id；
- aggregate reference；

但 payload 仍需遵守：

- Secret masking；
- sensitive data policy；
- 不把完整业务 payload 无条件写 operational log。

## 27. Central Restart

Central restart 后：

1. 重新注册 typed Handler；
2. Dispatcher 查询 PostgreSQL 中未完成 delivery；
3. 继续 pending / retry_wait；
4. processing 状态按 recovery 规则重新判定；
5. succeeded delivery 不重复产生业务副作用。

Event correctness 不依赖进程内队列 surviving restart。

## 28. 第一阶段实现边界

第一阶段实现：

1. typed Domain Event；
2. stable event_type；
3. DomainEventEnvelope；
4. PostgreSQL transactional outbox；
5. in-process Event Bus；
6. stable handler_id；
7. at-least-once delivery；
8. per-handler delivery state；
9. retry / failed delivery；
10. aggregate ordering contract；
11. consumer idempotency；
12. diagnostics；
13. manual failed delivery retry。

第一阶段不实现：

- Kafka / RabbitMQ / NATS；
- distributed Event Bus；
- exactly-once；
- Event Sourcing；
- 通用 Replay；
- global ordering；
- Listener Priority；
- cancellable Event；
- Redis event backplane。
