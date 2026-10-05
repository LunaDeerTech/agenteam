# D09 Secret Model 精确 usage 读取与旧入口边界

修订：rev1.1，2026-10-05；API、行为、20 路径及验收规则不变。状态：**20 源已独立验收 PASS，主线程采纳并提交推送 `8ad6759dbb499ae1cfec1d47bcab75f1abb2f56d`，远端一致已核。** [正式报告](../agent-team/d09-secret-model-usage-verification.md)保留作者 20 顶层/85 子例、独立 2 顶层/7 子例、全部原失败及验证边界。业务基线 `4cc4726b5707127f4b50c7da7023a3cd1f0b7f2d`；实现者 `recovery_handoff`，独立验收者 `restore_test_dependencies`，两者已停止本卡源码/Go/Docker 写入并交回资源。零迁移，`00017` 仍仅为 R4 保留；生产 Model consumer/Invocation/Runtime/HTTP/root 不因本卡完成。

独立规格审查记录：`/tmp/agenteam-secret-model-usage-review-_9um2kj3/review.md`，SHA-256 `39f0260066e579264773d66eccdbba39b6ce3ba7c44f41648ee685daaa605adc`；被审 rev1 原稿 SHA-256 为 `7d6bb71bc236b8dd0dc102acaa21efb0d9b500cdb1081ad54121a9b3e21b9c97`。结论仅为规格 PASS，无实现、编译或动态通过声明。实施须保留审查重点：实际 purpose+owner 的精确拒绝（System+ModelCallOwner 旧合法路径不受影响）；新入口本地处理 nil/零 Service 与必需接口 typed nil（含具名 nil chan），不改共享旧 helper；旧 Model 用例迁入 planned 流后仍命中原强断言；Unknown 结果装饰不冒称真实后端 attempt/网络故障；生产 Model consumer/lease/Invocation/Runtime 与发送、actual join 仍未绑定。

## 1. 完整结果与依据

按 [D09 正式设计](d09-model-system-token-usage-design.md) §4，Secret 库提供一次精确 Model usage 的 planned read：完整规划及锁后校验成功，读取当前密文并解密，真实 typed SecretResolve Audit 与读取在同一短 Tx，只有提交确认成功才交付受控材料。与此同时，旧 lease/read 入口对实际 Model + execution/model_call 组合关闭，不能绕过新计划或复活已释放 lease。此结果可以独立验收，不需要 OpenAI wire、Object 退出修复或 Summary 初值决定。

必读 [AGENTS](../../../AGENTS.md)、[团队流程](../agent-team/README.md)、[开发计划](../development-plan.md)、[设计技能](../../../.agents/skills/agenteam-design/SKILL.md)、[Go 技能](../../../.agents/skills/agenteam-go-development/SKILL.md)；独立验收读[验收技能](../../../.agents/skills/agenteam-verification/SKILL.md)。本卡仅冻结上述已确定规则的工程接缝，不补造 Summary 默认值或消费者权限。

| 固定依赖 | 已有事实及本卡消费边界 |
| --- | --- |
| Secret、D03、Audit | 已提交的加密存储、稳定 ref/lease、`DiscoverUsage` / `ApplyUsageInTx`、短事务和 typed Audit；复用实际 `secret/usage_plan.go`，不新增 mutation 接口或查询其它域私表 |
| Secret Project checker | `7d7c50df0dafcdeaaf700dc2662a6013245bbb6f`，[验收报告](../agent-team/secret-project-audit-verification.md)；本卡同步扩私有 resolution witness 与 checker 的 RequestID 来源，保留原同 Store/Tx/完整 Entry 和本域事实核对 |
| Project Secret 窄绑定 | `81fe7427ceb4672247b3d30a51c10a2e2808ba04`，[验收报告](../agent-team/project-secret-audit-binding-verification.md)；真实 Project 当前 gate/SecretProducer 路由仍原样使用。该报告中旧 Outbox 首红未归因事实保留 |
| D09 已验能力 | C0、System 配置及 Project 配置 `de00c610da62cb77cc03efe7c3cc842cf81f1ba5`；[Project 配置卡](recovery-d09-project-configuration.md)。当前 Model Authority 只绑定配置 reference，`AuthorizeLeaseInTx` 仍 `DEPENDENCY_UNBOUND` |
| 后继未绑定 | 真实 Model ConsumerAuthority、Invocation/call/snapshot/lease 事实与 Runtime、Resolve、Usage、发送/释放的 actual join、HTTP/root 均不在本卡。测试专用严格 provider 不等于生产 Model consumer 完成 |

