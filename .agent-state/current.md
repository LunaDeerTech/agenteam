# 当前执行检查点

## 目标与恢复

- 持续完成 D01–D28 产品、真实集成及 E01 平台内游戏复刻验收；总体状态仍为**进行中**。完整 D11/D27、生产平台和 E01 均未完成，不能把本分支的 WIP 当正式交付。
- 当前活动分支：`ai/product-continuation`。起点为远端 `ai/task-planning-recovery` 的 `da4953f5`，已完整保留其 Model 未验成果；正式 main 已正常整合至 `097e4103`，远端精确确认。不要从旧默认 `work` 或旧活动分支重新开发。
- 本地 `origin` fetch refspec 最初只跟踪 main。新环境先检查 dirty/index 并保留工作，再 `git ls-remote --heads origin 'ai/*'`，显式 fetch 活动分支；不能因本地缺 remote ref 推断无活动工作。
- 原逐轮记录保留在本文件 Git 历史、对应正式工作项和必要失败 JSON/probe 中；不删除或回填历史 FAIL。下述是最新接续信息，旧代理名/进程/资源授权不延续为新会话事实。

## 已正式交付

- Task Planning `0273a604`、Agent C1 `7f2bb211`、R1 `dbf4a5e0`、T0a `ad8b1fb6`、Blocker B0-C `70b16f06`、Task T0b `11c16867` 已接受并在 main；各自真实生产边界见台账，不重复无变化验收。
- 共享浮层按激活顺序绘制：`0bb98f2a` 已推送 main，16 真实组件场景及完整前端检查通过。
- 确认关闭、原输入同tick解禁后的安全回焦：`097e4103` 已推送 main，114组件单元、8独立定向、完整2640单元/类型/构建、21真实浏览器全部通过。只对原节点延后重试，焦点/层栈/页面/生命周期变化取消重试；见 D27卡 §0.3。原组件18FAIL、临时监督器清理证据缺口和 discovery setupFAIL 保留。

## 当前所有权与任务

- root 独占本文件、全局台账、迁移编号协调与全部Git操作。代理只写各自授权范围；检查点明确列文件，未验但可构建片段应保存，不提交未闭合无法构建源码。
- `/root/model_delivery` 接续 D27 Model Settings、专属测试夹具/恢复脚本及必要共享组件返修；其 `recover_harness` 实施，`independent_acceptance` 独验。共享焦点已正式交付，后续新产品修改仍按风险独验。
- `/root/service_delivery` 负责 B0-P Human未指派backlog Blocker持久服务；`blocker_implementation` 与其分配测试路径，`blocker_spec_review` 独立审查/亲自执行A/B。唯一迁移为 `00023_task_blockers.sql`；完整范围及接口见 `docs/development/work-items/d11-task-blocker-service.md`。
- 所有实例统一显式请求 Astra/Ultra；工具无service tier字段，实际Fast未确认。当前7席动态共享；已完成实例续派用followup_task，不用send_message冒启动。

## B0-P实际状态与下一步

- 完整产品/00023/作者测试/独立probe均可构建且已保存。四包pure/race/vet、两入口build、作者integration race编译/精确发现、独立公开契约pure/race及runtime静态接受通过。
- 首Persistence整体FAIL保留：history=4096新增缺BLOCKER_HISTORY_LIMIT字段；其它正常操作/历史恢复、00022已填充升级重跑/DDL回滚、Project大容量子项body通过。唯一字段修复已独审，并以HistoryCapacityRegression真实完整PASS复验，不把原整轮改PASS。
- 作者Authority、Unknown、Concurrency真实完整PASS；独立A/B由未参与实现者本人执行并完整PASS。全部这些轮均有实际Go/driver/外层退出、两自有资源/runtime/hostTCP双清、输入不变。
- **仍待3项真实门槛**：`TestTaskBlockerAtomicity`、`TestTaskPlanningAtomicityAndEvents`、`TestWorkStructureAtomicityAndProducer`。全部当前已编译/发现、固定输入就绪，不重复编译或重跑无影响检查。Interop已完整PASS（96788 exit0/72.722s），包含真实归档进入/UpdateSprint双顺序及两门禁，不代表归档cleanup完成。下一轮安排Atomicity，必须等当前Session探针窗口完整结束再启动。
- 当前作者完整binary：`output/ai/task-blocker-service/implementation/work-blocker-complete-race.test`；driver：`output/ai/task-blocker-service/author/pg-only-driver`；使用已有 `.agent-state/task-planning-recovery/pg_only_supervisor.py`，精确一个selector、原105s及完整资源尾，output为`output/ai/task-blocker-service/pg`。日志/二进制可重建；必要源码在正式tests和`.agent-state/task-blocker-service/`。
- 不提供Agent/执行/跨状态Blocker、HTTP、生产root或完整D11。剩余4动态通过并最终独验闭合后，整理为一个完整原子正式提交并推送main。

