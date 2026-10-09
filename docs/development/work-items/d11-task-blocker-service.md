# D11 B0-P：Human backlog Blocker 持久服务

状态：工程规格已独立 SPEC 接受；真实服务实现候选已形成，整卡验收未完成。本卡是 [B0-C](d11-task-blocker-contracts.md) 两类契约的真实服务消费者，不表示 Task transition、完整 D11 或生产 root 已完成。迁移 `00023_task_blockers.sql` 已实现并随候选保存。

## 1. 完整结果与真实依赖

Human Owner 可对正式 Task Planning 创建的、未指派的 backlog Task 新增 `rely_on` 或无引用 `waiting_for_human` Blocker、解除 Blocker、读取当前及保留历史，并在响应丢失后通过原命令身份确认结果。每次新增或解除在同一物理事务提交 Task version、Blocker、TaskEvent、typed Outbox、Account Activity 和幂等 receipt。依赖图取真实完整当前边，不使用空 provider。

依据：[Blocker 架构](../../architecture/project-work-management/task-blocker-dependency.md) §8–15、21–23 明确允许 backlog 预存 Blocker及独立 add/resolve；[Task transitions](d11-task-transitions.md) §4.2 固定图、容量与后继组合职责。Task Planning、Work Structure、Account/Project Authority、Foundation/Store、Outbox 已有接受子能力；B0-C 与 T0b已验纯能力可按本卡精确复用。

本卡新写只接受 `state=backlog && assignee_agent_id=null`，且所属 Sprint 是 planned/current。terminal Task 返回 `TASK_TERMINAL_IMMUTABLE`；其他 state 或已有 assignee 返回 `DEPENDENCY_UNBOUND`，不得为它们构造成功占用或 Agent 事实。当前 backlog 的直接 Blocker管理不 Launch、不变更 state/assignee/rank、不要求 Executor/Dispatch 占用为空。后继状态/指派写入仍须真实服务与共同 Schedule gate；缺少这些服务不会妨碍本卡真实未指派 backlog 正向。

新写不支持其余三类 Blocker、外域 waiting reference、自动 done recovery、Task transfer、AgentRun/Service、HTTP/UI/App root、Inbox projection 或生命周期清理。D10 F1仍缺真实 Model 引用/目录/Skills 初始化，T1仍缺Agent与occupancy。Object join/OpenAI tools/SPA/Jina/Image停止边界保持。B0-P完成仅表示本卡 backlog子结果完成，完整跨状态组合须后继扩展且再验，不能将本卡范围外路径默认为成功。

## 2. 写入所有权与工程闭包

本卡负责人独占下列路径；同一文件只由其指定执行者写入。root 独占 tasks/current/Git。新增未列共享路径先协调，不靠修改旧decoder扩大能力。

- 规格：本文。
- 新公共源码及对应 `_test.go`：`internal/central/work/contract/task_blocker_record.go`、`task_blocker_commands.go`、`task_blocker_events.go`。
- 新服务源码及对应 `_test.go`：`internal/central/work/blocker_service.go`、`blocker_repository.go`、`blocker_commands.go`、`blocker_reader.go`、`blocker_events.go`。
- 既有接线最小修改：`internal/central/work/events.go`、`internal/central/project/work_event_authority.go`及二者对应测试；`internal/central/foundation/fault.go`及对应测试。Work `Authority`沿原 issuer 分流新精确 triple。
- 新迁移：`db/migrations/00023_task_blockers.sql`。
- 新真实测试：`tests/work/task_blocker_persistence_test.go`、`task_blocker_authority_test.go`、`task_blocker_concurrency_test.go`、`task_blocker_unknown_test.go`、`task_blocker_interop_test.go`、`task_blocker_atomicity_test.go`。共享旧 fixture不改；必要本卡fixture helper在这些文件内。
- 独立验证输入：`.agent-state/task-blocker-service/`下独立验证者自己的可执行 probe；普通构建/log放 `output/ai/task-blocker-service/`。

不修改旧 Task Planning/Transition/B0-C DTO、旧 TaskEvent type/decoder、旧 planning command闭集及生产装配。新服务复用同包已接受 Store/Authority/Reader及typed scalar/strict helper；不复制事务驱动、Foundation ID marker、User/Project权限规则。相关旧边界若必须改动先说明真实影响，不直接扩域。