本卡不读取活动 wire/Object 源，不恢复已中断的 Object 实现或网络探针。wire 作者实际统计为 **13 顶层 / 36 子例（3 新 + 10 旧），16 为源路径数**；该进度不是本卡前置，也不构成本卡能力证据。Summary 产品问题继续待答。

## 2. 公共 API 与闭集输入

仅为既有 `UsageRequest` 追加一个字段，并新增独立读取接口；不扩 `UsageOperations`、`CredentialLeases`、`UsageAuthority` 或 Model contract，不导入 Model 实现：

```go
// internal/central/secret/contract/account.go：保留全部旧字段。
RequestID string // exact Invocation UUID，不是 HTTP trace 或 lease owner ID。

// internal/central/secret/contract/model.go
type CredentialUsageReader interface {
    ReadCredentialForUsage(context.Context, UsageRequest) (SecretMaterial, error)
}
// *secret.Service 实现该接口；沿既有 New/Initialize 装配。
```

`UsageRequest.Validate` 的新增规则：仅 `Action=ReadLeaseUsage && Purpose=Model && LeaseOwner.Kind ∈ {ExecutionOwner, ModelCallOwner}` 时 RequestID 必填，使用既有 `foundation.ParseID` 规则，要求非零、规范小写 UUIDv7。其它所有 action/purpose/owner 组合的 RequestID 必须空，原字段组合校验仍完整执行。新 `ReadCredentialForUsage` 只接受这一精确 Model read 组合；其它合法 usage 也不能借此接口读材料。RequestID 只定位业务事实，不授予权限。

`UsageBinding` 将非空 RequestID 纳入摘要；在原编码字段之后追加 `RequestID`，仅该新增字段采用 `json:",omitempty"`。空值不输出字段，原字段名、顺序、值和 Actor/Scope/owner 编码全部不变，确保既有 Account/MCP、Model reference 和 lease mutation 的绑定字节及 digest 不变。零 RequestID 的新 Model read 属非法请求，不借兼容规则放行。

新入口沿既有 Secret 构造器和已初始化 Keyring/Store/Audit/Usage 依赖；不存在另一个授权构造器、setter 或全局注册表。缺 `UsagePlanner`、真实 Usage 或其它必需依赖明确失败，nil/零值新入口不得 panic 或返回空成功。不改变旧入口的构造/错误优先级来顺带重构服务。

## 3. Discover、完整 union 与读取顺序

1. 结构校验后，在 Tx 外调用已有 `Service.DiscoverUsage`。它按 exact LeaseID 读取 lease，核 ref/owner/consumer/未释放，并取得 provider 的 opaque plan；这一阶段不能授权、解密或发起网络请求。沿私有 issuer、完整 request binding、mapping 与复制锁集封装，任何 RequestID 改动都使原 plan 无效。
2. 新 read 分配独立 resolution UUID，以既有 `secret-resolution` recovery cause 开一个短 Tx。一次 `AcquireAll` 取得完整 plan union：provider/consumer 的 User、Project、Agent、Execution、operation/call/Invocation 等实际 gate，加 Secret 的 Credential aggregate SH、`ReferenceRecordLock("secret-lease:"+LeaseID)` SH，以及 Project ref 的 Project SH；冲突时 EX 覆盖 SH。沿 D03 全局顺序去重，不在 Tx 内 Discover、分段补高低层锁或调用网络。
3. 同一 Store/Tx 先 `RequireHeldLocks`，再由已有 `validateUsage` 调用 provider `ValidateUsageInTx`，锁后重读 exact lease/ref/owner/consumer/released 与当前 metadata。mapping 变化整 Tx 失败；不换 Invocation、lease 或 ref 后继续。
4. 验证通过后复用 `leaseGrant(ReadLease)`，保留当前 scope、SecretService actor、owner/cause 和 purpose 检查。`Actor.CauseRef` 必须为 lease owner ID；Invocation 仅在 RequestID。不得用任意 Human/Project Owner 或“存在 lease”替代该完整验证。Model grant 的 RequestID 必须空，非空拒绝，不能悄悄清掉或反用作 Invocation 证明。其它 grant associations 按现有规则验证。System credential 的 grant Subject 限同 scope 的 SecretService；Project credential 的 subject 和当前权限继续交真实 Project/consumer 端口，不在 Secret 伪造 Owner/Agent grant。
5. 读取当前 payload，核 purpose/owner/scope，真实 AEAD 解密；在同 Tx 构造 typed `SecretResolveMetadata`、完整 Audit Entry 与 `SecretProducer/resolution/ordinal=0` key，Append 成功后等待 Tx 结果。Service audit subject 沿旧规则使用本次 resolution，不能用它替换原调用 actor 的 lease-owner 授权。

