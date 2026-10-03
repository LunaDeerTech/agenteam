# 持久事实、事件与实时读取

依据：[Domain Events](../../../architecture/platform-infrastructure/internal-domain-events.md)、[Runtime View](../../../architecture/agent-executor/runtime-view.md)、[Realtime](../../../architecture/platform-infrastructure/realtime.md)、[Transcript](../../../architecture/agent-loop/transcript-context.md)、[Inbox](../../../architecture/platform-infrastructure/human-inbox.md)、[Meeting Timeline](../../../architecture/meeting/meeting-timeline-realtime.md)、[Audit](../../../architecture/security-governance/audit.md)。

## 恢复事实源与投影

| 数据 | owner / 作用 | 恢复读取 |
| --- | --- | --- |
| Task / Sprint / Blocker | D11 工作状态 | 当前 canonical + 领域版本；不从 Execution failed 推断 Task failed |
| TaskEvent | D11 面向业务的评论、流转、证据时间线 | TaskEvent 自身；不复制逐 token/Tool 日志 |
| Domain Event / Outbox / Delivery | 各域发布事实，D06 可靠通知 | 重投失败 delivery；不是 Event Store，不回放全部历史重建业务 |
| Audit | D04 安全敏感行为与安全结果 | 专用 Audit 查询；不是全量工具记录或业务状态机 |
| Canonical Transcript | D22 模型语义消息、ToolResult、typed control、compaction 引用 | Loop 恢复与可追溯输入；不从 UI 文本反推模型对话 |
| Execution Snapshot / Checkpoint | D22 固定启动输入与安全恢复位置 | schema/version、committed Transcript position、Operation/Wait/Skill bindings |
| Operation / Attempt / Invocation | D18/D09 实际工具与模型尝试 | 幂等、unknown、结果、usage，不依赖普通日志猜测 |
| RuntimeItem Snapshot | D22 用户可见执行投影 | UI 恢复；不能作为 Loop checkpoint |
| MeetingMessage/Contribution/Summary | D24 消息事实、当前选择、摘要覆盖输入 | Meeting Context/编排；Timeline 只负责阅读排序投影 |
| Meeting Timeline / Human Inbox | D24/D25 read model | 从 canonical source 重建；操作回来源 API，不改投影冒充审批 |
| Realtime frame | D25 单 Central 内存 fan-out | transient，丢失后 Snapshot/resync，无持久 delta history |

## Domain Event 与投递

```text
DomainEvent<T> {event_id, event_type, schema_version: int, occurred_at,
                project_id?, aggregate_type, aggregate_id,
                aggregate_version?, aggregate_sequence?, payload: T}
AppendEventInTx(ctx, tx, DomainEvent) -> error
RegisterHandler(name, supported_event_versions, handler) -> error
Handle(ctx, event) -> Result<ack|retry {fault}|dead_letter {fault}>
Delivery {event_id, handler_name, state: pending|processing|succeeded|failed|dead_letter,
          attempt_count, next_attempt_at?, last_safe_error?}
```

每个 event ID 全局唯一，业务变更与 Outbox 同 Tx；生产者为同 aggregate 提供 aggregate_version 或单调 aggregate_sequence（至少其一），让 handler 识别相对顺序。同事务多个事件以独立 event_id/类型区分，可共享新 aggregate_version；需要区分其先后时补 aggregate_sequence，不能仅按 version 去重丢掉 sibling event。D06 unique `(event_id,handler_name)`；handler 的业务副作用与 processed marker 在同本域 Tx 原子提交。未知 schema 不 ack，当作可诊断 failed/dead_letter。

不同 handler 独立 retry，不保证跨 handler 的完成顺序，也不保证不同 aggregate 的全局顺序；这不取消同 aggregate 的相对顺序约束。handler 必须按 version/sequence 比较或重读 canonical 后带版本保护写投影，迟到的 v42 不得覆盖已应用 v43；有连续性要求而发生缺口时按该来源重新读取/收敛，不能猜测缺失变化。重复与同版本不同事件仍以 event_id 区分处理；全局时间戳不作可靠排序。

最小跨模块事件 payload 目录如下；schema_version 初始 1，生产者拥有类型，实际迁移随各域增加：

