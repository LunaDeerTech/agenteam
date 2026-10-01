# 前端设计与架构衔接清单

> 本文汇总后续实现需要补齐的领域契约。已确认的会议与账号规则已同步到架构；本文只保留尚待补齐的契约，不代表接口已有实现。

## 1. 已有契约直接复用

| 能力 | 来源与边界 |
| --- | --- |
| 项目访问 | [安全与治理](../architecture/security-governance/README.md)：一个 Owner，无人类成员矩阵；管理员不能据角色访问其他用户项目 |
| 任务页面 | [Work Management](../architecture/project-work-management/README.md)：强制层级、唯一 Current Sprint、正式状态机、version、rank、Timeline |
| 会议内容 | [Meeting](../architecture/meeting/README.md)：长期会话、结构化引用、Contribution、按需展开运行详情 |
| 模型与 Agent | [模型配置](../architecture/platform-infrastructure/model-system/model-configuration.md)、[Agent](../architecture/agent-management.md)：配置字段、模型替代、能力和审批边界 |
| MCP | [MCP Config](../architecture/mcp-integration/mcp-server-config-lifecycle.md)：Config / Connection / Credential Binding 分离 |
| 知识正文 | [KnowledgeDocument](../architecture/knowledge-memory/knowledge-document-domain.md)：当前版本、文件预览、索引异步、文档授权与删除 |
| 统一待办 | [Human Inbox](../architecture/platform-infrastructure/human-inbox.md)：open / resolved / dismissed、排序、忽略限制、来源动作路由与实时更新 |
| 账号认证 | [Authentication](../architecture/platform-infrastructure/authentication/README.md)：初始化、登录、邀请注册、资料与密码恢复 |
| 认证邮件 | [SMTP Delivery](../architecture/platform-infrastructure/authentication/smtp-delivery.md)：SMTP 配置与凭据、测试、重试及后台恢复日志 |

## 2. 已归位规则与待补齐契约

会议中央输入框、mention 与编排区别、首轮一次性标题、四字段 Summary 及详情卡片已同步到 [Meeting 架构](../architecture/meeting/README.md)。账号与 SMTP 的业务约定已迁入认证专题，布局只引用规则并描述可见交互。

| 子系统 | 已有架构来源 / 确认内容 | 尚需补齐 |
| --- | --- | --- |
| 账号安全实现 | [账号生命周期](../architecture/platform-infrastructure/authentication/account-lifecycle.md) | 密码策略与哈希参数、重置期限及重复请求规则、Session 生命周期与撤销、认证请求保护 |
| SMTP 实现 | [投递契约](../architecture/platform-infrastructure/authentication/smtp-delivery.md) | SMTP 出站策略适配、超时与重试参数、部署日志访问与恢复细节 |
| 头像 | 可修改 / 移除的账号资料 | 安全媒体范围、大小、对象处理与生命周期 |
| Project 资料与创建 | 本人项目列表、名称和描述、Owner 服务端绑定 | canonical 字段与查询 / 更新 / 并发契约 |
| 知识文档结构 | 同项目单父文档、可为根节点、禁止循环；每个节点有正文 | parent identity、树查询 / stable sort / 分页、调整父节点命令及并发 |
| 文档子树删除 | 明示范围并确认删除所有后代 | 一致性、批量 soft delete、索引 / 存储 cleanup 与历史引用投影 |
| 系统运行信息 | 管理员只读版本及依赖状态 | 授权 read model，不暴露部署凭据 |
| 全局 Inbox | 汇总本人项目待办 | 当前用户聚合查询、稳定分页及跨本人项目实时订阅 |

## 3. 布局层实现约定

- 当前视图、选中对象、树展开、抽屉状态由前端管理；可直接链接的对象身份应可恢复，最终 URL 命名在实现阶段统一确定。
- 通用入口按页面默认规则，显式链接优先；对象权限和存在性仍由服务端验证。
- 三类设置复用容器、二级菜单、显式保存、未保存提示、加载 / 冲突处理，不复制领域授权。
- 搜索本轮仅确定浮层容器，检索对象和接口后续单独设计。

## 4. 尚未定稿内容

Dashboard 内容、全局搜索范围、认证底层安全参数、知识结构的 version / indexing 耦合、样式参数均尚未定稿。这里的待补齐项不影响布局文档交付，但不能将文档视为这些后端接口的完整实现规格。

## 5. 后续验证要求

领域专题完成后复核本目录字段、操作可见性和失败状态；前端实现阶段再做真实页面、双导航、窄屏、键盘与焦点验证。本轮验证仅覆盖 Markdown 文档、相对链接和契约一致性。
