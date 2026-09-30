# MCP Resource Adapter 详细设计

> 状态：初版设计稿
>
> 上层架构：
> - [MCP 集成架构](./README.md)
>
> 相关详细设计：
> - [MCP Protocol Runtime](./mcp-protocol-runtime.md)
> - [MCP Server Config / Connection Lifecycle](./mcp-server-config-lifecycle.md)
> - [MCP Tool Execution](./mcp-tool-execution.md)
> - [Object Storage](../platform-infrastructure/object-storage.md)
> - [Artifact Builtin Tools](../tool-system/artifact-tools.md)

本文定义 MCP Bridge 中正式的一等 **MCP Resource Adapter**。它负责把 MCP Resources capability 映射为 agenteam 内部的 Resource Catalog、Resource Reference 与 StoredObject，使 Resource 可以被 Agent、Tool Result 和其他平台能力统一消费。

## 1. 定位

MCP Resource Adapter 与 MCP Tool Adapter 平级：

~~~mermaid
flowchart LR
    Server["External MCP Server"]
    Protocol["MCP Protocol Runtime"]

    subgraph Bridge["MCP Bridge"]
        Tool["MCP Tool Adapter"]
        Resource["MCP Resource Adapter"]
    end

    ToolRuntime["Unified Tool Runtime"]
    Catalog["MCP Resource Catalog"]
    Object["ObjectStorageService"]
    Agent["Agent Execution"]

    Server <--> Protocol
    Protocol --> Tool
    Protocol --> Resource

    Tool --> ToolRuntime
    Resource --> Catalog
    Resource --> Object
    Catalog --> Agent
    Object --> Agent
~~~

Resource Adapter 不属于 Tool Adapter 的内部辅助模块。

核心边界：

> MCP Tool Adapter 负责 MCP Tools；MCP Resource Adapter 负责 MCP Resources。Tool Result 中出现 ResourceLink / EmbeddedResource 时，Tool Adapter 把资源语义交给 Resource Adapter，而不是自己实现另一套 Resource 处理逻辑。

## 2. 第一阶段协议能力

第一阶段正式支持 MCP Resources capability：

~~~text
resources/list
resources/templates/list
resources/read
~~~

Resource Adapter 负责：

- Resource Catalog discovery；
- Resource Template discovery；
- ResourceLink resolution；
- Resource read；
- EmbeddedResource normalization；
- Resource content materialization；
- StoredObject 持久化；
- Agent-facing resource projection；
- Resource discovery Audit / diagnostics。

Resource change notification / subscription 仍属于 Resource Adapter 的职责边界，但第一阶段不建立长期 subscription。

因此第一阶段刷新策略与 Tool Discovery 保持一致：

~~~text
Connect / Enable 必要 discovery
Central startup refresh
manual refresh
~~~

未来增加 `subscriptions/listen` 时，Resources list changed / resource updated event 仍由 Resource Adapter 消费。

## 3. Resource Catalog

Resource Adapter 为每个 Project MCP Connection 维护 Resource Catalog。

Catalog 至少包含两类对象：

~~~text
McpResourceDescriptor
├── resource_ref
├── connection_id
├── uri
├── name?
├── title?
├── description?
├── mime_type?
├── size?
└── catalog_revision

McpResourceTemplate
├── template_ref
├── connection_id
├── uri_template
├── name?
├── title?
├── description?
├── mime_type?
└── catalog_revision
~~~

Catalog 是 metadata，不保存实际 Resource 内容。

### 3.1 Scope

Resource discovery 以 MCP Connection 为实际 scope：

~~~text
Project
-> MCP Connection
-> Resource Catalog
~~~

同一个 System MCP Config 在不同 Project 中可能因为 Credential 不同看到不同 Resource。

Catalog 不能跨 Project 合并。

### 3.2 Stable Resource Reference

内部不要求业务层长期依赖原始 URI 作为数据库主键。

概念身份为：

~~~text
project_id
+ mcp_config_id
+ resource uri
~~~

