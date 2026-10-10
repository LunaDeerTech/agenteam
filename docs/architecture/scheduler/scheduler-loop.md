# Scheduler Loop 详细设计

> 上层架构：[Scheduler](./README.md)
>
> 相关详细设计：
> - [Task Reconciliation](./task-reconciliation.md)
> - [Scheduler Dispatch](./scheduler-dispatch.md)

## 1. 设计范围

本文定义 Project Scheduler 的运行循环，包括：

- Current Sprint 解析；
- Task traversal；
- state group 顺序；
- priority / manual rank 排序；
- tick interval；
- Project pause；
- Project max concurrency；
- relaunch skip count；
- restart 后的 loop 恢复；
- Scheduler runtime state。

本文不定义：

- Task 状态机本身；
- Agent Execution 生命周期；
- Agent Loop；
- Sprint start / complete / rollover（见 [Sprint Lifecycle](../project-work-management/sprint-lifecycle.md)）；
- SchedulerDispatch 的完整持久化模型。

## 2. Project-local Scheduler Runtime

每个 Project 最多运行一条 Scheduler traversal。

概念：

```text
ProjectSchedulerRuntime
├── project_id
├── running
├── current_sprint_id?
├── traversal_generation
├── current_group?
├── current_task_id?
└── last_tick_at?
```

这些字段可以是运行时内存状态，不要求全部持久化。

Scheduler 的业务正确性不能依赖内存 cursor。

Central restart 后允许从：

```text
Current Sprint
-> first state group
-> first Task
```

重新开始 traversal。

因为每次 Task reconciliation 都基于最新数据库状态，并且 Launch 通过 SchedulerDispatch / idempotency 保证，所以无需恢复精确的内存 cursor。

## 3. Current Sprint

Scheduler 每轮 traversal 开始时读取：

```text
Project.current_sprint_id
```

约束：

- 一个 Project 最多只有一个 Current Sprint；
- Current Sprint 可以为空；
- Scheduler 不为非 Current Sprint Task 创建新的调度；已有 pending 的核对仍须纳入同一串行路径。

如果：

```text
current_sprint_id = null
```

Scheduler 不创建新调度；若还有历史 pending，沿本节及 §12 的串行恢复路径核对，没有 pending 才 idle。

Sprint 生命周期由 [Sprint Lifecycle](../project-work-management/sprint-lifecycle.md) 定义。

### 3.1 Current Sprint 在 traversal 中发生变化

Traversal 开始时记住：

```text
captured_current_sprint_id
```

每次处理下一个 Task 前都允许快速确认 Project 的 current_sprint_id 是否仍然相同。

如果已经切换：

```text
captured_current_sprint_id != Project.current_sprint_id
```

立即结束当前 traversal，并从新的 Current Sprint 开始下一轮。

这样不会在 Sprint 切换后继续向旧 Sprint Launch 新 Execution。

## 4. State Group 固定顺序

Scheduler 固定按照：

```text
todo
-> in_progress
-> in_review
-> blocked
```

遍历。

不包含：

- backlog；
- done；
- cancelled。

这些状态不创建新调度，也不自动 reopen；如果仍关联旧 pending Dispatch，必须沿同一串行路径核对它，不能被 state group 过滤永久遗漏。

这个顺序是固定 contract，不根据当前 Task 数量动态改变。

## 5. Group 内排序

同一个 state group 内：

```text
ORDER BY
  priority DESC,
  manual_rank ASC
```

其中 priority 业务顺序：

```text
critical
high
medium
low
```

manual_rank 使用 fractional rank。

### 5.1 默认 rank

当 Task：

- 新建；
- state 改变；
- priority 改变；

进入新的：

```text
sprint + state + priority
```

分组时，普通 state / priority 变化默认追加到该分组末尾。

唯一例外是 `scheduler_agent_busy_compensation`：原 todo claim 因极少数 `AgentBusy` 竞态被补偿时，在当前事实/version 仍允许的前提下恢复原逻辑位置，不能追加到 todo group 末尾，也不能无条件写回已失效的旧 rank。

