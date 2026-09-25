# Artifact Builtin Tools 详细设计

> 状态：初版设计稿
>
> 上层架构：
> - [统一工具系统架构](./index.md)
>
> 相关详细设计：
> - [Object Storage](../platform-infrastructure/object-storage.md)
> - [Tool Result & Backend](./tool-result-backend.md)

本文定义 Agent 面向文件和持久化结果使用的 Artifact Builtin Tools。

## 1. 定位

ObjectStorageService 是平台基础设施，不直接暴露为 Agent Tool。

Agent 操作具有业务语义和权限边界的 Artifact：

~~~text
Agent
-> Artifact Builtin Tool
-> ToolArtifact
-> StoredObject
-> ObjectStorageService
-> MinIO
~~~

核心原则：

> Agent 可以创建、列举和读取 Artifact，但不能直接操作 StoredObject、storage key、MinIO 或 Signed URL。

## 2. Core Agent Tools

第一阶段提供：

~~~text
list-artifacts
create-artifact
read-artifact
~~~

这三个 Tool 属于 Core Agent Tools：

- 不进入普通 Agent Capability 开关；
- 仍受 Project / Resource Scope；
- 仍执行服务端 Authorization；
- 所有调用仍进入 Unified Tool Runtime / Audit；
- 不允许访问没有业务引用或当前调用方无权访问的 StoredObject。

## 3. Artifact 业务实体

第一阶段复用 Tool Runtime 已有的 ToolArtifact 业务语义。

概念模型：

~~~text
ToolArtifact
├── id
├── project_id
├── stored_object_id
├── kind
├── name?
├── description?
├── media_type?
├── source_execution_id?
├── source_operation_id?
├── created_by_agent_id?
└── created_at
~~~

StoredObject 只负责 payload 与存储 metadata；ToolArtifact 负责表达文件对 Agent / 用户的业务语义、Project scope、来源、展示名称和访问权限。

## 4. list-artifacts

用途：

> 让 Agent 查看当前 Project 中自己有权访问的 Artifact metadata。

可选过滤条件：

~~~text
execution_id?
kind?
media_type?
name_query?
limit?
cursor?
~~~

只返回 metadata，不读取完整 payload。

Agent 不能通过 list-artifacts 枚举没有业务引用的 StoredObject。

## 5. create-artifact

create-artifact 用于 Agent 主动生成一份可持久化、可向用户展示或下载的文件。

### 5.1 直接内容

~~~text
create-artifact
├── name
├── media_type
├── content
└── description?
~~~

流程：

~~~text
content
-> ObjectStorageService.put
-> StoredObject
-> ToolArtifact
-> file_ref / image_ref
~~~

适用于 Markdown / text 报告、JSON / CSV 导出、配置文件等由模型直接构造的合理大小内容。

Tool input 仍受统一 Tool argument / request size limit；超大内容不应通过一次模型 Tool Call 直接传入。

### 5.2 从已有对象创建

~~~text
create-artifact
├── name
├── source_ref
└── description?
~~~

source_ref 可以引用当前 Agent 已经有权访问的 Tool Artifact、MCP Resource materialization、Runner / Tool Result file_ref、image_ref 等 StoredObject-backed business reference。

第一阶段流程：

~~~text
source_ref
-> Domain Authorization
-> resolve source StoredObject
-> ObjectStorageService.get(stream)
-> ObjectStorageService.put(stream)
-> new StoredObject
-> new ToolArtifact
~~~

第一阶段采用复制而不是共享同一个 StoredObject，因为当前 Object Storage 尚未实现 reference counting / shared-object GC。

未来实现可靠引用追踪后，可以优化为共享 StoredObject，而不改变 Artifact Tool contract。

### 5.3 输出

create-artifact 返回：

~~~text
artifact_ref
name
media_type
size
file_ref / image_ref
~~~

Agent 不需要生成 Signed URL。

## 6. read-artifact

用途：

> 让 Agent 重新读取当前 Project 中已有 Artifact。

输入：

~~~text
artifact_ref
offset?
limit?
~~~

offset / limit 主要用于文本和可分段读取内容，避免大文件一次性进入 Model Context。

输出按统一 Tool Result 投影：

~~~text
text
-> text preview / requested range
+ file_ref

image
-> image_ref

pdf / document / audio / binary
-> file_ref + media_type
~~~

Artifact 的 canonical payload 始终保存在 StoredObject。

read-artifact 不是任意文件解析器。PDF、Office、压缩包等需要结构化解析时，应由专门能力处理。

## 7. 用户预览与下载

Agent 不生成下载 URL。

用户侧流程：

~~~text
Tool Result / Message
-> file_ref / image_ref
-> UI renders Artifact
-> user clicks preview / download
-> Backend verifies business permission
-> ObjectStorageService.create_download_url
-> short-lived Signed URL
-> browser accesses object
~~~

因此 Signed URL：

- 不进入 Agent Context；
- 不作为 Artifact stable identity；
- 不写入长期 Tool Result；
- 到期后由 UI / API 重新申请。

## 8. 不暴露的底层 Tool

第一阶段不提供：

~~~text
put-object
get-object
stat-object
delete-object
create-download-url
list-stored-objects
~~~

StoredObject 没有足够业务语义，直接暴露会绕开 Domain Authorization；storage key / MinIO 也不应进入 Agent contract。

## 9. 删除

第一阶段不提供 delete-artifact Core Tool。

Artifact 删除属于明确的业务 / 用户操作。后续如确有 Agent 删除需求，应单独定义删除权限、Approval policy、ownership 限制和 StoredObject cleanup。

## 10. Tool Result Integration

任何 Tool Backend 已经产生 file_ref / image_ref 时，Agent 可以直接交给用户，也可以使用 create-artifact(source_ref) 建立明确命名、可长期展示的 Artifact。

~~~text
Runner generated file
-> file_ref A
-> create-artifact(source_ref=A, name="build-report.zip")
-> Artifact B
-> user preview / download
~~~

MCP Resource 同理。

create-artifact 不要求 Agent 把已有二进制内容重新送入 Model Context。

## 11. Security / Authorization

至少保证：

- Artifact 必须属于当前 Project；
- source_ref 必须是当前 Agent / Execution 有权读取的业务引用；
- Agent 不接触 storage_key；
- Agent 不接触 MinIO credential；
- Agent 不接触 Signed URL；
- Secret / sensitive content 沿用对应业务来源的 masking / access policy；
- Audit 记录 Artifact ID / source reference / operation metadata，不复制完整 payload。

Core Tool 只表示基础可用能力，不表示绕过这些限制。

## 12. Audit

至少记录：

~~~text
agent_id
project_id
execution_id
operation_id
tool
artifact_id
source_ref?
stored_object_id
media_type
size
result
safe_error?
~~~

不记录完整 Artifact payload。

## 13. 第一阶段实现边界

第一阶段实现：

1. list-artifacts；
2. create-artifact(content)；
3. create-artifact(source_ref)；
4. read-artifact；
5. ToolArtifact -> StoredObject；
6. source_ref 服务端授权；
7. source object 流式复制；
8. file_ref / image_ref projection；
9. UI / API 按权限生成短期下载 URL；
10. Audit。

第一阶段不实现：

- Agent 直接操作 StoredObject；
- Agent 直接访问 MinIO；
- Agent 创建 Signed URL；
- delete-artifact；
- shared StoredObject reference counting；
- Artifact 自动内容解析 / embedding；
- multipart / resumable Agent Tool upload。
