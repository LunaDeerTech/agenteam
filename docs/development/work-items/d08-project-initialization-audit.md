# D08 Project 初始化 Audit 授权库（rev0）

2026-10-09。状态：**SPEC 已获 model_delivery 独立有限审查接受（无 must-fix）；实现与作者验证进行中，作者真实 PG 首个 Facts 已完整通过；另外两 top 与独立产品验收尚未执行。** 工作树 `/workspace/agenteam-project-initialization`，分支 `ai/project-initialization`，交接基线为正式 main `f1c94ee5`。本卡只交付可独立验证的初始化专用授权库，不表示完整 D08 创建 HTTP、Skills 发布或 Object Runtime 已可用。

## 1. 目标、依据与当前缺口

落实 [D10 设计 §2/§6/§10](d10-skills-initialization-design.md) 已约定的 `NewInitializationAuditAuthority`：在调用方的同 Store 活事务、真实已持 Project EX 下，核验原 Project/Creation 初始化事实，再调用同事务的真实上游组合 facts provider。仅新增初始化 Object Audit 路线；普通授权、普通 Append、Lookup、Cleanup 保持原 Authority 行为。

正式依据与事实所有者：

- [D08 Owner 设计](d08-project-owner-design.md) 与现有 [Authority](../../../internal/central/project/authority.go)、[Creation 持久实现](../../../internal/central/project/creation.go)：Project 拥有 Project/Creation、原 owner、原 initialization key、初始化状态；原成功 gate 不变。
- [初始化收敛库](d08-project-initialization-convergence.md) 已于 `39ebd57e` 接受；[验收边界](../agent-team/project-initialization-convergence-verification.md) 是 active-only 的 Project 授权库，不是真 Skills 初始化。它允许原 accepted/initializing/failed/completed 的观察与失败收敛，不授权新 Reserve/Send/Publish。
- [Object Project Audit checker](../../../internal/central/object/project_audit.go) 及其 [既有验收](../agent-team/object-project-audit-verification.md) 已存在。其同 Store/同 Tx 私有 witness、持久 upload/object/attempt/lease/cleanup 事实是真实下游依赖；D10 旧卡“checker 尚不存在”的历史文字不再代表当前状态。
- [现有 Project facts routing](../../../internal/central/project/audit_facts.go) 仅绑定 Secret，`ObjectProducer` 仍 `DependencyUnbound`。本卡不把历史未接受候选当作普通 Object 路线，也不修改这份旧源。
- 当前 Skill 只有 builtin/package/contract，没有真实 Skill Store、initializer 或精确 Skill/Revision/Object/upload/attempt 映射 provider。初始化专用生产绑定因此仍缺依赖；不能以返回 nil 的 provider、任意 Service 或单独 Object checker 替代。

[Object Runtime join 停止边界](../agent-team/object-runtime-join-regression.md) 保持；本卡不调用或修复停止项，不接生产根、创建 HTTP、Skill Service/initializer。库的成功返回只说明本次同 Tx 授权检查通过，不是 commit、published/ready、进程 join 或完整创建成功证明。

## 2. 接口、绑定责任与可验证边界

只在新的 `internal/central/project/initialization_audit.go` 实现既定形状，返回满足现有 [Audit ProjectAuthority](../../../internal/central/audit/contract/access.go) 的私有 wrapper：

```go
func NewInitializationAuditAuthority(
    authority *Authority,
    facts audit.ProjectFactAuthority,
) (audit.ProjectAuthority, error)
```

`authority` 必须是已构造、Store 已绑定的真实 `*Authority`；nil/零 Authority、nil/typed-nil facts 返回 `DependencyUnbound`，不返回半可用 wrapper。构造不查询数据库、不注册 Service、不启动任务，不修改原 Authority 的 routing。wrapper 保留这两个依赖；没有可变 setter、late locator、默认 allow 或错误后的 fallback。

第二参数沿用 [ProjectFactAuthority](../../../internal/central/audit/contract/project_fact_authority.go)，其初始化用途是**一个完整组合 provider**，必须依次证明：

1. 真实 Skill 本域中，原 Project/Creation/初始化命令及其原 `initialization_key`/语义与精确 Skill、Revision、Object、upload/attempt 对应；SkillRevision owner 不能由 ResourceID 猜造，也不能把 RevisionID 当 SkillAggregate 锁 ID。
2. 使用自己的原始初始化请求调用同一个原 Project Authority 的成功或收敛端口，精确比较原 key/Project/Creation。wrapper 从 Creation 读回 key 只证明本域原记录合法，**不能自证 Skill 命令的 key 相同**；`Entry`/`AppendKey` 没有独立初始化 key 字段，该关联由真实 Skill provider 负责。
3. 原 ctx、原 Tx、原 Entry/AppendKey 传给同 Store 的既有 Object checker；后者验证包私有 witness 和真实 Object 前置事实。不得重建 context、伪造 witness、仅查终态或另开事务。

