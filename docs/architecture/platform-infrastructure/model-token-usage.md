# Model Token Usage 详细设计

> 状态：设计稿
>
> 上层架构：[平台基础设施与部署架构](./index.md)
>
> 相关架构：
> - [Agent Executor 架构](../agent-executor/README.md)
> - [Agent Loop 架构](../agent-loop/README.md)
>
> 相关详细设计：
> - [Model System 详细设计](./model-system.md)

## 1. 设计范围

本文定义 agenteam 如何记录和统计模型调用的 Token Usage。

目标是能够稳定回答：

- 某次 Agent Execution 使用了多少 Token；
- 某个 Agent 在指定时间范围内使用了多少 Token；
- 某个 Model 使用了多少 Token；
- 某个 Provider 使用了多少 Token；
- 某个 Project 总共使用了多少 Token；
- retry / failed request 实际消耗了多少 Token；
- input / output / cached input / reasoning 等 Token 分别是多少。

本文只负责 Token Usage 的运行记录和聚合统计。

本文不定义：

- Model Provider / Model 配置；
- 模型调用协议；
- Agent Execution 生命周期；
- 价格 / 账单；
- Provider 对用户实际收取的费用；
- Prompt / Response 内容存储。

Model Provider 如何产生标准化 usage，见 [Model System 详细设计](./model-system.md)。

## 2. 核心原则

1. 每一次真实发给 Provider 的模型调用都是 Usage 的最小事实单位。
2. retry 产生新的真实 Provider 请求，就产生新的 Usage Record。
3. Token Usage 的 Source of Truth 是 invocation-level record，不是 Agent / Model 上的累计 counter。
4. Agent Execution 保存可重建的 usage summary，但 summary 是派生结果。
5. Agent / Model / Provider / Project 统计通过 invocation records 聚合得到。
6. Provider 返回的 usage 与估算 usage 必须区分。
7. Provider 没有可靠 usage 时不伪造精确值。
8. Usage Record 不保存 Prompt、Response、Credential 或 Secret。
9. 第一阶段使用 PostgreSQL 保存事实记录并直接聚合，不提前引入独立 analytics infrastructure。

## 3. 数据流

~~~mermaid
flowchart LR
    Loop["Agent Loop"]
    Adapter["Model Adapter"]
    Provider["Model Provider"]
    Usage["Normalized ModelUsage"]
    Record["ModelInvocationUsage"]
    Execution["Agent Execution Usage Summary"]
    Query["Usage Query"]
    Dashboard["Agent / Model / Project Statistics"]

    Loop --> Adapter
    Adapter --> Provider
    Provider --> Adapter
    Adapter --> Usage
    Usage --> Record
    Record --> Execution
    Record --> Query
    Query --> Dashboard
~~~

Model System 负责把 Provider 原始 usage 转成统一 ModelUsage。

Token Usage 模块负责把这份 usage 与当前 invocation / Agent / Model / Project identity 一起持久化。

## 4. 最小事实单位：Model Invocation

一次 Model Invocation 表示：

> agenteam 实际向一个 Model Provider 发起的一次模型请求 attempt。

调用方可以是：

- Agent Execution 的 chat Model；
- Knowledge / Memory 的 embedding；
- Knowledge / Memory 的 reranker；
- Builtin Tool 使用的其他平台 Model，例如 image_generation。

它不是：

- 一整个 Agent Execution；
- 一个完整 Agent turn；
- 一次逻辑 retry operation；
- 一条 Meeting message。

例如：

~~~text
Agent Execution
├── Invocation 1
├── Tool Call
├── Invocation 2
│   └── provider timeout after usage
├── Invocation 3
│   └── retry of Invocation 2
└── Invocation 4
~~~

只要真正向 Provider 发起了一次请求，就应该拥有独立 invocation identity。

## 5. ModelInvocationUsage 数据模型

