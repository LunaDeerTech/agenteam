# Model System 详细设计

> 状态：设计稿
>
> 上层架构：[平台基础设施与部署架构](./index.md)
>
> 相关架构：
> - [Agent 管理架构](../agent-management.md)
> - [Agent Executor 架构](../agent-executor.md)
> - [Agent Loop 架构](../agent-loop.md)
> - [统一工具系统架构](../tool-system/index.md)
> - [安全与治理架构](../security-governance/index.md)
>
> 相关详细设计：
> - [Model Token Usage 详细设计](./model-token-usage.md)

## 1. 设计范围

Model System 是 agenteam 的平台基础能力，负责管理系统级 / 项目级 Model Provider 与 Model，并为 Agent Execution 以及 Knowledge / Memory / Builtin Tool 等平台内部能力提供稳定的模型调用边界。

本文定义：

- Model Provider 与 Model 的配置模型；
- 系统级 / 项目级配置边界；
- 一个 Provider 对应一种固定 Protocol 的调用模型；
- Agent 如何稳定引用 Model；
- Agent Execution 启动时如何解析 Model；
- chat Model Capability；
- chat Model Capability 与 Provider Adapter 的职责边界；
- Agent 如何选择 Model 与 reasoning_effort；
- Provider Credential；
- Unified Chat Model Contract；
- Provider Adapter；
- streaming；
- Tool Calling；
- reasoning / structured output；
- finish reason / error normalization；
- Execution 期间的模型配置快照与可追溯性。

Token 使用量的持久化、聚合、按 Agent / Model / Project 查询等统计设计不在本文展开，见 [Model Token Usage 详细设计](./model-token-usage.md)。

本文不定义：

- Agent Loop 自身的循环控制；
- Tool 的授权与执行；
- Agent Capability；
- Project Environment Variable / Secret；
- Knowledge / Memory 的检索逻辑。

## 2. 设计参考与核心取舍

Model System 的边界参考成熟 Agent Harness / AI Runtime 的通用做法，包括：

- Agent Loop 只依赖统一 Model Contract，不直接依赖第三方 Provider；
- Provider-specific request / stream / error 由 Adapter 处理；
- 显式选择的 Model 不进行 silent fallback；
- Model Capability 与 Provider Adapter 的协议映射职责分离；
- serializable execution snapshot 与进程内 Adapter 实现分离。

参考对象包括 pi / pi-ai、OpenCode、OpenAI Agents SDK 等。它们只作为设计参考，不成为 agenteam 的运行时依赖。

agenteam 在此基础上做一个明确简化：

> 一个 Model Provider 只使用一种 Protocol。

因此 Protocol 固定在 Provider 上，Model 不允许覆盖 Protocol。

如果同一个第三方平台需要以两种不同 Protocol 调用，应配置成两个不同的 Model Provider，而不是给 Model 增加 Protocol override。

## 3. 总体结构

~~~mermaid
flowchart LR
    Admin["System Admin / Project Owner"]
    Provider["Model Provider<br/>Protocol / Endpoint / Credential"]
    Model["Model Config<br/>Parameters / Request Overwrite"]
    Agent["Agent<br/>model_ref + reasoning_effort"]
    Resolver["Model Resolver"]
    Snapshot["Resolved Model Snapshot"]
    Loop["Agent Loop"]
    Contract["Unified Chat Model Contract"]
    Adapter["Provider Adapter"]
    API["Provider API"]

    Admin --> Provider
    Provider --> Model
    Model --> Agent
    Agent --> Resolver
    Provider --> Resolver
    Model --> Resolver
    Resolver --> Snapshot
    Snapshot --> Loop
    Loop --> Contract
    Contract --> Adapter
    Adapter --> API
~~~

长期配置与运行时职责分为三层：

~~~text
Configuration
├── ModelProvider
└── ModelConfig

Execution Resolution
└── ResolvedModel

Runtime
├── Unified Chat Model Contract
└── Provider Adapter
~~~

## 4. 核心原则

1. Agent 只引用逻辑 Model，不保存 Provider Credential。
2. 一个 Provider 固定一种 Protocol，Provider 下所有 Model 使用同一 Protocol。
3. Protocol 是调用协议；chat Model 的 parameters.capabilities 描述模型语义能力，两者不是同一概念。
4. Provider / Model 是长期配置，Agent Execution 使用解析后的稳定快照。
5. Agent Loop 不读取 Provider 配置，也不理解系统级 / 项目级配置。
6. Agent Loop 只依赖 Unified Chat Model Contract。
7. Provider / Protocol 的请求、响应和特殊差异统一封装在 Provider Adapter。
8. 显式选择的 Model 不 silent fallback 到其他 Model。
9. Credential 永远不进入 Agent Model Context。
10. 一次 Agent Execution 启动后，其 resolved model identity 与 parameters 保持稳定。
11. LLM / embedding / reranker / image generation 可以共享配置管理方式，但不强制共享一个万能运行时接口。

