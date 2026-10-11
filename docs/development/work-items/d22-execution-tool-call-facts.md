# D22 首轮 ToolCall 持久事实

状态：基于 `226ea38b` 实施；基础 race 与 vet 已分段通过，首次真实 PG 已运行但在 Model 捕获入口失败，显式补口尚待验证。本片止于真实原模型句柄完成并实际 Joined 后的 ToolCall 持久事实和 `tool-pending` 非终态 checkpoint，不声明完整多轮 Agent、ToolOperation、Runner 派发或生产 tools 接入。

正式来源：[Model/Tool 契约](d01-contracts/model-tool.md)、[Execution 编排契约](d01-contracts/execution-orchestration.md)、[Transcript 与 Context](../../architecture/agent-loop/transcript-context.md)。

## 最小调用链与边界

1. 沿正式 Launch/Preparation 和捕获的 Tool/Model/Agent/Mount 身份，原同事务建立 Snapshot、首轮输入、running 与 Started；模型 I/O 在事务外。
2. 首轮输入采用独立版本 2，完整绑定两条消息、一个 `runner:read-file` 的模型 Tool schema、required choice 与 text response format。旧 DirectText 版本 1 和默认 no-tools 门不变。
3. 只有原 `JSONCaller` 的完整成功结果、原 Call/Invocation 关联，以及实际 Close/Joined，才能进入短事务保存 Assistant、ToolCall 和保护参数。普通 DTO、Actor 或 digest 不能伪造运行权威。
4. 保存完整原参数字节、规范化摘要及不可变保护引用的身份与作用域；日志和普通 JSON 不暴露参数。Execution、逻辑调用、Invocation 与 call ID 关联不可替换。checkpoint 保持 running，不伪称 succeeded、人工 waiting 或 Tool 已完成。
5. 持久化前用同 Store、真实 Tx 和预收集完整锁集合核当前 Project/Agent/Execution、原 NameTable/Spec/Binding、参数与 Capability/Policy。Tool 表只由 Tool 端口读取；本片 metadata 校验端口不是 Runner 派发权限。
6. Unknown 保留原 owner 和候选，仅观察原提交；无确证不重发模型调用。取消胜出后拒绝新迟到事实；信号不等于实际 join。持有引用不会因本片暂停在 tool-pending 而假退休。

## 写域与验证

lead 独占 Execution/Loop、本卡、分支 current 与迁移 `00064_execution_tool_call_facts.sql`；Tool 子作者独占 Tool 契约/Registry 与 `00063_tool_runner_bindings.sql`。root 负责 Git 与资源排期，runtime_recovery 独立审核，不参与实现。

首条 PG 链只替换 Model 端口，真实 Agent/Execution/Tool Registry/持久化及当前权威不得用原 D15 上游替身替代。必要基础控制覆盖完整响应与原句柄退休、事实幂等、未知提交、当前权限与事务回滚；不扩大成多轮 Tool Runtime 或重新验收旧领域矩阵。

OpenAI tools 独立 A/B STOP、生产 nil/unbound、旧默认 no-tools 均保留；归档 input08 不当作动态通过。不得通过新 driver 恢复停项，也不引入网络故障 probe、新依赖或默认绑定。当前已运行限定 Go 基础检查与离线 Python 方法控制；已编译 PG 测试但没有运行服务或 native。

## 当前实现候选

显式 `NewToolCallDriver` 与 `NewToolCallController` 复用原私有运行者及真实 JSONCall 的 Result/Close/Joined，不放宽旧构造器。迁移 00064 在原 snapshots/rounds/transcript 上前向增加版本 2 和 `tool_pending`，保护参数与调用关系只追加不可改；模型完成后短事务写入完整原参数、规范化摘要、Assistant 和 ToolCall，并保持 running 与引用 live。

驱动的 `ReadToolCall`、`DiscoverToolCall`、`RequireToolCallInTx` 消费已落库事实，使用同 Store/Tx 的完整锁与短期私有 Agent current witness，再核 Tool 当前引用及原捕获版本/attempt/selection。它实现调用事实权威接口，但没有 read-file ToolOperation/Attempt 的生产 writer 或默认 Runtime 消费者。

Tool 子域已有 6 个新基础测试入口，Execution/Loop 增加 8 个入口，覆盖版本分离、原句柄 join、响应关联/闭集参数、私有同事务 witness 及历史/当前事实分离；已包含在下述 34 个测试入口的分段 race 通过证据中。独立 PG 作者只写 `tests/projectvariable/execution_tool_call_facts_test.go`，只替换 Model JSON 端口，其他领域调用保持真实。

