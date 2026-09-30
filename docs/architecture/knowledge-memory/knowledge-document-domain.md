# Knowledge Document Domain 详细设计

> 上层架构：[Knowledge Base 与 Agent Memory](./README.md)  
> 相关设计：[Knowledge Indexing](./knowledge-indexing.md)、[Retrieval Runtime](./retrieval-runtime.md)、[Object Storage](../platform-infrastructure/object-storage.md)、[统一工具系统](../tool-system/README.md)

## 1. 目标与边界

Knowledge Document Domain 负责 Project Knowledge Base 中 canonical document 的业务模型和生命周期。

它负责：

- KnowledgeDocument identity；
- Project scope；
- source kind / media type；
- title 与业务 metadata；
- canonical payload 对 Object Storage 的引用；
- document version；
- create / update / delete；
- list / read；
- Markdown / plain text 与 PDF / DOCX 的能力差异；
- indexing status projection；
- Meeting 等其他模块对 KnowledgeDocument 的稳定引用。

它不负责：

- Parser 细节；
- Chunker；
- embedding；
- lexical index；
- RRF / reranker；
- Agent Memory；
- Object Storage 底层 key / bucket；
- Agent Loop context assembly。

索引相关职责见 [Knowledge Indexing](./knowledge-indexing.md)。

## 2. Canonical Model

第一阶段建议的 canonical model：

~~~text
KnowledgeDocument
├── id
├── project_id
├── title
├── source_kind
├── media_type
├── stored_object_id
├── version
├── status
├── indexing_status
├── created_by
├── created_at
├── updated_at
└── deleted_at?
~~~

字段语义：

### 2.1 id

稳定 document identity。

Document 内容更新时：

- id 不变；
- version + 1；
- stored_object_id 指向新的 canonical payload。

任何长期引用必须引用 document_id，而不是：

- stored_object_id；
- chunk_id；
- index id；
- vector id。

### 2.2 project_id

Knowledge Base 严格 Project scoped。

所有 Domain query / mutation 必须先绑定 Project，再校验 document.project_id。

第一阶段不支持跨 Project Knowledge query。

### 2.3 title

Document 的业务标题。

title：

- 参与 UI 展示；
- 参与 list-docs；
- 作为 deterministic contextual prefix 的一部分进入索引；
- 不是 Object Storage filename 的替代品。

### 2.4 source_kind

固定：

~~~text
text
file
~~~

语义：

- text：平台内可编辑文本；
- file：上传文件型 canonical source。

第一阶段：

~~~text
Markdown    -> text
plain text  -> text
PDF         -> file
DOCX        -> file
~~~

source_kind 表达编辑和 ingestion 行为，不直接等同于 media_type。

### 2.5 media_type

保存规范 MIME type，例如：

~~~text
text/markdown
text/plain
application/pdf
application/vnd.openxmlformats-officedocument.wordprocessingml.document
~~~

Parser Registry 根据 media_type 选择 parser adapter。

### 2.6 stored_object_id

所有 canonical content 都进入 Object Storage。

KnowledgeDocument 不直接保存：

- MinIO bucket；
- storage key；
- signed URL；
- 大型正文 blob。

关系：

~~~text
KnowledgeDocument
    ↓ stored_object_id
StoredObject
    ↓
ObjectStorageService
    ↓
MinIO
~~~

Object Storage 是 payload storage；KnowledgeDocument 才是 Knowledge 业务身份。

### 2.7 version

从 1 单调递增。

version 同时用于：

- optimistic concurrency；
- indexing identity；
- query serving validity；
- read response；
- external reference consistency diagnostics。

第一阶段不维护完整 KnowledgeDocumentVersion 实体。

因此 version 代表：

> 当前 canonical content 曾发生过多少次有效替换。

旧 canonical payload 在业务上不再是可读历史版本。

### 2.8 status

Document lifecycle 第一阶段：

~~~text
active
deleted
~~~

删除采用 tombstone 语义。

deleted Document：

