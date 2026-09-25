# 默认审批策略详细设计

> 状态：设计稿
>
> 上层架构：[安全与治理架构](./index.md)

## 1. 设计范围

本文只定义 Agent `approval_policy = default` 时，平台如何判断一次已经通过基础权限校验、并且没有匹配 Approval 的 Tool Call：

- 直接调用 Tool Backend；
- 或要求用户审批。

本文不定义：

- `approval_policy = auto` 的 Approval Model；
- `approval_policy = allow`；
- Approval Scope 的匹配数据结构；
- Approval Request / Human Inbox 的完整生命周期；
- Agent Capability；
- Execution Policy。

这些内容由各自的架构或详细设计文档负责。

## 2. 基本原则

Default Approval Rules 是平台固定规则，不提供 Project 级配置。

判断发生在基础权限校验与已有 Approval 匹配之后：

~~~text
Tool available
  -> Agent Capability
      -> Execution Policy
          -> Project / Resource Scope
              -> 平台固定安全规则
                  -> Approval Match
                      -> 无匹配 Approval
                          -> Default Approval Rules
~~~

因此 Approval 不负责扩大权限。

一个 Tool Call 如果已经被 Capability、Execution Policy、Project / Resource Scope 或其他平台固定安全规则拒绝，就不会进入 Default Approval Rules。

默认规则遵循以下原则：

1. Agent 正常完成项目工作的常规读写操作应尽量直接执行；
2. 不因为“写操作”本身就要求 Approval；
3. 删除、任意代码执行、外部副作用、安全边界修改等高风险行为默认需要 Approval；
4. 无法可靠判断风险的 Tool 默认需要 Approval；
5. Tool 的风险可以由 Tool 本身决定，也可以由具体参数或目标资源决定。

## 3. Risk 分类

第一阶段使用以下 Risk 分类：

| Risk | 默认行为 | 含义 |
| --- | --- | --- |
| `destructive` | 需要 Approval | 删除、取消、不可逆终态或明显破坏性操作 |
| `arbitrary_execution` | 需要 Approval | 执行任意命令、启动任意进程等通用代码执行能力 |
| `external_side_effect` | 需要 Approval | 对 agenteam 之外的第三方系统产生写入或状态变化 |
| `security_sensitive` | 需要 Approval | 修改 Agent Capability、Runner Mount、Credential 等安全边界 |
| `interactive_control` | 需要 Approval | browser / computer / desktop 自动控制 |
| `project_control` | 需要 Approval | 启停整个 Project 的自动执行机制等项目级控制操作 |
| `unknown` | 需要 Approval | Tool 风险属性未知或无法可靠分类 |
| 无上述 Risk | 直接执行 | 普通项目内读写操作 |

一个 Tool Call 可以同时拥有多个 Risk。

只要命中任意一个默认需要 Approval 的 Risk，本次 Default Approval 结果就是：

~~~text
needs_approval
~~~

否则：

~~~text
allow
~~~

## 4. Builtin Tool 默认规则

### 4.1 普通项目操作

以下 Tool 默认直接执行：

~~~text
Project
- read-project-info
- update-project-info

Milestone
- list-milestones
- create-milestone
- update-milestone

Sprint
- list-sprints
- create-sprint
- update-sprint

Task
- list-tasks
- create-task
- update-task
- move-task
- update-task-plan
- comment-task
- transfer-task

Task Blocker
- list-task-blockers
- add-task-blocker
- resolve-task-blocker

Knowledge
- list-docs
- create-doc
- update-doc
- query-doc

Meeting
- list-meetings
- request-meeting
- add-agent-to-meeting
- remove-agent-from-meeting
- update-meeting
- comment-meeting
- request-decision
- request-execution-approval

Memory
- recall
- retain
- reflect
~~~

这些 Tool 中包含写操作，但它们属于 Agent 正常完成项目工作的基础能力，不应仅因为产生项目内状态变化就要求人工审批。

业务合法性仍由对应 Domain 在 Tool Backend 中校验。

### 4.2 删除 Milestone / Sprint

~~~text
delete-milestone
delete-sprint
~~~

默认 Risk：

~~~text
destructive
~~~

因此默认需要 Approval。

服务端仍然必须执行已有领域约束：

- Milestone 含有 Sprint 时禁止删除；
- Sprint 含有 Task 时禁止删除。

Approval 不能绕过这些约束。

### 4.3 Agent 管理

~~~text
list-agents
-> 直接执行

create-agent
-> security_sensitive
-> 需要 Approval

delete-agent
-> destructive + security_sensitive
-> 需要 Approval
~~~

`update-agent` 使用参数级判断。

普通配置修改：

~~~text
name
description
instructions
model
model parameters
其他不改变权限边界的普通配置
~~~

默认直接执行。

修改以下内容时：

~~~text
allowed tools
Runner mount
Capability
allowed_secret_variables
其他会扩大或改变 Agent 安全边界的配置
~~~

