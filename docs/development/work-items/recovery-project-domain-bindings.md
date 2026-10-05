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
