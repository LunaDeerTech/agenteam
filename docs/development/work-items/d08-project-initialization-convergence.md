# D08/D10 Project 初始化收敛授权库（rev1）

产品接受（2026-10-08）：Project 初始化收敛授权库完整6路径（5技术＋README）已接受，产品 `39ebd57e` 已推送且 root 核远端一致；6真实PG／40子例、42不同资源ID与24非owned未wait shim（449→473、轮间无额外）沿原表归档，owned资源实际wait双清，原STATIC／格式／工具失败保留。新gate无写、原成功gate不变；生产Skills/root与创建HTTP未绑定，完整D08/D10未完成。 [完整验收与原始证据](../agent-team/project-initialization-convergence-verification.md)。本段仅更新行政状态，以下规格归档历史与§1起技术原字节保留。

持久归档（2026-10-08）：rev1全文及正式末件完整STATIC已接受；规格 `b01b08d440ca253124efe4e725dea093fa1a958e` 已推送并核远端一致，[原始审查、工具修正与来源映射](../agent-team/project-initialization-convergence-spec-verification.md)已归档。当前仅另授5技术scratch实施，不安装仓库、不Go或资源；不是产品接受。以下原状态按正式归位时点保留，§1起技术原字节不变。

2026-10-08。状态：**完整规格独立 STATIC PASS，root 已采纳（含 active-only 工程边界）；正式归位末件待核，尚未授权产品实施、Go 或资源。** 固定产品基线 `4089d13128da8680955005d9507c8a7da74af1f1`。本卡归位不证明产品已经实现；原 scratch、独审与固定来源指纹见 §8。

## 1. 必要性、已接受依赖与结果边界

本卡落实 [D10 设计 §2/§10](d10-skills-initialization-design.md) 已明确的可选 `InitializationConvergenceAuthority`：调用方在原 Project/Creation 的同库活事务、已持 Project EX 下，核验初始化元数据观察及既有失败收敛所需的 Project 事实。它是可独立编译、实际 PG 验证、供未来 Skills 消费的完整授权库结果；不是一张仅留签名的占位卡。

已接受 [D08 Project 库](d08-project-owner.md) 有持久 Creation、当前 Project、原成功初始化 gate；[D10 P1](../agent-team/d10-p1-recovery-verification.md) 有真实 builtin AddSkills、不可变包和纯契约，没有生产 Skills Store/initializer/cleanup。当前 [Object Project Audit checker](../agent-team/object-project-audit-verification.md) 已接受，D10 旧卡“尚无 ProjectFactAuthority”不能作为当前事实继续引用；其存在不等于 Project 初始化 Object dispatch 已绑定。

