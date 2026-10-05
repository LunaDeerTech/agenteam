# Object Project Audit 独立验收

结论：**本窄卡通过，建议采纳**。实现已由主线程提交并推送 `a716ae2a16bc24c115d0207557cada96a30f1049`，主线程确认远端相同；独立核对该提交的 13 源与受验指纹全部一致。生产 Project ObjectProducer 分派、当前权限 gate 映射、root/HTTP 仍未绑定；不据此宣布 D05/D08 完整模块完成。

## 固定输入与独立性

规格为 `fcae3558f00131a0bcfbd40723b36525030bee63` 的 [rev2](../work-items/recovery-object-project-audit.md)，生产基线为已验 S2 的 `6658a6cb1f29299521773bc8dc86b2f607b8c809`。作者 final-source 的 13 源（8 生产、5 测试）manifest SHA256 为 `061a4c19791e812e68a0755059a171984bc3bf8070197ae6f034f7943f119558`；原路径/hash 清单 SHA256 为 `f865000d48eb625b8b727e4759f892194b301e144420c07a97daada5fbef7a8d`。

独立 verifier 没有参与产品实现，没有修改作者源码/测试；只消费停写副本与固定基线，在自有 snapshot 加探针。未纳入 D09、Artifact 或其它活动候选，未做 Git 写操作。全部证据与重建方法在 [evidence/object-project-audit-verification](evidence/object-project-audit-verification/README.md)；最终输入和交付提交的逐文件核对分别见 `independent/final-input-check.json`、`committed-input-check.json`。

## 审查与实际验证

三次独立静审原件保留在 `reviews/`。初审发现真实兼容阻断：GET 已完成历史 I/O，在真 retirement 后 lease released、清理中或对象逻辑 deleted 时，新 checker 错把历史 Complete 当作新读取准入。作者保留三种真实 tuple 的 FORBIDDEN 红例，仅改 `project_audit.go`；差量静审确认 GET 完成仍核当前授权、完整 Completed proof、原 manifest/owner、exact lease，以及 released 分支的真实 retirement ID/kind/digest/released_at，Issue 和 PUT 条件不放宽。作者修后真实三态通过，独立反例补证如下。

其余静审覆盖七阶段六 actions：prepublication、writer/recovery failure、predelete、transfer issue/complete/revoke。核对 same Store/活 Tx、完整 Entry/Key/九关联、实际完整 Acquire/Validate 私有阶段 proof；外部 Publish Tx 不丢外层 Transfer proof；publication/delete 保留原终态 UPDATE 前 Append；批量 revoke 仅在原 Tx/原谓词下先 UPDATE 后 Append。Audit 拒绝仍回滚原状态，stop 的 commitError 失败仍在实际 cancel 之前返回；S2 writer 引用、Discard 排除和 actual join 次序不变。

| 来源 | 实际检查与结果 |
| --- | --- |
| 作者最终纯检查，独立核证据复用 | Object/Audit unit/race **43 顶层、231 子例**；vet、integration compile、两个 cmd 实际 build 全部通过，未启动二进制。 |
| 作者真实新增，分段复用 | **10 顶层、19 子例**：外部组合 Tx/full witness 与 canonical 否定、failure/delete 前态、双 Audit/批撤销原子回滚、publication/issue/stop/最终 PUT 实际 Unknown、历史 GET 兼容。逐项来源在 `author/coverage.json`。 |
| 作者受影响旧回归 | **17 顶层、17 子例** 全部在 `final-real-1.log` 通过：完整权限 union/foreign proof、SourceLease/真实 reader join、cleanup/remote lease、旧 transfer、S2 stop/read、Download 原审计、Account Avatar/System。 |
| 独立纯检查 | 最终探针 integration race compile exit 0，4.813s；前版 probe compile/vet 也保留，前版 vet 不冒充最终版单独 vet。最终版由下述实际 race 执行。 |
| 独立真实增量 | **2 顶层、6 子例**，`tests/objects` 9.816s；driver exit 0、42.933s，含外部二次清零总 43.260s。 |

作者 `integration-new-2` **整组仍为 FAIL**，只复用未变且实际通过的项。其余红点是 PUT 重试把合法 staging 零 marker GET 混算为 payload；最终测试保留“不重复 payload”约束，按真实响应流字节观察 GET、按有效长度区分 PUT（未知长度直接失败）。`final-real-1` 中 raw stage GET `1→2`，实际 payload GET 字节 `46→46`、非空 PUT `2→2`；PUT 修正项和新增最终双 Audit Unknown 重新通过。该轮共 19 顶层/19 子例（2 新 + 17 旧）；未宣称原新组一次全绿。

