# D04 kind3 / Plan / Prepared / Read / Prepare 独立审查

Skills 未参与本片段设计或实现，只读 `/workspace/agenteam-secret-variable-storage`。**冻结输入 `372d1e94` 的有限阶段接受，无本范围 mustfix。** 不包含后继 Apply/native Audit、purpose 新实现、rotation/Cleanup；这些活动源通过 overlay 排除或恢复到该冻结输入。没有修改作者产品，没有 PG、socket、browser、网络或新大缓存。

范围是 `internal/central/secret/` 的 `envelope.go`、`storage.go`、`project_variable_envelope_test.go`，`contract/project_variable_plan{,_test}.go`，`service.go` 和 `project_variable_{prepared,read,prepare}{,_test}.go`，共12技术源。旧 `contract/project_variable{,_test}.go` 的 `92aca721`／本人 `35587/21c8a0` 有限接受复用，不重复完整 Intent 测试。依据 D04 工作项 rev2 §2–6 与 CheckPlan 已接受增量。

## 源码与现有证据

- kind3 限 Project、32-byte 明文/48-byte 密文；原 data/wrap AAD 继续绑定格式、scope/Project、kind、receipt owner、payload，kind1/2 编码没有改字节。数据库 kind 在 int16→byte 前闭集检查，不能借溢出成为合法 owner。作者 crypto 6 top `42255/f25143` 与独立 Python AESGCM literal vector `68539/b24eff` 输入未变，结果复用。
- opaque Plan 绑定私有 issuer、完整 Request/当前 Session，最低 command/User/Project EX、write-key SH、CredentialRef EX；locks 与 expected 均保持复制语义。历史 basis 不要求仍活 canonical，区分外部 Variable expected 与内部 Credential version。CheckPlan 先于 nonce/SQL，不重 Discover/create candidate；进入 Tx 后仍先同 Store InTx、完整 RequireHeldLocks、当前 ReceiptRead，再读专用 receipt。不能用结构 Matches 代替真实当前权限。作者 plan 两 contract 包 `97245/afbe81` 复用。
- native prepared 精确 private concrete/typed-nil/同 Service issuer 检查先于任意外部接口方法；所有 alias 共用 mutex 和 Destroy 状态，销毁清除自有 sealed buffers，保留 caller Intent。Preparation/locks 只是安全投影，不能自签私有能力。作者 prepared `8949/28c88f` 及 live canary `46230/60d6fb` 复用。
- Lookup 只读取精确历史 receipt 与 kind3 owner 元数据，不解密；observed 丢失安全失败，异 Ref 返回离锁重新准备。Match 才打开两份摘要、常量时间比较，不读当前 canonical 或业务 value。Prepare 真实 seal kind1/kind3，历史重放与 metadata-only 不封新业务值，错误路径 defer Destroy 与 digest/nonce 清理。作者 Lookup `8082/cdbf1b`、Prepare `10260/734ad6` 及互斥守卫强化 `13922/fa22d0` 复用。
- 冻结阶段未扩大旧 Purpose.Valid，旧 write 与 loadMetadata 的用途拒绝仍在。专用 purpose/旧入口隔离的后继实现不在本结论，不能把本次 crypto/plan 接受称为整个新用途生产写入已闭合。

## 本人有意义补控

`7872/530ae3` actual exit0、race **1.048s**，3 top/10 sub：

1. 真 Prepare 生成 kind3 密文，再作为受控历史行交真实 scan/open/Match 消费。相同原语义换 Session，旧 plan 必须在 SQL/nonce 前拒绝；新绑定 plan 可匹配原 receipt/ref/version。不同 description 为 KeyReused；observed 丢失、密文破坏、kind2 重标、receipt/Project/payload 替换均不发布 observation。当前 ReceiptRead 每次实际调用，任何读取当前 canonical/业务 value 的查询都会失败。
2. 真实 Match 已进入当前 authority、持有 native prepared mutex 时，从 alias 调 Destroy：原 Match 未返前不能退役或清缓冲。释放 barrier 后，原 Unknown/cause 或 cancellation 保留，未读取受保护历史；Destroy 返回后借出的所有 sealed buffers 为零，projection 不可再用，caller Intent 仍有效。
3. 公共 Match 拒 nil、typed-nil、自造 interface、包装真实 handle、另一真实 Service 的 Prepare 结果；不得调用其方法或到达 authority/SQL，不销毁原合法 prepared。