## 5. Model Provider

ModelProvider 表示一个可配置、可认证、可调用的模型服务端点。

~~~text
ModelProvider
├── id
├── scope
│   ├── system
│   └── project
├── project_id?
├── name
├── protocol
├── base_url
├── credential_ref
├── provider_options
├── enabled
├── created_at
└── updated_at
~~~

字段语义：

- id：稳定 Provider ID；
- scope：system 或 project；
- project_id：项目级 Provider 所属 Project；
- name：用户可读名称；
- protocol：当前 Provider 唯一的模型调用协议；
- base_url：Provider API endpoint；
- credential_ref：平台 Secret Management 中的 Credential 引用；
- provider_options：当前 Protocol Adapter 允许的 Provider 级扩展配置；
- enabled：是否允许新的 Agent / Execution 使用；
- created_at / updated_at：配置时间信息。

Chat Protocol 可以持续扩展。第一阶段实际实现：

~~~text
openai-chat-completions
anthropic-messages
~~~

后续可以增加 OpenAI Responses、Gemini 等其他 Protocol。

如果两个配置指向同一家外部服务，但分别使用不同 Protocol，则它们在 agenteam 中是两个独立 ModelProvider。

## 6. System Provider 与 Project Provider

~~~mermaid
flowchart TB
    System["System Providers"]
    Project["Project Providers"]
    Visible["Project Available Models"]
    Agent["Project Agent"]

    System --> Visible
    Project --> Visible
    Visible --> Agent
~~~

### 6.1 System Provider

System Provider：

- 由平台管理员配置；
- 可以供多个 Project 使用；
- Credential 属于平台 Secret；
- Project 不获得 Credential；
- Provider / Model 修改只影响后续新 Execution。

### 6.2 Project Provider

Project Provider：

- 只属于一个 Project；
- 只能被该 Project 的 Agent 使用；
- 由 Project Owner 配置；
- Credential 仍由平台 Secret Management 保存；
- 不进入 Project Environment Variables；
- 不通过 Agent Secret Variable 白名单暴露；
- 第一阶段只允许配置 `type = chat` 的 Model。

`embedding`、`reranker`、`image_generation` 属于平台内部能力，只能配置在 System Provider 下，不能由 Project Provider 定义。

### 6.3 Model 可见性

所有 `enabled` 的 System chat Model 对所有 Project 可见，第一阶段不增加 Project allowlist / availability policy。

对某个 Project 的 Agent：

~~~text
Available Chat Models
=
all enabled System chat Models
+
enabled chat Models from current Project Providers
~~~

UI 必须明确标识 System / Project 来源。

Model identity 不依赖 display name，因此不同 Provider 下可以出现同名 Model。

## 7. Model Config

ModelConfig 表示一个 Provider 下可被选择的具体模型。

~~~text
ModelConfig
├── id
├── provider_id
├── name
├── model_id
├── type
├── parameters
├── request_overwrite
├── header_overwrite
├── enabled
├── created_at
└── updated_at
~~~

其中：

- id：agenteam 内部稳定 Model ID；
- provider_id：所属 Provider；
- name：UI 展示名称；
- model_id：发送给 Provider 的原生模型标识；
- type：模型用途类型；
- parameters：由 Model type 定义的模型参数集合，不要求所有类型拥有相同字段；
- request_overwrite：对 Provider Adapter 最终生成的请求结构体进行强制字段覆盖，只覆盖显式配置的字段；
- header_overwrite：对 Provider Adapter 最终生成的请求 Header 进行强制字段覆盖，只覆盖显式配置的 Header；
- enabled：是否允许新的 Agent 选择和解析。

例如 chat Model 的 parameters 可以包含：

~~~text
parameters
├── context_length
├── max_output
└── capabilities
    ├── input
    │   ├── text
    │   ├── image
    │   └── file
    ├── tool_calling
    ├── parallel_tool_calls
    ├── streaming
    ├── reasoning
    ├── reasoning_efforts
    └── structured_output
~~~

而 embedding、reranker、image_generation 等 Model type 可以定义各自不同的 parameters。Model System 不要求所有类型共享同一套参数字段。

第一阶段 ModelConfig 是显式配置对象，不依赖 Provider 自动 discovery 才能存在。

### 7.1 Model / Provider 删除策略

Provider 和 ModelConfig 支持物理删除，不要求长期保留 disabled 记录。

