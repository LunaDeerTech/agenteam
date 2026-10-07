# Project Owner Audit HTTP rev1 独立 STATIC 审查

结论：完整规格静态审查已完成，**1 个必修项，暂不接受 rev1**。唯一问题 AUDIT-STATIC-01 是 §7.1 的两个构建命令路径不存在；其余规格范围未发现阻断。不涉及新产品决定，也不要求扩大 14 技术＋2 文档范围。

审查者 `/root/next_frontier` 未参与本 Audit 规格编写。作者为 `/root/usage_verification`。按 agenteam-verification 技能执行，只写本审查 scratch；未修改作者稿、正式文档或产品，未执行 Go、Node、schema 验证器、测试、监听、PG、容器或 Git mutation，未再委派。

## 固定输入与方法

- 产品：`e4b1b89197da0e9027018fdb41f6ab3e04050b9d`。
- 作者稿：`/workspace/scratch/project-owner-audit-http-spec/draft-rev1.md`，SHA256 `95db31411d7e41db7966cab30b894c11d4f905696ff6a4f6572b89b47cd9c02f`。
- 作者 inputs：`53d629d3b64915a4e122f79cfeb108a07523f92a5e3cc652459c79b56a6cf8e0`；freeze：`9679bacb12f600cc32704086482a233bb70c86a8903f1d8db2443fa8afa64702`。
- 75 个列举来源以只读 `git show <固定 commit>:<单路径>` 导出到本目录 `fixed/`；逐一核 SHA256、长度和 Git blob SHA1，均匹配作者输入。没有读取正在实施的配置写入 14 源作为依据；同名历史根源只读固定快照。
- 作者 freeze 中八件文件均核指纹。`source-check.json` 保存 75 个实际快照与来源；`independent-static-checks.json` 保存独立静态枚举、容量核算、旧测试声明和精确路径存在性。
- 执行仅文件读取、散列、静态 Python 枚举／JSON 字节核算、固定 Git 单路径读取／精确 `ls-tree`。这些不是实际 Go 编码、schema 或动态行为验收。

## 必修 AUDIT-STATIC-01：两条构建路径写错

位置：作者稿 §7.1，第 137 行，app 项末句。

原文：`cmd/central、cmd/agent_server按实际fixture固定CGO变体build但不启动`。

固定产品的 `git ls-tree --name-only e4b1b89197da0e9027018fdb41f6ab3e04050b9d:cmd` 仅返回 `agenteam-runner`、`agenteam`。两个原定路径均不存在，按卡执行必然不能完成所列 build；不是尚未接受的业务依赖。

最小修复：将两路径改为 `cmd/agenteam`、`cmd/agenteam-runner`，继续保持实际 fixture CGO 变体、冻结传递输入及只 build 不启动的原要求。无须增改技术白名单或产品语义。保留 rev1 原件，新修订只需核此差量及输入指纹；其余本轮完整审查可复用。

## 其余完整范围的静态结论