随后仅新 top `10894/a02a8b` actual exit0、race **1.016s**：先真实完成 kind3 seal，再让业务值的第二次 nonce 请求得到原 allocator 的 Unknown；必须返回 nil candidate、原 NonceReservationUnknown/Unknown 分类，已消费 nonce 不倒退，未知范围不接受/重试，caller 原材料保留。没有声称 nonce reservation 在真实数据库执行，错误后局部候选清除还结合真实 defer Destroy 源码核验。

四 top 是两次有限执行的合计；第一组三 top 源未改，不因新增单 top 重跑旧组合。受控 Store/authority/nonce-range 只支持调用顺序、拒绝和密码材料生命周期结论，不是 D10 真实授权、PG receipt 或事务提交证据。

## 复现与待闭合范围

```sh
# cwd: /workspace/agenteam-skills
python3 .agent-state/secret-variable-storage-review/run.py
# 仅最后新增控制：
STORAGE_REVIEW_RUN='^TestIndependentStoragePrepareSecondNonceUnknown$' \
 python3 .agent-state/secret-variable-storage-review/run.py
```

脚本核14个冻结源（含仅作输入的两旧 Intent 源）逐字 `372d1e94`，只运行自己的 `stages_test.go` overlay；新增活动 Go 文件变为空 package，已经修改的旧共享源取冻结原 blob。首次排除6个新 Apply 文件及原 project_audit；第二次另排新 maintenance 与还原原 rotation/cleanup。不消费作者活动实现。

固定 Go `/workspace/toolchains/go1.27.1/bin/go` 前置继承 PATH；`GOTOOLCHAIN=local GOENV=off GOWORK=off GOPROXY=off GOSUMDB=off GOTELEMETRY=off GOMAXPROCS=2 GOFLAGS=-mod=readonly`。只读 `GOMODCACHE=/workspace/agenteam/output/ai/model-ui-recovery/go-mod`，Skills 独占 `GOCACHE=/workspace/agenteam-project-variables-independent/output/ai/project-variables-independent/gocache`，`GOTMPDIR=/workspace/agenteam-skills/output/ai/skills/compile/tmp`。Go 使用 `-race -count=1 -p=1 -timeout=30s`，控制完成后实际命令均已退出。

后继门槛：真实 D10 authority 的 CheckPlan 私有 Store/binding 与 ReceiptRead/NewWrite 当前权限、原完整锁和真实 SQL；并发 create 重发现、历史 deleted Credential/receipt 重放与 KeyReused；Apply/D10 final-Tx/Audit/Outbox/Activity 原子性；正式连续迁移的约束/升级/回滚；kind3 rotation/canary/Retire、100/101 Cleanup。当前不含这些生产闭包，也不解除既有 Object Runtime 或其他停止项。

## 后继 Apply / native Audit / rotation / Cleanup 增量

独立审查输入为 `dccb6fed` Apply 与 `e0fb80df` 维护的11技术源；作者树后来 `e9fb256f` 只含无关文档，恢复后 `844cbc` 核 Go 差异为空。范围：`contract/project_variable_purpose.go`、`project_variable_metadata.go`、`project_variable_apply{,_test}.go`、`project_variable_audit{,_test}.go`、`project_audit.go`、`rotation.go`、`cleanup.go`、`project_variable_maintenance{,_test}.go`。**该后继增量有限离线接受，无本范围 mustfix。** 先前372阶段不重跑；没有把新未运行迁移/PG测试纳入结论。

- 新 Purpose 常量保持旧 Valid 闭集；专用 loader 拒旧用途，旧 loader 拒 dedicated 已存用途。Apply 先真实 Match/ReceiptRead，历史匹配直接返回原结果、不同语义拒绝；未观察时再次检查同 Tx、完整持锁、NewWrite 与 write epoch。Create 精确候选冲突需重新准备，Update/Delete 校验旧 Credential version/purpose/owner，Delete 检查 references/活 leases；metadata-only 不改变业务 payload/Secret Audit。只在 caller Tx 写值、专用 receipt 和 native Audit，不隐藏提交、补锁或确认事务。
- 新 Audit 分派只增加私有 variant，旧 mutation/resolution checker 字节保留（`f2af63` 逆核）；两 variant 互斥。实际 Apply 才产生私有上下文，绑定同 Store/Tx/Request/Entry/key/原 locks 与 receipt/value payload、前后版本；checker 重读专用 receipt、精确 kind3 owner 和实际 canonical 或删除事实。公开 prepared、projection、合法 metadata 均不是 witness。作者22项 native 负控及原6项旧 Secret Project Audit 控制在未变输入下复用，不额外复制矩阵。
- rotation 为 kind3 读原专用 receipt 的 exact id/payload/Project 取历史 Credential 锁；不依赖该 Credential 仍活。Apply 在原锁组内重新核 mapping 和 payload owner，漂移拒绝，不追加新聚合锁。原 CAS 仍使用每项 payload/master-version/wrap-revision；processed 取实际 affected count。Cleanup 继续原 Project EX/当前 lifecycle gate，保 reference/active lease Pending，在同事务新增最多100条原 Project receipt 的 tuple 检查与删除；完成还要求新 receipt 表为空。旧 kind1/2 路径没有改变。

