# Tool Definition & Registry 详细设计

> 状态：设计稿
>
> 上层架构：
> - [统一工具系统架构](../../architecture/tool-system.md)
> > 总体设计：
> > - [Unified Tool Runtime 详细设计](./tool-runtime.md)

本分册描述 Unified Tool Runtime 中 Tool 的稳定身份、定义、注册状态以及 Execution 级 Tool projection。

## 1. Stable Tool Identity

每个 Tool 必须具有稳定身份：

~~~text
stable_tool_id
~~~

stable tool id 用于：

- Agent Capability；
- Execution Tool Set；
- Approval Scope；
- Audit；
- Tool Call routing；
- Execution snapshot；
- 历史查询。

它不能依赖当前 display name 或模型可见名称。

建议的命名空间：

~~~text
builtin:<tool_name>

runner:<tool_name>

mcp:<mcp_server_config_id>:<remote_tool_name>
~~~

这里表达的是概念结构，不要求数据库直接把字符串作为唯一主键。

内部实现可以使用 UUID 作为数据库 identity，同时保存 canonical stable key。

### 1.1 Stable Identity 与 Schema

同一个 stable tool id 表示：

> 这是同一个逻辑 Tool 能力。

Tool description、input schema、output schema、annotations 等 ToolSpec 内容可以随配置或 discovery 更新，并形成新的 ToolSpec revision。

availability 属于 ToolRuntimeState，可以独立变化，不影响 stable tool id，也不要求 ToolSpec revision 随之变化。

但如果一个变化已经改变了 Tool 的授权语义或逻辑身份，则不应继续沿用原 stable identity 静默替换。

MCP Tool 的 schema compatibility 具体规则由 MCP discovery 详细设计决定。

## 2. ToolSpec

ToolSpec 是 Tool 的纯定义，回答的是：

> 这个 Tool 是什么、接受什么输入、产生什么输出、具有什么稳定语义。

它不负责描述：

- 当前 Backend 在哪里；
- 当前通过什么 adapter 执行；
- MCP endpoint / Runner / RPC 地址；
- credential reference；
- 当前是否 online / available；
- 当前健康状态。

这些分别属于 ToolBinding 与 ToolRuntimeState。

建议概念结构：

~~~text
ToolSpec
├── stable_tool_id
├── name
├── description
├── input_schema
├── output_schema?
├── annotations?
└── spec_revision
~~~

其中：

- `stable_tool_id`：平台长期稳定身份；
- `name`：Tool 自身逻辑名称，不等于 model-visible name；
- `description`：统一 Tool 描述；
- `input_schema`：统一输入 JSON Schema；
- `output_schema`：可选的结构化输出 JSON Schema；
- `annotations`：可选语义提示，不直接等于 Security Policy；
- `spec_revision`：ToolSpec 定义本身的单调递增版本号。

第一阶段不把以下内容放入 ToolSpec：

~~~text
timeout
concurrency
retry policy
availability
backend type
credential
model-visible name
~~~

这些分别属于 ToolBinding、ToolRuntimeState、ExecutionTool 或 Runtime / Execution Policy。

`spec_hash` 可以作为 Registry / discovery 的内部实现字段生成，用于 diff，但不作为第一阶段 ToolSpec 的正式 contract 字段，也不作为版本号。

### 2.1 input_schema

input_schema 使用 JSON Schema 表达。

第一阶段应形成一个平台 canonical schema 表示，不允许 Backend 直接把 Provider-specific function schema 塞进 ToolSpec。

链路固定为：

~~~text
Builtin definition / Runner definition / MCP schema
    ↓
Unified ToolSpec.input_schema
    ↓
Execution Tool projection
    ↓
Model Adapter
    ↓
OpenAI / Anthropic native tool schema
~~~

因此 Tool Runtime 不负责：

- OpenAI function schema quirks；
- Anthropic tool schema quirks；
- Model Provider 的字段限制。

这些由 Model Adapter 负责。

### 2.2 output_schema

output_schema 可选。

它用于描述 Tool 成功结果中的 structured data。

没有 output_schema 不代表 Tool 不能返回结构化结果，但 Runtime 无法进行完整的 schema validation。

如果 Tool 明确声明 output_schema：

- Backend 返回的 structured data 应进行 validation；
- validation 失败不应伪装成正常成功结果；
- 应形成标准化 runtime error / backend contract violation。

### 2.3 annotations

annotations 用于表达 Tool 自身的语义提示，例如：

