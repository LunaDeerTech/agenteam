# One-time Approval、Tool Retry 与 Idempotency 详细设计

> 状态：设计稿
>
> 上层架构：[安全与治理架构](../../architecture/security-governance.md)
>
> 相关架构：
> - [统一工具系统架构](../../architecture/tool-system.md)
> - [Agent Loop 架构](../../architecture/agent-loop.md)
> - [Runner 架构](../../architecture/runner.md)
> - [Approval Scope 详细设计](./approval-scope.md)

## 1. 设计目标

本文解决一个具体问题：

> 用户选择“批准本次”以后，如果同一个 Tool 操作因为网络错误、Runner RPC 失败或其他技术原因发生 retry，是否需要重新审批；如果执行结果是 unknown，又应该如何处理。

核心原则：

1. One-time Approval 绑定一个逻辑 Tool Operation，而不是绑定某一次底层执行 attempt；
2. Approval 只回答“用户是否允许执行这件事”；
3. Retry / Idempotency 只回答“这件事是否可以安全地再次尝试”；
4. Approval 不能把一个本来不安全的 retry 变成安全 retry；
5. Agent 后续重新发起一个参数相同的 Tool Call，也不等于原操作的 retry。

## 2. Tool Call、Tool Operation 与 Attempt

系统区分三个层次：

~~~text
tool_call_id
= Model 产生的一次 Tool Call

operation_id
= Tool System 为这次逻辑操作生成的稳定 ID

attempt_id / backend request_id
= 某一次实际执行尝试
~~~

概念关系：

~~~text
Tool Call
  -> Tool Operation
      -> Attempt 1
      -> Attempt 2
      -> ...
~~~

第一次执行时：

~~~text
tool_call_id = tc-1
operation_id = op-1
attempt_id = attempt-1
~~~

如果 Tool System 对同一个逻辑操作执行技术 retry：

~~~text
operation_id = op-1
attempt_id = attempt-2
~~~

operation_id 不变，attempt_id 改变。

如果 Agent Model 后续又生成一次新的 Tool Call，即使 Tool 和 arguments 完全相同：

~~~text
tool_call_id = tc-2
operation_id = op-2
~~~

它仍然是一个新的逻辑操作。

系统不能仅因为参数相同，就把新的 Tool Call 视为原操作 retry。

## 3. One-time Approval 的绑定对象

用户选择：

~~~text
批准本次
~~~

系统创建 One-time Approval。

One-time Approval 绑定：

~~~text
OneTimeApproval
├── id
├── project_id
├── agent_id
├── tool_id
├── operation_id
├── operation_fingerprint
├── source_approval_request_id
├── created_at
└── state
~~~

其中：

- operation_id：本次被批准的逻辑 Tool Operation；
- operation_fingerprint：对本次 Tool、关键参数和目标资源进行规范化后得到的不可变指纹；
- One-time Approval 不绑定具体 attempt_id。

因此 One-time Approval 的语义是：

> 用户允许当前 Agent 完成 operation_id 所代表的这一个逻辑操作。

而不是：

> 用户允许某一个底层 RPC request 恰好执行一次。

## 4. 不采用“首次执行即消费”的模型

One-time Approval 不作为一张可以被某次 attempt 消费掉的通用票据。

不采用：

~~~text
Approval available
  -> Attempt 1 consume
  -> Approval gone
  -> Attempt 2 重新审批
~~~

而采用：

~~~text
One-time Approval
  <-> Tool Operation

Tool Operation
  -> Attempt 1
  -> Attempt 2
  -> ...
~~~

只要仍然是同一个 operation_id，并且 operation fingerprint 没有变化，技术 retry 不需要重新向用户审批。

当该 Tool Operation 进入终态以后，该 One-time Approval 不再适用于任何其他 operation。

## 5. Operation Fingerprint

仅依赖 operation_id 不足以防止实现错误导致 retry 时参数被修改。

