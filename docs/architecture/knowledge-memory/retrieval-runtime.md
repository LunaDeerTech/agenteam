# Retrieval Runtime 详细设计

> 上层架构：[Knowledge Base 与 Agent Memory](./README.md)
>
> 相关设计：[Knowledge Indexing](./knowledge-indexing.md)、[Agent Memory Domain](./agent-memory-domain.md)、[Agent Memory Runtime](./agent-memory-runtime.md)、[Model System](../platform-infrastructure/model-system/README.md)

## 1. 目标与边界

Retrieval Runtime 是 Knowledge query 与 Agent Memory recall 共用的底层检索基础设施。

它负责：

- lexical retrieval abstraction；
- dense retrieval；
- metadata / scope filtering；
- candidate generation；
- RRF fusion；
- optional reranker；
- retrieval result normalization；
- candidate / top-k 参数；
- Retrieval Eval；
- lexical backend benchmark。

它不负责：

- KnowledgeDocument canonical CRUD；
- Knowledge chunking；
- Memory retain / consolidation；
- Memory revision lifecycle；
- Agent Loop context management；
- Tool Authorization。

Knowledge 和 Memory 共用 Runtime，但通过不同 adapter 提供：

- scope；
- metadata；
- candidate eligibility；
- final ranking signals；
- output projection。

## 2. 总体 Pipeline

~~~mermaid
flowchart TB
    Query["Query"]
    Scope["Domain Scope / Filters"]

    Lex["Lexical Retriever"]
    Dense["Dense Retriever<br/>pgvector"]

    LC["Lexical Candidates"]
    DC["Dense Candidates"]

    RRF["RRF Fusion"]
    Domain["Domain Ranking Adjustment"]
    Rerank["Optional Reranker"]
    Top["Top Results"]

    Query --> Lex
    Query --> Dense
    Scope --> Lex
    Scope --> Dense

    Lex --> LC
    Dense --> DC
    LC --> RRF
    DC --> RRF
    RRF --> Domain
    Domain --> Rerank
    Rerank --> Top
~~~

reranker 为空时：

~~~text
RRF
-> Domain Ranking Adjustment
-> Top Results
~~~

## 3. 为什么采用 Hybrid Retrieval

项目知识同时存在两类 query。

语义型，例如：

~~~text
Scheduler 在什么条件下会跳过一个任务？
~~~

Dense retrieval 更擅长。

精确 lexical 型，例如：

~~~text
SchedulerDispatch
waiting_reference
dependency_target_cancelled
ERR_RUNNER_BUSY
~~~

这类 query 依赖：

- exact identifier；
- class / field name；
- error code；
- path；
- terminology。

Lexical retrieval 更可靠。

因此第一阶段不使用 vector-only。

## 4. Unified Retrieval Contract

概念请求：

~~~text
RetrievalRequest
├── query
├── scope
├── filters?
├── lexical_candidate_k
├── dense_candidate_k
├── fused_candidate_k
├── rerank_candidate_k?
└── result_k
~~~

统一候选：

~~~text
RetrievalCandidate
├── source_id
├── source_kind
├── raw_text
├── locator / metadata
├── lexical_rank?
├── dense_rank?
├── lexical_score?
├── dense_score?
├── fused_score
├── domain_score_adjustment?
└── final_score?
~~~

Retrieval Runtime 不要求 lexical_score 与 dense_score 位于相同数值空间。

## 5. Scope 必须前置过滤

scope 不能在全库 retrieve 后再靠应用层过滤。

Knowledge：

~~~text
project_id = current_project
document status = active
document version = current
index profile = serving
index state = ready
~~~

Memory：

~~~text
project_id = current_project
agent_id = current_agent
memory revision = active
memory status = active
~~~

这样：

- 防止跨 scope 信息出现在候选集；
- 防止 reranker / embedding pipeline 接触无权限数据；
- 提高 candidate quality。

## 6. Lexical Retriever Abstraction

统一接口概念：

~~~text
LexicalRetriever.search(
  query,
  scope,
  filters,
  candidate_k
) -> RankedCandidate[]
~~~

上层不依赖：

- PostgreSQL tsquery DSL；
- PGroonga query syntax；
- pg_search syntax；
- backend score range。

第一阶段候选 backend：

- native PostgreSQL FTS；
- PGroonga；
- pg_search。

### 6.1 不把 native PostgreSQL FTS 等同于 BM25

架构文档只表达 lexical retrieval。

如果选择 native PostgreSQL FTS：

