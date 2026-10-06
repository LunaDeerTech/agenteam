# Invocation/Usage 账本验收记录

结论：**PASS，仅限 [ledger rev1 卡](../work-items/recovery-d09-invocation-usage-ledger.md)授权的 20 路径账本库与 00018 schema**。主线程已采纳、提交推送 `36e5ff1a124c957d200888ad2e40bdc4ecb01305`，推送后实际核远端一致。生产 Facts/Runtime 与 HTTP/default root 仍未绑定，完整 D09 未完成。

库支持可信调用事实下的 reservation / observation / final、历史精确回执、三表原子写入、nullable usage 汇总，以及当前 Human Owner 的 List / Aggregate / Get / Rebuild。确认读取不会授权重新发送。真实测试组合 Account、Project、Model Resolution、Secret、Audit 与受控 D04 wire；测试 Runtime 事实持久化在私有 schema，每次在原 Store/Tx、完整实锁下重读，不作为生产 Runtime 的替代。

## 固定输入与交付

作者 `backend_recovery`，独立负责人 `verification_recovery` 未参与产品实现。最终 [input13](invocation-usage-ledger-verification-evidence/inputs/author-input13.json) 是 `d09e8ef` 加精确 20 路径，871 个必要源码/运行资产指纹，SHA `b0dbeefe78312c2b2ca81f7e6de80fc8cde6b4f3354cfd3d6c7497a847b3009e`；主线程核 20 源与交付提交逐 SHA 相同。

最终接缝验证另用[组合输入](invocation-usage-ledger-verification-evidence/inputs/combined-input01.json)：账本 20 源不变，叠加已提交 `ecd733711caff5df46e423cadab52b32c34f785e` 管理读口 13 路径（含 Secret.Metadata 与 Model HTTP/OpenAPI），880 个指纹，SHA `8d97aef145a9582ac35444c9d7ba64688f0c74baec637f4bcbeba07e1ab30b23`。未消费并行 tools wire 或前端候选。core-freeze-01 实际基线 `e87c6ed` 与完整输入基线不同，原清单未重写。

[证据入口及校验脚本](invocation-usage-ledger-verification-evidence/README.md)保存原命令、环境、退出码、日志、输入/历史差量、独立 probe、资源与性能文本。脚本只读 Git 对象并核 SHA，可在旧 scratch 消失后核对原字节，不重跑业务。旧会话 wire03/schema02 临时实现和原日志未恢复，本轮不复用其历史通过声明。

## 实际检查与分版本复用

固定 Go1.27.1，offline / readonly 模块、私有 cache/TMPDIR。作者 usage / identity / model 相关 unit、race、vet、全部 integration 编译与 Central/Runner build 已通过；生产发布边界修复后又完成 input11 usage race/vet/model integration 编译，input13 测试修订编译通过。原 compile01 缺少装饰器 Acquire 的红例、unit-race01 缺运行期 OpenAPI 资产的整条 exit1 均保留；恢复原 Git 资产后只补失败包，不称原命令全绿。

所有真实运行沿原 `sh scripts/test-objects.sh -run <明确 selector>`，race / count1 / 6m / `-p=1`；`test-models.sh` 的固定 `^TestModel` 不命中八个 `TestUsage` 组。PG17 实际 170008、vector 0.8.1，PG16 实际 160012 仅为不支持版本反例，MinIO 使用规定源码构建与 SHA；没有真实供应商账号调用。

