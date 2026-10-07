# D08 Project Owner 列表与详情只读 HTTP

修订：rev2。状态：完整限定独立 STATIC PASS，主线程已采纳 rev2；未授权产品实施、Go 执行或真实资源。本卡只交付下述读取结果，不标记完整 D08 完成。

固定已接受产品基线：`baa6ffac7bdf87dea6f052704e509d7b547e9886`；[S3 验收](../agent-team/system-meeting-summary-resolution-verification.md)及归档 `77965be16feeb22d8c1b8301a047d95e3b15e8eb`不构成本卡的生成依赖。原 [Usage 根装配](../agent-team/project-usage-read-http-root-verification.md)产品 `03a4a0b87b21c9d3583c01dc3d543bb4ee31ea05`是实际读取根接缝。编卡时 S2 仍在最终联验，不消费其活动实现；共享文件交接见 §5。

必读：[仓库规范](../../../AGENTS.md)、[团队流程](../agent-team/README.md)、对应[设计](../../../.agents/skills/agenteam-design/SKILL.md)／[Go 开发](../../../.agents/skills/agenteam-go-development/SKILL.md)／[独立验收](../../../.agents/skills/agenteam-verification/SKILL.md)技能，以及 [D08 设计 §5/11/12](d08-project-owner-design.md)、[Usage HTTP §2–5](d09-project-usage-read-http.md)。规格来源与真实依赖见 §8；实现者、验收者按角色读取相关段落，不全量重读架构。

## 1. 完整结果与排除范围

| 资源 | GET 与 HEAD 的相同读取语义 |
| --- | --- |
| `/api/v1/projects` | 当前 Human 自己的已初始化项目，按既定 lifecycle 筛选、稳定顺序和 signed cursor 分页 |
| `/api/v1/projects/{id}` | 当前 Session、当前 Owner 和 Read gate 均在本次事务中通过后，返回稳定 ProjectID 的安全详情 |

两资源接入默认 `app.Run` 的现有正式 Account 边界。原 `/api/v1/projects/resolve` 和两个 nested Usage 资源继续归 Usage dispatcher，不重复实现名称解析，不让 `{id}` 抢占 `resolve`。稳定 ID 详情不绑定旧名称；每次读取重新授权，不能用先前 resolve 的结果代替当前权限。

不新增创建、修改、归档、删除、restore、retry、creation/status/command lookup、UI 或迁移；不启动 Project worker、Skills initializer、lifecycle runtime 或 Meeting consumer。生产 Resolution/Invocations 仍未绑定，D24 生成、compaction 与 Execution Summary 不变。fresh 环境允许合法空列表；本结果不解决该环境的项目创建。系统 Summary 未配置不成为 Project 读依赖，也不引入项目 Summary override 或默认值。

Object runtime join、OpenAI tools 独立验证、SPA 并发发布三停止保持；不以根装配验收重启其故障场景。ready503、完整 D08–D28/E01 未完成及 E01 未开始保持。

## 2. 窄 Reader 与既有 Service 兼容

在 `internal/central/project` 新增以下生产库接口；不修改 `project/contract.ProjectService`、Store、Authority、cursor 或共享 foundation 契约：

```go
func NewReader(store Store, authority *Authority, cursors cursor.Keyring) (*Reader, error)
func (*Reader) GetProject(context.Context, identity.Actor, contract.ProjectID) (contract.ProjectRef, error)
func (*Reader) ListOwnedProjects(context.Context, identity.Actor, contract.ListOwnedProjectsRequest, foundation.PageRequest) (foundation.Page[contract.ProjectListItem], error)
```

构造器纯、无 I/O；拒绝 nil/typed-nil Store、nil/零 Authority、Authority 所持 Store 与传入 Store 不满足原 `sameStore`、无效 cursor keyring。前述缺绑定沿 `DependencyUnbound`，无效 keyring 沿 `InvalidArgument`。相同 DSN 不等于同 Store；不替换真实 SessionAuthority，不新增可授权的公共 mock 接口。零 Reader 的调用明确拒绝，不空成功。

