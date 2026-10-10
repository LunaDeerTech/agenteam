# 团队任务台账

本页只保存全局产品状态、阻塞和下一步；任务分支的最小恢复状态见[分支记录](../../../.agent-state/current.md)。2026-10-09初始核对基线 `e55ad7d1` 仅做文档及文件存在性核对；下表已按后续实际交付与验证更新，不代表当前HEAD的一次全量测试。

长期范围与完成门槛见[开发计划](../development-plan.md)，协作规则见[团队流程](README.md)，环境与缺失输入见[恢复说明](recovery-2026-10-08-environment.md)。历史代理名、ACK、资源授权和 STOP 都是当时记录，不表示本次仍有活动实例、进程或可用测试窗口。

## 当前恢复点与并行所有权

Task Planning、B0-P限定Blocker服务、Work Owner HTTP/root及普通Variables Owner后端已正式交付main；D13固定语料＋离线评分工具、Secret Variable A纯合同、Secret Audit安全读端与Knowledge B02有限Service也已独立交付，Project唯一Skills CleanupPhase授权及Knowledge五查询metadata/tree只读HTTP也已正式交付，Runner迁移00026对应Linux/amd64限定能力也已正式交付，Skills P2初始化/00027也已正式交付，最新main为`3b7ed9da`。各线FAIL保留在领域卡/current。迁移00024–00027已正式；00028有界cleanup尚未完成；D05树9c4a2fd7仅装配25–27稳定依赖前缀。00028共享cleanup索引唯一writer为Knowledge/D05，00029 Secret存储唯一writer为Model/D04，00030归Model/D10 Owner、迁移专项已通过但仍属WIP未正式交付，Skills不再占28；依赖装配不等于整模块完成。

最新接续：Model原组合新6/6、旧14/14与独立A/B接受；新main两Audit完整通过，Model authority仍待。Knowledge B02作者/六组独立补集、主线共享整合与组合编译已闭合并正式交付，原失败保留。Work recovery08业务FAIL且环境切换丢失部分原尾，恢复限定清理不补PASS；单Project GET完成接缝已离线独审接受，09待新窗口。Variables CRUD03通过，Authority03仍FAIL/全尾齐，剩余detail GET真实消费者证明待补；归档409局部后验不提升整组。Runner修后B pending首子完整PASS/全尾齐，原3175整体FAIL及其committed子限定PASS分列保留；C三个子场景、默认Runner OS03及其正式Go入口1top/3sub均完整真实PASS，各自原Wait与资源尾齐，旧OS01/02 FAIL保留；Management独验按不变Concurrency三子/Unknown两子加新LogoutOrder两序共七子有限接受，旧Management01 wholeFAIL不回填。旧DefaultProcesses业务通过但原TCP delta尾失败，整轮FAIL保留；新main装配Default与CLI三项原回归均已完整PASS、原全尾齐；Runner限定00026能力已独立接受并正式交付main，按固定版本组合接受，不称完整D15或当前HEAD单次全量。Skills已有初始化/Stop、真实D05及修后三组完整作者结果，P2有限独立补集也已完整PASS，P2初始化/00027现已正式交付main；原P2-01 FAIL保留；新Cleanup两top/五sub已完整PASS、有限接受，old-attempt/成本/root仍待。

