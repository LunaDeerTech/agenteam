# D09 Project Owner Usage 只读 HTTP 与根装配

修订：rev1。状态：规格已独立完整 STATIC PASS；阶段 A 五路径已获独立限定 PASS、主线程采纳并提交推送 `ba7ce7296b7c08d976d3fc3ce1e1d0b727a3f04f`，见[阶段 A 验证归档](../agent-team/project-usage-read-http-stage-a-verification.md)。作者 B 已实际 ACK，仅 handler 两新源；原失败及 a04 未 join 限制保留，真实 HTTP/root/PG/native 与整卡产品尚未验收。技术 §1–7 保持原字节，后续阶段以任务台账和各自固定证据为准。
固定产品基线：`4ae02e5c58f4efc3b360706cb2622df05a96cdf0`。
已接受依赖：Usage ledger `36e5ff1a124c957d200888ad2e40bdc4ecb01305`、Project Owner/Store `6319d03`、Project 配置 `de00c610da62cb77cc03efe7c3cc842cf81f1ba5`；实际消费均以固定基线原字节为准。
依据：[D09 §8/11](d09-model-system-token-usage-design.md)、[D08 路径、Owner 与 HTTP 契约](d08-project-owner-design.md)、[Usage ledger 工作卡](recovery-d09-invocation-usage-ledger.md)。只补下述读边界，不重述或替换其内部契约。
来源和独立静审原件见[规格归档](../agent-team/project-usage-read-http-spec-verification.md)。原 rev0.1 技术 §1–7 原字节保持，SHA `36a7fa5c64cbb8d6716f87851b2b5eea895d344319944f6d558c37c55f53a11d`。

## 1. 完整结果与边界

交付三个正式资源，每个支持 GET 和 HEAD：

| 资源 | 行为 |
| --- | --- |
| `/api/v1/projects/resolve?username=…&project_name=…` | 正式 `Project.Authority.ResolveProjectPath`，解析当前名称并返回当前 Owner 可读 ProjectRef 的安全投影 |
| `/api/v1/projects/{id}/model-usage` | 正式 `Usage.Service.List`，读取该稳定 ProjectID 的 Invocation 历史 |
| `/api/v1/projects/{id}/model-usage/summary` | 正式 `Usage.Service.Aggregate`，按已接受八种维度统计；不是 Execution Summary 的生成、读取或重建 |

浏览器可读地址按 D08 先调用 resolve，再使用稳定 ID API；本卡不实现浏览器。旧 username/project name 的 resolve 返回 404，无 alias、重定向或旧名自动跳转。名称复用可能得到另一 ProjectID，不等于恢复原项目身份。
稳定 ID 读不携带原名称，不另增加路径 fence；改名后同一 ID 仍可在每次当前 Owner 授权通过时读取。不得把 resolve 的独立事务当作后续 Usage 的授权；Usage 每次自行完成同事务当前 Session、Owner、项目状态门禁。
不改 Project/Usage 现有公开签名或 Reader 接口；不新增 username 路径体系。随卡只对 D09 §11 作上述职责澄清，其他设计正文保持原字节。
只绑定 Reader：生产 `Invocations` 为真正的 nil。无 Facts 写口、consumer、Runtime/InvocationID 生成、Provider 请求或 System admin 的跨 Owner 豁免。
无 Project 创建、修改、初始化服务、生命周期执行者、迁移、UI 或 Execution Summary 选择；不声称完整 D08/D09。Object join、OpenAI tools、SPA publication 三项停止持续有效。

## 2. 当前身份、数据来源与事务终局

