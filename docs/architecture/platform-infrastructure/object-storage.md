# Object Storage 详细设计

> 状态：设计稿
>
> 上层架构：
> - [平台基础设施与部署架构](./index.md)
>
> 相关设计：
> - [Unified Tool Runtime 详细设计](../tool-system/tool-runtime.md)
> - [Artifact Builtin Tools](../tool-system/artifact-tools.md)

## 1. 设计目标

Object Storage 是 agenteam 的统一文件 / 大对象持久化层。

它解决的不是某一种业务附件，而是所有需要保存文件性质、大体积或二进制内容的统一存储问题，包括：

- Attachment；
- Tool Artifact；
- MCP Resource Materialization；
- Knowledge 原始文档；
- Agent Execution / Runtime Log 的大段内容；
- 导出文件；
- 生成图片、生成文件；
- 其他不适合直接写入 PostgreSQL 的大对象。

第一阶段底层使用 MinIO，但上层业务模块不能直接依赖 MinIO SDK、bucket 或 object key。

统一关系为：

~~~text
Business Entity
    ↓ stored_object_id
StoredObject
    ↓
ObjectStorageService
    ↓
MinIO
~~~

## 2. 核心原则

1. `StoredObject` 是统一存储实体，只表达“一份对象已经被平台持久化”。
2. Attachment、Artifact、Execution Log 等继续保留各自业务语义，不合并为一个业务对象。
3. 业务表通过 `stored_object_id` 引用统一对象，不保存 MinIO bucket / key。
4. 所有对象访问统一经过 `ObjectStorageService`。
5. PostgreSQL 保存对象 metadata、业务索引和引用；MinIO 保存实际 payload。
6. 大段文件性质内容优先放入 Object Storage，不把 PostgreSQL 当作 blob storage 使用。
7. Object Storage Service 隔离底层实现；以后从 MinIO 切换到其他 S3-compatible storage 时不影响业务层。
8. 对象访问权限来自引用它的业务实体和当前调用上下文，不通过公开 object key 绕过业务授权。

## 3. StoredObject

建议第一阶段统一实体：

~~~text
StoredObject
├── id
├── storage_key
├── media_type
├── size
├── checksum
├── status
├── created_at
└── deleted_at?
~~~

字段语义：

- `id`：平台内部对象 ID，业务模块只持有这个 ID；
- `storage_key`：ObjectStorageService 内部使用的 MinIO object key，不向普通业务 API 暴露；
- `media_type`：MIME type；
- `size`：对象字节数；
- `checksum`：内容校验值，第一阶段建议 SHA-256；
- `status`：对象当前存储状态；
- `created_at`：对象创建时间；
- `deleted_at`：逻辑删除时间，可选。

第一阶段不把业务语义字段放入 StoredObject，例如：

- attachment name；
- artifact kind；
- execution id；
- task id；
- meeting id；
- tool operation id。

这些属于对应业务实体。

## 4. Object Reference

业务层保存的“对象引用”不是 MinIO URL，而是 StoredObject 的 ID。

概念上可以称为：

~~~text
StoredObjectRef
= stored_object_id
~~~

不要求为 StoredObjectRef 单独建表或独立实体。

例如：

~~~text
Attachment
├── attachment_id
├── stored_object_id
├── filename
└── ...

ToolArtifact
├── artifact_id
├── stored_object_id
├── kind
├── name?
└── ...

ExecutionLogObject
├── execution_id
├── stored_object_id
├── log_kind
└── ...
~~~

业务实体负责表达“这份对象是什么”；StoredObject 负责表达“这份内容存在哪里、大小是多少、是否完整”。

## 5. ObjectStorageService

ObjectStorageService 是业务模块访问对象存储的唯一平台接口。

概念 API：

~~~text
put(input, metadata) -> StoredObject

get(stored_object_id) -> ObjectStream

stat(stored_object_id) -> StoredObject

delete(stored_object_id)

create_download_url(stored_object_id, expires_in) -> SignedURL
~~~

具体 Go interface 可以在实现阶段确定，但职责边界固定。

### 5.1 put

负责：

