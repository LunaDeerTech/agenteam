# D10 Skills 初始化与内容规格

状态：rev3，2026-10-10。P1 固定编码与纯口不变；P2 初始化／持久不可变内容／当前 Owner／精确 Stop 和 00027 有限库按 §§10–15 实现并接受，实际证据见[主卡](d10-skills-initialization.md)。§16 精确 Cleanup 库已另行有限接受；§§1–8 保留完整结果的目标和后段验收边界，生产 initializer、完整 participant 与 Runtime 不因库完成而视为交付。

历史 S01 `/tmp` 草案不是当前 DDL 来源；00027 为本次正式库模型。Object 原生 Audit checker、Project 初始化／收敛 wrapper 及有限 Skills CleanupPhase 已在 main，P2 消费初始化/读/Stop 子口，§16 后续精确清理库已闭合；完整多域 participant 仍未完成。

## 1. 完整结果目标与边界

完整目标：每个新 Project 的**真实 Add Skills 内容包 → 独立不可变 revision → D05 实际对象/规范引用 → D08 同 Tx 完成确认**，以及当前 Owner 元数据/包读取、原 key 恢复、当前生命周期停止与项目删除清理。生产缺任何真实绑定就保持 D08 `DEPENDENCY_UNBOUND`，不写无内容保护 Skill，不将 PUT 或构造 completed DTO 当成功。

可以独立于 D09 模型配置和未绑定 Tool/Mount 运行：包由仓库内审定文本构成，验证/打包不调用模型、不联网、不执行脚本、不建立 Agent。目录/读取是当前 Human Owner 的服务口。本卡不实现外部安装入口、Agent 分配、Tool 暴露、Runner staging 或 Agent 配置；它们以后消费同一 stable SkillID、不可变 revision、manifest 与对象 owner。无对应生产 API，不用默认成功实现宽接口。

依据：[开发计划](../development-plan.md) D10；[Skills](../../architecture/agent-skills.md) §§1–9；D01 [resources-skills](d01-contracts/resources-skills.md)、[domain-lifecycle](d01-contracts/domain-lifecycle.md) 删除矩阵；[D08 设计](d08-project-owner-design.md) §7 和正式 `project/contract/initialization.go`。D08 启用 initializer **同时**必须注册真实 `agent-skills-variables` participant，不能只消除创建端口的 unbound。

## 2. 当前前置与有限绑定

| 前置 | 当前事实与边界 |
| --- | --- |
| D08 初始化 | 正式 `ProjectSkillInitializer` 四方法、私有 plan issuer、初始化写 gate 和失败收敛 gate 已在 main。P2 同 Store/current Project/Creation/key 消费，不改其成功语义；生产 D08 initializer 和创建 HTTP/root 尚未接通。 |
| D05 初始化权限 | 本次 Object 窄分支仅允许 ProjectInitialization + SkillRevision + 同 Project、CreationCause 与 Actor.CauseRef 同一 UUIDv7。真实 Skill Authority 再核当前原请求和完整私有 plan；Service Read 只用于 exact Lookup，不能开正文/Stat/transfer，其它原变体不变。 |
| D05 initiator | 仅上述已授权 Service 分支持久原 CreationID，Human/Agent 原路径保留。不改 Audit 的 action/resource/producer 或旧 SQL 约束。 |
| 初始化 Object Audit | 正式 Project 初始化 wrapper 与 Object native checker 已在 main；本次新增 Skill facts 将当前原 Creation gate、本域完整 object/attempt 映射和原私有 witness 在同一 Tx 闭合。真实 D05 发布正向及公开字段伪 witness 拒绝已有限验收，不泛放未初始化 Project 的其它事实。 |
| 失败与 Unknown | 原 D08 收敛口只允许原工作观察及失败事实，不授新 Reserve/Send/Publish。P2 Inspect 不发 I/O，Recover 只收敛原工作账本；原 Unknown 与实际尾保留，不能用空查或 TTL 冒终局。 |
| 清理 | Release 的 SkillRevision+ProjectDeleted closed shape 本次加入，保 Avatar/Knowledge。P2 之后的 §16 库已实现 CleanupAuthority、私有事实及精确删除，并通过同 Store 的真实消费组合；该结果不等于完整 participant 或生产 root 生命周期启用。 |
| 生产组合 | main 的 Project/B02/Audit/Variables/Secret 与路由保持原字节。当前库不注册完整participant，不消除生产 root 的缺绑定，不恢复 Object Runtime join 停止项。 |

Foundation 锁序、D01 分层、正式 Project/Object 契约与 P1 编码不变；实现依赖上游契约，上游契约不反向 import skill。

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

后续完整 `SkillsParticipant`（现名 `agent-skills-variables`）必须按届时实际启用的 Skills、Variables、Agent 组合及 manifest 版本声明能力，缺绑定不能默认为空。本 P2 只实现 Skills 精确 Stop 子能力，不注册完整 participant。完整组合的 cleanup 排在 artifact-object/secret/outbox/audit 前（采用 P 登记的显式依赖）。

- Stop：同当前P lifecycle cause/phase验证后捕获 exact 本域 work/reader，短Tx确认再取消；Inspect 不创另一 stop cause。archive停止新写/既有初始化工作，保已发表包及合法读；delete同时撤正文准入。stopped取决于所有真实本地join/精确死亡及原PG事务终局，不取决于取消返回、租约过期或仅D08 work_claim已结束。
- Delete cleanup：同 Project gate+Skill/Object完整锁原子 tombstone 本域 serving，并通过扩展的 `ReleaseForCleanupInTx` 关闭原 upload/canonical（含reserved）gate；cause必须原Project lifecycleOperation+version和exactowner/object/upload，不允许单独删除 protected Add Skills。所有旧初始化/Lookup/Attach/Consume/Publish捷径仍先当前gate，不能凭历史success复活。
- 物理删除用正式 `DeleteUnreferencedWithinBudget`，pending reader/history/external lease、未终局writer/zero-marker保进度；**marker不是终止证据**。只有必要 payload清除、永久空marker核实与lease实际终局可证明完成后删除本域正文metadata/manifest/初始化输入/attempt/checkpoint；无跨域cascade。本域participant完成后，D05 Project participant继续清其拥有的剩余技术记录；仅D08最终receipt可跨项目永久删除保留。因阶段顺序不能在本域cleanup中等待“后序Artifact/Object整个participant completed”，只能确认本域exact objects实际已清，避免环。
- 已发表revision清理保 exact upload→Object映射直到所需D05物理证明完成。D05对象消失/ResourceDeleted仅在已经同cause关闭gate+已证terminal的路径可作幂等结果，不把无行当原Unknown回滚。

## 6. 构造与运行责任