Account 仅增加 `HTTPBoundary.RequireHuman(*http.Request) (identity.Actor, error)`：复用原 `check`、`csrfSession` 与原安全/非安全方法分类，验证返回 Human Actor；不复制 Cookie/CSRF/Origin 规则，不调用 System 授权，也不把认证结果当成 Project grant。
原 `RequireSystem` 的意图校验、调用顺序、授权及错误行为保持；原 System 读写回归是门槛。新方法不接受客户端 Actor、UserID、SessionID 或替代 bearer 身份。
List/Aggregate 原 Authorizations、Store、Query 均不改：同一实际 Tx 取得 User SH 和 Project SH，验证当前 Session，再由正式 Project Authority 验证当前 Owner/状态；grant 必须精确匹配 Actor 和 ProjectID。
resolve 使用正式 Account CurrentUserRoutes、Project 当前名称重读与现有锁内复验/有限重试。调用者不得用事先查得的名称、角色、项目行或旧 grant 替代这些步骤。
active、archiving、archived 的已初始化项目按原 Read gate 可读；未初始化、deleting、其他 Owner/缺失等沿原 typed error。admin 读取他人项目仍按原 Owner 隔离规则拒绝。
完整服务结果只有正常 `Committed` 且请求 context 仍有效才可投影/发布；Unknown、回滚、取消、Rows/Close/游标签发错误均为零候选。HTTP 不经会丢掉 Unknown 的旧映射重包装；保留既有 Fault code/state 和安全 Problem 语义。
List 必须由原库严格检查全部读取行，包括 `limit+1` 哨兵；Aggregate 复用原完整统计与校验，不能在 HTTP 内重算、截断总量或以局部页推断总量。
游标仍只绑定原稳定 Actor、Project、过滤和排序等契约，不新增 SessionID、当前角色、limit 或原名称绑定；不同资源/Project/过滤游标不得混用。
生产可读来源仅是数据库中通过既有正式契约持久化的 Project 与 Usage；本卡不保证新部署已有项目或非空 Usage。缺少生产 Project 初始化/Runtime Facts producer 不用伪初始化、直接写账本或假 Facts 填补。
真实测试允许既有严格 Runtime Facts fixture、正式 Project 创建与受控持久 Skills fixture 作为测试准备；它们不能装入生产 root，也不能把其存在宣称为生产生命周期/consumer 已交付。

## 3. HTTP 查询与安全投影

先沿原 HTTPBoundary 校验 Host/Origin/Fetch Metadata/路径；不为新资源改变原拒绝优先级。确切资源的其他方法为 405，`Allow: GET, HEAD`；无自动重定向。无关路由维持旧 dispatcher 行为。
`{id}` 是 canonical UUIDv7。resolve 优先于 ID 解析；username/project_name 只解码一次并交正式 Project 路径规范化，保留原 admin 名称兼容，拒绝斜杠、反斜杠、残留 `%`、空段及点路径等原禁止值。
原始 query 最多 32768 bytes，在通用解析分配前限界；严格拒绝未知键、解码后重复键、多值、显式空值、非法转义/UTF-8/NUL、空参数段和分号。裸 `?` 不视作合法有值查询。
resolve 仅两个必填 query 键 `username`、`project_name`；不接受 ProjectID、分页或 Usage 过滤。
List 仅 `consumer_kind, agent_id, execution_id, meeting_id, purpose, provider_id, model_id, status, from, to, limit, cursor`；Aggregate 相同，另有必填 `group_by`。
`group_by` 完整闭集为 `consumer, agent, model, provider, execution, meeting, purpose, day`；不添加多个 group 或自由 SQL 表达式。
limit 缺省 50，范围 1–100，只接受无符号、无前导零的十进制；`from/to` 同时缺省或同时给出，沿正式 Instant parser 和 `from < to`，缺省原 30 日窗口。
UUID、consumer/purpose/status 与 MeetingID 逐项沿原 typed constructor/Filter.Validate；`status` 是原终态过滤，不造 dispatch 过滤。不得在 HTTP 重解释历史过滤语义。
cursor 非空且解码后最多 8192 bytes；超限及 token/绑定错误沿 `CursorInvalid`，一般 query 语法错误沿 `InvalidArgument`。不可把 token 原文写入错误、日志或游标失效提示。
GET/HEAD 不接受请求实体；已声明非零/不确定长度或 Transfer-Encoding 的实体拒绝，实际 Body 的防御性有界读取也计入 §4，总量不随 Content-Length 信任扩大。
HEAD 完成与 GET 相同认证、查询、完整投影与编码检查，响应头/Content-Length 对应 GET 表示，但成功和 Problem 均不写响应 body；不能用 HEAD 省略当前权限或查询。
所有成功对象闭合，空列表为 `[]`，没有 cursor 为显式 `null`。不直接 Marshal 内部 Invocation/ModelError，不返回未知字段、SQL、错误 cause 或运行凭据。

