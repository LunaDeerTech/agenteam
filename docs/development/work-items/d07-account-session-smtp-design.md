# D07 账号、Session、SMTP 与个人资料实施规格

- 修订：6（已采纳设计）；在修订5基础上补B03账户投递端口、共享准入门禁、真实Secret使用授权及00011增量。B02已独立验收，基线 `ebe87e7`；本稿不代表B03实现或测试通过。
- 范围与所有权：[D07 主卡](d07-account-session-smtp.md)。本规格落实已确认的[账号生命周期](../../architecture/platform-infrastructure/authentication/account-lifecycle.md)、[SMTP](../../architecture/platform-infrastructure/authentication/smtp-delivery.md)、[D01 基础契约](d01-contracts/foundation.md)；不新增公开注册、角色管理、账号删除、设备管理或前端生产页面。
- D07 交付真实后端、HTTP、账户安全和后台投递。D08 仍拥有 Project/Owner；D25 接 Session 撤销与 WS；D26 接账号/个人页面，D27 接系统设置。挑战 Vue 只交独立兼容测试 harness。

## 1. 结果分块、文件所有权与固定依赖

按 B01→B02→B03→B04 顺序实施、各自可构建及独立验证；后块只在前块冻结后读其源码。主线程下发精确授权并负责提交，不把本表当源码已解冻。

| 块 | 完整结果 | 新增目录/文件与已有文件的限定增量 |
| --- | --- | --- |
| B01 身份与安全基础 | 真实 User/Session/System provider、初始化、登录/退出、密码与 keyring、受限 Secret/Audit 组合；无 HTTP 默认授权 | 新 `internal/central/account/contract/{types,identity,settings,commands}.go` 及对应测试；新 `account/{repository,authority,planning,keyring,password,bootstrap,session,login,audit_authority,secret_authority}.go` 及测试、`account/assets/weak-passwords.json`/许可；新 `internal/central/recoverylog/` 真实受限 Sink/测试；新 `db/migrations/00010_account_session_smtp.sql`；本节列明的 D04 补口、`go.mod/go.sum` 的必要依赖；新 `tests/account/` 基础/迁移测试 |
| B02 邀请、恢复与挑战 | 一次性兑换/重置、改密、挑战、事务投递意图与真实 Outbox handler、到期回收 | 新 `account/{invitation,reset,password_change,challenge,events,delivery_intent,delivery_handler,cleanup}.go`、`contract/{challenge,invitation,recovery,events}.go` 及测试；自有生成挑战素材；新 `tests/account/` 对应组合测试、`tests/account-captcha-web/` 独立锁定依赖 harness |
| B03 持久投递 | SMTP 配置/测试、三种协议、有限重试、受限恢复日志、claim/未知结果/重启与关闭 | 新 `internal/central/accountmail/` 与测试；新 `account/contract/delivery.go`、`account/{delivery,delivery_repository,delivery_usage,mail_admission,smtp_settings}.go` 及相邻测试；本节八个旧account文件窄增量；新 `db/migrations/00011_account_mail_delivery.sql`；§9限定 `recoverylog/sink.go`、`sink_test.go`，可新增 `recoverylog/{admission,ticket}.go` 及测试，补队首资格与per-work ticket，不改B01 bootstrap；限定出站TLS补口；新 `tests/testsupport/smtp/`、`tests/accountmail/`、`scripts/test-accounts.sh`；限定 `tests/testsupport/postgres/cmd/fixture/main.go` 仅向固定go test包列表追加 `./tests/accountmail/...`、`./internal/central/accountmail/...`，原包列表、fixture生命周期/nonce/权限/参数/6m不变；`scripts/test-accounts.sh` 复用既有 `scripts/test-objects.sh` 路径，无新runtime route；上述旧源码仍须root在B03正式解冻 |
| B04 资料与正式入口 | 本人资料/头像/偏好、完整 API/OpenAPI、Central 生命周期与当前权限装配 | 新 `account/{profile,avatar,object_authority,runtime,http,csrf}.go`、`contract/profile.go` 及测试；新 `api/openapi/account.json`；新 `app/account.go` 及实际进程测试；限定 `app/{app,security,outbound,object,outbox,resources,health,diagnostics}.go`、`config/config.go` 及受影响测试/fixture；新账户配置示例与运行说明须由主线程另授权文档范围 |

表内 `account/`、`audit/`、`secret/`、`object/`、`outbound/`、`foundation/`、`httpapi/`、`app/`、`config/` 均为 `internal/central/` 下包路径；`db/`、`tests/`、`api/`、`scripts/`、`go.mod/go.sum` 相对仓库。同目录新增私有辅助文件可由所属 B 作者选择，不能因此改未列旧域。已提交 `00010` 声明D07基础结构；B03仅由新事务型Up-only `00011_account_mail_delivery.sql` 补§3已证缺口，旧 `00001–00010` 字节不变。B01/B02 未装 HTTP 前，缺后块 capability 必须明确 unbound，不能在生产返回成功占位。

B03额外旧account授权仅限下表及对应新增窄测试；均在 `internal/central/account/`。`service.go/planning.go/invitation.go/reset.go/recovery.go/mutation_recovery.go/link_authority.go/audit_authority.go` 不因此解冻；共同helper和新文件承载其余行为，不跨域直接更新Secret表。

| 旧文件 | B03精确增量 |
| --- | --- |
| `authority.go` | 私有单实例mailAdmission及构造，不改Session/System规则 |
| `secret_authority.go` | AccountDelivery完整usage/当前授权及SMTP reference新helper分派，保留AccountResponse |
| `cleanup.go` | expire/revoke外层EX、removeLinkInTx消费同实例EX guard、取消retry_wait等未准入job；不伪造active attempt终局 |
| `link_commands.go` | RedeemInvitation最终消费Tx外EX并传私有guard |
| `reset_complete.go` | CompletePasswordReset最终改密/消费Tx外EX并传guard |
| `password_change.go` | ChangePassword最终password_version/Session/Audit/事件Tx外EX |
| `delivery_intent.go` | 正式test recipient variant/current producer校验；容量查询覆盖新job phases |
| `delivery_handler.go` | 正式test enqueue/current配置检查；保持邀请/reset与canonical唯一job |

固定 Go `1.27.1`/`GOTOOLCHAIN=local`；`golang.org/x/crypto v0.55.0` 从既有 indirect 转 direct，不升级旧版本。GoCaptcha 固定 `github.com/wenlng/go-captcha/v2 v2.0.5`（Apache-2.0），`golang.org/x/image v0.45.0`（BSD-3-Clause，用于 WebP 解码/缩放及 GoCaptcha），唯一额外间接模块为 `github.com/golang/freetype v0.0.0-20170609003504-e2365dfdc4a0`；不选会升级既有依赖的 x/image `v0.46.0`。Vue harness 固定 `go-captcha-vue 2.0.7`（MIT）、Vue 3.5.43、Vite 8.3.1、TypeScript 5.9.3、jsdom 27.4.0；不修改产品前端依赖。真实浏览器测试推荐固定 `@playwright/test 1.56.1`（Apache-2.0）及该发行对应Chromium，仅进入harness lock；此测试工具未由R01验证，B02先核官方integrity/实际浏览器可执行性并记录版本，再运行，不能以jsdom替代或安装浮动latest。引入前将 R01 的完整模块图、包校验和及 LICENSE 归档，拒绝无关 MVS 升级。B02实际固定1.56.1配套Chromium141.0.7390.37/build1194官方下载403后，允许使用现有系统Chromium151.0.7922.173执行真实harness：记录可执行路径、实际版本、SHA256与启动/交互结果，明确该组合不是Playwright发行配套；不改下载限制、不继续无界重试。若API或真实交互不兼容则停止相关验收报告，不以jsdom补足。

弱密码仅离线嵌入 `@zxcvbn-ts/language-common@4.1.3` 的 `src/passwords.json`，49,233 项、486,625 bytes，SHA256 `f422773d94d630f27e08b26d7017e847bb1feb2a84809340e9c0b0c47b33e3f1`；保留 MIT 许可/来源，不引 zxcvbn JS runtime，不请求泄漏库。GoCaptcha assets 1.0.7 的字体/照片未取得逐素材权属证据，首版不用该包；程序生成平台自有的非对称图案，不引用系统字体或远端图片。SMTP 使用标准库 `net/smtp`、`crypto/tls`、`net/textproto`，不另引 SMTP SDK。

下表未单独标注所属块的旧公共增量由 B01 一次完成；B03另限本节八个account文件、Sink及出站TLS补口，B04按表内限定增量绑定；旧消费者的拒绝行为须兼容验证：

| 旧文件/新补充文件 | 必要原因及最小口 |
| --- | --- |
| `identity/contract/identity.go` 及测试 | 封闭注册 `account-bootstrap`、`account-auth`、`account-maintenance`、`account-mail`；仍无匿名 Human、默认管理员或通用 Service 权限 |
| `audit/contract/{types,metadata}.go`、新 `audit/contract/account.go`、`audit/service.go` 及对应测试 | 本节 §4 的封闭动作/资源/producer、typed metadata 与 optional `AccountAuthority`；本人动作不走系统管理员捷径；旧 append/query 权限不放宽 |
| `secret/contract/types.go`、新 `secret/contract/account.go`；`secret/{service,write,lease}.go`、新 `secret/{account_write,usage_plan}.go` 及测试 | 受限服务写、Prepared 安全投影、新 lease owner；§4 的正式 DiscoverUsage/ApplyUsageInTx 与闭集 request/result、opaque deps及legacy拒绝；`contract/account_test.go`、`usage_plan_test.go`及lease/账户组合测试覆盖计划消费，不扩迁移；保留原 Human mutation 的意义、nonce/AAD/提交语义 |
| `outbound/policy.go` 及测试 | 正式 Human Session 绑定后，把 User SH 纳入 Get/Update/unknown lookup 的初始完整锁集合，不能授权时补低序锁 |
| 新 `outbound/smtp_tls.go` 及测试 | `TrustStore.SMTPClientTLSConfig(host string) (*tls.Config,error)`：仅受信配置根、精确规范 host、TLS≥1.2、不可跳校验；返回克隆不导出 roots；D07 不接受 caller 的任意 TLS 配置 |
| 新 `object/download_keyring_material.go` 及测试 | `DownloadKeyring.ContainsMaterial([]byte) bool` 常量时检查全部历史材料，不导出密钥；用于 ACCOUNT_KEYRING 四用途隔离 |
| B04 新 `object/contract/reference_cleanup.go`、`object/reference_cleanup.go`；限定 `object/contract/access.go`、`object/access.go`、`object/{references,upload}.go` 及相邻/真实组合测试 | §10 的 Avatar 已发布但尚未切换窗口；仅新增正式 cleanup-release variant/端口及旧 publish/consume 读取相同不可逆 gate，不放宽普通 Avatar grant或其他 owner |
| `foundation/fault.go`、`httpapi/problem.go`、`api/openapi/common.json` 及对应测试 | 新 `CHALLENGE_REQUIRED`（401）与 `CHALLENGE_INVALID`（400），提供统一机器判别；其余错误复用既有码+安全 FieldError，不回显输入 |

## 2. 工程参数与部署输入

邮箱接受单个裸 ASCII addr-spec，最长 254 bytes，拒 display name、注释、控制符与首尾空白；本版 canonical 为 ASCII 全部小写，不去点、不去 `+tag`。邀请/注册/登录/重置使用同一函数及唯一索引；email 永久只读。username 为 3–32 个 ASCII 字符，`[a-z0-9](?:[a-z0-9-]*[a-z0-9])?`，输入英文字母折为小写，无 trim；保留 `api, assets, auth, login, logout, invite, reset, settings, system, personal, diagnostics, livez, readyz, debug, support, root, admin`，bootstrap 的 admin 是唯一显式例外。保留字和 DB 唯一性两层验证。

display_name 为 0–80 Unicode code points、至多 320 UTF-8 bytes，拒控制符，不自动 trim，空串表示使用 email；主题只接受 `system|light|dark`。密码先验证 UTF-8，按 code points 计 15–128，最多 512 bytes；不 trim、NFC、截断或强制组合。弱表按原值及 Unicode 小写值匹配，验证不修改真正用于哈希的字节。49,233 项中长度≥15仅41项，不能宣称长口令全面覆盖。补充有限本地弱模式：完整密码为1–4码点短片段的精确重复（含单字符/空格重复），或完整ASCII小写副本是 `0123456789`/`abcdefghijklmnopqrstuvwxyz` 及反向序列的循环连续子串，均拒绝；其余不做“必须混合字符”推断。`123456789012345`、重复 `你好` 拒绝；非词表的 `一杯热茶配两本喜欢的书慢慢读`、`a long phrase with spaces` 不因单类字符/空格被拒。

