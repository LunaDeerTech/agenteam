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

`AGENTEAM_CENTRAL_PUBLIC_ORIGIN` 决定浏览器边界与邮件链接，正式部署须使用 HTTPS；默认仅字面 `localhost` 或 loopback 地址允许本地 HTTP，使用独立本地 Cookie 名。反向代理须保留 canonical Host，不以 `Host`、`X-Forwarded-*` 推导公共链接或安全模式。默认客户端 IP 只取 RemoteAddr。

`AGENTEAM_CENTRAL_DEV_MODE` 默认 `false`，只接受精确 `true` / `false`。设为 `true` 时允许通过任意域名或 IP 的 HTTP 地址开发测试，不检查请求的 Host、Origin、Sec-Fetch-Site，也不限制来源网段；即使 `PUBLIC_ORIGIN` 是 HTTPS，也统一使用普通、非 Secure Cookie。CSRF token、Session 与业务权限仍须验证。`PUBLIC_ORIGIN` 仍须为合法 URL，可使用远程 HTTP，并继续作为邮件和受限恢复日志中链接的固定基址；浏览器访问地址可与它不同，不从请求 Host 生成链接。关闭 Dev 模式即恢复默认来源与 Cookie 策略。

首次启动幂等创建 `admin@mail.com` / `admin`，随机初始密码仅向上述受限日志尝试输出一次；重启不覆盖密码或重印。若该次输出丢失，走普通密码恢复。只有 SMTP 未配置时邀请/reset 使用受限日志；配置后发送失败不会改渠道。公共 bootstrap HTTP 只给浏览器上下文/CSRF 等安全投影，不返回初始密码。

## Passkey 与邮箱验证部署边界

本轮新增能力已实现并完成适用的本地验证，具体范围见[Passkey 与邮箱变更工作项](../work-items/account-passkeys-email-change.md)；当前公开预览仍保留旧稳定组合，新功能未部署到该实例。系统设置不新增 Passkey 配置；RP ID 与允许 Origin 只从固定 `AGENTEAM_CENTRAL_PUBLIC_ORIGIN` 派生，使用规范 DNS hostname 与准确端口。IP Origin 明确不可用，原密码登录继续工作；不从请求或转发头生成 RP/Origin，不降低 TLS 验证。域名改变会影响原 RP 下凭据的可用性，须按正式部署域名配置并由用户在独立服务器验证物理认证器、云同步及外部邮件。

首次管理员的默认邮箱不是已验证邮箱。本人Passkey投影显式返回 `email_initialization_required`：仅未验证且有真实bootstrap记录时为true，不能按管理员角色或邮箱字符串猜测；普通未验证注册用户可用当前密码及原邮箱真实SMTP码完成首登记验证。

部署者入口源码为 `cmd/agenteam-account-recovery/main.go`，独立命令使用与当前Central相同的已加载部署配置、数据库和keyring，不自动读取 `.env`。使用Go1.27.1构建；initialize_email实际签发及后续真实PG/隔离SMTP初始化已验证，分轮原失败与只读续验边界见工作项。调用方式：

```sh
go build -o ./bin/agenteam-account-recovery ./cmd/agenteam-account-recovery
./bin/agenteam-account-recovery --user-id '<USER_ID>' --purpose initialize_email --email 'owner@example.invalid' --output '/absolute/private/mailbox-authorization.json'
```

UserID须取真实不可变账号ID，邮箱与路径示例须替换。`initialize_email`用于未验证邮箱初始化，`recover_account`用于旧邮箱/Passkey不可用但仍能以密码登录的恢复。授权固定30分钟，只绑定此UserID、purpose与目标邮箱；同用户/用途新签发撤销旧授权，并写Audit。命令只签发授权，不更换邮箱、不发验证码、不重置密码，也不直接标记邮箱已验证。

`--output`须是调用者控制的0700目录内尚不存在的绝对文件路径；命令以排他创建方式写0600文件，失败不覆盖已有文件。输出JSON使用小写 `id/authorization/user_id/purpose/email/expires_at`。授权不得输出到普通日志、截图或Git，经受限渠道交本人使用。命令不授予匿名登录能力：用户仍须已有当前Session并提供当前密码；同时丢失密码的离线接管不纳本轮。

用户在个人“登录与安全”打开初始化/恢复邮箱modal，填写授权和当前密码；服务器从授权读取目标，向该邮箱发真实数字码，用户确认后才原子变更并验证邮箱。两种purpose共用 `/me/email-initialization/request|resend|confirm`，请求不另传邮箱或purpose；普通页面只提示向部署者获取授权，不展示命令长文。恢复成功撤销旧Passkeys、其他Session及旧reset，保当前强Session；初始化/恢复确认不附登记grant，之后正常首登记需再以当前密码和新邮箱码验证。管理员角色、后台日志或直接修改资料不能替代收码事实。

