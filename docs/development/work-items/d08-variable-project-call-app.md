# Variables 普通/Secret 持久调用归属接入默认 App

状态：App 本片三处生产接线、必要纯控与真实 PG 验收已完成；basic01、controls/compile/list/prepare 和首次 native01 均实际通过。受测源码 `156da848e9f9b91bba1ad2fc88a9085d0f7d2da2`，最终文档 `0e6caf04` 已正常完整合入并推送 main `987ca6c4`，技术字节与受测源码一致；原证保全独核后，旧工作树 `/workspace/agenteam-variable-project-call-app` 及本地分支 `ai/variable-project-call-app-20261012` 已普通清理，远端原为空。建树及方法基线为已交付 main `9293ace4604b11529293ac6b26cb4c266ea65b2f`，迁移最大号保持 75；本片不新增迁移。

上游[持久调用归属卡](d08-variable-project-call-ownership.md)及[已验方法结果](../../../.agent-state/variable-project-call-ownership/README.md)记录受测技术源码 `a64ed9e2983d162457190e40d18e96f5280415f2`：basic02 原 `69647/e79f0c` actual 0，12 top / 38 sub、两个包 race 与两个 vet；native01 原 `66862/c2458c` actual 0，1 top / 3 sub，真实资源及输入 post 已接受。上游 basic01 原 FAIL 保留，实际通过只覆盖 registered Project business calls；fixture STOPPING 不冒完整 StopDriver/participant，Project EX 不证明 D04 全局技术 SQL 终局，更不代本片 App PG。此处引用本树受版本控制的来源，不依赖将清理旧树的绝对 source/bin/output 路径。

目标是在真实默认 App 中让普通 Variable 与 Secret Variable HTTP 共用一个 `projectvariable.ProjectCallOwnership`，完整保留已准入调用及退出结算。遵循[项目变量规范](../../../docs/architecture/project-work-management/project-environment-variables.md)、[D08/D10 开发门槛](../../../docs/development/development-plan.md)与[团队流程](../../../docs/development/agent-team/README.md)。不新增迁移、Initialize、环境开关、公开注入口、完整 LifecycleRegistry/Initializer、引用退休或全局停止报告；默认 Project 创建的既有缺口保持。

## 固定依赖与构造

[account.go](../../../internal/central/app/account.go) 已按以下次序构造：原 Account 与 Project/Variable Authority → 原 concrete Object spool/guard/process → 原 Audit → 唯一 `SecretWriteAuthority` → 引用该 authority 的 D04 → 原事件目录/Outbox/Account core → 普通与 Secret Variables。新的 private owned factory 在最后这个既有位置替换两次独立 `New`/`NewSecret`，无需延后到 Skill Service，也不改其他提供方的构造顺序。

factory 接收原 cfg/db、Variable Authority、Project Authority、Account/Audit/Outbox、两个已注册的事件描述、原 D04、原 `secretWrites` 以及 concrete `objects.guard/process`，一次调用现有 `NewProjectCallOwnership`。同一 `secretWrites` 同时归 D04 原授权图和 pair 的 SecretDependencies.Writes；禁止另造同 Store 替身、setter、可变 provider locator 或失败后退回无 ledger 服务。两个 lane 使用同一真实 Store/Project/Variable Authority；guard/依赖缺失则明确失败。

pair 构造无 I/O/worker，允许 guard 此时尚未 bind。这不是运行就绪证明：继续原 Account/Audit → D04 → Object.Runtime.Initialize → Account.Start/既有 Agent Registry 初始化 → HTTP 开放顺序；首次登记由原 guard 在登记 live Tx 内验证实际 Store/process，实际 Committed 后才业务。本片不新增 Variables Initialize，也不绕过 Runtime 恢复或把零 guard 伪装为 bound。

## 一个退出 owner 与两个兼容视图

默认 App 新增一个 private pair owner，直接持有 concrete pair，并保留 `accounts.variables` 的 `*projectVariablesAssembly` 与 `accounts.secretVariables` 的 `*projectSecretVariablesAssembly` 两个固定视图。两视图的 service 分别取自同 pair；原 HTTP handler/path/Session/CSRF/Owner 和响应契约不改。一次 `accounts.install` 原子发布 owner 与两视图，下一次可失败装配之前即取得完整退出归属；不先装一个 lane 再装另一个。

