# Knowledge Indexing 详细设计

> 上层架构：[Knowledge Base 与 Agent Memory](./README.md)  
> 相关设计：[Knowledge Document Domain](./knowledge-document-domain.md)、[Retrieval Runtime](./retrieval-runtime.md)、[Model System](../platform-infrastructure/model-system/README.md)

## 1. 目标与边界

Knowledge Indexing 负责把 canonical KnowledgeDocument 转换为可检索派生数据。

它负责：

- IndexProfile；
- Parser Registry；
- Chunker Registry；
- structured element model；
- section-aware chunking；
- deterministic contextual prefix；
- KnowledgeChunk；
- embedding generation；
- lexical indexing；
- asynchronous indexing job；
- idempotency；
- profile rebuild；
- serving switch；
- cleanup / reconciliation。

它不拥有：

- KnowledgeDocument canonical content；
- Agent-facing query ranking；
- Agent Memory domain；
- Object Storage payload lifecycle；
- Model Provider 配置。

KnowledgeDocument 是事实来源；Indexing 是可重建派生系统。

## 2. 总体流程

~~~mermaid
flowchart LR
    Doc["KnowledgeDocument<br/>current version"]
    Object["StoredObject"]
    Parser["Parser Adapter"]
    Elements["Structured Elements"]
    Chunker["Section-aware Chunker"]
    Prefix["Contextual Prefix"]
    Chunks["KnowledgeChunk"]
    Embed["Embedding"]
    Lexical["Lexical Index"]
    Dense["pgvector"]
    Ready["Index Ready"]

    Doc --> Object
    Object --> Parser
    Parser --> Elements
    Elements --> Chunker
    Chunker --> Prefix
    Prefix --> Chunks
    Chunks --> Embed
    Chunks --> Lexical
    Embed --> Dense
    Lexical --> Ready
    Dense --> Ready
~~~

同一个 indexing identity 必须完成全部 required index write 后才能 ready。

## 3. IndexProfile

IndexProfile 代表“如何把 canonical content 转换成持久化 retrieval representation”。

概念模型：

~~~text
IndexProfile
├── id
├── version
├── parser_profile
├── chunker_profile
├── contextualization_mode
├── embedding_model_ref
├── lexical_backend
├── lexical_config
├── index_schema_version
├── status
├── created_at
└── activated_at?
~~~

### 3.1 version

平台维护当前 active IndexProfile version。

Document indexing identity：

~~~text
document_id
+ document_version
+ index_profile_version
~~~

### 3.2 parser_profile

至少表达：

- parser registry version；
- format-specific parser version；
- parser options。

例如：

~~~text
markdown:v1
plain_text:v1
pdf:v2
docx:v1
~~~

Parser 行为变化如果可能改变 structured elements，就必须产生新的 IndexProfile。

### 3.3 chunker_profile

至少包含：

~~~text
chunker_version
soft_chunk_size
hard_chunk_size
forced_split_overlap
table_strategy
code_block_strategy
~~~

这些是可调参数，不是架构常量。

### 3.4 contextualization_mode

第一阶段固定支持 deterministic contextual prefix。

例如：

~~~text
document_title
heading_path
page
section label
+
raw chunk text
~~~

未来若实验 LLM contextualization，应作为新的 mode，而不是静默改变现有索引。

### 3.5 embedding_model_ref

引用 PlatformModelSelection.embedding_model_ref 对应 System embedding Model。

Embedding Model 改变时：

- 新建 IndexProfile；
- 后台重建；
- 不能把不同 embedding space 写进同一 active dense index。

Platform selector 的变更只决定新的目标 IndexProfile。旧 IndexProfile 仍在 serving 期间，query embedding 必须继续使用该 serving profile 记录的 embedding Model snapshot；只有新 profile 完成 activation 后，查询才切换到新 embedding space。

### 3.6 lexical_backend

保持抽象：

~~~text
native_pg_fts
pgroonga
pg_search
...
~~~

第一阶段实际 backend 在实现前通过中英混合 benchmark 决定。

### 3.7 reranker 不属于 IndexProfile

reranker 是 query-time stage，不改变持久化 index representation。

因此 reranker_model_ref 变化：

- 不触发 Knowledge reindex；
- 只影响 Retrieval Runtime。

## 4. IndexProfile Lifecycle

建议：

~~~text
building
ready
active
retired
failed
~~~

语义：

- building：profile 正在初始化 / 全量重建；
- ready：profile 可用于 serving，但尚未切为 active；
- active：当前新 Document indexing 与 query serving 的 profile；
- retired：已被替换，等待 cleanup；
- failed：profile build 不可用。

同一时刻只允许一个 active profile。

## 5. Parser Registry

统一 contract：

~~~text
Parser.parse(
  media_type,
  object_stream,
  metadata
) -> StructuredDocument
~~~

