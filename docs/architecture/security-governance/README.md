# 安全与治理架构

> 状态：设计稿

## 1. 安全模型总览

Security / Governance 负责回答一个核心问题：

> 某个用户或 Agent，在当前上下文中，是否可以执行某个操作？

整个系统的安全模型可以概括为：

~~~mermaid
flowchart LR
    User["用户"] --> Ownership["Project Owner 校验"]
    Ownership --> UserAction["项目级用户操作"]
    UserAction --> UserBackend["Domain / Platform Backend"]

    Agent["Agent"] --> Capability["Agent Capability"]
    Capability --> Execution["Agent Execution"]
    Execution --> Policy["Execution Policy"]
    Policy --> ToolCall["Tool Call"]

    ToolCall --> Auth["Tool Authorization"]
    Auth --> Resource["Project / Resource Scope<br/>+ 平台固定安全规则"]
    Resource --> Approval["Approval Match"]

    Approval -->|"存在匹配"| Execute["Tool Backend"]
    Approval -->|"无匹配"| ApprovalPolicy["Approval Policy"]

    ApprovalPolicy -->|"直接执行"| Execute
    ApprovalPolicy -->|"需要用户审批"| Human["Human Inbox / 用户审批"]
    Human -->|"批准"| Execute

    Execute --> Audit["Audit"]
    UserBackend --> Audit
~~~

这里有两条不同的权限路径：

- 用户作为 Project Owner 执行项目级操作；
- Agent 通过 Agent Capability 获得长期能力，再由单次 Agent Execution 的 Execution Policy 进一步收紧。

两条路径最终都必须经过服务端 Authorization。

### 1.1 基本原则

安全模型遵循以下原则：

- 默认拒绝，没有明确授权就不能执行；
- 最小权限，下层约束只能缩小权限，不能扩大权限；
- 服务端授权结果具有最终权威性；
- Project 是基本的数据隔离边界；
- Agent 的权限与 Human User 的权限分开建模；
- Human Approval 只能确认一个本来可授权但需要人工确认的动作，不能突破原有权限；
- Secret value 不暴露给 Agent Model；允许 Agent 使用的 Secret 可以把变量名和 description 暴露给模型；
- 敏感操作必须可以追溯。

### 1.2 不属于本文档范围

本文档不负责：

- 用户登录、Session、OAuth、密码等 Authentication；
- Task、Meeting 等业务自己的状态机；
- Runner Enrollment 的具体认证协议；
- Agent Loop 的模型调用逻辑；
- Runner 的具体 OS sandbox 实现。

这些模块仍然需要遵守本文档定义的安全边界。

## 2. Project Owner

每个 Project 只绑定一个 Owner。

项目级人类操作的授权模型保持简单：

~~~text
User
  -> Project.owner_user_id
      -> 当前用户是否为 Owner
          -> 是：允许进入项目级操作
          -> 否：拒绝
~~~

Project Owner 拥有该 Project 的全部人类侧管理能力，包括：

- Project 配置；
- Milestone / Sprint / Task；
- Meeting；
- Agent；
- Knowledge；
- Agent Execution；
- Approval；
- Project-level Model / MCP 配置；
- Project Environment Variables / Secret Variables；
- Human Inbox 中属于该 Project 的待处理事项。

不再为 Project 内的人类操作拆分 Role 或 Permission Matrix。

### 2.1 Project 边界

Project-scoped 资源必须同时验证：

~~~text
resource.project_id == 当前 project_id
AND
project.owner_user_id == 当前 user_id
~~~

知道某个对象 ID 不代表拥有访问权限。

Agent Execution 同样固定属于一个 Project，Agent 不能因为知道其他 Project 的对象 ID 就跨越 Project 边界。

### 2.2 系统级资源

Runner、系统级 Model Provider、系统级 MCP、系统配置等属于 System Scope。

Project Owner 身份只对当前 Project 有效，不自动获得系统级资源管理能力。

系统级资源的管理边界由 Platform / System Configuration 负责，不通过 Project Owner 权限向下继承。

