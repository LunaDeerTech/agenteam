# D09 Project Owner Provider/Model 配置写入 HTTP 与默认根组合

修订 rev1，2026-10-07。**scratch 完整工程规格待独立 STATIC；未正式归位，未授权产品、Go 或资源。** root 已采纳 [完整结果建议](../project-model-configuration-http-recommendation01/recommendation.md) 的方向和工程界。当前固定产品输入为 Model Owner read `a0b012ce`、其归档 `6bd11cda`；凭据仅接受[规格 rev2](../project-model-configuration-http-recommendation01/fixed/docs/development/work-items/d09-project-model-credentials-http.md) `9a2a9a1a` / 归档 `b498c0bc`。凭据活动 #1–22 实施不是已验依赖，本稿不读取其活动内容。

本卡正式实施须等凭据完整产品（含 README）独立接受并经 root 采纳、固定实际提交和共享根依赖移交；随后针对该新基线重冻本卡唯一文件表/实际图。不得用当前 `a0b012ce` 根覆盖凭据后继增量。本稿只定义完整结果，不授任何技术路径。

## 1. 结果、依据和精确依赖

当前 Human Owner 通过默认 HTTP 原子管理本 Project chat Provider/Model，返回既有配置命令的历史安全 receipt，并可另发显式请求查证原命令；创建凭据、配置 Provider/Model、读取配置仍是独立命令和请求。默认根沿同 Account Store、唯一 Project Authority、已构造 Model/Secret/Audit/Outbox；不生成默认 Provider/Model、凭据或项目覆盖 selector。

| 前置 | 已验能力或接入要求 |
| --- | --- |
| [Project 配置库及独立验收](../project-model-configuration-http-recommendation01/fixed/docs/development/agent-team/d09-project-configuration-verification.md)，`de00c610da62cb77cc03efe7c3cc842cf81f1ba5` | 六 CRUD、五查询、Project LookupCommand、当前权限与同 Tx Secret 引用/Model Audit/Event/receipt、历史与物理 Unknown。按当前接受源保持，不复制历史库实现 |
| [read 完整接受](../project-model-configuration-http-recommendation01/fixed/docs/development/agent-team/project-model-owner-read-http-verification.md)，`a0b012ce` / `6bd11cda` | 五 GET/HEAD、2s 实际 I/O、8MiB 完整表示界、真实 Model.Authorizations.Projects/默认根；原首红和组合验收边界保留 |
| [凭据接受规格](../project-model-configuration-http-recommendation01/fixed/docs/development/work-items/d09-project-model-credentials-http.md) | 规定 Secret Project lookup、同 Store Secret checker→唯一 Project Authority→Secret.Projects、凭据六 HTTP operations 和真实根。**产品尚未接受**；本卡不自己复制/补造这些实现，开工必须固定其最终完整接受结果 |
| 原 Account/Project/Secret/Model/Audit/Outbox | Cookie Session+CSRF、当前 Human Owner/gate、原 Secret Purpose=model reference 计划与 fact、typed Model Audit、Model producer、Project 两阶段 Event gate 均沿正式同 Store/Tx 端口 |

业务含义引用 [Model Configuration](../project-model-configuration-http-recommendation01/fixed/docs/architecture/platform-infrastructure/model-system/model-configuration.md) 与[原 Project 配置卡](../project-model-configuration-http-recommendation01/fixed/docs/development/work-items/recovery-d09-project-configuration.md)。后者有关 Summary 未决/根未绑定的页末是当时历史，不重开已确认的共享 System Meeting Summary 决策。实施读仓库 AGENTS/团队流程与 Go 技能，独立读 verification 技能；本规格按 design 技能形成，治理规则不复制为新接口。

范围为六配置 mutation、一个原命令 lookup、默认根组合及异常验收。排除凭据材料 mutation 实现、Agent/approval_model canonical 引用替换、Project selector、删除预览、UI、新 Resolver/Invocation/Provider 调用。Production Resolution/Invocations 与 D24 继续未绑定，`ready=false` / readyz503、整个 D09/D08–D28/E01 未完成和三项停止边界保持。无迁移、依赖/锁版本、worker、初始化阶段或共享脚本改动。

## 2. 正式构造、HTTP 资源和根分派

新增 `model.NewProjectConfigurationHTTPHandler(core *model.Service, boundary *account.HTTPBoundary) (http.Handler,error)`。在 model 包签名中 core 实际为 `*Service`；校验非 nil/有效 state、原 Store/Authority 同实例、原 Sessions/System/Projects 及 Secret/Audit/Events/Cursors/ConfigurationEvents 有效，沿已验 read 构造的检查能力，不新加公共 Store getter。全部捕获一次，不能 setter 替换；构造无 SQL、I/O、goroutine、Initialize。私有七方法 interface 与私有 boundary interface 可用于受控测试，不允许公开 caller grant 或生产 allow。

