# Work 当前 Agent 指派引用：原事务检查口

状态：[Variables→App](d08-variable-project-call-app.md) 已动态接受并完成技术 main 交付；本片在受测源 `f9548b17e33c8fdab474f9dc6148ff713e2dd8aa` 完成 basic02、controls、compile/list/prepare 及首次 native01 的原整轮 PASS，保留此前 basic01 FAIL。工作树 `/workspace/agenteam-work-agent-assignee-references`，分支 `ai/work-agent-assignee-references-20261012`，实际创建基线为 `987ca6c42fb2c316fe8ce6564711f2afdedfbd75`，迁移最大号仍为 75。最终文档与主线交付另行保存，尚不声明已保全或清理。本片只补一个真实可成功的 Work 事实口，不实现 Agent/Project 删除、调度停止或引用退休，不新增迁移、App 默认接线、后台 owner、HTTP 或 Project LifecycleRegistry。

## 具体缺口与契约

[Agent WorkReferences 契约](../../../internal/central/agent/contract/reference.go)已要求未来 Agent 删除在 Schedule EX、Agent EX 下通过 Work 自己的 port 检查当前 assignee 引用，包含 terminal Tasks。现在已有方向是 Work→Agent 的当前 Agent 校验；[TaskReader](../../../internal/central/work/task_reader.go)自开事务且分页，[Scheduler 遍历](../../../internal/central/work/scheduler_traversal.go)只观察调度所需范围，都不能证明调用者原 Tx 中完整的当前指派谓词。本片补反向 Work-owned 事实检查，不重复旧 Task 列表/调度快照。

已在 [Work 契约](../../../internal/central/work/contract/agent_assignee_references.go)定义：

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

产品限上述两个新文件；独立纯控为 `internal/central/work/agent_assignee_references_test.go`，真实跨模块 PG 为 `tests/projectvariable/work_agent_assignee_references_test.go`。不修改 Task 查询/写入、Agent 当前校验、调度器、历史/FK、数据库 schema 或 App。若真实接口需要扩大域，先列具体阻断，不先加通用框架。

- **合法正例与当前引用完整性**：真实非空 Project、两个通过正式原 Agent provider 可验证的 Agent，真实 Task 的正式 backlog→todo 路径指派其中一个；另一 Agent 在同 Tx/完整锁/原 Owner 下成功。当前 cancelled 生产 transition 未绑定、done 正式完成牵涉完整 Execution 图，因此 done/cancelled 仅在本新 PG 的原 owned Tx/完整锁 union 下精确 SQL 修改原 Task.state，assignee 不变；真实检查口返回 ResourceBusy 后，以固定 sentinel 强制该原 Tx Rollback，核 Task/history/claims 全字节恢复。这个受控查询前置不是正式取消/完成或 Execution 终局，不改 transition 产品、FK 或约束，不清引用制造正例。
- **职责与拒绝集**：不同 Store/失效 Tx、四种缺锁、Owner/Session/Project 拒绝、非法及不支持 Actor、取消与 SQL 错均不得产生成功。明确测试无 canonical Agent 的合法 ID 在无指派时只得 Work-scoped nil，而组合调用的真实 Agent 校验仍 NotFound；跨 Project 目标的存在性由原 Agent caller 校验拒绝，不能误报本口做了跨域存在检查。
- **原锁等待与实际返回**：按上一节验证两个只读 caller 的 Schedule EX 互斥、A 物理 COMMIT 后但原 Store return 仍 held 时 B 可读取完整结果；另核本口未 Acquire/WithinTx/Commit，原连接/调用全部实际返回。结果不声称真实 Task writer、Scheduler/Execution/foreign process 已 Join。

真实 PG 固定 `TestWorkAgentAssigneeReferences`，1 top / 3 sub：`same-transaction-current-and-terminal-references`、`authority-and-transaction-boundaries`、`caller-lock-release-before-original-return`。纯控只补本口必要拒绝、完整锁验证、原 Scan/返回及无自开事务行为；不扩旧 Work/Execution 全矩阵。方法绑定受测源码与实际输入闭包，具体原结果如下。