Registry：

~~~text
media_type
  -> Parser Adapter
~~~

第一阶段：

~~~text
text/markdown -> MarkdownParser
text/plain    -> PlainTextParser
application/pdf -> PdfParser
DOCX MIME     -> DocxParser
~~~

Parser Adapter 必须输出统一 structured element model，避免 Chunker 理解具体文件格式。

## 6. Structured Element Model

概念模型：

~~~text
StructuredElement
├── kind
├── text
├── ordinal
├── heading_path?
├── page?
├── char_range?
├── source_locator?
└── metadata
~~~

kind 第一阶段至少支持：

~~~text
heading
paragraph
list
code_block
table
caption
other
~~~

Element 是 parser output，不是长期业务实体。

它可以：

- 临时存在 worker 内存；
- 或作为当前 indexing job 的可重建中间产物持久化。

如果为了 read-doc locator 需要保存 parsed projection，可以持久化，但仍必须绑定：

~~~text
document_id
document_version
parser_profile
~~~

旧 version parsed projection 不用于当前 Document read。

## 7. Markdown Parser

Markdown Parser 应优先保留：

- heading hierarchy；
- paragraph；
- list；
- fenced code block；
- table；
- block quote。

heading_path 示例：

~~~text
[
  "Scheduler",
  "Dispatch",
  "Claim Lifecycle"
]
~~~

Chunker 不应因为 token size 很小就跨越明显的一级 / 二级 section 把不相关段落强行拼接。

Code block 应尽量保持完整。

如果 code block 超过 hard limit：

- 才进入 forced split；
- split locator 必须保留同一 heading path 和 code metadata。

## 8. Plain Text Parser

Plain text 没有可靠 heading 时：

1. 先识别空行 paragraph；
2. paragraph 内识别 sentence boundary；
3. 输出 paragraph / sentence-position metadata。

不要求 LLM 做语义 section 推断。

如果能从简单文本格式可靠识别标题样式，可以作为 parser heuristic，但不能成为必须依赖。

## 9. PDF Parser

PDF parser 目标不是“抽出一长串文本”，而是尽量还原：

- page；
- heading；
- paragraph；
- list；
- table；
- caption；
- reading order。

第一阶段不要求完美版面理解，但 Parser contract 必须允许以后替换成更高质量实现。

PDF 的 page 是重要 locator。

Table 优先输出独立 table element，不和前后段落混成同一 blob。

扫描型 PDF 如果没有可提取文本：

- indexing job 可以 failed with unsupported / extraction_failed；
- 第一阶段不强制 OCR；
- 未来加入 OCR 时只需要新增 parser capability / profile version。

## 10. DOCX Parser

DOCX parser 应利用结构信息：

- Heading styles；
- paragraphs；
- lists；
- tables；
- section order。

DOCX 没有稳定 page 语义时，不强行生成伪 page。

locator 可以使用：

- heading_path；
- paragraph ordinal；
- table ordinal；
- char range。

## 11. Section-aware Chunker

Chunker 输入 StructuredDocument，输出 raw chunks。

目标优先级：

1. 保留语义结构完整；
2. 控制 chunk size；
3. 不制造过量 overlap；
4. 保留可回溯 locator。

基础策略：

~~~text
Structured Elements
    ↓
group by section / heading path
    ↓
aggregate adjacent compatible elements
    ↓
soft size target
    ↓
hard limit check
    ↓
forced split only when necessary
~~~

### 11.1 soft_chunk_size

目标尺寸。

当当前 chunk 接近 soft size：

- 优先在 element boundary 结束；
- 不为了精确凑满 size 拆 paragraph。

### 11.2 hard_chunk_size

绝对上限。

单个 element 超过 hard limit 时才执行 forced split。

### 11.3 forced_split_overlap

只用于 forced split。

正常 section / paragraph 边界不机械 overlap。

这样避免：

- 每个 chunk 重复大量内容；
- lexical / vector ranking 被重复片段污染；
- index size 无意义膨胀。

## 12. Element Compatibility

默认可以相邻聚合：

- paragraph + paragraph；
- list + list；
- heading 下的短 paragraph。

默认优先独立：

- table；
- 大 code block；
- 特殊结构块。

Chunker 可以把 heading 文本作为 section metadata，而不需要把 heading 重复写进每个 raw chunk body。

Contextual prefix 会负责把 heading context 加入 retrieval representation。

## 13. Deterministic Contextual Prefix

每个 raw chunk 生成 index_text：

~~~text
context_prefix
+
raw_text
~~~

context_prefix 只使用确定性 metadata，不额外调用 LLM。

示例：

~~~text
Document: Scheduler Architecture
Section: Scheduler > Dispatch > Claim Lifecycle
Page: 8

