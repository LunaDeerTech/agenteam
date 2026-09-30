# Context Window 与 Compaction 详细设计

> 上级架构：[Agent Loop](./README.md)
>
> 相关设计：
> - [Agent Loop Runtime](./loop-runtime.md)
> - [Transcript 与 Model Context](./transcript-context.md)
> - [Model System](../platform-infrastructure/model-system/README.md)
> - [Model Token Usage](../platform-infrastructure/model-token-usage.md)
> - [Execution Lifecycle](../agent-executor/execution-lifecycle.md)

## 1. 目标

Agent Execution 可以长时间运行，因此 Canonical Transcript 可能远大于单次 Model context window。

本文定义：

- context budget；
- proactive compaction；
- overflow recovery；
- safe compaction boundary；
- rolling summary；
- recent Turns retention；
- Compaction Snapshot；
- large Tool Result 与 compaction 的关系；
- fixed input guard；
- compaction progress guard；
- checkpoint / recovery。

核心原则：

> 完整历史保留在 Canonical Transcript；Compaction 只改变当前 Model Context Projection。

## 2. 非目标

本文不负责：

- 删除历史 Execution；
- 压缩 Runtime View；
- 清理 Object Storage；
- 计算计费 Token Usage；
- 替 Tool System 截断 Source of Truth；
- 改变 AgentExecutionContext。

## 3. Context Budget

每轮 Model Request 都有 context budget。

概念：

~~~text
Model Context Window
    -
Reserved Output Budget
    -
Fixed Context Estimate
    =
Available Runtime Context Budget
~~~

runtime context 包含：

- Compaction Summary；
- recent Transcript；
- control input；
- model-visible Tool Result projection；
- 必要 Provider continuation metadata。

## 4. BudgetMetadata

概念结构：

~~~text
ContextBudget
├── model_context_window
├── reserved_output_budget
├── fixed_context_estimate
├── current_runtime_estimate
├── total_estimate
├── proactive_threshold
└── overflow_margin
~~~

数值单位可以统一为 approximate tokens。

## 5. Context Estimate 与 Usage 分离

Agent Loop 可以为了 context management 维护近似 token estimate。

但：

~~~text
approximate context estimate
!=
provider token usage
~~~

估算不得：

- 写入 Model Token Usage 作为真实 usage；
- 参与计费；
- 替代 Provider usage；
- 对外宣称为精确 token 数。

其用途只包括：

- 判断 proactive compaction；
- 判断 fixed context 是否过大；
- 比较 compaction 前后是否有实际缩减。

## 6. Token Estimate 策略

第一阶段可以使用统一轻量估算策略，例如：

- 文本字符 / 字节近似；
- message / content part 固定 overhead；
- tool schema approximate cost；
- Provider 官方 count-tokens API 若存在可作为更准确来源。

架构不要求为每个 Provider 实现本地 tokenizer。

实现要求是：

> estimate 稳定、单调、足够判断 budget，而不是计费级精确。

## 7. Reserved Output Budget

不能把整个 context window 塞满 input。

需要保留：

~~~text
reserved_output_budget
~~~

用于：

- 当前 Model generation；
- Tool Call arguments；
- reasoning / output；
- provider protocol overhead。

reserved budget 可以根据 Model Snapshot / 系统默认配置确定。

第一阶段不要求 Agent 动态调整。

## 8. Proactive Compaction

正常路径不等待 Provider 返回 context overflow。

每 Turn 开始：

~~~text
build preliminary projection
    ↓
estimate total context
    ↓
above proactive threshold?
    ├── no -> call model
    └── yes -> compact first
~~~

proactive threshold 应低于硬 context window。

这样给估算误差与 output budget 留空间。

## 9. Overflow Recovery

Provider 仍可能返回 context overflow，例如：

- 估算误差；
- Provider tokenization 差异；
- hidden overhead；
- model context 配置变化。

此时：

~~~text
Model Request
    ↓
context overflow
    ↓
finish failed invocation
    ↓
compaction recovery
    ↓
rebuild Model Context
    ↓
retry Model Turn
~~~

overflow recovery 是兜底，不是唯一触发机制。

## 10. Compaction 只能在稳定边界执行

默认允许 compaction 的位置：

- 完整 Tool Batch 已 settle；
- Tool Results 已进入 Transcript；
- 当前 Turn stable；
- 准备发起下一 Model Turn。