因此 One-time Approval 同时绑定 operation_fingerprint。

概念上：

~~~text
operation_fingerprint =
canonicalize(
  tool_id,
  security-relevant arguments,
  target resource,
  relevant execution target
)
~~~

retry 必须满足：

~~~text
same operation_id
AND
same tool_id
AND
same operation_fingerprint
~~~

如果 retry 前参数发生变化，例如：

~~~text
git push origin main
->
git push --force origin main
~~~

则不能继续沿用原 One-time Approval。

它必须被视为新的 Tool Operation，并重新经过正常 Authorization / Approval 流程。

具体 canonicalization 规则由 Tool System / Tool Adapter 定义，但必须稳定且可验证。

## 6. Approval 与 Retry 的职责边界

Approval 与 Retry 是两套不同判断。

### Approval

回答：

> 用户是否允许执行这个 Tool Operation？

### Retry / Idempotency

回答：

> 在已经尝试执行这个 Tool Operation 之后，是否可以安全地再次尝试？

因此：

~~~text
Approved
!=
Retry Safe
~~~

拥有 One-time Approval 不能绕过 Tool 的 retry policy。

例如：

~~~text
run-command("git push")
~~~

即使已经获得 One-time Approval，如果第一次 attempt 的结果是：

~~~text
unknown
~~~

并且无法证明操作幂等，也不能自动再次执行。

## 7. Retry 决策

同一个 Tool Operation 的 attempt 返回后，根据 outcome 和 Tool retry metadata 决定下一步。

推荐流程：

~~~mermaid
flowchart TD
    Start["Tool Operation<br/>已有执行授权"]
    Attempt["执行 Attempt"]
    Outcome{"Attempt Outcome"}

    Success["Operation Success"]
    Failure{"Failure 是否允许 Retry？"}
    Unknown{"Outcome = unknown？"}
    SafeUnknown{"Tool 幂等或存在<br/>可验证 idempotency key？"}

    Retry["同一 operation_id<br/>创建新 Attempt"]
    Failed["Operation Failed / Return Error"]
    NeedsResolution["Operation Unknown<br/>停止自动 Retry"]

    Start --> Attempt
    Attempt --> Outcome

    Outcome -->|"success"| Success
    Outcome -->|"明确 failure"| Failure
    Outcome -->|"unknown"| Unknown

    Failure -->|"是"| Retry
    Failure -->|"否"| Failed

    Unknown --> SafeUnknown
    SafeUnknown -->|"是"| Retry
    SafeUnknown -->|"否"| NeedsResolution

    Retry --> Attempt
~~~

每个 retry：

- operation_id 不变；
- operation fingerprint 不变；
- attempt_id / backend request_id 变化；
- 不重新创建 Approval Request；
- 不重新创建 One-time Approval。

## 8. Unknown Outcome

分布式执行中可能发生：

~~~text
Central -> Runner: 执行操作
Runner 已经执行
连接断开
Central 没有收到最终 Response
~~~

此时 outcome 为：

~~~text
unknown
~~~

其含义是：

> 操作可能没有执行，也可能已经产生副作用，但系统无法确认最终结果。

如果 operation：

- 明确幂等；或
- 携带后端可验证的 idempotency key；

则 Tool retry policy 可以允许在同一个 operation_id 下再次尝试。

否则：

~~~text
unknown
+ non-idempotent
-> 禁止自动 retry
~~~

One-time Approval 不改变这个结论。

## 9. Idempotency Key

如果具体 Tool Backend 支持幂等键，可以把 operation_id 或由它派生的稳定 key 作为 backend idempotency key。

例如：

~~~text
operation_id = op-123
        ↓
backend idempotency key = tool/op-123
~~~

多次 attempt 使用同一个 backend idempotency key。

这样即使：

~~~text
Attempt 1 已经成功
但 response 丢失
~~~

Attempt 2 到达 backend 时，backend 可以识别它属于同一个逻辑操作，而不是再次产生副作用。

