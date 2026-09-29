# Execution Runtime 详细设计

> 上层架构：[Runner 架构](./README.md)
>
> 相关详细设计：
> - [Agent Workspace](./agent-workspace.md)
> - [Control Protocol](./control-protocol.md)
> - [Data Channel](./data-channel.md)
> - [项目变量与 Secret](../project-work-management/project-environment-variables.md)

## 1. 设计范围

本文定义 Runner 本地真正执行 Tool Backend operation 的运行时。

主要覆盖：

- Filesystem；
- Command；
- stdout / stderr streaming；
- environment injection；
- cancellation；
- Managed Process；
- ring buffer；
- execution-scoped / persistent process；
- Secret masking；
- runtime error mapping。

本文不定义：

- Agent Tool authorization；
- retry policy；
- Tool Operation persistence；
- Data Channel wire protocol；
- Desktop automation；
- Tunnel forwarding。

## 2. Runtime 结构

~~~mermaid
flowchart TB
    Protocol["Control Protocol"]
    Router["Operation Router"]
    Workspace["Workspace Resolver"]

    FS["Filesystem Runtime"]
    Command["Command Runtime"]
    Process["Managed Process Runtime"]
    Transfer["Transfer Runtime"]

    Mask["Secret Masking / Safe Result"]
    Stream["stdout / stderr Stream"]

    Protocol --> Router
    Router --> Workspace

    Workspace --> FS
    Workspace --> Command
    Workspace --> Process
    Workspace --> Transfer

    Command --> Stream
    Process --> Stream

    FS --> Mask
    Command --> Mask
    Process --> Mask
    Transfer --> Mask

    Mask --> Protocol
    Stream --> Protocol
~~~

Runner 根据 `operation` 路由到对应 runtime handler。

Runner 不理解上层 Task / Meeting / Agent Loop 业务。

## 3. Capability Registry

Runner 启动时探测实际能力。

第一阶段 Runtime Capability：

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

Capability 只描述当前 Runner 实际能不能提供某种运行能力。

它不是权限配置。

Central 记录 capability metadata，并在具体 Tool Call 路由到某个 Mount / Runner 时校验目标设备是否支持对应 operation；不维护统一 Tool available / unavailable 状态。

## 4. Filesystem Runtime

第一阶段 Filesystem operation：

- `read-file`；
- `write-file`；
- `edit-file`；
- `list-directory`；
- `grep-files`；
- `find-files`。

所有 path 都是 workspace-relative path。

统一流程：

1. 根据 request 中的 project / agent / workspace 定位 workspace root；
2. 校验 workspace identity；
3. 校验 relative path；
4. canonical resolve；
5. 防止 symlink escape；
6. 执行 filesystem operation；
7. 返回安全结果。

Runner 不接受 Tool 请求直接指定任意 host absolute path。

## 5. read-file

`read-file`：

- 只读取普通文件；
- target 必须仍位于 workspace；
- 支持 offset / length 等受限读取；
- 对 binary 文件不强制转 UTF-8；
- 大型 / binary content 可以转 Data Channel / file reference，而不是塞入 Control Channel。

如果调用方请求文本模式但内容不是有效文本，应返回明确类型错误，不静默损坏数据。

## 6. write-file

`write-file` 写入 workspace 内目标。

建议流程：

~~~text
write temporary file
-> flush
-> atomic rename
~~~

避免中断时留下部分写入的目标文件。

是否允许 overwrite 由 Tool arguments 明确表达。

Runner 不通过本地权限配置再次决定某个 Agent 能否写；Authorization 已由 Central 完成。

## 7. edit-file

`edit-file` 用于结构化修改现有文本文件。

具体 patch schema 属于 Runner Tool definition。

Runtime 至少保证：

- target path containment；
- expected revision / hash 可选 optimistic check；
- patch conflict 返回明确 error；
- 写入采用安全替换，避免部分文件。

编辑失败不能留下半应用状态。

## 8. list / grep / find

### list-directory

只列举 workspace 内目录。

### grep-files

用于内容搜索。

实现可以使用 ripgrep 或等价 backend，但 Runner Tool contract 不依赖具体 binary。

### find-files

按路径 / 文件名 / glob 查找文件。

搜索过程仍需：

- 不越过 workspace root；
- symlink traversal 不逃逸；
- 结果返回 workspace-relative path。

第一阶段不增加 LSP / symbol index。

## 9. Command Runtime

`run-command` 支持两种显式模式：

~~~text
argv
shell
~~~

## 10. argv mode

argv mode 概念：

~~~text
program
args[]
cwd
environment
~~~

Runner 直接执行 program + args，不经过 shell 字符串解析。

适用于：

- 普通 CLI；
- 参数边界需要明确的命令；
- 不需要 pipe / redirect / expansion 的调用。

## 11. shell mode

shell mode 接收 command string，并通过当前目标平台约定 shell 执行。

用于：

- pipeline；
- redirect；
- shell expansion；
- compound command；
- shell-specific syntax。

