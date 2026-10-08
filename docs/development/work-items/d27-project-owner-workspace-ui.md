# D27 Project Owner 项目入口、工作区与基本信息编辑 UI

工程修订 rev2.1（2026-10-08，独立窄 STATIC PASS，root 已采纳）：仅追加 §8 第24个既有测试文件，修复既定 `/projects` 导航新增后的旧期待；当前24路径＝23技术＋1文档，新15/旧9。原 rev2 页首、§10 与附录A的23路径/65来源作为历史基线保留，不重写原指纹。产品合同、路由和UI范围不变；#24实施仍由root另授，不表示产品或真实资源接受。

归档状态（2026-10-08）：rev1 全文＋rev2 B1＋正式末件完整 STATIC 已接受，规格 `8cf81b44` 已推送并核远端一致；[原始审查与可逆修订](../agent-team/project-owner-workspace-ui-spec-verification.md)已归档。仅规格接受；当前另授纯 web/JS 实施与独立准备，产品及真实浏览器/资源未接受。以下原状态保留其正式归位时点，§1 起技术原字节不变。

状态：rev2 完整限定独立 STATIC PASS，root 已采纳；本次正式归位待独立末件窄核。仅规格接受，尚未授权产品实施、Go/浏览器执行或真实资源。固定接受产品 `cc850b2244cad771eb862a2c82d99887d5da7284`、配置归档 `b91cb89f0575cf7f54faa5d253b5571d7435b670`；固定来源见附录 A，23 路径白名单见 §8。不读取 Audit 活动实现，不把当前工作树等同于该接受基线。

## 1. 完整结果、既定决定与前置

交付普通 Human Owner 能从系统“项目”入口查看自己的项目，以名称地址进入已重新授权的稳定 Project 工作区，在双导航下查看真实概要、编辑名称和描述，并在结果不确定时查证、显式重放原 Update 意图。管理员同样必须是该 Project Owner，无身份豁免。页面行为依据[系统页面](../../frontend-design/layouts/system-pages.md)、[项目工作台](../../frontend-design/layouts/project-workspace.md)、[项目设置](../../frontend-design/layouts/project-settings.md)及[通用设置](../../frontend-design/layouts/settings-shell.md)，不另开产品决定。

本卡只绑定已接受的 Owner List/Get、独立 Resolve、Update 及其 lookup。来源分别为 [Owner Read 验收](../agent-team/project-owner-read-http-verification.md)、[Owner Update 验收](../agent-team/project-owner-update-http-verification.md)、[Resolve 所属 Usage 卡](d09-project-usage-read-http.md)，实际 schema 为 [project-owner.json](../../../api/openapi/project-owner.json) 与 [project-usage.json](../../../api/openapi/project-usage.json)。这些子能力已接受，不等于 D08–D28 全模块完成。

不做 Project 创建、归档、恢复、删除、Owner 转移，也不增加会议、任务、知识库、Agent、Skills、Model/Provider/Credential、MCP、变量、Scheduler、审计或 Usage 页面。设置只呈现本卡实际绑定的“基本信息”，不放死链接、假统计、未绑定功能占位页。系统管理员统一配置会议 Summary initial/update（含首轮标题）的决定保持；不增加 Project override、复制默认值或第二选择器。production Resolution/Invocations、D24、ready503、Object runtime join/OpenAI tools 独立验证停止均保持。

Audit 有自己的活动生产输入，包括 `app/account.go`；本卡纯前端不依赖它。实际 `tests/account` 编译/运行会传递依赖 app，必须等 Audit 完整接受并正式交接实际闭包，或由 root 明确授权使用固定 cc850b22 已接受源的最小 overlay。不得读活动 app/Audit 源来冻结或把未接受实现带入测试。本卡不写 app 三根、后端生产、schema、迁移、依赖锁或共享脚本。最终集成前对实际接受基线最小重绑，不回退 Audit。

## 2. SPA 停止项与可验交付边界

停止原件是[接续记录 §26](../agent-team/recovery-2026-10-06-continuation.md)：concurrent-publication 探针被平台内容安全机制终止，run 目录不存在、没有实际执行证据；stage02 竞态静态缺口未关闭，脚本返修与发布暂停，禁止重试、改写或转交该任务。本卡不以新名称重启该探针。

[D28 SPA 卡](d28-central-spa-hosting.md)的停止链包括 `scripts/build-central-web.mjs` 及其测试、`internal/central/webassets/` 的 handler/bundle/build-tag/共享 bundle 文件、`internal/central/app/app.go`/webassets 两源、`tests/process/central_web_test.go`、central-spa Playwright config/spec 及 `docs/development/backend/frontend-hosting.md`。本卡 24 写路径与该 16 路径无交集；也不执行 `agenteam_web` tagged build、共享 embed 生成、`dist/agenteam` 发布、staging/锁/原子发布或并发发布探针。前端源码是未来发布的上游输入，不代表停止缺陷被修复。

普通 `web/package.json` 的 Vue 类型检查、Vitest 和 Vite 构建可在未来授权的任务自有 outDir 完成。真实浏览器沿[已接受 Summary harness](../../../tests/account/system_meeting_summary_web_fixture_test.go)的方式：任务自有 loopback 同源入口提供固定静态 dist、以 reverse proxy 转发到真实 no-tag Central API，并为客户端路由提供 harness index fallback。此结果只证明浏览器产品、正式 API 与当前 harness 的集成；不是 Central 生产 SPA 托管、生产直链 fallback、安装部署或发布接受。Vite 代理不能代替正式 API。

旧浏览器用例若硬编码 `web/dist`，只能在 root 明确给出唯一资产窗口后，按已接受方式冻结旧资产清单、记录交换、actual reader 全部退出后恢复原字节并复核；不能并发构建/覆盖、改变发布脚本或把它叫生产发布。若某验收要求实际依赖被停止的发布链，则该项停报 root，不能换 harness 后宣称它通过。

## 3. 路由、入口与稳定身份

### 3.1 封闭路由与登录回跳

新增且仅新增以下受保护页面路由。系统导航“项目”使用 `/projects`，顺序在现有“系统设置”之前；原 `/`、个人设置、System、登录与公开账号入口保持。

| 路由 | 页面 |
| --- | --- |
| `/projects` | Owner 项目分页列表 |
| `/:username/:project_name` | Project 工作区首页 |
| `/:username/:project_name/settings` | 仅导向同 Project 的 `/settings/general` |
| `/:username/:project_name/settings/general` | 基本信息 |

`router/auth.ts` 的 `safeReturnTarget` 保留原静态闭集，增加 `/projects` 和上述 Project 三种路径的严格动态分支。只接受单个字符串、同源绝对 path；只接受合法 ASCII username/project_name，输出小写标准地址。允许合法大写原路径按同一闭集归一化；不经 URL 解析器点段折叠、不反复 decode。拒绝 query/hash、`%`、反斜线、双斜线、空段/末尾斜线、`.`/`..`、协议/协议相对地址、数组以及任何其它后缀。构建 `return` 用 router query 编码，解析所得值只过一次本闭集；双重编码不能绕过。普通无效 return 仍回 `/`，不发 Project 请求。

username 使用正式路由规则（3–32 ASCII、英文字母/数字、仅内部连字符），不是新账号创建规则；项目名 1–64 ASCII `[A-Za-z0-9._-]` 且不等于 `.`/`..`。不可把账号创建 reserved-name 表整张套到路由，尤其现有 `admin` 必须可作为 Owner username，`projects` 也可以拥有 `/projects/demo`，单段 `/projects` 仍是列表。

