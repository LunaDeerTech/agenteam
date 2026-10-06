# OpenAI Chat tools wire 有限验证记录

**状态：BLOCKED。作者自测已完成；独立动态 A/B 为 NOT RUN；14 个产品候选文件保持冻结、未提交，整卡尚未业务验收。** 记录日期为 2026-10-06。本页仅归档当前证据，不改变 [rev1 规格](../work-items/recovery-d09-openai-chat-tools-wire.md) 的完成门槛，也不表示完整 D09 已交付。

独立任务启动受到自动安全筛查中断，主线程收到的原因为 `possible cybersecurity risk`。协调者确认原 driver 未启动，无运行目录、所属进程或新资源；原窗口已经关闭。原独立任务保持停止，不重试、改派或变换执行。作者原定 Budget 补验经主线程确认不受影响，已完成；它不能替代独立 A/B。后续由主线程处理解除拦截的确认与原独立任务调度。本归档没有运行或重建该受阻任务。

## 固定范围与可恢复输入

实施者为 `management_reads`，独立审查者为未参与实现的 `recovery_documentation`，真实资源协调者为 `verification_recovery`。业务依赖固定 `ecd733711caff5df46e423cadab52b32c34f785e`，规格提交为 `b2be334cd8a78b904878bdbc388a43805390f7b7`；[规格独立静审及官方来源](openai-chat-tools-wire-spec-verification.md) 已另行归档。

最终候选是 [input08](openai-chat-tools-wire-verification-evidence/author/input08.json)，SHA-256 `6f48676f7e30f37b60a92777fdf80eb674af8d4baae349a6b6274aeeeefad5f9`。[14 路径清单](openai-chat-tools-wire-verification-evidence/candidate-files.json) 与 `candidate/` 保存逐字源码文本，末检确认 live/private 都匹配；固定执行目录共 877 文件，其中 863 个未改文件逐一匹配基线 Git blob，其余为这 14 个候选。没有 SQL、迁移、依赖或生产 root 改动，也未在作者 fixture 中引入 ledger 00018。

候选在同一个 `adapter.OpenAIChat` 增加 `openai-chat-tools-v1`，覆盖原生 function tools、四种 ToolChoice、历史调用及文本结果、普通完整工具输出和多工具交错 SSE。完成标记只含元数据，不含参数取回口或工具执行能力；1 MiB 单参数、16 MiB 总内容、32 项/256 KiB 事件队列与原 64/8 Budget 均保持。具体契约归原规格，本页不另立规则。

[历史输入映射](openai-chat-tools-wire-verification-evidence/history/reconstruction.json) 用最终 14 文件和 8 份不同版本文本保留 core01、core02、race03、input04–08 等原红与最终输入，避免复制完整工作树。最早 compile01/unit02 的两个旧源码版本没有完整原件；其原日志与指纹仅作为历史记录，最终判断依赖后续 unit04 和受影响复验，不依赖重建这两个早期通过输入。

## 已有结果与剩余门槛

| 层次 | 实际结论与限制 |
| --- | --- |
| 规格与来源 | 独立静审已通过；固定 OpenAI SDK `becc1d20eed83c1b8d85e15dc131a372d9dc7813` 的 21 个限定文件与指纹已归档，无 SDK 执行或 Provider 账号实测。 |
| 作者纯测试与构建 | unit04、vet、Model/C0 编译、integration race 编译、Central/Runner build 通过；race 以保留原失败的分组证据闭合，不能写成原整包 exit0。 |
| 作者真实 fixture | real01 的 8 个未改顶层通过；唯一失败的 Budget 新测试观察信号修正后，real02 同一顶层及 4 个子例全部通过。 |
| 独立静审和纯探针 | 核心静审、原分配探针 F1 红绿闭环、input08 唯一测试差量静审通过；两顶层四子例 A/B 已离线 race 编译。 |
| 独立真实 A/B | **NOT RUN / BLOCKED**。没有动态 raw、实际退出或业务通过结论；编译、静审、作者结果均不替代它。 |
| 产品提交及接通 | 14 产品路径未提交；Runtime、Resolver tools 绑定、凭据 read/release 分派、工具授权/执行、账本消费与生产 Provider 调用均未接通。 |

原命令、环境、输入与 raw 保存在 [作者最终报告](openai-chat-tools-wire-verification-evidence/author/author-report.md.txt) 和相邻同名 JSON/log。固定 Go 1.27.1、离线 readonly 模组、自有 GOCACHE/TMPDIR；真实检查沿原 `scripts/test-security.sh` 的 `-race -count=1 -timeout=6m`，没有扩大 fixture 或产品预算。

## 原始失败与修复关系

