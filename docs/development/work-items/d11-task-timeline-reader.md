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
