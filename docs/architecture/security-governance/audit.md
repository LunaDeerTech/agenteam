# Audit 详细设计

> 状态：设计稿
>
> 上层架构：[安全与治理架构](./README.md)
>
> 相关架构：
> - [平台基础设施架构](../platform-infrastructure/README.md)
> - [统一工具系统架构](../tool-system/README.md)
> - [Agent Executor 架构](../agent-executor/README.md)
> - [Runner 架构](../runner/README.md)
> - [Approval Scope 详细设计](./approval-scope.md)
> - [One-time Approval、Tool Retry 与 Idempotency 详细设计](./one-time-approval-retry-idempotency.md)

## 1. 设计目标

Audit 用于回答：

> 谁在什么时间，以什么身份和权限，对什么敏感资源执行了什么安全相关操作，结果是什么？

Audit 不是业务 Timeline，也不是 Agent Execution 的完整运行日志。

系统中三类记录保持明确边界：

~~~text
Task / Meeting Event
= 业务与协作发生了什么

Agent Execution / Tool Call Log
= Agent 实际运行了什么

Audit
= 谁以什么权限执行了什么敏感操作
~~~

Audit 的目标是长期保留结构化安全证据，而不是复制完整运行内容。

## 2. 第一阶段原则

第一阶段确定：

1. Audit 采用 append-oriented 设计；
2. 已产生的 Audit Record 不能通过普通业务接口修改；
3. Audit 不自动过期；
4. 不提供 Project 级 retention 配置；
5. 不把完整 Tool arguments、command、stdout、stderr、Tool Result、Model Prompt / Response 复制进 Audit；
6. Audit 通过结构化 ID 与 Agent Execution、Tool Call、Approval、Runner、Resource 等对象关联；
7. 查询以结构化过滤为主，不做全文搜索；
8. 第一阶段不依赖 Elasticsearch / OpenSearch 等独立检索系统；
9. Secret plaintext 永远不能进入 Audit。

## 3. Audit Scope

Audit 支持两种 scope：

~~~text
AuditScope
├── project
└── system
~~~

### 3.1 Project Audit

Project Audit 记录当前 Project 内发生的安全相关行为。

例如：

- Project 配置修改；
- Agent Capability / Secret 白名单修改；
- Approval 创建、批准、拒绝、撤销；
- Tool Authorization deny；
- 高风险 Tool Operation；
- Project Secret 创建、修改、删除、使用；
- Project-scoped MCP 配置修改；
- Agent Execution 中需要长期追溯的安全事件。

Project Owner 可以查询当前 Project 的 Audit。

### 3.2 System Audit

System Audit 记录不属于单一 Project 的平台级行为。

例如：

- Runner enrollment / revoke；
- System-scoped Model / MCP 配置修改；
- System credential 修改；
- 平台级配置修改。

Project Owner 身份不自动获得 System Audit 查询权限。

具体系统管理权限由 Platform / System Configuration 定义。

## 4. Audit Record 数据模型

概念结构：

~~~text
AuditRecord
├── id
├── scope
│   ├── project
│   └── system
├── project_id?
├── created_at
│
├── actor
│   ├── type
│   │   ├── user
│   │   ├── agent
│   │   └── system
│   └── id?
│
├── action
├── outcome
│   ├── success
│   ├── denied
│   ├── failed
│   └── unknown
│
├── resource
│   ├── type?
│   └── id?
│
├── tool_id?
├── execution_id?
├── tool_call_id?
├── operation_id?
├── request_id?
├── approval_id?
├── runner_id?
├── correlation_id?
│
└── metadata
~~~

字段语义：

- id：Audit Record 唯一 ID；
- scope：project / system；
- project_id：Project-scoped Audit 必填；
- created_at：事件发生时间；
- actor：谁触发了操作；
- action：稳定机器可读 action；
- outcome：最终安全相关结果；
- resource：主要目标资源；
- tool_id：如果事件来自 Tool System，则保存 stable Tool ID；
- execution_id / tool_call_id / operation_id：关联 Agent Execution 和 Tool Operation；
- request_id：必要时关联具体 backend / Runner Attempt；
- approval_id：关联 One-time / Reusable Approval；
- runner_id：关联 Runner；
- correlation_id：跨模块追踪同一事件链；
- metadata：少量结构化、脱敏后的附加信息。

## 5. Actor

Actor 表示安全语义上的操作发起者。

### User

~~~text
actor.type = user
actor.id = user_id
~~~

用于 Project Owner 或系统用户操作。

### Agent

~~~text
actor.type = agent
actor.id = agent_id
~~~

即使实际请求由 Central / Runner 代为执行，安全语义上的 Actor 仍是触发该 Tool Operation 的 Agent。

execution_id 用于进一步关联本次实际 Agent Execution。

### System

~~~text
actor.type = system
~~~

用于系统自身产生的动作，例如：

- 自动状态修复；
- system lifecycle；
- 后台安全维护任务。

如果系统动作是代表某个 User / Agent 执行，应优先保留真实安全 Actor，而不是统一写成 system。

