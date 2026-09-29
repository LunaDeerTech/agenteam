# Runner Management 详细设计

> 上层架构：[Runner 架构](./README.md)
>
> 相关详细设计：
> - [Control Protocol](./control-protocol.md)
> - [Agent Workspace](./agent-workspace.md)

## 1. 设计范围

本文定义一个 Runner 设备如何成为 agenteam 的受管执行节点。

主要覆盖：

- Runner 配置；
- Enrollment；
- Ed25519 Device Identity；
- Control Channel 连接认证；
- Runner version / protocol version；
- compatibility；
- capability synchronization；
- online / offline / incompatible；
- credential rotation / re-enrollment；
- shutdown；
- 本地诊断日志。

本文不定义：

- Tool Authorization；
- Agent Capability；
- Agent Workspace 内部执行；
- Runner RPC payload；
- Data Channel payload；
- Desktop / Tunnel operation。

## 2. 核心原则

1. Runner 是系统级设备资源；
2. Runner 不属于某个 Project；
3. Runner 不维护第二套业务权限配置；
4. Runner 本地只保存连接 Central 所需的设备配置与凭据；
5. Runner Capability 以实际运行环境探测结果为准；
6. Central 是 Runner metadata、Agent Mount、Authorization 与 Audit 的 Source of Truth；
7. 同一个 `runner_id` 同一时刻只保留一个有效 Control Channel。

## 3. Runner 配置模型

Central 中的 Runner 至少包含：

~~~text
Runner
├── id
├── name
├── description
├── tags[]
├── root_path
├── status
│   ├── online
│   ├── offline
│   └── incompatible
├── runner_version?
├── protocol_version?
├── os?
├── arch?
├── headless?
├── capabilities[]
├── last_seen_at?
├── enrolled_at?
└── device_public_key?
~~~

其中真正需要用户配置的字段只有：

~~~text
name
description
tags
root_path
~~~

其余字段都不应作为普通用户可编辑配置：

- `id`：Central 创建 Runner 时自动生成；
- `status`：由当前连接状态和协议兼容性自动计算；
- `runner_version / protocol_version`：由 Runner 在 hello 中上报；
- `os / arch / headless`：由 Runner 根据当前设备环境自动探测并上报；
- `capabilities`：由 Runner 实际能力探测结果自动生成并同步；
- `last_seen_at`：由 heartbeat / connection activity 自动更新；
- `enrolled_at`：Enrollment 成功时自动记录；
- `device_public_key`：Enrollment 时由 Runner 提交并绑定。

因此 Runner 配置 UI 只需要让用户维护：

> name、description、tags、root-path

设备状态、版本、平台信息和 Capability 只展示，不提供普通编辑入口。

其中：

- `root_path` 在创建 Runner 时确定；
- capability 不由管理员手工维护 allowlist，而由 Runner 实际探测并上报；
- status 不用于表达单项 Capability 健康度；
- Project / Agent Mount 不是 Runner 自身配置字段。

Runner 本地配置至少包含：

~~~text
RunnerLocalConfig
├── central_url
├── runner_id
├── private_key
└── root_path
~~~

第一阶段 private key 直接保存在本地配置文件中，并限制文件权限为 Runner 运行用户可读。

## 4. 创建与 Enrollment

管理员先在 Central 创建 Runner。

Central 创建：

- `runner_id`；
- Runner metadata；
- 短期一次性 enrollment token。

enrollment token 必须：

- 高熵；
- 有过期时间；
- 一次性消费；
- Central 只保存 hash 或等价不可逆验证值；
- 成功使用后立即失效。

初次安装流程：

~~~mermaid
sequenceDiagram
    participant U as Admin
    participant C as Central
    participant R as Runner

    U->>C: create Runner(root_path, metadata)
    C-->>U: enrollment token / install command
    U->>R: install + enrollment token
    R->>R: generate Ed25519 key pair
    R->>C: enroll(runner_id, token, public_key, platform)
    C->>C: validate + consume token
    C->>C: bind public_key to runner_id
    C-->>R: enrollment success
    R->>R: persist local config + private key
