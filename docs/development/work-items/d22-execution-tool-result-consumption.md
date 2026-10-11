# D22：read-file 成功结果的单次 Transcript 消费

状态：2026-10-11 首片源码与基础控制已分段通过，PG 尚未编译或运行。从初始 Operation 候选 `2d4` 合入上游 `b553` 的完整开发历史与窄修源码：上游 native01/02/03 均 wholeFAIL，原 `d40fc527` codec 红控保留；最新 PG 的两行 `f.Progress` 类型修正已静审，compile04/list 原 35958 实际 0，native04 尚未运行，尚未整体验收。此合并只统一实际编译输入，不构成交付接受，不通过更换 harness 绕过上游未验范围。

## 本片边界

依据 [Agent Loop Canonical Transcript 与 checkpoint](../../architecture/agent-loop/README.md#6-canonical-transcript)、[Transcript 与 Model Context](../../architecture/agent-loop/transcript-context.md)、[ToolCall 持久事实](d22-execution-tool-call-facts.md)和现有 read-file Operation/Attempt 契约，将**已持久成功**的完整 read-file 结果一次性消费为原 Execution 的 Canonical Transcript 检查点。

只处理实际 Operation phase `succeeded`、terminal outcome `success`、原 Attempt 已退休且完整结果 payload 已同事务落库的 read-file。running、Unknown、failed、cancelled 不产生本片 ToolResult，不把未知转成普通错误。没有 Runner 重发、第二 Model 请求、默认 initializer 绑定或真实 Model tools HTTP；原 OpenAI tools A/B STOP 保持。Skill ingress/cutoff 和多 Round 仍未交付，不能借本片进入下一轮或终态退休。

## 同事务接缝

Tool 独立结果 reader 提供私有 `ReadFileResultPlan`、完整 `RequiredLocks` 及安全 binding。`DiscoverReadFileResult` 只发现原持久事实；`WithReadFileResultInTx` 在调用者同 Store、完整锁 union 和真实 current Execution/Agent/Tool 权威下重新核对原 ToolCall、保护参数、Operation 输入与 fingerprint、Attempt/RID、terminal metadata、结果引用及 digest/长度。完整 canonical 结果仅同步借用，回调返回清理借用字节，不保留临时 witness，不嵌套事务或做 Runner I/O。

AgentLoop 仅作纯安全投影：验证原 binding 与完整结果 bytes，输出一个 `role=tool` 消息，唯一 `ToolResultPart` 使用原模型 `tool_call_id`，`IsError=false`，唯一 TextPart 为协议原六字段 canonical JSON。Op/Attempt/RID、内部引用和根目录不进入模型内容。默认格式化为脱敏标记，持久 writer 通过显式 accessor 取得副本。

Execution 消费者在同一事务取得完整锁、借用真实结果并复核原运行中的 Execution、未确认取消及原不可变 Snapshot/Round/ToolCall。首次写入只追加 `transcript_entries` 的 sequence 3、独立消费记录，并对 Execution 当前 version 做一次 CAS 增量。00066 是唯一前向迁移，仅扩展原 Transcript 的受限第三项与本域消费记录；00056/00064 文件、原 Round 的 `tool_pending=true` / `transcript_through=2` 及不可变约束不改。

新消费记录绑定 P/A/E、原 Round、Snapshot/InputBinding、逻辑调用、Invocation/ToolCall、保护参数引用/摘要、Op/Attempt/RID、Operation 输入/terminal/result 摘要与完整消息摘要、原 checkpoint 时间/version。它是非终态消费事实，不冒充原 Round 已进入下一稳定 Model 输入，也不释放 Model/Environment/Mount 引用。

重复消费先核当前授权及完整原关联，再返回同一 receipt，不重复追加或推进版本。真实回滚保持零部分写；Commit Unknown 保留同一候选及原物理 attempt，只允许观察原消费记录的完整 binding、消息和时间/version，不重发 Runner/Model，不把“暂未查到”当作未提交。原实际事务返回后才完成本次调用的生命周期。

## 唯一写域

- `recovery_lead`：`internal/central/execution/contract/tool_result_consumption.go`、Execution 新 consumer/repository/recovery 与专属控；`db/migrations/00066_execution_tool_result_consumptions.sql`；本卡与本树 current。必要原文件接线只作有明确消费点的窄增量。
- `skill_package_review`：Tool 新结果 contract、独立私有 reader/plan 及专属控；不写本 Execution、迁移或旧 Operation 树。
- `skills_milestone_review`：`internal/central/agentloop/tool_result.go` 及 `_test.go`，仅纯投影，不启动下一 Model。
- 独立 PG 方法作者：唯一 `tests/projectvariable/execution_tool_result_consumption_test.go`；两原 harness 的 selector metadata 增量由 lead 统一交接唯一 writer，不改原接受门。

## 最小验证目标

基础控制核严格关联、错误/Unknown 拒绝、脱敏编码、同 Store/全锁/私有 plan、重复与候选观察。真实 PG 复用本树正式 Owner、原持久 ToolCall、认证 WSS 与 Workspace.Manager 的成功结果；Model 仍为明确的 JSONCaller 替身。两场景限定为正常完整结果/重复与新 consumer 恢复，以及事务末回滚零部分/实际 COMMIT 后回执丢失再仅观察。检查原 ToolCall/Round、旧 Transcript、Op/Attempt 和 wire/Model 次数不被消费改写。仍使用既有真实资源/Wait/TCP 退出门，不重复已接受的上游矩阵。

上述真实 PG 目标尚未执行；基础控制的实际结果见下，不代替真实链验收。所有 Git 操作和共享资源窗口由 root 统一处理。

当前实现提供显式 `NewToolResultConsumer(actual *ToolCallDriver, results ReadFileResultAuthority)`，`Consume` / `Lookup` / `ResolveUnknown` 与 `Stop` / `Drain` / `Joined`；无默认装配。Execution 新纯控为 4 top（其中候选关联 8 sub），在 basic03 实际通过；Tool 2 top/8 sub 与 AgentLoop 3 top 已在 basic02 实际通过。

两原 harness 仅新增 `TestExecutionToolResultConsumption` 的同包输入与精确 2 sub；资源、Wait、预算、自动 TCP baseline/delta 双空门不变。离线 `method01` 使用原门：1 正例、7 反例实际退出 0，剥新增 selector 后两文件完整 AST 与旧有效映射相同，4 项输入前后不变。原命令、PID、日志及结果位于 `output/ai/execution-tool-result-consumption/checks/method01/`；该结果不替代 Go 或真实链验收。

首次基础配方仅完成真实 deps 查询；原保守闭包门将既有 Execution 子包判为未允许依赖而退出 1，race/vet 均未启动，不属于产品控制失败。后继 basic02 原工具 52198 实际退出 1：Tool 2 top/8 sub 与 AgentLoop 3 top 通过，但 Execution 测试以两个变量接收 Lookup 的三个返回值而编译失败，vet 未启动；749 项实际输入前后不变。仅修改该测试的接收变量（6aa 窄修），产品及旧断言未改。

basic03 原工具 67354 实际退出 0：Execution 4 top/8 sub race（PID 719933）实际 0 / 8.606s，5 个相关包 vet（PID 720093）实际 0 / 69.289s，每阶段 749 项输入前后相同。复用前轮未变的 Tool/Loop 5 top，合计 9 top 分段通过；basic02 整体 FAIL 与首 deps 配方退出 1 均保留，不改写为单轮全绿。原件见 `output/ai/execution-tool-result-consumption/checks/basic02/` 与 `basic03/`。
