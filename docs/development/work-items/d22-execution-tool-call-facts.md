# D22 首轮 ToolCall 持久事实

状态：基于 `226ea38b` 实施；首契约与源码候选尚未编译或动态验证。本片止于真实原模型句柄完成并实际 Joined 后的 ToolCall 持久事实和 `tool-pending` 非终态 checkpoint，不声明完整多轮 Agent、ToolOperation、Runner 派发或生产 tools 接入。

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

OpenAI tools 独立 A/B STOP、生产 nil/unbound、旧默认 no-tools 均保留；归档 input08 不当作动态通过。不得通过新 driver 恢复停项，也不引入网络故障 probe、新依赖或默认绑定。当前未运行 Go、Node、服务或 native。