禁止在：

- Tool Batch 仍执行中；
- 副作用 Tool outcome unknown；
- 半个 Assistant stream；
- tool_call 已出现但对应 result 尚未稳定；

直接切历史。

## 11. Safe Boundary

优先以完整 Turn 为切分单位。

概念：

~~~text
old history
| Turn 1 |
| Turn 2 |
| Turn 3 |
----------- compact boundary
| Turn 4 |
| Turn 5 |
| Turn 6 |  keep recent
~~~

不能把：

~~~text
Assistant tool_call
-----------
Tool Result
~~~

切在边界两侧。

Tool Call / Result pairing 优先于 token 精确最大化。

## 12. Split-turn Compaction

极端情况下，单个 Turn 本身可能非常大。

例如：

- 一个 Tool Result 极大；
- Assistant Response 很长；
- 大量并行 read-only Tool Call。

此时可以允许 split-turn compaction，但只能选择安全 semantic boundary。

优先顺序：

1. Tool Result 本身已经外部化并可缩短 model-visible projection；
2. Assistant / Tool Batch 的合法子边界；
3. 不会拆散 Provider continuation 所要求的内容。

如果无法找到安全边界，则失败，而不是任意删 message。

## 13. Recent Context Retention

Compaction 不把全部 history 总结后只剩 Summary。

需要保留最近一段 raw context。

概念：

~~~text
Previous Summary
+
Newly Compacted Span
        ↓
New Summary

Current Model Context
=
New Summary
+
Recent Raw Turns
~~~

recent retention 可以按 approximate token budget 而不是固定 Turn 数确定。

## 14. Rolling Compaction

采用 rolling summary，不在每次 compaction 时重新总结整个历史。

第 N 次：

~~~text
Summary N-1
+
Newly Compacted Transcript Span
        ↓
Summary N
~~~

优点：

- compaction input 不随 Execution 总历史无限增长；
- 成本更稳定；
- 避免反复对同一旧历史总结；
- 可持续长期运行。

## 15. Summary Model

第一阶段使用：

> 当前 Agent Execution 已固化的同一个 resolved Model Snapshot。

不单独配置：

- Project Compaction Model；
- System Compaction Model。

理由：

- Execution 内 Model 不漂移；
- 能力 / Provider compatibility 一致；
- 配置更简单；
- 减少额外模型依赖。

未来如果有明确成本需求再增加 override。

## 16. Summary Request

Summary generation 是 Agent Loop 内部 Model invocation。

它使用同一个 Model Snapshot，但不是普通 Agent Turn。

至少应：

- 使用专用 compaction prompt；
- 禁用业务 Tool Calling；
- 不让 Summary 自己触发 Tool；
- 记录独立 Model Invocation / Token Usage；
- 支持 cancellation；
- 服从 Model System timeout / retry。

## 17. Summary 不产生 Runtime Tool Loop

Compaction summary 是内部 context maintenance。

它不应该：

- 作为 Agent assistant message 伪装进 Runtime View；
- 执行 Tool Call；
- 修改业务状态；
- 触发 Meeting Message；
- 触发 Task Tool。

如需要 UI 可观测性，最多投影 Notice，例如：

~~~text
Context compacted
~~~

但不是必须。

## 18. Summary Format

Summary 使用固定结构化 Markdown，而不是自由文本或严格 JSON。

至少包含：

~~~markdown
## Objective

## Constraints / Important Decisions

## Completed / Current Work

## Blockers / Unresolved

## Next Steps

## Critical References
~~~

可以按业务需要增加字段，但核心 section 名称保持稳定。

## 19. Summary 质量目标

Compaction Summary 应优先保留：

- 当前目标；
- 明确用户 / 业务约束；
- 已做关键决策；
- 已完成工作；
- 当前进行中的工作；
- 失败原因 / blocker；
- 尚未解决问题；
- 下一步；
- 关键对象 identity；
- 关键文件 / artifact reference；
- 必要 Tool result conclusions。

不优先保留：

- 重复寒暄；
- 无后续价值的过程文本；
- 低价值 stream fragments；
- 已经被更高质量结论覆盖的中间猜测。

## 20. Summary 必须基于稳定语义

Summary input 来自 Canonical Transcript / previous Compaction Snapshot。

不直接总结：

