# D01 Tool、Mount 与项目变量的低层身份

状态：**rev0 纯契约工程草案，未接受、未实施**。本轮只新增本文，不修改已交付 Agent C1、Task、现有契约、迁移或其它规格。拟结果 R1 只解决三个 canonical ID 的 Go 定义位置与唯一性；没有目录、实体事实、授权、初始化、引用写入或运行适配。任何后续实施须先完成独立 SPEC 审查，再分配精确写域；本文不分配迁移号。

## 1. 问题、依据与已确定语义

[D01 分层](d01-contracts/README.md#代码依赖与运行时依赖)规定 `agent/contract` 属层 4、`tool/contract` 属层 5，Agent 不能为保存 Tool 引用向上 import Tool。已正式交付的 [Agent C1](d10-agent-configuration.md)刻意只提供 17 键 AgentCore 与当前身份端口声明；完整配置的三类引用数组不在 C1 中。R1 提供能够被这些层共同消费的标量身份，不增加第四套配置或目录模型。

| 身份 | 已有正式来源 | 已确定的含义 | 当前 Go 事实 |
| --- | --- | --- | --- |
| Tool | [D01 ToolSpec](d01-contracts/model-tool.md#toolspecbinding-与可信调用)、[Stable Tool Identity](../../architecture/tool-system/tool-definition-registry.md#1-stable-tool-identity) | `tool_id` 是 UUIDv7 数据库身份；`stable_key` 表达 `builtin:<name>`、`runner:<name>`、`mcp:<config_id>:<remote_name>`。Tool revision、模型可见名称与临时在线状态都不替代 identity | 尚无 Tool canonical marker 或 `tool/contract` 包；Audit、Artifact、Secret 中已有受校验的字符串关联字段，它们不是 Tool 目录 |
| Mount | [Agent Mount](../../architecture/runner/agent-workspace.md#3-agent-mount)、[D01 目录](d01-contracts/resources-skills.md#agent-配置与真实目录) | Mount 是 Agent 到系统级 Runner 的配置关系，携逻辑 workspace；归属指定 Project/Agent，不是 RunnerID、workspace 名或宿主绝对路径 | 尚无 Mount marker、正式 Mount 目录或 `runner/contract` 实现；RunnerProtocol 不得 import Central 业务类型 |
| Project Variable | [变量数据模型](../../architecture/project-work-management/project-environment-variables.md#2-数据模型)、[Secret 白名单](../../architecture/project-work-management/project-environment-variables.md#52-secret-白名单) | 一个 ProjectEnvironmentVariable 具有稳定 `id` 与 `type=variable\|secret`。普通变量全 Project Agent 可见；只有 Secret 分支进入 Agent 白名单。Secret 覆盖 value 保持同一个变量 ID | 尚无 ProjectVariable marker、真实业务目录与白名单引用保护；现有 Secret CredentialID/CredentialRef 是受保护凭据身份及引用，不是项目变量 ID |

[D01 标量](d01-contracts/foundation.md#标量与编码)已经固定业务 `ID<K>` 为非零 UUIDv7；本卡据此提出三个具体 marker 的工程声明，不把尚未冻结的资源 DTO、scope 字段或生命周期写成已接受。Tool 的 UUID 与 stable key 分离也已由 D01 收敛，无需重新决定是否改用字符串主键。

实际核对入口为 `internal/central/identity/contract/identity.go`、`internal/central/foundation/id.go`、`internal/central/secret/contract/types.go`、`internal/central/audit/contract/types.go`、`internal/central/artifact/contract/types.go` 与 `internal/central/agent/contract/{core,reference}.go`。现 identity 层已定义 User、Session、Project、Agent、Execution marker；它们不实现各实体服务。现有 Skill 使用低层 canonical SkillID 的 alias，也说明声明位置与领域事实所有者可以分开，但本卡不迁改 Skill。

## 2. 拟独立 R1 与精确 Go 声明

R1 选择现有层 1 `internal/central/identity/contract` 作为三个 canonical marker 的唯一定义位置，不新增依赖层，不把领域实体塞进 Foundation。

拟新增 `resources.go`：

```go
package contract

import "github.com/LunaDeerTech/agenteam/internal/central/foundation"

type Tool struct{}
type Mount struct{}
type ProjectVariable struct{}

type ToolID = foundation.ID[Tool]
type MountID = foundation.ID[Mount]
type ProjectVariableID = foundation.ID[ProjectVariable]
```

这六项声明是本卡的工程提案，当前不存在可消费实现。R1 不增加 `ToolRef/MountRef/VariableRef` DTO、`ResourceKind` 泛型资源 union、Catalog 接口、Actor、Scope、ServiceName 或 AccessGrant。三类业务互不赋值，仍各自调用现有 `foundation.NewID[K]/ParseID[K]`；不包装第二个 ID 生成器、parser、JSON codec、Clone 或 formatter。

变量仅用一个 `ProjectVariable` marker，普通与 Secret 的区别属于当前 canonical 记录的 `type`，不是两个可绕过类型检查的 ID namespace。不声明新的 `SecretVariable` marker，也不把 CredentialID alias 成 ProjectVariableID。未来 D10 完整配置可将现占位名称 `SecretVariableID` 收敛为 `identity.ProjectVariableID`，保留 `allowed_secret_variable_ids` wire 键，并由真实目录校验每项确属同 Project 的 Secret。此项尚待本卡接受及 D10 owner 同步后才能消费，本文不原地修改已交付 AgentCore。

领域未来若需要本包短名，只能写 `type ToolID = identity.ToolID` 等 **alias**；不得另写 `type Tool struct{}`、`type ToolID identity.ToolID` 或用字符串来形成第二种 canonical 身份。Alias 不代表该领域获得其它域的事实管理权。跨数据层或 wire 显式 parse 保持所属 marker；不能因底层字节相同就把 ToolID 转换为 MountID、变量 ID 或 AgentID。这里禁止的是业务误用：Go 对相同底层形状可能允许显式类型转换，本卡只要求不同 marker 不可隐式赋值，不宣称语言能阻止所有强制转换。

## 3. 字节语义、身份连续性与事实责任

R1 直接继承既有 Foundation 的 36 字符小写连字符 UUIDv7、非零、版本与 variant 验证，输出 JSON string，拒绝 UUIDv4、全零、大写 hex、非法 variant、JSON number/bool/null/对象/数组及尾随值。Foundation 的现有标量入站处理与出站规范化保持原样；不在三个 alias 上伪造新方法或改变已验标量规则。容器的 key 闭集、presence、duplicate/surrogate、正文与整体 raw cap 仍归持有它们的正式 DTO codec，不能拿 UUID parser 当完整对象 schema。

ID 只表示稳定关联，不编码 Project/Agent/scope、资源类别状态、可用性、授权、版本、Secret 标记或路径。生成或 Parse 成功不注册对象，也不证明数据库有该行；合法 UUID 字符串从不成为 bearer grant。ID 按值复制，没有切片或 pointer 共享状态；字符串输出可作为经授权的安全关联，但不能根据 ID 形状断言资源属于某 Project。

| 对象 | 稳定关系由哪个 owner 维护 | 本卡不能替代的证明 |
| --- | --- | --- |
| Tool | D18 Registry 维护 ToolID 与 stable_key 的 canonical 映射；D20 提供 MCP 来源和 Project Connection 事实。定义变化按 `(ToolID,spec_revision)` 保存 immutable revision | 不能从 stable_key hash/模型可见名临时生成 ToolID；同 key 重启后是否仍对应原 ID 由真实持久 Registry 证明。具体可调用 Tool、scope、Connection 授权与当前 binding 不在 ID 中 |
| Mount | D15 Runner/Mount 目录维护具体 AgentMount 身份，D16/D17 维护 workspace 与协议/运行事实，D10 经正式端口配置关系 | 相同 Runner、workspace 名或物理目录不证明是同一个 Mount；目录必须核同 Project/目标 Agent。offline 不是 removed；移除配置不隐式删除宿主目录或宣称进程停止 |
| Project Variable | D10 Variables 拥有 ProjectEnvironmentVariable 记录及 `type`、name、描述和普通值；Secret 加密、引用/lease 材料按 D04 正式端口处理 | Secret 覆盖值不换变量 ID，name/SecretMaterial/CredentialRef 不替代变量 ID。Agent 白名单中的合法 UUID 仍须验证当前同 Project、type=secret、未删除及引用保护 |

资源创建、删除重建、rename、type change 的具体命令/版本/引用策略归领域开工卡。本卡仅保留已定的值更新稳定引用与来源身份含义，不发明变量类型切换 API、Mount 跨 Agent 转移或 Tool key 迁移规则。上述领域缺真实端口时不能返回空目录、默认 valid、无引用或可执行。

## 4. 下游消费与目录分层仍未完成

R1 若被接受并交付，生产依赖方向将是：

| 消费者 | ID 的允许来源 | 仍须由责任卡闭合的内容 |
| --- | --- | --- |
| Agent 完整配置纯契约，层 4 | `identity.ToolID`、`identity.MountID`、`identity.ProjectVariableID`，层 1 | 完整配置与命令 DTO 的 accepted schema、presence、三类数组和整体 cap；不得原地给已交付 AgentCore 添加键 |
| Tool Registry / Tool contract，层 5 | 相同 `identity.ToolID`；必要时仅作 alias | ToolSpec、immutable revision、stable_key 与绑定/Connection/lease 的真实服务；低层身份不引入第二份 Registry |
| Runner/Mount contract，层 3 | 相同 `identity.MountID` | 真 AgentMount canonical、当前归属、Agent gate/目录与 runtime 有效性；workspace/协议 DTO 不由 R1 代拟 |
| Project Variables 领域 | 相同 `identity.ProjectVariableID` | D10 的普通/Secret record、type=secret 校验、D04 凭据映射、原子引用保护及删除责任；其业务 contract 位置由 D10 后继卡冻结 |
| Audit / Artifact / Secret 等现有消费者 | 保留已验字段，未来正式 adapter 可显式 Parse 到 canonical ID | 不把旧 `ToolID string` 关联字段自动变成 Registry 读取权；任何公开签名调整单独核兼容、调用方和验收 |

**ID 位置闭合不等于完整目录依赖闭合。** 即使已有 `identity.ToolID`，Agent 也不能 import 层 5 的 ToolSpec、RegisteredTool 或 ToolCatalog 返回 DTO。可行的后续工程方向是 Agent 拥有自己所需的窄目录投影/服务出口，Tool provider 或 Central 组合根按下向依赖提供适配；Tool→Agent 契约允许，不代表 adapter、当前权限或持久事实已经绑定。R1 不冻结这套返回 DTO、方法或 fake provider。

Mount 的未来 `runner/contract` 属层 3，可被 Agent 向下消费，但它当前没有真实目录实现。变量业务目录也不能因为 ID 放在 identity 层就把实体、Secret 材料或当前授权移入该层；其 DTO 与窄端口的位置、调用方向和同 Tx 保护仍由 D10 所有者冻结。同层域不因共同使用 ID 就获得隐式互相 import 的许可。

Runner 两端通信继续使用自己的 `internal/runnerprotocol` wire schema，不能 import `internal/central/identity/contract`。Central adapter 按同一规范 UUID 字符串进行显式、可失败的编解码；协议 schema、版本和信任校验由 D15–D17 开工规格负责。本卡不为共享 Go marker 修改 RunnerProtocol，不把协议里的原始字符串当可信 Central 事实。

## 5. 兼容、引用与仍缺的能力

R1 是向现有 identity 包添加独立声明，没有已有这三种 Go marker 需要迁移，也没有新 DB、event schema、HTTP wire 或历史数据。它不重写 Agent C1 的 17/3 键，不放宽已有 DTO，不把旧 Audit/Secret/Artifact 的字符串字段改名或重编码。既有 Model `model.Tool` 是模型调用投影，并不是 Registry Tool marker，不迁入 identity，也不能拿它生成 Registry ToolID。

Tool stable key 继续由 Tool/MCP owner 维护。`builtin:...`、`runner:...`、`mcp:...` 不能通过 `ParseID[identity.Tool]`；调用者只有 key 或 model-visible name 时，必须通过正式当前 Registry/执行映射找到 UUID，不能截断、大小写合并、hash 成 UUID 或生成新 ID 代替查询。新 discovery 不自动扩大 Agent allowlist，临时离线不重编号；这些既定规则与 R1 保持独立。

ProjectVariableID 也不证明资源是 Secret：同样的标量类型可以指普通变量。未来 Secret 白名单写入必须由 Variables owner 在活 caller Tx 中证明同 Project、当前 type=secret 及合法引用，不能靠一个 `SecretVariableID` 别名或解析函数为请求颁发证明。Secret 覆盖值与 Credential lease 的关系、MCP Connection 的独立凭据用途及删除/轮换责任保持现有设计；R1 不返回 SecretMaterial 或扩大 Agent 对 MCP 凭据的使用权限。

F1 的真实目录仍须各自提供完整请求 ID 对应的 `valid/removed/disabled/not_in_scope` 结论、同 Store 活 Tx 重验与原子引用维护，以及资源删除同锁下的检查/清理协议。精确 lock union、issuer/计划适用性、当前 Owner/Project gate、错误优先与生命周期门禁由对应实施卡冻结并真实验证；低层标量没有方法可以证明这些事实。F1 缺必需目录、Skill 初始化或 Registry 时明确失败，不能用空数组输入或合法 ID 绕过必需 provider。occupancy 只在后继确实需要该事实的删除或运行命令中作为前置；R1 与本卡所衔接的 F1 配置创建/更新不因此新增占用目录依赖。

尚缺能力按责任保留：

| 后续责任 | R1 之后仍未就绪的事实 |
| --- | --- |
| D18 / D20 | 真实 Tool 注册、canonical key 映射、MCP scope/连接目录、Agent 引用维护与删除保护；Agent 可消费的安全窄投影及组合 adapter 尚未冻结 |
| D15–D17 / D10 | AgentMount 的创建、当前归属与引用、workspace 映射、当前 runtime 校验；实际 offline/removed 不混淆，Mount 删除不隐式清磁盘 |
| D10 / D04 / D20 | Project Variable canonical、Secret 子集目录、Agent 白名单与 MCP 凭据的各自引用保护、真实轮换/删除及运行 lease |
| D10 / D09 / Skills | 完整 Agent 配置、Model 选择/引用替换、新 Agent 默认 Skill/Tool 初始化和真实 WorkReferences；R1 不解锁 Agent F1 正向或 Task 指派 |

这些是已定事实责任下的实现/组合缺口。本卡无需重新打开 UUID 编码、Tool stable key、普通变量可见性、Secret 白名单或 Mount trusted-host 的产品决定。变量 type change、Mount 转移等本卡不提供的命令也不阻塞这三个纯 ID 的定义；后继若确需该业务入口，再在所属卡按既有规则明确其影响。

Object runtime join、OpenAI tools 独立验收、Central SPA concurrent-publication、Jina/Image 来源停止线保持原状。R1 不启动这些工作，不使用外网、Provider、PG、Object、Runner 或目录资源，也不生成成功占位实现。

## 6. 可独立实施闭包与验收

当前实际写域仅本文。若本规格接受，R1 拟定的产品/测试源闭包精确为两文件：

1. `internal/central/identity/contract/resources.go`：仅 §2 六项声明与清晰注释，直接依赖 Foundation；不改旧 identity Actor/Scope、现 Foundation 或其它域。
2. `internal/central/identity/contract/resources_test.go`：新 marker 的消费、区分与继承标量边界测试；使用现 Foundation 生成/解析，不复制 parser、registry、authority、fake catalog 或可执行资源 fixture。

同一结果的必要文档同步由已分配 owner 处理：本卡状态；D01 索引中低层身份声明责任；D10 F1 文稿中 `SecretVariableID` 占位名与 ToolID 层级缺口的精确后续状态；后端 README/任务台账仅记录实际纯结果。它们的精确路径是 `docs/development/work-items/d01-resource-identities.md`、`docs/development/work-items/d01-contracts/README.md`、`docs/development/work-items/d10-agent-configuration.md`、`docs/development/backend/README.md`、`docs/development/agent-team/tasks.md`。这些同步不提前成为本轮写权，root 在实施派工中登记唯一作者；没有服务、go.mod、新依赖或迁移号。

纯实现验收必须区分以下事实：

| 检查 | 可判定结果 |
| --- | --- |
| canonical 唯一性 | 三种 marker 只在 identity 定义；新源仅依赖 Foundation。未来域短名使用 alias，示例消费者能原样传入/返回对应 canonical ID；不能另创同名 marker 凑编译 |
| 类型区分 | ToolID、MountID、ProjectVariableID 彼此以及与已有 AgentID、CredentialID 的反射 Go 类型不同、不可隐式赋值；相同规范 UUID 字符串可以分别解析为不同 marker，不据此产生业务转换许可 |
| 现标量复用 | 每一种的 zero/UUIDv4/非法 variant/大写/错误长度/非法字符拒绝；Parse/NewID、Text/JSON 成功输出相同规范 UUID，数值、null、对象/数组、坏 Unicode 与尾随值拒绝；不能把 stable key、变量名或路径当 ID |
| 失败原子性与值语义 | 已赋值 receiver 的失败 UnmarshalText/UnmarshalJSON 不改变旧值；成功值复制无共享状态。错误不回显原始输入；不重造错误体系或附带业务 grant |
| 下游可消费边界 | 外部测试包可复用已交付 C1/Secret 的稳定身份进行类型核对，不改它们的公开类型；identity 生产文件不反向 import Agent、Secret、Runner 或 Tool。只做纯类型消费，不构造假目录/事实方法 |
| 回归与静态分层 | 已有 identity Actor/Scope/ServiceName 闭集与 C1 codec 原样通过；实际导入图仍无反向领域依赖。单个 marker 编译成功不外推整个 F1 或任何资源服务 ready |

不为 R1 新增 DTO raw cap：Foundation 标量规则保持原样，后续完整 DTO/请求继续负责总字节限制、集合排序去重、presence 与逐层 schema。无需为只增三组 marker 重复整份 UUID 算法测试；保留能区分错误 alias、错误 marker、输入宽化及失败覆盖 receiver 的针对性反例。

实施后的作者执行 Go 1.27.1 离线 `GOTOOLCHAIN=local/GOPROXY=off/GOSUMDB=off`、`-p=2` 的准确包级 pure、race、vet；每条命令 ≤45 秒，实际 Wait/exit。最小范围为 Foundation、identity/contract、已交付 agent/contract，以及新外部测试直接引入的 secret/contract；根据实际差异扩展，不运行无关真实资源测试。独立验证者对冻结输入静审声明/依赖方向，并自行构造类型区分、下游 alias 与负例输入/receiver 保留验证；作者结果不代替独立接受。

## 7. 接受与后续边界

R1 的完成条件是：本卡正式接受、唯一 canonical 声明与针对性纯测试实现、作者检查及独立验证通过、必要文档在同一结果交付。当前仅形成待审规格，没有产品代码、动态测试或真实资源结果。

即使 R1 全部通过，它只解除 Agent 等消费者对三类 **ID 标量**的定义位置障碍。完整 AgentConfig、Tool 目录投影、Variables/Mount 目录、创建默认初始化与原子引用保护仍各有真实前置，不会因本卡接受自动取得写权、迁移号、生产绑定或 ready 结论。既有 Agent C1 与 Task 结果保持其原验收边界。
