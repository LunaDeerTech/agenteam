# D22 已提交后继输入的持久 Model 请求绑定

状态：2026-10-12，受测源码 `448f6adea22d62dcb71255a3c834dbb8f1b95d84` 的首次修后 `native02` 已实际 whole PASS，1 top / 2 sub、真实 SQL 与持久请求绑定均到达，原退出门全部完成；主审已完成最终有限 wholePASS 独验并接受。基础验证为分期 **9 top / 45 sub、4 包 vet**：原 8 top / 45 sub + 3 vet 已通过，随后仅补真实 EmbeddedSource 1 top + postgres 1 vet，没有整套重跑。`native01` 在迁移源前置加载返回 `MIGRATION_INVALID` 的原 whole FAIL 及全部输入、尾门记录完整保留。最终文档提交与受测源码不同，以原输入清单为准。

本片最终文档 `82f26436` 与两轮受测源码已随完整历史 main `be71c42c` 合入并推送；完整运行原件已无覆盖保全、历史版本main可恢复，原专用树 `/workspace/agenteam-next-round-model-request` 与本地/远端 `ai/next-round-model-request-20261012` 已按普通/精确lease流程实际清理。依赖Command源码 `1ab01c81` 的独立main `f1de1974` 交付保持。本片成功范围只到完整下一请求的持久绑定与历史观察，不代表下一 Model 已调用、默认生产链已绑定或 D01–D28/E01 整体完成。

