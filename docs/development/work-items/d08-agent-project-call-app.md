# D08 App Agent 调用归属装配与退出闭环

状态：三个 App 装配文件、专用基础控和首次真实 HTTP/PG 链已有限验收。basic 在 `763576b88b4ed6f819b3a067ac8007c7bfd3352f` 实际通过 5 top / 6 sub 及一个 App vet；合入上游完整 Skill/Object 夹具修正后，`7e202fcb1c471c13f812e6880500e2d7bff3345b` 的 profile、compile/list 和 native01 whole 均实际通过，原 basic 真实输入相同并复用。本片无失败运行或重试。上游 Agent native02/native04 的原 FAIL、native03 容量 HOLD 均保留，不回填为本片结果。完整历史已正常合入并推送 main `33feecbd`；原件保全独核接受后已普通清理旧树和本地分支，feature远端原本不存在，不计远端删除。

本片让默认 App 的 Agent 配置、Runner Mount 配置读取及目录 HTTP 使用同一个真实 `agent.ProjectCallOwnership`，并由一个 `accountWork` 管理其完整退出。沿 [Agent 管理规范](../../architecture/agent-management.md)、[持久调用归属卡](d08-agent-project-call-ownership.md)及[团队流程](../agent-team/README.md)。不新增迁移，不改变 Agent 业务权限、命令回执、原预算或公开默认构造；不交付 Project fullparticipant、初始化、引用清理或 legacy global join。

## 固定装配点与接口

原 [account.go](../../../internal/central/app/account.go) 在 Skill Service 构造前独立调用 `createAgentDirectoryWithAuthority`，之后才以 `createAgentConfiguration` 建写服务。本片已将默认 App 的目录实例创建移到既有 Skill Service、InstallAuthority、Registry/四引用提供方均已齐备的配置装配点，统一调用现有 `agent.NewProjectCallOwnership(store, deps, keys, objects.guard, objects.process)`。配置 assembly 持有这个固定 pair；配置及 Runner Mount handler 使用其 `Service()`，目录 handler 使用其 `DirectoryReader()`。不得先建原服务再 setter 转换、用假 pair 填空，或只替换其中一个 lane。

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

实际新增基础控仅 [agent_project_call_ownership_test.go](../../../internal/central/app/agent_project_call_ownership_test.go)，5 top / 6 sub，并执行一个 App vet；没有扩跑整个 App/Agent 矩阵。真实链为独立 [agent_project_call_app_test.go](../../../tests/projectvariable/agent_project_call_app_test.go)，1 top / 2 sub，复用公开 `app.Run`、setup owner 全退后交接、原 HTTP Session/CSRF/Owner 及资源退出能力。未改旧 fixture/assembly、预算或生产 test hook。

## 最小验收

1. **构造与兼容：** 真实固定 pair 无构造 SQL/worker，Service/Directory 同源且只一个 accountWork；缺失提供方/guard/keys fail closed。未 bound 时不得入账或业务执行；原域默认构造保持原行为，正常/取消的部分装配均可由原 owner 收回。
2. **公开 App 双 lane：** setup 原所有 owner 已退出后启动真实 `app.Run`，当前 Owner 经正式 HTTP 执行配置 mutation 和目录 read；独立 SQL 核相同 process/instance、正确 lane/class、不同实际 CallID 及真实返回后的 joined。原配置 Get/Lookup、目录路由和权限拒绝保持；无 Model/Runner 执行要求。
3. **Unknown 与原返回：** 基础控复用上游已验语义，验证 App 联合 owner 不遗漏登记 Unknown、原 confirmation、退休 Unknown 或目录 pending；登记未确认零业务，原业务 known/unknown 不被 Drain 重写。受控 Store 的结果不冒真实 PG commit-loss；本片不额外重做上游 foreign 死亡矩阵。
4. **真实关停：** 用原 live HTTP/PG 调用及确定性锁/返回屏障覆盖 HTTP 已知返回、原目录调用退休 checkpoint 尚未终局时的正常 App Drain；Stop 后无新准入，原 call/checkpoint 未终局时按依赖保留 provider/guard。屏障须在原 graceful 预算内释放，随后亲收原 HTTP、SQL、pair、App 及测试资源的实际完成；超时只记失败/未 Joined。基础控另覆盖启动取消、mutation 未返回，以及 Force 下联合 owner/guard 不虚报完成；不承诺 Force 后 Store 永久可用。真实夹具不得以 sleep、取消信号或伪报告代原 Join。

