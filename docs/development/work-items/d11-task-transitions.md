# D11 Task transition 与 reviewer 工程规格

> 状态：工程规格、T0a/T0b 纯契约已独立接受。Human `backlog→todo`、真实当前 Agent/占用/调度前置及对应 HTTP 已有限交付；完整流转、AgentRun/reviewer 与生产装配仍未完成。本批新增 Human `blocked→todo` 同事务解除 Blocker 的源码已冻结，纯检查和真实组合验证尚未完成，见 §1.1。
>
> 前置：[Task planning](d11-task-planning.md) 的规划库已正式交付并独立验收。本卡运行服务还须满足 §13 的真实 Agent/Blocker/执行前置；下述新增纯契约、状态决策及兼容方案可以分别审查，不因整个 Agent/Executor 尚未实现而全部停工。
>
> 业务依据：[D01 领域生命周期](d01-contracts/domain-lifecycle.md)、[Task 状态机](../../architecture/project-work-management/task-state-machine.md)、[Task 领域模型](../../architecture/project-work-management/task-domain-model.md)、[Task Timeline](../../architecture/project-work-management/task-event-timeline.md)、[Blocker / Dependency](../../architecture/project-work-management/task-blocker-dependency.md)、[Agent 管理](../../architecture/agent-management.md)。跨域身份、幂等和事务以 [D01 基础契约](d01-contracts/foundation.md)、[Execution / Scheduler](d01-contracts/execution-orchestration.md) 为准。

## 1. 目标、现状与兼容边界

提供统一 `TransferTask`，把合法 state 变化、显式 assignee/reviewer 交接、必要 comment、允许的 Blocker 变更、rank、TaskEvent、Outbox 和完成回执原子提交。reviewer 就是 `in_review` 的当前 `Task.AssigneeAgentID`，不建立第二个 reviewer 列或独立批准状态机。

原 Task planning 的创建范围仍是未指派 backlog。其 `Task` 能严格解码七种 state 和持久 assignee，不授权 Create/Update/Reorder 输入状态或指派；后继已接受的 assigned `in_progress` title-only 更新也不提供结构变更权限。规划验收中的直接 SQL future-state fixture 只验证读取、约束与否定边界，不能作为分配或转换正向证据。现 Human transition 已消费真实 Agent `WorkReferences`、Execution occupancy 与 Scheduler pending/group 门，不能以这些有限提供方已存在推导全部状态边可用。

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

### 1.1 Human 原子解除的有限实现

本批沿原 `PrepareTaskTransition` / `TransferTaskInTx` / `TransferTask` / `LookupTaskTransition` 实现当前 Human Owner 的 `blocked→todo`。指定 `ResolveBlockerIDs` 必须属于当前 Task 且尚未解决，全部应用后 unresolved 必须为零；新命令引用已解决 ID 拒绝，原 completed key 则先返回历史 receipt。assignee 省略时保留当前值，显式值替换为当前合法 Agent；原、新 Agent 均纳锁集，但合法换人不要求原 Agent 仍 active。新增范围不接收 AddBlockers，不扩大旧 backlog 请求、standalone BlockerService 或 AgentRun 权限。

原 Store 活事务内重验当前 Owner/Session、expected version、Sprint、最终 Agent、真实 occupancy 和 pending/task/source/target group 门。以当前 Task 为 preimage 保留标题等无关字段，进入当前 todo 同 priority 组尾部；解除、Task version、rank/group/query generation、typed history、一个 Outbox、completed receipt 与 activity 同事务提交。每个解除记录真实 Human resolver 和统一时间，`resolution_comment` 为 null；整体 `Comment` 只写独立 Task history。technical Blocker 原 Scheduler creator、metadata、Work failure 事实和旧 failed Dispatch 均保持，失败历史不能代替无占用证明，也不自动发起新 claim。

前向 `00048_task_unblock.sql` 增加 `resolved_transition_operation_id` 指向原 transition command，并与旧 standalone resolution parent 严格互斥；不改旧迁移或创建来源。新增 plan 仅在存在解除时保存完整 Blocker before/after 与来源，旧 backlog plan/receipt 编码保持兼容。沿原 `work.task_transitioned` schema 1 记录 state、可选 assignee、排序后的 resolutions 和独立 comment；支持读取已解决 technical 不等于允许 Human 创建 technical 或用 standalone 命令解除。

当前仅已冻结实现，尚无本批动态通过结论。最小真实验收分别由现 HTTP 的 TLS 正向/原 key 重放链，以及独立 caller Tx 在 `TransferTaskInTx` 全部写入后返回哨兵的真实 rollback/同 key 恢复链承担；`InTx` receipt 在外层 commit 前只是 tentative。原 planned intent 可保留，Unknown 仍沿原 key/digest Lookup，不自动重发。完整状态矩阵、生产绑定和 Scheduler retry 不由本切片完成。

## 2. 完整边表、Actor 与提交不变量

以下是正式边表的工程展开。H=当前 Project Owner Human；A=经真实当前 Execution/Tool authority 授权的同 Project AgentRun；R=A 且其 AgentID 等于转换前当前 reviewer。S=与持久 Scheduler/Executor cause 绑定的特定内部服务能力，不是任意 `identity.Service`。H 不需要伪装 Agent reviewer。

| from | to | 允许来源 | 同事务条件 |
| --- | --- | --- | --- |
| backlog | todo | H、A | 结果 assignee 当前合法；结果无 unresolved Blocker |
| backlog | cancelled | H、A | optional comment；默认保留原 assignee |
| todo | in_progress | Scheduler claim 专口 | 当前合法 assignee、Current Sprint、无 unresolved、无冲突 pending、真实 slot/额度及 Dispatch 同事务 |
| todo | blocked | H、A、限定 S | 结果至少一个 unresolved Blocker |
| todo | cancelled | H、A | optional comment；默认保留原 assignee |
| in_progress | todo | 对应 claim 的 AgentBusy 补偿专口 | 原 Dispatch、ClaimGuard 和持久逻辑位置匹配；不覆盖并发编辑 |
| in_progress | in_review | H、A | **显式** reviewer assignee、必填 comment；当前 Agent 事实有效 |
| in_progress | blocked | H、A、限定 S | 结果至少一个 unresolved Blocker |
| in_progress | cancelled | H、A | optional comment；默认保留原 assignee |
| in_review | done | H、R | 必填 review comment；默认保留 reviewer |
| in_review | todo | H、R | **显式** next assignee、必填 review comment、结果无 unresolved |
| in_review | blocked | H、R、限定 S | 必填 review comment；结果至少一个 unresolved |
| in_review | cancelled | H、A | optional comment；默认保留原 assignee |
| blocked | todo | H、A、Scheduler reconciliation 专口 | 全部 Blocker 已解决、结果 assignee 当前合法；Scheduler 不选人 |
| blocked | cancelled | H、A | optional comment；默认保留原 assignee/Blocker 历史 |
| done / cancelled | 任意 | 无 | `TASK_TERMINAL_IMMUTABLE`；独立 comment 属另一个命令 |

七乘七组合全部由纯决策测试枚举。包括 `from==to` 在内的未列边均拒绝 `INVALID_STATE`，不把空转换当 no-op，不借目标 state 绕过 Scheduler-only 边。合法历史 replay 不再次执行边检查。Human/Agent 的公共 DTO 即使填入 `in_progress`，也不能取得 claim 权限；`in_progress→todo` 不能成为普通“退回待办”。

合法 active 结果 `todo/in_progress/in_review` 必须有当前同 Project Agent；`blocked` 可以保留空 assignee，但至少一个 unresolved Blocker。存在 Blocker 不反推 state 必须 blocked；目标 todo 必须没有 unresolved。done 唯一入口是 in_review；终态禁止改业务内容和 Blocker 结构。取消不自动 resolve 原 Blocker，也不宣称满足其他 Task 的 rely_on。目标 done 的依赖满足只供后续 Scheduler reconciliation 读取，不在本 Tx 递归变更其他 Task。

A 的一般写权限来自正式 Tool/Execution 授权，不凭 AgentID 存在推断，也不凭同 Project 自动授权。Reviewer 检查另比较 **当前** assignee；旧 Dispatch/Execution 捕获的 Agent 不是当前 reviewer 的替代。其他 A 边不额外发明“只有当前 assignee 才可操作”的规则，仍受授权能力与资源 scope 收紧。运行身份真实端口与终态历史可见权限见 §4、§13。

## 3. 请求、结果与语义 presence

新增 `TaskTransitionCommandID`、`TaskBlockerID` 仅分别对应真实流转命令和 Blocker 身份，复用 Foundation typed UUIDv7；Agent、Task、TaskEvent、Project 等沿既有 marker。跨事件的 operation/correlation 使用流转命令身份；不把 transport RequestID 写成 operation。BlockerID 的唯一定义归 Blocker 契约子结果，本卡只引用，不能各造同名 marker。

以下完整请求/接口是 §10 的 **T0b** 目标，须先有 B0-C 已接受且可编译的唯一 `TaskBlockerID/TaskBlockerCreate` 与 typed metadata 契约。B0-C 的唯一类型及 rely_on、无引用 waiting_for_human 两类 metadata 已实现并独立接受，其余三类仍为 DEPENDENCY_UNBOUND；此片段仍属于 T0b，不属于 T0a，也不能用重复 marker、`json.RawMessage` 或 `map[string]any` 临时填洞。T0a 只交付不引用 Blocker 请求/事实的状态边决策、角色判定和新 Position；纯决策的受控输入不证明任何运行依赖事实。

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

显式选人始终须当前 Agent 验证；省略则保留旧值，不能因旧 Agent 当前不可用而擅自清空。正式领域模型对终态规定“默认保留”，没有规定 incoming transition 必须禁止显式 assignee；本卡不增加按目标 state 关闭该字段的便利限制。终态冻结针对转换前已是终态的 Task，而同一合法 incoming transition 仍整体受权限与最终不变量约束。允许显式 next/reviewer 等于当前 Agent，不新增“必须两人”规则；仅值真实变化才生成 `assignee_changed`。这按 Timeline§10“state + assignee 变化”及§17 worker→reviewer 示例解释状态机§7三事件伪码：不同Agent交接三条，同Agent交接state_changed+comment两条；字段必填与值是否变化是两种检查。

comment 省略=不创建 comment Event；存在须 1–32768 UTF-8 bytes、至少一非 Unicode 空白，允许 TAB/LF/CR，拒绝其他 Cc。保原字节、不 trim/NFC。必填 comment 四边为 `in_progress→in_review`、`in_review→done/todo/blocked`；空串或纯空白不是满足。其他合法边可有 comment，但 Scheduler claim/Busy 补偿不能使用公共字段制造 review comment。

`add_blockers`、`resolve_blocker_ids` 省略展开空数组；显式空数组同义。每个最多16项、合计最多32，重复 BlockerID 拒绝。新增项含 caller 预生成稳定 `blocker_id`，重试不换；resolve IDs 是集合，摘要和执行均按 canonical ID 排序。add 数组按 blocker_id 排序，不能通过重排输入制造另一语义。add/resolve 同 ID 冲突拒绝。组合先对未终态 preimage 验证全部 Blocker 变更，再验证结果 state 的强不变量；不额外将 add 限为目标 blocked 或将 resolve 限为 blocked→todo。例如已有 blocker 的 backlog→todo 可同 Tx resolve，in_review→blocked 可同时 resolve 旧因并 add 新因。任何新增 unresolved 导致目标 todo 不合法时整体拒绝，不能忽略输入后成功。incoming done/cancelled 不自动 resolve；明确输入仍按上述组合规则处理，提交后不再开放 Blocker mutation。

`TaskBlockerCreate` 的公共 type-specific metadata 由 §4 的 Blocker 子卡严格冻结；没有 `map[string]any` 成功后再猜类型的入口。非法字段组合返回 `INVALID_ARGUMENT`，非法目标边返回 `INVALID_STATE`。没有“清空全部 Blocker”布尔值或传入 unresolved count。

