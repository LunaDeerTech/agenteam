# D09 恢复：System Model 生产根装配

修订 1，2026-10-05（仅采纳状态收口，技术规则不变）。状态：**规格独立静审通过并获采纳，业务实施待本卡提交后另授。** 固定源码基线 `ac5b4c65e88ed0ec1813fbf341035c88ca9a38c7`，其 17 路径 System Model HTTP 已独立 PASS 并提交推送；本卡不将源码提交当作根装配已经实现。设计者为 `d08_recovery_design`，当前唯一仓库写权为本卡；实施者为 `recovery_handoff`，独立验收者为 `restore_test_dependencies`，实施授权及 fixture 窗口由主线程另行交接。

独立静审报告：`/tmp/agenteam-system-model-root-static-ig_oqjb5/review.md`，SHA-256 `6c146abeddb264fcc4b93146187f89539f3e3292986add56a190d1188b0f5a6b`；被审 rev1 卡 SHA-256 为 `c0a42b34b39cc6640b08f4d36c3cde28253b181f4aea4b0ac72909dccf544b46`。主线程已采纳 PASS，无业务范围修订；此处仅记录定位，不复制原报告，也不代表产品动态验收。

本卡引用 [D09 主卡](d09-model-system-token-usage.md)、[B01-K System 配置](d09-b01-system-configuration.md)、[System HTTP 规格](recovery-d09-system-model-http.md)及其 [OpenAPI](../../../api/openapi/model-system.json)，不重定义 DTO、授权、幂等或产品规则。必读 [AGENTS](../../../AGENTS.md)、[团队流程](../agent-team/README.md)、[开发计划](../development-plan.md)；设计、实施、独立验收分别使用 [design](../../../.agents/skills/agenteam-design/SKILL.md)、[Go](../../../.agents/skills/agenteam-go-development/SKILL.md)、[verification](../../../.agents/skills/agenteam-verification/SKILL.md) 技能。实施前核同一固定输入及本卡最终修订，不读其他活动候选作为依赖。

## 1. 完整结果与真实前置

使正式 `cmd/agenteam` 在默认真实装配下对外提供 **System Provider/Model CRUD、平台 selector、Model credential 独立写入及两类原命令查证**。使用现有部署环境、数据库迁移、Account cookie/CSRF/当前管理员、同一 Secret/Audit/Outbox；实际二进制启动、请求、重启及停止均须验收。没有新的部署环境变量、CLI、迁移、HTTP 合同或业务服务实现。

| 已验能力 | 本卡实际消费与边界 |
| --- | --- |
| D07 正式 Account/System 根、D03 数据库和原 HTTP 生命周期 | 当前身份、Session/admin、受限日志、SMTP/Account owners、原初始化及 DB 最后关闭协议；默认根仍要求真实 PG/MinIO 和原 keyring/日志配置 |
| B01-K `543511c`、Model typed Audit `26622bc`，均已包含于基线 | Model Authority/Service、00015、技术 singleton、原原子命令/引用/事件；不补默认模型或 Project Summary |
| Secret Model usage `8ad6759` 及[已验记录](../agent-team/d09-secret-model-usage-verification.md) | 保持原 Secret 正式口；本根的 Model usage router 仅绑定真实 Provider reference，Model call/lease 的消费者仍未绑定 |
| System HTTP `ac5b4c6` | 已验 Model handler、Account 安全边界、Secret 被动 receipt 查询；原权限与 Unknown/replay 语义全部保持 |

Project/Runner、Model Resolver/Invocation/Usage、Provider 实际调用、MCP、未来 Model event consumer 不属于本结果；OpenAI wire 库不在本根创建调用能力。Agent/project_summary 删除替换仍按已有真实 adapter 缺失拒绝整 Tx；Summary 创建初值/Settings 待用户决定，不新增输入、默认值或 NULL 完成声明。

[Object 普通锁回归](../agent-team/object-runtime-join-regression.md)仍是未修的已知退出缺陷；[Object 修复任务](recovery-object-runtime-join.md)因自动安全筛查中断保持停止，不重试、改派或恢复暂停探针。Artifact 最终共享 guard、Project 领域绑定及 R4 的阻塞不因本卡解除。**本卡只证明新增同步 System HTTP 接入原根协议，不证明共享 ProcessGuard 缺陷已修或完整 D09 已完成。** `/readyz` 继续 503，整体 `ready=false`。

