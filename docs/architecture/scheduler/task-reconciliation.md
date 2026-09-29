# Task Reconciliation 详细设计

> 上层架构：[Scheduler](./README.md)
>
> 相关详细设计：
> - [Scheduler Loop](./scheduler-loop.md)
> - [Scheduler Dispatch](./scheduler-dispatch.md)

## 1. 设计范围

本文定义 Scheduler 每次访问一个 Task 时的状态决策规则。

核心原则：

> Scheduler 不解释 Agent Execution 的业务结果，只读取当前 Task Source of Truth，并判断该 Task 是否需要自动状态修复或新的 Scheduler Agent Execution。

本文覆盖：

- backlog / todo / in-progress / in-review / blocked / done / cancelled；
- blocker 自动解除；
- active Execution 判断；
- relaunch skip；
- todo claim；
- review launch；
- concurrency / pending Dispatch 对单 Task 的影响；
- user cancel 与 blocked 的关系。

## 2. 输入

每次 reconciliation 至少读取：

```text
Task
├── id
├── project_id
├── sprint_id
├── state
├── assignee_id?
├── priority
├── manual_rank
└── blockers

Project
├── current_sprint_id
├── scheduler_enabled
└── scheduler_max_concurrency

Scheduler state
├── pending dispatch?
├── active scheduler execution?
├── latest scheduler execution?
├── assignee agent busy?
└── relaunch skip runtime
```

Scheduler 必须读取最新持久化 Task，而不能只使用 traversal snapshot 中的旧 Task 内容。

## 3. 总体状态决策表

| Task state | Scheduler 行为 |
| --- | --- |
| backlog | 不进入 traversal；不处理 |
| todo | 满足条件且 assignee Agent idle 时 claim 为 in-progress，并创建 work Dispatch；Agent busy 时保持 todo |
| in-progress | 有 active Execution 或 assignee Agent busy 则 skip；否则按 cooldown / concurrency 决定是否创建 work Dispatch |
| in-review | 有 active Execution 或 assignee Agent busy 则 skip；否则按 cooldown / concurrency 决定是否创建 review Dispatch |
| blocked | 检查可自动解除 blocker；全部解除后转 todo |
| done | 不进入 traversal；不处理 |
| cancelled | 不进入 traversal；不处理 |

Scheduler 不存在：

```text
Execution succeeded -> Task state X
Execution failed -> Task state Y
```

这类映射。

## 4. 通用前置校验

Traversal 已经限制 Current Sprint，但 reconciliation 仍应防御性校验：

1. Project scheduler 仍 enabled；
2. Task 仍属于当前 Project；
3. Task 仍属于当前 Current Sprint；
4. Task 当前 state 与 traversal entry expected state 一致；
5. Task 未被删除；
6. 当前没有需要优先恢复的 pending SchedulerDispatch 冲突。

任一条件不满足：

```text
skip
```

本次仍算一次 Task 调度，并消耗 tick。

## 5. backlog

`backlog` 不参与 Scheduler traversal。

Scheduler 不负责：

- 自动分配 assignee；
- 自动决定 ready；
- backlog -> todo。

这些变化由用户 / Agent 通过 Task Domain 完成。

## 6. todo

`todo` 是 Scheduler 唯一正常主动执行 claim 的状态。

### 6.1 Eligible 条件

至少满足：

```text
state = todo
AND assignee exists
AND no unresolved blocker
AND no pending SchedulerDispatch
AND no active Scheduler Execution
AND assignee Agent idle
AND project concurrency has capacity
AND scheduler enabled
AND task belongs to current sprint
```

这里的 Agent idle 检查不是只看当前 Task。只要 assignee 存在任意来源的非终态 Agent Execution：

```text
created
preparing
running
waiting
```

都视为 busy。

如果 busy：

```text
skip
Task remains todo
no new SchedulerDispatch
```

### 6.2 Claim 事务

必须原子完成：

```text
revalidate Task
create work SchedulerDispatch(status=pending)
Task: todo -> in-progress
commit
```

事务内还需要写 Task Domain 自己的：

- state_changed Task Event；
- 其他必要审计字段。

SchedulerDispatch 不是 Task Event。

### 6.3 Claim 后 Launch

事务提交后使用该 Dispatch 调用 Agent Executor。

由于 busy 预检查与真正 Launch 之间仍可能发生竞争，`AgentExecutor.launch()` 是最终裁决点。如果其他 Trigger 抢先占用了 Agent slot，Launch 返回 `AgentBusy`。

`AgentBusy` 不属于 temporary error，也不进入 Launch retry。Scheduler 必须把当前 Dispatch 终止为 `skipped(agent_busy)`；如果这是 `todo` claim，则通过补偿事务把 Task 恢复为 `todo`，不添加 technical blocker。下一轮 traversal 再重新判断。

即使 Launch 暂时失败：

- Task 不回滚到 todo；
- 同一个 pending Dispatch 按 retry policy 继续；
- 最终 Launch 失败才进入 technical blocker + blocked。

