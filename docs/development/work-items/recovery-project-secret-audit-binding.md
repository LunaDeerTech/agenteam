# 恢复：Project、Secret 与 Audit 的正式写入绑定

修订：rev3，仅同步阶段状态，rev2冻结API与行为不变。八源窄绑定已独立验收并由主线程采纳、提交推送`81fe7427ceb4672247b3d30a51c10a2e2808ba04`，远端一致。一般Project lease的Human subject正向resolve仍未交付；原规格与静审记录保留当时事实，正式验收及原Outbox首红边界见文末。

## 输入与完整结果

基线为 `49c6589c3919cad62a4bae2c993b5fd953d36b1e`，Secret checker 的已采纳规格为 `d57ce0b` 中的[真实事实校验卡](recovery-secret-project-audit.md)。[R3](recovery-d08-lifecycle-authority.md)已独立通过，固定提交为 `3e399c3044fbd256994ad4bb6f184a0a26787ea0`；其十六源与原冻结清单 SHA-256 `d0e5fd1c03cf6b9a22c74ae299b324d983a537a90bab8dd329d79f572d8b25d9` 逐项相同。规格只消费原基线事实、已采纳 Secret constructor/interface 和已验 R3 接缝，没有读取 Secret 八个活动实现文件。实际开工还须补齐 Secret checker 的已验提交，不依赖临时目录存续。

本块交付真实 Human Secret create/update/delete 经正式 Project delegate、Secret checker 和 Audit Service 的完整路径：当前 Session/Owner/gate、同 Tx 领域事实、原子提交、拒绝、幂等与 Unknown。补齐受限 `AuditFacts` 配置/分派，使真实 Secret Service 产生的见证能经过 Project 的当前权限检查后交到正确 checker。仅增加映射或适配类型不满足交付。