Argon2id PHC 编码固定 `$argon2id$v=19$m=65536,t=3,p=4$<salt>$<hash>`，随机 salt 16 bytes、输出 32 bytes、标准无填充 Base64。比较常量时；未知邮箱执行同参数 dummy hash。hash/verify 在 Tx 外，进程并发 2、排队最多 16、等候最多 2s；满额 429 是计算容量，不是登录冷却。库计算不可中断时继续跟踪到实际结束，不能提前释放 slot/ProcessGuard。首版PHC白名单就是该精确参数，编码总长≤256bytes；坏格式/超界不按 DB 声明分配无限内存。后续显式支持旧参数升级时，成功验证后重新哈希并锁后核 password_version，永不因旧验证结果覆盖并发改密。R01 的 Go1.27.1/linuxamd64/4CPU有界测量：单并发中位83.727ms，并发2中位149.018ms；不是生产SLO，128MiB活跃Argon内存外仍需GC/应用余量。

账号安全设置是 DB singleton+version，管理员写入及 Audit 同 Tx；字段使用秒整数字符串。下表更改仅影响随后签发的 Session/token，已签发对象保存原期限；挑战阈值下一次尝试读取当前值。

| 字段 | 默认 | 合法范围 |
| --- | --- | --- |
| `session_idle_seconds` | 604800（7d） | 900–2592000（15m–30d），不超过 absolute |
| `session_absolute_seconds` | 2592000（30d） | 3600–7776000（1h–90d），不小于 idle |
| `password_reset_seconds` | 1800（30m） | 300–7200（5m–2h） |
| `challenge_after_failures` | 5 | 1–20；不关闭挑战 |
| 邀请寿命 | 86400（24h） | 固定，无配置字段 |

沿已有 `PUBLIC_ORIGIN` 规范化；认证正式部署要求 HTTPS。仅字面 `localhost`/loopback 地址允许本地 HTTP，使用不同的本地 cookie 名且不声明 Secure；非 loopback HTTP 在 D07 账户初始化失败。不能依据反向代理/Host/X-Forwarded-* 生成邮件链接或推导安全模式。外部 TLS 终结反代须保留 canonical Host；客户端 IP 默认仅 RemoteAddr，不信任任意 forwarded 头，反代共享地址只影响挑战计数，不改变授权。

新增 `AGENTEAM_CENTRAL_ACCOUNT_KEYRING` 必填，格式沿 cursor keyring 的 `format:1/current_kid/keys[{kid,key_b64}]`，1–32 把独立随机 32-byte key。载入时验证 Cursor/Secret/Download ring 都有效并排除当前及历史材料复用、重复 kid/raw key；一般 Format/JSON/log 只有固定标签。`account.LoadKeyring(raw,cursorRing,secretRing,downloadRing)` 不导出原始材料。

HMAC-SHA256 输入有固定版本、用途前缀及长度编码：`command-v1`（含密码的规范命令）、`anonymous-cookie-v1`、`csrf-v1`、`privacy-counter-v1`。kid 随 receipt/匿名 cookie/Session CSRF 事实持久化；HMAC 不是可读密码存储。持久 registry 记录 kid→key fingerprint，拒同 kid 换材料。新 kid 只用于新事实；所有未清 receipt、未绝对到期 Session、未过期匿名上下文/隐私计数需要的旧 kid 均保留，删除仍被引用 key 启动失败，运行缺 key 为 503，不当作密码错误或悄悄重建。匿名 cookie 最长 1h；其终局请求 receipt 24h 后才可删且原 cookie 必已失效；Human 命令成功 receipt 不按短 TTL 删除，保留所需 kid。容量/轮换失败明确报安全字段名，不打印 env 值。

新增 `AGENTEAM_CENTRAL_ACCOUNT_RECOVERY_LOG` 为专用绝对文件路径，必须处于平台独占 0700 目录，regular file、0600、当前服务 UID、禁止符号链接，打开及校验失败阻断账户初始化。它是明确授权的敏感后台恢复日志，不能复用普通 slog/stdout/stderr；备份、读取权限与保留由部署操作者管理。文件滚动不由请求指定；重新打开只用固定部署路径。B04 必须补配置/进程 fixture 所需新 ring 和私有文件，不能让已有测试隐式依赖真实环境。

## 3. 持久事实、唯一性与事务纪律

`00010_account_session_smtp.sql` 为事务型、Up-only，schema `agenteam_account`；ID UUIDv7、version/sequence 正 bigint，时间 `timestamptz(6)`；API 计数沿 foundation decimal-string。仅 Secret 表存密文材料，账户表不存密码明文、Cookie、CSRF、链接 token、SMTP password、邮件正文或挑战答案。

| 表/关键事实 | 约束及索引 |
| --- | --- |
| `users` | id、immutable email、username、display_name、role=`admin\|user`、PHC/password_version、version、auth_sequence、initial_password_suggestion、theme、avatar_object_id、created/updated；email/username 各 canonical UNIQUE；avatar 引用只指 D05 已发布对象 |
| `bootstrap` | singleton、operation_id、user_id、creator_process_id、completed_at；log_state=`eligible\|attempted\|written\|failed\|unknown\|abandoned`、log_attempt_id；创建与首次 User 同 Tx，不存初始密码/可重发材料 |
| `account_settings`、`smtp_settings` | 各 singleton+version；前者 §2；后者 §8，未配置用显式 configured=false，非空错误配置不能视为未配置 |
| `sessions` | id、user_id、token_verifier(32B UNIQUE)、csrf_kid、issued_at、last_activity_at、idle_seconds、absolute_expires_at、revoked_at/reason；索引 user_id+active；token verifier 为 domain-separated SHA256(随机32B) |
| `commands`、`auth_attempts`、`reset_requests` | command namespace/稳定主体/key UNIQUE，semantic_kid+HMAC、业务关联 IDs、安全 receipt、可选短期 response_secret_ref；attempt ID、种类、真实 nullable user_id、安全结果/时间；`response_read` attempt另持 §4 的完整绑定、process/fence、lease ID/phase，非原LoginAttempt；reset request额外持私有canonical email供异步当前查找、browser/command/phase，24h终局后清；不保存原IP或密码摘要裸SHA，不向DTO/Audit投影私有email |
| `invitations`、`password_resets` | ID、email 或 user_id、verifier、material_ref、created/expires、version；每 canonical email/user 至多一行；到期先同锁删除才新建，不用 `now()` partial index；消费/撤销删 live row，完成 receipt 无 token/材料 |
| `auth_failures`、`challenges` | HMAC subject/IP key+kid，计数/窗口；challenge ID、process_id、browser/目的/email HMAC/login key 绑定、pass verifier、expires/phase；答案与原图只在有界私有内存，不明文落库；索引 expiry，数量有界，进程更换后旧challenge/pass失效 |
| `delivery_intents`、`mail_jobs`、`mail_attempts` | intent ID/预分配job_id/用途/link ID/请求发起者稳定 ID/created，只有test intent持私有recipient；job UNIQUE(intent_id)、phase、attempt数、next_at、pass、version、安全结果；attempt ID、job/fence/process_id、配置version、credential/token lease IDs、实际阶段/结果；不保存 URL/收件正文 |
| `avatar_changes`、`avatar_cleanup`、`material_cleanup` | 上传命令/目标 User/新旧 Object/进度与 cleanup cause；材料待断引用/删除及 lease 收敛事实；公平 pass+ID 批索引，终态最小 receipt |
| `account_key_registry` | kid、fingerprint、最后签发/引用边界；不含 key；同 kid 同材料不重复注册 |

`00010` 同时精确扩 Audit action/resource/producer/service CHECK 和 Secret lease owner CHECK，保留 00009 后所有合法值；新增分支显式处理 NULL，不能靠 SQL CHECK UNKNOWN 放行。Secret 仅追加 `account_delivery_attempt` 与 `account_response` owner（分别实际投递尝试ID和独立回应读取尝试ID，后者不是LoginCommandID）；不冒充 Execution，不改旧 scope/引用/密文/AAD 规则。账户邮箱/用户名冲突以命名 UNIQUE 转安全字段错误，不能输出 SQL/原值。

`00011_account_mail_delivery.sql` 仅作以下事务型增量，所有历史行原业务事实、ID/ref/版本保持，不执行网络或补发：

| 表/索引 | 00011与B03固定语义 |
| --- | --- |
| `smtp_settings` | 新 `sender_name text NOT NULL DEFAULT ''`，`char_length<=80`且未configured时必须空；新 `auto_retry_count bigint NOT NULL DEFAULT 3 CHECK 0..5`、`retry_interval_seconds bigint NOT NULL DEFAULT 60 CHECK 10..3600`。旧行补这些默认值，不改configured/enabled/凭据；头字段仍应用层拒CR/LF/control，无用户名/密码新列 |
| `mail_jobs` | phase CHECK加 `claimed,sending,retry_wait`，保留原 `processing` 仅用于旧数据恢复，其他原合法值不变；attempts CHECK从0..5改0..6，表示本job实际claim总数，首次+最多5次自动重试。当前auto_retry_count决定准入上限，不能把DB上限6当固定重试策略；人工retry仍新intent/job周期 |
| `account_mail_fair` | 同一新migration原子替换原仅pending的部分索引，保留键 `(pass,next_at,id)`，predicate改 `phase IN ('pending','retry_wait')`；扫描真实due时间，protected/foreign live轮转而不霸占前100 |
| `mail_attempts` | 新nullable `token_ref uuid`、`credential_ref uuid`（安全Ref ID，无密文/跨域FK/默认猜值）；各CHECK为该Ref非NULL则相应leaseID必须非NULL，旧行Ref=NULL保留。新claim必须同Tx捕获当前exactRef与预分配leaseID，材料槽成对存在：test无token、log或无认证无credential；有材料而缺Ref的新路径拒绝。复用job/fence/process/config_version、phase/result/io_joined/channel/terminal及UNIQUE(job,fence)，不建第二套attempt |

mail_attempts两Ref列补足本次组合核对发现的恢复缺口：仅leaseID+当前config无法可靠恢复多次替换后的原Ref，00010中没有这些列。新attempt的Ref一经捕获不可修改；唯一旧行补值例外沿§4正式exact lease核实，不能用ALTER DEFAULT/当前singleton回填。

B03不新写processing；安全DTO把旧processing投影unknown，恢复读取其exact current_attempt/fence/process/实际phase，未证实终局不接管、不猜成未发送。缺失/矛盾绑定安全拒绝并保留待恢复事实，不能由迁移补造attempt/lease/成功。历史pending/0、终态及合法processing行升级保留；迁移错误整轮回滚。未完成容量统计包括pending/claimed/sending/retry_wait/unknown及旧processing；新阶段不绕过10000上限。新SQL使用明确命名CHECK且显式非NULL/NOT NULL，不依赖CHECK UNKNOWN放行。

所有 HTTP 写带 `Idempotency-Key`；成功 receipt 记录稳定结果，失败 Audit 不冒充成功。Human 主体为 UserID（不含 SessionID）；匿名写主体为已验证 anonymous browser ID+目的，真正 token 命令另绑定 token ID；后台为注册服务+持久 cause。HMAC 规范输入包含动作、稳定目标、expected_version、全部业务字段/实际密码字节，排除 HTTP trace/CSRF/网络时间。raw avatar 的摘要核完整输入 bytes，不能只比 MIME/长度。

初始计划一次合并 Command EX→SystemConfig（account-directory/security/mail/secret-write-key/outbox-registration）→所有 User→CredentialRef→Object→record 锁；每个 key 预先取足 SH/EX。目录写用 `SystemConfigLock("account-directory")` EX，读映射 SH；账户 Session/密码/资料写用 User EX，当前 Session 校验用 User SH。外层唯一次 `AcquireAll`，有对象时由 `AcquireAccessPlansInTx(plans,extra)` 完成唯一 union；Outbox 加其 SH 注册屏障。内部 `RequireHeldLocks` 校验不得补锁/升级。

