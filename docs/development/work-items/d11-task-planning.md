# D11 Human Task 规划持久化

当前SPEC接受补充（2026-10-08）：root已接受rev1完整独立SPEC，独审`06ebfcb0`为SOURCE_SPEC_STATIC_PASS且无必修，见[永久SPEC验证](../agent-team/d11-task-planning-spec-verification.md)。root另授fixture_recovery仅在自有scratch推进20技术路径，当前先六源；main安装、Go编译/测试、真实资源及README #21仍未授。以下初始阶段条目保留原时点，§1–10工程正文不变；规格接受不代表Task产品或完整D11验收，后续实施和执行窗口由root另授。

- 修订：rev1，完整工程规格候选；尚未独立 SPEC 接受，未授业务实施、安装、编译或资源。唯一规格 writer 为 architecture_worker；业务唯一 writer 及独立验证者由 root 在接受后移交。
- 稳定前置：Milestone/Sprint Structure 全18路径已按明确版本组合接受并交付 `a64fb5e783373255a4b0ae7936e5685f25f458c6`，root 已核 origin/main 同；见[永久验收及全18补充](../agent-team/d11-work-structure-verification.md)。不是上游模块整体完成或本卡已经通过。
- root 唯一预留 `db/migrations/00022_task_planning.sql`。本卡只冻结该号码和内容，不授权写迁移源码，不修改旧迁移。
- 唯一结果：Human Owner 在真实 Project/Sprint 中创建未指派 backlog Task，持久读/筛选分页、修改普通规划字段及同组顺序，保存必要 TaskEvent，并提供同 caller Tx 的真实 membership。不是完整 Task CRUD、状态机、指派、执行、删除或完整 D11。

## 1. 已定依据与真实依赖