内部实现可以使用 UUID，并保存：

~~~text
McpResourceRef
├── id
├── project_id
├── mcp_config_id
├── connection_id
└── uri
~~~

Resource URI 仍作为远端 MCP Server 的 protocol identity 保存。

## 4. Resource Discovery

Resource discovery 分别调用：

~~~text
resources/list
resources/templates/list
~~~

Protocol Runtime 负责：

- protocol revision compatibility；
- pagination / cursor；
- cache metadata；
- request / response；
- authentication；
- network transport。

Resource Adapter 负责：

- descriptor normalization；
- template normalization；
- catalog diff；
- catalog persistence；
- Agent-facing metadata。

一次 discovery 应在得到完整 logical result 后再提交 Catalog，避免分页中途暴露半完成状态。

### 4.1 Discovery Failure

Resource discovery 是 best-effort metadata synchronization，不承担 Server health check。

失败时：

- 保留上一版成功 Resource Catalog；
- 不删除上一版 Resource；
- 不把 Resource 自动标记 unavailable；
- 不影响已经 materialized 的 StoredObject；
- 记录 Audit / diagnostics。

如果从未成功 discovery，则 Catalog 保持为空。

真实 Resource 可读性最终由 `resources/read` 的调用结果体现。

## 5. Resource Template

Resource Template 不是已经存在的 Resource 内容，而是 URI 模板。

例如：

~~~text
catalog://genres/{genre}
repo://files/{path}
~~~

Adapter 保存 template metadata，但不预先展开所有可能 URI。

Agent / UI 使用模板时形成具体 URI，再通过 Resource Adapter 执行：

~~~text
template
-> concrete uri
-> resources/read(uri)
~~~

Resource Adapter 不尝试猜测模板变量全集。

## 6. Resource Read

Resource 读取固定通过同一个 MCP Connection：

~~~mermaid
sequenceDiagram
    participant C as Agent / Consumer
    participant R as MCP Resource Adapter
    participant P as MCP Protocol Runtime
    participant S as MCP Server
    participant O as ObjectStorageService

    C->>R: read(resource_ref / concrete_uri)
    R->>P: resources/read(uri)
    P->>S: MCP request
    S-->>P: ReadResourceResult
    P-->>R: contents[]
    R->>O: materialize contents
    O-->>R: StoredObject refs
    R-->>C: resource projection
~~~

Resource URI 不作为普通 HTTP URL 下载。

即使 URI 是：

~~~text
https://...
file://...
repo://...
custom://...
~~~

Resource Adapter 仍然把 URI 原样传给同一个 MCP Server 的 `resources/read`。

因此：

~~~text
Resource URI
!= agenteam Outbound HTTP target
~~~

只有 MCP Server endpoint 经过 Outbound Network Policy。

## 7. Read Result

`resources/read` 可以返回一个或多个 ResourceContents。

第一阶段统一支持：

~~~text
TextResourceContents
BlobResourceContents
~~~

每个实际内容都形成 StoredObject。

~~~text
TextResourceContents
-> UTF-8 bytes
-> ObjectStorageService
-> StoredObject

BlobResourceContents
-> decode base64
-> ObjectStorageService
-> StoredObject
~~~

即使文本内容较小，Resource 的 canonical materialized copy 仍然保存为 StoredObject。

这样 Resource 内容只有一套生命周期和持久化规则。

## 8. Resource Materialization

一次 read 形成 Resource Materialization：

~~~text
McpResourceMaterialization
├── resource_ref / uri
├── connection_id
├── fetched_at
├── protocol_revision
├── contents[]
│   ├── stored_object_id
│   ├── uri
│   ├── mime_type?
│   └── content metadata
└── cache_metadata?
~~~

Resource Materialization 表示：

> 某次从远端 MCP Server 读取到的 Resource 内容快照。

它不是 live Resource 本身。

同一 URI 后续重新读取可能形成新的 Materialization / StoredObject。

## 9. Agent-facing Projection

