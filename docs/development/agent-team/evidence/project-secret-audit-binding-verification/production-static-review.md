# Project + Secret binding：四生产源独立静审

2026-10-05，skill_verification。结论：固定四生产源静态审查通过，未发现要求生产返修的明确阻断；不构成八源行为验收或模块通过。作者测试仍活动，本次没有读取其活动测试或把活动整树作为输入，没有运行 Go/Docker/PG，没有仓库/Git写入或新 fixture资源。完成后 all-stop，等待正式最终八源及窗口。

输入：基线 `7d7c50df0dafcdeaaf700dc2662a6013245bbb6f`；作者 production-freeze/manifest.json SHA `7dc05fe9f319e2631b88aac707c38ac2a04823a9afba7ca6d35654542d58d95b`。四源已复制到本目录 production/，逐项核 SHA，结束时与作者冻结副本再次匹配。规格为 dc45a6a rev2，前次风险准备计划 `../agenteam-project-secret-binding-prep-4flfjaji/acceptance-prep.md`。精确输入见 static-inputs.json 和原 inputs.json。

## 生产审查

- authority.go 的旧差异只有 AuditFacts 公开依赖、私有副本及构造 copy 调用；nil Store/Session 拒绝及 R3 具体 LifecycleAuthority bound/sameStore 校验仍保留。构造无 SQL/provider 调用。
- audit_authority.go 只有原非 ProjectAction 的 unbound 改为受限 checkDomainAuditInTx。现有真实 Project create/update/acceptance 行检查、EX/command锁、lookup/cleanup 均逐字不变；不能通过 map 覆盖 ProjectProducer。
- audit_facts.go:14–30 新建私有 map；SecretProvider nil/typed-nil 用原 nilPort 拒绝，Object 明确保留 unbound，其它 key InvalidArgument。复制选择不宣称冻结 provider 内部状态，构造不读取 opaque checker 私有 Store。nil/空映射支持旧 Owner 构造。
- audit_facts.go:33–52 先校 Project scope、ProducerFor(action)==key producer、SecretProducer、SecretResource/Success/ordinal0及四动作闭集。外层 CheckAppend 已校 opaque Entry/Key/Tx；其合法 typed metadata/actor/resource 由正式构造器保障，不接受未知原始 map。
- audit_facts.go:53–87 Human 一律 RequireOwnerInTx(Mutate)，使用原活 Tx 的当前 Account Session、Owner、初始化与 gate，admin 没有旁路。Service 只允许 SecretService+SecretResolve+同Project+cause=key，Project SH/本 Store InTx、真实 canonical initialized/active 均检查，非当前 Project 不授予普通 Owner grant。AgentRun 仍 unbound。
- audit_facts.go:88–94 当前权限全部通过后才查 provider；完整 ctx/tx/Entry/Key 原样转交真实 checker，不重建 witness、不省 Session/HTTPTrace/associations、不调用 Usage，不 Acquire、不嵌套事务。真实 Secret checker 仍独立核精确 Store/Tx、完整事实与 witness；角色+UUID不能独自通过。
- secret_authority.go:13–28 delegate 保存 Authority 值副本（内含已有不可变私有 state accessor），caller 后续替换原 Authority 变量不替换 delegate 指向的构造结果。nil/零值 Authority 与 nil/零值 delegate 均 unbound且没有 nil dereference。
- secret_authority.go:31–43 对真实 CredentialRef 验 Project scope/精确 ProjectID，再用原 RequireOwnerInTx(Mutate)。不从 Project 读取 Secret 私表，不把 Owner 身份当 credential canonical/witness；这部分仍由 Secret执行。CheckCleanup 一律 unbound，未借 R3放行。
- Secret 的现有执行顺序未改：Apply 当前授权先于 receipt，replay 不重新 Append；真实 Human CRUD 已预收 User/Project/本域锁。一般 Project lease Human subject 没有 User SH 时仍在实际 Audit Session路径拒绝；没有补锁或将 resolve 降为 Read。LookupWrite 保持旧 Read语义，不能据 archived Lookup可见误报 mutation 绕过。

四源的正确静态接缝没有代替 map race、真实 Account/current facts、poison、context 保留、跨Store和COMMIT的行为证据。最终按原prep风险逐项对作者同源日志去重后补独立增量。

## 首轮日志的有限解释

只复制并阅读原 real1.log，指纹见 static-inputs.json。记录显示 CRUD、CurrentAuthority、Human无UserSH负例、mutation rollback/pending、resolve三态通过；同时保留 Construction、FactRejection、ResolveGate部分错误码断言红，以及 mutation committed的5秒 checkpoint红。由于活动测试未读取且尚未冻结，这些观察不作最终验收。

从固定原码可确认：Secret write.go/lease.go 的 Append 失败统一走 unavailable(err)，故 Execute/Read外层可为 DependencyUnavailable，内层 Project/checker仍保留 Forbidden/ProjectNotActive/DependencyUnbound等精确原因。修测试不能只把所有错误期望统一为外层码；应同时记录/断言 actual Project/Audit边界的拒绝原因及最终无partial facts。原producer/action不匹配由 Audit upfront invalid拒绝，与Project入口直接调用的错误也应分开。

跨 Tx captured ctx 反例需要避开两个伪前置：原 Tx callback 的 ctx 已取消；且 postgres.WithinTxOptions 无条件拒绝携带旧 txContextKey 的父ctx。context.WithoutCancel(captured)只移除取消，不移除旧Tx标记。因此新 WithinTx 必须从全新bounded root ctx开始，取得新Tx后才将保留witness但不取消的 captured view交给 CheckAppend/Append，确保实际命中 witness.tx!=current；不要把 Nested或取消导致的失败当跨Tx精确绑定已经验证。两者的 callback fault与最终CommitResult需分别记录。

mutation committed proxy.completed 等待超时仍未唯一归因；未读取活动测试，不能声称是测试bug或生产bug。最终须保留原预算、原失败日志，并证明实际最终 command Tx（Prepare nonce结束后）、原 COMMIT帧/owner backend、原锁重串行化、canonical/receipt/Audit tuple及一次重放，不能通过删completed断言或延长5秒掩盖未join。

## 最小独立补充优先级

1. 先核修后测试是否真实进入 gate/checker、精确原拒绝和最终 rollback，而不是仅改 err!=nil/外层码；captured新Tx使用正确父context。
2. 真 Account当前Session撤销/Owner或gate变化先于已提交receipt，同时map caller变化不影响既构造选择；统计拒绝时provider0。
3. Human一般lease缺UserSH拒绝零material，与严格持久Usage Service正例对照；原样完整Entry含有效替换Session/trace及真实跨Store/Tx负例。
4. 最终mutation Unknown original writer pending/commit/rollback的组合证据；同源作者已经真实证明的分支复用，独立补尚缺的精确锁/原子性。

生产root/HTTP、Project Usage provider、AgentRun/Execution、Human一般lease正向resolve、Object/Artifact checker、cleanup与生命周期推进均不属于当前静审通过范围。没有建议扩大八路径或改Secret/Audit公共契约。