规则沿 [D08 §9.2](d08-project-owner-design.md#92-当前权限对象与-audit-分派)、[ProjectFactAuthority](../../../internal/central/audit/contract/project_fact_authority.go)与 Secret 已采纳卡；不复制其私有见证、加解密或 receipt 实现。本块不实现 Project Usage provider、AgentRun/Execution 授权、Object/Artifact checker、生命周期推进/清理、HTTP 或生产 root。Object checker 仍须等待 [D05 S2](recovery-d05-object-stop.md)冻结及独立范围下发；不能拿其活动实现作为输入。

必读 [AGENTS.md](../../../AGENTS.md)、[团队流程](../agent-team/README.md)、[开发计划](../development-plan.md)、[设计技能](../../../.agents/skills/agenteam-design/SKILL.md)和 [Go 技能](../../../.agents/skills/agenteam-go-development/SKILL.md)；独立验收者另读[验证技能](../../../.agents/skills/agenteam-verification/SKILL.md)。真实资源沿[依赖恢复记录](../agent-team/dependency-recovery-2026-10-05.md)并由主线程交接。

## 实现包 API 与构造边界

只扩 `internal/central/project` 实现包；`ac`、`sc`、`identity` 分别为现有 Audit、Secret、Identity contract，不改公共 contract。

```go
// AuthorityDependencies 原有 Sessions、Routes、Lifecycle 不变。
AuditFacts map[ac.Producer]ac.ProjectFactAuthority

type SecretAuthority struct { /* private immutable Authority reference */ }
func NewSecretAuthority(*Authority) (*SecretAuthority, error)
func (*SecretAuthority) AuthorizeProject(context.Context, foundation.Tx,
    identity.Actor, identity.ProjectID, identity.AccessIntent) (identity.AccessGrant, error)
func (*SecretAuthority) CheckMutationInTx(context.Context, foundation.Tx,
    identity.Actor, sc.CredentialRef) error
func (*SecretAuthority) CheckCleanupInTx(context.Context, foundation.Tx,
    identity.Actor, sc.LifecycleCause, identity.ProjectID) error

var _ sc.ProjectAuthority = (*SecretAuthority)(nil)
```

现有 `*Authority.CheckCleanupInTx` 接受 **Audit** LifecycleCause，与 Secret 端口同名异参，不能通过给 `*Authority` 再加一个同名方法实现两个接口。独立 SecretAuthority 只持有已构造 Authority，不另收 Store、不新增 setter 或可变 registry。nil/零值 Authority、nil/零值 delegate 均明确 unbound；不要求原 Authority 必须配置 R3 Lifecycle 或 AuditFacts 才能构造 delegate，缺失能力在实际使用处拒绝，保留旧 Owner 行为。

`NewAuthority` 将 AuditFacts 复制到私有不可变映射。共享规格的 producer 闭集为 SecretProducer/ObjectProducer，不能注册 ProjectProducer、ArtifactProducer、Account、Master、Outbox 或任意字符串覆盖内建权限。本卡只开放 SecretProducer 的实际配置与分派；ObjectProducer 是尚未交付的保留项，尝试绑定时明确 DEPENDENCY_UNBOUND，不能接受后静默忽略，也不能用 Secret checker 顶替。其它 key 为 INVALID_ARGUMENT；SecretProducer 对应 nil/typed-nil checker 为 DEPENDENCY_UNBOUND。nil/空映射兼容旧 B02/R2/R3，实际 Secret Append 因缺 provider 拒绝。调用者在构造后修改原 map 不影响内部选择；不宣称可以复制或冻结外部 provider 的内部状态。

正式装配为同一个 Store 实例 → `secret.NewProjectAuditAuthority(store)` → `project.NewAuthority(store, ... AuditFacts)` → `project.NewSecretAuthority(authority)` 与 Audit Service → Secret Service。R3 的具体 LifecycleAuthority 同 Store 构造校验保留。Secret checker 只依赖 Store，不能后置注入 Secret Service，构造不得调用数据库或 provider。

`ProjectFactAuthority` 只有一个校验方法，没有 Store getter。NewAuthority **不能**对任意 opaque checker 声称已做精确 Store 构造身份校验，也不增加 getter、反射读取私有字段或对具体 Secret 类型做绕行。当前真实保证分别是：装配明确使用同一 Store；Project 与 checker 各自 InTx 核活 Tx 归属；Secret 私有见证核 Service/checker 的精确来源。共享同一底层 Store 的两个 wrapper 不等于已被 Project 构造器证明为同一实例。跨 Store/过期 Tx 失败必须实测；若后续要求对任意 opaque provider 做构造身份比较，先升级公共 API 决策，不能在本卡假称具备。

## 当前权限、producer 与事务分派

`Authority.CheckAppendInTx` 的 Project 自有 action 继续沿原 command/creation/lifecycle 实际行检查，不走 AuditFacts。Secret 分支先验证 Entry/Key、Project scope、`ProducerFor(action)==key.Producer`、SecretResource、闭集 create/update/delete/resolve 与合法 typed metadata/outcome/ordinal，再检查当前 Actor/gate，最后调用已选 SecretProducer checker。不能因配置了 provider 就放行跨 producer/action/scope；完整 Entry/Key 和 context 原样传递，不能重建或丢掉 Secret 私有见证。Artifact、其它域与 Object 仍明确 unbound，不在此增加通用分派。

| 调用者/入口 | 本块的真实职责 |
| --- | --- |
| Human Secret mutation | Secret 原 `authorize` 已做当前 Session/AuthorizeProject；delegate 的 CheckMutationInTx 验 ref 为 Project scope，解析准确 ProjectID，在原活 Tx 再用 RequireOwnerInTx(Mutate)。实际 Owner、当前 Session、已初始化 active gate 均不可省略，admin 无旁路 |
| Human Secret Audit，包括 resolve | Project 再核同一当前 Session/Owner/Mutate gate。固定 Audit Service 对 SecretResolve 使用 Mutate，只有 Artifact list/read/download 使用 Read；本块不能把 resolve 改成 archived 合法只读 |
| Service Secret resolve Audit | 只接受 SecretService、SecretResolve、actor.ProjectID=Entry scope Project、actor.CauseRef=key.CauseRef 及有效 typed 字段；在 Project SH 下核真实 Project 已初始化且 active，再由 Secret checker 证明原 Usage/lease/grant/解密步骤。不给 Service 普通 Owner grant，也不把角色+UUID当授权 |
| Service Secret mutation / 错 ServiceName | 拒绝；当前 Project mutation 正向路径是 Human，不能借 Account/System service write 路径扩大范围 |
| AgentRun | 仍 DEPENDENCY_UNBOUND；D22 的当前 Execution 端口不在本块，不补造执行身份 |

delegate.AuthorizeProject 保持原 Authority 的 Owner 授权语义；真实 Secret 写入传入当前活 Tx。CheckMutationInTx、Audit Secret 分派及 checker 均仅消费调用者同一活 Tx：Store.InTx 确认归属、RequireHeldLocks 后重读，不 Acquire、不嵌套 Tx、不取消 handle、不调用外部 Usage。Human 所需 User SH 与 Project SH 已由 Secret `mutationLocks/preparedLocks` 收集；Credential aggregate/command/本域锁及 ref→canonical 映射仍归 Secret，不从 Project 直接查 Secret 私表或因 Owner 身份授权任意 CredentialID。

Service resolve 的原调用 actor 是 SecretService 且 cause=lease owner，Secret `leaseGrant` 已调用正式 Usage.AuthorizeLeaseInTx；Audit subject 随后转换为 SecretService/resolution。原 owner/consumer/UseGrant 关系只能由 checker 的已采纳私有见证和持久 lease 重验。Project 不从 Audit Entry 猜原 owner 后重新调用 Usage，不接收 caller 的“已经授权”布尔值。基线 Account Usage 仅覆盖 System 范围，不能冒充 Project Usage。真实组合测试可用严格持久 Usage fixture 隔离尚未绑定消费域，但必须保留此限制。

本卡不宣称一般 Project lease 的 Human subject 正向 resolve 可用。固定 Secret 仅对 AccountDelivery/AccountResponse owner 走 Usage 计划分支；一般 Project lease 的原路径只收 Project SH 与 Credential aggregate 锁，不会因为 provider 实现了 UsagePlanner 就自动预收集 User SH。缺正确 User SH 计划必须拒绝且零 material；Human Audit 仍要求 Mutate、当前 Session/Owner/gate，不能降为 Read、在 Project/checker 内补锁或借只覆盖 System 的 Account provider 伪造正向。正向 Human Project resolve 须由后继正式 UsagePlanner/消费者接缝另行明确范围与验证，本八路径不改 Secret 锁计划。AgentRun 即使已有合法形状的 UseGrant 也不因此获得本卡缺失的执行权限。

Secret delegate.CheckCleanupInTx 保持 DEPENDENCY_UNBOUND；原 Audit CheckCleanupInTx、CheckServiceLookup 不变。R3 的 stop/read-only 权限不转换为 Secret cleanup、Audit lookup 或 mutation 许可。Secret 的重放顺序不改：先当前授权，再读取已提交 receipt，重放不重新 Append、不要求旧临时见证。跨 gate 的旧命令仍按原权限拒绝，不能借 receipt 绕过撤权。

错误沿现有闭集和当前 Owner 的既有错误语义：非法配置/输入、缺依赖、伪造 producer/actor/事实、当前 gate 拒绝分别保留准确原因；Store/锁失败保守拒绝，真实 Store poison 的最终 rollback 与 callback 错误分开断言。checker/Audit 失败不改成成功 receipt；错误不暴露 SQL、明文、密文或原请求。

## 待下发的唯一八文件范围

| 类型 | 精确路径与允许改动 |
| --- | --- |
| 旧源码 2 个 | `internal/central/project/authority.go`：仅 AuditFacts 字段、私有复制与构造校验，保留 R3/Owner 接缝；`internal/central/project/audit_authority.go`：仅 CheckAppendInTx 的受限 Secret 分派与必要 import/helper 调用，不重写 Project 自有 action/lookup/cleanup |
| 新源码 2 个 | `internal/central/project/audit_facts.go`、`internal/central/project/secret_authority.go` |
| 新单测 2 个 | `internal/central/project/audit_facts_test.go`、`internal/central/project/secret_authority_test.go` |
| 新真实集成 2 个 | `tests/project/b03_secret_audit_fixture_test.go`、`tests/project/b03_secret_audit_test.go` |

八路径仅在两前置独立通过并由主线程下发后归同一实现者；authority.go 的 R3 已验内容保持冻结，当前仅有规格卡写权，不能提前动。现有 Secret 八源、Audit Service/contract、Project 其它源码/测试、SQL、Object/S2、锁文件、driver、生产 root 均只读。若缺少实际能力或必须扩范围，保留原失败并报告最小接缝，不能增加临时 allow。

## 完整验收门槛

新增真实顶层使用 `TestProjectSecretAuditBinding` 前缀；至少分 Construction、CRUD、CurrentAuthority、FactRejection、Unknown、ResolveGate 六项。测试执行真实 Account Session/Owner、Project Authority/delegate、已验 Secret checker、Secret Service/加解密、Audit Service 与 owned PG；不能用成功 receipt appender 或 provider allow 替代正向组合。

- 构造/闭集：nil/typed nil/零值、map 复制、未知 key、ProjectProducer 覆盖、Object 保留项拒绝；同 Store 正式顺序无环。区分不可证明的 opaque 构造身份与已实测的跨 Store/过期 Tx 拒绝，Validate 不获取额外锁、不嵌套 Tx。
- 正常 CRUD：active Owner create、update purpose 不变/改变、delete canonical 已不存在的合法后态；准确 receipt/Audit/版本与 changed_fields。相同命令重放零新增 Audit，原 key/credential 选择不变；System/Account Secret 兼容。
- 当前授权：撤销 Session、Owner 变化/外人/admin、漏锁/错 Project、初始化未完成、archiving/archived/deleting 以及准备后变化，真实写入拒绝且 canonical/receipt/Audit 无额外变化。检查 callback 错误及最终事务结果；R2 acceptance 或 R3 授权不能替代当前 Secret mutation gate。
- 事实伪造：直接 Audit.Append、没有真实 Secret 步骤、Entry/Key/producer/action/resource/cause/ordinal/session/association 的替换、捕获 context 后跨 Tx/Store 重用均失败；已核当前 Owner 不足以替代本域见证。沿 Secret checker 已验事实规则，不重造临时 witness mint。
- Unknown：受控真实 PG COMMIT ACK 丢失，精确武装 mutation 最终原 command Tx，覆盖原 writer committed/rollback/仍未决及恢复；canonical/receipt/Audit 原子一致、不重复，不把 PrepareWrite 的 nonce 预留提交算作 mutation，不以一次缺行判断 writer 已终局。
- ResolveGate：正式 Project gate + 真实 Secret checker；合法 Service resolve 使用真实 lease 与严格持久 Usage fixture，撤销 Usage/lease 或 Project gate 后不得发布 material。任意 Service UUID/错误角色/没有见证/AgentRun 不得放行；一般 Project lease 返回 Human subject 但缺 User SH 预收集计划时，必须实测拒绝且零 material，不作为正向能力计入。Human Audit 保持 Mutate 和当前 Session/Owner/gate，不补锁、不扩 Account fixture 的域语义，正向支持留后继 UsagePlanner/消费者范围。resolve 失败/Unknown 不发布明文沿 Secret 前置已验规则，受本次装配影响的场景补实际组合验证，不宣称生产消费域已绑定。

作者先做相关 Project/Secret/Audit 单测、race、vet/集成编译；获得资源窗口后用原 fixture 跑新增真实组及受影响 B02/R2/R3、Secret System/Account 回归，原版本、race、超时预算不弱化。真实 phase fixture 只能称 Authority 输入，不能称生命周期 runtime 已完成推进。原始失败、修复差异、命令/输入指纹、准确资源清零交独立验收；未运行/无测试包不计通过。

当前仅有规格静态检查，未运行 Go/Docker，未新增或修改任何实现。R3 已通过不代表 Secret checker 或本卡行为通过；实施仍须等待剩余前置独立门槛及主线程正式下发。

## 独立静审与采纳

2026-10-05，未参与编写的 `recovery_verification` 基于 R3 `3e399c3`、Secret 卡 `d57ce0b` 与 24 个固定输入完成独立静审。rev1 的 API、构造链与八路径无硬阻断，但一般 Project lease 当前仅取得 Project/Credential 锁，不能宣称 Human subject 正向 resolve 已具 User SH。rev2 以三处文字明确拒绝此未具备的路径，保留后继 UsagePlanner/消费者接缝责任；API、文件范围及其它行为不变。

冻结 rev2 SHA-256 `cda8ee0e2758b8b8509b38af904659df2b6181e30f70a83842a7d26599727d3b` 的 delta 复核通过，无未决静审阻断；12 个链接/fragment 与格式检查通过，未读取活动实现或执行 Go/Docker。最终报告 `/tmp/agenteam-project-secret-binding-review-llr3pb_q/review-rev2.md` 的 SHA-256 为 `8d0cb2365b5525f1acf94ed606969d03a408af238732ca76f88d60cd043d49f4`。本节持久记录结论，不依赖临时文件存续；主线程采纳仅同步状态与本记录，不开放尚未满足前置的实施。

## 八源窄绑定独立验收与后继交接

八源已独立验收并由主线程逐SHA核对清单`019f871617e5c2f49df4af44bf6c9c9153043bde69708cb72d767a7fa79d5e6f`，提交推送`81fe7427ceb4672247b3d30a51c10a2e2808ba04`、远端一致。[正式报告](../agent-team/project-secret-audit-binding-verification.md)保留作者六新跨轮组合通过、旧49顶层通过/1原Outbox顶层失败/1 child-only skip；独立map race、compile/vet及真实PG 1顶层3子例通过（4.307s），覆盖Owner撤权、缺锁poison及最终writer pending→COMMIT后撤Session拒绝旧receipt。

固定baseline与加八源各16次时间诊断均未观测逆序；原完整Outbox顶层按原断言单次通过（2.939s）。原首红未复现且原因未知，不称已修复或原旧组一次全绿，继续作为完整模块测试关注项。独立四轮8容器4网络二次exact-ID absent、runtime空、所属进程0，八源末检匹配。通过仅限本卡窄绑定；一般Human subject正向resolve、生产Usage/消费者、AgentRun、Object/Artifact事实checker、生命周期与root的未交付边界保持。

当前S2独占fixture复验review03 runtime11+2定向，再补余48旧项，独立25顶层计划待跑；Artifact/Object Audit规格已审未开写。完整D08/D28/E01仍未完成，后续按正式交接独立验收。