## 2. 默认构造图与精确旧接缝

入口继续是 [app/account.go](../../../internal/central/app/account.go) 的 `bindAccounts`，现有 `accountAssembly` 继续拥有部分构造、Account Core/Runtime/Mail/Sink 和安装的 handler。不增加并列可选装配入口、全局 registry、可变 provider locator/setter、第二 Secret/Outbox 或 Model runtime。新增 Model 构造器自身零 SQL、零 goroutine；原根打开日志/对象等既有资源的行为不改成“全根构造无 I/O”。

### 2.1 同一 Store 与正式依赖

按下列有向依赖完成构造，任一步错误原样安全返回，由既有 owner 清理已取得资源：

1. 原 `account.NewAuthority(store, cfg.AccountKeyring())` 成功后，从同一 `db` 取得 `model.Store`；调用 `model.NewAuthority(modelStore, model.Authorizations{Sessions: accountAuthority, System: accountAuthority})`。`Projects` 明确未绑定。Model Service 和其 Authority 必须使用**同一个 Store 实例**，不能以两个包装器指向同一个池代替身份相等。失败不得继续打开新的业务能力或发布 handler。
2. 用同一 `modelAuthority` 创建唯一 `model.NewSecretUsageRouter(modelAuthority, accountAuthority)`。它只把 Model Provider retain/release reference 的实际授权/规划交给 Model；其它既有用途和无 Purpose 的 lease 接缝沿原 Account fallback，缺消费者的 Model lease/read 继续明确拒绝。不得增加 allow、猜 owner 或访问 Project 私表。
3. 现有私有 `createSecurity` 与 `createSecret` 仅扩大显式参数和绑定，签名如下；现有 Store/Account 校验优先级保持，新必需参数缺失则拒绝启动，不产生降级实例：

~~~go
func createSecurity(cfg config.Config, db database,
    accounts *account.Authority, models *model.Authority) (*audit.Service, error)
func createSecret(cfg config.Config, db database, auditing *audit.Service,
    accounts *account.Authority, usage *model.SecretUsageRouter) (*secret.Service, error)
~~~

`Audit.Authorizations.Models` 指向真实 `modelAuthority`，原 Accounts/Sessions/System 仍为同一 `accountAuthority`。`Secret.Authorizations.Usage` 指向上述真实 router；AccountWrites/Sessions/System 保持原 Account，Projects 保持未绑定。构造必须先得到真实 providers，不能让授权 callback 在执行时查找后来才填入的 Service。新必需具体指针为 nil 时安全拒绝；正式构造器产生的错误/零能力不能被 root 忽略。

4. 使用原单一 `event.Catalog`：原 Account sessions-revoked、delivery-requested 定义保持；**在 `outbox.New` Seal 之前**调用一次 `model.DefineEvents(catalog)`。同一 `outbox.Authorizations.Producers` 同时注册 `account/contract.AccountProducer: accountAuthority` 与 `model.ModelProducer: modelAuthority`；Sessions/System/Processes/Audit/Cursors 仍为原实例/实际 process。两种 Model 事件为 `model.configuration_changed`、`model.embedding_selection_changed`，不另造 JSON producer。
5. 用同一 `modelStore`、`modelAuthority` 和 `model.Dependencies{Secret: secrets, Audit: auditor, Events: journal, ConfigurationEvents: modelEvents, Cursors: cfg.CursorKeyring()}` 调用 `model.New`。必须是该 catalog 返回的 opaque `ModelEvents` 和该 catalog 创建的 journal；不能复制载体或另建 catalog。
6. 原 Account Core、Avatar/Object、Account Runtime、Mail worker/runtime、唯一 Outbox Runtime 继续按原顺序构造和登记所有者。仅在 Account Core 已构造后调用 `model.NewSystemHTTPHandler(models, core, secrets, model.SystemHTTPOptions{PublicOrigin: cfg.PublicOrigin()})`，并与原 `account.NewHTTPHandler` 的结果组合。`secrets` 同时是 HTTP 的真实 `HumanWriteCommands` 和 Model Service 的真实 `UsageOperations`；不创建第二份写服务。

