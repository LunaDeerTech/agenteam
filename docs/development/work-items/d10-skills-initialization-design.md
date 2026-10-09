# D10 Skills 初始化与内容规格

状态：rev3，2026-10-09，P2 持久初始化服务 SPEC 待独立审查。P1 纯契约、真实 builtin 与编码不变，既有[独立验收](../agent-team/d10-p1-recovery-verification.md)只覆盖 P1。当前实现基线为正式 main `ca9f2d5d`；D05 精确 Object Audit checker、D08 收敛口和初始化 Audit wrapper 已存在并正式交付，以下按实际源码更新依赖。P2 尚无实现或真实 PG/对象验收，不代表 D10 完成。

本轮授权新 Skill 域、迁移 `00027`、本卡及[主卡](d10-skills-initialization.md)的服务实现准备。§§1–9 保留原完整结果与 P1 规则，§§10–15 给当前可编码口、持久状态和验收分层。共享 D05/D08/root 变化先由 root 协调；Object runtime join 停止项不恢复。

## 1. 完整结果与边界

本卡交付：每个新 Project 的**真实 Add Skills 内容包 → 独立不可变 revision → D05 实际对象/规范引用 → D08 同 Tx 完成确认**，以及当前 Owner 元数据/包读取、原 key 恢复、当前生命周期停止与项目删除清理。生产缺任何真实绑定就保持 D08 `DEPENDENCY_UNBOUND`，不写无内容保护 Skill，不将 PUT 或构造 completed DTO 当成功。

可以独立于 D09 模型配置和未绑定 Tool/Mount 运行：包由仓库内审定文本构成，验证/打包不调用模型、不联网、不执行脚本、不建立 Agent。目录/读取是当前 Human Owner 的服务口。本卡不实现外部安装入口、Agent 分配、Tool 暴露、Runner staging 或 Agent 配置；它们以后消费同一 stable SkillID、不可变 revision、manifest 与对象 owner。无对应生产 API，不用默认成功实现宽接口。

依据：[开发计划](../development-plan.md) D10；[Skills](../../architecture/agent-skills.md) §§1–9；D01 [resources-skills](d01-contracts/resources-skills.md)、[domain-lifecycle](d01-contracts/domain-lifecycle.md) 删除矩阵；[D08 设计](d08-project-owner-design.md) §7 和正式 `project/contract/initialization.go`。D08 启用 initializer **同时**必须注册真实 `agent-skills-variables` participant，不能只消除创建端口的 unbound。

## 2. 真前置与已核缺口

| 前置 | 固定事实 / 必要最小变化 |
| --- | --- |
| D08 创建 | 已验 `ProjectSkillInitializer` 四方法、私有 `InitializationPlanIssuer`；`ValidateInitializationInTx` 同 live Tx/Project EX 只认 exact Project/Creation/key、注册 Service、active gate，状态 initializing/completed。D10不改该成功确认语义。 |
| D05 初始化权限 | `object/contract/authority.go:53` 拒所有 Service Read/Mutate，实际 Prepare/Reserve/Send/Publish 是 Mutate，Lookup 是 Read。候选仅放形状 `project-initialization + SkillRevision + CreationCause=Actor.CauseRef(UUIDv7)`；配置的真实 Skill authority 再核原 Creation/request、完整 D05 私有 issuer plan 与当前事实。其它 Service、owner、cause 继续拒绝；Service Read 只供 exact Lookup，不能开正文/Stat/transfer。 |
| D05 initiator 事实 | `object/transfer_upload.go:18–21` 仅 Human/Agent 取 ID，Service 会把空 UserID 写入 `uploads.initiator_id safe_id NOT NULL`。仅上述已验证初始化分支持久原 CreationID；保 Human/Agent 原路径。原 ObjectService Audit 的 InitiatorKind=Service/InitiatorID=CreationID 已符合闭集，不改通用 Audit actions/resource/producer/SQL 旧约束。 |
| 初始化中的 Object Audit | 正式 `object.NewProjectAuditAuthority(Store)` 以同 Store、原 Tx/Entry/key 的私有 witness 校验真实对象前置。正式 `project.NewInitializationAuditAuthority` 先核当前 active Project/Creation/owner/action 状态，再调用真实 Skill exact key/object/attempt 映射与 Object checker 的组合；其普通四口原样委托，constructor 不证明所注入 provider 的真实身份。本域实现这个缺失组合，不新增 Audit 枚举或泛放 Service。 |
| 已失败初始化的收敛 | 正式 `InitializationConvergenceAuthority` 已存在；同 Store 活 Tx/Project EX 核 accepted/initializing/failed/completed 的原 Creation/key/owner 关系，仅 active Project 原工作观察/收敛，不授新写/发布。重试由 D08 将原 Creation 重新置 initializing。 |
| 不可逆清理 | 当前 `CleanupReleaseAccess` 支持 Avatar 和 Knowledge 的精确原因变体，尚无 SkillRevision+ProjectDeleted。新增仅此 closed 分支，绑定当前 lifecycle cause/版本和 exact revision/upload/object；保全部旧变体。`ReleaseForCleanupInTx` 已按精确授权通用执行，当前未发现必须改其实现。 |
| Project 清理准入 | 当前 `project.LifecycleAuthority.ValidateLifecycleInTx` 对 CleanupPhase 明确 DependencyUnbound；Stop/Inspect 的既有真实当前 cause 校验不等于清理准入。真实 Skill Project delete 集成依赖正式清理授权，不能通过本域 allow 或查 Project 私表替代。 |
| 删除中的 Object Audit | 已交 D08 初始化 wrapper 只允许 active Project；删除不复用或放宽该 gate。需本域精确外层组合：当前 Project cleanup cause + 本域已关 serving 的原 revision/upload/object 映射 + 原上下文中的真实 Object 私有 witness。否则返回 unbound/拒绝，不能把 ObjectDelete 的 digest cause 当 CreationID。 |
| 生产装配与停止项 | D05 checker 和 D08 两库已可复用；真实 Skill provider 尚未实现，root 未绑定，Object runtime join 仍停止。新服务可以消费正式 ports 独立编码/PG验证，受控 delegate 不能替代真实对象发布、永久删除或 root/shared guard 组合验收。 |

