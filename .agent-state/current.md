# Execution 身份当前状态

- 树 `/workspace/agenteam-execution-identity`，分支 `ai/execution-identity`，起点 `fd8bbaeb`；root 唯一 Git 操作者。本域唯一写者 work_ui。
- 首片源：`internal/central/execution/{store,authority,repository,calls,service}.go`、`contract/{identity,launch}.go`、`launch_test.go`、`db/migrations/00038_execution_identity.sql`、[D22 卡](../docs/development/work-items/d22-execution-identity.md)。已落真实 SQL Launch/Lookup、全 Agent 活动占用、私有同 Tx launch witness 与受控正常/拒绝测试源码。
- 仅 gofmt/diff-check 完成，未运行 Go/PG；没有 live 资源或热 cache writer。新测试不冒真实 Task/Meeting/Agent 事实。
- Agent 依赖由 content 冻结：contract `70a7be59`，适配/测试最终 `0acfb716`，root 待 exact 导入3路径。Tool contract/DDL37来自 secret `b5e87476`；36/37及完整后继组合仅root管理，本树不复制跨域 source。38父固定 executions(id,project_id,agent_id) 与37复合FK。
- 尚缺真实 Trigger/Scheduler Project gate、preparing捕获、完整immutable Snapshot/Started、Model成功调用proof、ToolCall当前授权与整个Loop。capture/current现明确unbound；不能把created/配置DTO当running授权。
- 下一步：保存首片WIP；补齐真正固定输入/状态实现与实际提供者接线，按共享窗口做必要包级检查后真实PG。不改Model AgentRetry，不伪造Service为Human，不恢复旧STOP。
