# Human Inbox 详细设计

> 状态：设计稿
>
> 上层架构：[平台基础设施与部署架构](./README.md)
>
> 相关设计：
> - [Internal Domain Events](./internal-domain-events.md)
> - [Realtime](./realtime.md)
> - [安全与治理架构](../security-governance/README.md)
> - [Meeting 架构](../meeting/README.md)
> - [项目与工作管理](../project-work-management/README.md)

## 1. 定位

Human Inbox 是 agenteam 中统一的人类待处理入口。

它解决的问题不是“再保存一份 Approval / Meeting / Task 状态”，而是把散落在不同业务模块中的人类待办统一投影成一个可排序、分页、实时更新的列表。

典型来源包括：

- pending Approval Request；
- proposed Meeting；
- pending DecisionRequest；
- 明确面向 Human 的 review request；
- standalone waiting_for_human / technical blocker；
- Runner / Execution 等由 Source Domain 形成稳定业务事实后需要用户介入的异常或提醒；
- 未来其他需要用户处理的业务对象。

核心关系：

~~~text
Source Domain Object
    ↓
Domain Event
    ↓
HumanInboxProjector
    ↓
HumanInboxItem
    ↓
Human Inbox UI
~~~

源业务对象仍然是 Source of Truth。

HumanInboxItem 是 read-side projection，只负责回答：

- 当前有哪些事情需要用户处理；
- 哪些事项更紧急或更阻塞；
- 用户从哪里进入或直接执行对应操作；
- 某个待办在 Inbox 中当前是 open、resolved 还是 dismissed。

## 2. 核心原则

1. Human Inbox 是 projection，不是新的业务 Source of Truth。
2. Inbox Item 有自己的 projection lifecycle，但不能复制源对象的业务状态机。
3. 同一个待处理动作只维护一条稳定 Item，不按事件重复追加。
4. Projection 由 durable Domain Event 驱动，允许最终一致。
5. Projection consumer 必须幂等。
6. 必须处理型 Item 不允许 dismiss。
7. resolved Item 第一阶段长期保留，不自动过期。
8. Projection 可以从当前 Source of Truth 重建，不依赖完整 Event Replay。
9. 用户直接处理 Approval 等业务动作时，调用对应 Source Domain API，而不是 Human Inbox 自己实现业务逻辑。
10. Realtime 只负责低延迟刷新，不承担 Projection correctness。

## 3. HumanInboxItem

建议数据模型：

~~~text
HumanInboxItem
├── id
├── project_id
├── source_type
├── source_id
├── action_type
├── status
├── blocking
├── priority
├── dismissible
├── title_snapshot?
├── summary_snapshot?
├── source_version?
├── source_occurred_at?
├── created_at
├── updated_at
├── resolved_at?
└── dismissed_at?
~~~

### 3.1 id

HumanInboxItem 自己的内部 identity。

不使用 source_id 直接作为主键，因为不同 source_type / action_type 可能引用同一个外部 ID。

### 3.2 project_id

Human Inbox 属于 Project scope。

当前产品是单用户 Project Owner 模型，因此查询时至少约束：

~~~text
item.project_id
-> project.owner_user_id
-> current_user.id
~~~

### 3.3 source_type / source_id

指向真正拥有业务状态的源对象。

例如：

~~~text
source_type = approval_request
source_id   = <approval_request_id>

source_type = meeting
source_id   = <meeting_id>

source_type = decision_request
source_id   = <decision_request_id>
~~~

Human Inbox 不通过这些字段反向成为 source object owner。

普通 Task `state = in_review` 不自动进入 Human Inbox。当前 Task review 流程默认由 `assignee_agent_id` 指向 reviewer，并由 Scheduler 驱动 review Execution；只有某个业务流程明确要求 Human Review 时，才建立 Human-targeted Inbox Item。

Task Blocker 也遵守 canonical source 去重：

- 如果 Blocker 只是引用已经拥有 Human Inbox Item 的 ApprovalRequest、DecisionRequest、proposed Meeting 等 actionable object，则继续以原对象作为 Inbox source，不再为 Blocker 创建第二条 Item；
- 只有 Blocker 自己就是独立待处理对象时，才使用 `source_type = task_blocker`。