无需改变 foundation 锁排序、D01 分层或 D08 已验 initialization 消费口。新增 `skill/contract` 位于第4层；实现可依赖 project/object/audit 的正式契约，project/object 契约不得反向 import skill。

## 3. 首个真实包与工程参数

[真实 builtin SKILL.md](../../../internal/central/skill/builtin/add-skills/v1/SKILL.md) 是实际可读指引：已有能力搜索/获取或总结文本 → 标准包 → 显式安装 → 另行授权分配；不增加工具权限、不承诺不存在的工具 schema。不是占位句或仅名称/description。业务名 `Add Skills`，内部 normalized_name=`add-skills`、bundle_id=`builtin.add-skills.v1`、revision=1，均作为本卡工程候选审定。

此结果只接内部 `TextFiles` 构造路径：UTF-8、LF、精确根 `SKILL.md`；路径 `/` 分隔，拒空段、`.`/`..`、绝对/反斜杠、NUL、重复及 NFC+case-fold 碰撞，保原合法字节。首包仅该文件，无脚本。公共 manifest 校验上限建议文件数128、单文件8MiB、总未压缩32MiB、路径1024B、入口文本256KiB；本块内部 builtin 另限制包≤256KiB。参数与 canonical ZIP v1 一并固定，不能扩成任意用户包入口。

canonical ZIP v1：按 UTF-8 路径字节排序、固定时间/普通文件mode、Store method、无extra/comment/link/目录项、计算每项CRC和SHA；校验编码后的完整容器恰好结束，无拼接、尾部、重复项。manifest 不含外部 URL、对象 key、凭据。builtin frontmatter 使用固定 `name: Add Skills` 和经审定非空 description；未来外部 installer 的格式支持另立卡，须复用同一内容规则并经明确适配进入 canonical package，不能把本结果当通用解压器已完成。

bundle 的字节、package SHA/size、manifest digest 首次规划即冻结；重启从相同 bundle_id+digest恢复。二进制升级不得替换已接受命令或已有 revision；保留恢复旧 bundle 的能力，缺 bundle 明确 unbound/failed。旧已完成重放不要求再次生成旧包。受保护内置包的未来升级是独立维护命令，本卡不自动升级。

## 4. 身份、当前授权与完整事务

SkillID 使用 `project/contract.SkillID` 的同一底层 typed ID；不可变 revision 另有 UUIDv7 `RevisionID`，D05 owner 为 `{Kind:SkillRevision, ID:RevisionID, ProjectID}`，不能把 SkillID/数字 revision 冒充 owner。完整映射是 Project → Skill → Revision → Object/upload/attempt；真实 `SkillAggregate(SkillID)` 必须由 D10 planner 发现，不能猜 RevisionID 就是父锁。

初始化命令 `NewCommandIdentity("skills", [projectID], "initialize", initialization_key)`；语义摘要绑定 service role/CreationID/ProjectID/key/bundle_id/冻结 manifest+package digest，排除 RequestID、process、网络 attempt。UUID/key 不证明授权。Human 读取每次同 User SH/Project SH/Skill SH 下调用真实 Project.RequireOwnerInTx(Read)，另一 Owner、另一 admin、撤销 Session 不可读；archived 可读，deleting/未初始化拒绝。Service 初始化仅上述 closed operations；AgentRun 无真实 D22 绑定一律 unbound。

所有 Tx 之前发现 D05 plans + D10 command/Project/Skill/对象锁 + 对应 Audit/confirmation 必需锁，一次 `AcquireAccessPlansInTx(plans, extra)`（无 D05 plan 的 Tx 一次 Store.AcquireAll）。Project 初始化写用 EX；其它模式按实际门禁，取最强 union。InTx 只 RequireHeld、验证 issuer/request/映射/同 live Store Tx 和当前事实；映射变化整个 Tx ResourceBusy 回滚，不补低序锁、不自动再调用外发。

