# Task / WorkClaim / Launch 恢复

- 当前树 `/workspace/agenteam-task-transition`，分支 `ai/task-transition`。WorkClaim 13 路径已保存并推送 `e66ac808`；原 Human 指派、Lookup/replay 与真实 Pending 接线保留。契约见 [D11 Task transitions](../docs/development/work-items/d11-task-transitions.md)。
- 已实现真实 Scheduler `Discover → Apply → CheckApplied`：原同 Store/Tx、完整锁与私有 Scheduler proof 下，将当前 Sprint 中的 todo Task 转为 in_progress；Task、两组排序、query/version、专用 Scheduler history/schema 2 Outbox 与私有 applied proof 同事务完成。旧 Human schema 1 及历史读取保持兼容，迁移为 00045。
- root 已从 AgentSystem `ddac1072` 精确借入 9 个 StartSprint 依赖路径（含迁移 00044），并随 `28e7a332` 保存，供本树独立恢复。组合 `pure-02` 的 14 top / 7 sub race 与 Work、Work contract、Project、Project contract 四包 vet 全 PASS，全部原 Wait 和退出尾已闭合。
- 真实 `TestSchedulerClaim` PG01：1 top / 2 sub、19.43s、whole PASS；1403 inputs 稳定、全部 Wait0、7 资源共 14 次 absent，所有双尾为空。该结果覆盖正式 StartSprint 后的真实 Claim/pending 正向与原最终事务晚失败回滚；原结果在 AgentSystem 组合树保留。
- 失败记录不覆盖：组合 `pure-01`（`e9ac`）因共享代码调用不存在的 `ProjectRef.Clone` 编译失败，0 top；窄修后才得到 `ddac1072` 的上述通过结果。早先 Human 纯测编译及容量预飞失败仍留本树原输出，不追认为通过。
- TaskLaunch 3 源 `341dcde7` 已推送，并入 AgentSystem `d3cfaeb1`；最终组合 `281fc3cb` 源码有限独审接受，新 8 top race 与三包 vet whole PASS。此前凭据 HTTP 401 阻断已解除，root 已确认组合及后继 `07dea` 保存推送完成；原纯验结果留 AgentSystem `output/ai/task-launch/combined-pure-01`。
- 真实 SchedulerLaunch PG01 两业务 sub 24.45s PASS，但 whole FAIL：唯一未通过门为 host TCP delta=1，supervisor/outer 原 Wait1；其余任务自有进程、资源及 inputs 尾均已闭合。原 TCP tuple 未保存，不能归因或追认门通过；原件保留在 AgentSystem。本结果不改变此前 Start/Claim 的整轮 PASS。
- AgentBusy 补偿首片 15 路径已保存并推送 `92a3c7b9`，消费先前冻结的 `22c7fa43` Work 合同：同 Store/原 Tx/完整锁与真实 Scheduler busy proof 下，恢复仍匹配原 claim 的 Task 逻辑位置，或完整保留后续用户变更；暂停时保留 confirmed-busy pending。仅恢复分支写专用 history/schema 3 Outbox，旧 Human 1/Claim 2 保持。
- 前向迁移 `00046_task_busy_compensation.sql` 来自 `92a3c7b9`，包含不可变 Work 补偿结果、第五 history arm，以及 Scheduler 同 attempt 的 busy marker/skip 字段；不改 42/45、不推断旧行 Busy。保留分支 Discover 在原读事务 Committed 后的取消检查已窄修并保存为 `b35077ee`，物理 Unknown 优先和原调用退出语义保持；46、Work/Project 与 title 差额均已有限独审接受。
- 正式已指派 in_progress Task 的 title-only 更新依赖 3 路径已从 `ce799` 借入，并保存为本树 `44e3ad97`。AgentSystem `07d490b2` 组合的 19 top race、四包 vet、候选编译与真实 Busy 1 top / 2 sub 均 whole PASS，全部原 Wait 和资源退出尾已关闭；原输出保留在组合树。此结果不改变原 Launch whole FAIL。
- Work provider 已实现真实 claimed `task/work` 启动来源校验及上述 Busy 恢复/用户变更保留链；自动调度循环、全 Snapshot/完整执行链仍未实现，不宣称完整 dispatch/capture/Execution/F1。源码停写，无本树 Go/native 在途；Git 保存、集成与资源窗口均由 root 统一处理。
