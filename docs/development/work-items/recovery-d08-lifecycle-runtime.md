# D08 B03-R4：真实归档终局、恢复与原操作重试

修订：rev2.1 / 迁移排期与前向恢复限定差量待审；业务规则沿已采纳 rev2。尚未开放业务实施。主线程已撤回本卡原 `00017` 文档预留，转留 D09 current Model Resolution；本卡迁移编号待实施前重新分配，不预占 `00018`。没有 Go、SQL 或测试资源写权。实施者和独立验收者由主线程另行下发，不再委派。

## 固定输入与完整结果

Project 生产核查基线为 `fda0a35`，只读其已提交源码、[D08 正式设计](d08-project-owner-design.md)、[R1 不可变注册库](recovery-d08-b03.md)、[R2 持久接受](recovery-d08-b03-acceptance.md)和 [R3 事实 Authority](recovery-d08-lifecycle-authority.md)。前置绑定采用已独立静审并提交的 [Project 领域绑定 rev2](recovery-project-domain-bindings.md) `086f984eeadadebddcbcaab2868af96f8dfe5ce7`；[Object Audit](recovery-object-project-audit.md) 实现已独立验收提交 `a716ae2a16bc24c115d0207557cada96a30f1049`，但其 Project 映射不因此自动完成。[D09 Project 配置](recovery-d09-project-configuration.md) 按固定规格 `6e0bda1` 读取，冻结前主线程确认实现已独立验收并提交推送 `de00c610da62cb77cc03efe7c3cc842cf81f1ba5`；本卡未据此读取活动稿。[Artifact stop](recovery-artifact-project-stop.md) 固定规格 `fcb83fc` 和领域绑定实现仍待验收，全部实际接缝仍须最终 delta，不能把活动候选视为已验生产。

本卡交付一个实际可验结果：真实四域参与者停止后，Project 从 `archiving` 原子完成为 `archived`，当前 Owner 可 Restore 成为新的 `active` 版本；失败、进程退出和 COMMIT Unknown 均沿原 operation、冻结 manifest 与首次事件身份恢复。Delete 只推进到 `cleaning/domains` 并明确交给后继 Cleaner。归档不执行 Cleanup；本卡不实现 Cleaner、最终删除、HTTP/app/root、未来 Agent/Tool/Runner/Meeting/D10 实际初始化，也不宣称 D08/D28/E01 完成。

先读 [AGENTS.md](../../../AGENTS.md)、[团队流程](../agent-team/README.md)、[设计技能](../../../.agents/skills/agenteam-design/SKILL.md)与 [Go 开发技能](../../../.agents/skills/agenteam-go-development/SKILL.md)；独立验收者另读[验证技能](../../../.agents/skills/agenteam-verification/SKILL.md)。正式行为沿 D08 §5–10；本卡不削弱“首次规划持久化、重试不换 EventID”，不改 R2 已接受命令的严格 plan/receipt。

## API、构造与生命周期

公共 Project contract 已有 Restore/Retry，本卡实现下列签名；Runtime 是 Project 包内部产品库的具体类型，不新增 shared contract：

```go
func NewRuntime(service *Service, authority *LifecycleAuthority) (*Runtime, error)
func (*Runtime) Initialize(context.Context) error
func (*Runtime) Start(context.Context) error
func (*Runtime) Check(context.Context) error
func (*Runtime) StopAdmission()
func (*Runtime) Drain(context.Context) error
func (*Runtime) Force(context.Context) error
func (*Runtime) Joined() bool

func (*Service) StopAdmission()
func (*Service) Joined() bool
func (*Service) RestoreProject(context.Context, identity.Actor,
    foundation.CommandMeta, c.ProjectID) (c.ProjectRef, error)
func (*Service) RetryLifecycle(context.Context, identity.Actor,
    foundation.CommandMeta, c.ProjectID, c.OperationID) (c.LifecycleResult, error)
// LookupCommand 新增 restore/retry-lifecycle 分派。
// 既有 Service.Stop()、Force()、Drain(context.Context) 签名保留。
```

