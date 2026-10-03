# Chat Model Runtime 详细设计

> 状态：设计稿
>
> 上层架构：[平台基础设施与部署架构](../README.md)
>
> Model System 总览：[README](./README.md)
>
> 相关详细设计：
> - [Model Configuration](./model-configuration.md)
> - [Model Resolution](./model-resolution.md)
> - [Model Token Usage](../model-token-usage.md)
> - [Agent Loop](../../agent-loop/README.md)
> - [统一工具系统](../../tool-system/README.md)

## 设计范围

本文定义 chat Model 的统一运行时 contract，包括：

- chat capability；
- capability 与 Provider Adapter 的职责边界；
- Provider Adapter Registry；
- Unified Chat Model Request / Response；
- Tool Calling / parallel Tool Calls；
- streaming；
- reasoning / structured output；
- usage normalization；
- finish reason / error normalization；
- progressive request timeout / cancel；
- Provider request metadata。

embedding / reranker / image generation 仍共享 Model Configuration，但使用各自独立 Adapter 边界，不强行复用本文的 chat contract。

## Chat Model Capability

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

## Chat Capability 与 Provider Adapter 的职责边界

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

## Provider Adapter

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

## Unified Chat Model Request

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

## Unified Model Response

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

## Tool Calling

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

## Parallel Tool Calls

chat Model 的 parameters.capabilities.parallel_tool_calls 表示模型是否可以在一次响应中产生多个 Tool Call。

它不表示这些 Tool Call 必须并行执行。

实际 Tool execution 仍由 Agent Loop / Tool System 根据：

- Tool metadata；
- 写冲突；
- Approval；
- backend idempotency；
- Execution Policy；

决定串行或并行。

## Streaming Contract

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
    participant A as Model System / Provider Adapter
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

同一逻辑调用自动重试时，标准流须表达实际 attempt 的边界和中止/完成归属。已向上层展示的 partial output 不能与后续 attempt 盲拼成一条完整回答，稳定 Transcript 只采用可恢复语义；半截 Tool Call 不执行，模型 retry 不重放已发生或 outcome unknown 的工具副作用。D01/D09/D22 固定 correlation、流重置/终态和持久化边界，不要求将每个 token 写入数据库。

## Reasoning / Thinking

Reasoning 是 chat Model 可选的 parameters.capabilities 能力；可选等级由 parameters.capabilities.reasoning_efforts 定义。

统一层只保存 Provider 明确允许返回且平台允许处理的 reasoning content / metadata。

原则：

- 不要求所有 Provider reasoning 格式等价；
- 不模拟 Provider 未提供的 reasoning；
- Agent Loop 核心控制不依赖某个 Provider 私有 reasoning 格式；
- 是否持久化 reasoning 内容由 Agent Execution logging policy 决定。

## Structured Output

structured_output 是 chat Model 可选的 parameters.capabilities 能力。

调用方通过统一 response_format 请求结构化输出，Adapter 映射为 Provider 原生协议能力。

Provider 不支持时：

- 不静默模拟成不等价的实现；
- Resolver / Adapter 返回 unsupported_feature；
- 调用方可以改用普通文本或 Tool Calling。

Agent Loop 的基础运行不依赖 structured output。

## Usage 输出边界

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

usage 的持久化、每次调用记录、retry 统计、Agent / Model / Project 聚合与查询，由 [Model Token Usage 详细设计](../model-token-usage.md) 负责。

Provider 不提供可靠 usage 时，不伪造精确值。

## Finish Reason

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

## Error Normalization

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

同一 Agent 逻辑模型调用的自动 Provider 请求重试由 Model System 统一决定和执行。Agent Loop 消费标准化流、结果及最终错误，不根据 retryable 再叠加同一请求的 retry；该字段是错误属性，不是第二个重试 owner。

SDK/Adapter 的真实 requests/attempts 须纳入这一策略与 Invocation/Usage 记录，不允许隐式重试叠加或只统计最终成功请求。上下文压缩后的新输入、模型自我修复的新 Turn、用户 retry/regenerate 与 Scheduler relaunch 分属语义/业务新调用，不迁入 Adapter；工具 retry 与副作用安全仍归 Tool Runtime。

认证失败、model_not_found、unsupported_feature 等配置类错误默认不自动 retry。

### Model Request Progressive Timeout

Model Request timeout 不等同于 Agent Execution timeout。

Agent Execution 不因为单次或多次 Model timeout 自动结束。

对于 Agent 路径的 retryable Model Request，Model System 采用既定渐进 timeout 策略：

```text
attempt 1
  -> initial request timeout

timeout / retryable failure
  -> retry
  -> increase request timeout

...

request timeout reaches configured maximum
  -> stop increasing timeout
  -> keep using maximum timeout for subsequent retries
```

原则：

- timeout 只约束单次 Provider Request；
- timeout-class failure 后，后续 attempt 逐步增加单次 request timeout；
- 增长到合理上限后保持该上限，不再继续增长；
- 只要 Agent Execution 仍 active 且没有 cancel，timeout 本身不构成 Execution terminal condition；
- authentication、permission、model_not_found、invalid_request、unsupported_feature 等非 retryable 配置错误不进入无限 retry；
- rate limit / provider unavailable / network 等 retryable error 可以结合 backoff；
- Agent Loop 的单轮 generation watchdog 是更上层的防循环机制，不由 Model Adapter timeout 取代。

初始 timeout、增长函数、最大 request timeout、backoff 参数属于实现配置，不写死在架构中。

选择 Model System 为 owner 不增设统一固定重试次数或 Execution 总期限。其他 consumer 按自身既定规则调用；例如自动审批不增加模块应用层 retry/备用模型，但允许有限审批请求 timeout 内的 Adapter 透明网络重试，失败转人工后持续等待。完整规则见 [自动审批契约](../../security-governance/auto-approval-model/approval-model-contract.md#不自动-retry)。

### Cancel

Agent Execution cancel 必须向当前 Model Request 传播 cancellation。

Cancel 与 progressive retry 的优先级：

```text
cancel requested
-> stop current request if possible
-> do not start next retry
```

## Provider Request Metadata

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

## 第一阶段实现边界

第一阶段 chat runtime 实现：

- Unified Chat Model Contract；
- Provider Adapter Registry；
- OpenAI Chat Completions / OpenAI-compatible Adapter；
- Anthropic Messages Adapter；
- streaming；
- Tool Calling 与 parallel Tool Call 表达；
- reasoning / structured output capability；
- finish reason / error normalization；
- usage normalization；
- progressive request timeout 与 cancellation 传播。

上述重试、attempt 流边界与真实用量须由 D01/D09/D22 的正式契约和故障验收落实，不表示当前已实现或通过 Provider 集成。

OpenAI Responses、Gemini 等其他 Chat Protocol Adapter 不属于第一阶段实现范围。
