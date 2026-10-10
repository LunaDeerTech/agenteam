# 当前执行检查点

## 目标与正式基线

持续推进 D01–D28 的产品能力和真实组合，最终完成 E01 平台内游戏复刻验收。当前平台约完成 **30%（25%–35%）**，这是按设计能力及端到端闭环权重作的工程粗估，不是逐卡等权或客观审计。E01 尚未开始，其“至少 50% 游戏内容”是独立的未来验收目标。

正式 main `18a27db5` 已推送确认，root 协调树已正常合入；Work Launch donor `341dcde7` 与组合 `d3cfaeb1` 已推送；最新组合 `281fc3cb`（含fixture/selector `de37f7cb`）与Scheduler donor `07dea4a9` 已推送确认，仍为 AgentSystem WIP。main的53路径有限交付 Human Skill 安装/Lookup、普通读取清理及 Owner HTTP POST/分页 catalog/旧读兼容；00032–00036 连续迁移随同交付，32–35复用独立schema02，36由真实Human安装清理链验证。HTTP03 1top2sub wholePASS与combined02 domain两sub实际PASS按版本组合接受，原两组合wholeFAIL保留。此前 metadata、后端有限组合、Secret Owner HTTP、Knowledge只读UI、D13/D16/D18有限库、Variables停止/phase/Batch/真实Guard及D09有限文本Runtime范围保留；不代表完整Agent创建、AgentRun安装、Runtime绑定、整张D卡或完整应用完成。

- Secret HTTP 的作者 PG/native/root、独立当前 Session 与安全错误补集、完整 app ordinary/race 已按相应版本接受；没有新增生产 Project 创建能力。
- Knowledge 读取 UI 最终组合 read04 wholePASS，原调用及七资源/private/runtime/desc/TCP 全尾闭合。独立三项组件风险测试使用真实 Session/Workspace 和受控网络，不冒真实 Owner 转让或 Logout PG。原 read01/read02 FAIL 保留。D13 的 1 top/3 sub 真实链已正式交付，但未接索引、其他 parser 或生产创建。
- Variables 本实例提供方与后继单轮 phase 引擎均已有限交付。新 phase02 1 top/3 sub wholePASS：真实确认提交后才调用同 Store 停止提供方，原调用及checkpoint实际返回、rollback/冻结版本拒绝和fence竞争通过，原Wait与两资源全部退出尾闭合。claim terminal仅表示本轮尝试退役；完整registry/phase loop、foreign业务join、cleanup和生产绑定仍未完成。原phase01测试hook失败及准备失败保留在topic；Unknown仍仅纯控。
- 有界 Batch 的 7 top/12 sub race 与 vet 通过；Guard01 真实扫描、活进程 Busy 后项推进、原 child SIGKILL/Wait 与 stdout join 后的死亡证明及 fence 重验 wholePASS，七资源14次 absence、704输入一致和全部原退出尾齐。test-only 组合不证明 foreign 业务调用已 join，不恢复 Object Runtime join 或完整 participant 停止。
- D16 只交付 Linux 本地读取库；D18 只交付 SpecRef/不可变名称表，同 ToolID 跨 revision 名称稳定，调用仍依原 Snapshot 表。真实 Registry/F1/Schema/授权、Agent/Executor 与 Runner 执行接线未因此完成。
- 默认生产 Project initializer 仍 **unbound**。旧根执行 PASS 与规范拒绝分别保留；撤回后的默认拒绝/零事实和显式 test-only 真实端口 fixture 不能证明生产创建。必填独立 `AGENTEAM_CENTRAL_KNOWLEDGE_CONFIRMATION_KEYRING` 保持。
- metadata04 真实 Account/Project/Model/Secret 同 Store 原 Tx 的正常读取、stale、当前 Session 拒绝及 rollback 已 wholePASS，原资源和退出尾闭合。此链不含 Agent canonical 创建、外域 References、Skills assignment 或 Registry install 成功。

## 当前并行产品线与执行者