1. **Plan Tx**：先 Project.ValidateInitializationInTx，再 original command receipt/语义；固定 SkillID/RevisionID/bundle/目标 mapping、技术 attempt。未提交不创建可见 Skill；未知先原 writer 序列化确认，确认拿不到锁不是已回滚。
2. **Prepare/Reserve**：Tx 外真正生成并验证 canonical bytes，调用 D05 PreparePayload；外层短 Tx ReserveUploadInTx 与本域 exact upload/object/attempt mapping 一起提交。Reserve 未明确 committed 前零 PUT/GET；Unknown 保原状态/cause，55P03/取消/确认Unknown均不降级。
3. **Physical**：D05 UploadPrepared 执行实际已封 payload；服从原 D05 attempt/fence/process/lease/verification，返回不是 publication。D10 不以 HEAD、过期、marker、local map 缺项证明 writer 已结束。
4. **Publish Tx**：重规划 exact PublishAccess，重验原 Project gate、command/attempt/语义；先插本域仍不可见的 Skill/revision，随后 D05 PublishVerifiedInTx（此 Tx 内 ExistingOwner，CreationCause仍原CreationID），得到 Available ObjectMeta且无prospective receipt，写 current_revision、immutable ObjectMeta/manifest、completed初始化checkpoint。Object canonical reference + D05 ObjectUploadComplete Audit + revision + checkpoint全有或全无。采用现 Artifact `publish.go` 的同 Tx 模式，不能先全局发布技能再补引用。D10不另造 Skill Audit action；D08之后负责 ProjectCreateCompleted/Event，D05负责 Object事实。
5. **Confirm**：四方法直接实现正式 initializer。DiscoverConfirmation 使用服务私有 issuer，绑定完整 request/actor、原 receipt、完整锁；ConfirmInitializedInTx同 D08 caller Tx 检查已持 locks、当前Project exact gate、保护标记、原bundle/Skill/revision/对象映射及已提交原子发布事实，禁止嵌套Tx/I/O。Confirm只核本域同Tx发布留下的准确ObjectMeta与不可变关系，不给Service额外Stat/正文权，也不在 Confirm 内开 Stat 自Tx。没有本域清理授权可独立更改 canonical mapping；后续删除与Confirm共用Project/Skill/Object锁。若新增消费者能绕过此不变量，须先补正式D05 InTx确认口，不能直接读D05表。

原 Publish Unknown 只能在原 command/Project/Skill/Object排他序列后以准确同义结果确认；无行仍需证明旧事务已终局。晚commit/rollback不换Skill/Revision/bundle/对象命令身份。发现同key异义报冲突，不能返回别人的已完成结果。

## 5. 失败、读流与项目清理

D08已定：失败保创建名称/原creation，不自动删除已产生技能对象。D10失败保原恢复事实与不可见 staging，不以 TTL 清空；未成功产生revision时不能供正文读取。D05 自身废弃候选按原 exact attempt 收敛，D10不把“重试”实现为永久 Cancel 同一 upload 后盲目重挂。要终止接受的创建是新的产品行为，本卡不新增 cancel/delete 未初始化 Project API。

正常 Owner `OpenPackage` 先当前权限+固定 revision，再 D05 actual ReadObject；无私有MinIO读。包reader保 meta/manifest副本，Close/取消必须区分实际本地 reader I/O join 与 D05 lease 的持久释放：wrapper Joined只在其所有Read/Close实际return后为真；D05释放Unknown仍保留原lease，由真实Object runtime/cleanup收束，不能把wrapper Joined当lease已释放。失败/Unknown不能宣称已读、已释放。Service Lookup不授予正文。普通archive保持合法read，不改既有快照；Project delete关闭serving且等待真实活动。

`SkillsParticipant` 名称沿现 `agent-skills-variables`，本域 adapter 只证明 Skills；生产根必须将所有实际启用的 Agent/Skills/Variables 域组合成同一 participant 并冻结 manifest 版本，不得用仅 Skills 的报告覆盖正在交付的 Variables。未启用域不能伪造实现，已启用域缺绑定不能默认为空。其 cleanup 排在 artifact-object/secret/outbox/audit 前（采用P登记的显式依赖）。

- Stop：同当前P lifecycle cause/phase验证后捕获 exact 本域 work/reader，短Tx确认再取消；Inspect 不创另一 stop cause。archive停止新写/既有初始化工作，保已发表包及合法读；delete同时撤正文准入。stopped取决于所有真实本地join/精确死亡及原PG事务终局，不取决于取消返回、租约过期或仅D08 work_claim已结束。
- Delete cleanup：同 Project gate+Skill/Object完整锁原子 tombstone 本域 serving，并通过扩展的 `ReleaseForCleanupInTx` 关闭原 upload/canonical（含reserved）gate；cause必须原Project lifecycleOperation+version和exactowner/object/upload，不允许单独删除 protected Add Skills。所有旧初始化/Lookup/Attach/Consume/Publish捷径仍先当前gate，不能凭历史success复活。
- 物理删除用正式 `DeleteUnreferencedWithinBudget`，pending reader/history/external lease、未终局writer/zero-marker保进度；**marker不是终止证据**。只有必要 payload/marker/lease清理可证明完成后删除本域正文metadata/manifest/初始化输入/attempt/checkpoint；无跨域cascade。本域participant完成后，D05 Project participant继续清其拥有的剩余技术记录；仅D08最终receipt可跨项目永久删除保留。因阶段顺序不能在本域cleanup中等待“后序Artifact/Object整个participant completed”，只能确认本域exact objects实际已清，避免环。
- 已发表revision清理保 exact upload→Object映射直到所需D05物理证明完成。D05对象消失/ResourceDeleted仅在已经同cause关闭gate+已证terminal的路径可作幂等结果，不把无行当原Unknown回滚。

