# Skills P2 / 00027 有限交付清单

2026-10-10，只读核对后的装配方案；尚未装配或执行新测试。写域只有本文。Cleanup 当前冻结的两个工具、entry-controls 和 current 未改，真实 Cleanup 候选与验收窗口独立排队。

## 1. 基线与交付结论

以正式 main `4c1db71cf0f86cb6d6330167577944b0466c8664` 为本次比对底，在 Runner 的 `00026_runner_control.sql` 正式进入 main 后，从届时 main 建独立交付树。P2 来源为 `eaad50fdb08e248b850a65c74f49d3aabf73b682`，其 Skill 产品与已验 `d0a16242` 相同；独立补集来自 `5bb671868ce5b4f9f0ff18fde0a692d432392911`。不得从当前 Cleanup 树复制 Skill 或 Object 产品，因为其中已有本次不交付的新清理实现。

有限结果可以独立交付：原四方法初始化、持久不可变 revision、真实 D05 发布和当前 Owner 读取、原命令恢复、精确 Stop 子能力，以及 00027 六表。它不是默认根可用的 Project 创建、完整生命周期 participant 或完整 D10。当前没有已知必须重跑全部作者/独立真实矩阵的源码差异；必须先完成下列逐字/共享合并核对与定向离线装配检查。若 Runner 正式交付改变本次列出的共同依赖或 SQL 前缀，先按实际差异收窄补验，不能沿用“字节未变”结论。

实际前缀核对：main 当前终点为 00025；P2 已验的 00026 与 `/workspace/agenteam-runner-control/db/migrations/00026_runner_control.sql` 当前逐字相同，SHA256 均为 `005ac481f6d76ac5035ad4174338fc7bf0385795ee15d84cf11067366373ab04`。这里只证明本次读取相同，不代替 Runner 的正式交付。Skill migration 测试从 embedded source 截取连续 `<=00026` / `<=00027`，不需要把旧树的 24/25/26 再覆盖到 main。

建议正式范围共 **51 路径：42 个新增、5 个共享窄合并、4 个文档**。其中 41 个新增保持同路径原字节，另一个独立纯测试只移动装配路径、保持全部字节。本文是交付准备资产，不计入这 51 路径。

## 2. 可直接复制的新文件

下列 40 路径均从 `eaad50fd` 取完整 blob；当前 main 不存在同名文件。现有 P1 `skill/contract`、builtin 与包编码文件原样保留。

29 个 Skill Go 文件（包含作者同包测试）：

```text
internal/central/skill/audit_authority.go
internal/central/skill/audit_authority_test.go
internal/central/skill/authority.go
internal/central/skill/initialization_read.go
internal/central/skill/initialization_read_test.go
internal/central/skill/initialization_state.go
internal/central/skill/initialization_state_test.go
internal/central/skill/initialization_write.go
internal/central/skill/initialization_write_test.go
internal/central/skill/lifecycle_stop.go
internal/central/skill/lifecycle_stop_test.go
internal/central/skill/object_authority.go
internal/central/skill/object_authority_test.go
internal/central/skill/object_maintenance.go
internal/central/skill/object_maintenance_test.go
internal/central/skill/read.go
internal/central/skill/read_package.go
internal/central/skill/read_package_test.go
internal/central/skill/read_test.go
internal/central/skill/recovery.go
internal/central/skill/recovery_test.go
internal/central/skill/repository.go
internal/central/skill/repository_test.go
internal/central/skill/runtime_work.go
internal/central/skill/service.go
internal/central/skill/service_test.go
internal/central/skill/store.go
internal/central/skill/work_repository.go
internal/central/skill/work_repository_test.go
```

8 个作者集成测试、00027 和 2 个 Object 同包测试：

```text
tests/skills/admission_unknown_test.go
tests/skills/commit_recovery_test.go
tests/skills/fixture_test.go
tests/skills/initialization_test.go
tests/skills/lifecycle_stop_test.go
tests/skills/migration_test.go
tests/skills/object_publication_test.go
tests/skills/owner_read_test.go
db/migrations/00027_skills.sql
internal/central/object/contract/skill_initialization_test.go
internal/central/object/skill_initialization_test.go
```