保护已有应用命名空间：`api`、`assets`、`auth`、`login`、`logout`、`invite`、`reset`、`settings`、`system`、`personal`、`diagnostics`、`livez`、`readyz`、`debug` 首段不进入 Project 动态分支。合法既有静态路由优先；其未知子路径保持原 NotFound/API 归属，不被解释为 Project。其余符合正式语法的现存用户名（含 `admin`、`root`、`support`、`projects`）不因创建时禁名而自行禁止。纯前端 raw path guard 在任何 Project API 前核验，不能仅相信 router 已 decode 的 params。必须覆盖 `/system/users`、`/settings/profile`、`/api/v1/...` 与动态路径竞争的反例；本卡不声明修改生产服务器的路由保留规则。

`forgot-password` 与 `reset-password` 不属于上述首段禁集：固定 Account `reservedNames` 没有二者，二者符合正式 NormalizeUsername 与 CurrentUserRoutes 规则。单段 `/forgot-password`、`/reset-password` 仍优先进入原公开账号页，原 query/token 捕获、隔离及清除保持；不能因新增 Project 路由改变它们。严格两段 `/forgot-password/demo`、`/reset-password/demo` 及各自 `/settings`、`/settings/general` 后缀，按同一封闭动态 route/return 规则接受，再执行正式 Resolve → 稳定 ID Get，不能只因首段同名拒绝。路由与 safeReturnTarget 测试须同时覆盖这六个合法 Project 目标、单段公开入口原 query/token 处理，以及 Project 目标携 query/token、额外后缀或编码路径仍被拒绝；公开 token 不能转成 Project return 或请求参数。

登录成功仅消费该安全目标；新的 Session 仍须自己的 Resolve/Get，不消费登录前缓存授权。未登录保存目标不保存 Project 内容、命令、token、key 或 body 到 URL/history/localStorage/sessionStorage。账号切换、公开邀请/reset 参数既有隔离及清除不受影响。

### 3.2 Resolve 与当前 Get 两步授权

地址是定位信息，稳定 ID 才是状态/意图归属。直接链接先调用正式 Resolve（username + project_name），严格校验 Resolve DTO 与当前 Human；它只提供候选 ID。随后按该 ID 调用正式 Owner Get，独立重新授权，只有两次读取与当前 Session/route generation 匹配、完整 Get 有效才发布内容及 ProjectNav。不能把 Resolve 的完整投影、列表行或用户名相等当作 Get 授权；管理员不可绕过。

从列表进入同样用标准地址与上述链路。删除中行不提供内容入口；直接 Resolve 得到 deleting 时不得把其投影变成详情，可显示不可用后返回列表。若继续执行稳定 ID Get，其拒绝须原样处理，不能放宽 Get decoder 接受 deleting。Get/Resolve 的 401/403/404 与 Unknown/网络错误分别显示，零子资源/写请求；无权和不存在不展示先前 Project 私密内容。

旧名称链接失效显示不存在并提供列表入口，不猜新名、不生成别名。旧名被复用时可 Resolve 到另一个 ID；必须建立新 Project generation，旧草稿、意图、回执及状态绝不迁移。浏览器前进/后退只恢复路由指向对象，不能用旧页面名串用 ID。

本次保存/显式恢复确认改名后，回执是历史快照。须再按原稳定 ID 当前 Get，确认同 Owner/ID 后才用当前 Session username + 当前 `normalized_name` 替换标准地址；不能用回执旧名直接跳转。当前 Get 失败时保留“命令已确认；当前信息读取失败”，只能显式重读，不能再 PATCH 或撤销已确认事实。外部并发改名、更新资料 username、路由与当前 Get 名称不一致，均重新核身份并按当前稳定 ID 的成功读结果更新定位；不自动跟随一个未经重核的新对象。

## 4. 封闭客户端、完整页及精确 DTO

### 4.1 五个端点和请求边界

在现有 `client.ts` 封闭 endpoint/transport 分派内增加以下五项，类型接口位于 `project-owner.ts`；组件/composable 不得到任意 URL、任意 headers、CSRF 或通用授权回调。继续 `credentials:same-origin`、`cache:no-store`、`redirect:error`、既有 Content-Type/status/Problem/request-id 验证。

| 操作 | 正式请求 | 客户端请求上限 / 成功响应上限 |
| --- | --- | --- |
| list | `GET /api/v1/projects`，只允许 lifecycle/limit/cursor | 无 body/key/CSRF；5 MiB |
| get | `GET /api/v1/projects/{id}`，无 query | 无 body/key/CSRF；64 KiB |
| resolve | `GET /api/v1/projects/resolve?username=…&project_name=…` | 无 body/key/CSRF；64 KiB |
| update | `PATCH /api/v1/projects/{id}` | 64 KiB / 64 KiB；原 key 与当前私有 CSRF |
| lookup | `POST /api/v1/projects/{id}/commands/lookup` | 1 KiB / 64 KiB；body 精确 `{"command":"update"}`，原 key/CSRF |

GET query 由类型化入参构造一次编码，不接收外部 query string。Project ID 必须是 canonical UUIDv7；禁止省略、混入其它 ID 或用名称作为 id。新增 cap 仅按上述 endpoint 区分；旧端点限额与通用 Problem 600000-byte 上限不改。成功必须完整读完、字节 cap、JSON 解码、全部 typed/refinement 验证后一次发布；超长/截断/多余键/状态或类型错误零新候选。不能截断字符串、项目行或偷偷改 page size 伪成功。

流读取沿既有 `readJSON` 的 actual bytes 计数、超限 cancel 与 await/releaseLock；不能只相信 Content-Length，也不能见 UI abort/Promise.race 超时即释放 Cookie owner。控制测试须有 Content-Length 缺失/谎报/分块、cap+1、cancel delayed/reject、最后字节/最后行坏值代表。

### 4.2 三种读取投影不混用

所有对象 exact keys（不接受 undefined/null 替代不同语义），字符串需合法 Unicode、不得让 TextEncoder 把 lone surrogate 静默替换。ID canonical UUIDv7；version canonical positive decimal int64 字符串，不通过 JS Number，最大 `9223372036854775807` 仍合法。Instant 用现有完整 UTC microsecond/calendar 校验，created ≤ updated，非空 archived 位于二者间。

| 投影 | 必须满足 |
| --- | --- |
| Owner Get | 精确 11 字段 id/owner_user_id/name/normalized_name/description/lifecycle/version/current_sprint_id/created_at/updated_at/archived_at；lifecycle 仅 active/archiving/archived；nullable 两字段仍必须出现；active/archiving 的 archived_at=null，archived 非 null；ID 等于目标、Owner 等于当前 Human |
| Resolve | 同 11 字段，但允许 deleting；deleting 的 archived_at 可 null 或合法区间时间。其合法性不把 Get lifecycle 放宽。normalized_name 等于 name 的 ASCII 小写；Owner 仍匹配当前 Human |
| List item | active/archived 精确 id/name/lifecycle/version/description；archiving 另必须有 operation_id；deleting 精确 id/name/lifecycle/version/operation_id，**无 description**。不混入完整 ProjectRef 字段，不推造 description、Owner、时间或进度 |

名称验证按 §3 的 Project name 规则；description ≤8192 UTF-8 bytes，拒绝 C0 中除 tab/LF 外的控制字符及 DEL，保留原空白、不 trim。页面以文本渲染，绝不 `v-html`。Get/Resolve 的完整11字段与 schema 各自对齐；lookup/PATCH 历史投影另按 §6 验证。不能用一个过宽 Project parser 替代四种应用场景的 refinement。