## 6. 构造与运行责任

先构造同 Store/current Account/Project authority → Skill Authority（本域 Store+Project 端口，无 Object Service）→ immutable owner/cleanup routing（保已有提供者）与 Skill exact facts + 实际 Object checker → D08 InitializationAuditAuthority → 本域精确 lifecycle Audit 外层组合 → Audit Service → 真 Object Service/Runtime → Skill Service → D08 initializer 和完整生命周期 registry。缺必需 provider 构造失败；不采用可变 locator、nil 替身或跨域查表解除构造环。

Object 的 UploadComplete/Failed/Delete Audit 在其终态 UPDATE 之前执行；Skill provider 必须使用同 Tx 的真实当前本域映射、原上下文及 Object witness，不能要求 Object 终态已写完。D08 wrapper 不改变普通授权/Lookup/Cleanup。lifecycle 外层只拦截同本域已持久关闭映射的 ObjectDelete 或对应失败收敛；其它 Entry 一遍委托既有 wrapper，不能拦截任意 Service。精确待补门槛见 §14；门槛未齐时 root 保持 unbound。

D08驱动原creation恢复，D10不另启第二套自动发布队列；D10 Service登记所有实际调用/读流/补偿直到return，D05继续拥有自己的实际writers/leases。技术Recover只处理原命令事实，不越过D08当前init gate重新发布；公平扫描按持久pass/游标推进，不能前100个busy阻塞tail。全局最多16实际初始化工作（与D08已有默认上限一致，不放宽其配置），持锁Tx不等待网络/join。

Start/Check/停止复用现root共享30s启动、2s健康round及同一停机deadline/额外共享1s Force；恢复单项建议≤2s并取caller剩余最短。清理包括失败checkpoint必须走WithinBudget，不用WithoutCancel/新15s/2s脱离；超时返回但实际I/O未join继续留registry/ProcessGuard，DB最后实际Drain/Force仍发起。root装配需把D10也纳入guard持有与最后释放，不能直接调用Object.Runtime.Drain先释放共享guard。该组合需真实测试，纯类型不证明。

## 7. 候选文件所有权 / 分阶段完整结果

