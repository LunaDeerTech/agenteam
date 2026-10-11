# 恢复：Project 与既有领域的真实权限和停止绑定

修订：rev2；澄清构造可判定范围，以及真实取消的 archiving 收尾规则。rev1 固定 SHA-256 为 `5c3b80bb80613452eaf7c3b57fdee55bfbd5cae0ff9f51cdfe82c6eda4b841e6`，原稿已留存。状态：规格已独立静审并由主线程采纳；本卡尚不授予业务写权。完成结果是可组合验证的真实 Artifact/Object 权限与 Audit、Outbox 普通路由及四个 Project stop adapter，供后继生命周期推进使用；不是 Project archive/Restore/Retry、Cleaner、HTTP 或 root 已完成。

## 固定输入与实施前置

生产核查固定 `fda0a35`，复用已验 R1 registry、R2 durable acceptance、R3 facts Authority、Object S2、Secret checker 与 Project Secret 绑定。必读 [AGENTS](../../../AGENTS.md)、[团队流程](../agent-team/README.md)、[设计技能](../../../.agents/skills/agenteam-design/SKILL.md)、[D08 设计](d08-project-owner-design.md)、[R3 卡](recovery-d08-lifecycle-authority.md)、[S2 验收](../agent-team/d05-s2-verification.md)及 [Project Secret 验收](../agent-team/project-secret-audit-binding-verification.md)。本卡不改变既有命令摘要、版本、完成重放与 Unknown 规则。

以下保留原固定规格来源，并同步已验能力；前置已验不代表本卡已实施：

| 前置规格 | 原规格与已验提交 | 本卡消费的接缝与开写门槛 |
| --- | --- | --- |
| [Object Audit rev2](recovery-object-project-audit.md) | 原规格 `fcae355`；已验 `a716ae2` | Store-only checker、真实私有同 Tx witness、六类动作及 revoke UPDATE→Audit 顺序；13 源已独立验收，实际接缝已完成本轮有界静态相容性核对 |
| [Artifact stop rev2](recovery-artifact-project-stop.md) | `fcb83fc` | `NewWithProjectStop`、真实 source 权限、整次 lifetime/join、`ObjectProjectStop` 与 Artifact component 报告；须独立验收并固定最终 16 源 |
| [D09 Project 配置](recovery-d09-project-configuration.md) | 原规格 `6e0bda1`；已验 `de00c610` | `events.go` 的 Model Append 分派及独立 issuer/purpose 校验；21 源已独立验收，实际接缝已完成本轮有界静态相容性核对，旧文件写权仍由主线程另授 |

Object Audit 与 D09 的已验接缝相容性已由主线程采纳，Artifact stop 最终验收与接缝 delta 仍未闭合。[Object Runtime join 修复](recovery-object-runtime-join.md)按 `2ca7d86` 的中断记录保持停止，退出缺陷尚未修复，本卡实施仍被阻塞。实施者只能在主线程确认剩余前置后，对本卡涉及的 authority/owner/access/Audit/stop/events 接缝作最终 delta 静审，再冻结实施快照和精确写权。出现签名、锁集合、当前 gate 或 witness 差异须先修订本卡，不沿规格预设虚构已存在 API。原初查时 Artifact 16 源、Object 13 源和 D09 21 源各属其作者；已提交不等于本卡获得旧文件写权，实际写权仍由主线程逐项另授。本卡无需迁移，不分配 `00017`；未来 operation completion plan 留给 R4 正式卡。

## 结果与非目标

真实组合必须支持：当前 Owner 的 Artifact 创建/上传/读取与对应 Audit；Object 原普通、writer 收尾、恢复、transfer 与 S2 stop 调用点经真实 Project gate 和 Object checker；D06 普通 Deliver/Inspect/Requeue；冻结 manifest 的 `artifact-object,secret,outbox,audit` 四个 stop adapter。缺组件、错 Store、错 cause 或缺锁均明确失败，不能由 Service 名称和 UUID 代替事实。

本卡不实现生命周期 worker/claim、operation 推进、Restore/Retry、completion Event、物理 Project 删除、Project Cleanup、AgentRun/Execution 权限、Knowledge/Skill 来源或 app 装配。Secret lease 使用者仍由其真实工作拥有者负责停止。四个 adapter 的 `Cleanup` 全部保持 `DEPENDENCY_UNBOUND`，不得触碰下游 Cleaner；registry 可解析不表示 cleanup 可执行。

## API 与无环构造

下文 `ac` 为 Artifact contract，`obj` 为 Object contract，`ob` 为 Outbox contract，`c` 为 Project contract。复用原方法签名；新增 API 仅如下。

```go
// internal/central/artifact/contract/convergence.go
type ConvergenceFacts interface {
    Discover(context.Context, AccessSubject) (obj.AccessDependencies, error)
    DiscoverInTx(context.Context, foundation.Tx, AccessSubject) (obj.AccessDependencies, error)
    CheckInTx(context.Context, foundation.Tx, AccessSubject, obj.AccessDependencies) error
}

// internal/central/artifact/project_convergence.go
type ConvergenceFactStore interface {
    Store
    RequireHeldLocks(context.Context, foundation.Tx, []foundation.LockRequest) error
}
type ConvergenceFactAuthority struct { /* private immutable Store */ }
func NewConvergenceFactAuthority(ConvergenceFactStore) (*ConvergenceFactAuthority, error)

// internal/central/project/artifact_authority.go
type ArtifactAuthority struct { /* private *Authority and ac.ConvergenceFacts */ }
func NewArtifactAuthority(*Authority, ac.ConvergenceFacts) (*ArtifactAuthority, error)
// ArtifactAuthority implements ac.Authority, including its existing four methods.

// internal/central/project/domain_participants.go
func NewArtifactObjectStopParticipant(*LifecycleAuthority, obj.ObjectProjectStop) (c.ProjectLifecycleParticipant, error)
func NewOutboxStopParticipant(*LifecycleAuthority, ob.ProjectLifecycleParticipant) (c.ProjectLifecycleParticipant, error)
func NewSecretStopParticipant(*LifecycleAuthority) (c.ProjectLifecycleParticipant, error)
func NewAuditStopParticipant(*LifecycleAuthority) (c.ProjectLifecycleParticipant, error)
```

新增构造自身零 SQL、零 goroutine，捕获不可变依赖。nil/typed-nil 拒绝为 `DEPENDENCY_UNBOUND`；本包能检查私有状态的具体 `*Authority`、`*LifecycleAuthority` 还须拒绝非 nil 零值。新 `ConvergenceFactAuthority` 自己的方法也须在读取 state 前检查零 receiver/零 state，返回 unbound。其余 opaque 接口（Store、ConvergenceFacts、两个下游 stop port）构造仅能检查 nil/typed-nil 与接口公开的声明，不能承诺识别任意包装器内部的零 Service。`obj.ObjectProjectStop` 没有 Valid/Name；Outbox 的 Name 只核固定名称，已提交零值 Outbox Service 的 Name 也返回 outbox，不能当健康证明。禁止为此反射私有 state、试调 Request/Inspect 或扩大旧 Service API；真实组合必须注入其正式构造成功的实例，实际调用仍须完整校验错误与报告。

事实 checker 的 Store 必须支持既有 postgres 的实际 held-lock 检查；只新增上述窄构造能力，不修改 Artifact 旧 `Store`。所有 InTx 方法用各自 `Store.InTx` 验证同一活 Tx，跨 Store（包括同一 PostgreSQL 的不同 Store 实例）拒绝。不能用反射比较 panic、类型名称或数据库 DSN 冒充实例归属。opaque stop 依赖的 Name/实际调用若同步 panic，以固定安全 unavailable 结束，零 stopped 报告、不披露原 panic，也不推断副作用已回滚或资源已 join；实际 stop 调用只在前置短 Tx 终结后于 Tx 外包此边界。任何 InTx 权限/事实调用的 panic 必须令原事务失败、先回滚，不能在持 Tx 回调中 recover 后继续提交。

构造顺序：同一实际 Store → Secret/Object Audit checker、Artifact Convergence checker、`LifecycleAuthority` → `Project.Authority`（复制 Secret/Object 两项 AuditFacts）→ `ArtifactAuthority` → Audit/Outbox Service、Artifact OwnerProvider → Object Service → Artifact Sources、带 stop 依赖的 Artifact Service → 四个 stop adapter → 原 R1 registry。Object owners/planner/gate/read/cleanup 与 Artifact source 前置授权均接真实 OwnerProvider；Artifact `Sources` 接真实对象与 Audit，不能把 `Sources` 错当 ResourceAuthority。Artifact stop 的 `Sources` 依赖在本结果中接真实 OwnerProvider，resolver 接真实 Artifact Sources；未知 owner/ref 明确 unbound。生命周期 typed port 仍接 R3 `LifecycleAuthority`，不能改接普通 Artifact adapter。没有 Authority 反向依赖后构造 Service、可变注册表或 setter。

## Artifact 当前权限与取消事实

`ArtifactAuthority.Discover` 验 `AccessSubject`，生成规范 `AccessDependencies`：Project SH；Human 再加 User SH；精确绑定完整 ActorDetails（含 Session）、ProjectID、Maintenance 与四个 provenance 字段。非 AgentRun 的非空 provenance 拒绝；AgentRun/Execution 尚无真实 provider，返回 unbound。发现不是授权，不返回用户数据或复用旧 AccessGrant。`DiscoverInTx` 在同 Store/活 Tx 下重算完整 mapping 与锁集合、验证已持锁；不能补 Acquire/嵌套 Tx。原 Artifact/Object 消费者仍把所有源、目标、命令、owner、Object 与这些依赖组成一次规范 union，取得后比较完整 discovery，再调用授权。

