# Execution、等待与业务编排

依据：[Execution Domain](../../../architecture/agent-executor/execution-domain-model.md)、[Context](../../../architecture/agent-executor/execution-context.md)、[Lifecycle](../../../architecture/agent-executor/execution-lifecycle.md)、[Loop](../../../architecture/agent-loop/loop-runtime.md)、[Dispatch](../../../architecture/scheduler/scheduler-dispatch.md)、[Meeting Turn](../../../architecture/meeting/meeting-turn-runtime.md)、[Summary](../../../architecture/meeting/meeting-context-summary.md)。状态/生命周期由[领域文档](domain-lifecycle.md)统一，模型工具边界见[Model/Tool](model-tool.md)。

## Launch 与只读结果查询

```text
TriggerRef = task {task_id}
           | meeting {meeting_id, turn_id, contribution_id, participant_id}
ExecutionPolicy {schema_version: 1, denied_tool_ids[], allowed_resource_constraints[]}
LaunchRequest {project_id, agent_id, trigger: TriggerRef,
               purpose: "task/work"|"task/review"|"meeting/response",
               execution_policy,
               lineage?: {dispatch_id?, contribution_generation?, contribution_attempt?,
                          retry_of_execution_id?, regenerate_of_execution_id?},
               meta: CommandMeta}
Launch(ctx, actor, request) -> Result<LaunchResult>
LaunchResult = created {execution_id, status: created, replayed: false}
             | existing {execution_id, current_status, replayed: true}
LaunchLookupKey {project_id, agent_id, idempotency_key}
LookupLaunch(ctx, actor, key, expected_request_digest) -> Result<LaunchLookup>
LaunchLookup = found {execution_id, request_digest, current_status}
             | not_observed
GetExecution(ctx, actor, execution_id) -> Result<ExecutionSummary>
```

首期只注册 task/meeting Provider；其他 trigger 或 purpose 不匹配返回 `TRIGGER_UNSUPPORTED / TRIGGER_PURPOSE_INVALID`，不引入 manual/webhook 产品路径。metadata 不接受可影响语义的任意 map；trace 单独在 meta。fingerprint 固定 project/agent、完整 typed trigger、purpose、canonical execution_policy、全部 lineage，排除 request_id。scope 为 `(project_id,agent_id,idempotency_key)`，全系统同一个 Agent slot 仍按 agent_id 唯一。

Launch 先验证当前 Actor 对 scope 与原结果的可见性，再按 key/digest 重放；同键异义 `IDEMPOTENCY_KEY_REUSED`，同键同义已成功则返回原 ID，即使当前 slot 被别的执行占用或原 Execution 已终态。没有成功记录才在同 Tx 取得 Project gate、Task 来源需要的 project-schedule、Agent gate 和来源 aggregate locks，调用 Provider 校验，检查 Agent/Project 可启动及 slot，原子写 created Execution、slot 唯一事实、Launch 幂等记录。不能先插 created 再异步争 slot。

Busy 返回 `AGENT_BUSY{agent_id,active_execution_id,active_trigger,active_purpose}`，历史来源内容按权限安全投影；不创建新 Execution，不计为该新 Execution failed。其他明确拒绝为 `PROJECT_NOT_ACTIVE,AGENT_NOT_AVAILABLE,TRIGGER_REFERENCE_INVALID,TRIGGER_AGENT_MISMATCH,INVALID_STATE,DEPENDENCY_UNBOUND`。提交结果无法确认返回 `COMMIT_UNKNOWN`，附原 key/digest；调用者不生成 replacement key。

LookupLaunch 只读已提交 Launch/Execution 事实，不注册请求、不创建 Execution、不取 slot、不调用 launch。`not_observed` 只表示当前没有观察到持久成功结果，不能证明旧请求未提交/不会迟到。DB 不可用返回错误，不伪装 not_observed；digest 不同返回幂等冲突；已获授权的历史结果可在 Project 归档门禁后查询。Scheduler 的 unknown 分支只用此端口，未得到结果持续原 pending，不引入额外负向证明/强制关闭服务。

## Trigger Provider 与固定输入

