# D09 OpenAI Chat 文本 wire 库恢复卡

修订 2，2026-10-05。状态：**规格及来源证据已独立复审通过并由主线程采纳，按文末授权实施；尚未验收业务行为。** 固定生产基线 `de00c610da62cb77cc03efe7c3cc842cf81f1ba5`，已含 Project 配置；本卡只消费已验 D04 与 C0，不消费活动 Artifact 或 Project Runtime 源。唯一规格写者为 `restore_test_dependencies`。

## 1. 完整结果与依赖

交付一个可供后继 Model Runtime 直接复用的 **OpenAI Chat Completions 文本 wire 库**：受限请求编码、一次真实 D04 POST、普通响应或 SSE 解码、原 Provider usage、安全错误，以及实际响应体/transport/parser 终局。受控私网 server 验证实际请求及故障；不把协议解析成功称为业务调用已提交。

依据 [D09 工程规格修订 4](d09-model-system-token-usage-design.md) §4–7/12/13、[Chat Runtime 架构](../../architecture/platform-infrastructure/model-system/chat-model-runtime.md)、已验 [C0](d09-model-system-token-usage.md) 与 [Project 配置卡](recovery-d09-project-configuration.md)。实施读 [Go 技能](../../../.agents/skills/agenteam-go-development/SKILL.md)，审查读 [验收技能](../../../.agents/skills/agenteam-verification/SKILL.md)；公共所有权与停止规则沿 [AGENTS](../../../AGENTS.md) 及 [团队流程](../agent-team/README.md)。

| 已有事实 | 本卡使用与明确边界 |
| --- | --- |
| C0 Message/ConfigSnapshot/Usage/ModelError | 复用值类型与校验；Snapshot 结构合法不证明已提交或有权调用。本库不签发 ConsumerDependencies，不实现 `mc.Chat` 或 `mc.ModelStream` |
| D04 PolicyService、TrustStore、Client.Do、CredentialBinding、Response/NetworkError.Decision | 使用原实现；不创建直连 HTTP client、放宽地址分类/证书、读部署凭据或改 D04 生产源 |
| 当前 Session/System、Audit、PG 与 owned outbound fixture | 测试用真实 Account 管理员更新隔离 policy，真实 Audit；合成 credential canary 明示为协议测试输入，不称 Secret Model resolve 已绑定 |
| 后继 Model Runtime | 独占 consumer/Project/输入授权、真正 snapshot/lease、Secret exact Invocation read、发送资格、重试、Invocation/Usage 持久化和生命周期。当前均不能由本库替代 |

Resolver/Invocation/Usage 的当前硬前置仍在：ConsumerAuthority 只有契约，合法 Agent/Execution/Meeting/Knowledge/Memory/Tool 消费事实未接；Secret Model lease/read 分支 unbound，RequestID reader 尚未实现；Call/Invocation 持久写计划也未冻结。因此不先造永远为零的 Usage 服务或以普通 Owner 批准伪 consumer。本卡不需要 Summary 初值产品决定、不新增契约/迁移/HTTP/root。Jina、其他协议、型号矩阵和真实供应商账号 smoke 不在本结果中。

## 2. 库级 API 与唯一资源 owner

新包 `internal/central/model/adapter`。下列是本卡正式 Go 形状；`mc` 为现有 `model/contract`，`sc` 为 `secret/contract`，`identity` 为 `identity/contract`。字段有意不含 CallID/InvocationID/Consumer/lease 身份；本库不能构造它们的成功 receipt。