这些验证码和旧邮箱变更通知使用专用 SMTP 邮件 kind，无后台日志 fallback；SMTP 未配置或不可用时明确反馈，不能称验证邮件已发送。实施与验收使用任务拥有的隔离数据库、域名/证书和 SMTP sink；不变更当前用户实例、域名、SMTP配置或 keys，不向外部邮箱发送。虚拟认证器/隔离 sink 的通过范围与真实设备、公开域名及外部投递分开记录。

### 隔离安全复验

[独立浏览器探针](../../../tests/account-preview/passkey-security-independent.cjs)使用真实App、虚拟认证器与私有SMTP sink，默认无opt-in不连接。运行前须确认fixture归属、`account-passkeys-email-change` scope、有效期限、专用CA/leaf/nonce与live实例一致，并取得浏览器及邮箱观测窗口的独占使用权；不能指向用户预览或复用其他任务的邮箱。

```sh
NODE_PATH='<repo>/tests/account-captcha-web/node_modules' \
NODE_EXTRA_CA_CERTS='<owned-certificate-ca.pem>' \
PASSKEY_OWNED_RUNTIME='<owned-runtime.json>' \
PASSKEY_SECURITY_RUN=1 \
node tests/account-preview/passkey-security-independent.cjs
```

从仓库根运行，前三个占位值替换为已核验的绝对路径，私有runtime及授权材料不入Git。已通过的独立范围是跨Session/操作/用途/目标拒绝、登记grant一次消费和认证凭据撤销后的邮箱确认拒绝；实际浏览器退出、会话logout与无外域访问另核。该结果不代表物理认证器、云同步、公网域名或外部邮件已通过；完整分轮结果与部署边界见本轮工作项。

## 组合与依赖

组合根以真实 Store 和独立 account keyring 构造同一 `NewAuthority`，将其注入 Audit 的 Accounts/Session/System、Secret 的 AccountWrites/Usage/Session/System、Outbound 的 Session/System 和 Outbox 的 account Producer/Session/System 端口。`LoadKeyring` 与全部当前/历史 cursor、Secret、download 材料交叉核验，不读取环境默认凭据。

在实际 event Catalog 注册 `DefineSessionsRevoked` 与 `DefineDeliveryRequested`，把 typed EventType、真实 Appender、Secret Service、Audit Appender、ProcessAuthority、受限 recoverylog Sink 和 `NewChallenges(authority, processID)` 传给 `account.New`。ProcessAuthority 必须关联真实共享 ProcessGuard；不能以心跳、连接断开或任意布尔代替死亡证明。缺挑战 provider 时，需要挑战的登录拒绝；缺 Events 不能先提交相应业务事实。

`Bootstrap` 只创建首次管理员；初始密码仅一次受限日志尝试，不能在重启中恢复或重印。Account 身份基于当前 User/Session 事实，不提供默认管理员或 Service 兜底。普通改密成功发出新 cookie 并撤销旧会话；旧 Session 不能借历史命令绕过当前授权，新响应丢失须重新登录。

真实构造顺序为 Authority→Audit/Secret/Outbound/Outbox→Account core→`NewAvatarAuthority(core)`→Object→`NewProfileService(core, objects)`→Account Runtime，再接真实 Mail Runtime 和 HTTP。Avatar wrapper 保持同 core，不能借未来 Project 端口默许操作。System HTTP facade 显式使用独立 cursor keyring，逐页重验当前管理员；合法 cursor 不授予权限。生产 catalog 只装配一个 `account.mail-enqueue` handler，`account.sessions-revoked` 的未来 WS 消费属于 D25。

持久表由仅 Up 的 `00010_account_session_smtp.sql`、投递增量 `00011_account_mail_delivery.sql` 及头像恢复索引 `00012_account_avatar_recovery.sql` 提供。组合方在事务外准备全部依赖，原业务 Tx 一次取得完整最强锁，再重验当前事实；Secret/Outbox/Audit 通过正式端口写入。Argon2id、随机材料和加密准备不占用业务事务。提交未知核实仍等待原命令 writer；确认失败保留原 `COMMIT_UNKNOWN/unknown` 与安全 cause，不从暂时缺行断言回滚。

## 正式 HTTP 与资料头像

[account OpenAPI](../../../api/openapi/account.json) 固定56个显式method/path、46条路径，均位于 `/api/v1`：

