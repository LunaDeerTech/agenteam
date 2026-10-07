# D09 Project Owner 模型凭据管理 HTTP 与真实根绑定

修订 rev2，2026-10-07。**scratch 工程规格候选，待完整独立 STATIC；没有正式卡/产品/Go/native/PG授权。** 推荐已获 root 采纳，本文件尚未接受。固定产品为 `61bed1fcf47c362f420f6bb45158b227af581acf`（Update 完整19），归档 `c839f965`。固定来源见同目录 `inputs-rev2.json`；当前活动 Project Model Owner read #1–13 不作已验输入。实施前须其完整接受并正式交接共享根，届时更新实际输入，不能直接按旧根覆盖。

## 1. 完整结果、已有能力与范围

当前 Human Owner 可以创建/旋转/删除自己 Project 的 Model-purpose Credential，读取安全 metadata，并在当前 Read 权限下被动查证原凭据命令。返回稳定 CredentialID，可由既有 Model 配置库构造同 Project CredentialRef；本卡不开放 Provider/Model 写入 HTTP。凭据写与后续 Model 配置分别是原有独立命令，不组成新跨命令原子操作，不生成默认 Provider/Model。

| 已接受前置 | 本卡消费与限制 |
| --- | --- |
| `7d7c50df0dafcdeaaf700dc2662a6013245bbb6f`，`docs/development/agent-team/secret-project-audit-verification.md` | Secret ProjectAuditAuthority 真实 canonical/私有 witness/Tx/锁 checker；不是任意 Owner allow |
| `81fe7427ceb4672247b3d30a51c10a2e2808ba04`，`project-secret-audit-binding-verification.md` | Project SecretAuthority、AuditFacts 窄路由及真实 CRUD/当前权限/Unknown组合；旧 Outbox 原未归因红保留，非一次全旧组通过 |
| `ecd733711caff5df46e423cadab52b32c34f785e`，`system-model-management-reads-verification.md` | Secret.Metadata 的 Project Owner/Read 同事务兼容已验；本卡补 Project HTTP |
| `ac5b4c65e88ed0ec1813fbf341035c88ca9a38c7`，`d09-system-model-http-verification.md` | System 凭据 HTTP、被动 lookup 的安全投影与 Purpose保护；Project lookup 契约目前不成立，必须按§3新增 |
| `de00c610da62cb77cc03efe7c3cc842cf81f1ba5`，`d09-project-configuration-verification.md` | 既有 Model Provider引用 retain/release 与 ProjectScope；用于真实引用竞争测试，不把 Model配置/Agent rewrite/root read 候选当本卡实现 |
| `61bed1fc`，`project-owner-update-http-verification.md` | 同 ProjectAuthority 的 Usage/Owner/Update、真实 Audit/Outbox/root、HTTP有限能力预检与实际尾部验收。仅复用适用模式，不把 Project Update 的自动确认规则移植到 Secret |

本卡公共契约增量限 `WriteCommandLookupRequest.Validate` 接受下述精确 Project 分支及库实现完整锁/当前授权；原方法/类型/JSON形状不变。Secret 的写语义、材料/加密/nonce/receipt schema、Purpose枚举、Account公共口、Project权限不改；无迁移、锁文件、UI、凭据列表/材料读取、Provider网络、Secret cleanup、Service写入、Project创建/初始化/生命周期或 Model Resolver/Invocation。

系统统一 Meeting Summary initial/update含标题，无 Project override/复制默认，compaction/Execution Summary原规则不变。Resolution/Invocations/D24仍未绑定、ready503、D08–D28/E01未完。Object runtime join、OpenAI tools独验、SPA concurrent-publication三停止不恢复。

## 2. HTTP 资源、严格输入与输出