Reader 只持上述读取依赖，不要求 Activity、Audit、Events、ProcessAuthority、Initializer 或 LifecycleRegistry，不注册 ProjectInitialization service actor。无 Initialize/Start/Stop/Force/background loop；调用前检查 context，所有请求同步使用调用者 context，默认根由既有 HTTP admission/drain 拥有调用终局，不为 Reader 新造进程/资源 owner。

原 `Service.GetProject`、`Service.ListOwnedProjects` 保留签名和 `begin/done`、Stop/Drain/Force 语义；提取其现有私有查询逻辑供 Service 与 Reader 共用，不能复制第二套 SQL/权限/分页实现。合法结果、错误顺序、read cause、原 cursor namespace 均兼容；本卡明确补上所有候选行（含哨兵）的完整检查以及 Committed 后的取消检查。旧命令、`ResolveProjectPath`、Unknown 恢复逻辑不改。

### 2.1 当前授权与事务

- 库入口沿 `human`／`CheckOwnerActorKind`，不把任意合法 Actor 当作权限。HTTP 仅接受正式 `RequireHuman`；直接库调用的 AgentRun 仍 `DependencyUnbound`，其他未授权 kind 仍拒绝。
- 详情沿原 `readCause("get")`，同一 `WithinTx` 先完整 Acquire User SH 与目标 Project SH，再调用同 Authority 的 `RequireOwnerInTx(..., Read)`。当前 Session、Owner、初始化和 lifecycle gate、ProjectRef 投影必须在该事务内，不跨连接先查权限再读内容。
- 列表沿原 `readCause("list")`，同一事务持 User SH、调用当前 `RequireCurrentSession`，SQL 强制 `owner_user_id=当前 Human`。列表沿既有快照读语义，不新增每行 Project 锁，不宣称整页能冻结后续项目状态。每次续页重新检查当前 Session。
- 已初始化 active、archiving、archived 可读详情；本人未初始化或 deleting 返回 `ProjectNotActive`（409）。另一 Owner（含系统管理员）与不存在项目按原顺序 `NotFound`（404），普通详情不读 deletion receipt。列表只包含 `initialized_at IS NOT NULL`，所以 pending/failed 初始化不成为 ready 项；deleting 可作为 §3 的最小列表项出现。
- 不 TouchActivity，不写 Project/receipt/Audit/Event，不以管理员身份扩大列表或访问别人的详情。请求前置身份检查不能取代事务内当前授权；读先取得 SH 与撤销先取得 EX 的两种合法串行顺序均须验证。

### 2.2 分页、完整行与提交终局

复用 `ListOwnedProjectsRequest.NormalizedFilter`、`ValidateProjectPage`、`OwnedProjectsQueryDigest`。省略 lifecycle 即原四状态全集；排序是 `created_at DESC,id DESC`，SQL 继续使用原 Owner／initialized／filter／严格小于游标元组条件和 `LIMIT limit+1`。cursor 仍绑定 System scope、`owned-projects-v1` 的 Owner/filter/order digest，恰为 instant、UUID 两个 scalar、无 OrderGeneration；不绑定 Session 或 page limit。换同一 User 的有效 Session、合法改变 limit、重排等价 filter 可续页；换 Owner/filter、篡改 token 或错误 key/scalar/绑定拒绝。

先完整 scan、验证全部最多 `limit+1` 行，包括 `ProjectRef.Validate`、隐藏的 canonical IDs、当前 Owner、initialized、请求 filter、严格降序且无重复以及下述列表状态映射，再决定去掉哨兵；异常返回超过 `limit+1` 行也拒绝。archiving/deleting 必须有合法 operation；active/archived 即使数据库保留历史 operation pointer，也不输出该 pointer。不能因裁掉哨兵而漏过其错误、晚到 `rows.Err` 或查询失败；不能部分发布已验证前缀。`Rows.Close` 必须在事务返回前实际完成；既有无 error 返回的 Close 不伪造成功证明，实际终局仍以其返回及 `rows.Err`／事务结果核对。