## 6. Action

action 使用稳定机器可读名称。

示例：

~~~text
project.update
agent.capability.update
agent.secret_access.update

approval.request.create
approval.approve
approval.reject
approval.revoke

secret.create
secret.update
secret.delete
secret.resolve

tool.authorization.deny
tool.execute

runner.enroll
runner.revoke
~~~

action 不是 UI 文案。

UI 可以根据 action 映射成人类可读描述。

不要把完整 command 或 Tool arguments 拼进 action。

## 7. Outcome

统一使用：

~~~text
success
denied
failed
unknown
~~~

语义：

- success：操作成功；
- denied：Authorization / Policy / Approval 等明确拒绝；
- failed：已经允许执行，但 backend / domain 执行失败；
- unknown：系统无法确认是否已经产生副作用或最终结果。

unknown 尤其适用于分布式 Runner / 外部系统执行。

对于同一个 Tool Operation 的多个 Attempt，可以：

- Execution Log 记录完整 Attempt 时间线；
- Audit 只在具有安全追溯价值时记录 attempt-level outcome；
- operation_id / request_id 用于把两者关联起来。

## 8. Resource

resource 表示 Audit 事件的主要安全目标。

例如：

~~~text
resource.type = agent
resource.id = agent-123
~~~

或：

~~~text
resource.type = project_secret
resource.id = secret-456
~~~

对于 Tool Operation，如果没有单一业务 Resource，可以依赖：

- tool_id；
- operation_id；
- runner_id；
- metadata 中的结构化目标摘要。

Audit 不需要复制复杂 Tool Scope。

详细 Scope 保留在 Tool Call / Approval 数据中。

## 9. Metadata 边界

metadata 只保存用于安全检索和理解事件的少量结构化信息。

允许示例：

~~~text
changed_fields = ["allowed_tools", "allowed_secret_variables"]
risk = ["arbitrary_execution"]
approval_mode = "one_time"
secret_name = "GITHUB_TOKEN"
mount_id = "mount-123"
tool_source = "runner"
~~~

禁止保存：

- Secret value；
- credential plaintext；
- API key；
- private key；
- 完整 command；
- 完整 stdout / stderr；
- 完整 Tool Result；
- 未脱敏的大体积 Tool arguments；
- Model Prompt / Response；
- Approval Model 完整 Context；
- 可能包含 Secret 的原始 Provider Response。

如果某个字段可能包含敏感数据，应保存稳定 ID、类型、hash / fingerprint 或脱敏摘要，而不是原始值。

## 10. Audit 与 Execution Log 的边界

例如 Agent 执行一个 Tool：

~~~text
tool = runner.run-command
operation_id = op-123
~~~

Execution / Tool Call Log 可以保存：

- 实际 Tool arguments；
- command；
- cwd；
- stdout / stderr；
- Attempt 时间线；
- duration；
- retry；
- backend error；
- Tool Result。

Audit 只保存类似：

~~~text
action = tool.execute
actor = agent:A
tool_id = runner.run-command
execution_id = exec-1
tool_call_id = tc-1
operation_id = op-123
runner_id = runner-1
resource.type = runner_mount
resource.id = mount-1
approval_id = approval-1
outcome = success
~~~

如果需要查看实际 command 或输出，UI 从 Audit Record 跳转到关联的 Tool Call / Agent Execution。

因此：

> Audit 是长期安全索引与证据，不是第二份 Execution Log。

## 11. 哪些行为进入 Audit

第一阶段至少记录：

### Project / Agent

- Project 创建；
- Project 删除 / purge；
- Project 关键配置修改；
- Agent 创建 / 删除；
- Agent Capability 修改；
- Agent Runner Mount 修改；
- Agent Secret Variable 白名单修改；
- Agent Approval Policy / Model 配置修改。

### Approval

- Approval Request 创建；
- 用户批准；
- 用户拒绝；
- Reusable Approval 创建；
- Reusable Approval 撤销。

One-time Approval 不设计独立 consume 动作，因此不存在 approval.consume Audit 事件。

### Secret

- Secret 创建；
- Secret value 覆盖更新；
- Secret 删除；
- Secret 被执行后端解析使用。

Secret 使用 Audit 只记录：

- secret id / name；
- actor；
- execution / operation；
- target backend metadata。

不记录 Secret value。

### Tool / Authorization

- Authorization deny；
- 需要长期安全追溯的高风险 Tool Operation；
- destructive / security-sensitive / arbitrary-execution / external-side-effect 等敏感 Tool 执行；
- backend 返回 unknown outcome 的敏感操作。

普通、低风险且已经完整记录在 Execution Log 的 Tool Call 不要求全部复制到 Audit。

### Runner / System

- Runner enrollment；
- Runner revoke；
- Runner Device Identity 安全变更；
- System-scoped sensitive configuration change。

## 12. Append-oriented 语义

Audit Record 创建后不能通过普通业务 API：

- update；
- replace；
- delete 单条记录。

如果某个安全状态发生变化，应追加新的 Audit Record。