| event type / producer | 必需 payload（envelope 已有 ID/version/scope 不重复） | consumer |
| --- | --- | --- |
| `task.state_changed` / D11 | from_state,to_state,reason,actor,related_task_event_ids | D25 展示；Scheduler 仍串行查事实，不靠事件驱动独立调度 |
| `task.blocker_changed` / D11 | blocker_id,change,dependency_task_id?,related_task_event_ids | D25、领域依赖收敛 |
| `agent_execution.started` / D22 | agent_id,source,purpose | D24/D25 |
| `agent_execution.succeeded/failed/cancelled` / D22 | agent_id,source,terminal_status,completed_at,safe_result_ref? | D24 Contribution 与 waiting_for_agent 唤醒、D25；D23 重读当前事实 |
| `approval.requested` / D19 | request_id,agent_id,execution_id,operation_id,source,blocking=true | D25 Inbox/Timeline |
| `approval.resolved/cancelled/invalidated` / D19 | request_id,operation_id,source,status,reason?,decision_version | D22 读取 WaitFact、D25 收敛 |
| `meeting.decision_requested/resolved/cancelled` / D24 | decision_id,四项 Meeting 身份,execution_id,status,decision_version | D22、D25 |
| `meeting.contribution_changed` / D24 | contribution_id,generation,attempt,current_message_id?,change,source_execution_id? | D24 Timeline、D25；摘要过期由实际输入判断 |
| `meeting.proposed/turn_changed/summary_changed` / D24 | source ID,状态/摘要版本、input_digest（按事件类型） | D25 |
| `skill.assignment_added/removed` / D10 | agent_id,assignment_id,assignment_sequence,skill_id,fixed_revision?,change_id | D25 配置投影；不替代 D22 同 Tx ingress |
| `knowledge.content_changed/deleted` / D12 | document_id,content_version,change,object_ref? | D13 索引、D25；不携正文 |
| `memory.revisions_changed` / D14 | agent_id,changed_memory_revision_refs[],retain_operation_id | D13/D25；无 raw retain/Secret |

更多领域事件由所属模块定义，不为每个内部函数强制造事件。Projection 消费旧/重复事件时读当前来源，不能把旧 payload 写成新状态。数据载荷含表达已发生变化的最小事实，不能只给 ID 再猜历史 from/to；展示正文仍按权限另读。

晚注册新投影按明确 bootstrap：D06 在短事务注册 durable handler，保证此后新提交事件进入 delivery，再开始 canonical rebuild。实现使用独立 Outbox 注册屏障：Append 在分配内部 outbox sequence 前取得 shared 屏障直到提交；注册取得 exclusive 屏障、保存当前提交边界及 handler 后释放，不在此事务读取业务表。这样不存在“sequence 已分配但稍后提交”落在旧边界而漏消费。该内部 sequence 不是浏览器全局 cursor，不改变 Outbox 非 Event Store。

D25/D24 按模块扫描 canonical 到新 projection generation；bootstrap 期间 handler 收集 dirty source IDs，baseline 完成后重新读取所有 dirty keys，再原子切换到正常处理（期间新增 dirty 不可丢）。删除/权限失效也进入 dirty，不能旧 baseline 覆盖新删除。延迟 delivery 在正常处理继续读当前 canonical；不需回放注册前全部历史。bootstrap 中断重跑该模块扫描/dirty 集合，不能把半张投影标 ready。无自动 Outbox/Delivery/Dispatch/Audit TTL；Project 显式删除清理受控项目记录及 delivery，并确保迟到 handler 不重建已删除投影。

Project archived 禁止新业务工作，但已提交事件的 delivery/processed marker、Runtime 最终投影和 Inbox/Timeline 从 canonical 的收敛可用明确注册的 Service cause + converge intent 执行；作用域限原来源与对应投影，不修改 Task/Meeting 等业务内容，不 Launch/Resume/调用 Model/Tool。不能把这类必要内部写入统一当业务 mutation 拒绝，也不能由任意 Service 自报 converge 绕过门禁。来源永久删除后按生命周期清理语义处理，不能创建项目残留。

## Runtime 类型与端口

```text
RuntimeProgress {epoch: ID<RuntimeEpoch>, update_seq: int64 /* 0 起始 */}
RuntimeItem {item_id, execution_id, item_seq: Sequence, item_revision: Revision,
             type: text|reasoning|tool|interaction|notice|error,
             status: streaming|running|waiting|completed|failed|cancelled,
             started_at, updated_at, completed_at?, duration_ms?, source_ref?, safe_data}
WindowSpec = tail {limit: int, item_types?: enum[]}
           | items {item_ids: ID<RuntimeItem>[]}
RuntimeSubscription {subscription_id, execution_id, session_id,
                     epoch, window_digest, ack_progress: RuntimeProgress}
SubscribeRuntime(ctx, Human, execution_id, window) -> Result<RuntimeSubscription>
ReadRuntimeSnapshot(ctx, Human, subscription_id) -> Result<RuntimeSnapshot>
ReadRuntimeHistory(ctx, Human, execution_id, before_item_seq?, page)
  -> Result<Page<RuntimeItem>>
RuntimeSnapshot {execution: SafeExecutionSummary, window: WindowSpec,
                 items: RuntimeItem[], included_progress: RuntimeProgress,
                 window_digest, terminal_complete: bool}
RuntimeUpdate {subscription_id, execution_id, progress: RuntimeProgress,
               window_digest, kind, payload}
```

