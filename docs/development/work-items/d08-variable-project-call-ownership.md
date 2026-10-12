# D08 Variables 项目业务调用归属

状态：消息设计 v1、canonical ProcessID FK、本地 pending 并集及 actual-return 共同前提已获独立设计审和 root 采纳，现正式落卡。工作树 `/workspace/agenteam-variable-project-call-ownership`，分支 `ai/variable-project-call-ownership-20261012`，基线 `a117e958c901a6146168c7837b3da0cc20377c2b`；建树实核迁移最大号 74，本片独占后继 `00075_project_variable_call_ownership.sql`，只写 forward-only Up，不写 Down。尚未实施或运行本片测试；旧 Agent 的通过结果不代本片验收。只用本树来源，无已清理 worktree 的 source/bin/output/cache 依赖。

本片给显式接入的普通 Variables `Service` 与 `SecretService` 增加持久调用登记、原返回结算和 foreign 项目业务闭合观察。基础是[现有普通服务](../../../internal/central/projectvariable/service.go)、[Secret 服务](../../../internal/central/projectvariable/secret_service.go)、[本地停止提供方](../../../internal/central/projectvariable/project_lifecycle.go)及已交付 [Agent 调用归属协议](d08-agent-project-call-ownership.md)。不将两个实例、调用表为空或进程死亡升级为全 legacy/global Joined，也不交付完整 `agent-skills-variables` participant、Project Initializer/Registry、App/HTTP 接入、引用退休或 D04 资源关闭。

## 固定组合与报告

新增纯构造入口 `NewProjectCallOwnership(store Store, ordinary Dependencies, secrets SecretDependencies, guard *object.ProcessGuard, expected objectcontract.ProcessID) (*ProjectCallOwnership, error)`。返回固定 `Service()`、`SecretService()`、`LocalStopper()`，并提供 `Stop()`、`Drain(ctx) error`、`Joined() bool` 与 `ObserveStop(ctx, actor, cause, scope) (ProjectBusinessStopReport, error)`。同 Store、同 Variables Authority、同 Project Authority及 Secret Writes 的原 Store/Project 身份均须相同；guard 为真实 concrete 类型，expected 为正式 ProcessID。发布指针前一次组合完毕，不接受 public proof 替身、setter 或成功 fallback。

构造无 I/O/worker，可在真实 guard 绑定前进行，但不在构造时冒称 same-Tx/ready。首次登记和闭合事务必须通过已有 [RequireCurrentProcessInTx](../../../internal/central/object/process_binding.go)，核 expected、bound/live 及原 Store 的真实 live Tx；guard 短锁不跨外部 Store 调用。原组合根持续持有 guard，不能以该方法代 pin 或死亡证明。旧 `New`、`NewSecret`、`NewProjectCallStopper` 的参数及默认语义保持，不查询或写新 ledger，不增加默认 readiness 条件。

`ProjectBusinessStopReport` 使用私有不可伪造状态，零值无效；只读 `Valid/Details/Matches`。Details 包含精确 ProjectID、OperationID、Action、ProjectVersion、PendingCalls、More 和 **RegisteredProjectBusinessJoined**，不公开 CallID、ProcessID、Secret 或 SQL。报告只表示本次观察点的已接入项目业务集合；它不实现 `ProjectLifecycleParticipant` 或 `StopReport`，不能由外部 bool 构造，也不能作为 Secret 全域退出、引用退休或完整生命周期状态推进的授权。

## 唯一持久事实

新本域表 `agenteam_projectvariable.project_calls` 只保存：`id`、`project_id`、`process_id`、`instance_id`、`lane`、`class`、`phase`、`registered_at`、`closed_at`。四个 ID 使用本域 `safe_id`；每次原调用先生成不可复用随机 CallID，同 command key 的两个调用仍各有 ID。`lane` 为 `ordinary|secret`，两 lane 的 `class` 均只允许 `mutation|read`；`phase` 为 `running|business_closed`。