[Object Runtime 未 join work 退出缺陷](../agent-team/object-runtime-join-regression.md) 仍受明确停止边界约束。固定 `42e3f7d` 与本基线四个关键源 blob 相同，见 [停止源核对](#停止源同字节核对)。D10 §6 要求未 join work 保持共享 guard，这条真发布/停止链不能由本卡解决或绕过。本卡不启动 Object Runtime，不修停止项，不把独立 gate 接成生产 Skills 初始化、创建 HTTP 或根绑定。OpenAI tools 独验、SPA 并发发布两项停止也不变。

成功交付只表明：新增 Project 所有的可选授权端口实际可用、错误请求/不一致事实/错误事务和弱锁均被拒绝、原成功 gate 不变。未来 Skills 仍须在同 Tx 核验自己所有的 Skill/Revision/Object/attempt 映射，取得真实本域授权并完成实际发布与收尾；本 gate 的 nil 不是其写授权、ready receipt、joined/death 证明或任意 Object 内容读取许可。

## 2. 公共接口、事实所有者与调用顺序

在 `internal/central/project/contract/initialization_convergence.go` 新增且只新增此可选接口：

```go
type InitializationConvergenceAuthority interface {
    ValidateInitializationConvergenceInTx(
        context.Context, foundation.Tx, identity.Actor, InitializationRequest,
    ) error
}
```

由现有 `*project.Authority` 在新源实现，并以编译期断言验证满足接口。使用原 `NewAuthority` 与其同一个 Store，不增加构造函数、配置或依赖。**不扩旧 `ProjectAuthority` 接口**，不改 `ValidateInitializationInTx` 签名/实现/调用者，不改四方法 `ProjectSkillInitializer`、PlanIssuer 或任何 request/receipt JSON。未来消费者显式接收新接口，不以类型断言失败后回退 allow，也不把它代替旧成功 gate。

[原 InitializationRequest](../../../internal/central/project/contract/initialization.go) 只有 `CreationID / ProjectID / InitializationKey`。三字段都逐项使用原 Validate 并与正式事实精确匹配，不新增 Owner/User 输入，不把 `command_key` 当 `initialization_key`，不要求字符串前缀来替代已持久化原 key。原 owner 由 Creation 表取得，与当前 Project.OwnerUserID 比较；它不来自 actor 的 Human 信息或历史 safe_result 授权。

按以下顺序处理，全程只用调用方 ctx 和原 Tx：

1. nil/零值 Authority 返回 `DependencyUnbound`；无效 Tx、Actor、三字段返回 `InvalidArgument`，均零 Store 调用。
2. 使用 [正式注册的 Actor](../../../internal/central/identity/contract/identity.go)；必须 `Kind=Service`、`ServiceName=ProjectInitialization`、`ProjectID=request.ProjectID`、`CauseRef=request.CreationID.String()`。System scope、Human、Agent、其他 Service、别的 Project/Creation 或合法 digest cause 均 `Forbidden`，零事实查询。注册角色本身不是持久事实授权。不能从 JSON 反解 Actor，不能凭旧 Human Session 代替该服务上下文。
3. 调用本 Authority Store 的 `RequireHeldLocks(ctx, tx, [{ProjectLock(request.ProjectID), Exclusive}])`。它只检查已有锁，不能 Acquire/升级/开新事务；缺锁、SH、别项目 EX 都失败。随后 `Store.InTx(tx)` 取得同 Store 活 Tx executor。正式 Store 拒绝 foreign Store、已退出或非 Store 创建的 token；有值的 `foundation.NewTx()` 不是持久事务证明。RequireHeldLocks 的原 poison 行为保留。
4. 仅通过该 executor 读本域 `creations` 与 `projects`。复用原严格 scanner；在新私有 helper 补本卡明确的跨行/状态对应检查，不改共享 scanner。`creation.ID == request.CreationID`、`creation.ProjectID == request.ProjectID`、`project.ID == request.ProjectID`、`project.creation_id == request.CreationID`、`creation.initialization_key == request.InitializationKey`。Creation 原 owner 必须等于当前 Project owner；completed 历史快照的 owner 也必须等于该原 owner。
5. 按 §3 校验全部所需本域事实。只返回 nil/error，不产出可缓存 grant、receipt、计划或状态副本，不调用 Account Session、Activity、Audit、Outbox、Skills、Object、ProcessGuard 或生命周期参与者。

不改变 Human Owner 读/写的当前 Session gate。该服务口不是 Human 凭据；之后任何 Human API 仍走其原当前授权。新口不确认 caller 的业务事务已提交；nil 只对当前活 Tx/持锁期间成立，不得带出 callback 在另一 Tx 使用。

## 3. 精确事实表与拒绝规则

[当前 schema](../../../db/migrations/00013_project_owner.sql)、[scanner](../../../internal/central/project/repository.go)、[原 writer](../../../internal/central/project/creation.go) 是字段依据；不新建状态、迁移或编造 Skills 完成。所有行本身的 UUIDv7、版本、时间、名称/描述、枚举、安全原因、key/digest 均沿原验证；读取失败或字段损坏为 `DependencyUnavailable`。

共同关系：Creation 与 Project 双向指针/请求精确对应；Creation owner 与当前 Project owner 一致；两表均为合法本域事实。`protected_skill_id` 与 `protected_revision` 必须同时存在或同时为空，不能利用原 scanner 的“二者同时存在”布尔表达式放过单边残值。允许初始化观察不需要创建人的 Session 仍活跃，也不授予当前 Owner 之外的人类访问。

| Creation.state | 当前 Project 必要事实 | Creation 必要事实 | 本 gate |
| --- | --- | --- | --- |
| accepted | active；未 initialized；version=1；无 Sprint、无 lifecycle operation | 两个原 request 字段均非 NULL、合法且等于当前 name/description；safe_reason 为空；无保护 Skill/revision、无 safe_result | 一致则 nil，仅原事实观察/失败收敛上下文 |
| initializing | 同 accepted 的 Project 条件 | 两原 request 字段合法且匹配；safe_reason 可为空或原闭集合法原因；无保护 Skill/revision、无 safe_result | 一致则 nil；不等于新写或成功确认许可 |
| failed | 同 accepted 的 Project 条件 | 两原 request 字段合法且匹配；safe_reason 必须为原闭集原因；无保护 Skill/revision、无 safe_result | 一致则 nil；不能据此重试 Reserve/Send/Publish |
| completed | active 且 initialized；当前 ProjectRef 合法，允许后来的改名、描述和 version 变化 | 原 request 字段均 NULL；safe_reason 为空；保护 Skill UUIDv7/revision 合法；safe_result 是合法初始 ProjectRef，ID/owner 与该 Creation/Project 对应、lifecycle=active、version=1、无 Sprint/ArchivedAt | 一致则 nil，仅原初始化元数据收敛上下文；不再写 initialized |

completed 的 safe_result 是历史创建完成快照；**不与当前 name/description/version 强等**，不能当成当前 Owner 授权或凭它覆盖当前行。新口只校验 Project 所有的保护 Skill/revision 标识和历史快照形状，不查 Skills 私表，也不证明那些标识对应真实可用包。创建事件/Audit/work_claims 的事实校验仍属于原写流程；本口不以 event JSON、process/fence 或 Session 造新的授权条件或完整性结论。

**active-only 已经完整规格独立 STATIC 与 root 采纳，属于工程收敛边界，不是新增用户产品决定。** 依据：D10 §2 的 failed/accepted 恢复处于原 active 未初始化 Creation；已接受成功 gate 也只认 active；D10 §10 将 deleting cleanup 明确交原 LifecycleCause。对 completed 后的 archiving/archived/deleting，本口没有打开初始化服务通道的必要性，统一拒绝；Archive/Delete 停止与清理须另走当前 lifecycle cause/phase/participant gate。不得用这个限制改写普通 Owner 对 archived 的 Read 规则。恢复 active 后仍可按原 Creation 精确事实观察，不能沿旧 lifecycle cause 偷渡。

| 条件 | 安全返回 | 说明 |
| --- | --- | --- |
| nil Authority；无效请求/actor/Tx | 分别 Unbound / InvalidArgument | 不读库 |
| actor 角色/scope/cause 不符 | Forbidden | 不读事实 |
| 缺 Project EX、SH、foreign/expired Tx、SQL/Scan 错误 | DependencyUnavailable | 原 Store 原因保留于内部 Fault；不吞 poison |
| 某目标行不存在；请求 key 或合法目标配对不符 | Forbidden | 没有观察上下文，不返回任何候选数据 |
| 当前 lifecycle 非 active | Forbidden | 生命周期通道不能被初始化服务借用 |
| 行投影自身损坏、两 owner 矛盾、双向 canonical 内部矛盾、状态与 initialized/保护字段/历史快照不一致 | DependencyUnavailable | 不把损坏当成不存在或 ready |

请求选中了两个各自合法但不对应的 Project/Creation，按调用方配对错误 Forbidden；只有已确认原 Creation 归属该 Project 后，当前 Project 反向指向另一 Creation 或 owner 不同，才归 canonical 内部矛盾。必须覆盖这一区分，不能靠 SQL WHERE 吞掉不一致字段。未知 state 被原 scanner 拒绝，不新增宽松 default。所有失败返回零可复用许可。

原成功 gate 的 accepted/failed 仍 `InvalidState`，initializing/completed 在原条件满足时仍可成功；本卡不能扩大它。未来失败重试的写操作须先由 D08 原 claim 流程进入 initializing，再走原 gate 与 Skills 本域事实/写权限，不能在本接口中 UPDATE creation 或初始化记录。

## 4. 事务、取消、幂等与副作用

本方法不拥有事务、不执行 Commit/Rollback、不建立异步任务、重试循环、WithoutCancel 或事后查证。只读两行并保留调用方取消/截止传播；不会产生独立 `CommitResult` 或新的 UnknownAttempt。调用者最终事务若 Unknown，仍按该调用者既有规则处理，不能把本方法返回 nil 视为已提交收敛。

调用方先按全局锁顺序取得全部所需锁，再调用此口。它只要求 Project EX，不额外新增 User、Command、Object、Skill 锁，也不证明调用方本域锁已齐。消费者未来需要的原 attempt/SkillRevision 锁必须自己证明，任何新写或 Audit/cleanup 仍必须走相应正式端口及本域 facts gate。

同 Tx 重复检查一致事实可重复 nil，不登记 idempotency receipt；只有当前 key 精确匹配。所有成功/拒绝/取消均不得改变 Project/Creation/command/work_claim/Audit/Outbox/Account Activity；不修改 initialized_at、不触碰 name reservation。真实锁等待发生在 caller 的 Acquire；本 gate 的 RequireHeldLocks 不等待获取新锁。纯投影结果不能冒充原 writer/process 已 join。

## 5. 精确实施白名单与后继交接

本卡限定以下 **5 个新技术源 + 1 个后授文档末件**；固定基线五新路径均不存在。实施权尚未授，正式接手须重核最新已接受增量和同包写者。无迁移编号/SQL schema变化、依赖锁文件或现有共享 helper 改权。

| # | 路径 | 最小责任 |
| --- | --- | --- |
| 1 | internal/central/project/contract/initialization_convergence.go | 独立可选接口及严格用途注释，不新增 DTO/JSON |
| 2 | internal/central/project/initialization_convergence.go | `*Authority` 方法、局部事实验证、编译期接口断言 |
| 3 | internal/central/project/initialization_convergence_test.go | 有界纯受控顺序、拒绝、零副作用、旧 gate 对照 |
| 4 | tests/project/initialization_convergence_fixture_test.go | 本卡私有真 PG/Store/注册服务 fixture；原 helper 不改 |
| 5 | tests/project/initialization_convergence_test.go | 真 Tx/heldEX/四状态/损坏事实/旧 gate 兼容与锁存续代表 |
| 6 | docs/development/backend/README.md | 完整技术接受后 root 单独授窄末件，准确记录库可用和未绑定 |

不写 authority.go/repository.go/creation.go/recovery.go/原 contract 文件、app/account/project_usage、安全根、Object、Skills、HTTP/OpenAPI/UI、迁移或旧 tests/helpers。当前 UI 工作不属于依赖，不读其活动 Go/JS。若新增方法确需改既有共享实现，必须报具体缺口，不能默认扩白名单。

此卡完成后仍不能开 Project 创建 HTTP。后继 D10 必须分别补真实 Skills 持久初始化、ProjectInitialization+SkillRevision 专用 Owner gate、初始化 Object Audit routing、真实 Reserve/Publish/观察/失败收敛与生命周期参与者，且 Object Runtime 停止缺陷未获恢复授权前不可接入真发布与共享 guard 根。旧 D10 状态的过时段落由 root 后续明确修订，本卡不自行改历史主卡。

## 6. 必要验收与执行门禁（后续实施时）

**当前只规格，以下不是已执行结果。** 普通/race测试须验证可观察契约，不给纯 interface 再造镜像测试。先冻结全部实际 GoFiles/TestGoFiles/embed/生成 testmain/fixture动态命令图及运行时读文件，复用已接受最小闭包+差量，不复制全树；不能用静态目录猜 actual graph。停止被执行输入写入后才运行，活跃 UI 包必须在实际图之外或由 root 安排冻结/隔离。

离线 Go1.27.1、offline/modreadonly/-p1；每命令 45s，test-c 与 binary run 分开，纯 binary 内40s。有 subreaper、direct/adopted actual wait、两次 owned 空与前后输入 hash；原失败保留，修订只重跑影响范围。必要普通/race `project`、`project/contract`、race vet，integration race compile/list，两 cmd (`cmd/agenteam`、`cmd/agenteam-runner`) build；全 app 测试不得冒充 pure。不运行 fixture 或监听直至 root 另授资源。

建议新纯 top：`TestInitializationConvergenceAuthorityContract`、`TestInitializationConvergenceAuthorityFacts`。覆盖三 request 字段逐一无效/错配、零值/合法非匹配 actor（Human/Agent/其他service/system/digestcause）、nil/零 Authority、精确调用顺序（Actor→heldEX→sameTx→facts）、各读错误/单边保护字段/损坏 snapshot/四状态表，query外Store/Session/Acquire/WithinTx/Exec调用计数均0。原方法逐状态对照证明 accepted/failed 仍拒，initializing/completed 原规则不变；归属/读错误不泄漏业务内容。

建议新真实 top 两项（精确命名可实现时保持）：

1. `TestProjectInitializationConvergenceFacts`：原 PostgreSQL migrator加载固定连续迁移；真实 `postgres.Store` 与 `project.NewAuthority`（必需 Sessions 绑定同 Store 的正式 `account.NewAuthority`，不是 allow stub；纯测试 keyring，仅服务方法不消费 Human Session）；正式 `RegisterService(ProjectInitialization).Actor(CreationID, InProject(ProjectID))`；四状态合法/互斥反例及其 Project/Creation 原字节快照。completed 后合法改名/version增长仍可观察，历史safe_result保持不变；archived/archiving/deleting 拒绝。测试在本卡私有 fixture 中直接写**本域 Project/Creation 授权输入**，所有行满足正式 schema；不移除约束、不造其他域完成。无法过 CHECK 的 corrupt 值只用 pure scanner 代表，不把 SQL注入失败当方法拒绝。未绑定真实 Skills 时不声称 Create/Account workflow E2E；不调用旧 `assemble/newFixture/skillFixture` 来假装真实初始化。
2. `TestProjectInitializationConvergenceTransactionBoundary`：两独立真实 Store 与同库不同 Tx；无锁、SH、别 Project EX、foreign Store token、已结束 Tx 均拒；忽略 missing-lock 返回仍由原 Store poison 导致事务 NotCommitted。另一普通 holder 对同 Project 的 SH/EX必须真实等待 caller 的 EX，门禁返回后 caller 未Commit仍阻塞，release/Commit 后实际完成；不能仅用睡眠或goroutine是否退出证明锁。取消上下文/已关闭Store拒绝，不开隐含事务。所有持有者都有取消、release 与无条件 actual join；停止不影响非owned backend。

新 fixture 不需要初始化 Skills/ObjectService/Runtime 或调用 MinIO。可复用接受 `scripts/test-objects.sh`/PG fixture 的原完整20包资源驱动和精确 selector（未选中包不计通过），但不得运行停止的 Object Runtime probe；实际 fixture 图、两本地固定镜像digest、资源预算在提资源窗时重新冻结，不写共享 helper。若希望改用更窄 fixture，先由 root 审其完整生命周期，不偷偷跳过正式 Store/PG。

旧回归精确保留两项：`TestProjectB02InitializationRejectsWrongMappingPlanAndActualSkillFacts`、`TestProjectB02InitializationMergesCompleteLockPlanAndPoisonsMissingLock`。只把它们计作原 D08 writer/测试持久Skills adapter 的兼容，明确其旧 Account SQL身份与 `project_fixture.skills` 事实来源；不补称生产 Skills绑定、Object join或真实Owner创建HTTP。新 gate 主验收不消费该假 initializer。

新真实 top 各120s（含 Cleanup）、包6m、race/count1/p1，最多新两项一轮+旧两项一轮；独立验收按风险补集选真 foreignTx/锁poison/状态矛盾代表，不重复整矩阵。fresh≥5GiB、当前 PID/starttime/Docker baseline、原7精确资源与全mount/nonce、实际 wait及资源两次清零、输入一致后才下一轮。任何失败先完成 owned 退役、停止后续，保首红不延预算。daemon/PID1 shim 差集合只记非owned未wait；不复用旧环境PID或声称全机零。若实际 fixture资源拓扑不同，按真实冻结图由 root 重新授窗。

本方法不拥有 Commit，故不为它新增网络ACK丢失测试或声称物理 Unknown 已验；新真实行更新/fixture提交必须按真 CommitResult采纳。独立查最少全部5源差异、正常/反例、真实锁与零写证据；README仅在有限产品结果通过后单独检查。

## 7. 接受状态与后继交接

root 已读并采纳完整独立 STATIC PASS；active-only 与精确事实/错误表已作为工程规格接受，没有新增用户产品问题。正式归位只调整状态与来源定位，末件窄审仍待完成。当前未授权产品实施或执行 Go/真实资源；后续仍由 root 分配五源唯一写权及所需冻结、执行窗口。发现范围外公共依赖或共享源缺口时必须报告，不能默认扩大写权。

未来产品交付五技术源与后授 README 末件，应明确“Project 初始化收敛授权库已验，未被生产 Skills/root 消费”。无默认模型猜测：Meeting Summary 继续由系统管理员统一 initial/update/首标题，项目不复制或覆盖。生产 Resolution、Invocations、D24与完整D08/D10仍未绑定/未完成，ready503与三停止保留。正式规格接受不替代产品实现、作者自测或产品独立验收。

## 8. 固定来源与审查定位

本节仅保存可复核的路径、Git blob 与 SHA-256，不复制源码。产品来源统一固定于 `4089d13128da8680955005d9507c8a7da74af1f1`；后继入口文档变化不回写这些历史指纹。只读按固定 commit:path 可重建源，正式链接用于定位路径，不能以活动工作区字节代替固定输入。

### 规格与独立审查

原件根 `/workspace/scratch/project-create-frontier4089/`：

| 原件 | SHA-256 |
| --- | --- |
| `draft-rev1.md` | `34346798c4e526bf56c08cea24c4fe0ede8f59a73cb1fc8c9bbe222479db4f7b` |
| `inputs01.json`（57 源） | `057d9f57e0e7f2786444fcc685c11cda23557587c7bb9dab7bd45f478176367e` |
| `freeze-rev1.json` | `1edf2e50ddb0e90ceca74e9ff2e21767329d54cb1f8e1f37784ef23c7e288d48` |
| `checks-rev1.json` | `727c29a1c9336f288dd6ca74dc960790186b52fb29bf6faecc03a88ec1fc1cbe` |

独立审查根 `/workspace/scratch/project-initialization-convergence-verification/rev1/`：`review.md` SHA-256 `54a8e0c5d4621ba10608ab7fc4a7a1530b218f8c077785232f2bd5b49d5b954c`；`result.json` SHA-256 `a583df8a99b3a9d492766c0a8cbbbadd6b8f75c52301d5e5ebfc1a0b503d6d61`。结论为完整 scratch rev1 STATIC PASS、无必修，包含 active-only；没有 Go、资源或产品动态执行。后续持久归档由 root 另授，不创建尚不存在的归档链接。

### 停止源同字节核对

回归基线 `42e3f7d59ceb65cb22cdd6b639608caa8830fae2` 与产品基线 `4089d13128da8680955005d9507c8a7da74af1f1` 的下列 blob 相同。原 `stopped-source-check.json` SHA-256 为 `8fac130c6cc3f61388c33e73caf9852ee6277c3b1741549d1d5a2c6310f16639`，位于上述原件根；此次只读核源，没有重跑停止的 regression。

| 路径 | 两基线相同 Git blob |
| --- | --- |
| [internal/central/object/service.go](../../../internal/central/object/service.go) | `400131e491325bbd4f9070ef2165ba629d40631b` |
| [internal/central/object/project_work.go](../../../internal/central/object/project_work.go) | `b7ef0c4030c798847daf89702cd7b7676062f076` |
| [internal/central/object/process.go](../../../internal/central/object/process.go) | `a988d582999d6b3c6d5fba35719f6917a9e8b1b2` |
| [internal/central/object/runtime.go](../../../internal/central/object/runtime.go) | `2c58a97c188f53cfef912f117eddda6437a55fc2` |

### 固定来源

| 路径 | SHA-256 | Git blob |
| --- | --- | --- |
| [AGENTS.md](../../../AGENTS.md) | `3f95c21627394aaa83a7e001fd8da1a610a5c861ed3445f4ab40edadfbd4306b` | `e580d36cf214560e166f05f5b4885b56958d8e08` |
| [docs/development/agent-team/README.md](../agent-team/README.md) | `ec016545fd2c135b1f431788c595dbc1bb4737652434b25c1a2771b8a89c25a2` | `7fe85e18dae8438b482118eeee44eb0dbb8bccc9` |
| [docs/development/development-plan.md](../development-plan.md) | `2db9fe8051f573bb9cf9ff61b935d9bedf45ea4d39e5c94870305e581e03a3ba` | `54ead294b08c8a4f4fe6ae303bae3e9b6b6938f3` |
| [docs/development/agent-team/tasks.md](../agent-team/tasks.md) | `7384f94115cb15a8e2f96b151d062fb34158327bc8b20345169d2954a7e79bb2` | `381c71962ea00aa73b07c07a8dad2b38c9c2ce21` |
| [docs/development/work-items/d08-project-owner-design.md](d08-project-owner-design.md) | `6b4fafa43f9ea3912c72ec40c042e79ba316ae863beb13365fad2b88dafbde8b` | `6ab8e48166d4f0b11bb315d56c771f1a61a4bcc7` |
| [docs/development/work-items/d08-project-owner.md](d08-project-owner.md) | `fc06c6fd328f4cd424e141e58acf696e22b350c8c9cfa0e0a38a85937724b186` | `bdf6d707372c93a8ef2e3708dce65eabe9e99dee` |
| [docs/development/work-items/d10-skills-initialization-design.md](d10-skills-initialization-design.md) | `55f90e0c05549023cda132cf68ce570148909104dd3b9d75d9371f5564b671d3` | `d8dc6ef38523356c3311c3d9b83448a8e12d6bf9` |
| [docs/development/work-items/d10-skills-initialization.md](d10-skills-initialization.md) | `87033b3a37d03568fa47441084d9153c3ce3d2bebf34c5949dd52150d4ea2f6b` | `14788e94d8ba24a42939737a9dc5d55c32da76f0` |
| [internal/central/project/contract/initialization.go](../../../internal/central/project/contract/initialization.go) | `0ed3217342ee10f3f0406f0576b0c63d4cac32c060da3b6c0bb9c62ee80303ee` | `649dbda163b98c5b8b35d42c52cb429bc28944c7` |
| [internal/central/project/service.go](../../../internal/central/project/service.go) | `ee0b6294d901829c0d89f4bdcfb0869031dfbea968ded2d07add070c6282591f` | `68d495375017a1562e2f712e55ce2d96bd77a6b1` |
| [internal/central/skill/contract/types.go](../../../internal/central/skill/contract/types.go) | `64f60865bdacf61b0244f0e880534c7e3884d2900ab826cdcc34fad6de496c33` | `32be32e2941f4edb808d1f79ba683ba00fbc1132` |
| [internal/central/skill/contract/read.go](../../../internal/central/skill/contract/read.go) | `d90dde0edfdaae9ae34dfe3c580f46aa12020def0f38ce6b59a718471e563460` | `8080fec59cf218cc8d6f795b3e66963a4a93ad3a` |
| [internal/central/skill/contract/package.go](../../../internal/central/skill/contract/package.go) | `15360c26f79ec5c5a4ae877eeef367a251afaa1d6240ad88e76d65b05cb11519` | `075ce101317ec50f1b608d08f82c966ad5f5da78` |
| [internal/central/skill/builtin.go](../../../internal/central/skill/builtin.go) | `07f8b9d09e7b525c3b419b88b796a4e1d7dd7a950baf10737a00d717c08cfdde` | `76b12fc070330b405ed3f465fe56d5c8e0c588c6` |
| [internal/central/skill/package.go](../../../internal/central/skill/package.go) | `a530b35597feb11f317dc76df3264d4b50239a0c8f92f7962c1bef25a656f34c` | `c0cdb715fa5c8e8e676be9eaf7b38b172f8e234e` |
| [docs/architecture/agent-skills.md](../../architecture/agent-skills.md) | `ee7e90006f129e2b4486b50c166b41f687fe8448bc59dc6c06fa95ef2c094275` | `f38fd98ad85729230ff6a5f938d77bae5b761220` |
| [docs/development/agent-team/d08-r1-verification.md](../agent-team/d08-r1-verification.md) | `06e32358cfff87fc23262b5b8bcc7ac438591bab76eda571f90d75d018153ba5` | `c6475b8a396f52f03cefd233fe85c8253006b283` |
| [docs/development/agent-team/d08-r2-verification.md](../agent-team/d08-r2-verification.md) | `dd66faf9129bbd64453ba35a4b8ef64500f61c3e03972d30a4e63a74d4fb23f9` | `9eb69a2be8d6fb35bcb1b19c489477003c84844e` |
| [docs/development/agent-team/d08-r3-verification.md](../agent-team/d08-r3-verification.md) | `c58e450b70fab5761c7c2a81a57530c0bf97a83bb82fc742c3d45aac76d08fc8` | `b4f1f0621cd802ee5b2e1c9da73e168553f828c3` |
| [docs/development/agent-team/d10-p1-recovery-verification.md](../agent-team/d10-p1-recovery-verification.md) | `e0fff9717e5ace0ec3c300206073d29312fc823a2d90b39cd779803e1fc40960` | `c9d184274cb7b067ba73a6fd8122aea3023871ee` |
| [docs/development/agent-team/object-project-audit-verification.md](../agent-team/object-project-audit-verification.md) | `c0ffc8d6edfe86eb4c51a10d0beec8c8031e4e8f984c1f5c99e2f29ab44d8833` | `da8aa454ec39b1e43dcf755e34ac49ff9598c44b` |
| [docs/development/agent-team/object-runtime-join-regression.md](../agent-team/object-runtime-join-regression.md) | `44e3404084fc25174576f2d0f319ad067b3a4b079c5876db8d20b79a3894645f` | `27cc0f1ebf59e44c79c885d6b91f8f288cabf2dd` |
| [docs/development/work-items/recovery-object-project-audit.md](recovery-object-project-audit.md) | `3751359f7019b05c4ffc18fdc551bf8581c37d0ec05b10937f88c5970d954546` | `d8f76e6f86a01bd5a7b8783ec4f4e27410ac5b98` |
| [internal/central/object/contract/authority.go](../../../internal/central/object/contract/authority.go) | `96e2e54596e7d9302a5733e3eeee746eaae5434b718d905703ca36055534fdfe` | `02c369ccab4f55603f30f54f07912c6e3fc21c81` |
| [internal/central/object/project_audit.go](../../../internal/central/object/project_audit.go) | `ad3db6970c5806c199976b08b137f5ab5d143a705e90afac67c0a1f7973839ed` | `d53af6b36b36f7d4ef4525f0a96708fc9806ed8d` |
| [internal/central/object/project_audit_witness.go](../../../internal/central/object/project_audit_witness.go) | `33bc4ff7a38bc18e96e20029324c01108598ceea9e8fe9206876f01056b3e262` | `88b445be4d4a1ed4e35a85ec6951c4d4abb382c7` |
| [internal/central/object/transfer_upload.go](../../../internal/central/object/transfer_upload.go) | `45ca4fdb9f2013067cfe9cdd4a5704e928b2ca26933f4eea9c330f3a6e37d079` | `3a8e68909f7c40019e61a13eb6e66d848a9db768` |
| [internal/central/project/authority.go](../../../internal/central/project/authority.go) | `a9e7d5768a18c23abf9a1cf86a396bbe6901d348c70b16c48b32fa559fa95321` | `c8af518a1a99084f07f56633d1ec3e3afe774af4` |
| [internal/central/project/audit_authority.go](../../../internal/central/project/audit_authority.go) | `3736a441bb6d49b48384069a1c0c014bd4df9278dd3b9b1713f2772fa4066a9b` | `fa2ad4a2cfb1bcca1d0432fde0c10679b25b6ec6` |
| [internal/central/project/creation.go](../../../internal/central/project/creation.go) | `0867dda75a498741ab141e356c953474a567071145df6daf4efafd3ac2bb1a25` | `3d5ab22e04287cad5d71473e3152fa8b7bce4ee5` |
| [internal/central/project/recovery.go](../../../internal/central/project/recovery.go) | `7149c163463416c6be4ea43351c375208b6e61278a6f170deeb302964cd29834` | `cd3df065366b74b5ded53c6fd3275c85a71bfcc1` |
| [internal/central/project/participants.go](../../../internal/central/project/participants.go) | `b38ebb0b2cfa0ae42e969bd993932808fdb20f7a5f4297375f27d5f6a74455ed` | `1c9fe964ae26c918a1b09ce631b7e5b015d40ed4` |
| [internal/central/app/project_update.go](../../../internal/central/app/project_update.go) | `98f55d15d6799d98c335680b5ef6ddd840597a78e201ca3de3c6f1601baa96fa` | `99fb2d77db62b441cc3fa4bf7e911280b145d28d` |
| [internal/central/app/project_usage.go](../../../internal/central/app/project_usage.go) | `13abb6bbdc4d38d40b1ba627aa989aec503ffe55c7a310dd42d11acc33d81275` | `3463ae43544a68548924b7faf513ab0cfffe490a` |
| [internal/central/app/account.go](../../../internal/central/app/account.go) | `c8afd6286fccef135f53667a66f8522553a77cc187fa9f32a2813ef1e926eb98` | `28615e47090f312603d729299d9069347fe9836d` |
| [internal/central/project/repository.go](../../../internal/central/project/repository.go) | `f89d5b582b79cb31434d70c03be43a99ac1b66261591850d141263c2af44ed55` | `e67938516ca40b87febce02b59608a4f678f5084` |
| [internal/central/project/contract/commands.go](../../../internal/central/project/contract/commands.go) | `30af1efac3ed5230ac06b1795e086fcf21840246243e293bcd46bbea335886fa` | `f47abca3975d0e4c0f95172eaab7c03bd2386be8` |
| [internal/central/project/contract/types.go](../../../internal/central/project/contract/types.go) | `7465815c5da4e1bf24b6958d1aa1c93959ffece4256883bd343199c3ed9cfbf1` | `317bf76d8f392be9b798b2b3d00c30ed9ff6edae` |
| [internal/central/project/contract/validation.go](../../../internal/central/project/contract/validation.go) | `73db8aeb4db685433279cdeeb119fe12a231d7193d9b93e97305b7f30445c64b` | `4ad29bd8cc99b46af8c640dcc925d33b02575754` |
| [internal/central/project/authority_test.go](../../../internal/central/project/authority_test.go) | `1716e35f980079f94ac25ad8233127898bfd30092b92090a6605cf3b589ba2a2` | `39bc43b6d1512be82ad0a044bc07ca2c5e69f979` |
| [docs/development/agent-team/d08-b02-recovery-baseline.md](../agent-team/d08-b02-recovery-baseline.md) | `01258a5f954a452e5dc0d74138da16a75addb2f9a9c920fd7f694d1292a965a2` | `629dd3a9b6f5e262073dd973eed4101dba5c9fa3` |
| [db/migrations/00013_project_owner.sql](../../../db/migrations/00013_project_owner.sql) | `6ec8c84b7fcedd538550ece134c2df3e35febfe13e4906c74aa91dd1fe878cc0` | `a85d049d4ed10b709eb68bc7465efa26129330bf` |
| [internal/central/postgres/transaction.go](../../../internal/central/postgres/transaction.go) | `aae85b4fcf7411e31dfedd7e125c4d0aa6119b41d7071ee1787e088c14cc83f3` | `2a2fc435b4edb4f56f278ee5f9135a3a561737e7` |
| [internal/central/identity/contract/identity.go](../../../internal/central/identity/contract/identity.go) | `35403f72515c25c7950e0c199e73ce5b18e0f81f3363bb9dfb200f795ac8e8b0` | `fd13667713bf1248111aa1f18687296be9c3cde0` |
| [internal/central/identity/contract/identity_test.go](../../../internal/central/identity/contract/identity_test.go) | `14a778eb74d859062a0f7208f06d62a954a14feb78c2fed3cb8513354912d15c` | `d3cb311756df378385d8b5497c4a66c46865255c` |
| [internal/central/project/contract/initialization_test.go](../../../internal/central/project/contract/initialization_test.go) | `1457ad4b7a067c334731643a6495966fa7bb6a9c010b450084984fab49fc2ddd` | `9f06535f19089f9fe22ea927ba913c5d0bdeed09` |
| [internal/central/project/events.go](../../../internal/central/project/events.go) | `4f97a16b6b9176f58c5ce05d22253664d602556a066a7c2ecec5f0995398d52d` | `042897b699c0b029160e1d7fca1b324593c0690a` |
| [tests/project/b02_fixture_test.go](../../../tests/project/b02_fixture_test.go) | `6687003d2f1700f336c365477237e413580fb5d217e6a6754733d4326434df94` | `1176191fdce702042bff77867a131575d2ec00e0` |
| [tests/project/b02_owner_commands_test.go](../../../tests/project/b02_owner_commands_test.go) | `5d4b886c58ae4b286da9e79e5470688db953091e54905badb0ea5993064b7b35` | `fb505896e709b9cae0c58755393d7043ce1431fa` |
| [tests/project/b03_r3_fixture_test.go](../../../tests/project/b03_r3_fixture_test.go) | `08c648c44071db30f8635da8b526ca4f49858b240565045af3b23ee46c6273c8` | `83b4fd8fff8334027db8e4bfa6ce213d7a445feb` |
| [tests/project/b03_r3_authority_test.go](../../../tests/project/b03_r3_authority_test.go) | `3c17e5dc6a64802b0c45f5f8e64f541dfa1545a04584247d8aca013d02f3fae7` | `ea561303437de368f0d962e2022f2a0ec2b6000e` |
| [tests/testsupport/postgres/fixture.go](../../../tests/testsupport/postgres/fixture.go) | `448c63e2986b823ac15ccda25b75f4c8ebc28e461b3c6dbfcbb49ae24bd89437` | `88bd505e180becb3978e52dc1aded63d2a9b3810` |
| [tests/testsupport/postgres/cmd/fixture/main.go](../../../tests/testsupport/postgres/cmd/fixture/main.go) | `55ae7da25e69c4f4f29ca516748d8cc837913016e9fc381fc96abc11f37298de` | `6502f79dc0c1db543178e049fc6c1eeee8bfd3b1` |
| [scripts/test-postgres.sh](../../../scripts/test-postgres.sh) | `eddfb4f44288f8ec448400b02435f5fd6d6c44b0d80003fd34f7785e2980cdfe` | `ec4a47d9797ceabef2eb173dfd1099369da65f7a` |
| [scripts/test-objects.sh](../../../scripts/test-objects.sh) | `71aff74a122d5935ec02d8b80fc27be0a7cb3020650ac40dc1c0d48b9d012d87` | `12ab48fcf0cd9c9cb8b2e9a2f9dfff27fa2720a7` |
| [.agents/skills/agenteam-design/SKILL.md](../../../.agents/skills/agenteam-design/SKILL.md) | `81834a981f226cc663c42b9dcdfd9b56001d2cfcd649107064bf865af4d61b81` | `dc06833821245cc578b46febc741c5740b3ec7f0` |
| [internal/central/account/authority.go](../../../internal/central/account/authority.go) | `4ee6365489ff21bfd629313b658f46f03495fe38a27e640ed4897a43547f14d5` | `d353cc3e0d0d4a249ec6290fb50c107a63b40967` |