### 4.3 分页与 5 MiB 完整响应

类型 API 支持 limit 1..100，界面提供 25/50/100，默认25。无任意数字或自制 offset。生命周期 UI 为“全部、active、archiving、archived、deleting”单选；类型层若消费正式多状态输入，只允许最多4个唯一枚举，序列化成一个 canonical comma query。cursor 为不解码的非空 opaque 字符串，≤8192 UTF-8 bytes；无自造 TTL/到期语义，坏 cursor/旧 key 无法验签只按正式错误显示，提供显式从首页重读。

页对象只有 items/next_cursor；items 0..limit，同页 ID 唯一；next_cursor 为 null 或合法 cursor，非 null 只能跟完整 limit 页。保留服务顺序：wire 没有 created_at，不能自称验证隐含 `(created_at,id)` 排序，更不能改按名称/ID 客户端排序。下一页仅用原 cursor+同 limit/filter/identity，上一页只用同 generation 的已保存 cursor 栈；筛选/page size/Session 改变清空 cursor 和页状态。迟到页、末行坏值、非法组合不与旧页拼接，不跨 generation 去重后假成功。cursor 被当前权限拒绝须清授权内容；普通传输失败可保留明确标记的先前完整页和重试入口，但不声称加载新页成功。

现有通用600000 bytes 不足覆盖正式合法完整 Project 页，新增 List 成功 cap 精确为 `5*1024*1024 = 5242880`。按最大100行、每行 description 最大8192 bytes 即使全部以 Go JSON 最坏6倍逃逸、其它安全字段/外壳每行预留1024 bytes、cursor8192 bytes也按6倍、页壳256 bytes，保守上界 `100*(6*8192+1024)+6*8192+256 = 5067008 < 5242880`；此推导不把数据库/服务内存或 UI 全 RSS 说成有界。测试构造合法100行、最大 description、实际默认 JSON 逃逸、非空 cursor 的完整 >600000-byte body，过正式 schema 与新 client；另测边界/cap+1/坏最后行，不能通过降低字段/行数规避。真实浏览器至少26项目跨25行分页，假 transport 大页与真实 PG 浏览器证据分列。

## 5. 唯一 Session owner、草稿和实际尾部

### 5.1 必要 useSession 窄接缝

固定 `useSession.ts` SHA `f8409de32debb34d1f36d502372a746217c54d8d86299fbd6cc4a16a2e92b90d` 的 `runAuthorized.current` 当前除 personal 外均要求 admin；现有 System facade 不能用于普通 Project Owner。候选 #23 必须加入封闭 ProjectAction（read/update/lookup 的明确集合）、Project revisions/context 与 `projects` facade。Project 分支只要求当前 authenticated Human、完整 userID/sessionID/identity epoch、原 op generation/revision 和必要 CSRF；Owner 权限来自每次正式 API，不能只依赖前端 username/role。

保留原所有 System admin/denied 条件和原 personal 分支；不得通过把 Project 伪装 personal/system、放宽总 current 谓词、导出 runAuthorized/任意 callback 或新建第二 Session owner 解决。Project 当前403/404只改变当前 Project 状态，不写 System denied，不把普通 Owner 变 admin；System403仍不授予/撤销另一 Project。当前401/CSRF失效沿唯一 Session 的原失效语义；旧 identity/generation 的迟到错误不能清除新登录。Project API 依赖如需注入测试，在现有构造器参数末尾追加带默认值的专用接口，保留既有位置参数。

所有新增请求沿原唯一 owner、Cookie/CSRF 私有持有者与实际 finally。visible timeout、页面离开、abort 或新 route 不释放 actual owner；真实 fetch/body/cancel/当前回调结束后才清 busy/owner。不得重开第二队列绕过忙态，Logout/恢复 Session/任何旧 mutator 必须等待原 actual 尾部。不能把 UI 30s 可见超时改成后端30s终局或回滚证明；后端读/lookup2s、Update30s以及 Project 原库 Unknown 私有 `WithoutCancel +3s` 确认语义均不改，不能照抄 Model 的原 ctx 确认假设。

### 5.2 页面状态与草稿

App 创建并 provide 一个 `useProjectWorkspace`，其生命周期覆盖 RouterView 因 Session checking 暂时卸载；它只管理页面状态/草稿并调用封闭 Session Project facade，无 HTTP/CSRF 私有副本或身份队列。每份状态绑定完整 Session identity、稳定 Project ID、当前 route/read generation。名称仅显示与定位，不作为 draft key。

Project 页面状态至少区分 loading、current、read-error、unavailable、checking/identity-invalid；更新进度按 §6 独立。checking 隐藏受保护内容且不丢同 identity 的 dirty draft/未决意图，成功恢复相同 identity 才可重显；真正 Logout、新 Session/user/CSRF identity 变化清除对应 draft/intent。当前 username 更名导致路由关系变化须重核，不因 ID 相同跳过路由检查。晚成功、晚401/403、旧 owner 的 finally 不能覆盖新状态或清新 owner。

基本表单只可编辑 name/description，显示稳定 ID、当前 Owner identity、创建/更新时间等正式只读字段。草稿从完整当前 Get 开始；无变化禁用“保存”，取消恢复当前已读值。并发刷新/冲突不得直接覆盖 dirty 输入；提供显式重读当前值与保留/重新编辑操作，不自动 merge、改 expected_version 或自动重发。archiving/archived 当前内容只读；未决原命令的查证/原重放面板按当前 Session/原身份保留，不因普通编辑禁用一并消失。

Project 导航离开、切换项目、系统入口、浏览器返回与 Logout 进入现有聚合确认链；取消保 URL/draft/焦点，确认才丢弃本地草稿或按说明放弃追踪。Project 的确认不能清除 System model selection/Summary 两个独立 draft/intent，也不能给所有旧 mutator 自动同意。导航进行中禁止重复确认触发新命令。`beforeunload` 对 dirty/未决提示采用既有浏览器约束；用户确定关闭、刷新或本地放弃仅丢 UI 追踪，不撤销服务端命令。内存私有原 key/body/CSRF 不写 storage、history、URL、普通日志、DOM 或截图。

## 6. Update 三态、原意图与查证

### 6.1 捕获和正常保存

提交前在当前 stable ID/Owner、active 当前详情下校验 name/description；body 必须有原 expected_version，并至少一个明确存在的 name/description。只发送修改字段，遗漏和空字符串不同：空 description 清空，null 非法；不能重新读取后偷偷填入原遗漏字段。版本保持十进制字符串；MaxInt64 可作为合法 no-op/history 版本，不能前端无条件拒绝，真正递增溢出按原错误处理。

用户明确保存才生成一次随机正式 Idempotency-Key，在 Session 私有 intent 冻结 targetID、完整 identity/原 CSRF、typed input、序列化 body bytes、原 expected_version 与 generation。发出后表单更改不能改变该 intent，双击/重复触发不生成第二命令。改名前提示旧链接失效、无别名/自动跳转；保存失败保留输入。

成功 PATCH 只接受当前请求的历史11字段 active Project：同 target ID/Owner、提交中存在的字段精确相等，version 为原 expected 或合法 expected+1（最大值不溢出）。合法 no-op 同版本不能误报失败。不把当前 GET、显示值相等或时间过去当命令回执。确认后保历史事实，再按 §3 当前 Get 刷新地址/表单；当前读失败不撤销确认、不重复写。

### 6.2 三类结果与原恢复材料

