# D06/B01 PostgreSQL 取消协调修复规格

- 修订：2；基线 `main@7a54eb7`；属于 [D06 主卡](d06-transactional-outbox.md) 的 B01 兼容门槛，非新模块。沿用 [D06 设计修订 2](d06-transactional-outbox-design.md) 与 [D03 关闭契约](d03-database-design.md)。本文仅确定工程修复，未执行实现或实验。
- 冻结前提：B01 的 35 源码及 313 依赖、数据库 schema/迁移、Go1.27.1/pgx v5.11.0、Tx 提交分类不变；不改 app 的 DB 最后关闭、shared 额外 1s、取消 phase 最多 100ms，亦不放宽原测试。
- 依据：只读诊断 `/tmp/agenteam-d06-force-cancel-readonly-diagnosis.md`，SHA256 `a9bb4ca81cc96e94eb74f7e1483d7a5756691cf4629823d6b6eea93060629b02`；其 evidence manifest SHA256 `4071e3824ac468846836c6f7125f8f9def15f4212417dfe0d30da99312822afc`；独立 V 的 `revalidation-report.md`、`app-failure-observation.json` 位于 `/tmp/agenteam-d06-b01-repair-verify-8enfytwx/`。

## 1. 已证事实与修复目标

原 app blocked_io 在两次无过滤组失败；第二次已观察进程按原预算退出、没有 committed 响应、事务事实表为 0，但 exact owned PostgreSQL backend 仍 active/PgSleep，elapsed=20.249s，无 blocker。没有该 PID 的 CancelRequest 发出/返回及 DB force 入场剩余预算记录，因此不能宣称已证明某一条具体调用分支。

[store.go](../../../internal/central/postgres/store.go) 与固定 pgx 源码证明以下窗口可达：HTTP 先取消 → 默认 watcher 仅到期 data socket → SQL 返回而 asyncClose 的 CancelRequest 尚未完成 → operation.release 先从 map 移除 → ForceClose 漏选该 target 并封闭同 DialFunc，后台取消被拒绝/截断。pgx CancelRequest 没有 IsClosed 早退；移动 op.cancel 的一行顺序不能找回已释放的 target。

目标是让平台负责的取消工作有明确 owner，在移交/释放连接或封闭取消通道前完成有界尝试或明确预算耗尽。本地取消工作 join、socket 关闭、CleanupDone、pool join、CancelRequest 返回 nil，都不证明服务器语句已停止。正常可达 owned PG 的服务端终止仍以真实观测验证；网络不可达或原预算已尽时保留未确认的准确结果。

## 2. 选择与精确实施范围

| 候选 | 结论 |
| --- | --- |
| 独立保留所有物理 PgConn/未知 backend tombstone | 能补 release 后的 Force 快照，但若把远端终局作为唯一退役条件，会使正常用户取消永久消耗 slot；还需新 server 探测/容量恢复机制。对当前 bounded best-effort 契约过大，不选 |
| 直接复制 migration watcher | Unwatch 会 join 有界 CancelRequest，能消除默认 watcher 的主要释放窗口；但 pool 复用、release fallback 与 Force 共享 phase 仍须明确，不能照搬独立 Background 100ms 作为强停后的新预算 |
| **joined watcher + Store 私有取消协调票据** | 采用。复用 [migrate.go](../../../internal/central/postgres/migrate.go) 已有“取消尝试后关闭 data socket”的模式，只跟踪尚有本地 owner 的取消工作，不做服务器终局登记库 |

建议 root 在规格通过后顺序授权一个完整修复结果：

| 文件 | 最小增量 |
| --- | --- |
| `internal/central/postgres/store.go` | 安装自有 pooled watcher；borrow 时在发布连接前绑定稳定 target、锁后重查 admission；release 先处理取消所有权再移除/Release；Force phase 和 socket seal 顺序；DialFunc 封闭前后检查 |
| 新 `internal/central/postgres/pool_cancellation.go` | 私有 watcher、target/票据、一次取消和共享 deadline/join 协调；不导出新业务端口 |
| 新 `internal/central/postgres/pool_cancellation_test.go` | 本地状态机/race、共享 phase、正常复用与取消后不复用的单元边界；不启动 Docker |
| 新 `tests/database/cancellation_force_test.go` | public Store + owned PG/协议 proxy 的确定性红回归和真实终止、预算、复用、并发验证；需要时同目录增加专用 `_test.go` proxy helper |

