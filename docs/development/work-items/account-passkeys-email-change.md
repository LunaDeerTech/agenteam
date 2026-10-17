# Passkey、登录与安全及个人邮箱变更

状态：2026-10-16 已实现并完成适用的本地功能与安全验证，基线 `c09d69b71e9ca982e02089560eeeaeb7765749e8`。本卡是本轮唯一工程合同；本轮证据与待用户验证的部署边界见第5节。系统设置、系统导航及全局开关不新增任何 Passkey 项。

## 1. 范围与完整旅程

| 入口 | 用户任务与完成事实 |
| --- | --- |
| 登录页 | 保留邮箱/密码登录，增加无需填写邮箱的 Passkey 登录；成功后确认当前 Session，再进入应用。 |
| `/settings/password` | 保留路由，个人导航与页题改为“登录与安全”；现有改密与 Passkey 列表分区，支持添加、命名、删除。 |
| 个人资料邮箱 | 邮箱旁提供修改入口；modal 内先以新鲜 Passkey 验证身份，再向指定新邮箱发送数字码，验证成功后原子更换邮箱。普通资料 PATCH 不接受邮箱。 |
| 首次登记/所有 Passkey 丢失 | 在密码登录的 Session 内，以当前密码和原邮箱实际收到的数字码验证；首次登记或恢复后才能创建新的 Passkey。不能因已有 Session 或管理员角色跳过。 |
| 原邮箱也不可用/首次邮箱初始化 | 部署者CLI生成指定账号/目标邮箱的一次授权；用户在个人“登录与安全”的共享modal输入授权与当前密码、验证目标邮箱数字码。初始化与恢复共用入口，不要求用户手工调用API。 |

用户会在独立测试服务器配置域名并验证真实设备；本轮先完成可运行实现、单元/受控认证器与隔离应用验证。现有用户实例、域名、账号、SMTP 配置和密钥不变，不发送外部邮件。

## 2. 身份、挑战与持久状态

- 使用成熟 WebAuthn 实现，Passkey 绑定稳定 UserID，不能绑定可修改的邮箱或用户名。用户名为空的登录使用 discoverable credential；登记要求 resident key，登记与断言均要求 User Verification。公钥、credential ID、签名计数及必要备份状态持久保存；不接收或保存认证器私钥。
- RP ID 与允许 Origin 只从固定部署 `PUBLIC_ORIGIN` 派生，RP ID 为规范 hostname，Origin 包括实际协议和端口；不从请求 Host、转发头或客户端 body 选择。IP Origin 明确返回能力不可用，不影响原密码登录。无跳过证书、宽泛 Origin 或 Dev 模式隐式绕过。
- 挑战随机、有效期5分钟、一次性并持久绑定用途。匿名登录绑定匿名浏览器上下文；登录后的登记、删除及邮箱操作绑定当前 UserID、Session、用途和相关操作目标。断言消费与凭据/Session/邮箱写入同事务，重放不能再执行副作用。
- 成功预检不代替提交时的当前授权：重新读取 Session、账号状态、当前邮箱、凭据集合/版本及相关申请；跨身份、跨 Session、跨用途和已失效/已消费挑战拒绝。邮箱改变不改变 UserID 或已有公钥归属。
- 需要新增明确的邮箱验证事实。`email_verified=false`不等于必须运维初始化：普通注册用户可用当前密码及原邮箱实际验证码建立验证事实；只有服务端`email_initialization_required=true`的未验证bootstrap账号须先运维初始化。该字段来自真实bootstrap记录，不按管理员角色或固定邮箱猜测。既有邮箱和角色都不是验证证据，不能将后台恢复日志里的材料当作邮箱收到验证码。

## 3. 重新认证、邮件与恢复

有可用 Passkey 时，添加/命名/删除 Passkey 和修改邮箱要求新鲜 UV 断言。grant有效期5分钟、一次性，绑定当前用户/Session及action；命名/删除同时绑定目标Passkey ID。邮箱用途不接受key管理action/目标。验证只授权对应当前操作，不能作为通用管理员通行证。首次登记与全 key 丢失恢复采用当前密码加原邮箱数字码；恢复grant只能登记新key，成功登记时原子撤销旧keys及旧管理授权，避免遗失的key继续登录；恢复开始不先删除旧keys。命名只改变显示名称，不改变公钥身份。

