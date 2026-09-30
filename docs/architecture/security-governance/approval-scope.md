# Approval Scope 详细设计

> 状态：设计稿
>
> 上层架构：[安全与治理架构](./README.md)
>
> 相关架构：
> - [Agent 管理架构](../agent-management.md)
> - [统一工具系统架构](../tool-system/README.md)
> - [默认审批策略详细设计](./default-approval-policy.md)
> - [自动审批模型详细设计](./auto-approval-model.md)

## 1. 设计目标

Approval Scope 用于回答：

> 用户批准一次 Agent Tool Call 后，这次批准只适用于当前操作，还是创建一个 Reusable Approval；如果创建 Reusable Approval，后续什么 Tool Call 算作“同一类操作”。

Approval Scope 必须是可机器匹配的结构化数据。

系统不使用以下方式判断“此类权限”：

- 自然语言相似度；
- LLM similarity；
- Tool display name 模糊匹配；
- risk category 相同；
- description 相似。

Risk Metadata 只说明“为什么这个操作需要审批”，不代表授权范围。

## 2. 术语与两类 Approval

本文统一使用以下正式术语：

~~~text
批准本次
-> One-time Approval

以后都允许此类调用
-> Reusable Approval
~~~

`Reusable Approval` 是唯一正式对象名称。

文档统一只使用 `Reusable Approval` 指代该对象，不再定义其他同义对象名称。

Approval 分为：

~~~text
Approval
├── one_time
└── reusable
~~~

### 2.1 One-time Approval

One-time Approval 绑定当前逻辑 Tool Operation。

它用于：

> 批准本次

概念上：

~~~text
project_id
agent_id
tool_id
operation_id
operation_fingerprint
state
~~~

用户批准后，当前等待中的 Tool Operation 直接继续执行。

如果该 Operation 因技术原因发生安全 retry，后续 Attempt 仍属于同一个 operation_id，不需要重新审批。

新的 Agent Tool Call 即使 Tool 和参数完全相同，也会创建新的 operation_id，不能复用原 One-time Approval。

One-time Approval 不采用“第一次 Attempt 执行后即消费”的模型；其生命周期绑定一个 Tool Operation。

详细 retry、idempotency、unknown outcome 和 operation fingerprint 规则见 [One-time Approval、Tool Retry 与 Idempotency 详细设计](./one-time-approval-retry-idempotency.md)。

### 2.2 Reusable Approval

Reusable Approval 是 Agent 级的长期 Approval。

它用于：

> 以后都允许此类调用

Reusable Approval：

- 固定属于一个 Project；
- 固定属于一个 Agent；
- 固定属于一个 stable Tool ID；
- 保存 Tool 自己定义的 structured scope；
- 不绑定某个 Agent Execution；
- 不设置过期时间；
- 持续有效，直到 Project Owner 撤销；
- 不能扩大 Agent Capability；
- 不能绕过 Execution Policy、Project / Resource Scope、平台固定安全规则或 Tool Backend 最终校验。

因此：

~~~text
Reusable Approval
= 当前 Agent 对某类 Tool 操作的长期审批授权

Agent Capability
= 当前 Agent 长期最多具备哪些 Tool 能力
~~~

二者职责不同。

## 3. 固定匹配维度

Security / Governance 只负责少量所有 Tool 都通用的固定维度：

~~~text
project_id
agent_id
tool_id
approval mode / state
~~~

对于 Reusable Approval，匹配前至少必须满足：

~~~text
approval.project_id == current_project_id
AND
approval.agent_id == current_agent_id
AND
approval.tool_id == current_stable_tool_id
AND
approval.state == active
~~~

之后再由对应 Tool 判断它自己的 structured scope 是否覆盖当前调用。

Reusable Approval 不保存 `execution_id` 作为匹配条件。

否则 Reusable Approval 将无法跨 Agent Execution 复用。

## 4. Tool-defined Approval Scope

Security / Governance 不尝试理解所有 Tool 的业务参数。

例如以下概念都属于具体 Tool 的领域语义：

- file path；
- Runner / Mount；
- repository；
- branch；
- issue；
- target Agent；
- Task state；
- MCP resource；
- command；
- Tool-specific arguments。

因此每个需要 Approval 的 Tool 可以提供：

~~~text
ApprovalScopeResolver
~~~

概念职责：

~~~text
Tool Call
   ↓
ApprovalScopeResolver
   ├── current_scope
   ├── reusable_scope?
   └── matches(saved_scope, current_call)
~~~

其中：