| 执行者 | 当前工作、真实状态与下一步 |
| --- | --- |
| content / Work | `/workspace/agenteam-task-transition` / `ai/task-transition`：Task Launch provider 3源`341dcde7`及组合`d3cfaeb1`已推送，work_ui有限独审接受。真实Scheduler私有handoff、原Work claim与当前Task来源校验已接原Execution Launch；本批业务子项验证created/slot及原key重放，整轮结果见下。此前WorkClaim/Start原子pending与Human真实链、对应pure和旧FAIL按版本保留。下一补正式Work AgentBusy补偿，不扩Task HTTP/UI。 |
| cleanup / Scheduler | `/workspace/agenteam-skill-install-runtime-source` / `ai/task-dispatch`：LaunchHandoff 5path donor `07dea4a9`已推送确认，work_ui有限独审接受。发送标记确认提交后一次原Launch，关联失败沿原key只读Lookup恢复；明确未创建保持known_not_created/pending并返回原错，无自动retry或伪skipped/failed。下一接Work AgentBusy正式补偿；当前不是完整Dispatcher、遍历或恢复循环。原Pending/Coordinator及Runtime Source证据、ignored材料保留。 |
| secret / Execution | `/workspace/agenteam-agent-system-integration` / `ai/agent-system-integration`：复用既有真实Launch/Lookup、Agent私有witness及原key/slot/capacity观察，无第二套Execution接口或新迁移；Scheduler首次lookup的Schedule锁与原Project门保持。本批真实功能止于created、slot及可靠关联，不进入完整preparing/Snapshot/Model运行或生产绑定。旧`ca30f836`独审、6新top/两包vet与后继Execution 4top有效。 |
| work_ui / Project / Sprint | 同AgentSystem树：StartSprint 9源`e6e7`已有限独审，00044及真实Work/Project同Tx Sprint状态与当前指针接线已在组合PG通过。Start/Claim批次Project 4top/7sub与对应vet通过；原`0c81`配置、defaults/当前gate/ServiceProjectAccess及00043的7top/四包vet和Human PG03升级证据保留。新/历史enabled=false保持，Owner命令可显式enable；不外推最终产品默认选择。 |
| skills_http / 组合测试 | 同AgentSystem树：fixture/3selector `de37f7cb`已随组合`281fc3cb`推送，work_ui有限独审接受；compile01 wholePASS。SchedulerLaunch PG01的1top2sub、24.45s业务PASS，但HOST_TCP delta=1导致wholeFAIL；原tuple未保存，不能归因或回填通过。真实子项覆盖created＋slot/原key重放、关联事务回滚后Lookup恢复，不冒物理CommitUnknown全矩阵。上批SchedulerClaim PG01、Human PG03、Agent.Create PG02及各原FAIL均按版本保留。 |
| cleanup / content | `/workspace/agenteam-skill-install` / `ai/skill-install`：Human Install/LookupInstall、普通读取与清理已随main有限交付；domain安装读取重放、原Object/Audit事实及普通清理两sub真实通过，定向pure/race/vet证据按版本复用。旧树已可逆sparse停放，原ignored材料保留；原Project/外人码/素材/Audit查询夹具FAIL保留。真实Source配置装配已进Agent创建链，AgentRun/Runtime实际调用仍未验。 |
| coordination | `/workspace/agenteam-skill-install-owner-http` / `ai/skill-install-owner-http`：HTTP03原1top2sub wholePASS，真实同Service POST/Lookup、分页catalog、旧读、当前Owner/CSRF及正式Logout后的安全401/清cookie/零新目标均闭。产品与必要恢复源已main，donor结果`1bc9ead5`已推送；适配器组合不冒全app-root/native/browser。旧树已由root停放为sparse，保metadata和所有原ignored位置；仅成功HTTP03可再生candidate已正常退休，原FAIL/launcher/输入/日志仍在，tracked代码可沿同分支恢复，本线无当前或排队源码/Go依赖。 |
| 已冻结Runtime/Registry | `/workspace/agenteam-tool-operation` / `ai/tool-operation` 与 `/workspace/agenteam-tool-registry` / `ai/tool-registry`：Runtime created、canonical-v1、Registry donor及Execution取消修正的有限实现/组合纯检查保留。neutral标准Schema核心已独审并单独5top race/vet通过；AgentSystem中的真实Registry Schema适配器`d26e89eb`已推送并独审接受。统一实际MVS的`regexp2 1.12.0`后，核心5＋适配3共8top race及schema/runtime两包vet全部通过，原Wait及group/runtime双尾闭合、缓存归还；`jsonschema/v6 6.0.3`不变。旧适配01/02依赖元数据缺失导致0top、vet未跑的FAIL保留；生产绑定、真实Execution授权和AgentRun发布仍未完成。 |
| skills_http / work_ui | `/workspace/agenteam-secret-owner-ui` / `ai/secret-owner-ui`：UI04十请求均normal但测试helper解析Response的`$ref`失败，已用原10body离线修正；UI05 DELETE仍aborted，其余9请求normal且consumer全部true，整轮仍wholeFAIL，全部原尾已闭，停止第六次全PG。公共EOF后cancel竞态仅待证，client未据此修改。源记录及唯一安全first-failure JSON在`fd426`保留；main18a基线的27路径候选已保存推送`ai/secret-ui-delivery` / `2b2a0ac0`，20控制通过，仅为WIP，不入main、不冒正式浏览器接受。 |
| 已冻结提供方 | Agent/Audit/Project 四包 18 top/28 sub race+vet、Mount 修后 4 top race+vet、Model References 6 top race+vet、Secret References 4 top/17 sub race+vet、Skills 初始化 7 top/27 sub race及独立 vet 补集均按各版本保留。原 Mount fixture、Skills pure01 容量及 Directory01 失败不回填；AgentCore旧树已可逆停放，61技术路径在AgentSystem保留，其余组合源按原位置保留。初始私有witness/assignment/refs写入由既有Agent创建PG链有限验证，其他权限与执行范围不外推。 |