- `id` 为主键；`project_id` 引用 `agenteam_project.projects(id)`，`process_id` 引用已有 [Object canonical process_claims](../../../db/migrations/00007_object_transfer.sql) 的 `process_id`，均 `ON DELETE RESTRICT`。Process FK 只证真实 claim 行存在；仍须登记原 Tx 的 guard/expected/Store 验证，不以 UUID、同 DSN 或存在行替代。ProcessGuard 的真实 bind 已先持久建立该 claim；其 `claimed→stopped` 状态变化保持原规则。
- check 约束精确 lane/class/phase、running 时 closed_at 为空、business_closed 时非空且不早于 registered_at。触发器只准 INSERT running；原 tuple（含 id/project/process/instance/lane/class/registered_at）不可改；UPDATE 只准 running→business_closed，拒重开及终局再写；拒 DELETE。既有历史、FK 与业务回执不清理，不 TTL/cascade，不新增审计事件或私自改其他 owner 的表。
- 索引 `(project_id, class, id) WHERE phase='running'`。保留闭合行作为调用归属历史；不保存 value/name/description、Secret 指纹、请求内容、业务 receipt、command key 或 Secret 技术 nonce。原 commands/history/execution environment 表不能替代全部读调用与真实 process 登记。

## 十二入口、权限与原事务

| Lane | mutation，每次原调用一条 | read，每次原调用一条 |
| --- | --- | --- |
| ordinary | CreateVariable、UpdateVariable、DeleteVariable | GetVariable、ListVariables、LookupVariableCommand |
| secret | CreateSecretVariable、UpdateSecretVariable、DeleteSecretVariable | GetSecretVariable、ListSecretVariables、LookupSecretVariableCommand |

空查询结果同样登记；completed 命令 replay 仍为 mutation，但其原 Read 优先级保持，不借登记替代新写入的 Mutate 门。Archive 只选 mutation，Delete 再含 read；其他 Project 不选。Request/Inspect/Observe 与登记/结算是 control，不递归写 ledger，但原实际返回必须计入两个 owner 的 Stop/Drain。原 Unknown confirmation 延续原 parent/CallID/process，不新登记、不新业务 writer。

先取得原本地 call 准入并完成纯校验，整个 pair 至多保留 256 个 admitted/returned-unconfirmed owner，登记前即计容量；Unknown 不能被丢弃腾位置。独立登记 Tx 一次取得 User SH＋Project SH 完整锁，再核 guard 原 Tx/expected、当前 Project Owner Read 并 INSERT。原 Store **实际返回 Committed 且原 ctx 仍 live**后才能进入外域 discovery/业务；NotCommitted 零业务，待其实际返回才释放；Unknown 零业务并保留原 attempt/cause/CallID，不用 command confirmation 猜登记状态。

所有原项目业务事务仍先取得原完整锁并集，在第一次领域业务 SQL 前核 private call 的完整 tuple 与 running phase；Project 锁持有到原 COMMIT/rollback。terminal/missing/错 owner 拒绝，旧默认分支不做新 SQL。原权限、Activity、Event、Audit 和 reference provider 事务边界不改，不在锁内补晚到锁或另起 writer。

Secret 原 [Writes.Discover/CheckInTx 的 current 门](../../../internal/central/projectvariable/secret_write_authority.go)也必须消费同一不可伪造私有 ctx witness，核原 ownership、Store、Project、secret lane 与 running；不能只包住 PV final Tx。沿原 ctx 的 D04 Match/Lookup/Apply 进入这个既有同 Tx 门。SecretWriteAuthority 不增加 setter、Service 反向依赖或 public 注入口；无 marker 的旧调用保持原行为，出现错误 marker 必须拒绝。普通 lookup 与两 lane 原 confirmation 使用同一个 witness；WithoutCancel confirmation 仍保原 mutex/stopRequested 协议和原至多 3 秒预算。

## 原返回与 Secret 技术边界

