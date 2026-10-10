# Secret Owner 集成测试：独立方法审查

输入为 `/workspace/agenteam-secret-variable-owner-service` 的 `26db9680`：`tests/projectvariable/secret_{fixture,persistence,authority,atomicity,concurrency,recovery}_test.go`，以及作者随后冻结的 atomicity/recovery 两个限定返修。其余四源逐字保持；`5e2fd7` 实际限定 diff-check/比较成功。本次只读，未编译或运行 Go/PG/socket，未审新 migration 测试或 00030，不继承作者编译/list 为业务通过。

发现一个测试方法缺口并由作者修复：Account Activity 有 60s 更新节流，原新 Session 下“调用成功后故障＋外部时间相等”可能根本没有 Activity 写入。两个末阶段现在先设置原 Session 合法的旧 issued/last-activity（恰一行），同原活 Tx 核活动时间确实大于原基线，才递增 reached 并返回原精确故障；外部仍要求时间逐字回滚。该发现来自实际 SQL 与测试源码对照，没有声称 PG 复现。返修后无剩余确认的 must-fix，有限接受为后续编译及真实场景执行的方法输入。

- fixture 的 Account/Project、D10 facts/write authority、D04、Audit、Outbox、Activity 绑定同一个真实 Store；proxy 场景也重建这些绑定。Skills 初始化和 Process 属明确受控上游，没有 F1/dispatcher/defaultroot 接入证明。
- AtomicFacts 在真实命名端口成功后或完整业务 callback 尾注入，先读取原 Tx 的两域后像、history、D04 receipt、native/D10 Audit、completed/event，按阶段检查；错误须为原 cause、NotCommitted、一次目标回调，最终持久快照、Activity 和 Lookup 同核回滚。nonce 合法预留不在业务快照内。
- Concurrency 由实际规范化锁序选择两原意图首交集，观察原物理 PID、本 DB、精确 advisory key/mode 及 blocker。原 leader 已到 completed callback 尾，释放后收两原结果；清理先 release/cancel，再等待原 goroutine 实际返回。没有靠睡眠推断到达，也未用受控 CommitResult 替代真实提交。
- CommitRecovery 只在同活 Tx 完整 canonical/history、两域 receipt、实际 AuditID 和 Outbox 齐全后给原 backend arm。复用原完整 PostgreSQL 帧代理，以 K 确认 backend、截原 Q/COMMIT，after-forward 等原 C/COMMIT 与 Z/idle；原 Store 自己产生 Unknown，原 cause/attempt 保留。确认使用新的实际 PID并观测第一真实锁阻塞，后继 lookup/replay 校验持久结果与去重；代理原连接 goroutine 必须实际 join。
- Stop 格先拒原 writer 已返回，再调用 Stop，随后收原结果并 Drain。该 PG 格不证明“已取消但 callback 仍 held 时 Drain 必失败”；这一负向及完整原意图/锁/一次 3s 由未变的 `secret_confirmation_test.go` 作者 `89554→59616a` controlled 纯控覆盖，本人仅核源码并复用其有限结果，未重跑。物理锁测试直接观测首锁，不冒每把锁均已独立 PG 观测。
- Persistence/Authority 使用真实服务读写及当前权限，明确直接 SQL 构造归档/Owner 变更的 fixture 边界；历史 lookup 涵盖 create/no-op/delete，删除后的原意图 replay 直接覆盖 create，不扩称所有变体。canary 扫描只证明所列输出与表中所列表示未泄漏。

真实阶段仍需分别证明适用的业务返回、SQL 约束、竞争、物理 COMMIT 与原资源完整尾。前三组旧 compile/list 不覆盖本次返修；concurrency/recovery 尚未编译。上述有限方法结论不替代生产全库独验、SQL30 迁移或任何实际验收。
