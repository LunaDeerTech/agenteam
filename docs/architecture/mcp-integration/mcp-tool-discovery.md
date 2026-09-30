# MCP Tool Discovery 详细设计

> 状态：初版设计稿
>
> 上层架构：
> - [MCP 集成架构](./README.md)
>
> 相关详细设计：
> - [MCP Protocol Runtime](./mcp-protocol-runtime.md)
> - [MCP Server Config / Connection Lifecycle](./mcp-server-config-lifecycle.md)
> - [Tool Definition & Registry](../tool-system/tool-definition-registry.md)
> - [MCP Tool 默认启用策略](./mcp-tool-default-enable.md)

本文定义 MCP `tools/list` 如何进入 Unified Tool Registry，以及 refresh、diff、schema revision、Tool 消失 / 恢复等行为。

## 1. 设计目标

Discovery 链路固定为：

~~~text
Project MCP Connection
-> MCP Protocol Runtime
-> tools/list
-> MCP Tool Adapter
-> canonical Unified ToolSpec
-> Tool Registry
~~~

Discovery 负责“定义同步”，不负责：

- Agent Tool Authorization；
- 自动给已有 Agent 增权；
- Tool 执行；
- MCP Connection authentication；
- Model Provider schema projection。

## 2. Discovery Scope

MCP Tool Discovery 以 `MCP Connection` 为实际 discovery scope。

原因是同一个 System MCP Config 在不同 Project 中可能绑定不同 credential，远端 Server 可能返回不同 Tool 集合或定义。

因此：

~~~text
MCP Config
    ↓
Project MCP Connection
    ↓
Connection-scoped discovery result
~~~

Tool Registry 对 MCP source 的 live view 必须保持 Project scope。

Canonical stable tool key 继续沿用架构约定：

~~~text
mcp:<mcp_config_id>:<remote_tool_name>
~~~

但在持久化 / Registry 查询中，MCP Tool identity 必须与 Project scope 一起解析，不能把不同 Project 的 Connection discovery 结果合并成一个全局 live record。

## 3. Discovery Trigger

第一阶段 Discovery 分为两类：

建立 / 恢复 Connection 所需的即时 discovery：

1. Connect 完成；
2. Enable / reconnect；
3. MCP Config runtime-affecting change 后重新建立 binding；
4. Credential rotation / re-auth 后重新验证。

持续刷新第一阶段只做：

1. Central startup reconciliation；
2. 管理员主动 refresh。

第一阶段不通过 Tool list change subscription 或 remote cache TTL 自动触发 refresh。

Trigger 只进入统一 refresh pipeline，不直接修改 Registry。

## 4. Refresh Pipeline

建议：

~~~mermaid
flowchart LR
    Trigger["Refresh Trigger"]
    Guard["Connection / Auth Guard"]
    List["Protocol Runtime<br/>tools/list"]
    Validate["Validate MCP Definitions"]
    Normalize["MCP Tool Adapter<br/>Normalize"]
    Diff["Diff Current Snapshot"]
    Commit["Atomic Discovery Commit"]
    Registry["Unified Tool Registry"]

    Trigger --> Guard
    Guard --> List
    List --> Validate
    Validate --> Normalize
    Normalize --> Diff
    Diff --> Commit
    Commit --> Registry
~~~

一次 refresh 应形成一个完整 discovery attempt。

不得在分页 / streaming 尚未完成时逐条修改 Registry。

## 5. tools/list 获取

Protocol Runtime 负责协议级调用、分页 / cursor、cache metadata 和版本兼容。

Discovery 层获得完整的 logical result：

~~~text
McpToolListResult
├── tools[]
├── protocol_revision
├── cache_metadata?
└── source_metadata?
~~~

如果 list 过程部分失败：

