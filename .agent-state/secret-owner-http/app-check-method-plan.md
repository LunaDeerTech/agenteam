# Secret HTTP 交付前完整 app 检查方法

状态：只读方案，尚未修改共享入口、编译或运行。产品与既有检查记录固定于 `61227844`；本文不替代后续独立方法审、root 保存与唯一真实窗口授权。

## 范围与既有证据

本次补 `internal/central/app` 整包普通与 race 检查。现普通标签静态枚举 83 个 Test 顶层入口，含默认不进入子进程业务的 `TestAppProcessFixture`；没有 TestMain、Example、Fuzz 或 Benchmark。完整清单见文末。不能以此前七个限定 pure 顶层测试替代整包。

已通过的受影响普通/integration vet、Central/Runner 两构建见[当前检查点](../current.md)及[HTTP 卡](../../docs/development/work-items/d10-secret-variables-owner-http.md)§12，不重跑。作者 HTTP PG/native、默认 root 一top两sub及 Owner 原30节点按原冻结输入复用。独立 Session/安全错误补集由非作者另行实施，本方法不包含该补集或 F1。

## 真实资源范围

- `process_test.go` 的 `TestRealProcessDrainDeadlineAndSecondSignal` 有 graceful/deadline/second_signal 三个子场景；`TestRealProcessSignalDuringStartup` 有 startup 子进程。它们实际启动同一测试可执行文件的 `TestAppProcessFixture`，使用真实 loopback TCP、SIGTERM/SIGINT 和原 `cmd.Wait`。子进程 `-test.timeout=15s`，原退出等待4s、公共 barrier/log/HTTP 等待5s保持。
- `app_test.go` 的 DiagnosticRoutes、GracefulDrain、ForcedStop、StartupStop 四个顶层测试也涉及真实监听。Secret/Outbound 普通测试还通过 `startApp` 启用真实监听；`unitDependencies` 并不替换默认 listener。因此窗口覆盖整包全部实际 socket，不能表述为仅两项 native。
- 本轮没有 integration 标签，不执行 PG、Docker、MinIO 或 Secret root/HTTP 的 integration 测试。普通 fixture 使用受控数据库/领域端口与任务自有配置；子进程收到显式 fixture 环境。仍需 host TCP 唯一窗口，root Git 网络和其他资源执行停到完整尾结束。

## 最小共享入口增量（coordination 唯一 writer）

目标文件仅 `.agent-state/owner-feature-integration/final_check.py`，另由 coordination 选择必要的有限纯控制位置。不得改 `scripts/check-go.sh`、`scripts/build-go.sh`、产品或测试。

1. 在原 mutually-exclusive mode 组加固定 `--app-repair`，与 full 默认、`--remaining`、`--audit-repair` 互斥。不得增加任意包名、命令、selector 或 skip 参数。
2. 新 `app_repair_script(source)` 严格核原 check-go header、ordinary 行以及完整 vet/integration-vet/race/build 尾的既定形状；保原 header（含 `set -eu`、本地 Go1.27.1 检查），仅生成以下两个原 test 命令的固定包替换：

   ```sh
   "$AGENTEAM_GO" test ./internal/central/app
   "$AGENTEAM_GO" test -race ./internal/central/app
   ```

   不加 `-run`、`-tags`、测试过滤或加时；不重复 vet/build。第一阶段失败沿原 `set -e` 停止，保原 FAIL，不自动重试。
3. 只在新分支选择这份固定脚本，result 增一个 JSON boolean `app_repair`。旧 full、remaining、audit 脚本文本与执行分支保留原行为。
4. 沿用现输出范围 `output/ai/owner-feature-integration`，新目录建议 `app-repair-01`；不扩大目录约束。沿用原 shared readonly mods、原任务树隔离 cache、私有 telemetry off/移除三旁路、schema Python/Node 输入映射及固定工具链。模式不执行 Schema 测试，不将解释器身份检查冒充 Schema 验收。

## 必修的原 TCP 提取兼容点

现 wrapper 的 `tcp_fragment()` 原终点为：

```text
            if secret_owner:
                try:
                    same =
```

此完整字串在当前 Secret HTTP supervisor 中出现0次，因为前面已插入 `if secret_http_selected`，原 Secret Owner 分支现为 `elif`。开始字串和两个 reap 边界仍各出现1次。直接扩 mode 会在测试之后提取尾时失败，不能等待真实轮才暴露。