本卡只实现 Secret 对已绑定正式端口的调用与验证。未来 Model planner 必须以 RequestID 定位唯一 Invocation，并证明 call/snapshot/consumer/input digest+schema、Process/fence/current attempt/dispatch、精确 lease 映射及 consumer `credential_read` 的当前资格；禁止查“最新 call”或任一 active call。当前生产 Model provider 仍 unbound，不能为让测试通过扩成 Owner allow。新的 read 也不能替代原 reservation/Acquire 的提交确认。

## 4. RequestID 的同 Tx 私有 witness 与 checker delta

固定源的 `projectResolutionAudit` 仅保存 UseGrant，`checkResolutionAudit` 从 `grant.RequestID` 重建 Entry；新接口不能只改 Append 而漏改 checker，也不能将 RequestID 注回 grant 绕过来源规则。

在 `project_audit_witness.go` 增加私有 Model planned-read 变体：保存**已通过本次同 Tx `validateUsage` 的完整 UsageRequest 值、其 binding/mapping 与复制后的完整 required locks**等安全证明。该值仅由新 read 的真实成功验证路径创建；至真实 lease/grant/metadata/payload/AEAD 完成、Append 前，才加入既有 resolution witness。没有公开 mint/WithWitness，也不接受 caller context 自报“validated”。不得持有 plaintext、SecretMaterial、Keyring、Service、加密 payload 或 provider 的可变私有状态；现有 Store/Tx 身份与安全 formatting 保持。

checker 对新变体执行以下兼容 delta：

- 维持原 exact Store、活 Tx、完整 Entry（含 Actor Session/HTTP trace/全部 associations）、Key 和 resolution/lease/metadata/payload 本域事实核对。不能改用会省略字段的 Audit semantic digest。
- 复核新变体为合法 Model read，RequestID 非空；request actor/ref/owner/LeaseID/purpose/action 与原调用 witness 完整一致，重新计算 binding 一致、mapping 合法，复制锁集合实际仍由此 Tx 持有。缺少、降级或毒化任一锁即拒绝；checker 不 Acquire、不 Discover、不再次调用外部 Usage，也不解密。
- Model Entry 的 RequestID 唯一来自该已验证 request；UseGrant.RequestID 必须空。其余 associations 仍来自原合法 grant。篡改 request、绑定、entry 或跨 Store/Tx 复用都不能借现存 lease/任意 UUID 通过。
- legacy Account/MCP 等 resolve 继续原 witness 变体，RequestID 从原 UseGrant 取得，原字节及校验语义不变。实际 Model + execution/model_call 组合缺新变体必须拒绝，不能退回 legacy witness。mutation witness 与全部 mutation checker 规则只读不变。

本证明仅覆盖 Secret 实际本域调用，不把 Invocation 私表复制进 Secret。未来消费者事实仍由原 provider 在同 Tx、完整锁下证明；本卡不伪造生产 Invocation 记录。

## 5. 材料、Unknown、轮换与旧入口

只有 `Committed` 返回非零 SecretMaterial；实现沿现有安全复制/Destroy/Use 所有权，并在所有返回路径清零临时 plaintext。Validate/lease/metadata/AEAD/Audit 失败、取消、`NotCommitted` 或 `Unknown` 一律零材料，不能把“已解密”或 reservation 已存在当作可交付条件。保留既有 Secret/foundation 错误闭集；不在错误、JSON、日志、witness、计划或 receipt 保存材料/明文派生摘要。

