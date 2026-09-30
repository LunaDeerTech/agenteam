# Agent Memory Domain 详细设计

> 上层架构：[Knowledge Base 与 Agent Memory](./README.md)  
> 相关设计：[Agent Memory Runtime](./agent-memory-runtime.md)、[Retrieval Runtime](./retrieval-runtime.md)、[Agent Management](../agent-management.md)

## 1. 目标与边界

Agent Memory Domain 负责单个 Project Agent 的长期经验数据模型与生命周期。

它负责：

- Memory namespace；
- stable Memory identity；
- Memory type；
- active revision；
- revision history；
- provenance；
- importance；
- tags；
- supersede / delete；
- recall eligibility；
- 用户侧管理。

它不负责：

- retain LLM extraction；
- consolidation decision；
- reflect reasoning；
- hybrid retrieval pipeline；
- Agent system prompt；
- Model Provider 调用。

这些运行流程见 [Agent Memory Runtime](./agent-memory-runtime.md)。

## 2. Namespace

Memory namespace 固定：

~~~text
project_id + agent_id
~~~

含义：

- 同一个 Project Agent 跨 Execution 共享长期 Memory；
- 不同 Agent 即使在同一 Project，也不共享 Memory；
- 同一 Agent identity 如果只存在于一个 Project，也仍显式保留 project_id；
- execution_id 只作为 provenance，不参与 namespace。

服务端必须把 namespace 作为 authorization boundary，而不是只作为 query filter。

## 3. 与 Knowledge Base 的边界

Agent Memory 不承担 Project 共同知识职责。

适合 Memory：

- “这个仓库在执行 migration 前要先生成 schema”；
- “上次 Runner 在某类目录失败是因为权限继承”；
- “处理这个模块时先跑 X，再跑 Y”；
- “某种错误通常意味着 Z”。

适合 Knowledge：

- 正式技术架构；
- PRD；
- API 说明；
- 团队决策；
- 跨 Agent 共享的稳定操作文档。

如果某条经验需要成为 Project 的正式共同知识，应通过 Knowledge Tool 创建 / 更新 KnowledgeDocument，而不是开放跨 Agent recall。

## 4. Core Model

建议分为稳定实体与 revision：

~~~text
Memory
├── id
├── project_id
├── agent_id
├── current_revision
├── status
├── created_at
├── updated_at
└── deleted_at?

MemoryRevision
├── memory_id
├── revision
├── type
├── text
├── tags
├── importance
├── provenance
├── derived_from_memory_ids?
├── created_by_operation_id
├── created_at
└── status
~~~

这样：

- Memory.id 稳定；
- 内容变化形成 revision；
- recall 只读取 active current revision；
- 历史 revision 保留审计与解释能力。

## 5. Memory Status

Memory 第一阶段：

~~~text
active
deleted
~~~

MemoryRevision：

~~~text
active
superseded
deleted
~~~

一个 active Memory 只能有一个 active revision。

当 UPDATE：

~~~text
revision n     -> superseded
revision n + 1 -> active
Memory.current_revision = n + 1
~~~

当 DELETE：

~~~text
current revision -> deleted
Memory.status    -> deleted
~~~

历史 revision 不物理删除。

## 6. Memory Type

第一阶段固定：

~~~text
fact
experience
procedure
observation
~~~

### 6.1 fact

长期认为成立的 Project 事实。

例如：

~~~text
该项目所有数据库 migration 都位于 server/migrations。
~~~

Memory fact 不等于 Knowledge canonical truth。

如果正式文档与 Memory fact 冲突，Agent 应优先核查当前项目事实，并通过 retain consolidation 更新过时 Memory。

### 6.2 experience

一次具体问题、踩坑、故障或解决经历。

例如：

~~~text
在 Runner 上启动 Playwright 时曾因缺少 DISPLAY 失败，改用 Mac desktop runner 后解决。
~~~

experience 应保留足够 provenance。

### 6.3 procedure

可复用的方法或顺序。

例如：

~~~text
修改 Runner protocol 后应先更新 protocol doc，再运行 compatibility tests。
~~~

### 6.4 observation

从多条 Memory 或长期工作中归纳出的高层观察。

第一阶段 observation 可以由 Agent 显式 retain 产生。

未来如果增加自动 Observation Consolidator，也必须保留 derived_from_memory_ids。

## 7. Tags

tags 用于表达与 type 不同维度的属性。

例如：

~~~text
warning
performance
migration
frontend
runner
~~~

warning 不作为 type。

第一阶段 tags：

- 可以由 memory extraction 产生；
- 用于 filter / display；
- 不建立复杂 taxonomy；
- 不直接决定 authorization。

避免把 tags 发展成第二套 arbitrary ontology。

## 8. Importance

Memory 保留 importance 作为 retrieval 辅助信号。