- Runtime View UI labels；
- Tool Attempt stack；
- 未稳定 Model stream；
- 未确认 Tool outcome。

否则恢复后可能把“执行中”误写成“已完成”。

## 21. CompactionSnapshot

Compaction 结果独立持久化。

概念：

~~~text
CompactionSnapshot
├── snapshot_id
├── execution_id
├── version
├── previous_snapshot_id?
├── summary
├── compacted_from_sequence
├── compacted_through_sequence
├── first_kept_sequence
├── before_context_estimate
├── after_context_estimate
├── model_snapshot_reference
├── model_invocation_reference
├── created_at
└── metadata
~~~

snapshot 不复制整个 Transcript。

## 22. Boundary Identity

至少需要两个稳定位置：

~~~text
compacted_through_sequence
first_kept_sequence
~~~

要求：

~~~text
first_kept_sequence
>
compacted_through_sequence
~~~

如果存在 intentionally retained overlap，需要显式表示，不允许靠模糊语义。

第一阶段建议不保留重叠 raw history，避免 Summary + raw history 重复。

## 23. Snapshot Version

每次成功 compaction 创建新 version。

~~~text
v1 -> v2 -> v3
~~~

旧 Snapshot 可以保留用于 debug / recovery evidence。

当前 Agent Loop runtime 只引用 active snapshot。

Snapshot 一旦 stable，不原地修改 summary 内容。

## 24. Previous Summary

后续 compaction 输入使用：

~~~text
active CompactionSnapshot.summary
+
Transcript(active.compacted_through + 1 ... new boundary)
~~~

而不是读取全部更老 Transcript。

这样保证 rolling compaction。

## 25. First Compaction

没有 previous snapshot 时：

~~~text
Old Transcript Span
    ↓
Summary 1
~~~

然后：

~~~text
Model Context
=
Summary 1
+
Recent Raw Transcript
~~~

## 26. Large Tool Result 与 Compaction

Large Tool Result 优先在 Model Context Projection 层做引用 / 摘要 / 截断。

Compaction 不应该把一个已外部化 Tool Result 的完整原始 body 再拉回 Summary input。

Summary 应看到：

- model-visible result projection；
- 稳定 artifact / object reference；
- 关键结论；
- 必要 metadata。

这避免 compaction 本身再次被巨型 Tool Result 撑爆。

## 27. Tool Result Critical Reference

如果一个大 Tool Result 对后续工作非常重要，Summary 的 Critical References 应保留：

- artifact identity；
- file / object reference；
- 产生它的 Tool Call purpose；
- 后续如何读取。

这样 compact 后 Agent 仍能按需重新打开内容。

## 28. Fixed Context

Fixed Context 包括：

- Final System Prompt；
- Platform Prompt-derived instructions；
- Agent instructions；
- Trigger-specific instructions；
- AGENTS.md snapshot；
- 不可压缩的 environment / execution constraints；
- Tool schema / Tool Set projection 中必要部分。

Compaction 不能减少这部分。

## 29. Fixed Context Too Large

如果：

~~~text
fixed_context_estimate
+
reserved_output_budget
>=
safe model context budget
~~~

则历史 compaction 无法解决问题。

Agent Loop 应直接失败：

~~~text
error.category = context_fixed_input_too_large
~~~

不进入无限 compaction。

## 30. 不自动截断 System Instructions

第一阶段禁止 Agent Loop 静默截断：

- Platform Prompt；
- Agent Instructions；
- Trigger Instructions；
- AGENTS.md；
- 安全约束。

理由：

> 这些内容改变可能改变 Agent 行为与权限语义。

如果固定输入过大，应明确失败，让上层配置 / 文档修正。

## 31. Compaction Progress Guard

每次 compaction 记录：

~~~text
before_context_estimate
after_context_estimate
old_boundary
new_boundary
~~~

成功 compaction 至少需要满足：

1. boundary 有推进；
2. context estimate 有显著下降；
3. first_kept_sequence 合法；
4. Summary + recent history 仍保持 message invariant。

## 32. 无有效缩减

如果：

~~~text
new_boundary == old_boundary
OR
after_context_estimate >= before_context_estimate - minimum_reduction
~~~

则认为本次 compaction 没有有效推进。

不能立即无限重试同一操作。

## 33. Compaction Recovery Limit

