# 产品完整性复核与后续路线

2026-10-14，基线 `d3e723d4`。本轮响应用户对前端简陋、信息与操作堆叠、架构落实不足，以及 M3 到完整交付仍有巨大距离的反馈。交付范围是**梳理、路线修订与前端设计技能准备**；没有实施以下 UI、领域或生产交付补齐。目标、依赖和 R1–R5 工作包出口以[开发计划](../development-plan.md#当前开发目标与里程碑)为唯一来源，动态状态见[任务台账](../agent-team/tasks.md#当前目标与工作包状态)。

## 结论与证据边界

M1/M2 是已成立的核心纵向链：真实模型与 Runner 工具工作、worker/reviewer/Human 交接、退回后修订接受，以及限定审批、取消与恢复。其[正式接受范围](m2-task-collaboration.md)与[独立审查](m2-independent-review.md)保持，不重新判为未完成；这些结果也没有覆盖完整产品。现有前端具备正式页面、共享组件和恢复逻辑，问题包括既有设计落实不均、用户流程分散，以及尚未实现的领域与入口，不能只靠视觉调整解决。

M3 仍包括完整检索/Memory、领域工具、Meeting、MCP 和 Runner 扩展等多条长链。即使 M3 全部通过，完整生命周期、领域与生产装配覆盖、跨页体验、发布升级及运行恢复等 M4 出口仍须单独闭合。里程碑数量、完成卡数和旧粗估比例均不能推算平台进度。

| 证据类型 | 本轮如何使用 | 不能推出什么 |
| --- | --- | --- |
| 正式契约 | 架构、前端设计及 D01–D28 的既定范围 | 文档存在不表示实现或用户体验通过。 |
| 历史限定接受 | M1/M2 与各工作卡的实际通过范围；输入未变时复用 | 不扩大为当前 HEAD 全测、全模块接受或默认生产开启。 |
| 当前静态观察 | 当前源码、路由、App 装配与调用闭集 | 能证明可见结构与明确分支；未运行的场景不记动态失败。未检出也不等于完整仓库不存在。 |
| 本轮浏览器观察 | 正式 Vue 页面、Playwright 1.56.1、合成 HTTP fixture 下的代表呈现与局部交互 | 无真实数据库、Runner 或模型；不能代替实际后端联调、完整键盘或所有状态验收。 |
| 待设计或未验证 | 明确范围与下一项要回答的问题 | 不把缺少证据写成已发现故障，不凭此新增产品规则。 |

本轮浏览器首脚本取得 8 张代表截图并退出 0，观察中未见非预期请求、pageerror 或 console error；增补取得 Agent 配置第 9 张图后，返回 Task 等待接受按钮可用超时并退出 1，未执行到键盘 Escape 断言，原因未定。首张桌面评审图采于读取尚未完成时，不能据禁用按钮推断永久不可用。两轮浏览器与 Vite 均已关闭，端口无 listener。图像只覆盖可见滚动区域；本轮没有新增后端、真实模型或全产品 PASS。

## 能力完整性矩阵

每行同时核对契约、当前实现与绑定、用户路径和证据缺口。下面是面向派工的宏观覆盖，不代替各模块开工规格或逐接口审计；发现静态明确缺口后可直接安排对应小范围实现，不要求先把全表设计完。

| 能力域与既定范围 | 已有实现、真实绑定与用户路径 | 本轮发现的差距及证据 | 对应后续包 |
| --- | --- | --- | --- |
| 核心项目、Agent、Task 与 Skills（D07–D11/D23） | 账号、项目创建及 Skills/Object 初始化、Agent 配置、Task 指派与正常 Scheduler 已形成真实 M1/M2 组合。不能继续按旧卡写成全部 unbound。 | **静态明确缺口：** active Execution 的新 Skills assignment 可入 queued，但 `multi_round_advance` 遇 queued 返回 `CapabilityUnsupported`，尚未闭合下一模型轮次生效要求；不影响既有初始 Skills 成果。见[当前推进代码](../../../internal/central/execution/multi_round_advance.go)、[执行上下文契约](../../architecture/agent-executor/execution-context.md)。完整 Work/Scheduler 仍按原门槛核齐。 | R1/R3 |
| 模型与执行引擎（D09/D22） | 显式配置的 ToolLoop 已接真实 Model、Scheduler、Runner 与结果消费，变量/Secret、Mount、初始 Skills 已进入执行装配；默认 Runtime 关闭是配置边界，不是没有实现。 | **静态与未验证：** 当前 [Model Runtime](../../../internal/central/model/runtime.go) 依赖 OpenAI Chat adapter；[模型契约](../../architecture/platform-infrastructure/model-system/README.md)所需 Anthropic 等完整范围未由现链闭合。[当前 RoundInput](../../../internal/central/execution/contract/round_input.go)拒绝非 nil `CompactionRef`；M2 的安全 Tool checkpoint 冷恢复不代 in-flight Model、全部 checkpoint、长期 waiting 内存释放或完整 budget/watchdog。按原 D22 场景补齐，未在本轮动态复现。 | R3/R5 |
| 工具与治理（D18/D19/D21） | Operation/Attempt、统一 Runtime、Task/Artifact Builtin、Runner read/write/run、Human 单操作审批与持久取消已有真实链。 | **静态明确缺口：** [当前 Builtin 装配](../../../internal/central/app/execution_builtin_runtime.go)集中在 Task/Artifact，[工具目录](../../architecture/tool-system/README.md)中的完整领域能力尚需真实 backend 与消费者。Registry 有定义不等于当前 Loop 可调用；当前审批合同只容 `run-command`，非 allow 分支走人工等待，auto-model/Reusable Scope 未闭合，见[审批决定路径](../../../internal/central/execution/multi_round_approval.go)与[审批合同](../../../internal/central/governance/approval/contract/types.go)。这是功能范围缺口，不是已证明的越权；不能用 M2 单操作审批代完整 D19。 | R3 |
| Runner 能力与平台支持（D15–D17） | 身份/WSS、Linux 文件与命令、Mount 配置及 M1/M2 的 read/write/run 实际调用可复用。 | **静态与未验证：** [当前 operation 选择](../../../internal/runner/operations/selection.go)仅覆盖有限文件/命令能力；[Runner 目标](../../architecture/runner/README.md)中的完整 macOS、数据通道、Desktop/Tunnel 与多 Runner 技能准备未形成完整旅程证据。Runner 本地具备的能力不能自动算模型已装配可用。 | R3/R5 |
| Knowledge、检索与 Memory（D12–D14） | 文档树、文本/Markdown 编辑、标题查找、有限 Parser 已有；固定语料和 native PostgreSQL FTS 候选已经真实测量，不能说所有候选均未运行。 | **静态与未验证：** [当前检索卡](d13-lexical-benchmark.md)保留剩余候选、固定 dense leg/RRF 及选型未完成；[Parser](../../../internal/central/retrieval/parser/types.go)不承担生产索引发布。生产 job/generation/hybrid query、版本来源引用与 Memory retain/recall/reflect 未形成完整实现/绑定证据，标题查找不代语义检索。 | R2 |
| Meeting（D24） | Summary 模型配置等前置存在。 | **静态未见完整实现：** [Meeting 契约](../../architecture/meeting/README.md)要求 Session/Turn/Contribution、编排、等待与 finalize；[当前项目路由](../../../web/src/router/index.ts)没有 Meeting 入口，本次装配审计也未见领域运行链。Summary 设置不等于会议功能。 | R3 |
| MCP（D20） | 详细领域与协议设计已在正式架构维护。 | **静态未见完整实现：** [MCP 契约](../../architecture/mcp-integration/README.md)要求 Connection、discovery、凭据、Tool/Resource 链；[当前设置菜单](../../../web/src/views/projects/ProjectSettingsView.vue)没有 MCP 入口，装配审计未见生产调用链。后续需核验固定 SDK/协议与授权接缝，不把配置形状当运行能力。 | R3 |
| Realtime 与 Human Inbox（D06/D25） | Outbox 机制、Task 执行历史/输出及局部 Approval 读取已有。 | **静态明确装配缺口：** [当前 App](../../../internal/central/app/account.go)的 Outbox Runtime 仅注册 Mail handler，尚未接完整浏览器 Realtime/Inbox/Session 撤销消费者；[Inbox 契约](../../architecture/platform-infrastructure/human-inbox.md)要求跨项目投影、来源路由与动作重验。Runner WSS 和 Task 局部审批不能替代 D25。 | R3 |
| 前端与完整用户旅程（D26/D27） | 正式 API 客户端、项目/设置/Task/Knowledge/Artifact 页面及共享样式已存在；未知结果、原意图查询和焦点恢复多处已有。 | **静态与限定浏览器观察：** [界面规范](../../frontend-design/interface-design.md)已有信息优先级、主动作与状态规则；[Task 评审详情](../../../web/src/components/work/TaskReviewDetail.vue)仍先呈决定动作、后置执行/成果入口。具体高影响偏离见下节；未观察的状态保持未验证。 | R1，随 R2/R3 扩展 |
| 完整生命周期（D05/D08 及各领域） | 项目创建、部分停止适配器、Object safe 与 M2 Execution 退出可复用。 | **静态明确缺口：** [生命周期装配](../../../internal/central/app/project_lifecycle_assembly.go)仍以 nil Registry 提供 authority，并说明破坏性命令等待完整注册；[Project 契约](../../architecture/project-work-management/README.md)要求归档/恢复/永久删除的全域停止与清理。责任 manifest 不是全部 participant 已绑定的证明。 | R4 |
| 生产发布、运行与最终实战（D02/D03/D28/E01） | 开发 Compose、构建、基础诊断/退出与若干真实进程测试存在；其限定接受保留。 | **静态与未验证：** [当前诊断](../../../internal/central/app/diagnostics.go)固定 ready=false，不能据静态 unbound 标签否定已接的局部能力；[运行契约](../../architecture/platform-infrastructure/deployment-runtime.md)的生产发布、升级、观测与恢复尚未全部闭合，SPA 原停止项仍在。数据备份恢复的完整责任与演练方案待明确；不新加 HA/PITR 或 RPO 承诺。E01 仍未开始。 | R5，汇合 R1–R4 |

## 首批前端改进：从任务到可审核成果

首批优先改进 **Task → Execution → Artifact → Human 审核/退回**，同时补齐 Agent/模型/Runner 配置的可理解性。这条路径有真实后端基座，适合先做完整可见结果；不等全域审计、全部设置页或 M3 完成。已有反馈应保留并重新组织，不能笼统说前端“没有反馈”。

| 问题与证据类型 | 对用户的影响 | 设计与实现出口 |
| --- | --- | --- |
| **Agent 配置层级偏离；静态+fixture 浏览器。** [Agent 详情](../../../web/src/views/projects/ProjectAgentsView.vue)把名称、标准化名、Agent/Project/模型 ID、version、指令及 Tool/Mount 引用同层展开；[项目设置布局](../../frontend-design/layouts/project-settings.md)已有内部分区要求。 | 用户需要解读技术字段才能判断 Agent 的用途、模型与能力，目录/配置/设置菜单占用多层空间。 | 按已有设置分区组织基础、模型/审批、环境、工具/Skills、Secrets；关键可用事实在前，身份/version 等进入明确诊断详情。保留原授权、恢复材料与必要复制入口。 |
| **Task 主次动作混排；静态+fixture 浏览器。** [TaskReviewDetail](../../../web/src/components/work/TaskReviewDetail.vue)把刷新、取消、指派/提交评审/接受等放同一动作区；[界面规范](../../frontend-design/interface-design.md)已要求主要动作与次要危险操作分离。 | 当前状态下该做什么不够清楚，窄屏属性占据大量首屏空间。 | 每个 Task 状态突出合法下一步；刷新、排序和取消使用次级入口，保留确认和真实不可用原因。属性按判断价值聚合，执行状态与任务状态分别呈现。 |
| **审阅材料与决定顺序断开；静态+fixture 浏览器。** [评审详情](../../../web/src/components/work/TaskReviewDetail.vue)先放接受/退回，正文、计划与执行入口在后；[TaskExecutions](../../../web/src/components/work/TaskExecutions.vue)需继续展开、选择执行并读取输出/成果。 | 用户先看到决定按钮，随后才寻找本次执行及对应成果；390px 下路径更长。 | 同一连续空间内呈现本次执行摘要、对应成果/输出与必要计划，再给出明确审核决定；历史与诊断渐进展开。本次旅程复用 M2 已接受的 Human 决定，不得自动把执行成功转为 Task 完成；Reviewer/Human 权限继续沿[正式状态机](../../architecture/project-work-management/task-state-machine.md)。 |
| **执行反馈关联不足；静态观察，未运行真实执行。** [执行详情](../../../web/src/components/work/TaskExecutions.vue)依赖手动刷新，[审批组件](../../../web/src/components/work/TaskExecutionApproval.vue)决定成功后提示刷新执行。现有反馈并非缺失。 | 用户自行拼接运行中、等我处理、失败和已产生成果的状态，难以判断下一步。 | 提供当前已确认执行摘要、需人处理项与读取时间，深层记录按需展开；按现有后端读取能力实现，D25 未完成时不承诺实时推送。 |
| **Explore 目标尚未落地；静态+fixture 入口观察。** [任务布局](../../frontend-design/layouts/tasks.md)规定 Kanban/Explore，[当前任务页](../../../web/src/views/projects/ProjectTasksView.vue)未见 Explore 实现。七列看板滚动本身符合设计。 | 长文与多轮执行审核只能依赖抽屉，阅读和返回上下文受限。 | 按既定布局提供适合持续审阅的 Explore 主区，并保留快速查看抽屉；切换保持 Sprint/Task、返回位置与焦点。 |
| **首次准备的引导待设计；有意占位与未验路径。** [项目首页](../../../web/src/views/projects/ProjectHomeView.vue)只有基本信息与空 Dashboard，符合[项目工作台布局](../../frontend-design/layouts/project-workspace.md)仅留容器的约定。 | 现有项目入口没有说明从准备配置到可执行任务的完整路径；不能将其记作违反原合同的缺陷。 | 先在对应工作项明确如何利用真实模型/Agent/Runner 配置入口解释缺项与返回路径；新增引导需同步正式布局，不直接加入分析图表、统计或未经定义的业务。 |

开工先按[界面设计逻辑](../../frontend-design/interface-design.md)给出页面类型、用户主要任务、信息优先级、主次动作、载体及宽度/滚动，再实现正常、空、加载、失败、权限、等待与取消状态；按领域补未知、冲突、失效和恢复。无需另建一套设计系统或多层评审档案。检查须覆盖实际操作、适用主题/尺寸/键盘与焦点；截图漂亮、所有字段存在或 fixture 请求成功都不足以接受整条旅程。

### Linear 依据、设计技能与组件增强

设计以 **Linear 官方公开产品资料为主要风格参考**，落实值以仓库已确认界面/样式规范及现有 tokens 为准。本轮已安装项目适配的 [frontend-design](../../../.agents/skills/frontend-design/SKILL.md) 与 [web-design-guidelines](../../../.agents/skills/web-design-guidelines/SKILL.md)，分别保留固定的 Anthropic 设计参考与 Vercel 界面规则；许可、版本、校验和发现边界见[技能来源](../agent-team/skill-sources.md#前端设计与-ux-审查技能)。UX 审计已实际读取入口与固定参考，并复核原 9 张截图及组件建议；没有新增浏览器交互通过，原超时与键盘未验证事实保持。安装与静态目录发现不证明 CLI 自动加载。

官方 [2024-03-28 UI 改版文章](https://linear.app/now/how-we-redesigned-the-linear-ui)明确提出减少视觉噪声、保持对齐、增强导航层级与密度，并讨论筛选/视图表头、属性侧栏和文字/中性图标对比。本轮审计实际查看了 [Projects 官方图](https://webassets.linear.app/images/ornj730p/production/5622bb57d625b2dbbe27639fe6f685e1c43589bd-2105x1332.png)与 [Coding session 官方图](https://webassets.linear.app/images/ornj730p/production/583931386478f7632a8e9c9d841ed8a2cd760f41-2550x1904.png)，分别来自 [Projects](https://linear.app/docs/projects) 和 [Coding sessions](https://linear.app/docs/coding-sessions)。这些是公开产品组织与可见层级的事实；本项目的单一主动作、受控阅读宽度、配置分区与诊断渐进展开是结合现有契约的设计选择，不能全部署为 Linear 官方规则。

未取得 Linear 完整官方 design system、内部组件 API 或完整 token 表，也未登录实测；不从截图反推颜色/间距并替换全局 tokens。参考不改变本项目的 Task/Human/Owner 模型、既定 Dashboard 范围或直接引入其他组件库。现有 `UiButton` 已有 primary/default/ghost/danger 与等待/成功反馈，Tabs、Collapse、Menu、Dialog/Drawer 也有基础；页面主次混排应先正确组合现有能力。

| R1 中按真实页面需要增强的共享能力 | 有界建议与验收 |
| --- | --- |
| [UiMenu](../../../web/src/components/ui/UiMenu.vue) 当前是平面 actions | 增加兼容的分组/分隔和禁用原因呈现，让 Task 次要/危险操作有层级；验证方向键、原因可读、Escape 回焦与原危险确认。 |
| [UiState](../../../web/src/components/ui/UiState.vue) 当前只覆盖 loading/empty/error 的固定区域样式 | 按任务摘要需要补紧凑状态、说明/动作与语义层级；不把领域枚举塞进基础组件，不造百分比进度。验证等待、未知、失败与权限反馈就地可读且不重复播报。 |
| [UiDialog](../../../web/src/components/ui/UiDialog.vue) 的对象上下文与长详情承载 | 按审核需要补受约束的头部摘要/动作或详情布局，保持 title/ARIA、关闭门禁、滚动与回焦；持续审阅优先 Explore 主区，不能仅把所有弹窗放大。 |

Tabs/Collapse 的状态摘要、实体行的名称/描述/状态与链接语义可随 Agent/成果页按需补充。每项组件增强须对应一个实际页面和可见问题；首批不等待通用组件体系先全部建成。以上是后续实现建议，本轮没有修改组件或发布新组件 API。

## 优先用户旅程与可验收出口

下面五条用于排首批完整结果，工作包职责仍只在开发计划维护；既有 M1/M2 未变证据可复用，只补新实现与失败影响范围。它们**不替代 D28 全部组合旅程**：Meeting 编排/Decision/summary、MCP 发现与凭据轮换/资源、Skills 多 Runner 准备、Tunnel 与完整领域删除仍须按原目录验收。

| 顺序与用户旅程 | 必须可见的结果与失败恢复 | 依赖及边界 |
| --- | --- | --- |
| 1. 打开待评审 Task → 读本次 Execution 与 Artifact → 退回 → 修订后再读接受 | 一次连贯审阅能找到对应成果、填写理由并作决定；同 Task 新 E 与旧成果/历史可追溯，返回后状态正确。失败保留输入，键盘/窄屏能完成。 | R1 优先，复用 M2 真实成果/审核基座；不得把执行 succeeded 显示成 Task done。 |
| 2. 项目准备 → 配置模型/Agent/Runner → 指派 Task → 首个成果 | 用户知道缺哪项、可从何处修正并回到原上下文；正常配置后真实工具工作产生成果。空态、无能力和加载失败有合法下一步。 | R1 与真实装配配合；先明确一个已支持任务组合，不等全部设置页。开发环境旅程不代 D28 首次部署，正式 UI 路径不能由直接 SQL 准备业务事实替代。 |
| 3. 执行等待审批 → 离开/刷新或受控重启 → 处理/取消 → 继续或终止 | 同一 E/Operation 批准后一次副作用、拒绝零副作用；用户能理解等待原因与后果。Task 与 Execution 取消区分，原请求可查、取消不复活。 | R1 先改善已有入口，复用 M2 限定证据；R3 的 Inbox/Meeting/Realtime 完成后补跨入口同一事实，不提前宣称全恢复或跨入口通过。 |
| 4. 更新知识 → Agent 检索引用 → Agent 显式 retain → 后续召回 | 索引与引用来源可理解，正文旧版本正确失效；Agent 在当前 Execution 绑定 namespace 显式 retain（可由用户任务要求触发），同 Agent 跨 Execution recall/只读 reflect、越权拒绝与删除失效通过。profile-only 重建按原契约保留旧 generation serving，失败可定位和恢复。 | R2 先知识后 Memory，复用检索；无自动 retain、无额外 OCR。Meeting/MCP 子链可按稳定契约并行。 |
| 5. 正式部署/升级与故障 → 恢复工作 → 归档/恢复/永久删除 | 已确认事实与成果保持，恢复不重复不安全副作用；完整 Registry 停止/清理、引用/名称与资源结果可解释；运维能判断阻塞和下一步。 | R4/R5，依原运行合同和先明确的数据恢复方案演练；当前尚未具备，不由 M2 退出或开发 Compose 代证。 |

## 本轮完成范围与接续

- 本轮已完成证据分级、能力差距与用户旅程整理，开发计划已把 M4 拆成明确出口，团队流程把信息层级与反馈恢复纳入功能交付，并补充两项可随仓库携带的设计技能；产品实现保持未启动。
- 后续首先派发 R1 的 Task/Execution/Artifact 审阅路径设计与实现；Agent/模型/Runner 配置可理解性随首条准备旅程补齐。R2 检索、R4 生命周期与 R5 运行规格按就绪依赖并行准备，不将全域设计设为每次改动的前置。
- Object、OpenAI tools 独立验收、SPA、Jina/Image 的原 STOP 与历史 FAIL 沿[停止项](../agent-team/tasks.md#停止项与真实阻塞)保留；文档复核不自动解除，也不使无关链全局停工。
- 本轮宏观矩阵采用三路只读审计，浏览器仅上述合成 fixture 观察。可再生的脚本、截图和原始笔记位于本机忽略目录 `output/ai/product-readiness-20261014/`，不作为跨设备已保存的验收原件；关键结论及正式源码/契约引用已归位本卡。
- 文档交付检查仅覆盖授权 Markdown 的链接/fragment、结构、格式与状态一致性；没有为本次规划运行无关产品测试。M1/M2 历史记录与本机原证据位置继续保留在[当前恢复索引](../../../.agent-state/current.md)。
