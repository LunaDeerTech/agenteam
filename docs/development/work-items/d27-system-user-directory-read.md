# D27：系统用户目录注册时间读口

状态：rev2，2026-10-06 后端六路径已完成作者检查及独立 PASS，主线程采纳并提交推送 `3affc0194214101cfa1e6fdc583afa5d60005db8`、实际远端一致；最终 input02 SHA256 `004ced3247661feca93ef7899dbc539f9f638a17daa824c30692881f26622c96`，见[正式验收与持久证据](../agent-team/system-user-directory-read-verification.md)。本次仅更新页首，以下修订依据与技术正文保持；rev2 被审稿 SHA256 为 `e05b1f60545cdae9756c195ee0b944be8cb0c6f1d3f073c959f6dcb04e30a0ba`。rev1 曾独立 STATIC PASS，被审稿 SHA256 `4edb37f8c7cd1f9aa492b2a797f348d6e8bb5a92c0a52b405c365eb40537d216`；其 HEAD 路由静审遗漏、author01 首红与修后原断言通过全部保留。本卡仅接受后端读口，不代表系统设置页面、完整 D27 或平台完成。

修订依据：作者真实 `author01` 的 `TestAccountHTTPSystemUserDirectory` 在 `tests/account/http_user_directory_test.go:35` 观察合法 admin HEAD 实际 405、期望 200。Account 的 `httpHandler` 按无 method 的路径注册，再严格匹配 `r.Method == route.method`，不存在 GET 隐式接收 HEAD；原来只有 avatar 显式登记 HEAD。保留首红、原输入与日志，不将测试改为接受 405，也不把直接调用 `httpJSON` 的无 body 通过证据当成路由已通。rev2 补这条正式路由和对应 query/OpenAPI，不泛化其他 Account GET。

依赖的网络恢复仅用于准备阶段；正式检查继续使用既定只读依赖模式及 `GOPROXY=off GOSUMDB=off`，不更改依赖锁。

## 1. 目标、基线与依赖