~~~text
read_only: boolean
destructive: boolean
idempotency:
  inherent
  keyed
  none
  unknown
~~~

第一阶段只保留这三个 annotation：

- `read_only`：Tool 是否只读取状态，不产生业务写入；
- `destructive`：Tool 是否可能产生删除、覆盖或其他明显破坏性副作用；
- `idempotency`：Tool 的幂等语义，使用 `inherent / keyed / none / unknown` 四种状态，而不是简单 boolean。

这些 annotation 可以辅助：

- Runtime concurrency；
- retry safety；
- UI 展示；
- 调试与审计。

但 annotation 不直接决定：

- 是否需要 Approval；
- Agent 是否有权限调用；
- 是否允许跨 Project 访问资源。

这些仍由 Security / Governance 决定。

对于 MCP 等外部来源提供的 annotation，是否可信、如何映射为 Unified ToolSpec annotation，由对应 Backend / MCP detailed design 决定。

### 2.4 ToolBinding

ToolBinding 回答的是：

> 这个 Tool 当前应该通过什么 Backend 路径执行。

建议概念结构：

~~~text
ToolBinding
├── stable_tool_id
├── adapter
│   ├── builtin
│   ├── runner
│   └── mcp
└── backend_ref
~~~

`backend_ref` 是统一抽象引用，具体 Backend 可以进一步解析为：

- Builtin handler / service binding；
- Runner capability / runner-side tool binding；
- MCP server config + remote tool name。

ToolBinding 不保存模型可见名称。

ToolBinding 也不应该把 credential plaintext 放入统一 Runtime 对象；Credential 仍通过 Secret / Credential 系统按调用需要解析。

第一阶段不在 ToolBinding 中提供 tool-specific retry profile。Retry 使用统一 Tool Runtime 策略；只有未来确有 Backend 特殊需求时，才考虑允许 ToolBinding 增加内部 override。

### 2.5 ToolRuntimeState

ToolRuntimeState 描述 Tool 当前的动态运行状态。

建议概念结构：

~~~text
ToolRuntimeState
├── availability
├── health?
├── last_checked_at?
└── source_state?
~~~

其中 `availability` 至少区分：

~~~text
available
unavailable
disabled
~~~

ToolRuntimeState 的变化不代表 ToolSpec revision 必然变化。

例如 Runner offline、MCP Server 暂时不可访问，只会改变 Runtime State，不应该修改 ToolSpec 的语义版本。

### 2.6 RegisteredTool

RegisteredTool 是 Tool Registry 中一个完整、当前可解析的 Tool 条目：

~~~text
RegisteredTool
├── spec: ToolSpec
├── binding: ToolBinding
└── runtime_state: ToolRuntimeState
~~~

因此：

~~~text
ToolSpec
= Tool 的纯定义

ToolBinding
= Tool 的执行绑定

ToolRuntimeState
= Tool 当前的动态状态

RegisteredTool
= Registry 中完整的 live tool view
~~~

RegisteredTool 是平台当前状态，会随着：

- MCP rediscovery；
- Runner online / offline；
- Tool disable / enable；
- ToolSpec `spec_revision` 更新；
- Backend binding 更新；

而发生变化。

它不是 Agent Execution 的 immutable snapshot。

## 3. Tool Registry

Tool Registry 是当前 Central 中所有 RegisteredTool 的统一索引。

概念上：

~~~text
Tool Registry
= RegisteredTool 的集合
~~~

它需要支持：

~~~text
get(stable_tool_id) -> RegisteredTool
list(source / scope / availability)
resolve binding
observe ToolSpec revision
observe ToolRuntimeState
~~~

Registry 的数据来源可以不同：

- Builtin Tool：代码注册；
- Runner Tool：Runner capability / platform definition；
- MCP Tool：discovery 结果。

Registry 不等于 Agent 当前能使用的 Tool Set。

~~~text
Tool Registry
= 平台当前知道的 RegisteredTool live view

Agent Capability
= 某 Agent 长期最多允许哪些 Tool

Execution Tool Set
= 某次 Execution 实际向模型暴露哪些 Tool
~~~

## 4. Tool Availability

Tool availability 是 ToolRuntimeState 的核心字段，并与 Agent Capability 分离。

统一 availability 至少区分：

~~~text
available
unavailable
disabled
~~~

建议语义：

### available

当前可以进入新的 Execution Tool Set，并且存在可解析 Backend。

### unavailable

