# Transcript 与 Model Context 详细设计

> 上级架构：[Agent Loop](./README.md)
>
> 相关设计：
> - [Agent Loop Runtime](./loop-runtime.md)
> - [Context Window 与 Compaction](./context-compaction.md)
> - [Execution Context](../agent-executor/execution-context.md)
> - [Runtime View](../agent-executor/runtime-view.md)
> - [Model System](../platform-infrastructure/model-system/README.md)
> - [Tool Result & Backend](../tool-system/tool-result-backend.md)

## 1. 目标

本文定义 Agent Loop 中三层不同对象：

1. Immutable AgentExecutionContext；
2. Canonical Transcript；
3. Per-turn Model Context Projection。

核心原则：

~~~text
Execution started with what
!=
What happened during execution
!=
What the current Model Request needs to see
~~~

这三个边界必须保持独立。

## 2. 三层 Context

~~~text
Immutable AgentExecutionContext
        |
        | startup snapshot
        v
Fixed Context Components
        |
        +-------------------------+
                                  |
Canonical Transcript             |
        |                         |
        | complete semantic       |
        | runtime history         |
        v                         v
      Model Context Projector
                |
                v
      Model Context Projection
                |
                v
      Unified Model Messages
                |
                +
      Final System Prompt
      Execution Tool Set
                |
                v
      Unified Chat Model Request
~~~

AgentExecutionContext 是启动事实。

Canonical Transcript 是完整运行语义历史。

Model Context Projection 是本轮请求视图。

## 3. AgentExecutionContext 不属于 Transcript

AgentExecutionContext 中已经固化：

- Platform Prompt component；
- Agent instructions；
- Trigger Context；
- AGENTS.md Snapshot；
- Project Environment Variables metadata / allowed values；
- Runner / mount metadata；
- resolved Model Snapshot；
- Execution Tool Set Snapshot；
- Execution Policy；
- 初始 Skill 固定版本绑定与目录 metadata；
- 其他启动 metadata。

这些内容不需要在 Transcript 中再复制一遍。

Final System Prompt 由这些 immutable components 组装，但仍属于 request fixed context，而不是 Transcript history。

运行中新增技能采用独立持久化绑定与 control 事实，不改启动 Context；本轮有效目录由初始绑定与已应用变更重建，完整说明仍通过已有工具按需加载。

## 4. Canonical Transcript 定义

Canonical Transcript 是：

> 该 Agent Execution 中，为保持后续模型连续性和安全恢复而需要完整保存的稳定语义历史。

它应该：

- 完整；
- 按稳定顺序追加；
- 能够表达 Turn；
- 能够表达 tool-call/result pairing；
- 能够表达 typed control input；
- 保留必要 provider continuation metadata；
- 不因 compaction 删除历史；
- 能够被 checkpoint / recovery 引用。

它不要求保存所有底层执行细节。

## 5. TranscriptEntry

概念结构：

~~~text
TranscriptEntry
├── entry_id
├── execution_id
├── turn_id?
├── sequence
├── kind
│   ├── input
│   ├── control
│   ├── assistant
│   └── tool_result
├── content[]
├── tool_call_id?
├── source
├── metadata
└── created_at
~~~

其中 content 尽量复用 Model System 已经定义的 Unified Message content parts：

~~~text
text
image
file/reference
tool_call
tool_result
provider_metadata
~~~

Agent Loop 不再定义第二套平行 content schema。

## 6. entry_id 与 sequence

每个 TranscriptEntry 有稳定 entry_id。

同一 Execution 内通过 sequence 定义全序。

要求：

~~~text
execution_id + sequence
~~~

唯一。

sequence 用于：

- 稳定 ordering；
- compaction boundary；
- checkpoint position；
- recovery；
- debug correlation。

sequence 不承担 Runtime View seq 的职责。

Transcript sequence、Runtime Item 创建 seq 与单 Execution 的 Runtime 更新进度各有用途，不能用条目创建序号判定 stream 更新已包含在快照中。

## 7. turn_id

Model 驱动的 Assistant / Tool Result entries 应关联 turn_id。

典型：

~~~text
Turn 12
├── AssistantEntry(turn_id = 12)
│   └── tool_call A
├── ToolResultEntry(turn_id = 12, tool_call_id = A)
├── ToolResultEntry(turn_id = 12, tool_call_id = B)
└── stable boundary
~~~

input / control entry 可以没有 turn_id，或关联它将要影响的 next turn。

turn_id 是 Agent Loop 内部 correlation identity，不成为业务对象 ID。

## 8. input entry

input 表示本次 Agent Loop 初始 model-visible 输入。