独立最终 probe SHA256 为 `6fa231f1e18b2d29388cd2583dcf6693e761b226a412e2c148d482e5954444c6`，实际 argv 为 `sh scripts/test-objects.sh -run '^TestObjectAuditIndependent'`。Go 1.27.1、`GOFLAGS=-v`，原 fixture 的 `-race -count=1 -timeout=6m`、800ms Unknown 请求及 3 秒/40 步 stop 收敛预算不变。

- **直接取消观察，4 子例**：普通 Audit 拒绝及真实 committed/rollback/pending COMMIT Unknown。通过正式 AccessPlanner 观察实际 preparation context，并使用 Close 才能解阻的真实输入；Unknown 返回、原 writer 结束、当前 stop manifest 撤权拒绝后，均实测 `ctx.Err=nil`、Close=0、Read 仍阻塞。确认当前授权的 stop gate 后才 `context canceled`、Close=2、PreparePayload 真返回，随后正式 retirement 和最终 join，Audit 恰一次。不是用 `joined=0` 代替零 cancel。
- **精确 COMMIT 目标**：选择原 Tx 内实际 revoke Audit/action/resource/xmin，并要求 stop/revoke 同 Tx；日志 PID/xid 为 `140/896`、`148/981`、`156/1067`。committed 收到 PG COMMIT + idle 后丢 ACK；rollback 未转发 COMMIT；pending 证明原 backend xid/idle-in-transaction，之后放行原 writer 并等待终结。没有第 N 次事务计数或手造 Unknown。
- **真实退休事实反例，2 子例**：已正式释放的 GET lease，在 Complete callback 内分别清除本域 retirement 或改错 lease owner；先证明原 facts 被真实 checker 接受，再改坏合法 schema 行，checker 必须拒绝。实际 phase、Audit、改坏的行全部回滚；当前 Session 撤权拒绝，恢复后合法历史 GET 一次 Complete、Audit 恰一次。

最终原日志 `independent/logs/independent-real-3.log` SHA256 为 `92bef639ff65ef525f5733b22e8087ca82cb10c7b7f6bf6d75d50fdf565e6557`。未再次机械运行无关整树；selector 未命中的包显示 no tests to run，不计业务回归。

## 原失败、资源与边界

作者早期构造/编译错误、GET 真实兼容红、new1/new2 原红和精确修订均保留。new1 三个 INVALID_ARGUMENT 是本地 HTTP proxy 错配 verify-full；修正不改生产 TLS 约束。Stop 的 ResourceBusy 必须伴随 Pending，之后仍按原预算收敛；原错误的具体内部返回点未唯一定位。PUT retirement 最多三次正式 ConfirmStopped 使用同一真 terminal evidence，必须实际 LeaseActive=false；不以直接改表或延长 stop 预算替代。

独立首次启动多传 CLI `-v`，仅参数拒绝、零资源、未跑行为；改为 GOFLAGS。第二轮出现 `/tmp` 空间不足的多包编译失败；并行执行中 objects binary 仍启动，6 个 probe 子例全部在 `newTransferFixture` 初始化报 `OBJECT_PAYLOAD_MISSING`，未触达目标 stop/GET 行为。原日志完整保留，不能仅凭并存的 no-space 唯一归因该初始化红；此前即时状态曾说“未进入 probe”，本次全文归档核查纠正为“已启动、初始化失败、未到目标行为”。该轮 4 容器/3 网络已二次清零。经授权仅将自己的可重建 cache/TMPDIR/输出迁到有空间的 `/workspace` 自有目录，固定源码/manifest/日志保留原路径，再跑原测试全部成功。两轮原失败不计产品通过，未改源码、探针断言或预算；最终成功不追溯解释初次初始化错误的唯一原因。

最终独立轮完整采集 **4 容器、3 网络** exact IDs，`independent/resource-zero-1.json` 和 `resource-zero-2.json` 两次全部 absent；原 **2 容器、4 网络** ID/name/labels 逐字段不变，owned process=0、runtime entries=0。其 SHA256 分别为 `9cf3a2458cfc7e56636e739cffd1dfb0ad8ea551993d3f8bf1904dea2df3a402`、`046fb09a0c3f96f4461a736db407227ea9019c070013df704980374ddd0d7950`。窗口已交回主线程并转交 D09，没有后续 Go/Docker 命令。

作者第三次短轮的历史外部资源采集只有部分 IDs，不能写成该轮完整 4/3 清单；保留其窗口末全量 baseline 恢复、已捕获 ID absent 与原 nonce cleanup 证据。独立轮完整资源证据不追溯替代这一限制。所有成功来自真实 Object checker + Audit + PG/MinIO 与严格正式端口隔离组合，不提供生产 allow，也不代表尚未绑定的 Project/root/HTTP 已完成。最终归档后报告、证据与探针冻结；剩余工作交对应后继绑定卡。
