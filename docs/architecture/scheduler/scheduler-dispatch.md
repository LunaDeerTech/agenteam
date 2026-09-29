# Scheduler Dispatch 详细设计

> 上层架构：[Scheduler](./README.md)
>
> 相关详细设计：
> - [Scheduler Loop](./scheduler-loop.md)
> - [Task Reconciliation](./task-reconciliation.md)
> - [Agent Execution Domain Model](../agent-executor/execution-domain-model.md)

## 1. 设计范围

本文定义 Scheduler 到 Agent Executor 之间的可靠 Launch 边界。

主要覆盖：

- SchedulerDispatch 领域模型；
- Dispatch identity；
- Agent Executor idempotency key；
- pending / launched / failed / skipped；
- todo claim 事务；
- in_progress / in_review relaunch；
- Launch retry / backoff；
- unknown outcome；
- Central restart recovery；
- Project concurrency slot；
- retention。

本文不追踪 Agent Execution 自身 succeeded / failed / cancelled 生命周期。

## 2. SchedulerDispatch 定位

SchedulerDispatch 表示：

> Scheduler 决定为某个 Task 当前阶段创建一次新的 Agent Execution，并可靠完成该 Launch 的过程。

核心边界：

```text
Task reconciliation
       ↓
SchedulerDispatch
       ↓
Agent Executor.launch()
       ↓
execution_id
```

Dispatch 结束后，Agent Execution 由 Agent Executor 自己管理。

## 3. Domain Model

概念结构：

```text
SchedulerDispatch
├── id
├── project_id
├── sprint_id
├── task_id
├── purpose
├── agent_id
├── claim_source_manual_rank?
├── status
├── idempotency_key
├── execution_id?
├── attempt_count
├── next_retry_at?
├── last_error?
├── skip_reason?
├── version
├── created_at
├── launched_at?
├── failed_at?
└── skipped_at?
```

### 3.1 purpose

第一阶段：

```text
work
review
```

对应：

- work：todo / in_progress；
- review：in_review。

### 3.2 agent_id

在创建 Dispatch 时固定当前 Task.assignee。

Dispatch retry 不重新解析 Task 当前 assignee。

原因：

> 同一个 Dispatch 表示同一次 Launch 意图。

如果业务后来修改了 assignee，应让当前 Dispatch先完成 / 失败，后续新的 reconciliation 创建新的 Dispatch。

### 3.3 sprint_id

保存 Dispatch 创建时的 Current Sprint。

用于：

- 审计；
- 排错；
- Project concurrency 查询；
- 历史解释。

Sprint 后续切换不会重写历史 Dispatch。

### 3.4 claim_source_manual_rank

仅 `todo` claim 创建的 Dispatch 保存该字段，用于记录 claim 前 Task 在 todo group 中的 `manual_rank`。

它只服务于极少数 `AgentBusy` 竞态补偿：如果 Launch 最终明确没有创建 Execution，Scheduler 将 Task 从 `in_progress` 恢复到 `todo` 时恢复该 rank，避免技术竞态改变用户排序。

普通 `in_progress / in_review` relaunch 不设置该字段。

## 4. Status

第一阶段：

```text
pending
   ├──> launched
   ├──> failed
   └──> skipped
```

### 4.1 pending

表示：

- Dispatch 已可靠持久化；
- Agent Launch Request 可能尚未发送；
- 也可能已经发送但 response outcome unknown；
- 尚未可靠获得 execution_id。

### 4.2 launched

表示：

- Agent Executor 已返回稳定 execution_id；
- Dispatch Launch 职责完成。

它不表示：

- Agent Execution 已进入 running；
- Agent 工作成功；
- Task 业务目标完成。

### 4.3 failed

表示：

- Scheduler 已经过有限 Launch retry；
- 仍无法可靠完成该 Launch；
- Scheduler 放弃本次 Dispatch。

failed 后不再复用该 Dispatch。

