# Secret Project Audit checker 独立验收

2026-10-05，`skill_verification` 独立验收通过，建议采纳本块。主线程核 8 源后已精确提交并推送 `7d7c50df0dafcdeaaf700dc2662a6013245bbb6f`，远端相同由主线程确认；验收者独立核该提交的 8 源指纹，见 [提交核对](evidence/secret-project-audit-verification/accepted-commit.json)。本结论限于真实 Secret 事实 checker 及实际调用位置，不表示生产 Project 路由或 Secret 消费者完整装配已完成。

## 固定输入与静态结论

规格为 [Secret checker 卡](../work-items/recovery-secret-project-audit.md)，已独立静审的 rev1 SHA-256 为 `e78c72d0135efddbeac8e948c6fd2dafb9b557e06a708bf21b77939f6bbfdf43`。实现基线 `d57ce0ba60150d0377082a2996b903353e867007` 加作者冻结 8 源；[清单](evidence/secret-project-audit-verification/author-final-source.sha256) SHA-256 `73a003de920ad6a8461bb60fd5c8510b1c28be4aee803cb56f9127556b27c046`。独立执行树从固定 `git archive` 构建，只覆盖这 8 源，未使用活动主树或纳入后来 R3/S2 等变化；go.mod/go.sum 不变，完整指纹见 [fixed-inputs.json](evidence/secret-project-audit-verification/fixed-inputs.json)。

两旧生产接缝仅在实际 Append 前建立私有 witness：mutation 在 canonical/加密 receipt 写入后；resolve 在真实 lease/grant/metadata 和成功 AEAD 后。新增 checker 不开事务、不获取锁、不写入、不读加密正文或解密；它检查同一可安全比较的 Store 实例、原活 Tx、既持锁、完整 Entry/AppendKey 与当前 Secret 行。

完整 Entry 比较包含 actor/session、scope/resource/action/outcome、Metadata 字节和全部 9 个 associations；没有用刻意忽略 Session/HTTPTraceID 的 SemanticDigest 替代。Entry 的公开 Fields 返回值、metadata 原输入与 JSON 缓冲区均不能修改其内部快照。witness 保留安全身份、grant/lease 和版本/所属事实，不保留 preparedWrite、envelope、plaintext、SecretMaterial、Keyring 或 Service。resolve 失败/Unknown 仍不返回可读 material，原函数的清理与 committed 后才构造 material 的顺序保持。

mutation 核精确 receipt、payload ownership 和 create/update/delete 的 canonical 后态；changed_fields 从实际 prior purpose 重建，delete 允许 canonical 已移除。历史 receipt 重放仍走原路径，不新增 Audit，当前 Session/Owner/Project/Usage 权限没有被 witness 取代。无配置/伪造/损坏事实均拒绝；未发现产品阻断。

## 实际检查及证据复用

| 检查 | 实际结果 |
| --- | --- |
| 作者最终纯检查 | Secret/Audit 修后 race、vet 通过；最终 security integration compile-only 通过 |
| 作者新真实组 | 原 PG helper `^TestSecretProjectAudit`，7 顶层、包含最终 mutation/resolve Tx 各 3 个 Unknown 子例；security 43.901s，exit 0 |
| 作者受影响旧回归 | 旧 Secret 写入/重放、System、lease、引用/cleanup、rotation、原 writer fence/子进程恢复/Unknown，以及 3 个真实 Account 组合；security 27.965s、account 31.373s，exit 0。未把无测试包计入覆盖 |
| 独立纯增量 | `TestSecretAuditIndependentEntryViewsAreImmutable`，精确 Go 1.27.1、race/count=1 通过；Secret vet 与新真实探针 integration compile/vet 通过 |
| 独立真实增量 | `^TestSecretAuditIndependent`，1 顶层、3 子例全部通过；security 6.306s，driver 39.025s，exit 0。原 race/count=1/timeout=6m 不变 |

作者 [命令/结果流水](evidence/secret-project-audit-verification/author-results.jsonl)、[新组原日志](evidence/secret-project-audit-verification/author-pg-new-1.log)和[旧组原日志](evidence/secret-project-audit-verification/author-pg-regression-1.log)完整保留。最终 8 源与 pre-PG-2 清单相同，两个真实组运行中及之后未改源。作者新组证明真实 Secret+AEAD+Audit+PG 的写入/重放、完整 Entry/key 篡改回滚、解密前后事实变化、当前撤权与活 Tx 限制。

六个 Unknown 子例经源码和原执行命令核实：mutation 在 PrepareWrite/nonce 事务结束后才 arm，进入原 ApplyPreparedWrite+receipt+checker+Audit 的最终事务；resolve 命中实际 read/Audit 提交。它们检查 committed ACK loss、真实 rollback、客户端已 Unknown 而原 backend 仍未决；以持久计数及原锁重新串行化确认原 writer，不把一次未加锁 SELECT 的缺行当 rollback。resolve Unknown 不泄漏 material，后续读取创建新的 resolution。该同源实际证据复用，独立增量没有机械重跑这些场景。