## 3. 公共数据与命令

新增 `TaskBlockerCommand` 唯一marker及 `TaskBlockerCommandID = foundation.ID[TaskBlockerCommand]`。BlockerID沿B0-C唯一marker；TaskEventID沿现有Work。所有公共struct具有 Validate、Clone、严格 MarshalJSON/UnmarshalJSON及固定安全 fmt/slog投影 `work_task_blocker`；每个Decode直接检查完整 supplied raw，nil receiver返回安全错误，失败不改接收者。逐层拒绝未知/重复/大小写替代key、非法UTF-8/孤立surrogate、尾随值、scalar/null形状错误；不能仅依赖DisallowUnknownFields。

```text
TaskBlocker {
  id, project_id, task_id, type, description, metadata,
  created_at, created_by,
  resolved_at: Instant|null, resolved_by: TaskEventActor|null,
  resolution_comment: string|null
}
TaskBlockerResolve {blocker_id, resolution_comment: string|null}
TaskBlockerMutation {task: Task, blocker: TaskBlocker,
                     task_event_id: TaskEventID, event_ids: EventID[1]}
TaskBlockerCommandLookupRequest {
  project_id, command, idempotency_key, semantic_digest
}
TaskBlockerCommandLookup {status: committed|in_progress|not_observed,
                         receipt: TaskBlockerMutation|null}
```

全部列出的wire key必有，只有注明null者可null。TaskBlocker type/description/metadata严格消费B0-C两个分支，不扩其未绑定type。CreatedBy/ResolvedBy使用既有Human `TaskEventActor`（human/user_id/task_domain）；不接收原始Actor或Session。未解除三resolved字段均null；已解除at/by同时非null且at≥created_at，comment可null，comment复用B0-C非空且≤1024字节规则。blocker核心字段创建后不可编辑。Blocker完整record≤16KiB；resolve≤8KiB；receipt≤512KiB；Lookup request≤16KiB。最坏description/comment escaping都须实际证明可编解码；Task正文沿既有上限。

```go
type TaskBlockerCommands interface {
    AddTaskBlocker(context.Context, identity.Actor, foundation.CommandMeta,
        ProjectID, TaskID, TaskBlockerCreate) (TaskBlockerMutation, error)
    ResolveTaskBlocker(context.Context, identity.Actor, foundation.CommandMeta,
        ProjectID, TaskID, TaskBlockerResolve) (TaskBlockerMutation, error)
    LookupTaskBlockerCommand(context.Context, identity.Actor,
        TaskBlockerCommandLookupRequest) (TaskBlockerCommandLookup, error)
}
```

`BlockerService`还提供 `ListTaskBlockers(ctx,actor,project,task,status) ([]TaskBlocker,error)`；status严格枚举 unresolved/resolved/all，无隐式零值。读取最多4096个保留record，按 `(created_at,id)` 升序；数组非null，所有返回值深Clone。不分页是本卡有界API决策：最大4096×16KiB总值上界64MiB，实际文本通常更小；未来HTTP须另定分页wire而不直接无限传输本内部结果。无过滤type必要性：本卡仅两个分支，调用方可按本地已授权结果筛选；后继正式query可新增。

两个固定command分别 `work.task.blocker.add`、`work.task.blocker.resolve`；均要求ExpectedVersion≥1。同义completed replay不重验当前Task version/Blocker状态；不同新命令再次resolve已解对象返回 `BLOCKER_ALREADY_RESOLVED`，不写第二历史。没有no-op成功mutation。

公开摘要函数 `TaskBlockerAddDigest` / `TaskBlockerResolveDigest` 输入Actor、CommandMeta、Project、Task及相应请求，固定canonical-v1七键 `{format,command,project_id,task_id,actor_user_id,expected_version,request}`，format=`work-task-blocker-v1`。expected为规范十进制string；稳定Human User进入摘要，Session/RequestID/key不进入但各自shape仍校验；request沿严格DTO规范编码。不trim正文；null comment有明确语义。identity为`project/[project]/command/key`，不复用旧Task三command或transferidentity。纯摘要不授予权限。合法AgentRun返回DEPENDENCY_UNBOUND，Service返回FORBIDDEN，零Actor为UNAUTHENTICATED。

