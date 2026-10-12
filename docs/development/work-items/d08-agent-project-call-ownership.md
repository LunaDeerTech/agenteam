# D08 Agent 持久调用归属与 foreign 停止观察

状态：设计候选，待独立设计审；本卡不授权产品、DDL 或真实资源执行。基线 `3ac9f2a6e62e7aa31b744695444c7e844e195ebc`，工作树 `/workspace/agenteam-agent-project-call-ownership`，分支 `ai/agent-project-call-ownership-20261012`。基线迁移最大号实际为 72；73 由 Model 预留，本片仅预留 74，写迁移前由唯一 Git owner 再核编号。只依赖本树及后续明确引入的共享前置，无旧交付 worktree 的 source/bin/output/cache 依赖。

## 最小单片及依据

在[已交付本地停止提供方](d08-agent-project-local-stop.md)上增加显式启用的 Agent `Service`＋`DirectoryReader` 调用归属：业务开始前持久登记每个原调用及其实际 ProcessGuard 身份，原调用及 confirmation 真正返回后才能退休；观察者可在真实 foreign 死亡证明后，以同 Store 原事务锁边界确认该登记终局。它只观察接入这条协议的调用，不实现完整 Agent/Skills/Variables participant，不开放默认 Project 创建，也不退休 Agent 的业务引用。

依据限定为 [Agent 调用登记](../../../internal/central/agent/calls.go)、[原命令与 confirmation](../../../internal/central/agent/commands.go)、[命令事务](../../../internal/central/agent/command_store.go)、[目录读取](../../../internal/central/agent/directory.go)、[本地提供方](../../../internal/central/agent/project_lifecycle.go)、[真实 ProcessGuard](../../../internal/central/object/process.go)和 [Skill 原 work 退休协议](../../../internal/central/skill/runtime_work.go)。`agenteam_agent.commands` 是业务幂等计划/回执，不覆盖 read，也不能合并同 key 的两个实际调用；`agenteam_project.work_claims` 只拥有 creation/lifecycle driver。两者均不能代替本片 call 登记。基础规约仍用[团队流程](../agent-team/README.md)、[Go 技能](../../../.agents/skills/agenteam-go-development/SKILL.md)、[数据库技能](../../../.agents/skills/agenteam-database/SKILL.md)和[测试技能](../../../.agents/skills/agenteam-test-engineering/SKILL.md)。

单片最小闭环为“一张私有 call 事实表＋显式双实例工厂＋登记/原返回退休＋foreign 观察＋必要控制/真实 PG”。只做表而不约束业务开始，或只确认进程死亡而不重新取得原事务排他边界，均不能交付。ProcessGuard 同 Store 接缝是明确共享前置；引用退休、远程取消、自动恢复 worker 和完整 app 接线留在本片之外。

## 共享前置与显式装配

root 已确定唯一共享 Object 前置签名：`func (g *object.ProcessGuard) RequireCurrentProcessInTx(ctx context.Context, tx foundation.Tx, expected objectcontract.ProcessID) error`。这是待实现的正式新接口，不能假称当前已有。由唯一 Object 作者在 Model worktree 单独实现并保存，再由 Git owner 带入本树；Agent 作者不修改 Object 文件。

该接口用原 guard 的短锁取得/核验 bound、live、expected identity；锁外调用其原 `Service.Store.InTx(tx)`，验证实际 live Tx 属于同 Store，并在返回前重验 guard 状态及 ctx。不能锁内调用外部 Store，不能只按同 DSN/ProcessID 推断同 Store。它不执行 SQL、不授业务权限、不 pin guard、不证明 foreign 死亡，也不改变 `ConfirmStopped`。组合根必须继续持有同一 guard，直到所有借用者及其实际 SQL/退出尾 join。

拟定显式入口 `agent.NewProjectCallOwnership(store Store, deps Dependencies, keys cursor.Keyring, guard *object.ProcessGuard, expected objectcontract.ProcessID) (*ProjectCallOwnership, error)`。返回对象以只读访问器给出固定的 `Service()`、`DirectoryReader()` 及 `LocalStopper()`；工厂在发布任何指针前完成两实例、同 Agent/Project Authority、同 Store、同 ownership 的组合。不得把已有服务以 setter 改成另一种模式，也不接受调用方提交任意 call/process 字段作为证明。

