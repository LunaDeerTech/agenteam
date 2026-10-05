# D09 Model System 与 Token Usage 工程规格

修订 3，2026-10-05。设计部分已采纳；修订 2 的 R01/R02/C01 与官方 SDK 字段事实不变，本轮仅归位独立静态通过的 C0 精确 Go 形状与实施范围。继承设计基线 `da5caab58ad7b0b6c03f2120e3d8456d3d8b9613`、已验 D08 B02 `6319d033f089140b34de60374312ce57141e6fa2`；C0 固定编译基线为 `16595ad1e78e5283dfe85fb812095acde382edd1`。

当前范围由[主卡](d09-model-system-token-usage.md)限定：B01/C0 仅实施 8+2 纯契约及相邻测试，源码待独立验收；完整 D09 未开工、未完成。本文不证明 Provider 运行兼容，§13 的 Summary 初值、Jina/型号 conformance 及未来真实绑定继续待定。

依据：[开发计划](../development-plan.md)、[Model 架构](../../architecture/platform-infrastructure/model-system/README.md)、[配置](../../architecture/platform-infrastructure/model-system/model-configuration.md)、[解析](../../architecture/platform-infrastructure/model-system/model-resolution.md)、[Chat Runtime](../../architecture/platform-infrastructure/model-system/chat-model-runtime.md)、[Usage](../../architecture/platform-infrastructure/model-token-usage.md)、[D01 Model 契约](d01-contracts/model-tool.md)、[事务](d01-contracts/foundation.md)、[生命周期](d01-contracts/domain-lifecycle.md)、[W17–19/W42](d01-contracts/walkthroughs.md)、[D08 规格](d08-project-owner-design.md)。本文只细化这些规则；未闭合事项见 §13。

## 1. 交付边界与所有权

- System 管理员管理 System Provider/Model/平台 selector；Project Owner 管理本项目 chat Provider/Model/会议 selector。管理员不因此读取其他 Owner 的项目正文或 Usage。
- Chat、Embedding、Rerank、Image 是四个独立能力端口；不提供任意 URL、任意 JSON 推理代理。Agent/Meeting/Knowledge/Memory/Tool 的真实 consumer authority 由 D13/D14/D19/D21/D22/D24 分别绑定。
- D08 B02 `6319d03` 已提供真实 Human Owner/Project gate、创建/恢复与当前路径解析；同 Tx 需预持 User SH+Project SH。其 AgentRun 仍 unbound、普通 Service 不获 Owner 权限，B03 LifecycleFacts/Service validator/参与者适配尚未接通。D07 最终根装配按实际已验提交绑定，不能以本基线 app 代码覆盖后来改动。
- 不引入 Provider SDK 或升级依赖；Go 1.27.1、`GOTOOLCHAIN=local`、标准库编码/HTTP，加已验 D04 outbound、Secret、D03 PostgreSQL、D05 Object、D06 Outbox。Provider fixture 与真实供应商 smoke 是不同证据。
- 新迁移由 root 在 D07/D08 迁移验收后分配连续编号，本文只称 `NNNNN_model_usage.sql`，Up-only；不占 00012/00013，不修改任何既有 SQL。D09 一个迁移单写者负责本域表及 Audit CHECK 增量。

| 完整结果块 | 生产新文件范围；同名测试随所属块 | 真实前置与完成门槛 |
| --- | --- | --- |
| B01 纯契约与配置/用量存储 | `model/contract/{types,configuration,resolution,chat,nonchat,authority,references,events}.go`，`usage/contract/{types,query}.go`；`model/{authority,store,configuration,commands,references,resolver,secret_authority,audit_authority,events}.go`；`usage/{store,invocations,summary,query}.go`；本次新迁移；`tests/model` 对应 PG 测试 | 纯 contract（含本轮 planned read/serving variant）可独立定向验收后先行；配置/Usage 消费已验 D08 Human Owner/gate，真实验 Secret/Audit/Outbox/Unknown、T01–T08/T15；生命周期/未来 consumer 另待正式绑定，不以 fake authority 宣称生产接通 |
| B02 真实调用、协议及恢复 | `model/{runtime,call,stream,retry,recovery,lifecycle,media}.go`、`model/adapter/{openai_chat,anthropic_messages,openai_embeddings,jina_rerank,openai_images,sse}.go`、协议版本清单与合成 fixture；`tests/model` 独占新增测试文件 | B01 稳定端口、§6 官方 wire 补证；D04 真实私网 HTTP/TLS fixture；T09–T14/T16，真实未来 consumer 未绑定仍拒绝 |
| B03 管理/用量 HTTP 与根组合 | `model/http.go`、`usage/http.go`、`app/model.go`；新 `scripts/test-models.sh` 与既有 fixture 最小包列表增量；OpenAPI 对应模型/用量片段 | B01/B02、D07/D08 实际最终提交，T17/T18；§13 创建初值规则闭合后才完成全部 D09；不提前生产前端 D27 页面 |

相对路径的 Go 文件均位于 `internal/central/`。包分层：`model/contract` 可引用 foundation、identity/secret/object contract，不导入 Tool、Execution 或任何实现包；Usage DTO 引用 model/contract 的身份与 Usage 类型，Model 实现可依赖 Usage 服务，后者不反向导入 Model 实现。

必要旧文件增量逐项单独授权：

1. `secret/contract/account.go` 仅给 UsageRequest 增加 §4 的 RequestID、闭集 Validate/UsageBinding；新 `secret/contract/model.go` 声明独立 CredentialUsageReader，不改旧 UsageOperations 接口。`secret/lease.go` 仅旧 Model wrapper 的实际用途拒绝、共用安全读取核提取；`secret/usage_plan.go` 保持完整规划/私有 mutation 核，按新绑定接入；新 `secret/model_usage.go` 实现显式 planned reader。新 `secret/contract/model_test.go`、`secret/model_usage_test.go` 验 RequestID、旧 binding 字节兼容、精确调用与 legacy/MCP 正反。无新 Secret 表/owner kind，不扩大角色；若旧 Purpose=Model fixture 缺 planner/RequestID，只机械补正式输入并单列文件，不删原断言。
2. `identity/contract/identity.go` 及对应测试：仅登记 `ModelRuntime`（`model-runtime`）技术职责，用于原已持久 call/Invocation 的终态、租约收敛；无 Human/System/Owner 权限，不用于普通配置写入。
3. `audit/contract/{types,metadata}.go`、新 `audit/contract/model.go` 及闭集测试；`audit/service.go` 仅新增 optional Models verifier 与 Model action 分派；新 `audit/model_test.go`。模型调用本身不逐条写 Audit；§10 限定配置审计。
4. `app/{app,resources,health,diagnostics,security,object,outbox}.go` 仅最终根绑定、typed registry 与联合 join；以届时 D07/D08 已验版本为基线，不重写账户/邮件实现。`tests/testsupport/postgres/cmd/fixture/main.go` 仅追加 model/usage/tests/model 包，原 nonce、隔离、版本和每包 6m/race 不改。
5. D08 对 ModelRuntime/Secret/Outbound/Audit 的事实验证通过其正式注册端口在新 `model/project_adapter.go` 和根组合完成；缺正式端口时先报告，不直接跨域查 Project SQL。§13 的 Project 创建输入/初始化不在当前旧文件授权内。

## 1.1. C0 已采纳公共形状

下列正式 Go 声明对应已审 C0 稿 `bec826d7cef677352e36c8c3113f541c8a237b48044084d8a960d203ad0328eb`；字段名、可空指针、构造器、闭集与端口以这些源码为准。仅 marker ID 可为空 struct，业务 DTO 均有真实字段；接口无生产占位实现。

