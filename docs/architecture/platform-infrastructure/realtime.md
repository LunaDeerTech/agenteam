# Realtime 详细设计

> 状态：设计稿
>
> 上层架构：[平台基础设施与部署架构](./README.md)
>
> 相关设计：
> - [Internal Domain Events](./internal-domain-events.md)
> - [Human Inbox](./human-inbox.md)
> - [Agent Executor Runtime View](../agent-executor/runtime-view.md)
> - [Meeting Timeline & Realtime](../meeting/meeting-timeline-realtime.md)

## 1. 定位

Realtime 是 agenteam Central 向浏览器推送短生命周期 UI 更新的统一平台能力。

第一阶段统一使用：

> WebSocket Realtime Gateway

典型消费者包括：

- Agent Execution Runtime View；
- Meeting Timeline；
- Human Inbox；
- Task / Review 页面状态更新；
- 未来其他需要低延迟刷新的 UI。

Realtime 的核心目标是：

- 降低 UI 状态变化的感知延迟；
- 避免每个模块自己维护一套 WebSocket / SSE；
- 提供统一连接、订阅、权限、backpressure 和断线恢复边界。

Realtime 不承担业务状态持久化。

## 2. 核心原则

1. WebSocket 是第一阶段统一 realtime transport。
2. Central 当前单实例运行，Realtime 使用进程内 fan-out。
3. 不使用 Redis Pub/Sub。
4. Realtime Event 与 Domain Event 分离。
5. Realtime Event 可以丢失。
6. 业务正确性必须由普通 API + Source of Truth 恢复。
7. 断线重连先恢复 subscription 并缓冲事件，再读取 Snapshot / Read Model，完成对齐后继续 live consumption。
8. 浏览器不直接订阅内部 Domain Event schema。
9. subscription 必须经过 Project / Resource scope 权限校验。
10. Slow consumer 不能拖死 Central 或业务写路径。

## 3. 总体结构

~~~mermaid
flowchart LR
    Domain["Domain / Projection"]
    Publisher["Realtime Publisher"]
    Hub["In-process Realtime Hub"]
    Gateway["WebSocket Gateway"]
    Browser["Browser"]

    Domain --> Publisher
    Publisher --> Hub
    Hub --> Gateway
    Gateway --> Browser
~~~

如果更新来自 durable Domain Event：

~~~text
Domain Event
-> Handler / RealtimePublisher
-> Realtime Event
-> In-process Hub
-> WebSocket
~~~

如果某模块拥有自己的 streaming runtime，例如 RuntimeItemUpdate，也可以：

~~~text
Runtime Store / Executor
-> Realtime Publisher
-> Realtime Event
-> WebSocket
~~~

只要最终遵守统一 Realtime contract。

## 4. Realtime Event 与 Domain Event

必须明确区分。

### Domain Event

~~~text
durable
backend internal
PostgreSQL Outbox
at-least-once
business fact
~~~

### Realtime Event

~~~text
transient
UI-facing
WebSocket
best-effort
projection update / invalidation / stream delta
~~~

例如：

~~~text
Domain Event:
approval.resolved

Realtime Event:
inbox.item.resolved
meeting.timeline.item.updated
~~~

一个 Domain Event 可以导致零个、一个或多个 Realtime Event。

Realtime Event 不需要写 DomainEventOutbox。

## 5. Event Envelope

平台统一 Realtime Event envelope。

建议：

~~~text
RealtimeEvent
├── event_type
├── project_id?
├── resource_type?
├── resource_id?
├── subscription_key?
├── revision?
├── occurred_at
└── payload
~~~

具体字段可以按实现收敛，但必须能够支持：

- routing；
- permission scope；
- client-side dispatch；
- optional revision / idempotent update。

Realtime Event 不要求全局 event_id 持久化。

## 6. Event Type

示例：

~~~text
inbox.item.created
inbox.item.updated
inbox.item.resolved

meeting.timeline.item.created
meeting.timeline.item.updated
meeting.participant.updated
meeting.turn.updated
meeting.summary.updated