用户可以通过前端拖拽重新排序。

### 5.2 Rank rebalance

当相邻 fractional rank 空间不足时，可以执行内部 rebalance。

rebalance：

- 不改变 Task 业务状态；
- 不产生 SchedulerDispatch；
- 不改变用户可见顺序；
- 可以批量更新同组 rank。

纯排序维护不推进 Task 业务 version、不生成 TaskEvent；真实拖拽/移动仍是业务 mutation。D11/D23 以维护与 pending claim 的互斥或位置映射等正式契约保证 Busy 补偿恢复逻辑顺序，普通字段写入不能覆盖新 rank；具体机制不在此预选。

## 6. Traversal Snapshot

每一轮 traversal 按 group 构造有序 Task identity snapshot。

概念：

```text
TraversalPlan
todo_ids[]
inProgressIds[]
inReviewIds[]
blocked_ids[]
```

Snapshot 只保存 Task ID 与当时所属 group，不复制 Task 完整内容。

处理每个 Task 时必须重新从数据库读取最新 Task。

如果 Task 在等待轮到它期间已经离开 snapshot 中的 state：

```text
current_state != snapshot_group
```

本次直接 skip。

这只跳过新业务调度；已存在 pending Dispatch 的恢复检查先于该过滤。Task 已 done/cancelled、group/Sprint 改变时，原 pending identity 仍进入同一串行 traversal/recovery，去重与排位由 D23 固定，不另启 worker。

它不会因为新状态重新插入本轮其他 group，而是等待下一轮 traversal。

这样可以保证：

- 每轮每个 snapshot Task 最多处理一次；
- group 顺序稳定；
- 避免 Task 在一个 traversal 中多次跳组执行。

## 7. Tick 定义

`tick_interval` 是两个相邻 Task 调度尝试之间的等待时间。

正确语义：

```text
reconcile task A
wait tick_interval
reconcile task B
wait tick_interval
reconcile task C
```

错误理解：

```text
wait 30s
scan all tasks
```

### 7.1 Skip 也消耗 Tick

无论当前 Task：

- 成功 Launch；
- blocked 条件尚未满足；
- 已经存在 active Execution；
- concurrency 已满；
- cooldown 未结束；
- state 已变化；
- Scheduler 当前无动作；

都算完成一次 Task 调度尝试。

之后必须等待完整 tick interval 才能处理下一个 Task。

### 7.2 Traversal 结束

最后一个 Task 调度完成后：

```text
wait tick_interval
-> start next traversal
```

因此 traversal 边界本身不会出现无间隔的立即重扫。

如果 Current Sprint 当前没有任何需要遍历的 Task，可以使用同一个 tick interval 作为 idle polling interval。

## 8. Scheduler 主循环

概念伪代码：

```text
while service_running:
    project = load_project()

    if scheduler_disabled:
        wait tick_interval
        continue

    sprint = project.current_sprint

    if sprint == null and no existing pending dispatch:
        wait tick_interval
        continue

    traversal = build_ordered_task_snapshot(sprint)
    include existing pending dispatch identities in the same serial recovery path

    for entry in traversal:
        if scheduler_disabled:
            break

        if project.current_sprint_id changed:
            break

        task = reload(entry.task_id)

        if entry has pending dispatch:
            reconcile original dispatch outcome without creating replacement
        elif task.state != entry.expected_state:
            skip
        else:
            reconcile(task)

        wait tick_interval
```

真正的 Task decision tree 见 [Task Reconciliation](./task-reconciliation.md)。

伪代码只表达既有串行路径的覆盖要求，不规定 pending 的具体插入顺序。无 Current Sprint、旧 Sprint 或终态 Task 的遗留 pending 都不得漏查；每次处理仍服从相同 pacing，不能用无间隔 query/launch 循环阻塞整条 traversal。