`migrate.go` 只作先例，不借修复重构；`sql.go`、`transaction.go`、app、原 `database_process_test.go` 和原 lock/force/unknown 测试保持字节不变。全部调用仍经现有 borrow/release，故无需修改各 SQL/Tx 调用点。若实现发现必须改这些冻结文件，先报告具体不可组合原因，不扩大改动。

## 3. 私有 target、票据和退役

```text
target = 当前物理 PgConn 的稳定引用 + data net.Conn + 所属 Store
         + owner 引用 + 该连接唯一 cancellation work（可尚未开始）
work: idle -> attempting -> locally_joined
      outcome: attempted_unconfirmed | request_error | budget_exhausted
force phase: not_started -> requesting -> sealed
```

target 在真实 checkout 仍由 Store 拥有时捕获；不能在 pgxpool.Conn.Release 之后再调用它的 Conn/PgConn 或读取可变状态。factory 收到的 PgConn 可供 watcher 使用；固定 pgx 在 BackendKeyData 和 ReadyForQuery 完成后才安装此 factory，不能把未完成认证的 PID/secret 当可取消 target。Store 不导出 target、backend secret、原地址/错误到日志或 HTTP。

borrow 的 conn、target 和 operation 关联在同一 Store.mu 临界区发布。watcher 在 pool 内部 Ping/ValidateConnect 等尚未关联 borrower 的时段触发时，可建临时 owner；其 Unwatch 结束即释放该 owner，不留下未完成连接的永久索引。按物理 PgConn 去重，不能为 watcher、release 和 Force 各建一个独立取消请求。

票据仅覆盖当前 checkout/连接内调用和正在取消的本地工作。一次真正取消后该物理连接必须丢弃，不能重置票据再交下一请求；未发生取消的正常 checkout 释放关联，健康物理连接照常复用。活动 target 数与 pgxpool MaxConns 的物理连接额度一致；纯等 Acquire 的 operation 没有 target，不额外发 goroutine。开始新连接的内部调用也计入该额度，测试核实最大并发。

已开始的 work 必须在 CancelRequest 返回/明确 phase 已关闭、所开 control socket 结束且 data socket 已关闭之后才能标 locally_joined；本地 owner 都解除后删除票据。request_error/budget_exhausted 也可完成这一**取消工作**的退役，不能标 server_stopped。不存在按 TTL 淘汰未知目标，也不把未完成工作的记录丢掉；正常用户取消不永久减少可用连接数。若底层 I/O 未真实 join，记录仍由其 owner 持有，不能伪完成后不断新增替代工作。

## 4. Watcher 与 pool 复用

自有 `BuildContextWatcherHandler` 的 HandleCancel 同步进入该 target 的 run-or-join；由第一次调用执行 PgConn.CancelRequest，其他入口等待同一 done，不另加任意 sleep。每次工作最多 100ms，若已进入 Force 则必须使用 §5 同一个更短 phase 截止时间。普通取消可用独立的有界 cleanup context，因为被观察的 query context 已取消；一旦 Force 开始，现存工作的 deadline 只能缩短。

CancelRequest 结束或预算耗尽后，对已捕获的 data socket SetDeadline(now) 并 Close，即使查询恰好先完成也不再复用该连接。HandleCancel 不在另一个 goroutine 中并发操作 PgConn 的可变状态；只做 CancelRequest 与并发安全的 net.Conn 操作。HandleUnwatchAfterCancel 不清除 deadline、不恢复连接。

必须在 SQL owner 侧把取消过的 PgConn 标为已关闭再交 pool。在 HandleUnwatchAfterCancel 的 owner 调用栈上，对**已经关闭的 data socket**执行 `PgConn.Close(context.Background())`：固定 pgx 此分支不递归 Unwatch，关闭 socket 后的 Terminate 写立即失败，不引入新网络等待；不得换成触发 watcher 的派生 context。未进入 watcher 的 release fallback 在其 owner 调用栈完成同样 discard；覆盖 pool 内部取消，不出现先归池后关闭窗口。Force goroutine 不能并发调用此可变 PgConn.Close。

pgx ContextWatcher.Unwatch 会等待已进入的 HandleCancel 返回；因此已开始真实取消工作的普通查询/Rows 结束不能早于该工作完成。borrow 必须保留调用方传入的**原始 caller context**，与其派生 op.ctx 分开；release 的 fallback 依据原始 caller 取消、独立 force phase/标记、已开始的 target work，不能只看 op.ctx.Err。Rows.closeLocked 的正常 raw.Close → r.cancel → release 已会提前取消 op.ctx；release 自己的 op.cancel 以及 WithinTx 逐 SQL 子 context 的正常 defer cancel，同样都是内部清理，均不单独触发取消请求/discard，不需修改 sql.go/transaction.go。