状态明确为“已确认”“首次明确拒绝”“结果不确定”。未派发的客户端 validation 错误零网络；首次明确拒绝只能在此前无不确定、且合法 Problem 的 commit_state 为 not_started/not_committed 时，按接受 HTTP 的精确 status/code/字段闭集判别；Unknown 优先，不能被相同错误码降格。保留草稿并允许用户另行明确保存。至少覆盖400/INVALID_ARGUMENT，409/VERSION_CONFLICT、INVALID_STATE、PROJECT_NOT_ACTIVE，以及正式名称冲突409/RESOURCE_BUSY且字段 `/name` 的 `NAME_TAKEN`；不能仅见 RESOURCE_BUSY、503、commit_state=not_started 或有 Problem 就推导没写入。名称冲突不是自造 NAME_CONFLICT。

派发后的超时、截断、连接断开、bad response、Unknown，或不在首次明确拒绝闭集内的失败，保存原 intent 为不确定；既往不确定具有粘性，之后一次拒绝或未观察不能覆盖它。Problem 原 code/commit_state/request_id 按实际响应 header 校验，不推造 cause/attempt。`IDEMPOTENCY_KEY_REUSED` 不自动换 key、不把其它 body 的历史结果认作本意图成功；保留说明并禁用原重放，允许明确放弃本地追踪。正常读 Unknown 没有 mutation intent，不能自动调用命令 lookup。

不确定状态仅提供用户明确的“查证原命令”“按原请求重放”（条件如下）与“放弃本地追踪”；无定时自动 lookup、自动 retry、新 key、延时猜成功或拿新 expected_version 改写原请求。查证只用原 target/key/command/update 和同完整 identity/原 CSRF，在唯一 owner 中执行；不会顺带 PATCH。Session/CSRF变更后清除旧私有材料，不靠新登录恢复旧意图；这是 UI 内存恢复范围，不改变后端可接受的历史权限契约。

### 6.3 lookup 精确语义

| wire 状态 | 页面与后续 |
| --- | --- |
| `committed` | exact `state,result`；result exact `command:"update",project`；project 为同 target/Owner 的合法 active 历史投影，并校验与原捕获输入可比较的字段/版本，不能把另一意图的回执当本次确认。确认后当前 Get；不自动写 |
| `in_progress` | exact 只有 state；显示仍处理，保原材料，允许稍后用户再查证，无自动轮询或重放 |
| `not_observed` | exact 只有 state；只说明这次观察没看到，不证明永未提交/已回滚；保不确定和原材料，可用户显式原重放 |

`result:null`、错 command、多余字段、错 ID/Owner、非法版本/历史值等拒绝整响应，零确认。没有在本次 UI 私有生成/捕获的 key 不提供任意 lookup 工具。旧 key 冲突时不能把同 key 的另一个已提交 body 投影混入当前草稿。

显式原重放也允许在网络未知尚无有效 lookup 结果时由用户选择，但必须同当前完整 identity/原 CSRF、无 active owner、非 key-conflict、原 body/expected_version/ID/key 全等；按钮说明仍可能得到旧结果。不得自动改变为新命令。archiving/archived 的历史 replay 可返回原 active receipt；因此只读页面保留这一恢复操作，不能以当前不可 Mutate 一律禁历史重放。当前非 Owner/Session 已撤销仍由正式 API 拒绝，无前端豁免。

## 7. 页面与交互验收合同

列表只展示真实 name、description（deleting 无此字段）、lifecycle/version及必要操作状态；不造总数、进度百分比、当前活动量。空列表解释当前没有项目，不提供未绑定“创建”动作。筛选/翻页有加载与错误状态、明确重读；Owner 内容不可在登录/Checking遮罩后继续可交互。

工作区保持原系统导航，增加 ProjectNav。当前只显示“项目名（首页）/项目设置”，保正式导航相对顺序；未绑定会议/任务/知识库不造可点击占位。项目名长时截断并可查看完整文本。首页显示真实标题和简要已验信息/基本设置入口；Dashboard 容器不放未设计统计。设置用现有 SettingsShell，只有基本信息叶子，不造空 Agent/Models 等页面。archiving/archived 显示实际只读状态，不把处理中显示完成；deleting 列表状态不代表清理成功。

浅/深主题、1440×900 桌面和390×844窄屏、默认/减少动效共8组合；导航可横滚，焦点/选中态可见，表单内容无横向溢出。键盘完成列表进入、设置编辑、保存/取消、冲突恢复、离开确认；Dialog 焦点进入/返回调用控件、Escape按既有规则，忙态不重复提交。错误与状态通过既有 Ui 控件/aria live 呈现，非只靠颜色。测试公开角色、文本、键盘和网络结果，不以全量组件快照代替行为验证。

## 8. 唯一候选白名单与兼容面

rev2.1 当前24路径：23技术（#1–21及#23–24），README #22末件。新15/旧9；#1–23原编号不变。附录 A.2 保留原rev2的8个既有路径指纹，新增既有#24指纹及窄修改界限见下；15个新路径仍按原规格冻结时点。实施、返修与文档授权由 root 另发，本规格不授写。

| # | 路径 | 作用 |
| --- | --- | --- |
| 1 | `web/src/api/client.ts` | 五封闭 endpoint、请求/成功响应 cap、原 transport 不变量 |
| 2 | `web/src/api/project-owner.ts` | 类型输入、三读取投影和 Update/lookup严格校验 |
| 3 | `web/src/composables/useProjectWorkspace.ts` | 当前 Project、分页、草稿、导航与恢复 UI 状态 |
| 4 | `web/src/App.vue` | 单 Project owner 注入、聚合离开/Logout、生命周期 |
| 5 | `web/src/router/index.ts` | 受保护列表/双导航工作区与唯一基本设置路由 |
| 6 | `web/src/router/auth.ts` | 动态安全 return 闭集与 Project 导航确认 |
| 7 | `web/src/components/layout/ProjectNav.vue` | 双导航第二层 |
| 8 | `web/src/views/projects/ProjectListView.vue` | 真实 Owner 分页列表 |
| 9 | `web/src/views/projects/ProjectWorkspaceView.vue` | 身份/加载/错误与项目布局 |
| 10 | `web/src/views/projects/ProjectHomeView.vue` | 真实概要 |
| 11 | `web/src/views/projects/ProjectSettingsView.vue` | SettingsShell 单基本信息菜单 |
| 12 | `web/src/views/projects/ProjectGeneralSettings.vue` | 表单/只读字段/原意图恢复 |
| 13 | `web/src/tests/project-owner-client.spec.ts` | DTO、请求、流/大页/错误闭集 |
| 14 | `web/src/tests/project-workspace-state.spec.ts` | Session/ID/generation/draft/原意图实际尾部 |
| 15 | `web/src/tests/project-workspace.spec.ts` | 路由/公开页面/导航交互 |
| 16 | `web/src/tests/authentication.spec.ts` | 登录安全 return 与原入口兼容 |
| 17 | `web/src/tests/session.spec.ts` | 唯一 Cookie owner及全部旧 mutator/System gate 兼容 |
| 18 | `tests/account/project_owner_web_fixture_test.go` | 私有真实 API/static/browser 组合与安全证据 |
| 19 | `tests/account/project_owner_web_test.go` | 五新 top与真实 cleanup |
| 20 | `tests/account-captcha-web/project-owner.config.js` | 仅本卡浏览器 config |
| 21 | `tests/account-captcha-web/e2e/project-owner.spec.ts` | 五完整 UI 场景 |
| 22 | `docs/development/frontend/README.md` | 技术接受后的最小能力与边界末件 |
| 23 | `web/src/composables/useSession.ts` | 封闭普通 Human Project action/context，唯一 owner/私有 intent |
| 24 | `web/src/tests/system-user-directory.spec.ts` | 仅既定 production 导航新增 `/projects` 的旧期待兼容 |