- `current_scope`：描述当前操作；
- `reusable_scope`：描述“以后都允许此类调用”对应的 Reusable Approval Scope；可以不存在；
- `matches`：判断某条 active Reusable Approval 是否覆盖当前 Tool Call。

Security / Governance 不提供一套通用 arbitrary constraints DSL。

## 5. 不是所有 Tool 都支持 Reusable Approval

Reusable Approval 不是所有 Tool 的通用能力。

每个 Tool 自己决定：

1. 是否支持 Reusable Approval；
2. 如果支持，当前 Tool Call 可以提升成什么 Reusable Approval Scope；
3. 后续 Tool Call 如何匹配该 scope。

如果 Tool 不支持 Reusable Approval：

~~~text
ApprovalScopeResolver.reusable_scope = unsupported
~~~

UI 只显示：

~~~text
批准本次
拒绝
~~~

不显示：

~~~text
以后都允许此类调用
~~~

## 6. “此类权限”的边界

“此类”由 Tool 实现生成，而不是由 Agent 自己提交。

例如某个结构化 Tool：

~~~text
Tool Call
  tool_id = example.resource.update
  resource_group = group-A
  resource_id = item-1
~~~

Tool 可以定义：

~~~text
current_scope:
  resource_id = item-1

reusable_scope:
  resource_group = group-A
~~~

那么用户选择“以后都允许此类调用”后，该 Reusable Approval 的含义是：

> 当前 Agent 可以在该 Tool 定义的 group-A 范围内执行同类操作。

后续是否命中仍由同一个 Tool 的 ApprovalScopeResolver 判断。

### 6.1 不允许 Agent 自定义授权范围

Agent 不能提交：

~~~text
resource = *
action = *
~~~

或其他任意 Approval Scope。

实际流程必须是：

~~~text
当前 Tool Call
    ↓
Tool ApprovalScopeResolver
    ↓
平台生成 current_scope / reusable_scope
    ↓
展示给 Project Owner
    ↓
用户选择批准方式
~~~

这样 Reusable Approval 的 Scope 始终由平台和 Tool 实现控制，Agent 无法通过构造 Approval Request 自行扩大权限。

## 7. Tool 自己决定是否支持 Reusable Approval

是否支持 Reusable Approval 是 Tool-defined 行为，不由 Security / Governance 按 Tool 名称硬编码。

Tool 可以：

- 提供 Reusable Approval Scope；
- 或返回 `reusable_scope = unsupported`。

具体选择由 Tool 的安全语义和 Scope Resolver 决定，并与 Builtin / Runner / MCP 来源无关。

Security / Governance 只执行统一的 Approval Scope 协议，不为某个具体 Tool 建立特殊匹配流程。

## 7.1 Runner Tool Scope 约束

Runner Tool 的 Reusable Approval Scope 必须保留会改变实际设备边界或生命周期的关键参数，不能只按 Tool 名称匹配。

第一阶段至少明确：

### start-process

`start-process(scope = execution)` 与 `start-process(scope = persistent)` 是不同授权范围。

从 execution-scoped 调用生成的 Reusable Approval：

- 只能覆盖 `scope = execution`；
- 不能自动匹配 `scope = persistent`。

persistent 调用需要独立形成能够明确表达 persistent 生命周期的 Approval Scope。

### expose-port

如果 `expose-port` 支持 Reusable Approval，Scope 至少绑定：

~~~text
runner_id
mount_id / workspace
local_port
~~~

不能使用一个已有 Approval 自动覆盖：

- 其他 Runner；
- 其他 Agent Workspace；
- 其他 local port。

是否还需要绑定 Managed Process identity、TTL 上限等参数，可以由 `expose-port` 的 ApprovalScopeResolver 根据实际 Tool contract 进一步收紧。

## 8. Reusable Approval 数据模型

概念结构：

~~~text
ReusableApproval
├── id
├── project_id
├── agent_id
├── tool_id
├── scope
├── source_tool_call_id
├── created_at
├── revoked_at?
└── state
    ├── active
    └── revoked
~~~

其中：

- `tool_id` 使用 stable Tool ID；
- `scope` 是对应 Tool ApprovalScopeResolver 生成的结构化数据；
- `source_tool_call_id` 记录该长期规则来源于哪次真实 Tool Call；
- `state = active` 时参与匹配；
- `state = revoked` 时不参与匹配；
- 不设置 `expires_at`。

Reusable Approval 作为独立实体持久化，而不是直接嵌入 Agent 配置 JSON。

这样可以独立记录创建、撤销和 Audit 历史。

## 9. Reusable Approval 的创建