未来 Skill Authority 可依赖自己的 Store、原 Project 两个 gate 和 Object checker，无需依赖 Object Service/Audit Service，符合 D10 无构造环的顺序。组合 provider 不查 Project 私表；只通过原 Project 端口检查自己持有的真实请求。缺任何映射、缺原 key、未持本域锁、没有 witness、错误 Store/Tx 或任一 checker 拒绝，都不能返回 nil；正常业务缺绑定使用 `DependencyUnbound`，不存在/错误映射拒绝，存储错误保留失败。

**接口能力的明确限制：** 两参数形状无法从 Go 接口运行时证明 provider 内部确实组合了上述两域，也无法在构造时反射证明其 Store 相同。构造只能拒绝 nil/零 Project Authority；非 nil 不等于完整依赖。生产根必须只绑定经过实际同 Store/同 Tx 组合验收的实现。当前没有该实现，本卡不新增生产绑定、不提供假实现或可绕过检查的能力票据。若后续要求在构造时机器证明组合身份，须另审共享契约；不得默默新增接口或 type-name 白名单。本卡交付的是完整 Project wrapper 和对上游端口的严格调用契约，不是尚不存在的 Skill provider。

## 3. 唯一新增 Append 分支与身份关系

所有方法保留调用方的原 ctx、Tx、Actor、Entry、AppendKey。`AuthorizeProject`、`CheckServiceLookup`、`CheckCleanupInTx` 直接委托原 Authority，不换 Service 身份、不补锁、不放宽 uninitialized/lifecycle gate；原 Lookup/Cleanup 目前 Unbound 也原样保持。

`CheckAppendInTx` 首先沿原规则检查有效 Tx、typed Entry 和 AppendKey。只有 `ObjectProducer`、下表三个 Object action、typed metadata 的 `initiator_kind=Service` 同时满足，才进入新分支；其余调用直接委托原 `Authority.CheckAppendInTx`，不调用新 provider。Project、Secret、Human/Agent 发起的 Object、ObjectTransfer、其他 producer 均不借初始化分支改变行为。

候选分支还必须满足全部条件，否则失败且 provider 调用数为零：

- Scope 是精确 Project，Actor 是正式注册的 `ObjectService` Service，Actor.ProjectID 与 Scope.ProjectID 相等；不是 `ProjectInitialization` Audit actor，不接受 ObjectMaintenance、任意其他 Service、Human/Agent 或 System scope。
- Actor.CauseRef 精确等于 AppendKey.CauseRef。这个 cause 对应 Object 的 UploadID、AttemptID 或 delete digest，**不是 CreationID**。
- Resource 是 Object；其 ID 与 metadata.object_id 精确相等；Associations 为空。Outcome、ordinal、phase、reason 遵守下表，不能把 generic Audit 可构造的较宽形状当成本分支许可。
- metadata.initiator_id 是精确原 CreationID，initiator_execution_id 为空。Service initiator 的 typed UUID 只是候选标识；必须随后查本域事实及真实组合 provider，不能凭该字符串认定原调用者已获初始化授权。

现 metadata 没有 ObjectFields accessor。实现只从已经通过 `Entry.Validate()` 的 `Metadata.JSON()` 副本提取所需固定字段，不反序列化 Actor/Metadata 私有能力，不接受外部 raw JSON，不修改 Audit contract。不得记录 metadata、标识或错误中的原业务内容。

| Action | Outcome / metadata | AppendKey | 允许的原 Creation 状态 |
| --- | --- | --- | --- |
| ObjectUploadComplete | Success；PublishedPhase；无 reason | cause 为合法 UploadID；ordinal=0 | initializing/completed；accepted/failed 仍 InvalidState |
| ObjectUploadFailed | Unknown；FailedPhase；reason 只 PayloadMissing/IntegrityMismatch/StorageUnavailable | cause 为合法 AttemptID；ordinal=0 或 1，真实 writer/recovery 区分由 Object checker 证明 | active 下 accepted/initializing/failed/completed，仅原失败收敛 |
| ObjectDelete | Success；DeletedPhase；无 reason | cause 为合法 digest；ordinal=1，真实 cleanup cause/object 推导由 Object checker 证明 | active 下上述四状态，仅原初始化对象的既有清理 |

