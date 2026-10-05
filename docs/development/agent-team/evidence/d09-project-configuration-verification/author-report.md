# D09 Project chat configuration — 作者冻结交付

2026-10-05。作者 `restore_test_dependencies`。状态：21 源停写，作者自测完成；独立最终验收交 `d08_recovery_design`，不据本报告标记完整 D09 或生产 root 完成。

## 固定输入与结果

- 生产基线：`81fe7427ceb4672247b3d30a51c10a2e2808ba04` archive；未消费活动 ObjectAudit/Artifact。
- 当前只读 21 源：`/tmp/agenteam-d09-project-config-tycjqwxh/candidate-rev3/`；manifest SHA-256 `6d122a4a34b9eb31421c0b795edfb22155dc26d5a50e93b02a3a1f964dee68d6`。精确清单直接使用该 manifest，不另造全仓索引。
- 13 生产路径为已独立静审的 production-rev2；rev3 只相对 rev2 补强 Audit Update 完整 ProviderView 回滚断言。第二轮后零源码 delta。
- 15:01:28 UTC 的 main / 固定测试 tree / frozen 副本 21 源 SHA 三方一致；`fixed-tree-check.json` 已核固定树其余 1209 个 baseline blob 一致。
- 第二轮明确执行 **43 顶层 / 117 子例，全部 PASS**，无漏选/多选/FAIL：8 个受影响新顶层、全部 12 个旧 B01 Model 顶层、23 个相邻旧回归。Model 64.922s；Project 46.424s；security 21.410s；Outbox 6.159s；Account 3.934s。
- 第一轮完整 `^TestModel` 覆盖 13 个新顶层及 12 个旧 B01 顶层，7 个新顶层失败，原输入与日志完整保留。另 5 个未受修订影响的新顶层复用首轮未失败结果；首轮未开 verbose，没有为这些成功测试单列 PASS 行。8 个第二轮新项中含已补强 poison 断言的 Producer 项；不把首轮整包失败改写成整包通过。

未变新项：
- `TestModelProjectCRUDScopeAndCanonicalReceipts`
- `TestModelProjectReceiptReadGateAndLegacySystemPlan`
- `TestModelProjectPreparationFailureRechecksReadReceipt`
- `TestModelProjectRollbackUnknownCannotAdoptDifferentSemanticReceipt`
- `TestModelProjectAvailableDirectoryHasOnlyEnabledSafeUnion`

## 完成行为与边界

真实 Account Session → Project Owner/Read/Mutate → 同 Project Secret reference → Model typed Audit → Outbox Model producer + Project 双阶段授权的配置链已在真实 PG 组合。六 CRUD、当前权限下 receipt/Lookup、Project 全配置查询、仅 enabled own+System chat 的 7 字段安全目录，scope 和稳定 user 绑定 cursor 均已实现。System 原 namespace/owners/semantic 字节及只限真实 System 行的旧计划加载保留；Projects 未绑定的 Project 请求零 I/O 拒绝。初次 union 锁、锁后映射重读、准备失败复查 receipt、当前撤权与真实 final COMMIT Unknown 确认均保留。

Project 窄 Event 分支不代替 Model 原 private prepared fact；同 deps 两阶段分别校验 Read/Mutate，完整 Actor/Session/Summary/Project/purpose/issuer 绑定。Agent/Summary canonical replacement adapter 未绑定时拒绝整 Tx；仅无引用删除/合法零 affected replacement 已交付。Summary 初值/Settings、Resolver/Usage/Provider 调用、HTTP/app/root、其他域与完整 D09 未交付，未改迁移、contract 或旧测试。测试 Skills initializer 是 B02 正式测试口，生命周期终态 fixture 是合法 authority 输入，不声称运行真实所有 participant。

## 实际检查与首红修复

Go 固定 `/workspace/toolchains/go1.27.1/bin/go`；`GOTOOLCHAIN=local`、`GOMODCACHE=/workspace/agenteam-dependency-cache/modcache`、`GOPROXY=off`，module/sum 未改。Model/Project 普通与 race 测试、vet，Model/Project/security/outbox/account 的 integration race 编译、两 cmd 编译/构建、21 路径 gofmt / diff 检查均已通过；原日志见 `author-evidence-sha256.json`。最后 test-only rev3 重跑 `go test -race -count=1 -tags=integration -run '^$' ./tests/model` 与该包 vet，通过；未重复无变化的纯检查。

真实第二轮实际命令入口：

```sh
python3 /tmp/agenteam-d09-project-config-tycjqwxh/run-fixture.py model-second-real "$(cat /tmp/agenteam-d09-project-config-tycjqwxh/evidence/second-real-regex.txt)"
```

