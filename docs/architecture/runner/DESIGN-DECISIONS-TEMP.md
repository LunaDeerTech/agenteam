# Runner 设计待确认项（临时）

> 状态：临时讨论文档
>
> 目的：记录 Runner 详细设计前已经确认的决策。正式设计已经据此拆分并写入对应详细设计文档。
>
> 说明：
> - 已确认事项只保留最终结论，不再保留原候选选项；
> - 已经在 Tool Runtime、Approval / Idempotency、Project Environment Variables 等模块明确的通用规则直接复用，不在 Runner 内重新定义；
> - 本文仍是讨论记录，待全部关键事项确定后再据此拆分并完善正式 Runner 设计文档。

1. **Runner 的职责边界 — 已确定**

   Runner 只作为远程资源执行节点，提供文件、命令、Managed Process、文件传输、Desktop 等设备能力。

   Task Scheduler、Agent Executor、Agent Loop、Task / Meeting、权限决策、审计等业务与控制逻辑都属于 Central。

   Runner 不运行完整的 agenteam 协作逻辑。

2. **Runner 是否维护独立权限配置 — 已确定**

   Runner 本身不维护一套独立的权限策略。

   Runner 本地只需要保存用于连接 Central 和完成设备身份认证的必要配置。除连接认证配置外，不再额外设计：

   - 本地 Tool allowlist；
   - 本地 capability 权限策略；
   - 本地 mount 授权策略；
   - 本地 operation policy；
   - 与 Central 重复的 Approval / Authorization 配置。

   Runner 的行为全部由 Central 发出的已授权请求驱动。

   权限判断的 Source of Truth 在 Central。Runner 只负责：

   - 校验协议请求本身合法；
   - 校验请求引用的 workspace / path 是否符合 Runner 的目录映射规则；
   - 按请求执行对应 operation；
   - 返回执行结果。

   因此 Runner 不形成第二套权限系统，避免 Central 与 Runner 两边策略漂移。

3. **Runner 与 Project / Agent 的关系 — 已确定**

   Runner 是系统级设备资源，但它不是“服务于 Project”的资源。

   Runner 通过 Agent Mount 为 Agent 提供运行环境和设备能力。

   关系应理解为：

   ```text
   System Runner
        ↓
   Agent Mount
        ↓
   Agent 获得该设备上的 workspace / capability
   ```

   Project 只参与 workspace 的路径命名与资源归属，不直接拥有 Runner。

4. **root-path 与 Agent Workspace 映射 — 已确定**

   创建 Runner 时指定一个固定 `root-path`。

   Agent 挂载 Runner 时不填写任意绝对路径，而只填写自己的 workspace 目录名。

   实际路径由系统统一映射：

   ```text
   <runner-root>/<project>/<agent>/<workspace>
   ```

   例如：

   ```text
   runner root-path = /workspace

   Project A
   Agent X
   workspace = 11

   -> /workspace/A/X/11

   workspace = 22

   -> /workspace/A/X/22
   ```

   因此同一个 Agent 可以在同一个 Runner 上挂载多个 workspace。

   Central 负责保存 Project、Agent、workspace 的逻辑关系；Runner 根据请求中的已解析身份定位实际目录。

   workspace name 必须使用安全的逻辑目录名，不能允许：

   - 绝对路径；
   - `..`；
   - 路径分隔符注入；
   - 其他可以逃逸 `<root>/<project>/<agent>` 的路径表达。

5. **run-command 的宿主机隔离语义 — 已确定**

   第一阶段直接在 Runner 宿主机当前运行用户权限下执行命令。

   Runner 只保证 command 的工作目录位于当前 Agent workspace 内。

   第一阶段不承诺 shell command 无法主动访问 workspace 外的宿主机路径，也不引入容器 / sandbox。

   因此第一阶段的信任边界是：

   > 将 Agent 挂载到某个 Runner，意味着允许该 Agent 通过该 Runner 的宿主机用户权限执行命令。

6. **Workspace 是否由 Runner 自动隔离 — 已确定**

   Runner 只操作已经映射好的物理 workspace。

   第一阶段不自动：

   - clone repository；
   - 创建 Git worktree；
   - 创建 per-execution workspace；
   - 管理 workspace lifecycle。

   如未来需要自动准备代码工作区，应作为独立 Workspace Provisioning 能力设计，而不是隐式加入 Runner Core。

