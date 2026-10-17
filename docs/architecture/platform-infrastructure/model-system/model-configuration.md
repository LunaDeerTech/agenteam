# Model Configuration 详细设计

> 状态：设计稿
>
> 上层架构：[平台基础设施与部署架构](../README.md)
>
> Model System 总览：[README](./README.md)
>
> 相关详细设计：
> - [Model Resolution](./model-resolution.md)
> - [Chat Model Runtime](./chat-model-runtime.md)
> - [Deployment Runtime](../deployment-runtime.md)

## 设计范围

本文定义 Model System 的长期配置面，包括：

- ModelProvider；
- System / Project Provider；
- ModelConfig；
- Model type 与平台用途 selector；
- Agent / Meeting 等 consumer 的长期 Model 引用；
- Provider Credential 安全边界；
- Provider / Model 删除与替换；
- Model Management UI。

本文不定义 Resolver / Snapshot 生成过程，也不定义 Provider Adapter 的模型调用 contract。

## Model Provider

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

非 chat 类型使用各自独立 Adapter，首版协议范围固定为：

| Model type | 首版协议范围 | 不纳入首版的原生接口 |
| --- | --- | --- |
| embedding | OpenAI Embeddings 及经实际验证的兼容接口 | Ollama 原生向量接口 |
| reranker | Jina Rerank 及经实际验证的兼容接口 | Cohere 原生 Rerank |
| image_generation | OpenAI 图片生成及经实际验证的兼容接口 | ComfyUI 原生工作流 |

Protocol profile 必须明确支持的请求、响应、错误和 usage；兼容标签或 endpoint 路径本身不是验证证据。例如 `/v1/rerank` 并不能唯一识别 Jina 协议。D09 固定 profile、Provider/type 兼容矩阵、测试服务及 conformance 场景，D13/D14/D21 验证真实消费；本文不声称已经实现这些 Adapter 或通过真实请求验收。

## System Provider 与 Project Provider

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

### System Provider

System Provider：

- 由平台管理员配置；
- 可以供多个 Project 使用；
- Credential 属于平台 Secret；
- Project 不获得 Credential；
- Provider / Model 修改只影响后续新 Execution。

### Project Provider

Project Provider：

- 只属于一个 Project；
- 只能被该 Project 的 Agent 使用；
- 由 Project Owner 配置；
- Credential 仍由平台 Secret Management 保存；
- 不进入 Project Environment Variables；
- 不通过 Agent Secret Variable 白名单暴露；
- 第一阶段只允许配置 `type = chat` 的 Model。

`embedding`、`reranker`、`image_generation` 属于平台内部能力，只能配置在 System Provider 下，不能由 Project Provider 定义。

### Model 可见性

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

## Model Config

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

### Model / Provider 删除策略

Provider 和 ModelConfig 支持物理删除，不要求长期保留 disabled 记录。

#### 删除 chat Model

如果待删除 chat Model 仍被 Agent.model_ref 或系统 `platform.summary` selector 引用，用户侧必须先通过删除确认弹窗选择替代 chat Model。

确认删除时，服务端以一个事务性操作完成：

1. 校验替代 Model 当前 enabled 且对所有受影响 Agent / Project 可见；系统 selector 的替代必须为 System Model；
2. 校验替代 Model 的 type = chat，并满足全部受影响用途的能力要求；
3. 校验受影响 Agent 的 reasoning_effort 在替代 Model 下仍然合法；存在不兼容配置时，删除流程必须先要求用户解决，不能留下无效 Agent 配置；
4. 同事务把所有受影响 Agent.model_ref 与系统 selector 更新为替代 Model，分别更新其版本；
5. 完成后物理删除原 ModelConfig。

System chat Model 可能被多个 Project 的 Agent 及共享总结 selector 引用，因此其替代 Model 必须是对全部受影响 Project 可见的 enabled System chat Model。总结替代还须支持`json_schema`；同事务只更新一次共享引用并推进平台用途版本，不批量改写 Project 配置。Memory 与 Meeting 的新调用随后解析同一选择，已接受调用保持原 snapshot。

