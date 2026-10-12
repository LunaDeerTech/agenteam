# Scheduler 按项目停止与原调用返回

状态：本片已实现并完成限定 basic 与真实 PG 整链验收，受测版本 `898ed8ebd91e0a272ad3eec07ca8d4721f133969`；首轮 basic 编译失败保留如下。树 `/workspace/agenteam-scheduler-project-stop`，分支 `ai/scheduler-project-stop-20261012`，基线 `e4cf974eacd303c46e763cf9524db8ad61cec1b1`，迁移上限 75；本片不新增迁移。只停止一个真实 manager 所有的目标 Project runner；不清 Task assignee、不停止共享 Execution/Coordinator/Visitor，不证明 foreign 终局或完整 participant，不接 App 默认 Registry。

## 原调用链与固定接口

[真实 StopDriver](../../../internal/central/project/lifecycle_stop_round.go) 的 `start` 原 Store 实际返回 Committed 后进入固定 step。step 调用 `stopper.DrainProject`，其原控制事务再次核原 claim、Project Delete/stopping 与完整 manifest；原事务实际 Committed 返回、原 context/私有 scope 仍有效后才停 A。A 原 `runOne` 真正返回且没有 retained Unknown 后，step 消费本地结果；B 原 runner 继续。driver 最终只结算原 attempt，不据此完成 Project 删除。

```go
project.NewSchedulerStopAuthority(driver *LifecycleStopDriver,
    directory *SchedulerProjects, managerStore Store) (*SchedulerStopAuthority, error)
authority.WithConfirmedSchedulerStop(ctx context.Context, actor identity.Actor,
    cause contract.LifecycleCause, scope contract.ScopeRef,
    fn func(context.Context) error) error
scheduler.NewProjectStopper(manager *ProjectRunners,
    driver *project.LifecycleStopDriver) (*ProjectStopper, error)
stopper.RequestStop(ctx context.Context, actor identity.Actor,
    cause projectcontract.LifecycleCause, scope projectcontract.ScopeRef) (ProjectStopReport, error)
stopper.DrainProject(ctx context.Context, actor identity.Actor,
    cause projectcontract.LifecycleCause, scope projectcontract.ScopeRef) (ProjectStopReport, error)
```

Scheduler constructor 只接受 manager 内原真实 `*project.SchedulerProjects`，并把原 PendingAuthority Store 交具体 Project constructor；后者在本包内核 driver、LifecycleAuthority、目录 Authority 和 manager Store 为同一实例。旧 `NewProjectRunners` 保持兼容；没有 public getter、fake issuer、可替换成功授权或目录 wrapper。

## 授权、实际返回与 manager 状态

1. 公开 Actor/cause/scope 和可见 deleting/stopping 行都不是 issuer。Project owner 只认 [上游准备读取](d08-agent-retirement-preparation.md) 已接入的真实 driver 当前活动私有 scope，绑定原 Project/operation/process/attempt/fence/actor/cause；Archive、另 driver、伪 DTO、旧/嵌套 loan context 均拒绝。登记独立 scheduler-control loan 必须先于 Store 调用，原 step context可在仍活动时独立再次借用。
2. loan 内用原 driver Store 控制 Tx 获取 Project SH，再借原 live Tx 逐项核原 running claim、canonical owner、完整 manifest及 Delete/stopping。完整授权 callback 真正完成才产生内部候选；未执行 callback 的 Committed 替身不能成功。只有这个原 Store 方法实际返回 Committed 且原 context、scope仍有效才进入同步 manager callback；Unknown/NotCommitted、错 Store、取消、step早返均零 stop。不得靠之后查询把原 Unknown 倒填为成功。
3. 私有 scope 关闭先封新准入并取消，再等 Agent 读取 loan 和 scheduler-control loan 各自实际返回；control loan 覆盖原授权 Tx 返回尾及随后同步 stop/drain callback。只由 invoke 退出关闭 scope，loan只退出自己，不等待自己或调用 close。若 step先返而迟 Committed仍未回，driver仍等待原 loan，迟返回不能再停 A。Agent 公共 API、原完整配置与借用语义不变。
4. manager 同时登记整个控制调用，纳入其全局 Stop/Drain/Joined；不把控制 ctxerror算原事务返回。每次 `runOne` 在同锁 active 登记时取得独立 ctx/cancel，覆盖原 recovery 与 runner Run。授权成功后只标记 A `stopping`、取消 A 原 runOne 并调用其原 Stop；共享 owner 与 B 不停止。`startDue` 同锁拒 stopping；已排队迟启动者返回非 nil 取消，保留 prior Unknown，不能用 nil 早退抹掉原错误。
5. 不建永久 Project tombstone。授权成功同 manager 锁递增 discovery generation、重置 position/cycle；[原 discover](../../../internal/central/scheduler/project_runners.go) 在 List 前捕获 generation，List 原返回后先比较，过时整页丢弃，不先改 seen/游标、不创建 A，也不据旧页停止 B。之后仅 [原真实目录](../../../internal/central/project/scheduler_projects.go) 的 active-only 新页参与；Delete 中 A 不会复活。本片不处理 Archive/Restore 或任意缓存目录的重放。
6. A 只有 `!active && runner.Joined() && !uncertainProjectVisit(err)` 才删除 entry并释放 capacity；Unknown仍留原 entry/handle/error/槽位，不能自动调用停后的 `recoverOriginal`（其中含 advanceExecution）。RequestStop给当前本地快照，DrainProject再次正式授权/请求后等待原 runner；retained Unknown返回原错误、不报 Joined。无 entry 也必须先完成私有授权和旧页失效，再确认此 manager 无原 A owner；不是跨 manager/进程证明。报告不可由外部 DTO构造，不实现 participant/StopReport。

