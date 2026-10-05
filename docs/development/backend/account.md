# 账号、Session、个人资料与 HTTP

`internal/central/account` 已通过 Central 生产根提供真实 User/Session、bootstrap、登录/登出、邀请、改密、公开找回、一次性 reset、挑战、Profile/Avatar/偏好及账户 System HTTP。SMTP 配置、人工重试与投递端口配合已装配的 `accountmail` 协议和受限恢复日志 worker，见[账号邮件说明](accountmail.md)。D07 当前后端范围已通过业务与测试门槛；Project/Runner 等生产绑定、D25 WS 撤销消费者和 D26/D27 产品页面仍由后续模块完成，诊断继续 `ready=false`。规则见 [D07 实施规格](../work-items/d07-account-session-smtp-design.md)，组合验收与历史限制见 [D07 主卡](../work-items/d07-account-session-smtp.md)。

## 部署输入与首次管理员

除数据库、MinIO 和既有 cursor/Secret/download keyring，Central 必填以下两项，完整示例见 [central.env.example](../../../deploy/central.env.example)：

```text
AGENTEAM_CENTRAL_ACCOUNT_KEYRING={"format":1,"current_kid":"a1","keys":[{"kid":"a1","key_b64":"<deployment-generated-independent-account-key-base64>"}]}
AGENTEAM_CENTRAL_ACCOUNT_RECOVERY_LOG=/var/lib/agenteam/account-recovery/recovery.jsonl
```

keyring 占位值故意无效；部署须独立生成随机 32-byte 材料并使用严格带 padding 的标准 base64。格式为 `format=1`、当前 kid 与 1–32 把 key，JSON ≤16 KiB；全部当前/历史材料均不得与 cursor、Secret、download 重复，不复制示例或复用其它用途的 key。保留仍被持久事实引用的旧 key，未知或缺失必需旧材料会拒绝初始化。

恢复日志使用固定规范绝对文件路径。部署先创建当前服务 UID 所有的 0700 父目录，不允许路径中的符号链接；文件须为同 UID 的 0600 常规文件，不存在时由启动创建。打开或权限核验失败阻止初始化。`--check-config` 仅验证路径，不打开/创建该文件，也不证明运行时权限合格；显式 CA 仍按既有规则读取。受限文件含敏感恢复材料，不能指向普通 stdout/stderr 或纳入通用日志采集；读取、备份及保留由部署操作者管理。

`AGENTEAM_CENTRAL_PUBLIC_ORIGIN` 决定浏览器边界与邮件链接，正式部署须使用 HTTPS；仅字面 `localhost` 或 loopback 地址允许本地 HTTP，使用独立本地 Cookie 名。反向代理须保留 canonical Host，不以 `Host`、`X-Forwarded-*` 推导公共链接或安全模式。默认客户端 IP 只取 RemoteAddr。

首次启动幂等创建 `admin@mail.com` / `admin`，随机初始密码仅向上述受限日志尝试输出一次；重启不覆盖密码或重印。若该次输出丢失，走普通密码恢复。只有 SMTP 未配置时邀请/reset 使用受限日志；配置后发送失败不会改渠道。公共 bootstrap HTTP 只给浏览器上下文/CSRF 等安全投影，不返回初始密码。

## 组合与依赖

组合根以真实 Store 和独立 account keyring 构造同一 `NewAuthority`，将其注入 Audit 的 Accounts/Session/System、Secret 的 AccountWrites/Usage/Session/System、Outbound 的 Session/System 和 Outbox 的 account Producer/Session/System 端口。`LoadKeyring` 与全部当前/历史 cursor、Secret、download 材料交叉核验，不读取环境默认凭据。

在实际 event Catalog 注册 `DefineSessionsRevoked` 与 `DefineDeliveryRequested`，把 typed EventType、真实 Appender、Secret Service、Audit Appender、ProcessAuthority、受限 recoverylog Sink 和 `NewChallenges(authority, processID)` 传给 `account.New`。ProcessAuthority 必须关联真实共享 ProcessGuard；不能以心跳、连接断开或任意布尔代替死亡证明。缺挑战 provider 时，需要挑战的登录拒绝；缺 Events 不能先提交相应业务事实。

`Bootstrap` 只创建首次管理员；初始密码仅一次受限日志尝试，不能在重启中恢复或重印。Account 身份基于当前 User/Session 事实，不提供默认管理员或 Service 兜底。普通改密成功发出新 cookie 并撤销旧会话；旧 Session 不能借历史命令绕过当前授权，新响应丢失须重新登录。

真实构造顺序为 Authority→Audit/Secret/Outbound/Outbox→Account core→`NewAvatarAuthority(core)`→Object→`NewProfileService(core, objects)`→Account Runtime，再接真实 Mail Runtime 和 HTTP。Avatar wrapper 保持同 core，不能借未来 Project 端口默许操作。System HTTP facade 显式使用独立 cursor keyring，逐页重验当前管理员；合法 cursor 不授予权限。生产 catalog 只装配一个 `account.mail-enqueue` handler，`account.sessions-revoked` 的未来 WS 消费属于 D25。

