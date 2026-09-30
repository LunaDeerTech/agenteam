# Task Blocker / Dependency 详细设计

> 状态：设计稿
>
> 上层架构：[项目与工作管理架构](./README.md)
>
> 相关设计：
> - [Task Domain Model](./task-domain-model.md)
> - [Task State Machine](./task-state-machine.md)
> - [Task Event Timeline](./task-event-timeline.md)
> - [Scheduler Task Reconciliation](../scheduler/task-reconciliation.md)

## 1. 设计范围

本文定义 Task Blocker 及 Task dependency 的统一模型，包括 Blocker schema、类型、immutable semantics、resolution、`rely_on` 图约束、自动解除、前置 Task cancelled、Scheduler reconciliation 与 Timeline 事实。

Task Dependency 不再建立独立领域实体；执行依赖统一表示为 `type = rely_on` 的 TaskBlocker。

## 2. 核心原则

1. Blocker 是正式领域对象，不是 Task 上的字符串；
2. Task dependency 是 Blocker 的一个结构化类型；
3. Blocker 创建后核心语义不可改写；
4. resolve 保留历史，不删除 blocker；
5. `blocked` state 与 unresolved blocker 建立强不变量；
6. 自动解除只发生在有明确 deterministic rule 的 blocker；
7. related Task cancelled 不等于 dependency satisfied；
8. Blocker mutation 与 TaskEvent 同事务写入。

## 3. Domain Model

概念模型：

```text
TaskBlocker
├── id
├── project_id
├── task_id
├── type
├── description
├── metadata
├── created_at
├── created_by
├── resolved_at?
├── resolved_by?
└── resolution_comment?
```

`created_by / resolved_by` 使用与 TaskEvent actor 一致的结构化 actor identity：human / agent / system + actor id / source。

TaskBlocker 不单独保存 Task state；Task.state 仍由 Task State Machine 持有。

## 4. Blocker Types

第一阶段固定核心枚举：

```text
rely_on
waiting_for_human
waiting_for_meeting_approval
technical
user_cancelled_execution
```

其中前四类来自 Work Management 核心设计；`user_cancelled_execution` 是 Scheduler 已有 Task Execution cancel flow 依赖的正式类型。

不使用自由字符串 type。

各类型允许携带结构化 `metadata`，但 metadata schema 由 blocker type 定义，不是任意 JSON 业务逻辑。

## 5. Type-specific Metadata

### 5.1 rely_on

```text
metadata
└── related_task_id
```

### 5.2 waiting_for_human

可以关联真正的底层业务对象，例如 DecisionRequest、ApprovalRequest、Meeting 或其他稳定业务引用：

```text
metadata
├── reference_type?
└── reference_id?
```

Human Inbox 去重边界：

- 如果 `waiting_for_human` 只是引用一个已经拥有自己 Human Inbox projection 的 actionable object，例如 ApprovalRequest、DecisionRequest、proposed Meeting，则 Blocker 本身不再创建第二条 HumanInboxItem；
- 这类 Blocker 只作为 Task 业务上下文和阻塞原因，Human Inbox 继续以原 actionable object 作为 `source_type / source_id`；
- 只有 Blocker 本身就是独立的人类待处理对象、且没有另一个 canonical actionable object 承载该待办时，才允许以 `source_type = task_blocker` 建立独立 HumanInboxItem。

这样避免同一个 Approval / Decision / Meeting 同时以“源对象 + Blocker”重复出现在 Human Inbox。

完整 projection 规则见 [Human Inbox](../platform-infrastructure/human-inbox.md)。

### 5.3 waiting_for_meeting_approval

```text
metadata
└── meeting_id / approval_reference
```

具体引用形状应复用 Meeting / Approval 已有 stable identity。

### 5.4 technical

technical 使用结构化 code 描述技术阻塞原因：

```text
metadata
├── code
├── source?
├── reference_id?
└── safe_summary?
```

例如：

```text
code = state_inconsistency
code = scheduler_launch_failed
```

不要把 `technical/state_inconsistency` 当成新的 blocker type。