[PrepareProjectVariableWrite](../../../internal/central/secret/project_variable_prepare.go) 的 CheckPlan 无 ctx/Tx；其 control 读取及 [nonce 范围预留](../../../internal/central/secret/nonce.go)属于原 D04 技术 SQL，不持 Project 锁。nonce 预留可独立提交，但不等于 Secret Variable 业务写入。此片不改 D04 schema、nonce 算法或关闭规则，也不承诺 Project EX 能等待该技术 SQL 物理终局。

本地 owner 只有在原业务、所有同步 provider/Prepare、原 intent/prepared 的安全 Destroy、rows/Tx/confirmation 均实际返回后才记 returned。随后原调用拥有一次同步、至多 3 秒 WithoutCancel 结算：同 Store Project EX＋guard＋exact tuple，将 running 改为 business_closed；结算原 Store 实际 Committed 返回且结算 ctx live 后才忘记原 owner。不是 callback 返回、cancel、超时或 token 失效就算返回。这个新增结算预算不刷新原 confirmation；显式 Observe/Drain 使用各自原剩余 ctx，不逐页续预算或启后台自动重试。

结算错误/Unknown 保留原 ID、私有 returned 证明及容量，不改写原业务 known/Unknown 结果，也不重放业务。known Committed 登记缺行必须报错；只允许 registration Unknown 在 exact Project EX 切面确认 absent。**本地 owner 退休须 original returned AND（registrationUnknown 的确证 absence OR 同 tuple business_closed 已确认）**，持久 closed 不能单独越过本地原返回。Stop 后只为这些原 returned owner 开内部结算，不重新公开 begin、Observe 或生成新 CallID；两服务及 pair 的 Drain/Joined 均计其实际结算尾和未确认 owner。

foreign 的 guard 死亡证明＋Project EX 只关闭该项目业务：已持 Project 锁的原业务 Tx 终局，迟到原业务 Tx 被 running fence 拒绝。它不证明 D04 全局 nonce 技术 SQL 已结束；其资源仍归原 D04 owner。本片的十二入口也不包含 ExecutionEnvironmentProvider、CurrentProcessEnvironmentProvider、环境退休、Secret refs、SecretUsage/lease 或其他同 Tx 借用者，不因此宣称全部 Variables/Secret 调用都停止。pair.Joined 只反映本 pair 的原返回和结算，仍非其他提供方或全进程证明。

## 有限观察与 pending 并集

ObserveStop 先作为原双 control，在短 Tx 取得 Project SH，核 guard 与真实 LifecycleAuthority 的当前 actor/cause/scope/version、SkillsParticipant、StopPhase，捕获所选 CallID/tuple/local owner。原事务实际 Committed 返回后才在 SQL 外调用真实 ConfirmStopped；同 process 只能使用精确本地 returned 证明，不凭其他实例无行或时间推断死亡。final 原 Tx 取得 Project EX 后重新授权并检查同 tuple/phase，才将确证的 running 行改为 business_closed；只在该原 Tx 实际 Committed 返回、ctx live 后发布报告，Unknown 不发布成功。

按 CallID 固定顺序每次读最多 100＋1 行。第 101 行只令报告 More/pending，不阻前 100 行已确证者合法闭合；下轮重新读取当前 running 集合，已闭合行退出集合。前批 live 阻住后项时如实 pending，不截断后报成功、不无界扫描或重设预算。

最终 Project EX 及实际返回后的报告必须并集所选 running 行与本 pair 的 local calls/owners，按 CallID 去重；包括 registration 原返回未结束、registrationUnknown 无行、原 confirmation/安全清理/checkpoint 未实际返回和 checkpointUnknown。固定 ordinary→secret 锁序读取本地事实；捕获后新 local 调用计 pending，不追认取消。不能单凭 running count=0 报 RegisteredProjectBusinessJoined；既有 closed 行也不能消去仍未实返的本地 call。最后重验属于观察点事实，不建立永久 Project gate。需要取消本地原调用时显式复用 LocalStopper 的授权实际提交后取消协议，Observe 自身不发 remote cancel。