`TaskTransitionMutation` 三字段全部必有；成功的新命令一定发生 state 变化，TaskEventIDs 长1–35且唯一、EventIDs 恰一个。没有 changed=false 分支；原命令 replay 返回深复制的原 Task 快照及同一有序 IDs。上限35=state1+assignee1+comment1+Blocker变更32。内部 reconciliation 可能只变 Blocker、不变 state，须使用另外的结果/事件契约，不能伪造本结果的 state change。

Lookup request 精确为 `{project_id,command:"work.task.transfer",idempotency_key,semantic_digest}`。结果 `{status,receipt}` 沿 committed/in_progress/not_observed；仅 committed 有非 null `TaskTransitionMutation`。不复用只容纳一条历史的 `TaskCommandLookup`。公共 DTO 不接受 actor、reason_code、cause、rank、版本覆盖值或 skip_* 权限字段。

所有新增 DTO / typed payload 严格拒绝重复/未知/大小写替代 key、非法 UTF-8/孤立 surrogate、尾随值、数值冒充字符串 ID/version、非法 null；Validate、MarshalJSON 与 UnmarshalJSON 语义一致，Clone 深复制全部 pointer/slice/metadata。具体 cap 与安全日志见 §11。

## 4. 跨域能力与真实事实端口

### 4.1 当前 Agent 与引用保护：D10 所有

与 D10 Agent configuration 草案协调的拟接缝如下；两卡都须正式接受并分别验收，名称出现不表示已有实现：

```text
agent/contract.WorkReferences.RequireCurrentInTx(
  ctx, tx, actor, project_id, agent_id) -> AgentRef
AgentRef {project_id, agent_id, config_version}

work/contract.AgentReferences.HasCurrentAgentReferencesInTx(
  ctx, tx, actor, project_id, agent_id) -> bool
```

D10 的读口证明真实当前 Agent 存在、归属同 Project、初始化完成、未进入删除门禁，返回配置 version；不把 Model 引用 shape、Skills 安装回执或单个 AgentID 当事实。它不检查 busy，也不把 Runner 暂时离线、Model 调用失败当作该 Agent 消失。D10 决定当前配置何时具备可分配资格；Work 不自行添 active/disabled 枚举。当前 [D10 Agent configuration 草案](d10-agent-configuration.md) 的此口仅支持 Human：先当前Owner Read再查真实Agent，合法AgentRun暂为DEPENDENCY_UNBOUND。T2必须另补真实Run authority及Agent读取接缝，不能把Human-only口当作已支持运行身份。

选择/保留 active-state assignee 时，在最终物理 Tx 内重新调用该口；显式目标 Agent 总是验证。配置 version 变动导致旧计划失效时，重新读取真实事实并重新准备，原 Task expected_version 不变。不能只凭准备阶段 AgentRef 或当前 Get 的缓存完成分配。缺口/typed nil/错误不能表示 Agent 合法；未实现返回 `DEPENDENCY_UNBOUND`。仅在同 Tx 当前 Owner Read 已成功后，对 D10 明确的 Agent missing（含外 Project）或 `AGENT_NOT_CURRENT` 事实映射安全 `TASK_ASSIGNEE_INVALID`；不能把该调用所有 NOT_FOUND/INVALID_STATE 一概换码。授权/生命周期、依赖未绑定、Store/SQL、ctx取消错误保留各自安全语义与提交确定性，不借 assignee 错误隐藏真实失败。

共享顺序为 Project gate→ProjectSchedule EX→AgentLock SH→Sprint/Task；显式新 Agent、当前 assignee、AgentRun actor 的 Agent 取完整 union。D10 删除/失效必须取得相同 Schedule EX 与目标 Agent EX，然后调用 Work 自有引用口；该引用口明确要求 Agent EX，SH 不满足。Work 口查真实 `tasks.assignee_agent_id`，包含 backlog/blocked 及终态保留的当前字段；reviewer 是同一列，不重复维护。历史 TaskEvent 中的 from/to Agent 是历史身份，不是当前分配；当前引用存在时 D10 不能通过级联/清空 Work 字段完成删除。Work 返回 false 只证明本域当下无引用，D10 仍须检查其余引用、slot、namespace/运行资源，不得据单个 bool 宣告安全删除。缺少任何必要 provider 不能回 empty。

Work 引用口验证同 Store 活 Tx、已持 User/Project/Schedule/Agent 锁和当前授权，第一条 Work SQL 前完成 Read；不补锁、不自开事务。D10 外域 Store/Tx 不匹配必须拒绝。后续 Agent 删除规则或迁移需要清引用时，使用正式 Work 业务命令，不跨表改写。

### 4.2 Blocker：先落 Work-owned 完整子结果

目前没有 Blocker 领域表、端口或真假空集证明。先拆 B0-C 纯契约结果，唯一定义 Blocker marker、type-specific metadata、Create DTO、严格codec/Clone/限额与必要历史 payload；其接受范围须逐类写明，未冻结的外域 metadata 不用 raw JSON 占位，也不能称五类全完成。完整 TaskTransfer 编码/摘要依赖此类型闭合，不能与它无依赖并行。随后 B0-P 实现真实持久与组合端口。进入 todo、进入 blocked、从 blocked 恢复及任何 Blocker input 都依赖 B0-P；构造器不能默认 `HasUnresolved=false`。建议在本卡运行实现前独立完成以下有界成果，并在接受其卡时确定唯一字段/文件所有者：

```text
TaskBlockers.ReadInTx(ctx, tx, actor, project, task) -> BlockerSnapshot
TaskBlockers.PrepareChangesInTx(ctx, tx, actor, task_preimage, changes)
  -> BlockerChangePlan
TaskBlockers.ApplyChangesInTx(ctx, tx, actor, prepared_changes, operation)
  -> BlockerMutationFacts
```

这些是同一 Work Store 的正式内部组合口；plan 带本域 issuer/revision、完整当前 unresolved IDs/version 与要写的 facts，不能接受 caller 提供的 count。Apply 使用 caller 同 Tx，校验预像与计划；Task version、所有历史和 Outbox 由本次最外层 transition 一次统一写，Blocker participant 不再独立推进 Task version 或另开事务。基础 Blocker 子结果必须同时提供真实 persistence、严格 type-specific metadata、局部/图约束、并发保护及 TaskEvent facts，不只一个接口加成功 fake。

最先可冻结 `rely_on{related_task_id}` 与无外域引用的 `waiting_for_human{}`：related Task 同 Project、非自身，新增边与本批 resolve 后当前 unresolved 图一起判环。图在 Schedule EX 下读真实完整边集，不能只查直接相邻一跳或只查未锁缓存；project Task 上限65536，当前 unresolved 边每 Task≤256、Project≤262144，超限为安全 RESOURCE_BUSY，不截断后声称无环。相关 Task cancelled 不满足依赖，只有 done 可自动满足；普通 Human/Agent 显式 resolve 不伪装自动规则。已 resolved ID 在新的不同命令里返回 `BLOCKER_ALREADY_RESOLVED`、整次流转不提交；原命令 replay 优先，绝不再写 resolution Event。

每 Task 所有保留 Blocker 记录上限4096；历史记录不自动清理，达到上限的新 add 拒绝 `RESOURCE_BUSY/BLOCKER_HISTORY_LIMIT`，已有 resolve 可继续。每 Task unresolved≤256、Project unresolved≤262144，检查本批 resolve/add 后值；不因先增后减的执行次序误拒合法最终值。大量相关任务只在 Schedule EX 下读取图/事实，不逐个占用无界 aggregate lock；任何新增跨域引用必须由所属端口给出有界完整锁 union。

`waiting_for_human` 的可选真实 reference、`waiting_for_meeting_approval` 的 Meeting/Approval 身份、`technical` 的正式 code/cause、`user_cancelled_execution` 的 Execution/cancel actor 不用自由 JSON 占位。相关类型依赖所属域真实校验端口，缺失时该类型返回 DEPENDENCY_UNBOUND；不能改写成无引用 waiting_for_human。metadata 的跨域闭集未定部分见 §13。不能据第一种类型已实现宣称五种 Blocker 全部通过。

独立 resolve 最后一条且 Task 仍 blocked 时须 `LAST_BLOCKER_REQUIRES_TASK_TRANSITION`；本组合的 blocked→todo 在一个 Tx 中 resolve 最后 Blocker 与 state。Scheduler 自动 resolve 后仍有其他 blocker 就保持 blocked；全部消失而当前 assignee 无效时，必须在同 Tx 新增 technical/state_inconsistency 继续 blocked，不能暂时提交无 Blocker 的 blocked。该无 state change 分支属于后续 Scheduler/Blocker reconciliation，不冒充本卡 `TransferTask` 成功结果。

### 4.3 Actor、占用与内部服务能力

Human 继续使用现有 Project Authority 的真实 Session/Owner Read/Mutate；非 Owner 管理员没有项目特权。AgentRun 需要 D18/D21/D22 提供 `TaskRunAuthority.RequireInTx(ctx,tx,actor,project,task,intent)`：同 Tx 证明真实 Execution 归属、Agent、Project、被授予的 transfer-task Tool 与固定 ExecutionPolicy/资源 scope，以及本次调用/恢复的当前合法性。它不是单独 `NewAgentRun` 或客户端布尔值。ordinary mutation 拒绝已停止/取消后无合法操作入口的执行；历史查询不凭“执行非终态”替代结果可见权限，精确恢复身份接缝见 §13。

当前 Project Outbox gate 只接受 Human 的三个已验 Work 事件 triple。Human transition 新增一个精确分支；AgentRun/S 专门绑定上述真实当前 authority 后才能进入，不把原 Human `RequireOwnerInTx` 换成任意 Actor 绕过，也不能令 Service 模拟 Owner。身份未绑定的分支明确 DEPENDENCY_UNBOUND，其他未授权 Service 为 FORBIDDEN。新普通任务转换仅 active Project、planned/current Sprint；completed Sprint 不可新写。历史 completed receipt 仍可在 archiving/archived 查，deleting/未初始化遵循现有 Project gate。

Task 当前活跃 Execution、pending Dispatch 读取沿 D01 的 `WorkOccupancy` / `PendingDispatchReader`，必须是持 Schedule EX 的真实同 Tx adapter，不能绑定空数组。普通交接可以发生在当前工作 Execution 内，不能以“有 active”普遍拒绝 review；assignee 更新不改写原 Dispatch/Execution 捕获身份。cancelled 后禁止新 Launch；若已有 active/pending，需要 D22/D23 定义的持久停止/恢复组合，不能先调用外部 Cancel 再假称与 Task 原子。该闭环缺失时只阻塞涉及它的运行服务能力；纯 DTO/状态机、无占用的真实 Human 场景可各自验收，前提是无占用来自真实 provider。

Scheduler claim、Busy 补偿、保护性 blocked 与自动恢复使用独立可信内部 participant；不暴露为公共 `TransferTask` 的 reason_code 特权。D23 提供 Dispatch/cause/ClaimGuard/位置映射，D22 提供真实 slot/launch 结论。本卡不新增一个未登记 Scheduler ServiceName 就视为实现；ServiceName、issuer 和 provider 在 D22/D23 接受的组合契约中共同固定。

## 5. 公共服务与可组合事务

`NewTaskTransitions(store,deps)` 构造新服务，不替换既有 `NewTask` 或其依赖集合。基础必需依赖为同 Store Work Authority、Structure Reader、TaskReader、Outbox Appender、新 typed-event catalog、Activity；可执行的能力 profile 明确要求 Agent/Blocker/Run/Occupancy/Pending 等真实 provider。构造器验证 typed nil/Store identity/catalog，缺少 profile 必需口直接 DEPENDENCY_UNBOUND。profile 是组合根配置的可用功能集，不是请求可填写的 skip-validation；Read/Lookup 不因某个新 mutation capability 未装配而伪装 not_observed。

