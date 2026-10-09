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

领域未来若需要本包短名，只能写 `type ToolID = identity.ToolID` 等 **alias**；不得另写 `type Tool struct{}`、`type ToolID identity.ToolID` 或用字符串来形成第二种 canonical 身份。Alias 不代表该领域获得其它域的事实管理权。跨数据层或 wire 显式 parse 保持所属 marker；不能因底层字节相同就把 ToolID 转换为 MountID、变量 ID 或 AgentID。

## 3. 字节语义、身份连续性与事实责任

R1 直接继承既有 Foundation 的 36 字符小写连字符 UUIDv7、非零、版本与 variant 验证，输出 JSON string，拒绝 UUIDv4、全零、大写 hex、非法 variant、JSON number/bool/null/对象/数组及尾随值。Foundation 的现有标量入站处理与出站规范化保持原样；不在三个 alias 上伪造新方法或改变已验标量规则。容器的 key 闭集、presence、duplicate/surrogate、正文与整体 raw cap 仍归持有它们的正式 DTO codec，不能拿 UUID parser 当完整对象 schema。

ID 只表示稳定关联，不编码 Project/Agent/scope、资源类别状态、可用性、授权、版本、Secret 标记或路径。生成或 Parse 成功不注册对象，也不证明数据库有该行；合法 UUID 字符串从不成为 bearer grant。ID 按值复制，没有切片或 pointer 共享状态；字符串输出可作为经授权的安全关联，但不能根据 ID 形状断言资源属于某 Project。

| 对象 | 稳定关系由哪个 owner 维护 | 本卡不能替代的证明 |
| --- | --- | --- |
| Tool | D18 Registry 维护 ToolID 与 stable_key 的 canonical 映射；D20 提供 MCP 来源和 Project Connection 事实。定义变化按 `(ToolID,spec_revision)` 保存 immutable revision | 不能从 stable_key hash/模型可见名临时生成 ToolID；同 key 重启后是否仍对应原 ID 由真实持久 Registry 证明。具体可调用 Tool、scope、Connection 授权与当前 binding 不在 ID 中 |
| Mount | D15 Runner/Mount 目录维护具体 AgentMount 身份，D16/D17 维护 workspace 与协议/运行事实，D10 经正式端口配置关系 | 相同 Runner、workspace 名或物理目录不证明是同一个 Mount；目录必须核同 Project/目标 Agent。offline 不是 removed；移除配置不隐式删除宿主目录或宣称进程停止 |
| Project Variable | D10 Variables 拥有 ProjectEnvironmentVariable 记录及 `type`、name、描述和普通值；Secret 加密、引用/lease 材料按 D04 正式端口处理 | Secret 覆盖值不换变量 ID，name/SecretMaterial/CredentialRef 不替代变量 ID。Agent 白名单中的合法 UUID 仍须验证当前同 Project、type=secret、未删除及引用保护 |

资源创建、删除重建、rename、type change 的具体命令/版本/引用策略归领域开工卡。本卡仅保留已定的值更新稳定引用与来源身份含义，不发明变量类型切换 API、Mount 跨 Agent 转移或 Tool key 迁移规则。上述领域缺真实端口时不能返回空目录、默认 valid、无引用或可执行。