## 9. Pause / Resume

Project 配置：

```text
scheduler_enabled: boolean
```

暂停后 Scheduler 不再做任何自动业务 mutation：

- 不解除 blocker；
- 不做 `blocked -> todo`；
- 不 claim todo；
- 不 relaunch；
- 不创建 SchedulerDispatch。

已存在的 Agent Execution 继续运行。

暂停不借核对调用 launch 或 mutation Task，恢复时仍沿原 Dispatch/key 处理，不能换 key 绕过。

Resume 后无需恢复旧 cursor。

直接读取当前 Current Sprint 并创建新的 traversal snapshot。

## 10. Project 最大并发

Project 配置：

```text
scheduler_max_concurrency: integer | unlimited
```

系统默认：

```text
default_project_scheduler_max_concurrency
```

创建 Project 时复制系统默认值。

### 10.1 计数范围

只统计该 Project 中由 SchedulerDispatch 创建的：

```text
AgentExecution.status in:
created
preparing
running
```

不统计：

```text
waiting
succeeded
failed
cancelled
```

另外：

```text
SchedulerDispatch.status = pending
```

也预占一个 concurrency slot。

确认关联后原子转为 launched，额度从 pending 预占转按 Execution 状态计算，不能双计；waiting 仍占 Agent 全局 slot，和本节 Scheduler 额度分别判断。

### 10.2 Task active Execution 判断与并发不同

单个 Task 是否已有 active Execution 使用：

```text
created
preparing
running
waiting
```

因此 waiting：

```text
does not consume project quota
BUT
still blocks duplicate launch for same task
```

这是两个独立判断，不能复用同一 SQL 条件。

### 10.3 Concurrency 满时

如果 concurrency 已满：

- 当前 Task 不创建 Dispatch；
- 当前 Task 仍算完成一次调度；
- 仍等待 tick interval；
- 后续 Task 继续按顺序被访问，但同样不能 Launch。

Scheduler 不为了“并发满”而中止整轮 traversal。

这是为了保持稳定、可预测的 traversal pacing。

## 11. Relaunch Skip Count

配置：

```text
relaunch_skip_count >= 0
```

只适用于：

- `in_progress`；
- `in_review`；

并且只在上一轮 Scheduler Execution 已 terminal、Task 仍需要继续执行时生效。

### 11.1 SchedulerTaskRuntime

为了在 Central restart 后仍保持 skip 语义，Scheduler 维护一个轻量内部 projection：

```text
SchedulerTaskRuntime
├── project_id
├── task_id
├── cooldown_execution_id?
├── cooldown_purpose?
├── relaunch_skip_remaining
├── version
└── updated_at
```

它不是 Task Domain 对象，也不暴露给 Agent。

唯一键：

```text
(project_id, task_id)
```

### 11.2 初始化 cooldown

当 Scheduler 第一次观察到：

```text
Task requires execution
AND
no active execution
AND
latest scheduler execution is terminal
AND
cooldown_execution_id != latest_execution_id
```

写入：

```text
cooldown_execution_id = latest_execution_id
cooldown_purpose = current purpose
relaunch_skip_remaining = configured relaunch_skip_count
```

当前这次 Task visit 立即参与 skip 消耗。

如果：

```text
relaunch_skip_remaining > 0
```

则：

```text
remaining = remaining - 1
skip launch
```

当后续某次访问开始时已经：

```text
remaining = 0
```

才允许创建新 Dispatch。

### 11.3 新 Execution 创建后

成功创建新的 SchedulerDispatch / Execution 后，可以清空旧 cooldown projection。

下一次只有在新的 Execution terminal 后才重新初始化 skip count。

### 11.4 Task phase 改变

如果 Task 从：

```text
in_progress -> in_review
```

或者：

```text
in_review -> todo
```

旧 purpose 的 cooldown 不继续继承。

新的 phase 按自己的最新 Execution 与最新 Task 状态重新判断。

