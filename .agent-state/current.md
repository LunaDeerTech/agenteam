# TaskDispatch 当前检查点

- 物理树：`/workspace/agenteam-skill-install-runtime-source`；当前分支 `ai/task-dispatch`。目录旧名用于保留原 Runtime Source 纯验材料的绝对路径，不代表当前写域。
- 本批源码基线 `a6ddde3d`：Scheduler PendingAuthority、四态持久模型及迁移 `00042`、单次 claim coordinator、私有同事务证明、配额分批观察；含配套 Execution capacity 与 Agent scheduler-current 提供方。已精确集成 AgentSystem `d041370d`。这不是完整 Scheduler 或生产接入完成。
- 首片 `0434bc76` 的 5 top / 12 sub race 与 Scheduler 包 vet 已实际通过（session `43029` → `762123`）。原结果、日志、runner 和输入前后记录仍在 `output/ai/task-dispatch/pure-01/` 及同层 `pure-01-*` 文件。
- 后片组合 `a6ddde3d` 的 12 top / 42 sub race 与 Scheduler、Execution、Agent 三包 vet 已实际通过（session `78264` → `d9b84b`）。其中 Scheduler 5/17、Execution 4/12、Agent 3/13；race、vet 原 Wait 均为 0，group/runtime 双尾为空，adopted=[]，1263 个源码输入前后相同。准确 argv、结果、日志在 `output/ai/task-dispatch/coordinator-pure-01/`；同层保留 `coordinator-pure-01-launcher.py` 和输入前后记录。旧通过范围未重跑。
- 首片、Scheduler 后片及两处返修已有有限独立源码审接受；修复包含真实 outbox 锁归一化导入，以及原 Schedule EX 事务内核验全部历史关联的 128 条分批计数。未提高锁上限或截断历史；后批失败、取消或非法计数均不发布部分配额。
- 当前尚未接通真实 WorkClaim 提供方、正式 StartSprint/current Sprint 来源、真实 claim 正向链及 Launch/关联链；本轮纯验不证明 `00042` 实际迁移、真实业务 claim、完整遍历/重试循环或 Execution 启动。没有用 SQL 种业务成功，也没有将空 pending/history 当作缺失提供方成功。
- 原 Runtime Source 纯验材料继续原位保留在 `output/ai/agent-system-integration/combined-core-01/`；其结果只代表当时源码范围，不升级为本批 Scheduler 证据。全部既有 output、FAIL、候选及日志未搬动、未删除，不新增历史档案。
- 当前本作者无运行中的 Go、cache writer 或资源窗口；本轮完整原尾已关闭，热 cache 已归还 root 调度。后继运行及 Git 保存由 root 安排；当前仅本页状态更新，产品源码保持冻结。
