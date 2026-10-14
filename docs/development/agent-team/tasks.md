# 团队任务台账

## 当前目标与工作包状态

2026-10-14 用户最新决定：**先继续原定 M3；M4 暂时废弃为当前里程碑，M3 完成后重新评估缺口并继续完善，不自动进入交付阶段。** 当前五条功能线与依赖只在[开发计划](../development-plan.md#当前五个工作包)维护。全面既有页面改造不再优先于 M3；已安装设计技能、信息层级和反馈要求随本次新增/改动功能适用。此前[完整性复核](../work-items/product-readiness-review.md)的缺口与证据保留，本次路线调整不新增产品通过结论。

M1 与 M2 仍按原范围正式接受并已交付 main。M2 正常 Scheduler 下 worker 发布 Artifact、reviewer 读取原成果、Human 退回同 Task 新 E 修订后再读接受，以及限定审批、持久取消、必要恢复、原投影/历史与退出门均保持；不外推自主审核决定、完整恢复或全系统 ready。实际合同与证据见 [M1 集成卡](../work-items/m1-app-integration.md#当前真实模型验收)、[M2 工作卡](../work-items/m2-task-collaboration.md)及[独立审查](../work-items/m2-independent-review.md)。M2 六个工作树、分支及本机跟踪引用已收尾；原证据和资源信息继续见[当前恢复索引中的 M2 记录](../../../.agent-state/current.md#上一任务m2-已正式交付任务收尾完成)。

| 工作包 | 当前状态与负责人 | 下一就绪结果 |
| --- | --- | --- |
| M3 集成与整体契约 | `m3_integration` 负责共同接缝与唯一资源调度；root 负责 Git；1–133 正式迁移与 Model 独立停止解析门已过 | 继续 134/135 完整 caller 组合、真实用户链与有限独审；当前仍为完整原 M3。 |
| M3 知识检索与引用 | `m3_knowledge`：自动索引、四格式 HTTP/PDFv2/Agent 引用及退出已过；浏览器后继 harness 修复待整链 | 完成四格式浏览器；用固定 pg_search 镜像、真实同库 bootstrap/readiness 验证原 hybrid，复用已过 SQL 组件证据。 |
| M3 Memory | `m3_memory_tools`：跨 E 工具、Owner 浏览器与两个未退休/未 Begin 的真实停止恢复窗口通过 | 保原 Unknown 的窄修复已独审；继续剩余原业务恢复与同库 hybrid，不重跑正常窗口。 |
| M3 领域工具与相关 Runner 扩展 | `m3_memory_tools`：self-assignment 整链过，catalog 首链末 harness SQL 失败；`m3_model` 在独立 Runner 树实施 prepare | 复验 catalog/目标权限、原 Install Unknown；Runner 本地安全下载/发布/脚本已过，正式 Transfer/session/mount/App 待接。 |
| M3 MCP | `m3_mcp`：控制面/正文 Tool/Resource/Owner Web 已过；OAuth 核心由 integration 负责 | OAuth HTTP 正常链已过；继续媒体/source、OAuth 浏览器/刷新、startup/stranded 恢复与受影响 modes 门。 |
| M3 Meeting | `m3_meeting`：Decision/Approval/单贡献取消真实链已过；Summary pin 第一 generation 过、整 top 待 fixture 修后复验 | 接 typed refs/参与者、retry/regenerate/queue/parallel/finalize 与领域 Tool/proposal/真实 Inbox 必要入口。 |
| 已知后续完善事项 | 未排期；原 R1–R5/M4 不再作为当前派工路线 | M3 完成后重新评估全面旧页体验、完整 Lifecycle/架构覆盖及生产发布运行等缺口。 |

已完成的完整性复核、设计技能准备与 M1/M2 工作不重新开工；来源见[复核卡](../work-items/product-readiness-review.md)和[技能来源](skill-sources.md#前端设计与-ux-审查技能)。历史 W1–W5/R1–R5 沿原卡及 Git 历史查询，不作为当前活动派工。默认 Execution Runtime 仍需显式配置开启；Object/tools/SPA/Jina/Image 原 STOP 保留其适用范围，不因 M3 授权自动解除，不以里程碑数量推算平台百分比。

当前源码与较新正式结果已覆盖部分旧表中的 unbound：Project 创建已接真实 Skills/Object 初始化，Agent 配置/Task 指派与真实 Model/ToolLoop/Scheduler 已形成 M1/M2 组合，变量/Secret、Mount 与初始 Skills 已被执行装配消费。D13 固定候选与 dense/RRF 比较已有记录，生产接线选择 pg_search 0.26.1；准确安装、两域过滤/索引/readiness 组件门通过，真实 App hybrid 与部署镜像仍待，见[当前检索卡](../work-items/d13-lexical-benchmark.md)。下方历史行保持原输入的事实，不能据其旧描述重建已存在功能，也不能据新链扩大完整模块接受。

## 当前验收与推进边界

本节及下方各子项保留各自历史输入的接受/失败边界，不构成当前 HEAD 的完整快照；其中旧 unbound/未实现描述须结合较新的工作卡和[当前摘要](#当前目标与工作包状态)判断，不据旧描述重建已存在功能，也不扩大旧 PASS。

- 正式 main 已按各自固定输入接受以下子能力：后端有限组合及连续 00028→00031、Secret Owner HTTP/既有 Project 默认根接入、Knowledge 有限只读 UI、D16 本地读取库、D18 名称投影库、D13 有界纯文本 Parser、Variables 本实例停止提供方、单轮 Stop phase 引擎、有界恢复 Batch/真实 ProcessGuard 链及有限文本 Model Runtime/必要恢复入口已交付。阶段成果不代表整个 D04/D05/D08/D09/D10/D12/D16/D18 完成。
- [默认 App 项目创建与原意图恢复](../work-items/d08-project-creation-app-ui.md)已交付 main：项目列表可真实创建、完成 Skills/Object 初始化并进入项目。创建与 Archive/Delete 分能力开放；后两者仍须完整 LifecycleRegistry、停止及清理闭环。旧根执行 PASS、规范撤回和 test-only 结果各保留原边界，不据新建成功宣称完整 participant、foreign join 或全平台 ready。
- Secret HTTP 的作者 PG/native/root、独立当前 Session 与安全错误风险补集、完整 app ordinary/race 已按限定输入接受。Knowledge 最终组合 read04 已 wholePASS，原全部 Wait、七资源及 private/runtime/desc/TCP 双尾、输入一致性齐；独立三项 UI 风险验证使用真实 Session/Workspace 和受控网络，不冒真实 Owner 转让或 Logout PG。
- D13 纯文本 Parser 的 pure/race/vet、非作者源码审查和首条真实 D12→Parser 1 top/3 sub 已通过、原资源全尾齐，正式13路径已交付；索引/检索/发布、Markdown/PDF 与完整 D13 未完成。
- 文本 Runtime 首条显式 consumer 契约 fixture 的 JSON 成功/策略拒绝链已 wholePASS，原 Wait、七资源和全部退出尾闭合；00031 与恢复入口已正式交付。生产 consumer/defaultroot仍unbound，SSE、Unknown、held I/O取消和重启接管不由该首链代证。
- Variables 本实例 ordinary/Secret 停止提供方、真实单轮 accepted→stopping/claim driver 及有界恢复 Batch 已有限正式接受。Batch 的 7 top/12 sub race 与 vet 通过，Guard01 真实 SQL 扫描/活进程拒接管/后项可访/原 child SIGKILL 后 Wait 和 stdout join/原 guard 死亡证明及 fencing 链 wholePASS，七资源和全部原退出尾齐。该 test-only 链不证明 foreign 业务调用已 join，LocalJoined 或 claim terminal 均不表示整个 participant 完成。
- [Agent metadata 前置](../work-items/d10-agent-configuration.md#23-已接受的-metadata-前置)本次有限交付：Model Selection 与 Secret Directory 的作者 race/vet、非作者源码审及真实联合 PG metadata04 的 1 top/3 sub wholePASS，全部原 Wait、七资源及输入/退出尾齐。只接受当前 Owner 的配置 metadata、同 caller Tx 重验、失权/陈旧映射及回滚链；原失败保留，Agent/References、默认初始化和完整 F1 未由此闭合。
- Skills03 原 wholeFAIL 且完整尾已释放：四个预期请求已出现，但 Playwright 首次 detail 请求 failed，不能升级为读取链通过。Rename原current-read失败与旧DIST03未复验保留；当前已有文档标题改名正常链native04为13.34s wholePASS，基础控、业务/资源及两图有限独审均接受，随本次提交交付main。原本轮01/03 FAIL、02独立业务PASS与过渡截图保持，不外推真实Unknown矩阵、完整Knowledge编辑或生产initializer；该历史失败不回填；后继 Skills 目录/详情 UI 已由本次恢复正常链、独立业务/资源及新浅深两图有限接受，详见下方目标行。
- Work 作者 Recovery R13 仅该门 wholePASS。Planning06 原 wholeFAIL/完整尾已释放：Doc1 第二次 Milestone GET 的原 finished 等至45秒，首 list 安装门仅有限通过；Planning04/05及独立 Recovery03/04、Authority01 原失败保持。九类规划写、read/identity、blockers/layouts 和独立整体门未闭。
- 历史平台比例粗估不作为当前进度结论；使用本轮能力矩阵、真实用户旅程与原完成门槛判断剩余工作。完整执行、调度/团队协作和 E01 仍有未完成范围；E01 尚未开始，其至少50%游戏内容目标另行冻结与验收。

原普通包、两 vet/全 race/两命令构建及 Audit、默认根整改后的定向复验按相应输入组合接受，不称当前 HEAD 一次完整脚本全绿。原 check01/02、Audit deadline、wrapper 元数据失败、Knowledge read01/read02 和各领域历史 FAIL 均保留；后验清理不补写原终态。部署独立必填 `AGENTEAM_CENTRAL_KNOWLEDGE_CONFIRMATION_KEYRING` 继续有效，见[后端说明](../backend/README.md)。

[Agent 显示设置](../work-items/d10-agent-display-settings-ui.md)的名称/颜色编辑、显式清空、真实正常链及原旧字段丢回执链已有限独立接受，随本次变更交付。[Task 历史筛选与顺序切换](../work-items/d11-task-history-filters-ui.md)的限定只读链也已独立接受，随本次变更交付。[Skill 固定修订包下载](../work-items/d10-skill-package-ui.md)的当前 Owner HTTP 与详情下载正常链已有限独立接受，随本次变更交付。[Milestone 标题/描述编辑](../work-items/d11-milestone-edit-ui.md)的正常保存、no-op、清空与原命令丢回执恢复也已有限独立接受；所有既有 STOP 与生产未绑定范围保持。

## 历史并行所有权

以下是旧 metadata/前置组合的所有权记录，仅用于解释历史结果与失败，不是当前派工或活跃会话。现工作包见[当前状态](#当前目标与工作包状态)，具体进程/分支恢复读 [current](../../../.agent-state/current.md)，旧实例和窗口授权不继承。

- **secret：Agent Secret 前置。** Secret Directory 与联合 metadata 链已按上述有限范围接受；References 后继实施仍须连接真实 Agent canonical authority。材料不进入目录，缺 owner 明确拒绝，不新增生产 consumer。
- **content：Agent canonical 核心。** 在独立树推进唯一配置 writer、同 Tx 私有 create/pre/post witness 与跨域回调；这些后继源码和迁移不纳入本次 metadata 交付。Rename 原 UI FAIL 保留。
- **cleanup：D18 Registry 前置及非作者复核。** 实施范围沿独立任务核真实注册定义、Backend binding、稳定身份和同 Tx 引用保护；NameTable 或纯检查不等于生产 install-skill 后端已完成。既有共享入口写入按 root 单独分配。
- **work_ui：Agent Model 前置。** Selection 与 Secret Directory 联合 metadata 已有限接受；双角色引用的后继源码/纯验证不冒真实 Agent 创建或引用写入 PG。Skills03 与旧 Work Planning06 整体失败仍冻结，Recovery R13 只闭自身有限门。
- **skills_http / coordination：** 前者负责未参与实现者的风险/方法审；后者负责跨域接口、集成和台账。Agent、Registry、Skills 和各域 References 的后继写域/迁移由 root 独立分配，不并入本次无迁移的 metadata 结果；不能用空 catalog、伪造创建 witness 或默认 false 缩减 F1。关系与边界见 [Agent 配置卡](../work-items/d10-agent-configuration.md)。

旧恢复、装配与逐轮资源记录保存在 Git 文件历史及各正式卡/任务 topic，不再在当前摘要复制过时“最新 main/未交付”流水。未验 Model UI/Runtime后继材料与五个旧库树的必要历史原件继续保留；完成子集不等于整个分支可删，清理由 root 逐对象核实。

| 工作项 | 当前可证状态 | 接手时的下一步与完成边界 |
| --- | --- | --- |
| [D08 Project Scheduler 配置 HTTP/UI](../work-items/d08-project-scheduler-ui.md) | **当前 Owner 调度配置已有限独立接受**。正式 GET/HEAD/PATCH、完整 int64/null、历史 ack 与当前 GET 分离；真实默认 App/browser 正常两次写入及丢响应后原 key Lookup/独立 GET、三图与完整资源尾接受。 | 原 browser01 导航 fixture wholeFAIL 保留；02 normal 9 请求8 finished（原 GET aborted 联证），lost 8 请求7 finished（原 PATCH 物理截断），无重发 PATCH。Lookup 不证明 Scheduler body；运行中 Execution、Scheduler Runtime、生产 initializer、ready 与既有 STOP 未由本片完成。 |
| [D01 低层资源身份](../work-items/d01-resource-identities.md) | **R1纯契约已实现并独立验收**。Tool/Mount/ProjectVariable三个canonical marker与Foundation ID alias，四依赖race/vet及独立3顶层6子例通过。 | 仅标量声明与类型消费；Tool/Mount/Variables真实目录、引用保护和Agent F1仍未闭合，普通与Secret共用变量ID且不同CredentialID。 |
| [D10 Agent 核心与引用](../work-items/d10-agent-configuration.md) | **C1纯契约已交付，Model Selection＋Secret Directory metadata 前置本次有限交付**。两域作者 race/vet、非作者源码/方法审与真实联合 1 top/3 sub wholePASS；当前 Owner、同 Store caller Tx、目录分类、撤权/陈旧映射/回滚与原资源尾闭合。 | Agent canonical 创建/当前事实、完整双角色/Secret 引用、默认 Skills/Registry 及 Task 指派绑定仍须后继真实组合；本次无 Agent 行、引用写入或新迁移，生产 initializer 保持 unbound，完整 F1/D10 未完成。 |
| [D10 Agent 显示设置 UI](../work-items/d10-agent-display-settings-ui.md) | **显示名称/标签颜色编辑与明确清空已有限独立接受**。原字段省略与 null、版本/原意图恢复、冲突保输入保持；真实设置/no-op/清空后三次当前 Get/List、原旧字段 lost 链、资源全尾与六图可见范围接受。 | 新显示字段真实 Unknown/完整权限矩阵未由旧 lost 子例代证；请求存在已联证的 aborted/cut，非全 finished。完整二十字段编辑、F1/D10、生产 initializer 与原 STOP 保持。 |
| [D10 普通 Project Variables Owner 后端](https://github.com/LunaDeerTech/agenteam/blob/3cea6076bb01693ead2755826826d189626aa3aa/docs/development/work-items/d10-project-variables-owner-http.md) | **限定后端已实现、独立接受并正式交付main（3cea6076）**。已有初始化Project当前Human Owner可经默认Central HTTP创建、读取、分页、更新、删除普通变量，以原意图Lookup/同key显式重放恢复；迁移00024、原子Audit/Outbox及真实退出均闭合。 | 作者库/HTTP/native/默认根与独立三个风险补集按限定版本组合通过，实际Wait与资源尾齐，原FAIL及独占缺口保留。本协调树已合并该main，Git保存由root负责；不含Secret、Agent白名单/执行注入、Runner环境、UI、Project创建HTTP或完整D10，Object join等停止项不变。 |
| [D27 普通 Variables UI](../frontend/README.md#普通-project-variables) | **目录/详情、创建、编辑与明确删除确认已限定交付 main**。当前 Owner 使用原目标、版本和幂等键；Unknown 只查原意图，删除确认后清除旧详情并独立重读目录。后继真实 App/Chromium 删除、取消零写、原 Lookup、焦点和完整退出已接受，见[删除界面结果](../work-items/d10-project-variables-owner-http.md#后继普通变量删除界面2026-10-12)。 | 复用分段有效证据，保留原前端、controls 与取消回焦浏览器失败，不称一次全量通过。不含 Secret 删除、Agent/Runner 注入、真实权限/Unknown 全矩阵或完整 D27；SPA 发布及既有 STOP 保持。 |
| [D27 Project Secrets 安全目录 UI](../work-items/d10-project-secrets-read-ui.md) | **八字段安全目录与详情只读 UI 已有限独立接受**。默认 App 真实50+1分页、末项独立Get与显式刷新、Enter标题焦点/返回回焦、两图及原完整资源尾通过；材料始终留在Go。 | 7GET为6 finished+1原Session aborted，7联合证明；四Secrets读均finished、零浏览器写/Lookup。保原真实焦点与控制/build FAIL；不含创建改值删除、Agent授权/有效性投影、真实撤权全矩阵、完整D10/D27、生产initializer或既有STOP。 |
| [D09 Project 模型调用记录只读 UI](../work-items/d09-project-usage-read-ui.md) | **近30天调用明细、分页返回/刷新与本页详情已有限独立接受**。默认 App 正式51条准备与五次 List、null/0/大int64原字符串、真实键盘与浅深三图、账本不变及原完整资源尾通过。 | Invocation24字段，分页外壳另2；8GET为6 finished＋两次Usage GET aborted，8联合证明及真实CDP Host/实际detach齐。保留native01/02与retire反例FAIL；不含summary、估算/费用、Provider调用、生产Runtime producer或完整D09，既有STOP保持。 |
| [D10 Secret Variable Owner A 纯合同与 Schema](../work-items/d10-secret-variables-owner.md#11-a-纯合同作者结果) | **限定纯合同已实现并独立接受**。安全材料请求、metadata/receipt、独立命令与 identity-only Lookup、事件及 F1 目录/引用端口、严格独立 OpenAPI 已闭合；作者与独立 race、解码保密/绑定负例及 schema 检查通过，原两处独审失败和修复保留。 | A本身仅证明类型、codec、端口形状与schema；后续D04/Owner库及00029/30见下方独立行。后续 Secret HTTP/既有 Project 默认根已按独立范围交付，真实 F1 与完整 D10 仍未完成。 |
| [D10 Secret Variable Audit 合同与读兼容](../work-items/d10-secret-variable-audit-read.md) | **限定结果已实现并独立接受**。三action安全metadata、独立predicate/既有producer、Go row/HTTP投影、OpenAPI及真实TS严格decoder闭合；作者与独立race、Schema、跨action/重复JSON及实际取消尾控制通过。 | 本行只证明纯合同与既有读兼容；后续D04/Owner真实持久事实与权限由独立库矩阵另证，后续 Secret HTTP/root 已另项交付，不由本合同结果代证，完整 D10 未完成。 |
| [D11 Work Structure](../work-items/d11-work-structure.md) | **本卡已完成**。四 contract `c3b1ee72` 与实现 `a64fb5e7` 已提交；18 路径均存在，限定技术域至本次基线无差异。 | 可供 Task planning 消费。全18按明确版本组合接受；保留 Unknown01 原 FAIL、U1 修复与独立 B 工具终态缺口。不是当前 HEAD 单次全量测试或完整 D11。 |
| [D11 Task planning](../work-items/d11-task-planning.md) | **本卡规划库已实现、独立验收并正式交付main，完整D11未完成**。契约、三个Human规划命令、Lookup、当前Get/List、同Tx membership、TaskEvent/Outbox及00022已完成；原enclosing日志FAIL与真实投影边界保留。 | 七新PG顶层、四旧Project/Outbox、四旧Structure及独立A/B按限定版本组合已实际通过并完成资源终态；原观察器/非法Project输入/rank fixture/归档输入FAIL保留，测试修正的真实对照与完整top已通过。状态机、指派、执行、删除、生产root与完整D11均未完成。 |
| [D11 Task 流转](../work-items/d11-task-transitions.md) | **T0a与T0b纯契约已实现并独立验收**。状态/角色/Position及Transfer/Lookup/摘要、Human typed历史、严格封套与纯多事实工厂已完成；作者及独立pure/race/vet通过。 | 仅纯决策与类型，不提供授权或提交能力。T0b仅消费B0-C两类；独立公开API各6顶层34子测试实际通过。运行服务仍须真实Agent/Blocker/occupancy与执行事实；完整D11未完成。 |
| [D11 Blocker 纯契约](../work-items/d11-task-blocker-contracts.md) | **B0-C两类纯契约已实现并独立验收**。唯一BlockerID、rely_on/无引用waiting_for_human typed metadata、Create及小payload；strict codec/cap/Clone/log，作者及独立pure/race通过。 | T0b只可消费已接受两类；其余三类和外域引用保持未绑定。B0-P持久服务见下行，跨状态组合与完整流转仍未完成。 |
| [D11 backlog Blocker 持久服务](../work-items/d11-task-blocker-service.md) | **限定服务已实现、独立接受并正式交付main**。Human Owner对未指派backlog Task的两类Blocker可add/resolve/list/lookup；真实完整图、Task版本及同事务历史/Outbox/Activity/receipt与00023已完成。 | 作者pure/race/vet/build、公开契约与runtime独验、真实权限/并发/Unknown/互操作/SQL回滚及旧Planning/Structure回归通过并完成资源终态；独立装配审查已接受。首Persistence整轮FAIL保留，以未受影响子项与修复后的History定向回归组合接受。非backlog、Agent/Executor、HTTP、生产root及完整D11仍未绑定。 |
| [D11 Work Owner HTTP/root](../work-items/d11-work-owner-http.md) | **限定HTTP/root能力已独立接受并正式交付main（f1c94ee5）**。已有initialized Project的当前Human Owner可经正式HTTP规划Milestone/Sprint、未指派backlog Task及两类Blocker；默认Central根实际绑定三服务/三Reader。 | 作者pure/race/vet/schema、分页/权限/恢复/Unknown、native及真实根21能力/生命周期通过；未参与实现者本人分页、网络断连恢复/撤销竞争及真实提交确认退出均完整PASS，实际Wait与各自资源尾齐。首分页/TCP尾、Reader期限判据及独立确认泛SQL断言原FAIL保留，按限定修后组合接受。无新迁移，不包含Project创建、UI、Task状态流转或完整D11。 |
| [D11 Task 历史筛选 UI](../work-items/d11-task-history-filters-ui.md) | **事件单选筛选与双向顺序已有限独立接受**。默认新到旧、条件变化从第一页读取；真实最近/较早/刷新、评论单型/空态/清除及旧到新分页八次查询通过，两图与完整资源退出接受。 | 25 请求为 22 finished 与 3 个实际 GET aborted，10 份联合证明；零浏览器写入及 Lookup。七类型受控检查不等于七类型真实浏览器矩阵，不完成评论写入、完整 Task/D11/D27、生产 initializer 或既有 STOP。 |
| [D11 Milestone 编辑 UI](../work-items/d11-milestone-edit-ui.md) | **已有 Milestone 标题/描述编辑已有限独立接受**。字段省略/明确清空、冲突保草稿及显式采用版本、Unknown 原 Lookup 与真实 Get/List 分开；正常三次保存与 lost 恢复、五图和完整资源尾接受。 | 原 native01 观察方法 wholeFAIL 保留；02 normal 30 全 finished，lost 17 为 15 finished、声明 PATCH 截断及已联证 GET aborted。受控失权/恢复不代浏览器完整权限矩阵；Sprint 编辑由独立切片交付；排序/删除、完整 D11/D27 与生产 initializer 未完成。 |
| [D11 Sprint 编辑 UI](../work-items/d11-sprint-edit-ui.md) | **planned/current 标题/描述编辑已有限独立接受**。completed 连 no-op 禁止；字段省略/清空、原版本冲突采纳与 Unknown 原 Structure Lookup、当前 Get/List 分开。真实 planned 三写与 current 丢回执恢复、五图可见范围及完整资源尾接受。 | normal60为52 finished+8原abort，lost31为29 finished+声明cut+原GET abort，86份联合证明。窄清空图底部按钮未入图，DOM证据分列；编译记录器/launcher控原FAIL保留。Sprint完成/rollover、排序/删除、完整权限矩阵、D11/D27及生产initializer未完成。 |
| [D27 Work Owner规划UI](../work-items/d11-work-owner-planning-ui.md) | **首 Milestone 创建、已有 Milestone 下 planned Sprint 创建、显式启动 Sprint 与当前 Sprint 关键词搜索已限定交付 main**。创建和启动各需独立确认；七列关键词查询在服务端执行并绑定原分页条件，真实浏览器与原退出已接受。见正式卡 §15–18。 | 完整 Explore、其余排序/Blocker、Sprint 完成/rollover 与完整规划 UI 未由这些切片完成；priority/type 筛选仍在独立候选验收中。原 Recovery R13 仅自身有限门，Planning04/05/06、独立 Recovery/Authority 及后继各原 FAIL 保留。 |
| [D27 Project Model Settings UI](../work-items/d27-project-owner-model-settings-ui.md) | **原限定版本组合接受，新main两Audit完整通过，Model authority仍待**。新6/6、旧14/14、独立A/B完整通过；新树AuditAuthority/AuditNavigation均实际全尾齐，原FAIL与Wait/TCP/独占缺口保留；原8个IPC非法参数的独验FAIL及其值校验修复不回填。 | 新树`/workspace/agenteam-model-ui-delivery`基于main3cea；首authority整体FAIL，限定Resolve方法已离线独审、尚待真实验收。两Audit通过仅覆盖其固定输入，不机械重跑旧14，不冒生产SPA、完整D27或后继新main装配。 |
| [D08 Project 与 Owner](../work-items/d08-project-owner.md) | **部分子能力接受，模块未完成**。基础权限/生命周期事实、Owner 读写 HTTP、Audit 与初始化收敛已有交付；[初始化 Audit 授权库](../work-items/d08-project-initialization-audit.md)已正式交付main（ca9f2d5d）。[Skills CleanupPhase授权](https://github.com/LunaDeerTech/agenteam/blob/ce65714aac6eb4995a43fc427a2c77e6497470a7/docs/development/work-items/d08-project-skills-cleanup-authority.md)已独立接受并正式交付main；作者三个真实PG顶层与独立风险补集均完整通过，实际命令与资源尾齐。 | 新授权仅在同Store活Tx、Project锁下核当前Deleting/Cleaning、Owner/版本/manifest及停止/依赖完成事实；零迁移，不执行清理、不授其他participant/final权。真实Skills/D05最后同Tx与初始化Object witness已由后续有限库组合验证；完整registry、生产initializer/创建HTTP及全生命周期仍未绑定，Artifact/Object完整组合未闭合。Object join停止项保持，完整D08未完成。见[领域绑定卡](../work-items/recovery-project-domain-bindings.md)。 |
| [D08/D10局部停止与phase推进](../work-items/recovery-project-domain-bindings.md) | **Local Variables Stopper、单轮 Stop Driver 与有界恢复 Batch/真实 guard 链已有限正式交付**。同 Store 精确停止、提交后调用、原 call/checkpoint 退出、Busy 后项推进及实际进程死亡后的 claim/fence 重验通过；Batch pure/race/vet 和 Guard01 wholePASS 原尾齐。 | 仅显式 test-only 组合消费原 ProcessGuard，不恢复 Object Runtime join。完整 registry/phase loop、foreign 业务 join、cleanup、生产 initializer 与完整 participant 停止均未完成。 |
| [D08 Secret/Audit 停止屏障](../work-items/d08-secret-audit-stop-adapters.md) | **两个限定 adapter 已实现并独立接受**。同 Store 真实 Authority/Project SH、原接受与 phase driver、exact 停止报告及零 checkpoint 写；4 top / 9 sub race 和真实 PG 1 top / 2 sub wholePASS，原 Wait、七资源及全部尾齐。 | canonical 账户种子、SQL terminal 观察及 Committed 后包装 Unknown 边界见卡；生产 initializer/registry、Cleanup、foreign join、ready 与旧 Object STOP 均不改变，原整卡未恢复。 |
| [D08 Outbox 停止适配器](../work-items/d08-outbox-stop-adapter.md) | **限定 adapter 已实现并独立接受**。同 Store/Project SH 当前事实预检后调用真实 D06；六 top / 二十六 sub race 与真实非空队列 stop/Inspect 一 top / 两 sub wholePASS 按分段证据组合接受，原 Wait、七资源及全部退出尾齐。 | 后继同步 panic 窄修/正常链复用已独审，不把旧正常 PG 改称新输入整套运行。保原编译/四项 panic FAIL；受控 Unknown 与 SQL terminal 边界见卡，普通 delivery/callback、Artifact adapter、完整 registry、Cleanup、生产 initializer/ready 与原 STOP 保持。 |
| [D09 Model System](../work-items/d09-model-system-token-usage.md) | **配置/HTTP/Resolver等限定库及[文本Runtime核心](../work-items/d09-text-chat-runtime.md)、00031、必要恢复入口已正式交付，模块未完成**。显式consumer契约fixture→同Store Secret/Usage→原text adapter的一次JSON成功与策略拒绝真实链wholePASS，原Wait和七资源全部尾齐。 | 生产Resolution/consumer/defaultroot仍unbound；SSE、提交Unknown、held I/O取消与恢复接管待后继验证。该fixture不证明Agent/F1或生产Provider调用；OpenAI tools独立STOP与Jina/Image来源缺口保持。 |
| [D10 Skills 初始化](../work-items/d10-skills-initialization.md) | **P1/P2及精确Cleanup限定库已交付，模块未完成**。真实初始化／不可变包、当前Owner、原命令恢复与精确Stop有作者固定版本组合和未参与者风险补集；P1 builtin结果继续复用。 | 后续Cleanup32条历史批次、两域最后anchors同Tx与真实历史消费者已有限接受；完整多域participant、生产initializer/创建HTTP与Agent/Tool/Runner消费未完成。库前置fixture不冒生产创建。Object Runtime join停止项保持，不因局部库交付改变全局ready。 |
| [D12 Knowledge B02](https://github.com/LunaDeerTech/agenteam/blob/29dd429881783595e5a1304b86af5bfb56ec80b2/docs/development/work-items/d12-b02-knowledge-service.md) | **有限Human Service及主线共享整合已独立接受并正式交付main（29dd4298），完整D12未完成**。canonical内容/文档树、原子变更、来源复制、本域恢复、Object/Audit/Outbox适配器及00025闭合；作者固定矩阵、独立六组与共享路由回归按限定组合接受。 | 原整体FAIL及终态缺口保留；HTTP/UI、下载URL、D13、B03全Project生命周期、生产root与Object Runtime join均不在本结果，停止项不恢复。 |
| [D12 Knowledge只读HTTP](https://github.com/LunaDeerTech/agenteam/blob/4c1db71cf0f86cb6d6330167577944b0466c8664/docs/development/work-items/d12-knowledge-owner-read-http.md) | **五查询metadata/tree Human Owner GET/HEAD及安全DTO/OpenAPI已独立接受并正式交付main（4c1db71c）**。作者真实PG/native与独立风险补集闭合，原命令与资源尾齐。 | 独立adapter消费正式B02；不含默认root、正文/下载、树命令、UI或完整D12。树命令另项已正式交付，见下行；旧FAIL保留。 |
| [D12 Knowledge](../work-items/d12-knowledge-documents.md) | **B02、Owner metadata/树命令/有界正文HTTP、默认根既有数据读取和限定文档读取/改名及文本创建/正文编辑UI已交付；完整D12未完成**。已有文档Rename及后继文本创建/正文替换正常UI链已有限验收；复用原命令并真实重读metadata/ancestors/body/tree。新增[文本创建/正文替换与原意图Lookup HTTP](../work-items/d12-knowledge-owner-text-http.md)及默认App接入已有限接受：plain/Markdown真实Account/PG/MinIO与App两条精确链wholePASS，原Wait、七资源和全部尾门齐，非作者已核源码/原件；写入与原Lookup的可恢复字节上限已修复并独审。 | 原read01/read02、旧Rename03及本轮01/03 FAIL保持，旧DIST03未验不升级；本轮02业务PASS独立保留，04的13.34s业务/资源与两图有限接受。文本App证据为测试进程内默认run/listener与signal channel SIGTERM；不外推外部OS信号、真实Unknown丢响应/撤权竞争、replay全矩阵、上传/移动/删除UI、下载URL、B03全Project生命周期、production initializer或Object Runtime join。文本UI正常链已后继有限接受，见下一行；Parser不冒索引或完整检索接入。 |
| [D12 Knowledge文本UI](../work-items/d12-knowledge-text-ui.md) | **根层/子文档纯文本与Markdown创建、完整正文编辑及当前回读已限定接受，随本次提交交付main**。App草稿与唯一Session原命令分域；冲突保输入、同Session恢复、真实失权清理有受控证据。native03实际4写/两文档v2、键盘/Drawer/两图与七资源全尾独验接受。 | 28请求为23finished+5原GETaborted，联合消费闭合；四POST全200/finished、browserLookup0，不冒真实Unknown/replay或撤权全矩阵。原native01双bundle、native02窄屏fixture及前端原FAIL保留；完整D12、上传/移动/删除UI、生产initializer与Object Runtime join未完成。 |
| [D12 Knowledge 标题查找 UI](../work-items/d12-knowledge-title-search-ui.md) | **全项目字面标题查找与当前文档读取已有限独立接受**。完整命中及祖先路径、键盘选择、既有未保存确认与唯一Session读取接通；真实默认App中未加载子文档命中、当前正文和空结果、两图及完整资源尾接受。 | native01为9GET全200/finished、2次标题查询、0写入/Lookup；不称failed补证分支、真实分页/撤权/Unknown矩阵已验。原接线fixture和路径预检FAIL保留；正文语义检索、Jina/Image、移动/上传/删除、完整D12与生产initializer/Object Runtime join保持未交。 |
| [D12 Knowledge 树命令 HTTP](../work-items/d12-knowledge-owner-tree-http.md) | **五个 POST adapter 已实现并完成限定独立验收**。当前 Human Owner 的改名、移动、删除确认、删除和原意图 Lookup 使用独立安全 DTO；作者 PG 固定组合 4 top/13 sub、native 3 top/6 sub 与新主线独立新 Owner/旧 receipt 补集均完整通过。 | 同包 integration 编译与独立产品/方法审按固定输入接受，装配保留原主线源；保留原 PG01 和独立首轮测试 helper FAIL，不称当前 HEAD 单次全量。本adapter证据不含创建、正文或UI；后续正文与默认根读取装配见下行，不代表完整D12，原停止项保持。 |
| [D05 有界 metadata 清理](../work-items/d05-bounded-metadata-cleanup.md) | **限定库、00028与真实Skills消费者已交付**。32条总额、原最终事务/Unknown、历史与成本矩阵及消费者完整尾均按固定版本闭合，成本保持64/512/2s。 | 原成本/退出FAIL保留；不代表Object Runtime全局join或完整生产participant。 |
| [D04 Secret Variable producer](../work-items/d04-secret-variable-storage.md) | **typed专用材料/receipt库与00029已交付**。core、rotation/cleanup、固定Unknown/并发矩阵有限接受，旧purpose闭集保持。 | 不公开value读取，不以受控authority替代真实Owner；后续Owner权限矩阵见下行，HTTP/F1不在本库。 |
| [D10 Secret Owner服务](../work-items/d10-secret-variable-owner-service.md) | **typed Owner库与00030已交付**。真实Account/Project当前授权、单final Tx、metadata/receipt、类型/命令/cursor隔离与作者30节点、独立两风险通过。 | 与普通变量共享业务ID/活名称唯一性，响应无value/密文/摘要；Secret HTTP/既有 Project 默认根见后继正式卡；F1 未交付，原 read01FAIL 保留。 |
| [D10 Secret Owner HTTP](../work-items/d10-secret-variables-owner-http.md) | **三路由HTTP与既有 initialized Project 的默认根接入已正式交付**。同Store当前Owner/Session、唯一facts producer、Catalog事件及Account前原Stop/Drain；作者PG/native/root、独立撤Session读与安全错误补集、完整app ordinary/race接受。 | 普通变量路径与命名空间保持；默认Project initializer仍unbound。原错误/恢复材料保留，F1、完整生命周期、Agent注入与UI不在此范围。 |
| [D10 Skills目录HTTP与只读UI](../work-items/d10-skills-owner-read-http.md) | **adapter、默认根既有数据读取与当前 Owner 目录/详情 UI 已限定交付**。UI 正常链两次 catalog/两次详情、当前上下文与完整消费、真实无新增业务事实和原退出尾闭合；独立方法/业务/资源及新浅深两图接受。 | 生产 initializer 保持 unbound；完整 participant、安装 UI、Agent/Tool/Runner 与完整 D10 未完成。本轮真实浏览器不代完整撤权、多页或生命周期矩阵；历史 Skills03、恢复01 Host FAIL 与恢复02品牌破图保留。 |
| [D10 Skill 固定修订包 HTTP/UI](../work-items/d10-skill-package-ui.md) | **当前 Owner 固定 revision GET/HEAD 与详情下载正常链已有限独立接受**。后端 canonical 包/当前授权、原 reader 退出与默认 App 接线分版本验证；浏览器实际点击、完整响应和实际保存文件 SHA/长度一致，7 GET 全 finished、浅深两图及全部原资源尾接受。 | 原 PG01 和 browser01 wholeFAIL 保留；浏览器02未触发 metadata aborted 补证分支，不代真实下载取消、撤权竞争或持久释放 Unknown。生产 initializer、安装 UI、Agent/Tool/Runner 和 Object Runtime join 等原边界保持。 |
| [D12 Knowledge正文HTTP](../work-items/d12-knowledge-owner-content-http.md) | **有界正文adapter与默认根读取接入已交付**。当前Owner、GET/HEAD、真实reader/事务退出、native与PG有限矩阵闭合；删除正文410保持权限前置。 | 原短正文fixture PG01FAIL保留；只读UI已后继交付；PDF/DOCX parser、下载URL、生产Project创建与全局Object Runtime join未交付。 |
| [D13固定语料＋离线评分工具](https://github.com/LunaDeerTech/agenteam/blob/b2a7d0abef0b7ca0623c8e2a7c020658948848da/docs/development/work-items/d13-lexical-benchmark.md) | **独立子结果已接受并正式交付main（b2a7d0ab）**。lexical-v2语料语义返修获独立接受，评分工具独立128控通过；原语义/夹具FAIL保留，任务分支关闭。 | 只交付固定语料和可重复离线评分工具；lexical-v2含32篇原创文档/64section、64有答案+8无答案query及4608显式相关性判断。三候选backend真实成绩、索引/查询性能、扩展/字典版本与许可、固定dense leg的hybrid RRF及最终选型仍未验证，不完成D13。 |
| [D13有界纯文本Parser](../work-items/d13-lexical-benchmark.md) | **限定库与首条真实调用已接受并正式交付**。plain_text:v1保原UTF-8字节/版本来源、有界段句与取消零结果；D12完整DTO适配及真实Close后解析、partial/非plain拒绝、外Owner无输入三子联调wholePASS。 | 纯函数/值适配的非作者审查与作者真实链分列；不冒独立动态验收、自动current发布、索引、chunker/ContextProvider、embedding、Markdown/PDF或完整D13。 |
| [D16 Linux Workspace读取库](../work-items/d16-runner-filesystem-read.md) | **限定本地read/list与Root生命周期库已正式交付**。Linux原目录fd边界、有界读取/列举、原调用与Close后Stop/Drain；本包pure/race/vet及非作者源码审接受。 | 可信根由调用方提供；不代表OS sandbox、current Mount授权、生产Runner operation/Dispatch、写操作、macOS或完整D16。 |
| [D16 Workspace manager](../work-items/d16-runner-workspace.md) | **限定本地库已独立接受**。可信 root fd→canonical Project/Agent/name 三层安全 Ensure→受管理 Handle 的 Read/List/Find；实际取消/I/O/close 后 Stop/Drain，11 top/11 sub race 与 vet 通过，关闭失败返修已独核。 | 不解析或授权当前 Mount，不绑定 Control/executor/生产 initializer；不自动删目录、不是 OS sandbox，非 Linux 未动态验证。原 FAIL 与既有 STOP 保留。 |
| [D18 Tool名称投影](../work-items/d18-tool-name-projection.md) | **SpecRef及不可变NameTable纯库已正式交付**。唯一ToolID、固定模型名称、同ID跨revision稳定名称、原表准确反解；unit/race/vet与非作者范围审接受。 | 调用必须使用原Snapshot的表；真实Registry/版本目录、Schema/授权、Tool Runtime、Agent/F1和Executor接线未完成，不宣完整D18。 |
| [D15 Runner 身份与控制通道](https://github.com/LunaDeerTech/agenteam/blob/fb6ab7f492850bf1d3c025a59acbff381312a989/docs/development/work-items/d15-runner-control.md) | **迁移00026对应Linux/amd64限定能力已独立接受并正式交付main（fb6ab7f4）**。设备管理/登记、私有身份、出站WSS代际与有界在线视图、空operation registry和默认双cmd已闭合；作者业务、独立七子、正式OS三格及new-main Default/CLI完整尾分别通过。 | 固定版本组合接受，原Device/Protocol/B/OS/Default等FAIL保留；不代表当前HEAD单次全量或完整D15。macOS/其他架构、跨UID及实际Dispatch/Mount/D16–18绑定未完成，ready503与Object join等原停止项不变。 |

D08–D28 各模块仍有未完成范围，E01 未开始；首个未完成模块编号为 D08，已接受子能力可继续组合。默认 Project 创建与真实 Skills/Object 初始化、Human Owner 规划 HTTP/UI 及[可选 DirectText App 执行接线](../../../internal/central/app/execution_runtime.go)已有主线实现；完整 Task/工具执行、Model 工具调用、D24 与 Project Archive/Delete 的完整 LifecycleRegistry/引用退休仍未闭合。既有 `ready=false` / `/readyz` 503 边界不因这些子能力改变，真实局部调用不代证生产部署、全平台 ready 或 E01。

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

`modelsrecover03`、author v06/private v04的历史准备描述不构成动态通过。当前独立A第二轮及B第六轮均已按限定完整结果接受，B1–5原FAIL与首轮Wait缺口保留；不能把历史二进制或前端unit代作当前真实浏览器结论。范围以[当前卡](../work-items/d27-project-owner-model-settings-ui.md)为准。

**Work Structure：** 六新 PG 按版本组合通过，Unknown01 原 FAIL 经 U1 Lookup 取消修复后 Unknown02 11/11；五旧回归及独立 A/B 实际断言通过。独立 B 的监督器 Wait、2个资源清理与 watcher join 有记录，但外部工具 `session20487` 恢复后返回 Unknown process，原 terminal/exit 缺失。后续 current-clear 只证明当时 PID/资源不存在，不补原工具终态、不推断机器重启；完整接受仍沿[报告中的限定组合](d11-work-structure-verification.md)。

最新独占环境核对：Runner误启native与Model LoginOwner42057监督尾重叠，保留其主体观察及实际退出事实，撤回整窗接受；Variables独验90414独占性未证，后继64224同输入新独占窗完整PASS仅作为新轮接受。UI read02原无tuple TCP差异独立保留，不推同一原因。

其他历史原 FAIL 与受限组合结论保留在对应验收报告及基线文件历史；这里不复制每轮日志，也不把后验通过改写为原失败原因已知。

## 停止项与真实阻塞

| 停止项 | 当前边界 | 恢复所需条件 |
| --- | --- | --- |
| Object runtime join | [旧退出屏障失败](object-runtime-join-regression.md)与[旧中断卡](../work-items/recovery-object-runtime-join.md)保留；[隔离安全修复](../work-items/d05-object-runtime-join-safe.md)已实现共同退出、首次 writer/确认 owner 与有界推进，三次分段证据与限定修复已独立接受，并由后继[默认项目创建](../work-items/d08-project-creation-app-ui.md)真实 Skills/Object 初始化链消费。 | 旧 FAIL 不回写，原网络故障 probe、事务代理/截断/异常 backend 保留或改派重建仍 STOP；完整 Registry/Cleanup、Artifact/Project 全生命周期、foreign 终局与生产部署 ready 未由这些有限结果证明。 |
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

1. 由主线程按[团队流程](README.md) fetch origin，检查 `ai/*` 分支和 [current.md](../../../.agent-state/current.md)，能定位任务就自动接续；再核分支、提交、实际差异与上表目标卡，dirty 改动先保存，不覆盖其他任务工作。
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
