# Passkey、登录与安全及个人邮箱变更

状态：2026-10-16 用户已授权完整实现，基线 `c09d69b71e9ca982e02089560eeeaeb7765749e8`。本卡是本轮唯一工程合同；接口细节由后端与前端在本卡固定，尚未实现或验证的部分不以既有密码登录结果代证。系统设置、系统导航及全局开关不新增任何 Passkey 项。

## 1. 范围与完整旅程

| 入口 | 用户任务与完成事实 |
| --- | --- |
| 登录页 | 保留邮箱/密码登录，增加无需填写邮箱的 Passkey 登录；成功后确认当前 Session，再进入应用。 |
| `/settings/password` | 保留路由，个人导航与页题改为“登录与安全”；现有改密与 Passkey 列表分区，支持添加、命名、删除。 |
| 个人资料邮箱 | 邮箱旁提供修改入口；modal 内先以新鲜 Passkey 验证身份，再向指定新邮箱发送数字码，验证成功后原子更换邮箱。普通资料 PATCH 不接受邮箱。 |
| 首次登记/所有 Passkey 丢失 | 在密码登录的 Session 内，以当前密码和原邮箱实际收到的数字码验证；首次登记或恢复后才能创建新的 Passkey。不能因已有 Session 或管理员角色跳过。 |
| 原邮箱也不可用 | 提供真实可执行的部署者恢复入口，以显式、短时、一次性的运维授权处理指定账号；不提供无后端能力的恢复按钮。 |

用户会在独立测试服务器配置域名并验证真实设备；本轮先完成可运行实现、单元/受控认证器与隔离应用验证。现有用户实例、域名、账号、SMTP 配置和密钥不变，不发送外部邮件。

## 2. 身份、挑战与持久状态

- 使用成熟 WebAuthn 实现，Passkey 绑定稳定 UserID，不能绑定可修改的邮箱或用户名。用户名为空的登录使用 discoverable credential；登记要求 resident key，登记与断言均要求 User Verification。公钥、credential ID、签名计数及必要备份状态持久保存；不接收或保存认证器私钥。
- RP ID 与允许 Origin 只从固定部署 `PUBLIC_ORIGIN` 派生，RP ID 为规范 hostname，Origin 包括实际协议和端口；不从请求 Host、转发头或客户端 body 选择。IP Origin 明确返回能力不可用，不影响原密码登录。无跳过证书、宽泛 Origin 或 Dev 模式隐式绕过。
- 挑战随机、短时、一次性并持久绑定用途。匿名登录绑定匿名浏览器上下文；登录后的登记、删除及邮箱操作绑定当前 UserID、Session、用途和相关操作目标。断言消费与凭据/Session/邮箱写入同事务，重放不能再执行副作用。
- 成功预检不代替提交时的当前授权：重新读取 Session、账号状态、当前邮箱、凭据集合/版本及相关申请；跨身份、跨 Session、跨用途和已失效/已消费挑战拒绝。邮箱改变不改变 UserID 或已有公钥归属。
- 需要新增明确的邮箱验证事实。既有邮箱、bootstrap 默认 `admin@mail.com` 和管理员角色都不是验证证据；不能将后台恢复日志里的材料当作邮箱收到验证码。

## 3. 重新认证、邮件与恢复

有可用 Passkey 时，添加/删除 Passkey 和修改邮箱要求新鲜 UV 断言。验证只授权对应当前操作，不能作为长期通用管理员通行证。首次登记与全 key 丢失恢复采用当前密码加原邮箱数字码；恢复须明确旧 key 的撤销与会话影响，避免遗失的 key 继续登录。命名只改变显示名称，不改变公钥身份。

数字验证码使用密码学安全随机源，服务端只存受保护摘要；绑定用途、申请、用户/Session 和规范邮箱。申请须有明确期限、错误次数上限和重发间隔，重发不能延长同一旧码的有效期或无限重置尝试预算。失效、取消、替代与已用状态均有可执行下一步；不输出码、密码、WebAuthn 原始认证材料或运维授权到普通日志/URL/浏览器持久存储。

新邮箱码、原邮箱验证/恢复码和旧邮箱变更通知使用专用邮件 kind，严格 SMTP 投递，不能 fallback 到 backend log。受理、SMTP 接受和实际邮箱收到是不同事实；未配置/不可用须有真实失败或等待反馈，不能用“已发送”假成功。隔离 sink 可测试收码链，但不代证外部邮箱。

新邮箱确认在同一事务内检查唯一性、当前身份/Session、原邮箱和 Passkey 授权状态，消费申请、更新规范邮箱和验证事实、写 Audit/receipt，并废弃旧 reset 与旧邮箱申请。保留本次强验证 Session，撤销其他 Session；向原邮箱排队发送变更通知，通知失败不回滚已完成变更，也不把通知当作授权依据。

