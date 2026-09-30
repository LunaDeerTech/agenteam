# Agent Memory Runtime 详细设计

> 上层架构：[Knowledge Base 与 Agent Memory](./README.md)  
> 相关设计：[Agent Memory Domain](./agent-memory-domain.md)、[Retrieval Runtime](./retrieval-runtime.md)、[Model System](../platform-infrastructure/model-system.md)、[Agent Loop](../agent-loop/README.md)、[Security / Governance](../security-governance/README.md)

## 1. 目标与边界

Agent Memory Runtime 负责 Agent-facing Memory Tools 的运行语义。

第一阶段 Core Agent Tools：

~~~text
retain
recall
reflect
~~~

Runtime 负责：

- Tool input validation；
- namespace binding；
- Secret / sensitive-data 前置处理；
- memory_model_ref 解析；
- retain extraction；
- related Memory retrieval；
- consolidation；
- structured output；
- Memory Domain mutation；
- recall adapter；
- reflect read-only reasoning；
- failure handling；
- observability。

它不拥有 Memory canonical schema；schema 与 revision 生命周期见 [Agent Memory Domain](./agent-memory-domain.md)。

## 2. Core Tool 语义

### retain

~~~text
write
~~~

把 Agent 提交的候选经验转化为 0..N 条规范化 Memory，并与已有 Memory consolidation。

### recall

~~~text
read
~~~

根据 query 检索当前 Agent namespace 的长期 Memory。

### reflect

~~~text
read + reason
~~~

基于 recall 的 Memory 结果使用 memory_model_ref 做更高层归纳 / reasoning，但不写 Memory。

只有 retain 产生长期 Memory mutation。

## 3. Namespace Binding

Tool arguments 不接受自由 project_id / agent_id。

Runtime 从当前 Execution identity 绑定：

~~~text
execution
  -> project_id
  -> agent_id
  -> MemoryNamespace
~~~

因此 Model 无法通过构造 arguments 切换 namespace。

所有内部 retrieval / mutation 都携带服务端生成的 namespace。

## 4. Agent System Prompt Guidance

平台 system prompt 必须告诉 Agent 何时使用 Memory。

核心规则：

~~~text
1. 遇到棘手、重复、疑似以前踩过坑的问题：
   优先 recall。

2. 如果 recall 没有有用记录，并且最终解决了具有长期复用价值的问题：
   retain 关键原因、踩坑过程和解决方法。

3. 不要 retain：
   - 普通一次性进度；
   - 很快过期的临时状态；
   - 可以从 canonical Project 数据直接读取的普通事实副本；
   - Secret / credential；
   - 无明显长期价值的聊天内容。

4. reflect 用于对已有长期经验做综合推理，
   不把 reflect 当成写 Memory 的方式。
~~~

这属于平台行为规范，不依赖某个 Agent 自己是否在 instructions 中重复说明。

## 5. 不自动生成 Memory

Agent Execution terminal 时不自动：

- summarize Execution；
- retain；
- reflect；
- 创建 observation。

原因：

- 大量普通 Execution 会产生低价值 Memory；
- 自动 summary 很容易把临时事实固化成长期经验；
- Agent 在真正解决问题的时刻最清楚什么值得记录。

长期 Memory 由 Agent 主动 retain。

## 6. Platform Model Selection

新增：

~~~text
memory_model_ref
~~~

约束：

- System chat Model；
- enabled；
- 平台级，不按 Project 配置；
- 不继承当前 Agent.chat model；
- 必须能够可靠满足 Memory structured output contract。

用途：

- retain extraction；
- retain consolidation；
- reflect reasoning。

embedding / reranker 仍分别使用：

~~~text
embedding_model_ref
reranker_model_ref?
~~~

## 7. memory_model_ref Resolution

每次 Memory model invocation 都先解析当前 PlatformModelSelection。

Memory Runtime 不创建 AgentExecution。

它作为普通平台内部 model consumer：

~~~text
Memory Runtime
-> Model Resolver
-> Resolved Model
-> Unified Chat Model Contract
~~~

必须记录 model invocation / usage evidence，但不把 Memory 内部调用伪造成 Agent Loop Turn。

## 8. Secret / Sensitive Data 前置处理

任何 retain 内容在进入：

- memory_model_ref；
- embedding model；
- lexical index；

之前，必须先经过 sensitive-data gate。

顺序固定：

~~~mermaid
flowchart LR
    Input["retain Tool Input"]
    Detect["Secret / Sensitive Data Detection"]
    Decision{"Safe?"}
    Reject["Reject"]
    Mask["Mask / Sanitize"]
    Extract["Memory Model Extraction"]
    Retrieve["Related Memory Retrieval"]
    Consolidate["Consolidation"]
    Persist["Memory Domain"]
    Index["Retrieval Index"]

    Input --> Detect
    Detect --> Decision
    Decision -->|"unsafe / cannot sanitize"| Reject
    Decision -->|"safe"| Extract
    Decision -->|"maskable"| Mask
    Mask --> Extract
    Extract --> Retrieve
    Retrieve --> Consolidate
    Consolidate --> Persist
    Persist --> Index
