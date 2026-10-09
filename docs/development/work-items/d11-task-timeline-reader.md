# D11 Human Owner Task Timeline 只读库

状态：rev1 草案，只授权本文与本树检查点；待独立SPEC审查，不是产品接受。正式基线 main `3cea6076`，独立树 `/workspace/agenteam-task-timeline`、分支 `ai/task-timeline-reader`。未实施、未占迁移号。

## 1. 完整结果与实际依赖

已有Project的当前Human Owner可从真实Task历史读取四类事件，稳定双向分页，每页重新验证Session/Owner。交付typed只读DTO、既有TaskReader的新方法及纯/真实PG验收；不包含HTTP、Tool、UI、Agent Context、comment、流转或执行日志。

依据：[Timeline](../../architecture/project-work-management/task-event-timeline.md)§4/15–20/22–24、[开发计划](../development-plan.md#d11-work-management-领域)、[Task planning](d11-task-planning.md)、[Blocker服务](d11-task-blocker-service.md)与[Foundation](d01-contracts/foundation.md)。按[团队流程](../agent-team/README.md)和[设计技能](../../../.agents/skills/agenteam-design/SKILL.md)先定契约/验收，再实施。

| 已验依赖 | 本卡消费与限制 |
| --- | --- |
| Task planning及B0-P，已进入正式基线 | 正式Owner命令产生的task_created、fields_updated、blocker_added、blocker_resolved；真实Task、History、同Tx/锁。不是Agent或状态流转能力。 |
| Account/Project、PG/Cursor | 当前Session/Owner Read、初始化/lifecycle、同Store活Tx、HMAC cursor与现有标量。管理员无Owner旁路。 |
| 00022/00023 | 四类都位于agenteam_work.task_events；共用(project_id,task_id,created_at,id)索引，00023仅新增blocker命令外键分支。优先零迁移，不改旧SQL。 |

不消费Agent F1、Execution/Dispatch、真实Skills/Object Runtime或D13。测试Project初始化复用已接受的隔离Skill持久fixture，不冒生产Skills绑定；Object join等停项保持。

## 2. 精确DTO与Go接口

新增在work/contract，不扩大旧TaskEventType、旧decoder或既有TaskReader interface。

```go
type TaskTimelineEventType string
const (
    TaskTimelineCreated TaskTimelineEventType = "task_created"
    TaskTimelineFieldsUpdated TaskTimelineEventType = "fields_updated"
    TaskTimelineBlockerAdded TaskTimelineEventType = "blocker_added"
    TaskTimelineBlockerResolved TaskTimelineEventType = "blocker_resolved"
)
type TaskTimelineOrder string
const (
    TaskTimelineAscending TaskTimelineOrder = "asc"
    TaskTimelineDescending TaskTimelineOrder = "desc"
)
type TaskTimelineFilter struct {
    Types []TaskTimelineEventType `json:"types,omitempty"`
    Order TaskTimelineOrder `json:"order,omitempty"`
}
type TaskTimelineEvent struct {
    Planning *TaskEvent `json:"-"`
    Blocker *TaskBlockerEvent `json:"-"`
}
type TaskTimelineReader interface {
    ListTaskEvents(context.Context, identity.Actor, ProjectID, TaskID,
        TaskTimelineFilter, foundation.PageRequest) (foundation.Page[TaskTimelineEvent], error)
}
```

实现只给既有`*work.TaskReader`增加该方法，放新文件，复用NewTaskReader同Store/依赖校验和私有read；无新constructor/worker。独立新interface不给旧consumer/mock增加实现要求。

Filter的Go零Order/JSON省略Order均归一desc；JSON显式空字符串/null/未知值拒绝。nil Types/JSON省略Types是全部四类；非nil空slice、JSON空数组/null、重复/未知类型拒绝。Types为集合，绑定摘要按固定四类顺序归一，排列不改变语义；不保留调用者slice。Marshal输出显式归一order，全部类型可省略types。Unmarshal失败不改receiver；各新增DTO有Validate/Clone、严格Marshal/Unmarshal及Decode，enum沿现有标量规则。

Event恰一个分支非nil，JSON直接使用该分支已有完整event record，不增加planning/blocker封套或新ID marker。按type四类分派旧严格decoder；旧TaskEvent仍拒绝blocker族，反之亦然。公共wire字段仍为id/project_id/task_id/task_version/type/actor/operation_id/correlation_id/payload/created_at。两族保留各自typed command ID、Human actor、payload、Validate/Clone；不能用当前Task标题或Blocker说明重建/改写历史。直接Go双空/双分支拒绝。

Filter原JSON上限16KiB，Event原JSON/Marshal上限16KiB（两旧族现有上限）；拒绝unknown/duplicate/case-variant字段、UTF-8/孤立surrogate、尾随值、required/null及非法canonical UUIDv7/Version/Instant。新类型直接Formatter/LogValuer固定`work_task_timeline`，业务JSON保留payload，不冒脱敏；调用者不得把Page/业务JSON送普通日志。

Page沿foundation：DefaultPageRequest默认50、1..200，显式0/null不自动修正，cursor≤8192B。最多读取limit+1条；空页Items为非nil空slice。页上界可达200×16KiB+16KiB，不以单event cap截断合法页。本卡不新增通用Page decoder，未来HTTP封套/路径另卡冻结。

## 3. 顺序、水位与cursor

排序为持久(created_at,id)，asc两列ASC，desc两列DESC；UTC微秒和UUID原生顺序，不以TaskVersion/operation/Outbox代替。NextCursor表示相同方向的下一页：asc严格大于、desc严格小于边界，边界行不重返；未来HTTP的after/before只映射方向，本库不同时接受两个边界。

首读在真实锁/授权内取当前Task.Version为W，各页只取task_version≤W。续页沿原W，不因后续append或Project query_generation改变失效；当前Task.Version<W则CURSOR_STALE，禁止静默降低水位。已有writer同Tx提交Task version与history，首读Task SH/ProjectSchedule SH和writer EX串行。新第一页重新采W才纳入后写；W冻结本四类append-only事实集合，不冻结当前权限，不声称未来可编辑comment也满足此语义。

Cursor复用keyring：Scope=InProject(project)；QueryDigest为canonical-v1 `{format:1,kind:"work.task-timeline",project_id,task_id,owner_user_id,types:<排序/展开子集>,order:<asc|desc>}`。不含Session/limit/正文；同User换有效Session或limit可续页，换Owner/Project/Task/types/order拒绝。

Order精确created_at:asc,id:asc或created_at:desc,id:desc；Position恰[Instant(created_at),UUID(event_id),Integer(W)]，W为1..MaxInt64，OrderGeneration必须nil。签名/kid/格式、绑定、scalar数量/类型/canonical、W越界或携带generation错误均CURSOR_INVALID。Cursor不是授权。

SQL在scope、可选type filter之后应用seek/水位/排序/LIMIT。无types filter时禁止用固定四类IN隐藏未知历史，遇不支持/损坏行整页安全失败、无partial。显式types只返回用户指定集合；任何未来writer/type接入必须同时扩展本Reader默认全量覆盖和兼容验收，不能令默认API静默漏新族。不能把future/comment/Agent/System映成旧Human事件。

## 4. 当前授权、事务、错误与资源尾

每页沿既有TaskReader.read进入同Store活Tx，Normalize后一次取得User SH、Project SH、ProjectSchedule SH与目标Task SH；先当前Session/Owner Read，再loadTask(project,task)，然后验证cursor绑定/水位、查询历史。不新增Command/Activity/Outbox锁，不写任何事实，不对event逐条另开Tx；复制DTO后才从方法返回。writer原ProjectSchedule EX/Task EX与本读锁形成水位边界，不能先取Task再补低序锁。

纯语法先于DB：ctx取消/Actor/ProjectID/TaskID/filter/PageRequest错误使用已有安全错误；未构造Actor UNAUTHENTICATED、合法AgentRun DEPENDENCY_UNBOUND、Service FORBIDDEN，非法ID/filter/page INVALID_ARGUMENT。Cursor原始签名/格式可先纯验证；不能以此跳过每页实际授权或查询历史来回答无权者。

实际授权沿RequireOwnerInTx(Read)：失效/撤销Session返回原安全码；Project不存在或非Owner（含非Owner管理员）NOT_FOUND；初始化pending、deleting沿PROJECT_NOT_ACTIVE；archiving/archived允许历史读。授权后的Task缺失或属于其它Project统一TASK_NOT_FOUND，不泄漏外Project Task。已通过授权且cursor不匹配scope/owner/filter/order等为CURSOR_INVALID，W高于当前版本为CURSOR_STALE。空历史/合法filter无匹配返回合法空页；依赖未绑定、DB/锁/ctx错误原样安全映射，禁止当空历史。

扫描同时校验完整行与其typed event：Project/Task必须匹配查询，ID/Version/Actor/Instant/payload通过旧严格decoder；planning operation_id非空且blocker_operation_id为空，blocker恰相反，correlation等于对应operation，不用COALESCE掩盖双列矛盾。未知type、非法payload/actor/foreign-key分支或row不一致整体INTERNAL_ERROR，不返回部分Items或跳坏行。所有候选，包括为判断NextCursor读出的第limit+1行均验证；坏行不得仅因是lookahead而被忽略。成功页数量超过limit即内部错。

Rows每条Scan与最终Err均检查，所有路径真实Close后再退出Tx；ctx传播到锁与SQL，返回前WithinTx实际结束。无额外goroutine、后台worker、callback或network请求，不以超时可见Promise替代实际SQL/Tx结束。输出使用Clone，错误时返回零Page，不泄露payload、cursor、Session、SQL或driver错误正文；直接DTO的日志格式按§2固定。取消仍保留errors.Is(ctx.Err)关系。

## 5. 实施闭集、兼容与所有权

SPEC独审接受后由root确认产品写权；当前只有本文和本树current可以编辑。拟定新文件：

| 路径 | 唯一内容 |
| --- | --- |
| internal/central/work/contract/task_timeline.go | 四类型、filter、union、独立Reader interface、严格codec/clone/安全格式 |
| internal/central/work/contract/task_timeline_test.go | 真实codec正反与不可变/边界纯控 |
| internal/central/work/task_timeline_reader.go | 既有TaskReader的方法、cursor绑定、SQL/scan，无新constructor |
| internal/central/work/task_timeline_reader_test.go | 查询/错误/Rows尾的必要纯控；不能冒真实PG |
| tests/work/task_timeline_test.go | 作者真实PG矩阵及最小自有fixture扩展 |
| tests/work/task_timeline_independent_test.go | 独立者在稳定源上编写/本人执行，作者不自称独立 |

Task planning/Blocker/Account/Project/PG/Cursor的已有实现和旧事件decoder只读；不修改其writer、旧Reader interface、migrations、schema、HTTP/root、shared harness、依赖锁文件或生产时钟。若实际代码需要超出新文件或上游契约，先给root说明精确缺口再调整SPEC；不借实现顺手重构。已有Task Get/List与旧两族decoder结果不变，Future Timeline consumer绑定归后续卡，本卡不加未实现HTTP的stub。

全局迁移编号归root统一协调；本卡当前零迁移，00022/00023索引已覆盖scope/时间/ID顺序。可在实际PG查看查询行为，但没有benchmark/吞吐接受声明，不借性能猜测开新索引。Work UI在另一树的冻结输入/失败记录不受本卡写入影响；全局current/tasks保持原唯一写者协调。最终必要状态随完整结果保存，不另建永久流水档。

## 6. 可执行验收矩阵

纯控按实际变更定向运行contract/work包，验证严格四族JSON、双空/双分支、未知/重复/大小写/null/空集合、过限与非法UTF-8、receiver失败不变、Clone、排序集合归一与cursor绑定/标量错位。真实PG验收使用tests/work既有正式服务fixture：Project与Task及四类历史均由正式命令产生，不手种正例事件或stub持久事实；fixture有限Skills准备能力不升级为生产绑定。

| 精确作者top | 必须证明的完整结果 |
| --- | --- |
| TestTaskTimelinePersistenceAndPaging | 正式Create/Update/AddBlocker/ResolveBlocker产生全部四类；持久记录/operation/correlation/payload逐项与原receipt相合，重建Reader后相同。真实累计超过200条，默认50、续页改7、limit200、asc/desc、逐类/组合filter无重漏；limit+1与空末页正确。同一User新有效Session可续、反序types等价，换scope/filter/order/Owner拒绝。原命令重放零新增历史。 |
| TestTaskTimelineAuthorityAndCancellation | 每页当前权限，页间Session撤销/Owner变更，跨Project Task及非Owner管理员不见历史；pending/deleting拒绝、archiving/archived读允许。实际持有目标锁后取消等待；真实SQL执行中取消后Rows/Tx/测试goroutine全部实际结束，后继同锁操作成功，零业务/Activity增量。 |
| TestTaskTimelineIntegrityAndCompatibility | 在本测试拥有的真实正例上施加严格范围SQL负向破坏（未知type、坏payload/actor/operation分支、非法cursor水位），整页拒绝，不把部分结果/空页冒成功；每项独立事务/隔离Task，不影响其他测试。旧Task Get/List和两族codec门槛保持。 |

同时间分页必须有确定性刺激：使用tests-only的同一个Store wrapper绑定所有相关Authority/Reader/writer，Underlying仍为实际PG Store，WithinTx/AcquireAll/RequireHeldLocks/InTx/Rows全部委托原实现。只对既有无参`SELECT clock_timestamp()`的实际成功Scan覆盖返回值为预选有效同一微秒时刻（不早于目标Task已有UpdatedAt）；原SQL本身仍执行、其错误不隐藏。正式writer连续生成至少两条完全相同created_at、不同真实UUID的历史后，按二元组分页并穿插实际新命令，验证W排除后写、两方向都无重漏。该刺激不修改生产时钟或事后UPDATE正例历史；不声称证明真实时钟碰撞概率。若不能保留sameStore身份/真实Tx语义，先回报替代刺激，不降为手造Rows或依赖偶发同微秒。

独立验收由未参与者审稳定diff，并本人以不同编排覆盖跨页权限失效、同时间/水位append、坏行整体拒绝与取消实际join；最终精确top由独立作者确认，不能把作者top换名当独立。兼容只跑受影响旧Task/Blocker读写/codec代表；Object Runtime join等停止场景不在本卡恢复。

编译采用已锁Go1.27.1、local/off共享只读modcache与本树独占cache，先纯控/race编译及精确-list非零发现，不启动真实依赖。实际PG-only沿既有driver/supervisor原105秒+15秒退出尾、Go包6分钟与TCP/资源门槛，每个精确top独占fresh grant、启动前同进程statvfs≥5GiB；不新增fixture监督器或扩预算。若单top矩阵确实超过原期限，按完整结果拆精确top并先修本SPEC，不能默改超时/跳清理。每次报告business、actualWait、owned PG两ID双退役、runtime/desc/TCP及输入终态，窗口和Git由root协调。

## 7. 当前接受边界与下一步

本文rev1已形成可审规格；未实施产品/迁移，未编译或运行Timeline纯控/真实PG，不宣称任何新能力通过。已有依赖来自正式main3cea6076，待未参与者重点审union wire兼容、当前授权/错误先后、水位在既有锁下的真实性、同时间测试刺激与未知future类型政策。root采纳并完成独审/写域确认后可直接推进实现，无需重复向用户询问已授权目标。