先构造 Store/current Account/Project authority → Skill Authority（只本域Store+Project端口，无Object Service）→ immutable owner/cleanup routing（保Avatar、Artifact等原提供者）及初始化Audit facts（D10映射+A D05facts）→ 新的 P InitializationAuditAuthority 包装已构造ProjectAuthority与上述facts → Audit Service → 真Object Service/Runtime → Skill Service（注入真Object ports+私有bundle）→ D08 Service initializer与生命周期registry。包装器仅新增初始化closed路线，其余方法委托真实原ProjectAuthority；不能反过来要求ProjectAuthority构造前先有SkillService。constructor缺任何必需provider拒绝，不用late locator、nil替身、隐式反向查表解决环。其中同 Store Object fact provider 已正式存在，P2 已实际组合初始化路线；含 Runtime／cleanup／registry 的完整构造仍是目标，不是生产绑定。独立前置应优先以同 Store 的只读新 checker 构造；不能依赖 Object Service/Audit Appender。现 UploadComplete/Delete 的 Append 在终态 UPDATE 之前，不能要求事后状态已经出现，也不能以任意 Entry 代事实；逐 action 的持久前置和必要私有同 Tx 见证或最小次序调整须由该独立前置卡审定。无此真实实现时 root 明确 unbound，不发明占位 getter。

D08驱动原creation恢复，D10不另启第二套自动发布队列；D10 Service登记所有实际调用/读流/补偿直到return，D05继续拥有自己的实际writers/leases。技术Recover只处理原命令事实，不越过D08当前init gate重新发布；公平扫描按持久pass/游标推进，不能前100个busy阻塞tail。全局最多16实际初始化工作（与D08已有默认上限一致，不放宽其配置），持锁Tx不等待网络/join。

Start/Check/停止复用现root共享30s启动、2s健康round及同一停机deadline/额外共享1s Force；恢复单项建议≤2s并取caller剩余最短。清理包括失败checkpoint必须走WithinBudget，不用WithoutCancel/新15s/2s脱离；超时返回但实际I/O未join继续留registry/ProcessGuard，DB最后实际Drain/Force仍发起。root装配需把D10也纳入guard持有与最后释放，不能直接调用Object.Runtime.Drain先释放共享guard。该组合需真实测试，纯类型不证明。

## 7. 有限交付文件边界

P1 的 contract／builtin／package及真实SKILL.md保持原内容。P2 新增 `internal/central/skill` 的 authority、initializer、repository、read、work／recovery与精确Stop实现及相邻测试；八个作者集成源与一个独立集成源位于 `tests/skills`，独立纯测试仅从原overlay移到正式同包文件。数据模型为原字节 `db/migrations/00027_skills.sql`；旧前缀和依赖锁不改。

共享仅五个窄文件：Object authority的初始化Service闭集、transfer_upload的该分支initiator、access的SkillRevision+ProjectDeleted Release形状、reference_cleanup对应注释，以及旧cleanup owner/reason矩阵仅增加该合法格。另含两个Object初始化相邻测试。其余 Object、全部 Project／Audit／B02／Account／app与默认root不由本批覆盖。

生产 immutable routes、D08 initializer／多域 participant、完整清理与共享guard退休另属后段。它们不能用生产stub或成功空结果代替，但不阻止本P2有限库独立交付。已有必要COMMIT proxy依赖在main保留，不复制新的资源框架或私有产物。

## 8. 完整结果验收门槛（有限已验项见主卡）

| 场景 | 必须可观察的事实 |
| --- | --- |
| 真实初始化 | 全新Project一次实际PUT/校验；非空 SKILL.md 与冻结manifest逐字一致；一Skill、一revision、一canonical；D08四口真实调用，Object Audit+ProjectAudit/Event各一次，最后才Project可用。 |
| 权限/计划 | 错Service/owner/project/creation/key/issuer/同Tx/缺真实Skill父锁/弱模式拒；Service尝试ReadObject/transfer拒；Human当前Owner读，跨Owner/admin/撤销Session拒；所有失败零外发/零部分publication。 |
| 幂等/Unknown | 并发同key、異key同Creation、RequestID变化、Reserve/Publish/D08确认的晚commit/rollback/55P03；原body/actor/owner/摘要匹配，保原Unknowncause，零重复PUT/revision/事件，旧writer活不得新外发。 |
| 内容/复原 | path碰撞/escape/非法UTF8/尾部/CRC/manifest不符/过限；首次规划后binarybundle变化不替换原包；重启exact旧process未知/存活拒接管，死亡+DB终局后原身份恢复。 |
| 删除/保护 | 单独删AddSkills拒；Project archive读/Restore不复活旧写；delete与Publish/Confirm/Owner读竞争；旧upload/receipt全部封闭；活lease/迟到writer保pending；exact空marker不代终止；其它Project不受影响。 |
| 公平/预算/join | 100项busy+tail实际推进；有新错误仍不饿死独立项；MinIO慢PUT/GET/delete/checkpoint锁、caller取消、force相反先后、实际Close阻塞；原共享预算不延长，未join留guard、DB最后。 |
| 适用兼容 | D05旧Human/Agent/其它Service负例、Avatar及A/D12已验cleanup变体；D08原missinginitializer/错误confirmation/T11Unknown；实际constructor/root启用participant、迁移fresh/最近已验序列升级；原预算/断言不削弱。 |

本卡没有待用户选择的模型/工具默认值；首个bundle与编码参数是供独立审查的工程候选。安装格式扩展、protected builtin升级、Agent删除/运行中分配等后段规则不得在实现中自行提前决定。S01仅是原静态规格，P1仅纯包/载体；P2 已运行的服务、PG、MinIO与精确 Stop 按主卡固定版本组合计证，表内尚未闭合的完整清理／root不由它们代替。

## 9. P1 固定编码与纯 Go 口

P1不开放外部安装API，只提供受信任调用的 `NewTextFiles` → `BuildPackage(ctx, input)` 与 `ParseCanonicalPackage(ctx, raw)`；解析器只接受同一text-only canonical v1，解析成功不等于安装成功。允许空附件，根入口必须非空、正文非空。UTF-8原字节保留；禁止NUL/CR，已有换行只能LF，不强制改写文件末字节。frontmatter只接受 `---\nname: <literal>\ndescription: <literal>\n---\n` 两行固定顺序及非空正文，不使用通用YAML解释器，不扩展标签/别名/多行语法。首包文本逐字来自审定候选。

路径上限1024B、拒空段/点段/绝对路径/反斜杠/控制字符；为跨平台避免drive歧义，冒号拒绝。碰撞键为 NFC→full case-fold→NFC，仅用于比较；拒同键和文件占用另一路径祖先，保原路径字节。数量128、单文件8MiB、合计32MiB、入口256KiB；全包字节另有明确ZIP头开销上界，builtin≤256KiB。`BuildPackage`/parse共享本次caller剩余与2s最短预算，在64KiB块间检查取消；无后台任务，CPU校验有固定输入上界。

ZIP精确参数：Store、UTF8 flags=0x0800、CreatorVersion=(Unix3<<8)|20、ReaderVersion20、DOS日期1980-01-01/时间00:00、Unix普通文件0100644，无extra/comment/目录/descriptor/zip64；路径UTF8字节升序，CRC32/压缩及原长写本地和中央头。进入标准ZIP reader分配前先核末尾EOCD及≤128条实际中央目录、准确范围/结束，防虚报count与模65536计数绕过；随后核实际CRC/长度/UTF8/内容/manifest并按同规则重编码，完整字节必须相等，因此拒prefix/tail/拼接、冲突local/central、替代压缩、links/devices和额外记录。它不是任意ZIP解压器。