## D27实际状态与下一步

- 六新case历史接受4项（recovery/read/configuration/credential）；authority/navigation仍FAIL。共享层两次产品修复后，受影响recovery/configuration/credential须用新资产补验；read只有单层，核相关输入/依赖无变化后可复用。旧14与独立A/B真实仍未完成。
- 最新authority第5完整FAIL：Session headers已见、finished未见、failed已见。需核真实account客户端同一body EOF/cancel与Playwright/CDP事件差异；不能去掉EOF门槛、重发GET或凭CDP事件猜根因。各轮Model业务测试均已完成自有资源清理。
- 最新navigation第4在继续编辑后焦点断言FAIL，原值保留。共享焦点局部正反例已证实并完成正式修复，但不回填原navigation因果；下一需新资产真实复验。
- 精确失败输入在 `.agent-state/model-ui-recovery/*failure.json`；完整主harness为两Model Go、config/spec及该目录case模块。独立A/B四源在`.agent-state/model-ui-independent/`，已编译/类型/发现并随新Go夹具复编，**尚未真实执行**。
- 新组件固定1/21监督器 `run-shared-components.py` 已有有界实际Wait/descendants/listener处理；单例及21矩阵当前完整PASS。最初临时runner留下已退出孤儿zombie PID115611/PPID1，无活测试进程/监听，原actualWait/Viteclose无法补回；不杀PID1，不称原清理PASS。
- Session消费探针 `session-consumption-probe.mjs` 已有保存基线；最新去除重复监督、复用组件清理的返修及新增 `run-session-consumption.py` 已闭合冻结并保存，node/Python语法与8input离线准备通过；使用固定30+15秒总界的任务自有worker执行。真实8case已运行：完整length/chunked的native/正式client均EOF与解码成功且PW/CDPfinished；断连无EOF/failed，缺字节JSON有EOF但解码拒绝，不支持“正常EOF后cancel导致假failed”猜测。80128原外层exit1仅runtime-empty未过；所有实际Wait/close/desc/listener齐。root接管后精确核已登记PID和owned监听均不存在，删除唯一任务自有regular0600 Chromium残留及空目录/同身份marker，另写post-cleanup记录；原terminal逐字事实保持FAIL，后验current-clear不回填原通过。下一沿同响应安全读取/abort诊断定位主authority，不放宽EOF门槛。
- Model私有dist已同步本次2640全检查构建的64文件，与web/dist逐字节同一。后续业务用该新资产，旧PASS不等于新资产通过。

## 环境与共享资源

- Go固定 `/workspace/toolchains/go1.27.1/bin/go`，1.27.1。完整固定依赖cache为 `/workspace/agenteam/output/ai/model-ui-recovery/go-mod`；各树GOPROXY=off只读复用，GOCACHE独占。不为默认cache缺项重复联网。
- Model隔离Go构建树 `/workspace/agenteam-delivery` 固定已接受 `11c16867`、末迁移00022；只增加与原树相同的两Model Go测试，不能复制新00023混入当前Model闭包。原路径不存在时由root以该基线创建worktree并复制明确两源。
- 正式组件整合树 `/workspace/agenteam-shared-layer-delivery` 当前 `ai/shared-layer-delivery`，包含已推送main的两组件提交；不混入未验Model或B0-P产品。
- 固定Chromium、锁定Playwright、PG/MinIO及helpers已恢复；可重建产物在output。既有dev infra不属于任务，不连接、不清理。
- PG/browser/hostTCP只运行一个明确资源窗口。实际Go/driver/外层终态及资源尾结束前，不进行Git网络、下载或另一browser/socket测试；离线纯测试与无输入冲突写入可继续。每轮完整终态才交接，原失败不补写PASS。
- 当前所有上一轮实际资源均已释放；root正在最终Git保存，完成后交B0-P Atomicity；Session8实际资源已由root核清。源、binary、fixture实际冻结范围按各driver确定，不把旧binary称作新增测试通过。

## 保留停止项与最终验收

Object runtime join、OpenAI tools独立动态、Central SPA concurrent-publication、Jina/Image来源停止条件仍沿任务台账，不因本次恢复自动解停。其他就绪工作继续。

E01尚未开始，平台全部前置未齐。实施游戏前必须冻结确切版本、完整内容分母、权重、关键门槛和可复现覆盖率；最终须使用agenteam本身组织任务/Agent/Execution/审核/产物，并真实试玩与独立验收至少50%完整内容，不能以局部模块或mock代替。
