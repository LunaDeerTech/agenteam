# Work 当前 Agent 指派引用：原事务检查口

状态：[Variables→App](d08-variable-project-call-app.md) 已动态接受并完成技术 main 交付，本片已获限定实施授权，尚无本片运行结果。工作树 `/workspace/agenteam-work-agent-assignee-references`，分支 `ai/work-agent-assignee-references-20261012`，实际创建基线与 HEAD 为 `987ca6c42fb2c316fe8ce6564711f2afdedfbd75`，迁移最大号 75。正式卡单独保存后，两个生产文件、独立纯控、PG 和方法分域并行；Go/Git/服务仍由 root 统一调度。本片只补一个真实可成功的 Work 事实口，不实现 Agent/Project 删除、调度停止或引用退休，不新增迁移、App 默认接线、后台 owner、HTTP 或 Project LifecycleRegistry。

## 具体缺口与契约

[Agent WorkReferences 契约](../../../internal/central/agent/contract/reference.go)已要求未来 Agent 删除在 Schedule EX、Agent EX 下通过 Work 自己的 port 检查当前 assignee 引用，包含 terminal Tasks。现在已有方向是 Work→Agent 的当前 Agent 校验；[TaskReader](../../../internal/central/work/task_reader.go)自开事务且分页，[Scheduler 遍历](../../../internal/central/work/scheduler_traversal.go)只观察调度所需范围，都不能证明调用者原 Tx 中完整的当前指派谓词。本片补反向 Work-owned 事实检查，不重复旧 Task 列表/调度快照。

拟在 `internal/central/work/contract/agent_assignee_references.go` 定义：

```go
type AgentAssigneeReferences interface {
    RequireAgentUnassignedInTx(
        context.Context, foundation.Tx, identity.Actor,
        identity.ProjectID, identity.AgentID,
    ) error
}
```

由已有 `*work.Authority` 在新 `internal/central/work/agent_assignee_references.go` 实现，并加入编译期接口断言。复用 [Authority](../../../internal/central/work/service.go)固定的 Store/ProjectAuthority，不引入新 mutable setter、构造 fallback 或公共 test seam。方法返回 nil 的唯一含义是：在该次已授权原 live Tx/锁切面中，给定 Project 的当前 Task assignee 没有指向给定 AgentID。它不是可缓存的 grant，不发行 private retirement witness、Stopped/Joined 报告或可序列化许可。

## 校验、查询与原调用归属

1. 缺 Authority 绑定返回 DependencyUnbound；nil ctx、非法 Project/Agent ID/Tx 依既有 InvalidArgument 约定，已取消 ctx 保留取消错误。Actor 用现有 [Work ValidateActor](../../../internal/central/work/contract/structure.go)：无效身份 Unauthenticated，Human 可继续，AgentRun DependencyUnbound，Service Forbidden。无成功 fallback。
2. 在任何 Task SQL 之前，用 Authority 原 Store.InTx 验证同 Store 的真实 live Tx，再 RequireHeldLocks 验证 **User SH、Project SH、ProjectSchedule EX、Agent EX**；EX 可满足 SH。缺锁必须拒绝，不自行补锁、不另开 Tx、不 commit/rollback、不延长调用者预算。
3. 同一个原 Tx 中调用固定 ProjectAuthority.RequireOwnerInTx(actor, project, Read)，核 grant 与当前 Actor/Project 一致；Owner/Session/Project gate 的原拒绝先于 Task 事实。复用当前 Human Read 语义，不更改 archived/archiving/deleting 的既有 Project gate。
4. 仅查询 Work 的当前 Task 表，核心谓词是 `EXISTS(SELECT 1 FROM agenteam_work.tasks WHERE project_id=$1 AND assignee_agent_id=$2)`，**不按 state、current Sprint、started/completed、分页或前 N 行过滤**。done、cancelled、blocked、backlog 与活跃状态均是当前指派。存在返回既有 ResourceBusy；不存在才 nil。EXISTS 的短路是完整存在性谓词，不是把截断页当无引用。SQL/Scan/原 ctx 错误照实返回，不能变 nil。
5. 方法是同步 same-Tx loan：原调用者持有调用、Tx 与取消/实际 return，不新增独立 call map 或 ProcessGuard owner；不触 Activity/Event/Audit/网络，不读 Agent/Execution/Tool/Secret 表，不修改任何业务或历史记录。

## 目标存在、跨 Project 与 future issuer 的责任

Work 不拥有 Agent canonical row，因此此口**不证明 Agent 存在、已初始化、active、属于该 Project 或可删除**。合法 UUID 但 Agent 不存在，只要当前授权 Project 无该指派，允许返回 nil；给定 Agent 实际属于别的 Project 而当前 Project 无指派，也不能从 nil 推出其归属正确。查询边界严格是请求的 Project，不宣称检查了其他 Project 的引用或整个集群。

