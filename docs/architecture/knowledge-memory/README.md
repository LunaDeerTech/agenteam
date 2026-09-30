# Knowledge Base 与 Agent Memory 架构

> 详细设计：
> - [Knowledge Document Domain](./knowledge-document-domain.md)
> - [Knowledge Indexing](./knowledge-indexing.md)
> - [Retrieval Runtime](./retrieval-runtime.md)
> - [Agent Memory Domain](./agent-memory-domain.md)
> - [Agent Memory Runtime](./agent-memory-runtime.md)
>
> 相关架构：
> - [Agent Management](../agent-management.md)
> - [Agent Loop](../agent-loop/README.md)
> - [统一工具系统](../tool-system/README.md)
> - [Model System](../platform-infrastructure/model-system.md)
> - [Object Storage](../platform-infrastructure/object-storage.md)
> - [Security / Governance](../security-governance/README.md)

## 1. 定位

Knowledge Base 和 Agent Memory 都为 Agent 提供长期可复用信息，但它们不是同一个业务系统。

| Knowledge Base | Agent Memory |
|---|---|
| Project 共同知识 | 单个 Agent 在某个 Project 内的长期经验 |
| 用户和 Agent 都可以显式维护 | 主要由 Agent 主动沉淀，用户可以管理 |
| PRD、设计、架构、说明、决策等正式文档 | 事实、经验、方法、观察 |
| Project scoped | Project + Agent scoped |
| 相对权威、可作为项目共享依据 | 允许包含 Agent 的归纳与经验判断 |
| Document 生命周期 | Memory revision / consolidation 生命周期 |

两者共享 retrieval 基础设施，但必须保持独立：

- domain schema；
- namespace；
- authorization；
- canonical source；
- mutation semantics。

Knowledge Base 是跨 Agent 的正式共享知识；Agent Memory 不承担跨 Agent 共享职责。

## 2. 总体架构

~~~mermaid
flowchart TB
    Project["Project"]
    Agent["Project Agent"]
    Loop["Agent Loop"]
    Tools["Unified Tool Runtime"]

    subgraph KB["Knowledge Base"]
        Document["Knowledge Document<br/>Canonical Source"]
        Indexing["Knowledge Indexing"]
        KTools["Knowledge Tools"]
    end

    subgraph MEM["Agent Memory"]
        Memory["Memory Store<br/>Project + Agent Namespace"]
        MRuntime["Memory Runtime<br/>retain / recall / reflect"]
    end

    subgraph RET["Shared Retrieval Runtime"]
        Lexical["Lexical Retriever"]
        Dense["Dense Retriever<br/>pgvector"]
        Fusion["RRF"]
        Rerank["Optional Reranker"]
    end

    subgraph Models["Platform Model Selection"]
        Embed["embedding_model_ref"]
        Rank["reranker_model_ref?"]
        MemoryModel["memory_model_ref"]
    end

    Project --> Document
    Document --> Indexing
    Indexing --> Lexical
    Indexing --> Dense

    Agent --> Memory
    MRuntime --> Memory
    Memory --> Lexical
    Memory --> Dense

    KTools --> RET
    MRuntime --> RET

    Lexical --> Fusion
    Dense --> Fusion
    Fusion --> Rerank

    Embed --> Dense
    Rank --> Rerank
    MemoryModel --> MRuntime

    Loop --> Tools
    Tools --> KTools
    Tools --> MRuntime
~~~

Agent Executor 和 Agent Loop 不在 Execution 启动时自动读取 Knowledge 或 recall Memory。

所有长期知识访问都通过 Tool 按需发生。

## 3. Knowledge Base

Knowledge Base 保存 Project 的 canonical documents。

第一阶段支持：

- Markdown；
- plain text；
- PDF；
- DOCX。

其中：

- Markdown / plain text 支持网页直接创建、查看、编辑；
- PDF / DOCX 支持上传、解析、索引、预览、下载，不支持网页直接编辑。

所有原始内容统一落入 Object Storage；KnowledgeDocument 只保存业务 metadata 和 stored_object_id。

Document 使用稳定 document_id 和递增 version。

详细 canonical model、CRUD、删除与 read contract 见 [Knowledge Document Domain](./knowledge-document-domain.md)。

## 4. Knowledge Indexing

Knowledge canonical content 与 retrieval index 分离。

~~~text
Canonical Document
    ↓
Format-specific Parser
    ↓
Structured Elements
    ↓
Section-aware Chunker
    ↓
Size Constraint
    ↓
Deterministic Context Prefix
    ↓
Lexical + Dense Index
~~~

索引是可重建派生数据，不是事实来源。

第一阶段采用：

- 结构感知 chunking；
- IndexProfile version；
- 异步 indexing job；
- document version 与 index version 显式一致性；
- parser / chunker adapter；
- deterministic contextual prefix；
- lexical + dense 两路索引。

详细设计见 [Knowledge Indexing](./knowledge-indexing.md)。

## 5. Retrieval Runtime

Knowledge query 和 Memory recall 共用底层 retrieval pipeline：

~~~text
Query
  ├── Lexical Retriever
  └── Dense Retriever
          ↓
         RRF
          ↓
   Optional Reranker
          ↓
     Ranked Results
~~~

基础原则：

- Dense retrieval 使用 pgvector 和平台 embedding_model_ref；
- Lexical Retriever 保持 backend abstraction；
- 第一阶段实现前通过中英混合项目文档 benchmark 选择 lexical backend；
- RRF 第一阶段 equal-weight；
- reranker 可选；
- chunk size、candidate count、top-k、RRF 权重等通过 Retrieval Eval 调优，不作为架构常量。