manifest有固定format=`skill-zip-v1`/entry_path=`SKILL.md`/按序files；`File`保四字段 path/media_type/byte_size/sha256（数量精确字符串整数）。TextFiles的`.md`为`text/markdown; charset=utf-8`，其它文本为`text/plain; charset=utf-8`。manifest SHA是SHA256(`skill.manifest.v1`+NUL+规范JSON)。`Manifest`/`TextFiles`/`Package`/`BuiltinBundle`均私有closure，explicit projection返回副本，fmt/JSON/slog不泄露正文；manifest通过显式 `DecodeManifest` 严格重建，不允许通用JSON覆盖已有handle。

`SkillID`复用`project/contract.SkillID`；新`RevisionID`为独立UUIDv7类型。`Metadata`为严格JSON DTO；`RevisionMetadata`含已验证typed Object scope，只能用真实投影构造，拒通用JSON制造。Manifest在revision中为不可变载体。名称规范化只派生路由键：保display原字节、拒首尾空白/控制字符/斜杠、UTF8≤128B，空白分词用`-`连接后NFC及case-fold；`Add Skills`精确映射`add-skills`，这不证明受保护身份。description≤8192B；后段当前权限仍是必需条件。

`NewPackageReader(meta, *object.ObjectReader)`只接受对应完整对象、禁止range，匹配scope/ID/MIME/size/SHA/state/version/time。所有admitted Read串行，Close不等Read mutex且只调用一次实际Close；实际Read未返回或Close未返回时Joined=false，Close panic不能伪成功。Joined只证wrapper同步本地I/O结束，不代表D05释放lease已经提交。构造失败不接管body，成功由wrapper Close负责；未绑定OwnerReader没有默认实现。

冻结builtin：bundle_id=`builtin.add-skills.v1`、revision1；SKILL.md 2473B SHA `a5f2416d9531ca0d450d97fd9d7064187c3d865573b9b20d874d146f1ee9663d`；ZIP2587B SHA `a67cef2cf755baa48880ea1727444ab060ac6237e5505e99081e868c91f1dcf1`。构造时核这两个常量，字节漂移必须新版本/审查，不能换同ID已有事实。无创建、安装、对象、Project成功状态产生。

## 10. 当前构造口与职责

正式 `ProjectSkillInitializer` 四方法、`InitializationPlanIssuer`、`InitializationConvergenceAuthority` 和 P1 `OwnerReader` 的签名原样实现，不复制契约。新 `skill.NewAuthority(Store, ProjectPorts)` 只接同一 Store 和 Project 当前授权/初始化/收敛/生命周期端口；生命周期本次只消费 Stop/Inspect，后段 Cleanup 不以端口形状冒已绑定。`skill.New(Dependencies)` 必需 Authority、真实 Object Uploads/Objects/ReferenceCleanup/预算内清理口、精确 ProcessAuthority、当前 ProcessID 和 P1 私有 BuiltinBundle；缺任何必需口立即 DependencyUnbound，不允许启动后才悄悄补绑定。

Authority 实现 D05 ResourceAuthority、AccessPlanner、ObjectReadAuthority、ProjectGate 所需的本域分支；CleanupAuthority 不在本 P2 实现，只有本域 SkillRevision 可匹配；外域路由由不可变组合交给原提供者。D05 maintenance/技术 checkpoint 需要的父 Skill 锁也从本域 exact object/attempt 映射发现，不从 RevisionID 猜 SkillID。它不持有 Object Service，也不开外部 I/O。服务实现 initializer、OwnerReader 和本域精确 Stop 子能力；没有通用安装/更新/独立删除口。

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
| `cleanup` | 00027 预留的当前 lifecycle operation/action/version 与 exact revision/upload/object、清理阶段/ID约束。本 P2 不写此表、不以 schema 存在声称 Cleanup 实现；原子 Release／物理终局／清理重放是后段责任。 |

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

## 13. 精确 Stop 与后段清理边界

本域 Stop 的 reference kinds 固定 `skill-initialization`、`skill-package-reader`，来自原持久 work。当前 scope/cause 先通过正式 Project ValidateLifecycleInTx；同 Tx 核当前 facts、完整锁并捕获 exact 工作集合，提交后才取消。InspectStop 重验同一 operation/action/version 和实际 join；外来 ProcessAuthority 证明后还必须持原事务锁重读原 process/kind/phase，不能用 TTL／本地无记录或 cancellation 返回当终局。100条有界扫描及 tail 保持 Pending，不以截断列表冒完成。

Archive 不删除 immutable 内容，现有合法 Owner read 保持；Delete 撤新正文准入且 reader 进入停止集合。原错误、Unknown 与未实际返回的调用继续阻止 Stopped。本 Service 不实现完整 participant Name，也不替 Variables/Agent 报完成。

D08 active-only 初始化 Audit wrapper 保持原样。本 P2 没有 CleanupAuthority 或 lifecycle Audit 外层；Project 已有有限 CleanupPhase gate 仍不足单独完成本域 gate/Release/物理清理/最后双域事务。D05 Release 的 closed shape、新 cleanup 表不等于清理可调用。后续必须保持 exact cause、当前权限、真实 native witness、原预算与 actual join，并独立验证；本文不带入后段清理实现。

## 14. 本次共享差异与后续依赖

| 所属 | 必要变化与授权边界 |
| --- | --- |
| D05 permissions | 仅 `ProjectInitialization + SkillRevision + Actor.CauseRef=CreationCause UUIDv7 + same Project` 允许 OwnerAuthorization Read/Mutate；operation planner 进一步仅授 Prepare/Reserve/Send/Publish/Lookup 及合法收敛。ReadObject/Stat/transfer 等不能借该宽 intent偷渡。只此分支的 upload initiator 使用 CreationID，Human/Agent 原样。 |
| D05 release | NewCleanupReleaseAccess 精确增加 SkillRevision+ProjectDeleted；具体原 implementation可复用。原 Avatar/Knowledge全变体及其它 owner拒绝不能变。 |
| D08 lifecycle | Project main 已交付有限 Skills CleanupPhase gate，核 operation/action/version/Project/required participant 当前 Cleaning、原Owner和删除gate。本 P2 不消费完整Cleanup，不查Project私表，也不以初始化active gate替代；后续真实Skills/Object清理组合仍是独立门槛。 |
| Audit 构造 | 复用已正式两个 checker/wrapper，新 Skill组合不改共享Audit闭集/迁移。初始化 facts为本次本域精确代码，真实Object witness正反与原Action/ordinal/key已有限验收；lifecycle外层另属后段，不在本 P2。 |
| Root/生命周期 manifest | root依据届时实际启用 Variables等域精确组合 `agent-skills-variables`，保其它既有 participant/owner routes及依赖顺序。没有完整组合/实际停止guard验收不绑定生产 initializer。这里不授权 app旧源，不恢复 Object runtime join停止项。 |

