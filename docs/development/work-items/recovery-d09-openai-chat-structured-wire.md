# D09 OpenAI Chat structured-output wire 恢复卡

修订：rev1，规格已独立静审通过并获主线程采纳，业务实施待本卡提交后另授。固定代码输入为 `ac5b4c65e88ed0ec1813fbf341035c88ca9a38c7`，仅消费其中已验 C0、D04 与 `openai-chat-text-v1`，不消费活动 root、Artifact 或 Object 源。当前 `d08_recovery_design` 仅有本卡状态修订权；后续由其转任 `backend_worker` 实施，独立验收者为 `skill_verification`，10 路径业务写权及真实 fixture 窗口仍由主线程另授。

必读 [AGENTS](../../../AGENTS.md)、[团队流程](../agent-team/README.md)、[设计技能](../../../.agents/skills/agenteam-design/SKILL.md)、[D09 主卡](d09-model-system-token-usage.md)、[D09 设计 §6–7](d09-model-system-token-usage-design.md#6-五种协议-profile固定源码事实与-conformance-边界)、[已验 text wire 卡](recovery-d09-openai-chat-wire.md)及其[验收证据](../agent-team/d09-openai-chat-wire-verification.md)。本卡只列增量，不重定 C0/出站权限/原 wire 生命周期。

## 1. 完整结果与前置

交付同一 `adapter.OpenAIChat` 内一个独立 `openai-chat-structured-v1` 修订：真实 strict `json_schema` 请求，经既有 D04 单次 POST，在普通响应及 SSE 中验证实际完整结果，保留安全错误、usage、背压与 actual join。无需迁移、Go 依赖、SDK 运行库、新环境变量或真实 Provider 账号。

固定 `model/references.go` 的 `selectionCompatible("memory", …)` 要求 Chat 且 `StructuredOutputModes` 包含 `json_schema`；已验 text-v1 在 `adapter/openai_chat.go` 明确拒绝该 capability/ResponseFormat。因此本块补齐后继 direct/platform.memory Resolver 所需协议能力；不宣称 Resolver、Memory canonical schema/consumer、Snapshot/Binding、Secret Acquire、Invocation/Usage 或生产 root 已交付。其他平台 selector、serving_snapshot、Summary 产品待决项及暂停的 Object 修复均不在本卡。

正式接口已够用：C0 `ResponseFormat{Kind, Name, Schema}` 已容纳 json_schema；`ConfigSnapshot.Identity.AdapterRevision` 已容纳独立修订。保留全部 C0 方法签名与 adapter 的 `NewOpenAIChat`、`Request`、`CallOptions`、`Result`、`Event`、`Exchange` 公共签名；唯一新增导出常量：

```go
const OpenAIChatStructuredRevision = "openai-chat-structured-v1"
```

仍返回 `Result.Text` / `TextDelta` 原文，不新增 `any` JSON 结果、自动补全或第二装配入口。后继 Resolver 按真实配置冻结修订；`ResolveRequest` 不携带 ResponseFormat，不能为每次格式选择改写已冻结 snapshot。

## 2. 修订、请求与不可变输入

| 输入 | 规则 |
| --- | --- |
| `openai-chat-text-v1` | 原 capability/format 闭集、原编码、finish/error 语义及全部旧测试保持；仍拒绝 json_schema capability/请求 |
| `openai-chat-structured-v1` | Profile=`OpenAIChatV1`、Protocol=`OpenAIChat`、type=Chat；StructuredOutputModes 必含 json_schema，可另含 text，顺序无关；其余原 text-only modality、无 tools/reasoning/overwrites、空参数等闭集保持 |
| 新修订的 ResponseFormat=text | 沿原普通文本路径，native body 不带 response_format；不编译 schema，不累计 SSE 全文，不以结构化规则重解释 length/content_filter |
| 新修订的 ResponseFormat=json_schema | Name 必须 1–64 字节 `[A-Za-z0-9_-]`；Schema 满足 §3；编码 `response_format={"type":"json_schema","json_schema":{"name":Name,"schema":Schema,"strict":true}}` |
| 未知修订/有效但未支持的能力 | 原 `unsupported_feature/wire_unsupported_feature`，不降级、不忽略字段、零 Exchange/slot/Do |

json_schema 的 strict 恒为 true，不增加调用方关闭开关；不发送 json_object、wrapper description、tools、reasoning 或原生任意字段。Schema 的允许成员和精确数值必须完整送入 native schema，不替调用者添加 required/additionalProperties、展开引用、删除约束或修改 schema 语义；JSON 空白/必要转义不承诺逐字节相同。原 messages/base path/Bearer origin binding/stream_options/include_usage/max_completion_tokens 保持。

Start 在 `Budget.accept`、造 Client、worker 或 Do 之前完成 C0 校验、schema 编译、native 请求总量检查。编译仅保留本次不可变私有程序，编码与校验必须来自同一份输入；Start 返回后调用方改 Schema slice/capabilities/messages 不得改变实际请求或验证规则。程序不暴露到公共类型、Format/JSON/日志，不借 Service 持有的能力创造 Project/consumer 授权。原 `prepare`/`stream` 的旧测试接缝可由私有 helper 保持，不能为返回值适配扩大旧测试写权。

## 3. 保守 schema 子集及资源预算

以下是本项目修订的闭集与实现上限，不是供应商全标准/逐型号支持矩阵。固定 SDK 只证明 §7 字段及 strict 转换的源码事实；真实账号支持仍未验证。通过 C0 的 schema 不等于通过本子集。

| 成员/形状 | 本修订完整语义 |
| --- | --- |
| 根 | 必须是 `type:"object"`，不允许 nullable 根、boolean schema 或根 union |
| type | object/array/string/number/integer/boolean/null；非根可用恰一个非 null 类型加 null 的两元素数组，顺序无关、无重复；不支持其他 union |
| object | 显式 properties 对象、required 字符串数组、additionalProperties=false；required 必须无重复且与 properties 的键集合完全相同；空对象可用 properties={}、required=[]；所有实际键恰属 properties，缺键和多键均失败 |
| array | 显式单一 items schema；所有实际元素按同一规则验证，不支持 tuple/prefixItems |
| enum | 仅 string 或 nullable string 节点可带非空 enum；元素为唯一字符串，可在 nullable 时包含一次 null；实际值须同时满足 type 与 enum；不支持其他类型 enum/const |
| title/description | 可选 UTF-8 字符串，分别最多 128/4096 字节；已知注释字段，完整发送，无输出约束含义 |
| 其他 | 一律拒绝，包括 $ref/$defs/definitions/$schema/$id、anyOf/oneOf/allOf/not/条件、default、pattern/format、min/maxLength、minimum/maximum/multipleOf、min/maxItems/uniqueItems、patternProperties 等；不解析本地或网络引用、不调用外部 validator |

编译必须逐层检查全部节点/允许成员和已知成员类型；不能只验证根或把未知 keyword 当注释。非对应类型上出现 properties/items 等也拒绝，不能静默忽略。C0 既有 UTF-8/重复键/完整 JSON、schema 64 KiB/JSON 深度 32 上限不变；额外固定 schema AST 至多 1024 节点（每个 schema 对象为一节点）、AST 深度至多 32（根为 1）、每 object 至多 256 properties、property 名至多 256 UTF-8 字节、每 enum 至多 128 项且每字符串至多 1024 UTF-8 字节。引用和组合分支数为零，无递归展开/回溯匹配。

输出保留原 16 MiB content、1 MiB SSE event、32 层 JSON 深度，并限制单次内容至多 65,536 个 JSON value 节点（object/array/scalar 均计，根为 1，不计 key）、number token 至多 128 字节。number 按合法 JSON 十进制词法处理；integer 必须是数学整数，含可等价为整数的 `1.0`/`1e2`，不能经 float64 或 int64 截断，`9007199254740993` 必须精确保留。指数只与有限小数位数作有界比较，不按指数展开大整数、不按指数值循环；不支持数值 enum/范围算术，数值比较成本受 token 上限约束。

解析/验证按有界输入及节点预算逐步推进，每节点和较长扫描检查原 ctx；不得另起不受 ctx 管理的 validator goroutine。JSONResponse 直接验证已解码 Text；仅 SSE json_schema 路径保留一个至多 16 MiB 的累计内容 buffer。验证直接遍历该内容，不再构造完整第二份累计字符串/通用 DOM；原单 event 与 256 KiB 队列仍各自有界，不能拿最终结果绕过队列限额。编译前已取消/编译期间取消不 accept；输出验证结束时仍须核原 ctx，取消后不发布成功。预算没有续期。

客户端 JSON/UTF-8/重复 key、Name、已知 schema 成员类型或不可满足的 required/property 结构错误为 `InvalidArgument/NotStarted`；可识别但在闭集外的 schema（含非严格 object、未支持 keyword/type union、上述额外编译规模上限）为 `unsupported_feature/wire_unsupported_feature`。均在 accept/Do 前，不能记录成 Provider 错误/已发送。C0 64 KiB/既有深度失败仍沿 C0 InvalidArgument；原编码请求 16 MiB 超限的 `wire_limit_exceeded` 先验行为不变，不因本卡改旧 text 分类。

## 4. 完成、失败与 usage

普通响应仍先执行原 native envelope/单 choice/usage/安全字段校验。非空 refusal 优先沿原 content_filter 错误；已合法观察的 usage 保留。仅 json_schema 路径的 finish=stop 且 Text 是单一完整 JSON object、无重复键/尾随值/非法 Unicode、满足全部已编译约束时返回成功 Result，Text 保留原精确正文（含空白/数字词法）。空/null/malformed/不符合 schema 不得返回成功 Result。

SSE 沿原状态机处理拆分 UTF-8/多 data/obfuscation/finish/最终 usage/[DONE]。TextDelta 是尚未验证的前缀，不能作为完整结构化结果或触发业务执行；不把每个 delta 当完整 JSON。finish 后仍可接一次合法空 choices usage；在合法 `[DONE]` 处，对同一次累计内容验证通过才具备成功资格，且沿原 actual join 后才交 StreamEnd。不能等 HTTP EOF 代替协议终止，也不能在 finish 时提前成功。错误一次后 EOF，无成功 StreamEnd；已有前缀保持事实，`PartialOutput` 只取实际向调用方交付的非空 TextDelta。

仅 json_schema 路径：finish=length 一律安全 `provider_error/wire_protocol_invalid`、不可重试，即使前缀恰能解析；finish=content_filter 或非空 refusal 为不可重试 content_filter。length/content_filter 的合法结束流可先观察 final usage，到 DONE 时返回相应失败；非空 refusal 沿原即时拒绝。缺 DONE/非法 envelope/工具/未知 finish 沿原错误，不能靠局部 schema 校验转成功。有效 JSON 违反 schema或 JSON 本身非法为 `provider_error/wire_protocol_invalid`；输出深度/节点/number token/content/event 超限为 `provider_error/wire_limit_exceeded`，均不可重试且无校验路径/失败值/正文进入错误。

原 status/网络错误分类、requestID 来源、NULL 与 0 usage、计数精度、无 usage 成功时 UnknownUsage、不累计 usage chunk，以及安全 Format/LogValue/MarshalJSON 完整保留。已经合法观察的 usage 在 schema 失败、后续断流或取消后仍可由 Observe 读取；本库无持久提交含义。不得加入 SDK 自动 parse/retry 或把 schema failure 变成第二次 Provider 请求。

## 5. 关闭与所有权

新旧 revision 共享同一个具体 Budget 的 64 全局/8 Project 配额，不增队列或第二 Budget。仍只使用一个 Exchange、一次 D04 POST、独占 Client、原 caller/overall/read-idle 控制；constructor 零 I/O/worker。不更改 Budget 或 D04 生产实现。

schema 编译在接受前，本次输出累计/验证在原 parser worker 内，因而属于原 Exchange owner；合法结果/StreamEnd 之前仍必须 Body.Close、worker 结束以及 Client.StopAdmission/Drain 实际成功。Close/Force/取消须唤醒原背压与 verifier；slot 只在 actual join 后退休，超时保留 handle/slot。沿原同一剩余预算，无 WithoutCancel/新超时/后台无限等待。材料继续由调用方拥有，所有真实工作终局之前不 Destroy。不宣称 Model 外层 DB writer、业务 consumer 或共享 ProcessGuard 已 join；暂停的 Object 缺陷与 ready503 边界不变。

## 6. 精确实施范围

本卡提交后另授唯一实施者 `d08_recovery_design`（`backend_worker`）以下 **10 路径**；当前仍仅写本卡状态。三个旧生产文件与固定 ac5 逐 SHA 比较，有其他已提交变更时先定向核差量，不能读活动候选作为前置。

| 路径 | 允许内容 |
| --- | --- |
| 旧 `internal/central/model/adapter/openai_chat.go` | 新 revision 分派、strict 格式编码、一次不可变编译与准入前校验；保留 text 分支 |
| 旧 `internal/central/model/adapter/transport.go` | Exchange 私有校验程序、普通结构化结果校验及原 owner 内完成；原关闭链保持 |
| 旧 `internal/central/model/adapter/sse.go` | 仅 json_schema 的有界累计、DONE 校验/失败；原 text 状态机与队列保持 |
| 新 `internal/central/model/adapter/structured_schema.go` | 私有有界 schema 编译、精确数值词法及输出验证；不新造公共 contract |
| 新 `internal/central/model/adapter/structured_schema_test.go` | 完整子集、未知关键字、精度/深度/节点/成本边界 |
| 新 `internal/central/model/adapter/openai_chat_structured_test.go` | revision/编码/先验拒绝、不可变输入、安全格式与来源 manifest |
| 新 `internal/central/model/adapter/structured_stream_test.go` | 结构化状态机、预算/取消、partial/usage/实际终局；复用原纯 helper |
| 新 `internal/central/model/adapter/testdata/openai-chat-structured-v1.json` | 修订、2 新来源原字节/身份与旧来源引用、字段映射、本地子集/上限和测试对应关系 |
| 新 `tests/model/openai_chat_structured_http_test.go` | 真实普通请求/响应/安全拒绝；可含两新文件共用的窄测试 helper |
| 新 `tests/model/openai_chat_structured_stream_test.go` | 真实 SSE、混合 revision 同 Budget/关闭与取消 |

复用现有 `tests/model/openai_chat_wire_fixture_test.go` 与 D04 `WireScenario` 的 Chunks/HoldAfter/DisconnectAfter/Release/请求观察，无需修改它们。server 每场景 512 KiB payload 上限保持；大于该上限的 parser 16 MiB 极限用新增纯测试实际覆盖，不提高 fixture 上限。`errors.go`、`budget.go`、所有旧测试/旧 manifest、C0、D04、Model Service/Resolver/Secret、app、SQL/迁移、依赖与 driver 都只读。若缺正式接缝，给出精确必要差量交主线程，不能绕开预算/权限或修改旧断言。

## 7. 固定来源与最小证据

沿[已归档 text 来源](../agent-team/evidence/d09-openai-chat-wire-source/README.md)的官方 `openai/openai-python@becc1d20eed83c1b8d85e15dc131a372d9dc7813`（3.24.0）。本轮只从其仍在本地的固定 Git 对象离线读取两份增量原件，未联网、导入或执行 SDK：

| 官方文件 | Git blob / SHA-256 | 采用事实 |
| --- | --- | --- |
| [response_format_json_schema.py](https://github.com/openai/openai-python/blob/becc1d20eed83c1b8d85e15dc131a372d9dc7813/src/openai/types/shared_params/response_format_json_schema.py) | `2b6bbdc3e6def1601d9f0c9b4b12be103cb1139e` / `4a81522587ed5fa7571f376854c87870f8b0a5e7f6e473eb9fc2e297bfbd856a` | 1815 bytes/54 行；原生 type/json_schema/name/schema/strict 与 name 字符闭集；strict 的 schema 支持子集，不证明所有模型 |
| [_pydantic.py](https://github.com/openai/openai-python/blob/becc1d20eed83c1b8d85e15dc131a372d9dc7813/src/openai/lib/_pydantic.py) | `3cfe224cb1e84ff721ecc814e3f6b134ba660b8f` / `09fd2f1b0b9674d12e21483c58d4448d644602930e6e097c0e374b5387f98c07` | 5623 bytes/155 行；strict 转换覆盖 object 的 required/additionalProperties、递归 properties/items；其扩展/补写/ref 行为不由本卡照搬 |

现有普通/SSE/usage/错误 envelope 来源复用旧 17 源 manifest，不重复存完整 SDK。新增 testdata manifest 仅容纳上述两小原件的可无损解码原字节（例如 base64）和 commit/blob/SHA、已采用字段/明确拒绝项、§3 工程上限、对应测试名；沿原[Apache-2.0 许可证](../agent-team/evidence/d09-openai-chat-wire-source/LICENSE.openai)保留来源。独立检查必须实际复原 bytes 并逐 SHA/Git blob 比对，不仅判断字符串长度。原对象丢失时先报告来源缺口，不静默换 latest 或联网取第三方替代。

本轮私有输入索引 `/tmp/agenteam-structured-wire-design-3w_dcwsr/inputs.json` SHA-256 `e2b32523758f7058c6fc8f26fd92b2ae783e84326e2cdb500397daede9f79d38` 固定 ac5 的 13 项源码/规格/fixture 与这 2 份新来源；临时目录不是持久验收证据。字段来源、本地模拟 conformance 和真实供应商账号能力必须分别记录。

## 8. 验收与交接门槛

实施先按固定 Go1.27.1、离线依赖、私有 workspace cache/TMPDIR 做全部 adapter 纯 unit/race/vet、integration compile 及两 cmd build；记录实际 argv/env/exit 与原始失败。旧 adapter 全部测试原样回归，C0/Model 相关编译通过；语义未变的外围包/旧 D04 证据可明确复用。真实组仍用原 owned PG17/PG16/MinIO/outbound driver、race/count1/每包 6m，root 另授独占窗口，无网络故障代理或暂停 Object 探针。

| 新顶层真实组 | 必须观察的完整行为 |
| --- | --- |
| `TestModelOpenAIChatStructuredHTTP` | 实际 POST/body 的 strict/name/schema、不增未知字段且 Bearer 正确；嵌套 object/array/nullable/string enum/大整数成功并保原文本/usage；同新 revision text 成功且不发 response_format；原 text-v1 拒 structured；非法/未支持/超编译规模请求全部零请求/slot；实际 valid envelope 内 missing/extra/type/enum/重复 key/尾随值、length、refusal/content_filter 均无成功 Result，正确 usage/安全错误，无正文/schema/material canary泄露 |
| `TestModelOpenAIChatStructuredStream` | 跨 chunk UTF-8/JSON/转义与数字、最终空 choices usage、无 usage、DONE 后服务器继续 hold 的合法结束；先消费前缀后真实 schema mismatch/尾随 JSON/length/content_filter/断流，PartialOutput 与已交付前缀一致、可靠 usage保留、错误一次后 EOF 且零 StreamEnd；合法 JSON但缺 DONE不成功；坏 schema 不因 native envelope 合法而通过 |
| `TestModelOpenAIChatStructuredCloseAndBudget` | 原受控 hold/背压时取消，真实 Client/body/parser 终局与 server handler/connection 双观察；同具体 Budget 新旧 revision 混合，8 Project/64 全局仍共用、超额零 Do，StopAdmission/Drain/Force 不续期；未 join 不释放 slot，最终校验/取消不发迟到成功，不 Destroy 借用材料 |

真实 fixture 仍用真实 Account/Audit/PG policy 与合成 SecretMaterial/snapshot，不能称 Secret Model Resolve、Memory 权限或 Invocation 装配通过。共享 D04 生产/fixture 无改动；补跑原三个 `TestModelOpenAIChatWireHTTP/Stream/JoinAndBudget` 以确认 text 兼容及原真实关闭。新增纯反例覆盖全部 keyword/type 分支、exact 下/上界、16 MiB/1 MiB/32层/1024 schema node/65,536 output node/128数值字节、指数不展开、schema 输入克隆、拒绝前后 budget事实、验证中取消和安全格式；真实单场景无法承载的极限不得写成已跑真实。

作者冻结精确 10 源、原始日志/命令/来源 manifest/首红与修复差量；独立 V 按风险选择真实反例，至少覆盖可解析但不匹配 schema 的失败终态/usage 与取消实际 join，不以 typed constructor 早拒冒充响应验证。最后资源 exactID 二次不存在、原基线不变、owned 进程/runtime 清零，立即交窗口；报告可随后整理。只有独立验收与主线程采纳才是此库修订交付，不代表完整 D09/Resolver/生产调用可用。

当前仅完成规格独立静审、采纳与离线来源核查；无业务/迁移/Go/Docker/Provider 执行，也没有新的运行通过声明。

## 规格采纳与实施交接

2026-10-05，主线程读核独立静审报告后采纳 rev1。被审卡 SHA-256 为 `103ef5b0d3e7451510d55d4bbf99986db59639ac309ab47ba9b9a8f2189f6eff`；独立报告为 `/tmp/agenteam-structured-wire-spec-v-zf97bw0x/report.md`，SHA-256 `d840b87853e1236423495c6751c921d508ccb36cbd25b16af20ca7e65e76c6f3`。结论为规格 STATIC PASS，不是实现或动态验收通过；本次仅更新状态/职责，固定 ac5、API、10 路径、技术规则及验收正文保持。

业务实施待本卡提交后由主线程正式续派，实施者 `d08_recovery_design` 转任 `backend_worker`，开工须补读 [Go 开发技能](../../../.agents/skills/agenteam-go-development/SKILL.md)；独立验收由 `skill_verification` 负责。当前不启动业务、测试或真实资源，不再委派。