它不是 AgentExecutionContext 的完整复制。

例如 Task trigger 可以形成：

~~~text
input
└── prepared task work context
~~~

Meeting trigger 可以形成：

~~~text
input
└── prepared meeting turn context
~~~

具体业务数据已经由 Trigger Context Provider 准备。

Transcript 只保存本次 Loop 实际采用的初始语义输入或其稳定引用。

## 9. control entry

control 用于运行期间由可信平台组件注入的 typed control input。

第一阶段至少包括：

~~~text
approval_result
decision_result
watchdog_notice
resume_notice
recovery_notice
loop_warning
skill_binding_added
~~~

control 不是任意 user message queue。

概念：

~~~text
ControlTranscriptEntry
├── type
├── reference?
├── payload
├── source
└── created_at
~~~

source 必须能区分：

- Agent Executor；
- Approval subsystem；
- Meeting Decision；
- Agent Loop internal guard。

Skill control 来自 Executor 验证过的正式分配/绑定端口，记录固定 revision、来源与应用输入边界的稳定关联；具体字段由 D01/D10/D22 固定。应用前校验分配仍有效，延迟新增不能恢复已撤权限。控制记录不是通用配置热更新，也不解除 Approval/Decision waiting 或复活终态。

## 10. assistant entry

Assistant entry 来自一次稳定 Model Response。

概念：

~~~text
AssistantTranscriptEntry
├── turn_id
├── content[]
│   ├── text
│   ├── reasoning / continuation metadata
│   └── tool_call[]
├── finish_reason
├── model_invocation_reference
└── provider_metadata?
~~~

只保存后续 continuation / model context 真正需要的 provider metadata。

完整 Provider request / response evidence 仍属于 Model Invocation 记录，不复制到 Transcript。

同一逻辑模型调用可能由 Model System 产生多个实际 attempt；失败 partial stream 不与下一 attempt 盲拼成稳定 Assistant entry。保留必要关联/中止事实与完整 call/result pairing，不执行半截调用，也不把请求 retry 伪造成重复工具动作。

## 11. Tool Call

Assistant entry 中的每个 tool_call 至少包含：

~~~text
tool_call
├── call_id
├── model_visible_tool_name
├── arguments
└── provider_metadata?
~~~

call_id 使用 Model System 标准化后仍能满足 Provider continuation 的 identity。

Tool 的长期 stable identity 由 Tool System 持有。

Transcript 不把 model_visible_tool_name 当作长期 Tool identity。

## 12. tool_result entry

每个 model-visible Tool Call 都必须有精确对应的 Tool Result entry。

概念：

~~~text
ToolResultTranscriptEntry
├── turn_id
├── tool_call_id
├── status / category
├── content[]
├── tool_operation_reference?
└── metadata
~~~

tool_call_id 是强关联。

禁止仅靠：

- Tool 名称；
- arguments；
- 执行顺序；

猜测对应关系。

## 13. Tool Call / Result Invariant

稳定 Transcript 必须满足：

> 任何进入下一轮 Model Context 的 Tool Call，都必须有合法、唯一、可序列化的对应 Tool Result。

在一个稳定 Turn boundary：

~~~text
tool_call_count
==
tool_result_count for model-visible calls
~~~

这不表示所有 Tool Result 都是 success。

invalid、denied、timeout、cancelled、truncated 等都可以形成结构化 Tool Result。

## 14. Technical Retry 不增加 Transcript Tool Call

Tool Runtime technical retry：

~~~text
Tool Operation
├── Attempt 1
├── Attempt 2
└── Attempt 3
~~~

在 Transcript 中仍然只对应：

~~~text
one Assistant tool_call
+
one final model-visible tool_result
~~~

Attempt evidence 不伪造成新的 Agent Tool Call。

否则模型会误以为 Agent 主动调用了三次。

## 15. Canonical Transcript 不等于 Execution Log

Transcript 保存 model continuity 所需语义。

Execution Log / Model Invocation / Tool Operation 可以保存更多诊断信息，例如：

- Provider request id；
- retry detail；
- latency；
- backend request id；
- Tool Attempt；
- stack；
- internal error detail；
- checkpoint tick。

这些内容不应该为了“完整”全部塞进 Transcript。

完整性是：

> 对 Agent Loop 模型语义完整。

不是：

> 对所有平台执行证据完整。

## 16. Canonical Transcript 与 Runtime View

二者来自同一次运行事实，但用途不同。

~~~text
Model / Tool / Control Activity
            |
            v
      Turn Processor
       /          \
      v            v
Canonical        Runtime View
Transcript       Projection
      |            |
      v            v
