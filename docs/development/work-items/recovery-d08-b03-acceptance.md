# D08 B03-R2：归档与删除的持久接受及结果确认

> 接续说明：本卡对应已交付阶段，旧模型、层级、实例、冻结输入和授权窗口均为当时记录。当前目标与阻塞见[任务台账](../agent-team/tasks.md)，新的调度、验证与提交遵循[团队流程](../agent-team/README.md)；以下业务契约和验收事实保留。

修订：rev2，仅同步交付状态，冻结 API 与行为规则不变。状态：R2 已独立验收通过并获主线程采纳，15源已提交推送 `73db0d45d673f804a37a7232a79a611a1ef447aa`，远端同 SHA。执行为同一 `backend_worker` 接续，独立验收为未参与实现的 `verification_worker`，按团队规则使用 `gpt-6-astra / max`，不得再委派。

## 输入与完整结果

恢复事实基线为 `8872110099c84cf0600bb5b62cdcd6c0c6c843e3`，已提交 Project B01/B02、C0、00013–00015 保留；中断前未提交候选及 `/tmp` 证据不作为本卡通过证据。直接新增依赖为 [B03-R1 注册库](recovery-d08-b03.md)：开工时主线程下发其实际验收提交/精确指纹，本卡不把活动未验源码当成已完成依赖。

本卡交付实际 Service 方法：归档/删除接受、当前 Owner 的进度查询、原命令重放及 COMMIT Unknown 串行确认。使用已提交 `00013_project_owner.sql`，真正持久化 Project gate、operation、冻结 required manifest、participants 与接受回执，并与真实 typed Audit、Outbox Event、首次 Touch 同事务提交。结果是已接受的持久操作；本卡不调用参与者，不推进 stopping/cleaning，不产生 archived 或物理删除终态。