1. `Service.New` 只捕获一次有效 `Processes.CurrentProcess()`，creation 和 lifecycle 始终使用该实例 ID。ProcessAuthority 是受信任外部依赖；合法 ID 不证明真实 ProcessGuard 已装配，组合验收另证。不得在 Project 中新建、重置或 Close guard。Authority 与 Store 必须是同一实际 Store；不按同 PG 地址替代同 Store。
2. `NewRuntime` 不执行 SQL、不启动 goroutine。拒绝 nil/可判定零 Service 或 LifecycleAuthority、异 Store、无效 registry、当前 manifest 解析失败及第二个 Runtime；Service Authority 的 Lifecycle 依赖须为传入的同一具体 R3 Authority。其完整版本声明与当前 registry 一致；恢复旧 operation 时再按该 operation 的完整 manifest 逐项核兼容版本。opaque participant 的非 nil、Name/声明检查不是内部健康证明，不反射猜私有状态。实际调用错误或 panic 不能转成 stopped。
3. 构造成功即把 Runtime 所有权挂入 Service；外层必须先登记资源，再调用 Initialize/Start。Initialize 自身计入 owned control，确认本卡届时获分配迁移的实际 schema 与当前装配，建立恢复扫描能力，不执行未确认接受的参与者调用。历史 operation 的缺版本/坏事实逐项显式失败或保留待处理，不用一个忙 Project 阻止所有其他 Project 初始化。启动检查失败/取消不撤销资源登记；晚返回控制仍计入 Joined。
4. Start 仅在 Initialize 成功后启动一次；重复 Start 返回 `INVALID_STATE`，StopAdmission 后不能重启。Start 的 context 只约束启动等待，成功后由已登记 Runtime 持有运行 context，后续请求取消不结束整个 worker。Check 用有限调用预算检查初始化/停止/致命调度错误和真实依赖状态，不启动业务、不等待所有 pending 操作完成；单个 failed operation 不等于全 Runtime 无法服务。
5. Service/Runtime 的 StopAdmission 共用一个 admission 屏障：停止新公共调用和新 work round，不取消已接受调用；已登记父调用的 checkpoint/确认属于同一 owner，可继续收尾，不借 child 再申请一个新 admission。既有 Service.Stop/Force 保留“关 admission 并请求取消”的兼容含义，其无 context 返回不构成 join 证明。Runtime.Force(ctx) 同时请求取消并在原预算内等待全局 Joined；Drain(ctx) 只等待并执行必要有界终局确认，不提前标成功。
6. `Joined` 在 admission 已关闭，所有 Service 公共调用、creation provider/confirmation、Runtime 初始化/调度/worker、原 writer 终局确认和实际尾部调用全部返回且资源所有权退休后才为 true。Runtime.Joined 与 Service.Joined 使用同一事实；没有 Runtime 的 Service 也必须满足 creation/命令部分。未关闭 admission 时不能因瞬间零调用报告最终 joined；Runtime.Drain 要求先 StopAdmission，否则返回 INVALID_STATE。零 Runtime 的 Joined=false，其有返回值方法拒绝 DEPENDENCY_UNBOUND；重复 StopAdmission 安全。

R2 仍可持久接受操作而 Runtime 尚未运行；接受成功不是已停止。新 Restore/Retry 的业务规则见后文，已提交结果的当前授权查询不依赖 Runtime 当前在线。这里没有自动调度 Create 的新功能；既有 Create/RecoverCreations 能力与未绑定真实 D10 的边界保持。

## 全局所有权与原 writer 终局

固定代码中 `Service.begin/Drain` 只跟踪公共方法返回，`Stop` 会立即取消；`creation.go` 在调用退出时释放初始化槽，`recovery.go` 的 checkpoint/confirm 和 `commands.go` 的 Unknown lookup 另开 `WithoutCancel + 3s`。固定 D03 `transaction.go` 的 callback 错误/取消分支调用 rollback，rollback 失败仅有界 discard 客户端连接，仍返回 NotCommitted；该正确业务结果不证明代理后的原 PostgreSQL writer 已释放锁。这些路径不能单独证明 SQL writer 已终局。R4 必须把它们与新 worker 一起纳入所有权；不得只新增一个能独立 joined 的 Runtime 而遗留旧调用。

- 每个 Service 自有 Tx 在开始前登记原 cause、owner 和实际串行锁；有原 attempt 时保留，不从 NotCommitted 捏造未公开的 attempt。已确认 Committed 可按实际终局证据退休；Unknown 留在 owner 下。NotCommitted 必须与资源终局分开：已进入 callback、可能持有本域锁而没有充分终局证据时，保留原 owner/身份/锁记录，即使业务 RPC 已返回；公开业务结果仍为原 NotCommitted，不改成业务 Unknown。仅已明确未启动、没有可能残留本域 writer 或另有充分终局证据时可直接退休，不能只凭错误码、断连、取消或方法返回。调用返回和 `joined[attempt]` 只能证明该实际调用不再继续，不能替代原 SQL writer 的终局证据。Create 的初始化槽以及 lifecycle 的四槽不能因超时或未证实收尾的 NotCommitted 提前释放。
- 原 writer 确认沿真正共享的原锁：creation/人类命令用原 command EX 与 Project EX；后台 lifecycle 用原 Project EX 和同一 claim 行串行规则；仅有只读 User SH 的路径用该 User EX 作为全局 join 的保守屏障。Service.ResolveProjectPath 委托 Authority 只返回安全 Fault、不暴露 CommitResult，故在委托前登记 owned read-call；其 Unknown 或无法证明无残留 writer 的 NotCommitted 用原 Human User EX 保守确认此前 User SH 读事务已结束，不编造物理 attempt/cause，不恢复或重放内容。该窄适配位于 Service，不修改 Authority。确认锁取得后还须证明本次确认 Tx 自身终局；55P03、取消、读失败、确认 Unknown，或可能已持锁而回滚终局未证的确认 NotCommitted 均不能完成 owner 退休。确认事务的收尾仍归原 owner，不遗留新的未跟踪锁，不重开过期预算。
- 内部 join 确认只证明旧 writer 不再运行，不返回内容、不 Touch、不重新执行业务，因此不要求原 Session 仍有效；公开 Lookup/重放仍须当前 Session/Owner。不能让撤权用户的旧 writer 永久阻止终局确认，也不能利用内部确认绕过结果权限。
- 私有确认控制使用已登记 owner 的记录与有界扫描/并行，不为每次 Unknown 或每轮扫描复制记录、启动脱离 goroutine。每个未决记录保留可串行确认的原身份和原 owner；四个真实生命周期调用均未返回时保留占用、报告容量受阻。初始化已有 MaxInitializing 仍独立限流，但与 lifecycle/普通调用共用退出屏障。
- 旧 3s 确认上限改为本次可用预算的交集。普通 RPC 取消可结束公共等待而不丢所有权；内部确认只使用已登记 Runtime/Service 的剩余 shutdown/Force 预算。原 Force/startup deadline 已耗尽时不再新开 3s，也不以 `WithoutCancel` 越过已发布的退出上限。未证明终局就返回未完成，Joined 保持 false。
- participant/initializer 忽略取消时等待真实返回，原槽、claim、process 所有权保留。只允许在外部调用已经 unwind 后记录安全 panic 原因；不能在持有 Tx 的 callback 内 recover 后继续提交。未注册的外部 goroutine 不能由接口返回值推定已 join，真实 adapter 必须承担自身 actual join。

