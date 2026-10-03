# Audit 与签名 cursor

`internal/central/audit` 消费 D03 Store/Tx，`audit/contract` 只依赖基础类型与最小 `identity/contract`。`00002_audit.sql` 在同一全局迁移序列增加 `agenteam_audit.audit_records`。本块不创建 User/Session/Project 事实，不提供 Audit HTTP 路由；D07 绑定当前 Session/System 权限，D08 绑定 Owner/gate/删除 cause。未绑定端口返回 `DEPENDENCY_UNBOUND`。规格见 [D04 B01](../work-items/d04-security-design.md#4-b01audit-与签名-cursor)。

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

版本和数量为规范十进制 JSON string。每个 action 绑定对应资源类型，Agent 只能提交本 Project 的 Secret resolve/出站拒绝事件；Project gate 仍须正式端口批准。SafeRecord 不返回 SessionID；summary 来自固定 action 文案，不复制关联对象正文。

## 查询与清理

每页和详情重新核验当前 Session、Project Owner 或 System 审计权限；AccessGrant 必须匹配本次 Actor（包括 Session）、scope 和 intent。Agent 没有审计浏览权限，管理员没有 Project 旁路。归档允许读历史，永久删除由正式 gate 阻止旧 cursor/receipt 再访问。

Filter 固定时间 `[from,to)`、actor kind/id、action/outcome、resource kind/id、tool/execution/operation/approval/runner ID。agent_id 为 actor Agent 或 resource Agent，与其余条件 AND。空可选字符串/空时间指针意味着不限制；没有正文搜索。使用 `foundation.DefaultPageRequest()` 取得默认 50，显式 limit 为 1–200。排序为 `(created_at DESC,id DESC)`，tuple 小于 cursor、多取一条判定 next，不使用 offset、不提供跨页快照。

Project/System 分别有时间、actor、action、resource 及适用关联部分索引。实际查询只拼接固定列/运算符，值全部参数化；integration 对足量隔离数据运行 EXPLAIN。

`CleanupProject` 只接受注册 Project lifecycle Service 与同 Project/operation 的 Delete cause。每批 500 条短 Tx 先取得 Project exclusive gate，再由 D08 端口验证持久 deleting + stopped + operation/version。归档、普通 Owner/admin、错误 checkpoint 均不能清理。checkpoint 标识 Project/operation，LastID 仅是诊断值，不作为跳过记录的边界。

未知提交重新拿 gate 并读取剩余记录，以免在旧事务未释放锁时推断完成；核实失败仍返回 `COMMIT_UNKNOWN`。不存在累计删除计数证明完成的路径。System 记录不动；延迟 Project append 必须再次通过同一持久 gate，不能重建已清理记录。D08 保存参与者进度；本块不实现完整项目删除引擎。

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