#### 删除 chat Model

如果待删除 chat Model 仍被 Agent.model_ref 引用，用户侧必须先通过删除确认弹窗选择替代 chat Model。

确认删除时，服务端以一个事务性操作完成：

1. 校验替代 Model 当前 enabled 且对所有受影响 Agent 可见；
2. 校验替代 Model 的 type = chat；
3. 校验受影响 Agent 的 reasoning_effort 在替代 Model 下仍然合法；存在不兼容配置时，删除流程必须先要求用户解决，不能留下无效 Agent 配置；
4. 批量把所有受影响 Agent.model_ref 更新为替代 Model；
5. 完成后物理删除原 ModelConfig。

System chat Model 可能被多个 Project 的 Agent 引用，因此其替代 Model 必须是对全部受影响 Project 可见的 enabled System chat Model。

Project chat Model 只影响所属 Project，可以替换为该 Project 当前可见的 enabled System chat Model 或 Project chat Model。

#### 删除平台内部 Model

如果待删除 Model 正被 PlatformModelSelection 引用：

- embedding_model_ref：必须先选择另一个 enabled System embedding Model，不能清空；
- reranker_model_ref：可以选择另一个 enabled System reranker Model，也可以清空；
- image_generation_model_ref：可以选择另一个 enabled System image_generation Model，也可以清空。

selector 更新完成后才能物理删除对应 ModelConfig。

#### 删除 Provider

Provider 不能级联删除其 Models。

只有当 Provider 下已经不存在任何 ModelConfig 时，才能物理删除 Provider。用户需要先逐个完成该 Provider 下 Model 的删除 / 替换流程。

Provider 配置删除与 Credential Secret 的生命周期分离。删除 Provider 不能直接使仍被运行中 ResolvedModel.credential_ref_snapshot 引用的 Secret 失效；Secret 清理必须在没有运行中引用后再进行。

#### 历史记录

AgentExecution、ModelInvocationUsage 等历史记录不阻止 Provider / Model 的物理删除。

历史记录对 Provider / Model 的数据库外键使用可空引用，并采用 `ON DELETE SET NULL`；与此同时必须保留调用时的 Provider / Model identity snapshot，因此配置记录删除后仍可追溯历史 Execution 和 Usage。

## 8. Model 类型、平台用途与运行时接口

平台统一管理以下 Model 类型：

~~~text
chat
embedding
reranker
image_generation
~~~

这些 Model 共享 Provider / Model Management，但不共享一个万能运行时接口，并且消费方式不同。

~~~mermaid
flowchart TB
    System["System Model Config"]
    Project["Project Model Config"]

    Chat["chat"]
    Emb["embedding"]
    Rank["reranker"]
    Image["image_generation"]

    Agent["Agent.model_ref"]
    Select["PlatformModelSelection"]
    KM["Knowledge / Memory"]
    ImageTool["Builtin generate-image Tool"]

    ChatAdapter["Unified Chat Model Contract"]
    EmbAdapter["Embedding Adapter"]
    RankAdapter["Reranker Adapter"]
    ImageAdapter["Image Generation Adapter"]

    System --> Chat
    System --> Emb
    System --> Rank
    System --> Image

    Project --> Chat

    Chat --> Agent
    Agent --> ChatAdapter

    Emb --> Select
    Rank --> Select
    Image --> Select

    Select --> KM
    KM --> EmbAdapter
    KM --> RankAdapter

    Select --> ImageTool
    ImageTool --> ImageAdapter
~~~

### 8.1 chat

`chat` 是 Agent 的运行模型。

Agent 通过 `model_ref` 选择当前 Project 可用的 chat Model，然后由 Agent Loop 通过 Unified Chat Model Contract 调用。

System Provider 和 Project Provider 都可以配置 chat Model。

本文后续的 Unified Model Contract 专指 Chat / LLM 调用。

### 8.2 embedding

`embedding` 只用于平台 Knowledge / Memory 的向量化与检索基础能力。

它：

- 不允许被 Agent 直接选择为运行模型；
- 不暴露为模型可见 Tool；
- 第一阶段只能由 System Provider 配置；
- 由 PlatformModelSelection 的 `embedding_model_ref` 选择。

### 8.3 reranker

`reranker` 只用于 Knowledge / Memory retrieval 的可选 reranking 阶段。

它：

- 不允许被 Agent 直接选择为运行模型；
- 不暴露为模型可见 Tool；
- 第一阶段只能由 System Provider 配置；
- 由 PlatformModelSelection 的 `reranker_model_ref` 选择；
- selector 允许为空，表示 retrieval 不启用模型 reranking。

### 8.4 image_generation

`image_generation` 用于平台内部的图片生成能力。

