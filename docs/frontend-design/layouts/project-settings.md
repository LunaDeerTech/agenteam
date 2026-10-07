# 项目设置布局与栏目

> 框架：[通用设置](settings-shell.md)，保留双顶部导航。仅当前 Project Owner 可访问；默认“基本信息”。

## 1. 区域与菜单

```text
双顶部导航
悬浮侧栏                         内容区
基本信息                         项目资料表单
Agent → Agent 列表               Agent 详情和配置分区
Skills → 项目技能库              导入、版本与 Agent 分配
模型与 Provider → Providers / 可用模型
MCP → 配置目录 / Connections
变量与 Secret → Variables / Secrets
会议 → 会议配置
Scheduler → 调度配置
安全记录 → 项目审计
```

项目审计是现有 Owner 查询能力的入口，置于设置尾部；不访问 System Audit。不增加成员、角色或权限矩阵。

## 2. 基本信息

可编辑项目名称、描述；只读 Project id、Owner identity、创建与更新时间。保存 / 取消遵循通用框架，不提供更换 Owner。名称在当前 Owner 下不区分大小写唯一，标准项目地址为小写 `/{username}/{project_name}`；改名前提示旧链接失效且不提供别名或自动跳转。重名、版本冲突或保存失败保留输入。

Owner 可归档、取消归档和永久删除项目。归档确认说明会自动停止现有项目活动，完成后数据只读且可恢复；处理中显示真实进度，停止失败或未知不得伪报已归档。归档项目保持名称和地址占用，恢复后已取消执行不自动重启。

