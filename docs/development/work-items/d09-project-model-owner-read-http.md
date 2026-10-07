# D09 Project 配置与安全可用目录 Owner 只读 HTTP

修订：rev2，2026-10-07，**完整限定独立 STATIC PASS，root 已采纳；未授权实施/Go 资源，仍待 Update 完整产品接受及共享根交接**。本轮唯一仓库写入为本规格；下列实施白名单不是产品、Go 检查或资源授权。固定产品输入为 root 已接受的 `901eb54605d293d4308caadd278c2c3a7ae1b824`；D08 Update 仅消费已接受规格 `03d1c107`，其活动实现尚未接受，完整产品接受与共享根交接门槛保持。固定来源索引 `/workspace/scratch/project-model-owner-read-http-spec/inputs02.json` SHA-256 `0b3ba4165bb0ef2f951a4883281221626d2a9c4bf3d6c02f831ab9e74e6af311` 原样保留；原 scratch draft01 与 cursor 问题记录不改。root 已采纳下文 8 MiB 完整表示限制及输出无法提供时的原 503 语义。本修订仅归位标题、状态与正式文档链接，并纠正 cursor 无时间到期语义；其余技术内容保持 draft01。

## 1. 完整结果与真实前置

交付当前 Human Owner 对自己的 Project Provider/Model 配置列表、详情，以及当前 Project 可用 chat 安全目录的五个 GET/HEAD；默认 `app.Run` 同时提供这些路由，当前权限、分页、严格安全 DTO、原 Read Unknown、实际 HTTP I/O 终局和根生命周期均有真实证据。它不重做已接受配置库，也不把生产 root 接口声明当已实现。

| 固定来源 | 已有事实、本卡消费及限制 |
| --- | --- |
| [D09 配置库验收](../agent-team/d09-project-configuration-verification.md)，`de00c610da62cb77cc03efe7c3cc842cf81f1ba5` | 已验 Project CRUD、五查询、当前 Owner/Session、同 Tx scope/锁、分页与七字段安全目录。本卡只消费五查询，不重复该库实施或开放 CRUD。该旧报告末尾关于 Summary 未定是当时历史，不能覆盖后来已接受的 S1/S2/S3。 |
| [Owner read](d08-project-owner-read-http.md)，产品 `901eb546` | 已验 Account HTTPBoundary、同一 Project Authority、Reader、Usage/名称 resolve 路由和有界 native I/O 终局。 |
| [Update rev1 固定规格](d08-project-owner-update-http.md)，规格 `03d1c107` | 规定将唯一 Project Authority 提前纯构造，供 Usage/Reader/Project Service/Audit/Outbox；仍明确 `Model.Authorizations.Projects=nil`。这是未来接线位置和交接约束，当前不声称该实现已验。 |
| `model/project_query.go`、`model/query.go`、`model/authority.go` | 五个查询签名已存在；Authority 的 `Projects ProjectAuthority` 可选口已由真实 `project.Authority.AuthorizeProject` 满足。现固定默认根未注入此口，既有 HTTP 只有 System 路由；这两个缺口就是本结果的生产增量。 |
| [模型配置正式规则](../../architecture/platform-infrastructure/model-system/model-configuration.md) | Owner 管理本 Project chat 配置；enabled System chat 对所有 Project 可选，第一阶段无 allowlist；System 配置秘密不通过目录给 Project。目录可选不等于 Provider 网络/型号已实测。 |

没有发现五查询所需的未接受公共端口或待用户决定的产品含义。所需 Account、Store/Tx、Project Owner gate、Model Service、cursor、Secret/Audit/Outbox 既有构造依赖均有已验能力；正式根接线必须等待 §7 的 Update 产品接受与共享文件交接。该等待是工程依赖/所有权门槛，不能用 fake grant、额外 Authority 或生产 stub 跨过。

排除 Project credential HTTP/材料读取、Provider/Model mutation、命令 lookup、Agent 引用替换、Resolver/Provider 调用、Model Invocation 写入、UI/前端 client、迁移、共享脚本及依赖锁。System 旧 HTTP/Schema 行为不变；Summary 管理员统一 initial/update（含首轮标题）、无 Project override/复制默认值与原四用途兼容不变。Production Resolution/Invocations 和 D24 仍未绑定；完整 D08/D09/D08–D28/E01 不因本结果完成，ready503 不变。Object runtime join、OpenAI tools 独验、SPA concurrent-publication 三停止保持。

## 2. 五个资源、正式调用及严格请求

公开纯构造建议为 `model.NewProjectHTTPHandler(core *model.Service, boundary *account.HTTPBoundary) (http.Handler,error)`，代码留 `internal/central/model` 新文件内；真实构造拒绝 nil/未绑定 Service、无 Project authority、无效原 keyring 或 Service/Model Authority 不同 Store，受控五查询/boundary 接缝仅包内私有。ProjectAuthority 接口不暴露 Store，不能声称这里可反射证明其内部 Store；正式同实例/同 Store 由 root 构造和真实 Tx/锁验收绑定。不能公开可传任意 grant 的 adapter，也不为此新建 Model Service。另提供精确 `model.HandlesProjectHTTPPath(string) bool` 供 root 分派。