- 分配 object ID；
- 生成内部 storage key；
- 写入 MinIO；
- 计算 / 校验 size；
- 计算 checksum；
- 写入 StoredObject metadata；
- 只有 payload 与 metadata 都形成可信状态后才返回可引用对象。

### 5.2 get

根据 `stored_object_id` 解析 StoredObject，再读取底层 payload。

业务模块不自行解析 storage key。

### 5.3 stat

只读取 metadata，不读取完整 payload。

适合 UI、Tool Result、下载准备、日志索引等场景。

### 5.4 delete

业务模块不能通过 MinIO SDK直接删除对象。

对象删除统一由 ObjectStorageService 执行，以便：

- 更新 StoredObject 状态；
- 删除底层 payload；
- 保持数据库 / MinIO 一致性；
- 后续扩展 retention / garbage collection。

### 5.5 create_download_url

如果 UI 或外部客户端需要下载对象，应在业务权限校验完成后，由服务端生成短期受控访问 URL。

业务 API 不返回长期公开 MinIO URL。

## 6. MinIO 映射

第一阶段 MinIO 是 ObjectStorageService 的默认 backend。

建议：

~~~text
StoredObject.id
    ↓
ObjectStorageService
    ↓
storage_key
    ↓
MinIO bucket / object
~~~

bucket、endpoint、credential 等属于平台基础设施配置，不进入业务对象。

`storage_key` 由 ObjectStorageService 生成，业务模块不自行拼接。

第一阶段不要求按 Attachment / Artifact / Log 分不同业务 bucket；具体 bucket 策略属于 ObjectStorageService 内部实现，不形成上层契约。

## 7. 写入一致性

PostgreSQL 与 MinIO 无法共享普通数据库事务，因此需要明确失败处理。

推荐写入流程：

~~~text
1. create StoredObject(status = pending)
2. write payload to MinIO
3. verify size / checksum
4. StoredObject -> available
5. business entity 保存 stored_object_id
~~~

如果 MinIO 写入失败：

~~~text
StoredObject -> failed
~~~

失败对象不能被业务实体作为有效对象引用。

如果 payload 已经写入 MinIO，但数据库状态更新失败，应允许后续 cleanup / reconciliation 清理 orphan object。

第一阶段不需要复杂分布式事务。

## 8. Object Status

第一阶段建议：

~~~text
pending
available
failed
deleted
~~~

语义：

- `pending`：对象正在写入，不能作为正常业务内容读取；
- `available`：metadata 与 payload 均可用；
- `failed`：写入失败或校验失败；
- `deleted`：对象已经删除，不允许继续读取。

## 9. Attachment

Attachment 是面向用户 / 业务的文件实体。

例如可以包含：

~~~text
Attachment
├── id
├── stored_object_id
├── filename
├── description?
├── created_by
└── created_at
~~~

Attachment 不保存 MinIO key。

同一个 Object Storage 能力可以被 Project、Task、Meeting、Knowledge 等不同业务模块复用，但每个模块仍决定自己的 Attachment 关联关系和权限。

## 10. Tool Artifact

Tool Artifact 是 Tool Runtime 对文件性质输出的业务引用。

统一关系：

~~~text
Tool Operation
    ↓
ToolArtifact
    ↓ stored_object_id
StoredObject
    ↓
ObjectStorageService
    ↓
MinIO
~~~

Tool Runtime 不直接操作 MinIO。

小型文本 / structured data 可以继续直接进入 ToolResult；大文件、图片、生成文件、大型 command output 等转为 ToolArtifact + StoredObject。

### 10.1 Agent-facing Artifact Tools

ObjectStorageService 本身不直接暴露为 Agent Tool。

Agent 通过 Artifact Builtin Tools 操作具有业务语义的 ToolArtifact：

~~~text
list-artifacts
create-artifact
read-artifact
~~~

因此：

~~~text
Agent
-> Artifact Builtin Tool
-> ToolArtifact
-> StoredObject
-> ObjectStorageService
~~~

而不是：

~~~text
Agent
-> put/get/delete StoredObject
~~~

Artifact Tools 的输入输出、source_ref、读取和用户下载规则见 [Artifact Builtin Tools 详细设计](../tool-system/artifact-tools.md)。

