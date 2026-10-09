# Project Skills CleanupPhase 独立审查恢复点

当前仅完成首轮静审；尚未编译、执行独立测试或 PG，不是整块最终接受。作者树 `/workspace/agenteam-project-skills-cleanup`，基线正式 main `29dd4298`。只读冻结生产 `internal/central/project/lifecycle_authority.go` 及 `docs/development/work-items/d08-project-skills-cleanup-authority.md`；作者新增测试仍在准备，本轮未将浮动测试纳入结论。依据为 Skills 已接受设计 §16.3 与正式 D08 LifecycleAuthority/contract/严格持久行 loader。

首轮未发现确定 must-fix：

- 新公开分派只处理 `SkillsParticipant + CleanupPhase`。其余 Cleanup participant 仍 DependencyUnbound；原 Stop/Inspect 的 shared fact 判据及 Outbox 不变。Archive cause 不能到新分支的数据读取。
- 沿原 `lifecycleExecutor` 核本 Store 活 Tx/Project SH（EX 可覆盖）后读私有事实；不 Begin/Acquire/Commit、不请求 Session、无外部清理或 participant I/O。第一次 loader 的 Rows 在返回时已 Close，第二次操作读取仍在原 Tx/同 Project 锁内。
- 原 `lifecycleFact` 核当前 Project/删除 receipt 互斥、initialized/current operation pointer、operation owner/当前 Owner、原 action/固定版本、冻结 manifest 元数据/兼容版本、所有 participant 的 strict 行形状及 Project gate/version；新分支再限制 Cleaning/domains、全部 stopped、Skills required/pending、全部 CleanupAfter completed。Failed/resume=cleanup 与 completed receipt 不能得到新的清理许可。
- `ActorDetails` 无独立 Scope 字段，但正式 opaque `ServiceRegistration.Actor` 拒 AgentMemory，SystemScope 投影空 ProjectID 在 `lifecycleActorProject` 解析失败。ProjectLifecycle/name、CauseRef=OperationID 与非空合法 ProjectID 因此只能由正式 Project scope 构造闭合；不需要为了本新分支扩改旧公共 actor helper，也不能将 UUID/声明当当前授权。
- `LifecycleAuthority` 仍仅返回 error，不生成跨 Tx grant、opaque cleanup plan、Skills cleanup事实或 actual stop/join 证明。构造的 immutable declarations 不代表真实 registry participant 已绑定。Skills 本域 consumer/D05最后映射清理/root不在本片段内。

本人只读逆差异命令 `8d6f98` actual exit0：将唯一 Cleanup dispatch 改回原 Unbound 并移除两个新私有函数后，`lifecycle_authority.go` 逐字等于29dd；`lifecycle_store.go`、Object Stop/Inspect、Outbox authority、Project contract 和 SQL 均未改。没有执行 Go 或任何真实资源，当前遵守 root 的磁盘/编译暂停。

后续只在作者测试稳定后选择有意义的独立负控；不重跑所有旧 Project。真实 PG 仍需证明所声明正向已到达当前事实门、SH/EX与foreign/ended/missing-lock边界、严格行/兼容声明与失败阶段拒绝、ctx取消后的原Tx退休；测试拥有的 Cleaning/participant事实必须与真实 BeginDelete 分开，不冒生产生命周期已推进。此处只是待验闭包说明，不向作者提供测试实现或提前认定未运行结果。