最终外层 ProcessGuard 释放屏障必须是 Project 全局 Joined、Artifact producer Joined、Outbox producer Joined、HTTP/Account 等已有 producer Joined 的合取，再由 Object Runtime 完成原 guard 退出。固定 `app/resources.go` 尚未装 Project/Artifact；本卡测试装配证明顺序，生产 root 改造留给后继卡，不能把当前 `producersJoined` 当成已经满足。

## 调度、claim 与参与者推进

### 有界公平调度

单 Runtime 固定最多 4 个 lifecycle work item，每 Project 同时一个；creation 沿既有 MaxInitializing，不占这四槽。一个 work item 包括实际外部调用、checkpoint/claim 退休、Unknown writer 与迟到尾部。单 participant round 的 Request/Inspect 合计最多 2s 或更短 parent 预算，不为每个阶段重新给 2s。超时未返回继续占原槽；有空槽时其他 Project 正常推进，四槽全占时不再加 goroutine。

扫描按稳定 operation ID 分页，每页至多 100；游标越过被其他实例占用、未到期和坏项目，周期回绕，不能每轮只取同一忙前缀。已知 R2 接受/Retry 提交只发可合并 wake，丢 wake 由启动及周期 durable scan 补齐。每轮选择一个到期 participant 或完成步骤，短轮结束并确认退休后让出槽；同 Project 内未尝试行优先，其余按 `next_attempt_at`、R1 StopOrder 排序，不能首项 pending 使后续 required 永不收到请求。

每次有效待处理尝试递增持久 `attempt_count`，按 1s、2s、4s…30s capped 设置 `next_attempt_at`；安全处理整数/版本溢出。operation.retry_after 由未完成行最早到期值投影，不能只存在内存。accepted/stopping 自动调度；failed 等待显式 Retry。cleaning/domains 不进入 stop 扫描，不空转调用 Cleaner。仅剩旧 running claim 的 failed/completed 项可做终局退休，不自动恢复其业务 phase。

### 真实 claim/fence 与恢复

使用 00013 既有 `agenteam_project.work_claims(work_kind='lifecycle', work_id=operation_id)`，不新增 lease/TTL 表。所有本域 lifecycle 写事务一次完整 AcquireAll，至少 Project EX；人类命令另含原 command EX/User EX，Event 事务再含完整 D06 计划 union。claim 行在 Project EX 下读取/CAS；不存在先持一个锁后 Discover/补锁的路径。Project EX 本身就是这些旧 writer 与接管者必须共同持有的串行屏障，不制造虚假人类 command identity。

1. 先读候选 old claim；同 process 只接受本 Service 对 exact attempt 的实际结束证据，另建 Service/Runtime 即使持同 ProcessID 也不能捏造该内存证据。foreign process 只由真实 ProcessAuthority 对 exact ProcessID 确认死亡，实际进程退出/Wait 与 ProcessGuard 锁证据按既有协议；过期时间、连接断开、请求超时不算死亡。
2. 死亡/join 检查在 Tx 外。随后取得 Project EX，重新核原 operation/current pointer、Owner、action、接受 ProjectVersion、完整 manifest 和 old claim 全 tuple；通过原 writer 锁证明其终局后才 CAS 新 process/attempt/fence。比较变化即重读重规划；fence 单调推进，溢出拒绝。没有旧 claim或其已 terminal 时按同样串行路径建立新 running claim。
3. 首轮把 accepted→stopping、operation.version+1/updated_at 与首 claim 原子提交；只有确认该事务已提交，才构造 exact ProjectLifecycle Actor 并调用 participant。claim COMMIT Unknown 时没有 stop 调用；沿原锁核 canonical claim，仍 pending 就保留原 attempt。takeover 不能只因旧进程已死就绕过其仍持锁 PG writer。
4. 每次 checkpoint、完成 plan、最终事务均重验 exact process/attempt/fence、operation.version 和参与者行 version；迟到旧报告不更新进度。业务 actor/cause 保持原 operation/action/接受 ProjectVersion，不能把新 fence/attempt 或当前增长的 op.version 当新 cause。后台不用接受者已经过期的 Session。
5. 一个实际 round 返回并且最后 writer 终局已知后才能将本 claim terminal/释放槽。成功/failed checkpoint 与 claim 退休尽量同 Tx；完成/archive 或 delete handoff 在最终 Tx 内退休。尚有未返回调用时不能把 claim terminal 当提前 join；Unknown 后先确认或沿当前 canonical facts重新决定，不能直接重放上一次内存 UPDATE。

### 完整 manifest 与报告

