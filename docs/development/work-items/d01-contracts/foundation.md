# 基础类型、访问与事务

依据：[基础契约](../../../architecture/platform-infrastructure/foundation-contracts.md)、[账号](../../../architecture/platform-infrastructure/authentication/account-lifecycle.md)、[安全治理](../../../architecture/security-governance/README.md)、[仓库结构](../../repository-structure.md)。责任与未绑定状态见[索引](README.md)。

## 标量与编码

| 类型 | 确定的边界 |
| --- | --- |
| `ID<K>` | 非零 UUIDv7，持久化 UUID，API 小写连字符字符串；各领域 ID 是不同命名类型，不能混用 |
| `Instant` | UTC、微秒精度，DB `timestamptz`，JSON RFC 3339，固定六位小数及 `Z`；入站偏移规范化 UTC，拒绝超出微秒精度，不能静默损失并发信息 |
| `Version / Revision / Sequence` | 1 起始的正 `int64`；JSON 用十进制字符串以免 JS 精度损失。无版本/零水位以显式 null 或专用 `Progress=0` 表达，不以 0 作 editable version |
| `DurationMS` | 非负 `int64` 毫秒；跨 JSON 为十进制字符串。单工具 timeout 的合法范围由工具契约声明 |
| `Digest` | `sha256:` 加 64 位小写 hex；摘要不承担权限或业务 ID |
| `IdempotencyKey` | 1–128 个 ASCII `[A-Za-z0-9._:/-]`；调用者稳定生成，区分大小写；不能含 Credential/正文 |
| `PageRequest` | `cursor?: string, limit: int`；默认 50，允许 1–200；特定资源可以在自身规格中降低上限并返回验证错误，不能静默提高 |