另加独立补集 2 路径，来源均为 `5bb67186`：

| 交付目标 | 来源与装配方式 |
| --- | --- |
| `tests/skills/p2_independent_test.go` | 同名文件完整 blob。保留真实 D05/SQL、上游 seed 与 nil Runtime 的原注释。 |
| `internal/central/skill/p2_independent_test.go` | `.agent-state/skills-p2-independent/pure_test.go` 原字节，移动到正式同包测试。 |

独立纯测试是 `package skill`，只依赖本批已有 `readFixture`、`stateID` 等受控 fixture，无 embed、文件路径或 runtime.Caller 依赖；原作者另以 `601d5c` 只读确认可这样移位。旧 `pure.py` 有 WIP `eaad50fd` Git 守卫和本机 cache 默认值，故不作为正式入口复制。移位后直接用 Go 的两个原 top 执行，无须新 Python runner 或 overlay。纯测试仍明确 Store/Project/Object doubles，不提升为真实 PG 授权证据。

## 3. 只在 main 上窄合并的 5 个共享文件

当前 `main4c1 → eaad` 在下列文件中恰好只有本表增量。装配仍按 hunk 合并，完成后逆去本表增量应逐字回到交付基线；不以旧整文件覆盖未来 main 的相邻变化。

| 路径 | 唯一允许增量与保留项 |
| --- | --- |
| `internal/central/object/contract/authority.go` | `NewOwnerAuthorization` 的 Service Read/Mutate 仅允许 ProjectInitialization + SkillRevision + 同 Project、合法 UUID CreationCause 与 Actor.CauseRef 相同；拒正文 ObjectID/protected lease。原 Converge/Lifecycle、Human/Agent 和全部后续约束不动。 |
| `internal/central/object/transfer_upload.go` | `reserveObjectCommand` 新 Service 窄分支再次核上述身份和原 grant/CreationCause，再以 CreationID 作 initiator；Human/Agent 原分支及 SQL 不动。 |
| `internal/central/object/contract/access.go` | `NewCleanupReleaseAccess` 的 closed predicate 仅加 SkillRevision + valid Project + ProjectDeleted；完整 Avatar/Knowledge 分支及其他 access 不动。 |
| `internal/central/object/contract/reference_cleanup.go` | 对上述 closed owner/reason 增量的注释；接口签名不动。 |
| `internal/central/object/contract/knowledge_cleanup_test.go` | 原全 owner/reason 矩阵的预期只加 SkillRevision + ProjectDeleted；原矩阵、binding 负例不删改。 |

后 3 项只是已有 P2 验证过的 closed contract/兼容面；P2 Authority 仍没有实际 CleanupAuthority，不能据形状构造成功宣称原子 Release、物理删除或 metadata purge 可用。本次不引入 `DeletedObjectMetadataPurger`、新 purge operation 或 00028。

## 4. 必须保留 main 的依赖闭包

- `internal/central/project/**` 全部保留 main。当前 main 已含 B02 Knowledge/Object Audit 委托、普通 Variables event、修正的初始化 Human/Agent 委托测试及 ce657 Skills CleanupPhase。P2 不需要旧树 Project 差异；覆盖会丢新路由或退回旧测试。实际初始化 Service 路径继续用正式 `NewInitializationAuditAuthority` 的原 Project EX/current gate 和真实 Object private witness。
- `internal/central/audit/**`、HTTP/schema/client、Knowledge Owner read、所有 B02 源及 migration25 全部保留 main。旧 eaad 缺新的普通/Secret Variable Audit variants，不能把旧 metadata/types/wire 搬回。
- Foundation、Identity、Postgres、Project contract、P1 Skill contract、testsupport、go.mod/go.sum：本次核对 main4c1 与 eaad 全部无差异；不复制、不另发依赖版本。
- `commit_recovery_test.go` 直接 import 的 `.agent-state/project-variables-independent/commitproxy` 已在 main，`proxy.go/proxy_test.go` 与 eaad 逐字相同。它是实际集成测试编译/执行依赖，交付树不得用 sparse/file manifest 漏掉；无需本批重复提交。
- Object 正式库除第 3 节 5 个 hunk 与第 2 节 2 个新测试外保留 main。特别不混入 Cleanup 树承接的 D05 22 源或其 Stop/metadata/索引实现。
- 00027 由现有 `db/migrations/embed.go` glob 包含，不改 embed、旧迁移/checksum、FK/列/约束或任何 SQL28。00027 中 cleanup 表/FK 是本次数据模型的一部分；存在该表不表示后段清理已实现。
- 所有 app/启动关闭链、Project registry、immutable owner router、完整 `agent-skills-variables` manifest 与默认 root 保留 main。本批不接生产 initializer，也不以只有 Skills 的 Stop 报告替代 Variables/Agent。