| 正式源码 | C0 责任 |
| --- | --- |
| [model/types.go](../../../internal/central/model/contract/types.go) | Consumer/ModelIdentity/ConfigSnapshot/ResolvedModel、能力、错误与非负 decimal-string TokenCount/nullable Usage |
| [configuration.go](../../../internal/central/model/contract/configuration.go) | Provider/Model 命令和安全投影、SelectionRef/Result、平台/Project 已配置状态；无 Summary 初始值载体 |
| [resolution.go](../../../internal/central/model/contract/resolution.go) | 显式 current_selection/serving_snapshot 请求与 Resolver 四方法，配置身份和新 lease 分离 |
| [authority.go](../../../internal/central/model/contract/authority.go) | ConsumerRequest/AttemptIdentity/InputIdentity/RetryPolicy、ConsumerAuthority、四 concrete opaque plan 与 issuer |
| [references.go](../../../internal/central/model/contract/references.go) | ReferenceChange/ReplacementPlan 与四个 References 方法；未来 owner canonical rewrite 仍需真实域适配 |
| [chat.go](../../../internal/central/model/contract/chat.go) | Message/Part/Tool/ModelRequest/Response/Frame、Chat 与 Stream 端口；部分/完整 payload 严格分离 |
| [nonchat.go](../../../internal/central/model/contract/nonchat.go) | Embed/Rerank/GenerateImage 独立请求与结果、有限向量/索引、Object 业务引用 |
| [events.go](../../../internal/central/model/contract/events.go) | typed 安全配置 payload；不注册 Outbox catalog/handler，不使中立 event 层依赖 Model |
| [usage/types.go](../../../internal/central/usage/contract/types.go) | exact Invocation 与历史安全身份、FieldSummary/Summary 的已知/未知分母 |
| [query.go](../../../internal/central/usage/contract/query.go) | Project Filter/Query/Page/分组 AggregatePage 与 Reader；不提供系统跨租户聚合或默认授权 |

四计划的 `New…(PlanIssuer, …Details)`、`Validate/Details/RequiredLocks/Matches` 只完成结构/不可变绑定：issuer 不导出，复制全部可变字段/锁，JSON 不能构造计划。绑定使用完整 Actor（包括当前 Session），不与持久命令的稳定主体摘要混用。服务仍须完整初始 union、D03 RequireHeld、锁后当前事实重验；C0 不证明已提交/权限/Secret 材料可读/actual join。JSON 参数只核大小、严格 object/深度/重复键/禁止覆盖，未核 profile/型号继续拒作为已支持声明。

## 2. 配置、默认与公开数据

所有 UUID、Version、Sequence、时间、JSON 数字沿 D01；版本正 int64，JSON decimal string。可累计 token/count 用非负 int64，SQL SUM 用 numeric 中间值，越 MaxInt64 返回明确范围错误而非截断/变 0。所有 JSON 拒重复 key、尾随值、未知字段及不有限数值；不 trim 用户正文。

| 对象 | canonical 字段及约束 |
| --- | --- |
| Provider | id/scope/project_id?/name/protocol/base_url/credential_ref?/provider_options/enabled/version/timestamps；scope 与 protocol 创建后不可变；System 可用四种能力协议，Project 只两种 chat 协议；name 为 1–128 Unicode 字符，不承担唯一身份 |
| ModelConfig | id/provider_id/name/model_id/type/parameters/request_overwrite/header_overwrite/enabled/version/timestamps；provider_id/type 创建后不可变；model_id 1–256 UTF-8 bytes，不按展示名推断供应商型号或能力；不同 Provider 同名合法 |
| PlatformSelection | 稳定 singleton ID/version；embedding、memory 必需；reranker、image 可空；全部仅 enabled System Model；memory 是支持目标 structured output 的 chat。首次尚未配置时明确 capability unavailable，不能写默认 Model 或宣称配置完整 |
| ProjectModelSettings | project_id、version、meeting_summary_model_ref；只接受当前可见 enabled chat；不要求 tool/reasoning/structured/image 能力；不得保存任意空值当合法已配置结果；创建初值适用 §13 |
| ResolvedModel | D01 字段仍为同一公开投影：snapshot_id 指不可变 Provider/Model/profile/adapter_revision/endpoint/config/稳定 CredentialRef/selection_version；credential_lease_ref 属于本次 consumer/lease owner 的独立持久绑定，不属于可跨 call 复用的配置身份。既有 serving snapshot 配新 call lease 沿 §4；不含明文，不把反序列化当权限 |

base_url 仅 http/https、无 userinfo/query/fragment，固定合法 host/port/路径；保留精确路径语义，不猜测追加 `/v1`。协议 profile 定义相对 endpoint 的拼接，发出前仍走 D04 当前出站策略；HTTP 仅显式配置且 D04 允许，不能放宽 loopback/metadata 禁令。Provider 未配置凭据是否可调用由已核 profile 的明确认证模式决定，不能隐式发匿名请求。

参数 64 KiB、overwrite 合计 64 KiB、Header 16 KiB 上限。Provider options 与 overwrite 用 profile 的 typed schema 白名单：只覆盖显式字段，最终覆盖顺序仍为 adapter → request_overwrite/header_overwrite。禁止覆盖身份、请求内容、工具 schema、URL、transport framing、Host、Cookie、认证材料或绕过 TLS/出站策略；认证 header 只能由 SecretMaterial 注入。纯生成参数可覆盖，最终请求再做 profile/能力/参数相容性验证，不以 overwrite 绕过 reasoning/structured 校验。

Chat 参数固定 context_length、max_output、输入 modalities、tool_calls、parallel_tool_calls、streaming、reasoning、reasoning_efforts、structured_output_modes。`reasoning=false` 禁存 effort；true 且 efforts 为空用 adapter 的已核默认，不保存虚构等级；非空必须属于该闭集。协议/型号不能满足显式要求返回 unsupported_feature，不靠 system prompt 模拟能力。

配置读取不给明文 credential；Project 可用模型目录只投影 enabled System chat 的 ID/名称/能力等选择所需字段，不暴露 System base_url、overwrite、凭据或其他项目配置。配置变更只影响下一次 Resolve，旧合法 Execution/consumer snapshot 不被 live enabled、参数或名称热换。

## 3. 存储、锁、命令与删除替换

新 schema `agenteam_model` 存 providers/models/platform_selection/project_settings/commands/references/snapshots/snapshot_bindings/calls/invocations/execution_usage_summaries/lifecycle_checkpoints。不创建 Agent/Execution/Meeting/Knowledge 业务表。不可为未来业务实体增加跨域级联 FK。

- providers/models 有 scope CHECK、版本 CHECK，Model→Provider 为 RESTRICT；Project provider/model 仅 chat。model refs 仅稳定 ID，显示名称不作唯一键。
- commands UNIQUE(scope,scope_key,actor_subject,command,key_digest)，保存 canonical-v1 请求摘要、安全结果及 Event/Audit 身份。Human 主体为 UserID，排除 Session/HTTP trace；模型配置中禁止明文 credential，故不另造敏感摘要/keyring。
- references UNIQUE(owner_kind,owner_id,role)，保存 project_id?/model_id/owner_version/reasoning_effort；kind 闭集 agent、project_summary、platform_selector，role区分 agent_model/approval_model/meeting_summary/embedding/memory/reranker/image。Agent上的 approval_model_ref 同属真实Agent配置，D10/D19负责其专用替代约束，不能因非generation而漏登记。它是与 owner canonical 同 Tx 更新的完整反向引用索引，不是异步投影。只有正式 owner authority 可注册/改写；未知 kind/缺 adapter 拒绝。
- snapshots 只保存 immutable identity/config JSON/稳定 CredentialRef，不复制输入/输出；snapshot_bindings UNIQUE(snapshot_id,lease_owner_kind,lease_owner_id) 保存 exact consumer/purpose、来源 variant/serving generation ID（仅该分支）、合法 LeaseID/ref 与创建事实；不固定 Secret value。calls 保存 consumer/snapshot及该 lease binding/input_digest+schema、固定 retry policy、process/fence/phase、accepted/finished/attempt ordinal；输入正文由原 consumer canonical 保存，不在 Usage 存第二份。
- invocation UNIQUE(logical_call_id,attempt_index)，真实 reservation、dispatch 状态、ProcessID/fence、开始/确认发送/结束时间、安全错误、nullable Usage、terminal boolean（初始false）及terminal version；live Provider/Model FK 可空 ON DELETE SET NULL，历史 identity ID/名称/protocol/model_id snapshot 保留。invocation 的 ProjectID 不从 live Provider 推导。
- execution_usage_summaries 主键(project_id,execution_id)，可重建版本、确定已发 attempt 数、dispatch_unknown 数、各 token sum/known_count/unknown_count；不写 D22 Execution 表。Meeting 源删除用独立 nullable source marker，保留已归属的历史 ID，不伪造第二条 Agent usage。
- lifecycle checkpoint 存 exact Project/cause/action/version、phase、recovery_pass、扫描水位与原 call IDs，不存过期 Human Session；Project 删除时随域事实清除，仅 D08 最小 receipt 留存。