恢复始终 `Registry.Resolve(stored RequiredManifest)`，逐项精确 name/version/owner module/reference kinds/cleanup dependencies；使用其固定 Participant/StopOrder。缺任何兼容版本返回 dependency_unbound，当前新增项不能默默替换旧集合，缺省 adapter、按空数据删项和只检查 DTO 前 100 refs 均禁止。

required/pending 行可按原 cause 幂等 RequestStop，若在同一总预算内得到有效 pending 再 InspectStop；后续 round 继续同 cause。已经 stopped 的行不重新 Request，仅在最终收束前用真实 Inspect 复核。不能把 Inspect 的 InvalidState/无行解释为“尚未请求”后绕过权限，也不能用 Cleanup 代 Stop。stop API 没有 cleanup checkpoint 输出，本卡不发明该输出或占用 cleanup checkpoint 编码；既有 checkpoint 原字节保持。

所有报告先核非零/Matches、完整 participant/scope/cause、state/reason 闭集、全量 active/unknown refs 的 manifest合法性与去重，再持久 stop_state/refs/reason/row version。stopped 必须真实报告 stopped 且完整 refs 为空。Outbox count 不能编造资源 UUID。错误或 Unknown 不采纳先前内存 stopped；保留 pending/outcome_unknown。真实 failed、缺绑定或坏报告记录固定安全失败及 `resume_phase=stop`，不撤 Project gate，不清已完成项；瞬时 dependency failure 保留安全可查询状态，重试规则不吞原 Fault。

全体持久 stop=stopped 后仍需在当前 claim 下逐项真实 Inspect；不持长 Tx 等外域 I/O。最终 Project EX 事务重读完整所有行与 exact claim，并核本轮复核对应行版本未变、没有本 item 尚活调用。复核失败不能伪造终态，也不能为了重新尝试抹去 stopped/checkpoint 历史。未来 enabled participant 若增加自主工作，必须升级其 adapter/manifest 证明，不能沿用本卡四域的事实假设。

## 待编号迁移：operation 自有完成事件计划

唯一迁移候选仍为一个 `project_lifecycle_completion_plan.sql`，完整文件名前缀待主线程按届时已提交连续序列分配；不得使用已转留 D09 的 `00017` 或自行占用下一号。00013 已有完整 claims、行版本、attempt_count/next_attempt_at；00016 是 Object maintenance 表，不提供 Project 完成事件身份。最小增加 `agenteam_project.lifecycle_operations.completion_plan JSONB NULL`，不改 commands/event_ids/R2 plan，不追加表或跨域 FK。migration embed 已 glob `*.sql`，无需为编号修改 Go embed。

计划格式固定 `project-archive-completion-v1`：ProjectID、OperationID、action=archive、接受 ProjectVersion、target_project_version=接受值+1、原 manifest_digest、完整 event_header 与 typed event_payload。Header 固定 Project producer/aggregate/scope、schema 1、独立首次 EventID、固定 target AggregateVersion；payload 精确 `operation_id/from=archiving/to=archived/action=archive`，不含 op.version、claim/fence、名称或 private refs。字段/格式版本严格闭集，拒绝缺字段、未知字段、null、坏 header/payload/摘要/版本；写前解码拒绝重复 key，读取已 JSONB 规范化的内容仍完整验证，不把 JSON 排版字节当事件身份。

DB CHECK 至少限制 SQL NULL 或 archive 的合法格式 JSON object、长度至多 65,536 bytes 和必需字段/基本 JSON 类型；应用 loader 再核完整 typed/cross-field 关系。旧 accepted/stopping/failed 和旧 completed 行允许 NULL，迁移不伪造历史 EventID、不重发旧完成事件。旧 NULL completed 只按既有终态事实读取，不能作为新 Append 许可。非法格式/过大/非 archive 计划拒绝；原 00013/00016 populated 数据和 CHECK 不削弱。沿已验迁移 Source 的前向约束，不写 Goose Down、不修改旧迁移或迁移引擎；失败恢复使用事务回滚、隔离备份恢复或另行审查的前向修正，不能删除已持久事件身份。隔离恢复演练不得冒充生产降级或允许带非 NULL 计划回退。

完成 plan 的首次 NULL→选定值在全 stop 确认后的短 Project EX + exact claim 事务内发生；一次 DbNow 作为计划 OccurredAt 与本次 updated_at，operation.version+1，Project/version/gate 不变，不 Append/Touch。计划随后不可变；已有同值计划只读复用，不因 participant checkpoint、Retry 或新 claim 生成另一个 EventID。格式 v1 不是随进度递增的 plan version；它绑定不变的 operation/project/manifest/接受版本，而不是会变化的 op.version 或过期 claim。

计划写 NotCommitted 且原 writer 已知结束时可重新规划尚未持久的 ID；Unknown 保留原候选和 attempt，沿原 Project 锁取得后读 canonical plan确认。原计划存在且完全匹配才算其持久；取得锁并确认原事务结束后的 NULL 才能认定未持久。锁失败/确认 Unknown 不许生成新 ID。接管者同样先证明原 writer 终局再复用已持久 plan；遇不同计划只采 canonical 并重新核状态/所有权，旧 worker 不继续。Outbox EventID 冲突拒绝，不替换已持久 ID“修复”。

## Archive 完成和 delete handoff