新 read 的 Unknown 沿 `SECRET_COMMIT_UNKNOWN` / `foundation.CommitUnknown` / `Unknown` 返回，并在私有 cause 链保留原 `CommitResult.AttemptID()` 与 `Cause()`；可通过 `errors.As` 到含这两个方法的 error 结构接口取得原值。普通 formatting/JSON/slog 仅安全码/状态。不复用会丢原 cause 的旧 `service.go:commitError`，也不修改该共用 helper 的 Account/MCP 行为。该封装放新增 `model_usage.go`；不新增公开恢复构造器或材料 Lookup API。

本入口不自动重读、重放材料或在 Unknown 后补写 Audit。新的必要读取必须重新验证相同精确 Invocation 的当前资格，并用新的 resolution 执行真实读取/Audit；原 Unknown 仍是原 Unknown，不能由新成功 Audit 倒推旧 Tx 已回滚。生产 Runtime 的原 reservation/writer 确认、取消后 actual join 及 release 责任仍未绑定。本库不发请求，因此“零材料”不被写成已验生产网络零发送。

稳定 ref 不固定 value version：合法更新/轮换后下一次已授权读取取得当前值，旧已交付 material 的内容不热换；失败轮换不绕过 read 检查，仍允许已验语义下其它合法当前值读取。

| 旧 public wrapper | 必须在实际事实处关闭的路径 |
| --- | --- |
| `AcquireCredentialLeaseInTx` | 原锁下读取实际 ref metadata；Purpose=Model 且 owner=execution/model_call，返回 `PreparationRequired/RESOURCE_BUSY`，在 lease 写入前拒绝，已有完整锁也不豁免；不执行旧 upsert 的 `released=false` 复活 |
| `ReleaseCredentialLeaseInTx` | exact 存储 lease 的 consumer/owner 确认该组合后返回同一错误，不 UPDATE；锁后重读仍需守住边界，不靠 caller 声称 purpose |
| `ReadCredentialForRequest` | 实际 lease 属该组合即要求显式新入口，零材料/零 resolve Audit；普通 legacy 分支锁后仍核当前实际组合，不能经早期非 Model 观察绕过 |

结构、初始化、Store/依赖失败等原有更早错误不被伪装成成功或强行改成 PreparationRequired。其余 purpose/owner 组合、尤其 MCP + execution 和 Account 两种 owner，保持旧规则。正式 planned Acquire/Release 继续调用已有 `ApplyUsageInTx` 私有核，不能转调被关闭的 public wrapper；已有 released lease ID 仍不得重新 acquire，Read action 仍不允许 Apply。lease 释放资格由真实使用者的 actual join 证明，不能用 ctx 取消替代；本卡不补造该生产 provider。

## 6. 精确实现白名单与兼容测试迁移

采纳后由主线程另授一个实现者独占 **6 个生产路径 + 14 个测试路径**。业务实现沿主线程对本结果的明确授权执行。必要的私有 helper 在下列源内组织，不为函数数量增加文件或生产接口。

| 生产路径 | 修改边界 |
| --- | --- |
| `internal/central/secret/contract/account.go` | 仅 RequestID 字段、闭集 Validate 和兼容 binding 编码 |
| `internal/central/secret/contract/model.go`（新） | 独立 CredentialUsageReader 接口及来源语义注释 |
| `internal/central/secret/model_usage.go`（新） | 正式新 read、同 Tx 验证证据、受控材料交付、原 Unknown cause 的私有封装 |
| `internal/central/secret/lease.go` | 三旧入口实际 Model 边界；可抽取本次所需的原读取私有核，Account/MCP 原顺序、字段和行为保持 |
| `internal/central/secret/project_audit_witness.go` | 新 Model proof 变体及真实调用点要求；原 mutation 不动 |
| `internal/central/secret/project_audit.go` | 仅 resolution 分支的新 RequestID 来源、完整 plan 锁和事实校验 |