此 runner 在固定 tree 执行原 `sh scripts/test-objects.sh -run <43 项 exact regex>`；原 driver `-tags=integration -race -count=1 -timeout=6m` 不变，GOFLAGS 仅 `-mod=readonly -p=1 -v`。实际 argv/env/21 指纹在 `model-second-real-inputs.json`；选取理由与完整项在 `second-real-selection.json`。原第一轮入口为同 runner 的 `model-first-real`，内部原 `scripts/test-models.sh`，整 Model 包 76.871s 返回失败。

第一轮 7 个真实失败顶层：
- `TestModelProjectQueryCursorsAndEveryPageCurrentAuthority`
- `TestModelProjectSecretReferencesAndAtomicEffects`
- `TestModelProjectSecretReleaseAndDeletionShareRealWriterLock`
- `TestModelProjectPreparedAuthorizationAndReferenceMapping`
- `TestModelProjectUnboundReferencesAndDeleteBarrier`
- `TestModelProjectAuditPreparedFactsAndProjectEventStages`
- `TestModelProjectRealFinalCommitUnknownThreeStates`

首红及修订的精确依据：

1. 三处 Session fixture 只设 revoked_at 违反 00010 CHECK，UPDATE 失败，原轮没有实际撤销成功。改为同时设 administrative reason、RowsAffected=1、持久复读和真实 Account RequireCurrentSession=SessionRevoked，再执行拒绝断言；本轮实际通过。
2. wrong-scope CredentialRef 在公开 request.Validate 就 InvalidArgument；校正精确预期，不改变生产映射。
3. 真实锁超时走事务 poison，外层为 InternalError/NotCommitted。原日志没有 SQLSTATE，不倒推原轮已证明 55P03。新 helper 同时要求实际 postgres LockFailed + SQLSTATE 55P03；第二轮 Secret writer、reference barrier 与 Unknown pending 三处均明示该完整链。
4. Audit resource 反例需同步合法 metadata.ProviderID；fields 从结构非法 Create 改真实 Update name + typed-valid counterfeit enabled。追加 Update 前后完整 ProviderView JSON 精确相等，证明旧行 name/version/time 等全部回滚，同时保留零新行、副作用计数和精确拒绝码。
5. Project missing-lock callback 外层 DependencyUnavailable / inner LockNotHeld；故意忽略 callback 后最终必须 InternalError / NotCommitted / 同一 LockNotHeld。Model missing-lock 与 foreign-store 也各自核 callback 和 final poison；跨 Tx 用全新 root 开新 Tx 后再传 captured witness，避免测成 Nested。

13 生产未因这些 fixture 修订变更。此前唯一生产静审红是具名 nil chan ProjectAuthority 被旧 nilPort 漏识别；仅在 model/authority.go 补 Chan typed-nil 拒绝，新 unit 先红后绿，原 service.go/System 语义不改。早期旧单元兼容红的 Command 锁顺序及无 Project delegate Lookup 零 I/O 已在授权源码修复；原日志/patch 保留。

Unknown 观察并非预设返回：按 actual prepared writer/command/receipt/Audit/Event 事实命中真正 final COMMIT。committed 模式日志三次证明 PG C=COMMIT + Z=Idle 后丢 ACK；rollback/pending 保持真实 writer，实际原 Command EX 检查终局，pending 完整 55P03。异义 receipt 冲突的新测试未受 rev2/3 影响，沿首轮完整执行证据；不把 socket 断开或请求 deadline 当 writer join。

## 资源与证据交接

仅使用自有空 DOCKER_CONFIG 与 unix socket，不读取现存 credentials，不动既有资源。原 fixture 核 PG17 170008、PG16 160012、vector 0.8.1 及精确源码构建 MinIO SHA dc5298474f0bc87a068f0b1135c583bb1278c17c11c512212ed7644a238c89c8；Model 行为主要由真实 PG 组合验证，不额外宣称对象 I/O 覆盖。

构建 cache/TMPDIR 仅自有可重建部分迁移到 `/workspace/agenteam-d09-project-config-build-tycjqwxh/{go-build,tmp}`，保留 /tmp 固定 source/manifest/log；未删其他任务文件。原预算未增加。

第二轮 driver 清理后，又于 15:01:28 UTC 两遍逐个 Docker inspect 确认 4 容器/3 网络 exactID absent；原 2 容器/4 网络 ID/name/labels 不变；自有 cwd/exe 进程 0、runner PID absent、runtime 目录空。`handoff-after-second-real.json` 保存详细结果。窗口已正式交 root，无后台 Go/Docker/fixture，21 源停止写入。后续独立验收由 root 调度。

首红 `model-first-real.log`、first inputs；本轮 `model-second-real.log`、second inputs；所有 patch、各阶段 frozen manifest 与清零证据均原样保存。`author-evidence-sha256.json` 是所需小型日志/patch 索引。本报告不替代独立验收。
