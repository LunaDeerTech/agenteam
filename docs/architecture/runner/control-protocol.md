# Control Protocol 详细设计

> 上层架构：[Runner 架构](./README.md)
>
> 相关详细设计：
> - [Runner Management](./runner-management.md)
> - [Data Channel](./data-channel.md)
> - [Execution Runtime](./execution-runtime.md)
> - [One-time Approval、Tool Retry 与 Idempotency](../security-governance/one-time-approval-retry-idempotency.md)

## 1. 设计范围

本文定义 Central 与 Runner 之间的 Control Channel contract。

主要覆盖：

- WSS connection；
- JSON Envelope；
- hello；
- protocol / feature negotiation；
- heartbeat；
- request / response；
- request_id / operation_id；
- stream event；
- deadline；
- cancellation；
- error；
- disconnect / reconnect；
- unknown outcome；
- Data Channel negotiation。

Control Protocol 不重新定义 Unified Tool Runtime 的：

- Authorization；
- Approval；
- Tool Operation persistence；
- retry policy；
- idempotency policy。

## 2. 核心原则

1. Runner 主动向 Central 建立 WSS；
2. Control Channel 只承载结构化控制消息和轻量 stream event；
3. 业务 RPC 只由 Central -> Runner 发起；
4. Runner -> Central 发送 response / event，不主动调用 Central 业务服务；
5. 每个 Backend Attempt 使用唯一 `request_id`；
6. technical retry 保持同一个 `operation_id`，但生成新的 `request_id`；
7. 协议层不自动重放失败请求；
8. 断线后无法确认的 request 使用 `unknown` outcome；
9. 大文件 / binary payload 使用 Data Channel，不塞进 JSON Control Channel。

## 3. Connection

Control Channel：

~~~text
WebSocket over TLS
JSON text frame
Runner initiated
~~~

连接认证由 Runner Management 定义的 challenge-signature 完成。

认证成功后，连接进入：

~~~text
connected
-> awaiting_hello
-> active
-> closed
~~~

在收到有效 hello 前，Central 不发送业务 request。

## 4. Envelope

所有 Control Channel message 使用统一 Envelope：

~~~text
Envelope
├── protocol_version
├── type
├── message_id
├── request_id?
├── operation_id?
├── timestamp
└── payload?
~~~

### protocol_version

当前协议 Major / Minor version。

### type

消息类型。

### message_id

每条协议消息自身的唯一 ID，用于诊断和 trace。

它不替代 request_id。

### request_id

某一次 Runner RPC Attempt 的唯一 ID。

### operation_id

当前 Unified Tool Operation 的稳定 ID。

非 Tool 型 protocol message 可以没有 operation_id。

### timestamp

发送方生成的协议时间戳，用于诊断，不作为业务顺序的唯一依据。

## 5. Message Types

第一阶段至少支持：

~~~text
hello
hello_ack

heartbeat
heartbeat_ack

request
response
cancel
stream

runner_status

data_channel_open
data_channel_ready
data_channel_close

protocol_error
~~~

不再使用 `process_snapshot` 作为重连必选消息。

Managed Process 第一阶段不做 reconnect full snapshot reconcile。

## 6. Hello

Runner 建立连接后必须首先发送：

~~~text
type = hello
~~~

payload：

~~~text
RunnerHello
├── runner_id
├── runner_version
├── protocol_version
├── os
├── arch
├── headless
├── capabilities[]
└── feature_flags[]
~~~

Central 校验：

- runner_id 与已认证 connection 一致；
- Major version compatible；
- payload schema 合法。

Central 返回：

~~~text
HelloAck
├── accepted
├── negotiated_protocol_version
├── heartbeat_interval
├── heartbeat_timeout
└── enabled_features[]
~~~

如果 Major 不兼容：

- 返回 protocol error 或在握手阶段直接关闭；
- Runner 标记为 incompatible；
- 不进入 active。

## 7. Heartbeat

Runner 在 active connection 上发送：

~~~text
heartbeat
~~~

payload 只包含轻量 runtime metadata，例如：

~~~text
Heartbeat
├── sequence
├── runner_time
└── optional lightweight health metadata
~~~

Central 返回：

~~~text
heartbeat_ack
~~~

Heartbeat 不承载：

- 完整 process list；
- 大量 capability payload；
- Tool result；
- 文件内容。

Central 可以通过 hello_ack 或后续控制配置更新 heartbeat interval / timeout。

Heartbeat timeout 后：

~~~text
connection invalid
-> close
-> Runner offline
~~~