- 不出现在默认 list-docs；
- read-doc 返回已删除；
- query-doc 不得命中；
- 不允许继续 update；
- 等待派生索引与 StoredObject cleanup。

### 2.9 indexing_status

indexing_status 是面向业务 / UI 的索引状态 projection，不是索引系统的完整状态机。

第一阶段：

~~~text
pending
processing
ready
failed
~~~

语义：

- pending：canonical content 已写入，等待 indexing job；
- processing：当前 document version 正在构建索引；
- ready：当前 document version 存在可 serving 索引；
- failed：当前 document version 的索引构建失败。

IndexingJob / IndexProfile 是 Knowledge Indexing 自己的详细领域对象。

## 3. Source Kind 与编辑能力

### 3.1 text Document

Markdown / plain text：

- 可以从 UI 创建；
- 可以从 Agent Tool 创建；
- 可以 read；
- 可以网页直接编辑；
- 可以 update-doc 替换正文；
- 可以下载 canonical payload；
- 每次有效正文修改都产生新 version。

UI 编辑器不直接编辑 StoredObject。

正确流程：

~~~text
Editor
  -> Knowledge Document API
  -> validate expected version
  -> ObjectStorageService.put(new payload)
  -> update KnowledgeDocument
  -> enqueue indexing
~~~

### 3.2 file Document

PDF / DOCX：

- 可以上传；
- 可以 read metadata；
- 可以 preview；
- 可以 download；
- 可以 query；
- 可以通过重新上传文件执行 update；
- 不允许网页内直接修改文件正文。

UI 不应提供 PDF / DOCX 的富文本编辑然后重新导出功能。

如果用户要修改内容，应上传新的文件版本。

### 3.3 source_kind 不随普通 update 任意切换

第一阶段 update-doc 可以更新：

- title；
- text content；
- file replacement。

但不建议把同一 document identity 在 text / file 之间任意转换。

例如：

~~~text
Markdown -> PDF
~~~

应通过显式 replace source 操作完成，并同样：

- version + 1；
- 重新 indexing；
- 重新校验 media type。

实现层可以把它包含在 update-doc 中，但必须作为明确 source replacement，而不是只改 source_kind 字段。

## 4. Canonical Payload 写入

写入顺序：

~~~mermaid
sequenceDiagram
    participant C as Caller
    participant K as Knowledge Domain
    participant O as ObjectStorageService
    participant DB as PostgreSQL
    participant I as Indexing Queue

    C->>K: create / update
    K->>K: authorize + validate
    K->>O: put canonical payload
    O-->>K: StoredObject
    K->>DB: transaction KnowledgeDocument
    DB-->>K: committed
    K->>I: enqueue indexing intent
    K-->>C: canonical document response
~~~

canonical write 成功不等待 indexing 完成。

如果 Object Storage 写入失败：

- Domain mutation 不提交；
- 不产生新的 document version。

如果 StoredObject 成功但 KnowledgeDocument transaction 失败：

- 新 StoredObject 变成 orphan；
- 由 Object Storage cleanup 处理。

如果 KnowledgeDocument 已提交但 enqueue 失败：

- Document 仍然是 canonical 成功；
- indexing_status 保持 pending；
- reconciliation 必须能够重新发现并补发 indexing job。

不把异步索引调度和 canonical mutation 做成分布式事务。

## 5. Create Document

概念命令：

~~~text
create_document(
  project_id,
  title,
  source,
  media_type,
  request_id?
)
~~~

创建流程：

1. authorization；
2. 校验 media type；
3. 根据 source_kind 校验允许输入；
4. 写 StoredObject；
5. 创建 KnowledgeDocument(version = 1, status = active)；
6. indexing_status = pending；
7. 事务完成后安排 indexing；
8. 返回 canonical projection。

创建完成后：

- read-doc 立即可读；
- query-doc 需要等待索引 ready。

## 6. Update Document

概念命令：

~~~text
update_document(
  document_id,
  expected_version,
  title?,
  replacement_source?
)
~~~

### 6.1 Optimistic Concurrency

update 使用 expected_version。

