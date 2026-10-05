# 公开账号链接与密码恢复：只读可行性

状态：仅架构分析，未形成正式规格或实施授权。固定后端及已验认证源码为 `9a710f272026b41ef69852bbeb41cb7670b500a8`；个人设置只消费已采纳技术卡 `e2ec65d4bb2bf220b67efb7a88d76bf4b2ceb901:docs/development/work-items/d26-personal-settings.md`，未读取其活动实现。未运行 Go/npm/browser/Docker/SQL/网络；未改仓库。

## 结论与完整边界

推荐合为一张“邀请链接兑换与密码恢复公开入口”卡：真实邀请链接→校验→填写账号→兑换→回到登录；找回入口→统一申请反馈→邮件/受限日志链接→校验→重置→回到登录。它们共用匿名 Browser Cookie/CSRF、一次性链接、原请求重试与同一个 Account Cookie 请求所有者，拆开会重复改同一身份协调器。无需管理员发邀请 UI、Project、D25、Summary、Model、暂停的 Object 修复；不新增后端端口、schema、依赖包或迁移。

当前硬前置是个人设置最终验收提交与相应源码接缝独立差量核对：其 24 路径正在变更同一 Cookie owner、稳定身份 epoch、App 草稿生命周期、safeReturnTarget 与真实 web fixture。认证 `9a710f2` 已验，但不能在其旧导出上直接授权后继、覆盖设置实现或把未见的个人门面视为实际可用。现在可拟规格，代码须等该前置关闭。正式 SPA 托管仍是后继根/部署责任；本结果可沿已验生产 dist + 任务自有同源服务 + 真实 Account app 验收。

## 已有实际端口与产品规则

下表路径都带 `/api/v1`，都是 POST、`credentials: same-origin`，严格 Origin/Host/Fetch-Site 与 JSON 边界。五条都使用匿名 Browser Cookie + `X-CSRF-Token`，不能用 Session CSRF 代替；两个 inspect 不要求幂等 key，三个写要求原 `Idempotency-Key`。

| 路径 | 请求 | 严格成功事实 |
| --- | --- | --- |
| `/invitations/inspect` | `{token}` | 200：`email, expires_at`；只读固定邮箱 |
| `/invitations/redeem` | `{token, username, display_name?, password, confirmation}` | 201：`completed:true, login_required:true`；无自动登录 |
| `/password-resets/request` | `{email}` | 202：`accepted:true, delivery_channel`；不证明账号存在或邮件送达 |
| `/password-resets/inspect` | `{token}` | 200：`valid:true, expires_at`；不返回邮箱或身份 |
| `/password-resets/complete` | `{token, new_password, confirmation}` | 204：改密并撤销目标用户全部 Session；不换发登录 Session |

来源：固定 `internal/central/account/{http.go,http_auth.go,csrf.go,http_wire.go}`、`api/openapi/account.json`。`http.go` 注册处明示 browser authority；`csrfBrowser` 独立核匿名 cookie。邀请 inspect/redeem 还在 Cookie 含 Session 时真实核当前 Session：另一身份返回 FORBIDDEN，失效 Session 可清 cookie；不能匿名绕过其当前身份检查。reset complete 不自动清浏览器里的其它用户 Cookie，更不自动把浏览器绑定到重置目标。

固定产品来源 `docs/frontend-design/layouts/account-entry.md`、`docs/architecture/platform-infrastructure/authentication/account-lifecycle.md`、D07 工程规格 §HTTP/恢复：三个页面均独立居中，不显示登录后主导航。没有公开注册；邀请先校验，邮箱只读，用户名/显示名/密码确认按真实 D07 规则；成功返回登录。找回对存在/不存在邮箱使用同一反馈，重置仅有效链接可提交，成功返回登录。密码精确保留 Unicode/空格，15–128 码点且 ≤512 UTF-8 bytes，不 trim/规范化/截断；使用已有控件、自动填充语义与可访问反馈。

正式链接固定 `/invite#<id>.<token>` 和 `/reset-password#<id>.<token>`。邀请 24h，重置默认30min；同一有效链接重发不延期。SMTP 未配置才提示由有权限的管理员从受限后端日志获取并转交链接；配置后的投递失败仍是 smtp，不回退普通日志。202 不含 job ID、投递失败原因或账号存在性。前端不读取日志、不展示原 token，不建立邮件轮询/诊断接口。

## 必须在下一规格闭合的工程接缝