## 4. 当前权限、错误顺序与锁

服务构造 `NewBlocker(store, BlockerDependencies{Authority,Structure,Events,BlockerEvents,Activity})`；Authority与Structure必须真实同Store且同Authority，typed nil拒绝、事件factory Valid才接受；不访问DB、不接受成功fallback。服务独立Stop/Drain沿TaskService已验语义，不关闭共享Store。

新命令固定顺序：admission/ctx和pure输入 → 第一次完整锁 → 当前Session/Owner Read（含Project initialized/lifecycle）→ 原command writer/摘要/completed → Owner Mutate → target同Project存在 → expected version → terminal → 本卡state/assignee → 当前Structure membership与Sprint门禁 → Blocker及relatedTask/图/容量 → 写入。异writer统一NOT_FOUND优先于摘要错误；同writer异义IDEMPOTENCY_KEY_REUSED。archiving/archived仍可Read/Lookup/completed replay；新写沿Project Mutate拒绝。deleting/未初始化不借历史旁路。

第一轮发现Task的Sprint：Command EX、User SH、Project SH、Schedule EX、Task SH。结束Tx后才构造准备/final union：Command EX、User EX（Activity）、Project SH、Schedule EX、Sprint SH、Task EX、Outbox全部计划锁。全union一次AcquireAll；发现Sprint变化则整轮重规划，共享最多3轮；不持Task锁补低序Schedule/Sprint。无需RankGroup，因为本卡不改变rank/priority/state或order generation；TaskQueryGeneration随Task版本变化恰+1使旧list cursor失效。Project EX的结构/归档及其他同Schedule EX Task规划与本卡串行，冲突方须重读version/归属，不能复用准备preimage。

List：User SH、Project SH、Schedule SH、Task SH，Owner Read先于第一条Work SQL。读取真实Task，外Project/missing均TaskNotFound；不重验当前state以免历史消失。Snapshot在同Tx内读全部符合filterrecord，SQL/decoder损坏或超4096直接错误，不能截断返回partial。Lookup：原Command EX、User SH、Project SH，不要求当前Task/Blocker仍存在，不依赖当前目录。

内部Blocker事实/图读写必须同Store活callerTx并检查已持User/Project/Schedule/Task锁及真实Owner权限，不能自开Tx/补锁/网络。采用私有函数及不可伪造本域plan，当前只有本服务是writer。它为后继transition保留可组合实现，但本卡不公开未验的跨状态Apply接口、count或授权布尔参数；后继组合须另验真实Task/state/occupancy与统一事件提交。

## 5. 真实图、不变量及容量

新增rely_on必须在Schedule EX下确认relatedTask真实同Project且非自身。missing/foreign都返回安全 `NOT_FOUND`加固定字段 `/metadata/related_task_id:INVALID_DEPENDENCY_TARGET`，不区分跨Project存在性；self为INVALID_ARGUMENT同字段`SELF_REFERENCE`。related cancelled不满足也不自动resolve，related done同样保留本次显式添加为unresolved；只有未来Scheduler正式自动recovery才依据done解边。本卡不产生自动规则。

读取本Project所有unresolved rely_on边构图，对新增A→B检查B可达A（任意深度/多分支），resolved历史不入图；重复A→B可存在不同Blocker，不按边去重影响历史/容量。图查询任何错误/坏metadata/悬空跨Project事实不得当空图。Task节点≤65536，unresolved Blocker每Task≤256、Project≤262144，保留历史每Task≤4096；先count并有界stream，超过既定边界拒绝RESOURCE_BUSY，不用LIMIT截断证明无环。所有rows与错误路径实际Close/Err检查。

本卡每命令一个add或resolve，因此最终计数可直接证明；resolve不因已达history上限而拒绝。新增达到4096保留记录上限时，沿Task transitions §4.2既定门槛返回RESOURCE_BUSY及唯一固定字段`/blocker_id:BLOCKER_HISTORY_LIMIT`；不能只返回无reason的泛化容量错误。单个新BlockerID全局占用为RESOURCE_BUSY固定`/blocker_id:TARGET_OCCUPIED`，即使属于其他Project也不泄露详情。resolve只接受目标Task所属Blocker；外Task/外Project/missing统一BLOCKER_NOT_FOUND。已解返回BLOCKER_ALREADY_RESOLVED。规范记录不可update type/metadata/description/created身份，仅更新一次resolution字段。

