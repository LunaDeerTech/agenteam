# D09 System Model 配置 HTTP 与被动 credential 命令查证

修订：rev1.1，2026-10-05。状态：**规格已独立静审通过并获主线程采纳；业务实施待本卡提交后由主线程另授，当前尚未授权。** 实施者为 `d08_registry_backend`，独立验收者为 `skill_verification`。固定代码输入为 `9110686a507716d8b4e042a5562a22507658adcb`。本卡只交付库级 HTTP 组合，不表示生产 root 已挂载或完整 D09 完成。零迁移，不分配或写入 `00017`。

独立规格审查：`/tmp/agenteam-d09-system-http-spec-v-sa0bp1n2/review.md`，SHA-256 `261683dce59f30b0ead5ecece9ced00a76cad0d67db8c9cc27797eda179b7bc6`，结论为规格 PASS。最终被审 rev1.1 SHA-256 为 `c8376a7d0670fcf3e657d0c6643a38bddcf1ef5deb9c87fb0c3cf83fd9aacd2c`；此次仅更新采纳状态与审查定位，API、17 路径及验收正文不变，不构成实现或动态通过声明。

rev1.1 仅校正 §5/§7 的 Unknown 验收表述：Model 沿原恢复确认成功应返回 200 与原 receipt，只有服务最终仍 Unknown 才返回 503；Secret ExecuteWrite 的直接 Unknown 仍返回 503。API、路径、权限、产品规则与其它验收不变，不修改 Model 生产恢复行为。

## 1. 完整结果、真实前置与边界

当前 System 管理员通过真实 Account Session 使用 System Provider/Model CRUD、平台 selector、原 Model 命令查证，以及独立的 Purpose=Model credential create/update/delete 和被动历史命令查证。HTTP 消费已有真实 Model/Secret/Audit/Outbox；新增的 Secret 查询只读已提交 receipt，不要求原材料、不分配 nonce、不执行或重放写命令。

必读 [AGENTS](../../../AGENTS.md)、[团队流程](../agent-team/README.md)、[开发计划](../development-plan.md)、[设计技能](../../../.agents/skills/agenteam-design/SKILL.md)、[Go 技能](../../../.agents/skills/agenteam-go-development/SKILL.md)；独立验收读[验收技能](../../../.agents/skills/agenteam-verification/SKILL.md)。业务与错误沿 [D09 设计](d09-model-system-token-usage-design.md) §2–4、§10–11；本卡冻结此前未具体化的 System wire 接缝，不重定义 Model 配置契约。

| 已验依赖 | 本卡使用及仍缺边界 |
| --- | --- |
| [B01-K System 配置](d09-b01-system-configuration.md)、00015；[已验 Project 配置](recovery-d09-project-configuration.md)的兼容基线 | 使用 `model.Service` 的 System CRUD、list/get、selector、LookupCommand，以及真实 Audit/Secret reference/event producer。Project 路由、目录、Settings 不在本卡 |
| Account、httpapi | 使用真实当前 Session/admin、已有 Origin/Host/CSRF/cookie 核和严格 JSON/Problem。Account 普通 HTTP 及其旧行为不改 |
| Secret、00003、D03、Audit | 使用真实 Human 写入/Metadata、稳定 ref、加密语义 receipt 和同 Tx Audit；新增被动查询沿原 receipt 唯一键，不改旧写/重放协议 |
| 活动 [Secret Model usage 卡](recovery-d09-secret-model-usage.md) rev1.1 | 20 路径为其它作者独占；本卡不读其活动候选、不依赖 planned material read，也不使用旧 Model lease/read 构造 HTTP 测试前置。目录和接口改动无交集；整合时仍核固定输入 delta |
| app/root 后继 | 真实注册 Model Audit authority、Secret usage router、Model event producer/catalog，初始化服务、挂载 handler 和管理生命周期；当前只验库级组合。生产根目前 Account 绑定不能被写成 Model 已上线 |

不涉及 Provider 调用、wire adapter、Resolver、snapshot/call/Invocation/Usage、Project/Summary、D10/领域绑定/R4 或 Object 实现。Summary 初值/Settings 产品问题继续待答；暂停的 Object 实现保持停止。本卡不启动 Outbox delivery，也不以事件 append 证明未来 handler/consumer 已绑定。

## 2. Secret 被动查询的正式增量

