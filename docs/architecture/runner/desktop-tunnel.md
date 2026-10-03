# Desktop & Tunnel 详细设计

> 上层架构：[Runner 架构](./README.md)
>
> 相关详细设计：
> - [Runner Management](./runner-management.md)
> - [Control Protocol](./control-protocol.md)
> - [Data Channel](./data-channel.md)
> - [Execution Runtime](./execution-runtime.md)

## 1. 设计范围

本文定义 Runner 上的交互型设备能力：

- Desktop；
- screenshot；
- automation；
- browser-use；
- computer-use；
- expose-port / Tunnel。

这些能力直接实现在同一个 Runner 工程中，不拆成独立 Runner plugin。

## 2. Capability 探测

Runner 启动时探测当前设备实际环境。

例如：

~~~text
headless = true
-> no desktop capability

headless = false
+ supported desktop backend initialized
+ required OS permissions granted
-> desktop
-> screenshot
-> automation
-> browser_use?
-> computer_use?
~~~

Central 不维护第二套 capability allowlist。

Runner hello 上报当前实际 capability。

首期适配与验收矩阵：

| 环境 | 目标能力 | 必须有的实际证据 |
| --- | --- | --- |
| macOS 原生桌面 | 截图、鼠标/键盘输入等桌面能力 | 支持系统版本、实际后端及屏幕录制/辅助功能等授权 |
| Linux X11 | 截图、鼠标/键盘输入等桌面能力 | display/session、后端初始化及实际动作 |
| Linux Wayland | 截图、鼠标/键盘输入等桌面能力 | 明确 compositor/后端与交互授权条件、原生 Wayland 场景 |

Windows 延后。D17 固定具体版本、后端与细分能力，缺桌面/权限/后端时返回明确不可用；不把 X11 或少数 XWayland 窗口验证等同于 Wayland 整桌面支持，也不承诺所有 compositor 无条件可用。交叉编译不能代替目标 OS 验收。

## 3. Desktop Capability 边界

Desktop Capability 是 Runner runtime capability，不是独立的 Agent Runtime。

Runner 不负责：

- Agent planning；
- Tool Authorization；
- Approval；
- UI task orchestration；
- Model vision reasoning。

Runner 只执行 Central 已经授权并路由过来的具体 Desktop operation。

## 4. screenshot

`screenshot` 获取当前设备 / display 的图像。

结果不应把大图像 base64 塞进 JSON Control Channel。

推荐：

~~~text
capture image
-> StoredObject / Data Channel
-> image_ref / file_ref metadata
-> Tool Result
~~~

单张小型截图可以直接上传 Object Storage 后返回 image_ref。

高频截图流使用 Data Channel。

## 5. automation

`automation` 表示较底层的桌面输入 / UI automation 能力，例如：

- mouse；
- keyboard；
- window interaction；
- accessibility backend interaction。

具体 input schema 由 Runner Tool definition 决定。

Runner 只执行 operation，不在本地运行独立的 LLM planning loop。

## 6. browser-use

当 Runner 当前环境具备 Browser Capability 时，可以提供 browser-use。

首期仍要求实际桌面及相应后端，不因此新增 headless browser-use；具体动作/会话 schema 与浏览器后端在 D17/D21 正式规格确定，不在此选定库或宣称兼容性已验证。

这属于 Runner 内置实现的一部分。

Runner 可以使用：

- 本地浏览器 automation backend；
- browser debugging protocol；
- OS / accessibility integration；

但这些实现细节不成为 Agent Loop 的 contract。

Agent 只看到统一 Runner Tool。

## 7. computer-use

computer-use 与 browser-use 一样属于可选实际 Capability。

它可以组合：

- screenshot；
- pointer；
- keyboard；
- desktop state。

Runner 不因为 headless=false 就自动声称完整 computer-use 可用。

实际 backend 初始化成功后才上报 capability。

## 8. Desktop Binary Data

Desktop 高频图像或 interaction stream：

~~~text
Control Channel
-> operation / metadata

Data Channel
-> image / binary stream
~~~

Data Channel 只负责 bytes，不理解 Desktop 业务语义。

单次 operation 的最终状态仍由 Control Channel terminal response 表达。

## 9. Desktop Cancellation

Desktop operation 收到 cancel 后应尽快：

- 停止后续输入；
- 停止截图 / stream；
- 关闭当前临时 interaction session；
- 返回 cancelled 或已知 terminal result。

如果某个 UI 动作已经产生外部副作用，cancel 不表示自动回滚。

仍遵守 Tool Runtime 的 unknown / idempotency 语义。

## 10. Tunnel 定位