## 5. 四个文档的最小收口

文档必须在 main 现态编辑，不能直接覆盖旧 eaad 或当前 Cleanup 树的整卡。

1. `docs/development/work-items/d10-skills-initialization.md`：保 P1 既有验收链接，开头改为“P2 初始化/不可变内容/Owner 读与精确 Stop 库及 00027 已有限接受”；用第 6 节固定证据说明版本组合。保留 4315、P2-01 等原 FAIL 及受控/真实边界，不再把历史“未 PG/未 MinIO/迁移待验”当当前结论。完整 Cleanup、生产 root 与全部 D10 未完成。
2. `docs/development/work-items/d10-skills-initialization-design.md`：保 P1 编码规则，以 eaad 的 §§10–15 描述实际 P2 四口/持久关系/Unknown/读与 Stop；逐处把 CleanupAuthority、Lifecycle Audit、新 purge 等归为后续合同，不能写成 P2 已实现。§§1–9 的整体生命周期目标保为目标，不作为本次结果。更新“Project CleanupPhase 不存在”为 main 已有有限 gate、P2 尚未消费清理；本次不导入当前 Cleanup rev2 §16 及其实现状态、00028 或组合验收。
3. `docs/development/backend/README.md`：仅加一段有限 Skills P2 库说明/卡链接，并修正现有初始化 Audit 小节“Skill provider 仍未实现”为“本域 provider 已有限实现/验收，生产 root 未绑定”。保全部 main Variables/B02/Audit/HTTP 等内容。实际旧 eaad 对 README 的差异只有删除已正式普通 Variables 段，因此旧 README 绝不能复制。
4. `docs/development/agent-team/tasks.md`：只改既有 Skills 初始化那一行；不改 D08、Agent、Variables、Secret、B02、D12 或全局 ready。建议事实为“P1/P2 限定库与 00027 已交付；真实初始化/不可变包、当前 Owner、原命令与 Stop 有作者版本组合及独立风险补集；完整 Cleanup/participant/创建 HTTP/root 和 Agent/Tool/Runner 消费未完成，Object Runtime join 停止项保持”。

## 6. 可复用的实际接受与限制

| 结果 | 固定实际证据 | 不能外推的边界 |
| --- | --- | --- |
| 持久初始化、发布回滚、原 COMMIT 恢复 | 作者 60950、58518、39205 均原完整 PASS | 前三轮原产品组合；Object 为受控端口，不称它们已经运行后加 Stop 或真实 MinIO。 |
| 当前产品精确 Stop | 96753，12 子完整 PASS；Knowledge 对 d0a16242 六源有限独审及 62067 纯控 | 规范 Project/lifecycle seed、受控 Object/Process；不证明 foreign death/完整 participant。 |
| 真实 D05 发布/读取 | 70196，3 子完整 PASS | 真 MinIO、canonical、private witness、重建 replay、EOF/Close/lease/work；Project/Creation/Human 为 seed，Runtime=nil/Guard 仅构造，未绑定清理。 |
| Migration/AdmissionUnknown/Owner | 修复后的 61543 完整 PASS：Admission 2、Migration 4 直接子含 30 约束、Owner 12 | 原 4315 wholeFAIL/TCP 缺项保留；修复仅 a5eb 两 fixture，37ed09 独审接受。规范 Session/Project/Delete seed 不冒 Login/Create/BeginDelete。 |
| 未参与实现者 P2 补集 | 21592/238565：2 pure top、7 子；P2-02 69242/a45a4e：1 real top、4 子，whole PASS | 当前 plan/权限重验、实际 D05 读与外层 Read/Close return、Skill work 和 D05 lease 分列。原 P2-01 39104 wholeFAIL 不回填；纯 doubles 不冒真实授权，外层 hold 不冒 MinIO 网络阻塞。 |

