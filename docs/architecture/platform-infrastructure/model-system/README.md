# Model System 详细设计

> 状态：设计稿
>
> 上层架构：[平台基础设施与部署架构](../README.md)
>
> 相关架构：
> - [Agent 管理架构](../../agent-management.md)
> - [Agent Executor 架构](../../agent-executor/README.md)
> - [Agent Loop 架构](../../agent-loop/README.md)
> - [统一工具系统架构](../../tool-system/README.md)
> - [安全与治理架构](../../security-governance/README.md)
>
> 相关详细设计：
> - [Model Configuration](./model-configuration.md)
> - [Model Resolution](./model-resolution.md)
> - [Chat Model Runtime](./chat-model-runtime.md)
> - [Model Token Usage](../model-token-usage.md)

## 设计范围

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

Token 使用量的持久化、聚合、按 Agent / Model / Project 查询等统计设计不在本文展开，见 [Model Token Usage 详细设计](../model-token-usage.md)。

本文不定义：

- Agent Loop 自身的循环控制；
- Tool 的授权与执行；
- Agent Capability；
- Project Environment Variable / Secret；
- Knowledge / Memory 的检索逻辑。

## 设计参考与核心取舍

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

## 总体结构

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

## 核心原则

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

## 详细设计拆分

Model System 按长期配置、Execution 解析和模型调用运行时三个职责层拆分：

~~~text
Model System
├── Model Configuration
│   ├── ModelProvider / ModelConfig
│   ├── System / Project scope
│   ├── Model type / selector
│   ├── credential
│   └── management UI / deletion
├── Model Resolution
│   ├── Agent model reference
│   ├── Model Resolver
│   ├── ResolvedModel snapshot
│   └── execution consistency
└── Chat Model Runtime
    ├── capability
    ├── Provider Adapter
    ├── unified request / response
    ├── streaming / tool calling / reasoning
    └── usage / finish reason / error / timeout
~~~

边界固定为：

- 长期 Model / Provider 配置由 [Model Configuration](./model-configuration.md) 定义；
- consumer 在 Execution 或平台内部调用开始前如何得到稳定运行时模型，由 [Model Resolution](./model-resolution.md) 定义；
- Agent Loop 以及其他 chat consumer 如何通过统一 contract 调用 Provider，由 [Chat Model Runtime](./chat-model-runtime.md) 定义；
- Token usage 的持久化、聚合与查询继续由 [Model Token Usage](../model-token-usage.md) 定义。

## 第一阶段实现边界

第一阶段建议实现：

1. System / Project 两级 Model Provider，所有 enabled System chat Model 对所有 Project 可见；
2. 一个 Provider 固定一个 Protocol；
3. 显式 Provider + ModelConfig；
4. System Provider 支持 chat / embedding / reranker / image_generation，Project Provider 只支持 chat；
5. PlatformModelSelection：embedding / optional reranker / memory / optional image_generation；
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
20. ProjectConfig.meeting_summary_model_ref 与 Project 配置 UI；
21. Provider / Model 物理删除、Agent model_ref / Project Meeting Summary Model 批量替换、PlatformModelSelection 引用处理，以及历史 snapshot 保留。

第一阶段不要求：

- Model 级 Protocol override；
- 自动 Model routing；
- silent fallback；
- 多模型 ensemble；
- 动态按价格 / 质量选择模型；
- Model Gateway 微服务；
- 一个统一万能 Adapter 覆盖 chat / embedding / reranker / image。