ObjectDelete 不是任意对象删除许可；失败收敛不能发起新的 Reserve/Send/Publish，不能替代 deleting 生命周期的 LifecycleCause。完成后的普通 Human Object 路由仍由旧 Authority 决定，当前仍 Unbound。

## 4. 同 Store、已持锁和 Project 事实

新分支按顺序执行，不能在任何拒绝之前调用组合 provider：

1. 完成 §3 typed/角色/Scope/cause/资源/动作形状检查；不查询任何 Store。
2. 对 Scope.ProjectID 调用本 Authority Store 的 `RequireHeldLocks`，只要求原已持 Project EX。随后 `Store.InTx(tx)` 取得同 Store 活事务 executor。缺锁、SH、别项目 EX、foreign/expired token 拒绝；不 Acquire、不升级、不另开 Tx，保留原 poison 行为。
3. 用原严格 scanner，按 metadata.CreationID 读取 Creation，再按 Scope.ProjectID 读取 Project。Creation.ID、ProjectID、当前 Project.ID、反向 creation_id 与原 owner 精确对应。原 `initialization_key` 必须合法；不得替换成 command_key、cause 或重新生成 key。没有行或选择两个合法但不对应的目标为 Forbidden；确认前向关系之后的反向/owner 矛盾视为 canonical 损坏。
4. 仅 active；复用已接受 `initializationConvergenceFacts` 的四状态完整性：未完成为未 initialized/version1/无 Sprint 与 lifecycle operation/原 name-description 对应/无 protected Skill-revision-result；completed 为 initialized、两个合法保护字段与合法初始 safe_result、原 request 两字段空、safe_reason 空。completed 允许合法后续 name/description/version 变化，历史结果不得冒充当前授权。
5. ObjectUploadComplete 另限定 initializing/completed，原成功 gate 的 accepted/failed 拒绝不变；失败/删除只用收敛状态表，不修改 Creation 状态。
6. 同步调用组合 provider **恰好一次**。传原 ctx、原 Tx、原 Entry、原 AppendKey，保留 Object 包的私有 context witness；provider 返回 nil 才返回 nil。不得捕获失败后改走普通路径、用旧 grant 重试或把取消视为许可。

本库只查 Project/Creation，不查询 Skill/Object 私表，不验证 upload/attempt 私有前置的替身，不用创建人的旧 Session 作服务授权。owner 关系取持久 Creation 和当前 Project，不接受 caller owner 布尔值。所有新分支均不写 Project/Creation、Audit、Outbox、commands、work_claims 或 Account Activity。

| 条件 | 返回与边界 |
| --- | --- |
| 未绑定构造依赖 | DependencyUnbound；无 wrapper、无 I/O |
| 无效 Tx/typed Entry/AppendKey | InvalidArgument；无 provider/事实查询 |
| 新分支的合法但错误角色/Scope/cause/资源/动作形状、缺行或目标不匹配、非 active | Forbidden；无 provider |
| 完整合法 accepted/failed 上请求 UploadComplete | InvalidState；无 provider |
| 缺/弱/错锁、foreign/ended Tx、读/scan 错误、canonical 状态/owner/反向关系损坏 | DependencyUnavailable；保留原内部原因与 Store poison |
| provider 的 Foundation Fault | 原 Fault 原样传播；零额外调用 |
| provider 非 Foundation 错误或取消 | 按现有 `portError` 转为 DependencyUnavailable 并保留 cause；不能变 nil |

本方法不拥有 Commit/Rollback，不创建 goroutine、重试、超时预算、WithoutCancel 或事后查证。返回 nil 后锁必须仍由调用者持有至事务结束；调用者 commit Unknown 按原事务规则处理，不由本库推断成功。重复同 Tx 调用可重复检查，但不产生缓存许可或 idempotency receipt。

## 5. 实施范围与交付条件

仅以下四个新 Go 源；只 SPEC 独立接受后实施：

| 路径 | 责任 |
| --- | --- |
| internal/central/project/initialization_audit.go | 两参数构造、私有 wrapper、精确新分支、事实检查与原方法委托 |
| internal/central/project/initialization_audit_test.go | typed/分支/调用顺序/错误/事实与普通路径的必要受控测试 |
| tests/project/initialization_audit_fixture_test.go | 私有测试装配；复用现真实 PG/Store/本域 Creation fixture，不造 Skill 完成 |
| tests/project/initialization_audit_test.go | 真实 Tx/锁/Project 事实、受控 provider 调用与真实 Object 缺 witness 拒绝 |

