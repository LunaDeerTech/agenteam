# SMTP 与认证链接投递

> 上层：[账号认证](README.md)。相关：[账号生命周期](account-lifecycle.md)、[Deployment Runtime](../deployment-runtime.md)、[Outbound Network Policy](../outbound-network-policy.md)、[Audit](../../security-governance/audit.md)。

## 1. 职责

向注册邮箱投递邀请和密码重置链接，并支持管理员测试邮件。未配置 SMTP 时使用明确授权的 Central 后台日志渠道；这不是通用邮件 / Push 通知中心，不为 Inbox 新增外部通知机制。

## 2. 配置与凭据

SMTP 配置为 Runtime Platform Config，由系统管理员维护，持久化 PostgreSQL。概念字段：host、port、encryption（none / STARTTLS / TLS）、auth username、credential_ref、sender email、sender display name。最终存储 schema 与 adapter 参数在实现阶段确定。

认证密码经平台 Secret Management 保存，不在 SMTP 配置中保存明文，不在读取 API 回显。空白替换输入保留现有凭据，移除凭据使用显式动作。保存配置不自动发送邮件；测试使用指定收件邮箱，不创建注册邀请或重置请求。

支持 TLS、STARTTLS、none 三种模式，由管理员明确选择。选择加密时正常验证证书，STARTTLS 升级失败停止发送，不自动降级明文；私有 CA 通过受信任证书配置支持，不关闭验证。none 是正式选项，不另加“仅限内网”的业务限制，也不能由选择 none 绕过网络授权。

目标地址仍遵循统一 Outbound Network Policy：固定禁止地址不能放行，内网须命中管理员维护的网段/端口规则。SMTP 使用非 HTTP 安全拨号适配，不直接调用 OutboundHttpClient，也不把内网 HTTP 的 allow_http 开关当作 SMTP 加密模式。协议、证书、连接/握手/投递 timeout 的具体适配由 D07 规格落实。

## 3. 投递渠道

| 条件 | 投递行为 |
| --- | --- |
| 已保存有效 SMTP 配置 | 通过 SMTP 发送邀请或重置邮件 |
| 尚未配置 SMTP | 输出相应链接到 Central 后台日志，UI 提示获取并转交方式 |
| SMTP 已配置但连接、认证或发送失败 | 记录投递失败，提供重试，不自动输出恢复链接到日志 |

不能将无效配置或发送失败解释为“未配置”。发件凭据不进入邮件正文；链接必须使用部署 public origin，不能使用未经验证的请求 Host 拼接恢复地址。

## 4. 保存、测试与重试

配置保存、测试发送和业务邮件投递分别反馈结果。测试失败不删除已经保存的配置。投递状态描述邮件发送或日志输出结果，不复制 User / Invite / PasswordReset 的生命周期。

邀请或重置请求创建成功后发送失败保留已提交事实，不回滚成从未创建，也不通过重复创建改变有效期。重试使用仍有效的原链接；过期、撤销或已使用后停止投递，不重新生成链接绕过生命周期。有效重置请求重复申请同样重发原链接，见[账号生命周期](account-lifecycle.md#6-密码修改与恢复)。

网络超时、服务端暂时不可用等临时故障后台有限次自动重试，并允许手动重试；认证错误等需修正配置的故障停止自动重试并提示处理。次数/间隔具有默认值，可由管理员系统配置修改；具体默认值、上限、timeout 和分类在 D07 规格确定。

投递任务持久化并可在 Central 重启后恢复，自动/手动重试须协调去重并重新读取当前合法配置/凭据。超时结果未知时可能已经发出邮件，SMTP 不保证收件端严格只收到一次；不得宣称数据库与邮件发送原子、严格一次投递或必达。幂等、材料引用、并发和未知结果处理由 D07/D06 规格落实。自动重试仍不把已配置 SMTP 的失败降级为后台日志。

## 5. 后台恢复日志与 Audit

首次管理员随机密码、未配置 SMTP 时的邀请链接与重置链接允许输出 Central 后台运行日志，这是已确认的恢复渠道。日志接收者具有恢复对应账号的能力；运维需限制该日志访问与转交范围。

这些内容不进入前端日志、业务 Timeline、Human Inbox、Model Context 或 Audit。Audit 只记录脱敏 actor / action / outcome / resource，不保存 Secret plaintext、邀请 / 重置 token 或完整链接。

后台日志的访问方式、保留及恢复流程需在部署实现中明确，不能把日志投递提示解释为公开查看凭据 API。后台投递失败应反馈失败，不声称用户已经收到链接。

## 6. 部署与验证

SMTP 是可选运行配置，无配置不阻断 Central ready。PostgreSQL / pgvector 和 MinIO 仍为 mandatory dependency，不新增基础设施 degraded mode。

验证 SMTP 有 / 无配置、TLS/STARTTLS/none、私有 CA 与升级失败、出站拒绝、保存/测试/业务投递失败、有限自动与手动重试竞争及重启恢复。验证邀请/重置链接不延长期限，撤销/过期/已用后禁止重发，未知投递结果被如实记录，恢复日志不进入其他可见数据流。