新增 `model.NewProjectCredentialHTTPHandler(writes sc.HumanWriteCommands, boundary *account.HTTPBoundary) (http.Handler,error)`；拒 nil/typed-nil writes 和 nil boundary，捕获一次。沿已存在 HumanWriteCommands 接口，不加 Secret→Account import 环、不增加服务定位器/可变 grant。真实 root 只传既有已构造 `*secret.Service` 与正式 boundary；私有受控 boundary 用于单元测试，不能以任意 grant 作为公开构造参数。接口无 Store getter，构造不冒称已检查任意实现内部 Store；真实装配/运行 witness 另验。

`model.HandlesProjectCredentialHTTPPath(path string) bool` 只识别以下资源形状，对所有方法分派，ID在handler解析；不抢 Provider/Model/available-chat-model/Owner/Usage 路径。唯一外层 RequestID middleware不复制，稳定 route模板不含 key/material。Account.CheckRequest先沿 Host/Origin/Fetch-Metadata/RawPath/规范path/安全头，RequireHuman取得当前Cookie Session；不从请求接受actor/scope/ProjectID/Purpose。所有POST/PUT/DELETE（含被动lookup）仍须CSRF。

| 方法与完整路径 | 必需 JSON/头 | 成功200 |
| --- | --- | --- |
| POST `/api/v1/projects/{project_id}/model-credentials` | `Idempotency-Key`；对象恰 `value` | Mutation |
| PUT `/api/v1/projects/{project_id}/model-credentials/{credential_id}` | 原key；对象恰 `expected_version`,`value` | Mutation |
| DELETE 同detail路径 | 原key；对象恰 `expected_version` | Mutation |
| GET/HEAD 同detail路径 | 无body/query，不要求key | Metadata |
| POST `/api/v1/projects/{project_id}/model-credential-commands/lookup` | 原key；Create对象恰 `kind:"create"`；Update/Delete对象恰 `kind`,`credential_id`,`expected_version` | Observation |

PUT是替换整个凭据材料，动作名仍 `update`；不新增 `rotate` 命令或HTTP PATCH。list GET/HEAD collection为405 `Allow: POST`；detail其余方法405 `Allow: DELETE, GET, HEAD, PUT`；lookup其余方法405 `Allow: POST`；HEAD错误无body。canonical未知资源404；路径清理/trailing slash/RawPath沿原Account错误，不重定向。所有资源拒 RawQuery/ForceQuery。GET/HEAD明确拒 ContentLength非0、TransferEncoding以及实际非空body；不把Header声称0当完整读流事实。

key恰一个header，沿原1–128字节字符闭集，不trim/合并/重生成，绝不放URL。所有ID取path/body的canonical UUIDv7并用正式typed parser；ProjectScope只由path正式构造。Create不准 credential_id/expected_version（含显式null）；Update/Delete两项必须非null。expected_version是原canonical正十进制字符串，范围 **1..9223372036854775806**：与既有凭据lookup约束一致，结果必须+1；不存在Project Update的no-op MaxInt64例外。metadata/result version允许完整正int64。数字/null/前导零/指数/溢出/未知kind拒绝。

沿 `httpapi.DecodeJSON` 的严格 UTF-8、深度64、未知/重复/大小写别名、尾随JSON、Content-Type application/json（可charset=utf-8）、Content-Encoding拒绝；presence显式校验，不能只用可nil指针混同missing/null。value是必需非null JSON string，解码后 **1..65536 bytes**，沿既有System string→SecretMaterial行为，不trim/规范化/新增字符限制；不允许二进制/base64替代协议。超过decoded材料上限400；rawbody超限413。

POST create/PUT rawbody上限 **400 KiB=409600 B**；DELETE与lookup **1 KiB**。65536B合法最坏逐byte `\u0000` 表示393216B，加字段/version外壳仍小于400KiB；验收必须实际编码/解码最大材料与末byte，不只算术。允许界内额外JSON空白；精确cap与cap+1分别验证。

安全对象固定如下，全部 `additionalProperties:false`，无scope、材料、加密payload/nonce、digest/key/cause、数据库行或鉴权封装：