公开 `model.HandlesProjectConfigurationHTTPPath(path string) bool` 只识别下表四种共享 collection/detail 形状和 lookup，所有方法都让所属资源到达边界；ID解析留 handler，非法ID形状不能落进其它 handler。它不匹配 available-chat-models、凭据、Owner、Usage 或泛 project prefix。原 `NewProjectHTTPHandler` / `HandlesProjectHTTPPath` 及五读自身不改。

| 方法和路径 | 严格 body 字段 | Service 方法 / 成功 kind |
| --- | --- | --- |
| POST `/api/v1/projects/{project_id}/model-providers` | `input` | CreateProvider / `provider.create` |
| PUT `/api/v1/projects/{project_id}/model-providers/{provider_id}` | `expected_version`, `input` | UpdateProvider / `provider.update` |
| DELETE 同 Provider detail | `expected_version` | DeleteProvider / `provider.delete` |
| POST `/api/v1/projects/{project_id}/models` | `provider_id`, `input` | CreateModel / `model.create` |
| PUT `/api/v1/projects/{project_id}/models/{model_id}` | `expected_version`, `input` | UpdateModel / `model.update` |
| DELETE 同 Model detail | `expected_version`, `replacement` | DeleteModel / `model.delete` |
| POST `/api/v1/projects/{project_id}/model-commands/lookup` | `command` 恰上列六 kind 之一 | LookupCommand / found+receipt |

没有 HTTP PATCH、选择器/credential lookup混用或新命令名；不修改七个 Service 正式签名。配置 lookup 不接受 target/expected/input/semantic，不能从客户端接受私有确认权限。

默认根在既有 `app/project_models.go` 的 `projectModelsHandler` 内，以同 boundary、同 Model Service 构造旧 read 和新 configuration handler 的组合；`projectModelsRoutes` 扩 lookup 形状且只分派一次。四种共享资源 GET/HEAD 直达原 read；其它方法由新 handler 作合法写或405，绝不把GET/HEAD送入写管线。collection Allow 为 `GET, HEAD, POST`；detail 为 `DELETE, GET, HEAD, PUT`；lookup 为 `POST`；available-chat-models仍旧 `GET, HEAD`。新 handler 单独收到共享 GET/HEAD 时不代替 read查询，可405声明完整组合 Allow；公开完整 GET/HEAD 行为由上述组合保证。lookup GET/HEAD/其它方法405，HEAD错误实体0。OPTIONS也按原边界和405处理，不新开CORS。

原认证、安全头和 RequestID middleware 保留唯一实例。先有限 I/O 能力预检及预算所有权，再 CheckRequest、RequireHuman（所有七操作的POST/PUT/DELETE均CSRF），然后资源/方法/typed输入。canonical 未知资源仍404；RawPath、清理/尾斜杠、反斜杠等按原 CheckRequest 拒绝，不自动重定向。非法路径在所属边界内校验；不改变现有 unknown route 的边界。不得用泛 prefix 抢走凭据、Owner Update、Usage、System、Summary。

本设计预期**仅改 app/project_models.go 与其测试，无需改 account.go/security.go/project_usage.go**。这三根在凭据接受后作为实际只读依赖固定，不能假定其旧 hash 仍是执行输入。若凭据最终根或实际编译证明需要扩大范围，先暂停受影响处向 root 提最小差量，不自动取得三根写权。

## 3. 严格配置输入、Project-only 投影和请求界

所有七请求 body 为单个 UTF-8 JSON 对象，`Content-Type: application/json`（允许原 charset=utf-8），拒 unsupported Content-Encoding、重复/未知头规则沿原 decoder；全层重复键含转义后相同键、未知字段、缺字段、错误 null、尾部第二值、深度>64、坏UTF8拒绝。动态原配置对象仍走原 strict scanner，不因 RawMessage 跳过重复键或损坏JSON。所有 query / RawQuery / ForceQuery 均拒绝。

raw body 最大 **1MiB=1048576 B**，沿 `httpapi.DecodeJSON` 原默认界，七operation一致，不新增更小DELETE/lookup界。只用有界读取；精确 cap 可含合法空白、cap+1 必须原 PayloadTooLarge/413且业务0。拒绝后也实际关闭原body并完成取消callback。该界不声称库所有合法输入都必可通过HTTP、也不控制库/DB前序或全进程RSS；不裁剪字段或数组以换成功。

恰一个 `Idempotency-Key` header，沿原1–128字节字符规则；不trim、不合并、不自动生成，既不入URL也不回显/普通日志。ProjectID/ProviderID/ModelID/CredentialID均canonical UUIDv7，使用正式typed parser。Actor只由boundary得到；ProjectScope只从path正式构造，body不得提供actor/scope/project_id/session/key。