| 表面 | 当前能力 |
| --- | --- |
| `/auth/bootstrap`、`/session`、`/sessions/*`、`/auth/challenges*` | 匿名浏览器上下文、当前 Session、登录/登出和挑战验证 |
| `/password-resets/*`、`/invitations/inspect`、`/invitations/redeem` | 统一恢复回执、链接检查和一次消费，token 只放请求正文 |
| `/me`、`/me/preferences`、`/me/change-password`、`/me/avatar` | 本人资料、主题偏好、改密及头像读写/删除 |
| `/auth/passkeys/*`、`/me/passkeys*` | 部署能力、免邮箱登录、重新认证、凭据管理与密码/邮箱码恢复 |
| `/me/email-change/*`、`/me/email-initialization/*` | 新鲜Passkey后修改邮箱、部署者授权初始化/恢复邮箱 |
| `/system/users`、`/system/invitations*`、`/system/account-settings` | 当前管理员列表、邀请/重发/撤销与账户安全设置 |
| `/system/smtp*`、`/system/mail-jobs*` | 当前管理员 SMTP 配置/取消配置/测试、安全任务状态与人工重试 |

非 Dev 模式的 Host 必须匹配规范化 PublicOrigin，非安全方法要求同源 Origin；两种模式的浏览器/Session 写请求均验证相应 `X-CSRF-Token`。非 Dev 模式的 HTTPS Cookie 为 `__Host-agenteam_session` / `__Host-agenteam_browser`，`HttpOnly; Secure; SameSite=Lax; Path=/` 且无 Domain；本地 HTTP 或 Dev 模式使用非 Secure 的 `agenteam_local_*`，其余 Cookie 属性相同。普通认证失败均不改写Session Cookie，避免另一标签页已登录后迟到GET或写请求401误删新Cookie；显式logout成功仍清Cookie，login/change-password成功仍换发Cookie，无效Session仍按服务端当前事实拒绝。每次当前身份/权限检查不被历史 receipt 或 cursor 替代；业务命令沿 OpenAPI 带幂等键和版本，拒绝未知字段。公共 HTTP middleware 由根只包装一次，响应、Problem 与日志使用同一 request ID。

SMTP PUT/unconfigure、测试发送和人工重试在命令已确认提交后，仍需按当前管理员权限读取安全结果。该后读失败返回错误 Problem，`commit_state=committed`、`retry_hint=replay`，不伪装为成功 DTO，也不声称原写未开始。保留原错误 code/status；只读事务自身的 COMMIT_UNKNOWN 改为 DEPENDENCY_UNAVAILABLE，原诊断仅保留在私有 cause。客户端保留原 method/path/Idempotency-Key/body/version，显式同键重放恢复原 receipt；每次重放重新鉴权，另读当前设置不能替代原 applied_version。普通读取和提交前失败维持各自原语义。 针对这四个命令的真实 PostgreSQL/HTTP 回归在只拦截后读的条件下，验证首次提交、历史同键重放、恢复 receipt 与无重复版本/投递/Audit；普通 GET 失败仍保留读事务语义。该回归不以已运行的旧预览二进制代证新源。

普通资料接口不修改email；邮箱变更新增独立强验证流程，当前实施边界见上节。本人可改 username/display_name/theme；username 按当前 DB 映射解析，改名后旧路径失效且无 alias。头像接单一原始 JPEG/PNG/WebP body，最多 5 MiB、边长 4096、总像素 16,777,216，拒动画/SVG/GIF、截断和尾随拼接；保持比例缩至 512×512 内、不放大，白底合成并重编码 JPEG，丢弃原元数据。命令摘要绑定完整原始 bytes，不能用重编码后的 SHA 替代输入语义。

头像切换、旧引用关门、Audit 与 receipt 在同一真实 Tx；后台按持久 cause 与实际 lease 公平清理，不从取消或年龄猜测实例死亡。GET/HEAD 支持单 Range，逐次核本人当前 exact ObjectID；已建立 reader 的底层 I/O 和 Close/join 真实结束后才释放 lease。它不是匿名 URL，也不是通用 Artifact/下载接口。