Project chat Model 只影响所属 Project，可以替换为该 Project 当前可见的 enabled System chat Model 或 Project chat Model。

系统管理员跨 Owner 的替换只获得受控配置替换能力，不因此读取其他用户的项目正文。D01/D09 固定引用扫描、并发新增引用、版本冲突、原子替换及调用 snapshot 的一致性；不能在扫描后漏掉新引用或留下部分替换成功。

#### 删除平台内部 Model

如果待删除 Model 正被平台用途 selector 引用：

- embedding_model_ref：必须先选择另一个 enabled System embedding Model，不能清空；
- reranker_model_ref：可以选择另一个 enabled System reranker Model，也可以清空；
- summary_model_ref：必须先选择另一个 enabled System chat Model，且支持`json_schema`，不能清空；
- image_generation_model_ref：可以选择另一个 enabled System image_generation Model，也可以清空。

selector 更新完成后才能物理删除对应 ModelConfig。

#### 删除 Provider

Provider 不能级联删除其 Models。

只有当 Provider 下已经不存在任何 ModelConfig 时，才能物理删除 Provider。用户需要先逐个完成该 Provider 下 Model 的删除 / 替换流程。

Provider 配置删除与 Credential Secret 的生命周期分离。删除 Provider 不能直接使仍被运行中 ResolvedModel.credential_ref_snapshot 引用的 Secret 失效；Secret 清理必须在没有运行中引用后再进行。

#### 历史记录

AgentExecution、ModelInvocationUsage 等历史记录不阻止 Provider / Model 的物理删除。

历史记录对 Provider / Model 的数据库外键使用可空引用，并采用 `ON DELETE SET NULL`；与此同时必须保留调用时的 Provider / Model identity snapshot，因此配置记录删除后仍可追溯历史 Execution 和 Usage。

## Model 类型、平台用途与运行时接口

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
    SummaryChat["System chat + json_schema<br/>summary_model_ref"]
    Emb["embedding"]
    Rank["reranker"]
    Image["image_generation"]

    Agent["Agent.model_ref"]
    Select["PlatformModelSelection"]
    KM["Knowledge / Memory"]
    MemoryRuntime["Agent Memory Runtime"]
    MeetingSummary["Meeting Summary Generator"]
    ImageTool["Builtin generate-image Tool"]

    ChatAdapter["Unified Chat Model Contract"]
    EmbAdapter["Embedding Adapter"]
    RankAdapter["Reranker Adapter"]
    ImageAdapter["Image Generation Adapter"]

    System --> Chat
    System --> SummaryChat
    System --> Emb
    System --> Rank
    System --> Image

    Project --> Chat

    Chat --> Agent
    Agent --> ChatAdapter

    Emb --> Select
    Rank --> Select
    SummaryChat --> Select
    Image --> Select

    Select --> KM
    KM --> EmbAdapter
    KM --> RankAdapter

    Select --> MemoryRuntime
    MemoryRuntime --> ChatAdapter
    Select --> MeetingSummary
    MeetingSummary --> ChatAdapter

    Select --> ImageTool
    ImageTool --> ImageAdapter
~~~

### chat

`chat` 是统一文本生成 / Tool Calling 模型类型。

Agent 通过 `model_ref` 选择当前 Project 可用的 chat Model，然后由 Agent Loop 通过 Unified Chat Model Contract 调用。

此外，System chat Model 可作为平台“总结模型”：`summary_model_ref`对应唯一的`SummarySelector = summary`，供 Memory 与 Meeting 共用。它仅接受启用的 System chat Model，并要求`json_schema`能力；Project chat Model 仍用于本 Project Agent 等原有合法用途。两个 consumer 共用该 selector 与 Unified Chat Model Contract，分别保留 prompt、Purpose、usage metadata 和业务生命周期。

System Provider 和 Project Provider 都可以配置 chat Model。

本文后续的 Unified Model Contract 专指 Chat / LLM 调用。

### embedding

`embedding` 只用于平台 Knowledge / Memory 的向量化与检索基础能力。

它：

