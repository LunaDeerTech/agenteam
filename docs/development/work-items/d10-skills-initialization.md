# D10：Skills 初始化与不可变内容

状态：rev3，2026-10-09，P2 持久初始化服务 SPEC 已获未参与实现者有限独立接受，开始实施。P1 纯契约与真实 builtin 的既有提交及[独立验收](../agent-team/d10-p1-recovery-verification.md)不变。当前基线正式 main `ca9f2d5d` 已包含 D08 初始化 Audit 授权库；P2 服务、PG、D05真实发布/清理和生产root仍未实现验收。本文不代表 D10 完成。

依据：[开发计划](../development-plan.md)、[Skills 架构](../../architecture/agent-skills.md)、[D01 资源/Skills 契约](d01-contracts/resources-skills.md)、[本工作项规格](d10-skills-initialization-design.md)。S01 候选基线 `71dc17671631632bb26e251ad8491e74092ac975`，原主卡 SHA `a258ed11366946529082e885b5ec1e74d033687d1aec60d69000691862811b88`；独立结论 `/tmp/agenteam-d10-s01-review-4r1gg40i/report.md` SHA `49381427440c1f2a09219361e8a1b902ecb8c0db1d1700a35350050f6136f990` 无新增硬阻断，只采纳规格，不证明真实链路。

## 已交付 P1

固定实现基线 `f401c15a5187690889eaea9ba9672ca8bdc85460`。新11路径：`internal/central/skill/contract/{types,package,read}.go` 及3对应测试，`internal/central/skill/{builtin,package}.go` 及2对应测试，`internal/central/skill/builtin/add-skills/v1/SKILL.md`。另仅本文及配套规格归位，共13路径；不改旧域、go.mod/go.sum、迁移、fixture、app或共享授权口。

P1交付真实非空 Add Skills 文本、确定性 ZIP v1、不可变 manifest、严格路径/UTF-8/碰撞/资源限制、只读载体与实际本地 reader Close/Read 记账。只返回准备材料，绝不生成已初始化Project、已发布Skill或成功Object的生产结果。`OwnerReader` 是后段端口；没有默认成功实现。规范化使用锁文件已存在 `golang.org/x/text v0.41.0`，不新增依赖。

## 当前 P2 授权与真实前置