| GET/HEAD 路径 | 唯一正式库调用 | 返回 |
| --- | --- | --- |
| `/api/v1/projects/{project_id}/model-providers` | `ListProjectProviders(ctx,actor,project,ProjectQuery)` | 本 Project ProviderPage；包含 disabled 配置 |
| `/api/v1/projects/{project_id}/model-providers/{provider_id}` | `GetProjectProvider(ctx,actor,project,provider)` | 本 Project Provider |
| `/api/v1/projects/{project_id}/models` | `ListProjectModels(ctx,actor,project,ProjectQuery)` | 本 Project 所有 Provider 下 chat ModelPage；包含 disabled 配置 |
| `/api/v1/projects/{project_id}/models/{model_id}` | `GetProjectModel(ctx,actor,project,model)` | 本 Project chat Model |
| `/api/v1/projects/{project_id}/available-chat-models` | `ListAvailableChatModels(ctx,actor,project,ProjectQuery)` | enabled System/本 Project chat 七字段目录 |

所有调用精确沿现有签名；一个请求只调用对应查询一次。不增加 `provider_id` 过滤：现有 ProjectQuery 只有 Cursor/Limit，Project Model 列表不是 System 的按 Provider 查询。不补 HTTP SQL、第二次详情查询、总数查询或目录配置查询。

分派只匹配上述精确 segment 形状；未知子路由交原链，非法 UUID 形状中的目标 segment 仍交本 handler 报 400，不能提前降成其它资源。无重定向或二次 decode。Account `CheckRequest` 原 canonical path、RawPath、encoded slash、反斜杠/NUL 和 Origin/Fetch Metadata 规则先行；不要把非 canonical 路径误期望为业务 404。匹配资源只允许 GET/HEAD，其他方法 405、`Allow: GET, HEAD`；这不开放 mutation。HEAD 执行与 GET 相同认证、当前 Tx、完整结果验证/预算/编码，成功及 Problem 都无 body，Content-Length 按相同 GET 表示。

预算先建立，然后 `CheckRequest`、方法、`RequireHuman`、严格 path/query/body、库查询；actor 只来自正式 cookie Session，不采用户、Owner、scope、actor header/body/query。GET/HEAD 沿原浏览器安全规则，不自造 CSRF 要求，不降级 Origin；越界方法不会进入服务。

列表仅 `cursor,limit`；详情拒绝任何 query/ForceQuery。RawQuery 在 split/decode 前限 32768 B；每键和值只解码一次，拒绝未知键、解码后重复键、多值、空值、缺等号、空段/分号、坏 escape、非法 UTF-8/NUL、空 `?`。limit 缺省 50，canonical 无符号无前导零十进制 1–100；cursor 非空且解码后最多 8192 B。过长 cursor/后续 token 或绑定错误沿 `CURSOR_INVALID`，其它输入错误沿 `INVALID_ARGUMENT`，不回显输入。不接受生命周期、enabled、scope、排序或任意 provider 过滤。

拒绝非零/不确定 Content-Length 或 Transfer-Encoding；即使声明 0，也须在同 native deadline 内对实际 Body 最多读取一个字节以证明 EOF，再由本请求同步关闭。不能仅检查头，也不能无界 drain。空请求不新增 Content-Type 要求。请求所有权、Close/回调/响应输出细节见 §6。

## 3. 当前权限、分页与只读事务终局

HTTP `RequireHuman` 只取当前身份；Model 每次 `readScope` 保持原正式 WithinTx，在同 Store/Tx 内 Acquire 所需 User/Project、全局 `model-references` SH 和详情 aggregate SH；Model 自核 held locks，再由 Sessions/Projects 当前授权。Project delegate 继续同 Tx 核当前 Session、Owner、initialized 和 lifecycle，不开启另一个事务或补锁。管理员不是他人 Project Owner，不能经目录读他人数据。

| 当前事实 | 结果 |
| --- | --- |
| 当前 Owner/Session，initialized，active/archiving/archived | 原 Read 通过；目录依当前 enabled，不拒绝 archived 只读 |
| 非 Owner（含另一管理员）或不存在 Project/跨 scope Provider/Model ID | 原 `NOT_FOUND`，不泄露是否存在或配置 |
| 未初始化或 deleting | 原 `PROJECT_NOT_ACTIVE`；不把当前库 gate 改成 Owner Reader 的另一种投影 |
| Session 撤销、失效或 Account 当前拒绝 | 保留原 Fault/status；不是 HTTP 缓存 grant |