为了避免 LLM 生成伪精确小数，第一阶段建议使用小型 ordinal：

~~~text
low
normal
high
~~~

语义：

### low

有一定复用价值，但不应明显影响一般 recall。

### normal

普通长期经验，默认值。

### high

对避免重大重复错误、关键操作顺序或高价值长期事实非常重要。

importance：

- 不是 confidence；
- 不是 correctness probability；
- 不应压过明显更相关的 retrieval result；
- 可以在 recall ranking 中作为轻量辅助。

## 9. 不保存 Confidence

第一阶段不保存：

~~~text
confidence = 0.83
~~~

原因：

- Memory 本身来自 Agent / LLM 归纳；
- 小数 confidence 容易制造虚假精确度；
- provenance 比自评概率更能说明来源。

如果未来需要可信度，应基于：

- source type；
- source freshness；
- corroborating evidence；
- user confirmation；

另行设计，不复用 importance。

## 10. Provenance

MemoryRevision 必须可追溯来源。

概念：

~~~text
MemoryProvenance[]
├── kind
├── reference_id
├── project_id
├── metadata?
└── captured_at
~~~

kind 第一阶段可包括：

~~~text
execution
task
meeting
tool_operation
knowledge_document
memory
manual
~~~

### 10.1 execution

记录 source execution_id。

### 10.2 task

记录 task_id。

### 10.3 meeting

记录 meeting_id，必要时加 message_id。

### 10.4 tool_operation

记录具体 Tool Operation identity。

### 10.5 knowledge_document

如果 Agent 基于正式 Knowledge 形成经验，可以记录 document_id + observed version。

这不把 Knowledge 内容复制进 Memory provenance。

### 10.6 memory

用于 derived observation。

## 11. Provenance 不是强引用删除约束

历史 source 被删除，不要求 cascade 删除 Memory。

例如 Task 被删除或 Meeting 归档：

- Memory 仍可存在；
- provenance UI 显示 source unavailable / deleted。

但如果 source 删除属于敏感数据删除流程，Security / Governance 可以触发 Memory scrub / delete。

普通业务删除与隐私 / Secret 删除要区分。

## 12. Derived Memory

observation 可以记录：

~~~text
derived_from_memory_ids[]
~~~

第一阶段只在显式 retain / future workflow 能提供来源时使用。

它的作用：

- 解释高层 observation 从哪里来；
- 防止“归纳结果看起来像原始事实”；
- 未来删除 / supersede source 时可以做 consistency review。

第一阶段不自动级联删除 derived observation。

## 13. Revision

Memory UPDATE 不原地覆盖 text。

例如：

~~~text
Memory #42
revision 1: old procedure
revision 2: corrected procedure
revision 3: more precise procedure
~~~

recall 只看到 revision 3。

用户管理 UI 可以查看 revision history。

Agent-facing recall 默认不返回完整历史。

## 14. Consolidation Actions 与 Domain Mutation

Memory Domain 暴露内部 mutation contract：

~~~text
ADD
UPDATE
DELETE
NOOP
~~~

### ADD

创建新 Memory + revision 1。

### UPDATE

对已有 Memory 创建新 revision。

### DELETE

把 Memory 标为 deleted，并让 current revision 失去 recall eligibility。

### NOOP

不修改 Domain。

Decision 由 Memory Runtime 产生，Domain Service 负责：

- 校验 namespace；
- 校验 expected current revision；
- transaction；
- revision history；
- index update intent。

LLM 不能直接写数据库。

## 15. Concurrency

Memory consolidation 可能同时发生。

例如同一个 Agent 并行 Execution：

~~~text
Execution A retain
Execution B retain
~~~

都可能命中同一 existing Memory。

UPDATE / DELETE 必须携带：

~~~text
expected_revision
~~~

如果 current revision 已变化：

- mutation conflict；
- Memory Runtime 重新读取相关 Memory；
- 重新执行 consolidation 或返回可恢复 Tool error。

不允许 silent last-write-wins。

## 16. Retain Operation Evidence

Memory revision 记录：

~~~text
created_by_operation_id
~~~

用于关联一次 retain operation。

单次 retain 可以产生：

~~~text
0..N Memory mutation
~~~

因此：

~~~text
RetainOperation
    ├── ADD Memory A
    ├── UPDATE Memory B
    └── NOOP Candidate C
~~~

Memory Domain 不要求把 raw retain Tool input 永久复制到每条 Memory。

原始 Execution / Transcript 已经是运行事实来源。

## 17. Recall Eligibility

可 recall 必须满足：

~~~text
Memory.project_id = current project
Memory.agent_id = current agent
Memory.status = active
Revision = current active revision
index generation ready
~~~

历史 / superseded / deleted revision 永不进入默认 recall。

## 18. Memory Index Projection

Memory canonical Source of Truth 是：

~~~text
Memory
+
active MemoryRevision
~~~