在新 `secret/contract/write_lookup.go` 定义以下形状；不扩旧 lease/usage 接口。`f`/`id` 分别为 foundation/identity contract：

```go
type WriteCommandLookupRequest struct {
    Actor           id.Actor
    Scope           id.Scope
    Identity        f.CommandIdentity
    Kind            MutationKind
    Ref             CredentialRef
    ExpectedVersion f.Version
    Purpose         Purpose
}
func (WriteCommandLookupRequest) Validate() error

type WriteCommandObservation struct {
    Observed bool            `json:"observed"`
    Result   *MutationResult `json:"result"`
}

type HumanWriteCommands interface {
    ExecuteWrite(context.Context, WriteRequest) (MutationResult, error)
    Metadata(context.Context, id.Actor, CredentialRef) (Metadata, error)
    LookupWriteCommand(context.Context, WriteCommandLookupRequest) (WriteCommandObservation, error)
}
```

`*secret.Service` 实现新增 `LookupWriteCommand`；其它两个方法沿现有实现。此接口聚合已有正式行为供 HTTP 消费，没有新工厂、全局注册表或第二写入实现。新查询仅接受 Human + SystemScope + Purpose=Model；其它结构合法 scope/purpose 也不在本次能力内，返回 InvalidArgument，不委派 Project。旧 ExecuteWrite/Metadata 的作用域不因此缩减。

请求 Validate 要求：当前 Actor 合法 Human；Identity namespace=`secret`、command 等于 Kind、OwnerIDs **精确为当前 UserID 一项**；key 沿既有 1–128 字符规则。Kind 闭集 create/update/delete。create 禁带有效 Ref 和非零 ExpectedVersion；update/delete 必须同 System Ref 与合法 ExpectedVersion，且其后继版本不溢出。Purpose 必须 Model。没有 Value、材料摘要、原 ActorUser/Session 覆盖参数、外部 receipt ID 或 caller-declared committed 字段。Actor/Identity 的普通 formatting/JSON/log 保持安全，不将 `Canonical()` 当日志字段。

### 2.1 已有事实与零迁移证明

固定 [00003](../../../db/migrations/00003_secret.sql) 的 `secret_command_receipts` 保存 `scope/scope_key/command_digest` 唯一键、`mutation_kind/credential_id/purpose/result_version/deleted`，在原真实 mutation/Audit 同 Tx 插入（`secret/write.go:351–381`）。原 command digest 是对 `Identity.Canonical()` 调用 `cursor.Digest`（`:120`），已包含 namespace、Human UserID、类别、原 key；scope 另在查询条件中，不是材料摘要。

这些持久列足以证明**该命令身份的历史结果曾提交**。既有 Account ServiceWrite 使用独立 `secret.account` namespace，且 purpose 仅 System/SMTP，不能与本口 Human `secret` + Model 身份混用。它们不能证明调用方此刻持有的 value 与原 value 相同，也不证明 credential 当前存在/可用。因此新查询不读取 `digest_payload_id` 指向的密文，不解密 receipt digest 或 value，不读当前 secrets/lease/reference，不调用 PrepareWrite/ExecuteWrite/LookupWrite、writable/control/nextNonce，不创建 Audit/Event/命令行。无新列、索引或凭据存储。原完整语义 digest 继续只由现有 ExecuteWrite/LookupWrite 路径比较，不能被观察结果替代。

### 2.2 同 Tx 当前授权与结果状态

1. 新入口自行安全检查 nil/零 Service、零 state、缺 Store/Sessions/System（含具名 nil chan 等 typed nil），返回 DependencyUnbound；不改旧共享 nil helper。不要求写入可用性或分配写 key epoch；服务构造有效且授权/Store 可读即可查历史 receipt。
2. 用新的只读用途 recovery cause 开短 Tx；一次 `AcquireAll` 取得原 `CommandLock(Identity)` SH 与当前 User SH，沿 D03 全局排序。取得同 Store executor，并 RequireHeldLocks 后，调用现有当前 Session/System 授权，intent=Read，核真实 grant Matches。Account 的 `RequireCurrentSession` / `AuthorizeSystem` 在同 Tx/同 User 锁下核 Session 和当前 admin；HTTP 前置检查不替代它。新查询不调用领域外私表。
3. 计算原 command digest，以 System scope/key 精确 SELECT receipt。未见行仅记录暂时 absent。见行则解析合法 UUID/version/purpose/类别，核请求 Kind/Purpose、update/delete 的 Ref 以及 `result_version=expected_version+1`；create 要求 version=1；`deleted` 必须与该 Kind 一致。不同请求 Ref/Purpose/expected 与已有合法 receipt 不符，返回 IdempotencyKeyReused，零结果；存储形状自相矛盾/非法则 DependencyUnavailable，不降为 absent。不扫描其它类别/UserID 下相同裸 key。
4. 只在本次读 Tx **Committed** 后交付观察值。授权/锁/Store/SQL/取消错误及读 Tx NotCommitted/Unknown 都返回零 observation + 错误，不能伪装成 `Observed=false, nil`。保留真实 foundation 错误/commit state；读 Tx Unknown 是本次观察失败，不是对原写结果的新判定。