`secret/usage_plan.go` 已有本卡所需 issuer、完整 binding、锁、Validate 与 mutation 内核，只读复用。`secret/service.go`、其它 Secret 生产源、Audit/Project/Model/Account 生产源、所有其它 contract、迁移、go.mod/go.sum、app/root、脚本与已冻结报告/证据均不在白名单。

| 测试路径 | 精确修改或新增责任 |
| --- | --- |
| `internal/central/secret/contract/model_test.go`（新） | 输入组合/RequestID 规范；固定基线 Account/MCP binding 字节与 digest 金样，Model RequestID 改变必改变绑定 |
| `internal/central/secret/model_usage_test.go`（新） | 新入口结构/依赖拒绝、原 Unknown attempt/cause 与安全投影；私有 proof 的完整绑定、锁复制与不可变性 |
| `internal/central/secret/project_audit_witness_test.go` | `TestSecretProjectAuditResolutionEvidence` 迁为 Model validated-request 证明；新增缺 proof/改 RequestID 等反例，保留原 resolution/owner/grant/metadata/payload/released 反例；其它原测试语义不动 |
| `tests/security/secret_model_usage_test.go`（新） | §7 新真实库风险组及严格 Model fixture provider；只能测试 schema 内造业务事实，不写生产 Model 表/stub |
| `tests/security/secret_nonce_test.go` | 仅将位置字面量 `&secretAuthority{auth}` 改为 `&secretAuthority{auditAuthority: auth}`，适配测试 provider 新私有字段；nonce 行为与全部断言不变，见 §8 |
| `tests/security/secret_common_test.go` | 必要的严格 UsagePlanner 测试实现、精确 lease/Invocation 准备及 Model acquire/read helpers；保留非 Model 原 helper 分支，不把旧 Model 测试改成 MCP |
| `tests/security/secret_access_test.go` | 仅 `TestSecretBindingsAuthorizationAndReadAuditBoundary`、`TestSecretReadUnknownNeverReturnsMaterial` 的 Model planned 前置/入口；保留 retained/current gate/AEAD/Audit/Unknown 强断言与原 MCP 分支 |
| `tests/security/secret_storage_test.go` | 仅 `TestSecretStorageReceiptAndCurrentLease` 的 Model acquire/read/release 前置；同 ID、不复活、稳定 ref、新旧材料、receipt/Audit 数量断言保持 |
| `tests/security/secret_cleanup_test.go` | 仅 `TestSecretProjectCleanupPendingReferencesBatchesAndGate` 的 Model lease acquire/release 前置；cleanup/reference/gate 强断言不动 |
| `tests/security/secret_fence_test.go` | 仅 `TestSecretReadSnapshotHoldsReferenceUntilAuditCommit` 的 Model planned read；原真实引用锁/提交顺序断言不动 |
| `tests/security/secret_rotation_test.go` | 仅 `TestSecretRotationCASReopenAndRetirement`、`TestSecretRotationCorruptionStopsWritesButKeepsUnrelatedAuthorizedRead` 的 Model read 准备/调用；原轮换和损坏结论不变 |
| `tests/security/secret_project_audit_test.go` | 仅 `TestSecretProjectAuditResolveFactsAndRejections`、`TestSecretProjectAuditResolveCommitUnknown` 的 Model planned 前置/入口；原篡改、事务原子性和零材料断言不动 |
| `tests/project/b03_secret_audit_fixture_test.go` | strict `bindingUsage` 的 Model planned 测试事实、完整锁及 exact Invocation 准备，原 Project/Audit 生产装配与已有协议工具不变 |
| `tests/project/b03_secret_audit_test.go` | 仅 `TestProjectSecretAuditBindingResolveGate` 的 Model planned 前置/入口；保留当前 Project gate、缺 User 锁、直接伪造、subject 边界及 Unknown 全部强断言，不调整未知旧 Outbox 失败 |

上述旧用例原先以 ModelCallOwner 走 legacy 正向，新契约下必须转到 planned 流，不能仅改预期为 PreparationRequired 而丢掉原业务覆盖。若某反例需保留“缺 User 锁才拒绝”的路径，fixture 应明确制造该缺锁计划并实际到达原校验点，不能由前置错误提前拒绝冒充原断言。不修改 Account 旧测试；MCP 既有子例字节与行为保持。若发现其它确切受影响路径或必须改动原断言，先向主线程提交原因/最小 delta 再扩白名单。

