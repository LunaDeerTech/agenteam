# D10 Skills 初始化与内容规格

状态：rev1，S01 主卡已独立审查并采纳；当前仅 [主卡](d10-skills-initialization.md) P1 纯块获实现授权。§§1–8 保留完整后段目标与未实现前置；不能把工程规格当真实业务验收。S01 固定基线 `71dc17671631632bb26e251ad8491e74092ac975`，P1 实现基线 `f401c15a5187690889eaea9ba9672ca8bdc85460`。

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
| 初始化中的 Object Audit | 已验 Project authority 只接受 ProjectAction。P r4 未验候选支持 Object facts 但 `audit_authority.go:252` 仍在 provider 前拒 `!initialized`，故不能直接用来发布初始化对象。必须有**初始化专用路线**：当前 exact Creation gate + Skill 本域 object/attempt 映射 + A 正式 D05 exact-fact provider 三者同 Tx 全满足。禁止泛放未初始化项目的 Object/Secret/Artifact/任意 Service。 |
| 已失败初始化的收敛 | D08 会保留 failed creation/name，现写授权只认 initializing/completed。候选新增独立窄 `InitializationConvergenceAuthority`（见 §10 后段形状）：只核原失败/未决工作的元数据观察、失败 Audit/清理，不允许 Reserve/Send/Publish 或把 Project 标可用。重试新写必须由 D08 原 creation 重新进入 initializing。不能让错误路径借用普通 Owner，也不能把合法 failed 原因丢成“未找到”。 |
| 不可逆清理 | 当前 `CleanupReleaseAccess` 和 `ReleaseForCleanupInTx` 只支持 Avatar。新增闭集 `SkillRevision + ProjectDeleted`，必须绑定原 Project lifecycle operation/版本、Skill revision、UploadID、ObjectID；原 Avatar 及任何先已验的新变体保留。仅普通 Release 会留下可 Attach 的 existing-owner upload，不足以封旧回执。 |
| P/A 真绑定 | P r4 三文件仅用来定位接口/排序，**未验**。A 已明确：当前已冻结首段只有 stop/work/fence，**没有 D05 `audit.ProjectFactAuthority` 实现或 constructor**。这是同时影响 D10/D12 的独立未实现前置，不能假定可注入。须另交真正同 Store、无 Service 构造环的精确事实校验器并验收，连同 A 的 stop/物理收敛及 P lifecycle authority 再接入；不消费活动稿、不跨域直接查/写私有表代替它们。当前卡不授权这些旧域修改。 |

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

`SkillsParticipant` 名称沿现 `agent-skills-variables`，本次注册声明启用owner仅Skills，不伪造Agent/Variable实现；将来启用这些领域必须扩同participant正式组合与manifest版本，缺绑定不能默认为空。其 cleanup 排在 artifact-object/secret/outbox/audit 前（采用P登记的显式依赖）。

- Stop：同当前P lifecycle cause/phase验证后捕获 exact 本域 work/reader，短Tx确认再取消；Inspect 不创另一 stop cause。archive停止新写/既有初始化工作，保已发表包及合法读；delete同时撤正文准入。stopped取决于所有真实本地join/精确死亡及原PG事务终局，不取决于取消返回、租约过期或仅D08 work_claim已结束。
- Delete cleanup：同 Project gate+Skill/Object完整锁原子 tombstone 本域 serving，并通过扩展的 `ReleaseForCleanupInTx` 关闭原 upload/canonical（含reserved）gate；cause必须原Project lifecycleOperation+version和exactowner/object/upload，不允许单独删除 protected Add Skills。所有旧初始化/Lookup/Attach/Consume/Publish捷径仍先当前gate，不能凭历史success复活。
- 物理删除用正式 `DeleteUnreferencedWithinBudget`，pending reader/history/external lease、未终局writer/zero-marker保进度；**marker不是终止证据**。只有必要 payload/marker/lease清理可证明完成后删除本域正文metadata/manifest/初始化输入/attempt/checkpoint；无跨域cascade。本域participant完成后，D05 Project participant继续清其拥有的剩余技术记录；仅D08最终receipt可跨项目永久删除保留。因阶段顺序不能在本域cleanup中等待“后序Artifact/Object整个participant completed”，只能确认本域exact objects实际已清，避免环。
- 已发表revision清理保 exact upload→Object映射直到所需D05物理证明完成。D05对象消失/ResourceDeleted仅在已经同cause关闭gate+已证terminal的路径可作幂等结果，不把无行当原Unknown回滚。