| 输出 | 唯一含义 |
| --- | --- |
| `Observed=false, Result=null`，无错误 | 此次受当前权限保护的读取未观察到该身份的 receipt；不证明 NotCommitted、原 writer 已终局、从未开始或可以换 key |
| `Observed=true, Result=<safe historical mutation result>`，无错误 | 全 scope/identity/kind/ref/expected/purpose 匹配的历史 receipt 已提交；只返回原 ref/purpose/version/deleted，不提供 value、摘要或当前可用性 |
| 非 nil error | 本次查证未成功，Result 必须 nil；不返回部分成功或猜测状态 |

同 User 的新合法 Session 可查原命令；另一 admin 的同裸 key 属不同身份，不能代入原 UserID。不同类别也属于不同命令身份。credential 后续更新/删除不覆盖旧 receipt：原 create/update 的 Deleted=false 不等于当前存在，旧 delete 的 Deleted=true 不等于当前全域 cleanup 完成。绝不把历史版本当当前版本，或把 true 当作任意新材料的重放证明。

### 2.3 唯一旧源码接缝

只在 `secret/write.go` 的 **ExecuteWrite 入口**新增 nil/零 Service/state 防护，返回 DependencyUnbound、零 MutationResult。固定旧链为 ExecuteWrite→PrepareWrite→prepareWrite→writable→`state()` 调用 nil data，会在合法请求进入零 Service 时 panic。只处理这类无有效 Service 的入口；有效 Service 的原参数校验优先级、Prepare/nonce/Tx/授权/Audit/receipt/commitError 一行语义不变，不顺便修改 PrepareWrite、Metadata、通用 service.go 或其它路径。该窄接缝已获主线程规格采纳，业务仍需整卡独立审查后另授。

## 3. Account 窄安全包装与 handler 构造

新 `account/http_boundary.go` 公开具体不可伪造内部状态的 `HTTPBoundary`，只包装同包既有安全核：

```go
func NewHTTPBoundary(core *Service, publicOrigin string) (*HTTPBoundary, error)
func (*HTTPBoundary) CheckRequest(http.ResponseWriter, *http.Request) error
func (*HTTPBoundary) RequireSystem(*http.Request, identity.AccessIntent) (identity.Actor, error)
func (*HTTPBoundary) WriteProblem(http.ResponseWriter, *http.Request, error)

// model 包；命名不覆盖已有 Service/New。
type SystemHTTPOptions struct { PublicOrigin string }
func NewSystemHTTPHandler(core *Service, accounts *account.Service,
    writes sc.HumanWriteCommands, options SystemHTTPOptions) (http.Handler, error)
```