Agent 不直接消费 MCP wire payload，也不直接依赖 MinIO URL。

Resource Adapter 把 Materialization 转成 agenteam 内部投影：

~~~text
text resource
-> file_ref
+ optional inline text preview / content

image resource
-> image_ref

audio / pdf / binary / other file
-> file_ref + media_type
~~~

inline text 只是 Model Context projection，不是 canonical storage。

StoredObject 才是实际持久化内容。

Project scope、文件读取和下载权限继续由 ObjectStorageService / 上层业务权限控制。

如果 Agent 需要把当前 Resource Materialization 以明确名称长期交付给用户，可以继续调用：

~~~text
create-artifact(source_ref = resource file_ref / image_ref)
~~~

由 Artifact Builtin Tool 创建独立 ToolArtifact；Resource Adapter 自身不负责生成用户下载 URL。

## 10. Agent Resource Access

MCP Resource Adapter 提供内部 Resource Catalog / Read API，Agent 不直接调用 MCP protocol。

Agent 需要按需访问 MCP Resource，而不是在每次 Execution 启动时把所有 Resource 内容自动注入 Context。

平台提供两个 Resource Access Core Agent Tools：

~~~text
list-mcp-resources
read-mcp-resource
~~~

### 10.1 list-mcp-resources

用于查询当前 Project 已连接、enabled MCP Connection 中的：

- concrete Resources；
- Resource Templates；
- MCP source / connection identity；
- name / description / mime type 等 metadata。

它不读取真实内容。

### 10.2 read-mcp-resource

输入内部 resource ref，或基于已发现 Template 形成的 concrete URI。

调用链：

~~~text
Agent
-> read-mcp-resource
-> MCP Resource Adapter
-> resources/read
-> ObjectStorageService
-> file_ref / image_ref / text projection
~~~

这两个 Tool 是平台 Resource Access 能力，不是远端 MCP Tool，也不映射到某一个 `tools/list` item。

它们属于 Core Agent Tools：

- 不进入普通 Agent Capability 开关；
- 只访问当前 Project 的 enabled MCP Connections；
- 仍经过 Project scope、服务端 Authorization 与 Audit；
- 不获得 Credential plaintext。

## 11. Tool Result ResourceLink

MCP Tool Result 中的 ResourceLink 是 Resource Adapter 的正式入口之一。

~~~text
tools/call
-> CallToolResult.content[].ResourceLink
-> MCP Tool Adapter
-> MCP Resource Adapter
-> resources/read(link.uri)
-> StoredObject
-> ToolResult file_ref / image_ref / text projection
~~~

Tool Adapter 不直接下载 URI。

ResourceLink 必须绑定到产生该 Tool Result 的同一个 MCP Connection，不能拿到另一个 MCP Connection 上读取。

如果 Resource read 失败：

- Tool 的原始调用结果仍然是已知完成的；
- Resource materialization failure 作为结果后处理错误记录；
- ToolResult 中返回安全说明，不能伪造 file_ref。

具体 ToolResult 映射见 [MCP Tool Execution](./mcp-tool-execution.md)。

## 12. EmbeddedResource

EmbeddedResource 已经直接携带 Resource 内容，因此不再执行 `resources/read`。

~~~text
EmbeddedResource
-> MCP Resource Adapter
-> normalize contents
-> ObjectStorageService
-> StoredObject
-> ToolResult projection
~~~

EmbeddedResource 与 ResourceLink 共享同一套 materialization / StoredObject 规则。

## 13. Image / Audio 与 Resource 的边界

MCP Tool Result 的 `ImageContent` / `AudioContent` 是直接内容，不是 MCP Resource。

它们仍由 Tool Result normalizer 保存到 ObjectStorageService：

~~~text
ImageContent
-> StoredObject
-> image_ref

AudioContent
-> StoredObject
-> file_ref
~~~

只有：

~~~text
ResourceLink
EmbeddedResource
resources/read result
~~~