这样避免 Task 在 Launch retry 期间被其他 traversal 再次 claim。

## 7. in-progress

`in-progress` 表示 Task 当前处于普通工作阶段。

Scheduler 不判断上一轮 Agent 工作得是否正确。

### 7.1 有 active Execution

如果存在由 Scheduler 为该 Task 创建、且非终态的 Execution：

```text
created
preparing
running
waiting
```

则：

```text
skip
```

即使 Execution = waiting，也不能创建第二个 Execution。

### 7.2 有 pending Dispatch

如果存在 pending work Dispatch：

优先进入 Dispatch recovery。

不能创建新的 Dispatch。

### 7.3 没有 active Execution

如果当前 phase 从未存在过 Scheduler work Execution，则不需要 cooldown，直接继续检查 Project concurrency。

如果最新 work Execution 已 terminal，但 Task 仍为 in-progress：

Scheduler 不看 terminal status。

统一进入 relaunch cooldown 判断。

### 7.4 Relaunch cooldown

如果当前 Execution 对应 cooldown 尚未完成：

```text
decrement skip remaining
skip
```

如果已经完成：

继续检查 Project concurrency。

随后还必须检查当前 assignee Agent 是否 busy。Agent busy 时本次直接 skip，Task 保持 `in-progress`，不创建新的 Dispatch。

### 7.5 Concurrency 有容量

创建新的：

```text
purpose = work
SchedulerDispatch
```

Task state 保持：

```text
in-progress
```

然后 Launch 当前 assignee。

### 7.6 Assignee 缺失

按 Task Domain 约束，in-progress 应有 assignee。

如果出现：

```text
in-progress + assignee = null
```

属于状态不一致。

Scheduler 不自行选择 Agent。

建议：

```text
add technical / state_inconsistency blocker
Task -> blocked
```

该动作属于保护性 reconciliation，不代表 Scheduler 在正常流程中负责分配 assignee。

## 8. in-review

`in-review` 与 in-progress 使用相同的 Execution presence / cooldown / concurrency 逻辑，但 purpose 为：

```text
review
```

### 8.1 有 active Task Execution

只要该 Task 仍存在任何 Scheduler 创建的非终态 Execution，无论上一轮 purpose 是 work 还是 review：

```text
skip
```

### 8.2 有 pending review Dispatch

优先恢复现有 Dispatch。

### 8.3 没有 active Task Execution

如果当前 phase 从未存在过 Scheduler review Execution，则不需要 cooldown，直接继续检查 Project concurrency。

如果最新 review Execution 已 terminal：

- 不解释 succeeded / failed / cancelled；
- 使用 review phase relaunch cooldown；
- cooldown 完成后创建新的 review Dispatch。

Task state 始终保持：

```text
in-review
```

真正创建 review Dispatch 前同样检查当前 assignee Agent 是否 busy。busy 时本次 skip，Task 保持 `in-review`。

### 8.4 Reviewer assignee

当前 Task.assignee 就是当前 reviewer。

Scheduler 不通过角色、能力或 Agent 名称自行推断 reviewer。

如果 assignee 缺失，按 state inconsistency 处理并进入 blocked。

## 9. blocked

`blocked` 是 Scheduler 第二种会主动改变 Task 状态的正常路径。

Scheduler 只自动处理具有明确自动解除规则的 blocker。

第一阶段至少：

```text
type = rely_on
```

### 9.1 rely_on

结构：

```text
Blocker
type = rely_on
related_task_id
resolved_at?
```

如果：

```text
related Task.state = done
```

Scheduler 自动 resolve blocker。

如果：

```text
related Task.state = cancelled
```

不自动 resolve。

其他非 done 状态同样保持 blocker。

### 9.2 其他 blocker

例如：

- waiting_for_human；
- waiting_for_meeting_approval；
- technical；
- user_cancelled_execution；

如果没有显式自动解除规则：

```text
Scheduler does nothing
```

### 9.3 全部 blocker resolved

在同一次 reconciliation 中：

```text
resolve auto-resolvable blockers
        ↓
check all blockers
        ↓
all resolved
        ↓
blocked -> todo
```

Task 进入 todo 后，本次 reconciliation 结束。

因为 blocked group 位于 traversal 最后：

> Scheduler 不会在同一轮 traversal 内立刻再次把它当 todo claim。

它会等到下一轮 traversal 的 todo group。

这样可以避免一次 Task visit 内连续发生：

```text
blocked -> todo -> in-progress -> launch
```

状态跨度过大。

## 10. done

done 不进入 Scheduler traversal。

Scheduler 不对 done Task 创建 Execution，也不自动 reopen。

## 11. cancelled

cancelled 不进入 Scheduler traversal。

Scheduler 不因为依赖它的其他 Task 存在而修改 cancelled Task。

依赖方的 rely_on blocker继续保持 unresolved。

## 12. Active Execution 查询