Runner protocol frame、RunnerStreamEvent、runner_status 等协议消息不能直接成为 Human Inbox source。若 Runner 状态需要进入 Inbox，必须先由 Runner Management 形成 canonical state / failure fact，再通过 typed Internal Domain Event 驱动 projection。

### 3.4 action_type

表示“用户当前需要做什么”，也是稳定去重 identity 的一部分。

示例：

~~~text
approval.resolve
meeting.review_proposal
decision.answer
review.resolve
technical_blocker.inspect
~~~

具体 action_type 集合由来源模块定义；Human Inbox 只要求它是稳定、可路由的逻辑标识。

## 4. Stable Identity 与去重

确认采用：

~~~text
(source_type, source_id, action_type)
~~~

作为同一待处理事项的稳定 identity。

建议数据库唯一约束：

~~~text
UNIQUE(source_type, source_id, action_type)
~~~

同一 Source Object 的重复 Domain Event：

~~~text
ApprovalRequested
ApprovalUpdated
ApprovalUpdated
~~~

都只能更新同一个 HumanInboxItem。

Human Inbox 不是 Event Timeline，因此不能因为重复事件创建多个视觉上相同的待办。

## 5. Projection Lifecycle

第一阶段至少定义：

~~~text
open
resolved
dismissed
~~~

### open

当前 Source Domain 仍要求用户处理。

### resolved

对应待处理动作已经完成、失效或不再需要用户处理。

resolved 只是 Inbox projection 状态。

例如：

~~~text
ApprovalRequest.status = approved
        ↓
HumanInboxItem.status = resolved
~~~

真正的 Approval 结果仍然属于 Governance。

### dismissed

用户主动隐藏一个允许忽略的普通提醒。

dismissed：

- 不修改 Source Object；
- 不等于 Source Object resolved；
- 不允许用于 Approval / Review / Decision 等必须处理型事项。

## 6. Dismiss Policy

每种 action_type 必须明确是否 dismissible。

第一阶段规则：

~~~text
Approval / Review / Decision
-> dismissible = false

ordinary notification / non-blocking reminder
-> 可以 dismiss
~~~

因此 API 不能只依赖前端隐藏按钮。

服务端 dismiss 时必须再次检查：

~~~text
item.status == open
AND item.dismissible == true
~~~

否则拒绝操作。

## 7. Source State 与 Projection State

两套状态必须明确分层。

~~~text
ApprovalRequest.status
Meeting.status
DecisionRequest.status
Task / Blocker state
= Source of Truth

HumanInboxItem.status
= Inbox projection lifecycle
~~~

因此文档中所谓“Human Inbox 不保存第二份状态”的准确含义是：

> Human Inbox 不复制源业务对象的权威业务状态；但它拥有自己的 projection lifecycle。

例如 HumanInboxItem 不应该保存：

~~~text
approval_status = approved
meeting_status = active
task_status = blocked
~~~

作为新的权威字段。

它可以保存面向列表展示的 snapshot：

- title_snapshot；
- summary_snapshot；
- blocking；
- priority；

但这些只是 projection data。

## 8. Projection Event Flow

Human Inbox 通过 Internal Domain Events 更新。

~~~mermaid
flowchart LR
    Governance["Security / Governance"]
    Meeting["Meeting"]
    Work["Work Management"]

    Outbox["PostgreSQL Outbox"]
    Bus["In-process Event Bus"]
    Projector["HumanInboxProjector"]
    Inbox["HumanInboxItem"]

    Governance --> Outbox
    Meeting --> Outbox
    Work --> Outbox

    Outbox --> Bus
    Bus --> Projector
    Projector --> Inbox
~~~

典型映射：

~~~text
ApprovalRequestedEvent
-> create / reopen approval.resolve Item

ApprovalResolvedEvent
-> resolve approval.resolve Item

MeetingProposedEvent
-> create / reopen meeting.review_proposal Item

MeetingActivatedEvent / MeetingArchivedEvent / MeetingDeletedEvent
-> resolve meeting.review_proposal Item

DecisionRequestedEvent
-> create / reopen decision.answer Item

DecisionAnsweredEvent / DecisionSkippedEvent / DecisionCancelledEvent
-> resolve decision.answer Item

AgentExecutionLongRunningWarningEvent
-> create / reopen execution.inspect_long_running Item