本卡不写blocked状态。`blocked => unresolved exists`、最后Blocker防护与批量resolve+add/transfer留完整B0-P组合后继；若读到非backlog任务的新命令，先按§4范围门禁拒绝，不以未实现的最后保护函数冒称可操作blocked。本次Foundation新增已实际使用三code `BLOCKER_NOT_FOUND`、`BLOCKER_ALREADY_RESOLVED`、`TASK_DEPENDENCY_CYCLE`，Known/Safe保真；不提前新增当前无消费者的LAST_BLOCKER码。

## 6. 两阶段写入、历史、Outbox与恢复

沿已有Task Planning模式：准备短Tx持完整本域union，真实权限/Task/图校验后保存planned command、固定commandID/TaskEventID/EventID、不可变规范request、预像/拟后像/时间/依赖与revision。不在准备Tx改canonical Blocker或Task。结束后调用真实Outbox Discover；final一次完整union重读当前Owner/command/version/placement/图/计数，计划不适用则bounded replan，不能吞Forbidden/SQL error为replan。

plan不复制整图或4096历史：保存targetTask pre/post、单Blocker pre/post、query generation、placement及确定history/event。final在Schedule EX重新读真实完整图，计算必须与本命令proposal一致。新增依赖链变化即使Task version未变，也须重新证明无环；不能仅比较targetversion。planned input≤512KiB、private plan≤4MiB，嵌套严格闭集覆盖所有对象/数组，不沿曾修复的私有decoder错误只校验顶层。

final原子顺序可以局部决定，但同一Tx必须保存Task version+1/updated_at、Blocker、Project TaskQueryGeneration+1、新TaskEvent、Outbox、Activity、completedreceipt/committed_at。Task其它业务字段、rank及全部order generation不变。Task.updated_at、本次Blocker created_at或resolved_at、TaskEvent.created_at、Outbox.occurred_at使用准备计划冻结的同一业务微秒时刻；它从真实DB取时并至少为原Task.updated_at、原Blocker.created_at及command.created_at，final重验计划后保持不变。command.created_at是首次准备持久时刻，completed的committed_at在final另取DB时刻并取不小于created_at/业务时刻的值；它记录final完成采样，不冒充物理COMMIT准确瞬间。Account Activity在final恰调用一次现有TouchActivityInTx，遵循该口自行取时与60秒节流，不保证同微秒或每次更新last_activity_at；历史replay不调用。version/generation溢出安全INVALID_STATE/COUNTER_EXHAUSTED，不回绕。

新增 `TaskBlockerEvent` wire：`id,project_id,task_id,task_version,type,actor,operation_id,correlation_id,payload,created_at`；type仅blocker_added/blocker_resolved，operation/correlation使用本卡CommandID且相等，actor仅Human；payload分别复用B0-C两个小payload，不复制description/metadata。完整record≤16KiB。新事件decoder不接受旧planning type；旧decoder保持拒绝blocker type。Foundation ID marker仅提供Go编译期类型区分，相同UUID及Human payload的JSON可与Transition历史分支同形，纯decoder只证明shape，不证明operation来源。实际来源由blocker_operation_id的本域command FK、producer issuer/purpose与同Tx command/history事实共同证明，不把仅可解码的历史当作写入授权。

唯一新typed Outbox triple为 `(work.task_blockers_changed,work.task,1)`、producer work。payload精确 `operation_id,actor_user_id,task_event_id,blocker_id,change`，change=added/resolved；无正文/metadata/key或Blocker快照，cap16KiB。Header Project/Task/version≥2/occurred_at必须匹配真实同Tx后像，无AggregateSequence。`TaskBlockerEvents` factory采用现Catalog Register/New/Restore/Decode，strict raw边界与安全日志沿T0b已定实践。

Work Authority只对该精确triple追加独立purpose `work.task-blocker.append-v1`，opaque精确kind=task_blocker、commandID/revision/historyID；binding包含Actor/Event Summary/identity/locks/两stage。CurrentAccess只验证已授权planned/completed语义；NewFact必须当前planned、Owner Mutate、真实同TxTask/Blocker/TaskEvent/querygeneration postimage及operation一致。异issuer、错stage、错revision、错payload/locks、无canonical或缺历史均拒绝。Project gate只增这一Human triple，复用两个stage当前Owner检查，不注册`work.task_transitioned`或Agent/System能力。