取消恢复的基础控制只表示对已观察 canonical `CancelRequestedAt` 的处理：当前 Execution 尚无公开 Cancel 命令写者，不声称本片新增或真实验证该 API。受控原事务验证已知回滚后转原 closing，Unknown 则仍保原候选。真实 PG 保持两场景：原句柄 held/Unknown 与当前正式 Agent 撤权、最终事务回滚。

原 PG supervisor/driver 只新增唯一 `TestExecutionToolCallFacts` 和两个子场景的 metadata 注册。离线 1 正 / 7 反控制已实际退出 0，剥该键后的两整 AST 与原有效映射等基线；原预算、实际 Wait、七资源与自动 TCP baseline/delta 双尾不变。原件 `output/ai/execution-tool-call-facts/checks/profile01.json`。

## 作者基础验证

原 `unit01` 五包限定 race 实际退出 1：Tool contract / Registry、AgentLoop、Execution contract 共 21 top 通过，Execution 包编译发现私有 `executionRound` 接口漏声明原有安全 `LogValue`。仅补 `log/slog` 引入和该方法签名，两种具体回合的原安全实现与旧断言均未改；原失败日志、命令、100 项有界输入清单和工具 Wait 保留。

随后 `unit02` 只补 Execution 原选择范围，13 top / 10 sub 实际通过（11.021 秒）；其余四包复用原通过结果。五包 `vet01` 实际退出 0（73.069 秒）。两阶段合计 34 top，包括 14 个新入口与 20 个适用旧入口；这是分段通过，不把首轮 FAIL 改写为通过。两次输入清单前后均相同，轮间唯一差额为上述两行接口修复。

精确 argv、进程与 actual Wait、输入和日志见 `output/ai/execution-tool-call-facts/checks/unit01-*`、`unit02-*`、`vet01-*`。尚未据此声明 PG、公开 Cancel 命令、多轮 Tool Runtime 或生产 tools 已通过。

PG 唯一测试的首次 `-race -tags=integration -c -p=1` 编译实际退出 0（Go PID 656310，109.403 秒），随后精确 `-test.list=^TestExecutionToolCallFacts$` 实际退出 0 且仅枚举该 top。1806 项原 driver 编译闭包前后及列表后均相同；runtime 仅追加真实 binary，原件见 `output/ai/execution-tool-call-facts/compile01/` 与 `native01/`。这仍是编译和配方准备，不代表两条 PG 业务场景通过。

## 首次真实链失败及显式捕获补口

`native01` 原工具 40757 实际退出 1，supervisor 659462 实际 1 / 136.730 秒。两个 sub 在依赖装配阶段返回 `CAPABILITY_UNSUPPORTED`，尚未进入 ToolCall 持久化断言；原通用 helper 未记录具体内部调用 stage，不将静态定位冒作额外运行观测。原 Go / driver 均实际 Wait 1，七资源双次不存在、descendants / private / runtime 双尾、自动 TCP delta 双空和输入未变门均完整。原件 `output/ai/execution-tool-call-facts/native01/` 保留，整轮为 FAIL。

静态定位确认不可满足的原路径：本夹具通过正式 Model 配置启用 `ToolCalls`，而原 `ExecutionCapture` 委托的默认 Resolver 明确拒绝该配置。不得删除原门或伪造 Snapshot。补口采用独立、显式的 `NewToolCallFactsCapture`，复用原真实 Model 配置、同事务计划 / Snapshot / Secret lease；只接受 AgentGeneration 的 direct 选择、文本模态、ToolCalls 且无 parallel / streaming / reasoning。捕获 revision 为 `execution-tool-call-facts-capture-v1`，表示配置捕获，未注册任何 HTTP adapter，也不等于被 STOP 的 tools adapter。

原 resolution identity / semantic / mapping 编码不变，完整 frozen draft 已把独立 revision 纳入 mapping。prepared 和 committed 历史须与调用 profile 一致，不能修改成另一 profile；最终 Resolve 与 Unknown 观察复核原候选、owner 和 revision。公开 `DiscoverResolve` / `ResolveModelInTx`、原 `NewExecutionCapture` 和 `Runtime.Chat` 仍走默认 no-tools 门。该补口尚未执行新增 Model 控制或复验 PG；不放宽原 A/B STOP 或默认生产接入。