List 外壳为 `{items, next_cursor}`。每条 Invocation 只含以下字段，所有字段 required，原可缺省关联使用 null：

```text
id, call_id, attempt_index, project_id, consumer_kind, purpose,
agent_id, execution_id, meeting_id,
provider_id, model_id, provider_name, model_name, provider_model_id,
protocol, model_type, live_provider_id, live_model_id,
dispatch, started_at, dispatched_at, final, usage, provider_request_id
```

`provider_id/model_id` 与名称/原生型号来自原冻结 Identity；`live_*` 保留原 nullable 当前引用。不能用当前配置覆盖历史名称或删除历史行。
`final` 为 null 或 `{status, finished_at, error_category}`；`error_category` 为原安全类别或 null。`usage` 为原六个 nullable TokenCount 加 `source`，不推算 total，不把 unknown 改成 0。
`provider_request_id` 仅正式 safe ID 或 null。排除内部 ProcessID、Fence、SnapshotID、Consumer.OperationID、Profile、AdapterRevision、原错误 code/cause、提示词/内容、endpoint 和凭据。
投影前仍完整 `Invocation.Validate`，包括不外露字段；再验证每条 ProjectID 等于请求及页大小。隐藏字段损坏不能因不输出而被默许。
历史 Protocol 完整保留五种正式值及其 ModelType 关系；ConsumerKind 五种、Purpose 十二种、dispatch 五种、final status 四种、安全 error category 十四种均按固定 typed contract 建闭集/条件 schema。只读历史不因某 wire 尚未实现被裁剪。
Aggregate 外壳为 `{items, next_cursor, as_of}`，行 `{key, summary}`；key 为 `{by, id, day}`，day 组仅 day 非空，其他组 day 为 null，允许空关联的组按原契约保留 null ID。
summary 保留原六种计数、六种 `{sum, known_count, unknown_count}` 和 `as_of`；行与页 AsOf 一致。只验证并投影原 Aggregate，不重新定义成功计数/confirmed 或 token 分母。
所有 TokenCount、计数、attempt_index、Project version 均用正式无损十进制 JSON 字符串，范围 0…MaxInt64 或其正式更窄正值范围；禁止 float64 中转、科学记数和 >2^53 截断。
resolve 投影原 ProjectRef 的十一字段：`id, owner_user_id, name, normalized_name, description, lifecycle, version, current_sprint_id, created_at, updated_at, archived_at`；两个可空字段显式 null，完整原校验且 Owner 等于当前 Human。
时间沿原 Instant 的规范 JSON；null、已知字符串 `"0"`、缺少 final 是不同事实。未配置数据、空页和未完成/unknown 行不能伪装成成功调用或零 token。
三种成功表示使用默认 JSON 转义，完整编码后最大 1 MiB，超限零成功候选并安全失败；不得边扫边写、静默截断或放宽内部字段。
按固定字段上界，List 保守每行 5376 B（两名称 1536、原生型号 1536、安全 request ID 256、其余闭合字段 2048），100 行加 8192 B cursor 和 256 B 外壳共 546048 B；实现以最大合法值实测此推导并始终保留最终 1 MiB 检查。
Aggregate 每行保守 2048 B，resolve 含最多 8192 B 描述的默认转义仍小于 64 KiB；若实际字段使上述保守推导不成立，先报告规格差异，不能凭估计绕过编码门槛。
新 `api/openapi/project-usage.json` 完整描述三个资源 GET/HEAD、query、闭合 DTO、nullable/字符串整数精确范围、上述关联与共用 Problem；`additionalProperties:false`，不把仅 19 位数字当作 MaxInt64 校验。
schema 以实际标准解析器验证合法全集代表和非法边界；ECMA pattern 终止符与 Unicode 不能只用 Go regexp 自证。HEAD 文档不声明成功/错误响应 body。