只从最后一条可见行签发 next cursor；空页 `Items` 非 nil；不返回总数、不重解释当前页为全局统计。Reader 不做自动重试、额外事务或网络调用。

候选仅在 `WithinTx` 实际返回 Committed 且调用 context 仍有效后交出。原 `commitError` 的 NotCommitted/Unknown 映射及 `UnknownAttempt` 原 attempt/cause 保留；Unknown 返回零 ProjectRef/零 Page，不将它改成 NotCommitted。先保留非 Committed 原结果，Committed 后取消则丢弃候选并返回取消，不谎称数据库回滚。最终 HTTP 发布前还须检查自己的预算；显式 GET 重读是新读取，不是确认原事务或取得写 receipt。本卡不为 `retry_hint=lookup` 新造 lookup 路由。

## 3. HTTP、安全投影与 schema

新包 `projecthttp` 位于 `internal/central/project/http`，公开纯构造：`NewHTTPHandler(reader *project.Reader, boundary *account.HTTPBoundary) (http.Handler, error)`；另有 `HandlesPath(string) bool` 供默认根使用。handler 的受控 Reader/boundary 接缝仅包内私有。实际 root 必须传真实 Reader 和 `account.NewHTTPBoundary`，不能公开任意 caller-supplied grant。

匹配仅精确 `/api/v1/projects` 或其后恰好一个非空 segment，且 segment 不为 `resolve`；非法 UUID 单段仍由本 handler 安全返回 400。路径清洁、RawPath、encoded slash、反斜杠、NUL 等沿原 Account CheckRequest 拒绝，无重定向或二次 decode；trailing slash、nested 资源及非本资源交回原链。匹配资源只支持 GET/HEAD，其他方法 405 且 `Allow: GET, HEAD`；POST `/projects` 的 405 不代表创建已绑定。method、安全边界、RequestID 和日志沿原 Usage 模式，不增加第二层全局 middleware。

列表只接受 `lifecycle,limit,cursor`，详情不接受任何 query。沿已验 strict query 规则先限制 RawQuery 为 32768 bytes，再每键值仅解码一次；未知键、解码后重键、多值、空值、缺等号、空段、分号、坏 escape、非法 UTF-8/NUL、空 `?` 均拒绝。lifecycle 是单个逗号分隔值（例如 `active,archived`），1–4 个原闭集成员，禁止空项、重复或空白，顺序不影响集合语义；不接受重复 lifecycle 键。limit 缺省 50，1–100，无符号无前导零十进制；cursor 非空且解码后不超过 8192 bytes。cursor 过长或 token/绑定错误为 `CursorInvalid`，其他 query 错误 `InvalidArgument`；不回显原文。

GET/HEAD 不接受实体；拒绝声明非零/不确定 Content-Length、Transfer-Encoding，并在同预算内对实际 Body 做有界 EOF 检查，不能只信头。HEAD 完成同样认证、事务、完整 DTO 验证和编码；成功与 Problem 都无 body，Content-Length 按同 GET 表示，不能省略调用。

列表外壳精确 `{items,next_cursor}`，空 items 为 `[]`，无 cursor 为显式 null。每项按以下互斥字段集闭合；不直接 Marshal 含 `omitempty` 的内部 Page：

| lifecycle | 精确字段 |
| --- | --- |
| active / archived | `id,name,lifecycle,version,description`；description 为字符串，可空串；不出现 operation_id |
| archiving | 上述五字段加必需非空 `operation_id` |
| deleting | `id,name,lifecycle,version,operation_id`；不出现 description，不泄露 Owner/时间/其它旧内容 |