## 写域与必要验收

生产唯一作者写四个文件：新增 `internal/central/project/scheduler_stop.go`、`internal/central/scheduler/project_stop.go`；窄改 `internal/central/project/agent_retirement_preparation.go` 的独立 control loan 关闭归属，以及 `internal/central/scheduler/project_runners.go` 的控制调用、per-runOne取消、generation和容量回收。不改 Work、Execution、DDL、App 或上游公开 Agent contract。

纯控独立写 `internal/central/project/scheduler_stop_authority_test.go` 与 `internal/central/scheduler/project_stop_test.go`：非法 issuer/同 Store/真实 callback 前置、授权原返回、scope早返/迟 Committed、A/B隔离、旧页、两个 Unknown次序及 manager全局控制尾。复用必要旧 scope/manager 回归，不重复全平台矩阵。

PG 独立写 `tests/projectvariable/scheduler_project_stop_test.go`，`TestSchedulerProjectStop` 两 sub：`committed-stop-isolates-project-and-discards-stale-page`；`driver-scope-and-authorization-unknown`。用真实原目录、真实 A/B runner、正式初始化图和真实 guard；仅 accepted Delete 为明确受控前置，stopping/running claim必须真实 StopDriver产生。原 A capture 和原目录页面可在物理 Committed 后 hold 原 Store 返回，精确说明锁已释放但原调用尚未返；授权自己的 Committed 原返回前零 stop，后 A pending，release后 A实返/槽位释放且 B继续，旧页返回后、下一新页发布前仍不得重建 A，不能只等最后 active-only 页掩盖短暂复活。仅授权 Tx 可用明确受控 ackmask Unknown证明零 stop，之后同活 step独立授权正常退出；不把它冒真实wire断线或原 Unknown回滚。

runner 业务 Unknown 两序留纯控，不为了 PG 清理伪造 Joined；任何原调用尚未 return，失败清理仍强保原图/guard至原实际退出。PG不发 Model请求、不靠任务完成/取消制造停止证明。验证只覆盖本片及必要旧行为，遵循 [团队流程](../agent-team/README.md)；源码/gofmt/static并行，Go/服务/Git由根线程协调。

## 实际验收

- [basic01 原 FAIL](../../../output/ai/scheduler-project-stop/basic01-outer/post-check.json)：`86231/f2eb4b` actual exit 1；Project 7 top / 30 sub 已 PASS，Scheduler 新测试缺 `NewRecoveryCause` 第三个参数而未执行，vet 与后继未启动。唯一修正是在该测试补空 `checkpointRef`，形成受测 `898ed8e`；没有产品修正，不抹原编译失败。
- [basic02](../../../output/ai/scheduler-project-stop/basic02-outer/post-check.json)：`59860/2c2a82` actual exit 0，15 top / 34 sub、Project/Scheduler 两包 race 与两包 vet 全 PASS；573 六图及 14 outer 输入一致。[sequence01](../../../output/ai/scheduler-project-stop/sequence01/post-check.json) `45610/a0ee44` actual exit 0，controls 1+/7-、单 binary compile/list/prepare 原 Wait 全 0，2,085 compile → 2,086 runtime 输入一致。
- [native01 whole PASS](../../../output/ai/scheduler-project-stop/native01/post-check.json)：`5554/d9b30e` actual exit 0；supervisor 1508414 Wait 0 / 151.351s，driver 1508435 与 Go 1510238 原 Wait 0。1 top / 2 sub 共 36.49s（17.74s / 18.75s）；2,086 输入、四身份及受测 HEAD 相同，7 资源/14 次缺失、双尾与 TCP75 双空、无 race warning。完整方法和原件索引见 [验证说明](../../../.agent-state/scheduler-project-stop/README.md#actual-results)。

以上仅证明本 manager 的 Delete-only 停止、真实 A/B 隔离和私有 scope 下原调用返回；accepted Delete 与授权 ackmask 的受控边界不变。物理 COMMIT 后的原 Store 返回屏障不冒 SQL 锁仍持有；不证明 foreign/global 终局、Task 关系清退、Agent 退休或 full Registry。