完成 plan 提交后，Tx 外从原完整 Header/Payload 重建 typed Event 并真实 PrepareAppend。Project producer 和 Project Authority 两个私有 issuer/purpose、完整 ActorDetails、完整 Event Summary、plan 内容、当前 exact claim 都必须绑定；旧私有计划在 takeover 后失效，新的 private plan仍使用同一个持久 EventID。D06 自有完整锁与 Project EX 一次 union Acquire。当前 `events.go` 无条件把 eventFact 当 command 加锁；本卡只为 operation completion 加独立封闭分派，不能用 OperationID 冒充 EventID/CommandIdentity，也不放宽 creation/update/R2/D09/普通路由。

最终事务执行以下一个原子结果：复核 Project 当前 `archiving`/initialized、原 pointer/Owner/接受版本；exact running claim；原完成 plan；完整 manifest 和全量 stopped 进度及复核版本。随后 Project→archived/version+1/archived_at、operation→completed/version+1/completed_project_version/completed_at、真实 typed `project.archive.completed` Audit、原 `project.lifecycle_changed` Event、claim terminal 同 Tx 提交。仅一次 DbNow 同时写相关 Project/operation 终态时间；Event.OccurredAt 保留首次规划时间，不能为追求相等重写事件或回填较早完成时间。若实际 DB 时间不能满足已有单调 CHECK，事务失败，不能捏造钟值。

Audit 用 `ProjectProducer`、cause=原 OperationID、ordinal=0、原 Owner initiator、目标 ProjectVersion 和本次最终真实 next operation.version；Event 不携带不断变化的 operation.version。真实 Project CAS 后才建立包内私有同 Tx 完成见证，绑定 Tx/Store、原 claim、完整 Entry/AppendKey/Event Summary及目标版本，原 context 传入 Audit/Outbox；不导出 mint/witness。CurrentAccess 验 exact 持久 plan/当前内部 cause，NewFact 还必须有本次真实 canonical 变更见证；仅有 Service+UUID或旧 completed 行不能重新制造缺失 Event/Audit。Audit/Event 任一失败回滚整个终态和 claim退休，先前独立提交的 completion_plan 保留。

最终 COMMIT Unknown 保留原 claim/plan，沿 Project EX 等原 writer终局后核 canonical completed operation、同计划及目标版本；确认提交只返回既有事实，不重 Append、不再 Stop。若已发生合法 Restore/新 operation，历史 completed row仍可证明原 writer已提交，但不再授权旧 cause写入。确认回滚时仍为原 stopping/plan，重验当前 claim后可重新 Prepare；仍持锁或确认不成保持 Unknown。stale fence 永远不能凭旧 report/私有 plan完成新状态。

Delete 全 stop 后仅原子设置 `state=cleaning, cleanup_stage=domains`、operation.version+1/updated_at 并退休本 stop claim，保留 Project deleting/版本/name、manifest、原 cause 和 cleanup required 行；不写 completed_at/最小 deletion receipt，不发额外 Event/Audit/Touch，不调用任何 Cleanup。清理顺序、Outbox/Audit 不可逆屏障及最终 claim退休由后继 Cleaner负责；此处返回的 cleaning 不是物理删除完成。新 cleanup-phase Retry 在本卡仍明确 unbound。

## Restore、Retry 与 Lookup

两类命令复用正式 Meta、RestoreDigest/RetryLifecycleDigest、CommandIdentity、当前 Session/Owner 和幂等优先级；另建严格私有 loader，不让旧 updatePlan 或 R2 lifecycleCommandPlan 接受额外可选分支。后台 completion_plan 与人类命令是两个持久实体，互不借槽。

### RestoreProject

当前 Session/Owner 与结果可见性先于回执；人类实际变更一次 AcquireAll 原 command EX、User EX、Project EX及完整 Outbox union。新命令必须 initialized+archived、Project expected_version 匹配，当前 pointer为同项目已完成 archive且其 completed_project_version等于当前 Project.version，原 claim已 terminal/原 writer已终局。不得从 archiving/deleting恢复或仅用 fixture终态绕过真实 stop 证明。

先持久 restore 专属 planned command：原预期 ProjectVersion、源 completed archive ID、目标版本、唯一 EventID及完整 header/payload（archived→active/action restore，无 operation_id）。规划不改 gate/Touch。Tx 外 Prepare 后，最终重新核当前身份、完整 plan、原 archived事实；原子 active/version+1、清 archived_at/current operation pointer、ProjectRestore Audit、原 Event、completed command安全 ProjectRef和首次 Touch。相关 Project.updated_at/command.committed_at使用同一个本次 DbNow，Event时间保持首次计划值。新事实校验用真实同 Tx Restore 见证，不能因 pointer 已在本 Tx 清除便跳过源 operation证据。

同 key同义完成重放在当前 Read可见性下返回原安全 ProjectRef，先于旧 expected_version/依赖变化；不重复 Audit/Event/Touch。异义为 IDEMPOTENCY_KEY_REUSED。旧 operation和停止回执保留历史，旧 cause因 pointer改变失权；不得恢复旧 execution/delivery/handle，也不重开旧 claim。Project后来 deleting拒绝普通正文重放；已物理删除仅原 Owner获 RESOURCE_DELETED，其他人 NOT_FOUND。规划/最终 Unknown都沿原 command+Project锁确认，保留原 EventID。

### RetryLifecycle

expected_version针对 operation.version。当前 Session/Owner、精确 Project/Operation和原可见性先校验；新 retry还核当前pointer、action、接受版本、完整 manifest和phase。只调度原 stop phase 的 failed/pending步骤：failed→stopping；已 stopping且有pending/failed步骤可重新置为到期。accepted尚未开始的操作返回 INVALID_STATE而不制造第二次接受；完整 stopped/正在完成或活 claim尚未实际join则 RESOURCE_BUSY，不先持Tx等待外部死亡证明。Runtime可仅退休已证明终局的失败旧 claim，再由同一 Human意图重试。

