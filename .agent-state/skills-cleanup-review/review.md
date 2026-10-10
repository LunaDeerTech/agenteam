# Skills Cleanup 库与真实组合补审

结论：固定输入的本域库/已执行组合有限接受，未发现新的确认 must-fix。现有证据不能直接升级为完整 Cleanup 正式交付、完整 D08/Project purge 或 Object Runtime 通过。剩余重点是既定旧 attempt/上游规模及连续迁移交付；不建议重复已过矩阵。

审查者为 Model，未参与本 Cleanup 产品或三条组合测试。目标只读树 `/workspace/agenteam-skills-cleanup`，根固定30607a56；9个产品源固定598bc02e（含cc68首片段），3个真实测试固定58073b67，入口6a1c03b3，既有候选c04c9f12。作者只收口current/card。本文仅在审查者D10树保存，未修改作者代码、重建候选、运行测试/PG/MinIO/socket或重新取得原outer Wait。

## 核实的事实与边界

- 883f34实际只读检查：`internal/central/skill/{cleanup_audit,cleanup_authority,cleanup_maintenance,cleanup_repository,lifecycle_cleanup,lifecycle_cleanup_checkpoint,lifecycle_cleanup_transaction,object_authority,object_maintenance}.go`共9源逐字598bc02e；`tests/skills/lifecycle_cleanup{_fixture,,_unknown}_test.go`共3源逐字58073b67。
- 同次解析原`output/ai/skills-cleanup/pg/cleanup-02/pg-2657f4e5a47f402facdff3d307877766.log`及其owned.json：完整2父5子各RUN/PASS恰一次；Go PID1399503 Wait0、driver PID1397618实际Wait0；7个原ID各双次absent，private/runtime/desc/TCP各双空，inputs_unchanged=True，supervisor terminal0/102.655s。原外层session22843→f97748 actual0复用作者/root已取得的证据，未声称由本审重新wait。
- 已复用 Knowledge 独立4top/17受控子项与真实方法静审；未把其接受外推到由其本人实现的D05全部能力。按仓库 verification/test-engineering 技能，补审沿当前授权、实际事实流、同Tx与Unknown/真实join核查，没有为重复已有控制另建矩阵。

## 产品与方法判断

1. `cleanupTransaction` 在同一原事务取得完整锁并集、当前真实Project CleanupPhase，重读初始化映射；discovery或checkpoint不代权限。全空恢复仍走当前门及六表检查和本Service实际call/work账本。旧/错operation/version与部分核心缺失均不能认终态。
2. 关闭serving、插本域gate与D05 Release共一个真实Tx；只明确Committed后才执行物理调用。组合负控先同Tx看真实关闭/引用释放，再精确故障，外部重读原serving/upload/Object/无gate，排除了提前失败假回滚。
3. 物理结果须原CleanupID、正确状态、无剩余引用/活lease，同步调用真实返回后才落本域completed。Audit外层保普通delegate，exact Skill ObjectDelete先重验当前门/原Creation/完整Entry，再原样传给真实D05私有事实checker。真实组合核原payload删除或永久空marker、native Audit恰一次。
4. 本域历史单批至多32；最终D05 purge与Skills五核心仍在原liveTx。组合故障注入先看到两域均清空和原Project/Object锁，再精确故障并核外部两域原记录保留；随后真实提交及新Service全空重放，不能以公开result提前报告完成。
5. 两个真实Unknown刺激分别发生在已到达gate事实和最后两域清空后的原backend完整COMMIT帧。原Store Unknown/attempt保留，独立连接基线未提前可见；竞争者绑定真实双方PID与同DB/key/mode/pg_blocking_pids，取消后实际返回。释放后原COMMIT与proxy writer实际join，再以当前权限恢复原CleanupID或全空；不是假CommitResult或字段猜测完成。
6. Service将Cleanup登记原call，原2s与父deadline内同步执行并defer实际end；Stop取消不代return。真实组合是已返回工作/Close后的路径；held或cancelled但未returned不得Joined的负向复用受控独验，不冒该负向已独立真实PG覆盖。Runtime仍nil，foreign process death与全Object Runtime未启用。

## 最小正式交付缺口

- **历史attempt接缝仍需真实证据。** 本次65次OpenPackage得到66个joined work、真实按32/32/2推进；测试明确要求`attempts==1`，没有执行本域非当前`object_attempts`批删。既定设计§16.5/§16.8要求超过两批的实际历史attempt，以及published当前项与旧未发布/AbandonedAttempt原cause并存。Knowledge受控65 attempt证明方法/分批逻辑，不能代真实SQL/FK和D05原cause组合。应在已计划的D05大历史组合中补这个真实Skills接缝，尽量共用一次证据，不要求重跑本次两top。
- **D05和00028仍需按其已定范围闭合。** 本次真实65 reader/lease与4+5最后记录不能代D05大量旧attempt/活writer位于历史尾部/PUT跨source、有限扫描和00028升级回滚及多Project EXPLAIN/FK反查成本。LIMIT32只证明删行上限，不单独证明扫描成本。这是上游已有责任，不能通过扩大Skills权限或预算补过。
- **正式迁移顺序与集成输入。** 当前组合使用26/27/28测试前缀；正式库交付需26→27→28各自达到主线条件，再在正式集成树核实际依赖差异并只补受影响检查。不能将未验前缀连同Skills复制入main，或据本组PASS宣称前缀各域业务已交付。
- **生产root保持后继任务。** Project creation/ready/accepted Delete、Cleaning转移及其它participant停止为明确fixture；这里只有本Skills与Object Stop实际返回。真实BeginDelete、完整agent-skills-variables组合、不可变root路由/manifest与其余Object/Secret/Outbox/Audit阶段及最终DB/guard退出均未验证。不应把这些后继装配全部当成本库新增默认测试，但正式状态必须明确排除，不能标完整Project purge或D08完成。

在上述边界下，本次库和2top/5sub真实结果可作为已验子能力保存；无需为报告补跑旧pure/真实矩阵。作者实现和既有结果均未改动，审查结束无自有命令或资源。
