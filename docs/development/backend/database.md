# PostgreSQL 基础库

`internal/central/postgres` 提供 D03 B01 的连接、迁移、事务、锁和数据库健康能力。当前 Central 命令尚未装配此库；数据库必需配置、诊断采样、进程关闭顺序和修复 CLI 由 D03 B02 接入。Runner 不依赖 Central 或数据库包。规则与所有权见 [D03 主规格](../work-items/d03-postgresql-foundation.md)、[实施规格](../work-items/d03-database-design.md)和 [D01 Tx/锁契约](../work-items/d01-contracts/foundation.md#tx-与锁顺序)。

## 版本与配置

固定 Go 1.27.1、pgx v5.11.0、Goose v3.28.0；使用 Goose SQL-only Provider，不导入 Goose CLI 或其他数据库驱动。生产门禁为 PostgreSQL 17 且 patch ≥ 8、vector 精确 0.8.1。当前实测版本为 PostgreSQL 17.8；其他 patch 部署前需复跑验证。库不自动升级或删除 extension。

`postgres.LoadConfig(lookup, environment)` 只解析配置，不连接数据库；显式 CA 文件在此时读取并验证。Config 不可变，格式化/JSON/slog 仅输出固定安全标识。`Open` 和迁移建立连接前再次拒绝当前进程的隐式 PG 环境来源。

以下变量均以 `AGENTEAM_CENTRAL_DATABASE_` 开头。变量存在但为空无效，未知自有变量拒绝。

| 后缀 | 默认值 | 约束 |
| --- | --- | --- |
| `URL` | 必填 | 完整 postgres/postgresql URL，显式单 host、port、database、user、非空 password；禁止 query、fragment、Unix socket、multi-host、NUL |
| `TLS_MODE` | `verify-full` | 只允许 verify-full / disable；disable 必须显式选择 |
| `CA_FILE` | 系统根证书 | 可选 PEM；disable 不接受此项 |
| `MAX_CONNS` | `10` | 2–100，运行池上限 |
| `CONNECT_TIMEOUT` | `5s` | 100ms–1m，包含拨号/TLS/认证 |
| `STARTUP_TIMEOUT` | `2m` | 1s–30m，供装配层建立总启动预算，迁移自身也受此上限约束 |
| `LOCK_TIMEOUT` | `5s` | 100ms–1m，每次事务 SET LOCAL；同时服从调用 context |

不自动加载 `.env`、pgpass、service 配置或用户目录客户端证书/私钥。非空 `PGHOST/PGPORT/PGDATABASE/PGUSER/PGPASSWORD/PGPASSFILE/PGSERVICE/PGSERVICEFILE/PGSSL*/PGOPTIONS/PGAPPNAME/PGCONNECT_TIMEOUT/PGTARGETSESSIONATTRS/PGTZ/PGMINPROTOCOLVERSION/PGMAXPROTOCOLVERSION/PGCHANNELBINDING/PGREQUIREAUTH` 均拒绝；错误只投影固定 `PG*` 分类。代码不临时改动全进程环境。

verify-full 使用 TLS ≥ 1.2、URL host 对应的 ServerName 和指定/系统根证书，没有明文 fallback。部署账号须具备本库迁移 DDL 和 vector 安装权限；库不提升账号权限。已经安装的同版本 vector 可以复用。

## Store 与事务

装配层在同一个总启动 context 下调用 `Open`、`NewMigrator(...).Migrate` 和 `Store.Check`，任一步失败都清理已经取得的资源。Open 验证真实连接及版本/extension 可用性；迁移安装 extension 和 schema；Check 确认目标迁移、vector 运算和实际读写。`DatabaseHealth` 的布尔字段来自本次检查，随机健康记录在读回后 rollback，不留探针行。

领域服务只使用 `foundation.Tx`、`TransactionCause` 和 `CommitResult`。SQL repository adapter 才使用 `Store.InTx(tx)` 得到 Exec/Query/QueryRow 子集。没有 raw pgx Tx/Conn、Begin、Commit 或 Rollback 入口。普通查询也通过 Store 执行，以共享 admission、checkout 和 socket 归属。

`WithinTx(ctx, cause, callback)` 默认 Read Committed；`WithinTxOptions` 可显式选择 Serializable。每次 callback 恰执行一次，无内部重试。同一 context 不得嵌套；token 仅在当前 Store 的当前 callback 内有效。跨 Store、过期、并发使用、未关闭 Rows、锁顺序错误等都会拒绝；活动事务被 poison 后，即使 callback 忽略错误并返回 nil，也不能提交。Query 返回的 Rows 必须关闭或读到 EOF；QueryRow 的 Scan 自动关闭。

SQL adapter 不得自行控制事务或执行外部 I/O。SQL 入口对常见事务控制语句做误用检查；它不是不可信 SQL 的沙箱或权限检查器。panic 使用独立有界 context 清理后原样传播，由已有安全 recover 边界处理，不记录 panic 值。

| 提交观察 | 结果 |
| --- | --- |
| COMMIT 调用前 callback/SQL/句柄失败或 context 已取消 | `not_committed`，有界 rollback，失败时丢弃连接 |
| 收到 COMMIT 成功响应 | `committed` |
| `ErrTxCommitRollback`，或明确服务器回滚 ERROR 且协议已回 idle | `not_committed` |
| 已调用 COMMIT 后连接断开、超时、FATAL 或证据不足 | `unknown`，保留 attempt_id 和原 cause |

固定 pgx v5.11.0 在响应 EOF 后可能返回 `connLockError`，即使 COMMIT 已发出、服务端已提交，`pgconn.SafeToRetry` 仍为 true。因此该提示不能把已尝试 COMMIT 的传输错误改为 not_committed。真实 TCP 代理测试分别确认“服务端已提交但响应丢失”和“COMMIT 未转发给服务端”都返回 unknown。调用者只能读取自己负责的 canonical 幂等、版本或 processed fact 确认结果；没有通用 attempt 结果表，也不根据 unknown 自动重放 callback。

`postgres.Error` 只隐式输出固定 code、合法五位 SQLSTATE 和安全字段名。原始原因通过显式 Unwrap 保留判断能力；不得将展开后的错误、PG Message/Detail/Hint、SQL、参数、DSN、证书路径或 TransactionCause 整体写日志。Goose 文本日志关闭。

## 有序锁

使用 foundation 的固定 LockKey 构造器，先预收集完整资源集合，再 `AcquireAll`。实现按 D01 的 rank/kind/identity 排序，重复 key 合并为最强模式；逻辑 key 通过固定 SHA-256 算法映射为 PostgreSQL bigint 事务 advisory lock。shared 兼容、exclusive 互斥；锁随真实 commit、rollback 或 session 终止释放。

已经持有相同/更强模式可以幂等申请；shared→exclusive 升级始终拒绝。新 key 低于已持最高 key、锁 timeout/cancel/SQL 错误均 poison 当前事务。资源集合变化时由外层退出事务后重新收集，不在内部释放重排。

## 迁移与修复

`db/migrations/embed.go` 内嵌一个全局序列。文件名为 `00001_name.sql`，版本从 1 连续递增，每个文件声明 `-- agenteam:transaction tx` 或 `non_tx`，包含 Goose Up，禁止 Down。已发布 SQL、编号与文件名不可变；完整文件的 SHA-256、名称和模式写入 journal。新增 non_tx 必须同时提供编译内置的 RecoveryPlan；生产命令不会接受任意文件路径、SQL 或恢复步骤。

Goose 事实表为 `agenteam_meta.goose_db_version`；`migration_journal` 保存来源指纹及 pending/running/applied/needs_repair/repairing 状态。migrator 使用独立 max-open=1 的 database/sql 连接；Goose 实际执行 SQL 的同一连接持有双 int32 advisory guard `(0x4147544d, 1)`。每个 ApplyVersion 之前都在 guard 下核验完整历史并持久写 running，之后依据 Goose 事实收敛 journal。元数据格式、缺失指纹、checksum/name 改动、版本洞或 DB 比二进制新均拒绝启动。

| 中断事实 | 恢复行为 |
| --- | --- |
| Goose 已应用，journal running/repairing，指纹一致 | 收敛 applied，不再执行 SQL |
| tx 未应用，journal running | guard 新持有者确认旧 session 已退出后转 pending，可重新运行 |
| non_tx 未应用，journal running/needs_repair/repairing | 明确要求修复，不自动重跑 |
| journal applied 但 Goose 缺失、后续先应用或指纹不符 | history diverged，保留事实供明确恢复 |

修复先停止该部署的 Central，保留备份和失败证据，再由部署装配调用 `Migrator.Repair(ctx, version, expectedChecksum)`。只接受已编译 non_tx 版本、精确 checksum、未应用且无后续版本的残留。取得同一 guard 后持久写 repairing，依次执行幂等 restore SQL；只读 verifier 必须恰返回一行、一列、类型为 bool 的 true。全部通过才转 pending，随后正常 Migrate 重新执行。修复失败/中断保留 repairing，重复修复从首步重做。

首个正式迁移仅安装/核验 vector 0.8.1 和 health_probe，是事务型，没有生产 non_tx 恢复计划，因此生产 Repair 明确返回 unsupported。真实 non_tx 验证使用独立测试 FS 的 `CREATE INDEX CONCURRENTLY`、`DROP INDEX CONCURRENTLY` 和 verifier。`--repair-migration` CLI 尚待 B02 接入。

## 资源关闭

StopAdmission 先拒绝新的普通借用/事务；已经进入的事务和 Rows 继续使用其连接。Drain 使用调用者剩余预算等待所有 checkout 结束，再关池，不能在 handler 仍需要数据库时提前调用它。

ForceClose 共用最多 1 秒总预算：取消已经登记的操作，向 owned 连接发出短时 PostgreSQL CancelRequest，关闭自己 DialFunc 登记的 socket，再等待池退出。关闭 TCP 本身不足以立即停止服务端 pg_sleep，因此真实测试同时验证 owned backend 退出。非合作 callback 可以超出逻辑 checkout 生命周期，但不能无限阻塞 ForceClose 返回；装配层仍须按进程关闭规则退出。force 不证明正在提交的事务已回滚。

## 实际验证

```sh
export AGENTEAM_GO=/workspace/toolchains/go1.27.1/bin/go
GOTOOLCHAIN=local "$AGENTEAM_GO" test ./...
GOTOOLCHAIN=local "$AGENTEAM_GO" vet ./...
GOTOOLCHAIN=local "$AGENTEAM_GO" test -race ./...
sh scripts/test-postgres.sh
sh scripts/build-go.sh
```

普通 Go 测试不需要 Docker。数据库测试带 `integration` build tag；没有 fixture 时失败，不 skip。脚本核对精确工具链，创建一次性 nonce/label、专属 bridge 网络、tmpfs 容器、临时 CA/口令和 0600 描述文件，只发布随机 loopback 端口。固定镜像 digest 见实施规格。测试连接前核对容器 ID/name/label/image、network 和端口，每个测试单独建库；cleanup 只处理登记的本次资源 ID，并核验无残留。测试失败/信号仍清理且保留非零退出码。

`tests/database` 覆盖空库/重复/升级、双迁移进程争锁与 kill 恢复、真实非事务中断及修复再次中断、事务 poison/并发 Rows/Serializable/死锁/延迟约束、有序共享/排他锁、提交未知 TCP 代理、TLS CA/hostname/认证、PG 环境来源、权限与错误安全投影、drain/force。PG16 反例使用另一个固定镜像实际运行；vector 0.8.0 反例仅修改隔离 fixture 的 extension catalog，证明版本门禁，不宣称运行了旧 vector 二进制。

驱动 EOF 分类依据可定位到固定 pgx 的 `pgconn.MultiResultReader.NextResult`（忽略 peek 错误）、`peekMessage`（EOF 关闭连接）、`receiveMessage`（返回 closed connLockError）与 `connLockError.SafeToRetry`；提交分类回归以真实协议和另一连接的数据库事实为准。