| Subject / intent | 当前事实与结果 |
| --- | --- |
| Human Read | 同 Tx 当前 Session/Owner；initialized 且 active/archiving/archived；当前 Project version 与数据库时间构造精确 grant |
| Human Mutate | 同上且 active；包括新增上传/创建；管理员无 Owner 旁路 |
| Human Converge | 当前 Session/Owner、initialized、active/archiving/archived；仅提供已存在工作的 Project 收敛部分，包括原 prospective upload/creation 取消与现存 transfer 的 Cancel/ConfirmTerminal/GET Capture。实际 original actor、owner/target、原 Runner/Operation/evidence 仍由 Artifact/Object 原入口核验，不发 Read/Mutate、不创建新工作；deleting 不走此普通入口 |
| ObjectMaintenance Converge | 完整 service actor 与 Project 匹配、无 provenance、真实已持久取消事实与精确 owner/target/锁成立，且 initialized、active/archiving/archived；只给该取消的 Project 级 Converge，不给 Read/Mutate/一般 Lifecycle。archiving 为下述待独立审的正式状态规则窄澄清；deleting 与 Project Cleanup 仍 unbound |
| `Maintenance=true` | 仅 actorless discovery，空 Actor/provenance；Project SH 进入原维护计划，不套 ordinary Mutate gate 阻止旧 writer 收尾；传入 Authorize 一律拒绝 |
| ProjectLifecycle、其它 Service、Lifecycle/Launch/Resume intent | 普通 Artifact Authority 不提供此资格；typed stop 继续由 R3；Project cleanup 接口保持 unbound |

`ConvergenceFacts` 只接 ObjectMaintenance、合法 Project、合法原 cause、无 Maintenance/provenance 的 subject。本卡只绑定已持久 `upload_intents`/`commands` 取消（`cancel_requested|cancelled`，精确 `project_id + target_upload_id == Actor.CauseRef`）；`cleanup` 行不是本卡放行依据，Project cleanup 分支仍 unbound。禁止查 Project/Object 私表。

发现用 Artifact 自有记录解析真实 owner/artifact ID、原 command/upload 身份与 target upload。锁集合为这些真实记录已有 `artifactLock(actualArtifactID)` 的去重 EX 集合，不凭 cause 猜 owner、不仅返回 Project SH。mapping 含完整 subject 及排序后的本域记录种类、主键、Project、artifact、target upload、原创建 cause/command 身份；允许取消状态在上述两个合法值间推进，不能令不同目标/新 cause 复用计划。多条匹配必须属于同一真实 owner/target，冲突或坏行返回 unavailable；无取消事实返回 Forbidden。

Project adapter 把该 discovery 完整并入原 union；同 Tx 的 `DiscoverInTx` 与 `CheckInTx` 重读本域事实、核 expected mapping/locks 并由实际 Store 检查所有 record EX 已持有。Project adapter 另核 Project SH/当前 gate，再调用 checker，成功后才发 Converge grant。Authorize 没有旧 plan 参数，须在同 Tx 重做 discovery/check；缺锁失败，绝不临时加锁。这个端口只证明 Project 级取消资格；既有 OwnerProvider 对实际 owner/object/target、prospective original actor、CleanupCause 的检查全部保留。不能复制一个 `EXISTS` 就声称精确资源授权已完成。

固定输入的 `OwnerProvider.checkTechnicalCause` 只核 Project/cause 的存在，尚未把取消行关联到本次 owner；这是已确定的事实匹配不足，本卡不据此声称已证明可到达的越权。`owner.go` 仅补此窄校验：`AuthorizeOwnerInTx` 的 Service 路径先取得请求 owner 对应的真实 ownerFact，核其 Project；`CheckInTx` 的同一路径也取这个真实 ownerFact，再把它与 actor 交给包内 technical-cause 检查，不能只传 pid。取消分支须同时匹配本次真实 artifact ID（commands.artifact_id 或 upload_intents.id）、原 target_upload_id/cause 与合法取消态；本次 owner/target 不符即拒绝，再调用 Project Authority。原 Human 授权顺序、grant.Matches、最小 terminal receipt、原 Project cleanup 分支不变，不在 OwnerProvider 旁路当前 Project gate；Project cleanup 仍被本卡 Project port 拒绝。只消费原完整 union 中的 owner record EX 与上面新增事实 discovery 的锁，不补锁。合法取消及跨 owner/cause 反例均须走真实 OwnerProvider 消费路径，不能只测 checker。

固定 `fda0a35` 的实际取消主链是 `Artifact.CancelUpload/CancelCreation` → 首个 Human Converge Tx 持久 `cancel_requested` → `finishCancellation` 保留原 Human actor 调 `Object.CancelUpload` → Object 以 Human Converge 撤销 reservation、`stopWriters` 后通过 actorless maintenance `cleanObject` → Artifact 的末个 Human Tx 写 cancelled。该主链并非必经 ObjectMaintenance，不能把两者状态不同直接写成每次取消必卡死。另一路真实 `Object.CancelUpload(ObjectMaintenance)` 的 `checkCancelMaintenance`、`DeleteUnreferenced` 或 `ReleaseForCleanupInTx` 会调用 Artifact `CheckCleanupInTx(CancelledUpload)`；后者核 owner/object/upload 后生成 ObjectMaintenance actor，再调 Project Converge。rev1 规则在这里拒绝 archiving，不能承诺两条既有取消/恢复路线均可收敛。

本卡推荐且主线程已采纳为待审候选的有界澄清：只把有真实已持久取消事实、精确 owner/target、完整锁的 ObjectMaintenance Converge 纳入 archiving，使 Human 取消确认后或 active→archiving 竞态中的既有取消补偿能沿原 cause 收尾。只改 `artifact/contract/authority.go` 的 Authority 状态职责注释，将该取消分支明确为 active/archiving/archived；不改接口签名、不新增删除/Project cleanup 授权，既有 authorized deleting lifecycle 仍须另行真实绑定，本卡 unbound。这里的未发布对象取消补偿不是 Project Cleanup，也不允许 Artifact stop 为归档调用 CancelUpload/CancelCreation 的物理清理路径；该正式 stop 禁令保持。共享 contract 现在不改，须本卡独立审通过、Artifact 最终冻结/delta 交接后方可实施。

## Project Audit 分派

`AuditFacts` 只允许 SecretProducer/ObjectProducer，复制 map、拒绝 nil 和未知 producer；Object 分支只在其真实 checker 前置通过后开放。原 Secret 分支与其实际 CheckMutation delegate/receipt/witness 保持。不存在将 Artifact/Outbox 塞入该事实 map 的兜底路径。

全部本卡 domain Audit 先核 Project scope、producer/action 对应、当前同 Store Tx 与 Project SH，再核当前 Project。initialized 必须成立；已进入 delete `cleanup_stage=audit|final` 的 operation，无论 cleaning/failed/Unknown 后恢复，均拒绝新增 Project Audit，不重开屏障。阶段和 operation 由 Project 自有严格记录读取，坏事实拒绝；本卡不推进这些阶段。Project 自有 action 的既有专用分派保持，后继 completion/Retry 须另遵守同屏障。

Object 只接 ObjectProducer/ObjectService、scope 与 Actor.ProjectID 一致、Actor.CauseRef 精确等于原 key cause；动作、resource、ordinal、outcome、metadata 采用 Object 卡闭集。服务 actor 或 metadata 中的历史 initiator 都不是当前业务权限证明。以下为 Project 的必要 gate，成功仍必须把原 context、原 Tx、完整 Entry/Key 原样交给真实 checker；不得重造 context 丢私有 witness，或先生成公开授权值再绕过 checker。

| Object 动作 | 必须满足的当前 Project gate；真实上游事实仍不可省略 |
| --- | --- |
| upload.complete；transfer.issue | initialized + active；实际 Publish/Runner/Operation/Owner/gate 与完整计划由真实调用点和 checker 证明。GET 的新 issue/materialize 也有现存 Mutate gate，不以其读取方向放开 archived 新工作 |
| upload.failed（writer/recovery 两 ordinal）；object.delete | initialized、当前存在且未越 Audit 屏障的 active/archiving/archived/deleting；仅允许 checker 证实的原 writer retirement、真实 recovery 或原 cleanup 终局。这里不是给调用者创建新 cleanup 的权限 |
| transfer.complete；transfer.revoke | 同上一行的技术收敛范围；真实 GET terminal/PUT Publish/普通 Cancel/S2 Continue 各走原权限与 witness。PUT 的对象 publication 仍受上一行 active-only 的 upload.complete；不能凭 completed metadata 造 grant，也不把 revoke 当 actual join |

ArtifactProducer 的四个 Human typed 动作是另一个必要真实分支：Create 用当前 Owner Mutate，List/Read/Download 用当前 Owner Read；Entry 自身闭集校验、Artifact resource/metadata 和原 ordinal 0 保持，Download 成功/失败 phase/outcome 不被一律 Success 覆盖。真实 Artifact service/OwnerProvider/Sources 在同 Tx 证明资源、来源版本、download plan/事件；Project 不读其表、也不把这四个 Human 动作开放给 Service/AgentRun。OutboxProducer 只新增 Human `OutboxDeliveryRequeue`，当前 Owner Mutate、合法 typed delivery resource/metadata、Success/ordinal 0；真实 D06 命令、失败态、版本、handler、delivery 更新与 Audit 仍同 Tx。原 key/associations 原样保留，不为这些分支制造第二个 receipt。

## Outbox 普通正式路由