Unknown沿已有Task策略：保原CommitResult/Attempt/Cause，只一次独立≤3秒确认、原Command EX当前Owner Read。completed同义返回原receipt，planned仍Unknown，真正串行not_observed可NotCommitted/retry_same_key；确认自身失败保原Unknown。显式Lookup用callerctx，不用后台无限重试；取消等待不能假not_observed。Committed不能被deliverycancel改成失败。Stop取消所有已登记调用/confirmation；Drain等待函数、Rows、Tx真正退出。

## 7. 迁移与既有表兼容

`00023`新建 `agenteam_work.task_blockers` 与 `task_blocker_commands`。Blocker保存完整typed字段与安全Human actorjson/必要created和resolvedoperationID，本域复合FK到Task/command（无外域FK/CASCADE），局部CHECK保证type两值、两类metadata形状、resolution字段一致、时间顺序、JSON caps及immutable字段最小DB约束。type语义仍由真实服务strictdecoder验证，不靠jsonb CHECK代替权限。

commands独立两command闭集、unique(project,command,key)、positive revision、planned/completed、canonicalrequest/plan/receipt、唯一拟TaskEventID/EventID、created/committed时刻。无no-op，planned与completed均有plan/historyID/eventID；receipt成功必须与它们一致。明确命名约束用于安全映射，未知SQL错误不伪造成业务冲突。

task_events将旧operation_id改为nullable，新增blocker_operation_id nullable及本域复合FK；CHECK精确XOR：旧task_created/fields_updated必须旧operation非null/blockeroperationnull、原correlation=operation和8KiBpayload约束保持；新blocker_added/resolved必须旧operationnull/blockeroperation非null、correlation=blockeroperation，payload仍≤8KiB。actor仍Human，旧数据不改写，旧约束识别后精确替换，不全表放宽任意type/payload。新History单Operation/TaskEventId保持1:1。

migration实际测试fresh、populated00022升级、re-run与失败回滚；原planning service、receipt/Event codec仍工作。新旧持久Task事件查询按分支解析，不把blocker history送旧decoder并因此破坏planning历史。无生产Timeline读口的现状保持，不在本卡虚构统一query。

## 8. 验收与运行所有权

作者先限定pure/race/vet及两入口build，再冻结输入交未参与实现实例独立审查和场景验证。Go固定 `/workspace/toolchains/go1.27.1/bin/go`，GOTOOLCHAIN=local/GOPROXY=off/GOSUMDB=off/GOTELEMETRY=off、任务自有GOCACHE/GOTMPDIR、-p=2；pure命令每段≤45秒，独立离线编译≤180秒并记录实际Wait/exit，不能因此放宽真实PG driver的105秒预算。普通输出在output，必要probe入正式路径，不复制历史归档。

| 真实顶层selector | 最低必须观察的结果 |
| --- | --- |
| TestTaskBlockerPersistence | fresh/populated升级、DDL局部失败回滚；正式Owner/Structure/TaskCreate→add两个分支→list三filter→resolve→重建lookup/replay，完整持久Task/Blocker/history/Outbox/Activity/receipt一致；最大文本、容量、querycursor stale而rankgeneration不变及旧planning继续可用 |
| TestTaskBlockerAuthority | 非Owner管理员、另Ownerwriter、Session撤销、archived Read与newwrite拒绝、跨Project/Task Blocker及relatedTask、缺外域端口、producer两stage/foreignTx/missinglock、坏持久JSON拒绝和Outbox/Activity失败回滚；typednil另由pure验证 |
| TestTaskBlockerConcurrency | A→B→C与C→A真实图、resolved边消失、双向并发新边恰一胜；同key/异key同expected；Planning Update与Blocker争同version两顺序、真实Schedule gate；单赢家的query/history/Outbox恰一次 |
| TestTaskBlockerUnknown | 准备/final COMMIT未转发和响应丢失、内建确认与显式Lookup独立等待原writer、确认超时保原Unknown/Attempt/Cause、cancelledLookup无假空、迟到Lookup/原key恢复及Stop/Drain实际join |
| TestTaskBlockerInteroperability | 正式BeginArchive与UpdateSprint分别双向prepared/final锁竞争，当前未初始化与deleting门禁；仅证明归档入口accepted/archiving，不宣称生命周期cleanup完成 |
| TestTaskBlockerAtomicity | add/resolve在Task、Blocker、query、history、Outbox、completedcommand、Activity各真实SQL写点注错，完整回滚与同key恢复；正确planned后像仅缺history时producer拒绝 |
| TestIndependentTaskBlockerRuntimeA | 自有跨Project/异writer、双prepared图竞争、同key旧revision恢复、producer后像不足与准备后Session撤销；由未参与实现者本人执行 |
| TestIndependentTaskBlockerRuntimeB | 自有回滚、旧Planning Update/Reorder竞争与sibling rank重算、四种原COMMIT Unknown、内建archived completed确认和确定rollback；由未参与实现者本人执行 |
| TestTaskPlanningAtomicityAndEvents | 00023扩展旧TaskEvent表与Work dispatcher后，既有Task三个writer、旧producer门禁及原子提交保持 |
| TestWorkStructureAtomicityAndProducer | Work dispatcher与Project gate新增精确triple后，既有Structure producer回退路径及原子提交保持 |