next model       user UI
~~~

对比：

| 运行事实 | Canonical Transcript | Runtime View |
| --- | --- | --- |
| Assistant stream text | 最终一个稳定 text part | 同一个 TextRuntimeItem 多次 delta |
| Reasoning | 保存 continuation 所需内容 / metadata | 可展示 summary / elapsed，默认折叠 |
| Tool Call | Assistant tool_call part | ToolRuntimeItem running |
| Tool Result | 独立 tool_result entry | 更新同一个 ToolRuntimeItem |
| Approval / Decision | typed control input | InteractionRuntimeItem |
| Tool retry | 不产生新 Tool Call | UI 可显示必要状态 |
| Provider metadata | 仅保留 continuation 所需部分 | 通常不显示 |
| Compaction | 原 Transcript 不改 |通常无需显示 |

因此 Runtime View 不能被用于重建 Transcript。

Transcript 也不要求一对一生成 Runtime Item。

## 17. Model Context Projection

每个 Turn 开始前，Agent Loop 从当前稳定状态构造 Model Context Projection。

输入：

~~~text
ModelContextProjectionInput
├── Immutable AgentExecutionContext
├── Canonical Transcript
├── current Compaction Snapshot?
├── persisted Skill bindings effective at this model input boundary
├── context budget
├── current model capability
└── projection policy
~~~

输出：

~~~text
ModelContextProjection
├── final_system_prompt
├── messages[]
├── tool_set
├── estimated_input_budget
└── metadata
~~~

## 18. 每 Turn 重新 Projection

Projection 每个 Turn 都重新计算。

原因：

- Transcript 新增了 Assistant / Tool Results；
- 可能新增 control input；
- compaction snapshot 可能变化；
- large Tool Result projection 可能需要应用；
- context budget 每轮变化；
- provider / model capability projection 可能影响 message representation。

但 immutable startup input 不重新查询。

此前已提交且有效的新增 Skill binding 在本次输入确定点可靠应用；不靠每轮重读全部 Agent 配置、不改 Tool Snapshot。绑定版本不随包更新漂移，后续正文/文件读取仍校验当前资源与授权，移除不回写旧 Transcript 或强制清空已读内容。

## 19. Final System Prompt

Final System Prompt 来自 AgentExecutionContext 中已固化的 Prompt components：

~~~text
Platform Prompt
+
Agent Instructions
+
Trigger Instructions
+
AGENTS.md Snapshot
+
Environment / Execution Constraints
+
需要系统级表达的 Project Variable context
        ↓
Final System Prompt
~~~

Final System Prompt 可以在 Execution 内缓存。

只要 AgentExecutionContext 不变，其语义不应漂移。

缓存的是不可变启动 components。运行期技能目录从独立绑定状态投影，不能因缓存整个请求而漏掉下一轮已生效技能，也不能为更新目录重读并替换 Agent instructions、模型或工具集。

## 20. System Prompt 不进入 Rolling Transcript

Final System Prompt 不作为每个 Turn 的 TranscriptEntry 重复追加。

否则会造成：

- 重复存储；
- sequence 噪声；
- compaction 混淆；
- 不同 Provider role 映射困难。

System Prompt 是每次 Model Request 的 fixed component。

## 21. Unified Model Messages

Projection 最终产出 Model System 接受的统一 Message：

~~~text
system
user
assistant
tool
~~~

以及统一 content parts。

Agent Loop 的 TranscriptEntry.kind 不要求与这些 role 一一相同。

例如：

~~~text
control
-> project as system / user-like model message
~~~

具体 role mapping 由平台统一规则决定，不由 Provider Adapter 猜测业务语义。

## 22. Provider Adapter 边界

Model Adapter 接收的已经是 Unified Model Messages。

Provider Adapter 负责：

- role / part 映射；
- Tool schema；
- call_id；
- provider-specific continuation metadata；
- reasoning capability；
- media representation。

它不负责：

- 选择哪些历史需要保留；
- compaction；
- large Tool Result 截断；
- business control input 解释。

这些属于 Agent Loop Model Context Projector。

## 23. Large Tool Result

Tool Result Source of Truth 由 Tool System 持有。

Transcript 不需要为模型长期保存无限大的原始 result body。

原则：

> Transcript 保存稳定引用 + 必要结构；Model Context Projection 决定本轮模型看到多少。

典型：

~~~text
Tool Result
├── status
├── short model-visible summary
├── artifact / object reference
├── content metadata
└── source operation reference
~~~

## 24. Large Tool Result Projection

当 Tool Result 较大：

优先：