独立检查补了以下反例：

1. **公开返回值不能同时污染 Entry 和 witness。** 修改 caller 的 changed_fields 输入、Entry.Fields/actor details、Metadata.JSON 缓冲区，16×32 次并发读取保持原 snapshot；原 witness 仍能验证，而重新构造的变更 Entry 被拒绝。见 [纯命令](evidence/secret-project-audit-verification/independent-pure-checks.json)与[原日志](evidence/secret-project-audit-verification/independent-pure-race.log)。
2. **真实 receipt payload 所属事实改变。** 在同一真实最终 Tx、checker 之前改变实际加密 receipt payload 的 owner_id；checker Forbidden，canonical/receipt/payload ownership/Audit 完整回滚。恢复 checker 后，原 prepared command 重试仅一次原子提交。
3. **相同底层 PG 不等于同一 Store 实例。** Secret 与 checker 使用指向同一真实 PG Store 的不同 wrapper 实例；原 witness 被拒绝，完整事务回滚。改回同实例后原 prepared command 正常提交。
4. **缺锁 poison 不能被调用方忽略。** 在实际 checker 的 RequireHeldLocks 边界，向原 PG Store 要求一个未持有锁；真实 Store 将原 Tx 标为失败。分别断言 callback/port 为 DependencyUnavailable、最终为 InternalError/NotCommitted，即使调用方忽略 callback 错误并返回 nil，也不能提交部分事实；原 prepared command 后续正常重试成功。

真实增量使用正式测试端口检查 Session/Project/Usage，其 checker、Secret/Audit Service、加密及 PG 均为实际实现；没有新增生产 allow。真实[命令](evidence/secret-project-audit-verification/independent-fixture-command.json)、[原日志](evidence/secret-project-audit-verification/independent-fixture.log)和[汇总](evidence/secret-project-audit-verification/verification-results.json)均保留。独立 pure/integration/PG 无失败，无预算或断言放宽。

## 作者失败记录与未绑定边界

保留作者首轮 integration compile 的两种 Cleanup 端口 cause 签名冲突/缺 import，以及 vet 的未键名测试字面量；修复限定新测试。另有实际 Store 反例：类型可比较、interface 字段的动态值却含 slice 的实例被错误接受，红例为预期 INVALID_ARGUMENT 却得到 nil。最终使用 `reflect.Value.Comparable` 拒绝该实例，防止后续接口相等比较 panic；修后 race/vet 与两个真实 PG 组通过。未声称红例实际执行到了 panic。见 [红例](evidence/secret-project-audit-verification/author-comparable-regression-red.log)和[作者交付记录](evidence/secret-project-audit-verification/author-author-handoff.md)。作者没有 PG 失败。

此块只交付 Secret 本域事实 checker。生产 Project AuditFacts 的 Secret 分派和真实 Secret CheckMutation delegate 仍未绑定；普通 Human/AgentRun 消费者端到端组合、Object checker、生命周期推进、生产 root 不在通过范围。Service resolve 正例来自真实持久 lease 与严格 fixture Usage 授权，不把 fixture 当生产授权。构造无 Service/Registry 循环，没有迁移或公共 contract 变更，00017 未被本块使用。

## 复现、资源与冻结

完整两个独立探针随仓库保留：[不可变性探针](evidence/secret-project-audit-verification/independent_project_audit_test.go.txt)，SHA-256 `0b8cce6fbe4adb7b5e01e1ef7479c2a9de4d1b9879048eb5a8686f80d9293984`；[真实回滚/poison 探针](evidence/secret-project-audit-verification/independent_secret_project_audit_test.go.txt)，SHA-256 `bfa424aac0a196c702a85668c3095f91fdb9b3c0dd2b04c0b50c58c32702ed2b`。不依赖原 tmp 存续。[reproduce.py](evidence/secret-project-audit-verification/reproduce.py)默认重建固定基线和采纳提交的 8 源及 overlay；已验证 [prepare-only 重建与指纹](evidence/secret-project-audit-verification/reproduce-prepare-check.json)。`--pure` 运行纯检查，获得独占窗口后 `--fixture` 运行原 PG helper 与独立选择器。

实际环境为精确 Go 1.27.1、锁定离线依赖、自有空 Docker 配置与本机 socket；PG 17.8/vector 0.8.1 执行业务测试，helper 的 PG 16.12 不支持版本 fixture 不计第二份业务通过。没有启动 MinIO。二进制指纹与环境覆盖均在独立命令记录。

本轮 2 容器、1 网络已由 driver nonce 清理并二次 exact-ID inspect absent；既有 2 容器/4 网络的 ID/name/labels 与已确认基线一致，任务进程 0，runtime 空，见 [清零与 8 源末检](evidence/secret-project-audit-verification/independent-cleanup-and-inputs.json)。作者两轮合计 4 容器/2 网络清零记录也保留。窗口已交回主线程供 S2 复验，验收者不再使用 Go/Docker/fixture；只归档指定报告/证据，不修改其它状态卡、业务源码或 Git。本报告与证据现已冻结，验收者 all-stop。