- 本次 refresh 失败；
- 不提交 partial Registry state；
- 保留上一次成功 discovery snapshot；
- 不因为部分结果缺失把 Tool 标记 removed。
- 不修改已有 ToolSpec / ToolBinding / 当前 Registry registration；
- 只记录本次 Discovery failure 的 Audit / diagnostics。

如果此前从未成功完成 discovery，则 Registry 继续保持没有该 Connection 的已发现 Tool；失败本身不构造占位 Tool 或 unavailable 状态。

## 6. MCP Tool Definition Validation

每个远端 Tool 至少校验：

- remote name 存在且满足协议约束；
- inputSchema 可解析；
- outputSchema 如存在则可解析；
- definition 大小、schema depth 等不超过平台安全限制；
- 不自动 dereference 外部 `$ref`；
- annotation 格式合法；
- duplicate remote tool name 不接受为有效完整 snapshot。

单个 Tool definition 无法安全解析时：

- 不影响本次 discovery 中其他有效 Tool 的正常提交；
- 如果这是一个此前从未成功注册的新 Tool，则忽略该 Tool，不创建可用 ToolSpec；
- 如果同 stable identity 的 Tool 之前已有成功 ToolSpec 且当前仍注册，则保留上一版 ToolSpec / ToolBinding / registration，不因为本次 definition invalid 改写当前 Registry；
- Audit / diagnostics 记录 invalid tool name、safe validation error 与 invalid tool count；
- 不把无效 schema 投影给模型。

如果 Server 返回结构本身不符合 MCP protocol，则视为整个 refresh protocol failure。

## 7. Schema Normalization

MCP Tool Adapter 把 MCP definition 转换为 canonical Unified ToolSpec。

~~~text
MCP Tool
├── name
├── description?
├── inputSchema
├── outputSchema?
└── annotations?
        ↓
MCP Tool Adapter
        ↓
Unified ToolSpec
├── stable_tool_id
├── name
├── description
├── input_schema
├── output_schema?
├── annotations
└── spec_revision
~~~

### 7.1 JSON Schema

MCP 2026-07-28 Tool schema 使用 JSON Schema 2020-12。

Discovery 层应尽量保留 schema 语义，不在这里为了某个 Model Provider 主动降级。

~~~text
MCP JSON Schema
-> canonical ToolSpec schema
-> ExecutionTool
-> Model Adapter
-> provider-specific projection
~~~

Provider 不支持的 JSON Schema feature 由 Model Adapter 处理。

### 7.2 External $ref

第一阶段不自动从任意外部 URI 下载 schema。

允许：

- local `$defs` / internal ref；
- schema 内部可验证的引用。

外部 `$ref`：

- 保留原定义用于 diagnostics / revision；
- 不进行网络 dereference；
- 如果因此无法形成可用 canonical input schema：新 Tool 本次不注册；已有 Tool 保留上一版成功 ToolSpec / ToolBinding，并记录 invalid definition diagnostics。

## 8. Annotation Mapping

MCP annotation 不是 Security Policy。

第一阶段：

- `readOnlyHint` 映射到 Unified ToolSpec 的 read-only 语义；
- `idempotentHint` 映射到 Unified ToolSpec 的 idempotency 语义；
- `default_enabled` 使用独立的 [MCP Tool 默认启用策略](./mcp-tool-default-enable.md)；
- annotation 不直接决定 Approval / Authorization。

idempotency 映射固定为：

~~~text
readOnlyHint === true
-> idempotency = inherent

idempotentHint === true
-> idempotency = inherent

otherwise
-> idempotency = unknown
~~~

第一阶段不从 MCP annotation 推导 keyed idempotency。

任何 annotation mapping 都必须是显式规则，不能把未来 MCP 新增 annotation 自动映射成 agenteam 权限含义。

## 9. Stable Identity

同一个 MCP Config 下 remote tool name 稳定时，逻辑 stable identity 保持不变：

~~~text
mcp:<mcp_config_id>:<remote_tool_name>
~~~

以下变化默认不创建新 stable identity：

