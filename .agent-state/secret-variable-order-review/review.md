# D10 Secret 单事务事实顺序限定复核

独立审查者：Variables agent；未参与此次顺序修订实现。输入固定为 Model 树提交 `870b50309af4909cc61915eac482e0543b617d8e`，相对 `b2c87c4f`，只审 `docs/development/work-items/d10-secret-variables-owner.md` §4.1、§6.1、§6.2 的精确差异。另两份变化是 D04 结果/current；没有产品、公开合同、DDL 或工具差异。本次不审活动 D04 实现或其真实 PG 结果。

结论：**SPEC 有限接受，无 must-fix**。这是工程顺序与私有证明边界的修正，不新增 Secret 权限，不要求变更已交付 A receipt 或现 Audit 公开 API。

对照正式 `ce65714a`：

- `projectvariable/contract/secret_commands.go` 的 `NewSecretVariableMutation` 要求 Changed 当且仅当 EventID/AuditID 均非空；不能以不完整 receipt、预造 AuditID 或新增持久 planned 行消除循环。
- `audit/service.go` 的 `AppendInTx` 先校验同 Store Tx 和 authorizeAppend，之后才生成候选 ID，实际写入/幂等读取并返回真实 AuditID。因此先用 completed receipt 授权本次 Audit 存在先后依赖。
- 正式 `project/audit_facts.go` 与 `projectvariable/authority.go` 当前仅接受普通变量事实。SPEC 保持普通 predicate/事实分支原义，Secret 仍需后继生产实现自己的闭集分支，不能把 e940 的严格读取兼容当写入授权。

冻结差异闭合以下条件：

1. finalTx 先完整 union/当前两域前像校验、D04 Apply，再写 D10 canonical/映射/version/generation/history。新 mutation witness 仅在这些实际写入成功后签发；准备阶段 discovery witness 仍仅供 Outbox 准备，不能替代。
2. mutation witness 绑定私有 issuer、同 Store/同一个活 Tx、完整当前 Actor（含 Session）、原 identity/cause、Project/Variable/operation/version、exact Entry/AppendKey 与该次真实 D04 Apply observation。没有公开 DTO→witness 工厂，也不接公开 Observation 形状充作 Apply 证明，不带材料或值摘要。
3. Project 先核当前 Session/Owner/Mutate 与锁；Secret checker 再核完整私有绑定、完整持锁及本 Tx 实际 canonical/墓碑/history 后像，与原 D04 receipt/Ref/effect/result 相合。它只在此 Audit 阶段免除尚不可能存在的 completed 要求，不免除真实写入或当前权限。
4. 真实 Audit 返回有效 AuditID 后才写完整 completed；Outbox NewFact 仍核 completed、真实 AuditID 与 D04 完整事实，不退化为 witness 放行。后续 Outbox/Activity/commit 失败同 Tx 回滚或保原 Unknown/Attempt/Cause，不重跑 callback。
5. no-op/replay 无新 witness/Audit/Event/Activity；metadata 真变化保 D10 Audit，D04 effect=none 不冒值 Mutation。已归档历史只重放原 receipt，普通变量分支和公开安全 DTO/API 不扩义。

复核为只读 Git 固定差异与正式源码对照；未运行 Go、Schema、PG、socket 或 browser，没有产品动态验收结论。实现阶段仍需验证真实 witness 签发点、foreign/ended Tx/错 Session/cause/Entry/Key/只有准备的拒绝、真实后像与完整 completed 的逐边界回滚及 Unknown。此记录不把这些尚未实现的门称通过。