配置仍然存在，stable identity 保留，但当前 Backend 暂时不可用。

例如：

- MCP Server discovery 失败；
- Runner offline；
- image generation backend 当前不可用。

已有 Agent Capability 不删除。

### disabled

管理员主动禁用该 Tool 来源或相关配置。

disabled Tool 不进入新的 Execution Tool Set。

是否允许已经启动的 Execution 继续使用对应 snapshot，由具体 Provider / Backend 的配置生命周期设计决定，不由 Registry 自行决定。

## 5. ToolSpec Revision

ToolSpec 需要可识别的 `spec_revision`。

第一阶段统一使用单调递增整数：

~~~text
spec_revision = 1, 2, 3, ...
~~~

`spec_revision` 表示的是 ToolSpec 定义的版本，而不是 Backend、MCP Server、Runner 或具体 Tool 实现程序的版本。

当同一个 `stable_tool_id` 对应的 ToolSpec 定义发生变化时，例如 description、input schema、output schema 或 annotations 更新，应产生新的 `spec_revision`。

每个已经产生的 ToolSpec revision 都是 immutable versioned entity：

- 新定义产生新的 `spec_revision`；
- 已存在 revision 不原地修改；
- 历史 revision 不因后续更新而删除；
- 历史 Execution 可以始终通过 `stable_tool_id + spec_revision` 还原当时使用的 ToolSpec。

`spec_revision` 的目标不是暴露给模型，而是用于：

- 判断 Execution snapshot 使用哪个 ToolSpec；
- discovery diff；
- 审计；
- 调试；
- 检查 Backend 返回是否仍与当前 Execution 的 schema contract 一致。

Execution 一旦启动，不应因为 Registry 中 ToolSpec `spec_revision` 更新就静默改用新 schema。

`spec_hash` 可以作为内部 diff 辅助字段，用于判断内容是否发生变化，但不承担正式版本语义。

## 6. Execution Tool Set

Agent Executor 在创建 Agent Execution 时，从 Tool Registry 的 RegisteredTool live view 中解析本次 Execution 的 Tool Set。

概念过程：

~~~text
RegisteredTool
∩ Agent Capability
∩ Execution Policy
∩ current availability
=
Execution Tool Set
~~~

Core Agent Tools 按已有架构规则处理。

Execution Tool Set 不复制完整 ToolSpec schema，而是固定引用当时的 immutable ToolSpec revision，并单独保存 execution-scoped 的 projection / binding snapshot。

建议：

~~~text
ExecutionTool
├── stable_tool_id
├── spec_revision
├── model_visible_name
└── binding_snapshot
~~~

需要 Tool 定义时：

~~~text
stable_tool_id + spec_revision
-> immutable ToolSpec
~~~

`binding_snapshot` 固定本次 Execution 实际使用的执行绑定，credential plaintext 不进入 snapshot。

### 6.1 为什么要 snapshot

ExecutionTool 是从 RegisteredTool 派生出的 execution-scoped immutable projection。

这样运行中的 Execution 不会因以下变化改变语义：

- MCP rediscovery；
- Tool description 更新；
- input schema 更新；
- Runner capability metadata 更新；
- Tool 配置被重新命名。

其中 Tool 定义通过 immutable ToolSpec revision 固定，Backend 路由通过 `binding_snapshot` 固定。

长期配置变化默认只影响新的 Execution。

如果 Backend 本身已经被 disable / deleted / unreachable，Runtime 可以执行失败，但不能偷偷改成另一个 Tool。

## 7. Model-visible Tool Projection

模型不能直接依赖 stable tool id。

原因包括：

- stable id 可能过长；
- 某些 Provider 对 function name 有格式限制；
- 不同来源可能存在同名 Tool；
- Provider 对名称字符集和长度限制不同。

因此每次 Execution 建立：

~~~text
stable_tool_id
<-> model_visible_tool_name
~~~

映射。

要求：

1. 在当前 Execution Tool Set 内唯一；
2. 在整个 Execution 生命周期内稳定；
3. 不因后续 Registry 更新变化；
4. 可以被 Model Adapter 安全转换；
5. Tool Call 返回时必须能无歧义反解 stable tool id。

示例：

~~~text
builtin:update-task
-> update_task

mcp:<github-id>:search-code
-> github_search_code

mcp:<jira-id>:search-code
-> jira_search_code
~~~

具体命名算法可以在实现阶段确定，但必须确定性生成，并保存 snapshot。