持久表由仅 Up 的 `00010_account_session_smtp.sql`、投递增量 `00011_account_mail_delivery.sql` 及头像恢复索引 `00012_account_avatar_recovery.sql` 提供。组合方在事务外准备全部依赖，原业务 Tx 一次取得完整最强锁，再重验当前事实；Secret/Outbox/Audit 通过正式端口写入。Argon2id、随机材料和加密准备不占用业务事务。提交未知核实仍等待原命令 writer；确认失败保留原 `COMMIT_UNKNOWN/unknown` 与安全 cause，不从暂时缺行断言回滚。

## 正式 HTTP 与资料头像

[account OpenAPI](../../../api/openapi/account.json) 固定 34 个显式 method/path、26 条路径，均位于 `/api/v1`：

| 表面 | 当前能力 |
| --- | --- |
| `/auth/bootstrap`、`/session`、`/sessions/*`、`/auth/challenges*` | 匿名浏览器上下文、当前 Session、登录/登出和挑战验证 |
| `/password-resets/*`、`/invitations/inspect`、`/invitations/redeem` | 统一恢复回执、链接检查和一次消费，token 只放请求正文 |
| `/me`、`/me/preferences`、`/me/change-password`、`/me/avatar` | 本人资料、主题偏好、改密及头像读写/删除 |
| `/system/users`、`/system/invitations*`、`/system/account-settings` | 当前管理员列表、邀请/重发/撤销与账户安全设置 |
| `/system/smtp*`、`/system/mail-jobs*` | 当前管理员 SMTP 配置/取消配置/测试、安全任务状态与人工重试 |

Host 必须匹配规范化 PublicOrigin；非安全方法要求同源 Origin，浏览器/Session 写请求验证相应 `X-CSRF-Token`。HTTPS Cookie 为 `__Host-agenteam_session` / `__Host-agenteam_browser`，`HttpOnly; Secure; SameSite=Lax; Path=/` 且无 Domain；本地 HTTP 使用 `agenteam_local_*`。每次当前身份/权限检查不被历史 receipt 或 cursor 替代；业务命令沿 OpenAPI 带幂等键和版本，拒绝未知字段。公共 HTTP middleware 由根只包装一次，响应、Problem 与日志使用同一 request ID。

email 只读，本人可改 username/display_name/theme；username 按当前 DB 映射解析，改名后旧路径失效且无 alias。头像接单一原始 JPEG/PNG/WebP body，最多 5 MiB、边长 4096、总像素 16,777,216，拒动画/SVG/GIF、截断和尾随拼接；保持比例缩至 512×512 内、不放大，白底合成并重编码 JPEG，丢弃原元数据。命令摘要绑定完整原始 bytes，不能用重编码后的 SHA 替代输入语义。

头像切换、旧引用关门、Audit 与 receipt 在同一真实 Tx；后台按持久 cause 与实际 lease 公平清理，不从取消或年龄猜测实例死亡。GET/HEAD 支持单 Range，逐次核本人当前 exact ObjectID；已建立 reader 的底层 I/O 和 Close/join 真实结束后才释放 lease。它不是匿名 URL，也不是通用 Artifact/下载接口。

Account/Mail 初始化、bootstrap 和恢复消耗同一剩余安全启动预算。健康采样核真实技术状态，不发送邮件。停机先停全部准入，Mail 的 FinishDelivery 和 actual join 必须先于 Core 最终排空；LoginResponse.UseCookie、头像解码/reader 和整个恢复日志 Sink 未 join 时保留共享 guard，DB 最后关闭，详见[生命周期](README.md#停止与退出)。

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

普通检查不使用 Docker。真实 suite 校验 nonce/label/exact-ID 的 PG、MinIO 和网络资源，结束只清本轮资源；固定 MinIO 来源见[后端入口](README.md)。脚本默认 `all`，顺序运行 `mutations`、`identity`、`mail`、`profile`、`avatar`、`avatar-recovery`、`http`、`app`、`library` 九组，也可传单个组名。前八组沿原 fixture 选择，`library` 完整执行 `internal/central/account/...`；保留原 `-race -count=1 -timeout=6m`，包级 `-p=1` 不改变内部并发。

D07 组合验收按实际 build tags 固定 Account 95 顶层库存、Mail 31 选择、library 67 和 app 12（11 功能加 1 child helper），另保旧域完整包与普通 check-go 的覆盖。HTTP 六项采用已核的四个完整未失败函数、修后邀请单项与受 artwork adapter 影响的挑战单项，不重复跑绿覆盖原失败。原独立五 probe 和 Sink 专用 overlay 只存在其固定测试输入，裸 helper/no-tests 不算功能通过；实际证据与历史限制见[主卡](../work-items/d07-account-session-smtp.md#当前进度)。不要将测试图片、密码、proof、完整链接、受限日志正文或浏览器环境凭据写入普通日志和报告。
