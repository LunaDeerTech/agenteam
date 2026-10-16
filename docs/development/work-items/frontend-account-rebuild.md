# 新 Vue 账号入口与个人设置

2026-10-16。本轮行为、接口取舍与验收的唯一工作项；当前实施/资源状态见[当前状态](../../../.agent-state/current.md)。用户已授权从零实现登录、找回/重置、相关会话与接口，以及完整个人设置。既定[页面蓝图](../../frontend-design/redesign/page-blueprints.md#23-账号入口)含邀请兑换，本轮一并闭环，不增加开放注册、邀请管理、MFA、设备管理或邮箱修改。

## 1. 交付范围与设计

| 页面 | 用户任务与完成结果 |
| --- | --- |
| `/login` | 邮箱、密码登录；真实需要时才出现旋转挑战；确认当前会话后返回合法目标。提供找回密码。 |
| `/invite` | 先检查链接，显示只读邀请邮箱，再填写用户名、显示名、密码和确认；兑换后明确返回登录，不自动登录。 |
| `/forgot-password` | 邮箱申请恢复；显示真实受理渠道，不透露账号是否存在，不承诺邮件送达。 |
| `/reset-password` | 检查链接，填写新密码和确认；完成后说明需重新登录及旧会话已失效。 |
| `/settings/profile` | 编辑用户名、显示名；邮箱只读；独立上传、替换或移除头像。 |
| `/settings/appearance` | 浅色、深色、跟随系统的小预览；明确保存/取消；服务端保存与暂时预览分开。 |
| `/settings/password` | 当前密码、新密码、确认；成功说明其他会话退出，确认本次换发的新会话。 |

账号页使用正式品牌、单任务标题、有标签字段、就近错误与一个主动作。用户已确定 `/login` 与 `/forgot-password` 共用同一AccountShell面板模板：正式Logo、标题和当前表单放在居中的轻边框容器内，配克制阴影。邀请与重置保持各自既定单任务正文。设置沿已确认的唯一 SettingsShell：宽页头与正文左起点一致，较窄开放表单靠左，无外围表单框；官方 Logo 直接取 `docs/logos/`，不重画。显示技术请求号仅用于错误详情，不把内部状态、版本、CSRF 或请求键作为日常表单字段。取消、返回、保存及加载反馈在当前任务区；页面使用一处主滚动，390px 和 200% 缩放时提交与错误可达。

顶栏不单列“个人设置”按钮，入口保留在右上用户菜单内。菜单触发器显示当前身份真实已保存的头像，未设置时使用默认占位；资料页的未上传预览不进入顶栏。头像读取和更新遵守下述同一请求owner、代际隔离与Blob生命周期，不另设请求队列。

所有已有应用路由均受会话保护，系统设置入口另依据真实 `user.role=admin` 显示/允许进入；项目与其他未实现正文仍为原骨架，不能因登录成功宣称已接入其业务权限。登录返回只接受路由器已登记的站内受保护路径，拒绝协议、host、`//`、反斜杠、控制字符、query/hash 及未匹配路径；不以返回路径授予授权。默认返回 `/`，`/settings` 仍重定向 profile。

## 2. 实现边界与提供方

新 `web/` 的客户端、会话协调、路由守卫、页面状态和业务表单独立编写，不导入、复制或包裹 `web-crape/` 的实现。旧 D26 卡仅用于确认产品规则，不继承其固定路径数量、审批流程或巨大 controller 结构。

后端继续使用已装配的 Account service、Store、Argon2id、独立 keyring、Session 撤销、CSRF、挑战、限流、链接一次消费、事务/Audit/receipt、邮件恢复和头像安全处理。当前 `/api/v1` 已覆盖这七页，首版保留下列 wire 合同；没有页面收益时不新增同义 namespace 或兼容代理。后端 owner 可修真实安全/可用性缺口，若要改变公开 shape/状态/恢复语义，须先与前端及本工作项同步，并保持其他现有模块的兼容。

| Owner | 唯一职责 |
| --- | --- |
| `account_contract` | 本合同、当前恢复状态与前端说明；把页面需求映射为契约，不改产品实现或后端说明。 |
| `vue_integration` | 新 `web/src`、配置与共享 UI 的实现、联调及作者检查。 |
| `account_backend` | 独立工作树的 account/API/装配与后端说明；真实提供方和隔离服务能力。 |
| `vue_tests` | 新账号测试与浏览器 harness；不把 fixture 成功冒充真实生产依赖。 |
| `vue_visual_review` | 未参与对应实现的安全、真实浏览器视觉与交互审查；按 root 实际派工恢复执行。 |
| root | 跨任务/资源协调、全部 Git 与最终集成。 |

## 3. 最小 HTTP 合同

精确 schema 以 [account.json](../../../api/openapi/account.json) 与 [common.json](../../../api/openapi/common.json) 为准，实际提供方为 `internal/central/account/http_auth.go`、`http_profile.go` 与 Central 账户装配。以下路径统一前缀 `/api/v1`。

`User` 包含 `id,email,username,display_name,role,theme,version,initial_password_suggestion`；`Session` 包含 `id,issued_at,absolute_expires_at,idle_expires_at`。`Profile={user,avatar}`，avatar 为 null 或 `{media_type,byte_size,sha256}`；`Preferences={version,theme}`，其 version 与 User 共用版本。ID、Version、Progress、Instant 按 common wire 解码，不按 JS 数字或本地时间猜测。

| 方法与路径 | 输入 | 成功与身份 |
| --- | --- | --- |
| GET `/auth/bootstrap` | 无 body | 200 `{csrf_token,challenge_modes:['rotate'],delivery_channel:'smtp'\|'backend_log'}`，建立匿名 browser Cookie/CSRF，不意味着当前 Session 被注销。 |
| GET `/session` | 无 body | 200 `{user,session,csrf_token}`；401 表示未认证/失效。 |
| POST `/sessions/login` | `{email,password,challenge_pass?}` | 200 `{user,session,next_path:'/'}`，设置 Session Cookie；随后 GET session 取得当前 CSRF。 |
| POST `/sessions/logout` | `{}` | 204，撤销当前 Session 并清 Cookie。 |
| POST `/auth/challenges` | `{mode:'rotate',email,login_key}` | 201 `{id,mode,master,thumb,expires_at}`。 |
| POST `/auth/challenges/verify` | `{email,login_key,challenge_id,proof:{angle}}` | 200 `{pass}`；与原 browser/email/login key 绑定的一次性 pass。 |
| POST `/password-resets/request` | `{email}` | 202 `{accepted:true,delivery_channel}`；不含存在性、job ID、token 或投递结果。 |
| POST `/password-resets/inspect` | `{token}` | 200 `{valid:true,expires_at}`；不返回目标邮箱。 |
| POST `/password-resets/complete` | `{token,new_password,confirmation}` | 204；撤销目标账号全部会话，不自动登录。 |
| POST `/invitations/inspect` | `{token}` | 200 `{email,expires_at}`；携带另一当前身份时按真实权限拒绝。 |
| POST `/invitations/redeem` | `{token,username,display_name?,password,confirmation}` | 201 `{completed:true,login_required:true}`；不返回 Session。 |
| GET `/me` | 无 body | 200 Profile。 |
| PATCH `/me` | `{version,username?,display_name?}`，至少一个实际修改字段 | 200 Profile；空显示名用 `""`，不用 null。 |
| GET/PUT `/me/preferences` | GET 无 body；PUT `{version,theme}` | 200 Preferences。 |
| GET `/me/avatar` | 无 body，无 Range | 200 受保护图片；当前 Profile 为 null 时不发请求。 |
| PUT `/me/avatar` | 原始 File bytes；允许的 Content-Type；`If-Match: "<version>"` | 200 Profile；非 multipart/JSON。 |
| DELETE `/me/avatar` | `{version}` | 204；随后重读 Profile，读取失败不否认已确认删除。 |
| POST `/me/change-password` | `{version,current_password,new_password,confirmation}` | 200 `{completed:true,next_path:'/'}`，撤销全部旧 Session 并设置新 Cookie；随后 GET session。 |

browser authority 使用匿名 `X-CSRF-Token`：login、挑战和全部 invitation/password-reset 路径。logout 与所有本人写使用 Session CSRF。两种上下文可以共存，不能因访问公开页自动注销、把 Session CSRF 用作匿名 CSRF，或重新 bootstrap 破坏未决原命令。所有实际 mutation 带不可预测 `Idempotency-Key`；challenge create/verify 与 inspect 不带幂等键。JSON 请求上限 16 KiB。

请求固定同源相对路径、`credentials: 'same-origin'`、`cache: 'no-store'`，拒绝重定向与 HTML/畸形成功体，不自行设置 Cookie/Host/Origin 或身份头。精确 status、媒体类型、有限响应 body 与必要 shape 校验后才能发布成功；204 不解析 JSON。安全Problem使用 `code,commit_state,field_errors,retry_hint,request_id`；`commit_state` 的四个合法值为 `not_started / not_committed / committed / unknown`，须保留服务端原值。正常认证拒绝可返回 `not_started`，不能因漏识别该值误进Unknown；缺失或非法状态的已发mutation仍按Unknown处理。后一次明确拒绝不抹掉前一次已保留的Unknown。页面显示固定安全文案与对应字段错误，不回显raw body、密码或底层异常。

## 4. 会话、请求所有权与恢复

新会话状态保持小而明确：`checking / anonymous / authenticated / unavailable`；页面另有 `loading / ready / dirty / submitting / confirmed / uncertain / conflict / error`，不用全局一次失败清掉所有普通字段草稿。

1. **一个实际请求 owner与浏览器Cookie边界。** 全部账号HTTP，包括头像读取，串行经过同一协调器直到响应body/取消清理真正结束；不得用裸 `<img src='/api/v1/me/avatar'>` 或另一队列绕过。可见超时/导航取消不证明服务端回滚，也不能提前放出下一身份请求。未提交的新操作明确反馈忙，不静默成功。这只协调同标签页，不能阻止另一标签页先完成新登录。浏览器在JavaScript代际检查前应用Set-Cookie，因此普通认证错误响应均不得清Session Cookie，包括GET/HEAD及POST/PATCH等不安全方法；服务端仍拒绝旧身份，前端清当前失效显示。保留显式成功logout的清Cookie及成功login/改密的换Cookie行为。独审已复现旧GET/PATCH401迟到清新Cookie，修复是否完成只见当前状态，不能由前端ignoreResponse代证。
2. **代际隔离。** 每次请求记录当前 user/session、稳定身份代际与请求代际；匿名写另记 browser context 代际。返回及 finally 均检验归属；旧响应不能改新身份、主题、头像、草稿或导航。普通同身份会话重验不销毁 dirty 草稿或重基其版本；新身份/真正失效时清旧私有内容和敏感引用。
3. **确认与再读分开。** login/change-password 收到严格成功立即清密码，持同 owner 再 GET session；会话再读失败只允许重查会话，不再 POST 已确认命令。reset 204 立即确认并清 token/密码，再核当前 Session：目标可能与当前身份不同，不能假定当前 Cookie 必然失效。DELETE avatar 204 后同样保持已确认事实。
4. **Unknown 是原命令事实。** mutation 已发出后的 transport/坏成功响应/取消、`COMMIT_UNKNOWN` 或 `commit_state=unknown` 进入 uncertain。冻结原 method/path/key/body/version/context；只允许显式原请求重放或明确放弃，不能自动换 key、改 body、重新 inspect/prepare 后重发。HTTP 当前没有命令 lookup；`retry_hint=lookup` 不代表存在可调用端点。后续401/403/410/冲突、Session检查或试登录都不证明原写未发生；上下文失效则停止重放并保留“原结果未确认”。
5. **改密丢响应的特例。** 原 Session 可能已撤销，历史幂等记录不会向旧 Session 补发新 Cookie；提示重新登录，不承诺原请求重放能恢复会话。新登录只证明当前身份，不改写原改密的 Unknown 历史。
6. **拒绝边界。** 当前 Session401/撤销清私有显示并返回登录；普通403不伪注销，CSRF_FAILED区分匿名或Session上下文并禁止该上下文的原写继续重放。VERSION_CONFLICT保留草稿，提供明确重读最新值；不自动换 version/key 重试。字段错误就近，429不虚构冷却时间，503不改造成成功。
7. **离开规则。** dirty密码/资料、主题预览或未确认写有继续编辑/放弃离开的明确选择；取消离开保留原草稿。离开只清页面责任与敏感引用，不声称取消已发出的服务端写。请求实际尾部仍归原 owner。所有关键材料仅在私有内存保留，不进 URL、storage、普通日志、截图或测试报告。

首条联调为受保护页面 → session401 → 登录 → session200 → profile → 注销 → 受保护页面重回登录。之后并行扩展邀请、恢复与三项设置；不等全文/所有矩阵完成才联调。

## 5. 页面内容与敏感材料

**登录与挑战：** 邮箱、密码保留浏览器自动填充、粘贴和 Enter 提交；提交时同步冻结最终值。挑战仅按真实服务要求出现，旋转图片与0–360角度的鼠标/键盘操作共用服务端验证，不降级为假验证。验证所得 pass 只用于原 login key；更改邮箱或开启新登录意图不能复用旧 pass。当前会话进入普通登录页可返回应用；显式切换账号必须用户点击退出，失败/Unknown不假称已退出。

**邀请/reset 链接：** 在创建浏览器 history 前，从固定 `/invite` 或 `/reset-password` 的 fragment 捕获正式 token，先清 URL 再解析/请求，非法片段也立即移除。格式为 UUIDv7 + `.` + 43字符原始 Base64URL，共80字符；只放请求 body。token仅由页面私有 owner 持有，不进 props/公开 reactive state/history.state/storage/DOM属性；刷新后请用户重开原链接。清 URL 失败就停止提交。同标签新链接、hash/popstate和离页须隔离旧代；新链接不能覆盖 dirty/未决原意图。成功、明确放弃或离页释放敏感引用。有效Session打开邀请仍带真实Cookie，另一身份被拒绝时提供返回当前账号/显式切换并重开原链接，不自动logout绕过。邀请失效提示联系管理员；reset失效可返回找回页。

**字段规则：** 邮箱为≤254字节裸 ASCII 地址、不 trim；服务端规范小写。用户名3–32 ASCII字母/数字/中间连字符、服务端小写与唯一性/保留字校验；未改的现有 `admin` 不按新建保留字拒绝。显示名≤80 Unicode码点/320 UTF-8字节、无控制字符，允许空/空格、不 trim，空值显示邮箱。新密码15–128 Unicode码点且≤512 UTF-8字节，确认完全一致；不做正规化、组合字符规则或 UTF-16 maxlength 截断。当前密码按现值提交。服务端弱密码/保留名判断权威，`/password` 在reset/改密错误中映射新密码。

**资料/头像：** 用户名改变前提示“项目路径会变化，旧链接失效且没有别名或自动跳转”。字段保存、头像上传与移除各自独立，不伪装一个原子保存。选择文件仅本地预览，明确未上传；当前头像保持直到真实成功。支持静态JPEG/PNG/WebP，≤5MiB、单边≤4096且总像素≤16,777,216；服务端验证完整容器、拒动画/SVG/GIF并缩至512以内、去元数据重编码。读取图片有界验证 Content-Length/媒体/ETag并与当前Profile metadata匹配，构造Blob URL；变化/读取失败保留真实错误与重载入口，不能当无头像。顶栏与资料页共享当前身份已保存头像及读取状态；候选Blob只归资料页，取消/替换/离页/失效时释放。已保存Blob可跨页复用，被替换、确认移除、身份更换或失效时释放，迟到结果不能恢复旧头像。Unknown上传保留同一File原bytes供原请求恢复。

**外观：** 只有light/dark/system。选项立刻暂时预览，保存才PUT服务端；system才响应系统配色变化。取消/离开恢复当前同一身份最新已确认服务端值；匿名/失效回system。刷新清预览，重新读取服务端；本地主题缓存不证明账号持久化，不能跨用户继承。资料、头像、偏好共用User version，脏草稿保持产生时的版本；其他成功写不能暗中改其expectedVersion。无脏草稿才吸收新版本。

**改密：** 明确当前/新/确认三个字段与简短规则；字段失败保留本表单和其他页草稿。严格成功立即清三字段，说明其他会话已退出；会话确认失败显示“密码已修改，请重新确认登录状态/重新登录”，不降回改密失败。首次管理员初始密码建议是建议入口，不新增强制改密门禁。

## 6. 恢复渠道与公网依赖

`accepted:true` 只证明请求受理。存在/不存在的合法邮箱反馈一致；smtp显示“若该邮箱可用，将通过邮件提供恢复方式，请查看邮箱”，backend_log显示“恢复请求已受理；当前由有权限的管理员通过受限后端日志协助提供恢复方式”。不显示 token、完整链接、存在性、job ID、内部投递错误，也不声称已发出或已送达。SMTP配置后发送失败不回退日志；无SMTP时的受限恢复日志是现有真实渠道，不能冒充外部邮件服务。受控SMTP sink只能证明受控投递，真实提供方与公开找回可用性由后端/环境owner据实报告；未获另行授权不向真实用户/外部邮箱发送邮件。

公网真实登录必须HTTPS、固定规范PublicOrigin与Secure/HttpOnly/SameSite Cookie，不为HTTP18081开启DevMode、去Secure或放宽Origin/Host。用户最新选择由本地独立服务5254终止TLS，frp以TCP直透公网18443；固定PublicOrigin为 `https://154.12.86.233:18443`，证书含该IP的SAN。该IP已由DNS和域名/IP的18081同页响应校验确认。此方案不要求frps配置HTTPS/证书或用户修改Nginx，现有443应用与18081预览保持。临时自签证书会产生浏览器不信任提示，用户已知；不能将这种临时证书表述为公开CA信任或通过关闭产品TLS校验掩盖。公开实例与5250本地测试服务隔离，启动及公网验证状态见当前状态；完成入口响应归属、安全Cookie和真实登录旅程验证后才交地址。HTTP预览只保留静态框架，不传输真实账户材料。

## 7. 适用验收与完成出口

| 旅程/风险 | 必需可观察结果 |
| --- | --- |
| 登录与保护 | 真实匿名拒绝、登录与Session确认、刷新恢复、退出和受保护返回；普通用户无系统管理入口；失效清私有内容。挑战正常/失败/限流与键盘操作。 |
| 资料与主题 | 真实用户名/显示名保存和刷新，邮箱只读、冲突保草稿；light/dark/system预览/取消/服务端保存，换账号无旧预览。 |
| 头像 | 真实上传/替换/移除与读取后态；顶栏同步已保存头像，未设置时占位，候选预览不进入顶栏；坏类型/超限拒绝、读取失败可重载、身份隔离及Blob清理。 |
| 改密 | 当前密码错误与字段校验；新密码真实登录、旧密码拒绝、其他真实Session撤销；200后Session读取失败零重复改密。 |
| 邀请与找回 | 自有隔离真实管理员发起邀请→受限渠道→新页兑换→显式登录；已消费/撤销链接拒绝；合法邮箱存在/不存在同反馈；真实reset→全部旧Session失效→新密码登录。 |
| 所有权/Unknown | 受控迟到响应、跨身份、离页/取消、原key/body/context重放、确认后再读失败、旧401 Cookie副作用；模拟证据与真实PG/浏览器证据分开。 |
| 安全与部署 | CSRF/Origin/Host/权限负例、无敏感材料泄漏、无自动账号切换；公开HTTPS和安全Cookie确证后才接入真实账户。 |
| 完整页面 | 浅深主题、桌面/390px、键盘与焦点、200%缩放、reduced motion、长字段/错误和适用失败状态；整页截图实际观察，图片展示先上传图床。 |

必要正常/失败检查后尽早进入自有真实服务联调。正式完成须七页行为、适用工程检查、真实旅程与独立安全/视觉审查闭合，必要文档同步；HTTPS或真实SMTP未就绪则只标明对应公开能力未完成，不妨碍本地实现与检查。旧HTML、旧Vue或框架验收不代证本轮业务。
