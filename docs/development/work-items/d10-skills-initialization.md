# D10：Skills 初始化与不可变内容

状态：rev3，2026-10-09，P2 持久初始化服务 SPEC 已获未参与实现者有限独立接受，持续实施。P1 纯契约与真实 builtin 的既有提交及[独立验收](../agent-team/d10-p1-recovery-verification.md)不变。当前基线正式 main `ca9f2d5d` 已包含 D08 初始化 Audit 授权库；P2 受控 Object 边界下的真实 PG 持久初始化、发布回滚与原 COMMIT 恢复三个作者 top 已通过。后增Project精确Stop已获有限独审，当前产品真实PG12子完整PASS；D05组合仍只完成编译准备。完整服务验收、D05真实发布/清理和生产root仍未闭合。本文不代表 D10 完成。

依据：[开发计划](../development-plan.md)、[Skills 架构](../../architecture/agent-skills.md)、[D01 资源/Skills 契约](d01-contracts/resources-skills.md)、[本工作项规格](d10-skills-initialization-design.md)。S01 候选基线 `71dc17671631632bb26e251ad8491e74092ac975`，原主卡 SHA `a258ed11366946529082e885b5ec1e74d033687d1aec60d69000691862811b88`；独立结论 `/tmp/agenteam-d10-s01-review-4r1gg40i/report.md` SHA `49381427440c1f2a09219361e8a1b902ecb8c0db1d1700a35350050f6136f990` 无新增硬阻断，只采纳规格，不证明真实链路。

## 已交付 P1

固定实现基线 `f401c15a5187690889eaea9ba9672ca8bdc85460`。新11路径：`internal/central/skill/contract/{types,package,read}.go` 及3对应测试，`internal/central/skill/{builtin,package}.go` 及2对应测试，`internal/central/skill/builtin/add-skills/v1/SKILL.md`。另仅本文及配套规格归位，共13路径；不改旧域、go.mod/go.sum、迁移、fixture、app或共享授权口。

P1交付真实非空 Add Skills 文本、确定性 ZIP v1、不可变 manifest、严格路径/UTF-8/碰撞/资源限制、只读载体与实际本地 reader Close/Read 记账。只返回准备材料，绝不生成已初始化Project、已发布Skill或成功Object的生产结果。`OwnerReader` 是后段端口；没有默认成功实现。规范化使用锁文件已存在 `golang.org/x/text v0.41.0`，不新增依赖。

## 当前 P2 授权与真实前置