列表按原 `created_at DESC,id DESC` keyset 和首水位，cursor 绑定 Project scope、稳定 User、查询种类及固定 chat/enabled 语义；包含 water/after instant+UUID 四 scalar、无 OrderGeneration。更换同一 User 的有效 Session/合法变更 limit 可以续页；跨 User/Project/三查询种类、System cursor、篡改 token，或签名 kid 已不在当前加载 keyring 中的 token 均拒绝。cursor 无 TTL/时间到期语义；轮换后保留旧 kid 及对应 key 时，原 token 仍可验签，且必须继续通过每页当前权限和查询绑定检查。每页重验当前权限；首水位限制后插入行，不宣称跨页 frozen configuration snapshot。目录原单 JOIN 的 statement snapshot 同时读取两层 enabled 和七字段；不得改成多个未约束查询或把 SH 当配置 mutation 排他锁。

原五查询负责 scan/Rows.Close/rows.Err、哨兵与签游标，HTTP 不接触数据库。HTTP 检查返回 item 数不超过请求 limit、ID 无重复、cursor 长度/空页关系及各 DTO 的完整安全条件；Provider/Model 列表可用已有 CreatedAt/ID核严格降序，目录七字段没有 CreatedAt，不为验证排序偷偷添加或加载该字段，顺序真实性由原库及真实分页验收证明。不能因为某条不能安全投影而删除它，不能改变 next_cursor 或减 limit 后自动重查。

服务返回 error 时首先返回原 error，**先于一切输出预算/DTO 检查**。原 `readScope → commitError` 的 NotCommitted Fault 和 UnknownCommandError 原 attempt/cause 保留；Unknown 返回零结果，即便 callback 已得到完整候选也不能发布。有效发布预算内 Unknown 为原 503/`COMMIT_UNKNOWN`/`commit_state=unknown`，不改成 503 cap 错误或 NotCommitted；不新造 read lookup、二次查询或写幂等 key。显式再次 GET 是新读取，不能声称确认原读事务。服务 nil error 后 context 已取消/到期则丢弃成功候选并 abort，不谎称数据库没有读取或已回滚。

## 4. 安全 DTO 与 schema

全部使用显式 HTTP DTO，不直接 Marshal 领域 View/Page/Scope 或从完整配置 JSON 删字段。列表外壳固定 `{items,next_cursor}`，空为 `[]`、无 cursor 为显式 null；Provider/Model 与目录都无额外 total。所有资源 ID 为原 canonical UUIDv7；version/正 TokenCount 为原正 int64 十进制 JSON 字符串至 `9223372036854775807`，不用 float64/JSON number；时间沿原 UTC 微秒 Instant。

| DTO | 精确字段与条件 |
| --- | --- |
| 本 Project Provider（6 字段） | `id,scope,input,version,created_at,updated_at`；scope 精确 `{kind:"project",project_id:target}`。input 精确 `name,protocol,base_url,enabled,credential_ref,options`；credential_ref 为同 Project 引用的 ID 字符串或显式 null，不输出内部 Scope/ref 对象。protocol 仅原两个 chat protocol。 |
| 本 Project Model（7 字段） | `id,provider_id,scope,input,version,created_at,updated_at`；scope 同上。input 精确 `name,provider_model_id,type,enabled,parameters,request_overwrite,header_overwrite,capabilities`；type 固定 chat，header map 空为 `{}`。 |
| 安全目录 item（恰 7 字段） | `id,provider_id,scope,name,provider_name,version,capabilities`；scope 仅 `{kind:"system"}` 或 `{kind:"project",project_id:target}`。不可多出 endpoint/base_url、protocol/options、CredentialRef/ID、provider_model_id、parameters/request/header overwrite、时间或其它 Project 字段。 |

本 Project 完整配置是当前 Owner 原已接受安全配置视图，包含其配置 URL/参数/覆写与稳定 credential ID；不代表提供 Secret material/metadata API。目录即使包含 System 模型，也绝不能消费 System GetProvider/GetModel 补数据。用各字段唯一 canary 证明目录体/错误/日志不含这些字段和值，不能只测 JSON key 名。

成功为200、`application/json`、准确Content-Length和`Cache-Control:no-store`；原Account安全headers沿用。错误沿公共Problem：输入/cursor400、会话401、Origin等403、隐匿资源404、方法405、Project gate409、依赖/Read Unknown503及其它既有Fault映射，不笼统改锁/取消错误码。HEAD不声明也不发送成功/错误实体。内部cause/attempt保留但wire无cause_id，统一使用原服务端RequestID；新handler在外层可见的request.Pattern设置安全路由模板，未知为unknown_route，日志不记录原path目标、query/cursor、配置、Secret引用/材料、Session/CSRF/cookie，不新增第二层日志或RequestID。

