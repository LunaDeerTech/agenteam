# D26 正式 Account 认证客户端与页面

修订：rev1.2，2026-10-05。状态：**rev1.2 待独立卡片差量复审，尚未业务实施；本轮只授权本文件设计，不授权业务实施或运行。** 设计者 `d08_registry_backend` 本轮承担 architecture_worker；实现及独立验收由主线程另派。统一固定源码基线为已接受 `457b1979c9d6563740543b2011eedc06cce34c71`，包含原 `422e0c1d97d924e503ad816992b778e917599507`、已验 Account OpenAPI 修正 `df78a9215d1a0dd22f8b8fb26a24e329130fb034` 及 System Model root 装配；本卡无迁移。

可行性依据：`/tmp/agenteam-d26-auth-feasibility-v-oqwi6632/report.md`，SHA-256 `923d56d639cbb335ce7a11a39b70bd755dc1dd15c65a3ace500b84a864fdf9e9`。该报告只做静态核对，不是 npm、浏览器或真实后端执行通过。活动 app/Model 装配及 Artifact 改动不进入本卡固定输入；后续消费新提交必须先核相关依赖差量。

rev1 独立静审为 **BLOCKED，仅 F1 前置契约矛盾**，原报告 `/tmp/agenteam-d26-auth-static-pz4ka2qc/review.md`，SHA-256 `8db17da803c93e6a9e9afebb67a68dfd9c086db63784127979891f888a214d3d`。F1 已单独纠正并通过独立复核：`/tmp/agenteam-account-pass-schema-v-j3_3gecx/review.md`，SHA-256 `87474952e73b48f111b48adcc1b0cd9e869a91920263c0eca76200dcfa06c536`；已验 `account.json` SHA-256 `583783ef80da1e6f93f84e7d5906dfabd3169e2d21f518df930f45d52409f797`。该修正只使两处 pass 长度与既有签发/消费行为一致，不改变认证行为或本卡 21+3 路径。

rev1.1 已独立 **STATIC PASS** 并获主线程采纳：`/tmp/agenteam-d26-auth-rev11-v-is_xe5w_/review.md`，SHA-256 `14a591cc38896e5c1256393fb281764297574de1e7a20741bd0d161e3c8f9746`。统一至 `457b197` 的依赖差量也已独立 **STATIC PASS**：`/tmp/agenteam-d26-root-dependency-v-betgak7_/review.md`，SHA-256 `f47c91544e9129b0f8bc0726986e016f8bdce3b3e73dd3652553bd4ffd774421`。两项均不代表前端业务或新基线浏览器组合已运行；本修订仅归位 §6 已验依赖，卡片差量仍待独立复审。

## 1. 完整结果与前置

交付正式 `web/` 中可连接真实 Account HTTP 的登录、rotate 挑战、刷新恢复 Session、当前 Session 注销，以及已确认的空首页和认证路由。生产构建必须经过自有同源服务器与真实 Account root/PG/Secret/Audit/Outbox 的浏览器组合验收；既有 CAPTCHA harness 只提供已锁依赖与测试算法，不能充当产品页面。