rev2.1新增#24固定原文件 SHA-256 为 `0b895b48f2f79cb08f11e03d723bef2129edeed0009f12bc1c57b18f44995f65`。只允许原第437行 production `router.getRoutes().filter(meta.navigation).map(path)` 的期待由 `toEqual(['/system'])` 改为 `toEqual(['/system', '/projects'])`；其它断言、鉴权 metadata、重定向及 history 清理均保持。这里是路由枚举顺序，不更改 §3.1 的可见导航“项目在系统设置之前”规则，也不允许排序、弱化为包含检查或调整生产路由来迎合旧测试。

作者 f17 全纯原结果 actual1/27.742908426s、1978 PASS/1 FAIL 保留（原 command SHA `361d67301412805f01074aa0861cb4c0252a040555a95ab9d2b5ce29cfa96c8e`，raw SHA `160218af4b95c2896dd943ae76ed5f810e55e009861303dae81af1e7f47d4b55`）；原日志唯一失败就是上述导航期待。#24仅该断言修订且其它输入未变时，其余1978通过结果可作为版本组合复用，不能把f17改称全过。修后先执行受影响单文件，再完成§9既定后续必要全量/check与独立范围核对；保留原失败、精确差量和实际命令/退出/清尾证据。

不改其它旧 browser spec、公共 Ui/SettingsShell/SystemNav、System/Summary composables、schema/backend/helpers/公共 TestMain、脚本、锁或生成目录。本卡通过 existing props/meta 使用这些依赖。若实现必须新增候选或修改共享接口，先冻结具体差量报 root，不能夹带。read/Update/backend原失败与当前产物接受保持，不把本卡 UI 原恢复说成后端新能力。

## 9. 实施冻结、实际命令与预算

### 9.1 离线及依赖

先冻结 endpoint/Session/route最小稳定子集供独审，再完成24路径与实际输入闭包。读取当前可用工具的绝对路径、版本和 hash；复用接受的 Node/Chromium/Go/MinIO/PG 来源与锁版本，不升级/安装新依赖。按 design、agenteam-vue-development、vue-testing-best-practices 对应参考执行；真实浏览器另读可用 Playwright 技能，缺失报告限制。当前只做规格，自查不运行下列命令。

离线默认每条实际命令45s、Go test 内部40s；由已接受 subreaper driver监督 direct/adopted wait、实际完成与两次 owned PID 空、输入前后同。失败保存原源码/原命令/raw/退出，归因后仅重跑受影响；超预算不得后台留进程后写PASS。Go 使用 `/workspace/toolchains/go1.27.1/bin/go`、`GOTOOLCHAIN=local GOPROXY=off GOSUMDB=off GOMODCACHE=/workspace/go/pkg/mod` 与 `-mod=readonly`，GOCACHE显式任务路径或合法既有缓存；不重设 HOME。环境剥离未授权 AGENTEAM_*，不让 graph/compile意外启动资源。

| 阶段 | 精确命令形式（工具绝对路径在实际 freeze 落定） |
| --- | --- |
| types | `npm --prefix web run type-check` |
| scoped format | 现有 Prettier 对 #1–17/#20–21/#23–24 的实际文件 `--check`；只格式化授权文件，不 `--write .` |
| pure | `npm --prefix web run test:unit -- src/tests/project-owner-client.spec.ts src/tests/project-workspace-state.spec.ts src/tests/project-workspace.spec.ts src/tests/authentication.spec.ts src/tests/session.spec.ts src/tests/system-user-directory.spec.ts`；再 `npm --prefix web run test:unit` 核全部既有 mutator兼容 |
| static build | `npm --prefix web run build -- --outDir <任务自有绝对dist>`，冻结全部asset字节；不调用SPA stopped script |
| Go actual graph | `go list -deps -test -json -tags=integration ./tests/account`，另核真实TestMain动态两cmd及fixture server的CGO0变体；与正式运行环境一致 |
| Go compile/vet | `go test -race -tags=integration -c -o <私有account.test> ./tests/account`；`go vet -tags=integration ./tests/account`；两cmd必要受影响build，不执行server |
| list | 私有binary仅精确 `-test.list` 五新/下述旧top，核全名与数量；不得误跑TestMain/fixture |
| final check | `npm --prefix web run check`，前提唯一web/dist写窗；如总命令45s不足，先保原红、冻结等价子命令分组，经root协调，不静默加时 |

`go list/test -c/-list`须先静态确认 TestMain/init不因该调用启动资源，无法证明则仅准备交root，不拿离线授权启动fixture。真正闭包绑定实际 Import/Go/Cgo/Test/XTest/Embed/生成TestMain、工具、两cmd、runtime schemas、fixture/main与脚本；浏览器另加Node/Playwright/Chromium/dist/config/实际源。共享 app/Audit未冻结则不执行Go，纯前端可继续。禁止复制全源码树或只按目录名猜闭包，漏文件先停补最小输入。

### 9.2 五新真实 top

| 精确新 top | 必须覆盖 |
| --- | --- |
| `TestAccountProjectOwnerWebReadAndNavigation` | 普通Owner登录回跳；≥26真实项目跨页/筛选；list/get/resolve同body schema+client；稳定ID双授权；非Owner/admin不豁免；删除中精确行；旧名复用隔离 |
| `TestAccountProjectOwnerWebEditAndRename` | name/description、空描述、no-op/version、重名/版本冲突保输入；改名当前Get后标准地址；历史回执与当前值分离；当前Get失败只重读 |
| `TestAccountProjectOwnerWebOriginalRecovery` | 真实已提交响应丢失→未知→lookup三态→原body/key显式重放→唯一命令事实；历史archived replay；不自动lookup/换key；本地放弃不撤销 |
| `TestAccountProjectOwnerWebIdentityAndOwnership` | 实际Logout/撤权、不同Owner、Session checking与新Session、路由切换、迟到尾部；唯一Cookieowner；旧System/Summary双draft及聚合确认 |
| `TestAccountProjectOwnerWebLayouts` | 双导航/SettingsShell、8主题/尺寸/动效组合、键盘/焦点、溢出、空/错/只读；生产dist无Debug路由导入 |

Playwright config `workers:1/retries:0`，每case45s；每新Go top120s包含注册的Cleanup，包总6m。按top单轮或明确分组串行，不能超预算就扩时/绕Cleanup。真实root/API使用正式 bootstrap invitation redeem/login、正常Owner和另一个用户；Project fixture用已接受正式 project service 创建，所需Skills prepared依赖沿正式fixture。不能跨 `model_test` 包直接调用私有helper，也不能用生产stub/fakeOwnergrant替代defaultroot。若不得不辅助SQL设置已归档/删除中状态，仅合法约束下私有准备，明确它不验收生命周期停止链；不改生产锁/operation/共享helper。

恢复代表用私有同源proxy在真实backend已完成并有真实receipt后丢弃/截断返回，页面观测不确定；DB/lookup验证唯一写入，和受控无网络JSON负例分开。真实read后代理门控可造in_progress/not_observed观察窗口，但不能将假响应当真实库状态：优先真实前置阻塞/事务事实；若某状态仅离线可稳定控制，报告分列而不谎称真实PG三态。不得将HTTP响应丢失称成PG COMMIT ACKdrop，或把受控Context/error替换称成原服务Unknown。客户端取消后 proxy/body/浏览器/Central实际尾部都要有真实退出证据。