本卡与本树 `.agent-state/current.md` 记录可恢复状态；完整验收后只对 `docs/development/backend/README.md` 写实际可用范围和复现命令。无迁移、无依赖锁变化；00024 已由 ProjectVariables 预留，本卡不申请编号。不得修改旧 Authority/成功 gate/convergence/scanner/audit_facts、Audit/Identity contract、Skill、Object、app/root、HTTP/UI、共享 fixture 或停止源。root 后补唯一资源接缝授权：本树既有 `.agent-state/task-planning-recovery/pg_only_supervisor.py` 的 non-root 异常 direct/adopted Wait 有界化；不复制框架、不改旧 root-chain 语义、原工作/收尾/TCP 预算不扩，作者本地进程控制后须未参与者窄审。确需共享口时先报具体缺口，不在局部文件绕开。

交付需：本 SPEC 独立接受；实际实现及作者离线检查；下述完整 PG；未参与实现者独立审查与风险补集真实验收；README 末件准确。任何中间编译/受控绿例不能标整卡完成。当前 SPEC 对上游组合端口的前提不是一份虚构可运行 Skills 绑定。

## 6. 验收矩阵与证据分层

**以下均是待执行计划。** 不以历史 Object/Convergence 验收或接口 fake 代替本库实际结果。

### 6.1 离线纯检查

新增建议 top：`TestInitializationAuditAuthorityContract`、`TestInitializationAuditAuthorityFacts`、`TestInitializationAuditAuthorityDelegation`。用正式 typed 构造器覆盖：

- nil/typed-nil/零 Authority；四方法普通委托与原错误相同；非目标 action/producer/initiator 不调用新 provider。保持 Secret witness 原 context，不重新构造业务身份。
- §3 所有字段逐一错误、cause 与 CreationID 明确不同的合法正例；UploadComplete/Failed/Delete 各精确形状与错误 ordinal/outcome/reason；合法不同 Service/Project/Creation、资源错配、非空 association 拒绝。
- 实际调用顺序与零多余调用：precheck → RequireHeld EX → sameTx → Creation → Project → facts → 一次 provider；早拒绝、每次读错误、provider Fault/普通 error/取消，不调用 Acquire/WithinTx/Exec/Sessions 或写端口。原 context 中的私有测试哨兵和原 token 必须到达 provider。
- 四状态/历史 completed 合法变化，单边 protected 字段、损坏 snapshot、owner/反向/原字段矛盾、错生命周期、原 key/command key 混用。原成功 gate accepted/failed 仍拒，原普通行为不放宽。

运行原 project 包必要 pure/race/vet；新 integration race compile 与精确 list；实际依赖图若涉及原 contract 则测原 contract。Go 固定仓库要求的 1.27.1，离线 `GOPROXY=off GOSUMDB=off`、mod readonly、低并发；无授权前不启动任何真实 fixture。首次编译失败按实际保留，不伪造可构建检查点。

### 6.2 本域真实 PostgreSQL

使用 `tests/project` 已接受的 `newInitializationConvergencePG`/真实 migrator/同 Store 正式 Account Authority 与 Project Authority；新私有 fixture 仅直接种本域 Project/Creation 授权输入。不得调用假 Skill initializer 的 `newFixture/assemble` 来冒充真实初始化。Project 测试 seed 中 completed 的 Skill ID/revision 只是 Project 持久字段，不能据此声称存在真实 Skill 包。

1. `TestProjectInitializationAuditFacts`：三个动作 × 四状态必要正负、精确 Creation/Project/owner/cause、合法 key 与错误配对、active/非 active、可由正式 SQL 表达的 canonical 损坏、completed 后合法更新；真正执行 wrapper。受控 provider 只在期待的同 Tx 精确参数下接受，拒绝任一差异，所有失败不得越过调用门槛。前后本域与 Audit/Outbox/Account/commands/work_claims 快照无写。不能注入的非法 SQL 值用 pure scanner 表达，不以 schema 拒绝算方法拒绝。
2. `TestProjectInitializationAuditTransactionBoundary`：无锁、SH、别 Project EX、foreign Store token、已结束 Tx 拒绝；忽略 missing-lock 错误仍实际不能 commit。另一真实 connection 对同 Project 的 SH/EX 在 caller EX 下按实际 PG 阻塞事实等待，wrapper 返回 nil 后仍等待，caller commit/release 后完成；不能用固定 sleep 证明锁。取消和关闭 Store 拒绝；所有 holder 实际 release/join，无隐含 Tx。
3. `TestProjectInitializationAuditDelegation`：真实本域正向 gate + 严格受控 composite 仅证明参数/调用契约；provider 内拒绝、返回 Fault 与非 Fault、真实查询错误或 caller rollback 均失败且无写。另把同 Store **真实 Object checker** 作为负控传入：没有包私有 witness 时必须拒绝，不能把本域 gate 正例提升为 Object 发布许可。普通调用与原 Authority 并排比较结果/副作用，原 Object 普通 Unbound 保持。

