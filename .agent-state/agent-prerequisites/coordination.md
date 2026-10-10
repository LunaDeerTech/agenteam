# Agent F1 前置关系与并行接口建议

本简报区分现有实现片段与下一接口建议，不声明完整前置已落地。Model Selection 与 Secret Directory 首片段的修后纯检查已通过，尚无真实 PG/F1。root 已分配实际作者：content 为 Agent canonical 核心/00032、cleanup 为 Registry/00033、coordination 为 Skills 新 Agent 初始化/00034；三域尚在实施，默认生产 Project initializer、完整 participant/Registry 与 F1 保持未绑定。

## 正式来源与实际缺口

按 [Agent F1 §2/§6/§7](../../docs/development/work-items/d10-agent-configuration.md)、[D01 Skills](../../docs/development/work-items/d01-contracts/resources-skills.md)、[D01 Tool](../../docs/development/work-items/d01-contracts/model-tool.md)及 [Registry](../../docs/architecture/tool-system/tool-definition-registry.md)推进，不能以本简报改写其门槛。

| 提供方 | 现有事实 | 当前作者与最小下一口 |
| --- | --- | --- |
| Model | 配置期 Selection 首片段已实现，7 新 top race 与两包 vet 原全尾通过；运行解析不证明配置授权 | work_ui，`/workspace/agenteam-agent-model-prerequisites`：下一真实 PG 首链；agent_model/approval_model 双角色 References 及真实 Agent owner authority 尚未实现 |
| Secret | [SecretDirectory/References](../../internal/central/projectvariable/contract/secret_directory.go) 合同已有；Directory 修后 4 top race 与同包 vet 原全尾通过，目录不读取或输出值 | secret，`/workspace/agenteam-agent-secret-prerequisites`：下一真实 PG 首链；References/反向 Agent 私有 witness 尚未实现，不冒 F1 |
| Skills | [Service](../../internal/central/skill/service.go) 与 published 映射可复用；[00027](../../db/migrations/00027_skills.sql)没有 Agent assignment/head/sequence | coordination，`/workspace/agenteam-agent-skill-initialization`：新 Agent 专用初始化器、集合及 Cleanup 保护/00034；不实现普通运行中 Assign/Remove |
| Tool | [SpecRef](../../internal/central/tool/contract/spec_ref.go) 与 [NameTable](../../internal/central/tool/projection/name_table.go)仅纯值/名称投影 | cleanup，`/workspace/agenteam-tool-registry`：真实 Registry、配置目录及原子 refs/00033；不能把名称表当 Registry |
| Agent | 原主线只有 C1，canonical 创建服务/表/私有 mutation witness 由新切片实现 | content，`/workspace/agenteam-agent-configuration-core` 与00032；四个提供方不能各自模拟 Agent 存在或成功 |

Model Selection 接口与实现已落在 `model/contract/agent_configuration.go`、`model/agent_selection.go` 及相邻测试；`model/agent_references.go` 仍为后继范围。Secret Directory01 的 Version0 私有 projection JSON 编码失败保留，Directory02 改可选凭据版本（live 填真实值、缺凭据 nil）后有限通过。两域结果均为相应源码的纯 race/vet，不能拿测试替身宣称真实 PG、Agent 创建或 F1 已通；不另造 CredentialRef 或第二份 Agent 表。

联合 metadata PG 由 work_ui 独占 `/workspace/agenteam-agent-configuration-integration` 中的新 `tests/projectvariable/agent_configuration_metadata_test.go` 与方法；secret 独占该链入口/必要 shared 增量。真实 Account/Project 和正式 Model/Secret 创建提供数据；两个 Discover 使用同 Human/Project/原 agent.create key，caller 一次 Normalize/AcquireAll 完整并集，在同 Store 原 Tx 调两个 Require。首链只验证正常 metadata、正式状态变更后的 stale、Logout 后当前 Session 拒绝与原 callback rollback；公开说明沿用的 Project Skills 初始化 fixture，不 SQL 伪造 Agent。References、默认 assignment、Registry install 与 F1 成功不在这条链。

## 四域共用的 Agent 创建与事务语义

Agent owner 是 canonical 配置、config_version、原 command/receipt 和创建意图的唯一写者。创建 witness 必须由该 owner 在**同一活 caller Tx** 内产生并验证：当前 Human/Session、Project、AgentID、原 `agent.create` CommandIdentity、原计划修订/意图、expected absent/resultVersion=1，以及本 Tx 实际尚未提交的 canonical postimage。普通 AgentRef、OwnerGrant、公开 UUID 或查到一行不构成该证明。

Model/Secret/Skill/Tool 可各有本域 typed owner-authority 接缝，但其实现须由同一 Agent 私有 writer 提供本域安全投影。更新沿 `agent.update` 的真实 preimage/postimage 与 expected/resultVersion；不能让四个调用方各维护一份可漂移的 version。完整外域引用和初始 assignment 与 Agent、receipt、typed Outbox/Audit 在同一 final Tx 提交，外层提交前结果只属 tentative，不能给 Work 当前可指派身份。原 completed replay 先返回 receipt；Unknown 由原 Agent command Lookup 裁决，不给初始化器另造命令 key。

Discover 只读发现依赖，不能授权写入。首个 AcquireAll 必须合并 Command、User/系统 registry、Project、Agent、Model/Provider、Skill/Tool 与引用/Outbox 的完整锁计划，遵循原 rank 0→1→2→4→5→6；具体 Registry key 由该域在正式 SPEC 冻结，不临时补 rank 1 锁。InTx 仅用同 Store/活 Tx 和 RequireHeldLocks 重验，不 Begin/Acquire/Commit、不执行对象或网络 I/O。当前权限失效、依赖变更和 SQL 错误都不能降级为合法空集合。