它不直接进入 Agent Loop，而是由统一 Tool System 暴露为普通 Builtin Tool，例如：

~~~text
generate-image
~~~

调用链路：

~~~mermaid
flowchart LR
    Loop["Agent Loop"]
    Tool["Builtin generate-image"]
    Selection["image_generation_model_ref"]
    Adapter["Image Generation Adapter"]
    Provider["Model Provider"]

    Loop --> Tool
    Tool --> Selection
    Selection --> Adapter
    Adapter --> Provider
~~~

因此 Agent 只感知图片生成 Tool，不感知背后的 image_generation Model。

image_generation Model 第一阶段只能由 System Provider 配置。

### 8.5 Platform Model Selection

平台基础设施保存三个模型用途 selector：

~~~text
PlatformModelSelection
├── embedding_model_ref
├── reranker_model_ref?
└── image_generation_model_ref?
~~~

它们只保存已配置 ModelConfig 的稳定 ID，不在 selector 中重复配置 Model 本身。

约束：

~~~text
embedding_model_ref
-> System Model
-> type = embedding

reranker_model_ref
-> System Model
-> type = reranker
-> optional

image_generation_model_ref
-> System Model
-> type = image_generation
-> optional
~~~

这三个 selector 都是平台级配置，不支持 Project override。

Knowledge / Memory 共用同一组 embedding / reranker selector。

如果 `image_generation_model_ref` 未配置，则平台没有可用的默认 image generation backend，`generate-image` Tool 不应作为可执行 Tool 暴露给 Agent。

## 9. Agent Model Reference

Agent 配置：

~~~text
Agent
├── model_ref
└── reasoning_effort
~~~

model_ref 保存稳定 ModelConfig ID。

Agent 不保存：

- Provider base_url；
- Provider Credential；
- Provider Protocol；
- Provider 配置副本。

当 ModelConfig 被 disabled：

- 已存在 Agent 的 model_ref 不自动改写；
- UI 标记当前引用不可用；
- 新 Agent 不应继续选择该 Model；
- 新 Agent Execution 在 Model Resolver 阶段失败；
- 系统不自动切换到其他 Model。

## 10. Model Resolver

Agent Executor 在构造 AgentExecutionContext 时通过 Model Resolver 解析当前模型。

~~~mermaid
sequenceDiagram
    participant E as Agent Executor
    participant A as Agent Management
    participant M as Model Resolver
    participant P as Provider / Model Config
    participant X as Agent Execution

    E->>A: load Agent config
    A-->>E: model_ref + reasoning_effort
    E->>M: resolve(project, model_ref, reasoning_effort)
    M->>P: load Provider + ModelConfig
    P-->>M: config
    M->>M: validate scope / enabled / type
    M->>M: resolve model parameters / reasoning effort / overwrites
    M-->>E: ResolvedModel snapshot
    E->>X: persist execution context
~~~

Model Resolver 负责：

- 校验 Model 对当前 Project 可见；
- 校验 Provider / Model enabled；
- 校验 Model type；
- 解析固定 Provider Protocol；
- 解析当前 Model type 对应的 parameters；
- 对 chat Model，从 parameters.capabilities 读取语义能力；
- 校验并解析 Agent 选择的 reasoning_effort；
- 解析 request_overwrite / header_overwrite；
- 生成 Resolved Model Snapshot。

Model Resolver 不发起模型调用。

## 11. Resolved Model Snapshot

AgentExecutionContext 中的 model 是运行时快照，而不是裸 Model ID。

~~~text
ResolvedModel
├── model_config_id?
├── provider_id?
├── model_config_id_snapshot
├── provider_id_snapshot
├── provider_name_snapshot
├── provider_scope_snapshot
├── protocol
├── base_url_snapshot
├── credential_ref_snapshot
├── model_id_snapshot
├── type
├── parameters
├── reasoning_effort
├── request_overwrite
├── header_overwrite
└── provider_options_snapshot
~~~

Credential value 不进入 snapshot，只保存 `credential_ref_snapshot`。

Model Adapter 在实际调用时根据 `credential_ref_snapshot` 从 Secret Management 获取当前 Credential value。因此同一个 credential_ref 对应的 Secret 被轮换后，运行中的 Execution 后续模型 turn 会使用轮换后的值。

Agent Execution 应保存足够的 resolved metadata，使历史 Execution 能回答：

- 当时使用哪个 Provider，即使 Provider 配置已经被物理删除；
- 使用哪个 Protocol；
- 使用哪个 model_id，即使 ModelConfig 已经被物理删除；
- 当时完整的 Model parameters，包括 chat Model 的 capabilities；
- 当时 Agent 选择并实际生效的 reasoning_effort；
- 当时 request_overwrite / header_overwrite。