## 3. Agent 权限

Agent 权限与 Project Owner 的人类操作权限独立建模。

Agent 的长期权限由 Agent Capability 描述。

例如一个 Agent 可以被配置为：

- 可以读取和修改 Task；
- 可以查询 Knowledge Base；
- 可以调用某些 MCP Tool；
- 可以使用某个 Runner；
- 可以访问某个 Mount；
- 不允许使用其他高风险 Tool。

概念上：

~~~text
Agent
  -> Agent Capability
      -> Agent Execution
          -> Execution Policy
              -> Tool Set
~~~

### 3.1 Agent Capability

Agent Capability 表示：

> 这个 Agent 长期最多允许做什么。

它可以包含：

- Builtin Tool；
- MCP Tool；
- Runner Tool；
- Runner / Mount；
- Integration；
- 其他长期能力配置。

Agent Capability 是权限上限，不是每次执行都自动获得全部能力。

### 3.2 Execution Policy

每次 Agent Execution 都可以进一步限制 Agent Capability。

例如：

#### Task work

允许完成任务所需的写操作。

#### Task review

允许审核所需能力，但可以限制不相关操作。

#### Meeting response

默认以读取和讨论为主，写操作通常需要人工审批。

因此：

~~~text
Effective Agent Permission
=
Agent Capability
∩ Execution Policy
~~~

Execution Policy 只能收紧，不能扩大 Agent Capability。

### 3.3 Core Agent Tools

现有设计中的 Core Agent Tools：

- query-doc；
- recall；
- retain；
- reflect；
- list-mcp-resources；
- read-mcp-resource；
- list-artifacts；
- create-artifact；
- read-artifact；

不会被普通 Agent Capability 开关移除。

但它们仍然只能：

- 查询当前 Project 的 Knowledge；
- 访问当前 Agent 自己的 Memory；
- 访问当前 Project 中 enabled MCP Connection 的 Resource Catalog / Resource；
- 列举、创建和读取当前 Project 中当前调用方有权访问的 Artifact；
- 遵守服务端 Authorization。

“Core”不等于“跳过权限”。

## 4. 一次 Tool Call 如何被授权

Agent 权限最终必须落实到具体 Tool Call。

这一过程拆成三部分：

1. 总体授权流程；
2. Approval Policy 子流程；
3. 用户审批子流程。

### 4.1 总体流程

总体流程依次处理三件事：

- 当前 Tool Call 是否具备基础权限；
- 是否已经存在可以覆盖当前操作的有效 Approval；
- 如果没有匹配 Approval，再由 Approval Policy 决定直接执行还是进入用户审批。

~~~mermaid
flowchart TD
    Call["Agent 发起 Tool Call"]

    Capability{"Tool 属于 Agent Capability？"}
    ExecPolicy{"Execution Policy 允许？"}
    ScopePolicy{"Project / Resource Scope<br/>及平台固定安全规则允许？"}

    Match{"存在匹配的 Approval？"}
    ApprovalPolicy["执行 Approval Policy 子流程"]
    UserApproval["进入用户审批子流程"]

    Backend["调用 Tool Backend"]
    BackendValidation{"Domain / MCP / Runner<br/>最终校验"}

    Execute["执行"]
    Deny["拒绝"]
    Audit["记录 Tool Call / Approval / Audit"]

    Call --> Capability
    Capability -->|"否"| Deny
    Capability -->|"是"| ExecPolicy

    ExecPolicy -->|"否"| Deny
    ExecPolicy -->|"是"| ScopePolicy

    ScopePolicy -->|"否"| Deny
    ScopePolicy -->|"是"| Match

    Match -->|"存在"| Backend
    Match -->|"不存在"| ApprovalPolicy

    ApprovalPolicy -->|"直接执行"| Backend
    ApprovalPolicy -->|"需要用户审批"| UserApproval

    UserApproval -->|"批准"| Backend
    UserApproval -->|"拒绝"| Deny

    Backend --> BackendValidation
    BackendValidation -->|"合法"| Execute
    BackendValidation -->|"非法"| Deny

    Execute --> Audit
    Deny --> Audit