1. 保留结果类型与关键 metadata；
2. 保留模型判断下一步所需摘要 /片段；
3. 保留 artifact / object reference；
4. 提示模型需要完整内容时使用对应 Tool 再读取。

不采用：

- 永远把全部大结果放入每一轮 Model Request；
- 统一硬截固定字符数且不留下来源；
- 静默丢失“结果被截断”的事实。

## 25. Artifact / File Reference

如果 Tool Result 已外部化到 Object Storage / Artifact：

Model Context 中应该保留：

- reference identity；
- type；
- 必要 filename / media type；
- size / truncation metadata；
- 如何再次读取的 Tool 指引。

Agent Loop 不把 Object Storage 当作隐藏 memory。

模型需要全文时仍通过显式 Tool 获取。

## 26. Tool Result 首次回填与后续 Projection

同一个大 Tool Result 在“刚执行完后的下一 Turn”与“若干 Turn 以后”可以使用不同 projection。

例如：

~~~text
Immediate next Turn
-> larger excerpt + reference

Later Turns
-> short summary + reference
~~~

但 Canonical Transcript 的原始语义 identity 不变。

这属于 Model Context Projection policy，不修改 Tool Operation Source of Truth。

## 27. Compaction Snapshot 在 Projection 中的位置

有有效 Compaction Snapshot 时：

~~~text
Final System Prompt
+
Compaction Summary
+
Recent Transcript Entries
+
Current Control Input
+
Current fixed-revision Skill catalog from durable bindings
+
Tool Set
~~~

而不是：

~~~text
Compaction Summary
+
all already-compacted raw history
+
recent history
~~~

否则 compaction 不产生实际 context reduction。

详细 boundary 见 [Context Window 与 Compaction](./context-compaction.md)。

## 28. 已 Compact 历史仍保留在 Transcript

compacted_through 以前的 TranscriptEntry 仍然持久存在。

它们：

- 不默认进入当前 Model Context；
- 仍可用于 debug / audit correlation；
- 仍可支持未来重新生成 Summary；
- 不因 Summary 生成而删除。

Compaction 是 projection state，不是 history mutation。

## 29. Control Input Projection

typed control input 应明确转换成 model-visible instruction。

例如 approval_result：

~~~text
The requested tool action was approved.
Continue from the pending operation/result.
~~~

decision_result：

~~~text
Human decision resolved:
<structured result>
Continue using this decision.
~~~

具体文本模板可以实现化，但必须：

- 表达来源可信；
- 不伪装成 Agent 自己的输出；
- 不把 private internal metadata暴露给模型；
- 保持 reference / result 语义。

## 30. Watchdog Notice Projection

watchdog 中止当前 generation 后，下一 Turn 应看到明确 notice：

- 上一轮 generation 被系统中止；
- 原因是持续时间过长 / loop watchdog；
- 任何不完整 Tool Call 都没有执行；
- 需要重新规划并继续。

这比静默丢掉上一轮更容易让模型自我恢复。

## 31. Doom Loop Warning Projection

Doom Loop 第一阈值触发的 warning 进入 Transcript 作为 control entry。

下一 Turn 可见。

warning 至少表达：

- 检测到连续重复相同 Tool + arguments；
- 已有结果没有变化；
- 请先检查现有信息并采取不同策略。

如果模型改变策略，guard reset。

## 32. Context Budget Metadata

Model Context Projection 应输出 budget metadata，至少概念包含：

~~~text
ContextBudgetMetadata
├── model_context_window
├── fixed_context_estimate
├── transcript_projection_estimate
├── reserved_output_budget
├── total_estimate
└── compaction_required
~~~

estimate 只服务 runtime context management。

它不是 Model Token Usage Source of Truth。

## 33. 不使用估算补写 Token Usage

项目已经明确：

> Model Token Usage 不使用 tokenizer estimation 补算计费 / usage 数据。

因此：

~~~text
Context Budget Estimate
!=
Token Usage
~~~

前者可以采用近似估算。

后者只使用真实 Provider usage / contract 允许的数据。

## 34. Message Normalization

在进入 Provider Adapter 前，Model Context Projector 需要保证：

- Tool Call / Result pairing；
- message ordering；
- role legality；
- control input 已转为允许的统一 Message；
- unsupported content 有明确 projection；
- large result 已按 policy 处理；
- compaction summary 位置稳定。

Provider Adapter 不应该收到一份已经语义损坏的 message list 再去修业务上下文。

## 35. Provider Continuation Metadata

某些 Provider 的 reasoning / tool continuation 可能要求保留 opaque metadata。

原则：