authority/planner 先在 Tx 外只读收集真实身份/secret/ref/job 映射，不算授权；完整锁后重读映射、当前权限→幂等→version/新事实。映射变化整体 NotCommitted/RESOURCE_BUSY，调用方重规划；不在已有 Tx 回头读资源并追加低序锁。Argon、图片解码、Secret nonce 预留及所有 SMTP/MinIO/日志文件 I/O 均在业务 Tx 外。mailAdmission始终先于DB Tx/DB锁取得；禁止Validate/已开Tx内部再等内存门禁。发送SH与失效mutation EX共享同一Authority实例，完整DB锁/权限校验仍不可省略，见§8。

`WithinTx` 的 callback 成功不代表提交；只认 Committed。Unknown 以原命令/attempt fence 持相同 writer 锁等原事务终局，再查完整语义及 canonical receipt；没看到事实且未证明旧事务结束不能重复 hash 后覆盖/签发新 token/发送。异义 unknown 查询不得返回旧成功。响应发出前确认；确认自身 Unknown 仍不开外部 I/O。没有通用结果仓库。

## 4. 正式身份、Audit 与 Secret 组合口

`account.Authority` 只依赖 Store、keyring、当前账户事实，不依赖 Audit/Secret/Object/Outbox 以避免构造循环。它实现现有 `SessionAuthority`/`SystemAuthority`：zero Tx 为真实短读；非零 Tx 验 live 同 Store、User SH 已持及当前 Session 未撤销/未 idle/absolute 到期、user_id 匹配。它不获取锁、不开嵌套 Tx、不接受缓存 grant。System 只向当前 Human admin 授权，普通本人操作走独立 account 服务，管理员不代 Owner。

预认证事实先有明确持久 cause（auth_attempt/command/token intent），再由组合根注册 Service 创建 Actor；HTTP DTO 不能构造/传入 Actor 或 service 名。bootstrap 只接受 singleton operation、AccountAuth 只接受本次登录/请求/兑换事实、AccountMaintenance 只接受已持久到期/替换/材料 cleanup、AccountMail 只接受 exact job/attempt+fence。Service 能力不提供 Audit 浏览或通用 Secret mutation。

Audit 新 producer `account`、`account.mail`；资源 `user`、`session`、`account_attempt`、`invitation`、`password_reset`、`account_settings`、`smtp_settings`、`mail_job`（singleton 也用真实稳定 ID）。`account.`前缀动作后缀闭集为 `bootstrap,login,logout,invite.create,invite.revoke,invite.redeem,password.change,password.reset.request,password.reset.complete,profile.update,avatar.update,settings.update`；`smtp.`前缀后缀为 `settings.update,test.request,delivery`。Metadata 仅稳定 User/Invite/Reset/Job/Attempt/Object IDs、version、变更字段闭集、channel/phase/固定 reason、真实发起者 ID；没有邮箱/username/显示名/地址/IP/hash/token/正文/SMTP 响应原文。

新增 `audit.AccountAuthority`：`CheckAppendInTx(ctx,tx,Entry,AppendKey) error`、`CheckServiceLookup(ctx,Actor,AppendKey) error`，`audit.Authorizations.Accounts`显式注入。新contract提供 `AccountMetadata(action,AccountMetadataFields)` 与只读typed字段投影，不用任意map/raw JSON授权。仅分派上述动作及受限 account 服务产生的 Secret create/delete；Human 本人动作验当前 Session+资源归属，管理员动作验 System；Service 验持久 cause、动作/资源/结果及 key 一致。会撤销当前 Session 的 logout 在同 Tx 先合法 Audit 后撤销；改密可用新 Session 的同 User Actor 完成 Audit。匿名登录失败/不存在邮箱/reset 请求用实际 account_attempt 资源，不造 UserID；无条件审计成功时才提交安全事实。Audit 失败则整 Tx 回滚，公开 reset 仍按 §7 统一回执。

原 Audit System 查询、Outbox 人工重投及 outbound policy 管理仍仅 admin；AccountAuthority 不是查询豁免。内部 Lookup 只核属于该服务 exact cause 的 append receipt，不泄漏别人的事件。密钥材料/初始密码/恢复 URL 属 §9 唯一受限日志例外，不写 Audit。

Secret 保持 `PrepareWrite/ApplyPreparedWriteInTx` Human-only；新增独立 `PrepareServiceWrite(ctx,ServiceWriteRequest) (PreparedServiceWrite,error)`、`ApplyServiceWriteInTx(ctx,tx,PreparedServiceWrite) (MutationResult,error)`、`LookupServiceWrite(ctx,PreparedServiceWrite) (MutationLookup,error)`。Request 绑定注册 account Actor、System scope、Purpose=System（token/回应）或 SMTP（配置凭据）、明确 OwnerKind/OwnerID、MutationKind/CommandIdentity/expected_version、SecretMaterial；不接 caller 授权 bool。

服务写owner闭集为 invitation、password_reset、login_response、smtp_settings；IDs分别为真实Invite/Reset/LoginCommand/配置singleton ID。login_response 的稳定写入/引用owner不等于每次读取的account_response lease owner。Prepare前先短Tx建立真实planned command/request/cleanup cause及预分配身份；它不代表完成邀请/登录、不能发材料。锁后validator读取这些持久事实（同Tx新事实可由已持久planned cause证明），管理员来源仍核当前Session/role；后台reset来源核accepted reset_request，清理核exact已取消/消费cause。未完成旧planned操作可恢复核已有sealed ref或安全取消，不能猜明文重新签token/冒用失效Session继续新业务。

权限矩阵固定：AccountAuth只为前三类owner创建System材料；AccountMaintenance只删除该exact已断引用的System/SMTP材料；SMTP新建/替换继续由当前admin的原Human PrepareWrite完成，Service的smtp_settings分支只用于已持久清理cause的delete。AccountBootstrap/AccountMail没有Secret mutation权。原有有效链接的材料只通过正式lease读回，不以新create延长或更换；未知写入先Lookup，不补造另一Ref。

`PreparedWrite`/`PreparedServiceWrite` 只新增 `Ref() CredentialRef`、`RequiredLocks() []LockRequest` 安全复制投影；不暴露 envelope、明文、nonce 或修改入口。服务 Prepared 绑定私有 issuer、完整 request/current metadata plan；准备时查同 command 已有 receipt 的实际 Ref，锁后变更整体回滚，不把新随机 Ref 假称原 replay Ref。独立 prepare 提前完成 nonce 预留/加密，InTx 只验证 issuer/完整已持集合+当前 cause/epoch→原幂等→mutation；nonce 耗尽回外层准备，不嵌套 Tx。

原 Human `ExecuteWrite`/`acquireMutationLocks` 的首次集合也补User SH，不能只有外层新facade安全；原InTx重复校验已持锁不产生新锁。普通人类Metadata/Audit List的zero-Tx读继续合法，不误要求外部调用方伪造Tx。所有Secret公开安全投影不得绕过原receipt语义检查。

新增 optional `secret.AccountWriteAuthority.DiscoverServiceWrite(ctx,request)→WriteDependencies`、`ValidateServiceWriteInTx(ctx,tx,request,dependencies) error`，通过 `secret.Authorizations.AccountWrites` 显式注入，由真实 account provider 核 request/token/配置 row 或已持久创建/cleanup cause；只给明列资源的 create/update/delete，删除须 ref/lease 归零。Dependencies在secret/contract本层定义，私有issuer+完整request摘要+mapping摘要+复制locks，不借用上层Outbox/对象实现，不能caller自证授权。D04 store 增 `RequireHeldLocks` 结构要求；缺 provider 返回 DependencyUnbound，原 Human API 不接受 Service。

Account credential usage 增明确 `UsageRequest{Actor,Ref,Purpose,ReferenceOwner,LeaseOwner,LeaseID,Action,Retain}`；`UsageAction`仅 `retain_reference,release_reference,acquire_lease,release_lease,read_lease`。所有variant必填合法Actor/Ref/Purpose；reference两项只填ReferenceOwner，LeaseOwner/LeaseID必须空，Retain仅retain_reference为true；lease三项只填exact LeaseOwner/LeaseID，ReferenceOwner空且Retain=false。Acquire的LeaseID由调用方在Tx前预分配并绑定真实attempt，不由执行时另换ID；Release/Read使用实际已持久LeaseID。多余、缺失、冲突字段拒绝，不能把bool当授权。

`secret/contract/account.go` 提供正式 `UsageOperations`：`DiscoverUsage(ctx,UsageRequest) (UsageDependencies,error)`、`ApplyUsageInTx(ctx,tx,UsageRequest,UsageDependencies) (UsageResult,error)`，由Secret Service实现。`UsageDependencies.RequiredLocks() []LockRequest`返回复制集合；`UsageResult`是闭集typed结果，Action匹配原请求，只有acquire_lease携exact `CredentialLease`，其余mutation不含Lease/材料。Apply明确拒read_lease；读取材料仍只能调用原 `ReadCredentialForRequest`，不新增能在外层未提交Tx取明文的口。Apply成功仅是callback结果，沿原WithinTx提交/Unknown核实规则。

optional `UsagePlanner.DiscoverUsage(ctx,request) (UsageDependencies,error)`、`ValidateUsageInTx(ctx,tx,request,dependencies) error`仍为正式provider能力，从显式注入的Usage provider取得，不从context猜。外层只调用Service.DiscoverUsage：Service封装provider计划，最终opaque deps绑定本Service私有issuer、provider身份、全部请求字段（含Actor当前Session/cause、Action/Retain及显式空字段）、真实owner/attempt/ref/lease映射和完整锁模式；直接交provider原始结果、跨Service或自行构造deps不能Apply。Discover只读规划、不授权、不改业务事实；新ref/lease的预分配身份必须来自本章planned cause/Prepared.Ref，不能假称已存在。实际Release/Read在Tx外按LeaseID读真实记录，核预期ref/owner/consumer后收集父gate；不存在或不匹配明确拒绝，不把输入Ref当查到的事实。

外层合并所有deps、PreparedWrite及其他参与者锁并唯一AcquireAll；Service补入Project适用gate、credential锁（四项mutation为EX、read为SH）及自身记录锁，provider给足User/job/attempt等早序锁。Apply只验live同Store Tx、issuer/完整request binding，`RequireHeldLocks`核完整集合，再在该Tx重读当前映射并 `ValidateUsageInTx`授权，最后调用私有reference/lease操作核；不得再Discover、Acquire/升级、另开Tx或做Tx外读取。Validate必须承担原reference权限、purpose/owner和lease当前用途检查，不只是比摘要；私有操作核再次验证真实metadata及exact记录，不能被旧公共wrapper当绕过授权的入口。映射变化整体回滚/RESOURCE_BUSY后外层重规划，缺锁按RequireHeldLocks poison；同Tx正式新建事实只接受原计划精确身份。

Release在持锁后重新读取exact LeaseID，逐项核scope/ref/owner/consumer与request/deps；当前已released按合法幂等收敛，不因此得到读取权。Acquire只插入预分配LeaseID；若同ref+owner已有同ID，仍重验当前attempt并核原事实；已有不同ID整体回滚，不静默返回另一lease。已终局account attempt或released旧lease不得以冲突upsert清released来复活。实际Read同样锁后核exact当前lease、未released及原用途；新owner缺planner返回DependencyUnbound。

旧 `RetainReferenceInTx/ReleaseReferenceInTx/AcquireCredentialLeaseInTx/ReleaseCredentialLeaseInTx`签名保留，不能在其Tx内补Discover。新account lease owner无计划调用旧Acquire直接PreparationRequired；旧Release按exact当前lease识别真实owner后同样拒绝，不能靠传伪owner降级。ReferenceOwner没有kind：正式Account provider的旧 `CheckReferenceInTx` 对其负责的账户reference返回PreparationRequired，以注册职责及真实owner/ref/cause分派，不按ID字符串前缀猜；未知/不匹配拒绝，不fallback旧授权。planned reference由ValidateUsageInTx完成当前授权后进私有操作核，不再回调这个legacy拒绝口。原非账户provider/owner保留既有接口和授权语义；optional planner不会默认授予权限。

`ReadCredentialForRequest`保持原签名，在自己短Tx之前读exact lease映射、构造read_lease完整请求并调用Service.DiscoverUsage，唯一初始union后RequireHeld/当前Validate及原UsageAuthority/SecretResolve Audit；不能在credential后补User/job锁或把原lease读取当授权。其结果仍为每次当前材料，绝不冻结SMTP旧明文；提交未确认或Audit失败零材料，沿修订2的实际join与独立lease规则。