这些条件分别报告未完成，不能因本域 PG PASS改写成整 D05/D08/D10已完成。若实现发现现有 formal port确实无法证明某当前事实，先给精确差异和风险，请root协调；不会以新通用allow端口绕过。

## 15. 当前实现与验收分层

1. **纯层**：新 constructor缺依赖、闭集 permission/plan issuer、精确 parent locks、语义幂等、metadata重建、原Fault/Unknown身份、实际Reader/call join与fair扫描控制。P1当前未变结果可复用；改动相关处才重跑。
2. **真实 PG 本域**：迁移完整前缀fresh/upgrade；真实 Account/Project/Creation/currentSession；同 Tx Plan/Reserve映射/Publish原子结果和4口确认；跨Store/endedTx/漏锁/弱锁/跨Owner/归档删除gate；并发同key/异义、独立连接晚提交/回滚/锁争用。受控 Object delegate精确标注，不能称真实Object成功；真实 Object checker缺witness负控必须拒绝。旧基础/schema权限不变。
3. **真实对象组合**：共享门槛齐后实际 D05 Uploads/ObjectReader/MinIO/私有 Audit witness。真实包逐字相同，一Skill/revision/canonical和各exact Audit；注入 Reserve/Publish ACK丢失并核原身份，无活writer重发；实际取消/迟到/关闭/释放Unknown、Project archive/delete与读/发布竞争、原upload回执失效、全部物理清理终局。未运行项逐项保留，不用PG受控delegate替代。
4. **运行/生产组合**：真实 lifecycle provider与完整多域participant、公平tail、共享启动/健康/停止budget、原guard至actualjoin/DB最后、生产构造和root检查。Object停止项未解时此层不可执行/不可通过；本域可构建库仍继续交付准备。没有新增HTTP/UI端点，不宣称整站创建可用。

源码放新 `internal/central/skill/*.go`、适用同包tests、`tests/skills/*_test.go` 和00027；测试fixture复用现 tests/testsupport和既有有界监督器，不复制一套资源框架。实际资源命令/输入/预算先报root按全局窗口执行，全部direct/adopted Wait、资源ID/私有目录/双TCP尾闭合后才释放。独立验收由未参与实现者选风险补集，作者pure/PG不能冒独验。本次服务／PG／真实Object有限结果和独立风险补集已按主卡接受；生产root、完整Cleanup及全D10仍未完成，不把此规格的后段验证目标当成已运行。

## 16. Skills 精确 Project Cleanup（rev2，库范围有限接受）

本节约定的 Cleanup 库、当前授权/opaque plan、原子 gate、有限历史批次、真实 D05 元数据终局及 Audit 组合已按固定版本有限接受。Project 的唯一 Skills CleanupPhase 已在 main；本组合采用已接受 D05/00028，保留原 FK、00027 和 P2 初始化语义。原 Cleanup02 两 top/五子与 Historical02 一 top/两子的真实资源、原 Wait 和所有退出尾均完整通过；后者入口窄修后的结果不回填 Historical01 的 whole FAIL 或推断未知后代 PID 原因。历史映射 SQL fixture 与真实 native 重试分列，证据范围见[主卡](d10-skills-initialization.md)。

rev1 曾有删除 FK、无界删除历史 work、先删父映射阻断后序 D05 的问题；rev2 已撤回 drop FK，规定有限批次与两域最后 anchors 同 Tx。00028 由 D05 维护共享索引，本域没有新增 DDL。下面保留正式约束；默认根的完整 participant、后台调度、全 Project 清理及 Object Runtime join 仍未由本结果交付，原 Runtime STOP 不恢复。

### 16.1 可交付结果与授权范围

实现本域 `Cleanup(ctx, ProjectLifecycle actor, LifecycleCause, Project ScopeRef, *CleanupCheckpoint)`、`oc.CleanupAuthority` 的 exact SkillRevision 分支，以及同一 Authority 的 cleanup access planner／必要维护分支。没有通用 Skill 删除、Creation 取消、按目录扫描删除或 Project 全域 Object 清理入口。现有 Stop/initializer/OwnerReader 的正式语义不变。

| 当前正式能力 | 本结果如何消费／尚缺什么 |
| --- | --- |
| D08 `ValidateLifecycleInTx` | 复用原签名；main 已提供 §16.3 唯一 Skills 当前门禁。Stop 已验事实不能直接当清理许可。 |
| D05 `NewCleanupReleaseAccess`＋opaque `AccessLockPlan/LockedAccess` | 已支持 SkillRevision+ProjectDeleted、原 UploadID，具体 Release 同 Tx 无 I/O；本域 Discover/Validate 已补精确 CleanupReleaseAccess/ObjectCleanupAccess 分派。 |
| D05 `DeleteUnreferencedWithinBudget` | 同原 cause 获取当前 cleanup 授权后，负责实际 writer/reader/lease/marker/payload 和自己的事务尾；不由 Skills 操作 MinIO 或 D05 表。 |
| D05 元数据最终清理 | D05 已提供 §16.6 的单对象 `DeletedObjectMetadataPurger`，在本组合按限定范围消费。当前 `CleanupProject` 不能在 Skills 父表已删后代替该口。 |
| 本域旧 Maintenance provider | 保留 Inspect/FinishWriter/JoinAttempt/ReleaseReader/ReleaseProcess；已补 ClaimCleanup/CheckpointCleanup/FinalizeCleanup 的 exact parent 映射。它们不授普通读写或任意 Project 清理。 |
| Object Audit 私有 witness | `object.NewProjectAuditAuthority` 已有；active 初始化 wrapper 不能用于删除。新增本域精确 ObjectDelete 外层，原 context/Tx/entry/key 原样交真实 checker。 |
| D08 清理报告／checkpoint | 已有 opaque 类型，只作进度、没有免复验权限。最小清理回执仍只有 D08 最终 deletion receipt，Skills 不新增永久第二份项目墓碑。 |

### 16.2 原始映射与 reserved 边界

本域唯一初始化映射是 `Project/Creation/key → Skill/Revision → Object/Upload`，`object_attempts` 追加每个原 candidate 的 AttemptID/ProcessID；换 candidate 不换 Object/Upload。D05 在旧 candidate 已实际 closed 后接受重试：对旧 candidate 记 `AbandonedAttempt + 原 UploadID` cleanup，再生成新 candidate。新 candidate 后来 Publish、D08 正式 Confirm 后，已 initialized Project 仍可能留下旧的**未发布 candidate payload/marker**；它们可能已是 abandoned，而非字面 reserved 状态。这是本节要清的真实遗留来源，不创造第二个 Object 或重写原 attempt 归属。

Project Delete 时清理 canonical 引用及同一原 Object 下所有 candidate；本域在同 Store/Tx/完整锁下核原 Creation/key/Skill/Revision/Object/Upload，D05 再核实际 upload/attempt/cleanup 私有事实。旧 candidate 的 process 不是当前 cleanup worker；两者不能混为同一进程所有权。

00027 的 `cleanup→skills(project,id,revision)` FK 对本轮合法范围足够：旧未发布 candidate 与已发表 candidate 共用同一个 Skill/Revision/Object/Upload，已有发表行仍是正确父项。本轮保留该 FK 和全部 exact 映射约束，不用 DDL 增加尚未获准的无 Skill 清理形态：

