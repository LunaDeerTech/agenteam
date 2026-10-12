# D22 Write 回执消费与后继只读投影

状态：2026-10-12 在 `/workspace/agenteam-write-file-result-consumption` 从正式主线 `4ebde5a7` 实施。当前为源码阶段，尚未运行本片 Go、PG 或真实链；原 Write Operation 的通过结果不代替本片验收。

依据：[Model/Tool 契约](d01-contracts/model-tool.md)、[Write Operation](d15-runner-write-file-operation.md)、[原结果消费](d22-execution-tool-result-consumption.md)、[DetermineRoundInput](d22-execution-round-input.md)及[只读 round projection](d22-next-round-projection.md)。本片只接受已持久、已知完成发布的 Write 回执，追加原 Execution seq3 和版本，再沿既有 Determine 与只读 Projection；不启动下一 Model、不改默认装配、无新迁移。

## 历史事实与事务

新增独立 `CommittedWriteFileCallAuthority`，由实际 Execution owner 在同 Store、live Tx、完整原锁集内借出原 ToolCall 与保护参数。它验证原 Round/Snapshot/captured Tool/Model 响应、原参数与 canonical 摘要，不查询新的派发权限或签发 current witness。原 Write dispatch authority 保持当前权限语义，不能用它适配历史口，也不能将历史口作为派发 grant。

`NewWriteFileResultReader` 只依赖 Store 与该历史口。私有 plan 先发现原 Operation，再持完整 union 重读；`WithWriteFileResultInTx` 使用调用者原 Tx，不嵌套事务或追加锁。原 Operation、retired Attempt/RID、terminal 原字节、69 回执 scope/SHA/长度及原 path、写入字节数逐项绑定，同步借用后清除临时 bytes。它不依赖旧 executor 活跃状态，不调用 Runner、Model 或公开 Lookup。

独立 `NewWriteFileResultConsumer` 复用既有 consumer 生命周期和 Unknown 候选。首次写仍要求真实 Running、无 canonical cancel 及 Project 可写；已持久 receipt 的 Lookup/Resolve 核历史事实，不因后来撤去 Tool/Mount grant 而抹掉原结果。seq3、消费记录与 Execution version 在同一 Tx；真实 rollback 零部分，Unknown 只观察原候选，不重写或重发。原 Round 的 tool_pending/through2 不变，未实现通用 waiting owner 或 Execution 终态退休。

## 投影边界

Write 采用独立 typed binding，与 Read 互斥；原 Read canonical 字节与 32KiB 参数门不变。Write 原参数在既定 1MiB JSON/512KiB 解码数据门内核原始长度、SHA、canonical SHA、完整 captured schema 与回执 path/bytes_written，临时解码的文件数据使用后清除。

新增 ToolResult 只包含原 canonical Write receipt：`state`、`path`、`bytes_written`、`durable` 及可选 `completion_code`，原 CallID 不变。已知发布即使 durable=false 或含迟到完成错误仍保持原成功外层与 IsError=false，不伪称已经持久落盘，也不接收 Unknown 或缺失回执。原 seq2 assistant ToolCall 的完整 canonical 参数与 Transcript prefix 保留；显式只读 Projection 仍可访问这些原历史内容，默认格式化、JSON 和日志脱敏。不得将文件数据重复塞进新增 ToolResult，也不新增摘要格式或改写历史调用。

Determine 复用既有68的 cutoff、Skill 绑定及唯一 successor；Projection 只对原已提交输入作闭集 Read/Write 分派。66/68 的原成功回执约束可直接承载本片，无 DDL 修改。Command 的非零退出/取消报告消费不在本片。

## 实现与验证范围

skills_milestone_review 负责 Tool 历史合同与私有结果 reader；skill_package_review 负责 Execution 历史 authority、66 consumer、68 source 与 Projection reader；recovery_lead 负责 Loop receipt/round projection 和本卡；独立 fixture 作者负责 PG、方法及 README；variables_review 负责 Git 与全局状态，runtime_recovery 为唯一 principal。

Loop 已新增 5 个源码控制入口，覆盖完整回执、迟到完成状态、脱敏/复制、512KiB 原调用历史及混合绑定和回执错配；受影响旧 Read 投影按必要范围验证，不重跑无关 Runner 矩阵。计划真实 PG 保持 1 top / 2 sub：正式 Write 持久回执后撤去当前 Tool/Mount，消费、Determine 与 Projection 仍保持原事实且不增加 wire/Model；另以真实 callback rollback 和实际 COMMIT 后受控丢回执验证零部分与仅 Lookup 恢复。方法通过与实际事务/资源退出分开记录，尚未运行不记通过。

所有原 tools HTTP、默认 executor/initializer 与其他 STOP 保留；本片不声称下一 Model、多轮工具执行或完整生产接入完成。
