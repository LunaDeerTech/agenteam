# D09 B01-K：System 配置原子存储首块

修订 1，2026-10-05。原实现依赖为 `e6e94c4`，C0 已独立验收；真实迁移组固定输入为已验连续前缀 `30f5c29` 加本块源码。状态：**System 配置实现与 00015 已完成作者验证，正在独立终验，未声明模块完成或根 HTTP 已绑定。** 复用[工程规格](d09-model-system-token-usage-design.md) §2–4/8/10，与[D09 主卡](d09-model-system-token-usage.md)共同限定范围。System 先行、Project 后接同一正式 P adapter；Summary/Jina/型号决定保持待定，不阻塞本块独立结果。

## 1. 完整结果与边界

交付真实 **System Provider/Model CRUD、平台 selectors、Secret Provider 引用保护、typed Audit 与 Outbox 同 Tx、幂等/Unknown 核实、当前管理员读取/分页**。提供可直接由后续 HTTP 使用的库和真实 PG 验证；没有 Provider 网络调用、usage 估算或成功 stub。

- 所有本块管理命令仅 scope=system。传入 Project scope 明确 DEPENDENCY_UNBOUND；Get/List 只读取 System 行，不能借 ID 泄露 Project 数据。
- 不实现 Project summary 创建/初值/迁移默认；不定义模型字段到 D08 创建请求。ProjectModelSettings 更新、Agent/Project 引用 adapter、Resolver、Invocation runtime、Usage 查询及根 HTTP/ready 装配不是本块完成项。
- 配置执行 C0 已定安全结构与已知平台参数/能力闭集；依赖尚未核 native schema 才能判定的参数明确 unsupported_feature/unbound，不静默存为已验证配置。允许 metadata 与空 profile options/parameters 的合法管理输入，但不因此声明型号可调用；Jina、native overwrite/型号矩阵继续由后续 profile conformance 完成。
- Provider 无默认凭据；由真实 admin 先通过既有 Secret Human 命令创建 Purpose=Model 的 System ref，本块只接 ref。Secret 写和 Provider 绑定是两个结果，绝不回读材料。
- 平台 singleton 可只有稳定 ID/version=1 与显式 unconfigured 状态；首次选择必须原子提供合法 embedding+memory，reranker/image 可空。技术空态不是默认 Model，不得返回合法的空 PlatformSelection；此规则不涉及待定的 Project summary。

## 2. 实施单作者白名单

`model/`、`audit/` 路径相对 `internal/central/`，`tests/`、`scripts/`、`db/` 路径相对仓库根；作者为 root 后续指派的 backend_worker，只在 root 发源码卡后实施。必读[Go 技能](../../../.agents/skills/agenteam-go-development/SKILL.md)；共享规则、设计来源及固定依赖沿主卡。

- 新 `model/{service,store,authority,configuration,commands,query,configuration_policy,references,secret_authority,secret_router,audit_authority,events}.go`；测试仅上述 12 个 basename 对应的 `*_test.go`。内部 helper 可在这组文件内安排，不增加 Runtime/网络/通用权限框架。
- 新 audit/contract/model.go、audit/contract/model_test.go、audit/model_test.go；旧 Audit 窄增量见 §4。
- tests/model/system_configuration_integration_test.go：隔离实际 PG、真实 D07 admin/Session、Secret、Audit、Outbox；领域 fixture 只提供可核事实，不以 permissive authority 冒充生产绑定。
- 一个 root 后配编号的 `db/migrations/NNNNN_model_configuration.sql`（Up-only），范围与候选位置见 §6。00014 属 A，已独立验收并提交 `30f5c29`；root 随后明确授权本块唯一 `00015_model_configuration.sql`，既有 SQL 不变。
- scripts/test-models.sh 可复用既有 fixture 启动协议；tests/testsupport/postgres/cmd/fixture/main.go 仅在原固定包列表追加 ./internal/central/model/... 与 ./tests/model/...。不改资源/nonce/权限/6m/race/原列表；真实执行等 root 移交 fixture。

C0 20 源、D08 源、Secret 源、identity、app/config、go.mod/go.sum 均不是首块写入范围。后续若确需 C0 补载体先报明确缺口；下述 service 自有返回类型不改变 C0 声明。

