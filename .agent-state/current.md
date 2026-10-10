# 当前执行检查点

## 目标与正式基线

持续推进 D01–D28 的产品能力和真实组合，最终完成 E01 平台内游戏复刻验收。当前平台约完成 **30%（25%–35%）**，这是按设计能力及端到端闭环权重作的工程粗估，不是逐卡等权或客观审计。E01 尚未开始，其“至少 50% 游戏内容”是独立的未来验收目标。

正式 main `6fa3e147be637dc9c5b11ba198b232cbecba7349` 已提交、普通推送并经远端ls-remote确认；24路径交付原key到期重试、真实temporary checkpoint及耗尽原子技术失败结算/00050，产品与实测源码`08428005`一致。此前 main `fd939099` 的25路径交付显式retry配置、Claim不可变policy绑定/00049与真实Execution锁超时temporary证明；`031b5df9` 的29路径交付Owner原子解除blocker并blocked→todo、HTTP兼容、00048及显式参数retry policy库。前一main `6cf00865` 的282路径有限包包括Agent默认配置创建及其真实提供方、Task Human指派与HTTP、StartSprint与HTTP、Scheduler claim/created关联、Busy补偿、有界pending和单类技术失败结算，以及连续00037–00047迁移，原证据按各自输入接受。此前 `18a27db5` 的Human Skill安装/读取清理/Owner HTTP及00032–00036保持兼容，旧metadata、Knowledge读取、D13/D16/D18、Variables/phase/Batch/Guard与D09文本Runtime有限接受保留。默认生产initializer、完整F1/ToolExecution/Dispatcher仍未完成，不提升整体粗估。

- Secret HTTP 的作者 PG/native/root、独立当前 Session 与安全错误补集、完整 app ordinary/race 已按相应版本接受；没有新增生产 Project 创建能力。
- Knowledge 读取 UI 最终组合 read04 wholePASS，原调用及七资源/private/runtime/desc/TCP 全尾闭合。独立三项组件风险测试使用真实 Session/Workspace 和受控网络，不冒真实 Owner 转让或 Logout PG。原 read01/read02 FAIL 保留。D13 的 1 top/3 sub 真实链已正式交付，但未接索引、其他 parser 或生产创建。
- Variables 本实例提供方与后继单轮 phase 引擎均已有限交付。新 phase02 1 top/3 sub wholePASS：真实确认提交后才调用同 Store 停止提供方，原调用及checkpoint实际返回、rollback/冻结版本拒绝和fence竞争通过，原Wait与两资源全部退出尾闭合。claim terminal仅表示本轮尝试退役；完整registry/phase loop、foreign业务join、cleanup和生产绑定仍未完成。原phase01测试hook失败及准备失败保留在topic；Unknown仍仅纯控。
- 有界 Batch 的 7 top/12 sub race 与 vet 通过；Guard01 真实扫描、活进程 Busy 后项推进、原 child SIGKILL/Wait 与 stdout join 后的死亡证明及 fence 重验 wholePASS，七资源14次 absence、704输入一致和全部原退出尾齐。test-only 组合不证明 foreign 业务调用已 join，不恢复 Object Runtime join 或完整 participant 停止。
- D16 只交付 Linux 本地读取库；D18 只交付 SpecRef/不可变名称表，同 ToolID 跨 revision 名称稳定，调用仍依原 Snapshot 表。真实 Registry/F1/Schema/授权、Agent/Executor 与 Runner 执行接线未因此完成。
- 默认生产 Project initializer 仍 **unbound**。旧根执行 PASS 与规范拒绝分别保留；撤回后的默认拒绝/零事实和显式 test-only 真实端口 fixture 不能证明生产创建。必填独立 `AGENTEAM_CENTRAL_KNOWLEDGE_CONFIRMATION_KEYRING` 保持。
- metadata04 真实 Account/Project/Model/Secret 同 Store 原 Tx 的正常读取、stale、当前 Session 拒绝及 rollback 已 wholePASS，原资源和退出尾闭合。此链不含 Agent canonical 创建、外域 References、Skills assignment 或 Registry install 成功。