- Metadata：`{credential_id:<UUIDv7>,purpose:"model",version:<decimal-string>}`。
- Mutation：Metadata三字段加必需 `deleted:boolean`；Create/Update=false，Delete=true。
- Observation：恰 `{observed:false,result:null}` 或 `{observed:true,result:<Mutation>}`。不输出in_progress，不以缺席result替代null，不返回材料语义匹配结论。

正式结果必须验证typed ref有效且scope等于path Project；metadata目标ID一致且purpose=model，合法其它Purpose按原System规则404，不泄露其metadata。损坏ref/version/坏union/错scope或结果目标/动作/版本绑定返回DependencyUnavailable且零成功候选；lookup错kind/ref/version的原库KeyReused保持。Create成功版本=1，Update/Delete=expected+1；lookup同样约束；当前metadata与历史result不得混用。

成功完整编码后再发布，最大 **1 KiB**（真实最长合法四字段及Observation外壳须实际编码断言；该限不改变原公共Problem schema）。标准Problem沿Account投影，内部Fault CauseID/attempt原样保留，wire只有既有code/commit_state/request_id/retry_hint等字段，不加cause_id头/字段。Content-Type application/json、Cache-Control no-store、精确Content-Length及安全头；HEAD运行相同正式读取/校验/完整编码/尾部且Content-Length等于GET，body0。编码失败/坏结果不返回前缀、部分成功或换当前GET。

新增 `api/openapi/project-model-credentials.json`，OpenAPI3.1/Draft202012，六operations（GET和HEAD分别计），只引用既有common Problem/UUIDv7/PositiveInt64String等固定schema。版本Max-1及UTF-8字节约束在Go执行并说明，不能把JSON Schema maxLength当bytes；value标writeOnly，不放真实/测试材料example。原System/project-models schema不改。

## 3. Project被动查证：最小正式契约补口

现 `secret/contract/write_lookup.go` 只接受System+owners=[User]。保留该分支全部旧行为；增加Project时：合法Human、Scope精确Project、Purpose=Model、namespace=`secret`、command等于kind、owners恰 `[ProjectID,stable UserID]` 顺序一致；不包含Session/trace。Create为零ref/expected0；Update/Delete ref必须同scope、合法ID、expected在上述范围。其它scope/purpose/kind/多owner/错误顺序拒绝。`HumanWriteCommands`三个方法与Observation载体不变。

库 `LookupWriteCommand` 初步state/store/Sessions有效性与原System端口拒绝保持；不能把Project分支错误地继续依赖System管理员端口。Project分支同Tx **一次**预持 Command SH、User SH、Project SH，再RequireHeldLocks→当前Session+ProjectAuthority(Read)→只读取原scope/scope_key+command_digest的安全receipt列。Project缺口/typed-nil端口用既有lookupNil检查，当前Session拒绝优先再DependencyUnbound（可沿Metadata分支），不在HTTP造Owner grant。Grant必须精确匹配Actor/scope/Read。不能Tx外预查receipt、跨Store、追加临时补锁或用Txn外授权代替。

保留原receipt校验：kind/purpose/credential/version/deleted组合完整；请求绑定不符KeyReused。不读payload/encrypted semantic或canonical现材料，不调用Metadata/Execute/Resolve，不分配nonce/创建命令/追加Audit/Event。已删除credential仍可查历史receipt；Project本身删后/非Owner/未初始化/deleting沿真实gate拒绝。archiving/archived允许Read的历史观察；同User新有效Session可以查，旧Session撤销/Owner变化不得泄露。

库只在真实读事务Committed及末端ctx仍有效返回观察；Unknown/NotCommitted/SQL或扫描失败/提交后取消均零observation并保原Fault commit state/cause。present/absent都受相同规则；持原writer Command EX时lookup真实等待，超时不是absent。不得把合法无行观察解释为先前Unknown已回滚，也不让一份异义写借观察变成成功。System旧口必须逐输入回归，不能在此卡把其它Purpose/Service actor开放。

