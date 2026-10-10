# Execution 身份当前状态

- 树 `/workspace/agenteam-execution-identity`，分支 `ai/execution-identity`，起点 `fd8bbaeb`；root 唯一 Git 操作者。本域唯一写者 work_ui。
- 首片源：`internal/central/execution/{store,authority,repository,calls,service}.go`、`contract/{identity,launch}.go`、`launch_test.go`、`db/migrations/00038_execution_identity.sql`、[D22 卡](../docs/development/work-items/d22-execution-identity.md)。已落真实 SQL Launch/Lookup、全 Agent 活动占用、私有同 Tx launch witness 与受控正常/拒绝测试源码。
- 仅 gofmt/diff-check 完成，未运行 Go/PG；没有 live 资源或热 cache writer。新测试不冒真实 Task/Meeting/Agent 事实。
- 当前保存源 `373428ea` 已含 root 导入的 Agent execution-configuration 三路径；实际接口与本域调用一致。Tool contract/DDL37来自 secret `b5e87476`；36/37及完整后继组合仅root管理，本树不复制跨域 source。38父固定 executions(id,project_id,agent_id) 与37复合FK。
- 尚缺真实 Trigger/Scheduler Project gate、preparing捕获、完整immutable Snapshot/Started、Model成功调用proof、ToolCall当前授权与整个Loop。capture/current现明确unbound；不能把created/配置DTO当running授权。
- 定向 Go 范围由 root 收敛为 AgentSystem 组合的一次21 top/受影响包 vet，包含本域4 top 与 Agent capture3 top；由 content 执行，本树不再单跑7 top，不认领未收到的动态结果。不扩 Snapshot/Model proof、不重旧 Agent/Runtime 测试。
- content 在准备37–39真实迁移时发现：37 Tool scope三列是 text domain，而38父三列原为 uuid domain，直接复合FK类型不兼容。与 coordination 有限核完整38/相邻32、37、39及本域repository后，仅把尚未应用的38自有 safe_id 改为 text，保原canonical UUID v7/variant正则、FK及所有约束。38不向Project/Agent建FK，39仅Skill自域父FK；原Go始终.String()传参、::text扫string/ParseID，未见二进制UUID依赖。此为源码修复，尚未PG验证；root将复制修后38至AgentSystem，由原真实迁移检查闭合。