~~~

Enrollment 成功后：

- public key 成为当前 Runner Device Identity；
- private key 只保存在 Runner 本地；
- enrollment token 不再具有任何长期认证用途。

## 5. Control Channel 连接认证

Runner 每次建立 Control Channel 前先获取一次性 challenge。

~~~mermaid
sequenceDiagram
    participant R as Runner
    participant C as Central

    R->>C: request challenge(runner_id)
    C-->>R: nonce + expires_at

    R->>R: sign(nonce | timestamp | runner_id)
    R->>C: WSS connect(runner_id, nonce, timestamp, signature)

    C->>C: verify runner + nonce + timestamp + signature
    C->>C: consume nonce
    C-->>R: connection accepted

    R->>C: hello
~~~

要求：

- nonce 单次使用；
- nonce 有短有效期；
- timestamp 有允许的时钟偏差窗口；
- 已经失效或不存在的 `runner_id` 不能连接；
- 验签失败直接拒绝；
- challenge 成功不代表 Runner 可以执行任意 operation，业务 Authorization 仍在 Central 完成。

Central 身份由 TLS server certificate 保证。

## 6. 单连接模型

同一个 `runner_id` 同一时刻只允许一个有效 Control Channel。

当新的已认证连接建立：

1. Central 将新连接注册为当前 connection；
2. 旧 connection 被替换；
3. 旧 connection 后续消息不再作为有效 Runner 消息处理；
4. 旧 connection 应被主动关闭。

这样避免：

- RPC 路由分叉；
- heartbeat split-brain；
- capability snapshot 冲突；
- 同一个 Runner 同时执行来自多个连接状态的请求。

## 7. Hello 与 Capability 探测

Runner 连接成功后第一条协议消息必须是 `hello`。

概念：

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

Capability 由 Runner 根据当前设备实际环境探测，例如：

~~~text
filesystem
command
managed_process
transfer
desktop
browser_use
computer_use
tunnel
~~~

Central 不维护另一套 Runner capability allowlist，也不把 capability probe 映射成统一 Tool available / unavailable 状态。

Central 使用 Runner 当前上报结果：

- 记录该设备当前实际 Capability；
- 把必要 capability metadata 进入 AgentExecutionContext；
- 在具体 Tool Call 选择某个 Mount / Runner 后校验目标设备是否支持该 operation。

Runner offline 或某项 Capability 缺失时，实际调用直接返回对应 Backend / Capability error。

## 8. Protocol Version 与 Compatibility

Protocol 使用 Major / Minor version。

规则：

~~~text
major incompatible
-> reject connection
-> Runner.status = incompatible

major compatible
+ minor difference
-> allow connection
-> negotiate features in hello
~~~

Central 不承诺无限兼容所有历史 Runner。

Runner status = `incompatible` 仅表示协议 / 版本无法建立正常服务，不用于表示单项 capability 故障。

管理员升级 Runner 后，如果版本恢复兼容，可以重新进入正常连接流程。

## 9. Runner 状态模型

第一阶段只使用：

~~~text
online
offline
incompatible
~~~

### online

存在有效 Control Channel，且 hello 已完成。

### offline

不存在有效 Control Channel。

常见原因：

- Runner 未启动；
- 网络断开；
- heartbeat timeout；
- Runner 正常退出；
- 当前连接被新连接替换。

### incompatible

Runner 可以到达 Central，但 protocol major version 或其他强制 compatibility gate 不满足。

Capability 的某一项不可用不改变顶层状态。

## 10. Heartbeat 与 last_seen

Control Channel 建立后 Runner 周期性发送 heartbeat。

Central 返回 heartbeat ack，并可以在连接建立或后续控制消息中下发：

- heartbeat interval；
- heartbeat timeout；
- 其他非业务运行参数。

Runner 有本地合理默认值用于连接 bootstrap，但连接稳定后采用 Central 下发参数。

Central 根据 heartbeat 更新 `last_seen_at`。

超过 timeout：

~~~text
connection unhealthy
-> close / invalidate connection
-> Runner.status = offline
~~~

## 11. Reconnect