保留既有 `UsageAuthority` 的真实 reference/lease 检查；内部基础设施 Actor 仍为合法 `secret` 服务且 CauseRef=实际 lease owner ID，UseGrant 的 consumer=SMTP/System。Account usage闭集分为 `account_delivery_attempt`（exact mail attempt/job、process/fence、有效token/当前配置、exact ref）和 `account_response`（下段独立response read attempt）；variant不接受另一分支字段，登录回应不要求或伪造SMTP job/config。cleanup可释放已终止lease，不因此取得正文读取权。Secret resolve Audit不伪装成失效的人类Session；业务SMTP Audit由AccountMail记录真实原发起者稳定ID。

B03补齐AccountDelivery分支：UsageDependencies绑定exact Ref/scope/Purpose、LeaseID/owner AttemptID、job/current_attempt/fence/process/io_joined、intent/kind/link、channel/config_version，以及reset UserID/password_version；复制锁集合含account-directory/mail、reset User SH、job EX/attempt EX与link/config record，再与Secret自身credential/usage锁唯一union。原只按attempt表lease槽取映射不足以授权。Acquire/Read须当前attempt未终局且io_joined=false、job fence匹配、当前token未失效且SMTP config/ref同版本；锁后摘要必须包含并重读io_joined，旧Discover/prepared在acquisition-stop后不得通过或重新建lease。test无token lease，无认证无credential lease，log只允许configured=false。改变host/配置不得继续用旧密码。SMTP reference保留/释放由新helper按真实singleton及material_cleanup核Ref/Purpose，legacy口仍拒未带计划。

AccountDelivery的Release前先按§8确认io_joined=true/terminal=false的acquisition-stop checkpoint（已确认终态的幂等收敛除外）；其依据只能是§8验证的真实工作joined（含从未移交Claim的完整正向零外发证明）或ProcessGuard正向exact旧process死亡；不以缺Registry条目/单独DB返回代替。Release重读exact Secret lease并核同ref/owner/ID/consumer；不要求live token仍存在或旧SMTP配置仍current。删除/替换后的原Ref来源由仍受lease保护的material_cleanup保存；不能先删它再猜owner，也不能借cleanup读旧正文。正常/新attempt直接使用自身不可变token_ref/credential_ref，不能从当前singleton改写。仅10→11旧attempt缺Ref时，恢复adapter从该真实source owner的live/cleanup事实收集候选，经Secret.DiscoverUsage的release_lease/exact LeaseID+owner+Ref核真实lease唯一匹配；错误Ref不执行mutation，不跨域读Secret表或猜当前值。候选按已有cleanup pass分批≤100并受本轮预算限制，明确依赖错误保留pending/unknown；确认后在完整锁下以正式Validate/Apply再次核实，将仍NULL的原Ref补值与安全收敛同Tx，已有不同值拒绝。不能补值后继续旧attempt发送，不能因材料缺失伪join。删除cleanup前必须保证未join旧attempt仍有可验证的原映射，lease保护未终局不得清除保护事实。现有DiscoverUsage/ApplyUsageInTx/ReadCredentialForRequest保持签名，AccountResponse字段、权限与回应生命周期不变。

每次实际恢复Cookie创建新 `auth_attempts.kind=response_read`，以其独立ResponseReadAttemptID作为account_response owner；不共享原LoginCommand的lease。完整绑定为已验证browser ID、原LoginCommand identity+semantic_kid/完整HMAC、UserID、目标SessionID、password_version、exact response_secret_ref、System scope/Purpose/consumer、原登录提交后5m的固定expires_at、当前ProcessID/fence及该次lease ID；不存原密码/Cookie。Acquire前预收集原command、User SH、credential、attempt/lease等完整锁，锁后核原成功receipt及全部绑定、窗口、当前密码版本和目标Session仍current；browser+完整原请求HMAC证明原登录重放，不要求已持有可能丢失的Session Cookie，也不能豁免目标Session的撤销/idle/absolute检查。同Tx持久化读取attempt和它独占的lease，Dependencies覆盖完整绑定；同attempt仅可核实原Acquire，不作为另一并发请求的owner。

Acquire提交Unknown先按原writer锁核exact attempt/fence/lease，未确认Committed不开读；确认自身Unknown仍不继续。Read重新规划并在短Tx完整锁后做同样当前检查及真实SecretResolve Audit；该Tx Unknown或Audit失败不交付材料/Set-Cookie，已有私有副本销毁。原登录提交未核实也不得进入读取；Session已失效或密码已变一律拒绝，不复活Session。每个attempt在外部读/材料使用前登记到本Process的实际工作集合，Cancel/错误/到期只停止准入，待实际读取与HTTP材料使用结束、私有副本销毁后才释放本attempt的lease；一请求的Cancel/Release不触碰另一请求。Release Unknown核exact事实后收敛，过期/撤销后的cleanup只依据已终止attempt，不再次授权读。重启仅以ProcessGuard的exact死亡证据回收旧本地attempt；新的合法请求重验并新领lease，不复用旧活句柄或以5m到期猜join。稳定reference可在窗口结束断开，材料删除仍等所有真实lease终局。

装配顺序：DB/migration→纯 account Authority→Audit（Sessions/System/Account）→Secret（Sessions/System/AccountWrite/Usage）→Outbound（Sessions/System）→Object（Avatar provider+ProcessGuard）→D07 codec/Outbox producer+handler→account Service→无I/O的accountmail WorkRegistry→account.NewDeliveryPort(Service,Registry)→accountmail Worker/Runtime→bootstrap/recovery→HTTP。Registry构造只接正式ProcessAuthority，不依赖DeliveryPort；worker稍后接port，不能用mutable locator解决环。通过构造参数传依赖，不循环回调一个尚未绑定的 mutable service locator。D08 Project authority、D17 Runner、Model/MCP credential use仍 nil/unbound。

## 5. Session、登录与浏览器边界

Session cookie 为独立随机 32 bytes URL-safe token（可带安全 SessionID 定位），DB 仅 verifier；生产 `__Host-agenteam_session; HttpOnly; Secure; SameSite=Lax; Path=/`，无 Domain。local HTTP 使用 `agenteam_local_session`，不能接受本地 cookie 当生产 cookie。签发固定 absolute 上限；闲置以 DB last_activity_at+已签发 idle_seconds 判定，更新不越 absolute。

`GET /api/v1/session` 返回当前安全 User/Session 投影、CSRF token、首次改密建议，no-store；无效返回 401 并清 cookie。CSRF 为当前 raw session token+kid/SessionID 的域分离 HMAC，同步 token 只给同源响应；轮换 Session 同时更换 CSRF。GetSession、health、后台 poll、challenge、WS ping/仅连接不算 activity。成功的已声明用户业务路由才节流 touch（最多每 60s），D25 将明确命令沿此端口 touch；不信任客户端 `active=true`。

匿名 `GET /api/v1/auth/bootstrap` 仅建立 1h 技术浏览器上下文，不做业务变更；返回 anonymous CSRF、支持的 challenge mode 与全局 delivery_channel，不查询邮箱。独立 HttpOnly host-only cookie，签名载荷 `{format,kid,browser_id,nonce,issued,expires}`；写请求必须原样带 cookie+CSRF。已登录写验当前 Session+CSRF，未登录的登录/找回/邀请/重置/挑战写验 anonymous context+CSRF，所有浏览器写都精确验 canonical Origin；缺失、null、多值、不匹配拒绝。无 permissive CORS；跨站 Fetch-Metadata 拒绝，缺该头不能免 Origin。

HTTP Host 仅用于拒错误入口，不作为 public origin；同源比较沿浏览器 origin 序列化，无 suffix 匹配。安全响应 no-store、nosniff、Referrer-Policy:no-referrer。Body 日志、反代 query/body 采集必须禁用；一次性链接采用固定页面路径 `/invite#<id>.<token>`、`/reset-password#<id>.<token>`，D26 从 fragment 取出后立即移除再 POST body。D07 不生成带 token 的 API URL。

登录流程：按 browser+命令 key 及规范邮箱先读安全计数/旧 receipt→必要挑战→Tx 外 real/dummy Argon→User/目录/命令锁后重读 password_version/当前设置及挑战消费→原子 Session、成功/失败 attempt、Audit、receipt。并发改密/重置先提交则旧验证不得签发 Session，重新验证或安全拒绝。不存在邮箱与错密码都同 401、同字段、同 challenge 规则、同 hash 参数和有限排队路径；invalid PHC 是依赖故障，不当作密码错。

LoginAttempt 本身是按 browser+login key 唯一的持久尝试，带完整HMAC；同一已终局失败尝试重传返回原安全401且不再递增计数，不记录为“登录成功”。修改密码输入/新的登录尝试必须新key，异义复用409；不能靠重复同一key试新密码绕过阈值。登录失败与不存在不承诺微秒恒时，也不人为加冷却，但必须实测相同必要hash/队列/挑战路径，避免显著可重复的身份分支。

一次成功 login 的原 cookie 只在首次确认提交后 Set-Cookie；短期 5m 的同命令网络重放可从login_response Secret材料、经 §4 独立 `account_response` 读取attempt/lease恢复该cookie，先验证相同browser/完整语义、当前密码版本及目标Session仍有效。5m后、Session已撤销或密码已变不恢复旧cookie，要求新登录，不复活原Session；Acquire/Read未知或Audit失败零Set-Cookie。Secret ref随回应窗口结束断开，payload删除等待各自实际读取join/lease收敛，成功receipt不保存cookie。

普通改密先核旧密码、新密码/确认，在 Tx 外 hash，锁后核 password_version；同 Tx 写新 PHC、清 suggestion、撤销全部旧 Session、签发当前设备新 Session及 Audit/Outbox/receipt。确认后送新 cookie；若响应遗失且旧 Session 已撤销，客户端重新用新密码登录，不能为重放豁免旧 Session。reset 完成撤销全部，无自动登录。logout 仅撤销当前 Session；不取消 Agent Execution。重试已撤销 cookie 返回 401 并清 cookie，不用空成功掩盖当前身份失效。

返回导航只允许规范单 `/` 开头的本地路径，拒 `//`、反斜线、控制符、编码分隔歧义和外部 URL；当前 D07 成功 `next_path=/`。未来 D26/D08 重新验证目标资源后才启用指定目标；传入路径不成为跳过 Owner 校验的授权。

## 6. GoCaptcha 协议与反滥用

首版启用库的 rotate 模式：`rotate.NewBuilder().SetResources(rotate.WithImages(ownImages))`（按真实可编译调用分句）→`Make().Generate()`；`GetData().Angle`仅服务端保管，master220×220/thumb160×160图转DTO，`rotate.Validate(clientAngle,expectedAngle,5)`，角度只接受整数0–360。库不提供TTL/身份/一次性保障。Vue 使用官方 rotate 组件，经受控wrapper映射wire；键盘箭头/按钮调整角度、聚焦/ARIA/重试同一服务端验证，不另提供弱化免挑战分支。R01已证原组件无keyboard/slider ARIA；D07必须补键盘实测，但不声称解决依赖视觉判断的视障可达性，D26保留该明确限制及页面反馈，不能标完整无障碍通过。

连续失败以所有输入邮箱的 HMAC key 计（存在/不存在同逻辑），达到当前 N 后必须挑战；按 RemoteAddr 的 HMAC IP key 在 15m 内 10×N 次失败也触发。邮箱计数成功登录后清零，连续无失败 24h 后清；IP 窗口自然滚动，不因有一个账号成功就清除整个地址历史。不设置账号锁定、逐次延迟、登录冷却或倒计时。容量/公开找回频次限额和挑战不是登录禁用。

创建 challenge 绑定 browser ID、action=`login`、规范邮箱 HMAC、预定 login Idempotency-Key，TTL 120s；每 browser 最多 3 active，全进程最多 1024，超限 429，不能挤掉已有有效挑战。随机挑战 ID；server answer只在受限进程内存，重启后全部无效，DB只保消费/绑定及安全结果；不将旋转角度/原图/验证参数回传。无需 Redis/外部服务。

verify 单次比较，成功生成仅该 browser+email+login key 可用的随机 pass verifier（120s、DB一次消费）；失败/并发/replay都消耗本 challenge，新挑战不重置失败计数。login 将 pass 消费与 attempt/Session 同 Tx；原登录命令已成功才允许安全 receipt replay，不第二次消费 pass。challenge body/图片获取/通过与否不能检查邮箱存在；未通过不执行账号 PHC，不产生可比较“用户不存在”分支。

使用安全 DTO 显式 limits：JSON 16KiB，challenge proof 4KiB，坐标/角度有限数值；图片响应最多 256KiB，独立 cookie/token不写 image URL。验证图及 pass 永不进入普通日志、Audit 或 Outbox。harness 必须真实生成→官方组件→验证→登录消费及键盘流程，源码编译或截图不等于兼容通过。