`createSecurity/createSecret` 的固定生产调用点只有 `bindAccounts`，不存在要同步的跨域公共 API。私有路由辅助放新 `app/model.go`，其余私有 helper 可在三个生产文件内选择；不因测试增加公共工厂、运行开关或新 provider 接口。

### 2.2 单一事件日志与未绑定消费者

Model 原命令继续在同一 Tx 写 canonical/receipt/Secret reference/typed Audit/事件，root 不查询或改写这些业务表。Credential 写与 Provider 绑定仍是两个独立 HTTP 命令，不宣称跨请求原子，也不自动补偿删除 Secret。

唯一原 Outbox handler 仍为 `account.mail-enqueue`；不增加空 Model handler/订阅。没有 Model 订阅时，真实 Model 事件必须成功持久 append、产生零 Model deliveries；不能以“无消费者”为由丢事件，也不能伪造已处理状态。已存在的 Account handler/subscription 身份与声明不变。未来 D13/D14 等消费者仍自行实现正式订阅及 canonical bootstrap，本卡不将历史广播当作完整历史重放承诺。

## 3. 初始化、失败及重启

默认 `bindAccounts` 已返回捕获真实实例的初始化回调。本卡只扩原 `deps.secret` 回调：先调用原 `secrets.Initialize(ctx)`；若已有 package-local `deps.secretInitialize` 测试 hook，则仍由该 hook 执行原 Secret 阶段。其失败立即返回，取消立即传播；**只有 Secret 阶段成功且原 ctx 仍有效，才对已构造的同一 `models` 调用 `Initialize(ctx)`**。返回仍是同一 `secrets`，不二次构造、异步执行、轮询或延迟到首次 HTTP。

新 Model 初始化消费已有 `SecurityStartupTimeout=30s` 的**同一个** ctx/deadline，在 Secret maintenance 启动、后续 outbound/Object/Outbox/Account 启动及 HTTP bind 之前完成。每次正常根启动只调用一次；不为 Model 续期或新增 budget。现有安全日志阶段足够：新阶段失败沿原 `SecurityFailed/InitializationFailed` 安全投影，不能输出配置、材料、SQL 或原错误详情。

`model.Initialize` 的既定职责是检查真实配置存储，并幂等持久化 version=1/configured=false 的技术 singleton。该空态不是默认模型；不产生默认 Provider、Model、Summary、Secret 或业务 Audit/Event。重启保留同一 singleton ID/version 和已配置引用/命令历史，不重置值、不重放写命令。返回错误或 Unknown 都不能放行监听；实际持久技术事实可由下次原 Initialize 幂等读取，不由 root 猜测提交终局。

Model Service 没有后台或独立关闭资源。原 accountAssembly 的 `constructing/install/constructionDone`、resources 对 Sink/Store/Object/Outbound/Outbox 的单 owner 与晚返回处理完整保留。Model 初始化失败时尚未启动 Secret maintenance；此前已经取得的资源仍通过既有启动失败路径正常清理，不能把新初始化错误吞掉以兼容旧测试。不得要求尚未登记的 Object process claim 已有 stopped 行；断言应按真实取得/登记阶段观察。

## 4. 路由及原安全边界

将组合后的 handler 安装到原 `accounts.handler`。根 [app.go](../../../internal/central/app/app.go) 继续分流 `/livez`、`/readyz`、`/diagnostics`，其余通过该 handler；不需要修改 app.go、resources.go 或 diagnostics。

Model 路由选择只覆盖以下六个**完整路径根及紧随 `/` 的子路径**，其它全部交给原 Account handler：

| 精确路径根 |
| --- |
| `/api/v1/system/model-providers` |
| `/api/v1/system/models` |
| `/api/v1/system/model-selection` |
| `/api/v1/system/model-commands` |
| `/api/v1/system/model-credentials` |
| `/api/v1/system/model-credential-commands` |