依据：[Model 调用与固定输入](d01-contracts/model-tool.md#model-解析与调用)、[重试所有权](d01-contracts/model-tool.md#model-stream重试与计量)、[下一轮输入确定点](d01-contracts/execution-orchestration.md#技能变更与下一轮输入确定点)、[RoundInput](d22-execution-round-input.md)、[历史 Projection](d22-next-round-projection.md)、[Write 消费](d22-write-file-result-consumption.md)与[Command 消费](d22-run-command-result-consumption.md)。实施按[团队流程](../agent-team/README.md)及对应 Go、数据库、测试技能，不重复旧完整矩阵。

## 有限交付与真实阻断

目标是从已提交 `(ExecutionID, RoundInputID)` 建立唯一、完整、可重读的下一请求及稳定 logical CallID。请求是持久数据，不是调用授权或第二个已执行 Round。请求绑定阶段新增 Model Begin/Result/Close、CredentialRead、Provider、Runner 调用均为零；不产生 Invocation/Usage、新 Tool Operation、Transcript 或 Execution 终态，不接默认生产装配。

已有 Projection 提供原 Snapshot、完整 ToolCall/ToolResult、保护参数、Tool schema、固定 Skill 目录和可选 Control；本片沿真实历史读取，不用当前 Registry/Agent/Tool/Mount 配置替换封存事实。以下门保持：

- [56](../../../db/migrations/00056_execution_direct_text.sql) 的 `rounds.execution_id UNIQUE` 只容纳首轮；[68](../../../db/migrations/00068_execution_round_inputs.sql) 保留此门，Transcript 仅 seq1–3 与可选 Control seq4。新请求不得塞入旧 rounds 或新增任意 assistant/seq5。
- [实际 Model runtime](../../../internal/central/model/runtime_call.go) 拒绝 tool role/ToolResult、非空 Tools、ToolCalls 及不匹配 revision；[ToolCallFacts 捕获](d22-execution-tool-call-facts.md) 的 revision 是 configuration-only，未注册 tools HTTP adapter。不能修改历史 revision、重解析模型或删 ToolSet 进入 text profile。
- [首轮 ToolCall session](../../../internal/central/agentloop/tool_call.go) 固定 `required` 且只收 `tool_calls`，没有正式 successor 响应 profile。真实 tools HTTP A/B STOP、生产 nil/unbound、Object runtime join 等原 STOP 保持；不新增 nil/no-op Runtime 或更换实现绕过。

本卡由主线程明确选择完整原 ToolSet、`auto + text + AgentRetry` 及独立版本化编码。auto/text 是本次新后继请求设计，AgentRetry 则沿现有 AgentConsumer 必需契约；这不表示当前 Runtime 已支持该 profile。所有选择值进入 canonical bytes/digest，不能靠默认值重建。后继实际响应类型与处理范围另定。Model 仍是唯一 Provider retry owner，不能改 bounded consumer 或加 Loop 自动重放。

## 实际接口与完整编码

已落接口：`RoundModelRequestBinder.Prepare(ctx,E,RoundInputID)`、`Lookup(ctx,E,RoundInputID)`，以及 `Stop/Drain/Joined`。入口只收原身份，不接受 caller 提供的消息、ToolSet、Snapshot、lease、CallID 或 committed 布尔值。`NewRoundModelRequestBinder(store, authority, projects)` 绑定真实同 Store 的 Execution authority 与 Project gate，内部直接调用实际 Loop 纯构造器，不注入 Model Caller/Runner。返回私有拥有的 `ec.NextRoundModelRequest`：安全身份/摘要可读，显式正文访问给独立副本，不提供 Invoke 能力。

这里的 RoundInputID 是 68 `round_inputs.id`，对应 `RoundInput.Fields().RoundID`；新 InputIdentity.RoundID 绑定这个后继 ID，不表示 56 rounds 已有该 ID 的第二行。

Execution 在同 Tx 真实历史 loan 内调用 Loop 的纯 `BuildNextRoundModelRequest`。固定内容包括完整顺序 messages、Tool schema/名称/描述、原 call/result 配对、Control、有效 Skill 目录、原可信 AgentRun actor P/A/E、Consumer 完整身份、Model ConfigSnapshot 全字段及 CredentialRef.Details/LeaseOwner.Details/LeaseID。显式模型编码优先在 Execution contract 内复用 [preparation_input.go](../../../internal/central/execution/contract/preparation_input.go) 的 `modelWire/refWire`，不在两域复制；默认 ModelRequest 忽略 Actor，CredentialRef/LeaseOwner 虽可 Marshal Details 却拒绝默认 Unmarshal，不能直接代替完整可回读 DTO。Write data 和 Command 双流不截短；新请求值的默认格式化/JSON/日志脱敏。不保存明文凭据、context 或活 witness，未来凭据读取仍遵循原稳定引用与轮换契约。

显式 canonical DTO 分两层避免摘要循环：`input_digest` 绑定版本、完整语义请求及固定模型选择，不含自身摘要；`request_digest` 再绑定该 DTO、P/A/E、RoundInput ID/digest、Snapshot ID/digest、Projection digest、稳定 CallID 和含前者的 InputIdentity。完整 Tools、choice、response format、retry class 都参与编码，不能拿 Projection digest、首轮 text/schema2 digest 或日志编码代替。解码拒绝未知/重复字段、非 canonical 字节及身份/摘要不符；完整包装 schema 1、Model Input schema 3；长度先计数（含 JSON escaping），上限为原 16 MiB Projection + 8 MiB Preparation + 16 KiB 身份开销，共 25,182,208 B，不放宽原结果预算。

新增前向迁移 [72](../../../db/migrations/00072_execution_round_model_requests.sql) 的单一请求表：唯一 `(execution_id, round_input_id)`、唯一 logical CallID，保存上述完整关联、canonical bytes/长度/摘要/版本/时间；请求行不可改删。FK、精确 shape/digest/长度约束与同 Tx 原历史重验共同证明来源，合法 JSON 本身不授信。旧 Round/Snapshot/结果消费/Transcript/pending 与原 Execution version 不因本片写请求而改写。

## 同 Tx、幂等与退出

- 入 Tx 前收齐并一次规范取得原 Project/Agent/Execution、适用 ProjectSchedule 与新增记录所需完整锁集；同 Store/live Tx 下先核已存请求。已存在返回原身份与字节；首次不存在才检查 Running、无 canonical cancel、Project 可写，重验原已提交 RoundInput/历史后保存候选。
- [公开 Projection reader](../../../internal/central/execution/round_projection.go) 自开 Tx，不能嵌在本片写 Tx 中。复用既有内部同 Tx 接缝，不改公开 reader；使用 `loadRoundInput`、`loadCommittedRoundProjection` 与同步 loan 清理；不 nested Tx、不外域 IO、不接 caller fabricated source。回调成功不是事务成功。
- 同 key 并发由原 gate/唯一约束收敛。CallID 在原尝试中固定，Store **实际返回 Committed** 后才发布成功；NotCommitted 不发布。Unknown 保原 key/CallID/完整候选，仅 Lookup 精确观察，不重插入、换 key、新建 CallID或自动重试；未观察到不能推出未提交，新实例 Lookup 也不恢复执行权限。
- Prepare/Lookup 均登记原调用；Stop 拒绝新调用并取消已有，Drain 受自身 context 约束并等原 Store/loan/回调实际返回。未确认 Unknown 保留原身份观察责任，不用取消信号或清空内存假称 Joined。

未来真正调用须另有实际 process/调用所有者的私有能力，绑定同 Store/full locks、原 CallID/Input/Snapshot/lease 和实际 Invocation。Invoke/CredentialRead 检查当前新调用权限；Finalize 只结算原调用并保取消后的清退。请求 Committed 是 Begin 的必要条件而非充分授权；单次 Begin、原 Result、实际 Close/Joined 与 Unknown 只观察原 handle 均属后续调用实现，**本卡不声称已实现或验证**。

## 文件分域与集成

| 唯一所有者 | 文件域 | 责任 |
| --- | --- | --- |
| Execution/存储 | `execution/{round_model_request,round_model_request_owner,round_model_request_repository}.go`、`execution/contract/round_model_request.go`、专控与迁移 72；既有历史 reader 未改 | 历史/完整锁、唯一持久事实、原提交/Unknown/退出；目录均在 `internal/central/` 下 |
| Loop/纯 codec | `internal/central/agentloop/next_round_request.go` 及专控、`internal/central/execution/contract/next_round_request_codec.go` | 纯构造、完整版本化编码、克隆/脱敏；不改首轮 session |
| 独立 PG/方法 | `tests/projectvariable/next_round_model_request_test.go` 与专属必要方法 | 真实结果消费→Determine→请求绑定/独立 SQL/Lookup，不伪造 authority |
| Model/Tool 边界审查 | 本片只读已有合同与拒绝门 | 不新增 Tool reader/派发口，不修改 adapter/default registry |

类型与 canonical 编码一次冻结，不由两域复制算法；迁移和共享接缝各单 writer。Command 依赖已验收并合入主线；其有限交付范围不扩展为下一 Model 执行。Execution 专控 4 top / 20 sub、Loop/codec 专控 4 top / 25 sub 已在 basic01 通过，随后 basic02 只补真实 EmbeddedSource 1 top 与 postgres vet；原测试与包范围没有扩大为旧完整矩阵。

## 已验证范围与实际证据

1. **纯控的完整编码：** Read/Write/Command 闭集的原配对、合法既有大正文、完整 schema/选择项进入摘要；身份/正文/配置/profile 篡改拒绝，副本与默认表示不改写或泄漏原值。
2. **真实 PG 的唯一历史：** 最小真实 PG 独立连接核 `(E,RoundInputID)` 唯一及完整 CallID/bytes/digests，重复和新实例 Lookup 相同；当前 Tool/Mount 配置变化不改变已存请求。
3. **真实 PG 的原提交：** callback 末真实回滚无部分请求；原 COMMIT 返回 barrier 前不发布，实际提交后受控丢回执只原/新 Lookup，零重写/新 CallID。回执丢失不冒数据库物理提交未知。
4. **受控 Store 的当前门与退出：** 首次写在错 Store/Tx、缺锁、非 Running、canonical cancel、Project 不可写时拒绝；历史观察不授新调用。原返回 barrier 证明 Stop/Drain 等实际返回与 loan 清除。
5. **兼容与零调用：** 56/68/旧 Transcript/结果预算不变；新增 Begin/CredentialRead/Invocation/Usage/Runner 均为零。fixture 原有受控 Model/真实 Runner 只算前置链，不记为下一 Model 或 tools HTTP 通过。

实际 PG 选择器为 `TestNextRoundModelRequest`，两个 sub 为 `committed-command-input-request-and-history`、`request-rollback-and-commit-loss-lookup`。每个 sub 先经真实 Command 结果消费及 68 Determine 建立后继输入：原 setup **Model 1 / wire 1**，随后请求绑定新增 **Model 0 / Runner 0**，不能把整链写成零调用。Model JSON Caller 与 Project 持久 test-Skills 回执边界保留，不冒真实 tools HTTP 或生产 Project/Skills 装配。其他 Read/Write 结果 profile 的完整编码由纯控验证，没有新增其 PG 矩阵。

原记录按阶段保留：

- [sequence01](../../../output/ai/retired-20261012/next-round-model-request/sequence01/terminal.json) 在 `5f4b9a4ec00d49bbbccb3641a89c10d8be20e673` 实际 exit 0 / 371.988s：方法 1 正 / 7 反、basic01 原 8 top / 45 sub 和 3 vet、compile01/list/prepare01 均通过。basic01 原 708 inputs 前后相同；[精确选择](../../../output/ai/retired-20261012/next-round-model-request/basic01/unit01-selection.json)保留。
- [native01](../../../output/ai/retired-20261012/next-round-model-request/native01/terminal.json) supervisor 1034030 实际 1 / 86.773s；两 sub 在 inherited fixture 中返回 `MIGRATION_INVALID`，尚未到请求业务。七资源与全部原尾门通过，2017 runtime inputs / 四身份相同。原错误自身未定位加载或 apply；随后静态确认 72 的 `-- +goose Down` 被真实 `EmbeddedSource` 规则拒绝，Up 未运行。最小修复删除 Down 标记、随后的 DO 块和末尾分隔空行，不修改 Up 业务 SQL；新增 [真实 embed 加载控制](../../../internal/central/postgres/embedded_source_test.go)。该控制只证源规范，不冒 SQL apply；原 FAIL 不回写。
- [sequence02](../../../output/ai/retired-20261012/next-round-model-request/sequence02/terminal.json) 原工具 91753 / `d61b8c` 实际 0 / 41.582s：方法再验 1 正 / 7 反，basic02 **只**跑新 source 1 top（Go 1040973，0 / 4.428s）及 postgres vet（1041064，0 / 1.201s），102 inputs 相同；旧 8 / 45 + 3 vet 复用。[compile02](../../../output/ai/retired-20261012/next-round-model-request/compile02/result.json) Go 1041131 实际 0 / 30.297s、exact list 1041485 实际 0 / 1.051s，2017 compile inputs 前后/list 全同；加原实物 binary 后为 2018 runtime inputs。binary 90,933,206 B，SHA256 `96153091d73fc5a61d3d18b9e21e83687650c0f73eb4a5a74a593a288e0d18c2`。
- [native02 原工具观测](../../../output/ai/retired-20261012/next-round-model-request/native02/tool-observation.json)为 47617 / `9134b7` actual 0；supervisor 1042254 Wait 0 / 133.161s，raw terminal 0 / 132.031s，driver 1042275 与 Go 1043867 实际 Wait 0。1 top / 2 sub 在 38.46s 通过（20.55s、17.91s），真实迁移加载/apply与两条业务链均到达。原同 7 个资源共 14 absence、descendant/private/runtime 双尾、自动 HOST_TCP delta 75s 内双空全部齐，2018 inputs / 四身份相同。[post-check](../../../output/ai/retired-20261012/next-round-model-request/native02/post-check.json)及原 `/tmp/nrmr02/pg-ccd0cbccd81c4c0ea56bd38343af684a.log` 保留；未软化原 6m、540+60 或 TCP 门。

真实验收证明同 Store / 同 Tx 完整历史 loan、唯一稳定 CallID 和全部 canonical bytes / 两层 digest；独立 SQL 核历史不变及幂等重读；完整 callback 后真实 rollback 零请求行；实际物理 COMMIT 后遮蔽回执只用原与新实例 Lookup 恢复，未自动重 Prepare/换 CallID。它不授未来 Invoke/CredentialRead 权限，不改 56/68、默认装配、原 tools HTTP STOP，也未验证下一 Model 执行、通用等待所有者或终态退休。具体方法与阶段命令见[本片 README](../../../.agent-state/next-round-model-request/README.md)。

原件保全含159文件/190,552,654B/21目录、44图/2020仓库历史版本、两binary与两轮tmp共8副本；原tmp、native01 FAIL与所有退出门原记录保持。实际保全/清理回执、外部依赖和跨设备限制见[恢复说明](../../../.agent-state/next-round-model-request/README.md#main-delivery-and-retirement)。
