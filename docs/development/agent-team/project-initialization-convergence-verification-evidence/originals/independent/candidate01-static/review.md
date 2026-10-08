# candidate01 完整五源 STATIC

结论 **NEEDS_FIX：仅 IC-PG-01 新 fixture 失败收尾必修**。五源 manifest `a2c3f5fcc8cd5e34b09b67317f8f13e7cfbccdfc8a3f20f3c861ce2b77af97e5` 全部快照指纹相符；前三源与 unit04 逐项同 hash，直接复用 unit03 完整 STATIC＋unit04 排版/作者离线证据接受。没有新增生产阻断。

`initialization_convergence_fixture_test.go:46–57` 在 Drain(2s) 失败后只调用 ForceClose(1s)，即使其超时仍退出 Cleanup。固定 Store 的 ForceClose 在 total budget 到期即可返回；不是 pool/operations 已 join 的证明。应保留原错误，再实际等待 Drain 终局；外层原 top120s（含 Cleanup）/包6m负责最终有界所有权。取消或 Force 的返回不可替代 join。仅本卡新 fixture 可修，旧 Store/shared fixture 不改。本项纯 STATIC 发现，不能记为动态首红。

其余两份 PG 测试已全文检查：

- fixture 仅调用固定 `newDatabase` 的 nonce-owned DB＋真实 Migrator、两个真实 postgres.Store、同 Store 正式 Account Authority 作为 Sessions 构造依赖、正式注册 ProjectInitialization Service。`keys` 仅纯 keyring 构造。没有调用 newFixture/assemble/skillFixture，也没有初始化 Skills/Object/HTTP 或声称 Human Account 工作流。
- 原 migration00013 循环 Project/Creation FK 均为 deferred；两行在同一 seed Tx 写入，检查真实 Committed 后才调 gate。四状态的 request NULL、initialized、保护字段/revision、safe_result/event 对满足本域 SQL CHECK。名称/key/UUID/time/digest 参数与类型匹配，无 UUID/text 共享参数错误。completed 的 `{}` event/随机 Skill ID仅是本域授权输入，不能证明真实 Skills/事件发布完成。
- accepted request 不同、initializing owner 不同、failed initialized、completed uninitialized以及合法但关联错误的历史 ID/owner/version均是 schema 可接受、待 gate 拒绝的代表；后继必须以实际种子提交验证，不能把 setup SQL 失败当 gate PASS。合法当前改名/version8保留历史原 name；非active分支先有真实本域 operation 行再设置引用，archived有时间。旧 gate accepted/failed仍 InvalidState、initializing/completed成功按原规则对照。
- missing/SH/other Project EX/foreign Store/cancelled均真正调用新 gate，并故意在原 callback忽略 error，要求原 Store NotCommitted。foreign分支用 other.WithinTx→原 authority，没有嵌套继承原 Tx marker。固定 RequireHeldLocks/find 的真实 poison 会保留；expired 和 closed Store代表有实际旧 token/实际 Drain。closed Store例同时使用expired token，只证明组合拒绝，不单独证明某种内部错误优先级。
- EX存续代表两个 goroutine均使用独立于任一 Tx marker的有界 Background context；两个 backend PID都由各自真实 Tx查询。gate成功返回后原 caller仍持EX；`pg_locks`未授advisory＋`pg_stat_activity`等待＋`pg_blocking_pids`精确owner证明物理竞争，覆盖另一SH和EX。release后两个CommitResult都必须Committed；buffered result/done独立，Cleanup在启动前注册 cancel/release/ownerDone/waiterDone无条件实际等候，失败也不遗失join。无goroutine调用t.Fatal。
- 新快照比较两条完整 Project/Creation JSON及command/work_claim/Audit/Outbox/Account user/session计数；这是该范围零副作用观察，不是所有数据库字节或Account Activity字段的全面比较。生产无写和纯端口0调用证据一并复用，不扩大该snapshot声明。
- 两旧精确selector存在于固定4089 source：`TestProjectB02InitializationRejectsWrongMappingPlanAndActualSkillFacts`、`TestProjectB02InitializationMergesCompleteLockPlanAndPoisonsMissingLock`。仅兼容旧SQL Human/持久Skills测试adapter，不算生产Skills绑定或Create HTTP E2E。

本轮仅读取冻结快照/固定接受依赖并用 Python核指纹、写自有scratch；未运行 Go、format、compile、PG、Docker/监听或Git，未安装仓库源，也未读作者活动 integration overlay。新增两PG源码尚未格式化/编译/实跑；以上是可执行性静态核对，非SQL/锁竞争动态通过。正式运行仍须实际integration图、精确TestMain/selectors、full20fixture/7资源/镜像/live baseline与独占窗口。5源完整产品尚未接受，README仍未授；三停止及Skills/root未绑定保持。

复用证据：`../unit03-static/result.json`；`../unit04-offline-review/result.json`。本轮固定来源详见 `bound-inputs.json`，单一必修见 `findings.json`。审查结束时已复核candidate01五源不变。已停写。
