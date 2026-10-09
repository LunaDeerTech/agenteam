# D04 SQL recovery 方法独立审查

结论：对作者 `sql-recovery-followup.md`（自述基线 `9ae2e0ab`）及现有实际源码，限定方法可实施；无须更改当前 core3/maintenance1 候选或生产接口。接受的是下面的证据设计，不是尚不存在的测试、工具入口或真实运行。D10 authority/plan mapping 仍为明确 controlled fixture，不能称真实 D10 Owner 发布、HTTP 重试或整卡验收。

本次只读作者树 `/workspace/agenteam-secret-variable-storage`；没有启动 Go test、子进程被测程序、PG、socket、browser 或网络，没有替作者修改提案/产品。源码路径及函数在下表和证据段列出；没有用新 mock 重复已通过的 Go/DDL 控制。冻结资产仅本文及本树 current。

## 最小实际矩阵

保持四个精确 top、十个目标格；无需顺带重复 101 行批处理、旧 kind1/2 矩阵或此前 SQL core3。分组能否在原时限内完成须据实际候选/运行判断，不据此增加预算。

| 精确 top 后缀（共同前缀 `TestSecretVariableStorageSQL`） | 目标格 | 必须获得的实际证据 |
| --- | --- | --- |
| `CommitUnknown` | before / after / pending，3 格 | 实际同 Store/Tx/完整原锁的 Apply 与 native Audit 已成功返回，随后目标 COMMIT 才被截获；原 `CommitResult.Unknown`、合法 AttemptID、原 CommandIdentity cause 保留。before 在实际退役后无新增事实；after 与 pending 在原 writer 退役后仅有原 canonical/value/kind3 receipt/native Audit。专用 Lookup/Match 及显式恢复使用当前 gate，历史返回/显式重放无重复。 |
| `NonceUnknown` | 已提交 nonce reservation 的 ACK 丢失，1 格 | 新服务初始化时代理关闭，目标 Prepare 进入真实 nonce SQL，截获时 registry high-water 确实前进；Prepare 返回 nil、`NonceReservationUnknown`/CommitUnknown，业务事实较基线零增量。相同 Service 的后继确认 reservation 再前进，后继新生成/提交的 kind1/value 与 kind3/digest wrap counter 均大于本次未确认区间上界；原 caller Intent 可继续使用，不能内部重试、返回半候选或复用该区间。 |
| `MaintenanceUnknown` | rotation refresh 可用 / 不可用 / cleanup ACK 丢失，3 格 | 使用真实 producer 创建并删除 Credential 后保留的 kind3 receipt。rotation 目标 batch 的封装 tuple/processed 在 barrier 已持久改变；可用 refresh 从真实数据库恢复，不可用只在 barrier 后 StopAdmission，保原 Unknown；后继恢复不重复 processed。cleanup 在 barrier 真实删除目标行，原调用只能空 report+Unknown；显式后续调用先重新核当前 deleting/stopped/operation，active/错 operation 不得因数据已空而通过。 |
| `Concurrency` | 同 command 同语义 / 同 command 改 value / 不同 update command 同旧 Credential version，3 格 | A 完成真实 Apply/native Audit 后在原 Tx 中持锁；B 是独立 Store/Secret/private issuer，以原完整锁进入。数据库证明 B 等的是 A 所持的确切原 advisory lock。释放 A 并实际等待 A/B 返回后，分别为同原 receipt 的重放 / KeyReused / VersionConflict，无部分写或多余 Audit。 |

## 为什么真实 Unknown 成立

- `tests/security/audit_proxy_test.go:newCommitProxy/serve` 比对真实客户端 simple-query `Q COMMIT`。before 不转发目标帧；after 必须读到原 server 的 `CommandComplete(COMMIT)` 及紧接 `ReadyForQuery(I)` 才到 barrier，再丢响应。它不替换 Store 返回值。初始化/Prepare/nonce 必须先完成并处于 disarmed，不能只凭“下一次 COMMIT”推定截中目标。
- `tests/security/outbound_reload_test.go:newOutboundLateCommitProxy/serve` 收到原 COMMIT 后关闭客户端，保留原 server 连接及事务，release 后转发同一帧；`completed` 只在真实 COMMIT/idle 帧后关闭。由此可以同时观察 caller 已 Unknown、原 backend 尚持锁、receipt 尚不可见；“尚不可见”仍不是 NotCommitted。
- `internal/central/postgres/transaction.go:WithinTxOptions` 在 callback/poison/context 错误时 rollback 并返回 NotCommitted；只有实际 `raw.Commit` 的非 proven-rollback 失败才保原 AttemptID/cause 返回 Unknown。应直接保留该结果，不能用 fixture 包装器合成 Unknown。公开 `secret.commitError`/`nextNonce` 不返回原 CommitResult，所以 nonce/maintenance 只断其真实公开错误分类和 wire/durable 事实，不臆造其未暴露的 AttemptID。
- final-write 三格在 callback 内、返回前只读实际 canonical/payload/receipt/native Audit 或保留足够精确成功事实；并要求目标 barrier 确实发生。缺锁/当前 gate 拒绝、native checker 拒绝、nonce 失败、callback 被取消时只能得到相应前置失败，不能因为最终存在某种错误而当目标 Unknown 通过。错误路径及正确拒绝需要非空精确错误断言。