## 7. 初始化、邀请和恢复

bootstrap 先于对外账户入口，但在必需 DB/Secret/MinIO/Outbox 初始化之后。固定 bootstrap command+目录 EX，原子创建 `admin@mail.com/admin/admin role`、强随机 24-character 密码的 PHC、suggestion=true、singleton marker、Audit。先 Tx 外随机/hash，锁内读 marker；已有 marker只检查对应 User事实，不覆盖、不再输出初始密码。无 marker但已有冲突账号/半损坏结构则失败，不自动认领既有 User。

首次确认提交后尝试一次专用恢复日志输出；Unknown先同锁核User+marker的exact candidate User/operation/process，只有该首次创建进程仍持原明文才有资格。输出前独立短Tx把该marker的eligible原子改attempted并保存唯一log_attempt_id，确认提交或核实同attempt已提交后才写文件；claim Unknown未核实绝不写。普通重启即使看到eligible也不得重新取得输出权，安全置abandoned；不保存可重印明文或Secret。

进程在创建commit后/claim前、claim后/写前、写后/完成checkpoint前死亡分别留下abandoned、attempted/unknown或unknown，均不重印、不重置账号；只有本次真实写+Sync确认才记written。错误记failed/unknown，不自动再尝试初始密码输出。日志与DB不能原子exactly-once：可能已创建账号却没有完整密码行，或完整行存在而checkpoint未知；统一forget-password恢复。此规则与可按原有效链接重发的邀请/reset明确分离。

管理员创建邀请：当前 admin→幂等→已注册 email拒绝→删除已过期 live invite并建立材料 cleanup→若已有未到期邀请则复用原 ID/token/expires，产生新的 delivery intent；否则随机 token32B，PrepareServiceWrite(System)、invite+reference+intent+Outbox+Audit+receipt 同 Tx。有效期严格 created+24h；重发、SMTP故障、重启都不延长。创建/重发每 invite 60s 最多一次实际意图，原同命令重放不再占次数。

检查邀请只接 body token，当前有效才返回只读 email/expires；已登录其他身份返回身份不匹配，绝不自动关联。兑换 Tx 外验证密码/hash，锁后验 token verifier、期限、用户名唯一、email未注册、角色固定 user；同 Tx 新 User、消费删除邀请、撤销待发 jobs/intents、断材料引用并建立 cleanup、Audit/receipt。两个不同 key 只有一个成功；同原 browser+token及完整命令语义可读原无敏感成功 receipt，不再使用 token 创建第二次账号。新 key/其他 browser/inspect 对已消费 token 均统一无效。

撤销/到期在相同目录/链接与发出门禁下删 live row、关闭新发出准入、撤销未开始 jobs、断引用+清理材料；不能因 SMTP暂时不可用保留已到期邀请。后台到期批次每批100，pass+ID公平轮转；活跃 lease 暂时保护 Secret bytes而不使 token仍有效，join后清理继续。

找回请求的公开返回始终 `202 {accepted:true,delivery_channel:smtp|backend_log}`，channel只由全局当前配置决定，与邮箱存在/投递结果无关。对任意合法邮箱都创建相同形状的受限请求 attempt/receipt；registered时连接到有效 reset 或新建随机 token+Secret+intent，unknown时创建无目标的请求事实，均由异步处理结果隐藏其差异。不得同步 SMTP/Secret结果或 token ID回显；日志/邮件状态只有管理员合法查询。公开 invalid邮箱格式可400，但“未注册”不能成为字段错误。

存在/不存在走相同队列及请求事务轮廓，公开 202 不等待加密/SMTP；若内部材料准备尚未完成，以已提交 request intent 由受跟踪 worker生成，worker重新查当前账号和有效 reset，再同 Tx绑定唯一 token。注册邮箱工作量不同不由同步响应暴露。DB不可用可统一503，账户级节流仍202；每邮箱60s只安排一次实际投递，每IP每小时30次请求超额统一429（存在/不存在均计）。这不是登录限速。

reset 有效期取首次签发设置；已有未用有效 token重发同链接、同expires，不反复 hash/换token。提交重置验证 token/新密码+确认，hash在Tx外；锁后 token/verifier/password_version重验，同 Tx改密/清suggestion/全部Session撤销/消费删除reset/取消后续投递/清材料ref/Audit+事件+receipt。失效、撤销、消费 token统一410；原命令安全成功 receipt 仅以原browser+token证明+相同语义核实，不泄漏User资料。

## 8. SMTP 配置、Outbox 和真实投递状态机

SMTP singleton DTO：`configured,host,port,encryption:none|starttls|tls,username,sender_email,sender_name,credential_present,auto_retry_count,retry_interval_seconds,version`。host是规范DNS/IP，不含scheme/path/userinfo；port1–65535；username≤256bytes、sender_name≤80codepoints，全部头字段拒 CR/LF/control；邮箱沿 §2。认证 none（username空且无credential）或明确 PLAIN；空 password替换表示保持，删除凭据用显式 `credential_action:remove`；非空新值只进入 Secret。

`auto_retry_count` 默认3、范围0–5，表示首次之外的最大自动次数；`retry_interval_seconds` 默认60、范围10–3600，指数 `base*2^(n-1)` cap1h，加确定性≤10% jitter并不越 token expiry。设置更改影响下次 attempt，已有job保留已用次数，不重置预算；降低上限立即禁止多余重试。授权手工 retry产生新 intent/周期，仍原token/expires，不与active attempt并行。

save 只保存配置/Secret ref/Audit，不连接或发信；unconfigure显式操作断旧ref并安全清理。test命令必须admin及明确recipient，创建无链接的测试job，配置必须已configured；失败不回滚配置、不创建邀请/reset。公开reset无论该配置是否可达仍只显示smtp渠道。配置后任何网络/认证/证书/材料失败均不降级日志。

Typed event `account.delivery-requested` v1：System scope，producer=`account`，aggregate=`account-delivery`，payload `{intent_id,kind}`，无email/token/正文；首次 intent Tx带 SH注册屏障同业务提交。正式 handler `account.mail-enqueue` 按已提交 intent/current合法性，同 Tx写唯一 mail_jobs+processed marker。它不访问 SMTP/文件；canonical bootstrap在注册事务之外扫描未形成job的合法intent，按同计划/版本校验补齐，不依赖“收到过事件”猜状态。

`account.sessions-revoked` v1：System scope，aggregate=`account-user-auth`/稳定UserID，header version与sequence取同一持久单调auth_sequence；payload为UserID、作用域=`one|all`、one时SessionID、固定reason，无凭据。与真正撤销同Tx；D25未来注册consumer并从当前Session canonical bootstrap，不补造当前已支持的WS行为。ProducerAuthority核持久auth/intent事实、当前Actor/cause、精确event摘要；即使Actor是Service也不能发任意系统event。

job phases=`pending|claimed|sending|retry_wait|sent|failed|unknown|cancelled`；sent仅完整SMTP最终250或恢复日志完整写完且Sync成功。claim在短Tx持job EX，分配实际AttemptID、递增fence/attempts、绑定当前ProcessID、记录配置version、exact token/credential Ref及对应预分配leaseIDs；此时没有声称lease已Acquire。Claim提交确认后按下文正式discover/acquire，两次worker/手工retry不并发。worker并发2，全局pending上限10000（新增意图准入429；公开reset仍用泛化accepted并受其独立限额），批次100/pass+ID，失败/未知/foreign live不能阻断其他job进展。

Outbox尚未形成job时，管理员按已接受intent的预分配job_id查询得到 `enqueue_pending` 安全投影，不将202的status目标暂时404；这个派生phase不伪造已发送attempt。sent只表示SMTP服务器接受，不保证到达收件箱。公开reset受理队列也有全局10000个未处理请求上限，满额对所有邮箱统一429；已提交accepted请求的后续投递失败不改变其公开receipt。

恢复以持久 exact attempt/fence 和同 D05 ProcessGuard 正向 ConfirmStopped判本地旧进程死亡；TTL/lease age/重启时间不证明死亡，更不证明邮件没发出。attempt记录 `negotiation|auth|envelope|data|awaiting_acceptance|closed` 阶段；明确未进入MAIL/DATA且旧本地工作已终止可重新准入，AUTH发送不能冒充邮件已发送；业务发送已开始而无法确证完整结果时置unknown。unknown可按有限预算再试但UI/Audit明确可能重复，使用稳定 `Message-ID: <jobID@public-origin-host>` 仅辅助去重，不能宣称SMTP exactly-once。任何checkpoint Unknown先查原fence，不偷偷递增并重发。

每attempt分两个短Tx：第一段Claim持久真实mail_attempts/job/current_attempt、phase=claimed/negotiation、fence/process/config及需要的exactRef/预分配leaseID对；test的token对、无认证或log的credential对均NULL，新非空材料槽缺Ref或缺leaseID拒绝。确认提交后，Tx外Secret.DiscoverUsage从已存在attempt取得完整计划；第二段唯一AcquireAll后重验claim、source/ref/配置及mapping，再ApplyUsageInTx acquire两正式lease。第二段Unknown未确认不得读材料；第一段crash无外部I/O，按exact process/fence回收，不在首次INSERT尚未提交时外部Discover。Claim成功后未进入Prepare/从未Acquire，或第二段明确回滚，均可通过下文acquisition-stop与可信零lease分支合法终局；不得为了Release先创建lease。Acquire确认后才ReadCredentialForRequest；下一AUTH/MAIL仍当前重验，配置改变关闭原attempt并走有界新尝试。

`account/contract/delivery.go` 固定以下内部正式面；业务表只由account实现读写，accountmail不直接SQL读写账户/Secret域：

```go
type DeliveryPort interface {
    NextDeliveries(context.Context) ([]foundation.ID[MailJob], error)
    ClaimDelivery(context.Context, foundation.ID[MailJob]) (DeliveryAttempt, error)
    PrepareDelivery(context.Context, DeliveryAttempt) (DeliveryMaterials, error)
    BeginDelivery(context.Context, DeliveryAttempt, DeliveryPhase) (SendPermit, error)
    CheckpointDelivery(context.Context, DeliveryAttempt, DeliveryProtocolPhase) error
    FinishDelivery(context.Context, DeliveryAttempt, DeliveryCompletion) error
    RecoverDeliveries(context.Context) (DeliveryRecoveryStatus, error)
}
type DeliveryRuntime interface {
    CurrentProcess() ProcessID
    RequireActive(DeliveryAttempt) error
    RequireJoined(DeliveryAttempt, DeliveryCompletion) error
}
```

构造函数固定 `account.NewDeliveryPort(service *Service, runtime contract.DeliveryRuntime) (contract.DeliveryPort,error)`；参数类型不依赖accountmail具体实现，接收已构造Registry，CurrentProcess须等于Service正式ProcessAuthority。它返回上述DeliveryPort并只使用Service已经持有的Authority/Store/Secret/Audit；缺任一真实绑定明确拒绝。NextDeliveries一批至多100个due JobID，仅作技术调度/推进持久pass，不授权或claim；空slice为本批无候选。worker逐项尝试整批，不能首项busy即中断；不因只消费首项反复挤掉尾部。RecoverDeliveries亦单批≤100、逐项有界，返回Examined/Advanced/Pending安全计数，不以foreign live/首项错误阻断其余；旧processing/claimed/sending/unknown及io_joined=true/terminal=false的剩余清理按原证据恢复；后者不是已投递/已全清，也不得再次发送。

`DeliveryAttempt` 为port私有issuer登记的opaque句柄；安全Details仅JobID/IntentID/AttemptID/DeliveryKind/ProcessID/fence/config_version/channel，私有binding还含exact link/User/password_version、材料Refs/leaseIDs。ID沿现有MailJob/Attempt/DeliveryIntent标签，fence/version正int64沿foundation编码。公开构造/反序列化/Format/JSON都不能重建权限，跨port/process/错绑定拒绝。Claim Unknown不交可用句柄；该次调用只能在移交仍开放、同原writer锁确认canonical成功后选择成功移交；一旦关闭移交，随后核实只供私有收敛，不能再恢复可发送handle。