AgentExecutionSucceededEvent / AgentExecutionFailedEvent / AgentExecutionCancelledEvent
-> resolve execution.inspect_long_running Item if it is open
~~~

上述名称必须与 Source Domain 的真实状态机语义一致。Meeting 没有 rejected 状态，因此“拒绝 proposed Meeting”表现为 `proposed -> archive` 并产生 MeetingArchivedEvent；DecisionRequest 没有 invalidated 状态，因此 Turn / Execution cancellation 对应 DecisionCancelledEvent。

具体 Event type 仍归各 Source Domain 所有，Human Inbox 不另造平行状态或事件词汇。

Human Inbox 只定义 projection contract。

## 9. Projection 幂等

Internal Domain Events 是 at-least-once delivery，因此 HumanInboxProjector 必须支持重复处理。

每个处理器至少使用：

~~~text
event_id
source_version?
~~~

进行幂等保护。

实现可以选择：

- 保存 projector_name + event_id；
- 或使用 source_version / projection_version；
- 或同时使用。

要求是：

- 重复 Event 不重复创建 Item；
- 旧 Event 不覆盖新 projection；
- resolve Event 重试不会产生副作用；
- projection update 必须是可重放的 deterministic operation。

## 10. Ordering

Human Inbox 不依赖全局 Event 顺序。

同一个 source aggregate 的 Event 使用 Internal Domain Events 定义的 aggregate ordering 语义。

如果收到旧 source_version：

~~~text
incoming source_version < projected source_version
-> ignore / no-op
~~~

如果 Source Domain 不提供 version，则 Projector 需要使用其稳定的状态查询或 occurred_at 等保守策略，不能假定全局 Event 严格有序。

## 11. 排序

确认排序原则：

1. blocking / priority 优先；
2. 同组内按最近更新时间倒序。

概念上：

~~~text
blocking DESC
priority DESC
updated_at DESC
id DESC
~~~

具体 priority 的内部取值范围属于实现配置，不在本文固定数字枚举。

### blocking

表达：

> 如果用户不处理，该事项是否明确阻塞 Agent Team / 当前工作继续推进。

例如 pending Approval / Decision 通常属于 blocking。

普通告警或非关键提醒可以是 non-blocking。

## 12. Query

默认 Inbox 只展示：

~~~text
status = open
~~~

建议接口支持：

~~~text
list_inbox_items(
    project_id,
    status?,
    source_type?,
    blocking?,
    cursor?
)
~~~

默认排序使用上一节规则。

resolved / dismissed 历史可以通过筛选查看。

第一阶段 resolved Item 不自动清理。

## 13. Action Routing

Human Inbox 自己不执行业务状态机。

Item 的操作分为两类。

### 13.1 Direct Action

例如 Approval：

~~~text
Human Inbox
-> Governance Approval Action Contract
-> approve / reject
~~~

Human Inbox 只是统一入口。

审批结果由 Governance 保存。

随后 ApprovalResolved Domain Event 驱动 Inbox Item -> resolved。

### 13.2 Navigation Action

例如 proposed Meeting：

~~~text
Human Inbox
-> Open Meeting
-> 用户在 Meeting 页面批准 / 拒绝
~~~

Human Inbox 只提供 source reference 和导航。

对应业务操作完成后，由 Meeting Domain Event 更新 Inbox projection。

## 14. Approval Request

Approval Request 是 Human Inbox 中最重要的可直接操作类型之一。

关系：

~~~text
ApprovalRequest
= Governance Source of Truth

HumanInboxItem
= approval.resolve projection

Approval UI / Action Contract
= shared interaction
~~~

Human Inbox、Meeting Timeline、Task / Execution Detail 可以同时展示同一个 Approval Request。

这些入口：

- 不复制 Approval 状态；
- 不创建自己的 approve / reject API；
- 都调用统一 Governance API。

Approval Item：

~~~text
dismissible = false
~~~

## 15. Meeting / Decision

proposed Meeting：

~~~text
HumanInboxItem
source_type = meeting
action_type = meeting.review_proposal
~~~

默认是导航型 Item。

DecisionRequest：

~~~text
HumanInboxItem
source_type = decision_request
action_type = decision.answer
~~~

可以导航到对应 Meeting / Execution interaction UI。

两者的源状态仍由 Meeting Domain 持有。

