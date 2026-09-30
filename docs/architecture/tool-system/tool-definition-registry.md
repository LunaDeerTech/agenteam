# Tool Definition & Registry 详细设计

> 状态：设计稿
>
> 上层架构：
> - [统一工具系统架构](./README.md)
> > 总体设计：
> > - [Unified Tool Runtime 详细设计](./tool-runtime.md)

本分册描述 Unified Tool Runtime 中 Tool 的稳定身份、定义、Backend 绑定、Registry 注册生命周期以及 Execution 级 Tool projection。

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

Backend 暂时离线、网络不可达或目标 Runner offline 都不改变 stable tool id，也不产生额外的 Tool availability 状态。

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
- Backend 当前是否在线；
- 当前网络 / 连接健康状态。

这些临时运行状态不进入 Tool Registry 的正式状态模型，而是在实际调用时由对应 Backend 直接返回运行结果或错误。

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
backend type
credential
model-visible name
temporary backend health
~~~

这些分别属于 ToolBinding、ExecutionTool、Runtime / Execution Policy 或具体 Backend 的调用时状态。

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
- Runner adapter / operation binding；
- MCP server config + remote tool name。

Runner Tool 的 ToolBinding **不绑定某一台具体 Runner**。

例如：

~~~text
runner:run-command
-> Runner Executor / run_command operation
~~~

具体调用落到哪台设备，由 Tool Call 中选择的 Agent Mount 在执行时解析：

~~~text
mount_id
-> Agent Mount
-> runner_id + workspace
-> current Runner connection / capability
~~~

因此某一台 Runner offline 不会改变 `runner:run-command` 的 Registry registration，也不会影响同一 Agent 挂载的其他 Runner。

ToolBinding 不保存模型可见名称。

ToolBinding 也不应该把 credential plaintext 放入统一 Runtime 对象；Credential 仍通过 Secret / Credential 系统按调用需要解析。

第一阶段不在 ToolBinding 中提供 tool-specific retry profile。Retry 使用统一 Tool Runtime 策略；只有未来确有 Backend 特殊需求时，才考虑允许 ToolBinding 增加内部 override。

### 2.5 RegisteredTool

RegisteredTool 是 Tool Registry 中一个当前已注册、可以解析定义与 Backend binding 的 Tool：

~~~text
RegisteredTool
├── spec: ToolSpec
└── binding: ToolBinding
~~~

因此：

~~~text
ToolSpec
= Tool 的稳定定义

ToolBinding
= Tool 的执行绑定

RegisteredTool
= 当前 Registry 中存在的 Tool 定义 + binding
~~~

Registry 不再维护独立的 `ToolRuntimeState / availability` 层。

Runner offline、MCP Server 暂时不可访问、网络故障、临时 backend health 变化等都不修改 RegisteredTool。真正执行时由 Backend 返回对应错误。

## 3. Tool Registry

Tool Registry 是 Central 当前已经注册的 RegisteredTool 统一索引。

概念上：

~~~text
Tool Registry
= 当前 RegisteredTool 的集合
~~~

它需要支持：

~~~text
get(stable_tool_id) -> RegisteredTool
list(source / scope)
resolve binding
observe ToolSpec revision
~~~

Registry 的数据来源可以不同：

- Builtin Tool：代码注册；
- Runner Tool：平台 Runner Tool definition；
- MCP Tool：最近一次成功 discovery 的当前注册结果。

Registry 不等于 Agent 当前能使用的 Tool Set。

~~~text
Tool Registry
= 平台当前已注册的 Tool 定义 / binding

Agent Capability
= 某 Agent 长期最多允许哪些 Tool

Execution Tool Set
= 某次 Execution 实际向模型暴露哪些 Tool
~~~

## 4. Registry 注册生命周期

第一阶段不维护：

~~~text
available
unavailable
disabled
~~~

这样的统一 Tool availability 状态层。

Registry 只回答：

> 当前这个 Tool 是否仍然具有有效的平台注册定义与 Backend binding。

临时运行状态不改变 Registry：

- Runner offline；
- MCP Server 临时网络不可达；
- MCP 调用 timeout；
- Backend 短暂故障。

这类情况统一在真正调用时形成：

- backend_unavailable；
- network；
- timeout；
- capability_unsupported；
- 或其他标准 ToolError。

### 4.1 从当前 Registry 移除

以下控制面变化可以使 Tool 不再进入新的 Execution Tool Set：

- Tool 来源被明确 disable；
- MCP Connection disconnect；
- MCP Config delete；
- 一次完整、成功的 MCP discovery 明确确认远端 Tool 已消失；
- 平台代码 / 配置明确移除某个 Tool definition。

此时从当前 Registry 移除对应 RegisteredTool，但继续保留：

- stable Tool identity；
- 历史 ToolSpec revision；
- Agent Capability 中的 stable Tool ID 引用；
- Audit / historical Execution 引用。

因此“未注册”不等于删除历史身份。

### 4.2 重新注册

来源恢复后，可以使用原 stable identity 重新注册：

- definition 未变化：继续引用已有 ToolSpec revision；
- definition 已变化：生成新 immutable spec_revision；
- Agent Capability 不需要重新创建。

### 4.3 已运行 Execution

Execution 启动时已经保存自己的 ToolSpec revision 与 binding snapshot。

后续 live Registry 中 Tool 被移除，不主动修改运行中的 Execution snapshot。

实际调用如果 Backend 已经不存在、连接失败或目标资源不可执行，则自然形成 Backend error；Runtime 不偷偷切换到其他 Tool 或其他 Backend。

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

对于 Runner Tool，`binding_snapshot` 固定的是 Runner Executor / operation contract，而不是某个具体 `runner_id` 的在线连接；目标设备仍由每次 Tool Call 的 Mount 参数解析。

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

Execution Tool Set 构建时不因为 Runner offline、MCP 暂时不可达等瞬时状态过滤 Tool。真正调用时再检查目标 Backend / Runner / Connection，并把失败作为标准 ToolError 返回。

如果 Tool 来源在 live Registry 中被明确移除，新的 Execution 不再获得它；已经运行的 Execution 仍保留自己的 binding snapshot，但实际调用可能失败，Runtime 不能偷偷改成另一个 Tool。

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