与 D01 `TransferTaskInTx` 的组合关系固定为：**准备在外层 Tx 之外，应用在 caller Tx 之内**。不提供“收到外部 Tx 后，内部再跑 PrepareAppend”的 raw-body wrapper。

```text
PrepareTaskTransition(ctx, actor, meta, project, task, request)
  -> TransitionPreparation {prepared?, completed_receipt?} // 恰一项
PreparedTaskTransition.RequiredLocks() -> copied complete lock union
TransferTaskInTx(ctx, caller_tx, actor, prepared)
  -> TaskTransitionMutation
```

`PreparedTaskTransition` 是当前服务 issuer 签发的 opaque 非序列化值，绑定 command identity、plan revision、当前 Actor、typed Outbox AppendPlan 和全部参与者计划；不能从 JSON/任意 struct 恢复，不能跨 Store/issuer/进程伪造复用。进程恢复用持久 command+原请求重新 Prepare。D01 的 raw 参数由 Prepare 验证并固定，InTx 不再接受另一个 target_state/assignee/body。public `TransferTask` 顺序调用这两个阶段，自己持有短 Tx；D23 等真正的外层组合者先准备全部 participant，再一次 AcquireAll 并在同物理 Tx 写 Dispatch/Task/Blocker/Event/receipt。各 participant 不私自 commit、嵌套 Tx 或发送外网。

有界 discovery 先当前 Read 和命令历史；未完成时仅发现真实 Task 所在 Sprint/group/assignee、所需 Blocker/相关 Agent/占用身份，不写计划或业务。结束 Tx 后构造完整锁集合。准备 Tx 重新授权、版本/边/全部依赖与组事实，生成 before/after、history bodies/稳定事件 IDs、最小 rank 向量和参与者计划，保存 planned；结束 Tx 后恢复同 catalog typed event，再调用 PrepareAppend。finally 新 Tx 一次取得本域与所有 append/participant 锁，重新验证 plan applicability 后才写。

discovery 失效、准备到 final 失效共用最多3个规划轮次；仅本域可证明的适用性失效允许重新发现/生成更高 revision。原 expected_version/body/原caller BlockerID 不变；版本冲突立即返回，不偷偷更新 expected。PrepareAppend 的 Forbidden/依赖错误不吞掉改成重试。重规划替换拟事件 IDs 时保留旧 revision 可拒绝性；旧 AppendPlan 不能在新 revision 下使用。耗尽为 RESOURCE_BUSY，无业务成功。

final 原子集合为：Task 指定 state/assignee/rank/version/updated_at、必要 spectator rank、source/target generation、Project query generation、Blocker mutations、全部真实 TaskEvent、唯一 typed Outbox、completed receipt、Human Account Activity。Human Activity 恰一次；Agent/S 不 touch 一个虚构 User。Task version/updated_at 在本次组合恰+1，无论产生多少 history；同一时刻用于 Task.updated_at、所有 history.created_at、Outbox.occurred_at，以及 §7 的 `task_transition_commands.committed_at` 持久列。该时刻不是 receipt wire 字段，`TaskTransitionMutation` 仍只有 §3 的三个字段。Blocker 行同 Tx 使用同一正式 operation/actor/time。

writer 先将本次 canonical/history 写入未提交 Tx，供 Outbox producer 的 NewFact 真实重验；随后 Append、完成 receipt 和 Activity，任何参与者/触 Activity/提交前取消失败全部回滚。不允许仅返回组装的 events 而未实际插入 TaskEvent。独立 pure comment 未来可以不推进 Task version，但本次必填 comment 从属于实际 state change，使用同一新 version。

## 6. TaskEvent 与 typed Outbox

### 6.1 新历史分支

新增 `TaskTransitionEvent`，通用字段仍为 `id,project_id,task_id,task_version,type,actor,operation_id,correlation_id,payload,created_at`；type 闭集 `state_changed,assignee_changed,blocker_added,blocker_resolved,comment`。新 decoder 不吞旧两类为通用 RawMessage；未来 Timeline dispatcher 按 type 精确选择旧 `TaskEvent` 或新分支，未知 type/schema 拒绝。同 operation 的新事件关联 §7 的 transition command，不要求旧 `TaskCommandID` 假装认识 transfer。

本卡先冻结 Human actor wire `{type:"human",user_id:<UserID>,source:"task_domain"}`，与旧安全身份形状兼容。Agent/System 分支使用独立 tagged union：Agent 为 `{type:"agent",agent_id:<AgentID>,execution_id:<ExecutionID>,source:"task_domain"}`；System 为 `{type:"system",service_name:<已登记服务名>,cause_id:<正式稳定cause>,source:<scheduler|executor>}`。每个分支精确字段、无不适用字段、无 Session/显示名/任意 Actor JSON；Agent/System 的持久 source/cause decoder 在真实 authority 子卡中冻结后才注册，Human 子结果不提前接受未绑定 ServiceName。

| type | payload 精确内容 |
| --- | --- |
| state_changed | `{from_state,to_state,reason_code}`；reason_code 对普通 H/A 为 null；Scheduler 使用正式闭集 reason，不能为空串或任意技术错误 |
| assignee_changed | `{from_agent_id,to_agent_id}`；两字段必有、仅前者可 null，本卡没有清空分配；真实值变化时一条 |
| blocker_resolved | `{blocker_id,blocker_type,resolution_comment}`；本 DTO 没有逐 Blocker resolution comment，写 null；不要把总 review comment 复制到每个 blocker |
| blocker_added | `{blocker_id,blocker_type}`；不复制大段 metadata/底层 Approval 对象 |
| comment | `{body}`；保存明确输入正文，不用 state_changed.reason_code 承载它 |

事实顺序固定：state_changed → assignee_changed（适用时）→ 按 BlockerID 排序的 resolved → 按 BlockerID 排序的 added → comment（存在时）。每条独立 TaskEventID、同 operation/correlation、同新 Task version/time；receipt 的有序 IDs 与此完全相等。IDs 用同一微秒下递增的 canonical UUIDv7 生成或等价严格单调生成器，保证既有 `(created_at,id)` Timeline 序与事实顺序一致，不能依赖 INSERT 顺序。correlation 先等于 operation；跨 Scheduler 原 cause 由 Actor/内部计划关联，不开放客户端 correlation 覆盖。

必填 review comment 必须是独立 comment Event；不能用一条 `fields_updated` 或一个混合 payload 代替 state/assignee/comment 的多条事实。除 comment 未来明确编辑/软删 API 外事件 append-only；本卡不引入删除/编辑路径。旧事件内容、cap、Actor 和 receipt 不回填重编码。

### 6.2 新 typed envelope 与两阶段 authority

新 triple 精确为 `(work.task_transitioned,work.task,schema_version=1)`，producer 仍同一个 `work`。新增 `TaskTransitionEvents` 同 catalog factory/restore/decoder，payload 全字段必有：

```text
TaskTransitioned {
  command_id, actor, task_event_ids[], milestone_id, sprint_id,
  from_state, to_state,
  assignee_change: null | {from_agent_id,to_agent_id},
  source_position, target_position
}
TaskTransitionPosition {
  sprint_id, state, priority, previous_id:null|TaskID,
  next_id:null|TaskID, order_generation
}
```

Position 使用全部七 state，新独立 Validate；source 描述从原组移除后原逻辑槽位的当前相邻锚点与 source 后代数，target 描述目标插入后的相邻锚点与 target 后代数。Task 自身不能作为 previous/next；两个非 null 锚点不可同一。普通转换 target 必为尾部，next=null；前一项为当前目标组尾或 null。纯 rank rebalance 后以同一逻辑邻居和最终代数为准。priority/sprint 本次不变，source/target state 必须对应 payload。payload 不携 title/plan/comment/Blocker metadata 原文；TaskEventIDs 让消费者通过授权 Timeline 读取业务事实，不在 Outbox 复制评论。

Header 精确沿 Project scope、Task aggregate ID、Task 新 version、无 aggregate_sequence、OccurredAt=本次统一时间。Producer 验证每个字段、完整事件有序列表和 payload digest；手工构造一个 schema 合法 TypedEvent 不等于合法可发布计划。

依赖 purpose 固定新域 `work.task-transition.append-v1`；binding 包含当前 Actor、Summary、完整 CommandIdentity、transition command ID/revision、全部 history IDs、CurrentAccess/NewFact 两 Stage、完整锁 canonical/mode。opaque 只含 `{kind:"task_transition",command_id,plan_revision,task_event_ids}`，不含正文/key。与原 planning issuer/purpose 分离，不修改旧 opaque。

InTx 先证明本 Store 活 Tx、own issuer/purpose/opaque、完整已持锁及当前 Project/Actor Read，再进行第一条 Work SQL；随后对真实命令重算 binding/exact locks/typed summary。CurrentAccess 允许合法 completed 历史，但只核不可变原计划、事件与当前结果可见权限，不要求今天 Task/group/Agent 仍等于旧 postimage。NewFact 必须仍 planned，当前 Mutate 成功，并在同 Tx 证明 Task、所有 Blocker/TaskEvent、source/target/query 代数和计划精确一致。缺任一 history、乱序/多一条、Actor/operation/version/time/payload/header/revision 不符均拒绝。

completed 命令对应 Outbox event 被删除时，不允许通过 NewFact 重建。Project gate 只增加明确新 triple/新 Actor 能力分支，不扩大到 `work.*`；原三个 triple 原样回归。没有新 Outbox handler、delivery、Audit 或 Inbox 自动订阅；本卡一条 typed envelope 代表一次 aggregate mutation，和内部多条 TaskEvent 并存。

## 7. 迁移与专用命令存储

新建 `agenteam_work.task_transition_commands`，不扩大旧 `task_commands.command_name` 三值闭集，不把多历史 receipt 塞进旧 `TaskMutation`。列包括 ID、Project、稳定 writer tagged identity、唯一 `(project_id,command_name,idempotency_key)`、semantic digest、规范 request、plan_revision、planned/completed、私有 plan、拟 history IDs 有序数组、唯一拟 Outbox EventID、receipt、created/committed 时间；command_name 公共仅 `work.task.transfer`。Scheduler 等专口使用它们自己已接受的 command identity，不悄悄复用 Human transfer 名。

新记录 planned 有 plan、无 receipt/committed_at；completed 有完整 receipt/committed_at；state/唯一性/正revision/字节cap/IDs唯一且数量边界/时间顺序由可表达 SQL 约束与严格持久 decoder 共同保证。所有成功 transfer 恰一个 state change、1–35 history IDs、一个 Outbox ID；没有 changed=false completed 路径。保留 completed plan 用于历史 producer 的不可变事实核对，不依赖今天 Task 状态重新构造。transition operation 与 typed OutboxEventID 各唯一；EventID 非 null 的 planned 仅是拟身份，不能证明事件已发布。

继续使用单一 `task_events` 事实表，不建第二条影子 Timeline。迁移增加 nullable `transition_operation_id` 外键到新命令 `(project_id,id)`，将旧 `operation_id` 改为可空但由分支 CHECK 保证：

1. 旧 type `task_created/fields_updated`：原 operation_id 必有、transition_operation_id 必空、原 Human actor/correlation=operation_id/payload≤8KiB 全保留。
2. 新五 type：operation_id 必空、transition_operation_id 必有、correlation=transition_operation_id，按新 actor/type/payload 分支校验；保持同 Project Task FK。
3. 两 operation 互斥且恰有一个；不允许通过两个 nullable FK 绕过 operation 存在性。新 history decoder 读取 transition_operation_id 投影成 wire `operation_id`，旧 decoder 只读取旧分支，公开字段形状不变。

此方案不搬迁旧 command、不重写旧 history Actor/payload/ID/time，不用跨两个表的多态弱引用。新 events 复用 TaskEventID 全局 PK 与现有 Timeline index；新分支可增加 `(project_id,transition_operation_id,id)` 索引。新 command 不 FK 尚未存在的拟事件，final producer 同 Tx 校验完整关联，避免 planned 插入与 history FK 循环。旧 planning 服务仍正常插入带 operation_id 的事件，数据库默认 transition_operation_id=null，无需其知道新 receipt。

