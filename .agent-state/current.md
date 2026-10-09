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

## 当前所有权与任务

- 按当前用户要求，root负责统筹、规划、安排、汇报、验收决策与全部Git操作；实现、排查、测试及文档由子代理执行。本文件与全局台账已交接给 `/root/work_ui` 唯一写入，原实施实例停写；两领域专属卡仍由对应负责人维护。检查点明确列文件，未完成必要源码也应按可恢复片段保存并如实注明未编译或失败；保存不代表验收。
- 六名子代理按阶段轮转：`/root/work_ui`、`/root/model`、`/root/variables_ui`、`/root/skills`、`/root/runner`、`/root/knowledge`分别接续Work UI、Model诊断、普通Variables Owner管理UI、Skills、Runner与Knowledge；Variables作者已转普通Variables Owner管理UI，独验者完成Knowledge Business harness窄审后恢复Skills，不为每线永久配置作者与独验。
- Work 规划 UI：`/root/work_ui`拥有`/workspace/agenteam-work-ui`、`ai/work-owner-planning-ui`。recovery06仍整体FAIL，顺序UI后最终观察/schema/Go后验未闭合；原actualWait与七资源全尾齐。新的限定普通完成方法已实施，作者真实消费者/事件/退役纯控和类型检查通过，冻结交Skills独验；仅recovery detailGET/Structure+Task Lookup，原同体、四截断/held及预算不减。此前planning/read/identity/recovery所有FAIL与缺口保留在[Work卡](../docs/development/work-items/d11-work-owner-planning-ui.md)及Work树必要failure记录，不重复逐轮流水；UI未接受。
- Model：原冻结组合新6/6、旧14/14及独立A/B完整接受；负责人`/root/model`已转main3cea基线`/workspace/agenteam-model-ui-delivery`、`ai/model-ui-delivery`。新main首authority49546整体FAIL，不能把原组合通过外推新装配；下一依该树卡/current的新现场处理必要差异。原Model技术源冻结，旧FAIL/Wait/TCP/独占缺口不回填。
- Variables UI：`/root/variables_ui`拥有`/workspace/agenteam-project-variables-ui`、`ai/project-variables-owner-ui`。普通后端已正式交付main3cea；UI CRUD02整体FAIL且全尾齐，产品getter窄修和native诊断已有限独审。CRUD03已作者真实完整PASS且actualWait/七资源双退役/TCP等全尾齐；仅CRUD/history范围，不回填前两轮根因，其余UI矩阵与最终独立验收仍待完成，细节以该树current和卡为准。
- Skills：`/root/skills`拥有`/workspace/agenteam-skills`、`ai/skills-service`、迁移00027。Persistence/Rollback/CommitRecovery、本域Project Stop12及真实D05组合70196已有作者完整PASS；原受控Object结果与真实D05组合分别记。Migration、AdmissionUnknown、OwnerMetadataCurrentAuthority和最终独立补集仍待；真正Cleanup rev2只到SPEC/00028预留，无SQL实现授权，生产root/创建HTTP未闭合，Object runtime join不解停。
- Runner：`/root/runner`拥有`/workspace/agenteam-runner-control`、`ai/runner-control`、迁移00026。有限Native/Client作者通过；四top组合整体FAIL且全尾齐，三个top的通过不覆盖Protocol清理失败。物理Close join夹具修复已有限独审；A当前权限/凭据失效新源亦有限静审接受，均未冒真实新通过。下一按该卡有限候选、DeviceReader原尾失败及B/C门槛推进，不机械重复已通过组；完整D15未接受。
- Knowledge：`/root/knowledge`拥有`/workspace/agenteam-knowledge`、`ai/knowledge-service`、迁移00025。两个本域缺陷修复获限定独审，Runtime、四组14子及修后Process50756已作者真实完整PASS/原尾齐；原Process及环境/终态FAIL保留。最终独立补集、HTTP/UI/D13/root与全Project生命周期未闭合，B02整卡未接受。
- 新结果并行：Timeline `/workspace/agenteam-task-timeline`、`ai/task-timeline-reader`，SPEC有限独审后typed只读库与contract/Reader纯控已实现，作者真实PG矩阵正在写，零迁移且尚未PG验收；检索benchmark `/workspace/agenteam-search-benchmark`、`ai/search-benchmark`的数据/evaluator已实现，独立语义审发现mustfix，返修前不接受数据，未跑候选backend或真实选型；Secret `/workspace/agenteam-secret-variables`、`ai/secret-variables-owner` rev2下A纯合同WIP，无迁移号/持久服务。三树均正式main3cea基线，各自卡/current保存细节，不以SPEC或pure冒整卡通过。
- 全局迁移保持普通变量00024已正式、Knowledge00025/Runner00026/Skills00027各线所有；00028仅为Skills后续真正cleanup SPEC预留，尚无SQL实现授权，Secret不占号。
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