新任务按已验依赖推进：Work的`/workspace/agenteam-task-timeline`只读Reader获有限独验、三PG候选就绪但实际未验。Knowledge在`/workspace/agenteam-object-metadata-cleanup`的D05首批32总额/最终事务/Unknown两方向已完整PASS、七资源全尾齐，专用索引迁移1top/3sub亦完整PASS，历史/Stop期限与proxy join测试返修已审、新候选就绪但未运行；历史规模/执行计划成本仍待，真实Skills最后同Tx组合已有下述有限实证；Model在`/workspace/agenteam-secret-variable-storage`的D04 core3已完整真实PASS、全尾齐，覆盖replay/effects、5类原子回滚与9类约束；maintenance与固定恢复四top/十格亦已完整真实PASS并有限接受，正式00029交付仍待连续迁移前缀，真实D10 provider/Owner未验。Variables回旧UI，Runner限定00026结果已正式交付；Knowledge的`/workspace/agenteam-knowledge-http`五类metadata/tree Owner只读HTTP已独立接受并正式交付main，作者PG/native与独立补集闭合，未接root；Work负责的`/workspace/agenteam-project-skills-cleanup`已完成唯一Skills CleanupPhase授权，作者三个真实PG顶层与独立PG补集均完整通过，已限定接受并正式交付main（ce65714a）；Skills实际清理已有有限实证，生产绑定仍未完成。新增三个作者树基于正式ce657：Skills 的 `/workspace/agenteam-skills-cleanup` 完整Cleanup库pure/race/vet及独立受控补集通过，真实D05/MinIO两top/五sub已完整PASS且全尾齐，最后两域同Tx及Unknown恢复获有限接受；原setup FAIL保留，old-attempt/成本/root仍待；Model 的 `/workspace/agenteam-secret-variable-owner-service` 已有repository/authority/六能力与单final Tx，六组验收方法独审及候选编译/发现通过，00030 Migration单top九节点已完整PASS，其余五个业务top尚无接受结果，read首组已整体FAIL、原资源/TCP全尾齐，两处读Lookup断言待定位/返修，尚无产品缺陷依据；迁移仍WIP未正式交付；Work 的 `/workspace/agenteam-knowledge-tree-http` 五POST/pure/Schema及实现独审通过，PG01三个top/12子通过但整轮FAIL、原全尾齐；Session时间前置、delete返回ID及native背压timeout/原时限判据修复均已独审，PG03定向Mutations＋Unknown两top六子已整轮PASS、原七资源及完整Wait/TCP/input尾齐，与未变Authority三子/Transactions四子组成作者4top/13sub有限通过；原PG01整FAIL不回填。native02三top六子已整轮PASS、原Wait/private/desc/TCP/input全尾齐；Skills已有限接受实际方法，正实施唯一新Owner/旧actor receipt独立PG格，尚待验收。各树范围与恢复入口见各自current，互不把未验模块当依赖，Object Runtime join仍停止。