如果：

~~~text
expected_version != current version
~~~

返回 conflict，不自动覆盖。

原因：

- UI 和 Agent 都可能更新同一 Document；
- document version 已经是 indexing identity 的组成部分；
- silent last-write-wins 会导致内容丢失。

Caller 应重新 read-doc 后决定是否重试。

### 6.2 Metadata-only update

如果只更新 title：

- canonical content 不变；
- stored_object_id 可以保持；
- version 仍递增，因为 title 会进入 contextual prefix / lexical representation；
- 需要重新 indexing。

只要变更会影响 retrieval representation，就必须生成新 document version。

### 6.3 Content update

更新正文 / 替换文件：

1. 写新的 StoredObject；
2. KnowledgeDocument.version + 1；
3. stored_object_id 切换到新对象；
4. indexing_status -> pending；
5. 旧 document version 的索引立即停止 serving；
6. 新 version 进入 indexing。

第一阶段不提供旧 content version read API。

## 7. Document 更新时的 Retrieval 可见性

必须保持：

> query-doc 返回的内容不能来自已经不是 canonical 的 document version。

因此内容更新后的窗口：

~~~text
Document v5
index v5 ready

update -> Document v6

read-doc -> v6 immediately
query-doc -> v5 no longer eligible
index v6 -> building

after ready:
query-doc -> v6
~~~

允许短暂出现：

~~~text
read-doc 可读
query-doc 暂时搜不到
~~~

不允许：

~~~text
query-doc 返回 v5 snippet
read-doc 只能读取 v6
~~~

IndexProfile-only rebuild 的 serving 规则不同，见 Knowledge Indexing。

## 8. Delete Document

概念命令：

~~~text
delete_document(
  document_id,
  expected_version?
)
~~~

删除流程：

1. authorization；
2. 标记 status = deleted；
3. deleted_at = now；
4. 立即使所有 index entry 不可 serving；
5. 事务后触发派生数据 cleanup；
6. 解除 / 清理 StoredObject 引用；
7. 后台按 Object Storage 策略删除 payload。

删除不是数据库 cascade 直接删 MinIO。

### 8.1 Meeting Reference

Meeting 可以长期引用：

~~~text
knowledge + document_id
~~~

Document 被删除后：

- Reference identity 不需要悄悄改写；
- UI 可显示 referenced document deleted；
- Agent 不得再通过 read-doc 读取正文；
- query-doc 不得命中。

这样保留历史 Meeting evidence，而不保留已删除 Knowledge 内容访问能力。

## 9. List Documents

Agent-facing / API query：

~~~text
list-docs
~~~

至少支持：

- project scope；
- text query / title filter；
- source_kind；
- media_type；
- indexing_status；
- pagination；
- stable sort。

默认只返回 active Document。

概念 projection：

~~~text
KnowledgeDocumentSummary
├── document_id
├── title
├── source_kind
├── media_type
├── version
├── indexing_status
├── updated_at
└── preview metadata?
~~~

list-docs 不返回完整 payload。

## 10. Read Document

~~~text
read-doc(document_id, locator?)
~~~

read-doc 面向 canonical Document，而不是 index chunk。

支持：

### 小型 text Document

可以直接返回全文。

### 大型 Document

支持 locator：

~~~text
page
section
heading_path
range
char_range
~~~

具体可用 locator 取决于 parser 能力和 media type。

read-doc 不要求 Caller 提供 chunk_id。

### 10.1 read-doc 与 parsed representation

对于 PDF / DOCX，Agent-facing read-doc 可以读取 parser 生成的可读文本 projection，而不是把二进制文件本身塞进 Model Context。

但 canonical source 仍是原始 StoredObject。

因此需要区分：

~~~text
Canonical payload
= uploaded PDF / DOCX

Readable projection
= parser output bound to current document version
~~~

Readable projection 属于可重建派生数据。

如果当前 version 尚未成功 parse：

- preview / download 仍可以工作；
- 结构化 read-doc locator 可能返回 processing / unavailable；
- 不回退读取旧 version parser output。

