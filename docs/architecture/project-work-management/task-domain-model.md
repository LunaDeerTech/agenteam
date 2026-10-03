# Task Domain Model 详细设计

> 状态：设计稿
>
> 上层架构：[项目与工作管理架构](./README.md)
>
> 相关设计：
> - [Task State Machine](./task-state-machine.md)
> - [Task Blocker / Dependency](./task-blocker-dependency.md)
> - [Task Event Timeline](./task-event-timeline.md)
> - [Sprint Lifecycle](./sprint-lifecycle.md)
> - [Scheduler](../scheduler/README.md)

## 1. 设计范围

本文定义 Task 及其上层规划结构的长期领域模型，包括 Task canonical model、Milestone / Sprint / Task 层级、type / priority / assignee / plan、乐观并发、manual rank、create / update / move / delete、幂等与查询 contract。

状态转换见 Task State Machine；Blocker / dependency 见 Task Blocker / Dependency；Timeline 见 Task Event Timeline；Sprint start / complete / rollover 见 Sprint Lifecycle。

## 2. 核心原则

1. Task 是最小可调度工作单元，Plan item 不独立调度；
2. Task 永远属于 Sprint，Sprint 永远属于 Milestone；
3. move-task 改变结构位置，不隐式改变 Task state；
4. Task 使用 `version` 显式检测 stale write；
5. `manual_rank` 是正式领域数据，Tasks View 与 Scheduler 使用同一排序 contract；
6. `done / cancelled` 是冻结终态；
7. 进入工作流后的 Task 不允许删除；
8. mutation 具备幂等语义；
9. 第一阶段不引入 date、labels、subtask、通用 TaskRelation。

## 3. Work Hierarchy

```text
Project
└── Milestone
    └── Sprint
        └── Task
```

约束：

- Milestone 属于 Project；
- Sprint 属于一个 Milestone；
- Task 属于一个 Sprint；
- Task 的 Milestone 由 Sprint 归属决定；
- 如果 Task 持久化 `milestone_id`，它必须等于 `Sprint.milestone_id`；
- 不允许脱离 Sprint / Milestone 的孤立 Task。

Task Execution Context 中，当前 Milestone / Sprint 的 `title`、`description` 属于基础业务上下文。

## 4. Milestone

第一阶段模型：

```text
Milestone
├── id
├── project_id
├── title
├── description
├── manual_rank
├── created_at
└── updated_at
```

Milestone 第一阶段不建立 planned / active / completed 或 open / closed lifecycle，也不引入 start date / due date。

Project 内 Milestone 使用 fractional indexing / LexoRank 类字符串 `manual_rank`，支持拖拽排序。

## 5. Sprint 结构字段

Sprint lifecycle 的 Source of Truth 见 Sprint Lifecycle。Work Hierarchy 额外明确：

```text
Sprint
├── id
├── project_id
├── milestone_id
├── title
├── description
├── manual_rank
└── lifecycle fields ...
```

同一 Milestone 内 Sprint 使用 `manual_rank` 做人工规划排序。Lifecycle state 与 manual rank 是两个独立维度。

## 6. Task Canonical Model

```text
Task
├── id
├── project_id
├── milestone_id
├── sprint_id
├── title
├── description
├── type
├── priority
├── state
├── assignee_agent_id?
├── plan
├── manual_rank
├── version
├── created_at
└── updated_at
```

关联对象：

```text
Task
├── TaskBlocker[]
├── TaskEvent[]
├── AgentExecution[]        # 通过 trigger reference 查询
└── SchedulerDispatch[]     # Scheduler-owned
```

Task 主记录不保存 execution id list、runtime logs、Tool Call history、scheduler runtime、comment JSON 或 dependency JSON。

## 7. Type 与 Priority

Task type 第一阶段固定：

```text
feature
bug
task
spike
chore
```

Task priority 第一阶段固定：

```text
low
medium
high
critical
```

