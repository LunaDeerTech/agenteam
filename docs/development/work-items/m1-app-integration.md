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

执行配置新增显式 `tool-loop-v1`；旧 `direct-text-v1` 保留原语义。profile 与 Provider adapter revision 分开；配置不自动删 Core、补 deny IDs 或制造权限。当前首片只有配置及明确 DependencyUnbound 门，多轮真实依赖未绑定前不回退成单轮文本成功。

共享同 Store/Authority/Registry，创建 owner 立即登记；停止时先禁新调度，再等待原 Scheduler/Execution/Loop/Tool/Model 返回，随后释放共享 Secret/Usage/Outbox/ProcessGuard。具体工具构造参数由领域 owner 的实际接口固定，尚未实现不声明可调用。

## 验证与资源

固定 Go 1.27.1、PG17 镜像及 MinIO release/SHA，复用 tests/testsupport 自有 fixture。工具与原生缓存外置共享，数据库/Object/spool/临时文件/输出按轮隔离，不接 onboarding 容器。初期两个 Go 命令、每个 -p=2；native 同时一个栈，按实际资源再调整。

每个稳定候选先必要正常/拒绝/取消基础检查，尽早真实首链；工具副作用验证实际内容及准确身份，恢复不能重发已完成副作用。复用未变输入证据，失败按原因定向修复。macro_milestones 为未参与实现者的独立验证负责人。环境准备通过、编译或旧单域能力均不替代端到端接受；所有旧 STOP/FAIL 保留。

## 当前进展

显式 profile 与 getter 已实现，App 暂明确 Unbound；未跑产品测试。固定 MinIO 已恢复且 SHA 匹配，固定 PG17/nginx 镜像已取得，Playwright 锁安装已完成，web 锁安装进行中。没有业务测试资源或外部模型调用。本片为可恢复 WIP，后续结果在本节更新。