最小修正为将终点移到当前 `if secret_http_selected` 输入冻结分支前；仍要求两边界唯一，不改 supervisor。只读比较已确认：从 `tail_deadline = time.monotonic() + 75` 至新终点的563字节，恰等于 main `fb84a892` supervisor 从同起点至原终点的563字节。必须保留这个逐字等价控制，避免改变原 TCP 采样、75s期限、连续两次空 delta 或 FAIL 决策。旧 full/remaining/audit 也使用同一原 TCP 块，不得借本次更换它们的尾语义。

## 原预算、Wait 与完整结果

- 保原 Go test 命令的默认整包 timeout，不把 PG root 的540+60+3s预算移植过来。当前 final_check 的直接 shell `child.wait()` 本身没有额外超时；不能宣称存在独立全局限时。子进程测试已有15s及局部原等待不变。
- 原 shell 实际 Wait、子进程自身 Wait、subreaper与后代处理不变。原 survivors 一旦存在先置 FAIL，随后 kill/reap 不能升级；root-chain 提取块的 adopted-reap期限为5s，后代两次观察继续执行。
- 保 runtime 两次空观察、原 hostTCP最多75s且连续两次空 delta；同步诊断只消费原采样，不加背景采样或改变 ownership/acceptance。外层必须实际 terminal 后才报告窗口释放。
- 本模式的输入是保存后的入口 ref、现冻结产品/普通 app 测试与正常依赖树、go.mod/go.sum、check-go/build-go、当前 supervisor、tcp_diagnostics 和原工具链/环境映射。执行期停写这些输入；保原 source 参数与 supervisor SHA记录。不宣称此 wrapper 有 PG 的703输入枚举门。
- 整包普通/race成功输出应分别留原日志；如输出显示缓存，仅如实标缓存复用，不把它写成新真实 socket 执行。原 check02/fullscript 等 FAIL保留，最终以限定复验与既有检查组合呈现，不回填旧轮 wholePASS。

## 实施前最小有限控制

coordination 可只做固定脚本生成、parser 与源片段的纯控制：确认新模式恰两条完整 app 命令且无过滤；破坏原脚本锚点/尾形状必须拒绝；模式互斥；旧 full/remaining/audit 输出逐字不变；新 TCP 边界唯一且提取563B与原块相同。控制不启动 Go、Python/Node身份进程、wrapper main、socket或资源。实际差异接受、root checkpoint后才准备一次新窗口；本文件只提供方法。

## 普通标签完整顶层清单

以下为当前源码的静态枚举，不是 `go test -list` 或执行结果；全部83项由整包命令保留，不逐项拼 selector。

`account_lifecycle_test.go`

```text
TestB04AppMailFinishesBeforeCoreRetirement
TestB04AppJointGuardRequiresEveryActualJoin
TestB04AppPartialSinkAndLateAcquisitionUseOneOwner
TestB04AppPartialRealSinkActuallyCloses
TestB04AppExhaustedSinkBudgetStillForcesDatabaseLast
TestB04AppAccountHealthHasIndependentFreshness
TestProjectUpdateAssemblyPureOrderingAndPartial
```

`app_test.go`

```text
TestDiagnosticRoutes
TestGracefulDrainPreservesActiveContextAndRejectsLateRequest
TestForcedStopBoundsNonCooperativeHandler
TestStartupStopAndUnexpectedServeFailure
```

`audit_http_test.go`

```text
TestAuditRootPureRoutePreservationAndSingleMiddleware
TestAuditRootPureSameInstanceConstructionGraph
```

`database_test.go`

```text
TestDatabaseStartupOrderAndFailureClosesAcquisitions
TestStartupSecondSignalBoundsUncooperativeAcquisitionAndClosesLateStore
TestHealthSnapshotsExpireRecoverAndRejectLateSuccess
```

`health_sampling_test.go`

```text
TestB04HealthSamplingWaitsForEachActualReturn
TestB04HealthSamplingStopKeepsActualChecksSeparate
```

`knowledge_skills_test.go`

```text
TestKnowledgeRootProcessEnvironmentConfiguration
TestKnowledgeSkillRootActualDrain
TestKnowledgeSkillRootProviderOrder
TestKnowledgeSkillRootRoutes
```

`model_test.go`

```text
TestModelRootRouteOwnershipAndRequestPreservation
TestModelRootRequiredConstructionDependencies
```

`object_authorities_test.go`