所有上表称 whole PASS 的真实组，原 Go/driver actual Wait、owned 资源双退役、private/runtime/desc/TCP 双尾和输入不变均由原记录闭合；不是当前 main 单一新 binary 的一次全套通过。既有日志/原失败留在已远端保存的作者和独立分支，正式卡保留准确定位/版本，不复制 ignored 二进制、私有 runtime 或凭据。

## 7. 装配后的必要检查（本轮未执行）

先由 root 装配，做 41 个同路径新文件与移位 pure 文件逐字比较；共享 5 文件逆去唯一增量后必须等于新 main 基线；其他技术路径零差异。再核连续前缀 1..27、链接、仅 D10 台账单行及文档有限表述。Runner26 正式字节若保持本次值可复用旧27前缀证据；若有变化只针对实际影响评估 migration/相邻接口补验，不自动扩跑全部矩阵。

定向离线检查建议以下最小集合，在 root 允许的既有 cache/固定 Go/off/readonly/-p1 与磁盘门内执行：

- `go test -race -count=1 -timeout=60s ./internal/central/skill/...`：实际装配源与新移位的 2 个独立 top 都执行；不另跑旧 overlay runner。
- Object 两包只运行 `^Test(SkillInitialization|SkillCleanupRelease|KnowledgeCleanupRelease)`：覆盖两个共享权限/initiator 分支、精确 cleanup shape 与旧 Avatar/Knowledge 矩阵。
- Project 只运行初始化 Audit、Knowledge/Object Audit、普通 Variables Audit/event 和 Secret read-shape 拒绝相关 top；目的为验证 main 现有委托/当前 gate 未因装配丢失。保主干测试原字节，不复制旧 eaad 初始化委托 fixture。
- 对实际新 Skill 与 5 个 Object 受影响文件所在包作一次 vet；integration 只做一次 `go test -tags=integration -race -c ./tests/skills` 和精确 `-test.list`，不启 PG/MinIO。编译输入包括 8 作者 + 1 独立业务文件及正式 commitproxy。

integration 预期恰好以下 9 个 top；list 只作发现，不计业务通过：

```text
TestSkillInitializationPersistence
TestSkillInitializationPublicationRollback
TestSkillInitializationCommitRecovery
TestSkillMigration
TestSkillInitializationAdmissionUnknown
TestSkillOwnerMetadataCurrentAuthority
TestSkillLifecycleStopPersistence
TestSkillObjectInitializationPublication
TestSkillIndependentP2ConfirmationAndPackage
```

没有新增产品语义且上述保持成立时，有限交付不需重跑既有所有 PG/D05 矩阵。若装配必须修改实际权限、锁、事务、reader 或 SQL 逻辑，就不再是本清单的逐字/窄合并交付，须报告具体受影响路径并补其必要验证。原完整 Cleanup 的 2top/5sub 候选、新 metadata 清理/00028、全 Project/root 和 Runtime 停项不进入本批验收或完成宣称。

## 8. 本次只读核对记录

`8bbcfa/ec23fe/a743b8/8e478c` 是此前源/路径比对；本次恢复后 `0db60b` 实际显示五共享唯一 hunks，`64d2ff` 确认29+8及27新增，`143ea9` 核 main 新 Project/Audit 与未变底层依赖，`986226` 核 main 初始化普通委托/README，`85003e` 核 26 当前逐字相同与29+8全新增。一次只读路径查找用了不存在的 `docs/design/...`（0698d2 exit2），一次误查 `00026_runner.sql`（e085d7 内 git show 报 path 不存在）；均已按实际 work-items 路径和 `00026_runner_control.sql` 纠正，未被计作检查通过。未编译、未运行资源、未修改产品/SQL/冻结四路径或执行 Git 写操作。
