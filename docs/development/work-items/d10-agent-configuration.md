# D10 Agent 配置与当前身份事实

状态：**C1 六个纯契约文件已独立接受；F1 核心在隔离树实施，完整创建前置未齐、未验收**。当前授权范围见 §9；既有 C1 意义与生产未绑定状态保持。`identity.AgentID` 只是 typed identity。root 已分配本域迁移 `00032_agent_configuration.sql`；新源码与迁移尚未编译或运行，不能作为 F1 能力使用。

本卡拟分成两个可分别验收的结果：**C1 纯 AgentCore 与当前身份端口契约**可在规格接受后先行；**F1 Human Owner 创建、读取、修改真实 Agent 配置，并提供同 caller Tx 的当前身份事实**必须等本卡列出的真实前置闭合后开工。F1 不接受 SQL 手种 Agent、默认成功目录或未绑定初始化作为生产创建路径。C1 完成不解锁 Task 指派；F1 完成也不等于 Executor、Agent 删除、完整 D10 或平台 ready。

## 1. 依据与当前事实

业务依据为 [Agent 管理](../../architecture/agent-management.md)、[Agent Skills](../../architecture/agent-skills.md)、[D01 资源与技能](d01-contracts/resources-skills.md#agent-配置与真实目录)、[D01 生命周期](d01-contracts/domain-lifecycle.md)、[D01 Model/Tool](d01-contracts/model-tool.md)及[开发计划 D10](../development-plan.md#d10-agent-配置与项目变量)。当前交付与停止项以[任务台账](../agent-team/tasks.md)为准；现行[团队流程](../agent-team/README.md)负责唯一写域、保存及独立验收。

| 依赖 | 当前可证范围 | 本卡消费与缺口 |
| --- | --- | --- |
| Foundation / PostgreSQL | typed UUIDv7、Version、Instant、CommandMeta、活 caller Tx、完整锁 union、CommitResult 已有实现 | 复用，不新增事务框架、SQL 驱动、锁类别；`AgentLock` 已在 rank 4 |
| Account / Project Owner | 当前 Session、Owner Read/Mutate、初始化及 Project lifecycle gate、Activity 已有已验库能力 | 同 Tx 真验；管理员无 Owner 旁路，Project initialized 不能被 Agent 构造器伪造 |
| Model 配置、读取及部分解析 | 真实 Model/Provider canonical、Owner/System 配置能力、部分当前 Resolver 已交付 | 能力可复用但范围有限；配置选择验证不能借运行 Resolver 生成虚构 Execution/lease |
| Model Agent 引用 | `model/contract.ReferenceOwner{Kind:"agent",Role:"agent_model"或"approval_model"}` 与 `References` 为现存纯契约 | **仅 contract**。`DiscoverReference/ApplyReferenceInTx` 未见实现；`model.prepareReplacement` 遇 Agent 引用明确 `DEPENDENCY_UNBOUND`，不能称已支持 Agent 原子替换 |
| Agent 配置目录 Model 校验 | D01 `ModelCatalog.ValidateSelectionInTx` 是概念端口；现有 `resolution_policy.go` 拒绝非空 reasoning effort，且解析含运行 snapshot/lease 职责 | **未实现本卡所需配置窄口**。由 Model owner 补同 Tx 可见性、enabled chat、effort 与引用写入/替换后，F1 才能消费 |
| Skills | [初始化卡](d10-skills-initialization.md) P1 的不可变包/builtin 材料、纯 contract 已验；Project 初始化收敛前置另已验 | **真实 Skill 发布、Agent 默认分配及 AssignmentRuntimeSink 未实现/未绑定**。材料存在不等于保护技能已发布或新 Agent 已初始化 |
| Tool / MCP 目录 | 架构与 D01 稳定 Tool 引用、Core Tool、MCP scope 规则 | **未实现所需真实目录/原子引用保护**。默认 install-skill 的普通 Builtin Tool 身份也须由正式 Registry 返回 |
| Runner / Mount 目录 | 逻辑 Mount/workspace、offline 与有效引用分离的规则 | **仅设计契约**；不得用 Runner 在线布尔值、任意路径或空目录替代同 Project/Agent 引用校验 |
| Secret / 项目变量 | D04 Secret 加密、凭据/lease 等限定能力已交付 | **项目 SecretVariable 业务目录及 Agent 白名单引用口未实现**；`CredentialRef` 不能冒充 `identity.ProjectVariableID` 或证明其为 Secret |
| Task / Work | Structure 已验；Task Planning 契约已验，runtime 当前仍在验收 | 本卡不将 Task runtime 候选算已接受。assignee/reviewer 当前引用及状态转换是后继 Work 的事实责任 |
| Execution / Dispatch / Meeting / Memory / Governance | D01 端口与架构责任存在 | **实际 Agent slot、活动/历史占用、运行 snapshot、Memory namespace 清理等未绑定**；不实现 Agent 删除或把这些域报成无引用 |
| Outbox / Audit | 已有 typed planned Append、同 Tx 与当前 Project gate 等前置 | Agent 自己的 typed producer/Audit 业务类型仍须正式实现与验收；合法 event DTO 不构成 producer 权限 |

源码核对入口：`internal/central/model/contract/{configuration,references,authority}.go`、`internal/central/model/{references,resolution_policy,configuration}.go`、`internal/central/project/contract/lifecycle.go`、`internal/central/foundation/lock.go`、`internal/central/skill/contract/{types,read}.go`。此表是库能力核对，不是本轮重新执行全部历史测试的声明。

Object runtime join、OpenAI tools 独立验收、Central SPA concurrent-publication、Jina/Image 来源停止线保持原状；本卡不修复、重新发起或以新测试恢复它们。真实 Skills/Object 组合仍受原阻塞约束。

## 2. 结果边界与分段门槛

### 2.1 C1：可独立先交付的纯契约

仅新增 `internal/central/agent/contract` 的 `AgentCore`、`AgentRef`、严格 codec、枚举、Clone、当前事实端口声明及纯测试。`AgentCore` 仅包含§3 canonical清单去掉三类引用数组后的核心配置字段，所有ID类型均已存在；它明确不是完整 AgentConfig，也没有创建服务。完整 AgentConfig/三类Capability与创建、更新、Lookup命令契约等待各外域ID/目录契约实际冻结后作为F1前置补齐，不把不存在的Go marker纳入C1。不创建 Store、service、adapter、HTTP、生产 root、迁移、目录数据、默认 Agent 或 fake authority。复用 `identity.AgentID`，不定义第二种 Agent marker。

C1 需要闭合其实际类型的 presence、当前事实错误顺序、锁要求与删除阻挡责任；本文同时规定完整配置的后续目标与 Model 引用接缝，尚缺的跨域接口不能算C1已实现。接口值可以供调用方编译依赖；零值、typed nil、缺失依赖的行为必须明确失败。不得提供返回 active、free、initialized、空引用或成功 mutation 的默认实现。

### 2.2 F1：真实有限事实能力，当前前置未齐

拟提供 Human Owner 的 `CreateAgent/GetAgent/UpdateAgent/LookupAgentCommand`，以及 `WorkReferences.RequireCurrentInTx`。只做直接配置；Preset 复制、运行时授权、Skill 动态分配、Agent 删除/停用/恢复、Project lifecycle participant、HTTP/UI/App root 不在 F1。

F1 开工前必须已有可供组合的 Model 配置验证与 Agent 引用写入/替换、保护 Skill 默认分配、普通 Tool Registry 的 install-skill 身份、所支持 Capability 的真实目录及其引用保护。Agent constructor 不接受缺口端口，不能以“本次传空数组”为由注册假的全域目录。某类配置若拟另分结果，必须先收窄受支持命令/DTO与本卡验收范围并再经 SPEC 审查，不能实现时静默丢字段。

**F1 仍 BLOCKED；ID 位置接缝已由 [R1](d01-resource-identities.md)闭合。** `identity.ToolID/MountID/ProjectVariableID` 的唯一 marker 位于层 1，完整配置可向下依赖；`agent/contract` 仍不能 import 层 5 的 ToolCatalog/ToolSpec。真实窄目录投影、同 Tx 引用保护及其余前置尚未闭合，不能把三个 ID 声明当 F1 已就绪。C1 的 AgentCore 排除三类引用数组，保持原样。

默认 Add Skills 启用、install-skill 默认启用且允许显式禁用属于既定产品规则。新 Agent 的配置事实与默认分配必须原子可证明，已有 Project initialized 只证明 Project 初始化，不证明该 Agent 已获得默认能力。缺保护技能、缺 Registry 或初始化错误时，创建失败且无可供 Work 指派的 active Agent。

## 3. 类型、字段与严格 schema

建议包名 `internal/central/agent/contract`，下文简写 `agentc`；`ProjectID/AgentID` 复用 identity，`ModelID` 复用 model contract，`Version/Instant/Digest/CommandMeta` 复用 Foundation。`AgentCommandID` 使用本域新 marker，不能用传输 RequestID 作业务操作 identity。

### 3.1 canonical AgentConfig

所有下列 JSON 键必有；仅带 nullable 的值允许 null：

```text
AgentConfig {
  id, project_id, name, normalized_name,
  display_name: string|null, tag_color: string|null,
  description, instructions, inject_agents_md: bool,
  model_ref: ModelID, reasoning_effort: string|null,
  allowed_tool_ids: ToolID[], allowed_mount_ids: MountID[],
  allowed_secret_variable_ids: ProjectVariableID[],
  approval_policy: default|auto|allow, approval_model_ref: ModelID|null,
  lifecycle: active|deleting,
  version: Version, created_at: Instant, updated_at: Instant
}
```

`version` 是 D01 所称 Agent `config_version` 的唯一权威；不另存两个可能漂移的计数器。Skill assignments 的真实配置改变也必须经正式 Agent 更新口推进此 version，不能由 Skill 直接改 Agent 私表。`lifecycle` 是配置删除门禁，**不表示 busy/idle、模型可调用或 Execution 状态**。F1 只产生 active；deleting 的进入/恢复/完成由未来删除卡冻结，本卡不暴露该写入口。没有 pending Agent 的成功创建结果；初始化未完成的材料不能通过本卡当前事实口。

Skill assignments 是独立权威集合，不伪造 `skills:[]` 到 AgentConfig；完整 immutable Skill 包、Tool schema、Secret 值、Model 请求参数、Platform Prompt、system_prompt、运行 Snapshot/Execution/审批历史均不得嵌入本 DTO。关联类型须由所属 contract 提供，类型尚未存在时先由该 owner 冻结；不得为凑编译在 Agent 中复制其 marker。

工程边界提案：

| 字段 | 严格约束 |
| --- | --- |
| name | 3–32 ASCII 字节，`[A-Za-z0-9][A-Za-z0-9-]{1,30}[A-Za-z0-9]`，沿现有 username 字符/长度规则；保留输入大小写，normalized_name 仅 ASCII lowercase，Project 内唯一；创建/改名保留字沿本节明确清单，当前规范不允许自行扩大长度 |
| display_name | null 或 1–256 Unicode scalar、≤1024 UTF-8 字节，至少一个非空白，无 Cc；不 trim/NFC，空字符串不当 null |
| tag_color | null 或规范小写 `#[0-9a-f]{6}`；展示用途，不映射权限/角色 |
| description | 0–8192 UTF-8 字节；允许 TAB/LF/CR，其余 Cc 拒绝，保持原字节 |
| instructions | 0–32768 UTF-8 字节，控制字符规则同 description；不是 Secret 存放渠道 |
| reasoning_effort | null 或 1–32 ASCII 字节的 `[A-Za-z0-9_.:-]+`，与现有 Model contract safe token 一致；C1 只验证语法，F1 目录还必须核 Model 能力闭集 |
| 引用数组 | 完整Config阶段必有非 null、各≤128、合计≤256，无重复，按所属契约canonical identity升序；非升序先严格拒绝，不通过去重掩盖调用者错误；各类ID边界见下段，C1不声明这些Go类型 |
| version / time | version≥1、int64 上界，wire 为规范十进制字符串；UTC 微秒，created_at≤updated_at；没有可写客户端 version/time/lifecycle |

名称语法与 `account/contract/profile.go` 一致；创建/改名的保留字按 `account/validation.go` 当前已定集合：`api,assets,auth,login,logout,invite,reset,settings,system,personal,diagnostics,livez,readyz,debug,support,root,admin`。Agent 单独执行纯校验，不调用 Account 实现、共用 User 占用表或引入 bootstrap admin 例外；允许与任意合法普通 User 同名。大小写仅在规范化名/路由消除，正文不归一。

外域引用不得混淆：D01 ToolSpec 的 `tool_id` 是 UUIDv7 数据库身份，`stable_key` 才是 `builtin:<name>` / `runner:<name>` / `mcp:<config_id>:<remote_name>` 字符串；Agent 的 `allowed_tool_ids` 保存前者，不把后者塞进 UUID 解析器。R1 已在 identity 层唯一定义 Tool、Mount、ProjectVariable 的 canonical UUIDv7 类型。上文 ToolID/MountID/ProjectVariableID 均指这些 alias，普通与 Secret 变量共享稳定变量身份；白名单仍须由真实目录验证当前同 Project 且 type=secret。各 owner 的 scope、目录及引用保护仍须冻结并实现后才能补完整 AgentConfig，不使用原始宿主路径、RunnerID、Secret CredentialRef 或字符串占位来代替。C1只依赖已存在的 identity/Model/Foundation。

全体对象包括嵌套 Capability、私有持久 request/plan、历史 payload，逐层拒绝未知 key、大小写别名、重复 key、null 越界、缺必有字段、非法 UTF-8、孤立 surrogate、尾随值、错误标量类型。不能只用 `DisallowUnknownFields` 或 canonical-v1 代替 schema。请求/单个 Agent/单 receipt cap 建议512KiB，Lookup request16KiB，safe history/typed event payload16KiB，私有计划4MiB。每个 codec 检查其方法实际收到的完整 raw；标准 `json.Unmarshal` 裁外层空白的行为单独测试。正文最坏 escaping 与三类共256项引用合计仍须经实测证明低于 cap。

全部导出 DTO 提供 Validate、严格编解码与 Clone，pointer/slice/maps 深复制。fmt/slog 使用固定 `agent_config` 等安全投影，Fault wire 不含正文、Secret、command key、SQL 或内部 cause；外域纯 DTO 的安全格式化边界不得靠反射猜测私有结构。显式授权 JSON 才是业务传输通道。

## 4. 正式端口与 Work 引用保护

### 4.1 C1 的精确当前事实口

```go
// 新 internal/central/agent/contract；全部ID复用已有identity marker。
type AgentRef struct {
    ProjectID     identity.ProjectID `json:"project_id"`
    AgentID       identity.AgentID   `json:"agent_id"`
    ConfigVersion foundation.Version `json:"config_version"`
}
type WorkReferences interface {
    RequireCurrentInTx(context.Context, foundation.Tx, identity.Actor,
        identity.ProjectID, identity.AgentID) (AgentRef, error)
}
```

AgentRef 三键必有，非零 UUIDv7、正 Version，完整编码≤16KiB。它是本次活 Tx 内的当前事实投影，**不是 bearer grant、运行资格、空 slot 或有效 Model/Tool 的证明**。不能序列化后在未来 Tx 直接复用。C1 只有纯类型/接口；F1 才提供真实实现。

请求先作纯参数、ctx 与 Actor 分类检查；对合法 Human，F1 的方法严格依次：同 Store 活 caller Tx → RequireHeldLocks（User SH、Project SH、ProjectSchedule EX、目标 AgentLock SH）→ 真实当前 Human Owner Read → 本域 canonical 行及初始化/删除门禁。EX 满足 SH，SH 不满足 Schedule EX。不自开/提交 Tx、不补锁、不 TouchActivity、不访问网络、不查询 Work 私表。外 Store/结束 Tx/缺锁在第一条 Agent SQL 之前拒绝；Project不存在/非Owner管理员/Session撤销沿真实Authority错误，不能先查 Agent 暴露存在性。缺Agent或外Project Agent统一 `NOT_FOUND`，deleting为 `INVALID_STATE/AGENT_NOT_CURRENT`；SQL/decoder损坏返回安全错误与零 AgentRef。

只在真实 Agent 已完成创建默认分配且 lifecycle=active 时返回匹配 ProjectID/AgentID/当前 version 的 Ref；合法 Actor 中 AgentRun 返回 `DEPENDENCY_UNBOUND`，Service 返回 `FORBIDDEN`，未构造 Actor 为 `UNAUTHENTICATED`。AgentRun 管理/执行授权将来必须消费真实 Execution/Tool 权限，不能靠此口或合法 AgentID 构造出权限。

调用 Work 新指派/reviewer 转换前必须已取得 Project Mutate 并锁完整 union。已知新 assignee 的 AgentLock SH 与旧 assignee 的 AgentLock SH 一并取得；旧身份未知时用只读发现 Tx 收集，结束后再构造下一完整集合。顺序为 Command→User/System registry→Project→Schedule/RankGroup→全部 Agent gates→Sprint/Task等 aggregate→Outbox/reference record。不得先读/锁 Task 再补 rank4 AgentLock。

### 4.2 Work 拥有当前引用，Agent 拥有删除决定

拟由 Work 后继实现下述窄口，类型/接口放 Work contract；Agent 只消费正式接口，不跨域 SQL：

```go
type AgentReferences interface {
    HasCurrentAgentReferencesInTx(context.Context, foundation.Tx,
        identity.Actor, ProjectID, identity.AgentID) (bool, error)
}
```

要求同 Store 活 Tx、User SH、Project SH、Schedule EX、目标 AgentLock EX，真实 Owner Read 先于 Work SQL。检查本 Project 全部 canonical Task 当前 assignee 引用，包括 done/cancelled 保留值。reviewer若由 `in_review.assignee_agent_id` 表达就读同一权威列；后续若新增独立 current reviewer/review assignment，Work必须同时纳入，不允许Agent端自行拼第二份表。TaskEvent 中历史 actor/旧assignee不是 current ref，按显式历史保留策略处理。

返回false只证明该 Tx 内 Work 没有当前引用，不证明 Agent 可删除、无历史或没有 Execution。未来 Agent 删除/进入删除门禁必须 Schedule EX＋Agent EX，先完成所有事实所属域的真实检查：Work当前引用、Meeting participant、Execution/Dispatch active与历史责任、Skill assignment/运行 binding、Model references、SecretVariable白名单、Mount托管资源、Memory namespace、Governance与其它已登记引用。任一required port缺失/未知/读取失败都不能成功；waiting和取消未join仍busy，不能由 Agent 配置行不存在busy列推出空占用。

F1没有 DeleteAgent、DisableAgent 或 lifecycle participant，因此本卡不实现“默认拒绝的完整删除服务”。删除的精确历史保留、清理操作/确认与 required manifest 由后继完整删除卡冻结；这张卡先规定不得丢引用及共享锁协议。Model/Work也不因未来删除计划而级联清Agent/Task历史。

该接缝同时出现在 [Task transitions 草稿](d11-task-transitions.md)中，两卡互相引用只表示待审约定；必须分别取得SPEC接受、真实F1和Work实现验收才可宣称可指派/可删除。

## 5. F1 命令、历史与提交结果

以下是完整配置阶段的目标接口；只有§2.2真实前置和外域类型补齐后才进入实施，不纳入初始C1的可编译声明：

```text
CreateAgent(ctx, Human, CommandMeta, project_id, AgentCreate) -> AgentMutation
UpdateAgent(ctx, Human, CommandMeta, project_id, agent_id, AgentUpdate) -> AgentMutation
GetAgent(ctx, Human, project_id, agent_id) -> AgentConfig
LookupAgentCommand(ctx, Human, project_id, command, idempotency_key,
                   semantic_digest) -> {status,receipt}
AgentMutation {agent: AgentConfig,changed: bool,event_ids: EventID[]}
```

- `AgentCreate` 必有 `agent_id,name,model_ref,inject_agents_md,approval_policy`及三类显式引用数组；description/instructions省略为空。display_name/tag_color/reasoning_effort/approval_model_ref省略展开null，允许显式null。`add_skills_enabled`、`install_skill_enabled` 是只在创建请求存在的可选bool，省略各展开true，显式null拒绝；服务通过真实目录取得默认对象，最终授权以正式assignment及allowed_tool_ids事实为准。显式禁用install-skill而引用数组仍包含该正式ToolID为矛盾输入，拒绝，不静默重新启用。创建无 expected_version；caller预生成AgentID并以同意图复用。
- `AgentUpdate` 只含可编辑配置字段，不含 id/project/lifecycle/version/time、Skill assignment或运行数据；至少一项presence。普通text/bool/arrays用optional pointer表达省略，显式null拒绝；nullable字段用 `{Present bool, Value *T}` 服务内三态，JSON省略不改、null清空、值设置。需要 expected_version≥1；不能以当前读出version替换caller原expected。数组全量替换时不重新补创建默认Tool或Skill，不复活用户禁用。
- 当Model支持非空reasoning_efforts时，reasoning_effort必须显式在集合中；否则必须null。auto必须有当前合法approval_model_ref，default/allow必须null；切换时连同引用增删原子提交。approval_policy只配置将来授权策略，不发Provider请求、自动批准或绕过Scope/Capability。
- Lookup的command闭集为 `agent.create,agent.update`；identity=`project/[project_id]/command/key`。摘要精确包含format、command、project、target、stable Human User、expected presence与完整请求presence/default展开，canonical-v1 SHA256；排除Session、RequestID、CSRF、Key、服务生成ID/时刻/计划。跨Session同User仍须当前有效。Lookup无body猜测或可自动重试标志。

错误顺序：admission/ctx与纯语法 → 完整锁 → 当前Session/Owner Read/Project initialized → 原command identity（异writer NOT_FOUND，异义 IDEMPOTENCY_KEY_REUSED，同义completed先Clone历史）→ 新写Mutate → target存在/Project归属 → 原expected → Agent active → 候选配置全部真实引用/默认项 → 有效变化/容量/计数器 → 原子写。archiving/archived可读及replay完成receipt，不能补planned。deleting/未初始化Project沿Project门禁拒绝。错误沿现有Foundation `NOT_FOUND,VERSION_CONFLICT,INVALID_STATE,RESOURCE_BUSY,DEPENDENCY_UNBOUND,DEPENDENCY_UNAVAILABLE` 及固定字段理由，不提前批量新增未来AGENT_* code。

创建目标ID全局占用为安全RESOURCE_BUSY/TARGET_OCCUPIED；Project内normalized_name冲突为RESOURCE_BUSY/NAME_OCCUPIED。跨Project同名合法。new create最多4096 Agent/Project（提案工程上限，真实计数不截断），历史replay不重验容量。version+1溢出为INVALID_STATE/COUNTER_EXHAUSTED；不回绕。真实变更version和updated_at各推进一次；no-op仍过当前门禁与引用校验，保存独立completed receipt＋Activity，canonical/version/Outbox不变；历史replay不Touch。

同物理final Tx必须包含Agent canonical、外域正式引用/新Agent默认assignment、幂等receipt、typed Outbox以及适用的Account Activity/Audit。拟唯一事件`agent.config_changed`，producer=`agent`、aggregate=`agent.config`、schema1；payload只含command_id、actor_user_id、created/updated、严格排序的实际changed_fields，不含instructions/description、Secret、参数、raw key或完整Capability对象。Header为同Project/Agent/version与DB时刻。新producer及Project/Audit精确闭集由对应owner在实施闭包内交付，不用普通合法TypedEvent冒充写权限。

不新增完整Agent Timeline API。命令中原input、最小preimage/postimage、不可变receipt与安全Outbox/Audit记录构成本卡恢复/必要历史；保留旧receipt不等于当前配置仍相同。外域运行snapshot/Transcript、Usage等由其owner保留，本卡编辑不热改历史snapshot。未来Project永久删除必须按各owner正式清理口删除本域数据及正文，不新增默认TTL或成功空Cleanup。

需要事务外目录/Append发现时沿两阶段：短准备Tx保存planned＋固定commandID/revision与精确依赖映射；结束后取opaque plans；final一次AcquireAll完整union并重验Actor、计划issuer/revision、preimage与每项当前引用，再原子执行。raw lock union包含重复项先≤512后Normalize；计划适用性最多三轮共享上限，外域Forbidden/SQL错误不能吞掉当replan。所有InTx回调禁止网络/Provider/解密材料/内层Begin。

Unknown保原CommitResult、writer Attempt与Cause，仅一次独立≤3秒、原Command EX下的当前Read/identity确认；completed同义才成功，planned仍Unknown/lookup，无行真串行观察可NotCommitted/retry_same_key，确认自身错误不覆盖原writer。显式Lookup用caller ctx、返回committed/in_progress/not_observed；后两种不准自动重发。只有自有Tx明确NotCommitted且ctx已取消才可返回可errors.Is的取消与零值；Committed不被delivery cancel改写。Stop关闭admission并取消已登记调用/confirmation，Drain等待实际函数/Rows/Tx结束，不关闭共享Store。

## 6. Model 与资源目录的真实接缝

### 6.1 Model 能复用什么、必须补什么

已验Model的DTO/capabilities/配置表、Owner/System鉴权、完整锁/计划算法可复用；`model/contract.ReferenceOwner`验证只证明shape，不证明Agent存在、归属、版本或Actor权限。`References`四方法目前只有声明，Resolver的direct选择也不提供Agent配置授权。F1不能直接查`agenteam_model`表，不能伪造Consumer/Execution启动运行解析以验证配置。

Model owner须在独立前置或同一已批准实施闭包提供：

1. 配置专用同Tx选择事实口：输入当前Actor、Project、ModelID、用途agent_model/approval_model、reasoning_effort；验证System或同Project scope、Provider/Model enabled chat、具体能力/effort。返回稳定Model/Provider身份及版本和安全capabilities，**不返回credential材料、不创建runtime lease、不调用Provider**。Discover必须给出所有Model/Provider/registry锁，InTx只验已持锁并重读。
2. 现有`DiscoverReference/ApplyReferenceInTx`真实Agent分支：使用已注册Agent owner的当前事实/预期版本与拟postimage witness；创建owner时验证同Tx已插入的Agent事实与planned创建identity，不能要求事务外Agent早已存在，也不能以caller任意ReferenceOwner字符串绕过。保存完整role和owner_version；字段变更使owner_version推进时，两种Model引用的版本必须同Tx一致更新。
3. Model删除替换真实跨owner回调：`PrepareDeleteModel/DeleteModelInTx`完整发现受影响Agent与owner版本；System删除仅接受合法System替代chat且所有项目均可见，Project删除仅限对应Owner。final同锁重验每个Agent模型/effort/版本并经Agent自有更新口修改canonical，同时维护Model引用、Agent version/安全事件。任一不兼容或版本变化整批回滚，不能修改Model侧索引却漏Agent。
4. 受控System替换只授予精确旧Model→已验证replacement的配置变更，**不授予Agent正文读取或普通Owner管理旁路**。以Model实例发行、绑定Actor/命令/旧新Model/受影响ID与version/锁集的opaque plan证明，Agent回调经正式Model校验口确认计划与当前System权限；禁止JSON构造计划或任意Service actor模拟Owner。替换返回数量/安全ID和版本，不含instructions。

现`ReferenceChange.Validate`仅对reranker/image允许After=nil，因此Agent从auto切到default/allow所需的approval_model移除**也是待补契约缺口**。Model前置只能为真实可选approval_model、当前Agent postimage证明的移除扩展，不笼统开放agent_model置空或所有role删除。F1始终要求非空agent_model，Agent物理删除时移除必需引用的授权归后继删除卡。

新增普通引用使用`system-config:model-references` SH；删除/跨owner替换EX。其后按User/Project/Agent/Provider/Model/record完整顺序预取，不在Agent4后补registry1，不在Model aggregate5后补Agent4。与已验Model `prepareReplacement`拒未绑定Agent的行为兼容，真实分支没有验收前保留该拒绝，不能下游直接写表绕过。

### 6.2 目录、初始化与不具备的运行能力

每类目录对每个请求identity返回typed valid/removed/disabled/not_in_scope，完整一一对应、无缺项/重复项；读取失败是error，不能“失效自动过滤”。直接Human保存引用遇任一不合法整次拒绝；只有将来的Preset复制才可按已确认失效推荐跳过，F1不实现Preset。临时Runner/MCP离线不是removed，不删稳定Capability。

Tool目录需验证具体ToolID、来源/是否普通可配置、当前Project的MCP连接与显式授权；Core Tools不进入可关闭白名单，新的MCP discovery不自动扩大已有集合。Mount目录只返回本Project/Agent的逻辑Mount/workspace，不接受任意绝对路径。SecretVariable目录只返回同Project稳定变量identity及安全metadata，普通变量没有Agent白名单；不能返回Secret value或把CredentialRef当业务变量。

D01 ToolCatalog事务外Resolve概念不足以独自支持保存后的原子删除保护。F1开工的目录接缝必须各具备事务外发现依赖、同Store callerTx内重验/维护引用、资源删除与配置更新共享gate/aggregate锁的闭合协议；查询成功后隔一个无保护窗口直接保存是不合格实现。新增typed plan/API由各owner先冻结，C1不编造这些实现。

新Agent默认Skill分配要求真实保护Add Skills资源/其当前revision及正式assignment身份，和Agent创建同final Tx落库；默认install-skill由Registry正式稳定ID解析。新Agent初始化是未发布身份的创建操作，不能拿面向既有运行Agent的“新增assignment→下一轮”语义伪造no_active_execution；若实现复用运行变更路径，则其真正AssignmentRuntimeSink也必须绑定。具体默认初始化窄口须由Skills owner在F1前置规格中冻结并证明，当前P1没有该口。

读取AgentConfig只读现有canonical，不重新取Secret/Model正文、不分配Skill、不启动Runner、不生成runtime snapshot。`inject_agents_md`仅保存配置选择；文件来源读取、Platform Prompt版本、允许Tool集合快照、初始Skill binding、Memory namespace与实时授权由D22等后继真实接通。缺occupancy不推断idle，缺Runtime不会构造可执行AgentRun。

## 7. 持久化与权限实施入口

F1拟独占`agenteam_agent`本域表：`agents`保存canonical核心与删除门禁，受控引用集合保存各外域稳定identity；`commands`保存幂等input/plan/revision/receipt。是否使用三张本域引用表须在外域类型/引用协议冻结后确定；不能用跨域FK或CASCADE代替业务授权/删除门禁。固定`UNIQUE(project_id,id)`、`UNIQUE(project_id,normalized_name)`、正version/时间顺序、枚举/数量/局部约束；已有数据升级不得重写外域数据。迁移号与精确DDL在F1开工时由root分配，此文不创建SQL占号。

锁最低要求：

| 路径 | 第一次AcquireAll必须包含；外域计划锁仍须并入 |
| --- | --- |
| GetAgent | User SH、Project SH、Agent SH；当前Owner Read后读本域 |
| RequireCurrentInTx | caller已持User SH、Project SH、Schedule EX、Agent SH，不Acquire |
| Create / 改名 | Command EX、User EX、Model registry按动作SH/EX、Project EX（名称/容量）、Agent EX、所有资源/Outbox计划锁 |
| 其它Update | Command EX、User EX、Model registry按动作SH/EX、Project SH、Agent EX、所有资源/Outbox计划锁；如所涉引用保护要求Schedule，则预先并入EX |
| Lookup | 原Command EX、User SH、Project SH，不依赖Agent当前仍存在/版本/外部目录可用 |
| 将来Agent删除 / Work引用检查 | User/Project gate、Schedule EX、Agent EX以及各required域正式完整锁；本卡不实现删除 |

构造器只验证Store/Authority同实例、typednil及必需端口/typed events注册，不访问DB或窥探外域wrapper内部Store。所有InTx外域口自行验证同Store活Tx。没有依赖时明确DEPENDENCY_UNBOUND；C1不提供假Service constructor。

## 8. 拟写域与验收

本轮已授权写域为本文及下列 C1 六文件；F1 路径仍仅为后续建议，不是现在的写权：

- C1精确六技术路径：`internal/central/agent/contract/core.go,core_test.go,reference.go,reference_test.go,codec.go,codec_test.go`。`core`定义AgentCore与合法元数据/Model标量；`reference`定义AgentRef/WorkReferences；`codec`只服务该六路径的嵌套strict raw/presence/Clone与安全输出。已有identity/Model/Foundation不改，不能添加未存在的Tool/Mount/Variable marker。必要开发README/台账同完整结果更新。
- 完整Config契约与F1尚不能列为“可立即实施闭包”：Model配置窄口与Agent引用/替换回调、Tool/Mount/Variable canonical ID/目录、Skill新Agent初始化和Agent typed producer/Audit allowlist须由各owner先给出精确文件与责任，root统一批准完整实施闭包。然后在Agent本域安排commands/reader/repository/authority/events及纯/真实PG测试和唯一新迁移；不隐含改旧Model/Work/Project/Skill、HTTP、前端或生产root的权限。
- 当前事实实现不能以仅读test-owned手种Agent代替真实创建验收；测试可种损坏数据做负例，但正向事实必须来自正式Owner创建命令及真实默认Skill/Model引用流程。

C1纯验证：每个核心字段上下限/最大escaping；UUIDv4/零/大写ID、Version overflow、枚举未知、bool缺省/null、可清字段presence、大小写别名/重复键/unknown/孤立surrogate/尾随值；嵌套和直接Unmarshal caps；Ref三字段与跨scope不混用；Clone无共享slice/pointer；fmt/slog/Fault安全投影；与既有username语法一致而Project名称占用独立。接口声明与pure构造成功不记作真实授权PASS。

C1 当前作者验证已覆盖上述纯边界，包括解码失败时接收者不变、同时最大正文的六倍 JSON escaping、完整 raw cap 以及标准 `json.Unmarshal` 裁外层空白的边界。`AgentCore/AgentRef` 的 `Decode` 入口检查完整 supplied raw；直接 fmt/slog 为固定安全投影，业务 JSON 和任意外层容器的 JSON 日志回退仍可包含正文，不宣称自动脱敏。Go 1.27.1 离线 `-p=2` 下，Agent contract 及其 Foundation、Identity contract、Model contract 的作者 pure、race、vet 均已通过；这不证明真实 Owner/Tx/Agent 初始化、引用或执行授权。

未参与实现的验证者另以五个独立纯测试检查严格 JSON、字符边界、组合上限、深复制与日志安全边界，全部通过，race 检查实际退出 0；同时核实六文件与规格输入稳定，依赖不含 Task runtime 或上层 Tool 契约。该独立接受仅覆盖 C1 纯契约，F1、真实 Agent 初始化与授权仍未验收。

F1真实PG必须逐项证明：

| 场景 | 必须观测的事实 |
| --- | --- |
| 迁移与重建 | fresh/旧版本populated升级、重跑、DDL失败原子回滚；新表约束和旧域数据不变；重建服务读同一Agent和receipt |
| 默认初始化 | 真保护Skill/默认Tool/Model引用与Agent同Tx；显式禁用保持；任意中段失败全回滚；缺目录/缺Skill/缺Registry不产生active Agent |
| 当前权限 | 两个Owner/非Owner管理员、Session撤销与续期；currentRead优先；active/archiving/archived/deleting/未初始化；历史completed先于当前版本/目录/门禁的新写检查 |
| 当前Ref与Work竞争 | foreign/ended Tx、缺每一锁、SH/EX模式、同Project真实Agent；ScheduleEX+Agent gate下Work指派与未来删除检查的独立Tx真实waiter，两种赢家后事实一致，terminal当前引用不遗漏 |
| 配置原子性 | Agent列/全部引用/Skill/Outbox/Audit/Activity/receipt每一边界注错回滚；no-op与历史replay差异；不同key同expected恰一胜，改名/同名/Project容量竞争 |
| Model引用与替换 | agent_model/approval_model新增、auto移除、owner_version一致；Model删除和Agent修改真实竞锁；System替换跨Project合法性、effort不兼容整批rollback、无正文权限扩张、未绑定owner拒绝 |
| 资源引用 | Tool/Scope/MCP/SecretVariable/Mount每一invalid结论和读取error；离线合法引用保留；资源删除vs新增引用同锁无phantom，未知目录不能变合法空集合 |
| Unknown / U1 / 停止 | 准备和final真实COMMIT未转发/已提交响应丢失；确认/Lookup与原writer独立Tx握手；原Attempt/Cause不替换，取消Lookup不假空；Stop/Drain及proxy/连接/Rows实际join |
| 历史/Runtime边界 | 编辑不改已保存receipt/外域snapshot；无slot接口时不声称idle；无生产AgentRun/Provider/Tool调用，无假Lifecycle stopped/cleanup completed |

所有并发必须用实际caller Tx PID、精确预期key/mode、granted=false与blocker握手后释放/取消，不靠sleep。独立验证者须全文STATIC并各自构造Owner撤权/Ref竞争以及跨ownerModel替换/Unknown两组真实场景；作者tests不能代替独立结论。真实资源/单top预算沿当时明确的私有PG-only fixture与所有权，未获扩展不启动Object/Runner/Provider；需要受阻真实依赖时如实BLOCKED，不削弱断言或复制空实现。离线检查每条≤45秒且GOTOOLCHAIN=local/GOPROXY=off/GOSUMDB=off。

C1 六个纯契约文件已实现并经独立验收接受；其接受只开放依赖纯类型的编译工作。F1 尚未接受，完整创建前置仍 BLOCKED，只有全部实际前置、真创建/事实/引用与独立验证通过才可供 Task assignee/reviewer 正向绑定。完整 Agent 配置、Skills/Variables、删除、Executor 与完整 D10 的未完成事实保留。

## 9. F1 核心实施边界

root 已授权隔离树中的 Agent 契约、Store、canonical writer、Authority、命令服务及唯一迁移 00032；本节更新 §2/§8 的历史开工范围，不把前置计划或源码接受变成完整 F1 验收。C1 六源不修改。首片段新增完整三类引用配置、创建 Skills/Tool 的 consumer-owned 窄口及命令身份，Registry 与 Skills 直接消费这些低层契约，Agent 不导入 ToolSpec 或外域实现。

本域仅写 `agenteam_agent.agents`、三张 canonical 白名单和 `commands`。外域反向引用/Skills assignment 仍由其 owner 写。planned command 持久绑定原稳定命令、revision、完整创建 absent/更新 preimage、完整 postimage 与两个默认选项；唯一私有 writer 在原活 Store Tx 检查并实际写入全部 canonical 后才产生私有 context witness。Skills、Tool 与 Secret 回调同时重核原命令、完整 Actor、same Tx/Authority、完整锁和当前 postimage；公开 DTO、已存在行、空集合或 false 均不替代该见证。

首片段仅格式/静态准备，未执行 Go、迁移或真实授权。服务、严格 Create/Update/receipt/Lookup codec、实际完整 provider 调用链与必要测试仍在实现。Model 双角色引用、Secret 引用、Mount 初始化/引用及 Agent typed Outbox/Audit 闭集尚未全部提供；缺任一 required provider 必须 `DEPENDENCY_UNBOUND` 且不提交。默认两个 bool 省略 true、false/空集仍核真实依赖；不提供 fake `no_active_execution`。未接 App/HTTP/Work 正向绑定，不解既有 Object STOP。