1. **一个真实 Cookie owner，两个私有 CSRF 上下文。** `useSession.ts` 固定 `run` 是私有 void/no-op 所有者，不是可冒充业务 DTO 的公共队列；`publishSession` 清匿名上下文、`bootstrap` 创建新 BrowserID，而匿名上下文真实有效期1h。新公开门面必须纳入个人设置验后同一个 typed/actual-tail 协调器，在持有合法 Session 时也能取得并保留匿名 CSRF，不自动登出、不隐藏 Session Cookie，不建第二身份 store/队列。30s 可见取消不提前放掉真实 fetch/body 尾部；忙拒绝发生在生成 key/启动请求之前。
2. **先清 URL，再路由与网络。** 只对固定两种链接路径捕获初次 fragment，立即 `replaceState` 去除，包括无效 fragment；token 只进私有内存，再放 POST body。不能写入 query、router params、history state、storage、日志或测试 trace。固定 `main.ts` 静态导入 router，故单在 mount 前写代码仍可能晚于 `createWebHistory`；可在 `router/index.ts` 建 history 前调用窄链接捕获 helper。刷新后不能恢复已清除的材料，应要求重新打开原链接；不得造 token 查询 API。
3. **Unknown 与已消耗链接分开。** 三个写的私有意图绑定原 BrowserID、key、token/邮箱、完整值；显式重试只用原身份原请求。`link_commands.go` 与 `reset_complete.go` 都先查原 receipt 再校验活链接，故提交后 inspect 的410不能否定原成功，也不能作为重放的前置门槛。HTTP 没有 Account command lookup；`retry_hint=lookup` 不授权新增端口。服务内部可能已确认后直接成功；仅实际仍 Unknown/传输未知保留 uncertain。不能重 bootstrap、换 key、自动重复邮件请求，或拿 GET Session/试登录代替原写结果确认。用户显式放弃或上下文过期后不能声称未提交。
4. **公开路由及身份导航。** 建议工程路径 `/invite`、`/forgot-password`、`/reset-password`；前两种 capability 路径以正式后端链接为准，forgot 名是待卡冻结的工程选取。继续只接受认证/个人设置已定 `/` 和三个 settings 叶子作为 return 目标；不得把 fragment、任意 URL 或公开表单路径当登录后 return。当前 login guard 遇 authenticated 自动回首页，App 的可见性恢复也会调用 restore，下一卡必须精确处理这些已知接缝：公开页不得被自动保护页重定向/旋转匿名上下文；成功清密码并走显式登录入口，不自动登录、自动登出或改绑已存在的另一 Session。重置目标不同于当前身份时，后续保护页访问仍以真实 Session 复核为准；不可假定204已删除所有浏览器 Cookie。无需新产品规则，但要在规格明确 route/controller 的工程状态矩阵。
5. **真实 fixture 足够，但要消费最终 seam。** `authentication_web_fixture_test.go` 已有真实 app.Run、PG/MinIO、生产 dist、同源 history fallback、私有目录/恢复日志及退出清理；原局部 `old.record` 由个人设置卡授权保留私有接缝。后继同包 helper 可真实管理员 HTTP 创建邀请，按实际 `f.origin` 提取自有受限日志链接，再让真实浏览器兑换/申请/重置。原 `httpFixture.invite` 写死 localhost:8080，不能直接用于随机 origin；不能复制整套 root、直接改表造用户/Session/链接，或用 UI stub 模拟成功。受限日志材料只留运行时0700/0600文件，不归档秘密。SMTP 与普通日志隔离可复用固定 D07 已验事实，新增页面实际值/分支仍须测。

## 建议最小文件草案

这是待个人设置最终源码审计收敛的草案，不是对当前24路径扩权。生产主体为现有 `web/src/api/client.ts`、`api/account.ts`、`composables/useSession.ts`、`router/auth.ts`、`router/index.ts`、`views/auth/LoginView.vue` 六条；新增 `composables/useAccountEntry.ts`、`router/account-link.ts` 与 `views/auth/{InvitationView,ForgotPasswordView,ResetPasswordView}.vue` 五条。前者只管理页面 draft/私有链接状态，真实请求所有权仍在 useSession；若最终 App 的 visibility/身份 owner 确需分流，精确追加既有 `web/src/App.vue`，不先造另一入口。预计11–12生产路径，无 shared UI、theme、backend生产或包锁变更。

测试最小闭包：新增 `web/src/tests/account-entry-{client,state}.spec.ts`、`account-entry.spec.ts`；新增 `tests/account-captcha-web/account-entry.config.js`、`e2e/account-entry.spec.ts`；新增 `tests/account/account_entry_web_fixture_test.go`、`account_entry_web_test.go`。旧 `session.spec.ts`、`authentication.spec.ts` 只为 AccountAPI 完整 mock/新增可用找回链接与 guard 兼容窄改；个人设置新增 mock 若被 ReturnType 扩展影响，最终审计后逐文件列出，不能 optional 空成功。已有 web fixture 的私有 record 接缝若个人设置最终实现已足够则只读；如不足，先向 root 提一条窄路径。预计20–22条基础路径，最终准确数依已验设置实现决定，不承诺虚假的固定数。

## 有意义的验收与保留范围

- 真实邀请邮件/受限日志 URL 进入生产页面，URL/hash/history 不保留材料；inspect 固定邮箱，兑换后无自动 Session，旧链接不能新 key 再兑换；经明确登录可访问真实个人账号。真实另一身份现存 Session 必须被后端拒绝，不以预先清 cookie 制造假匿名正例。
- 已存在/不存在邮箱同反馈与响应形状；真实 request 经 Outbox/日志生成 reset link，重置后旧密码失败、新密码经正常登录成功，目标旧 Session 确实失效；UI不能泄露存在性/渠道内部错误。配置SMTP失败不解释成受限日志回退。
- 纯受控 transport 覆盖原 key/完整材料重试、已消耗 inspect 不否定 receipt、上下文过期、坏响应、取消后的实际 owner、双击/跨页、Session/匿名相互影响与迟到响应；不得以纯例声称 PG 已 Unknown。真实完整链覆盖 CSRF/当前 Session 与正式幂等正常重放，不增网络故障设施。
- 三公开页 desktop/窄屏、键盘/焦点、深浅主题、密码 Unicode/空格与错误归属、失效/撤销链接、dirty/离页敏感清理、无 token/password 日志/trace；沿同一生产 dist 跑必要认证/个人设置回归。不开管理员 UI，不改权限、邮件或业务生命周期。

以上为源码与正式规则的静态可行性，未声称新页面、真实组合或当前设置最终验收已通过。建议 root 接受此完整结果方向后，再授权唯一候选卡并在个人设置最终提交上做接口/所有权差量冻结。