派生：

- lexical index；
- embedding vector；
- retrieval features。

Memory text / type / tags / importance / current revision 变化后，都需要更新 retrieval projection。

索引失败不能删除 canonical Memory，但该 revision 在索引修复前可能无法正常 hybrid recall。

具体 retrieval 见 [Retrieval Runtime](./retrieval-runtime.md)。

## 19. Memory Embedding Identity

Memory 不使用 Knowledge 的 Document IndexProfile identity。

但必须记录自己的 index generation / embedding profile，使 embedding model 切换时能够重建。

至少能确定：

~~~text
memory_id
revision
embedding model snapshot
retrieval schema version
~~~

Knowledge / Memory 可以共用 embedding infrastructure，但不共用 chunk 表或业务表。

## 20. Delete

Agent 不提供 delete-memory Tool。

删除入口：

- retain consolidation 的 DELETE；
- 用户 Memory 管理 UI/API；
- Security / Governance 强制删除流程。

删除后：

1. Memory.status = deleted；
2. current revision = deleted；
3. 立即停止 recall eligibility；
4. 异步清理 lexical / dense index；
5. 保留 revision history / audit evidence，除非安全删除规则要求物理清除。

## 21. Supersede 与 Delete 的区别

UPDATE：

~~~text
old revision superseded
Memory remains active
new revision active
~~~

DELETE：

~~~text
Memory no longer active
no active revision for recall
~~~

不要用：

~~~text
UPDATE text = ""
~~~

模拟删除。

## 22. 用户侧 Memory 管理

用户可以查看当前 Agent 的 Memory：

至少展示：

- text；
- type；
- tags；
- importance；
- updated_at；
- provenance summary。

用户可以：

- 删除 Memory；
- 查看 revision history。

第一阶段不要求用户直接编辑 Memory text。

如果需要修改，建议：

- 删除错误 Memory；
- 或未来提供 explicit correction workflow。

避免 UI 编辑绕过 consolidation / provenance 语义。

## 23. Agent Access

Agent-facing：

~~~text
recall
retain
reflect
~~~

Agent 不获得：

- list all memories 无限制 dump；
- delete-memory；
- read another agent memory；
- arbitrary namespace selector。

recall / reflect 自动绑定当前 Execution 的：

~~~text
project_id
agent_id
~~~

Tool arguments 不接受 Agent 自己指定 project_id / agent_id 来改变 scope。

## 24. Knowledge Promotion

第一阶段不提供自动：

~~~text
Memory -> Knowledge
~~~

Agent 如果判断某条经验应该成为正式共享知识，应明确调用 Knowledge create-doc / update-doc。

这样：

- 用户能看到正式 Knowledge mutation；
- Tool Authorization 正常生效；
- 不出现 Memory 自动泄露给其他 Agent。

## 25. Secret / Sensitive Content

Memory Domain 假设写入的数据已经经过 Memory Runtime 的前置 Secret detection / masking。

Domain 层仍应：

- 拒绝明确标记为 secret-bearing 的 mutation；
- 不把 Secret metadata 写入 tags / provenance details；
- 遵守 Security / Governance 删除要求。

完整顺序见 [Agent Memory Runtime](./agent-memory-runtime.md)。

## 26. 生命周期示例

### ADD

~~~text
retain
-> candidate lesson
-> no related active Memory
-> ADD
-> Memory #7 revision 1 active
~~~

### UPDATE

~~~text
retain
-> related Memory #7 found
-> old procedure outdated
-> UPDATE expected_revision=1
-> revision 1 superseded
-> revision 2 active
~~~

### DELETE

~~~text
retain
-> new evidence proves Memory #7 should no longer be kept
-> DELETE expected_revision=2
-> Memory #7 deleted
~~~

### NOOP

~~~text
retain
-> existing Memory already expresses same lesson
-> NOOP
~~~

## 27. 第一阶段不做

- Agent 跨 namespace recall；
- shared team Memory；
- automatic TTL；
- confidence score；
- graph memory；
- causal relation graph；
- arbitrary user taxonomy；
- auto Memory -> Knowledge promotion；
- Agent-facing delete-memory；
- background automatic observation generator；
- full Memory merge graph；
- semantic conflict resolution outside retain consolidation。

## 28. 关键不变量

1. namespace 永远是 project_id + agent_id。
2. 一个 active Memory 只有一个 active revision。
3. recall 只检索 current active revision。
4. UPDATE 创建新 revision，不原地覆盖。
5. DELETE 立即取消 recall eligibility。
6. Memory type 固定为 fact / experience / procedure / observation。
7. warning 是 tag，不是 type。
8. importance 不是 confidence。
9. provenance 必须可追溯。
10. Agent 不能读取其他 Agent Memory。
11. Agent 不直接删除 Memory。
12. Memory 与 Knowledge 不共享业务表或 authorization。
