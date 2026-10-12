# D22 Command 已知报告消费与后继只读投影

状态：2026-10-12 从正式主线 `ac025026` 在 `/workspace/agenteam-run-command-result-consumption` 实施。当前为源码与控制准备，尚未运行 Go、PG 或真实联调，不记行为通过。

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

## 所有权与待验证范围

skills_milestone_review 负责 Tool 历史合同、bundle codec、共享分类器、私有 reader 与71；skill_package_review 负责 Execution 历史口、显式 consumer、68与Projection reader；recovery_lead 负责 Loop 结果/round projection、专控及本卡；独立 fixture 作者负责 PG、方法和 README；variables_review 负责 Git/current，runtime_recovery 为唯一 principal。

必要纯控检查已知正常、非零、信号、取消、超时及本地 I/O 报告的精确投影，双流边界与 JSON escaping/NUL、错误分类、损坏摘要、profile 互斥、原历史参数、复制与默认脱敏。旧 Read/Write 仅复用受影响的投影兼容控制，不重复 Runner/WSS/Operation 全矩阵。

拟真实 PG `TestRunCommandResultConsumption` 保持1 top / 2 sub：真实 exit23 报告持久后正式撤 Tool/Mount，首次消费并 Determine/Projection；真实 child readiness 后取消原 Execute，保 Execution Running/no canonical cancel，消费实际 COMMIT 后受控遮蔽回执，仅原与新 consumer Lookup，再 Determine/Projection。每条链仍只有原 Model 与 wire，不把受控回执丢失声称为物理数据库结果未知。具体方法与实际结果由独立 fixture 作者交付，当前未运行。

本片只显式构造。原 Model JSONCaller 与 Project test Skills receipt 边界、tools HTTP 及其他 STOP 保留；不接默认装配，不实现下一 Model、通用 waiting owner、多轮执行或 Execution 终态退休，不表示整体目标完成。
