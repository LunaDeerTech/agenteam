# Scheduler 架构

> 详细设计：
> - [Scheduler Loop](./scheduler-loop.md)
> - [Task Reconciliation](./task-reconciliation.md)
> - [Scheduler Dispatch](./scheduler-dispatch.md)
>
> 相关架构：
> - [项目与工作管理](../project-work-management/README.md)
> - [Agent Executor](../agent-executor/README.md)

## 1. 定位

Scheduler 属于 Project Workspace 下的 Tasks / Work Management。

它不是事件驱动的工作流引擎，也不负责根据 Agent Execution 的结果决定 Task 下一步状态。

Scheduler 的核心职责是：

> 在每个 Project 的 Current Sprint 中，按照稳定顺序持续遍历 Task，并根据 Task 当前持久化状态进行调度 reconciliation。

核心关系：

```text
Project
  -> Current Sprint
      -> Tasks
          -> Scheduler
              -> SchedulerDispatch
                  -> Agent Launch Request
                      -> Agent Executor
                          -> Agent Execution
```

Scheduler 负责判断：

- 当前 Task 是否需要自动解除 blocker；
- 当前 Task 是否需要从 `todo` claim 为 `in_progress`；
- 当前 `in_progress / in_review` Task 是否缺少一个活动 Agent Execution；
- 当前是否允许继续创建新的 Scheduler Agent Execution；
- 某次 Launch 是否需要 retry / recovery。

Scheduler 不负责：

- 为 Task 选择 assignee / reviewer；
- 解释 Agent final output；
- 根据 Execution succeeded / failed 决定业务状态；
- 组装 AgentExecutionContext；
- 执行 Agent Loop；
- 保存 Runtime View；
- 监听 Task / Execution 领域事件。

## 2. 核心运行模型

每个 Project 最多有一个 Current Sprint。

Scheduler 只遍历该 Current Sprint 中的 Task。

一个 Project 只有一条串行 Scheduler traversal：

```text
Current Sprint Tasks
        ↓
todo
        ↓
in_progress
        ↓
in_review
        ↓
blocked
        ↓
next traversal
```

同一个 state group 内按照：

```text
priority DESC
+
manual rank ASC
```

排序。

Scheduler 逐个 Task 调度。

两个相邻 Task 调度尝试之间固定等待一个可配置 `tick_interval`。

因此 tick 的含义是：

> Task-to-Task pacing interval。

它不是“每隔 N 秒扫描整个 Project 一次”。

即使某个 Task 当前没有任何可执行动作而被 skip，本次仍然算完成一次 Task 调度，并且在处理下一个 Task 前仍要等待完整 tick interval。

完整 traversal、排序、tick、pause、并发和 cooldown 规则见 [Scheduler Loop](./scheduler-loop.md)。

## 3. Current Sprint 边界

一个 Project 同一时间最多只有一个 Current Sprint。

Scheduler 只运行在：

```text
project.current_sprint
```

中的 Task 上。

如果 Project 当前没有 Current Sprint，则 Scheduler 保持 idle，不遍历其他 Sprint。

Sprint 的 start / current / complete / rollover 由 Project / Work Management 的 [Sprint Lifecycle](../project-work-management/sprint-lifecycle.md) 定义。

Scheduler 只消费 `Project.current_sprint_id`，不拥有 Sprint lifecycle。

Scheduler 只依赖一个稳定 contract：

```text
Project.current_sprint_id?
```

并假设一个 Project 不会同时存在两个 Current Sprint。

## 4. Scheduler reconciliation 范围

Scheduler 正常主动改变 Task 业务状态的路径只有：

```text
blocked
+ all blockers resolved
    -> todo
```

以及：

```text
todo
+ eligible
 assignee Agent idle
    -> in_progress
    -> launch work Agent Execution
```

如果 assignee Agent 当前 busy：

```text
todo
+ assignee Agent busy
    -> skip this visit
    -> keep todo
```

正常路径下不创建 SchedulerDispatch，也不消耗该 Agent 的 execution slot。

对于已经处于执行阶段的 Task：