是否支持这种机制由具体 Tool / Backend 决定。

Tool System 不能假定所有 Tool 都支持 idempotency key。

## 10. Agent Loop 与 Tool System 的职责

### Agent Loop

Agent Loop 可以触发有限 technical retry，但必须由 Tool System 返回明确的 retryability。

Agent Loop 不通过“Tool 名称和参数看起来一样”自行判断 retry。

### Tool System

Tool System 负责：

- 创建并维护 operation_id；
- 创建 attempt；
- 计算 / 校验 operation fingerprint；
- 执行 Approval Match / Approval Policy；
- 保存 One-time Approval 与 operation 的绑定；
- 根据 Tool / Backend metadata 判断 retryability；
- 保证 retry 使用同一个 operation_id；
- 保证新的 Agent Tool Call 创建新的 operation_id。

## 11. Runner RPC

Runner RPC 的每一次实际请求都是一个 attempt。

概念映射：

~~~text
Tool Operation
operation_id = op-123

Attempt 1
runner request_id = req-1

Attempt 2
runner request_id = req-2
~~~

RunnerRequest 应携带当前 operation_id，并保留唯一的 request_id。

对于支持 idempotency 的 Runner operation，也可以携带：

~~~text
idempotency_key
~~~

Runner 返回：

~~~text
success
failure
cancelled
timeout
unknown
~~~

Central 根据 outcome 和 operation retry policy 决定是否创建下一次 attempt。

Runner 自身不能因为看到同一个 command 字符串就判断它属于 retry。

## 12. One-time Approval 生命周期

One-time Approval 的生命周期跟随绑定的 Tool Operation。

概念上：

~~~text
Approval Request
    ↓
用户“批准本次”
    ↓
One-time Approval
    ↓
bound to operation_id
    ↓
Attempt 1
    ↓
可能的 Attempt 2 / Attempt 3
    ↓
Operation terminal
~~~

Operation 进入终态以后，这条 One-time Approval 不再匹配任何新的 Tool Operation。

第一阶段不需要设计独立的“consume”动作。

因此文档语义统一使用：

~~~text
One-time Approval is bound to one Tool Operation
~~~

而不是：

~~~text
One-time Approval is consumed by the first Attempt
~~~

## 13. 审计与日志

为了能够还原 retry 和 Approval 的关系，执行记录至少保留：

~~~text
execution_id
tool_call_id
operation_id
attempt_id / backend request_id
tool_id
operation_fingerprint
approval_id
attempt outcome
retry reason
idempotency metadata
~~~

这样 Audit / Execution Log 可以区分：

~~~text
同一个 operation 的技术 retry
~~~

和：

~~~text
Agent 又发起了一次新的相同 Tool Call
~~~

不得只根据 Tool arguments 合并两次调用。

## 14. 第一阶段结论

第一阶段确定：

1. One-time Approval 绑定 Tool Operation，而不是底层 Attempt；
2. 每个 Tool Operation 有稳定 operation_id；
3. 每次实际执行有独立 attempt_id / request_id；
4. 同一 operation 的 technical retry 不重新审批；
5. 新 Agent Tool Call 永远创建新的 operation，即使参数相同；
6. One-time Approval 同时绑定 operation fingerprint；
7. retry 时 operation fingerprint 必须保持一致；
8. 参数变化视为新 operation，需要重新经过授权流程；
9. Approval 不决定 retry 是否安全；
10. retry 是否允许由 Tool / Backend 的 idempotency 和 outcome 规则决定；
11. unknown + non-idempotent 不允许自动 retry；
12. 明确幂等或具备可验证 idempotency key 时，可以在同一 operation 下 retry；
13. backend 支持时，多次 attempt 使用同一个稳定 idempotency key；
14. 第一阶段不设计 One-time Approval 的独立 consume 动作；
15. 日志 / Audit 必须区分 tool_call_id、operation_id 和 attempt/request_id。