Claim Tx前，port在新account私有delivery文件中登记exact ProcessID/JobID/预分配AttemptID的claim-operation，保原事务cause、完整writer锁计划及结果provenance；实际DB调用纳入已有Service.begin/finish的operation、Stop/Force/Joined跟踪，不改service.go。caller's ctx返回、取消通知或外层select退出不等于原DB调用join；必须由该实际调用真正结束的一侧记join，迟到结果仍归此记录处理。已跟踪的accountmail worker在调用Claim前也已纳入本Process实际工作集合。

claim-operation在私有互斥状态下一次选择“成功移交worker”或“关闭移交、port收敛”，不可逆且互斥；关闭位本身还不是join证明。返回取消/Unknown/失败前须关闭移交，后续Recover/迟到DB返回不得重开；若成功移交先胜出，Claim必须交回原handle，不能因随后ctx取消改成丢弃handle的错误分支。port与worker不得各为同一Claim签发不同终局依据。

成功移交的worker不在Claim返回与接收登记之间插入ctx取消early-return：成功handle先受其已安装finally保护，并同步在同Registry登记，再决定是否Prepare。StopAdmission不能拒绝这类已移交handle的终局跟踪；取消/Force或不再允许继续者仅作清理登记，登记本身不授予Prepare/外发，是否继续仍按既有准入规则。finally承担零外发工作的实际结束或全部真实I/O/material join，之后才取得Registry completion。此路径的port不能凭“暂时没有Registry条目”代签join，原DB局部调用join也不解除worker责任。

从未移交路径只有“原DB调用实际join + 私有移交永久关闭且从未成功移交 + 同原writer锁下exact canonical确认”合并才是正向零外发依据；canonical存在本次记录时须匹配原job/attempt/process/fence/Ref/lease事实，不能采用另一worker的Claim。port仅以此走私有Recover/Finish收敛路径，可与既定acquisition-stop在同一短Tx内确认；不伪造DeliveryRuntime completion或发出可用handle。若canonical确证本次Claim没有产生事实，只结束私有账目，不造attempt/lease/业务Audit。原writer或确认Tx仍Unknown时保原Unknown/cause；预算耗尽、绑定不符或其他核实失败保留待确认，不折成NotCommitted/absence，不进入Release Discover。

claim-operation的实际join状态与待canonical/checkpoint确认的durable进度分别保留；已join不代表server已停或持久收敛完成，确认/最终收敛前不丢原provenance。原DB调用join后其实际operation可结束，私有待确认记录不冒充仍在I/O；后续确认每次复用正式cleanup operation及已有请求/恢复/共享Force预算，不另起秒数或无界任务。进程重启失去私有记录后，只按exact Process死亡及持久writer事实恢复，不能从内存缺记录猜零外发。

真实worker按上述成功移交规则同步登记handle，任何Prepare/Secret读取/网络/文件之前都已有该exact工作的跟踪。Prepare/Begin/Checkpoint要求RequireActive及DB当前绑定，并在与Acquire共有的job EX/attempt EX及完整union锁后拒io_joined=true；调度器不得为terminal=false的当前attempt开启新fence。Registry只登记实际工作，不授予业务读写。DeliveryMaterials是仅受信worker消费的敏感载体，绑定同attempt/config/channel、recipient/host/TLS/发送人及正式lease/material，不向HTTP/Audit/log通用投影；SMTP设置及job admin DTO仍由account当前System授权facade提供。test使用真实recipient且无link/token；其producer/handler不能借邀请分支或默认unbound成功。

DeliveryPhase闭集SMTPAuth/SMTPMail/RecoveryLog。BeginDelivery取得mailAdmission SH，短Tx当前核验并提交相应检查点：AUTH记auth但job仍claimed；MAIL或日志资格准备记job=sending、protocol=envelope。checkpoint先于可能发送，crash可能保守unknown，不能把已提交sending当真实首写/资格。校验自身Unknown不交SendPermit。opaque permit绑定issuer/exact attempt/fence/phase/config、单次及绝对deadline=min(取得SH时刻+1s,parent,token expiry)，对外不暴露原锁。

SendPermit提供受信adapter使用的 `RunSMTPFirstWrite(func(context.Context)(int,error)) (int,error)`、`GrantRecoveryLog(func()error) error`、`Close()`；phase不匹配、重复/过期、wrong-work拒绝。SMTP方法只同步执行该attempt真实已准备socket的首写，使用permit给定deadline/实际Close保障有界；记录n>0或真实失败后释放SH。日志方法只在Sink同步队首Authorize中调用并同步消费该work GrantOnce，成功或失败随即释放SH；不调用文件Write。Close只放弃尚未执行的准入，不能把进行中的首写当join；超时不允许以后补调用，执行中的实际I/O仍归Registry跟踪。不把任意goroutine/返回nil回调视为真实首写或Grant。

CheckpointDelivery仅接受协议阶段data/awaiting_acceptance，分别在可能开始DATA内容/等待最终接受前持久记录；只能同fence合法向前，不重开终态/改变材料。其Unknown中止新协议动作并按原attempt核事实，不能换fence补发。所有port InTx工作均在account内部完整计划/当前验证下，无外部I/O；发现映射变化整体回滚后外层准备。

DeliveryCompletion是同Registry私有登记的不可变终局，闭集sent/failed/unknown/cancelled及既有safe reason、实际channel，绑定exact attempt/fence/process与完整socket读写/材料销毁或Sink ticket.Done终局。RequireJoined只检查自身登记、不等待I/O、不接受caller bool/context返回；进程内取消先请求停止，真正返回/Close/全部goroutine或ticket join才签发completion。只登记/准备、未开始外部I/O的工作也须真实结束才能得到该证据；本进程仍活且未找到登记不证明join；从未移交Claim仅使用前述私有正向零外发证明进入收敛，不把它伪装成Registry completion。恢复旧process另由正式ProcessAuthority.ConfirmStopped核exact死亡，不能用新Registry伪造旧completion。

Finish/Recover统一先关闭acquisition再规划Release，复用已有io_joined/terminal，无新列/Secret口：只有上述实际join（含已完整验证的未移交Claim零外发依据）/exactdeath证明后，完整预收集依赖并一次AcquireAll，必须包含所有Acquire也持有的job EX+attempt EX；锁后重验原job/current_attempt/fence/process/Refs/leaseIDs，将io_joined置true而保持terminal=false、原protocol/result/completed_at和原job结果不变。这是技术acquisition-stop checkpoint，不提前写closed、成功、失败或投递终态。Acquire/Read/Begin/Checkpoint同锁拒该位，usage摘要含该位，故先前已Discover的迟到prepared也必须失败；最终收敛前不能增新fence。

该checkpoint Tx与原Acquire writer串行。只有提交确认，或Unknown后在相同writer锁下核实exact checkpoint确已持久，才离开Tx作Release Discover；原Acquire或checkpoint Unknown未核实时保原Unknown/cause/pending，不改称NotCommitted、不推断零lease。checkpoint确认后，所有早先Acquire已终局，未来Acquire已被关闭；无外部I/O跨该Tx。

对每个原不可变Ref/Purpose/owner/LeaseID槽，正式调用Secret.DiscoverUsage(release_lease)：存在则保留其完整release计划；**只有该调用直接返回、尚未包装的顶层 `*secret.Error` 且 `Code()==SECRET_NOT_FOUND`** 才可记录该槽可信零lease。不得用errors.As匹配嵌套NotFound、provider错误、依赖不可用或错映射；这些都保留待收敛，不吞错。原本不需要材料的NULL槽不调用Release。legacy Ref=NULL仍按§4核原映射，构不出可信Ref时保pending，不能用任意候选Ref/当前config加NotFound假造零lease。

最终Tx将存在lease的release计划与完整本域锁唯一union，再核exact binding、io_joined=true/terminal=false checkpoint；释放真实存在lease，原子写closed/result/terminal、job状态/next_at及真实SMTPDelivery Audit，零lease槽不Release。存在计划的Apply若失败（含其内NotFound）整Tx回滚并重新正式规划，不能在Tx内吞错继续终态。最终Unknown同writer锁核原事实，不重发送；已终局同义只核原收敛事实，不覆盖后来job fence，旧completion不能更新新attempt。

重启公平扫描io_joined=true/terminal=false，保原Ref/leaseID/fence继续Release Discover与最终Tx；已released走正式幂等，可信不存在走零Release，未知/拒绝不假成功。中途crash不重开acquisition、不因io_joined就跳过终态Audit。lease释放后仍等所有实际工作与Sink.Close join才退休全局guard。

网络链：`Outbound.Client.DialTarget`→TLS/STARTTLS协商→`BeginSend`后AUTH→`EndSend`→再次`BeginSend`后MAIL/RCPT/DATA/最终响应→`EndSend`→Close。TLS配置来自固定trust，hostname验证原目标，STARTTLS必须扩展存在且升级成功后重发EHLO；none仅在明确该配置时允许明文，不加未确认的“仅内网”产品限制。stdlib PlainAuth拒非TLS远程时使用严格绑定该显式none profile/精确host的PLAIN Auth适配器，不伪造ServerInfo.TLS；未advertise支持机制则失败。不在EndSend后写QUIT，不复用连接跨job。

SMTP overall30s、dial5s、TLS10s、每读/写idle5s，响应行≤4KiB、单reply≤32KiB/100行、message≤64KiB、仅一个收件人；自有有界协议包装阻止 `net/textproto` 无限缓冲。标准库并非自动提供这些限制，必须真实大响应/slowpeer测试。所有 DNS/pinning/current policy/固定禁止地址沿D04；none不复用 allow_http。

单Central mailAdmission由纯account.Authority构造并独占，同Service的mutation和DeliveryPort共享一把可取消、公平SH/EX门禁；等候者按到达序排队，连续队首SH可合并，已排队EX之后的新SH不得插队。等待ctx取消移除本等候者，不能取消别人或泄漏锁；不能用无ctx的裸RWMutex等候冒充有界。门禁不依赖业务Service locator、不对未绑定worker默认放行。

固定次序为Tx外昂贵准备/计划→mailAdmission→DB Tx/完整AcquireAll；provider Validate只检查已持锁，不内部取得门禁或倒序加锁。SMTP worker完成DNS/协商/Outbound.BeginSend后才BeginDelivery；SH从当前短Tx校验起到真实首写n>0/失败为止≤1s，更短parent/token expiry优先，不等待整个响应/body。DB权威，当前校验提交Unknown不发；不持DB Tx跨socket或用cache放行。

EX只覆盖以下最终失效事务及其确认：expireLink/RevokeInvitation删除live token；RedeemInvitation最终消费；CompletePasswordReset最终password_version/消费/撤销Session；ChangePassword最终password_version/Session/Audit/事件，即使reset row尚在；新SMTP save/replace/unconfigure。removeLinkInTx消费同Authority私有EX guard，不能从Tx内另取。Argon/nonce准备、初步planned Tx、创建/有效重发、enqueue和最终物理清理不持EX；共同expire路径覆盖重建过期链接和后台Recover。Unknown不返回成功、不永久占gate；后续sender必须等冲突DB锁并读当前事实，不能凭旧镜像发。自然到期由每次DB时间校验和permit截止保障，不依赖清理扫描先发生。

上述实际首写n>0/失败规则仅用于SMTP，保持不变。普通文件日志使用同一mailAdmission协调，但线性化点为 §9 的队首单次写入资格授予；不能把文件Write/Sync放进SMTP首写期限，或将入队当作获得资格。

mutation先关闭准入则旧attempt不得新AUTH/MAIL；业务首写先发生则在途邮件可继续，撤销/消费返回后链接已立即无效，即使邮件随后到达也不能兑换。同一连接AUTH和MAIL分别准入，AUTH先发不代表MAIL永远获准。取消信号/断socket只代表发出了停止请求；worker实际返回、socket.Close/读写goroutine join后才释放credential/token lease、登记io_joined。Secret引用断开后已有lease保护只为收敛，不让旧token恢复授权。

临时4xx/连接暂不可用按预算retry；5xx/auth/cert/配置/材料错误终止自动重试并给safe reason，管理员改配置后显式retry；DATA终止或最终reply丢失为unknown。原始server回复/recipient/host/password/MIME正文不能进入error/日志/Audit。可信失败不会变成sent，sent checkpoint unknown核实后才能展示；邮件有可能真实已发但DB仍unknown。

## 9. 受限恢复日志与材料清理

专用 `recoverylog.Sink` 只接受typed bootstrap/invitation/reset记录，参数是私有敏感载体/SecretMaterial及安全cause，不提供普通字符串通用logger接口。B01 `WriteBootstrap`、`WriteInvitation`、`WriteReset`保留兼容；B03邀请/reset必须经下述带admission的Submit口，不绕过资格走旧Write口。固定一行版本化JSON（正常logger永远不调用），只含用途、时间、相关ID、必要邮箱及password或public-origin URL；严格JSON编码防注入，最大4KiB。文件Write/Sync属于受跟踪外部I/O；失败只记录固定安全code，不能把敏感行再次写stderr。