## 当前并行产品线与执行者

上一组合源码`eca3336f`的Owner原子解除、HTTP兼容、独立事务回滚及显式参数retry policy库已随29路径正式main交付：16 top race/四包vet及native01的HTTP/Atomic各1 top/1 sub wholePASS，1468输入首尾一致、全部原Wait和资源尾闭合，原验收不重跑。上一批四树复用main `031b5df9`基线，组合源码`6d0a6a5c`已保存推送并完成有限验收：显式配置、Claim不可变policy绑定/00049、Execution真实锁超时temporary证明的15 top race/五包vet、compile01及native01均wholePASS。真实PG的1 top/2 sub共41.10s，20:05:04–20:07:11 UTC，原Go/driver/sup/outer均Wait0，七资源14次absence及private/runtime/desc/HOST_TCP/outer全部双尾空，1478输入首尾一致。该25路径有限包已正式交付main `fd939099`；root分支已正常merge main，integration已ff至同一main并推送，原启动、PendingVisitor及生产Loop不因配置存在而自动启用。

上一批unblock compile01原wholeFAIL保留：实际compile与list均退出0，输入及原尾一致，仅预期top顺序与真实列举相反；原列表恰为HTTP、Atomic。经独立核对后，以数量加精确集合确认原候选可复用，未重编译、未重跑Go/list；native01使用同一候选并于19:48:14–19:50:01 UTC整轮通过。原result/runner不回填，恢复recipe记录修正与复用依据。上一批retry binding的compile01为独立wholePASS，无新增FAIL；当前无Go/cache/native在途，旧Launch01 wholeFAIL及全部STOP不变。

上一批有限retry闭环已随main `6fa3e147`的24路径正式交付：组合源码`08428005`已保存推送，所有源码/00050/方法有限独审接受，11 top race/三包vet、compile01/list及native01全部wholePASS，无新增FAIL。真实PG 1 top/2 sub共34.35s（20:24:38–20:26:45 UTC），覆盖temporary→到期同key创建关联、持续temporary→耗尽沿原Work Finalize原子technical blocker；原Go/driver/sup/outer Wait全0、七资源14次absence及全部退出尾closed，原验收不重跑。

本批四树从main `6fa3e147`开始实现Project串行运行器与Work固定组snapshot/current facts读取。`Run(ctx)`连续重建快照，pending优先、todo沿真实Claim/Handoff，每次visit/skip/deferred及空轮均pacing；后三组明确Deferred，不冒完整relaunch/cooldown/blocked处理。整个Run寿命在同Authority同Project内互斥，Stop/Drain等原调用退出，不声称跨进程leader或生产app绑定。Work合同`411e3e5d`已保存推送并借入消费者，reader/runner及两测试作者已启动；本批尚未动态验证，无Go/cache/native活动。