索引：scope+id、provider_id+id、references(model_id,owner_kind,owner_id)、calls(project_id,phase,id)、calls(process_id,phase,id)、invocations(project_id,started_at,id) 与 project/consumer/execution/meeting/model_snapshot/provider_snapshot/purpose 对应过滤索引、lifecycle(recovery_pass,id)。不自动 TTL，不给 Usage 设计价格表或 token 估算。

统一顺序为当前权限 → 当前不可逆删除 gate → 同 key 摘要/已提交结果 → 新 mutation 的 gate/version/依赖。幂等重放仍当前有权，随后先幂等再 expected_version；归档后的历史结果可查，永久删除不可复活。相同 key 异义一律冲突；创建 ID 第一次计划即固定，Unknown 不换 ID/key。

所有 wrapper：Tx 外 Discover 全部映射 → 合并一次 AcquireAll → RequireHeld/锁后重读 → mutation+reference+Audit+Outbox+receipt 同 Tx。InTx 不调用 wrapper、不在锁内 Discover/补锁/取外部 I/O。计划含私有 issuer、完整请求绑定与复制锁集；mapping/version/引用集合变更整体 RESOURCE_BUSY/未提交，外层显式重规划，不能吞错自动补锁。

全局 `SystemConfigLock("model-references")`：普通 ModelRef 建立/更新/Resolve 用 SH；删除/替换 Model 用 EX。它与 command rank0、User/SystemConfig rank1、Project gate rank2、Agent rank4、Provider→Model→Credential→Execution 等 aggregate、record 一并初次收集。D06 Append 需要的 outbox-registration SH 也在这一次 union；`RequireHeldLocks` 只验证，不以重复 Acquire 伪装缺锁检查。

删除流程：Tx 外查 D09 references 并向实际 owner 获取替换计划/全部父 gate；EX 屏障后重查完整集合与版本，校验 replacement enabled/visible/type/effort，逐 owner 同 Tx 改 canonical+reference，然后删除 Model/Audit/Event/receipt。一个不兼容或 lifecycle 拒绝即全回滚，不分批部分替换。System chat replacement 必须 System chat；管理员只获 exact ref rewrite 能力，返回安全计数，不获得他人项目正文/通用 Owner grant。未知 owner provider 且有真实索引行必须 unbound；空索引只有在这个完整 canonical 索引的正式屏障下才是有效“无引用”。

Provider 删除须无 Models；释放其 Secret reference，不擅自删除仍被运行 lease 持有的 Secret。历史 snapshot/Usage 不阻止删除；live FK 变 NULL，快照不改。平台 embedding/memory selector 不能清空；reranker/image 可显式清空。必需 selector 当前无合法配置与数据库不可用是不同结果，不 fallback。

## 4. 正式规划、授权与 Secret 组合

以下是新 model contract 的稳定端口形状；类型构造器校验闭集并复制切片。实现局部 SQL/helper 不属于公共契约。

```go
type ResolutionSource string
const (
    CurrentSelectionSource ResolutionSource = "current_selection"
    ServingSnapshotSource ResolutionSource = "serving_snapshot"
)
type ResolveRequest struct {
    Actor identity.Actor; Consumer Consumer; Purpose Purpose
    Source ResolutionSource
    ModelRef *ModelID; Selection *SelectionRef; ReasoningEffort string
    ServingSnapshotID *SnapshotID; ServingGenerationID string // UUID；D13/D14 自有事实身份
    LeaseOwner secret.CredentialLeaseOwner
}
// C0 分别声明 Resolver / References；实际 ModelService 后续实现，各 Plan 为实例签发的 concrete opaque type。
SelectModel(context.Context, identity.Actor, SelectionRequest) (SelectionResult, error)
DiscoverResolve(context.Context, ResolveRequest) (ResolutionPlan, error)
ResolveModelInTx(context.Context, foundation.Tx, ResolveRequest, ResolutionPlan) (ResolvedModel, error)
ResolveModel(context.Context, ResolveRequest) (ResolvedModel, error)
DiscoverReference(context.Context, identity.Actor, ReferenceChange) (ReferencePlan, error)
ApplyReferenceInTx(context.Context, foundation.Tx, ReferenceChange, ReferencePlan) error
PrepareDeleteModel(context.Context, identity.Actor, DeleteModelRequest) (ReplacementPlan, error)
DeleteModelInTx(context.Context, foundation.Tx, DeleteModelRequest, ReplacementPlan) (CommandReceipt, error)
```

Source 必填闭集，不以零值默认。current_selection 分支保持 SelectionRef=direct/platform/project_summary，ServingSnapshotID/ServingGenerationID 必须空；Select 返回 selected（ModelID/version）或仅可选 selector 的 not_configured。Resolve 锁后核当前 selector 或 consumer 长期 ModelRef，不能用过期 Select 绕过切换；新 Resolve 配置变化重规划，已提交 snapshot 不变。各 concrete plan 仅安全 RequiredLocks 投影，不接受 JSON；绑定 Actor 全身份，持久摘要仍以稳定主体为准。plan 含 source 完整绑定、精确配置/来源映射、snapshot/lease ID、consumer plan 和 Secret UsageDependencies；同 preparing 重入查原 snapshot/binding/lease。错实例、错 Tx Store、零/过期 Tx、缺锁/弱模式保持 D03 拒绝/poison。

serving_snapshot 分支仅 knowledge_embedding 或 memory_embedding：ServingSnapshotID 与规范 UUID ServingGenerationID 必填，ModelRef/Selection/ReasoningEffort 必须空，LeaseOwner 必须 model_call，其 ID 为此次新的 logical_call_id。复用相同 DiscoverResolve/ResolveModelInTx，不另开事务：未来 D13/D14 的 ConsumerAuthority 必须从实际当前 serving generation 证明 Project/consumer、generation→snapshot/稳定 CredentialRef、原输入及新 call 归属；历史 SnapshotID 本身不是授权。Discover 收集来源 generation/current-serving gate、snapshot/binding、目标 call/lease 与 Secret 全部锁；一次 union 后重读全部映射，变化整 Tx RESOURCE_BUSY。snapshot 参数/profile/endpoint/selection_version 不随 live selector 或 config 改写；只新增本 call 的 snapshot_bindings+新 model_call LeaseID，同 Tx 记录，绝不使用或复活旧 call lease。来源不存在、Secret 无合法保留、当前权限不符或 provider 未绑分别失败；D13/D14 负责真实 generation 及其跨 call 的合法 Secret 保留，本模块不造该业务表/默认成功。

execution owner 可跨 turn/call 使用同稳定 ref：D09 从自己已提交的 snapshot_bindings/准备事实找回该 (CredentialRef,execution owner) 的 canonical LeaseID，并向 Secret planned Acquire 验同一 ID；无本域绑定才预分配。锁后另一准备已建立不同 ID 则整 Tx 回滚重规划，不忽略唯一冲突；已释放 ID 不复活。model_call owner 固定一次 logical call，重试复用该 call 的 binding，新 call 新 owner/ID。所有绑定 commit Unknown 均沿原 preparing/call writer 锁确认后才进入调用；不能因已有 snapshot 就跳过新 lease/binding 的提交确认。

`ConsumerAuthority.Discover(ctx, ConsumerRequest) -> ConsumerDependencies` / `ValidateInTx(ctx,tx,request,deps) error` 为正式受信 provider：request 闭集 resolve/invoke/credential_read/retire/finalize，绑定 Actor、完整 consumer、ResolutionSource/serving generation（适用时）、snapshot/call/invocation/lease IDs、原输入 digest、Process/fence、requested action。Discover 不授权；Validate 重读当前 Project/Agent/Execution/Meeting/Tool cause 与固定输入事实，不自行补锁。新业务 invoke/credential_read 必须当前 active；retire/finalize 仅原 accepted fact、exact join/death/terminal fence，可收敛而不能发出请求。尚未实现的各 consumer 缺绑定返回 DEPENDENCY_UNBOUND，不能由 caller 自报 active/Owner。

Consumer校验闭集：agent_generation/agent_compaction为agent，AgentID+ExecutionID必需、MeetingID仅实际关联；meeting_summary_initial/update为meeting，MeetingID+稳定OperationID必需、ExecutionID为空；approval_auto/image_generation为tool，真实OperationID必需，ExecutionID按原Tool事实而非伪造；knowledge_embedding为knowledge；memory_embedding/extraction/consolidation/reflection为memory且AgentID必需；rerank只knowledge/memory。非Execution consumer用自己的持久OperationID固定输入。所有分支ProjectID必需，request.Purpose与consumer.Purpose一致，Resolve的SelectionRef不得绕过对应System/Project selector。未知组合拒绝。

