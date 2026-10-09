# D11 Task transition 与 reviewer 工程规格

> 状态：工程草案，尚未接受；本文没有授权产品实现，也不是运行验收结果。
>
> 前置：[Task planning](d11-task-planning.md) 的契约子结果已交付，运行服务整卡仍在验收。本卡运行服务开工须以该整卡通过为前提；下述新增纯契约、状态决策及兼容方案可以分别审查，不因整个 Agent/Executor 尚未实现而全部停工。
>
> 业务依据：[D01 领域生命周期](d01-contracts/domain-lifecycle.md)、[Task 状态机](../../architecture/project-work-management/task-state-machine.md)、[Task 领域模型](../../architecture/project-work-management/task-domain-model.md)、[Task Timeline](../../architecture/project-work-management/task-event-timeline.md)、[Blocker / Dependency](../../architecture/project-work-management/task-blocker-dependency.md)、[Agent 管理](../../architecture/agent-management.md)。跨域身份、幂等和事务以 [D01 基础契约](d01-contracts/foundation.md)、[Execution / Scheduler](d01-contracts/execution-orchestration.md) 为准。

## 1. 目标、现状与兼容边界

提供统一 `TransferTask`，把合法 state 变化、显式 assignee/reviewer 交接、必要 comment、允许的 Blocker 变更、rank、TaskEvent、Outbox 和完成回执原子提交。reviewer 就是 `in_review` 的当前 `Task.AssigneeAgentID`，不建立第二个 reviewer 列或独立批准状态机。

现有 Task planning 只成功写入未指派 backlog。其 `Task` 已能严格解码七种 state 和持久 assignee，这不证明存在 Agent/Blocker/Execution 服务，也不授权现有 Create/Update/Reorder 写这些状态。规划验收中的直接 SQL future-state fixture 只验证读取、约束与否定边界，不能作为本卡的分配或转换正向证据。当前 Agent 只有 `identity.AgentID` marker，没有可供 Work 使用的当前 Agent 领域实现。

兼容边界固定如下：

| 已有结果 | 本卡处理 |
| --- | --- |
| `Task`、`TaskType/Priority/State`、TaskReader 与七 filter | 复用 canonical，不增加 reviewer、blocker 数组或执行状态；所有真实变化推进现有 Project query generation |
| `TaskCreate/TaskFieldsUpdate/TaskReorder`、三名 `TaskCommandName`、`TaskMutation` | 保持旧闭集、presence、单历史 ID 与单 Outbox ID 语义；不借 Update 输入 state/assignee |
| `TaskPosition` | 保持 **backlog-only** 验证；新增 `TaskTransitionPosition` 表达其他组，不能放宽旧类型绕过已验 schema |
| `TaskEvent` / `TaskEventActor` / 两种历史 payload | 保持规划命令 Human、`task_created/fields_updated` 严格解码；新增版本化历史分支，不令旧 decoder 默认接受 Agent/System/comment |
| `work.task_changed` / `work.task` / schema 1 | 继续由旧 typed 工厂和 producer 校验；流转使用独立 `work.task_transitioned` schema 1 |
| `00022` 与旧 `task_commands` | 不重写历史迁移或旧回执；新迁移按 §7 引入专用流转命令存储和兼容的 TaskEvent 外键分支；实施前由全局迁移唯一负责人分配编号，本文不占号 |

本卡不实现 HTTP/Tool adapter、独立 comment 编辑/软删、Timeline 分页、普通 assignee-only update、Move/Delete/Sprint Complete、Scheduler loop、Executor 或 Inbox/Audit projection。它们消费正式端口，不能把本卡当作相应生产装配已完成。取消 Task 与取消 Execution 是不同命令；有真实活动时的组合见 §4、§13。

旧架构伪代码中承担去重含义的 `request_id` 在此映射为业务 `IdempotencyKey`，传输 RequestID 不参与摘要。旧文档“恢复旧 manual_rank”按 D01 收敛为恢复 claim **逻辑位置**与持久位置映射，不写回已失效 rank。未列状态边的 code 统一沿 D01 `INVALID_STATE`；不再新增含义相同的 `TASK_STATE_TRANSITION_INVALID`。

## 2. 完整边表、Actor 与提交不变量