## 12. Chat Model Capability

Capability 不再是 ModelConfig 的一级字段，而是 chat Model 的 `parameters` 子结构：

~~~text
parameters
├── context_length
├── max_output
└── capabilities
    ├── input
    │   ├── text
    │   ├── image
    │   └── file
    ├── tool_calling
    ├── parallel_tool_calls
    ├── streaming
    ├── reasoning
    ├── reasoning_efforts
    └── structured_output
~~~

它描述：

> 这个 chat Model 在 agenteam 的 Agent Loop 中能够做什么。

embedding、reranker、image_generation 等其他 Model type 不要求配置 capabilities；它们由各自的 parameters schema 描述运行时需要的模型属性。

chat Model 的 `parameters.capabilities` 用于：

- Agent 配置 UI；
- Model Resolver 校验；
- Agent Loop 调用前能力判断；
- 避免请求明显不支持的模型特性。

第一阶段以 ModelConfig 中显式配置的 `parameters.capabilities` 为准。Provider Adapter 可以提供默认建议用于辅助配置，但运行时不依赖 Adapter 隐式推断来替代显式 ModelConfig。

`parameters.capabilities` 不用于：

- Tool Authorization；
- Approval；
- Provider request / response mapping。

## 13. Chat Capability 与 Provider Adapter 的职责边界

chat Model Capability 和 Provider Adapter 解决的是不同层次的问题。

~~~mermaid
flowchart LR
    Capability["parameters.capabilities<br/>chat 模型能做什么"]
    Loop["Agent Loop"]
    Contract["Unified Chat Model Contract"]
    Adapter["Provider Adapter<br/>请求 / 响应 / Provider 差异"]
    API["Provider API"]

    Capability --> Loop
    Loop --> Contract
    Contract --> Adapter
    Adapter --> API
~~~

chat Model 的 `parameters.capabilities` 描述上层语义能力，例如：

~~~text
tool_calling = true
reasoning = true
reasoning_efforts = [low, medium, high]
image_input = true
structured_output = false
~~~

Agent Loop 和 Model Resolver 只需要根据这些能力判断：

- 模型是否支持 Tool Calling；
- 是否支持 reasoning；
- 可以选择哪些 reasoning effort；
- 是否支持图片输入；
- 是否支持 structured output。

至于具体请求应该如何发送给 Provider，则全部属于 Provider Adapter 的职责。

例如某个 Provider / Protocol 可能存在：

~~~text
- 不接受 developer role
- 使用特定 max token 字段
- streaming usage 需要额外 option
- Tool Result 需要特定字段
- reasoning effort 使用 Provider 自己的原生字段
~~~

这些都只是 Provider Adapter 内部需要处理的协议差异和 Provider-specific quirks，不形成额外的运行时层或独立领域对象。

Provider Adapter 可以结合：

- Adapter 自身实现；
- Provider 级 `provider_options`；
- ModelConfig.request_overwrite；
- ModelConfig.header_overwrite；

完成最终 Provider-native request / response 的转换。

因此职责边界固定为：

~~~text
parameters.capabilities
= 上层判断“模型能做什么”

Provider Adapter
= 处理“这个 Provider / Protocol 具体怎么调用”
~~~

`request_overwrite / header_overwrite` 不改变 Provider Protocol，只是在 Adapter 已生成的原生请求上对显式配置字段做最终覆盖。

## 14. Model Parameters、Reasoning Effort 与 Request Overwrite

ModelConfig.parameters 是 Model type 自身的配置参数。

chat Model 第一阶段使用：

~~~text
parameters
├── context_length
├── max_output
└── capabilities
    ├── input
    ├── tool_calling
    ├── parallel_tool_calls
    ├── streaming
    ├── reasoning
    ├── reasoning_efforts
    └── structured_output
~~~

其中：

- context_length：模型上下文窗口；
- max_output：模型最大输出；
- capabilities.reasoning：是否支持 reasoning；
- capabilities.reasoning_efforts：该模型允许 Agent 选择的 reasoning effort 等级。

例如：

~~~text
reasoning = true
reasoning_efforts = [low, medium, high]
~~~

Agent 不再保存通用 model parameters / request parameters。

Agent 只保存：

~~~text
Agent
├── model_ref
└── reasoning_effort
~~~

Agent 配置界面根据所选 chat Model 的 `parameters.capabilities.reasoning_efforts` 提供可选项；当列表非空时，Agent 必须从中选择一个 reasoning_effort。

如果：

~~~text
reasoning = false
~~~

则不展示 reasoning_effort 配置。

如果：