## 7. 验收、执行边界与交付

风险为高：权限、完整锁、跨域 Audit、明文交付与 Unknown 均须未参与实现的验收者独立核对。真实库测试使用原 owned PG fixture、真实 Secret/Keyring/AEAD、Audit.Service、持久行及事务；适用处用已验真实 Project Authority/Audit 路由。测试 provider 必须用测试 schema 保存并核 exact Invocation→call/snapshot/consumer/input/process/fence/attempt/lease 映射及当前 gate，不返回恒定 allow。它只证明 Secret 消费正式端口，不证明生产 Model consumer 或 Runtime 已实现。

新真实库验收按以下风险组组织；顶层名称冻结，子例可按实际覆盖组合，不能以单测替代实际 PG/AEAD/Audit：

| 顶层 | 必须取得的可观察证据 |
| --- | --- |
| `TestSecretModelUsagePlannedReadAndAudit` | System/Project × execution/model_call 合法 exact request 成功；真实当前值和同 Tx Audit 的 RequestID=Invocation；原 actor CauseRef=owner、Audit resolution 独立；System subject 不冒充 Human/Agent |
| `TestSecretModelUsageRequestAndPlanIsolation` | 零/错 Invocation、另一个合法 call、错 owner/ref/lease/input/process/fence/dispatch、外来 issuer/改 mapping/复制后改锁分别拒绝；Human Owner 不能替代 usage；仅“有 lease”无 provider 时 unbound；Apply(Read) 无 mutation |
| `TestSecretModelUsageLockMappingAndRevocation` | 普通第二 Tx 真实锁竞争；原完整 union 阻塞且尚无材料/Audit，释放后重验当前映射或撤销并拒绝；缺 User/Project/lease/provider gate 的计划不得被后补锁掩盖；锁后 released/ref/purpose 变化零材料 |
| `TestSecretModelUsageLegacyWrappersStayClosed` | 三 legacy 入口在实际 Model 组合、双方 scope/owner、已有完整锁时均 PreparationRequired，lease 行/释放标记/Audit 不变；released 行不复活。MCP + execution 正向及原 Account 隔离规则保留 |
| `TestSecretModelUsageAuditWitnessAndNoMaterial` | 新 request RequestID 被篡改、缺/错 proof、跨 Store/Tx、相同 PG 不同 Store、改 Entry/Key/association、缺锁 poison 均失败；合法 RequestID 源自真实验证。Audit 拒绝或 AEAD 失败不能有可 Use 的 material，不因 provider 返回非空 grant.RequestID 通过 |
| `TestSecretModelUsageOutcomeAndCurrentValue` | 真实 Committed 与真实 callback/Audit rollback；标准 Store 结果装饰的 Unknown 保留原 attempt/cause 且零材料。必要再读重新授权并产生新 resolution；更新/轮换后读当前值、已交付材料不热换，未释放 lease 持续保护 ref |

新 Unknown 用例可在真实 Tx 实际提交/回滚后，由遵循既有 Store 接口的测试装饰器返回 Unknown，并分别核真实 canonical/Audit 后态；必须明确这是**结果装饰覆盖**，不冒称网络 COMMIT ACK 丢失或原 backend 未终局。新测试不建立网络代理、截断网络或恢复任何被中断的 Object/ROLLBACK 探针。既有 Secret/Project Unknown 工具保持原样，动态回归是否执行及窗口由主线程单独交接；未执行不能算覆盖，旧独立证据也不能直接证明新 read 路径。

作者应完成精确 Go 1.27.1 的 Secret unit/race/vet、受影响 Model/Account/Project 编译与相关单测、integration-tag 编译/vet、两 cmd build。真实 PG 新组、上述已改旧组及只读 Account 回归沿原 driver `-race -count=1 -timeout=6m` 预算；需要 `-v` 时放 GOFLAGS，不改 driver、不机械扩大超时。只读 Account 回归至少含 `TestAccountUsagePlansCannotBeForgedDowngradedOrBypassed`、`TestAccountReadWaitsExactCurrentUserGateAndRejectsRevocation`、`TestAccountAuditFailureRollsBackLoginAndDestroysUnconfirmedRead`、`TestAccountExpiredLinkInvalidWhileActualMaterialUserKeepsLease`；原 System/MCP/Model reference 绑定金样和定向配置回归不得遗漏。