前端业务只消费 [D07 已验能力](d07-account-session-smtp.md#当前进度)：B04 `022dcea`、文档关闭 `0ed8085` 所归位的六项 HTTP 接口及当前固定实现。D07 原组合验收的历史限制沿原卡保留，不另称一次全绿。本卡不依赖 D25、Project 或 Model 页面/调用，也不授予 Project Owner 权限；真实后端启动包含 §6 已验 System Model 初始化。D26 后续负责邀请/恢复/改密/个人设置与主题保存、项目路径及双导航、Realtime/refetch；D27 负责相应业务页面；[D28](../development-plan.md#d28-集成部署与交付验收)负责生产资源托管和 History fallback。当前 Central 未托管 SPA，本卡不改 root，不将 `/readyz` 改绿，不宣布完整 D26 或部署完成。

产品依据为 [账号入口](../../frontend-design/layouts/account-entry.md)、[账号生命周期](../../architecture/platform-infrastructure/authentication/account-lifecycle.md)、[应用框架](../../frontend-design/layouts/application-shell.md)、[系统首页](../../frontend-design/layouts/system-pages.md)。首页仅“首页”标题及待设计的 Dashboard 容器，不增加统计、项目列表、Session 调试面板或虚构数据。初始密码仍为可继续使用的改密建议；本卡显示建议文字，不生成尚未实现的个人设置链接。找回、邀请、注册、搜索、Inbox、项目和设置入口由后继页面开放，本卡不放不可用链接或假导航，也不新增公开注册。

必读 [AGENTS](../../../AGENTS.md)、[团队流程](../agent-team/README.md)、[设计技能](../../../.agents/skills/agenteam-design/SKILL.md)、[Vue 开发技能](../../../.agents/skills/agenteam-vue-development/SKILL.md)、[Vue 测试技能](../../../.agents/skills/vue-testing-best-practices/SKILL.md)、[前端基础](../frontend/README.md)、[组件接口](../frontend/components.md)与[样式规范](../../frontend-design/styles/README.md)。实际浏览器实施另读环境可用的 Playwright 技能；技能/工具缺失须报告，不自行增加依赖代替。前端基础目前“认证未实现”等旧句不能推翻已验 D07，实施验收后按 §8 窄同步。

## 2. 六项 API 与客户端类型

唯一 wire 来源为统一 `457b197` 中的 [account.json](../../../api/openapi/account.json)、[common.json](../../../api/openapi/common.json)，并核同一基线 `account/http.go`、`http_auth.go`、`http_wire.go`、`csrf.go`；前者与已验 `df78a92` 字节相同，common 及 Account 实现与 `422e0c1` 无差量。本卡不改服务端 DTO、状态码、Cookie 或 CSRF 规则。

| 方法与同源路径 | 请求与权限材料 | 成功响应 |
| --- | --- | --- |
| GET `/api/v1/auth/bootstrap` | 无 body、无 query；尚未完成认证的浏览器上下文 | 200 `{csrf_token,challenge_modes:["rotate"],delivery_channel:"smtp"\|"backend_log"}`；浏览器接收匿名 HttpOnly Cookie |
| GET `/api/v1/session` | 浏览器当前 Session Cookie，无 body/query | 200 `{user,session,csrf_token}`；401 表示当前认证不可用，服务端按原规则清 Session Cookie |
| POST `/api/v1/sessions/login` | 匿名 CSRF；`Idempotency-Key`；`{email,password,challenge_pass?}` | 200 `{user,session,next_path:"/"}`，浏览器接收 Session Cookie；**没有 Session CSRF** |
| POST `/api/v1/sessions/logout` | 当前 Session CSRF；`Idempotency-Key`；`{}` | 204，无 JSON body；确认后服务端清 Session Cookie |
| POST `/api/v1/auth/challenges` | 匿名 CSRF；`{mode:"rotate",email,login_key}`；无独立命令 key | 201 `{id,mode:"rotate",master,thumb,expires_at}` |
| POST `/api/v1/auth/challenges/verify` | 匿名 CSRF；`{email,login_key,challenge_id,proof:{angle}}`；无独立命令 key | 200 `{pass}`；一次性材料，绑定 browser/email/login_key |

`client.ts` 仅提供本卡使用的同源 JSON 传输、取消及安全错误解析；`account.ts` 提供 `bootstrap`、`getSession`、`login`、`logout`、`createChallenge`、`verifyChallenge` 六个 typed 方法。每方法显式接收其所需 CSRF/key/input/AbortSignal，不将匿名与 Session token 藏进同一个可变默认 header。不得生成通用资源 CRUD、任意目标 URL、自动重试中间件或未实现 API 的占位方法。

客户端以 `unknown` 接收 JSON 后验证本次所需完整 shape，不以 `as SessionView` 掩盖畸形响应。类型与 wire 要求：

- `User`：`id,email,username,display_name,role,theme,version,initial_password_suggestion`；role=`admin|user`，theme=`system|light|dark`。Session 为 `id,issued_at,absolute_expires_at,idle_expires_at`。ID 沿 UUIDv7，小写标准形；version 保持正 int64 十进制**字符串**，不得转 number。Instant 保持 UTC 六位小数字符串，不用本地时钟代替服务端过期判定。
- 运行时解析区分 CSRF 与 challenge pass：匿名及 Session CSRF 均保持原 **43 字符**；`ChallengePass.pass` 响应与可选 `LoginInput.challenge_pass` 请求均沿已验 OpenAPI 的 **minLength=80、maxLength=80**。既有 pass 为 UUID36 + `.` + 32 随机字节的 canonical RawURL43，共 80 ASCII 字符，客户端将其作为 opaque 材料原样传递，不截断、解码重组或套用 CSRF43 限制；长度/shape 通过不证明其授权、活性或一次性消费成功，这些仍由服务端判断。challenge/图片其余字段沿 OpenAPI 限制。
- 只渲染服务端返回的 `data:image/png;base64,` 公共题图，校验单项和合计上界；不访问响应提供的任意远端图片 URL。angle 为 0–360 的整数，官方组件角度取整后与键盘控件使用同一值。
- `Idempotency-Key` 是 foundation 的 1–128 字符 key，**不是 UUIDv7 ID**；新意图用 `crypto.randomUUID()` 生成合法不可预测 key，不用 Math.random 或时间戳降级。熵不可用时显示本地错误，不发送写请求。
- `Problem` 保留 `type,title,status,detail,instance,code,request_id,field_errors?,retry_hint?,commit_state`，commit_state 闭集沿 common.json。逻辑按 `code` 和明确状态处理；字段错误只映射本页面已知 JSON Pointer，未知字段作为区域错误。安全服务端文字仅作文本，不用 HTML，不拼接请求值或 raw response。
- JSON/Content-Type/成功状态与 shape 不符、断网和 abort 是独立 `transport/invalid-response/cancelled` 失败，不能伪造一个 `Problem`、伪造 `not_committed` 或转换为空成功；已发出的 mutation 在这类失败后视为结果未确认。204 不尝试 JSON 解析。

所有请求为相对 `/api/v1/...`，`credentials:"same-origin"`、`cache:"no-store"`；POST 为 `application/json`，CSRF header 为 `X-CSRF-Token`。不读写 `document.cookie`，不设置 Cookie/Host/Origin/转发身份头，不接受 body Actor/role。拒绝跨源地址和重定向响应，不让 API 失败落到 SPA HTML。无自动 token refresh、后台轮询续期或登录重发；正常 GET Session 不算可信活动。

## 3. Session、CSRF 与迟到响应

`useSession.ts` 是本卡单一认证协调者，导出 readonly 安全用户/Session/阶段和显式动作。推荐阶段 `checking | anonymous | authenticating | authenticated | signing-out | uncertain | unavailable`；页面不能自行设置 authenticated。密码、pass、CSRF 和待重试 body 不包含在公开可序列化 Session 状态中。

| 触发 | 必须执行的状态转移 |
| --- | --- |
| 首次应用加载、真实刷新 | 先 GET Session；200 验证完整 view 后发布身份及 Session CSRF；401 `UNAUTHENTICATED/SESSION_REVOKED` 后才进入 anonymous 并取得匿名 bootstrap；其它失败保持 unavailable，不把网络失败当未登录 |
| 登录 200 | 保留“正在确认会话”，随后一次 GET Session；只有取得真实 Session CSRF、身份与登录结果匹配，才显示首页。登录响应本身不能拿匿名 CSRF 继续注销；后续 GET 失败不能把已发 Cookie 说成登录未提交 |
| 已登录路由重入、页面恢复可见/pageshow | 去重执行当前 Session 查询；失效后移除受保护内容并进入登录提示；依赖失败显示恢复入口，不继续展示旧用户内容。不为保活定时轮询，不发 `active=true` |
| 注销意图 | 单飞 POST logout；等待中禁重复提交且不显示退出成功；204 才显示确认退出、清当前前端身份/Session CSRF并进入登录。之后按需新 bootstrap |
| 当前 Session 401 | 只处理上述两个失效 code；清本地身份和 token、递增代际，进入登录提示。login 的普通 401 和 `CHALLENGE_REQUIRED` 不适用全局失效处理 |
| 已知主题 | 经当前 Session 确认后调用现有 useTheme 应用服务端值；未认证恢复 system。无主题编辑/保存、本地持久化，不把 Debug 的主题选择当账号偏好 |

每次身份意图更换、退出、作废上下文都增加单调 generation。Session/bootstrap/login/challenge/verify 每个请求捕获 generation 与本次请求身份，完成及 finally 均先核对；过期结果不覆盖用户、token、题图、pass、按钮状态或导航，也不能解锁更新一代请求的 busy。不能只依赖 AbortController：测试必须包含忽略 abort 后仍回包的 transport。

**Cookie 同样是状态。** Bootstrap、Session GET（401 可清 Cookie）、login、logout 经同一 cookie 请求队列单飞，不允许上一代响应与新登录/退出并行写 Cookie。路由卸载先做逻辑取消，不以卸载为由丢掉队列中的真实请求责任；由协调者观察其 transport 结束，旧结果仍不发布。Cookie 请求不因页面卸载立即 abort 并释放队列。统一请求等待上界 30s；超时取消后只能记为未确认，不能声称服务端已取消/回滚，也不自动启动相反 mutation。非 Cookie 的题图/verify 请求可主动 abort；原请求及回调仍受 generation 防护。

开始新身份操作前须结束当前单飞请求；旧 login 成功已写 Cookie 但其页面意图作废时，只能重新读取当前 Session，不能把迟到结果当新身份直接入站。未知/失配身份不借已有响应恢复旧 UI。已确认当前 Session 与待确认 login 的 user/session 不匹配时显示身份已变化、要求重新检查，不把另一个账号当本次登录成功。跨页缓存不作为认证事实；不引跨标签页凭据广播或自造 cookie 同步协议。

## 4. 登录、挑战与 Unknown

登录表单使用原 UiField/UiInput/UiButton。邮箱使用 email/username 自动填充语义，密码为 `type=password`、`autocomplete=current-password`，不禁止粘贴；不 trim、normalize、截断密码，不用 HTML maxlength 静默截短 UTF-16 内容。只作已定输入反馈，最终认证与挑战由服务端判断；用户名不是登录标识。

一次登录意图私有保存 `{generation,browserGeneration,key,email,password,challengePass?}`。发请求使用冻结快照；不得在 await 期间从可编辑表单重新取值。提交中禁重复；未知结果期间禁改写原快照。普通明确凭据失败显示同一“邮箱或密码不正确”反馈，不揭示邮箱是否存在；失败保留当前表单，新的明确登录意图生成新 key，不能把旧失败 receipt 当新验证。

`CHALLENGE_REQUIRED` 是登录流程分支，不是普通密码错误。保留该 login key、邮箱和密码，取得并展示真实 rotate。创建/验证使用同一匿名 browser 与 login_key；服务端 pass 校验成功才可重交 login。原 `loginMAC` 绑定 browser/规范邮箱/密码，不把补入 pass 当新密码意义；不得因为出现挑战就自动更换 login key。非 Unknown 的错题/到期清题图/pass并允许用户请求新题，禁止自动循环或冷却倒计时。换邮箱、密码、新 key 或重新 bootstrap 必须作废旧题与 pass；取消/关闭挑战不等于已验证，焦点回到创建挑战/登录动作。服务端仍决定有效期，客户端倒计时只可作到期提示，不作准入依据。

下表的命令重试规则针对 login/logout。challenge create/verify 没有公开命令 key 或查证接口：未得到有效 pass 的失败/Unknown 一律不算验证成功，不自动重放 verify 或提交 login；用户可在同一仍有效的 browser/email/login key 上明确请求新题。旧题/pass 同时作废，不能用“检查当前会话”冒充挑战查证。

| 登录/注销的未确认或错误 | 客户端动作 |
| --- | --- |
| `COMMIT_UNKNOWN`、mutation 的 transport/畸形响应、`RESOURCE_BUSY` 等尚未收敛状态 | 显示结果未确认，保留原 key 和完整原输入；只提供显式“检查当前会话”或“重试原请求”，每次点击有界一次，不自动换 key/改密码/循环重发 |
| 同 key 重试 | 匿名 browser 仍相同才重交原 login；不得先 bootstrap 换 browser，也不得把旧 pass 配新 key。当前 HTTP 没有 LookupLogin/command-lookup 路由，不能虚构 `/commands/lookup`；`retry_hint=lookup` 不授权新接口 |
| GET Session 200 / 401 | 分别只证明当前可用身份 / 当前认证不可用；不证明原 login/logout 命令提交或负向终局。可据真实身份恢复页面，但不能把 401 当旧写失败而自动换 key再写 |
| logout Unknown | 保留原 logout key/所绑定的 Session/CSRF，只能在相同 Session 仍当前时显式原请求重试。若随后真实401，提示“当前会话不可用，请重新登录”，不伪称原注销命令已确认提交；网络错误时不能仅清缓存宣布撤销 |
| `CSRF_FAILED` | 不把匿名与 Session token 互换；当前上下文作废并提示重新建立。存在未知 mutation 时先保留未确认边界，不静默 bootstrap 后用新 browser 重放旧 key |
| `IDEMPOTENCY_KEY_REUSED` | 保留错误和原意图，不自动改 key 绕过；用户明确开始新意图时才重新输入和建 key |
| `ORIGIN_DENIED`、依赖失败、未知 Problem code | 安全区域错误及恢复入口，不泄漏后端配置/请求体，不显示登录或注销成功 |

密码/pass/CSRF/key/命令 body 仅在必要内存及实际 fetch body 内存在，不写 localStorage、sessionStorage、IndexedDB、URL/history state、日志、错误对象、分析事件或 Service Worker 缓存。成功、明确放弃、路由离开或上下文作废时清表单和不再需要的引用；仍未结束的请求仅由私有协调者保存其必要快照，结束后清理。JS 字符串和浏览器临时副本不能承诺物理擦除。刷新后不尝试恢复已丢失的密码/key/pass；从真实 GET Session 重新开始，仍不声称旧未知事务回滚。

## 5. 页面、路由与可访问性

- 正式 `/login` 为独立居中认证面板，不含已登录 AppShell/管理员菜单；`/` 是唯一受保护的已实现目标。加载/失败使用 UiState/Spinner 和明确恢复操作，不闪现旧账号内容。已认证访问 `/login` 经当前 Session 检查回 `/`。
- return target 只接受已注册、允许返回且重新检查身份的目标；本卡集合只有 `/`，不支持任意 URL、协议相对地址、项目文字路径或 hash。非法目标不用原值构造链接；未知业务路径仍 NotFound，不能登录后换到另一个项目。登录 wire 的 next_path 只接受 `/`。
- AppShell 仅窄透传现有 SystemNav slot。首页保留 Logo → `/`，右侧显示当前显示名（空则邮箱）和真实“退出登录”按钮；显示名暂为文字，不伪装个人设置链接。无管理员/项目/搜索/Inbox 假入口。Debug 仍仅 DEV 的独立开发路由，不请求产品认证、不作为登录默认落点；生产资源继续排除 Debug。
- 使用现有 tokens 和字体密度；页面局部布局不得复制共享控件样式。无 eyebrow、底部成功 toast 或假定时成功。错误就近持久显示并以 alert/status 宣读；输入标签和 aria-describedby/invalid 正确，点击/Enter 走同一提交防重入路径。
- 官方 GoCaptcha Rotate 2.0.7 在本卡包装中使用真实图片与真实 verify；补充同题、同 angle 的原生 range + 验证按钮，支持方向键/Home/End/Enter。官方拖拽和键盘结果都须实际提交 `proof.angle`，不能只更新本地“验证成功”。视觉对齐仍是限制，不声称已经提供非视觉替代挑战。
- 挑战展开聚焦其说明/键盘控件，验证成功返回登录按钮，关闭/失效回创建挑战按钮，路由切换聚焦新页面标题/主内容。若使用浮层只复用已有焦点管理，不能使用无键盘语义的自造 modal；允许直接内联题图。
- 1440/1024/834/390px 浅深主题验收，长邮箱/显示名、200% 缩放不产生页面级横向溢出；窄屏单列，操作可达。reduced motion 不延迟请求，第三方旋转 UI 的辅助动效也应在包装内响应，题目角度功能不被取消。只读应用服务端 theme；登录未认证态按 system。

## 6. 同源运行、依赖及固定输入

实现及真实后端构建统一从已接受 `457b197` 固定快照取得完整闭包，再覆盖本卡授权候选，不从活动主树拼接源。相对原 `422e0c1`，生产差量只有已验 `account.json` 纠正及 `internal/central/app/account.go`、`security.go`、`model.go` 三处装配：Audit 加入 Model Authority、Outbox 加入 Model producer/events、Secret UsageRouter 的非 Model 用途仍转交原 Account Authority，六个完整 System Model 路由前缀不接管本卡六认证路径。root8 的另五文件为 `internal/central/app/model_test.go`、`model_process_test.go`、`tests/process/model_system_database_test.go` 及 `AGENTS.md`、后端 README；中间 structured-wire 新卡仅为规格文档，不成为本卡页面或 Provider 调用前置。Account handler/CSRF、common、config、迁移、Go 锁、scripts/testsupport、旧 Account fixture 与 web/旧 CAPTCHA 锁均无差量，21+3 白名单不增加写权。

新浏览器 fixture 必须真实经过该完整 root：保留既有全迁移（含 00015/00016）与 accountenv/PG/MinIO、独立 keyring/recovery log；Secret Initialize 成功且 ctx 未取消后，同一 ctx、同一原 30s SecurityStartupTimeout 内执行 Model Initialize，再等待真实 listener。后者检查既有 Model 存储并幂等建立 `configured=false` 的技术 singleton，不选择默认 Model、不请求 Provider，无新增环境变量或 Provider 凭据。不得跳过初始化、换局部 Account 根或扩启动/浏览器预算；旧基线通过记录不证明新组合已验，也不表示已知 Object 退出缺陷已修。

保持 `web/package.json` Node 范围 `^22.18.0 || >=24.12.0` 和现有 Vue/Vite/TypeScript/测试依赖锁定值；唯一正式依赖增量为精确 `go-captcha-vue: "2.0.7"` 及必要锁闭包。不得顺带升级、引入 Pinia/Axios/MSW/Testing Library 或另建全局客户端框架。Playwright 复用 `tests/account-captcha-web` 已锁 `@playwright/test 1.56.1`，新增专属 config/spec；不改其原 harness、lock、旧测试或将其 src 导入 web。只复用只读的 public-solver/drag-geometry 测试算法。

开发代理在 `web/vite.config.ts` 中仅代理 `/api/v1`，目标从显式 `AGENTEAM_DEV_API_TARGET` 读取且只允许开发者自有 loopback HTTP 端口；无默认远端目标、无客户端构建注入秘密。未配置时保持纯前端开发并明确 API 不可用，不造成功响应。Vite 仍只绑定 127.0.0.1:5173/strictPort。代理保留浏览器 Host/Origin（不 `changeOrigin:true`），真实 Central 的 PUBLIC_ORIGIN 必须等于浏览器外部 origin；不添加 CORS、Cookie rewrite、CSRF 豁免或信任 X-Forwarded-*。

真实生产构建验收由新 Go 测试 fixture 托管 `web/dist`，只将 `/api/v1` 反代至固定真实 `app.Run`；浏览器外部监听使用自有 loopback 随机端口，先确定 PUBLIC_ORIGIN 再启动真实 root。API 保持原状态/headers/body；资产缺失/API 404/服务端错误不能变成 index.html。非 API 的 History 页面才可 fallback。该服务器仅属于测试，正式生产静态托管保持 D28 责任；`vite preview` 不是生产认证部署证明。

新 fixture 可复用已验 `accountenv`、PG/MinIO 与只读 helper，但不能消费共享主树活动 app 文件、既有端口/数据库或真实外部凭据。需要 Account 设置/主题/注销等前置尽量走正式 HTTP；过期场景允许 fixture 在自有 PG 精确定位已确认 Session 的期限后态，记录所改行和原事实，不能伪造 Session/Cookie/CSRF 或修改认证逻辑。浏览器测试与 Go fixture 的控制只用自有进程私有 IPC/0700 临时文件交换安全 ID，不新增产品或 `/fixture/*` API，不读取挑战答案。初始密码只来自本轮私有 recovery log，传递不进普通日志、截图、trace 或证据材料。

## 7. 精确实施白名单

以下为待主线程授予实现者的 **21 个代码/测试/配置路径**；本轮设计没有这些写权。新增 helper 只能放在列出的文件内，缺文件权限先报真实必要性，不自行增加第22个实现路径。

| 路径 | 类型与唯一职责 |
| --- | --- |
| `web/src/api/client.ts` | 新；本卡同源传输/Problem/取消边界 |
| `web/src/api/account.ts` | 新；六接口 DTO、运行时解析与调用 |
| `web/src/composables/useSession.ts` | 新；单一协调、CSRF/代际/单飞与 Unknown |
| `web/src/components/account/RotateChallenge.vue` | 新；官方 Rotate 包装、键盘/焦点 |
| `web/src/views/auth/LoginView.vue` | 新；正式表单/挑战/安全反馈 |
| `web/src/views/HomeView.vue` | 新；已确认的空首页、初始密码建议 |
| `web/src/router/auth.ts` | 新；认证 guard/有限返回目标 |
| `web/src/App.vue` | 旧窄改；按认证布局渲染、当前身份与退出动作 |
| `web/src/router/index.ts` | 旧窄改；登录/首页、DEV Debug 与 NotFound 边界 |
| `web/src/components/layout/AppShell.vue` | 旧窄改；SystemNav slot 透传，不新造导航 |
| `web/vite.config.ts` | 旧窄改；显式开发同源代理，保留既有测试配置 |
| `web/package.json` | 旧窄改；GoCaptcha 精确依赖；原 Node/脚本行为保持 |
| `web/package-lock.json` | 旧窄改；仅上述必要依赖闭包，唯一写者 |
| `web/src/tests/account-client.spec.ts` | 新；六 DTO/安全 Problem、CSRF 与完整请求 |
| `web/src/tests/session.spec.ts` | 新；状态/Unknown/同 key/取消/代际竞争 |
| `web/src/tests/authentication.spec.ts` | 新；页面/真实路由组合与可见反馈 |
| `web/src/tests/rotate-challenge.spec.ts` | 新；组件 angle/焦点/题目作废 |
| `tests/account-captcha-web/authentication.config.js` | 新；专属正式产品浏览器 selector/环境，不运行旧 harness |
| `tests/account-captcha-web/e2e/authentication.spec.ts` | 新；正式生产构建浏览器场景 |
| `tests/account/authentication_web_fixture_test.go` | 新；真实 root、同源 dist/反代、自有资源/私有控制 |
| `tests/account/authentication_web_test.go` | 新；四组真实浏览器顶层与 DB/HTTP 后态证据 |

验后文档仅 `docs/development/frontend/README.md`、`docs/development/frontend/verification.md` 和本卡三个路径，由主线程另交唯一写者；前者同步实际 API/路由/代理/hosting 边界，后者追加本轮证据、保留旧验证历史，本卡只更新实际阶段/验收定位。主线程持有 tasks/development-plan/AGENTS 更新权，不由业务作者一并修改。总候选范围24路径不是本轮已授权写入范围；当前只有本文件。

共享 UI、SystemNav、useTheme、tokens、旧 tests、所有 Go 生产/Account API/OpenAPI、迁移、scripts、Go module、旧 CAPTCHA package/lock/harness 均只读。无 backend 旁路、Debug 数据导入、跨领域 SQL 或未来菜单。若新已验 app 提交改变后端根，只能由主线程采纳精确 delta 后纳入验收输入，不能拿未验活动树解释结果。

## 8. 自测、独立验收与交付

先完成 meaningful Vitest/类型/格式/生产构建，随后主线程交接唯一浏览器/Go/PG/MinIO 窗口。设计轮没有运行这些命令，当前 Node/浏览器/缓存是否可用仍需实施者实际核对；不凭历史 Chromium 151 或旧下载403记录假定环境就绪，不自动下载/升级。

| 检查 | 必须闭合的行为 |
| --- | --- |
| typed client | 六 API exact method/path/body/status，204无解析；匿名和 Session CSRF43不混用；同一代表性合成80字符 pass 在响应解析及登录请求校验/原样传递两侧通过，旧误shape43、79、81两侧均拒绝，不截断或生成替代材料；Problem/畸形响应/网络/取消分别处理；精确大 version、不显示 raw body/secret |
| session/路由纯测试 | 首次401与依赖失败区分、login200后GET、logout未确认、Unknown同key同输入、CSRF失效不偷偷换browser；晚到GET/login/bootstrap/verify及finally均不能污染新代；Cookie请求串行；错误返回目标/重入/卸载；不凭401证明旧命令未提交 |
| 登录/rotate纯交互 | 错密码与ChallengeRequired分流；换输入/刷新题/过期使旧pass失效；双击/Enter单飞，官方与range同angle；密码首尾空格/Unicode原样、敏感值不持久化；焦点和辅助说明 |
| `TestAccountAuthenticationWebSessionLifecycle` | 浏览器打开正式构建：真实登录→Session CSRF确认→空首页→刷新恢复→当前Session退出→受保护页再次拒绝；真实User/Session/Audit后态，初始密码建议非强制 |
| `TestAccountAuthenticationWebRotateChallenge` | 真实错误密码达到正式阈值→服务端要求挑战→公共题图求解→官方拖拽/键盘两分支→真实verify的80字符pass经正式typed client解析并原样送同key登录、DB一次性consume；不通过私有答案或本地success绕过，不记录pass原值 |
| `TestAccountAuthenticationWebRevocationAndExpiry` | 正式服务端撤销与自有Session期限后态，两分支均经真实GET和路由使旧内容消失；保留Cookie/CSRF安全验证；旧请求不能恢复已退出身份 |
| `TestAccountAuthenticationWebLayoutsAndProduction` | 正式构建浅深、1440/1024/834/390、200%缩放、键盘焦点/reduced motion/overflow、服务端theme读取；生产无Debug源码/路由，`/debug`为NotFound；API及缺失资产不被fallback |

纯异常可以用可控 fetch promise/正常失败结果模拟，但不能标成真实登录、服务端 Unknown 或真实 Cookie 竞争通过；至少有一个晚到 transport 忽略取消的确定 barrier。未知写结果的真实网络/COMMIT 注入不属于本卡，不恢复暂停的方法。浏览器真实组不使用 route.fulfill、假 Session、假权限或 `/fixture` 业务响应；公共题图 solver 只属测试，不打包到产品。当前六接口之外的真实业务错误模拟须先说明必要性，不改旧后端断言/预算。

工程命令为原 `npm run check --prefix web`（format:check、Vitest、type-check、build），只格式化授权文件；锁更新后用 locked install、记录 Node/npm 版本和必要增量，保留旧锁中未变包。新 Go fixture 用固定 Go1.27.1/offline readonly 做 `-tags=integration -race -run '^$' ./tests/account` 编译及适用 vet；生产未变不机械重跑无关两 cmd/整套后端。

实际浏览器沿原 `scripts/test-objects.sh -run '<冻结 selector>'` 运行新增 Account Go 顶层；原 `-race -count=1 -timeout=6m` 与 verbose 保持。四组按前两/后两分批，单个 Go 顶层沿既有浏览器模式最多2分钟，独立子例共用该预算；Playwright workers=1、retries=0、单例45s，失败先保留输入和日志，不自动重跑或扩预算。专属 config 使用锁定安装的本地 Playwright 二进制、明确自有 BASE_URL/CASE/Chromium路径，无自动安装、既有 server reuse 或端口漂移。截图仅取已清空敏感表单的布局场景；trace/video/请求body日志关闭，凭据不进入故障附件。

每次 fixture 结束必须实际确认自有 nonce+label+exactID 资源两遍 absent、原基线不变、子进程已 wait、runtime/私有凭据文件清理，再交还窗口；不能只声明all-stop。作者冻结基线、21源码/锁/必要依赖 hash、真实 argv/env/exit、原红/修复与场景映射后停写；不可把含凭据 env 原值归档，保留变量名和安全脱敏说明。独立验收者优先补 Session/CSRF及迟到结果、真实挑战/注销、构建/同源边界，按固定有效证据去重，不以纯页面mock代替正式浏览器。

本卡规格自查只证明来源、边界、链接和格式；环境准备仅查询 Node/npm 版本、浏览器安装文件和锁定缓存，未进行业务、安装、构建、浏览器、Go/Docker 或网络执行。独立规格审查及主线程采纳后才下发业务实现。产品规则变化、必须新增API/依赖/第22实现路径、Cookie安全或生产hosting范围变化，先向主线程提供事实与最小必要修订。