连续 compaction recovery 有一个小的固定 / 可配置上限。

例如逻辑上：

~~~text
overflow
-> compact
-> still overflow
-> compact
-> no progress / limit exceeded
-> fail
~~~

架构不写死具体次数。

超过上限：

~~~text
error.category = context_compaction_failed
~~~

## 34. Summary Generation Failure

Summary Model invocation 可能：

- timeout；
- provider unavailable；
- cancelled；
- invalid response；
- output truncated。

Model System 自身先执行其 retry policy。

最终仍失败时：

- 不能删除旧 Transcript；
- 不能推进 active CompactionSnapshot；
- 保留 previous snapshot；
- Agent Loop 根据错误进入 failed 或 cancel。

Compaction 是 replace-after-success。

## 35. Atomic Activation

新 Compaction Snapshot 只有在以下全部完成后才能 active：

1. Summary Model invocation 成功；
2. Summary 可解析 / 验证；
3. boundary 合法；
4. before / after metadata 已计算；
5. Snapshot durable persist 完成。

然后：

~~~text
active_compaction_snapshot_id
-> new snapshot
~~~

如果任何步骤失败，继续引用旧 snapshot。

## 36. Compaction 与 Checkpoint

compaction 完成后是一个优先 checkpoint safe point。

Checkpoint 可以引用：

~~~text
compaction_snapshot_id
+
transcript_position
~~~

不需要把 summary 再复制一份进 checkpoint body。

恢复：

~~~text
load checkpoint
-> load active CompactionSnapshot
-> load recent Transcript
-> rebuild Model Context Projection
~~~

## 37. Compaction 与 Waiting

如果 Execution 进入 waiting：

- 不因为 waiting 自动 compaction；
- checkpoint 机制照常；
- 如果 waiting 前最后一个 Turn 已 stable，可以按正常 budget policy 决定是否 compaction；
- 长时间 waiting 释放内存时，active CompactionSnapshot 必须已 durable。

resume 后下一 Turn 再正常执行 budget check。

## 38. Compaction 与 Cancel

收到 cancel 时：

如果正在 summary generation：

- 传播 cancellation；
- 不激活半成品 Snapshot；
- 保持旧 snapshot；
- 停止下一 Turn。

如果新 Snapshot 已成功 durable 并 active：

- 可以作为最后稳定 checkpoint state 使用。

## 39. Proactive Threshold

推荐把 proactive threshold 表达为 runtime policy：

~~~text
estimated_total_context
>=
context_window - reserved_output_budget - safety_margin
~~~

而不是固定“使用到 80%”。

不同 Model 的 context window / output 能力差异较大。

具体 margin 可以实现配置化。

## 40. Context Overflow Classification

Agent Loop 只有在 Model System 明确标准化为 context overflow 时才进入 overflow compaction recovery。

不能把：

- invalid API request；
- auth failure；
- rate limit；
- provider outage；

全部当成“可能 context 太长”然后 compaction。

## 41. Tool Schema Budget

Execution Tool Set 本身也消耗 model context。

因此 fixed_context_estimate 应包含 ToolSpec projection 的成本。

如果 Tool Set 太大导致 fixed context 过大：

- 同样进入 fixed_input_too_large；
- 不能通过删除对话历史无限解决。

未来可以设计 Tool discovery / dynamic exposure，但不属于本篇第一阶段。

## 42. Summary 与业务事实

Summary 不是业务 Source of Truth。

例如 Summary 写：

~~~text
Task appears completed
~~~

不等于 Task state = done。

业务事实仍来自：

- Task Domain；
- Meeting Domain；
- Tool Source of Truth；
- Artifact；
- 其他正式领域对象。

Summary 只服务模型连续性。

## 43. Summary 与错误事实

Summary 应区分：

- confirmed fact；
- Agent assumption；
- unresolved hypothesis；
- failed attempt。

不能把失败尝试总结成已成功。

专门的 Blockers / Unresolved section 用于保留这些差异。

## 44. Summary Prompt

compaction prompt 至少应明确：

- 只总结提供的 previous summary + newly compacted span；
- 不得假设未提供事实；
- 保持对象 ID / file / artifact references；
- 保留未完成状态；
- 不要生成 Tool Call；
- 按照固定 Markdown section 输出；
- 优先面向后续 Agent continuation，而不是面向用户写报告。