独立验收按作者有效覆盖去重，至少补 exact Invocation/issuer/锁后映射反例、同 Tx RequestID witness 篡改、实际锁竞争撤权及 Unknown 零材料；保留所有原失败、修复与固定输入。未修改语义及依赖的历史证据可注明复用，改动后的关键路径必须真实重验。Docker/PG/MinIO 仅在主线程另授独占窗口后执行，沿既定版本和脚本；资源按实际 created IDs 清零，既有资源 ID/name/labels 不变，自有进程/runtime 清零后交窗。不得访问既有服务或把 no-tests/skip 计成通过。

交付固定基线及全部实际输入 SHA、精确文件清单、实际命令/原始日志、新旧组真实结果、失败归因及未验范围；代码停止写入后交独立验证。采纳本卡不代表 D09、D08、D28/E01、生产 consumer/lease/Runtime 或根装配完成。需要扩大接口/文件、改变 Account/MCP 语义、增加迁移或依赖未实现事实时，暂停受影响部分并交主线程裁决。

## 8. rev1.1：一行测试编译兼容 delta

主线程于 2026-10-05 采纳此最小范围扩充：实现中的测试 `secretAuthority` 新增私有 planner issuer/once/反例控制状态，既有 `secret_nonce_test.go:26` 的位置字面量因此无法编译。只将该行改为显式 `auditAuthority: auth`，不引入测试全局 registry、不改 nonce 行为/断言；总范围由 6 生产 + 13 测试变为 **6 生产 + 14 测试（20 路径）**。公共 API、生产六路径、零迁移和全部验收要求保持。

原失败保留于 `/workspace/agenteam-secret-model-usage-author-l4eactgz/logs/compile-integration-01.log` 与同名 `.json`（实际 argv/env/输入 SHA/exit）；首次 `go test -tags=integration -run '^$' ./tests/security ./tests/project ./tests/account` 在 33.489s 以 exit 1 结束，唯一编译错误为上述 too few values，Project/Account 当轮 compile-only 通过。原作者输入已保存于该目录 `inputs/compile-integration-01/`，不以修后输入覆盖首次失败。本修订记录范围授权，不预宣称修后编译或动态验收通过。

## 9. 独立验收与提交归档

主线程采纳独立 V 报告 `9b184a0177fcda1ddc16b7d7981be0a477c70bc9740a30a9dcc991e7907a4087`；最终 20 源 manifest `ccc8044f7ac0c9ba2ac13ee00edbbd9c65ef042bdef9c43c712f223a32d006f0` 与已提交 `8ad6759` 逐项相同。见[正式报告](../agent-team/d09-secret-model-usage-verification.md)及[证据入口](../agent-team/evidence/d09-secret-model-usage-verification/README.md)。作者三真实组 20/85、独立真实 2/7 通过，最终纯检查、原命令/env/exit、双清理和输入恢复材料均已持久归位；其它包 no-tests 不计覆盖。

原 nonce 编译失败、基线 API 漏拷贝、S1 两轮优先级/available 原红和修复链保留；S2 修前只有静态确定问题，不冒称动态原红。首轮网络零统计为采集遗漏，原值和实际三网存活标签/双 absence 补证并存。Unknown 原 AttemptID/Cause 与物理 backend 分开；Human 缺锁可先在 Audit→Account Session gate poison，独立合法 Service 路径另证真实 Secret checker。

本次通过仅覆盖本卡 Secret planned read、RequestID 同 Tx witness/checker 与实际 Model legacy 边界；严格 fixture 不是生产消费者。Model consumer/lease/Invocation/Runtime、发送/actual join、HTTP/root 未因本卡绑定，无真实 Provider 账号测试。System Model HTTP 另按 `b4dd1e6` 的 17 路径实施、待独立验收；Object/Artifact 原阻断及完整 D08/D09/D28/E01 未完成状态保持。
