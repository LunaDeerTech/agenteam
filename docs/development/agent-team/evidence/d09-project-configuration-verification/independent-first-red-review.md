# D09 首轮失败归因独立静审 — rev1

结论：首轮保持 FAIL。rev1→rev2 的四文件测试修正符合既有契约，未发现需要改生产的已证实问题；有一个最小验收补点尚未闭合：`fields` 反例改为真实 ProviderUpdate 后，须增加原 Provider 完整 view/canonical row 不变断言。其余修正可进入真实复验。本报告不是动态验收或 D09 整体通过声明。

## 固定输入与独立性

- 审核角色：verification_worker；未参与 D09 实现。此前参与规格，不承担本次实现。
- 规格：`6e0bda1:docs/development/work-items/recovery-d09-project-configuration.md`；只读基线 `81fe742`。
- 原 21 源：`candidate-rev1/manifest.json` SHA-256 `d743fa508bea0477651b74b9848c5c2cd8e4d085a7c1857f2ec7fff30b3ec8ca`，21 项逐一核验。
- 修正 21 源：`candidate-rev2/manifest.json` SHA-256 `8c4fd951ededc8933eabe58232a74f33943b90b1dcdfc4a776c7b93b972733d9`，21 项逐一核验；13 生产、4 unit 与 rev1 完全相同，仅四个新集成文件变动。
- 作者差量 SHA-256 `d4a79b3810df2de8626cb52d1da20618866753cebf4f009deb17a904d6afc9b4`；本目录另保存独立重算的 `independent-test-diff.patch`。
- 原已结束日志：`model-first-real.log` SHA-256 `347066082d33b4ac26f6c03c7d94495772db0f1d95abbd0d1f4a3c742c5dcc81`。实际 tests/model FAIL 76.871s；7 个失败顶层、10 个具体失败位置，详见 `result.json`。没有把其他无 FAIL 条目当成本审核新动态通过证据。
- 本轮只执行固定文件读取、git show/ls-tree、rg、Python hash/diff/日志解析；没有 Go、SQL、Docker、Git 写入，没有读取活动源码或活动日志。只写本目录。

## 逐类结论

| 原失败与依据 | rev2 修正判断 | 真实复验要求 |
| --- | --- | --- |
| QueryCursors:161、PreparedAuthorization/session:388、Unknown/committed-revoked:199 的 SQL 仅写 revoked_at。00010:59–61 CHECK 要求 revoked_at/revoked_reason 成对且原因属于枚举；原 Session 插入未设两字段（旧 system_configuration_integration_test.go:252–256）。故原前置事实非法，授权断言未执行。日志只有 DATABASE_SQL_FAILED，未记录 SQLSTATE，不能追认现场 SQLSTATE。 | fixture:233–246 同时写 administrative/时间，要求恰 1 行、复读事实，并调用真实 Account.RequireCurrentSession 取得 SessionRevoked。Account authority:32–74 / repository:103–118 实际取 User SH 并读该 Session。保留三条下游权限/Unknown 断言。 | 下一轮须实际经过 helper 且下游仍拒绝；这是测试用事实输入，不宣称验了 Account 正式撤销命令。Unknown 原真实 final COMMIT selector、原 cause/attempt、无回执断言未改。 |
| SecretReferences:219 的跨 scope CredentialRef 在已有 model/contract/configuration.go:322–325 的 request.Validate 先拒绝；wrong-purpose 是不同且合法结构的后续权限边界。 | 将这一条期望改 InvalidArgument 正确；wrong-purpose 仍须 Forbidden，原六类持久计数不变。 | 保留合法 Project credential 成功、purpose 权限和零副作用；不得把这一反例称作 Secret Authority 内部拒绝证据。 |
| SecretRelease:260、UnboundReferences/shared-builder:531、Unknown/pending:209：原日志仅证明外层 InternalError。固定 postgres/transaction.go:43–56,116–124,278–325 会将底层 LockFailed poison 优先回滚，并包装 InternalError；不能仅凭外层推断当场 55P03。 | fixture:251–259 新 helper 同时要求 foundation NotCommitted/InternalError、postgres.LockFailed、SQLSTATE 55P03；比宽泛 ResourceBusy 期望更精确，未接受任意 InternalError，未延长等待预算。 | 必须由下一真实轮命中全部链；原首轮不追记锁超时已验。释放真实持锁者后 join、既有最终事实/Unknown 检查保留。 |
| Audit/two-stages:894：Project helper 返回 transaction.Fault；RequireHeldLocks 的 LockNotHeld poison 优先于 callback 包装的 DependencyUnavailable。postgres/transaction.go:251–274 与上列 rollback 逻辑吻合，项目源码 external_event_authority.go:65–69 明确包装 callback。 | rev2 integration:876–927 分别核 callback DependencyUnavailable/LockNotHeld 与最终 NotCommitted/InternalError/LockNotHeld，故意忽略 callback 仍须 poison；没有抹去原 primitive 错误。Model producer 的 foreign-store/missing-locks 同样加强为忽略 callback 仍须 NotCommitted 和精确 PG 码。 | 下一轮保留 actual primitive/rollback 检查，不能只检查安全外层。真实 Store 跨 store 会 poison 所有者（transaction.go:190–202），无需生产变更。 |
| Audit/resource:812 只换 Resource，未同步 metadata.ProviderID；既有 audit/contract/model.go:197–199 在 NewEntry 拒绝不一致。Audit/fields:808 在 provider.create 设 changed_fields=name；同文件:86–89 只允许 created。原两例均未抵达真实 Appender，不能算 verifier 已拒绝。 | resource 同步合法 metadata.ProviderID；fields 改为真实 ProviderUpdate name，而伪造合法 changed_fields=enabled。原 ac.NewEntry 校验仍存在，随后必须收到真实 tap.innerErr=Forbidden；修改避免 typed constructor 抢先失败，没有绕过它。 | **一个补点待闭合**：fields 的 v.unchanged 只比较行数，不能发现已有 Provider 的 name/version/updated_at 被错误保留。应在拒绝后读该行，比较修改前完整 view/canonical；六类计数及 innerErr/outer Forbidden 断言同时保留。 |

## 原子性与边界

固定 Model commands.go:220–295 表明 command row、canonical configuration、Secret Usage、Audit、Outbox、receipt 位于同一实际 WithinTx，任何 Audit 错误都返回 callback 并回滚；本次没有修改该实现。原六类计数覆盖 providers/models/commands/model Audit/model events/Secret references。对于新改成 Update 的反例，行数不够验证既有内容，所以单独提出上述补点，而不是放宽错误或更改生产语义。

首轮非 verbose 日志不含细分 PASS 或具体 PG 错误链；本审核不据此宣称所有未报错子项、SQLSTATE、原子性场景均已独立动态验证。下一真实轮结果由 fixture 所有者与最终独立验收继续提供。原失败证据保留，所有超时和循环预算在本次 diff 中均未改变。

## 停止状态

已停止本轮读取/报告命令，无后台任务、无 fixture 占用。仓库和 Object 作者 13 源均未写。待作者冻结最小 Provider 回滚断言差量后仅核对应差量；本报告保持冻结。
