# 团队任务台账

本页只保存全局产品状态、阻塞和下一步；任务分支的最小恢复状态见[分支记录](../../../.agent-state/current.md)。2026-10-09初始核对基线 `e55ad7d1` 仅做文档及文件存在性核对；下表已按后续实际交付与验证更新，不代表当前HEAD的一次全量测试。

长期范围与完成门槛见[开发计划](../development-plan.md)，协作规则见[团队流程](README.md)，环境与缺失输入见[恢复说明](recovery-2026-10-08-environment.md)。历史代理名、ACK、资源授权和 STOP 都是当时记录，不表示本次仍有活动实例、进程或可用测试窗口。

## 当前恢复点与并行所有权

Task Planning与B0-P限定Blocker服务已正式交付，当前继续D27 Model业务验收与Work Owner HTTP/root实施。主线程负责统筹、规划、安排、汇报、验收决策和Git；实现、排查、测试与文档由子代理承担。实际所有权与资源接续见分支记录，共享入口、迁移、构建资产与测试资源只允许一个写入者或明确串行。

| 工作项 | 当前可证状态 | 接手时的下一步与完成边界 |
| --- | --- | --- |
| [D01 低层资源身份](../work-items/d01-resource-identities.md) | **R1纯契约已实现并独立验收**。Tool/Mount/ProjectVariable三个canonical marker与Foundation ID alias，四依赖race/vet及独立3顶层6子例通过。 | 仅标量声明与类型消费；Tool/Mount/Variables真实目录、引用保护和Agent F1仍未闭合，普通与Secret共用变量ID且不同CredentialID。 |
| [D10 Agent 核心与引用](../work-items/d10-agent-configuration.md) | **C1六纯契约已实现、独立验收并正式交付main**。17键AgentCore、3键AgentRef、strict codec/Clone/直接日志投影；作者pure/race/vet与5组独立pure/race通过。 | 仅纯类型与WorkReferences声明。F1真实创建/初始化/Model引用/目录/引用保护未实现；R1已闭合ID定义位置，Tool目录窄投影及其余真实前置未闭合，没有真实Agent或Task指派绑定。 |
| [D11 Work Structure](../work-items/d11-work-structure.md) | **本卡已完成**。四 contract `c3b1ee72` 与实现 `a64fb5e7` 已提交；18 路径均存在，限定技术域至本次基线无差异。 | 可供 Task planning 消费。全18按明确版本组合接受；保留 Unknown01 原 FAIL、U1 修复与独立 B 工具终态缺口。不是当前 HEAD 单次全量测试或完整 D11。 |
| [D11 Task planning](../work-items/d11-task-planning.md) | **本卡规划库已实现、独立验收并正式交付main，完整D11未完成**。契约、三个Human规划命令、Lookup、当前Get/List、同Tx membership、TaskEvent/Outbox及00022已完成；原enclosing日志FAIL与真实投影边界保留。 | 七新PG顶层、四旧Project/Outbox、四旧Structure及独立A/B按限定版本组合已实际通过并完成资源终态；原观察器/非法Project输入/rank fixture/归档输入FAIL保留，测试修正的真实对照与完整top已通过。状态机、指派、执行、删除、生产root与完整D11均未完成。 |
| [D11 Task 流转](../work-items/d11-task-transitions.md) | **T0a与T0b纯契约已实现并独立验收**。状态/角色/Position及Transfer/Lookup/摘要、Human typed历史、严格封套与纯多事实工厂已完成；作者及独立pure/race/vet通过。 | 仅纯决策与类型，不提供授权或提交能力。T0b仅消费B0-C两类；独立公开API各6顶层34子测试实际通过。运行服务仍须真实Agent/Blocker/occupancy与执行事实；完整D11未完成。 |
| [D11 Blocker 纯契约](../work-items/d11-task-blocker-contracts.md) | **B0-C两类纯契约已实现并独立验收**。唯一BlockerID、rely_on/无引用waiting_for_human typed metadata、Create及小payload；strict codec/cap/Clone/log，作者及独立pure/race通过。 | T0b只可消费已接受两类；其余三类和外域引用保持未绑定。B0-P持久服务见下行，跨状态组合与完整流转仍未完成。 |
| [D11 backlog Blocker 持久服务](../work-items/d11-task-blocker-service.md) | **限定服务已实现、独立接受并正式交付main**。Human Owner对未指派backlog Task的两类Blocker可add/resolve/list/lookup；真实完整图、Task版本及同事务历史/Outbox/Activity/receipt与00023已完成。 | 作者pure/race/vet/build、公开契约与runtime独验、真实权限/并发/Unknown/互操作/SQL回滚及旧Planning/Structure回归通过并完成资源终态；独立装配审查已接受。首Persistence整轮FAIL保留，以未受影响子项与修复后的History定向回归组合接受。非backlog、Agent/Executor、HTTP、生产root及完整D11仍未绑定。 |
| [D11 Work Owner HTTP/root](../work-items/d11-work-owner-http.md) | **rev2规格已独立接受，实施中**。消费已接受的Structure、Task Planning与两类Blocker服务，为已有有效Project Owner提供正式HTTP规划能力。 | 分页、九码Problem与根生命周期库片段已可构建并通过限定pure/race，Problem独立静审接受；HTTP/root及真实动态验收尚未完成，无新迁移。不包含Project创建、UI或Task状态流转。 |
| [D27 Project Model Settings UI](../work-items/d27-project-owner-model-settings-ui.md) | **前端与harness已恢复，业务验收未完成**。六新case历史通过recovery/read/configuration/credential四项；authority第7轮与navigation第5轮仍FAIL。第7轮原45s case timedOut、具体子步骤未知，实际资源已退役，但原terminal未单列runtime_empty不得补PASS；旧Session/ProjectNav与焦点失败保留，不能把共享组件修复或旧业务PASS外推为新资产验收。 | 旧14的owner-edit、owner-recovery、owner-identity与audit-authority已真实完整PASS并完成资源终态，现已过4项，余10项待验；独立A第二轮完整PASS且资源已清；首轮credential-rotation的完整FAIL及原原因未确证边界保留，B待验。请求读取诊断返修已过类型/bundle及20项独立纯控制，有限审查接受，不证明旧间歇Session失败已修复；recovery/configuration/credential须补受影响验证，read仅复用有效原证据。Session八控制及runner纯控制只证明限定行为；原监督器runtime尾FAIL与后验清理分开保留，不放宽finished/EOF/身份门槛。 |
| [D08 Project 与 Owner](../work-items/d08-project-owner.md) | **部分子能力接受，模块未完成**。基础权限/生命周期事实、Owner 读写 HTTP、Audit 与初始化收敛已有交付。 | 完整 lifecycle 推进/清理、Artifact/Object 组合、Skills 初始化与创建 HTTP/root 仍未闭合；Object join 是实际阻塞，不能用空适配器跳过。按[领域绑定卡](../work-items/recovery-project-domain-bindings.md)定位剩余接缝。 |
| [D09 Model System](../work-items/d09-model-system-token-usage.md) | **部分库与 HTTP 接受，模块未完成**。已有配置、Owner/Usage HTTP、Summary、Chat text 与 Embeddings/平台选择 Resolver 等限定成果。 | 生产 Resolution/Invocation 与 consumer 仍未绑定；OpenAI tools 独立动态验收停止，Jina/Image 来源缺口另列。不能把 wire、受控 fixture 或 UI 通过写成真实 Provider/生产调用完成。 |
| [D10 Skills 初始化](../work-items/d10-skills-initialization.md) | **P1 已交付，真实服务/绑定未完成**。builtin/不可变包 `8872110` 与 Project 初始化收敛 `39ebd57e` 可复用。 | 真实 Skill 服务、PG/Object 发布、D08 创建/生命周期与 Agent/Tool/Runner 绑定待实现或验收；Object join 阻塞相关真实组合。旧卡“ProjectFactAuthority 不存在”的时点描述已被 Object Audit/初始化收敛前置取代，接手应核现有接口。 |
| [D12 Knowledge](../work-items/d12-knowledge-documents.md) | **纯契约/共享补口接受，B02 主体未恢复**。B01 `914fd84`、C1 `71dc176`、C2 `f401c15`、C3 `231a384` 已交付；当前仅12个 Knowledge contract 文件。 | 按[B02完整服务卡](../work-items/d12-b02-knowledge-service.md)恢复 Human canonical 内容/树服务，核现有 Owner/Object/Audit/Outbox 接口；原29路径主体和未编号 SQL 草案不可当现存实现。迁移须核当前全局序列，D13 不阻止 canonical 新领域准备，但真实组合依赖仍须满足。 |