详情精确十一字段：`id,owner_user_id,name,normalized_name,description,lifecycle,version,current_sprint_id,created_at,updated_at,archived_at`。两个 nullable 字段显式 null；成功 lifecycle 仅 active/archiving/archived，完整原 ProjectRef 校验，并核 ID 等于目标、Owner 等于当前 Human。名称/规范名称、description UTF-8/控制字节/8192 B、正 version、UUIDv7、UTC 微秒 Instant 与 archived_at 状态/时间关系沿原 typed contract，不直接暴露 creation、operation、initialized、原始数据库或可信 Access 对象。

version 为 `"1"` 至 `"9223372036854775807"` 的正式十进制 JSON 字符串；禁止 JSON number、float64 中转、前导零、指数或超范围值。有效时间/ID 使用原规范 JSON。每行先验证后投影；隐藏字段损坏不因不输出而被忽略。无 SQL、错误 cause、Session、cookie、凭据进入 body/日志。

使用默认 JSON 转义，完整成功表示上限 **5 MiB（5242880 B）**，不边扫边写、不截断、不放宽字段。依据：100 条最大 description 的 6 倍 JSON 转义为 4915200 B；每项其余字段保守 1024 B，加 8192 B cursor 及 256 B 外壳共 5026048 B，小于上限。详情的 8192 B description 加有限其它字段小于 64 KiB。实现须实测最大合法默认转义页，并保留最终编码上限；推导不成立先报差量，不靠提高预算掩盖。

成功响应 200、`application/json`、`Cache-Control: no-store`、准确 Content-Length，安全头沿 Account 边界。错误的内部 Fault/UnknownAttempt 保留原 CauseID/attempt；HTTP 沿既有 `HTTPBoundary.WriteProblem` 和公共 Problem 映射保留 `code/commit_state/request_id/retry_hint`，固定安全 instance，不新增 `cause_id` 字段或头，也不修改公共 Problem/schema；日志只用安全模板或 unknown_route，不记录原 query、名称、cursor。校验或终局失败零成功候选；已部分写时按 §4 abort。

新 `api/openapi/project-owner.json` 为 OpenAPI 3.1，完整描述两路径四操作、query、共同 Problem、闭合 Page/三种列表项/详情与条件约束；复用只读 `common.json`。所有对象 `additionalProperties:false`；nullable/字段缺席/空串分别表达。十进制正整数须精确排除 MaxInt64 以上值，不能仅限制 19 位。HEAD 不声明成功/错误 body。标准 Draft 2020-12 解析器验证合法/非法代表及真实 HTTP 原 body，ECMA pattern 的尾换行/Unicode 不以 Go regexp 自证。

## 4. 两秒总预算、取消与实际 I/O

同已接受 Usage HTTP：在 Account CheckRequest/RequireHuman 前建立 `min(parent deadline, now+2s)`，同一个 context 贯穿身份、空实体检查、Reader、投影、编码、写与 Flush。Reader 不另开 2s，原 DB 更早期限不延期。真实 ResponseController read/write deadline、取消 callback、Body.Close、回调 join 与 keepalive deadline 清除采用该已验机制的本包实现；无公共 helper 扩权，能力不支持则安全 abort，不退化成无界读取。

返回前实际完成同步数据库工作、Rows.Close、Body.Close、Write/Flush 和已经启动的取消 callback，再清除该请求设置的 deadline。短写、Write/Flush/Close 失败、清 deadline 失败均有明确 abort 路径；失败后不再写第二个 Problem，不留迟到写。不能用 `ctx.Done`、select 返回、异步 cleanup 或 goroutine 已发送结果代替 actual join。受控非合作尾部须先放行并实际等待，真实网络通过 deadline/关闭解除阻塞；测试最外层实际进程终局仍负责兜底，不宣称任意不可合作组件都能被 context 强制终止。