如果 Task 后续重新可执行，需要新的 Dispatch。

### 4.4 skipped

`skipped` 表示 Dispatch 已经建立，但在真正 Launch 时发现目标 Agent slot 被其他 Trigger 抢先占用。

第一阶段：

```text
skip_reason = agent_busy
```

该状态只用于 busy 预检查与实际 `AgentExecutor.launch()` 之间的竞态兜底。

`skipped`：

- 不创建 Agent Execution；
- 不进入 Launch retry；
- 不添加 technical blocker；
- 不消耗 relaunch cooldown；
- 后续 Scheduler traversal 可以在 Agent 空闲后创建新的 Dispatch。

## 5. Dispatch Identity

每次新的 Launch 意图创建：

```text
dispatch_id = new stable id
```

不能用：

```text
task_id + purpose
```

作为永久 Dispatch identity，因为同一 Task 可以多次 work / review。

## 6. Agent Executor Idempotency

每个 Dispatch 派生稳定：

```text
idempotency_scope = scheduler
idempotency_key = scheduler_dispatch:{dispatch_id}
```

同一个 Dispatch 所有 retry：

```text
same idempotency_scope
same idempotency_key
```

因此：

```text
launch request sent
response lost
retry same dispatch
        ↓
Agent Executor returns same execution_id
```

Scheduler 不需要自己实现“先查再创建”的分布式协议。

## 7. Agent Launch Request

Task Scheduler 至少传：

```text
agent_id = dispatch.agent_id

trigger:
  type = task
  reference = task_id

purpose:
  work | review

idempotency_key:
  scheduler_dispatch:{dispatch_id}

metadata:
  scheduler_dispatch_id
  project_id
  sprint_id
```

Scheduler 不复制完整 Task / Sprint / Milestone Context。

Agent Executor 通过 TaskContextProvider 构建 Trigger Context。

## 8. Work Claim 事务

对于 todo：

```text
BEGIN

lock Project scheduling row
reload Task
validate:
  state = todo
  assignee exists
  no unresolved blockers
  belongs to current sprint
  no pending dispatch
  no active scheduler execution
  assignee Agent idle
  project concurrency has capacity

create SchedulerDispatch:
  purpose = work
  status = pending
  agent_id = current assignee
  claim_source_manual_rank = Task.manual_rank

Task:
  todo -> in_progress
  manual_rank -> target in_progress group tail
  version += 1

write Task Domain state_changed event

COMMIT
```

这两个核心事实必须原子：

```text
Task is in_progress
+
there is a pending work Dispatch
```

在进入该事务前，Scheduler 先通过 Agent Executor active slot 视图检查当前 assignee 是否 busy。busy 时本次 Task visit 直接结束：Task 保持 `todo`，不创建 Dispatch。

因此正常路径不会出现“先 claim 再发现 Agent 明显 busy”。

但该检查不能替代 `AgentExecutor.launch()` 自身的原子 slot 约束：Meeting 或其他 Trigger 仍可能在 precheck 与 Launch 之间抢先占用 slot，所以保留后面的 `AgentBusy` 竞态补偿。

## 9. in_progress / in_review Relaunch

对于已经处于执行 phase 的 Task：

- 不修改 Task state；
- 只创建新的 Dispatch。

前置：

```text
no active scheduler execution
no pending dispatch
relaunch skip exhausted
project concurrency has capacity
assignee exists
assignee Agent idle
```

如果 assignee busy，本次 relaunch 直接 skip，Task 保持原 `in_progress / in_review` 状态，也不创建 Dispatch。

事务：

```text
BEGIN
lock Project scheduling row
revalidate Task
revalidate current_sprint_id
revalidate no pending Dispatch
revalidate no active Execution
create SchedulerDispatch(status=pending)
COMMIT
```

purpose：

```text
in_progress -> work
in_review   -> review
```

## 10. 防重复约束

