# Audit 与签名 cursor

`internal/central/audit` 消费 D03 Store/Tx，`audit/contract` 只依赖基础类型与最小 `identity/contract`。`00002_audit.sql` 在同一全局迁移序列增加 `agenteam_audit.audit_records`。本库不创建 User/Session/Project 事实；D07 绑定当前 Session/System 权限，D08 负责 Owner/gate/删除 cause。现有通用库语义保持，System 管理 HTTP 与 Project Owner Audit GET/HEAD 均已装配，具体边界见下文。未绑定端口返回 `DEPENDENCY_UNBOUND`。规格见 [D04 B01](../work-items/d04-security-design.md#4-b01audit-与签名-cursor)。

## 写入与未知提交

`contract.NewEntry` 仅接受 typed scope、Actor、action/outcome、resource、metadata 和规范关联 ID。客户端不能指定 AuditID/created_at。`AppendInTx(ctx,tx,entry,key)` 使用调用方 Tx；业务 mutation、Audit 和业务 receipt 由外层原子提交。返回内存 receipt 不证明外层 commit。

`AppendKey` 由 producer、稳定 cause_ref、非负 ordinal 组成。cause_ref 只能是 UUIDv7 或规范 SHA256；`audit.CommandAppendKey` 对完整规范 command identity 求 digest，不存原始幂等键。数据库按 scope/key 唯一；同义重放保留第一条 ID、时间和诊断关联，异义为 `IDEMPOTENCY_KEY_REUSED`。

`SemanticDigest` 使用 canonical-v1。Human 只绑定 user_id，排除 session_id；Agent 绑定 project/agent/execution；Service 绑定注册职责与 cause。HTTPTraceID 不参与，代表实际 Runner/Tool attempt 的 RequestID 参与，因此不同实际 attempt 使用不同 AppendKey。资源、typed metadata、其他稳定关联参与摘要。

外层 `unknown` 使用正式 `LookupAppend` 核实：先验证当前 Session 与 scope 权限，再读取最小 receipt。Service 必须匹配固定角色、producer、scope 与 cause，不能借此浏览记录。`not_observed` 仅表示这次读取没看见，不能证明旧事务已经结束或没有提交；读取失败保持未知。跨 Project、失效 Session、已非 Owner 的系统管理员都不能取旧 receipt。

错误只隐式输出固定 code/reason。Entry、AppendKey、Actor、密钥和原始 cause 使用不可反射遍历内容的私有存储；显式 `Fields/Details/Unwrap` 是受信调用边界，不可写入普通日志。

## 固定 metadata

没有自由文本、URL、Prompt、密钥或 `map[string]any` 入口。各构造器约束版本/数量/UUID、consumer/reason/changed_fields 枚举，规范化后最多 4 KiB。`DecodeMetadata` 仅在仓储读取时按相同封闭 schema 再验证，拒绝未知/重复字段与类型错误；不是通用事件输入。

| action | metadata 构造器/内容 |
| --- | --- |
| `secret.create/update/delete` | `SecretMutationMetadata`：version；create/update 的 value/purpose changed_fields，delete 不带 changed_fields |
| `secret.resolve` | `SecretResolveMetadata`：lease_id、consumer、可选固定 reason |
| `secret.master.register` | `MasterMetadata`：version，System scope |
| `secret.master.rotation.start/complete/failed` | `MasterMetadata`：version、rotation_id、count；failed 必须有固定 reason；System scope |
| `outbound.policy.update` | `PolicyMetadata`：version、0–256 rule_count；System scope |
| `outbound.access.deny` | `DenialMetadata`：consumer、固定 reason、policy version；outcome=denied |

版本和数量为规范十进制 JSON string。每个 action 绑定对应资源类型，Agent 只能提交本 Project 的 Secret resolve/出站拒绝事件；Project gate 仍须正式端口批准。SafeRecord 的 ActorSummary 不返回认证 Actor 的 SessionID；正式 typed metadata/resource 中的 Session ID 仍是合法审计标识，不是 Cookie/token。summary 来自固定 action 文案，不复制关联对象正文。

## 查询与清理

每页和详情重新核验当前 Session、Project Owner 或 System 审计权限；AccessGrant 必须匹配本次 Actor（包括 Session）、scope 和 intent。Agent 没有审计浏览权限，管理员没有 Project 旁路。归档允许读历史，永久删除由正式 gate 阻止旧 cursor/receipt 再访问。

Filter 固定时间 `[from,to)`、actor kind/id、action/outcome、resource kind/id、tool/execution/operation/approval/runner ID。agent_id 为 actor Agent 或 resource Agent，与其余条件 AND。空可选字符串/空时间指针意味着不限制；没有正文搜索。使用 `foundation.DefaultPageRequest()` 取得默认 50，显式 limit 为 1–200。排序为 `(created_at DESC,id DESC)`，tuple 小于 cursor、多取一条判定 next，不使用 offset、不提供跨页快照。

Project/System 分别有时间、actor、action、resource 及适用关联部分索引。实际查询只拼接固定列/运算符，值全部参数化；integration 对足量隔离数据运行 EXPLAIN。

`CleanupProject` 只接受注册 Project lifecycle Service 与同 Project/operation 的 Delete cause。每批 500 条短 Tx 先取得 Project exclusive gate，再由 D08 端口验证持久 deleting + stopped + operation/version。归档、普通 Owner/admin、错误 checkpoint 均不能清理。checkpoint 标识 Project/operation，LastID 仅是诊断值，不作为跳过记录的边界。

未知提交重新拿 gate 并读取剩余记录，以免在旧事务未释放锁时推断完成；核实失败仍返回 `COMMIT_UNKNOWN`。不存在累计删除计数证明完成的路径。System 记录不动；延迟 Project append 必须再次通过同一持久 gate，不能重建已清理记录。D08 保存参与者进度；本块不实现完整项目删除引擎。

## System 管理 HTTP

当前管理员通过 GET /api/v1/system/audit 分页读取 System 记录，通过 GET /api/v1/system/audit/{id} 读取详情。列表恰返回 items/next_cursor，空为 []/null；详情返回单条记录。两口均为 GET-only，HEAD 等其他方法为405、Allow: GET，HEAD 错误无 body。正式路由、16个严格标量 query 和条件 DTO 见[工作卡](../work-items/d04-system-audit-management-http.md)与独立的 [Audit OpenAPI](../../../api/openapi/audit.json)。列表默认50条、显式1–200条，RawQuery 最多32 KiB、decoded cursor 最多8192B；详情无 query，两口均拒绝 body。原 Filter 合法集合与 cursor 的 scope/filter/order 绑定不变，limit 可变；cursor 不提供权限或跨页快照，其他 scope 的 ID 与不存在的 ID 同为404。

HTTP 沿现 Account boundary 核 Cookie、Host/Origin/Fetch-Metadata；ListSystem/GetSystem 再在同一 Tx 内取得 User Shared 锁并核当前 Session/admin。全部记录含 limit+1 哨兵、rows.Err/Close、cursor 签发与实际事务尾部完成，正常 Committed 且 ctx 有效后才发布。失败、失权、取消或 Unknown 均零候选；Unknown 保留 COMMIT_UNKNOWN/unknown，无新 lookup 或自动重读。正式 Logout 的 User Exclusive 与读取串行，root 复用同一原 auditor/core。旧通用 List/Get、Project、Append/LookupAppend/Cleanup 语义不变。

输出为闭合十字段 DTO，保留37种 action、13个 Service、17类 resource 和5种允许关联及其条件约束。metadata 经正式 typed 构造器重建，默认 JSON 转义后最多4096B；不输出认证材料、原 DB JSON 或关联对象正文。成功响应在写200前完整编码，最终 UTF-8 JSON 最多1 MiB（1048576B）；975420B 是保守页上界算术，不是规模性能证据。局部3s预算从预认证之前开始并继承更早 parent，实际读写、Flush、Body.Close 和取消 callback 必须完成；能力不足或短写/写错/Flush失败 abort，不作无界降级。普通日志/显式 Problem 不带 filter/cursor、记录内容或嵌套 cause，保持唯一 RequestID、安全 headers 和原其他 API 链。

## Project Owner Audit HTTP

默认 Central 根提供 `GET/HEAD /api/v1/projects/{project_id}/audit` 和 `GET/HEAD /api/v1/projects/{project_id}/audit/{audit_id}`，契约见 [Project Owner Audit 规格](../work-items/d04-project-owner-audit-http.md) 与 [Project Audit OpenAPI](../../../api/openapi/project-audit.json)。公开窄能力 `ListProject/GetProject` 在同一个真实 Tx 中先取 User Shared、再取 Project Shared，并重验当前 Human Session、Owner 和 Project Read gate；管理员没有跨 Owner 豁免，Agent/Service 不是查询主体。initialized 的 active/archiving/archived 可读，initializing/deleting 拒绝；没有新增 Audit 写入、保留期、导出、清理或生命周期流程。

列表默认50条、显式1–200条，复用原16项严格 query 和签名 cursor。filter 接受通用合法动作，合法 System-only action 在 Project 下返回空页；输出为31种 Project action、14类 resource 的闭合11字段投影，历史 Actor/typed metadata/关联必须满足各分支。全部行含额外哨兵均验证，Rows.Close 后再查 Err，严格 `(created_at,id)` DESC，next_cursor 签最后实际返回行；cursor 绑定 scope/filter/order，不授权限，也不绑定 limit 或提供跨页快照。summary 只取固定安全文案，不复制 name/description、材料、模型参数、正文或原数据库 JSON。

GET/HEAD 都实际读空请求体至 EOF，并完成相同授权、SQL、验证和完整编码；HEAD 不写实体，Content-Length 等于 GET 表示。其他方法405、Allow 精确 `GET, HEAD`；原 Account pathClean 先拒绝 trailing slash 等不规范路径，合法未知尾部404。完整响应最多1 MiB，写200前已验证和编码。局部3秒预算从预认证前开始并继承更早 parent，覆盖实际 I/O、Flush、Body.Close、服务和取消回调的实际收尾；逐项处理 setter/Close panic，保留原 tracked writer/RequestID/安全错误日志。过期后不发布候选或继续响应写入，尾部必须同步等待，不承诺所有 handler 在3秒内已返回。

仅原读取 Tx 实际 Committed、callback 完整且 ctx 仍有效才发布。取消、错误、坏行或 Unknown 一律零 record/items/cursor；`ProjectReadUnknownAttempt` 保存原 State/Cause/AttemptID，HTTP 只用既有安全 Problem，不增加 cause 字段或头。该读取没有 command/receipt、确认端点、WithoutCancel 或自动重读；既有 `retry_hint=lookup` 不表示本口提供查证能力。旧 System facade、通用 List/Get 和写入 `LookupAppend` 不改。默认根复用同 Store、Account/Project Authority 与原 auditor，不新增 initializer/worker。

本卡作者普通/race、标准 schema、三轮真实 native 和四新八旧 PG top 已通过，独立受控补集及 A02/B01 两个真实代表也通过。31动作及有限可选分支/反例的341标准例、实际200行页598220B与真实 Sign/Verify 页590584B分别有证据；8192B shape cursor 仅用于容量边界，不冒称实际签名产物。作者四份 GET 原字节及两份空 HEAD、独立两份 GET 原字节（1389B/727B）及同长度空 HEAD 均按各自来源验证；GET 由固定标准 Draft202012/FormatChecker 检查，不把合成样例当真实 HTTP。

原作者 new1 的400只定位至断言行，日志未记当时 action；两新测试把 `provider.*` 误写成 `model.provider.*` 的静态缺陷修正后重跑通过，生产未变，原 STATIC 漏检保留。独立 A01 私有坏 metadata 注入实际仅返回 DATABASE_SQL_FAILED，未进入待测 List；根据固定 schema 静态定位为既有 Project typed CHECK 不接受该形状，原轮未记录 SQLSTATE 或约束名；改用 PostgreSQL 可存而正式 scanner 拒绝的 UUIDv4 request_id 后 A02 通过，不放宽 SQL 或生产校验。所有原失败和版本组合保留。三 native 轮实际等待 direct3/adopted3、9个 listener 的全 TCP 含 TIME_WAIT 及 owned 双清；作者九轮和独立三轮 PG 各七资源实际等待/双清，daemon/PID1 差集另列未 wait，不声称全机清零。

物理 Unknown 证据绑定同一原读取 backend 的 `C(COMMIT)` 与 `Z(I)` 后丢失 ACK，保留原 cause/attempt，与结果装饰分开。正常关闭和100ms耗尽均有实际根路径；强制 root 返回不能由后续 fixture/client/backend 退役倒填为全部 inner join。正式 Account 身份和真实 Project/Secret/Model producer 参与主链，隔离 Skills 辅助不等于生产创建绑定。生产 Resolution/Invocations、D24、Project 创建/生命周期根仍未绑定，ready503；Object runtime join、OpenAI tools 独审、SPA 并发发布三停止保持，本卡不宣称完整 D04/D27、UI 或 Runtime 完成。

## cursor 与部署配置

`AGENTEAM_CENTRAL_CURSOR_KEYRING` 从本块起必填，进程不自动生成、不从数据库或 UI 读取：

```json
{"format":1,"current_kid":"c1","keys":[{"kid":"c1","key_b64":"<部署生成的独立随机32字节，标准带padding base64>"}]}
```

1–32 把 key、JSON ≤16 KiB、kid 1–64 位 `[A-Za-z0-9_-]`；拒绝未知/重复字段、重复 kid/材料、尾随值、非规范 base64。keyring 不可变；部署变更需要重启。保留旧 kid 只验签，新签名只用 current；移除后旧 cursor 失效。不得复用后续 Secret AES key。

三段 token 为 canonical header、canonical payload、HMAC-SHA256，各段严格无 padding base64url，总长 ≤8 KiB。签名域为 `agenteam.cursor.v1` 加零字节再加 `H.P`。payload 绑定 scope、完整规范 Filter digest、order 和 typed position；limit 不参与。需要 order_generation 的其他列表在公开 Go 端口使用正 int64，wire 必须是正十进制 string，最大 MaxInt64。Audit 固定排序不使用 generation。

`--check-config` 只校验配置，输出 scope=d04/valid=true/ready=false，不连接数据库。正常启动先完成 D03 DB 阶段，再在独立 30s 安全预算内初始化 cursor、验证 Audit 存储，之后监听。`/diagnostics` 保持 ready=false，cursor/Audit 存储可用不表示授权已绑定；其余未实现安全模块仍 unbound。

## 验证

```sh
AGENTEAM_GO=/path/to/go1.27.1/bin/go sh scripts/check-go.sh
AGENTEAM_GO=/path/to/go1.27.1/bin/go sh scripts/test-postgres.sh -run '^TestAudit'
AGENTEAM_GO=/path/to/go1.27.1/bin/go sh scripts/test-postgres.sh
```

普通测试覆盖封闭 metadata、安全投影、摘要、授权拒绝、独立 cursor 已知向量与配置/启动顺序。integration 使用经 nonce/label/精确 ID 校验的临时 PG fixture；包含真实事务回滚/并发重放、COMMIT 响应丢失与正式 LookupAppend、每页授权/过滤、索引、分批删除/未知重读及迟到 append。测试权限事实只存在隔离 fixture，不能作为生产身份绑定的验收。完整 suite 同时回归已有数据库迁移/恢复及实际 Central 信号/故障处理。


### System 管理 HTTP 的实际验证范围

本次只运行受影响 query、HTTP DTO/schema、root 的纯测/race/vet、集成编译及 Central/Runner 构建，未重跑上述完整 suite。作者用正式 Account/出站 producer、正式邀请普通用户与 Logout 撤销造事实，不 SQL 写 Audit 或改 role。三个实际 fixture 轮次如下，分别 exit0 /103.558s、55.982s、55.256s；原 race/count1/pkg6m 保持，各业务场景小于2m：

~~~sh
AGENTEAM_GO=/path/to/go1.27.1/bin/go sh scripts/test-security.sh -run '^(TestSystemAuditHTTPQueryAndProjection|TestSystemAuditHTTPTransactionAndBudget)$'
AGENTEAM_GO=/path/to/go1.27.1/bin/go sh scripts/test-security.sh -run '^(TestSystemAuditHTTPRootProducerBinding)$'
AGENTEAM_GO=/path/to/go1.27.1/bin/go sh scripts/test-security.sh -run '^(TestAuditPaginationBindingsFiltersAndRevocation)$'
~~~

原生 HTTP 使用以下两个固定版本轮次，不能称首轮全绿。native01 两项通过，SlowBody 两子例仅因要求终局 Err 必为 DeadlineExceeded 而失败；native02 增加 parent/本地期限夹界、Done 闭合与终局已到期限的断言，允许 Go 原生 I/O/timer/defer 竞争下的 Canceled 或 DeadlineExceeded，保留原预算、abort、零查询、Close/join 及无200要求。pure 自然 DeadlineExceeded 断言未放宽。

~~~sh
# native01 / input02：实际 exit1，原失败保留。
/path/to/go1.27.1/bin/go test -race -count=1 -timeout=45s -v -run '^(TestAuditHTTPNativeEOFAndKeepAlive|TestAuditHTTPNativeSlowBodyBudgets|TestAuditHTTPNativeWriteAndFlushAbort)$' ./internal/central/audit/http
# native02 / input03：仅受影响顶层两子例 PASS，复用另外两项。
/path/to/go1.27.1/bin/go test -race -count=1 -timeout=45s -v -run '^(TestAuditHTTPNativeSlowBodyBudgets)$' ./internal/central/audit/http
~~~

独立私有 overlay 的 selector 为 ^(TestAuditIndependentCurrentAuthorityAndTerminal|TestAuditIndependentRootProducerProjection)$，同轮两项 PASS、actual exit0 /105.717s；这些探针不是仓库公开测试入口。前者是真实 PG/正式 Authority 加受控 writer，后者为同 root 原生连接；受控 writer 提交前未发布不能称 TCP 零字节。真实 DB1s锁超时不冒称自然3s，Unknown 零候选沿受控验证。全部资源轮实际等待并双清，自有 ID/PID 与原基线分开，历史 PPID1 僵尸不在回收声明中。原纯测/trace/native 前提失败及 query-pure02 早退路径缺少独立 goroutine join 证据的限制均保留。

上述 System 管理 HTTP 的历史验收不覆盖 Project Audit HTTP；Project 的当前范围见上节。该历史验收仍不覆盖 UI、完整 D04/D27、SPA 或 Runtime。后续主线新增同包前端 harness 的整合编译/发现需另记，固定基线真实轮不冒称后续主线动态验证。