Sink仅一个串行writer、最多32项等待（另1项active），每项接收前登记安全attempt及本Process实际工作；队列满返回可重试容量错误，不以无界goroutine等待。仅Write全部完成且Sync成功可标written；StopAdmission拒新项并取消尚未授予资格的日志工作，已授予项作为在途工作真实收敛，Drain等待实际active/清理join；B01 bootstrap仍沿原一次输出及真实join规则。普通文件Write/Sync/Close不保证响应context；Force不得同步无界等这些syscall，至多发起一个预登记的Close清理任务，不能每次Force再生后台任务或把Close请求当完成。共享额外1s到期即返回安全失败，未返回的Write/Sync/Close仍登记为未join并保留ProcessGuard，仍按 §12 实际发起DB ForceClose；不得另开预算。部分写/未知结果不触发bootstrap密码自动重印，排队/active材料仅在所属实际任务结束后销毁。

最小正式口为 `SubmitInvitation(ctx,InvitationRecord,FirstWriteAdmission) (WriteTicket,error)`、对应 `SubmitReset`；成功只说明bounded work已登记，不说明准入或写入。Sink签发opaque ticket绑定本实例/exact purpose/resource/AttemptID/唯一work，提供 `Wait(waitCtx) (Result,error)`、`Done() <-chan struct{}`。work ctx控制未授予工作的取消，waitCtx只控制等待；Wait超时可返回Unknown但不关闭Done。Done只在该work不能再发起写、实际Write/Sync已结束、临时材料已销毁、不可变最终结果已发布后关闭；多Wait不能竞争消费一次性结果。全Sink Joined仍另外要求worker及Close终局，不能与单ticket Done互代。

队列等待、Secret材料读取及≤4KiB记录校验/编码均不持mailAdmission。真实单writer已消费队首work并完成这些准备后，才同步调用受信B03 `FirstWriteAdmission.Authorize(ctx,RecordIdentity,GrantOnce) error`；RecordIdentity仅安全purpose/resource/AttemptID，adapter绑定真实job/attempt/process/fence及token/lease/配置。GrantOnce由Sink私有状态生成，只有本次同步回调期间可用，绑定该work且单次；不能由caller bool构造、保存后迟到使用、跨ticket使用或另起goroutine代授予。缺adapter拒绝，不把普通回调存在当已授权。

adapter在真实队首调用 `DeliveryPort.BeginDelivery(attempt,RecoveryLog)`，共享mailAdmission SH下按完整初始锁短Tx重验exact claim/fence、当前token/lease与SMTP configured=false，确认同attempt持久sending/fence检查点提交；再通过该permit的GrantRecoveryLog于SH仍持有时同步消费GrantOnce。该操作在work互斥状态下与取消/停止竞争：取消先则不授予且零Write，授予先则该work取得不可撤回的写入资格并计为在途。**这次资格授予是普通文件日志的线性化点**；1s仅限制持SH开始当前校验至授予/失败的准入段，并不越更短parent或已验token有效期。校验/检查点提交Unknown、未Grant或Grant失败均不写；Grant成功后的错误只按该在途work的真实终局处理，不能把它重新称作未准入。

授予后立即释放SH并回到同一writer，随后至多一次执行该已准备记录的文件Write及Sync，不再次排队、不换材料/目标/attempt；整个Write/Sync不持SH。授予与os.File.Write之间仍可被调度暂停，**不声称syscall已经进入或首字节已经可见**。EX先提交则旧work不能取得新资格；资格先授予则允许EX提交后才开始实际文件调用/出现字节，此时仍是已合法准入的在途工作。配置从无到有先提交则未获资格的日志attempt关闭并重规划到SMTP；已获资格者可完成且如实channel=backend_log。配置已存在但坏配置/SMTP失败绝不进日志；撤销/消费返回后链接立即无效，不因在途资格恢复合法性。每次明确重发沿原有效链接可产生新的日志投递，但公开API/Admin DTO均不返回该链接。

Wait返回、取消请求、资格授予或Close请求都不代表io_joined；业务worker继续持有ticket与该attempt的Secret lease，Done后同Registry才可形成该attempt的DeliveryCompletion，经FinishDelivery按exactfence登记实际终局/释放lease，最终checkpoint Unknown先核原事实。重启不复用ticket，也不能因内存资格记录消失推断未写；已持久sending但缺可信完成结果按原unknown/ProcessGuard规则收敛。资格授予不是written，仍仅完整Write+Sync成功才written；不得在文件阻塞时为释放lease/guard或重试同job伪造完成。

管理员恢复渠道须真实可读、权限受限、包含操作交接和轮转/留存说明。首次初始化明文只活在首次尝试内存，普通日志只说是否发生受限输出；链接重发Secret删除后不可再恢复。文件日志不能承诺DB事务exactly-once；部分写/Sync未知标unknown，重试可重复，写路径不截断既有文件。

token消费/撤销/到期在原Tx删除live row并释放reference/建立material_cleanup；删除Secret payload须等全部真实lease释放，无读权限的清理provider只可释放已join attempt并删除该exactref。清理批次公平、幂等、Unknown核receipt；永不因缺Session假装admin、把外部I/O取消当join或仅到期强行删在用材料。完成清理只保最小IDs/安全结果，不保存Actor凭据、正文或token。

## 10. 个人资料、头像与对象协议

本人读/改 `username,display_name,theme`，email只读；每mutation带expected_version，User EX下当前Session→命令重放→version→唯一性/Audit/receipt。username提交后按当前DB映射解析，旧路径立即NotFound，无alias/redirect；D08从稳定UserID解析当前username，不以URL名字授权。

头像接单一原始image body（不允许multipart任意文件字段），Content-Type限定JPEG/PNG/WebP但不信header。压缩输入≤5MiB、宽/高各≤4096、总像素≤16,777,216；DecodeConfig先界限再Decode，最多2并发/16排队/2s准入；实际decoder消费bytes及容器检查拒尾随第二图片/拼接伪装、APNG `acTL/fcTL/fdAT`、WebP ANIM/ANMF/VP8X animation、SVG/GIF、坏CRC/截断。不能只读第一帧说静态。

保持比例fit512×512、不放大，透明合成明确白底，统一JPEG quality85重新编码，丢EXIF/ICC/XMP及原文件名；只把新编码bytes以image/jpeg交D05。纯Go解码不可强杀但须实际join并受像素/并发上限；不以ctx取消假称内存任务已结束。响应声明服务端限制，D26不是上传验证主体。

流程：先当前Session/容量预检→有界原bytes摘要/解码重编码→持久avatar_changes→真实PreparePayload→完整access plan/ReserveUploadInTx→Tx外UploadPrepared→原对象发布→User+新旧对象完整计划下，锁后重验当前User/version、Attach新/Release旧canonical、切User.avatar/Audit+receipt+avatar_cleanup同Tx。已有owner的D05发布会建立canonical，不等于头像已切换；其exactupload/command登记在avatar_changes，取消/恢复必须有明确“尚未被User选为当前”的持久事实。

成功只保当前头像；旧对象在切换同Tx已Release引用，后台以 `ObjectCleanupCause{ReplacedObject,UserOwner,operation}` 调 `DeleteUnreferenced`。失败/冲突的新对象也由真实cleanup协议回收；未attached走既有ObjectMaintenance CancelUpload专门分支。已有canonical尚未切换的窗口由新增 `ReferenceCleanup.ReleaseForCleanupInTx(ctx,tx,cause,ObjectID,plan,locked) error` 闭合：首版只Avatar且cause为ReplacedObject/CancelledUpload，不向Service发普通Avatar grant，不由account直接SQL改object表。

`avatar_changes` 必须在Publish前持久化 original command/User/Object/Upload/Attempt 映射（来自真实 Reserve结果），phase=`preparing|published|applied|cancelled|cleanup_pending|completed`。实际切换原User版本冲突、明确取消或exact本地死亡恢复后，在User EX下把尚未applied操作持久置cancelled+cleanup cause；不能仅凭avatar!=object授权，也不能据年龄判原活跃上传死。对象旧reference已成功被另一个头像替换则用持久replacement operation，不改变已applied事实。

新增 `CleanupReleaseAccess` 闭集request绑定完整cause/owner/object/originalupload；D05 Discover查其真实upload/command/object并经Avatar planner收集User EX，和原command/对象EX/引用record一次union。ReleaseForCleanupInTx验live token/当前mapping、对象确为已发布或原cleanup幂等状态，账户provider核exact cancelled/replaced cause与current avatar!=object、原User切换gate已关闭；同Tx删除该exactcanonical/reserved ref，把原upload.disposition置revoked并对象置不可逆cleaning，沿原cleanup_operations建立或复用checkpoint。原PublishVerified/Attach/Consume/Lookup及账户迟到切换都检查同一durablegate：旧成功receipt只能返回revoked安全结果，不能重新引用/发布/改User。其他owner/ref和current头像一律拒绝；若仍有reader lease仅保留payload待join，引用释放不等于物理删除。Unknown先同锁核原gate/receipt，之后仍由原DeleteUnreferenced完成I/O；不扩大普通对象删除权限。

Avatar AccessPlanner 对全部Owner/Read/Release/Cleanup/Maintenance request真实映射UserID，计划含User SH或EX及actor Session的User gate；非Avatar/未知未来Project scope明确unbound。ObjectReadAuthority只允许本人当前头像exactObjectID；被替换对象不可按猜ID读取。`GET /api/v1/me/avatar`通过ReadObject实际reader lease流式发送，非匿名URL，无generic ArtifactDownload Audit；头像读本版不额外制造Artifact事件。D05缺payload/完整性错误、关闭join、range与lease语义保持。

## 11. HTTP wire 与正式服务表面

所有资源JSON有显式字段/OpenAPI；ID、version、seconds、计数沿D01，未知字段/重复key/尾随JSON拒绝；普通JSON16KiB，SMTP配置32KiB。GET no-store；敏感body和Cookie仅安全载体，泛化fmt/JSON不能泄密。origin/CSRF先验证，再解析业务身份/当前权限→幂等→version。表内“匿名”仍指已验证anonymous context；Inspect也是POST安全body避免URL泄漏。

| Method/path（均 `/api/v1`） | 输入/权限 | 成功及关键安全结果 |
| --- | --- | --- |
| GET `/auth/bootstrap` | 技术匿名入口 | 200 anonymous CSRF/mode/channel，不查邮箱 |
| GET `/session` | 当前Session | 200 user/session/CSRF/suggestion；无效401 |
| POST `/sessions/login`、`/sessions/logout` | email/password，或当前Session | 200当前安全身份+Set-Cookie；logout204/清Cookie；错密码/不存在同401 |
| POST `/auth/challenges`、`/auth/challenges/verify` | mode/email/login_key；challenge_id/proof | 201 challenge图；200 pass；proof无效400同义，无邮箱存在分支 |
| POST `/password-resets/request` | email | 始终泛化202；可重放原request接受结果 |
| POST `/password-resets/inspect`、`/password-resets/complete` | token；token/new_password/confirmation | inspect200仅有效/expiry；complete204；失效统一410，无自动登录 |
| POST `/invitations/inspect`、`/invitations/redeem` | token；token/username/display_name/password/confirmation | inspect200绑定邮箱；redeem201注册完成并要求登录，无Session |
| GET/PATCH `/me` | 当前本人；version+username/display_name | 200安全User；邮箱/role/password hash不可写 |
| GET/PUT `/me/preferences` | 当前本人；version+theme | 200持久主题 |
| POST `/me/change-password` | version/current_password/new_password/confirmation | 200安全完成+当前新Session Cookie |
| GET/PUT/DELETE `/me/avatar` | 当前本人；PUT原bytes与`If-Match: "<decimal-version>"`（拒weak/星号/多值），DELETE JSON version | GET实际stream；PUT200新头像metadata+User version；DELETE204 |
| GET `/system/users`、`/system/invitations` | 当前admin，cursor/limit≤100 | 200安全列表；不含token/material/ref/邮件正文 |
| POST `/system/invitations`、`/system/invitations/{id}/resend`、`/system/invitations/{id}/revoke` | admin；email或version | 201/202安全invite+job/204；有效同链接不续期 |
| GET/PUT `/system/account-settings` | admin；version+§2字段 | 200设置及只影响新签发的说明 |
| GET/PUT `/system/smtp`、POST `/system/smtp/unconfigure` | admin；version+§8配置/凭据动作 | 200安全配置，永不回显credential_ref/value |
| POST `/system/smtp/test` | admin；recipient | 202 test job ID，不改变保存配置 |
| GET `/system/mail-jobs`、`/system/mail-jobs/{id}`；POST `/{id}/retry` | admin；分页或version | 安全状态/channel/尝试次数/固定reason；只对仍合法原材料重试 |