Account/Mail 初始化、bootstrap 和恢复消耗同一剩余安全启动预算。健康采样核真实技术状态，不发送邮件。停机先停全部准入，Mail 的 FinishDelivery 和 actual join 必须先于 Core 最终排空；LoginResponse.UseCookie、头像解码/reader 和整个恢复日志 Sink 未 join 时保留共享 guard，DB 最后关闭，详见[生命周期](README.md#停止与退出)。

## 可恢复的真实 SMTP 运行准备

真实 SMTP 使用生产 `cmd/agenteam`。当前已确认的[独立直连契约](../work-items/system-outbound-policy-proxy.md)将 SMTP 作为基础设施，直接连接管理员保存的 host/port/TLS，不受业务出站规则、全局代理或环境代理影响，也不新增 SMTP 专属代理开关；严格证书验证、Secret、当前配置/凭据、账户 SendPermit、权限审计、重试与 Unknown 保持。SMTP 继续复用现有静态系统/部署 CA，既有配置键名与五用途 keyring 不变。

SMTP 独立直连已完成关键 owned 隔离验证，并保数据切换至 `e2039777`；实际活动 binary、manifest、验证范围及恢复入口见[当前状态](../../../.agent-state/current.md)。本轮保留现有账号、Session、SMTP、代理与规则配置、凭据和密钥，无迁移、清库或配置重建。真实SMTP实发继续暂停，本次未验证公网或真实外部SMTP；配置保存、升级或本机可用不等于邮件发送成功。

旧预览测试 binary 的固定 sink resolver 仅供隔离测试，不能承担真实 SMTP 部署；其外部 hostname 拒绝是当时 fixture 的限制，不应通过修改业务规则或 SMTP 密码绕过。以下首次准备说明只用于新实例；既有持久实例按后续 stage 流程升级，不能重新生成其配置或密钥。

最小可恢复组合为独立 PostgreSQL/pgvector、MinIO、宿主生产 Central 和 HTTPS 反向代理。复用已核验的工具与只读镜像即可；每个新部署使用独立数据库、bucket、数据目录、私有配置及端口，不复用旧测试实例的运行数据。TLS 代理保留 canonical Host，`PUBLIC_ORIGIN` 固定准确 HTTPS origin，`DEV_MODE=false`。若保留两个实例同时登录，仅更换同一 IP/hostname 的端口不能隔离 `__Host-agenteam_session` Cookie；应使用不同 hostname，或明确使用独立浏览器 profile。

部署输入以 [central.env.example](../../../deploy/central.env.example) 为唯一字段来源。需先持久保存五用途独立随机32字节密钥（Cursor、Secret、Object download、Account、Knowledge confirmation）、数据库和对象凭据、TLS证书/私钥，再启动二进制；再次启动不得覆盖生成新密钥。程序不自动读取 `.env`，启动管理器须将0700目录下0600配置作为环境加载且不得输出。Account恢复日志、spool及其相邻 `.processes`、机器身份与数据库/对象数据应持续保留；停止App不应删除数据库或基础设施。

可审阅的运行文件最小集合为：私有持久环境/密钥文件、数据库和对象TLS及独立容器配置、同运行UID的恢复日志/spool目录、只启动已核编译版本的Central进程管理配置，以及保留Host的HTTPS代理配置。新数据库由生产二进制执行完整迁移，使用PostgreSQL17.8及vector0.8.1，新bucket由正式初始化绑定store identity。现有 `deploy/dev/init.mjs` 可参考原子发布和不覆盖密钥的做法；现有开发Compose默认DevMode/HTTP与不同MinIO版本，不能原样作为此HTTPS部署。测试fixture的退出DROP/清资源生命周期也不能用作持久启动器。

本轮固定部署的准备入口为 [prepare-persistent-central.py](../../../scripts/prepare-persistent-central.py)：传入工作树外新建的 `--state-dir`、已编译 `--central-binary`、固定 SHA 的 `--minio-binary` 和完整40位 `--source-revision`。脚本只生成私有输入，不启动容器，不连接SMTP；原子发布 `runtime.json`，重复运行只校验并复用，拒绝覆盖不完整目录或改变密钥。所有数据库/对象数据、环境、密钥、凭据和日志均保存在仓库外私有目录，不进入Git。当前固定端口为 Central127.0.0.1:5257、TLS127.0.0.1:5256、PostgreSQL127.0.0.1:5456、MinIO127.0.0.1:9456；TLS代理仍转发新Vue5245。

部署owner先核固定镜像及端口归属，再对私有 `compose.json` 启动 `database`、`minio`，随后以 [manage-persistent-central.py](../../../scripts/manage-persistent-central.py) 的 `check --runtime /absolute/private/runtime.json` 和 `start` 启动Central，最后启动 `web`。不要打印展开后的Compose或环境配置。manager只负责该Central，使用独立session、私有日志和实际PID/start_ticks/executable/二进制摘要确认归属；`stop` 发送TERM并等待真实退出，不删除数据库、对象、密钥、spool或代理。再次 `start` 使用原私有环境。`--check-config` 的 `valid=true, ready=false` 仅表示配置合法，仍需实际启动及API验证。

首次bootstrap从正式私有Account恢复日志提取最小管理员材料至独立0600凭据文件，不输出到普通运行日志。独立验收应在内存中保留一个登录Cookie，只停止/重新启动Central，再验证原Cookie、用户与SMTP配置投影仍有效；确认数据库/对象容器ID与持久密钥未更换。公开切换仅在新实例通过后顺序停止旧映射/旧测试实例，再将原18443映射改接5256；同host旧Cookie在新实例无效，用户重新登录。当前专用CA/叶证书30天，仅适合本次受控预览；没有fixture自动45分钟/24小时销毁期限，进程与证书有效期仍须实际核验。

首次创建独立新实例时，完成环境和入口核验后，用户在新入口的SMTP设置页输入SMTP密码，经正式API存入Secret；不从旧实例导出凭据。先通过真实应用测试API受理，再读取同一job终态；202、SMTP接受、最终邮箱收到仍是三个不同事实。若部署环境无法连接实际SMTP端口，新实例或标准解析器都不能替代出口连通性；单目标无AUTH/MAIL的TLS/EHLO工具见 [probe-smtp-transport.py](../../../tests/account-preview/probe-smtp-transport.py)，它仅证明传输阶段，不证明应用发送。

以下为此前部署与诊断的历史记录，仅代表当时输入，不作为本轮 SMTP 拆分验收或继续真实发送的授权。

2026-10-16此前已实际完成上述持久组合和一次Central优雅停启：独立验证同一Cookie、用户/会话及SMTP投影保留，PG/MinIO容器与密钥不变，logout仍生效。当前公开18443映射已由旧5254测试fixture顺序切至5256；旧App和四套owned设施已清理，Vite5245保留。旧SMTP字段/密码未迁移，用户随后自行在新实例保存完整配置。公网唯一匿名identity读取在管理网络严格TLS校验处失败（CONNECT200、curl60、HTTP0），未取得公网实例标识，不能据本机验收宣称公网验证通过。

用户配置完成并恢复诊断后，生产实例安全投影显示SMTP已配置且有凭据。其已有授权目标测试任务经过原配置的4次attempt，最终failed/network_failed；诊断只观察该job，没有新增POST或人工重试，结束logout204。固定生产入口工具见 [diagnose-persistent-smtp.py](../../../tests/account-preview/diagnose-persistent-smtp.py)：严格私有manifest、活动进程身份、CA/SPKI/实例标识门禁，秘密仅在内存和仓库外0600材料，错误只输出安全分类。旧fixture诊断器不可用于此入口。

此前诊断时Central实际已继承proxy:8080的HTTP(S)代理环境，但该运行版本SMTP保持直连。独立同目标465直连诊断在TCP被拒；显式系统代理探针取得CONNECT200和严格系统信任/hostname TLS成功，随后10秒未收到SMTP220，未进入认证或发信。该探针的代理叶证书不等于已独立核实目标SMTP服务器叶证书；本次无SMTP greeting不外推所有SMTP代理能力。`network_failed`本身不能区分DNS/TCP/TLS阶段，密码正确性与邮箱收到均未验证。当前保留服务并停止追加探针，需要实际可用SMTP网络出口，不能通过重复改密码、无效重启或放宽TLS/出站策略修复已观察到的网络限制。

### 既有持久实例的stage与恢复

SMTP 拆分升级不新增数据库迁移；保留现有数据库、对象数据、账号/Session、用户已保存SMTP、代理与规则配置、五用途keyring、TLS材料、spool与恢复日志。只切换同一Central端口的候选二进制；`prepare-persistent-central.py`仍用于首次准备/原输入校验，不能用新source重建目录或重置已有数据。实际升级须先完成候选功能与恢复门、最终组合构建及完整source/hash核对；当前进度见[恢复状态](../../../.agent-state/current.md)。

管理器新增 `stage`：

```sh
python3 scripts/manage-persistent-central.py stage \
  --runtime /absolute/private/runtime.json \
  --candidate-binary /absolute/private/agenteam-candidate \
  --source-revision FULL_40_CHARACTER_REVISION
```

以输出的 `runtime_file`作为候选manifest。stage只复制候选到私有 `bin/agenteam-<revision>`、检查配置并写独立 `runtime-staged-<revision>.json`，不停止原进程、不更换环境/密钥/数据库，不改原preparation；相同输入可重复，同revision不同SHA拒绝。`--check-config`不证明迁移或服务健康。

切换顺序由运行owner执行：先核真实PID/start_ticks/executable及精确容器/nonce归属、无未终结或未join邮件任务；用当前manifest执行 `stop`并确认实际退出和端口释放。保留数据库/对象/TLS代理/frp，停止应用写入后对原数据库做私有custom-format备份并验证归档可读，记录原配置/密钥和数据身份摘要；备份、stderr及归档清单只存0700目录/0600文件，不打印凭据。随后只用stage返回的manifest执行 `start`，再核实际进程/exe SHA、迁移记录、同端口服务、原账号/SMTP安全投影与数据/密钥保持。

`central-release.json`在候选Popen前持久记录已选source/SHA与manifest路径；后续管理使用其中的活动manifest，以 `python3 scripts/manage-persistent-central.py <action> --runtime /absolute/private/runtime-staged-FULL_REVISION.json`执行所选动作（action取status、stop或start）。本轮SMTP拆分切换保持既有schema145，原 `runtime.json`仅保留首次准备身份；准确活动路径、二进制摘要与验证限制见[恢复状态](../../../.agent-state/current.md)，不能继续使用旧manifest管理新实例。候选可能已提交迁移，因此一旦尝试启动，旧manifest不能再次start/stop/stage；保持所选候选并做兼容的前向修复。旧144及更早二进制不能在当前schema145数据库上回退启动；不自动回旧binary或恢复旧dump。候选进程记录发布失败时，manager只回收自己刚启动的child并实际wait，保留已选release和原错误，不清除未知残留文件；原正常stop超时仍不强杀。数据恢复如有需要必须另核配套schema、binary、备份与对象/密钥事实。

保留原immutable nginx配置、TLS与nonce，`/__instance__/identity.source_revision`继续表示基础设施首次准备版本；它不证明新候选代码版本。新Central用活动manifest、实际PID/start_ticks/executable SHA证明，前端用实际构建/源组合单独核验。旧 `runtime-recovery-probe.cjs` 和 `diagnose-persistent-smtp.py`的identity.source等于runtime.source假设不适用于staged manifest，本轮升级用独立组合核验，不修改nginx来伪造候选身份。严格本机验证、公开入口响应归属和真实邮件结果分别记录；SMTP实测暂停，升级操作不触发测试发送。

### 保留旧后端时固定前端快照

Passkey源码已集成主树，但现有用户预览仍保留e203 Central、schema145及原账号/Session、SMTP、keys和数据，不运行主树新后端或升级该数据库。为避免新页面连接旧API，5245使用仓库外快照 `/workspace/scratch/agenteam-central-persistent-r1/frontend-stable-c09d69b7/`；其92份源码与c09d逐项一致，node_modules与Vite缓存独立。不要再以主树的npm/dev命令启动此保留入口。

快照内 `preview-record.json` 保存绝对node命令、工作目录及required_environment/recovery_environment中的 `VITE_ACCOUNT_ENABLED=true`，私有run-state的frontend_preview登记实际进程；恢复前须核PID/start_ticks和端口归属。此环境变量是必需项，遗漏会将账号入口禁用。恢复命令在该快照目录运行：

```sh
cd /workspace/scratch/agenteam-central-persistent-r1/frontend-stable-c09d69b7
VITE_ACCOUNT_ENABLED=true /opt/codex/runtimes/codex-primary-runtime/dependencies/node/bin/node /workspace/scratch/agenteam-central-persistent-r1/frontend-stable-c09d69b7/node_modules/vite/bin/vite.js --host 127.0.0.1 --port 5245 --strictPort
```

恢复按现有运行owner流程脱离临时工具进程组并写私有日志，记录新实际进程身份后再核严格本机TLS/nonce、快照源码与实际accountEnabled。此次首次迁移遗漏flag已修正；最终匿名浏览器确认账号输入/按钮可用、禁用提示消失、session401/bootstrap200，浏览器与relay退出，无凭据读取、登录或邮件。本机检查不代表公网身份验证。当前准确PID与后端manifest见[当前状态](../../../.agent-state/current.md)；Passkey真实链使用独立owned环境，不复用此用户实例。

## 新 Vue 账号联调

以下fixture说明仅供受控隔离回归，固定SMTP sink不能承担真实邮件部署；当前可恢复公开运行使用上一节的生产Central与TLS5256。

新 `web/` 独立实现账号客户端、Session 与七个页面，页面和恢复语义见[账号重建工作项](../work-items/frontend-account-rebuild.md)。当前 HTTP 能力覆盖这些页面，无须新增同义 API。新客户端先调用匿名 bootstrap，用 browser CSRF 登录，再 GET session 取得会话 CSRF；本人资料/偏好/头像共享 User version。改密成功换发新 Cookie，若返回丢失须重新登录；不能用旧已撤销 Session 重放取回 Cookie。

本轮隔离服务入口为 [run-account-preview.sh](../../../scripts/run-account-preview.sh) 与测试专属 [App fixture](../../../internal/central/app/account_preview_fixture_test.go)，不改变生产身份入口。使用固定 PostgreSQL、MinIO、真实 App/Account/Mail 与只向自有私网 sink 投递的 SMTP；不读取部署数据库或邮件凭据。运行前按本页依赖说明准备固定工具链、镜像和 MinIO binary。示例只启动本地服务，未创建公网映射：

```sh
AGENTEAM_GO=/workspace/toolchains/go1.27.1/bin/go \
AGENTEAM_MINIO_BINARY=/task-owned/cache/minio \
GOMAXPROCS=2 GOFLAGS=-p=2 sh scripts/run-account-preview.sh
```

脚本新建0700私有目录，就绪后写入0600的 `runtime.json`，仅向受信测试调用方提供本轮 origin、证书/SPKI、临时账号与私有 SMTP 控制材料。默认 `https://localhost:5250` 将正式 `/api/v1` 转给真实 App，其他页面转给 `http://127.0.0.1:5245` 的新 Vue。App 关闭 DevMode，保留 Host/Origin/CSRF 与 Secure Cookie；浏览器只信任本轮精确证书/SPKI，不关闭全局证书验证。`runtime.json` 不入版本控制、普通日志或截图。

SMTP fixture 默认只记录计数/摘要；显式 `Scenario.CaptureMessage` 才保留最后一封已完整收到且250响应实际写回的邮件。`UseLastMessage` 经已核所有权的私网与 nonce 控制通道借用正文，回调返回即清本地副本，替换清旧正文；邮件正文不进普通 State/日志，不提供生产取回接口。新浏览器验收从此真实 SMTP 正文提取一次性链接，不能将 `accepted:true` 或其他恢复渠道拼接成邮件已送达。该 sink 只证明受控 SMTP 接受，不证明外部邮箱投递。

本地轮次默认最多45分钟；仅下述独立公网实例可显式选择24小时。停止时创建私有目录下的 `stop` 文件，或向该目录 `launcher.pid` 对应的原脚本发 SIGTERM。脚本等原 App、SMTP、PG、MinIO 退出和精确清理；不要直接终止下层测试进程来代替资源清理。下一轮须重新启动并核实，旧 manifest 不证明服务仍在运行。

### 历史测试fixture的IP直连HTTPS入口

本轮公开入口固定为 `https://154.12.86.233:18443`。IP已由域名DNS及现有18081公网页面内容一致性核实。采用浏览器TLS → frp TCP18443 → 本机TLS5254直通，不需要修改服务器Nginx或配置服务器证书；原443应用、`/~!frp`及18081纯前端预览保持。公网实例使用独立任务数据库、对象及SMTP资源，本地5250联调不会被重启或更换PUBLIC_ORIGIN。

公网实例的显式启动输入如下；这只准备本机服务，不建立公网映射：

```sh
AGENTEAM_GO=/workspace/toolchains/go1.27.1/bin/go \
AGENTEAM_MINIO_BINARY=/task-owned/cache/minio \
AGENTEAM_ACCOUNT_PREVIEW_PORT=5254 \
AGENTEAM_ACCOUNT_PREVIEW_LIFETIME=24h \
AGENTEAM_ACCOUNT_PREVIEW_ORIGIN=https://154.12.86.233:18443 \
GOMAXPROCS=2 GOFLAGS=-p=2 sh scripts/run-account-preview.sh
```

fixture只允许5250/5254及明确列出的HTTPS origin，公开证书的IP SAN必须匹配154.12.86.233。App始终关闭DevMode、使用Secure/HttpOnly/SameSite Cookie并严格核Host/Origin；first-chain自检直接拨本机TLS listener，但URL、SNI、Host与Cookie jar按选定origin工作并验证本轮CA。临时专用CA不受浏览器公共信任，用户访问会遇到该站点的证书提示；不修改系统信任或全局禁用证书检查。测试浏览器只信本轮精确SPKI/CA。

24小时模式要求显式预览开关、端口5254与上述精确时长同时成立；脚本拒绝其他时长及本地5250延长请求。该模式将本轮页面TLS、PostgreSQL、对象、SMTP及私网outbound的专用CA/叶证书有效期设为25小时，普通fixture期限不变；测试进程预算为24小时10分钟，App在24小时后进入正常退出与精确清理。实际对外可用截止以本轮私有 `runtime.json` 的 `expires_at` 为准。环境重启、进程退出仍会提前结束预览，不代表持久部署。

只有本机真实链与安全门槛通过后才创建任务唯一frp映射。以下 `--check` 只验证配置，不建立连接：

```sh
python3 /workspace/agenteam-onboarding/frp/run-preview.py \
  --server-addr mcpc-terra.lunadeer.cn --server-port 443 \
  --local-port 5254 --remote-port 18443 \
  --name account-preview-20261016 --check
```

实际启动使用同一助手去掉 `--check`，继承既有代理及系统CA验证，不输出或复制FRP认证值。供用户持续使用的隧道须脱离短生命周期工具的进程组，并关闭继承的工具管道；以下方式与本轮恢复相同。先选定本轮0700私有目录和未占用的唯一代理名，仅在确认没有该轮活跃代理时执行：

```sh
export AGENTEAM_ACCOUNT_PREVIEW_DIRECTORY=/task-owned/private-preview
export AGENTEAM_ACCOUNT_PREVIEW_TUNNEL_NAME=account-preview-unique-name
python3 - <<'PY'
import json, os, pathlib, subprocess
directory = pathlib.Path(os.environ["AGENTEAM_ACCOUNT_PREVIEW_DIRECTORY"])
assert directory.is_dir() and not directory.is_symlink()
assert directory.stat().st_mode & 0o077 == 0
assert not (directory / "frp-detached-process.json").exists()
os.umask(0o077)
args = ["python3", "/workspace/agenteam-onboarding/frp/run-preview.py",
        "--server-addr", "mcpc-terra.lunadeer.cn", "--server-port", "443",
        "--local-port", "5254", "--remote-port", "18443",
        "--name", os.environ["AGENTEAM_ACCOUNT_PREVIEW_TUNNEL_NAME"]]
with (directory / "frp-detached.log").open("xb", buffering=0) as log:
    child = subprocess.Popen(args, stdin=subprocess.DEVNULL, stdout=log,
                             stderr=subprocess.STDOUT, close_fds=True,
                             start_new_session=True)
fields = pathlib.Path(f"/proc/{child.pid}/stat").read_text().rsplit(")", 1)[1].split()
with (directory / "frp-detached-process.json").open("x") as output:
    json.dump({"pid": child.pid, "session_id": int(fields[3]),
               "start_ticks": fields[19], "argv": args}, output)
print("Detached helper PID:", child.pid)
PY
```

启动命令退出后，在后续独立检查中核 `/proc/<pid>/stat` 状态不是 `Z`/`X`、`start_ticks`与登记一致、helper为独立session leader，并确认实际命令及子frpc身份。`kill(pid, 0)`、存在 `/proc/<pid>` 或历史login日志都不能证明进程存活：本轮在工具会话结束后观察到属于原工具进程组的旧helper/frpc已SIGKILL退出，遗留僵尸PID及成功启动日志。持续提供服务的App启动脚本也应使用独立session和私有日志，避免外层launcher被终止而丢失真实wait结果；已有App仍活跃时不要盲目重复启动。停止时只向身份仍匹配的本轮helper发SIGTERM，核子进程退出和精确私有TOML清理；已死进程遗留配置须按该轮精确路径处理，不遍历或终止其他映射。

WSS验证的是隧道入口，页面TLS由本机应用另行提供；助手的 `type=tcp` 传输不终止HTTP或TLS。先核本次frp登录与代理启动及真实工作连接，再从 `https://154.12.86.233:18443` 验证本轮identity marker、正式登录旅程、Cookie及Origin/CSRF拒绝边界，才能向用户交入口。配置通过、TCP连通或本地自检都不能代替公网验真。预览有本轮证书与资源期限；结束后依本页方法真实退出、清理或重新创建自有轮次，不把旧manifest当持续存活证明。

本轮环境的管理HTTP代理会终止客户端TLS，并因不信任预览专用CA而返回上游 `CERTIFICATE_VERIFY_FAILED` 503；其为纯IP签发的DNS类型SAN也与服务端真实IP SAN不同。客户端信任或固定代理证书不能改变代理到服务端的信任策略。遇到该明确结果后保留服务与隧道，停止重复请求或扩大TLS例外；只能记录本机TLS/业务验证、frp登录/端口分配已过，公网归属及浏览器交互仍待用户直接访问验证。浏览器固定本机路由的同origin验证可证明Cookie行为，但不能替代公网验真。

## 邮箱 canonical 与 SMTP wire

邮箱仍采用1–254字节的裸 ASCII addr-spec，整体小写，不 trim、去点、去 tag、IDNA 或压缩 IP 文本。令 `S` 为本次修复前正式 `NormalizeEmail` 接受的集合，现在只接受 `S ∪ lower(S)`：旧合法输入的 canonical 字节不变，并允许其完整小写结果再次输入。原 `IPv6:` literal 的小写 canonical 可往返；如 `iPv6:`、`IPV6:` 的其他标记，以及使用 `ipv6:` 标记但其他部分未全部小写等原不合法输入仍拒绝，详见[闭包修复规格](../work-items/d07-email-canonical-roundtrip.md)。

数据库身份、SMTP GET 和命令语义始终保留小写 canonical。worker 在原消息准备边界验证材料，只在私有 SMTP wire 副本将实际 literal 标记 `ipv6:` 改为 `IPv6:`；MAIL FROM、RCPT TO、From、To 共用同一对已验证地址，其余字节与显示名安全编码保持。wire 不回写身份或持久材料，旧唯一性、命令 MAC、历史回执和发送预算不变；合法输入等价不能用于改写前端已冻结的 pending 请求。

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

邮箱闭包的补验包括受影响 Account/Accountmail 纯测与 race、正式 SMTP PUT→GET→仅编辑 host→PUT、原命令重放与当前配置区分、TestSMTP 二次规范化、邀请/登录/真实挑战/reset 代表，以及四种地址的实际 SMTP 信封/邮件头与材料释放；原 none/STARTTLS/TLS 三模式回归保留。作者三个新顶层在 `new-account01` 与 `new-wire01` 两轮通过，旧回归另在 `old-smtp01` 通过；独立验收组合原轮已通过子例与修正私有夹具前提后的复跑，不称同轮全绿，原失败永久保留。精确命令、输入和实际退出/双清见[邮箱闭包验收记录](../agent-team/email-canonical-roundtrip-verification.md)。历史回执在候选版本内创建，不是跨二进制升级实证；受控 SMTP 接受不等于外部邮箱送达，本次也不证明 SMTP UI 或整个 D27 已通过。