~~~

这里的关键边界是：

> Approval 不负责赋予 Agent 原本没有的 Capability。

如果 Agent Capability、Execution Policy、Project / Resource Scope 或平台固定安全规则已经拒绝，流程直接结束，不进入 Approval 匹配或 Approval Policy。

Approval 匹配发生在 Approval Policy 之前。已有 Approval 可以直接短路后续审批判断，但不能跳过前面的基础授权。

如果没有匹配 Approval，才执行 Approval Policy 子流程。

如果最终进入用户审批，用户对当前 Approval Request 的批准本身就授权当前挂起的 Tool Call。系统记录相应 Approval 后，当前 Tool Call 直接继续调用 Tool Backend，不再重新执行一次 Approval 匹配。

### 4.2 Approval Policy 子流程

Agent 配置一个 `approval_policy`，用于决定一个已经通过基础权限校验、并且没有匹配 Approval 的 Tool Call：

> 直接调用 Tool Backend，还是进入用户审批。

当前确认三个模式：

- 默认权限；
- 自动审批；
- 完全允许。

三种模式在配置层面是平级选项，但自动审批模式会先复用默认权限规则，只对默认规则原本要求审批的操作调用 Approval Model。

~~~mermaid
flowchart TD
    Start["没有匹配 Approval<br/>进入 Approval Policy 子流程"]

    Mode{"Agent Approval Policy"}

    Default["执行默认权限规则"]
    AutoDefault["先执行默认权限规则"]
    AutoModel["调用配置的 Approval Model"]
    Allow["完全允许"]

    DefaultDecision{"默认规则结果"}
    AutoDefaultDecision{"默认规则结果"}
    AutoDecision{"Approval Model 结果"}

    Direct["直接执行"]
    NeedHuman["需要用户审批"]

    Start --> Mode

    Mode -->|"默认权限"| Default
    Mode -->|"自动审批"| AutoDefault
    Mode -->|"完全允许"| Allow

    Default --> DefaultDecision
    DefaultDecision -->|"允许直接执行"| Direct
    DefaultDecision -->|"需要审批"| NeedHuman

    AutoDefault --> AutoDefaultDecision
    AutoDefaultDecision -->|"允许直接执行"| Direct
    AutoDefaultDecision -->|"需要审批"| AutoModel

    AutoModel --> AutoDecision
    AutoDecision -->|"允许直接执行"| Direct
    AutoDecision -->|"需要审批"| NeedHuman

    Allow --> Direct
~~~

#### 默认权限

使用平台固定规则、Tool risk metadata 和当前执行上下文决定：

- 直接执行；
- 或需要用户审批。

具体 Tool / Action / Risk 的固定判断规则不属于架构层，见 [默认审批策略详细设计](./default-approval-policy.md)。

#### 自动审批

Agent 额外配置一个 `approval_model_ref`。

自动审批不是让 Approval Model 判断所有 Tool Call。

它首先执行与默认权限模式相同的固定规则：

~~~text
默认规则允许直接执行
  -> 直接执行

默认规则需要审批
  -> 调用 Approval Model
~~~

Approval Model 再根据当前 Tool Call 和必要上下文输出：

- 允许直接执行；
- 需要用户审批。

Approval Model 只是审批判断组件，不扩大 Agent Capability，也不能绕过前面的基础授权。

如果 Approval Model 调用失败、结果无效或无法形成可靠判断，应保守地进入用户审批，而不是默认放行。

Approval Model 的输入上下文、输出 Schema 与失败处理等具体规则不属于架构层，见 [自动审批模型详细设计](./auto-approval-model.md)。

#### 完全允许

跳过 Approval 判断，直接调用 Tool Backend。

“完全允许”仍然不会绕过：

- Agent Capability；
- Execution Policy；
- 平台固定安全规则；
- Project / Resource Scope；
- Tool Backend 自身的业务或本地安全校验。

### 4.3 Tool Set 只是前置过滤

Agent Executor 在创建 Agent Execution 时，会根据：

