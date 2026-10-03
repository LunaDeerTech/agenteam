# 领域身份、状态与生命周期

依据：[Project](../../../architecture/project-work-management/README.md)、[Task](../../../architecture/project-work-management/task-domain-model.md)、[Task 状态机](../../../architecture/project-work-management/task-state-machine.md)、[Sprint](../../../architecture/project-work-management/sprint-lifecycle.md)、[Execution 生命周期](../../../architecture/agent-executor/execution-lifecycle.md)、[Meeting](../../../architecture/meeting/meeting-domain-model.md)。共同类型与锁见[基础契约](foundation.md)。

## 身份投影

```text
ProjectRef {id, owner_user_id, name, normalized_name, version,
            lifecycle: active|archiving|archived|deleting,
            current_sprint_id?: ID<Sprint>}
WorkRef {project_id, milestone_id, sprint_id, task_id}
AgentRef {project_id, agent_id, config_version}
ExecutionSource = Task {project_id, task_id}
                | Meeting {project_id, meeting_id, turn_id,
                           contribution_id, participant_id}
KnowledgeRef {project_id, document_id, content_version?}
MemoryRef {project_id, agent_id, memory_id, revision?}
ArtifactRef {project_id, artifact_id}
ApprovalRef {project_id, agent_id, execution_id, operation_id, request_id}
HistorySource {kind, stable_id, safe_label?, source_deleted: bool}
```