## 8. Request

业务 operation 只由 Central 发起。

概念结构：

~~~text
RunnerRequest
├── request_id
├── operation_id
├── execution_id
├── project_id
├── agent_id
├── mount
│   ├── mount_id
│   └── workspace
├── operation
├── deadline?
├── environment?
├── idempotency_key?
├── audit_correlation_id?
└── payload
~~~

### request_id

每个 actual Attempt 唯一。

technical retry：

~~~text
operation_id = same
request_id = new
~~~

### operation_id

对应 Unified Tool Runtime 中同一个逻辑 Tool Operation。

### mount

Runner 不接收 Central 提供的任意 host absolute path。

请求只携带稳定 Mount identity 和 workspace logical identity，Runner 根据本地 root-path 计算 physical path。

### environment

只在 command / managed process 等需要时出现。

其中可以包含：

- normal variables；
- 当前 Agent 被允许使用的 Secret plaintext。

environment 是 sensitive payload：

- 不写普通 protocol log；
- 不进入 Runner persistent config；
- operation 完成后释放。

### deadline

Central 可以给当前 request 指定 absolute deadline。

Runner 使用 deadline 驱动本次执行 cancellation。

### idempotency_key

只有具体 backend operation 支持可验证 idempotency 时使用。

Runner Protocol 不假设所有 operation 都支持。

## 9. Response

每个 request 最终最多形成一个 terminal response。

~~~text
RunnerResponse
├── request_id
├── operation_id
├── outcome
│   ├── success
│   ├── failure
│   ├── cancelled
│   └── unknown
├── code?
├── message?
├── payload?
└── metadata?
~~~

### success

operation 已形成成功结果。

### failure

Runner 能明确判断本次 Attempt 没有成功完成，并有确定错误。

### cancelled

Runner 已确认当前 request 被取消并形成终态。

### unknown

Runner 自身仍在线时也可以在无法确定 operation 最终状态时显式返回 unknown。

更多时候，unknown 由 Central 因连接中断推导。

## 10. Error Code

Runner 返回稳定机器可读 code。

第一阶段至少包括：

~~~text
INVALID_REQUEST
UNSUPPORTED_OPERATION
NOT_FOUND
CONFLICT

WORKSPACE_NOT_FOUND
PATH_OUTSIDE_WORKSPACE

PROCESS_NOT_FOUND
PROCESS_ALREADY_EXITED

TIMEOUT
CANCELLED

TRANSFER_FAILED
DATA_CHANNEL_FAILED

CAPABILITY_UNAVAILABLE
INTERNAL_ERROR
~~~

不再使用 Runner 本地权限系统语义的 `PERMISSION_DENIED` 作为常规业务授权结果。

Authorization 应在 Central dispatch 前完成。

Runner 仍然可以因协议完整性或 workspace/path 校验拒绝请求，但应返回更具体 code。

## 11. Stream Event

长时间 command / process operation 可以在 terminal response 前产生 stream event。

这里的 RunnerStreamEvent 是 Runner Control Protocol message，不是 Platform Internal Domain Event。它只表达当前 RPC / process stream 的协议级增量，不能直接写入 DomainEventOutbox，也不能直接作为 Human Inbox projection source。

统一结构：

~~~text
RunnerStreamEvent
├── request_id
├── operation_id
├── stream
│   ├── stdout
│   ├── stderr
│   └── progress
├── sequence
├── data
└── timestamp
~~~

规则：

- stream event 必须关联 request_id；
- 同一 stream 的 sequence 单调递增；
- terminal response 才决定当前 request 的最终 outcome；
- stream event 本身不是 Tool Result；
- Central 可以把 stdout / stderr 转换为 Runtime View Item / Execution Log；
- Secret masking 在 Runner 发出 stream event 前执行；
- Central 持久化前仍可再次执行 masking。

Control Channel 只适合文本 / 小型 progress stream。

高频 binary / image stream 应转 Data Channel。

## 12. Deadline

Central 可以为当前 request 提供 deadline。

Runner 收到 request 后：

1. 校验 deadline；
2. 创建 cancellation context；
3. 执行 operation；
4. deadline 到达时触发 cancel；
5. 尝试形成 cancelled / failure；
6. 如果最终副作用无法确认，返回或被 Central 标记 unknown。

Deadline 是当前 Attempt 的 transport/runtime bound。

Tool Operation 的完整 timeout 语义仍由 Unified Tool Runtime 决定。

## 13. Cancel

Central 可以发送：