Capabilities 显式十字段与现有 System HTTP wire 一致：四 bool（tool_calls/parallel_tool_calls/streaming/reasoning）、四非 null 数组（input_modalities/output_modalities/reasoning_efforts/structured_output_modes）、context_length/max_output 显式 null 或正 int64 字符串。数组保持原顺序，不删重/裁剪；原 Validate 的 parallel→tools、非 reasoning 时 efforts 必须空、input/output 4 值和 structured 2 值闭集/去重、effort safeToken≤32 B、max_output≤context_length 全保留。**reasoning_efforts 不发明有限枚举或业务 maxItems**。

预估预算通过后才调用原 View/Capabilities Validate 等可能分配的验证，再逐字段投影/必要复制。Provider/Model完整校验含未直接输出的 scope/ref 同 scope、时间关系和 Project chat限制；详情 ID必须等于 target。目录验证原 typed IDs/version/scope、name/provider_name 原1–128 rune且≤512B、UTF-8/NUL、原 caps；不能用不在七字段中的 enabled/provider config 做虚假验证。错误统一零成功投影，不含坏值原文。

新增 `api/openapi/project-models.json`，五 GET及五 HEAD；OpenAPI 3.1/Draft 2020-12，引用现有 common Problem/UUIDv7/Instant/PositiveInt64String，新增闭合 ProjectScope/AvailableScope、三 DTO/三 Page。所有固定对象 `additionalProperties:false`；受原契约允许的 options/parameters/request_overwrite/header_overwrite 是明确动态对象，不能为“闭合”误禁合法扩展。能力 schema 至少精确闭集数组/unique、effort pattern/32 ASCII字节、布尔关系、nullable/presence；跨数值字段及 UTF-8字节等标准 schema不宜完整表达的部分注明由正式 Go 验证/专门反例执行，不拿schema通过代替所有业务校验。绝不修改 System schema 来迁就新口。

标准已安装 schema 引擎及 FormatChecker/固定本地 refs验证实际 body；不安装依赖。正例覆盖空页/续页、nullable、两scope、禁用配置与大版本；负例覆盖每个额外敏感字段/错误scope/错IDtype、缺null字段/数组null、数字version/溢出、非法 capability枚举/重复/关系及目录第八字段。真实输出保存原bytes、status、Content-Type、实际 X-Request-ID、target/source-run/input hash，不重编码，不从body反推真实header。只导出合成fixture安全材料，cookie/CSRF/Secret/actor/原命令key和私密日志不入原件。

## 5. 8 MiB 完整表示限制：精确依据与不保证事项

root 已采纳新 HTTP 的 **8 MiB（8,388,608 B）完整表示硬界**。成功只能在整页/详情完全验证和编码后发布。合法数据无法在此预算内安全提供时，零成功 item/零部分200，返回原 `DEPENDENCY_UNAVAILABLE`/503/`NotStarted`，不裁剪字段、数组或整行，不自动减 limit/重试，不改 accepted domain/DB合同。这是 HTTP 表示阶段未开始发布，不是数据库未执行或回滚声明。

依据及与旧行为的差别：

- `model/contract/types.go:286–308,472–481` 的 reasoning_efforts 只有每项 safeToken≤32 B和 unique；允许字符为 `[A-Za-z0-9_.:-]`。其它三数组的闭集确有小上界，但不能套用给 efforts。合法构造 `reasoning=true`，efforts 为 `r00000000` 至 `r000fffff`（1,048,576 个不同9 B token），单数组紧凑 JSON 就有 `1+12*N = 12,582,913 B`，还未加字段/外壳，已超过8MiB。无需实验构造大值便可由合同推导；本草稿没有执行产品验证。
- `00015_model_configuration.sql:42` 对 capabilities 只检查 jsonb object，无该数组总量约束。PG一般字段上限并不是可用于2s HTTP投影的小上界。直接库合法值/已存配置不能拿 System HTTP 的1MiB输入体上限排除。
- `model/http_dto.go:httpModelDTO` 现先 append 四数组，`httpapi.WriteJSON` 直接完整 Marshal且无输出cap；`DefaultMaxJSONBytes` 是 DecodeJSON 输入默认值。旧 System 配置 GET/List不具本段新增保证，本卡不改旧口或追称它有。
- 原库 `project_query.go` 在HTTP之前已经读取/Unmarshal/Validate/Clone；本 HTTP 无法约束这些前序分配、DB内存或全进程RSS。此限制只控制本次新增 HTTP 校验/DTO复制/编码；原数据库context/锁机制和实际终局另验，不宣称取消能强杀不合作依赖。

工程算法必须在**新 DTO clone、会分配去重map的 Validate、json.Marshal之前**完成。以budget余量作checked/saturating计数，溢出即拒绝，不做无保护乘法/加法。先按容器len及每成员最小JSON成本检查：页 items≤limit≤100、各字符串/RawMessage长度、四数组len、header map len等；对 efforts 可先用非空JSON字符串加逗号至少3B的保守下界判不可容纳，再逐项计数，不能先分配同长度slice/map。即使依赖返回错误超大容器，也不遍历/复制全部后才报错。

