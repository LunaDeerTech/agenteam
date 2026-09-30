# Task Event Timeline 详细设计

> 状态：设计稿
>
> 上层架构：[项目与工作管理架构](./README.md)
>
> 相关设计：
> - [Task Domain Model](./task-domain-model.md)
> - [Task State Machine](./task-state-machine.md)
> - [Task Blocker / Dependency](./task-blocker-dependency.md)
> - [Agent Executor](../agent-executor/README.md)
> - [平台基础设施](../platform-infrastructure/README.md)

## 1. 设计范围

本文定义 Task Domain 的用户可见事件时间线，包括 TaskEvent schema、event type、actor、comment、operation correlation、append-only 规则、与 Task mutation 的事务关系、与 Internal Domain Event / Outbox 的边界，以及 Agent Task Context 如何消费最近事件。

## 2. TaskEvent 与 Timeline

`TaskEvent` 是一条持久化的 Task 业务历史记录。

`Task Event Timeline` 是多个 TaskEvent 按时间排序后的用户可见 read model。

因此：

```text
TaskEvent = one record
Task Event Timeline = ordered TaskEvent collection / projection
```

两者不是两个独立领域对象。

## 3. Product Semantics

Timeline 参考 GitHub Issue：

- comment 与系统事件出现在同一个时间线；
- 状态变化、assignee、blocker、move 等作为不可改写的历史事实；
- 普通 comment 允许编辑；
- comment 删除使用 soft delete；
- Timeline 不把底层 runtime log 直接混进业务历史。

## 4. TaskEvent Model

概念模型：

```text
TaskEvent
├── id
├── project_id
├── task_id
├── type
├── actor
│   ├── type
│   ├── actor_id?
│   └── source?
├── operation_id?
├── correlation_id?
├── payload
├── created_at
├── edited_at?       # comment only
├── deleted_at?      # comment only
└── deleted_by?      # comment only
```

TaskEvent 按 `created_at + id` 提供稳定顺序。

## 5. Actor

第一阶段 actor type：

```text
human
agent
system
```

### human

保存当前 Project Owner / 用户 identity。

### agent

保存 Agent stable id。

### system

用于 Scheduler、Domain reconciliation 等自动动作。

`source` 可以进一步说明：

```text
scheduler
sprint_lifecycle
task_domain
... 
```

display name 不作为唯一 identity。

## 6. Core Event Types

第一阶段至少包含：

```text
task_created
fields_updated
state_changed
assignee_changed
task_moved
blocker_added
blocker_resolved
comment
```

不为每个普通字段机械定义独立 Event Type；普通业务字段变化统一使用 `fields_updated`，payload 只保存有业务意义的 changed fields。

## 7. task_created

记录 Task 被创建这一事实。

payload 可以保存创建时的关键摘要，例如 initial state / Sprint / type / priority，但不需要复制整个 Task row。

纯草稿 backlog Task 如果满足删除条件，可以连同仅有的 creation history 一起删除；一旦产生其他业务事件就不能再作为可删除草稿。

## 8. fields_updated

只记录有业务意义的普通字段变化，例如：

- title；
- description；
- type；
- priority；
- plan。

不进入 Timeline 的内部字段包括：

- version；
- database timestamps；
- manual_rank rebalance 产生的内部 rank 重写；
- scheduler runtime projection。

payload 推荐：

```text
changed_fields
├── title?
├── description?
├── type?
├── priority?
└── plan?
```

可以保存 old/new 的安全摘要；大文本字段不要求完整复制旧值和新值。

Plan 不建立特殊 `plan_updated` Event，按 `fields_updated` 处理。

## 9. state_changed

payload：

```text
from_state
to_state
reason_code?
```

例如 Scheduler claim、review handoff、blocked recovery、cancel 都使用统一 state_changed 事实。

## 10. assignee_changed

payload：

```text
from_agent_id?
to_agent_id?
```

如果一次 transfer-task 同时 state + assignee 变化，写两条独立 TaskEvent，并共享 operation / correlation。

## 11. task_moved

Task move / Sprint rollover 导致结构位置变化时写：

```text
from_sprint_id
to_sprint_id
from_milestone_id
to_milestone_id
```

手工 move 与 Sprint rollover 都使用同一业务事件语义；actor / source 用来区分来源。

## 12. Blocker Events

### blocker_added

至少引用：

```text
blocker_id
blocker_type
```

可以附安全摘要，不复制整个 blocker。

### blocker_resolved

至少引用：

```text
blocker_id
blocker_type
resolution_comment?
```

actor / created_at 对应谁在何时解除。

## 13. Comment

第一阶段 comment 直接使用：

```text
TaskEvent(type = comment)
```

不建立独立 TaskComment 实体。

payload：

```text
body
```

comment 不参与 Task state machine，但可以与 transfer-task 在同一 operation 中原子创建。

## 14. Comment Edit / Soft Delete

comment 是 TaskEvent append-only 原则的唯一产品级例外。

允许：

- 修改 comment body；
- 设置 `edited_at`；
- soft delete，设置 `deleted_at / deleted_by`。