私有实现接口固定为 `createOwnedProjectVariables(..., ordinaryEvents, secretEvents, objects) (*projectVariableCallAssembly, error)`，前述原依赖全部显式传入。`projectVariableCallAssembly` 持 `pair`、`variables`、`secretVariables` 和 stopped 状态；两 view 新增私有 `owner` 指针，`accountAssembly` 新增 `variableCalls` owner 字段。owner 的 Joined 要求已 Stop 且 pair 实际 Joined；factory 不执行 Initialize 或登记 SQL。

这不是两次生命周期登记：owned 分支的 `works()` 只排列 pair owner 一次，跳过两 view；没有 owned pair 时仍走旧两 assembly 的原 legacy 分支。owned views 的 Stop/Drain/Force/Joined 委托同一 owner，不能依赖从未被 `works()` 更新的旧 `stopped/joined` 缓存；owner 直接调用 pair，不能反调 view 形成递归。旧私有 `createProjectVariables`/`createProjectSecretVariables` 与公开域 `New`/`NewSecret` 保持无 ledger 的独立语义，不删除旧字段或改变类型断言。

具体兼容消费者包括 [普通 process 控](../../../internal/central/app/project_variables_process_test.go)、[Secret process 控](../../../internal/central/app/project_secret_variables_process_test.go)、[普通原确认控](../../../internal/central/app/project_variables_independent_confirmation_test.go)、[Secret 写 Web 控](../../../internal/central/app/project_secret_write_web_test.go)和[Secret 读 Web 控](../../../internal/central/app/project_secrets_web_test.go)。它们取得两个 concrete assembly，并观察原 service/Joined；保留视图不能仅做到类型相同而遗失实际归属。

## 退出与资源边界

- pair owner 的 StopAdmission 调用 pair.Stop，关两 lane 新准入并取消原调用；Drain/Force 使用调用者原 ctx 同步调用 pair.Drain，并要求 pair.Joined，不能把超时、取消或一次返回错误当完成。登记/退休 Unknown 保留原 CallID，停后仍只用核心片原 returned 结算通道，不新注册、不启后台替身。
- 维持 Execution/Runner 在前、Variables owner 在后续 Work/Agent/Project/Skill/Account provider 前的顺序。`accountAssembly.Force` 增加此 owner 未 Joined 时阻止下游 provider 退休的门，与既有 Execution/Agent/Install 门并存；`accountAssembly.Joined` 必须包含联合 owner。晚到 install 即使 ctx 已取消或 Force 已开始，也先保有 owner/两视图，再沿原 stopped/forced 状态与原剩余 ctx 排空，不重置预算。
- Secret Prepare、prepared/intent Destroy、原 confirmation、Store return 和 checkpoint 的实际尾部由核心 pair 持有；App 不另外拆分 D04 子调用。D04 是共享提供方，其 `StopMaintenance` 只停止下一维护批次，当前维护批次仍由原 maintenance owner 等待；它不是关闭所有 D04 资源的证明。pair.Joined 不包括其他 Secret/Usage/lease 或独立维护调用。
- 正常 shutdown 沿原 HTTP/Account/Outbox 实际 Join → Object Runtime/guard → Store 的条件推进。[resources.go](../../../internal/central/app/resources.go) 不扩域：根级 Force 仍按原预算调用 Store.ForceClose，可能终止 DB socket；联合 owner 门不保证 Force 后 Store 永久可用。未 Joined 的原调用不能因此被抹去，原 joint producer check 继续保留 guard。真实屏障必须在原 graceful 预算内释放才能作为正常成功例。
- App 当前 ProjectAuthority 没有完整 Lifecycle 绑定；正常 Owner CRUD 的登记/结算可用，不能据此开放 ObserveStop HTTP、注册 fullparticipant 或宣称 `RegisteredProjectBusinessJoined` 覆盖 legacy、跨版本集群、foreign D04 技术 SQL、全部 Variables 或引用退休。

## 唯一生产写域

| 文件 | 必要差额 |
| --- | --- |
| [app/project_variables.go](../../../internal/central/app/project_variables.go) | private owned factory/pair owner、普通 view 固定接线及 owned 生命周期委托；保原 legacy 构造 |
| [app/project_secret_variables.go](../../../internal/central/app/project_secret_variables.go) | 同 pair 的 Secret view/原 handler，owned 生命周期委托及原 legacy 构造兼容；不重造 D04/writes |
| [app/account.go](../../../internal/central/app/account.go) | 原子 install 三个固定引用、默认两个 handler、唯一 works owner、联合 Force/Joined 与晚到构造归属 |