WIP/纯检查不等于正式 main 或 F1 接入。六个子代理席位动态共享，旧实例/进程/授权不继承。root负责Git、最终集成和共享资源窗口；执行者保持单文件唯一写者。本批8个新top＋三包vet wholePASS，compile01 wholePASS。SchedulerLaunch PG01（17:48:24–17:52:04 UTC）业务1top2sub通过，Go/driver原Wait0，但sup/outer原Wait1：唯一HOST_TCP delta=1原tuple未保存，整轮FAIL且TCP尾门未获通过；其余七资源14次absence、private/runtime/desc双空、1411输入不变，owned进程全部退出。当前无Go、cache writer或native在途，不追加测试。上批Start/Claim的14top/7sub＋四包vet和PG wholePASS继续有效；其pure01编译FAIL、Human PG01/02及其他旧FAIL不回填。更早Schema、21top/十包vet、各真实链与Secret UI05结论不变。coordination只处理API与唯一写者协调及本摘要，不作为参与接口决策范围的独立验收者。

## 未闭合结果与停止项

Work Planning06 仍 wholeFAIL：Doc1 第二次 Milestone GET 的原 finished 等待超时，首 list 安装门仅有限通过，九类写与最终 Go 后验未闭。Planning04/05、独立 Recovery03/04 与 Authority01 的失败不升级。planning/read/identity、blockers/layouts 及独立两门整体未完。

Model UI `41cbf90f` 的有限纯/独立结果不代 authority 验收；协调树未验源码与材料保留。普通 Variables UI `60dbcee7` 仅 CRUD03 有限通过、Authority03 整体 FAIL；Task Timeline `9ad6f1cd` Reader 有限独审，三组真实 PG 未验收。原后端 check01、check02/Audit deadline 和 inline 元数据 wrapper FAIL 仍保留；后继受影响检查按版本组合接受，不宣称原完整 check-go 单次 wholePASS。

Object Runtime join、OpenAI tools 独立动态验收、Central SPA concurrent-publication、Jina/Image 来源及其他 RootMainFeature0 残余 STOP 保持；`ready=false`/`/readyz`503 不变。完整 participant/F1、Agent 执行器、调度与团队协作端到端仍是主要缺口。生命周期提供方与阶段推进的集中缺口保留在 `docs/development/work-items/recovery-project-domain-bindings.md`，不靠空 handler 或本地 map 为空解除。

## 恢复与容量