Tunnel 是 Runner 第一阶段正式 Capability。

目的：

> 把 Runner 本地监听服务临时暴露为 Central 管理的外部访问入口。

典型场景：

- dev server；
- preview；
- debug UI；
- 本地 Web service；
- browser testing target。

Tunnel 网络仍保持 Runner 主动出站。

## 11. Tunnel 总体结构

~~~mermaid
flowchart LR
    User["已登录的 Project Owner"]
    Gateway["Central Tunnel Gateway<br/>Session / Owner / 生命周期校验"]
    Channel["Outbound Tunnel Channel"]
    Runner["Runner"]
    Local["127.0.0.1:port<br/>local service"]

    User -->|"HTTPS / WSS"| Gateway
    Gateway <-->|"Tunnel stream"| Channel
    Channel --> Runner
    Runner --> Local
~~~

Central 不反向拨号 Runner。

访问地址和路由由 Central 创建管理，Runner 主动建立反向通道，仅负责本地代理。知道地址不授予访问权限，不提供访客分享入口。

## 12. 第一阶段 Tunnel 类型

第一阶段优先支持 Web 开发场景：

~~~text
HTTP
HTTPS termination at Central
WebSocket upgrade
~~~

Central 提供外部 HTTPS URL。

Runner 连接本地目标：

~~~text
http://127.0.0.1:<local_port>
~~~

第一阶段不要求开放任意公网 raw TCP port。

如果未来需要数据库、SSH 等 raw TCP tunnel，再单独扩展 tunnel transport。

## 13. Tunnel Session

概念：

~~~text
TunnelSession
├── tunnel_id
├── runner_id
├── project_id
├── agent_id
├── workspace
├── creator_execution_id
├── operation_id
├── managed_process_id?
├── local_port
├── state
├── public_endpoint
├── created_at
├── expires_at
└── closed_at?
~~~

state：

~~~text
creating
active
closed
expired
failed
~~~

## 14. expose-port

Runner Tool：

~~~text
expose-port
~~~

输入至少包括：

- mount / workspace；
- local port；
- optional Managed Process ID；
- TTL。

创建 expose-port 的 Tool Authorization 在 dispatch 前完成；访问该入口的人类 Session 授权是另一条独立检查，工具获批不等于任何知道地址的人均可访问。

`expose-port` 会改变 Runner 本地服务的网络暴露边界，因此默认审批规则将其标记为 `security_sensitive` 并要求 Approval。如果未来支持 Reusable Approval，Scope 至少绑定 Runner / Mount / Workspace 与 local port，不能自动扩大到其他设备、workspace 或端口。完整规则见 [默认审批策略](../security-governance/default-approval-policy.md) 与 [Approval Scope](../security-governance/approval-scope.md)。

Runner 本地确认：

- local port 合法；
- 如提供 process_id，则 process 属于当前 Runner / Agent / workspace；
- tunnel backend 当前 available。

## 15. Managed Process Binding

Tunnel 可以绑定 Managed Process。

例如：

~~~text
start-process(dev server)
-> process_id = p1

expose-port(
  process_id = p1,
  local_port = 3000
)
~~~

绑定后：

- Tunnel metadata 记录 process_id；
- process 明确退出时，Central 可以关闭对应 Tunnel；
- Tunnel 关闭不自动 stop Managed Process。

如果没有 process_id，只按 local_port 暴露现有 listener。

这允许 Agent 暴露由普通 command / 已有进程启动的服务，但仍需要正常 Tool Authorization。

## 16. Tunnel Transport

Central 创建 TunnelSession 后，通过 Control Channel 通知 Runner 建立 outbound Tunnel Channel。

Tunnel Channel 可以使用长期 WSS stream：

~~~text
Runner
-> outbound WSS
-> Central Tunnel Gateway
~~~

Gateway 接收外部 HTTP / WebSocket 请求，并通过 tunnel stream 转发到 Runner。

Runner 再代理到：

~~~text
127.0.0.1:<local_port>
~~~

Tunnel Channel 与普通 Data Channel 不复用同一个 session：

- Data Channel 是有限 payload transfer；
- Tunnel Channel 是 TTL 范围内持续 proxy session。

但两者可以共享底层 connection/authentication primitives。

## 17. Tunnel Authentication

Runner 建立 Tunnel Channel 前，Central 通过已认证 Control Channel 下发：

- tunnel_id；
- one-time connection credential；
- expires_at。

credential：

- 与 runner_id + tunnel_id 绑定；
- 短期；
- 单次连接使用；
- 不写普通日志。