| 工作项 | 当前可证状态 | 接手时的下一步与完成边界 |
| --- | --- | --- |
| [D01 低层资源身份](../work-items/d01-resource-identities.md) | **R1纯契约已实现并独立验收**。Tool/Mount/ProjectVariable三个canonical marker与Foundation ID alias，四依赖race/vet及独立3顶层6子例通过。 | 仅标量声明与类型消费；Tool/Mount/Variables真实目录、引用保护和Agent F1仍未闭合，普通与Secret共用变量ID且不同CredentialID。 |
| [D10 Agent 核心与引用](../work-items/d10-agent-configuration.md) | **C1六纯契约已实现、独立验收并正式交付main**。17键AgentCore、3键AgentRef、strict codec/Clone/直接日志投影；作者pure/race/vet与5组独立pure/race通过。 | 仅纯类型与WorkReferences声明。F1真实创建/初始化/Model引用/目录/引用保护未实现；R1已闭合ID定义位置，Tool目录窄投影及其余真实前置未闭合，没有真实Agent或Task指派绑定。 |
| [D10 普通 Project Variables Owner 后端](https://github.com/LunaDeerTech/agenteam/blob/3cea6076bb01693ead2755826826d189626aa3aa/docs/development/work-items/d10-project-variables-owner-http.md) | **限定后端已实现、独立接受并正式交付main（3cea6076）**。已有初始化Project当前Human Owner可经默认Central HTTP创建、读取、分页、更新、删除普通变量，以原意图Lookup/同key显式重放恢复；迁移00024、原子Audit/Outbox及真实退出均闭合。 | 作者库/HTTP/native/默认根与独立三个风险补集按限定版本组合通过，实际Wait与资源尾齐，原FAIL及独占缺口保留。活动树尚未合入新main；不含Secret、Agent白名单/执行注入、Runner环境、UI、Project创建HTTP或完整D10，Object join等停止项不变。 |
| [D11 Work Structure](../work-items/d11-work-structure.md) | **本卡已完成**。四 contract `c3b1ee72` 与实现 `a64fb5e7` 已提交；18 路径均存在，限定技术域至本次基线无差异。 | 可供 Task planning 消费。全18按明确版本组合接受；保留 Unknown01 原 FAIL、U1 修复与独立 B 工具终态缺口。不是当前 HEAD 单次全量测试或完整 D11。 |
| [D11 Task planning](../work-items/d11-task-planning.md) | **本卡规划库已实现、独立验收并正式交付main，完整D11未完成**。契约、三个Human规划命令、Lookup、当前Get/List、同Tx membership、TaskEvent/Outbox及00022已完成；原enclosing日志FAIL与真实投影边界保留。 | 七新PG顶层、四旧Project/Outbox、四旧Structure及独立A/B按限定版本组合已实际通过并完成资源终态；原观察器/非法Project输入/rank fixture/归档输入FAIL保留，测试修正的真实对照与完整top已通过。状态机、指派、执行、删除、生产root与完整D11均未完成。 |
| [D11 Task 流转](../work-items/d11-task-transitions.md) | **T0a与T0b纯契约已实现并独立验收**。状态/角色/Position及Transfer/Lookup/摘要、Human typed历史、严格封套与纯多事实工厂已完成；作者及独立pure/race/vet通过。 | 仅纯决策与类型，不提供授权或提交能力。T0b仅消费B0-C两类；独立公开API各6顶层34子测试实际通过。运行服务仍须真实Agent/Blocker/occupancy与执行事实；完整D11未完成。 |
| [D11 Blocker 纯契约](../work-items/d11-task-blocker-contracts.md) | **B0-C两类纯契约已实现并独立验收**。唯一BlockerID、rely_on/无引用waiting_for_human typed metadata、Create及小payload；strict codec/cap/Clone/log，作者及独立pure/race通过。 | T0b只可消费已接受两类；其余三类和外域引用保持未绑定。B0-P持久服务见下行，跨状态组合与完整流转仍未完成。 |
| [D11 backlog Blocker 持久服务](../work-items/d11-task-blocker-service.md) | **限定服务已实现、独立接受并正式交付main**。Human Owner对未指派backlog Task的两类Blocker可add/resolve/list/lookup；真实完整图、Task版本及同事务历史/Outbox/Activity/receipt与00023已完成。 | 作者pure/race/vet/build、公开契约与runtime独验、真实权限/并发/Unknown/互操作/SQL回滚及旧Planning/Structure回归通过并完成资源终态；独立装配审查已接受。首Persistence整轮FAIL保留，以未受影响子项与修复后的History定向回归组合接受。非backlog、Agent/Executor、HTTP、生产root及完整D11仍未绑定。 |
| [D11 Work Owner HTTP/root](../work-items/d11-work-owner-http.md) | **限定HTTP/root能力已独立接受并正式交付main（f1c94ee5）**。已有initialized Project的当前Human Owner可经正式HTTP规划Milestone/Sprint、未指派backlog Task及两类Blocker；默认Central根实际绑定三服务/三Reader。 | 作者pure/race/vet/schema、分页/权限/恢复/Unknown、native及真实根21能力/生命周期通过；未参与实现者本人分页、网络断连恢复/撤销竞争及真实提交确认退出均完整PASS，实际Wait与各自资源尾齐。首分页/TCP尾、Reader期限判据及独立确认泛SQL断言原FAIL保留，按限定修后组合接受。无新迁移，不包含Project创建、UI、Task状态流转或完整D11。 |
| [D27 Project Model Settings UI](../work-items/d27-project-owner-model-settings-ui.md) | **原限定版本组合接受，新main两Audit完整通过，Model authority仍待**。新6/6、旧14/14、独立A/B完整通过；新树AuditAuthority/AuditNavigation均实际全尾齐，原FAIL与Wait/TCP/独占缺口保留。 | 新树`/workspace/agenteam-model-ui-delivery`基于main3cea；首authority整体FAIL，限定Resolve方法已离线独审、尚待真实验收。两Audit通过仅覆盖其固定输入，不机械重跑旧14，不冒生产SPA、完整D27或后继新main装配。 |
| [D08 Project 与 Owner](../work-items/d08-project-owner.md) | **部分子能力接受，模块未完成**。基础权限/生命周期事实、Owner 读写 HTTP、Audit 与初始化收敛已有交付；[初始化 Audit 授权库](../work-items/d08-project-initialization-audit.md)已正式交付main（ca9f2d5d）。[Skills CleanupPhase授权](https://github.com/LunaDeerTech/agenteam/blob/ce65714aac6eb4995a43fc427a2c77e6497470a7/docs/development/work-items/d08-project-skills-cleanup-authority.md)已实现、独立接受并正式交付main；作者三个真实PG顶层与独立风险补集均完整通过，实际命令与资源尾齐。 | 新授权仅在同Store活Tx、Project锁下核当前Deleting/Cleaning、Owner/版本/manifest及停止/依赖完成事实；零迁移，不执行清理、不授其他participant/final权。Skills/D05最后同Tx组合已有有限实证；registry、创建HTTP/root及完整生命周期推进仍未绑定；初始化Object witness正向组合、Artifact/Object组合未闭合。Object join停止项保持，完整D08未完成。见[领域绑定卡](../work-items/recovery-project-domain-bindings.md)。 |
| [D09 Model System](../work-items/d09-model-system-token-usage.md) | **部分库与 HTTP 接受，模块未完成**。已有配置、Owner/Usage HTTP、Summary、Chat text 与 Embeddings/平台选择 Resolver 等限定成果。 | 生产 Resolution/Invocation 与 consumer 仍未绑定；OpenAI tools 独立动态验收停止，Jina/Image 来源缺口另列。不能把 wire、受控 fixture 或 UI 通过写成真实 Provider/生产调用完成。 |
| [D10 Skills 初始化](https://github.com/LunaDeerTech/agenteam/blob/3b7ed9da35844e3a367cc5e9da0cf424ab36499a/docs/development/work-items/d10-skills-initialization.md) | **P2初始化库、限定PG/真实D05组合及独立补集已接受，并正式交付main（3b7ed9da/00027），完整生命周期未完成**。Persistence/Rollback/CommitRecovery、本域Project Stop、真实D05及修后Migration/AdmissionUnknown/OwnerMetadataCurrentAuthority均通过且全尾齐；受控Object与真实组合分列，迁移00027。 | P2限定独立已完整PASS，旧FAIL保持；00026与00027均已正式交付。真正Cleanup新库纯控/独审及真实D05两top五sub已完整通过、有限接受，含最后同Tx/Unknown；原setup FAIL保留，old-attempt、余下有界/成本及root仍待；00028已移交Knowledge/D05共享cleanup索引，不再由Skills占用，生产root/创建HTTP与全Project组合未绑定，Object join不解停。 |
| [D12 Knowledge B02](https://github.com/LunaDeerTech/agenteam/blob/29dd429881783595e5a1304b86af5bfb56ec80b2/docs/development/work-items/d12-b02-knowledge-service.md) | **有限Human Service及主线共享整合已独立接受并正式交付main（29dd4298），完整D12未完成**。canonical内容/文档树、原子变更、来源复制、本域恢复、Object/Audit/Outbox适配器及00025闭合；作者固定矩阵、独立六组与共享路由回归按限定组合接受。 | 原整体FAIL及终态缺口保留；HTTP/UI、下载URL、D13、B03全Project生命周期、生产root与Object Runtime join均不在本结果，停止项不恢复。 |
| [D12 Knowledge只读HTTP](https://github.com/LunaDeerTech/agenteam/blob/4c1db71cf0f86cb6d6330167577944b0466c8664/docs/development/work-items/d12-knowledge-owner-read-http.md) | **五查询metadata/tree Human Owner GET/HEAD及安全DTO/OpenAPI已独立接受并正式交付main（4c1db71c）**。作者真实PG/native与独立风险补集闭合，原命令与资源尾齐。 | 独立adapter消费正式B02；不含默认root、正文/下载、树命令、UI或完整D12。树命令在独立作者树开发，不依赖未验实现；旧FAIL保留。 |
| [D13固定语料＋离线评分工具](https://github.com/LunaDeerTech/agenteam/blob/b2a7d0abef0b7ca0623c8e2a7c020658948848da/docs/development/work-items/d13-lexical-benchmark.md) | **独立子结果已接受并正式交付main（b2a7d0ab）**。lexical-v2语料语义返修获独立接受，评分工具独立128控通过；原语义/夹具FAIL保留，任务分支关闭。 | 只交付固定语料和可重复离线评分工具；没有三候选backend真实成绩、性能/具体版本许可或最终选型结论，不完成D13。 |
| [D15 Runner 身份与控制通道](https://github.com/LunaDeerTech/agenteam/blob/fb6ab7f492850bf1d3c025a59acbff381312a989/docs/development/work-items/d15-runner-control.md) | **迁移00026对应Linux/amd64限定能力已独立接受并正式交付main（fb6ab7f4）**。设备管理/登记、私有身份、出站WSS代际与有界在线视图、空operation registry和默认双cmd已闭合；作者业务、独立七子、正式OS三格及new-main Default/CLI完整尾分别通过。 | 固定版本组合接受，原Device/Protocol/B/OS/Default等FAIL保留；不代表当前HEAD单次全量或完整D15。macOS/其他架构、跨UID及实际Dispatch/Mount/D16–18绑定未完成，ready503与Object join等原停止项不变。 |
| [D10 Secret Variable A](https://github.com/LunaDeerTech/agenteam/blob/8cb0a95338dc417ff34be06c00086fbbc7159efa/docs/development/work-items/d10-secret-variables-owner.md) | **纯合同/OpenAPI子结果已独立接受并正式交付main（8cb0a953）**。材料生命周期、安全DTO、独立命令/Lookup、Catalog和F1引用合同闭合；普通变量合同未扩义，零迁移。 | Audit读兼容见下行；D04专用provider、Owner持久服务/HTTP、真实F1授权与生产绑定仍待。公开合同和plan形状不作为权限/SQL事实，不完成Secret Owner或D10。 |
| [D10 Secret Variable Audit读端](https://github.com/LunaDeerTech/agenteam/blob/e94077ebe1810d9d61918d5fe4a4d932d8bbfba2/docs/development/work-items/d10-secret-variable-audit-read.md) | **完整纯读子结果已独立接受并正式交付main（e94077eb）**。三个Project Secret Variable action的typed metadata、真实row decoder、HTTP安全投影、OpenAPI与TS严格读兼容及独立控制已闭合。 | SQL CHECK、普通事实authority、System记录闭集未扩；不含真实Audit持久写入、D04值Audit、Owner事务或生产绑定，不完成Secret Owner或D10。 |

D08–D28 各模块仍有未完成范围，E01 未开始；首个未完成模块编号为 D08，但只消费已接受子能力的其他完整结果可并行。Human Owner规划HTTP与对应默认root已限定绑定；完整Task状态/执行与UI、生产Skills/创建HTTP、Resolution/Invocation及D24仍未完整绑定；现有 `ready=false` / `/readyz` 503 的产品边界不因本次文档整理改变，隔离fixture中的真实root启动与局部测试不证明生产部署或全平台ready。

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
