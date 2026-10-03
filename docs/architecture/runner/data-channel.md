# Data Channel 详细设计

> 上层架构：[Runner 架构](./README.md)
>
> 相关详细设计：
> - [Control Protocol](./control-protocol.md)
> - [Execution Runtime](./execution-runtime.md)
> - [Desktop & Tunnel](./desktop-tunnel.md)
> - [Object Storage](../platform-infrastructure/object-storage.md)

## 1. 设计范围

本文定义 Runner 与 Central 之间用于 binary / large payload 的独立 Data Channel。

主要覆盖：

- channel negotiation；
- channel identity；
- authentication；
- outbound connection；
- binary transfer；
- upload / download；
- checksum；
- progress；
- cancel；
- cleanup；
- Transfer Capability；
- 与 Object Storage 直连路径的边界。

## 2. 为什么需要独立 Data Channel

Control Channel 使用：

~~~text
WSS + JSON text frame
~~~

适合：

- RPC metadata；
- heartbeat；
- status；
- cancellation；
- stdout / stderr 小型文本 stream。

不适合：

- 大文件；
- binary；
- Desktop 高频 image；
- 长时间大量 transfer payload。

因此第一阶段正式提供 Data Channel，而不是只保留未来扩展点。

## 3. 网络拓扑

Data Channel 仍由 Runner 主动建立。

~~~mermaid
sequenceDiagram
    participant C as Central
    participant R as Runner
    participant D as Central Data Endpoint

    C->>R: data_channel_open(metadata + one-time credential)
    R->>D: outbound WSS connect
    D->>D: validate channel_id + credential
    D-->>R: channel accepted
    R->>C: data_channel_ready
    Note over R,D: binary transfer
    R->>C: data_channel_close(result)
~~~

Central 不主动连接 Runner。

## 4. 第一阶段 Transport

第一阶段 Data Channel 使用独立：

~~~text
WebSocket over TLS
binary frames
~~~

原因：

- 与现有反向代理部署方式一致；
- Runner 仍只需要 outbound HTTPS/WSS；
- 支持双向 binary；
- 不需要额外定义 TCP port protocol；
- Runner client 实现成熟。

Data Channel 与 Control Channel 使用不同 WebSocket connection。

## 5. Channel Session

每次 Data Channel session 有稳定：

~~~text
DataChannelSession
├── channel_id
├── runner_id
├── request_id?
├── operation_id?
├── direction
├── purpose
├── state
├── expected_size?
├── media_type?
├── checksum?
├── created_at
├── expires_at
└── completed_at?
~~~

state：

~~~text
created
connecting
active
completed
failed
cancelled
expired
~~~

第一阶段一个 Data Channel Session 只服务一个逻辑 transfer。

不在同一 channel multiplex 多个独立文件 transfer。

这样可以简化：

- lifecycle；
- cancellation；
- checksum；
- retry；
- correlation。

## 6. Channel Authentication

Data Channel 不复用 Runner private key 做每个 binary frame 签名。

Central 在 Control Channel 上创建 session 时生成：

~~~text
one_time_channel_credential
~~~

credential 必须：

- 高熵；
- 与 `channel_id + runner_id` 绑定；
- 短期有效；
- 单次使用；
- 成功建立 channel 后立即消费；
- 不进入普通日志。

Runner 建立 Data Channel 时提交：

- runner_id；
- channel_id；
- one-time credential。

TLS 验证 Central server identity。

## 7. Direction

session 明确 direction：

~~~text
runner_to_central
central_to_runner
bidirectional_stream
~~~

文件 transfer 第一阶段通常使用单向：

- upload：runner_to_central；
- download：central_to_runner。

Desktop 高频交互场景可以使用 bidirectional_stream。

## 8. Binary Frame

第一阶段 Data Channel binary frame 采用简单顺序流。

概念 frame：

~~~text
DataFrame
├── sequence
├── offset
├── flags
└── bytes
~~~

metadata 不重复塞入每个 binary payload。

完整文件 metadata 在 Control Channel 的 `data_channel_open` 中协商。

接收端必须：

- 按 sequence / offset 校验；
- 检测重复 / 缺失；
- 不静默拼接错误顺序的 payload。

## 9. Transfer Completion

发送方完成 payload 后发送 terminal control frame / channel completion marker。

接收方校验：

- received size；
- checksum（如果提供）；
- expected metadata。

成功后 session：

~~~text
active -> completed
~~~

失败：

~~~text
active -> failed
~~~

Control Channel 同步收到：

~~~text
data_channel_close
├── channel_id
├── status
├── transferred_size
├── checksum?
└── error?
~~~

## 10. Checksum

文件 transfer 推荐使用 checksum。

概念：

~~~text
algorithm = sha256
value = ...
~~~

如果调用方提供 expected checksum，接收端必须验证。

checksum mismatch：

~~~text
transfer failed
~~~

不能把损坏 payload 当作成功文件。

## 11. Progress

大文件 transfer 可以通过 Control Channel 发送低频 progress event：

~~~text
TransferProgress
├── channel_id
├── transferred_bytes
├── total_bytes?
└── timestamp
~~~

不要求每个 binary frame 都产生 Control Channel progress。

具体节流间隔属于实现配置。

## 12. Cancellation

Central 可以通过 Control Channel cancel：

~~~text
cancel data channel / owning request
~~~

Runner / Central：

1. 停止继续发送；
2. 关闭 Data Channel；
3. 删除未完成临时文件；
4. session -> cancelled；
5. owning Runner request 根据实际 operation 语义形成 cancelled / failure / unknown。

Data Channel cancellation 不自动回滚已经被消费的业务数据。