## 11. Preview 与 Download

### 11.1 PDF

UI 可以使用业务 API 获取受控 preview / download。

### 11.2 DOCX

UI 可以：

- 下载原文件；
- 使用服务端生成的预览 projection；
- 或由前端支持的安全 viewer 展示。

预览实现不改变 canonical model。

### 11.3 Signed URL

Signed URL：

- 只能在完成 Project / Document authorization 后生成；
- 短期有效；
- 不进入 Agent Context；
- 不持久化到 KnowledgeDocument；
- 不作为 Tool Result 的长期身份。

## 12. Agent-facing Knowledge Tools

第一阶段：

~~~text
list-docs
query-doc
read-doc
create-doc
update-doc
delete-doc
~~~

其中：

- query-doc / read-doc：Core Agent Tools；
- list-docs / create-doc / update-doc / delete-doc：普通可配置 Builtin Tools。

query-doc 与 read-doc 共同构成 Knowledge 的基础读取链路：query-doc 负责发现和定位，read-doc 负责读取 canonical Document 或其可读 projection，因此两者都不能通过普通 Agent Capability 关闭。

所有 Tool 最终调用 Knowledge Domain / Retrieval Runtime，不在 Tool Adapter 中复制业务规则。

### 12.1 create-doc

输入可以是：

- Markdown / plain text content；
- 已授权 Artifact / StoredObject-backed source 的业务引用。

Agent 不直接提交 MinIO key。

### 12.2 update-doc

Mutation 必须提供 / 使用当前 document version 进行 optimistic concurrency。

### 12.3 delete-doc

属于 destructive mutation，按 Security / Governance 的统一规则处理。

## 13. Authorization

Knowledge Document 的业务 scope 是 Project。

服务端必须验证：

~~~text
caller
  -> current project
  -> KnowledgeDocument.project_id
  -> operation authorization
~~~

Agent：

- 只能访问当前 Project；
- 不能通过 document_id 猜测访问其他 Project；
- 写操作同时受 Agent Capability / Tool Authorization；
- query-doc / read-doc 虽然是 Core Agent Tools，也不代表绕过 scope / authorization。

## 14. Indexing Status 与 UI

UI 至少展示：

~~~text
Ready
Indexing
Index failed
~~~

推荐映射：

~~~text
pending / processing -> Indexing
ready                -> Ready
failed               -> Index failed
~~~

failed Document：

- canonical content 仍存在；
- read / preview / download 正常；
- query-doc 不把该失败 version 当 ready index 使用；
- UI 提供 retry indexing。

Retry 不创建新的 document version。

它创建 / 重试同一个：

~~~text
document_id
+ document_version
+ index_profile_version
~~~

indexing identity。

## 15. Source of Truth

Knowledge Document Source of Truth：

~~~text
PostgreSQL KnowledgeDocument metadata
+
StoredObject canonical payload
~~~

不属于 Source of Truth：

- parsed text；
- structured elements；
- chunk；
- contextual prefix；
- embedding；
- lexical index；
- query result。

这些派生数据全部可以重新生成。

## 16. 第一阶段不做

- 完整历史 DocumentVersion 浏览；
- diff / restore old version；
- 协同实时编辑；
- PDF / DOCX 在线正文编辑；
- OCR correction UI；
- 跨 Project document sharing；
- public Knowledge URL；
- Document label / taxonomy 系统；
- chunk 作为用户可管理对象。

## 17. 关键不变量

1. 一个 active KnowledgeDocument 始终只有一个 current version。
2. canonical payload 只通过 stored_object_id 引用 Object Storage。
3. query-doc 不得返回非 current document version 的内容。
4. Document 更新后旧 version index 立即停止 serving。
5. read-doc 不依赖 chunk identity。
6. PDF / DOCX 不在网页内直接编辑正文。
7. deleted Document 不可再被 query / read。
8. index failure 不回滚 canonical write。
9. indexing 派生数据可以全部重建。
10. 跨模块长期引用只使用 document_id。