~~~

不能：

~~~text
raw input
-> LLM
-> detect secret afterward
~~~

因为这已经把 Secret 发送给 Model Provider。

## 9. Sensitive Data Decision

Gate 可以返回：

~~~text
allow
mask
reject
~~~

### allow

输入没有识别到需要阻止的敏感内容。

### mask

可以安全删除 / 替换具体值，同时保留经验语义。

例如：

~~~text
不要把 DATABASE_PASSWORD 写入日志。
~~~

可以保留概念，但不能保留密码值。

### reject

如果去掉敏感值后内容已失去合理语义，retain Tool 返回明确错误，要求 Agent 改写成不包含 Secret 的经验。

## 10. Raw retain Input

第一阶段不需要把 raw retain payload 再复制成一份长期 Memory Source of Truth。

运行事实已经存在于：

- Canonical Transcript；
- Tool Operation；
- Task / Meeting；
- Execution evidence。

Memory Runtime 只保存：

- sanitized input 的必要 operation evidence；
- extraction / consolidation result；
- mutation correlation。

普通日志不记录完整 raw retain text。

## 11. retain Input

Agent-facing contract 概念：

~~~text
retain
├── content
├── source_context?
└── intent?
~~~

其中 namespace、execution_id、agent_id、project_id 由服务端注入。

source_context 只是 Agent 提供的辅助线索，不能取代服务端 provenance。

Runtime 从 Execution 自动补充真实 provenance。

## 12. retain 总流程

~~~mermaid
sequenceDiagram
    participant A as Agent
    participant T as retain Tool
    participant S as Sensitive Data Gate
    participant M as Memory Model
    participant R as Retrieval Runtime
    participant D as Memory Domain

    A->>T: retain(content)
    T->>S: inspect / sanitize
    S-->>T: sanitized content
    T->>M: extract candidates
    M-->>T: 0..N candidates

    loop each candidate / related set
        T->>R: retrieve related memories
        R-->>T: active memories
    end

    T->>M: consolidate candidates + related memories
    M-->>T: structured actions
    T->>D: apply validated mutation batch
    D-->>T: resulting memories
    T-->>A: retained / updated / no-op summary
~~~

## 13. Extraction

Extraction 把候选内容转成 0..N 独立 Memory candidates。

输出必须使用 schema-constrained structured output。

概念 schema：

~~~text
MemoryExtraction
└── candidates[]
    ├── type
    ├── text
    ├── tags[]
    ├── importance
    └── source_hint?
~~~

允许：

~~~text
candidates = []
~~~

表示输入不值得形成长期 Memory。

## 14. Extraction Prompt 原则

Memory Model 应被要求：

- 提取长期可复用信息；
- 不复制普通执行过程；
- 不臆造 source 中不存在的信息；
- 一条 Memory 表达一个可独立 recall 的经验；
- experience 保留问题与解决关键点；
- procedure 保留可复用步骤；
- observation 只在输入本身已经形成明确归纳时产生；
- 不生成 Secret；
- 不生成 confidence。

## 15. Candidate Size

Memory candidate 应保持短而完整。

它不是：

- 整份任务总结；
- 全量 Transcript；
- 整个文档副本。

如果一个候选包含多个独立 lesson，Extraction 拆成多个 candidates。

具体长度不是永久架构常量，但应设置合理 Tool / Model output guard。

## 16. Related Memory Retrieval

每个 candidate 在 consolidation 前检索相关 active Memory。

~~~text
candidate text
  -> Memory Retrieval Adapter
  -> related active memories
~~~

这里使用与 recall 相同的 hybrid Retrieval Runtime。

但 operation mode 是：

~~~text
consolidation lookup
~~~

可以使用不同的 candidate_k / threshold 配置。

仍然只能访问当前 namespace。

## 17. Consolidation

Consolidation 输入：

~~~text
candidate
+
related active memories
+
their current revisions
+
provenance context
~~~

输出：

~~~text
ADD
UPDATE
DELETE
NOOP
~~~

必须是 structured output。

### 17.1 ADD

candidate 是新的长期 Memory。

### 17.2 UPDATE

candidate 应修改某个 existing Memory。

必须返回：

~~~text
target_memory_id
expected_revision
new Memory content
~~~

### 17.3 DELETE

新信息明确表明某条 existing Memory 已不应保留。

DELETE 必须返回：

~~~text
target_memory_id
expected_revision
reason
~~~