item_seq 仅在创建时分配，用于稳定排序/历史分页；同 item 的 delta/status/finish 不改 item_seq。每 Execution 协调器分配独立 update_seq，所有可见 summary/item 更新推进它；item_revision 每次该 item 内容/状态变化推进。epoch 为本次 Runtime 协调器重建的 UUIDv7，进程重启或协调器重建必须换 epoch，不能把新 update_seq=1 当旧帧继续。

上述 Runtime 类型/端口由 execution contract 拥有，D25 view/gateway 消费并包入 transport envelope，不使 D22 反向导入 View。活跃 Execution 的 ReadRuntimeHistory 同样通过 coordinator 读取所请求页面的当前内容（合并 dirty state），但历史 Page 不提供覆盖整个 Execution 的续接水位。

RuntimeUpdate payload 使用以下带类型形状：

- `item_upsert{item}`：完整受控条目，含 revision。
- `text_append{item_id,item_seq,base_revision,new_revision,start_utf8,end_utf8,text}`：仅 text/允许的 reasoning 投影；offset 是 UTF-8 byte position，adapter/client 按此验证后追加，不把 JS UTF-16 length 当字节数。
- `item_replace{item}`：状态/结果等结构化变化采用完整条目替换，避免任意 JSON patch 歧义。
- `summary_replace{execution}`：状态/wait/cancel 等可见变更。
- `window_replace{items}`：tail 成员进入/移出时给完整当前窗口。
- `progress_only{changed_item_ids[]}`：当前窗口外的变化，仅推进本订阅已观察的进度；不宣称窗口外内容已加载。
- `resync_required{reason}`：overflow、gap、epoch/window 不一致等；不是可跳过的普通增量。

所有帧仅展示经过安全投影的内容。Provider 原始私有 reasoning、credential、Signed URL、完整敏感工具 payload 不因 `reasoning` item 或 detail API 获准暴露。Tool detail 经 D18/D19 按 Owner/Project 重新授权后加载，不能从日志抄原异常。

## subscribe-first 与未 flush 间隙

D22 提供每 Execution 的单进程串行 Runtime coordinator；它按序更新内存当前投影、分配 update_seq，再通知 D25 fan-out，并批量/节流保存 Item Snapshot。D25 不自行生成第二套 Runtime 序号。数据库 Snapshot 是该投影的持久基线，未 flush dirty state 同样属于此 coordinator 的当前读取来源。

可进入 live 的读取算法固定为：

1. Gateway 当前 Session/Owner 授权后，将订阅注册送入 coordinator。coordinator 安装 bounded buffer，在序列 K 的屏障返回 ack；WebSocket writer 先发送 ack 再发送该订阅后续帧。
2. 客户端收到 ack 后持续缓冲帧，携带 subscription_id 请求 Snapshot。服务端验证 Session/Execution/window/epoch 并向**同一 coordinator** 提交读屏障。
3. coordinator 在序列 S（S≥K）复制当时 summary 与窗口完整 Item 状态；从持久基线和当前 dirty item 合并，所有订阅前已发布而尚未 flush 的内容必须在返回快照中。返回的 included_progress 恰为实际复制状态的 S，不是独立读取的“最新广播号”。
4. 客户端替换该窗口基线，丢弃同 epoch 且 ≤S 的已覆盖缓冲帧，只按 S+1 以后连续处理。缺 item/base_revision/offset、seq gap、window digest 或 epoch 不匹配时停止追加，重新订阅/读 Snapshot，不猜字符串拼接。

不能实现协调器屏障读取、dirty state 缺失或持久基线加载失败时返回 `RUNTIME_SNAPSHOT_UNAVAILABLE`/resync_required；不能返回旧 DB 内容加新水位。独立 GET 一个节流落后的 DB Snapshot 不满足活跃 Execution 的读取端口。该算法不要求每 token 写 DB，不新增 durable delta log，也不依赖浏览器保存全部历史增量。