进入 MCP Resource Adapter 的 Resource 语义。

## 14. Cache Metadata

2026-era Resource list / read result 可以带 cache metadata。

第一阶段：

- 保存 safe cache metadata 用于 diagnostics；
- 不因为 TTL 自动删除 StoredObject；
- 不因为 TTL 到期自动把 Resource Catalog 标记 unavailable；
- Agent 明确再次 read 时可以重新读取并形成新的 Materialization。

后续如果增加 Resource read cache，可以把远端 cache hint 作为缓存策略输入，但不改变 StoredObject 的不可变内容语义。

## 15. Connection Lifecycle

Disable MCP Connection：

- 新 Agent Execution 不通过该 Connection list/read Resource；
- 已有 Materialization / StoredObject 不主动删除；
- 已运行 Execution 按既有 runtime binding / refs 继续。

Disconnect / Delete：

- Resource Catalog 不再作为 live source；
- 新 read 不允许解析到已删除 Connection；
- 已持久化 StoredObject 仍按业务引用生命周期保留；
- 已运行 Execution 使用 retained runtime shadow 时，可以继续完成已经建立的 Resource read。

Resource Catalog metadata 的物理清理不能删除仍被业务对象引用的 StoredObject。

## 16. Authentication / Security

Resource Adapter 使用 MCP Connection 已有 Credential Binding。

它不拥有独立 Credential。

Resource access 仍受：

- Project scope；
- MCP Connection enabled state；
- 服务端 Authorization；
- Object Storage access boundary；
- Audit。

Resource URI 是远端 MCP namespace，不自动获得本地文件系统或任意网络访问权限。

## 17. Error Model

Resource Adapter 至少区分：

~~~text
resource_not_found
resource_unavailable
invalid_resource_uri
resource_protocol_error
resource_contract_violation
resource_materialization_failed
capability_unsupported
~~~

对 Agent-facing `read-mcp-resource`，这些错误映射为 Unified ToolError。

Protocol transport / authentication error 继续由 Protocol Runtime 提供标准化分类。

## 18. Observability / Audit

至少记录：

- project id；
- connection id；
- resource ref / safe URI metadata；
- operation：list / templates-list / read / materialize；
- protocol revision；
- duration；
- result；
- read content count；
- StoredObject refs；
- safe error。

不得记录 Credential plaintext。

对于 URI 中可能包含敏感 query / path data 的场景，Audit 应使用安全脱敏表示，而不是无条件保存完整 URI。

## 19. Resource Subscription

Resource list changed / resource updated subscription 属于 Resource Adapter 的正式职责。

当前阶段不实现长期 subscription。

未来接入 Protocol Runtime `subscriptions/listen` 后：

~~~text
resources list changed
-> Resource Adapter
-> refresh Resource Catalog

resource updated(uri)
-> invalidate / mark stale cache
-> 不自动删除已有 StoredObject
~~~

notification 只作为 refresh / invalidation trigger，不直接绕过 normal read / discovery pipeline 修改业务状态。

## 20. 第一阶段实现边界

第一阶段包含：

1. 一等 MCP Resource Adapter；
2. `resources/list`；
3. `resources/templates/list`；
4. `resources/read`；
5. Project / Connection-scoped Resource Catalog；
6. Resource Template Catalog；
7. ResourceLink resolution；
8. EmbeddedResource materialization；
9. Text / Blob ResourceContents；
10. 所有实际 Resource 内容统一保存到 ObjectStorageService；
11. file_ref / image_ref / text projection；
12. `list-mcp-resources` / `read-mcp-resource` Core Agent Tools；
13. startup / manual / Connect 必要 discovery；
14. Audit / diagnostics。

第一阶段不实现：

- Resource change subscription；
- 基于 notification 的自动 Catalog refresh；
- 自动把所有 Resource 内容注入 Agent Context；
- 把 Resource URI 当普通 URL 下载；
- MCP Resource 与 Knowledge Base 自动同步；
- Resource 内容的自动 embedding / indexing。
