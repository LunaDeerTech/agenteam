# D22 Command 已知报告消费与后继只读投影

状态：2026-10-12 显式 Command 已知报告消费已完成 scoped basic、编译与 `native03` whole actual 0；唯一 principal 已独验最终有限 wholePASS，允许保全并合入main。测试产品、PG 与方法固定为 `1ab01c81352f254ee45123ed29a08f40b1c310c1`，来自正式主线 `ac025026`，工作树 `/workspace/agenteam-run-command-result-consumption`。本次结果文档在测试后修改，最终文档提交不会替换原 tested 输入；尚不声称主线交付、清理或整体目标完成。

依据：[Model/Tool 契约](d01-contracts/model-tool.md)、[Command Operation](d15-runner-command-operation.md)、[Write 回执消费](d22-write-file-result-consumption.md)、[DetermineRoundInput](d22-execution-round-input.md)和[只读 Projection](d22-next-round-projection.md)。本片从70已持久的完整 Command 报告追加原 Execution seq3 与版本，沿68 Determine 后只读投影；不启动下一 Model，不重新派发。

## 事实、事务与结果分类

独立 historical committed-call authority 在同 Store、live Tx、完整原锁集和私有 plan 下核原 ToolCall、Round、Snapshot、captured binding 与保护参数，借出原 bytes 后清除。它不查询新的 current allow、环境、Tool/Mount grant，也不成为派发权限；原 Command current authority 不变。结果 reader 不调用公开 Lookup 嵌套事务，不依赖旧 executor 活跃状态。

Tool owner 重读原 Operation、retired Attempt/RID、原 terminal 与70的报告、双流及三个 SHA/长度。只接受已经持久且 `report_available=true` 的完整已知调用报告；未知 Wait、断链或缺失报告不能合成已知结果。发现过程中的每个原短事务单独受2s边界约束，全程仍受 caller context 约束；同 Tx 借用不另加整段短 deadline。

wire `completed` 是调用报告。Tool 的既有分类顺序保持：先原 completion code，再输出完整性、signal、exit code；正常零退出才是 success，非零退出、信号和本地故障保留真实 failure，取消或超时保留 cancelled。`output_complete=false` 的已知报告仍可消费；它不等同于未知结果。共享纯分类器由 Tool owner 提供，Loop 不复制另一套映射。

首次消费仍要求 Execution Running、无 canonical cancel 和 Project 可写。消费记录、seq3、版本在同一 Tx；DB 回执 Unknown 只 Lookup 原候选，不重写、不重发。已持久历史读取不因后续撤去派发 grant 而失效。真实 Command 取消属于原 `ExecuteRunCommand` 上下文与相关 Cancel 帧，不写 Execution canonical cancel；PG 必须独立确认这一差别。

## 封存与投影

结果采用 canonical `{report,stdout,stderr}` envelope，report 保持原协议 Validate 和完整 exit/signal/时长、取消、截断、输出与进程组事实，元数据不超过32KiB；stdout/stderr 各不超过64KiB UTF-8 bytes。合法 NUL 保留为 JSON string 转义，不静默替换或截断。实际编码后的完整 envelope 不超过1MiB，摘要与长度绑定全部内容。71的 Command 专属约束由实际 ToolCall 的精确 tool/spec/operation/revision/protocol 与原 binding 一致性封闭；Read/Write 旧限额与成功形状不放宽。

新增 ToolResult 保留唯一原 CallID，Text 为完整 canonical envelope，IsError 与真实归一 outcome 相符。Read、Write、Command 是互斥的三个显式 profile。原 Read/Write canonical 字节和参数/结果预算、外层16MiB上限保持；完整 seq2 argv、captured Tool schema 和 Transcript prefix 不改写。显式访问可含原参数与双流，默认 JSON、格式化和日志仍脱敏。

68的 cutoff、Skill 绑定、唯一 successor 和 SQL 来源关系复用；后继 reader 从原封存 binding 读取 Command outcome/code，不能硬填原 Read/Write 的 success。Projection 不触发 Model、Runner、调度或资源退休。

## 所有权与实际验证

skills_milestone_review 负责 Tool 历史合同、bundle codec、共享分类器、私有 reader 与71；skill_package_review 负责 Execution 历史口、显式 consumer、68与Projection reader；recovery_lead 负责 Loop 结果/round projection、专控及本卡；独立 fixture 作者负责 PG、方法和实际执行，本轮最终 README 与本卡由 recovery_lead 统一收口；variables_review 负责 Git/current，runtime_recovery 为唯一 principal。