## 4. 原写语义、Purpose防误用与Unknown

每个HTTP写请求最终至多调用一次既有 `ExecuteWrite`，只返回该调用实际result/error。Create直接原请求；Update/Delete沿固定 `model/http_credentials.go` 的Purpose防护形状：

1. 用原完整非材料kind/ref/version/key做一次被动lookup；失败即返回，不执行写。
2. observed=true只表示可跳过当前Metadata，**绝不从observation.Result返回写口成功**。未观察到则Metadata核同ref、Purpose=model、version=expected。
3. Metadata失败（含NotFound、非Model、version冲突）时仅再做一次晚到lookup；若仍未观察到，返回原metadata错误；若观察到，继续原写。任一步失败/预算到期不继续。
4. 无论首次/晚到lookup命中，最终无条件以原scope/kind/ref/version/key及原材料调用一次ExecuteWrite。不能拿receipt重建材料或改key，也不在本handler自动重试Unknown/ResourceBusy。

这个模式只防把其它Purpose当前凭据错当Model、并允许删后历史材料重放；不移植Model或ProjectUpdate的Read→receipt语义。`Secret.Apply`在正式同Tx既持完整锁后先当前Mutate授权，再findReceipt/完整材料语义校验；archived/archiving即使已有committed receipt、即使单独Read lookup可见，写/同义重放仍拒绝。不得用Tx外Mutate precheck替换最终真实授权。

精确保留原准备边界：`PrepareWrite(Create)`已有Tx外receipt→stableRef发现和nonce分配，不声称所有receipt SQL都先Mutate；对外成功/历史返回由最终Apply授权+材料digest控制。失败/拒绝写可能已有持久nonce预留，验收不得要求nonce0；canonical凭据、业务receipt、Audit与材料事实须按原事务原子规则核零新增/回滚。相同key但不同完整材料仍KeyReused；安全lookup不含材料，不能代替此判断。Secret Delete有引用/未released lease原ResourceBusy，旋转同purpose不改变原lease/历史payload规则。

Secret ExecuteWrite **没有** Project Update的自动WithoutCancel≤3s确认尾部：HTTP遇实际Unknown直接保留原Unknown/503（若仍在发布期），不发自动lookup追认成功。客户端另一个显式lookup请求才观察原identity；同一key的显式材料重发仍走原ExecuteWrite，不因为此前观察而绕授权/摘要。Pending backend/commit/rollback用真实PG协议和Command锁区分；lookup/read本身Unknown也不能输出观察。默认root的其它后台事务不可偷走故障武装，须精确目标Project/command/phase和唯一backend锁事实匹配。

## 5. 材料安全、I/O预算与实际所有权

材料仅在本次私有DTO/同步SecretMaterial中短暂存在。可复用既有安全 `httpCredentialValue`，但所有新增含value DTO及错误/Format/slog均须审防泄漏。完整strict DTO成功后才创建owned bytes/SecretMaterial；owned临时bytes clear，SecretMaterial在所有返回/panic路径Destroy且恰当同步；不保留request/value到闭包后台/响应/日志。Go JSON decoder、string及runtime内部副本不能声称可可靠擦除；不改公共DecodeJSON或SecretMaterial来制造此承诺。

不得持久化材料请求raw、Cookie、CSRF token、密码、DSN、加密key或Material hash到公开证据。最大材料/异义测试仅公开场景标签、长度、计数/布尔及安全响应；敏感fixture/handoff如必须落盘仅私有0600且cleanup删除。native/proxy trace只捕PID/starttime、socket建立/关闭、长度/阶段/锁摘要；syscall trace固定为安全闭集 `socket,bind,listen,connect,accept,accept4,getsockname,getpeername,shutdown,close`，只记录端点/FD和返回状态。禁止 `%network`、`all`、read/write/send/recv及其向量/消息变体等会记录buffer的调用，不以 `-s 0` 或事后脱敏代替采集前闭集；raw PostgreSQL帧不得进入证据。真实schema只导出安全response原byte，安全source包含method/path/status/真实Content-Type/length/target/run/candidate/schemahash，不含请求头或body。