### 17.4 NOOP

已有 Memory 已经完整表达内容，或者 candidate 不值得保存。

## 18. Consolidation 安全约束

Memory Model 只能对 Runtime 提供的 related Memory 做 UPDATE / DELETE。

不能输出任意 memory_id 并操作未读取内容。

服务端验证：

- target memory 属于当前 namespace；
- target 在 consolidation input set；
- expected revision 一致；
- action schema 有效。

LLM output 永远只是 mutation proposal。

## 19. Batch Mutation

一次 retain 可能产生多个 actions。

推荐：

~~~text
validate all actions
    ↓
Memory Domain transaction
    ↓
apply ADD / UPDATE / DELETE
    ↓
commit
~~~

如果任一 UPDATE / DELETE 发生 revision conflict：

- 整个 mutation batch 不部分提交；
- 重新读取相关 active Memory；
- consolidation 可以安全重试一次或按 Tool retry policy处理。

这样 Agent 不会收到“retain 部分成功、部分未知”的混乱语义。

## 20. Retain Result

Agent-facing 返回应简洁：

~~~text
RetainResult
├── added[]
├── updated[]
├── deleted_count
├── noop_count
└── summary?
~~~

不需要把内部：

- embedding vector；
- lexical scores；
- consolidation prompt；
- model chain-of-thought；

返回给 Agent。

## 21. recall

Agent-facing：

~~~text
recall(query, filters?, limit?)
~~~

Runtime：

1. bind namespace；
2. validate query；
3. call Memory Retrieval Adapter；
4. return ranked active Memory。

recall 不调用 memory_model_ref 做二次总结。

因此 recall：

> 检索事实 / 经验本身。

如果 Agent 需要综合 reasoning，可以自己使用返回内容推理，或调用 reflect。

## 22. recall Result

概念：

~~~text
RecallResult
└── memories[]
    ├── memory_id
    ├── type
    ├── text
    ├── tags?
    ├── importance?
    ├── provenance summary?
    └── updated_at
~~~

不返回：

- historical revisions；
- embedding；
- internal retrieval score breakdown。

必要时可以返回 coarse relevance metadata，但不是 contract 必需。

## 23. reflect

Agent-facing：

~~~text
reflect(query)
~~~

语义：

> 在当前 Agent Memory namespace 上先 recall，再用 memory_model_ref 对相关 Memory 做只读综合推理。

流程：

~~~mermaid
flowchart LR
    Query["reflect(query)"]
    Recall["Memory Recall"]
    Context["Ranked Memories"]
    Model["memory_model_ref"]
    Answer["Reasoned Answer"]

    Query --> Recall
    Recall --> Context
    Context --> Model
    Model --> Answer
~~~

reflect：

- 不 ADD；
- 不 UPDATE；
- 不 DELETE；
- 不自动 retain；
- 不生成隐藏 observation。

## 24. reflect Output

返回：

~~~text
ReflectResult
├── answer
└── memory_refs[]
~~~

memory_refs 可以帮助 Agent 理解回答基于哪些长期 Memory。

如果 reasoning 得出值得长期保存的新 insight：

~~~text
Agent
-> retain(...)
~~~

写入动作保持显式。

## 25. Future Observation Workflow

未来如果需要自动生成 observation：

~~~text
Observation Consolidator
-> selected memories
-> derived observation
-> derived_from_memory_ids
-> explicit Memory mutation
~~~

它应是独立 workflow。

不能通过改变 reflect 的既有语义偷偷增加写副作用。

## 26. Memory Model Structured Output

Extraction 和 Consolidation 必须使用统一 Model System 的 structured output capability。

如果所选 memory_model_ref 无法可靠提供 schema-constrained output：

- 配置阶段应阻止选择；
- 或 Runtime 启动 invocation 前返回 model capability error。

不使用：

~~~text
free-form text
-> regex parse ADD / UPDATE / DELETE
~~~

## 27. reflect Model Output

reflect 本身可以输出普通 text answer。

它不需要 mutation schema。

但仍使用：

- Unified Chat Model Contract；
- usage tracking；
- provider error normalization；
- model invocation evidence。

## 28. Error Handling

### Sensitive data rejected

retain Tool 返回结构化安全错误。

Agent 可以移除 Secret 值后重新 retain。

### memory_model_ref unavailable

retain / reflect Tool error。

recall 不依赖 memory_model_ref，可以继续工作。

### embedding unavailable

recall / consolidation lookup 按 Retrieval Runtime degradation policy 处理。

### consolidation schema invalid

Memory Model invocation 可以按 Model System 的技术 retry / structured-output recovery policy重试。

最终仍 invalid：

- retain failed；
- 不提交 mutation。

### revision conflict

不 silent overwrite。

