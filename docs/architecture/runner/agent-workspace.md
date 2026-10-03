# Agent Workspace 详细设计

> 上层架构：[Runner 架构](./README.md)
>
> 相关架构：
> - [Agent 管理架构](../agent-management.md)
> - [Agent Executor](../agent-executor/README.md)
>
> 相关详细设计：
> - [Execution Runtime](./execution-runtime.md)
> - [项目变量与 Secret](../project-work-management/project-environment-variables.md)

## 1. 设计范围

本文定义 Agent 如何通过 Runner Mount 获得远端设备上的 workspace。

主要覆盖：

- Runner / Agent / Project 的关系；
- Agent Mount；
- workspace logical name；
- physical path mapping；
- workspace create / remove；
- filesystem path resolution；
- symlink / traversal；
- command working directory；
- trusted-host 安全边界；
- Mount 配置变化；
- AgentExecutionContext 中的 workspace metadata。

## 2. 核心关系

Runner 是系统级设备资源。

Runner 不属于某个 Project，也不直接“服务于 Project”。

真正的资源关系是：

~~~text
Runner
  ↓
Agent Mount
  ↓
Agent Workspace
  ↓
Agent 获得设备能力
~~~

Project 只参与 workspace 的业务归属和物理路径命名。

## 3. Agent Mount

Agent Mount 是 Agent 配置的一部分。

概念：

~~~text
AgentMount
├── id
├── agent_id
├── runner_id
├── name
├── description?
└── workspace
~~~

其中：

- `name`：模型看到的逻辑 mount name，例如 `source`、`mac`；
- `workspace`：Runner 上该 Agent 的逻辑 workspace 目录名；
- `runner_id`：目标系统级 Runner。

Agent 可以：

- 在不同 Runner 上配置多个 Mount；
- 在同一个 Runner 上配置多个不同 workspace；
- 给多个 workspace 使用不同逻辑 mount name。

## 4. root-path

每个 Runner 创建时配置固定 `root-path`。

例如：

~~~text
/workspace
~~~

Agent Mount 不允许配置任意绝对路径。

Agent 只配置逻辑 workspace：

~~~text
source
docs
frontend
mac
~~~

## 5. Physical Path Mapping

实际 workspace path 固定由系统生成：

~~~text
<root-path>/<project-id>/<agent-id>/<workspace>
~~~

示例：

~~~text
root-path = /workspace
project-id = prj_123
agent-id = agt_456
workspace = source

physical path =
/workspace/prj_123/agt_456/source
~~~

物理路径建议使用稳定 Project ID / Agent ID，而不是 display name。

原因：

- Project rename 不导致目录迁移；
- Agent rename 不导致目录迁移；
- display name 不参与路径安全语义；
- identity 映射稳定。

## 6. Workspace Name

`workspace` 是逻辑目录名，不是路径。

第一阶段必须拒绝：

- 空字符串；
- `.`；
- `..`；
- 绝对路径；
- path separator；
- NUL；
- 平台非法目录字符；
- 规范化后发生变化的路径表达；
- 可以逃逸当前 Agent directory 的值。

推荐约束为单个安全 path segment。

例如允许：

~~~text
source
frontend
repo-1
mac_debug
~~~

不允许：

~~~text
../source
foo/bar
/tmp/source
C:\source
~~~

## 7. Workspace 创建

Agent Mount 被创建或第一次实际使用时，Central 可以请求 Runner 确保 physical workspace directory 存在。

概念操作：

~~~text
ensure_workspace(
  project_id,
  agent_id,
  workspace
)
~~~

Runner：

1. 根据本地 root-path 计算 physical path；
2. 校验 path 仍位于 root-path；
3. 创建缺失目录；
4. 返回 canonical workspace metadata。

Runner 不负责：

- git clone；
- git checkout；
- git worktree；
- repository initialization；
- dependency installation。

这些如果未来自动化，应作为独立 Workspace Provisioning 能力。

### 7.1 技能版本包准备

技能文件准备是独立的正式能力，不是创建 Workspace 时隐式执行安装脚本。Central 经 Skill/Agent/Execution/Mount 端口校验当前资源与权限，并确定本次绑定的不可变 revision；Runner 按需直接从对象存储获取该包，在当前 Agent Workspace 内建立平台管理的版本目录。

版本目录保留包内相对关系，与普通工作文件分开；具体相对路径、保留名称及重复准备校验由 D16/D17 固定，不接受上传包指定宿主机绝对路径。临时接收、大小/摘要校验和受控解包成功后原子发布，拒绝越界、链接、特殊文件与路径碰撞；取消/断连/磁盘不足不把半套包当成功。

返回 Skill revision、Mount identity 和工作区相对路径，不返回宿主机绝对路径或对象凭据。多个 Runner/Mount 各自准备同一固定版本，单 Agent 非终态 Execution 唯一约束保留。读取纯说明由 Central 完成，无需 Runner 在线。

准备不执行脚本、不隐式安装依赖；实际命令继续走授权/审批与 trusted-host 边界。移除技能不主动收回已读内容或工作文件；缓存清理只处理已验证归属、没有活动使用的版本，不递归删除用户文件或共享挂载。Runner 离线或清理未知不报告已完成。业务版本与授权见 [Agent Skills](../agent-skills.md)。

## 8. Workspace 删除

删除 Agent Mount 默认只删除 Central 中的配置关系。

第一阶段不自动删除 Runner 上的物理目录。

原因：

- workspace 可能包含未提交代码；
- 目录删除是高风险不可逆操作；
- 一个 workspace 的生命周期可能长于某次 Mount 配置；
- 自动清理会引入额外 ownership / retention 规则。