## 12. Dispatch Recovery 与 Loop 的关系

Scheduler 不运行独立事件消费者。

Pending Dispatch recovery 仍发生在原串行 traversal/recovery/pacing 中，包括 Task 已离开正常 group 或变为 done/cancelled 的旧 pending，不另建服务或 timer worker。

当 Scheduler 访问一个 Task，发现存在：

```text
pending SchedulerDispatch
```

优先处理该 Dispatch：

- 未到相应处理时机：skip 并保持正常 pacing；
- 已确认暂时 Launch 失败且允许重试：按有限 retry/backoff 处理原 Dispatch/key，并重验当前合法性；
- Launch 结果未知：只读查询原 key 的 Executor 持久事实，无法确认就保留 pending/待确认；查询失败或暂未找到不能解除防重复保护；
- 只有已确认 Launch 失败才走最终失败事务，并检查当前 Task 是否仍允许 mutation；unknown 不因 retry 耗尽被改为 failed/blocked。

绝不能在存在 pending Dispatch 时再创建第二个 Dispatch。

完整规则见 [Scheduler Dispatch](./scheduler-dispatch.md)。

## 13. Restart

Central restart 后：

1. 不恢复精确 traversal cursor；
2. 重新读取每个 Project 的 Current Sprint；
3. 从 `todo` group 开始新 traversal；
4. Task reconciliation 使用最新数据库状态；
5. pending Dispatch 按已知暂时失败与 unknown 分流；unknown 用原 key 只读核对，不以 launch 冒充查询；
6. SchedulerTaskRuntime 继续保存 relaunch skip progress。

因此重启可能使某些 Task 比原计划更早再次被访问，但不会导致重复 Launch 或破坏 cooldown 语义。

## 14. 配置建议

第一阶段至少：

```text
SchedulerRuntimeConfig
├── tick_interval
├── relaunch_skip_count
├── launch_retry_max_attempts
├── launch_retry_initial_backoff
└── launch_retry_max_backoff
```

Project：

```text
ProjectSchedulerConfig
├── scheduler_enabled
└── scheduler_max_concurrency
```

系统：

```text
SystemSchedulerDefaults
└── default_project_scheduler_max_concurrency
```

具体默认秒数 / retry 次数可以作为部署配置，不需要写死在架构 contract 中。

这些有限次数只控制已确认暂时 Launch 失败，不为 unknown 核对设置耗尽即失败规则；查询端口与节奏在 D01/D22/D23 固定，继续复用本 Loop。

### 14.1 显式 retry policy 与已实现前置的边界

`internal/central/scheduler/retry_policy.go` 的库实现已通过原三个定向 top 的组合 race 与 Scheduler 包 vet。`NewLaunchRetryPolicy(maxAttempts, initialBackoff, maxBackoff)` 必须显式传入三个参数，满足 `maxAttempts≥1` 和 `0<initialBackoff≤maxBackoff`；没有默认值。不可变值的 `NextDelay(attemptCount)` 把首次 Launch 计为第1次：首次失败延迟为 initial，后续按 `min(initial×2^(attemptCount−1), max)` 计算并安全封顶；达到或超过上限返回无重试额度。零/负 attempt 拒绝，不使用 jitter、时钟、sleep 或 timer worker。