~~~text
type = cancel
request_id = ...
~~~

Runner 找到 active request 后触发 cooperative cancellation。

对 command：

~~~text
graceful termination
-> grace period
-> force kill
~~~

对其他 operation：

- 尚未开始时直接取消；
- 文件 / transfer 尽可能停止；
- Desktop automation 尽可能停止；
- 已经产生不可逆副作用时不能假装回滚。

Cancel ack 不单独作为最终结果。

最终仍通过 terminal response 返回：

~~~text
cancelled
or
unknown
or
success/failure if already completed
~~~

## 14. Disconnect 与 Pending Request

连接断开时，Central 对 pending request 分类。

如果在断线前已经收到 terminal response：

~~~text
known outcome
~~~

如果 request 已发送，但没有 terminal response：

~~~text
unknown outcome
~~~

Central 将对应 Tool Attempt 标记为 unknown。

协议层不执行：

- 自动 resend；
- 自动 replay；
- reconnect 后继续等待旧 request；
- 通过参数相同猜测旧结果。

后续是否 retry 完全交给 Unified Tool Runtime。

## 15. Reconnect

Runner 重连：

1. challenge authentication；
2. new Control Channel；
3. hello；
4. capability / feature negotiation；
5. 进入 active。

重连不会恢复旧 request_id。

如果 Tool Runtime 决定 retry：

~~~text
same operation_id
new request_id
~~~

Runner 不实现通用 persistent RPC Journal，因此重启后也不承诺能查询旧 request outcome。

## 16. 并发

Runner Protocol 本身允许同一 Control Channel 存在多个 active request。

Runner 第一阶段不增加独立的全局并发限制。

真正是否并行由：

- Unified Tool Runtime；
- 当前 model turn；
- Tool read/write semantics；
- Central dispatch；

决定。

Runner 只负责正确关联：

- request；
- stream；
- response；
- cancel。

## 17. Data Channel Negotiation

需要大文件 / binary payload 时，Central 通过 Control Channel 发送：

~~~text
data_channel_open
~~~

payload 至少包含：

~~~text
DataChannelOpen
├── channel_id
├── request_id?
├── operation_id?
├── direction
├── purpose
├── size?
├── media_type?
├── checksum?
├── expires_at
└── one_time_credential
~~~

Runner 建立独立 Data Channel 后返回：

~~~text
data_channel_ready
~~~

完成或失败后：

~~~text
data_channel_close
~~~

完整数据平面见 [Data Channel](./data-channel.md)。

## 18. Runner Status Event

Runner 可以在 active connection 上发送轻量 `runner_status` event，用于 capability / environment metadata 发生变化。

`runner_status` 同样只是 Runner -> Central 的协议消息，不等于 Platform Domain Event。Central 收到后先由 Runner Management 更新 / 解释 canonical Runner runtime state；只有 Central 形成了需要跨模块传播的稳定业务事实时，才由 Runner Management 另行产生 typed Internal Domain Event。

例如：

- headless environment 变化；
- Desktop capability added / removed；
- tunnel capability added / removed。

这类变化：

- 不改变 Runner 顶层 online/offline/incompatible，除非连接或 compatibility 本身改变；
- Central 更新 capability metadata；
- 后续新的 Tool resolution 使用最新 runtime state。

## 19. Protocol Error

非法协议消息使用 `protocol_error`：

~~~text
ProtocolError
├── code
├── safe_message
├── offending_message_id?
└── fatal
~~~

fatal error 可以直接关闭 Control Channel。

例如：

- invalid envelope；
- incompatible version；
- hello order violation；
- duplicate terminal response；
- malformed request correlation。

## 20. 日志与敏感数据

Control Protocol logging 必须区分：

~~~text
safe metadata
vs
sensitive payload
~~~

可以记录：

- type；
- message_id；
- request_id；
- operation_id；
- timing；
- payload size；
- safe error code。

默认不记录：

- environment secret values；
- file content；
- binary payload；
- full command output；
- Desktop image data。

## 21. 第一阶段实现边界

第一阶段实现：

1. authenticated WSS；
2. JSON Envelope；
3. hello / hello_ack；
4. heartbeat / heartbeat_ack；
5. request / response；
6. cancel；
7. stream；
8. runner_status；
9. Major / Minor negotiation；
10. Central-provided heartbeat config；
11. multiple active request correlation；
12. unknown outcome on disconnect；
13. no automatic replay；
14. no persistent RPC journal；
15. Data Channel negotiation messages；
16. protocol error。
