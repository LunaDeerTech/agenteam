# D08 App Agent 调用归属装配与退出闭环

状态：设计候选，仅本卡获写入授权；未实现、未执行。本树基线 `d64a8f8cc03167b4582a9ea64aa277dfa48da9ac`，包含迁移 74 和 Agent foreign 夹具修正，修正仍待真实验收，不能称 native PASS。正式交付须先合入已接受的上游结果。

本片让默认 App 的 Agent 配置、Runner Mount 配置读取及目录 HTTP 使用同一个真实 `agent.ProjectCallOwnership`，并由一个 `accountWork` 管理其完整退出。沿 [Agent 管理规范](../../architecture/agent-management.md)、[持久调用归属卡](d08-agent-project-call-ownership.md)及[团队流程](../agent-team/README.md)。不新增迁移，不改变 Agent 业务权限、命令回执、原预算或公开默认构造；不交付 Project fullparticipant、初始化、引用清理或 legacy global join。

## 固定装配点与接口

现 [account.go](../../../internal/central/app/account.go) 在 Skill Service 构造前独立调用 `createAgentDirectoryWithAuthority`，之后才以 `createAgentConfiguration` 建写服务。将默认 App 的目录实例创建移到既有 Skill Service、InstallAuthority、Registry/四引用提供方均已齐备的配置装配点，统一调用现有 `agent.NewProjectCallOwnership(store, deps, keys, objects.guard, objects.process)`。配置 assembly 持有这个固定 pair；配置及 Runner Mount handler 使用其 `Service()`，目录 handler 使用其 `DirectoryReader()`。不得先建原服务再 setter 转换、用假 pair 填空，或只替换其中一个 lane。

此顺序无构造环：原 Agent Authority 已先建立；[Skill AgentInitializer](../../../internal/central/skill/agent_initialization.go) 只依赖该 Authority 的创建口，不依赖 Agent Service/Directory。Skill Service 依赖原 Object/InstallAuthority，Work planning 依赖原 Agent Authority，也不依赖目录实例。原 Account core、Object service、Skill service 的创建顺序保持；仅目录实例延后，HTTP 路由仍在整图构造完成后发布。

`agentConfigurationAssembly` 是默认 App 唯一的 Agent `accountWork`，同时拥有原 Registry Initialize 和 pair；目录 assembly 只作为该 pair 的 handler 引用，不在此 App 的 `works()` 再登记为第二退出 owner。所有提供方固定后、下一次可失败装配前，沿原 `accounts.install` 登记完整 assembly，保留取消/Force 与晚到 acquisition 的原归属；构造失败不得遗失已取得的 pair。旧 `agent.New`、`NewDirectoryReader`、`NewProjectCallStopper` 及其无 ledger 默认语义保持，不增环境开关、通用 fallback 或公开可变接线口。

pair 构造无 I/O，允许原 concrete guard 尚未 bound；这不证明 Store 或运行就绪。原 `deps.objects` 执行真实 `Object.Runtime.Initialize`，再由 `accountAssembly.Start` 执行 Registry Initialize，确认原事务返回后才推进启动并开放 HTTP。首次 Agent 登记及退休事务继续通过 `RequireCurrentProcessInTx` 验证原 live Tx/Store、bound guard 与 expected process，未实际 Committed 前零业务；不以构造成功、同 DSN 或 ProcessID 代替该证明。

## 生命周期与资源归属

- `StopAdmission` 取消原 Registry Initialize，并调用 pair 的 `Stop` 同时关闭 Service/Directory 新准入。`Drain(ctx)` 等 Initialize 原返回，再调用 pair 原 `Drain(ctx)`；`Joined` 同时要求初始化已返回及 pair 实际 Joined。只操作该固定 pair，不另启后台恢复、公开 Observe 或新 CallID。
- `Force(ctx)` 沿原剩余预算走同一实际排空路径，不把取消、超时、回调结束或 force 尝试当 Joined。登记 Unknown、业务原 confirmation、退休 checkpoint Unknown 与目录原调用均由原 pair 保留，停后仅原私有 CallID/returned 结算通道可继续；业务结果不会被退出结果改写。
- `accountAssembly.works/Force` 必须以这个联合 owner 判定 Agent 完成。正常 Drain 按依赖保留原 InstallAuthority、Skill/Account/Store，供原调用收尾；保留 Execution/Runner 在前、Agent 在 Install/Skill/Account 前的顺序。Force 中联合 owner 未 Joined 时停止该 accountWork 顺序中的下游 provider 退休，但根部原 `cleanupResources` 仍会按原预算调用 `store.ForceClose`，可能终止 DB socket；本片不保证 Force 后 Store 持续可用，不修改 `resources.go`。pending/退休 Unknown 不因此变成 Joined，原 joint producer check 继续保留未 Join 借用者的 Object guard；HTTP server 结束不代调用结束，根部 Object Force/guard 规则不改。
- 本片只覆盖此 App 新协议的实际双 lane。`RegisteredJoined` 不是全旧实例或混版集群证明，不将 pair/LocalStopper 注册成完整 `agent-skills-variables` participant，不从空 ledger 推导全域安全。

