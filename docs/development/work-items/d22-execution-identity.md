# D22 Execution 身份与固定输入

本卡记录 [D22 完整执行工作项](../development-plan.md#d22-executor-与-loop) 的实现入口，完整范围仍含 Loop、Transcript、等待、取消、Checkpoint 与真实恢复。本片依据 [D01 Execution 契约](d01-contracts/execution-orchestration.md)；当前没有生产 Runner、Scheduler、Meeting 或 Agent 运行接入结论。

## 模块关系

- Execution 独占 `internal/central/execution/` 和迁移 `00038_execution_identity.sql`，管理真实身份、全 Agent 活动占用、原 Launch key 与未来 immutable Snapshot。37 的 Tool Operation 只持标量，38 将其 `(execution_id,project_id,agent_id)` 外键绑定到真实 Execution 父身份。
- Task/Meeting 通过显式注册的 `TriggerProvider` 提供预收完整锁的计划，并在原 caller Tx 验证真实来源关系、launch intent、当前 gate 与完整 policy。普通构造出的 Trigger/Actor/permit DTO 均不产生 Execution 授权；缺 provider 不写 created/slot。
- Human 调用直接消费现 Project `RequireOwnerInTx` 当前 Session/Owner/初始化及 lifecycle 门。真正非 Human 调用必须有 Project/Scheduler 所有者实现的 `ServiceProjectAccess`，不得用 Service 名称或伪 Human Session 放行。该真实提供者目前未绑定。
- Agent 的 `ExecutionConfiguration` 接口由 Agent 所有者实现。Execution 在成功校验 Project/Trigger 后、canonical created 插入前发出私有同 Authority/同 Tx/完整 Actor 与 P/A/E/原命令 witness；Agent 再读取真实 initialized canonical。launch、preparing capture、running current 为不同阶段，不能用 running 门形成首次 Snapshot 捕获循环。
- Model 必须提供原持久调用、固定输入、Invocation 和成功 terminal 对应的真实证明；当前 Model 的 AgentRetry/Agent 消费口未绑定，不能凭 ModelResponse DTO、历史模型配置或任意 payload 签出 ToolCall 权限。
- Tool Runtime/D19 独立负责 Registry 当前 binding、Backend、scope/risk/审批。Execution 后继 current proof 还须消费当前 Agent allowlist 与 policy；固定 Snapshot 本身不是当前 ACL。

## 当前首片

已落 Launch/Lookup 的真实 SQL 路径：授权读取旧 key → 未观察时发现来源锁 → 原 command/User/Project/Task schedule/Agent/Execution/来源锁完整 union 一次 Acquire → 当前 scope/来源重验 → 私有 launch witness/Agent 全配置 → 全局 Agent 占用检查 → 原子 created 行。数据库唯一索引覆盖 created/preparing/running/waiting，原 key/digest 和来源身份不可修改。Lookup 不写任何事实，not_observed 不证明旧请求不会迟到；提交 Unknown 保留原 attempt/cause，不能重发新 key。

迁移 38 当前为首片草稿，依赖 root 组合已保留的 36/37；尚未验证连续升级或真实约束。它不修改 Agent/Tool/业务域表，除正式约定的 Tool→Execution 父关系。默认应用未装配新 Service。

preparing 捕获、完整 immutable Snapshot、Started 同 Tx、原 Model 成功输入、ToolCall proof、运行中取消/终态/资源收尾仍未实现。当前 Agent `capture/current` callback 显式 `DEPENDENCY_UNBOUND`，不能据 created 行、NewAgentRun 或本片测试变为 running/授权事实。首片不是 D22 完成。

## 必要验证

新增受控测试源码覆盖 Launch/原 key 重放/新 key busy、撤权/来源陈旧/缺 provider、Unknown 原 key Lookup 与私有 sameStore witness；真实 repository 与 callerTx 路径参与，SQL/Project/Trigger/Agent 半边明确受控。当前仅 gofmt 与差异检查，无 Go 运行、无真实 PG。

后续先验证本包必要正常/拒绝范围，再组合真实迁移和权威端口验证 Task/Meeting 同 Agent 竞争、Unknown、回滚与当前授权。真实提供者不全时保持未绑定，不把受控通过或新增 schema 当生产联调成功；不重复已通过且未变的 Registry/Model/Agent 全套。