本轮在独立 Skills 树实施完整持久初始化服务：新 Skill 域实现/测试、迁移 `00027_skills.sql`、本文/配套规格、后端 README必要事实与本树恢复点。不更改已交 P1编码规则，不新建外部安装/HTTP/UI入口；共享 D05/D08/Audit/root源须由root协调唯一写权。[当前详细规格](d10-skills-initialization-design.md#10-当前构造口与职责)覆盖数据、原四口、OwnerReader、权限/Tx/Unknown、真实Object发布清理与停止账本。

- **已存在且可复用**：D05 `object.NewProjectAuditAuthority(Store)` 私有同Tx witness checker；D08 `InitializationConvergenceAuthority` 和 `NewInitializationAuditAuthority`。旧“checker/constructor不存在”的历史前提已过时，不另造重复实现。
- **仍待精确补口**：D05 初始化 Service/SkillRevision/Creation cause闭集、同分支Service initiator UUID、SkillRevision+ProjectDeleted不可逆release。原Avatar和Knowledge变体必须保留。
- **仍未绑定**：真实 Skill exact key/object/attempt provider、本域服务、完整 participant/root组合。D08初始化Audit wrapper是active-only，不能用它放行删除。
- **真实清理前置**：当前Project lifecycle CleanupPhase明确DependencyUnbound；需正式同cause清理授权和本域删除Audit组合，不跨域私表、不临时allow。Object runtime join停止项保持，未齐前不得运行或宣称完整root停止/永久删除组合。
- 00024/25/26前缀由root精确集成，才执行00027真实迁移；不改旧迁移/共享Audit CHECK。新库可以先实现并以真实PG+明确受控delegates验本域，但真实D05/MinIO/witness及生产绑定须各自实际验收。

P2 首片段已新增Store/原命令与冻结包状态校验、对应pure测试、00027 DDL草案；作者 `TestInitialization` 3top/9子实际通过，仅本域纯状态，尚无服务/PG/真实Object/网络结果。首次编译因误用不存在的Object NormalizeLocks setupFAIL已修正为已知四锁有序集合，原失败保留在恢复点。[验收分层](d10-skills-initialization-design.md#15-当前实现与验收分层)保留完整结果门槛；不得以空Skill、构造completed或仅PUT成功代替。

## P2 当前可恢复阶段

- rev3 SPEC 已获未参与实现的 Variables 作者有限独审接受。首 Store/命令/冻结包状态与3top/9子作者pure通过，只证明本域局部状态，不是初始化服务成功。
- D05三个精确补口已按root授权落盘并获未参与者有限独审接受：初始化Service形状/Creation initiator、SkillRevision+ProjectDeleted release；原Runtime和Release实现未改。作者相关pure、Object contract完整race、原reservation函数定向race均actual0；独立实际overlay2top/6子通过；真实Object/PG和生产装配未跑。
- 本域 repository/Authority 与有界 service owner 已形成可构建片段，作者累计7top离线race通过；观察/发现确认/同Tx确认三口新增2top共20子有效，覆盖当前门禁、原Unknown、私有issuer、活Tx/完整锁、发布关系损坏与serving关闭。首轮门禁Fault身份失败已修复并保留恢复记录。这些是受控端口证据；后续Initialize写入已形成片段，OwnerReader及恢复仍在实施，尚未PG。
- 初始化写入四口已编译闭合：原计划/已知Reserve commit后才physical、同Tx Object canonical发布和Skill/Revision/checkpoint，作者1top/11子race actual0。Plan/Reserve/Publish Unknown、撤权、错误对象/临时receipt和Discard错误保留；这些由受控SQL/Object端口验证流程，尚不证明真实授权、PG、工作账本/恢复或生产绑定。
- 精确Object/Audit授权作者定向race通过（2top/15子与1top/13子），包含真实D05 checker缺私有witness拒例；正向delegate仍受控。Owner元数据读1top/10子通过，OpenPackage随后已接入，作者3top/10子race通过；真实读流仍依赖D05且未做对象网络验收。持久work在physical前登记原process/父关系，实际Discard和调用返回后才结账；取消、Unknown和未结尾不提前Joined。作者集成5top/23子race actual0，跨进程恢复/真实PG仍未验；初次构建及fixture错误保留恢复点。
- 00024/25/26已由root按各域冻结来源导入本树，00027草案尚未PG；前序26没有本轮真实迁移通过结论。继续实现真实本域服务/四口与读流，不扩大P1或上述pure的结论。

## 验收与当前证据

以下保留原作者阶段的历史记录，原 `/tmp` 输入及日志本轮未恢复，不作为本轮重新验收的执行证据：P1 在固定隔离基线+32旧编译文件上完成 `skill/...` unit、race、vet、build，均exit0。最终13顶层/16子例：unit包0.008s/0.104s，race1.026s/1.272s；覆盖路径/ZIP攻击边界和读流实际join。首次12顶层unit同样通过，随后自查补ZIP解析前真实中央目录上限并重验。精确命令/原日志路径为 `/tmp/agenteam-d10-pure-1nuilmr2/validation.json`，当时分别保留原输入两版，并要求独立验收后才由主线程精确提交。原作者记录没有产品测试失败、没有fixture运行，最初Go技能路径定位失败已改读实际 `agenteam-go-development`，不计测试证据。

后段验收见配套规格；未运行PG/DDL/MinIO/网络产品路径，未启服务、未绑定D08，无UI/Tool/Agent/Runner完成声明。原草案SQL路径为 `/tmp/agenteam-d10-s01-skills-uve1ji1r/skills-schema.draft.sql`（SHA `4b473f8785c8a15ca4a7b7e6a8f4b9d33569fd6f47bffe9a10f05fd904854dfc`），本轮未恢复；当时没有迁移号或仓库迁移文件；当前P2已落00027草案，尚未执行或数据库验收。

### P1 本轮独立验收

`skill_verification` 未参与该实现，对固定 `8872110` 的 P1 独立验收通过，未发现阻断缺陷。[正式报告](../agent-team/d10-p1-recovery-verification.md) SHA256 `e0fff9717e5ace0ec3c300206073d29312fc823a2d90b39cd779803e1fc40960`；[持久结果与命令](../agent-team/evidence/d10-p1-recovery/results.json)记录 Go1.27.1 `skill/...` unit/race各13顶层16子例、vet及7项独立race探针全部exit0，编译仓库源与固定输入匹配。首次锁定x/text缓存缺失的setup失败原样保留，独立缓存恢复后通过，未改依赖锁。

本轮仅确认纯包与载体，不改变上述后段门槛；`OwnerReader`仍未绑定，`Joined`不证明D05 lease持久释放。验收者已停止写入及命令，报告与证据交主线程提交；P1实现本身已在`8872110`提交，rev2仅同步状态。