- 当前 D08 `checkNewLifecycle` 拒绝 `!initialized` 的 BeginDelete；初始化收敛口只观察 active 原 Creation，不授不可逆清理。整个 Creation 尚未完成时，不能构造 ProjectDeleted、伪造当前 Cleaning，或把 failed 当已取消。
- 本轮合法正向是已 initialized Project 的当前正式 Delete/清理，以及该发布对象的旧未发布 candidate；已 initialized 而本域发布事实矛盾仍拒绝，不借新 FK 放过损坏映射。
- 未 initialized Creation 的独立取消／不可逆清理需要后续 D08 正式语义和授权，另列依赖。本轮不为它移除 FK、不加 schema 正例、不制造可用清理入口。

### 16.3 Project CleanupPhase 最小真实门禁

共享实现归 root 指定的 Project owner，优先沿原 `LifecycleAuthority.ValidateLifecycleInTx` 补**仅 SkillsParticipant** 的 CleanupPhase 分支，其他尚未实现 participant 保持原明确 unbound。不修改 D04/D06 的清理口或把整 CleanupPhase 统一 allow。

门禁不得开自己的 Tx、补调用方漏锁或做 I/O；同一 Store 的活 Tx 已持 Project SH（Skills 实际持 EX）才读 Project 私有事实，逐项核：

1. actor 为正式 ProjectLifecycle，ProjectScope、Actor.ProjectID、Actor.CauseRef、scope.ProjectID 和原 OperationID 精确一致；cause.Action=Delete，固定 ProjectVersion 合法。
2. 当前 Project 已 initialized、lifecycle=Deleting、当前 operation pointer 等于原 operation；Project.version 与 operation.project_version 均等于 cause.ProjectVersion，原 operation.owner 与当前 Project Owner 一致。当前 Session/Human 不是此后台许可来源，不能只核一个 Service 名称。
3. operation **当前**为 Cleaning、cleanup_stage=domains；Accepted/Stopping/Failed（即使 resume=cleanup）/Completed 全拒绝。失败必须由正式 Retry 重新进入原 Cleaning 后才继续，不由下游跳阶段。
4. 冻结 manifest 的 participant 名称、contract_version、owner module、reference kinds、CleanupAfter 与已注册兼容声明完全一致；Skills 必须存在，不按空表省略。所有 required participant stop_state 已 stopped；Skills 当前 cleanup_state 只能 required/pending，不能凭 completed 重新获得物理写许可。
5. Skills 的全部 CleanupAfter 依赖已 completed；不得越过 outbox/audit/final 屏障。此检查只证明当前清理准入，真实启用的 Variables/Skills 组合报告和全域 actual stop 由生产 registry 负责，声明元数据不冒实现绑定。

缺 binding/兼容版本保持 DependencyUnbound，错 actor/cause/Project/版本拒绝，阶段不符 InvalidState，矛盾持久事实 DependencyUnavailable。Project 已删除的最小 receipt 只可支撑另有明确契约的终态查询，本分支不给新清理、Audit 或资源写权。Stop/Inspect 的既有分支和 Outbox 当前 unbound 不因本补口改变。

### 16.4 本域权限、opaque plan 与完整锁

`Authority.CheckCleanupInTx(ctx,tx,cause,object)` 只认 SkillRevision owner、ProjectDeleted reason、exact 本域 cleanup ID。由本域持久记录恢复原 lifecycle OperationID/action/version 并构造该职责的 actor，随后调用 §16.3；构造 actor 本身不是授权。必须同时核 exact owner revision/Project、Object/Upload、原 initialization provenance，以及该记录已在当前 Tx 关闭服务 gate。发表 Skill 必须存在且 serving=false；所有物理清理步骤还须保留全部 attempt 映射。§16.6 的已知物理完成记录之后才可删除历史 attempt；新 metadata 分支只依赖仍保留的原 initialization/当前 attempt/cleanup 核心映射，不据缺失旧 attempt 授技术维护。

`CheckProjectCleanupInTx` 不由 Skill provider 获得成功实现：全 Project 清理属于后序 artifact-object participant。不可变 root router 把该方法送真实 Project/Object adapter；若孤立 Skill provider 被直接用于全 Project 清理，明确 DependencyUnbound，不能拿单个 revision 的记录授权全 Project。

AccessPlanner 新增两种 exact 分支：

- CleanupReleaseAccess：包含原 cleanup cause、ObjectID、UploadID，发现原初始化 command、Project EX、真实父 Skill EX、Object EX 及 D05 返回的所有 command/object/work 锁。初次 discovery 可从初始化原映射发现拟用 CleanupID；它不是授权，不能因此作 I/O。
- ObjectCleanupAccess/CleanupObjectAccess：必须已有持久 gated/pending 记录，只匹配原 cleanup identity；同 Tx 当前门禁和不变映射重验后才交 D05。
- 新 ObjectCleanupAccess/PurgeDeletedObjectMetadataAccess：必须已有本域 completed 记录及仍完整的核心映射；不借旧 CleanupObjectAccess plan 进入。这个 operation 与 §16.6 新接口由 D05 单独正式补齐，不能先自造本地常量或强制类型转换。

服务调用 D05 `DiscoverAccess`，在自己的 `WithinTx` 内**一次** `AcquireAccessPlansInTx` 获取 D05 plan＋本域额外锁的完整排序并集；不先拿部分锁再补。完整 plan 仍使用真实 Object 实例私有 issuer 和该 live Tx 的 LockedAccess。错 Store/实例/Tx、漏锁/弱锁、mapping drift、替换 owner/object/upload/cleanup/cause 均拒绝。discovery digest 绑定全部不可变原材料，不把首次 Tx 内合法的“拟建记录→gated／serving关闭”转换误作新的授权；Validate 则检查转换后的真实当前行。两个并发首次清理只能复用唯一已提交 CleanupID，不能冲突后随意换 ID。

### 16.5 数据约束与有限扫描

00027 原样保留。cleanup 的两个 composite FK、原字段 CHECK/时间/version/Project+operation+revision 唯一约束均保留；不添加外域 FK/cascade，不复制 Object payload/key/lease/process 私有事实，不增加本域永久墓碑或新的工作 kind。

一 Project 一个不可变内置 revision，只有 initialization、skills、revision、cleanup 和当前 attempt 这组核心行有固定基数。work 每次 reader/init 可追加，历史 attempts 也不能按常数处理。最终事务前必须分批删完这两类历史；不能对整个 Project 发一次无界 DELETE，再依赖2s超时从头重试。