## 原锁与 actual join 的实现门槛

1. A/B backend PID 均从各自原事务取得，并绑定本测试数据库；不得从 pool 另一连接取得。B 开始 `AcquireAll` 前传出 PID，在实际 PG 观察 A 持有、B 等待、`pg_blocking_pids(B)` 指向 A；`pg_locks` 的数据库、advisory lock key、mode、granted 状态须对应正式 `LockKey.AdvisoryKey()`，不能只查全库“存在 advisory wait”。同 command 与异 command 的首个冲突锁可能不同，应按原排序/完整 union 推导，不能写死为 CommandLock。
2. pending Unknown 的有界锁探测不仅要求 NotCommitted，还须证明原 caller 到达锁获取、确切 blocker 是仍活着的原 backend；权限拒绝、StopAdmission、提前取消/初始化失败、错误数据库或错误锁不能当持锁证据。探测与正常 B 竞争分别保原 lock_timeout；普通竞争在观察真实等待后释放 A，不用 sleep 假定重叠或为了通过增加 timeout。
3. `Unknown` 返回、late proxy `completed`、新直接事务取得原全部锁、测试 goroutine 返回、代理所有 forwarding/accept goroutine 的 Wait 完成是不同事实。后续 Lookup/重放仅在原 writer 全锁 join 后。仅 `completed` 不证明代理已退出，`StopMaintenance` 也不是某个已运行 MaintenanceStep 的 join。
4. 成功和 setup/断言失败路径都应先关闭/释放持有 barrier，取消需要取消的 caller，实际接收每个启动 goroutine 的终态，再关闭代理 listener/connections 并等待其原 wg；Store ForceClose/Drain 必须取得实际返回。清理回调按 LIFO 的真实次序安排，不能先等待仍被本测试持有的 callback。保原 supervisor TCP/input/owned resource 双尾，不把测试方法返回或外层 deadline 当已清理。
5. 原代理使用普通 `net.Dial`，`commitProxy` 只在 map 中登记 client、`outboundLateCommitProxy` 登记两端；正常 serve 收到一端退出后关闭双方并等第二 forwarding 返回。复用这些代码不等于已有所有故障下的限时退休证明。新增用例必须保其实际 cleanup/Wait，任何未取得尾的情况都失败，不能仅凭 outer kill 后无资源补认。若实现需要增加可调用关闭入口或限定拨号取消，应仅作必要 test-only 修改并纳真实输入闭包。

## 文案与实现需固定的细节

- 作者 nonce 表格的“every persisted kind1/kind3 ... above ... high-water”应明确限定为**本次 Unknown 后继新生成/持久化的目标 payload**；旧合法 payload/canary 不满足也不应满足该关系。相应“zero D04 rows”按业务表基线差量，而非抹去 fixture 已有 registry/canary/Audit。先在 barrier 证明 nonce high-water 真正变化，避免本地缓存命中或别的 COMMIT 被误截。
- “一个已删除 Credential 的 receipt”不意味着总计数等于 1；真实 Create/Delete 会留下各自历史 receipt。对 producer 实际生成的精确集合/前后像作比较，不靠手种 kind3、伪造 canonical/private witness 或删去多余真实历史简化。
- rotation 的预热 `PrepareRewrap` 不应被当已提交 batch，必须确认 arming 后 MaintenanceStep 仍截到目标写而不是新的 nonce reservation/final fence；barrier 的 wrapping/processed 精确事实可以排除误截。refresh unavailable 在 barrier 后才 StopAdmission，否则是 pre-gate 失败。Cleanup 与 Audit 的 Unknown 后行为不同，保现行 Secret 的空 report+Unknown，不要求其自动重试或立即 Completed。
- 独立并发 fixture 不共写原 `secretProjectAuditPorts` 计数；计划仍由各自真实绑定 issuer 构造，duplicate-create 仅共享正式 safe Ref/请求，不跨 Service 传 prepared。相同 command 改 value 应确实到达历史语义比较；不同 command 旧版本应确实到达当前 Credential 校验，而不是被人为 stale D10 fixture 提前拒绝。D10 external VariableVersion 的真实性不在本测试范围。
- Session 撤销/当前 Project gate 的负例应在已有真实 receipt 后调用专用接口，确认确切 Forbidden/current gate；把它和历史不存在、缺锁、错误 issuer 引起的拒绝分开。不能据此称正式 D10 provider 已集成。

上述是本方法接受后实现必须兑现的判据，不新增审批流程。新源、实际 helper/proxy 输入闭包、精确 selector 与预算沿原流程核定；当前 `742fb20b…4457380b` core/maintenance 候选、原 driver 和其既定实际窗口全部保持。PG 容器/网络仍可只有两项，但新增 loopback socket/goroutine 由本测试自有并严格退休。cross-epoch publication、rewrap 与 Cleanup 并发、真正 D10 Owner/HTTP 恢复未纳入这十格。