虽然一个 Project 只有一条 Scheduler traversal，数据库仍应防御性保证不会产生并发重复 Dispatch。

建议至少有 partial unique constraint：

```text
UNIQUE(task_id)
WHERE status = pending
```

如果未来允许一个 Task 同时存在多个不同来源 Dispatch，再调整 scope。

第一阶段：

> 同一个 Task 同一时间最多一个 pending SchedulerDispatch。

active Execution 的防重复需要通过事务中的 existence check + Scheduler 单线程语义共同保证。

## 11. Launch 流程

概念：

```text
pending Dispatch
      ↓
build AgentLaunchRequest
      ↓
AgentExecutor.launch()
      ├── execution_id
      │       ↓
      │   persist:
      │     status = launched
      │     execution_id = ...
      │     launched_at = ...
      │
      └── AgentBusy
              ↓
          persist:
            status = skipped
            skip_reason = agent_busy
            skipped_at = ...
```

Launch 是异步 Execution creation contract。

Scheduler 只需要可靠拿到 execution_id，不等待 Execution 进入 preparing / running。

`AgentBusy` 表示没有创建新的 Agent Execution，因此不属于 unknown outcome，也不需要用同一个 Dispatch 做 Launch retry。

如果该 Dispatch 来自原 `todo` claim，Scheduler 执行内部补偿事务：

```text
lock Task + Dispatch
Dispatch: pending -> skipped(agent_busy)
Task: in_progress -> todo
Task.manual_rank = Dispatch.claim_source_manual_rank
Task.version += 1
TaskEvent(state_changed, reason_code = scheduler_agent_busy_compensation)
commit
```

这不是正常业务 transition，也不暴露给 `transfer-task`。恢复原 todo rank，避免一次纯技术竞态改变用户队列顺序。

如果来自 `in_progress / in_review` relaunch，则只把 Dispatch 标记为 `skipped(agent_busy)`，Task state / rank 不变。

## 12. Retry

Launch temporary error 使用有限 retry + backoff。

`AgentBusy` 明确排除在 temporary error 之外：它直接使 Dispatch 进入 `skipped(agent_busy)`，不 retry、不 backoff、不添加 technical blocker。

Dispatch 至少记录：

```text
attempt_count
next_retry_at
last_error
```

### 12.1 每次 attempt

在发起 Launch 前递增或原子记录 attempt。

失败后：

```text
attempt_count < max_attempts
    -> pending
    -> calculate next_retry_at
```

达到上限：

```text
pending -> failed
```

### 12.2 Backoff

可以使用：

- exponential backoff；
- capped exponential backoff；
- optional jitter。

具体数值属于 runtime config。

与 relaunch cooldown 不同：

- Launch retry 使用 wall-clock backoff；
- Task relaunch cooldown 使用 traversal skip count。

这两者不能混淆。

## 13. Unknown Outcome

例如：

```text
request sent
Agent Executor persisted Execution
network response lost
```

Scheduler 看到的只是 Launch error / timeout。

处理：

```text
keep same pending Dispatch
wait retry backoff
launch again with same idempotency_key
```

Agent Executor 根据 idempotency 返回第一次创建的 Execution。

Scheduler 获得 execution_id 后：

```text
pending -> launched
```

不能因为 outcome unknown 创建新的 Dispatch。

## 14. Retry 到期前的 Task Visit

Scheduler traversal 再次访问该 Task 时，如果：

```text
pending Dispatch exists
AND now < next_retry_at
```

则：

```text
skip
```

本次仍消耗正常 tick interval。

不会为 retry 单独启动 timer worker。

到后续某次 Task visit：

```text
now >= next_retry_at
```

再继续同一 Dispatch。

因此 Dispatch retry 仍然服从 Project Scheduler 的串行 Task pacing。

### 14.1 Pending 期间 Task 状态变化

pending Dispatch 可能已经向 Agent Executor 发出过 Launch，只是 Scheduler 尚未可靠确认 outcome。