成功新 retry保留 operation/cause/manifest/completion_plan、所有 stopped项和 checkpoint/attempt_count；failed stop项改为pending，清其旧失败reason，未完成行 next_attempt_at与retry_after置同一个DbNow，operation.version+1/updated_at、清仅failed态适用的resume_phase/safe_reason，不改 Project.version/gate。Audit `project.lifecycle.retry`、命令完成回执、首次 Touch与该改变同Tx；无 Project Event。Auditmetadata取本次真实新operation.version，action沿原archive/delete，from/to为空。receipt记录原命令关联和接受retry版本，重放/Lookup再读该 operation当前安全投影。新retry单Tx完成，不伪造一个需要Event的 planned行。

同key已提交stop retry优先于旧expected_version、新registry/容量变化；若操作后来进入cleaning，可只读返回当前安全进度而不实施cleanup。completed archive按正式contract拒绝Retry endpoint，即使是原retry key；GetLifecycle仍可读，Lookup可在当前授权下确认原已提交retry receipt，仅为命令确认、不重试终态。新的cleanup-phase retry返回DEPENDENCY_UNBOUND，不部分更新phase/receipt/Touch。此限制不授权放开屏障后Audit，也不改变后继完整Cleaner应实现的正式规则。

completed delete的最小receipt分支保持正式只读语义：当前Session、原Owner、精确Project/Operation与Meta/key/正版本语法；不验证已删retry历史，不证明同义，不Touch/写Event/Audit。Lookup(retry-lifecycle)缺历史返回RESOURCE_DELETED；原Delete的严格keyhash/digest规则不变。本卡不产生该receipt，测试拥有的receipt前置必须显式标记。

### Lookup 与结果未知

Lookup增加两种严格分派，先当前身份/Owner和可见性；持原command与Project串行锁后才能返回committed/in_progress/not_observed。Restore planned只算in_progress；Retry完成receipt查当前operation安全视图。确认锁失败或原writer未终局不返回not_observed。撤权时公开结果拒绝，内部join仍可完成但不泄露内容；换合法新Session重新发现完整权限plan。Restore/Retry各自覆盖真实晚COMMIT、ROLLBACK与writer仍持锁三态，不能用同一个重试新key绕过Unknown。

## 错误与不可越过的边界

坏类型/Meta/报告语法为 INVALID_ARGUMENT；缺版本/真实依赖/未来cleanup为 DEPENDENCY_UNBOUND；旧fence、错actor/cause/issuer/见证为 FORBIDDEN或既有受控冲突；非Owner沿NOT_FOUND，Session错误沿原端口；版本冲突沿VERSION_CONFLICT；忙claim/计划竞争沿RESOURCE_BUSY；缺实际锁、坏持久事实、异Store及依赖故障沿DEPENDENCY_UNAVAILABLE。底层 not_started/not_committed/unknown保持原因和原身份，安全响应不带SQL、对象key、checkpoint或payload。worker安全失败不把操作恢复active，不吞错转stopped。

所有本域 SQL 只操作 agenteam_project；实际 Account/Artifact/Object/Secret/Audit/Outbox事实只走已审真实端口。R3事实Authority只证明exact当前cause/phase/版本和持锁，不等于Runtime持claim，也不替代参与者真实停止证据。Cleaner、原普通消费者或root缺口不能用 allow adapter填充。

## 精确候选实施路径

共34个路径：26个Project生产/纯测试、1个迁移、7个真实集成测试。表中“新”以固定fda0a35为准；实施前必须重新核上游已验delta，若任何路径已被新增占用或所需正确改动超出清单，先重审最小增量，不覆盖活动稿。

