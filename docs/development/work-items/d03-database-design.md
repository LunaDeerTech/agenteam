# D03 数据库基础实施规格

- 修订：1；B01 已验收，B02 待开始；基线 `main@c4e0320`。
- 上层：[D03 主规格](d03-postgresql-foundation.md)修订 1；既定接口：[D01 Tx/锁](d01-contracts/foundation.md#tx-与锁顺序)、[部署运行](../../architecture/platform-infrastructure/deployment-runtime.md)、[计划 D03](../development-plan.md#d03-postgresql-与全局迁移)。
- 本模块只拥有连接、数据库健康、事务/锁和迁移元数据；不创建业务事实表、Outbox、Secret、账号或通用事务结果库。独立可验收顺序为 B01 库与真实数据库→B02 入口集成。

## 1. 固定依赖与已知证据

| 项目 | 固定选择 | 证据与兼容范围 |
| --- | --- | --- |
| Go | 1.27.1，`GOTOOLCHAIN=local` | 沿用 D02 真实编译/测试和版本检查；不重复增加会被 tidy 删除的同值 toolchain 指令 |
| pgx | `github.com/jackc/pgx/v5 v5.11.0` | [精确 metadata](https://proxy.golang.org/github.com/jackc/pgx/v5/@v/v5.11.0.info)，2026-09-07；模块最低 Go 1.25.0 |
| Goose | `github.com/pressly/goose/v3 v3.28.0` | [精确 metadata](https://proxy.golang.org/github.com/pressly/goose/v3/@v/v3.28.0.info)，2026-09-02；模块最低 Go 1.26.0；只使用 library/Provider，不导入全驱动 CLI |
| 测试镜像 | `pgvector/pgvector@sha256:99a149d3c84cfb0f32d8da7d72737e4643468787220af2223418730f8e9e9cdc` | linux/amd64；来源标签 `0.8.1-pg17-bookworm`，不以标签/latest 作为最终身份 |
| 反例测试镜像 | `pgvector/pgvector@sha256:16e62164a405447dca191079a924ee5b8a9dbf04fe53128701ffbea857b37782` | linux/amd64，来源标签 `0.8.1-pg16-bookworm`；仅测试真实不支持PG major拒绝，不是生产支持版本 |
| 实际数据库 | PostgreSQL 17.8，`server_version_num=170008`，vector 0.8.1 | 主线程已在无网络、tmpfs、具名临时容器内实际 CREATE EXTENSION、读写 vector(3)、距离运算通过，容器已清除 |

生产版本门禁为 PostgreSQL major=17 且 minor≥8，vector **精确 0.8.1**；这里只实测过 PG 17.8，后续 patch 部署前复跑本模块检查，不宣称其他 patch 已验收。其他 PG major、过旧 patch、不同 extension version 明确拒绝；不自动升级/drop extension。

作者通过 Go proxy 只读核对固定源码 API：pgx `dbTx.Commit`/`pgconn.SafeToRetry`，Goose `Provider.ApplyVersion`/`WithSessionLocker`/`SessionLock(*sql.Conn)`。这些不是实际依赖编译证据；B01 必须以 go.mod/go.sum、真实库和隔离 PG 完成确认，不复制或修改第三方源码。

主线程另以本任务临时容器验证宿主随机 loopback 端口可达、PG SSLRequest 返回 `N`，容器已清理。这只证明网络可用且镜像缺省未启 TLS；B01 的 TLS 正反例须自行生成测试 CA/证书并实际启用服务端 SSL。

## 2. 两块结果与文件所有权

| 卡 | 独占实现/文档范围 | 完成结果 |
| --- | --- | --- |
| B01 | `go.mod/go.sum`；新增 `internal/central/foundation/{tx,cause,lock}.go` 及对应测试；`internal/central/postgres/`；`db/migrations/`（embed 与首个 SQL，替换占位）；`tests/database/`、`tests/testsupport/postgres/`；`scripts/test-postgres.sh`；`docs/development/backend/database.md` | 固定驱动/Goose、真实迁移/修复、opaque Tx/锁、健康与有界关闭；库可独立构建和真实 PG 验收提交 |
| B02 | Central `config/`、`app/`、`cmd/agenteam/`；`internal/platform/logging/`、必要 lifecycle 错误适配；`tests/process/`；`scripts/check-go.sh`；`deploy/central.env.example`；后端 README、根 README、AGENTS 的当前命令/边界说明 | 必需 DB 真实启动、诊断与资源关闭、修复 CLI；完整进程验收后提交 |

B02 在 B01 验收提交后开始；不直接改已验收 postgres/foundation 实现，需要返修先移交。现有 D02 scalar/HTTP 保持其已验收契约；公共 Tx 新文件仅依赖标准库。领域服务引用 foundation.Tx，SQL repository adapter 才依赖 postgres/pgx；Runner 依赖图不增加任何 Central/数据库包。主规格/台账/计划由主线程维护。

## 3. 连接、配置与安全边界

`postgres.Config` 是不可变数据库技术配置，B01 提供纯解析/验证，B02 的 Central loader 将环境映射给它；加载/检查配置不连接数据库。只新增以下 `AGENTEAM_CENTRAL_` 字段：

| 后缀 | 缺省 | 约束 |
| --- | --- | --- |
| `DATABASE_URL` | 必填 | 完整 postgres/postgresql URL，显式单一 host、port、database、user、非空 password；无 query/fragment/Unix socket/multi-host，拒绝 NUL；不输出原值 |
| `DATABASE_TLS_MODE` | `verify-full` | 仅 verify-full/disable；disable 是显式受控部署选择，隔离测试只对 loopback 使用；无 prefer/allow/自动明文 fallback |
| `DATABASE_CA_FILE` | 无 | verify-full 缺省系统根证书；设置时读指定 PEM，空/损坏/不可读失败；disable 时禁止此项；不自动加载用户目录证书/私钥 |
| `DATABASE_MAX_CONNS` | `10` | 2–100；runtime pool；migration 单独最多 1 个连接 |
| `DATABASE_CONNECT_TIMEOUT` | `5s` | 100ms–1m；包含拨号/TLS/认证，不重置外层截止时间 |
| `DATABASE_STARTUP_TIMEOUT` | `2m` | 1s–30m；连接、迁移 guard、全部迁移与首次健康的总预算 |
| `DATABASE_LOCK_TIMEOUT` | `5s` | 100ms–1m；事务 SET LOCAL lock_timeout，不能代替 caller context |

存在但为空无效，未知自有字段继续拒绝。当前只用一个专属数据库账号；启动迁移要求该账号具备本库所需 DDL 与 vector 安装权限，权限不足直接失败，不提权、不以超级用户作为代码默认。已预装同版本 vector 可复用；不同运行/迁移角色的生产部署细化留 D28，不能因此削弱当前连接门禁。

避免 pgx 自动读取部署未选择的来源：拒绝 pgx 支持的非空 `PGHOST/PGPORT/PGDATABASE/PGUSER/PGPASSWORD/PGPASSFILE/PGSERVICE/PGSERVICEFILE/PGSSL*/PGOPTIONS/PGAPPNAME/PGCONNECT_TIMEOUT/PGTARGETSESSIONATTRS/PGTZ/PGMINPROTOCOLVERSION/PGMAXPROTOCOLVERSION/PGCHANNELBINDING/PGREQUIREAUTH` 环境项，错误只报固定 `PG*` 分类；不临时修改全进程环境。

URL 先由自有 allowlist 解析，不把任意 query 透传给 pgx。以完整受控参数调用 pgx ParseConfig（关闭其 TLS 自动发现，password 显式给定）；随后设置自有 TLSConfig、清空 Fallbacks、固定 application_name/timezone/search_path，禁止默认 pgpass/service/client key 内容加载。verify-full 使用 ServerName、可信 CA 与 TLS≥1.2，不能设置 InsecureSkipVerify；需真实证书正反例验证该路径。

Config、错误和嵌套容器的 fmt/JSON/slog 输出均只含安全字段，数据库密码不能因私有字段递归格式化泄露。pgx/Goose 原始 error、SQL、参数、DSN、PG Message/Detail/Hint、证书路径不进入日志/HTTP；允许稳定内部 code、合法五位 SQLSTATE、attempt_id、migration version、server version、耗时。Goose 默认/verbose 日志关闭或经过安全适配，不直接转发格式化文本。

## 4. 公共 Tx 与 adapter 端口

按 D01 新增下列公共类型，Cause 的字段复用 D01 的 commands/job/delivery/recovery 判别结构，commands 包含 primary/related；业务 key/cause 只作查询关联，不整对象记录日志。

```text
foundation.Tx                    // 可比较的不透明 token；无 pgx、Commit/Rollback/Conn 方法
foundation.TransactionCause     // 校验判别与稳定标量身份；不包含 Actor/领域实体
foundation.CommitResult = committed
  | not_committed {fault}
  | unknown {transaction_attempt_id, cause}
postgres.Open(ctx, Config) -> (*Store, error)
Store.WithinTx(ctx, cause, fn(ctx, foundation.Tx) error) -> CommitResult
Store.WithinTxOptions(ctx, cause, {isolation: read_committed|serializable}, fn) -> CommitResult
Store.Acquire(ctx, tx, LockKey, shared|exclusive) -> error
Store.AcquireAll(ctx, tx, []LockRequest) -> error
Store.InTx(tx) -> (SQLExecutor, error)  // 仅 SQL adapter 使用，检查 active/owner
Store.Check(ctx) -> (DatabaseHealth, error)
Store.StopAdmission(); Store.Drain(ctx) -> error; Store.ForceClose(ctx) -> error
Migrator.Migrate(ctx) -> MigrationState
Migrator.Repair(ctx, version, expected_checksum) -> RepairResult
```

foundation 可提供 opaque token 构造器；Store 的私有活动表将 token 绑定本 Store、本次 attempt 和真实 pgx.Tx，未登记/跨 Store/已结束 token 均拒绝，不把 token 当权限。InTx 只暴露 adapter 的 Exec/Query/QueryRow 子集，不暴露 raw transaction、提交、回滚或嵌套 Begin；领域 contract 不接触 pgx 类型。SQL adapter 不允许自行执行 BEGIN/COMMIT/ROLLBACK/savepoint 或外部 I/O。

WithinTx 默认 Read Committed；isolation 只有显式 options 才改变，禁止同 context 重入/嵌套 WithinTx。callback 恰调用一次，不自动 retry。一次新 attempt 有新 UUIDv7 attempt_id，相同命令重试仍保留其 cause 语义；不建立“万能查询 attempt 提交结果”的表。

句柄只在 callback 生命周期有效；同连接并发使用、遗漏关闭 Rows、callback 结束后继续使用均拒绝/使本事务失败。adapter 记录 poisoned 状态，不能因 caller 忽略 Acquire/使用错误就继续提交。callback panic 先用独立有界 context 尝试 rollback/释放，再原样传播给既有安全 recover；不记录 panic 值。

| 观察 | CommitResult 与处置 |
| --- | --- |
| Begin/业务校验/SQL/callback 失败，尚未调用 COMMIT；提交前 ctx 已取消 | not_committed；有界 rollback，无法 rollback 则丢弃连接；不能继续复用 aborted session |
| Commit 收到正常成功 | committed，即使随后 caller 取消或响应丢失；callback 的成功本身不算此证据 |
| `pgx.ErrTxCommitRollback` | not_committed；清理/丢弃该连接，不在内部重跑 callback |
| COMMIT 收到明确 ERROR 回滚证据，例如 40001/40P01/延迟约束 23503/23505/23P01，并确认协议回到 idle | not_committed；可由拥有者按原 command 重新读事实后重试完整 Tx |
| Commit 已尝试，网络断开/超时、FATAL/连接丢失或其他不能证明回滚的错误 | unknown，保留原 cause/attempt_id；不能以 context cancellation、ErrTxClosed 或连接被关推断回滚 |

固定pgx v5.11.0真实代理验证发现：COMMIT已发且服务端已提交时，响应EOF可被驱动转换为带SafeToRetry=true的connLockError。因此COMMIT一旦调用，SafeToRetry单独不构成未发送证明；所有传输/closed/connLockError都保守归unknown，不以字节计数或QueryTracer重建提交发送事实。COMMIT调用前明确取消仍按前述not_committed处理。

未知提交的调用者只查自己的 canonical 幂等/版本/processed fact；没有确认就保持 unknown。SQLSTATE 40003/08007 不得当确定回滚。服务端 23505 等领域约束的具体冲突码由对应 repository 映射；D03 只提供安全分类，不猜测 Task/Project 含义。

## 5. 有序 PostgreSQL 锁

LockKey 构造器固定 D01 rank/kind/identity 规则，不能由调用者随意填排序等级。调用者先预收集完整资源集合，再调用 AcquireAll：在获取首把锁前按 D01 层序、同层 key/aggregate kind+ID 排序，重复 key 合并为最强模式，所有必需锁逐一真正取得后才返回。

Acquire 使用事务级 `pg_advisory_xact_lock` / `_shared`；键由 `SHA-256("agenteam.lock.v1\0" + canonical LockKey)` 的前 8 字节按 big-endian int64 固定映射，所有调用方共享算法，排序按逻辑 key 而不是 hash。hash 碰撞只会增加竞争，不允许跳锁；PostgreSQL deadlock 检测仍是最后防线。

- rank 0 为业务 Command identity 专用命名空间；1–6 严格复用 D01，不以进程 mutex 代替 DB 锁。
- 已持有相同或更强模式的同 key 可幂等返回；**shared→exclusive 升级一律 LOCK_UPGRADE_FORBIDDEN，poison 并回滚**，必须预先选择 exclusive 或重新开始事务，避免两个 shared 同时升级死锁。
- 新申请 key 低于已持最高 key 为 LOCK_ORDER_VIOLATION，poison 并回滚；adapter 不自动释放重排、不默默忽略、不在持后序锁时补前序锁。资源集合变动由外层退出 Tx 重收集。
- Acquire 的 timeout/cancel/SQL error 返回并 poison；锁随真实 commit/rollback/session 终止释放。40001/40P01 不等于自动重复业务操作。
- migration guard 使用另一 PostgreSQL 双 int32 advisory namespace，固定 `(0x4147544d, 1)`；与上述 bigint 锁空间分开，只限同数据库，不用 migration guard 取代运行事务锁。

## 6. 全局 SQL 迁移与持久中断证据

`db/migrations/embed.go` 内嵌同目录 `00001_*.sql` 等全局递增序列；只增新版本，不改已发布 SQL/编号/文件名。启动扫描拒绝重号、缺号、非规范文件名、未声明模式；每个文件全字节 SHA-256 进入内存 manifest。Goose 使用 Postgres dialect、禁止 Go global registry/out-of-order/disable-versioning，默认每文件事务。

首个正式 SQL 只安装/核验 vector 0.8.1、建立 `agenteam_meta.health_probe(probe_id uuid primary key, value bigint not null)`；健康操作插入/读取后 rollback，不留业务数据。连接先检查 PG 版本与 available extension；已装错误版本、缺少目标扩展或权限不足均失败。

migrator 在 guard 下幂等 bootstrap `agenteam_meta`、固定格式的迁移 journal；Goose canonical 版本表为 `agenteam_meta.goose_db_version`。bootstrap 是此基础设施的元数据初始化，不生成领域表；格式不匹配拒绝，未来元数据变更也需明确迁移，不能静默 ALTER。

```text
agenteam_meta.migration_journal
  version bigint PK (>0), filename text, checksum text,
  mode tx|non_tx, state pending|running|applied|needs_repair|repairing,
  attempt_id uuid, started_at timestamptz, finished_at timestamptz?, safe_code text?
```

Goose version 表是 SQL 已应用的事实；journal 只保存不可变来源指纹及执行/修复进度，两者不一致必须按下面规则解释，不能创建第二套独立 schema version。无自动 TTL、无生产 down/reset/force-version 命令。

采用 Goose `Provider.ApplyVersion(ctx, version, true)` 按版本循环，而非直接 `Up/HasPending`（该版本的预检查可能在 session guard 外初始化版本表）。每次 ApplyVersion 使用带当前 manifest entry 的自有 SessionLocker，在 **Goose 实际执行 SQL 的同一个 `*sql.Conn`** 上完成：

1. 有界取得 migration session advisory guard；验证/创建元数据，重读完整 Goose applied 集与 journal，检查它是当前 manifest 的连续前缀。DB 比二进制新、旧文件 checksum/name 改动、缺历史指纹或版本洞均拒绝。
2. 若目标已按同指纹应用，返回经验证的已有结果；否则必须是下一版本。先 autocommit 写 running/attempt/checksum，再让 Goose执行该文件；不能先执行 SQL 后补中断标记。
3. 常规 SQL 与 Goose 版本写入处于同一 DB Tx；`NO TRANSACTION` 则每个 SQL 和最后版本写入不原子。SQL/guard 必须共用上述连接；不在另一会话持锁后让执行会话独立存活。
4. 完成后在 guard 内读取 Goose fact并更新 journal；解锁使用独立最多 2s context。SessionLock 中途失败也负责解锁/丢弃；解锁失败丢弃物理连接，不把带 session lock 的连接放回池。
5. 最后一次带 guard 的整体验证确认目标版本/指纹/extension 全部正确才返回 migrated。并发启动每个版本都重新检查，可观察他人已应用结果；不能重复执行同文件或依赖首次扫描。

迁移使用独立 `database/sql` + pgx stdlib 连接（max open=1），SQL-only Provider；runtime 使用 pgxpool，不让 migration 吃光业务池。Goose cleanup 的 WithoutCancel 不能变成无限等待，adapter 自己限时；所有退出分支释放本次资源。

## 7. 中断、失败与可执行修复

每次新持有 guard 时，先按持久事实恢复，不凭上次进程日志判断成功：

| 残留事实 | 允许的恢复 |
| --- | --- |
| Goose 已应用 + journal running/repairing，来源指纹一致 | SQL 与版本完成已有证据，收敛 journal 为 applied；不再执行 SQL |
| tx migration 未在 Goose 应用、journal running | 旧会话 guard 已释放且无版本事实，整文件/版本同事务未提交；按原指纹转回 pending 后允许重新执行 |
| non_tx 未在 Goose 应用，journal running/needs_repair/repairing | 可能已部分产生 DDL；启动返回 MIGRATION_REPAIR_REQUIRED，不自动重跑/标成功 |
| journal applied 却 Goose 缺失、指纹不符或后续版本先应用 | MIGRATION_HISTORY_DIVERGED，拒绝自动修复/启动；保留数据库供明确恢复，不伪造 rollback |

任何 non_tx 文件必须同时登记编译内置的 recovery plan：有序 `restore_steps[]`（每步一个独立 SQL、可重复执行）和只读 `verify_restored.sql`（恰一 bool，true 表示恢复到重新运行该迁移的前置条件）。没有计划就拒绝该文件，不能以 `IF NOT EXISTS` 假装部分执行安全。只有需要 non_tx 的后续版本才增加正式 repair 文件；D03 的真实验证通过独立测试 migration FS 提供。

B01 提供 `Repair`，B02 接 `agenteam --repair-migration <version> --expected-checksum <sha256:...>`：只接受已编译版本/精确指纹，不接受 SQL/文件路径；不开放 HTTP 修复或 generic force-version。

具体流程是停止该部署的 Central→保留备份/失败事实→执行上述命令。命令获取同会话 guard，确认 non_tx、Goose 未应用、无后续版本且指纹一致，持久 state=repairing；逐步执行已登记的幂等 restore SQL，再执行 verifier。全部确认后原子设 state=pending 并返回 `repaired_to_pending`，随后正常启动重新由 Goose 执行；pending 明确表示可开始新 attempt。修复中断继续保留 repairing，重复同命令从首步幂等重做；失败不擦除标记。

可执行测试样例：fixture v2 的 `CREATE INDEX CONCURRENTLY` 在真实表上被取消，留下无效/已有 index；restore 为 `DROP INDEX CONCURRENTLY IF EXISTS <固定fixture_index>`，verifier 为 `to_regclass(...) IS NULL`。修复后重新迁移，核验 index有效、Goose 只记一次。不能仅删除 journal/手工插 Goose version；没有可安全恢复计划的迁移只允许经单独明确的版本恢复方案处理。

当前第一版正式迁移是事务型，故生产 repair 对其他版本明确 unsupported；该拒绝与测试中真实非事务修复分开记录，不创建成功空实现。迁移引发的 commit unknown 用 Goose+journal事实恢复，不套用业务通用结果存储。

## 8. 健康、入口与关闭

`Store.Check` 在有界 ctx 下核验 server version、vector版本/真实向量表达式、Goose目标版本/无dirty，以及真实读写：Tx中写 health_probe随机ID/值→读回→rollback。返回安全 DatabaseHealth{postgresql,pgvector,migrations,read_write,checked_at,server_version,extension_version}；没有 `SELECT 1` 冒充 write/schema ready，也不把诊断写入提交为历史数据。

B02 启动在 D02 HTTP bind 之前执行数据库配置→带总预算连接→迁移→首次 Check。DATABASE_URL 缺失为配置 exit 2；连接、版本、扩展、迁移或 read/write 失败为初始化 exit 1，不继续伪装 DB unbound 的旧诊断模式。`--help/--version` 不读配置；`--check-config` 验证 D02+D03 参数、可读 CA 格式但不连接，输出 `scope=d03, valid=true, ready=false`。

数据库通过后仍只提供诊断：`/diagnostics` 的 PostgreSQL/pgvector/migrations/read_write 来自真实状态，其余 Secret/object/identity/protocol 仍 unbound；整体 ready=false。`/readyz` DB 故障为 DEPENDENCY_UNAVAILABLE，其余情况为 DEPENDENCY_UNBOUND。liveness 不随 DB短暂故障失败，不新增业务 API。

一个可停止 worker 每 10s 做 Check（单次 2s，不重叠）；超过 20s 无有效采样即 unavailable，不能永久保留启动时 ready。HTTP只读并发安全快照，不在每次请求做写探针；测试注入 clock/interval 而不增加生产测试参数。故障恢复重验后可恢复数据库子状态，但仍不把未实现依赖标 ready。

保持 D02 的首次信号排空：先关 HTTP接入/停止健康领取，在途 handler 继续使用 DB；HTTP完成后停止 Store 新借用，按同一剩余 drain 预算等已有事务/Rows结束，再关池。不能先 pool.Close 导致在途事务失败，也不能直接把无期限的 pgxpool.Close 放入现有同步 Closers，卡死第二信号/超时。

Store拥有所有 checkout 与实际 socket：普通借用/Tx/Rows全部登记并受同一 admission gate 管理，不向调用者暴露 pool。force 时取消其操作 context，向已登记owned连接发送有界 PostgreSQL CancelRequest，再关闭自己 DialFunc 登记的 net.Conn，促使阻塞 I/O返回；取消握手、socket关闭与pool.Close/join共用 D02 最多额外1s总预算，不能按连接重复增加预算。只关闭TCP不保证服务端阻塞语句立即察觉EOF，因此须实测owned backend/query退出，不按PID遍历终止其他连接。commit在此时丢响应仍是unknown；不合作callback不能无限阻止进程退出。正常路径不得留下连接/worker；force关闭不宣称事务已rollback。

启动中首信号取消连接/迁移等待并清已得资源；正在提交的迁移按 journal留下可恢复事实，不记录 migration completed。Runner配置/生命周期不改；日志只增加有限技术状态/错误白名单，中立包不导入数据库或业务实现。

## 9. 隔离验收与命令

`scripts/test-postgres.sh` 独占本次具名/label容器、私有测试网络、tmpfs/临时目录和随机 `127.0.0.1` published port，使用本文件固定镜像digest。生成一次性测试账号/口令和0600 fixture描述（container ID、nonce、port、digest、CA等），不打印口令/DSN，不读取外部DSN/.env/pgpass。

Go测试在连接前核对 Docker inspect 的本次label/nonce/digest/端口与fixture，拒绝缺失或不匹配，不让任意环境DSN指向现有库。每测试独立具名database；只终止已核对该fixture/database的backend PID，只清理记录的本次资源ID。trap处理失败/信号；不使用prune、通配delete或“清空现有库”。测试超时与错误仍需清理、核验无残留。

普通 `go test ./...` 不需要Docker；真实DB测试以 `integration` build tag显式运行，无fixture时失败，不 skip 假通过。B02将原Central成功启动进程测试放入实际DB路径；纯CLI、Runner和测试注入的HTTP drain仍可独立运行，生产装配不得使用测试替身。

| 场景 / 卡 | 必须验证的真实结果 |
| --- | --- |
| D01 / B01 | 空库、重复迁移、fixture旧版本升级；相同SQL不重复执行；向量与读写真实可用，探针无残留 |
| D02 / B01 | 两迁移进程争同guard；锁等待可cancel；持锁进程kill后后者恢复；DDL与版本没有交叉/重复 |
| D03 / B01 | 错误事务SQL/权限不足全部回滚；checksum改动、DB较新/版本洞、错PG/extension拒绝；原始SQL/密码sentinel不泄露 |
| D04 / B01 | 非事务部分执行/连接中断及修复过程再次中断；needs_repair/repairing持久，具名Repair恢复后才可重跑，无盲目force-version |
| D05 / B01 | Tx成功、callback拒绝/panic、提交前cancel、Serializable冲突/死锁；无自动重跑，token跨Store/过期/并发和未关Rows拒绝 |
| D06 / B01 | 共享兼容/排他互斥；反序与两shared升级均拒绝并rollback；行变更/锁随commit、rollback、断连释放 |
| D07 / B01 | 真实TCP故障代理在COMMIT后截住PG的CommandComplete/ReadyForQuery并断客户端；adapter返回unknown，另一连接证实数据已提交；提交前明确取消则not_committed且无数据 |
| D08 / B01 | TLS verify-full成功、错CA/主机名拒绝、不降级明文；PG环境/文件自动来源和未知URL参数拒绝；密码/PG Detail/Goose error不出日志 |
| D09 / B02 | 实际Central配置缺失、不可达DB、迁移失败退出；有效库诊断已绑定而整体非ready；故障/恢复/采样超时不显示旧健康 |
| D10 / B02 | SIGINT/SIGTERM、启动中断、HTTP+Tx在途正常drain、悬挂查询/第二信号force有界退出；owned连接清理，Runner保持原语义 |

D07代理只使用测试loopback无TLS链路解析明确COMMIT与服务端完成帧，用barrier控制转发和截断；不能仅用fake Commit错误充当提交未知验收。另测COMMIT已发但代理未转发就断开，实际未提交仍须报告unknown，证明分类依据是调用者可知事实而非测试内幕。

```sh
AGENTEAM_GO=/workspace/toolchains/go1.27.1/bin/go
GOTOOLCHAIN=local "$AGENTEAM_GO" env GOVERSION GOOS GOARCH
GOTOOLCHAIN=local "$AGENTEAM_GO" test ./...
GOTOOLCHAIN=local "$AGENTEAM_GO" vet ./...
GOTOOLCHAIN=local "$AGENTEAM_GO" test -race ./...
AGENTEAM_GO="$AGENTEAM_GO" sh scripts/test-postgres.sh
AGENTEAM_GO="$AGENTEAM_GO" sh scripts/build-go.sh
GOTOOLCHAIN=local "$AGENTEAM_GO" list -deps ./cmd/agenteam-runner
git diff --check
```

test-postgres.sh 在已核对fixture生命周期内实际执行 `go test -tags=integration -race -count=1 ./internal/central/postgres/... ./tests/database/...`；B02再加入 `./tests/process/...`。子命令exit/输出必须传播，整个脚本不因cleanup成功掩盖测试失败。B01/B02各记精确依赖、镜像/实际版本、命令、冻结指纹与未验证范围后交独立验证；D03通过才进入D04，不把镜像探针或静态API阅读当作本模块验收。