### 5.5 user_cancelled_execution

```text
metadata
├── execution_id
├── cancelled_by
└── reason?
```

## 6. Immutable Semantics

Blocker 创建后以下核心字段不可直接修改：

- type；
- related object identity；
- description 的业务含义；
- created_by；
- created_at。

如果创建错误：

> resolve 原 blocker，再创建新的 blocker。

这样 Timeline 能保留真实历史，而不是事后改写过去发生过的阻塞。

## 7. Resolution

resolve 不删除 blocker。

至少写入：

```text
resolved_at
resolved_by
resolution_comment?
```

并产生 `blocker_resolved` TaskEvent。

Timeline 必须能展示：

- 哪个 blocker 被解除；
- 何时解除；
- 由谁解除；
- resolution comment（如果存在）。

对同一个已经 resolved blocker 再次 resolve 应返回幂等结果或 `BLOCKER_ALREADY_RESOLVED`，不能重复产生业务事件。

## 8. Task blocked Invariant

事务提交后的强不变量：

```text
Task.state = blocked
=>
exists unresolved TaskBlocker(task_id = Task.id)
```

反方向不成立：backlog 等非执行状态可以预先存在 blocker；存在 blocker 不意味着必须立刻把 Task state 改成 blocked。

目标 `todo` 必须不存在 unresolved blocker。

## 9. Add Blocker

概念命令：

```text
add_task_blocker(
  task_id,
  type,
  description,
  metadata,
  expected_version,
  request_id,
  actor
)
```

要求：

- Task 属于当前 Project；
- type 是固定枚举；
- type-specific metadata 合法；
- terminal Task 不允许新增 blocker；
- request id 幂等。

如果 blocker 是一次 `transfer-task(... -> blocked)` 的一部分，应与状态变化同事务提交。

## 10. Resolve Blocker

概念命令：

```text
resolve_task_blocker(
  blocker_id,
  expected_task_version,
  resolution_comment?,
  request_id,
  actor
)
```

如果 Task 当前不是 blocked，可以正常 resolve blocker，并按 Task 当前 state invariant 校验。

如果 Task 当前 blocked 且这是最后一个 unresolved blocker，独立 resolve 不允许单独提交：

```text
LAST_BLOCKER_REQUIRES_TASK_TRANSITION
```

此时 Human / Agent 应通过 `transfer-task(target_state = todo, resolve_blocker_ids = [...])` 在同一事务中解除最后 blocker 与恢复 Task。

Scheduler 自动 dependency recovery 也必须在同一 reconciliation 事务中 resolve + `blocked -> todo`。

## 11. rely_on Dependency Model

依赖边：

```text
Task A
  blocker(type = rely_on, metadata.related_task_id = Task B)

=>
A depends on B
```

只使用 unresolved `rely_on` blocker 作为当前有效依赖边。

resolved blocker 保留在历史中，但不再参与 dependency graph。

## 12. rely_on Validation

创建 rely_on 时必须验证：

1. related Task 存在；
2. related Task 与当前 Task 属于同 Project；
3. Task 不能依赖自身；
4. 新增边不会形成循环依赖；
5. terminal Task 不能新增依赖。

失败返回结构化 error。

## 13. Cycle Detection

使用当前 Project 内所有 unresolved `rely_on` edge 构造有向图。

新增：

```text
A -> B
```

前检查是否已经存在从 B 可达 A 的路径。

如果可达：

```text
TASK_DEPENDENCY_CYCLE
```

拒绝创建。

第一阶段不对 resolved 历史 dependency 参与 cycle detection。

## 14. Dependency Satisfied

`rely_on` 的唯一自动满足条件：

```text
related Task.state = done
```

Scheduler 访问 blocked Task 时可以自动 resolve 该 blocker。

以下状态都不自动满足：

```text
backlog
todo
in_progress
in_review
blocked
cancelled
```

尤其：

> cancelled 不等于 done。

## 15. Related Task Cancelled

如果 related Task 进入 cancelled：