本轮在独立 Skills 树实施完整持久初始化服务：新 Skill 域实现/测试、迁移 `00027_skills.sql`、本文/配套规格、后端 README必要事实与本树恢复点。不更改已交 P1编码规则，不新建外部安装/HTTP/UI入口；共享 D05/D08/Audit/root源须由root协调唯一写权。[当前详细规格](d10-skills-initialization-design.md#10-当前构造口与职责)覆盖数据、原四口、OwnerReader、权限/Tx/Unknown、真实Object发布清理与停止账本。

- **已存在且可复用**：D05 `object.NewProjectAuditAuthority(Store)` 私有同Tx witness checker；D08 `InitializationConvergenceAuthority` 和 `NewInitializationAuditAuthority`。旧“checker/constructor不存在”的历史前提已过时，不另造重复实现。
- **精确补口已完成并获有限独审**：D05 初始化 Service/SkillRevision/Creation cause闭集、同分支Service initiator UUID、SkillRevision+ProjectDeleted不可逆release，保留原Avatar和Knowledge变体。该证据仍不证明真实D05发布或清理。
- **实现与绑定边界**：真实 Skill exact key/object/attempt provider及本域服务已实现，真实Object组合源码已接线并编译；完整 participant/生产root仍未绑定。D08初始化Audit wrapper是active-only，不能用它放行删除。
- **真实清理前置**：当前Project lifecycle CleanupPhase明确DependencyUnbound；需正式同cause清理授权和本域删除Audit组合，不跨域私表、不临时allow。Object runtime join停止项保持，未齐前不得运行或宣称完整root停止/永久删除组合。
- 00024/25/26前缀已由root按精确来源集成，00027已随三次真实初始化fixture连续执行；独立升级/约束/DDL回滚矩阵仍待。未改旧迁移/共享Audit CHECK。真实PG+明确受控delegates只验证本域；真实D05/MinIO/witness及生产绑定须各自实际验收。

P2 首片段已新增Store/原命令与冻结包状态校验、对应pure测试、00027 DDL草案；作者 `TestInitialization` 3top/9子实际通过，仅本域纯状态，尚无服务/PG/真实Object/网络结果。首次编译因误用不存在的Object NormalizeLocks setupFAIL已修正为已知四锁有序集合，原失败保留在恢复点。[验收分层](d10-skills-initialization-design.md#15-当前实现与验收分层)保留完整结果门槛；不得以空Skill、构造completed或仅PUT成功代替。

## P2 当前可恢复阶段

- rev3 SPEC 已获未参与实现的 Variables 作者有限独审接受。首 Store/命令/冻结包状态与3top/9子作者pure通过，只证明本域局部状态，不是初始化服务成功。
- D05三个精确补口已按root授权落盘并获未参与者有限独审接受：初始化Service形状/Creation initiator、SkillRevision+ProjectDeleted release；原Runtime和Release实现未改。作者相关pure、Object contract完整race、原reservation函数定向race均actual0；独立实际overlay2top/6子通过；真实Object/PG和生产装配未跑。
- 本域 repository/Authority 与有界 service owner 已形成可构建片段，作者累计7top离线race通过；观察/发现确认/同Tx确认三口新增2top共20子有效，覆盖当前门禁、原Unknown、私有issuer、活Tx/完整锁、发布关系损坏与serving关闭。首轮门禁Fault身份失败已修复并保留恢复记录。这些是受控端口证据；后续Initialize写入已形成片段，OwnerReader及恢复仍在实施，尚未PG。
- 初始化写入四口已编译闭合：原计划/已知Reserve commit后才physical、同Tx Object canonical发布和Skill/Revision/checkpoint，作者1top/11子race actual0。Plan/Reserve/Publish Unknown、撤权、错误对象/临时receipt和Discard错误保留；这些由受控SQL/Object端口验证流程，尚不证明真实授权、PG、工作账本/恢复或生产绑定。
- 精确Object/Audit授权作者定向race通过（2top/15子与1top/13子），包含真实D05 checker缺私有witness拒例；正向delegate仍受控。Owner元数据读1top/10子通过，OpenPackage随后已接入，作者3top/10子race通过；真实读流仍依赖D05且未做对象网络验收。持久work在physical前登记原process/父关系，实际Discard和调用返回后才结账；取消、Unknown和未结尾不提前Joined。作者集成5top/23子race actual0，跨进程恢复/真实PG仍未验；初次构建及fixture错误保留恢复点。
- Object技术尾维护新增本域精确映射和同Store锁内重验，作者1top/14子race通过；D05仍须验证自身instance及私有实际return/lease/process证明，不产生新读写或不可逆清理许可。新physical恢复/清理尚未接入，原Owner操作兼容2top/15子仍通过。
- 恢复新增持久work pass、100项有界轮转和精确进程终局/原锁/当前convergence gate，只结束原记账，不重开初始化发布。作者3top/14子race通过（含重建Service后仍可越过100忙项），完整skill/... race及vet均actual0。恢复首次错误构造名setupFAIL保留；PG、对象physical恢复/生命周期和root绑定仍未验/未闭合。
- 00024已由root刷新到正式Variables来源，00025/26仍与各自稳定来源逐字一致，各域前序SQL验收事实可复用；本树00027尚未PG。新增真实PG验收源码覆盖四口持久发布/重建服务重读确认及发布失败回滚，race-c与两top精确发现通过；Project/Creation为披露的规范测试事实、Object为受控端口，本域Skill和Project权限/锁/事务实现真实消费。尚未运行PG、D05/private witness正例或生产root。原COMMIT恢复将复用已正式完整帧代理，不以受控CommitResult代实际提交证据。
- 原 COMMIT 恢复第三 top 已形成可构建源码：复用正式完整帧代理，按实际发布事务 PID hold 原 COMMIT、Unknown 返回后释放并观察原提交与实际 join，再由原 key 和重建 Service 重读/重放。16497 race-c、858c2c 精确发现实际通过，尚未执行 PG/代理网络；没有把编译或控制端口写成真实提交结果。当前三个 top 每个都调用连续迁移，独立升级/DDL 失败回滚矩阵仍须另行实际覆盖。
- 单独 `TestSkillMigration` 已实现 fresh/repeat、保旧 Account/Audit 事实与原约束的 00026→27 升级、六表合法图及 30 个 CHECK/FK 拒例、整 schema 故障回滚和原 checksum 恢复；35384 race-c、a4373e 精确发现 actual0。独立迁移 binary 与原初始化 binary 分开，尚未运行 SQL，不将前序各域验收或编译充作 00027 的真实结果。
- 首个作者真实 PG top `TestSkillInitializationPersistence` 已完整 PASS（60950，Go 2.51s、outer 72.659s）：本域持久发布、重建 Service 后原 ID 重读/同命令重放、同 Tx 确认，以及缺锁/ended Tx/外来私有 issuer 拒绝。Go/driver 实际 Wait0、两资源双清、runtime/私有文件、desc/TCP 双尾与输入不变全部闭合，root 已有限接受。真实 Project Authority 与受控 Object 的分界保持；不证明 Project.Create/Human、D05 私有 witness/MinIO、生产 root 或独立验收。该轮连续迁移已实际执行，另外三个 top（发布回滚、COMMIT 恢复、独立迁移矩阵）仍未动态。
- `TestSkillInitializationAdmissionUnknown` 另补 work 登记／Reserve 原 COMMIT 未确认时不得开始 physical 的两场景：使用真实 Store/Tx/原结果和正式完整帧代理，只有测试观察转发器，不替换 CommitResult；释放后原可见事实／只读 Pending／实际 work 结账与原 Unknown provenance 分开验证。97930 race-c、c293f4 精确发现 actual0，未运行 PG；受控 Object 边界不变，先前已冻结测试与产品没有修改。
- 新 `TestSkillOwnerMetadataCurrentAuthority` 准备真实 PG 的 List/Get 当前权限矩阵：真实 Account.Initialize 注册测试 keyring，既有 Account/Project Authority 消费测试 User/Session；发布后未初始化 Project、跨 Owner/admin、Session 错配/缺失/撤销/过期、未知 Skill/Project、归档可读与删除拒绝共 12 子项，正向结果比对持久元数据且每次零 Object 操作。98760 race-c 与 33518 精确发现 actual0；初次编译因自有 GOTMPDIR 缺失未启动，补目录后构建。未执行 PG；Project/Creation completed 和生命周期状态是明示的规范测试前置，不是 Login、Project.Create、归档/删除命令或 D05 读流验收。只有新测试源，产品/原 fixture/既有 binaries 均未改。
- 第二个作者真实 PG top `TestSkillInitializationPublicationRollback` 完整 PASS（58518，Go 1.86s、outer 69.153s）：受控 Object 发布拒绝后，真实 Skill/Revision 写入原子回滚、原 reserved attempt 保留、只读 Inspect 不续发且不能获得完成确认。Go/driver 实际 Wait0、两资源双清、私有目录、desc/TCP 双尾与 inputs_unchanged 均闭合后已释放窗口；不把受控发布失败外推成真实 D05 失败注入。COMMIT Recovery、Migration、Admission Unknown、OwnerReader 仍未实际运行。
- 真实对象组合另形成 `TestSkillObjectInitializationPublication` 三子源码：同一 Store 的 Skill exact facts → D08 初始化授权 → 真实 Object 私有 witness/Audit，独立 object.Service.Initialize 后发布并重放，真实包 EOF/Close 与 reader lease/work 记账，精确公开字段不能伪造私有 witness。首 69925 编译拒绝把尚未实现的 Skill CleanupAuthority 接入；移除该错误 fixture 接线后 8884 race-c、2c8c10 精确发现 actual0。Cleanup 口明确 unbound、ProcessGuard 只构造、不装 Object Runtime，不声称旧进程停止/删除/root可用。新增两既有 harness 各一条精确单 top 映射，原 6m/七资源/Wait/预算不变；作者 d47937 控制与未参与者 685fef 窄审通过。尚未执行 MinIO/PG，不将编译、映射独审或此前受控 Object 结果充作真实对象通过。
- 本域Project精确Stop子能力已实现并获未参与者有限独审接受：原初始化/包读取从准入关联稳定work ID，RequestStop同Store完整锁下消费现有StopPhase门禁、明确commit后才取消原call；Archive保合法reader，Delete包含reader。Inspect只按本实例实际返回或foreign精确停止+原Tx锁终局结账，不把取消/Close/空本地map当Stopped；原Unknown与100项之外Pending保持。最终4top/21子作者定向race、完整 `skill/...` race与vet通过；Knowledge作者对 `d0a16242` 六源独立overlay62067实际4top/2子race通过，覆盖锁失败、call未end、proof后identity漂移和原Unknown。全部仍用受控Store/Project/Process，不是PG/生命周期/根停止验收。原五个integration binaries和来源产品checkpoint `5291515f` 保留，新产品须另验。
- 第三个原产品作者真实PG top `TestSkillInitializationCommitRecovery` 完整PASS（39205，Go3.88s、outer73.143s）：真实原COMMIT完整帧hold、原Unknown、释放后提交及proxy实际join，原key只读恢复与重建Service同ID零physical重放。Go/driver实际Wait0、两资源双退役、private/runtime及desc/TCP双尾、inputs_unchanged全闭合后释放窗口。此证据消费旧 `skill-pg-recovery.test`/`5291515f` 前产品，Object仍受控，不外推后增Stop、真实D05或root。
- 当前Stop另有独立 `TestSkillLifecycleStopPersistence` 12子源码准备，消费真实Project LifecycleAuthority/同Store锁/真实work SQL，并以原完整帧proxy验证Stop COMMIT未确认不得取消。覆盖当前phase/cause/participant/Owner拒例、archive/delete原held Discard的实际join和独立真实父锁竞争；Project/Creation/lifecycle明示规范种子，Object/Process受控。70036 race-c与f8911f唯一top发现actual0，独立 `skill-pg-stop.test` 使用 `d0a16242` 产品+新测试；尚未执行PG/socket，旧binaries及harness未改。

- 当前产品Stop作者真实PG `96753` 完整PASS：沿70036 binary／d0a16242产品＋19353f4e测试，12子Go5.32s；同env首采5,707,370,496 bytes满足5GiB后exec，Go986629与driver986046实际Wait0，driver15.375s／supervisor74.585s、outeractual0。两精确PG资源双退役、desc/runtime/private及TCP双尾、inputs_unchanged全齐，现场run仅owned.json且两PID不存在后归还窗口。实证当前Project LifecycleAuthority/原cause与phase/participant/Owner、真实父锁、archive/delete持有调用实际join、原Stop COMMIT Unknown不取消及后继已知提交取消；仍为规范Project/lifecycle种子和受控Object/Process，不称D05/foreign死亡/整participant/root通过。原件与完整env/cwd见 `.agent-state/current.md`；原初始化三组旧产品结论不扩大。

## 生命周期后续依赖与责任

上述Stop两口不构成完整`ProjectLifecycleParticipant`。本域下一段负责精确CleanupAuthority、同cause关闭serving与Release、预算内物理删除、删除Audit外层及所有实际terminal后的本域清理；必须消费真实D08 CleanupPhase准入，不能拿active初始化授权或技术退休权替代。

当前00027的cleanup表通过FK要求已有skills行，无法持久表达尚未发表的reserved attempt清理。已执行迁移不重写；后续由root协调新的全局迁移编号及兼容升级。本轮只记录该实现前置，没有修改DDL或Cleanup口。

root负责D08当前Cleaning/participant/原Owner/operation/action/version的真实清理授权，以及把实际启用的Variables、Skills等域组合为同一`agent-skills-variables` participant并固定manifest/依赖顺序；生产initializer与participant同时接入，Object共享guard保持至实际join及DB最后。Object Runtime停止项和完整生产组合未验事实继续保留。

## 验收与当前证据

以下保留原作者阶段的历史记录，原 `/tmp` 输入及日志本轮未恢复，不作为本轮重新验收的执行证据：P1 在固定隔离基线+32旧编译文件上完成 `skill/...` unit、race、vet、build，均exit0。最终13顶层/16子例：unit包0.008s/0.104s，race1.026s/1.272s；覆盖路径/ZIP攻击边界和读流实际join。首次12顶层unit同样通过，随后自查补ZIP解析前真实中央目录上限并重验。精确命令/原日志路径为 `/tmp/agenteam-d10-pure-1nuilmr2/validation.json`，当时分别保留原输入两版，并要求独立验收后才由主线程精确提交。原作者记录没有产品测试失败、没有fixture运行，最初Go技能路径定位失败已改读实际 `agenteam-go-development`，不计测试证据。

后段验收见配套规格；上述 P1 历史阶段未运行 PG/DDL/MinIO 或绑定 D08。当前 P2 的有限 PG 结果以上节为准，仍无生产 root、MinIO、UI/Tool/Agent/Runner 完成声明。原草案SQL路径为 `/tmp/agenteam-d10-s01-skills-uve1ji1r/skills-schema.draft.sql`（SHA `4b473f8785c8a15ca4a7b7e6a8f4b9d33569fd6f47bffe9a10f05fd904854dfc`），本轮未恢复；当时没有迁移号或仓库迁移文件；当前00027已随初始化 fixture 连续执行，独立迁移验收仍待。

### P1 本轮独立验收

`skill_verification` 未参与该实现，对固定 `8872110` 的 P1 独立验收通过，未发现阻断缺陷。[正式报告](../agent-team/d10-p1-recovery-verification.md) SHA256 `e0fff9717e5ace0ec3c300206073d29312fc823a2d90b39cd779803e1fc40960`；[持久结果与命令](../agent-team/evidence/d10-p1-recovery/results.json)记录 Go1.27.1 `skill/...` unit/race各13顶层16子例、vet及7项独立race探针全部exit0，编译仓库源与固定输入匹配。首次锁定x/text缓存缺失的setup失败原样保留，独立缓存恢复后通过，未改依赖锁。

本轮仅确认纯包与载体，不改变上述后段门槛；`OwnerReader`仍未绑定，`Joined`不证明D05 lease持久释放。验收者已停止写入及命令，报告与证据交主线程提交；P1实现本身已在`8872110`提交，rev2仅同步状态。
