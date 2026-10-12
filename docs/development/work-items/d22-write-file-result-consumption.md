# D22 Write 回执消费与后继只读投影

状态：2026-10-12 已随完整历史 main `ac02502698af864c20a65fd83d35467511f014be` 有限交付并实际推送。分阶段基础检查、最终 compile/list 与 native04 通过，唯一 principal 已接受 whole PASS；native01–03 原 whole FAIL 完整保留。完整原件已无覆盖保全，原树/本地分支普通删除及远端精确lease清理全部实际完成，详见[恢复说明](../../../.agent-state/write-file-result-consumption/README.md)。

依据：[Model/Tool 契约](d01-contracts/model-tool.md)、[Write Operation](d15-runner-write-file-operation.md)、[原结果消费](d22-execution-tool-result-consumption.md)、[DetermineRoundInput](d22-execution-round-input.md)及[只读 round projection](d22-next-round-projection.md)。本片只接受已持久、已知完成发布的 Write 回执，追加原 Execution seq3 和版本，再沿既有 Determine 与只读 Projection；不启动下一 Model、不改默认装配、无新迁移。

## 历史事实与事务

新增独立 `CommittedWriteFileCallAuthority`，由实际 Execution owner 在同 Store、live Tx、完整原锁集内借出原 ToolCall 与保护参数。它验证原 Round/Snapshot/captured Tool/Model 响应、原参数与 canonical 摘要，不查询新的派发权限或签发 current witness。原 Write dispatch authority 保持当前权限语义，不能用它适配历史口，也不能将历史口作为派发 grant。

`NewWriteFileResultReader` 只依赖 Store 与该历史口。私有 plan 先发现原 Operation，再持完整 union 重读；`WithWriteFileResultInTx` 使用调用者原 Tx，不嵌套事务或追加锁。原 Operation、retired Attempt/RID、terminal 原字节、69 回执 scope/SHA/长度及原 path、写入字节数逐项绑定，同步借用后清除临时 bytes。它不依赖旧 executor 活跃状态，不调用 Runner、Model 或公开 Lookup。

真实联调发现 reader 把整个历史发现序列放在同一个2s内层 deadline，导致外层 caller 仍 live 时历史借用已超时、消费 callback 尚未进入。修复将2s预算分别限定在两个原短事务上，完整历史发现及序列继承原 caller context，较短 caller deadline 仍优先；同 Tx 的结果借用不另建短 deadline。未另设全程总预算，全程仍受 caller context 约束；PG、driver、资源和 TCP 门不变。

独立 `NewWriteFileResultConsumer` 复用既有 consumer 生命周期和 Unknown 候选。首次写仍要求真实 Running、无 canonical cancel 及 Project 可写；已持久 receipt 的 Lookup/Resolve 核历史事实，不因后来撤去 Tool/Mount grant 而抹掉原结果。seq3、消费记录与 Execution version 在同一 Tx；真实 rollback 零部分，Unknown 只观察原候选，不重写或重发。原 Round 的 tool_pending/through2 不变，未实现通用 waiting owner 或 Execution 终态退休。

## 投影边界

Write 采用独立 typed binding，与 Read 互斥；原 Read canonical 字节与 32KiB 参数门不变。Write 原参数在既定 1MiB JSON/512KiB 解码数据门内核原始长度、SHA、canonical SHA、完整 captured schema 与回执 path/bytes_written，临时解码的文件数据使用后清除。

新增 ToolResult 只包含原 canonical Write receipt：`state`、`path`、`bytes_written`、`durable` 及可选 `completion_code`，原 CallID 不变。已知发布即使 durable=false 或含迟到完成错误仍保持原成功外层与 IsError=false，不伪称已经持久落盘，也不接收 Unknown 或缺失回执。原 seq2 assistant ToolCall 的完整 canonical 参数与 Transcript prefix 保留；显式只读 Projection 仍可访问这些原历史内容，默认格式化、JSON 和日志脱敏。不得将文件数据重复塞进新增 ToolResult，也不新增摘要格式或改写历史调用。

