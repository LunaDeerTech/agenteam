# M1：默认 App 的单 Agent 工具工作闭环

2026-10-14 开始实施，起始基线 `c75e1bb8`，集成负责人 delivery_path。目标与正式门槛沿[开发计划](../development-plan.md#当前开发目标与里程碑)。`tool-loop-v1` 受控 Provider 的正常工具链、浏览器送审与收尾，结合失败/取消及旧 profile 回归已按受限范围交付 main。本次真实模型的连续工具工作、可见成果、Owner 送审与正常退出已获独立接受，M1 已达到定义的阶段出口。M2 已另树保存推送并继续暂停。

## 当前真实模型验收

保留默认 HTTP_PROXY/HTTPS_PROXY/NO_PROXY 与 CA/TLS 配置时，真实 chat 和 embedding POST 均已返回 HTTP 200；历史本地目标 DNS 失败不能推出服务不可达。正式出站使用显式精确 HTTPS Origin 可信代理，目标 DNS 委托网关且正常校验目标 TLS；JSON tools 对可选 `reasoning_content` 作有界验证后丢弃，响应头预算窄修已通过受控慢首包及独立验证。当前实现、真实调用结果与限定证据见[live 验收卡](m1-live-acceptance.md)和[独立复核](m1-live-proxy-review.md)，不外推 embedding 产品全链。

真实模型按 Task 目标在同一 work Execution 连续读取 `input.json`、写入 `stats.py`、运行程序并读取 `result.json`；实际文件/统计结果、stdout/exit、持久调用/结果配对和最终 v2 均已验证。浏览器完整成果消费、唯一 Owner 显式送审并刷新至持久 `in_review`，以及原 Runtime/Runner/App Join 和精确资源退场已获[独立接受](m1-live-proxy-review.md#第四轮真实-m1正式出口接受)，M1 真实工作验收通过；接受范围限本次 Provider 配置与目标场景。review 在正常收尾时取消，仅证明送审接续，不证明自主审核决策或 Task 完成。

本次保留 300 秒总预算与 12 sent 软阈值；预算 observer 的纯测/race、真实 PG 合成计数和真实 App deadline 停止/Join/无后续派发已获[限定接受](m1-live-proxy-review.md#预算补验限定接受)，合成记账行不计作真实模型调用。前三轮 live 的原 FAIL 保留，后继成功不回填失败，也不反推旧超时的具体阶段。

## 当前成果与验收边界

默认 App 使用正式 Project/Agent/Task 配置，由原 Scheduler 自动启动同一 Execution。自有确定性 Provider 经真实 TLS HTTP 依次请求读取 `input.json`、写入 `stats.py`、运行真实 Python、读取 `result.json`，最后给出文本：同一工作 Execution 精确五次 Model / 四次 Tool，真实文件、stdout/exit 0、持久调用/结果配对及最终 `durable_final_text_v2` 快照均已到达。既有详情 UI 支持旧 v1 和新 v2，Execution 成功不自动使 Task done。

`4ca30370` 的第十轮完整通过：浏览器三次读取原 work 成果，唯一 Owner 送审 HTTP 200，Task 持久化为 `in_review` / version 4，reload 后原成果保持。原 Runtime Drain → Runner Wait 0 → App 全 owner Joined 与所有连接 Joined 均完成，最终精确核对一个 work Execution 的五次 Model / 四次 Tool，以及一个 review Execution 的两次 Model / 一次 read。Reviewer 执行成功只表示读自己的说明文件并确认收到任务，不证明 worker 成果传递、自主评审决策或 M2 完成。

失败/取消的独立两个子例已通过并获限定独立接受：真实 read missing 无载荷使 Execution `failed/runtime_failed`；真实命令 marker 后调用原 Runtime StopAdmission/Drain，使 Execution `cancelled`，并确认原子进程、进程组退出。两者保留原 pending bytes，完成同事务终态、精确 1 Model / 1 Tool、Runner 原 Wait 0 与 App 全 owner Joined。

这些结果证明自有受控 Provider 的协议、领域调用和 Runner 副作用；外部模型的当前证据另见[真实模型验收](#当前真实模型验收)。此前 `DEV_CHAT` 的本地 DNS 预检失败保留当时范围，不覆盖后继显式可信代理路径。原 OpenAI tools 独验、Object runtime join、SPA concurrent-publication、Jina/Image 来源等既有 STOP 与未完成范围保留。

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
| `4ca30370` / `run.EW1axd`，第十轮正常 | 原 outer Wait 0，14.58 秒 PASS；三次成果消费、唯一 Owner 送审、刷新一致、正常 Drain/Wait/Joined、精确 peer 归属全部到达；三层 nonce cleanup 及两个自建依赖链接清理完成，已获限定独立接受。 |
| `fee69b0d` / `direct-text.2tWIPp`，旧 profile 兼容 | 原 outer Wait 0，1 top / 1 sub 5.38 秒 PASS；真实 Model HTTP 1 / Tool 0、v1 输出、同 DB 关闭 runtime 后重启读取、Owner 权限及原 roots/Guard/Store/peer Join 到达，三层 cleanup 完成，已获限定独立接受。 |
| `33009a96` / `run.Wxxzus`，failure-and-cancellation | 原 outer Wait 0，两个子例 11.21 秒 PASS；macro_milestones 已独立接受限定失败/取消及精确资源/PID 退场。 |
| `59dba7eb` / `run.C9sKHQ`，第八轮正常 | 原 test Wait 1；唯一 work 的 5 Model / 4 Tool 到达，浏览器正文等待 45 秒超时，未到 Owner；三层 cleanup 日志与进程消失已核。 |
| `fee69b0d` / `run.KkNSps`，第九轮正常 | 原 outer Wait 1；Owner 送审与 review 已到，刷新第三次 output 的 Playwright 正文观测为 `ERR_ABORTED`，完整消费未在该轮证明；三层 cleanup 已完成，正常末尾 Join/peer 未到，不回填 PASS，也不据此断言 native 正文不完整。 |

每轮原始输出位于 `output/ai/m1-app-integration/`；本次必要 native.log、owned-resources.json、browser 消费证明/阶段及截图由 delivery_path 保全到主工作区 `/workspace/agenteam/output/ai/m1-delivery-evidence/app-integration/` 的原轮次目录，保全材料已逐文件 bytes/SHA 核对确认，不带二进制、缓存或私有 Session。跟踪源码、锁文件与运行说明是正式恢复入口，不依赖待清理的临时工作树。独立验证负责人为未参与实现的 macro_milestones；后验资源消失不补写旧失败轮未执行的正常收尾断言。

macro_milestones 已独立接受上述正常、失败/取消与旧兼容组合。本次正常链的 11 条联合证明全部取得 `requestfinished`，原 Go 的同 RequestID、完整 EOF/Close/handler 与正文 digest 一致；未借 `ERR_ABORTED` fallback 形成 PASS。该结论仅覆盖本次受控实现增量。

前七轮 FAIL 保留于 Git 文件历史及原输出：首轮 seed 输入不合法，第二至四轮准备/首 Start 断点最终定位 SQLSTATE `42P08` 并修复；第五轮浏览器误读 detail 封套，第六轮 Owner 503，第七轮正文等待超时且零冷却导致反复 relaunch。后继修复不把这些旧轮回填为 PASS。

## 下一步

M1 按本卡定义的单 Agent 真实工作出口结项，未变的受控失败/取消与旧 direct-text 证据继续复用。下一产品优先级为 M2 的成果传递与审核通过/退回；M2 已保存工作继续暂停，本次不恢复。无工具 Agent 兼容、运行中重启接管、Human 持久取消、完整审批恢复与自主评审闭环仍按后继工作包完成，M1 通过不替代这些门槛或完整 D09/D22/D28 验收。