## 确定写域

| 文件 | 最小变更 |
| --- | --- |
| [app/agent_configuration.go](../../../internal/central/app/agent_configuration.go) | 同一完整依赖构造真实 pair；assembly 持 pair 与原 Registry Initialize，联合 Stop/Drain/Joined/Force |
| [app/agent_directory.go](../../../internal/central/app/agent_directory.go) | 目录 handler 接收该固定 pair 的 reader；保留原路径路由与公开域构造兼容 |
| [app/account.go](../../../internal/central/app/account.go) | 延后目录接线，原 install 登记唯一联合 owner，默认 HTTP 双 lane 与 Force provider 顺序 |

必要基础控限上述真实接缝：`app/agent_configuration_test.go`、`agent_directory_test.go`、`account_lifecycle_test.go` 中受影响的装配/退出/路由控制，必要时一个专用 `app/agent_project_call_app_test.go`。不重跑整个 App 或 Agent 矩阵来替代差额证明。真实链复用 [TestAgentConfigurationApp](../../../tests/projectvariable/agent_configuration_app_test.go) 的公开 `app.Run`、setup owner 全退后交接、原 HTTP Session/CSRF/Owner 和资源退出能力；新增场景优先放 `tests/projectvariable/agent_project_call_app_test.go`，只对共享 helper 作必要窄接缝，不改原业务断言。本卡不预设已存在的 hook/完整方法，实施时按真实接口确定最小范围。

## 最小验收

1. **构造与兼容：** 真实固定 pair 无构造 SQL/worker，Service/Directory 同源且只一个 accountWork；缺失提供方/guard/keys fail closed。未 bound 时不得入账或业务执行；原域默认构造保持原行为，正常/取消的部分装配均可由原 owner 收回。
2. **公开 App 双 lane：** setup 原所有 owner 已退出后启动真实 `app.Run`，当前 Owner 经正式 HTTP 执行配置 mutation 和目录 read；独立 SQL 核相同 process/instance、正确 lane/class、不同实际 CallID 及真实返回后的 joined。原配置 Get/Lookup、目录路由和权限拒绝保持；无 Model/Runner 执行要求。
3. **Unknown 与原返回：** 基础控复用上游已验语义，验证 App 联合 owner 不遗漏登记 Unknown、原 confirmation、退休 Unknown 或目录 pending；登记未确认零业务，原业务 known/unknown 不被 Drain 重写。受控 Store 的结果不冒真实 PG commit-loss；本片不额外重做上游 foreign 死亡矩阵。
4. **真实关停：** 用原 live HTTP/PG 调用及确定性锁/返回屏障覆盖目录未返回时的正常 App Drain；Stop 后无新准入，原 call/checkpoint 未终局时按依赖保留 provider/guard。屏障须在原 graceful 预算内释放，随后亲收原 HTTP、SQL、pair、App 及测试资源的实际完成；超时只记失败/未 Joined。基础控另覆盖启动取消、mutation 未返回，以及 Force 下联合 owner/guard 不虚报完成；不承诺 Force 后 Store 永久可用。真实夹具不得以 sleep、取消信号或伪报告代原 Join。

PG 失败保留规则：若原 App 或 guard 尚未实际释放，夹具先记录安全失败，再维持进程与 fixture 存活，只等待原实际释放或原 Go 6 分钟／外层 540+60 秒硬预算终止；这不代表本地 Drain/Join 成功，不刷新预算、不重试或进入后继子例。正常 PASS 仍须原 `app.Run` 实际返回 nil、原迁移 74 调用 joined、guard 释放及全部原资源尾门通过。

## 待兑现与边界

上游 Agent 修正的真实验收、上述 App 装配控制与真实 HTTP/退出链均待实际结果；现有受测结论按输入范围复用，不从卡或接口存在推导通过。真实 App fixture 的有界关停屏障和 Unknown 注入位置在实施时核准，不能为便利新增产品 test hook、扩大 shutdown 预算或越过 Store/guard 证明。

[project_update.go](../../../internal/central/app/project_update.go) 的 Initializer/LifecycleRegistry 继续未绑定；完整 Skills/Variables 排空、Execution/Agent/Skill 引用退休、完整 enabled manifest 与有序 cleanup 仍是后继。Object runtime join、Knowledge、真实 tools HTTP 等既有 STOP 不变；本片不改变其实现或用替身绕过，不宣称完整 D08、生产 Project 创建或全平台生命周期完成。
