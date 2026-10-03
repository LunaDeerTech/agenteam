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

首次初始化创建邮箱 admin@mail.com、username 为 admin 的管理员，随机密码打印 Central 后台日志；管理员首次登录提示改密但不阻断使用。普通用户接受管理员邮箱邀请时填写全站唯一 username，单次邀请固定 24 小时有效，撤销及过期删除邀请记录。不开放公开注册，登录继续使用邮箱。

普通用户和管理员都可找回密码。SMTP 已配置时邮件投递；未配置时邀请和重置链接输出后台日志。已配置但发送失败不自动切换为日志投递。

密码使用 Argon2id；设密规则为 15–128 字符、允许中文/空格并拒绝常见弱密码。Session 默认空闲 7 天、绝对 30 天，重置链接默认 30 分钟；这些期限由管理员在系统 UI 配置。有效期内重发同一重置链接，不延长期限；普通改密换发当前 Session 并撤销其他会话，重置撤销全部会话。

浏览器写请求使用 Origin + CSRF 保护；登录连续失败默认 5 次后要求挑战，阈值可配置。GoCaptcha 与 go-captcha-vue 直接内嵌 Central / Vue，不部署独立验证服务。头像仅静态 JPG/PNG/WebP，经服务端验证、缩放和重新编码；只保留当前头像，旧对象异步清理。

初始化密码及恢复链接属于明确授权的敏感后台恢复渠道，不写入 Audit、前端日志、Model Context 或项目内容。SMTP 不是 startup mandatory dependency，不改变 PostgreSQL / MinIO 的 readiness 契约。

## 4. 尚待实现设计

上述业务方向已确定。D07 仍须落实哈希性能参数、Cookie/CSRF 和挑战协议、期限配置的生效边界、并发消费/撤销、媒体与存储限制及 SMTP 投递恢复；D26 接入真实浏览器流程。字段、库版本和可执行验收未完成，不把文档归位当作实现通过。