## 4. 两秒总预算与实际 I/O 终局

每请求在 HTTPBoundary/预认证之前建立 `min(parent deadline, now+2s)`，同一个 context 传到 RequireHuman、resolve/List/Aggregate 和投影；原 Usage 内层 2s 只能继承更早界，不能重新获得完整时段。
沿已接受管理读 HTTP 的局部预算机制：真实 ResponseController read/write deadline、取消 callback、受控 Body 读取、完整写入与 Flush；能力不支持必须安全 abort，不悄悄降级无界响应。
返回前必须实际完成本请求同步工作、Body.Close、写/Flush 及已启动取消 callback 的 join，清理 keepalive deadline 后才退出；不以 context 到期、goroutine select 超时或异步清理代替完成。
处理短写、Write/Flush/Close 错误与 deadline 清除失败；已部分写或无法再写安全 Problem 时 abort。正常/错误/HEAD/取消路径均不遗留迟到写与持有的资源。
无自发 probe、轮询、重试、异步数据库查询或新后台 owner。公共库原有 resolve 有限冲突重试不改；HTTP 不外加重试。
边界错误用原 HTTPBoundary 安全 Problem/RequestID，一次全局 request ID/Recover，固定安全 instance；日志 route 使用既有安全模板或 unknown_route，不把原 query、名称、cursor 写入 Pattern/instance。
Unknown 保持公共映射与 CommitState，不虚构 receipt 或 lookup API；显式重读是一次新读，不能证明原事务结果。
受控纯测试实际等待自然 2s 与较早 parent，分别证明 deadline 夹界、Done、尾部放行前未返回、放行后实际 join/零候选；真实 DB 1s lock_timeout 只证明更早失败，不能当作本地 2s 自然到期。
原生网络取消可能竞争得到 Canceled 或 DeadlineExceeded：核实际期限、Done 与 elapsed/服务未进入/Close 次数等强事实，不要求不稳定的唯一取消来源；纯直接 context 到期仍严格检查 DeadlineExceeded。

## 5. 真实根装配与现有行为

在现有 account root 装配中，用同 Account Store、Authority 构造 Project.Authority（Sessions、Routes 为同 Authority），继而构造 Usage.Authority（Sessions、Projects、真正 nil Invocations）及 Usage.Service；沿同已载入 cursor keyring。
所有构造无 I/O。新 handler 持有上述同实例与正式 Account.HTTPBoundary；受控测试接缝私有，不新造公共 Project/Usage authority 接口或替代权限实现。
仅在既有 secret/models 启动初始化回调中，沿同 startup context 后接一次 `Usage.Initialize` 检查已有表结构；不新建表/默认行/Project、不启动 worker，不另建超时或生命周期。
新增 `app/project_usage.go` 保持局部装配/路由职责；既有 `account.go` 只接构造、初始化和三个精确资源的 dispatcher。现 account/model/outbound/audit 等路由、同 auditor/core、API middleware 和未匹配路径保持。
已取得 listener 后构造或初始化失败仍走原拥有者 cleanup；不改 `resources.go`、`health.go`、全局 root 清理算法或 readiness，不为此新建资源 owner。
生产 root 的 Usage 写/Confirm 能力继续因 Invocations 未绑定而拒绝；读取既有真实记录与未来 consumer/Facts 写入是不同交付。
fresh 环境缺项目/初始化/账本时按正式 404/状态/空集合返回，不通过测试专用 ProjectSkill/Facts 实现在 root 内制造可读数据。