### 10.2 Artifact from Existing Object

Agent 可以把自己已有权限的 StoredObject-backed reference 转成一个明确命名的 ToolArtifact。

第一阶段不共享原 StoredObject，而是：

~~~text
source_ref
-> authorization
-> stream source object
-> new StoredObject
-> new ToolArtifact
~~~

这样不依赖尚未实现的 shared-object reference counting / GC。

## 11. Agent Execution / Runtime Log

Execution Log 的结构化索引和关键 metadata 仍保存在 PostgreSQL。

大段、文件性质的 log payload 可以存入 Object Storage：

~~~text
PostgreSQL
├── execution id
├── log index / sequence
├── timestamp
├── log type
└── stored_object_id

Object Storage
└── actual large log payload
~~~

这样可以避免把大量 stdout / stderr / runtime trace / transcript 直接写入 PostgreSQL 大字段。

是否需要进入 Object Storage 可以由日志大小和类型决定，小型结构化日志仍可以保存在 PostgreSQL。

## 12. Original Document 与 Knowledge

用户上传的原始文档同样使用 StoredObject。

Knowledge 系统保存：

- 文档业务 metadata；
- parsing / indexing 状态；
- chunk / embedding 等派生数据；
- `stored_object_id`。

原始文件 payload 只保存在 Object Storage。

## 13. 权限边界

StoredObject 自身不是绕过业务权限的公共下载资源。

访问流程应是：

~~~text
Caller
-> Domain Service / API
-> 校验 Project / Resource 权限
-> ObjectStorageService
-> StoredObject / MinIO
~~~

因此：

- 客户端不能仅凭 storage key 访问对象；
- MinIO credential 不暴露给普通用户或 Agent；
- Signed URL 必须短期有效；
- Agent 不直接调用 ObjectStorageService 或创建 Signed URL；
- Agent 只能通过有业务语义的 Artifact / Resource 等上层能力访问 StoredObject；
- 是否允许读取对象由引用它的业务实体决定；
- Secret masking / sensitive data policy 在内容进入 Object Storage 前仍按对应业务规则执行。

用户预览 / 下载应通过业务 API 校验权限后再调用 `create_download_url`，Signed URL 不进入 Agent Context 或长期 Tool Result。

## 14. 删除与引用关系

业务实体删除不应直接由数据库 cascade 删除 MinIO payload。

正确流程是：

~~~text
业务实体解除 / 删除对象引用
    ↓
ObjectStorageService / cleanup policy
    ↓
确认对象不再需要
    ↓
删除 StoredObject 与 payload
~~~

第一阶段可以采用显式删除策略，不要求实现自动 reference counting 或复杂 garbage collector。

未来如果多个业务实体需要共享同一个 StoredObject，可以在 Object Storage 层增加引用追踪 / GC，而不改变业务引用方式。

## 15. 大对象与流式访问

ObjectStorageService 的读取 / 写入接口应支持 stream，不要求把完整对象一次性加载到 Central 内存。

特别适用于：

- 大附件；
- command output；
- Execution log；
- Knowledge 原始文件；
- generated artifact。

第一阶段不要求实现复杂 multipart upload UI，但 Service contract 不应限制未来加入 multipart / resumable upload。

## 16. 第一阶段实现边界

第一阶段实现：

1. StoredObject 数据实体；
2. ObjectStorageService；
3. MinIO backend；
4. put / get / stat / delete；
5. 短期下载 URL；
6. size / media_type / checksum metadata；
7. pending / available / failed / deleted 状态；
8. Attachment / Tool Artifact / Original Document / large Execution Log 使用 `stored_object_id` 引用；
9. 流式读取 / 写入接口；
10. orphan object 的基础 cleanup 能力；
11. Artifact Builtin Tools 通过 ToolArtifact 间接使用 Object Storage。

第一阶段不要求：

- 内容级 deduplication；
- 跨对象自动合并；
- 自动 reference counting；
- 复杂生命周期分层存储；
- CDN；
- 多云 object storage replication；
- 用户直接访问 MinIO API；
- Agent 直接操作 StoredObject / MinIO / Signed URL。