| 路径 | 允许的完整结果范围 |
| --- | --- |
| 新 `internal/central/project/runtime.go`、`runtime_test.go` | Runtime API/调度、四槽与admission/readiness |
| 新 `internal/central/project/ownership.go`、`ownership_test.go` | 全局owned调用/控制/原writer、预算交集与Joined |
| 新 `internal/central/project/lifecycle_claims.go`、`lifecycle_claims_test.go` | 真实claim/fence/原writer串行确认与退休 |
| 新 `internal/central/project/lifecycle_progress.go`、`lifecycle_progress_test.go` | 全manifest参与者round、报告/行版本/checkpoint与delete handoff |
| 新 `internal/central/project/lifecycle_completion.go`、`lifecycle_completion_test.go` | operation完成plan、归档最终事务/私有见证 |
| 新 `internal/central/project/lifecycle_restore.go`、`lifecycle_restore_test.go` | Restore真实命令/事件/Unknown |
| 新 `internal/central/project/lifecycle_retry.go`、`lifecycle_retry_test.go` | 原phase stop Retry与最小receipt只读 |
| 新 `internal/central/project/lifecycle_command_store.go`、`lifecycle_command_store_test.go` | Restore/Retry严格计划/回执；不放宽R2 loader |
| 旧 `internal/central/project/service.go` | 捕获process、全局admission/owner、旧公共读调用/Resolve委托及Runtime关联 |
| 旧 `internal/central/project/creation.go`、`recovery.go` | 既有creation调用、claim与Unknown所有权、初始化槽/3s预算修正；不实现新initializer |
| 旧 `internal/central/project/commands.go` | 新Lookup分派，原Create/Update/Lookup所有权与Unknown预算接缝 |
| 旧 `internal/central/project/lifecycle.go` | 原R2调用/Unknown所有权、已知提交wake；原接受/查询语义保持 |
| 旧 `internal/central/project/lifecycle_store.go` | 加载私有completion_plan、retry_after、行version/attempt_count/next_attempt_at；全量校验后才截DTO |
| 旧 `internal/central/project/events.go`、`lifecycle_events.go` | 完成/Restore的精确事实与双私有计划；保留D09、领域绑定及R2旧路由 |
| 旧 `internal/central/project/lifecycle_audit.go`、`audit_authority.go` | completion/Restore/Retry的typed同Tx真实事实分派 |
| 新 `db/migrations/<主线程待分配>_project_lifecycle_completion_plan.sql` | 仍只占一个路径；仅上述 operation 列/CHECK，前向迁移，不含 Down |
| 新 `tests/project/lifecycle_runtime_fixture_test.go` | 真实四域/Account/PG/MinIO/guard固定装配、明确原writer协议 |
| 新 `tests/project/lifecycle_runtime_test.go` | 真实archive终局/delete handoff/manifest/公平预算 |
| 新 `tests/project/lifecycle_runtime_recovery_test.go` | actual process死亡/旧writer/claim接管/完整checkpoint恢复 |
| 新 `tests/project/lifecycle_restore_retry_test.go` | Restore/Retry/Lookup的权限、幂等及原子性 |
| 新 `tests/project/lifecycle_runtime_unknown_test.go` | claim/checkpoint/plan/final/Restore/Retry真实Unknown |
| 新 `tests/project/lifecycle_runtime_shutdown_test.go` | 旧creation/公共命令与Runtime全局join、联合guard屏障 |
| 新 `tests/project/lifecycle_completion_schema_test.go` | 届时连续前缀的 fresh/populated、严格 plan、前向迁移与隔离恢复 |

不改R1/R3、领域绑定/Artifact/Object/D09文件、foundation/postgres、旧SQL、shared contract、go.mod/sum、脚本、HTTP/app或共享台账。旧 `events.go` 等接缝须先由D09/领域绑定作者验收冻结并交还。新测试复用已验B02 COMMIT proxy和owned资源原则，不修改旧fixture/旧断言。

## 必须真实验收的完整场景

所有新顶层以 `TestProjectLifecycleRuntime` 为前缀。真实组使用已验生产Project/四域adapter/Account/Audit/Outbox/Artifact/Object/Secret/ProcessGuard及PG/MinIO；D10仍可用明确隔离initializer验证已有创建边界，但不能替代本卡任何权限、stop或事件端口。独立验收者不得只复用作者屏幕输出，固定同源快照后执行完整新组并补自己的反例。

| 顶层后缀 | 必须观察的结果 |
| --- | --- |
| `ArchiveRestore` | 真R2接受→四域实际stop/join→archived→Owner Restore；Project/operation版本和单次typed Audit/Event/Touch正确，archive合法只读流保持，旧handle/delivery/cause不复活；接受EX被SH阻塞时零cancel |
| `DeleteHandoff` | 真实delete停止reader/source/transfer/Artifact/Outbox后只到cleaning/domains；零Cleanup/receipt/终态Event/Audit，名称仍占用，Get/原Delete重放看到正确当前进度 |
| `FrozenManifest` | 缺旧版本、错metadata/phase/cause/report、被DTO100cap隐藏的坏ref、某参与者未停均不能完成；全量合法refs保留，未来module不能被默认缩掉 |
| `FairnessBudgets` | 四个真实不返回调用阻止第五项且不假释放；一个busy Project不饿死其余；同Project一项；Request/Inspect共2s剩余预算、持久1..30s退避和实际返回后槽释放，不用sleep猜先后 |
| `ClaimRecovery` | exact旧process kill/Wait/guard释放，原PG writer仍持锁时不接管；原writer结束后fence推进，旧报告/旧privateplan不能checkpoint/完成；同process新Service无join证据拒绝偷claim |
| `CompletionPlan` | 首次EventID/header/payload持久；participant进度、Retry、重启、takeover不换；篡改完整Summary/issuer/claim、用completed行或任意Service UUID直接Append拒绝；单DbNow终态时间一致，Event仍为原规划时间 |
| `RestoreRetryAuthority` | 当前Owner/管理员/撤Session/新Session/非Owner、Project与operation两种expected_version、同key异义/重放、completed archive拒Retry、最小delete只读与cleanup unbound矩阵；原Outbox时间红单独保留关注 |
| `CommandAtomicity` | Restore与Retry的Audit/Outbox/Touch任一实际失败完整回滚业务/receipt，原计划可保留但不算成功；恢复不触发旧业务，Retry只原cause/phase并保留已完成项 |
| `ClaimCheckpointUnknown` | 原claim、accepted→stopping和participant checkpoint的真实COMMIT丢ACK/ROLLBACK/仍持锁三态；claim未确认零participant调用，writer未终局不释放槽，确认后按canonical进度继续 |
| `CompletionUnknown` | plan与最终archive各自真实三态，原候选/持久ID不换、Audit/Event只一次、所有权保留；final已提交而随后Restore时仅确认历史结果，零旧cause续写 |
| `RestoreRetryUnknown` | 两命令规划/提交适用的真实三态与当前撤权/新SessionLookup；still writer不能not_observed，同key确认零重复Touch/事件 |
| `GlobalJoin` | Create实际卡住initializer/确认、creation Unknown、R2/Update/Lookup原writer及Runtime尾部同时存在；另以精确backend PID/原ROLLBACK协议，令真实Service Tx持域锁并执行可回滚操作后，ROLLBACK确认失败、客户端已返回NotCommitted而原backend仍在事务持锁：业务保持NotCommitted，Joined=false、槽仍占用、Force/Drain有界未完成；放行原ROLLBACK并经原共享锁与确认Tx证明终局后才join。不得手造结果或只看客户端关闭；确认Tx自身/opaque Resolve的同类收尾也保持owner。旧API取消/3s窗口不假join，Force预算不重开；晚Initialize返回仍被跟踪，Project+Artifact+Outbox+HTTP的AND未成立前guard不释放 |
| `SchemaCompletionPlan` | 按实施时已分配连续前缀验证 fresh/populated accepted/stopping/failed/旧 NULL completed/delete，原行/约束保持；非法/过大/非 archive plan 拒绝；事务失败无半列，Source 拒含 Down 输入，隔离备份恢复保留已持久 plan/EventID，不实施生产降级 |