- 构造零 SQL/网络/初始化/后台任务，验证真实 Model state、Account `httpValidateCore`、PublicOrigin 及新 port 非 nil（含具名 nil chan）。内部调用 Account wrapper 构造，不新增 Secret 工厂。不能仅靠接口非 nil 声称服务已初始化；零 Secret Service 调用由 §2.3/新查询明确拒绝，正常 fixture 必须真实构造服务。不得通过 recover 将依赖 panic 当成功。
- `CheckRequest` 先设置 no-store/nosniff/no-referrer，调用原 `csrfCheck`，沿旧精确 Host/Origin/Fetch-Metadata 规则，再按原 predicate 拒绝 RawPath、未规范路径、反斜杠/NUL；不先路由重定向、解码 body 或查身份，不信 X-Forwarded-*。nil request/URL、nil/零 boundary 安全拒绝。
- `RequireSystem` 自身复核同一 Origin/path 边界，不能由 caller 漏调 CheckRequest 绕过；复用 `csrfSession(core,r,csrfUnsafe(r.Method))` 和真实 Account Authority。intent 只接受 Read/Mutate；**POST lookup 的 intent=Read，CSRF 仍按 unsafe HTTP 方法验证**。其它命令 intent=Mutate。Actor 唯一来自真实当前 cookie Session，不能接受请求体/上下文中的自报 Actor。
- `WriteProblem` 复用安全 Problem 投影，request/URL 为 nil 也不 panic；Instance 固定安全 `/api/v1`，不回显未知路径/query/材料。仅 Unauthenticated/SessionRevoked 沿原规则清 Session cookie，其它错误不误清。旧 Account handler 不改、不绕到本 handler。
- handler 外层只调用边界再路由；已知路由先 RequireSystem，再解析 key/query/body，最后调用真实业务。服务内部仍在各原 Tx 重验权限。共享 WithRequestID/Recover/logger 只由外层安装一次；handler 不自行再包日志/恢复，不输出 raw request body、cookie、CSRF、key 或 Secret diagnostics。

## 4. 路由、严格 DTO 与响应

以下路径统一前缀 `/api/v1`。所有成功为 200 JSON（delete 也保留 receipt）；GET 同时支持 HEAD，执行相同权限/查询但不发 body。未知路径安全 404，已知路径不支持的方法 405 + 精确 Allow；不新增 CORS 或匿名 fallback。只有表内列表允许 query，其它路径拒绝 query/ForceQuery。body 上限 1 MiB，使用既有 `httpapi.DecodeJSON`，禁止未知/重复键、尾随 JSON、非 UTF-8、错误 media/encoding、非法 null/数值。

| 路径/方法 | 输入与真实调用 |
| --- | --- |
| `GET/HEAD /system/model-providers` | `cursor?`, `limit?`，ListProviders |
| `POST /system/model-providers` | `{input: ProviderInputDTO}`，CreateProvider |
| `GET/HEAD /system/model-providers/{id}` | GetProvider |
| `PUT /system/model-providers/{id}` | `{expected_version,input}`，UpdateProvider；完整替换，不是 PATCH |
| `DELETE /system/model-providers/{id}` | `{expected_version}`，DeleteProvider，无级联 |
| `GET/HEAD /system/models` | 必填单一 `provider_id`，另 `cursor?`, `limit?`，ListModels；不编造现有服务没有的全域列表 |
| `POST /system/models` | `{provider_id,input: ModelInputDTO}`，CreateModel |
| `GET/HEAD /system/models/{id}` | GetModel |
| `PUT /system/models/{id}` | `{expected_version,input}`，UpdateModel，不允许改 provider_id |
| `DELETE /system/models/{id}` | `{expected_version,replacement}`；replacement 必须显式 UUID 或 null，DeleteModel |
| `GET/HEAD /system/model-selection` | GetPlatformSelection；技术 singleton 的 `configured=null` 仍表示未配置，不补默认 |
| `PUT /system/model-selection` | `{id,expected_version,embedding,memory,reranker,image}`；后两项显式 UUID/null，前两模型必填，构造 Selection.Version=expected_version，UpdatePlatformSelection |
| `POST /system/model-commands/lookup` | `{command}`；原 Idempotency-Key，LookupCommand，intent=Read |
| `POST /system/model-credentials` | `{value}`，独立 Secret create |
| `PUT /system/model-credentials/{id}` | `{expected_version,value}`，独立 Secret update |
| `DELETE /system/model-credentials/{id}` | `{expected_version}`，独立 Secret delete |
| `POST /system/model-credential-commands/lookup` | `{kind}`（create）或 `{kind,credential_id,expected_version}`（update/delete），新 LookupWriteCommand，intent=Read |

所有 POST/PUT/DELETE 都要求唯一合法 Idempotency-Key，包括两个 lookup（它是**原命令 key**，不是新查询命令）。路径/DTO ID 均规范 UUIDv7；Actor、Scope、Purpose、command owners 由受信 HTTP 适配重建，body 禁带这些字段。Model command 闭集沿原七项 `provider.create/update/delete`、`model.create/update/delete`、`model.selection.update`。列表参数不接受未知/重复/空键值；limit 缺省 50，合法十进制 1–100；cursor 原样传入已签名分页，不能跨 User/provider 复用，也不代替每页当前授权。