```go
const OpenAIChatTextRevision = "openai-chat-text-v1"

type ResponseMode string
const (
    JSONResponse ResponseMode = "json"
    SSEResponse  ResponseMode = "sse"
)
type Transport struct {
    Policy   *outbound.PolicyService
    Trust    outbound.TrustStore
    Resolver outbound.Resolver // nil 使用 D04 正式默认；typed nil 拒绝。
}
type Request struct {
    Snapshot       mc.ConfigSnapshot
    Messages       []mc.Message
    Tools          []mc.Tool
    ToolChoice     mc.ToolChoice
    ResponseFormat mc.ResponseFormat
    Mode           ResponseMode
}
type CallOptions struct {
    ProjectID  identity.ProjectID // 仅受信 caller 的调度 key，不是 grant。
    Context    outbound.CallContext
    Credential sc.SecretMaterial  // 必需；借用至 Exchange.Joined。
    AllowHTTP  bool
    Limits     outbound.Limits
}
type Budget struct { /* private */ }
func NewBudget() *Budget
func (*Budget) StopAdmission()
func (*Budget) Drain(context.Context) error
func (*Budget) Force(context.Context) error
func (*Budget) Joined() bool

type OpenAIChat struct { /* private */ }
func NewOpenAIChat(Transport, *Budget) (*OpenAIChat, error)
func (*OpenAIChat) Start(context.Context, Request, CallOptions) (*Exchange, error)

type End struct {
    FinishReason      mc.FinishReason
    Usage             mc.Usage
    ProviderRequestID string
}
type Result struct { Text string; End End }
type EventKind string
const (
    TextDelta   EventKind = "text_delta"
    UsageUpdate EventKind = "usage_update"
    StreamEnd   EventKind = "stream_end"
)
type Event struct {
    Kind  EventKind
    Text  string
    Usage *mc.Usage
    End   *End
}
type Observation struct {
    DoStarted bool
    Decision  *outbound.Decision
    Usage     mc.Usage
}
type Exchange struct { /* private */ }
func (*Exchange) Result(context.Context) (Result, error) // 仅 JSONResponse。
func (*Exchange) Next(context.Context) (Event, error)    // 仅 SSEResponse。
func (*Exchange) Observe() Observation
func (*Exchange) Close(context.Context) error
func (*Exchange) Joined() bool
```

构造只捕获一次配置/预算，不读 policy SQL、开 socket、启动 goroutine或预先授权。必需依赖 nil/可判定零值拒绝；按 D04 正式构造校验 Policy/Trust，不反射读取其私有状态。非 nil opaque Resolver 不证明健康。Request/Options 只供受信 Go 调用，不增加 JSON/HTTP 入口；所有可变 slice/map/指针在接收及返回时复制。含正文/endpoint/material/错误的类型实现安全 Format/LogValue/MarshalJSON，默认只输出固定标签；显式字段仅交回受信调用者。

共享 **一个具体 Budget** 是组合要求；未来 root 为整个 Model wire 层创建一次并注入所有 adapter，不可每请求新建。固定 active 上限 64、每 Project 8，来源是 D09 §7；本窄库**不排队**，满额立即 `ResourceBusy/NotStarted`、零 Do、无 Exchange。D09 最多 128 项的业务等待队列留给后继 Runtime，不能复制成每 client 的队列。校验失败/已取消/封 admission 同样在接受前返回，无隐藏 worker。接受时先登记 exact Exchange 并占 slot，随后才造本次 Client/启动工作；返回 handle 后任何失败均归该 handle 管理。

首块只做 Project chat。ProjectID 必须有效但不从 System credential/snapshot 推导，也不授权 Owner/consumer；后继 Runtime 必须传自己已核的 canonical Project。未来 System embedding/nonchat 不得暗套此调度 key。Budget.StopAdmission 只封新接收，Drain 等现有工作，Force 先向全部 handles 请求取消再以同一传入剩余预算等待；不得逐项续期。Budget.Joined 仅 admission 关闭且全部 accepted handles 实际终局。slot 不因请求 ctx 返回、错误、取消或 parser EOF 提前释放。

## 3. 支持闭集与一次请求