## 6. 构造与运行责任

先构造 Store/current Account/Project authority → Skill Authority（只本域Store+Project端口，无Object Service）→ immutable owner/cleanup routing（保Avatar、Artifact等原提供者）及初始化Audit facts（D10映射+A D05facts）→ 新的 P InitializationAuditAuthority 包装已构造ProjectAuthority与上述facts → Audit Service → 真Object Service/Runtime → Skill Service（注入真Object ports+私有bundle）→ D08 Service initializer与生命周期registry。包装器仅新增初始化closed路线，其余方法委托真实原ProjectAuthority；不能反过来要求ProjectAuthority构造前先有SkillService。constructor缺任何必需provider拒绝，不用late locator、nil替身、隐式反向查表解决环。其中 A fact provider 当前不存在，所以上述是目标构造顺序、不是已可执行装配。独立前置应优先以同 Store 的只读新 checker 构造；不能依赖 Object Service/Audit Appender。现 UploadComplete/Delete 的 Append 在终态 UPDATE 之前，不能要求事后状态已经出现，也不能以任意 Entry 代事实；逐 action 的持久前置和必要私有同 Tx 见证或最小次序调整须由该独立前置卡审定。无此真实实现时 root 明确 unbound，不发明占位 getter。

D08驱动原creation恢复，D10不另启第二套自动发布队列；D10 Service登记所有实际调用/读流/补偿直到return，D05继续拥有自己的实际writers/leases。技术Recover只处理原命令事实，不越过D08当前init gate重新发布；公平扫描按持久pass/游标推进，不能前100个busy阻塞tail。全局最多16实际初始化工作（与D08已有默认上限一致，不放宽其配置），持锁Tx不等待网络/join。

Start/Check/停止复用现root共享30s启动、2s健康round及同一停机deadline/额外共享1s Force；恢复单项建议≤2s并取caller剩余最短。清理包括失败checkpoint必须走WithinBudget，不用WithoutCancel/新15s/2s脱离；超时返回但实际I/O未join继续留registry/ProcessGuard，DB最后实际Drain/Force仍发起。root装配需把D10也纳入guard持有与最后释放，不能直接调用Object.Runtime.Drain先释放共享guard。该组合需真实测试，纯类型不证明。

## 7. 候选文件所有权 / 分阶段完整结果

独立可先行：`internal/central/skill/contract/{types,package,read}.go`及各同名test；`skill/{builtin,package}.go`及同名test；`skill/builtin/add-skills/v1/SKILL.md`。真实body+纯规则是可审结果，仍不解除生产unbound。

完整库候选新源：`skill/{service,store,initialization,object_authority,audit_authority,read,recovery,lifecycle,runtime}.go`及同名适用test；`tests/skills/{fixture,initialization,recovery,authorization,lifecycle}_test.go`。`skill/contract`只发实现真正消费的口，不提前生成Agent/Tool空实现。

共享候选须root顺序授写，不能和A/P/D12/B同时写：