若原始 caller 已取消而 Unwatch 阻止尚未开始的 callback，release 仍须 run-or-join 同一 target；独立 force 或已有 work 也必须处理/join，不能因忽略内部 op.ctx 取消而漏掉真实工作。driver 已关闭或已发生真实取消的物理连接照常丢弃；只有内部 cleanup cancel、无上述来源的正常结束应保留健康连接供后续复用。

release 保持 operation/target 注册，直到上述有界 work join 和 owner discard 完成，随后才从 operations 移除并执行 pool Release。重复 release/Force/watch cancel 不重复发同一平台取消工作。底层 pgx 自身的 asyncClose 可能额外尝试取消；它不承担平台正确性证明，不作为新预算来源，也不允许使已丢弃的 data connection 回到池中。

## 5. Force phase、锁序和实际预算

ForceClose 保留现有调用者 parent 和最多 1s 本地等待上限。首次 force 在 Store.mu 下原子停止新借用、发布 requesting phase 的绝对截止时间 `min(原 parent deadline, force 入场+100ms)`，固定已绑定 target 集合，然后在锁外取消 operation contexts/run-or-join target；不能先发 op.cancel 再公布 phase。

已有普通取消工作以自己的原截止和上述 phase 截止取更早者，借助取消函数使已在 dial/read 的 control I/O 同时收紧。后到 watcher/release 只加入该 phase 的剩余时间；重复 ForceClose 不重开 100ms/1s，任何更短 parent 只缩短等待/phase。同一次 force 的 N 个连接并发共享 100ms，不是 N×100ms；work 结果记录在其内部 cleanup cancel 之前读取的真实 context 状态。

Store.mu 只保护 admission、target owner、work 选择和 phase，不能持锁执行 CancelRequest、socket.Close、PgConn.Close、pool.Close 或等待 done。socketSet 的 mutex 只保护 socket 集合；不与 Store.mu 反向嵌套。一次启动 work、持有稳定 target、结果/done 发布必须线性化；不能在 WaitGroup 等待开始后无约束新增工作，用固定准入或锁下计数/changed 通知关闭 phase。

所有已准入平台取消工作完成，或同一 phase deadline 到达，才 seal DialFunc 并关 owned sockets；到期先取消对应 control contexts，真实 socket 关闭令 I/O 返回。DialFunc 在实际拨号前检查 sealed，拨号返回后再检查并关闭竞态新 socket。force 已开始时 Acquire 的后台连接可能晚返回；borrow 在同 gate 锁后拒绝发布，关闭本次真实连接，不能交给业务。sealed 后的新 watcher 只作预算耗尽的本地关闭，不开新控制连接。

之后调用现有 pool.Close/join，仍在同一个原 force context 内；不让额外 cleanup、driver 自有 15s asyncClose 或不合作 callback延长 API 返回。若已有工作因不合作 I/O 未 join，返回已有稳定失败类型并保留其 owner，不伪 drained。正常 Drain 沿原预算停止借用并等待 owners；不能因为 operations 提前被移除就封闭尚有取消工作的 socket registry。

parent 已取消或预算本来就为 0 时，实际执行停止准入/关闭 owned sockets/启动 pool 关闭，但不能保证成功交换 CancelRequest。deadline/canceled 分支用既有 DrainTimeout；明确的取消连接错误用既有 ConnectionFailed，公共 SQL/Tx 原错误与 CommitState 分类不变。ForceClose 的 nil 至多表示本地取消尝试未报已知失败且本地关闭/join 完成，不能解释为服务端语句终止证明；CancelRequest nil 但请求 context 已到期仍记 budget_exhausted。

app 的共享 1s 可能在调用 DB ForceClose 前已被其他资源耗尽。这一事实不能靠重设超时弥补，也不能因此跳过 DB 实际关闭调用。COMMIT 已开始后丢响应仍按原规则 unknown；取消、net.Conn.Close、CleanupDone 都不是 rollback 证据。schema、SQLExecutor、Tx token 和 ordered locks 不改变。

## 6. 确定性红测试与验收门槛

先在**冻结旧实现的独立副本**跑新增红 probe，记录输入 SHA、真实 PgSleep、取消连接 barrier、owner 返回、control socket/registry 关闭和 force 入场剩余预算；失败必须证明具体协调条件，不能只靠增大负载碰概率。再以同一 probe/断言跑修复版。

