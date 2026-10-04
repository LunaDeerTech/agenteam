# 账号、邀请与密码恢复库

`internal/central/account` 提供真实 User/Session、bootstrap、登录/登出、邀请、改密、公开找回请求、一次性 reset、挑战和持久投递意图。SMTP 配置、人工重试与投递端口配合 `accountmail` 的真实协议和恢复日志 worker，见[账号邮件说明](accountmail.md)。生产 HTTP、Central 账号装配和个人资料仍在后续块，现有诊断继续 `ready=false`。规则见 [D07 实施规格](../work-items/d07-account-session-smtp-design.md)，验收状态见 [D07 主卡](../work-items/d07-account-session-smtp.md)。

## 组合与依赖

组合根以真实 Store 和独立 account keyring 构造 `NewAuthority`，将其注入 Audit 的 Accounts/Session/System、Secret 的 AccountWrites/Usage/Session/System 以及 Outbox 的 account Producer 端口。`LoadKeyring` 与全部当前/历史 cursor、Secret、download 材料交叉核验，不读取环境默认凭据。

在实际 event Catalog 注册 `DefineSessionsRevoked` 与 `DefineDeliveryRequested`，把 typed EventType、真实 Appender、Secret Service、Audit Appender、ProcessAuthority、受限 recoverylog Sink 和 `NewChallenges(authority, processID)` 传给 `account.New`。ProcessAuthority 必须关联真实共享 ProcessGuard；不能以心跳、连接断开或任意布尔代替死亡证明。缺挑战 provider 时，需要挑战的登录拒绝；缺 Events 不能先提交相应业务事实。

`Bootstrap` 只创建首次管理员；初始密码仅一次受限日志尝试，不能在重启中恢复或重印。Account 身份基于当前 User/Session 事实，不提供默认管理员或 Service 兜底。普通改密成功发出新 cookie 并撤销旧会话；旧 Session 不能借历史命令绕过当前授权，新响应丢失须重新登录。

持久表由仅 Up 的 `00010_account_session_smtp.sql` 及投递增量 `00011_account_mail_delivery.sql` 提供。组合方在事务外准备全部依赖，原业务 Tx 一次取得完整最强锁，再重验当前事实；Secret/Outbox/Audit 通过正式端口写入。Argon2id、随机材料和加密准备不占用业务事务。提交未知核实仍等待原命令 writer；确认失败保留原 `COMMIT_UNKNOWN/unknown` 与安全 cause，不从暂时缺行断言回滚。

## 邀请、找回与恢复

`CreateInvitation` 与 `ResendInvitation` 只接受当前管理员。重发保持同一 token/ID 和首次 24h 到期时刻；每链接 60s 一次新意图，原命令重放不重复计次。receipt 只有安全 ID/version；token 只进入 Secret。`InspectInvitation` 返回当前链接 email/期限，拒绝提供了另一当前身份的检查；`RedeemInvitation` 原子创建普通用户、消费链接、取消未开始任务、撤引用、建立材料清理、Audit 和命令 receipt，不自动登录。

`RequestPasswordReset` 对存在/不存在的合法邮箱具有相同接受事务和队列形状，返回 `{accepted:true,delivery_channel:smtp|backend_log}`，不等待 Secret 准备或发送，不回显 token 或用户存在性。每 IP 一小时 30 次，原同命令 receipt 可安全重放；每邮箱 60s 的实际投递节流在恢复工作中执行。`Recover` 异步按当前账号/密码版本准备或复用未过期 reset，公开响应不暴露这一步结果。`CompletePasswordReset` 改密、全会话撤销、消费 token、事件/Audit/receipt 同 Tx，始终不自动建立 Session。

`MailHandler` 是真实 Outbox 事务 handler：合法 intent 幂等建立一个 pending mail job，与 processed marker 原子提交。`ReconcileDeliveryIntents` 和 `Recover` 从权威 intent 补齐晚注册/重启留下的 job。pending 或 handler ACK 都不代表邮件发送；SMTP/log attempt、当前门禁及实际材料使用由独立 `accountmail.Runtime` 跟踪。

到期/撤销/消费先让链接失效并撤 reference，活跃 Secret lease 仍保护 bytes。只有实际使用者 join 后按 exact lease 释放，材料清理才可完成。恢复每阶段至多取 100 条，以持久 pass+ID 轮转，保护项不能饿死独立尾项；parent 取消后不领取下一条。planned 本地 mutation 在操作真实 join 后仍须原 writer 锁下核实；跨实例只接受正式 exact-process 停止证据。安全 pending 不伪装为 completed。

## 挑战与浏览器 harness

固定 GoCaptcha v2.0.5，使用自身生成的几何素材，未引入第三方照片或字体素材包。答案只在本进程受限内存；DB 保存 browser/email HMAC/login key/120s 期限及一次性结果。每 browser 最多 3 个、进程最多 1024 个有效挑战/未消费 pass，不淘汰旧有效项腾位。pass 与登录事务一次消费；回滚保留其可用性，进程重启不接受旧答案。

[真实浏览器 harness](../../../tests/account-captcha-web/README.md) 使用官方 Vue 组件，调用测试专用 HTTP adapter 上的真实库与 PG。覆盖鼠标旋转、窄屏、深色、reduced motion、键盘角度、焦点和 overflow；没有产品页面或生产测试后门。键盘使用相同验证，仍依赖视觉判断，不声称完整视障可达性。

## 检查

```sh
export AGENTEAM_GO=/path/to/go1.27.1/bin/go
sh scripts/check-go.sh
npm ci --prefix tests/account-captcha-web
AGENTEAM_MINIO_BINARY=/task-owned/cache/minio GOFLAGS=-p=1 \
  sh scripts/test-accounts.sh
```

普通检查不使用 Docker。真实 suite 校验 nonce/label/exact-ID 的 PG、MinIO 和网络资源，结束只清本轮资源；固定 MinIO 来源见[后端入口](README.md)。脚本以固定 `mutations`（原 24 项）、`identity`（原 32 项）和 `mail` 三组穷尽仓库内 Account/B03 用例，分别复用原 fixture 路径及 6m 包预算；也可传组名只运行该组。独立验证者的原五项 probe 位于其冻结副本，另用原精确 filter 执行，不作为仓库中的测试。包级串行不改变内部并发；未命中的其他包不能当兼容通过。不要将测试图片、密码、proof、完整链接、受限日志正文或浏览器环境凭据写入普通日志和报告。