execution.runtime.item.started
execution.runtime.item.updated
execution.runtime.item.completed

task.updated
~~~

UI-facing Event type 与内部 Domain Event type 独立演进。

## 7. WebSocket Connection

浏览器登录后建立统一 Realtime WebSocket。

概念：

~~~text
Browser
-> authenticated WebSocket
-> Realtime Gateway
~~~

第一阶段不要求：

- 每个模块一条 WebSocket；
- Meeting 一条、Runtime 一条、Inbox 一条独立连接。

平台允许在同一个连接上维护多个 logical subscription。

具体 route 名称属于 API 实现，不在本文固定。

## 8. Authentication

WebSocket 建连必须使用当前 Web Session / Auth identity。

服务端在 connection context 中绑定：

~~~text
current_user_id
session / auth context
~~~

匿名连接不能订阅 Project realtime。

Credential 不进入 Realtime Event payload。

## 9. Subscription

客户端显式订阅逻辑资源。

概念：

~~~text
project:<project_id>:inbox
meeting:<meeting_id>:timeline
execution:<execution_id>:runtime
~~~

实际 wire protocol 可以使用结构化 JSON，而不是要求使用上述字符串格式。

核心要求：

- subscription key 稳定；
- 服务端可以做 resource permission check；
- 不允许客户端通过猜测 resource ID 越权订阅。

## 10. Authorization

每次 subscribe 必须在服务端校验。

当前单用户 Project Owner 模型下，核心是：

~~~text
resource.project_id
-> project.owner_user_id
-> current_user.id
~~~

Execution / Meeting / Task 等具体资源还要验证它确实属于该 Project。

不能因为 WebSocket 在登录后建立，就默认允许任意后续 subscription。

## 11. In-process Fan-out

当前架构明确：

> 单 Central，不考虑多个 Central 实例。

因此 Realtime Hub 直接位于当前 Central 进程内。

~~~text
RealtimePublisher
-> in-memory hub
-> matching connections
~~~

不引入：

- Redis Pub/Sub；
- NATS；
- Kafka；
- external realtime broker。

如果未来明确演进到多 Central，再重新设计跨实例 backplane。

当前不为未计划的水平扩展保留 Redis 运行依赖。

## 12. Realtime Delivery Semantics

第一阶段：

~~~text
best-effort transient delivery
~~~

允许：

- Browser 断网漏 Event；
- tab sleep 漏 Event；
- Central restart 中断连接；
- slow consumer 被主动断开。

不允许业务把：

~~~text
收到 WebSocket Event
~~~

作为业务操作已经发生的唯一证据。

客户端 authoritative state 始终来自普通 API / Snapshot。

## 13. Snapshot + Subscription

所有重要 Realtime UI 都必须有 Snapshot / Read API。

例如：

~~~text
Human Inbox
subscribe inbox realtime
+
GET current inbox

Meeting
subscribe meeting timeline realtime
+
GET timeline

Execution Runtime View
subscribe runtime updates
+
GET runtime snapshot
~~~

第一阶段统一采用 subscribe-first：先完成 subscription acknowledgement 并开始缓冲当前资源的 Realtime Event，再读取 authoritative Snapshot。

Realtime 只负责增量体验，Snapshot / Read API 仍是 authoritative view。

## 14. Snapshot / Stream Race

Snapshot 与 subscribe 之间可能发生更新。

第一阶段统一 contract：

~~~text
connect WebSocket
    ↓
subscribe resource
    ↓
subscription acknowledged
    ↓
buffer resource events
    ↓
GET authoritative Snapshot
    ↓
reconcile buffered events
    ↓
enter normal live consumption
~~~

Reconcile 由具体模块选择，但不能跳过：