| 输入对象 | 必填且精确的字段与 nullable 规则 |
| --- | --- |
| Provider input | `name` string、`protocol` 原协议枚举、`base_url` string、`enabled` bool、`credential_ref` 必填null或CredentialID string、`options` JSON object；不能以缺失代null/false/空对象 |
| Model input | `name`、`provider_model_id`、`type`、`enabled`、`parameters` object、`request_overwrite` object、`header_overwrite` string-map、`capabilities` object，全部必填且不能null；provider_id仅Create在外层提供 |
| capabilities | `tool_calls/parallel_tool_calls/streaming/reasoning` bool，`input_modalities/output_modalities/reasoning_efforts/structured_output_modes` 必填非null数组，`context_length/max_output` 必填null或原正int64十进制字符串；布尔false与空数组合法、缺席不合法 |
| update/delete expected_version | 必填正int64 canonical十进制字符串，范围1..9223372036854775807；非字符串、null、前导零、指数、符号和溢出拒绝 |
| DeleteModel replacement | 必填null或合法ModelID，不可等于自身；缺字段不是null，遵守原请求Validate |

name≤128、base_url原endpoint规则/≤8192、provider_model_id≤256，64KiB options/parameters/request overwrite、16KiB header map及原组合界沿 `contract/configuration.go`；不放宽URL userinfo/query/fragment、头覆写敏感字段或模型保留字段。通用 Capabilities 保原枚举、unique、每 reasoning token≤32 与关联验证，不发明小的reasoning数组总数上限。

**先验投影失败不查证。** strict decode成功后，私有Provider转换必须以**path的ProjectScope**执行 `secret.NewCredentialRef`；不能调用固定 `httpProviderInput.input()` 的SystemScope转换再修字段。Model/Provider私有转换保持每个输入语义，不删未知字段、不改变nil/false/数组、版本或配置bytes意义。先验证完整typed request，包括Project chat、CredentialRef scope以及target/provider/replacement和capabilities；任何decode/转换/typed Validate失败直接InvalidArgument或原明确decoder Fault，**七个库方法（含Lookup）均0调用**。若内部受控投影本身损坏也直接拒绝，不准查历史receipt盖掉。仅原库负责业务policy、当前状态/version、Secret purpose、完整事务授权与历史顺序；不得在HTTP预读当前配置或先Mutate来替代。

Provider protocol目前仅支持chat的原两种（openai-chat-completions、anthropic-messages），Model type固定chat；provider scope/protocol、Model provider/type不可变的原库判断保留。原policy拒绝非空Provider options、Model parameters/request_overwrite/header_overwrite和非空reasoning_efforts，错误仍原CapabilityUnsupported，不能因DTO省略或“未来兼容”使其成功。通用typed合法而policy拒绝，与typed坏输入的业务0门槛分开。配置描述不证明真实外部Provider、网络或型号可用。

## 4. 原配置命令、当前权限、并发和查证

只调用既有六正式方法一次，不在HTTP增prepare/retry/lookup/GET预检流程。Meta为正式Human Actor、path ProjectScope、原key；库namespace固定 `model.project`，owners精确 `[ProjectID, stable Human UserID]`，命令名/完整 semantic 沿库，Session/trace不进semantic。同User新有效Session可同义重放，跨user/Project不能串行；异义同key原KeyReused。Create的稳定ID由原首次计划生成，不由HTTP预造。

[commands.go](../project-model-configuration-http-recommendation01/fixed/internal/central/model/commands.go) 原顺序逐层保持：当前Read/gate→原receipt与semantic→首次Mutate/version/dependencies；正式事务一次完整union Acquire后再次当前Read→receipt→首次Mutate→mapping重核和全部事实，不能补Acquire掩盖漏锁。HTTP只RequireHuman，无额外Owner Mutate预检。准备失败的同key并发receipt复查保持，不能把过时版本/依赖错误固定在已提交同义receipt之前。

| 当前事实 | lookup / 同义六命令历史重放 | 无receipt首次新写 |
| --- | --- | --- |
| active+initialized+当前Session/Owner | 原当前Read后返回原历史；实际mutation仍由库决定 | 正式Mutate、version/依赖/锁后可提交 |
| archiving/archived+当前Session/Owner | 可Read查证及返回精确原receipt，包括资源删后/旧expected | 原ProjectNotActive，零新配置业务事实 |
| 删除中/未初始化/删后、失去Owner或撤Session | 原正式gate/授权错误，零成功receipt | 同样拒绝，管理员无跨Owner豁免 |
| System/其它Project资源ID | 不能跨scope泄露配置/历史 | 原scope查询NotFound等正式错误，不以裸ID绕过 |

六动作业务字段/版本不扩展：Create version=1；首次Update/Delete version=expected+1，原MaxInt64→nextVersion为InvalidState，不把HTTP输入上界缩成凭据卡max−1；未改变有效配置的首次Update仍原InvalidArgument，不造no-op成功。历史同义receipt先于当前version/依赖；返回的是历史version，不查当前GET替换。