~~~text
ModelInvocationUsage
├── id
├── project_id
├── consumer_type
│   ├── agent
│   ├── knowledge
│   ├── memory
│   └── tool
├── agent_id?
├── execution_id?
├── provider_id?
├── model_config_id?
├── provider_id_snapshot
├── model_config_id_snapshot
├── provider_name_snapshot
├── protocol
├── model_id_snapshot
├── turn_index?
├── attempt_index
├── provider_request_id?
│
├── input_tokens?
├── output_tokens?
├── total_tokens?
├── cached_input_tokens?
├── cache_write_tokens?
├── reasoning_tokens?
│
├── usage_source
│   ├── provider
│   └── unknown
├── status
│   ├── succeeded
│   ├── failed
│   ├── cancelled
│   └── unknown
│
├── started_at
├── finished_at?
└── created_at
~~~

字段说明：

- project_id：所属 Project；
- consumer_type：本次模型调用的消费方；
- agent_id：由 Agent 发起或可归属到 Agent 时记录，否则为空；
- execution_id：属于 Agent Execution 时记录，否则为空；
- provider_id：当前 Provider 配置的可空外键引用；Provider 被物理删除后通过 `ON DELETE SET NULL` 置空；
- model_config_id：当前 ModelConfig 的可空外键引用；Model 被物理删除后通过 `ON DELETE SET NULL` 置空；
- provider_id_snapshot：调用发生时的 Provider stable ID 快照；
- model_config_id_snapshot：调用发生时的 ModelConfig stable ID 快照；
- provider_name_snapshot：调用发生时的 Provider 展示名称快照；
- protocol：调用时使用的 Provider Protocol；
- model_id_snapshot：调用时实际发送给 Provider 的 model_id；
- turn_index：chat Agent Loop 调用时记录逻辑模型轮次，其他 consumer 可以为空；
- attempt_index：同一逻辑轮次内的实际 Provider attempt 序号；
- provider_request_id：Provider 返回时可记录；
- token fields：标准化 Token Usage；
- usage_source：Token 数值来源；
- status：本次 Provider invocation 的结果；
- started_at / finished_at：实际请求时序。

Usage Record 不依赖 Provider / ModelConfig 后续仍然存在。物理删除配置只会清空 live foreign key，不删除 Usage Record；长期统计和历史追溯使用 provider_id_snapshot / model_config_id_snapshot / model_id_snapshot 等字段。

## 6. Invocation Identity 与 Retry

retry 必须独立记录。

~~~mermaid
sequenceDiagram
    participant L as Agent Loop
    participant A as Model Adapter
    participant U as Usage Store
    participant P as Provider

    L->>A: Model Request / turn 3
    A->>U: create invocation attempt 1
    A->>P: provider request
    P-->>A: partial response / error / usage
    A->>U: finalize attempt 1
    A-->>L: retryable error

    L->>A: retry same logical turn
    A->>U: create invocation attempt 2
    A->>P: provider request
    P-->>A: success + usage
    A->>U: finalize attempt 2
    A-->>L: Model Response
~~~

如果 Provider 在失败请求中仍然报告 usage：

> 这些 Token 必须统计。

因此不能只记录最终成功 attempt。

## 7. Usage Source

usage_source 用于区分 Token Usage 是否由 Provider 明确返回。

### 7.1 provider

~~~text
usage_source = provider
~~~

表示 Token 数值直接来自 Provider response / stream。

这是第一阶段唯一的精确 Token Usage 来源。

### 7.2 unknown

~~~text
usage_source = unknown
~~~

表示 Provider 没有返回可靠 usage。

对应 token 字段允许为空。

第一阶段不实现 tokenizer estimation，也不通过本地 tokenizer 或启发式算法补算 Token Usage：

~~~text
Provider 有 usage
-> provider

Provider 无 usage
-> unknown
~~~

平台不把“估算值”混入正式 Token Usage 事实。

## 8. Token 字段

标准字段：

~~~text
input_tokens
output_tokens
total_tokens
cached_input_tokens
cache_write_tokens
reasoning_tokens
~~~

其中：

- input_tokens：本次模型输入 Token；
- output_tokens：本次模型输出 Token；
- total_tokens：Provider 明确返回时保存；
- cached_input_tokens：Provider 报告的 cache read / cached input Token；
- cache_write_tokens：Provider 报告的 cache creation / cache write Token；
- reasoning_tokens：Provider 单独报告时保存。