- description；
- input schema；
- output schema；
- annotations。

definition 变化通过 `spec_revision` 表达。Tool 是否仍存在于当前 Registry 由最新一次成功 discovery 的 registration diff 决定，不使用 ToolRuntimeState。

remote tool name 改名第一阶段按：

~~~text
old tool removed
+ new tool added
~~~

处理，不猜测 rename。

## 10. Discovery Snapshot

建议为每次成功 refresh 保存 logical snapshot metadata：

~~~text
McpDiscoverySnapshot
├── connection_id
├── discovery_revision
├── protocol_revision
├── tool_count
├── content_hash
├── discovered_at
├── cache_metadata?
└── source_diagnostics?
~~~

ToolSpec revision 仍是每个 Tool 自己的定义版本。

~~~text
discovery_revision
= 一次 Connection discovery snapshot 版本

spec_revision
= 某一个 stable ToolSpec 的定义版本
~~~

两者不要混用。

## 11. Diff 规则

以“上一次成功 snapshot”和“本次完整 `tools/list` 结果”做 diff。

必须区分：

~~~text
remote tool name absent
= removed candidate

remote tool name present but definition invalid
= invalid, not removed
~~~

只有 remote tool name 确实从本次完整 list 中消失时，才能进入 `removed`。

至少区分：

~~~text
unchanged
added
definition_changed
removed
restored
invalid
~~~

### 11.1 unchanged

canonical ToolSpec 内容未变化：

- 不创建新 spec_revision；
- 更新 discovery / health metadata 即可。

### 11.2 added

新 remote tool：

- 创建 stable Tool identity；
- spec_revision = 1；
- 注册 ToolBinding；
- 加入当前 Registry；
- 不自动加入已有 Agent Capability。

`default_enabled` 只用于新 Agent / capability configuration 的建议值。

### 11.3 definition_changed

同 stable identity 的 definition 变化：

- 生成新的 immutable ToolSpec revision；
- Registry current revision 前移；
- 新 Execution 使用新 revision；
- 已运行 Execution 保留旧 revision snapshot。

### 11.4 removed

上次存在、本次完整 snapshot 中不存在：

- stable identity 保留；
- 从当前 Registry 移除 RegisteredTool；
- Agent Capability stable reference 保留；
- 新 Execution 不再获得该 Tool；
- 不删除历史 ToolSpec revision。

### 11.5 restored

之前已经从当前 Registry 移除的 stable identity 再次出现：

- 如果 definition 不变，使用已有 ToolSpec revision 重新注册；
- 如果 definition 改变，先生成新 spec_revision 再注册；
- Agent Capability 不需要重新创建。

### 11.6 invalid

本次 list 中 remote tool name 仍存在，但 definition 无法通过 validation：

- 已存在且当前已注册 Tool：保留上一版 ToolSpec / ToolBinding / registration；
- 之前已移除的 Tool：保持未注册；
- 新 Tool：本次忽略，不注册；
- 不产生新 spec_revision；
- 不视为 removed；
- 不影响其他有效 Tool 的 diff / commit；
- 记录 Audit / diagnostics。

## 12. Definition Change 与信任边界

MCP Tool definition 变化不触发 reconfirmation。

用户决定连接某个 MCP Server，并进一步允许 Agent 使用其中某个 Tool 后，平台视为用户接受该外部 MCP Server 后续对该 Tool definition 的更新。

因此同一个 stable identity 的：

- description；
- input / output schema；
- annotation；
- read-only / destructive 等语义提示；

发生变化时，只要新 definition 本身可以被正常解析和归一化，就生成新的 immutable `spec_revision`。

平台不尝试通过 schema / annotation semantic diff 判断是否需要用户重新确认，也不因为语义变化自动撤销 Agent Capability。

一致性仍通过 Execution snapshot 保证：

~~~text
running Execution
-> 继续使用既有 spec_revision