| 执行者 | 当前工作、真实状态与下一步 |
| --- | --- |
| content / Work读取 | `/workspace/agenteam-task-transition` / `ai/work-scheduler-traversal`：新增SchedulerTaskReader合同`411e3e5d`已推送，真实同Store原Tx/fullheld的固定组身份快照与当前Task事实读取正在实现；不复用Human Actor或preclaim proof。 |
| cleanup / Scheduler | `/workspace/agenteam-skill-install-runtime-source` / `ai/scheduler-project-runner`：实现持续Run及单轮内核、同Project寿命互斥、pending精确访问和原Claim/Handoff组合；旧构造器、Unknown只Lookup及paused门保持，尚未验证。 |
| secret / 独立差额审查 | Work新合同已有限接受；后继冻结产品按实际差额独审，未参与接口设计。上一批00050等接受事实保持。 |
| work_ui / Scheduler纯测 | `/workspace/agenteam-task-human-http` / `ai/scheduler-runner-tests`：唯一新project_runner_test.go覆盖顺序、pending优先、skip/idle pacing、暂停与取消join；noCurrent/旧Sprint在纯控覆盖，不冒真实PG。 |
| skills_http / 组合测试 | `/workspace/agenteam-task-flow-delivery` / `ai/scheduler-runner-delivery`：准备真实todo排序/串行创建与association回滚pending的pause/resume/原Lookup恢复两sub；不SQL清Project指针制造noCurrent。旧新PASS/FAIL/output原位保留，当前未Go。 |
| cleanup / content | `/workspace/agenteam-skill-install` / `ai/skill-install`：Human Install/LookupInstall、普通读取与清理已随main有限交付；domain安装读取重放、原Object/Audit事实及普通清理两sub真实通过，定向pure/race/vet证据按版本复用。旧树已可逆sparse停放，原ignored材料保留；原Project/外人码/素材/Audit查询夹具FAIL保留。真实Source配置装配已进Agent创建链，AgentRun/Runtime实际调用仍未验。 |
| coordination | 维护本批模块契约、唯一写者和必要恢复摘要；正式Scheduler说明待源码冻结后按实际行为更新，不新卡/归档，不作为参与设计范围的独立验收者。 |
| 已冻结Runtime/Registry | `/workspace/agenteam-tool-operation` / `ai/tool-operation` 与 `/workspace/agenteam-tool-registry` / `ai/tool-registry`：Runtime created、canonical-v1、Registry donor及Execution取消修正的有限实现/组合纯检查保留。neutral标准Schema核心已独审并单独5top race/vet通过；AgentSystem中的真实Registry Schema适配器`d26e89eb`已推送并独审接受。统一实际MVS的`regexp2 1.12.0`后，核心5＋适配3共8top race及schema/runtime两包vet全部通过，原Wait及group/runtime双尾闭合、缓存归还；`jsonschema/v6 6.0.3`不变。旧适配01/02依赖元数据缺失导致0top、vet未跑的FAIL保留；生产绑定、真实Execution授权和AgentRun发布仍未完成。 |
| skills_http / work_ui | `/workspace/agenteam-secret-owner-ui` / `ai/secret-owner-ui`：UI04十请求均normal但测试helper解析Response的`$ref`失败，已用原10body离线修正；UI05 DELETE仍aborted，其余9请求normal且consumer全部true，整轮仍wholeFAIL，全部原尾已闭，停止第六次全PG。公共EOF后cancel竞态仅待证，client未据此修改。源记录及唯一安全first-failure JSON在`fd426`保留；main18a基线的27路径候选已保存推送`ai/secret-ui-delivery` / `2b2a0ac0`，20控制通过，仅为WIP，不入main、不冒正式浏览器接受。 |
| 已冻结提供方 | Agent/Audit/Project 四包 18 top/28 sub race+vet、Mount 修后 4 top race+vet、Model References 6 top race+vet、Secret References 4 top/17 sub race+vet、Skills 初始化 7 top/27 sub race及独立 vet 补集均按各版本保留。原 Mount fixture、Skills pure01 容量及 Directory01 失败不回填；AgentCore旧树已可逆停放，61技术路径在AgentSystem保留，其余组合源按原位置保留。初始私有witness/assignment/refs写入由既有Agent创建PG链有限验证，其他权限与执行范围不外推。 |