每个服务命令包含 `CommandMeta{request_id: ID<Request>, idempotency_key: IdempotencyKey, expected_version?: Version}`。HTTP server 每次请求生成独立 request_id，在所有响应的 `X-Request-ID` 及错误 Problem 中返回，不接受客户端复用传入值作为服务端追踪身份；后台 job 每次处理同样产生独立 trace ID，幂等仍使用原业务 key。原 Task/Meeting 伪签名中承担去重含义的 `request_id/client_request_id` 一律映射到 `idempotency_key`；Runner `request_id` 专指一次 RPC Attempt，见[模型工具](model-tool.md#runner-与-mcp-通信边界)。

## Actor 与 scope

```text
Actor = Human {user_id, session_id}
      | AgentRun {project_id, agent_id, execution_id}
      | Service {service_name, cause_id, project_id?}
Scope = System | Project {project_id} | AgentMemory {project_id, agent_id}
AccessIntent = read | mutate | launch | resume | lifecycle | converge
Authorize(ctx, actor, scope, intent) -> Result<AccessGrant>
AccessGrant {actor, scope, intent, checked_at, authorization_revision}
RequireSession(ctx, session_cookie) -> Result<Human>
RequireOwner(ctx, tx?, human, project_id, intent) -> Result<ProjectAccess>
RequireSystemAdmin(ctx, tx?, human, action) -> Result<SystemAccess>
```

Actor 只能由受信任 HTTP/Tool/后台适配层构造。JSON 不接收 user/Owner/Agent 权限布尔值；模型传的 `project_id/agent_id` 即使属于业务参数也必须和真实执行身份核验。`AccessGrant` 是当前调用的服务内校验结果，不能跨请求作为 bearer token，也不能在异步恢复中长期复用。

Actor/Scope/AccessGrant 属于 identity contract；foundation 的 Tx/CommandMeta 不 import 这些类型，命令作用域经下文纯标量 CommandIdentity 映射，保持底层依赖单向。

人类 Project 操作要求当前 Session 有效且 `project.owner_user_id == user_id`；系统管理员只能管理 System scope，不据此读取他人项目、Memory、Inbox、Execution 或 Tunnel。跨项目或同项目不同 Agent 的 Memory 都不得仅凭 caller-supplied ID 访问。Service 身份限组合根预注册职责和 cause，不模拟 Owner；只能调用明定的内部命令，不能成为绕过门禁的通用管理员。

Execution 能力快照与逐调用检查分开：快照固定模型/Tool/schema/Policy/资源身份；每次 Tool 调用及恢复仍检查当前 Project gate、执行取消、资源归属、当前授权与必要 lease。Policy 只收紧 Capability。Core Tool 仍受 scope 与领域校验；批准不能扩充基础能力。

## HTTP 与前端 DTO

API 命名空间固定 `/api/v1`。集合和 ID 路径采用复数 kebab-case 资源名，命令使用 `POST .../<verb>`；页面 `/{username}/{project_name}` 仅作可读路由，API 解析后始终使用稳定 ID。具体业务 URL 在所属模块开工时固定，不以 D01 预写全部 handler。

Session Cookie 为服务端随机会话凭据，`HttpOnly; Secure; SameSite=Lax; Path=/`，生产名 `__Host-agenteam_session`。写方法 `POST/PUT/PATCH/DELETE` 必须验证允许的同源 `Origin` 和绑定当前 Session 的同步 CSRF token（`X-CSRF-Token`）；缺失/不匹配拒绝，不把 SameSite 当唯一防护。登录/邀请兑换/重置等匿名写请求也校验 Origin，并使用 D07 挑战/一次性凭据契约。Runner 设备认证不复用浏览器 Cookie API。

`GET /api/v1/session` 返回当前安全身份投影和当前 CSRF token，`Cache-Control: no-store`。改密/重置/退出撤销以 DB Session 状态为事实；D25/D17 关闭失效 WebSocket/Tunnel，并在每次订阅、HTTP/upgrade 重新校验。读 API 不产生业务变更。日志、错误和 DTO 不回显 Cookie、CSRF、Secret、Signed URL 凭据。

HTTP 写请求用 `Idempotency-Key` header；领域适配器转换为 `CommandMeta.idempotency_key`。对象 mutation 的 JSON 包含 `expected_version`。同一页面重试相同业务操作保留 key，新用户意图使用新 key；fetch abort 只取消客户端等待，不证明服务端未提交，也不代替领域 cancel 命令。

成功读/命令默认 200，创建 201；持久化取消/生命周期接受但未完成返回 202 与 `operation_id/status_url`。完成后查询返回真实终态；拒绝或明确失败不伪装成 202。UI DTO 单独定义，显式字段白名单转换，内外都保留 canonical snake_case 枚举。

D02 建立 Problem/分页 schema；各模块维护自己的 `/api/v1` OpenAPI 3.1 片段及请求/响应 DTO（实际文件在模块实施时增加）。D26 在构建中从汇总 schema 生成 TypeScript transport types，领域 client 手写薄适配；类型生成/解析器版本在 D26 固定。服务器 DTO、schema、生成类型必须在同一变更验证，不能另建前端手抄领域模型或把内部 struct 全量序列化。未知枚举显示受控“暂不支持/请刷新”，不能当 success。

## 错误

内部 `Fault{code, safe_message, field_errors[], retry_hint, commit_state, cause_id?}`；`cause_id` 仅关联脱敏诊断。`commit_state = not_started | not_committed | committed | unknown` 明确区分存储提交和外部副作用 outcome，二者不能混用。

Problem Details 使用 `application/problem+json`：

```json
{
  "type": "urn:agenteam:problem:version-conflict",
  "title": "Version conflict",
  "status": 409,
  "detail": "对象已被修改，请重新读取。",
  "instance": "/api/v1/tasks/019...",
  "code": "VERSION_CONFLICT",
  "request_id": "01900000-0000-7000-8000-000000000001",
  "field_errors": [{"path": "/expected_version", "code": "STALE_VERSION"}],
  "retry_hint": "reread",
  "commit_state": "not_committed"
}
```

示例 instance 是示意路径；实际 ID 必须合法 UUID。`field_errors.path` 是 JSON Pointer；错误 detail 为可本地化安全描述，客户端分支只使用 code。

| 共同 code | HTTP | 调用者动作 |
| --- | --- | --- |
| `INVALID_ARGUMENT / CURSOR_INVALID` | 400 | 修正字段，不重试原错误输入 |
| `UNAUTHENTICATED / SESSION_REVOKED` | 401 | 重新认证，关闭旧订阅 |
| `FORBIDDEN / CSRF_FAILED / ORIGIN_DENIED` | 403 | 停止，不退化匿名调用 |
| `NOT_FOUND` | 404 | 对不可见项目资源也使用此投影，防枚举；内部可保留 forbidden 原因 |
| `RESOURCE_DELETED` | 410 | 仅对已授权可见的历史来源暴露；否则 404 |
| `VERSION_CONFLICT / IDEMPOTENCY_KEY_REUSED / INVALID_STATE / AGENT_BUSY / RESOURCE_BUSY / PROJECT_NOT_ACTIVE / CONFIRMATION_STALE` | 409 | 按 code 重读/等待/重新确认；绝不自动覆盖 |
| `SCHEMA_UNSUPPORTED / CAPABILITY_UNSUPPORTED` | 422 | 当前配置/协议不能执行，显式处理 |
| `RATE_LIMITED` | 429 | 可附 Retry-After，保持同一业务 key |
| `DEPENDENCY_UNBOUND / DEPENDENCY_UNAVAILABLE` | 503 | 不当作空列表、无占用或授权通过 |
| `COMMIT_UNKNOWN` | 503 | `retry_hint=lookup`，沿同 key 查结果，不能生成新 key |
| `INTERNAL_ERROR` | 500 | 安全诊断 ID；不能泄露堆栈/SQL/原始 Provider body |

领域可增加稳定 code，例如 `TASK_VERSION_CONFLICT` 映射 409；其语义仍服从共同类别。waiting 是成功控制结果，不返回 HTTP 错误或把 `approval_required` 当永久失败。ToolError/ModelError/RunnerOutcome 分别见[模型工具](model-tool.md)，经 HTTP 展示时仍包为安全 DTO。

## Cursor

```text
CursorHeader {signature_version: 1, kid: string}
CursorPayload {format: 1, scope: Scope, query_digest: Digest,
               order: string, position: [typed scalar], order_generation?: int64}
Page<T> {items: T[], next_cursor?: string}
```

规范化筛选/搜索条件/排序/权限 scope 后计算 digest；header/payload 使用下节 canonical-v1 JSON 字节，分别无 padding base64url 编码为 H/P。token 为 `H.P.M`，M 是 HMAC-SHA256 对固定域分隔字节 `agenteam.cursor.v1`、一个零字节及 ASCII `H.P` 的签名，再无 padding base64url。signature_version=1 固定算法，kid/版本都在签名覆盖内，不接受客户端指定其他算法；签名密钥由部署 Secret 配置加载。游标不含 Secret/正文，签名不替代每页授权。limit 可变化，scope/filter/order 不匹配或签名失败均 `CURSOR_INVALID`；密钥轮换按已签 kid 选择保留的验证密钥，未知kid/版本要求首屏重读。

默认 `(created_at DESC, id DESC)`；特定列表明确自身稳定唯一排序。Task 用业务排序 + rank + ID；rank 重整推进所属排序组 `order_generation`，不推进 Task version/updated_at，旧 cursor 返回 `CURSOR_STALE`（409）。Knowledge 同层 `(title ASC, id ASC)`，绑定 parent/filter；不增加结构版本/历史。遍历不承诺全程快照隔离，内容变化可能改变分页；Runtime 的水位与窗口另见[实时契约](runtime-events.md)。

## 业务幂等

```text
CommandIdentity {namespace: string, owner_ids: UUID[], command: string, key: IdempotencyKey}
CommandRecord {identity, semantic_digest, state: in_progress|completed,
               result_ref?, committed_at?}
LookupCommand(ctx, actor, identity, semantic_digest) -> Result<CommandLookup>
CommandLookup = committed {safe_original_result}
              | in_progress | not_observed
```

语义摘要使用版本化编码 `canonical-v1`：验证类型、展开契约明确的默认值，JSON object key 排序，整数精确编码，UUID/时间规范化，集合按契约排序，有序数组保持顺序，省略和 null 仅在字段明确等价时合并。拒绝重复 JSON key、NaN/Infinity 与无法精确表达的数字；不 trim 任意正文，不推断 URL/命令等价。摘要输入包括 command、稳定目标、Actor 的授权主体、expected_version 和全部影响语义的参数；Human 主体为稳定 user_id，排除 session_id，因此同用户换新有效 Session 仍可重放。AgentRun 主体为 project/agent/execution，Service 为服务名与稳定业务 cause，不纳入其处理 attempt ID。摘要不包括 trace ID、CSRF、传输超时/连接信息。具体 Launch/Tool/Summary 摘要字段在各自契约定义。

CommandIdentity 是 foundation 的纯标量命名空间，不引用领域 Scope：System 映射 `system/[]`，Project 映射 `project/[project_id]`，Agent Memory 映射 `agent_memory/[project_id,agent_id]`；Launch 使用 `launch/[project_id,agent_id]`。owner_ids 顺序固定，合法 namespace 由对应命令注册/校验，外部调用者不能自行指定以绕过授权。

处理顺序：

1. 当前 Session/Actor、scope 和结果可见权限校验；不能靠命中幂等记录泄露已失去权限的结果。
2. 在唯一 command identity 上查询/串行化；同 key 不同摘要返回 `IDEMPOTENCY_KEY_REUSED`。已完成同语义重放原结果，不重验旧 expected_version、不重复副作用或竞争 slot。
3. 无已完成结果才检查当前生命周期/业务权限、expected_version 与不变量；同事务提交 mutation、必要事件和成功幂等结果。
4. 取消/归档后仍可授权读取的历史成功结果可重放；这是读旧结果，不是获准重新执行。已删 Project 无权重放已清理正文。

作用域默认 `(scope, command, key)`；Task 固定 `(project_id, task-command-type, key)`；Launch 固定 `(project_id, agent_id, key)`。对象 key 不按 title/参数相同自动合并。明确未提交失败不冻结未来执行结果，可以同 key 同语义重试并重新验证；永久业务拒绝可保存审计，但不是成功幂等记录。异步生命周期接受本身是已提交结果，返回原 operation identity。Project 永久删除后只允许原 Owner 当前有效 Session 读取/重放[最小完成 receipt](domain-lifecycle.md#project-生命周期端口)，不查询已删项目正文或依赖已不存在的 Owner 行。

同 identity 并发由数据库唯一约束 + transaction lock 排序；不能仅用进程 mutex。记录随所属业务事实保留，首期不自动 TTL；明确永久删除按生命周期矩阵清理，不保留可重放项目内容副本。网络断开/超时既可能未提交也可能已提交，持久化读取不可用时保留 unknown，不能以 `not_observed` 推断所有旧工作已终止。Scheduler Launch 的专用只读契约更严格，见[执行与编排](execution-orchestration.md#launch-与只读结果查询)。

## Tx 与锁顺序

```text
Tx // 不透明句柄，仅限创建它的数据库/事务回调；不含公开 pgx 类型
TransactionCause = commands {primary: CommandIdentity, related: CommandIdentity[]}
                 | job {job_type, job_id, attempt_id}
                 | delivery {event_id, handler_name}
                 | recovery {owner, recovery_run_id, checkpoint_ref?}
WithinTx(ctx, cause: TransactionCause, fn: (ctx, Tx) -> error) -> CommitResult
CommitResult = committed | not_committed {fault}
             | unknown {transaction_attempt_id, cause: TransactionCause}
Acquire(ctx, Tx, LockKey, mode: shared|exclusive) -> error
```

事务由最外层应用命令/后台处理拥有，内部 `...InTx` 端口不得私自 commit、嵌套开新事务或逃逸 Tx。一个命令协调多个原子幂等事实时列出 primary/related；无外部命令的投递、索引job、恢复事务使用各自稳定 cause，不伪造业务 command identity。unknown 交回拥有者按相同 cause 查询其 canonical checkpoint/processed marker/幂等记录，没有确证就保留未知；transaction_attempt_id 仅关联诊断，不另承诺通用事务结果服务。普通读不接 Tx；正确性关键占用/授权/gate 检查接同 Tx。D03 适配 pgx，事务隔离默认 Read Committed 配合明列 locks/约束；无法用既定锁保护的多行断言在领域规格选 Serializable，并处理确定回滚重试。外部模型、SMTP、Runner、MCP、MinIO 请求不得持有数据库事务/锁等待。

固定全局锁层序；同层多个 key 按规范 UTF-8 key 字节排序，aggregate 层先按下表的 kind 顺序、再按 ID 字节排序：

| 顺位 | key 与模式 | 保证 |
| --- | --- | --- |
| 1 | `system-config:<kind>`、`user:<id>`（仅需要时） | 系统引用替换、账号/路径变更；影响多个 Project 先收集再排序 |
| 2 | `project:<id>` gate，正常 mutation shared、生命周期切换 exclusive | 允许已开始短事务先提交，门禁提交后不产生新活动 |
| 3 | `project-schedule:<id>`、`knowledge-tree:<id>`、`rank-group:<scope>` | 调度/Complete/Move、树防环/确认范围、排序维护互斥 |
| 4 | `agent:<id>` | slot、分配、输入确定点、终态释放与 Agent 配置引用 |
| 5 | aggregate 按 `(kind,id)`，kind 顺序 `provider,model_config,tool_spec,mcp_connection,credential_ref,sprint,task,meeting,turn,contribution,dispatch,execution,operation,approval,decision,skill,memory,object` | 配置/引用捕获、本域状态与跨模块原子变更；所需 key 提前收集后顺序取得 |
| 6 | Command/Outbox/引用/投影写记录 | 唯一约束、同事务副事实 |

命令幂等 identity 的 transaction advisory lock 可以作为所有业务锁之前的专用序位 0，锁 key namespace 与业务 gate 分开。顺序“查已成功幂等再业务锁”不代表先锁 aggregate 后回头抢较低 gate。只读预扫描用于收集 key，取得高层 guard 后重新读取；发现新增更低序资源释放并重试整个未提交事务，不能逆序加锁。Tx 内禁止调用会重入上层锁的流程。

Project 删除推进、Task claim/Complete、Launch、Resume、审批决议/消费、Skill 分配/输入边界必须使用同一 gate/key 定义；各模块自行定义同名但不共享的锁无效。数据库 unique slot、unique pending dispatch、Owner 名称等约束仍必需，锁不是唯一防线。D03/D11/D22/D23/D24 按[走查](walkthroughs.md)真实验证死锁、提交未知与竞争。

## 版本与兼容

HTTP `/api/v1`、Runner `{major,minor}`、Domain Event `schema_version`、Checkpoint/Trigger/ToolSpec schema 各自独立版本。新增枚举不能由旧客户端默认成既有成功；Runner 未协商的 required feature 返回 unsupported。事件 decoder 按 event type/version 注册，未知版本进入可诊断 failed delivery，不丢弃当成功。不可变 Snapshot 引用旧 decoder；迁移失败时拒绝不安全恢复，不静默用最新配置重建旧输入。
