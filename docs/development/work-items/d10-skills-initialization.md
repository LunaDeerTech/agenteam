# D10：Skills 初始化与不可变内容

状态：rev3，2026-10-10。P1 和 P2 初始化／不可变内容／当前 Owner 读取／精确 Stop 库及 00027 已有限接受；本次在正式 Runner 前缀之上交付。完整 Cleanup、生产 initializer／多域 participant、创建 HTTP/root 与 Agent/Tool/Runner 消费仍未完成，Object Runtime join 停止项保持。本文不代表 D10 完成。

依据：[开发计划](../development-plan.md)、[Skills 架构](../../architecture/agent-skills.md)、[D01 资源/Skills 契约](d01-contracts/resources-skills.md)、[本工作项规格](d10-skills-initialization-design.md)。S01 候选基线 `71dc17671631632bb26e251ad8491e74092ac975`，原主卡 SHA `a258ed11366946529082e885b5ec1e74d033687d1aec60d69000691862811b88`；独立结论 `/tmp/agenteam-d10-s01-review-4r1gg40i/report.md` SHA `49381427440c1f2a09219361e8a1b902ecb8c0db1d1700a35350050f6136f990` 无新增硬阻断，只采纳规格，不证明真实链路。

## P2 有限交付

实现正式 `ProjectSkillInitializer` 四方法、私有 confirmation plan、持久同 key 恢复、不可变 Add Skills revision 与当前 Owner 的目录／metadata／包读取。真实 D05 Reserve 映射、Publish/canonical/native Audit 与本域 revision/checkpoint 在同一 Store 的原 Tx 闭合；Unknown 保留原 cause/attempt，取消不冒实际 return 或 lease 已提交。Project/Creation/Session 的部分上游事实在集成测试中为明确 seed，不声称运行 Project.Create、Login 或 BeginDelete。

精确 Project Stop 子能力在既有 StopPhase 端口和完整锁下捕获原工作，提交后取消，实际 join 后才 Stopped；Archive 保留合法 reader，Delete 包含 reader。Service 只报告 Skills 本域，不提供 Name 伪装完整 `agent-skills-variables` participant。真实 foreign ProcessAuthority 和生产共享 guard 接入不在该范围。

00027 建立本域六表及原 FK/CHECK；cleanup 表是后段数据模型，不表示已实现清理。当前 Authority 仅实现 ResourceAuthority、AccessPlanner、ObjectReadAuthority、ProjectGate 与技术返回所需 exact mapping；没有本轮 CleanupAuthority、生命周期 Audit 外层、metadata purge 或新清理迁移。Project 已正式提供有限 Skills CleanupPhase gate，本 P2 库尚未消费完整清理。D05 `SkillRevision + ProjectDeleted` Release 的 closed shape 属兼容面，不能凭构造成功认物理删除可用。

交付来源：Skills 作者产品/测试 `eaad50fdb08e248b850a65c74f49d3aabf73b682`（产品与 `d0a16242` 相同），独立补集 `5bb671868ce5b4f9f0ff18fde0a692d432392911`；在 main `fb6ab7f492850bf1d3c025a59acbff381312a989` 上装配 42 新文件和 5 个 Object 窄 hunks，保留 main 全部 Project/B02/Audit/Variables/Secret/root。独立纯测试仅从原 overlay 源移到正式同包路径，断言字节不变。00026 与先前已验前缀逐字相同，迁移连续到 00027；不覆盖旧迁移。

## P2 实际证据与限制