~~~text
reasoning = true
reasoning_efforts = []
~~~

表示模型支持 reasoning，但平台不提供可选 effort 等级；Agent 不保存 reasoning_effort，由 Provider Adapter 使用该 Protocol / Model 的默认 reasoning 行为。

Model Resolver 在 Execution 启动时校验：

~~~text
Agent.reasoning_effort
∈
ModelConfig.parameters.capabilities.reasoning_efforts
~~~

校验通过后，所选 reasoning_effort 写入 ResolvedModel。

Provider Adapter 再把统一 reasoning_effort 映射为当前 Protocol 的原生请求字段，例如：

~~~mermaid
flowchart LR
    Agent["Agent.reasoning_effort"]
    Resolver["Model Resolver"]
    Resolved["ResolvedModel.reasoning_effort"]
    Adapter["Provider Adapter"]
    Native["Provider-native reasoning config"]
    Req["request_overwrite"]
    Header["header_overwrite"]
    Final["Final Request + Headers"]

    Agent --> Resolver
    Resolver --> Resolved
    Resolved --> Adapter
    Adapter --> Native
    Native --> Final
    Req --> Final
    Header --> Final
~~~

不同 Protocol 可以用不同方式表达 reasoning effort，由 Provider Adapter 负责转换；Agent 不感知 Provider 原生字段。

Provider Adapter 生成原生请求后，再应用 ModelConfig 的强制覆盖：

- request_overwrite 只覆盖显式配置的请求字段；
- header_overwrite 只覆盖显式配置的 Header；
- 未配置字段保持 Adapter 原始生成结果；
- overwrite 具有最终优先级，因此也可以强制覆盖 Adapter 根据 reasoning_effort 生成的对应请求字段。

embedding、reranker、image_generation 等其他 Model type 使用各自的 parameters schema，不继承 chat Model 的 reasoning_effort 设计。

## 15. Provider Credential 与安全边界

Model Provider Credential 属于平台 Secret Management。

长期配置只保存 credential_ref。

~~~mermaid
sequenceDiagram
    participant L as Agent Loop
    participant A as Provider Adapter
    participant S as Secret Management
    participant P as Provider API

    L->>A: Unified Model Request
    A->>S: resolve credential_ref
    S-->>A: credential
    A->>P: provider-native request
    P-->>A: response / stream
    A-->>L: unified events / response
~~~

Credential：

- 不进入 Prompt；
- 不进入 model-visible AgentExecutionContext；
- 不写入普通 Execution Log；
- 不允许 Agent 读取；
- 不作为 Tool arguments；
- 不记录 Authorization header 或等价敏感字段。

另外，Provider endpoint、Protocol、Credential 都属于模型调用安全边界。

普通 Agent / 项目内容不能通过 Prompt 或普通 Tool 修改这些字段。允许修改这些配置的用户侧管理操作必须经过服务端权限校验。

## 16. Provider Adapter

Agent Loop 只依赖统一 Chat Model Contract。

Provider Adapter 负责：

- 将统一 request 映射为当前 Provider 固定 Protocol 的 native request；
- 应用 Provider-specific options 和协议差异处理；
- 转换 Tool schema；
- 发送请求；
- 解析 streaming；
- 标准化 response；
- 标准化 usage；
- 标准化 finish reason；
- 标准化 error；
- 管理 Provider client / transport lifecycle。

概念接口：

~~~text
ChatModelAdapter
├── validate(provider, model)
├── complete(request)
├── stream(request)
├── normalize_error(error)
└── close() / client lifecycle
~~~

Adapter Registry 根据 Provider.protocol 选择实现。

第一阶段 Chat Model Adapter 只实现：

~~~text
openai-chat-completions
-> OpenAI Chat Completions / OpenAI-compatible Adapter

anthropic-messages
-> Anthropic Messages Adapter
~~~

OpenAI Responses、Gemini 等其他 Chat Protocol Adapter 不属于第一阶段实现范围。

## 17. Unified Chat Model Request

~~~text
ModelRequest
├── resolved_model
├── messages
├── tools
├── tool_choice
├── response_format
├── metadata
└── cancellation
~~~

统一 Message 至少支持：

~~~text
system
user
assistant
tool
~~~

Message content 使用可扩展 content parts：

~~~text
text
image
file/reference
tool_call
tool_result
provider_metadata
~~~

第一阶段 Agent 主流程以 text + tool calling 为核心。

Model Adapter 只负责把 Tool System 提供的 ToolSpec projection 转换成 Provider schema。

Tool Capability、Authorization、Approval 和 Backend execution 不属于 Model Adapter。

## 18. Unified Model Response

非 streaming 调用返回：

