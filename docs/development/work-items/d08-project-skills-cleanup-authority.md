# D08 Project 对 Skills 清理的当前授权

状态：按正式 main `29dd4298` 已实现精确分支，作者限定 pure/race/vet 通过；产品与测试获有限离线独审，作者当前事实、Tx与真实锁竞争三个PG矩阵均完整通过；独立PG补集仍待，尚未正式交付。规则来源为已接受的 Skills cleanup rev2 §16.3、§16.8（`ai/skills-initialization` 的 `d10-skills-initialization-design.md`），以及现有 [LifecycleAuthority](recovery-d08-lifecycle-authority.md) 与 [生命周期合同](../../../internal/central/project/contract/lifecycle.go)。

## 结果与范围

补齐现有 `ValidateLifecycleInTx(ctx, tx, actor, cause, SkillsParticipant, CleanupPhase)` 的真实 Project 准入，替代这一精确分支的 `DependencyUnbound`。方法只有 error 返回，不签发跨事务权限，不执行清理、不推进状态。其他 participant 的 CleanupPhase、旧 Stop/Inspect、Outbox/Audit/final 授权保持原行为。无迁移、无新公开接口；不依赖 D05 backend，不表示 Skills 整体清理、生产 registry 或 Project 生命周期已完成。

唯一产品写域为 `internal/central/project/lifecycle_authority.go`；新相邻测试、`tests/project/*skills_cleanup*`、本卡与 current 归作者。`lifecycle_store.go`、contract、旧测试及所有 SQL 只读。复用原已验严格行解码与当前原因核验，不能复制外域表或补调用方漏锁。

## 固定准入与错误

同 Store 的活 caller Tx 已持目标 Project SH（实际消费者可持 EX），才读 Project 私有事实；不 Begin/Acquire/Commit，不调用 participant 或外部 I/O。

1. 仅正式 ProjectLifecycle actor；Project scope、actor Project/CauseRef 与原 OperationID 一致，cause 必须 Delete。当前 Human/Session 不作为后台清理许可。
2. Project 已 initialized、当前 Deleting、current operation pointer 精确；当前 Project version、operation.project_version 与 cause 固定版本一致，operation.owner 为当前 Owner。
3. operation 当前 Cleaning 且 cleanup_stage=domains。Accepted、Stopping、Failed（即使 resume=cleanup）、Completed 均拒；正式 Retry 未重新进入 Cleaning 不得继续。删除 receipt 不授清理权。
4. 冻结 manifest 各项名称/版本/owner module/reference kinds/CleanupAfter 与当前或兼容声明全等；Skills 必须存在，所有 required participant 已 stopped；Skills cleanup_state 只允许 required/pending。
5. Skills 的全部 CleanupAfter 依赖已 completed；outbox/audit/final 阶段不可进入。声明和持久 stop 事实不代替生产参与者实际绑定及工作 join 证明。

缺 binding/兼容版本为 DependencyUnbound；非法形状为 InvalidArgument；错 actor/cause/pointer/固定版本为 Forbidden；不允许的当前阶段或 cleanup 状态为 InvalidState；损坏行、manifest/Owner/gate 矛盾、外 Store/已结束 Tx/漏锁为 DependencyUnavailable。原 Fault 安全投影保持，不输出 SQL 或内部材料。ctx 原样传入；失败不写任何事实，可在新的同锁 Tx 下重新核当前状态。

## 验收与依赖边界

相邻 pure 覆盖精确 dispatch、非法 actor/cause、状态及依赖矩阵；正式 PG 复用原 Account/Project Service、BeginDelete 与真实 Store。进入 Cleaning/参与者完成由明确测试拥有的阶段 fixture 提供，不能声称生产推进已实现。核 required/pending 两阳性、SH/EX、同原 Tx/锁，错 Owner/pointer/版本、未知兼容版本与 manifest 漂移、缺/多 participant、停止未齐、依赖未完成、Failed 尚未 Retry、各不可逆屏障、未 initialized、foreign/ended Tx、漏锁以及原 Stop/Inspect 回归。授权查询无持久变化；取消及返回后的原 Tx 必须实际结束。

先固定 Go1.27.1/local/off，复用原 Work 独占缓存及只读 modcache，进行受影响 pure/race/vet；真实 PG 和候选集成编译另按资源调度，不启动浏览器/Object backend。作者与未参与者结论分列，编译/pure 不替代真实授权矩阵。原 Object Runtime join、E01 等停止项不变。

作者当前检查：固定 Go1.27.1/off/-p1、原缓存，新增两 pure top 与原 LifecycleAuthority 构造兼容共3top/19sub race实际0（89499→0cff7b，1.024s），同包vet实际0（65938→750b69）。三PG测试源码已落盘，包含原SH阻塞Owner writer的实际pg_locks等待判据与两个caller Tx实际返回，离线race候选现已编译并各exact-list恰1，尚未实际运行；不将这些刺激设计记作真实通过。

Skills独立有限接受44702/c18c79：真实公开gate/严格loader/Rows方法配controlled Store共1top4sub race0，覆盖同ctx/Tx/SH、两轮Rows关闭与读取错误拒绝；原Stop/Inspect/Outbox/SQL逆差异不变。PG候选及3single发现、原driver编译已实际0；新增仅三exact入口的RUN/PASS闭集与纯控制待独立窄核，实际仍沿原2资源、105+15/123+3/TCP75，无自动实跑。

当前真实矩阵：CurrentFacts首轮1top28sub全部PASS、业务4.59s，原Go/driver/outer实际Wait0，2资源双退役、private/runtime/desc双清、TCP双empty/input一致，supervisor72.057s（14793→b4694d）。该轮仅本项作者PG结果，Transactions后继结果如下；CurrentFactsRemainLocked后继结果如下，不将有限源码独审或受控initializer/phase/前驱声明当作真实Skills cleanup/生产registry已绑定。

Transactions第2single也已完整PASS：1top4sub、业务1.79s，原Go/driver/outer实际Wait0、2资源/private/runtime/desc/TCP/input全尾齐，supervisor70.969s（86222→c6f0be）。覆盖原SH/EX caller Tx、不代补锁或开Tx、foreign/ended/取消和封闭actor/participant拒绝；第三真实SH阻塞Owner writer矩阵后继结果如下。

CurrentFactsRemainLocked第3single完整PASS：业务1.74s，实际观察原Project SH持有者与Owner writer的精确ExclusiveLock等待，释放后两个原Tx均实际Committed，随后旧Owner/operation不再授权。原Go/driver/outer Wait0、2资源/private/runtime/desc/TCP/input完整尾齐，supervisor71.755s（66964→75bf35）。作者3top32sub按固定输入组合通过；独立PG补集尚待，源码独审及受控Rows控制不能提升为正式交付或真实Skills工作完成。