```text
TestKnowledgeSkillRootFixedDispatch
```

`object_test.go`

```text
TestObjectsReceiveUnrenewedSecurityDeadlineBeforeListener
TestLateObjectsAreRejectedAndForcedWithOriginalExpiredBudget
TestCancelledObjectsRemainOwnedByOriginalShutdownGroup
TestObjectHealthAgeDoesNotRefreshDatabaseSample
```

`outbound_policy_http_test.go`

```text
TestOutboundPolicyRootRoutePreservationAndSingleMiddleware
```

`outbound_test.go`

```text
TestOutboundHTTPMaintenanceDrainAllPrecedeDatabase
TestOutboundAndDatabaseForceShareOneBudget
TestOutboundStartupFailureAndLateResourceDisposed
```

`outbox_test.go`

```text
TestOutboxForcePreservesGuardUntilActualJoinAndClosesDBLast
TestOutboxOwnerRegisteredBeforeInitializationAndLateForce
TestOutboxSharesStartupBudgetAndFailurePreventsListener
TestOutboxHealthFreshnessIsIndependent
```

`process_test.go`

```text
TestAppProcessFixture
TestRealProcessDrainDeadlineAndSecondSignal
TestRealProcessSignalDuringStartup
```

`project_audit_test.go`

```text
TestProjectAuditRootPureCompositionAndRoutes
```

`project_credentials_test.go`

```text
TestProjectCredentialsRootPureMissingDependencies
TestProjectCredentialsRootPureRouteOwnership
```

`project_models_test.go`

```text
TestProjectModelsRootPureMissingDependencies
TestProjectModelsRootPureRouteOwnership
TestProjectModelConfigurationHTTPComposition
```

`project_read_test.go`

```text
TestProjectReadRootPureConstruction
TestProjectReadRootPureRouteOwnership
```

`project_secret_variables_test.go`

```text
TestProjectSecretVariablesPureConstructionAndActualJoin
TestProjectSecretVariablesRootOrderAndLateInstall
TestProjectSecretVariablesRootPreciseRouteOwnership
```

`project_update_test.go`

```text
TestProjectUpdateRootPureConstruction
TestProjectUpdateRootPureActualDrain
TestProjectUpdateRootPureRouteOwnership
```

`project_usage_test.go`

```text
TestProjectUsageRootPureConstructionAndReadOnly
TestProjectUsageRootPureStartupContextAndFailure
TestProjectUsageRootPureRoutesPreserveOriginalChain
```

`project_variables_test.go`

```text
TestProjectVariablesPureConstructionAndActualReadLookupJoin
TestProjectVariablesRootOrderAndLateInstall
TestProjectVariablesRootPreciseRouteOwnership
```

`runner_control_test.go`

```text
TestRunnerRootPureConstructionAndRetirement
TestRunnerRootPureActualCallAndOriginalForce
TestRunnerRootPureRouteOwnership
```

`runtime_information_test.go`

```text
TestRuntimeInformationRootPureObservationHistoryAndBoundaries
TestRuntimeInformationRootPureCompletenessAndReadiness
TestRuntimeInformationRootPureSingleCopyAndActualReturn
TestRuntimeInformationRootPureBoundDependencies
TestRuntimeInformationRootPureConcurrentWholeObservation
TestRuntimeInformationRootPureFactoryHandoffAndFailedStartup
TestRuntimeInformationRootPureRoutesAndFormalBoundary
TestRuntimeInformationRootPureSameInstanceConstructionGraph
TestRuntimeInformationRootPureLegacyDiagnosticsRemainSeparate
```

`secret_test.go`

```text
TestSecretWorkerAndExistingHTTPDrainBeforeDatabase
TestSecretWorkerForceAndJoinUseSingleTotalBudget
TestSecretLateStartupAcquisitionCannotStartWorkerAfterStop
TestSecretWorkerFailureDoesNotClaimReadiness
```

`security_test.go`

```text
TestSecurityInitializationSeparateBudgetAndBeforeListener
TestSecurityInitializationFirstAndSecondSignal
```

`work_planning_test.go`

```text
TestWorkPlanningPureConstructionAndDrain
TestWorkPlanningAllThreeActualCallsMustJoin
TestWorkPlanningForceAttemptsEveryDrainWithOriginalContext
TestWorkPlanningRootOrderingAndLateInstall
TestWorkPlanningRootRouteOwnership
```