Determine 复用既有68的 cutoff、Skill 绑定及唯一 successor；Projection 只对原已提交输入作闭集 Read/Write 分派。66/68 的原成功回执约束可直接承载本片，无 DDL 修改。Command 的非零退出/取消报告消费不在本片。

## 实现与验证范围

skills_milestone_review 负责 Tool 历史合同与私有结果 reader；skill_package_review 负责 Execution 历史 authority、66 consumer、68 source 与 Projection reader；recovery_lead 负责 Loop receipt/round projection 和本卡；独立 fixture 作者负责 PG、方法及 README；variables_review 负责 Git 与全局状态，runtime_recovery 为唯一 principal。

基础证据按实际输入分阶段复用：`basic01` 原 17 top / 78 sub race 与5包 vet 通过，843项输入前后一致；deadline scope 修复后 `basic02` 仅补 reader 的4 top / 24 sub race 与同包 vet，572项输入前后一致。后者包含原3个 reader top 和1个新增 deadline 控；替换重叠范围后，最终闭集为 **18 top / 81 sub，5包 vet**，不是一次全量命令的结果。Loop 的5个新 top 及未受影响旧控沿原通过输入复用。三轮离线方法均保留1正7反；`compile01`、`compile02`、`compile03` 各自编译与精确 list 实际退出0，最终 compile03 的1991项输入一致，runtime1992项仅增加对应 binary。

真实入口 `TestWriteFileResultConsumption` 保持1 top / 2 sub。`revoked-dispatch-grants-consume-and-project` 先由真实 Write 链完成512KiB写入并独立分块全读，停止并 Drain 旧 dispatch owner 后，普通 Owner 正式撤去 Tool/Mount 授权；首次消费历史 receipt，seq3与版本同 Tx，再真实 Determine 和只读 Projection。`commit-loss-lookup-no-resend` 在消费事务实际 COMMIT 后受控遮蔽回执，撤权后原 consumer 与新 consumer 仅 Lookup 同一记录。原 Snapshot、Round、ToolCall、保护参数及 seq1–2 保持，每条链 Model与wire各一次，不再次 Consume、Execute 或调用 Model。本片未新增 callback rollback 场景，共用66写入机制的原回滚证据沿原片保留，不计作本轮运行。

native04 两 sub 分别55.64s、51.46s，合计107.10s。原工具 `83628` / chunk `c81c0b` 由执行作者亲收 actual0；supervisor `956700` Wait0 / 199.212s，Go `958337` 与 driver `956720` 原 Wait0。同7资源两次 absence 共14项、desc/private/runtime 双尾、75s原自动 HOST_TCP delta 两次 empty、1992项运行输入不变与四身份均齐，whole PASS 已独立接受。原件及各轮方法见[恢复说明](../../../.agent-state/write-file-result-consumption/README.md)。

三个失败轮次不回写：native01 在 fixture 创建目录时实际 ENOSPC、同进程 bavail=0，未到消费；native02 首消费失败、第二 DEPENDENCY_UNAVAILABLE，并检出运行中替换共享 delegate 的 race；该方法窄修后 native03 无 race warning，但历史借用内层 deadline 到期、消费 callback 为0，未到消费 COMMIT、Lookup、Determine 或 Projection 成功门。后继修复分别使用原锁保护的回执转换钩子和上述 deadline scope；各轮原 Wait、七资源及 TCP/输入尾与失败源码均保留，最终通过不回填旧轮业务。

范围仍是显式构造的真实 Owner/Execution/Tool/Runner 链，Model JSONCaller 是唯一模型端口替身，Project 原 test Skills receipt 边界保持。PG 实际健康写入的 receipt 为 durable=true、无 completion_code；durable=false 和迟到完成码仅由纯控制验证，未宣称真实 durable fault 注入。受控 COMMIT 回执丢失不等于物理数据库结果未知。

所有原 tools HTTP、默认 executor/initializer 与其他 STOP 保留；本片不启动下一 Model 或新 dispatch，不实现通用 waiting owner、Execution 终态退休、多轮工具执行或完整生产接入，也不表示整体目标完成。