本卡 comment 的 raw payload cap256KiB、record272KiB，其他新 facts payload≤8KiB/record≤16KiB；DDL 对旧 type 保留原cap，对 comment 分支精确增量，不能全表统一放大。JSONB `::text` 增加分隔空白仍纳入上界。新 actor schema 与 Human/Agent/S 阶段逐项启用，未实现分支不能先放宽 SQL 为任意 JSON object。没有跨 Agent/Executor/Scheduler schema FK；引用归属由真实端口/共享锁保护。

迁移必须 fresh/已有规划数据升级/重复执行/中途 DDL 失败整体回滚；快照旧五张 Structure 表、Project current pointer、五张 Task planning 表的所有行及时间/ID/receipt原字节语义，升级前后相等。新增约束不能使已验合法旧记录变成不可读；约束更名/替换必须在同一次事务完成，不留双 operation 可空窗口。Blocker schema 属 Blocker 子卡的独立全局迁移；顺序由 root 按真实能力分配，不改00022或抢占其他并行模块编号。

## 8. 锁、并发、rank 与 generation

锁顺序沿现有 Foundation：Command0→User1→Project2→ProjectSchedule/RankGroup3→Agent4→Sprint/Task及正式参与者Aggregate5→Outbox/record6。同级用 Normalize/CompareLockKeys 完整比较，不能按 hash 或含函数 LockKey 做等值。原始完整 union（含重复）先≤512再去重取较强模式，第一次 AcquireAll 一次取得；超限明确拒绝，不能丢锁/边写边补低序锁。

Human final 基础锁为 Command EX、User EX（Activity）、Project SH、ProjectSchedule EX、source/target精确 RankGroup EX、全部相关 Agent SH、Sprint SH、target Task EX，再并入 Blocker/真实Run/Occupancy/Pending/Outbox plans 全部锁。rank group scope 复用 `work.task:<project>:<sprint>:<state>:<priority>`。discovery 用 Command EX、User SH、Project SH、Schedule EX、Task SH 读取身份；它不在已取 Task 后补 Agent/Rank，只结束整个 Tx 后重新准备完整集合。源/目标组和 Agent 变化后旧计划必须重规划。

ProjectSchedule EX 同时保护 Task membership、query generation、Blocker 图与 pending claim 所属序列，Project EX 仍阻止父结构变更。D10 Agent删除/失效与分配用同 Agent 锁；只读配置用 SH，删除 EX。Task 活跃 Execution / pending provider 自行声明必要 Execution/Dispatch locks；在 caller 取得完整 union 后验证，不能持外域高序锁临时读取后再索要低序 Work 锁。

普通 state 变化从原组移出，按已验32位小写 hex rank、开区间与 midpoint/rebalance 算法追加目标同 priority 组尾。source/target各 `order_generation+1`，Project `query_generation+1`，Task version/updated_at只一次。priority/sprint/milestone/title/description/plan 不在本请求中，SQL 只写明确列，不能用旧全 Task 覆盖并发业务字段。spectator 纯 rank 维护保持相对顺序，不改其 version/time/history。

任一 Task/version/group/query/plan revision 达 MaxInt64，或目标组已有4096成员，整次明确失败 RESOURCE_BUSY，对 canonical/history/Blocker/Outbox/Activity 零业务改动。迁组不新增 Task，因此不对65536的 Project成员上限误判成 create，但须验证持久事实没有损坏。缺 group/query 行只在真实空集合允许逻辑初值，非空缺失是 INTERNAL_ERROR；不能补一行默认值掩盖损坏。旧分页 cursor 因本次 query代数变化全部 stale；replay/拒绝不变。

todo pending claim 的逻辑位置不能由本卡忽略：任何会改涉事组 rank/邻居的写入，在 D23 尚无真实位置保护 adapter 时不得对存在/未知 pending 的组运行。D11/D23 后继持久 mapping 至少保存 ClaimGuard 的 source Version/assignee/priority/sprint/source generation/前后锚点与受保护逻辑槽；普通 reorder/rebalance 同 Tx 更新该映射。双方锚点消失后不能猜旧 rank/尾部。Busy 只回退仍匹配 claim 的状态，保留后来 title/assignee/priority 等修改；不匹配时仅按 D23 关闭相应已知 Busy Dispatch，不覆写 Task。精确 mapping SQL 和内部补偿结果必须独立接受并真实竞争验收后开放。

Claim 的 todo→in_progress 与 pending Dispatch/history 同 Tx；slot hint busy 时不创建 Dispatch，Launch 后明确 AgentBusy 才能补偿。Launch unknown 保持原 pending、额度与 key，只 Lookup；不能因此转 blocked、恢复 todo、重发 Launch 或创建新 Dispatch。Execution succeeded/failed 也不推断 Task done/blocked。上述禁止在纯 kernel 中就测试，不等运行 Scheduler 才补。

## 9. 幂等、版本、错误顺序与 Unknown

identity 固定 `namespace=project,owner_ids=[project_id],command=work.task.transfer,key=IdempotencyKey`。digest 是 canonical-v1 精确对象 `{format:"work-task-transition-v1",command,project_id,task_id,actor_subject,expected_version,request}`；Human主体=user_id，AgentRun=project/agent/execution，正式Service=service_name+稳定cause。排除 key 本身、Session、RequestID、CSRF、deadline、服务端 command/event IDs、时刻/rank/generation。assignee/comment presence 保留；数组按 §3 展开空和排序；不 trim comment/metadata。

ExpectedVersion 必有且≥1。相同 Project/key 不同 command 独立；不同 Project同key独立。相同identity换 Task/Actor/expected/presence/目标/任何 Blocker内容/comment 为异义。当前有权 Human 换 Session 仍同一主体；换 User 不得通过摘要差异探测旧 writer。

执行顺序为：

1. ctx/admission、可信 Actor、DTO/meta/schema纯验证；构造错误/非法字段先 INVALID_ARGUMENT，未构造Actor UNAUTHENTICATED；未绑定 Actor capability 明确 DEPENDENCY_UNBOUND。
2. 本阶段完整锁后真实当前 Actor/Project Read、初始化/生命周期门禁；坏 token/digest 不压过当前 Session/结果不可见。第一条 Work SQL 前当前权限必须已通过。
3. identity 查历史：writer不符安全 NOT_FOUND；同writer异digest为 IDEMPOTENCY_KEY_REUSED；同义completed深复制原receipt，先于当前Task/version/Agent/Sprint/Blocker/容量/合法边。
4. 没有completed才 Mutate；读真实同ProjectTask和placement，当前expected不符 TASK_VERSION_CONFLICT；completedSprint TASK_SPRINT_INVALID；已有terminal TASK_TERMINAL_IMMUTABLE；未列边 INVALID_STATE；合法边但Actor不匹配 FORBIDDEN。
5. 检查必填assignee/comment、当前Agent、Blocker批次与结果invariants、真实执行/调度能力、容量/overflow；全部通过后才持久planned。

沿用已有 TaskNotFound/VersionConflict/AssigneeRequired/SprintInvalid/TerminalImmutable；新增明确 `TASK_ASSIGNEE_INVALID,TASK_UNRESOLVED_BLOCKER,TASK_BLOCKED_WITHOUT_BLOCKER,COMMENT_REQUIRED`。Blocker子卡负责 `BLOCKER_NOT_FOUND,BLOCKER_ALREADY_RESOLVED,TASK_DEPENDENCY_CYCLE,LAST_BLOCKER_REQUIRES_TASK_TRANSITION` 的唯一定义与Foundation Known/Safe保真。仅语法错误不用新业务码；数据库/JSON损坏安全 INTERNAL_ERROR，读取失败不得伪 not_observed/无blocker/无occupancy。可呈现的错误参数只含已授权 Task 的 current/target machine state与固定reason，不能输出comment、key、SQL、跨Project对象身份或原cause文本。HTTP映射由后续adapter统一，不在纯域卡先实现API。

所有新成功 transfer 是真实变更；target=current不会保存no-op completed。对原completed replay不Touch、不增版本/代数、不重放comment或Blocker；重复resolve另命令不是幂等捷径。Caller要纯comment或换人须用对应后继命令，不能伪转回相同state。

U1和取消沿已验规划规则：请求取消且明确未开始/已回滚，安全返回确定未提交；final COMMIT已经发出而结果不能确认，返回 `COMMIT_UNKNOWN`，保留原key/digest，不能声称rollback。Prepare提交不确定也要保留可查询的原命令身份，不生成replacement。Lookup committed返回原receipt；planned为in_progress；当前未见为not_observed，两者均非负向提交证明，不授权自动重发有副作用动作。DB/权限错误直接返回，不伪观察状态。无后台恢复worker；停止服务拒绝新admission，Drain等待本服务已拥有的Tx/Rows/commit join，超时不报告已收敛。真实未知提交与进程退出恢复见 §12。

## 10. 能力切分与实施顺序

| 可独立结果 | 完整验收边界 | 依赖 / 尚不能声称 |
| --- | --- | --- |
| T0a 无Blocker类型依赖的纯结果 | 49边、权限角色判定、`TaskTransitionPosition`及旧Position拒绝回归；只用现有Task/Agent等typed ID | 不含完整TaskTransfer、Blocker历史或全请求摘要；纯决策输入不证明真实Actor/Agent/Blocker能力 |
| B0-C Blocker纯契约前置 | 唯一TaskBlockerID/Create、已明确type-specific metadata、严格codec/Clone/cap、相应历史payload | 逐类冻结；未定外域metadata保持未完成，不填RawMessage、不复制marker；无真实持久能力 |
| T0b 完整transition纯契约 | 完整TaskTransfer的presence/摘要、多事实编码/cap/Clone、typed envelope与旧schema拒绝回归 | 依赖T0a及B0-C可编译类型闭合；不依赖Agent数据库，仍不证明真实授权/运行成功；Blocker类型覆盖不超过已验B0-C |
| B0-P Blocker持久基础 | 真实表/图/批次/最后Blocker保护、共享锁与事实产出 | 依赖B0-C；真metadata外域provider逐个绑定；无Provider不当empty |
| A0 D10当前Agent+引用保护 | 当前同Project事实、初始化/删除门禁、Work当前引用/删除竞争 | D10配置Create/Read/Update若仍缺Model/Skills/Tool目录则保持其阻塞；纯Agent契约不等于此结果 |
| T1 Human transition服务 | 规划整卡通过；T0b、真实A0/B0-P及必要占用/位置provider，合法Human流转、rank/History/Outbox/receipt全事务 | 不把直接SQL造todo/Agent行作为正向种子；不声称Agent Tool、Scheduler或Executor已绑定 |
| T2 AgentRun/reviewer绑定 | 真实Execution+Tool授权、当前reviewer比较、权限撤销/运行结束/恢复Lookup | 依赖D18/D21/D22；不能用NewAgentRun或成功mock替代 |
| T3 Scheduler组合 | claim/Dispatch/slot/Busy逻辑位置/unknown/reconciliation真实原子竞争 | 依赖D22/D23正式adapter及mapping；不能经公共Transfer开放Scheduler边 |
| 后继adapter/Timeline | HTTP/Tool一致服务入口、comment独立编辑软删与Timeline读取、Inbox投影 | 另卡明确范围与真实验收；不在本文自动注册 |

T0a、B0-C、A0 可按各自真实前置并行安排；B0-C类型闭合后，T0b与B0-P可以分别推进。T1所需代码可以拆有意义的可构建库结果，但不能把缺provider的运行构造器验收当作transition正向。D10独立C1纯契约即使已接受或交付，也不是A0的当前Agent事实/引用保护运行能力。确有不需要Agent/Blocker/执行能力的局部业务路径，必须逐项证明其真实前置和引用/占用边界后独立定scope；不能以一个nil adapter开启整个服务。具体文件闭集、shared gate/fault/迁移唯一作者由实施卡接受时登记，本纯规格不赋予任何代码写权。

