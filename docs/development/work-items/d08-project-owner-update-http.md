# D08 Project Owner 名称/描述更新、命令查证与默认根

修订：rev1，2026-10-07，**完整限定独立 STATIC PASS，root 已采纳；尚未授权产品实施、Go 检查或资源执行**。本轮唯一仓库写入为本页；下列实施白名单不是业务、测试或资源授权。

固定已接受产品基线：`901eb54605d293d4308caadd278c2c3a7ae1b824`，root 已确认提交推送及远端一致。它包含 [Owner 列表/详情只读完整结果](../agent-team/project-owner-read-http-verification.md)、已接受 Usage root 和 Summary S1/S2/S3；不消费未验实现。本卡由 root 采纳的下一完整结果建议而来，只把已定 Owner 改名/描述能力接到正式 HTTP 和真实根，不重新定义 Project 业务契约。

## 1. 结果、前置与不包含的能力

当前 Owner 可以通过稳定 ProjectID 更新 name/description，收到原命令的历史 ProjectRef；响应丢失后，可在当前授权下查证原 update key 或显式同义重放。GET/list/名称 resolve 能观察同一 ProjectID 的新配置；旧名称立即失效且可复用，不建立 alias/redirect。规则归 [Project 架构](../../architecture/project-work-management/README.md)、[D08 设计](d08-project-owner-design.md)及既有 typed contract，本卡只补可运行的边界与装配。