- 当前 Tool Registry 中已经注册的 Tool；
- Agent Capability；
- Execution Policy；

计算本次允许暴露给模型的 Tool Set。

这样模型不会看到明显不能使用的 Tool。

但这不是最终授权。

真正执行每次 Tool Call 时，Tool System 仍必须重新校验。

### 4.4 Tool 参数也需要参与校验

不能只判断：

~~~text
Agent 能不能调用 update-task
~~~

还必须判断：

~~~text
它准备修改哪个 Task
这个 Task 是否属于当前 Project
这个具体操作是否允许
~~~

Runner Tool 同理。

不能只判断：

~~~text
Agent 能不能调用 run-command
~~~

还需要检查：

- 使用哪个 Runner；
- 使用哪个 Mount；
- 当前 Agent 是否拥有这个 Mount；
- 当前 Execution Policy 是否允许。

### 4.5 Tool System 与业务服务的职责不同

Tool System 负责：

> 当前 Agent Execution 有没有权限发起这个操作？

业务服务负责：

> 这个操作在当前业务状态下是否合法？

例如 Agent 有 transfer-task 权限，也不能执行：

~~~text
in_progress -> done
~~~

因为 Task Domain 会根据状态机拒绝。

权限校验不能替代业务规则。

### 4.6 Builtin / MCP / Runner 使用同一套入口

无论 Tool 来自：

- Builtin；
- MCP；
- Runner；

都必须经过统一 Tool Authorization。

MCP 不能因为来自外部协议就绕过权限。

Runner 的本地安全边界按 Capability 区分：

- Filesystem Tool 必须严格限制在当前 Agent Workspace 内，并执行 path / symlink containment；
- Command / Managed Process 第一阶段采用 trusted-host 模式，只要求 cwd 位于当前 Workspace，进程本身拥有 Runner OS user 的实际系统权限。

因此 Runner 不维护一套与 Central 重复的本地 Authorization Policy。Central 完成 Tool Authorization 后，Runner 只继续执行协议完整性、Workspace / path 和实际 Capability 等运行时校验。

## 5. Approval 与用户审批

Approval 用于记录已经被批准、并且可以被后续 Tool Call 匹配的授权范围。

Approval 不是新的超级权限，也不改变 Agent Capability。

### 5.1 Approval 匹配

在基础权限校验通过后、执行 Approval Policy 之前，系统先检查是否已经存在匹配的有效 Approval。

~~~text
基础权限通过
  -> 查找匹配 Approval
      -> 存在：调用 Tool Backend
      -> 不存在：执行 Approval Policy
~~~

因此，一个已经存在的 Approval 可以避免再次执行默认审批判断或 Approval Model。

Approval 仍然不能绕过：

- Agent Capability；
- Execution Policy；
- Project / Resource Scope；
- 平台固定安全规则。

只有这些基础校验已经通过，Approval 才参与后续授权。

### 5.2 用户审批子流程

当没有匹配 Approval，并且 Approval Policy 的最终结果是“需要用户审批”时，Security / Governance 创建统一 Approval Request，并通过 Human Inbox 暴露给用户。

~~~mermaid
flowchart TD
    NeedHuman["Approval Policy<br/>需要用户审批"]

    Request["创建 Approval Request"]
    Inbox["Human Inbox"]
    Decision{"用户处理"}

    Once["批准本次"]
    Similar["以后都允许此类调用"]
    Reject["拒绝"]

    Exact["创建一次性 Approval"]
    Reusable["创建 Reusable Approval"]

    Continue["当前 Tool Call<br/>直接继续"]
    Deny["拒绝 Tool Call"]

    NeedHuman --> Request
    Request --> Inbox
    Inbox --> Decision

    Decision -->|"仅本次允许"| Once
    Decision -->|"以后都允许此类调用"| Similar
    Decision -->|"拒绝"| Reject

    Once --> Exact
    Similar --> Reusable

    Exact --> Continue
    Reusable --> Continue
    Reject --> Deny
~~~

用户批准时，系统仍然需要产生结构化 Approval 记录：