Scheduler 排序语义为：

```text
critical > high > medium > low
```

Project 不自定义 type / priority。

## 8. Assignee

`assignee_agent_id` 只能引用当前 Project 内的 Agent。用户不是 Task assignee；需要用户介入时使用 Blocker、Human Inbox、Meeting、Approval / Decision 等机制。

- `backlog`：assignee 可空，也可以预先指定；
- `todo / in_progress / in_review`：必须有合法 assignee；
- `blocked`：保留当前值，不强制非空；
- `done / cancelled`：不要求非空，默认保留进入终态前的历史值。

Scheduler 不自行选择或恢复 assignee。

## 9. Plan

Task Plan 使用 Markdown / plain text 单字段：

```text
plan: text
```

Plan 用于 Agent 和用户维护执行计划，不建立 PlanItem、checklist 状态机或独立 Scheduler 单元。需要独立调度的计划项应创建新的 Task。

Plan 更新按普通业务字段更新处理，不建立独立 `plan_updated` 领域模型。

## 10. Version 与并发

Task 使用整数 `version` 作为 optimistic concurrency token。

改变 Task aggregate 业务状态的命令至少携带：

```text
task_id
expected_version
```

数据库 mutation 必须验证：

```text
WHERE id = :task_id
AND version = :expected_version
```

业务 mutation 成功后 `version = version + 1`。不匹配返回 `TASK_VERSION_CONFLICT`，调用方重新读取后再决定是否重试；仅维护 rank 空间且相对顺序不变的内部重整按 §11 不推进业务 version。

数据库事务 / 行锁负责事务内部一致性；`version` 负责跨请求 stale write 检测。

至少以下操作推进 Task version：update-task、update-task-plan、move-task、transfer-task、add / resolve blocker、Scheduler claim / reconciliation mutation。纯追加 comment 不改变 Task 主记录时不必为了 comment 推进 Task version。

Scheduler 内部 mutation 不能使用 traversal snapshot 中的旧 version。它必须在自己的事务中重新读取 / lock 最新 Task，并把当前 version 作为本次 Domain mutation 的 expected version，再原子推进 version。

## 11. Manual Rank

Task `manual_rank` 使用 fractional indexing / LexoRank 类字符串 rank。

正式排序 scope：

```text
sprint_id + state + priority
```

同一 scope 内按 `manual_rank ASC`。Scheduler 与 Tasks View 使用同一 ordering contract。

规则：

- 拖拽优先生成前后 rank 之间的新 rank；
- rank 空间不足时只 rebalance 当前 group；
- 仅维持 rank 空间、保持实际相对顺序的 rebalance 不推进 Task 业务 version，不产生用户可见 TaskEvent；
- 普通 state / priority 改变后自动追加到目标 group 末尾；
- Scheduler 的 `scheduler_agent_busy_compensation` 例外：恢复 claim 前的 todo 逻辑位置；若发生 rank 重整，不能直接写回已陈旧的 rank 字符串；
- move 到其他 Sprint 后自动追加到目标 group 末尾；
- rollover 多个 Task 时保持源 group 内相对顺序。

实际拖拽、移动、字段或状态修改仍校验并推进 version，不能伪装成内部维护。rebalance 只更新当前 group 的 rank，普通字段更新不得用旧 rank 覆盖重整结果；不推进业务 version 不代表可以省略并发保护。

D01/D11 明确重整与插入/拖拽/跨组移动的 SQL、锁、唯一性和稳定排序，以及 cursor/排序视图与业务更新时间的维护边界。D11/D23 必须协调 pending claim / busy compensation，通过与维护互斥或正式位置映射恢复原位置，避免重整后写回 `claim_source_manual_rank` 改变逻辑顺序或破坏唯一性；具体机制和真实并发验收在责任规格完成。

## 12. Create Task

概念命令：

