# Agent F1 前置关系与并行接口建议

## 当前批次与活动树

正式main `d4a8626162b13c5eff9b98fcf63a5eabd956ba42`已交付第八批Human Owner无active review三边并远端exact确认，产品与实测SOURCE `8cfbe24d21938947787fcfeb7633b64a72866e94`一致；root正常merge/push（`b548eb95`），delivery已ff/push同一main，恢复README `245f2285`已保存。第七批main `5b0a3eed`及更早已接受范围保持。

第八批12 top、四包vet、compile01/list及native02最终接受；`TestTaskHumanReview`真实1 top/2 sub、34.54s整轮wholePASS，原TLS执行终态后review→done、原Tx rework回滚/重放已验证。四原Wait0、七ID14次absence及全部双尾闭合，1616输入一致（含876个编译输入）。candidate01复用、原件保留；pure01/native01仅容量门FAIL，无产品FAIL。当前源码停写，0活动、0待跑，资源窗口归root；约30%粗估和全部旧STOP不变。

最终恢复映射：Work `/workspace/agenteam-task-transition` / `ai/work-human-review`（`5e2c6c32`）已park；HTTP `/workspace/agenteam-task-human-http` / `ai/work-review-http`（`e8e251a2`）已park。`/workspace/agenteam-task-flow-delivery` / `ai/work-review-delivery`（`d4a86261`）是唯一完整源码树。content的Work/Foundation、work_ui的HTTP、coordination的`task_review_test.go`、skills_http的方法及secret有限审均已收口，无待执行任务。Scheduler donor继续park；FeatureIntegration `4d1b605e`与ToolOperation `6eb3a62a`本批九目录普通可逆park及protected后验已完成，精确回收量见current，前者全部原FAIL保留、后者output原本absent。第七全PASS候选04精确退休事实保持，第八candidate01与全部旧FAIL/refs不动。恢复位置见[当前并行产品线与执行者](../current.md#当前并行产品线与执行者)，方法沿[既有组合recipe](../agent-system-integration/README.md)，不沿下表历史分工安排写入或清理。

第八批三边`in_progress→in_review`、`in_review→done`、`in_review→todo`复用原Transfer/Lookup/schema 1，无新迁移。Human无active的有限profile已完成；三新边遇active明确DependencyUnbound，仍不改写规格允许的执行内交接。done省略assignee保旧reviewer，显式指派验当前资格；仅目标todo要求零unresolved。原Owner、pending/occupancy及同Tx门保持，Execution成功不自动done；active评审、AgentRun写权、review自动Dispatch、多轮/tools/Stream及生产app/initializer仍待后继。下一验证前预留缓存与候选增量空间，fresh≥5GiB门不变。

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
