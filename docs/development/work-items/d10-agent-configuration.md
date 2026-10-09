# D10 Agent 配置与当前身份事实

状态：**rev0 草案，未接受、未实施**。本轮只编写本文件，没有 Agent 产品代码、迁移、动态验证或生产绑定。正文中的新类型、接口、字段与工程上限都是待独立 SPEC 审查的提案，不因被其它草稿引用而成为已验能力。Agent Management 尚无实现包；现存 `identity.AgentID` 只是 typed identity。本文不占用迁移号，进入真实持久实施前由 root 根据当时全局序列分配。

本卡拟分成两个可分别验收的结果：**C1 纯 Agent 配置与当前身份端口契约**可在规格接受后先行；**F1 Human Owner 创建、读取、修改真实 Agent 配置，并提供同 caller Tx 的当前身份事实**必须等本卡列出的真实前置闭合后开工。F1 不接受 SQL 手种 Agent、默认成功目录或未绑定初始化作为生产创建路径。C1 完成不解锁 Task 指派；F1 完成也不等于 Executor、Agent 删除、完整 D10 或平台 ready。

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
| Secret / 项目变量 | D04 Secret 加密、凭据/lease 等限定能力已交付 | **项目 SecretVariable 业务目录及 Agent 白名单引用口未实现**；`CredentialRef` 不能冒充 `SecretVariableID` |
| Task / Work | Structure 已验；Task Planning 契约已验，runtime 当前仍在验收 | 本卡不将 Task runtime 候选算已接受。assignee/reviewer 当前引用及状态转换是后继 Work 的事实责任 |
| Execution / Dispatch / Meeting / Memory / Governance | D01 端口与架构责任存在 | **实际 Agent slot、活动/历史占用、运行 snapshot、Memory namespace 清理等未绑定**；不实现 Agent 删除或把这些域报成无引用 |
| Outbox / Audit | 已有 typed planned Append、同 Tx 与当前 Project gate 等前置 | Agent 自己的 typed producer/Audit 业务类型仍须正式实现与验收；合法 event DTO 不构成 producer 权限 |

源码核对入口：`internal/central/model/contract/{configuration,references,authority}.go`、`internal/central/model/{references,resolution_policy,configuration}.go`、`internal/central/project/contract/lifecycle.go`、`internal/central/foundation/lock.go`、`internal/central/skill/contract/{types,read}.go`。此表是库能力核对，不是本轮重新执行全部历史测试的声明。

Object runtime join、OpenAI tools 独立验收、Central SPA concurrent-publication、Jina/Image 来源停止线保持原状；本卡不修复、重新发起或以新测试恢复它们。真实 Skills/Object 组合仍受原阻塞约束。

## 2. 结果边界与分段门槛

### 2.1 C1：可独立先交付的纯契约

仅新增 `internal/central/agent/contract` 的 DTO、严格 codec、枚举、摘要、Clone、当前事实与引用端口声明及纯测试。不创建 Store、service、adapter、HTTP、生产 root、迁移、目录数据、默认 Agent 或 fake authority。复用 `identity.AgentID`，不定义第二种 Agent marker。

C1 需要闭合本文的类型、presence、错误顺序、锁要求、删除阻挡责任和 Model 引用接缝。接口值可以供调用方编译依赖；零值、typed nil、缺失依赖的行为必须明确失败。不得提供返回 active、free、initialized、空引用或成功 mutation 的默认实现。

### 2.2 F1：真实有限事实能力，当前前置未齐

拟提供 Human Owner 的 `CreateAgent/GetAgent/UpdateAgent/LookupAgentCommand`，以及 `WorkReferences.RequireCurrentInTx`。只做直接配置；Preset 复制、运行时授权、Skill 动态分配、Agent 删除/停用/恢复、Project lifecycle participant、HTTP/UI/App root 不在 F1。

F1 开工前必须已有可供组合的 Model 配置验证与 Agent 引用写入/替换、保护 Skill 默认分配、普通 Tool Registry 的 install-skill 身份、所支持 Capability 的真实目录及其引用保护。Agent constructor 不接受缺口端口，不能以“本次传空数组”为由注册假的全域目录。某类配置若拟另分结果，必须先收窄受支持命令/DTO与本卡验收范围并再经 SPEC 审查，不能实现时静默丢字段。

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
  allowed_secret_variable_ids: SecretVariableID[],
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
| name | 1–63 ASCII 字节，`[A-Za-z0-9](?:[A-Za-z0-9-]{0,61}[A-Za-z0-9])?`；保留输入大小写，normalized_name 仅 ASCII lowercase，Project 内唯一；保留字 `new,settings,api,debug` 按 normalized_name 比较 |
| display_name | null 或 1–256 Unicode scalar、≤1024 UTF-8 字节，至少一个非空白，无 Cc；不 trim/NFC，空字符串不当 null |
| tag_color | null 或规范小写 `#[0-9a-f]{6}`；展示用途，不映射权限/角色 |
| description | 0–8192 UTF-8 字节；允许 TAB/LF/CR，其余 Cc 拒绝，保持原字节 |
| instructions | 0–32768 UTF-8 字节，控制字符规则同 description；不是 Secret 存放渠道 |
| reasoning_effort | null 或 Model contract 所接纳的非空安全 token，≤32 ASCII 字节；目录必须核能力闭集，不能只做格式检查 |
| 引用数组 | 必有非 null、各≤256、无重复、元素规范非零小写 UUIDv7；canonical 按 UUID 字符串升序，输入若非升序先严格拒绝，不通过去重掩盖调用者错误 |
| version / time | version≥1、int64 上界，wire 为规范十进制字符串；UTC 微秒，created_at≤updated_at；没有可写客户端 version/time/lifecycle |

全体对象包括嵌套 Capability、私有持久 request/plan、历史 payload，逐层拒绝未知 key、大小写别名、重复 key、null 越界、缺必有字段、非法 UTF-8、孤立 surrogate、尾随值、错误标量类型。不能只用 `DisallowUnknownFields` 或 canonical-v1 代替 schema。请求/单个 Agent/单 receipt cap 建议512KiB，Lookup request16KiB，safe history/typed event payload16KiB，私有计划4MiB。每个 codec 检查其方法实际收到的完整 raw；标准 `json.Unmarshal` 裁外层空白的行为单独测试。正文最坏 escaping 与256项每类引用合计仍须经实测证明低于 cap。

全部导出 DTO 提供 Validate、严格编解码与 Clone，pointer/slice/maps 深复制。fmt/slog 使用固定 `agent_config` 等安全投影，Fault wire 不含正文、Secret、command key、SQL 或内部 cause；外域纯 DTO 的安全格式化边界不得靠反射猜测私有结构。显式授权 JSON 才是业务传输通道。
