# Agent F1 前置关系与并行接口建议

## 当前批次与活动树

正式产品交付基线为已推送main `4270a6702456051ede025c30912b56e9da42d912`，本次交付13/15页面及文档，承接`295f46b4`已交的12A/B和14/16backend；第十一批`f4190ad7`保留为历史基线。A06创建/独立Ready与Unknown原Lookup两链wholePASS，业务及15PNG有限视觉终核接受；14 Agent Create公共App TLS两链wholePASS并最终接受。B05真实两sub25.84s wholePASS、全部尾闭合，独立最终有限验收已接受；B01业务接受仍撤回，B02–04 FAIL保留。第十三批Task编辑的native02（SOURCE `8417`/candidate09）81.74s wholePASS，第十五批Agent创建页面的native03（runtime SOURCE `7a0224a9`，复用compile `8417`的candidate09及原Agent dist）41.82s wholePASS；两片业务、原资源尾与有限视觉独审均已接受，验收闭合，已随main `4270a670`交付。13原native01 FAIL及15原native01容量FAIL、native02 Lookup绑定FAIL保留，不回填。第十六批Update HTTP/schema仍仅十九race与两包vet分段接受，无真实Update PG；第十七批SOURCE `3c17d0a6`前端20项、format10/type/build实际及独立接受，仍无真实Update/App/browser，未纳入本次13/15组合。 约30%、E01未开始、ready=false及原STOP不变。