~~~text
ModelResponse
├── message
├── finish_reason
├── usage
├── provider_metadata
└── request_metadata
~~~

Assistant message 可以包含：

- text；
- reasoning metadata；
- 一个或多个 Tool Call。

Agent Loop 根据统一结果决定继续 Tool Loop 或完成 Execution。

## 19. Tool Calling

统一 Tool Call：

~~~text
ModelToolCall
├── call_id
├── model_visible_tool_name
├── arguments
└── provider_metadata
~~~

Adapter 负责：

- Provider-native Tool Call -> Unified Tool Call；
- 维持 Provider 要求的 call_id；
- 将 Tool Result 正确映射回 Provider Protocol。

Tool 的长期稳定 identity 仍由 Tool System 负责。

~~~text
Stable Tool ID
    ↕
Model-visible Tool Name
    ↕
Provider Tool Call
~~~

Model-visible name 是 transport representation，不改变 Tool 长期 identity。

## 20. Parallel Tool Calls

chat Model 的 parameters.capabilities.parallel_tool_calls 表示模型是否可以在一次响应中产生多个 Tool Call。

它不表示这些 Tool Call 必须并行执行。

实际 Tool execution 仍由 Agent Loop / Tool System 根据：

- Tool metadata；
- 写冲突；
- Approval；
- backend idempotency；
- Execution Policy；

决定串行或并行。

## 21. Streaming Contract

统一 streaming event：

~~~text
message_start
text_delta
reasoning_delta
tool_call_start
tool_call_delta
tool_call_end
usage_update
message_end
error
~~~

~~~mermaid
sequenceDiagram
    participant L as Agent Loop
    participant A as Provider Adapter
    participant P as Provider API

    L->>A: stream(ModelRequest)
    A->>P: native streaming request
    P-->>A: provider stream events
    A-->>L: message_start
    A-->>L: text_delta / reasoning_delta
    A-->>L: tool_call_delta
    A-->>L: usage_update
    A-->>L: message_end or error
~~~

Provider transport stream 关闭不等于模型调用成功。

Adapter 必须确认 Provider 的 terminal state，并明确输出：

- 正常 message_end；
- incomplete / failed 对应的标准化 error；
- cancelled。

Partial Tool Call 必须在 Adapter 层完整组装后，才交给 Agent Loop / Tool System。

## 22. Reasoning / Thinking

Reasoning 是 chat Model 可选的 parameters.capabilities 能力；可选等级由 parameters.capabilities.reasoning_efforts 定义。

统一层只保存 Provider 明确允许返回且平台允许处理的 reasoning content / metadata。

原则：

- 不要求所有 Provider reasoning 格式等价；
- 不模拟 Provider 未提供的 reasoning；
- Agent Loop 核心控制不依赖某个 Provider 私有 reasoning 格式；
- 是否持久化 reasoning 内容由 Agent Execution logging policy 决定。

## 23. Structured Output

structured_output 是 chat Model 可选的 parameters.capabilities 能力。

调用方通过统一 response_format 请求结构化输出，Adapter 映射为 Provider 原生协议能力。

Provider 不支持时：

- 不静默模拟成不等价的实现；
- Resolver / Adapter 返回 unsupported_feature；
- 调用方可以改用普通文本或 Tool Calling。

Agent Loop 的基础运行不依赖 structured output。

## 24. Usage 输出边界

每次 Provider 调用返回的 usage 由 Adapter 标准化，例如：

~~~text
ModelUsage
├── input_tokens
├── output_tokens
├── total_tokens
├── cached_input_tokens?
├── cache_write_tokens?
├── reasoning_tokens?
└── provider_usage?
~~~

Model System 只负责：

> 从 Provider response / stream 中尽可能得到标准化 usage。

usage 的持久化、每次调用记录、retry 统计、Agent / Model / Project 聚合与查询，由 [Model Token Usage 详细设计](./model-token-usage.md) 负责。

Provider 不提供可靠 usage 时，不伪造精确值。

## 25. Finish Reason

统一 finish reason：

~~~text
stop
tool_calls
length
content_filter
cancelled
error
unknown
~~~

Adapter 将 Provider 原生原因映射为统一值，并可以额外保留 provider_finish_reason。

未知 Provider reason 映射为 unknown，不猜测其业务语义。

## 26. Error Normalization

统一 Model Error 至少包括：

~~~text
invalid_request
authentication
permission
model_not_found
rate_limited
context_too_large
provider_unavailable
timeout
network
cancelled
content_filter
unsupported_feature
provider_error
unknown
~~~

标准错误 metadata：

~~~text
retryable
provider_code
provider_request_id
safe_message
~~~

Adapter 去除 Credential / Secret 后返回标准化错误。