进程/COMMIT试验使用明确ready/entered/COMMIT协议和精确backend PID，不以固定sleep、宽松SQLSTATE或无锁缺行解释成功。新组不skip；记录每轮固定源码/上游manifest、顶层/子例、原失败与修复、复用的未变输入，不能把跨轮组合写成最终输入一次全绿。受影响旧完整顶层包括Project B02/R2/R3/Secret/D09/领域绑定、Artifact stop/Object S2/ObjectAudit、Outbox普通及lifecycle；原 `TestOutboxLifecycleInspectionAuthorityBoundary` 首红未重现但原因未知，仍保留关注，不能声明R4已修复其根因。

作者完成纯unit/race/vet、integration compile/vet、两cmd build；适用根check-go执行一次完整检查，后续只因新变化/失败补必要验证。真实资源按[依赖恢复记录](../agent-team/dependency-recovery-2026-10-05.md)由主线程独占交窗；本卡规格阶段不运行Go/Docker。独立验收保存准确源码、完整探针与重跑说明，确认自有exact-ID容器/网络absent、baseline不变、进程/运行目录清零后正式交还窗口。

## 开工门槛与交付边界

本卡静审通过不等于实现授权。开工前由主线程收齐 Artifact 和领域绑定的正式验收提交，连同已验 ObjectAudit/D09 逐项核 R4 实际消费端口/Store/锁/事件事实差异，交还全部旧文件所有权；重新确认全局迁移连续前缀、分配一个实际编号并完成对应输入差量审核后，才下发唯一实现者及该迁移写权。若最终 delta 改变本卡完整结果或新增路径，先收窄可核差异重审，不省略真实绑定。最终只在真实 archive/Restore/Retry、恢复/全局 join 和 delete handoff 均被独立验证后声明本卡通过；后继 Cleaner、生产 HTTP/root、D10 和完整模块门槛仍独立未交付。

rev1 原候选 SHA-256 为 `d1030889c9a10ce939e2acb08c9ef1e685486f4c7ca48c7a9a2ad0ea32efacba`；独立静审报告 SHA-256 为 `507edec7a03c7bab946147cfc7ba81fbf9b5d809dea71a9bd05e6d11781caf18`。rev2 仅修正实际 work_claims 表名，以及 NotCommitted 业务结果和原 writer/锁终局的区别（包含 Resolve、确认事务和原 GlobalJoin 顶层反例）；API、34路径、00017范围和13顶层保持。该失败路径本轮只有固定源码静态依据，没有动态复现或实现通过声明，等待限定 delta 复审。

## rev2 规格采纳

2026-10-05，主线程核对冻结候选 SHA-256 `99cf338a8254768d11c46964a6b6ffbdd27686bb6a4fb3b8b10c1de9283ae924` 与独立差量复审报告 SHA-256 `c92c4745d7f4c8e6c6072b67b2c70bfc1452b662b1d5ce66feb6a525b4804839`，采纳 rev2 规格。实际检查为固定差量、API/34路径/00017范围、13顶层与14链接核对；未运行 Go/Docker，不构成行为验收。Artifact stop 和领域绑定最终验收、真实接缝 delta、文件所有权交还仍是实施前置，00017 继续仅保留编号。

## rev2.1 工程排期差量

2026-10-05，主线程在固定 `3e5d88209224678c72302ec9b307327469ecd38c` 及工作区只读核查后，确认全局 SQL 仅 00001–00016，R4 的 00017/completion_plan 没有实现；独立核查报告 SHA-256 为 `a1c7d95b3ab9691851d3b0a9f05a364f0fbe4ba2b0da390eabb621e8f8949e16`。主线程撤回原文档预留，将 00017 转留 current Model Resolution，R4 待后续实际分配，不保证 00018。上段及旧报告保留原时点事实。

本次只改当前编号/实施门槛，并将原受保护 Down 设想纠正为 [Source](../../../internal/central/postgres/source.go) 已验的前向迁移及隔离恢复约束；不改变 completion_plan 业务/API、34 路径数量、13 组验收结果目标或现有上游阻断，不写 SQL、不改迁移引擎。该限定差量待独立复核，规格通过仍不授业务实施权。
