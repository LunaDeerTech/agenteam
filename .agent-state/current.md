# 当前执行检查点

## 目标与正式基线

持续推进 D01–D28 的产品能力和真实组合，最终完成 E01 平台内游戏复刻验收。当前平台约完成 **30%（25%–35%）**，这是按设计能力及端到端闭环权重作的工程粗估，不是逐卡等权或客观审计。E01 尚未开始，其“至少 50% 游戏内容”是独立的未来验收目标。

正式 main 已推送到 `b0df7340`，D09有限文本core及必要恢复入口均已正式交付。正式范围包括后端有限组合、连续迁移 00028→00031、Secret Owner HTTP 与既有 initialized Project 的默认根接入、Knowledge 有限只读 UI、D13 纯文本 parser 与真实正文调用、D16 本地文件读取库、D18 名称投影库、Variables 本实例生命周期停止提供方和真实单轮 accepted→stopping/claim 引擎，以及 D09 有限文本 Runtime 与显式 consumer 契约 fixture 首链。各子项不代表整张 D 卡或完整应用完成。

- Secret HTTP 的作者 PG/native/root、独立当前 Session 与安全错误补集、完整 app ordinary/race 已按相应版本接受；没有新增生产 Project 创建能力。
- Knowledge 读取 UI 最终组合 read04 wholePASS，原调用及七资源/private/runtime/desc/TCP 全尾闭合。独立三项组件风险测试使用真实 Session/Workspace 和受控网络，不冒真实 Owner 转让或 Logout PG。原 read01/read02 FAIL 保留。D13 的 1 top/3 sub 真实链已正式交付，但未接索引、其他 parser 或生产创建。
- Variables 本实例提供方与后继单轮 phase 引擎均已有限交付。新 phase02 1 top/3 sub wholePASS：真实确认提交后才调用同 Store 停止提供方，原调用及checkpoint实际返回、rollback/冻结版本拒绝和fence竞争通过，原Wait与两资源全部退出尾闭合。claim terminal仅表示本轮尝试退役；完整registry/phase loop、foreign业务join、cleanup和生产绑定仍未完成。原phase01测试hook失败及准备失败保留在topic；Unknown仍仅纯控。
- D16 只交付 Linux 本地读取库；D18 只交付 SpecRef/不可变名称表，同 ToolID 跨 revision 名称稳定，调用仍依原 Snapshot 表。真实 Registry/F1/Schema/授权、Agent/Executor 与 Runner 执行接线未因此完成。
- 默认生产 Project initializer 仍 **unbound**。旧根执行 PASS 与规范拒绝分别保留；撤回后的默认拒绝/零事实和显式 test-only 真实端口 fixture 不能证明生产创建。必填独立 `AGENTEAM_CENTRAL_KNOWLEDGE_CONFIRMATION_KEYRING` 保持。

## 四条当前产品线与执行者

| 执行者 | 当前工作、真实状态与下一步 |
| --- | --- |
| coordination | `/workspace/agenteam-project-variable-lifecycle` / `ai/project-variable-lifecycle` 的本实例停止14路径和后继phase11路径均已main有限交付。新 Batch 两源 `a16ddd5e` 已获非作者有限静审接受，7top/12sub race及vet已PASS；首次容量门FAIL、零Go事实保留。真实Batch调用尚待，不把LocalJoined或claim terminal写成整个participant stopped，不碰app绑定、Project Audit、00031或共享UI源。 |
| secret | `/workspace/agenteam-model-text-runtime` / `ai/model-text-runtime`：有限文本Runtime及恢复入口均已随main `b0df7340`正式推送。Model wire02一top两sub wholePASS，原Wait、7资源、765输入及全部双尾闭合；旧wire01 Docker路径预飞FAIL、零Go/资源事实保留。生产consumer/defaultroot仍unbound，SSE/Unknown/取消尾及完整D09未由首链接受。当前受root授权仅同步本主树current，不改topic current。 |
| content | 真实ProcessGuard隔离fixture候选编译及精确list已PASS（1top/0sub），尚未执行正文；真实7资源入口待准备。原 `/workspace/agenteam-knowledge-owner-rename` 的Rename03 current-read阶段wholeFAIL及两次更早零Go/browser预飞失败保留，原全尾已释放；旧Knowledge读取UI通过不移作rename验收。 |
| work_ui | `/workspace/agenteam-skills-owner-ui`：Skills02 wholeFAIL及原全部退出尾保持，Skills01 FAIL不升级。新四技术source `7b027dfb` 的三情形测试PASS、TypeScript检查0，当前独审中，尚未build或真实运行，UI未正式交付。旧 `/workspace/agenteam-work-ui` Planning06 wholeFAIL冻结，Recovery R13只闭自身有限门。 |
| cleanup | Model恢复9源码的精确主线增量已随main `b0df7340`正式推送，原共享入口保留；未整树带入topic未交付内容。D13已有限交付，旧独立Work Recovery04/Authority01与D05历史材料保留。 |
| skills_http | 非实现者接口/权限/方法复核；phase两个JobCause钩子窄修实际diff已接受，未冒作者动态独验。当前按root调度跟进两UI/Model等冻结片段，必要旧topic材料保留。 |

