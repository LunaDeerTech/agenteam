# D11 首张 Milestone/Sprint 结构结果：有界可行性

**建议进入正式 SPEC：Human Owner 的 Milestone/Sprint 结构事实与命令库；当前不是实施 READY。** 交付真实层级持久化、读取/分页、创建、title/description 更新、人工顺序及其版本/幂等/事务行为。首卡不提供 Sprint start/complete/rollover、Task canonical 或破坏性删除的正向完成。它是可独立验收的结构结果，不声称整个结构 CRUD、D11 或生产 Work 已完成。

计划 `development-plan.md:308–316` 明定“结构→Task canonical/查询/rank→状态→Blocker→Sprint lifecycle→TaskEvent/context”。虽然 Sprint `start` 明确允许空 Sprint（sprint-lifecycle:194–214），本建议仍将其留在计划的 lifecycle 卡，不用它绕过顺序。不存在必须先恢复 Object/tools/SPA 或下载未知 SDK 才能实现此结构库的逻辑依赖。

## 1. 范围与事实所有者

- Milestone 属 Project，无 lifecycle；Sprint 属同 Project 的一个 Milestone；Project 内 Milestone、同 Milestone 内 Sprint 各有 manual_rank。依据 task-domain-model:32–86。结构卡必须自己建真实 Milestone/Sprint canonical 表、同项目父子约束、排序组及命令结果事实；不能用fixture或调用方提供的parent关系代替。
- 首卡只创建 planned Sprint；结构读取正确投影既定 lifecycle；update只允许planned/current，completed只读（sprint-lifecycle:46–130、606–669）。不新增Sprint.status，不把改milestone_id塞入普通update，不提供跨Milestone搬Sprint。
- Human 的当前Session、Owner、initialized和Project生命周期门禁可复用现有真实Authority。读取可按既有gate访问归档项目，新业务写仅active；管理员没有他人Project旁路。AgentRun 当前由 CheckOwnerActorKind 明确返回 DependencyUnbound，Service不能自报Owner；首卡不授Agent/Tool执行入口，因此不消费尚未完成的D10 Agent配置/Skill/Execution授权。
- 稳定Project/Sprint身份已存在：00013:12和project/contract/types.go:70–90已含current_sprint_id/typed SprintID。首卡不改变此pointer；后继lifecycle才需要Project-owned同Tx pointer写口与Work真实Sprint事实。不要重加字段，也不让Work直接UPDATE Project表。
- 普通结构业务事实可定义最小typed Event及同Tx Outbox；精确producer/event type/payload必须在SPEC冻结。D01 runtime-events:55允许领域定义必要事实，不要求每个内部函数发事件。结构变化不是TaskEvent或Execution日志，不能用Audit代替。普通结构操作未发现必须新增Work Audit producer的已定要求；本最小范围不预授Audit闭集/迁移/UI修改。后继Sprint lifecycle的安全审计另按其§20落实。

## 2. 直接及传递依赖

| 依赖 | 可复核已验依据 / 当前真实接口 | 本卡处理 |
| --- | --- | --- |
| PG/Tx/全局锁与迁移 | D03主卡rev1已完成；foundation/lock.go:89–137有User/Project/ProjectSchedule/RankGroup/SprintAggregate，Store有同Store活Tx/AcquireAll/RequireHeldLocks | 复用；一张新增迁移编号由root另分配。不开新DB入口或运行资源 |
| 当前Human/Project gate | D08 B01 `199554b`、B02 `6319d03`；B02正式接受报告SHA9ab034850a899ec58a9099e09f5b98f8d1c412bfc9172ed736f32e81c4e4f228，主卡:68–74；当前Authority:64–106实际检查User+Project持锁、RequireCurrentSession、Owner、initialized/lifecycle | 精确复用已验子能力，非D08/D10整体完成；实际接口为RequireOwnerInTx(ctx,tx,actor,project,intent) |
| Outbox同Tx持久Append | D06最终接受主卡:170–176，报告SHA3a9d1b7c1df941086d56ce9bcb00411007ba8c3f974c25e099fb9857f8998626；Appender为PrepareAppend/AppendEventInTx（contract/append.go:59–62） | 同卡新Work producer、真实命令/canonical fact验证、注册catalog与同Tx append；不新增dispatcher或消费者 |
| Project对Work事件的许可 | 当前events.go:289–348仅Project/Model；external_event_authority.go:23–26只model.configuration_changed，尚无Work准入 | **未满足，但可在同卡精确补齐**：Project-owned闭集Work gate，不能把已有generic接口当已绑定 |
| Project初始化正向fixture | D08 B02:74明确已有隔离initializer验证，生产Create缺D10仍拒绝 | 真实Account/Session/Project服务+明确标注的已验隔离initializer可以提供持久initialized Project；不冒真实Skills或创建HTTP成功 |
| D10/Agent事实 | 模块表D11整体依赖D10；计划:177允许只消费已验子能力。现Project contract/lifecycle.go:54–67对AgentRun明确Unbound | 此首卡没有assignee/Agent能力/Skill调用；保未绑，Task及Agent入口后继另验 |
| Task membership、Execution/Dispatch | 只存在D01要求，未看到Work包或Work canonical迁移；WorkOccupancy/PendingDispatchReader是后继接口契约 | 不作为首卡创建/字段更新/排序的隐含空事实；所有依赖它们的能力仍未绑定 |