- 只保存 continuation 所必需部分；
- 通过统一 provider_metadata content part 承载；
- 不让上层业务依赖其结构；
- compaction summary 不尝试解释 opaque metadata；
- 跨 Provider 不保证 opaque continuation metadata 可迁移。

本次 Execution 的 Model Snapshot 是 immutable，因此正常运行中不存在跨 Provider continuation。

## 36. Model Change

Agent Execution 运行中不切换 resolved Model Snapshot。

所以 Transcript / Projection 不需要解决：

~~~text
Turn 1 uses provider A
Turn 2 uses provider B
~~~

这种动态迁移问题。

如果未来支持运行中 Model 切换，需要单独设计 provider continuation compatibility。

第一阶段不做。

## 37. Knowledge / Memory Result

Knowledge / Memory 通过普通 Tool 返回。

进入 Transcript 后与其他 Tool Result 使用同一套规则：

- tool_call/result pairing；
- large result projection；
- compaction；
- artifact reference。

不建立隐藏的“自动 Knowledge Context”消息类型。

## 38. Transcript 持久化原则

完整 Transcript 需要 durable 保存，至少能支持：

- checkpoint；
- Central restart recovery；
- compaction boundary；
- debug；
- 模型 continuation。

实现可以按 Execution 分表 / append-only collection / JSON records 等方式持久化，但语义必须保持：

- append-first；
- 稳定 sequence；
- 不因 Runtime View 更新覆盖；
- 不因 compaction 删除。

物理 schema 可以在实现设计中进一步确定。

## 39. Transcript Mutation

原则上已经 stable 的 TranscriptEntry 不进行业务语义修改。

允许的技术性更新应非常有限，例如：

- 补充持久化 reference；
- 补充已确定的 correlation metadata；
- 修复非语义字段。

不能在后续 Turn 中把旧 Tool Result 从 failed 改成 success。

新事实应追加新 entry。

## 40. Checkpoint Position

Checkpoint 不需要复制完整 Transcript。

可以引用：

~~~text
transcript_position
=
last stable sequence / entry_id
~~~

恢复时：

1. 加载 AgentExecutionContext Snapshot；
2. 加载 Transcript 到该 stable position；
3. 加载当前 Compaction Snapshot；
4. 根据初始 Skill 绑定和已持久化的应用位置重建绑定状态，不依赖 Summary 文本猜测；
5. 恢复 pending control / waiting state；
6. 重建 Model Context Projection。

## 41. Transcript 与 Checkpoint 一致性

checkpoint 只能引用已经 durable 的 Transcript position。

禁止：

~~~text
checkpoint says seq = 100
but entries 98-100 not committed
~~~

因此 stable Turn 写入与 checkpoint correlation 必须遵守明确 commit 顺序。

具体事务边界由实现确定，但恢复不能依赖未持久化 message。

同样不能让 checkpoint 声称某个 Skill binding 已应用，而其绑定/control 事实尚未提交。被 compaction 覆盖的技能控制记录仍可追溯，重建目录不重新解析库中最新 revision；精确位置与提交顺序由 D01/D10/D22 落实。

## 42. 数据安全

Transcript 可能包含：

- Tool arguments；
- Tool Results；
- 模型输出；
- 业务上下文。

因此必须服从项目统一安全策略：

- Secret value 不因 Transcript 设计额外暴露；
- Tool Runtime 已做的 safe projection 不被绕过；
- Runtime View public-safe projection 不等于 Transcript 可公开；
- API 查询 Transcript 应有 Execution / Project authorization。

## 43. 与 Runtime View 的恢复边界

Runtime View reconnect 使用自己的 Snapshot + Update Stream。

Agent Loop recovery 使用 Transcript + Checkpoint。

不能：

- 通过 RuntimeItemUpdate replay 重建 Agent Loop；
- 通过 TranscriptEntry 机械重建 UI stream。

二者可以通过 source_reference / turn_id / tool_call_id 关联。

## 44. 设计原则

1. Immutable startup context 与 runtime history 分离；
2. Canonical Transcript 完整保存语义历史；
3. Model Context 是每 Turn projection，不是持久化 Source of Truth；
4. Transcript content 尽量复用 Model System Unified content parts；
5. Tool Call / Result pairing 是硬 invariant；
6. technical retry 不伪造成新的 Agent Tool Call；
7. Runtime View 不能反向充当 Transcript；
8. compaction 不删除 Transcript；
9. large result 使用引用 + model-visible projection；
10. context estimate 不污染 Token Usage；
11. fixed System Prompt 不重复写入 rolling Transcript；
12. Provider-specific metadata 被限制在 continuation 边界内。