- Profile 必须 `mc.OpenAIChatV1`、Protocol=`mc.OpenAIChat`、ModelType=`mc.ChatModel`，Snapshot.Identity.AdapterRevision 必须上述精确 revision。这是 wire 修订，不证明所填型号/账号可用；以后要兼容新修订须显式增加测试，不忽略旧 snapshot 的 revision。
- Snapshot 按 C0 Validate；本修订只接受空 parameters/request_overwrite、空 header_overwrite，输入/输出 modality 仅 text。ToolCalls/ParallelToolCalls/Reasoning 必须 false，ReasoningEfforts 空，StructuredOutputModes 只允许空或 text；SSE 还要求 Streaming=true。MaxOutput 若有则正整数，编码为 `max_completion_tokens`；其余参数不猜默认。ContextLength 只作已给配置约束，不估算 token。
- Messages 非空，最多 256 条，每条恰一个 TextPart，role 仅 system/user/assistant；保留精确 UTF-8 正文，不 trim、不拼入额外 prompt。Tools 必须空，ToolChoice=none，ResponseFormat=text。有效但不在闭集的工具、媒体、metadata、reasoning、structured/native overwrite 请求返回 `unsupported_feature` 且零 Do；结构非法返回 InvalidArgument。不得静默丢字段或降级。
- 统一 body 固定 model=Snapshot.Identity.ProviderModelID、messages、stream；SSE 额外 `stream_options.include_usage=true`，普通请求不带 stream_options；可选 max_completion_tokens 沿上款。不发送 tools、任意调用方 header/URL/JSON。request 完成编码后的总 body 上限 16 MiB；拒重复 key、无效 UTF-8/数字和超限，不把 native 数字转 float64 后丢精度。
- endpoint 沿 C0 及 D04 ParseTarget 校验：仅 http/https，无 userinfo/query/fragment；保留配置 base 的精确转义路径，只在边界追加一个 `/chat/completions`，不自动补 `/v1`、不使用会丢 base path 的绝对 ResolveReference、不清理内部双斜线/点段。边界已有 `/` 时只处理该分隔符；编码路径须以实际 RequestURI 验收。
- 每 Exchange 新建并独占一个真实 D04 Client，共享现有 Policy/Trust/Resolver 配置；只有一次 Do、一次 POST，无 SDK/adapter 重试，无 keepalive 跨 Exchange 复用。这是初版明确的握手/连接复用代价；64 个 active slot 不变成 64 个可再自主并发的 client 池。共享连接优化须后继正式每-call 终局端口与单独验收，不改本卡 D04。
- 从最终 target.Origin 构造 D04 HeaderCredential(`Authorization`, `Bearer `, material) 与 CredentialBinding，Profile.Consumer=Model、Context=传入的正式 CallContext。认证不先写入普通 http.Header，不复制/持久化明文；不存在匿名 fallback。AllowHTTP 仅允许 D04 继续检查实际 policy，不能越过规则；Trust 只能来自既有部署 CA 口。Limits.Overall 必须显式为正且≤120s，来自 D09 单 attempt 上限；由后继 Runtime 选择 30/60/120s，wire 不按次序猜预算。ReadIdle 为零时取 min(60s,Overall)，显式值不能超该上限；其余 limit 只允许在 D04 cap 内收紧，并始终服从更短 caller deadline。SSE 使用 Streaming=true，也不能继承 D04 的 30min streaming Overall 默认。POST 3xx 由真实 D04 拒绝，不把 Redirects=0 当禁跳开关。

可观测的服务器一次请求包含 canonical path/body、Bearer canary、Content-Type/Accept；服务器请求计数及 D04 Decision 才是网络事实。Future Runtime 只投影现有 snapshot/request 到此 Request，消费 Event/Result/Observation，不重新实现 endpoint、认证、usage、SSE 或错误映射。本块不改 Model 配置准入规则来冒充已核型号。

## 4. 响应、SSE、usage 与错误

普通成功响应仅接受 status=200、Content-Type=`application/json` 的 profile 对象：恰一个 index=0 choice、assistant message、文本 content、已知 finish_reason。SSE 接受 status=200、Content-Type=`text/event-stream`；MIME 允许合法参数但不改协议。原生 envelope 的明确可读键为 id/object/created/model/choices/usage/system_fingerprint/service_tier，choice 为 index/message 或 delta/finish_reason/logprobs；message 为 role/content/refusal/tool_calls/function_call/annotations/audio，delta 为 role/content/refusal/tool_calls/function_call。refusal 只允许缺失/null/string，非空字符串按下款安全错误处理。tool_calls/function_call、audio、annotations、logprobs 或非文本 content 不被转成普通文本成功；这些不适用 optional 字段只允许 null，列表另允许空数组。未知键不推导新能力，按本 profile 的严格解码规则拒绝。id/model/fingerprint/tier 等未投影元信息仍核 SDK 字段类型与有界长度；object 精确区分 chat.completion 与 chat.completion.chunk，created 用非负 int64。profile manifest 逐项保存所采字段及固定源码定位，不能以 Go 默认忽略未知字段完成 conformance。仅 SSE chunk envelope 另接受固定 SDK 的 `obfuscation`：可缺失、null 或 string，字符串和整个原始 event 同受既有 1 MiB event 上限约束，累计网络读取沿真实 D04 `Profile.Limits.ResponseBodyBytes`（Streaming 默认 cap 256 MiB，可收紧）。验证后忽略，不投影正文、Usage、错误或日志；保留供应商默认，不发送 `include_obfuscation=false`。非 string/超 event 上限安全失败，不能借忽略绕过读取或内存上限。