bootstrap 初始邮箱不能自动视为真实联系地址。部署者初始化/邮箱丢失恢复必须有单独、一次性、短期授权和可运行入口；具体 CLI 与授权消费流程由后端固定后写入[运行说明](../backend/account.md)，不得以直接改库教程、长期万能 token 或管理员页面开关代替。授权至少绑定指定 UserID、目的、目标邮箱和期限，实际新邮箱仍须通过 SMTP 数字码证明持有。

## 4. HTTP 与前端状态边界

首批固定路径以下均带 `/api/v1` 前缀；公开能力独立读取，不扩既有 bootstrap DTO。二进制字段使用原生 WebAuthn JSON 的无 padding base64url；前端在浏览器 API 边界转换，不能自行改变签名内容。

| 方法与路径 | 请求与成功响应 |
| --- | --- |
| `GET /auth/passkeys/capabilities` | `{available:boolean,reason:"available"\|"secure_origin_required"\|"domain_required",rp_id:string\|null}`。仅代表固定部署能力，浏览器支持另行判断。 |
| `GET /me/passkeys` | 当前 Session；`{version,email_verified,items:[{id,label,created_at,last_used_at}]}`，version为规范正int64十进制字符串，last_used_at可为null。 |
| `POST /auth/passkeys/login/options` | 匿名browser CSRF、无需Idempotency-Key；返回 `{operation_id,expires_at,public_key}`，public_key为原生PublicKeyCredentialRequestOptionsJSON，不要求邮箱。 |
| `POST /auth/passkeys/login/complete` | 匿名browser CSRF及Idempotency-Key，body恰为 `{operation_id,credential}`，credential为AuthenticationResponseJSON；成功沿既有登录响应及Session Cookie，再GET session确认。 |

operation_id为不透明UUID，选项仅在其用途/浏览器和期限内有效。登记拟使用 `/me/passkeys/registration/options` 的 `{grant_id,label}` 与 `/complete` 的 `{operation_id,credential}`；重新认证、grant、邮箱、CLI及具体期限/限次待后端下一批固定，不能由前端猜测成功结构或增设lookup。这些操作不混入系统设置或普通资料 PATCH。

所有写操作沿当前 Origin、浏览器/Session CSRF 和幂等命令边界。相同命令原 key/body 恢复原 receipt，先核当前授权；不能因挑战已消费就把已提交重放错误判作新失败。WebAuthn 用户取消在提交前返回可重试状态；提交开始后断网/结果未知保持原意图并冻结可改字段，显式同请求恢复，不创建新 key 重试。GET 仅观察当前状态，不能代替原命令回执。

登录成功涉及 Cookie：用当前 Session 观察确认登录，不能凭丢失响应猜身份或从历史回执重新发行另一 Cookie。密码、验证码和授权材料只留当前受限内存；完成、显式放弃或身份失效后清理。409 保留非秘密草稿并显式采用当前版本，401/403 沿现有身份/权限代际处理，不静默注销其他有效 Session。

页面继续采用开放表单、少说明与共享控件；命令反馈、错误、限流与验证方式不可用在对应任务内显示。邮箱 modal 保留输入/步骤，关闭未决写需明确后果；Passkey 系统弹窗取消不显示为后端错误。IP/不支持的浏览器显示真实能力限制及密码登录路径，不能出现可点击却无功能的认证按钮。

## 5. 实施所有权与验收

root 负责 Git、隔离工作树与最终组合；account_backend 负责领域/持久化、WebAuthn 库、HTTP/邮件/CLI 与真实装配；proxy_copy 负责新 Vue 页面与 browser credential 适配；account_contract 维护本卡、当前状态及授权蓝图/运行说明。测试与独审由 root 指定独立 owner，不与实现者混称独审。

完成出口包括：密码入口不回退；能力禁用边界；首次登记/再次登记/命名/删除与免邮箱登录；密码加旧邮箱码恢复；邮箱强验证到真实隔离 SMTP 收码和原子更新；部署者初始化/邮箱丢失恢复；错误码、限次/过期/重发与跨用途/Session 反例；当前凭据变更竞争、唯一性、Unknown 原请求恢复与 Session/reset 撤销；日志无秘密泄露。浏览器覆盖可用的虚拟认证器、宽窄主题/键盘/取消/焦点，真实设备与公网域名留给用户独立服务器验收，不将缺少公网条件写作产品已通过。

当前检查点：行为合同及能力/列表/匿名Passkey登录首批wire已固定，可并行实现；其余grant/邮箱/CLI与具体期限/限次待下一批约定。结构升级00146由account_backend唯一维护。未运行本轮产品测试、未部署、未发外部邮件。