| 已接受能力/证据 | 本卡实际消费 |
| --- | --- |
| D08 B01 `199554b`、B02 `769ec8c`/`6319d03`，[B02 接受记录](d08-project-owner.md#b02-独立验收与提交) | `UpdateProject`、`LookupCommand`、Owner/Session、命名、版本、幂等、两阶段计划与原子 Audit/Event/receipt/Activity；保留原首红及20真实主例的验收粒度 |
| D07 及已接受默认 System Model 根；[根规格](recovery-d09-system-model-root.md) | 原 Account HTTPBoundary、Activity Authority、Audit、Outbox、真实 process/guard、唯一 middleware 与关闭链 |
| [Owner read](d08-project-owner-read-http.md) `901eb546`、[Usage 根验收](../agent-team/project-usage-read-http-root-verification.md) | 同一 Project Authority、Reader、名称 resolve 与 Usage 路由；已验完整编码、native deadline/实际尾部机制 |
| [Summary S2 接受](../agent-team/system-meeting-summary-settings-verification.md) `4e615c7d` | 原 Secret→Model→Summary→Usage 初始化、System 路由及真实根兼容 |

已定点核对实际接缝：`project.Authority` 实现 `audit.ProjectAuthority`、`outbox.ProducerAuthority` 和 `outbox.ProjectAuthority`；`tests/project/b02_fixture_test.go` 已用同一个 Authority 组合真实 Audit/Outbox。Project Update 的正式事实不依赖 Object/Knowledge checker。现 root 只是尚未把该 Authority 传给 Audit/Outbox，也未构造命令 Service；没有发现必须先补的公共依赖。

本卡不开放创建、初始化状态、归档/恢复/删除/Retry 或它们的 lookup，不提供 UI、迁移、Project Secret/Model 配置、通用 Project Audit HTTP、Model Resolver/Invocation 消费或后台恢复 worker。`Initializer`、`LifecycleRegistry`、Authority 的 Lifecycle/AuditFacts 仍真实未绑定；不得用空实现。Object runtime join、OpenAI tools 独验、SPA concurrent-publication 三停止保持，不调用停止任务的修复/探针。`Resolution`/`Invocations` 仍 nil，ready503、完整 D08–D28/E01 未完成、E01 未开始保持。

## 2. HTTP 资源与严格请求

新增两个操作，均复用原 Account 的 Host/Origin/Fetch-Metadata、规范 path/RawPath、Session/CSRF 和安全头；Actor 不来自 JSON。所有业务目标只取 path 中 canonical UUIDv7，不接受可读名称或 body 中第二个 ProjectID。

| 方法/完整路径 | 请求 | 成功200 |
| --- | --- | --- |
| `PATCH /api/v1/projects/{id}` | 唯一合法 `Idempotency-Key`；JSON 必需 `expected_version`，可选 `name`/`description` 且至少出现一项 | 原 UpdateProject 返回的安全十一字段 Project 投影 |
| `POST /api/v1/projects/{id}/commands/lookup` | 同样携带原 key；JSON **恰** `{ "command": "update" }` | §3 的三态原命令观察 |

lookup 是当前 Read，但 unsafe POST 仍须当前 CSRF；不新增 lookup 自身的业务 key，不把 key 放 URL，不接受任意 namespace、command、actor、scope 或 target。调用固定构造 `pc.CommandLookupRequest{ProjectID:target,Command:pc.UpdateCommand,Key:originalKey}`，在服务调用前拒绝其他 command，不能把 `CommandName.Validate()` 的全枚举直接当 HTTP 白名单。该专用查证是本卡对旧 D08 “不开放任意 namespace/key 枚举入口”的窄落实，不开放通用命令入口。

PATCH 构造原 `foundation.CommandMeta{RequestID:httpapi.RequestID(ctx),IdempotencyKey:key,ExpectedVersion:&version}` 及 `pc.UpdateProjectRequest`，只调用一次 UpdateProject。RequestID 取原唯一外层 middleware 的服务端 ID，缺失则拒绝，不采纳用户 trace header，也不生成第二层 middleware。key 沿原1–128字节允许字符闭集，必须恰一个 header 值，不 trim/分割合并/自动换 key。

两个新口均拒绝非空 query 和 ForceQuery。PATCH body 上限 **64 KiB（65536 B）**；lookup body 上限 **1 KiB**。前者是此单口对旧 D08 普通16KiB草案的明确上限修订，保证8192 B description 的默认最坏 JSON 转义仍完整可达；不放宽 decoded description。沿 `httpapi.DecodeJSON` 的 UTF-8、深度、未知/重复/大小写别名字段、尾随 JSON、media/charset/Content-Encoding 检查，并补精确 presence：

- `expected_version` 必需，是原正 int64 canonical 十进制字符串 `1..9223372036854775807`，不能 number/null/空/前导零/指数/溢出。**不在 HTTP 排除 MaxInt64**：合法 no-op 或历史重放仍可成立，真正变更的溢出由原库判 InvalidState。
- `name`/`description` 缺席表示不修改；显式 null 非法。空 description 是清空正文，空 name 非法；两项均缺席非法。presence 差异保留到原 UpdateDigest，不能先合并当前 GET 再构造请求。现 `DecodeJSON` 对指针字段允许null，仅用 `*string` 不能区分缺席/null；须用本包私有 presence-aware 解码（如拒null的可选scalar）或保留原字段presence再校验，不能因公共decoder成功就放过null。
- name 沿原1–64字节 ASCII 规则、显示大小写与 lowercase 规范名；description 沿原 UTF-8、控制字节及8192 B限制。不 trim、不归一化正文、不从 MaxLength 字符数冒称字节约束。

路由采用新 `NewUpdateHTTPHandler(*project.Service,*account.HTTPBoundary)` 与窄 `HandlesUpdateRequest(method,path string) bool`。后者只匹配：详情资源上的非 GET/HEAD 方法，及精确 `/projects/{id}/commands/lookup` 的所有方法；不提前验证 ID，非法 ID 仍交目标 handler 报400。`/projects`、`/projects/resolve` 和其它子路由不被抢占。root 在已验 `projectReadRoutes(...)` 外再包一层 update dispatcher：GET/HEAD 详情仍交原 Reader；PATCH 详情交新 handler；其余详情方法405 `Allow: GET, HEAD, PATCH`；lookup 非 POST 为405 `Allow: POST`，HEAD 错误也无 body。原 read-only constructor/dispatcher 保持原能力与测试语义，不在未绑定的独立 reader 上虚报 PATCH。

canonical 未知路由404；trailing slash、重复斜杠/路径清理、RawPath 沿 Account.CheckRequest 的实际400等原错误，不 redirect，不统一误写成404。Body/URL/Context/RequestID 不在 dispatcher 中重建或消费；日志仅设稳定 route 模板，无 key/body/query/名称或旧内容。

## 3. 当前授权、历史结果与安全投影

服务原顺序保持：形状检查→当前 Session/Owner/可见性→原 key/semantic receipt→仅新 mutation 才核 Mutate、version、命名/依赖。HTTP 预认证只是会话事实，不用额外 Mutate 预检查阻断 archived/archiving 上合法的历史重放；他人管理员没有 Owner 豁免。库在同一实际 Tx 内重验当前授权，HTTP 不查 account/project 私表。

初次实际变化沿原两阶段持久 plan、完整锁 union 与最终重核；canonical、ProjectUpdate Audit、project.updated Event、completed receipt 与 Activity Touch 在原事务闭合。no-op 沿原 completed receipt + Activity，不推进 Project version/updated_at，不新增 Update Audit/Event。重放返回原历史 ProjectRef，无第二次 Touch/Audit/Event，不拿当前 GET 或当前 version 覆盖历史结果；原 user 同 key 异义（包括 presence/expected_version 差异）仍冲突，新 Session 同义重放沿当前权限允许。

PATCH 成功 DTO 复用原 Project 十一字段，精确字段、nullable、时间和 ID 规则见 [Owner read §3](d08-project-owner-read-http.md#3-http安全投影与-schema)。同时要求结果 ID=target、Owner=本次 Human、有效 typed ProjectRef、非 deleting，且 update 历史结果为 active；请求中出现的 name/description 与结果一致。版本只可等于 expected_version（no-op）或在未溢出时等于 expected_version+1（变化），不能笼统要求全部+1。历史内容可能早于当前配置，但当前失权不能发布它。不得序列化 CommandMeta、私有 plan、creation、operation、initialized、semantic/key 或 DB 原行。

lookup 精确三态：

| state | 其它字段 |
| --- | --- |
| `committed` | 必需 `result:{command:"update",project:<上述安全历史Project>}`；无 creation/lifecycle/其它字段 |
| `in_progress` | 只有 state，result **缺席**，不能 null 或假回执 |
| `not_observed` | 只有 state，result **缺席** |

先核原 typed union，再核 command=update、目标/Owner/Project完整性和闭合集合，坏 union/错目标/错Owner/多余分支返回 DependencyUnavailable 且零成功候选。lookup 没有请求摘要/expected_version，**只观察该 command/key，不证明另一份 body 同义，也不授重发资格**。`not_observed` 是原锁串行后当前查不到回执；不能把查询失败/超时/仍持 writer 锁当 not_observed，也不能据它抹除客户端先前未知结果。已删除 Project/不属于当前 Owner等沿原库错误，不新增删除后历史内容入口。

两个成功表示均完整编码后才写，上限 **64 KiB**。最大描述默认转义49152 B，加有限十一字段/lookup外壳小于该限；必须实际编码/反解最大合法 PATCH 请求、Project response 和 committed lookup，而非只有算术。复用本包已验投影检查/私有 I/O helper，不改读口5MiB限额、不截断、不分段输出。200 为 `application/json`、`Cache-Control:no-store`、准确 Content-Length。

沿公共 Fault/Problem：400输入、401会话、403CSRF/Origin等、404非Owner/无目标或路径、405方法、409版本/名称/状态/异义key、413超体积、415media、503依赖或最终 CommitUnknown、其它原映射保持；不把所有 PG 锁/取消错误重标同一码。内部 `UnknownAttempt` 保原 CauseID/attempt/commit-state；wire 只用原 `code/commit_state/request_id/retry_hint` 等公共 Problem，**没有 cause_id 字段或头**。有效预算内最终 Unknown 保503/lookup提示；已过发布期限走 abort，不能伪造 NotStarted/NotCommitted Problem。

`api/openapi/project-owner.json` 仅在原路径增加 PATCH、增加此 lookup POST，以及闭合 UpdateInput/UpdateLookupInput/UpdateLookupResult schemas；复用 Project/common Problem。PATCH 输入保至少name/description之一，version精确正int64字符串；所有对象 additionalProperties:false，三态 result presence精确。GET/HEAD原 schema不重写。标准 Draft 2020-12 + FormatChecker/本地 refs 核真实成功 body 和正反代表；schema注释明确description字节约束，不能用Go regexp/文件含字段自证。

## 4. 发布预算、Unknown 观察尾部与实际收尾

PATCH 从 CheckRequest/RequireHuman **之前**设 `min(parent,now+30s)` 发布/I/O预算；lookup从相同位置设 `min(parent,now+2s)`。同一 context贯穿认证、读体、服务、完整投影/编码和 Write/Flush，不更新成功期限。使用已有本包 `requestIO` 的真实 ResponseController read/write deadline、取消回调、同步 Body.Close 与 callback join；不导出通用框架、不改旧读预算。DecodeJSON 替换的 MaxBytesReader 包装与原 Body 的真实 Close 所有权须一致，不能重复Close/遗失原流。能力不支持就在业务调用前安全abort，不退化为无界读或脱离 owner 的goroutine。

**原库例外必须如实区分**：`project.lookupAfterUnknown` 已接受的 `WithoutCancel(ctx)+3s` 确认保留，不改 Project commands。Update 在 planning 或 final CommitUnknown 后可能继续持有原调用，作至多3秒的原writer锁/历史事实观察；它是既有确认退役尾部，**不是延长HTTP发布期限，也不宣称全部 handler 必在30秒返回**。更早 parent/断连/Stop发生时同样可能触发此尾部。handler同步拥有这次唯一服务调用，等其实际返回后才结束，不用 select+遗留goroutine代替；过发布期限不再成功/Problem写，不取消后续语义为“未提交”，不因确认返回历史结果补迟到200。

保留库原 planning/final 分类：planning阶段持久plan/in_progress不等于Update成功，原Unknown保留；final阶段在原writer终局串行后明确未完成的原plan可给原NotCommitted/retry_same_key；确认失败仍保原Unknown/cause，不能由HTTP二次lookup、重新GET或重试变更分类。原服务已确认且HTTP预算尚有效时才允许返回历史200；调用方显式再次PATCH必须保存原target/key/body/version。

成功在实际Body.Close、完整Write、Flush以及停止/实际join取消callback后才清除连接deadline。短写、Write/Flush/Close错误、panic、清deadline失败或超时安全abort，不补第二个Problem。HEAD只用于原读或错误，不新增写口HEAD业务调用。自然30s PATCH与自然2s lookup各有代表，更早parent另测；数据库1s lock_timeout不能代替HTTP自然预算。受控非合作尾部须先放行并实际join；原3s确认也不保证任意不合作driver能被context强杀，最外层实际进程终局/owned清理保留其边界。

## 5. 同一 Authority 的真实根与关闭顺序

### 5.1 纯构造与固定 providers

仅窄改 `bindAccounts` 的纯依赖创建顺序，不改变后续真实初始化次序：

1. 原 `account.NewAuthority` 成功后，提前调用原 **纯构造** `createProjectUsage(cfg,db,accountAuthority)` 一次；取得唯一 `projectUsage.projects`。这个 helper只构造 Project Authority、Usage Authority/Service，不执行SQL/Initialize/worker；后面的原调用删除，所有 Usage/Reader/新Service/Audit/Outbox捕获这同一指针和Store。不得改其参数、内部授权或造第二个Authority。
2. 原 Model Authority/SecretUsageRouter照旧，**Model.Authorizations.Projects 与 Resolution仍nil**。`createSecurity` 私有签名追加必需 `projects *project.Authority`，保持原 Store/Account/Model校验并拒nil；调用原 `audit.New`，新增 `Projects:projectUsage.projects`，其余原字段不变。固定生产调用点只有bindAccounts；`app/model_test.go`按真实纯构造补参数/缺参反例。原 Project Authority 的 Lifecycle/AuditFacts为空，不能因注入Audit而假装Secret/Object/Knowledge事实已绑定。
3. 原 objects assembly得到唯一真实process与ProcessGuard，沿原 `accountProcessAuthority{process,guard}` 供新 Project Service；不新建process、不实现always-stopped、不执行创建恢复。其CurrentProcess为已有实体，ConfirmStopped仍只委派原guard，此卡不证明已停止Object缺陷修复。
4. 在原同一 event.Catalog 被 `outbox.New` seal之前，调用 `pc.RegisterProjectEvents(catalog)`，保留Account/Model定义。该正式注册器一次注册 created/updated/lifecycle三种typed schema；这是catalog必要闭包，**不意味着开放Create/lifecycle命令或发布这些事件**。失败丢弃本次构造，不吞重复/不单独手写替代schema。journal新增 `Producers[pc.ProjectProducer]=projects` 及 `Projects:projects`，原Account/Model producer、Sessions/System/Processes/Audit/Cursors不变；唯一生产handler/subscription仍为account.mail-enqueue，无空Project consumer。
5. 在真实auditor/journal/processes齐备后，以同Store调用 `project.New`：Authority=projects、Activity=原Account Authority、Audit=auditor、Events=journal、ProjectEvents=上述typed handle、Processes=原真实process authority、Cursors=原keyring、DefaultConfig；Initializer/LifecycleRegistry明确nil。constructor已允许二者未绑定，新HTTP只持Update/Lookup能力。它没有额外Initialize/Start/Health/RecoverCreations，不为fresh系统制造项目或Skill。
6. 立即把新Service的私有work adapter登记到原accountAssembly，再继续后续可失败构造；不在所有构造成功后才补登记。原Account core建立后，用原NewHTTPBoundary给新handler，最外层仅加§2窄dispatcher；不重建core、middleware或setter授权locator。

Audit最终调用原 `CheckAppendInTx` 验ProjectUpdate持久计划/目标/Owner/version/changed_fields/cause/held锁；Outbox必须同时验证Project producer的prepared canonical事实与Project gate的CurrentAccess/NewFact两阶段，不能仅Owner boolean或只调用其中一边。两者与canonical/receipt使用原同Tx完整锁计划；新root不给其它producer通用allow。缺端口/错Store/registration失败沿既有owned partial cleanup，不发布handler。

原 `deps.secret` 的 Secret→Model→Summary→Usage **实际Initialize**仍在原SecurityStartupTimeout=30s同ctx/deadline内、原后继Object/Outbox/Account启动和listen之前；前移纯构造不能前移SQL或重开预算。原新read/Usage初始化、Summary未配置、唯一mail注册及ready/diagnostics不变。

### 5.2 Project work 的实际 Drain 与 root 联合终局

新增私有 `projectCommandWork`（位于app/project_update.go）包装同一具体Service，实现现有 `accountWork`，不改Project公共Service/contract：

- StopAdmission同步调用 `Service.Stop()`；Force(ctx)同步发 `Service.Force()`，随后只用传入的原ctx调用 `Service.Drain(ctx)`。不得派一个无人拥有的Drain goroutine，也不得重开3秒/30秒/root预算。
- Drain(ctx)先Stop，再实际等同一Service.Drain；**只有返回nil才记录joined**。Joined必须同时证明已停止准入和实际Drain完成；ctx过期/调用尚在原Unknown确认尾部时为false。Force仅取消不构成join。并发Stop/Drain/Force记账须race安全；超时后再次合法Drain可观察真正终局，不能因一次超时永久虚报成功。
- accountAssembly新增私有projects work，`works()`顺序为 **Project→Mail→原runtime/core（或partial sink）**。StopAdmission仍先停止所有work，再开始任何等待；Project先于其需要的Account Activity/剩余provider退休。原Mail→core及partial sink规则不改。install与constructionDone继续覆盖取消中/Force后迟到安装，所有已构造Service有同一owner。
- 所有Drain/Force消费原root剩余shutdownctx。Project未join使accountAssembly.Joined=false，原 `resources.producersJoined`/HTTP/Outbox联合门禁不能放行guard正常退休；DB仍按原流程最后ForceClose。即使余时为零，也同步发出全部既有Force，不因Project首错跳过Mail/core/DB。forced进程退出不冒称所有组件goroutine已join，也不改Object已知缺陷。

没有后台Project任务，仅新同步命令请求及其原确认尾部。自然关停、服务调用取消、HTTP Close/callback和Unknown确认必须各自实际完成；不能用HTTP响应已abort、ctx.Done或socket关闭代替Service.Drain。

## 6. 精确后续实施白名单

**18技术路径 + 1后端README末件**。当前只授权本规格；实施必须由root另授唯一作者，必要共享包/实际传递输入冻结后才执行。新文件应先核不存在；以下只列必需范围，不为凑数改字节。

| # | 路径 | 允许范围 |
| --- | --- | --- |
| 1 | 新 `internal/central/project/http/update.go` | 正式Update handler、两操作、窄route判定、一次服务调用与原I/O helper收尾 |
| 2 | 新 `internal/central/project/http/update_wire.go` | strict请求/presence/原meta、两类安全结果/完整编码上限 |
| 3 | 新 `internal/central/project/http/update_test.go` | 受控授权/预算/Unknown/服务与尾部实际拥有，不监听 |
| 4 | 新 `internal/central/project/http/update_wire_test.go` | 请求/receipt闭集、版本边界、真实最大编码及标准schema |
| 5 | 新 `internal/central/project/http/update_native_test.go` | 显式授权的native keepalive/慢读体/写与取消尾部 |
| 6 | `internal/central/project/http/wire.go` | 仅首行package说明由“reads, not commands”同步为已验读口加限定Owner update；读业务/DTO/helper原字节不变 |
| 7 | `api/openapi/project-owner.json` | 增PATCH、专用lookup及闭合schemas，原GET/HEAD/common引用保持 |
| 8 | 新 `internal/central/app/project_update.go` | Service纯构造、固定providers、projectCommandWork及窄handler/dispatcher |
| 9 | 新 `internal/central/app/project_update_test.go` | 同Authority/Store与纯装配、route所有权、缺依赖/partial/真实Drain记账 |
| 10 | `internal/central/app/account.go` | 前移既有纯createProjectUsage、catalog/Audit/Outbox/Service接线、projects work顺序及HTTP组合 |
| 11 | `internal/central/app/security.go` | createSecurity必需projects参数与真实Audit.ProjectAuthority注入 |
| 12 | `internal/central/app/model_test.go` | 仅createSecurity旧构造测试同步参数与缺Project反例；System/Summary路由原断言不减 |
| 13 | `internal/central/app/account_lifecycle_test.go` | 在原joint/partial/force框架补Project工作、预算耗尽/迟到安装/门禁反例；原Mail/core/sink断言保持 |
| 14 | 新 `tests/model/project_owner_update_http_fixture_test.go` | 正式账户链、真实Project命令/Audit/Outbox、持久Skills测试口与实际PG ACK代理接缝 |
| 15 | 新 `tests/model/project_owner_update_http_test.go` | 真实HTTP strict/success/no-op/权限/幂等/版本/名称竞争/精确原子事实 |
| 16 | 新 `tests/model/project_owner_update_http_unknown_test.go` | planning/final真实Unknown、writer串行、预算/当前权限和原确认尾部 |
| 17 | 新 `tests/model/project_owner_update_http_root_test.go` | 默认app.Run真实写查读、旧路由/唯一注册/无初始化/关停组合 |
| 18 | `tests/model/project_owner_read_http_root_test.go` | 仅末尾旧“no write binding”日志改成“no creation/lifecycle binding”；所有旧读取/POST-list拒绝断言原样 |
| 19 | `docs/development/backend/README.md` | 技术独审通过后单独授权的最终状态；保留Summary/Usage/Owner读口与原失败边界 |

Project `commands.go`/service/authority/events/contract、Audit/Outbox/Account核心、app/resources.go/app.go、原project_usage.go/project_read.go、共享helper、迁移00001–00020、锁文件及前端均只读。公共同事务/lookup/关闭机制如暴露新缺口，仅冻结该点报root，不扩白名单或用新生产假端口补洞。既有新read fixtures可只读复用，但新集成前置由本卡私有fixture承担，不改旧schema解释器/helper。

## 7. 必需自测、独立验证与执行边界

### 7.1 离线与资源分界

沿Go1.27.1、离线modreadonly、固定已装工具/cache；先实际go list普通/race/integration和两cmd/fixture动态构建图，冻结import/embed/TestMain/实际runtime文件/标准schema依赖，不复制全树。每条离线命令45s、pure/race编译与运行分开、p1、subreaper/direct+adopted实际wait、前后hash及两次owned空；首次失败保原命令/输入/raw，scope内修复后只重跑受影响检查。

相关Project/HTTP纯普通+races、racevet、两cmd build、integration compile与精确selector-list必须覆盖。app普通包含真实socket测试，**不得把无过滤app包当纯测**；只列举后执行本卡构造/路由/受控work及明确无资源旧selector，原B04 Sink/health等需按真实行为另分类。标准schema检查冻结实际Python模块与jsonschema_specifications数据，用Draft202012+FormatChecker且不联网取refs，不因“仅schema”遗漏文件。

native与PG/MinIO实际资源另交root独占窗，不由卡的STATIC接受自动获得。每轮固定输入/driver/精确selector；native至少2GiB余量，新native单top包含自然30s及清理，外层受控watchdog按45s测试/55s干预与额外退役区分，前置trace捕获全部listener/socket/PID-starttime，实际wait后关联TCP含TIME_WAIT及owned进程双空。可把自然30s只置一个native代表，其余采用更早parent，不把短parent冒自然30s。

PG沿完整既有fixture7资源/全Mount/nonce、fresh≥5GiB、race/count1/p1、包6m；每新top120s总预算**含t.Cleanup**，实时-v watchdog真正跟踪top开始/结束，不能仅每调用预算或pkg6m。失败先实际终局/双清并停后继，不自动重跑。daemon/PID1 shimZ按每轮actualbaseline集合差另记，非owned/未task-wait，不声称全机零。具体分轮按实际编译图和库存冻结后由root授窗，预算不足先分组，不扩预算/减断言。

### 7.2 正式身份与验收矩阵

新身份沿Bootstrap/Login及Invitation/Redeem/Login，不SQL伪造Session/admin；Project测试前置可沿正式Create + 持久Skills测试fixture，明确不是生产Skills。特殊archived/archiving/Owner事实可在正式锁下做可回滚canonical测试输入，不宣称生命周期/转移命令已实现。测试wrapper只观测、阻塞、失败或委派真实端口，不以always-allow替代Owner、Audit或Outbox事实。

| 代表 | 必须证明 |
| --- | --- |
| strict wire/结果 | body字节与presence/重复/别名/非法UTF8/null、query/ForceQuery/media、key多值、版本MaxInt64/no-op/溢出、非法目标；不同编码同义沿原digest；最大description请求及两种成功表示实际编码/完整反解；坏union/错ID/Owner/command/版本零投影 |
| 真实写入与幂等 | 改name/description及显式清空；旧名称404、新名称同ID/旧名被另一Project复用；相同key跨有效Session重放旧Project，后来配置不覆盖历史；异义/旧版本/同名竞争、两已计划target抢名；no-op只有receipt+Touch、变更恰一Audit/Event/receipt |
| 当前授权与原子门禁 | 他人Owner/admin、失效Session、gate前后变化，当前Read先receipt；archived合法历史重放但新写拒绝；canonical/receipt/Audit/Event/Activity前后集合精确；在真实Append后注入失败整Tx回滚；缺User/Project/Outbox锁、错actor/session/summary/cause/changed_fields/原prepared plan拒绝，核到达真实validator，不以构造错误替代 |
| 物理Unknown与查证 | 分别在planning plan持久及final业务/receipt/Audit/Event/Touch事实齐后才arm真实PG COMMIT ACK代理；记录writer实际终局/锁串行，commit/rollback/pending、撤权确认无历史泄露；planning in_progress不报成功，final未完成plan沿原NotCommitted；HTTP仅调用一次，外部lookup三态/同key异义分清 |
| 预算与关闭 | PATCH自然30s、lookup自然2s与更早parent；权限、慢Body、Write/Flush/Close/callback；确认尾部跨parent取消实际仍在调用内/Joined=false，放行后实际Drain；root关停并发Unknown尾部、余时耗尽Force与DB最后关闭；过发布期零迟到成功/Problem且无悬空goroutine |
| 默认root | 不注入私有成功handler；真实app.Run PATCH→lookup→原GET/list/resolve/Usage，同一Authority/Store链；真实Project Audit/Event及唯一mailhandler，原System/Summary初始化和路由、Account/ready503保持；Create/listPOST/lifecycle命令与lookup均不开放，生产Resolution/Invocations仍nil，实际关停 |

新PG精确顶层固定为 `TestModelProjectOwnerUpdateHTTPStrictAndReplay`、`TestModelProjectOwnerUpdateHTTPAuthorityAndAtomicity`、`TestModelProjectOwnerUpdateHTTPUnknown`、`TestModelProjectOwnerUpdateHTTPRootBinding`；可在各自top用subtest分场景，不把helper当新覆盖。native三top固定为 `TestProjectOwnerUpdateNativeKeepalive`、`TestProjectOwnerUpdateNativeBodyDeadline`、`TestProjectOwnerUpdateNativeWriteAndClose`；自然预算/确认尾部的纯受控代表仍不监听，名称中标Pure并在运行前列举。

必要旧PG回归精确覆盖：`TestProjectB02AuditEventReceiptTouchAtomicityAndNoOp`、`TestProjectB02TwoPlannedTargetsCompeteForOneCanonicalName`、`TestProjectB02ReceiptRequiresCurrentSessionAndReplaysAcrossRenewal`、`TestProjectB02UnknownOriginalCallResolvesAfterWriterSerialization`（tests/project）；`TestModelProjectOwnerReadHTTPProjectionAndPaging`、`TestModelProjectOwnerReadHTTPRootBinding`、`TestModelProjectUsageHTTPRootBinding`、`TestModelMeetingSummarySettingsRoot`、`TestModelSystemHTTPConfigurationCRUD`（tests/model）。普通/root关停受影响旧纯项据实际调用行为精确冻结；不为未变模块机械重跑全库，no-tests/SKIP不计业务兼容。

独立验收者未参与实施，先核完整冻结18技术路径/图/原失败，再独立重组至少两个真实代表：A当前权限、Audit/Outbox双门禁/原子性、原writer Unknown与预算尾部；B默认root、历史receipt与当前读分离、旧路径兼容、真实PATCH/lookup响应原字节的标准schema解析及真实关停。可复用固定未变作者证据，但来源和断言独立性要明确，不能拿库已验替代新root接线。HTTP原body保安全source metadata（method/path/status/真实Content-Type/length、run、target、candidate/schemahash），无Cookie/key/密码/DSN；敏感临时身份材料仅0600并随资源清理。

交付固定完整结果的代码/schema、作者命令/raw/实际wait/资源证据、版本组合与原红、独立结论，技术通过后才由root另授README末件；不是只交建议、compile或HTTP半口。当前仅规格作者定点源码与文档自查，不运行Go/native/PG，不宣称本卡产品PASS。新产品问题为零；工程公共依赖/范围若发生实际差异交root裁决，不能代用户扩大产品。
