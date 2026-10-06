# D04 / D27：System 出站规则管理 HTTP

修订：rev1。状态：已独立静审通过并获主线程采纳；被审全文 SHA `8b3770b1900c52126a50975d0c452b38096a1e284770b539ae8a961b172a9a74`，技术§1–7 SHA `44515e212a996d1a1413022a38ae2b56297ad7e5cd9d195d732225aab818f8dc`。当前仅规格接受，十二条产品候选路径及真实资源均未授实施，另由主线程授权。固定接受基线 `819aba1b8f764328f1e2e67b53c274fad0db877d`；不消费活动 SMTP 前端或未接受候选。本结果仅交付正式出站规则 GET/PUT 与同实例 root 装配，不代表 D27 页面、完整 D08/D09/D27 或运行时调用已完成。

## 1. 结果、来源与已验前置

管理员通过正式 HTTP 读取当前完整出站规则，以 expected_version 和幂等键完整替换规则；保存真实更新 Central 既有受控客户端使用的同一个 PolicyService。复用已有事务、权限、版本、Audit、receipt、Unknown 与发送门禁，不重新实现策略引擎，不增加探测目标、网络代理、策略 Reload 或命令 lookup HTTP。

必读 [AGENTS](../../../AGENTS.md)、[团队流程](../agent-team/README.md)、[设计技能](../../../.agents/skills/agenteam-design/SKILL.md)、[D04 规格](d04-security-design.md)、[出站正式设计](../../architecture/platform-infrastructure/outbound-network-policy.md)、[后端接缝](../backend/outbound.md)与[系统设置 §6](../../frontend-design/layouts/system-settings.md#6-安全审计与平台配置)。下列源码均按固定基线读取，文档中的旧阶段描述不替代后续已接受绑定。

| 前置 | 已接受事实与本卡消费范围 |
| --- | --- |
| [D04 完成记录](d04-security-foundation.md#d04-完成与交接) | `outbound.PolicyService`、typed Rules、同 Tx Audit/receipt、进程内发送门禁、真实受控 HTTP/SMTP 及启动/关闭；00004 已在连续接受迁移1–19内。未变分类器、DNS/TLS/redirect/发送协议证据复用。 |
| [D07 后端](d07-account-session-smtp.md#当前进度) | 真实 Account Session/SystemAdmin、CSRF、登录/撤销与 `account.HTTPBoundary` 已接受；不是 D04 早期测试权限替身。 |
| 固定 root | `app/account.go` 构造同 Store 的 Account Authority、Audit 与 `transport`；`app/outbound.go` 的 `policy` 与 `client` 已在启动前构造，原启动阶段调用 Reload 后监听，真实 SMTP 捕获同一个 client。 |
| 固定 HTTP 基础 | `httpapi.DecodeJSON`、WriteJSON/Problem、单层 RequestID/Recover 与 Account 安全响应头已接受；`api/openapi/common.json` 只提供公共 schema。现无出站管理 operation，本卡新增独立文档，不修改 Account/Model operation。 |

D08 Project 领域绑定仍受 Artifact 最终验收与停止的 Object join 任务阻塞；本 System 结果不消费 Project/Owner、Object 停止修复、tools、Summary 或未绑定 Provider/MCP Runtime。没有新产品待决；HTTP 命名、预算及拆包是本卡冻结的工程选择。生产 ready503、既有未绑定责任与单 Central 镜像模型保持。

## 2. 正式 API 与闭合 wire

路径仅 `/api/v1/system/outbound-policy`，无参数化路径、分页、scope/user/actor 输入或查询参数。仅两个 operation，**GET/PUT-only**；HEAD/POST/PATCH/DELETE/OPTIONS 等返回405，精确 `Allow: GET, PUT`，HEAD 错误响应无 body。不借 ServeMux 的 GET→HEAD 回退扩口。

| Operation | 输入 | 成功 |
| --- | --- | --- |
| `getSystemOutboundPolicy` | GET；当前管理员；无 body、无 query；不要求写 CSRF 或幂等键 | 200 `OutboundPolicy`：恰 `{version,rules}`。version 为规范正 int64 十进制字符串；rules 为非 null 数组，空策略 `[]`。返回数据库当前完整观察，不表示进程镜像可用、发送成功或某命令已接受。 |
| `updateSystemOutboundPolicy` | PUT；当前管理员、合法 Origin/CSRF、唯一 `Idempotency-Key`；JSON 恰 `{expected_version,rules}`，两字段必需且非 null | 200 `OutboundPolicyUpdateResult`：恰 `{version,rule_count,audit_id,created_at}`，直接来自原 `UpdatePolicy`；不追加 GET。version 为命令接受时的正 int64 版本，首次更新为2；rule_count 为 `"0"`–`"256"`；audit_id 为 canonical UUIDv7；created_at 为 canonical UTC 微秒 Instant。 |

各对象 `additionalProperties:false`。GET 的 rule 恰 `{cidr,ports,allow_http}` 且三字段必需；PUT rule 的 cidr/ports 必需，allow_http 可省略并沿现引擎取 false，非 null 时只能 boolean；不因 UI 推荐显式选择而收紧现引擎输入。ports 为字面字符串 `"all"` 或1–256个不重复整数的数组，每项1–65535；数字不是十进制字符串，拒绝浮点、指数、负数、0、溢出、null/空数组。允许端口和规则输入未排序，由正式引擎归一化；不擅自把顺序当优先级。

使用 `outbound.DecodeRules` 作为唯一规则语义入口，原字段 JSON 直接传入：最多256条，原始 rules JSON 不超过512 KiB、原严格深度与全层重复键/尾随/null拒绝保持。CIDR 必须是 `netip` 的原规范字符串、网络位已掩码、非 IPv4-mapped IPv6，且完整处于正式 RFC1918/ULA 范围；不扩成任意 private/loopback/public 网段。NewRules 按 canonical rule JSON 排序，端口升序，完全相同的规范规则拒绝；不同但重叠的规则沿现引擎，不新增覆盖冲突规则。命中网段不自动允许 HTTP，固定禁止地址仍由分类器优先拒绝，SMTP 加密语义不变。

PUT 整个 UTF-8 JSON body 独立上限 **1 MiB**，保留 rules 字段自己的512 KiB上限，不能重编码后才检查原字段预算；外层空白和转义计入实际输入字节。只接受单个 application/json（可 charset=utf-8）、无 Content-Encoding；重复键、未知/缺失字段、非对象、非 UTF-8、第二 JSON 值均拒绝。外层超过1 MiB为413，rules 字段超过原引擎上限或非法规则为400；不改变全局 DecodeJSON 默认或其他 endpoint。规范 GET 包装与现合法 Rules 足以放入1 MiB响应界限；编码/范围检查失败不得输出局部规则。

幂等键沿 `foundation.IdempotencyKey`：唯一 header，1–128字节，仅 `[A-Za-z0-9._:/-]`，不 trim、拼接或接受重复值。服务端构造 `NewCommandIdentity("outbound-policy", []string{actor.UserID}, "update", key)`；owner、namespace、command 不从 body/URL 取得。expected_version 使用原 foundation.Version，拒绝 JSON number、前导零、0及溢出。transport RequestID 仅由现中间件产生，可作为 HTTPTraceID；它与 SessionID/CSRF 均不进入原业务幂等身份。

OpenAPI 新增 `api/openapi/outbound-policy.json`（3.1.0），只登记上述两 operation、Session/LocalSession、PUT CSRF/Idempotency-Key 及独立闭合输入/输出 schema；复用 common 的标量/Problem。输入与输出 Rule 分开表示 allow_http 的必需性。明确 body/规则数量与字节约束、规范化规则、GET/历史 receipt 差别、HEAD405无body和无 lookup；不改 common/account/model-system 文档或旧 route-count 断言。

## 3. HTTP 边界、预算与错误

新增 `internal/central/outbound/http` 子包，package 名 `outboundhttp`，提供：

```go
type SystemHTTPOptions struct { PublicOrigin string }
func NewSystemHTTPHandler(policy *outbound.PolicyService, accounts *account.Service, options SystemHTTPOptions) (http.Handler, error)
```

构造纯且不访问 SQL/网络、不 Reload、不启动工作；拒绝 nil policy 和非法 Account 边界。非 nil PolicyService 是不透明依赖，root 必须传正式构造成功的实例；不反射私有 state、试调方法或把 Status.Available 当构造/权限证明，也不宣称检测任意 opaque 零值。生产构造只接具体服务；包内窄接口可用于纯测试，不新增可变 setter、公开替身注册或通用 locator。

依次复用 `account.NewHTTPBoundary` / CheckRequest / RequireSystem(Read|Mutate) / WriteProblem。CheckRequest 在路径匹配、身份、body解码前校验 Host、Origin、Fetch-Metadata 和 clean Path/RawPath；不信转发头、不开放 CORS、不重定向清理路径。不自动修改请求 URL、方法、body、Host 或 Cookie。合法但非精确路径返回404；含重复斜线、点段、反斜线、NUL或 RawPath 的歧义路径按原边界400。根的完整路径前缀分派后仍由子边界检查，不能先 normalize。

精确合法 method/path 在预认证前建立 **GET最长3s、PUT最长30s**，继承更早 parent deadline；预算包含预认证、受限 body 读取/解码、门禁、事务、原 Unknown 核实及编码前检查，不能分段续期。任何 query（包括裸 `?`）拒绝。GET 拒绝非空 body、非零/未知 Content-Length 或 Transfer-Encoding；实际空体检查不得只相信伪造长度。PUT 的实际读取必须随原生连接 deadline/取消退出，不能仅取消 ctx 后留下阻塞 Read；可用 ResponseController 的局部读写期限及可停止/join 的取消回调，不修改全局服务器超时。底层不支持所需期限时在服务调用前安全拒绝，不无界降级；适用纯测试提供明确支持期限的 writer/body，原生 HTTP 测试另证实实际行为。退出前结束取消回调、清除本次连接期限，避免迟到回调污染后继请求。

所有服务调用、body读取、取消回调和事务实际尾部结束后才返回；不使用超时 goroutine 提前释放调用，也不补自动重试。GET 服务、验证或最后取消检查失败返回零规则/安全 Problem。PUT 不把取消等同回滚：未调用服务时可报 not_started；调用后保留原服务真实结果/错误分类，已经确认提交却不能送达时终止响应，不能改造为 not_started。成功 DTO 完整校验/编码及最后期限检查通过才提交响应；写出失败或已无法回包用 `http.ErrAbortHandler`，不追加第二份 Problem。已开始响应不承诺可撤回。

所有错误走原安全 projector：400 InvalidArgument，401 Unauthenticated/SessionRevoked并仅清选定 Session cookie，403 Forbidden/CSRFFailed/OriginDenied，404未知路径，405方法及Allow，409 VersionConflict/IdempotencyKeyReused/InvalidState，413/415传输输入，503 DependencyUnbound/DependencyUnavailable/CommitUnknown，未知内部错误500；root shutdown仍沿现门禁。缺少 policy singleton 是不可用，不返回默认空策略。只读事务 Unknown 也保留 Unknown，不能因拿到候选便成功。

**兼容提示**：当前公共 Problem 对 COMMIT_UNKNOWN 固定输出 `retry_hint=lookup`。保留该投影及 commit_state=unknown，OpenAPI 明示这是既有通用恢复提示，本域没有 lookup 路由；确认方式仍为原 PUT/key/body、当前合法 CSRF。不得据该字符串虚构端点、改所有 Problem 或把 GET 当 receipt。授权可见规则只在显式 DTO/response 中编码；不更改 Policy/Rules 的普通 fmt/JSON/slog 脱敏，日志/Problem 不含原规则、内网地址、请求正文、Cookie、CSRF、幂等键、SQL或嵌套 cause。响应保留 no-store/nosniff/no-referrer 和唯一 X-Request-ID。

## 4. 原事务、回执与镜像语义

直接调用固定 `PolicyService.GetPolicy/UpdatePolicy`，不在 HTTP 层开启第二业务 Tx、预取 policy 决定写权限、查询私表 receipt 或新增锁。GET 原 policy SH+User SH 同 Tx当前授权及读取；PUT 原进程独占 send gate、command EX+policy EX+User SH、同 Tx当前授权→原语义 receipt→expected_version→policy/Audit/receipt 原子写入，全部保持。HTTP 预认证不替代后者；原 context/actor/meta 必须完整下传。

原语义摘要包含稳定 User、expected_version 和 canonical Rules；不含 Session/trace。合法同用户新 Session 可在当前授权和新合法 CSRF 下重放原命令；其他用户即使复用 key 也是另一个 identity。规则/端口顺序改变但归一化语义相同不造新命令；同身份同 key 异义409。当前授权先于历史重放，失权不能取得旧 receipt。两个新 key 竞争同版本时只有一个新事实；成功每次版本+1、Audit/receipt各一次，不创建 Outbox 事件或邮件任务。最大版本拒绝沿原 InvalidState。

正常新提交后原 service 发布新镜像；已接受的旧 key 返回原 version/rule_count/AuditID/时间，**不发布旧规则、不恢复 unavailable 镜像**。GET 读取数据库也不 Reload。COMMIT Unknown 保留原 cause；原 service 只在同权限/锁核实 receipt 和当前 policy 均真实确认时发布当前镜像并返回原结果，核实不成则503 Unknown并保持镜像不可用。HTTP 不重写此流程、不制造“未提交”证明、不用后读失败推翻已发生写。后继 UI 必须分离当前观察与历史确认；本卡不实现 UI 控制器。

保存只约束后续新请求、重试/每跳与原首写门禁；不取消在途发送、不保证撤回外部副作用。HTTP 可见版本不是健康或真实 Provider/MCP/SMTP 投递成功证明。进程镜像恢复仍由正式启动/恢复端口负责，本卡不新开管理员 Reload、定时刷新或多 Central 缓存一致性协议。

## 5. root 装配与同实例证明

`app/account.go` 在现 Account/Model handlers 构造处，以已有 `transport.policy`、同一个 `core`、cfg.PublicOrigin 构造新 handler；原 policy/client 只构造一次。新 `systemOutboundPolicyRoutes(existing, policyHTTP)` 只把 `/api/v1/system/outbound-policy` 及其完整 `/` 子路径交新边界，其余请求原样交既有 `systemModelRoutes`。前缀近似路径不误归属；子路径只为新边界统一拒绝，不增加资源。固定 `app/model.go` 六根分派及旧 Account 路由保持。

新子包依赖 Account、outbound、foundation、httpapi/identity；Account 继续只依赖 outbound 父包，父包不导入子包，因而没有 account↔outbound 环。仅 app 导入新子包；不把 HTTPBoundary 搬入引擎，也不向 Account 增加 outbound setter。服务初始化、Reload、监听前门禁、HTTP admission/关闭及唯一外层 RequestID/Recover 保持；新 handler 不安装第二中间件或常驻任务。

真实 root 验收既检查构造图，又实际发出任务自有受控请求：捕获正式 root 的 `outboundRuntime`，所有规则修改经新 HTTP；空规则拒绝→精确允许 fixture 私网目标→请求到达→清空规则后新请求被拒且目标计数不增→旧 key 重放仍不能重新允许。可沿既有 `outboundConstruct` 测试接缝在 client 被消费者捕获前注入任务自有 DNS/CA，policy 指针及生产分类器不变；客户端保持正式 `outbound.NewClient`。不新增生产 `/egress`/测试发送口，不用另建 PolicyService 查询到同 DB 冒充同一镜像，不使用现有基础设施或真实收件箱。

## 6. 唯一候选路径与所有权

以下十二路径为完整结果候选，独立静审及主线程另授唯一 backend_worker 后才实施；当前架构写权只有本卡。按已提交最小依赖编译，不消费 SMTP 活动输入。

| # | 路径 | 限定用途 |
| --- | --- | --- |
| 1 | `internal/central/outbound/http/handler.go`（新） | 纯构造、精确两 operation、Account 边界、局部期限/实际尾部与服务调用。 |
| 2 | `internal/central/outbound/http/wire.go`（新） | 独立严格 DTO/输入、命令身份、安全完整响应投影；复用 DecodeRules。 |
| 3 | `internal/central/outbound/http/handler_test.go`（新） | 路由/边界/预算/取消与安全错误纯测试及有界原生 HTTP body 测试。 |
| 4 | `internal/central/outbound/http/wire_test.go`（新） | 最大合法规则、闭合形状、规范化/键/错误、OpenAPI双向及标量一致性。 |
| 5 | `internal/central/app/account.go` | 仅在原 handler 装配位置接已有 policy/core，原初始化/关闭不改。 |
| 6 | `internal/central/app/outbound_policy_http.go`（新） | 完整单根路由分派，保留原请求与既有 Account/Model 链。 |
| 7 | `internal/central/app/outbound_policy_http_test.go`（新） | 纯构造/路由归属、零提前I/O、单中间件及旧分派不变。 |
| 8 | `internal/central/app/outbound_policy_http_process_test.go`（新） | 正式 root、同实例真实请求切换、旧 receipt/不可用镜像组合；复用既有 app/网络 fixture。 |
| 9 | `api/openapi/outbound-policy.json`（新） | 两 operation 与本卡闭合 schema，引用 common，不添加不存在的 lookup/HEAD。 |
| 10 | `tests/account/outbound_policy_http_test.go`（新） | 复用 newHTTPFixture 的真实 root/admin/Session/CSRF、wire、保存/重放/冲突与日志。 |
| 11 | `tests/account/outbound_policy_http_transaction_test.go`（新） | 正式 Account Authority/PolicyService/PG 的预认证后撤权、同 Tx 锁与期限/尾部、回执/Audit安全组合。 |
| 12 | `docs/development/backend/outbound.md` | 产品接受前最后作本 HTTP/预算/历史回执及 root 使用边界的局部说明，不改旧规则。 |

不写父包 policy/rules/client/gate/classify、Account 核心/HTTPBoundary、公共 httpapi、旧 app/model.go、OpenAPI其他文档、迁移、锁文件、fixture/driver、前端、SMTP 卡、台账或档案。§5不需要改旧 root 实例构造；若实际必须增加公共 API、源码路径或变更旧语义，先报告主线程修卡，不临时扩大。共享 `app/account.go` 与后端说明需主线程确认没有并行作者；新测试只读取旧 fixture，不覆盖旧断言。开发时必读 [Go 技能](../../../.agents/skills/agenteam-go-development/SKILL.md)、[验证技能](../../../.agents/skills/agenteam-verification/SKILL.md)，局部说明沿[文档技能](../../../.agents/skills/agenteam-documentation/SKILL.md)。

## 7. 验收、资源与交付

| 验收组 | 必须证明的结果 |
| --- | --- |
| 纯 wire/规范化 | 空规则、256条且每条256个端口、all与显式HTTP、规范IPv4/ULA；整个外包装1 MiB与rules512 KiB独立边界。重复/未知/缺失/null、非规范CIDR、mapped/越界网段、端口数值形状及重复、canonical重复规则、最大版本和幂等键边界；规则/端口换序原语义相同。最大合法响应经正式编码完整输出，Rules普通日志/JSON仍脱敏。两 operation 与新 OpenAPI双向一致，旧两份OpenAPI断言原样通过。 |
| HTTP/权限/输入 | 原生 root 两口、空策略及新保存；匿名/普通用户/已撤销Session/错误CSRF/Origin/Host拒绝，GET不需写令牌；裸query、body、路径歧义、近似前缀/未知子路径、HEAD405/Allow及无body，全部安全headers。每请求只有一条正式RequestID链，普通日志与Problem不泄漏规则/凭据/key/cause。 |
| 有界调用/实际尾部 | 纯受控边界证明预认证即取得GET3s/PUT30s或更早parent，涵盖body/native deadline、发送门禁、服务与编码前；取消回调与实际Read/事务返回全部join，迟到结果零发布且不污染下个请求。真实PG固定LOCK_TIMEOUT=1s不改：锁等待证明min(parent,本地期限,DB1s)、安全typed错误及实际退出，不声称DB1s是HTTP3s/30s自然到期。提交后的取消/断响应不得返回not_started或创建替代命令。 |
| 真事务/重放/撤权 | 使用正式Account登录、邀请兑换普通用户、退出/撤销及Policy命令；策略/Audit/receipt只经正式写服务产生，SQL仅读断言与任务自有锁屏障。两个新key同版本竞争恰一成功，异义key409，旧expected新key409；同用户新Session合法原重放回原receipt且无重复Audit。预认证后发生正式Session撤销，再进policy事务必须拒绝；持User SH的已准入事务与撤销串行关系按原锁证明，不凭预授权放行。 |
| 当前镜像/旧历史 | root同实例完成§5实际目标计数验证。在任务自有policy EX锁屏障下，让正式维护Reload因短parent失败并实际退出，以形成原引擎unavailable；释放屏障后GET读取当前DB、旧key成功重放仍不可令Status.Available=true，受控新请求仍拒绝。最后正式维护Reload才恢复当前版本；不提供HTTP恢复口、不破坏policy行或伪造receipt。真实操作数及历史receipt四字段精确核对。 |
| Unknown与兼容 | 纯测试将原typed CommitUnknown安全投影为503/unknown/兼容lookup hint，无候选规则/receipt；不得暴露cause。真实已提交响应截断后用原HTTP key/body和合法CSRF确认唯一receipt，后续新命令版本不被历史重放覆盖。数据库COMMIT两方向及核实失败沿未变D04 `TestOutboundPolicyRealCommitUnknown` 接受证据复用，HTTP截断不冒充DB Unknown；当前权限、旧Account/Model配置/SMTP读取与ready503保持。 |

新真实顶层固定为 `TestSystemOutboundPolicyHTTP`、`TestSystemOutboundPolicyHTTPTransactionAndBudget`（tests/account）与 `TestSystemOutboundPolicyHTTPRootSharedInstance`（app）；根实例组复用已验 `newModelRootApp`/正式 bindAccounts、任务自有 PG/MinIO 与 outbound fixture，不启动被停止任务或其探针。纯测试的替身不作为实际权限/持久化/网络证据；System入口正常关闭沿已验 root，不能把本卡称 Object join 修复。

固定 Go1.27.1；正式检查 `GOTOOLCHAIN=local GOPROXY=off GOSUMDB=off GOFLAGS=-mod=readonly`，准备阶段与检查分离。运行新子包及 account/outbound/app 受影响纯测、race/vet，受影响 integration 编译/vet、Central/Runner构建；复用原 Model route 与 Account OpenAPI回归，不写镜像实现式测试。真实独占窗口由主线程另移交，按 `scripts/test-security.sh -run` 选择上述三新顶层与原 `TestOutboundPolicyDurabilityReplayAndAuthorization`、`TestOutboundPolicyRollbackAndUnavailableReplay` 必要回归；检查每个预期包实际命中，skip/无测试不算通过。保留现有内部期限与 `-race -count=1 -timeout=6m`，单个新真实场景2m内、普通受控用例45s内；按组分批，失败不扩大预算或机械重跑全D04/D07。

作者冻结十二路径和最小固定编译闭包，提交全文/输入SHA、命令与实际退出/原日志、首红、复用与未验边界；独立验证重点复核当前权限、原身份重放/Unknown、最大规则、取消实际尾部及同实例生效。自有fixture exact IDs、所属进程与adopted wait实际终结并双次确认清零后才交资源；未验SMTP UI、未来出站页面/系统Audit及完整D27不纳入接受。当前只有规格静态工作，无产品、PG/网络或浏览器执行。