以下是正式边表的工程展开。H=当前 Project Owner Human；A=经真实当前 Execution/Tool authority 授权的同 Project AgentRun；R=A 且其 AgentID 等于转换前当前 reviewer。S=与持久 Scheduler/Executor cause 绑定的特定内部服务能力，不是任意 `identity.Service`。H 不需要伪装 Agent reviewer。

| from | to | 允许来源 | 同事务条件 |
| --- | --- | --- | --- |
| backlog | todo | H、A | 结果 assignee 当前合法；结果无 unresolved Blocker |
| backlog | cancelled | H、A | optional comment；保留原 assignee |
| todo | in_progress | Scheduler claim 专口 | 当前合法 assignee、Current Sprint、无 unresolved、无冲突 pending、真实 slot/额度及 Dispatch 同事务 |
| todo | blocked | H、A、限定 S | 结果至少一个 unresolved Blocker |
| todo | cancelled | H、A | optional comment；保留原 assignee |
| in_progress | todo | 对应 claim 的 AgentBusy 补偿专口 | 原 Dispatch、ClaimGuard 和持久逻辑位置匹配；不覆盖并发编辑 |
| in_progress | in_review | H、A | **显式** reviewer assignee、必填 comment；当前 Agent 事实有效 |
| in_progress | blocked | H、A、限定 S | 结果至少一个 unresolved Blocker |
| in_progress | cancelled | H、A | optional comment；保留原 assignee |
| in_review | done | H、R | 必填 review comment；保留 reviewer |
| in_review | todo | H、R | **显式** next assignee、必填 review comment、结果无 unresolved |
| in_review | blocked | H、R、限定 S | 必填 review comment；结果至少一个 unresolved |
| in_review | cancelled | H、A | optional comment；保留原 assignee |
| blocked | todo | H、A、Scheduler reconciliation 专口 | 全部 Blocker 已解决、结果 assignee 当前合法；Scheduler 不选人 |
| blocked | cancelled | H、A | optional comment；保留原 assignee/Blocker 历史 |
| done / cancelled | 任意 | 无 | `TASK_TERMINAL_IMMUTABLE`；独立 comment 属另一个命令 |

七乘七组合全部由纯决策测试枚举。包括 `from==to` 在内的未列边均拒绝 `INVALID_STATE`，不把空转换当 no-op，不借目标 state 绕过 Scheduler-only 边。合法历史 replay 不再次执行边检查。Human/Agent 的公共 DTO 即使填入 `in_progress`，也不能取得 claim 权限；`in_progress→todo` 不能成为普通“退回待办”。

合法 active 结果 `todo/in_progress/in_review` 必须有当前同 Project Agent；`blocked` 可以保留空 assignee，但至少一个 unresolved Blocker。存在 Blocker 不反推 state 必须 blocked；目标 todo 必须没有 unresolved。done 唯一入口是 in_review；终态禁止改业务内容和 Blocker 结构。取消不自动 resolve 原 Blocker，也不宣称满足其他 Task 的 rely_on。目标 done 的依赖满足只供后续 Scheduler reconciliation 读取，不在本 Tx 递归变更其他 Task。

A 的一般写权限来自正式 Tool/Execution 授权，不凭 AgentID 存在推断，也不凭同 Project 自动授权。Reviewer 检查另比较 **当前** assignee；旧 Dispatch/Execution 捕获的 Agent 不是当前 reviewer 的替代。其他 A 边不额外发明“只有当前 assignee 才可操作”的规则，仍受授权能力与资源 scope 收紧。运行身份真实端口与终态历史可见权限见 §4、§13。

## 3. 请求、结果与语义 presence

新增 `TaskTransitionCommandID`、`TaskBlockerID` 仅分别对应真实流转命令和 Blocker 身份，复用 Foundation typed UUIDv7；Agent、Task、TaskEvent、Project 等沿既有 marker。跨事件的 operation/correlation 使用流转命令身份；不把 transport RequestID 写成 operation。BlockerID 的唯一定义归 Blocker 契约子结果，本卡只引用，不能各造同名 marker。

