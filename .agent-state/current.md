# 当前执行检查点

## 目标与正式基线

持续推进 D01–D28 的产品能力和真实组合，最终完成 E01 平台内游戏复刻验收。当前平台约完成 **30%（25%–35%）**，这是按设计能力及端到端闭环权重作的工程粗估，不是逐卡等权或客观审计。E01 尚未开始，其“至少 50% 游戏内容”是独立的未来验收目标。

正式 main `18a27db5` 已推送确认，root 协调树已正常合入；当前开发组合固定源码 `9aa572cf` 已推送，仍为 AgentSystem WIP。本批53路径有限交付 Human Skill 安装/Lookup、普通读取清理及 Owner HTTP POST/分页 catalog/旧读兼容；00032–00036 连续迁移随同交付，32–35复用独立schema02，36由真实Human安装清理链验证。HTTP03 1top2sub wholePASS与combined02 domain两sub实际PASS按版本组合接受，原两组合wholeFAIL保留。此前 metadata、后端有限组合、Secret Owner HTTP、Knowledge只读UI、D13/D16/D18有限库、Variables停止/phase/Batch/真实Guard及D09有限文本Runtime范围保留；不代表完整Agent创建、AgentRun安装、Runtime绑定、整张D卡或完整应用完成。

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
| content / Work | `/workspace/agenteam-task-transition` / `ai/task-transition`：`60ddcc7a`已推送，Human backlog→todo首片16路径已有限独审；原Work alias编译FAIL及pure02容量门0Go保留，窄修后5个新top＋3个必要兼容top与两包vet通过。00041、真实Pending gate及旧调用接线已纳组合；Agent Scheduler-current的3个top已在后继组合通过，不借Human权限证明Execution来源。真实WorkClaim、正式StartSprint和Launch仍未闭。 |
| cleanup / Scheduler | `/workspace/agenteam-skill-install-runtime-source` / `ai/task-dispatch`：`44c0b229`已推送，实际后片产品为`a6dd`；首片`0434`的真实Pending reader/组保护与00042已独审，5top/12sub＋包vet通过。后片两项must-fix已修，Coordinator 5top纳后继组合通过；不据纯检查宣称真实claim、StartSprint或Launch通过。原Runtime Source 6top通过是历史有效证据，ignored材料原位保留。 |
| secret / Execution | `/workspace/agenteam-agent-system-integration` / `ai/agent-system-integration`：`ca30f836`的同Tx原key/AgentSlot观察、Scheduler首次lookup锁与正式identity角色已独审，6个新top＋两包vet通过；后继Execution 4top也已纳组合通过。角色只选择锁，不授权；未因此获得真实Scheduler Launch或完整执行链。 |
| work_ui / Project | 同AgentSystem树：`0c81`的持久scheduler配置、真实defaults/当前gate/ServiceProjectAccess及00043已通过7top与四包vet，新增Schema用例已有限独审。新/历史enabled=false保持，Owner命令可显式enable；不外推最终产品默认选择。 |
| skills_http / 组合测试 | 同AgentSystem树：`TestTaskTransitionHuman`固定1top2sub（assignment-config-and-replay、final-transaction-rollback）。PG01 wholeFAIL保留，已修SQL43 Audit约束及fixture UUID比较；PG02 rollback子项真实PASS，整轮仍因升级helper的非法Project名称FAIL，全部原尾已闭。修后PG03已wholePASS：1top2sub共28.55s，真实覆盖prefix40正式Project旧事实→43及重复迁移保持、默认false/unlimited、Owner配置、Human backlog→todo与真实Agent指派/Get/Lookup/同key重放，以及原finalTx全部写完后的marker整体rollback；不造claim。上批Agent.Create PG02 wholePASS、Source 6top及Occupancy 5top/16sub和对应vet保留，原fixture FAIL不回填。 |
| cleanup / content | `/workspace/agenteam-skill-install` / `ai/skill-install`：Human Install/LookupInstall、普通读取与清理已随main有限交付；domain安装读取重放、原Object/Audit事实及普通清理两sub真实通过，定向pure/race/vet证据按版本复用。旧树已可逆sparse停放，原ignored材料保留；原Project/外人码/素材/Audit查询夹具FAIL保留。真实Source配置装配已进Agent创建链，AgentRun/Runtime实际调用仍未验。 |
| coordination | `/workspace/agenteam-skill-install-owner-http` / `ai/skill-install-owner-http`：HTTP03原1top2sub wholePASS，真实同Service POST/Lookup、分页catalog、旧读、当前Owner/CSRF及正式Logout后的安全401/清cookie/零新目标均闭。产品与必要恢复源已main，donor结果`1bc9ead5`已推送；适配器组合不冒全app-root/native/browser。旧树已由root停放为sparse，保metadata和所有原ignored位置；仅成功HTTP03可再生candidate已正常退休，原FAIL/launcher/输入/日志仍在，tracked代码可沿同分支恢复，本线无当前或排队源码/Go依赖。 |
| 已冻结Runtime/Registry | `/workspace/agenteam-tool-operation` / `ai/tool-operation` 与 `/workspace/agenteam-tool-registry` / `ai/tool-registry`：Runtime created、canonical-v1、Registry donor及Execution取消修正的有限实现/组合纯检查保留。neutral标准Schema核心已独审并单独5top race/vet通过；AgentSystem中的真实Registry Schema适配器`d26e89eb`已推送并独审接受。统一实际MVS的`regexp2 1.12.0`后，核心5＋适配3共8top race及schema/runtime两包vet全部通过，原Wait及group/runtime双尾闭合、缓存归还；`jsonschema/v6 6.0.3`不变。旧适配01/02依赖元数据缺失导致0top、vet未跑的FAIL保留；生产绑定、真实Execution授权和AgentRun发布仍未完成。 |
| skills_http / work_ui | `/workspace/agenteam-secret-owner-ui` / `ai/secret-owner-ui`：UI04十请求均normal但测试helper解析Response的`$ref`失败，已用原10body离线修正；UI05 DELETE仍aborted，其余9请求normal且consumer全部true，整轮仍wholeFAIL，全部原尾已闭，停止第六次全PG。公共EOF后cancel竞态仅待证，client未据此修改。源记录及唯一安全first-failure JSON在`fd426`保留；main18a基线的27路径候选已保存推送`ai/secret-ui-delivery` / `2b2a0ac0`，20控制通过，仅为WIP，不入main、不冒正式浏览器接受。 |
| 已冻结提供方 | Agent/Audit/Project 四包 18 top/28 sub race+vet、Mount 修后 4 top race+vet、Model References 6 top race+vet、Secret References 4 top/17 sub race+vet、Skills 初始化 7 top/27 sub race及独立 vet 补集均按各版本保留。原 Mount fixture、Skills pure01 容量及 Directory01 失败不回填；源码树继续保留供组合，初始私有witness/assignment/refs写入现由本批Agent创建PG链有限验证，其他权限与执行范围不外推。 |