例如：

~~~text
approval.approve
...
approval.revoke
~~~

是两条独立记录。

不能把原 approve Audit Record 修改成 revoked。

这样 Audit 能保留真实事件历史。

## 13. Retention

第一阶段：

> Audit 不自动过期。

不提供：

- 30 天；
- 90 天；
- 1 年；
- Project 自定义 retention；

等自动删除策略。

### 13.1 Project soft delete

Project 被 soft delete 后，其 Audit 继续保留。

这样仍然可以追溯删除前后的安全事件。

### 13.2 Project permanent purge

当用户执行 Project 的永久数据 purge 时，Project-scoped Audit 与该 Project 的其他持久数据一起删除。

因此“无自动过期”不代表永久绕过用户主动的数据删除。

### 13.3 System Audit

System Audit 第一阶段同样不自动过期。

未来如果出现企业合规、存储成本或监管需求，可以增加平台级 retention / archive policy，但不属于第一阶段。

## 14. 查询入口

Project 内提供统一：

~~~text
Project
└── Audit
~~~

页面。

Agent 等资源页面可以提供 Audit 入口，但本质上仍查询同一份 Project Audit。

例如 Agent 页面：

~~~text
Agent
└── Audit
~~~

等价于：

~~~text
project_id = current_project
AND
(actor = current_agent OR resource = current_agent)
~~~

不建立独立 Agent Audit Store。

## 15. 查询维度

第一阶段支持结构化过滤：

- time range；
- actor type；
- actor id；
- action；
- outcome；
- resource type；
- resource id；
- tool id；
- execution id；
- operation id；
- approval id；
- runner id。

可以组合过滤。

例如：

~~~text
Project A
+ Agent B
+ action = tool.execute
+ outcome = denied
+ last 7 days
~~~

第一阶段不提供：

- 全文搜索；
- 对 command / stdout 搜索；
- 对任意 metadata JSON 做无约束全文检索。

需要运行细节时跳转到 Execution / Tool Call。

## 16. 查询结果

列表至少展示：

~~~text
time
actor
action
resource / target
outcome
summary
~~~

并提供关联对象跳转：

- Agent；
- Agent Execution；
- Tool Call；
- Approval；
- Runner；
- Project resource。

详情页展示：

- AuditRecord 结构化字段；
- metadata；
- correlation / reference IDs；
- 关联 Execution / Approval / Runner 等对象。

## 17. Pagination

Audit 是按时间增长的 append-oriented 数据。

第一阶段使用 cursor pagination，而不是 offset pagination。

推荐排序：

~~~text
created_at DESC
id DESC
~~~

cursor 包含：

~~~text
created_at
id
~~~

这样在不断产生新 Audit Record 时分页更稳定。

## 18. 数据库索引

第一阶段至少考虑：

~~~text
(project_id, created_at, id)
(actor_type, actor_id, created_at)
(action, created_at)
(resource_type, resource_id, created_at)

execution_id
operation_id
approval_id
runner_id
~~~

实际索引应根据数据库查询计划调整。

不需要为第一阶段引入独立全文搜索引擎。

## 19. Correlation

Audit 通过 ID 与其他运行记录关联。

典型链路：

~~~text
AuditRecord
  -> execution_id
      -> Agent Execution

  -> tool_call_id
      -> Tool Call

  -> operation_id
      -> Tool Operation / Attempts

  -> approval_id
      -> Approval

  -> runner_id
      -> Runner
~~~

对于跨模块事件可以附带 correlation_id。

correlation_id 用于方便追踪，不替代各领域实体自己的稳定 ID。

## 20. UI 权限

Project Audit：

~~~text
current_user == project.owner_user_id
~~~

才允许查询。

Agent 不能通过普通 Agent Tool 查询 Project Audit，除非未来明确提供受控 Audit Tool。

System Audit 由 System Configuration / 平台管理权限控制，不继承 Project Owner 权限。

## 21. 第一阶段结论

第一阶段确定：

1. Audit 为独立 append-oriented 安全记录；
2. Audit 与 Task Event / Meeting Timeline / Execution Log 分离；
3. Audit 不自动过期；
4. Project 不提供 retention 配置；
5. Project soft delete 后 Audit 保留；
6. Project permanent purge 时删除对应 Project Audit；
7. System Audit 同样不自动过期；
8. Audit 保存结构化安全 metadata 和关联 ID；
9. 不复制完整 command、stdout、stderr、Tool Result、Model Prompt / Response；
10. Secret plaintext 永远不进入 Audit；
11. Project 提供统一 Audit 查询页；
12. Agent / Resource 页面只是带过滤条件的同一 Audit 查询；
13. 支持 time / actor / action / outcome / resource / tool / execution / operation / approval / runner 等结构化过滤；
14. 使用 created_at + id cursor pagination；
15. 第一阶段不做全文搜索，也不引入独立搜索引擎；
16. Audit 通过 execution_id、tool_call_id、operation_id、approval_id、runner_id 等跳转到详细运行证据。