Prompt 具体措辞属于实现，可基于 Harness 经验优化。

## 45. Summary Output Validation

第一阶段不要求 JSON schema，但仍需要轻量 validation：

- 非空；
- 包含核心 section；
- 长度不明显失控；
- 不是 Provider error text；
- 没有 Tool Call。

若 section 缺失，可允许规范化或一次重试；不要无限 retry。

## 46. Summary Output Budget

Compaction summary 自身必须有输出预算。

否则 summary 可能接近被压缩历史长度，失去意义。

实现应根据：

- model context window；
- target recent context；
- summary target size；

设置合理 output budget。

架构不写死 token 数。

## 47. 首轮即 Overflow

如果第一个 Agent Turn 前就发现 context 超限：

先判断：

~~~text
fixed context too large?
    ├── yes -> fail fixed_input_too_large
    └── no
         |
         v
initial input / trigger context too large?
         |
         v
attempt safe projection / compaction if possible
~~~

如果没有可 compact 的历史，不能进入无意义 rolling compaction loop。

## 48. Initial Trigger Context 很大

Trigger Context 已是 Execution Snapshot，不能重新查询替换。

但 Model Context Projection 可以对其中明确允许引用化的 payload 使用：

- artifact reference；
- file reference；
- typed summary；

前提是 Execution Context 本身已经保存稳定来源。

不能静默丢 Trigger 的强业务约束。

具体 Trigger Context 的可引用字段由 Execution Context contract 决定。

## 49. Compaction Observability

内部 evidence 至少记录：

- snapshot_id；
- boundary；
- before / after estimate；
- summary Model Invocation；
- duration；
- trigger = proactive / overflow；
- success / failure category。

这些用于 diagnostics。

Runtime View 是否显示 compact notice 由 UI policy 决定。

## 50. Compaction Token Usage

Summary Model Invocation 是真实 Provider invocation。

因此：

- 它产生真实 Token Usage；
- 应计入对应 Agent Execution；
- usage Source of Truth 仍由 Model Token Usage 模块记录；
- context estimate 不代替其 usage。

## 51. Recovery 后重新 Compaction

Central restart 后：

1. 加载 active CompactionSnapshot；
2. 加载其 first_kept_sequence 之后 Transcript；
3. 重建 projection；
4. 执行正常 budget check。

如果仍接近阈值，可以产生下一版 compaction。

不需要仅因为 restart 重新生成相同 Summary。

## 52. Snapshot Corruption

如果 active CompactionSnapshot 无法读取或 boundary 与 Transcript 不一致：

- 不能凭猜测继续；
- 可以尝试回退到最近一个已验证旧 Snapshot；
- 如果仍无法构造安全 projection，则 recovery failed。

不能删除 Transcript 来“修复”。

## 53. 不自动过期

第一阶段：

- CompactionSnapshot 不自动过期；
- 不做 retention cleanup；
- 不删除已 compact Transcript。

未来存储成本需要时，可以单独设计 retention。

## 54. 与 Harness 设计的关系

本设计借鉴成熟 Harness 中共同出现的做法：

- pi：rolling compaction、recent context retention、safe boundary；
- OpenCode：session compaction、overflow recovery 与 compaction guard 教训；
- Codex：完整 history 与 per-request prompt projection 分离；
- Gemini CLI：history compression 与 large Tool Output masking。

agenteam 的差异：

- Execution Context 在 preparing 后 immutable；
- 不重读业务数据来动态改 System Context；
- compaction snapshot 是 Execution runtime state；
- Tool / Model / Runtime View 各自保持独立 Source of Truth。

## 55. 设计原则

1. proactive compaction 为主，overflow recovery 为兜底；
2. compaction 只在稳定边界执行；
3. 优先按完整 Turn 切分；
4. tool_call / result 永不被非法拆散；
5. 使用 rolling summary；
6. 保留 recent raw context；
7. Compaction Snapshot 独立 durable；
8. 使用当前 Execution Model Snapshot；
9. 大型 Tool Result 优先引用化，不反复塞全文；
10. 近似 token 只服务 context budget；
11. 固定 System input 不静默截断；
12. 无 progress 的 compaction 必须终止；
13. 新 Snapshot 成功后才原子激活；
14. Summary 不是业务 Source of Truth；
15. crash recovery 复用已持久化 Snapshot，不重复 summary。