Runner 应在 capability / platform metadata 中暴露必要的 shell environment 信息。

第一阶段不尝试把 shell command 静态解析成权限规则。

## 12. Command Working Directory

所有 command 必须指定或解析到当前 Agent workspace 内 cwd。

Runner 校验：

~~~text
cwd canonical path
is under
workspace root
~~~

但第一阶段不限制 command 进程本身访问 workspace 外 host path。

这是明确的 trusted-host 模式。

## 13. Trusted-host 边界

第一阶段 Command Runtime 不提供：

- container sandbox；
- chroot；
- namespace isolation；
- seccomp policy；
- per-Agent OS account。

因此：

~~~text
Filesystem Tool
= strict workspace path boundary

Command Tool
= workspace cwd boundary
+ Runner OS user permissions
~~~

Agent 如果通过 shell 主动访问：

~~~text
/etc
/home/other-readable-path
network
local services
~~~

最终由 Runner 宿主机当前 OS user 的真实权限决定。

系统 UI / 文档必须把 Mount Runner 视为一个设备信任决策。

## 14. Environment Injection

Command / Managed Process 可以收到 Central 解析后的 environment：

~~~text
environment
├── variables
└── secrets
~~~

来源：

- Project normal variables；
- 当前 Agent allowlist 允许的 Secret Variables。

Runner：

1. 在 child process 创建前把 environment 合并到 process env；
2. 不保存到 Runner persistent config；
3. 不写普通 protocol log；
4. 不把 Secret value 写入 process metadata；
5. request / process lifecycle 结束后释放内存引用。

完整 Source of Truth 见 [项目变量与 Secret](../project-work-management/project-environment-variables.md)。

## 15. stdout / stderr Streaming

`run-command` 启动后实时读取 stdout / stderr。

Runner 通过 Control Protocol 发送：

~~~text
stream
├── request_id
├── stdout / stderr
├── sequence
└── text data
~~~

terminal response 最终返回：

- exit code；
- outcome；
- duration；
- summary metadata。

大规模 binary stdout 不应无限塞入 JSON stream。

需要 binary / large payload 时转 Data Channel 或 StoredObject / Artifact 路径。

## 16. Secret Masking

Runner 知道当前 request 注入过哪些 Secret plaintext。

因此 stdout / stderr 在发回 Central 前，对已知 Secret value 做直接 masking：

~~~text
secret-value
-> ***
~~~

最终 response 中的安全文本同样执行 masking。

Central 在写 Execution Log 或回填 Model Context 前仍执行自己的 masking。

Masking 不是 DLP。

例如对 Secret 做：

- base64；
- hash；
- 分片；
- 变形；

可能绕过简单字符串 masking。

安全语义仍然是：

> Agent Secret allowlist 表示允许该 Agent 的 process 实际使用 Secret。

## 17. Command Cancellation

Runner 为每个 active command 保存 cancellation handle。

收到 cancel 或 deadline 后：

1. 请求 graceful termination；
2. 等待 grace period；
3. 如果仍运行，force kill；
4. 收集可确认 exit / termination status；
5. 返回 cancelled / failure / unknown。

具体 OS signal / API 由平台实现适配。

第一阶段不要求所有平台完全使用相同 signal。

## 18. Child Process Tree

取消 command 时应尽可能终止其 process group / child tree，而不是只终止最外层 shell。

否则：

~~~text
shell stopped
child server remains
~~~

会产生不可控后台进程。

实现应使用当前 OS 可用的 process group / job object 等机制。

如果无法确认 child tree 已终止，应在结果 metadata 中保留相应诊断信息。

## 19. Managed Process

Managed Process 用于跨多次 Tool Call 持续存在的进程，例如：

- dev server；
- test watcher；
- local service；
- debugger backend。

概念实体：

~~~text
ManagedProcess
├── process_id
├── runner_id
├── project_id
├── agent_id
├── workspace
├── creator_execution_id
├── scope
│   ├── execution
│   └── persistent
├── command
├── cwd
├── state
├── pid/runtime_handle
├── started_at
├── exited_at?
├── exit_code?
└── output_buffer
~~~

## 20. Managed Process State

第一阶段：

~~~text
starting
running
exited
failed
stopped
~~~

`start-process` 成功的含义是：

> Runner 已经创建并登记 Managed Process。

不表示该进程未来一定持续健康。

## 21. execution-scoped Process

默认：

~~~text
scope = execution
~~~

当 creator Agent Execution 进入终态时，Central 应对其 active execution-scoped Managed Process 发起 stop。

Runner 自己不运行 Agent Execution 状态机。

因此 cleanup orchestration 仍在 Central。

如果 Central / Runner 在 cleanup 时失联，可能存在短暂残留 process；连接恢复后的实际操作再根据 process result 更新状态。

第一阶段不额外引入 Runner 自主 execution lease 系统。

## 22. persistent Process

显式：

~~~text
scope = persistent
~~~

表示进程不随着 creator Agent Execution 正常结束自动 stop。

它仍绑定：

