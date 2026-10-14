# D09 M1 JSON 工具 Runtime

本片实现 M1 新 Execution 的同步工具调用接缝，消费现有 Model Runtime、Secret、Usage 和 Execution consumer authority。原 tools wire 独立动态 STOP 保留；本片的编码、静审和纯单元检查不构成其解除，也不宣称完整 D09 或 live provider 已通过。

## 契约与组合

- 入口保持 `mc.JSONCaller.BeginChat(ctx, mc.ModelRequest)`，结果为原 `mc.JSONCall.Result/Close/Joined`；不另建 HTTP、事件、数据库表或迁移。
- 新 `model.NewToolCallingExecutionCapture(models, owner)` 仅为新 preparation 冻结 `openai-chat-tools-v1`。旧默认 capture、`execution-tool-call-facts-capture-v1` 及旧 Snapshot 保持原意义，禁止换 revision 重用历史 lease。
- 仅 JSON、text 输入输出、`ToolCalls=true`、`ParallelToolCalls=false`，无 reasoning/structured overwrite。`Capabilities.Streaming` 可为 true（模型具备能力），调用仍须显式 JSON；SSE 请求明确拒绝，不能静默改传输。完整 ToolSet 随 Snapshot 固定，wire 支持 native function tool 与 none/auto/required/named；本轮 Execution 每响应最多一个 ToolCall。
- 首轮 schema 2 保留旧单工具 required/text 与 canonical 摘要，单一实现下移 `mc.ToolCallInputDigest`；新完整 ToolSet 首轮使用 schema 4 `mc.ToolInputDigest(ctx,messages,tools,choice)` / `mc.ValidateToolInputRequest`。后继 schema 3 复用 `mc.ValidateSuccessorRequest`，保持原 canonical 字节并允许 2..256 条完整配对历史及 1..128 项冻结工具，沿 C0 正式上限拒绝超限，不裁剪 Core/Skill 工具。
- assistant ToolCall/对应 tool ToolResult 保留原 ID、参数、文本；历史调用必须完整配对。结果参数必须是完整合法 JSON object 且工具名在本次声明内；参数 schema、当前授权及实际执行仍归 Tool/Execution。
- `Result.ToolCalls` 经原实际 I/O join 后返回，Runtime 投影为 `ModelResponse.Message.Parts`，保留 `tool_calls` finish 与可靠 Usage。无效响应、取消、未知提交不得发布成功。继续原 ConsumerAuthority、Secret 精确读取、Usage ownership 和 retry/close 协议。

## 责任与验证

W1/model_runtime 唯一写 Model 域；W2/core_readiness 连接首轮与后继请求、持久结果和当前 Execution authority；runner_tools 保持 Runner 真实派发/结果；delivery_path 负责 App 组合。任何新迁移归 W2 登记。

基础纯单元检查覆盖请求/历史编码、工具响应及可靠 usage、参数/选择/历史配对拒绝、原修订兼容、schema 2/3 摘要、深复制与早取消。沿 Go 1.27.1 使用现有缓存，无外部凭据、网络 fixture 或旧动态 driver。后续真实 Model→Runner→结果→Model 组合及独立验证由集成/验证 owner 明确范围；纯单元通过不代替该链路。

当前状态：首个实现片段与基础纯单元已完成。Go 1.27.1 的 adapter/contract/model 限定选择器已通过，覆盖原 text/structured/wire、Runtime、新 JSON tools 和 successor；随后完整128工具/256历史、旧schema2固定hash、1MiB参数/+1边界定向通过。首轮曾发现 C0 解码会替换孤立 UTF-16 surrogate，adapter 已增加严格 JSON Unicode 门并复验通过；保留原失败事实。尚未跑新真实 provider/Runner 联调或独立动态检查，不宣称完整 tools wire/SSE 通过。


独立macro已对f47d3eb0完成限定静审及7顶层/20子项纯race（actual exit0），三包作者vet实际exit0。其窄隔离建议已补：默认Capture直接拒tools revision，不能靠剥离ToolCalls能力bit跨profile；作者新增单项通过，等待该差量独立复验。以上都不代替Model/Secret/Usage真实持久组合、Runner端到端或原独立动态STOP的解除。