与现有 Agent 契约逐项对齐：Work 自有表/port；caller 原 Tx；Schedule EX＋Agent EX；包含 terminal 的全部当前指派；无 Work 引用不是其他领域无引用或执行证明。此片可独立真实验收，但仍只是未来退休的必要事实前置，不开放 Agent 删除、默认 Project 创建、full Registry 或历史退休。

## 实际验收与保留边界

完整方法及每段原 Wait/输入核对见 [方法 README](../../../.agent-state/work-agent-assignee-references/README.md#actual-validation)。tool-observation 仅为 assistant 亲收原工具返回的转录，不称 provider 原生导出；文档收尾不会回写原日志、FAIL 或历史输入图。

- **basic01 原 FAIL**：`22945/cfb42d` actual 1，源 `f92d787c1ada9dc79c876610ec7e9adc0d6b887a`。5 top / 32 sub 全在 `newAssigneeRefFixture` 构造处 `INVALID_ARGUMENT`，尚未调用新 port；vet 与其后阶段均未启动。459 输入四张已完成图、13 outer 与 461 实际并集核对一致，无 race。静态必经校验表明两个 `assignee reference` 测试 name 不符原 `NormalizeName` 规则；原 log 没有内部执行 trace，不将静态诊断倒填为动态 trace 或产品故障。唯一 test 修正将两 literal 改为 `assignee-reference`，形成受测 `f9548b17`，产品、方法、断言和预算不改。[原 FAIL 核对](../../../output/ai/work-agent-assignee-references/basic01-outer/post-check.json)永久保留。
- **basic02 原 PASS**：`52911/263b16` actual 0，method PID 1301821 Wait 0 / 67.498 s；race PID 1301933 Wait 0 / 28.298297 s，vet PID 1302224 Wait 0 / 35.834012 s。精确 5 top / 32 sub 各运行并通过一次，仅 Work 包 race 与 Work/contract 两包 vet；459 六图、13 outer、461 实际并集相同，无 fail/skip/race。[原核对](../../../output/ai/work-agent-assignee-references/basic02-outer/post-check.json)不覆盖首轮失败。
- **controls/compile/list/prepare 原 PASS**：`38060/47fa54` actual 0 / 152.261 s；一正七反、实际 Go PID 1303338 0 / 146.637 s、sole list PID 1305214 0 / 1.059 s、prepare PID 1305283 0 / 1.013 s。2,073 compile 三图到 2,074 runtime 仅增加同一原 PG binary，33 outer、全部 75 migration、102 同包 Go 输入齐；未添加新 schema。[原 sequence 核对](../../../output/ai/work-agent-assignee-references/sequence01/post-check.json)与 native 分开。
- **native01 原 whole PASS**：`16513/a3e934` actual 0，sup PID 1305989 Wait 0 / 137.123 s，driver PID 1305990、Go PID 1307604 Wait 0；1 top / 3 sub 40.28 s（12.75 / 14.03 / 13.50 s）。[原 post](../../../output/ai/work-agent-assignee-references/native01/post-check.json)核 2,074 实际输入及 input/invocation/plan/recorder 四身份不变，7 个资源 ID 两次全 absent（14 项），descendant/private/runtime 双空、原 TCP 双 delta empty、无 race。原 raw 为 `/tmp/wagr01/pg-fe6fd1323720481eadd51c25b161446b.log`。

PG 成功限定于本口：真实非空 Agent/Task 指派与原 Owner/锁；done/cancelled 是保留 assignee 的原 Tx SQL 查询前置并实际 rollback，不能称正式终态 transition/Execution 完成。两个锁竞争者都是 readonly caller，原物理 COMMIT 与包装 Store return 分开观测，不宣称 Task writer/Scheduler/旧执行已 Join。目标 Project/Agent 沿 newTaskTransitionFixture→newAgentCreateFixture 使用真实 Skill/P2 初始化；跨 Project 职责反例的另一个 Project 来自 inherited base 的持久测试 Skills receipt。不称 Project 创建 HTTP 或完整 production Skills 验收。合法无引用 nil 不证明 Agent 存在、同 Project 归属、可删除或其他域无引用；没有开放 Agent/Project 删除、full participant、默认 App 接线或任何既有 STOP。