WIP/纯检查不等于正式 main 或 F1 接入。六个子代理席位动态共享，旧实例/进程/授权不继承。root 负责 Git、最终集成和实际共享资源窗口；执行者保持单文件唯一写者。本批后继Agent 3top、Execution 4top、Coordinator 5top合计12top/42sub及三包vet已通过；表列旧有效检查按版本复用。此前37–39 schema、Secret UI05、Schema 8top/两包vet、旧21top及十包vet、preparation/Task真实PG、Source/Occupancy及Agent创建PG的原尾均已闭合；TaskHuman PG03的Go/driver/supervisor/outer均原Wait0，七资源14次absence及private/runtime/desc/TCP/outer双尾齐，adopted为空，1387输入不变；PG01/02原FAIL及原尾证据保留。当前无Go、cache writer或native在途，各模块已停写。热cache由root统一排程，不由有限PASS推断完整执行链已通。coordination只处理本批API歧义、唯一写者协调与该摘要，不作为已参与接口决策范围的独立验收者，不占Go/native窗口。

## 未闭合结果与停止项

Work Planning06 仍 wholeFAIL：Doc1 第二次 Milestone GET 的原 finished 等待超时，首 list 安装门仅有限通过，九类写与最终 Go 后验未闭。Planning04/05、独立 Recovery03/04 与 Authority01 的失败不升级。planning/read/identity、blockers/layouts 及独立两门整体未完。