从handler入口至授权、读body、所有顺序lookup/metadata/Execute、校验编码与Write/Flush采用同一最短ctx：三写 **30s**，GET/HEAD及显式lookup **2s**，更早caller优先；内部Metadata原3s不得覆盖更早HTTP期限。写前置查证共享写的30s，不另嵌2s窗口改变流程。此为发布/I/O预算，原Store事务回收及Body.Close/AfterFunc callback都需同步实际收尾，不宣称整个handler必在30s/2s返回。

在任何认证/业务调用/写头前，有限解析writer的SetReadDeadline/SetWriteDeadline/Flush能力；逐能力保持ResponseController的首接收者/FlushError优先Flusher再Unwrap语义，最大64层、typed nil/环/无能力拒绝，不先Flush提交200。先建安全adapter+budget并安装finish defer，再解析，Unwrap panic也走原Body.Close/abort；拒绝后finish不能重新陷入Unwrap环。能力不可用或deadline设置失败时业务0/发布0，仍拥有原body同步Close。业务writer必须保留 `httpapi.stateOf` 的透明链：认证、DecodeJSON、WriteProblem及业务Write/Header仍走原writer或正确Unwrap至原tracked writer的包装；能力adapter仅供ResponseController使用，若也参与业务链则必须正确Unwrap。不得因屏蔽stateOf丢失code记录或已提交防二写，不能绕过原tracked writer直接发布。不得改已接受Ownerread/Update/本轮read helper。

start、AfterFunc取消callback、finish的abort/正常reset每次调用SetReadDeadline或SetWriteDeadline，都必须各自以私有safe-call独立recover为固定安全error；不得格式化、记录或返回panic值。任一setter报错/panic仍继续另一setter和其余必要收尾；start失败禁止认证/业务，callback失败不能逃逸goroutine且必须并发安全记录失败并触发实际完成通知。finish须继续原Body.Close与已启动callback的实际stop/wait，最后才统一ErrAbortHandler；不得让abort/reset重复触发的panic跳过Close或join。owned Body.Close同样局部recover为安全error，在调用前标记已调用，至多调用一次，panic结束不能跳过后续callback join，也不能伪报正常完成。正常reset仅在callback已停止或实际完成后执行，任一reset失败保留失败并abort，不能宣称连接可安全keepalive。

正常与取消分别实际停止/等待AfterFunc，原Body.Close恰一次，保持原body所有权不重复关闭MaxBytesReader wrapper；正常路径写前先完成Close、再次核ctx，成功后Flush并仅在实际收尾后复位deadline供keepalive。过发布期、write/flush/close/callback/panic失败用ErrAbortHandler终止；禁止迟到200、Problem或部分JSON尾缀，不起无法join的goroutine。HEAD、错误响应同样受deadline与收尾，错误HEAD无body。测试环wrapper只证明新handler接缝，不把旧httpapi.stateOf宣称任意环安全。

## 6. 同一ProjectAuthority的真实根装配

在固定61bed根的 `createProjectUsage` 内新增必要secret.Store断言，先 `secret.NewProjectAuditAuthority(theSameStore)`，再唯一 `project.NewAuthority(...AuditFacts:{audit.SecretProducer:checker})`，将该Authority保存在原projectUsageAssembly，不新增第二份Authority。所有构造纯，无SQL/网络/goroutine/Initialize提前。Project原Lifecycle仍nil，仅SecretProducer新增；Object等保留unbound，不复制第二ProjectAuthority。