六个子代理席位动态共享，旧实例/进程/授权不继承。root负责分支、worktree、提交/推送、最终集成和实际共享资源窗口；执行者保持单文件唯一写者。Model wire02与Skills02实际窗口均已完整释放；Batch必要pure已通过，guard仅候选编译/list通过，二者均不冒后继真实调用结论。任何后继PG/native/browser/socket须root重新分配，不继承已释放窗口。

## 未闭合结果与停止项

Work Planning06 仍 wholeFAIL：Doc1 第二次 Milestone GET 的原 finished 等待超时，首 list 安装门仅有限通过，九类写与最终 Go 后验未闭。Planning04/05、独立 Recovery03/04 与 Authority01 的失败不升级。planning/read/identity、blockers/layouts 及独立两门整体未完。

Model UI `41cbf90f` 的有限纯/独立结果不代 authority 验收；协调树未验源码与材料保留。普通 Variables UI `60dbcee7` 仅 CRUD03 有限通过、Authority03 整体 FAIL；Task Timeline `9ad6f1cd` Reader 有限独审，三组真实 PG 未验收。原后端 check01、check02/Audit deadline 和 inline 元数据 wrapper FAIL 仍保留；后继受影响检查按版本组合接受，不宣称原完整 check-go 单次 wholePASS。

Object Runtime join、OpenAI tools 独立动态验收、Central SPA concurrent-publication、Jina/Image 来源及其他 RootMainFeature0 残余 STOP 保持；`ready=false`/`/readyz`503 不变。完整 participant/F1、Agent 执行器、调度与团队协作端到端仍是主要缺口。生命周期提供方与阶段推进的集中缺口保留在 `docs/development/work-items/recovery-project-domain-bindings.md`，不靠空 handler 或本地 map 为空解除。

## 恢复与容量

D05、Skills HTTP、Knowledge 正文、Secret Owner、Skills Cleanup 等旧 topic 的必要 FAIL、原始材料与固定引用继续保留；不能由局部产品已入 main 推断整个 worktree 可删。临时交付树仅由 root 逐对象核实用途、保存与依赖后清理。Variables两批及Model临时交付树均已正常清理，原功能topic与必要材料保留。

新树使用 root 管理的 sparse 规则，只不展开明确历史证据对象，生产源码、正式文档和必要 `.agent-state` 保留。仅经 owner/root 明确授权的旧 D18、Knowledge UI、Secret HTTP 可再生私有 Go cache 已退休；候选、dist、原日志/失败与共享模块未删。后续运行仍须同进程 fresh≥5GiB、固定 Go1.27.1、只读共享模块、自有 cache 和实际 task-private telemetry off，不把旧容量快照当新授权。

E01 实施前须冻结游戏版本、完整内容分母、权重及关键门槛；最终由 agenteam 本身组织任务、Agent/Execution、审核和产物，以可运行游戏、真实试玩和独立验收证明至少 50% 内容覆盖。