Runner 断线后自动重连。

采用：

- exponential backoff；
- jitter；
- 稳定连接一段时间后重置 backoff。

重连后：

1. 重新执行 challenge authentication；
2. 建立新的 WSS；
3. 发送新的 hello；
4. 重新上报 capability metadata。

第一阶段不要求在 Runner Management 层恢复未完成 RPC，也不要求重连时自动进行 Managed Process 全量 reconcile。

RPC outcome 由 Control Protocol 与 Tool Runtime 的 unknown 语义处理。

## 12. Credential Rotation / Re-enrollment

需要轮换 Runner credential 时：

1. Central 使旧 public key 失效；
2. 当前 Control Channel 关闭；
3. Central 生成新的短期 enrollment token；
4. Runner 生成新的 Ed25519 key pair；
5. Runner 使用原 `runner_id` 重新 enrollment；
6. 新 public key 与原 Runner 绑定；
7. 原 Runner metadata、Agent Mount 关系保持不变。

Re-enrollment 不创建新的 Runner identity。

## 13. Runner Upgrade

第一阶段 Runner 由管理员显式升级。

Central 展示：

- runner version；
- protocol version；
- compatibility。

第一阶段不实现：

- 自动静默升级；
- Central 直接远程替换 Runner binary；
- 自动 rollback。

如果版本不兼容，Runner 进入 `incompatible`。

## 14. Shutdown

Runner clean shutdown 时直接停止，不设计 draining protocol。

关闭流程固定为：

1. 停止接受新的 Runner request；
2. 对当前 active RPC 触发 cancellation；
3. 按 Execution Runtime 的 graceful -> force 规则终止 active command；
4. 终止 Runner 管理的全部 Managed Process，包括 `scope = persistent`；
5. 关闭 active Data Channel / Tunnel Channel；
6. 关闭 Control Channel；
7. 退出 Runner process。

因此 `persistent` 只表示 Managed Process 可以跨 Agent Execution 存活，不表示可以跨 Runner shutdown 存活。

如果 Runner 因 crash、kill -9、设备掉电等原因无法执行 clean shutdown，则 Central 仍可能得到 unknown outcome，OS 中也可能留下无法确认归属的进程；这属于异常恢复语义，不改变 clean shutdown 的终止规则。

Central 根据连接断开把 Runner 标记 offline。

具体 command / process 语义见 [Execution Runtime](./execution-runtime.md)。

## 15. 本地日志与 Audit

Central Audit 是 canonical audit。

Runner 本地只保留用于诊断的：

- 启动 / shutdown 日志；
- enrollment / connection 状态；
- protocol error；
- capability probe；
- operation diagnostic metadata；
- 本地异常。

Runner 本地日志：

- 不是正式 Audit；
- 不持久化 Project Secret plaintext；
- 不记录 environment secret value；
- 不保存完整敏感 request payload；
- 应执行必要脱敏。

正式 Tool / Execution Audit 仍由 Central 持久化。

## 16. 与其他模块的边界

### Agent Management

保存 Agent Mount 配置，不由 Runner Management 自己管理 Agent。

### Agent Executor

读取 Runner / Mount metadata 构造 AgentExecutionContext。

### Tool System

提供 Runner / Mount / Capability metadata，供具体 Runner Tool 调用时完成目标设备解析与 Capability 校验。

### Security / Governance

完成 Authorization / Approval / Audit。

### Control Protocol

负责连接建立后的 message contract、RPC、streaming、cancel、reconnect outcome。

## 17. 第一阶段实现边界

第一阶段确定：

1. Central Runner entity；
2. 本地 Runner config；
3. root-path；
4. 一次性 enrollment token；
5. Ed25519 identity；
6. challenge-signature WSS authentication；
7. 单 runner_id 单连接；
8. hello + capability synchronization；
9. Major / Minor protocol compatibility；
10. online / offline / incompatible；
11. heartbeat + Central 下发参数；
12. reconnect with backoff + jitter；
13. re-enrollment；
14. 手工升级；
15. 直接 shutdown；
16. 本地诊断日志。