本人新 `7587/7e1ab1` actual exit0、race **1.025s，3 top/5 sub**：

1. 构造真正合法的旧 `secret/update` identity 作校验阳性，再只改新 Purpose，公共 PrepareWrite 必须在 SQL/nonce 前拒绝；旧 current loader 的 Model 阳性、stored dedicated 拒绝与专用 loader 的 Model 拒绝分别可达。不会因沿用 D10 command namespace 本就错误而产生伪阴性。
2. Apply 的 ReceiptRead 通过后，让完整持锁条件失效：第二次检查阻止 NewWrite 和所有写/Audit。另令 NewWrite 返回原 Unknown/cause，必须原样透传、零写，不从前一个读阶段继承写权限。
3. 通过小型既有 Postgres Rows overlay bridge，让真实 `PrepareRewrap` 执行 last-ID 空页→head rescan→三种 owner kind 的扫描与实际 Rows.Close，再真实 AEAD rewrap。三种内容/ciphertext 与历史 Credential 都保持；实际 Apply 检查各自 CAS 参数，末项 UPDATE0 只计两项，末项 owner 漂移则返回 Busy/零公开进度、无 checkpoint、不补锁。漂移发生前前两项可以已执行 SQL；真实 caller Tx 的整批回滚仍须 PG，控制不冒该事实。

原作者证据复用：新 Apply/Audit 6 top `15128/4c748a`，旧 Secret Project Audit 6 top `21736/111c8b`，维护3 top/20 sub `92922/82e3af`，完整两个 Secret 包 `5785/713106`。这些及本人的 Store、authority、Appender transport、候选 Rows、CAS tag 都是明确受控输入；实际执行的是 Service/AEAD/Match/native checker/Rows wrapper，未连接数据库。环境恢复前本后继尚未启动命令；本次7587实际终态已取得，不复用未知进程结果。

复现后继独验：

```sh
# cwd: /workspace/agenteam-skills
python3 .agent-state/secret-variable-storage-review/run_increment.py
```

固定离线 env/cache 与上段相同。仅运行 `^TestIndependentStorageIncrement`，复用 [已有 Rows 桥](../knowledge-b02-review/postgres_rows_bridge.go)；新脚本检查 Secret Go 固定差异为空，未改生产或前阶段控制。`f2af63` 另核旧 Purpose/write/storage/error 逐字372原版，原 Project Audit 除唯一新分派完全不变。

后继真实闭包仍是 D10 provider/Owner final-Tx 与当前权限、连续00029迁移/SQL约束和原子回滚、原COMMIT Unknown确认、专用 receipt 并发与历史 deleted Credential、实际 rotation/head rescan/CAS/canary/Retire、100/101 Cleanup 与同 Project 完整尾。Runner 独审00029是另一范围；此处既不提前接受DDL，也不把controlled 100/101计数或原Unknown分类控制当真实数据库结论。

## SQL 候选与 owned 入口有限接受（业务未执行）

只读作者 `fde3ecb5` 两 SQL 测试文件及 `17969f85` 的 fixture。四 top 的真实边界是 Migrator/Postgres Store、Secret AEAD/receipt、Audit 服务与 native checker；D10 的 plan mapping/version 和当前授权仍由 `audit_fixture` 的测试端口提供，Discover 明确 unbound。原调用树不含 TestMain、MinIO 或出站 fixture，PG-only 两资源是适用的有限入口；实际耗时和完整尾尚未执行。

首轮源码审查提出两项测试 must-fix，作者已接受并完成最小返修：

1. `AtomicAuditAndOwnerRollback` 只查 NotCommitted/count/version，未断言目标 hook 确实到达或精确错误来源；`fixture.apply` 又主动清空所有未提交 observation，因此这个 invalid-result 断言不能证明本体拒绝路径。需要区分提前失败与实际 native 写/Audit 后的故障，并在回滚后检查旧 canonical/payload 精确身份与内容。
2. 原 `audit-wrong-payload-kind` 只把 kind3 改成 kind2；合法 kind3 下错误 owner_id 的跨行关联由 native checker 而非 DDL 保证，尚缺该真实可存输入的拒绝与回滚证据。需要证明注入 SQL 确实成功，不能把 SQL CHECK 拒绝计为 native 拒绝。