`stop/length/content_filter` 原样映射为 End.FinishReason；length/content_filter 不是偷偷补全的 stop。未知 finish reason、tool/function 原因在本修订安全失败，不能发 StreamEnd。合法非空原生 refusal 返回安全 `ModelError{Category: content_filter, Retryable:false}`，不转发其正文；JSON 不返回成功 Result，SSE 只交付安全错误一次后 EOF、绝不发 StreamEnd。保留此前合法 usage；PartialOutput 仅来自实际已交付文本前缀（JSON 未交付正文时为 false），Dispatched 仅投影真实 Decision.Sent，并满足 C0 的 PartialOutput ⇒ Dispatched。refusal 结构/类型错误仍为协议错误；无非空 refusal 时原生 finish_reason=content_filter 的 End 映射保持。Result/End 仅表示 wire 协议结果；Runtime 仍须依据真实发送/终态提交决定公共 ModelResponse/Frame，不能本库先发业务 message_end。

SSE 状态机固定单 choice：处理 LF/CRLF、跨 Read UTF-8/JSON、注释、多个 data 行；event 上限 1 MiB，累计 text 上限 16 MiB，JSON 深度最多 32。角色若出现必须 assistant，index 必须 0；finish 后不得再有 choice 文本。只在已见合法 finish 且收到 `[DONE]` 后结束，HTTP EOF、choice.finish_reason 都不能替代 `[DONE]`。最终空 choices 的 usage chunk 可在 finish 后出现一次；缺 usage 仍可形成 unknown usage。重复/倒序终态、坏 JSON/重复 key、stream error、断流均失败，不转成成功默认。已经合法观察到的 usage 在随后错误后仍由 Observe 返回。

Next 串行消费事件；并发 Next 或错模式 Result/Next 为 InvalidState，不隐式启动第二个 reader。TextDelta 保持 UTF-8 且不含完整 response；UsageUpdate 与 StreamEnd 各自携带一份复制值。队列最多 32 项/256 KiB（计真实 payload 字节，不能只计指针），大文本 delta 按 UTF-8 拆分为不超过 64 KiB 的块；满时 parser 等待可取消，不能新增无界 goroutine。StreamEnd 不含累计正文，避免以最终大对象绕队列上限。正常 StreamEnd 一次，随后 EOF；错误一次，随后 EOF，Observe 仍可查询。JSON Result 不作为正文重放仓库，不签发 call receipt。

usage 按既定 OpenAI 口径：prompt/completion/total 原值分别映射 input/output/total；prompt detail 的 cached_tokens/cache_write_tokens 与 completion detail 的 reasoning_tokens 各自映射。严格非负 int64，超 MaxInt64、fraction、负数失败；缺失/null 为 nil，明确 0 保留。全无可靠字段为 UnknownUsage，否则 ProviderUsage；不自算 total、不累加 cache/reasoning、不逐 chunk 求和。原生 detail 的其他字段只能按固定 SDK manifest 明确校验后不投影，不能以任意 map 接纳。Observe 的 usage 与送出的 UsageUpdate 同源，但没有任何 DB 已提交含义。

错误返回现有 `*mc.ModelError` 或结构/资源准入的 foundation Fault，不包含原响应正文/自由 error.message/请求/材料。固定安全 category 规则：401 authentication、403 permission、429 rate_limited、502/503/504 provider_unavailable；400/404 不按状态猜 model_not_found，未知 Provider 错误为 provider_error/unknown。Code 仅本库闭集 `wire_protocol_invalid/wire_unsupported_feature/wire_limit_exceeded/wire_http_error/wire_transport_error/wire_cancelled`，本修订不返回任意原生 error.code，也不按自由 code/message 猜类别。ProviderRequestID 仅从实际 `x-request-id` header 提取规范 ASCII safe token（1–256 bytes），非法则为空，不以 response.id/error.message 替代。错误正文最多读 64 KiB，截断不解析为完整协议成功，随后实际关闭；错误格式/日志无正文/原始 header。网络 timeout/cancelled 等从真实 D04 NetworkError 归类；语义/协议拒绝不可重试，已核 429/502/503/504 或网络 timeout 可保 Retryable 属性但本库不重试。错误流同一安全规则。