自然 2s 和更早 parent 各做受控检查，覆盖预认证、实际 Body、数据库、编码前后、写/Flush/Close/callback 尾部；未终局前 handler 不返回，取消后无完整成功候选。真实 PG 的 1s lock_timeout 只证明更早数据库失败，不能替代自然 2s。native 取消竞争可为 Canceled 或 DeadlineExceeded，但须核实际期限、Done、elapsed、是否进入服务及完整关闭事实；受控直接 deadline 场景仍检查确定的 DeadlineExceeded。

## 5. 默认根与 S2 交接

新增局部 `app/project_read.go`，从现有 `createProjectUsage` 的 **同一 Project.Authority**、同 database 的 Project Store 和原 `cfg.CursorKeyring()` 构造 Reader；用同 Account core/PublicOrigin 构造正式 HTTPBoundary。依赖缺失即失败，不重新造 Project Authority、复制 Account SQL 或构造带假写依赖的完整 Project Service。

`account.go` 仅在原 Usage 读取装配已成功后调用上述窄构造，给现有组合 handler 外包精确 Project read dispatcher。原 Account/System Model/Summary（若 S2 已接受）/Outbound/Audit/Usage 链原样保留，Request/Context/URL/RequestID 不被改写，resolve 与 Usage 仍命中原 handler。构造失败沿原启动拥有者 cleanup，不引入新的启动、初始化、迁移、health/readiness 或关停算法；尤其不改 Secret→Model→已接受 Summary/Usage 的既有启动顺序。Reader 没有新 schema 初始化回调。

**共享文件门槛：** 本卡编写时仅按已接受 Usage/c210 固定来源设计，没有把 S2 活动 `app/account.go`、`project_usage.go`、`project_usage_test.go`、`model_test.go` 读成已验前置。实施前 root 必须确认 S2 完整接受与相关文件停写，提供实际已接受 commit/输入指纹并唯一交出本卡 `app/account.go` 写权；保留其已接受 S2 初始化和路由差量。其它三个文件只读、不在本卡写白名单。若 S2 尚未交接，可独立准备 Project/HTTP 私有源，不能编译/测试穿过活动共享包。

Go graph、纯测试、集成与动态 cmd 之前冻结实际传递依赖（含 S2 已接受部分或 root 明确批准的最小固定覆盖）；不能拿活动源码补闭包。固定旧副本仅证明该副本，不代替最终 S2+本卡共同根验收。scope/接口差异需报 root，不通过回退 S2、拷全树或放宽测试绕过。

## 6. 精确实施白名单与交付顺序

以下 **16 技术路径 + 1 最后文档路径**是后续实施范围，不是本轮写权。除现存 service.go、account.go、README 外均为新文件；实施者开工仍须复核无文件冲突。

| # | 路径 | 唯一职责 |
| --- | --- | --- |
| 1 | `internal/central/project/reader.go` | 窄 Reader、共用读取 helper、完整行与提交终局 |
| 2 | `internal/central/project/reader_test.go` | 构造/同 Store/库读取与旧 Service wrapper/行和哨兵/取消纯验证 |
| 3 | `internal/central/project/service.go` | 仅原 Get/List 提取到共用 helper，保留 Service 生命周期和旧命令 |
| 4 | `internal/central/project/http/handler.go` | 两资源、正式 Human 边界、2s 与真实尾部 |
| 5 | `internal/central/project/http/wire.go` | strict query、条件 DTO、精度与完整有界编码 |
| 6 | `internal/central/project/http/handler_test.go` | 不监听的受控权限/预算/终局检查 |
| 7 | `internal/central/project/http/wire_test.go` | query/投影/容量/schema 边界 |
| 8 | `internal/central/project/http/native_test.go` | 三个另授资源窗口的真实 TCP 顶层 |
| 9 | `api/openapi/project-owner.json` | 两资源正式闭合协议 |
| 10 | `internal/central/app/project_read.go` | 同实例 Reader/handler 构造及精确 dispatcher |
| 11 | `internal/central/app/project_read_test.go` | 无监听构造/缺依赖/同实例/原路由兼容 |
| 12 | `internal/central/app/account.go` | S2 交接后局部构造和 dispatcher 调用 |
| 13 | `tests/model/project_owner_read_http_fixture_test.go` | 本卡正式身份/Project/受控屏障和 PG 终局胶水 |
| 14 | `tests/model/project_owner_read_http_test.go` | 实际列表/详情/状态/分页/原 resolve/schema |
| 15 | `tests/model/project_owner_read_http_terminal_test.go` | 当前 Session/Owner、锁序、Unknown/取消 |
| 16 | `tests/model/project_owner_read_http_root_test.go` | 公开默认 app.Run 与真实路由组合 |
| 17 | `docs/development/backend/README.md` | 技术独立接受后由 root 另授的最后局部能力说明 |

