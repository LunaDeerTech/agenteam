# Task execution trigger 恢复检查点

本树 `/workspace/agenteam-task-execution-trigger`，分支 `ai/task-execution-trigger`，基线 `f8962895`。当前模块源为 `b98d0b9b`；共同 Execution preparation 契约来自 `7effdf66`。源码冻结，Git 保存、组合与资源窗口由 root 负责。

## 实现与写域

六个新源位于 `internal/central/work/`：`task_trigger.go`、`task_trigger_input.go`、`task_trigger_repository.go` 及各自 `_test.go`。三个生产源已获 coordination 有限实际源码独审接受。

`NewTaskTrigger(store, authority, nil).ReadTaskInput` 使用真实当前 Human Owner、同 Store 原事务和 User/Project/Schedule/Task 锁，返回 Task、Sprint/Milestone、全部未决 blocker 与有界最近事件。backlog/planned 可以作为普通观察，不构成执行资格。Capture 必须先取得 Execution 同事务私有 preparing 证明及真实 Project gate 视图；Discover 不读取受保护内容。缺依赖、未知策略或来源不符均拒绝，Build 侧仅严格解码原捕获字节，不替换为新的当前内容。

## 实际有限验证

在 AgentSystem 组合 `a962ccc4`，修后 `combined-pure-02` 的 Task 六个新 top（0 sub）race 全 PASS；Execution/Work 两包 vet 均 PASS。session `15741` → `ab300b`，Go `877176`（9.688s）、vet `877332`（3.518s）、outer `877172` 均实际 Wait0，原 group/runtime 尾关闭。复用该组合结果，不在本树重复同输入测试。

原 `combined-pure-01` 保留：Execution 六 top/六 sub race PASS，Task 因不存在的 `f.AgentAggregate` 编译失败、0 top 执行；两包 vet 当轮未执行。`b98d0b9b` 仅将该行改为正式 `f.AgentLock`，由第二轮 Task 补集和原未执行 vet 闭合，不追认首轮整体通过。

原输出在 `/workspace/agenteam-agent-system-integration/output/ai/execution-preparation/combined-pure-01/` 与 `combined-pure-02/`，不复制到本树。

## 下一步与边界

AgentSystem 源码 `879a7252` 的真实 `TestExecutionPreparation` 1 top/4 sub（31.31s）whole PASS，所有原 Wait0、资源尾关闭。其中 `current-owner-task-input` 通过正式 Work 服务创建 Milestone/Sprint/Task，真实当前 Owner 完整输入读取及 Session 撤销拒绝均通过；本模块复用这一有限 PG 证据。原记录由 skills_http 保存在 `/workspace/agenteam-agent-system-integration/.agent-state/agent-system-integration/current.md`，不复制原输出。

Task 指派/状态转换、Scheduler Dispatch/claim 和 Meeting 真实提供方尚未实现。本轮不宣称真实 Launch、完整 capture、完整 preparation input、Snapshot/running 或 F1 接通。完整 preparation 必须所有必需域在同事务内成功；缺 provider 不得提交部分输入、引用或 lease。当前无本模块 Go、PG 或缓存使用者在途。
