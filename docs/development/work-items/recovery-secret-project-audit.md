# 恢复：Secret 的 Project Audit 真实事实校验

修订：rev1。状态：已通过独立静审并获主线程采纳，代码实施与资源按下述范围另行下发。设计阶段只新增本卡，不自动开放其它文件。

## 完整结果与固定依赖

固定输入 `49c6589c3919cad62a4bae2c993b5fd953d36b1e`。正式规则沿 [D08 §9.2](d08-project-owner-design.md#92-当前权限对象与-audit-分派)及 [ProjectFactAuthority](../../../internal/central/audit/contract/project_fact_authority.go)。本块让真实 Secret 库在 create/update/delete/resolve 的真实调用点提供同事务证据，由独立 checker 核本域持久事实；以真实 Secret、Audit、PG 和严格正式端口 fixture 验证成功、拒绝、原子回滚、Unknown 与重放，不只交付接口或孤立 helper。

当前正式 interface 已存在，`secret.NewProjectAuditAuthority` 及其实现不存在。Project 的 `AuditFacts` 配置/分派、真实 Secret `CheckMutationInTx` delegate 仍不存在；正在实施的 R3 只交付生命周期事实/stop 授权，也不包含这些能力。本卡不改 Project，不在 Secret 生产代码内提供 Project allow，不声称真实 Project/消费者装配已完成。

不需要迁移，不占用 00017，不新建 resolution 表、不改变保留规则。写操作的 canonical 变化、`secret_command_receipts` 与 Audit 已在同 Tx：失败全部回滚，已提交重放由 `findReceipt` 返回，不再 Append。resolve 的 resolution UUID 原本只存在本次读取；Audit 失败/Unknown 不返回明文，之后一次真实读取重新授权和解密并产生新 resolution，不能把读取伪装成可按旧 resolution 恢复的命令。私有临时见证只证明本次 Append 前真实步骤，不是恢复记录。

必读 [AGENTS.md](../../../AGENTS.md)、[团队流程](../agent-team/README.md)、[Go 技能](../../../.agents/skills/agenteam-go-development/SKILL.md)、[设计技能](../../../.agents/skills/agenteam-design/SKILL.md)；独立验收另读[验证技能](../../../.agents/skills/agenteam-verification/SKILL.md)。沿[依赖恢复记录](../agent-team/dependency-recovery-2026-10-05.md)使用既定 Go/PG/fixture，不改版本或依赖锁；真实资源须由主线程交接。与活动 Object S2、Project/Outbox R3 文件隔离，不读取其活动候选作为实现依据。

## API、构造与权限分工

API 位于既有 `secret` 实现包，使用现有 Secret `Store` 与 Audit contract，不改任何公共 contract：

```go
type ProjectAuditAuthority struct { /* private store */ }
func NewProjectAuditAuthority(Store) (*ProjectAuditAuthority, error)
func (*ProjectAuditAuthority) CheckProjectAuditInTx(
    context.Context, foundation.Tx, audit.Entry, audit.AppendKey,
) error

var _ audit.ProjectFactAuthority = (*ProjectAuditAuthority)(nil)
```

构造只保存 Store，不调用 SQL、不依赖 Secret Service、Audit Service、Keyring、Usage 或 Project 实现。nil/typed-nil Store 返回 DEPENDENCY_UNBOUND；零值 checker 同样 unbound。见证必须携带来源 Store 的精确身份；构造器仅支持可安全比较身份的 Store 实例，不可比较的包装明确 INVALID_ARGUMENT，不通过名称/类型相同猜同一实例。Secret 旧 `New` 的普通兼容不变；实际 Service/checker 装配使用同一个 Store 实例。

正式构造顺序为 Secret/Object Store → 两域独立 checker → Project Authority 的受限 `AuditFacts` 映射 → Audit Service → Secret/Object Service。checker 不依赖后构造的 Service，不增加 setter、全局注册表或公开 mint/WithWitness。Project 后继只注册允许的 producer 并复制映射；先核当前 Project gate/actor，再调正确 producer 的 checker。Human 仍需当前 Session/真实 Owner；Service 不能取得普通 Owner grant，必须消费自身正式 Usage 等真实权限口。checker 只核 Secret 事实，不查 Project/Account/其他域私表，不替代其权限。

`CheckProjectAuditInTx` 只接收 Project scope、SecretProducer、SecretResource、闭集四动作、其合法 Outcome/Metadata/AppendKey。System/Account/Master/其它 producer 不归本口；普通 System 与 Account Secret 路径保持。缺见证、跨 Store/Tx、改 Entry/Key、任意 service UUID 或不匹配本域行均拒绝。已有 `Audit.Service.AppendInTx` 不修改。

## 两类真实见证

见证使用包内私有 context key，仅在下列真实步骤完成后、调用既有 `AppendInTx` 之前创建。见证绑定同一个非零活 Tx、精确 Store、完整 Entry/AppendKey 和本次安全本域事实。完整 Entry 比较 ActorDetails（含 Session）、scope、action、outcome、resource、metadata 及全部 associations；不能只用 `audit.SemanticDigest`，后者有意忽略 Session/HTTP trace。Key 比较 producer/cause/ordinal 全字段。见证不含明文、语义明文摘要、SecretMaterial、密钥或解密缓冲，不延长其生命。

| 入口与已完成步骤 | 见证及 checker 必须重新核对的事实 |
| --- | --- |
| `write.go:applyWriteInTx`：已通过原授权与完整锁验证，执行 canonical 变化、写入加密 receipt 及 `secret_command_receipts` 后 | 原 command identity/digest、receipt ID、credential/scope、mutation kind、purpose、result version/deleted、必要的写前安全 metadata。AppendKey ordinal=0，cause 精确为已有 `audit.CommandAppendKey` 的 command digest；不能另造 key。checker 核同 Tx 的精确 receipt；create/update 核当前 metadata 的 ref/purpose/version，delete 核 receipt 与 canonical 已删除的合法后态 |
| `lease.go:ReadCredentialForRequest`：真实 lease 未释放，`AuthorizeLeaseInTx` 返回的 UseGrant 已通过检查，当前 metadata/payload 所属与 purpose 已核对，真实 AEAD 解密成功后 | resolution、原调用 actor、实际 audit subject、lease/ref/owner/consumer、metadata/payload ID/version、UseGrant 的安全关联字段及完整 Entry/Key。checker 重读实际未释放 lease 与 metadata/payload 关联，核 resolution ordinal=0、subject 变换、consumer 和全部 associations；不能仅根据存在 lease 或任意 resolution UUID 成功 |

更新 receipt 不保存旧 purpose，不能从更新后行猜 `changed_fields`。mutation 私有见证保留实际写前 purpose 与本次新 purpose等安全字段，重建现有 `SecretMutationMetadata`：create 为 value+purpose，update 固定 value 并仅在实际 purpose 改变时增加 purpose，delete 无 changed_fields；核完整 metadata。只有真实 `applyWriteInTx` 在正确阶段可创建该见证，不能导出供 caller 断言“已写入”。不为此扩 receipt/schema。

resolve 的 Service subject 仍沿既有代码转换为 `SecretService`、CauseRef=本次 resolution；原 lease owner 与调用 actor、实际 UseGrant 的关系由已完成的真实 `leaseGrant` 步骤及见证保存，再结合持久 lease 检查。不得因新的 Audit subject 丢掉原授权关联，也不重新伪造 UseGrant。Human/AgentRun subject 的真正权限仍由 Project/消费域正式端口负责，未绑定时拒绝；fixture 不补造生产缺失的 User/Execution 权限。

checker 每次用自身 Store.InTx 确认同一活 Tx，再以现有 RequireHeldLocks 复核所需锁。mutation 按原 command、actor、scope、ref 重建既有 prepared/mutation lock 要求；resolve 至少核 Project SH 与 Credential aggregate SH，EX 可覆盖。只核锁，不 Acquire、不嵌套 Tx，不解密、不调用外部 Usage/网络。来源 Store、Tx 或安全事实不匹配即失败；不得仅信见证布尔值而不读本域行。

错误沿现有闭集：非法值 INVALID_ARGUMENT，缺依赖 DEPENDENCY_UNBOUND，伪造/不匹配证据 FORBIDDEN，损坏持久事实或不可用依赖保守拒绝。SQL、明文、密文、nonce、原 request 和未清洗原因不进入错误/日志。旧调用的 fault/原子性沿现有规则，不通过新增后台任务补写被拒的 Audit。

## 事务、失败与恢复

- checker 自身零写入；只在实际 Append 调用期间消费见证。相同 Tx/Entry/Key 的 Audit 幂等检查仍可进行，跨事务重用或直接对历史 receipt 手工 Append 必须失败。
- mutation 的 checker/Audit 任一步失败，canonical、receipt、Audit 全回滚；成功同 command 重放走原 receipt，不再要求已消失的临时见证，不重复 Audit。Unknown 保留既有 command identity/原提交事实，沿既有重试确认，不新建历史或额外写 Audit。
- resolve 的 checker/Audit/commit 未确认成功，不发布 SecretMaterial；既有明文清零保持。真实新读取再次运行权限、lease/metadata、解密和 Append，不复用旧 witness/resolution。既有 committed/rollback/仍未决 Unknown 行为不得变成“无 Audit 也返回值”。
- 只为 Project Audit 生成相应见证。System、Account secret 写入/读取、rotation、cleanup、旧准备/重放及 lease 生命周期语义保持；本卡不实现 Secret 项目停止、清理或 Usage provider。

## 待下发实现范围：8 个文件

| 类型 | 精确路径与限制 |
| --- | --- |
| 旧源码 2 个 | `internal/central/secret/write.go`：仅真实写入/receipt 后、Append 前的私有见证接入与必要 import；`internal/central/secret/lease.go`：仅真实 lease/grant/解密后、Append 前的私有见证接入与必要 import。不改原权限/锁/加解密/重试顺序 |
| 新源码 2 个 | `internal/central/secret/project_audit.go`、`internal/central/secret/project_audit_witness.go`：上述 checker、精确 Store/Tx 见证和只读校验 helper |
| 新单测 2 个 | `internal/central/secret/project_audit_test.go`、`internal/central/secret/project_audit_witness_test.go` |
| 新真实集成 2 个 | `tests/security/secret_project_audit_test.go`、`tests/security/secret_project_audit_fixture_test.go` |

既有 service/storage/contract、Audit、Project、Object、迁移和旧测试只读。需要扩大范围时先给具体失败与最小接缝，不能在本域旁路权限。一个实现者独占 8 路径；卡与代码冻结后交未参与实现的独立验收者。

## 验收与真实绑定边界

集成必须运行真实 Secret.Service、真实加解密、真实 Audit.Service、真实 PG 行与事务；可复用既有 security owned fixture。测试专用 Project/Usage ports 可在测试 schema 保存当前 user/session/project/lease owner/binding 状态，但必须严格核当前映射、gate、actor、held locks/同 Tx，并真正调用本生产 checker。它们只隔离未绑定上游，不能返回恒定 allow、替换 Audit appender 为成功 receipt 或冒称 D08/D09/D22 已绑定。Service resolve 正向使用真实 lease + 受控严格 UsageGrant；权限拒绝必须先于 material 发布。

新增测试至少覆盖下列有意义的结果，测试名以 `TestSecretProjectAudit` 为共同选择器：

- 构造 nil/typed nil/零值/不可比较 Store、跨 Store/过期 Tx、缺锁、漏/错 witness；对完整 Entry 的 actor/session/association/action/metadata/ordinal/cause/resource/producer/scope 任一替换均拒绝。不从无见证的持久 receipt/lease 单独 mint 审计权限。
- 真实 create/update/delete 成功，update purpose 不变/改变两支、delete 无 canonical 的正确后态、同 key 重放零新增 Audit；错误 changed_fields/result version/purpose/command digest/credential 拒绝，并断言 canonical、receipt、Audit 的原子回滚。
- 真实 acquire/read lease 的 resolve 成功；released/mismatched lease、失效消费绑定、错 purpose/ref/payload、解密失败、错 UseGrant subject/association、任意 resolution 拒绝。checker/Audit 失败不返回可读取 material，明文与加密材料不进入安全投影。
- 同事务内替换 Entry/Key、捕获 context 后跨事务/Store 重用、缺实际领域步骤的直接 Audit.Append 均失败；当前 Project gate/Session/Usage 在读取前撤销即拒绝。测试不能在 checker 内补锁或补授权。
- 真实 PG 受控 commit ACK 丢失验证 mutation 与 resolve 的 committed/rollback/仍未决 Unknown；mutation 保留原命令/receipt/Audit 唯一性，resolve 不返回未确证 material，后续新读取使用新 resolution。保留失败日志，不用单次缺行推断原 writer 终局。
- 既有 Secret System/Account、准备写/重放、lease/rotation/cleanup 相关回归保持；不因 checker 未配置影响旧非 Project 能力，不打开 service Audit lookup/Project cleanup。

实现自测至少为相关 Secret/Audit 单测、race、vet/编译和新增真实 PG 选择器；资源交接后补受影响既有 security Secret/Account 组合。固定输入、实际命令与原始日志交独立验收，按失败和改动影响扩展，不运行无关模块全树。当前只有规格静态检查，未运行 Go/PG/Docker，不写已通过行为声明。

后继由 Project 在 R3 固定后交付受限 `AuthorityDependencies.AuditFacts`、当前 gate/Actor 分派及真实 Secret CheckMutationInTx delegate，再做真实 Project+Secret+Audit 集成。Object checker 须等 S2 固定后接其 upload/transfer/维护真实调用点；Object 的 delete cause 是原 cleanup operation+ObjectID 的域分隔摘要，失败上传还有 writer/recovery 两种 ordinal，不能套用 Secret receipt。D10/D12 仍分别缺初始化专用授权/Owner/source 与真实 Object Audit 组合，本块不解除其产品集成门槛。

## 独立静审与采纳

2026-10-05，未参与本卡编写的 `skill_verification` 对冻结 SHA-256 `e78c72d0135efddbeac8e948c6fd2dafb9b557e06a708bf21b77939f6bbfdf43` 与固定 `49c6589` 的 21 个相关文件完成独立静审，通过，无确定阻断。构造、Store/Tx 见证、mutation 后态/旧 purpose、lease/grant/解密后 resolve 接缝均可在八文件范围实现，不需新增迁移或契约。8 个链接/fragment 与格式检查通过，未运行 Go/Docker。报告为 `/tmp/agenteam-secret-audit-static-uyjzyxxs/review.md`，SHA-256 `93c9619e372088bc34f289f533da233a3632d2383ce61171cdce9c7f95fdbec5`；本节持久保留结论，不依赖临时文件存续。

现有 Entry/Metadata 的 opaque 值语义已隔离 caller 可变 slice；实施仍须精确比较包含 Session 的完整 ActorDetails、包含 HTTPTraceID 的全部九个 Associations、Metadata 内容及全 AppendKey，不能使用省略字段的安全投影。Unknown 实测必须命中 mutation/resolve 自身的最终 Tx，不能把 PrepareWrite 的 nonce 预留提交计入，也不能以一次缺行推断原 writer 已终局。主线程采纳仅更新状态与本记录，行为正文不变；实现、正式 Project 绑定与运行时验收仍未完成。
