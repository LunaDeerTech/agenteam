# Task / Scheduler 有限交付候选

本有限范围验收与集成已完成；实际main提交及远端确认由协调索引记录。候选从main `18a27db5` 构建，选择AgentSystem `13138dbd` 的255个技术路径、7个兼容路径与B32精确增量（`3cc9f1bd`），去重后连同current/本说明共282路径；最终实际验收source为 `70d383c5`。main已有Human Skill安装、Lookup、分页目录与读取入口保留；00032–00036不重取旧分支版本，新增连续迁移为00037–00047。

Work 的真实应用装配依赖 Agent 当前事实、Execution occupancy 和 Scheduler pending；真实 Agent 创建组合进一步使用 Model/Secret refs、Mount、Skills 初始化及真实 Runtime InstallSource/Builtin/Registry。对应有限库实现和测试均在选择范围中。Go 依赖保持 `jsonschema/v6 v6.0.3` 与实际 MVS 的 `regexp2 v1.12.0`；不是只复制 HTTP 层或以空 provider 补齐构造。

## 已有接受与限制

下表除最后一行外均复用各源分支原输入的真实结果，不冒合并后重新执行；最后一行在最终delivery运行。B整源 `542574b5` 已有限静审，精确26top race及五包vet wholePASS；候选main兼容4个app top race通过，app-pure01容量wholeFAIL/0vet保留，独立app-vet02后继wholePASS。必要旧证据复用，不要求重新运行旧全量矩阵。

| 精确真实入口 | 已有有限结果 |
| --- | --- |
| `^TestAgentConfigurationSchema$` | 1 top / 3 sub wholePASS；32–35 升级、旧事实与 metadata，相关迁移已在 main。 |
| `^TestAgentRuntimeSchema$` | 1 / 4 wholePASS；37–39 迁移、事务约束、Human 安装兼容，SQL 回滚探针不是 ToolCall 执行。 |
| `^TestExecutionPreparation$` | 1 / 4 wholePASS；40 升级、claim 约束、真实 Project gate 与 Owner Task 读取，不是完整 capture。 |
| `^TestAgentConfigurationCreate$` | 1 / 2 wholePASS；真实 Source/Reconcile、默认两项 true、refs/receipt 重放及最终事务整体回滚。 |
| `^TestTaskTransitionHuman$` | 1 / 2 wholePASS；40→43/repeat、Owner 配置、真实指派、重放与整体回滚。 |
| `^TestSchedulerClaim$` | 1 / 2 wholePASS；正式 Start、Work claim 与 durable pending 同事务、重放和回滚。 |
| `^TestSchedulerLaunch$` | 1 / 2 业务通过，原 HOST_TCP delta=1 导致 wholeFAIL；原 tuple 未保存，不补认或回填。 |
| `^TestSchedulerBusyCompensation$` | 1 / 2 wholePASS；真实明确 Busy、逻辑位置恢复、用户修改保全、原事务回滚与重放。 |
| `^(TestSchedulerPendingVisit\|TestTaskHumanHTTP)$` | 2 / 4 wholePASS；有界 visit 与真实 Human HTTP/原意图 Lookup。 |
| `^TestSprintStartHTTP$` | source `ff86ff65` 的9top/两包vet、compile/native全部wholePASS，1 / 1、7.49s；真实 TLS Start/Get/Lookup/重放。 |
| `^TestSchedulerLaunchFinalFailure$` | 最终delivery compile02/native01 wholePASS，1 / 2、39.54s；真实单类拒绝、技术阻塞与标题保全、原Tx整体回滚/结算。 |

Runtime/authorization/Agent capture 的受控纯检查、Schema 核心与历史 SpecRef 适配器检查不等于真实 ToolCall/Invocation；Agent Update、完整 capture/Snapshot 与 Model loop 未获本说明中的真实成功结论。生产 Project initializer 仍未绑定，真实 Agent.Create 使用测试中的正式服务组合，不能称生产 F1 已完成。完整 Dispatcher 的 retry/finalfailure/loop、Task UI、E01 及既有 Object/OpenAI tools/SPA/Jina/Image STOP 不随本候选改变。