WIP/纯检查不等于正式 main 或 F1 接入。六个子代理席位动态共享，旧实例/进程/授权不继承。root负责Git、最终集成和共享资源窗口；执行者保持单文件唯一写者。上一Launch批次8个新top＋三包vet wholePASS，compile01 wholePASS。SchedulerLaunch PG01（17:48:24–17:52:04 UTC）业务1top2sub通过，Go/driver原Wait0，但sup/outer原Wait1：唯一HOST_TCP delta=1原tuple未保存，整轮FAIL且TCP尾门未获通过；其余七资源14次absence、private/runtime/desc双空、1411输入不变，owned进程全部退出。Busy产品`07d490b2`的pure02 19top race＋四包vet、compile及native01 1top2sub均wholePASS，全部原进程Wait0、资源与双尾闭合，无Go/cache/native在途；pure01同启动fresh 5,326,884,864B不足5GiB，0Go/0top、vet未启的容量FAIL保留，不是产品失败。旧Launch FAIL不重跑或回填。上批Start/Claim的14top/7sub＋四包vet和PG wholePASS继续有效；其pure01编译FAIL、Human PG01/02及其他旧FAIL不回填。更早Schema、21top/十包vet、各真实链与Secret UI05结论不变。coordination只处理API与唯一写者协调及本摘要，不作为参与接口决策范围的独立验收者。

## 未闭合结果与停止项

Work Planning06 仍 wholeFAIL：Doc1 第二次 Milestone GET 的原 finished 等待超时，首 list 安装门仅有限通过，九类写与最终 Go 后验未闭。Planning04/05、独立 Recovery03/04 与 Authority01 的失败不升级。planning/read/identity、blockers/layouts 及独立两门整体未完。

Model UI `41cbf90f` 的有限纯/独立结果不代 authority 验收；协调树未验源码与材料保留。普通 Variables UI `60dbcee7` 仅 CRUD03 有限通过、Authority03 整体 FAIL；Task Timeline `9ad6f1cd` Reader 有限独审，三组真实 PG 未验收。原后端 check01、check02/Audit deadline 和 inline 元数据 wrapper FAIL 仍保留；后继受影响检查按版本组合接受，不宣称原完整 check-go 单次 wholePASS。

Object Runtime join、OpenAI tools 独立动态验收、Central SPA concurrent-publication、Jina/Image 来源及其他 RootMainFeature0 残余 STOP 保持；`ready=false`/`/readyz`503 不变。完整 participant/F1、Agent 执行器、调度与团队协作端到端仍是主要缺口。生命周期提供方与阶段推进的集中缺口保留在 `docs/development/work-items/recovery-project-domain-bindings.md`，不靠空 handler 或本地 map 为空解除。

## 恢复与容量

四个物理树现从main `6fa3e147`复用为上述runner/traversal/test/delivery分支；上一`ai/scheduler-retry-delivery`已ff并推送同一main，原retry、binding、unblock及更早topic保留恢复源码。ignored PASS/FAIL、候选、输入、结果与日志仍在原位置，四个逐次launcher/app-checks仍只在旧topic保留。当前无Go/cache/native活动；这些原件仍为恢复依赖，全部保留，不扫描或删除树/cache。

当前无Go/cache writer或native在途。secret经content明确owner确认，仅退休 `/workspace/agenteam-knowledge-content-http/output/ai/knowledge-content-http/gocache`，生成缓存1,041,469,440 B，actual0/absent=true；一次实际可用6,372,360,192 B，比5GiB多1,003,651,072 B。shared hot/mod及其它cache、FAIL、source、inputs/results/logs、MinIO、refs均未动，旧Skills/WorkUI缓存保留；compile02及首次native现已wholePASS，原容量FAIL保留。HTTP `857d85`、Work `91e384`、Scheduler `e0fe`三个donor的tracked docs已获owner确认可逆停放，source、refs与原evidence保留；该事实不授权删除其它路径。

先前GH_TOKEN 401导致的push失败保留；重启后认证已恢复，root沿现有gh helper普通fetch成功，未改治理配置且无远端他方独有提交。组合`281fc3cb`与Scheduler donor `07dea4a9`已原push Wait0，最新产品代码已远端保存；本轮pending/HTTP实际结果及恢复记录由root统一保存/推送，未收到确认的提交不预填远端成功。