## 13. Connection Failure

Data Channel 断开：

- session -> failed；
- 已接收不完整 payload 不作为成功结果；
- 临时文件应清理；
- owning RPC 得到 `DATA_CHANNEL_FAILED` 或 unknown，取决于业务副作用是否能确认。

Data Channel 自己不自动重连续传。

第一阶段 retry 由上层重新创建新的 Data Channel Session。

## 14. Runner Transfer Capability

Runner backend 可以提供：

- `file-upload`；
- `file-download`；
- `file-transfer`。

它们表示 Runner 与 Central 之间的数据搬运能力。

### file-upload

~~~text
Runner workspace
-> Data Channel
-> Central
~~~

### file-download

~~~text
Central
-> Data Channel
-> Runner workspace
~~~

### file-transfer

用于明确的双端 Runner/Central transfer orchestration。

是否把这些 operation 直接暴露为 model-visible Tool，由 Tool System 决定。

Data Channel 本身不是 Agent Tool。

## 15. Workspace Integration

写入 Runner workspace 的 transfer 必须走 Agent Workspace path resolution。

请求只提供：

~~~text
mount/workspace identity
+ relative target path
~~~

Runner 不接受 Central 下发任意 host absolute target path。

Filesystem containment 规则与普通 Filesystem Tool 相同。

## 16. Object Storage 是另一条路径

平台 Artifact / Skill / Object Storage 场景使用 Runner 对象直传，payload 不要求经过 Central Data Channel。

当 Agent 已经通过平台 Tool 获得 Object Storage object，并需要把它传到 Runner：

~~~mermaid
sequenceDiagram
    participant C as Central
    participant R as Runner
    participant O as Object Storage

    C->>R: object transfer metadata + short-lived access
    R->>O: download object
    O-->>R: object bytes
    R->>C: operation result
~~~

上传相反：

~~~text
Runner
-> Object Storage
-> Central records StoredObject metadata
~~~

因此固定边界：

~~~text
Runner <-> Central binary/file
= Data Channel

Runner <-> Object Storage object
= Control Channel 协商
+ Runner direct object transfer
~~~

不把 Object Storage payload 绕回 Central Control Channel。

技能准备由 Central 验证 Project、Agent、Execution、当前技能授权/固定 revision 和 Mount 后，协调指定 Runner 获取完整版本包；安装工具提交 Runner 中标准包时使用相反上传路径。业务发布仍由 Skill 安装服务完成，上传对象成功不等于安装成功。版本目录和校验见 [Agent Workspace](agent-workspace.md#71-技能版本包准备)，内容/分配规则见 [Agent Skills](../agent-skills.md)。

对象存储端点由可信部署配置提供，须对 Runner 实际可达，可经内网/VPN 或受保护 HTTPS；不要求公开 bucket 或控制台。不可达时明确失败，不自动回退 Central 中转。端点/TLS、单次授权及真实上传下载由 D05/D15/D17/D28 验证。

## 17. Object Storage Credential

Runner 不持久化 Object Storage credential。

Central 为单次 transfer 提供：

- short-lived signed URL；
- 或等价短期 scoped credential。

credential 必须：

- 只覆盖当前 object / operation；
- 有短 TTL；
- 不写普通日志；
- operation 完成后不缓存。

Runner 不获得平台 Object Storage 的长期 access key。

短期传输凭据也不进入模型、普通 Tool Result/Transcript 或长期缓存，只经受信任协议给指定 Runner；工具返回业务对象 identity 与 Mount 相对路径。

## 18. Desktop Data

Desktop screenshot / browser / computer use 如果产生：

- 单张小型图片：可以通过 Tool Result 引用 StoredObject；
- 高频截图 / video-like binary stream：使用 Data Channel。

Data Channel 不理解 Desktop 业务语义，只传 binary stream。

具体 Desktop contract 见 [Desktop & Tunnel](./desktop-tunnel.md)。

## 19. 临时文件

接收大文件时优先：

1. 写入 temporary file；
2. 完整接收；
3. 校验 size / checksum；
4. atomic rename 到最终目标。

避免传输中断留下看似完整的目标文件。

临时文件命名必须避免用户控制路径注入。

## 20. Size Limit

Data Channel 本身不维护独立管理员 policy。

Central 在创建 session 时可以下发：

- max size；
- expected size；
- deadline。

Runner 根据当前 request 执行。

这里的传输大小/期限与连接故障保护不构成全工具统一 Operation deadline，也不消耗或结束人工审批等待。字段和取消后的完整性/unknown 处理按正式传输契约验证。

实际磁盘不足 / OS 错误作为明确 transfer failure 返回。

## 21. 日志

Data Channel diagnostic log 可以记录：

- channel_id；
- runner_id；
- direction；
- purpose；
- size；
- timing；
- outcome；
- safe error。

不记录：

- binary payload；
- one-time credential；
- Object Storage signed credential；
- file content。

## 22. 第一阶段实现边界

第一阶段实现：

1. independent WSS binary Data Channel；
2. Runner outbound connection；
3. one-time channel credential；
4. one logical transfer per session；
5. upload / download；
6. ordered binary frame；
7. size + optional sha256；
8. low-frequency progress；
9. cancellation；
10. failed transfer cleanup；
11. Runner Transfer Capability；
12. Object Storage direct transfer path；
13. short-lived Object Storage access；
14. Desktop binary stream extension boundary。

第一阶段不要求：

- resumable chunk upload；
- transfer dedup；
- multi-file multiplexing in one channel；
- peer-to-peer Runner transfer；
- persistent Data Channel pool。