7. **Runner Capability 来源 — 已确定**

   Runner Capability 以 Runner 实际探测到的设备能力为准。

   Central 不额外维护一套 capability allowlist。

   Runner 在 hello / status synchronization 中把当前实际能力上报给 Central，Central 将其作为目标设备 runtime metadata 使用。

   Unified Tool Registry 不为 Runner Tool 维护 available / unavailable 状态。`runner:<tool_name>` 是平台稳定 Tool definition；真正调用时根据 Agent Mount 解析到具体 Runner / workspace，再检查当前设备连接和实际 Capability。Runner offline 或缺少对应 Capability 时直接返回执行错误，不影响其他 Runner 上的同名 Tool 能力。

8. **Code Navigation / LSP — 已确定**

   第一阶段不实现 LSP / Code Navigation Capability。

   当前统一 Tool System 已经确定的 Runner Tools 只有：

   ```text
   Filesystem
   - read-file
   - write-file
   - edit-file
   - list-directory
   - grep-files
   - find-files

   Command
   - run-command

   Managed Process
   - start-process
   - get-process / read-process-output
   - stop-process

   Transfer
   - file-upload
   - file-download
   - file-transfer

   Desktop
   - screenshot
   - automation
   - browser-use
   - computer-use

   Tunnel
   - expose-port
   ```

   Runner 详细设计与 Tool System 保持一致，不在 Runner 层单独新增 LSP Tool。

9. **LSP 配置方式 — 当前不适用**

   因第 8 项已经确定第一阶段不做 LSP，因此不设计 Language Server 的安装、探测、配置和生命周期。

   如果未来 Tool System 正式增加 Code Navigation Tools，再单独讨论：

   - Language Server discovery；
   - per-language server configuration；
   - process lifecycle；
   - workspace initialization；
   - symbol / reference result normalization。

10. **Control Channel 传输协议 — 已确定**

    第一阶段使用：

    ```text
    WebSocket over TLS (WSS)
    + JSON Envelope
    ```

    Runner 主动向 Central 建立出站连接。

11. **Runner 长期身份认证 — 已确定**

    Runner 首次 Enrollment 时在本地生成 Ed25519 key pair。

    后续连接通过 Central challenge + Runner signature 完成设备身份认证。

    不使用长期 bearer token，也不要求第一阶段使用 mTLS client certificate。

12. **Runner private key 保存 — 已确定**

    第一阶段直接保存在 Runner 本地配置文件中。

    文件权限应限制为 Runner 运行用户可读。

    第一阶段不额外集成系统 Keychain / Credential Store。

13. **同一个 runner_id 的连接数量 — 已确定**

    同一时刻只允许一个有效 Control Channel。

    新连接认证成功后替换旧连接。

14. **Protocol Version 兼容策略 — 已确定**

    Major version 不兼容时直接拒绝连接。

    Minor version 的差异通过 hello 中的 capability / feature negotiation 处理。

15. **Heartbeat 参数 — 已确定**

    Runner 有合理本地默认值用于 bootstrap。

    Control Channel 建立后，由 Central 下发 heartbeat interval、timeout 等运行参数。

16. **断线后的 pending RPC — 已确定**

    Control Channel 断开后，Central 对无法确认最终结果的 Tool Attempt 标记为 `unknown`。

    Runner Protocol 层不自动重放 RPC。

    后续是否 technical retry，继续由 Unified Tool Runtime 根据：

    - outcome；
    - idempotency；
    - Tool retry policy；

    统一决定。

17. **Runner Protocol RPC 方向 — 已确定**

    业务 Operation 只由：

    ```text
    Central -> Runner
    ```

    发起。

    Runner -> Central 只发送：

    - protocol event；
    - status；
    - heartbeat；
    - process / stream event；
    - response。

    Runner 不主动调用 Central 的业务 RPC。

18. **持久化 RPC Journal — 已确定**

    第一阶段不实现通用持久化 RPC Journal。

    Runner 不为了恢复 RPC outcome 而把每个 request / response 持久化到本地数据库。

    网络断开导致无法确定结果时继续使用现有 `unknown outcome` 语义。