```text
in_progress
+ no active Task Execution
+ relaunch cooldown exhausted
 assignee Agent idle
    -> launch work Agent Execution
```

```text
in_review
+ no active Task Execution
+ relaunch cooldown exhausted
 assignee Agent idle
    -> launch review Agent Execution
```

`in_progress / in_review` relaunch 不修改 Task state。

如果 assignee Agent 因 Task、Meeting 或其他 Trigger 的非终态 Execution 正在 busy，则本次 relaunch 直接 skip，Task state 保持不变。

`backlog / done / cancelled` 不产生 Scheduler Launch。

完整状态决策表见 [Task Reconciliation](./task-reconciliation.md)。

## 5. Agent Execution 边界

Scheduler 不解释 Agent Execution terminal result。

它不根据：

- `succeeded`；
- `failed`；
- `cancelled`；
- AgentExecutionError；
- retryable；
- completion reason；
- Agent final text；

推断 Task 应该如何变化。

Agent 在 Execution 内通过 Task Tool / Domain Service 修改 Task。

对 Scheduler 而言：

> terminal Execution 只表示该次执行已经退出，不再占用当前 Task 的活动执行槽位。

如果 Execution 退出后 Task 仍然是 `in_progress` 或 `in_review`，Scheduler 在 relaunch cooldown 结束后会再次为当前 assignee 创建新的 Agent Execution。

因此 Scheduler 的正确性依赖 Task 当前 Source of Truth，而不是依赖 Execution completion callback。

## 6. Active Execution 与 Project 并发

Scheduler 需要区分两个概念：

- **Task active Execution**：防止同一个 Task 被重复 Launch；
- **Agent busy**：防止同一个 Agent 因任何 Trigger 同时存在两个非终态 Execution。

Agent busy 由 Agent Executor 的 active slot 派生，范围跨 `task / meeting / ...` 全部 Trigger。Scheduler 在准备创建新的 Dispatch 前先做 busy 预检查。

该预检查是正常 claim 的前置条件：预检查已经 busy 时不 claim、不创建 Dispatch。最终并发裁决仍由 `AgentExecutor.launch()` 原子完成；如果 precheck 后 Agent 被其他 Trigger 抢先占用，Launch 返回 `AgentBusy`，Scheduler 将它作为极少数竞态补偿，而不是 technical failure。

对单个 Task：

只要存在 Scheduler 启动的非终态 Agent Execution：

```text
created
preparing
running
waiting
```

都视为：

```text
Task has active execution
```

此时绝不能再为同一个 Task创建第二个 Execution。

Project Scheduler 最大并发则只统计主动执行状态：

```text
created
preparing
running
```

不统计：

```text
waiting
```

因此 waiting Execution：

- 仍阻止同一个 Task 被重复 Launch；
- 不占用 Project Scheduler concurrency quota。

未完成 Launch 的 `pending SchedulerDispatch` 也预占一个并发 slot。

## 7. SchedulerDispatch

Scheduler 通过独立的 `SchedulerDispatch` 持久化对象管理可靠 Launch。

状态：

```text
pending
   ├──> launched
   ├──> failed
   └──> skipped
```

`skipped` 用于 Dispatch 已创建后才发生的 `AgentBusy` 竞态兜底。它不创建 technical blocker，也不进入 Launch retry。

对于原 `todo` claim，`AgentBusy` 兜底执行 `scheduler_agent_busy_compensation`：Task 恢复为 `todo`，同时恢复 claim 前的 `manual_rank`，避免技术竞态改变用户排序；对于 `in_progress / in_review` relaunch，Task state / rank 保持不变。后续 traversal 再重新判断 Agent 是否空闲并创建新的 Dispatch。

每次需要新的 Agent Execution 时创建新的 `dispatch_id`。

该 `dispatch_id` 派生 Agent Executor 的稳定 idempotency key。

同一个 Dispatch 的 Launch retry 始终复用同一个 key。

`launched` 以后，Agent Execution 的 succeeded / failed / cancelled 不再改变 SchedulerDispatch。