详细设计见 [Retrieval Runtime](./retrieval-runtime.md)。

## 6. Agent Memory

每个 Project Agent 拥有独立 namespace：

~~~text
project_id + agent_id
~~~

Memory 第一阶段类型：

- fact；
- experience；
- procedure；
- observation。

warning 等语义作为 tag / attribute，而不是独立 type。

Memory 保留：

- stable id；
- revision history；
- provenance；
- importance；
- active / superseded / deleted 等生命周期状态。

Agent 不能读取其他 Agent 的 Memory；需要跨 Agent 共享的信息应进入 Knowledge Base。

详细设计见 [Agent Memory Domain](./agent-memory-domain.md)。

## 7. Memory Runtime

Agent-facing Core Tools：

- retain：写入长期经验，并执行 extraction + consolidation；
- recall：检索当前 Agent namespace 中的相关 Memory；
- reflect：基于 Memory 做只读 reasoning，不自动产生写入副作用。

Memory 不在每次 Execution 结束时自动生成。

Agent system prompt 应要求：

- 遇到棘手、重复或疑似曾经踩过坑的问题时，优先 recall；
- 问题解决后，如果形成可长期复用的经验且此前没有记录，主动 retain；
- 不保存普通、一次性、低价值执行过程；
- 不把 Secret / credential 写入 Memory。

retain 的 Secret detection / masking 必须发生在任何 memory model、embedding 或索引调用之前。

详细设计见 [Agent Memory Runtime](./agent-memory-runtime.md)。

## 8. Platform Model Selection

Knowledge / Memory 使用平台级模型选择：

~~~text
embedding_model_ref   -> required System embedding Model
reranker_model_ref    -> optional System reranker Model
memory_model_ref      -> required System chat Model
~~~

其中：

- embedding_model_ref：Knowledge index 与 Memory retrieval embedding；
- reranker_model_ref：Knowledge query / Memory recall 的可选第二阶段 rerank；
- memory_model_ref：Memory extraction、consolidation、reflect reasoning。

Memory 不继承某个 Agent 当前 chat model。

embedding_model_ref 变化会触发 Knowledge / Memory 新索引 generation 的重建；已经在 serving 的 generation 在切换前仍使用自身记录的 embedding Model snapshot，避免 query vector 与旧索引进入不同 embedding space。

完整模型约束见 [Model System](../platform-infrastructure/model-system.md)。

## 9. Tool 与 Agent Loop 边界

Knowledge / Memory 通过 Unified Tool Runtime 接入 Agent Loop。

Core Agent Tools：

~~~text
query-doc
read-doc
recall
retain
reflect
~~~

其中 query-doc / read-doc / recall / retain / reflect 不能被普通 Agent Capability 关闭，但仍然受：

- Project scope；
- Agent namespace；
- Tool Authorization；
- Security / Governance；
- Audit。

其他 Knowledge 管理 Tool，即 list-docs / create-doc / update-doc / delete-doc，作为普通可配置 Builtin Tools。

Tool Result 进入 Canonical Transcript 后，继续遵守正常 context projection 和 compaction 规则。

## 10. 一致性原则

Knowledge：

- canonical Document 是事实来源；
- chunk / vector / lexical index 都是派生数据；
- Document 更新后，旧 document version 的索引不能继续 serving；
- 仅 IndexProfile 变化时，可以在后台重建期间继续由旧 profile serving，再原子切换。

Memory：

- active Memory revision 是 recall 的业务事实；
- embedding / lexical index 是可重建派生数据；
- consolidation 产生的 UPDATE / DELETE / NOOP 必须留下 revision / audit evidence；
- reflect 不改变 Memory Source of Truth。

任何删除或 supersede 都必须保证旧索引最终不可再检索。

## 11. 第一阶段明确不做

第一阶段不要求：

- 跨 Project Knowledge search；
- Agent 跨 namespace recall；
- Knowledge 完整历史版本系统；
- LLM semantic chunking 作为默认策略；
- HyDE；
- multi-query expansion；
- LLM query rewriting；
- 动态 query routing；
- 自动 weighted RRF；
- Memory 自动 TTL；
- Execution 结束自动生成 Memory；
- reflect 自动写入 Memory；
- 自动跨 Agent Memory 共享；
- Agent-facing delete-memory Tool；
- 自动生成高层 observation 的后台 workflow。

这些能力以后可以基于 Retrieval Eval 和真实使用需求扩展。

## 12. 架构原则

1. **Canonical 与 Index 分离**：长期事实不依赖向量索引存在。
2. **Knowledge / Memory 分域**：共享 retrieval infrastructure，不共享业务语义。
3. **按需访问**：不把 Knowledge / Memory 隐式塞入 Execution Context。
4. **Project / Agent scope 显式化**：权限和 namespace 在服务端强制执行。
5. **Index 可重建**：parser、chunker、embedding、lexical backend 变化通过 IndexProfile 重建处理。
6. **Retrieval 可评测**：参数通过离线 eval 调优，而不是固化经验值。
7. **Memory 可追溯**：长期经验必须有 provenance 和 revision history。
8. **Secret 前置过滤**：敏感数据在任何 LLM / embedding 处理之前被拒绝或掩码。
9. **Read / Write 语义清晰**：recall / reflect 是读取；retain 才产生长期 Memory mutation。
10. **正式共享走 Knowledge**：Agent Memory 不发展成第二套 Project Knowledge Base。