19. **Runner 并发控制 — 已确定**

    Runner 第一阶段不增加独立的全局并发限制。

    Central / Unified Tool Runtime 决定哪些 Tool Operation 可以并行或必须串行。

    Runner 收到合法 RPC 后按请求执行，不再额外使用 Runner 级并发策略改变上层调度语义。

20. **Command / RPC Cancellation — 已确定**

    Runner 支持 cooperative cancellation。

    对 command：

    1. 先请求 graceful termination；
    2. 等待 grace period；
    3. 超时后强制终止。

    cancellation 仍需遵守分布式 unknown outcome 语义，不能把“已发送 cancel”解释成“操作一定没有产生副作用”。

21. **资源限制策略 — 已确定**

    timeout、输出限制、文件大小等运行限制由 Central 决定并随请求下发。

    Runner 第一阶段不维护另一套管理员可配置的 resource policy / hard ceiling。

    Runner 仍然会受到实际 OS、磁盘、内存、文件系统等客观资源限制，但这些不是 agenteam Runner 的第二套权限或策略配置。

22. **Managed Process 生命周期 — 已确定**

    Managed Process 默认是 execution-scoped。

    同时允许显式创建 persistent process。

    persistent process 需要通过对应 Tool 参数 / 权限语义明确表达，不能由普通 execution-scoped process 自动变成长期进程。

    `scope = persistent` 与 `scope = execution` 属于不同 Approval Scope；普通 execution-scoped 的 Reusable Approval 不能自动覆盖 persistent process。默认审批规则中 persistent 额外视为 `security_sensitive`。

23. **Managed Process 输出保存 — 已确定**

    Runner 为 stdout / stderr 维护有界 ring buffer，并使用 cursor 支持增量读取。

    Runner 不负责永久保存完整运行日志。

    需要长期保留的 Execution Log 由 Central 持久化。

24. **重连后的 Managed Process 对账 — 已确定**

    第一阶段不额外进行 Managed Process snapshot reconcile。

    Control Channel 重连后，Central 保留原有 Managed Process 状态认知。

    后续真实操作如果发现进程不存在或状态变化，再通过对应 RPC Result 更新状态。

25. **Command stdout / stderr Streaming — 已确定**

    `run-command` 执行期间实时发送 stdout / stderr stream event。

    stream event 关联当前 `request_id`。

    command 结束后再发送 terminal response。

    因此：

    ```text
    stream events
       ↓
    Runtime View / live output

    terminal response
       ↓
    Tool Attempt final outcome
    ```

26. **run-command 输入形式 — 已确定**

    同时支持：

    ```text
    argv mode
    shell mode
    ```

    调用时显式选择。

    argv mode 直接执行程序和参数。

    shell mode 用于：

    - pipeline；
    - redirect；
    - shell expansion；
    - 复合命令；
    - 其他 shell syntax。

27. **Data Channel 与文件传输 — 已确定**

    Runner 第一阶段需要正式实现独立 Data Channel。

    Data Channel 用于 Runner 与 Central 之间的大文件 / binary 数据传输。

    Runner 自身的 Transfer Capability：

    - `file-upload`；
    - `file-download`；
    - `file-transfer`；

    直接通过 Runner Data Channel 搬运文件。

    Object Storage 是另一条路径。

    当 Agent 调用平台 Object Storage / Artifact 相关能力，需要把对象实际上传到或下载到 Runner 所在设备时：

    ```text
    Control Channel
        -> 协商 metadata / operation

    Runner
        -> 直接与 Object Storage 数据端交互
        -> upload / download payload
    ```

    因此二者边界是：

    ```text
    Runner <-> Central 文件搬运
    -> Runner Data Channel

    Runner <-> Object Storage 对象上传下载
    -> Control Channel 协商
    -> Runner 直接执行对象存储数据传输
    ```

    不把 Object Storage 文件内容绕回 Control Channel。

28. **Desktop Capability 的代码组织 — 已确定**

    Desktop / Browser / Computer Use 相关能力直接实现于同一个 Runner 工程中。

    不拆成独立 Runner 插件或单独 Provider 工程。

    是否可用仍由设备实际环境决定，例如 headless Runner 不会上报 Desktop Capability。