显式组合另提供 `Stop()`、`Drain(ctx) error`、`Joined() bool`：Stop 关闭两实例原准入；Drain 先等待原调用/confirmation/checkpoint 实际返回，再只为已保留的原 CallID/returned 证明执行内部结算。这个停后通道不调用公开 Observe/LocalStopper 的 `begin`，也不向已关闭 callSet 插入新 control；它仍归原 ownership/lane，内部 checkpoint 的实际返回及未确认事实计入 owned 模式的 Service、Directory 和组合 Drain/Joined。默认模式不改，停后公开新调用继续被拒绝，无私有原证明不能借此获得准入。

构造无 I/O、无 worker，只固定实际 guard 和 expected 身份；允许在原 Object 初始化绑定 guard 前构造，**不在此宣称同 Store 已被证明**。第一次登记写入及后续终局事务必须调用共享前置，未绑定/关闭/跨 Store/错 expected 均 fail closed；登记未实际确认前零业务 SQL、零外域 discovery。nil/零对象不能作为成功默认值。

既有 `agent.New`、`NewDirectoryReader`、`NewProjectCallStopper` 的参数、默认路径和调用语义保持；默认构造不创建/查询新 ledger、不新增 readiness 依赖。真实 app 目前由 [agent_configuration.go](../../../internal/central/app/agent_configuration.go) 和 [agent_directory.go](../../../internal/central/app/agent_directory.go) 构造原服务，本片不改这些接线，也不加默认开启配置。真实 fixture 必须显式使用新工厂；未来 app 启用需另卡同时接入两个实际实例和 guard 的初始化/退出顺序，不能只开启写服务后把目录遗漏，不能用空 ledger 宣称默认 app 已被覆盖。

## 持久事实、准入与原返回

拟新增 `agenteam_agent.project_calls`；74 只预留 `00074_agent_project_call_ownership.sql`，当前不落 DDL。最小事实为随机稳定 CallID、ProjectID、ProcessID、组合实例 ID、lane（service/directory）、class（mutation/read）、单调 phase（running/joined）及登记/退休时间。CallID 在首次登记前生成且不复用；不可改绑 project/process/lane/class，不存配置、Secret、SQL 文本或业务回执，不用幂等 key 作为 CallID。CallID 主键；Project FK 为 RESTRICT，ProcessID 的真实性由原 guard 校验而非 UUID 自证；lane/class/phase 与 joined 时间一致性设约束，directory 仅允许 read。按 `(project_id,class,id) WHERE phase='running'` 查询未终局集合，不可变 tuple 和单调 phase 由 schema 约束/触发器拒绝改绑及复活。终局行保留，本片没有 TTL、物理清理或 cascade；DDL 仍须按 Agent schema 规范独审。

| 调用 | ledger 归属 |
| --- | --- |
| `CreateAgent`、`UpdateAgent` | 每次实际调用一条 mutation；同 command key 的并发调用仍各有 CallID |
| 原 Unknown confirmation | 继续同父 mutation、同 CallID、同 process，不新登记，不创建新业务 writer |
| `GetAgent`、`LookupAgentCommand`、`ListAvailableRunners`、`GetRunnerMounts`；Directory `GetAgent/ListAgents` | 每次实际调用一条 read，空结果同样覆盖 |
| lifecycle Request/Inspect/Observe、登记及退休 checkpoint | 控制调用；不递归登记 ledger，仍归原双实例 Stop/Drain，不能被当成 Project 待取消业务 |

