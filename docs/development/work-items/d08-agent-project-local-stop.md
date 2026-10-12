# D08/D10 Agent Project 本地停止提供方

状态：本地提供方已实现，有限独审、Agent basic 与首条真实 PG 验收通过；交付整合另行记录。受测 HEAD 为 `fe6beb28031caf5b80ee55feda4a476253d9db4b`，组合基线为 `448f6adea22d62dcb71255a3c834dbb8f1b95d84`；工作树 `/workspace/agenteam-agent-project-local-stop`，分支 `ai/agent-project-local-stop-20261012`。本卡结果只覆盖下述两个实际实例。

## 目标与依赖

为同一实际组合中的 Agent 配置 `Service` 和 `DirectoryReader` 提供精确 Project 范围的本地停止。Archive 取消本 Project 的原 mutation；Delete 同时取消原 read；其他 Project 继续工作。只有原业务函数及其同步 confirmation 实际返回才记为本地退出，取消、超时或 SQL 回调返回都不能替代它。

沿用 [D08 初始化与生命周期规则](d08-project-owner-design.md#7-项目创建与正式-skill-初始化)、[Variables 本地停止及单轮 Stop Driver 协议](recovery-project-domain-bindings.md)和现有 `ProjectAuthority.ValidateLifecycleInTx`。Agent 的当前权限、完整业务锁集合、引用提供方、原提交/Unknown 协议继续由其原入口负责。本片不实现新的业务 SQL、迁移或外域授权。

`agent/calls.go` 现按 Project/class 登记原调用，`commands.go` 的 Unknown confirmation 在 `WithoutCancel` 后仍连续归属原 mutation；提供方同时观察 `Service` 和 `DirectoryReader` 的原 callSet。目录及 Runner 配置读取均进入对应 read 登记，进程级 Stop/Drain 保留原全实例退出语义。

必读 [团队流程](../agent-team/README.md)、[Go 技能](../../../.agents/skills/agenteam-go-development/SKILL.md)、[验证技能](../../../.agents/skills/agenteam-verification/SKILL.md)及上述协议的相关段落；真实 fixture 作者另读 [测试技能](../../../.agents/skills/agenteam-test-engineering/SKILL.md)。不重读全部历史矩阵。

## 接口与实际退出协议

新增 `agent.NewProjectCallStopper(service *Service, directory *DirectoryReader)`，返回具体本地提供方及 error。构造无 SQL、无 goroutine，拒绝 nil/零实例，并核两者持有同一 Store、同一 Agent Authority 及其原 Project Authority；不按同 DSN 或类型名称推定同一实例。

提供 `RequestStop(ctx, actor, cause, scope)`、`InspectStop(ctx, actor, cause, scope)`，参数复用 Project 正式类型。返回私有、不可反序列化制造的本地报告，包含精确 ProjectID/OperationID/Action/accepted ProjectVersion、`PendingCalls` 与 `LocalJoined`，支持 `Valid`/`Matches`；零报告无效。它不实现 `ProjectLifecycleParticipant`，不把本地报告转换成全域 `StopReport`，不编造 PendingRef。

每个入口在首次 SQL、依赖发现或 confirmation 前登记私有 `{ProjectID, class, original-call}`：

| class | 实际入口 |
| --- | --- |
| mutation | `CreateAgent`、`UpdateAgent` 及其原 Unknown confirmation |
| read | 配置 `GetAgent`、`LookupAgentCommand`、`ListAvailableRunners`、`GetRunnerMounts`，以及 `DirectoryReader.GetAgent/ListAgents` |
| control | 本提供方的 Request/Inspect，自身不进入 Project 待停止集合 |

1. 拒绝非法 ctx、非 Project Scope、非法 cause、非正式 `ProjectLifecycle` 服务身份，以及 actor ProjectID/CauseRef 与 scope/operation 不符。调用本身登记到 Service 和 DirectoryReader 的 control 生命周期，按固定 Service→Directory 顺序操作两套登记；任一登记失败均收尾已取得的原登记。进程级 Stop/Drain 必须等待原 control 事务及函数实际返回。
2. 使用原 Store 短事务取得 Project SH，经原 Project Authority 调用 `ValidateLifecycleInTx(..., SkillsParticipant, StopPhase)`，核真实当前 operation、accepted version、冻结 manifest 与合法 stopping 状态。在该事务内捕获原调用指针；只以短持有的原 mutex 保护快照，不跨 COMMIT、外域调用或等待持锁。Request/Inspect 本片均只支持这个正式口已授权的 stopping 阶段。
3. 原 `WithinTx` **实际返回 Committed**，且入口原 ctx 仍有效后，Request 才标记仍登记的捕获调用为 stop-requested，并取消这些原调用及其已登记 confirmation。NotCommitted、Unknown、授权/锁失败均返回原安全错误与零报告，零取消、零标记；不得重读一行后擅自改判原 Unknown。Inspect 每次重新核验同一真实授权及提交边界，始终不取消。
4. Unknown confirmation 与其原 mutation 连续归属，不能借 `WithoutCancel` 成为无 Project 身份的独立调用，也不能通过通用新准入脱离父调用的停止标记。confirmation 登记与 stop-requested/global stopped 检查使用同一原 mutex：先 stop 则后来登记的 confirmation 不得逃逸，先登记则由原 stop 捕获。保留既有至多 3 秒预算、原 CommitResult/attempt/cause 和只读确认语义，不新增 writer、不延长预算；若确认因停止而被拒绝或取消、未能确认原提交，仍返回原 Unknown，不把 lifecycle 取消或 LocalJoined 改判为业务已提交或已回滚。父 mutation 登记始终保留到原同步确认路径返回。
5. Request 不等待业务 I/O。原 call 持续占登记直到包括 confirmation 在内的同步业务函数真正返回并执行原 done；不能在 cancel、回调退出或 token 失效时提前退休。授权捕获后新准入的匹配 call **不能被旧快照追认取消，但必须计入当前 PendingCalls**。Request/Inspect 在确认提交后，按同一固定 mutex 顺序统计两实例当前匹配调用；只对这次本地观察给出 `LocalJoined = PendingCalls == 0`。

stop-requested 只属于捕获的原调用，不产生永久 Project 本地 gate；后来调用仍通过原真实 Project 当前权限。不得持 SQL 事务等待 join；进程级 Stop/Drain 继续覆盖全部业务、confirmation 和 control，且不关闭共享 Store 或其他提供方。报告默认格式不得泄漏 Agent 配置、凭据或原始内部错误。

## 写域与并行责任

| 路径 | 允许的最小变更 |
| --- | --- |
| `internal/central/agent/{calls.go,service.go,commands.go,reader.go,runner_mounts.go,directory.go}` | 调用 Project/class 登记、原 mutation/confirmation 连续归属及必要私有辅助；原业务逻辑与权限不改 |
| `internal/central/agent/project_lifecycle.go`、`project_lifecycle_test.go` | 新本地提供方、报告和定向控制测试 |
| `internal/central/agent/{calls_test.go,command_lifetime_test.go}` | 仅受上述登记变更影响的原测试适配及全局退出回归 |
| `tests/projectvariable/agent_project_lifecycle_test.go`、`agent_project_lifecycle_fixture_test.go` | 独有真实组合与确定性 barrier；复用已有生产提供方和 fixture 能力，不覆盖旧验收 |
| `tests/testsupport/agentconfiguration/assembly.go` | 可选真实 LifecycleAuthority，沿同 Store 的原构造图绑定；nil 保留原组合语义 |
| `.agent-state/agent-project-local-stop/`；共享 `root_chain_driver.py`、`pg_only_supervisor.py` 的本片 profile | 复用原验证方法，仅限定 selector、输入与结果计数；资源及 Wait 协议不改 |
| 本卡及本片 README | 已确认范围、实际验证结果与未完成边界 |

上述 assembly seam 和两个共享 profile 是 root 单独分配的必要验证增量；旧 PG fixture、SQL/迁移、公共合同、app、Model、Execution 均未因此开放修改。不修改 `.agent-state/current.md`。Model 纯契约实现使用独立 worktree 并行；共享真实资源窗口由 root 协调，执行者不做 Git 操作。

## 五项必要验收

1. **构造与当前权限**：同 Store/Authority 正例；nil/零值、跨 Store/Authority、错 actor/project/cause/version、未授权 phase 拒绝。真实授权事务 Unknown/回滚/取消时零报告、零取消、零 stop-requested；不能用公开 UUID 或已有行替代实际提交结果。
2. **选择与动态准入**：Archive 仅本 Project mutation，Delete 增加两实例的 read；其他 Project 及 control 不受 Project stop 取消。覆盖上述全部 read 入口登记，Inspect 不取消；捕获后新 call 计 pending 而不被旧快照取消。
3. **confirmation 与退出竞争**：先 stop 后 confirmation、先 confirmation 后 stop 两种顺序；原 mutation/confirmation 或 control held 时仍不 join，实际 return 后才退休；进程级 Stop/Drain 不越过原 control/confirmation。使用确定性 barrier，保原 Unknown 和预算，不靠 sleep 猜调度。
4. **首条真实 PG 链**：真实 Account、同 Store/Project Authority、Agent Service 全部真实依赖与 DirectoryReader。fixture 只建立尚未生产开放的合法 initialized Project 和 accepted operation/完整冻结 manifest 前置；由原 `LifecycleStopDriver` 实际提交 `accepted→stopping` 后调用新提供方，不以测试 SQL 写本轮 phase/claim 结果。实际原业务在 gate 前 barrier 持有，验证提交后取消、原调用 held 时 pending、释放并真实返回后 LocalJoined；另一 Project 继续可用，Archive 合法读保留，Delete 覆盖目录读。participant 状态不被写成 stopped/completed。fixture 不假冒生产 Create/Archive 或完整 Registry 已可用，不伪造成功报告。
5. **收敛与有限兼容**：定向 Agent race/vet 和受影响原 lifetime 回归；纯控回答问题后尽早进入上述真实组合，输入未变的通过证据复用。所有实际调用即使断言失败也必须真实 join，按既有 owned fixture 核原 Wait/资源退出尾；原失败保留。正式交付由未参与实现者核验适用行为和真实证据，不扩大为整个 Agent/D08 验收。

## 实际验收结果

方法及结果详见[验证记录](../../../.agent-state/agent-project-local-stop/README.md)。`sequence01` 原工具 `94116/d4d54c` 实际退出 0：profile 1 正例／7 负例通过，27 项方法／源码输入前后相同；Agent race 精确 **10 top／29 sub**，单包 vet 通过，285 个输入的六份 maps 相同。Go 编译及精确 sole-top list 均实际返回 0；编译 2021 个输入的三份 maps 相同，运行集合仅增加测试 binary，合计 2022。

`native01` 只完成 prepare，容量快照 2,785,267,712 B 触发 hold，未启动原生验证。随后按 root 明确授权，仅回收 145 个精确列出的可再生旧 Go 包缓存 `-d` 文件，释放分配量 2,163,302,400 B；Agent 2022、Model 655／22 的 union 2680、49 份记录及四个身份保护相同。该例外不是全缓存清理或用 mtime 推定无引用，也未永久保留被回收原物。`prepare02` 原工具 `17869/6dfef3` 实际返回 0，使用新 namespace `/tmp/apls02`；4,726,820,864 B／1,704,318 inode 仅为启动前快照。

唯一实际原生轮次 **native02 PASS**：原工具 `5391/a0d6c9` 实际退出 0；Supervisor PID 1075262 原 Wait 0／108.910 s，Driver PID 1075263 和 Go PID 1076914 原 Wait 均为 0。真实 PG 精确 **1 top／2 sub**，17.94 s；Archive 9.67 s、Delete 8.27 s。实际 COMMIT-return／BEGIN-held 原调用返回屏障、Archive mutation 选择、Delete 双 owner read、跨项目保留及既有 Agent／引用／event 保全均通过。七个精确资源有 14 次缺席观察，descendant／private／runtime 双空及 HOST_TCP 双 delta-empty 齐全；2022 个实际输入与四个身份的 post 核对相同，无 race。

首个方法保存曾因 basic 文件末尾多一个 LF 静态检查失败，修正后才保存受测方法；原失败记录保留。另一个 Model worktree 的 test FAIL 属其自身验证，本卡不把它或 prepared-only 容量 hold 记为 Agent 业务失败。本轮未据此声明已合入 main 或已清理工作树／分支。

## 本片不解除的缺口

`LocalJoined` 只描述这两个实例的原调用，不证明其他 Agent/Skills/Variables 消费者、foreign process 或全域停止。完整 participant 仍需可恢复的原 work/process 事实、真实退出或精确 `ConfirmStopped` 及原事务终局重验；原 Stop Driver 的 claim terminal 不能替代这些业务证明。

完整清理仍需各引用所有者的退休与有序清理；既有 Execution 环境 lease 使用资格退休保留历史引用/FK，不等于可以物理删除。本片不新增 ledger、清理 SQL、永久 gate，不禁用约束，也不接生产 app、initializer、创建 HTTP 或完整 LifecycleRegistry；不得加载部分成功 adapter 冒充全域绑定。已有 Project/Skills 的完整锁 union 与 same-Tx `ConfirmInitializedInTx` 原协议继续保留，既有 STOP 与全平台 ready 边界不变。
