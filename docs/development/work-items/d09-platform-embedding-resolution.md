# D09：平台 Embedding current-selection Resolver

修订：rev2，2026-10-08。状态：仅修 rev1 完整独审的 D1/D2，待独立差量与最终组合 STATIC 及 root 采纳；当前只授权本卡和作者自有 scratch，未授权产品实施、测试、资源或生产装配。基线为 root 指定的 `main@5320a187`；本轮 Audit UI 活动产品不作为输入。作者只读分析 `e28547a8373ba9532f26610a3dc67715e93c263811a9dd3ea0bed9daff648268` 已获 root 采纳，采纳分析不等于本规格或产品通过。

依据：[D09 设计 §4](d09-model-system-token-usage-design.md#4-正式规划授权与-secret-组合)、[Model Resolution](../../architecture/platform-infrastructure/model-system/model-resolution.md)、[已接受 current-resolution](recovery-d09-current-model-resolution.md)、[已接受 S3](d09-system-meeting-summary-resolution.md)、[Embeddings float wire §2–5](d09-openai-embeddings-wire.md)、[开发计划 D09](../development-plan.md#d09-model-system-与-token-usage)。必读 [AGENTS](../../../AGENTS.md)、[团队流程](../agent-team/README.md)及角色对应的[设计](../../../.agents/skills/agenteam-design/SKILL.md)、[Go](../../../.agents/skills/agenteam-go-development/SKILL.md)、[验证](../../../.agents/skills/agenteam-verification/SKILL.md)技能。

## 1. 完整结果与真实依赖

现有四个 Resolver 方法为 `knowledge_embedding`、`memory_embedding` 解析当前 `platform.embedding`，只选已接受 OpenAI Embeddings float profile；在正式 consumer 事实、同一 Store 和完整锁内形成不可变 snapshot、consumer binding 及适用的 canonical Secret lease。Select 仍只是当前授权的安全选择读；最终 Resolve 的接受点仍为真实提交。

这是完整的 current-selection 库结果，用于未来 Knowledge/Memory 新索引构建目标的解析。已经 serving 的索引查询必须绑定真实 serving generation 的原 embedding snapshot；改变 selector 不改变其查询模型。本卡不接 `serving_snapshot`、业务 `Nonchat.Embed`、材料读取、Provider 发送、Invocation/Usage writer 或生产根，不造 IndexProfile/维度默认或成功 consumer。管理员统一会议 Summary initial/update 含首标题、Project 无 override/复制初值与原 compaction/Execution Summary 规则保持。

| 直接前置 | 已接受范围与本卡消费 |
| --- | --- |
| C0 `e6e94c4` | [D09 主卡](d09-model-system-token-usage.md)的 purpose/consumer、SelectionRef、ResolveRequest、ResolvedModel 与 opaque 计划；现源码已将两个 embedding purpose 映射到 EmbeddingSelector。只证明契约，非实际 consumer。 |
| System 配置 `543511c` / Audit `26622bc` | [B01-K](d09-b01-system-configuration.md)的00015、System Provider/Model、四用途 singleton/version、引用与原子替换；[System 根验收](../agent-team/system-model-root-verification.md) `457b197`补充其真实配置组合。无网络能力推断。 |
| current-resolution `4295df7` + 当前 S3 `baa6ffac` | [原 Resolver 验收](../agent-team/current-model-resolution-verification.md)的00017、preparation/snapshot/binding/Secret Acquire，以及[S3 验收](../agent-team/system-meeting-summary-resolution-verification.md)的共享源码最终组合。保留 direct/Memory/Summary，不用旧429覆盖S3。 |
| Secret planned usage `8ad6759` | [正式验收](../agent-team/d09-secret-model-usage-verification.md)与上述 Resolver/S3 实际组合的同 Tx Acquire/witness/canonical lease；本卡不消费 read/release/retire。 |
| 当前 Account/Project gate | D07 `022dcea`/关闭`0ed8085`与D08 B02 `6319d03`的真实 Session/Owner/gate已由上述 Resolver/S3组合验收；[Owner读验收](../agent-team/project-owner-read-http-verification.md) `901eb546`仍仅有限读结果。生产Skills、创建、完整生命周期并非前置已完声明。 |
| float wire `2debfdde` | [完整限定验收](../agent-team/openai-embeddings-wire-verification.md)的精确 profile/revision/能力边界；这里只复用已验协议闭集，不重新宣称供应商smoke、维度事实或业务Embed已绑定。 |

本库没有必须先解除三停止的上游实施依赖。D13/D14 的真实业务事实/授权及维度、serving generation/长期 Secret 保留，D09 Runtime/Facts/Invocation 和实际调用仍是后继生产绑定的必需前置，不能把测试隔离解释成已经满足。

## 2. 已核接缝与唯一候选写域

当前 `model/resolution_policy.go` 的 `resolutionVariant` 只允许 direct、platform.memory、platform.meeting_summary；`resolutionProfile` 只支持 OpenAI Chat，`currentResolutionDraft` 的旧四用途分支总读 Memory。该文件需增加本卡闭集，分别读取 Embedding/Memory 并选择已接受的 float revision。

公共 `contract/configuration.go` 已有两 purpose→EmbeddingSelector 映射；`contract/resolution.go` 已拒非chat reasoning与current/serving混用；`resolver.go`和`resolution_plan.go`已对所有platform持 `model-platform-selection` SH。现format1私有DTO和00017三表不限制Chat类型。因此本卡不改公共签名、contract、原计划/存储格式或迁移。发现其中真实必要缺口须先提交具体调用/编译/反例证据给root提高修订，不能按预留写权扩展。

以下8技术路径与1末件共9路径仅为后继单一 backend 作者候选，须本卡独审及root明示交接后才可写；本轮作者只写本卡。

| # | 精确路径 | 责任 |
| --- | --- | --- |
| 1 | `internal/central/model/resolution_policy.go` | variant、当前四用途字段分派、strict embedding profile；唯一既有产品改动 |
| 2 | 新 `internal/central/model/platform_embedding_resolution_test.go` | 无I/O entry/结构拒绝、完整profile矩阵及旧分支对照 |
| 3 | 新 `tests/model/platform_embedding_resolution_fixture_test.go` | 正式身份/真实服务组合、两个purpose严格持久事实、同Store锁与清理 |
| 4 | 新 `tests/model/platform_embedding_resolution_selection_test.go` | 两purpose当前选择、错误/版本/System边界和零副作用Select |
| 5 | 新 `tests/model/platform_embedding_resolution_atomicity_test.go` | selector/替换竞争、原子接受与canonical lease |
| 6 | 新 `tests/model/platform_embedding_resolution_authorization_test.go` | 当前身份/Owner与真实subject/operation/input、完整计划和同Tx反例 |
| 7 | 新 `tests/model/platform_embedding_resolution_replay_test.go` | prepared重规划、committed历史、重启/新Session/新Call与改义 |
| 8 | 新 `tests/model/platform_embedding_resolution_unknown_test.go` | prepare/final Unknown、原writer屏障、取消和真实后态 |
| 9 | `docs/development/backend/README.md` | 技术接受后root另授的能力/未绑定说明末件，不提前写 |

原 Resolver/S3/adapter/contract测试均只读。Account/app、HTTP/OpenAPI、web/JS、全局dist、迁移序列、锁文件、脚本及共享fixture源不在写域；与Audit UI独占路径无产品重叠。实际Go输入/缓存及PG/MinIO窗口仍须root协调，目录独立不授权并发资源。

## 3. 请求、选择与严格profile

保持 `SelectModel(ctx, actor, SelectionRequest)`、`DiscoverResolve(ctx, ResolveRequest)`、`ResolveModelInTx(ctx, tx, request, plan)`、`ResolveModel(ctx, request)` 四签名与原构造依赖。`SelectModel`仍只接受Human当前Project读授权，不以Select结果或裸SnapshotID授予Resolve。

| 请求项 | 本卡规则 |
| --- | --- |
| Source/selector | `current_selection`；`SelectionRef{Kind:"platform",Selector:EmbeddingSelector}`，ProjectID=nil，Version可省略或正数；ModelRef=nil，ServingSnapshotID=nil，ServingGenerationID为空 |
| Knowledge | `KnowledgeConsumer` + `KnowledgeEmbedding`；必需ProjectID/OperationID；AgentID/ExecutionID/MeetingID均无 |
| Memory | `MemoryConsumer` + `MemoryEmbedding`；必需ProjectID/AgentID/OperationID；ExecutionID/MeetingID均无 |
| Purpose/owner | request.Purpose等于Consumer.Purpose；原`model_call` owner ID为该逻辑CallID。两个purpose都不建立Agent Execution；显式execution owner与其结构不符，沿原输入拒绝 |
| Actor/effort | Actor沿C0完整匹配；合法Actor不证明当前权限或操作存在。非chat ReasoningEffort必须空，非空在现`ResolveRequest.Validate`即InvalidArgument，不伪称Chat的unsupported分支 |
| 其它选择 | direct完整保留原C0合法agent/tool chat用途（含ToolConsumer/ApprovalAuto）；错purpose/selector、额外ModelRef或非法结构为原InvalidArgument。结构合法的serving_snapshot及旧project_summary仍DependencyUnbound，reranker/image等未交付variant仍CapabilityUnsupported |

只读原 `loadSelection` 的四用途singleton；该行缺失或Configured=false沿原 `INVALID_STATE`，不是optional absent成功。使用Embedding字段与该行version；新Select/尚未提交Resolve的显式Version不符为`RESOURCE_BUSY`。旧四用途任一配置改变都会改变同一version，即使embedding ID未变也必须按原版本语义处理；Summary独立version不混入本分支。SQL/缺表失败沿原DependencyUnavailable，已授权目标缺失沿NotFound，缺Provider沿原unavailable；不存在或错误配置不选备用模型。

目标只能是System Model及其System Provider，不能借Project模型、同名或裸ID fallback。Provider或Model disabled为InvalidState；合法但不支持的协议/参数/能力为CapabilityUnsupported。profile精确对齐[已验float wire](d09-openai-embeddings-wire.md#3-支持闭集体积与一次请求)：Protocol=`OpenAIEmbeddings`、Profile=`OpenAIEmbeddingsV1`、ModelType=`EmbeddingModel`、AdapterRevision=`OpenAIEmbeddingsFloatRevision`；input恰`[text]`、output恰`[vector]`，Streaming/ToolCalls/ParallelToolCalls/Reasoning=false，ReasoningEfforts/StructuredOutputModes为空，MaxOutput=nil。Options/Parameters/RequestOverwrite严格空对象、HeaderOverwrite空；不剥掉实际能力凑通过。ContextLength只保C0合法正值，不估token或供应商容量，不新增型号默认。

`ExpectedDimensions`不在Selector/ConfigSnapshot/ResolveRequest中，是后继受信调用者从其已接受配置/IndexProfile事实取得并冻结的本地结果要求，由D13/D14对应profile/generation负责。原wire的1–4096及总values界限是工程上限，不是型号默认或兼容证明。本卡不从响应向量推断维度，不增平台字段，不发送原生dimensions；仅有本卡snapshot仍不足以调用业务Embed。当前selector只确定新构建目标，serving查询的原模型绑定继续由其正式Source variant完成。

## 4. 原锁、事务、历史及Unknown

完整行为继承[原Resolver卡](recovery-d09-current-model-resolution.md)与[S3 §4–5](d09-system-meeting-summary-resolution.md#4-授权锁与跨域事实)，本节只固定新增分支必须遵守的接缝：

- 使用原Command/`model-references` SH/Project gate、四用途`model-platform-selection` SH、实际Provider/Model SH、本域preparation/snapshot记录、完整consumer锁和适用Secret锁，一次原有union排序；InTx先核sameStore/活Tx/完整RequireHeld及opaque issuer/完整request与mapping绑定，沿调用者同一Tx执行必要SQL读写和planned Secret Acquire；禁止补锁、开内层Tx及Provider/外部网络I/O。selector更新与Model删除替换通过原EX串行，锁后重读；当前权限先于候选配置存在性错误。
- prepared仍保持原SnapshotID；配置draft改变按原算法重新Discover并推进plan_version。consumer事实/mapping改变由其authority重新发现和重验，使旧plan失效，不据此承诺Model plan_version必然增加；显式旧Selection.Version不因重规划自动替换。committed同Project+model_call单位保持原snapshot、selection version、endpoint/parameters/capabilities/revision与lease；新合法Call才取当前配置。旧direct/Memory/Summary format1、稳定semantic与bytes不改。
- 持久semantic继续以稳定initiator区分，内存binding含完整Actor/Session。新Session同义重入仍核当前授权，旧plan不能跨Session/实例使用；当前授权与新请求事实成立后，同Call换purpose、Project内另Operation、Memory Agent、selector或其他原semantic字段拒IdempotencyKeyReused，不能自动换CallID逃避。另一Project隔离沿原身份规则，不发明全局CallID唯一。
- 有System CredentialRef时沿同最终Tx的planned Acquire及exact-Tx witness取得metadata；同Call/ref复用canonical LeaseID，不同Call独立、released不复活。无ref只代表解析无lease且零Secret调用，不宣称以后可以匿名发送。input接受事实、snapshot/binding/lease全提交或全回滚；错误不可吞成合法提交，InTx返回只属provisional。
- `ResolveModel`只在Committed且原ctx仍有效后返回；prepare/final Unknown沿原cause/attempt与writer屏障确认，未确认/取消/锁忙不返plan或ResolvedModel，不因缺行猜rollback，不后台重发。真实Store commit/rollback结果装饰及普通writer锁竞争足够本卡验证，不新增物理丢ACK/网络故障方案。取消返回不是writer/join终局。

不新增goroutine、后台恢复、全局Drain、deadline续期、Secret材料读取/退休或lease释放。若上述不变量的现有实现出现新失败，保留原结果并按所属源码所有权处理，不能在本卡静默修宽域。

## 5. 两purpose的真实持久授权fixture

新fixture在独占PG中组合真实Account/Project/Model/Secret/Audit/Outbox和同Store。正向身份沿已验 `meetingResolutionIdentity` 的Bootstrap/Invitation/Redeem/Login与restricted recovery sink及真实Logout/新Login；可复用其身份助手与`assembleProjectConfiguration`，不能调用旧直接INSERT users/sessions的便利路径当作本卡正向身份证明。Project.Create及严格Skills测试事实的既有边界保留，不宣称生产初始化已完成。

仅未实现的Knowledge/Memory owner事实放入新测试schema，以独立列/关系保存，不存一份ResolveRequest JSON当授权：

- Knowledge保存同Project的真实测试subject及其active/version；Memory另有同Project Agent归属/当前可用状态与版本，subject精确关联该Agent。两种subject严格互斥；这些是明确的测试canonical事实，不是新生产领域表。
- operation以稳定OperationID关联subject，保存CallID、精确purpose、稳定Human initiator、当前构建目标用途、input identity/digest/schema version、phase与version。实际input来自operation关系，不能让caller提供digest即算一致；测试标签不规定D13/D14未来SQL字段或业务状态机。
- Discover绑定完整request、以上实际行/version与原锁；ValidateInTx先核sameStore/RequireHeld及当前Session/Project Mutate，再从关系重读kind/Project/Agent/subject/operation/CallID/purpose/initiator/input/当前许可及mapping。Memory必须持真实Agent锁，Knowledge subject和operation使用现有正式锁族；不能只验请求序列化相等、UUID存在或Owner身份。
- 同外层Tx保存本次operation/input与snapshot对应的接受事实，和Model/Secret一起验证提交/回滚。prepared→committed或历史重入不要求live selector仍指原模型，但当前主体/操作仍需合法；已取消、终结、失权或输入版本漂移不能借历史snapshot取得许可。

fixture的两purpose正向使用真实Human Session；Select的nonhuman拒绝保持。production后台Service/真实Agent授权未绑定，不用fixture注册新权限，不称Service或D13/D14真实消费者通过。反例必须是结构合法、能到目标检查的坏事实，记录目标检查已达和零副作用；本域坏行只做测试负例并恢复/回滚，不直接改Model/Secret正例绕开服务。

## 6. 适用验收与精确top

纯测试新增 `TestModelPlatformEmbeddingResolutionProfileMatrix`、`TestModelPlatformEmbeddingResolutionEntryGuards`：覆盖两个purpose、结构和错误优先级、完整profile正反/MaxOutput/ContextLength、unbound/取消零返回、clone与安全投影、原direct（包括ToolConsumer/ApprovalAuto代表）/Memory/Summary闭集。现public类型及原unit断言保持，不为本卡改旧预期。原float adapter固定输入的`TestOpenAIEmbeddingsProfileAndSafeViews`、`TestOpenAIEmbeddingsPrepareAndIsolation`结果可复用；新snapshot的闭集逐项静态/纯验证，不能用Start/Do做隐藏网络检查。

| 精确新顶层 | 必须观察的完整结果 |
| --- | --- |
| `TestModelPlatformEmbeddingResolutionSelection` | 两purpose真实Select/Resolve；singleton未初始化/未配置、当前与显式version、四用途其它项改version、独立Summary不混版本、strict profile与disabled/System限定；Select零preparation/snapshot/lease，普通Owner可读且admin无Owner旁路，配置错误不早泄漏 |
| `TestModelPlatformEmbeddingResolutionAtomicity` | 两purpose有/无ref；真实UpdatePlatformSelection/跨Provider删除替换与原SH/EX竞争，实际PG holder/waiter证据；prepared后变化拒旧plan；外Tx input接受+snapshot/binding/lease提交/回滚，poison不可吞；同Call canonical/不同Call独立，零SecretResolve Audit |
| `TestModelPlatformEmbeddingResolutionAuthorization` | 正式Logout/另Owner/项目gate，Knowledge subject与Memory Agent/subject的Project/phase/version，operation/purpose/CallID/initiator/input漂移；仅Owner不足。缺低序/selector/Agent锁、弱模式、foreign issuer、同DB异Store/外Tx/失效witness/篡lease拒绝；反例到目标检查且零最终副作用 |
| `TestModelPlatformEmbeddingResolutionReplay` | 两purposeprepared重规划保SnapshotID并使旧plan失效；committed后selector切换、模型/Provider停用、更新endpoint、合法删除替换与重启仍原snapshot/lease，新Call取当前值；新Session重新授权，旧plan拒；同单位改义、released lease拒复活，旧三分支字节/历史保持 |
| `TestModelPlatformEmbeddingResolutionUnknown` | 两purpose的prepare/final真实commit和rollback后结果装饰Unknown；原cause/attempt、零返回、真实持久后态；原Command writer锁未终局不得凭缺行确认，释放后同key查证；提交后交付前取消零候选，所有SQL/并发holder实际终结。不是物理ACK丢失证明 |

必要旧真实top为原Resolver六组：`TestModelCurrentResolutionSelection`、`TestModelCurrentResolutionReplay`、`TestModelCurrentResolutionAtomicity`、`TestModelCurrentResolutionCanonicalLease`、`TestModelCurrentResolutionAuthorization`、`TestModelCurrentResolutionUnknown`；另跑S3受policy影响三组：`TestModelMeetingSummaryResolutionSelection`、`TestModelMeetingSummaryResolutionAtomicity`、`TestModelMeetingSummaryResolutionReplay`。S3其余授权/Unknown、00017 schema、Secret与wire未变证据按已接受输入/语义复用，不机械重跑全Model，更不运行tools/停止probe。

独立验收至少两组自有增量：A核两个purpose的真实归属/输入撤权和selector锁竞争；B核接受后的历史snapshot/lease及Unknown/取消原子边界。应有不同构造或触发/断言，不能只有作者top复跑。所有复用须匹配当前冻结版本，失败后只重跑受影响项并保留首红。

## 7. 源隔离、命令与资源门槛

这是browserless Go库卡，规格阶段无执行权。实施/验证以停止写入的8技术源、固定基线必要依赖及实际import/embed/test/helper/动态构建root确定输入；不读Audit UI活跃源、不复制全树或反复生成大型图。共享fixture即使`-run`过滤仍可能编译其它测试包、TestMain或动态两cmd，必须据原启动链冻结实际用到的固定版本，不能靠测试名断言无依赖。图/指纹只服务选定roots，语义不变可复用已有闭包记录；活动源阻碍冻结时等root提供固定输入。

Go精确1.27.1、`GOTOOLCHAIN=local`、`GOWORK=off`、`GOPROXY=off`、`-mod=readonly`；独占task cache/TMPDIR与输出。单离线命令45s，执行受影响unit/race/vet、integration race编译与精确`-list`、原fixture实际要求的两cmd/helper编译；两cmd/helper只编译不启动，pure与list的实际调用分列，发现不算PG测试体通过。pure选择本卡两个top及原`^TestModel(CurrentResolutionProfileClosedMatrix|MeetingSummaryResolutionProfileMatrix|CurrentResolutionConstructionAndClosedVariants|MeetingSummaryResolutionEntryGuards|CurrentResolutionRegistrationAndTypedNil)$`，均锚定整名。可分包/复用缓存，不能放大命令到native/browser/全仓测试；gofmt只授权Go文件。

真实执行必须root另交唯一窗口，使用原 `scripts/test-objects.sh -run '<整名锚定selector>'`，不使用无过滤的test-models.sh。新五组selector为：

```text
^TestModelPlatformEmbeddingResolution(Selection|Atomicity|Authorization|Replay|Unknown)$
```

旧两组selector为：

```text
^TestModelCurrentResolution(Selection|Replay|Atomicity|CanonicalLease|Authorization|Unknown)$
^TestModelMeetingSummaryResolution(Selection|Atomicity|Replay)$
```

root可按原每包6m总预算将这些固定整top分轮，保持`-tags=integration -race -count=1 -p=1`。新每top120s含所有t.Cleanup，以实际RUN→PASS/FAIL流式watchdog核实；内层沿现有`testContext`20s或更短原caller，不重开业务deadline。root发出的确切argv/env、固定输入与资源授权是运行依据，本卡不是一次性授予所有轮次。

沿已接受fixture的PG17.x（最低17.8）正向、PG16不支持版本基线、固定MinIO、nonce/labels/exact IDs与原环境门禁（含fresh≥5GiB），不新迁移或修改fixture配置。driver可能起配套MinIO/私网容器，但本卡不发Provider请求、不取外部来源。资源与Audit UI等真实轮串行；每轮actual direct/adopted wait、holder/watchdog真实join、source前后hash、owned PID/starttime及精确资源双次清理都须记录，daemon/PID1非owned差量另列，不称全机清零。FAIL先退役并交root，不自动重试、改预算或扩大触发。

## 8. 后继责任、升级与交付

D13/D14负责真实Knowledge/Memory操作和input authority、IndexProfile维度/构建generation、serving查询来源及长期Secret保留；D09后续Runtime/Facts与每真实attempt的Invocation/Usage负责材料/发送/actual join与重试收敛；实际消费者同Store装配和生产根须另卡独立验收。本卡通过只关闭平台embedding当前解析缺口，`Authorizations.Resolution`和`Invocations`生产仍nil，ready503、完整D09/D13/D14/D28与E01状态不扩。

Object runtime join repair（原auto-review possible cybersecurity risk）、OpenAI tools独立验收、SPA concurrent-publication三停止不重试、不改名拆卡/转派或间接恢复；Jina停止取证也不重开。E01仍待D28。出现公共contract/plan/store必改、未授权依赖、未知业务规则、所有权冲突或无新证据的反复失败，只暂停受影响部分并交root，不造fallback。

交付须8技术路径的作者自测、独立限定验收、原失败与复用映射、实际资源终局，之后另授README末件并由root接受完整9路径。作者本次STATIC只核稳定源码/正式依据、路径/链接和范围一致性，封card/freeze/来源摘要后STOP；不称独立SPEC通过，不在规格未验前实施。
