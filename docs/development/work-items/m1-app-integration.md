# M1：默认 App 的单 Agent 工具工作闭环

2026-10-14 开始实施，起始基线 `c75e1bb8`，集成负责人 delivery_path。目标与正式门槛沿[开发计划](../development-plan.md#当前开发目标与里程碑)。当前已实现受控 Provider 的真实工具链；正常浏览器验收仍有断点，外部模型尚未验证，不声明 M1 完成或已交付 main。

## 当前成果与验收边界

默认 App 使用正式 Project/Agent/Task 配置，由原 Scheduler 自动启动同一 Execution。自有确定性 Provider 经真实 TLS HTTP 依次请求读取 `input.json`、写入 `stats.py`、运行真实 Python、读取 `result.json`，最后给出文本：同一工作 Execution 精确五次 Model / 四次 Tool，真实文件、stdout/exit 0、持久调用/结果配对及最终 `durable_final_text_v2` 快照均已到达。既有详情 UI 支持旧 v1 和新 v2，Execution 成功不自动使 Task done。

`fee69b0d` 的第九轮已到达 Owner 唯一送审 HTTP 200，Task 持久化为 `in_review` / version 4；独立 `task/review` Execution 完成两次 Model / 一次 read。刷新后的第三次输出读取实际 `ERR_ABORTED`，HTTP 200 正文不完整，正常 App Join 和最终 peer 断言未到达，整轮仍 FAIL。该 Reviewer 只读自己的说明文件并确认收到任务，不证明 worker 成果传递、自主评审决策或 M2 完成。

失败/取消的独立两个子例已通过并获限定独立接受：真实 read missing 无载荷使 Execution `failed/runtime_failed`；真实命令 marker 后调用原 Runtime StopAdmission/Drain，使 Execution `cancelled`，并确认原子进程、进程组退出。两者保留原 pending bytes，完成同事务终态、精确 1 Model / 1 Tool、Runner 原 Wait 0 与 App 全 owner Joined。

这些结果证明自有受控 Provider 的协议、领域调用和 Runner 副作用，不证明外部模型自行规划。外部 `DEV_CHAT` 预检因本环境 A 成功 / AAAA NXDOMAIN 被既有 D04 合同拒绝，尚未发送外部 Model HTTP；不据此判断服务权威 DNS 错误，不改解析器、DNS 或出站策略。原 OpenAI tools 独验、Object runtime join、SPA concurrent-publication、Jina/Image 来源等既有 STOP 与未完成范围保留。

## 配置与组合接缝

通过已有 `AGENTEAM_CENTRAL_EXECUTION_RUNTIME` 的完整 JSON 显式选择 `profile: "tool-loop-v1"`；原 `direct-text-v1` 与旧 Snapshot 保留。profile 与 Provider adapter revision 分开，无新增 UI 配置；所需字段、三项 Scheduler Launch retry 和授权前提见[后端配置说明](../backend/README.md#配置与命令)。配置不自动删 Core Tools、补 deny IDs 或授予权限。

| 提供方 | App 消费的约定 |
| --- | --- |
| Model | 原 `mc.JSONCaller`、`model.NewRuntimeWithAgentRetry` 与单一 adapter 生命周期；新 `NewToolCallingExecutionCapture` 为新执行选择 tools revision。 |
| Execution | `ProjectCallDependencies.MultiRound` 由原 Ownership 构造多轮 driver，沿原 Associated/Stop/Drain/Joined；迁移 00091 解除原单轮约束。 |
| Runner / Tool | 私有 `RunnerExecutor` / `RunnerResultAuthority`、真实 Mount resolver、原 DeviceHandler 与 dispatcher，精确配对 Execution/Round/Call。 |
| Task / UI | 既有 list/detail/runtime-view 读取最终结果；Owner 沿 Transfer 送审，不由 Execution 终态推导 Task done。 |
| App | 同 Store/Authority/Registry 装配真实提供方；构造 owner 即登记，禁新调度后等待原工作返回，再释放共享资源。 |

正常 harness 显式设置 tick 250ms / `relaunch_skip_count: 240`，提供约 60 秒 Owner 查看窗口；原 Scheduler 持续运行，产品默认及 Work 语义未改。窗口内必须只有原 work Execution；送审后允许一个独立 review Execution。现首轮要求非空工具集/required，因此 reviewer 也授权真实 readFile 和独立 Mount，无工具 Agent 尚不在兼容声明内。正常回验后按原 Runtime Stop/Drain → Runner Wait → App Join 收尾；失败/取消场景保留原配置和 observer，复用未变输入证据。

## 验证与可恢复入口

运行命令、固定 case 和资源前提只维护在 [M1 harness 说明](../../../tests/m1-app-workflow/README.md)。使用 Go 1.27.1、固定 PG17/MinIO/nginx 和锁定 Playwright 1.56.1；工具与缓存可外置共享，每轮数据库、Object、spool、工作目录和输出均独立，不连接 onboarding 现有环境。当前预算为两路 `-p=2` Go 命令、一个 native 栈。

| 固定输入与实际运行 | 结果与保留边界 |
| --- | --- |
| `fee69b0d` / `direct-text.2tWIPp`，旧 profile 兼容 | 原 outer Wait 0，1 top / 1 sub 5.38 秒 PASS；真实 Model HTTP 1 / Tool 0、v1 输出、同 DB 关闭 runtime 后重启读取、Owner 权限及原 roots/Guard/Store/peer Join 到达，三层 cleanup 完成，已获限定独立接受。 |
| `33009a96` / `run.Wxxzus`，failure-and-cancellation | 原 outer Wait 0，两个子例 11.21 秒 PASS；macro_milestones 已独立接受限定失败/取消及精确资源/PID 退场。 |
| `59dba7eb` / `run.C9sKHQ`，第八轮正常 | 原 test Wait 1；唯一 work 的 5 Model / 4 Tool 到达，浏览器正文等待 45 秒超时，未到 Owner；三层 cleanup 日志与进程消失已核。 |
| `fee69b0d` / `run.KkNSps`，第九轮正常 | 原 outer Wait 1；Owner 送审与 review 已到，刷新第三次 output 正文中止；三层 cleanup 已完成，正常末尾 Join/peer 尚未到达。 |

证据位于各轮 `output/ai/m1-app-integration/run.*`，包含 native.log、owned-resources.json 与 browser 阶段/截图；可再生输出不新增归档。独立验证负责人为未参与实现的 macro_milestones；后验资源消失不补写未执行的正常收尾断言。

前七轮 FAIL 保留于 Git 文件历史及原输出：首轮 seed 输入不合法，第二至四轮准备/首 Start 断点最终定位 SQLSTATE `42P08` 并修复；第五轮浏览器误读 detail 封套，第六轮 Owner 503，第七轮正文等待超时且零冷却导致反复 relaunch。后继修复不把这些旧轮回填为 PASS。

## 下一步

实现增量的受限 main 交付须同时满足正常链全部门、有效复用的失败/取消两门及旧 direct-text 实际回归，并由独立验证者确认；该出口仍不代表 M1 整里程碑或外部模型完成。先完成真实浏览器刷新输出的完整读取与正常收尾、peer 归属独立验收；对变更影响范围复验，已接受的失败/取消输入不变时复用。随后在原配置、权限及出站条件满足时验证外部模型实际推理。运行中重启接管、Human 持久取消、完整审批恢复与自主评审闭环继续按既定工作包推进；当前受控链进展不代替这些完成门槛。