work 批次按 `WHERE project_id=$1 AND phase='joined' ORDER BY id LIMIT 33` 发现，至多取前32个 exact ID，在原 Project EX/Skill EX 和当前门禁下逐项重验后删除。第33项只表示 Pending；每个物理事务最多32条，取消／Unknown 不推进已确认游标。现有 work 主键仅 id，`skill_work_live_project` 又排除 joined，不能声称已有按 Project 的历史索引。该历史查询的候选索引为 `(project_id,id) WHERE phase='joined'`；但 work 另有 `(project_id,skill_id)` 指向 initializations 同列的 deferred FK，单 joined partial 不足以证明删除父行时的全部反查成本有界。root 已将 00028 移交 D05/Knowledge 统一维护共享 cleanup 索引，D05 规格 §7.1 须同时纳入这两条本域访问需求；由真实大历史 EXPLAIN/缓冲访问及升级回滚证据选择必要的最小索引，不先认定 partial 或完整 Project 前缀方案。LIMIT 不是扫描成本上界；Skills 不另写 SQL，不写或删除任何 FK。

attempt 批次按现有 `skill_attempts_original_object(object_id,attempt_id)` 的原 Object 扫前33项，排除 initialization.current_attempt_id，最多删32个匹配原 Project/Creation/Skill/Revision/Upload 的历史行。每次均保留当前 attempt 与全部核心行；不依赖 UUID 生成时间或未来 ID 单调假设。当前 Deleting＋已完成 Stop 保证本 Project 没有新 work/attempt，按持久剩余事实从头取下一批即可，不需要可丢失的内存 offset。

### 16.6 Gate、实际清理及 Unknown

本域 Cleanup 进入已有真实 service call 账本，绑定 Project 便于本地 join 核对；不启动 background goroutine。所有 Tx 和 D05 I/O 都在调用方剩余预算内，D08 单 participant 既定至多2s是上限而非新的2s续期。root Stop/Drain/Force 必须等待这次调用及其实际尾；持久 cleanup worker/instance/fence 由 D05 与 D08 自有 claim 承担，不给 Skill work 表新增假 reader/init 工作来充数。

1. 核 actor/cause/scope/checkpoint 形状，发现原映射与真实 D05 release plan。在完整 union Tx 下重验当前 CleanupPhase、原 stop 事实和本域没有未 joined 初始化/reader 工作、没有仍活的原本地 call（本次 Cleanup 不计入原工作）。Unknown 或外国死亡/原 writer 终局未证，返回 Pending/原错，不能先物理删除。
2. 同 Tx 插入／复用唯一 exact cleanup 记录，关闭 serving，并调用 `ReleaseForCleanupInTx` 撤销原 upload、删除 reserved/canonical 引用、gate 全部原 attempt。任一错误回滚本域与 D05 gate；不持 Tx 做物理 I/O。只有整个事务 **Committed** 才进入下一步。
3. 若 gate COMMIT Unknown，保留原物理 transaction attempt/cause、原 CleanupID，不取消／清理新对象，也不把无行当回滚。后继同原完整锁与当前门禁确认：真实 gated 行与关闭服务事实只能由上述原子 Tx 留下。确认已 gated 后直接继续物理阶段，**不重复 Release**。
4. 已提交 gated/pending 原项调用 `DeleteUnreferencedWithinBudget`。匹配 result.OperationID，只有 CleanupCompleted 且 Remaining.References/ActiveLeases 为空才获本次对象完成事实；Pending、原错误／Unknown、无行／ResourceDeleted 均不能单独变 Completed。D05 负责所有原 candidate 的实际 payload 清除、按正式模式核实空 marker 及 lease/work 终局；zero_marker 首版永久保留，不要求删除 marker key。空 marker、cancel、wrapper flag 或本地空 map 均不替代实际终局证明，见 [Object runtime](../backend/object-runtime.md) 与 [D05 设计](d05-object-storage-design.md) §6。
5. 物理调用实际返回后，在新 union Tx 重验 §16.3、exact cleanup/mapping 与原工作终局，将唯一 cleanup.phase 写 completed；这表示物理阶段已证，不是 Skills participant 完成。该事务明确 Committed 或在同原锁下确认 completed 后，才允许历史压缩；若仍 gated/pending，全部父映射保留，仍走原 D05 物理恢复。
6. completed 阶段按 §16.5 分批删除已 joined work 与非当前 attempt，每次最多一批32行、一次同当前 gate 的事务，然后返回 Pending。保留 initialization/current attempt/skills/revision/cleanup 核心映射。已 completed 后不再调用 Release 或 DeleteUnreferenced，也不再授 Claim/Checkpoint/FinalizeCleanup 新许可；任何未实际返回原调用、非 joined 工作、未知状态或映射矛盾均阻断。历史删除 Unknown 保留原错误／cause／attempt；下轮取原 Project EX 查真实剩余集合，已删除行不会重造，未提交行仍可被同一批安全删除，不凭请求中游标跳过。
7. 两类本域历史已收敛为 work=0、attempt=原当前一条后，调用下述新 D05 metadata 口，每次最多32条相关历史记录。D05 pending 的成功事务只表示已有部分元数据清除，仍保留其 Object/Upload/current attempt 与本域所有核心行；调用方只在该事务已提交后报告 Pending。D05 metadata 完成时，它在**同一 live Tx** 删除最后 Object/Upload/current attempt；Skills 紧接着在同 Tx 删除本域 cleanup/revision/skills/当前 attempt/initialization。任一步失败两域最后 anchor 一起回滚，永不提交“Object anchor 已没、本域核心仍在”或反向状态。
8. 最后 Tx 已知 Committed，才返回本域 Completed。D05 最后返回的值不是提前发布的授权／证明；必须等同一 Tx 的 commit 结果。当前操作最后全空后不得再调用依赖已删父映射的 D05 方法。后序 artifact-object 的 ProjectCleanup 自己仍取得正式 Project gate，但它查询不到已在本原子事务清掉的 Object，因此不再对它调用 actor-less Maintenance。

重放细节：D05 `gateAttempt` 的冲突分支保留每个旧 attempt 的原 operation/reason；当前 Release 的 revoked 重放分支要求 D05 所有 cleanup 原因一致，不能把存在旧 AbandonedAttempt 的对象当作这种单因重放。步骤2的本域 gated 行与 Release 原子提交，因此步骤3用本域当前事实确认后跳过 Release，不更改旧 D05 原因来迁就重放。部分混杂/矛盾 gate 仍拒绝，不修成“已 revoked 即成功”。

最终清空 Tx Unknown 后，必须重新取得原 Project EX 和当前 §16.3 gate，核**全部六表本 Project 都空、原本地工作实际已退役**；这个单调终态才能返回 Completed，不发新的 Object 调用。证明依赖现行 FK 保留和上述唯一最后路径：历史阶段从不删核心行；唯一删初始化/cleanup核心行的事务必已由真实 D05 口删除同一 Object anchor，且随后两域一起提交。部分历史变少是合法 Pending；部分核心缺失或尚活原 call 是矛盾/未完成，不能冒终态。若核心仍在则从 completed 原项继续 metadata 清理，不能因曾发出最后 purge 请求而推断成功。首次 Stop 缺初始化仍失败、未 initialized 不进入本分支；root 不能删 required manifest 项来逃过真实调用。

checkpoint 使用既有 provider-owned Schema=1 opaque bytes，仅包含原 Project/operation/version、exact cleanup identity与阶段等有界进度，复制与公开格式均沿现契约。错 participant/cause/scope/schema/材料拒绝；传入 checkpoint 从不替代当前行、锁、gate、物理结果或 join。只有 D08 最终 receipt 可以在 Project 永久删除后保留；本域 Completed 不声明后序 Object/Secret/Outbox/Audit 已清。