## 6. 精确实施范围与阶段

除本卡和独立交付的 D09 §11 澄清外，产品实施白名单共 15 路径；没有迁移、锁文件、既有 fixture/helper 或脚本改动：

| # | 路径 | 最小职责 |
| --- | --- | --- |
| 1 | `internal/central/account/http_boundary.go` | 追加复用 csrfSession 的 RequireHuman |
| 2 | `internal/central/account/http_boundary_test.go` | 新 Human 边界及旧 System 保持 |
| 3 | `internal/central/usage/http/handler.go` | 三资源、预认证预算与实际 I/O 终局 |
| 4 | `internal/central/usage/http/wire.go` | strict query、安全 DTO、有限完整编码 |
| 5 | `internal/central/usage/http/handler_test.go` | 受控 handler/尾部与另行授权的 native 测试 |
| 6 | `internal/central/usage/http/wire_test.go` | 全闭集、精度、容量与 schema |
| 7 | `api/openapi/project-usage.json` | 三资源六操作的正式契约 |
| 8 | `internal/central/app/project_usage.go` | 同实例只读服务/handler 局部装配 |
| 9 | `internal/central/app/project_usage_test.go` | 无监听的构造/dispatcher/初始化接线 |
| 10 | `internal/central/app/account.go` | 原 root 装配最小调用 |
| 11 | `tests/model/project_usage_http_fixture_test.go` | 新卡自有正式 Account/Project 与严格 Facts 准备胶水 |
| 12 | `tests/model/project_usage_http_test.go` | Query/投影/名称解析真实顶层 |
| 13 | `tests/model/project_usage_http_terminal_test.go` | 当前权限/事务终局真实顶层 |
| 14 | `tests/model/project_usage_http_root_test.go` | 默认 app.Run 实际根装配真实顶层 |
| 15 | `docs/development/backend/README.md` | 产品独立接受后最后说明能力与限制 |

A：先固定 #1/2/4/6/7 的认证、查询/投影/schema 纯阶段；保持已有 Account 行为，交独立有限审查。实现所需私有拆分可在这些路径内完成，不为阶段造 stub 成功。
B：#3/5 完成 handler、2s/受控实际尾部，native 仅编写/编译/发现；固定输入独立检验后再进入 root。
C：#8–14 的同实例/root 与真实 harness 离线准备，冻结所有 14 源及实际执行闭包/driver；不得因 `-run` 过滤而遗漏 TestMain 的动态 build 目标。
D：根另授每个唯一资源窗口执行 native、新三组、必要旧回归及独立代表；实际终局后才移窗。#15 仅在产品独立接受后写。各阶段不把局部 PASS 冒成整卡接受。
若任一必要变更超出白名单、需要改 Project/Usage 契约或旧 fixture，先报精确缺口并重新裁定；不得复制权限/存储框架、SQL 改角色或用 stub 绕过未交付依赖。

## 7. 验证与证据边界