删除后 Timeline 保留位置与“comment 已删除”语义，不物理删除该 Event。

第一阶段不引入：

- thread；
- reaction；
- nested reply。

如果未来需要这些能力，再评估独立 TaskComment 模型。

## 15. Append-only Domain Facts

以下事件一旦写入不可编辑 / 删除：

```text
task_created
fields_updated
state_changed
assignee_changed
task_moved
blocker_added
blocker_resolved
```

如果业务事实后来改变，追加新的 Event 表达新的事实，而不是修改旧 Event。

## 16. Operation / Correlation

一次领域命令可能产生多条事件。

例如：

```text
transfer-task
├── state_changed
├── assignee_changed
├── blocker_added
└── comment
```

这些 Event 各自独立，但共享：

```text
operation_id
correlation_id
```

`operation_id` 优先对应当前 Domain / Tool logical operation。

`correlation_id` 用于把更大的跨模块流程关联起来；没有更大流程时可以等于 operation_id。

Timeline UI 不需要把同 correlation 的事件强制折叠成一条，但可以做视觉分组。

## 17. Transaction Contract

Task Domain mutation 与其 TaskEvent 必须同事务写入。

例如：

```text
BEGIN
Task.state: in_progress -> in_review
Task.assignee: worker -> reviewer
Task.version += 1
insert state_changed
insert assignee_changed
insert comment
COMMIT
```

不允许：

```text
Task mutation committed
TaskEvent async later
```

否则会产生当前状态已经变化但 Timeline 缺失业务事实的窗口。

## 18. TaskEvent 与 Internal Domain Event / Outbox

两者职责分离。

### TaskEvent

- Task Domain 业务历史；
- 用户可见 Timeline；
- 长期保留；
- 产品查询语义。

### Internal Domain Event / Outbox

- 模块间可靠通知；
- consumer / retry / delivery 语义；
- 不作为用户 Timeline Source of Truth。
- 一旦 Task Domain 产生 Domain Event，该 Event 必须写入统一 PostgreSQL Outbox；
- Event envelope、aggregate ordering、consumer idempotency 与 retry 使用平台统一 contract。

同一数据库事务可以同时写：

```text
Task row
TaskEvent
Outbox event
```

但 TaskEvent 与 Outbox Event 不共用一个 schema，也不互相作为 Source of Truth。

完整 Event contract 见 [Internal Domain Events](../platform-infrastructure/internal-domain-events.md)。

## 19. Agent Execution Boundary

TaskEvent 不复制 Agent Execution lifecycle。

以下内容不写成 TaskEvent：

- Execution started / token streaming；
- Runtime View item；
- Model request / response；
- Tool Call / Tool Result；
- Execution checkpoint；
- stdout / stderr。

Task 页面如果需要展示 Task 历史 Execution，按：

```text
trigger_type = task
trigger_reference = task_id
```

查询 Agent Execution Source of Truth。

可以在 UI 中把 Execution history 与 Task Timeline 邻近展示，但数据模型保持分离。

## 20. Query Contract

Domain 至少提供：

```text
list_task_events(
  task_id,
  before_cursor?,
  after_cursor?,
  limit,
  types?
)
```

要求：

- cursor pagination；
- stable chronological ordering；
- 支持正序 / 倒序读取；
- server-side upper limit；
- comment soft-delete projection。

Agent-facing Builtin Tool 提供 `list-task-events`，用于按需读取更早 Timeline。

## 21. Task Context Projection

Task Trigger Context 默认只注入最近有限数量 TaskEvent。

Projection 至少优先保留：

- 最近 comment；
- 最近 state / assignee change；
- unresolved blocker 相关事实；
- 最近 review comment。

不把完整 Timeline 永久塞入 Model Context。

更早历史：

```text
Agent
-> list-task-events
-> paginated Timeline
```

默认 recent event limit 是 runtime / implementation tuning，不属于 Task Domain 业务语义，因此不在 schema 中固定为某个数字。

## 22. Retention

第一阶段 TaskEvent 不自动过期。

因为 Timeline 是长期项目工作历史，而不是短期 runtime log。

comment soft delete 也保留 Event identity。

## 23. Idempotency

同一个 Domain request retry 不得重复追加 TaskEvent。

Event write 与 command idempotency result 在同一事务中完成。Task Domain 必须先解析 idempotency，再对新 operation 做 version / business validation；Timeline 不定义第二套 retry 规则。

## 24. Error / Authorization

Event 查询必须受 Project / Task scope 授权。

comment edit / delete 至少需要：

- 当前 Project 用户权限；
- comment 类型校验；
- comment 尚未被 soft delete（重复 delete 可以幂等返回）。

系统领域事件不可通过普通 comment API 编辑。

## 25. Timeline UI Projection

推荐 Timeline 显示形态：

```text
Agent A changed state from in_progress to in_review
Agent A assigned Agent B
Agent A commented: ...

System added blocker: scheduler launch failed
User resolved blocker: ...

User commented: ... (edited)
```

UI 文案可以本地化，但必须基于结构化 event type / actor / payload，而不是把显示文本直接持久化为唯一事实。
