# D10：Skills 初始化与不可变内容

状态：rev2，2026-10-05 仅同步提交与验收进度，P1 契约不变。S01 首个完整结果规格已独立采纳；P1 纯契约与真实 builtin 已提交 `8872110099c84cf0600bb5b62cdcd6c0c6c843e3`，并通过[本轮独立验收](../agent-team/d10-p1-recovery-verification.md)。**Skill 初始化服务、PG、D05 发布、D08 绑定、Agent/Tool/Runner 均未实现验收。** 本文状态不代表 D10 完成。

依据：[开发计划](../development-plan.md)、[Skills 架构](../../architecture/agent-skills.md)、[D01 资源/Skills 契约](d01-contracts/resources-skills.md)、[本工作项规格](d10-skills-initialization-design.md)。S01 候选基线 `71dc17671631632bb26e251ad8491e74092ac975`，原主卡 SHA `a258ed11366946529082e885b5ec1e74d033687d1aec60d69000691862811b88`；独立结论 `/tmp/agenteam-d10-s01-review-4r1gg40i/report.md` SHA `49381427440c1f2a09219361e8a1b902ecb8c0db1d1700a35350050f6136f990` 无新增硬阻断，只采纳规格，不证明真实链路。

## 当前授权 P1

固定实现基线 `f401c15a5187690889eaea9ba9672ca8bdc85460`。新11路径：`internal/central/skill/contract/{types,package,read}.go` 及3对应测试，`internal/central/skill/{builtin,package}.go` 及2对应测试，`internal/central/skill/builtin/add-skills/v1/SKILL.md`。另仅本文及配套规格归位，共13路径；不改旧域、go.mod/go.sum、迁移、fixture、app或共享授权口。

P1交付真实非空 Add Skills 文本、确定性 ZIP v1、不可变 manifest、严格路径/UTF-8/碰撞/资源限制、只读载体与实际本地 reader Close/Read 记账。只返回准备材料，绝不生成已初始化Project、已发布Skill或成功Object的生产结果。`OwnerReader` 是后段端口；没有默认成功实现。规范化使用锁文件已存在 `golang.org/x/text v0.41.0`，不新增依赖。

## 后段真实前置

- D05 初始化专用 Service/SkillRevision/Creation cause 闭集、Service initiator UUID、不可逆 SkillRevision+ProjectDeleted release，各需单独授权及兼容验收。
- **D05 ProjectFactAuthority 实现与 constructor 当前不存在。** 旧 A 的 stop/work/fence 冻结描述属于中断前记录，其未提交主体本轮未恢复，详见[恢复记录](../agent-team/recovery-2026-10-05.md#3-未恢复的实现与临时证据)；Object Audit 写前真实事实/私有同Tx见证方案仍由独立任务审定，不能临时allow。
- P 初始化中的Object Audit专用路线和原失败创建的窄收敛口，以及当前lifecycle provider，均需各自正式冻结和验收。
- 后段 Skill 服务在同Tx插入revision、D05 canonical/Audit及checkpoint；D08再次用其正式四方法确认后才初始化完成。全过程权限、Unknown、物理恢复和Project永久删除需真实PG/MinIO验证。

这些前置不依赖D09默认模型或未绑定Tool/Mount；不阻止P1纯包。它们也不能通过空Skill、假的initializer、跨域查表或仅PUT成功绕过。

## 验收与当前证据

以下保留原作者阶段的历史记录，原 `/tmp` 输入及日志本轮未恢复，不作为本轮重新验收的执行证据：P1 在固定隔离基线+32旧编译文件上完成 `skill/...` unit、race、vet、build，均exit0。最终13顶层/16子例：unit包0.008s/0.104s，race1.026s/1.272s；覆盖路径/ZIP攻击边界和读流实际join。首次12顶层unit同样通过，随后自查补ZIP解析前真实中央目录上限并重验。精确命令/原日志路径为 `/tmp/agenteam-d10-pure-1nuilmr2/validation.json`，当时分别保留原输入两版，并要求独立验收后才由主线程精确提交。原作者记录没有产品测试失败、没有fixture运行，最初Go技能路径定位失败已改读实际 `agenteam-go-development`，不计测试证据。

后段验收见配套规格；未运行PG/DDL/MinIO/网络产品路径，未启服务、未绑定D08，无UI/Tool/Agent/Runner完成声明。原草案SQL路径为 `/tmp/agenteam-d10-s01-skills-uve1ji1r/skills-schema.draft.sql`（SHA `4b473f8785c8a15ca4a7b7e6a8f4b9d33569fd6f47bffe9a10f05fd904854dfc`），本轮未恢复；**没有迁移号、没有仓库迁移文件、没有执行或数据库验收**。

### P1 本轮独立验收

`skill_verification` 未参与该实现，对固定 `8872110` 的 P1 独立验收通过，未发现阻断缺陷。[正式报告](../agent-team/d10-p1-recovery-verification.md) SHA256 `e0fff9717e5ace0ec3c300206073d29312fc823a2d90b39cd779803e1fc40960`；[持久结果与命令](../agent-team/evidence/d10-p1-recovery/results.json)记录 Go1.27.1 `skill/...` unit/race各13顶层16子例、vet及7项独立race探针全部exit0，编译仓库源与固定输入匹配。首次锁定x/text缓存缺失的setup失败原样保留，独立缓存恢复后通过，未改依赖锁。

本轮仅确认纯包与载体，不改变上述后段门槛；`OwnerReader`仍未绑定，`Joined`不证明D05 lease持久释放。验收者已停止写入及命令，报告与证据交主线程提交；P1实现本身已在`8872110`提交，rev2仅同步状态。