不改 Object/Secret/Project 产品、域 pair 协议、HTTP 契约、迁移 75 或 root resources 关闭政策。方法沿现有 basic/PG harness，输入闭包与原 Wait/资源检查照实绑定最终实现，不能在草案中预填动态结果。

## 最小验收选集

独立纯控唯一新增 `internal/central/app/variable_project_call_ownership_test.go`，覆盖三个有行为区别的问题：

1. 真实 factory 固定同 pair/原完整依赖，构造无 SQL，缺依赖或零 guard 不授权业务；旧两构造/view/路由仍可独立使用。兼容回归限现有 `TestProjectVariablesPureConstructionAndActualReadLookupJoin` 与 `TestProjectSecretVariablesPureConstructionAndActualJoin`，不扩整个 App 矩阵。
2. 普通与 Secret 各一个真实入口在原 registration Store-return 屏障期间，由 App owner 同时保有调用；受控 Unknown 保留原身份且零业务/Secret Prepare，停止后原结算未返回时 Joined 为 false，Force 不退休下游。受控 Store 结果不冒真实 COMMIT loss，concrete 零 guard 只用于拒绝，不伪造 bound 状态或新增生产 fake seam。
3. 两 view 的实际 Joined 与唯一 owner 一致；停止/Force 后原子晚到 install 不遗失任一 lane，消费原 ctx/预算且不重复排入 works。纯控检查原调用实际返回，不仅检查内部字段或调用次数镜像。基础方法只运行新增控、上述两个兼容 top 与 App vet，最终计数从实际源码提取。

真实验收唯一新增 `tests/projectvariable/variable_project_call_app_test.go`，实际 `TestVariableProjectCallApp` 固定 **1 top / 2 sub**：

1. `default-http-shared-variable-owner`：复用[真实 App handoff](../../../tests/projectvariable/agent_project_call_app_test.go)的正式非空 Project/Skill 图，setup 原 owner/guard 全退后启动公开 `app.Run`，使用原验证 TLS、真实 Session/CSRF/Owner。选择普通与 Secret 各 Create、Get、Lookup，合计 **六个公开入口**；检查两 mutation/四 read 为六个 distinct closed CallID，same process/instance、canonical process claim，以及真实命令回执、Secret D04 receipt和安全读投影。跨 Owner 请求拒绝且零登记，不输出 Secret plaintext。六个入口用于证明默认双 lane 接线、写入与既有回执读取；本片不宣称已重新执行全部十二入口，Update/Delete/List 及领域完整入口/fence/foreign/nonce矩阵复用已接受核心片与原 HTTP 契约测试。
2. `secret-checkpoint-drains-before-guard`：使用默认 App 的真实 Secret Get。在 task-owned Project SH blocker 下，原登记/read 可完成而退休 Project EX 等待；按确切原 backend/transaction/lock 身份取消一次原 checkpoint statement，使其真实 NotCommitted 返回，核已知 HTTP 200 不被结算失败改写、同 CallID 仍 running。随后真实 SIGTERM，确认 App 原 Drain 正在结算同 CallID、原 guard flock 仍占用且 App 尚未返回；在原 graceful 预算内释放 blocker，亲收 HTTP、SQL、pair、App 原返回及 guard/process claim 退休。此例不是 COMMIT Unknown/foreign death/Force PG，不增加新 wire gate、fake report 或重试框架。

第二子例只选 Secret lane，覆盖默认组合中容易遗漏的一侧；普通 lane 的同样核心结算语义由上游核心片和本片联合 owner 纯控覆盖，不为凑十二或双倍尾部矩阵增加同义场景。Secret nonce 技术 SQL 的实际返回/安全清理属于上游核心片，不以这个 HTTP read 例重作证明。

失败时先记安全错误，原 App/guard/HTTP/SQL 未实际退出前保持 fixture 和真实 owner；仅等待原返回或既有整轮硬截止终止，不把有界等待失败当 Joined、不刷新预算、不继续下一个 sub。成功必须得到原 `app.Run` 返回、同 CallID business_closed、guard 实际释放和原资源尾部检查；不以日志词句或 HTTP server 关闭代替。