Reusable Approval 不能通过普通配置页面手工新增。

唯一正常创建链路：

~~~text
Tool Call
  ↓
基础权限校验通过
  ↓
没有匹配已有 Approval
  ↓
Approval Policy 要求 Human Approval
  ↓
Tool 提供 reusable_scope
  ↓
Approval Request
  ↓
Project Owner 选择
“以后都允许此类调用”
  ↓
创建 Reusable Approval
  ↓
当前 Tool Call 直接继续
~~~

如果 Tool 没有提供 Reusable Approval Scope，则这次 Approval Request 不提供 Reusable Approval 选项。

## 10. Reusable Approval 的匹配

每一次新的 Tool Call 都重新执行 Approval Match。

概念流程：

~~~text
Tool Call
  ↓
基础权限校验
  ↓
查找：
  project_id
  + agent_id
  + tool_id
  + state = active
  ↓
Tool ApprovalScopeResolver.matches(...)
  ├── match
  │    -> 直接进入 Tool Backend
  │
  └── no match
       -> Approval Policy
~~~

撤销后的规则因为 `state != active`，后续 Tool Call 自然无法匹配。

不需要额外设计“是否立即影响当前 Agent Execution”之类的状态传播机制。

Approval Match 本身就是每次 Tool Call 的动态授权步骤。

## 11. Agent 配置界面

Reusable Approval 在产品语义上属于 Agent 的长期配置。

因此 Agent 配置页面增加：

~~~text
Reusable Approvals
~~~

用于展示当前 Agent 的全部 Reusable Approvals。

例如：

~~~text
Tool                   Scope                     State
example.resource.update group-A                  active
mcp:github:create-x     repository = repo-A       active
...
~~~

列表至少展示：

- Tool；
- human-readable Scope；
- state；
- created_at。

Project Owner 可以：

- 查看规则；
- 撤销 active 规则；
- 查看规则来源和必要的 Audit 信息。

第一阶段不提供：

- 手工新增 Reusable Approval；
- 任意编辑 Scope；
- 修改 Tool；
- 修改 Agent；
- 修改成更宽范围。

如果用户需要不同 Reusable Approval Scope，应由新的真实 Tool Call 产生新的 Reusable Approval。

## 12. 撤销

Project Owner 可以在 Agent 配置页撤销 Reusable Approval。

撤销采用状态变化：

~~~text
active
-> revoked
~~~

不建议物理删除。

这样可以保留：

- 谁创建了这条 Reusable Approval；
- 来源 Tool Call；
- 创建时间；
- 什么时候撤销；
- 后续为什么不再匹配。

撤销行为进入 Audit。

Approval 创建、撤销和关联查询遵守统一 [Audit 详细设计](./audit.md)。

## 13. Human-readable Scope

机器匹配使用 structured scope。

UI 同时需要让用户看懂自己正在授权什么。

因此 Tool 的 ApprovalScopeResolver 除 structured data 外，还应能够生成或配套提供 human-readable description。

例如：

~~~text
机器 Scope:
{
  "repository_id": "repo-A"
}

UI:
允许此 Agent 使用该 Tool 操作 repository repo-A
~~~

Human-readable description 只用于展示。

服务端不能使用自然语言重新解释或匹配 Approval Scope。

## 14. 第一阶段边界

第一阶段确定：

1. Approval 分为 one-time 与 reusable；
2. Reusable Approval 是 Agent 级长期 Approval；
3. Reusable Approval 不设置过期时间；
4. 固定匹配维度为 Project + Agent + stable Tool ID + active state；
5. Tool-specific scope 由 Tool ApprovalScopeResolver 定义；
6. 不建设通用 Approval constraints DSL；
7. 不是所有 Tool 都支持 Reusable Approval；
8. Tool 决定是否提供 Reusable Approval Scope 以及如何匹配；
9. Agent 不能自定义 Approval Scope；
10. Reusable Approval 只能从真实待审批 Tool Call 创建；
11. 当前获批 Tool Call 创建 Approval 后直接继续，不重新匹配；
12. Reusable Approval 不绑定 Agent Execution；
13. 每次后续 Tool Call 动态匹配当前 active Reusable Approval；
14. Agent 配置页展示该 Agent 的 Reusable Approval 列表；
15. Agent 配置页只支持查看和撤销，不支持手工新增或任意编辑 Scope；
16. 撤销保留 revoked 记录并进入 Audit；
17. 是否支持 Reusable Approval 由具体 Tool 的 ApprovalScopeResolver 定义，Security / Governance 不按 Tool 名称硬编码特殊规则。