重新读取后安全重试，或返回 conflict。

## 29. 与 Agent Execution 的失败边界

retain / recall / reflect 都是普通 Tool Call。

Tool 失败：

- 进入结构化 Tool Result；
- Agent Loop 可以自行修正 / retry；
- 不因为 Memory Tool 单次失败直接把整个 Execution 标记 failed，除非 Agent Loop 最终无法继续。

Memory indexing / embedding 的后台修复也不反向修改已结束 Execution。

## 30. Tool Authorization

三者是 Core Agent Tools：

~~~text
retain
recall
reflect
~~~

含义：

- 不受普通 Agent Capability 开关移除；
- Execution Tool Set 中始终包含定义；
- 仍受服务端 scope；
- 仍经过 Unified Tool Runtime；
- 仍经过 Security / Governance；
- 仍产生 Tool Operation / Audit evidence。

Core 不等于 unrestricted。

## 31. Approval / Destructive

recall：

~~~text
read-only
~~~

reflect：

~~~text
read-only
~~~

retain：

~~~text
write
~~~

retain 内部 consolidation 可能提出 DELETE，但这是 retain domain mutation 的一部分，不暴露独立 delete-memory Tool。

Security / Governance 应按 Memory retain Tool 的既定 action classification 处理，而不是把 LLM 内部每个 consolidation action重新变成 Agent-facing Tool Call。

用户直接删除 Memory 则走用户侧 Domain API 与 Audit。

## 32. Audit

至少审计：

### retain

- execution / agent / project；
- sensitive gate outcome；
- memory model identity；
- candidate count；
- ADD / UPDATE / DELETE / NOOP count；
- affected Memory ids；
- operation result。

### recall

- namespace identity；
- query metadata / hash；
- result count；
- retrieval mode；
- failure / degradation。

### reflect

- memory model identity；
- referenced Memory ids；
- invocation result；
- usage。

默认 Audit 不保存 Secret-bearing raw input。

## 33. Observability

指标至少：

- retain latency；
- extraction latency；
- related retrieval latency；
- consolidation latency；
- candidate count；
- mutation count；
- NOOP rate；
- revision conflict rate；
- sensitive reject / mask rate；
- recall latency；
- recall degradation rate；
- reflect latency；
- memory model usage。

高 NOOP rate 可能说明 Agent 过度 retain。

高 ADD rate + 低 recall quality 可能说明 consolidation 太保守。

## 34. Memory Index Update

Domain mutation commit 后：

~~~text
ADD / UPDATE / DELETE
    ↓
Memory retrieval projection update
~~~

ADD：

- index active revision。

UPDATE：

- old revision 立即失去 eligibility；
- index new revision。

DELETE：

- Memory 立即失去 eligibility；
- async cleanup vector / lexical entry。

不能让 superseded revision 长期被 recall。

## 35. Embedding Model Change

Knowledge IndexProfile 负责 Knowledge rebuild。

Memory 也必须对 embedding model change 做自己的 rebuild generation。

平台切换 embedding_model_ref 时，需要协调：

~~~text
Knowledge rebuild
+
Memory rebuild
~~~

在 Memory 新 generation ready 前，可以继续使用旧 embedding generation，只要 canonical active revisions 没变。

这段期间 recall 的 query embedding 必须继续使用旧 serving generation 记录的 embedding Model snapshot，而不是直接使用已经切换后的 PlatformModelSelection.embedding_model_ref。

新 generation 完整 ready 并原子切换后，query embedding 与 Memory dense index 才一起进入新的 embedding space。

如果某条 Memory revision 本身 UPDATE：

- 旧 revision 立即不能 serving；
- 与 Knowledge Document content update 规则类似。

## 36. 第一阶段不做

- Execution terminal 自动 retain；
- background automatic observation generator；
- reflect 自动写入；
- Agent-facing delete-memory；
- cross-Agent recall；
- global user memory；
- Memory chat / conversation mode；
- automatic TTL；
- LLM-generated confidence；
- arbitrary memory graph；
- self-tuning consolidation prompt。

## 37. 关键不变量

1. 只有 retain 产生 Agent-facing Memory write。
2. recall / reflect 都是 read-only。
3. namespace 由 Execution 服务端绑定，Tool arguments 不能切换。
4. Secret detection 在任何 Memory Model / embedding 调用之前。
5. raw retain input 不复制成第二份长期事实库。
6. extraction 可以产生 0..N candidates。
7. consolidation 只允许操作提供给 Model 的 related Memories。
8. UPDATE / DELETE 必须校验 expected revision。
9. retain batch 不部分提交。
10. memory_model_ref 是平台级 System chat Model，不继承 Agent model。
11. structured mutation 不用自由文本 regex 解析。
12. Execution 结束不自动生成 Memory。