返修只改 `secret_variable_storage_test.go` 的 Atomic top：owner-tail 使用同 Tx executor 实读此次新 canonical、receipt 与 native Audit 已落，再返回带唯一 cause 的故障；Audit before 实读 native 写到位，再分别去掉 private witness、修改 kind 或保 kind3 修改 owner_id。篡改必须成功影响一行，并重新读取原 receipt 对应 payload 确認目标值；真实 checker 次数、精确错误链和 hook 次数均有断言。回滚后用独立当前读取对照旧 payload ID、purpose/version、ciphertext/data nonce/wrapped DEK/wrap nonce/master/revision，结合 receipt/payload/Audit 数量，不能再仅靠 helper 清零或任意提前失败通过。`7bad5e` 核 Replay top、原 fixture/maintenance 文件及全部产品/DDL 保持原字节。

返修新增精确错误码断言一度把 missing-lock 的提交结果写成 DependencyUnavailable。本人 `32793/f30506` actual0、race **1.020s**，调用实际 `RequireHeldLocks` 与 `rejected`，证明原 `postgres.LockNotHeld` 会 poison transaction，原 poison 投影是 NotCommitted/InternalError 并保 cause；WithinTx 源码优先原 poison，不能用 callback 的 Secret 包装结果代替。作者只改该期望为 InternalError 并加注释，保 LockNotHeld cause、零 hook/checker；新 race 编译 `57950/b929ba` 与两个精确 selector discovery `96441/8b4a0e` actual0。此控制没有打开 DB，既不执行真实 WithinTx，也不代实际回滚验证。原错误预期/旧候选不回填。

其它有限场景的路径已静核：四 effect/删除后历史重放与 KeyReused、新 Session 后当前拒绝；实际 producer 产生101条历史 receipt、删除 Credential 后轮换、移除旧 key 后重新初始化/原 receipt 重放、100+1 Cleanup；九个精确 SQLSTATE 约束负例。**冻结的测试及精确入口有限接受，无剩余本范围 must-fix；这不是业务 PASS。** 00029 静态审查由 Runner 负责。当前新候选 `secret-variable-storage-sql-reviewed.test` 为27,867,046 bytes，作者已记录实际编译与身份；原27,842,774-byte候选只有编译/列举证据。

owned 入口相对 `fde3ecb5` 只增闭集 core3 与 maintenance1，原单 top/root 控制流和 driver 字节可逆投影保持。core 要求3 top＋5 rollback＋9约束的全部17个 RUN/PASS 恰一次；维护要求唯一 top，额外/漏项/重复/FAIL/SKIP 不接受。固定两产物及本域依赖输入前后比较，读取失败安全失败。只用原PG两个资源，Go6m、driver105+15、supervisor123+3、TCP75不变；实际资源删除/双观测和业务 Wait 仍由原 Go driver 执行，不新增通用 harness。

本人 `35834/55bb7e` 实际执行作者50项离线控制；另 `79504/016f03` 8项独立 actual-main 控实际0：core/maintenance 正向、坏UTF8、日志OSError、漏PASS、重PASS、原driver exit2及末次输入读取失败。注入的是明确受控 Popen/TCP/OS 边界，不创建进程或资源；真实 supervisor main/parser/hash继续执行，核调用原 wait(123)、desc两观测、TCP两空、输入及terminal尾全到达，原2仍2。没有把控制里的空资源或替身 wait 当真实 PG 收尾。

上述入口控制执行时使用修正 missing-lock 错误码前的 `b8231f34` 业务候选；最后只改该断言和注释，driver/supervisor/控制未变，因此复用这两组入口结果。新的 `742fb20b` 候选由作者重新编译/发现，本人 `28f41c` 实核该两行与27,867,046-byte产物；没有声称已执行新业务。

```sh
# cwd: /workspace/agenteam-skills；固定离线 Go/cache 与前文相同
PYTHONDONTWRITEBYTECODE=1 python3 .agent-state/secret-variable-storage-review/sql_fault.py
PYTHONDONTWRITEBYTECODE=1 python3 .agent-state/secret-variable-storage-review/sql_entry.py
# 作者入口控制（cwd 切到 /workspace/agenteam-secret-variable-storage）
PYTHONDONTWRITEBYTECODE=1 python3 .agent-state/task-planning-recovery/secret-storage-entry-controls.py
```

仍待 root fresh grant 下的两组实际 SQL/AEAD/Audit/回滚/轮换/清理与完整 owned 尾。D10 生产 provider/Owner final-Tx、真实 COMMIT Unknown/并发 create/锁竞争不在这四组覆盖内，不据有限 SQL 测试设计接受扩大结论。
