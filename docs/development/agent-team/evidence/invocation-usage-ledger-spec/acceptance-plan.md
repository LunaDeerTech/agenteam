# Invocation / Usage ledger 独立验收最小计划

状态：仅计划，尚未编写探针或运行 Go / fixture。复用已通过的规格静审；固定业务基线 `4295df7d51c1f171df78ab3f0d9cef2fd241a505`，已采纳规格提交 `9aad5f5`。技术正文以已审 rev1 SHA `d53835cb79618b5cea487e8d84161cdc2e4f3a9900c6d35a86a258f75f795cab` 为准。未来测试输入只能是该基线 + 作者冻结的精确 20 路径 + 独立私有 overlay，不读活动实现或复制全树。

## 作者八组的复用与必要观察

| 作者顶层组 | 冻结后需核的证据 | 独立选择 |
| --- | --- | --- |
| WireLedger | 真 Resolve / Secret read、每个 exact Exchange 的 Decision / Joined、发送次数、可靠 usage 的真实来源、拒绝提前 Finalize | 复用协议矩阵；仅取下述组合所需真实链 |
| IdentityAndAuthority | 当前 Actor / Session / Project gate、exact identity、foreign/expired Tx、缺原 writer 锁；技术收尾不授新调用 | 探针 A |
| ReplayAndAtomicity | 三表完整前后事实、同号/旧号/终态精确回执、错误被吞后的实际 CommitResult | 探针 B 的原子子例 |
| UnknownConfirmation | 底层实际 state 与装饰 Unknown 分记，原 attempt/cause，原 writer 锁阻和释放后的 exact receipt | 探针 B 的确认子例 |
| ReaderNonemptyPagination | 非空完整组、0/NULL 分母、首上界与 keyset、每页当前授权及签名绑定 | 复用作者矩阵；不足时探针 C |
| ExecutionSummaryRebuild | 同 Execution 多真实调用、完整六字段/版本、缺失/错误投影重建及并发互斥 | 不足时探针 C |
| SchemaAndHistory | PG17 fresh / PG16 populated、同字节失败重试、全旧约束/数据、正式 Model 删除后历史与 live FK | 复用作者真实原件并静审 SQL；不重复两 PG 全迁移 |
| QueryBudgetAndSafety | 有量数据/EXPLAIN、2s 含锁等待、溢出/坏行零部分结果及安全输出 | 复用作者实际 argv/exit/raw；不重复库级全矩阵 |

## 最多三个独立顶层候选

**A. `TestIndependentUsageCurrentAuthorityAndTechnicalFinalization`（一个组合场景）。** 真链得到已发送且 actual Joined 的同一 Invocation；在最后记账前正式撤销原 Human Session。使用撤销前的 reserve/authorized 计划重放，必须精确拒绝且三表完整事实不变；另起合法事务，用 exact ModelRuntime + 原因果事实确认可靠 usage 和 terminal。旧 Session 查询拒绝，新合法同 User Session 可读取真实终态/摘要。每个拒绝与正例分 Tx，不能在已 poison 的事务里证明后续成功。该场景同时区分“当前准入必须关闭”和“已发生事实必须仍可收尾”，不声称增加生产技术授权。

**B. `TestIndependentUsageAtomicityAndHistoricalConfirmation`（两个隔离子例）。**

- 原子子例：在冻结实现确认实际写入位置后，选择真实统计溢出或 SQL 约束失败，保持请求、Facts、当前权限及全锁合法。调用方故意吞掉入口错误；记录 callback 精确错误链与真实 outer CommitResult，并逐项比较 invocation、observation、summary，不能留该入口半写入。错误若使原 PG Tx poison，须断言其确切 NotCommitted；若应用写前拒绝而外层合法提交，则须证明 Committed 下该入口零改动。不能把任意错误、坏 DTO、伪造返回错误或正常回滚冒称这项命中；具体错误/state 预期待冻结实现后先固定。
- 历史确认子例：真实已提交后仅装饰返回 Unknown，保留原 attempt/cause；同一 Invocation 已有后续合法事件时仍确认原 Action/Sequence。用真实原 canonical writer 锁制造有界等待：未取得锁必须返回精确错误而非 Observed=false；释放并真实完成后读取原序号回执，Fact 来源必须是旧不可变事件，不是 latest。对比原 receipt/digest、当前 live 投影与三表事实；实际受控服务器请求数不增加。不新增网络代理/ACK 故障或自动重发。

**C. `TestIndependentUsageNonemptyAggregateAndRebuild`（仅覆盖不足时保留）。** 选择至少两个真 wire Invocation 的同 Execution 数据，含一个已知0及一个可靠字段缺失；按一个有判别力的 nullable group 核完整聚合与六字段分母，而非重复八 GroupBy。对合法结构但错误的本域摘要做已有规格允许的 fixture 设置后重建，逐字段对照 canonical rows；与一个正式 Observe/Finalize 的摘要锁持有者同步，证明不丢增量、重复重建不改版本。若作者对应真实组已提供完整数值/锁后态/版本原证据，则静审复用，删除本探针。

暂定只执行 A + B（2 top / 3 sub）；C 最多增加 1 top / 1 sub。这是风险候选，不是已冻结 selector 或运行授权。作者覆盖足够时还可进一步减少；不机械重复作者八组。

## 冻结及运行门槛

先等作者 contract / schema / 关键生产停写后，只审固定原字节：Facts 身份和旧事件映射、同 Store 原 Tx/full union、真实错误前后写入顺序、nullable / overflow / cursor SQL。测试和日志冻结后再核严格 provider 是否实际读自己的持久事实与 held locks，不能常量 allow、手造材料、伪造 join 或把新锁当原 writer。作者的 raw、输入、argv/env/exit、精确 Fault 与资源清理均与使用版本绑定；原红保留，不把后续绿色倒写为原结果。

独立源码/精确 selector/预期错误/state/输入 manifest 只在上述接缝确定后准备，并另行取得 Go/资源授权；当前不提前写大探针。届时沿原 driver、Go1.27.1/offline/readonly、race/count1/6m 与查询2s预算，不延时。跨新 Tx 用 fresh parent，保留 witness 的 context 只在正确 callback 使用；拒绝会 poison 时隔离子例。资源只归当轮唯一 owner：核 trusted exact ID/name/labels、存活时 fixture nonce、4c3n 两次 exact absent、原基线不变、所属进程/runtime 空，再立即交回窗口。

所有结论限库级账本/查询。生产 Runtime Facts、call/发送编排、Model read/release、Outbound 委派、lease/Process/lifecycle/root 仍未绑定；DB Unknown 装饰不代表网络 ACK 故障，Summary 产品决定与 ready=false / 503 不变。本计划未写仓库、业务或 SQL，未运行 Go、Docker、网络或 Git，未再委派。