## Skills：可独立实现的最小真实部分

root 已授权 coordination 独占 `skill/contract/agent_initialization{,_test}.go`、`skill/agent_initialization{,_test}.go`、`skill/agent_assignment_repository{,_test}.go`、必要相邻 Cleanup 保护及00034。独立 SQL-only 构造 `NewAgentInitializer(existing Authority, AgentCreationAuthority)` 的精确跨域口与 content 直接落定，不改变当前 Skill Service 的 Object/读取/生命周期依赖，不提供后设 setter 或默认成功 authority。

建议窄口是 `DiscoverNewAgentInitialization(ctx, Human, request)` 与 `InitializeNewAgentInTx(ctx, tx, Human, request, plan)`。request 绑定 ProjectID、AgentID、原创建 command/修订与展开后的 AddSkillsEnabled；plan 绑定实例 issuer、原 identity/意图、真实 Agent 创建计划、protected Skill 映射及完整锁。Initialize 顺序为同 Store/活 Tx/完整锁→当前 Project Owner、initialized 与 Mutate→真实 Agent 创建 witness→本 Project protected/serving/published Skill 及当前 revision 重验→本域唯一初始集合写入。

assignment 是独立权威集合，Skills 写自身 head/assignment；Agent config_version 仍仅 Agent owner 写，创建 version 保持 1。建议 head 从 sequence=1 起步（显式 false 也是一次空初始集合）；true 产生唯一 assignment identity 和稳定 SkillID，观察到的 revision 只作本次初始化校验，不永久钉住后续 Execution。false 不插 assignment、不在后继读写中自动补回，仍不能绕过真实初始化 provider、当前权限及原创建意图。普通运行中分配的 AssignmentRuntimeSink 未绑定时仍拒绝，不能为新建路径伪造 no_active_execution。

新增本域 head/assignment 表使用唯一分配的 `00034_agent_skill_assignments.sql`，不改旧00027或Agent/Registry的00032/00033。同域 FK/活 assignment 唯一性、创建身份和安全 receipt、Project 查询索引应纳该迁移。**相邻 Cleanup 必须同时封口**：当前清理只认旧表，新增集合后，在真实 gate 释放材料前对未退役初始化/assignment 事实 fail closed，并把新表纳 empty 检查；不能等最后 FK 报错时才保护已经删除的 payload。

初次正向组合须等真实 Agent owner：已初始化 Project/真实 protected 出版→同 final Tx 新 Agent 与初始集合→commit/原 receipt；中段失败全部回滚。未发布 witness 缺失、结束/foreign Tx、缺锁、旧 Agent、Owner 失效、Skill 映射变化与显式禁用为必要基础问题。SQL 手种 Agent 或 fake authority 只属明确测试替身，不证明 F1。

## Registry：与 Skills 并行但不伪造默认 Tool

真实 Registry 至少需要持久且唯一的 stable_key→UUIDv7 ToolID、不可变 `(ToolID, SpecRevision)` 定义、当前注册状态与正式 Backend binding。UUIDv7 仅首次创建事务生成，同定义重启/重新注册复用原身份与 revision，不硬编码 ID 或每次解析重造。定义含 canonical Schema/annotations，不含在线状态、凭据或统一 timeout；注册必须关联真实领域端口、scope resolver 和 risk classifier。建议 cleanup 后续独占 `internal/central/tool/contract/` 的目录/owner 窄口及 `internal/central/tool/registry/` 的身份/spec/目录/引用实现与相邻测试，精确文件和迁移由其有限 SPEC 再冻结，不改 NameTable 算法或 Agent C1。

F1 所需配置口是只读 Discover 与同 Tx Require/Apply refs 的组合：对每个请求 ID 返回明确 typed 状态，核当前 Owner/Project scope 与 ordinary 可配置类型，禁止 Core Tool 进入白名单；反向用 Agent 同 Tx witness 核完整 before/after 集合。注册/移除与引用变更共享 gate，历史身份/spec/ref 保留，显式移除影响新选择；临时 Runner offline/MCP 不可达不视为 removed。当前 Registry 更新不能改旧 Execution Snapshot，NameTable 必须按原 Snapshot 反解。

**当前独立缺口是 install-skill 的实际后端。** [统一安装设计](../../docs/architecture/agent-skills.md)要求 UI/Tool 消费同一受控包安装服务；现 Skill Service 只有 Project 初始化、读取和 Cleanup，没有通用 Install。不能注册常量 ToolID 加空 handler，也不能借 Project 初始化接口冒通用安装。需另定 Skill Install 服务与 D21 adapter 的作者域，先冻结受控材料、校验/对象写入/不可变发布/幂等恢复及正式 schema，再以真实绑定注册默认 Tool；职责名称不替代尚待冻结的 stable_key、handler/contract_revision、schema、scope/risk 身份。Registry 的持久身份/目录/引用可与 Skills 新 Agent 初始化并行开发，明确 contract fixture 只证明其事务协议；生产默认 install-skill 解析保持 unbound，不能将定义草稿标 callable。

Agent 创建的两个默认 bool 继续省略为 true、允许显式 false；显式禁用不授权缺省依赖为空，不收窄其他 capability 字段。Mount 真实目录/引用保护也仍在 F1 完整门内，不由此次 Model/Secret/Skills/Registry 准备代证。当前约 30% 是平台粗估，E01 未开始；Object runtime join、OpenAI tools 独立验收等旧 STOP 均不改变。

## 本次检查边界

本次只有实际设计/源码阅读、作者间接口对齐及文档链接/差异检查；未写模块合同或迁移，未运行 Go、PG、guard、浏览器或新资源。上述接口建议须各 owner 在获授权的有限规格中落定；接口存在、纯控通过与真实跨域成功分别记录。