- “批准本次”产生绑定当前操作的一次性 Approval；
- “以后都允许此类调用”产生可供后续操作匹配的 Reusable Approval。

但对当前正在等待审批的 Tool Call 来说，用户刚刚作出的批准决定本身已经完成授权，因此记录 Approval 后直接继续调用 Tool Backend，不再把当前 Tool Call 打回 Approval 匹配步骤重新判断一次。

### 5.3 Approval Request 与 Human Inbox

Approval 不属于 Meeting 专属能力。

任何 Agent Execution 在执行过程中产生的审批需求，都由 Security / Governance 持久化为统一 Approval Request，并通过 Human Inbox 暴露给用户处理。

~~~text
Agent Execution / Tool Authorization
  -> Approval Request
      -> Human Inbox
          -> Human User
~~~

Approval Request 至少需要能够关联：

- Project；
- Agent；
- source Agent Execution；
- tool_call_id；
- operation_id；
- Tool / Action；
- 目标 Resource / Scope；
- 触发审批的业务来源，例如 Task / Meeting。

Human Inbox 是 pending Approval Request 的统一聚合和直接处理入口，不保存另一套审批状态。

平台应提供可复用的 Approval Request 交互组件 / Action Contract，使以下入口共享同一套 approve / reject 行为：

- Human Inbox；
- Meeting Timeline；
- Task / Agent Execution Detail。

这些 UI 都直接调用 Security / Governance 的统一 Approval API。业务页面只负责提供上下文和展示位置，不实现自己的审批状态机。

如果审批来源于 Meeting，Meeting Timeline 可以引用、展示并直接处理同一个 Approval Request；如果来源于 Task Execution，也可以从 Task / Agent Execution 页面直接处理同一个 Approval Request。

因此不同页面只是同一审批对象的不同入口：

~~~text
Governance Approval Request
  ├── Human Inbox
  ├── Meeting Timeline
  └── Task / Execution UI
~~~

Approval Request 的最终状态仍由 Security / Governance 统一维护。

### 5.4 Approval Scope

Approval 分为：

~~~text
批准本次
-> One-time Approval

以后都允许此类调用
-> Reusable Approval
~~~

Reusable Approval 是 Agent 级的长期 Approval，但仍作为独立 Approval 实体持久化。

Approval Scope 必须是可机器匹配的结构化范围，不能依赖自然语言相似度。

所有 Tool 共用的固定匹配维度是：

- Project；
- Agent；
- stable Tool ID；
- Approval state。

Tool-specific Scope 则由具体 Tool 自己定义。

每个需要 Approval 的 Tool 可以提供 `ApprovalScopeResolver`，负责：

- 生成当前操作的 scope；
- 决定是否支持 Reusable Approval；
- 如果支持，生成“以后都允许此类调用”对应的 Reusable Approval Scope；
- 判断后续 Tool Call 是否命中已有 Reusable Approval。

不是所有 Tool 都支持 Reusable Approval。

如果 Tool 不支持，审批界面只显示：

~~~text
批准本次
拒绝
~~~

如果 Tool 支持，则可以额外显示：

~~~text
以后都允许此类调用
~~~

Agent 不能自己构造或扩大 Reusable Approval Scope。Scope 必须由当前真实 Tool Call 和 Tool 的 `ApprovalScopeResolver` 生成。

Reusable Approval：

- 不绑定 Agent Execution；
- 不设置过期时间；
- 持续有效直到 Project Owner 撤销；
- 每次 Tool Call 都重新匹配当前 active Reusable Approval；
- 撤销后因为不再处于 active 状态，后续调用自然无法匹配。

Reusable Approval 的完整数据模型、Tool-defined Scope、创建、匹配和 Agent 配置页管理见 [Approval Scope 详细设计](./approval-scope.md)。

### 5.5 Approval 的机器校验

给用户展示的 Scope 可以使用自然语言说明，但真正用于服务端判断的是结构化 Approval Scope。

Reusable Approval 匹配时，Security / Governance 先确认：

