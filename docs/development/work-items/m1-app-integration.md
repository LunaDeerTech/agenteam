# M1：默认 App 的单 Agent 工具工作闭环

2026-10-14 开始实施，基线 `c75e1bb8`，负责人 delivery_path。目标沿[开发计划](../development-plan.md#当前开发目标与里程碑)；本卡记录 W5 接缝与真实组合，不代表 M1 通过。

## 可见结果与验收边界

默认 App 以正式 Project/Agent/Task 配置，由原 Scheduler 自动启动同一 Execution。Agent 读取自有目录的 input.json/任务说明，生成统计程序及结果文件，运行真实校验命令，并提交可见成果。至少两轮实际工具交互；核对文件内容、stdout 和 exit，以及持久 Model/ToolCall/ToolResult 配对。Execution 成功后 Task 不自动 done，Owner 沿已有 Transfer 入口明确进入 in_review。

先使用任务所有确定性 Provider 的真实 HTTP，验证正式协议/领域调用/Runner 文件与进程副作用；它不证明外部模型实际推理。后续外部 Provider 在原配置/权限/出站 gate 下有限调用，不输出凭据、不改变 DNS 合同或重跑旧 STOP 驱动。

## 组合接缝和唯一写者

| 提供方 | App 消费的约定 |
| --- | --- |
| model_runtime | 原 mc.JSONCaller、model.NewRuntimeWithAgentRetry 和单一 adapter 生命周期；新 model.NewToolCallingExecutionCapture 选择 tools revision，旧快照不改写。 |
| core_readiness | ProjectCallDependencies.MultiRound 选配；原 ProjectCallOwnership 内构造多轮 driver 并接原 Associated/Stop/Drain/Joined；负责持久多轮及全局迁移 00091。 |
| runner_tools | 同 Execution 私有 authority 的 RunnerExecutor/RunnerResultAuthority；真实 Mount resolver、Runner dispatcher、schema validator 与精确 E/Round/Call。 |
| product_recent | 既有执行 list/detail/runtime-view 与 Human 评审入口；可见最终结果，不从 Execution 终态推导 Task done。 |
| delivery_path | App/config/必要 cmd 接线与新 M1 harness、测试依赖和资源协调；不替领域服务实现业务状态机。 |

执行配置新增显式 `tool-loop-v1`；旧 `direct-text-v1` 保留原语义。profile 与 Provider adapter revision 分开；配置不自动删 Core、补 deny IDs 或制造权限。当前已绑定新 Capture/多轮选配及同原路由 DeviceHandler 的三 Runner factory，旧配置保持原组图。构造缺依赖仍明确拒绝。

共享同 Store/Authority/Registry，创建 owner 立即登记；停止时先禁新调度，再等待原 Scheduler/Execution/Loop/Tool/Model 返回，随后释放共享 Secret/Usage/Outbox/ProcessGuard。具体工具构造参数由领域 owner 的实际接口固定，尚未实现不声明可调用。

## 验证与资源

固定 Go 1.27.1、PG17 镜像及 MinIO release/SHA，复用 tests/testsupport 自有 fixture。工具与原生缓存外置共享，数据库/Object/spool/临时文件/输出按轮隔离，不接 onboarding 容器。初期两个 Go 命令、每个 -p=2；native 同时一个栈，按实际资源再调整。

每个稳定候选先必要正常/拒绝/取消基础检查，尽早真实首链；工具副作用验证实际内容及准确身份，恢复不能重发已完成副作用。复用未变输入证据，失败按原因定向修复。macro_milestones 为未参与实现者的独立验证负责人。环境准备通过、编译或旧单域能力均不替代端到端接受；所有旧 STOP/FAIL 保留。

## 当前进展

显式 profile 与 getter 已实现，配置定向测试实际退出 0。App 三 Runner metadata 来源、Registry 同事务初始化、新 Model Capture、MultiRound 选配、真实 private authority factory 已落；每个 Tool authority 构造即注册原关闭组，复用路由持有的 DeviceHandler。整体尚待 W2 可编译源码，不声明构造通过。

实际导入 Model `7cac84aa`、Tool `0c523752`、Task/UI 与浏览器 `9a206769`；其原证据范围保持，不等于组合通过。主 `TestM1AppToolWorkflow` 使用正式 Project/Runner/Provider/Agent/Task 操作并释放原 Scheduler 首空目录，计划五轮真实 HTTP read/write/run/read/final；正常 fixture WireSequence 由固定两条扩到有界 2..16，原最后一条重复及同路径校验保留。

root 授权在本树三文件并行：runner_tools 写 Runner TLS/原进程 Wait，model_runtime 写持久终态/文件/stdout/exit/peer 断言，product_recent 写 browser invoke 与 Owner 回验。主 harness 由 delivery_path 单写。第二片保存时三辅助尚在写，root 排除这三文件并另片保存；无整体编译与 native PASS。

固定 MinIO SHA、PG17/nginx 镜像和两个 Node 锁安装均完成，外置共享依赖可复用。没有业务测试资源或外部模型调用。两路 Go / 一路 native 预算不变；后续将先同栈验 00091 全新迁移与真实 App 首链。