| 原件 | 实际失败与后续证据 |
| --- | --- |
| [race03](openai-chat-tools-wire-verification-evidence/author/logs/race03.log) | 新 pure helper 误用旧 helper 的 10s 期限，两支 16 MiB 失败。只增加日志的诊断均确认原 ctx deadline exceeded、timeout/wire_transport_error；固定原 11 源绑定保留。仅新 helper 改用已有正式 120s 上界，精确上界/+1及进入扫描后的三阶段取消断言保留，后续通过。 |
| [独立 F1 原红](openai-chat-tools-wire-verification-evidence/independent/core01-name-red02/raw.log) | 非法 8 MiB Tool.Name 在验证前被 clone，分配 8,392,792 B。先完成 native name/C0/schema 校验再构造 policy 后，[同一探针](openai-chat-tools-wire-verification-evidence/independent/core02-name-green01/raw.log) 分配 4,232 B 并通过；原 probe SHA 不变。最早 runner 准备错误也保留，未计为产品失败。 |
| [race04](openai-chat-tools-wire-verification-evidence/author/logs/race04.log) | 新工具组通过，旧 TestOpenAIChatSSECumulativeTextBound 在原 10s 期限失败，整包 exit1 保留。完全相同输入仅该旧顶层 `-race -count=3` 实际 8.78/8.26/7.76s 通过。根因未确定，不能认定并发负载；共享 unicode 扫描增加 ctx 检查成本这一生产差量明确保留。 |
| [manifest06](openai-chat-tools-wire-verification-evidence/author/logs/manifest06.log) | 修正 manifest 的 Protocol/Profile 时，新 pure 测试漏声明字段，编译失败。input07 补声明后定向 race 通过；生产始终使用正确 C0 常量。 |
| [real01](openai-chat-tools-wire-verification-evidence/author/real01/raw.log) | 新 Budget 子例等待 D04 不会调用的 GotFirstResponseByte，5.03s 触及原观察上界，未进入取消断言。只读 D04 手动解析响应头，因此这是测试阶段信号错误，不能视为生产取消失效。 |
| [input07→08](openai-chat-tools-wire-verification-evidence/author/input07-to08.diff) | 只改新 Budget 测试，使用成功 Decision 的非 nil/Sent/空 Reason 联合 owned server 一请求、active1、completed0、连接未闭；hold1 还必须实际交付非空 ToolDelta。原 5s 观察、未 Joined、取消/PartialOutput、一次错误后 EOF、actual join、一次请求均保留。独立静审及 compile08 通过，real02 实际通过。 |

此前 input04→05 的新 stream 断流预期改回既有 `network/wire_transport_error`，发生在首次真实运行之前；input05→07 仅 manifest 和对应 pure 字段绑定变化。每段差量与原输入均保留；未变检查按语义复用，没有覆盖或改写原红。

## 作者真实结果与资源

real01 的 tools HTTP、tools SSE、原 text 三顶层、原 structured 三顶层共 8 个顶层通过；Budget 顶层内背压、真实阻塞 writer、三修订同具体 Budget 的 3 个未改子例也通过，但该轮整包及 driver **exit1**。实际 320 KiB 参数按不超过 64 KiB 的片段交付；完整 1 MiB/16 MiB 极限属于 pure parser 证据，未宣称突破原 512 KiB fixture 上限的网络验证。

[real02 命令](openai-chat-tools-wire-verification-evidence/author/real02/command.json) 仅选择 `^TestModelOpenAIChatToolsCloseAndBudget$`。2026-10-06 02:03:01.390025 UTC 启动，02:03:57.731079 UTC 结束，driver **exit0**；1 顶层/4 子例全 PASS，tests/model 2.857s。原始日志分别证明 hold0 已解析头但未交片段、hold1 已交非空工具片段，均 caller live、服务端 held/open、未 Joined；之后原取消、PartialOutput、EOF、wireClose/settled actual join 断言通过。

| 窗口 | 原始退出与精确清理 |
| --- | --- |
| [tools-author-01](openai-chat-tools-wire-verification-evidence/resources/docker-window-tools-author-01.json) | driver exit1；4 容器+3 网络共 7 个 exact IDs，两次全部 absent；原 2 容器/4 网络 ID/name/labels 不变，owned PID0/runtime 空，observer 无错误，协调者接收归还。 |
| [tools-author-02](openai-chat-tools-wire-verification-evidence/resources/docker-window-tools-author-02.json) | driver exit0；同样 7 个实际观察 ID 双次 absent、原基线不变、PID0/runtime 空、observer 无错误，02:04:30 UTC 协调者接收归还。 |
| [tools-independent-01](openai-chat-tools-wire-verification-evidence/resources/docker-window-tools-independent-01.json) | 授窗后受筛查中断，driver 未启动；协调者两次只读确认无 run/PID/tmp/新资源，基线不变，01:59:42 UTC 关闭为 NOT STARTED。它不是一次动态通过。 |

作者双清原件在各 `real01/cleanup.json`、`real02/cleanup.json`，包含 exact IDs、时刻和进程/目录观察；没有仅以脚本打印 success 代替清理证据。[最终末检](openai-chat-tools-wire-verification-evidence/author/final-author-checks.json) 绑定冻结输入与源闭包。

## 独立证据与归档使用

[独立核心报告](openai-chat-tools-wire-verification-evidence/independent/core-review.md.txt) 截止 input07；[input08 差量报告](openai-chat-tools-wire-verification-evidence/independent/input08-delta-review.json) 只核新 Budget 测试。A/B 原 probe、overlay、selector、driver、716 文件编译闭包及离线 compile02 原件仅证明准备和编译，明确 **NOT RUN**。主线程接收的自动筛查原因和禁止重试状态见上述窗口原件，不推断未执行场景的行为。

已提交 ledger `36e5ff1` 的只读接缝核对表明 identity 仅新增 ModelRuntime 注册值，既有类型与值不变；本卡继续使用固定 ecd 输入，未冒称 ledger/tools 组合实跑。Object 历史 join 缺陷、Summary 产品决定、其他协议与生产编排边界保持。

[归档说明及只读哈希检查](openai-chat-tools-wire-verification-evidence/README.md) 列明恢复方法。`verify_archive.py` 只读取本地文件，检查归档、14 候选与历史输入 SHA；不执行 Go、Docker、SQL、SDK或保存的驱动脚本。所有原件逐字保存，缓存、二进制、完整源码树和运行资源没有纳入归档。