P1 已提交并通过本轮独立验收的范围：`internal/central/skill/contract/{types,package,read}.go`及各同名test；`skill/{builtin,package}.go`及同名test；`skill/builtin/add-skills/v1/SKILL.md`。真实body+纯规则通过仍不解除生产unbound，实际验证与限制见[主卡记录](d10-skills-initialization.md#p1-本轮独立验收)。

完整库候选新源：`skill/{service,store,initialization,object_authority,audit_authority,read,recovery,lifecycle,runtime}.go`及同名适用test；`tests/skills/{fixture,initialization,recovery,authorization,lifecycle}_test.go`。`skill/contract`只发实现真正消费的口，不提前生成Agent/Tool空实现。

共享变化须 root 顺序授写、独立审查，不和其它域并写：

1. D05 `object/contract/authority.go` 的初始化 Service closed shape、`object/transfer_upload.go` 的同分支 initiator，及精确正负/兼容测试。
2. D05 `object/contract/access.go` 的 SkillRevision+ProjectDeleted closed shape，`object/contract/reference_cleanup.go` 的适用说明与负例。先复用 `object/reference_cleanup.go` 已有实现；只有实际证明缺口再报额外路径。
3. 已交 D05 `object/project_audit.go`、D08 `initialization_convergence.go` / `initialization_audit.go` 原样消费，不新造重复 checker，不放宽 active gate。
4. 当前 Project 清理授权缺口与生产 registry/root 配置由 root 另配唯一 owner；此卡仅提出 §14 精确依赖，不自行改旧 Project、Account、Audit 或 app 源。
5. 本域迁移为 `db/migrations/00027_skills.sql`，只建 Skill 私有数据，不改旧迁移/共享 Audit CHECK。00024 Variables、00025 Knowledge、00026 Runner 的正式前缀由 root 精确集成后，才做 fresh/upgrade 真实迁移验证。旧 `/tmp` SQL 草案不再是实现输入。

P2 先交真实可构建持久服务与当前端口下的 PG 证据，再在共享依赖齐备且获资源窗口后做真实 D05/D08/对象组合。不得用占位 initializer、空包或成功 cleanup 替身宣布完整发布；也不因 Object 停止项而停止无冲突的本域实现。 §§11–15 是该分层的具体门槛。

## 8. 验收门槛（后续执行，本次未运行）

| 场景 | 必须可观察的事实 |
| --- | --- |
| 真实初始化 | 全新Project一次实际PUT/校验；非空 SKILL.md 与冻结manifest逐字一致；一Skill、一revision、一canonical；D08四口真实调用，Object Audit+ProjectAudit/Event各一次，最后才Project可用。 |
| 权限/计划 | 错Service/owner/project/creation/key/issuer/同Tx/缺真实Skill父锁/弱模式拒；Service尝试ReadObject/transfer拒；Human当前Owner读，跨Owner/admin/撤销Session拒；所有失败零外发/零部分publication。 |
| 幂等/Unknown | 并发同key、異key同Creation、RequestID变化、Reserve/Publish/D08确认的晚commit/rollback/55P03；原body/actor/owner/摘要匹配，保原Unknowncause，零重复PUT/revision/事件，旧writer活不得新外发。 |
| 内容/复原 | path碰撞/escape/非法UTF8/尾部/CRC/manifest不符/过限；首次规划后binarybundle变化不替换原包；重启exact旧process未知/存活拒接管，死亡+DB终局后原身份恢复。 |
| 删除/保护 | 单独删AddSkills拒；Project archive读/Restore不复活旧写；delete与Publish/Confirm/Owner读竞争；旧upload/receipt全部封闭；活lease/迟到writer保pending；exact空marker不代终止；其它Project不受影响。 |
| 公平/预算/join | 100项busy+tail实际推进；有新错误仍不饿死独立项；MinIO慢PUT/GET/delete/checkpoint锁、caller取消、force相反先后、实际Close阻塞；原共享预算不延长，未join留guard、DB最后。 |
| 适用兼容 | D05旧Human/Agent/其它Service负例、Avatar及A/D12已验cleanup变体；D08原missinginitializer/错误confirmation/T11Unknown；实际constructor/root启用participant、迁移fresh/最近已验序列升级；原预算/断言不削弱。 |

本卡没有待用户选择的模型/工具默认值；首个bundle与编码参数是供独立审查的工程候选。安装格式扩展、protected builtin升级、Agent删除/运行中分配等后段规则不得在实现中自行提前决定。S01只静态证明缺口和提出可实施路径；P1仅纯包/载体，不声称 Skill 服务、PG、MinIO、权限、恢复或生命周期已运行通过。

## 9. P1 固定编码与纯 Go 口

P1不开放外部安装API，只提供受信任调用的 `NewTextFiles` → `BuildPackage(ctx, input)` 与 `ParseCanonicalPackage(ctx, raw)`；解析器只接受同一text-only canonical v1，解析成功不等于安装成功。允许空附件，根入口必须非空、正文非空。UTF-8原字节保留；禁止NUL/CR，已有换行只能LF，不强制改写文件末字节。frontmatter只接受 `---\nname: <literal>\ndescription: <literal>\n---\n` 两行固定顺序及非空正文，不使用通用YAML解释器，不扩展标签/别名/多行语法。首包文本逐字来自审定候选。

路径上限1024B、拒空段/点段/绝对路径/反斜杠/控制字符；为跨平台避免drive歧义，冒号拒绝。碰撞键为 NFC→full case-fold→NFC，仅用于比较；拒同键和文件占用另一路径祖先，保原路径字节。数量128、单文件8MiB、合计32MiB、入口256KiB；全包字节另有明确ZIP头开销上界，builtin≤256KiB。`BuildPackage`/parse共享本次caller剩余与2s最短预算，在64KiB块间检查取消；无后台任务，CPU校验有固定输入上界。

ZIP精确参数：Store、UTF8 flags=0x0800、CreatorVersion=(Unix3<<8)|20、ReaderVersion20、DOS日期1980-01-01/时间00:00、Unix普通文件0100644，无extra/comment/目录/descriptor/zip64；路径UTF8字节升序，CRC32/压缩及原长写本地和中央头。进入标准ZIP reader分配前先核末尾EOCD及≤128条实际中央目录、准确范围/结束，防虚报count与模65536计数绕过；随后核实际CRC/长度/UTF8/内容/manifest并按同规则重编码，完整字节必须相等，因此拒prefix/tail/拼接、冲突local/central、替代压缩、links/devices和额外记录。它不是任意ZIP解压器。

manifest有固定format=`skill-zip-v1`/entry_path=`SKILL.md`/按序files；`File`保四字段 path/media_type/byte_size/sha256（数量精确字符串整数）。TextFiles的`.md`为`text/markdown; charset=utf-8`，其它文本为`text/plain; charset=utf-8`。manifest SHA是SHA256(`skill.manifest.v1`+NUL+规范JSON)。`Manifest`/`TextFiles`/`Package`/`BuiltinBundle`均私有closure，explicit projection返回副本，fmt/JSON/slog不泄露正文；manifest通过显式 `DecodeManifest` 严格重建，不允许通用JSON覆盖已有handle。

`SkillID`复用`project/contract.SkillID`；新`RevisionID`为独立UUIDv7类型。`Metadata`为严格JSON DTO；`RevisionMetadata`含已验证typed Object scope，只能用真实投影构造，拒通用JSON制造。Manifest在revision中为不可变载体。名称规范化只派生路由键：保display原字节、拒首尾空白/控制字符/斜杠、UTF8≤128B，空白分词用`-`连接后NFC及case-fold；`Add Skills`精确映射`add-skills`，这不证明受保护身份。description≤8192B；后段当前权限仍是必需条件。

`NewPackageReader(meta, *object.ObjectReader)`只接受对应完整对象、禁止range，匹配scope/ID/MIME/size/SHA/state/version/time。所有admitted Read串行，Close不等Read mutex且只调用一次实际Close；实际Read未返回或Close未返回时Joined=false，Close panic不能伪成功。Joined只证wrapper同步本地I/O结束，不代表D05释放lease已经提交。构造失败不接管body，成功由wrapper Close负责；未绑定OwnerReader没有默认实现。

冻结builtin：bundle_id=`builtin.add-skills.v1`、revision1；SKILL.md 2473B SHA `a5f2416d9531ca0d450d97fd9d7064187c3d865573b9b20d874d146f1ee9663d`；ZIP2587B SHA `a67cef2cf755baa48880ea1727444ab060ac6237e5505e99081e868c91f1dcf1`。构造时核这两个常量，字节漂移必须新版本/审查，不能换同ID已有事实。无创建、安装、对象、Project成功状态产生。

## 10. 当前构造口与职责

正式 `ProjectSkillInitializer` 四方法、`InitializationPlanIssuer`、`InitializationConvergenceAuthority` 和 P1 `OwnerReader` 的签名原样实现，不复制契约。新 `skill.NewAuthority(Store, ProjectPorts)` 只接同一 Store 和 Project 当前授权/初始化/收敛/生命周期端口；其中 lifecycle 端口拒绝 CleanupPhase 时原错返回。`skill.New(Dependencies)` 必需 Authority、真实 Object Uploads/Objects/ReferenceCleanup/预算内清理口、精确 ProcessAuthority、当前 ProcessID 和 P1 私有 BuiltinBundle；缺任何必需口立即 DependencyUnbound，不允许启动后才悄悄补绑定。

Authority 实现 D05 ResourceAuthority、AccessPlanner、ObjectReadAuthority、ProjectGate、CleanupAuthority 所需的本域分支，只有本域 SkillRevision 可匹配；外域路由由不可变组合交给原提供者。D05 maintenance/技术 checkpoint 需要的父 Skill 锁也从本域 exact object/attempt 映射发现，不从 RevisionID 猜 SkillID。它不持有 Object Service，也不开外部 I/O。服务 implements initializer、OwnerReader 和本域 lifecycle adapter；没有通用安装/更新/独立删除口。

新 Skill Audit facts 同活 Tx 先核本域完整映射及当前正式 Project gate，再一遍调用实际 `object.NewProjectAuditAuthority` 所得 checker，传原 ctx/Tx/Entry/key。生产构造根应显式使用同 Store 的具体对象，单凭非 nil 接口不能证明此关系；真实验收必须有真实 checker 的正例和缺私有 witness 的反例。禁止跨模块 SQL、复制 witness、造成功 Entry 或只比较 DTO。

## 11. 持久数据与不可变关系

迁移 00027 建 `agenteam_skill` 私有 schema，所有主键 UUIDv7，版本正整数、状态 closed CHECK，业务外键只在本域，跨 Project/Object 关系由正式端口验证；只使用 Foundation 已有类型/锁序，不新增数据库函数绕授权。

| 数据 | 约束和存续 |
| --- | --- |
| `initializations` | 每 Project 一条 original creation/key，creation 唯一；完整 command identity/semantic digest、稳定 SkillID/RevisionID、bundle_id/包与 manifest digest、冻结 manifest/大小/name/description、phase/version。Project+key、creation、SkillID、RevisionID 各有防歧义唯一约束。同 key 异义拒绝，异 key 同 Creation 不能另建 Skill。phase 仅 planned/reserved/published/failed；failed 不等 D08 CreationFailed，也不抹除 attempt/Unknown。 |
| `skills` | SkillID、ProjectID、name/normalized_name/description、protected、current_revision、version、serving tombstone。Project+normalized_name 唯一；本结果唯一受保护 Add Skills，revision=1。仅 Publish Tx 插入，不把 planned 行列入目录。 |
| `revisions` | RevisionID、SkillID/ProjectID/revision、immutable manifest/包摘要/真实 ObjectMeta/发布时间。SkillID+revision 唯一；每个完整 ObjectID 只属此 revision。本域 FK 保父关联，ObjectMeta 逐字段通过正式 typed constructor 恢复，不让通用 JSON 制造 scope。 |
| `object_attempts` | 本域 command/Skill/Revision 与真实 UploadID/ObjectID/AttemptID、创建 ProcessID、当前 attempt/version 的 exact 映射；一个 upload/object 对应一 revision，旧 attempt 留存到真实收敛。不能用本表自造 verified/published 代替 D05。Reserve 与 mapping 同外层 Tx；D05 Audit failure ordinal/cause 必须能映射原真实 AttemptID。 |
| `work` | 接受的实际初始化调用/包读取、ProcessID、原 operation/cause、开始与 joined/unknown 状态、稳定 work UUID 和 fence。持久闭合不先于实际调用/Read/Close 结束；取消或进程心跳过期不删行。只保存必要安全标识，不保存包正文、用户凭据、底层 error 文本。 |
| `cleanup` | 每 Project+当前 lifecycle operation 的 action/version、gate/checkpoint 与 exact revision/upload/object、Object CleanupID、结果。重放只复用同 cause，跨 cause 不接管。服务关闭与原回执失效同 Tx，物理完成前保所有证明映射。 |

只保固定 builtin 的内容摘要/manifest/metadata，正文从审定 bundle 或真实 Object 读取，不在 PG 保存第二份包。计划缺行与矛盾行有别：无权范围先权限拒绝；已授权但 canonical 关系损坏返回 Unavailable，绝不当 NotFound 自动重建。已完成 command 结果从持久 revision 投影，不因进程升级重新生成包或换原 bundle。

## 12. 服务步骤、错误与恢复

`InitializeProjectSkills` 逐步执行 §4 的 Plan → Prepare → Reserve → Physical → Publish，每步前重验当前原 request。工作上限16，名额包括取消后未真实 return 的调用；满额 ResourceBusy。短 Tx 之外的任何读写都受 caller 原 deadline 和 D05 实际预算限制；不自动再执行已可能外发的 callback。

- `InspectProjectSkills` 只用收敛口观察原 command/revision/attempt；无本域命令返回 Pending，published 且全映射一致返回 Completed，已有安全 failed checkpoint 返回 Failed。它不执行 Reserve/Send/Publish，不通过“没看到记录”清除原 Unknown。
- `DiscoverConfirmation` 只从准确已发表结果发私有 issuer plan，含 Project EX、真实 Skill EX、Object EX 和原 command 必需锁；`ConfirmInitializedInTx` 拒另一 issuer/actor/request、弱锁、外 Store、已结束 Tx，重验当前 Project gate/完整不可变映射，不嵌套 Tx、不做 I/O。
- 每一步的 Foundation Fault 原 commit state、cause 和 metadata 原样传递；已接受写的 Unknown 不改成 KnownFailure/NotStarted。Malformed 输入 InvalidArgument，形状或当前权限不符 Forbidden，未绑定 DependencyUnbound，状态不允许 InvalidState，语义异义 IdempotencyKeyReused，预期版本不符 VersionConflict，当前争用 ResourceBusy；底层未知/损坏 Unavailable。仅明确业务失败写闭集 SafeReason，不存原错误文本。
- Commit Unknown 后只在原 complete locks 与 writer 终局序列下重读同 command，可证同语义已发表才确认完成；未确认仍返回原 Unknown。COMMIT ACK 丢失必须真实代理验证，受控 Fault 只证明传播。ProcessAuthority 只用于精确旧 ProcessID 已停止；必须另证旧 Tx 已结束才接管，不能用 TTL/lookup 缺行或本地未登记代替。
- D05 `ReserveUploadInTx` 对活旧 candidate 返回 busy、对同进程 reserved/verified 或 committed 返回原 handle；D10复用该行为，不发明 attempt 重置。允许接管时仍用原 upload/command 和冻结包，持久新 candidate 映射再 PUT；每个真实 attempt 最多一次本域外发选择，不保证网络层 exactly once。
- 本域 `Recover` 只推进原工作收敛/账本，不代 D08 重启初始化发布。按持久 cursor/pass 扫描固定有界批次，忙项记位置再走 tail；不每轮只查前100。当前 cause、process 终局和权限均不满足时保 Pending/Unknown，不能返回成功 stop。

OwnerReader 每次用原 actor 当前授权，不沿用 initialization Service；列表只有本结果的有限 protected 目录，具体 ID/revision 先验证当前 Project Owner 后查本域。读包是 immutable revision 的实际 ObjectReader→P1 PackageReader；本地工作直到所有 Read/Close 返回才 join，D05 lease 的 Unknown 另保真实待收敛事实。archived 可读、deleting 拒绝；服务 Stop 的当前 lifecycle gate关闭新入口，不能只靠本地 map 检查。

## 13. 停止、删除与 Audit 的闭合条件

本域 registration 使用 SkillsParticipant，reference kinds 固定 `skill-initialization`、`skill-package-reader`、`skill-object-cleanup`，ID 来自对应持久 work/cleanup。新 scope/cause 先正式 Project ValidateLifecycleInTx，再同 Tx 抓取本域 exact ref 集；RequestStop 只取消捕获工作，InspectStop 重验相同 operation/action/version 并检查实际 join。跨进程必须精确 ProcessAuthority+原 Tx 终局；本地 cancel/Close 调用返回不单独证明 I/O 全退出。原错误和未结束工作继续出现在报告。

Archive 不删 immutable 内容；现有允许的 Owner read 保持，不能把停止写等价撤销全部合法读。Delete 才关闭本域 serving 和正文准入。Cleanup 未获正式 CleanupPhase gate就返回原 DependencyUnbound；有门禁后同完整锁持久 exact lifecycle cause 并调用 ReleaseForCleanupInTx，关闭 canonical/reserved 及旧 Upload gate后才在 Tx 外预算内物理删除。CleanupCompleted 必须每一 exact Object 实际 terminal、本域活动完全 joined；否则返回 Pending/Unknown refs，不用成功空列表。

D08 active-only 初始化 Audit 分支保持原样。新本域生命周期 Audit 外层只能针对已关 serving 的 SkillRevision 原 ObjectUploadFailed/ObjectDelete，核本域 persisted lifecycle operation/action/version/Project 与原 creation/key/attempt；用恢复出的原 ProjectLifecycle actor 验当前 CleanupPhase，再调用同 Store Object checker。它**不能自行制造 Project 授权**：上述 actor只是 port 的输入，真实当前 gate必须通过。ObjectDelete key 的 digest 由真实 Object checker 验其实际 cleanup cause；不要求该 digest等于 CreationID，也不假定历史最早 cleanup_operation 就是本轮 lifecycle ID。没有真实 witness、映射、当前清理门禁任何一项即拒。

完整清理结束后删除本域内容/初始化输入/attempt/work/checkpoint；保留到 Object Audit append 和技术收敛全部终局。普通 Project/Audit四口及外域 Entries 一次原样委托，不授 Owner/任意 Service 直接删除 protected Add Skills。原 D08 deletion receipt 是最后跨域事实，不新增第二套永久业务凭据。

## 14. 当前明确待协调的共享差异

| 所属 | 必要变化与授权边界 |
| --- | --- |
| D05 permissions | 仅 `ProjectInitialization + SkillRevision + Actor.CauseRef=CreationCause UUIDv7 + same Project` 允许 OwnerAuthorization Read/Mutate；operation planner 进一步仅授 Prepare/Reserve/Send/Publish/Lookup 及合法收敛。ReadObject/Stat/transfer 等不能借该宽 intent偷渡。只此分支的 upload initiator 使用 CreationID，Human/Agent 原样。 |
| D05 release | NewCleanupReleaseAccess 精确增加 SkillRevision+ProjectDeleted；具体原 implementation可复用。原 Avatar/Knowledge全变体及其它 owner拒绝不能变。 |
| D08 lifecycle | 当前 CleanupPhase unbound 是真实未绑定边界。所需正式端口必须证明 operation/action/version/Project/required participant 当前 Cleaning、原Owner和删除gate；本域只能消费，不能查 Project 私表或用初始化 active gate替代。root协调该实现与独立验收后才解真实清理门槛。 |
| Audit 构造 | 复用已正式两个 checker/wrapper，新 Skill组合不改共享Audit闭集/迁移。初始化 facts和后续 lifecycle外层都是本域精确代码；生产装配前真实 Object witness正反+完整 Action/ordinal/key映射验收。 |
| Root/生命周期 manifest | root依据届时实际启用 Variables等域精确组合 `agent-skills-variables`，保其它既有 participant/owner routes及依赖顺序。没有完整组合/实际停止guard验收不绑定生产 initializer。这里不授权 app旧源，不恢复 Object runtime join停止项。 |

这些条件分别报告未完成，不能因本域 PG PASS改写成整 D05/D08/D10已完成。若实现发现现有 formal port确实无法证明某当前事实，先给精确差异和风险，请root协调；不会以新通用allow端口绕过。

## 15. 当前实现与验收分层

1. **纯层**：新 constructor缺依赖、闭集 permission/plan issuer、精确 parent locks、语义幂等、metadata重建、原Fault/Unknown身份、实际Reader/call join与fair扫描控制。P1当前未变结果可复用；改动相关处才重跑。
2. **真实 PG 本域**：迁移完整前缀fresh/upgrade；真实 Account/Project/Creation/currentSession；同 Tx Plan/Reserve映射/Publish原子结果和4口确认；跨Store/endedTx/漏锁/弱锁/跨Owner/归档删除gate；并发同key/异义、独立连接晚提交/回滚/锁争用。受控 Object delegate精确标注，不能称真实Object成功；真实 Object checker缺witness负控必须拒绝。旧基础/schema权限不变。
3. **真实对象组合**：共享门槛齐后实际 D05 Uploads/ObjectReader/MinIO/私有 Audit witness。真实包逐字相同，一Skill/revision/canonical和各exact Audit；注入 Reserve/Publish ACK丢失并核原身份，无活writer重发；实际取消/迟到/关闭/释放Unknown、Project archive/delete与读/发布竞争、原upload回执失效、全部物理清理终局。未运行项逐项保留，不用PG受控delegate替代。
4. **运行/生产组合**：真实 lifecycle provider与完整多域participant、公平tail、共享启动/健康/停止budget、原guard至actualjoin/DB最后、生产构造和root检查。Object停止项未解时此层不可执行/不可通过；本域可构建库仍继续交付准备。没有新增HTTP/UI端点，不宣称整站创建可用。

源码放新 `internal/central/skill/*.go`、适用同包tests、`tests/skills/*_test.go` 和00027；测试fixture复用现 tests/testsupport和既有有界监督器，不复制一套资源框架。实际资源命令/输入/预算先报root按全局窗口执行，全部direct/adopted Wait、资源ID/私有目录/双TCP尾闭合后才释放。独立验收由未参与实现者选风险补集，作者pure/PG不能冒独验。README在对应事实接受后更新，当前仅SPEC，无服务/PG/对象/生产PASS。