### 4.1 配置 DTO

- ProviderInputDTO：必填 `name,protocol,base_url,enabled,credential_ref,options`。credential_ref 显式 System credential UUID 或 null，由适配构造 System CredentialRef；无 scope 输入。options 为对象，不接受 null。名称/URL/profile/schema/大小、禁认证/transport overwrite 等由已定 Model 验证核处理，不在 HTTP 另定协议规则。
- ModelInputDTO：必填 `name,provider_model_id,type,enabled,parameters,request_overwrite,header_overwrite,capabilities`。前三 JSON 配置字段依次为对象、对象、字符串值 map；均不接受 null。capabilities 的四个 bool、四个字符串数组沿既有同名字段，另 context_length/max_output 显式 null 或正整数字符串；必填 bool 的 false 不等于遗漏，空数组用 `[]`。完整构造现有 ModelInput，再交真实服务校验 profile/type/能力/参数相容性。
- Provider 输出明确投影 `id,input,version,created_at,updated_at`；Model 输出另有 provider_id，input 沿上述 DTO。System scope 由路由固定，不透传任意内部 struct。当前 admin 可以读已存配置；没有 credential value、payload/digest、actor 或内部计划。列表为 `{items:[],next_cursor:<string|null>}`，无下一页用 null。
- selector 输出 `{id,version,configured:null|{embedding,memory,reranker,image}}`。更新不能清必需 embedding/memory；合法可选 null、引用类型/能力、删除替换及缺 owner adapter 的全事务拒绝继续由原业务执行，不能将 Agent/project_summary 缺 adapter 当无引用。
- Model 写成功输出原 CommandReceipt 的 `kind,resource_id,version,affected_references`；查询输出 `{found,receipt}`，false 时 receipt=null。它同样是历史身份观察，不验证某个新 DTO 的重放语义，也不提供原 writer 终局证明。不要为写成功响应追加 canonical GET 而丢掉已提交 receipt；需要当前状态由独立 GET 获取。
- version、expected_version、TokenCount/affected_references 等 int64 沿公共十进制字符串规则；拒 number、负数、前导零、溢出，>2^53 精确往返。时间沿既有 Instant。OpenAPI 明确必填/nullable/闭集/additionalProperties 和两种 cookie 模式，不改变 `common.json`。

### 4.2 Credential 命令及安全材料

value 是仅输入的非空 JSON 字符串，UTF-8 bytes 不超过 Secret MaxValueBytes=65536。用私有安全 scalar/DTO，普通 Format/JSON/slog 不回显；完整严格 body 解码成功后才创建 SecretMaterial，并在全部成功/失败/取消路径 Destroy/清理本方 byte buffer。不得承诺 Go runtime 中一切临时 string 可擦除，也不得把临时解析副本保存到 closure、receipt、队列或日志。没有 credential GET/明文回读路由。

每个写入构造当前 Human、SystemScope、Purpose=Model、namespace=secret、owners=[UserID]、对应 kind 与原 key 的现有 WriteRequest。create 直接执行既有 ExecuteWrite。update/delete 为防通用 Human 写口被用于其它 purpose，按以下有界流程：

1. 调用新被动 lookup，要求全 scope/key/kind/ref/expected/purpose 匹配。命中历史 Model receipt 时，只跳过当前 metadata 前置；**仍执行原 ExecuteWrite，比较完整原值语义 digest**，不能把 observation 转成写成功。这样原请求可以在 credential 已删除后合法重放，相同身份换材料仍冲突。
2. 未命中时调用真实 Metadata，只接受 Purpose=Model、同 System ref、Version=expected_version。非 Model ref 或不存在返回 NotFound；版本变化返回 VersionConflict。Metadata 仅用于限域，不证明旧命令终局。原 ExecuteWrite 必须保留该 expected_version，在真实同 Tx 再核版本，防 metadata 读取后 purpose/version 改变的 TOCTOU。
3. metadata 前置失败时可再被动查证一次，处理并发原命令刚提交的 receipt；仅命中才进入原 ExecuteWrite，否则返回原前置错误。不无限轮询，不忽略 lookup 错误，不以 false 发起新 key。每次写 HTTP 请求最多调用一次 ExecuteWrite；其 Unknown 后立即返回原错误，不能自动再执行。