| 范围 | 核对结论与固定依据 |
|---|---|
| 既定产品边界 | 架构 `docs/architecture/security-governance/audit.md` §3.1、14–17、20 已规定 Owner 统一查询、Agent 只是过滤视图与安全 metadata。新卡不授权管理员跨 Owner、Agent／Service HTTP 查询、正文、写入或停止链；不需用户再决定。 |
| 公开 facade 与同 Tx 权限 | 新 `ListProject/GetProject/ProjectReadUnknownAttempt` 可落在新增 `audit/project_query.go`，不需要改旧 Reader／Store／System 公共签名。`audit/service.go:143` 的 authorizeHuman 能在传入 Tx 内先验 Session、再 Project grant；`project/authority.go` 的 current／RequireOwnerInTx 实际要求 User SH、Project SH 并重新读 Owner／gate。`foundation/lock.go` 固定 User rank1、Project rank2，卡顺序正确。Read 对 initialized active／archiving／archived 可读、deleting／未初始化拒绝，非 Owner 含管理员隐匿规则与原 authority 一致。 |
| 完整页和授权次序 | `audit/query.go` 的 getRecord/listRecords 可供同包新 facade 直接使用非 nil check。原 SQL scope／scope_key 独立参数且 Project list 另含 project_id；filterDigest canonical-v1 不含 limit；cursor 绑定 scope＋filter＋AuditOrder。recordPage 的 check 处理每行包含哨兵，并拒绝多于 limit+1，严格读先 Close 后 Err 才签下一页。规格补充严格顺序／全过滤／GetID／Scope 验证可在新增 helper 完成，无 shared helper 改写需要。 |
| 事务终局 | `foundation/tx.go` 的原 CommitResult 保有 State／Cause／AttemptID；新私有 wrapper＋公开专属 errors.As accessor 可保存原结果而不改 System 分支。卡明确 Committed 要完整 callback＋有效 ctx；NotCommitted 保原 Fault；Unknown 零候选、原 cause/attempt，不自动确认。原 attempt 的内部 CauseID 与公共 Problem 无 cause_id 字段区分正确。 |
| 动作／Actor／资源闭集 | 独立从五个 Audit contract 源枚举通用 Action 为 53；排除 16 Account、5 System Secret/Policy 和 1 ModelSelection，Project 输出精确 31。Resource 14、ServiceName 13，与三 Actor 分支及 action 条件一致。filter 合法 System-only action 返回空结果而非 400 的要求保留了旧 Filter.Validate 语义。 |
| metadata 全矩阵 | 逐项核 `contract/types.go` NewEntry 和 metadata.go/project.go/model.go/knowledge.go 工厂／decoder：Secret purpose/value、6 consumer、27 reason；Object phase／transfer／initiator／sent 约束；Artifact source 条件／phase／failed reason；Outbox handler/redrive；Project creation/operation/transition/cause／关联；Model 六动作与字段集合／replacement／affected；Knowledge 五标量和禁关联，均与表一致。保留原上传 complete 不强制 sent==byte、download 不新加该限制、Outbox 不额外禁关联等原语义。Human wire 不要求伪造 Session，扫描原行才由真实完整 Actor 重建 Entry。 |
| scalar 与 schema | UUIDv7、微秒 UTC、十进制字符串、最小／MaxInt64、optional 缺省及未知／null 拒绝可在新增 wire/schema 实现。foundation.Instant 明确 0000–9999，卡要求标准 schema checker 扩展正确日历而非 Python datetime 误收窄合理。metadata canonical 及 typed 条件、MIME 原 Parse/Format＋charset 闭集、跨字段相等由产品校验而非夸大 schema 能力，责任清楚。 |
| 1MiB 上界 | 独立重新选择最长 Actor/service/action/resource、全部 11 顶层字段和 9 associations：Human/AgentRun/Service shell 中 Actor 编码分别 60/170/191B，最大 Service project-initialization；cause digest71B、UUID36B、Instant27B。总壳1049B，canonical metadata≤4096B，单 record≤5145B；页壳8221B（8192安全 token）、200条＋199逗号＝1,037,420B，余11,156B。原工厂限制的是已 Go JSON canonical 编码后的 metadata；Knowledge 五固定标量也远小于4096。未发现额外引号／反斜线／换行量级漏项。RawMessage 不二次字符串编码的前提已写清。这里仅证明静态包络，不声称具体代表真实最大；31工厂／200实际Go编码／201哨兵／真实 signer／原body标准解析仍是未来必验。 |
| 3s I/O 与尾部 | 规格把发布／实际 I/O 预算与同步尾部实际 join 分开；先安装 defer／Body 所有权再 bounded capability parsing；不同层首 receiver、FlushError 优先、零副作用预检、各 setter 独立 recover、Close 至多一次、callback done 必达并真实 wait、reset 失败不逃尾。业务 writer 透明给 httpapi.stateOf、controller adapter 分离，满足现有跟踪链要求。恶意环仅本 handler 接缝，不声称旧递归 stateOf 全局修复。 |
| 根与范围 | 固定 account.go 在唯一 ProjectAuthority／ModelAuthority 后创建同一 auditor，createSecurity 已注入真实 Sessions／Projects；可用新增 project_audit.go 纯构造后仅在 account.go 增加 dispatch，不需改 security/resources 或新增 work。16路径精确存在性均符合13新＋1旧技术＋2旧文档。配置写入最终接受、account根交接和实际图冻结属于调度门槛；Audit 功能不依赖未验配置代码。最终根必须消费配置接受版而非覆盖回本 e4 旧源，卡有明确约束。 |
| 动态验收设计 | 新2 facade＋3 HTTP＋1 app pure、新3 native、新4 PG 与独立A/B风险分工覆盖主要风险；28个既有 selector 的固定声明全部找到，2个配置未来 selector 明标待接受后核。物理 Unknown 精确目标／原 cause/attempt／锁／backend／实际查询绑定，terminal-first C(COMMIT)+Z(I) 后丢ACK与 held-pending 分开，不用装饰替代；默认root不注入Store／handler。安全10 syscall闭集无 buffer，当前PID/starttime、新资源门禁、120s含Cleanup／6m、实际wait／双清／daemon未wait边界齐备。唯构建路径需按上项修正。 |

## 未执行与交接

本结论仅是完整规格可实现性与既定源码契约的静态审查。未证明产品实现、实际导入／embed／TestMain 图、实际编译／race／schema／31分支最大页、3s自然 I/O、物理 Unknown、PG／native／默认根或资源退役。相关要求都保留在实施门槛，不能把本报告或静态容量脚本作为动态 PASS。

待作者按 root 授权修正 AUDIT-STATIC-01 并冻结新修订后可做最小差量复核。当前无需扩大技术路径，无需询问用户新产品规则。三停止、未绑定 Resolution／Invocations／D24／Project 创建与生命周期根、默认 readyz 既有状态均不因本卡改变。

审查原件现已冻结停写；机器可读结果及全部证据指纹见本目录 `result.json`。