不改 contract、Authority/repository、Account HTTPBoundary、旧 Usage 源/fixture、共享 testsupport、scripts、依赖锁、任何新旧迁移、全局日志/HTTP/启动 helper。必要范围缺口先报具体文件与原因，由 root 修订授权。测试可引用固定已接受的本包 helper；无法在自有胶水完整实现时不得私改旧 helper。

先完成 Reader/HTTP/schema 的完整候选与受控验证，再在共享路径交接后完成默认根和真实测试；分块冻结可便于独审，但不能把只有空 handler 或替代权限的阶段当可交付产品。最终 root 仅在完整读根、native/PG 和独立验收后决定提交；README 末件另授，台账/架构同步不在作者业务写权内。

## 7. 可执行验收与资源边界

### 7.1 离线与纯验证

检查本卡新库及原 Service 同输入等价；nil/typed-nil/不同 Store（含同 DSN）拒绝且构造零 I/O；直接 Actor kind 的原错误、User/Project 缺锁拒绝、当前授权不能被预认证绕过。分页含同 timestamp 的 ID tie-break、等价 filter、Session 更新、limit 改变、跨 Owner/filter/token/key/scalar 拒绝；验证空页、最后可见行与第 `limit+1` 行坏数据/operation/顺序/Owner、rows.Err/Close、Unknown/NotCommitted/Committed 后取消均零候选。

HTTP 纯组覆盖全部 query/路径/method/body/HEAD/安全 Problem，四 lifecycle 的条件字段及十一字段详情，版本 >2^53 与 MaxInt64、null/缺席区别、非法隐藏字段、默认转义最大 100 项页与超限安全拒绝。用固定已安装标准 schema 解析器验证合法与坏边界，不安装依赖、不调用网络。2s 自然到期和实际尾部检查不使用全成功/立即返回 fake 冒充取消证明。

固定 Go 1.27.1、离线 `GOTOOLCHAIN=local GOENV=off GOWORK=off GOPROXY=off GOSUMDB=off`、`-mod=readonly`、既有 `/workspace/go/pkg/mod` 与 root 指定已验 build cache。实际 graph 包括普通/race/integration、import/embed、任何 TestMain、完整 fixture 与动态 cmd 的全部真实输入；runtime `os.ReadFile`／schema 文件另列，不能误以为 Go overlay 会覆盖运行时读取。每条离线命令 45s，编译 binary 与运行分开；发现列表不运行 test body。普通/race pure、必要 race vet、两 cmd build 与精确集成列表按受影响闭包执行，图超出冻结输入先停并最小补齐，不机械复制源码/缓存。

### 7.2 新顶层与真实身份来源

native 三顶层固定为 `TestProjectOwnerReadHTTPNativeSlowBody`、`TestProjectOwnerReadHTTPNativeWriteAndClose`、`TestProjectOwnerReadHTTPNativeKeepAlive`。真实 socket 验证慢空实体、读/写期限、短写/关闭、EOF、HEAD、callback actual join 和同连接 deadline 清除；不把 httptest.ResponseRecorder 称为原生网络。native 包及命令各 45s，默认 pure selector 排除这三个顶层。

真实 PG 新三个顶层固定为：