1. 先取得原本地 call 登记，完成纯输入验证，再以独立短事务持 User SH＋Project SH：真实当前 actor/project Read 授权、guard 同 Store/expected 证明、插入精确 call。登记只证明 ownership，不替代任何原业务权限；命令 completed replay 的 Read 优先级保持，后续真正写入仍经过原 Mutate 门禁。只有原登记 `WithinTx` **实际返回 Committed** 且原 ctx 仍有效，才进入业务。NotCommitted 零业务；Unknown 保留原 attempt/cause 和私有调用记录，零业务，不能运行 command confirmation 替这次登记改判，也不新 CallID 重发。
2. 新工厂的每个正式 Agent 业务事务，包含 prepare/freeze/final commit、全部六 read、原 confirmation，仍先取得既有完整锁 union；在第一次业务 SQL 前重验该 call 的不可变 tuple 与 running phase，且原 Project SH 必须在同一 Tx 持续到真实 COMMIT/rollback。terminal/missing/错 owner 一律拒绝；普通旧构造不做这项 SQL。现有所有依赖 Apply/Audit/Event 仍在原同 Tx；外域只读 discovery 保持原同步归属，不能拆出后台尾。本片不据此宣称外域服务本身已经停止。
3. `WithoutCancel` confirmation 仍沿已交付的原 mutex stopRequested 协议，保持既有至多 3 秒、原业务 Unknown attempt/cause；先 stop 后确认不能逃逸，先确认后 stop 必须被原父调用捕获。登记和退休事务结果不能冒充业务事务的结果，不能用 joined 将原 Unknown 改判 known。
4. 只有原业务主体以及它调用的全部 provider、Tx、rows、同步 confirmation 实际返回，私有 owner 才能记 returned。最外层调用继续拥有一次有界、同步的退休 checkpoint，直到它实际返回；不得凭 cancel、ctx deadline、callback 退出或 `Store.InTx` token 失效提前记返回。checkpoint 在同 Store 取得 Project EX，重验 guard 与精确 tuple 后把 running 更新为 joined；成功提交之前不忘记私有证明。登记 Unknown 的无行结论也必须经过这个排他边界，不能裸查无行就退休。
5. 退休失败/Unknown 保留精确私有 returned 证明及原 checkpoint 结果，不能吞掉 ownership 或报告 Joined。业务已有的 receipt/错误/Unknown 保持原事实，不用记账失败降格已知提交或重发业务。后续显式 Observe/Drain 只重验同 CallID 的记账终局，不重做业务；原 Stop/Drain 必须等待所有实际 checkpoint 返回，未确认记账则返回安全错误或未 join，不能释放 guard。原调用仅做一次至多 3 秒的 `WithoutCancel` 退休 checkpoint，不增加原 confirmation 预算；显式 Observe/Drain 使用各自原 ctx，总预算不逐页重置，无自动重试/后台 worker。每组合最多保留 256 个 live/returned-unconfirmed owner，超限 ResourceBusy，不能丢弃证明腾位置。

这里的 returned 是实际业务 owner 的返回事实；最外层 checkpoint 的实际返回另由原 callSet/StopDrain 持有。磁盘终局、两实例本地 join 与全 participant stop 是三种不同事实，不能相互替代。

## foreign 观察与数据库终局

`ProjectCallOwnership.ObserveStop(ctx, actor, cause, scope)` 返回新的私有报告；复用正式 Project lifecycle actor/cause/scope，包含精确 cause、被观察的已登记 call pending、分页/溢出标志及 `RegisteredJoined`。它不是 `ProjectLifecycleParticipant`/`StopReport`，不改变旧 `LocalJoined` 含义，不公布 raw call/process 身份。`RegisteredJoined` 只说明本次完整观察的接入调用集合；不能声称未启用 ledger 的默认 app 或其他 owner 已停。

观察按三个阶段执行，任何阶段 Unknown/失权/调用取消都不能发布成功终局：

1. 原短事务 Project SH、共享 guard/Store 检查、`ValidateLifecycleInTx(... SkillsParticipant, StopPhase)`，捕获稳定 CallID/不可变 tuple 及精确本地 owner。原事务实际 Committed 返回后，才进入外部证明。Archive 只选 mutation，Delete 同时选 read；其他 Project 和 control 排除。调用方需取消本地调用时，先显式调用 `LocalStopper.RequestStop`，复用其真实提交后取消协议；Observe 自身不取消业务，也不请求 remote cancel。
2. 对本地进程，只接受该组合的精确私有 returned 证明；同 process 的另一实例、map 缺失或名字相同都不证明返回。对 foreign process，在 **SQL 事务之外** 调原实际 `ProcessGuard.ConfirmStopped`。live/self 为 pending；错 host/deployment/spool、丢失 claim、权限/存储错误为未证明，不能用 PID、TTL、heartbeat、probe、锁行过期或测试常量成功代替。证明只绑定当前捕获 process，不能转授或永久缓存。
3. 再以同 Store 短事务取得 **Project EX**，重验原 lifecycle gate、共享 guard binding、同 call/process/instance/lane/class 和当前 phase，然后才写 joined。Project EX 与所有原 Agent 正式 Tx 的 Project SH 形成终局切面：已持 SH 的原 SQL 必须先结束；尚在等待旧锁的 Tx 后来取得 SH，也必须先看到 joined 并拒绝继续业务。死亡证明和这一数据库边界缺一不可；不通过抓到 Project EX 就反推进程死亡。整个事务不持 callSet mutex 等锁/证明；本地 mutex 仍固定 Service→Directory。