```go
type TaskTransitions interface {
    TransferTask(context.Context, identity.Actor, foundation.CommandMeta,
        ProjectID, TaskID, TaskTransfer) (TaskTransitionMutation, error)
    LookupTaskTransition(context.Context, identity.Actor,
        TaskTransitionLookupRequest) (TaskTransitionLookup, error)
}

type TaskTransfer struct {
    TargetState TaskState
    AssigneeAgentID *identity.AgentID
    Comment *string
    AddBlockers []TaskBlockerCreate
    ResolveBlockerIDs []TaskBlockerID
}
type TaskTransitionMutation struct {
    Task Task
    TaskEventIDs []TaskEventID
    EventIDs []event.EventID
}
```

wire 字段是上述 snake_case；`TaskTransfer` 仅 `target_state` 必有，其他可省略，**所有显式 null 拒绝**。assignee 省略=保留转换前持久值；存在=明确选择该合法非零 Agent。没有清空语义：backlog/blocked 的清空分配由未来 assignee-only 命令独立定义。交接两条边即使现值非空仍必须显式提供 next/reviewer，不能用“保留”替代必填。

本卡把可选 assignee 限于目标 todo、`in_progress→in_review` 及进入 blocked 的合法边；done/cancelled 禁止携带该字段，以落实保留历史值而非终态瞬间换人。进入 blocked 显式选人仍须当前 Agent 验证；省略则保留旧值，不能因旧 Agent 当前不可用而擅自清空。对同 ID 的显式交接是否需要一条 from=to 的 `assignee_changed` 见 §13 的局部来源冲突；它不阻塞不同 Agent 的交接规则或纯边判定。

comment 省略=不创建 comment Event；存在须 1–32768 UTF-8 bytes、至少一非 Unicode 空白，允许 TAB/LF/CR，拒绝其他 Cc。保原字节、不 trim/NFC。必填 comment 四边为 `in_progress→in_review`、`in_review→done/todo/blocked`；空串或纯空白不是满足。其他合法边可有 comment，但 Scheduler claim/Busy 补偿不能使用公共字段制造 review comment。

`add_blockers`、`resolve_blocker_ids` 省略展开空数组；显式空数组同义。每个最多16项、合计最多32，重复 BlockerID 拒绝。新增项含 caller 预生成稳定 `blocker_id`，重试不换；resolve IDs 是集合，摘要和执行均按 canonical ID 排序。add 数组按 blocker_id 排序，不能通过重排输入制造另一语义。add/resolve 同 ID 冲突拒绝。本卡公共组合只允许目标 blocked 携 add、`blocked→todo` 携 resolve；不允许 cancelled/done 同时改 Blocker，不把流转口变成独立 Blocker CRUD。其余需要改变 Blocker 的组合待 Blocker 卡扩展，不能在实现中默许。

`TaskBlockerCreate` 的公共 type-specific metadata 由 §4 的 Blocker 子卡严格冻结；没有 `map[string]any` 成功后再猜类型的入口。边与字段不适配返回 `INVALID_ARGUMENT`，非法目标边返回 `INVALID_STATE`。没有“清空全部 Blocker”布尔值或传入 unresolved count。

`TaskTransitionMutation` 三字段全部必有；成功的新命令一定发生 state 变化，TaskEventIDs 长1–35且唯一、EventIDs 恰一个。没有 changed=false 分支；原命令 replay 返回深复制的原 Task 快照及同一有序 IDs。上限35=state1+assignee1+comment1+Blocker变更32；当前公共组合最多16个 Blocker，较大的闭集供同一正式内部 reconciliation contract 使用，不能据此默认开放额外组合。

Lookup request 精确为 `{project_id,command:"work.task.transfer",idempotency_key,semantic_digest}`。结果 `{status,receipt}` 沿 committed/in_progress/not_observed；仅 committed 有非 null `TaskTransitionMutation`。不复用只容纳一条历史的 `TaskCommandLookup`。公共 DTO 不接受 actor、reason_code、cause、rank、版本覆盖值或 skip_* 权限字段。

所有新增 DTO / typed payload 严格拒绝重复/未知/大小写替代 key、非法 UTF-8/孤立 surrogate、尾随值、数值冒充字符串 ID/version、非法 null；Validate、MarshalJSON 与 UnmarshalJSON 语义一致，Clone 深复制全部 pointer/slice/metadata。具体 cap 与安全日志见 §11。

## 4. 跨域能力与真实事实端口

本节及后续章节为本草案待完成的工程展开；此处不表示任何依赖已绑定。