### 10.1 T0a 文件闭集与冻结 Go API

T0a 后续实现拟新增且仅新增 `internal/central/work/contract/task_transition_rules.go`、`internal/central/work/contract/task_transition_rules_test.go`。此段接受后仍须由主线程明确授予这两个文件写域；本轮只补本文，不实现源码。开工先确认路径及下列导出名未被其他任务占用，冲突交主线程协调，不另造近义 API。复用现有 [TaskState/TaskID](../../../internal/central/work/contract/task.go)、[Foundation Fault](../../../internal/central/foundation/fault.go)、[identity.AgentID](../../../internal/central/identity/contract/identity.go)；不新建 ID marker，不 import Agent/Work service 或数据库实现。已有 [AgentRef/WorkReferences](../../../internal/central/agent/contract/reference.go) 仅作为事实责任依据，T0a 不消费或实现该端口。

以下声明属于 `work/contract`；`identity` 指既有 `identity/contract`，`fmt`、`slog` 指标准库。角色是非 wire 枚举，下面的整数值不注册为 HTTP/Tool 字段；输入只供受控 Go 调用，不是 TaskTransfer DTO。

```go
type TaskTransitionRole uint8

const (
    TaskTransitionHumanOwner TaskTransitionRole = iota + 1
    TaskTransitionAgentRun
    TaskTransitionSystemBlock
    TaskTransitionSchedulerClaim
    TaskTransitionAgentBusyCompensation
    TaskTransitionSchedulerReconcile
)

type TaskTransitionRuleInput struct {
    FromState TaskState
    ToState TaskState
    Role TaskTransitionRole
    ActorAgentID identity.AgentID
    CurrentAssigneeAgentID *identity.AgentID
}

func CheckTaskTransitionRule(in TaskTransitionRuleInput) error
func (TaskTransitionRuleInput) Format(fmt.State, rune)
func (TaskTransitionRuleInput) LogValue() slog.Value

type TaskTransitionPosition struct {
    SprintID SprintID `json:"sprint_id"`
    State TaskState `json:"state"`
    Priority TaskPriority `json:"priority"`
    PreviousID *TaskID `json:"previous_id"`
    NextID *TaskID `json:"next_id"`
    OrderGeneration int64 `json:"-"`
}

func (TaskTransitionPosition) Validate() error
func (TaskTransitionPosition) ValidateTarget(TaskID) error
func (TaskTransitionPosition) Clone() TaskTransitionPosition
func (TaskTransitionPosition) MarshalJSON() ([]byte, error)
func (*TaskTransitionPosition) UnmarshalJSON([]byte) error
func DecodeTaskTransitionPosition([]byte) (TaskTransitionPosition, error)
func (TaskTransitionPosition) Format(fmt.State, rune)
func (TaskTransitionPosition) LogValue() slog.Value
```

判定函数只读输入、无外部调用或全局可变状态，返回 nil 只表示该 state pair 对所给角色满足纯边规则；不返回 grant、prepared plan 或可用于提交的凭证。`TaskTransitionRuleInput` 不实现业务 JSON codec，不得作为公共 decoder 目标；不提供 `identity.Actor`/`ActorKind`/ServiceName 到 role 的转换器、通用 Service role 或可直接指定的 Reviewer role。两个 struct 的直接 Format/LogValue 均为固定 `work_task_transition`，容器日志限制沿 §11。

### 10.2 受控角色输入、49 组合与错误优先

角色权限完全引用 §2 的 15 边表，不增加边：HumanOwner 对应 H；AgentRun 对应 A，在 `in_review→done/todo/blocked` 上还必须 `CurrentAssigneeAgentID != nil` 且与 `ActorAgentID` 相等，才满足 R。其他 A 边不比较 assignee，避免将一般 Tool 授权缩成“仅本人任务”。SystemBlock 仅对应 `todo/in_progress/in_review→blocked` 的限定 S；SchedulerClaim 仅 `todo→in_progress`；AgentBusyCompensation 仅 `in_progress→todo`；SchedulerReconcile 仅 `blocked→todo`。这四种专用角色相互不继承，也不因某条边与 H/A 重叠而继承 H/A 的其余边；Human、Agent 即使知道目标 state 也不能取得 claim 或 Busy 边。

`ActorAgentID` 在 AgentRun 时必须是合法非零既有 typed ID，其他五种角色必须是该类型零值；`CurrentAssigneeAgentID` 可 nil，非 nil 必须合法非零。nil 只表达此纯输入未提供 assignee，不能证明数据库当前没有 assignee；本函数不验证完整 Task aggregate，因此 nil reviewer 只会使 Agent 的 R 边拒绝，不在其他角色上补造 Task 事实。H/S 等角色仍须由调用服务保证真实 preimage 有效；当前持久 active Task 缺 assignee 属损坏，不能把纯输入 nil 规则用于修复或跳过读取。

未来调用者必须先通过各自真实当前端口，再从本次事实形成角色与 reviewer 输入：H 来自当前 Session/Project Owner，A 来自 §4.3 的当前 TaskRunAuthority，current-assignee 来自同 Tx 当前 Task preimage。旧 grant、已验形状的 AgentRef、旧 Dispatch/Execution 捕获的 AgentID、同 Project 身份及 `NewAgentRun` 都不能替代这些事实。专用内部角色必须由正式 cause/claim/ClaimGuard/调度组合口形成；任意 `identity.Service` 无法因角色名称或注册成功获得权限，也不能被映射为 H。T0a 只测受控值下的规则，不声称已经阻止真实权限撤销、旧 reviewer 或伪造 Service 调用；这些运行否定由 T1/T2/T3 真实 adapter 验收。

`CheckTaskTransitionRule` 的错误顺序固定如下，所有错误为现有 `foundation.Fault` 且 `CommitState=NotStarted`，不增加 Foundation code：

1. 按 FromState、ToState、Role、ActorAgentID、CurrentAssigneeAgentID 顺序验证上述 shape；任何未知 state、零值/未知 role 或不合规则的 ID 返回 `INVALID_ARGUMENT`。字段路径依次为 `/from_state`、`/to_state`、`/role`、`/actor_agent_id`、`/current_assignee_agent_id`，字段 code 固定 `INVALID_TASK_TRANSITION_INPUT`，不附输入值。
2. shape 合法且 from 是 done/cancelled 时，全部 14 对返回 `TASK_TERMINAL_IMMUTABLE`，包括两个 terminal 自循环；terminal 优先于“未列边”和角色拒绝。
3. 其他 from 的未列 pair 返回 `INVALID_STATE`，共 20 对，包括五个非 terminal 自循环；不能返回旧 planning 专用的 `TASK_STATE_INVALID`。
4. 其余 15 对按上述角色判定；角色不匹配或 Agent 的 R 比较失败返回 `FORBIDDEN`，匹配返回 nil。此三种业务拒绝仅输出 code/state，不附 caller 值或 cause。

这里的 shape→terminal→边→角色是纯函数局部顺序，不改 §9 服务的 Actor/当前权限、历史 replay、Task version、Sprint、terminal 等顺序。真实服务中同义 completed replay 不再调用此函数。assignee/comment presence、当前 Agent、Blocker 结果、occupancy、slot、Dispatch、rank、版本/代数、容量及事务均不进入 T0a 输入；不得增加 `Authorized`、`HasUnresolved`、`AgentValid`、`NoOccupancy` 等布尔值或任意 metadata 来伪装这些已满足。

### 10.3 新 Position 的严格边界与旧 schema 隔离

新 Position 是 §6.2 的独立值类型，不是旧 [TaskPosition](../../../internal/central/work/contract/task_events.go) alias、嵌入或放宽开关。Validate 接受全部七个 canonical state、既有四 priority、合法非零 SprintID、`1..MaxInt64` 的 OrderGeneration；previous/next 可 nil，非 nil 必须是合法非零 TaskID，两个非 nil 邻居不可相同。ValidateTarget 先 Validate，再拒绝零/非法 target 或任一邻居等于 target。全部 shape/邻居错误沿 `INVALID_ARGUMENT/NotStarted`；Validate 使用固定 `/position:INVALID_POSITION`，ValidateTarget 的新增拒绝使用 `/position:SELF_NEIGHBOR`。它们不证明邻居存在、同组或当前 generation；真实组读取、普通 target 必须尾部、source/target 与事件相符仍属于后继组合。

wire 精确六个 required 字段 `sprint_id,state,priority,previous_id,next_id,order_generation`；只有 previous/next 可显式 null，省略仍拒绝。generation 通过现有 `foundation.Version` 编解码为 canonical 十进制字符串；拒绝 JSON number、0、负数、前导零、正号、指数、小数及 MaxInt64+1。复用现有严格 object helper，拒绝 unknown/duplicate/case-variant key、非法 UTF-8/孤立 surrogate、尾随值、非对象/null 及非法 ID。Validate、MarshalJSON、UnmarshalJSON 的 shape 判定一致；成功后才替换 decoder receiver，失败保留旧值，nil receiver 返回安全错误不 panic。DecodeTaskTransitionPosition 直接调用自有 codec，任何错误返回零值及错误。

复用 `MaxTaskHistoryPayloadBytes=8KiB` 作为单个 Position 的 raw 和 marshal 输出 cap；Decode 与直接 UnmarshalJSON 按整个传入 raw 计数，包括外空白。恰 cap 的合法空白填充可接受，cap+1 拒绝；标准 `json.Unmarshal` 会裁外空白，不能用它证明整个请求 cap。Clone 分别复制两个非 nil 邻居指针，修改副本不影响原值。Position JSON 保留合法业务值，fmt/slog 固定标记不改变 JSON。

旧 TaskPosition 的 Validate/Marshal/Unmarshal 仍只接受 backlog；新 Position 的非 backlog JSON 交给旧类型仍拒绝。保持 `task.go`、`task_events.go`、Foundation/identity 及现有测试源不变，不放宽旧 TaskCreate/FieldsUpdate/Reorder/CommandName/Mutation/Event/TaskChanged 的字段、type 或 history cardinality。T0a 不引入 TaskTransitionCommandID、TaskBlockerID/Create、TaskTransfer、摘要、typed event factory、Service registration、DB/迁移或服务构造器；T0b 仍须等 B0-C 的唯一 typed metadata 闭合，T1 仍须真实当前 Agent/Blocker/occupancy/Task 事实。

### 10.4 T0a 最小验收与独立否定

下列 selector 固定在新增 test 文件；预期值直接按 §2 手写，不用被测判定生成 oracle，也不复制整张 49 行表到另一份文档。作者先自测，未参与设计/实现的实例再独立核对角色升级、错误优先与旧 codec 否定，使用停止写入的两个源文件及必要既有依赖；作者的静态自查不称独立 SPEC 或产品验收。

| top selector | 必须实际证明 |
| --- | --- |
| `TestTaskTransitionRulesMatrix` | 7×7 对全部六角色；14 terminal、20 非法 pair、15 合法 pair 精确分组；Agent 在三条 R 边分别匹配/不匹配/nil，其他 A 边不同 assignee 仍按表允许；不存在第二条 done 入口 |
| `TestTaskTransitionRulesInputAndPrecedence` | 未知/大小写 state，role 0/7/255，AgentID 缺少、非 Agent 混入 AgentID、非 nil 零 current-assignee；shape 与 terminal 冲突、terminal 与角色冲突、非法边与角色冲突分别取得固定优先码；Fault Known/Safe/NotStarted，无输入值泄露 |
| `TestTaskTransitionRulesInternalRoles` | H/A 的 claim、Busy 全拒；SystemBlock 不能 claim/reconcile/cancel，三个 Scheduler/Busy 专角色不能互用或取得一般写权限；静态确认无 Actor/Service 转换器、授权布尔输入或提交能力，真实身份/provider 证明仍未执行 |
| `TestTaskTransitionPositionStrictCodec` | 七 state/四 priority、generation 1/MaxInt64、零/非法 ID、全部缺/null/duplicate/unknown/case key、损坏 UTF-8/surrogate/尾值、generation 非 canonical、raw cap/cap+1、marshal 非法值拒绝、失败不改 receiver/nil receiver |
| `TestTaskTransitionPositionNeighborsAndClone` | 空/单边/双边邻居、重复/自身邻居、非法 target；两指针独立深拷贝；直接 Format/LogValue 固定投影；并行重复纯判定/codec 使用独立或只读输入，无共享可变状态 |
| `TestTaskTransitionLegacyIsolation` | 新 Position 六个非 backlog state 全部被旧 Validate/Marshal/Unmarshal 拒绝；旧 TaskCreate/FieldsUpdate/Reorder 不接受 transition 字段，CommandName 不接受 transfer，旧 Mutation/Event/TaskChanged 不接受新多历史/type/position 字段；既有 backlog 正例继续有效 |