逐项计数有剩余字节和context双界；固定字段名、引号、冒号、逗号、括号、null/bool/标量、scope、外壳、cursor全部计入。字符串用与最终Go编码一致的escape规则计算（控制字节、引号/反斜杠、默认HTML转义`<>&`、U+2028/U+2029等），不得把rune数当编码字节；原raw对象先len≤原64KiB等必要边界，使用覆盖其最终compact/escape的保守上界或等价无分配精确扫描，不能低估。保守上界若无法证明在8MiB内可拒绝；因此不承诺一切实际小于cap但上界较大的表示必成功，尤其不能宣传覆盖全部合法最大页。预算通过后分配规模已受界，再做正式结构验证/复制/Marshal；最终len仍不得超cap，任何意外超出同样零发布。

cap、坏依赖结果等表示错误沿本域 `unavailable(nil)` → `fault(DependencyUnavailable)` →公共Problem 503/NotStarted，来源是现有 Model“依赖结果不能提供”的语义；不是413请求体过大，更不是重新解释原Read事务。**服务先返回的Unknown/其它Fault不进此cap分支**；已过HTTP期限则abort，不尝试迟到503。默认外层安全headers/RequestID保持，不回显实际大数组/计数/配置。

后继实现必须测 checked overflow、len先拒绝、escape真实bytes与上界关系、恰边界/边界+1、100项全页且坏项在末尾、空body的HEAD同检查，以及上述合法大efforts反例。对保守预估“刚好通过/刚好拒绝”与最终实际wire长度分别断言，不用只断言len(body)证明前置分配有界；不可虚报未做的RSS保证。PG增量可用已有合法fixture辅助SQL写入该大caps原bytes并说明来源，它证明读取边界而非新mutation入口；未调用Provider。

## 6. 发布预算、native I/O 与实际收尾

五 GET/HEAD 均从 `CheckRequest/RequireHuman` 之前建立 `min(parent,now+2s)` 的**一次发布/I/O预算**，沿Owner read/Usage当前只读工程标准。原五查询没有新增私有WithoutCancel确认尾部。认证、严格输入/EOF、唯一同步服务调用、预估/验证/编码、Body.Close、Write和Flush都消费同ctx，不为编码、HEAD或错误另开预算。

可直接复用同 package 已接受的私有 `meetingSummaryRequestIO`/abort writer机制（由新handler传入2sctx），不改Summary的3s/30s、旧System dispatcher或公共HTTP框架。它负责真实 ResponseController read/write deadline、context.AfterFunc回调、同步Body.Close、callbackDone实际join与deadline reset。若实际实现选择独立私有helper，必须保持同样所有权/终局并在本卡新文件内，不导出通用框架。最外层唯一middleware仍在root，不能handler里再装Recover/日志/RequestID。

没有deadline/Flush能力就在调用业务之前安全abort，不能静默退回无界读或扔goroutine。完整 body在发布前编码，短写、Write/Flush/Close错误、panic、回调错误、deadline reset失败或期限过后均走原 `http.ErrAbortHandler`，不补第二个Problem/迟到200；只有原服务调用、实际输入Close、Write/Flush、取消回调结束后才清该连接deadline。用同步调用保有依赖尾部；ctx.Done/HTTP abort不是服务已返回，更不是goroutine/socket已join。

三组独立native顶层建议精确命名：`TestProjectModelHTTPNativeKeepAliveDeadline`（自然GET与HEAD2s、早parent、成功后同连接新请求/无残deadline）、`TestProjectModelHTTPNativeSlowBodyAndCallback`（真慢EOF/断连及回调、body实际结束）、`TestProjectModelHTTPNativeWriteFlushCloseAndAbort`（完整大响应/慢读、native短写/断连、HEAD Flush/partial/panic/reset真实尾部）。受控writer/helper断言另列普通纯测试，不能把它算native。native只使用已编race binary和本任务唯一loopback/原syscalltrace；不用PG/外部服务假造依赖。

每native条总45s、测试内40s；自然2s不得缩短成毫秒取消，预算从测试启动覆盖其全部真实尾部，阻塞noncooperative seam必须先放行并actual join。沿已验trace unfinished/resumed精确PID/FD/port重建方法，保留旧parser空ports首红限制；解析结果不能空集合自动通过。业务结束后TIME_WAIT最多75s仅是cleanup观察，不能据此延长业务预算或把TIME_WAIT叫活动handler。actual wait和两次owned进程/端口清理成立才进入后组。

## 7. 默认 root 接线与 Update 产品交接

当前不读取/改写Update活动 `app/account.go/security.go`、新Project Service/dispatcher等18技术路径。现有根结论来自901eb固定baseline副本（inputs02列读路径），未来接缝来自接受Update rev1，不能从活动实现推导已验事实。