```text
create_task(
  project_id,
  sprint_id,
  title,
  description?,
  type?,
  priority?,
  assignee_agent_id?,
  plan?,
  initial_state?,
  request_id,
  actor
)
```

默认 state = `backlog`。默认创建到目标 `sprint + state + priority` group 末尾。

创建时必须验证目标 Sprint 属于当前 Project 且不是 completed，并从 Sprint 推导 `milestone_id`。

如果显式创建为 `todo`，必须同时具有合法 assignee 且不存在 unresolved blocker。

## 13. Update Task

`update-task` 修改普通业务字段，例如 title、description、type、priority，以及在不破坏 state invariant 前提下的 assignee。

运行中的 Task 也允许显式修改 assignee。已经创建的 SchedulerDispatch / AgentExecution 仍固定使用创建时的 Agent identity；assignee 变化只影响之后的新 reconciliation / Dispatch，不会迁移或取消当前 Execution。

它不直接修改：

- state；
- sprint / milestone membership；
- blocker；
- terminal Task 冻结字段。

状态变化统一走 Task State Machine；结构移动走 `move-task`；Plan 可以保留独立 `update-task-plan` Tool，但 Domain 仍把它视为普通业务字段。

## 14. Move Task

概念命令：

```text
move_task(task_id, target_sprint_id, expected_version, request_id, actor)
```

约束：

- Task 与目标 Sprint 必须同 Project；
- 目标 Sprint 不能 completed；
- 来源 Sprint 如果 completed，禁止移出；
- Current Sprint Task 存在 active Task Execution 或 pending SchedulerDispatch 时禁止移出；
- `done / cancelled` 受终态冻结规则限制，不允许通过普通 move 改 membership。

move 不改变 state、assignee、plan、blockers。目标 Milestone 由目标 Sprint 推导，manual_rank 追加到目标 group 尾部。

move 产生独立 `task_moved` TaskEvent，记录 from / to Sprint 以及由此变化的 Milestone。

## 15. Delete Task

进入工作流后的 Task 不允许删除。第一阶段只允许删除纯 backlog 草稿 Task。

必须同时满足：

```text
state = backlog
AND no AgentExecution
AND no SchedulerDispatch
AND no unresolved blocker
AND no resolved blocker
AND no incoming/outgoing dependency reference
AND no business TaskEvent except creation record
```

否则返回 `TASK_DELETE_NOT_ALLOWED`。

第一阶段不为所有 Task 引入 generic soft-delete。正常结束工作使用 `cancelled`。

## 16. Terminal Immutability

`done / cancelled` 进入终态后禁止修改 title、description、type、priority、plan、assignee、Sprint / Milestone membership 与 blocker 业务结构。

仍允许追加 comment，以及按权限编辑 / 软删除 comment。

终态不支持 reopen。需要继续工作时创建新的 Task，并在 description / comment 中引用旧 Task。

## 17. Command Idempotency