```text
TriggerContextProvider
  ValidateLaunchInTx(ctx, tx, actor, LaunchRequest) -> Result<LaunchPermit>
  CaptureInputInTx(ctx, tx, execution_id, LaunchRequest) -> Result<TriggerInputRef>
  Build(ctx, execution_id, input_ref: TriggerInputRef) -> Result<TriggerContext>
LaunchPermit {provider_type, reference_digest, project_id, agent_id}
TriggerInputRef {provider_type, schema_version, input_id, digest}
TriggerContext {provider_type, schema_version, source: ExecutionSource,
                input_ref, data: validated provider DTO, prompt_components[]}
```

Provider 在 registry 中显式注册。LaunchPermit 仅当次 Tx 内有效，不是客户端 bearer grant。Launch Tx 只调用 ValidateLaunchInTx，不在其中做重型 Build；D22 在 preparing 的首次配置捕获短 Tx 中，按下节预收集的资源和全局锁序重新校验取消与来源 gate，调用 CaptureInputInTx，并与 Agent/Model/Tool/初始 Skill 引用一起持久化为 preparation input。恢复 preparing 复用该 input，不重新捕获不同正文。Build 在事务外只读取已捕获的 immutable input，不沿新的 current 指针替换。Executor 拥有通用外层与历史 source identity，provider decoder 拥有自己的版本化 data schema；不直接查 Task/Meeting 表或解释其状态机。D11 提供 Task Provider 的领域构造，D22 组合注册；D24 提供 Meeting Provider。

Task 输入固定 `WorkRef,title,description,task_version,state,assignee_agent_id,plan,unresolved_blockers[],recent_task_events[],sprint/milestone title/description,purpose`。记录 recent events 的有序 ID/内容，不全量复制历史 Runtime/Transcript；旧 version mutation 冲突后通过 read-task 获取新事实再决定新命令。

Meeting Provider 在同一短 Tx 逐项核验四个非空 UUID：Meeting 属于 Project、Turn 属于 Meeting、Contribution 属于 Turn、Participant 属于 Meeting 且与 Contribution 一致、Participant.kind=agent 且 agent_id=目标 Agent。核验调用身份、删除/取消 gate、本次 generation/attempt 的 launch intent；客户端显式传四项不等于可信。错误分别为 `TRIGGER_REFERENCE_INVALID`（归属/必填）、`TRIGGER_AGENT_MISMATCH`、`SOURCE_NOT_LAUNCHABLE`，跨项目仍按外层不可见处理。

```text
MeetingInput {input_id, meeting_id, reference_set: FrozenMeetingReference[],
              summary: {version, four_fields, covered_input_digest}?,
              messages: FrozenMessage[], timeline_cutoff}
FrozenMessage {contribution_id, generation, attempt, message_id,
               order: {occurred_at,timeline_item_id}, content_digest, content_ref}
FrozenMeetingReference {kind: task|knowledge|link|file, stable_ref, safe_label?}
```

parallel Turn 在首个 Agent 启动前的 D24 短 Tx 一次捕获上述输入，并将同 input_id 写入本批每个 Contribution 的持久 launch intent（含 generation/attempt）；sequential 为每个轮到启动的 Contribution 写自己的 intent/input，包含前序已正式发布消息。Launch Provider 校验该 intent，preparing 的 CaptureInputInTx 引用其中已固定 input_id，不能重新选择消息集合。Turn cancel/delete 与 intent 创建、Launch 校验、preparing 捕获共享 Meeting/Turn gate，先提交取消时不再启动；先提交 Launch 时取消端口按其已创建 Execution 收敛。不能只保存 timeline cutoff 后重读 mutable current_message_id/reference/summary。只固定轻量资源引用，不复制 Task/Knowledge/link/file 正文；按需读取仍验证当前权限与存在性。旧 Message immutable，历史 regenerate 只改变未来捕获的 current 选择，不热改已有输入。

## Snapshot、执行与取消