## 3. 最小可实施 Go 形状及无环构造

简称 mc=model/contract、sc=secret/contract、ac=audit/contract、oc=outbox/contract、id=identity/contract、f=foundation。

~~~go
// model/service.go；Store沿既有postgres真实端口，不定义假Store。
type Store interface {
    postgres.SQLExecutor
    InTx(f.Tx) (postgres.SQLExecutor,error)
    WithinTx(context.Context,f.TransactionCause,func(context.Context,f.Tx)error) f.CommitResult
    AcquireAll(context.Context,f.Tx,[]f.LockRequest) error
    RequireHeldLocks(context.Context,f.Tx,[]f.LockRequest) error
}
type Authorizations struct { Sessions id.SessionAuthority; System id.SystemAuthority }
func NewAuthority(Store,Authorizations) (*Authority,error)
type ModelEvents struct { /* 私有、同 catalog 的两种 EventType；只由 DefineEvents 构造 */ }
func DefineEvents(*event.Catalog) (ModelEvents,error)
type Dependencies struct { Secret sc.UsageOperations; Audit ac.Appender; Events oc.Appender; ConfigurationEvents ModelEvents; Cursors cursor.Keyring }
func New(Store,*Authority,Dependencies) (*Service,error)
func (*Service) Initialize(context.Context) error // 真DB检查及singleton技术身份；构造无I/O
func (*Service) CreateProvider(context.Context,mc.CreateProviderRequest) (mc.CommandReceipt,error)
func (*Service) UpdateProvider(context.Context,mc.UpdateProviderRequest) (mc.CommandReceipt,error)
func (*Service) DeleteProvider(context.Context,mc.DeleteProviderRequest) (mc.CommandReceipt,error)
func (*Service) CreateModel(context.Context,mc.CreateModelRequest) (mc.CommandReceipt,error)
func (*Service) UpdateModel(context.Context,mc.UpdateModelRequest) (mc.CommandReceipt,error)
func (*Service) DeleteModel(context.Context,mc.DeleteModelRequest) (mc.CommandReceipt,error)
func (*Service) UpdatePlatformSelection(context.Context,mc.UpdatePlatformSelectionRequest) (mc.CommandReceipt,error)
func (*Service) GetProvider(context.Context,id.Actor,mc.ProviderID) (mc.ProviderView,error)
func (*Service) GetModel(context.Context,id.Actor,mc.ModelID) (mc.ModelView,error)
type SystemQuery struct { Cursor string; Limit int }
type ProviderPage struct { Items []mc.ProviderView; NextCursor string }
type ModelPage struct { Items []mc.ModelView; NextCursor string }
func (*Service) ListProviders(context.Context,id.Actor,SystemQuery) (ProviderPage,error)
func (*Service) ListModels(context.Context,id.Actor,mc.ProviderID,SystemQuery) (ModelPage,error)
type PlatformSelectionState struct { ID string; Version f.Version; Configured *mc.PlatformSelection }
func (*Service) GetPlatformSelection(context.Context,id.Actor) (PlatformSelectionState,error)
// 仅查该key的历史事实，不替代Unknown的原语义确认；Command限定C0 receipt七种Kind，Meta含完整Actor/System/key。
type LookupCommandRequest struct { Meta mc.CommandMeta; Command string }
type CommandLookup struct { Found bool; Receipt *mc.CommandReceipt }
func (*Service) LookupCommand(context.Context,LookupCommandRequest) (CommandLookup,error)
~~~

构造次序：真实 D07 Authority → Model Authority（只DB/Session/System，不依赖Service）→ Audit.Models verifier、Secret Usage router、Outbox Model producer/catalog → Model Service。Secret、Audit、Outbox均使用同一DB Store；纯构造拒缺必需依赖，不开启goroutine、网络或预先授权。首块不改生产根；测试组合必须真实完成各域 Initialize。