new Execution
-> 使用最新成功 discovery 的 spec_revision
~~~

外部 MCP Server definition 更新带来的潜在行为变化属于用户选择连接和授权该 MCP Server 后接受的信任风险。

## 13. Atomic Commit

一次成功 discovery 的 Registry 更新应原子提交。

至少保证：

- added / changed / removed 属于同一 discovery_revision；
- UI 不看到半刷新状态；
- Execution Tool Set 创建时读取一致的 Registry view。

数据库实现可以使用 transaction + versioned rows，不要求内存 Registry 自己成为唯一事实来源。

## 14. Discovery Failure

Discovery failure 分为：

~~~text
authentication
network / server unavailable
protocol
invalid response
partial listing
schema invalid
internal
~~~

原则：

- failure 不删除长期 Agent Capability；
- failure 不覆盖最后一次成功 snapshot；
- failure 不修改已有 ToolSpec / ToolBinding / Registry registration；
- failure 不因为临时网络错误移除现有 RegisteredTool；
- failure 不改变新 Execution 对上一版成功 Tool definition 的可见性；
- 已运行 Execution 不受影响；
- 只记录本次失败的 Audit / diagnostics。

Discovery 不承担 MCP Server 健康检查职责。

如果 Registry 中已有上一版成功定义，后续实际调用失败时，由 MCP Tool Execution 映射成正常的 Unified ToolError，Agent 可以据此重试、调整计划或选择其他 Tool。

如果该 Connection 从未有过成功 discovery，则没有可进入 Registry 的 Tool；失败本身不构造占位 Tool。

## 15. Tool List Change Event

第一阶段不接入 Tool list change subscription。

未来增加该能力时，Tool list change event 只作为 refresh trigger：

~~~text
subscription event
-> debounce / coalesce
-> full tools/list refresh
-> diff
-> atomic commit
~~~

不根据 notification payload 猜测局部 Registry patch。

这样可以避免 event 丢失或乱序导致 Registry 漂移。

## 16. Refresh 并发

同一个 MCP Connection 同一时间只允许一个 active discovery refresh。

新的 trigger 到达时可以：

- coalesce 到当前 refresh；
- 或标记 pending refresh，在当前完成后再执行一次。

不得并发执行两个 snapshot commit 互相覆盖。

## 17. Execution 一致性

Discovery 只影响 Registry live view。

Execution 创建时：

~~~text
Registry current ToolSpec revision
+ current ToolBinding
-> ExecutionTool snapshot
~~~

Execution 启动后，即使发生 rediscovery：

- 不替换 spec_revision；
- 不重写 binding snapshot；
- 不改变 model-visible name；
- 不自动新增新 Tool。

## 18. Audit / Diagnostics

至少记录：

- connection id；
- discovery trigger；
- discovery revision；
- start / finish time；
- tool count；
- added / changed / removed count；
- protocol revision；
- safe error；
- invalid definition count。

对于单 Tool validation failure，还应记录：

- remote tool name；
- safe validation error；
- 是否存在上一版成功 ToolSpec。

不默认把完整远端 schema 重复写入 Audit；schema 由 ToolSpec revision 持久化。

## 19. 第一阶段实现边界

第一阶段包含：

1. complete `tools/list` refresh；
2. connection-scoped snapshot；
3. canonical ToolSpec normalization；
4. JSON Schema 2020-12 接入；
5. immutable spec_revision；
6. added / changed / removed / restored diff；
7. atomic Registry commit；
8. Connect / Enable 必要 discovery；
9. startup / manual refresh；
10. discovery diagnostics。

第一阶段不实现：

- rename guessing；
- 自动外部 `$ref` dereference；
- Tool definition reconfirmation；
- JSON Schema / annotation semantic risk diff；
- 自动把新 Tool 加给已有 Agent；
- Tool list change subscription；
- TTL 驱动的自动 refresh；
- 仅依赖 notification 的增量 Registry patch。