数字验证码使用密码学安全随机源，服务端只存受保护摘要；绑定用途、申请、用户/Session 和规范邮箱。数字码固定6位ASCII数字。申请有效期10分钟，同一用户/用途的连续窗口累计最多5次错误，发送间隔60秒；重复request和resend均不能洗掉窗口预算；重发使旧码立即失效，不重置累计错误预算，也不能无限延长同一申请的期限。失效、取消、替代与已用状态均有可执行下一步；不输出码、密码、WebAuthn 原始认证材料或运维授权到普通日志/URL/浏览器持久存储。

新邮箱码、原邮箱验证/恢复码和旧邮箱变更通知使用专用邮件 kind，严格 SMTP 投递，不能 fallback 到 backend log。受理、SMTP 接受和实际邮箱收到是不同事实；未配置/不可用须有真实失败或等待反馈，不能用“已发送”假成功。隔离 sink 可测试收码链，但不代证外部邮箱。

新邮箱确认在同一事务内检查唯一性、当前身份/Session、原邮箱和 Passkey 授权状态，消费申请、更新规范邮箱和验证事实、写 Audit/receipt，并废弃旧 reset 与旧邮箱申请。保留本次强验证 Session，撤销其他 Session；向原邮箱排队发送变更通知，通知失败不回滚已完成变更，也不把通知当作授权依据。