- Execution Runtime View 使用每 Execution 独立更新进度，覆盖 started / delta / updated / completed / failed；Snapshot 返回实际已包含内容的水位，只对齐该边界之后的 buffered update。RuntimeItem.seq 仅用于条目创建排序，不能充当更新水位；
- 如果 read model 没有可比较的 revision，例如第一阶段 Meeting Timeline / Human Inbox，可以把 buffered state event 视为 invalidation，按 stable resource identity 合并 dirty 标记并重新读取对应 authoritative read model，而不是盲目把可能早于 Snapshot 的 payload 覆盖到客户端状态；
- 相同 resource update 必须能够幂等处理或安全转化为 refresh；
- 任何时候客户端都能通过 Snapshot 校正最终状态。

仅 subscribe-first 不能覆盖“订阅前已发送、Snapshot 批量持久化尚未 flush”的 Runtime delta。Executor 的 Snapshot / resync 契约必须让该内容实际进入返回视图，或在进入 live consumption 前完成相应恢复，不能用最新已发送编号冒充快照已包含水位。完整边界见 [Runtime View](../agent-executor/runtime-view.md)。

Execution 更新进度的具体 revision/offset 编码、重复/乱序/缺口、终态完整性、分页/筛选与重启代际由 D01/D22/D25 固定；其他 read model 可保留 invalidation + refresh，不要求平台新增全局 cursor。

## 15. Reconnect

统一原则：

~~~text
connection lost
    ↓
reconnect WebSocket
    ↓
restore subscriptions
    ↓
buffer incoming realtime events
    ↓
reload authoritative Snapshot / Read Model
    ↓
reconcile buffer
    ↓
resume live consumption
~~~

第一阶段不要求平台提供：

- durable WebSocket queue；
- 全局 cursor；
- 任意历史 Realtime replay。

Execution Runtime View 使用自身更新进度和实际快照水位；重启或无法证明连续时沿 Snapshot/resync 恢复，不拿 RuntimeItem.seq 推断丢失更新。这不改变平台 Realtime 的 transient 语义，也不新增持久 delta 日志或逐 token 写库。

## 16. Backpressure

Realtime Gateway 必须保护 Central。

每个 connection / subscription 应有有限 buffer。

如果客户端消费过慢：

1. 不允许无限增长内存；
2. 可以 coalesce 可覆盖更新；
3. 对不可合并 stream 可以达到阈值后断开连接；
4. 客户端重连后重新加载 Snapshot。

Realtime slow consumer 不能反向阻塞：

- Agent Loop；
- Domain transaction；
- Event Dispatcher；
- Scheduler；
- Meeting Runtime。

## 17. Coalescing

对于状态型 Event，例如：

~~~text
task.updated
inbox.item.updated
meeting.summary.updated
~~~

允许实现对同一 resource 的旧未发送 update 做 coalesce。

对于 ordered stream delta，例如：

~~~text
RuntimeItem text delta
~~~

不能无条件丢弃中间 delta。

如果 stream 无法继续安全发送：

- 断开 / 标记 resync required；
- 客户端重新加载 Runtime Snapshot。

## 18. Runtime View

Agent Executor Runtime View 使用统一 Realtime WebSocket。

逻辑：

~~~text
subscribe execution runtime
-> acknowledge and buffer
-> GET Runtime View Snapshot with actual included progress
-> reconcile / resync
-> live updates
~~~

RuntimeItem 的 item_id / 创建排序 seq、每 Execution 更新进度及 Snapshot 实际包含水位由 Agent Executor 定义。对终态 Execution，只有完整终态 Snapshot 才能直接只读；不能因状态已 terminal 就省略尚未入快照内容的恢复。

Realtime 平台只负责：

- subscription；
- routing；
- transport；
- connection lifecycle；
- backpressure。

不复制 Runtime View domain model。

## 19. Meeting Timeline

Meeting Timeline 使用：

~~~text
GET Meeting Timeline
+
subscribe meeting timeline
~~~

Meeting 逻辑事件包括：

~~~text
meeting.timeline.item.created
meeting.timeline.item.updated
meeting.participant.updated
meeting.turn.updated
meeting.summary.updated
~~~

Meeting 不单独维护 SSE / WebSocket 实现。