当前 Human 调用者若需要“现存同 Project Agent 无 Work 指派”的组合事实，应在同 Tx/同完整锁下先调用真实 [Agent Authority.RequireCurrentInTx](../../../internal/central/agent/current.go)，保留它的 NotFound/InvalidState，再调用本口；Agent EX 满足原当前 Agent 校验的 SH 要求。即使两个方法都成功，也没有获得删除权限。不得为了补存在性在 Work 内跨域读 Agent 表、用测试替身永远声称 Agent 存在或增加循环 provider。

跨 Owner 请求先由当前 Project gate 拒绝，不能用无引用绕过；调用者自称外部 Agent 属于 Project 不被本口背书。若未来要捕获跨 Project 非法指派或修改查询作用域，需另定语义，不能静默将此 Project-scoped nil 扩成全球无引用。

未来 Project Delete 使用的是原生命周期 service issuer，不能伪造 Human actor、借这个 Human Read 入口或沿用一次旧 nil 结果授权退休。其 Project→Agent 子阶段/private original-owner witness 仍需正式设计；届时可以复用 Work 内完整查询，但新增的授权分支必须独立验证原 cause/阶段/Store/Tx，不放宽本片 Actor 门。本片不声称已提供那个尚不存在的 retirement port。

## 锁竞争的真实证明边界

RequireHeldLocks 只验证，不负责等待另一事务。真实竞争验收只用两个只读 caller Tx：A、B 各自取得 User SH、Project SH、同一 Schedule EX，以及各自不同 Agent 的 EX。B 必须先在自己的原 `WithinTx` callback 内执行规范化 `AcquireAll`；A 原 callback 后仍持原 Tx 时，阻塞发生在 **B 的 AcquireAll 获得 Schedule EX 之前**，此时不得宣称已进入 Work 检查口。用真实 pg_locks/pg_blocking_pids 绑定 A/B backend 与该 Schedule key 证明等待，禁止 sleep 猜阶段；不把 A 称为真实 Task writer、Scheduler owner 或 Execution。

测试释放 A 的物理 COMMIT 后继续持住其原 Store return，保留实际 Committed 结果；B 此时可以取得锁、进入检查口并返回。最后才释放并亲收 A 的原 Store return。分别记录 callback 后原 Tx 未终局、物理 COMMIT 已完成及原包装调用实际 return，不能把这三层合成一项。本口不要求 B 等待 A 包装 return，也不替 A 证明 Joined；各原调用/连接仍须实际回收，不能把 B 得到 nil 当成调度、Execution 或旧调用全已结束。

## 最小写域与验收

产品限上述两个新文件；独立纯控拟 `internal/central/work/agent_assignee_references_test.go`，真实跨模块 PG 拟 `tests/projectvariable/work_agent_assignee_references_test.go`。不修改 Task 查询/写入、Agent 当前校验、调度器、历史/FK、数据库 schema 或 App。若真实接口需要扩大域，先列具体阻断，不先加通用框架。

- **合法正例与当前引用完整性**：真实非空 Project、两个通过正式原 Agent provider 可验证的 Agent，真实 Task 的正式 backlog→todo 路径指派其中一个；另一 Agent 在同 Tx/完整锁/原 Owner 下成功。当前 cancelled 生产 transition 未绑定、done 正式完成牵涉完整 Execution 图，因此 done/cancelled 仅在本新 PG 的原 owned Tx/完整锁 union 下精确 SQL 修改原 Task.state，assignee 不变；真实检查口返回 ResourceBusy 后，以固定 sentinel 强制该原 Tx Rollback，核 Task/history/claims 全字节恢复。这个受控查询前置不是正式取消/完成或 Execution 终局，不改 transition 产品、FK 或约束，不清引用制造正例。
- **职责与拒绝集**：不同 Store/失效 Tx、四种缺锁、Owner/Session/Project 拒绝、非法及不支持 Actor、取消与 SQL 错均不得产生成功。明确测试无 canonical Agent 的合法 ID 在无指派时只得 Work-scoped nil，而组合调用的真实 Agent 校验仍 NotFound；跨 Project 目标的存在性由原 Agent caller 校验拒绝，不能误报本口做了跨域存在检查。
- **原锁等待与实际返回**：按上一节验证两个只读 caller 的 Schedule EX 互斥、A 物理 COMMIT 后但原 Store return 仍 held 时 B 可读取完整结果；另核本口未 Acquire/WithinTx/Commit，原连接/调用全部实际返回。结果不声称真实 Task writer、Scheduler/Execution/foreign process 已 Join。

真实 PG 固定 `TestWorkAgentAssigneeReferences`，1 top / 3 sub：`same-transaction-current-and-terminal-references`、`authority-and-transaction-boundaries`、`caller-lock-release-before-original-return`。纯控只补本口必要拒绝、完整锁验证、原 Scan/返回及无自开事务行为；不扩旧 Work/Execution 全矩阵。方法绑定最终源码与实际输入闭包，不预填通过。

与现有 Agent 契约逐项对齐：Work 自有表/port；caller 原 Tx；Schedule EX＋Agent EX；包含 terminal 的全部当前指派；无 Work 引用不是其他领域无引用或执行证明。此片可独立真实验收，但仍只是未来退休的必要事实前置，不开放 Agent 删除、默认 Project 创建、full Registry 或历史退休。
