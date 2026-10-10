# D09 有限 text-only Model Runtime

状态：有限 SPEC 与首 Object/Project 五路径经 cleanup 非作者有限审接受；首边界15top普通/race均实际通过。Runtime九源、00031、Secret router显式分派及两基础test已落盘，首有限8top普通/race与model包vet实际wholePASS；Runtime实际源独审和真实调用尚未完成。基线 main `04455194`，工作树 `/workspace/agenteam-model-text-runtime`、分支 `ai/model-text-runtime`。root 分配本执行者独占迁移 `00031_model_logical_calls.sql`。本卡交付一次真实 attempt 的 logical-call 服务，生产 consumer 与默认根未绑定；不宣称完整 D09、D22 或 Agent F1。

## 1. 正式依据、提供方与调用方

遵循 [D09 工程规格 §4–5/9](d09-model-system-token-usage-design.md#4-正式规划授权与-secret-组合)、[D01 Model 调用](d01-contracts/model-tool.md#model-解析与调用)、[Chat Runtime](../../architecture/platform-infrastructure/model-system/chat-model-runtime.md)、[已验 text wire](recovery-d09-openai-chat-wire.md)、[当前解析](recovery-d09-current-model-resolution.md)、[Invocation ledger](recovery-d09-invocation-usage-ledger.md)及 [Secret Model usage](recovery-d09-secret-model-usage.md)。原完整规格继续约束；本卡仅明确已授权的受限能力。

| 提供方 | 本批实际消费 | 不由 Runtime 代替的事实 |
| --- | --- | --- |
| Model 管理/Resolver | 00017 immutable snapshot 与 exact snapshot binding；原 typed `ResolvedModel` | 不重新选择 live Model，不借 caller JSON 证明 snapshot 已提交 |
| 未来 D22/Meeting/Memory/Tool consumer | 原 `mc.ConsumerAuthority.Discover/ValidateInTx` | 原输入、来源、当前可调用/取消与 retry policy；当前没有生产实现 |
| Runtime（本批） | Call/attempt canonical 事实、发送资格、进程内句柄、Usage/Secret 的精确 provider | 不自签 Agent、Meeting、Operation 或 Project 当前权限 |
| Usage | 原 `uc.Writer` reserve/observe/finalize/lookup；00018 ledger/summary | 不由 Runtime 直接写其私有表，不再造 token 汇总算法 |
| Secret | 原 `CredentialUsageReader`、`UsageOperations` 与真实 callback | 当前 value、AEAD、SecretResolve Audit、lease mutation |
| OpenAI text adapter / D04 | 原 `OpenAIChat.Start`、`Exchange`、Policy/Trust/Do/Decision | 不新增 HTTP client、TLS/地址旁路或 Provider SDK |
| Object ProcessGuard | 同一真实 guard 的稳定 ProcessID与持有生命周期 | 不用 PID/TTL/缺行推断死亡，不新造死亡探测 |

首条隔离链为：**明确标注的 consumer 契约 fixture → 本 Runtime 真实 PG Call/attempt → Secret planned read / Usage 同 Tx → 原 adapter 与 D04 owned native HTTP/TLS → Exchange 实退 → 持久 terminal 与 lease 退休**。fixture 不证明 Agent/Meeting 等业务授权；其它被测边界使用实际库和持久事实，不拿 fake Chat/Usage/Secret 宣布组合成功。

D22 后继通过原 `mc.Chat` 消费，固定 input/Snapshot 并处理其业务 terminal；D19/D24 的人工 waiting 不在本服务。Model 不 import Tool/Executor/Meeting 实现。OpenAI tools 独立验收、Object runtime join 和 SPA 等停止项均不由本卡恢复。

## 2. 首版能力与精确接口

公开调用沿原 `model/contract`，不新增标准 DTO、HTTP 或推理代理：

```go
Chat(context.Context, mc.ModelRequest) (mc.ModelResponse, error)
Stream(context.Context, mc.ModelRequest) (mc.ModelStream, error)
// ModelStream 原接口：Next(ctx), Close(ctx), Joined()
```

新增本域构造形状为 `NewRuntimeAuthority(store, RuntimeAuthorizations)` 与 `NewRuntime(store, authority, RuntimeDependencies)`。前者先构造，后者再注入 Secret/Usage/adapter，避免互相构造。具体 dependencies 只装以下真实能力，构造不做 SQL/network；typed nil、缺 ConsumerAuthority、错 RuntimeAuthority Store 均拒绝。

- Authority：同一 `model.Store`、必需的 `mc.ConsumerAuthority`、真实 ProcessGuard、根明确注册的 ModelRuntime/SecretService/OutboundService 身份。不能传 caller 任意 ProcessID/fence 或注册名字符串代替实际 registration。
- Runtime：已绑定到该 Authority 的 Usage Writer、Secret planned reader+operations、原 text adapter+其共享 Budget。Secret/Usage 的 InTx 回调必须接收原 Store 的活 Tx；错 Store/Tx、缺锁/弱锁失败，不用 wrapper 另开事务。
- 生命周期仅实现本切片需要的 `Initialize(ctx)`、`StopAdmission()`、`Drain(ctx)`、`Force(ctx)`、`Joined()`。Initialize 检查 schema/依赖及本 guard，不创建默认配置或主动请求供应商。不提供后台接管/维护扫描或默认根启动；Force 只是原预算内取消/关闭/收敛，不能伪造 joined。

支持条件须在任何 reservation 与 adapter Start 前成立：

1. 已接受 `OpenAIChatV1` / `OpenAIChatTextRevision`、text 输入输出；原参数/overwrite/header/profile约束保持。
2. `Tools` 长度 0、`ToolChoice.Kind=none`、`ResponseFormat.Kind=text`，消息只含原支持的 text part/role；拒绝工具、reasoning、媒体、structured output 或其它 adapter revision，错误为安全 `CapabilityUnsupported`。
3. 原请求为 `BoundedRetry`，且**当前 ConsumerAuthority plan** 的 RetryPolicy 为 BoundedRetry、MaxAttempts=1、Categories空、有效有限 Deadline。客户端 RetryClass 不是策略凭据。AgentRetry 或其它策略明确 unsupported、零 reservation/dispatch；不将 Agent 请求改标 Bounded，不削弱正式渐进重试规则。
4. 原 snapshot 必须含合法 CredentialRef 与 exact model_call LeaseID，owner ID=CallID。已验 wire 必需材料，无匿名 fallback；零 credential 或 execution-owned lease 在本版 unsupported、零 reservation。execution lease 的真正释放仍属于 D22，不能让单 call 释放它。

首版每 Call 只分配 ordinal=1 的 Invocation；失败、取消、未知或重复入口都不分配第二 attempt。无 automatic retry、换模型或同参合并。单 attempt 原上限取已验 wire 的初始 30s，并服从更早的 caller/Consumer Deadline；不能因 Close、确认或回调重置发送期限。不是 Execution 总期限，也不涉及人工等待。

## 3. 输入绑定与 snapshot

先做有界结构/能力检查，再 clone 接受的数据。消息/parts 总量沿原 DTO 与 wire 上限，累计 text/编码请求不得超过原 wire 16 MiB；先累计长度拒绝超界，不能先把 256 个各 16 MiB part 复制成数 GiB。保持 UTF-8 原文，不 trim/normalize。

本版 `Input.SchemaVersion=1` 明确为 text Model input 投影。Runtime 重算 SHA-256 canonical JSON：固定字段顺序 `format:1,messages,tool_choice,response_format`；messages 保原次序、role、parts 次序与 text 原字节，采用标准 `encoding/json.Marshal` 的字符串编码；Tools固定为空不带额外内容。投影不含 Actor、CallID、InvocationID、usage、超时或传输模式。提供同模块 `TextInputDigest(messages []mc.Message) (foundation.Digest,error)` 供正式 caller 固定输入，复用同一私有编码实现；它只算输入，不授予调用权。

请求 `Input.Digest` 必须等于实际 messages 重算结果；随后 ConsumerAuthority 必须从其 canonical 输入证明该 InputIdentity/digest/schema/CallID 归属与当前 gate。正确摘要仍不是授权。此版本不接受未声明的 digest dialect；后续 consumer 采用该格式时须在自己的输入边界持久固定。

Call 的 canonical binding 另覆盖完整 consumer、original initiator 身份、InputIdentity、snapshot ID及持久 snapshot digest、exact snapshot binding/LeaseID、固定 retry policy 与 json/sse模式。不同消息、来源、lease、policy或模式不能借同 CallID 进入；Credential value 不进入摘要或持久状态。snapshot 参数/endpoint/identity从00017当前原绑定读取并与请求对应，不能把 request.Snapshot 自报值送往 wire；live配置更新不热换旧合法 snapshot。

在重复请求检查之前仍调用当前 ConsumerAuthority；缺授权不能探测其他 caller 的 Call 状态。原 initiator完整投影留在 Call，技术收敛使用真实 ModelRuntime actor，不伪装过期 Human Session或 AgentRun。

## 4. 00031 与单一事实所有者

新增 Up-only `00031_model_logical_calls.sql`，不修改00017/18/其它前缀。迁移只给 Model Runtime 增加本域事实；没有 Agent/Execution/Meeting/Tool表、跨域级联 FK、正文或明文材料。

| 新表 | 必需事实/约束 |
| --- | --- |
| `agenteam_model.calls` | CallID主键；ProjectID、consumer/input/initiator的显式安全关联投影；snapshot/binding/lease引用；request binding、固定RetryPolicy；ProcessID/fence；phase=`accepted/running/succeeded/failed/cancelled/unknown`；当前ordinal/InvocationID；accepted/finished时间；retired标记/版本。唯一(project,id)，snapshot同Project本域FK。phase/时间/terminal组合 CHECK；bounded元数据JSON，不存messages/result text |
| `agenteam_model.runtime_attempts` | InvocationID主键、CallID同Project FK、ordinal=1并UNIQUE(call_id,ordinal)；ProcessID/fence；dispatch=`reserved/authorized/sent/not_sent/unknown`；开始/真实观察发送/结束时间；canonical observation sequence；安全ModelError/nullable Usage/terminal状态与版本；绑定digest。这里是Runtime事实，00018是经原Writer产生的ledger/summary，不反向把Usage请求DTO当事实 |

同一 call 的 claim、authorization、observation、terminal 都持同一 call writer gate；Invocation gate、Consumer完整锁、Usage plan、Secret plan所需锁先收集、一次按现有 D03 序取得。用已有 CommandLock/CommandRecordLock 类，不发明锁框架；必须采用 Usage InvocationPlan 的正式 Cause/相同原 Tx，任何映射变化回滚重规划，不在 Tx 内 Discover/补锁。

本地 exact CallID/InvocationID/guard实例句柄在 claim Tx **之前**登记；同锁确保只能转交一次 wire 或永不转交。只有Runtime私有控制流可创建/推进 observation，外部既不能提交Value也不能通过context布尔量声称 sent/joined。`uc.InvocationFacts` 使用私有 issuer+完整request/mapping锁计划，并在原 Tx 读取已写的runtime canonical事实、验证该阶段的私有apply见证；原 Writer据此写00018。任一步失败两个事实侧同 Tx 回滚。

不持久正文意味着本卡没有成功正文 replay。相同 CallID：语义冲突=`IdempotencyKeyReused`；相同语义且仍运行=`ResourceBusy`；先前未知且未能确认=`CommitUnknown`；已terminal且无可重放正文=`InvalidState`。不会返回空答案当成功，不重调Provider复原答案。本版不增加新公开Lookup DTO；仅内部按原锁确认自己的已接受事实。

## 5. 一次 attempt 的顺序与 Unknown

| 阶段 | 必须已确认的事实 | 失败/Unknown 的规则 |
| --- | --- | --- |
| 入场/claim | 能力、真实consumer输入/gate、00017绑定；本地句柄已登记 | 拒绝零reservation/dispatch；call尚未接受时不接管原Resolve owner的lease退休责任 |
| reservation | calls+runtime_attempt reserved 与 Usage reserve 同Tx | Committed或原writer锁下确认才继续；Unknown未确认不得读材料/Start |
| 材料 | exact Invocation planned Secret read；当前 credential_read gate、ref/lease/Process/fence | Secret read/Audit未明确Committed不返回材料；零发送，不以旧reservation代替本次读提交 |
| 发送资格 | 当前consumer再次验证；runtime+Usage authorized 同Tx、期限仍有效 | Unknown未确认不得 Start；同一local句柄一次性handoff，缺row/registry不能证明未发送 |
| wire | Tx外原 adapter.Start/Exchange；原D04Policy与正式Outbound cause | Start或DoStarted不等于sent；只有Decision.Sent=true才记录sent。false也须实际I/O结束且不可再写才可not_sent；无法证明为unknown |
| observation | 原wire可靠usage/Decision，Runtime与Usage observe同Tx且确认 | 未持久确认不发布相应usage_update；失败停止继续发布，取消wire并保存原句柄/identity，绝不开第二Start |
| terminal | wire/parser/body/D04writer actual join；可靠usage/原终态；Runtime与Usage finalize同Tx | commit Unknown不发布message_end。原writer锁确认一致事实，否则安全失败/关闭、保留Unknown，不改写原失败 |
| lease退休 | 该model_call全部材料/读/wire/DB回调实退、terminal已确认；Consumer retire核原已接受事实 | Secret planned release与calls retired同Tx；Unknown沿原ID确认，不能删本地所有权后让guard释放 |

`dispatched_at` 是首次观察到真实 Sent 的本地确认时间，不能称精确首字节时间。部分文本是瞬时输出，不能从“看到文字”跳过可靠usage和terminal提交；收到可靠usage后断流仍保留原usage，不推算total/未知tokens。

确认只使用原 CallID/InvocationID、原Cause/transaction attempt及完整锁，从Store取得已提交canonical事实；新确认本身失败/超时不推翻原Unknown。相同进程的持有句柄在原caller退出后仍由Runtime登记，后续显式Close/Drain可继续只做确认、终态与退休；不后台重新发请求。确认若需独立清理预算必须来自显式Close/Drain ctx，不用新的30s请求期限或无人持有的background waiter。

`CommitUnknown` 表示该次提交尚未确认，不自动把数据库 call phase 写成 `unknown`；持久 `unknown` 必须是按原事实和锁明确提交的业务终态。任何已确认 terminal 均不重开，迟到取消不能覆盖已提交成功；公开失败/Unknown 的原结果也不因后续确认而回填成功。

重启接管/自动恢复不在本版。Initialize发现未知旧进程未退休call不能当空态；报告未具备恢复能力并拒绝新调用，不能以TTL、数据库无行、连接断开或CurrentProcess读取结果接管旧实例。完整恢复须另按原ProcessGuard exact death与原DB writer终局设计实现。

## 6. Secret、Usage、Outbound 的真实构造闭环

现有 `SecretUsageRouter` 对 Model Read/Release 明确 unbound；只注入 reader 不构成闭环。本批拟最窄扩其本域组合（旧构造/旧分派行为保留）：

1. 先构造现 Model Authority 和新 RuntimeAuthority（后者不依赖 Secret/Usage service）。新 `NewRuntimeSecretUsageRouter(modelAuthority,runtimeAuthority,fallback)` 显式启用本域runtime provider，旧 `NewSecretUsageRouter` 继续原语义。
2. 原配置retain/release-reference及Resolve Acquire仍委派原Model Authority；仅 Purpose=Model 的 ReadLeaseUsage/ReleaseLeaseUsage 显式委派 RuntimeAuthority。Account/MCP等原fallback不改变；未知组合拒绝。无Purpose的 `AuthorizeLeaseInTx` 只认由原已验证planned callback建立的exact apply/read见证，不凭execution owner字符串猜Model。
3. Runtime planner按Read `RequestID=InvocationID` 定位唯一accepted、未发送、未退休attempt，核snapshot/ref/owner/lease、runtime私有current handle/Process/fence和 Consumer credential_read完整plan。Secret在自己的短Tx统一拿锁后重验；`SecretService` actor的scope=credential scope、CauseRef=lease owner ID，精确Invocation单独在RequestID，grant RequestID仍留空。
4. SecretMaterial仅实际Committed读返回后保存于本call私有句柄，安装唯一Destroy责任；传给adapter借用至实际Exchange.Joined，不能在ctx取消/Start返回/Close超时即清理仍借用材料。Start失败且未返回exchange时实际同步调用退出后Destroy。默认fmt/JSON/slog（含嵌套）不得出现材料、正文、请求digest或任意错误cause文本。
5. model_call lease由本已接受call退休；release先Discover完整consumer retire/Secret计划，锁后查同call terminal+exact实际join/Destroy见证，ApplyUsageInTx与retired标记同Tx。未被本call接受的Resolve lease仍归原创建方，不按“无call行”推断可释放。v1不释放execution lease、不复活已释放ID。
6. Usage Authority 的 Invocations绑定新RuntimeAuthority；它实施原`InvocationFacts`两方法。reserve/authorized使用原initiator，observe/finalize/confirm使用注册ModelRuntime、Project scope、CauseRef=InvocationID，完全沿现Writer的Actor规则。外部无可调用“记录已发送/已成功”接口。
7. Outbound用真实OutboundService(Project scope、CauseRef=InvocationID)、Access producer的原AppendKey/associations；Runtime提供精确已接受attempt/current consumer的事实验证，供实际Project/Audit注册组合消费。若正式注册口不足，缺口须报出，不能改成Owner/System或放宽D04地址策略。在本卡组合测试中也要验证真实Audit拒绝路径，不能静默跳过deny Audit。

Root最终同Store组合与生产Consumer注册另行负责，本卡不改 app；该分离不省略本次隔离真实服务组合的上述callbacks。全部大材料只在受控内存，00031/Usage/Audit不落正文或Credential value。

### 6.1 Project Access Audit 的实际缺口与窄口候选

现 `project/audit_facts.go` 只允许 Secret/Variables/Knowledge/Object producer；真实 Audit 在 Project scope 的 AccessDeny 必调 `Projects.CheckAppendInTx`，当前返回 DependencyUnbound，旧 System scope 管理员 wire fixture 不能代表本链。候选仍复用现 `audit.ProjectFactAuthority.CheckProjectAuditInTx(context.Context, foundation.Tx, audit.Entry, audit.AppendKey) error`，由唯一 RuntimeAuthority 提供本域 invocation 事实，不新造 Audit 接口。

- Project 的 AuditFacts 可选映射新增 `AccessProducer`，缺 provider 仍 unbound。最窄分支仅接受 `Action=AccessDeny/Outcome=Denied/PolicyResource/ordinal=0`，原注册 `OutboundService`、Project scope及actor cause=key cause；其它actor/action不因本注册放行。
- Project 在原 Audit Tx 检查既有 Project Shared 锁、同Store活Tx、真实已初始化/current lifecycle gate；不创建Call、不补Consumer锁、不重建用户或Service授权。然后原样委派精确 Entry/Key。业务权限仍须原 Runtime invoke/credential_read plan在dispatch前验证。
- Runtime checker只认该Project下原 InvocationID cause、accepted未退休call和发送资格已确认的实际attempt、同Process/fence、当前私有已handoff且未join owner、原consumer精确associations及本text Model consumer元数据；当前dispatch可以为 `authorized` 或 `sent`。原D04在已发后仍可产生RedirectDenied/ResponseLimit等拒绝审计，不能把当前dispatch必须等于authorized作为门槛；不接受caller自报“sent”。检查不升级调用资格，也不引入另一Access事实producer。
- 非法entry/actor/cause/关联返回原安全Forbidden；缺provider是DependencyUnbound；错误Store/锁及SQL沿原安全DependencyUnavailable；Project状态沿既有gate fault；所有错误保原cause、零成功 receipt。不能将 Audit 失败改成网络允许。
- 额外写域仅 `internal/central/project/audit_facts.go`、新 `model_access_audit.go` 与对应必要 `model_access_audit_test.go`（均在 Project 包）。cleanup接口审后root已授权这三路径，首源已获非作者有限代码审接受。本域 checker放原计划 `model/runtime_authority.go`，不扩app/defaultroot或Project管理。

## 7. Stream 与实际关闭

Stream只在reservation、材料与发送资格确认并实际取得原Exchange后返回。使用原ModelFrame，CallID/InvocationID/AttemptIndex=1贯穿；本invocation Sequence单调递增。仅产生`attempt_started/message_start/text_delta/usage_update/message_end/call_failed/call_cancelled`，没有tool/reasoning/attempt retry边界。

Text累计按原wire上限，有界保存以形成原ModelResponse；UTF-8 offset按已发布字节数，无Provider块拼接猜测。usage_update必须其真实observation已经确认。成功协议结束后，只有原Exchange actual join、terminal/Usage确认且本call材料使用与lease收敛完成，才能发布唯一message_end。无usage仍合法unknown usage；length保持length，不改成stop或编造Tool调用。

每个已返回stream最多一个公开terminal；terminal之后Next为EOF。普通Provider错误经原安全ModelError形成call_failed；当前取消为call_cancelled。无法确认持久terminal时，可以返回安全error并关闭公开流，但不声称DB terminal、Joined或已退休；不能先message_end再反悔。Next取消/Close不在后台重开Next来吞结果。

同一stream不允许并发Next；Close与Next/Stop竞态由同一call owner协调。`Close(ctx)`请求取消，等待原Next/Exchange/材料/DB回调实际退出，再沿原identity完成能证明的terminal与lease退休；预算耗尽返回错误且Joined=false，句柄仍在Runtime registry。`Joined()`只做无等待的实际状态观察，不因caller已返回、cancel ack或deadline而变true。

`StopAdmission`同准入锁封新call并取消所有已准入句柄；`Drain(ctx)`逐实际owner等待和收敛，ctx耗尽不抢关共享guard。`Force(ctx)`用上级传入同一额外ctx，仍保真实等待结果；不创建新的1s/30s。Runtime未Joined时，根不得释放共享Object ProcessGuard或先停止它还需调用的Secret/Usage/DB。构造/Initialize失败也必须由安装它的owner按同规则退休。

## 8. ProcessGuard 窄口候选（需 root 单独授权）

现`*object.ProcessGuard`无当前identity读取口。传入一对任意ProcessID+guard不能由Model库检验配对；`ConfirmStopped`只能证明别的exact旧实例死亡，ResourceBusy不能被反解成“这个ID属于当前guard”。

候选新增：`CurrentProcess() (oc.ProcessID,error)`。只在 `g!=nil && g.data!=nil`，取既有 `processState.mu`，检查`bound && !closed && service!=nil`，返回原`process`值；其余返回零ID+安全unbound/unavailable。无I/O、无新flock、无Close、无死亡结论、不返回宿主路径/nonce/文件句柄。原bind/finish/Close已用同mutex，此读取不得改其线性化和close职责。Runtime Initialize/准入从该对象读取，不接受callerProcessID。

该读取不是pin或死亡证明；共享guard保持到Model等所有借用者真实Joined的根关闭顺序仍是必要条件。它不修复或解禁Object STOP。cleanup契约审后root已授权 `object/process.go` 与新 `object/process_current_test.go` 窄增量，首源已获非作者有限代码审接受；pure控制只检查内存状态/同mutex，不冒真实flock/数据库注册或退休证据。

## 9. 写域与首次验证

唯一作者目录与精确路径：

- 本卡和本树 `.agent-state/current.md`。
- 新 `internal/central/model/{runtime.go,runtime_authority.go,runtime_store.go,runtime_call.go,runtime_attempt.go,runtime_stream.go,runtime_usage.go,runtime_secret.go,runtime_recovery.go}`，以及对应必要 `*_test.go`。`runtime_recovery.go`仅原ID确认/拒绝未支持接管，不引入后台恢复平台。
- 本域旧 `internal/central/model/secret_router.go` 与 `secret_router_test.go` 仅上述显式新runtime构造/分派；原构造和所有原分支必须兼容。
- `db/migrations/00031_model_logical_calls.sql`；原连续00001–30完整保留。
- 新 `tests/model/runtime_persistence_test.go`、`tests/model/runtime_native_test.go`，复用原正式fixture；新增精确harness入口由root协调唯一writer，不整文件覆盖旧输入。
- §8 Object窄口与§6.1 Project精确三路径已单独授权；adapter/contract/原Model管理与Resolver/app默认根不在允许写域。若另缺observer或正式验证端口，先报具体协约，不私加callback绕口。

先实现正常和明确失败基础测试：profile/策略拒绝零reservation、实际输入摘要/clone与安全日志、单call重复与一次handoff、frame唯一terminal、Close/Next生命周期。基础片段稳定就保存，不等全部恢复矩阵才联调。

首个真实隔离测试只贯通一次带凭据的text JSON成功（canonical Call/attempt、原Secret/Audit、真实Usage及实际wire、terminal/lease尾）与一次明确拒绝零发送；随后按暴露风险补SSE partial/usage/terminal、取消held I/O/Close超期、关键提交Unknown零重复Start。Consumer自有canonical契约fixture必须明示范围；不以SQL种“Agent ready”或默认allow宣业务正链。

首次Go前固定Go1.27.1、same-process fresh≥5GiB、私有telemetry off/去旁路、自有cache、共享只读mods/offline；先必要包unit/race/vet与fixture编译。实际PG/native须root新授窗口，原Wait/资源/私有目录/desc/TCP完整尾，失败不自动重试、不扩未变旧矩阵。

首接缝 `boundary-01` 固定入口 `.agent-state/model-text-runtime/boundary-checks.py`：Object 2个CurrentProcess pure＋Project 3个新Access与10个既有AuditFacts/ObjectAudit/KnowledgeAudit，共15top，ordinary与race各实际通过。两个Go/outer实际Wait0、原组absent、无adopted子进程、runtime/组尾各双空；每阶段启动同进程fresh≥5GiB。仅内存/既有SQL doubles范围，未跑真实flock/PG/Secret/Exchange，也不包含本次在写的Runtime包。原始结果保留于任务ignored output；后续Model Go仍须新的空间预飞，不据此宣 Runtime 可用。

Runtime `core-01` 固定入口 `.agent-state/model-text-runtime/core-checks.py`：7个新pure top与原Secret router兼容top共8top，ordinary/race及model包vet各实际0，三Go/outer Wait0、无adopted、runtime/原组双空，三次同进程fresh≥5GiB。只核文本输入手算摘要/上限/clone、策略拒绝零reservation、held原Consumer预检的Stop/Drain、终帧与安全默认nested输出/错误cause；没有fake正链。真实00031迁移、同Tx Usage/Secret、D04/Exchange与Unknown收敛仍需后续限定隔离联调，生产Consumer/defaultroot未绑定。

cleanup后续实际core窄审发现两项并发错误：活跃重复分支提前cancel原admission ctx，及start返回后先交gate再覆盖首错。现窄修保重复原当前授权/Tx实退，并在原gate内仅首次保存start错误；两个实际diff均获非作者有限接受，无剩余确认must-fix。新增回归只测原ctx活性/实际callback join及gate转交时首错可见/晚错不覆盖；不重复旧8top或boundary15。`regression-01`原预飞4808167424B不足5GiB而exit1，零Go启动，原FAIL保存；容量改变后root授权新`regression-02`，仍因启动fresh5057089536B不足门而exit1，零Go；两个原FAIL分别保留。静审与旧证据均不代替此修后运行，也不证明真实PG/Secret/wire。

在实际资源尾释放后的新授权`regression-03`中，两修定向top ordinary/race及model vet均实际0（session59305/outer567378，Go567381/567521/567681全Wait0）；每阶段启动fresh≥5GiB、无adopted、原组与runtime各双空。只补两修的ctx/首错转交风险，旧8top/boundary15不重复，首真实00031/Secret/Usage/D04链仍未运行。