上述 controlled provider 不宣称真实 Skill mapping，也不模拟一份 Object 表或提供 witness minting hook。**当前没有可执行的真 Skill+Object 初始化正向链**；该缺口是明确的后继 D10 集成门槛，不隐藏在本卡 PG 通过数量内。实际两域组合验收必须等真实 Skill provider、正式 Object 初始化入口及 runtime 停止项获得各自授权后另做。

### 6.3 独立验收、回归与资源

未参与实现者必须亲自审最终输入，至少独立构造两类风险组合：一类原 cause/Creation/key/状态或 owner 错配与真实受控 provider 拒绝；一类真实 foreign/ended Tx 或弱锁/poison/锁持有时序。独验可用新输入的真实 PG，不把作者结果或旧验收换名为独验。

旧成功 gate 和 convergence 保持原实现；新受控逐状态对照加现有两域 pure 是必要回归。若真实装配复用影响旧 D08 fixture，补精确旧 selector `TestProjectB02InitializationRejectsWrongMappingPlanAndActualSkillFacts`、`TestProjectB02InitializationMergesCompleteLockPlanAndPoisonsMissingLock`，仅记录其原 fixture 层级，不称生产 Skills。无需运行停止的 Object Runtime、MinIO 或完整 app/browser。

真实 PG 窗由父/root 串行分配；执行前冻结实际 Go/test/迁移/fixture 动态闭包，先 race compile/list 再真实 run，不因可编译自行联网。复用既有测试资源监督和固定镜像，若它额外启动的资源超过本域所需，先报实际资源图与有界替代，不另造巨大框架。每新 top 120s（含 Cleanup）、包 6m；锁 holder/driver/直接与 adopted 子进程均须实际 wait，owned ID 两次 absent、runtime/private 与监听清空、输入不变。清理不无限续预算，不影响非 owned 资源；业务 PASS 与资源 PASS 分开记录，未 join 或退出缺口整体失败并留原件。

## 7. 恢复与当前状态

SPEC 已由未参与其实现的 model_delivery 独立有限审查接受，无 must-fix，root 已授权四新 Go 源持续实施。当前 wrapper 与纯测试已落盘；作者离线检查和后续真实 PG 的实际结果分别记录在本树 current，不把中间编译或纯测试当产品独立验收。原首次依赖缓存缺失、冷构建超时和测试编译/预期错误保留，不回填。尚未运行 PG/browser/网络或执行 Git，生产绑定仍缺真实 Skill provider。后续持续完成四新源、实际验证及 README；真实资源另等明确交窗。

### 作者当前实际结果（2026-10-09）

四新 Go 源已完成，原成功 gate/普通 routing/共享域源未改。Project/contract 全部普通与 race pure、vet 以及 integration race 编译/精确三 top 发现均实际 exit0；这是作者检查，不是独立产品接受。资源监督器 non-root 有界退出窄修已由未参与者独立六控制接受；不改变正常两 ID、旧 root-chain 或业务预算。

`TestProjectInitializationAuditFacts` 首轮由作者本人实际执行：session64065 outer exit0，Go 2.57s、26子例全部通过，完整72.323s。Go/driver实际Wait0，无adopted残留，owned descendants两空；1PG容器+1nonce network两ID双absent，私有凭据/CA私钥已删除（目录仅留安全owned.json），TCP两空、driver/binary inputs unchanged。原始安全日志：`output/ai/project-initialization-audit/pg/pg-bee9d7b19c6b4f80b22ac79400bbc0a5.log`。窗口已向root释放。

本轮只证明真实 Project/Creation 状态/原key/owner/事务中的授权调用门槛及受控 delegate 契约，不能推成真Skill/Object发布。`TestProjectInitializationAuditTransactionBoundary`、`TestProjectInitializationAuditDelegation` 和未参与者独立产品/PG补集仍待实际运行；不标本卡完成，不用旧验收顶替。下一top保持同一四Go源、race binary与已独审supervisor，等root freshgrant。