`createSecret`明确增加 `projects *project.Authority` 参数，在纯构造中调用一次 `project.NewSecretAuthority(projects)`（该正式构造拒nil/零值Authority），把返回的真实delegate传入 `secret.Authorizations.Projects`；AccountWrites/Sessions/System/Model UsageRouter原实例不变。`createSecurity`保原Projects=同Authority；Secret checker只依赖同Store，不依赖尚未构造SecretService，不后置setter。opaque ProjectFactAuthority无Store getter；不反射内部或声称任意wrapper构造时已验证同实例，真实root同Store及Tx/witness跨Store失败分别实测。

Account bind后新增纯 `projectCredentials` helper构造handler，精确dispatcher接入现有route链；当前Model read完整接受后，保留其Projects实口、五GET/HEAD和实际装配顺序。其它 Usage/Owner Update/System/Summary/Mailcatalog/Outbox producer及Project gate均保持；凭据写只有原Secret Audit，没有新Model或Project Outbox事件。不能拿root已有Outbox作为“凭据也产生Event”声明。

初始化继续原Secret→Models→MeetingSummary→Usage，增加checker不增加Initialize或迁移。Secret仍由原maintenance/根资源链拥有；本卡不造Secret Stop/Drain接口，也不把maintenance已停当HTTP调用已join。新请求在原HTTP server shutdown/Force/DB最后关闭体系中同步拥有；默认root在途Secret writer/慢body与关闭竞态真实验收，forced root返回与未终局backend/privateproxy分别记账，不声称所有inner joined。

共享 `account.go` 当前属于D09 read，不得并写；`project_usage.go/security.go`及相关app测试虽此刻不在该13写表，也必须由root统一正式移交后实施。在移交前只可此scratch规格和固定接受源分析；不借活动handler/helper/根结果作为依赖。移交后重新冻结共同源码/实际图，禁止旧61bed快照覆盖后继已接受read增量。

## 7. 候选实施唯一白名单（22技术+1末件，当前未授权）

| # | 路径 | 必要职责 |
| --- | --- | --- |
| 1 | `internal/central/secret/contract/write_lookup.go` | 仅Project lookup验证分支，旧形状/方法不变 |
| 2 | `internal/central/secret/contract/write_lookup_test.go` | 两scope/identity/ref/version闭合 |
| 3 | `internal/central/secret/write_lookup.go` | scope端口/Project SH/真实同Tx与零候选 |
| 4 | `internal/central/secret/write_lookup_test.go` | 锁/当前授权/故障/旧System兼容 |
| 5 | `internal/central/model/http_project_credentials.go` | 新构造/路由/预算/能力与同步尾部 |
| 6 | `internal/central/model/http_project_credentials_wire.go` | strictpresence/材料请求/安全结果/完整编码 |
| 7 | `internal/central/model/http_project_credentials_test.go` | 受控调用顺序/权限/Unknown/尾部 |
| 8 | `internal/central/model/http_project_credentials_wire_test.go` | 严格矩阵/最大值/schema/保密 |
| 9 | `internal/central/model/http_project_credentials_native_test.go` | 三native组 |
| 10 | `api/openapi/project-model-credentials.json` | 六operations/闭合安全对象 |
| 11 | `internal/central/app/project_credentials.go` | 真实handler和精确root路由 |
| 12 | `internal/central/app/project_credentials_test.go` | 构造无I/O/缺依赖/唯一实例/路由 |
| 13 | `internal/central/app/account.go` | 交接后的实口/handler接线 |
| 14 | `internal/central/app/project_usage.go` | 同Store Secret checker→唯一Authority→delegate |
| 15 | `internal/central/app/project_usage_test.go` | 新构造依赖及原Usage兼容 |
| 16 | `internal/central/app/security.go` | Secret.Projects真实绑定 |
| 17 | `internal/central/app/model_test.go` | createSecret签名及原System根构造适配 |
| 18 | `tests/model/project_credentials_http_fixture_test.go` | 正式身份/真实Secret-Audit-Project/私有安全proxy与schema辅助 |
| 19 | `tests/model/project_credentials_http_test.go` | 完整CRUD/安全结果/原key历史/最大材料 |
| 20 | `tests/model/project_credentials_http_authority_test.go` | 当前授权/Project passive lookup/真锁/坏事实原子性 |
| 21 | `tests/model/project_credentials_http_unknown_test.go` | 真实原writer commit/rollback/pending/取消 |
| 22 | `tests/model/project_credentials_http_root_test.go` | 默认app.Run与既有路由、真实关闭 |
| 23 | `docs/development/backend/README.md` | 技术独立接受后root另授末件 |