<raw chunk>
~~~

不同格式可省略不存在的信息。

### 13.1 两种文本必须分开

KnowledgeChunk 至少逻辑区分：

~~~text
raw_text
index_text
~~~

raw_text：

- 用于 query-doc snippet；
- 用于 read / citation；
- 不包含人为重复 prefix。

index_text：

- 用于 dense embedding；
- 用于 lexical indexing。

这样 contextual enrichment 不污染 Agent 看到的 canonical excerpt。

## 14. KnowledgeChunk Model

概念模型：

~~~text
KnowledgeChunk
├── id
├── document_id
├── document_version
├── index_profile_version
├── ordinal
├── raw_text
├── index_text
├── locator
├── content_hash?
├── embedding?
├── created_at
└── serving_state
~~~

唯一约束：

~~~text
UNIQUE(
  document_id,
  document_version,
  index_profile_version,
  ordinal
)
~~~

chunk_id 可以使用普通 UUID / ULID。

不能只用：

~~~text
document_id + document_version + ordinal
~~~

因为同一个 canonical version 在不同 IndexProfile 下可能切出完全不同的 chunk。

## 15. Locator

locator 是 structured metadata。

概念：

~~~text
ChunkLocator
├── heading_path?
├── page?
├── section?
├── element_range?
├── char_range?
└── ordinal
~~~

query-doc 返回 locator，read-doc 可以使用 locator 进一步读取 canonical / parsed projection 附近内容。

locator 不承诺所有字段都存在。

## 16. Embedding Write

每个 chunk.index_text 使用 active profile 的 embedding_model_ref。

调用结果写入 dense index。

必须记录至少：

- embedding model identity / snapshot；
- index profile version；
- dimension；
- chunk id。

如果返回 dimension 与 profile 预期不一致：

- indexing job failed；
- 不把部分新索引切到 ready。

## 17. Lexical Index Write

Lexical backend 接收同样的 chunk.index_text。

统一 contract 概念：

~~~text
LexicalIndexer.upsert(chunks, profile)
LexicalIndexer.delete(index_identity)
LexicalRetriever.search(query, scope, candidate_k)
~~~

Knowledge Indexing 不让上层依赖 backend-specific query DSL。

Backend-specific tokenizer / analyzer 属于 IndexProfile.lexical_config。

## 18. IndexingJob

概念模型：

~~~text
KnowledgeIndexingJob
├── id
├── document_id
├── document_version
├── index_profile_version
├── status
├── attempt_count
├── error_code?
├── error_detail?
├── created_at
├── started_at?
└── finished_at?
~~~

唯一业务 identity：

~~~text
document_id
+ document_version
+ index_profile_version
~~~

重复 enqueue 必须返回 / 合并到同一业务 job，而不是重复构建多个竞争索引。

## 19. Job State

建议：

~~~text
pending
running
succeeded
failed
cancelled
obsolete
~~~

### obsolete

Job 运行期间 Document 已更新到更高 version：

~~~text
job v5 running
document -> v6
~~~

v5 job 即使继续完成，也不能 ready / serving。

Worker 在关键阶段必须 re-check current version。

可将 job 标为 obsolete，并清理其派生数据。

## 20. Job Execution

推荐步骤：

~~~text
1. load current KnowledgeDocument
2. validate document version
3. load canonical StoredObject
4. resolve IndexProfile
5. parse
6. chunk
7. build deterministic prefix
8. write lexical candidates
9. generate embeddings
10. write dense candidates
11. verify completeness
12. re-check document/profile eligibility
13. mark index ready
14. update KnowledgeDocument indexing projection
~~~

只有步骤 12 校验通过后才能 serving。

## 21. Partial Failure

不能出现：

~~~text
lexical v7 ready
dense v7 half-written
query starts serving v7
~~~

同一个 index identity 必须有聚合 readiness。

实现方式可以是：

- staging tables / generation id；
- ready flag；
- serving generation pointer。

架构要求：

> Retrieval Runtime 只看完整 ready generation。

## 22. Document Content Update

当 Document v5 -> v6：

1. canonical v6 提交；
2. v5 立即不再 serving；
3. enqueue v6 + active profile；
4. v6 build；
5. ready 后 serving。

即使 v5 物理 index entry 尚未 cleanup：

- Retrieval filter 也必须排除；
- 不能继续命中。

## 23. IndexProfile-only Rebuild

当 canonical content 没变，只改变 IndexProfile：

~~~text
profile p3 active
document v6 / p3 serving

create p4
build v6 / p4 in background

p4 all required coverage ready
atomic profile activation
p4 serving
p3 retired
cleanup p3
~~~

这里允许 p3 在 p4 build 期间继续 serving，因为 canonical Document version 相同。

## 24. 全量 Profile Migration