### 9.3 原端点与 Session 兼容回归

`useSession.ts`与client/auth/App变化影响所有mutator，不能只测Project。全部既有纯测试必须覆盖原admin谓词、personal、Profile/Avatar/Password、Invitation、Provider credential、Model、四用途/System Summary两draft、AccountSecurity、SMTP settings/delivery、OutboundPolicy与Logout；Project本地拒绝不污染其权限。必要补断言仅#16/#17，不重写其它测试证明自己。

旧真实精确top必须按原源实际-list核名，分轮不改body/预算，至少以下16个：

1. `TestAccountAuthenticationWebSessionLifecycle`
2. `TestAccountAuthenticationWebRevocationAndExpiry`
3. `TestAccountPersonalSettingsWebProfileAndAvatar`
4. `TestAccountPersonalSettingsWebThemeAndNavigation`
5. `TestAccountPersonalSettingsWebPasswordRotation`
6. `TestAccountSystemInvitationsWebOutcomeRecovery`
7. `TestAccountSystemProvidersWebCredentialReplacement`
8. `TestAccountSystemModelsWebOutcomeRecovery`
9. `TestAccountSystemModelSelectionWebOutcomeRecovery`
10. `TestAccountSystemMeetingSummaryWebRecovery`
11. `TestAccountSystemMeetingSummaryWebAuthorityNavigation`
12. `TestAccountSystemAccountSecurityWebOutcomeRecovery`
13. `TestAccountSystemSMTPSettingsWebOutcomeRecovery`
14. `TestAccountSystemSMTPDeliveryWebTestAndRetry`
15. `TestAccountSystemOutboundPolicyWebMutationAndRecovery`
16. `TestAccountPublicEntryWebIdentityNavigation`

只读System UserDirectory/Audit/RuntimeInformation的current权限与late-response路径还须在普通/race独立受控或既有浏览器适用证据中覆盖；不把Project身份分支放宽它们。旧证据只在相关输入/实际调用图未变时复用；本次唯一Session改动直接影响的场景不能仅引用旧PASS。已接受后端自然deadline/事务/root完整结果不因纯UI变更整套重跑；若实际图显示其它共同影响再明确补项。

### 9.4 真实资源、证据与独立门槛

真实窗口由 root 另授、唯一占用，不能与 Audit 或其他团队资源并行。沿当前 `scripts/test-objects.sh` 的实际20包/TestMain/dynamiccmd/full fixture，四容器三网络；每轮fresh≥5GiB，重建 live PID+starttime、Docker/daemon与TCP基线。任务空Dockerconfig核两接受固定PG localdigest，缺失/不符停报不pull替tag；MinIO SHA/source固定。不复用旧机器/重启前PID或daemon计数。

实际 driver freeze 包含精确selectors、120s/6m、完整fixture启动/失败清理、subreaper direct/adopted actualwait、watchdog actualjoin、两次owned PID/全TCP含TIME_WAIT及7资源空、输入/hash/fileset前后同。浏览器、Node、proxy、Central及辅助进程均登记所有权并有幂等cleanup/actualjoin，失败前注册，不能只有成功close。cleanup观察预算与业务预算分列，不以ctx取消/port关闭证明goroutine/进程join；daemon/PID1非owned新shim等不能wait须单列实际集合，owned双清不等于全机零。每轮满足PASS+实际wait/双清/input同才下一轮；任何失败保原红清尾后停，不自动retry。

安全证据只保存必要原响应 bytes 与sidecar：实际status、Content-Type、实际X-Request-ID、target endpoint/Project ID、source run、input hash。成功/Problem均不重编码原body；安全DTO可逐hash去重。无Cookie/CSRF/Idempotency-Key/密码/账号token/请求正文/原payload日志；命令是否原样与key一致用私有比较及安全布尔结果，不能在闭集trace输出原材料。screenshots清除私密请求材料，不录HAR或任意网络body。schema脚本用已有依赖、固定 `$ref` 文件验证同一真实safe body，再走公开client；controlled负例与真实原件分列。

作者交freeze、实际命令/env/raw/退出/actualwait/cleanup/安全body/版本组合与限制；独立负责人对24路径完整STATIC，独立受控验证三parser、合法100行cap、return闭集、原意图/Sessionowner尾部，再至少实际A读写身份/路由、B恢复与旧域回归组合，核同body schema/client及8布局。不是复看作者日志就称独立动态。最终README #22只在技术接受后写能力/命令与SPA/harness边界，独立末件核后root整合。产品全卡接受不得提前写成D26/D27模块完成、生产托管或三个停止解除。

## 10. 当前交付与结束条件

本卡归位已接受的 rev2 规格、候选23路径及有限来源哈希，没有源码复制、产品实现、测试运行或资源。工程选择均基于已确认 Owner页面规则与正式端口；无需新增用户产品决定。正式归位末件窄核、实施派工及Audit实际共享图交接仍待完成；原三个停止保持。任何技术来源冲突、未接受依赖或scope不足只停受影响处报root，不以占位能力补齐。

## 附录 A. 固定来源与行政定位

以下为规格来源指纹，不是实际 Go/browser/runtime 闭包，也不授权消费 Audit 活动源。基线提交由 root 提供并已核远端；本卡作者未执行 Git。独立 rev1 全文审查唯一 B1 已在 rev2 修订：合法 username `forgot-password`、`reset-password` 的 Project 路径不再被首段整体禁用，公开单段入口保持。原 rev1 与修订证据保留，正式规格验收原件待归档；不以尚未生成的归档页作链接。

| 固定文档原件 | SHA-256 |
| --- | --- |
| 受审 rev1 草稿 | `182dafda155613991980d0ebacf47efda39164eb7bd2f3d798615928a035ba18` |
| 受审 rev1 冻结 | `6680d2964553812dd5002d45efe7344610abc081591d0617e758652bf09bacfd` |
| 已接受 rev2 草稿 | `c71bfc5acbdd63dce8895af6f25beedef85fe1aa8f23730803692dd58bd72974` |
| rev1 → rev2 精确差量 | `5822465422498218af94554333c3222e73eb95c3234a564c0ece94e975be2d23` |
| rev2 冻结 | `e801086265339e2799ba6fac6444f008954527f9c18a85e64813226ca46cfbad` |
| rev2 输入索引（65源） | `dbd4a8c68311f0de89b3442bfdc259f3d0ac16d5ca6d595d1d6a81530b29cf3b` |
| 候选清单（23路径） | `408b9c64cf397ce1ef472522b433bc8e90fc35dd064410e6c2cd44a1c4933597` |

### A.1 有限来源65项

文件路径为仓库定位，hash 固定本卡读取版本；文档后续行政更新不自动成为产品输入，未来运行须绑定当时真正接受并交接的相关源。下表不复制源码或工具。