#### 16.6.1 D05 正式元数据补口（本组合已消费）

```go
type DeletedObjectMetadataPurger interface {
    PurgeDeletedObjectMetadataInTx(context.Context, foundation.Tx,
        ObjectCleanupCause, ObjectID, AccessLockPlan, LockedAccess,
    ) (ObjectMetadataPurgeResult, error)
}
type ObjectMetadataPurgeResult struct {
    State       CleanupState // Pending / Completed，指元数据清理进度
    OperationID CleanupID
    ObjectID    ObjectID
}
```

提供方是同 Store 的真实 `object.Service`，消费方是 Skills Cleanup；独立新增 interface，不要求现有 `Objects`／`ReferenceCleanup` 实现用假默认方法补齐。新 closed operation `PurgeDeletedObjectMetadataAccess` 仅在 ObjectCleanupAccess 中有效，第一实现只支持 SkillRevision＋ProjectDeleted。构造式、Validate、request binding、opaque issuer/liveTx 验证须与原 Access 协议一起补齐；不能拿普通 Owner grant、旧 CleanupObjectAccess plan、公开 Result 或 `state=deleted` 字段当调用权。未绑定时明确 DependencyUnbound，整 Cleanup 不能完成。

每次调用同时满足：

- 外层仍在原 Project EX/Skill EX/Object EX/原 command 的完整 union Tx；调用真实 Skills CleanupAuthority 核当前 Cleaning、原 owner/cause/version、completed 与仍存核心映射。D05 再核同一原 Upload/Object、Scope/owner/initiator、revoked gate。不能把已丢失 Object/Upload 当正向；最后事务 Unknown 的全空恢复只在 Skills 层按上一段完成。
- D05 私有事实证明真实物理阶段已完成：原 Object 的正式 Deleted 终态由其 finalize＋真实 Audit 事务生成，全部候选已 cleaned、清理记录已 completed、无 references/active leases；原 writer/reader/cleanup/transfer 回调与当前对象工作实际退出。它不制造死亡证明、不取消或等待新 I/O，不把 phase、lease TTL、marker 或 ctx.Err 当 actual join。缺任何证据返回 Pending/原错误且不删；仍受 Object Runtime 未闭合总限制约束。
- 单次只清最多32条 D05 历史记录，跨表按 FK 依赖合计计数，不是每表各32。复用 exact Object/Upload 的已有索引，终局 lease、旧 cleanup/attempt、transfer 等 D05 自有记录由 D05 自己判断原退休证据并推进。被依赖的 current attempt、Upload/Object anchor 必须保到末尾；任何 transfer/source/其他关联退休未证仍 Pending，不直接级联或由 Skills 读 Object 表。缺合适索引或有界查询形状要在 D05 实现中另列，00028 由 D05/Knowledge 唯一写者维护，Skills 不跨写 DDL。
- Pending 的 SQL 进度由调用者原事务提交后生效，零额外事务／goroutine／HTTP／MinIO。下一调用根据尚存原 anchor 与剩余私有事实继续，历史行不存在只表示先前清除，不能补造原因或回执。最后所有历史已空时才删除最多固定数的 anchor 并返回匹配的 Completed；Skills 的最后核心删除必须跟在同 Tx 内。metadata purge 不新增/重写 ObjectDelete Audit、原历史 cause 或 Activity/Event。

这里选择**两域最后 anchor 同 Tx 删除**，不选择长存父表、内存 handoff、缺行 allow 或后续 router 猜测。当前 `CleanupProject` 的真实控制流是 `access.go` 无 deleted 过滤地选对象，再 `cleanObject` 先取 Maintenance plan、最后才看 completed；它证明 rev1 的“先删 Skills 六表、以后让 Object purge”不可行。本组合已验证该补口的有限消费；完整生产 participant 仍须根装配及生命周期门槛。

#### 16.6.2 D05 原清理预算的必要条件

`upload.go` 只限制尚未 published/cleaned 的活候选数量，已 cleaned 历史不因此有常数上界。rev2 制定前 `gateObject/stopWriters/cleanObject` 的 `attemptIDs` 都会遍历全部历史；`DeleteUnreferenced` 在 cleanObject 前必调 stopWriters，后者还逐项 loadAttempt 做原 writer 终局重验，所以只优化 gate/claim 仍会每次卡在同一无界前缀。不能凭2s ctx 或一个小 fixture 宣称任意规模都有进度。D05 独立实现必须同时处理 gate、stopWriters 发现与终局重验、claim/checkpoint/finalize 的候选查询和最后未完成检测：以原记录状态区分已证完成历史和未终局候选，每批最多32项，按精确 Object/Upload 选择可索引范围；所需索引与查询计划由D05实现核明，不能在最终检测中又全量加载旧历史。未完成项保原 operation/reason/worker/fence，真实 writer 必须继续等待其实际 done 或消费正式 ProcessGuard死亡／原终局证明，不能因不在本批、已 cancel 或无本地map而跳过。物理与checkpoint每次实际返回后才持久推进；已完成历史可为元数据阶段保留，无须先重新进入Skills Maintenance。

此为 Object Service／SQL 调度范围，不是放松 actual join、扩大2s或恢复 Runtime。Release 的原子关闭 upload/引用与全对象新写 gate不能分散成中途可复活状态；如有界标记需要进一步正式状态/口，D05 作者须先提出具体契约，不能把部分 gate当完成。上游有界 gate/恢复及新 metadata 口均获独审并有大历史、失败/Unknown证据前，只能实现/验证独立本域边界，不把完整 Cleanup 标完成。

### 16.7 清理维护与 ObjectDelete Audit

本域 Maintenance provider 的新增允许表必须显式闭合：

| operation | 本域核对 | 仍由 D05 证明 |
| --- | --- | --- |
| ClaimCleanupAccess | 原 Object＋旧或当前 exact Attempt 必须存在 object_attempts，关联原 Project/Creation/Skill/Revision/Upload；原 cleanup gate 已有，完整父锁纳入 plan | 实例匹配、真实 candidate 的 cleanup_gate、引用/活 lease、原 claim/worker/fence |
| CheckpointCleanupAccess | 同上；请求的 InstanceID/AttemptID/CleanupID/WorkerID/Fence 全部进入不变 request/dependency binding，不把原 candidate ProcessID当当前 worker | 私有当前 cleanup 调用证据、实际返回、原 worker/fence 与 checkpoint终局；不能凭公开字段构造成功 |
| FinalizeCleanupAccess | exact 原 Object 及本域 gate/原 mapping；不要求本域或 Object deleted 后态已经写出 | 所有 candidate cleaned、引用与活 lease 为空，原清理原因与原 Tx/witness |

这三个分支只在本域 gated/pending 物理阶段提供父锁/映射和原 gate；不调用 active 初始化写授权，也不扩成所有 Maintenance 通配。写 completed 后先前物理调用已经实际返回，不再新授这些依赖历史 attempt 的分支；metadata purge 有单独新 operation，只有核心映射依赖。RecoverAttemptAccess／其他未列 operation 仍 unbound。已有 FinishWriter/JoinAttempt/ReleaseReader/ReleaseProcess 技术退役语义不改；所需 work/attempt 被收缩前必须证实实际退役，而不是删除证明本身来声称退役。原 Object Runtime join 停项不在本节解决。

