# Task Human 指派与转态恢复

- 当前树 `/workspace/agenteam-task-transition`，分支 `ai/task-transition`；基线 AgentSystem `34960978`。本批 Human 16 源 `649ce950`，测试 Sprint ID 类型修正 `2461f61e`；旧真实 Pending 构造注入另存 `845b79a5`。公共语义见 [D11 Task transitions](../docs/development/work-items/d11-task-transitions.md)。
- 已实现当前 Human Owner 将 backlog Task 显式指派同 Project initialized active Agent 并转 todo；同原 key 的 Lookup/replay、opaque Prepare/InTx、完整锁与真实 Occupancy/Pending/Blocker 检查、Task/排序/history/Outbox/Activity/receipt 原子写入，以及迁移 00041。旧 Create/PATCH 函数签名保留，排序写必须注入真实 Pending gate；无新 HTTP 路由。
- 必要纯验收有效补集：`pure-01` 的 Project 新/旧 2 top PASS；`pure-03` 的 Work 6 top race（9.173s）及 Work/Project vet（19.353s）PASS。session `94288` → `1a3dfa`，outer/Go/vet `930645/930649/930782` 全 Wait0，group/runtime 双空、adopted=[]，热 cache 已归还。作者与独立有限源码审接受，不冒真实 SQL。
- 原失败保留：`pure-01` Work 测试错误使用 Work Sprint marker，编译 0 top FAIL，已只改为正式 Project Sprint marker；`pure-02` 同进程 fresh 5,314,179,072B 未达 5GiB，0Go。原材料留本树 `output/ai/task-transition/pure-{01,02,03}`，复现为同目录 `run-pure-repair-03.py --cache /workspace/agenteam-project-variable-lifecycle/output/ai/project-variable-lifecycle/go-build`；启动仍需 root 独占窗口与 fresh 原门，不覆盖既有输出。
- 真实 Human `TestTaskTransitionHuman` 两 sub 由 skills 在 AgentSystem 组合树实施，当前编译/PG安排中；本树不宣称 00041、正式指派或 late rollback 的 PG 已通过。
- 后片仍未完成：Work SchedulerClaim/Launch Trigger 实现与当前 Sprint 正向来源；仅 claim 公共合同 `36a7e9bf` 已冻结。Agent Scheduler-current 三新源归 secret；Scheduler Pending/Dispatch 与 00042 归 cleanup；Project配置/00043 归 work_ui。没有正式 StartSprint，不 SQL 置 current；不以 Human prepared 证明 Scheduler，也不宣称完整 dispatch/capture/Execution/F1。
- 当前生产源码停写，无本树 Go/native 在途。所有 Git 保存、组合与资源窗口由 root 统一处理。