正式行为引用 [D08 设计 §3–6](d08-project-owner-design.md#3-本域持久事实)、[§8](d08-project-owner-design.md#8-生命周期编排)、[§9.3/10](d08-project-owner-design.md#93-outbox)、[§12 T02/T04/T06/T08/T11/T13/T14](d08-project-owner-design.md#12-验收矩阵)及 [D01 生命周期](d01-contracts/domain-lifecycle.md#project-生命周期端口)。首次必读 [AGENTS.md](../../../AGENTS.md)、[团队流程](../agent-team/README.md)、[Go 开发技能](../../../.agents/skills/agenteam-go-development/SKILL.md)；验收者另读[验证技能](../../../.agents/skills/agenteam-verification/SKILL.md)。

## API 与精确文件所有权

实现已有 `project/contract.ProjectService` 中以下方法，不改公共 contract：

```go
func (*Service) BeginArchive(context.Context, identity.Actor,
    foundation.CommandMeta, c.ProjectID) (c.LifecycleOperation, error)
func (*Service) BeginDeleteProject(context.Context, identity.Actor,
    foundation.CommandMeta, c.ProjectID, c.DeleteProjectRequest) (c.LifecycleResult, error)
func (*Service) GetLifecycle(context.Context, identity.Actor,
    c.ProjectID, c.OperationID) (c.LifecycleResult, error)
// 已有 LookupCommand 只增加 archive/delete 两种 command 分派。
```

`project.Dependencies` 唯一新字段为 `LifecycleRegistry *LifecycleRegistry`。nil 保留 B02 既有构造/能力；非 nil 要求是有效 R1 registry。新接受需要有效 registry 的完整当前 manifest/adapter 解析，缺失明确 unbound。已接受命令的当前授权重放、Get/Lookup 不以后来 registry 缺失为由丢失已提交结果。

唯一实现者获以下写权；没有目录通配授权：

| 文件 | 允许范围 |
| --- | --- |
| 新 `internal/central/project/lifecycle.go`、`lifecycle_store.go`、`lifecycle_audit.go`、`lifecycle_events.go` 及各自同名 `_test.go` | 本卡命令/查询、私有本域持久投影与计划、两个接受动作 Audit、接受事件的真实事实校验 |
| 旧 `internal/central/project/service.go` | 增加可选 Registry 依赖及对应构造校验，复用已有 begin/Stop/Drain；不新增 worker |
| 旧 `internal/central/project/commands.go`、`commands_test.go` | archive/delete Lookup 分派及同身份 Unknown 确认接缝、相应回归；Create/Update 行为保持 |
| 旧 `internal/central/project/events.go`、`events_test.go` | lifecycle_changed canonical payload、规划/验证的窄分派及必要私有 eventFact 分支；只处理本卡接受事件 |
| 旧 `internal/central/project/audit_authority.go` | ProjectArchiveAccepted/ProjectDeleteAccepted 分派到本卡同 Tx 事实校验；旧动作保持 |
| 新 `tests/project/b03_r2_fixture_test.go`、`b03_r2_acceptance_test.go`、`b03_r2_unknown_test.go` | 复用 B02 owned fixture、真实 Account/Audit/Outbox/PG 与原 COMMIT proxy；本卡专用装配和测试 |

旧 `repository.go` 的 `loadCommand` 只理解 updatePlan；不得将新 lifecycle plan 塞入该类型或放宽旧验证。新文件用私有生命周期 command/operation loader，全部 SQL 只操作 `agenteam_project` 本域。只读复用 `authority.go` 的 current/Owner/CurrentUserRoutes 和已验 B02 fixture/helper；不改这些旧文件。

R1 两文件继续冻结。不改任何 shared contract、Account/D05/Secret/Audit/Outbox 领域实现、旧 SQL、新迁移、go.mod/sum、app/HTTP、脚本或 fixture driver。额外文件或旧接口改动须先报主线程；本卡、台账和共享说明由各自负责人维护。

## 接受、查询与 Unknown 规则

1. 复用现有 typed Meta、ArchiveDigest/DeleteDigest、CommandIdentity、路径规范化和 DeletionCommandKeyHash。每个物理事务一次完整 AcquireAll，含同 command、User 和 Project 锁，以及正式 Outbox plan 的全部锁；不能锁内补锁。新接受使用 User EX（含首次 Touch、当前 username）与 Project EX。Session/Owner 先于结果可见性和回执；新意图再校验 Project expected_version、当前状态和删除确认。管理员无 Owner 旁路。
2. 归档仅 active→archiving；删除仅 active/archived→deleting。未初始化项目不可接受。原 gate、名称与 active-operation 唯一性按 00013；不能接受第二个未结束操作。接受推进 Project.version，固定该新版本为 LifecycleCause/operation.project_version；operation 初始 accepted、version=1、各 stop_state=required，archive 的 cleanup_state=not_applicable、delete=required。冻结 manifest 与 digest、每参与者原版本均由 R1 输出，缺任何 required binding 不写接受事实。
3. 删除当前完整路径通过已验 `CurrentUserRouteInTx` 加当前 Project 名，在同 User/Project 锁下重读比较；缺 Route 口拒绝，不读 account 表。并发 username/Project 改名使陈旧新接受返回 CONFIRMATION_STALE 或先遇到真实 version 冲突，不按路径查到另一 Project 后转删。已接受同 key 同义重放返回原 operation 的当前安全状态，不以旧 version、旧确认路径后来变化或新 registry 配置重做新意图校验。
4. 可沿已有 B02 两阶段模式，先持久 planned command 和唯一 operation/event IDs、事件 header/payload、原预期版本/确认输入、冻结 manifest，再在 Tx 外 PrepareAppend；只有随后接受事务将 gate、operation、participants、Audit、Event、completed command receipt 和首次 Touch 一起提交才算已接受。规划不得创建 accepted operation、关闭 gate、Touch 或取消工作；planned/in_progress 不能投影为业务成功。重试沿原 plan/ID，不重新取当前 manifest替换；尚未接受的旧plan也必须经当前registry.Resolve(原manifest)精确解析，缺兼容版本拒绝接受。最终事务重新核当前身份、状态、version、路径与原 plan；失败可以保留未接受 plan，但上述接受事实全部回滚。
5. `project.lifecycle_changed` 只使用既有 typed event，payload精确为本次原 operation/from/to/action，aggregate version为接受后的 Project.version。Discover 可从原已存 command plan 规划；CurrentAccess 核当前 Session/Owner及原 plan，NewFact 再核同 Tx 实际 gate/operation/participants 与事件完全一致。不能沿旧普通 Mutate gate 拒绝已进入 archiving/deleting 的合法接受事务，也不能为此放宽其他事件。两个 accepted Audit action 同样核真实 command/cause、当前 Owner、原 operation、版本、typed metadata、ordinal和本次阶段；不接受仅构造合法 Actor/UUID 的直接 Append。
6. GetLifecycle 按当前 Session/Owner 和精确 Project/Operation 读本域事实；DTO和pending refs限量沿既有 contract，解析并校验完整存储 manifest/digest，不能用DTO截断判断全体状态。同 key重放及 Lookup 重新读取原 operation当前安全状态，不返回命令中已经过时的 accepted快照。读取/重放/Lookup不 Touch，不调用 adapter、不推进 checkpoint。
7. GetLifecycle及原 Delete重放/Lookup实现已存在最小 deletion receipt 的只读分支：当前有效 Session、原 Owner、精确 Project/Operation或原 key hash匹配；BeginDelete原 key还比较请求digest。原Owner可读、其他Owner/admin为NOT_FOUND；已删除的 Archive Lookup为RESOURCE_DELETED。没有live Project就不能从旧命令正文恢复，且本卡绝不写 deletion receipt或删除Project。此分支测试使用明确标记的测试拥有receipt前置，不当作物理清理证据。
8. 每次接受/规划 COMMIT Unknown 保留原 cause、Project、key、operation/event identity，沿原 command锁与Project锁等待原writer终局后确认。55P03、确认取消或读失败仍Unknown；不得用无锁缺行判回滚。原规划确未提交才是not_committed；规划存在仍是in_progress。最终接受确未提交可沿同plan重试；接受已提交只返回同一operation。原HTTP取消只结束等待，不删除已接受事实、改key或启动取消。

## 真实依赖与下一块边界

本卡直接消费已验 D03事务/锁/Unknown、D07当前Session/Route/Touch、D04 Project typed Audit、D06真实Outbox append、Project B01/B02/00013和已验R1。生产根仍未绑定Project服务；测试参与者只供 registry装配，所有业务方法应设为调用即失败以证明本卡零停止/清理调用。缺真实D05/runtime时不注册生产占位adapter，不以库测试解锁生产归档或删除。

D05 C0与00014仅提供契约/表；真实 Object/Artifact work登记、项目stop/inspect和清理尚缺，不阻塞本卡接受机制的隔离PG验收，但阻止真实归档完成或delete进入cleaning。Secret/Object Audit fact checker、Outbox stop/cleanup authority、当前cause/phase与ProjectLifecycle service actor也不在本卡实现。

下一块应消费本卡已提交operation，通过真实持久claim/fence和单轮Advance完成 archive停止/完成及Restore/Retry，覆盖失败原phase恢复和原writer终局；不得用SQL造终态替代该块实际推进。只有真实D05/Outbox及其余required adapter绑定并独立验证后，才可宣称实际项目归档可用。delete cleaning/final receipt随后按原完整B03规格推进，模块门槛不因本卡缩减。

## 验收与证据

这是权限、数据与事务高风险块，必须有独立验证。作者先完成纯/race/vet与真实PG适用场景，固定相关源码/依赖、R1验收输入、测试集合及原始日志后停止写入交验收者；保留失败与修复输入，不反复跑到绿。至少观察：

- 真实 Account/Audit/Outbox 组合的archive/delete接受：每项canonical/command/manifest/Audit/Event/Touch单次且同Tx；Audit、Outbox或Touch失败无任何已接受事实；允许存在先前独立已提交的planned行，不能混为原子性失败。
- 当前Owner/另一用户/另一admin/撤销或到期Session、未初始化、旧version、未绑定registry/Route；合法同key重放先于后来依赖/version变化，同key异义拒绝，名称持续占用。完成查询和重放不 Touch。
- 同一项目并发Archive/Delete、同key并发、不同key竞争、rename/username变更与删除确认竞争；接受EX有真实SH阻塞时有限失败，零operation/Audit/Event/Touch和零participant调用。不同Project不受误关gate影响。
- 同一计划/operation/manifest与event IDs跨Service重建和同key重试保持，当前registry新增/缺旧版本不能重写原计划；查询已接受事实不依赖adapter重新可用。缺/坏manifest或digest显式失败，不缩减清单。
- 实际COMMIT代理分别截留规划及最终接受，覆盖晚COMMIT、ROLLBACK和writer仍持锁；55P03时保持Unknown，writer终局后原调用或Lookup确证，零双operation/Event/Audit/Touch。固定预算与精确holder/waiter观察，不用sleep推断或宽松接受多种SQLSTATE。
- 最小receipt只读负例与异义校验；直接伪造accepted Audit/Event因无真实同Tx事实而拒绝；R2 API完全不调用RequestStop/InspectStop/Cleanup。若用测试拥有的已归档前置覆盖archived→deleting，只证明删除接受，不证明归档runtime。B02 Create/Update/Owner/Audit/Event/Unknown受影响回归保持。

命令在仓库根运行，先核Go确为1.27.1，始终GOTOOLCHAIN=local。本树没有 `scripts/test-projects.sh`；沿已有脚本及已包含tests/project的fixture执行，不借本卡改driver：

```sh
GOTOOLCHAIN=local /path/to/go1.27.1/bin/go test -count=1 ./internal/central/project/...
GOTOOLCHAIN=local /path/to/go1.27.1/bin/go test -race -count=1 ./internal/central/project/...
GOTOOLCHAIN=local /path/to/go1.27.1/bin/go vet ./internal/central/project/...
AGENTEAM_GO=/path/to/go1.27.1/bin/go AGENTEAM_MINIO_BINARY=/owned/cache/minio sh scripts/test-objects.sh -run '^TestProjectB03R2'
git diff --check
```

真实资源由主线程/验收负责人明确交接单一owner；复用nonce/label/exact-ID的owned PG/MinIO fixture，不接既存服务。每包原6m与race保持，实际顶层集合冻结后按合理组穷尽执行；其他包no-tests不算兼容通过。已有B02受影响回归集合由验收者基于四旧接缝确定，无迁移变更不重跑无关迁移/对象停止套件；最后核全部命令和owned资源确已清零。

卡片编写仅为只读现状和规格核对；上述实现及Go/PG检查尚未执行。交付必须分别列出接受机制已验证范围与真实停止、恢复runtime、物理清理及HTTP仍未验证范围，不将本卡标成B03或D08整体完成。

## 独立静审记录

2026-10-05，未参与本卡设计及 R1 实现的 `skill_verification` 对冻结 rev1（SHA-256 `80ce099cafdec999bf610be0dd4042bb87bf3f0b7c86517da0a63a628b31b7f7`）与固定 `8872110` 的旧接缝独立静审，通过且无硬阻断。主线程全文核读报告并采纳；本节及状态行仅追加审查事实，不改变上文接口和授权。核对范围包括 00013 容量、Session/Owner 与路径锁、接受事务原子性、原清单恢复、Audit/Event 事实分派、Unknown 原身份串行确认及最小 receipt 权限；未运行 Go/PG/Docker。

实施须特别保留三个已有约束：GetLifecycle/deleting 重放不能走普通 Read gate；旧 B02 `commitStore` 的非 creation 分支只截留 update，R2 必须在本卡新测试文件中精确武装 archive/delete 的真实 COMMIT；最小 receipt 由 typed constructor 从本域行还原，不能对 opaque 类型泛 JSON 解码。原报告位于 `/tmp/agenteam-d08-r2-spec-review-u2lfdanp/report.md`，SHA-256 `cefb5947c1b2d0333e52589b4d4e8cc41c92a9943f4edb65f2f9d26b582f5730`；关键结论已在本节持久记录，不依赖该临时文件续接。

## R2 独立验收与提交

主线程已采纳[独立报告](../agent-team/d08-r2-verification.md)，并以 `73db0d4` 精确提交、推送15源。作者13个新R2顶层按组合通过：首轮唯一 failed-init 测试前置错误保留，修后该1项与15个旧B02顶层通过；unit/race/vet及两个命令构建通过。独立真实race验证4顶层10子例通过（16.972s），两个命令构建通过，相关进程与owned资源清零。实际输入、命令、原失败和分组边界见报告，不称单次无过滤全绿。

本次通过范围仅为归档/删除接受、Get、原命令重放与Unknown确认，不包含stop、cleanup、生命周期终态、HTTP/root或D10绑定。后续[D05规格](recovery-d05-object-stop.md)已静审并提交`7000676`；00016 S1两源由`restore_test_dependencies`实施并独占fixture，尚未验收。R3当前cause/phase Authority卡由设计负责人准备；其实现与正式推进仍按后续卡分别验收。
