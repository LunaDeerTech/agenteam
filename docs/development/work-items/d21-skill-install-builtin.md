# D21 install-skill Builtin v1

状态：**工程契约已固定，等待真实 Service 和 Agent 执行授权接线；尚未实现或验收可执行 Builtin**。本卡只固定 `text_files` 来源的新建调用，不缩减 [D01 统一安装](d01-contracts/resources-skills.md#skill-类型与统一安装)中后续受控文件、Runner 包和显式更新的产品范围。默认启用且可按 Agent 禁用保持不变。

## 1. 依据、范围与责任

依据 [Agent Skills](../../architecture/agent-skills.md#3-内置-add-skills-与统一安装)、[D18 Registry](d18-tool-name-projection.md#10-registry-配置前置实施中)、[D01 ToolSpec/可信调用](d01-contracts/model-tool.md#toolspecbinding-与可信调用)、[默认风险规则](../../architecture/security-governance/default-approval-policy.md#3-risk-分类)与 [Skill 包规则](d10-skills-initialization-design.md#9-p1-固定编码与纯-go-口)。工具只向当前 Project 技能库发布不可变包，不自动分配、不执行脚本、不下载 URL、不安装依赖。

| 提供方 | 调用与所有权 |
| --- | --- |
| D10 Skill | 真实安装 Service、同一包验证器、对象发布/恢复、当前领域授权及幂等记录；UI 和 Builtin 共用，不各建发布流程 |
| D21 Builtin | 严格参数解析、显式结果投影、绑定到该 Service 的 handler、ScopeResolver、RiskClassifier 与 Registry source |
| D18 Registry/Runtime | 正式稳定 ToolID/immutable spec、原 Operation/Attempt、schema 校验、同 Operation 恢复和结果处理；不在 Registry 执行业务安装 |
| D19/D22 | 当前 AgentRun、固定 Execution Tool Set/Policy、授权/等待，以及真实 Operation 与调用身份；缺失时拒绝，不伪装 Human Owner |
| D05/D15/D17 | D05 提供真实对象能力；以后新增 Runner 来源须另接当前 Mount、System Runner ACL、原执行权限与直传，v1 不调用 Runner |

已有 `skill-install` 候选 `748ac4a7` 只提供 `NewInstallRequest(ctx, SkillID, Package)`、typed `InstallReceipt` 与私有 `planInstallation` 等基础；原 pure 通过不等于完整发布。其 `installationActor` 对 AgentRun 明确返回 `DependencyUnbound`。以下公开调用及 Agent 授权是下一实现目标，不把私有计划行、Human 成功或非 nil 接口当作 Backend 已绑定。

## 2. 唯一注册描述

| 字段 | 固定值 |
| --- | --- |
| `Definition.StableKey` | `builtin:install-skill` |
| `Definition.Name` | `install-skill` |
| `Definition.Description` | `Create a new skill in the current project from a validated text-file package. Does not assign the skill or execute its contents.` |
| `BuiltinBinding.HandlerID` | `skill.install` |
| `BuiltinBinding.ContractRevision` | `1` |
| `ScopeResolverID` | `skill.install.project.v1` |
| `RiskClassifierID` | `skill.install.publish.v1` |
| `Class` | `ordinary` |
| `Annotations` | `read_only=false, destructive=false, idempotency=keyed` |

`Active=true` 只在下述真实绑定检查通过时返回。`tool_id` 仍由 Registry 首次真实注册生成；`spec_revision` 由 Registry 按完整定义分配，不能用 handler contract revision 冒充。模型实际调用名沿 D18 NameTable 的 `tool_<uuid>`，不以 display name 反解。

默认启用是新 Agent 配置的既定规则：通过真实 Registry 解析此稳定 key 的 ToolID，写入正式 allowed tools；显式禁用保留。它不是 Core Tool，不添加第四个 ToolSpec annotation，也不让已有 Agent 在注册/升级时自动扩权。

source 固定在组合时，以 `registry.BuiltinSource` 的 `DescribeInTx`/`CheckBindingInTx` 使用同 Store 原 caller Tx。检查对象必须是实际 Skill Service/Authority 和这三个精确代码绑定；真实 Agent 安装授权提供方也须已接入。该检查证明实现身份与依赖关系，不在注册时伪造某个运行中 Agent 的许可。禁止对象 I/O、补锁或安装试调用；当前请求权限由每次调用另验。缺依赖返回 `DependencyUnbound`，不注册占位 handler；同一 spec 绑定不得悄悄更换成别的 Service 或 schema。

## 3. 模型 input_schema

JSON Schema 方言固定为 2020-12。以下为 v1 完整 input_schema，不支持的 Provider 投影应返回 `SCHEMA_UNSUPPORTED`，不删约束。D18 的 canonical JSON 编码检查本身不是 schema validator。

```json
{
  "$schema": "https://json-schema.org/draft/2020-12/schema",
  "type": "object",
  "additionalProperties": false,
  "required": ["source", "mode"],
  "properties": {
    "source": {
      "type": "object",
      "additionalProperties": false,
      "required": ["kind", "files"],
      "properties": {
        "kind": {"type": "string", "const": "text_files"},
        "files": {
          "type": "array",
          "minItems": 1,
          "maxItems": 128,
          "items": {
            "type": "object",
            "additionalProperties": false,
            "required": ["path", "utf8_text"],
            "properties": {
              "path": {"type": "string", "minLength": 1, "maxLength": 1024},
              "utf8_text": {"type": "string", "maxLength": 1048576}
            }
          }
        }
      }
    },
    "mode": {"type": "string", "const": "create"}
  }
}
```

整个原 arguments JSON 最大 **1 MiB**，与现 Model ToolCall 输入上限一致；在分配无界对象前计量。只接受一个 UTF-8 JSON 对象，拒重复键、额外值、非法 Unicode/孤立 surrogate，不 trim、修正文稿或静默忽略字段。Schema 的 `maxLength` 是字符数，领域仍按 UTF-8 字节执行原限制，不把两者混同。

解析后只经 `sc.NewTextFiles` → `skill.BuildPackage` → `skill.NewInstallRequest`。沿唯一验证器：路径 ≤1024 字节、相对 `/` 分隔、拒空/点段、绝对路径、反斜杠/冒号/控制符及 NFC+case-fold 碰撞、文件/祖先冲突；精确根入口 `SKILL.md` 必有，≤256 KiB，其严格 frontmatter/非空正文规则不改。附件允许空文本，所有文本拒 NUL/CR，保留合法原字节。沿原 128 文件、单文件 8 MiB、合计 32 MiB 上限及 canonical ZIP v1；Tool 的 1 MiB arguments 门更早限制本路径，不扩大包服务。包构建保留现 caller 剩余与 2 秒最短校验预算，不给整个 Operation 添加统一 timeout。

模型不传 `project_id/agent_id/execution_id/operation_id/skill_id`、幂等 key、revision、审批、Runner/宿主路径、ObjectID/MinIO key/URL 或凭据。名称与简介来自经验证的 `SKILL.md`，不再添加可与包内事实冲突的第二组字段。`update`、`controlled_file`、`runner_package` 在此 revision 均为非法参数，不降级为 create；后继须扩真实 Service/来源授权和新 immutable spec，不改变已固定的旧 Execution 定义。

## 4. Service 调用与幂等

消费口固定为以下公开 Service 目标，须由 Skill owner 实现；本卡不声明现源码已有该方法：

```go
func (s *skill.Service) Install(
    ctx context.Context, actor id.Actor, meta f.CommandMeta,
    project id.ProjectID, request skill.InstallRequest,
) (skill.InstallReceipt, error)
```

1. Runtime 先核固定 spec、schema、当前 Capability/Execution Policy、Project lifecycle 和正式授权。Actor 与 Project 取自可信 Tool Context，保持真实 AgentRun；不得用 ProjectOwner 的 Human Session、maintenance actor 或自签许可替代。
2. 本 Operation 首次形成持久业务输入时生成且保存一个 SkillID，原规范参数、包/manifest digest 和该 ID 共同绑定。幂等 key 固定为 `tool.skill.install:` 加 canonical Operation UUID；以 `project/[ProjectID]/skill.install/key` 建立原领域 CommandIdentity。模型不能提供/重置 key。`meta.ExpectedVersion=nil`；`RequestID` 只记录实际调用，不作为语义 key。
3. 同 Operation 的重试/恢复使用原 SkillID、key 和完整包事实。新模型调用即使正文相同仍是新 Operation，不按内容 hash 偷合并。建立 Operation 业务输入后才可进入首次有副作用的 Service 调用；生成 ID 本身不形成已安装资源或权限。
4. Service 拥有实际完整发布生命周期：当前授权/同名和保护规则 → 可恢复计划 → 正式 Object 请求/原实际尾 → 同 Tx immutable revision/current/reference/幂等结果发布。UI 走相同 Service；D21 不跨库 SQL、不直接调用私有 `planInstallation`，不另造 Object 维护权限或第二份上传流程。
5. AgentRun 分支必须消费真实执行方授权，绑定该 Project/Agent/Execution、Operation、spec/binding、原 command/target/package 与当前生命周期，在原计划及最终发布点重验。授权 issuer 由真实 D18/D22 提供，Skill 定义窄消费口；普通 DTO/ToolID 白名单/Registry 行不构成 witness。Human UI 仍沿现 current Owner/Session 门，不改为 Agent 冒充 Owner。
6. 仅原发布已知 committed 返回合法 receipt；响应丢失或提交 Unknown 保留原 attempt/cause 和 key，由原安装命令查询/收敛，不自动重新上传或创建第二 revision。取消必须保实际 reader/Object/Service call 的退出与 Stop/Drain 语义；不能以 ctx 结束当物理操作已 join。安装服务发布和数据库事务不是跨 Object 的假原子事务。

`keyed` 表示上述真实业务去重与恢复条件，不独自授权技术重试。当前基础仅绑定 Human User 的语义摘要；实现 Agent 分支须绑定真实 Agent/Execution/Operation，不能解析虚构 UserID 复用该摘要。完整正式发布、Agent 窄授权和原命令恢复都未接好前，source 不可声称可执行。

## 5. 当前 scope、风险与结果

ScopeResolver 只从可信 Context、原 Operation 持久输入和经验证的包构造当前 scope：`project_id, agent_id, execution_id, operation_id, action=skill.install.create, skill_id, normalized_name, package_sha256, manifest_sha256`。原 Tool/spec/binding 和 canonical arguments 同时进入 D18 fingerprint；同 Operation 任一事实漂移拒绝。此 scope 不含包正文、文件列表、key、用户材料或宿主路径，不接受模型传入的 scope。v1 `reusable_scope=unsupported`；如 Policy 要求审批，只有对原 Operation/fingerprint 的正式一次性批准，不能扩为整个 Project 的安装权。

RiskClassifier 对这一精确 v1 返回**空风险集合**：受控文本只发布项目内资源，不执行代码、不改 Agent Capability、不自动分配、不删除旧内容或写第三方系统，符合普通项目读写默认规则。`read_only=false` 不自动变成 destructive；包里有脚本文本也不等于执行脚本。基础授权或 Policy 的 deny/waiting 仍先于 Backend；空风险集合不等于无权限校验。以后新增任意执行/网络下载/分配或来源规则须重新分类，不能借此沿用空集合。Runner 来源必须另核 System Runner ACL、当前真实 Mount、执行权限与 D17 直传，不能把本路径无 Runner 依赖解释为绕过这些门。

成功 `structured_data` 只含下列显式投影，不直接 JSON 序列化安全常量型 `InstallReceipt`，也不暴露其 ObjectID/InstallationID：

```json
{
  "$schema": "https://json-schema.org/draft/2020-12/schema",
  "type": "object",
  "additionalProperties": false,
  "required": ["skill_id", "revision", "version"],
  "properties": {
    "skill_id": {
      "type": "string",
      "pattern": "^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$"
    },
    "revision": {"type": "string", "const": "1"},
    "version": {"type": "string", "const": "1"}
  }
}
```

先 `receipt.Validate()`，再核其 ProjectID/SkillID/包摘要与原调用、revision/version 均 1；完整内部 receipt 交 Runtime 原操作恢复记录，模型只见这三字段。输出总 JSON ≤256 字节，`artifacts=[]`、`truncated=false`；成功不附包正文或虚构 assignment。失败不返回部分成功投影，不因 Object 已 available 宣称发布成功。

沿 D01 ToolError：参数/包校验失败为 `invalid_arguments`，越项目/失权/保护资源拒绝为 `authorization_denied`，同名或 key/语义冲突为 `conflict`，缺 Service/授权绑定为 `backend_unavailable`，已提交与否未知为 `unknown_outcome` 且 `outcome_known=false`；输入/服务上限的安全错误不回显正文、路径集合或原底层异常。字段齐备但 receipt 不匹配为 `backend_contract_violation`。重试与可见取消结果仍由 Runtime 根据实际 outcome 判定，不通过固定 `retryable=true` 恢复调用。

## 6. 最小实施链与验收边界

先由 Skill owner 完成公开 Install 的 Human 原发布/恢复基础与生命周期，再接真实 Agent 安装授权；D21 实现上述 source/handler/scope/risk，D18 以实际绑定注册，Agent 创建按真实 ToolID 保默认/禁用规则。注册不需要连接 Runner 或运行一次安装来探活，也不要求本次完成其余 Builtin，但不能用仅 Human 可用的服务冒充 Agent Backend。

首条真实链为：实际已授权 AgentRun/Operation → 原 text_files 包 → 同一 Skill InstallService → Object/数据库已知提交 → 三字段结果；检查同 key 同 SkillID 恢复、同名失败、不产生 assignment。必要拒绝覆盖无执行授权/跨 Project、旧 Operation 或包漂移、保护 Add Skills，以及对象成功后发布失败/Unknown 不报成功。另核关闭 install-skill 后真实 Tool Set 不可调用；如尚缺实际 Runtime，则可先联调 Human 公共 Service，但只能报告该有限结果。

本次只是固定 schema、工程身份和接线职责，无新增 Go/迁移/后端绑定，无测试或真实安装通过声明。公开 Service、Agent authorization、D18 Operation/schema/dispatcher 的缺口按上述 owner 实施，不再以工具名称或字段“待固定”阻塞；既有 Runtime、Object 与其他 STOP 不因本卡解除。
