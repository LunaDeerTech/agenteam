# Model Resolution 详细设计

> 状态：设计稿
>
> 上层架构：[平台基础设施与部署架构](../README.md)
>
> Model System 总览：[README](./README.md)
>
> 相关详细设计：
> - [Model Configuration](./model-configuration.md)
> - [Chat Model Runtime](./chat-model-runtime.md)
> - [Agent Executor](../../agent-executor/README.md)

## 设计范围

本文定义长期 Model 配置如何在一次 consumer 调用开始前解析为稳定的运行时模型，包括：

- Agent Model Reference；
- Model Resolver；
- ResolvedModel Snapshot；
- reasoning_effort 的解析与校验；
- request/header overwrite 的快照边界；
- Provider / Model 配置变化与运行中 Execution 的一致性。

Model Resolver 不发起模型调用；实际调用由 [Chat Model Runtime](./chat-model-runtime.md) 负责。

## Model Resolver

Model Resolver 是平台共享能力。Agent Executor 在构造 AgentExecutionContext 时使用它；MeetingSummaryUpdater 等平台内部 chat Model consumer 也通过同一个 Resolver 解析 Project 可见 Model。

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

MeetingSummaryUpdater 调用：

```text
resolve(project, meeting_summary_model_ref)
-> ResolvedModel
-> Unified Chat Model Contract
```

Summary 调用不创建 Agent Execution，因此其 resolved model identity 记录在 Summary generator metadata / Model Invocation Usage 中，而不是 AgentExecutionContext。

## Resolved Model Snapshot

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

## Model Parameters、Reasoning Effort 与 Request Overwrite

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

## 配置变化与 Execution 一致性

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

## 第一阶段实现边界

第一阶段解析面实现：

- Model Resolver；
- Resolved Model Snapshot；
- chat parameters.capabilities 与 reasoning_effort 校验；
- request_overwrite / header_overwrite 快照；
- 显式 Model 不进行 silent fallback；
- Provider / Model disabled 或删除后的新旧 Execution 一致性语义。