- ranking 使用其 ts_rank / ts_rank_cd 语义；
- 不能在上层把 score 描述成 BM25。

如果选择提供 BM25 的 backend，则由该 adapter 暴露 backend-native score 供 diagnostics 使用。

RRF 只依赖 rank，避免 score normalization 绑定具体 backend。

## 7. Lexical Backend Benchmark

实现第一阶段前，使用中英混合项目文档做 benchmark。

至少比较：

~~~text
native PostgreSQL FTS
PGroonga
pg_search
~~~

测试内容应包含：

- 中文自然语言；
- 英文自然语言；
- 中英混合；
- exact identifier；
- snake_case / camelCase；
- file path；
- error code；
- architecture terminology。

比较：

- Recall@K；
- MRR；
- nDCG@K；
- index size；
- indexing latency；
- query latency；
- operational complexity。

最终 backend 是实现决定，但结果应记录在工程 ADR / benchmark 文档，不写死在 Retrieval Runtime 的领域 contract。

## 8. Dense Retriever

Dense Retriever 第一阶段使用 pgvector。

统一接口：

~~~text
DenseRetriever.search(
  query_embedding,
  scope,
  filters,
  candidate_k
) -> RankedCandidate[]
~~~

Knowledge 和 Memory 的新索引 generation 都以 PlatformModelSelection.embedding_model_ref 作为目标 embedding Model。

但查询时不能简单读取“当前 selector”后直接生成 query embedding。Domain adapter 必须提供当前 serving generation 的 embedding Model snapshot：

~~~text
Knowledge serving IndexProfile
    -> embedding model snapshot

Memory serving generation
    -> embedding model snapshot
~~~

query embedding 必须使用与当前 dense index 完全一致的 embedding space。

因此平台修改 embedding_model_ref 后，旧 generation 在 rebuild 完成前仍继续使用旧 embedding snapshot；只有 serving generation 切换后，query embedding 才一起切换。

### 8.1 Embedding identity

Dense index 必须知道：

- embedding model snapshot；
- vector dimension；
- index profile / memory index generation。

不能把不同 embedding space 的 vector 混在同一检索 generation。

## 9. Query Embedding

每次 query：

~~~text
query
  -> resolve serving generation embedding snapshot
  -> embedding adapter
  -> query vector
  -> dense search
~~~

如果 embedding provider 调用失败：

- dense leg 失败；
- 是否继续 lexical-only 取决于 Runtime degradation policy。

第一阶段建议：

> hybrid query 的某一路发生明确 transient failure 时，可以退化到另一条可用 retrieval leg，并在 result metadata 中标记 degraded；如果两路都失败，则 Tool error。

这样单个 embedding provider 短时故障不必让所有 exact lexical query 完全不可用。

但 authorization / scope error 不允许降级绕过。

## 10. Candidate Generation

两路独立取候选：

~~~text
Lexical Top N_l
Dense Top N_d
~~~

N_l / N_d 是可调 retrieval config。

不能假设：

~~~text
N_l = result_k
N_d = result_k
~~~

因为后续 RRF / reranker 需要足够候选池。

第一阶段具体数值由 Retrieval Eval 决定。

## 11. Reciprocal Rank Fusion

第一阶段使用 equal-weight RRF。

概念：

~~~text
RRF_score(d)
=
sum over retrievers:
1 / (k + rank_r(d))
~~~

其中 RRF constant k 是 retrieval config，不作为架构固定值。

优点：

- 不要求 lexical / dense score normalization；
- backend 更换时融合 contract 不变；
- 对单一路异常高分更稳健；
- 简单可解释。

第一阶段不使用 query-specific dynamic weighting。

## 12. Duplicate Candidate Merge

同一个 domain source 同时被 lexical / dense 命中时必须合并为一个 candidate。

Knowledge merge key：

~~~text
chunk_id
~~~

Memory merge key：

~~~text
memory_id + active_revision
~~~

合并后保留：

- lexical rank / score；
- dense rank / score；
- fused RRF score。

Tool output 不需要向 Agent 暴露全部内部 score。

## 13. Domain Ranking Adjustment

RRF 后允许 domain adapter 施加有限辅助信号。

### Knowledge

第一阶段不增加 recency / importance 等业务 bias。

Knowledge ranking：

~~~text
relevance
-> optional reranker
~~~

Document 新旧不应凭 updated_at 人为压过明显更相关内容。

### Memory

Memory recall 可以在 RRF relevance 基础上轻量考虑：

- importance；
- recency。

原则：

> relevance 为主，importance / recency 为辅。

不能出现：