## 5. 发送观察与 actual join

Observe.DoStarted 在实际调用 Do 前登记；Decision 仅复制真实 Response.Decision 或 NetworkError.Decision，nil 表示尚无该事实。DoStarted=true 且 Decision=nil **不是未发送**，后继 Runtime 必须保留 unknown。ModelError.Dispatched 只投影已知 Decision.Sent；不得只用这个 bool 把无 Decision 的窗口记成 not_sent。Request.Validate/预算拒绝发生在 DoStarted=false，可证明零 Do；HTTP status、取消、EOF 不能反推 Sent。

Client.Do 的取消分支可能早于内部 writer 返回。因此每 Exchange owner 覆盖已登记的 Do、唯一 parser、response Body.Close 与该独占 Client 的关闭/Drain；Joined 必须本地 I/O/解析 work 实际返回，且 Client.StopAdmission 后 **原 Client.Drain 真实返回成功**。Next/Result 是有界结果消费者，不是另一个 I/O worker；本库 Joined 不宣称外层 caller/consumer 或其 DB 写已返回，避免 Result 等待自身返回的循环条件。D04 Body.Close 返回不单独证明其他 writer 完成。本库不读取 Client 私有 map、不根据 ctx.Done 或 error 推 join，也不关闭其他调用的共享 policy/transport。

Close/Start ctx 取消会取消本次 Do、请求 body/transport 关闭、唤醒背压，并在所给预算内等待；Result/Next 等待 ctx 取消也请求本次取消。Close 超时或 Do 晚返回保持 Joined=false 与原 slot/handle 所有权；迟到 response 必须仍被原 handle 接住并实际关闭。没有预算时不新开后台无限 Drain、独立 1s 或 WithoutCancel 续期；后续 Close/Budget.Drain/Joined 可对原 handle 复核终局，不能丢登记。全部 controls 退出前不销毁/释放借用材料；本库从不 Destroy 调用方的 SecretMaterial。

没有可用 channel 暴露 D04 writer 终局时，只能通过其正式 Drain 判定。纯状态复核可用已经取消的 ctx 调用 Drain：该正式方法先检查实际 active/writer/连接条件，只有确实为空才成功；超时值不是成功。实现不得因此忙轮询；有等待预算的 Drain 直接等待既有 D04 通知。Probe 只用于终局核查，不额外执行网络或增加等待预算。已实际终局才能释放 Budget slot；关闭失败保留不完成状态。普通 Result/StreamEnd 发布前完成本次正常 body/parser/Client 终局，且仍不表示 Model 业务 COMMIT。

## 6. 固定来源与实现路径

采用已采纳 D09 §6 的 OpenAI 官方 SDK `openai/openai-python@becc1d20eed83c1b8d85e15dc131a372d9dc7813`（3.24.0），来源主要为 `resources/chat/completions/completions.py`、`types/chat/completion_create_params.py`、`chat_completion.py`、`chat_completion_message.py`、`chat_completion_chunk.py`、`chat_completion_stream_options_param.py` 与 `types/completion_usage.py` 及其被采用的 message/detail 子类型。既有研究报告 SHA `41f04a202111e3ab99ea0c904112accc76dfc6844975b3c518640c1e090a515f` 只作来源定位，字段证据不等于新代码 conformance。

rev1 本地未定位原 checkout，原直接 HTTPS DNS 失败已保留；随后获有界取证授权，通过公开 Git 恢复上述完整 commit 的 17 个采用文件，独立核 SHA256、Git blob 与原 commit 身份通过。恢复门槛现已落实，固定 [来源/字段与重建证据](../agent-team/evidence/d09-openai-chat-wire-source/README.md) 含官方许可证、精确摘录、完整文件 SHA/URL 与原失败/成功传输记录；源码未安装或执行。实施 manifest 必须复用该来源，并绑定 profile/revision、所采/验证后忽略字段及实际正反 fixture；来源复核不等于 conformance 通过。若有新的来源冲突保留失败并先修规格；不能换 latest、借第三方兼容文档或随意补字段。无需引入 SDK/改 go.mod。Jina 官方协议、逐型号 effort/structured/schema/image 支持矩阵是后继工程补证；Summary 初值仍是产品待决，二者不得混淆。

