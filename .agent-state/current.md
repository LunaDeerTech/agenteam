# Execution preparation 当前切片

基线 `f8962895`；独立树 `ai/execution-preparation`。本树仅写 Execution、迁移 00040 和本任务测试。本批定向 race / vet 与下述有限 PG 检查已在组合树通过；PreparationDriver 真实成功、进程证明与完整 capture 尚未验收。全部 Git 由 root 管理。

## 已实现与未绑定

- 新 `contract/preparation.go` 定义原 Execution/Launch 身份、独立 Project gate、Trigger capture/反向私有证明及有界不可变输入。原 Launch 构造与接口未改。
- 独立 `PreparationDriver.Run(ctx, executionID)` 从原持久 Launch 取得请求，已知提交后才进入捕获；迁移 00040 保存 `preparation_claims` / `preparation_attempts`，单调 fence 与完整 Execution scope、attempt/process 关联。
- 同一次 capture Tx 预收完整锁后，依次验证原 preparing/claim/fence、真实 Project 当前 gate、原 Trigger capture 返回，再用精确 AgentRun 的同 Tx 私有 witness 调用 Agent 配置读取。旧 Human Session 不代替 capture 授权；旧 current/running 口仍未绑定。
- **本批没有完整 Model/Tool/Skill/ref/lease capture provider。** 原 capture Tx 明确返回 `DependencyUnbound` 并整事务回滚，零持久 preparation input、零部分 source refs/lease；不创建 Snapshot、不转 running、不发布 Started。原 Execution 保持 preparing/version 2 与唯一 Agent slot，后继仍须沿原 Execution 身份恢复。
- attempt 的 `terminal` 仅表示该原调用/事务已实际返回并完成原锁下收尾，不表示 Execution failed/terminal 或 preparation ready。已知失败可沿原 EID 重新收集依赖、取得下一 fence；旧 source 捕获不作为已保存输入复用。
- Commit Unknown 保留原 attempt/cause 与本地归属；零未知提交后业务回调。`ResolveUnknown` 只收敛本 driver 已返回的原 attempt，不发新 claim/调用 provider；not-observed 仍保留原 Unknown。Stop 仅取消原调用，Drain/Joined 必须等实际返回与未知归属解除，composition 继续持有真实 ProcessGuard。

## 本批必要检查

新 6 top，仅 `internal/central/execution/preparation_test.go`：

- `TestExecutionPreparationCapturesInOriginalTransactionAndRollsBackPartialInput`
- `TestExecutionPreparationUnknownOwnsOriginalAttemptUntilObserved`（claim/finish）
- `TestExecutionPreparationSourceRejectionAndPrivateProof`（4 sub）
- `TestExecutionPreparationStopWaitsForOriginalSourceAndCheckpoint`
- `TestExecutionPreparationForeignClaimRequiresDeathAndExactFence`
- `TestExecutionPreparationCapturedInputCopiesAndSafeProjection`

这些测试复用原 Launch fixture 的真实领域方法，但 SQL、Project/Task、process 是明确受控端口；不等于 PostgreSQL、Task 可启动来源、真实 ProcessGuard 或完整 capture 通过。迁移 00040 的实际 fresh/repeat/39→40 与约束由 skills 独立新 fixture 负责。

contract `7effdf66`、core/DDL `39560eff`、测试修正 `7fc4ba7f` 已保存。测试修正仅让新受控 Store 按真实 PostgreSQL 回滚行为保留原 callback cancellation cause，旧 Launch 测试和产品未改。

2026-10-10 的实际组合检查位于 `/workspace/agenteam-agent-system-integration/output/ai/execution-preparation/`，复现入口为同目录 `combined-pure-01-launcher.py` 和 `combined-pure-02-launcher.py`，原结果分别保存在 `combined-pure-01/result.json`、`combined-pure-02/result.json`，未复制日志。

- 首轮源 `15ade4cf`：Execution 恰 6 top / 6 sub race PASS；Task 编译遇不存在的 `f.AgentAggregate`，0 top，整轮 FAIL，vet 未启动。原 Go 875383 / outer 875379 Wait1，原组与 runtime 双尾为空。该失败保留。
- Task 作者只改为正式 `f.AgentLock`（`b98d0b9b`），root 同步为 `a962ccc4`；第二轮仅 Task 恰 6 top race PASS（Go 877176 Wait0，9.688s）及 Execution/Work 两包 vet PASS（Go 877332 Wait0，3.518s）。outer 877172 / session 15741 最终 0；两阶段组 absent、组/runtime 双尾为空、adopted=[]。
- 第二轮启动 fresh 5,757,984,768 B；固定 Go 1.27.1、offline readonly modules、私有 telemetry off 与原热 cache；未重跑已通过的 Execution 或旧 Launch 矩阵。结束时组合树 HEAD 仍为 `a962ccc4`、工作区 clean，本批两包及锁文件与该保存源无差异。共享 cache 窗口已释放。

纯检查有效覆盖为本批 Execution 6 top / 6 sub、Task 6 top 与两包 vet，保留上述首轮 FAIL。

后续 AgentSystem 源 `879a7252` 的 `TestExecutionPreparation` 原 1 top / 4 sub（31.31s）wholePASS，全部原 Wait0、完整尾已关闭。覆盖迁移 40 fresh/repeat、39→40 upgrade、claim/attempt 回滚约束，以及真实 Project gate、Owner Task read 与 Session revoked 拒绝。此为 SQL 约束及真实读取证据，不证明 PreparationDriver 真实成功、完整 capture 或 Started；完整组合记录见 [AgentSystem 模块 current](/workspace/agenteam-agent-system-integration/.agent-state/agent-system-integration/current.md)。本记录停写待 root 保存。