~~~text
最新 Memory
> 明显更相关的旧 Memory
~~~

具体组合函数由 Retrieval Eval 调优。

## 14. Optional Reranker

平台：

~~~text
reranker_model_ref?
~~~

为空时跳过。

配置时：

~~~text
fused candidates
    ↓
take rerank_candidate_k
    ↓
reranker(query, candidate text)
    ↓
reranked order
    ↓
take result_k
~~~

Reranker：

- 不改变持久化索引；
- 不属于 IndexProfile；
- 可以随配置改变立即生效；
- 失败时可以按 degradation policy 回退 RRF order。

## 15. Reranker Input

Knowledge：

~~~text
query
+
chunk.index_text or retrieval text projection
~~~

Memory：

~~~text
query
+
active Memory revision text
+
minimal type metadata
~~~

不要把：

- Secret；
- unauthorized content；
- unrelated full Document；
- entire Memory namespace；

发送给 reranker。

## 16. Knowledge Retrieval Adapter

query-doc 调用 Retrieval Runtime。

概念：

~~~text
KnowledgeQueryRequest
├── project_id
├── query
├── filters?
└── result_limit?
~~~

Adapter 负责：

- Project scope；
- current Document version；
- active serving IndexProfile；
- Document metadata filter；
- Knowledge result projection。

输出：

~~~text
KnowledgeHit
├── document_id
├── document_version
├── title
├── locator
├── snippet
├── score?
└── media_type?
~~~

query-doc 用于：

> discovery + location

不是 full read。

Agent 需要更多上下文时再调用 read-doc。

## 17. Memory Retrieval Adapter

recall 调用同一 Retrieval Runtime。

概念：

~~~text
MemoryRecallRequest
├── project_id
├── agent_id
├── query
├── filters?
└── result_limit?
~~~

Adapter 负责：

- Project + Agent namespace；
- active Memory revision；
- type / tag filter；
- importance / recency adjustment；
- Memory projection。

输出：

~~~text
MemoryRecallHit
├── memory_id
├── type
├── text
├── tags?
├── importance?
├── provenance_summary?
└── score?
~~~

历史 revision 不参与 recall。

## 18. reflect 与 Retrieval Runtime

reflect 不是新的检索 backend。

它内部：

~~~text
reflect(query)
    ↓
Memory recall adapter
    ↓
ranked active memories
    ↓
memory_model_ref reasoning
    ↓
read-only answer
~~~

因此 reflect 与 recall 使用同一 namespace 和 retrieval contract。

reflect 不：

- 扫描其他 Agent Memory；
- 自动写 observation；
- 修改 recall ranking；
- 绕过 candidate limit。

## 19. Metadata Filtering

Knowledge 第一阶段可支持：

- document_id；
- media_type；
- source_kind。

Memory 可支持：

- type；
- tags；
- provenance kind。

Filter 必须在 retrieval leg 能力允许时尽量 push down。

不能先 retrieve 全部数据，再把 unauthorized result 删除。

## 20. Result Limit 与 Tool Result Budget

result_k 既是 retrieval 参数，也是 Model Context 成本控制点。

Tool Runtime / Knowledge Adapter 应避免 query-doc / recall 一次返回大量长文本。

原则：

~~~text
retrieve candidates
-> rank
-> return compact hits
-> read-doc / further recall when needed
~~~

Knowledge hit 默认返回 snippet，而不是 full chunk 周围整页。

Memory hit 返回一条完整 Memory，因为 Memory 本身应保持较短粒度。

## 21. Snippet

Knowledge snippet 优先来自 raw_text，不来自带 contextual prefix 的 index_text。

这样 Agent 看到的是原文，而不是：

~~~text
Document: ...
Section: ...
Page: ...

Document: ...
Section: ...
...
~~~

locator 单独结构化返回。

## 22. Degradation Policy

允许降级：

### Dense unavailable

~~~text
lexical-only
~~~

### Lexical unavailable

~~~text
dense-only
~~~

### Reranker unavailable

~~~text
RRF order
~~~

必须在内部 diagnostics 记录：

~~~text
retrieval_mode
= hybrid
| lexical_only
| dense_only
| hybrid_without_rerank
~~~

不允许降级：

- authorization failure；
- namespace validation failure；
- query input validation failure；
- index generation inconsistency。

## 23. Timeout

不同阶段应有独立 timeout：

- query embedding；
- lexical retrieval；
- dense retrieval；
- reranker。

不能只有一个巨大 retrieval wall-clock timeout 而无法知道卡在哪个阶段。