Provider 不支持某字段时保持 null。

不能用 0 表示“未知”。

## 9. total_tokens 语义

不同 Provider 对 total_tokens 的计算口径可能存在差异。

因此：

- Provider 明确返回 total_tokens 时保存原值；
- Provider 未返回时，可以在统一展示层按已知字段计算近似总量；
- 但计算值不能覆盖 Provider 原始 total_tokens；
- cached / reasoning 是否包含在 input / output 中，以 Provider 原始语义为准。

为了避免跨 Provider 错误推断，Source of Truth 是各标准字段和 Provider usage metadata，而不是强行统一所有 Provider 的计费公式。

## 10. Agent Execution Usage Summary

Agent Execution 保存可重建的派生 summary：

~~~text
AgentExecutionUsageSummary
├── invocation_count
├── input_tokens
├── output_tokens
├── total_tokens
├── cached_input_tokens
├── cache_write_tokens
├── reasoning_tokens
└── unknown_usage_invocation_count
~~~

~~~mermaid
flowchart TB
    I1["Invocation 1"]
    I2["Invocation 2"]
    I3["Invocation 3 retry"]
    I4["Invocation 4"]
    Sum["AgentExecution Usage Summary"]

    I1 --> Sum
    I2 --> Sum
    I3 --> Sum
    I4 --> Sum
~~~

Summary 用于：

- Execution 页面快速展示；
- Loop Guard；
- execution result；
- 避免每次读取单个 Execution 都扫描全部 invocation。

但它不是唯一 Source of Truth。

如果 summary 与 invocation records 不一致，应允许从 invocation records 重建。

## 11. 聚合维度

第一阶段至少支持以下维度：

~~~text
Project
Consumer Type
Agent
Model
Provider
Agent Execution
Time Range
~~~

常见查询：

~~~text
Project + Time Range
Consumer Type + Time Range
Agent + Time Range
Model + Time Range
Provider + Time Range
Agent + Model + Time Range
Execution
~~~

例如：

~~~text
Agent A
2026-09-01 ~ 2026-09-30

input_tokens       12,400,000
output_tokens       1,800,000
cached_input        4,200,000
reasoning             600,000
invocations              840
~~~

## 12. 聚合模型

第一阶段直接使用 PostgreSQL aggregation：

~~~text
SUM(input_tokens)
SUM(output_tokens)
SUM(cached_input_tokens)
SUM(reasoning_tokens)
COUNT(*)
GROUP BY ...
~~~

按 Model / Provider 做长期历史统计时，使用 `model_config_id_snapshot` / `provider_id_snapshot` 分组，而不是依赖可能因为物理删除而变为 null 的 live foreign key。

不在 Agent / Model 表上维护不可重建的累计 counter。

~~~mermaid
flowchart LR
    Records["ModelInvocationUsage<br/>Source of Truth"]
    SQL["PostgreSQL GROUP BY"]
    Agent["By Agent"]
    Model["By Model"]
    Project["By Project"]
    Provider["By Provider"]
    Consumer["By Consumer Type"]

    Records --> SQL
    SQL --> Agent
    SQL --> Model
    SQL --> Project
    SQL --> Provider
    SQL --> Consumer
~~~

当数据量明显增长后，可以增加 daily rollup / materialized view，但 rollup 必须可以从 invocation records 重建。

## 13. 查询索引建议

ModelInvocationUsage 至少需要考虑：

~~~text
(execution_id, started_at)
(project_id, consumer_type, started_at)
(project_id, started_at)
(agent_id, started_at)
(model_config_id_snapshot, started_at)
(provider_id_snapshot, started_at)
(project_id, agent_id, started_at)
(project_id, model_config_id_snapshot, started_at)
~~~

具体索引数量应根据真实查询和数据量调整，不要求第一阶段一次性创建全部组合索引。

started_at 是时间范围统计的主要时间字段。

## 14. Agent / Model Dashboard

Usage UI 可以提供统一统计视图。

~~~mermaid
flowchart TB
    Usage["Usage"]
    Usage --> Project["Project"]
    Usage --> Agent["By Agent"]
    Usage --> Model["By Model"]
    Usage --> Provider["By Provider"]
    Usage --> Execution["Execution Detail"]

    Agent --> AgentModel["Agent + Model"]
    Model --> ModelAgent["Model + Agent"]