配置行、command/原持久plan、Secret Retain/ReleaseReferenceUsage、typed Model Audit、真实Model configuration event和safe receipt全在原一笔Tx。Secret引用必同Project、Purpose=model；wrong purpose/scope按原分层错误拒绝；引用增加/替换/释放和Secret删除竞争沿原正式锁、fact、权限，无直接改Secret表。Provider有任意Model原InvalidState；删除空Provider仅释放引用，不删除Secret材料/lease。删除Model先EX完整引用重扫；Project有任何reference时原DependencyUnbound，不能只删index、跳过引用或以fake canonical owner替换。无引用且replacement为enabled本Project/System chat+enabled Provider可按原库成功，affected_references=0；其它Project不可见。Agent/approval_model rewrite和新的引用所有者均不在本卡。

**保留库内私有Unknown确认。** 六方法的 `runCommand` 原Unknown分支在**原ctx**下用原Command EX等待writer终局、当前Read、原完整expected semantic和真实canonical committed receipt。确认成功可返回原receipt；absent/仍pending/确认失败保持原Unknown cause/attempt，异义KeyReused保原错误。没有新WithoutCancel或额外确认宽限；与Secret“库不自动确认”的语义不同。handler不自动重试、lookup追认、换key或转换其他请求结果，ctx耗尽后即使随后拿到receipt也不发布迟到成功。

**公开lookup只被动观察。** 调用 `LookupCommandRequest{Meta,Command}`，六kind闭集；HTTP不接受model.selection.update，返回InvalidArgument/业务0，不把未绑定Project selector开放出来。正式库沿 `readScope` 一次事务/完整锁、当前Owner Read，再查原identity；其中 `commandLock` 现为**EX**，还有User/Project及全局model-references SH，保持实际实现，不抄凭据lookup的SH。held原writer时lookup等待；2s耗尽不是found=false。Committed读+ctx有效才允许输出，读Unknown/NotCommitted/error零观察。found=false不证明先前Unknown回滚，found=true不验证另一个写请求input/expected semantic；只有重新提交原完整写请求才能触发原同义校验，仍受当前授权。

## 5. 安全输出、错误优先与新私有分类

成功统一200。配置结果严格为四字段 `kind/resource_id/version/affected_references`；kind只六动作，resource_id canonicalUUIDv7，version正int64字符串，affected_references非负int64字符串。create绑定kind且version=1；update/delete绑定原请求kind、目标ID和expected+1（仅在成功结果投影阶段安全检查overflow），非model.delete的affected_references必须0。当前Project缺rewrite adapter决定合法delete成功也为0；不可声称改写未来consumer。

lookup严格两种：`{"found":false,"receipt":null}`，或 `{"found":true,"receipt":<完整四字段>}`；found/receipt同时必填，错union拒绝。命中receipt.kind必须匹配请求command；只按receipt自身合法性验证ID/version/计数，不从未提供的target/input发明绑定，不能要求当前资源还存在。HTTP不直接序列化any/contract未来字段，不返回完整配置/Scope/Actor/Session/key/semantic/材料。

顺序固定：先完整输入typed投影→唯一Service调用→**error优先**→ctx检查→闭合结果验证/安全DTO→完整编码/长度界→实际发布。即使受控服务同时返看似合法receipt和error，也保原error，零receipt/lookup候选、不验证候选来替换error。原Fault/Unknown包装必须保Cause/Attempt可errors.As比较；wire只原公共Problem字段，不新造attempt/cause header或字段。

| 来源 | 新HTTP处理 |
| --- | --- |
| malformed输入/typed投影 | Service和lookup均0调用，原输入Fault或InvalidArgument/NotStarted |
| 服务有error（包括Unknown或typed cause） | 原error优先，ctx仍有效时正式boundary安全Problem；不能被receipt、限长或后续查询盖掉 |
| **六写服务nilerror但receipt损坏/错绑定/成功表示不可提供** | 本卡新私有 `DependencyUnavailable` + **Unknown** 安全分类，零成功表示；调用可能已写入，不声称未开始/回滚，不附不存在的cause/attempt，不重执行或自动lookup |
| 显式lookup nilerror但坏union/错kind或损坏receipt | 私有DependencyUnavailable/NotStarted，表示本次观察无法发布，不断言原写不存在/未提交；零观察，不自动换当前GET |
| ctx/写/Flush/Close/callback尾部失败或已过发布期 | ErrAbortHandler；不补迟到Problem/200/部分JSON尾缀 |

写后坏receipt的Unknown是**本卡显式新增的HTTP结果分类**，不是旧System HTTP或`model/store.go:29–30`（其fault/unavailable默认NotStarted）的既有行为；只在本卡私有helper使用，不能改共享helper/库/System/旧read。它没有原物理attempt/cause，验收不得把此分类当真实COMMIT Unknown证据。原服务Unknown另按原cause/attempt保真验证。