继续用 `Authority.Discover/ValidateInTx(ob.ProjectRequest, ob.Dependencies)`，只给 DeliverProject/InspectProject/RequeueProject 增加封闭分派。R3 LifecycleProject 的 exact cause、双计划继续/只读终态矩阵不变；已有 Project Append 和前置 D09 Model Append 分派不变，不新增其它 producer。

新私有计划用现有 `projectIssuer`，purpose 区分三个 kind；binding 含完整 ActorDetails、ProjectID、kind、完整 Event Summary（全部 header 与 payload digest）、DeliveryIdentity 或 RequeueTarget、精确 Stage，不能复用省 Session 的 StableActor/RequeueBinding。锁为 Project SH，Human 再加 User SH；计划的规范 locks/opaque/issuer/binding 全部校验。与实际 D06 完整计划取 union 后一次 Acquire；ValidateInTx 不 Acquire、不嵌套 Tx，重验同 Store、完整 held locks 与当前 Project/Session/Owner。

| 路由 | 当前行为 |
| --- | --- |
| DeliverProject | 只接正式 OutboxDelivery actor，cause 是完整 DeliveryIdentity 的规范 digest，EventID/scope/handler/effect/attempt/fence 全绑定；initialized active 允许已核 effect，archiving/archived 仅 canonical_converge，deleting/未初始化拒绝业务 callback |
| InspectProject | Human 当前 Owner Read；active/archiving/archived 可诊断，deleting 拒绝普通内容；不是 R3 stop Inspect 或删除最小 receipt 路由 |
| Requeue CurrentAccess | 当前 Owner Read，允许读取已完成同 key 的原结果；D06 仍核真实 RequeueTarget/当前 scope/handler/effect/command，不能读他人 delivery |
| Requeue NewFact | 当前 Owner Mutate，仅 active；真实 D06 failed/dead_letter、version、processed、handler 与命令语义检查全部保留，更新+Audit+receipt 同 Tx |

D06 为 Requeue 两 stage 分别 Discover，因此本卡精确绑定 stage，CurrentAccess 计划不能冒充 NewFact；不要套用 Append 的同一 plan 两阶段规则。同 key 在 archived 重放仍须当前 Owner/Session，可返回原结果且零新 Audit/Touch。新 Session 必须重新计划；撤权在 receipt/版本比较前拒绝。

Project 的 Deliver 校验只证明当前 Project gate 部分，不能证明 DeliveryIdentity 真存在。真实 D06 `PrepareDelivery` 的私有 issuer、当前 claim/process/fence、canonical delivery/event/handler plan 在 `ApplyDelivery` 同一 Tx 先复核，随后才经 Project/handler Validate 到 callback；本卡不改顺序或绕开该入口。验收必须经真实 D06 路径证明伪造 service/cause、旧 claim、跨 Service plan 无法执行 callback；单独调用 Project gate 通过不算真实 delivery 通过。D06 技术收尾与 R3 stop/只读 Inspect 不因本分支变成普通 callback。

## 四个 stop adapter

构造返回固定 Name，禁止 caller 改名；Outbox 依赖必须 Name=`outbox`，此声明不是内部 state 健康证明。调用先校验 actor/cause、Project Scope（Meeting unbound）、当前冻结 manifest 的精确 name/version/metadata。每个 adapter 的 Project facts 来自同一个具体 `LifecycleAuthority`，并按 R1 显式 `LifecycleParticipantBinding{Registration, Participant}` 注册；本卡不发明默认 contract_version/ReferenceKinds，不按空数据删必需项。旧版本只能显式保留对应已审能力，错 metadata/缺版本拒绝。

Project preflight 在短 Tx 取得 Project SH 后调用已有 facts；Request 只允许当前 stopping 且选中 stop 未 failed。Inspect 沿 R3 当前 operation 的 stopping/failed/cleaning/completed 检查；终态只能只读。短 Tx 真实提交确认后释放锁才调用下游，绝不把 Project SH/Tx 跨 I/O/长 join 持有。下游 Artifact/Object/Outbox 在每个真实阶段仍自行经 R3 再校验，预检不是一张可缓存授权票。

| 固定 participant | 真实委托与报告 |
| --- | --- |
| artifact-object | 保留原 operation/action/接受 ProjectVersion，转换为 obj.ProjectStopCause；调真实 Artifact Service 的 RequestProjectStop/InspectProjectStop，由它与 Object S2 合并。必须 `Matches(ArtifactStopComponent, exactCause)`；不能只调 Object 或接受 Object component。state/reason 与 8 种固定 Object/Artifact ref 原名、原 ID 一一转为该 participant 的 PendingRef，并核冻结 ReferenceKinds；未知 ref/reason/错 cause 拒绝。Stopped 只能来自真实合并报告且 refs 为空 |
| outbox | 转换同一原 cause 为 ob.LifecycleCause，调用真实 D06 RequestStop/InspectStop；R3 resolver/逐 Tx facts 与 D06 actual callback join 保持。Stopped=true 必须 Pending=0；否则 pending/work_pending。D06 没有逐项 refs，不能从 count 编造 UUID。错 scope/cause/当前 pointer 先拒绝，不增加普通 delivery 权限 |
| secret | 没有自主 Project worker；在同 Project SH 下重新验证 exact current cause/冻结能力，接受时 EX 已等待此前普通短 Tx，当前 gate 阻止新普通写，方可 stopped；不调用 Secret Cleaner、不把 lease 使用者当已 join |
| audit | 同样只证明已验 Audit 库没有自主 Project worker及当前 gate 屏障；技术收尾 Audit 仍由实际 Object/Outbox 等 producer 的 stop 负责，合法 Read/Download Audit 仍由其真实短 Tx 与当前 Read gate 管理。archive stopped 不表示从此零 Audit，不能仅凭 audit stopped 推进整个 operation |

Secret/Audit 在 stopping 可依据上述真实屏障报告 stopped；在 failed/cleaning/completed 只读 Inspect 必须核同一 operation 对应 participant 已持久 stop=stopped，否则返回真实 pending/failed，不能补写成 stopped。仅剩最小 deletion receipt、无原版本/participant 事实时，本卡屏障 adapter 返回 unbound，不借 receipt 编造已完成证据；物理删除后的最终协议属于后继 Cleaner 卡。Artifact/Outbox 的有限终态检查继续由各自真实技术 receipt 约束原版本。

报告由原 c.NewStopReport 创建，绑定 participant/cause/scope。底层 error（含 OutcomeUnknown）原安全 Fault 传播，不能转换为 stopped/failed 或用空 refs 表示成功；有效 pending/failed report 保留固定 reason。Secret/Audit 的短 Tx Unknown 不返回先前内存计算的 stopped，重试重新确认事实；所有 adapter 均不更新 operation/participant checkpoint，不换 cause，不自行 Retry/claim。

本卡没有 Runtime，也不持有 ProcessGuard。真实组合测试必须用同一 guard，按 Artifact producer actual join + Outbox producer actual join + Object runtime 收束的联合屏障后才允许 guard 释放。不能提前 Close、按 TTL/断连接判死、重新创建 ProcessID 或取消 goroutine 就说 joined。后继 app 必须实现同一联合屏障；库测试通过不代表 root 已绑定。

## 错误、事务与副作用边界

错误沿原 foundation 安全 Fault：坏类型/字段 `INVALID_ARGUMENT`；缺依赖/未来能力 `DEPENDENCY_UNBOUND`；错 issuer/purpose/cause/producer 或事实不符 `FORBIDDEN`；当前非 Owner 沿原 `NOT_FOUND`，Session 撤销沿原 Session 错误；当前普通 gate 拒绝 `PROJECT_NOT_ACTIVE`；缺实际锁、坏行、Store/Tx 不匹配和依赖故障 `DEPENDENCY_UNAVAILABLE`；discovery 映射改变 `RESOURCE_BUSY`，整轮重新发现。底层 not_started/not_committed/unknown 语义不得丢失。外域错误不带 SQL、对象 key、payload 或密钥。

本卡新增权限/事实方法只读，不 Touch、不写 receipt/业务表；Audit/Event、delivery、stop 的真实副作用仍在其既有实际 Tx/资源 owner 中发生。不能忽略 callback 的缺锁/权限错误后提交；集成须证明原事务拒绝、无半条 Audit 或业务更新。提交 ACK 丢失必须保留原 Command/Delivery/Stop cause，沿原 writer 锁确认 canonical 结果，不能绕过消费者协议重新发 ID。

## 精确候选实施路径

共 22 个路径：17 个生产/单测路径、5 个真实集成路径。仅规格静审与前置最终 delta 通过后，由主线程另授写权；除表内文件均只读。

