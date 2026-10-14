# M2：Task 执行者与审核者协作

2026-10-14 M2 约定能力已完成真实链与适用独立验收。m2_delivery 已完成实现集成，root 已将产品与正式文档整合并推送 main，任务 Git 收尾已完成。完整目标与门槛沿[开发计划](../development-plan.md#当前开发目标与里程碑)，本卡保存真实接缝、接受范围及后续边界。

## 目标与现有复用

真实 Task/Scheduler 驱动 worker 产出 Artifact，Human 实际读取原成果后，通过进入 `done`，或退回 `todo`，同一 Task 由新 Execution 修订并再次提交、读取与接受。reviewer 协作仍须实际消费 worker 成果。并行完成持久用户 Execution 取消、与 Task 取消的区别、Approval 等待后的批准/拒绝处理，以及必要冷恢复；批准后继续原 Execution/Operation，拒绝不执行副作用，取消不复活，恢复不重复已有副作用。

复用已正式交付的 [M1 真实工具工作链](m1-app-integration.md#当前真实模型验收)、原 Work 转移/权限、Scheduler、Runner read/write/run、持久调用/结果及 v1/v2 输出；真实代理/TLS/解析器与适用风险证据见 [live 验收卡](m1-live-acceptance.md)。M1 的受控 reviewer 自有说明、真实 review 收尾取消、Runtime 停止和已完成输出重启读取，均不代替本卡的真实成果评审、用户持久取消或运行中恢复。

正常 Scheduler 下真实外部 worker/reviewer 与 Human 的完整成果交接、原最终 Join/退休及精确资源退出已独立接受；受控业务、原生 Approval、持久取消与必要恢复、pending Unknown Found 及最小来源拒绝的已接受证据按范围复用。M2 约定出口已闭合，原失败与其他 STOP 不自动改变。

## 当前组合与验证状态

最终 review 组合已正式交付 main。最终真实 live 完成两 work、两 reviewer 精确原成果读取、Human 退回同 Task 新 E 修订后再读接受为 done、原新 E 完整投影及历史保持；原 App/owner/root、Node/Runner、guard及退休检查均通过并获独立接受。实际推送及任务收尾状态见 [current](../../../.agent-state/current.md)，不同输入证据按独验明确组合，不称全仓一次全绿。

旧审批 Start、Ensure 类型配对回滚与 retained 超时、Artifact 首链、浏览器 return旧投影比较等失败保持原结论。真实 live 的 review failed/model_failed、浏览器 Execution 详情502、review cooldown超期及退休辅助断言四轮均仍 FAIL；502原因和旧退休断言的具体false项未确定，最终通过不倒填。cooldown按合格Task visit计数；实际M2 live的正式review skip为40次，M1仍240，默认Runtime仍关闭，正常Scheduler、300秒总预算/单attempt60秒/MaxOutput4096/sent12软预算与代理/NO_PROXY/CA/TLS均保持。

| 范围 | 实际结果与当前边界 |
| --- | --- |
| Approval 与00099 | 真实 waiting 后批准/拒绝已独立接受；保持原 request/E/Operation/fingerprint，批准一次 Attempt/report/文件副作用，拒绝零 Attempt/report/副作用；决定/receipt/Outbox 同事务，00099 合法发布配对门已关闭。 |
| 拒绝后继与结果配对 | 00098 合法零 Attempt 拒绝后继、分表断言及最小错 approval ID 提交拒绝/完整回滚已获真实 PG 接受；不泛称所有非法字段矩阵完成。 |
| retained/waiting 生命周期 | 定向 race、真实常规审批及 waiting 取消/该终态重启不复活已独立接受；waiting 冷恢复后保持原 request/E/Operation/fingerprint，批准继续、一次副作用和原 Join 通过。原混合轮 FAIL 保留，不外推跨进程竞争或完整恢复。 |
| 运行中持久取消 | Human HTTP 取消 running Execution 后 Task blocked/原 blocker，与整 Task cancelled 分别验证；原 child/group 退出、lease 退休、同事务 receipt 和无后继调用已独立接受，不代 waiting/Unknown。 |
| 两个 cold 子场景 | 无结果时新 ProcessGuard 只读原 Operation、零重派发；已完成结果时只 Lookup 原报告、保原字节和一次副作用，后继与原 Join 通过。前者故意未解 owner 最后 kill/Wait，原混合轮其他子例仍 FAIL，不冒完整冷恢复。 |
| Artifact 与 Human HTTP | 真实 Core Builtin/Artifact/Object 发布及 Owner 完整读取、接受 done、退回同 Task 新 E/新 Artifact 后再读接受已独立接受；该受控场景审核阶段暂停 Scheduler；最终持续 Scheduler 的真实链另行接受。 |
| Human 浏览器 | accept 与修复后的 return 定向复验按组合独立接受；实际原/新成果读取、同 Task 新 E、Owner 决定、刷新、完整响应联合证明与原 Join 闭合。旧 return/整轮 FAIL 保留；最终真实 live 的浏览器证明另获接受。 |
| pending Unknown 与来源拒绝 | 真实原 COMMIT 响应丢失后仅 Lookup Found，Task/E/pending 取消同事务、不重派发、原全部 owners Join 已接受；实际跨项目 ArtifactRef 被原 Adapter gate 拒绝，Artifact/Object 与读取审计不变。不代 NotFound/cold Unknown、当前撤权或领域内全部授权。 |
| 完整真实 live | 正常 Scheduler 下两 work/reviewer、Human 退回修订后再读接受及严格原新投影/历史保持已独立接受；原 App/owners/root、Node/Runner、guard和ProcessClaim stopped/PG连接0均通过，精确资源退出。18条联合响应证明中1条ERR_ABORTED满足同请求完整证据，不能称零浏览器异常。 |

独立范围、P1/P2处置、最终真实链接受及旧失败由 [M2 独立审查](m2-independent-review.md#最终-m2-约定范围接受)唯一维护；本卡不将 M2 接受外推为完整 D19/D22、全部恢复或全产品 ready。

## 接口责任与唯一写域

以下保存本轮实现期间的责任划分；交付后已无活动 M2 写域。

| 负责人 | 提供方与消费责任 | 写域/迁移 |
| --- | --- | --- |
| m2_delivery | W2：组织 Task 协作/成果引用与 App 组合，消费各域端口；保留 Builtin/Task adapter 和 harness | App、registry/dispatcher、通用 Builtin/runtime 与 RunnerExecutor/shared contract；00095 Tool Builtin 持久结果；已移交审批范围除外。 |
| m2_work | Work 通过/退回及 Task 保护的已保存候选，由 W2 协调接续 | 共用 review 树，Work transition/保护、pending Scheduler、指定 builtin_backends 与 Task harness；00093/00097；review/current 归 m2_delivery。 |
| m2_controls | 持久 Execution 取消与必要恢复，向前端提供真实状态/命令合同；与 Work 区分 Task 取消 | Execution cancel/recovery、driver/multi_round/authority 与 ProjectCall ownership/repository/stop outcome、`api/openapi/execution.json`；00092/00096，前向修复00098。 |
| m2_approval | 权威 Approval 等待/批准/拒绝及原 E/Operation 继续；与 Tool/Execution 协商调用接缝 | governance、approval_operation 与必要 run_command 实现；独占00094/00099，后者合法发布配对已由真实审批场景接受；App审批harness已移交W2，共享Execution文件逐文件协商。 |
| m2_artifacts | 成果领域工具与真实持久提供方，供 worker 产出和 reviewer/Human 读取 | 领域适配及 Artifact/Project Audit 指定文件独占；与 W2 协调 registry/dispatcher 接入。 |
| product_recent | 消费正式 Task/Execution/Approval/成果合同，呈现实际操作与等待/取消/恢复结果 | `web/` 与已移交M2 browser消费者；不在UI合成领域终态。 |
| documentation_worker | 当前协调状态、正式目标与接缝索引 | 本卡、current、台账摘要和开发计划；macro独占独立审查记录，root负责全部Git。 |

共同契约沿 [Execution orchestration](d01-contracts/execution-orchestration.md)、[Model/Tool](d01-contracts/model-tool.md)、[Task 状态机](../../architecture/project-work-management/README.md#6-task-state-machine)和 [Approval 用户流程](../../architecture/security-governance/README.md#52-用户审批子流程)。W2 已确认现有 [TaskTransitionComposer](../../../internal/central/work/contract/task_transition_service.go) / TaskTransitions 与 [checkTransitionOccupancy](../../../internal/central/work/task_transition_plan.go) 为 Work 接口来源。取消合同已进入组合的 `execution/contract/cancellation.go`，使用原cancel私有调用/同事务Task保护与receipt；Governance `Gate` / `ExecutionAuthority` / `RejectedResults` 和 Tool `ApprovalOperations` 固定原E/Operation及零Attempt等待/拒绝。Artifact AgentRun权威与领域适配已入组合。具体方法/schema沿各owner正式源码及OpenAPI，不在协调卡重复定义；精确单写移交见各owner树current。迁移 00095 已专属 W2 的 Tool Builtin 持久结果；Artifact 确认无需新增迁移。

## 首条真实链与验收路径

1. Task/Scheduler 驱动 worker 在正式 Execution 中产生真实 Artifact，持久引用可追溯；Human 沿正式权限实际读取这些成果，不能以自有说明文件或 fixture 答案代替。
2. 通过使原 Task `done`；另一场景退回 `todo`，同 Task 新 Execution 修订后再次提交、读取并接受，核状态/事件/执行身份及原成果归属。Execution 成功不直接完成 Task。
3. 用户持久取消 Execution 后核实际停止/Join、可见终态与重启后不复活；Task 取消按 Work 合同单独验证，不能互相代证。
4. 真实受治理调用进入 Approval 等待，批准后继续同 E/Operation，拒绝不执行副作用；等待中取消、必要重启与重复提交沿原身份查证，核不重复实际副作用。
5. 使用真实提供方、隔离数据库/Runner/浏览器与原退出证据完成适用独立验证。先做必要正常/拒绝基础检查再联调，固定输入未变的证据复用；不能以编译、接口声明或受控 Provider 通过代替真实外部模型实证。

本卡约定验收与 main 远端交付已完成。reviewer 读取后确认不冒充自主审核决定，Human 仍唯一决定；NotFound/cold Unknown、完整审批恢复及未覆盖授权组合保持原边界，不启动 M3/M4。

## 恢复与接续

原 GitHub 认证阻碍已解除，root 已确认 main 推送成功并完成六个 M2 工作树、本地/远端分支和跟踪引用清理；无 M2 专用 fetchspec，main 配置保留。必要原始证据已保全在主树，外置共享依赖保持；实际收尾及证据保存见 [current](../../../.agent-state/current.md)。本轮无活动 M2 写域，后续按用户目标从已确认 main 建立新任务分支，不恢复已清理的旧任务。

M1 已在保留默认代理、NO_PROXY、CA/TLS 的条件下取得真实链成功并正式交付；历史本地 DNS 失败仅解释当时直连输入，不构成本轮代理场景不可达结论。复用真实证据按相同输入与影响范围判断，M1 review 接续不代表 M2 成果审核完成，其他旧 STOP 不因本轮恢复而重开。