纯代表包括：RequireHuman/原 System；两类过滤与全部 group；超长/重复 query、cursor scope/身份；所有合法历史枚举与条件；>2^53/MaxInt64/null/0；完整字段损坏与末行/哨兵失败；最大默认 JSON 转义/容量；HEAD；预认证自然 2s/更早 parent、Unknown 与所有实际尾部。
旧 library public query/authority 原样复用，不以新增纯 mock 替代其既有真实行/锁/游标证据；不要求为本卡重复全部已验 ledger 矩阵。
native 精确顶层定为 `TestProjectUsageHTTPNativeSlowBody`、`TestProjectUsageHTTPNativeWriteAndClose`、`TestProjectUsageHTTPNativeKeepAlive`；循环连接实际 EOF/写deadline/Close/callback join，45s 包预算，默认纯组明确排除它们。
真实新三顶层定为 `TestModelProjectUsageHTTPProjectionAndPath`、`TestModelProjectUsageHTTPAuthorityAndTerminal`、`TestModelProjectUsageHTTPRootBinding`，每顶层 2m；同固定脚本完整 fixture、`-race -count=1 -p=1`、包 6m，不扩原预算。
ProjectionAndPath：正式 bootstrap/login/邀请普通用户、正式 Project 创建与既有持久 Skills fixture、strict Facts 与 exact wire actual Joined 后产生的非空正式 Usage；核 list/八种统计代表/HEAD/无损数与闭合 schema。
同组用正式 Project/Account 改名操作证明旧 resolve 404、新 resolve 当前值、原稳定 ID 仍由当前 Owner 读取；名称复用产生不同 ID，不能复用原 Project cursor。不得 SQL 伪造 role、名称或 Usage 行。
AuthorityAndTerminal：真实 PG User SH/Project SH 与正式 Logout/User EX 的前后顺序、另一普通 Owner 及 admin 无跨 Owner 豁免；只读 SQL/锁屏障观察，当前授权与完整事务终局后才发布，错误/Unknown 零候选。
该组受控 writer 只证明 handler 调用/发布边界；不得声称它证明 native TCP 提交前零字节。自然 HTTP 期限、较早 DB 超时与更早 parent 各自记录，屏障失败路径也必须释放并实际 join。
RootBinding：通过公开默认 `app.Run` 启动真实根，同 Account/Store/Project/Usage 实例读取测试准备的持久项目/非空 Usage；不注入替代 handler，不给生产 root 注入 Skills/Facts/lifecycle。
该组验证同实际连接的 GET/HEAD EOF、原 Account/System 路由兼容、原 readiness 语义及正常 root 关闭；不做 MinIO 故障/Object join 或其它停止项试验。
必要旧顶层：`TestUsageReaderNonemptyPagination`、`TestUsageInvocationWireLedger`、`TestModelSystemHTTPBoundaryAndStrictWire`、`TestModelSystemHTTPCurrentAdministrator`、`TestProjectB02OwnerSessionNamePathAndPaging`、`TestProjectB02OwnerPortRequiresCallerTransactionLocks`。先离线精确发现；缺名须报告，不默默改 selector 掩盖。
新测试只复用固定已接受 helper；正式身份不能借用旧 fixture 的直接 INSERT user/role 捷径。测试专用 Runtime tables/Skills fixtures 与生产读能力的来源限制分别记录。
独立至少两组合：一组真实 PG/正式权限+受控终局与非空安全投影；一组默认 root/native 路由+名称变化/ID 身份边界。不得只复跑作者 assertions 代替独立风险判断。
执行前冻结最小实际 Go/import/embed/脚本/helper/TestMain 动态 targets/tool 图，不复制全仓或缓存；固定现有 Go/readonly modcache，离线每命令 45s，编译/发现不运行业务或隐式监听测试。
真实采用现有完整对象/PG fixture；自有 nonce 和 exact IDs、原有资源完整 labels/Mount canonical 基线、PID/starttime/direct wait/adopted wait、输入前后哈希及两次资源/进程清零。native fresh 空间至少 2 GiB，PG 轮至少 5 GiB；每次实际运行另获唯一窗口。
所有首红、前置门禁失败、输入版本、命令/env/raw/实际退出及清理保留；失败先实际收尾，再裁定，不自动重跑或加预算。通过可按未变源码精确复用，报告按真实轮次/版本组合，不虚构同轮全绿。
文档只说明已实现三资源、生产数据来源、当前 Owner/只读边界和真实验证范围；不声称 Project 生命周期、生产 Runtime Facts、Project UI、Summary 选择或完整 D09 已交付。