建议沿已有 [commit proxy](../../../tests/database/commit_proxy_test.go) 的 owned loopback 协议 fixture，保持真实 Store/pgxpool/PG。先取得 exact backend 身份、观察 PgSleep，再取消普通 query；proxy 只扣住第一条 CancelRequest 的转发/SSL negotiation，不记录取消密钥。控制通道被扣留时观察 public owner 返回和该连接实际 EOF/完成信号：旧实现可先释放 borrower，而该取消连接还活着、尚未转发；修复后的 owner 返回必须等同一取消工作的正常结束或 100ms 耗尽后真实关闭。

此 red/green 的不变量是**本地工作 join 先于 owner release**，不是“必须有第二次 CancelRequest”。可用 proxy 的独立有界观察确认 control socket 是否仍活，辅以包内协调器的通道 barrier 验证本地顺序；不用任意 sleep 当服务器事实。若故意扣留到超过 budget，这个分支只验错误/本地 join，不能要求服务器停止，也不能把 nil CancelRequest 当成功。与之分开的正常转发场景才要求 exact backend 结束。

| 场景 | 必须观察到的结果 |
| --- | --- |
| C01 owner 释放窗口红回归 | 旧版在首取消尚未完成时返回/移除 owner；修复版同一 work 有界终局后才释放。normal/timeout 均实际 join proxy/task，无成功 stub 或固定 sleep |
| C02 正常可达服务端 | 真实 pg_sleep(60) 先进入 PgSleep，取消控制请求正常放行；Exec/WithinTx/Rows 均结束，exact owned backend/query 在原观察门槛内消失，其他 backend 不受影响 |
| C03 正常完成与取消竞争 | 普通 Exec、Query 正常 EOF/显式 Rows.Close、QueryRow.Scan，以及 WithinTx 逐 SQL/Tx Rows 的 EOF/Close/Scan 和事务正常收束后，持续复用同一健康连接且无额外 CancelRequest；内部 cleanup cancel 不误触发 discard。真正 caller/force/已有 work 取消及完成竞争仍不把已取消物理连接交下一请求，迟到 Cancel 不击中新请求；连续超过 MaxConns 次取消后新请求仍可成功，票据归零 |
| C04 Force 共享 phase | watcher 已启动/尚未启动、release 同时发生、多个 target/重复 Force、较短 parent，均一个绝对 phase；记录剩余预算/实际 control I/O 及 done，最终不得多出每连接 100ms |
| C05 过期/不可达 | 入场预算已尽、TLS/dial/读超时、CancelRequest nil 但 ctx deadline；明确稳定错误和本地 join/未 join 状态，不宣称 server 已停；不新开长 timeout |
| C06 迟到 Acquire 和容量 | Acquire 取消后后台连接仍可返回，force fence 阻止其发布；不会从 released pool.Conn 读状态；targets/goroutines 有界，无 TTL 淘汰、永久 slot 损耗或正常复用被关闭 |
| C07 Tx 与既有兼容 | 原真正 COMMIT 丢回复 unknown、rollback/锁释放、RequireHeldLocks、migration cancel、D05/Outbox 组均保持语义；不以取消结果重分类提交 |
| C08 原 app 门槛 | 原 `TestRealDatabaseHTTPTransactionDrainAndForce/blocked_io` 文件/断言/时限不变：forced ≤原 1.6s bound、进程 exit1、无 committed、事实表 0、原 exact backend 消失观察通过；原第二信号、正常 drain 与启动取消一并保持 |

普通单元/race 不启 Docker；集成仅使用授权 owned PG17.8/vector0.8.1、PG16.12 负例及原 MinIO fixture。先红 probe，后定向修复组/原 app，再跑无过滤 `scripts/test-objects.sh`；不能用定向偶尔通过替代最后完整组。原 app 测试仅允许 V 在独立副本增加已授权失败路径只读诊断，正常路径、断言和时限不改；诊断与执行源码差异如实记录。

```sh
GOTOOLCHAIN=local /workspace/toolchains/go1.27.1/bin/go test -race ./internal/central/postgres/...
AGENTEAM_GO=/workspace/toolchains/go1.27.1/bin/go sh scripts/check-go.sh
AGENTEAM_GO=/workspace/toolchains/go1.27.1/bin/go AGENTEAM_MINIO_BINARY=/owned/verified/minio sh scripts/test-objects.sh
```

MinIO 路径替换为本次已核固定 checksum 的 owned binary；定向集成过滤沿现有 fixture 参数，交付记录实际命令。报告分别写本地 cancellation attempt/join、server 实际观测和剩余预算；未观测的远端结果保持未确认。作者实现冻结后独立 V 复验，B01 整体门槛通过前不进入 B02。