| 来源 | SHA-256 |
| --- | --- |
| [docs/development/development-plan.md](../development-plan.md) | `93b80c48b6b2fba4240b0a271177e5ce2ccb899b892ba39cd5f4a5b42babaa78` |
| [docs/frontend-design/README.md](../../frontend-design/README.md) | `729afba2f45792b46fcc6906947b9516c87e88515c62a20b605f62c07af82e12` |
| [docs/frontend-design/layouts/system-pages.md](../../frontend-design/layouts/system-pages.md) | `ecdeebc169105d27218c4b8759a750ff9948a54f4e96fd95a8d74204fb9fea22` |
| [docs/frontend-design/layouts/project-workspace.md](../../frontend-design/layouts/project-workspace.md) | `7201d7be393cbd95a6fdea9b6b33dd0bdebb8758cdb7b48243815b4d968f092e` |
| [docs/frontend-design/layouts/project-settings.md](../../frontend-design/layouts/project-settings.md) | `ed5eaf1bc5c3565af29e0a262daccbdab1ce49e53e90843a74f3e0c5a66e8115` |
| [docs/development/frontend/README.md](../frontend/README.md) | `2b4545145a9b377de0f6d7d76cc0e8d808e62f2b846f51b4561d9cc2e9ba24d6` |
| [docs/development/agent-team/project-owner-read-http-verification.md](../agent-team/project-owner-read-http-verification.md) | `0a8fcce3e0360179e35ed079332b220219c0f500f0cd9576e58b7c67c0efbc6e` |
| [docs/development/agent-team/project-owner-update-http-verification.md](../agent-team/project-owner-update-http-verification.md) | `a79a76f60a38f57af558734892836f55883cfc0da105d4ff85bdcf8591bb1a50` |
| [docs/development/work-items/d09-project-usage-read-http.md](d09-project-usage-read-http.md) | `b872bc7481c8236bbfdea826c7e40c9f96f083669b4189f1a9437b1d976ac6e0` |
| [api/openapi/project-owner.json](../../../api/openapi/project-owner.json) | `1b4dfdb90214dacfa1451c57e27936ee8b43cba74523b7d0d10642285e0f5ac3` |
| [api/openapi/project-usage.json](../../../api/openapi/project-usage.json) | `a908bd1e82d576441e978006fbfd5328e600480a4aa606dca3c818160a47feb4` |
| [docs/development/agent-team/project-model-configuration-write-http-verification.md](../agent-team/project-model-configuration-write-http-verification.md) | `d1262d7435cd2b5c3f83800b8b2212a299f78ef4ee9a140304807fa80a8d8e3e` |
| [docs/development/agent-team/project-model-credentials-http-verification.md](../agent-team/project-model-credentials-http-verification.md) | `b8e3f0f75b2235a228db6e3213adcbff1169be5712e7306c6fb5ab5a73b7f560` |
| [docs/development/agent-team/project-model-owner-read-http-verification.md](../agent-team/project-model-owner-read-http-verification.md) | `a2ad088c3bd7f72c33eae9222f3b1759bc0f198e042a73d3eaab9455dec3c39d` |
| [docs/development/agent-team/system-meeting-summary-settings-verification.md](../agent-team/system-meeting-summary-settings-verification.md) | `cfc2d76f81444f869c826cab319f060242132b1dc82de0f550052ae27fed7f2f` |
| [docs/development/agent-team/system-meeting-summary-resolution-verification.md](../agent-team/system-meeting-summary-resolution-verification.md) | `dcdca9e22f46ea69b8b03c108873422a77aa4a644108d37f6beed2c0c6205ec1` |
| [docs/development/work-items/d04-project-owner-audit-http.md](d04-project-owner-audit-http.md) | `504ce392a4352a395c527bd7928fca5abef94e0aa313b733be12677c0ac79f55` |
| [docs/development/agent-team/recovery-2026-10-06-continuation.md](../agent-team/recovery-2026-10-06-continuation.md) | `a5a629d17ae51b0c037b8efdf238f42b01afe46479a28779340af1021504ab18` |
| [docs/development/work-items/d28-central-spa-hosting.md](d28-central-spa-hosting.md) | `2cd71ed46779cbf382d0c507624839129a912ffa26a14056fe70678ae117fbd8` |
| [web/src/router/index.ts](../../../web/src/router/index.ts) | `b38c47c9965fb22a0366e9af73137725d3648ce6f4ce786b5f5d6d059c9c71f0` |
| [web/src/router/auth.ts](../../../web/src/router/auth.ts) | `bdc8db93bfd9222a5d1d079f2f6b868d0c00200edb892a517ae28e1f9f725715` |
| [web/src/api/client.ts](../../../web/src/api/client.ts) | `998e1edd8c1ffe3a3133b821f5169ec4b48cba8fefa98478b9abfa17f0bb6d7e` |
| [web/src/App.vue](../../../web/src/App.vue) | `4f7068223c59aa7e819dd5cfef6599fb590f663633edd2e821d29d1a1bc1b24b` |
| [web/package.json](../../../web/package.json) | `20951cd8bd5687d40700328af4fef60e3585c638b57ec939e5f2a54dbbbbed4c` |
| [tests/account/system_meeting_summary_web_fixture_test.go](../../../tests/account/system_meeting_summary_web_fixture_test.go) | `d88fe8e6e77dfd43785e885e19e024d455cba364cef33a452498db7fe711da35` |
| [web/src/composables/useSession.ts](../../../web/src/composables/useSession.ts) | `f8409de32debb34d1f36d502372a746217c54d8d86299fbd6cc4a16a2e92b90d` |
| [web/src/composables/usePersonalSettings.ts](../../../web/src/composables/usePersonalSettings.ts) | `bdc6a6580e89b71de90176a0ba72884981a88de001a0a72b08a349f7acad1679` |
| [web/src/composables/useSystemProviders.ts](../../../web/src/composables/useSystemProviders.ts) | `79e92bc3f18a9045d5eacf0de425df128997940b31958aee65ae795728ff7488` |
| [web/src/api/system-providers.ts](../../../web/src/api/system-providers.ts) | `b0b9c0bbc8f342cb1a4b98c237d0951faccaf94f681c734eae696c5e4fa48ade` |
| [web/src/api/system-account.ts](../../../web/src/api/system-account.ts) | `84e39fbc4378c5e779096aa09b9a8052a23a1c3aa93915b3abd1e674a71c01a6` |
| [web/src/components/layout/SystemNav.vue](../../../web/src/components/layout/SystemNav.vue) | `f381d7134b04d7f2f43e10a2be55ca2fd885b75b75e5528913582fc6d88ed1f7` |
| [web/src/components/layout/SettingsShell.vue](../../../web/src/components/layout/SettingsShell.vue) | `8815f6b4ccae1afbaeef4edd3e18ae92d700a791ea156bb97204069e0859feac` |
| [internal/central/project/contract/validation.go](../../../internal/central/project/contract/validation.go) | `73db8aeb4db685433279cdeeb119fe12a231d7193d9b93e97305b7f30445c64b` |
| [internal/central/project/contract/types.go](../../../internal/central/project/contract/types.go) | `7465815c5da4e1bf24b6958d1aa1c93959ffece4256883bd343199c3ed9cfbf1` |
| [internal/central/project/repository.go](../../../internal/central/project/repository.go) | `f89d5b582b79cb31434d70c03be43a99ac1b66261591850d141263c2af44ed55` |
| [internal/central/account/profile.go](../../../internal/central/account/profile.go) | `ea855986f5af22b6d2eead4137409526dd24d8d87ed82049a3443b77d473b1b5` |
| [internal/central/account/validation.go](../../../internal/central/account/validation.go) | `60728d7f7959cc0644146798f7d82798adbfc054dcf815faf7acf0f282ae0c94` |
| [internal/central/foundation/fault.go](../../../internal/central/foundation/fault.go) | `94a339976fb8f1e1173256aa840dd1273644cafd37dc6a3e96e33d1cb8ff09de` |
| [docs/development/work-items/d08-project-owner-read-http.md](d08-project-owner-read-http.md) | `45d77d3add041e9c87cb936c6c7cc82b84c112d897758e6234e3d8a654071bc4` |
| [docs/development/work-items/d08-project-owner-update-http.md](d08-project-owner-update-http.md) | `fe99b367797e6a4d02278581cff900c38fa769d572c1d5e98389d5a70bf3846d` |
| [docs/development/frontend/components.md](../frontend/components.md) | `0a20dc50008737e56d094e641dc2354816596553785fa4c3bf5de2d3d3784e96` |
| [docs/frontend-design/layouts/settings-shell.md](../../frontend-design/layouts/settings-shell.md) | `696e85efd8a042696921b9733795d0f58d8351286200b5b9d420311e64d8f0b8` |
| [docs/frontend-design/styles/components-and-interactions.md](../../frontend-design/styles/components-and-interactions.md) | `ae4e5a22a34aa9c05582e0a5799d417ef18d82aad9413e2fe811cdca9ed3f3a7` |
| [tests/account/system_meeting_summary_web_test.go](../../../tests/account/system_meeting_summary_web_test.go) | `dbd26eab2e29128fc014209d6ba4e5f3ffa36abb61f759f15f99efe4119b0376` |
| [tests/account-captcha-web/system-meeting-summary.config.js](../../../tests/account-captcha-web/system-meeting-summary.config.js) | `df49760216621b4534ba03f3cddef2d82b2747168080f9e8f6fd3120a42331e1` |
| [tests/account-captcha-web/package.json](../../../tests/account-captcha-web/package.json) | `ffb289585d3cb1ed8e70c62ee55bd56ca66e2c79a807909e77630a4283a15e58` |
| [scripts/test-objects.sh](../../../scripts/test-objects.sh) | `71aff74a122d5935ec02d8b80fc27be0a7cb3020650ac40dc1c0d48b9d012d87` |
| [tests/model/project_configuration_fixture_test.go](../../../tests/model/project_configuration_fixture_test.go) | `d19c5fb039e784e9747fe853261ceefa4ebdccba9659e5a1010e34832e5c523a` |
| [.agents/skills/agenteam-design/SKILL.md](../../../.agents/skills/agenteam-design/SKILL.md) | `81834a981f226cc663c42b9dcdfd9b56001d2cfcd649107064bf865af4d61b81` |
| [.agents/skills/agenteam-vue-development/SKILL.md](../../../.agents/skills/agenteam-vue-development/SKILL.md) | `a52ae4b3e6913f3a37b6944e833de9fc282ecb282bf6d2478aea783b19bee039` |
| [.agents/skills/vue-testing-best-practices/SKILL.md](../../../.agents/skills/vue-testing-best-practices/SKILL.md) | `376fd21d6adfd37f7d102fa17ca099abddef16943389f37588405b013cef23b8` |
| [tests/account/account_entry_web_test.go](../../../tests/account/account_entry_web_test.go) | `9d939b87c874b1674e2c1615a3ca5e15de8afcf0cac47393215ea9371c3c48fd` |
| [tests/account/authentication_web_test.go](../../../tests/account/authentication_web_test.go) | `5773a99d9b335e24a18c1fd2e8b6c6a3e019ff475eb8158c64cd651e89c42e76` |
| [tests/account/personal_settings_web_test.go](../../../tests/account/personal_settings_web_test.go) | `bb0855b76f323f793978b98c45723b60efd621e47ee7e2c36075d4b0a8fd941b` |
| [tests/account/system_invitations_web_test.go](../../../tests/account/system_invitations_web_test.go) | `bae6e22be8f6bbcf06349f8b1a85858503c1b5ce623ce2a519e48df04f92cf0a` |
| [tests/account/system_providers_web_test.go](../../../tests/account/system_providers_web_test.go) | `1082f1a9e7bb38d9655c0a4d3fcf3dcdd62d63aac60b14472b6cebd7873f48cd` |
| [tests/account/system_models_web_test.go](../../../tests/account/system_models_web_test.go) | `d05ac43aca6998bd251d40d9ec4a41b0f33b399490e555faa18d13dd68132038` |
| [tests/account/system_model_selection_web_test.go](../../../tests/account/system_model_selection_web_test.go) | `6c60bb68e521685eedfbca9107c026c3f6646119cda4f5c841d2533ed77e3c8e` |
| [tests/account/system_account_security_web_test.go](../../../tests/account/system_account_security_web_test.go) | `3fdece54c9b80706a9939f4bb6346afe220b2f37abb2201d7e41213d6b10556d` |
| [tests/account/system_smtp_settings_web_test.go](../../../tests/account/system_smtp_settings_web_test.go) | `df9fee364590ffe0f8fa9998e341c59e466a8d0b8c65047970b9f4ffb90bbd1b` |
| [tests/account/system_smtp_delivery_web_test.go](../../../tests/account/system_smtp_delivery_web_test.go) | `9cb43e944cfdfaed19955dd80c6646e576aa8a2500b669888755129389196f52` |
| [tests/account/system_outbound_policy_web_test.go](../../../tests/account/system_outbound_policy_web_test.go) | `61ec8d9518df4ef6c24e3ab67c6afd83babdac888660fdd338856555bec473b8` |
| [tests/account/system_user_directory_web_test.go](../../../tests/account/system_user_directory_web_test.go) | `8f949e08cb00f4676a4b6f0367cc110e5782dfd261f3e8cdee4e9155fb2f8dbe` |
| [tests/account/system_audit_web_test.go](../../../tests/account/system_audit_web_test.go) | `ced0a767c84ef783ab0ca6170627c8f77fc0bb30cd299d4783cfbed08c58930f` |
| [tests/account/system_runtime_information_web_test.go](../../../tests/account/system_runtime_information_web_test.go) | `d0ff7c71af3cac638031507c9f3f502bfcacdec23335294ff8b40ce512a9b121` |

