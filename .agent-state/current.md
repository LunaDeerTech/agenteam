# 当前执行检查点

## 目标与恢复

- 持续完成 D01–D28 产品、真实集成及 E01 平台内游戏复刻验收；总体状态仍为**进行中**。完整 D11/D27、生产平台和 E01 均未完成，不能把本分支的 WIP 当正式交付。
- 当前活动分支：`ai/product-continuation`。起点为远端 `ai/task-planning-recovery` 的 `da4953f5`，已完整保留其 Model 未验成果；正式 main 已交付D08初始化Audit授权库 `ca9f2d5d`、B0-P `0b44e26a`及Work Owner HTTP/root `f1c94ee5`，后者远端精确确认 `f1c94ee520e8153e935fea7e7ed269e7e8b9adca`；活动树继续保留 Model 未验成果，合并与检查点由root处理。不要从旧默认 `work` 或旧活动分支重新开发。
- 本地 `origin` fetch refspec 最初只跟踪 main。新环境先检查 dirty/index 并保留工作，再 `git ls-remote --heads origin 'ai/*'`，显式 fetch 活动分支；不能因本地缺 remote ref 推断无活动工作。
- 原逐轮记录保留在本文件 Git 历史、对应正式工作项和必要失败 JSON/probe 中；不删除或回填历史 FAIL。下述是最新接续信息，旧代理名/进程/资源授权不延续为新会话事实。

## 已正式交付

- Task Planning `0273a604`、Agent C1 `7f2bb211`、R1 `dbf4a5e0`、T0a `ad8b1fb6`、Blocker B0-C `70b16f06`、Task T0b `11c16867` 已接受并在 main；各自真实生产边界见台账，不重复无变化验收。
- 共享浮层按激活顺序绘制：`0bb98f2a` 已推送 main，16 真实组件场景及完整前端检查通过。
- 确认关闭、原输入同tick解禁后的安全回焦：`097e4103` 已推送 main，114组件单元、8独立定向、完整2640单元/类型/构建、21真实浏览器全部通过。只对原节点延后重试，焦点/层栈/页面/生命周期变化取消重试；见 D27卡 §0.3。原组件18FAIL、临时监督器清理证据缺口和 discovery setupFAIL 保留。

- Work Owner规划HTTP及默认根 `f1c94ee5` 已原子提交并推送main，39路径经最终独立接受；限定已有initialized Project的当前Human Owner，不含创建、UI或完整D11。
- 普通 Project Variables Owner 后端 `3cea6076bb01693ead2755826826d189626aa3aa` 已独立接受并正式推送main、远端精确核对及候选clean完成；含00024、六能力HTTP与默认根，限定已有初始化Project当前Human Owner的普通变量。此活动树尚未合入该main，不称Secret、Agent/Runner注入、UI、创建HTTP或完整D10完成。