## 写域、协作与最小验收

产品唯一作者写 `internal/central/projectvariable/{service,secret_service,commands,reader,secret_commands,secret_reader,secret_write_authority,project_lifecycle}.go` 的必要差额及新的私有 `project_call_*` 文件族、迁移 75；不改 Object、Secret、Agent、App 或其他 schema。独立作者负责必要纯控、真实 PG 三新 `tests/projectvariable/variable_project_call_ownership{,_fixture,_process}_test.go`（`TestVariableProjectCallOwnership`，1 top / 3 sub）和复用方法。单卡精准 checkpoint 后统一放行各 writer；集中保存只短暂停其他写域。Go/cache/真实资源窗口由 root 协调，所有作者不自行 Git。

PG foreign 夹具新增的 spool 交接时序尚待本片动态验证：child 在 registration 实际 Committed、原 Store return 仍受屏障保持期间，仅对 idle Object.Service StopAdmission 并实际 Drain 以释放 spool；不调用 Runtime.Drain，不释放真实 guard，也不消去 pair 的原调用归属。observer 才能在同一 spool inode 上完整 Initialize，真实 ConfirmStopped 对仍存活的 child 返回 Busy 后，再放行原 Variables 业务；后续只借同 Store/guard，不调用已停止的 Object 业务。此前置不代表 Runtime、D04 或全图 Joined；pair 原返回及 Store/guard 仍须保持到原 owner 实际退出或其进程实际死亡，不能借既有 Agent 验收推定本时序已通过。

1. **非空入口与默认兼容：** 真实 pair、same Store/authorities/guard，普通与 Secret 各六入口实际建立非空事实和独立 CallID；旧构造无 ledger。registration 实际 COMMIT 未原返回、NotCommitted/Unknown 均零业务/nonce；wrong Store/guard/process/FK/tuple 不放行。
2. **完整原返回：** 真实入口及必要受控 Store barrier 区分 Prepare、confirmation、Destroy、业务 Store、结算原返回；原 Unknown 维持同 parent。无 row 的 Unknown/local pending 与已 closed 但本地未返均不能报成功；Stop 后仅原 ID 结算，cap 与失败原结果保持。
3. **业务 fence 与选择：** 真实登记后，在独立 Project EX 下对 exact row 注入合法 terminal 状态，分别验证普通及 Secret authority 原业务门拒绝、零新业务；这是明确 SQL 夹具，不冒 foreign 死亡。Archive mutation/Delete read、跨 Project、control、100＋1/逐批进展均保持。Secret 已消耗 nonce 不伪称业务已写，也不据 Project EX 宣告技术 Tx 终局。
4. **真实 foreign/原 SQL 尾：** 已完成普通命令在 Archive 后合法 Read replay。observer capture 真 COMMIT 后仅 hold 原 Store return，child 才取得原业务 Project EX并由原 COMMIT proxy 保持；亲 SIGKILL＋唯一 Wait 后放 capture 原返回，再由真实 guard 证明死亡。final EX 被原 upstream EX 挡住时，原 Observe 尚未返回且无成功报告；独立 SQL 行仍 running。释放真实 COMMIT/Ready/HeldJoined 后，final 实际返回才发布 business-joined。真实锁按 ExclusiveLock 核，不假称 ShareLock，不放宽原预算。
5. **资源与边界：** 两 lane actual Drain/Join 在原 D04、Skill、Installer、Runtime/guard 清理前；失败保有实际 owner，不能超时当 Join。复用正式完整 fixture 图与原唯一 Wait/资源尾门，固定安全诊断，无明文/凭证输出。未覆盖 owner、引用退休、历史 FK、完整 Registry/Initializer 和 App 默认入口继续保持原缺口，不以本卡或局部 PASS 代正式完成。