1. D05 `object/contract/authority.go`（上述服务closed形状）及专门新初始化负例test；`object/transfer_upload.go`（Service initiator UUID仅该分支）。
2. D05 `object/contract/access.go`、`object/contract/reference_cleanup.go`、`object/reference_cleanup.go`（SkillRevision+ProjectDeleted closed分支），新同包test；原Avatar/已采其它variant保留。无需先扩大 object/access.go、普通cleanup.go、foundation。
3. A 独立后段前置：优先新 `object/project_audit_facts.go` 及对应 test，构造只接同 Store，消费已验 `audit.ProjectFactAuthority`；不得借 Service/Appender 或跨 Project 表。若仅已持久事实不能充分证明某 action，精确列该 action 现有调用文件与私有同 Tx 见证/写入次序方案，另行审定范围后才可改；本卡不把该 checker 当已有或临时 allow。
4. P 新 `project/contract/initialization_convergence.go`及test、`project/initialization_convergence.go`；新 `project/initialization_audit.go` 及test做初始化Audit专用wrapper：复用 `audit.ProjectFactAuthority`，普通授权/cleanup/lookup全部委托原authority。优先不改P活动 `authority.go`/`audit_authority.go`；若实际冻结构造链仍需旧调用点变动，先交精确delta获批。不修改已验 `ValidateInitializationInTx` 成功条件。
5. D08注册/组合根：`app/{account,object,resources}.go`必要窄调用点与新 `app/skills.go`，P registry的配置调用方；准确旧路径在冻结P/A后确认，不授权批量覆盖。注册Project初始化的HTTP/root入口仍属D08 B04，不以此提前宣布整站Project可用。
6. 新无编号DDL只在 `/tmp/agenteam-d10-s01-skills-uve1ji1r/skills-schema.draft.sql`（SHA `4b473f8785c8a15ca4a7b7e6a8f4b9d33569fd6f47bffe9a10f05fd904854dfc`）；全局编号由root在A014/B015等已验序列后分配，旧迁移不改。真实fixture脚本只在迁移归属与批次明确后精确更新。

先纯载体/真实bundle，随后共享补口独立审，最后同一完整Skill服务+真D08/D05/P/MinIO组合。P/A尚未验收阻止的是该集成门槛，**不阻止无依赖的新Skill纯包/服务准备**。不得以避共享文件为由交无内容假初始化或把cleanup留成成功stub。

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

## 10. 后段必要形状（未实现、未授权旧域写入）

以下仅把已审候选的组合口归位，不把它们放进P1接口或伪实现：

```go
// Project 新可选口：exact original failed/accepted work 收敛，不能新写/发布。
type InitializationConvergenceAuthority interface {
    ValidateInitializationConvergenceInTx(context.Context, foundation.Tx,
        identity.Actor, project.InitializationRequest) error
}
// 同活Tx、Project EX、真实registered service/CreationID/ProjectID/key/owner。
// accepted|initializing|failed|completed 的一致事实；删除走lifecycle cause。
// Skill Authority 先构造，只接 Store+Project 端口，不要求 Object Service。
func NewAuthority(Store, ProjectPorts) (*Authority, error)
func New(Dependencies) (*Service, error)
// Dependencies: Authority, real Object Uploads/Objects/ReferenceCleanup +
// DeleteUnreferencedWithinBudget, exact ProcessAuthority, validated BuiltinBundle.
// 新 P implementation wrapper 在SkillAuthority+A真facts之后、Audit之前构造：
func NewInitializationAuditAuthority(*project.Authority,
    audit.ProjectFactAuthority) (audit.ProjectAuthority, error)
```

既有 `ProjectSkillInitializer` 四方法和private confirmation issuer原签名不变。A Object facts当前未实现；同Store无Service环的checker、逐action真实前置以及必要私有同Tx见证由其独立卡决定，本规格不授权任意Entry或占位provider。原候选shapes `/tmp/agenteam-d10-s01-skills-uve1ji1r/contract-shapes.go.txt` SHA `aca3c103b36073aa47cc605492269e009be4e86e8ee7188dedca73943621b7ee` 供版本定位；P1实际公开口以新纯源码和本节明确边界为准。