| 路径 | 唯一变更范围 |
| --- | --- |
| `internal/central/artifact/contract/convergence.go` | 新三方法窄事实端口，不改 AccessSubject/Authority 的旧签名 |
| `internal/central/artifact/contract/authority.go` | 仅 Authority 注释明确上述真实取消的 active/archiving/archived 状态范围；签名、deleting/Project Cleanup 的独立授权要求不变 |
| `internal/central/artifact/project_convergence.go`、`internal/central/artifact/project_convergence_test.go` | 新 Store-only 取消事实、完整 discovery/锁验证及纯测试 |
| `internal/central/artifact/owner.go` | 仅两个 Service 调用处把实际 ownerFact 传给包内取消 cause 检查，精确匹配 owner/target；原 Human、cleanup 分支与锁取得协议不变 |
| `internal/central/project/artifact_authority.go`、`internal/central/project/artifact_authority_test.go` | 新 Artifact 当前权限 adapter |
| `internal/central/project/object_audit_authority.go`、`internal/central/project/object_audit_authority_test.go` | 新 Object gate/实际 checker 分派及 domain Audit 不可逆屏障 helper |
| `internal/central/project/domain_human_audit.go`、`internal/central/project/domain_human_audit_test.go` | 新 Artifact/Outbox Human typed Audit 闭集 |
| `internal/central/project/outbox_authority.go`、`internal/central/project/outbox_authority_test.go` | 新三个普通路由的 private plan/当前 gate |
| `internal/central/project/domain_participants.go`、`internal/central/project/domain_participants_test.go` | 新四个 stop adapter；不改 R1/R3 或增加 worker |
| `internal/central/project/audit_facts.go` | 仅开放 Object map key、精确 producer 分派并调用共同 domain 屏障；Secret 原权限/事实语义保持 |
| `internal/central/project/events.go` | 仅普通 Deliver/Inspect/Requeue 分派到新 helper；实际写权由主线程另授，保留 Project/Model Append 与 R3 Lifecycle 行为 |
| `tests/project/domain_bindings_fixture_test.go` | 新真实组合 fixture、可跟踪预算/资源/原 writer 协议工具；不改旧 fixture |
| `tests/project/artifact_domain_bindings_test.go` | Artifact 权限、取消事实、真实上传/来源/读取/Download Audit |
| `tests/project/object_audit_domain_bindings_test.go` | 真实 Object 六类动作/当前 gate/witness 与原子性/Unknown |
| `tests/project/outbox_domain_bindings_test.go` | 真实 Deliver/诊断/Requeue/current Session/双 stage |
| `tests/project/stop_domain_bindings_test.go` | R2 接受事实 + 四真实 adapter、冻结 registry、真实 join/只读终态 |

没有业务 Service、Object/D06/Audit 旧实现、R1/R2/R3、共享 Store/identity、SQL、脚本、app/root、HTTP 的修改许可；OwnerProvider 仅上表窄范围。新 Artifact contract 文件与 owner.go 须在前置 Artifact 冻结交接后才能实施。若真实消费路径无法在此范围正确接通，给出固定输入证据和最小 delta 重新审规格，不写 allow fixture 或缩弱验收来守名单。

## 可执行验收

真实组以 `TestProjectDomainBindings` 为统一前缀；全部使用生产 Project Authority/Artifact Authority/Convergence checker、真实 Session、Audit、Object checker、Artifact/OwnerProvider/Sources、Object S2、D06 与 PG/MinIO/ProcessGuard。未来 D10 初始化与 Runner/Execution 仅按正式口隔离且明确标签；不得用 allow Project/Owner/ObjectAudit/stop port 替代本卡目标。尚无 R4 时，operation phase/终态由测试 fixture 明确建立合法本域事实，不伪称生产 archive/Restore 已执行。

| 固定顶层 | 必验行为与反例 |
| --- | --- |
| `TestProjectDomainBindingsArtifactCurrentAuthority` | Owner 创建/上传/读取/list/download 正向及 typed Audit；非 Owner、管理员、新旧 Session、未初始化/archiving/archived/deleting 矩阵；旧计划换 Session/Project/provenance/缺 User/Project 锁均拒绝，archived Read 正向 |
| `TestProjectDomainBindingsArtifactConvergence` | 真实取消后的本域记录、实际 record EX + Project SH 才准 Converge；archiving 下 Human CancelUpload/CancelCreation 原完整主链，及取消确认后切 archiving 的 ObjectMaintenance CancelUpload、CancelledUpload CheckCleanup/Release 两恢复入口均实跑到真实终局或可核 pending，不因自有 gate 错配失败；任意 service UUID、无取消行、错 project/target、伪 discovery、缺 record 锁、跨 Store/Tx、取消/发现竞态失败。只读/Mutate/deleting/Project Cleanup 均不能借该事实获权；OwnerProvider 对另一 owner/object 仍拒绝 |
| `TestProjectDomainBindingsObjectAuditOrdinary` | 真 Publish/transfer issue/GET Capture/PUT publication/revoke 及 Audit 同 Tx；关闭 gate 后新 issue（含 GET）和 Publish 拒绝，合法 terminal 收敛仍走原证据；直接 Append、历史 metadata/UUID、错 ordinal/producer/private witness/同 PG 异 Store均拒绝 |
| `TestProjectDomainBindingsObjectAuditMaintenance` | 真 writer failure、recovery、cancel cleanup finalization 与 S2 revoke 经生产 Project checker；archiving/deleting 技术收尾不被 Mutate 误拦，Audit 屏障后无新 Audit；缺锁 poison 被忽略也不能提交。实际原 writer pending/COMMIT ACK 丢失/rollback 均保持 canonical 单事实，不假称全部 Unknown 已终局 |
| `TestProjectDomainBindingsOutboxDelivery` | 真实 Prepare/Apply 与已登记真实 handler：active 两 effect、archiving/archived 仅 canonical、deleting 无 callback；完整 Summary/actor/attempt/fence/effect 篡改、跨 Service 私有计划/旧 claim 拒绝；撤 gate 的 Project EX 与真实 callback Tx 串行 |
| `TestProjectDomainBindingsOutboxInspectRequeue` | Owner 当前只读诊断；失败 delivery 真 requeue+Audit+receipt 原子成功；CurrentAccess 换 NewFact、不同 target、旧 Session/撤权失败；同 key archived 仅重放零新增 Audit，新的 requeue 拒绝；Ack Unknown 沿原命令确认，失败回滚全部 |
| `TestProjectDomainBindingsFourStopAdapters` | 真 R2 accepted/stopping cause，经原不可变 registry 调全部四项；真实 Artifact 自身工作与 Object writer/source/transfer、真实 D06 callback/尾部 Tx 未 join 时 pending，Secret/Audit 只作自身屏障；原 join 后 stopped、准确 refs/版本；错 component/cause、缺冻结版本、会议 scope、替换 pointer 和所有 Cleanup 拒绝 |
| `TestProjectDomainBindingsStopInspectAndProcess` | failed/cleaning/completed 的有限只读 Inspect 不新取消/写 Audit；Secret/Audit 无 stopped checkpoint 不冒成功；精确旧 ProcessGuard 死亡 + 原 writer 终局与活 shared guard 的反例；Artifact/Outbox 尚活不能释放 guard，deadline/cancel 不等于 join |

测试中 Archive/Delete 的 Project EX 接受屏障与 SH 消费者必须真实竞争；不靠时间 sleep 推断先后，用明确 ready/entered/COMMIT 协议与后态检查。独立验收在同源快照复查上述八个完整顶层并另加完整 actor/held-lock/服务伪造探针，不以 unit 或 test adapter 代替真实组合。所有新组无 skip；作者与独立运行均记录 distinct 顶层/子例、原失败和修复、真实 PG/MinIO/race、总预算、固定输入 hash。

纯测试须区分 nil/typed-nil、可判定具体零 Authority 与 opaque 非 nil 依赖；有效 Name 包装零 Service 不得被宣称已证明健康，实际调用错误/同步 panic/坏报告均不得返回 stopped；InTx panic 不得 recover 后提交。纯 unit/race/vet、integration compile/vet、两 cmd build 必须通过；真实资源沿 [依赖恢复记录](../agent-team/dependency-recovery-2026-10-05.md)单窗口运行。旧回归选择 Artifact普通/stop、Object S2/Audit、Project R2/R3/Secret/D09、Outbox ordinary/lifecycle 的实际受影响完整顶层，不改旧断言；旧 Outbox 时间首红仍单列关注，不以单次未复现说已修。结束核 exact-ID 容器/网络 absent、基线不变、所有所属进程/运行目录清零并正式交窗。通过只代表本卡库组合，完整 D08/D28/E01 与 root 仍未完成。

## 规格采纳与剩余实施门槛

2026-10-05，独立 verification_worker 对 rev2 原候选 SHA-256 `5a41d0cccff9c44166db2d0cae9506105a4d62c234caaee0d09f761dcb7c0cb6` 完成静审，未发现规格硬阻断；22 路径、11 链接、构造与完整持锁关系均已核。主线程复核实际取消调用链后，采纳仅针对精确持久取消的 ObjectMaintenance Converge 在 archiving 收尾的工程澄清；deleting、Project Cleanup 和普通读写权限不变。报告 `/tmp/agenteam-project-domain-bindings-static-x58klrdr/review.md` SHA-256 `0f757cd006279945a8de1f67fda96e91c85815deb9d29d4f1beb4f5c4be07077`，输入清单 SHA-256 `322f9e9c689f624b0c3ee41a7ee3303cb8cea86ef5cad0bcb092710ade0afccb`。本节持久记录结论，不依赖临时文件存续。

Object Audit 的13源已独立验收并提交推送 `a716ae2a16bc24c115d0207557cada96a30f1049`，最终源清单 SHA-256 `061a4c19791e812e68a0755059a171984bc3bf8070197ae6f034f7943f119558`；独立2顶层6子例通过，真实资源已清零。其通过不代表本卡的 Project 映射已绑定。D09 Project 配置21源已独立验收并提交 `de00c610da62cb77cc03efe7c3cc842cf81f1ba5`。Artifact stop 最终验收、受阻的 Object Runtime join 修复及后续组合接缝 delta 尚未闭合；本卡仍须等待这些前置和文件所有权交还，再由主线程单独授权实施。本次状态归位不恢复 Object 修复任务，没有本卡 Go、PG/MinIO 或产品行为通过声明。

