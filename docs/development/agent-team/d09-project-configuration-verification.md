# D09 Project chat 配置与安全目录独立验收

2026-10-05，`d08_recovery_design` 独立验收 **PASS**。主线程已将精确 21 源提交并推送 `de00c610da62cb77cc03efe7c3cc842cf81f1ba5`；验收者核对该提交的 21 个 blob，全部等于冻结指纹，见 [提交核对](evidence/d09-project-configuration-verification/accepted-commit.json)。结论限于 [恢复卡 rev1](../work-items/recovery-d09-project-configuration.md) 的 Project chat Provider/Model CRUD、当前配置查询、命令查证及安全可用目录，不表示完整 D09 或生产根装配完成。

## 固定输入与静态结论

生产基线为 `81fe7427ceb4672247b3d30a51c10a2e2808ba04`；作者最终 [21 源清单](evidence/d09-project-configuration-verification/source-manifest.json) SHA-256 为 `6d122a4a34b9eb31421c0b795edfb22155dc26d5a50e93b02a3a1f964dee68d6`。独立执行树只取固定基线的后端编译及 fixture 必要文件，覆盖这 21 源，再加自有探针；未消费活动 Object Audit/Artifact 或后续 Project 代码。

复用未参与实现者的 [13 生产静审](evidence/d09-project-configuration-verification/independent-production-static.md)及 [typed-nil 差量复审](evidence/d09-project-configuration-verification/independent-typednil-delta.md)。原唯一静态阻断是具名 nil channel 实现 ProjectAuthority 时未被旧 nilPort 识别；限定 `model/authority.go` 的可选 Project 依赖分支补齐拒绝后闭合，System 原构造行为保持。最终 13 生产与已审 production-rev2 完全相同；后续首红修订只涉及四个新集成测试。

已核的边界包括：当前 Session/Owner/Project gate 委派与同 Store/Tx/既持锁；Project 独立 identity/semantic 与 System 旧字节兼容；当前 Read → 原 receipt → 首次 Mutate；union 锁后映射重核；同 Project Secret retain/release；Model 自己的 typed Audit 和 private prepared fact；Project 对 Model producer 的窄 Outbox 分支、完整 Actor/Session/Summary/Project/purpose/issuer 及两阶段用途绑定。七字段可用目录仅投影 enabled System/本 Project chat；缺真实 Agent/Summary rewrite adapter 时拒绝整笔删除，未模拟改写成功。

## 实际检查和复用证据

| 检查 | 实际结果与证据粒度 |
| --- | --- |
| 作者最终真实复验 | [原日志](evidence/d09-project-configuration-verification/author-model-second-real.log)、[实际输入](evidence/d09-project-configuration-verification/author-model-second-real-inputs.json)及[完成记录](evidence/d09-project-configuration-verification/author-model-second-real-completion.json)：43 顶层、117 子例全部 PASS，无 FAIL/SKIP；8 个受影响新项、12 个旧 Model 项、23 个相邻 Project/ProjectSecret/SecretAudit/Outbox/Account 项 |
| 作者其余新项 | 5 个未变新顶层复用首轮完整 `^TestModel` 执行中的未失败结果；原轮非 verbose，无单列 PASS 行。已核实际入口/GOFLAGS 无 failfast、13 个新顶层无直接 Skip；这 5 个函数文本与最终版本完全一致，逐项见[覆盖表](evidence/d09-project-configuration-verification/verification-results.json) |
| 作者纯检查 | 作者报告普通/race、vet、集成编译和两个 cmd 编译/构建通过；原 [race 日志](evidence/d09-project-configuration-verification/author-pure-race-rev2.log)、[最终集成编译](evidence/d09-project-configuration-verification/author-fixture-rev3-compile.log)、[cmd 编译](evidence/d09-project-configuration-verification/author-cmd-compile.log)及其它 stdout/stderr 保留。早期 exact argv/exit 未单独持久化，本文复用当时执行声明与原输出，**不把空 vet/build 日志当作退出码记录，也不补造元数据** |
| 独立离线编译 | 精确 Go 1.27.1，`test -c -race -count=1 -timeout=6m -tags=integration ./tests/model`，包含最终 21 源与独立探针；exit 0，60.355s。完整 [argv/env](evidence/d09-project-configuration-verification/independent-compile-input.json)、[结果](evidence/d09-project-configuration-verification/independent-compile-result.json)及[原日志](evidence/d09-project-configuration-verification/independent-compile.log)保留；此项未运行测试 |
| 独立真实增量 | 2 顶层、5 子例全部 PASS，无 FAIL/SKIP；tests/model 9.055s，driver 92.523s，exit 0。完整 [argv/env/源指纹](evidence/d09-project-configuration-verification/independent-real-input.json)、[结果](evidence/d09-project-configuration-verification/independent-real-result.json)及[原日志](evidence/d09-project-configuration-verification/independent-real.log)保留 |

