# Human Owner Task transfer HTTP

树 `/workspace/agenteam-task-human-http`，分支 `ai/task-human-http`，基线 AgentSystem `07d490b2`。此片仅 HTTP、应用装配和 OpenAPI；不改 Work 业务、迁移、Execution 或生产 Project initializer。

- `POST /api/v1/projects/{project_id}/tasks/{task_id}/transfer`：`{expected_version,request:<TaskTransfer>}`。
- `POST /api/v1/projects/{project_id}/task-transition-commands/lookup`：`{command:"work.task.transfer",target_id,expected_version,request}`。两路要求唯一原 `Idempotency-Key`、当前 Human 和原 Account CSRF/Origin 边界；Lookup 从原意图计算正式 digest，只调用 Lookup，不发写、不用当前 Get 重建历史。
- 返回原 `TaskTransitionMutation` 与 `{status,receipt}` 三态；严格核 Project/Task、expected+1、目标 state、显式 assignee、历史和事件 ID。服务成功后坏投影 abort，不能编造未提交 Problem。Unknown 保原 safe Problem 与 lookup hint。
- app 在原 Catalog freeze 前登记 `TaskTransitionEvents`，使用同 Store 的真实 Agent WorkReferences、Execution WorkOccupancy、Scheduler PendingAuthority，复用原 Work/Project/Account/Outbox。`createWorkPlanning` 的可选尾参数仅接受一个非 nil 原 Project Authority；省略保旧三服务，新路由明确 DependencyUnbound。默认 root 唯一调用传既有 `projectUsage.projects`，第四服务纳原 Stop/Drain/Force；未绑定生产初始化事实保持未绑定。

2026-10-10 有限交付：HTTP 产品 `81ab48c2`、单文件真实 TLS fixture `e28f916e` 均经 content 独立源码审接受。gofmt、OpenAPI 静态检查通过，旧 paths/components 保持。

HTTP 新 `TestWorkHTTPTaskTransitionAndOriginalLookup`、`StrictIntent`、`BoundaryAndUnknown`、`ProjectionAndRoutes`、`StandardSchema`（后四同 `TestWorkHTTPTaskTransition` 前缀）；app 新 `TestWorkPlanningTransitionActualCallMustJoin`。必要兼容为 `TestWorkHTTPBindingAndExactRoutes`、`TestWorkPlanningPureConstructionAndDrain`。这 8 top 已在 AgentSystem 组合 16 top race 中通过，`scheduler`、`work/http`、`app` 三包 vet 通过；标准 schema 经原 `AGENTEAM_WORK_HTTP_SCHEMA_PYTHON` 实际执行。原 race/vet/outer Wait0、group/runtime 双尾闭合；原结果 `output/ai/pending-http/combined-pure-01/result.json`（AgentSystem 树），不重跑旧矩阵。

真实 `TestTaskHumanHTTP` 已在 AgentSystem `ccfd5674`／方法 `612d92a9` 的 pending+HTTP PG01 组合通过（本片 1 top／2 sub、16.01s）：

- `transfer-lookup-replay-and-get`：真实 P2 初始化、默认能力 Agent、Account Owner/CSRF，同 Store 四 Work 服务与真实 Pending/Occupancy，HTTPS Create→backlog 到 todo Transfer→原意图 Lookup/replay→Get；原 receipt、Task/history、Outbox、Activity 及零 Dispatch/Execution 一致。
- `owner-csrf-and-new-session-lookup`：非 Owner／错误 CSRF 零写，同 key 异义拒绝；正式 Logout 后旧 Session 拒绝，新真实 Session 仅用保存的原意图 Lookup 历史，无额外业务或 Activity 写。此项不冒充物理 CommitUnknown。

HTTPS 保留原证书校验、Secure cookie、精确 Origin；每次原响应 bounded EOF/Close/Content-Length 与 ServeHTTP 返回齐，原 Server.Close 实际 Wait 先于四服务 Stop/Drain 和 P2 清理。组合 Go/driver/sup/outer 全 Wait0，七资源 14 次 absence、private/runtime/desc/TCP 双尾关闭，1432 输入初尾一致、wholePASS。原结果 `/workspace/agenteam-agent-system-integration/output/ai/agent-system-integration/pending-http-01-control/result.json`。

恢复该组合使用 `/workspace/agenteam-agent-system-integration` 的上述 source/method、`.agent-state/agent-system-integration/pending-http-compile-01-launcher.py` 和同目录 `pending-http-launcher-01.py`，selector 为 `^(TestSchedulerPendingVisit|TestTaskHumanHTTP)$`。本 HTTP 树保留自身产品和 fixture，未包含 pending 新 fixture／完整组合入口，不能直接作为该轮完整运行来源。默认 app 接线已由 pure 验证；真实 TLS fixture 显式组装同一套领域能力，生产 Project initializer 仍未绑定，不宣称完整 F1、Scheduler 或 UI 已完成。本批未 SQL 种指派、未 StartSprint、未进入 Scheduler 专属 todo→in_progress。
