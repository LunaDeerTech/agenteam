# D22 后继 Model 语义与单调用响应纯合同

状态：2026-10-12 设计卡，尚未授权产品实现或运行验证。专用树 `/workspace/agenteam-model-successor-contract` / `ai/model-successor-contract-20261012` 基于已通过 NRMR 的 `448f6adea22d62dcb71255a3c834dbb8f1b95d84`；NRMR 已以完整历史合入并推送 main `be71c42c4c68ae54ce76d3dca6b34974ad208203`。本片只准备下一调用所需的纯数据合同，不称下一 Model 已接通。

依据：[D01 Model 固定输入](d01-contracts/model-tool.md#model-解析与调用)、[完整响应与重试所有权](d01-contracts/model-tool.md#model-stream重试与计量)、[原 schema 校验](d01-contracts/model-tool.md#toolspecbinding-与可信调用)、[D22 输入确定点](d01-contracts/execution-orchestration.md#技能变更与下一轮输入确定点)、[Parallel Tool Calls](../../architecture/platform-infrastructure/model-system/chat-model-runtime.md#parallel-tool-calls)、[NRMR](d22-next-round-model-request.md)及[真实 HTTP STOP](recovery-d09-openai-chat-tools-wire.md)。继承卡中的旧进度文字不覆盖上述已交付事实。

## 有限交付与不变项

将已交 NRMR 的 schema3 **语义层**下沉到 Model 可依赖的纯合同；Execution 保留 schema1 外包装、来源身份和持久提交证明。Model 不反向 import Execution、Loop 或 Tool。原 schema1/2 算法、所有已存 NRMR canonical bytes、`input_digest` 与完整 `request_digest` 均不变，不迁移或重写旧行。

主线程明确采用版本化 `auto + text + AgentRetry`、单调用响应 profile。AgentRetry 沿现有 AgentConsumer 必需契约；单调用范围沿当前 captured ToolSet 恰一项、`parallel_tool_calls=false` 和[首轮 count==1](../../../internal/central/agentloop/tool_call.go)的实现基础。D01 广义有序 ToolBatch 保留为后继：本片不支持完整多调用，也不取第一项继续。原 `required` 首轮 session 不改，NRMR 原 ToolSet 与请求字节不删减。

新增 Begin/Result/Close、CredentialRead、Invocation/Usage 写入、Runner、默认装配与服务调用全部为零；纯候选不是 committed、Invoke 或派发能力。[现 Model runtime](../../../internal/central/model/runtime_call.go) 的 text/schema1/无 Tools 拒绝门及[configuration-only capture](../../../internal/central/model/execution_tool_facts_capture.go)原样保留，不接 nil/no-op adapter。原 wire 16 MiB 限额不因 NRMR stored 上限 25,182,208 B 自动提高；本片不实现或宣称 HTTP 可发送所有合法存储请求。56 单 Round、68 Transcript seq1–4、72 请求表及真实 tools HTTP STOP 均不改；无迁移、新 ledger 或下一轮调度。

## 共享接口与唯一实现

拟接口在实现前一次冻结，均为同步纯函数，无 Store、resolver、Model Caller 或 credential port：

- Model contract：`NewSuccessorSemantic(ctx, SuccessorSemanticParts)` / `DecodeSuccessorSemantic(ctx, raw)` 返回私有拥有的 `SuccessorSemantic`；显式 `CanonicalBytes/Digest/Parts` 返回独立副本，默认格式化/JSON/日志脱敏。Parts 含可信 Actor、完整 ResolvedModel、Messages、Tools 及明确的 auto/text/AgentRetry；schema 固定为 3。有效值只证明完整语义，不证明来源或调用授权。
- Execution：现 `NewNextRoundModelRequest/DecodeNextRoundModelRequest` 保持公开形状，由上述唯一算法产生/读取原 semantic JSON，再按原外包装核 P/A/E、68 RoundInput、Snapshot、Projection、CallID 与 InputIdentity。不保留第二套 schema3 生产编码或摘要算法；旧 preparation/schema1/2 编码不随此抽取改写。
- Loop：`ValidateSuccessorResponse(ctx, request mc.ModelRequest, response mc.ModelResponse)` 返回私有拥有的分类值；仅完整成功分类可显式取完整响应副本，其余分类没有可执行 ToolCall 候选。先校原 schema3 semantic digest、请求身份/profile 与 ModelResponse C0 形状，再按下节分类。CallID 必须匹配；InvocationID 仅校合法形状并保留，实际 Invoke 所有者/attempt 的真实性由后续调用实现核验，本纯函数不能证明。

完整 semantic 字段顺序/表示和 canonical 算法以[已交 codec](../../../internal/central/execution/contract/next_round_request_codec.go)为准：显式 AgentRun P/A/E、Consumer、Model ConfigSnapshot 全字段、CredentialRef Details、LeaseOwner Details/LeaseID、顺序 messages/完整 Tool schema、choice/response/retry。`ModelRequest.Actor` 默认 JSON 被忽略，Secret 引用拒绝默认 Unmarshal，因此必须可回读完整 DTO；不得用默认 ModelRequest JSON 或日志形式算摘要。语义摘要不含 logical CallID 或自身摘要；外包装继续绑定稳定 CallID 与含语义摘要的 InputIdentity。计数包含 JSON escaping，原大小门、数值词法、克隆和借用清理不放宽。

## 单调用 auto 响应闭集

| 原 finish reason / 形态 | 本片结果 |
| --- | --- |
| `stop`，只有合法 Text parts、零 ToolCall | 完整 text；空文本按现 C0 规则，不新增空回答含义 |
| `tool_calls`，恰一完整 ToolCall，可伴合法 Text parts | 完整单调用，保原 parts 顺序与全部参数 |
| `tool_calls` 多于一项，即使各项完整合法 | `unsupported`，整响应无成功候选，不截取首项 |
| `length` / `content_filter` | 中断/受限，无完整 text/tool 候选，不改成 stop；已有片段不派发 |
| `cancelled` / `error` / `unknown` | 非成功分类；未知值拒绝，不伪 stop、不拼接其他 attempt |

完整单调用必须匹配请求唯一 Tool 的名称，call ID 合法；参数沿 [C0](../../../internal/central/model/contract/chat.go)严格完整 JSON object、1 MiB、深度/重复键/尾随值/Unicode 门，再用[既有纯 Tool schema](../../../internal/central/tool/schema/schema.go)校验请求携带的原 InputSchema。该 compiler 只解析内存资源，不加载外部引用；不删 required、不重序列化数值、不靠 model-visible name 猜 RunnerBinding。schema 不支持或参数不符均拒绝成功候选。具体 Tool 语义、当前授权及 Operation 仍归后续真实 Tool owner，本片无权限替代。

成功形态内拒绝 Image/File/ToolResult 等本 profile 未支持的 part；finish 与内容不符、半参数、缺身份或错 CallID 同样拒绝。保留既有全局 FinishReason 枚举及 ModelFrame 门：例如现 message_end 允许 length/content_filter，不代表后继可完整执行。输入 Response/Usage 不修改，不制造 usage=0，也不在本片结算 Usage；仅 `tool_call_end` 或 frame 片段不能传成完整响应权威。

## 文件与分工

| 唯一写域 | 拟文件与责任 |
| --- | --- |
| Model 纯合同作者 | `internal/central/model/contract/successor_input.go` 与专控：schema3 完整 DTO、唯一 canonical/digest、回读/副本/脱敏；不引入高层 import |
| Execution 兼容作者 | `internal/central/execution/contract/next_round_request_codec.go` 与兼容专控：薄委托、原包装和两层摘要逐字保持；不动 binder/72/持久记录 |
| Loop 响应作者 | `internal/central/agentloop/successor_response.go` 与专控：版本化 auto 闭集，复用原纯 schema；不修改旧 controller/session/retry |

共享 Model 类型由第一域唯一 writer；两域消费该同版接口，不各自复制 semantic wire。实现授权后先冻结最小接口，再并行源码。后继真实执行仍须独立 durable request→私有 Invoke/CredentialRead/Finalize、Committed before unique Begin、实际 Result/Close/Joined 与响应 checkpoint 设计；本卡不补这些缺口。

## 必要验收（未运行）

1. **独立旧字节基线：** 重构前以已交 `448f6ade` 原算法和固定安全 fixture 确定并保存 Read/Write/Command canonical golden、原 input/request 两 digest 与来源版本；改后逐字相同，新的 helper 不参与 expected 生成，也不常驻第二生产算法。已有实际 canonical 可作额外原件比对，不输出私有正文；只做新 codec 自身 round-trip 不算兼容证明。
2. **完整身份与纯 codec：** Actor/model/consumer/lease、消息/Tools/schema/profile 任一语义变化进入摘要；CallID 只改变原外层摘要。错误身份、非 canonical/重复/未知字段、逃逸后越界拒绝；深副本、默认脱敏、取消实际返回保全。Model 无高层反 import，原 schema1/2 与预算不变。
3. **完整响应正反控：** text-stop、单 call 加 text 均通过；多 call 整体 unsupported；错 CallID/name、畸形/半 JSON、schema 不符与 finish/parts 不符不能产生成功候选。用原 schema 严格校验，不只测自造布尔判断。
4. **中断与兼容：** length/content_filter/cancelled/error/unknown 保真实非成功分类，原响应及可靠 Usage 不被改写；不把 frame/tool_call_end 当 terminal。旧 required/count==1 与新 auto 的必要兼容控并存，不扩旧运行矩阵。
5. **有限集成：** 必要纯 Go 控与相关包 vet 证明上述数据边界；不需 PG/native 或真实 Model/credential/Runner 调用来冒充本片完成。默认/runtime/capture-only、HTTP STOP、16 MiB wire、56/68/72 与存储零差额；实际运行另获资源窗口授权。