13个新技术文件在规格冻结时核不存在；9个旧技术文件来源逐hash固定。此表不是实施授权。既有Secret write.go/service.go/project_audit.go、Project Authority/SecretAuthority、旧SystemHTTP/公开httpapi、当前D09 read13、共享fixture/helper、迁移、脚本均不在写入范围。发现普通适配超出表先报告root，不借白名单推定可扩公共语义。

## 8. 最小完整验收、图与资源门槛

先冻结22技术源码/schema，再实际 import/embed/TestMain/运行时schema读文件图；选中Secret/contract、Model、app普通/race，integration tests/model/project/security及原fixture实际包链，两cmd/TestMain动态build、CGO0 outbound server变体与Python模块/20官方schema JSON数据。只最小补实际图，不复制全仓；当前活动read尚未停写/接受时不得用其传递包编译。固定Go1.27.1/offline/modreadonly/p1/cache，单条45s、compile/run分开、真实subreaper/direct/adopted wait与两次owned空；app只精确列出的pure，禁止全app隐起native。

身份沿正式Bootstrap/Login及Invitation/Redeem/Login；Project可用正式Create+已验持久Skills测试fixture，明确不是生产初始化。archived/Owner变更等特殊状态可在正式锁内作合法canonical测试输入，不冒生命周期/转移命令已交付。Model引用通过既有正式配置库建立；无真实生产lease consumer时用已验Secret契约的严格测试事实，不能alwaysallow。

| 验收组合 | 必须实际证明 |
| --- | --- |
| strict/保密/完整值 | 三body cap与+1、65536B最坏转义材料实际编码并传完整末byte、65537 decoded拒绝；同key完整材料重放及只改最后byte冲突；wire key/null/version/Purpose/scope/query矩阵；安全Fmt/JSON/slog/错误/body证据不含材料。GET/HEAD安全body原byte标准schema及长度一致。 |
| CRUD与原授权 | create→metadata→rotate→delete，三种历史lookup及删后材料重放；同User新Session、他人Owner/admin/跨scope/currentSession撤销；archived committed历史的passive Read成功与每种mutation/replay仍正式Mutate拒绝互补；lookup不能替代Execute的材料检查。 |
| 查证真实锁/终局 | 精确Command/User/Project SH同Tx、当前Session/Owner在读取receipt前；缺锁/typednil/crossStore、不合法receipt/当前权限变化拒绝；读事务present/absent Unknown/NotCommitted/提交后取消零observation；原writer EX阻塞时不能假absent。 |
| Audit/原子引用 | 真Secret→Project gate→Secret checker完整witness；修改合法Entry actor/session/cause/changed_fields或错误Tx到达真实validator被拒，canonical/receipt/Audit一起回滚；typed constructor拒绝不能替代validator证据；引用/活lease删除拒绝与释放后删除、旋转原语义保持。nonce准备预留单独记，不假称失败全部表无变化。 |
| 物理Unknown | Prepare阶段与最终Apply阶段区分；最终精确Project/command事实和唯一backend锁后arm，COMMIT已server terminal丢ACK、ROLLBACK、held-writer pending分别核。HTTPUnknown只单次Execute、零候选；另一个原keylookup等待真实writer终局并重新授权，撤权无泄漏。无原帧/凭据落证据。 |
| native/关闭 | keepalive成功deadline复位；自然写30s/显式lookup2s及更早parent覆盖慢body/写/Flush；缺能力业务0、Unwrap panic/环有界；受控逐次注入start/callback/abort/reset的read/write setter panic，以及owned Close panic，证明另一setter、Close至多一次与callback实际join仍执行，无panic逃逸/晚写；callback或Close阻塞时不得先报完成，放行实际join。用正式tracked writer核Problem code记录及committed后二写被拒，能力adapter不遮蔽stateOf；默认root在途writer/取消正常及合法耗尽shutdown，不用idle stop替代。 |
| 默认root兼容 | 不注入handler/Store成功假口；真实app.Run全部六operation及当前read五GET/HEAD、原Owner GET/PATCH/Usage/System凭据/Summary兼容，安全模型配置只消费CredentialRef；Secret Audit有且仅一次，重放不增；无新Project/ModelEvent、ready503与未绑定保持。 |