安全成功表示最大 **1KiB=1024 B**（不改变公共Problem schema界）。固定kind/UUID/十进制字符串闭合后再编码，最长合法四字段及lookup外壳在纯测试实际编码证≤1KiB，不凭平均样本；任何错typed值/unknown field不能先Marshal任意大payload。原配置input受1MiB读界，后续DTO转换只拷贝其有界部分；这里的receipt界不替代旧read完整8MiB计数，也不宣称全RSS上界。

发布使用application/json、no-store、既有安全头、精确Content-Length与唯一RequestID。错误HEAD实体0且拥有相同Close/Flush尾部；不会开放新成功HEAD查询。原五GET/HEAD继续旧完整校验/8MiB/2s。普通日志、fmt/slog、错误与panic值不可泄漏配置输入、凭据材料、key、Cookie/CSRF、密码/DSN/keyring；新增含配置DTO提供安全格式化，不把JSON请求原bytes或配置摘要当公开证据。

## 6. 预算、实际尾部和默认根关闭

六写入口起**30s**，显式lookup入口起**2s**；不支持的方法/未知所属资源采用2s拒绝预算；更早parent优先。覆盖认证、body、唯一Service及其内部原确认、结果验证编码、实际Write/Flush。原事务回收/Close/AfterFunc收尾须同步真实完成，不宣称整个handler硬在预算内返回，也不加隐式新的确认时间。原五读ctx规则不改。

任何认证/业务/写头前，有限解析可达SetReadDeadline、SetWriteDeadline、Flush能力，逐能力遵循ResponseController首接收者/FlushError优先Flusher再Unwrap，最大64层、typed nil/环/缺能力拒绝，不提前Flush。先建budget与安全adapter并安装finish defer，再解析；Unwrap panic也进入原body实际Close/abort，finish不再陷回动态Unwrap。adapter只供controller；业务Header/Write/Decode/Problem仍走原tracked writer透明链，不能屏蔽stateOf、code日志或已提交后二写保护。

start、取消callback、abort、正常reset每一次read/write deadline setter分别私有recover为固定安全error，禁止格式化panic值；一项失败不跳另一setter和其余收尾，callback失败不逃逸goroutine。owned原Body.Close在所有返回/panic路径至多一次，调用前标记、局部recover；Close阻塞需真等其完成，不用另起不可join goroutine逃逸。Close panic仍继续已启动callback实际stop/wait，最后统一abort；MaxBytesReader包装不重复关闭原body。

正常业务返回后Close原body、再次核ctx，完整成功/安全错误发布后Flush；AfterFunc stop或真实join后才reset供keepalive。超时/非合作服务返回晚、Close/callback阻塞、部分写、Flush错误或任何setter/reset错误不能产生成功尾缀或晚Problem。原取消原因保留，socket断开/context取消/handlerentered都不是服务/Close/callback已结束的证明。

默认根不新造Model/Secret/ProjectAuthority，服务已共享正式Store/Audit/Event/UsageRouter；构造不提前Initialize。原Secret→Model→MeetingSummary→Usage顺序、Account/Project Update Drain、HTTP shutdown/Force与DB最后关闭链保持。新同步Model调用由原HTTP server所有权收尾；正常shutdown实际等待调用/子进程/端口，耗尽forced-root返回与仍未终局backend/privateproxy分开记录并随后实际释放，不把旧Object inner-join限制宣称修复。

## 7. 候选唯一白名单（13技术＋1末件；本稿不授实施）

| # | 路径 | 唯一允许差量 |
| --- | --- | --- |
| 1 | 新 `internal/central/model/http_project_configuration.go` | 七operation、边界/预算/I/O所有权、方法资源匹配 |
| 2 | 新 `internal/central/model/http_project_configuration_wire.go` | Project-only严格输入、安全receipt/lookup、私有错误分类 |
| 3 | 新 `internal/central/model/http_project_configuration_test.go` | 单调用、输入坏投影/error优先、恢复边界、真实尾部纯测 |
| 4 | 新 `internal/central/model/http_project_configuration_wire_test.go` | 完整wire/presence/限长/最长receipt/schema |
| 5 | 新 `internal/central/model/http_project_configuration_native_test.go` | 三精确native真实socket/预算/关闭代表 |
| 6 | `api/openapi/project-models.json` | 追加六mutation+lookup及Project私有闭合schema，五GETHEAD不漂移 |
| 7 | `internal/central/app/project_models.go` | 同Service/boundary构造读写组合、精确路由/完整Allow |
| 8 | `internal/central/app/project_models_test.go` | 构造无I/O/缺依赖、逐方法单分派、旧链兼容 |
| 9 | 新 `tests/model/project_configuration_write_http_fixture_test.go` | 正式身份/真实服务/安全body+schema/私有proxy与actualjoin |
| 10 | 新 `tests/model/project_configuration_write_http_test.go` | 六CRUD/history/严格wire与Secret引用组合 |
| 11 | 新 `tests/model/project_configuration_write_http_authority_test.go` | 当前授权/归档重放/原子fact/缺adapter/实际锁 |
| 12 | 新 `tests/model/project_configuration_write_http_terminal_test.go` | 物理Unknown、私有确认与公开lookup、终局/并发 |
| 13 | 新 `tests/model/project_configuration_write_http_root_test.go` | 默认root业务组合及独立关闭top |
| 14 | `docs/development/backend/README.md` | 完整13技术独审/root采纳后另授末件 |