在后续获得源码及工具链资源授权后，使用仓库要求的 Go 1.27.1 与任务自有临时 GOCACHE/GOTMPDIR：`go test -count=1 ./internal/central/work/contract`、`go test -race -count=1 ./internal/central/work/contract`、`go vet ./internal/central/work/contract`；以 `GOTOOLCHAIN=local` 和实际选定二进制运行，不联网补依赖或占共享缓存。独立实例至少实际复跑上述新增 selector 的 pure/race 负例并检查限定 diff，记录准确命令/退出；本次文档只检查本地链接、anchor、限定差异与格式，不运行 Go/PG/network。两文件闭包若必须改 shared helper、Foundation 或旧 schema 才能通过，先升级所有权与规格，不越界补丁。

### 10.5 T0b 完整纯契约的工程闭包

**范围与文件。** T0a 与 [B0-C 两类 Blocker 契约](d11-task-blocker-contracts.md) 已实现并分别独立接受，现可冻结 T0b 的纯 Go 结果；六文件纯结果已实现并独立验收，实施结果见文末；此段不授予运行服务能力。后续拟新增且仅新增 `internal/central/work/contract/task_transition_contracts.go`、`task_transition_history.go`、`task_transition_events.go` 及各自同名 `_test.go`，共六文件；源码开工须另授写域，名称冲突先协调。不改既有 Task/T0a/B0-C/identity/Foundation/event/Outbox/Project 源或测试，不新增依赖、迁移、服务构造器、真实 producer/gate 注册。完整请求及数据结果仅支持 B0-C 已验两类 metadata，其余三类保持 `DEPENDENCY_UNBOUND`；带引用 waiting_for_human 保持 B0-C 的严格拒绝，不作降级。

**请求、回执与命令。** 采用 §3 的 TaskTransfer、TaskTransitionMutation、TaskTransitions 接口原字段/签名；接口只有声明，无成功实现。`identity`、`foundation`、`event` 指现有 contract 包，新增 API 为：

```go
type TaskTransitionCommand struct{}
type TaskTransitionCommandID = foundation.ID[TaskTransitionCommand]
type TaskTransitionCommandName string
const TaskTransitionTransfer TaskTransitionCommandName = "work.task.transfer"

type TaskTransitionLookupRequest struct {
    ProjectID ProjectID
    Command TaskTransitionCommandName
    IdempotencyKey foundation.IdempotencyKey
    SemanticDigest foundation.Digest
}
type TaskTransitionLookup struct {
    Status LookupState
    Receipt *TaskTransitionMutation
}
func TaskTransitionIdentity(ProjectID, foundation.IdempotencyKey) (foundation.CommandIdentity, error)
func TaskTransferDigest(identity.Actor, foundation.CommandMeta,
    ProjectID, TaskID, TaskTransfer) (foundation.Digest, error)
```

所有 wire key 沿 §3 精确 snake_case。TaskTransfer.Validate 只检查 canonical target、pointer/text、两数组数量、Blocker typed shape、ID 唯一与 add/resolve 不相交；不凭无 from/preimage 的请求判合法边，也不判当前 assignee/Blocker/占用。每数组0–16项、合计≤32；先核数量再逐项，重复和重叠即拒。所有已定义字段/集合的 shape 错误优先 INVALID_ARGUMENT；逐项遇到 B0-C 的 DEPENDENCY_UNBOUND 时保留该错误并继续检查其余已定义 shape，全部 shape 合法后才返回所保留的未绑定错误，不因数组排列改变两者优先级，也不检查未冻结 metadata 的业务语义。MarshalJSON 在深复制上排序两个集合，始终输出 `add_blockers:[]`、`resolve_blocker_ids:[]`；Unmarshal 也规范为非 nil 空数组及 canonical ID 排序，assignee/comment 仍只在实际存在时输出。Clone 保留 Go 的 nil/presence 与顺序，不改输入；编码排序不能改 caller slice。评论严格执行 §3 的32768-byte/非空白/控制字符规则，正文不规范化。

Mutation 三字段 required/non-null，Task必须合法且version≥2，有序 TaskEventIDs 长1–35、均合法且严格递增，EventIDs 恰一个合法ID；没有 no-op 或单历史兼容分支。Lookup request四字段 required/non-null且command只能 transfer；Lookup两字段 required，committed 必须有完整receipt，in_progress/not_observed 必须显式null receipt。Lookup标量与历史读取规则仍按 §9，纯值不产生提交观察结果。

**摘要闭集。** TaskTransitionIdentity 精确使用 §9 的 project namespace、单个 project owner ID与固定command。TaskTransferDigest 独立实现，禁止复用旧 planning 的 `taskCommandDigest`（其 format/target_id/actor_user_id不同）。错误顺序固定为：先检查Project/Task参数与CommandMeta（含必有ExpectedVersion），形状失败返回INVALID_ARGUMENT；再调用上述TaskTransfer.Validate，原样保留其Known Fault，已定义shape错误为INVALID_ARGUMENT，shape合法但含三类未绑定Blocker时为DEPENDENCY_UNBOUND，不能改码；仅请求验证成功后才检查Actor，未构造为UNAUTHENTICATED。支持已构造 Human 与 AgentRun 的稳定主体编码，但只证明身份形状。AgentRun 的主体Project必须与参数Project一致，否则 `FORBIDDEN`；任意 Service均 `FORBIDDEN`，正式Service/cause摘要分支留给T3，不由当前Service注册推导权限。不得调用或放宽旧Human-only ValidateActor来假装已绑定Agent能力；后继服务仍按§4/§9重新证明当前权限，未绑定运行入口仍为DEPENDENCY_UNBOUND。

canonical-v1 对象精确七键为 §9 所列，`actor_subject` 的 Human 分支为 `{kind:"human",user_id}`，AgentRun为 `{kind:"agent_run",project_id,agent_id,execution_id}`；不含Session。`expected_version` 使用必有且合法的 foundation.Version 十进制字符串，meta.RequestID/IdempotencyKey 仍须通过形状验证但不进摘要。`request` 使用上述规范请求 JSON：省略的 assignee/comment 仍省略，两个集合展开空并排序；再由现有 cursor.CanonicalJSON 排key并 SHA-256，返回 `sha256:<64 lowercase hex>`。任何错误返回空 Digest，不产生计划或grant。静态黄金向量：Human user UUID `00000000-0000-7000-8000-000000000001`、project尾号002、task尾号003，expected=`"1"`，请求只有 target_state=todo及展开的两个空数组，规范对象350 bytes，摘要为 `sha256:ca26dbe5269b1cb0355ab2008a5ddbf2a3f5f33cfdf2f4ea6c1e8443ae9df657`；这只是字节规格，尚非Go实测。

**独立历史分支。** Human历史的实际结构按§6.1冻结；新类型不复用旧RawMessage TaskEvent。当前Actor Go值只持UserID，codec固定生成/严格读取 `{type:"human",user_id,source:"task_domain"}`；不存在可填写的Service/Agent分支、身份转换器或授权布尔值。后继Agent/System的持久source/cause decoder仍待正式authority卡，当前均不能解码成功。

```go
type TaskTransitionActor struct { UserID identity.UserID }
type TaskTransitionEventType string
const (
    TaskTransitionStateChanged TaskTransitionEventType = "state_changed"
    TaskTransitionAssigneeChanged TaskTransitionEventType = "assignee_changed"
    TaskTransitionBlockerAdded TaskTransitionEventType = "blocker_added"
    TaskTransitionBlockerResolved TaskTransitionEventType = "blocker_resolved"
    TaskTransitionComment TaskTransitionEventType = "comment"
)
type TaskStateChangedPayload struct { FromState, ToState TaskState }
type TaskAssigneeChangedPayload struct {
    FromAgentID *identity.AgentID
    ToAgentID identity.AgentID
}
type TaskCommentPayload struct { Body string }
type TaskTransitionFactPayload struct {
    StateChanged *TaskStateChangedPayload
    AssigneeChanged *TaskAssigneeChangedPayload
    BlockerAdded *TaskBlockerAddedPayload
    BlockerResolved *TaskBlockerResolvedPayload
    Comment *TaskCommentPayload
}
type TaskTransitionEvent struct {
    ID TaskEventID
    ProjectID ProjectID
    TaskID TaskID
    TaskVersion foundation.Version
    Type TaskTransitionEventType
    Actor TaskTransitionActor
    OperationID TaskTransitionCommandID
    CorrelationID TaskTransitionCommandID
    Payload TaskTransitionFactPayload
    CreatedAt foundation.Instant
}
```

state payload wire仍为§6.1三required字段，`reason_code` 必须null，由codec固定输出；任何非null值拒绝，不预造未冻结reason枚举。该Human数据分支的from/to必须满足§2的H边，终态/同state/claim/Busy不合法；验证只核数据自洽，非法声明统一INVALID_ARGUMENT，不替代T0a/§9的运行错误顺序。assignee payload两个key required，仅from可null，to合法且与非null from不同。comment沿请求正文规则。FactPayload是Go typed union，不提供独立JSON codec；ValidateFor(TaskTransitionEventType)要求恰一个对应非nil指针，payload wire不含分支名或第二个type。

TaskTransitionEvent十个wire字段全部required/non-null，version≥2，operation/correlation为同一新command ID，Actor和CreatedAt合法；payload按type直调具体codec。Blocker payload只引用既有B0-C类型，Resolved在本transition历史分支额外要求ResolutionComment=nil。每个事件自身不证明一整批事实完整，组合顺序/关联由下述数据工厂检查。旧TaskEvent/TaskEventActor/两旧type及单历史receipt继续拒绝全部新增分支。

**封套与纯数据工厂。** 复用已验 TaskTransitionPosition；只有下面新封套进入现有 event catalog，较大的本地comment历史不注册成Outbox payload：

```go
const TaskTransitionedName event.StableName = "work.task_transitioned"
const TaskTransitionSchemaVersion uint32 = 1
type TaskTransitioned struct {
    CommandID TaskTransitionCommandID
    Actor TaskTransitionActor
    TaskEventIDs []TaskEventID
    MilestoneID MilestoneID
    SprintID SprintID
    FromState, ToState TaskState
    AssigneeChange *TaskAssigneeChangedPayload
    SourcePosition, TargetPosition TaskTransitionPosition
}
type TaskTransitionEvents struct { /* private catalog + EventType[TaskTransitioned] */ }
func RegisterTaskTransitionEvents(*event.Catalog) (TaskTransitionEvents, error)
func (TaskTransitionEvents) Valid() bool
func (TaskTransitionEvents) NewTaskTransitioned(event.Header, TaskTransitioned) (event.Event, error)
func (TaskTransitionEvents) Restore(event.Header, []byte) (event.Event, error)
func (TaskTransitionEvents) DecodeTaskTransitioned(event.Event) (TaskTransitioned, error)
func (TaskTransitionEvents) NewTaskTransitionData(h event.Header, before, after Task,
    request TaskTransfer, history []TaskTransitionEvent,
    source, target TaskTransitionPosition) (TaskTransitionMutation, event.Event, error)
```