29. **Port / Tunnel Capability — 已确定**

    Runner 第一阶段提供一等的 tunnel / expose-port capability。

    用于把 Runner 本地 Managed Process 暴露成临时可访问服务。

    网络拓扑仍保持 Runner 主动出站，不要求 Central 反向连接 Runner。

    Tunnel 至少应绑定：

    - Runner；
    - workspace；
    - target local port / Managed Process；
    - lifecycle；
    - TTL；
    - 当前 Agent Execution / Tool Operation correlation。

    `expose-port` 会改变 Runner 本地服务的网络暴露边界，默认按 `security_sensitive` 处理并要求 Approval；如果未来支持 Reusable Approval，Scope 至少绑定 Runner / Mount / Workspace 与 local port。

30. **Runner 更新方式 — 已确定**

    第一阶段由管理员显式升级 Runner。

    Central 展示：

    - runner version；
    - protocol version；
    - compatibility。

    不实现自动静默自更新，也不允许 Central 直接替换远端 Runner binary。

31. **Runner 状态模型 — 已确定**

    第一阶段只需要：

    ```text
    online
    offline
    incompatible
    ```

    Capability 的单项可用性 / 健康情况通过 capability metadata 表达，不扩展为 Runner 顶层状态。

32. **Runner Shutdown — 已确定**

    Runner 关闭时直接停止。

    第一阶段不设计 draining 状态或优雅排空协议。

    clean shutdown 时停止接收新请求，取消 active RPC，终止 active command 与全部 Runner-managed process（包括 persistent process），关闭 Data / Tunnel / Control Channel 后退出。

    persistent 只表示可以跨 Agent Execution，不表示可以跨 Runner shutdown。

    如果 Runner 因 crash、kill -9、设备掉电等原因无法完成 clean shutdown，未确认 outcome 仍按现有 cancellation / unknown outcome 规则处理。

33. **Mount / Workspace 配置变化对在途操作的影响 — 已确定**

    配置变化只影响后续请求。

    已经开始执行的 operation 继续运行，不主动取消。

    这样避免因为配置变化再引入一套在途 operation 回收流程。

34. **Runner 本地日志与系统 Audit — 已确定**

    Central Audit 是 canonical audit。

    Runner 本地只保留：

    - 脱敏后的诊断日志；
    - 连接日志；
    - 必要运行信息。

    Runner 本地日志不是正式系统 Audit。

35. **Runner Secret 处理 — 已确定**

    Runner 不维护 Project Secret Store。

    每次 command / process 只接收 Central 针对当前 operation 解析出的 environment。

    生命周期继续沿用现有 Project Environment Variables 设计：

    - Secret 不同步成 Runner 长期配置；
    - Runner 不持久缓存整个 Project Secret Store；
    - environment 只进入对应 child process；
    - stdout / stderr / Tool Result 对已知 Secret value 做 masking。

36. **正式 Runner 设计文档拆分 — 已确定**

    Runner 正式设计采用：

    ```text
    runner/
    ├── README.md
    ├── runner-management.md
    ├── agent-workspace.md
    ├── control-protocol.md
    ├── data-channel.md
    ├── execution-runtime.md
    └── desktop-tunnel.md
    ```

    职责分别为：

    - `README.md`：总体架构、模块边界和详细设计索引；
    - `runner-management.md`：Enrollment、Device Identity、版本、状态与设备生命周期；
    - `agent-workspace.md`：Agent Mount、workspace path、Filesystem 边界与 trusted-host 语义；
    - `control-protocol.md`：WSS、Envelope、RPC、stream、heartbeat、reconnect、unknown outcome；
    - `data-channel.md`：Runner 与 Central 的 binary / large-file data plane，以及 Object Storage 直连边界；
    - `execution-runtime.md`：Filesystem、Command、Managed Process、environment、Secret、cancellation；
    - `desktop-tunnel.md`：Desktop / Browser / Computer Use 与 expose-port。

    不单独创建 `runner-security.md` 或通用 `runner-capabilities.md`。安全和 capability 语义分别归入真正拥有对应 contract 的 workspace、protocol、runtime、desktop / data channel 文档。