不使用泛化的 `model*` 匹配，不在组合层 `path.Clean`、改写/重定向、读取 body、改变 method/Host/Origin/RawPath 或代办身份。带危险路径的原请求仍交对应已验 boundary 拒绝；Model 的 17 method/path、HEAD/405、CSRF、严格 DTO、错误及两类 receipt 语义完全沿 HTTP 卡/OpenAPI。未知 prefix、Account 路由及诊断不得被 Model 截获。

组合层只分派一次，不做额外授权或缓存 Actor。已知 Model 路由仍由真实 Account boundary 获取当前 cookie Session/admin，业务 Tx 再核当前授权。POST lookup 虽是 Read 意图仍需原 unsafe-method CSRF；未见 receipt 不证明 NotCommitted/原 writer 终局。Model 底层 Unknown 若原确认流程恢复成功仍返回 200+receipt；只有服务最终 Unknown 才 503，Secret 原 Unknown 直接 503。root 不改写这些结果。

公共 `httpapi.Handler` 仍只由原 app 安装一次，保留一次 request ID/recover/安全 HTTP 日志。普通日志不得输出 credential、密码、cookie、CSRF、命令 key 或 raw body/query；受限 Account recovery log 不进报告或普通日志。

## 5. 同步请求、停止与就绪

新增路径没有 Model Invocation、credential material lease、Provider I/O、消费者 worker 或 detached goroutine；全部业务调用使用原 request context 并在 handler 返回前完成本方同步处理。无需新增 Model StopAdmission/Drain/Joined、独立 ProcessGuard 或后台所有者。

原 `app.go` 的 gate 对新增路径执行同一次 `admitHTTP` 和 defer `finishHTTP`；`resources.producersJoined` 仍要求实际 `httpActive==0`、原 Account/Outbox Joined。首个停止信号不提前取消在途 handler；原 HTTP Shutdown、Account/Mail、Outbox、outbound/maintenance 的排空顺序及 DB 最后 StopAdmission/Drain 保持。停止后仍进入 gate 的请求为 503 `SHUTTING_DOWN`；监听/连接已关闭时可以连接失败/EOF，不承诺所有迟到请求都有 Problem body。

原超时/第二信号路径仍取消 serving 并关闭 HTTP，沿原共享 force context 依次发起已有 owners 和最后 DB ForceClose；不续期、不改退出码。force 预算耗尽时不能宣称所有请求/原 writer 已 join 或所有副作用回滚。新增测试必须区分正常 drain 的实际完成、force 的有界退出和最终进程退出，不把 HTTP 返回或 context cancel 当作通用数据库/ProcessGuard 终局证明。

`/readyz` 健康时仍为 503 `DEPENDENCY_UNBOUND`，原组件故障时为 503 `DEPENDENCY_UNAVAILABLE`；diagnostics 的 ready=false、Project/Runner 等 unbound 及现有健康采样不变。不得为了新路由增加虚假 Model runtime 健康项。已知 Object guard 回归原样保留；此处不是其修复、复验或绕过路径。

## 6. 精确实施范围与旧测试核查

规格已获采纳；本卡提交后由主线程另授下列 **8 路径：3 生产、3 新测试、2 验后能力文档**。当前本卡不授业务写权。

| 路径 | 唯一变化范围 |
| --- | --- |
| `internal/central/app/account.go`（旧） | bindAccounts 的真实 Model 构造、同 catalog producer、两 handler 组合及原 secret 回调顺序增量；不改变 accountAssembly 生命周期协议 |
| `internal/central/app/security.go`（旧） | §2 的两个私有构造签名与 Models/Usage 真实注入；原 Account 绑定保持 |
| `internal/central/app/model.go`（新） | 无状态的精确路由辅助及上述范围内必要私有装配 helper；不新增 runtime/注册框架 |
| `internal/central/app/model_test.go`（新） | 纯路由/请求保持、构造拒绝及一次 middleware 边界 |
| `internal/central/app/model_process_test.go`（新） | integration 下真实默认 bind、初始化/所有权、同步 HTTP active/admission、原 drain/force/DB-last 观察 |
| `tests/process/model_system_database_test.go`（新） | 正式 Central 二进制、真实 Account 登录/PG/MinIO、System HTTP 组合、重启及失败/退出 |
| `AGENTS.md`（旧） | 验收后窄同步已对外的 System 配置能力及未绑定/ready503/Object 已知缺陷边界，不改团队规则 |
| `docs/development/backend/README.md`（旧） | 验收后同步启动顺序、已验 System 路由/OpenAPI 与能力限制；不改配置/CLI/原历史验收 |