~~~

至少展示：

- input token；
- output token；
- cached input token；
- reasoning token；
- invocation count；
- unknown usage invocation count。

## 15. 与 Agent Loop Guard 的关系

Agent Loop 可以对当前 Execution 使用累计 Token Budget。

逻辑上：

~~~text
Execution Token Usage
=
SUM(all finalized invocation usage in current execution)
~~~

达到配置预算后，Loop Guard 可以阻止新的模型 turn。

但是：

- unknown usage 不能被当成 0 来证明“还有预算”；
- budget policy 如何处理 unknown usage，由 Agent Loop policy 决定；
- Token Usage 模块只提供事实数据和累计值，不决定是否继续执行。

## 16. Streaming Usage

部分 Provider 只在 stream terminal event 返回最终 usage。

因此 invocation record 的生命周期：

~~~text
started
    ↓
streaming
    ↓
terminal event
    ↓
finalized usage
~~~

如果连接异常结束：

- 已经收到 Provider usage，则保存；
- 没有收到 usage，则 usage_source = unknown；
- status 根据 Adapter 标准化结果保存 failed / cancelled / unknown。

不能因为 stream 没有成功完成就直接丢弃这次 invocation。

## 17. 与 Agent Execution Log 的关系

Usage Record 和 Execution Log 不是同一个东西。

~~~text
Execution Log
= 本次执行发生了什么

ModelInvocationUsage
= 每次实际模型请求消耗了多少 Token
~~~

Execution Log 可以引用 invocation_id。

Usage Record 不保存：

- 完整 Prompt；
- 完整 Model Response；
- Tool Result；
- reasoning 原文。

这样可以单独长期做统计，不需要读取大型 Execution Log。

## 18. 与 Audit 的关系

Token Usage 不是安全 Audit。

普通模型调用不需要因为产生 Token Usage 就额外写 Audit。

如果用户修改：

- Provider；
- Credential；
- billing / pricing policy；

是否产生 Audit，由对应 Governance / 配置操作决定。

## 19. Usage 与 Cost 的边界

第一阶段本文只设计 Token Usage。embedding / reranker 等平台内部模型调用只要 Provider 能返回 Token Usage，也进入同一事实表。image_generation 如果使用的是图片数量、像素或其他非 Token 计量单位，则不在本文强行折算成 Token，后续应另行设计对应 usage unit。

后续如果 ModelConfig 增加 pricing metadata，可以在 invocation 时记录 Pricing Snapshot，并根据 Usage 推导 Cost。

~~~text
ModelInvocationUsage
+
Pricing Snapshot
↓
ModelInvocationCost
~~~

但是 Cost 是派生值，不应反过来成为 Token Usage Source of Truth。

Provider 实际账单与平台推算费用也可能存在差异，应继续区分。

## 20. 第一阶段实现边界

第一阶段不设置 ModelInvocationUsage 的自动过期、自动清理或 retention policy。Usage Record 持续保留；以后如果数据规模或合规要求需要增加 retention / archive / cleanup，可以在不改变当前事实模型的前提下单独增加。

第一阶段建议实现：

1. ModelInvocationUsage 持久化，并使用可空 live Provider / Model foreign key + identity snapshot 支持配置物理删除；
2. 每次真实 Provider request 独立 invocation；
3. retry 独立计数；
4. provider / unknown usage source，不实现 tokenizer estimation；
5. 标准 input / output / cached / reasoning Token 字段；
6. 持久化可重建的 AgentExecution usage summary；
7. Agent / Knowledge / Memory / Tool consumer_type；
8. Project / Agent / Model / Provider / Execution 聚合；
9. 时间范围查询；
10. PostgreSQL aggregation；
11. Usage API / UI 可区分 unknown。

第一阶段不要求：

- tokenizer estimation / 本地 tokenizer 补算；
- 独立 OLAP / ClickHouse；
- daily rollup table；
- 实时 billing；
- 用户配额结算；
- 精确成本核算；
- 自动按价格选择 Model。