Agent Loop 根据 retryable 和自己的 retry policy 决定是否重试。

认证失败、model_not_found、unsupported_feature 等配置类错误默认不自动 retry。

## 27. Provider Request Metadata

模型调用可以记录：

- provider request id；
- provider latency；
- protocol；
- model_id；
- finish reason；
- normalized usage；
- retry attempt；
- error category。

不得记录：

- Credential；
- Authorization header；
- Secret；
- 不必要的完整 Provider request dump。

完整 Prompt / Message 是否持久化由 Agent Execution logging 设计决定，Model System 不复制另一份。

## 28. 配置变化与 Execution 一致性

~~~text
Execution Start
    ↓
Resolve Provider + Model
    ↓
Persist ResolvedModel Snapshot
    ↓
Run Agent Loop
~~~

运行期间：

- Provider display name 修改不影响当前 Execution；
- Provider Protocol 修改不允许作为普通在线更新影响当前 Execution；
- Model parameters 修改不影响当前 Execution；
- Model request_overwrite / header_overwrite 修改不影响当前 Execution；
- Agent reasoning_effort 修改不影响当前 Execution；
- Model parameters（包括 chat capabilities）修改不影响当前 Execution snapshot；
- Provider / Model 被 disabled 时，已经启动的 Execution 继续使用启动时的 ResolvedModel snapshot；disable 只阻止新的 Execution；
- Credential value 不进入 snapshot；如果 credential_ref 对应的 Secret value 被轮换，后续模型 turn 使用新值。

配置变化默认只影响新的 Agent Execution。

Provider / Model 即使之后被物理删除，历史 Execution 仍依赖 snapshot 保留当时的模型身份与调用配置，而不是重新读取已删除的配置记录。

## 29. UI 配置模型

模型配置 UI 按 Provider 组织：

~~~text
Model Providers

System
└── Provider A
    ├── Protocol
    ├── Endpoint
    └── Models
        ├── Model 1
        └── Model 2

Project
└── Provider B
    ├── Protocol
    ├── Endpoint
    └── Models
        ├── Model 3
        └── Model 4
~~~

Provider 页面至少配置：

- name；
- protocol；
- base URL；
- credential；
- provider options；
- enabled。

Model 页面至少配置：

- display name；
- model_id；
- type；
- parameters（字段由 Model type 决定，例如 chat 的 context_length / max_output / capabilities）；
- request overwrite；
- header overwrite；
- enabled。

System Provider 可以配置 chat / embedding / reranker / image_generation Model。

Project Provider 的 Model type 固定为 chat，UI 不允许选择其他类型。

平台 Model Management 另外提供 Platform Model Selection：

~~~text
Embedding Model        -> System embedding Model
Reranker Model         -> optional System reranker Model
Image Generation Model -> optional System image_generation Model
~~~

这些 selector 只选择已有 Model，不在这里编辑 Model 参数。

Agent 配置页只消费当前 Project 可用的 chat Model，并根据所选 Model 的 parameters.capabilities.reasoning_efforts 展示 reasoning_effort 选项。

## 30. 第一阶段实现边界

第一阶段建议实现：

1. System / Project 两级 Model Provider，所有 enabled System chat Model 对所有 Project 可见；
2. 一个 Provider 固定一个 Protocol；
3. 显式 Provider + ModelConfig；
4. System Provider 支持 chat / embedding / reranker / image_generation，Project Provider 只支持 chat；
5. PlatformModelSelection：embedding / optional reranker / optional image_generation；
6. Model Resolver；
7. Resolved Model Snapshot；
8. chat parameters.capabilities 与 Provider Adapter 职责分离；
9. Unified Chat Model Contract；
10. streaming；
11. Tool Calling；
12. reasoning / structured output capability；
13. finish reason / error normalization；
14. Agent 级 reasoning_effort 选择与校验；
15. Provider Credential secret reference；
16. Provider Adapter Registry，第一阶段 Chat Adapter 只实现 OpenAI Chat Completions / OpenAI-compatible 与 Anthropic Messages；
17. embedding / reranker / image generation 的独立调用 Adapter 边界；
18. image_generation 通过 Builtin generate-image Tool 消费；
19. usage normalization，并把统计交给独立 Token Usage 模块；
20. Provider / Model 物理删除、Agent model_ref 批量替换、PlatformModelSelection 引用处理，以及历史 snapshot 保留。

第一阶段不要求：

- Model 级 Protocol override；
- 自动 Model routing；
- silent fallback；
- 多模型 ensemble；
- 动态按价格 / 质量选择模型；
- Model Gateway 微服务；
- 一个统一万能 Adapter 覆盖 chat / embedding / reranker / image。