## 开工与交付边界

core 动态前置及共同基线已确认。正式卡单独保存后启动上述限定实现，产品、纯控、PG、方法各文件只有一个 writer；产品及测试分别由未参与该实现的作者独审。Go、服务和 Git 仍由 root 按实际窗口安排，源码作者不自行启动。core 通过不代本片 App 结果，原 FAIL/HOLD/未运行事实继续保留；不得用 always-reject 适配器、partial Registry 或人工清引用冒完成。


## 实际验收

完整索引见[方法 README](../../../.agent-state/variable-project-call-app/README.md#actual-validation)。原工具返回保存在 assistant-observed 转录中，不冒 provider 原生导出；所有输入一致结论属于原运行观察点，最终文档改动不冒仍有原 outer 文档 SHA。

- **basic01：** 原 `51459/1c50c2` actual 0，method PID 1273383 Wait 0 / 287.275 s，race PID 1273494 Wait 0 / 156.829 s，vet PID 1275599 Wait 0 / 128.105 s；四个新 top / 七 sub 加两个旧 legacy top / 零 sub，共 **6 top / 7 sub、App 一包 vet**，无 fail/skip。[原 post](../../../output/ai/retired-20261012/variable-project-call-app/basic01-outer/post-check.json) 核六份 1138 输入图及 21 outer 同。受控 Store Unknown 与 concrete unbound guard 的拒绝控制不代真实 PG COMMIT loss 或成功 bound 证明。
- **controls / compile / prepare：** 原 `37348/0da0ce` actual 0，outer PID 1278026 / 46.737 s；controls PID 1278073 为 1 正 / 7 反控，实际 Go PID 1278122、exact-list PID 1278696、prepare PID 1278765 均 Wait 0。[sequence post](../../../output/ai/retired-20261012/variable-project-call-app/sequence01/post-check.json) 核 2069 compile 三图一致、加唯一 binary 后 2070 runtime、43 outer 同，含全部既有迁移与 101 同包 Go 源。[原 binary](../../../output/ai/retired-20261012/variable-project-call-app/compile01/binary.json) 为 92,656,152 B，SHA `14320ece7545a1d7edccf8135e59d97419abc2056a51eb3f55b12b5f2579ccc5`；prepare 仍只记原准备，不覆盖为执行通过。
- **首次 native01 whole PASS：** 原 `53840/56d7d0` actual 0，supervisor PID 1279902 Wait 0 / 112.131 s，driver PID 1279924、Go PID 1281535 Wait 0；1 top / 2 sub 为 21.56 s（10.62 / 10.93 s）。[原 post](../../../output/ai/retired-20261012/variable-project-call-app/native01/post-check.json) 核全部 2070 实物 SHA、四身份和原受测 HEAD 同，7 ID 两轮 14 次 absent、desc/private/runtime 双空、TCP 双 delta_empty、无 race。原 raw 为 `/tmp/vpca01/pg-cfdc7919d62642d0af96113381f803d2.log`，无重跑。本片没有失败尝试；上游 core basic01 原失败继续由原记录保留，不混为 App FAIL。

实际 marker 覆盖默认 `app.Run` 六 HTTP 入口、same process/instance 的六独立闭合 CallID、真实 D04 receipt及越权零登记；Secret 原已知 HTTP 返回后，精确取消的 checkpoint 实际 NotCommitted，不改写业务结果，SIGTERM 沿同 CallID 原 Drain 实际结算后才放 guard。真实 bound guard 证明来自本 PG。setup 使用 Project service 与持久测试 Skills receipt，不冒 Project 创建 HTTP 或 production Skills 验收；此片也不证明 full Lifecycle、foreign death、Force PG 或全局 D04 技术 SQL Join。根级 Store.ForceClose 的原预算/断 socket 政策及未 Joined 时联合 guard 门保持，不能宣称 Force 后 Store 永久可用。最终 Git 交付、保全和普通清理待各自实际回执，不提前计入。


原证现位于 main 的 `output/ai/retired-20261012/variable-project-call-app`；完整历史、单 binary、24 输入图及 tmp 原件/副本恢复边界见[交付与保全说明](../../../.agent-state/variable-project-call-app/README.md#main-delivery-and-retirement)。保全原84801/862283及普通清理原94310/c9cf15均 actual0，既有有限验收与上游FAIL/STOP不因清理扩大。
