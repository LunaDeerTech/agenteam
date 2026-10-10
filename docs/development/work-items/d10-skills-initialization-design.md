# D10 Skills 初始化与内容规格

状态：rev3，2026-10-10。P1 固定编码与纯口不变；P2 初始化／持久不可变内容／当前 Owner／精确 Stop 和 00027 有限库按 §§10–15 实现并接受，实际证据见[主卡](d10-skills-initialization.md)。§§1–8 保留完整结果的目标和后段验收边界，不把尚未实现的 Cleanup、生产 root 或 Runtime 当作本次交付。

历史 S01 `/tmp` 草案不是当前 DDL 来源；00027 为本次正式库模型。Object 原生 Audit checker、Project 初始化／收敛 wrapper 及有限 Skills CleanupPhase 已在 main，本 P2 消费初始化/读/Stop 子口；真实清理与完整多域 participant 另行闭合。

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
| 清理 | Release 的 SkillRevision+ProjectDeleted closed shape 本次加入，保 Avatar/Knowledge。main 已有有限 Skills CleanupPhase，但本 P2 尚无 CleanupAuthority／生命周期 Audit／完整删除实现；schema和形状成功不代表清理可执行。 |
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