## 16. Review / Blocker / Technical Item

Human Inbox 可以继续接入：

- Task Review；
- waiting_for_human；
- technical blocker；
- Runner failure；
- long-running execution warning；
- 未来其他来源。

接入新 source 时至少必须定义：

1. source_type；
2. source_id；
3. action_type；
4. 什么 Event 打开 Item；
5. 什么 Event resolve Item；
6. blocking；
7. dismissible；
8. direct action 或 navigation action；
9. rebuild 如何从 Source of Truth 找回 open Item。

## 17. Projection Rebuild

第一阶段不依赖通用 Event Replay。

Human Inbox 必须提供专用 rebuild。

概念流程：

~~~text
pause / isolate projector writes if needed
    ↓
scan current Source of Truth
    ↓
find all currently actionable objects
    ↓
upsert corresponding open HumanInboxItem
    ↓
resolve stale projection items
    ↓
resume normal event projection
~~~

例如：

- 查询 pending Approval Request；
- 查询 proposed Meeting；
- 查询 pending DecisionRequest；
- 查询明确面向 Human 的 pending review，以及 standalone actionable blockers；
- 重建 projection。

这样 Human Inbox 可以从当前业务事实恢复，而不需要从历史第一条 Domain Event 开始 replay。

## 18. Realtime

Human Inbox 使用平台统一 Realtime WebSocket Gateway。

Projection commit 后可以发布：

~~~text
inbox.item.created
inbox.item.updated
inbox.item.resolved
inbox.item.dismissed
~~~

这些是 Realtime Event，不是 Domain Event。

浏览器漏掉 realtime event 不影响正确性。

断线后：

~~~text
reconnect WebSocket
    ↓
subscribe project inbox
    ↓
buffer inbox realtime events
    ↓
reload Inbox API
    ↓
if buffered inbox event exists:
  coalesce dirty item / list invalidation
  refresh affected authoritative Inbox read model
    ↓
live
~~~

Human Inbox 第一阶段不要求单独引入全局 snapshot revision。缓冲的 inbox state event 主要作为 invalidation 使用，不能把一个无法证明比 Snapshot 更新的旧 payload 盲目覆盖当前 projection。

这样既遵循 Platform subscribe-first contract，又不改变 HumanInboxItem 的 Source of Truth / projection lifecycle。

完整 transport 规则见 [Realtime](./realtime.md)。

## 19. Transaction Boundary

HumanInboxProjector 消费 Domain Event 时，单个 projection 更新应在数据库事务中完成。

例如：

~~~text
BEGIN
  idempotency / event delivery check
  upsert HumanInboxItem
  mark handler delivery success
COMMIT
~~~

如果事务失败：

- Handler retry；
- 不产生部分更新。

Source Domain transaction 与 Human Inbox projection transaction 不要求是同一个事务。

它们通过 durable Domain Event 实现最终一致。

## 20. 删除与失效

如果 Source Object hard delete：

- Source Domain 应产生对应 Domain Event；
- HumanInboxProjector resolve 或删除其 open projection。

默认优先：

~~~text
open -> resolved
~~~

保留历史。

只有明确属于临时 projection、且产品不需要历史时，未来才考虑物理删除。

## 21. 权限

Human Inbox 查询按 Project Owner boundary 校验。

即使用户拥有某个 HumanInboxItem ID，也不能绕过：

~~~text
item.project_id
-> Project owner check
~~~

Direct Action 仍必须再次经过 Source Domain 自己的服务端权限校验。

Human Inbox 的权限通过不等于 Source Action 自动授权。

## 22. 第一阶段实现边界

第一阶段实现：

1. HumanInboxItem projection；
2. open / resolved / dismissed；
3. stable identity；
4. blocking / priority sorting；
5. dismiss policy；
6. Domain Event projector；
7. consumer idempotency；
8. Approval / proposed Meeting / Decision 等现有来源接入；
9. open + history query；
10. 专用 rebuild；
11. WebSocket realtime update；
12. direct action / navigation routing。

第一阶段不要求：

- 通用跨产品 Notification Center；
- 用户自定义 Inbox rule；
- Snooze；
- 多用户 assignment；
- 邮件 / Push 等外部通知渠道；
- 自动 retention；
- Event Sourcing / 全历史 replay。