作者第二轮原日志 SHA-256 为 `52460eb827cc3cfc30034d2eec952f01730fd176d55d9b98df1a1cb6fa9606eb`；独立真实日志为 `e9b555f4c5d7c4313c25a98e78d14c39442d63514b0f96f590d38fe06df8e300`。没有把 driver 中的 `[no tests to run]` 包计作行为覆盖。原 race/count=1/每包 6m 不变，未延长预算或降低断言。

同源作者证据覆盖六 CRUD/scope/版本、Secret 引用增换减及删除竞争、准备后撤 Session/Owner/gate、reference barrier/缺 adapter、typed Audit/producer facts、Project 两阶段与 ignored-error poison、真实 final COMMIT Unknown、目录隔离及每页撤权。Unknown 在同 Tx 实际 Provider/receipt/Audit/Event 事实完整后才 arm；第二轮三个 committed 分支实际观察 COMMIT command tag 与 ReadyForQuery=Idle 后丢 ACK，rollback/pending 检查原 writer Command EX，absent/失权不返回成功回执。跨语义 A/B 竞争复用上述未变首轮项，不将 socket 关闭或 deadline 当作原 writer 已终局。

## 两个独立探针

[独立探针全文](evidence/d09-project-configuration-verification/independent_d09_project_configuration_test.go.txt) SHA-256 为 `69827f5a7ad28ffed51f4c237dde14639c27cec8c48271ecf0817bb4918df5fd`，没有生产 allow 或替换 CommitResult。

1. `TestModelProjectIndependentSixHistoricalReceiptsRequireCurrentRead`，archiving/archived 两子例。真实执行六 CRUD，删除 canonical Provider/Model，确认恰 6 commands、6 Model Audits、6 Model events、Secret 引用归零。建立 gate 后，同 User 新 Session 能重放和 Lookup 全部六个精确旧 receipt，包括 stale expected version 与资源已删除的情况；新 key 全部 ProjectNotActive。原 Session 撤销、随后 canonical Owner 改变均使旧访问者被拒绝、零回执和零新 Model 副作用。
2. `TestModelProjectIndependentLateGateBindingRollsBackAllFacts`，同 User 另一有效 Session、同 Owner 另一真实 Project、合法 Header version 改动三子例。先委派真实 Outbox Appender；同 Tx SQL 必须确认 ProviderUpdate、Secret 新引用保留/旧引用释放、Model Audit、Outbox row 和 prepared command 均已存在。再以原 deps 向真实 Project validator 提交结构合法但绑定改变的请求，必须到达 validator 才计算目标拒绝。最终 NotCommitted/Forbidden/零回执，完整旧 ProviderView JSON、两方向精确引用、两个 Secret 和所有计数恢复；同 key 正常请求随后恰好提交一次。

第二个探针的观察 wrapper 在真实 Append 后调用正式 validator 注入末端拒绝，验证错误传播和整 Tx 回滚；不宣称生产 Outbox 额外增加了该调用。两个 Project 都由真实服务创建且归同一实际 Owner，scope/ID 同步改变，未以非法 typed 构造提前失败代替权限证据。

archiving 使用真实接受路径；archived 是既有 fixture 的合法终态输入，Owner 改变也是受真实锁保护的 canonical authority 输入。它们不代表已经交付或验收生产 Archive/Restore 全域推进、所有权转移命令。

## 首红、修订与限制

[首轮原日志](evidence/d09-project-configuration-verification/author-model-first-real.log)仍为整包 FAIL 76.871s：7 个失败顶层、10 个具体失败位置。输入与[完成记录](evidence/d09-project-configuration-verification/author-model-first-real-completion.json)原样保留；[独立归因](evidence/d09-project-configuration-verification/independent-first-red-review.md)与 [rev3 窄复核](evidence/d09-project-configuration-verification/independent-test-rev3-delta.md)明确区分当时证据和后续修正。