主线程采纳本轮有界静态报告 `/tmp/agenteam-project-domain-prereq-delta-t7gyhkzz/review.md`，SHA-256 `6ef0db9394550cd695efeaaa439b44d7894c5c70bf71a7a7bc621b4e0a970742`：固定 `8bce2d9319f0b649b7c144ba14d9ee52f7dd09da` 核对上述 D09/Object Audit 已验接缝相容，保留 Model Append 的完整绑定、不同 stage 计划及 Object 同 Tx witness/真实事实消费。该核对不改本卡 API、22 路径或行为，不覆盖 Artifact 最终实现或 Object 修复，也不授予任何文件写权；原 `fda0a35` 核查与规格来源仍按当时历史保留。

## 有限后继：Project Variables 本实例精确停止

状态：基线 main `33903460`，SPEC 准备完成，产品尚未实施。该后继只消除普通 Variables 与 Secret Variables 的真实调用退出缺口，不恢复前文 Object Runtime join STOP，不注册完整 participant，不启用生产 Project initializer。D08 §7/§8 与 [D10 §14](d10-skills-initialization-design.md#14-本次共享差异与后续依赖) 的完整组合门保持。

### 提供方与局部接口

提供方在 `projectvariable` 包内直接组合原 `*Service` 和 `*SecretService`，两者必须使用同一 Store、同一具体 `*Authority` 和同一可比较 ProjectAuthority 端口；Secret 的 Writes 原同 Store/Project 约束保持。复用当前唯一 facts/Outbox producer，不增加新 producer、数据库表、公共 HTTP 或迁移，不改 Secret 的材料接口。

拟新增 `NewProjectCallStopper(ordinary, secrets)`，返回不可变 `*ProjectCallStopper`，提供 `RequestStop(ctx, actor, cause, scope)` 与 `InspectStop(ctx, actor, cause, scope)`。返回本包 `ProjectCallStopReport`：安全 Details 只含原 ProjectID、OperationID、Action、accepted ProjectVersion、当前已授权快照的 PendingCalls 与 LocalJoined；报告私有构造、零值无效，fmt/slog 不含业务材料。它不实现 `ProjectLifecycleParticipant`，没有 `Name`/`Cleanup` 方法，不返回可被 D08 当作全域完成证据的 `pc.StopReport`。LocalJoined 只证明同一提供方两个 Service 的该次快照，不证明其他进程、Skills、Agent 或整个 Project 已停止。

每个生产调用在任何 SQL、D04 Prepare 或 confirmation 之前固定私有 `{ProjectID, class, original-call}`。class 仅 read、mutation、control：普通/Secret Get、List、identity-only Lookup 都是 read；Create/Update/Delete 及其原 CommitUnknown confirmation 归同一个 mutation call；生命周期方法自身是 control，不进入待停止集合。调用身份是原登记对象，不能用同 Project 的后来调用替换。无 timer、后台 goroutine、额外生命周期预算或持久 stop 表。

### 授权、取消与实际返回

1. 拒绝 nil/非法 ctx、非 Project Scope、非法 cause 或非 `ProjectLifecycle` Service actor；actor ProjectID、CauseRef 必须分别等于 scope.ProjectID 与 cause.OperationID。精确身份为 `{ProjectID, OperationID, Action, accepted ProjectVersion}`，不把 UUID 合法或当前 ProjectID 相等当授权。
2. 使用原共享 Store 的短事务，完整取得 Project SH，再通过两个 Service 原同一 `Projects.ValidateLifecycleInTx(..., SkillsParticipant, StopPhase)` 核持久 operation、当前 pointer、accepted version、冻结 manifest/name/version 与合法 stopping 状态。它是 agent-skills-variables 的 Variables 子能力，不另注册一个同名成功 adapter。当前正式口仅允许 stopping 且相应 stop 未 failed；本切片 Inspect 同样只支持该阶段，不把 cleaning/completed 的旧 cause 当新取消资格。
3. 在该授权事务内按固定 ordinary→Secret 锁序捕获对应本实例原调用指针，锁只包住内存快照，不跨 Commit 或外部 I/O。Archive 捕获 mutation（包括原 confirmation），保留合法 Get/List/Lookup；Delete 捕获本 Project 的 read 和 mutation；其他 Project 与 control 都排除。
4. 只有原 `WithinTx` 实际返回 Committed，且入口原 ctx 仍有效，RequestStop 才在各服务原 mutex 下标记捕获且仍登记的原 call 为 stop-requested，取消其原 ctx 和已登记 confirmation。NotCommitted/Unknown、授权或锁失败均返回原安全错误和零报告，**零取消、零 stop-requested 标记**；不因重读当前行“看起来存在”改判原 Unknown。
5. 两个原 confirmation 都在 `context.WithoutCancel` 后登记，故其现有同 mutex 准入必须同时检查 `service.stopped || originalCall.stopRequested`：Stop 先发生时，后来登记的原 confirmation 立即取消；confirmation 先登记时由 RequestStop 取消。保原三秒确认预算、原 CommitResult/Unknown 语义，不能以 lifecycle 取消抹成 committed/not-committed。
6. RequestStop 不等待 I/O；取消后原 call 仍留在 calls 集合。只有原调用函数（包括同步 confirmation）实际返回并执行原 done，才退出登记。InspectStop 每次重新执行第1–3步的真实当前授权与原 Commit 判据，不再取消；在确认提交后同一双 mutex 下统计当前本实例所有匹配 scope/action 的调用，仍登记即 PendingCalls>0/LocalJoined=false。COMMIT 期间新登记的同范围调用也阻止 LocalJoined，但不能被旧授权快照追认取消。即便 ctx 已取消、Tx token 已失效或 SQL 结果已返回，也不提前 join。
7. 提供方的每次方法自身登记进两 Service 的 control 调用并沿原 ctx 传播，保证进程级 Stop/Drain 不能越过仍在途的授权事务。现全服务 Stop/Drain 仍覆盖全部调用、仍不关闭共享 Store/D04/Account。不会用全服务 Stop 实现单 Project 停止。

stop-requested 只附在被授权捕获的原 call 上，不永久封闭整个 Project 本地实例；新调用继续经过现有真实 Project gate。首轮不声称本地 map 空能证明 foreign process 死亡；未来完整 participant 必须另落实跨实例事实及原事务终局，不能复用 LocalJoined 直接报告全域 stopped。

### 唯一写域与第一条真实链

实现限 `internal/central/projectvariable/{service.go,secret_service.go,commands.go,reader.go,secret_commands.go,secret_reader.go}` 中的调用登记、confirmation 准入及必要窄辅助，新增本包 `project_lifecycle.go`/有限 helpers 与相邻测试；新增独有 `tests/projectvariable/project_lifecycle_test.go` 及必要 fixture。实际旧入口只有上述四个业务文件，不为凑路径修改其它源。app、Project、Audit facts、共享 harness、Schema、迁移和 go.mod/sum 不写；00031 由 Model Runtime owner 独占。

先用有限纯控制核构造身份、事务 NotCommitted/Unknown 零取消、Archive/Delete/跨Project选择、先stop后confirmation与反序、原call held不join、Inspect不取消及全服务Drain等待control；基础可用后即准备首真实 PG，不重跑 Owner30 或完整 HTTP/native 矩阵。

首真实链使用原 Account/Login、同 Store/Project Authority、普通与 Secret Service 和 D04；两个 Project 的合法初始化/生命周期 stopping operation、manifest/participants 由规范 fixture 明确建立。**该 phase fixture 不声称执行了生产 Archive API/worker**：当前真实 BeginArchive 只接受 operation，accepted→stopping 的生产推进者仍未实现。授权/完整锁/业务调用/提交结果均用真实提供方，不造 stop 成功报告。

用原 BEGIN 或 confirmation barrier 持有该 Project 实际调用，先验证 Project EX 未获时没有已接受 gate/取消；再以明确的已提交 phase fixture 行为刺激 RequestStop，证明原调用未返回时 RequestStop/Inspect 均 pending，release 后原 call/confirmation 真正返回才 LocalJoined。Archive 的合法读与另一 Project 的读写不被取消；Delete 包含本 Project 读。新旧 cause、跨 Project/Store、错误 actor、未提交授权反例不产生取消或材料泄漏；所有原调用即使断言失败也实际 join。真实资源仅在 root 独占窗口中运行。

### 集中保留的后继缺口

生产 initializer 仍 unbound。后继尚需：D08 lifecycle claim/phase worker；Variables 普通/Secret 的真实 cleanup 与跨进程终局；Skills+已启用 Variables 的完整组合；四个基础真实 adapter 及当前已启用 Work/Knowledge/Usage 等域的停止与清理；最后真实共享 guard。既有 00013 已包含 lifecycle manifest/participants/work_claims，可供独立 D08 推进者消费；本切片不占新迁移、不扩真实 phase authority，不删除 required 项或注册空处理器。Object Runtime join 原 STOP 保持，不能由本实例退出证据解除。

### 本切片当前恢复状态

- 核心 provider 与原调用登记已实现；独立静审有限接受。首纯测试编译因测试比较非 comparable LockKey 失败，原 FAIL 保留；修为正式 Mode/CompareLockKeys 后作者 9 top / 20 sub race 实际通过。
- 首 PG 候选 race-c 与精确 list 通过后，冻结55a6e725的1 top / 3 sub 沿上述规范 phase fixture 首次实际整轮 PASS：top9.20s、原 Go/driver/supervisor/tool Wait0，两资源/private/desc/runtime/TCP双尾与435输入一致全部闭合（76.621s，无重试）。作者动态结果不冒独立动态；独立实例已有限静审核心/方法/入口。独有 [恢复入口与结果边界](../../../.agent-state/project-variable-lifecycle/README.md) 保留原准备失败及原两资源监督协议。
- 此提供方切片只闭本实例 ordinary+Secret 精确停止/原call退出；foreign join、cleanup、完整复合participant 与生产phaseworker仍未交付。原生产initializer unbound、Object Runtime join STOP 和本卡其它阻断不变。

## 有限后继：真实单轮 Stop phase 推进

本段在 Variables 本实例提供方已有限交付后实施。目标是把合法原 operation 的 `accepted→stopping`、持久 claim/fence 与一次真实提供方调用连起来；不增加后台扫描器、全量 scheduler、生产 app 绑定或整个 participant 的完成报告。首链不再用测试 SQL 推进 stopping，但完整 registry 尚未就绪时，既有 initialized Project 与 accepted operation 仍由明确的规范 fixture 建立，不能称生产 BeginArchive/Create 正向已完成。

### 提供方、接口与所有权

新增 Project 库 `NewLifecycleStopDriver(store, lifecycleAuthority, processes, localStep)`；同一个具体 `LifecycleAuthority` 必须与 driver 使用同 Store，当前 process 身份固定且有效。`LocalStopStep` 是构造时唯一固定的 Tx 外函数，签名为 `func(context.Context, identity.Actor, LifecycleCause, ScopeRef) error`；nil 拒绝，不提供任意逐次回调或动态名称。`Run(ctx, ProjectID, OperationID)` 只运行一轮；`Stop/Drain` 追踪原 Run 和回调实际返回。回调成功仅表示该次同步调用正常返回，绝不等于本实例所有业务工作、foreign process 或整个 participant 已停止。

首消费者在同 Store 的真实装配处捕获已交付的 `ProjectCallStopper`，用原 service actor/cause/scope 调 RequestStop/InspectStop 并核报告 Matches；LocalJoined 和 PendingCalls 只作此消费者的局部观察。driver 不引入 Project 对 projectvariable 具体实现的反向依赖。跨包 opaque 提供方的 Store 归属由正式构造关系与真实接缝检查保证，不能通过反射私有 state 或同 DSN 冒充具体实例证明。

唯一新增产品路径为 `internal/central/project/lifecycle_stop_round.go` 与 `lifecycle_stop_claim.go`，纯测试分别为同名 `_test.go`。原 creation recovery、Project Audit/Model facts、app、公共 UI harness、旧迁移和 Model 独占 00031 不修改；需要新增真实 integration 源或私有入口时另报精确路径，不覆盖已验 Variables 首链。

### 原事实、claim 与提交边界

- 使用 00013 现有 `work_claims(work_kind='lifecycle', work_id=OperationID)`，完整身份固定为 `(ProjectID, OperationID, ProcessID, AttemptID, fence)`。其 `phase=terminal` 是该 claim 尝试的执行退役事实，不是 `lifecycle_operations.state=completed` 或 participant stopped；已有 creation checkpoint 同样允许 operation 未完成而本轮 claim terminal。本切片不新增 DDL。
- Run 从原 operation 读出 action/accepted ProjectVersion，不允许 caller 改 cause。Project EX 短 Tx 内重读 Project 当前 operation 指针、initialized/生命周期 gate、owner、原固定版本、完整 frozen manifest/digest/每项 metadata 及所有 participant 行；缺历史声明、错版本、失败或 cleanup/completed phase 均拒绝。声明匹配不冒 registry adapter 绑定；缺 provider 的完整参与者事实保留，绝不删 required 项或注册空 handler。
- 新 claim 仅在旧 claim 缺失、原本 terminal、该 driver 已观察同 process 原 attempt 实际返回，或真实 ProcessAuthority 确认精确 foreign process 已停止时接受。死亡检查在 Tx 外；取得 Project EX 后再次核原 claim 身份，串行等待原 writer 终局，只有完全相同旧 attempt/fence 才能递增 fence。TTL、失联、取消和本机未知 attempt 均不能接管。
- claim 与首次 `accepted→stopping`/operation.version 前进在同一原事务提交；accepted ProjectVersion、manifest 和所有 participant 完成事实不变。已 stopping 的合法重试不倒退 phase。原物理 CommitResult 为 Unknown/NotCommitted，或 caller 已取消，均不启动 localStep，Unknown 保原 attempt/cause。
- 提交已确认后释放 Tx/锁，再以 ProjectLifecycle 服务身份调用 localStep，单次调用最多 2s 或更早 parent。同步调用超时仍等待其实际返回，不借 goroutine 超时替身释放占用。同实例每 Project 仅一轮，最多四个原活 Run；未返回即占位，其他请求明确 ResourceBusy，不无界排队。
- 回调实际返回后才允许短 Tx 以 exact claim/fence 记录本轮 terminal；回调错误保原错误，checkpoint Unknown 保物理 Unknown。该收尾使用至多 3s 的独立取消上下文，仍由原 Run/Drain 持有并追踪；不能先释放 driver/guard 再后台补写。若终态未确认，保原 claim 与本机实际返回证据，后继仍须原 Project 锁和 exact claim 重验。Stop 仅取消；Drain 只在原回调及全部本轮事务实际返回后结束。
- driver 从不把 LocalJoined、回调 nil、claim terminal 写成 `agent-skills-variables.stop_state=stopped`，也不进入 cleaning/archived/completed。完整复合提供方、foreign 业务调用与 Cleanup 继续 required/pending；新的 phase 能力不会解除 production initializer unbound 或 Object Runtime join STOP。

### 首个真实调用与必要验证

首链使用真实 Account/Project/ordinary+Secret、同 Store 与 ProjectCallStopper。在已合法 accepted 的 operation 上调用真实 driver，检查实际 SQL 仅该引擎推进 stopping；原 Variables 调用停在 BEGIN 后、Project gate 锁前，提交后才收到取消，callback/SQL/commit/原业务函数全部实际返回后局部观察才 joined。另一 Project 保持可用，所有全域 participant 不被标 stopped。fixture 只提供尚未开放的 accepted 前置，不直接写本轮 phase/claim 结果。

基本纯控覆盖未确认提交/取消零提供方调用、冻结 manifest/cause/claim 错配拒绝、同 Project 双轮拒绝、Stop/Drain 不提前 join、精确 foreign proof 与原 writer 锁重验、回调错误/收尾 Unknown 原样保留。真实 PG 重点验证原 EX 屏障、确认提交后调用、原调用 held 时 claim/Drain 仍活、释放后的 exact terminal，以及旧 attempt/fence 不能覆盖新轮。foreign 业务调用缺口仍显式保留；不以 fake ProcessAuthority success 声称真实进程死亡或完整 participant 验收。该有限源冻结并受独立方法审后才申请真实资源窗口。

当前四个Project源与两新PG源已实现；既有fixture只保留原LifecycleAuthority指针。首6top/2sub纯race、Project vet通过；非作者实际审发现组合checkpoint错误可回显provider error，已窄修安全包装并以新private canary定向race及vet通过，物理Unknown/原cause仍保留。修后第一次fresh容量门失败且0Go的事实保留。PG候选race-c与exact list仅一top均已通过；非作者已有限接受核心与PG方法，首次实际结果见下段。私有入口新增固定1top/3sub与3项离线方法控制，原提供方入口/共享sup/driver/预算不改；详细命令与分版本结果在既有[本域恢复说明](../../../.agent-state/project-variable-lifecycle/README.md)。本轮仍不交付生产全phase worker/完整participant完成语义。

首次phase PG随后实际整体FAIL并完整退出（来源1ccbc56b，top12.07s、Go/driver/outer Wait1、两资源/private/desc/TCP双尾与439输入一致齐）。第一sub phase barrier未命中，第二sub回滚断言失败，第三fencing子例通过；不把局部通过升级成整体接受。已定位两测试hook误用Recovery的Owner筛选真实JobCause，后续仅窄修原Kind/JobType/JobID/合法Attempt身份，产品/全部断言/预算未改；完整原失败边界保留。

修后candidate02 race-c/list通过，非作者已对该唯一修正实际diff有限接受。来源88adc94a的`pg-phase-02`单次1top/3sub整轮PASS（12.44s；三个子例0.47/0.10/0.21s），原Go/driver/outer Wait0，两资源/private/desc/HOST_TCP双尾与439输入一致全齐（sup85.470s，无重试）。真实引擎确认phase/claim提交后才调用同Store provider，原业务与checkpoint实际返回才退役本轮；真实rollback/冻结版本拒绝及旧fence不可覆盖均已验。此有限链不把claim terminal或LocalJoined升级为participant/operation完成，不开放生产initializer、foreign join或cleanup；原phase01 FAIL保持，动态结果为作者执行且不冒非作者动态验收。

## 有限后继：有界 Stop recovery 批次

`NewLifecycleStopRecovery(store, authority, processes, localStep)` 私有持有上述同 Store、固定 process/回调的原 driver；`RunBatch(ctx, after *OperationID, limit)` 只接受 limit 1..4，最多一个活动批次，不启动后台循环。发现查询仅取 Project 当前 pointer 对应的 accepted/stopping operation，按 ID 取 limit+1；实际 Rows.Close 返回后逐项调用原 Run，全部授权、完整 manifest、claim/fence 与 EX 重验仍由原 driver 执行。

结果只有 `Visited/Pending/Next`，每个已访 operation 仍计 Pending，没有 Completed。Busy 不阻止访问后项；其他错误保留，首物理 Unknown 的原 attempt/cause 不被后项错误或取消遮掉。Next 只推进至已访问边界；nil 表示本次扫描末尾，调用者下一轮必须从 nil 重扫先前 Busy/Pending，不能永久遗漏前缀。未访问任何项即失败不消费输入 cursor。ctx 失效后不启动新项；Stop 取消原批次与 driver，Drain 等原查询/Rows.Close/Run/checkpoint 实际返回，不能递归 Drain 或提前释放共享 guard。

实现仅新增 `internal/central/project/lifecycle_stop_recovery.go` 与同名测试，不改迁移、Audit、app/initializer 或完整 participant 状态。两源已获非作者有限静审，7 top / 12 sub 纯控源码明确使用 SQL 边界替身。首次预飞 5,262,024,704 B 未达 5 GiB，故0 Go；第二次预飞通过后，测试夹具将 `error` 接口传给需要 `*Fault` 的 `NotCommittedResult`，编译失败、0测试正文，vet未达。仅改为同一安全 Fault 的具体构造后，第三轮原定向7 top / 12 sub race 全部通过（package1.053s），Project vet通过；原进程/outer Wait0及group/runtime双空齐。产品和断言未变，两原失败均保留，未重复旧 phase 矩阵。真实 guard 单链测试已获非作者有限方法审；content 的 integration race-c 与精确1top/0sub list实际通过，683输入不变，原Go/list/outer Wait0及group/runtime双空齐，候选为本树 `output/ai/project-variable-lifecycle/guard-compile-01/project-phase-guard.test`。Guard七资源入口随后获非作者实际源审并完成以下首次真实链；Object Runtime join STOP 与 foreign 业务 join/cleanup 缺口不变。

来源`af133f7a`的Guard01单次执行`TestProjectLifecycleStopBatchRealGuard`（1 top / 0 sub）整轮PASS，top10.58s。真实同Store原引擎在旧claim所属ProcessGuard仍活时返回Busy且后项可继续；原child进程SIGKILL后，由原cmd.Wait与stdout reader实际join，再经原ProcessAuthority确认和Project锁内重验推进原claim/fence。accepted及完整manifest仍是明确上游fixture，所有participant保持required；本轮不伪造完整domain停止或operation完成。Go、driver、supervisor、outer原Wait全部0，7资源双退役、private/runtime/desc/TCP双空，704输入初末不变；全部原尾结束后才释放窗口，无重试。必要安全结果见[Guard首链结果](../../../.agent-state/project-variable-lifecycle/guard-first-actual-result.json)，历史两次Batch FAIL与phase01整体FAIL不回填。

本次只接受有界批次和真实guard死亡后的本轮claim恢复；完整Registry/participant、foreign业务调用join、cleanup、生产initializer与完整D08继续未交付，原Object Runtime join STOP不解除。正式取入为Batch两源、guard测试、本卡和必要Guard恢复入口增量；共享入口保main已有其他域，旧phase两资源方法不变。

## 有限后继：Execution 初始 Skill 与 Tool 捕获提供方

Skill、Tool、Execution提供方及00051/00052已有限验收：定向纯测与补验、五包vet、compile/list及真实1 top/1 sub通过，原调用和全部资源退出尾闭合，源码与最终结果获独立有限接受，原pure01失败保留。依[Execution Context](../../architecture/agent-executor/execution-context.md)和[D01 Snapshot原子捕获约束](d01-contracts/execution-orchestration.md)，本片提供Skill初始固定revision绑定、Tool固定metadata/ref，以及Execution preparing阶段的真实私有授权接缝。Skill拥有00051和清理引用保护，Tool拥有00052；旧迁移和既有STOP保持，恢复方法与分轮结果见[既有组合说明](../../../.agent-state/agent-system-integration/README.md)。

两个提供方的请求只标识Project/Agent/Execution。discovery通过原preparing claim/process/fence的私有证明，读取各自真实assignment或Agent引用head，冻结版本、集合及完整锁计划；final在同Store原Tx取得完整锁并完成真实Project→Trigger→Agent捕获后，重验相同attempt与当前事实。Skill绑定当前已发布revision及assignment sequence，不把初始化时ObservedRevision当运行版本；Tool固定真实注册的Spec/名称/binding，与实际Agent配置和原Execution Policy精确对应，未实现的binding或约束明确拒绝。各域引用与捕获结果同Tx产生，不能凭公开DTO、虚构Human或已存在Execution行取得授权。

真实组合已在原PreparationDriver内调用两真实提供方并观察非空固定结果及同Tx本域引用；完整Model/Context/ref/lease/input尚未齐备时，外层实际回滚，未提交半份preparation input、引用或Snapshot。此结果不等于完整capture、running或Started。Model捕获和网络请求、实际Tool调用、完整Core Tools、生产app/initializer、Execution终态与relaunch/cooldown仍未由本片完成。


## 有限后继：受支持配置的完整 preparation input

Model、环境及真实空Mount提供方、版本化Platform Prompt和00053/00054输入持久化已实现并获有限源码审查接受；定向23 top按原20项通过及修后Mount三项补验、十一包vet、compile/list及真实1 top/2 sub整轮通过，原调用与全部资源退出尾闭合。完整input、引用与租约真实提交后丢回执的Unknown由原owner只读恢复，未重复调用提供方；缺Mount时原NotCommitted回滚input、引用与租约，Model prepared intent按合同单独保留。原纯测素材失败和启动前容量失败保留。本片复用原Model Resolver与Execution真实Consumer授权，仅捕获现有受支持的主模型配置；Model credential沿原Model私有Secret usage链取得Execution租约，环境Secret沿独立ProjectVariable用途取得租约，不扩大旧Purpose闭集。普通环境值可进入输入，Secret只保存元数据、稳定引用与lease ID，不保存明文或密文，也不承诺冻结后续进程读取的Secret值。

PreparationDriver在原claim/process/fence及完整锁计划下，依次取得真实Project、Trigger、Agent、Skill、Tool、Model、Environment与Mount结果；输入固定原Launch、完整命令身份、RequestID、attempt binding、捕获时间及版本化Prompt。00054按Execution唯一身份保存规范字节、摘要和原claim完整关系，所有本域引用、Model snapshot/binding与租约和输入在同一原Tx提交。Model discovery的prepared intent允许独立保留；最终捕获缺项或失败不得留下部分输入、引用或租约。Unknown保留原调用及精确输入身份，通过原只读观察确认，不能因未查到行重发；已提交输入的重放不重新捕获当前配置或生成新attempt。

本片要求正式Agent配置显式`InjectAgentsMD=false`，Mount提供方真实读取head/version并确认配置为空；默认true缺内容源、非空Mount缺运行引用或未知资源约束均明确拒绝。Tool结果按当前注册交集冻结，Agent保留未注册Tool ID不等于捕获结果缺失。输入codec仅验证封闭类型、版本、边界和各域身份关系，不代替提供方私有证明；Platform Prompt包含Knowledge按需检索及Memory recall/retain/reflect的既定职责，不声称这些工具必然可用。输入提交后Execution仍为preparing，不构成sealed Snapshot、Running或Started；实际模型请求、OpenAI tools动态STOP、完整终态/relaunch/cooldown及生产app/initializer边界保持。重跑方式和最终结果继续放在[既有组合说明](../../../.agent-state/agent-system-integration/README.md)。

## 有限后继：固定输入的 Execution Context 构造

`execution.NewContextBuilder`以显式Task builder组合原`PreparationInput`，不持Store、不读current配置。`work.TaskContextBuilder`先用原Work decoder校验已捕获Task输入，再按`task/work`或`task/review`添加各自版本化场景Prompt；原Task、Sprint、Milestone、blockers及有限历史字节与输入身份保持不变。`DecodeTaskContext`校验对应场景版本和内容后返回Work自有typed视图，不授予Task mutation、review启动或运行权限。

`ExecutionContext`以schema 1固定原input、TriggerContext及input digest，提供封闭规范编码、显式解码与不可变副本；平台Prompt、Agent instructions与场景Prompt保持独立组件。该Build不新增DDL或持久事实，不生成最终SystemPrompt、Messages或ModelRequest，不重新解析当前Tool注册，也不写sealed Snapshot、running或Started。源码与方法已有限独审接受；来源`346003e6`的定向4 top/三包vet、compile/list及真实1 top/1 sub全部wholePASS。真实00054输入→Build→修改current后仍保持原Context已验证；原四Wait0、七资源14次absence与全部退出双尾闭合，1542输入首尾一致。结果及重跑方式沿[既有组合说明](../../../.agent-state/agent-system-integration/README.md)记录。


## 有限后继：Model AgentRetry 与首轮请求投影

`model.NewRuntimeWithAgentRetry`以显式`AgentRetryTiming`启用Agent文本重试，固定单次请求超时、增长倍率、上限及退避参数，不引入默认值、逻辑调用总期限或统一尝试次数。Model是唯一自动重试owner，仍须真实Consumer授权相应错误类别；Loop不得按Retryable叠加重试。00055扩展同一Call下多个Attempt/Invocation及其Usage记录，保留各次事实和当前attempt关系，旧Bounded构造与单次行为保持。Agent路径使用Execution拥有的credential lease，单个Model call退役不释放该共享租约，也不把公开Actor/Call/Round身份当运行授权。

`agentloop.BuildDirectTextRequest`仅从固定`ExecutionContext`组装首轮候选SystemPrompt、初始user消息与`ModelRequest`，沿原Model输入摘要规则保留Context来源身份；`InitialInput`只是未来Transcript writer的语义输入候选，不是已提交记录。该投影不读current、不调用Model、不写数据库；要求原捕获结果真实无tools、纯text且无reasoning，保留既定`InjectAgentsMD=false`、empty Mount等支持边界，非空tools等不支持配置明确拒绝，不通过丢弃工具适配请求。

来源`870b370b`已完成定向9 top及五包vet、compile/list与真实1 top/3 sub有限验证。原pure01的8项通过复用，唯一Loop测试浮点素材被canonical拒绝后仅修两处测试literal并补验该项；生产未改，原FAIL保留。native01整轮wholePASS（20.38s），真实TLS覆盖503→200及每attempt Usage、跨call复用Execution lease、取消阻止下一attempt并等待Stop/Drain真实退休、401仅一次attempt；原四Wait0、七资源14次absence与全部退出尾闭合，1591输入首尾一致。该验证限Model域，显式test-only Consumer不证明生产Execution运行授权。完整LoopController、持久Round/Transcript、sealed Snapshot与running/Started启动事务、生产app绑定均未由本片完成；tools/reasoning既有STOP保持，AgentRetry则是本片正式新增能力。结果与重跑方法沿[既有组合说明](../../../.agent-state/agent-system-integration/README.md)记录。

## 有限后继：Execution direct-text 首轮启动与终态

`execution.NewDirectTextDriver`消费真实已提交的00054 preparation input，复用固定Context、原ProcessGuard、初始Skill目录读验证与真实Execution运行授权。Loop先接受一个无I/O的session；00056在同一原Tx固定sealed Snapshot、首Round/Input、初始Transcript及running/typed Started事件，只有确认提交后才由该session调用Model。首Round Skill提供方重验原assignment/head和00051固定revision/ref完整集合，空集合也须有真实head；配置或绑定变化明确拒绝，不虚构动态分配能力。支持范围固定为原捕获结果真实empty tools、`ToolChoice=none`、text输出的单次JSON Turn，保留`InjectAgentsMD=false`和真实empty Mount，不通过删工具适配请求。

实际Model调用沿同一Execution Authority的私有live owner、持久Snapshot/Round/Input/Call及原Tx证明，Model继续独占AgentRetry。正常`finish_reason=stop`且响应为严格assistant text时，在原Model handle真正Joined后，同Tx写assistant Transcript、Execution `succeeded`/completed时间及typed Succeeded事件；Task保持`in_progress`，模型文本不代替Work状态变更。错误、不完整响应与取消不得冒充正常完成，相应终态仍须原调用实际退出和完整事务提交。启动或终态Unknown保留原owner和精确候选，只读核对原事实；Model结果或清理未终结则继续同一session/JSON handle的确认与Close/Drain，不重新BeginChat，Stop不等于Joined。

终态事务由Execution先证明原terminal候选与allJoined，再调用Model execution lease和专用ProjectVariable环境lease的两个真实退休提供方，最后追加对应typed事件；任一失败全部回滚，Unknown不重复退休写入。00057只增加环境lease的单向`released/released_at`资格退休，已退休lease不得被原capture重放重新取得。原lease tuple、环境head/ref及物理FK全部保留，历史引用仍可能阻止Secret或Variable删除，不把使用资格退休解释为可物理清理。

来源`f524b1fe`已完成定向20 top、十三包vet及compile/list。原pure01类型编译FAIL、native01取消FAIL、native02子selector入口FAIL（0业务测试）均保留；取消修复后仅补受影响的1 top/一包vet并重建compile02候选，其余有效通过证据复用。native03的`TestExecutionFirstRound`真实1 top/3 sub整轮wholePASS（39.05s）：正常ExecutionSucceeded且Task仍为in_progress、启动回执丢失后原owner恢复、取消后真实Joined并提交Cancelled与双lease退休均已验证。原四Wait0、七资源14次absence与全部退出双尾闭合，1588输入首尾一致。完整多轮Loop、工具调用、Stream、relaunch/cooldown与生产app/initializer绑定仍未完成，既有STOP保持。结果和可重跑入口继续使用[既有组合说明](../../../.agent-state/agent-system-integration/README.md)。

## 有限后继：Scheduler 关联执行的异步交付

`scheduler.NewProjectRunnerWithExecutions`显式接入`AssociatedExecutor`，旧Runner构造保持原行为。新关联成功后交付原Execution；既有launched记录按固定高水位、稳定ID和有界页补投，完整一轮结束才清游标并重取高水位，持续新增记录不延后旧轮回绕。每条均重读原Dispatch并由真实Execution observer核原key、digest、lineage和ExecutionID；页或交付失败不跳过该条，同一轮同一关联只交付一次，访问与跳过仍按原tick节奏推进。该扫描不以当前Task状态、Sprint或scheduler_enabled过滤已有执行，暂停只阻止新的调度业务。

`execution.NewAssociatedExecutor`固定同Store、Authority与有效Process身份的原PreparationDriver和DirectTextDriver，使用显式`MaxOwned`与`RecoveryInterval`。调用方以独立`Run`建立服务寿命，`Ready`只表示寿命已建立；`Advance`仅准入或观察，不等待捕获、数据库回调或Model响应，也不把DTO当运行授权。后台原调用再次核完整持久关联：created沿真实Preparation→固定input→DirectText推进；preparing缺input且没有本实例原owner时明确deferred，不悄悄重新捕获。Unknown保原driver与精确调用，后继显式访问按间隔最多推动一次原恢复，不更换原Launch或Model调用身份，也不实现跨进程running接管。

Runner的取消与Stop/Drain只覆盖本次遍历及准入调用，不停止共享executor或已接受执行。executor停止时取消自身原调用并等待真实退出；保留Unknown或未终结owner时不能宣称Joined，已完成Model和环境lease仍沿原终态事务退休。Task完成语义、完整多轮Loop、工具调用、Stream、relaunch/cooldown及生产app/initializer绑定均不随接线完成，本片不新增迁移或通用结果读取接口。定向10 top（7项新增、3项原受影响）及三包vet、compile/list、真实Scheduler→Execution的1 top/2 sub均整轮wholePASS；真实旧关联补投与异步取消共37.80s，四原Wait0、七资源14次absence及全部退出尾闭合，1596输入首尾一致。本批无新增FAIL，既有FAIL和STOP保持；结果与重跑方式沿[既有组合说明](../../../.agent-state/agent-system-integration/README.md)收口。

## 有限后继：work 阶段 relaunch 与持久访问计次

`scheduler.NewRelaunchCoordinator`复用原Coordinator、Work当前事实与Execution占用提供方，要求显式非负skip count；经`ProjectRunnerOptions.Relaunch`接入同一Runner，旧构造与未接入行为保持。范围仅为`in_progress`、`task/work`；Work以独立不可变relaunch来源记录当前Task/Sprint/Milestone关系，与新pending Dispatch同Tx提交，不伪造todo ClaimGuard，也不修改Task版本、状态、rank或历史。Launch沿原Handoff与Execution提供方，claim与relaunch两个来源分别重验。

00058保存Scheduler task runtime、原visit回执和Work relaunch来源。计次按Task真正被遍历访问的次数进行，历史Execution补投不计入；首次观察当前phase最近终态Execution时，skip count大于零则同次减一并跳过，只有访问开始时remaining已为零才继续容量及Agent slot检查。显式零不额外等待；pause、pending或active Execution不扣次数。计次与原visit回执同Tx提交，重建owner读取持久剩余数；Unknown保留原visit及原调用，只读核对完整回执，不因未观察到行重复扣减或创建Dispatch。最近关联由Scheduler自域维护，并经原Execution observer核完整key/digest/lineage；旧claim历史以真实Work版本确定顺序，后继只在新的可靠关联事务中前移指针并清除旧cooldown，历史重放不能倒写。

每次新Dispatch仍须原同Store、完整锁和Schedule EX下的当前Project enabled/CurrentSprint、Task版本与assignee、真实Agent资格、无blocker/pending/active、额度及空闲slot。relaunch的已证AgentBusy只关闭原Dispatch为skipped，不恢复Task或重置已耗尽cooldown。已证最终Launch失败沿原原子Finalize处理：Work重读当前适用关系，保留用户后改字段，适用时同Tx写technical blocker、blocked状态、typed历史与schema 5 Outbox事件；已不适用则保留当前Task。旧claim失败记录、FK与codec保持兼容，Unknown和普通未分类错误不冒最终失败。

来源`146594e6`已完成定向18项独立top及四包vet、compile04/list；有效通过证据复用，修复后仅补受影响项。native04真实1 top/2 sub整轮wholePASS（25.59s），验证持久访问计次跨新owner恢复后创建新Dispatch/Execution且Task仍为in_progress，以及relaunch来源的真实最终失败、schema 5事件与原Tx整体回滚、同key正常结算重放。四原Wait0、七资源14次absence及全部退出双尾闭合，1614输入首尾一致。原pure01/02/03及native01/02/03 FAIL均保留。review因正式状态writer缺失继续Deferred；blocked系统reconciliation、完整多轮/tools/Stream及生产app/initializer绑定仍未完成，既有STOP不变。结果与重跑方式继续使用[既有组合说明](../../../.agent-state/agent-system-integration/README.md)。

## 有限后继：Human Owner review 三边（开发中）

第八批实现`in_progress→in_review`、`in_review→done`、`in_review→todo`，复用原Transfer/Lookup、schema 1及同Tx状态、assignee、comment/history写入，不新增迁移。三边均要求comment；交接reviewer和退回todo的下一assignee须显式提供，done省略则保原reviewer。仅目标todo要求零unresolved blocker，原Owner、pending/occupancy及完整锁重验保持。

本片限定Human无active Execution，遇active明确DependencyUnbound，不将规格允许的执行内交接永久判非法；Execution成功不自动使Task done。Work、HTTP（含安全`COMMENT_REQUIRED`→409映射）与真实PG 1 top/2 sub正在并行开发，首sub沿原真实TLS执行链取前置，尚未动态验证。AgentRun写权、review自动调度、生产app绑定及原STOP不因此开放。