依据[开发计划 D11](../development-plan.md#d11-work-management-领域)的“Structure → Task canonical/查询/rank → 状态/reviewer → Blocker → Sprint lifecycle → 完整 Timeline/context”顺序。必需历史从首次 mutation 就持久化，不能把顺序中的完整 Timeline 卡解释成现在可以不写 TaskEvent。

| 依据/依赖 | 本卡消费 | 未外推的边界 |
| --- | --- | --- |
| [Task Domain §6–19/22–23](../../architecture/project-work-management/task-domain-model.md) | 强制 parent、五种 type、四种 priority、backlog 可不指派、计划普通字段、版本/幂等/排序/查询 | 不是所有概念命令均已实现；type/priority 概念可选记号没有给默认值 |
| [状态机 §2/3/6](../../architecture/project-work-management/task-state-machine.md) | 七种 canonical state、active 三状态必须 assignee、终态内容冻结 | 本卡没有任何状态转换/审核、Blocker 或 Agent adapter |
| [Timeline §4–8/15–18](../../architecture/project-work-management/task-event-timeline.md) | task_created/fields_updated、Human actor、operation、append-only、同 Tx | 不提供 comment、完整 Timeline/context、运行日志投影 |
| [Sprint §17–18](../../architecture/project-work-management/sprint-lifecycle.md) | planned/current 可创建；completed membership 冻结 | 不实现 Start/Complete/rollover/DeleteSprint，不把 membership bool 当完整 Task 清单 |
| [D01 Foundation](d01-contracts/foundation.md)、[Work 端口](d01-contracts/domain-lifecycle.md#work-命令与占用端口)、[D03](d03-postgresql-foundation.md) | typed ID/Version/CommandMeta/Fault、同 Store 活 Tx、全锁 union、强一致 membership/排序 | 不新增锁类别、SQL 驱动、泛型 UnitOfWork 或 fake occupancy |
| [Structure 卡](d11-work-structure.md)及其全18永久验收 | `work.NewAuthority/NewReader`、真实 `ReadPlacementInTx`、rank 纯算法、U1取消边界、两旧事件/Project gate | 本卡新增 Task canonical、Task producer 和 membership；旧接口不是 Task 已绑定证据 |
| [D08 Owner](d08-project-owner.md)、[D06 Outbox](d06-transactional-outbox.md#d06最终采纳与后续绑定) | 当前 Human Session/Owner/Project Read/Mutate、Account Activity、typed PrepareAppend/AppendEventInTx | 只消费已有真能力；测试持久 Account/Session 与 Skill receipt 不冒正式 Login/生产 Skills/创建 HTTP |

基线候选 rev02 `6b5dd72e`（单句 raw512 修正记录 `c6298c80`）只作方向来源；本卡以下内容冻结全部待定工程项，不能继续把“建议”当实现自由选项。Structure 的原接受组合及 B 外部工具 terminal 缺口保留，不复活 Object runtime join、OpenAI tools 独验、SPA 并发发布或 Image/Jina 停止线。

## 2. 类型、presence、安全编码与规模

### 2.1 Task 与输入闭集

包仍为 `internal/central/work/contract`。复用 `ProjectID`、`MilestoneID`、`SprintID`，新增 `TaskID = foundation.ID[Task]`、`TaskEventID = foundation.ID[TaskEvent]`、`TaskCommandID = foundation.ID[TaskCommand]`。Agent/User ID 复用 identity 的 marker；不定义第二种 Sprint/Agent ID。全部非零、规范小写 UUIDv7，拒绝 UUIDv4、nil ID 和未初始化 Go ID。

`TaskType` 恰 `feature,bug,task,spike,chore`；`TaskPriority` 恰 `low,medium,high,critical`；`TaskState` 恰 `backlog,todo,in_progress,in_review,blocked,done,cancelled`，大小写/显示文案替代拒绝。Priority 的业务顺序是 critical > high > medium > low，不按字符串大小排序。

`Task` JSON 字段全部必有：`id,project_id,milestone_id,sprint_id,title,description,type,priority,state,assignee_agent_id,plan,manual_rank,version,created_at,updated_at`。仅 assignee_agent_id 允许 null；不含 Execution/Dispatch/Blocker/Timeline 的假空数组。version≥1，canonical 十进制字符串、int64 上界；时间沿 foundation.Instant/UTC 微秒，created_at≤updated_at。todo/in_progress/in_review 的 assignee 必须非 null；其它状态可 null。读回其它合法 state/非空 assignee，不改写成 backlog/null；这仅是持久局部事实，不证明 Agent 当前合法或 blocked 对应 Blocker 已验证。

本卡 Create 的成功结果恒 backlog/assignee=null；Milestone 只能从真实同 Tx Sprint placement 推导。TaskID 由 caller 为原意图预生成，重试不得再生成。Create 的 type/priority 必须显式输入，不选择 task/medium 默认值，不替未来 UI 定默认。

| Go DTO | 精确 JSON 字段 / presence |
| --- | --- |
| `TaskCreate` | 必填 `task_id,sprint_id,title,type,priority`；可省略 `description,plan,initial_state,assignee_agent_id`。description/plan 省略展开为空串，initial_state 省略展开 backlog。所有显式 null 拒绝，assignee 若存在必须合法非零 AgentID。非空 assignee/todo 只进入 §5 的明确否定分支，不能忽略后成功 |
| `TaskFieldsUpdate` | 可省略 `title,description,type,priority,plan`，至少一项存在；Go 为对应 `*string/*TaskType/*TaskPriority`，nil 是省略，JSON null 拒绝，空 description/plan 是清空。没有 state/assignee/parent/rank 字段，输入这些未知 key 是 INVALID_ARGUMENT |
| `TaskReorder` | 仅可选 `before_id:*TaskID`；省略表示组尾，显式 null 拒绝。before=target 为 INVALID_ARGUMENT/SELF_ANCHOR；不提供 after、caller rank、整组 ID 列表或跨组 move |
| `TaskCommandName` | 恰 `work.task.create,work.task.update,work.task.reorder`；独立类型，不扩原 Structure 六名 Validate |
| `TaskCommandLookupRequest` | 必填 `project_id,command,idempotency_key,semantic_digest`；没有 body/target 猜测、expected_version 替换或跨身份授权标志 |
| `TaskMutation` | 全部必有 `task,changed,task_event_id,event_ids`；changed=true ⇒ task_event_id 非 null、event_ids 恰一个 Outbox EventID；false ⇒ null 和空数组。返回原完成 Task 快照，不是当前 Get；D01 概念的 task_id/version/state 分别由 task 内字段给出 |
| `TaskCommandLookup` | 全部必有 `status,receipt`；status 为 committed/in_progress/not_observed。仅 committed 时 receipt 非 null，另两种必须 null；两种观察状态不授自动重执行 |

`TaskCreate` 的可省略文本和 initial_state 在 Go 可使用零值表示省略后语义，但 JSON decoder 必须区分“缺少必填 type/priority”与值为空；initial_state 的零 Go 值仅表示省略，Validate/摘要先展开 backlog。Update 永远保留字段 presence。所有请求/结果/事件均有 Validate、严格 JSON 编解码与必要 Clone；深复制 pointer/slice，调用方不能改内部 receipt、plan、锁或结果缓存。

Go 字段按上述 snake_case 一对一导出：所有文本（含 ManualRank）为string，枚举为各自命名string类型；Task.Version、TaskEvent.TaskVersion 为 foundation.Version，CreatedAt/UpdatedAt 为 foundation.Instant，AssigneeAgentID 为 *identity.AgentID。TaskCreate 的 Description/Plan 为string、InitialState 为TaskState、AssigneeAgentID 为 *identity.AgentID；其它必填项为相应非指针值。TaskFilter 除 AssigneeAgentID 使用上述三态值外，State/Priority/Type/MilestoneID/SprintID/Text 均为对应类型指针，nil表示省略。TaskMutation.TaskEventID 为 *TaskEventID，EventIDs 为 []event.EventID；Lookup.Receipt 为 *TaskMutation。TaskPosition.OrderGeneration 是正int64，wire为规范十进制字符串；TaskEvent.TaskVersion沿Version字符串，禁止JSON number。内部group/query/revision也是1..MaxInt64，不引入浮点代数。

### 2.2 文本、wire cap 与最坏编码

title 沿已验 Structure：1–256 Unicode scalar、≤1024 UTF-8 bytes、至少一非 Unicode 空白、拒绝全部 Cc；description 与 plan **各自** 0–32768 UTF-8 bytes，允许 TAB/LF/CR，拒绝其它 Cc。均保留原字节、不 trim/NFC/大小写折叠；重名合法。非法 UTF-8、JSON 孤立 surrogate、重复 key、大小写替代、未知字段、尾随值、数值替代字符串标量、越界 null 全拒绝。`encoding/json` 或 canonical-v1 单独使用不足以完成这些职责。

固定 cap：TaskCreate/Update/Reorder 的单个原 JSON `MaxTaskRequestBytes=512KiB`；Task/TaskMutation/TaskCommandLookup 单值 `MaxTaskResultBytes=512KiB`；Filter/LookupRequest 各16KiB；本卡 TaskEvent record16KiB、各历史 payload8KiB、TaskChanged payload16KiB；私有 plan4MiB。cap 核完整交给自有 UnmarshalJSON 的 raw（包括其可见空白），MarshalJSON 同样核完整输出；标准 json.Unmarshal 会裁外层空白，测试必须分清方法层和标准入口可见范围，不凭外层空白伪造覆盖。直接 Go 输入仍逐字段验证。

两段大文本加 title 的保守 escaping 上界为 `6×(32768+32768+1024)=399360` bytes；本卡单 Task、请求或单 receipt 的固定字段/标量/封套保守留8192 bytes，合计407552<524288。必须用同时满额 `<>&`、引号/反斜线及合法 TAB/LF/CR 代表验证请求→持久回执→Lookup 编解码，不能仅 ASCII 最短向量。原 Structure 命令/receipt256KiB 与 Placement 派生 cap 不改。

私有 plan 只保存 target before/after、最小 placement facts、至多两个组的前后 `(id,rank)` 向量、固定摘要/事件；不复制每个 sibling 正文或整个父 DTO。每向量≤4096项、每项序列化≤128B，至多四向量≤2MiB，加两个512KiB Task及固定元数据≤64KiB，总量<4MiB。request 另存一份，不在 plan 重复多次。输入或合法最大文本不能因内部 plan/receipt 误设小 cap 而失败。

每 Task 排序组最多4096；每 Project 最多65536 Task（工程存储/查询界限）。新 create 或迁入已满目标组返回 RESOURCE_BUSY/GROUP_LIMIT，Project 上限用 PROJECT_TASK_LIMIT；不截断。历史 replay 不重验容量。List 沿 PageRequest 默认50、1–200，显式0/null拒绝，cursor≤8192B；最多 limit+1 条用于下一页。返回 `foundation.Page[Task]`，不新增 HTTP 封套；每 item 校验上述 Task cap，最大200项的完整序列化可达 `200×512KiB+16KiB`，**不得套单 DTO 512KiB cap 截断页**。本卡不声称现有通用 Page decoder 等价新严格 HTTP schema，也不承诺未测固定查询耗时。

常规 fmt（包括 enclosing 值）、slog 和 error 只输出固定 `work_task`/安全 Fault 元数据；不输出正文、key、cursor、semantic原文、SQL/DB连接信息或原 error。显式 typed JSON 是授权业务结果/持久化通道，不把“安全 fmt”误作可任意日志打印 MarshalJSON。

## 3. 精确公共口与读/membership

以下为本卡新增口；既有 Structure Commands/Reader、Dependencies、WorkEvents 和构造保持字节/语义兼容。

```go
// work/contract；ctx/actor/meta/project 顺序沿现有库
type TaskCommands interface {
    CreateTask(context.Context, identity.Actor, foundation.CommandMeta, ProjectID, TaskCreate) (TaskMutation, error)
    UpdateTask(context.Context, identity.Actor, foundation.CommandMeta, ProjectID, TaskID, TaskFieldsUpdate) (TaskMutation, error)
    ReorderTask(context.Context, identity.Actor, foundation.CommandMeta, ProjectID, TaskID, TaskReorder) (TaskMutation, error)
    LookupTaskCommand(context.Context, identity.Actor, TaskCommandLookupRequest) (TaskCommandLookup, error)
}
type TaskReader interface {
    GetTask(context.Context, identity.Actor, ProjectID, TaskID) (Task, error)
    ListTasks(context.Context, identity.Actor, ProjectID, TaskFilter, foundation.PageRequest) (foundation.Page[Task], error)
    HasTasksInSprintInTx(context.Context, foundation.Tx, identity.Actor, ProjectID, SprintID) (bool, error)
}
```

work 新构造 `NewTask(store Store, deps TaskDependencies) (*TaskService,error)` 与 `NewTaskReader(store Store, authority *Authority, structure *Reader, keys cursor.Keyring) (*TaskReader,error)`。TaskDependencies 恰 `Authority *Authority, Structure *Reader, Events oc.Appender, TaskEvents c.TaskEvents, Activity ActivityAuthority`。沿既有 `work.NewAuthority(store,ProjectAuthority)`，同一 Authority 注册为 producer `work`。TaskService 提供 Stop()/Drain(ctx)；不增加后台恢复 worker。

构造只做绑定/type/自有 Store identity 校验：所有必需端口（含 typed nil）有效；store 与 Authority/Structure Reader 的 Store 同一，Reader 的 Authority 必须为传入的同一实例，keys/TaskEvents 有效。缺失/外 Store 绑定返回 DEPENDENCY_UNBOUND；不访问 DB、不用类型转换窥探外部 Project/Outbox 内部 Store，不称不同 wrapper 等价同 Store。每次同 Tx 外域口自行真验 Store；不允许只写 Task 而跳过事件/Activity。

### 3.1 TaskFilter 与稳定分页

Filter 闭集 `state?,priority?,type?,assignee_agent_id?,milestone_id?,sprint_id?,text?`，都是单值、没有数组/OR组合/排序参数。除 assignee 外，省略是不过滤、显式 null 拒绝。assignee 是精确三态：省略=不过滤、null=未指派、合法 AgentID string=该持久值；Go 用 `TaskAssigneeFilter{Present bool, AgentID *identity.AgentID}`，Present=false 时 AgentID 必须 nil。不得把未筛选与未指派归一。assignee filter 只是持久谓词，不查询/证明 Agent 存在或可分配。

text 1–256 Unicode scalar、≤1024 UTF-8 bytes，至少一非空白、无 Cc；保原字节。第一阶段用 PostgreSQL 字面、大小写敏感 substring，分别匹配 title/description/plan 后 OR，**不跨字段拼接**；`%/_/\\` 是普通字符，无SQL LIKE通配、正则或隐式Unicode归一。ID filter 合法但不命中已授权 Project 时返回合法空页，不提供跨项目存在性探测；不据此表示该 Sprint 可写。

固定总序：`sprint_id ASC, state_order ASC, priority_order ASC, manual_rank COLLATE C ASC, id ASC`。state_order 为 backlog=0,todo=1,in_progress=2,in_review=3,blocked=4,done=5,cancelled=6；priority_order 为 critical=0,high=1,medium=2,low=3。组内仍正式 rank/id；跨组序只是本库稳定分页，不替 UI 规定视觉排序。排序用相同 SQL CASE/生成表达式及索引，不能 SELECT 一种顺序而 cursor 比较另一种。

cursor Binding.Scope=InProject；QueryDigest=canonical-v1 `{format:1,kind:"work.tasks",project_id,owner_user_id,filters:<完全展开的三态/其余nil>}`。不含 Session/limit/正文；text 只进入 digest。Order=`sprint:asc,state_order:asc,priority_order:asc,manual_rank:asc,id:asc`。Position 精确五 scalar `[UUID(sprint),Integer(state_order),Integer(priority_order),Text(rank),UUID(task)]`，OrderGeneration 必有，绑定本 Project 的 `query_generation`。类型/枚举范围/签名/kid/query/scope/order 不符 CURSOR_INVALID；代数不同 CURSOR_STALE。换有效 Session 同User可续页，换 User/Project/filter 不可复用；limit 可变化。

Get/List 在各自一个短 Tx 先验当前 Human/Owner Read，再读取 canonical/query generation/同页 item；User SH、Project SH、ProjectSchedule SH（Get 再 TaskAggregate SH）。不 Touch、不创建行/组/command，不调用外网。非法原始DTO先拒绝；但坏 token 内容/签名解析晚于当前授权，撤销 Session 不被坏 cursor 覆盖。Get 未命中本 Project 的 Task 为 TASK_NOT_FOUND。Get/List 返回存储 canonical，FK/局部解码错误安全 INTERNAL_ERROR，不凭未接通的 Agent/Blocker/Execution 口补默认值。

Project query generation 对所有真实 Task 业务修改和必要物理 rank 变化恰+1，任何 filter/sort 可能受影响即 stale；no-op/replay不变。它与各 group 的 order_generation 分开。缺 query row 的逻辑初值1只适用于实际无Task；有Task而缺 generation 为损坏，不能返回默认1。事务锁保证同页单代数，不承诺跨页历史快照。

### 3.2 HasTasksInSprintInTx 的真实责任

严格先本 Store.InTx 证明活 Tx，再 RequireHeldLocks：User SH、Project SH、ProjectSchedule **EX**、目标 SprintAggregate SH；然后真实当前 Owner Read 与既有 `Structure.ReadPlacementInTx` 验同 Project/Sprint/Milestone。未知/外项目 Sprint 安全 TASK_SPRINT_INVALID。沿 caller 同一 Tx 查询 `agenteam_work.tasks WHERE project_id=? AND sprint_id=?` 的真实 EXISTS；false 仅代表此时无任何 state 的 Task，不只 backlog，不用 group行、TaskEvent、缓存或调用者 task_ids 代替。

不补锁、不升级、不自开/提交事务，不 Touch，不执行 lifecycle。表不存在/Store/SQL/ctx失败返回错误与false零值；调用者必须检查 error，不能把零值当空成员。EX 满足SH，SH不满足该口Schedule EX。Future Task create/move/delete 与 Sprint删除/完成须同调度锁，才能消除 phantom；本卡仅提供 Human Owner bool，Service lifecycle授权、完整不可截断Task ID清单、占用和Delete正向另卡真实绑定。

## 4. 持久化、rank 与完整锁

### 4.1 00022 的五张 Task 表

仅在既有 `agenteam_work` 新增 `tasks,task_order_groups,task_query_generations,task_commands,task_events`；不改00021。00022为现有sprints增加唯一 `(project_id,milestone_id,id)`，供同域复合FK使用，不重写历史行或指针。旧五表正文/版本/生命周期不变，升级时Task五表为空，不为所有Project预建假组。

| 表 | 必需约束/内容 |
| --- | --- |
| tasks | 全部§2 canonical；id PK、UNIQUE(project_id,id)；FK(project_id,milestone_id,sprint_id)→sprints(project_id,milestone_id,id)，RESTRICT/NO ACTION；同组(project_id,sprint_id,state,priority,manual_rank) UNIQUE DEFERRABLE INITIALLY DEFERRED；ID复用safe_id domain；枚举、字节/字符限额、version>0、rank开区间、时间顺序、active三state assignee非null检查 |
| task_order_groups | PK(project_id,sprint_id,state,priority)，另存由placement取得的milestone_id，并用同一复合Sprint FK；order_generation>0。缺组初值1只在该真实空组成立，有成员缺组或超过既定容量是损坏 |
| task_query_generations | project_id PK、query_generation>0；由首次合法变化创建，不跨域FK Project |
| task_commands | TaskCommandID PK、UNIQUE(project_id,command_name,idempotency_key)、User原writer、semantic_digest、规范request≤512KiB、plan_revision≥1、planned/completed、plan≤4MiB、TaskEventID与OutboxEventID各UNIQUE nullable、receipt≤512KiB、created_at/committed_at；三个command名闭集；planned有plan无receipt/committed_at；completed有严格receipt/committed_at；changed与两个事件身份同有同无；不FK到尚未创建的Task |
| task_events | TaskEventID PK、同项目Task FK→tasks(project_id,id) RESTRICT、type仅task_created/fields_updated、task_version>0、actor安全投影、operation_id/correlation_id、typed payload≤8KiB、created_at；同项目operation FK→task_commands(project_id,id)，后者另有该unique键。按(project_id,task_id,created_at,id)索引；不对operation强加“永远一条Event”的跨未来约束，本卡命令自身保证恰一条 |

Command中的事件ID若为planned是拟身份，不能靠该列非null断言Event已存在。成功changed必须同时留下对应TaskEvent、Outbox及receipt；原子性/producer重验保证这点，不跨Outbox建立FK。TaskEvent不含comment编辑/软删列，不开放UPDATE/DELETE API；未来comment/多事实操作必须另冻schema，不能由通用字段包提前接受。SQL约束只负责可表达的局部invariant；输入和持久解码仍执行完整Unicode/闭集校验，不把PG文字规则当等价Go schema。

索引至少覆盖Project稳定五元排序、Sprint membership、group rank、task_events稳定序、command identity/EventID。text条件用参数化strpos；无新搜索扩展/索引服务依赖。新迁移fresh、已有00021数据升级、重复执行与DDL中途失败全事务回滚均须真实验证；禁止CASCADE、跨域FK、改00021/迁移校验绕过、重建Project current_sprint pointer。

### 4.2 rank 与 generation

固定复用已验32位小写hex、M=2^128−1、(0,M)开区间和整数midpoint/必要时 `floor(i*M/(n+1))`。可以直接复用未修改的work/rank.go纯算法；Task组key不是Structure组。新create在backlog/该priority尾部。Reorder移除target再按before身份插入；合法但不在同组的anchor统一TASK_NOT_FOUND，不能跨组借position写parent。

Update priority真实改变时从旧组移出，追加新priority backlog组尾；target业务version/updated_at只+1。source/target group各+1，Project query_generation只+1；target组必要rebalance不再次加代数。其它字段更新只写显式业务列和version/time，不回写读出的rank/parent；不改变group代数，但query代数+1。真实Reorder为target/group/query各+1。no-op先完成所有新写门禁，再保存独立completed receipt＋Activity；canonical/rank/group/query/history/Outbox不变，历史replay连Activity也不变。

纯rebalance只改当前组存储rank、保相对顺序和每个spectator的全部业务字段/版本/时间/关联；不写spectator TaskEvent。它不是独立公共命令，只随本次必要合法mutation同Tx执行、失败一起回滚。不得以“rank维护”名义改变真实逻辑顺序而不产生业务version/TaskEvent。不同priority组之间不移动spectator。未来todo pending claim的位置保护/映射尚未实现，本卡只改未指派backlog；不能沿本验收直接开放D23组维护。

### 4.3 锁、发现与计划修订

rank scope key=`work.task:<project UUID>:<sprint UUID>:<state>:<priority>`，最长<128 ASCII bytes，复用foundation.RankGroupLock；不加AggregateKind。ProjectSchedule保护membership/query代数，RankGroup保护精确排序组；SprintAggregate在TaskAggregate之前，Outbox record最后。

完整union原始拼接（含重复项）**先≤512**，再Normalize排序/去重取较强模式，然后第一次AcquireAll一次取得完整锁。不得只检查去重后数量、忽略失败或边写边补低序锁。rebalance不对4096 sibling逐个加锁；Schedule EX＋group EX保护它们的rank。CompareLockKeys用于完整key比较，不能比较含func的LockKey值或只比较64bit hash。

| 阶段 | 本域完整锁；final另并入AppendPlan全部锁 |
| --- | --- |
| 有界只读discovery（Update/Reorder取未知旧组） | Command EX、User SH、Project SH、ProjectSchedule EX、目标Task SH；不写计划/Task/Event/Activity、不持Tx调用PrepareAppend |
| 准备/最终mutation | Command EX、User EX（Activity）、Project SH、ProjectSchedule EX、source/target RankGroup EX（去重后至多2）、该Sprint SH、目标Task EX |
| 普通Get/List | §3.1；不取Command，Task List无逐item锁 |
| Lookup | 原Command EX、User SH、Project SH，不依赖Task当前存在或其现在的group |
| membership | §3.2；Task存在性不加无界Task locks |

逻辑锁顺序恒 Command0→User/registry1→Project2→Schedule/RankGroup3→Sprint/Task5→Outbox record6，同级由官方Normalize排序。Structure既有Project EX阻止Task持Project SH时的父变化；本卡不改其粗锁政策。

Update/Reorder不知道旧group时，先在单独只读discovery按§5先当前Read/identity/历史；仅未completed才检查Mutate、读target的最小旧Sprint/state/priority。结束整个Tx后据这些身份构造准备的完整union。下一准备Tx重验旧group/target/parent；变化时结束、不写，按原请求再发现。不能先拿Task5再补Rank3；discovery不是持久planned，不是成功，也不批准以旧发现事实继续写。Create的Sprint/目标组由已验证请求可确定，但真实placement仍在锁内读取。

本卡最多3个规划轮次，**discovery过期、准备到final适用性过期共用该总上限**，不能各三次嵌套变成九次；不因合法版本冲突而刷新expected_version。读出当前target version不匹配原expected立即TASK_VERSION_CONFLICT。PrepareAppend的拒绝（含旧计划Forbidden）原样安全返回，不吞任意错重试/改为Busy；只对明确本域适用性失效结束Tx后重规划，耗尽RESOURCE_BUSY。

InTx口先同Store活Tx→完整已持锁→当前授权→Work SQL，EX满足SH，禁止升级/内层Tx/commit/Provider或其它外网I/O。普通Store/ctx失败不解释成无Task/无命令；未结束Tx/Rows不能退役成已释放。

## 5. 当前授权、命令身份与 Unknown

### 5.1 摘要、错误先后和安全 code

Identity为 `namespace=project,owner_ids=[project_id],command=三完整work.task名,key=meta.IdempotencyKey`。三个摘要的精确签名为 `TaskCreateDigest(identity.Actor,foundation.CommandMeta,ProjectID,TaskCreate) (foundation.Digest,error)`、`TaskUpdateDigest(identity.Actor,foundation.CommandMeta,ProjectID,TaskID,TaskFieldsUpdate) (foundation.Digest,error)`、`TaskReorderDigest(identity.Actor,foundation.CommandMeta,ProjectID,TaskID,TaskReorder) (foundation.Digest,error)`。Validate后按既有Structure方式，对精确对象 `{format:"work-task-planning-command-v1",command,project_id,target_id,actor_user_id,expected_version,request}` 做canonical-v1后SHA-256，返回规范`sha256:`摘要。包含稳定Human user、Project/command/target、expected_version presence/value、真实请求SprintID及所有输入字段。Create展开description/plan空串、initial_state backlog、未指派null；显式backlog与省略同义。Update逐字段presence/value进入摘要；Reorder省略tail展开null（入站null仍拒绝）。排除独立identity中的IdempotencyKey、Session/传输RequestID/CSRF/超时、服务端event/commandID/rank/generation/时刻。Semantic不是授权token。

Create ExpectedVersion必须nil；Update/Reorder必须非nil且≥1。同scope/command key换Task、Actor、expected或有效字段是异义；不同Project或不同command同key独立。原writer不同不先报摘要差异而泄漏历史。

新语义错误在foundation.Code/Code.Known精确增加六项：`TASK_NOT_FOUND,TASK_VERSION_CONFLICT,TASK_STATE_INVALID,TASK_ASSIGNEE_REQUIRED,TASK_SPRINT_INVALID,TASK_TERMINAL_IMMUTABLE`。对应Go常量`TaskNotFound,TaskVersionConflict,TaskStateInvalid,TaskAssigneeRequired,TaskSprintInvalid,TaskTerminalImmutable`。Known/Safe/fmt/JSON/Unwrap保真，不改共同错误类别；本卡无HTTP adapter，不修改HTTP映射。其余未来TASK_*不预加、不假查询Agent后返回TASK_ASSIGNEE_INVALID。

合法输入按下序执行（纯语法不读库）：

1. ctx/已Stop admission、Actor和DTO/meta/ID/schema验证。未构造Actor UNAUTHENTICATED；合法AgentRun DEPENDENCY_UNBOUND，Service FORBIDDEN；畸形DTO/枚举/非法ID/缺type或priority INVALID_ARGUMENT。预取消用安全Fault保errors.Is(ctx.Err)，无业务写。
2. 本阶段完整锁后当前Session/Project Owner Read/initialized门禁。Session撤销/失效优先；Project不存在或非Owner（含非Owner管理员）沿真实Authority的NOT_FOUND，不转换成Task可见；未初始化/deleting等沿PROJECT_NOT_ACTIVE。archiving/archived可以查历史。
3. 原Command EX下查identity：原stable User不同→NOT_FOUND；同User异semantic→IDEMPOTENCY_KEY_REUSED；同义completed→Clone原receipt，**先于当前target/version、Sprint completed、terminal、assignee/state能力和容量检查**。新Session同User可恢复但必须当前有效。
4. 无completed才RequireOwnerInTx(Mutate)，只active。先读目标：Update/Reorder未命中→TASK_NOT_FOUND；Create目标ID全局占用→RESOURCE_BUSY/TARGET_OCCUPIED，不泄对方Project。然后读真实placement：请求Sprint缺失/外Project→TASK_SPRINT_INVALID；现存Task冗余parent与真实placement矛盾是INTERNAL_ERROR，不自动修复。
5. Update/Reorder原expected不符→TASK_VERSION_CONFLICT；三命令共同检查所在/目标Sprint completed→TASK_SPRINT_INVALID/COMPLETED_SPRINT；然后Update/Reorder目标Task done/cancelled→TASK_TERMINAL_IMMUTABLE。其余非backlog，或backlog已指派→DEPENDENCY_UNBOUND（本卡未提供这些写入能力），不谎称非法历史/Agent不存在。
6. Create initial_state为backlog/todo以外已知值→TASK_STATE_INVALID；todo无assignee→TASK_ASSIGNEE_REQUIRED；todo有assignee或backlog有assignee→DEPENDENCY_UNBOUND。只有backlog且无assignee继续。该拒绝发生在持久planned之前，无Agent/Blocker成功stub。
7. anchor/组归属、容量、generation/version溢出和局部一致性；最后计划/原子落库。no-op不跳门禁。溢出为INVALID_STATE/COUNTER_EXHAUSTED，禁止回绕。

TASK_SPRINT_INVALID只给固定安全理由/字段路径，不区别“外Project”与“不存在”。DB精确已知Task PK占用仅在确定失败/回滚后映射TARGET_OCCUPIED；不能匹配错误字符串或在poisoned Tx继续写。任意SQL/约束/持久DTO损坏安全内部错误，私有cause不出wire。没有completed的失败可留下原planned，但不保存成功/永久失败receipt，也不能吸收异义新请求。

### 5.2 准备、TaskEvent 和终局

复用[Structure §4.2–4.3](d11-work-structure.md#42-准备与原子终局)的真实两阶段/issuer/Unknown安全规则，Task私有记录单独实现，不扩旧Structure命令解码器。

准备Tx在完整union、当前授权/历史/新写规则后持久原input、expected facts、完整拟Task postimage、组前后rank/generation、query_generation、拟TaskEvent与Outbox Header/payload。这里只存planned，不改canonical/TaskEvent/Outbox/Activity/组/query行。TaskCommandID首准备生成并固定，plan_revision重规划+1；每次新revision生成新的TaskEventID/OutboxEventID使旧opaque失效。数据库时刻用于拟Task.updated_at/TaskEvent.created_at/Outbox.OccurredAt，且不早于原updated_at；最后committed_at是final Tx DB时刻，不宣称物理COMMIT精确时刻。

事务外用同catalog TaskEvents Restore并PrepareAppend；final在新Tx首次取得本域＋AppendPlan完整锁，重做§5.1、不可变identity/revision、target/parent/组和postimage适用性。按§4.3仅对明确适用性失效重规划。原expected/body/target/before身份不能改变。

final同物理Tx写target、必要纯rank维护、组代数/query代数、**真实TaskEvent**、typed Outbox、completed receipt、Account Activity。可以先写canonical与TaskEvent供producer NewFact重验再Append，然后写receipt/Touch；任一错误包括Rows未闭/触Activity失败均全部回滚。恰一次真实业务变化→恰一TaskEvent＋一Outbox；no-op直接在授权同identity锁Tx保存changed=false receipt＋Activity，不产生假event/plan业务记录；历史replay不再Touch。

真实reorder必须写§6定义的fields_updated/manual_rank安全逻辑位置事实，因此不再是“只有creation history”的可删草稿。纯maintenance的spectator不生成历史，不能与真实reorder混为一谈。

### 5.3 Unknown、Lookup、取消、Stop/Drain

WithinTx原始CommitResult及Attempt必须在私有错误链保留；不能降级成只有Fault再丢writer身份。准备/final Unknown时最多一次独立、上限3s、只读原Command EX确认；当前Read/User/semantic重验。看到同义completed才成功；真串行确认无行可报告NotCommitted＋retry_same_key并保原attempt诊断来源；planned只证明准备存在，仍返回原COMMIT_UNKNOWN/lookup。确认超时、撤销、读取错误或确认自身Unknown，不取代原writer的CauseID/CommitState，不在该分支继续final或重规划。

显式Lookup用caller context，同原identity EX真实等待；completed→原receipt、planned→in_progress、无行→not_observed，无行/行均不创建任何数据。in_progress不证明有worker；not_observed也不授权自动重发。仅caller保留原请求后显式Execute可继续；归档历史completed可replay，planned不能补写。

承接已验U1：只有本次自有WithinTx已明确NotCommitted且caller ctx.Err非空，才返回零值和可errors.Is的取消错误；Committed不因delivery取消改成未提交，Unknown不由ctx/连接关闭/空SELECT推断回滚。Get/List同样不能把取消读降为合法空结果；InTx口无自行宣布caller整体commit状态的权力，只回安全错误由caller真实Tx决定。

TaskService.Stop取消本实例已登记Execute/Lookup及confirmation，拒新admission；Drain等待实际函数、confirmation、Rows/Tx退出，不把cancel通知当join，不关闭共享Store/Outbox。独立TaskReader/StructureReader/其它TaskService或proxy writer由组合者另行join；无后台自动恢复、无“租期到期”抢writer、无process-death新增服务。

## 6. TaskEvent 与 typed Outbox 的精确 schema

### 6.1 本卡持久 TaskEvent

TaskEvent record闭集：`id,project_id,task_id,task_version,type,actor,operation_id,correlation_id,payload,created_at`，全部必有。actor恰 `{type:"human",user_id:UserID,source:"task_domain"}`；operation_id=本TaskCommandID，correlation_id同operation_id，不复用传输RequestID/Session、不接受客户端任意actor。type两值；TaskEvent的自然读取顺序(created_at,id)，本卡不新增Timeline查询口。

| type | payload（所有键必有，null只限下述位置） |
| --- | --- |
| task_created | `{initial_state:"backlog",milestone_id,sprint_id,type:TaskType,priority:TaskPriority}`，不复制title/description/plan或执行信息 |
| fields_updated | `{changed_fields:[TaskChangedField],type_change:null|{from:TaskType,to:TaskType},priority_change:null|{from:TaskPriority,to:TaskPriority},position:null|TaskPosition}` |

TaskChangedField闭集 `description,manual_rank,plan,priority,title,type`，数组严格按上述词法序升序、无重复/非空。普通Update仅含值真正变化的title/description/type/priority/plan，不把presence相同值算变化；type_change恰在type变化时存在且from≠to，priority同理。无priority变化的普通Update position=null；priority改变position为新组最终安全位置。Reorder的TaskEvent仅changed_fields=[manual_rank]、两个change=null、position非null；不记raw rank值。Plan按普通fields_updated，不造plan_updated事件。

TaskPosition全部键必有：`sprint_id,state,priority,previous_id,next_id,order_generation`，前后ID可null，其余合法；本卡state=backlog，previous/next互异且不等target，同新组当前locked真实邻居，generation为该次后值。priority Update的位置隐含尾部，next_id=null；create的Outbox位置同理。历史payload引用Sprint/Milestone须与本Task/plan一致；历史TaskEvent一旦完成不改写/补齐正文。

### 6.2 唯一新 Domain Event

新增 `work.task_changed` / producer `work` / aggregate `work.task` / schema_version=1；Header scope为同Project、aggregate_id=TaskID、aggregate_version=成功后Task.version、aggregate_sequence缺省，OccurredAt等拟Task.updated_at。旧Work两schema及原WorkEvents返回值不改。

新增 `TaskEvents`，精确方法 `RegisterTaskEvents(*event.Catalog)(TaskEvents,error)`、`Valid() bool`、`NewTaskChanged(event.Header,TaskChanged)(event.Event,error)`、`Restore(event.Header,[]byte)(event.Event,error)`、`DecodeTaskChanged(event.Event)(TaskChanged,error)`。只能同catalog typed definition构造/Restore，注册必须在Outbox.New Seal前；缺定义/重复/错误catalog明确拒绝。

payload闭集：`command_id:TaskCommandID,actor_user_id:UserID,task_event_id:TaskEventID,milestone_id:MilestoneID,sprint_id:SprintID,change,changed_fields,position`，全部必有；change恰created/updated/reordered，独立Task类型/校验不扩旧ChangedField。created字段数组固定 `[description,manual_rank,plan,priority,title,type]`、position非null、Header version=1；updated字段为真实普通变化集合（不含manual_rank）、position与priority是否变化同有同无；reordered字段固定[manual_rank]、position非null，后两者Header version>1。position遵守§6.1，不含正文/key/raw rank。no-op不产生此schema。

### 6.3 真 producer、Project gate 与旧分支

仍只有同一 `work.Authority` 登记producer work。`work/events.go`只增两个精确Task分派入口（DiscoverAppend和ValidateAppendInTx各一处），Task实际代码放新task_events.go；仅精确event_type/schema/aggregate三元匹配才进新支，不能扩大为任意work.*。旧Structure分支保原currentAccess/NewFact、header、planned/completed、issuer、opaque、locks和postimage规则。

Task Discover在事务外被PrepareAppend调用，只可发对应当前持久planned/合法completed的依赖；先从Header同Project做真实当前Read（短Tx、User/Project锁），再有界读本域command，验证原writer、summary/Header/payload digest/revision和task_event_id。它不是外部读API，不凭合法schema+人工canonical就造计划。

Task依赖域 `work.task-planning.append-v1`，binding覆盖Actor完整当前身份、Summary、CommandIdentity、TaskCommandID、plan_revision、TaskEventID、两Stage、完整锁canonical+mode。opaque仅严格 `{kind:"task_planning",command_id,plan_revision,task_event_id}`；不携正文/key。InTx先同Store活Tx、own issuer与Task闭集/opaque目的、已绑定完整锁及必要User/Project/Schedule/Task最小锁、当前Project Read，**再第一条Work事实SQL**。此后仍重新按实际record生成binding/opaque/locks并逐字段精确比较；前置检查不能替代最终exactcompare。

CurrentAccess允许同义历史completed但只验不可变command/event计划，不能要求当前Task仍等旧postimage；NewFact必须仍planned，并真实Mutate、同TxTaskpostimage/真实TaskEvent record/两个组与query后代数全部匹配。没有TaskEvent、不同operation/actor/version/time/payload、旧revision、缺旧writer或仅手工TypedEvent均不得Append。已completed但Outbox旧event缺失时拒绝NewFact重建，不把历史授权变成再次发布。

`project/work_event_authority.go`仅在原两对闭集中新增 `(work.task_changed,work.task,schema1)`；沿原purpose/issuer绑定Actor/Event/Project与CurrentAccess/NewFact，原Read/Mutate门禁不变。Project不读取Work私表、不import Work实现；`project/events.go`现有producer=work分派已经覆盖，不修改它。无新Outbox接口、handler、consumer、Audit或生产root注册。

## 7. 精确写域、兼容与交接

下表是本规格拟实施的**21路径闭集（20技术＋README末件）**；当前只写本卡，不授下表业务写入。每项由root指定的同一业务writer负责，独审实例不得参与实现。必要新增原因已对应上文，不为将来模块预铺文件。

| # | 路径 | 本卡唯一责任 |
| --- | --- | --- |
| 1 | `internal/central/work/contract/task.go` | 新Task DTO、枚举、过滤/presence、三命令/读口/摘要 |
| 2 | `internal/central/work/contract/task_test.go` | 严格编码/cap/摘要/Clone/局部invariant纯测试 |
| 3 | `internal/central/work/contract/task_events.go` | 本卡TaskEvent schema与唯一TaskChanged typed定义 |
| 4 | `internal/central/work/contract/task_events_test.go` | 两历史payload/typed Header/安全闭集 |
| 5 | `internal/central/work/task_service.go` | 独立TaskDependencies/构造/Stop/Drain |
| 6 | `internal/central/work/task_commands.go` | 三命令/Lookup/发现-计划-终局/Unknown |
| 7 | `internal/central/work/task_repository.go` | Task五表读写/局部错误与generation、最小计划记录 |
| 8 | `internal/central/work/task_reader.go` | 当前Get/List、cursor、同Tx真实membership |
| 9 | `internal/central/work/task_events.go` | Task真实planned/canonical/history producer |
| 10 | `internal/central/work/task_test.go` | 计划/锁/纯控制流/constructor/Stop与新Project gate无资源组合 |
| 11 | `internal/central/work/events.go` | 仅两处精确Task分派，旧Structure函数体语义保持 |
| 12 | `internal/central/project/work_event_authority.go` | 仅新Task三元闭集，旧purpose/issuer/两stage保持 |
| 13 | `internal/central/foundation/fault.go` | 六Task code及Known，原code和安全机制保持 |
| 14 | `internal/central/foundation/task_fault_test.go` | 新六码＋安全投影/Unwrap/未知码负例，不改旧scalar tests |
| 15 | `db/migrations/00022_task_planning.sql` | 唯一预留、五表与既有Sprint复合unique，真实升级 |
| 16 | `tests/work/task_fixture_test.go` | 新private真实组合/故障和必要同Tx观察，复用已验PG/COMMIT proxy |
| 17 | `tests/work/task_planning_test.go` | migration/planning/filter/authority/atomicity/events |
| 18 | `tests/work/task_concurrency_test.go` | rank/并发/membership/Unknown真实断言 |
| 19 | `tests/work/fixture_test.go` | 只将newDatabase的schema-wide硬5表改为核原5个命名表各存在；不得吞migration error或改真实authority/COMMIT helpers |
| 20 | `tests/work/structure_test.go` | 仅TestWorkStructureMigration正常分支显式through00021；保旧fresh/populated00020→21、恰五表、DDL失败/约束全部断言 |
| 21 | `docs/development/backend/README.md` | 技术接受后root单独移交文档末件，不先写完成 |

#19 仍先实际migrate最新schema再核原五表，不把5改成“≥5”就放弃命名身份；原错误不吞。#20历史迁移测试准确锁定其要证明的00021，不能按最大migration变化偷改旧事实；Task新Migration必须独验当前fresh/populated00021→22并证明旧表数据/约束保持。其余Structure真实回归使用current最新migration，因此不能只跑历史prefix来回避00022兼容。

新的Task fixture必须在同一catalog Seal前装Project＋旧两Work＋新Task事件、同一真实Work Authority producer；旧assemble仍只装原两Work也应可工作，不为它强填Task依赖。可在新task_fixture中按已验组合重建必要小装配，复用真实Account/Project/持久test-only Skill helpers；不改旧assemble/TestMain/脚本或引入“已停Object”的假实现。

没有 Structure contract/service/reader/rank/repository、project/events.go、D01设计、HTTP/OpenAPI/前端/App、go.mod/sum、外域SQL、生产Audit/handler、公共测试支持脚本的隐含写权。共享Foundation/Project/migrations/Work以及tests/work编译集合会改变Model UI固定输入；即使目录不同也不得在其资源/Go窗口安装。本卡可先独立scratch候选，main安装/闭包必要差量重绑/Go/cache/资源由root串行移交。超表路径须先给必要原因/最小替代并由root明确调整，不能自行加通用层。

## 8. 验收门槛与预算

实施/验收分别遵守[Go技能](../../../.agents/skills/agenteam-go-development/SKILL.md)、[verification技能](../../../.agents/skills/agenteam-verification/SKILL.md)及[团队规则](../agent-team/README.md)。本规格无动态结果；编译、发现、body、独立probe、资源退休分别记账，no-tests/Skip不是PASS，原失败和未到达分支不可覆盖。

### 8.1 静态与纯/离线

逐项审21路径及旧两分派差量；纯测试覆盖全部字段边界/孤立surrogate/大小写/重复key/null/presence、两个同时满额大文本的最坏escaping与lookup cap、未知state/type/priority/typedID、两种assignee空值语义、safe fmt/JSON cause、三个摘要每字段变化与Session/Request排除、Clone、防旧rank覆盖、query排序/筛选digest、typed schema闭集、constructor nil/typednil/Store绑定、计划issuer/revision/ctx/Stop真join。新六Fault码不能被Safe静默降为INTERNAL_ERROR，旧码行为仍需实际回归。

经root单授的每个离线命令≤45s，`GOTOOLCHAIN=local GOPROXY=off GOSUMDB=off`，真实Wait/child/watchdog/owned双空；source/工具/配置输入固定。适用 `go test -race -count=1 ./internal/central/foundation ./internal/central/work/...`、`go vet ./internal/central/foundation ./internal/central/work/... ./internal/central/project`，以及下列旧Project pure精确selector；integration只race-c编译tests/work，再在generated testmain/init闭包确认后root另授精确-list。可复用已验helper本体，selector扩展与Task/旧输入图必须单独封；不以旧binary/hash冒当前已编译。

旧Project pure selector：`^(TestOwnerAuthorityOrdersLockSessionAndFacts|TestForeignOwnerAndUninitializedRowsAreNotGrants|TestMissingOrdinaryProjectDoesNotExposeTombstone|TestEventFactUsesTypedCanonicalPayload|TestAppendPlanBindsCurrentSession|TestModelProjectEventUsesSameCompletePlanForReadAndMutate|TestModelProjectEventPlanClosesActorSummaryIssuerAndPurpose)$`。Foundation旧pure全包和Work旧pure全包是现共享入口回归；不调用整仓脚本。

### 8.2 七个新真实 PG top

integration标签、无t.Parallel，每次只一个精确top，全部矩阵不得用“源码里有断言”代替到达。

| selector | 必须实际证明 |
| --- | --- |
| `^TestTaskPlanningMigration$` | fresh全00022、populated00021→22、重跑、失败DDL原子rollback；五新表/旧五表/复合FK/合法枚举与active-assignee/rank/版本约束、零跨域FK；旧数据/时刻不变；失败22不标applied |
| `^TestTaskPlanningPersistenceAndPaging$` | 真实Owner/Project/Structure父层→三命令→重建服务读同一事实；显式type/priority、重复title、满description＋plan roundtrip；205项默认50/续7完整无重复，全部7filter及组合、assignee三态/字面%/_、五元跨组序、换Session/limit/cursor key、所有相关变化stale、不假空当前错误 |
| `^TestTaskPlanningAuthorityAndReplay$` | 无Actor/AgentRun/Service、当前Session、非Owner管理员、跨User/Project/command/target、异义/presence、历史先version/terminal/能力；initial todo/assigned明确否定；current正向、completed拒新写；archiving/archived历史vsplanned、deleting/未初始化拒绝；不冒正式lifecycle |
| `^TestTaskPlanningAtomicityAndEvents$` | TaskEvent后、真实Outbox后、Activity后注错，Task/所有组/query/history/event/receipt/touch原子回滚；三种changed与no-op真实payload；伪计划/typed事件/TaskEvent/Actor/time/version/header/外Store/结束Tx/缺锁/旧issuer/revision拒绝，首个WorkSQL前currentRead；旧事件清除不能重建；无Work Audit/delivery handler |
| `^TestTaskPlanningConcurrencyAndRank$` | A final真Command EX＋B同key实际waiter后历史replay、不同key旧version恰一胜、两个Service；三/四对象低高密度rank及priority迁组，全部spectator字段/版本/time，source+target/query精确代数；no-op/replay区别；内容更新不能写回旧rank；计划适用性重做和共享三轮耗尽，不吞Forbidden |
| `^TestTaskPlanningMembership$` | 同Store caller活Tx真实false→create后true、全部state成员都算；异Project/缺Sprint/缺lock/结束Tx拒绝；membership Tx先持Schedule EX，另一Service create成为exact实际waiter，同Tx两次读稳定，释放后真create并见true；反向create先赢，membership真实等待后见true；false不冒DeleteSprint成功 |
| `^TestTaskPlanningCommitUnknown$` | 准备/最终实际COMMIT frame hold×真实commit/rollback四组合；原writer/confirmation/Lookup独立Tx握手，取消Lookup errors.Is+zero，迟到真实结果＋显式同请求恢复恰一次；writerAttempt/Cause不被确认SQL错/撤权/Unknown替换；known commit后delivery cancel保成功，NotCommitted取消保cause；Stop/Drain真join且proxy独立终局 |

所有锁等待必须从实际caller Tx取得PID，核完整expected key、实际SH/EX mode、granted=false及精确blocker，先握手再取消/释放；固定sleep或观察另一个Tx不得代替。COMMIT proxy真实读写完整frame、exactwriter、commit/rollback和各listener/handler/child/finally都注册与join；clone模拟CommitResult纯测试只能作为离线控制流证据。

为核reader/filter/membership对未来canonical值不抹写，可用test-owned SQL种入本卡不能创建的state/assignee对照；须符合真实局部DDL/typed ID和parent关系并明确来源。它只证明持久查询行为，不能当Agent存在/同Project授权、Blocker、状态转换或assignee正向绑定；这类行不得经fake adapter获得本卡写入成功。

容量测试可用受控持久fixture填充到4096/65536，但必须满足真实FK/局部字段与group/query generation，再调用实际命令；不靠修改cap常量或fake count证明。生成数据限在本top、清理/证据计入预算；若无法按既定预算完整执行先STOP报告，不省边界或静默延时。

### 8.3 八个必要旧 PG selector 与独立不同构造

恰以下旧选择器（每个独轮）：`TestWorkStructureMigration`、`TestWorkStructurePersistenceAndPaging`、`TestWorkStructureAtomicityAndProducer`、`TestWorkStructureCommitUnknown`、`TestProjectB02OwnerPortRequiresCallerTransactionLocks`、`TestProjectB02ReceiptRequiresCurrentSessionAndReplaysAcrossRenewal`、`TestOutboxAtomicAppendCurrentAuthorityAndReplay`、`TestOutboxMissingLocksPoisonAndMappingChangeRollsBack`。理由分别为旧历史迁移/新schema兼容与旧六命令读写、两Work旧producer、原U1/Unknown、同Tx权限锁、当前历史Session、Outbox原子历史与缺锁/mapping。只选择这些旧代表，原Structure其余旧实际结论保原版本，不冒当前整组全跑。

未参与实现者须全文STATIC并实际执行两个自有不同构造，不能只复用作者oracle：

- A（`TestTaskPlanningIndependentAuthorityMembership`）：真实撤权/归档Tx持门槛，当前Execute与历史Lookup/Get的实际waiter，另一User/Project同key不混；membership持Schedule后create真实等待与反向赢家；同Tx重新观察及完成后事实，不用“fake空成员”。planned/completed归档边界与Session更新历史分别验。
- B（`TestTaskPlanningIndependentCommitRank`）：另一种四对象密集rank和priority源/目标组几何，真实final COMMIT hold期间取消原Lookup、另一key rank/priority竞争；分别commit/rollback后核原writer因果、原receipt、两组全部spectator字段/代数、Project query代数、TaskEvent/Outbox恰一次及no-op/historical Activity差别。原更新大文本兼rank变化至少一次，防只验证作者的小文本向量。

两probe仅scratch单虚拟tests/work目标，经root单授source/overlay/离线/精确发现/资源，各层STOP后才消费。最终验收按固定源版本和原件组合记录，不将作者selfcheck或本规格作者的STATIC自查称独立验收。

### 8.4 实际运行与退休预算

沿已验Structure **PG-only两ID** fixture基准，冻结一个network＋一个container及其精确端口/目录/child/COMMIT proxy/listener清单；不为凑旧七ID基准启动MinIO/browser/Node，扩资源需root另授。每top120s含setup/body/cleanup，执行105s＋cleanup reserve15s，Go package hard timeout≤6m；启动前fresh可用磁盘≥5GiB，**不是内存上限**。owned/resource cleanup后补充host TCP delta尾部两次清空观察≤75s，不是整个运行总暴露窗口或全短连接所有权证明。

launcher→driver→helper→Go每层actual Wait，reader/handler/monitor/watchdog/goroutine真实join；固定两ID两次exact不存在、owned/runtime双空、输入前后一致、历史ID不重用、失败disposition不改写。non-owned daemon shim只观察、不冒wait；工具外层terminal与落盘监督器Wait单列，环境通知不补造exit。冻结false/prepared不授资源，root必须另给每轮精确grant。卡片/source/static/compile/list都不能替代真正body或退休。

## 9. 后继未绑定责任与完成门槛

| 后继 | 本卡留下的真实边界 |
| --- | --- |
| D10 Agent/assignee、状态/reviewer | 当前只有未指派backlog新写；合法Agent同Project、引用保护、todo/blocker/reviewer、Service/Agent权限后继真实接通，无默认成功adapter |
| Sprint Delete/Complete/rollover | membership的Task canonical SoT已由本卡负责，但正向命令、所有Task清单、占用、current pointer、TaskEvent迁移与竞争另验；planned+空Task才是未来删除必要条件，不替代完整授权/执行 |
| D22/D23 occupancy/Dispatch/claim补偿 | active含waiting和取消未join、history/pending不能假空；future rank维护须绑定pending claim逻辑位置保护/映射。现无Move/claim/Complete/TaskDelete API |
| Task Delete | 真实无Execution/Dispatch历史、双向dependency、resolved/unresolved Blocker、除creation以外TaskEvent都必须同Tx验证；本卡已有fields_updated即不是纯creation草稿，不靠当前空闲放行 |
| Work清理 | 没有Project lifecycle participant，整个Work数据/命令正文/事件与引用的合法停止清理后继负责，不造empty Inspect/no-op Cleanup |
| 完整Timeline/context/Tool/HTTP/UI/root | 本卡仅必要TaskEvent持久与库口；无comments、执行context/运行日志、前端或生产Task服务/consumer；ready503不变，D11/平台/E01未完成 |

完成须精确20技术源全文独审、必需离线、七新＋八旧实际、独立A/B和所有实际退休/失败处置通过；再由root移交README #21，独核文档与真实范围，最终固定21路径版本组合及永久证据。root负责三协调页/计划后续状态/Git，本文行政header更新不提前标本Task卡接受。发现未冻公共接缝或实施无法满足cap/锁/原子性须先交具体差量，不改变用户业务规则以“做出可运行”代替。

## 10. 来源固定、自查与移交

本规格只作工程冻结；来源采用当前已接受Structure公共实现与必要正式规则的精确指纹。下表固定必要来源；不是Go依赖图或全部上游已验声明。本文的设计自查不是独立SPEC审查，待root和未参与实现者完整核后才可给唯一writer实施授权。

读取基线为root报告已交付的 `a64fb5e783373255a4b0ae7936e5685f25f458c6`。本作者未运行Git；以下对必要当前文件做静态字节指纹，与已验源身份核对。本次给Structure卡增加的当前接受header可以逐字移除恢复下表原812490b6，不改变该规格业务正文。

| 固定来源（仓库相对路径） | bytes | SHA-256 |
| --- | ---: | --- |
| `docs/architecture/project-work-management/task-domain-model.md` | 14768 | `6cec22a2d2022613c518dac5054c76c4151e6a2272b9a815206c183d5c376624` |
| `docs/architecture/project-work-management/task-state-machine.md` | 13518 | `33a78a2d321c54e26181c11461967fc1215051ac4d4a78f9af03c97c7a0366c8` |
| `docs/architecture/project-work-management/task-event-timeline.md` | 10133 | `c6ed1e30e4fab1b203096542e30677a470dfe062f0f06c663ad1445a3ef372a8` |
| `docs/architecture/project-work-management/sprint-lifecycle.md` | 15564 | `8286667a16cbdebef8e26a12c853d20e6d8f578fbd3353748d70151c3ce68e31` |
| `docs/development/work-items/d01-contracts/foundation.md` | 17854 | `8761c3d2871c5a8121b31de978fd96530466eafd3cae9a2a3c1a5267827edc60` |
| `docs/development/work-items/d01-contracts/domain-lifecycle.md` | 19526 | `0119faf85081fa60719f10a73a5483343021bd34f26da1be06af79fce1317c71` |
| `docs/development/work-items/d11-work-structure.md` | 60230 | `812490b67b956469e274eecfde851eb084e47648fe0e3944eb5668fa8961c27f` |
| `docs/development/agent-team/d11-work-structure-verification.md` | 12595 | `9628cd5ea675adc8777440842055b4996a9ae162ad120a502fafa8a5ac37707a` |
| `internal/central/work/contract/structure.go` | 30084 | `485ddab3e8f53313ff1d886eff3c815c88a1f5b469d0fbd508801bbef81d8d39` |
| `internal/central/work/contract/events.go` | 10590 | `3db5751b8fa349755c6fa80b385f48829a5ff7adf5186847278813c635490ae9` |
| `internal/central/work/service.go` | 3559 | `244d368c4eceec23506f28356144a1c327ddf059d09352225960f934a3c3ae3b` |
| `internal/central/work/reader.go` | 11714 | `e33e75cc10fd73eb718cf7b856b776904f24c2b797bddbacf3275940bb5191ad` |
| `internal/central/work/structure.go` | 33511 | `5e8202d24084f2360687c8e898ab718aa5505a3d8f439abf090b97036f3eefdf` |
| `internal/central/work/repository.go` | 24060 | `9a19e48423883cd8cc85015e5ace1ba0b1a890ebc1fb2bc14d84f156383afbf2` |
| `internal/central/work/rank.go` | 4259 | `dab61abaff9e0309cc59ac33f52432c5480a344fd6013ee14269a68bacafcce8` |
| `internal/central/work/events.go` | 10263 | `b98bb27b72ba9b483c6d4428c06ce3bb9aa7b6064b3386f1984c9bb308a1f3d5` |
| `internal/central/project/work_event_authority.go` | 3296 | `9317a994514d12be5e8f1c10ede34dc82da65453c7f23d82b73d09b13aa83b79` |
| `internal/central/project/events.go` | 12568 | `dad67f6379891cd75cd2ba3122d269f62fe47a1933cde9d96d80c989e0f836ef` |
| `internal/central/foundation/fault.go` | 5460 | `94a339976fb8f1e1173256aa840dd1273644cafd37dc6a3e96e33d1cb8ff09de` |
| `internal/central/foundation/lock.go` | 5196 | `5f328d35a0bf313f589654c9e52b5b6b0b9f8f3f3b8502e9d1f612b588220384` |
| `internal/central/foundation/page.go` | 1860 | `3be782a4c61729a5a2b13e195e5653ad1070e883765035b5cbb1f4f9e85c3c52` |
| `internal/central/cursor/cursor.go` | 6023 | `c9f13d1cc4f40966cf545d0ea6dcc0f5c5ffc821982e54f3e680e1a604150e5f` |
| `internal/central/outbox/contract/authority.go` | 11951 | `1c78ac9f0e8c9ff996e9b9048f65d1f8d7bc005989d1021b2e5e13c2f721f5a0` |
| `db/migrations/00021_work_structure.sql` | 5127 | `a7c541546af5bf7892abeebee1a46089683ebea7a81e5951403752ee50d392ad` |
| `tests/work/fixture_test.go` | 37961 | `fa437258e76de68065cf90ccb234d34e20d78b4a0fa0430c6e5e45b9a424585c` |
| `tests/work/structure_test.go` | 39046 | `86c051609e14294a9989d264ecbcb41e9eca3024e6d380c015ee34b0b42a8dfa` |

现有接口核定：`work.NewAuthority(Store,project.ProjectAuthority)`、`work.NewReader(Store,*Authority,cursor.Keyring)`；`Reader.ReadPlacementInTx(context.Context,foundation.Tx,identity.Actor,contract.ProjectID,contract.SprintID) (contract.Placement,error)`。Store沿现有WithinTx/InTx/AcquireAll/RequireHeldLocks，Activity仍TouchActivityInTx；不扩大外域接口。根确认当前全部Structure18已接受不等于Task依赖中的Agent/Execution/Dispatch已绑定。

作者静态自查：21拟实施路径为20技术＋README；三个命令＋Lookup、两个当前读口＋同Tx membership；七新/八旧PG＋两个不同构造独验；raw512前置、独立Page cap、同Tx TaskEvent/Outbox、历史与新写门禁、真实Unknown/U1、当前五表与历史through21兼容均在本文自含。来源及两处行政header差量另有停止原件；未执行Go/Node/编译/测试/资源，不把作者自查替代完整独立SPEC接受。