标记：

~~~text
security_sensitive
~~~

默认需要 Approval。

以下配置不允许通过 Agent Tool 修改：

~~~text
approval_policy
approval_model_ref
~~~

它们只能由 Project Owner 通过用户侧配置修改，因此不进入 Default Approval 判断。

### 4.4 Task 状态流转

`transfer-task` 默认直接执行，但根据目标状态进行参数级判断。

普通工作流转：

~~~text
in-progress -> in-review
in-review -> done
in-review -> todo
in-progress / in-review -> blocked
blocked -> todo
~~~

默认直接执行。

如果目标状态是：

~~~text
cancelled
~~~

则标记：

~~~text
destructive
~~~

默认需要 Approval。

Task Domain 仍然负责判断实际状态流转是否合法。

### 4.5 Scheduler

~~~text
start-scheduler
pause-scheduler
~~~

默认 Risk：

~~~text
project_control
~~~

因此默认需要 Approval。

原因是它们改变整个 Project 的自动执行状态，而不是单个 Task 的普通业务操作。

## 5. Runner Tool 默认规则

### 5.1 Filesystem

以下读取类 Tool 默认直接执行：

~~~text
read-file
list-directory
grep-files
find-files
~~~

以下写入类 Tool也默认直接执行：

~~~text
write-file
edit-file
~~~

Runner 文件写入不因为是 write 就自动要求 Approval。

Agent 已经通过：

- Agent Capability；
- Runner Mount；
- Execution Policy；

获得对应工作目录后，应能够正常修改文件，否则 Coding Agent 的常规执行会被频繁打断。

Filesystem Tool 仍必须执行 Mount / path 边界校验。

### 5.2 Command / Process

Runner Command / Managed Process Tool 不使用独立审批路径。

它们与 Builtin / MCP / 其他 Runner Tool 一样：

~~~text
ToolSpec risk metadata
  -> Default Approval Rules
~~~

例如某个 Tool 声明：

~~~text
arbitrary_execution
~~~

则根据统一 Risk 规则进入 Approval。

Security / Governance 不因为 Tool 名称是 `run-command` 而额外增加一套特殊规则。

### 5.3 Managed Process 其他行为

具体 Process Tool 是否需要 Approval 由其 ToolSpec risk metadata 和参数级动态 risk 决定。

读取进程状态：

~~~text
get-process
read-process-output
~~~

默认直接执行。

`stop-process` 根据目标进程来源判断：

~~~text
停止当前 Agent Execution 自己创建的 Managed Process
-> 直接执行

停止其他 Agent / Execution 创建的 Managed Process
-> project_control
-> 需要 Approval
~~~

### 5.4 Desktop

~~~text
screenshot
-> 直接执行
~~~

以下 Tool：

~~~text
automation
browser-use
computer-use
~~~

默认 Risk：

~~~text
interactive_control
~~~

因此默认需要 Approval。

## 6. Risk 的来源

Builtin Tool 和 Runner Tool 的 Risk 由平台静态定义，必要时结合参数动态补充。

例如：

~~~text
transfer-task
  默认无 destructive
  target_state = cancelled
    -> 添加 destructive
~~~

~~~text
update-agent
  修改 description
    -> 无 security_sensitive

  修改 allowed_tools
    -> 添加 security_sensitive
~~~

Risk metadata 只用于 Default Approval 判断等安全逻辑，不替代 Capability 和服务端授权。

## 7. 决策接口

Default Approval Rules 的输入至少需要：

~~~text
agent_id
execution_id
tool_id
tool_source
tool_risk_metadata
tool_arguments
target resource metadata
execution context
~~~

输出保持简单：

~~~text
allow
needs_approval
~~~

Default Approval Rules 不返回“denied”。

真正的权限拒绝应在前面的 Authorization 或后端业务校验中产生。

## 8. 与 Approval 的关系

Default Approval Rules 只会处理已经确认“没有匹配 Approval”的 Tool Call。

当结果为：

~~~text
allow
~~~

继续调用 Tool Backend。

当结果为：

~~~text
needs_approval
~~~

进入用户审批：

~~~text
Approval Match
  -> 无匹配 Approval
      -> Default Approval Rules
          -> allow
              -> Tool Backend
          -> needs_approval
              -> Approval Request / Human Inbox
~~~

用户批准当前请求后，系统记录相应 Approval，并让当前挂起的 Tool Call 直接继续执行；不会再把当前调用重新送回 Approval Match。

已有 Approval 的具体匹配方式以及“以后都允许此类调用”所创建的 Reusable Approval Scope 见 [Approval Scope 详细设计](./approval-scope.md)。One-time Approval 与 Tool retry / idempotency 的关系见 [One-time Approval、Tool Retry 与 Idempotency 详细设计](./one-time-approval-retry-idempotency.md)。