第十二批B沿SOURCE `deec8a506b98a77b120446e7e4360740557ac861`/candidate07完成B05真实 `TestAppExecutionRuntime`，两sub25.84s：已有initialized Project自动发现并经真实Model到Execution终态12.34s，held-wire下Stop等待原调用及双lease退休13.50s。两个原SystemResolver A/AAAA前验均通过，DNS私有run PID与实际Go Wait身份一致；四原Wait0、七ID14次absence、全部双尾/TCP0、1665输入同，整轮wholePASS。secret独立最终原件有限验收已接受。原定向16 top/三包vet及compile07通过事实保持；本轮仅接受显式direct-text-v1且已初始化Project的有限链，不开放Project initializer、全局ready或完整Loop。 B01原29.39s PASS因afterOwners写入struct副本、目标app.Run未执行而撤回业务接受，只保留实际setup/cleanup和资源尾；原结果不修改。B02观察deadline/必达完成、B03失败退休、B04已到达DNS/wire但两处uuid/text后验SQL错误的wholeFAIL，以及compile05等失败原件均保留，不以后继通过回填。B04两行fixture修复不改产品Resolver或Runtime授权。 A06沿SOURCE `75e23e9d`/candidate06运行 `TestTaskIntakeWeb`，两sub37.63s（18.85/18.78）wholePASS；四Wait0、两Node成功、七ID14次absence、全部双尾/TCP0、2426输入同。正常创建后独立Ready，以及丢创建回执后原Lookup再Ready均通过，原A01–05 FAIL保留。正式范围见[Task intake](../../docs/development/work-items/d11-work-owner-planning-ui.md#12-current-sprint-任务创建与指派切片批-12a)与[Runtime装配](../../docs/development/work-items/recovery-project-domain-bindings.md#有限后继显式配置的-central-execution-runtime-装配)。

第十三批 `ai/task-edit-ui` @`bfe52d55`的独立前端24项、format10/type/build接受事实保持。SOURCE `8417da7a0d6de6e7813092b106797ba3491e66df`/candidate09的 `TestTaskEditWeb` native02两sub81.74s wholePASS：真实编辑与显式清空、No-op及独立Ready，以及丢编辑回执后的原完整patch/key Lookup均闭合。四Wait0、两Node实际成功、七ID14次absence、全部双尾/TCP0及2436输入一致，secret最终业务/资源有限接受；cleanup接受8PNG及同轮键盘回焦、主题、reduced-motion和overflow检查，截图下半部与Unknown提示仅有DOM断言支持，不外推完整视觉或全面无障碍审计。原native01整轮34.22s FAIL保留，后继只修原请求完成联证，不回填旧结果。

第十五批 `ai/agent-configuration-ui`原SOURCE `6ef52a5a`前端20项、format15/type/build通过。runtime SOURCE `7a0224a98c724ade5bab956d3f5e2a1d57948353`的 `TestAgentConfigurationWeb` native03两sub41.82s wholePASS：`default-create-config-and-directory`21.37s，`lost-create-confirmation-only-lookup`20.45s。后者真实Create仅一次、回执截断保留Unknown且仅查询原Lookup一次；完整配置二十字段与安全目录九字段经原严格接口验证。四Wait0、两Node实际成功、七ID14次absence、所有private/runtime/desc/TCP双尾空、outer无遗留、2436输入同，secret最终原件有限接受。cleanup接受本轮7PNG与键盘/主题/overflow有限检查；二十字段未同时出现在同一截图，不外推完整视觉或无障碍审计。共享candidate09仍由 `8417da7a0d6de6e7813092b106797ba3491e66df`编译，原Agent dist01复用；15的runtime修复只涉及夹具对原Agent进度的绑定谓词，不将不同SOURCE混为一次编译。原native01容量FAIL及native02整轮28.66s FAIL（normal子场景通过、lost在Lookup绑定失败）原样保留。13/15验收闭合，已随main `4270a670`交付；14既有App创建链保持接受，16真实Update PG和17 Update页面不由本轮代验。

Timeline后端SOURCE `5335d5b06128069913fe3b6555e619550c0b0ce4`已按原有效输入分段接受14项与3包vet；combined-pure-01断言FAIL、02物理缺包零top FAIL保留。UI `ai/task-timeline-current-ui` @`b1fdc7d5`十路径静审及format10通过，frontend01新增7项加直接受影响page9项共16个不同用例、完整type/build全部PASS、原尾闭合，cleanup已独立接受。新真实页面exact 1top/1sub及methods八路径静审接受；作者 `ai/task-timeline-current-web` @`c44e173df64e3202cc33f4ffcfa88a821ef1cf7f`已保存推送。format01因五个授权文件物理缺失而FAIL、零源码修改，root按同HEAD恢复精确文件后format02 Go1/JS4实际Wait0且全尾闭合；原FAIL保留。metadata组合 `ai/task-timeline-current-delivery` @`afe23feefde20dff4ce1254941cfd4b963628e2e`已合入已验13/15与Timeline backend/UI/Web，正常push已成功；尚待新controls、三top真实PG与一top浏览器联调，不能据组合或格式通过宣称Timeline已验收或交付。本次正式交付范围仅13/15，17仍独立。 原件与既有恢复入口见[组合recipe](../agent-system-integration/README.md#task编辑与agent创建页面有限组合)，当前分工见[执行者](../current.md#当前执行者与保留树)。

14/16原组合SOURCE `548c9959`的十九race top与combined-vet-02两包vet已按原输入分段接受；pure01因容量门未启vet的wholeFAIL、后继DNS entry容量FAIL/0Go原件保留，不由后继成功回填。DNS方法后已在B04真实消费，B04两处后验SQL类型错误修复不改产品Resolver；B05验收来源`deec8a506b98a77b120446e7e4360740557ac861`已随产品基线main `295f46b4`交付，compile07与B05实际wholePASS，secret独立最终原件有限验收已接受。14实际创建验收及A06原件入口见[组合recipe](../agent-system-integration/README.md#task-intake与agent配置创建有限组合)。

第十一A最终SOURCE `53bd25cb80b2b890edb4df14452fede05edeb5e1`：native06真实1top2sub86.97s，review/done、返工回执截断后的原Lookup和Logout/Session撤销均通过；四原Wait0、两Node实际Wait成功、七ID14次absence、全部双尾/TCP0/adopted[]、2404输入一致，secret有限终核接受。前端06的31控、07的Session20＋WorkReviewSession4、08的body9＋精确旧5及对应format/type/build分别有效，不将31/24/14累加唯一数；auth62/state3按未变输入复用。最终dist08，compile03的898 Go/embed记录与candidate03复用。

恢复来源：UI `/workspace/agenteam-work-ui` / `ai/work-review-ui` @`b15c423f`，Web `/workspace/agenteam-work-ui-independent` / `ai/work-review-web` @`f596c326`均已推送；第十一delivery来源 `ai/work-review-runtime-delivery`与该main同为`f4190ad7`，当前活动分支见上文。原native01–05、compile01、前端FAIL、candidate02/03及旧dist全部保留，不改旧结论；不因局部交付删除原refs、树或唯一原件。

原件与命令见[既有组合recipe](../agent-system-integration/README.md#work-评审-ui真实链已通过)；最终为delivery的`output/ai/work-review-web/frontend-08/result.json`、`output/ai/agent-system-integration/task-review-web-06-control/{result.json,supervisor.log}`及PG `/tmp/trw06/ui-008f2459cc554212.log`。第十一批接受范围限评审UI与B单Project audit包装；第十二批App Runtime有限结果见本页当前结论。生产initializer、全局ready、完整Work Planning与既有STOP仍未完成，约30%粗估不变。

第十一批资源窗口已全部归还；本轮A native02 runtime目录非空仍保留原FAIL，A06与14实际原Wait及全部资源双尾均已闭合。B04及其此前FAIL不回填，B05也已四Wait0、全部双尾闭合，旧尾状态不授权新运行。既有同锁Web/PW1.56.1/go-captcha-vue仅复用原依赖与软链；固定Node v24.19.0/批准Chromium、原private环境、实际进程fresh≥5GiB门保持。原树的ordinary sparse停放、protected/ignored/FAIL保留与恢复流程见[当前检查点](../current.md#恢复与容量)。

第十二容量只读规划为 available 5,608,574,976 B，距额外500 MB缓冲尚260,134,144 B；指定两旧树已park，不扩大扫描。root已仅精准退休第十一B全PASS可再生bin `execution-runtime-access-audit-race-01.test`（`ba0da8`，66,467,736 B/allocated66,469,888 B）；原source/recipe/results/inputs/PG与A曾FAIL的candidate02/03全留。该数值不是启动证明，每实际Go/native仍须同进程fresh≥5GiB。

本轮按root显式授权将停用FAIL候选01/02无损压缩到 `/workspace/agenteam-task-flow-delivery/output/ai/agent-system-integration/task-intake-runtime-race-{01,02}.test.gz`（33,310,370 / 33,312,738 B）；exclusive创建、原payload与解压SHA/size复核、file/dir fsync并确认零consumer/原inode后才移除raw副本，actual退出0、两raw absent，净allocated回收88,141,824 B，所有原results/log/source/input/runtime失败残留原样；恢复须先独占资源并满足容量，将 `gzip -dc -- /workspace/agenteam-task-flow-delivery/output/ai/agent-system-integration/task-intake-runtime-race-01.test.gz`（02同理）的输出写入exclusive `.restore` 临时文件，按原 `task-intake-runtime-compile-{01,02}/result.json` 的 `candidate.bytes/sha256/path` 校验、设执行权限和fsync后，以不覆盖既有路径的link发布原raw并fsync目录，校验失败不发布，始终保留gzip，这不改变原FAIL结论。旧第十一候选`task-review-web-race-{02,03}.test.gz`亦按同一无损流程保存于上述目录（30,056,226 / 30,066,075 B），raw移除前核原解压bytes/SHA、fsync及零consumer，actual退出0、净allocated回收79,130,624 B；恢复复用上述exclusive流程，仅family改为`task-review-web-compile-{02,03}/result.json`，保留gzip及全部原证据，A04尾闭后，第十二候选03也依同一verified/fsync/零consumer流程保存于上述目录`task-intake-runtime-race-03.test.gz`（33,314,307 B），还原77,378,315 B且SHA同原compile03记录，actual退出0后仅移除raw；恢复同上述流程读取`task-intake-runtime-compile-03/result.json`，全部原FAIL原件/runtime保留。 本轮同目录再保存`task-intake-runtime-race-04.test.gz`（33,325,890 B，还原77,406,603 B）和`scheduler-review-dispatch-race-02.test.gz`（28,432,532 B，还原65,919,007 B）；actual `4ff4f3`退出0，按原payload/解压SHA及size、fsync、零consumer核验后仅移除raw，净释放81,575,936 B。恢复分别读取原`task-intake-runtime-compile-04/result.json`与`scheduler-review-dispatch-compile-02/result.json`，复用上述exclusive恢复规程；gzip、原FAIL内容/日志/输入均保留。 root随后对旧SecretVariableOwnerService（`f9cc`）及KnowledgeContentHTTP（`9b9`）作普通sparse九目录停放，原archive排除规则保留，actual `bf4ac3`/`b3331f`均退出0；后验HEAD=origin/clean未变、九目录absent，output/.agent-state/.codex/.agents/refs与全部FAIL/ignored仍在。 root后继同规程停放SecretOwnerHTTP（`8ef`）与KnowledgeOwnerRename（`40e`），actual `404b98`/`aeed92`均退出0；HEAD=origin/clean及protected/output/refs不变，前者九目录absent，后者tests/web仅原ignored依赖保留、其余目录absent，不删除任何FAIL。

本轮四个停用FAIL候选已按原payload与解压SHA/bytes双核、fsync和零consumer流程无损保存为`.gz`，actual `2aefa3`净回收148,422,656 B；仅raw退休，原FAIL/source/log不变。四个精确basename、原compile映射及恢复规程见[当前恢复与容量](../current.md#恢复与容量)，不另复制历史结果。

## 前置阶段记录（历史）

以下保留早期metadata交付后的前置勘察与检查边界；其“当前缺口”“下一口”及迁移未验证描述对应当时输入，后继有限交付以根current为准，不回填原FAIL或解除STOP。默认生产initializer、完整participant/F1仍不因本批有限单轮交付而获得接受。

本简报区分已实现、纯检查与真实组合。Model Selection/Secret Directory metadata 同事务首链已 wholePASS 并正式进入 main `7a693cb6`；原联合 topic 保留供后继 Agent 组合/失败恢复，临时 delivery 已正常清理。Agent、References、Skills 初始化与 Mount 的必要纯检查分版本通过，00032–00036 连续迁移及 canonical Agent 创建尚无真实组合证据。默认生产 Project initializer、完整 participant 与 F1 保持未绑定。

## 正式来源与实际缺口

按 [Agent F1 §2/§6/§7](../../docs/development/work-items/d10-agent-configuration.md)、[D01 Skills](../../docs/development/work-items/d01-contracts/resources-skills.md)、[D01 Tool](../../docs/development/work-items/d01-contracts/model-tool.md)及 [Registry](../../docs/architecture/tool-system/tool-definition-registry.md)推进，不能以本简报改写其门槛。

| 提供方 | 当时事实 | 当时作者与最小下一口 |
| --- | --- | --- |
| Model | Selection 已有限正式交付；双角色 References 已实现，6 新 top race 与两包 vet wholePASS | work_ui，`/workspace/agenteam-agent-model-prerequisites`：下一接真实 Agent 私有 writer，不以 metadata 首链代 References SQL 验证 |
| Secret | [Directory/References 合同](../../internal/central/projectvariable/contract/secret_directory.go) 的 Directory 已有限正式交付；References 4 top/17 sub race 与两包 vet wholePASS | secret，References 两源位于 `/workspace/agenteam-agent-model-prerequisites/internal/central/projectvariable/`；原 Directory 树保留历史失败，不误作当前 References 源 |
| Skills | 新初始化器、assignment/head/00034 和 Cleanup 前置保护已实现；7 top/27 sub race PASS，加独立两包 vet PASS | coordination，`/workspace/agenteam-agent-skill-initialization`；pure-01 vet 前容量失败仍为 wholeFAIL，后继真实 Agent/SQL 组合未验，不实现运行中 Assign/Remove |
| Tool | [SpecRef](../../internal/central/tool/contract/spec_ref.go) 与 [NameTable](../../internal/central/tool/projection/name_table.go) 保持；Registry 身份/spec/配置目录/refs 与00033已有候选 | cleanup，`/workspace/agenteam-tool-registry`：尚缺真实 install-skill Backend binding，不能以空目录或测试 source 通过默认创建 |
| Agent/Mount | canonical 服务/00032 与共享 Audit/Project 四包18 top/28 sub race、四包 vet wholePASS；Mount/00035 四 top race/vet 修后 run02 wholePASS，原 run01 fixture 比较失败保留 | content，`/workspace/agenteam-agent-configuration-core`；真实 SQL/F1 未验，四个提供方不能模拟 Agent 成功 |

Model References 实现为 `model/agent_references.go`，Secret References 为 `projectvariable/secret_references.go`，均复用已交 Selection/Directory，在调用方原 Tx 验证完整旧集合/owner version 和 Agent 私有 postimage 后写完整新集合。两项纯检查不证明真实 Agent/SQL 集成。Secret Directory01 的 Version0 私有 projection JSON 失败、Skills pure-01 与 Mount run01 失败分别保留，不由后继补集回填。

已交联合 metadata PG 由 work_ui 编写唯一真实 fixture，secret 编写入口；正常 metadata、正式变更后的 stale、Logout 后当前 Session 拒绝与原 callback rollback 已 wholePASS。两个 Discover 使用同 Human/Project/原 agent.create key，caller 一次 Normalize/AcquireAll 完整并集，在同 Store 原 Tx 调两个 Require；沿用的 Project Skills 初始化 fixture 已披露。References、默认 assignment、Registry install 与 F1 成功不在此链。

## 四域共用的 Agent 创建与事务语义

Agent owner 是 canonical 配置、config_version、原 command/receipt 和创建意图的唯一写者。创建 witness 必须由该 owner 在**同一活 caller Tx** 内产生并验证：当前 Human/Session、Project、AgentID、原 `agent.create` CommandIdentity、原计划修订/意图、expected absent/resultVersion=1，以及本 Tx 实际尚未提交的 canonical postimage。普通 AgentRef、OwnerGrant、公开 UUID 或查到一行不构成该证明。

Model/Secret/Skill/Tool 可各有本域 typed owner-authority 接缝，但其实现须由同一 Agent 私有 writer 提供本域安全投影。更新沿 `agent.update` 的真实 preimage/postimage 与 expected/resultVersion；不能让四个调用方各维护一份可漂移的 version。完整外域引用和初始 assignment 与 Agent、receipt、typed Outbox/Audit 在同一 final Tx 提交，外层提交前结果只属 tentative，不能给 Work 当前可指派身份。原 completed replay 先返回 receipt；Unknown 由原 Agent command Lookup 裁决，不给初始化器另造命令 key。

Discover 只读发现依赖，不能授权写入。首个 AcquireAll 必须合并 Command、User/系统 registry、Project、Agent、Model/Provider、Skill/Tool 与引用/Outbox 的完整锁计划，遵循原 rank 0→1→2→4→5→6；具体 Registry key 由该域在正式 SPEC 冻结，不临时补 rank 1 锁。InTx 仅用同 Store/活 Tx 和 RequireHeldLocks 重验，不 Begin/Acquire/Commit、不执行对象或网络 I/O。当前权限失效、依赖变更和 SQL 错误都不能降级为合法空集合。

## Skills：可独立实现的最小真实部分

root 已授权 coordination 独占 `skill/contract/agent_initialization{,_test}.go`、`skill/agent_initialization{,_test}.go`、`skill/agent_assignment_repository{,_test}.go`、必要相邻 Cleanup 保护及00034。独立 SQL-only 构造 `NewAgentInitializer(existing Authority, AgentCreationAuthority)` 的精确跨域口与 content 直接落定，不改变当前 Skill Service 的 Object/读取/生命周期依赖，不提供后设 setter 或默认成功 authority。

实际实现遵循 Agent consumer-owned `AgentSkillsInitializer`：`DiscoverNewAgentInitialization(ctx, request)` 与 `InitializeNewAgentInTx(ctx, tx, request, plan)`。request 内含 Actor、ProjectID、AgentID、原 command/PlanRevision 与展开后的 AddSkillsEnabled；私有 plan 绑定 issuer、原请求、真实 Agent 创建计划、protected Skill 映射及完整锁。Initialize 先同 Store/活 Tx/完整锁，再当前 Owner Read/Mutate、真实 Agent 创建 witness、protected/serving/published Skill 当前映射，最后写本域唯一初始集合。

assignment 是独立权威集合，Skills 写自身 head/assignment；Agent config_version 仍仅 Agent owner 写，创建 version 保持 1。建议 head 从 sequence=1 起步（显式 false 也是一次空初始集合）；true 产生唯一 assignment identity 和稳定 SkillID，观察到的 revision 只作本次初始化校验，不永久钉住后续 Execution。false 不插 assignment、不在后继读写中自动补回，仍不能绕过真实初始化 provider、当前权限及原创建意图。普通运行中分配的 AssignmentRuntimeSink 未绑定时仍拒绝，不能为新建路径伪造 no_active_execution。

新增 head/assignment 表使用唯一分配的 `00034_agent_skill_assignments.sql`，不改旧00027或Agent/Registry的00032/00033。同域 FK/活 assignment 唯一性、创建身份/receipt 和索引已写；迁移未真实运行。相邻 Cleanup 已在 gate/材料释放前对未退役 head/assignment fail closed，并将新表纳 empty 检查；显式 disabled head 也不是已退休事实。

初次正向组合须等真实 Agent owner：已初始化 Project/真实 protected 出版→同 final Tx 新 Agent 与初始集合→commit/原 receipt；中段失败全部回滚。未发布 witness 缺失、结束/foreign Tx、缺锁、旧 Agent、Owner 失效、Skill 映射变化与显式禁用为必要基础问题。SQL 手种 Agent 或 fake authority 只属明确测试替身，不证明 F1。

## Registry：与 Skills 并行但不伪造默认 Tool

真实 Registry 至少需要持久且唯一的 stable_key→UUIDv7 ToolID、不可变 `(ToolID, SpecRevision)` 定义、当前注册状态与正式 Backend binding。UUIDv7 仅首次创建事务生成，同定义重启/重新注册复用原身份与 revision，不硬编码 ID 或每次解析重造。定义含 canonical Schema/annotations，不含在线状态、凭据或统一 timeout；注册必须关联真实领域端口、scope resolver 和 risk classifier。建议 cleanup 后续独占 `internal/central/tool/contract/` 的目录/owner 窄口及 `internal/central/tool/registry/` 的身份/spec/目录/引用实现与相邻测试，精确文件和迁移由其有限 SPEC 再冻结，不改 NameTable 算法或 Agent C1。

F1 所需配置口是只读 Discover 与同 Tx Require/Apply refs 的组合：对每个请求 ID 返回明确 typed 状态，核当前 Owner/Project scope 与 ordinary 可配置类型，禁止 Core Tool 进入白名单；反向用 Agent 同 Tx witness 核完整 before/after 集合。注册/移除与引用变更共享 gate，历史身份/spec/ref 保留，显式移除影响新选择；临时 Runner offline/MCP 不可达不视为 removed。当前 Registry 更新不能改旧 Execution Snapshot，NameTable 必须按原 Snapshot 反解。

**当前独立缺口是 install-skill 的实际后端绑定。** [统一安装设计](../../docs/architecture/agent-skills.md)要求 UI/Tool 消费同一受控包安装服务。skills_http 已在 `/workspace/agenteam-skill-install` / `ai/skill-install` 保存并推送生命周期/Object 接缝 10 路径 `748ac4a7`；cleanup 执行的 pure01 新 8 top/9 sub race、旧 init-success/Stop-discard 与单包 vet wholePASS，输入一致、全部原尾闭合。00036、公开 Install 与真实发布 Builtin Backend 仍未完成。Registry 候选的 `BuiltinSource.CheckBindingInTx` 必须证明实际 Backend/resolver/risk 绑定；`InstallSkillEnabled` true/false 都查真实 source/注册身份，不允许 false+空目录绕过。不能以 Project 初始化接口、常量 ToolID、测试 source 或空 handler 冒通用安装成功。

Agent 创建的两个默认 bool 继续省略为 true、允许显式 false；显式禁用不授权缺省依赖为空，不收窄其他 capability 字段。Mount 真实目录/引用保护也仍在 F1 完整门内，不由此次 Model/Secret/Skills/Registry 准备代证。当前约 30% 是平台粗估，E01 未开始；Object runtime join、OpenAI tools 独立验收等旧 STOP 均不改变。

## 前置阶段检查边界

本次摘要更新只写既有恢复文档，未运行新 Go/PG/guard/浏览器。SecretRefs、Installer 与 Mount 检查点均已推送确认；metadata、Installer pure01 和本轮其他模块原尾均已闭合，热 cache 已归还，当前无 Go/PG/socket 在途。后继先组合连续迁移与真实提供方，再验证同 Tx canonical Agent/完整 refs/Skills assignment/Audit/outbox/receipt；不以当前纯控或 metadata 首链宣称 00032–00036 SQL、生产 F1 或完整 participant 已通过。