实现澄清：`DefineEvents` 在调用者提供的同一 `event.Catalog` 中注册两种闭合事件，返回不可拼接的私有 `ModelEvents` 载体；该 catalog 同时显式传给真实 Outbox 构造器。Model `New` 拒零载体/无效 schema，且不另建 catalog。`oc.Appender` 没有只读 catalog 身份口，故 `New` 不声称已验证它的私有 catalog；传错 catalog 时，原 `PrepareAppend` 的 `Owns` 检查会在业务事务及首个配置/command 持久写之前明确拒绝。该错误原样沿安全错误口返回，不绕过校验、不更改 C0/Outbox 公共接口。

Authority 实现 sc.UsagePlanner/sc.UsageAuthority 的 **Model Provider reference** 分支、ac.ModelAuthority、oc.ProducerAuthority。首块不实现合法 Model lease/call：Acquire/Read/Release 类依赖明确 unbound，不建立虚构call。Secret router 构造显式接既有 fallback sc.UsageAuthority（若需 planned fallback，必须真实支持 sc.UsagePlanner）；仅 Purpose=Model 的 retain/release_reference 路由到本域，其他既有 Account/MCP 路径原样转交，不猜 owner 字符串。缺请求用途的 AuthorizeLeaseInTx 在本块只转交原 fallback，不以 execution owner 推断 Model/MCP。

## 4. 真实接缝与最小旧文件授权

| 接缝 | 首块决定与精准范围 |
| --- | --- |
| D07 Session/System | 已验 account/authority.go:72/76；同 User SH/EX覆盖下 RequireCurrentSession/AuthorizeSystem，不开嵌套Tx、不补锁。首块无账户源改动 |
| Model Audit 闭集 | 旧 audit/contract/types.go：Action.Valid/ResourceKind.Valid/NewEntry/Producer.Valid/ProducerFor 只分派 Model；旧 metadata.go：Model typed metadata存储/Decode分支；新 model.go 定义下面正式口。旧 audit/service.go 只增加 Authorizations.Models 和 ModelAction 分派；不扩大其他action/Service |
| Secret Provider refs | 现 DiscoverUsage/ApplyUsageInTx 已支持 Human retain/release，无需 RequestID 或旧 lease/write 修改。新 Model planner 外层发现 Provider/command旧新ref，锁后核同一请求/mapping/current admin/确切本域变更；Secret自己核实际 metadata.Purpose=Model。legacy CheckReference 对本域管理的 Model ref拒PreparationRequired，planned私有核不走它 |
| Secret材料创建 | 既有 Human/System Prepare/Apply/Execute Write 保持。Provider命令不接材料，不调用write wrapper嵌套Tx，故本块不改 secret/write.go/service.go |
| Outbox | 现 Appender.PrepareAppend/AppendEventInTx + ProducerAuthority 已够；新 model/events.go 注册闭合两事件 codec/catalog/producer。System Event 不需要 Project adapter；没有D13 handler不是假注册成功。注册与Append屏障原样使用 |
| Project 后接 | P已明确在自己范围补 Human非Project producer Append：完整summary+stage绑定，CurrentAccess=Read/NewFact=Mutate，UserSH/ProjectSH一并计划；尚未独立验收，不能视为已可用。本块不复制该adapter、不读Project表。P的SecretAuthority在冻结验收后使用；Model typed Audit分派长期留Model并委派真实Owner，不让Project再读Model事实 |

新 Audit 口精确为 ac.ModelAuthority.CheckAppendInTx(context.Context,f.Tx,ac.Entry,ac.AppendKey) error。动作 provider.create/update/delete、model.create/update/delete、model.selection.update，Producer=model；resource=model_provider/model_config/model_selection。ModelMetadataFields 仅 ProviderID?/ModelID?/SelectionID?/Version/ChangedFields/ReplacementID?/AffectedCount/SelectorKind，按动作严格必填/禁带，changed_fields沿C0事件闭集；不接名称、endpoint、参数/JSON、输入或材料。provider/resourceID/版本/原Human user/当前Session及key producer+command cause+ordinal0须与同Tx typed command plan完全吻合；读取现有原receipt不能另造Audit。新Model分派自己的provider负责当前System（后续Owner）与fact两层检查，不借通用Secret/Project action冒充。

## 5. 原子性、锁、恢复与读取