新PG顶层固定5个：`TestModelProjectCredentialHTTPCRUDAndHistory`、`TestModelProjectCredentialHTTPCurrentAuthorityAndFacts`、`TestModelProjectCredentialHTTPPassiveLookup`、`TestModelProjectCredentialHTTPUnknown`、`TestModelProjectCredentialHTTPDefaultRoot`。native精确3个：`TestModelProjectCredentialNativeKeepalive`、`TestModelProjectCredentialNativeBodyDeadline`、`TestModelProjectCredentialNativeWriteAndClose`；纯自然预算代表显式Pure命名并事先精确list，不在普通短组意外超过45s。

旧必要PG集合固定：`TestProjectSecretAuditBindingCRUD`、`TestProjectSecretAuditBindingCurrentAuthority`、`TestProjectSecretAuditBindingFactRejection`、`TestProjectSecretAuditBindingUnknown`、`TestProjectSecretMetadataReadCompatibility`（tests/project）；`TestModelSystemHTTPCredentialWritesAndBinding`、`TestModelSystemHTTPPassiveCredentialLookup`、`TestModelSystemHTTPPassiveLookupLocksAndFailures`、`TestModelSystemHTTPUnknownAndHistoricalResults`、`TestModelProjectSecretReferencesAndAtomicEffects`、`TestModelProjectOwnerUpdateHTTPRootBinding`、`TestModelMeetingSummarySettingsRoot`、`TestModelProjectUsageHTTPRootBinding`（tests/model）。当前read产品接受后另固定其默认root精确top，未接受前不引用活动helper/断言；旧System/Project库同源证据可复用适用未变场景，不把SKIP/no-tests/编译当行为通过。

native每轮≥2GiB，test45s/干预55s和额外退役分开，生命周期trace命令须逐项冻结§5安全syscall闭集，独审核原argv及实际raw不含buffer、材料或协议帧，不使用 `%network`/`all`；真实TCP含TIME_WAIT/PID-starttime双空，actualwait。PG每轮≥5GiB，新top120s**含Cleanup**、包6m、race/count1/p1，实时-v watchdog实际逐top计时；完整原fixture链、7exact资源/全Mount/nonce，实际direct/adopted wait及owned/runtime双清。启动前新环境baseline/固定工具与两local镜像digest检查，缺失不默拉最新。任何失败先实际清理、停后继、保原raw，不自动重跑；root明确授每个独占窗，预算不足先分轮。daemon/PID1 Z按实际集合差单列非owned未wait，不称全机清零。

独立验收先核完整停写候选，A至少重组当前权限/archived双语义、Project lookup锁/原子Audit及真实Unknown代表；B默认root、真实安全response原byte+Draft202012/FormatChecker与材料日志负证据、在途关闭。普通格式/schema只读检查不计Go/资源通过。技术22完整独立接受后才另授README，完整23由root最终采纳/提交；本scratch无产品完成声明。