构造拆分 `model.Authority`（只Store、Session/System/Project及consumer/reference事实端口）与 `model.Service`（再接Secret/Audit/Outbox/Usage/Outbound）；先建Authority供Secret UsagePlanner与Audit Models verifier注册，再建Service/Runtime，避免Service互相构造循环。跨域dispatcher保留已验Account/MCP等原分支，按正式purpose/owner/cause分派，不以nil成功。首次Provider尚无行时Discover只规划stable ProviderID/ref scope；最终Retain须同Tx真实Provider行+原配置command证明该scope/ref映射和当前权限，不能以caller“新建”bool授权。

管理调用依赖已验当前 Session/System/Project authority。`ModelRuntime` 仅由根注册，CauseRef=原 CallID 或 InvocationID，scope=实际 Project；原 initiator/consumer 保存在 call 中，后台技术收敛不冒充该 Human。ModelRuntime 无权管理模型或替未来域批准业务。D08 注册 exact validator 后才允许相关 converge。

Secret 已有 Purpose=Model、owner=execution/model_call 和 DiscoverUsage/ApplyUsageInTx，不加新 owner CHECK。Provider reference owner 固定真实 ProviderID，reference scope 与 Provider scope一致；正式 Model UsageAuthority 读取真实 Provider/命令和 planned lease/call/snapshot，不按 owner 字符串前缀或任意 caller flag 授权。

新增的 Secret 公共读取能力只为本模块精确 use，不导入 model contract：

```go
// secret/contract/account.go：仅 RequestID 为新增字段。
type UsageRequest struct {
    Actor identity.Actor; Ref CredentialRef; Purpose Purpose
    ReferenceOwner string; LeaseOwner CredentialLeaseOwner; LeaseID LeaseID
    Action UsageAction; Retain bool
    RequestID string // exact Invocation UUID；不是 HTTP trace/lease owner ID。
}
// secret/contract/model.go：独立接口，不扩旧 UsageOperations。
type CredentialUsageReader interface {
    ReadCredentialForUsage(context.Context, UsageRequest) (SecretMaterial, error)
}
// *secret.Service 实现；D09 依赖该正式接口，未绑定即 DEPENDENCY_UNBOUND。
```

- UsageRequest.RequestID 仅在 Action=ReadLeaseUsage、Purpose=Model、LeaseOwner=execution/model_call 的组合必填，且必须规范 UUID；其他所有 action/purpose/owner 必须空。该 Model read 零 ID 结构校验失败，不回退猜“最新 call”。UsageBinding 将非空 RequestID 纳入完整摘要；零字段用省略编码，保 Account/MCP 原请求绑定字节不变。DiscoverUsage/UsageDependencies 继续私有 issuer、完整 request binding、mapping、复制锁集；ApplyUsageInTx 仍禁止 read action。
- 配置引用及 lease Acquire/Release 仍 Tx 外 DiscoverUsage→一次完整 caller union→ApplyUsageInTx；Resolve 只 DB metadata/ref，无解密/网络。新 snapshot/binding 与 Acquire 同 Tx，Unknown 未确认不开流。旧 reference 入口的独立 CheckReferenceInTx 可由真实 Model provider 对其管理的 Provider reference 拒绝 PreparationRequired，planned reference 私有核不调用该 legacy 分支。
- **旧 lease public wrapper 自身拒绝绕过**：AcquireCredentialLeaseInTx 从实际 ref metadata 的 immutable Purpose 判断；ReleaseCredentialLeaseInTx 从 exact 存储 lease 的 consumer/owner 判断。Purpose=Model 且 owner=execution/model_call 一律 PreparationRequired，已经持全锁也不例外，不进行写入或 released=false 复活。ReadCredentialForRequest 读到该实际组合也返回 PreparationRequired，要求显式新 read 口。判断不靠 caller 的 Purpose 或 provider 猜“是否来自 plan”；planned Apply 直接走现有私有 mutation 核，仍 RequireHeld/Validate/leaseGrant。相同 execution owner 的 Purpose=MCP 原义不变；原 Account 规则不变，无新 SQL/owner CHECK。
- ReadCredentialForUsage 仅接受上述 Model ReadLeaseUsage 请求；每个真实 attempt 在准备发送前传入 `RequestID=InvocationID.String()` 与 exact Actor/ref/purpose/owner/lease。Secret 在 Tx 外 DiscoverUsage，由 Model planner **按该 ID 定位唯一 invocation**，绑定 canonical call/snapshot/consumer/input_digest+schema、Process/fence/current attempt/dispatch 与 lease 映射；再调用该 consumer 的 credential_read Discover，不能查最新/任一 active call。该值只定位事实，不授予权限；Model provider 验当前受信 Runtime 的 Process/fence 与原已准入、未退休/未发出的 attempt、当前 consumer 输入/gate；错 call/use/input/Process 不得借另一个合法 call 通过。
- 新 read 自开短 Tx，一次 AcquireAll 包含 Model/consumer 提供的全部 User/Project/Agent/Execution/operation/call/Invocation writer gate 与 Secret credential/lease locks；按 D03 全局层序排序。RequireHeld/锁后 Validate 重验 exact request/mapping 和当前 lease/ref/owner/purpose/released；变化整体回滚，不在 Tx 内 Discover、补锁或网络。完整 Validate 后复用 leaseGrant 的当前 scope/owner/purpose 检查；它不分辨 legacy/planned，也不反猜 Invocation。Model grant 的 RequestID 留空，读取核仅从**已验证** request 写本次 Audit 的 RequestID，其他关联仍受既有 scope 校验；不改 UsageAuthority.AuthorizeLeaseInTx 签名。
- 解密当前 value 与 SecretResolve Audit 仍同短 Tx，材料仅 Committed 后返回；Audit/读事务失败或 commit Unknown 全部清零材料/零发送。原 reservation/Acquire Unknown 必须先沿其原 writer 核实，read 自身 Unknown 也不因 reservation 已存在而返回材料；保留原 cause，确认失败不覆盖原 Unknown，不造明文重放仓库。必要重读仍用 exact Invocation+当前授权并生成新的实际 resolution Audit。轮换后下一 attempt/turn 用新值；已发请求不热换，snapshot 不固定明文/value version。
- SecretService Actor 使用 credential scope，CauseRef=lease owner ID，不能拿 Invocation 替换；显式 RequestID 单独绑定 use。System credential 的 UseGrant Subject 仍受限 SecretService，真实 Project/Agent/consumer 归属保存在 Model call/Usage，不能把 AgentRun 放到 System Scope/冒充管理员。Project credential 必须同项目。D04 outbound 用真实 OutboundService、Project scope、CauseRef=InvocationID、producer=access；D08 正式 validator 委派 Model 核 exact 已准入 call。
- execution lease 由 D22 真实 execution terminal/join 协议释放；model_call lease 由本 logical call 的全部读/发送/response/material 使用 actual join 后释放。lease 保护稳定 ref，不授权请求；取消/超时/Session 失效不是 join。Release 必须有完整 opaque plan，锁后再读 exact lease 并匹配 request/ref/owner/ID/purpose；旧已释放 ID 不再 acquire，不在高阶锁内 Discover。

管理端写 credential 复用 D04 独立 Secret 命令（Purpose=Model、真实 Human/Scope），Provider CRUD 只接受引用，不在 Model commands 存明文或明文派生摘要。写入材料后绑定 Provider 是两个明确结果；若绑定失败，原 Secret receipt/ref 可重试或由有权管理者安全删除，不宣称两次 HTTP 原子。值轮换是原稳定 SecretRef 的更新；Provider 删除和 Secret 清理仍独立。将来表单可编排此流程，不能回读/回显旧值。

## 5. 调用、reservation 与用量原子事实

D01 ModelRequest/ModelFrame/Usage/ModelError 保持字段含义。ModelRequest 内没有任意 retry HTTP 头或外部 deadline 权限；caller 输入必须与当前 consumer 的 input_ref 完整匹配，服务重算 canonical digest，不信任裸 caller digest。logical_call_id、consumer、snapshot、输入/schema、retry_class/policy 同义绑定；不同语义输入/压缩/用户再生成使用新 call。