## 冻结五个 recovery 测试源码的独立静审

后继结论：对作者 `7ff214a8` 加已冻结 maintenance 新源的五文件，有限源码接受，无本范围 must-fix。精确文件为 `tests/security/secret_variable_storage_recovery_fixture_test.go`、`secret_variable_storage_recovery_test.go`、`secret_variable_storage_recovery_nonce_test.go`、`secret_variable_storage_recovery_concurrency_test.go`、`secret_variable_storage_recovery_maintenance_test.go`。作者仅早先 fixture 阶段 `43492/8ea827` 编译实际0；本次四 top 完整新源尚未编译或 PG。本人未启 Go、proxy、socket、PG、browser 或被测 child；没有用 mock 重写这些待验 SQL。原 core/maintenance 两窗口的实际接受仍只属于原输入。

实际静核闭包：

- final Unknown 三格代理在 Initialize、Prepare/nonce 全返回后才 armed。callback 原 Tx 中验证 actual Apply、kind1/3 ownership 与 native Audit，随后原 COMMIT frame barrier 才可能满足；before/after 的可见行数与 pending 的原 Unknown/Attempt/Command cause 分列。pending 原 PID 来自同 Tx connection，在释放 server COMMIT 前用精确原锁阻塞证据确认事务仍活；服务端 completed、全部原锁再取得、原调用返回各有独立等待。Lookup/Match、当前 Session 拒绝、显式同 prepared 重放均在原 writer 全锁 join 后，比较原 receipt/Ref 与最终唯一事实。
- nonce 新 binding 初始化沿真实 `registerAndVerify` 复用已有 canary，不预占本 Service 的 nonce cache；arm 后 high-water 必须实际前进1024且业务零增量，nil candidate+精确公开 Unknown 才符合目标。相同 Service 后继必须再确认一个新范围，实际发布的 value/digest 两个 wrap counter 均在后继范围且不同；没有要求旧 canary/历史行也大于本轮阈值。公开方法没有 AttemptID，不伪造此证据。
- 三个竞争格 A/B 使用独立 Store/Secret/private issuer，prepared 不跨 Service。A 在原 Tx 内实际写 native facts/Audit 后才持有 callback，B 在同一原 Tx 取得 PID 后进入正式完整锁。`secretRecoveryFirstConflict` 按正式规范化 union 查首个冲突，实际 SQL 校验两 PID、DB、advisory key 高低位/objsubid、mode、granted 和 `pg_blocking_pids`，不写死为 Command。释放后两个原 goroutine 必须返回，分别核 replay/KeyReused/VersionConflict、赢家实际 payload/计数及只一次 native Audit。stale 格外部 VariableVersion=7 与 CredentialVersion=1明确分开；实际 value-present Update 会增 CredentialVersion，即使值字节与原值相同。
- 维护三格由真实 Create/Delete 留下两个 receipt，再读取精确 kind3 payload 集合，不手种历史或凭空要求只有一条。rotation 初始化与 PrepareRewrap 预热在 arm 前，barrier 必须同时见两原 tuple 仅 wrapping/master/revision 改变及 processed=2；预热不冒实际批次。只有这个证明之后才可 StopAdmission，阻断后续 refresh/journal，不能用 pre-gate 失败伪装 Unknown。重建服务后 processed/封装不重做。cleanup barrier 真实删除后，首调用只能空 report+Unknown；empty 数据仍重新检查错 operation/active 拒绝，最后原 current deleting gate 才允许显式 Completed。
- 所有新增 flight 的 done 在原函数返回后关闭；retire 会释放 barrier、取消原 ctx、实际 proxy Close/wg、等待自有 flight，再取得 ForceClose 返回；成功路径也显式调用 retire。Rows 快照有实际 Close/Err。原 late proxy 关闭入口使用同一个 once、两端连接和原 wg，不另造“完成”标记。普通 proxy 的 `net.Dial` 与 wg 没有独立超时，仍是已披露的失败尾限制：如其不返必须保留 FAIL/缺尾，不能用外层 kill 或“现在无连接”回填 joined；本静审不宣称已证明所有故障下限时退休。

仍待实际的是五源整组编译、真实 SQL/锁与10格行为、原预算耗时、原 goroutine/proxy/Store 及 PG 两资源/TCP/input全尾。后继工具输入必须包含五新文件和实际复用的 `audit_proxy_test.go`、`outbound_reload_test.go`，精确 selector/count 与原门槛另核；不得复用旧742作为新 recovery 证据。D10当前权限、mapping/discovery 仍 controlled，生产 Owner/final Tx 接线、cross-epoch 与 maintenance 并发不扩认。本次无需增加虚构 SQL 控制套件或重复已过 Go/29DDL/100+1；结论已直接交 Model/root。