root先确认Update全部产品结果（独立验收、README末件、精确commit/源指纹、命令资源actual终局）接受并停写，然后唯一交出本卡 `app/account.go`；动态图/运行再冻结实际传递依赖。若Update最后接缝与规格有差异，按其接受产物重核影响后由root采纳必要规格差量，不能回退Update或靠旧overlay冒充最终联合根。共享security.go/Project Update路径没有本卡写权。

接线最小变化：在Update已规定的 Account Authority → 一次 `createProjectUsage` 纯构造之后，把原同一 `projectUsage.projects` 传给原 `model.NewAuthority(... Authorizations{Sessions:authority,System:authority,Projects:projectUsage.projects})`。原Resolution仍nil；同一Store和同一Project实例继续供Usage/Reader/Project Update/Audit/Outbox，不创建第二个Authority，不重建core/model，不用mutable locator/setter。Model新HTTP构造捕获现有同一Service及Account HTTPBoundary，外包精确新路由dispatcher；不抢 `/projects`、Project详情GET/HEAD/PATCH、resolve、commands/lookup、Usage或System路径。

本卡无新Initialize、后台worker或work owner。原 Secret→Model→Summary→Usage顺序仍在同一SecurityStartupTimeout=30sctx内；原后继Object/Outbox/Account/listen次序、未配置Summary、唯一mail handler及诊断/ready不变。Update已验 Project work StopAdmission/Drain/Force/Joined与原accountAssembly/HTTP/Outbox联合终局不动；新同步读属于原HTTP请求和Store事务的所有权，必须实际退役后才宣告正常root终局。不会把Force/cancel当join或改Object停止边界。

故障构造不发布半绑定handler；默认公开 `app.Run` 的初始化失败/取消/重启与资源释放继续原流程。测试必须证明运行的是默认依赖函数，不能以 `runWith`注入fake初始化/替换Model authority/缩短安全预算代替默认root。fixture只为无生产创建API的事实建立提供明确已验正式口或辅助canonical seed，不把测试组合当新root创建能力。

## 8. 候选实施白名单（13 技术 + 1 README末件）

这是建议范围，不是当前写权。正式卡采纳、Update交接及root明确派工后才可写。除account.go/README外均拟新增，开工再次核不存在；若存在冲突交root，不覆盖。

| # | 路径 | 必要职责 |
| --- | --- | --- |
| 1 | `internal/central/model/http_project.go` | 纯handler/精确路由、一次正式查询、2s与真实I/O所有权 |
| 2 | `internal/central/model/http_project_wire.go` | strict请求、完整安全DTO、clone前有界计数/校验/编码 |
| 3 | `internal/central/model/http_project_test.go` | 无监听权限/错误/Unknown/取消/受控尾部 |
| 4 | `internal/central/model/http_project_wire_test.go` | 请求/DTO/schema/bigcaps/预算与零前缀 |
| 5 | `internal/central/model/http_project_native_test.go` | 三native顶层，必须另授唯一资源窗口 |
| 6 | `api/openapi/project-models.json` | 五GET/HEAD、闭合scope/config/catalog/pages、原Problem引用 |
| 7 | `internal/central/app/project_models.go` | 同Service新handler纯构造、精确root dispatcher |
| 8 | `internal/central/app/project_models_test.go` | 缺依赖/同实例接线、Update/旧路由组合和纯构造无I/O |
| 9 | `internal/central/app/account.go` | Update完整接受交接后窄加Projects实口和handler调用 |
| 10 | `tests/model/project_configuration_http_fixture_test.go` | 正式身份/库/PG组合、明确seed、无秘密raw body导出 |
| 11 | `tests/model/project_configuration_http_test.go` | 五资源、safe union、分页、原byte/schema、8MiB实际边界 |
| 12 | `tests/model/project_configuration_http_terminal_test.go` | 当前Session/Owner、锁/Read Unknown/取消零候选与实际终局 |
| 13 | `tests/model/project_configuration_http_root_test.go` | 默认公开app.Run、真实五路由/旧Update保留、失败/重启 |
| 14 | `docs/development/backend/README.md` | 技术独立接受后root另授的最后局部能力说明 |

不改 model/project_query.go/query.go/authority.go/contract、旧System/Meeting Summary实现/测试、Project/Secret/Audit/Outbox业务库、security.go/project_usage.go、迁移/脚本/锁/入口台账。若真实检查揭示库缺陷或旧测试断言确需改，保留首红、明确最小scope差量交root；不通过新HTTP过滤丢项掩盖原库错误。

## 9. 冻结输入、作者验证与独立验收

以下全部为将来计划，当前未运行。精确selector/driver/binary/源与依赖闭包冻结后由root分别授离线、native、唯一PG窗口；每一步实际返回、输入一致和owned双清成立才推进。失败保留原输入/原raw/exit/实际wait，先清尾再归因，不自动重跑/续后组。