独立静审采纳、root 正式续派后，建议以下 **16 路径**交一个 backend 作者；本阶段只修本卡及归档上述 SDK 来源证据。

| 路径 | 唯一允许内容 |
| --- | --- |
| 新 `internal/central/model/adapter/openai_chat.go` | §2 API、输入闭集/编码、普通响应与安全输出 |
| 新 `internal/central/model/adapter/transport.go` | 单 Exchange 的真实 D04 请求、Observation、Body/Client actual join |
| 新 `internal/central/model/adapter/sse.go` | 唯一有界 SSE 状态机与事件背压 |
| 新 `internal/central/model/adapter/errors.go` | 安全错误/原生数值与 usage 解码共用核 |
| 新 `internal/central/model/adapter/budget.go` | 64/8 无队列共享 admission、Stop/Drain/Force/slot 退休 |
| 新 `internal/central/model/adapter/{openai_chat,transport,sse,errors,budget}_test.go`（五个） | 纯闭集、克隆/安全格式、parser 极限、并发/终局反例 |
| 新 `internal/central/model/adapter/testdata/openai-chat-text-v1.json` | 精确 SDK 逐文件 SHA、字段/修订与测试 provenance，不装入生产 SDK |
| 新 `tests/model/openai_chat_wire_fixture_test.go` | 真实 Account/Audit/PG policy 与原 owned outbound server 的窄组合 |
| 新 `tests/model/openai_chat_wire_http_test.go` | 原生请求/普通响应/认证/状态码/发送计数/拒绝安全 |
| 新 `tests/model/openai_chat_wire_stream_test.go` | 实际 SSE 分片/断流/取消/背压/usage/真实资源终局 |
| 旧 `tests/testsupport/outbound/fixture.go` | 仅新 wire scenario 的控制 DTO/安全状态投影 |
| 旧 `tests/testsupport/outbound/cmd/server/main.go` | 仅对应 nonce 保护的合成 wire scenario |

两个旧 fixture 文件是唯一 shared 写接缝，必须由 root 与其他 fixture 使用者协调独占。ScenarioConfig 只新增 `Wire *WireScenario`；只有 Mode=`openai_chat_wire` 可带 Wire，其余旧 Mode/path/default 原义保持。新增 DTO 精确为：

```go
type WireScenario struct {
    Suffix          string
    Status          int
    Headers         map[string]string
    Chunks          [][]byte
    HoldAfter       *int
    DisconnectAfter *int
}
```

Suffix 是 `/case/<既有32hex>` 后的精确 raw path，最多 2048 bytes，必须以 `/` 开始及 `/chat/completions` 结束，不带 query/fragment；默认 `/chat/completions`，可用合法转义 base path 验证拼接。Status 闭集为 200/302/307/308/400/401/403/404/408/409/429/500/502/503/504。Headers 只允许 Content-Type、X-Request-Id、Retry-After、Location，值拒 CR/LF/非法头，总量≤16 KiB；Location 仅测试 POST 拒跳，不能使服务端主动访问网络。Chunks≤512 个、decoded 总量≤512 KiB，控制 JSON 仍服从旧 1 MiB 上限；每块 flush。HoldAfter/DisconnectAfter 若非 nil 为 0..len(Chunks)，表示发送指定块数后等待已有 Release/断开；header 已发送，hold 可被 client 取消唤醒。wire Mode 禁用旧 Body/Size/HeaderBytes/Redirect 的叠加语义，不增加任意响应脚本。

Request 状态只新增 `RequestURI string`；State 只新增 `ActiveHandlers, CompletedHandlers int`，按该 scenario 的 handler 实际进入/退出计数。连接标志与 client 真实 Drain 分别观察，server 标记不代替 client join。只存合成 canary/有限请求，不写运行日志。全部控制仍校验原 nonce/exact container/network；不增公网 listener、映射端口或替换镜像/CA。

原 driver 已运行 `internal/central/model/...` 与 `tests/model/...`，不需改 driver/脚本/预算。所有 D04 生产源、C0/Secret/identity/Model 主 Service/Resolver/Usage、旧测试、迁移、root/app 均不开放。Source/fixture 接缝不足须报最小证据，不弱化断言或私加执行旁路。