十个候选新技术文件在本稿静态冻结时核不存在；四个旧路径固定接受snapshot/hash见 [候选清单](candidate-paths-rev1.json) 和 [输入清单](inputs-rev1.json)，未来凭据README变化必须从其已验最新版本增量，不能还原这里旧README。所有model库/contract/Secret生产、旧System/read/Update handler、旧fixture/shared helper、三根、迁移/依赖/脚本、入口/台账不在写范围。必要旧测试计数/签名出现真实差量时先报root窄授，不用本表推定可改。

## 8. 离线、pure、native与真实fixture验收

### 8.1 固定图、离线执行与精确入口

先冻结13源、继承凭据最终实际graph+read有效图的最小文件hashdelta；包括普通/race Go输入、embed、test-only、20包TestMain、实际Central/Runner两cmd构建、outbound server CGO0及runtime schema/脚本/工具，不读活动输入为固定结论。固定Go1.27.1/绝对Python、GOTOOLCHAIN=local、GOPROXY/GOSUMDB=off、GOFLAGS=-mod=readonly及正确GOMODCACHE；不改版本或复制全cache/依赖树。漏输入先停、补最小freeze；每命令外围45s、subreaper实际direct/adoptedwait和owned双空、前后输入/fileset同。仅在root授权离线窗执行graph→受影响compile/vet→精确list→pure/race/schema→两cmd检查；编译/no-tests/SKIP不算行为通过，禁止普通全app误跑init/native/TestMain资源。

新pure顶层固定：model包 `TestProjectModelConfigurationHTTPPureInputAndDispatch`、`TestProjectModelConfigurationHTTPPureResultBoundary`、`TestProjectModelConfigurationHTTPPureActualTail`（#3）、`TestProjectModelConfigurationHTTPPureWire`（#4）；app包 `TestProjectModelConfigurationHTTPComposition`（#8）。普通/race精确selector、40s test/45s执行；其中不可放30s自然native等待。核全部表格字段/方法/最大raw+cap+1、nullable和typed损坏投影业务0，服务带候选error优先，写后坏receipt的新Unknown无伪cause，lookup错union/command、MaxInt64仍可入库且原错误优先；正式middleware state.code/已提交防二写、setter/Close/Unwrap各panic点和真实Close/callback阻塞尾部。所有异步所有者创建即登记幂等release+实际join cleanup，进入同步有界，前置Fatal也可回收且不新加超预算长等。

新wire全部成功形状、found false/true及Problem经Draft202012/FormatChecker；旧五读schema解释保持原bytes。解析器/refs用固定解释器，不安装版本。新样本最多包含安全receipt字段；输入配置与材料不导出。

### 8.2 三个native顶层

`TestProjectModelConfigurationNativeKeepalive` → `TestProjectModelConfigurationNativeBodyDeadline` → `TestProjectModelConfigurationNativeWriteAndClose`，仅 `AGENTEAM_PROJECT_MODEL_CONFIGURATION_NATIVE=1` 开关下执行且普通离线selector排除。每组40s test/45s执行，fresh≥2GiB；owned cleanup15s、所有TCP含TIME_WAIT退役75s只是清理观察，不作业务预算。前组实际PASS、direct/adoptedwait、ownedPID+全TCP两次空、输入同才推进。失败先清尾停止，不自动retry或无证据调长parent。

keepalive核完整30s写/2slookup成功后的reset、下一请求deadline不继承；BodyDeadline至少一个六写自然30s慢body、显式lookup自然2s及更早parent（同top串行总量需在40s内，不能放两次30s）；WriteAndClose核真正Write/Flush entry/exit/actualn/真实net timeout、partial/close/panic/noncooperative服务实际尾部与错误HEAD。自然30s写超时最多一个，2s lookup Flush代表可组合，其余受控panic/阻塞边界在pure完成，不把“进入handler”当Write已开始。响应小也不能跳过编码或伪造errno，使用已接受真实socket方法并在采集前限制trace。关闭/回调归属和socket退役证据与受控writer结论分列。

### 8.3 五个新真实PG顶层