`sequence01` 原工具 `90163 / ed9d55` actual 0：方法1正/7负，四个测试包23 top（16新+7必要旧控）/139 sub 全过，五包 vet 实际0；unit 979174 Wait 0 /139.754s、vet 980928 Wait 0 /84.060s，857 inputs 前后相同。纯控覆盖已知正常、非零、信号、取消、超时及本地 I/O 的分类与投影、双流/escaping/NUL、损坏绑定、profile互斥、完整历史参数、复制和默认脱敏。旧 Read/Write 只做受影响兼容控制，不重跑原 Runner/WSS/Operation 矩阵。

`compile01` race编译982090 Wait 0 /86.164s、精确list 983255 Wait 0 /1.083s；2006编译输入前后同，runtime只加原90,630,940B binary成为2007项（SHA `9a7a97668bb656a5af575851bff67e9afecbc59ecf42decabfa1cf253a833086`）。

`native03` 原工具 `58011 / a581e7` whole actual 0：supervisor 1016102 /126.116s（raw 125.022s），driver1016123、Go1017739亲收Wait0。`TestRunCommandResultConsumption` 1 top /2 sub在33.62s通过，分项16.90s/16.72s：真实exit23报告持久后正式撤Tool/Mount，首次消费并Determine/Projection；真实双流readiness后仅取消原Execute，SQL证明Execution仍Running/no canonical cancel，fresh context消费实际COMMIT后受控丢ack，仅原/新consumer Lookup再Determine/Projection。每链Model1/wire1；不把受控回执丢失当数据库物理提交未知，也不把Command已知取消报告称为canonical Execution cancel。

原七资源14次absence、desc/private/runtime双尾、自动HOST_TCP delta在原75s门内双空、2007输入和四身份不变全部完成；PG540+60s、Go6m及原record/资源门未改。71检查包括1个完整合法pair通过全部deferred约束后sentinel真实回滚、2个Command pair精确约束拒绝及1个claimed Read即时bytes guard拒绝；其中旧Read大尺寸反控只证明即时尺寸guard，不是完整历史Read生产/配对验收。其他classification、output-incomplete和最坏双流escaping范围来自pure controls，不扩为PG额外故障注入。

## 原失败与容量记录

`native01` 仅prepared，从未执行。首次实际尝试`native02`原工具`75881 /626ce1` whole actual1，supervisor991784 /78.957s；两个sub均在自有目录MkdirTemp报ENOSPC/可用块0，未到fixture或消费。其原Wait、七资源/TCP全尾与2007输入证据完整保留，不能改写为业务通过。

容量整理另获两次明确授权：`cache-recovery01`固定78个可再生Go包缓存，人工旧缓存cutoff为2026-10-12T00:00:00Z（不是Popen时间/原Go Trim阈值/无引用证明）；`cache-removal01`逐原stat/header重核后仅精确unlink这78个regular `-d`，原`f1db57` actual0、outer1013030/child1013033 Wait0 /0.313s，allocated释放1,078,173,696B，索引和目录保留。没有归档派生包bytes，后继helper可能重建并使容量回涨；原2007/四身份/source/binary/modules/MinIO及验收原件保持。

PG parent available实采3,005,591,552→4,082,937,856B；native03 prepare观察4,082,470,912B。共享FS差额不孤立归因，也不把整理视作后继容量成功保证。本片[方法与实际结果说明](../../../.agent-state/run-command-result-consumption/README.md)集中链接[sequence](../../../output/ai/run-command-result-consumption/sequence01/terminal.json)、[native03有限结果](../../../output/ai/run-command-result-consumption/native03/post-check.json)、[native02原失败](../../../output/ai/run-command-result-consumption/native02/post-check.json)与[精确缓存释放](../../../output/ai/run-command-result-consumption/cache-removal01/result.json)小proof；原output及/tmp原日志未改写。

本片只显式构造。原 Model JSONCaller 与 Project test Skills receipt 边界、tools HTTP 及其他 STOP 保留；不接默认装配，不实现下一 Model、通用 waiting owner、多轮执行或 Execution 终态退休，不表示整体目标完成。