因此如果 pending 期间 Task 被用户或 Agent 改成 `blocked / cancelled / done` 等不再需要本次 Execution 的状态，Scheduler 也不能简单删除 pending Dispatch 或创建 replacement Dispatch。

必须先继续使用同一个 idempotency key，把 Launch outcome reconciliation 到确定状态。

如果最终确认已经创建 Agent Execution：

```text
pending -> launched
obtain execution_id
```

再根据 Task 当前 Source of Truth 决定是否需要请求 Agent Executor cancel 这个已经确认存在、但现在不再匹配 Task 状态的 Execution。

这样可以避免 unknown outcome 下既丢失旧 Execution、又错误创建第二个 Dispatch。

## 15. Final Launch Failure

本节只处理真正的 Launch technical failure / unknown outcome retry 耗尽；明确的 `AgentBusy` 已在前面进入 `skipped`，不进入本流程。

当 retry 耗尽：

必须在一个事务中完成：

```text
Dispatch:
  pending -> failed
  failed_at = now

Task:
  add technical blocker
  -> blocked

Task Event:
  blocker_added
  state_changed
```

对于原 todo claim：

Task 此时通常已经是 in_progress。

对于 work / review relaunch：

Task 可能是 in_progress / in_review。

无论来源，最终统一：

```text
Task -> blocked
```

Scheduler 不保留“launch failed 但 Task 仍自动可执行”的状态。

## 16. Technical Blocker

technical blocker 统一使用 Task Blocker schema：

```text
type = technical
description = safe user-facing summary
metadata:
  code = scheduler_launch_failed
  source = scheduler_dispatch
  reference_id = dispatch_id
```

不要把完整 internal error stack 放进 blocker。

诊断细节保存在 Dispatch.last_error / logs 中。

解除 technical blocker 后：

```text
blocked -> todo
```

由 Task Domain / 用户决定何时解除。

Scheduler 不自动重试 failed Dispatch。

## 17. Execution 关联

Launch 成功后：

```text
SchedulerDispatch.execution_id = AgentExecution.id
```

Agent Execution 本身也包含：

```text
trigger_type = task
trigger_reference = task_id
purpose
```

两者一起支持：

- Task 历史 Execution 查询；
- Dispatch 历史；
- duplicate prevention；
- Project Scheduler concurrency；
- 调试。

Task 主记录不需要复制 execution_id 列表。

## 18. launched 之后

Dispatch 一旦：

```text
status = launched
```

SchedulerDispatch 不再追踪 Execution lifecycle。

因此下面这些都不修改 Dispatch status：

```text
Execution -> running
Execution -> waiting
Execution -> succeeded
Execution -> failed
Execution -> cancelled
```

Dispatch 仍保持 launched。

它表达的是：

> Launch 已成功。

不是：

> Execution 已成功。

## 19. Pending Dispatch 与 Project Concurrency

`pending` Dispatch 预占一个 concurrency slot。

原因：

在 Agent Executor 尚未返回 execution_id 时，Scheduler 不能确定 Execution 是否已经创建。

所以 Project 使用：

```text
concurrency_used
=
pending_dispatch_count
+
scheduler_executions(status in created, preparing, running)
```

注意避免双计数：

如果 pending Dispatch 已经能可靠关联 execution_id，则应尽快原子更新为 launched。

Project concurrency 查询应保证：

- pending 无 execution_id：算 pending slot；
- launched Execution：按 Execution status 计数。

## 20. Current Sprint 切换

正常 Sprint Lifecycle 下，`complete-sprint` 在提交前强制要求：

- Current Sprint 不存在 active Task Execution；
- Current Sprint 不存在 pending SchedulerDispatch。

并且 Sprint completion 与 Scheduler Dispatch creation 共享 Project scheduling lock。

因此正常的 Current Sprint 切换不会留下旧 Sprint 的 pending Dispatch 或 active Execution。