| 原始运行 | 固定输入与实际结果 |
| --- | --- |
| 作者 real01 | input07，八新顶层中 7 PASS / 1 FAIL，driver **exit1**；只在 QueryBudget 的未 ANALYZE 索引名断言失败。原日志与精确失败输入保留。 |
| 作者 real02 | input11，五个受影响顶层中 4 PASS / 1 FAIL，driver **exit1**；只在新增 Reader 用 DELETE Session 却期望 SessionRevoked 的断言失败。实际返回 Unauthenticated / 零 DTO。 |
| 独立 publication01 | 原 input07，四读取 API 在真实 Committed 后 caller 取消仍发布候选，四子例红，driver **exit1**。 |
| 独立 perf-lookup01 | 原 input07，Lookup 同族发布反例红；完整统计计时组通过，整条 driver **exit1**。 |
| 独立 final01 | input13 + 固定 probe，**11 个指定顶层全 PASS、0 skip、driver exit0**：4 独立风险组 + 6 旧回归 + 1 修后的作者 Reader。 |
| 独立 combined01 | 最终上游组合 + gate probe，**3 个指定顶层全 PASS、0 skip、driver exit0**：gate-only + UsageWireLedger + SystemModelManagementMetadata。 |

八个新组的最终依据如下；不同版本之间只复用未变语义，不称一次八组全绿，也不把无匹配测试的其他包计入通过数。

| 新顶层 | 最终证据与覆盖 |
| --- | --- |
| `TestUsageInvocationWireLedger` | real01 PASS；combined01 对最终上游组合再次 PASS。真实 usage / 0 / NULL、可靠 usage 后流失败、实际 Joined 前拒终态、真实零发送 Do 与协议 unknown。 |
| `TestUsageInvocationIdentityAndAuthority` | real02 PASS；完整合法错身份、同 Owner 真实他 Project、issuer/Store/过期 Tx/完整原 writer 锁、当前 Session、精确 Secret owner/request、已发送后技术收尾。 |
| `TestUsageInvocationReplayAndAtomicity` | real01 PASS；写入语义之后未改，独立 final01 再补吞入口错误/三表原子性及历史回执。覆盖并发 reservation/sequence、旧回执、同义/异义 final、前驱终局与 SQL/溢出/外层回滚。 |
| `TestUsageInvocationUnknownConfirmation` | real02 PASS，独立 final01 补真实 wire 后 Unknown/旧观察回执/原 writer 阻塞与零重发。 |
| `TestUsageReaderNonemptyPagination` | input13 在独立 final01 全 PASS。正式 Logout、旧 cursor 拒绝、同 User 新 Session / 不同 limit、固定签名 30 日范围、过滤/八分组/NULL、显式历史范围及无 Execution 不造摘要。此为作者测试由独立执行，没有第三轮作者 fixture。 |
| `TestUsageExecutionSummaryRebuild` | real02 PASS；多真实调用六字段精确汇总、未知/他 Project、缺失/合法结构但错误投影修复、重复与并发重建。 |
| `TestUsageSchemaAndHistory` | real01 PASS；PG17 fresh1..18、populated1..17→18、受控依赖失败后同字节重试、PG16 拒绝、真实 CHECK 反例、正式配置删除后的安全历史/nullable live FK/回执。SQL18 之后未改。 |
| `TestUsageQueryBudgetAndSafety` | real02 PASS；1201 行完整统计与实际 EXPLAIN、短 caller 锁等待、服务自身 2 秒 context、零部分结果、坏 JSON/digest、独立合法摘要下的 numeric 溢出与安全投影。 |

六个旧顶层实际在 final01 通过：`TestModelCurrentResolutionAuthorization`、`TestModelCurrentResolutionCanonicalLease`、`TestModelProjectCRUDScopeAndCanonicalReceipts`、`TestSecretModelUsagePlannedReadAndAudit`、`TestModelOpenAIChatWireHTTP`、`TestModelOpenAIChatStructuredHTTP`。精确 selector、各子例和源码归属见[独立报告](invocation-usage-ledger-verification-evidence/independent/report.md)。

## 原失败、修复与关键独立观察