Central 配置 `Load` 已支持显式的三项环境变量，并由 `Config.SchedulerLaunchRetryPolicy()` 返回不可变值及是否配置的标志，详见[后端配置说明](../../development/backend/README.md#配置与命令)。三项全部缺省保持旧启动行为；任一项存在时必须全部存在且合法，空串也拒绝，不补默认值。加载配置不创建 Scheduler，也不启动生产 Loop。

`NewCoordinatorWithRetryPolicy` 固定经验证的 policy 值，只在首次 Claim 的原 Work 写入与 pending INSERT 同一事务中保存规范字节和 `Identity()` 摘要。摘要包含固定 `capped_exponential_v1` 算法及三个精确参数（duration 为整数纳秒）；迁移 00049 要求两列成对合法且不可修改。旧构造器保持兼容，旧 NULL 行不补 policy；普通重放沿用原持久值，部署配置变化不能替换它。原 Claim Unknown 核对须匹配当次写入或 final 重放实际观察到的 policy。`Dispatch.RetryPolicy()` 只返回值副本；缺失或存储损坏不能成为发送授权。

Execution 已实现 `MatchLaunchTemporaryRejection` 的唯一临时证明 `launch_lock_timeout_v1`：只在最终 Launch 的原 `AcquireAll` 发生 `LockFailed` / SQLSTATE `55P03`，原 `WithinTx` 已实际退出并返回 `NotCommitted`，且上下文未取消时签发。证明绑定完整原请求摘要、RequestID 和幂等 key，并核对原锁错误与事务返回的同一物理错误；初次 Lookup、Discover、Unknown、取消或任意通用 Fault/RetryHint 均不能产生该证明。错误分类不改变原 cause 或 Unknown 边界，也不直接授权重发。

上述配置、Claim 绑定和 Execution 证明已通过有限独审、15 个定向 top 的 race、五包 vet、候选编译及真实 PG 的 1 top/2 sub 整轮验证。真实链覆盖显式配置、Claim 原事务绑定与重放、旧 NULL 兼容及真实最终锁超时后签发证明且零 Execution；原调用、七资源和全部退出尾闭合。

后继有限 retry 实现已通过有限独审、11 个定向 top 的 race、三包 vet、编译及真实 PG 的 1 top/2 sub 整轮验证，原调用和七资源全部退出尾闭合。`NewLaunchHandoffWithRetry(authority, dependencies, projects)` 绑定真实 Project 门，沿原 Handoff 的调用登记与 Unknown owner 提供 `RetryDue(ctx, projectID, dispatchID)`；一次至多发送同一 key/request 的一个 attempt，不替换旧构造器或 policy。只有原同步 temporary 证明在原拒绝事务中持久记录、policy 仍为 Claim 时的绑定且到期后，才能在当前 Project 门及原 CAS 下递增 attempt、写入 unknown；该标记明确提交后才调用原 Launch。到期时间向上取整至微秒，上一 attempt 的临时错误仅保留诊断，不授权下一次发送。真实链已覆盖 temporary→到期→created，以及持续 temporary→耗尽→原 `FinalizeLaunchFailure` 同事务结算 Work 与 Dispatch，详见[有限 retry 实现边界](./scheduler-dispatch.md#123-有限-retry-实现边界)。

有界 PendingVisitor 已接入上述显式 retry 分支；原 unknown 先 Lookup，Busy 和最终失败仍由各原 owner 处理。未到期、暂停、Current Sprint 不匹配或旧 NULL policy 不产生新发送，暂停也不结算 Task；旧无分类 known-not-created 不补证明。生产 app / Loop 尚未绑定，没有新增 timer worker、自动遍历或 relaunch 实现，不能将这些可调用端口视为完整 Scheduler Loop 已可用。

## 15. Observability

建议至少暴露：

- project scheduler enabled；
- current_sprint_id；
- current state group；
- current task_id；
- last task scheduled at；
- active scheduler execution count；
- pending dispatch count；
- last traversal started_at；
- last traversal completed_at。

这些数据用于诊断，不作为 Scheduler correctness 的 Source of Truth。

## 16. 不在本文定义的内容

- Task 状态判断：[Task Reconciliation](./task-reconciliation.md)
- Dispatch schema / retry：[Scheduler Dispatch](./scheduler-dispatch.md)
- Task Domain：[项目与工作管理](../project-work-management/README.md)
- Agent Execution：[Agent Executor](../agent-executor/README.md)
- Sprint lifecycle：[Sprint Lifecycle](../project-work-management/sprint-lifecycle.md)