- 属于当前 Project；
- 属于当前 Agent；
- stable Tool ID 与当前 Tool 相同；
- `state = active`。

然后由当前 Tool 的 `ApprovalScopeResolver.matches(saved_scope, current_call)` 判断 Tool-specific Scope 是否覆盖当前调用。

One-time Approval 绑定当前 Tool Operation 的 operation_id 和 operation fingerprint，不绑定某一次底层执行 Attempt。

同一个 Tool Operation 的安全 technical retry 可以继续使用该 One-time Approval；新的 Agent Tool Call 则创建新的 operation_id，即使参数相同也不能复用。

Approval 只确认“这个 Operation 是否被授权”，不决定 retry 是否安全。retry 仍必须遵守具体 Tool / Backend 的 idempotency 与 outcome 规则。

详细设计见 [One-time Approval、Tool Retry 与 Idempotency 详细设计](./one-time-approval-retry-idempotency.md)。

Approval Match 不根据 risk category、自然语言描述或相似度扩展授权范围。

## 6. Secret 与 Audit

Secret 与 Audit 都属于安全体系的重要辅助机制，但它们不参与 Agent 权限主流程本身。

### 6.1 Project Environment Variables 与 Secret

Project 可以配置普通 Variable 和 Secret Variable。

普通 Variable：

- 对当前 Project 的所有 Agent 可见；
- 可以把 name / description / value 注入 AgentExecutionContext；
- 可以在执行后端作为环境变量使用。

Secret Variable：

- Secret value 加密存储；
- Project Owner 为具体 Agent 选择可用 Secret，形成 Agent Secret Variable 白名单；
- 白名单只保存 Secret Variable 引用，不复制明文；
- 当前 Agent 被允许使用的 Secret 可以把 name / description / Secret 标记放入 AgentExecutionContext；
- Secret value 不进入 Model Context；
- Secret value 只在 Runner、MCP Backend 或其他执行后端需要时解析。

因此：

~~~text
Project Environment Variables
  ├── Variable
  │    -> 所有 Agent 可见
  │
  └── Secret
       -> Agent allowed_secret_variables
            -> 执行后端按需解析
~~~

把 Secret 加入 Agent 白名单，代表允许该 Agent 的执行环境使用这个 Secret，因此修改白名单属于 security-sensitive 配置。

Secret value 不应出现在：

- Model Context；
- Task Event；
- Meeting Message；
- 普通 Tool arguments；
- 普通 Execution Log；
- Audit metadata。

Runner / Tool backend 对已知 Secret value 应执行日志 masking，但 masking 只降低意外泄漏风险，不替代 Agent Secret 白名单这一权限边界。

Project-scoped MCP 等后端配置可以通过 credential reference 引用 Project Secret Variable；这类 backend binding 不等于把该 Secret 加入 Agent 的环境变量白名单。

Runner 自己的 Device Private Key 只保存在 Runner 本地，不属于 Project Environment Variables。

详细设计见 [项目变量与 Secret 详细设计](../project-work-management/project-environment-variables.md)。

### 6.2 Audit

Audit 是独立的 append-oriented 安全记录，用于回答：

> 谁在什么时间，以什么身份和权限，对什么敏感资源执行了什么操作，结果是什么？

Audit 与 Task Event、Meeting Timeline、Agent Execution Log 保持分离。

第一阶段：

- Audit 不自动过期；
- 不提供 Project 级 retention 配置；
- Project soft delete 后 Audit 保留；
- Project permanent purge 时删除对应 Project Audit；
- Audit 只保存结构化安全 metadata 和关联 ID；
- 完整 Tool arguments、command、stdout / stderr、Tool Result、Model Prompt / Response 不复制进 Audit；
- Secret plaintext 永远不能进入 Audit；
- Project 提供统一 Audit 查询入口；
- 查询支持 time / actor / action / outcome / resource / tool / execution / operation / approval / runner 等结构化过滤；
- 第一阶段不做全文搜索，也不引入独立搜索引擎。

完整数据模型、retention、query、pagination、index 和 correlation 设计见 [Audit 详细设计](./audit.md)。