- 三处 Session fixture 仅写 revoked_at，违反 00010 对 revoked_reason 的配对约束；原轮未建立撤销事实。修正同时写 reason/时间、要求恰 1 行并复读，再调用真实 Account 确认 SessionRevoked 后测权限。
- wrong-scope CredentialRef 在既有公开 request.Validate 先 InvalidArgument；只修正该层期望，wrong-purpose 的 Forbidden 和零副作用保留。
- 锁冲突原日志只有外层 InternalError，没有 SQLSTATE，不能追认原轮已动态证明 55P03。**第二轮**三个精确断言实际证明 NotCommitted/InternalError → postgres.LockFailed/55P03。Project missing-lock 分别断言 callback DependencyUnavailable/LockNotHeld 与最终 poison；故意忽略 callback 仍不能提交。
- 原 Audit resource 反例未同步 metadata.ProviderID，fields 反例在 create 使用非 created 字段，均在 typed constructor 先失败。修后使用合法结构，再由真实 checker Forbidden；fields 改真实 Update，追加完整 ProviderView 回滚比较，原计数断言保留。

[测试 rev1→rev2](evidence/d09-project-configuration-verification/author-test-rev1-to-rev2.patch)、[rev2→rev3](evidence/d09-project-configuration-verification/author-test-rev2-to-rev3.patch)、[typed-nil 原红](evidence/d09-project-configuration-verification/author-typednil-channel-rev1-red.log)及[生产窄修](evidence/d09-project-configuration-verification/author-production-rev1-to-rev2.patch)均归档。早期 unit/compile 红及局部修正也保存为原证据；未将这些历史失败改写成通过。

Summary 创建初值/Settings 仍待产品决定；D10 Agent 与 project_summary 的真实 canonical rewrite adapter、Resolver/Usage/Provider 调用、HTTP/app/root 和完整 D09 均未交付。Skills initializer 只沿正式测试口隔离未绑能力；测试组合不等于生产根装配。没有新迁移、公共 contract 或旧测试修改，本卡未占 00017。

## 重建、资源和冻结

[implementation.patch](evidence/d09-project-configuration-verification/implementation.patch)仅包含固定基线到采纳提交的 21 源。[reproduce.py](evidence/d09-project-configuration-verification/reproduce.py)默认在指定空私有目录重建基线、应用该差量并加入探针；已经真实执行默认 prepare-only，21 源和探针全部匹配，见 [重建结果](evidence/d09-project-configuration-verification/reproduce-prepare-result.json)。原 tmp 消失后仍可复原。`--compile` 使用精确 Go 1.27.1 与离线依赖；取得独占 fixture 窗口后才使用 `--fixture --minio <已记录 SHA 的二进制>`，并按共享流程核资源清零。

实际独立环境使用 `/workspace` 私有 GOCACHE/GOTMPDIR/TMPDIR、锁定 module cache、GOPROXY/GOSUMDB=off、本机 Docker socket 和自有空 Docker 配置。PG17 170008/vector 0.8.1 执行业务组合；PG16 160012 是 helper 的不支持版本 fixture，不计第二份 Model 业务通过。MinIO 精确源码构建 SHA 为 `dc5298474f0bc87a068f0b1135c583bb1278c17c11c512212ed7644a238c89c8`；本卡没有 Provider 网络调用或新的对象 I/O 验收声明。

15:06:01 UTC，独立运行的 4 容器/3 网络已二次 exact-ID inspect absent，原 2 容器/4 网络 ID/name/labels 完全不变，自有 process 0、tmp/runtime 空，21 源指纹相同，见 [清零与末检](evidence/d09-project-configuration-verification/independent-handoff.json)。窗口随即交还主线程供 Artifact 使用，未因报告归档占用。作者第二轮[清零证据](evidence/d09-project-configuration-verification/author-handoff-after-second-real.json)也保留。

本次仅新增本报告及对应 evidence 目录，未修改共享状态文档、业务源码或 Git。原证据按字节归档；[来源映射](evidence/d09-project-configuration-verification/archive-provenance.json)、[SHA 清单](evidence/d09-project-configuration-verification/SHA256SUMS.json)和[归档检查](evidence/d09-project-configuration-verification/archive-checks.json)支持后续核对。验收者已停止源码、fixture 和后台命令；文档与证据冻结后 all-stop。