Secret 写成功和被动观察中的安全结果统一为 `{credential_id,purpose:"model",version,deleted}`；query 外层为 `{observed,result}`。Provider 绑定仍是另一次独立 Model 命令：Secret 成功而绑定失败时保留原 receipt/ref，不声明两 HTTP 原子、不会自动删除 credential，也不把 Provider 删除变成 Secret 删除。

## 5. 错误、提交与真正原子性

直接复用 `httpapi.WriteProblem` 的 foundation 映射：InvalidArgument/CursorInvalid 400；Unauthenticated/SessionRevoked 401 并清 Session；Forbidden/CSRFFailed/OriginDenied 403；NotFound 404；MethodNotAllowed 405；VersionConflict/IdempotencyKeyReused/ResourceBusy/InvalidState 409；PayloadTooLarge 413；UnsupportedMediaType 415；已定 capability/schema 错误 422；DependencyUnbound/Unavailable、CommitUnknown 503；真实 InternalError 500。不拆掉 error 链重标安全码或猜 commit_state。

只有服务最终返回 Unknown 时，HTTP 才沿 Problem 的 `retry_hint=lookup`、原 command identity 查表中对应 lookup 路径，不换 key。Model 底层 Tx Unknown 会进入已有 `replayScope`；确认提交及匹配 receipt 成功后返回正常 200 与原 receipt（固定 `model/commands.go:288–289,389–390`），不能因曾观察到下层 Unknown 强行改为 503。Secret ExecuteWrite 沿原实现直接返回的 Unknown 则映射 503。响应不提供 raw cause、SQL、nonce、摘要、材料或伪造 attempt/final 状态；查证读事务本身失败不抹掉原写 Unknown。Receipt 成功后存在当前权限撤销，后续 lookup 仍必须当前授权，不能因为知道旧 key 放行。

Model 原 Tx 的 canonical 数据、Secret reference、typed Audit、Outbox event 仍完整原子；Secret 独立写的 payload/metadata/receipt/Audit 仍完整原子。HTTP 不读写这些表，也不替代原 producer/fact checker。发生任何缺依赖/错绑定/变更后拒绝，必须无相应业务副作用，不造宽松 production allow。原 Secret 写准备可能先预留技术 nonce；后续授权/业务失败不回退或复用该 nonce，本卡不改此协议，回滚断言须与业务事实区分。只读查询无新的 Audit/Event/receipt/nonce/payload 写入；当前 Account 认证可能沿既有规则更新 Session 活跃事实，不能将“无 Secret 写入”夸成全进程绝无任何数据库写入。

## 6. 精确实现所有权

独立静审采纳后，由主线程另授一个作者 **17 路径**：8 Go 生产（7 新 + 1 旧窄口）、1 新 OpenAPI、8 新测试。现在唯一写权仅为本卡，不授下面源码。

| 路径 | 唯一责任 |
| --- | --- |
| `internal/central/account/http_boundary.go`（新） | §3 原核窄包装；不改旧 Account handler |
| `internal/central/secret/contract/write_lookup.go`（新） | §2 请求/观察/窄 HumanWriteCommands port |
| `internal/central/secret/write_lookup.go`（新） | 被动读取、同 Tx 当前授权、结果校验与安全零值处理 |
| `internal/central/secret/write.go`（旧） | 仅 §2.3 ExecuteWrite 入口零值防护 |
| `internal/central/model/http.go`（新） | 构造、路由、System 当前安全边界 |
| `internal/central/model/http_configuration.go`（新） | Provider/Model/selector/原命令查证的真实调用 |
| `internal/central/model/http_credentials.go`（新） | credential 独立写、purpose 边界、有界查证与材料生命周期 |
| `internal/central/model/http_dto.go`（新） | 明确安全 DTO/投影、参数与 wire 校验 |
| `api/openapi/model-system.json`（新） | §4 的精确 wire 合同及错误/查证含义 |
| `internal/central/account/http_boundary_test.go`（新） | 原核等价、构造/零值、Origin/CSRF/清 cookie |
| `internal/central/secret/contract/write_lookup_test.go`（新） | scope/Actor/identity/类别/ref/version 闭集与安全投影 |
| `internal/central/secret/write_lookup_test.go`（新） | 零依赖/零 Service、完整锁/错误/结果；nonce 状态不消费 |
| `internal/central/model/http_test.go`（新） | 路由/OpenAPI 一致、typed nil、边界调用、一次 middleware |
| `internal/central/model/http_dto_test.go`（新） | 严格 DTO、nullable/精确整数、材料与 safe errors |
| `tests/model/system_http_fixture_test.go`（新） | 真实 Account/Model/Secret/Audit/Outbox/PG 组合及有限观察器 |
| `tests/model/system_http_configuration_test.go`（新） | 配置/selector/分页/权限/原子性真实风险组 |
| `tests/model/system_http_credentials_test.go`（新） | credential/被动 receipt/Unknown/历史重放真实风险组 |