D08–D28 各模块仍有未完成范围，E01 未开始；首个未完成模块编号为 D08，但只消费已接受子能力的其他完整结果可并行。Task/Work HTTP、UI/App/生产 root，生产 Skills/创建 HTTP、Resolution/Invocation 及 D24 均未完整绑定；现有 `ready=false` / `/readyz` 503 的产品边界不因本次文档整理改变，隔离fixture中的真实root启动与局部测试不证明生产部署或全平台ready。

共享浮层遮挡修复已独立验收：`useLayer` 按激活栈同步 Dialog/Drawer/Popover 绘制层级，浅深色16个真实组件场景与完整前端2632单元/类型/构建检查通过。见[限定结果](../work-items/d27-project-owner-model-settings-ui.md#02-共享浮层遮挡修复)；D27剩余业务验收仍未完成。

确认关闭后原输入同tick解除禁用的焦点恢复修复已独立验收：仅对原节点安全延后重试，后继焦点/层栈变化不抢焦；114组件单元、8项独立定向检查、完整2640单元/类型/构建及21真实浏览器场景通过。见[限定结果](../work-items/d27-project-owner-model-settings-ui.md#03-确认关闭后的原焦点恢复)。旧浏览器失败及清理证据缺口保留，D27权限/导航真实业务仍未完成。

## 可复用的已交付前置

以下入口证明各自限定成果，不要求新会话重扫原件或重新运行全套。报告中的固定版本组合、历史失败和未验证范围仍有效；使用时只核与当前目标有关的接口及差异。

| 能力 | 已有交付与证据入口 | 不能外推的范围 |
| --- | --- | --- |
| D00–D07 基础与账号 | [D01 契约](../work-items/d01-contracts/README.md)、[D07当前进度](../work-items/d07-account-session-smtp.md#当前进度)；D07末件 `0ed8085` | 既有组合验收不等于当前 HEAD 单次全绿；后发现的 Object join 缺陷仍保留。 |
| Project Owner 读写 | 读取 `901eb546`、更新 `61bed1fc`；[读取验收](project-owner-read-http-verification.md)、[更新验收](project-owner-update-http-verification.md) | 不包括完整 Project lifecycle、创建 HTTP 或生产 Skills。 |
| Project Model 配置与凭据 | Owner 读取 `a0b012ce`、凭据 `e4b1b891`、配置写入 `cc850b22`；[配置写入验收](project-model-configuration-write-http-verification.md) | HTTP 接口接受不替代未完成 Model Settings 浏览器验收。 |
| Project 初始化收敛 | 六路径 `39ebd57e`；[验收](project-initialization-convergence-verification.md) | 提供必要授权/收敛前置，不接通生产 Skills 或创建 HTTP。 |
| Project Owner 工作区 UI | 全24限定版本组合；[最终验收](project-owner-workspace-ui-final24-verification.md) | 不包含完整 D26/D27、Project 创建/lifecycle 或生产 SPA 发布。 |
| Project Owner Audit HTTP/UI | HTTP `4089d131`；UI全22路径，技术 `75411e27` 与README `5fba4510`；[HTTP](project-owner-audit-http-verification.md)、[UI验收](project-owner-audit-ui-verification.md) | 新 Audit 完整 EOF/schema/client 与旧 Owner response-event 重放是不同证据，不合并为当前 HEAD 全测。 |
| Meeting Summary 与平台 Embedding | [Summary S1](system-meeting-summary-selection-verification.md)、[S2设置](system-meeting-summary-settings-verification.md)、[S3解析](system-meeting-summary-resolution-verification.md)、[Embedding九路径验收](platform-embedding-resolution-verification.md)，Embedding交付 `c36717c2` | 生产 consumer、Serving、Invocation 与 D24 仍未接通。 |
| Work Structure | [全18验收](d11-work-structure-verification.md)，实现 `a64fb5e7` | Task、Agent、执行占用、Sprint lifecycle 与生产 Work root 不在本卡结果内。 |

## 未完成验收与必须保留的失败

**Model Settings UI：** 历史 `modelsconfig01/02` 均失败；`modelsconfig03` 与 `modelscred01` 后续有限通过。`modelsrecover01/02` 仍为 FAIL：前者缺原 native/DOM/final facts；后者虽到达 Credential 断连恢复、Model DELETE cut/lookup/replay 等步骤，最终 browser-result/final durable 缺失，原 host TCP 未在原期限内双清。后续观察或窗口释放不能补写原通过。受控 header/Flush 与 ReadPrivate 同 fd 快照探针通过只证明限定行为，不确定旧 business FAIL 的具体原因。

`modelsrecover03`、author v06/private v04的历史准备描述不构成动态通过。当前主harness与独立A/B可执行输入已恢复并保存，独立A第二轮真实完整PASS且资源已清，首轮FAIL保留，B尚未真实运行；共享组件修复后按相关差异补验，不能把旧二进制、旧sourcecheck或前端unit代作真实浏览器结论。详细场景与实现范围以[当前卡](../work-items/d27-project-owner-model-settings-ui.md)为准。

**Work Structure：** 六新 PG 按版本组合通过，Unknown01 原 FAIL 经 U1 Lookup 取消修复后 Unknown02 11/11；五旧回归及独立 A/B 实际断言通过。独立 B 的监督器 Wait、2个资源清理与 watcher join 有记录，但外部工具 `session20487` 恢复后返回 Unknown process，原 terminal/exit 缺失。后续 current-clear 只证明当时 PID/资源不存在，不补原工具终态、不推断机器重启；完整接受仍沿[报告中的限定组合](d11-work-structure-verification.md)。

其他历史原 FAIL 与受限组合结论保留在对应验收报告及基线文件历史；这里不复制每轮日志，也不把后验通过改写为原失败原因已知。

## 停止项与真实阻塞

| 停止项 | 当前边界 | 恢复所需条件 |
| --- | --- | --- |
| Object runtime join | [已复现退出屏障缺陷](object-runtime-join-regression.md)；[修复卡](../work-items/recovery-object-runtime-join.md)原实施停止，缺陷未修，Artifact最终共享guard/Project组合受阻。 | 保持原停止，不自动改派或重试；明确恢复范围后按正式修复契约补实现和独立验证。 |
| OpenAI Chat tools 独立验收 | [有限验证记录](openai-chat-tools-wire-verification.md)：作者结果与候选保存，独立真实 A/B NOT RUN/BLOCKED，产品未接受。 | 保持受阻任务停止；不能由作者自测、离线编译或重建driver替代独立动态结果。 |
| Central SPA concurrent-publication | [SPA卡](../work-items/d28-central-spa-hosting.md)与[规格记录](system-central-spa-hosting-spec-verification.md)保留脚本原失败与阶段边界；并发发布验证停止，生产托管未接受。 | 文档/环境恢复不解停，不以私有 Vite build 或测试资产替代生产发布验收。 |
| Jina rerank 来源 | [来源准备](jina-rerank-frontier-2026-10-08.md)：五次请求失败，未取得有效官方字段正文、固定版本与许可。 | 后续只有明确恢复来源工作并取得可用资料后才冻结协议实施；不推测字段或绕换通道重试。 |
| OpenAI Image 来源 | [来源准备](openai-image-source-preparation.md)：首轮DNS失败，标准代理CONNECT 403；四件固定SDK源码未取得，字段仍未知。 | 固定资料缺口先解决；Git push可用不证明原读取通道可用或已重新授权，不自动重试。 |

## 已定产品规则与后续边界

- Meeting Summary initial/update（含首轮标题）由系统管理员统一选择；Project 不 override 或复制初值，compaction 与 Execution Summary 沿各自规则。见[Summary解析卡](../work-items/d09-system-meeting-summary-resolution.md)及[Meeting](../../architecture/meeting/README.md)。
- Human Owner 权限、Project 当前事实与管理员 System scope 分开；管理员没有 Owner 旁路。Task planning 本卡只做未指派 backlog 规划与必要事件，不能以无执行的假适配器冒 D10/Executor/Scheduler 绑定。具体规则归各当前任务卡及[Work架构](../../architecture/project-work-management/README.md)。
- 既定品牌/布局不因恢复重设；正式来源见[前端设计](../../frontend-design/README.md)。界面或某张卡完成不意味着完整 D26/D27 或系统 ready。
- [E01](../work-items/platform-game-acceptance.md)仍待平台全部前置完成。Minecraft 或 Terraria 的确切版本、完整内容分母、权重及关键系统门槛尚未冻结；必须通过实际平台协作、可运行游戏、至少50%覆盖率和真实试玩，不能提前写成已选或已验收。

## 接手步骤

1. 按[团队流程](README.md)先 fetch origin，检查 `ai/*` 分支和 [current.md](../../../.agent-state/current.md)，能定位任务就自动接续；再核分支、提交、实际差异与上表目标卡，dirty 改动先保存，不覆盖其他任务工作。
2. 后续接续Model业务验收与下一就绪产品结果时，由对应一级负责人组织子代理执行；核对共享文件/构建/Go缓存/测试资源所有权，局部细节与返修由负责人处理，无需恢复旧实施或重建已存在harness。
3. 先恢复必要源码与工具条件，再运行有意义的自测及按风险独立验证。必要源、harness、probe 进入正式仓库或 `.agent-state/<task>/`；原失败与未验范围保留，可再生原始日志放忽略的 `output/ai/`，不可再生的必要材料随 checkpoint 保存。
4. 中间结果由主线程按[自动 checkpoint 规则](README.md#自动保存与跨设备恢复)在 `ai/<task>` 批次提交并推送，不等待整体验收或用户提醒；下级交出可恢复片段并停止相关写入。完整结果正式验收后交付 `main`，保存记录不代表PASS，也不替代真实测试与资源终态。

## 历史查询与兼容入口

逐轮授权、旧所有权和哈希清单已从当前入口移除，完整原文保存在 `e55ad7d1` 的文件历史，原验收报告及证据目录不删除：

```sh
git show e55ad7d1:docs/development/agent-team/tasks.md
git log --oneline -- docs/development/work-items/d11-task-planning.md
```

### B03 当前局部验证与所有权

此标题保留供旧恢复记录入链定位。它原指 2026-10-05 D08 B03 的 A/P 局部结果：原 Transfer 收敛后段失败，局部通过不等于生命周期整体接受。当前应读上表 D08 与[领域绑定卡](../work-items/recovery-project-domain-bindings.md)；历史细节用上面的 `git show` 按本标题查询，旧执行者和资源分配不延续到当前任务。