### 9.1 实际编译图和离线检查

用固定Go1.27.1 `/workspace/toolchains/go1.27.1/bin/go`、显式 `GOMODCACHE=/workspace/go/pkg/mod`、既定mod/sum只读、GOTOOLCHAIN=local、GOPROXY/GOSUMDB=off、独立scratch产物；不复制全Go树/大依赖索引，复用已接受Ownerread及未来Update适用的actual graph，按实际delta核对。新schema的运行时os.ReadFile/本地refs必须纳冻结，不只统计Go import。

冻结普通/race model/app tests图、`-tags=integration` tests/model/受影响tests/project实际图，以及完整固定脚本链 `test-objects.sh→test-security.sh→test-postgres.sh` 的实际包集合和TestMain。原 `test-models.sh` 只是固定`^TestModel`的宽入口且拒绝附加argv；本卡按组精确运行应直用 `sh scripts/test-objects.sh -run <精确selector>`，不改脚本或误把宽入口算成精确组。三个动态helper分别为 `tests/testsupport/{objectstore,outbound,postgres}/cmd/fixture`；outbound helper还以显式 `CGO_ENABLED=0`构建 `tests/testsupport/outbound/cmd/server`。`tests/process/process_test.go:TestMain`在任何test selector前构建 `./cmd/agenteam` 与 `./cmd/agenteam-runner`，不能因本轮没有Process top而排除两个cmd。16个脚本package pattern展开的实际包图（原接受闭包20包）、stdlib cgo_stub/模块源、Go/CC/Python/shell/Docker工具及driver/parser均纳准确使用条件，旧模式数量不能替代本轮actual图。先根据固定来源准备保守清单，再actual `go list -deps -test -json`核真实闭包；漏项最小补冻，不能读活动源吞漂移，也不把历史普通CGO图当0变体。若未来Update还活动，只能准备，最终root验证不得靠隐藏其实现的旧图。

离线每条45s总、实际wait/双owned进程空：实际graph→受影响test-c普通/race/integration、vet及两cmd build→精确-list→model/app原全部pure普通/race（含新增受控测试）。默认native/fixture顶层必须明确排除出纯选择器，-list只发现不跑body。同输入已PASS可复用，范围内修订只补受影响图/编译/运行，不无条件全跑；不能以compile/no-tests替代行为。

### 9.2 真实HTTP/PG必须覆盖的结果

建议四新top分四轮，以 `TestModelProjectConfigurationHTTP` 前缀分别覆盖 Projection、AuthorityAndTerminal、BoundedRepresentation、DefaultRoot；每顶层总120s包含其Cleanup，原完整fixture `-race -count=1 -p=1` 每包6m保持，watchdog由RUN追踪到cleanup后的PASS/FAIL。不得把子调用deadline当顶层总预算，也不任意延长预算容纳测试堆叠。

| 结果 | 真组合/反例 |
| --- | --- |
| 配置与安全目录 | 正式库建立本Project两chat协议/disabled配置、System enabled/disabled/nonchat、另一Project；五GET/HEAD精确集合/七字段/DTO/null/version/时间。System完整配置不可经Project详情读；目录所有敏感canary值与字段零泄露。配置payload不作日志。 |
| 分页与scope | 三列表首水位、同User新Session、limit改变、无重复/晚插入不混入；跨三种query/System/Project/User cursor拒绝。archiving/archived Read真实gate沿既有正式接受方式或明确合法canonical fixture，不冒称新生命周期命令已root开放。 |
| 当前授权 | 正式Logout证明Session撤权（不误称权限角色撤销），另一有效Session可读；锁屏障内真实canonical Owner/initialized/lifecycle改变后当前Tx拒绝。另一管理员不能读；不存在/他人ID不可区分；零新增Model commands、Audit、Event、Secret引用/租约/Usage。辅助SQL事实必须说明来源并由正式authority复核。 |
| Read Unknown/取消 | 原真实PG代理在查询callback已完整后丢final COMMIT ACK，保原Unknown/attempt/cause、零成功DTO；原writer/连接实际终局后才结束。锁等待/rows错误/服务完成后取消/后期编码失败不发布前缀，不能用socket关闭代替writer已退出，也不用伪造CommitResult。 |
| 输出上界 | 合法大efforts同一原数据由Model详情/列表/目录读到，8MiB拒绝无200/无截断；一般合法页、精确escape/刚好预算/超界实际输出分别核。记录前序库已读/分配的限制，不将cap错写成DB安全/回滚。原Read Unknown与oversize同输入时仍优先Unknown。 |
| 默认root | 默认公开app.Run迁移/真实初始化/正式登录、五新路由、原Owner列表/详情/resolve/Update+lookup/Usage/System/Summary同时存在。测试新口Project权限能随真实事实撤回，证明同一authority；初始化失败不得先listen，重启原singleton/配置不重置，无Project默认模型/新worker。自然root停止/HTTP实际尾部/原Update Drain均保留。 |