- 依赖方 `rely_on` blocker 保持 unresolved；
- 不自动取消依赖 Task；
- 不自动把 blocker 改成 waiting_for_human；
- 不自动换依赖目标；
- 不因为“依赖目标被取消”这一事实单独向依赖 Task 写 TaskEvent。

后续由 Human / Agent 显式决定：

- resolve 当前 blocker；
- 建立新的 rely_on blocker；
- 取消当前 Task；
- 采取其他业务动作。

## 16. Scheduler Automatic Resolution

Scheduler 只自动处理有明确 deterministic rule 的 blocker。

第一阶段：

```text
rely_on -> related Task done
```

同一次 reconciliation：

```text
load Task + unresolved blockers
resolve satisfied rely_on blocker(s)
check remaining blockers

if remaining blockers exist:
    keep blocked
else if current assignee valid:
    Task blocked -> todo
else:
    add technical blocker(code = state_inconsistency)
    keep blocked

write TaskEvent(blocker_resolved / state_changed / blocker_added)
Task.version += 1
commit
```

Scheduler 不自动处理 waiting_for_human、waiting_for_meeting_approval、technical、user_cancelled_execution，除非未来为某个类型明确增加 deterministic resolve rule。

## 17. Assignee Boundary

Blocker 不保存 `resume_assignee_id`。

进入 / 解除 blocked 都不自动恢复历史 assignee。

- Human / Agent 显式恢复时可以在 transfer-task 中顺便修改 assignee；
- Scheduler 自动恢复时只使用 Task 当前 assignee，不选择或改写。

## 18. User-cancelled Execution

Task Execution 被用户取消时，业务层可以先原子：

```text
add blocker(type = user_cancelled_execution)
Task -> blocked
```

再调用 Agent Executor cancel。

这样即使 cancel RPC 结果未知，Task 也不会继续被 Scheduler 自动执行。

Blocker 保存 execution_id / cancelled_by / reason 等业务引用，不复制 Execution Runtime View。

## 19. Technical Blocker

technical blocker 用于保护性停止自动执行，例如：

- Scheduler launch retry 最终失败；
- assignee / state inconsistency；
- 其他必须人工处理的执行基础设施问题。

完整 internal stack 不进入 blocker description / metadata。只保存可安全展示的 summary 与 stable reference。

## 20. Timeline

Blocker 生命周期至少对应：

```text
blocker_added
blocker_resolved
```

Event 保存 Blocker identity，但不复制完整 blocker snapshot。

Task Event Timeline 详细模型见对应文档。

## 21. Query Contract

至少支持：

```text
list_task_blockers(task_id, status?, type?)
```

status：

```text
unresolved
resolved
all
```

默认 UI 可以优先展示 unresolved，再在 Timeline 中查看历史 resolved blocker。

## 22. Idempotency 与 Concurrency

add / resolve blocker：

- 先按 request_id 解析 idempotency；
- 已完成的相同 fingerprint 直接 replay 原结果；
- 新 operation 才校验 Task expected_version；
- Task row / relevant blocker row 在事务内锁定；
- mutation 成功后推进 Task.version。

同一 request 重试不得重复创建 blocker 或重复 resolution Event。

## 23. Error Contract

至少区分：

```text
BLOCKER_NOT_FOUND
BLOCKER_TYPE_INVALID
BLOCKER_METADATA_INVALID
BLOCKER_ALREADY_RESOLVED
LAST_BLOCKER_REQUIRES_TASK_TRANSITION
TASK_DEPENDENCY_TARGET_NOT_FOUND
TASK_DEPENDENCY_CROSS_PROJECT
TASK_DEPENDENCY_SELF_REFERENCE
TASK_DEPENDENCY_CYCLE
TASK_TERMINAL_IMMUTABLE
TASK_VERSION_CONFLICT
```

## 24. Source of Truth

TaskBlocker 是“为什么不能继续”的 Source of Truth。

Task state 仍由 Task State Machine 持有；Scheduler 只消费 Blocker rule 并执行明确的 automatic reconciliation，不拥有 Blocker Domain。