TaskTransitioned采用§6.2全部required字段，只有assignee_change可null；IDs有序合法、严格递增且1–35，Actor只Human，from/to为H边。source/target state分别对应from/to；两Position的Sprint均等于封套Sprint、priority相同，target.NextID必须nil。New/Restore/Decode还检查Header精确triple `(work.task_transitioned,work.task,1)`、Project scope、合法AggregateID、AggregateVersion≥2、无AggregateSequence及OccurredAt；以aggregate TaskID对两Position调用ValidateTarget。仅此封套无法证明TaskEventIDs的内容或真实邻居，不能把New成功当成已完成producer事实核对。

NewTaskTransitionData是有界纯数据一致性工厂，不读取事实或自动生成ID/time。它要求合法before/after、同一Task/Project，before.Version<MaxInt64且after恰+1；除state/assignee/manual_rank/version/updated_at外所有Task字段精确不变，after.UpdatedAt=Header.OccurredAt且不早于before.UpdatedAt。Header的Project/aggregate/version匹配after；request目标、显式或保留assignee与after一致，四条必填comment和两条显式交接沿§2/§3检查。source/target分别匹配before/after的Sprint/state/priority；真实rank/邻居/代数关系仍不可由这些受控值证明。

history必须恰好是本请求的完整事实序列：首条state，其payload.FromState/ToState必须分别等于before.State/after.State；assignee仅当实际值变化时一条，其payload.FromAgentID与before.AssigneeAgentID按nil/value精确相等，payload.ToAgentID与非nil after.AssigneeAgentID的值精确相等；resolve按请求ID排序且逐项ID相等、comment=null；add按请求ID排序且逐项ID/type相等；总comment仅当请求存在时一条且正文逐字节相等。无缺项/多项/重复/乱序；同ID显式交接没有assignee事件，仍保留state+comment。全体history必须同Project/Task/new version/Actor/operation/correlation/CreatedAt，IDs严格递增，时间等于Header；command ID/Actor从这批一致数据取值。工厂从核对后的history生成封套IDs与assignee_change，使用after深复制生成receipt、EventIDs恰Header.EventID，再经NewTaskTransitioned构造event值；失败返回零Mutation和零Event。传入的before/after/history仍可能是调用者编造值，纯检查不证明它们曾存在或写入；没有Blocker count、Grant、无占用bool、AppendPlan或伪持久完成结果。

Register仅向caller提供的有效未Seal Catalog调用既有 `event.DefineEvent`，精确绑定WorkProducer、上述name/aggregate/version、`event.JSONCodec[TaskTransitioned]`及Validate；不自动Seal/注册旧事件，也不改变生产composition root。nil/zero/sealed/重复或冲突注册拒绝。Restore先查工厂与schema，再直接调用自有严格payload decoder，后走New；错误schema为SCHEMA_UNSUPPORTED。Decode先查同Catalog.Owns和WorkProducer，再event.DecodeEvent与Work Header/Position复核，成功返回深Clone，拒绝foreign/zero event。所有event生成/解码失败返回零值；其余数据不符为现有INVALID_ARGUMENT/NotStarted，B0-C缺类型保留DEPENDENCY_UNBOUND。

实际event API是 Definition/EventType/Event；Catalog issuer只证明schema来源。通用Catalog.Restore在交给codec前会canonicalize，可能压掉外空白或替换孤立surrogate，故它不提供本卡原字节边界保证；新自有Restore必须先严格检查完整raw，Decode无法还原已丢失的原字节。Event.Summary().PayloadDigest由既有event.NewEvent对canonical payload计算，不对`json.Marshal(event.Event)`的安全标记求业务摘要。Project当前gate只认识旧三个triple，新triple仍未绑定；§6.2的purpose、PlanIssuer、CurrentAccess/NewFact、锁与同Tx真实producer全部留T1，不在T0b造成功adapter或调用Appender发布。

**统一codec与cap。** TaskTransfer、Mutation、两个Lookup、Actor、三个新concrete payload、TaskTransitionEvent、TaskTransitioned均有精确 `Validate() error`、`Clone() T`、`MarshalJSON() ([]byte,error)`、`(*T) UnmarshalJSON([]byte) error`、`Format(fmt.State,rune)`、`LogValue() slog.Value`；FactPayload有`ValidateFor(TaskTransitionEventType) error`、Clone及安全投影，不单独编解码；两个新string枚举有Validate/Marshal/Unmarshal/安全投影。TaskTransitionEvents有固定Format/LogValue，不提供业务JSON恢复工厂。分别提供 `DecodeTaskTransfer`、`DecodeTaskTransitionMutation`、`DecodeTaskTransitionLookupRequest`、`DecodeTaskTransitionLookup`、`DecodeTaskTransitionActor`、`DecodeTaskStateChangedPayload`、`DecodeTaskAssigneeChangedPayload`、`DecodeTaskCommentPayload`、`DecodeTaskTransitionEvent`、`DecodeTaskTransitioned`，签名统一为 `func DecodeX([]byte) (X,error)`。

| 新增上限常量 | 值 / 对象 |
| --- | --- |
| MaxTaskTransitionNameBytes | 64，两新string枚举 |
| MaxTaskTransferBytes / MaxTaskTransitionResultBytes / MaxTaskTransitionLookupBytes | 各512KiB，request / mutation / lookup result；Lookup request复用MaxTaskLookupRequestBytes=16KiB |
| MaxTaskTransitionActorBytes | 1KiB |
| MaxTaskTransitionCommentBytes / MaxTaskCommentPayloadBytes | 32768正文 / 256KiB comment payload |
| MaxTaskTransitionEventBytes / MaxTaskTransitionCommentEventBytes | 16KiB非comment record / 272KiB comment record |
| MaxTaskTransitionedBytes | 16KiB封套；其他小payload复用MaxTaskHistoryPayloadBytes=8KiB |

raw与完整marshal输出都执行这些上限。外层在任何canonicalize/标准Unmarshal前检查自身完整raw；嵌套的每个Blocker item/metadata、Actor、Position和按type选择的payload必须保持原始token范围后调用自有codec核各自cap，不能先压空白再检查。拒绝missing/null/unknown/duplicate/case-key/非法UTF-8/孤立surrogate/尾值/数值ID或版本；nil receiver安全拒绝，失败不改receiver，所有Decode失败返回零值；Clone复制全部slice、pointer与B0-C metadata。输入数组/历史最多既定数量，不放宽既有Task的512KiB容量；最大合法Task文本、32768字节最坏escaping comment、16个最大Blocker create+16个resolve必须各自及组合能编码。沿§11的request/receipt/history预算，Factory不返回或宣称已验证4MiB持久plan。

所有新DTO/enum/factory的直接fmt/slog固定`work_task_transition`，错误只现有Known/Safe Fault/NotStarted与固定安全路径，不附正文/ID/key/原cause；enclosing容器限制仍沿§11。不新增§9的运行业务错误码，不以纯schema错误改变服务的权限/replay/版本优先。三份test文件固定以下六个top selector；T0b设计者及实现者不能充当独验。

| top selector | 独立可判定的完整目标 |
| --- | --- |
| TestTaskTransferPresenceAndSets | 五字段presence/null闭集；七target shape；assignee/comment精确保留；16/17与32上限、重复/重叠；排序不改caller；两Blocker成功、三类unbound、外域reference拒绝；完整raw及嵌套item/metadata cap |
| TestTaskTransferDigestAndLookup | 上述固定黄金摘要；每语义字段/expected/subject变化，Session/RequestID/key变化不变；集合排列与nil/[]等价、presence区分；非法meta/Actor/跨Project Agent/Service拒绝；非法meta+合法technical→INVALID_ARGUMENT，任一shape错误+合法technical不论排列均INVALID_ARGUMENT，合法technical+Human或零Actor均DEPENDENCY_UNBOUND，合法已绑定请求+零Actor→UNAUTHENTICATED；Lookup状态/receipt/command闭集与最大receipt |
| TestTaskTransitionHistoryTypedFacts | 五分支恰一typed payload、十字段严格codec、Human-only Actor及reason=null、B0-C两类小payload；version/operation/correlation、comment字节与cap、失败原子/深Clone；Agent/System与旧type拒绝 |
| TestTaskTransitionDataFactory | 完整多事实/同ID交接两事实；缺/多/乱序/重复/换type/ID/Actor/operation/time/version/Task字段/位置/Header全部拒绝；before in_progress(A)→after in_review(B)却给另一合法H边backlog→cancelled或另一合法assignee对C→D分别拒绝，from-assignee的nil/value不符亦拒；两必填assignee/四必填comment；MaxInt64边界、输入不变、零结果；不伪造当前授权/图事实 |
| TestTaskTransitionTypedEnvelopeFactory | 同catalog注册/New/Restore/Decode/Seal；foreign/zero/duplicate/wrongproducer/triple/header/sequence/自邻居拒绝；原raw16KiB/孤立surrogate先于canonicalize；canonical payload digest、clone隔离与受控并行；明确通用Catalog恢复的限制 |
| TestTaskTransitionContractBoundsAndLegacy | 所有raw cap/cap+1与同时最坏合法文本/Blocker/receipt组合、直接日志/Fault保真、nilreceiver；旧planning请求/command/TaskEvent/Human/单历史Mutation/TaskChanged严格拒绝新增输入，旧正例及T0a/B0-C继续通过 |

后续实现按Go1.27.1、离线GOTOOLCHAIN=local/GOPROXY=off/GOSUMDB=off/GOTELEMETRY=off、任务独占缓存/temp及`-p=2`执行准确selector发现、Work contract整包pure/race/vet；独立实例用未参与设计的公开API oracle覆盖摘要/多事实/嵌套raw/foreign factory/旧schema否定，实际Wait/退出后才记通过。当前仅文档链接/anchor、限定差异与字节算式静查，无Go或数据库执行；§13及B0-P/T1/T2/T3所有真实事实门槛不变。

## 11. 上限、持久编码与安全投影

新增单request≤512KiB，单mutation/lookup receipt≤512KiB，lookup request≤16KiB，typed `TaskTransitioned` payload≤16KiB；comment payload≤256KiB、comment record≤272KiB，其他history payload≤8KiB/record≤16KiB，actor≤1KiB，私有plan≤4MiB。cap检查自有codec实际收到的整个raw及marshal完整输出，不把标准json.Unmarshal裁掉的外空白算作已测覆盖。Task自身沿原512KiB，不用新较小结果cap限制其满额description/plan。

comment最坏6倍escaping为196608B，加固定封套<4KiB，低于256KiB；最大32个Blocker请求项各完整JSON≤8KiB，最多262144B，再加comment/IDs/封套<512KiB。BlockerDescription与metadata各自限额必须在B0卡把单项最坏输出证明为≤8KiB，不能仅测试短ASCII。mutation不复制comment正文，仅Task与≤35个history IDs/一个Outbox ID，沿Task最坏407552B加IDs/封套<16KiB，低于512KiB。

plan最多两个512KiB Task、source/target前后四个4096项`(id,rank)`向量共≤2MiB、32个Blocker最小before/after事实合计≤512KiB、一次comment编码≤256KiB、其他固定metadata≤128KiB，合计≤3.875MiB；不能再复制request、整组Task正文、完整dependency graph或多个comment副本。外围持久request另列保存；编码前先核规模，真实最大合法request必须能够生成plan/receipt。若B0最小fact实际超该算式，必须在接受规格时调整它的字段/限额或正式提高plan cap及SQL，不能运行时拒绝已称合法的最大输入。

全部ID是非零canonical小写UUIDv7；version/generation/revision使用1..MaxInt64的canonical十进制字符串，禁止float/JSON number。Task/Group/Project容量沿规划卡；Blocker和批次/lock上限见§3/4/8。历史TaskEvent和completed命令本卡不自动清理，不能用达到体积阈值后删历史破坏幂等。

