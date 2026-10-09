# Project Skills CleanupPhase 独立审查恢复点

结论：生产 `415e0df5` 与随后冻结的两新测试有限离线接受，无剩余 must-fix；真实 PG 尚未编译/执行，不是整块实际授权或生命周期验收。作者树 `/workspace/agenteam-project-skills-cleanup`，基线正式 main `29dd4298`。依据为 Skills 已接受设计 §16.3 与正式 D08 LifecycleAuthority/contract/严格持久行 loader。先前首轮静审记录保留如下，后续实际控制见末节。

首轮未发现确定 must-fix：

- 新公开分派只处理 `SkillsParticipant + CleanupPhase`。其余 Cleanup participant 仍 DependencyUnbound；原 Stop/Inspect 的 shared fact 判据及 Outbox 不变。Archive cause 不能到新分支的数据读取。
- 沿原 `lifecycleExecutor` 核本 Store 活 Tx/Project SH（EX 可覆盖）后读私有事实；不 Begin/Acquire/Commit、不请求 Session、无外部清理或 participant I/O。第一次 loader 的 Rows 在返回时已 Close，第二次操作读取仍在原 Tx/同 Project 锁内。
- 原 `lifecycleFact` 核当前 Project/删除 receipt 互斥、initialized/current operation pointer、operation owner/当前 Owner、原 action/固定版本、冻结 manifest 元数据/兼容版本、所有 participant 的 strict 行形状及 Project gate/version；新分支再限制 Cleaning/domains、全部 stopped、Skills required/pending、全部 CleanupAfter completed。Failed/resume=cleanup 与 completed receipt 不能得到新的清理许可。
- `ActorDetails` 无独立 Scope 字段，但正式 opaque `ServiceRegistration.Actor` 拒 AgentMemory，SystemScope 投影空 ProjectID 在 `lifecycleActorProject` 解析失败。ProjectLifecycle/name、CauseRef=OperationID 与非空合法 ProjectID 因此只能由正式 Project scope 构造闭合；不需要为了本新分支扩改旧公共 actor helper，也不能将 UUID/声明当当前授权。
- `LifecycleAuthority` 仍仅返回 error，不生成跨 Tx grant、opaque cleanup plan、Skills cleanup事实或 actual stop/join 证明。构造的 immutable declarations 不代表真实 registry participant 已绑定。Skills 本域 consumer/D05最后映射清理/root不在本片段内。

本人只读逆差异命令 `8d6f98` actual exit0：将唯一 Cleanup dispatch 改回原 Unbound 并移除两个新私有函数后，`lifecycle_authority.go` 逐字等于29dd；`lifecycle_store.go`、Object Stop/Inspect、Outbox authority、Project contract 和 SQL 均未改。没有执行 Go 或任何真实资源，当前遵守 root 的磁盘/编译暂停。

后续只在作者测试稳定后选择有意义的独立负控；不重跑所有旧 Project。真实 PG 仍需证明所声明正向已到达当前事实门、SH/EX与foreign/ended/missing-lock边界、严格行/兼容声明与失败阶段拒绝、ctx取消后的原Tx退休；测试拥有的 Cleaning/participant事实必须与真实 BeginDelete 分开，不冒生产生命周期已推进。此处只是待验闭包说明，不向作者提供测试实现或提前认定未运行结果。

## 冻结测试与本人实际控制

作者稳定两源为 `internal/central/project/skills_cleanup_authority_test.go` 和 `tests/project/skills_cleanup_authority_test.go`。本人逐项静核3个 PG top：真实 Account/Project 创建与 BeginDelete/accepted/resolver 来源保留；Cleaning、额外前驱和 participant 完成、initializer 是明确受控测试事实。当前事实/严格兼容声明、只读全快照及Activity、SH/EX/foreign/ended/missing/取消与原事务终态均有断言。原 SH 持有者 PID、同DB/正式advisory key、pg_blocking_pids 的实际等待观察后才释放；两个原 callback/WithinTx 结果必须实际返回，失败 cleanup 亦释放/取消/等待。不把 SQL UPDATE 返回当事务 join；这三 top 尚未实跑。

首轮较早读取的 pure fixture 曾未把新增 Skills 加入 Outbox 的直接 CleanupAfter；作者在运行前已补5行，最新冻结源与其 `89499/0cff7b` race0（3top/19sub）一致，此项已闭合、无生产变更。本人复用该作者定向结果及 `65938/750b69` vet0，不重做作者矩阵。

root 解除本次小控制后，命令（cwd `/workspace/agenteam-skills`）：

```sh
python3 .agent-state/project-skills-cleanup-review/run.py
```

本人 `44702/c18c79` actual exit0，race1.025s，1top/4sub：

- 实际公开 `ValidateLifecycleInTx` 经严格 Project/operation/manifest/participants loader 的正向；原ctx/Tx/Project SH只验证一次，第一轮真实 Rows wrapper 已 Close 后才第二次读取，两轮均关闭。
- 原 RequireHeldLocks 失败保原 cause、零私有查询；没有补锁/新Txn。
- 第二次 operation QueryRow 失败不得借第一轮成功事实放行，保原 cause。
- participant 流已提供完整有效行但最终 Rows.Err 失败仍拒绝、保原 cause，并实际关闭 wrapper，不继续第二轮授权。

Store/pgx.Rows 是明确 controlled facts；实际 public authority、strict decoder 和 postgres.Rows wrapper 在跑，不称真实 PG/真实锁或SQL解析。复用本树既有只读 `knowledge-b02-review/postgres_rows_bridge.go`，overlay不写作者树。首同process statvfs=5,649,293,312B后才执行，固定Go1.27.1/local/off/readonly/-p1，GOMODCACHE与独占 Variables-independent GOCACHE/GOTMPDIR 完整固化在run.py，非Work/VarsUI当前缓存，不建新cache。生产source运行前核415e、返回后逐字未变；无资源或子程序在途。

冻结四路径：本文、`authority_test.go`、`run.py`、本树 current。待真实闭包仍为新3top与原PG完整尾，不外推Skills本域Cleanup、D05物理清理/last anchors、生产 registry/root或完整 Project 删除。

## 三个 single-top 入口窄审

root 已保存上述四路径 `4e30ba15`。后继独立窄核作者 `9c60199f` 后唯一 supervisor 增量与新 `selector-controls.py`，有限离线接受，无 must-fix；原 Go driver 不变。只有三个批准 literal 会在原 reap 后、TCP 尾前校验日志：实际 RUN/PASS 父 top 各恰一，任何父/子 FAIL 或 SKIP 拒绝。UTF-8 解码错误与 OSError 返回 false，使本轮失败但不跳过后续 TCP/input/terminal。

本人命令（cwd `/workspace/agenteam-project-skills-cleanup`）：

```sh
PYTHONDONTWRITEBYTECODE=1 python3 .agent-state/project-skills-cleanup/selector-controls.py
```

`91627/d3ced9` actual exit0，6 tests/3.224s：三个真实冻结 race candidate 的 exact `-test.list`、日志正负矩阵、literal 闭集、原 main 的失败日志/旧入口与完整受控尾、逆去增量后 supervisor/driver 逐字 `9c60199f`。过程只有只读 binary listing；main 的 Popen/Wait/reap/desc/TCP 为明确 doubles，无 PG/socket/真实驱动。继承 Go6m、PG driver105+15、supervisor123+3 与 TCP75 原预算，不凭这次 pure 控制接受三业务 top。原两新测试现在已由作者编入65468候选，仅发现而未 PG；早先“未编译”是当时状态，不回填当时结论。