```go
Chat(context.Context, ModelRequest) (ModelResponse, error)
Stream(context.Context, ModelRequest) (ModelStream, error)
Embed(context.Context, EmbeddingRequest) (Embeddings, error)
Rerank(context.Context, RerankRequest) (RankedItems, error)
GenerateImage(context.Context, ImageRequest) (GeneratedFiles, error)
// 以下持久/Runtime 端口尚属后续块，不在 C0 新源中定义空载体。
LookupCall(context.Context, identity.Actor, CallIdentity) (CallReceipt, error)
// 持久化口由 Runtime 使用，均须绑定实际 reservation/attempt，不开放 HTTP。
ReserveInvocationInTx(context.Context, foundation.Tx, InvocationReservation, InvocationPlan) (InvocationReceipt, error)
ObserveInvocationInTx(context.Context, foundation.Tx, InvocationObservation, InvocationPlan) error
FinalizeInvocationInTx(context.Context, foundation.Tx, InvocationFinal, InvocationPlan) error
```

Call phase=`accepted|running|succeeded|failed|cancelled|unknown`；Invocation dispatch=`reserved|authorized|sent|not_sent|unknown`，terminal status 沿 D01 succeeded/failed/cancelled/unknown；尚未终局不能拿 succeeded 默认值。DB CHECK 禁止 not_sent 搭 provider usage/成功响应；reserved 未发出不计成真实调用。attempt ordinal 包括 reservation，实际调用计数只按 dispatch=sent，unknown dispatch 另计；不会因一次零字节失败伪造 Provider token 消费。

本地 operation 在 claim Tx **之前**登记 exact Process/call/Invocation 身份；只有单次成功交给 worker 或不可再交付两分支。caller ctx 返回不当 DB join；迟到 claim/commit 必须同 writer 锁确认，不允许 Registry 缺项证明“从未发送”。同 call 至多一个活 process/fence/attempt；重启恢复先 exact ProcessGuard death 与原 DB writer 终局，不用 TTL 接管。

每 attempt：当前授权/consumer gate/固定 snapshot验证 → reserve+Usage skeleton 提交确认 → 读取当前 credential/受控输入 → 短 Tx 固定发送资格 → Tx 外 D04 Do → 解析/actual join → 终态+可靠 Usage+Execution summary 同 Tx。任何准备提交 Unknown 未确认不得调用 Do；若权限/输入改变，明确 no-send 终局且零真实调用计数。

D04 `Response.Decision().Sent` / 安全 NetworkError.Decision.Sent 是真实发送证据；拿到 true 后保存 sent 与本地**确认发送时间**（`dispatched_at`，不是假称精确 socket 首字节时刻），另存 attempt started_at。false 只有实际 Do/本地 I/O 完成且不可能再写时才可 not_sent。发送资格提交后崩溃、未观察 Decision 的窗口只能 dispatch=unknown，不能按缺 row/ctx取消猜 0，也不能伪写 sent。保留 started/observed/finished 三种含义；若将来需精确首写时间，须另审 D04 observer，不在本稿暗改端口。

Model Runtime 是 invocation record/finalize 唯一写者；same InvocationID 同 final digest 幂等，异义冲突。Sent/usage_update观察经ObserveInvocationInTx按attempt观察序列持久化，重复/旧序列不回退，可靠usage在相应对外usage_update前确认；summary按canonical前后delta同Tx变化。成功 terminal、最终可靠 usage、summary增量/版本原子提交；callback 成功但 COMMIT Unknown 仍不是成功。沿原 call+invocation writer 锁确认 canonical final；确认超时保持 unknown。finalize 新 Tx 的 unknown 也不能发送第二请求。

Provider 返回成功但本地结果发布提交未确认，不发 message_end；已 streaming 前缀不能收回，最终发安全失败/关闭并保原ID供查询。Usage 不保存答案，因此已完成 call 的重复入口只返回安全 CallReceipt/明确 already_finalized，不重调 Provider 或伪造重放正文；正文重放由原 consumer canonical 负责。LookupCall 当前授权先行，仅返回安全状态/ID/Usage，不回读别的 caller 输入。

失败/取消仍保留已收到的可靠 usage。成功协议终止缺 usage 也是 succeeded+unknown usage；收到 usage 后断流是失败/unknown+保留该 usage。字段缺失为 NULL，provider 明确 0 为 0；total 只保存 provider 给值，不相加 cache/reasoning，也不以图片数量代 token。各 profile 累计/增量规则必须由 §6 证据冻结，不能对 usage chunk 盲加。

恢复只推进原 canonical 终态、释放/清理、重建 summary；不自行重发 Model 请求或从缺失正文猜输入。活 consumer 如需继续原 logical retry，必须以相同输入完整重入、原 attempt 已 join/terminal 且当前授权，并创建可见新 Invocation；外部效果未知明确保留，不能把后台恢复当自动业务启动。

## 6. 五种协议 profile：固定源码事实与 conformance 边界

