# M1 多轮执行与真实工具结果

本卡执行已确认的 M1：真实 Model → read-file → 持久结果 → 下一 Model，随后在同一个 Execution 中至少两轮工具交互并保存最终输出，供当前 Human Owner 的正式评审入口消费。Execution 成功不写 Task done。

## 当前组合和所有权

- 基线 c75e1bb8，执行工作树 `agenteam-m1-execution-loop`。本树唯一写者负责 `internal/central/execution/`、`internal/central/agentloop/`、执行域测试和前向迁移 00091；其他域迁移需求先交 root 协调。
- Model 由 W1 提供显式 tools capture 与真实 `model.Runtime`，仍通过 `mc.JSONCaller` 和 `ConsumerAuthority`。旧 text snapshot/revision 不改写；自动请求重试仍仅归 Model。
- Tool 由 Runner/Tool owner 提供按 `ExecutionID/RoundID/CallID` 的精确派发、持久结果和原同事务结果 reader。Execution 不以 DTO 或收到回包冒充结果已持久化。
- App/config 由 W5 装配可选 `tool-loop-v1`，共用原 Store、Project/Agent 权威、ProcessGuard、AssociatedExecutor 和原生命周期。

## 实施边界

新的循环必须实际扩展 canonical Round、ToolCall、结果和 Transcript 的单轮约束。每轮请求在发送前持久固定完整历史、ToolSet、CallID 和输入摘要；每个结果与原 Operation/Attempt/Runner Request 精确配对。未知提交只查原事实，不能换 key 或重复派发。取消保留原调用所有权直到 Model/Runner/Store 实际返回；Finalize 可结算原调用但不再授权新调用。

沿现有当前权限和明确 allow 分支接写/命令；需要尚未实现审批或 Secret 环境能力时保持拒绝。原 tools 独立动态 STOP 由 root/独立负责人核实，本卡不运行或变换受阻驱动。

最终文本使用 `durable_final_text_v2`：与 v1 同外壳，generation 为原 Snapshot，成功只有一个 text item，ID/source_reference 均为最终 RoundID；seq/revision/progress 为 1，item.started_at 为最终轮开始，满足 Execution.started_at ≤ item.started_at ≤ Execution.completed_at。失败/取消为空项。v1 的时间和来源契约保持。

## 验证出口

先验证请求/结果配对、当前授权、重复推进零重复调用和实际取消退出，再以真实 PG、Model adapter、Runner 做首条只读组合；随后覆盖至少两轮工具交互、最终输出/重启读取及 Owner 提交评审。源码自测、真实联调与独立验证分别报告；当前尚未产生本卡通过结论。


## 2026-10-14 候选实现

已落首轮 schema4/完整ToolSet 与后继 schema3；循环保留原 Snapshot/Model Secret lease，结果由原 RunnerResultAuthority 在同一事务借出，Transcript 与后继 Round/唯一 CallID 同事务固定后才进入下一次 Model。前向迁移00091解除单轮唯一键并新增 schema4 专属位置/结果来源约束；旧版本保留原形状。最终v2生产者复用真实最后一轮ID/时间。

`bef5ba7f` 首编译 exit0，随后小片修复复用 App 已注册 Loop、v2旧跳过门和含Actor函数字段的结果比较。限定 ordinary 请求/多轮历史/输出/旧行为测试 exit0；新增四次工具交互语义测试保留完整3工具、全部历史和原credential lease，单独验证最后一轮来源。尚未宣称真实数据库、App、Model/Runner组合或M1通过。

当前联调前边界：动态Skill ingress已有queued变更时明确返回CapabilityUnsupported，不静默丢弃；未完成其后继cutoff/control投影。运行中进程重启不自动接管；Tool未知与取消的精确原owner join端口由Runner已提供，Execution消费仍待补齐。恢复及故障最终事实继续保持原授权和Unknown，不能以未绑定端口代替验收。