先前GH_TOKEN 401导致的push失败保留；重启后认证已恢复，root沿现有gh helper普通fetch成功，未改治理配置且无远端他方独有提交。组合`281fc3cb`与Scheduler donor `07dea4a9`已原push Wait0，最新产品代码已远端保存；三个恢复runner及本轮current材料仍待root本次保存，不预填其提交身份。

D05、Skills HTTP、Knowledge 正文、Secret Owner、Skills Cleanup 等旧 topic 的必要 FAIL、原始材料与固定引用继续保留；不能由局部产品已入 main 推断整个 worktree 可删。临时交付树仅由 root 逐对象核实用途、保存与依赖后清理。本轮 metadata 临时 delivery 已正常 remove/branch-d，未创建远端 delivery 分支；原 joint `01157924` 与后继 topic 保留用于 Agent 组合及失败恢复。旧Execution preparation与Task trigger两个donor工作树已由root正常移除，不再是活动物理树；远端 `ai/execution-preparation`（`d0f72300`）与 `ai/task-execution-trigger`（`5122c1dc`）保留、可复建，已确认无ignored独有材料。其后Occupancy donor `ai/task-assignment` / `c87a1019`也已正常移除物理树，确认无ignored独有材料，原pure留在AgentSystem。原Source donor物理树现复用为`ai/task-dispatch`，其原pure/ignored材料保留；TaskTransition donor继续保留供后继Work补偿。除下述已授权成功候选外，原FAIL与共享依赖未动。

新树使用root管理的sparse规则；已停用的SkillInstall（`7439e881`）、Skills Cleanup（`92cfb069`）和OwnerHTTP（`1bc9ead5`）三个旧树按授权可逆停放，保留恢复metadata和全部原ignored位置，tracked源码可从原分支重新铺开。AgentCore（`b4d6499`）旧树也在clean、无消费者及owner确认后可逆停放，显式保留/output、metadata与refs，61技术路径已在AgentSystem。经owner/root授权的旧D18、Knowledge UI、Secret HTTP私有Go cache，以及旧WorkUI私有node_modules已退休；共享依赖不动。本批成功旧SchedulerClaim候选已按授权精确退休约54.8MB，原source/PASS/inputs/log全留，新Launch FAIL候选保留。成功TaskHuman03候选已必要退休，`9aa`源码与原PASS及01/02 FAIL材料全留；此前HTTP03、AgentCreate02及schema/runtime/preparation三个成功候选的退休事实不变，原FAIL候选、源码、launcher、输入、结果和日志保留。后续运行仍须同进程fresh≥5GiB、固定Go1.27.1、只读共享模块和实际task-private telemetry off，不把旧容量快照当新授权。

现存热缓存 `/workspace/agenteam-project-variable-lifecycle/output/ai/project-variable-lifecycle/go-build` 由root串行分配；不并写、不复制数GB，各树使用私有XDG/telemetry/runtime。Human安装/清理与Owner HTTP已正式交付；Runtime Registry Schema适配、preparation迁移/Project gate及真实Task Owner读取按既有有限范围验证。install-source/Backend构造解耦与真实SkillService、adapter、Scope/Risk、BuiltinSource配置装配已在Agent创建链通过；它不证明Execution调用proof或ToolExecution已通。Human Task PG03及本批StartSprint/WorkClaim原子pending、重放与finalTx rollback真实链已分别wholePASS，41–45按各组合范围接受，原FAIL不回填。本批Task Launch与Scheduler交接的业务子项通过，但TCP收尾未闭全门、整轮FAIL保留。下一实际开发为正式Work AgentBusy补偿与Scheduler衔接；自动retry、完整Dispatcher/Loop、Task HTTP/UI及生产绑定仍未完成。完整capture须所有原提供方同Tx原子参与，缺项拒绝且零input，不持久化部分Snapshot。显式false不能绕过真实目录与初始化提供方。Skill HTTP产品已main但ignored原结果/launcher/输入清单和原PG日志尚未全部归档，各源topic继续保留；本轮不改正式tasks、不新建归档。

E01 实施前须冻结游戏版本、完整内容分母、权重及关键门槛；最终由 agenteam 本身组织任务、Agent/Execution、审核和产物，以可运行游戏、真实试玩和独立验收证明至少 50% 内容覆盖。