| 结果 | 固定实际证据 | 不能外推的边界 |
| --- | --- | --- |
| 持久初始化、发布回滚、原 COMMIT 恢复 | 作者 60950、58518、39205 均原完整 PASS | 前三轮原产品组合；Object 为受控端口，不称它们已经运行后加 Stop 或真实 MinIO。 |
| 当前产品精确 Stop | 96753，12 子完整 PASS；Knowledge 对 d0a16242 六源有限独审及 62067 纯控 | 规范 Project/lifecycle seed、受控 Object/Process；不证明 foreign death/完整 participant。 |
| 真实 D05 发布/读取 | 70196，3 子完整 PASS | 真 MinIO、canonical、private witness、重建 replay、EOF/Close/lease/work；Project/Creation/Human 为 seed，Runtime=nil/Guard 仅构造，未绑定清理。 |
| Migration/AdmissionUnknown/Owner | 修复后的 61543 完整 PASS：Admission 2、Migration 4 直接子含 30 约束、Owner 12 | 原 4315 wholeFAIL/TCP 缺项保留；修复仅 a5eb 两 fixture，37ed09 独审接受。规范 Session/Project/Delete seed 不冒 Login/Create/BeginDelete。 |
| 未参与实现者 P2 补集 | 21592/238565：2 pure top、7 子；P2-02 69242/a45a4e：1 real top、4 子，whole PASS | 当前 plan/权限重验、实际 D05 读与外层 Read/Close return、Skill work 和 D05 lease 分列。原 P2-01 39104 wholeFAIL 不回填；纯 doubles 不冒真实授权，外层 hold 不冒 MinIO 网络阻塞。 |

所有上表称 whole PASS 的真实组，原 Go/driver actual Wait、owned 资源双退役、private/runtime/desc/TCP 双尾和输入不变均由原记录闭合；不是当前 main 单一新 binary 的一次全套通过。既有日志/原失败留在已远端保存的作者和独立分支，正式卡保留准确定位/版本，不复制 ignored 二进制、私有 runtime 或凭据。


原 4315 whole FAIL 的 Admission/Deleting fixture 错误及 TCP 尾缺项、独立 P2-01 39104 whole FAIL 均保留；修复后的新候选和真实组分别计证，不回填旧失败。旧 opaque cause 的函数字段 DeepEqual 改为正式完整语义比较；Deleting fixture按原 Owner/version/accepted operation/manifest/participants 的有效同 Tx 事实建立 FK，不关约束，不称 BeginDelete。独立包读取区分真正 D05 lease 与外层原 Read/Close held 状态，不称外层 hold 是 MinIO 网络阻塞。

## 本次装配验证

作者c0ff89与未参与者Runner d154f6分别核对42新源（含pure移位）逐字来源、5共享精确hunk、保护域与连续前缀1..27。装配后33262/25555d原outeractual0：Skills两包race42top/183sub、Object受影响闭集race5top/10sub、Project相邻初始化/Knowledge/Object/Variables/Secret路由race13top/57sub，全部0skip；Skill/Object对应包vet0。唯一一次integration race-c0及九top精确list0。没有修改47技术源，没有重复PG/MinIO；list只作入口发现，实际业务复用上表固定结果。

## 已交付 P1

固定实现基线 `f401c15a5187690889eaea9ba9672ca8bdc85460`。新11路径：`internal/central/skill/contract/{types,package,read}.go` 及3对应测试，`internal/central/skill/{builtin,package}.go` 及2对应测试，`internal/central/skill/builtin/add-skills/v1/SKILL.md`。另仅本文及配套规格归位，共13路径；不改旧域、go.mod/go.sum、迁移、fixture、app或共享授权口。

P1交付真实非空 Add Skills 文本、确定性 ZIP v1、不可变 manifest、严格路径/UTF-8/碰撞/资源限制、只读载体与实际本地 reader Close/Read 记账。只返回准备材料，绝不生成已初始化Project、已发布Skill或成功Object的生产结果。`OwnerReader` 是后段端口；没有默认成功实现。规范化使用锁文件已存在 `golang.org/x/text v0.41.0`，不新增依赖。

### P1 本轮独立验收

`skill_verification` 未参与该实现，对固定 `8872110` 的 P1 独立验收通过，未发现阻断缺陷。[正式报告](../agent-team/d10-p1-recovery-verification.md) SHA256 `e0fff9717e5ace0ec3c300206073d29312fc823a2d90b39cd779803e1fc40960`；[持久结果与命令](../agent-team/evidence/d10-p1-recovery/results.json)记录 Go1.27.1 `skill/...` unit/race各13顶层16子例、vet及7项独立race探针全部exit0，编译仓库源与固定输入匹配。首次锁定x/text缓存缺失的setup失败原样保留，独立缓存恢复后通过，未改依赖锁。

P1 证据只覆盖纯包与载体；P2 的服务／PG／真实 Object 接受按上表另计，不由 wrapper Joined 推导 D05 lease 持久释放。原缺失 `/tmp` 草案不是当前迁移来源，正式数据模型为本次原字节 00027。