以上是依赖能力与当前接口定位，不是“现源码与旧验收全部字节相同”的声明。正式SPEC/实施仍需冻结实际最小输入。`agenteam_project.work_claims`仅creation/lifecycle进程claim（00013:83–88），Object/Artifact的project_work也不是Task membership；不能按名字借用来证明Sprint无Task。

## 3. 删除/占用边界与循环处理

**Sprint删除正向必须等Task canonical真实membership。** 既定条件是planned且no Task，current/completed禁止删除（sprint-lifecycle:630–669）。首次正向删除必须由Work在同一个调用方Tx、Project gate/调度锁下查询自己拥有的canonical Task行；不能信客户端task_ids/count、缓存、只查当前执行，或因为尚无Task实现就返回empty。若结构服务提前保留DeleteSprint签名，未绑定Task membership时明确DependencyUnbound；此负向封闭行为可验，但不能计成删除正向或完整CRUD验收。首卡不另建一个永远为空的membership registry充当Task Source of Truth。

Milestone删除、Sprint换Milestone：本次所读正式依据只限定“草稿/无引用、不递归删历史”（D01 domain-lifecycle:151），未给完整Milestone删除、移子Sprint语义。首卡不增加这些动作；如后续要求提供，再明确其范围/历史保护规则，不能自行决定cascade或迁走Task。这是扩大范围时的语义缺口，不阻断上述首卡范围，也没有现在必须向用户重新问产品的问题。

Execution与Dispatch端口已固定：WorkOccupancy.ReadInTx、PendingDispatchReader.ReadInTx返回active/pending及history_task_ids（D01 domain-lifecycle:65–95）。task_ids由Work在持Project gate+project-schedule锁的同Tx查询；Task创建/移动/Launch/Dispatch均取同一锁防phantom。nil、typed-nil、读失败不得投影empty，waiting/取消未join仍占用。D22/D23负责真实适配与DB竞争；Task删除还需“没有任何历史”，不等于“现在空闲”。首卡不调用它们完成start/complete/move，也不做生产允许stub。

因此没有不可打破循环：结构真实表/placement只读口先固定 → Task canonical拥有真实membership并接删除保护 → Sprint lifecycle调用真实Task迁移/TaskEvent及占用端口 → D22/D23再完成实际执行/调度绑定。D01允许在后继库阶段做明确的契约fixture，但不允许生产未绑成功。若root要求第一卡就声明Sprint删除和Complete全通过，必须同时带入Task canonical/TaskEvent/占用绑定，便不再是本计划第一结构卡；建议保顺序，显式延后相应正向验收。

## 4. SPEC前需同步冻结的工程契约