直接新DTO的fmt Formatter/slog LogValuer使用固定`work_task_transition`，错误只Safe Fault metadata。业务JSON必须保留合法正文，不宣称JSON是脱敏日志。Go fmt遍历外层未导出字段和slog对wrapper/slice/map的JSON回退可能绕过内层安全投影，沿规划已验限制：调用者不能把任意enclosing DTO/业务JSON送日志，必须显式固定标记或批准安全字段；不把直接Format测试外推所有容器自动隐文。

## 12. 验收矩阵

纯规格验收只检查事实来源、精确闭集/权责/迁移兼容/依赖/矩阵/本地链接，不运行产品或PG，也不把文档接受标为服务PASS。实施时作者先完成自测，独立验证使用冻结输入与实际结果。真实PG按团队当前资源所有权调度；每轮记录准确selector、实际wait/退出与所拥有资源终态，不复制过时的逐轮审批或永久证据包流程。

### 12.1 纯契约与决策

下表是 T0a、B0-C、T0b 的合并目标矩阵，不能把整表归入无需前置的 T0a。T0a只执行 §10限定的边/角色/Position项；涉及TaskTransfer、BlockerID/metadata、多事实或完整摘要的项，须等相应B0-C类型闭合后由T0b验证。

| 测试组 | 必须证明 |
| --- | --- |
| DTO闭集 | 每字段缺少/null/重复/unknown/大小写、非法UTF-8/surrogate/尾值/数值ID；assignee/comment presence；空数组同义和set排序；add/resolve重复或重叠；全上下界；所有结果Clone |
| 状态矩阵 | 49对from/to逐项；全部15正式边与H/A/R/S角色；同state非法、done唯一入口、terminal不可变、Reviewer必须当前、Scheduler-only不能被普通Actor取得 |
| 评论/分配/Blocker | 四条必填comment、空白/正文保留；两条显式assignee必填；active合法Agent、blocked非空blocker/todo零blocker；原子多事实order/version；同ID显式交接成功且恰state/comment两事实，不伪造from=to变化 |
| 摘要与安全 | 每个影响语义字段/expected/actor主体变化；Session/RequestID不变；set排序等价；direct fmt/slog、Safe cause；最大同时Task文本/comment/Blocker输入→plan→receipt→Lookup，不能只单字段ASCII |
| typed兼容 | 新Position七state与严格邻居约束、旧Position非backlog仍拒；旧TaskMutation/TaskEvent/TaskChanged不接受新字段/type；新工厂catalog/issuer/header/payload版本闭集；新Fault Known/Safe |
| 有界性 | 512原始lock union、4096组、65536Project、Blocker各级限额、所有MaxInt64、plan最坏总量；发现/准备/final共3次而非嵌套9次 |

### 12.2 真实 PostgreSQL 与正式端口

| 建议 top selector | 真实正向、否定及反证 |
| --- | --- |
| `TestTaskTransitionsMigrationCompatibility` | fresh/规划数据升级/重复/DDL失败回滚；旧Structure+Project pointer+全部Task五表快照一致；两个operation FK分支/XOR、type/actor/correlation/cap/唯一key/revision/receipt约束；旧规划服务继续写读/replay，新旧Timeline同序 |
| `TestTaskTransitionsHumanWorkflow` | 真实D10服务创建至少worker/reviewer/next三Agent；真实TaskCreate→合法分配ready→正式claim进入in_progress（claim尚缺则该完整链保持未验，不能SQL替代）→不同reviewer+comment→done；另链review→todo换next、review→blocked+comment→resolve恢复；各取消入口、默认保留/显式赋值、terminal拒绝、comment/body exact与多IDorder |
| `TestTaskTransitionsActorAndHistory` | 当前Owner/非Owner管理员/撤Session/换Session/换Owner；异Project/Agent、current Agent删除门禁；completed先于当前version/Sprint completed/terminal/容量/Agent有效性，archiving/archived可查历史而planned不可新写，deleting/未初始化沿gate；同key跨command/Project独立、换writer不可见、每presence异义 |
| `TestTaskTransitionsBlockerAtomicity` | 真实Blocker新增/resolve多项、最后Blocker+state原子、已resolved新命令不重复事件；有向长链/自环/跨Project/双事务相反边防环；resolved边不参与；done才自动满足、cancelled不满足；历史保留/各容量边界；缺provider或SQL失败不能empty |
| `TestTaskTransitionsRankAndPaging` | 每个普通边迁入目标尾、 source/target各+1/query+1/Task+1；必要rebalance spectator正文/version/time不变；4096目标满拒/65536迁组可行；所有版本溢出全回滚；旧五元分页/filter cursor stale；replay不stale；与真实pending mapping并发保位置 |
| `TestTaskTransitionsAtomicityAndProducer` | 在真实Blocker后、所有history后、真实Outbox后、Activity后分别注错，Task/Blocker/groups/query/history/receipt/event/touch完整回滚；producer SQL证明确实看到全部history；缺/增/换序history、伪Typed/Actor/payload/time/version/header/issuer/revision/foreignStore/endedTx/缺锁拒绝；当前Read先于第一Work SQL；旧completed event删除不能重建；无Audit/handler |
| `TestTaskTransitionsConcurrency` | 两真实连接同expected不同key仅一业务成功；同key同义唯一Task/历史/comment；同key异义冲突；Agent删除/换归属门禁与assign竞争；current reviewer交接与旧reviewer批准竞争；Blocker last-resolve与新增竞争；targetgroup变动迫使真实replan、旧AppendPlan被拒 |
| `TestTaskTransitionsCommitUnknownAndStop` | commit前确定回滚与COMMIT发出后确定成功/Unknown分别注入；后者以原key/digest Lookup同一receipt、无重复comment；not_observed不伪负证明；进程恢复/当前Session换新后历史；Stop admission、Drain实际join、取消/Rows/commit未退役不得称完成 |

Actor/调度绑定增加真实 `TestTaskTransitionsAgentReviewerAuthority`、`TestTaskTransitionsSchedulerClaimAndBusy`、`TestTaskTransitionsPendingAndCancellation`：运行Agent身份与授权撤销/停止/历史恢复、reviewer旧快照拒绝；slot预先busy不Dispatch、claim同Tx、rareBusy保护逻辑位置及并发字段、unknown仍pending不重Launch；任务交接后原Execution身份保留、取消迟到结果不能复活、真实pending位置mapping在rebalance及两锚点消失后的恢复。它们属于T2/T3，缺D22/D23时保持未验，不能用Human top通过取代。

服务正向fixture必须来自所属域正式创建/变更端口及真实PG；允许直接SQL仅用于迁移旧快照、故意损坏、边界批量装载和否定/回滚证明，并在测试中标注其不证明业务入口。无Agent表/无Blocker表时插一行自造fixture schema不构成正式provider。纯port doubles可验证Work控制流，但必须与真实跨域PG证据分列。

回归选择覆盖旧Task planning的codec/producer/Unknown与分页、Structure migration/producer、Project当前授权/历史、Outbox atomic append/missing locks。由共享路径实际diff选择准确现有selector；不需要为纯卡片执行这些，也不把旧版本结果外推新迁移/新gate。

## 13. 精确未决项与完成条件

下列项是需要所属域/业务来源明确的局部接缝，不表示整份纯规格无法审查：

1. **AgentRun当前授权与终态历史恢复。** D18/D21/D22须明确真实TaskRunAuthority的入口、锁与持久ToolOperation/cause身份：普通新mutation要求当前有效调用，原已完成命令在Execution终态后由哪个可信恢复入口读取、如何仍检查当前Project与原writer可见性。不能以Actor构造成功或旧grant当当前权限；Human T1与纯kernel不依赖此缺失正向。
2. **Task cancel与已活动Execution/pending的停止组合。** D01固定取消不等于实际停止，架构没有给出本Task incoming cancelled与停止intent的精确共同回执。D22/D23须明确持久stop intent writer、同Tx组合/其后驱动、Lookup和unknown恢复，以及不同来源activeExecution/迟到结果。没有真实provider时不能宣称无占用或Task cancel已停止执行；无活动的真实路径与全部纯契约可独立验收。
3. **Blocker外域引用与受信System原因。** Meeting/Approval的稳定typed reference、technical reason/cause允许来源、user_cancelled_execution的执行归属和用户停止证明，分别由D24/D19/D22/D23提供。B0可先完成无外域引用waiting_for_human/rely_on及全图/批次不变量；未冻结类型拒绝DependencyUnbound，不假空metadata。
4. **D11/D23持久claim位置mapping。** 逻辑保护规则已定，精确映射表、两锚点消失时如何保留槽位、普通reorder的映射迁移与Busy并发保留字段需D23组合卡给出SQL和真实竞争证据。禁止把旧manual_rank恢复或“没有Scheduler实现”当todo组永远无pending的证明。

D10当前Agent事实与Work引用保护是明确责任依赖，不是产品未决；同Project、当前存在/初始化/删除门禁及真实锁/引用检查已经固定，不能推迟为任意stub。规划整卡、D10真实服务和必要Blocker/运行provider完成前，T1/T2/T3不报完成。T0a或类型已闭合的T0b等正式子结果可以按各自边界独立交付；接受本规格也仅代表工程设计就绪，不代表迁移、服务、HTTP/Tool、Scheduler、Agent授权或真实PG矩阵已执行。

本文只新增这张规格卡，不修改原状态机、planning/TaskPosition、产品或测试源；若独立SPEC审查确认需要调整业务来源、已有契约或跨域卡，由对应唯一作者在另一个明确范围中处理。交付记录以实际代码/测试和团队任务台账为准，不在本文追加逐轮运行日志。

### T0a 实施与验收结果

已实现 `internal/central/work/contract/task_transition_rules.go` 及相邻测试。作者 pure、root contract race（2.931s）、准确 vet 与六个新增 selector 发现实际退出0；独立公开API overlay 使用 Go1.27.1、离线 `-race -p=2`，4顶层/9子测试全部实际运行通过（1.078s，工具session9346实际exit0）。独立 oracle 检查49状态对×6角色×3当前assignee，共882组合，以及错误优先、8KiB原始输入、失败receiver保留、并行Clone和旧Position/schema隔离。两产品源与独立探针均冻结，未修改旧契约或迁移。

可复跑独立验收：[probe_test.go](../../../.agent-state/task-transition-core-recovery/probe_test.go)、[run.sh](../../../.agent-state/task-transition-core-recovery/run.sh)。以上只证明纯类型与决策；没有运行PG/Agent事实/Blocker/Executor/Scheduler，也没有授权或提交能力。完整Transfer纯契约已由T0b补齐；§13真实前置继续待实现。

### T0b 实施与验收结果

已实现§10.5限定的六个contract/test文件，覆盖完整请求与命令摘要、Human typed历史、严格封套和纯多事实数据工厂。作者最终整包pure、race、vet及六selector发现实际退出0；最大35事实、两类Blocker及最坏文本组合通过。首次新测试使用不存在的旧API导致编译失败，只修新测试后复验通过，原失败不作成功记录。

独立公开API验收使用Go1.27.1离线overlay，pure/race各6顶层、34子测试实际运行通过，vet实际退出0（外层工具session82636）。它另行构造固定摘要、前后状态/assignee逐值反例、最大组合、嵌套原始raw容量、foreign factory、Clone/日志及旧schema否定，不使用作者helper计算期望。可复跑：[probe_test.go](../../../.agent-state/task-transition-recovery/t0b-independent/probe_test.go)、[run.sh](../../../.agent-state/task-transition-recovery/t0b-independent/run.sh)。

本结果只消费B0-C已接受两类metadata，历史仅Human分支；AgentRun摘要只验证稳定主体形状。纯数据工厂不证明真实Task/Blocker/邻居或授权事实，不写DB、不发布event、不提供Grant，生产gate仍未绑定新triple。通用Catalog.Restore原字节局限保留；自有Restore先严格检查raw。T1/T2/T3与完整D11仍未完成。
