# D09 current Model Resolution 库级恢复卡

修订：rev1.1 技术规格保持；§9 精确 20 路径及 00017 已独立业务验收 PASS，主线程采纳并提交推送 `4295df7d51c1f171df78ab3f0d9cef2fd241a505`，远端同 SHA 已由主线程确认。固定验收输入为 `be0bd07b1dc1fcd91ad217c9bdbfe5a14003ce74` 加最终 20 路径，不是整个后续提交。作者 `recovery_handoff` 与独立验收 `restore_test_dependencies` 已停止源码、Go/Docker 和资源操作；[正式报告与原证据](../agent-team/current-model-resolution-verification.md)记录分版本作者 26 顶层/109 子例及独立 2 顶层/4 子例。规格接受提交 `e81b029cd65fb26cc8598955e32b961dac35cb85`、原被审卡 SHA-256 `9413b70e0d524cc3223bf6aec9d1f761237ec22e96f988a99c9fa5e6827052b6` 不变。§1–11 保留已审技术原文和当时待授权状态，当前状态以本页首及 §12 验收记录为准。生产 Resolution 仍 nil，真实 consumer/Invocation/Usage 等后继未交付，完整 D09 未完成。

必读 [设计技能](../../../.agents/skills/agenteam-design/SKILL.md)、[Go 技能](../../../.agents/skills/agenteam-go-development/SKILL.md)、[D09 设计 §4](d09-model-system-token-usage-design.md#4-正式规划授权与-secret-组合)、[Model Resolution 架构](../../architecture/platform-infrastructure/model-system/model-resolution.md)及已验 [Secret planned usage](recovery-d09-secret-model-usage.md)。本卡落实其中一个完整结果，不把整个 D09 或未来 consumer 的正式端口视为已实现。

## 1. 完整结果与真实前置

在受信 consumer 事实端口下，从 `current_selection` 的 `direct` 或 `platform.memory` 解析当前配置，持久化不可变 snapshot、consumer/owner binding，并在同一最终 Tx 通过 Secret 的正式 planned Acquire 建立 lease。相同解析单位重入得到原已提交快照和 canonical lease；并发、权限变化、事务失败与 Unknown 不返回未经确认的结果。全程只有 metadata/DB，不读取凭据材料、不发送 Provider 请求。

| 前置 | 固定事实与本卡边界 |
| --- | --- |
| C0 / System / Project 配置 | C0 `e6e94c4`、System `543511c`、Project `de00c610` 已验；现有四个 Resolver 方法仅为 contract，Service 尚未实现。配置、当前 Session/Project Owner 和 selector 数据可复用，Owner 不代替 execution/operation/consumer 授权 |
| Secret Model usage | `8ad6759dbb499ae1cfec1d47bcab75f1abb2f56d` 已验；DiscoverUsage/ApplyUsageInTx 可组合 Acquire，旧 Model lease wrapper 已关闭绕过。材料读取的 RequestID/typed Tx witness 不等于本卡的 Acquire 授权 |
| wire | text-v1 `9c72190` 已验；[structured-v1 正式卡](recovery-d09-openai-chat-structured-wire.md) `b629714` 的 10 源已独立动态 PASS，主线程采纳并提交推送 `be0bd07b1dc1fcd91ad217c9bdbfe5a14003ce74`、远端一致。本卡已核相对 `3e5d882` 只有该 10 源业务差量及正式 revision/profile 接缝；不将 wire 验收扩大为 Resolver 或真实 Provider 账号实测 |
| 生产根 | System 配置根 `457b197` 已验，归档 `3e5d882` 已接受；没有真实 positive Model ConsumerAuthority。根保持 Resolution 未装配、`ready=false`/503；本卡不改 app 或 HTTP |
| 迁移排期 | `6a9c04ee741edcf8722a5d828a05eb6e3bc55891` 已接受：主线程撤回 [R4](recovery-d08-lifecycle-runtime.md) 仅文档的 00017 预留，转留本结果；R4 待后续实际编号，不保证 00018。本卡只规划 `00017_model_current_resolution.sql`，尚无 SQL 写权 |

排期独立报告 `/tmp/agenteam-migration-reservation-review-8u9l64q6/review-freeze02.md`，SHA-256 `9a94da8ba692ae1e12110eeeb210da430bcd81b5012ff26cbd3ba3cf9f389973`。排期提交只改文档，源码基线不变。

structured 最终独立报告 `/workspace/agenteam-structured-wire-independent-v-jmkxen5r/final-report.md`，SHA-256 `702c30f4da9a1e074205d9d9e10058b995c5ad00dab9cd5b732e9511f0a6ed6b`。本卡不复写该验收；只沿其已接受输入固定 profile。

本卡不实现 `serving_snapshot`、其他平台 selector、`project_summary`、真实 consumer、Invocation/Usage/Runtime、Model credential read/release/retire、Provider I/O、Project cleanup、HTTP/root。Summary 初值及 Settings 产品决定仍待用户答复，不生成默认模型或假 generation。Object 退出修复仍中断，Artifact 16 源未提交；本结果不消费或恢复这些任务，也不解除其阻断。

## 2. 公共 API 与无环装配

保持 [C0 Resolver](../../../internal/central/model/contract/resolution.go) 四签名及所有公共 carrier 不变，由 `*model.Service` 实现：

```go
SelectModel(context.Context, identity.Actor, contract.SelectionRequest) (contract.SelectionResult, error)
DiscoverResolve(context.Context, contract.ResolveRequest) (contract.ResolutionPlan, error)
ResolveModelInTx(context.Context, foundation.Tx, contract.ResolveRequest, contract.ResolutionPlan) (contract.ResolvedModel, error)
ResolveModel(context.Context, contract.ResolveRequest) (contract.ResolvedModel, error)
```

在 `model.Authorizations` 新增可选 `Resolution *ResolutionAuthorizations`，新配置类型仅含 `Consumers contract.ConsumerAuthority` 与 `SecretService identity.ServiceRegistration`。`nil` 表示未装配，不影响原 System/Project 配置构造；所有 Resolve 方法明确 `DEPENDENCY_UNBOUND`。`SelectModel` 的 Human 安全目录分支使用原 Sessions/Projects，详见 §3。

非 nil 的 Resolution 必须完整：拒绝 nil/typed-nil（包括具名 nil Chan）consumer；通过正式 `ServiceRegistration.Actor` 投影验证 SecretService 注册有效且角色精确为 SecretService，不能反射其私有 state。验证角色所用无副作用 Actor 不授业务权，真正使用时仍以实际 owner ID/ref scope 派生并精确核对。构造复制配置；之后修改原结构体不改变装配。依赖为 opaque 接口时，只核可判定结构与调用结果，不声称非 nil 证明内部健康。

装配顺序为：同一个 Store → Account/Project 及未来真实 consumer provider → Model Authority → 原 SecretUsageRouter/Secret → Model Service。Authority 只持 Store、事实端口与实例 issuer，不持 Model/Secret Service，禁止可变全局注册表或构造后 setter。注册由受信组合者提供，Service 不自行注册技术角色。此卡仅在严格 fixture 中装配 consumer；默认生产根的 nil 依赖保持原样。

`New` 继续核 Model Store/Authority 同实例；Secret 为正式 opaque UsageOperations，不能猜内部 Store，跨 Store 的 Tx 必须由真实 `InTx/RequireHeldLocks` 拒绝。Resolution 启用时 Initialize 检查本卡 schema 的实际可用性，失败返回错误；不建 worker、不扫描或退休历史 lease、不把未启用 Resolver 变成既有 System 根的新初始化前置。

## 3. 选择、profile 与权限分层

`SelectModel` 返回 C0 的 ID/version 安全投影，不建 snapshot/preparing/lease。它没有 SnapshotID/LeaseOwner，不能伪造这些字段去调用 `ResolveConsumer`。当前仅支持真实 Human Session + 当前 Project Read；同时验证 Consumer 与请求的结构关系，但不以所填 execution/operation 声明其业务存在。其他 Actor 的 Select 当前无正式独立选择授权端口，返回 `DEPENDENCY_UNBOUND`。成功 Select 不是 Resolve 许可；Resolve 不调用它来替代 consumer 授权。

| 输入 | 当前结果 |
| --- | --- |
| direct | 严格沿 C0 SelectionRequest：显式 ModelRef、chat、合法 agent/tool 用途；只选 System 或当前 Project 的 Model。不存在/禁用/不支持时明确失败，不 fallback；其他 Project 不泄露其配置 |
| platform.memory | 沿真实 singleton 的 memory_id/version，必须 System、enabled 的 chat Model 与 enabled Provider，capabilities 含 json_schema。未配置返回明确 `INVALID_STATE`，不把必需 selector 表示为可选空结果 |
| 显式 Selection.Version | 新解析在锁后必须等于实际 selector version，否则 `RESOURCE_BUSY`；direct 不允许带版本。Select 的结果不能绕过后续切换 |
| project_summary / serving_snapshot | 有效结构也返回 `DEPENDENCY_UNBOUND`，不查“最近一个”、不填默认或借已提交普通 snapshot 作为 serving 证明 |
| 其他平台 selector | 本轮 profile/完整结果未交付，返回 `CAPABILITY_UNSUPPORTED`；不误报为已支持的 not_configured |

Select 的当前 Human 授权先于有区分度的资源错误；Resolve 的当前 consumer 授权也须先于向外暴露配置/准备记录是否存在。Discover 阶段读到的候选只是收集锁和依赖；必要时用包含当前 consumer 全部锁的短只读 Tx 先核权限，不能把 `ConsumerAuthority.Discover` 当授权。所有成功准备和最终 Tx 仍锁后重新验证；不以一次前置检查跨 Tx 放行。

错误保持安全闭集：坏结构/零 ctx 为 `INVALID_ARGUMENT`，缺装配为 `DEPENDENCY_UNBOUND`；当前授权失败保留正式 authority 错误，可见资源缺失 `NOT_FOUND`，disabled/必需 selector 未配置 `INVALID_STATE`，不支持的 profile/字段 `CAPABILITY_UNSUPPORTED`，锁后配置/plan 变化 `RESOURCE_BUSY`，持久同单位异义 `IDEMPOTENCY_KEY_REUSED`。不向普通错误/日志加入 snapshot/private plan、endpoint、credential 或 consumer 输入内容。

新 snapshot 只冻结已验 profile 子集：OpenAIChat/OpenAIChatV1/Chat；无 tools、parallel tools、reasoning 或 reasoning_efforts，input/output 均为 text，空 Provider Options/Parameters/RequestOverwrite/HeaderOverwrite。streaming 与合法 ContextLength/MaxOutput 原值保留。StructuredOutputModes 不含 json_schema 且满足 text-v1 闭集时固定 `openai-chat-text-v1`；含 json_schema 并满足 structured-v1 闭集时固定 `openai-chat-structured-v1`。platform.memory 只能后者。直接 chat 也按实际 capability 选 revision，不按未来单次 ResponseFormat 更换快照。

当前 C0 snapshot 没有独立 ProviderOptions/reasoning_effort 字段；已验配置策略本就拒绝非空 options/overwrites/parameters 和可选 reasoning levels。本卡继续明确拒绝非空/未支持配置及非空 ReasoningEffort，不能丢字段后声称支持、更不能自行扩 C0 或原配置策略。无 CredentialRef 是 C0 明确允许的 metadata 分支，快照与 binding 的 lease 字段均为空、零 Secret 调用；这不宣称 wire 已能无凭据发送。

## 4. 稳定身份、准备计划与三张本域表

`ResolveRequest` 没有 HTTP 幂等 key。本卡固定解析单位：model_call owner 的同一 Project + owner ID 只有一个单位；execution owner 按 Project + owner ID + consumer kind/purpose + canonical execution/operation ID 区分单位。Agent generation/compaction 因用途不同可分别绑定快照，但同 ref/owner 共用 canonical lease。此身份只用于查找/串行，不证明 consumer 存在。

使用本域 `model.resolve` CommandIdentity 作 rank-0 writer 锁/TransactionCause，owner IDs 为 Project 与 lease owner 的规范 UUID；command 区分 owner kind，key 为上述解析单位的固定版本 canonical digest。它不写配置 commands 表、不伪造用户提交的 CommandMeta。不同 model_call purpose/consumer 不产生第二单位，而是下列异义冲突。

持久 semantic digest 为版本化 canonical 私有标量 DTO：完整 consumer、purpose/source、LeaseOwner、原始 ModelRef/Selection（含是否带版本）/ReasoningEffort及稳定 initiator。Human 稳定身份含 UserID、不含 SessionID；AgentRun/Service 保留各自完整稳定角色/Project/Agent/Execution/Cause 身份。**不把第一次观测的 live Provider/Model/selector 版本、endpoint 或配置值写入 semantic digest**。新 Session 必须重新规划和当前授权；旧内存 plan 的完整 Actor binding 不可移用。相同单位异义返回 `IDEMPOTENCY_KEY_REUSED`，不复用别人的快照。

规划中的唯一全局迁移为 `db/migrations/00017_model_current_resolution.sql`，只新增以下三张 `agenteam_model` 表及本域约束/索引；不改 00001–00016、配置 commands、Audit schema、Secret 私有表或跨域 FK：

| 表 | 必需数据与约束 |
| --- | --- |
| `resolution_preparations` | ID、唯一 resolution identity、Project/owner/解析单位索引、semantic_digest、稳定 initiator/consumer/request DTO、phase=prepared/committed、正 plan_version、稳定 SnapshotID、私有 draft_plan、created_at/updated_at；committed_at 仅 committed 有值。ID 均用既有 safe_id；首次准备后 SnapshotID 不换。prepared 不是 binding/lease/许可 |
| `snapshots` | 不可变 SnapshotID、ProjectID、完整私有 ConfigSnapshot DTO、首次 Provider/Model versions、snapshot digest、created_at；同域 nullable live ProviderID/ModelID FK 可 ON DELETE SET NULL，历史 ID/name/protocol/profile/revision/endpoint/capabilities/ref 全部留在不可变 DTO。配置删除不删除快照 |
| `snapshot_bindings` | 唯一 preparation/resolution identity、SnapshotID、Project、完整 consumer/稳定 initiator、owner kind/ID、可空 exact CredentialRef 标量与 LeaseID、created_at；同域 FK 关联 preparation/snapshot。credential 与 lease 全空或全有；owner/consumer/Project 与原准备一致。增加按 `(credential_id, credential_scope, credential_project, owner_kind, owner_id)` 查 canonical lease 的索引，及 Project 分页索引 |

JSON 格式各自有固定 `format_version=1`、严格字段闭集；draft_plan 至多 256 KiB，snapshot 至多 128 KiB，request/consumer DTO 至多 16 KiB。SQL CHECK 限 object/基本类型/长度/phase/null 关系，应用 loader 再完整核 UUID/正版本/摘要/C0 类型/跨字段关系。未知字段/版本、坏摘要、半空 ref/lease、同 owner/ref 存多个不同 LeaseID 均视为坏事实，不能随意取首行。序列化使用显式私有 scalar DTO，重建 opaque Scope/Ref/LeaseOwner；不能依赖公共安全 JSON/Format 反序列化签发 plan。

三个表的相关最终时间取同一次 DbNow；不使用客户端时钟决定提交/权限。fresh prefix 1..17 与 populated 1..16 升级均验证，原配置/Secret/Project 数据和约束不变。沿现有 Source 只写 Up；失败靠事务回滚/隔离恢复或另审前向修正，不写 Goose Down、不删持久身份。编号预留不是迁移已交付。

## 5. Discover、同 Tx 原子完成与重入

### 5.1 DiscoverResolve

先复制并校验请求/原 ctx/装配。有效未交付分支按 §3 拒绝；对支持分支，查本域 preparation/binding 候选，按同一解析单位取稳定 SnapshotID，尚无候选才预分配。构造 C0 `ConsumerRequest{Action: ResolveConsumer, Actor, Consumer, Resolve, SnapshotID, LeaseOwner, LeaseID}`；model_call 还以 owner ID 填 exact CallID，execution 不造 CallID。消费者必须证明实际 execution/operation/原始 ModelRef 或所用 selector、Project gate、owner/call 归属和当前输入事实；不能只检查 owner 字符串、Actor 结构或 Project Owner。

Tx 外读取相应配置/canonical binding 候选，确定 ref 与候选 LeaseID 后，才固定本轮完整 ConsumerRequest 并调用 ConsumerAuthority.Discover；用于授权前置的无 LeaseID 候选不能沿用为后续 Acquire 的 consumer plan。生成私有 draft；需要 ref 时构造精确 `UsageRequest{Actor: SecretService(owner.ID, ref.Scope), Ref, Purpose: Model, LeaseOwner, LeaseID, Action: AcquireLeaseUsage}`，RequestID/ReferenceOwner 为空，交正式 Secret.DiscoverUsage。此时上下文仅携带 Model 私有候选计划，尚不是 Tx witness 或权限。完成一次全量锁 union 后，准备 Tx 重读当前 consumer/config/canonical lease 并调用真实 Consumer.ValidateInTx，才插入或更新本域 prepared row；不在此 Tx 获取 Secret lease 或返回 ResolvedModel。

同单位 prepared 重入首先核 semantic。未变计划复用原 SnapshotID/候选 LeaseID/plan_version；配置映射或 canonical lease 已合法改变时，整轮重新 Discover，在原 writer 锁下递增 plan_version、替换 draft，SnapshotID 保持。新的 CredentialRef 可有新候选 LeaseID；同 ref/owner 已有已提交 binding 时必须换用其 canonical ID。旧 plan 因版本/映射不符失效，不能持高序锁现场补规划。首次/更新准备提交未确认均零 ResolutionPlan 返回；重试须先经过同一个原 writer 锁再读真实准备状态。

最后只以自己的 `contract.PlanIssuer` 签发 C0 ResolutionPlan，完整 Actor 的 ResolveBinding 与 mapping 分离。mapping 包含 preparation identity/semantic/plan_version、冻结配置或原 committed snapshot digest、SnapshotID、ref/lease、Provider/Model/selector versions、**本次** consumer mapping；plan 包含真实 ConsumerDependencies、Secret UsageDependencies 及完整复制锁集。持久 draft 保存配置/身份事实，不永久固定 Session 或 opaque consumer plan；已 committed 重入以原不可变事实加本次当前授权投影签新 plan，不更新历史 snapshot 来适配新 Session。持久 JSON 不保存闭包、issuer 或可重建授权的万能 token。

### 5.2 ResolveModelInTx

由外层 consumer 先取得 `plan.RequiredLocks()` 与自身 input capture 所需锁的完整 union。Model 不开嵌套 Tx、不 Acquire/升级/补锁、不 Discover、不网络或解密；只用 `InTx` 和 `RequireHeldLocks`。零/过期/外 Store Tx、缺锁/弱模式沿 D03 错误与 poison 原义。

依次核原 ctx、自己的 plan issuer、完整 request binding/mapping、持久 preparation/plan_version、当前 consumer 正式 Validate、当前配置或已 committed 原 snapshot。任何 stale plan/映射变化返回 `RESOURCE_BUSY` 并回滚，不在同 Tx 换 snapshot/lease。

首次完成在该 Tx 插入 immutable snapshot 与 exact binding，将本域 row 标为 committed；只有这些真实同 Tx 行/持锁/consumer 验证成立后才建立 §7 私有 witness 并调用 `Secret.ApplyUsageInTx(AcquireLeaseUsage)`。mapping 绑定固定 draft/version 而非这次 phase 投影；允许 prepared→committed 的证据只来自本次包内 exact-Tx 变更链或已核的既有完整 committed 事实，不能把任意 phase 更改当签发依据。Secret 失败、outer callback 失败或 D03 poison 时，snapshot/binding/phase/lease 与外层 input capture 一起回滚；此前独立 prepared 行可以保留。没有 secret ref 则不造 lease，其他原子性相同。返回对象由 private DTO 重建并 deep-clone、通过 ResolvedModel.Validate。

InTx 的返回只是外层事务里的 provisional 值；不得发布、发请求或将其称为已提交。在外层 Unknown 时由实际 caller 保留原 cause/attempt 并沿自己完整原 writer 确认，本方法不伪造外层 CommitResult。

### 5.3 ResolveModel 与已提交重入

便利 wrapper 只编排上述 Discover → 同一 Store WithinTx 一次完整 AcquireAll → InTx；原 ctx 贯穿，不新开后台确认/延长预算。最终只在真实 `Committed` 且交付前原 caller ctx 未取消时返回 clone。取消后零返回，已提交行不伪称回滚。

已 committed 的同单位重入先重新当前授权和完整 semantic 校验，再返回原 snapshot/binding；不从 live 配置重建，不因 Provider/Model disable/delete、名称/endpoint 更新或 selector 切换改历史配置。后来的新解析单位必须使用当前可用配置。旧 Session 的 plan 不能借新 Session 直接使用；新 Session 经当前正式授权重新 Discover 后可查询同一稳定主体的既有结果。

有 credential 的重入仍通过 planned Acquire 核 exact canonical lease/当前 ref/owner/purpose，没有“已有 snapshot 就跳过 lease 确认”的路径。已 released 的旧 ID 返回明确 `INVALID_STATE`，不得生成新 ID复活该单位。生产 retire/read 端口未绑定时不猜“lease 过期”，也不自动释放已提交 lease。

## 6. 完整锁与 canonical lease

每个准备或最终 Tx 的 union 由实际依赖决定，按 D03 排序去重/最强模式，至少包括：

- `model.resolve` 原 Command EX；Human User SH（适用时）和 `model-references` SH；platform.memory 时 `model-platform-selection` SH。
- 当前 Project gate SH，consumer 必需的 Agent、Execution/Operation 及其私有事实锁；某 provider 要更强模式则取并集，不降级。
- 当前/冻结 identity 的 Provider、ModelConfig aggregate SH；Secret Acquire 的 CredentialRef EX 与精确 `secret-lease:<LeaseID>` ReferenceRecord EX，均取 Secret 返回的完整锁而非手抄子集。
- 本域 preparation/snapshot/binding 记录及 owner/ref canonical 查找的 ReferenceRecord EX。记录 key 使用固定 namespace + canonical UUID/digest；不会引入新 D03 lock kind 或无界全域 EX。

`model-references` SH 与已验 Model 删除/替换 EX 串行；snapshot 不是长期 Agent/summary 配置引用，不往既有 references 索引造 owner。live FK 清空只影响可选 live link，不能改变 snapshot DTO 或 lease。

同 `(CredentialRef, owner)` 下已有 binding 的全部记录必须一致指向一个 LeaseID；读取的候选在 CredentialRef EX + 本域 owner/ref EX 下重新核对。owner 已映射到不同 Project 或不相容 Agent/execution 身份时拒绝，不因 System ref 相同借用其他 Project 的 lease。两个尚无 binding 的准备可各有候选 ID，但最终只有一个成功；另一个发现 canonical 已被建立必须整 Tx `RESOURCE_BUSY`、零第二 binding/lease，再 Tx 外重规划复用该 ID。不能用 `ON CONFLICT DO NOTHING` 隐藏冲突或只接收 Secret 返回的另一 ID。不同 model_call ID 使用各自 lease；相同 execution 跨用途/快照复用同 ref 的 canonical lease。

## 7. Model Acquire 的私有 exact-Tx witness 与路由

新增内容属于 Model 自有权限，不扩 Secret/C0/Project contract。现有 [Model Secret authority](../../../internal/central/model/secret_authority.go) 只实现配置 reference，现有 [router](../../../internal/central/model/secret_router.go) 的无 Purpose grant 一律 fallback；本卡仅补 Acquire 所必需的三个接缝：

1. `Authority.DiscoverUsage` 对 Model Acquire 仅接受本实例 Resolver 放入包内私有 context 的 exact candidate request/plan，返回使用原 usageIssuer 签发的完整 UsageDependencies；没有该私有准备上下文时 `DEPENDENCY_UNBOUND`，不能根据 Service+UUID 签发。reference 两分支保持原实现；read/release 明确 unbound。
2. `Authority.ValidateUsageInTx` 核同 Authority/Store/exact Tx、完整 UsageBinding/私有 mapping、全部 RequiredHeldLocks、真实同 Tx preparation/snapshot/binding 和当前 Consumer.ValidateInTx。仅这条正常内部调用链可以建立一个包内不可导出的 Acquire witness；绑定完整原 ResolveRequest/Actor、ConsumerRequest/Dependencies、prepared version、snapshot/consumer/owner/ref/lease/request、SecretService Actor 与 exact Tx。候选 context 不能充当此 witness；外 Store、另一 Tx、回调已结束、错 request/actor/owner 或已修改 mapping 一律拒绝。
3. `SecretUsageRouter.AuthorizeLeaseInTx` 的签名没有 Purpose，**只在持有上述已验证的 Model Acquire witness 时**走 Model grant，且再核 exact action=AcquireLease/actor/ref/owner/Tx/本域事实；错 witness 拒绝、不回落。无 Model witness 时保持原 fallback 的 Account/MCP 判断，尤其 execution owner 不能被猜成 Model。Model grant `Consumer=Model`、Subject 为 ref scope 的原受限 SecretService Actor，RequestID 为空；真实业务 initiator/Project/Agent/operation 仍保存在 Model binding，System credential 不借 AgentRun 冒 System Scope。

实际 Apply 的 context 是两阶段私有载体：先有仅可验证的 prepared 调用信息；Secret 调到 Model ValidateUsageInTx 并验证成功后，后续同一个同步 Apply/leaseGrant 才能消费 exact-Tx 验证见证。不能向外导出 mint 函数、以一个 bool 代替事实，或仅在 Resolver 提前塞“已授权”而省去 Validate。若实现需要共享一个私有指针传递验证结果，该指针限定单次 Apply、原 Tx、同步调用，使用结束即失效，不存在跨请求全局 map 或跨 Tx 重放。

此 witness 不是 [Secret planned read](recovery-d09-secret-model-usage.md) 的 RequestID/Audit witness。本卡不开放 ReadLeaseUsage、不给 Invocation 编号或材料；无 lease mutation Audit 的既有 Acquire 不增加伪造的 model.invoke/secret.resolve Audit。原配置 reference 的 typed Audit/Event 及 Account/MCP RequestID 省略编码与行为保持。旧 Model public Acquire/Release/Read wrapper 仍遵守 PreparationRequired 与既有更早错误优先级，不以本卡 witness 放行 legacy。

## 8. Unknown、取消与回收边界

准备和最终 Tx 分别使用自身的原 TransactionCause/AttemptID。真实 Unknown 必须返回零 plan/ResolvedModel 并保留原标识，可沿现有 `UnknownCommandError` 安全投影；确认错误不能覆盖成另一个 Unknown 或把旧 snapshot 当本次新 binding 已提交。此错误名不表示写入配置 commands。

新一次同请求重入先以同一原 Command EX 和本次所需完整 union 等待旧 writer 终局，再按 canonical prepared/binding/snapshot/lease 事实重验。缺行只有取得原共享 writer 屏障且确认 Tx 本身已结束后才能说明未提交；锁忙、预算耗尽、确认 Unknown 或未证明终局的 NotCommitted 均不能提供结果。D03 的业务 NotCommitted 不自动证明旧 PG writer 已释放锁，不修改其公开分类。

本结果无自有后台 worker/ProcessGuard 或材料/网络句柄，不实现全局 Drain/Joined。公共等待结束并不宣称 SQL writer 或后继 consumer 已 join；实际 Store/外层事务所有权沿现有协议，后继 Model Runtime 仍须完整覆盖。重试使用原 ctx 剩余预算，不 `WithoutCancel`、不另开保活 goroutine、不发请求或清除持久绑定。普通 mutex 与正式 Store 结果装饰可验证这条库边界，不恢复被中断的 Object 网络/ROLLBACK 方案。

rotation 只改变 Secret 当前 value；snapshot 不包含明文或 value version，绑定/ref/lease 不因轮换重写。本卡可验证真实轮换前后稳定引用，但下一 Invocation 的实际材料读取由既有 Secret read + 未来真实 Model read provider 完成，不能把本卡零材料当作已验生产发送。

execution lease 终局由 D22 真实 execution/join 负责；model_call 由未来 Invocation 的所有材料/发送/响应 actual join 后退休。此卡只保留 prepared/committed 事实，不按超时、取消或缺 consumer 自动 Release、TTL 删除或伪造 terminal。Project stop/cleanup、孤儿 prepared 的完整清理与根 Runtime 装配列为后继责任；因此库级接受后仍不能在生产安装 Resolution provider。

## 9. 精确候选写入范围

共 **20 路径：9 生产、4 相邻纯测试、6 集成测试、1 迁移**。以下只是待采纳范围，当前没有业务/SQL 写权。所有“新”均以固定 `be0bd07` 核对；实施前确认相关文件无活动所有权，之后新增的已验依赖差量另审。C0、Secret、Account、Project、D03、adapter、app/HTTP 和旧测试均只读。

| 路径 | 必要改动 |
| --- | --- |
| 旧 `internal/central/model/authority.go` | optional ResolutionAuthorizations、复制配置、正式 consumer/SecretService 依赖验证及私有 issuer |
| 旧 `internal/central/model/service.go` | Resolver 装配状态/启用时 schema 检查；nil helper 含 Chan，原配置构造与根语义保持 |
| 旧 `internal/central/model/secret_authority.go` | 保留 reference 分支，最小分派 Acquire 到新私有实现，其他 lease action 继续 unbound |
| 旧 `internal/central/model/secret_router.go` | 精确 Model planned Acquire 与无 Purpose witness grant 路由，fallback 字节/语义保持 |
| 新 `internal/central/model/resolver.go` | 四方法、当前权限/准备/同 Tx 完成/便利 wrapper 与确认后交付 |
| 新 `internal/central/model/resolution_plan.go` | 稳定身份/semantic、完整 mapping/lock union、opaque plan 与私有 DTO |
| 新 `internal/central/model/resolution_store.go` | 三表严格读写、canonical lease/版本 CAS/原 writer 重入、同域 live links |
| 新 `internal/central/model/resolution_secret.go` | candidate/已验证 exact-Tx witness、Acquire grant 与实际事实校验 |
| 新 `internal/central/model/resolution_policy.go` | direct/platform.memory 选择、已验 profile/revision 闭集及不可变 snapshot 构造 |
| 新 `internal/central/model/resolver_test.go` | 构造缺依赖/typed nil/原错误、取消与无隐式授权 |
| 新 `internal/central/model/resolution_plan_test.go` | 稳定主体/新 Session、不同 owner/用途语义、deep-copy/错 issuer/mapping |
| 新 `internal/central/model/resolution_secret_test.go` | 单 Tx witness 生命周期、无 Purpose 路由及 Account/MCP fallback 反例 |
| 新 `internal/central/model/resolution_policy_test.go` | profile/revision/capability 闭集、显式拒绝未支持字段，原配置策略不变 |
| 新 `tests/model/current_resolution_fixture_test.go` | 真实共用 Store/Account/Project/Secret/Model + 严格 consumer 事实 fixture；精确 Tx/锁/输入观测与有界结果装饰 |
| 新 `tests/model/current_resolution_selection_test.go` | 安全 Select、真实 selector/replay/配置变化/轮换边界 |
| 新 `tests/model/current_resolution_atomicity_test.go` | outer input capture/快照/绑定/Acquire 原子性与 canonical lease 并发 |
| 新 `tests/model/current_resolution_authorization_test.go` | 当前权/consumer 事实变化、完整低序锁、same DB different Store/witness 反例 |
| 新 `tests/model/current_resolution_unknown_test.go` | 准备/最终 Unknown、取消交付与原 writer 普通锁竞争；不新增网络代理 |
| 新 `tests/model/current_resolution_schema_test.go` | fresh/populated 17、严格存储事实与 live 删除/历史快照保持 |
| 新 `db/migrations/00017_model_current_resolution.sql` | 仅三表/本域约束索引，Up-only；正式实施授权后才可写 |

不能为兼容方便改旧测试断言、现有配置 policy、ConsumerAuthority/Secret 公共签名或 Root 注册。若 20 路径不足，报告固定编译/调用证据和最小差量，由主线程采纳后再写。

## 10. 验收结果与可观察证据

严格 consumer fixture 只隔离尚不存在的 D22/Memory/Tool canonical 事实：在测试专属 schema 建真实 execution/operation/owner/input/gate 映射，Discover 签自己的 opaque plan，Validate 在 exact Store/Tx 下 RequireHeld 并读实际行。Project Owner/Session/gate、Model 配置、Secret 加密存储/lease 使用已验真实服务；不得返回万能 allow，不把该测试 provider 宣称生产 consumer。AgentRun/技术 Actor 正例须明确其身份与事实来源。

| 固定新真实顶层 | 最低辨别力 |
| --- | --- |
| `TestModelCurrentResolutionSelection` | Human Select 的 System/本 Project 安全可见集、非 Owner/撤 Session/他 Project；direct 不 fallback；真实 memory selector 的 required/unconfigured/version/profile；非 Human Select unbound，AgentRun Resolve 不借 Human Owner；未交付 variant 明确失败且零新表/lease |
| `TestModelCurrentResolutionAtomicity` | System 与 Project ref 各有真实 planned Acquire；同一个 outer Tx 的 fixture input capture/snapshot/binding/lease 全部提交或全部回滚；Secret 错误/缺锁 poison 即使被 callback 吞掉也不提交；zero-ref 分支零 Secret 调用；零解密/Provider I/O |
| `TestModelCurrentResolutionCanonicalLease` | 两独立准备并发共享 execution/ref：一方成功，另一原 ID 冲突零第二写，重规划复用 canonical ID；相同 model_call 重入稳定、不同 call 不共享 lease；released fixture 行拒绝复活。fixture 设置 released 只证拒绝，不冒充生产 Retire |
| `TestModelCurrentResolutionAuthorization` | 正常 Agent/Memory 严格事实；Discover 后原输入/owner/Agent/execution/operation/gate/Session 改变拒绝；当前授权早于存在性错误；错 issuer/删低序锁/弱锁升级/外 Tx/同 PG 不同 Store/过期 witness/篡 ref/lease/actor 全部确实命中目标检查，记录实际错误与 rollback |
| `TestModelCurrentResolutionReplay` | 同单位同语义/新 Session 新 plan/restart 新实例重入；旧 plan 跨实例拒绝；已提交 snapshot 在 disable/delete/selector/endpoint 变更后不变，新单位取当前配置；异义冲突；轮换真实 metadata 后 ref/lease 稳定而本卡仍零材料/read；Release/Read/Retire unbound |
| `TestModelCurrentResolutionUnknown` | 准备与最终分别真实库 commit/rollback 后结果装饰 Unknown，保原返回 attempt/cause、零返回、按后态正确重入；普通第二 Tx/原 writer mutex 持有时不能据缺行/旧 snapshot 返回，预算内退出，放行后确认；真实 Committed 后交付前 caller 取消零结果且 canonical 行仍在。区分装饰结果与真实 DB 锁/后态，不称物理 COMMIT ACK 丢失或原 ROLLBACK 持锁已验证 |
| `TestModelCurrentResolutionSchema` | fresh 1..17、populated 1..16 升级/失败事务无半表；坏 JSON/版本/摘要/phase/owner-ref-lease 组合拒绝；同域 live FK NULL 后历史 DTO不变；旧 System/Project/Secret 行与约束保持，无 Down/Secret 私有 SQL 生产访问 |

新纯测试覆盖构造零值与 named nil Chan、所有公开返回/plan projection 不可变性、稳定 semantic 与完整 Actor binding 的区别、profile 拒绝、候选不能 grant、已验证 witness 跨 Tx/请求不得复用、缺 Model witness 的 fallback 与 legacy 门禁。Account/MCP 的既有 request binding 字节不变；本卡不新增 RequestID 编码。

适用旧真实顶层原字节运行：`TestModelB01CRUDCurrentAuthorityAndCanonicalReplay`、`TestModelB01PlatformSelectionAndAtomicReplacement`、`TestModelB01SecretReferencesAndAllEffectsRollback`、`TestModelB01PreparedReferencesAndCurrentAdminAreRechecked`、`TestModelProjectSecretReferencesAndAtomicEffects`、`TestModelProjectPreparedAuthorizationAndReferenceMapping`、`TestModelProjectReceiptReadGateAndLegacySystemPlan`、`TestModelProjectAvailableDirectoryHasOnlyEnabledSafeUnion`，以及已验 Secret Model usage 六顶层和必要 Account/MCP lease 兼容组。原 `TestModelSecretRouterPreservesFallbackAndDoesNotGuessLeasePurpose` 与 reference 纯测试必须保持。新增 migration 不替代旧 schema/配置断言；不因新 scope 改旧强反例。

作者先冻结 9 生产给独立静审，再完成必要纯 unit/race/vet、integration compile/vet、两 cmd build。真实组按原 fixture driver 的 exact selectors、Go 1.27.1/offline/modreadonly、`-race -count=1 -timeout=6m` 与原预算执行；精确组在运行前固定，no-tests/skip 不算覆盖。复用未变输入的有效结果，不重复全仓构建或为排版复制全树。发生失败保留原输入/argv/env/exit/log，修后仅重跑受影响检查。

当前规格阶段不运行任何 Go/Docker/网络。后续真实资源必须主线程另授唯一窗口；只用 owned 标准 PG/MinIO fixture，记录实际创建 ID/nonce-label，两次 exact absent、既有基线不变、owned 进程/runtime 清零后交回。不得恢复已停止网络/ROLLBACK 探针或依赖未接受 Artifact/Object；普通结果装饰不扩写物理网络验收声明。

## 11. 开工与交付门槛

structured-v1 动态前置已接受；仍须独立采纳本卡、核最终依赖与 20 路径独占状态，再由主线程另授 00017/唯一作者/独立验收者。需要新增正向 consumer、材料读取/退休、Invocation/Usage 或服务根的场景，属于后继完整结果，不能以当前 fixture 代替。新表的全生命周期清理和生产根可用性仍未交付。

设计输入复用 `/tmp/agenteam-current-resolution-design-zmmmha4u/report.md`（SHA-256 `0c236afe24de726d631dc1bcc26369521446d1a67a0d0ee27054fd87ba88e4e8`）与 `/tmp/agenteam-d09-next-dependency-v-8f_3yzzz/report.md`（`494e4e8b9dbb19f5c8d7f2194abd1cad5b312dc88ecc166fda0a20c75a430292`）；两者是早先固定输入分析，不覆盖后来真实根或 structured 动态结果。本卡只声明规格自查，不声明独立通过、业务实现或动态验收。完成独立采纳和真实库级验收后，仍保留 Summary 待决、无 production consumer/Invocation/Usage、Project cleanup/root 未绑定、Object/Artifact 原阻断和完整 D09 未完成的限制。

## 12. 规格采纳与实施授权

主线程采纳 rev1.1 独立 STATIC PASS 并以 `e81b029cd65fb26cc8598955e32b961dac35cb85` 接受本卡，随后正式授权 `recovery_handoff` 转 backend_worker，在固定 `be0bd07` 上实施 §9 的 20 路径（含 00017）；本次不扩 API、路径或技术规则。`restore_test_dependencies` 已冻结独立计划，后续只审作者停写的生产/SQL 输入，真实 fixture 窗口仍须另授。规格采纳和实施授权不构成 Resolver、迁移或生产 consumer 已验收。

[规格验收及原证据](../agent-team/current-model-resolution-spec-verification.md)保留 rev1 两处错误码 R1、rev1.1 窄修与迁移排期三文档两处遗漏/修后 PASS。最终独立规格报告 SHA-256 为 `1f1946e347d10e3d91937fd165068220168469d6a0c9ee512adcc2f6b4b636a7`；仅将两处 `IDEMPOTENCY_CONFLICT` 对齐既有 `IDEMPOTENCY_KEY_REUSED`，不新增 Foundation 契约。排期 `6a9c04e` 已接受，R4 不预占 00018；本次归档不读取或改动活动 20 源。

structured wire 115 路径归档已接受推送 `9da1e2ba8f6a2b4d44d9b9f8213509e990956e23`，复用其已验前置。Summary 待决、无生产 consumer/材料读取退休/Invocation/Usage/Project cleanup/root 正向绑定、Object/Artifact 原阻断和 ready503 不变；完整 D09 未完成。

### 库级实施验收已采纳

主线程采纳最终 20 路径 manifest `60a278f7472cc62bad9a1d09c880d054e76c599ba97c9e56a0b1faa0e7a54a2d`，以 `4295df7d51c1f171df78ab3f0d9cef2fd241a505` 提交推送；9 生产与 SQL 保持独立 PASS 的 production-review-02 原字节。作者六新业务组按未变输入复用，完整 Schema 及旧 Model/Secret/MCP/Account 回归通过，合计分版本 26 顶层/109 子例；独立真实权限、竞争重验、exact-Tx witness 与四类事实回滚 2 顶层/4 子例通过。原 Schema checksum 失败及首次 Fault SQLSTATE 未观测、P1/P2 与 T1/T2/T3 的静态性质、各阶段输入和实际双清理见[正式验收报告](../agent-team/current-model-resolution-verification.md)，不写成一次最终输入全绿。

00017 已用于 current-resolution 库级三表，R4 后续仍由主线程实际排号，不默认 00018。生产根 Resolution=nil；本验收没有真实 consumer、serving_snapshot、材料读取/退休、Invocation/Usage、Provider 发送、Project cleanup 或 HTTP/root 正向绑定。Unknown 仅正式 CommitResult 装饰、真实后态与普通原锁确认，不扩大为物理 ACK 故障验收。Summary 待决、Object 原阻断与 ready503 保持；没有新增动态资源授权。