如果两路 candidate generation 并行：

~~~text
lexical
dense
~~~

可在各自 deadline 后 settle，再按 degradation policy 决定是否融合。

## 24. Observability

每次 retrieval 至少记录：

~~~text
request_id
domain_kind
scope_identity
query_length / hash
retrieval_mode
lexical_backend
lexical_candidate_count
dense_candidate_count
fusion_candidate_count
rerank_used
result_count
stage_latency
degradation_reason?
error?
~~~

不应默认把用户完整 query 写入普通日志。

敏感日志策略遵守 Security / Governance。

## 25. Retrieval Eval

需要维护独立小型评测集。

概念：

~~~text
RetrievalEvalCase
├── id
├── corpus / profile
├── query
├── relevant_sources
├── relevance_grade?
└── notes?
~~~

Knowledge relevant source 可以是：

~~~text
document_id
+ locator / expected section
~~~

Memory eval 可以使用 synthetic / sanitized namespace corpus。

## 26. 基础对照组

至少比较：

~~~text
Dense only
Lexical only
Hybrid RRF
Hybrid RRF + Reranker
~~~

这样可以确认：

- hybrid 是否真的提升；
- reranker 是否值得成本；
- lexical backend 是否适合中文 / identifier。

## 27. Eval 指标

第一阶段至少：

- Recall@K；
- MRR；
- nDCG@K。

另外记录：

- query latency；
- reranker latency；
- embedding latency；
- index size；
- candidate count；
- provider usage / cost。

不只优化一个离线 relevance 指标。

## 28. 参数治理

首期由管理员通过系统设置统一维护分块大小、候选/返回数量等默认值与上限，持久化为平台 Runtime Config；Project 使用平台配置，没有项目覆盖。Knowledge / Memory 可有不同场景默认值，合法调用参数受对应预算约束；管理参数不授予管理员他人项目知识或 Memory 正文访问权。

具体默认值通过中英混合 eval 决定，不在架构中提前固定经验数字。可调参数包括：

~~~text
soft_chunk_size
hard_chunk_size
forced_split_overlap
lexical_candidate_k
dense_candidate_k
RRF constant
rerank_candidate_k
result_k
Memory importance weight
Memory recency weight
~~~

第一阶段 RRF 固定等权，不开放不等权配置；RRF constant 与 candidate 数量的调优不改变该规则。配置/API、合法范围、版本并发和 UI 由 D01/D13/D14/D26/D27 落实，不把本节视为参数已完成评测。

参数变更分两类。

### Index-time

例如 chunk size：

- 需要新 IndexProfile；
- 需要 rebuild。

UI 分开展示保存配置、重建进度与实际 activation；旧 generation 仍 serving 时使用它记录的 embedding snapshot，不能仅因保存新配置立即切换查询模型。

### Query-time

例如 candidate_k：

- 不要求 reindex；
- 可以通过 runtime config / rollout 调整。

具体查询生效边界在正式配置契约明确。关键词 backend/数据库扩展仍按 §29 benchmark 选择，不作为普通管理员参数热切换。

## 29. Lexical Backend 选择流程

建议：

~~~text
1. 准备中英混合 eval corpus
2. 构建三套 lexical candidate backend
3. Dense leg 固定
4. 分别测 lexical-only
5. 分别测 hybrid RRF
6. 比较 relevance + latency + operation cost
7. 选择第一阶段 backend
8. 记录 benchmark 决策
~~~

不要只看 lexical-only 指标。

最终目标是 hybrid pipeline。

## 30. 第一阶段明确不做

- HyDE；
- LLM query rewrite；
- multi-query；
- query decomposition；
- agentic retrieval planner；
- dynamic retriever routing；
- learned fusion；
- automatic RRF weight learning；
- cross-Project retrieval；
- global Memory retrieval；
- external SaaS vector DB；
- reranker mandatory dependency。

## 31. 关键不变量

1. Scope filter 先于候选泄露。
2. Knowledge / Memory 共用 retrieval engine，不共用 domain authorization。
3. Lexical backend 通过 abstraction 隔离。
4. Dense 与 lexical 原始 score 不直接相加。
5. 第一阶段 equal-weight RRF。
6. Reranker 是 optional query-time stage。
7. Knowledge 不使用 recency bias。
8. Memory relevance 优先于 importance / recency。
9. query-doc 返回 discovery hit，不代替 read-doc。
10. reflect 复用 Memory recall，不建立另一套搜索系统。
11. 参数通过 eval 调优，不写成架构常量。
12. degraded retrieval 必须可观测。