| 顶层 | 必须观察的完整结果 |
| --- | --- |
| `TestModelProjectOwnerReadHTTPProjectionAndPaging` | 正式 Bootstrap/Login 与 Invitation/Redeem/Login 形成 admin、普通 Owner、另一 Owner；正式 Project.Create 加已验持久 Skills 测试 fixture 产生项目。读取非空分页、同 timestamp tie-break、filter/limit/cursor 兼容与串用拒绝、完整 wire/HEAD/标准 schema；正式 rename 后旧 resolve 404、新 resolve 与原稳定 ID 对齐。包含未初始化排除、archiving/archived/deleting 的准确状态投影。 |
| `TestModelProjectOwnerReadHTTPAuthorityAndTerminal` | 当前 Owner 与 admin 无跨 Owner 豁免；列表 User SH、详情 User SH+Project SH 的真实持有；正式 Logout/User EX 两种顺序及每页当前 Session；真实 PG 锁超时、取消尾部、Query/行/提交失败零候选。至少一份真实目标读 COMMIT 响应丢失导致的 Unknown，核原 attempt/cause 与 GET/HEAD 无成功内容。 |
| `TestModelProjectOwnerReadHTTPRootBinding` | 通过公开默认 app.Run 同 DB 启动、正式 Cookie 调用两资源 GET/HEAD，读取持久测试准备的非空项目；同连接 EOF、原 Account/System/S2（接受后）/resolve/Usage 路由兼容、写方法未绑定、ready503、正常关闭。无注入替代 root/handler/生产 initializer/Resolution/Facts。 |

只复用固定已接受的 `tests/model/project_usage_http_*`、Project configuration/Skill 等本包 helper，必要适配写在 #13–16。已有测试专属 Model/Usage canonical 表或测试 Resolution 只是 fixture 来源，不成为本卡生产前置，不要求另造 Invocation。新正向身份不可调用旧 SQL INSERT user/role 捷径；构造 Human Actor 不替代正式 Login 和同 Tx Session gate。项目 ready 的证据只到 Project.Create+持久 Skills fixture，不称 D10 真实技能对象初始化。

archiving/deleting 优先使用已接受的正式 lifecycle 接受端口。当前没有完整 runtime/Restore；需要 archived、特殊时间/version、pending 或损坏行的代表可在独占 DB 使用明确标注的最小 canonical 测试 fixture，保留完整映射/约束，并区分正式命令与 SQL 种子的来源。不得以这些种子声称真实归档/删除/恢复/初始化全链已验；不 SQL 造身份、撤权、改 Owner 冒充授权证据，不删 required participant 或配置成功 stop stub。

Unknown 的真实代表采用受控 PG 协议屏障，只在目标 Reader 已完成授权和全部查询的同事务最终 COMMIT 上 arm；原 COMMIT 被实际发送后丢失其终局响应，实际等待原连接/事务/代理结束。保留安全 attempt/cause、代理原始阶段及零候选事实；任意改写 `CommitResult` 只算受控投影检查，不算真实 Unknown。只读 Unknown 没有写 receipt，后继读取不能被说成原提交确认。

每个新 PG 顶层总计 120s **包含 Cleanup**，完整 fixture `-race -count=1 -p=1`、包 6m，实时 `-v` watchdog 从顶层 RUN 追踪至该顶层全部 Cleanup 完成后的 PASS/FAIL；每子调用期限不能冒充顶层预算，fixture/进程最外层的清理仍须实际结束。可按预算分轮串行，不为了测试矩阵任意加时。必要旧六顶层固定：`TestProjectB02OwnerSessionNamePathAndPaging`、`TestProjectB02OwnerPortRequiresCallerTransactionLocks`、`TestModelProjectUsageHTTPProjectionAndPath`、`TestModelProjectUsageHTTPRootBinding`、`TestModelSystemHTTPBoundaryAndStrictWire`、`TestModelSystemHTTPCurrentAdministrator`；先离线精确发现，缺名报告，不静默换 selector。