如果由于历史数据、故障恢复或未来管理操作仍观察到：

```text
current_sprint_id changed
+
old sprint pending Dispatch exists
```

Scheduler 必须把它视为异常恢复场景：不能直接丢弃 pending Dispatch，仍需使用同一个 idempotency key 把 unknown Launch outcome reconciliation 到确定状态，再按 Task 当前 Source of Truth 决定后续处理。

历史 `launched / failed / skipped` Dispatch 不受 Sprint 切换影响，也不改写 `sprint_id`。

完整 Sprint start / complete / rollover 见 [Sprint Lifecycle](../project-work-management/sprint-lifecycle.md)。

## 21. Central Restart Recovery

Central restart 后不需要独立 Dispatch recovery worker。

Scheduler traversal 访问 Task 时：

### pending

根据：

- attempt_count；
- next_retry_at；
- idempotency_key；

继续同一 Dispatch。

### launched

无需处理。

### failed

无需处理。

### skipped

无需处理。它表示此前一次 Launch 意图因明确 `AgentBusy` 竞争而结束；后续是否创建新的 Dispatch 由当前 Task reconciliation 决定。

如果 pending Dispatch 对应 Task 已经发生人工状态变化，仍然不能直接创建新 Dispatch。

必须先完成 / 终结原 pending Dispatch，避免 unknown outcome。

## 22. Dispatch Error

建议结构：

```text
SchedulerDispatchError
├── code
├── category
├── retryable
├── public_message?
├── internal_detail?
└── occurred_at
```

第一阶段 category 可以简单：

```text
agent_executor_unavailable
network_error
launch_timeout
invalid_launch_request
unknown
```

Scheduler 只用它决定：

- 是否进入 retry；
- 是否最终 failed。

它不代表 Agent Execution error。

`AgentBusy` 不写入 `SchedulerDispatchError` 作为 Launch failure；它记录在 `status = skipped` 与 `skip_reason = agent_busy`。

## 23. Version 与并发控制

SchedulerDispatch 使用 version：

```text
version bigint
```

mutation 使用 compare-and-swap。

例如：

```text
UPDATE scheduler_dispatch
SET
  status = launched,
  execution_id = ?,
  version = version + 1
WHERE id = ?
AND status = pending
AND version = ?
```

避免 retry completion 与 concurrent recovery 互相覆盖。

`pending -> skipped(agent_busy)` 使用同样的 compare-and-swap 规则，避免与迟到的 Launch success / recovery 互相覆盖。

## 24. Retention

第一阶段：

- SchedulerDispatch 不自动过期；
- retry metadata 不自动清理；
- error metadata 不自动清理。

历史 Dispatch 用于：

- 解释 Task 为什么启动某次 Execution；
- 排查 duplicate / unknown outcome；
- 统计调度轮次；
- 审计 Scheduler 行为。

后续如果引入 retention，应与：

- AgentExecution；
- Audit；
- Tool Operation；
- Runtime View；

统一考虑。

## 25. 查询接口

Scheduler 内部至少需要：

```text
get_pending_dispatch(task_id)
get_latest_launched_dispatch(task_id, purpose)
create_dispatch(...)
mark_launched(dispatch_id, execution_id)
record_retry(dispatch_id, ...)
mark_failed(dispatch_id, error)
list_project_pending_dispatches(project_id)
```

这些是内部 service contract，不要求直接暴露给前端。

前端若展示 Scheduler 调试信息，可以通过专门的 Project / Task diagnostic API 读取 projection。

## 26. 不在本文定义的内容

- Project traversal / tick：[Scheduler Loop](./scheduler-loop.md)
- Task decision tree：[Task Reconciliation](./task-reconciliation.md)
- Agent Launch idempotency：[Agent Execution Domain Model](../agent-executor/execution-domain-model.md)
- Task Domain：[项目与工作管理](../project-work-management/README.md)