### A.2 候选既有路径基线

§8是完整23路径白名单；下表仅补8个既有文件的固定原字节，其余15个新路径及编号以§8为准。#22 README最后另授，#23 Session扩展已经纳入本规格；不包括三根、Audit、SPA停止链或四入口。

| 编号 | 既有路径 | SHA-256 |
| --- | --- | --- |
| 1 | `web/src/api/client.ts` | `998e1edd8c1ffe3a3133b821f5169ec4b48cba8fefa98478b9abfa17f0bb6d7e` |
| 4 | `web/src/App.vue` | `4f7068223c59aa7e819dd5cfef6599fb590f663633edd2e821d29d1a1bc1b24b` |
| 5 | `web/src/router/index.ts` | `b38c47c9965fb22a0366e9af73137725d3648ce6f4ce786b5f5d6d059c9c71f0` |
| 6 | `web/src/router/auth.ts` | `bdc8db93bfd9222a5d1d079f2f6b868d0c00200edb892a517ae28e1f9f725715` |
| 16 | `web/src/tests/authentication.spec.ts` | `da1065fc139ecb8343ef87199a300424233c802cbb9b9ef1aa546039cc2a96b1` |
| 17 | `web/src/tests/session.spec.ts` | `4c4d1483b3f75a5a3fca6f5f54d39526cae24c5f0a6674989d90348f30fbfc3a` |
| 22 | `docs/development/frontend/README.md` | `2b4545145a9b377de0f6d7d76cc0e8d808e62f2b846f51b4561d9cc2e9ba24d6` |
| 23 | `web/src/composables/useSession.ts` | `f8409de32debb34d1f36d502372a746217c54d8d86299fbd6cc4a16a2e92b90d` |