正向Task全部由正式TaskCreate产生；图/容量大样本可以在真实首对象建立后使用明确测试fixture批量铺设同schema，只用于边界压力，不替代授权/基础创建正向。非backlog/assigned/terminal损坏或未来状态样本只能作拒绝负例，不能据此声称transition正向。相关Task done/cancelled仅验证不会被本卡自动处理，不声称已通过Task状态写服务。

并发用真实callerTx PID、精确lock key/mode、granted=false和blocker握手；不得sleep猜竞态。必测旧planning及当前Project/Structure/Outbox必要回归，不能只测本服务自洽。独立验证至少一组自有Owner撤权/跨Project/graph竞争，一组自有Unknown/rollback与旧planning竞争，另全文审查本卡路径；作者测试不能称独立验收。

真实PG-only fixture按root统一分配的单一资源窗运行；其他树占用真实PG/browser/hostTCP时，本卡仅进行离线工作，未获窗口不得启动。每轮使用任务自有两IDdriver、精确selector、bounded进程预算及双次资源/runtime/TCP终态；不加载Object/MinIO/外网fixture，不碰现有devinfra，不扩大root已分配资源。输入/依赖未变复用已通过证据，失败保留并只复验影响项。

## 9. 当前状态

独立SPEC已接受本卡限定backlog子结果。首轮发现的两处文义缺陷（Activity同微秒/逐次更新承诺、同形UUID JSON的命令marker来源）已按真实端口修正§6并经差异复审；原首轮不接受事实保留。SPEC独审覆盖正式源码、链接及格式，没有运行Go/PG/browser或产品行为。当前实现与真实验收进展如下，不代表完整B0-P组合/T1或D11完成。

实现候选、00023及四个真实测试top已进入正式路径。作者四包完整pure/race/vet及两入口build通过；三公开contract另经独立公开API pure/race接受，仅覆盖其限定类型能力，完整runtime独审与独立PG仍在准备。首个runtime纯计划roundtrip因误用只认旧planning triple的header helper失败，已补本域严格decoder并复验通过；初始缺固定依赖缓存、作者与独立probe自身编译/刺激错误均保留，不当产品行为PASS。

首轮 `TestTaskBlockerPersistence` 整体FAIL：真实4096历史边界得到RESOURCE_BUSY但遗漏既定BLOCKER_HISTORY_LIMIT reason；此规则由§1所引transition契约继承，本轮将安全字段位置在§5明确。262144项目容量、已填充00022升级与重跑、DDL失败回滚及两类真实新增/解除/读取/重建重放四个子项本轮body通过，但不替代整top通过。Go、driver与外层实际退出1；两任务资源、runtime及host TCP均完成双次清空，冻结输入未变。该首次失败保留。

history错误字段已作单分支最小修复，定向六分支pure/race通过，并经独立STATIC限定接受；恰4096的resolve继续进入真实读取、history超限损坏事实及其它容量错误保持原行为。定向顶层 `TestTaskBlockerHistoryCapacityRegression` 复用原失败场景，已在固定输入下真实race通过（body 6.89秒），覆盖4096历史与256未解除容量；Go、driver与外层均实际退出0，两任务资源、runtime及host TCP均双次清空，输入未变。首轮Persistence整体FAIL仍保留，已通过的四个无关子项未重复运行。