bootstrap 初始邮箱不能自动视为真实联系地址。部署者初始化/邮箱丢失恢复必须有单独、一次性、短期授权和可运行入口；CLI与授权消费流程见[运行说明](../backend/account.md#passkey-与邮箱验证部署边界)，不得以直接改库教程、长期万能 token 或管理员页面开关代替。此运维流程仍要求当前密码登录Session和当前密码；同时失去密码的离线账号接管不纳本轮，也不提供对应假入口。授权绑定指定UserID、`initialize_email`或`recover_account`目的、目标邮箱，固定30分钟有效；同用户/用途重新签发撤销未过期旧授权。实际新邮箱仍须通过SMTP数字码证明持有；recover_account成功撤销旧keys及其他Session/reset，保当前强验证Session。

## 4. HTTP 与前端状态边界

首批固定路径以下均带 `/api/v1` 前缀；公开能力独立读取，不扩既有 bootstrap DTO。二进制字段使用原生 WebAuthn JSON 的无 padding base64url；前端在浏览器 API 边界转换，不能自行改变签名内容。

| 方法与路径 | 请求与成功响应 |
| --- | --- |
| `GET /auth/passkeys/capabilities` | `{available:boolean,reason:"available"\|"secure_origin_required"\|"domain_required",rp_id:string\|null}`。available为false时rp_id必须为null；仅代表固定部署能力，浏览器支持另行判断。 |
| `GET /me/passkeys` | 当前 Session；`{version,email_verified,email_initialization_required,items:[{id,label,created_at,last_used_at}]}`，集合version初始为"1"且为规范正int64十进制字符串，id为UUIDv7，last_used_at可为null；最多20个活动keys。email_initialization_required为必填boolean，定义为未verified且真实bootstrap记录存在。 |
| `POST /auth/passkeys/login/options` | 匿名browser CSRF、无需Idempotency-Key；返回 `{operation_id,expires_at,public_key}`，public_key为原生PublicKeyCredentialRequestOptionsJSON，不要求邮箱。 |
| `POST /auth/passkeys/login/complete` | 匿名browser CSRF及Idempotency-Key，body恰为 `{operation_id,credential}`，credential为AuthenticationResponseJSON；成功沿既有登录响应及Session Cookie，再GET session确认。 |

operation_id为不透明UUID，选项仅在其用途/浏览器和期限内有效。认证器JSON采用成熟库对应结构，客户端不拼接或修改签名材料。

下表路径亦带 `/api/v1`。options用当前Session CSRF，后续命令同时使用Idempotency-Key。options/confirm/管理命令成功200；实际邮件入队的request/resend成功202，不表示已送达。命名label去首尾空白后须为1–64个Unicode字符、最多256个UTF-8字节。

| 方法与路径 | 请求与成功响应 |
| --- | --- |
| `POST /me/passkeys/reauth/options` | `{purpose,action?,passkey_id?}`；purpose为`manage_passkeys`或`change_email`。manage必须带`register`/`rename`/`delete` action，后两者必须带目标id；change_email禁止action/id。返回options operation。 |
| `POST /me/passkeys/reauth/complete` | `{operation_id,credential}` → `{grant_id,purpose,expires_at}`；服务器保存并验证action与目标绑定，不以响应少字段丢失约束。 |
| `POST /me/passkeys/registration/options` | `{grant_id,label,expected_version}` → `{operation_id,expires_at,public_key}`；public_key为原生PublicKeyCredentialCreationOptionsJSON，expected_version为Passkey集合版本；options原子将grant消费并绑定该operation，取消后须重新认证。 |
| `POST /me/passkeys/registration/complete` | `{operation_id,credential}` → `{version,id,created_at}`；credential为原生RegistrationResponseJSON，登记与grant/挑战消费原子提交。 |
| `PATCH /me/passkeys/{id}` | `{expected_version,label,grant_id}` → `{version,id,created_at}`。 |
| `DELETE /me/passkeys/{id}` | `{expected_version,grant_id}` → `{version,id,created_at}`；仍返回确认回执，不能凭列表未见目标替代原结果。 |
| `POST /me/passkeys/recovery/request` | `{password}` → `{request_id,expires_at,resend_after}`；当前Session发起，使用原邮箱真实SMTP码。 |
| `POST /me/passkeys/recovery/resend` | `{request_id}` → 同申请回执；成功202，沿同用户/用途的窗口预算。 |
| `POST /me/passkeys/recovery/confirm` | `{request_id,code}` → 上述grant结构，服务端限定为register；已有keys时作为丢失恢复，最终新登记才撤旧keys。 |
| `POST /me/email-change/request` | `{grant_id,email,expected_version}` → `{request_id,expires_at,resend_after}`；expected_version是`/me`的User版本，规范邮箱绑定该唯一申请。 |
| `POST /me/email-change/resend` | `{request_id}` → 同申请回执；重发不转移邮箱/用途，不清尝试预算。 |
| `POST /me/email-change/confirm` | `{request_id,code}` → `{version,email,created_at}`；当前Cookie不旋转，保本次强Session并撤其他Session，按当前权限支持原命令回执恢复。 |
| `POST /me/email-initialization/request` | `{authorization,password}` → 202同申请回执；目标邮箱和初始化/恢复purpose只由授权记录决定，客户端不另传email/purpose。 |
| `POST /me/email-initialization/resend` | `{request_id}` → 202同申请回执，沿原绑定目标与窗口限制。 |
| `POST /me/email-initialization/confirm` | `{request_id,code}` → 200 `{version,email,created_at}`，不附登记grant；其后正常首登记再次以当前密码及新邮箱码验证。 |

Passkey集合版本与User版本独立，不互相推导。能力/所有读取成功200，严格拒绝未知字段。错误或过期验证码用400 `INVALID_ARGUMENT`、field `/code` 的 `INVALID_OR_EXPIRED`；失效grant用409 `INVALID_STATE`，限次用429 `RATE_LIMITED`。SMTP未配置/停用用409 `INVALID_STATE`，依赖失败503；不把验证码错误当Session401而清空身份。原key/body显式重放恢复receipt，无新增GET lookup，complete重放不能再次消费挑战/grant。运维初始化/恢复共用上述email-initialization路径，服务器按授权purpose区分。申请回执不含目标邮箱，modal提示发送到部署者授权文件所列的邮箱，不从token猜测或新增可改邮箱字段；确认成功后用权威receipt.email更新资料。这些操作不混入系统设置或普通资料 PATCH。

所有写操作沿当前 Origin、浏览器/Session CSRF 和幂等命令边界。相同命令原 key/body 恢复原 receipt，先核当前授权；不能因挑战已消费就把已提交重放错误判作新失败。WebAuthn 用户取消在提交前返回可重试状态；提交开始后断网/结果未知保持原意图并冻结可改字段，显式同请求恢复，不创建新 key 重试。GET 仅观察当前状态，不能代替原命令回执。

登录成功涉及 Cookie：用当前 Session 观察确认登录，不能凭丢失响应猜身份或从历史回执重新发行另一 Cookie。密码、验证码和授权材料只留当前受限内存；完成、显式放弃或身份失效后清理。409 保留非秘密草稿并显式采用当前版本，401/403 沿现有身份/权限代际处理，不静默注销其他有效 Session。

个人“登录与安全”提供初始化/恢复邮箱的共享modal入口，普通页面只短提示向部署者获取授权，不展示CLI长文。普通未验证邮箱仍可走密码加原邮箱码，不因email_verified=false错误阻断。页面继续采用开放表单、少说明与共享控件；命令反馈、错误、限流与验证方式不可用在对应任务内显示。邮箱 modal 保留输入/步骤，关闭未决写需明确后果；Passkey 系统弹窗取消不显示为后端错误。IP/不支持的浏览器显示真实能力限制及密码登录路径，不能出现可点击却无功能的认证按钮。

## 5. 实施所有权与验收

root 负责 Git、隔离工作树与最终组合；account_backend 负责领域/持久化、WebAuthn 库、HTTP/邮件/CLI 与真实装配；proxy_copy 负责新 Vue 页面与 browser credential 适配；account_contract 维护本卡、当前状态及授权蓝图/运行说明。测试与独审由 root 指定独立 owner，不与实现者混称独审。

完成出口包括：密码入口不回退；能力禁用边界；首次登记/再次登记/命名/删除与免邮箱登录；密码加旧邮箱码恢复；邮箱强验证到真实隔离 SMTP 收码和原子更新；部署者初始化/邮箱丢失恢复；错误码、限次/过期/重发与跨用途/Session 反例；当前凭据变更竞争、唯一性、Unknown 原请求恢复与 Session/reset 撤销；日志无秘密泄露。浏览器覆盖可用的虚拟认证器、宽窄主题/键盘/取消/焦点，真实设备与公网域名留给用户独立服务器验收，不将缺少公网条件写作产品已通过。

当前检查点：后端产品 `163c21` 与前端产品 `257cc` 已集成主树，69项web unit、类型/build、受控视觉与P2/P3返修结果按未变输入保留。owned r3的真实浏览器run02通过密码加原邮箱码恢复→原生登记→免邮箱登录→新鲜Passkey/新邮箱码→邮箱确认；实际提交丢响应后原key/body恢复，保当前Cookie/Session并撤另一Session，验证码同key不双扣、四错后成功与五错锁定均已验证。独审run03另通过跨Session/operation/action/target拒绝、登记grant单消费者，以及认证key撤销后正确邮箱码不能确认；两把真实server key、退出及零外域请求已核。

accountmail完整非integration单测通过；account完整执行仅旧HEAD/schema测试因新增合法布尔字段解析失败，测试模型修正后该项定向通过，其余保留同输入已执行结果，不称一次全绿。CLI initialize_email实际进程exit0、空stdout、0600授权及同事务Audit已核；真实SMTP码、错码400及原key重放只扣一次、正确确认HTTP200通过。原harness随后将正式字符串Version误解为int64而退出1，测试修正后没有重复签发/发送/确认；严格owned只读续验实际通过，核唯一已提交receipt、verified邮箱、已消费授权/申请、others事件与当前Session保留、其余撤销。续验首次编译失败后修正通过。普通邮箱变更的旧verified邮箱通知仅一条intent/job，已sent且terminal/io_joined。

r1迁移Down误项、r2异步旧邮件观测错误已修并精确清理，首r3编译错误亦保留；作者browser run01导航Timeout、独审run02准备Error的原因未定，原失败保留，后续通过不改写原因。独审run01过严测试前置未进入业务。任务Vite5274已退出且无监听；owned r3的App及runner均实际退出0，4容器/4网络逐exact ID清零，5270无监听且可绑定，runtime/sidecar/临时文件已撤。CLI未再次登出的测试Session随owned数据库清退终结。

验证限制：CLI真实链覆盖initialize_email与同次提交续查；recover_account的旧key/reset分支未独立穷举真实实测，不宣称全部恢复组合均覆盖。2026-10-17用户授权将最新代码更新到既有18443：329591cc后端/schema146与同版私有Vue快照已保数据部署，真实1440/390只读页面核验已通过，密码登录与IP能力限制、初始化入口及系统项不变均已核；前两次浏览器定位命中隐藏modal属harness错误，限定定位后通过，未改产品。公网两次IP TLS拒绝、域名SNI对照HTTP503上游证书拒绝均未取nonce，不称公网身份验证通过。IP下Passkey不可用，原密码登录保留，本次未注册真实Passkey或发外部邮件；真实域名、物理认证器、云同步和外部收件由用户在独立服务器后验。复现与运行边界见[账号运行说明](../backend/account.md#passkey-与邮箱验证部署边界)。