## 20. Human Inbox

Human Inbox 使用：

~~~text
subscribe current user's inbox scope
-> acknowledge and buffer
-> GET current user's open inbox items
-> reconcile invalidation / refresh
~~~

Inbox 可聚合本人全部项目并按项目筛选；查询与订阅都以服务端 Project Owner 关系授权，管理员身份不越过该边界。具体跨本人项目的订阅/游标接口由 D01/D25/D26 固定，不扩为跨用户通知中心。

Projection commit 后可以发布：

~~~text
inbox.item.created
inbox.item.updated
inbox.item.resolved
inbox.item.dismissed
~~~

这些 Event 丢失时，重新 GET Inbox 即可恢复。

## 21. Domain Event Handler 与 Realtime Publisher

RealtimePublisher 可以作为 Internal Domain Event Handler。

例如：

~~~text
TaskStateChanged Domain Event
-> RealtimePublisher
-> task.updated
~~~

但不是所有 realtime 都必须经过 Domain Event。

例如模型 token streaming / RuntimeItem delta 本身是 runtime stream，可以直接：

~~~text
Agent Executor
-> RealtimePublisher
-> execution runtime event
~~~

区别在于：

- durable business fact 先由 Source of Truth / Domain Event 保证；
- Realtime 只负责展示。

## 22. Central Restart

Central restart：

- 所有 WebSocket 断开；
- in-memory Hub 状态丢失；
- 不需要恢复 in-memory Event queue；
- Browser reconnect；
- restore subscriptions 并缓冲；
- reload Snapshot、对齐 / resync 后继续 live consumption。

业务状态不受影响。

## 23. Resource Deleted / Access Lost

如果订阅中的资源：

- hard delete；
- Project access 失效；
- resource scope 不再可见；

Gateway 可以发送 terminal realtime notification，例如：

~~~text
resource.deleted
resource.access_lost
~~~

然后移除 subscription。

即使该通知丢失，后续 API / reconnect 也必须正确返回 not found / forbidden。

## 24. Connection Lifecycle

概念状态：

~~~text
connecting
authenticated
active
closing
closed
~~~

订阅状态只存在于当前 connection lifecycle。

不把 active WebSocket subscription 持久化到 PostgreSQL。

## 25. Heartbeat

WebSocket 实现应使用 ping / pong 或等价 heartbeat：

- 发现断链；
- 清理 zombie connection；
- 保持代理 / LB 连接。

具体 interval 属于 Deployment / Implementation config，不在本文固定。

## 26. Observability

至少统计：

- active connections；
- active subscriptions；
- events published；
- events dropped / coalesced；
- slow consumer disconnect；
- send queue depth；
- reconnect rate；
- per event type throughput。

日志不能写：

- Secret；
- credential；
- private reasoning；
- 大型 streaming payload 全量内容。

## 27. 与 Redis 的边界

当前正式架构：

> Realtime 不使用 Redis。

原先 Redis 的 realtime / pubsub 设想全部删除。

当前：

~~~text
single Central
-> in-process Realtime Hub
~~~

未来如果系统明确支持：

~~~text
Central A
Central B
Central C
~~~

再单独设计：

- cross-instance backplane；
- connection ownership；
- subscription routing；
- presence；
- distributed fan-out。

届时重新评估 Redis、NATS 或其他方案。

## 28. 第一阶段实现边界

第一阶段实现：

1. 统一 WebSocket Gateway；
2. authenticated connection；
3. logical subscription；
4. resource authorization；
5. structured Realtime Event；
6. in-process fan-out；
7. bounded send buffer；
8. slow consumer handling；
9. reconnect + Snapshot reload；
10. Runtime View 集成；
11. Meeting Timeline 集成；
12. Human Inbox 集成；
13. basic observability。

第一阶段不实现：

- Redis Pub/Sub；
- external message broker；
- multi-Central realtime；
- durable realtime queue；
- global cursor；
- arbitrary event replay；
- offline push；
- mobile push notification。