如果 Project 文档很多，不要求一次 transaction 全部重建。

可以：

- profile = building；
- 并发 enqueue 每个 active Document；
- 记录 build progress；
- 达到 activation policy 后切换。

第一阶段推荐严格策略：

> active Project KnowledgeDocument 全部在新 profile ready 后才 activate。

避免同一个 query 混用多个 profile ranking space。

### 24.1 Migration 期间的 Document 写入

Profile migration 不能假设 KnowledgeDocument 在 rebuild 期间冻结。

当 candidate profile 正在 building 时，新建 / 更新 Document：

1. 当前 active profile 仍按正常规则构建 current document version，用于恢复 / 保持现网 query serving；
2. candidate profile 同时为同一个 current document version 建立 shadow indexing job；
3. 如果 Document 再次更新，旧 version 在两套 profile 下都立即失去 serving / activation eligibility；
4. candidate profile activation 前重新校验每个 active Document 的 current version 都存在 candidate profile ready generation。

因此 migration 是持续 catch-up，而不是只对启动时的静态文档快照做一次 rebuild。

### 24.2 Activation

activation 必须原子切换 Project Knowledge 的 serving profile identity。

切换前：

- query embedding 使用旧 serving profile 的 embedding snapshot；
- lexical / dense 都读取旧 serving generation。

切换后：

- query embedding 使用新 profile 的 embedding snapshot；
- lexical / dense 都只读取新 serving generation。

不能出现 lexical 已切换、dense 仍使用旧 profile 的部分切换状态。

如果未来数据量过大需要 gradual migration，再单独设计 mixed-profile serving policy。

## 25. Reconciliation

后台 reconciliation 至少扫描：

### Missing job

~~~text
document indexing_status = pending
but no active job
~~~

重新 enqueue。

### Stuck running

超过合理 worker lease / heartbeat：

- 标记 failed / retryable；
- 重新调度。

### Orphan index

index identity 不再对应：

- current Document version；
- active / building IndexProfile；
- retained migration generation。

进入 cleanup。

### Ready mismatch

Document indexing_status = ready，但 serving index 不存在：

- 修正 projection；
- enqueue rebuild。

## 26. Retry

Retry 必须区分：

- transient model / network error；
- parser deterministic failure；
- unsupported format；
- storage read failure；
- backend write failure。

transient 可以自动 retry。

deterministic unsupported / parse failure：

- job -> failed；
- UI 展示；
- 手动 retry 不应无限循环，除非 parser/profile 已变化。

## 27. Delete Cleanup

Document deleted：

1. serving eligibility 立即 false；
2. cancel / obsolete active indexing jobs；
3. 删除 lexical entry；
4. 删除 dense entry；
5. 删除 parsed projection；
6. 后续由 Knowledge Domain / Object Storage 清理 canonical StoredObject。

物理 cleanup 可以异步。

关键约束是：

> delete commit 后 query-doc 立即不能再检索该 Document。

## 28. Observability

至少记录：

- indexing job duration；
- parser duration；
- chunk count；
- average / p95 chunk size；
- forced split count；
- embedding request count；
- embedding token / usage；
- lexical write duration；
- dense write duration；
- failure reason；
- obsolete job count。

这些数据用于后续 Retrieval Eval 和参数调优。

## 29. 与 Retrieval Eval 的关系

IndexProfile 的可调字段不能凭感觉修改后直接覆盖 active profile。

推荐流程：

~~~text
candidate IndexProfile
    ↓
offline build / eval corpus
    ↓
Retrieval Eval
    ↓
compare metrics
    ↓
accept or reject
    ↓
production rebuild
~~~

评测指标与 query pipeline 见 [Retrieval Runtime](./retrieval-runtime.md)。

## 30. 第一阶段不做

- LLM semantic chunking 默认路径；
- 每个 chunk 单独调用 LLM 生成 contextual summary；
- OCR pipeline；
- multimodal PDF embedding；
- image/table specialized embeddings；
- incremental delta embedding inside same Document version；
- mixed-profile serving；
- automatic profile optimizer；
- distributed vector database；
- cross-Project shared index。

## 31. 关键不变量

1. Knowledge index 永远可从 canonical Document 重建。
2. indexing identity = document_id + document_version + index_profile_version。
3. chunk identity 必须包含 index_profile_version。
4. reranker 不属于 IndexProfile。
5. Document version 更新后旧 version 立即失去 serving eligibility。
6. IndexProfile-only rebuild 可以由旧 profile serving 到新 profile ready。
7. Retrieval Runtime 只读取完整 ready generation。
8. normal chunk boundary 不机械 overlap。
9. contextual prefix 只影响 index_text，不改 raw_text。
10. parser / chunker / embedding / lexical backend 改变必须通过新 IndexProfile 表达。