本集合与 Secret usage rev1.1 的 20 路径无交集，不改其 `contract/account.go`、`contract/model.go`、lease、Model read、Project checker 或旧测试。也不改 Model 原服务/contract、Account 旧源、D03、Audit、Outbox、SQL、公共 httpapi/common.json、脚本、依赖锁、app/root、状态页或已冻结报告。必要私有 helper 放白名单内；若发现必须改变原有效 Service 的行为、公共契约扩大到 Project/其它 purpose 或必须追加迁移，先报具体证据与最小范围，不自行扩大。

## 7. 验收门槛与真实证据

权限、敏感材料、历史 receipt 与事务边界属高风险，必须由未参与实现的验收者独立审查和动态验证。普通测试 stub 可验证 wire 分支，但下列真实组不能用永远 allow 的 Session/System/Model/Secret/Audit/Event 替代。使用真实 Account 发放的 Session/cookie/CSRF、真实 Account Authority、Model Service/Authority、Secret Service/Keyring、Audit.Service、Outbox Appender/catalog 与 PG；Model authority 与 Account fallback 按正式 router 装配。非本次路径的未来 consumer/外部 delivery 保持明确未绑定，不发真实 Provider 请求。

| 新真实顶层测试 | 必须出现的观察与反例 |
| --- | --- |
| `TestModelSystemHTTPConfigurationCRUD` | 真正 HTTP CRUD、各已支持配置 type/profile、safe receipt、get/list；同 key 原输入重放只一组 Audit/Event，语义变化冲突；Provider 有 Model 不可删，无引用 Model 可删 |
| `TestModelSystemHTTPSelectionAndReferenceDeletion` | 未配置 selector 原样读；四项原子更新/版本冲突；合法替换、必需项不得清空；真实非平台 owner 索引且缺 adapter 时整 Tx 拒绝，不能用空集合 |
| `TestModelSystemHTTPCurrentAdministrator` | 普通用户/Agent/伪 Actor 不放行；真实 Session 撤销或 admin 角色变化在入口后、业务 Tx 前发生，业务重验拒绝；每页/lookup 重新当前授权；另一 admin 同 key 不冒领 receipt |
| `TestModelSystemHTTPBoundaryAndStrictWire` | 真 cookie/CSRF、Host/Origin/重复 cookie/header、POST Read lookup 仍需 CSRF；不规范路径不 redirect；重复嵌套 JSON/unknown/null/超限/错误 content-type；安全 404/405/清 cookie、HEAD 无 body；错误不回显输入 |
| `TestModelSystemHTTPPaginationAndSafeProjection` | 默认/边界 limit、签名 cursor 跨 User/provider 拒绝；System admin 安全投影；Project ID/ref 不能借 System 路由访问；DTO 精确大整数及 schema/route 一致 |
| `TestModelSystemHTTPCredentialWritesAndBinding` | 真实 AEAD 写、metadata/receipt/Audit；独立 Provider bind 的成功/失败；System 非 Model ref 的 update/delete 拒绝且原数据不变；metadata 后并发改 purpose/version 导致原 expected Tx 拒绝 |
| `TestModelSystemHTTPPassiveCredentialLookup` | create/update/delete 的 true 历史结果；未见 identity 的 false；全 identity/kind/ref/expected/purpose 与另一 User 反例；更新/删除后仍读原 receipt；无 payload/digest 读取、nonce/receipt/Audit/Event 新增；观察不触发 Execute |
| `TestModelSystemHTTPPassiveLookupLocksAndFailures` | 实际 User/Command 锁竞争和当前 Session/admin 检查；错误不能包装 false；同 Store/Tx/held locks 必须真实成立；读 Tx Unknown 零结果；重建 Service 尚未 Initialize/不可写时仍可授权查已有 receipt，零 nonce 消费 |
| `TestModelSystemHTTPAtomicEffects` | 真 Audit/Event 拒绝导致 Model canonical/reference/receipt 全回滚；Secret Audit 拒绝导致 Secret payload/metadata/receipt 全回滚；比较完整前后事实，不能只数新增行 |
| `TestModelSystemHTTPUnknownAndHistoricalResults` | Model 真实最终业务 Tx 被投影 Unknown 后，原恢复确认 receipt 成功应 200 + 原 receipt；只有服务仍 Unknown 才是 503 lookup。Secret ExecuteWrite 的实际 Unknown 直接 503；原 key passive true/未见 false 含义；lookup 绝不重执行/解密；已删 credential 原请求完整重放成功，同 identity 错材料仍冲突；撤销后不能查旧成功 |