外部访问复用平台现有登录 Session。Central/Gateway 从 Session 解析 User，按 Tunnel.project_id 校验当前 Project Owner，并检查 Tunnel/项目生命周期；每个 HTTP 请求和 WebSocket upgrade 都走相同正式授权端口。系统管理员身份不能绕过 Owner，不能以 URL 或 Runner 建连 credential 代替用户授权，也不新增访客账号体系。

退出、过期、改密/重置撤销等沿用[账号生命周期](../platform-infrastructure/authentication/account-lifecycle.md#5-登录与-web-session)，已建立 WebSocket 也须响应 Session 失效；持续连接不能延长会话期限。Tunnel 自身 TTL、关闭和断连规则独立生效。D07/D08/D17/D25 明确路由、长连接撤销与生命周期竞争，不依赖缓存的旧授权无限访问。

平台 Session/Cookie、CSRF 标识与内部 Tunnel 建连凭据不得透传给被代理本地服务。D17 固定网页 origin/cookie/响应隔离及代理头处理，防止本地服务获得平台认证信息；Runner 不维护另一套账户权限系统。

## 18. TTL

每个 Tunnel 必须有 TTL。

到期：

~~~text
active
-> expired
-> public endpoint disabled
-> tunnel channel closed
~~~

第一阶段不提供永久无限期 Tunnel。

如需要继续使用，由上层重新创建或显式续期。

## 19. Runner Disconnect

Runner Control / Tunnel connection 断开后：

- public endpoint 不继续转发到未知状态 Runner；
- TunnelSession 进入不可服务状态并关闭 / failed；
- 用户请求得到明确 unavailable；
- Runner 重连后第一阶段不自动恢复旧 TunnelSession。

需要继续暴露时重新创建 Tunnel。

## 20. Tunnel Close

关闭来源：

- TTL 到期；
- 用户 / Agent 显式 close；
- creator workflow cleanup；
- bound Managed Process 已退出；
- Runner disconnect；
- Central 删除 session。

关闭 Tunnel：

- 关闭 public routing；
- 关闭 Tunnel Channel；
- 不删除 workspace；
- 不自动删除文件；
- 默认不 stop Managed Process。

## 21. Public Endpoint

Central 返回短期 public endpoint metadata。

Tool Result 可以包含：

~~~text
TunnelResult
├── tunnel_id
├── url
├── expires_at
└── status
~~~

URL 属于当前 TunnelSession 的外部入口，由 Central 生成并管理；“public endpoint”只表示可路由地址，仍要求当前 Owner 的平台 Session，不代表匿名公开访问。

不要把 Central 内部 tunnel credential 暴露给模型或用户。

## 22. Audit

正式 Audit 在 Central。

至少记录：

- Agent Execution；
- Tool Operation；
- Runner；
- workspace；
- local port；
- tunnel_id；
- public endpoint identity；
- TTL；
- create / close outcome。

不记录 Tunnel 内全部 HTTP content 作为默认 Audit。

## 23. Secret 与 Desktop / Tunnel

Desktop operation 不应自动读取 Project Secret Store。

只有具体 Tool Operation 明确需要 environment / credential 时，才沿用 Central 的现有 Secret resolution。

Tunnel 本身只代理本地服务，不注入 Project Secret。

本地服务如果需要 Secret，应在其 Managed Process 创建时由 Execution Runtime 注入。

## 24. 与 Tool System 的边界

Tool System 负责：

- Desktop / Tunnel ToolSpec；
- Agent Capability；
- Authorization / Approval；
- Tool Operation；
- result / error normalization。

Runner 负责：

- 实际 capability detection；
- 本地 desktop backend；
- screenshot / input；
- tunnel outbound connection；
- local port forwarding。

## 25. 第一阶段实现边界

第一阶段实现：

1. Desktop capability probe；
2. screenshot；
3. automation extension boundary；
4. browser-use / computer-use 可选 capability；
5. Desktop binary Data Channel integration；
6. expose-port；
7. Central HTTPS public endpoint；
8. HTTP + WebSocket forwarding；
9. optional Managed Process binding；
10. TTL；
11. outbound Tunnel Channel；
12. Runner disconnect closes tunnel；
13. no automatic tunnel recovery；
14. Central Audit correlation；
15. macOS 原生桌面 / Linux X11 / Wayland 分别适配与真实权限/动作验证；
16. Owner Session 的逐请求/upgrade 授权、已建 WebSocket 撤销及平台凭据隔离。

第一阶段不要求：

- raw public TCP tunnel；
- permanent tunnel；
- independent Runner plugin system；
- Runner-local account / ACL system；
- tunnel content full capture。
