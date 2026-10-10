# Task / WorkClaim 恢复

- 当前树 `/workspace/agenteam-task-transition`，分支 `ai/task-transition`。WorkClaim 13 路径已保存并推送 `e66ac808`；原 Human 指派、Lookup/replay 与真实 Pending 接线保留。契约见 [D11 Task transitions](../docs/development/work-items/d11-task-transitions.md)。
- 已实现真实 Scheduler `Discover → Apply → CheckApplied`：原同 Store/Tx、完整锁与私有 Scheduler proof 下，将当前 Sprint 中的 todo Task 转为 in_progress；Task、两组排序、query/version、专用 Scheduler history/schema 2 Outbox 与私有 applied proof 同事务完成。旧 Human schema 1 及历史读取保持兼容，迁移为 00045。
- root 已从 AgentSystem `ddac1072` 精确借入 9 个 StartSprint 依赖路径（含迁移 00044），供本树独立恢复；当前由 root 待统一保存，未改这些依赖。组合 `pure-02` 的 14 top / 7 sub race 与 Work、Work contract、Project、Project contract 四包 vet 全 PASS，全部原 Wait 和退出尾已闭合。
- 真实 `TestSchedulerClaim` PG01：1 top / 2 sub、19.43s、whole PASS；1403 inputs 稳定、全部 Wait0、7 资源共 14 次 absent，所有双尾为空。该结果覆盖正式 StartSprint 后的真实 Claim/pending 正向与原最终事务晚失败回滚；原结果在 AgentSystem 组合树保留。
- 失败记录不覆盖：组合 `pure-01`（`e9ac`）因共享代码调用不存在的 `ProjectRef.Clone` 编译失败，0 top；窄修后才得到 `ddac1072` 的上述通过结果。早先 Human 纯测编译及容量预飞失败仍留本树原输出，不追认为通过。
- Launch 提供方、自动调度循环及完整执行链尚未实现；不以 Claim 通过宣称 dispatch/capture/Execution/F1 完成。源码停写，无本树 Go/native 在途；Git 保存、集成与资源窗口均由 root 统一处理。