Unknown 分支可用受控 Store 包装，**先实际执行底层最终业务 Tx**，记录其真实结果、向服务投影的 Unknown、原确认流程及服务最终结果；按原 command/receipt/canonical 语义命中，不按第 N 个事务计数。实际 Committed 后仅装饰 Unknown 不保证 Model HTTP 503；Model 的 503 场景须以正式 Store 结果或普通有界 ctx 使原确认未成功，并核服务确实仍返回 Unknown，不改 Model 生产、不新增网络代理。该证据只证明应用处理 Unknown，不称真实网络 COMMIT 丢回复或原 writer 仍存活；不恢复暂停的网络/ROLLBACK 探针。未见 receipt 的普通读取不证明负向提交终局。锁竞争仅使用普通 task-owned PG 事务及有界 ctx，不降低原错误/原子性断言来通过。

必须复用或重跑适用原回归：`TestModelB01CRUDCurrentAuthorityAndCanonicalReplay`、`TestModelB01PlatformSelectionAndAtomicReplacement`、`TestModelB01SecretReferencesAndAllEffectsRollback`、`TestModelB01CursorAndScopeRemainCurrentlyAuthorized`、`TestModelB01PreparedReferencesAndCurrentAdminAreRechecked`；Account 的 `TestAccountHTTPBoundaryAndProfile`、`TestAccountHTTPAdministratorPagesSettingsAndCurrentAuthority`、`TestAccountCurrentSessionRequiresRealHeldUserAndCurrentRole`；Secret 的 `TestSecretStorageReceiptAndCurrentLease`、`TestSecretBindingsAuthorizationAndReadAuditBoundary`。固定输入不变的原证据可按风险复用，不能把尚未验的活动 Secret20 算作本卡通过；整合其已验提交后再核相关 delta。

执行时固定 Go 1.27.1、`GOTOOLCHAIN=local`、`GOPROXY=off`、`GOSUMDB=off`、`GOWORK=off`、`-mod=readonly`，自有 `/workspace` GOCACHE/TMPDIR。先对 Account/Secret/Model/httpapi 的相关包运行 unit/race/count=1 与 vet，新增 integration 包作 `-tags=integration -race -run '^$'` 编译，并实际 build 两个 cmd 到自有目录。OpenAPI JSON/引用/路由/schema 与文档链接单独核对；不得为检查覆盖扩大旧测试写权。

真实组经主线程交接唯一 PG/MinIO fixture 窗口后，使用既有脚本与已冻结 selector；保留原 `-race -count=1 -timeout=6m`，verbose 单列实际顶层/子例，记录真实 argv/env/exit、固定输入 SHA 与原始失败，不自增预算或用过滤器漏掉红例。独立验收针对本卡的当前权限、被动查询和材料重放选择实际风险探针，不机械重复全部作者场景；真实窗口结束核 task-owned exact ID 两遍 absent、原基线不变、所属进程 0/runtime 空再交还。

## 8. 冻结与交付边界

作者冻结 17 源清单与小型原始证据后停止写入；报告区分纯检查、真实库组合、底层实际 commit 与向上层 Unknown、复用项、失败和未验证范围。库级通过只证明上述 System HTTP 和 Secret 被动观察，不能声称 Model 调用、Project/Summary、真实外部账号、生产根或整体 D09 已完成。主线程核独立结果后负责 Git 与状态页；本设计者自查不是独立验收。