永久删除确认显示停止执行、清理项目数据且不可恢复，要求输入完整当前项目路径，例如 `alice/demo`。改名或生命周期发生并发变化时拒绝旧确认，重新读取并确认当前对象；失败保留正常输入与结果说明。删除处理中不提前移除项目，确认清理完成后才返回列表。共享挂载和外部副作用不作“一并全部清除”承诺，正式范围见 [项目生命周期](../../architecture/project-work-management/README.md#22-project-归档与永久删除)。归档后的普通设置只读，恢复/永久删除按受控生命周期入口执行。

## 3. Agent

列表优先显示 display_name，未配置时回退到标识名 name，并保留标识名、tag-color、description、model_ref、真实运行 / busy 摘要。可新建空白 Agent 或从系统 Preset 复制配置；详情用内部标签分区，不在左菜单无限生成第三级。

| 详情分区 | 字段与操作 |
| --- | --- |
| 基础配置 | 标识名 name、可选 display_name、tag-color、description、instructions、model_ref、reasoning_effort、AGENTS.md 注入策略；approval_policy 按安全架构选择 default / auto / allow；auto 时另配置 approval_model_ref，候选合法性遵循 Approval Model Contract |
| 工具与 Skills | allowed tools 与项目技能分配；工具按来源、功能、读写及风险筛选；既有 Core Agent Tools 只读标记为始终可用，普通 Skill 工具按各自授权可配置 |
| Runner 挂载 | name、runner、workspace、description；创建、编辑、移除；workspace 仅逻辑目录名 |
| Secret 授权 | allowed_secret_variables 的稳定引用，显示名称和描述，不显示明文 |
| Reusable Approvals | Tool、Scope、创建时间、来源和状态；仅查看及撤销 active 记录 |
| Memory | namespace 身份与按需访问机制说明；不增加未定义的 Memory 编辑器 |

模型候选是当前项目 enabled System chat Models 与当前项目 enabled chat Models；reasoning_effort 仅在模型支持可选等级时出现。普通变量对全部项目 Agent 可见；Secret 需显式授权。模型切换与删除替代流程必须保持 reasoning_effort 合法。

标识名在当前 Project 内唯一，允许英文字母、数字和连字符，不区分大小写，路径使用小写；可与 User username 同名。显示名不承担身份或路由作用，用户/Agent 的选择和展示明确区分类型，不靠同名文字判断权限。

Preset 模型可空，但实际 Agent 必须选定合法可用模型；失效的内置工具推荐自动跳过，可提示未复制项，不阻止满足实际约束的创建。模板不携带项目 Skill 或 MCP Tool/Connection 引用。

Agent 创建 / 更新按领域契约执行；没有正式删除生命周期约定前不增加 Agent 强制删除入口。编辑 instructions 不引入第二个独立 system prompt 字段。

### 3.1 项目 Skills 库与分配

项目库展示技能名称、简介、当前版本与安装结果，Owner 可上传/导入受支持的标准包；不增加独立手工编写、任意 URL 导入、市场或下载后台服务。安装失败显示真实结果并保留可重试输入；同名默认冲突，只有选定现有技能并明确更新才发布新版本，失败保留原版本。

安装成功只表示进入项目库，不自动分配给安装者。库和 Agent 配置提供明确分配/撤销入口，安装与分配分别反馈；版本更新不静默替换正在执行时已绑定的旧版本。移除分配不等于删除包，库删除和历史版本的具体清理不在布局新增规则。

Add Skills 是受保护的内置引导，不能从项目库单独删除，默认对 Agent 启用但可按 Agent 禁用；`install-skill` 是默认启用、可禁用的普通工具，`assign-skill` 是普通可选工具。它们不变为不可关闭 Core 能力，模板复制、保存或读取指引不能重新开启显式禁用项。

Owner 的项目管理入口与 Agent 工具权限分别按 [Skills 架构](../../architecture/agent-skills.md) 授权。普通 Agent 只读取自己获配技能，管理目录权限不等于正文读取权限；技能分配不顺带授予模型、工具、Runner 或 Secret 权限。

对正在执行的 Agent，新分配在下一模型轮次生效，不修改已发出的请求；waiting 不因此自动继续，已结束执行不重启。撤销/禁用不主动停止当前执行或脚本，后续读取/准备按当前权限返回正常结果或错误。页面显示实际保存/生效反馈，不把提交中、失败或冲突显示为已可用。

## 4. 模型与 Provider

Project Provider 表单：name、protocol、base_url、credential、provider_options、enabled。Model 表单：name、model_id、type（只读 chat）、parameters、request_overwrite、header_overwrite、enabled。列表显示名称、协议 / 类型、enabled 与模型归属。

“可用模型”展示系统与项目两种来源；System 配置只读，Project Owner 不能编辑系统 Provider Credential。删除引用中的 Project chat Model 先选择合法替代，更新受影响 Agent 引用；会议 Summary 只使用系统统一选择，不引用 Project chat Model。

## 5. MCP

配置目录显示 System Config 和 Project Config 来源，字段为 name、description、endpoint、auth_profile、enabled、revision。System Config 只读；Project Config 可创建 / 编辑 / 删除。配置认证字段与系统设置一致，不保存项目 Credential。

Connections 列表展示 mcp_config_id 对应名称、state、enabled、auth_state、capability_state、last_connected_at、last_error_code。详情展示 Credential Binding 的安全身份和各 capability 同步状态。提供 Connect、Disconnect、重新认证、重试、enabled 调整及工具 discovery 状态与默认启用提示的查看入口。

一个 Project 对同一个 Config 最多一个 Connection；Connect 时选择授权的 Credential source 或进入 OAuth。connected 不代表 capability 同步成功；不将实际 Tool 授权与连接状态混合。Core Resource Tools 不提供关闭开关。`default_enabled` 由平台根据 discovery annotation 推导，只用于初次配置提示，不提供任意编辑入口，不自动追加到已保存的 Agent Capability。

## 6. Variables / Secrets 与会议

Variables 列表 / 表单字段：name、description、value；Secrets 列表只显示 name、description、配置状态及已授权 Agent，表单仅提供新值写入，不读取回显旧值。type 在对应创建入口固定；提供创建、更新、删除，删除影响需在确认中展示。

会议配置说明 Summary（initial/update，含首轮标题）由系统管理员统一配置模型；Project 不保存或复制模型初值，不提供本地选择器、override 或第二套编辑入口。系统初始未配置不阻止 Project 创建，但需要生成 Summary 时会明确失败。本说明不新增 Project 只读 HTTP，也不展示 Agent Tools、Capability 或 reasoning effort 配置。

## 7. Scheduler 与项目审计

Scheduler 字段：`scheduler_enabled` 启停、`scheduler_max_concurrency` 并发上限（可不限制）。展示 Current Sprint 和实际运行摘要。暂停只阻止新自动调度，不自动终止已有 Execution；启动 Scheduler 不自动启动 planned Sprint。

`tick_interval`、`relaunch_skip_count`、`launch_retry_policy` 是运行时参数，不作为本轮 Project 表单字段。修改并发不覆盖其他项目。

项目审计字段及结构化筛选复用系统审计布局，但只查询本项目；只读，不复制 Task Timeline 或执行完整日志。

## 8. 默认、加载、异常与窄屏

普通入口打开基本信息，指定配置链接优先。Agent、Skills、模型、MCP、变量列表独立加载，空列表提供合法创建入口。连接错误显示诊断与重试，版本冲突保留输入。删除 / 撤销前明确影响范围，不以系统管理员身份绕过 Owner；归档项目的普通创建、配置与分配入口只读。

窄屏遵循通用设置布局，Agent 分区标签可横向滚动，凭据及参数表单单列，列表详情提供返回入口。

## 9. 相关架构

[项目生命周期](../../architecture/project-work-management/README.md)、[Agent 管理](../../architecture/agent-management.md)、[Agent Skills](../../architecture/agent-skills.md)、[项目变量与 Secret](../../architecture/project-work-management/project-environment-variables.md)、[Model Configuration](../../architecture/platform-infrastructure/model-system/model-configuration.md)、[MCP 生命周期](../../architecture/mcp-integration/mcp-server-config-lifecycle.md)、[MCP 默认启用](../../architecture/mcp-integration/mcp-tool-default-enable.md)、[Scheduler](../../architecture/scheduler/README.md)、[Audit](../../architecture/security-governance/audit.md)、[安全与治理](../../architecture/security-governance/README.md)。