[系统设置布局 §2](../../frontend-design/layouts/system-settings.md#2-用户与邀请)要求用户列表显示邮箱、用户名、显示名、角色和注册时间；[账号架构](../../architecture/platform-infrastructure/authentication/account-lifecycle.md#1-user-与身份边界)已确定 User 的 `created_at`。没有新增产品决定。

固定基线 `125e3c222ffca01ea0fd987d7dc47b8c30fa5f8b`。实际 `SystemHTTPFacade.ListUsers` 已从 `agenteam_account.users` 读取 `created_at`，但只用于分页；`account/contract.User` 和共享 `httpUser` 均无该字段。直接扩充共享 User 会改变已验登录、Session、个人资料及前端严格 DTO，故仅增加系统用户列表专用投影。

| 依赖 | 已验收证据与本卡消费范围 |
| --- | --- |
| D07 Account/System、真实 Session/admin 授权及根装配 | [D07 主卡 B04 终局](d07-account-session-smtp.md#b04-终局采纳与文档关闭)，B37 `022dcea`、关闭 `0ed8085`；消费现有 `SystemHTTPFacade`、`httpRead`、用户表与真实 HTTP fixture。 |
| D03 事务、D04 signed cursor | 已随上述 D07 组合验收；复用现有事务与 users cursor binding，不增加共享接口或迁移。 |
| 现有 Account 消费方兼容 | [认证前端](../agent-team/d26-authentication-verification.md)、[个人设置](../agent-team/personal-settings-verification.md)、[公开入口](../agent-team/public-account-entry-verification.md)均已接受；保留其共用 User 原形状，不要求重新实现或完整重跑前端。 |

本卡不依赖未验 OpenAI tools、Anthropic 实施、Summary 初值、Project/Artifact 生命周期或新的 Object 绑定；被安全筛查停止的 Object 原任务和 tools 独立原任务均不得恢复、改派或重建。真实测试只使用已验 Account fixture；不执行被停止的探针。

## 2. 库投影与 HTTP

在 `internal/central/account/http_facade.go` 定义专用结果：

```go
type HTTPUserListItem struct {
	User      c.User
	CreatedAt foundation.Instant
}
type HTTPUserList struct {
	Items      []HTTPUserListItem
	NextCursor string
}
```

`ListUsers(ctx, actor, HTTPListRequest)` 签名和用途不变，只有列表元素换为上述专用类型；当前唯一产品调用者为 `httpListUsers`。`c.User`、`httpUser`、`httpUserDTO` 及其他读写响应不改。

`GET /api/v1/system/users` 的外层仍为 `{items: [...], next_cursor?: string}`。每个 item 是以下**平坦九字段**，原八字段值与规则保持，只增加必需、非 null 的 `created_at`：

```json
{
  "id": "<UUIDv7>",
  "email": "member@example.com",
  "username": "member",
  "display_name": "Member",
  "role": "user",
  "theme": "system",
  "version": "1",
  "initial_password_suggestion": false,
  "created_at": "2026-10-06T01:02:03.456789Z"
}
```

时间来自对应用户行的 `created_at`，不取 `updated_at`、Session 签发时间、UUID 时间、请求当前时间或前端推导值。新 DTO 仅在 `http_system.go` 内定义，可组合既有 `httpUserDTO`；不得输出嵌套 `user` 字段，不增加项目、凭据、密码或其他资料。

OpenAPI 保留 `User` schema 原字节；新增闭合 `SystemUser` schema，明确列出原八字段与 `created_at`，`additionalProperties: false`，九字段均 required。`UserList.items` 改引用 `SystemUser`；不要用受 `User.additionalProperties: false` 阻断的 `allOf` 扩展。仅 `/api/v1/system/users` 新增显式 `head` operation，唯一 `operationId: headUsers`，security 与 cursor/limit 参数同 GET；200 和 default 错误响应均不声明 `content`，描述与 GET 相同的状态/权限/查询检查、安全 headers、Content-Type 与所编码表示的 Content-Length，但没有 body。其他 schema/route 语义不改，GET 描述同步准确能力。

在 `httpRoutes` 紧邻 users GET 显式增加 `HEAD /system/users`，同为 `authority: admin`、`command: false`、`serve: httpListUsers`。`httpDispatch` 的列表 query 分类仅额外接受这个 path+HEAD 组合，保持三个既有 GET 列表的原分类；不得将请求 method 改写为 GET，或为其他路径增加隐式 HEAD。两种 method 各自通过原 CSRF/安全分类、Session/admin 和事务内当前权限检查，再进入同一分页、时间校验和编码路径。合法 limit/cursor 对两者含义相同；非法 query 的状态同 GET。

HEAD 复用现有 `httpWriteEncoded`/`WriteProblem`，成功及错误都无 body；成功 Content-Length 是完整列表表示的编码长度，错误保留对应安全 Problem 的元数据与状态，不把 HEAD 当成免查询或免权限的快捷路径。users 的不支持 method 沿原 handler 返回 405 且 `Allow: GET, HEAD`；其他 Account GET 的 HEAD 仍保持既有 405，原 avatar HEAD 不变。

## 3. 事务、时间与分页

- 继续调用原 `httpRead(..., "users", account-directory SH, ...)`；保留同事务的当前 User SH 与 `AuthorizeSystem(Read)`，HTTP 预授权不能替代事务内当前身份核对。不改 `httpRead`、Authority、生命周期或 CommitResult 处理，不新增超时、事务、SQL 写入、重试、后台工作、Audit/Event/命令身份。
- SQL 继续使用现有显式列、`ORDER BY created_at DESC,id DESC`、`LIMIT limit+1`；在一次扫描中取得资料和注册时间。将每行扫描的 `time.Time` 直接交给 `foundation.NewInstant`，**显式检查其错误**，再写入专用 item；禁止使用丢弃转换错误的 `instant` 助手，禁止失败后以零值或当前时间补齐。有效时间沿 foundation 规则规范化为 UTC/微秒；不另造注册时间业务范围或拒绝 foundation 支持的年份。
- 行扫描、ID/version/role/theme 或时间转换错误保留现有安全失败方式；新时间转换错误用 `DependencyUnavailable`。任何错误只返回零 `HTTPUserList`，HTTP 不发布部分 items、候选 cursor 或伪造的时间。新增校验也覆盖已扫描的第 `limit+1` 行，不能跳过无效哨兵行后伪称分页成功。
- limit 默认 25、范围 1–100，空列表为 `items: []`。cursor 的资源 binding、排序、签名、末行边界、跨资源拒绝、可变合法 limit 及省略空 next_cursor 全部不变；旧有效 users cursor 可继续使用。注册时间不是资料 version，不改变写入或冲突语义。
- 未认证、权限拒绝、cursor/limit 错误、事务失败/Unknown 与取消，均沿原读链和错误投影；不新增本卡专有查证或命令重放。若发现原 `httpRead` 的其他真实缺陷，保留证据另报主线程，本卡不默默扩大修复范围。

## 4. 唯一候选路径与实施边界

| 路径 | 授权候选范围 |
| --- | --- |
| `internal/central/account/http.go` | rev2 新增授权候选；仅 users 显式 HEAD route 与该 path+HEAD 的 list query 分类，不泛化 method 分派。 |
| `internal/central/account/http_facade.go` | 仅 `HTTPUserListItem` / `HTTPUserList` 与 `ListUsers` 扫描、校验及投影。 |
| `internal/central/account/http_system.go` | 仅系统用户列表专用 DTO 与 `httpListUsers`。 |
| `api/openapi/account.json` | 仅新增 SystemUser、UserList items 引用、listUsers 描述与 users HEAD operation。 |
| `internal/central/account/http_user_directory_test.go` | 新；专用投影、时间错误、分页兼容、共享 User 形状及实际 handler/HEAD/OpenAPI 边界。 |
| `tests/account/http_user_directory_test.go` | 新；真实 Account HTTP/PG，保留首红 HEAD 200 断言，补 query/权限/错误无 body 与本文件私有 helper。 |

共六个候选业务/测试路径；本规格当前由 architecture_worker 独占，另行移交后才能更新状态。实现者必读[Go 技能](../../../.agents/skills/agenteam-go-development/SKILL.md)，独立负责人读[验证技能](../../../.agents/skills/agenteam-verification/SKILL.md)。实际 `TestB04HTTPOpenAPIRoutesAgree` 是双向枚举，没有固定 route count；原 `TestB04CSRFNoIdentityBypassOnProtectedRoutes` 也逐路由枚举，因此两项原测试应自动覆盖新增 HEAD，**不修改旧 `http_test.go`/`csrf_test.go` 或放宽其断言**。不改 `http_wire.go`、`contract.User`、共享鉴权/事务、旧测试/fixture、数据库迁移、依赖锁、应用根或 web。若确有范围外编译接缝，先报精确引用与最小差量。

邀请列表目前尚缺持久可关联的投递渠道/最近结果投影，不在本卡顺带处理；也不生成默认用户空页面。后续系统设置 UI 从真实用户目录开始，保持普通入口默认“用户与邀请 → 用户”；Model UI 再消费已验 System Model 配置和管理读口。

## 5. 验收与交付

| 检查 | 必须证明 |
| --- | --- |
| 纯投影与兼容 | 系统 item 恰九字段、flat、canonical 微秒字符串；旧 `httpUserDTO` 与 OpenAPI User 仍恰原八字段。版本仍 decimal string，空 items 非 null，SystemUser 与 UserList schema 正确闭合。 |
| 时间与失败 | 有效 offset/微秒时间按 foundation 规范化；超出 foundation 年份范围的扫描值显式失败，不能转成零 Instant 成功；中途行/哨兵校验失败均零结果。通过同包受控 Store/row 测试覆盖，不把替身说成真实数据库行为。 |
| 真实注册时间与分页 | 新顶层 `TestAccountHTTPSystemUserDirectory` 使用 `newHTTPFixture`，读取真实初始化用户和正式邀请兑换创建的用户；GET 注册时间逐项等于数据库 canonical 值。验证资料更新不改注册时间、limit 分页无重复/遗漏，以及同 created_at 按 ID 的稳定边界；同时间值仅可在自有测试库作为明确 fixture 调整，不改生产规则。 |
| HEAD 路由与权限 | 新纯测试经实际 `httpHandler` 验 users HEAD 已进入权限检查、其他未登记 HEAD 仍405及 users 精确 Allow；OpenAPI HEAD security/参数、200/default 无 content 正确，原双向路由/CSRF遍历通过。真实 admin HEAD 无 query、合法 limit/cursor 均200且 body 空；非法 limit、篡改/跨资源 cursor 的 HEAD 与 GET 同状态且无 body。普通用户、匿名/撤销 Session 的 HEAD 分别沿403/401拒绝；不能将无权限405计作通过。新增时间校验失败的 HEAD 仍沿503且无 body；复用既有权限链。 |
| 旧响应与回归 | 真实登录/Session/profile 的 User 仍为原八字段。运行既有 `TestAccountHTTPAdministratorPagesSettingsAndCurrentAuthority`，保留其当前角色变化、cursor 和管理员行为断言；不重写旧断言以迎合新投影。 |

作者先对固定输入执行 Go1.27.1 的 `go test -count=1 ./internal/central/account/...`、对应 `-race`、`go vet`，再以 `-tags=integration -run '^$'` 编译 `./tests/account`，记录无动态用例的编译边界；构建 Central/Runner 以核调用兼容。依赖用既有锁和缓存，`GOTOOLCHAIN=local GOPROXY=off GOSUMDB=off GOFLAGS=-mod=readonly`，GOCACHE/TMPDIR 归任务私有。

主线程移交独占真实资源窗口后，仅运行既有 `scripts/test-security.sh -run '^(TestAccountHTTPSystemUserDirectory|TestAccountHTTPAdministratorPagesSettingsAndCurrentAuthority)$'`；保持原 `-race -count=1 -timeout=6m` 与 fixture 内预算。复用受支持 PG17.x（最低17.8）/MinIO/真实 Account root，不新增网络/ROLLBACK/Object join 探针，不执行 tools 验证。保留实际 argv/env/exit/raw、固定输入与所有原失败；自有资源 exact-ID 双次清零、既有基线不变、所属进程实际退出后交回窗口。

未参与实现的 verification_worker 对冻结六路径及必要依赖核验，至少独立检查专用 schema/旧形状隔离，并在真实 HTTP/PG 上核 canonical 注册时间、完整 handler 的 HEAD/query 和非管理员拒绝；可复用未变语义的作者证据，不机械重跑完整旧账号或前端套件。原 author01 HEAD405 首红保留，修后必须按原200断言复验，不能以 serializer 单测或规格静审替代。作者/独立结论分开记录，由主线程采纳完整结果、登记限制、提交推送。

本卡没有前端、邀请投递补口、SMTP新增实投或新的生产绑定验收；这些后继责任保持单独任务。设计自查只证明规格与现有来源相符，不能写成该读口已实现或产品测试通过。