每新top **120s含Cleanup**，原integration包 **6m**、race/count=1；完整fixture4容器3网络、两个固定本地PG digest及原MinIO SHA/version。PG17固定 `pgvector/pgvector@sha256:99a149d3c84cfb0f32d8da7d72737e4643468787220af2223418730f8e9e9cdc`，PG16固定 `pgvector/pgvector@sha256:16e62164a405447dca191079a924ee5b8a9dbf04fe53128701ffbea857b37782`（原不支持版本fixture，不计第二份业务通过）；MinIO `RELEASE.2025-10-15T17-29-55Z` / SHA `dc5298474f0bc87a068f0b1135c583bb1278c17c11c512212ed7644a238c89c8` 由任务自有binary通过AGENTEAM_MINIO_BINARY注入，不改helper。每轮使用任务空Docker配置检查精确本地digest；缺失或不符先停，不默拉tag或来源。fresh每轮≥5GiB，首次及每轮live基线，不复用旧208/240或重启前基线。命令真实actualwait、所有ownedPID和7exact资源双清、前后输入同才下一组；不以ctx结束或daemon列表无新活进程替代wait。包预算不足分轮，不能缩fixture/放宽断言或延长。每轮daemon/PID1 shim及其它非owned差量单列未wait，不称全机零。