Task Domain mutation 统一具有业务幂等身份。下文既有领域示例中的 `request_id` 表示业务 `idempotency_key`，来源可以是 Agent Tool Operation ID 或稳定业务 command key；每次 HTTP 传输的追踪 `request_id` 独立生成，不可替代它。正式字段映射由 D01/D11 固定，遵循[基础契约](../platform-infrastructure/foundation-contracts.md#5-请求追踪与业务幂等)。

### 17.1 Idempotency 必须先于 version 校验

同一个 Tool Operation technical retry 可能发生：

```text
Attempt 1
  expected_version = 5
  -> mutation committed
  -> Task.version = 6
  -> response lost

Attempt 2
  same request_id
  same expected_version = 5
```

因此 Domain 处理顺序必须是：

```text
1. lookup request_id / idempotency record
2. existing + same fingerprint
   -> replay original result
3. existing + different fingerprint
   -> IDEMPOTENCY_KEY_REUSED
4. no existing result
   -> validate expected_version
5. execute mutation
6. write TaskEvent / Outbox / idempotency result in same transaction
```

不能先检查 expected_version 再做 idempotency lookup，否则一次已经成功但响应丢失的 retry 会被错误返回 `TASK_VERSION_CONFLICT`。

推荐唯一 scope：

```text
(project_id, command_type, request_id)
```

同一 key + 相同 request fingerprint：返回第一次结果，不重复副作用。

同一 key + 不同 payload：返回 `IDEMPOTENCY_KEY_REUSED`。

`create-task` 支持 `client_request_id / idempotency_key`；同一 Project + key 重试返回原 Task，不能按 title 去重。

## 18. Transaction Boundary

一次 Task mutation 需要共同成立的事实必须同事务提交：

```text
Task mutation
Task.version + 1
TaskEvent(s)
DomainEventOutbox row(s), if this mutation emits Domain Event
idempotency result
```

是否需要表达某个跨模块业务事实由 Task Domain 决定；但一旦 emit Domain Event，就必须按平台统一 Transactional Outbox contract 写入 Outbox，不存在“产生 Domain Event 但不持久化 Outbox”的分支。

统一 Event contract 见 [Internal Domain Events](../platform-infrastructure/internal-domain-events.md)。

状态转换的更严格 transaction contract 见 Task State Machine。

## 19. Query Contract

Work Management Domain 提供精确读取：

```text
read_task(task_id)
```

`read_task` 返回当前 Task canonical read model，必须包含 `version`，供 Agent / UI 在 mutation 前取得可靠的 `expected_version`。

`list_tasks` 返回的每个 Task projection 也必须包含 `version`；当调用方遇到 `TASK_VERSION_CONFLICT` 时，优先通过 `read_task(task_id)` 重新取得最新状态。

Work Management Domain 至少支持以下 Task filter：

- state；
- priority；
- type；
- assignee_agent_id；
- milestone_id；
- sprint_id；
- text。

text search 第一阶段至少覆盖 title、description、plan。

查询支持 pagination 与 stable sort。Explore / Kanban 的前端筛选器布局不属于 Domain contract。

## 20. Task Context Boundary

Task Trigger Context 默认可以包含：

- 当前 Task canonical fields；
- 当前 Milestone / Sprint title 与 description；
- Plan；
- unresolved blockers；
- 最近有限数量 TaskEvents。

更早 Timeline 历史由 Tool 按需查询。

Task Context 不直接复制 Agent Execution Runtime View、Execution transcript 或 Tool Call log；需要历史 Execution 时按 Agent Execution Source of Truth 查询。

## 21. 第一阶段明确不引入

- Project / Milestone / Sprint / Task start date / due date；
- Task labels / tags；
- parent / subtask 层级；
- related / duplicate 等通用 TaskRelation。

真正影响执行顺序的 Task 关系使用 `rely_on` Blocker。

## 22. Error Contract

至少区分：

```text
TASK_NOT_FOUND
TASK_VERSION_CONFLICT
TASK_STATE_INVALID
TASK_ASSIGNEE_REQUIRED
TASK_ASSIGNEE_INVALID
TASK_SPRINT_INVALID
TASK_MOVE_NOT_ALLOWED
TASK_TERMINAL_IMMUTABLE
TASK_DELETE_NOT_ALLOWED
TASK_HAS_ACTIVE_EXECUTION
TASK_HAS_PENDING_DISPATCH
IDEMPOTENCY_KEY_REUSED
```

Tool / API 层可以转换 display message，但必须保留 machine-readable code。

## 23. 模块边界

- Scheduler 使用 Task state / priority / manual_rank / assignee，但不拥有 Task Domain；
- Agent Executor 通过 TaskContextProvider 读取 Task Context，不复制 Task Source of Truth；
- Builtin Task Tools 是 Domain command 的 Agent-facing adapter，不重新实现状态机；
- Sprint Lifecycle 可以批量 move unfinished Task，但仍遵守 membership、rank、TaskEvent 与 transaction contract。