- **U-R01**：终态 `terminal_version=1` 的 SQL CHECK 可能以 NULL 通过；增加显式非 NULL，真实 SQL 反例拒绝。超过 255 的 PG ARE 量词改为字母表与长度分别校验；本轮 fresh/populated 均通过，不反向声称已恢复旧 SQLSTATE2201B 的原始取证。
- **U-F01–03**：SecretService CauseRef 改为正式 LeaseOwner.ID，独立 RequestID 为 Invocation；not_sent 改用实际 denied Do/Joined；统计溢出 fixture 重算合法 digest，另测坏摘要。共享 Execution lease 的候选查找只检查本 Tx 精确已持 advisory lock，再完整 RequireHeldLocks 与 canonical 重读，避免吞掉会 poison Tx 的缺锁探查。
- **U-R02**：List、Aggregate、Get、Rebuild、Lookup 在实际 Committed 后、发布候选前复验原派生 ctx。独立原五个反例均转为 DependencyUnavailable、保留取消 cause、零 DTO；Rebuild 已发生的修复提交仍真实存在，不能改称回滚。纯测试同时核原 NotCommitted / Unknown attempt/cause 不被覆盖。
- **Reader 修正**：删除 Session 应 Unauthenticated；最终测试通过正式 Account.Logout 持久写入 command / typed event / SessionRevoked。旧 cursor 被拒、新合法 Session 续页成功，默认 From/To 与首上界保持签名值。独立 A 原未执行 probe 的同类错误版本及编译记录也保留。
- **原子性与 Unknown**：独立实际内层 InvalidState 被故意忽略、外层 Committed，仍零回执且三表逐字相同。真实 wire 后观察已提交再装饰 Unknown，后续 final 不改旧 Action/Sequence 回执；原 writer 锁阻止确认，解除后真实读取，实际 Provider 请求仍一次。结果装饰不是物理 COMMIT ACK 丢失证明。
- **gate-only 与最终依赖**：保持 Session / Runtime facts 有效，正式 BeginArchive / BeginDeleteProject 接受 archiving/deleting gate 后，原 reserve/authorized 两计划均 ProjectNotActive / NotCommitted、零回执、三表不变。最终组合另证真实账本 wire 与已提交管理 Metadata 共存；不是完整 Project lifecycle 装配验收。

## 预算、资源和限制

作者 real02 的 1201 行完整 Aggregate API 为 **1.361191577s**，普通 ANALYZE 后分页使用 `model_invocation_project_page`；独立测得 **1.378896329s**，两条 SQL EXPLAIN ANALYZE 执行约 4.12ms，主要成本在客户端严格 JSON/digest 与 race instrumentation。原 real01 的 **1.92s 是包含 EXPLAIN 的子例时长**，不是精确 API 计时。未减样本、放宽验证、强制 planner 或提高 2 秒预算；更大集合仍可能到期并返回零 DTO。服务 context 固定 2 秒，观察到的到期返回墙钟时间包含约数毫秒调度/返回开销，不宣称硬实时精确截止。

作者两轮、独立四轮均实际结束原 driver，逐轮记录 4 个 task container / 3 个 network，两次 exact-ID absent、原 2-container/4-network ID/name/labels 不变、runtime 空。real01 早于持续 PID 树监视器，独立另核 `/proc` 无作者进程；后续轮次均有 PID/starttime 观察及所属进程 0。final01 于 01:31:38 UTC、combined01 于 01:36:14 UTC 双清交还；最后 live20 与 input13 匹配。原监视器/资源记录与这一限制保留，不事后补造旧轮持续监控。

连续已验迁移前缀至 00018，但没有生产部署结论。生产 InvocationFacts owner、Runtime 编排、Provider 发送/重试、lease/Process/lifecycle/HTTP/default root 仍未绑定；测试协议 unknown 与统计 backfill 不证明网络故障恢复或历史真实发送。Summary 默认值仍待决定，Object 原任务停止、Artifact/Project 依赖阻塞、`ready=false` / 503 不变。公开 Account 入口继续验证、tools wire 继续实施，本卡不代表完整 D09 或 D26 已完成。

本次归档使用 documentation 技能，按 Git 与作者/独立原件核验、做链接/格式/hash 检查；没有重新运行 Go、Docker 或浏览器。文档交付通过本报告及状态文件 Git 历史定位。