### 6.3 最终执行责任

用户和 Agent 使用不同的授权路径，最终都由服务端执行实际校验：

~~~mermaid
flowchart LR
    User["User"] --> Owner["Project Owner 校验"]
    Owner --> UserAuth["Project Authorization"]

    Agent["Agent"] --> Capability["Agent Capability"]
    Capability --> Execution["Execution Policy"]
    Execution --> AgentAuth["Tool Authorization"]

    UserAuth --> Backend["Domain / Platform Backend"]
    AgentAuth --> ApprovalMatch["Approval Match"]
    ApprovalMatch -->|"matched"| Backend
    ApprovalMatch -->|"no match"| ApprovalPolicy["Approval Policy"]
    ApprovalPolicy --> Backend

    Backend --> Audit["Audit"]
~~~

其中：

- Project 用户侧操作只判断当前用户是否为该 Project Owner；
- Agent 侧操作依赖 Capability / Execution Policy；
- 基础权限通过后先执行 Approval Match；
- 没有匹配 Approval 时，才由 Approval Policy 决定直接执行还是进入 Human Inbox；
- Domain / MCP / Runner 负责各自最后一层具体校验；
- Audit 负责留下安全证据。

## 7. 与其他架构的边界

### Agent Executor

负责：

- 固定 Agent Execution 所属 Project；
- 保存 Agent Capability snapshot；
- 应用 Execution Policy；
- 计算本次可见 Tool Set。

### Tool System

负责：

- Tool Call 进入统一 Authorization；
- 根据 Tool 参数判断目标 Resource；
- 返回 allow / deny / approval_required；
- 在允许后调用实际 Backend。

### Meeting

负责：

- 在 Meeting Timeline 中展示关联 Approval Request；
- 为用户提供审批入口之一；
- 根据审批结果继续 Meeting 场景的后续流程。

Approval Request 本身由 Security / Governance 统一持久化，并同时进入 Human Inbox。

### Runner

负责：

- Runner Device Identity；
- Agent Mount / Workspace 解析；
- Runner 实际 Capability；
- 协议完整性、Workspace / path containment 与运行时 Capability 校验。

Runner 不维护与 Central 重复的本地 Authorization Policy。Command / Managed Process 的宿主机访问边界采用 Runner 设计中已经确定的 trusted-host 语义。

Runner Tool 与 Builtin / MCP Tool 使用同一套 Agent Capability、Approval Match、Approval Policy、Tool Authorization 和 Audit 流程。Security / Governance 不为某个具体 Runner Tool 建立独立授权模型。

### Platform Infrastructure

负责：

- Authentication；
- Project Environment Variables / Secret Management；
- Audit 持久化；
- 相关基础设施。

## 8. 第一阶段实现范围

第一阶段只需要先形成完整安全闭环：

1. Project 绑定唯一 Owner，并对所有 Project-scoped 人类操作执行 Owner 校验；
2. Agent Capability；
3. Execution Policy；
4. 平台固定安全规则与 Project / Resource Scope 校验；
5. Agent Execution 保存权限快照；
6. Tool System 执行统一 Authorization；
7. Builtin / MCP / Runner 都走相同授权入口；
8. Agent 支持 default / auto / allow 三种 Approval Policy；
9. Approval 支持 One-time Approval 与 Reusable Approval，并通过 Human Inbox 处理未命中的审批请求；
10. Project Environment Variables 支持普通变量与 Secret；Secret 使用 Agent 白名单 + Backend Resolution；
11. 敏感操作 Audit；
12. Runner 执行协议完整性、Workspace / path containment 与实际 Capability 校验；Command / Managed Process 采用 trusted-host 边界；
13. 结构化返回 authorization_denied / approval_required 等结果。

第一阶段暂时不需要：

- 通用 Policy DSL；
- 复杂 ABAC；
- 无明确 Scope 的无限范围 Approval；
- 自动权限学习；
- 跨 Project Agent 权限；
- 独立 SIEM；
- 把 Runner Mount 当作完整 OS sandbox。