- Tx外先当前admin预检、预分配resource/event/command身份、发现旧CredentialRef与反向引用集合、准备 Secret UsageDependencies/Outbox AppendPlan；发现不是授权。
- 初次 AcquireAll 完整 union：command EX、User SH、model-references（通常SH，删除/替换EX）、outbox-registration SH、真实Provider/Model/selector/旧新Credential/Reference/command-record锁。全局排序由D03；InTx只 RequireHeld，缺锁/模式不够、映射/资源ID变化整体回滚。wrapper可显式重规划，不能在高阶锁内Discover/补锁。
- 锁后当前Session/admin → 原command摘要/receipt → 首次version/依赖。命令摘要用稳定Human UserID，不含Session/trace；计划binding包含完整Actor。同key异义冲突，换Session同义且仍admin可重放。CreatorID在原command已出现时按原事实重规划，不使用本次新ID替换。
- Model command保存 typed before/after引用及安全receipt/event identity；private mutation写配置/完整反向reference、planned Secret retain/release、Audit、Outbox、receipt同Tx。引用release必须证明该exact Provider操作释放的旧ref（同Tx command before/after），不能从“当前ref不同”单独授权。删除Provider只在无Model时释放自身引用，不删除Secret材料；运行lease是否保留由Secret原规则承担。
- 平台selector的canonical与references同Tx；模型删除/替换EX屏障后重查完整集合。只内建 platform_selector owner，System替换校验类型/enabled/必需项/已定effort-capability；遇未来agent/project_summary引用实际存在且owner adapter未绑则unbound/全回滚，不忽略该行或仅删索引。现无引用不是绕过正式barrier的理由。
- CommitResult.Committed才返成功；NotCommitted可按原key重试。Unknown的私有确认必须保留原request/expected semantic_digest、cause与key；在同一canonical command writer锁下完成当前Session/admin检查并等待原writer终局，再将canonical摘要与原摘要精确比对。仅同义且已committed的receipt可确认原mutation成功；异义按同key冲突拒绝，绝不采纳另一请求的成功。无行、确认超时/失败或未证明原writer终局仍保持原Unknown及cause，不据此断言已回滚，不换ID/key，不再发Audit/Event副作用。
- Get/List/Lookup每次当前Session/admin。列表limit1..100（HTTP以后默认50），固定首次 created_at,id 水位，cursor独立keyring，bind查询/稳定User、不bind limit/session；不把cursor当权限。公开Lookup只报告该key的canonical历史事实；无receipt返回Found=false且Receipt=nil，既不能零receipt伪成功，也不能用Found=false证明先前Unknown已回滚。所有API/日志沿C0安全格式，不在错误中放配置原文。

## 6. Schema 与后续 Usage/Secret 完整块

首块迁移只配置五表及 Audit 闭集。原未编号 DDL 候选保留在 `/tmp/agenteam-d09-b01-prep-q49oerfz/schema-candidate.sql`，SHA256 `ad828d0306f044d9bd133b0cb756cfca44f024c09cf086eb52835c1f987c67cf`；它仅是原准备输入。正式 `00015_model_configuration.sql` 已按 `30f5c29` 的连续已验前缀生成并归位，作者真实 fresh、已填充 00014 升级、非法 NULL/组合 CHECK、事务失败及同 checksum 重试均通过；旧 Audit 行与无关约束保留。既有 00001–00014 未改，00015 仍待本块独立终验采纳。

| 后续能力 | 可以先做的事实/查询 | 必须先行或同块的真实前置 |
| --- | --- | --- |
| Project Provider/Model | 与System共用配置事务实现，但本块路径unbound | P已验Human Secret/Outbox gate；Model自己fact verifier委派D08Owner。无需改D08创建输入；Summary settings仍待产品答复 |
| Usage规范存储 | snapshots/bindings/calls/invocations/summary的DB原子reservation/observe/finalize可按已审§3/5推进，无Provider网络、无Jina；Reader使用已验D07Session+D08Owner同锁 | 必须同块给完整调用准备/ConsumerAuthority/真实call-fence/input计划及本域canonical authority，不接受任意Invocation DTO直接写成功；无consumer绑定拒绝。不能先造永远零的生产Usage服务 |
| Resolve/租约准备 | C0 current/serving计划；配置metadata+snapshot/binding+Secret Acquire同Tx | 真Model UsagePlanner；旧 Model lease Acquire/Release wrapper按**实际PurposeModel+execution/model_call**拒PreparationRequired、不可复活released。保MCP execution原义。此时统一排 secret/lease.go + usage_plan.go 所有权 |
| 每attempt材料读取 | 独立的 sc.CredentialUsageReader.ReadCredentialForUsage(ctx,UsageRequest) | 新 secret/contract/model.go；旧 contract/account.go只加RequestID及省略空值的兼容Binding；新 secret/model_usage.go；lease.go安全读取核提取。RequestID=exact Invocation；完整union/当前fact/Audit、Committed才材料。无RequestID不猜最新call |
| terminal/recovery | 已验D03 Unknown/ProcessGuard原则可复用 | identity/contract仅ModelRuntime闭集登记与测试；真实Model call/consumer terminal/join事实和P生命周期validators。类型注册本身不等于获得Owner，首块不提前登记无生产使用者 |