固定源码已核：`app/database_test.go:unitDependencies` 为旧纯编排显式替换 bind，不执行真实根，保持其原测试目的；`app/database_process_test.go` 只有 `secret_startup_second_signal` 设置 secretInitialize，执行真实 Secret Initialize 后延迟失败返回，必须保留原返回/取消优先；`app/account_process_test.go:guardAccountAssembly` 和默认 process fixtures 使用实际完整迁移，再调用同一 bind/secret 回调，能够包含新 Model 初始化；`tests/process/outbox_test.go:TestCentralOutboxEmptyMechanismHealthRecoveryAndSignal` 核的是唯一 mail handler/subscription，而不是只能定义两种 event。`createSecurity/createSecret` 没有其它固定调用点。

因此当前**没有已证实必须修改的旧测试路径**，不授旧断言写权。实施若实际出现初始化次序/fixture Store 能力造成的兼容红，保留原失败并报精确文件、函数与最小 delta；不得关闭 Model 初始化、伪造 root provider、删断言或偷扩路径来维持 8 路径。业务 packages/contract、HTTP/OpenAPI、D03、Object/Artifact、app.go/resources.go/diagnostics、SQL/迁移、脚本/依赖、CLI/config、状态页及已冻结报告均只读。

## 7. 可独立验证的验收结果

新增组使用默认真实 bindAccounts；不能以 `unitDependencies`、假 Session/admin/Audit/Event/Secret、替换生产 handler 或直接写业务表代替根绑定。纯路由测试可用记录型 child handler；integration 可使用只委托真实 Store 的有界观察器和既有 package-local deps，不能改变底层结果/权限。二进制组必须运行实际 cmd，无测试 route/装配 override。

| 新测试 | 必须观察到的完整结果 |
| --- | --- |
| `TestModelRootRouteOwnershipAndRequestPreservation`（pure） | 六路径根/子路径精确归属、相似 prefix 不误匹配；原 method/URL/RawPath/Host/Origin/body 不变，一次分派/一次外层日志；不 canonical redirect |
| `TestModelRootRequiredConstructionDependencies`（pure） | 缺 Store/Account/Model/router 依赖安全失败，无 SQL/后台或可用 handler；有效图用真实构造器，不以纯 fixture 声称已验真实授权 |
| `TestModelRootInitializationAndFailureOwnership`（app real） | 真实 Secret 成功后才 Model Initialize，原 ctx/deadline 不变，监听前完成；原 Secret 失败/取消不得进入 Model；真实 Model 存储失败/普通锁等待取消时不监听、部分原 owners 实际清理；无第二初始化/服务 |
| `TestModelRootHTTPActiveDrainAndDatabaseLast`（app real） | 真实新增 System 配置请求已过正式身份边界并进入业务调用；普通有界锁/语义 barrier 确认在途与 httpActive。首信号后不再 admit，新请求安全拒绝/连接关闭；释放等待后请求真实完成、receipt/canonical 结果相符，才沿原顺序关闭 DB |
| `TestModelRootHTTPForceUsesOriginalBudget`（app real） | 同样真实新增路由在途，原 deadline/第二信号两分支取消且有界退出；观察原 serving cancel/HTTP 关闭/DB ForceClose 发起顺序，预算不续期，保留底层实际 commit/error；不把 forced 当 join/rollback，测试结束实际命令/资源归零 |
| `TestCentralModelSystemConfigurationAndRestart`（binary real） | 实际 bootstrap→login→session/CSRF，再 credential create/bind、Provider/Model CRUD、初始 unconfigured selector→合法配置、两类原 key 查证及重放；真实 typed Audit/Secret refs/Model events 同原规则持久，无 Model deliveries。相同部署输入重启，singleton/配置/receipt/原日志不重置，旧 Account/mail 声明仍唯一；正常 SIGTERM/SIGINT 退出 |
| `TestCentralModelSystemCurrentAuthorityAndRouting`（binary real） | 真登录成功请求与匿名、错误 Host/Origin/CSRF、已注销 Session 的旧 receipt 拒绝；六路径与旧 Account/诊断互不抢占，HEAD/405/安全未知路径，一次日志无材料；/readyz 仍 503，Project/Invocation/Usage 未注册。普通用户/role 竞争等 HTTP 库已有同输入证据可按风险复用 |
| `TestCentralModelInitializationFailureBeforeListen`（binary real） | task-owned 已迁移库中的实际 Model 必需存储不可用，以及普通初始化锁等待被停止两分支；实际启动失败/取消不监听、原受限日志/进程/DB owner 正常收束，退出码和原预算不变；无吞错或临时关闭初始化 |