### 7.3 独立与实际终局

独立至少两组互补风险：A 检当前授权/不同 Owner、User/Project 真实锁序及缺锁目标、分页与坏哨兵、Unknown/取消；B 检默认根真实两资源、原静态 resolve/nested 路由、HEAD/keepalive、同输入标准 schema。不是仅重跑作者 assertions；可复用未变输入的已验旧能力，明确未重跑范围。既有 S2/S3 接受不自动证明本卡的共同根。

所有执行须 root 分别授权离线、native、唯一真实资源窗口；本规格不自动启动资源。native fresh 空间至少 2 GiB，PG 轮至少 5 GiB，完整已验 fixture 的七个精确资源、nonce/labels/全部 Mount、任务 PID/starttime、direct/adopted actual wait、输入前后哈希与两次 owned 资源/进程清零均记录。使用实际 subreaper driver，预算失败先终结并实际等待自有链再归因；不以 ctx 返回代替 join，不操作非任务容器/onboarding/资源，不失败自动重跑或续后继组。

daemon 侧 PID1 shim zombie 按每轮前后 PID/starttime 集合单列，既非 task-owned 又未 task wait，不纳入 owned 清零、不沿用心算总数或声称全机零残留。原失败、门禁/编译/driver 首红、输入/命令/env/raw/实际退出与清理保持；修复后只复测受影响项并按版本组合报告。真实 body 可保存经确认不含敏感数据的原字节/hash、method/path/status/Content-Type/run/源/schema；凭据、Cookie、私有 descriptor 只在 0600 临时材料中，按 owner cleanup，不进日志证据。

## 8. 来源、待交接与完成判定

- 正式业务/依赖：[开发计划 D08](../development-plan.md)、[当前台账](../agent-team/tasks.md)、[D08 主卡](d08-project-owner.md)、[D08 设计 §11/12](d08-project-owner-design.md)。D08 B01/B02 已提交 `199554b`／`6319d03`；[恢复后固定 B02 真实基线](../agent-team/d08-b02-recovery-baseline.md)证明原 Owner/分页/权限能力可运行，不证明本新 HTTP。
- 实际读取：[service.go](../../../internal/central/project/service.go) 的 Get/List/read cause/commitError，[authority.go](../../../internal/central/project/authority.go) 的 RequireOwnerInTx，[repository.go](../../../internal/central/project/repository.go) 的 scan/ownerProject，以及 [commands.go](../../../internal/central/project/contract/commands.go) 的 filter/list item/digest、[lifecycle.go](../../../internal/central/project/contract/lifecycle.go) 的 Read gate。这些是复用和明确补齐的范围，类型 Validate 不是权限。
- 正式边界：[Account HTTPBoundary](../../../internal/central/account/http_boundary.go)、[已验 Usage HTTP 规格](d09-project-usage-read-http.md)与[根验收](../agent-team/project-usage-read-http-root-verification.md)。原根在固定 c210 来源中使用同 Store/Account 的 Project.Authority；本卡只在该接缝增加 Reader，§5 要求最终接受 S2 后交接，不以未验活动源为结论。
- 未绑定能力：[D10 Skills](d10-skills-initialization.md)、[Project 领域绑定阻断](recovery-project-domain-bindings.md)、[R4 runtime 规格](recovery-d08-lifecycle-runtime.md)、[S3 Resolution 绑定边界](d09-system-meeting-summary-resolution.md)。本卡没有通过读口绕开这些前置。

当前没有需再向用户询问的产品含义；Reader、逗号 filter、HEAD、5 MiB 完整表示、2s 总预算与共享根交接都是本卡明确的工程决定。完成门槛是两个真实默认根资源及其权限/分页/状态/终局完整验证，作者与独立证据、最终 README 和 root 接受一致；此时仍不能标记 Project 创建、生命周期、UI、D24 或完整 D08/D09 完成。当前仅规格候选，等待独立 STATIC 和 root 后续实施授权。