Model UI `41cbf90f` 的有限纯/独立结果不代 authority 验收；协调树未验源码与材料保留。普通 Variables UI `60dbcee7` 仅 CRUD03 有限通过、Authority03 整体 FAIL；Task Timeline `9ad6f1cd` Reader 有限独审，三组真实 PG 未验收。原后端 check01、check02/Audit deadline 和 inline 元数据 wrapper FAIL 仍保留；后继受影响检查按版本组合接受，不宣称原完整 check-go 单次 wholePASS。

Object Runtime join、OpenAI tools 独立动态验收、Central SPA concurrent-publication、Jina/Image 来源及其他 RootMainFeature0 残余 STOP 保持；`ready=false`/`/readyz`503 不变。完整 participant/F1、Agent 执行器、调度与团队协作端到端仍是主要缺口。生命周期提供方与阶段推进的集中缺口保留在 `docs/development/work-items/recovery-project-domain-bindings.md`，不靠空 handler 或本地 map 为空解除。

## 恢复与容量

D05、Skills HTTP、Knowledge 正文、Secret Owner、Skills Cleanup 等旧 topic 的必要 FAIL、原始材料与固定引用继续保留；不能由局部产品已入 main 推断整个 worktree 可删。临时交付树仅由 root 逐对象核实用途、保存与依赖后清理。本轮 metadata 临时 delivery 已正常 remove/branch-d，未创建远端 delivery 分支；原 joint `01157924` 与后继 topic 保留用于 Agent 组合及失败恢复。旧Execution preparation与Task trigger两个donor工作树已由root正常移除，不再是活动物理树；远端 `ai/execution-preparation`（`d0f72300`）与 `ai/task-execution-trigger`（`5122c1dc`）保留、可复建，已确认无ignored独有材料。其后Occupancy donor `ai/task-assignment` / `c87a1019`也已正常移除物理树，确认无ignored独有材料，原pure留在AgentSystem。原Source donor物理树现复用为`ai/task-dispatch`，其原pure/ignored材料保留；新TaskTransition是本批唯一新增sparse树。除下述已授权成功候选外，原FAIL与共享依赖未动。

新树使用root管理的sparse规则；已停用的SkillInstall（`7439e881`）、Skills Cleanup（`92cfb069`）和OwnerHTTP（`1bc9ead5`）三个旧树另按授权可逆停放，保留恢复metadata和全部原ignored位置，tracked源码可从原分支重新铺开。经owner/root授权的旧D18、Knowledge UI、Secret HTTP私有Go cache，以及旧WorkUI私有node_modules已退休；共享依赖不动。HTTP03、AgentCreate02及更早schema/runtime/preparation三项成功测试候选已必要退休，原FAIL候选、源码、launcher、输入、结果和日志保留。后续运行仍须同进程fresh≥5GiB、固定Go1.27.1、只读共享模块和实际task-private telemetry off，不把旧容量快照当新授权。

现存热缓存 `/workspace/agenteam-project-variable-lifecycle/output/ai/project-variable-lifecycle/go-build` 由root串行分配；不并写、不复制数GB，各树使用私有XDG/telemetry/runtime。Human安装/清理与Owner HTTP已正式交付；Runtime Registry Schema适配已获8top及两包vet有限验证，本批preparation迁移/Project gate及真实Task Owner读取已验证，下一仍须接真实启动来源。install-source/Backend构造解耦与真实SkillService、adapter、Scope/Risk、BuiltinSource配置装配已在Agent创建链通过；它不证明Execution调用proof或ToolExecution已通。Human Task PG03已完成修后升级/配置/指派/重放与finalTx rollback的整轮真实验证，41–43按此有限组合接受，原PG01/02整轮FAIL不回填。后续先接正式StartSprint与WorkClaim/pending原子写入，再接真实Task Launch provider；这些链及Task HTTP/UI仍未完成。完整capture须所有原提供方同Tx原子参与，缺项拒绝且零input，不持久化部分Snapshot。显式false不能绕过真实目录与初始化提供方。Skill HTTP产品已main但ignored原结果/launcher/输入清单和原PG日志尚未全部归档，各源topic继续保留；本轮不改正式tasks、不新建归档。

E01 实施前须冻结游戏版本、完整内容分母、权重及关键门槛；最终由 agenteam 本身组织任务、Agent/Execution、审核和产物，以可运行游戏、真实试玩和独立验收证明至少 50% 内容覆盖。
