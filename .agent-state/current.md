# TaskDispatch 当前检查点

- 物理树：`/workspace/agenteam-skill-install-runtime-source`；当前分支 `ai/task-dispatch`。目录旧名用于保留原 Runtime Source 纯验材料的绝对路径，不代表当前写域。
- 最新源码 `75fb14c5` 已远端保存：Scheduler Busy 9 个源码路径及 Work 消费合同，组合为 AgentSystem `232c7af5`。新增同 attempt 的真实 AgentBusy 标记、BusyCompensator 与私有原事务证明；Work Apply/Check 成功后才同事务 CAS 为 skipped，paused 保留 pending，Unknown 只按原身份 Lookup，Stop 等原调用实际返回。secret 有限独立源码审接受，无确认 must-fix；本片尚未运行 Go 或 PG，不能据此称实际补偿通过。
- 强前置是 Work `92a3` 中的 `db/migrations/00046_task_busy_compensation.sql` 及真实 Work 补偿提供方；不得只部署新增扫描/UPDATE 而缺少 busy_attempt、skip_reason、skipped_at 三列及对应约束。后继限定 Scheduler 新 6 top / 32 sub 加实际受影响旧 Launch 2 top / 7 sub，组合测试由 root 安排，未受影响的旧通过证据复用。
- 前片源码检查点 `07dea4a9` 保存 Scheduler Launch 四源及 Work `task_launch` 合同，组合输入为 AgentSystem `281fc3cb`，两者现均已由 root 成功推送。有限交接已实现首次发送标记已知提交后同步 Launch、持久关联与原键 Lookup；Unknown 不重发，明确未创建仍保留 pending/known_not_created；该前片未实现 Busy 补偿、自动重试或完整 Dispatcher。此前 HTTP 401 推送失败保留为历史事实，重启后的登录恢复已解除该推送阻塞。
- 新增 Work/Scheduler 8 top race 与 3 包 vet 已 whole PASS，原尾关闭；材料原位于 AgentSystem `output/ai/task-launch/combined-pure-01/`。真实 PG `TestSchedulerLaunch` 1 top / 2 sub 业务 24.45s PASS，但整轮 whole FAIL：Go/driver Wait 0、supervisor/outer Wait 1，host TCP delta=1 且未保存该 tuple，无法归因；其余 owned 资源、进程及输入尾门齐。原件在 AgentSystem `output/ai/agent-system-integration/scheduler-launch-01-control/`，不得将业务通过升级为整体通过。
- 本批源码基线 `a6ddde3d`：Scheduler PendingAuthority、四态持久模型及迁移 `00042`、单次 claim coordinator、私有同事务证明、配额分批观察；含配套 Execution capacity 与 Agent scheduler-current 提供方。已精确集成 AgentSystem `d041370d`。这不是完整 Scheduler 或生产接入完成。
- 首片 `0434bc76` 的 5 top / 12 sub race 与 Scheduler 包 vet 已实际通过（session `43029` → `762123`）。原结果、日志、runner 和输入前后记录仍在 `output/ai/task-dispatch/pure-01/` 及同层 `pure-01-*` 文件。
- 后片组合 `a6ddde3d` 的 12 top / 42 sub race 与 Scheduler、Execution、Agent 三包 vet 已实际通过（session `78264` → `d9b84b`）。其中 Scheduler 5/17、Execution 4/12、Agent 3/13；race、vet 原 Wait 均为 0，group/runtime 双尾为空，adopted=[]，1263 个源码输入前后相同。准确 argv、结果、日志在 `output/ai/task-dispatch/coordinator-pure-01/`；同层保留 `coordinator-pure-01-launcher.py` 和输入前后记录。旧通过范围未重跑。
- 首片、Scheduler 后片及两处返修已有有限独立源码审接受；修复包含真实 outbox 锁归一化导入，以及原 Schedule EX 事务内核验全部历史关联的 128 条分批计数。未提高锁上限或截断历史；后批失败、取消或非法计数均不发布部分配额。
- 后续组合已接正式 StartSprint、WorkClaim 与 durable pending 同事务链；本批又完成有限 Launch/关联的真实业务调用，整体退出结果仍以上述 FAIL 为准。既有纯验不扩为完整遍历、重试、补偿、Snapshot/capture 或 Execution Started 证据；没有用 SQL 种业务成功，也没有将空 pending/history 当作缺失提供方成功。
- 原 Runtime Source 纯验材料继续原位保留在 `output/ai/agent-system-integration/combined-core-01/`；其结果只代表当时源码范围，不升级为本批 Scheduler 证据。全部既有 output、FAIL、候选及日志未搬动、未删除，不新增历史档案。
- 当前本作者无运行中的 Go、cache writer 或资源窗口；本轮完整原尾已关闭，热 cache 已归还 root 调度。后继运行及 Git 保存由 root 安排；当前仅本页状态更新，产品源码保持冻结。
