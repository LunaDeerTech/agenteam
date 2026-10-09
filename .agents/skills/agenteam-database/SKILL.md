---
name: agenteam-database
description: 在 agenteam 修改 PostgreSQL schema、SQL adapter、事务锁、业务幂等或提交未知恢复时使用，依据正式契约实施前向迁移并验证真实并发和持久事实。
---

# agenteam 数据库与事务

供 `data_worker`、后端实现者及数据库验证者按任务组合使用。以[数据库说明](../../../docs/development/backend/database.md)、[Tx 与锁契约](../../../docs/development/work-items/d01-contracts/foundation.md)及当前工作项为准；权限、所有权和交付遵循[团队流程](../../../docs/development/agent-team/README.md)。本技能不另定义业务状态或迁移规则。

## 触发与产物

适用于 schema/index 变更、SQL repository、跨领域同事务、锁竞争、命令重放和 `CommitResult` 恢复。产物是授权范围内的 SQL、adapter/领域实现、迁移与并发测试，以及实际升级和恢复结论。只审查时输出缺陷与复现，不取得实现文件写权。

## 迁移落地

1. 对照[全局迁移目录](../../../db/migrations/)和当前规格确认编号、唯一写者、已发布基线与目标 schema。已有编号、文件名和 SQL 不改写；出现预留冲突先由负责人协调。
2. 从业务约束推导表、唯一性、引用、版本和查询需求，检查存量数据能否满足新约束。索引服务实际查询；可能的大表扫描、锁持有和回填按任务的真实数据规模评估。
3. 按正式规则编写连续前向迁移、Goose Up 和 `tx`/`non_tx` 声明。需要 `non_tx` 时同时处理已编译 RecoveryPlan 与中断恢复，不能只写 SQL 后假设可安全重跑。
4. 用任务所有的真实 PostgreSQL 验证空库、已有数据升级、重复启动和实际约束；迁移失败核对 journal、Goose 事实及数据状态。涉及中断恢复时按该迁移模式验证，不把新建空库成功当作升级完成。

## 一次命令的事务边界

先列出本次读取的授权事实、修改的领域事实、幂等记录及适用的 Audit/Outbox 写入，标明它们所属的同一个事务。领域层使用 `foundation.Tx`；SQL adapter 从当前 Store 的有效 Tx 取得 executor，关闭 Rows，不自行 Begin/Commit，也不在 SQL callback 中执行外部 I/O。

实现前预收完整锁集合，使用正式 LockKey 构造器和 `AcquireAll`。逐项检查锁覆盖的事实、共享/排他模式与持有期；集合变化按正式规则退出事务后重新收集，不能在持锁途中降序加锁或升级 shared→exclusive。权限重验和命令重放必须使用对应契约要求的当前事实与锁。

对 `committed`、`not_committed`、`unknown` 分别安排输出与恢复。Unknown 保留原 attempt/cause，按所属领域的 canonical 幂等或持久事实查证；传输错误或驱动 SafeToRetry 不能证明未提交，也不能触发 callback 自动重放。领域规则允许哪些查证、返回什么结果，以目标工作项为准。

## 有意义的验证

- 从[真实 PG fixture](../../../tests/testsupport/postgres/)和相邻领域测试复用建库、迁移与连接管理。通过[现有脚本](../../../scripts/test-postgres.sh)取得任务所有的环境，运行前按[后端说明](../../../docs/development/backend/README.md)核工具链、固定 fixture 与 MinIO 前提。
- 并发测试使用两个真实事务、明确的 barrier/锁等待事实和独立观察连接；断言互斥、最终版本/行数和副作用。单纯延时后读取不能证明两操作发生了竞争。
- 提交未知测试区分 COMMIT 未转发和服务端已提交但响应丢失，查另一连接的持久事实与后续 lookup。可参考[Work 并发测试](../../../tests/work/structure_concurrency_test.go)的方法，不能照搬其业务规则。
- 取消、poison、授权撤销或清理与写入竞争只覆盖当前变更涉及的边界；核对事务结果、对外输出和遗留资源。需要 harness 方法时组合[测试工程技能](../agenteam-test-engineering/SKILL.md)。

## 失败与交接

缺真实 fixture、迁移号冲突或正式事务契约不明时，记录具体缺口并暂停受影响操作。普通 Go 测试、编译或静态 SQL 检查不能代替真实 PG 结论；历史失败保留。必要 SQL、harness、脱敏输入随[可恢复检查点](../../../.agent-state/README.md)保存，报告实际版本、场景、结果及自有数据库资源终态。