新增本域 `LifecycleAuditAuthority` 组合为 `audit.ProjectAuthority` 的外层，构造接既有 Project 初始化/普通授权 delegate、本域 Authority 与真实同 Store Object ProjectFactAuthority。只拦截当前 Skills cleanup exact Object 的 `ObjectDelete`，其它条目完整原样委托一次。它不修改 D04 actions/schema，不自己 append：

- 同原 context/Tx、Project EX/Skill EX/Object EX及完整 command locks，当前 §16.3 gate、serving已关闭、原 cleanup/object/upload/Creation 和 retained attempt 映射均真实；同 scope/actor=ObjectService、resource=原 Object、空 associations、Outcome=Success、phase=Deleted、reason空、ordinal=1、完整 media/size 与原包事实一致。
- metadata.InitiatorKind=Service／InitiatorID仍为**原 CreationID**，不是 lifecycle OperationID。AppendKey.CauseRef 是 D05 的删除 digest；D05 从最早 cleanup operation 派生，存在旧 AbandonedAttempt 时可能不同于本域新 CleanupID，不能在 Skills 猜一个摘要替代。
- 原 Entry/key/context 原样交 `object.ProjectAuditAuthority.CheckProjectAuditInTx`，由真实私有 witness证明实际 native调用点、同 Store/Tx和原 digest/attempt/lease前置。公开字段完全相同但没有 witness仍拒；Audit在 objects.state=deleted UPDATE前发生，不能要求不存在的后态。
- 本最小清理路径不生成 UploadComplete，也不新增删除期间 UploadFailed 的绕过；后者仍沿原初始化 wrapper及其真实gate，若后续确有合法清理失败事实需要另补精确分支，先报告root。普通active初始化Audit与其它域 routes 保持原样；不增加Activity或业务Event。

### 16.8 精确写域、组合责任与验收

本域实现范围为 `internal/central/skill/{service,object_authority,object_maintenance,audit_authority}.go` 及新 `cleanup*.go/lifecycle_cleanup*.go` 与相应同包测试、`tests/skills`、本文/主卡/current。00028 已由 root 移交 D05/Knowledge 作为共享 cleanup 索引迁移，须纳 §16.5 历史 work 与原 FK 反查候选并以真实计划取舍；本域写域不含 SQL。不回写00027，不重新解释旧 frozen binary/StopPG PASS。

共享需求分两项，均由 root 指定独立写者，不由 Skills 越权实现：

- Project：`internal/central/project/lifecycle_authority.go` 和相邻测试提供 §16.3 Skills CleanupPhase，原 Project contract 不加通用 allow/新 phase。
- Object：新 `internal/central/object/contract/metadata_cleanup.go`、对应 Service 实现及测试，`contract/access.go` 的唯一新 closed operation 与 `access.go` 的真实 opaque plan 绑定；`cleanup.go` 的既有预算内清理需要 §16.6.2 的有限推进核验。具体是否要内部索引由 D05 作者根据精确 SQL/EXPLAIN 另报 root，不能扩为 Runtime join/其他 owner 删除接口或由 Skills 修改 D05 表。上游方法的物理完成证明、每批上限、最后 anchor 原子性均须独审，非仅 compile 满足接口。

同 Store/Tx/锁的真实组合必须实际验证；本组合已验证新 Object 口的有限清理链路，但不能据此声称完整生产 participant。根的 immutable owner/cleanup/planner/Audit routes、完整 `agent-skills-variables` 组合、manifest能力版本、initializer与participant同时绑定，以及 DB最后／guard实际join是随后生产接入责任，不能替代上述具体正式端口。D04／App没有本节作者写权。

最小验收范围：

1. 原00027两个cleanup FK继续拒绝错tuple/无Skill；当前已发表＋旧未发布candidate正向不需要dropFK。仅当确认加入 §16.5 索引，另验fresh/upgrade/回滚及含其它Project的大历史执行计划；不增加未初始化Project删除的schema或业务正例。
2. CleanupPhase真实PG：当前Cleaning/Owner/cause/version/manifest/依赖成功；停止未齐、wrong participant/contract version、failed尚未Retry、audit/final屏障、foreignStore/endedTx/漏Project锁与未initialized全拒，既有Stop回归不扩大范围。
3. 本域opaque plan与原子gate：同实例/liveTx/完整锁；fake/mutated plan、same-public-fields／不同issuer、mapping drift；Release失败本域与Object一起回滚；COMMIT Unknown在原锁终局前不发物理调用，确认gated后不重复Release。
4. 真D05组合：当前published＋旧未发布candidate/旧AbandonedAttempt cause、reserved/canonical关闭、原upload回执不能复活、多个原因保持；同cause重试一次ObjectDelete Audit，fake witness拒，真实Audit失败回滚Object终态；真实writer/reader/lease/marker未终局不得Completed，实际Close／死亡原writer终局后才推进。未解决Object全域Runtime停项不因此标PASS。
5. 超过两批的实际 joined work／历史 attempts 必须按32上限留下已提交进度；中途重建Service、取消、原COMMIT Unknown、低/高ID均不遗漏，不靠延长2s。gated时不得删任何必要映射，completed后合法历史变少为Pending；缺核心、未joined、迟到旧Creation/operation仍拒，另一Project不变。证据须区分来源：真实 API 已可生成65次 reader/work；当前单 Project 只有一个初始化命令，D05 限制每命令未 cleaned attempt 最多2，而本域 RecoverAttemptAccess 仍正式 unbound，因此不为制造65次 native 重试扩权限。先以真实失败验证／同 key 重试生成旧 AbandonedAttempt＋当前 published 两 attempt 验证第4项；本域超过两批的保留映射使用明确历史 SQL fixture，仅在真实 physical completed 后新增本域 object_attempts、核所有现有 FK/CHECK 和每批32及回滚／最终事务。不称这些65行由当前初始化 API 产生，不制造 D05 cleanup/join/private witness，也不替代 D05 自身旧 attempt／成本实测。
6. 新 D05 metadata 精确opaque plan／假result／错cause／活lease或原回调未返均不能purge；两个域最后anchor在同Tx失败／真实Unknown时一起保留或一起消失，最终Skills全空后后序真实CleanupProject能完成且不再调用旧Skills Maintenance。大量D05终局历史须在gate/stopWriters/clean/final未完成检测全链保持有限进度，并把尚活writer放在历史后部验证不漏，原ObjectDelete仅一次、无额外Activity/Event、原System Audit不动；不以无行、field-shaped proof或fake provider获得该正向。

独立者审SPEC后可分别实施本域provider/受控事务边界与 root 分派的两个上游补口，正式接口齐后再做当前门禁PG和真实D05组合；真实资源需各自freshgrant。可构建、pure、受控PG、真实D05和完整root分别报告，任何一个层次都不替代其他层次。
