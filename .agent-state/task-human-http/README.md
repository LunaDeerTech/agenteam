# Human Owner Task transfer HTTP

树 `/workspace/agenteam-task-human-http`，分支 `ai/task-human-http`，基线 AgentSystem `07d490b2`。此片仅 HTTP、应用装配和 OpenAPI；不改 Work 业务、迁移、Execution 或生产 Project initializer。

- `POST /api/v1/projects/{project_id}/tasks/{task_id}/transfer`：`{expected_version,request:<TaskTransfer>}`。
- `POST /api/v1/projects/{project_id}/task-transition-commands/lookup`：`{command:"work.task.transfer",target_id,expected_version,request}`。两路要求唯一原 `Idempotency-Key`、当前 Human 和原 Account CSRF/Origin 边界；Lookup 从原意图计算正式 digest，只调用 Lookup，不发写、不用当前 Get 重建历史。
- 返回原 `TaskTransitionMutation` 与 `{status,receipt}` 三态；严格核 Project/Task、expected+1、目标 state、显式 assignee、历史和事件 ID。服务成功后坏投影 abort，不能编造未提交 Problem。Unknown 保原 safe Problem 与 lookup hint。
- app 在原 Catalog freeze 前登记 `TaskTransitionEvents`，使用同 Store 的真实 Agent WorkReferences、Execution WorkOccupancy、Scheduler PendingAuthority，复用原 Work/Project/Account/Outbox。`createWorkPlanning` 的可选尾参数仅接受一个非 nil 原 Project Authority；省略保旧三服务，新路由明确 DependencyUnbound。默认 root 唯一调用传既有 `projectUsage.projects`，第四服务纳原 Stop/Drain/Force；未绑定生产初始化事实保持未绑定。

2026-10-10 源码准备：指定 Go 源 gofmt 通过；OpenAPI JSON/所有 schema 语法、七个定向静态向量通过，旧 paths/components 逐项未改。未运行 Go 编译、测试、vet、真实 HTTP/PG 或浏览器；旧业务通过不替本片结果。

后继定向检查：HTTP 新 `TestWorkHTTPTaskTransitionAndOriginalLookup`、`StrictIntent`、`BoundaryAndUnknown`、`ProjectionAndRoutes`、`StandardSchema`（后四同 `TestWorkHTTPTaskTransition` 前缀）；app 新 `TestWorkPlanningTransitionActualCallMustJoin`。必要兼容仅既有 `TestWorkHTTPBindingAndExactRoutes`、`TestWorkPlanningPureConstructionAndDrain`；affected 包为 `internal/central/work/http` 和 `internal/central/app`。schema 用原固定 `AGENTEAM_WORK_HTTP_SCHEMA_PYTHON`；Go/资源须 root 窗口，当前均未执行。

首真实链待后续授权：真实 Account Owner/CSRF、已初始化 Project 和正式 Agent，Task Create→backlog 到 todo Transfer→原意图 Lookup/replay→Get/持久事实；非 Owner、失效 Session、错误 CSRF 拒绝。不得 SQL 种指派、假空占用或启用 Scheduler 专属 todo→in_progress。