- Runner；
- Project；
- Agent；
- workspace；
- creator execution metadata。

persistent 只表示：

> 生命周期可以跨 Agent Execution。

因此 `scope = persistent` 必须作为 Tool Authorization / Approval Scope 的显式参数参与授权，不能被仅覆盖 `scope = execution` 的 Reusable Approval 自动扩大。默认审批规则中 persistent 额外标记 `security_sensitive`。完整规则见 [默认审批策略](../security-governance/default-approval-policy.md) 与 [Approval Scope](../security-governance/approval-scope.md)。

它不表示：

- 可以跨 Runner clean shutdown；
- 可以跨 Runner restart；
- 可以跨设备 reboot；
- OS crash 后自动恢复。

Runner clean shutdown 时，execution-scoped 与 persistent Managed Process 都必须被终止。

第一阶段不实现 process supervisor 自动重启。

## 23. Process Output Ring Buffer

Runner 为每个 Managed Process 维护有界 stdout / stderr ring buffer。

读取：

~~~text
read-process-output(process_id, cursor)
~~~

返回：

~~~text
ProcessOutput
├── next_cursor
├── stdout[]
├── stderr[]
├── truncated_before_cursor
└── process_state
~~~

如果旧 output 已从 ring buffer 淘汰：

~~~text
truncated_before_cursor = true
~~~

调用方不能误认为返回内容完整。

## 24. Process Cursor

cursor 是 Runner 本地 process output sequence position。

要求：

- 单调；
- 不等同字节 offset 的具体实现；
- 同一 process 生命周期内稳定；
- Runner restart 后不保证继续有效。

Central 需要长期保留的输出应写 Execution Log / Object Storage，而不是依赖 Runner ring buffer。

## 25. get-process

`get-process` 返回当前已知：

- process identity；
- state；
- timing；
- exit code；
- scope；
- workspace；
- safe command metadata。

不返回：

- Secret environment；
- private process environment dump。

## 26. stop-process

`stop-process` 使用与 command cancellation 相同的：

~~~text
graceful
-> grace period
-> force kill
~~~

Process 已退出：

- 返回当前 terminal state；
- 不把它当成 protocol failure。

不存在：

- `PROCESS_NOT_FOUND`。

## 27. Runner Reconnect 与 Managed Process

第一阶段重连时不发送完整 process snapshot 进行自动 reconcile。

Central 保留已有 process metadata。

之后调用：

- get-process；
- read-process-output；
- stop-process；

如果 Runner 返回 process not found / exited，则 Central 更新记录。

这种 lazy reconciliation 比重连时同步整张 process table 更简单。

## 28. Runner Restart

正常 restart 应先执行 clean shutdown，因此 Runner 管理的 Managed Process 都会被终止。

如果 Runner 因 crash、kill -9、设备掉电等原因异常退出：

- 不承诺恢复 Managed Process registry；
- 不承诺 persistent process 自动重挂；
- 旧 process cursor 失效；
- Central 后续查询可能收到 `PROCESS_NOT_FOUND`；
- 即使 OS 中旧 child process 因异常退出而残留，Runner 也不能仅凭 PID 猜测它就是原 Managed Process。

未来如需要跨 Runner restart process persistence，应单独设计 supervisor / durable process identity。

## 29. Transfer Runtime

Runner Transfer Capability 的实际 binary payload 使用 Data Channel。

Execution Runtime 只负责：

- workspace source / target path；
- file open；
- temporary file；
- checksum；
- atomic completion；
- 将 transfer outcome 映射回 Runner response。

完整 channel lifecycle 见 [Data Channel](./data-channel.md)。

## 30. Error Mapping

Runtime error 应映射成稳定 code，例如：

~~~text
WORKSPACE_NOT_FOUND
PATH_OUTSIDE_WORKSPACE
NOT_FOUND
CONFLICT

PROCESS_NOT_FOUND
PROCESS_ALREADY_EXITED

TIMEOUT
CANCELLED
CAPABILITY_UNAVAILABLE
INTERNAL_ERROR
~~~

原始 OS exception：

- 可以进入脱敏诊断日志；
- 不直接作为模型可见 safe_message。

## 31. 本地资源限制

第一阶段 Runner 不提供管理员可配置的第二套 resource policy。

Central 可以在 request 中下发：

- deadline；
- max output；
- max file size；
- transfer size；
- cancellation grace period。

Runner 按 request 执行。

实际 OS：

- disk full；
- OOM；
- process limit；
- permission error；

作为运行错误返回。

## 32. 第一阶段实现边界

第一阶段实现：

1. Filesystem read/write/edit/list/grep/find；
2. strict workspace path containment；
3. argv + shell command；
4. trusted-host command semantics；
5. Central-provided environment；
6. stdout/stderr live stream；
7. Secret masking；
8. graceful -> force cancellation；
9. process tree termination best effort；
10. Managed Process；
11. execution / persistent scope；
12. bounded ring buffer + cursor；
13. lazy process reconciliation；
14. no durable process recovery across Runner restart；
15. Data Channel integration。