PG 失败保留规则：若原 App 或 guard 尚未实际释放，夹具先记录安全失败，再维持进程与 fixture 存活，只等待原实际释放或原 Go 6 分钟／外层 540+60 秒硬预算终止；这不代表本地 Drain/Join 成功，不刷新预算、不重试或进入后继子例。正常 PASS 仍须原 `app.Run` 实际返回 nil、原迁移 74 调用 joined、guard 释放及全部原资源尾门通过。

## 实际验收与后继边界

已完成的基础控仅为 [agent_project_call_ownership_test.go](../../../internal/central/app/agent_project_call_ownership_test.go) 的 5 top / 6 sub，各 RUN/PASS 一次，无 FAIL、SKIP 或 race warning；同包 vet 通过。原 method `1169230` Wait `0`／`303.568s`，race `1169361` 为 `0`／`164.018s`，vet `1171621` 为 `0`／`136.935s`，两次依赖发现均 `0`。原工具 `96017`／final `5b813a` 返回 `0`；1133 项输入的六份 maps 和实际 post hash 相同，outer 16 项相同，受测 HEAD clean，post `4cdc39` 为 `0`。具体原件见[方法与结果](../../../.agent-state/agent-project-call-app/README.md#recorded-basic-result)；其中 tool-observation 仅为 assistant 对原工具完成结果的转录，不是 provider-native 导出。

真实 profile 原 `1191648` actual `0`，1 正 / 7 反。compile Go `1191716` 为 `0`／`34.602s`，exact list `1192260` 为 `0`／`1.053s`，prepare `1192329` 为 `0`／`1.021s`；原 sequence 工具 `94694`／final `baf93c` actual `0`。2052 项 compile 前后/list 相同，含实际同包 96 Go 源及全部迁移至 74；原 38 项 sequence 输入相同。runtime 增加原实际 binary 后为 2053 项。未重跑已通过 basic，也不将 prepare 计为 native。

首次 native 原工具 `69540`／final `731bb9` actual `0`；supervisor `1192901` Wait `0`／`117.740s`，driver `1192921`、Go `1194521` 均实际 `0`。`TestAgentProjectCallApp` 及两个子例全部 PASS，`24.32s`（`12.99s`／`11.33s`）。原实际 SQL/HTTP 证明同一 pair 的 service mutation 1 次、service read 3 次、directory read 2 次，共六个 distinct joined CallID，拒绝请求零登记；第二子例证明 HTTP 200 后原目录 checkpoint 尚未退休时 guard 保持，原同 CallID Drain 取得事务终局后才释放。七资源 14 次精确 absence、desc/private/runtime 双尾、TCP 两次 delta-empty、2053 项实际输入及四 recorder 身份全部相同，无 race warning、源码漂移或重试。原 post `abf250` actual `0`，完整小证据见[方法结果](../../../.agent-state/agent-project-call-app/README.md#recorded-compile-and-native-result)。tool-observation 仍仅 assistant 亲收转录；原 outer 文档输入图是历史事实，不因本次最终文档修改而声称当前文档 SHA 相同。

本片真实 PG 只覆盖默认 App 双 lane 与原 directory-return/checkpoint/guard 退出顺序；不外推 StopDriver、foreign death、真实 COMMIT-Unknown 或 Force PG。Unknown/Force 仅按基础控受控范围记证，原上游失败不改。上述 PG 失败保留规则及原 2s HTTP、finish、graceful/整轮预算均未放宽。

[project_update.go](../../../internal/central/app/project_update.go) 的 Initializer/LifecycleRegistry 继续未绑定；完整 Skills/Variables 排空、Execution/Agent/Skill 引用退休、完整 enabled manifest 与有序 cleanup 仍是后继。Object runtime join、Knowledge、真实 tools HTTP 等既有 STOP 不变；本片不改变其实现或用替身绕过，不宣称完整 D08、生产 Project 创建或全平台生命周期完成。

## 交付与原件恢复

最终文档 `903b87a7` 与受测 `7e202fcb` 已由正式 main 完整历史保留。原保全 `91842/b55591` 实际0：88文件、99,022,474B、11目录同FS NOREPLACE逐SHA/stat/inode相同，24图的2,055历史路径版本可恢复，4份tmp副本7,397B齐且原件保留。Agent缓存证据所引1,132源码版本和35输出records已逐项闭合到main及迁后路径；独核 `5e5b23` 接受后才执行普通清理 `30264/96f71f` 实际0。远端原空、显式main/ai prune及tracking空不虚计远端删除。详见[恢复说明](../../../.agent-state/agent-project-call-app/README.md#main-delivery-and-retained-evidence)。原上游FAIL/HOLD、外部工具与可见性限制及本片有限范围均保留。