system list用cursor现有签名keyring，每次当前admin验证；摘要绑定scope/filter/order但不绑定limit，默认25/max100，keyset按created_at+ID；role固定枚举无新管理API。密码弱/长度、username占用用400/409+安全FieldError；token失效410统一；容量429，基础设施503，Unknown503+lookup提示；公开reset不得把内部jobID、故障原因或已注册状态投影出去。

`account.Service` 公开命令与表一一对应；`Authenticate(ctx,cookie)→Human`、`RequireCurrentSession`、`AuthorizeSystem`、`TouchActivityInTx`、`LookupCommand` 是正式后端端口，敏感返回为opaque Cookie/LinkMaterial，仅HTTP或受限Sink适配器消费。未来域不导入HTTP读取cookie。所有mutation facade封装完整plan/WithinTx；只有明列InTx组合口使用调用方Tx，不外部I/O。

D07 绑定既有 Audit/Secret/outbound/Outbox 的 System权限但不提前安装它们全部D27管理HTTP。现有D05 Artifact/download和Runner Transfer保持其真实领域provider未绑定；无Project/Runner成功stub。管理员账户UI需要的列表不变成浏览项目内容的入口。

## 12. Central 运行、恢复与验收

延用[部署运行](../../architecture/platform-infrastructure/deployment-runtime.md)及[D05 生命周期](d05-object-storage-design.md#10-初始化健康关闭与验收)；DB自身预算后 Security/Objects/Outbox/Account共用已有30s启动预算或更短parent。SMTP未配置或远端不可达不是启动必需探测；必须验证账户schema/registry/bootstrap/恢复日志权限、Secret必需key、job引用/本地旧process证据及真实对象检查。不能以无HTTP业务端口为由跳过遗留安全恢复。

app私有assembly保留真实 Authority、Secret/OutboundClient/ObjectService/ProcessGuard/OutboxService/AccountRuntime。construct无I/O，资源owner在Initialize之前登记，所有早退/late add都同owner关闭；不再只保留接口而丢掉需组合的实例。D07 mail/hash/avatar、response read/material use、recoverylog writer/Close清理工作和Outbox一起纳入guard退休条件：先StopAdmission/StopClaims，允许已准入HTTP与mail drain；这些真实join后才ObjectRuntime.Drain释放guard。

首信号不直接取消全部HTTP BaseContext；原drain期限/第二信号/超时沿现有lifecycle。额外force总共1s，各socket关闭、worker取消/join、outbound/object force共用同ctx；即使Sink阻塞耗尽该预算，仍最后实际发起DB ForceClose，不跳过或重置期限；D03取消phase100ms不改。任何账户/SMTP/hash/回应读取/日志Write、Sync或Close工作尚未join时，只调用ObjectService.Force关闭transport，不能Runtime.Force释放ProcessGuard；保持到真实OS退出。它只证明本地进程死，不能证明SMTP未送达。

10s健康采样/共享2s round/20s陈旧沿旧规则；账户DB/关键ring/provider/worker故障为unavailable，SMTP配置与最近投递失败是独立安全诊断，不让未配置SMTP阻断技术健康。`readyz` 仍如实503显示未实现Project/Runner等，identity/system已绑定状态与project unbound分开，不因D07上线声称全产品ready。无匿名邮件诊断接口。

验收使用owned PostgreSQL/MinIO和私网SMTP fixtures，禁止连接既有DB/真实邮箱/改放行loopback。新 `scripts/test-accounts.sh` 组合既有对象/出站/PG隔离机制与仅本任务SMTP listener，私网CIDR按正式outboundpolicy授权，TLS fixture固定host+独立测试CA。每个异常分支都exact resource finally清理，不共享后台进程或默默skip缺fixture。

| 场景 | 必须证明的真实结果 |
| --- | --- |
| T01 migration/兼容 | 原fresh及9→10仍保旧Audit/Secret/Outbox、新非法NULL/owner/action拒绝及rollback原子；另真实fresh→11/10→11保smtp configured/未配置及旧ref/version、pending/0/终态/合法processing和真实attempt（旧Ref=NULL不猜回填）；新三字段默认/NULL/范围及Ref/lease非法组合、claimed/sending/retry_wait、attempt6合法7拒绝、retry_wait partial index公平；processing不迁移造attempt/发送/join，故障整Tx rollback；00001–10字节不变 |
| T02 密码/keyring | 15/128边界、中文/空格/组合字符、不trim、弱表、坏PHC；Argon并发峰值/取消实际join；key跨用途/重复/换材料/旧引用缺key拒绝，轮换旧CSRF/receipt仍可验证 |
| T03 bootstrap | 并发只一管理员；真实COMMIT掉响应先核事实；restart不改密码/不重复输出；提交后日志前crash可走普通reset，无专用重设口 |
| T04 公开隐私/挑战 | 存在/不存在同status/shape/channel/hash参数和队列；threshold/IP/成功重置；captcha replay/跨browser/换email/key/并发消费/重启失效；官方Vue真实旋转和键盘harness成功 |
| T05 Session/CSRF | Cookie flags、伪Host/Origin/null/multiOrigin/CSRF/forwarded拒绝；固定issued期限、idle/absolute/touch排除poll；并发logout/reset/改密与System mutation由User锁序线性化；旧Session立即401，不停止执行；两次合法Cookie重放各自read-attempt/lease，一方Cancel须真实join后仅释放自身，另一仍受保护；5m/撤销/idle/absolute/改密后均不恢复Cookie或复活Session |
| T06 邀请/reset | 唯一email/username并发；24h固定/同链接不续期；两次兑换/撤销/到期抢占；unknown同义查receipt/异义409；公开无投递泄漏；清理无Secret引用/lease孤儿 |
| T07 权限/事务 | 普通本人Audit成功但System管理403；admin不能Avatar他人/Project Owner；Service伪cause/错误owner/fence/来源/issuer拒绝；account_response跨browser/command完整HMAC/User/Session/password_version/ref/Purpose/process/fence拒绝，不借SMTP字段；原登录/Acquire/Read提交Unknown及Audit失败零材料/Set-Cookie，exact核实及独立lease恢复；五项usage字段矩阵/结果匹配、Apply拒read、跨Service/改字段/缺计划拒绝；旧reference及lease口不能绕过account planning，非账户旧消费者兼容；Release/Read重验actual lease mapping，Acquire同owner异ID/终态不复活；全批锁缺低序/SH→EX/映射变更fail closed，InTx零Discover/补锁/nested/外部读；AccountDelivery exact job/current_attempt/process/fence/config/ref/Purpose/lease及reset User/password_version错配拒绝，源码无Tx内usage discover；失效后只凭实际join+exact cleanup/lease释放，不恢复读，AccountResponse兼容不退化 |
| T08 SMTP协议 | none、STARTTLS、TLS+私CA真实投递；证书host/链错及STARTTLS降级拒绝；AUTH与每MAIL单独BeginSend；policy更新/DNS全结果/pinning；大reply/slowpeer/CRLF不泄密且有界 |
| T09 外发竞争 | 同一Authority gate下SMTP原首写前/后barrier与revoke/redeem/reset-complete/normal-password-change/expiry/config竞争；普通改密保留reset row仍拒后续发送，DB→内存反序/独立第二gate/取消等候泄锁/EX后SH插队拒绝；日志精确卡队首资格授予前/后：EX先提交零新资格/零Write，资格先则允许调度间隙后Write但链接已失效；不拿入队/标记称syscall已进入；无DB Tx跨I/O，日志Write/Sync不持SH；原lease直到实际join；DATA后断响应unknown可重复且从不假sent |
| T10 durable jobs | handler业务+marker同Tx；晚注册/重启canonical补job；真实无link test意图/current admin/configured；重复event不重复意图；首次+5最多6次、降低预算不重置已用、人工新周期、retry_wait/到期公平100+1、foreign live不阻断；claim→discover/acquire两短Tx间crash/unknown无外部I/O，原fence核实；新attempt Ref不可变且与lease成对，legacy Ref=NULL在多次SMTP替换后只经正式exact lease核实补原值，无法核实保pending/unknown且保护cleanup不删；opaque跨issuer/process/attempt、未登记工作/假completion拒绝，Checkpoint/Finish未知不重发送，未证死亡不接管processing/旧attempt；Claim后零Prepare/从未Acquire、第二Tx明确回滚可终局且不先造lease；原Acquire晚COMMIT/Unknown与job+attempt锁串行，acquisition-stop自身Unknown未核实保cause，迟到旧prepared被join位/摘要拒；checkpoint后crash/restart续清；两材料槽存在/不存在组合、顶层SECRET_NOT_FOUND合法零Release，嵌套provider NotFound/依赖错/Apply内缺行不得吞；最终job/Audit/terminal原子且unknown不重发；Claim未移交Unknown/返回前取消、成功返回后登记前取消含Stop/Force、Recover与迟到返回抢同一私有决议：只一个终局责任且无丢handle/重移交；原DB未join不能清理，确证回滚/晚COMMIT经原writer确认，未确认保cause；私有待确认不假I/Ojoin或server终止，原共享预算不延长 |
| T11 恢复日志 | 真实0600文件只在bootstrap/合法backend_log资格下出现capability；configured失败绝不降级；普通日志/Audit/API/异常fmt无明文；32等待+1active/串行完整行/先登记；A阻塞Sync、B排队时配置/revoke先提交，B出队当前校验零资格；GrantOnce迟到/跨ticket/重复拒绝，准入超时/提交Unknown零Write；授予后至真实Write暂停时EX可提交；分别阻塞Write/Sync，Wait先Unknown但Done/lease未终局，多Wait结果一致；完整Write+Sync才written，部分/Sync故障unknown，不自动重印bootstrap/截断或乱换path |
| T12 Avatar/profile | 静态JPG/PNG/WebP成功且元数据去除；SVG/APNG/动画/伪MIME/像素炸弹/尾随拒绝；samekey不同原bytes冲突；Publish后切换前crash/Session撤销、两次替换交错、cleanup与迟到Publish/Consume竞争，无孤儿/复活；清理Unknown先核事实、current引用不删、真实reader关闭lease |
| T13 根装配/停机 | 真正非stub System端口；SMTP空仍技术健康；所有Initialize早退/late add/blockedI/O/第二信号/hash在算及response read未join，sharedguard不早退；单ticket Done不代替全Sink Joined，Wait取消不当join；Sink Write/Sync/Close阻塞耗尽原共享1s仍实际发起DB最后force，不延长预算/重复Close任务，日志准入1s不成为新增停机预算；既有全套app/PG/安全/对象/Outbox断言不放宽 |

执行门槛：`AGENTEAM_GO=/workspace/toolchains/go1.27.1/bin/go sh scripts/check-go.sh`；真实模块测试 `AGENTEAM_GO=... sh scripts/test-accounts.sh`（脚本明确运行 `go test -tags=integration ./tests/account/... ./tests/accountmail/... ./internal/central/app/...` 普通及race）；模块最终沿现有 `scripts/test-postgres.sh`、`test-security.sh`、`test-objects.sh` 的**无过滤**兼容任务，加完整新HTTP/SMTP套件。harness执行其固定package lock的 `npm ci && npm run test`，包含真正浏览器，不把仅单测/类型检查当端到端。具体命令与耗时由实施/V记录，本S01没有运行这些产品测试。

技术证据采用已冻结 R01 `/tmp/agenteam-d07-r01-bnRjHRYP/report.md`（SHA256 `65f8ec602bb4a67f10fcfba10838a53a51c94ecb6e98eecc9cef06ed8c03ae01`）、`evidence.sha256`（`779309a97a2f19fbbcee06ebcdd2b7576acc1294cac179b56ed931bb872ecf2f`）；其真实编译/几何/短hash实验不代表D07运行验收。作者交付附actual-read manifest、UTF-8/LF/EOF/whitespace及本地链接检查。没有用户产品待定；新增公共口、依赖和旧文件增量必须先经root/独立V采纳再实施。