## 复现入口与输入

真实业务源码集中在 [tests/projectvariable](../../tests/projectvariable)；固定真实组合在 [assembly.go](../../tests/testsupport/agentconfiguration/assembly.go)。共享入口使用原 [root_chain_driver.py](../work-owner-http/root_chain_driver.py) 和 [pg_only_supervisor.py](../task-planning-recovery/pg_only_supervisor.py)，精确 selector、required inputs 和完整 cases 由原 family 表选择。保留 main 原 Human Skill domain、HTTP 及组合三个 selector，不复制 observer、预算或资源退出方法。

候选编译沿已验的私有编译入口，对 `./tests/projectvariable` 执行 integration/race `-c`，仅列举所选精确 top；使用固定 Go 1.27.1、只读离线模块、任务私有 telemetry/runtime、同进程 fresh ≥5 GiB 门和原进程 Wait/双尾。新的二进制、输出目录和实际输入清单须独立生成，不能拿原候选声称已验证合并后的源码。

在既有资源窗口及原私有环境准备完成后，实际调用仍是原入口：

```sh
python3 -B .agent-state/task-planning-recovery/pg_only_supervisor.py \
  --driver .agent-state/work-owner-http/root_chain_driver.py \
  --binary "$CANDIDATE" --run "$EXACT_SELECTOR" \
  --output "$FRESH_OUTPUT" --root-chain
```

保留固定 MinIO、empty Docker config、原 Schema 解释器及 nonce 资源协议；完整判据仍包括实际 Go/driver/supervisor/outer Wait、七资源十四次 absence、private/runtime/desc/TCP 双尾和实际输入初末一致。业务通过不替代 wholePASS；失败保留原材料、先定位首个具体差额，不自动重发业务、不扩大旧矩阵。

旧轮可恢复源码与方法保留在 `ai/agent-system-integration` 及原donor分支，结果保留于原树 `output/ai/agent-system-integration/`。本次结果在原delivery的 `scheduler-failure-01-control/result.json`，恢复方法保留在 `ai/task-flow-delivery`（验收后checkpoint `f9e3c14b`）。三个 `scheduler-failure-*-launcher.py` 和 `.agent-state/task-flow-delivery/app-checks.py` 只保留topic，不作为main入口或本地链接；正式复现使用上面的原通用入口。候选可重建，原FAIL/输入/日志不删除。

## 本次最终接受边界

`app/account.go` 保main Skill management两处接线，只加入Work所需Project authority；六个入口/控制文件将System exact profiles合入main generic family，strict inverse、未知输入拒绝和原退出门均保留并获有限静审。四app受影响top及独立vet已通过；failure编译/真实链已在最终delivery通过，System复用相同产品纯证据，37–46未重跑。

00047及其代码、HTTP只读schema已随B32纳入并完成静审、纯检查和本次真实链。唯一新增类别为真实Work producer返回的 `unsupported_resource_constraints_v1`：同一次原同步KnownNotCreated、私有typed marker与完整原请求/attempt相符，才可持久分类并由Work原事务生成technical-blocker/历史；其它错误不推断永久失败或retry exhaustion。该有限接受不代表完整Dispatcher、生产F1或ready。

native01于2026-10-10 19:23:00–19:25:06 UTC整轮通过：所有原Go/driver/supervisor/outer Wait0，七资源十四次absence、private/runtime/desc/HOST_TCP及outer双尾空，adopted为空，1460inputs首尾一致；资源与cache窗口已归还。compile01容量门前0Go/0PG的FAIL、app-pure01容量wholeFAIL以及旧Launch01 TCP wholeFAIL全部保留，不被后继补集或本轮通过改写。