## 7. 独立验收与冻结

必须先纯编译/测试/race/vet、两 binary 编译；真实组沿原 Go1.27.1 离线缓存与 owned PG17/PG16/MinIO/outbound driver 的 race/count1/每包6m，由 root 单独交窗口。新增真实组不得因 fixture 未配置而把 skip 当通过；改旧 server 后补实际受影响的 D04 TLS/credential/redirect/stream/cancellation 旧组，保留所有原失败。

| 顶层真实组（固定名称） | 必须证明 |
| --- | --- |
| `TestModelOpenAIChatWireHTTP` | 精确 base-path/原生 body、Bearer origin绑定、普通文本/NULL与0 usage；合法非空 refusal 返回 content_filter/不可重试且无成功 Result/正文泄露，保留合法 usage；未支持请求在 server count=0；真实 admin policy allow/deny、TLS错误、POST redirect零第二请求、错误正文/材料 canary 不出安全错误或日志 |
| `TestModelOpenAIChatWireStream` | 真实私网分片 UTF-8/SSE、默认 obfuscation 字符串及缺失/null 成功且不入正文、错误类型/收紧 D04 ResponseBodyBytes 超限安全失败；finish后最终空 choices usage、必须 DONE、无usage成功；EOF/坏event/未知finish/工具反例，非空 refusal 错误一次后 EOF、无StreamEnd/正文泄露，保留已合法 usage 并核实际 PartialOutput；有限队列背压、取消后无迟到StreamEnd |
| `TestModelOpenAIChatWireJoinAndBudget` | hold headers/body、Do晚返回、慢消费/已取消 Close、独占 Client 实际 Drain；未join不归还 slot；64全局/8Project及无排队超额零Do、共享两adapter同Budget、StopAdmission/Drain/Force与其他Project未越界 |

最后一组可放 stream 测试文件。纯反例补 nil/typednil/非法Snapshot、所有 unsupported、深拷贝、精度>2^53/MaxInt64、重复键/尾随JSON、event与总量极限（含 obfuscation 错误类型/超 event 上限）、JSON/SSE refusal 与无 refusal 的 content_filter finish 区分、并发Next及安全format。真实发送前/后分别断网，必须核 Decision.Sent 原值及真实 server 计数；不能仅用预设错误代替 D04。测试直接提供的合成 SecretMaterial 与现有真实 Account/System policy 是**协议 fixture**，不声称 Project Model SecretResolve/Invocation Authority 已装配。

成功交付含固定16源指纹、原始 argv/env/exit/log、SDK小型manifest、首红与delta、受控server通过及未做真实Provider账号smoke说明。作者与独立V分离；源码冻结后独立复核。容器/网络 exact ID二次不存在、原基线不变、owned进程/runtime/命令清零后立即交回窗口，不因报告排版延迟。当前规格阶段只有上述有界官方源码恢复；无Go/Docker/SQL/SDK或Provider调用，也无业务实现通过声明。

## 规格采纳与实施交接

2026-10-05，主线程采纳冻结 rev2（SHA-256 `650cf36506951abc54bf44e7c84738e074238c28c65db3a770bfdc0a8157c508`）。独立差量报告 SHA-256 `eb8e04f77989be2540edc53e80c15d84b89d4bde86a54d5c4e4121482d772f96`；两处协议修订已闭合，API/DTO/16路径未扩。主线程核全部来源索引并实际离线重建17源/17摘录/许可证通过，未执行SDK或Provider。冻结卡副本位于来源证据目录，当前采纳页首不改变原审查输入。

唯一实施者为 `restore_test_dependencies`，仅开放本卡16个源/测试/fixture路径。固定生产输入为 `de00c610da62cb77cc03efe7c3cc842cf81f1ba5` 加本卡自身变更；不消费活动Artifact/Project修复。已有两fixture接缝实施前核Git差量和唯一所有权；不得扩D04/C0核心、SQL/迁移/依赖/driver/生产root或业务Invocation。先纯测试与编译、冻结后独立验收；真实资源须主线程另行交窗。源代码、实际命令/环境/退出码/原日志及安全错误、实际join证据必须保留，真实供应商账号测试未执行不作通过声明。
