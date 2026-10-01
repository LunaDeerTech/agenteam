# 账号认证与邮件投递架构

> 上层：[平台基础设施](../README.md)。状态：已确认产品契约；安全实现参数见各详细设计的待定项。

## 1. 职责与边界

Authentication 负责人类账号、身份认证、Web Session、邀请注册、密码修改与恢复；SMTP Delivery 负责认证邮件和未配置 SMTP 时的后台链接投递。两者属于单个 Central 的平台能力，不拆分服务，不引入 Redis。

Authentication 确定当前用户 identity，不替代 [Authorization](../../security-governance/README.md)。每个 Project 只有一个 Owner；系统管理员管理系统资源，不因角色获得其他用户项目权限。Runner Device Enrollment、MCP OAuth 和 Provider Credential 不属于人类账号登录。

## 2. 详细设计

- [账号生命周期](account-lifecycle.md)：初始化、User、邀请、登录、Session、资料及密码恢复。
- [SMTP 与恢复链接投递](smtp-delivery.md)：配置、凭据、测试、重试和日志渠道。

前端表单和反馈见[账号入口布局](../../../frontend-design/layouts/account-entry.md)、[个人设置](../../../frontend-design/layouts/personal-settings.md)、[系统设置](../../../frontend-design/layouts/system-settings.md)。架构是业务规则的唯一文档来源，布局不另建生命周期。

## 3. 核心契约

首次初始化创建 admin@mail.com，随机密码打印 Central 后台日志；管理员首次登录提示改密但不阻断使用。普通用户由管理员输入邮箱邀请，单次邀请 24 小时有效，撤销及过期删除邀请记录。不开放公开注册。

普通用户和管理员都可找回密码。SMTP 已配置时邮件投递；未配置时邀请和重置链接输出后台日志。已配置但发送失败不自动切换为日志投递。

初始化密码及恢复链接属于明确授权的敏感后台恢复渠道，不写入 Audit、前端日志、Model Context 或项目内容。SMTP 不是 startup mandatory dependency，不改变 PostgreSQL / MinIO 的 readiness 契约。

## 4. 尚待实现设计

密码策略及哈希算法参数、重置 token 有效期及重复请求规则、Session cookie / 时限 / 撤销策略、登录与找回请求保护、头像上传限制尚未确定。实现前需补齐，不在布局文档或代码中假定邀请期限同样适用于重置。