D05、Skills HTTP、Knowledge 正文、Secret Owner、Skills Cleanup 等旧 topic 的必要 FAIL、原始材料与固定引用继续保留；不能由局部产品已入 main 推断整个 worktree 可删。临时交付树仅由 root 逐对象核实用途、保存与依赖后清理。本轮 metadata 临时 delivery 已正常 remove/branch-d，未创建远端 delivery 分支；原 joint `01157924` 与后继 topic 保留用于 Agent 组合及失败恢复。旧Execution preparation与Task trigger两个donor工作树已由root正常移除，不再是活动物理树；远端 `ai/execution-preparation`（`d0f72300`）与 `ai/task-execution-trigger`（`5122c1dc`）保留、可复建，已确认无ignored独有材料。其后Occupancy donor `ai/task-assignment` / `c87a1019`也已正常移除物理树，确认无ignored独有材料，原pure留在AgentSystem。原Source donor物理树现复用为`ai/scheduler-project-runner`，TaskTransition donor复用为`ai/work-scheduler-traversal`；旧分支及原pure/ignored材料均保留。除下述已授权成功候选外，原FAIL与共享依赖未动。

新树使用root管理的sparse规则；已停用的SkillInstall（`7439e881`）、Skills Cleanup（`92cfb069`）和OwnerHTTP（`1bc9ead5`）三个旧树按授权可逆停放，保留恢复metadata和全部原ignored位置，tracked源码可从原分支重新铺开。AgentCore（`b4d6499`）旧树也在clean、无消费者及owner确认后可逆停放，显式保留/output、metadata与refs，61技术路径已在AgentSystem。另四个旧树的tracked生产目录及docs均已可逆sparse停放：Model `/workspace/agenteam-agent-model-prerequisites`（835a）、Secret `/workspace/agenteam-agent-secret-prerequisites`（e591）、Skills `/workspace/agenteam-agent-skill-initialization`（e9a）、Metadata `/workspace/agenteam-agent-configuration-integration`（0115）；AGENTS、.agent-state、output/ignored、FAIL与refs保留，使用源码或docs须沿原分支hydrate；四树漏继承的两组archive排除事故已纠正。经owner/root授权的旧D18、Knowledge UI、Secret HTTP私有Go cache，以及旧WorkUI私有node_modules已退休；共享依赖不动。本批成功旧SchedulerClaim候选已按授权精确退休约54.8MB，原source/PASS/inputs/log全留，新Launch FAIL候选保留。成功TaskHuman03候选已必要退休，`9aa`源码与原PASS及01/02 FAIL材料全留；此前HTTP03、AgentCreate02及schema/runtime/preparation三个成功候选的退休事实不变，原FAIL候选、源码、launcher、输入、结果和日志保留。后续运行仍须同进程fresh≥5GiB、固定Go1.27.1、只读共享模块和实际task-private telemetry off，不把旧容量快照当新授权。

现存热缓存 `/workspace/agenteam-project-variable-lifecycle/output/ai/project-variable-lifecycle/go-build` 仍由root串行分配，各树使用私有XDG/telemetry/runtime；上一批所有Go/cache/native调用和资源均已退役，本批尚未启动。SprintStart HTTP、technical-blocker单类失败及Human原子解除已有限验收并随main交付，旧矩阵未重复运行；完整Dispatcher、Task UI和生产绑定仍未完成。解除回todo不自动claim或重开failed Dispatch。显式配置、Claim持久policy、真实锁超时temporary证明及后继checkpoint/next_retry_at/原keyRetryDue/Work耗尽结算均已有限交付。当前Project串行runner正在实现，生产app、完整relaunch/cooldown仍未绑定或完成；不能用policy额度推断已创建与否或耗尽结算。完整capture仍要求所有原提供方同Tx原子参与，缺项拒绝且零input；显式false不绕过真实目录与初始化。原topic和ignored FAIL/输入/日志按恢复用途保留，本轮不新建归档。

E01 实施前须冻结游戏版本、完整内容分母、权重及关键门槛；最终由 agenteam 本身组织任务、Agent/Execution、审核和产物，以可运行游戏、真实试玩和独立验收证明至少 50% 内容覆盖。