- D13固定原创语料＋可重复离线评分工具已独立接受并正式交付main `b2a7d0ab`；数据语义修复与评分工具分别独审通过，检索benchmark分支已关闭。范围见[正式卡](https://github.com/LunaDeerTech/agenteam/blob/b2a7d0abef0b7ca0623c8e2a7c020658948848da/docs/development/work-items/d13-lexical-benchmark.md)；没有实际候选backend、性能、许可或最终选型结论，不完成D13。
- Secret Variable A纯合同、独立OpenAPI及复验资产已独立接受并正式交付main `8cb0a95338dc417ff34be06c00086fbbc7159efa`；[正式卡](https://github.com/LunaDeerTech/agenteam/blob/8cb0a95338dc417ff34be06c00086fbbc7159efa/docs/development/work-items/d10-secret-variables-owner.md)限定材料生命周期、独立Secret命令/Lookup、Catalog与F1引用合同，不含持久服务、HTTP实现、D04 provider或生产绑定。没有新迁移，D13既有交付保留。
- Secret Variable Audit合同与安全读端已独立接受并正式交付main `e94077ebe1810d9d61918d5fe4a4d932d8bbfba2`，远端已精确确认；[正式卡](https://github.com/LunaDeerTech/agenteam/blob/e94077ebe1810d9d61918d5fe4a4d932d8bbfba2/docs/development/work-items/d10-secret-variable-audit-read.md)限定三个Project Secret Variable action的Go/HTTP/OpenAPI/TS严格读兼容。未扩SQL CHECK、普通事实authority或System记录，不提供真实Audit写入、D04值Audit、Owner事务或生产绑定。

- Knowledge B02有限Human Service已正式交付main `29dd429881783595e5a1304b86af5bfb56ec80b2`（parent e94077eb，51路径），远端精确确认；[正式卡](https://github.com/LunaDeerTech/agenteam/blob/29dd429881783595e5a1304b86af5bfb56ec80b2/docs/development/work-items/d12-b02-knowledge-service.md)限定canonical内容/文档树、原子变更、来源复制、本域恢复及Object/Audit/Outbox适配器与00025。作者固定矩阵、独立六组补集、主线共享路由独审及当前组合编译已闭合；原FAIL不回填，不含HTTP/UI/D13、全Project生命周期、生产root或Object Runtime join。

- Knowledge五类metadata/tree Human Owner GET/HEAD、安全DTO与独立OpenAPI已独立接受并正式交付main `4c1db71cf0f86cb6d6330167577944b0466c8664`；[正式卡](https://github.com/LunaDeerTech/agenteam/blob/4c1db71cf0f86cb6d6330167577944b0466c8664/docs/development/work-items/d12-knowledge-owner-read-http.md)限定消费正式B02的只读HTTP，作者真实PG/native及独立补集已闭合。未接默认root，不含正文、下载、树命令/UI或完整D12。

## 当前所有权与任务

- 按当前用户要求，root负责统筹、规划、安排、汇报、验收决策与全部Git操作；实现、排查、测试及文档由子代理执行。本文件与全局台账已交接给 `/root/work_ui` 唯一写入，原实施实例停写；两领域专属卡仍由对应负责人维护。检查点明确列文件，未完成必要源码也应按可恢复片段保存并如实注明未编译或失败；保存不代表验收。
- 六名子代理按阶段轮转：`/root/work_ui`负责Knowledge树命令HTTP、Work UI、Timeline与本全局文档；`/root/model`在已验D04子能力上推进D10 Secret Owner库并保留Model后继验收；`/root/variables_ui`负责普通Variables UI及交叉独审；`/root/skills`推进真正Skills Cleanup并接独立补验；`/root/runner`收口Runner默认OS及主线装配；`/root/knowledge`已交付Knowledge只读HTTP并保留D05有界cleanup后继。各线按就绪输入交叉独审，不为每线永久配置作者与独验。
- Work 规划 UI：`/root/work_ui`拥有`/workspace/agenteam-work-ui`、`ai/work-owner-planning-ui`。recovery08业务FAIL于归档后Project refresh原finished等待；环境切换丢失driver/outer/reap/TCP/input原尾，恢复仅精确清除manifest残余并核七ID/3private/runtime双清，四orphan Z/ECHILD不补原通过。窗口已由root释放。单Project根GET的原消费/Workspace发布完成接缝已获Skills独立有限接受，Work09输入离线就绪、等待新真实窗口；既有方法与新纯控不升动态接受，旧FAIL与缺口见本树卡及failure记录。
- Model：原冻结组合新6/6、旧14/14及独立A/B完整接受；main3cea基线`/workspace/agenteam-model-ui-delivery`、`ai/model-ui-delivery`的AuditAuthority与AuditNavigation两精确top均已完整PASS、实际全尾齐。新Model authority仍未通过；其限定Resolve方法已离线独审，等待真实验收，首authority FAIL与旧Wait/TCP/独占缺口不回填。原技术源冻结，负责人`/root/model`同时推进下述D04独立树。
- Variables UI：`/root/variables_ui`拥有`/workspace/agenteam-project-variables-ui`、`ai/project-variables-owner-ui`。CRUD03作者真实完整PASS且全尾齐，仅接受CRUD/history；Authority03仍整体FAIL、实际全尾齐。新归档409的原typed拒绝/owner尾及零command后验已证，剩余首拒绝为唯一detail GET的普通完成门，native EOF/取消尾不能代替typed发布。下一只补该原消费者证明，不放宽普通门槛或提升完整权限矩阵；Authority01/02原FAIL及其余未验矩阵见该树current和卡。
- Skills：`/root/skills`拥有`/workspace/agenteam-skills`、`ai/skills-service`、迁移00027。Persistence/Rollback/CommitRecovery、本域Project Stop、真实D05组合及修后三组Migration/AdmissionUnknown/OwnerMetadataCurrentAuthority均已作者完整PASS/全尾齐。P2独立2pure/7sub与真实1top/4sub已完整PASS，原P2-01 FAIL保留；真正Cleanup rev2正在新树实施并消费新的D05有界provider，Skills不再占00028，生产root/创建HTTP未闭合，Object runtime join不解停。
- Runner：`/root/runner`拥有`/workspace/agenteam-runner-control`、`ai/runner-control`、迁移00026。有限Native/Client、修后Protocol及A当前权限/凭据失效＋DeviceReader作者真实通过，均有完整尾；原四top组合FAIL保留。B首次3175整体FAIL且全尾齐：pending子原handler未在期限内返回，committed子限定PASS；原请求body EOF/Close修复及唯一first-sub映射已独立有限接受，修后pending首子现已完整PASS、实际全尾齐；它与旧committed子结果分列，不回填3175整轮。C三个子场景与默认Runner OS03三格均已完整真实PASS，原Wait及各自资源尾齐；OS03包括EOF、双信号和原deadline，旧OS01/02 FAIL保留。独立Management按不变Concurrency三子/Unknown两子加新LogoutOrder两序共七子有限接受，新轮全尾齐、旧Management01 wholeFAIL不回填；正常DefaultProcesses、正式主线装配及完整D15仍待。
- Knowledge：B02有限Service已按上述正式main交付；独立末子82746、必要共享整合独审与e940装配integration编译已闭合，旧89530/62452/837326等FAIL保持。`/workspace/agenteam-knowledge-delivery`可停止交付分支工作，旧作者/独立树仅保恢复证据；`/root/knowledge`的`/workspace/agenteam-knowledge-http`只读HTTP已按上述正式main交付，可停止该有限分支；原FAIL保留，不接root、正文、下载或D13。D05后继边界见下文。
- Timeline：`/root/work_ui`拥有`/workspace/agenteam-task-timeline`、`ai/task-timeline-reader`。四类Human Task只读合同/Reader获Knowledge有限独验，三个作者PG矩阵已race编译/精确发现，零迁移，尚未真实PG验收；细节见该树卡/current。
- 新依赖结果：Knowledge在`/workspace/agenteam-object-metadata-cleanup`的D05首批真实32总额、最终同Tx提交/回滚及未转发/服务端已提交丢响应两种Unknown已完整PASS、实际七资源全尾齐；只含明确fixture父mapping，不替代专用迁移/历史规模/执行计划成本和真实Skills最后同Tx组合。Model在`/workspace/agenteam-secret-variable-storage`、`ai/secret-variable-storage`的D04 core3已完整真实PASS、全尾齐，覆盖replay/effects、5类原子回滚与9类约束；maintenance与固定恢复四top/十格亦已完整真实PASS、有限接受；00029正式交付仍待26–28连续前缀，真实D10 provider/Owner未验，不称Owner持久实现通过。D05 Runtime join停止项保持。
- Project Skills清理授权：`/root/work_ui`的`/workspace/agenteam-project-skills-cleanup`、`ai/project-skills-cleanup-authority`基于正式main29dd；仅SkillsParticipant/CleanupPhase当前事实门禁已实现、独立接受并正式交付main（ce65714a）。作者pure/race/vet、3个真实PG顶层32子项及独立PG补集1顶层3子项均完整通过，实际命令与资源尾齐。零迁移，不授其他participant/final写权；Skills实际清理、D05最后同事务组合及生产registry/root仍未绑定。
- 新增产品作者树均基于正式main ce657：Skills 在 `/workspace/agenteam-skills-cleanup`（`ai/skills-cleanup`）的真实Cleanup库已实现，纯race/vet及独立受控Store/Object补集通过，真实D05/MinIO两top组合源码已齐、实际未验；Model 在 `/workspace/agenteam-secret-variable-owner-service`（`ai/secret-variable-owner-service`）已实现安全repository、D04 authority、查询与单final Tx首版，读层/权限/计划pure通过，事实路由与写事务控制及真实PG未闭合，00030仍草案；Work 在 `/workspace/agenteam-knowledge-tree-http`（`ai/knowledge-owner-tree-http`）的五POST adapter/pure/Schema已通过并获实现独审，PG四top及native三top候选已离线编译/精确发现，实际PG/native与入口独审仍待。三树以各自current为恢复入口，互不依赖未验HTTP或共享root，不提升完整D10/D12。
- 全局迁移：普通变量00024与Knowledge00025已正式；00026 Runner/00027 Skills仍归各线。D05树9c4a2fd7已精确装配25–27作为稳定迁移依赖前缀，仅表示依赖齐备，不表示Runner/Skills/D05整模块完成。00028共享cleanup索引唯一writer现为Knowledge/D05，Skills不再占号；00029 Secret存储唯一writer为Model/D04，00030已预留Model/D10 Owner草案；正式SQL交付仍按连续前缀和各卡门槛，不跳号或借号。
- 上述新树基于正式main，各自current只记本任务；原树维持全局唯一台账。共享物理资源与真实窗口统一由root调度，源码仅在各自授予写域内实施。
- 所有实例统一显式请求 Astra/Ultra；工具无service tier字段，实际Fast未确认。当前7席动态共享；已完成实例续派用followup_task，不用send_message冒启动。

## 已交付与历史证据入口

B0-P、Work Owner HTTP/root与普通Variables后端已按限定版本组合独立接受并正式交付main；范围、原FAIL及必要补验分别见[Blocker卡](../docs/development/work-items/d11-task-blocker-service.md)、[Work HTTP卡](../docs/development/work-items/d11-work-owner-http.md)和[任务台账](../docs/development/agent-team/tasks.md)。Model原组合与新main装配分开，以[Model卡](../docs/development/work-items/d27-project-owner-model-settings-ui.md)及新树current为准。此处删除的是重复旧阶段叙述，原日志、failure JSON与Git历史不改写；不能由后验current-clear补原Wait/TCP/独占缺口或推旧根因。

## 环境与共享资源

- Go固定 `/workspace/toolchains/go1.27.1/bin/go`，1.27.1。完整固定依赖cache为 `/workspace/agenteam/output/ai/model-ui-recovery/go-mod`；各树GOPROXY=off只读复用，GOCACHE独占。不为默认cache缺项重复联网。
- 正式组件整合树 `/workspace/agenteam-shared-layer-delivery` 当前 `ai/shared-layer-delivery`，包含已推送main的两组件提交；不混入未验Model或B0-P产品。
- 固定Chromium、锁定Playwright、PG/MinIO及helpers已恢复；可重建产物在output。既有dev infra不属于任务，不连接、不清理。
- PG/browser/hostTCP只运行一个明确资源窗口。实际Go/driver/外层终态及资源尾结束前，不进行Git网络、下载或另一browser/socket测试；离线纯测试与无输入冲突写入可继续。每轮完整终态才交接，原失败不补写PASS。
- 真实窗口和Git仍逐次归root调度；当前窗口状态以负责人本人actualterminal为准，不从进程当前不存在推原PASS。每轮仍须原Go/driver/outer actualWait、精确资源双退役及runtime/private/desc/TCP双尾；旧hostTCP/独占/Wait缺口不回填。

## 保留停止项与最终验收

Object runtime join、OpenAI tools独立动态、Central SPA concurrent-publication、Jina/Image来源停止条件仍沿任务台账，不因本次恢复自动解停。其他就绪工作继续。

E01尚未开始，平台全部前置未齐。实施游戏前必须冻结确切版本、完整内容分母、权重、关键门槛和可复现覆盖率；最终须使用agenteam本身组织任务/Agent/Execution/审核/产物，并真实试玩与独立验收至少50%完整内容，不能以局部模块或mock代替。