此前 15 次 HTTP GET 的 DNS 失败事实保留；随后独立补证通过只读 Git 取得 [OpenAI 官方 SDK](https://github.com/openai/openai-python/tree/becc1d20eed83c1b8d85e15dc131a372d9dc7813) `becc1d20eed83c1b8d85e15dc131a372d9dc7813`（3.24.0，下称 O）和 [Anthropic 官方 SDK](https://github.com/anthropics/anthropic-sdk-python/tree/18f25547f20cf5f01da69ac611e700e3bc9ebf21) `18f25547f20cf5f01da69ac611e700e3bc9ebf21`（1.11.0，下称 A），2026-10-05 获取。补证报告 SHA `41f04a202111e3ab99ea0c904112accc76dfc6844975b3c518640c1e090a515f`，逐文件 commit/path/hash 与已独立定向复核记录随输入 manifest；SDK 不成为运行依赖。以下 profile ID 是本项目工程标识，固定字段事实**不证明真实 Provider 执行、账号可用或全型号支持**。

| profile | 本轮可冻结的 wire 字段/映射 | 尚未闭合及固定源定位 |
| --- | --- | --- |
| openai-chat-completions-v1 | 配置 base 保留路径（官方默认 `/v1`），POST相对`/chat/completions`、Bearer；messages/model必需，max_completion_tokens；structured为response_format.json_schema的name/schema/strict；原生function tools、tool_choice与parallel_tool_calls；stream显式include_usage | 逐型号effort/default/schema子集待证；O `types/chat/completion_create_params.py`、`types/shared_params/response_format_json_schema.py`、`types/chat/chat_completion_chunk.py`、`chat_completion_stream_options_param.py`、`resources/chat/completions/completions.py` |
| anthropic-messages-2023-06-01 | POST `/v1/messages`，X-Api-Key，anthropic-version=2023-06-01；独立system、max_tokens/messages/model；structured为`output_config.format={type:json_schema,schema:...}`，不是顶层output_format；enabled thinking budget≥1024且<max_tokens，adaptive独立无budget；effort在output_config | effort low/medium/high/xhigh/max只证枚举，不证所有型号；标准非beta create无自动beta头不证明GA/账号支持；thinking/tool互斥、schema子集待证。A `types/{message_create_params,output_config_param,json_output_format_param,thinking_config_enabled_param,thinking_config_adaptive_param}.py`、`resources/messages/messages.py` |
| openai-embeddings-v1 | POST相对`/embeddings`，Bearer；首版只收文本/文本数组，**显式encoding_format=float**；data的index对应输入，embedding为float数组；dimensions只按已核型号要求 | SDK省略encoding_format时会发base64并用post_parser，D04直连不能继承该隐式转换；型号维度/限额待conformance。O `resources/embeddings.py`、`types/{embedding_create_params,create_embedding_response,embedding}.py` |
| jina-rerank-v1 | 仅能力边界：System reranker，query+文本候选，返回原index/score | 本轮仍零官方字段证据；query/documents/top_n/return_documents/results/usage均候选，score范围/截断/error未证，不凭路径或第三方兼容验收 |
| openai-images-generations-v1 | POST相对`/images/generations`，Bearer；首版非流，显式model/prompt；GPT Image用output_format=png/jpeg/webp及b64输出；n=1..10、jpeg/webp compression=0..100仅在对应型号支持下使用；不发送legacy response_format/style | legacy字段仍在SDK union不证明retired DALL-E可用；quality/size宽类型不是全型号矩阵。usage可空，注释仍限gpt-image-1而枚举更广，不推导新型号必返/不返。O `types/{image_generate_params,images_response,image}.py`、`resources/images.py` |

OpenAI reasoning_effort的源码枚举为none/minimal/low/medium/high/xhigh/max，但逐型号支持/默认须另核；标准ChatMessage/Delta没有通用reasoning_content，不凭兼容厂商惯例虚构。OpenAI工具delta按index拼接可选id/name与arguments字符串，完整JSON才形成tool_call_end；finish_reason有stop/length/tool_calls/content_filter/function_call（deprecated），choice finish不能替代`[DONE]`。include_usage的最终chunk在`[DONE]`前且choices=[]；中断可能永不收到。原error.code可空，HTTP404不能无条件归model_not_found。

Anthropic content block按index，input_json_delta.partial_json与signature_delta分别处理；thinking/signature与redacted data原顺序原样roundtrip，不把safe reasoning展示替代opaque metadata。tool_choice=auto/any/tool/none，disable_parallel对auto为至多一个、any/tool为恰一个；不启用SDK额外server tools。message_start/delta/stop与content-block事件分别解析，明确message_stop才正常协议终止；stop_reason的pause_turn/refusal/model_context_window_exceeded等不猜正常stop或自动续发。对应 A `types/{thinking_block_param,redacted_thinking_block_param,raw_message_delta_event,stop_reason}.py` 与 `lib/streaming/_messages.py`。

| Provider usage 口径（绑定 profile/adapter_revision） | 平台字段与流规则 |
| --- | --- |
| OpenAI chat：prompt_tokens/completion_tokens/total_tokens；prompt details cached_tokens/cache_write_tokens；completion details reasoning_tokens | input/output/total分别取原值；cache/reasoning为细分，不再相加。最终usage为全请求统计，不能逐chunk求和；缺失NULL。O `types/completion_usage.py`、`chat_completion_stream_options_param.py` |
| Anthropic：input_tokens/output_tokens/cache_read_input_tokens/cache_creation_input_tokens；可选output_tokens_details.thinking_tokens；无total_tokens | input保原input，cache分别映射cached_input/cache_write，reasoning为输出细分；**cumulative覆盖**，delta省略optional字段保先前值而非归0。其input与cache是独立组成，与OpenAI包含关系不同；如显示含cache输入总数只能明确derived，不伪Provider total（仍NULL）。A `types/{usage,message_delta_usage,output_tokens_details,raw_message_delta_event}.py` 与 `_messages.py:514–534` |
| OpenAI embedding：prompt_tokens/total_tokens | input/total原值；output未知NULL，不补0。O `types/create_embedding_response.py` |
| OpenAI image：可空usage及input/output/total，image/text细分 | 仅实际合法返回才保存，细分不重复累计，缺失NULL；不按像素/图数估算。O `types/images_response.py` |

各 adapter 的 manifest 必须绑定上述官方 commit/path、所采用字段、profile_revision 与实施后的反例 fixture；已核字段也须真实受控 server conformance，不能把SDK类型存在当执行通过。逐型号 reasoning/structured/image 参数、schema子集及Jina仍待补证；未核显式能力报unsupported_feature。无 live Provider 请求时只称 schema/fixture 验证，兼容实现跑同套正反，不提供万能“兼容”开关；本轮不继续联网或调用模型。

所有 inference 用 POST 经 D04 Client.Do；显式关闭 adapter/SDK 重试，D04 仅其已验零字节透明网络重试，n>0 后不能隐式重发。POST 3xx 不跟随，不能用 Redirects=0 误当禁跳（该值是 D04 默认）；真实试验验证 server 请求计数。错误只采安全 category/code/requestID；原响应正文/header/凭据不进日志。

SSE/JSON parser 有界：单 event 1 MiB、累计 content 16 MiB、单 tool arguments 1 MiB、最多 128 tool calls、最多 256 message parts；协议行、UTF-8、数字、JSON重复key错误正常失败，背压有界。结构化 schema≤64 KiB、最大深度32，验证所支持 subset；不执行 `$ref` 网络解析。adapter 终止检测独立于 HTTP EOF，不从未知 stop reason 猜 stop。

## 7. Stream、工具、媒体与重试预算

Stream 为 `Next(ctx) (ModelFrame,error)`、`Close(ctx) error`、`Joined() bool` 的受控句柄；Close 表示取消并等待真实 reader/transport/parser/finalizer，等待超时不伪 joined。消息含 call/invocation/attempt/sequence；sequence 每 invocation 正递增，恰一个逻辑 terminal。backpressure 队列≤32帧/总256KiB，满则 producer等待可取消，不能无限 goroutine。

attempt_started → message_start/parts/usage → 成功 message_end；重试先 attempt_aborted 再新 attempt_started，不拼接两个 attempt 的文本/工具参数。tool_call_end 仅表示本 attempt JSON 组装完整；只有成功 terminal 才允许 D22 派发 Tool。工具 side effect/Approval 属 D19/D21/D22，adapter不执行。reasoning 仅投影明确允许返回的字段；provider metadata 固定 adapter/revision 的 opaque ref，不能当通用跨模型注入正文。

image/file 输入只接受 object BusinessFileRef，经实际 source authority+SourceReads/current lease 打开，取消后实际 close/join再释放；不接受任意远端 URL或把签名 URL 交 Agent。尚未绑定 Execution/Meeting source provider 时明确 unbound。GeneratedFiles 经受控 byte验证与正式 D05 Uploads 发布独立对象，再返回业务 ref；输出 owner/cause 必须由对应 consumer 提供真实授权，不能伪造 ArtifactID/共享已有对象。Provider临时 URL 只在 adapter内通过 D04重新授权读取，不转发 API credential到图片 host；结果 URL不出现在日志/长期模型内容。

Embed 校验每输入恰一对应 index、维度一致、有限 float；Rerank 校验合法唯一原 index、有限 score、top_n 约束，不信任返回 document 替换原输入；Image 校验已核格式/数量、字节与媒体容器一致、D05实际成功，任一步失败不伪生成成功。全部保留原 Provider attempt usage，文件发布失败不能丢已发生模型调用。

Agent retry 唯一 owner 为 Model Runtime：单请求 timeout 30s→60s→120s，达到120s保持，读闲置≤60s且不超请求剩余预算；backoff 1/2/4/8/16/30s封顶带jitter，合格 Retry-After 纳入同上限。active/current consumer 允许时不设置最大 attempt 数或 Execution 总期限。每次重试重新验证当前 gate/取消并新 Invocation；达到超时上限仍可重试，不偷偷终止Agent。

可重试为已核 rate_limited/provider_unavailable/timeout/network及 profile 明确 transient provider_error；authentication/permission/model_not_found/invalid_request/context_too_large/unsupported_feature/content_filter 不自动重试。unknown 不因默认 bool 自动重试，须具体已核安全错误归类；cancelled永不retry。用户取消的logical terminal为call_cancelled，但已发且无法确认Provider结果的Invocation仍为unknown；已先提交成功终态则取消不倒写失败。partial stream失败同规则，前一attempt已join且usage终局处理后才下一次。

其他 consumer 必须由受信 authority给有限 `deadline,max_attempts,allowed_retry_categories`，不是客户端自增预算；Model runtime不替 Approval/Meeting创造上层应用重试。approval_auto 不叠加 Model应用重试，仅允许 D04 已定未发送透明重试；语义 regenerate/compaction 是新call。D22 watchdog取消本call并决定新round，adapter不得将其重解释成Provider timeout续期。

并发工程上限：全局64个实际 Provider attempt、每Project8个，排队最多128个且服从 caller deadline；超准入429，不持DB锁等待。backoff释放 attempt槽，logical工作及实际 I/O仍登记；请求/响应缓冲并发预算受这些槽与上述上限约束。取消中的加密/解码/Close仍占真实槽至join，不因ctx返回早释放。

## 8. 用量查询与可重建汇总

`usage.Service.List(ctx, Human, Query) (Page,error)`、`Aggregate(ctx,Human,AggregateQuery)(AggregatePage,error)`；ProjectID必需，当前 Session+Owner，系统管理员无跨Owner豁免。任意聚合前先同UserSH+ProjectSH当前权限，归档可读，deleting只按D08限定安全状态拒正文查询。

Query 支持 consumer_type/Agent/Execution/Meeting/purpose/历史ProviderID/历史ModelID/status/started_at[from,to)。默认最近30日；显式范围不靠默认时间改摘要，同筛选可游标分页查全部保留历史。分页默认50/max100，使用部署 cursor.Keyring；digest绑定scope/Actor稳定主体/排序/全部filter，不绑定limit/session/trace。order started_at DESC,id DESC，固定第一页水位，所有页当前授权，cursor不是权限。

Aggregate 项目内分组维度闭集 consumer/Agent/Model/Provider/Execution/Meeting/purpose/day；limit≤100组，可继续分页，不动态拼SQL标识。2s查询总预算，参数化SQL+实际索引EXPLAIN/较大fixture验证；超时返回错误，不以截断扫描冒充总量。不要求跨分页持久snapshot；AsOf标明本次真实统计时间，页水位与Summary含义分列。

返回 confirmed_invocation_count、dispatch_unknown_count、status counts；每 token 字段独立 sum/known_count/unknown_count。无调用时计数0、sum0；有调用但全未知时sum=NULL，不报总token0；部分已知给已知sum及未知数，UI不得标成完整账单。执行累计基于每次真实 attempt，不将Agent会议发言另记meeting，也不把会议辅助摘要记Agent。

Observe/Finalize同Tx按原Invocation规范事实delta更新summary，已发送在途计入确定调用数，重复不会再次累加；重建按canonical invocations在正式summary锁/版本下替换，不与并发Finalize丢增量。历史Provider/Model删除后仍按snapshot ID分组。Meeting历史删除仅断live source link；Project永久删除清全部本Projectusage，无全局TTL或默删未知记录。

## 9. Lifecycle、恢复与共享预算

新 `model.Runtime` 为单一 operation/worker owner，提供 Initialize/Recover/StartMaintenance/Check/StopAdmission/Drain/Force/Joined；构造无I/O，Initialize前根先登记partial资源。恢复按 durable recovery_pass+id 每批100，受保护前缀推进pass，不阻挡其他Project；foreign live/unbound 是本条pending，不能整轮提前退导致饥饿。只在DB/storage真实检查后判断“空态”；有遗留call/lease且缺authority保持 capability unavailable，不静默跳过。

复用实际 D05 ProcessGuard 的相同 ProcessID/deployment/spool/host与kernel boot事实；不是再建独立死亡探测。samehost可信旧boot/exactflock或已join可证明旧**本地**进程死，不能证明远端Provider没执行。已authorized未核发送attempt按unknown收敛，Usage不能重写not_sent。lease退休依真实使用者join或exactdeath+同writer终局，不依timeout/过期。

D08 manifest注册 `Usage`/Model实际参与者版本，scope Project；RequestStop/InspectStop/Cleanup 均校验 exact当前LifecycleCause。停止预检用当前正式只读授权计划、Project SH捕获**精确在途**handles，提交后取消，后续EX transition；正式provider若要求EX不能擅自降SH。旧archivecause不能取消Restore后新admission；删除不可逆gate不能因新version/active投影复位。

archiving封新Resolve/call/retry，等待原工作actualjoin/终态；archived保留配置/快照/Usage并允许限定原事实finalize收敛，不允许新Model请求。Restore不重开旧terminal call；新合法call按当前version开始。删除停止本Project所有模型/媒体I/O，再释放reference/lease，清Project配置/snapshot/call/usage/summary/commands；System配置和其他Project不受影响。

模型lease/引用清理在Secret/Object最终Cleaner之前；最后Outbox/Audit/D08清理顺序沿正式manifest。DB清理Tx锁后复核cause/checkpoint及无活writer/reader，不能仅信旧报告；持续late finalize/旧command不得upsert重建已清项目。清理unknown沿原writer/cause确认，缺key/缺provider不作为清理成功。

根初始化共享30s或更短parent；健康轮2s、10s采样、20s陈旧即unavailable。必要DB/Secret/adapter registry/恢复检查失败不得ready；未配置必需selector、未来consumerunbound、§13未定能力如实分列，技术HTTP可监听不代表产品ready，Provider外部断网不靠启动付费探测判定。

StopAdmission先封新work；HTTP/Outbox/实际consumer/Model parser-body/material-finish所有已准入调用join后，才关闭会拒绝其cleanup的依赖域。Force所有参与者用**同一个额外1s ctx**，前项耗尽仍实际发起后项close/DB ForceClose；DB最后，不续预算。Model未join时不能调用会释放共享ProcessGuard的Object.Runtime最终finish，可先关已有Transport但guard保持到actualjoin或OS退出。Initialize早退/晚安装/异常同一owner承担，不让晚启动worker脱离registry。

## 10. Audit、领域事件与错误

新 Audit producer=`model`，resource=`model_provider|model_config|model_selection`，闭集动作 provider.create/update/delete、model.create/update/delete、model.selection.update。metadata仅scope、stable IDs/version、changed_fields闭集、replacement ID/affected_count/selector kind，不存name/base_url/overwrite/schema/输入/输出/凭据/自由错误文本。普通模型调用由InvocationUsage负责，不为了计量逐条扩Audit。

Human配置命令在当前Session/System或Owner之后，`ModelAuditAuthority.CheckAppendInTx(ctx,tx,entry,key)` 核真实同Tx command/resource/resultversion/action/ordinal0；不同动作、错误scope、原Initiator/command不符拒绝。System批量替换只写System配置审计，不冒充其他项目Owner。原Secret创建/旋转/解析和D04出站拒绝沿各自producer/cause真实事实；后台ModelRuntime不获配置Audit授权。

D06新增中立 event catalog payload（仅foundation）：`model.configuration_changed`（aggregateID/version/scope/projectID?/changed_fields）、`model.embedding_selection_changed`（selection_version、old/newModelID、新selection identity）。生产者授权实现属于 Model，Append和配置事实同Tx，预收集registration SH及计划，安全payload不含凭据/正文。处理器按同aggregate version防旧覆盖；D13缺handler时不伪称索引已重建。

Embedding selector 改变只发新的期望选择/版本；D13/D14 负责实际索引 generation 的完整构建与切换。查询经 §4 serving_snapshot variant 将当前 serving 的 immutable snapshot 绑定**本次新 call lease**，不能携旧已退休 ResolvedModel lease 或重新 Resolve 当前 selector 混入新向量。真实来源 authority/Secret 保留未绑定即拒绝；D09 不造 generation 表。后接 handler 需正式注册/初始 canonical bootstrap，不把历史 Outbox 广播当唯一真相。

业务安全错误沿foundation：INVALID_ARGUMENT/NOT_FOUND/FORBIDDEN/VERSION_CONFLICT/IDEMPOTENCY_KEY_REUSED/RESOURCE_BUSY/INVALID_STATE/DEPENDENCY_UNBOUND/DEPENDENCY_UNAVAILABLE/COMMIT_UNKNOWN；可选未配置返回 typed absent capability，配置存在但坏引用为错误。Provider ModelError.category及finish_reason完全沿D01，不向HTTP回原provider error/body，未知字段不给成功默认。HTTP状态复用既有映射，不擅自将计量unknown包装200成功答案。

## 11. HTTP 与后续绑定

D09先后端服务与HTTP，无匿名模型调用路由，无新增账户/System身份实现。路由统一挂已有公共httpapi层，沿当前Origin/CSRF/cookie/严格JSON/敏感body策略，不双包recover/logger。管理写需Idempotency-Key与expected_version；返回安全receipt/version，Unknown带原command查询方式，不提示换key。

| 路由组（前缀沿D08正式Project路径适配） | 能力与权限 |
| --- | --- |
| `/system/model-providers`、`/system/models` 的list/get/create/update/delete | 当前System admin；Provider delete无级联；Model delete含显式replacement或合法selector清空 |
| `/system/model-selection` get/update | 当前System admin；同一次更新全部提交/冲突；不回显Secret |
| `/projects/{id}/model-providers`、`/projects/{id}/models`、`/projects/{id}/available-models` | 当前Owner；只管理Project chat，System候选安全只读 |
| `/projects/{id}/meeting-model` get/update | 当前Owner；必需 enabled可见chat，初值按§13，不加新的创建表单默认 |
| scope对应 `/model-credentials` create/update/delete | 仅D04 Secret正式Human命令，PurposeModel，独立receipt；敏感正文不log，不提供明文GET |
| `/projects/{id}/model-usage`、`/model-usage/summary` | 当前Owner；§8过滤/游标/空值/安全时间统计，admin无他人访问豁免 |

具体path parser复用D08已验路径解析，`{id}`表示内部稳定ProjectID接点，不绕过本人username/Project路由校验；最终OpenAPI不同时造两套不一致项目地址。未绑定真实provider时能力端口明确503，不注册匿名fallback。D22 Stream是内部受控Go口；D25/D28负责以后向浏览器的RuntimeEvent投影，不在D09造第二条WS频道。

## 12. 验收及命令

C0 按主卡执行纯构建/unit/race/vet，其作者结果不代替独立源码验收；下表涉及实际服务/存储/Provider 的断言均为**后续实现后执行**。每一真实probe附固定源码/依赖manifest、确切命令/exit/资源finally清理；生产stub或仅interface编译不能替代真实PG/Providerfixture。新tests必须进入既有完整脚本包列表，不能只在作者临时命令中存在。

| ID | 必须观察的真实断言 |
| --- | --- |
| T01 类型/输入 | scope/protocol/type能力组合；未知/重复JSON字段；整数>2^53精确往返、>MaxInt64拒绝；overwrite禁认证/正文/URL、safe格式化零材料；optional absent与dependency error区分 |
| T02 权限 | 普通Human无法System写；admin不能读他人Project/Usage；每页当前Session/Owner；Agent/Meeting/Tool伪consumer或缺adapter拒绝；archived只读、active恢复新请求不复活旧call |
| T03 migration | fresh与原schema→新migration真实升级；旧Audit合法行不变；非法scope/NULL CHECK/阶段组合拒绝；任一失败整Tx回滚；升级不猜模型或summary默认 |
| T04 配置事务 | commands/currentauth→幂等→version；Secret引用、Model/Audit/Outbox同Tx故障全回滚；同key新Session同义重放，改字段冲突，权限撤销不泄露旧结果 |
| T05 replacement | barrier后并发新增Agent/summary引用不能漏；多Project实际authority只受控改ref；不合法reasoning全回滚；Provider有Model拒删；System候选不含他人内容；liveFK清空历史snapshot仍完整 |
| T06 Resolve | 同outerTx input capture+snapshot/binding+Secret lease原子；缺低序锁/SH升级/错issuer/映射变化拒绝；InTx零网络；commit丢回复不发出；current_selection用当前配置；serving_snapshot新call保持原endpoint/参数并新lease，旧call退休不复活，来源/retained ref缺失与未绑定拒绝；execution同ref/owner重用canonical LeaseID，不重复预分配撞键 |
| T07 credential | 同execution lease下两个call的exact RequestID不得串读；旧attempt迟到、Discover后fence/current input/Process/mapping变更、错use均拒绝，不能借另一合法call；新read零ID/其他variant带ID拒绝；完整低序union，SystemSecret无伪scope；轮换下一attempt新值；Acquire/read/Audit/COMMIT Unknown零材料零发送；已全持锁的legacy Model Acquire/Release/Read仍PreparationRequired，planned同请求成功且已释放ID不复活；MCP execution/Account与零RequestID旧Binding逐字兼容 |
| T08 invocation/summary | reserve未发不计真调用；同attempt finalize重复0增量、异义冲突；可靠usage NULL/0/部分/累计/overflow；三类COMMIT丢回复（真commit/rollback/仍挂起）；summary重建并发不漏增量 |
| T09 chat/stream | 每已核profile真实受控server：OpenAI choices=[]最终usage、finish后缺[DONE]；Anthropic output_config.format编码/cumulative覆盖及省略保值、thinking/signature/redacted原样；正常terminal、usage缺失/后断流、EOF/流内error、UTF-8拆片/半toolJSON、length/filter/refusal/背压；abort新baseline，工具仅成功terminal后执行；cache包含关系/total NULL反例 |
| T10 retry | 原server请求计数证明D04无发送后隐藏重试；Agent30/60/120后继续多轮，无固定次数；cancel/Watchdog在backoff/读body/terminal前阻下一attempt；bounded consumer不越其budget、autoApproval无应用retry；每真实attempt独立Usage |
| T11 nonchat | embedding显式float、index/维度/非有限值反例、缺output为NULL；rerank在官方补证后核索引/score/top_n；GPT Image拒legacy response_format/style、型号不支持quality/size拒绝，缺payload/坏b64不造文件，无usage不估token；仅真实支持URL的profile走D04取回，D05发布失败保Usage；不把同路径别协议当Jina兼容 |
| T12 Project并发 | Stop预检/callback阻塞、archive与Resolve/send资格/新retry竞争；停止只有actualjoin，restore新call不被旧cause取消；deleting后lateFinalize不重建；Model→Secret/Object→Outbox/Audit清理顺序完整 |
| T13 recovery | Claim前登记与未知迟到claim；foreign live不挡本地；首100受保护不饿第101；exactdeath不证明Provider未发；authorized crash dispatch_unknown不归零、不自动再请求；ReleaseUnknown沿原事实查，恢复无伪权限 |
| T14 shutdown | HTTP/consumer/model/body/material/finalize在途，第一信号graceful；第二/超时同1sForce，DB最后实际发起；blockedClose/DBUnknown/partialInitialize/latereturn未join guard不释放；不从ctx返回推Joined |
| T15 usage query | Owner隔离与每页权限撤回；page limit可改、cursor filter不可改；旧liveFK NULL仍按snapshot聚合；known/unknown分母、meeting speech单计、辅助purpose独立；大fixture真实索引/2s超时如实失败 |
| T16 native provenance | 五profile官方固定schema来源、合成fixture正反与精确adapter_revision；有env凭据另行真实smoke才标Provider实测；无凭据可明确skip该smoke但不得把skip写通过 |
| T17 HTTP/root | 真实System/Project provider、Origin/CSRF/当前Session、敏感请求零日志、幂等Unknown、唯一公共middleware；缺D08/consumer仍failclosed；healthy与productready分列 |
| T18 settings/bootstrap | 按§13最终确认规则做首次配置、旧项目/原Create receipt重放及迁移；不假造Summary初值；D13只在完整新generation后切serving snapshot（后续真实集成责任单列） |

建议实现后的固定命令：

```sh
GOTOOLCHAIN=local /workspace/toolchains/go1.27.1/bin/go test -mod=readonly -race ./internal/central/model/... ./internal/central/usage/...
GOTOOLCHAIN=local /workspace/toolchains/go1.27.1/bin/go vet -mod=readonly ./internal/central/model/... ./internal/central/usage/...
GOTOOLCHAIN=local /workspace/toolchains/go1.27.1/bin/go build -mod=readonly ./cmd/...
AGENTEAM_GO=/workspace/toolchains/go1.27.1/bin/go GOTOOLCHAIN=local sh scripts/test-models.sh
```

`test-models.sh` 为本次将新增脚本，复用既有 `test-objects.sh`/PG fixture隔离协议，保持原每包6m/race；新增owned私网HTTP/TLS provider fixture与ca/随机端口，D04只对该私网测试地址显式allow，绝不放宽loopback。请求计数/停顿barrier、COMMIT断链、exact子进程死与foreign live由独立fixture观测，不用sleep猜竞态。纯parser可用离线bytes，不能称真实网络。所有PG/MinIO/Docker运行仍由实施/验收任务获得资源所有权后执行。

## 13. 明确待闭合事项

1. **Project会议模型初值（产品含义，root已向用户询问）**：模型配置架构429–436行明确必填；[系统项目创建布局](../../frontend-design/layouts/system-pages.md)24行只名称/描述；[会议设置](../../frontend-design/layouts/project-settings.md)82行给选择器；未找到系统默认summary规则。D01只提供显式ModelRef解析，D08已验InitializationRequest/Receipt专属Skills，不可填入模型冒充完成。本稿不决定“创建后再配/创建必选/系统默认”，不改D08输入、不新增默认或用NULL宣称必填满足。确定规则后仅补必要初始化来源/version、同CreationID/key引用绑定、旧幂等优先及不猜值迁移；受影响初始化/HTTP/完整D09验收保持未满足，其他块可独立推进。
2. **Provider conformance 与剩余 wire**：§6 四 profile 已有固定官方 SDK 字段事实及独立局部复核，不再称零官方源码；Jina、逐型号能力/互斥矩阵、完整 schema 子集和真实 server conformance 仍未验。SDK不证明账号/API可用，无真实smoke不宣称Provider实测。此限制不阻纯配置/锁/Usage或本轮公共契约，但B02/完整D09不得据源码证据宣称ready；后续联网/运行另行授权，不继续本轮研究。
3. **真实跨模块实现**：已验 D08 B02 Human Owner/gate 可用，B03 lifecycle/Service validators仍待绑定；D07实际最终根提交、D10 Agent model-ref authority、D13/D14 serving source与长期Secret保留、D19Approval、D21ImageTool、D22Execution/Loop、D24Meeting分别接正式端口，缺实现不是空成功。可先独立验本轮纯契约，不代表完整D09依赖齐备。本次不改这些活动源码、迁移或产品界面。

作者自查与 C0 固定编译输入随本轮交付；纯契约验证不证明实际 D09 授权、事务、Provider 调用或完整模块完成。