订阅/读取 barrier 只保护内存投影和短复制，不等待 Provider/Runner 或持数据库业务锁；后台 flush 捕获 snapshot revision/水位并条件写入，旧 flush 不能覆盖较新的持久快照。并发 Snapshot 与 flush 的正确性来自相同内容版本，不来自墙钟时间。

## 分页、过滤、终态与重启

WindowSpec 及 scope 参与 window_digest，改变筛选/页窗口必须重订阅并取新 Snapshot。每个订阅都收到 Execution 的连续进度：窗口内实际 payload、窗口外 progress_only，因此不会因过滤把合法进度误判为丢帧。progress_only 只更新已观察水位，不能给未返回条目标最新 revision；客户端加载了历史 item 且要持续观看时将它加入 items window，重新取完整基线。历史分页只按 item_seq 返回当前条目，不能和 live window 的水位混为一体。

tail 窗口新增/移出成员用 window_replace，包含完整最新成员状态和同一进度；单 item 的既有较小 item_seq 仍按独立 update_seq 更新。对重复帧 `seq <= consumed` 忽略；乱序可短缓冲但不能跨缺口提交，达到 bounded 阈值走 resync。不能仅按 item_seq 去重 delta，也不能把一个 filtered page 水位声称为整个 Execution 所有内容的完整快照。

终态发布由 D22 先形成完整最终投影并在短 DB Tx 与 Execution terminal、最终 RuntimeItem snapshots/持久水位共同提交，之后才发布终态帧/terminal_complete=true。内存协调器按该已提交最终内容收敛；DB commit unknown 时查询原终态/版本，不提早报告完整。不存在“Execution terminal 已读到，因此未 flush 的最终文本自然已可读”的假设。非活跃的终态读取可走持久快照，但必须校验 terminal_complete 标记与最终水位覆盖。

重启断开所有订阅、更换 epoch，客户端清除旧缓冲重新取 Snapshot。Runtime 从持久 Item snapshots 及安全 canonical 结果重建；无法恢复的未提交 partial 明确标中断/Recovery Notice，不把丢失字节宣称已包含，也不重放不安全工具副作用。Loop 仍从 Checkpoint/Transcript 恢复。首次新 epoch 水位对应重建后的真实内容；旧 epoch 帧永不混入。D22/D25 必须用故障注入证明这些边界。

## WebSocket、Timeline 与 Inbox

```text
RealtimeEnvelope {protocol_version: 1, subscription_id, event_type,
                   project_id?, resource_type, resource_id?, occurred_at, payload}
SubscribeRequest {client_subscription_id,
                  resource: execution_runtime {execution_id,window} |
                            meeting_timeline {meeting_id} |
                            my_inbox {filters}}
ReadMyInbox(ctx, Human, filters, page) -> Result<Page<InboxItem>>
InboxItem {id, project_id, source_type, source_id, blocking: bool,
           state: open|resolved|dismissed, safe_summary, source_action_ref, occurred_at}
```

D25 用当前 Session 建连，每次 subscribe 解析真实资源归属并验证 Owner，my_inbox 绑定当前 user_id 聚合本人所有项目，不接受替换 owner 参数；系统管理员不读他人待办。filter/cursor 按[基础契约](foundation.md)绑定；排序/类型优先级采用既定 Inbox 规则，全部页面通过 source_action_ref 调用同一 Approval/Decision/Meeting API。

Timeline/Inbox 没有可比较的更新 revision 时，将事件只当 invalidation。subscribe ack→开始缓冲→读取 authoritative read model→合并 dirty source IDs→再次读取 dirty 范围后进入 live；不把早于快照的事件 payload 覆盖最新状态。新消息流式执行部分仍嵌入独立 Runtime subscription，不能靠 Timeline refresh 替代已定 streaming。

每连接/订阅 buffer 有界；慢消费者必须 resync/断开，不能阻塞 Loop、Domain Tx、Scheduler 或 Event Dispatcher。初期不静默丢 sequenced frame 以 coalesce；合并仅发生在生产者分配可见 update_seq 之前，或用完整 window_replace 加明示覆盖边界。阈值由 D25 规格/负载验证确定。

Session 撤销、资源删除/Project 不可见时关闭相关订阅与 Tunnel，可发 `resource.deleted/access_lost` 安全提示；即使通知丢失，后续 HTTP/重连也重新校验。来源取消/失效使 mandatory Inbox open→resolved，不自动 dismiss、不物理抹去正常历史；Project/Meeting 显式删除按其矩阵处理投影。任何动作都重新验证来源当前状态，不能凭过时 Inbox 卡片直接修改业务结果。