`TestTaskBlockerAuthority` 在同一冻结产品基线上完整race PASS，六个子项body 3.39秒，Go、driver及外层实际退出0，两资源/runtime/host TCP双次清空。它证明已写的权限、隔离、回滚和归档历史读取场景；新增未初始化/deleting明确负向归Interop后续验收，不冒充已覆盖。

`TestTaskBlockerUnknown` 在同一冻结产品基线上完整race PASS，body 15.69秒；四种planned/completed×COMMIT转发/未转发组合均实际观察独立Command锁等待，确认超时保原Unknown、取消Lookup无假空、迟到Lookup/原key恢复与Stop/Drain均通过。Go、driver和外层实际退出0，两资源/runtime/host TCP双次清空；该旧输入不含后续B补充的内部确认成功/确定回滚分支。

`TestTaskBlockerConcurrency` 在包含补充场景的新冻结输入上完整race PASS，七个子项body 5.97秒，Go/driver/外层实际退出0并完成两资源/runtime/host TCP双清。真实观察SH/EX精确锁等待，覆盖完整图与已解边排除、相反边一胜、同Task异key同expected一胜、同key重放、与Task Planning的双向版本竞争及Schedule gate；未用手持Schedule锁替代后续正式Structure命令互操作。

`TestIndependentTaskBlockerRuntimeA` 由未参与实现的独立验证者本人执行并完整race PASS，四个子项body 6.60秒；Go/driver/外层实际退出0并完成两资源/runtime/host TCP双清，输入未变。独立动态接受跨Project/异writer隔离、双prepared真实图重算、Discover前旧revision被替换后的安全FORBIDDEN及Lookup/replay恢复、producer后像不足和准备后Session撤销拒绝。此同key限制仍在：旧调用可能先返回FORBIDDEN，不承诺所有并发初次调用都成功。

`TestIndependentTaskBlockerRuntimeB` 同样由独立验证者本人执行并完整race PASS，十一个子项body 17.93秒；Go/driver/外层实际退出0、两资源/runtime/host TCP双清、输入未变。独立动态接受Outbox/Activity回滚、旧Planning两提交顺序、rank重算、四种原COMMIT Unknown，以及新增内建确认：真实COMMIT后completed在archived Read下返回原receipt，准备事务确定rollback后返回NotCommitted并保留原Attempt/Cause。这里的archived仍为明确fixture，不代表生命周期执行完成。

`TestTaskBlockerInteroperability` 完整race PASS，六个子项body 5.22秒；Go/driver/外层实际退出0并完成两资源/runtime/host TCP双清，输入未变。正式BeginArchive和UpdateSprint各两种提交顺序均观察真实prepared/final与精确PID锁等待，Blocker事实、版本、rank及各域事件保持；未初始化与deleting拒绝路径也通过。归档仅接受入口accepted/archiving，四participant调用均为零，未执行或接受生命周期cleanup。

`TestTaskBlockerAtomicity` 完整race PASS，十六个子项body 7.00秒；Go/driver/外层实际退出0并完成两资源/runtime/host TCP双清，输入未变。七个真实SQL写点×add/resolve均以AFTER ROW及不可回滚sequence证明实际命中，所有业务事实回滚、原planned保留、同key恢复及重放不重复；正确Task/Blocker/query后像仅删除history时，两分支producer均拒绝并回滚。

独立runtime全文STATIC除上述已修history字段未发现新增must-fix；自有A/B probe与构建脚本已落入 `.agent-state/task-blocker-service/` 并完成上述独立真实验收。逐条验收覆盖核对发现的缺口已集中补入两个新top、Concurrency异key同版本及B内部确认。作者Concurrency两处首次User SH观察误用旧EX专用helper，已在运行前修为本地精确mode检查并经独立差异接受，未把此未运行的测试错误当产品FAIL。三个补充作者测试源已联合race编译通过、精确发现七个作者top和两个必要旧回归top，另经独立STATIC接受并完成上述真实验收。两个必要既有服务回归仍待运行，尚无完整服务接受结论。