只在最后原事务实际返回 Committed、原 ctx 仍 live 后发布报告/释放已证私有记录。终局 Unknown 保留原 pending，下次独立观察读取同 CallID 的实际持久事实，不新 writer/replay 业务。每次固定 CallID 顺序最多处理 100 行，以第 101 行判定溢出并明确 pending；溢出只阻止 joined 报告，不阻止前 100 行中已确证者合法退休。后续从当前 running 集合重新取前 100，已退休行退出集合，不凭旧页游标跳过后来插入的较小 ID；若前批 live 行阻挡后项，则如实 pending，不无界扫描或假报 full join。最终 Project EX 下重新核当前全部相关未终局集合；报告只是该观察点的事实，不建立永久 Project gate。新的 completed replay 等合法调用仍受原权限和自己的登记约束。

## 预期写域、顺序与验收

当前唯一写域是本卡。后续实现须 root 单独授权并分配作者/独审；预计范围如下，不以表格自动取得写权。

| 文件域 | 必要内容 |
| --- | --- |
| `internal/central/agent/{calls.go,service.go,directory.go,commands.go,command_store.go,reader.go,runner_mounts.go}` | 显式新模式、原调用/confirmation 归属及每个原 Tx 的 call fence；旧构造分支保持 |
| 新 `internal/central/agent/project_call_{ownership,repository,observer}.go` 及专控 `_test.go` | 工厂、私有事实、原返回/记账、有限观察；必要辅助保持该私有文件族 |
| 预留迁移 74、必要 schema 控 | 唯一新增 Agent call 事实，不改旧业务回执或 Project claims |
| 新 `tests/projectvariable/agent_project_call_ownership*_test.go`，必要显式 testsupport 入口 | 真 PG、真 ProcessGuard、原子进程/实际 SQL 终局与原 Wait；复用已交付 fixture 能力 |
| Object 共享前置 | 由另一唯一作者先交付 `RequireCurrentProcessInTx`，本片只消费；不另改死亡算法 |

顺序：共享前置独审/交付 → 本卡独审关闭设计点 → 最小产品＋专控 → 隔离真实 PG/进程链 → 有限既有 local-stop/默认构造兼容。适用 Go/cache/真实资源仍由 root 分配唯一窗口，作者不做 Git，不运行全站矩阵。

1. **默认兼容和实际装配**：旧构造及真实 app 路由不触新表；显式双实例正例。构造前未绑定 guard 不冒 readiness；首个登记拒绝 unbound/closed/wrong process/cross Store 的真实原 Tx，旧默认路径不因新前置不可用失败。
2. **准入与结果边界**：登记 COMMIT 实际返回前零业务，NotCommitted/Unknown 零业务；同 key 双调用拥有不同 CallID；六 read 均登记。业务已知提交不被退休失败改判，失败记账在 Drain/报告可见；无行、重绑/缺 row/terminal fence 都不能假阳性或重发业务。
3. **原返回与确认**：原 Tx callback 返回、物理 COMMIT 返回、业务主体返回、confirmation 返回、最终 checkpoint 返回分别设置确定性 barrier。先 stop/先 confirmation 两序仍同父；取消不提前 retired；registration/finalization Unknown 留精确证明，重观察只读取/补记原 CallID，control 计入双 owner 全局 Drain。
4. **真实 foreign 及 SQL 尾**：两个实际进程、同部署/同 spool 的实际 guard，原 live foreign 稳定 pending；保留 child 原 Wait/exit 后获得精确死亡证明。仍持 Project SH 的真实 PG 原事务未终局时，观察者不能提交 joined；其结束后才可过 Project EX；另测旧 Tx 死后晚获锁仍被 running fence 拒绝。此边界用真实 PG 锁/原 backend 事实，不能仅用内存 channel 或假 ConfirmStopped。
5. **选择、有限观察与范围**：Archive mutation/保读，Delete 两 owner read，跨 Project 不误退；同 process 无私有证明、foreign alive/证明错误、页溢出、新行、错 actor/cause/version 均不报 RegisteredJoined。实际 provider 驱动 accepted→stopping，participant 行保 required，Agent/引用/业务回执保全，旧 LocalJoined 仅两个实例。真实原 Wait、资源全尾和适用 race/vet 另留执行证据，本卡不写预期 PASS。

## 不包含与设计关闭点

不实现远程取消、租约续命/抢占、引用退休、Agent 删除、历史 FK 清理、Model/Execution 调用、完整生命周期 Registry、默认 initializer 或 Project 创建开放；进程死亡证明不能代替任何引用所有者的释放授权。共享前置尚未带入是实施阻塞项；卡内表约束、返回证明容量和观察页上限须经设计独审，不以 nil/no-op 或无限重试代替。若需要扩大 app、Object 或外域 provider 事务写域，先报告具体不闭合边界，由 root 指派共享 owner 和次序。
