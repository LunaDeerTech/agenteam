# MCP Tool 默认启用策略详细设计

> 状态：讨论中
>
> 上层架构：
> - [安全与治理架构](../security-governance/README.md)
> - [MCP 集成架构](./README.md)
> - [统一工具系统架构](../tool-system/README.md)

## 1. 设计范围

本文只讨论：

> MCP Server 在 Tool discovery 时声明的 annotations，如何由 MCP Bridge 翻译成 agenteam Unified Tool 的 default_enabled。

MCP Tool 的 annotations 只承担默认启用建议的输入职责，不构成独立的运行时安全策略。

MCP Tool 一旦被 Bridge 转换为 Unified Tool，后续与 Builtin / Runner Tool 走相同的 Tool Registry、Agent Capability、Execution Policy、Approval 和 Tool Authorization 流程。

## 2. 核心原则

MCP Server 提供的 Tool annotations 只用于：

> 决定该 MCP Tool 在 Agent Capability 配置界面中是否应该默认启用。

不用于：

- 直接决定某次 Tool Call 是否允许执行；
- 直接决定某次 Tool Call 是否需要 Approval；
- 绕过 Agent Capability；
- 绕过 Execution Policy；
- 绕过统一 Tool Authorization。

因此：

~~~text
MCP Tool annotations
    ↓
MCP Bridge
    ↓
Unified ToolSpec.default_enabled
~~~

而不是：

~~~text
MCP Tool annotations
    ↓
运行时授权 / Approval 决策
~~~

## 3. MCP Tool Annotations

MCP Tool 可以声明：

~~~text
readOnlyHint
destructiveHint
idempotentHint
openWorldHint
~~~

这些值由 MCP Server 提供。

MCP Bridge 可以读取这些 annotations，并根据平台固定翻译规则计算：

~~~text
default_enabled: true | false
~~~

第一阶段不要求把这些 annotation 原样保留为 agenteam 运行时安全策略。

如果为了 UI、调试或 discovery diff 需要，可以把原始 annotation 作为 discovery metadata 保存，但它们不参与后续统一 Tool 的运行时授权。

## 4. Unified ToolSpec

Unified ToolSpec 增加：

~~~text
default_enabled: boolean
~~~

其语义是：

> 当用户第一次为 Agent 配置 Tool Capability 时，这个 Tool 是否应该默认处于启用状态。

它不是：

- 当前 Agent 是否已经拥有这个 Tool；
- 当前 Execution 是否可以调用这个 Tool；
- 当前 Tool Call 是否需要 Approval。

真正的长期权限仍然保存于 Agent Capability。

因此：

~~~text
default_enabled
= UI / 初始配置建议

Agent Capability
= 已确认的长期 Tool 权限
~~~

## 5. MCP Bridge 翻译流程

~~~text
MCP Server
  -> Tool Discovery
      -> Tool schema + annotations
          -> MCP Bridge
              -> Unified ToolSpec
                  - stable id
                  - name
                  - description
                  - input schema
                  - source = mcp
                  - default_enabled
                  - execution adapter reference
~~~

MCP Bridge 在 discovery 时根据平台固定规则计算 default_enabled。

该规则属于 MCP Bridge 的转换规则，不由 Agent 修改。

## 6. 与 Agent Capability 的关系

default_enabled 只影响初始配置体验。

例如：

~~~text
MCP Server
  -> search-code
     default_enabled = true

  -> create-issue
     default_enabled = false
~~~

当 Project Owner 新建 Agent 或首次配置该 MCP Server 的 Tool Capability 时，UI 可以据此：

~~~text
[x] search-code
[ ] create-issue
~~~

但最终保存的仍然是明确的 Agent Capability。

### 6.1 新 discovery Tool

MCP Server 后续 discovery 出新的 Tool 时：

> 不能因为 default_enabled = true 就自动加入已有 Agent Capability。

新 Tool 只出现在配置界面中，并展示建议默认值。

已有 Agent 的 Capability 不自动扩大。

### 6.2 已有 Capability

一旦 Project Owner 已经明确保存 Agent Capability，default_enabled 不再参与该 Agent 的运行时权限判断。

即使 MCP Server 后续 annotation 改变、Bridge 重新计算出不同的 default_enabled，也不能静默修改已有 Agent Capability。

## 7. 与 Approval 的关系

MCP annotation 和 default_enabled 不直接参与 Approval Policy。

MCP Tool 一旦成为 Unified Tool，并且已经被明确加入 Agent Capability，后续 Tool Call 按统一安全链路执行：

~~~text
Agent Capability
  -> Execution Policy
      -> Project / Resource Scope
          -> Approval Match
              -> Approval Policy
                  -> Tool Backend
~~~

因此不会存在一套单独的：

~~~text
MCP Approval Policy
MCP Effective Security Metadata
MCP Owner Security Override
~~~

## 8. 配置界面

MCP Tool 配置界面至少需要区分：

~~~text
默认建议状态
当前 Agent Capability 状态
~~~

例如：

~~~text
search-code
  Recommended: enabled
  Current: enabled

create-issue
  Recommended: disabled
  Current: enabled
~~~

这样即使 Owner 手动启用了默认关闭的 Tool，也不会和 Bridge 的默认建议混淆。

如果展示 MCP 原始 annotations，它们只能作为解释“为什么系统建议默认开启/关闭”的辅助信息。

## 9. Rediscovery

MCP Tool rediscovery 时可以重新计算 default_enabled。

它只影响：

- 后续新 Agent 的初始配置；
- 尚未确认的配置界面建议；
- UI 中的推荐状态。

它不能自动修改：

- 已保存的 Agent Capability；
- 正在运行的 Agent Execution Tool Set；
- 已经存在的 Approval；
- 当前 Tool Call 的授权结果。

## 10. 已确认的翻译规则

第一阶段只使用 `readOnlyHint` 计算 `default_enabled`：

~~~text
readOnlyHint = true
-> default_enabled = true

readOnlyHint = false
-> default_enabled = false

readOnlyHint 缺失
-> default_enabled = false
~~~

等价实现：

~~~text
default_enabled = (readOnlyHint === true)
~~~

其他 annotation 第一阶段不参与 `default_enabled` 计算：

~~~text
destructiveHint
idempotentHint
openWorldHint
~~~

原因：

- `destructiveHint` 主要描述非只读操作的破坏性；非只读 Tool 已经默认关闭；
- `idempotentHint` 描述重复调用语义，不代表适合默认启用；
- `openWorldHint` 描述 Tool 是否与开放世界交互，不等价于写入风险，例如只读搜索 Tool 也可能是 open world。

例如：

~~~text
search-code
readOnlyHint = true
-> default_enabled = true

list-issues
readOnlyHint = true
-> default_enabled = true

create-issue
readOnlyHint = false
-> default_enabled = false

send-message
readOnlyHint = false
-> default_enabled = false

unknown-tool
readOnlyHint 缺失
-> default_enabled = false
~~~

因此第一阶段不计算复杂风险分数，也不组合多个 annotation 推导默认启用状态。

## 11. 待实现时确认

是否在 Agent Tool 配置界面展示 MCP Server 原始 annotations，以及是否展示“默认关闭原因”，属于 UI 展示问题，可在实现对应配置界面时再确认，不影响本设计的权限语义。