1. 新结构DTO/命令/query/receipt和ID唯一归属；SprintID复用现有project contract的marker，不定义另一种不能互换的Sprint身份。title/description限额、Milestone version、排序token/cursor generation、重排与纯rebalance的版本/事件规则需明确。Task已有rebalance规则不能未经说明自动外推Milestone/Sprint。上述是工程规格要定型的内容，当前不虚填值。
2. 当前授权先于历史结果读取；同义幂等结果先于expected_version。命令作用域、完整semantic捕获、同key异义、错误优先序、COMMIT Unknown按原writer串行确认及取消边界必须冻结，不能把网络request_id当command key。
3. 同Tx父子存在/归属/并发创建与排序；原命令结果、业务version及所选typed事件原子提交。Update只写明确字段，不能写回旧parent/rank。completed的普通修改一律拒绝；rank维护与历史只读边界须在SPEC明确。
4. 最小建议采用短事务Project gate EX串行结构mutation，加现有规范化RankGroup/Sprint锁（必要时project-schedule）；所有锁在首次AcquireAll组成完整union。当前没有MilestoneAggregate，粗粒度Project锁可先避免修改全局锁类型。若需细粒度MilestoneAggregate，必须另给D01+foundation锁顺序增量，不默许任意lock名称/次序。
5. Work producer与Project gate必须各自校验完整Actor/Event/项目/版本/阶段。复用Outbox opaque issuer、CurrentAccess/NewFact、同Store/活Tx/持锁校验；不能把Model白名单改成任意producer通行，也不由Work伪造ProjectAuthority grant。建议新增Project-owned work_event_authority.go，仅events.go的两个入口显式分派新闭集。
6. 后继Task placement/membership接口应与结构规范同次冻结其owner、Tx、锁与错误，但本卡只实现真实结构placement；Task reader未实现就Unbound。生产root、Project Work生命周期participant/清理、Task/Execution/Scheduler/消费者继续未绑；不能注册一个只清两张结构表的“Work已全部清理”实现。首卡只库层隔离组合，不改变ready503。

## 5. 最小可能写域与验收边界

候选18路径（17技术＋README），是后继SPEC讨论输入，**不是授权白名单**：

- 新 `internal/central/work/contract/structure.go`、`structure_test.go`、`events.go`、`events_test.go`。
- 新 `internal/central/work/service.go`、`structure.go`、`rank.go`、`reader.go`、`repository.go`、`events.go`、`structure_test.go`。
- 新 `internal/central/project/work_event_authority.go`；窄改既有 `internal/central/project/events.go`。
- 新 `db/migrations/<root-reserved>_work_structure.sql`，不改00013/旧迁移；schema约束归本域，不用跨域CASCADE替代生命周期清理。
- 新 `tests/work/fixture_test.go`、`structure_test.go`、`structure_concurrency_test.go`。
- `docs/development/backend/README.md`技术验收后末件。

这与当前Model Settings前端及其两新account Go测试不共享产品源。共享点仅Project事件分派、全局迁移编号/嵌入构建输入、Go/cache/PG fixture及最后backend README；root必须逐项独占/串行。App/HTTP/OpenAPI/前端、Audit闭集、go.mod/sum、公共测试脚本与Object runtime都不在建议最小写域。若SPEC决定必须加安全Audit或公共新锁类型，需精确扩口及兼容验收，不能沿此列表预授。

验收至少覆盖：纯DTO/非法Unicode/版本/排序边界；真实持久父子同Project与跨Project拒绝；当前Owner/Session撤销、archived/deleting/未初始化gate；同key重放/异义/过期expected及COMMIT Unknown；结构/命令/Outbox原子回滚、完整issuer/锁/Tx错误；同父并发创建/更新/重排与稳定分页；completed只读；缺Task/占用端口明确Unbound。后继真实Task插入与Sprint删除竞争、Complete/Dispatch竞争不在首卡PASS中，必须由对应后继卡完成。正向fixture只证明结构库及已声明接口组合，不冒Task/Skills/生产root绑定；新卡独立审查与真实PG窗口均待另授，本轮未运行任何检查。

**下一动作：** root若采纳，先派正式“D11结构事实与命令库”SPEC，冻结上述六组契约、最小事件闭集/Project共享扩口和精确路径；并在卡中登记Task membership/删除、Work清理、Sprint lifecycle、D22/D23适配的后继验收责任。若希望首卡扩大到破坏性操作，先补所述具体语义，不通过假empty消除依赖。本次只scratch分析，三停止/Jina及两Image BLOCKED原件保持；无业务/正式卡/Go/Node/网络/资源/Git执行。STOP。