```text
ExecutionSnapshot {schema_version, execution_id, captured_at,
                   project_context, agent_config_snapshot, platform_prompt_revision,
                   trigger: TriggerContext, model: ResolvedModel,
                   tools: ExecutionTool[], execution_policy,
                   initial_skills: SkillBinding[], initial_assignment_sequence,
                   mount_metadata[], variable_metadata[], secret_lease_refs[]}
ExecutionSummary {id, project_id, agent_id, source, purpose, status, version,
                  cancel_requested_at?, waiting_reference?: WaitRef,
                  created_at, started_at?, completed_at?}
CancelExecution(ctx, actor, meta, execution_id, reason) -> Result<CancelReceipt>
CancelReceipt {execution_id, cancel_requested_at, current_status, accepted: bool}
```

created 事务提交后由 D22 可靠扫描/驱动 preparing。首次捕获先在事务外通过各域正式只读端口预收集 Agent/model/provider/ToolSpec/MCP binding/credential/Skill/来源的 ID、版本与所需锁集合；这些读是加锁计划，不是可信最终输入。随后先取 system-config 引用 guard，再按 Project、project-schedule（适用时）、Agent、已排序配置与来源 aggregate 等[全局层序](foundation.md#tx-与锁顺序)取得完整锁集合并重读。发现资源集合或版本已变化即回滚未提交捕获并重新收集，不在持有后序锁时补取前序锁。

同一捕获 Tx 只调用 `ResolveModelInTx`、`ResolveExecutionToolsInTx`、`AcquireRuntimeBindingInTx`、`AcquireCredentialLeaseInTx` 及 Agent/Skill/Trigger 的 InTx 端口，原子保存 preparation input 和各自拥有的 metadata/ref/lease；这些端口与签名见[Model/Tool](model-tool.md)。提供者不借普通 wrapper 另开 Tx，不查网络、不建 SDK/Runner/MCP 连接、不调用模型或解密外发凭据。捕获提交后才在事务外执行 Provider/对象/环境连接准备；完成后重新校验取消/gate 并原子提交不可变 Snapshot 与 running、Started Event。失败不使用另一模型/工具/空目录静默代替。Snapshot 固定 Provider 参数、Tool revision、Prompt、初始技能和稳定 credential_ref；Model Secret 值不在此固定，后续请求按该引用读取当前值。新增 Skill 走下节独立控制事实。

Cancel 先持久 cancel_requested_at、阻止新 Model/Tool/Resume 驱动，立即返回接受（HTTP 202）；重复请求返回同停止意图。协调已发模型、工具、Runner、waiting 来源与资源后，确认 Loop 不再推进才提交 cancelled/释放 slot。cancel ack、客户端断连、RPC 超时不能充当实际停止。已有 terminal 的取消返回当前 terminal、不倒改 succeeded/failed；成功提交终态与先提交 cancel 的竞争由同 gate/Execution lock 排序，cancel 已生效后迟到结果只留证不得恢复运行。

所有 Task-triggered Execution 生命周期中影响 Scheduler 额度的状态变化（含 waiting/resume/terminal）取得相同 project-schedule lock。slot 唯一性从 created 至 waiting 不释放，cancel 尚在收敛也不释放；不把 Agent busy 另存一套可漂移状态。

## 等待登记与恢复

```text
WaitRef // 导入 tool contract 的中立类型，定义见 Model/Tool 文档
WaitFact {reference: WaitRef, project_id, agent_id, execution_id,
          version, state: pending|resolved|cancelled|invalidated,
          resolved_input?: ApprovalInput|DecisionInput}
ApprovalInput {approval_request_id, decision: approved|rejected, grant_id?, fingerprint}
DecisionInput {decision_request_id, decision: answered|skipped, answer?: string}
WaitFactProvider.ReadInTx(ctx, tx, expected_execution, reference) -> Result<WaitFact>
RegisterWaitInTx(ctx, tx, execution_id, reference, checkpoint_ref)
  -> Result<waiting|already_resolved {control_input_id}|cancelled>
Resume(ctx, service_or_authorized_actor, execution_id, reference)
  -> Result<resumed|already_consumed|still_pending|not_registered|terminal>
ControlInput {id, execution_id, round_id,
              kind: approval_result|decision_result|skills_added|watchdog_notice|resume_notice,
              source_id, schema_version, payload, transcript_position}
```

WaitFactProvider 由 D19 Approval、D24 Decision 实现，登记/消费都读取持久事实并核对 Project/Agent/Execution/Operation/round。事件 payload 中的“已批准”不是恢复凭据。人工等待无自动期限；User Session 过期不让已持久 request 失效，新有效 Owner Session 可处理。Decision skipped 注入显式 skipped/null，不等于默认回答；cancelled/invalidated 不伪造人类决议。

固定无丢唤醒流程：

1. 来源模块先持久化 request 与 event；Tool/Loop 可稍后登记 waiting。
2. RegisterWait 在短 Tx 取得 Project/Agent/Execution/Operation/对应来源锁，校验取消与安全 checkpoint，调用 ReadInTx。pending 则同 Tx 保存 waiting_reference/checkpoint/status；已 resolved 则直接提交唯一 consumption/control input，无需先进入 waiting。
3. 来源 resolve 使用同序锁持久化决议与 Outbox；若 resolve 在登记之前已完成，早到 Resume 可返回 not_registered，登记仍会读取该决议。若登记先提交，后到事件/恢复扫描触发 Resume；不靠一次内存通知。
4. Resume 验证当前 reference 或已消费记录；pending 返回 still_pending；resolved 时在同 Tx 写唯一 `(execution_id,kind,request_id)` consumption、typed ControlInput、Transcript 引用与 waiting→running。运行线程只能消费已提交控制事实。
5. cancel/Project gate/Meeting 删除先赢时，不注入、不恢复；重复事件返回 already_consumed/terminal。reference 不同且无对应消费记录返回 `WAIT_REFERENCE_CONFLICT`。

Checkpoint 丢失/不兼容、Operation outcome 不可安全重建返回明确 recovery failure，不能通过重新调用有副作用工具凑齐结果。来源失效不当作等待超时。

首期 Tool Batch 在模型给定 call 顺序中建立稳定 Operation。Runtime 可并行一段已直接授权且确定只读、无冲突的调用；进入可能等待的下一操作前先 settle 已发批次，之后串行处理交互/写调用。这一调度规则保证同 Execution 同时只有一个待消费 WaitRef，剩余队列持久化，不覆盖前一个 reference、不绕过待审操作继续发工具。批准后继续同 Operation，再处理队列；全部 model-visible calls 得到对应结果后才进入下一 Model round。D18/D22 验证多批准/Decision 批次，不能用一条 waiting_reference 遗失其他待办。

## 技能变更与下一轮输入确定点

```text
SkillAssignmentChange {change_id, project_id, agent_id, assignment_id,
                       assignment_sequence, agent_config_version,
                       skill_id, fixed_revision, catalog_metadata, committed_at}
SkillAssignmentIngress.RecordInTx(ctx, tx, change)
  -> Result<queued {execution_id}|no_active_execution>
DetermineRoundInputInTx(ctx, tx, execution_id, next_round_id)
  -> Result<RoundInput>
RoundInput {round_id, execution_id, snapshot_id, transcript_through,
            compaction_ref?, skill_input_binding_id, applied_change_ids[],
            assignment_cutoff, digest}
```

D10 分配、D22 Launch/preparing 捕获、终态释放及 DetermineRoundInput 共用 Project gate、Agent gate；分配与 ingress 在同 Tx 提交。Ingress 只为 created/preparing/running/waiting 的唯一 active Execution 写 pending change 与其 captured revision/package 的受保护引用，按 `(execution_id,change_id)` 去重；引用经所属域端口建立，不能只保存通知等待将来补保护。无 active 时返回真实结论，新 Launch 会从当前配置捕获初始绑定。

preparing 在 Agent gate 内固定初始 assignment sequence S 和 bindings。分配先提交的 change 必在初始绑定或 durable pending 中；初始 capture 已覆盖的 `sequence <= S` 只标已覆盖，不重复添加。capture 后提交的新增由 ingress 保留 pending，不要求重读全部 Agent 配置。配置移除不做运行撤回。应用新增前，Executor 先验证本域已有 binding 或 pending change 的 captured identity/受保护引用，再调 Skill 的 ValidateAssignmentInTx 验 assignment 当前有效、资源 active、revision 属于该 Skill；初次新增不要求已有正式 Execution binding。失效候选标 skipped，不用旧通知恢复授权。

下一 Model Request 之前的输入确定点是 DetermineRoundInput Tx **提交**：在 gate 内读取已提交 pending changes/当前分配验证，固定 cutoff，并按 skill_id 分组选择最高且当前仍有效的 assignment_sequence 新增候选。每个 RoundInput 的有效目录是唯一 `skill_id → binding` 映射：同一 Skill 的旧分配 A 移除后新增 B，在该确定点以 B 的 captured revision 替代当前映射的 A，不能同时展示两个 binding 或因已有 skill_id 永远忽略 B；重复/更低序/失效候选不覆盖较新结果。仅移除而无有效新增时不主动清除旧上下文，后续读取仍按当前授权拒绝。

新映射、applied change IDs、唯一 skills_added ControlInput（含 replaced_assignment_id，如适用）、Transcript 事实和新 RoundInput 同事务提交；历史 Snapshot、旧 RoundInput、旧 Transcript 及其保护引用保持不变。若分配先持相同锁并提交，则此确定点必须包含有效新增；若确定点先提交，则分配进入再下一轮。Model 外发只使用已提交 RoundInput。

同一已发 Model Request/Tool Batch 共用该轮 input_binding_id，不因工具完成顺序改变集合。库升级不替换固定 revision；新增不能扩 Tool/Model/Mount/Secret。waiting 可以存 pending change，但不能因此恢复；terminal 不接受运行绑定，不复活。Checkpoint 保存初始 bindings、已应用 change 集合/位置及 round input ref；Compaction 不得丢弃结构化目录，不能只靠摘要里的自然语言恢复。

## Scheduler 协作

```text
Dispatch {id, project_id, sprint_id, task_id, agent_id,
          purpose: work|review, launch_request, launch_digest,
          idempotency_key: "scheduler_dispatch:<id>",
          status: pending|launched|failed|skipped,
          launch_outcome: not_sent|known_not_created|unknown|created,
          execution_id?, attempt_count, next_retry_at?, claim_guard?, version}
ReadSchedulerCapacityInTx(ctx, tx, project_id) -> Result<{limit, used}>
ClaimTaskInTx(ctx, tx, service_actor, task_id) -> Result<Dispatch>
AssociateLaunchInTx(ctx, tx, dispatch_id, expected_version, execution_id)
  -> Result<Dispatch>
FailDispatchInTx(ctx, tx, dispatch_id, known_failure) -> Result<Dispatch>
SkipBusyInTx(ctx, tx, dispatch_id, busy_fact) -> Result<Dispatch>
```

Claim 锁 Project gate/schedule、Agent、Task，再读最新 Task/assignee/Current Sprint/未解决 blocker/占用/额度；Scheduler 不选 assignee。todo→in_progress、TaskEvent、pending Dispatch 和幂等同事务；relaunch 不凭 Execution 终态推断 Task 应 done/blocked。Dispatch 保存固定 agent/purpose/request，不因中途换 assignee 改写原 key 语义。

额度 `used = pending Dispatch 数 + 已可靠关联 launched Dispatch 的 created/preparing/running Execution 数`，不计 waiting；Agent slot 仍计 waiting。关联 pending→launched、execution_id 和额度交接在同 schedule lock/Tx 下原子提交，不能两个独立快照相加双计；TaskExecution status 更新也取得此锁。unique partial pending Task 约束与 gate 下检查防同 Task 多派发。

首次/已确认未创建的暂时失败按有限 retry/backoff，再调用同 LaunchRequest/key；`launch_outcome=unknown` 只按原串行 traversal/recovery 节奏 LookupLaunch。查到关联；查不到/错误保持 pending/额度预占，不重发 launch 试探，不改 Task blocked，不新建 Dispatch/key，不增独立 recovery worker。Task 已 terminal、离开 group/旧 Sprint或无 Current Sprint，原 pending 仍纳入同一路径；scheduler_enabled=false 可只读核对，不能借此新 Launch 或 Task mutation，恢复后继续持久关联。

明确 AgentBusy：pending→skipped，原 todo claim 经 Work 正式补偿端口验证 ClaimGuard、当前业务事实与逻辑 rank；保留并发 title/assignee/state 等修改，不添加 technical blocker、不耗 relaunch cooldown。明确最终失败：pending→failed 与适用 Task blocker/state mutation 同 Tx，但只有仍匹配原意图才变 Task，终态/已换人不被覆盖。unknown 永不进入这两个已知分支。

## Meeting 摘要与结果发布

```text
SummaryInput {meeting_id, turn_id, expected_previous_summary_version?,
              ordered_messages: FrozenMessage[], input_digest,
              needs_initial_title: bool, model_snapshot}
PrepareSummaryInTx(ctx, tx, meeting_id, turn_id) -> Result<SummaryInput|reused>
GenerateSummary(ctx, SummaryInput) -> Result<SummaryCandidate>
SummaryCandidate {input_digest, goals: string, decisions: string,
                  unresolved: string, facts: string, initial_title?: string,
                  invocation_ids[]}
PublishSummaryInTx(ctx, tx, SummaryInput, SummaryCandidate)
  -> Result<{summary_version, turn_status: completed}|reused>
```

D24 根据 canonical Contribution current_message 选择全部当前有效 immutable Messages，以 Timeline 的 `(occurred_at,timeline_item_id)` 顺序形成 FrozenMessage[]，digest 包含有序 contribution/generation/attempt/message_id/content_digest。最后 message_id 只是导航字段；早期替换而末条不变也必须改变输入 digest。Summary 模型只消费这些消息，不复制 Turn/Approval/Decision 控制对象；四字段都是 text，不变成数组。

正常 Turn 全 Contribution terminal 后进入 finalizing，成功 summary 才 completed；skipped 不导致 partial_failure，failed/cancelled 才导致。Prepare 固定实际输入、上版 summary、首次标题标志和当前选定 summary model；同 `(meeting_id,input_digest)` 已有成功记录则复用，不重复推进 version/生成标题。Generate 在 Tx 外进行；首轮同次模型请求产出 title+四字段，服务端校验后一起发布。

Publish 锁 Project/Meeting/Turn/涉及 current message 指针的保护范围，重新计算允许输入 digest，验证 expected_previous_summary_version、finalizing、无 cancel/delete gate、标题尚未生成条件。全部成功才同 Tx 更新 summary/title、Turn completed、幂等结果和 Outbox。输入改变返回 `SUMMARY_INPUT_CHANGED`，旧候选不能发布；本次正常 finalize 重新准备合法输入并沿自身有限 policy 继续。cancel/delete 先提交则返回 `SOURCE_NOT_FINALIZABLE`，不能借重试复活。

历史已完成 Contribution 成功 regenerate/retry 替换 current_message 才令 `summary_pending_update = (current_message_digest != summary.input_digest)`。它是可重建投影/派生判断，不新增摘要 worker、timer 或重开历史 finalize；无下一正常 Turn 就保持待更新。失败/取消没有改变输入时不标脏。下一正常 finalize 重算全部当前有效消息；不重新生成已提交 title、不级联运行其他 Agent、不改已有 fixed context。Summary 失败有限重试耗尽仍 finalizing，queued Turn 不越过，不跳过、不换备用模型。

## 恢复事实与持久边界

D22 从 Execution/不可变 Snapshot、Canonical Transcript、Checkpoint、Operation/Attempt、WaitFact、Skill bindings 恢复；D23 从 Dispatch 和 Task 当前事实恢复；D24 从 Turn/Contribution/Message/Decision/Summary input identity 恢复。Runtime View/Inbox/Timeline 和普通日志均不能驱动业务恢复。

恢复执行前校验 schema decoder、引用/lease、取消/gate、Transcript committed position、Operation outcome；只恢复已证实可安全的位置。created/preparing 可按原 ID 继续构造；waiting 读取源事实后保持等待或消费一次；running 丢失未提交流内容不能重新执行 outcome unknown 的工具。详细 checkpoint 内部字段/压缩算法/恢复失败分类在 D22 开工规格落实，但这些共同端口不能被运行内存或成功 stub 取代。