完整 schema、事务、幂等、retry、unknown outcome 与 recovery 见 [Scheduler Dispatch](./scheduler-dispatch.md)。

## 8. Relaunch cooldown

对于 `in_progress / in_review`：

如果上一轮 Agent Execution 已经 terminal，但 Task 仍然需要继续执行，Scheduler 不立即 relaunch。

使用可配置：

```text
relaunch_skip_count
```

表达 cooldown。

Scheduler 后续每次再次遍历到该 Task 时消耗一个 skip。

只有 skip count 耗尽，并且仍没有 active Execution 时，才允许创建下一次 Dispatch。

Cooldown 使用 Scheduler 自身 traversal 次数，不使用 wall-clock duration。

## 9. Task 排序

Task 在同一个 state group 内按：

```text
priority DESC
manual rank ASC
```

排序。

manual rank 的 scope 为 `sprint + state + priority`：

- 前端允许拖拽；
- 使用 fractional rank / 可插入排序键；
- 新 Task 默认位于对应 sprint + state + priority 分组末尾；
- priority 改变后进入新 priority 分组末尾；
- state 改变后进入当前 sprint 下的新 state + priority 分组末尾。

前端视图与 Scheduler 使用同一套 rank。

## 10. Project Scheduler 配置

Project 至少包含：

```text
scheduler_enabled
scheduler_max_concurrency
```

Scheduler runtime 至少具有：

```text
tick_interval
relaunch_skip_count
launch_retry_policy
```

系统配置提供：

```text
default_project_scheduler_max_concurrency
```

默认值为不限制。

创建 Project 时复制当时系统默认值作为该 Project 的初始 `scheduler_max_concurrency`。

修改系统默认值不追溯覆盖已有 Project。

## 11. Pause

Scheduler pause 是 Project 级控制。

暂停后：

- 不创建新的 SchedulerDispatch；
- 不执行新的 `todo -> in_progress`；
- 不 relaunch `in_progress / in_review`；
- 不自动执行 `blocked -> todo`；
- 不 cancel 已经存在的 Agent Execution；
- 不修改已运行 Task。

恢复后继续从当前持久化状态进行 traversal。

## 12. 用户取消 Task Execution

当用户执行“取消当前 Task Execution”时：

```text
add blocker
+ Task -> blocked
        ↓
commit
        ↓
Agent Executor.cancel(execution_id)
```

必须先把 Task 置为 blocked，再请求 cancel。

这样即使 Execution 很快进入 terminal，Scheduler 后续遍历到该 Task 时也不会立即重新 Launch。

取消一次 Execution 不等于取消 Task。

## 13. Task Event 边界

Task Event 只记录 Task Domain 自身事实，例如：

- comment；
- state_changed；
- assignee_changed；
- blocker_added；
- blocker_resolved。

不复制 Agent Execution lifecycle：

- 不写 `agent_execution_started`；
- 不写 `agent_execution_finished`；
- 不写 `agent_execution_failed`。

Task 页面如果需要展示关联 Execution，直接通过：

```text
trigger_type = task
trigger_reference = task_id
```

查询 Agent Execution 历史。

Scheduler 因此不负责写 Agent Execution lifecycle Task Event。

## 14. 架构原则

Scheduler 保持以下原则：

1. **Project-local**：每个 Project 有自己的单一 Scheduler traversal；
2. **Current Sprint only**：只调度 Current Sprint 中的 Task；
3. **Polling / reconciliation**：不依赖任何领域事件；
4. **Task Source of Truth**：只根据 Task 当前状态做决策；
5. **Execution-result agnostic**：不解释 Agent Execution terminal result；
6. **Serial pacing**：Task 逐个调度，每两个 Task 之间严格等待 tick；
7. **Deterministic ordering**：固定 state group 顺序 + priority + manual rank；
8. **Reliable launch**：通过 SchedulerDispatch + Agent Executor idempotency 保证 Launch；
9. **No duplicate Task execution**：同一个 Task 只允许一个 active Scheduler Execution；
10. **Minimal business mutation**：只主动执行 `blocked -> todo` 与 `todo -> in_progress` 两类正常状态变化。