二进制身份使用自己 fixture 的受限 bootstrap log 及正式 `/auth/bootstrap`、`/sessions/login`、`/session` 协议，不铸造 cookie/Actor、直接造 Session 或跳过真实 root。仅读取任务自有日志，材料使用后清理，报告/原普通日志不能包含其正文。跨服务事务事实允许只读 SQL 旁证；制造存储不可用/普通竞争只能针对 task-owned fixture，不能改变业务授权数据来伪装生产允许。

适用旧回归至少包括 `TestAccountProcessBootstrapRecoveryLogAndBoundCapabilities`、`TestAccountProcessUnsafeRecoveryLogFailsBeforeListening`、`TestCentralRealDatabaseSIGTERMAndSIGINT`、`TestCentralSecretActualStartupRotationAndIndependentKeyRemoval`、`TestRealSecretStartupSecondSignalSingleForceBudget`、`TestCentralOutboxEmptyMechanismHealthRecoveryAndSignal`、`TestCentralOutboxInvalidStorageAndCatalogPreventListening`、`TestB04AppSMTPFinalReplyDrainsBeforeCoreAndGuard`、`TestRealDatabaseHTTPTransactionDrainAndForce`。它们分别覆盖旧 Account 启动/日志、普通退出、Secret 初始化/fallback、catalog/handler、真实 Mail 和原 DB-last；不降低原断言，不重开 Object 已停止修复任务。语义和输入未变的 Model/Secret/HTTP 库证据可引用原提交及精确覆盖，不机械重跑全部库级矩阵。

实施先固定 Go 1.27.1、`GOTOOLCHAIN=local`、`GOPROXY=off`、`GOSUMDB=off`、`GOWORK=off`、`-mod=readonly` 和自有 `/workspace` cache/TMPDIR。对 app、tests/process 运行 unit/race/count=1、vet；两包 integration/race `-run '^$'` 实际编译，两个 cmd 实际 build 到自有目录。不运行前端或真实 Provider 账号，不为通过修改依赖。

真实 PG/MinIO 组只在主线程交接独占 fixture 窗口后用既有脚本运行；预先冻结 selector，沿原 `-race -count=1 -timeout=6m`，需要分组时按真实 package 预算固定划分，不能增预算或运行后过滤失败。保留实际 argv/env/exit、顶层/子例名称、固定输入及最初失败。普通有界锁/Store 观察不得演变为网络/ROLLBACK 故障代理。独立验收者按默认生产绑定、实际事件/引用/当前权限与请求关闭风险选择真实组合验证，不能只审构造签名或复述作者通过。

## 8. 冻结与交付

作者冻结 8 路径清单及基线、纯/真实原始证据，明确新结果/旧复用/失败与未验部分；动态窗口结束须核 task-owned exact ID 两遍 absent、原资源基线不变、所属进程 0/runtime 空再交还。轻量报告不保存凭据、完整快照或 cache。独立结论通过后只同步白名单两份能力文档；主线程负责状态页、精确 Git 提交推送与最终移交。

本卡后续实施验收完成只表示 System 配置已在真实 Central 默认根对外服务。完整 D09、Summary、Model 调用/usage/消费者、Project/R4/Artifact 与 Object 已知缺陷均保持上述边界。规格静审不是产品验收；当前设计者未运行 Go/Docker/SQL 或任何动态探针。
