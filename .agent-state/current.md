# Execution preparation 当前切片

基线 `f8962895`；独立树 `ai/execution-preparation`。本树仅写 Execution、迁移 00040 和本任务测试。Go、真实 PG、进程及网络资源尚未运行；全部 Git 由 root 管理。

## 已实现与未绑定

- 新 `contract/preparation.go` 定义原 Execution/Launch 身份、独立 Project gate、Trigger capture/反向私有证明及有界不可变输入。原 Launch 构造与接口未改。
- 独立 `PreparationDriver.Run(ctx, executionID)` 从原持久 Launch 取得请求，已知提交后才进入捕获；迁移 00040 保存 `preparation_claims` / `preparation_attempts`，单调 fence 与完整 Execution scope、attempt/process 关联。
- 同一次 capture Tx 预收完整锁后，依次验证原 preparing/claim/fence、真实 Project 当前 gate、原 Trigger capture 返回，再用精确 AgentRun 的同 Tx 私有 witness 调用 Agent 配置读取。旧 Human Session 不代替 capture 授权；旧 current/running 口仍未绑定。
- **本批没有完整 Model/Tool/Skill/ref/lease capture provider。** 原 capture Tx 明确返回 `DependencyUnbound` 并整事务回滚，零持久 preparation input、零部分 source refs/lease；不创建 Snapshot、不转 running、不发布 Started。原 Execution 保持 preparing/version 2 与唯一 Agent slot，后继仍须沿原 Execution 身份恢复。
- attempt 的 `terminal` 仅表示该原调用/事务已实际返回并完成原锁下收尾，不表示 Execution failed/terminal 或 preparation ready。已知失败可沿原 EID 重新收集依赖、取得下一 fence；旧 source 捕获不作为已保存输入复用。
- Commit Unknown 保留原 attempt/cause 与本地归属；零未知提交后业务回调。`ResolveUnknown` 只收敛本 driver 已返回的原 attempt，不发新 claim/调用 provider；not-observed 仍保留原 Unknown。Stop 仅取消原调用，Drain/Joined 必须等实际返回与未知归属解除，composition 继续持有真实 ProcessGuard。

## 本批必要检查（待运行）

新 6 top，仅 `internal/central/execution/preparation_test.go`：

- `TestExecutionPreparationCapturesInOriginalTransactionAndRollsBackPartialInput`
- `TestExecutionPreparationUnknownOwnsOriginalAttemptUntilObserved`（claim/finish）
- `TestExecutionPreparationSourceRejectionAndPrivateProof`（4 sub）
- `TestExecutionPreparationStopWaitsForOriginalSourceAndCheckpoint`
- `TestExecutionPreparationForeignClaimRequiresDeathAndExactFence`
- `TestExecutionPreparationCapturedInputCopiesAndSafeProjection`

这些测试复用原 Launch fixture 的真实领域方法，但 SQL、Project/Task、process 是明确受控端口；不等于 PostgreSQL、Task 可启动来源、真实 ProcessGuard 或完整 capture 通过。拟同本批 Task 新测试合并一次定向 race/受影响 vet；不重跑旧 Launch 矩阵。迁移 00040 的实际 fresh/repeat/39→40 与约束由 skills 独立新 fixture 负责。

当前必要源已 gofmt，`git diff --check` 为 0；无 Go/PG 结果。contract 已 root 保存 `7effdf66`，首 core/DDL 已 root 保存 `39560eff`，新增测试与本记录待 root checkpoint。