| 精确top | 必须完成的真实断言 |
| --- | --- |
| `TestModelProjectConfigurationWriteHTTPCRUDAndHistory` (#10) | 正式凭据create→Provider/Model六CRUD→当前read/目录→删后history；六safe receipt，Secret引用增换减、Secret仍存，原key同义/异义和新Session、首次无变化Update/MaxInt64拒绝；输入/policy分层错误不泄漏、所有成功schema |
| `TestModelProjectConfigurationWriteHTTPCurrentAuthorityAndFacts` (#11) | Owner/admin/跨Project/System、撤Session/Owner/gate；archiving/archived六history及lookup Read成功而新写拒绝；Secret/Audit/Outbox/Projectgate任一末端失败整Tx rollback（完整view/ref/receipt/Audit/Event）；真writer lock/poison与缺adapter，Provider有Model/Secret删除竞争 |
| `TestModelProjectConfigurationWriteHTTPUnknownAndLookup` (#12) | 定向原Project/command/唯一backend完整事实后arm；实际C(COMMIT)+Z(I)丢ACK后的私有确认，ROLLBACK、held-writer pending、确认取消保持原Unknowncause/attempt；public lookup EX真实等待/撤权/读Unknown零观察；另原key完整重放和异义不采其他receipt，副作用恰一次 |
| `TestModelProjectConfigurationWriteHTTPDefaultRoot` (#13) | 不注入fake成功handler/Store，默认app.Run/正式Login/CSRF→凭据→六配置write→五read/lookup；同Authority/Store/Tx真实事实，凭据/Owner GET+PATCH/Usage/System/Summary兼容；初始化失败不监听，重启保数据与receipt，ready503保持 |
| `TestModelProjectConfigurationWriteHTTPShutdown` (#13) | 默认root在途正式writer/慢body、正常shutdown与耗尽force；服务/backend/privateproxy当前真实终局与端口/子进程归属清楚，原forced inner-join限制不抹去，所有fixture测试所有者最后实际join |

身份主路径正式admin初始化+Login创建真实Owner/session，再正式Project/Secret命令；必要辅助Owner/archived/引用反例可沿已有私有SQLseed并受锁复读+正式Login，但明确不是生产所有权迁移/Archive全域或新Agentadapter已实现。真实项目建立沿接受fixture的Skills initializer测试隔离口，不声称Production Skills绑定。权限失败不把非法typed输入提前失败当目标事实。

Unknown proxy只在指定实际事务/command与唯一backend/writer锁匹配后武装，根后台Initialize/Mail/维护读不能偷走；安全事件最多type/length/阶段/连接序号/布尔事实。C+Z观测、原writer终局与其后的lookup是不同事实，必须同connection顺序及实际ACK丢弃；不造CommitResult或把socketclose当commit。没有原帧就不回填原失败已观测frame length；私有helper超界先报不改共享proxy。所有连接两方向actualjoin/ForceClose必须闭合当前实际调用链，不宣称任意Dial都有界。

### 8.4 旧行为精确复用与实际增量

下列名称均由接受read源或凭据接受规格固定；完整产品交接后必须逐字list、绑定实际最终源和可复用证据，无法定位/改名须root定窄修，不把待实施名冒现有通过。

必须有本卡实际默认根兼容（可分包/分轮，不能只引用library）：`TestModelProjectConfigurationHTTPDefaultRoot`、`TestModelProjectOwnerUpdateHTTPRootBinding`、`TestModelProjectUsageHTTPRootBinding`、`TestModelMeetingSummarySettingsRoot`、`TestModelSystemHTTPConfigurationCRUD`、`TestModelSystemHTTPAtomicEffects`，加凭据完整接受后的 `TestModelProjectCredentialHTTPDefaultRoot`、`TestModelProjectCredentialHTTPCRUDAndHistory`。后两目前是接受规格入口，产品验收前不得运行本卡。

原库必需语义按源/依赖无变可复用既有接受证据，若本卡触及其有效执行输入或新根组合未覆盖则跑精确受影响代表：`TestModelProjectCRUDScopeAndCanonicalReceipts`、`TestModelProjectSecretReferencesAndAtomicEffects`、`TestModelProjectSecretReleaseAndDeletionShareRealWriterLock`、`TestModelProjectReceiptReadGateAndLegacySystemPlan`、`TestModelProjectPreparedAuthorizationAndReferenceMapping`、`TestModelProjectUnboundReferencesAndDeleteBarrier`、`TestModelProjectPreparationFailureRechecksReadReceipt`、`TestModelProjectRealFinalCommitUnknownThreeStates`、`TestModelProjectRollbackUnknownCannotAdoptDifferentSemanticReceipt`、`TestModelProjectProducerFactsCannotBeReplayedOrRebound`、`TestModelProjectAuditPreparedFactsAndProjectEventStages`、`TestModelProjectAvailableDirectoryHasOnlyEnabledSafeUnion`、`TestModelProjectQueryCursorsAndEveryPageCurrentAuthority`。

凭据其余当前权限/被动查证/Unknown三个top（`TestModelProjectCredentialHTTPCurrentAuthorityAndFacts`、`TestModelProjectCredentialHTTPPassiveLookup`、`TestModelProjectCredentialHTTPUnknown`）在其完整接受后建立来源绑定；本卡不改Secret其实现，适用部分复用，不重述Secret与Model不同的重放/Unknown规则为相同。System旧wire/Session读可复用未改证据；新schema/组合方法有影响时跑精确相关pure/actualbody。所有复用与本次实际命令分别标清版本、top/sub和范围，不称一次全包/全旧组通过。

## 9. 安全证据、独立验收和交付

公开原件不得包含配置/credential请求body、Cookie/CSRF、密码/DSN/keyring、材料或材料hash、idempotency key/canonical semantic。新增含配置DTO的fmt/slog不输出输入；panic只记固定标签，实际Fault cause/attempt相等用测试内部断言及安全布尔，不落可还原key的cause/identity原值。敏感fixture/handoff必须私有0600并cleanup删除，公开目录不复制其内容或hash作为材料指纹。

native/proxy syscall trace只准闭集 `socket,bind,listen,connect,accept,accept4,getsockname,getpeername,shutdown,close`；仅端点/FD/PID-starttime/返回值，不用`%network`、`all`、read/write/send/recv及vector/message变体，不能以-s0/事后脱敏代替采集前限制。Go私有观察只安全长度/阶段，不记录writebuffer/PG原帧。

schema解释器只取绝对 `AGENTEAM_PROJECT_MODEL_CONFIGURATION_SCHEMA_PYTHON`，事先冻结已有jsonschema/referencing依赖；需要导出时取独立绝对 `AGENTEAM_PROJECT_MODEL_CONFIGURATION_BODY_DIR` 和非空 RUN_ID/INPUT_ID（同前缀），缺绑定拒绝导出，不默认写共享目录。未开启导出只是不产生本轮联验原件，不能称新schema/body通过。真实schema导出安全response**原bytes**及sidecar：实际method/path/target、status、Content-Type、Content-Length、实际X-Request-ID、run/candidate/schema hash；材料请求/actor/session/key不导出。不同top独立目录绑定输入，不把失败轮部分body称全PASS；Unknown body.request_id与实际header不能互相推造。同一真实body走Draft202012+FormatChecker/原common refs，坏receipt和union负例另列受控证据，native、PG、schema不互相冒充。

作者完整13停写后，由未参与实施的独立实例STATIC全审；A重组当前权限/archived六history、原子末端事实、物理Unknown/公开EX lookup，B默认根实际凭据→配置→read与关闭，加必要独立native/同body schema。复用不受影响旧图和语义证据，原失败/版本/实际wait/清理逐轮保持；任何资源FAIL先真实收尾停后继，root明确授修复和重跑，不能自动调大budget或削断言。

native/PG/独立真实窗口由root另行显式授权，真实资源严格串行；离线准备不代表可启动资源，作者闭合actualwait/双清并交还后独立才能接力。

全13技术独立PASS经root采纳后才另授README14，明确只交付本卡完整结果，保所有未绑定/未完成边界。最终14重新逐hash匹配、末件独审、作者与验收者停写，无owned命令/资源，由root整合提交推送；作者不Git、不再委派。当前rev1只有scratch规格/自查，没有Go运行、产品、资源或独立STATIC通过声明。