Scheduler 对“Task 是否已有 active Execution”的判断只针对：

- trigger_type = task；
- trigger_reference = task_id；
- 来源可追溯到 SchedulerDispatch。

Active Execution 判断跨 purpose：只要同一个 Task 仍有任意 Scheduler 创建的非终态 Execution，就不能再 Launch 第二个 Execution。

只有 relaunch cooldown 在查找“上一轮 Execution”时按当前 phase 的 purpose（work / review）区分。

active 状态：

```text
created
preparing
running
waiting
```

建议通过 SchedulerDispatch -> execution_id 关联，而不是模糊查询 Task 全部 Execution。

这样未来 Manual Task Execution 不会意外阻止 Scheduler，除非明确规定它也占用 Scheduler execution slot。

第一阶段 Scheduler concurrency / duplicate prevention 只考虑 Scheduler 自己创建的 Execution。

## 13. Pending Dispatch 优先级

对任意可执行 Task，如果存在：

```text
SchedulerDispatch.status = pending
```

必须优先恢复它。

禁止：

```text
pending dispatch exists
+
create second dispatch
```

因为 pending Launch outcome 可能已经在 Agent Executor 侧成功，只是 Scheduler 尚未拿到响应。

## 14. Project Concurrency

如果 Task 本身需要 Launch，但 Project concurrency 已满：

```text
skip
```

不能：

- 创建 pending Dispatch；
- 修改 Task state；
- 给 Task 增加 blocker。

对于 todo，只有真正拿到 concurrency slot 后才执行：

```text
todo -> in-progress
```

否则 Task 继续保持 todo。

## 15. Relaunch Skip Runtime

对于 in-progress / in-review：

Scheduler 使用 [Scheduler Loop](./scheduler-loop.md) 中定义的 SchedulerTaskRuntime 保存：

- cooldown_execution_id；
- cooldown_purpose；
- relaunch_skip_remaining。

它只是防止 Execution terminal 后立刻连续空转。

它不表示：

- retry count；
- failure count；
- Agent quality score；
- Task progress。

Scheduler 不因为 relaunch 次数很多自动 blocked。

## 16. Agent Execution Terminal Result

下面这些对 Scheduler reconciliation 都等价：

```text
succeeded
failed
cancelled
```

统一含义：

```text
not active anymore
```

Task 的下一步只由 Task 自己当前 state 决定。

示例：

### 16.1 Execution succeeded，但 Task 仍 in-progress

```text
cooldown
-> relaunch work
```

### 16.2 Execution failed，但 Agent 已经把 Task 改成 in-review

```text
Task Source of Truth = in-review
-> review reconciliation
```

### 16.3 Execution cancelled，但用户取消流程已先把 Task blocked

```text
Task Source of Truth = blocked
-> blocker reconciliation
```

## 17. 用户取消 Execution

用户执行 Task Execution cancel 时必须通过业务操作：

```text
transaction:
  add blocker
  Task -> blocked
commit

Agent Executor.cancel(execution_id)
```

建议 blocker type：

```text
user_cancelled_execution
```

至少保存：

- execution_id；
- cancelled_by；
- reason?；
- created_at。

Scheduler 不监听 Agent Executor cancel completion。

如果 cancel 最终失败，Task 仍保持 blocked，避免继续自动调度；用户可以根据实际 Execution 状态处理。

## 18. State Inconsistency

Scheduler 允许对明显违反 Task Domain invariant 的状态做保护性阻塞，例如：

```text
todo / in-progress / in-review
AND assignee missing
```

这类情况建议：

```text
add blocker(type = technical/state_inconsistency)
Task -> blocked
```

Scheduler 不自行修复 assignee。

其他复杂 Task corruption 不应在 Scheduler 中自动猜测修复，应交由 Task Domain / 用户处理。

## 19. 并发安全

每次需要 mutation 时都必须在数据库事务内重新校验关键状态。

例如 todo claim：

```text
WHERE
task.id = ?
AND task.state = todo
AND assignee_id IS NOT NULL
AND current_sprint_id matches
AND no unresolved blockers
AND no conflicting pending dispatch
```

如果更新失败：

```text
reload
-> treat as normal skip / state changed
```

不能依赖 traversal snapshot 的旧状态完成写入。

## 20. Task Event

Task 状态 / blocker 的变化仍通过 Task Domain Service 产生 Task Event，例如：

- state_changed；
- blocker_added；
- blocker_resolved。

Scheduler 不直接拼装 Agent Execution lifecycle event。

Task Event 记录的是业务事实，不是 Scheduler runtime trace。

## 21. 不在本文定义的内容

- Task traversal / tick：[Scheduler Loop](./scheduler-loop.md)
- Reliable Launch：[Scheduler Dispatch](./scheduler-dispatch.md)
- Task Domain：[项目与工作管理](../project-work-management/README.md)
- Agent Execution lifecycle：[Agent Executor](../agent-executor/README.md)