身份主路径沿已验Account正式管理员初始化/登录；辅助Owner若沿SQLseed+正式Login必须标明其边界，不用伪造actor/header绕HTTP。Project事实/配置使用既有正式Project/Model能力，Skills仅允许既有fixture私有初始化测试口及明确provenance；不把该helper说成生产Skills。没有Project配置HTTP写是本结果明确范围，不因此发明隐藏root测试管理口。

旧回归最少固定七个已存在top：`TestModelProjectAvailableDirectoryHasOnlyEnabledSafeUnion`、`TestModelProjectQueryCursorsAndEveryPageCurrentAuthority`、`TestModelSystemHTTPBoundaryAndStrictWire`、`TestModelSystemHTTPCurrentAdministrator`、`TestModelSystemHTTPPaginationAndSafeProjection`、`TestModelProjectUsageHTTPProjectionAndPath`、`TestModelProjectUsageHTTPRootBinding`；再按Update最终接受输入精确发现其root联合/历史Update lookup关键top和Ownerread真实root top加入，不现在读取活动Update源码或伪造未来selector。缺名报告而不静默替换；库未变且原已验大CRUD/Secret原子性矩阵可复用，只对新增Projects绑定/路由影响补查。

### 9.3 资源与独立证据

PG沿已验当前7资源闭包（4容器/3网络及自有进程）：两正式PG精确digest、MinIO固定版本/二进制SHA、outbound/fixture进程、网络/目录与必要动态构建。PG17为 `pgvector/pgvector@sha256:99a149d3c84cfb0f32d8da7d72737e4643468787220af2223418730f8e9e9cdc`，PG16为 `pgvector/pgvector@sha256:16e62164a405447dca191079a924ee5b8a9dbf04fe53128701ffbea857b37782`；PG16为不支持版本fixture，不虚增第二份Model业务通过。MinIO为 `RELEASE.2025-10-15T17-29-55Z`、binary SHA `dc5298474f0bc87a068f0b1135c583bb1278c17c11c512212ed7644a238c89c8`，可用已验任务自有路径经AGENTEAM_MINIO_BINARY注入；不假设旧/tmp CachedBinary仍在。执行前逐hash、输入集合、fresh≥5GiB（native≥2GiB）、未授权AGENTEAM变量剥离核对。镜像tag不能替代digest，恢复缺依赖单列root授权，不改全机/已有基础设施或凭据。采用实际subreaper：direct/adopted PID+starttime、真实wait状态、exit、准确资源ID/labels/所有Mount、原始命令/env/raw、前后输入与两次owned资源/进程/端口空均保存。Docker daemon新PID1 shim Z可能不受任务owner/wait控制，逐轮差量单列，owned双清不等于全机零。

未参与实现的独立验收者先对冻结13技术源及root已接受Update delta独审，再准备私有探针与实际图：独立真PG至少覆盖当前撤权/目录隔离/分页、原Read Unknown和默认root；独立native至少覆盖自然2s与实际Write/Flush/Close/callback终局；schema对同一真实body而非手工重建JSON。可复用未变正式库/工具/fixture已验事实，不重复整树。原失败/harness错/产品修复区分，修复保原版本与受影响复验，不把SQLseed/受控writer/私有helper冒充正式HTTP或native。

完整接受门槛是五资源与默认root的正常/权限/协议/分页/预算/Unknown/实际终局、受影响旧兼容、原bytes/schema、独立检查全部闭合，README末件经独审，root绑定精确commit与停写终局。不得据单一STATIC、编译或一轮PG称整卡接受。

## 10. 当前可行性结论与后续交接

五查询/安全规则/权限和分页公共契约均已稳定，没有需要用户再决定的产品项。完整scope可在13技术路径+最后README内设计；唯一必要实施顺序依赖为Update完整接受和共享root文件交接，不能抢写。8MiB限制是已由root采纳的新HTTP工程规则，合法超界的存在和旧System/库前序分配限制已明确，没有假设小闭集或成功裁剪。

当前产物为本正式规格 rev2 及原样保留的 scratch 输入/差量记录，完整限定独立 STATIC PASS，root 已采纳；未授权实施/Go 资源，仍待 Update 完整产品接受及共享根交接。自查只核文档来源/路径、JSON、哈希、结构与格式，不运行业务/资源；自查不叫独立 STATIC。后续实施与资源仍须 root 另行授权，并满足 Update 完整产品接受及共享文件交接门槛，不因本卡落盘自动启动。若后续实际图、Update交接或正式库发现新公共缺口，仅暂停相应部分并给具体源/失败证据，不能在新HTTP里造替代端口。