后续 Usage 表候选仍沿正式规格：snapshots immutable config/history；snapshot_bindings UNIQUE(snapshot,leaseOwnerKind,leaseOwnerID)，CredentialRef/LeaseID成对；calls exact Consumer/Input/RetryPolicy；invocations UNIQUE(call,attempt_index)，Process/fence、dispatch与terminal独立、usage六nullable字段与source；execution_usage_summaries可重建且与Observe/Finalize同Tx。reserve不计真调用、可靠usage不估算，历史live Provider/Model FK可NULL但snapshot identity不变；field known/unknown分母只确定sent。需独立短实施卡冻结 InvocationPlan/写入签名后才写，当前不新增这些表或空API。

## 7. 首块真实验收/可排程点

纯新源/closed Audit契约可在root单独授权后按固定基线准备；最终PG迁移/全结果验收等连续migration前缀与fixture交接。推荐同一backend单作者负责Model+Audit公共增量+候选迁移，避免构造/闭集在多个任务分叉；P保持其Project源所有权，Secret lease/write不在这张卡解冻。

必须真实验证：D07非admin/失效Session拒绝、同key换Session先授权再receipt、CRUD不可变字段、SecretRef wrong purpose/scope、引用释放与Secret删除竞争、Audit/Outbox故障整Tx回滚、commit丢回复三分支、模型替换与新增引用barrier、缺owner adapter不部分改写、首次platform配置/必需项不能清空、分页cursor改limit/撤权、Project分支unbound且不写任何域行。Schema fresh+前缀升级/非法NULL与组合CHECK/事务回滚，旧Audit行不破坏；无Provider请求计数伪实际模型执行。 另以真实提交故障验证 A rollback-unknown → 同稳定User/key/command的异义B先提交 → A确认：A不得返回B receipt，B仅一份Audit/Event；无行/确认失败保留原Unknown及cause，公开Lookup的历史结果不能当作A成功或回滚证明。

命令由后续实施者实际执行：Go1.27.1/local/mod=readonly 的 model/audit相关unit/race/vet/build；新 tests/model 纳入现有真实隔离fixture原完整包列表。作者PASS与独立验收分开；本准备未执行Go/PG/Docker/网络。

本卡原准备沿 `e6e94c4` 的 22 项实际源码输入及独立静态审查，原清单 `/tmp/agenteam-d09-b01-prep-q49oerfz/inputs.sha256` 与历史“未实施”结论保留在原提交。实现期间未消费活动 A/P/D12 服务源码；首轮真实组使用 `30f5c29` 固定 Git 来源加本块 34 源，精确来源差异、实际 argv、原日志、清理和检查见 `/tmp/agenteam-d09-b01-system-mh2pkw62/`。`pg1-result.json` 记录整条 exit0/163.387s，tests/model 12 个真实顶层 32.308s、internal/model 16 个纯顶层 1.045s；其余包的 no-tests 不计兼容。原 pure1 生产摘要编码错误及新增测试准备错误、首次 integration 编译错误、误 cwd 的排除执行均保留，不称首次全绿。独立 ModelEvents 定点核与 R01 同义并发重放修复已采纳；当前真实成功是作者证据，最终独立验收及 Git 交付由 root 收束。Project 分支、Resolver/Usage、Provider 网络调用及根 HTTP 继续不在本块完成范围。
