# 系统设置布局与栏目

> 框架：[通用设置](settings-shell.md)。仅系统管理员可访问；默认“用户与邀请 → 用户”。

## 1. 区域与菜单

```text
系统导航
悬浮侧栏                         内容区
用户与邀请 → 用户 / 待注册邀请   对应列表、详情或配置表单
模型与 Provider → Providers / 平台模型用途
MCP → 系统配置目录
Runner → 设备列表
Agent 模板 → 模板列表
安全与审批 → 审批规则 / 自动审批模型
审计 → 系统审计
平台配置 → SMTP / 项目默认值 / 运行信息
```

系统级 MCP 是现有架构的配置目录，并不替某个 Project 建立 Connection。系统配置不得提供直接浏览其他用户项目、会议、知识或执行内容的入口。

## 2. 用户与邀请

业务来源：[账号生命周期](../../architecture/platform-infrastructure/authentication/account-lifecycle.md)。用户列表显示邮箱、显示名、角色与注册时间；待注册邀请显示邮箱、创建时间、过期时间、投递方式与最近投递结果。

提供“邀请用户”邮箱输入弹窗、投递失败重试与撤销确认，不提供手工设置普通用户初始密码、角色切换、禁用或删除用户入口。重复邀请、兑换、失效和记录清理由账号架构定义；页面依据返回结果更新列表，不维护独立生命周期。

邮件 / 后台日志投递提示及错误反馈来自 [SMTP Delivery](../../architecture/platform-infrastructure/authentication/smtp-delivery.md)，页面不展示后台日志中的明文链接。

## 3. 模型与 Provider

| 区域 | 列表 / 详情字段 | 表单与操作 |
| --- | --- | --- |
| Provider | name、protocol、base_url、enabled；Models 子列表 | name、protocol、base_url、credential 替换、provider_options、enabled；创建 / 编辑 / 删除 |
| Model | name、model_id、type、enabled、所属 Provider | name、model_id、type、parameters、request_overwrite、header_overwrite、enabled；创建 / 编辑 / 删除 |
| 平台模型用途 | Embedding、Reranker、Memory、Image Generation 当前引用 | 从合法 enabled System Model 中选择，不在此编辑参数 |

System Model type 可选 chat / embedding / reranker / image_generation。chat parameters 展示 context_length、max_output、capabilities（input、tool_calling、parallel_tool_calls、streaming、reasoning、reasoning_efforts、structured_output）；其他类型按架构定义渲染其参数，不能复用错误的 chat 字段。

chat Model 删除仍有引用时要求合法替代模型并说明受影响引用数量和类型，系统操作不提供跳转他人项目内容的入口；Provider 删除连同 Models 按领域规则处理。平台内部必需用途引用不可留空，Reranker / Image Generation 可不配置。第一阶段协议候选为 openai-chat-completions / anthropic-messages。

## 4. 系统 MCP 配置目录

列表显示 name、endpoint、auth_profile、enabled、revision；表单编辑 name、description、endpoint、auth_profile、enabled。认证类型为 none / static_credential / oauth；静态 scheme 为 bearer / basic / custom_header，custom_header 显示 header_name。

系统目录不保存某项目认证材料，不提供任意 custom_headers 或本地 stdio 配置。创建 / 编辑 / 删除遵循 MCP Config 生命周期；Project 的 Connect 与认证在项目设置处理。

## 5. Runner 与 Agent 模板

Runner 列表：name、description、tags、status、平台与版本、last_seen_at；详情补充 root_path、capabilities、enrolled_at 等只读信息。创建表单 name、description、tags、root_path，后续编辑仅按领域允许修改的字段；root_path 创建后保持只读。提供 Enrollment 引导和领域支持的重新 Enrollment / 撤销凭据操作，明确已有运行影响，不把设备 capability 变成手工 allowlist。

Agent 模板列表：name、tag-color、description、model_ref；表单另含 reasoning_effort、instructions、AGENTS.md 注入策略、allowed tools、skills。提供创建、编辑、删除、查看。模板不含项目挂载、Secret 白名单、Reusable Approval 或 Memory；修改模板不覆盖既有 Agent。

## 6. 安全、审计与平台配置

审批规则页面只读说明固定 default 规则及风险分类，不提供 Project 自定义矩阵。自动审批模型页面只读展示调用 contract、固定 Prompt 与失败回退规则；实际 `approval_model_ref` 在项目 Agent 的 auto 策略中配置，不新增系统全局审批模型 selector 或可编辑 Prompt。

系统审计列表展示 created_at、actor、action、outcome、resource 身份及关联 ID；按结构化字段筛选，详情只展示授权可见的结构化证据，不提供全文搜索、编辑或删除，也不回显 Secret。

SMTP 页面展示主机、端口、加密方式、认证账号、密码替换、发件人邮箱和显示名。提供保存 / 取消、测试收件邮箱及测试发送；显示配置、保存、测试和业务投递结果。凭据与渠道选择遵循 [SMTP Delivery](../../architecture/platform-infrastructure/authentication/smtp-delivery.md)，不由布局定义。未配置时显示后台链接获取说明，发送失败提供重试。

项目默认值表单仅编辑 `default_project_scheduler_max_concurrency`，支持不限制；修改只影响以后创建的项目。平台模型用途由模型菜单统一维护，此处提供跳转，不建立第二套 selector。

运行信息只读展示 Central 版本、PostgreSQL / pgvector、MinIO 和 ready 状态；地址、数据库凭据等部署配置不做普通在线编辑。所需 read model 待后端设计补齐。

## 7. 加载、默认、异常与窄屏

普通入口选用户列表；直接链接选指定叶子项。每个列表独立加载，空列表提供相应创建入口；无权限不展示配置内容。保存和删除遵循通用设置框架；设备离线不等于配置读取失败，认证失败不伪装成禁用。

窄屏侧栏覆盖，列表堆叠、表单单列、详情可返回列表，局部 JSON 参数编辑区横向滚动。

## 8. 相关架构

[Model Configuration](../../architecture/platform-infrastructure/model-system/model-configuration.md)、[Runner Management](../../architecture/runner/runner-management.md)、[Agent 管理](../../architecture/agent-management.md)、[MCP Config](../../architecture/mcp-integration/mcp-server-config-lifecycle.md)、[安全与治理](../../architecture/security-governance/README.md)、[自动审批模型](../../architecture/security-governance/auto-approval-model/README.md)、[Audit](../../architecture/security-governance/audit.md)、[Deployment Runtime](../../architecture/platform-infrastructure/deployment-runtime.md)。
