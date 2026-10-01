# SMTP 与认证链接投递

> 上层：[账号认证](README.md)。相关：[账号生命周期](account-lifecycle.md)、[Deployment Runtime](../deployment-runtime.md)、[Outbound Network Policy](../outbound-network-policy.md)、[Audit](../../security-governance/audit.md)。

## 1. 职责

向注册邮箱投递邀请和密码重置链接，并支持管理员测试邮件。未配置 SMTP 时使用明确授权的 Central 后台日志渠道；这不是通用邮件 / Push 通知中心，不为 Inbox 新增外部通知机制。

## 2. 配置与凭据

SMTP 配置为 Runtime Platform Config，由系统管理员维护，持久化 PostgreSQL。概念字段：host、port、encryption（none / STARTTLS / TLS）、auth username、credential_ref、sender email、sender display name。最终存储 schema 与 adapter 参数在实现阶段确定。

认证密码经平台 Secret Management 保存，不在 SMTP 配置中保存明文，不在读取 API 回显。空白替换输入保留现有凭据，移除凭据使用显式动作。保存配置不自动发送邮件；测试使用指定收件邮箱，不创建注册邀请或重置请求。

由配置决定的出站地址服从部署安全边界；SMTP 协议的出站策略适配需补齐，不能将 SMTP 直接当作既有 OutboundHttpClient 调用，也不能绕过 private-network / TLS 边界。

## 3. 投递渠道

| 条件 | 投递行为 |
| --- | --- |
| 已保存有效 SMTP 配置 | 通过 SMTP 发送邀请或重置邮件 |
| 尚未配置 SMTP | 输出相应链接到 Central 后台日志，UI 提示获取并转交方式 |
| SMTP 已配置但连接、认证或发送失败 | 记录投递失败，提供重试，不自动输出恢复链接到日志 |

不能将无效配置或发送失败解释为“未配置”。发件凭据不进入邮件正文；链接必须使用部署 public origin，不能使用未经验证的请求 Host 拼接恢复地址。

## 4. 保存、测试与重试

配置保存、测试发送和业务邮件投递分别反馈结果。测试失败不删除已经保存的配置。投递状态描述邮件发送或日志输出结果，不复制 User / Invite / PasswordReset 的生命周期。

邀请创建成功后发送失败保留有效邀请供重试，不回滚成从未创建，也不通过重复创建改变有效期。投递重试再次校验邀请有效性；过期或撤销后不能投递旧链接。密码重置重试与重复申请策略待账号安全设计补齐。

测试及投递的 timeout、retry、幂等与安全 token 材料保存细节待实现专题明确；不在此承诺无限重试或必达。邮件和数据库 mutation 不组成分布式事务。

## 5. 后台恢复日志与 Audit

首次管理员随机密码、未配置 SMTP 时的邀请链接与重置链接允许输出 Central 后台运行日志，这是已确认的恢复渠道。日志接收者具有恢复对应账号的能力；运维需限制该日志访问与转交范围。

这些内容不进入前端日志、业务 Timeline、Human Inbox、Model Context 或 Audit。Audit 只记录脱敏 actor / action / outcome / resource，不保存 Secret plaintext、邀请 / 重置 token 或完整链接。

后台日志的访问方式、保留及恢复流程需在部署实现中明确，不能把日志投递提示解释为公开查看凭据 API。后台投递失败应反馈失败，不声称用户已经收到链接。

## 6. 部署与验证

SMTP 是可选运行配置，无配置不阻断 Central ready。PostgreSQL / pgvector 和 MinIO 仍为 mandatory dependency，不新增基础设施 degraded mode。

验证 SMTP 有 / 无配置、保存失败、测试成功 / 失败、业务发送失败、有效邀请重试、不延长邀请期限、撤销 / 过期后禁止重发，以及日志投递内容不进入其他可见数据流。