如果未来需要清理，应提供独立显式操作，而不是 Mount delete 的隐式副作用。

## 9. Execution Context Projection

Agent Executor 在 preparing 时把当前 Execution 可见 Mount snapshot 固化到 AgentExecutionContext。

模型需要看到的 metadata 至少包括：

~~~text
MountContext
├── mount_id
├── mount_name
├── mount_description?
├── runner_id
├── runner_name
├── runner_description?
├── headless
├── capabilities[]
└── workspace_logical_path
~~~

不注入：

- Runner private key；
- Control Channel detail；
- physical host credential；
- Central connection token。

模型使用 mount name 选择目标环境，不需要知道完整 host filesystem topology。

## 10. Filesystem Path Resolution

Runner Filesystem operation 不接受任意 host absolute path。

请求使用：

~~~text
workspace identity
+ workspace-relative path
~~~

例如：

~~~text
mount = source
path = src/main.ts
~~~

Runner resolve：

~~~text
/root/prj/agent/source
+ src/main.ts
↓
/root/prj/agent/source/src/main.ts
~~~

校验流程：

1. 获取当前 workspace physical root；
2. 拒绝 absolute path；
3. normalize relative path；
4. resolve canonical target；
5. 确认 canonical target 位于 workspace root；
6. 才执行 filesystem operation。

## 11. Symlink

Filesystem Tool 必须防止通过 symlink 逃逸 workspace。

例如：

~~~text
workspace/link -> /etc
read-file link/passwd
~~~

必须拒绝。

因此读写实际 target 前，需要校验 canonical resolved target 仍位于 workspace root。

对于创建 symlink，本轮 Runner Tool Set 没有专用 symlink Tool。

如果 Agent 通过 shell command 自行创建 symlink，Filesystem Tool 后续仍必须按上述 canonical path 规则阻止越界访问。

## 12. Command Working Directory

`run-command` 的 cwd 使用 workspace-relative path。

Runner 必须保证 cwd：

- 属于当前 workspace；
- resolve 后没有逃逸；
- 目录实际存在。

但第一阶段只限制 cwd，不限制 shell command 本身可以访问哪些宿主机路径。

例如：

~~~text
cwd = workspace root
command = cat /etc/hosts
~~~

在宿主机用户本身有权限时，第一阶段允许执行。

## 13. Trusted-host 安全语义

第一阶段不引入：

- container；
- namespace sandbox；
- chroot；
- per-agent OS account；
- mandatory filesystem jail。

因此：

> 将 Agent Mount 到某个 Runner，意味着信任该 Agent 可以通过 Runner 当前宿主机用户的 OS 权限执行命令。

Filesystem Tool 本身仍严格 workspace-scoped。

Command Tool 的安全边界不同：

~~~text
Filesystem Tool
-> workspace-contained

Command Tool
-> cwd workspace-contained
-> process OS access = Runner user access
~~~

这个差异必须在 UI / 文档中保持清楚。

## 14. Mount / Workspace 配置变化

Mount 配置变化只影响后续 operation。

已经开始的 Tool Operation：

- 不因为 Mount 被删除而主动 cancel；
- 不因为 workspace name 更新而迁移；
- 不因为 Agent 配置变化而中断。

新的 operation 必须使用最新有效 Mount 配置。

如果一个 active Agent Execution 使用的是 immutable Mount snapshot，而 Central 已经删除对应 Mount，则 Tool Runtime 在每次实际调用阶段仍应执行当前服务端有效性检查，并可以拒绝新的操作。

## 15. Runner Capability 与 Mount

Mount 自身不维护一套本地 capability 权限。

Runner 实际 Capability 来自 Runner hello。

Agent Capability / Execution Policy 决定当前 Execution 可以使用哪些 Runner Tools。

因此有效能力大致为：

~~~text
Runner actual capability
∩ Agent Capability
∩ Execution Policy
∩ current server-side authorization
~~~

这个集合由 Central 计算。

Runner 不维护平行 allowlist。

## 16. Environment Variable 与 Workspace

Project Environment Variable / Secret 不属于 workspace 持久配置。

Command / Process operation 执行时：

1. Central 根据 Project + Agent Secret 白名单解析 environment；
2. 通过本次 Runner RPC 临时下发；
3. Runner 把 environment 注入对应 child process；
4. Runner 不把 Secret 写入 workspace metadata；
5. Runner 不把 Project Secret 持久化到本地 config。

详细规则见 [项目变量与 Secret](../project-work-management/project-environment-variables.md)。

## 17. 与 Tool System 的边界

统一 Tool System 暴露：

- Filesystem Tools；
- Command；
- Managed Process；
- Transfer；
- Desktop；
- Tunnel。

Agent Workspace 只定义这些 Tool 在哪个逻辑 workspace 上工作。

它不决定：

- Tool 是否出现在模型可见 Tool Set；
- 是否需要 Approval；
- Tool retry；
- Tool concurrency；
- Tool Operation lifecycle。

这些继续由 Unified Tool Runtime 负责。

## 18. 第一阶段实现边界

第一阶段确定：

1. AgentMount；
2. stable runner_id；
3. mount name；
4. workspace logical name；
5. `<root>/<project-id>/<agent-id>/<workspace>` path mapping；
6. safe single-segment workspace name；
7. ensure workspace；
8. Mount 删除不自动删除物理目录；
9. filesystem canonical path containment；
10. symlink escape prevention；
11. command cwd containment；
12. trusted-host command semantics；
13. Mount 变化只影响后续请求；
14. immutable Execution mount snapshot + runtime current validation；
15. Skill 固定 revision 的托管目录、完整性校验及受控清理，精确路径与真实传输由 D16/D17/D21 绑定。
