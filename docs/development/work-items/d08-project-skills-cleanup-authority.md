# D08 Project 对 Skills 清理的当前授权

状态：精确 Skills CleanupPhase 授权已实现并独立接受，待主线程正式交付。作者 pure/race/vet、三个真实 PG 顶层与独立 PG 风险补集均通过，原命令与资源完整收尾。规则来源为已接受的 [Skills cleanup rev2 §16.3、§16.8](https://github.com/LunaDeerTech/agenteam/blob/1e8b5c67/docs/development/work-items/d10-skills-initialization-design.md)，以及现有 [LifecycleAuthority](recovery-d08-lifecycle-authority.md) 与 [生命周期合同](../../../internal/central/project/contract/lifecycle.go)。

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

已完成的限定验证如下；各项均按其固定输入接受，不声称当前主线全量回归或整个 D08 完成。

| 验证 | 实际结果与边界 |
| --- | --- |
| 作者 pure/race/vet | 新增两 pure 顶层与原 LifecycleAuthority 构造兼容共 3 顶层、19 子项通过（89499→0cff7b），同包 vet 通过（65938→750b69）。 |
| 独立源码与受控 Store/Rows | 44702/c18c79 的 1 顶层、4 子项 race 通过；实际公开 gate/严格 loader 验证同 ctx/Tx/SH、两轮 Rows 关闭、后次读取失败不能复用先前授权。逆差异确认旧 Stop/Inspect/Outbox、合同及 SQL 不变。受控 Store 不代替下列 PG 结果。 |
| 作者当前事实 PG | `TestProjectSkillsCleanupCurrentFacts` 的 1 顶层、28 子项完整通过（14793→b4694d），覆盖当前阶段、Owner/版本/manifest、参与者停止及依赖完成矩阵。 |
| 作者 caller Tx PG | `TestProjectSkillsCleanupTransactions` 的 1 顶层、4 子项完整通过（86222→c6f0be），验证 SH/EX、不补锁或开 Tx、foreign/ended/取消及封闭 actor/participant 拒绝。 |
| 作者锁竞争 PG | `TestProjectSkillsCleanupCurrentFactsRemainLocked` 完整通过（66964→75bf35）；实际观察 Project SH 与 Owner writer 的精确 ExclusiveLock 等待，释放后两个原 Tx 均提交，旧 Owner/operation 随后不再获准。 |
| 独立 PG 补集 | `TestProjectSkillsCleanupIndependentRevalidation` 的 1 顶层、3 子项完整通过（76329→d17c95）：同原 Tx 首次获准后进度撤权/恢复并回滚、Owner 漂移/恢复并提交、原调用取消与 ended Tx 拒绝及后继健康 Tx。原私有快照及 Activity/Audit/Event/command 保持。 |

作者 3 顶层、32 子项与独立 1 顶层、3 子项均由未放宽的各自实际窗口完成：Go/driver/outer 原 Wait、两个精确资源双退役、private/runtime/desc、TCP 双样本与输入一致性全部收尾。入口发现及闭集映射另获有限独审；其工具只用于恢复执行，不作为产品授权或 PG 结果替代。

正式交付仅含该单一产品文件、相邻 pure 测试、作者与独立 PG 测试、本卡和 D08 台账条目。独立 PG 源按其已验版本原样纳入。环境监督工具、缓存、二进制、原始日志及逐轮检查点保留任务分支，不混入产品交付。受控阶段/参与者事实不证明真实 Skills cleanup、D05 最后同事务组合或生产 registry/root 已绑定；这些后继工作及 Object Runtime join 停止项保持。