- 不允许被 Agent 直接选择为运行模型；
- 不暴露为模型可见 Tool；
- 第一阶段只能由 System Provider 配置；
- 由 PlatformModelSelection 的 `embedding_model_ref` 选择。

Embedding Adapter 的批处理、输入限制、维度校验及错误/usage 归一化由 D09 明确。selector 变化创建新的索引 profile/generation 并后台重建；实际 serving 仍用旧 generation 的 embedding snapshot，完整新 generation 就绪后才原子切换，不能混合向量空间。见 [Knowledge Indexing](../../knowledge-memory/knowledge-indexing.md#35-embedding_model_ref)。

### reranker

`reranker` 只用于 Knowledge / Memory retrieval 的可选 reranking 阶段。

它：

- 不允许被 Agent 直接选择为运行模型；
- 不暴露为模型可见 Tool；
- 第一阶段只能由 System Provider 配置；
- 由 PlatformModelSelection 的 `reranker_model_ref` 选择；
- selector 允许为空，表示 retrieval 不启用模型 reranking。

首版按已验证的 Jina Rerank profile 处理 query、候选文档、返回索引/分数和 usage；具体字段与排序约束由 D09 conformance 固定，不因路径相同静默接受其他协议或切换模型。

### image_generation

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

首版调用 OpenAI 图片生成及已验证兼容接口，典型路径为 `/v1/images/generations`；这不自动增加图片编辑或工作流能力。生成结果经媒体校验后进入平台 Object Storage；Provider 返回远端 URL 时，由正式出站能力安全取回，不能绕过网络策略，也不能把临时 Provider URL 当作永久平台对象。请求/返回 profile、错误/usage 和结果保存由 D09/D05/D18/D21 绑定验证。

### Platform Model Selection

平台基础设施用一个 PlatformModelSelection 保存四个用途，配置、版本和保存回执均为一份：

~~~text
PlatformModelSelection
├── embedding_model_ref
├── summary_model_ref
├── reranker_model_ref?
└── image_generation_model_ref?
~~~

这些字段只保存 ModelConfig 的稳定 ID，不重复配置 Model。HTTP对应`embedding / summary / reranker / image`，`summary_model_ref`的运行 selector 与平台引用 role 均为`summary`；不再保留独立 Memory 或 Meeting Summary 配置。

约束：

- embedding_model_ref：必填、启用的 System embedding Model；
- summary_model_ref：必填、启用的 System chat Model，供应商亦启用，支持`json_schema`；
- reranker_model_ref：可空、启用的 System reranker Model；
- image_generation_model_ref：可空、启用的 System image_generation Model。

整组初始可未配置；保存时 embedding 与 summary 必填，另两字段显式可空。所有用途都是平台配置，不支持 Project override，通过唯一`/system/model-selection`原子更新四字段与版本，不存在第二次会议配置写入。早期开发直接重建用途配置，不从旧 Memory 或 Meeting 选择自动迁移。

Knowledge / Memory 共用 embedding / reranker。Memory 的 extraction / consolidation / reflect 与 Meeting Rolling Summary initial/update（含首轮标题）共用 summary，不继承 Agent 的 chat Model；两者各自的输出契约、Purpose、用量与业务生命周期保持独立。

如果 `image_generation_model_ref` 未配置，则平台没有可用的默认 image generation backend，`generate-image` Tool 不应作为可执行 Tool 暴露给 Agent。

### 系统 Meeting Summary Model

Meeting Rolling Summary initial/update（含首轮标题）与 Memory 共用系统管理员配置的`platform.summary`（`summary_model_ref`）。Project 消费系统选择，不保存或在创建时复制模型初值，也不提供 override；Agent compaction 沿原 Execution snapshot，其他 Purpose 与 Execution Summary read model 不变。

该共享 selector：

- 保存稳定 ModelConfig ID；
- 由系统管理员显式选择 enabled System `type = chat` Model，要求`json_schema`，不接受 Project Model；
- 初始可以未配置，不猜默认模型，也不阻止 Project 创建；未配置时 Summary 生成明确失败；
- 一旦选择，本范围不提供清空；停用保留引用并阻止新生成，删除必须同事务替换为合法 Model；
- 新逻辑 generation 解析当前选择并固化 selector version / Model snapshot；已接受的同一逻辑调用及重试继续使用原 snapshot，不因当前选择变化热换模型。

这是已确认的目标契约；统一配置、Resolution 与两个 consumer 的绑定须按当前工作项分别验收，本文不以设计声明代替运行结果。Project 既有未绑定预留端口不因此启用。

它不要求所选 Model 支持：

- Tool Calling；
- parallel Tool Calling；
- image / file input。

Meeting Summary Generator 按会议固定输出 envelope 生成并校验内容；共享 selector 的`json_schema`能力门不改变会议内容与提交边界，本轮保留现有调用格式：

```text
ResponseFormat = text
tools = []
```

并直接通过 Unified Chat Model Contract 调用所选 Model。

这些会议辅助调用使用独立 `meeting` consumer 分类，保留 Project、Meeting 和具体用途；Agent 在 Meeting 中发言仍按 `agent` 记账，不能重复计量。每次真实请求单独记录，Provider 未返回 usage 时标记 unknown，见 [Model Token Usage](../model-token-usage.md#5-modelinvocationusage-数据模型)。

完整 Summary 生成流程见 [Meeting Context & Summary](../../meeting/meeting-context-summary.md)。

## Agent Model Reference

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

## Provider Credential 与安全边界

Model Provider Credential 属于平台 Secret Management。

长期配置只保存 credential_ref。

Secret value 统一使用 AES-256-GCM 信封加密，数据密钥加密 Secret、部署主密钥保护数据密钥。原始带版本主密钥环只从部署环境变量注入；数据库仅保存密文、受保护数据密钥和非敏感版本/迁移元数据。Central 通过 Secret Management 解析 credential_ref，原始密钥不进入数据库或 ModelConfig / ResolvedModel Snapshot；轮换与恢复沿用部署专题。

Secret storage 与 master key 边界见 [Deployment Runtime](../deployment-runtime.md)。

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

## UI 配置模型

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
Summary Model          -> System chat Model + json_schema
Reranker Model         -> optional System reranker Model
Image Generation Model -> optional System image_generation Model
~~~

这些 selector 只选择已有 Model，不在这里编辑 Model 参数。

总结模型供 Memory 与 Meeting 共用，只允许选择 enabled System chat Model，并且支持`json_schema`。

Agent 配置页只消费当前 Project 可用的 chat Model，并根据所选 Model 的 parameters.capabilities.reasoning_efforts 展示 reasoning_effort 选项。

System 平台模型用途页将向量、总结、重排、图像四项平级展示，只有一个保存动作；“重新获取”位于标题行右侧，模型显示“供应商名称/模型ID”。一份四字段草稿对应一个资源版本与回执，409保留草稿，Unknown沿原命令恢复，不拆分保存或制造部分成功。

未配置状态须明确显示；保存成功不等于真实调用已可用。Project 会议配置只说明消费系统统一模型，不提供本地选择器或第二个编辑入口，也不因此新增 Project 只读 HTTP。用途页不展示 Agent Capability、Tools 或 reasoning effort 配置。

## 第一阶段实现边界

第一阶段配置面实现：

- System / Project 两级 Model Provider，所有 enabled System chat Model 对所有 Project 可见；
- 一个 Provider 固定一个 Protocol；
- 显式 Provider + ModelConfig；
- System Provider 支持 chat / embedding / reranker / image_generation，Project Provider 只支持 chat；
- 非 chat 首版分别采用已验证的 OpenAI Embeddings、Jina Rerank、OpenAI 图片生成 profile；
- PlatformModelSelection：embedding / summary / optional reranker / optional image_generation，一份配置与版本；
- Agent model_ref / reasoning_effort 与 Memory、Meeting 共用的系统 summary selector；
- 会议辅助请求使用独立 meeting consumer，用途与关联 identity 由正式 usage 契约定义；
- Provider Credential secret reference；
- Provider / Model 物理删除、引用替换与历史 snapshot 保留约束。