持久化外键/约束必须验证同 Project；单个对象传递 project_id 不证明归属。Task 始终属于 Sprint，Sprint 属于 Milestone，Milestone 属于 Project；Task 冗余 milestone_id 时由 Work 同事务维护一致。Execution 的来源历史由 Executor 保存 typed ID，不能依赖尚未删除的 Task/Meeting 行才能展示。四项 Meeting 身份的可执行校验见[Trigger](execution-orchestration.md#trigger-provider-与固定输入)。

`ResolveProjectPath(ctx, Human, username, project_name) -> Result<ProjectRef>` 由 D07 用户名解析与 D08 Project 解析组合。名字规范化由 D07/D08 唯一服务实现，Project 名大小写不敏感、标准路由小写；改变名字不增加别名。每次路径解析后仍校验 Owner，历史后台任务永远按 ID。

## 状态与原子转换所有者

| 对象与 owner | canonical 状态 | 允许转换与原子事实 |
| --- | --- | --- |
| Project / D08 | `active,archiving,archived,deleting` | active→archiving→archived→active；active/archived→deleting→物理删除。gate、version、生命周期进度同事务；archiving 与 deleting 互斥，先完成/恢复既有命令再申请另一动作 |
| Milestone / D11 | 无生命周期 | 只保存结构/排序；不得添 planned/completed 状态 |
| Sprint / D11 | 投影 `planned,current,completed` | planned→current→completed；由 Project.current_sprint_id 与 started/completed 时间推导，禁止冗余 status。start/complete/rollover 与 Project pointer、Task membership 同事务 |
| Task / D11 | `backlog,todo,in_progress,in_review,blocked,done,cancelled` | 合法边见下表；mutation/version/TaskEvent/Outbox/幂等同事务 |
| Dispatch / D23 | `pending,launched,failed,skipped` | pending→三终态；unknown 保持 pending，AgentBusy 才 skipped；状态/额度/Task 合法补偿同事务 |
| Execution / D22 | `created,preparing,running,waiting,succeeded,failed,cancelled` | created→preparing→running；running↔waiting；非终态可 failed/cancelled，running 可 succeeded；终态不可复活。状态/version/slot/控制事实/事件同事务 |
| ToolOperation / D18 | `created,authorizing,waiting_for_approval,ready,running,succeeded,failed,cancelled` | 验参/授权后 ready→running→终态；等待决议后 ready 或 failed；取消待实际停止确认；终态 unknown error 保存在 failed result，不新增可自动重跑状态 |
| ToolAttempt / D18 | `created,running,succeeded,failed,cancelled,unknown` | Backend 发出前 created，实际发出 running，之后唯一终态；每次真实重试新增独立实体 |
| ApprovalRequest / D19 | `pending,approved,rejected,cancelled,invalidated` | pending→唯一终态；approved/rejected 仅有效 Owner 决议；cancelled 为显式执行/用户停止；invalidated 为来源/资源生命周期失效，均不等于 expired |
| Approval grant / D19 | `active,revoked` | active→revoked；one-time 另要求原 Operation 非终态+同 fingerprint，不设 consume/自动 expiry |
| Meeting / D24 | `proposed,active,archive` | 按领域批准、归档/恢复；hard delete 是另一个有持久进度的命令，不增加 deleting 主状态 |
| MeetingTurn / D24 | `queued,running,finalizing,completed,cancelled` | queued→running→finalizing→completed；前三态在停止确认后 cancelled。result 为 null/success/partial_failure，摘要失败保持 finalizing |
| Contribution / D24 | `pending,waiting_for_agent,running,completed,failed,cancelled,skipped` | pending→running/waiting_for_agent/cancelled；waiting_for_agent→running/skipped/cancelled；running→completed/failed/cancelled；显式 retry/regenerate 新 attempt/generation 可使 failed/completed→running；稳定 Contribution 不换 ID |
| Decision / D24 | `pending,answered,skipped,cancelled` | pending→唯一终态；skip 是用户明确决定，cancelled 是取消来源；无 expiry |
| StoredObject / D05 | `pending,available,failed,deleted` | pending→available/failed；无有效引用且明确清理后 deleted；payload 未可读不能标 available |
| Knowledge / D12 | `active,deleted` | 删除 tombstone 立即撤销 serving；indexing_status 是 pending/processing/ready/failed 投影 |
| Inbox / D25 | `open,resolved,dismissed` | 来源事实驱动 resolved，只有允许忽略的类型可 dismiss；Approval/Decision 不能 dismiss |

`cancelling` 仅 UI phase，由 `cancel_requested_at != null && nonterminal` 推导，不能冒充已停止。Tool/Attempt 的未知副作用与 Execution 状态分别记录；允许 Execution 停止继续驱动，但资源生命周期如果仍不能确认活动已停，仍返回 pending，不能据此宣称 Project 已物理清理。

Task 允许边固定如下，未列边全部 `INVALID_STATE`：

| from | to | 条件 |
| --- | --- | --- |
| backlog | todo / cancelled | todo 要合法 assignee 且无 unresolved blocker |
| todo | in_progress | 仅 Scheduler claim，合法 assignee、Current Sprint、无 blocker |
| todo | blocked / cancelled | blocked 要至少一个 unresolved blocker |
| in_progress | in_review | reviewer assignee 与 comment 同事务必填 |
| in_progress | todo | 仅对应 claim 的 AgentBusy 补偿，保留并发编辑与原逻辑排序 |
| in_progress | blocked / cancelled | blocked 要 unresolved blocker |
| in_review | done / todo / blocked / cancelled | done 要 review comment；todo 要 next assignee/comment/无 blocker；blocked 要 comment/blocker |
| blocked | todo / cancelled | todo 要全部 blocker resolved 且合法 assignee |
| done / cancelled | 无 | 业务内容冻结；仍按权限追加/编辑/软删 comment，不 reopen |

## Work 命令与占用端口

```text
TransferTaskInTx(ctx, tx, actor, meta, task_id, target_state,
                 assignee_agent_id?, comment?, add_blockers[], resolve_blocker_ids[])
  -> Result<TaskMutation>
MoveTaskInTx(ctx, tx, actor, meta, task_id, target_sprint_id) -> Result<TaskMutation>
CompleteSprintInTx(ctx, tx, actor, meta, sprint_id, rollover_target_sprint_id?)
  -> Result<SprintCompletion>
TaskMutation {task_id, version, state, event_ids[]}
SprintCompletion {sprint_id, version, moved_task_ids[], current_sprint_id: null}

WorkOccupancy.ReadInTx(ctx, tx, project_id, task_ids[]) -> Result<ExecutionOccupancy>
ExecutionOccupancy {active: [{task_id, execution_id, agent_id, status}],
                    history_task_ids: ID<Task>[]}
PendingDispatchReader.ReadInTx(ctx, tx, project_id, task_ids[]) -> Result<DispatchOccupancy>
DispatchOccupancy {pending: [{task_id, dispatch_id, agent_id, sprint_id}],
                   history_task_ids: ID<Task>[]}
AgentSlot.ReadInTx(ctx, tx, project_id, agent_id) -> Result<SlotState>
SlotState = free | occupied {execution_id, status, cancel_requested_at?}
```

这些是强一致查询，在调用方持有 Project gate、`project-schedule` lock 时读最新事实；不能用缓存/Realtime/最终一致投影表示无占用。`task_ids` 由 Work 在同事务从指定 Sprint 查询，不相信任意客户端清单；新 Task 创建、移动、Task Launch 和 Dispatch 创建都取得同一调度锁，避免 phantom。Provider 校验 project_id，缺少 adapter/读取失败不能返回 empty。

Agent slot 由 D22 保证 `(agent_id)` 最多一个 `created/preparing/running/waiting` Execution 的数据库唯一性。Slot 读只是提示，Launch 必须再次原子 claim；waiting 和取消未完成均 busy，Resume 不重新竞争。D11 的 Sprint Complete 检查所有 Task Trigger active Execution，不只 Scheduler 创建的执行。

Complete 校验当前 Sprint、无 active/pending；未完成 Task 保持 state rollover 到同项目 planned Sprint，done/cancelled 留原 Sprint，不自动启动目标。Move 的来源 completed、目标 completed、跨 Project 或 Task terminal 均拒绝；Current Sprint 移出有 active/pending 拒绝。Delete Task 同时要求纯 backlog、无任何 Execution/Dispatch 历史、无 blocker/dependency、除创建外无业务 TaskEvent；不能用“当前无占用”替代“从未有历史”。

rank 由 Work 拥有，Task/Scheduler 使用相同排序：`sprint_id + state + priority` 分组内 rank/ID。所有 rank mutation 与 rebalance 共享 group lock；rebalance 保持相对顺序，不改 Task.version/updated_at 或生成业务 TaskEvent，但推进 cursor 排序 generation。普通更新只写明确字段，不回写读出的旧 rank/parent。

claim 保存 `ClaimGuard{task_id, claimed_version, source_state, source_assignee_id, source_priority, source_sprint_id, source_order_generation, predecessor_id?, successor_id?}`；补偿重新锁 Task，只撤回仍匹配原 claim 的状态，不覆盖用户后改字段/assignee。用当前顺序中仍存在的前后锚点恢复原逻辑位置；为避免双方锚点丢失含义不明，D11/D23 在 pending claim 存续期间保护该组中的 claim 逻辑位置（维护可重排 rank，但必须更新持久 claim 位置映射），不能直接写旧 rank。精确 SQL/映射表属 D11/D23，逻辑位置不丢失是契约门槛。

## Project 生命周期端口

```text
BeginArchive(ctx, Human, meta, project_id) -> Result<LifecycleOperation>
RestoreProject(ctx, Human, meta, project_id) -> Result<ProjectRef>
BeginDeleteProject(ctx, Human, meta, project_id,
                   confirmation: {normalized_current_path, permanent: true})
  -> Result<LifecycleOperation|ProjectDeletionReceipt>
GetLifecycle(ctx, Human, project_id, operation_id)
  -> Result<LifecycleOperation|ProjectDeletionReceipt>
LifecycleOperation {id, project_id, action: archive|delete,
                    state: accepted|stopping|cleaning|completed|failed,
                    required_participants[], completed_participants[],
                    pending_resources[], fault?, version}
ProjectDeletionReceipt {operation_id, deleted_project_id, original_owner_user_id,
                        command_key_hash, request_digest,
                        status: completed, completed_at}
```

Owner/Session/expected_version 和当前完整路径在持有 Project exclusive gate 的同事务检查；接受即持久化门禁和 operation/key，不因 HTTP 断连丢失。archiving/deleting 拒绝普通 mutation/Launch/Resume/分配，但允许经过内部身份校验的停止、决议失效、事件收敛、清理和生命周期状态查询。archived 对业务只读，禁止新业务 mutation、Launch、Resume、Model/Tool 工作；Restore 不复活旧执行。`failed` 保留 gate/已完成进度，由同 operation 的重试继续；不自动回到 active。必要活动停止在进入 archived 前完成；已经提交事实的 Outbox/delivery marker、Runtime 最终状态和 Inbox/Timeline canonical 投影仍可由限定 Service cause 内部收敛，不能复活执行、修改业务内容或借此派生新工作。这些收敛写入仍校验原 Project/来源身份与幂等，来源永久删除后不得重建残留项目数据。

```text
ProjectLifecycleParticipant // 每个数据拥有者各实现一个
  Name() -> string
  RequestStop(ctx, LifecycleCause, ScopeRef) -> Result<StopReport>
  InspectStop(ctx, LifecycleCause, ScopeRef) -> Result<StopReport>
  Cleanup(ctx, LifecycleCause, ScopeRef, checkpoint?) -> Result<CleanupReport>
ScopeRef = Project {project_id} | Meeting {project_id, meeting_id}
LifecycleCause {operation_id, action, project_version}
StopReport {state: stopped|pending|failed, active_refs[], unknown_refs[], fault?}
CleanupReport {state: completed|pending|failed, checkpoint?, remaining_refs[], fault?}
```

接口幂等键为 `(operation_id, participant, phase, resource_id)`；调用/检查不持有长 Tx。参与者读取已持久 gate，验证内部 cause，自己提交本域状态与事件。数据库字段相邻不授权直接跨表清理。负责人按持久 required participant 清单推进，必要端口未绑定即明确 failure/pending；只在所有 required stop=stopped 后 archive 完成或 delete 进入 cleaning。清理可能分块，各块 checkpoint 持久化；所有必要物理清理确认后删除 Project 主记录、释放名称。名称在 archiving/archived/deleting/failed 全部占用，清理始终按原 ID。

required participants 随已启用的正式模块装配确定；D28 必须包括 Work、Agent/Skills/Variables、Knowledge/Retrieval/Memory、Executor、Scheduler、Meeting、Governance、Artifact/Object、Runner managed resources/Tunnel、MCP Project connections、Usage、Audit、Outbox/Delivery、Views。单模块隔离验收不宣称已经完成全项目清理。

最终清理事务在删除 Project/释放名称的同时持久最小 ProjectDeletionReceipt。它不含名称、路径、正文、项目Audit副本或可恢复资源，仅保留上列原命令完成事实，不依赖 Project live 外键；不是软删除或额外删除平台。202 的 status_url 按 operation_id 定位，最终响应丢失后原 Owner 的当前有效 Session 仍可查询 completed；原 delete key/digest 重放仅返回此 receipt。command_key_hash 用于 `(deleted_project_id,original_owner_user_id,key)` 的安全定位，不保原业务参数。其他用户/系统管理员无该项目权限时不能查该 receipt，同 key异义仍冲突。旧项目名称已可重用，receipt 不占名称，后台不得借 receipt 对新同名项目执行清理。

## Meeting 删除与取消

`DeleteMeeting(ctx, Human, meta, meeting_id) -> Result<MeetingDeleteOperation>`；D24 持久化删除 gate/进度，Project gate 保持正常上层顺序。Meeting 主状态仍是 proposed/active/archive；所有新 Turn、retry/regenerate、Launch validation 和 Summary 发布都检查本 Meeting 删除 gate。

先取消该 Meeting 的 queued/running/finalizing Turn、未启动 Contribution、pending Decision/Approval，以及属于此会议的非终态 Execution。按稳定来源 ID 筛选，不能停止相同 Agent 的无关 Task。调用上面的 ScopeRef.Meeting 停止端口，确认停止后清理本域及 Timeline。失败/unknown 保持 operation pending/failed、gate 不释放。Meeting archive 仅改变会话可见性，不套用 Project 自动停止策略。

取消/删除与迟到 Summary、Execution finished、Approval resolve 必须在相同 gate/aggregate locks 下重验；终态/已删来源不发布消息、不恢复 Execution、不派生 Turn。已完成消息在普通取消中保留；Meeting hard delete 才清本域。终态 Execution/Approval/Audit 仍按外域规则保留安全来源关联。

## 删除与保留矩阵

| 删除/变更对象 | 必须经正式端口处理的引用 | 完成后保留 / 清理 |
| --- | --- | --- |
| Project archive / restore | 停止所有既有活动；门禁阻止新活动；Restore 仅恢复可用性 | 所有项目数据与 Project Audit 保留；旧执行不重启 |
| Project permanent delete | 上述全体参与者 stop/cleanup；Owner 路径确认；未知 Runner 清理不得成功 | 清所有 Project-scoped 数据、Audit、Outbox/delivery、Usage、对象引用/专属 payload、托管资源及投影；仅保最小命令完成receipt供原Owner查结果；系统 Audit 保留，不保留项目 Audit 副本 |
| Meeting hard delete | 停本会议活动与 finalize；失效等待；释放本域对象引用 | 清 Meeting 本域/Timeline；Execution/Tool/Governance/Usage/Audit 历史保留 source_deleted，原 Project 删除时再清 |
| Task/Sprint/Milestone | 仅领域允许的草稿/无引用删除；Sprint 完成保护按上节；不得递归删历史工作 | Task 正常结束走 done/cancelled；不新增通用软删或终态 reopen |
| Knowledge subtree | D12 确认范围锁；全部 tombstone 同事务；立即从 serving 排除 | Meeting 引用保留 deleted 标记；派生索引和当前 payload 事务后清理，不保留内容历史供业务读取 |
| Agent 配置资源移除 | D10 正式配置/引用校验；Skill 移除见下一行；Agent 删除内部规则在 D10 固定 | 当前执行快照不热改；任何资源删除须处理 slot/namespace/运行引用，不能用级联使未停止工作失去事实 |
| Skill remove assignment / delete library item | 移除配置不取消执行；库删除使当前资源无效；Add Skills 禁止单独删除 | 不改历史 snapshot/transcript；旧 revision 对象有运行/历史引用时保留，后续读/准备必须拒绝失效授权；Project 删除统一清理 |
| ModelConfig delete | Agent.model_ref、Project summary model、平台 selector 在正式 Tx 中替换且校验能力/reasoning effort | live FK 可空，ResolvedModel/Usage 身份快照保留；运行 Secret lease 保护材料；不静默选替代模型 |
| Provider delete | 必须已无 ModelConfig；Secret lifecycle 独立 | 不级联删 Models/Usage；运行 lease 完成后才能清相关凭据 |
| MCP Config delete / Project disconnect | D20 live 目录/Connection 删除或停用，Agent 长期 Tool reference 不因暂时离线移除 | retained runtime shadow/credential lease 供已启动执行；释放后清专属材料；共享 Project Secret 不被 disconnect 删除 |
| Approval revoke / source invalidation | D19 active grant→revoked；pending request cancelled/invalidated；检查当前调用与恢复 | 无自动 expiry；保留安全历史，不能以 revoked/invalidated 假造用户拒绝 |
| Artifact / StoredObject | 业务 owner 撤引用，D05 检查所有有效引用/leases，外部 payload 删除需确认 | 非所属业务引用未解除拒绝物理删除；未引用对象可按显式恢复/清理流程处理 |
| Runner/Mount/Tunnel | D15–D17 按业务身份停止操作/关闭代理，清明确归属的托管文件 | 不递归清用户共享挂载，不承诺外部副作用回滚；离线/unknown 留进度 |

尚未在本阶段定义的 Agent 删除具体 UI/字段、非当前 Sprint 草稿层级删除 API 等归责任模块开工规格；共同规则已固定为查询真实引用、阻止不安全删除、保留明确历史、按正式生命周期端口收敛，不接受默认“没有引用”。首期 Outbox/delivery、Dispatch、Audit 无自动过期/清理，只有明确领域永久删除执行矩阵中的授权清理。
