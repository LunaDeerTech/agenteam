# D10 普通 Project Variables Owner 服务与 HTTP

> 状态：SPEC 已获独立有限审查接受，按授权范围实施中，尚未产品验收。工作分支 `ai/project-variables`，正式基线 `f1c94ee5`。迁移 `00024` 由 root 为本卡独占预留。
>
> 完整结果：已有初始化 Project 的当前 Human Owner，可以经默认 Central HTTP 创建、读取、分页、更新及删除普通变量，以原意图 Lookup 与显式同 key 重放恢复响应丢失。本卡不是完整 D10、Agent F1、Secret 或执行环境注入交付。

## 1. 来源、真实依赖与排除范围

业务依据是[项目变量架构](../../architecture/project-work-management/project-environment-variables.md)，身份复用[已交付 R1](d01-resource-identities.md)的 `identity.ProjectVariableID`。流程、完整模块门槛见[团队流程](../agent-team/README.md)、[开发计划](../development-plan.md#d10-agent-配置与项目变量)；全局产品状态只在[任务台账](../agent-team/tasks.md)维护。

本基线已具有 Account HTTPBoundary/当前 Session/Activity、Project `Authority.RequireOwnerInTx`、Postgres Store/Tx/完整锁集与提交状态、签名 cursor、typed Audit、Catalog/Outbox 同事务追加、默认根及[Work Owner HTTP](d11-work-owner-http.md)的自然期限和真实退出模式。`identity/contract/resources.go` 已有唯一 ProjectVariable marker，不新增或借用 CredentialID。

本领域不依赖 Skills、Model UI、Agent 目录或 Object 的新业务能力。默认根原有 Object/MinIO 启动与测试资源保持，不因本卡解除 Object join 停项。若测试需要新 Project，用正式 Project 服务与明确披露的持久测试 Skills 前置，不宣称真实 Skills 交付。

排除 Secret 加密/明文/白名单、MCP 凭据引用、Agent F1/Prompt/ExecutionContext、Runner 环境注入、UI、Project 创建 HTTP、Project 完整归档/删除参与者。本次普通变量没有外域引用消费者，不造成功空引用检查。后继 Secret 必须扩展同一 Variables 目录和名称空间，另行落实 Credential 映射及引用保护；现 Credential 目录不自动成为 Project Variable。

## 2. 精确文件与接缝

下表精确实施范围已由 root 授予本树唯一实施者；独立验收路径仍由后续未参与实现者独占，不得扩至其他树或未列共享源。

| 范围 | 路径与必要理由 |
| --- | --- |
| 新契约 | `internal/central/projectvariable/contract/{types,commands,query,events}.go` 及对应 `_test.go`；被本卡服务与 HTTP 直接消费，不先交无消费者纯类型 |
| 新领域 | `internal/central/projectvariable/{service,authority,commands,repository,reader,events}.go` 及对应 `_test.go`；同 Store 授权、持久命令、分页、事实与调用生命周期 |
| 新 HTTP | `internal/central/projectvariable/http/{handler,read,commands,wire,io}.go`、对应 `_test.go` 与 `native_test.go`；私有有界 I/O，不重构共享框架 |
| 迁移 | `db/migrations/00024_project_variables.sql`；目录/命令/历史/generation 与 Audit 闭集 SQL 扩展，旧 migration 不改 |
| Audit 契约 | 新 `internal/central/audit/contract/projectvariable.go`、`projectvariable_test.go`；现 `types.go`、`metadata.go` 及同名测试，仅注册本卡三 action/producer/resource 和严格 metadata |
| Project 精确分派 | `internal/central/project/audit_facts.go`、`audit_facts_test.go`、`events.go`；新 `projectvariable_event_authority.go`、`projectvariable_event_authority_test.go`，保留既有 Work/Model 行为 |
| 已有 Audit HTTP 消费 | `internal/central/audit/http/project_wire.go`、`project_wire_test.go`、`api/openapi/project-audit.json`；新记录要能经已有 Owner Audit 页读取，summary 沿 `Audit event`，无需修改 `audit/query.go` |
| 已有 Audit 客户端兼容 | `web/src/api/project-audit-metadata.ts`、`web/src/tests/project-audit-metadata.spec.ts`、`web/src/tests/project-audit-client.spec.ts`；现客户端闭集解码会拒绝新 action/resource，须精确扩三分支与关系校验，避免普通变量写入导致既有安全记录页整页失败；不是新增变量 UI 或扩 System Audit filter |
| 默认根 | `internal/central/app/account.go`、`account_test.go`、`project_usage.go`、`project_usage_test.go`；新 `project_variables.go`、`project_variables_test.go`、`project_variables_process_test.go`，落实构造顺序、路由、Stop/Drain/Force/Joined |
| 新 schema | `api/openapi/project-variables.json`，复用现 `common.json` Problem，不新增公共错误 |
| 集成/进程 | `tests/projectvariable/{fixture,persistence,authority,concurrency,recovery,http}_test.go`、`tests/process/project_variables_http_test.go`；复用真实 fixture、COMMIT 代理与默认根资源链 |
| 独立验收 | `.agent-state/project-variables/` 内必要 probe/入口由未参与实现者独占，具体新增文件随派工登记，不复制日志或建立 manifest |
| 必要文档 | 本卡、`docs/development/backend/README.md`、最终台账本卡行；current 只作恢复，不复制其他任务流水 |

Foundation fault/lock、公共 httpapi/problem、common.json、Account 服务接口、Project authority.go 与生命周期参与者均保持现有契约。若实际需要扩域，先报具体缺口。不引入新依赖、worker、定时清理、通用注册框架。架构业务规则未改变，不预占架构文档写域。

### 2.1 服务与 HTTP 的稳定接口

`VariableID` 仅 alias `identity.ProjectVariableID`，ProjectID 仅 alias `identity.ProjectID`。DTO 具有严格 Validate/JSON/Clone；含原意图或用户值的类型采用私有封闭存储与显式 wire 投影，顶层和 enclosing fmt/slog 不泄露内容。

```go
type Commands interface {
    // CommandMeta.ExpectedVersion must be nil for create.
    CreateVariable(context.Context, identity.Actor, foundation.CommandMeta,
        ProjectID, VariableCreate) (VariableMutation, error)
    // CommandMeta.ExpectedVersion is required and validated for update/delete.
    UpdateVariable(context.Context, identity.Actor, foundation.CommandMeta,
        ProjectID, VariableID, VariableUpdate) (VariableMutation, error)
    DeleteVariable(context.Context, identity.Actor, foundation.CommandMeta,
        ProjectID, VariableID) (VariableMutation, error)
    LookupVariableCommand(context.Context, identity.Actor,
        VariableCommandLookupRequest) (VariableCommandLookup, error)
}
type Queries interface {
    GetVariable(context.Context, identity.Actor, ProjectID, VariableID) (Variable, error)
    ListVariables(context.Context, identity.Actor, ProjectID,
        foundation.PageRequest) (foundation.Page[VariableSummary], error)
}
```

版本输入只有现成的 `foundation.CommandMeta.ExpectedVersion *foundation.Version`（`foundation/id.go`）一个来源，不另加可冲突的版本参数。三个入口先 `CommandMeta.Validate()`，再按 command 检查 presence：create 必须 nil，update/delete 必须非 nil 且 `ExpectedVersion.Validate()` 成功，否则 INVALID_ARGUMENT；不能只依赖允许 nil 的通用 `CommandMeta.Validate()`。入口将值复制到私有原意图，避免调用者随后改变指针。HTTP PATCH/DELETE 的必需 `expected_version` 解码到此参数；Lookup 的原版本也放入同一 CommandMeta 后经同一 `VariableCommandDigest` 绑定其 presence 与值。delete 没有业务 request DTO，但绝不省略版本：预检、final 前像核对和同 key 改义检查均使用捕获的原版本，不能从当前对象补齐。

command 稳定名为 `project.variable.create|update|delete`。公开纯工厂 `VariableCommandIdentity`、`VariableCommandDigest` 共用原意图规范化；Lookup request 含 ProjectID、Command、IdempotencyKey、SemanticDigest，不接任意 namespace。HTTP 由原 request 构造 digest，不要求客户端实现 Go 摘要。

`NewAuthority(store Store)` 纯构造自有事实证明器，实现 Outbox ProducerAuthority、Audit ProjectFactAuthority，不读取 Project 私表或颁发 Owner 权限。该确切对象先注入 Project AuditFacts 与 Outbox Producer；再 `New(store, Dependencies{Authority, Projects, Events, VariableEvents, Audit, Activity, Cursors})` 建立同时实现 Commands/Queries 的 Service。拒绝 nil/typed nil、无效 keyring、已知不同 Store，不 late-fill 权限。`Stop()`、`Drain(ctx)` 覆盖读、写、Lookup、确认尾。HTTP `NewHTTPHandler(service, boundary)` 消费这六能力和真实 Account HTTPBoundary，`HandlesPath` 仅认本卡路径。

## 3. 字段、版本与删除

| 字段 | 规则 |
| --- | --- |
| id / project_id | canonical 小写 UUIDv7；id 创建前由调用者生成，稳定且全局不重用；跨 Project 不可见 |
| type | 固定 `variable`；写 request 不接 type，拒绝 secret/credential 或额外字段 |
| name | ASCII `[A-Za-z_][A-Za-z0-9_]{0,127}`，1..128 B；不 trim、不折叠大小写、不 Unicode 归一化；同 Project 存活记录按 C collation 精确唯一 |
| 保留名 | ASCII 不区分大小写后等于 `AGENTEAM` 或以 `AGENTEAM_` 开头拒绝，避免覆盖平台 namespace |
| description | 必须是字符串，可空，合法 Unicode scalar/UTF-8，≤4096 B；禁止 NUL 和除 TAB/LF/CR 外 Cc，逐字保存 |
| value | 必须是字符串，可空，合法 Unicode scalar/UTF-8，≤32768 B；禁止 NUL，其余逐字保存，不解释模板/shell/URL/凭据 |
| version | canonical 十进制字符串 `1..9223372036854775807`；创建1，真实更新/删除+1，不借 Project.version |
| created_at / updated_at | 正式 Instant，DB 微秒时刻；创建相等，真实更新单调非减；no-op 不刷新 |

完整 Variable 恰 `id,project_id,type,name,description,value,version,created_at,updated_at`；VariableSummary 恰去掉 `value` 的这些字段，不用空值假装详情。name、description 也是用户数据，不进日志或 Audit metadata。

create request 恰 `{variable_id,name,description,value}`。update request 为 name/description/value 中至少一个 presence 字段：缺席保留、空串可清 description/value，null/重复/未知/大小写别名 key 拒绝。精确字节比较，先 expected_version 再判 no-op；陈旧版本即使新值相同也 VersionConflict。合法 no-op 保存 completed receipt，但不改对象/generation、不产生 history/Audit/Outbox/Activity。MaxInt64 允许 no-op 和完成重放，真实改变为 RESOURCE_BUSY。

delete 是稳定 ID 的逻辑删除：版本+1、写 deleted_at、当前行清空 value/description；Get/List 排除墓碑。旧名称可给新 UUID 使用，旧 ID 不复活、不换 Project/type。delete 必须 expected_version，无 caller 时刻。新 key 删除已删除/不存在目标返回 NOT_FOUND；只有已完成同义同 key delete 返回原成功，不以“当前不存在”伪造幂等成功，不写第二事实。

create/update 历史 receipt 保留当时完整普通值，后续更新、删除、同名新建不改写，只对当前仍获准的同 User Owner 可见。不承诺删除即擦除命令历史/备份，不增加历史公开浏览。普通变量不提供 Secret 安全承诺。后继外域引用消费者必须先落实正式同 caller Tx 引用保护和删除责任，本卡不开放其绑定或绕过现 Secret/Credential 保护。

每 Project 最多4096条存活普通变量；满额 create 为 RESOURCE_BUSY、field `/variable_id:PROJECT_VARIABLE_LIMIT`，更新/删除不受此 create 限制。墓碑/commands/history 不自动清理，无保留期 job。

## 4. 迁移、锁及原子事实

00024 是 `-- agenteam:transaction tx` 的前向 Goose Up，新 schema `agenteam_projectvariable` 与本域 UUIDv7 domain，不改旧 migration。后继全局编号已分别预留00025给Knowledge、00026给Runner；本卡不提前消费尚未交付的后继迁移或据此扩大基线。最低表列如下：

- `variables`：id PK、project_id、type、name COLLATE C、description、value、version、created_at、updated_at、deleted_at nullable；UNIQUE(project_id,id)，存活 `(project_id,name)` partial unique。type 当前 CHECK 仅 variable；未来 Secret 须前向扩同表/payload shape，不能另造不共享唯一约束的名称目录。
- `project_generations`：project_id PK、query_generation 正 bigint；无行视为1，首次真实改变得2，每次真实 create/update/delete +1；no-op/replay 不变。
- `commands`：id（本域 Operation UUIDv7）PK、project_id、actor_user_id、command_name、idempotency_key、semantic_digest、target_id、request jsonb、state(planned/completed)、plan_revision、plan jsonb nullable、event_id nullable、receipt jsonb nullable、created_at、committed_at nullable；唯一 `(project_id,command_name,idempotency_key)`，CHECK 严格关联计划/完成/有无改变 receipt 与 event ID。
- `history`：id、project_id、variable_id、operation_id、version、kind(created/updated/deleted)、changed_fields、actor_user_id、occurred_at、event_id；operation_id、event_id、`(project_id,variable_id,version)` 各唯一，复合 FK 绑定本域 variable/command；仅安全字段名，不复制 name/value，无历史 HTTP。

持久 request/receipt JSON 各≤512 KiB，plan≤1 MiB，history/event安全payload≤8 KiB。SQL CHECK 防 NULL 三值逻辑绕过、字节上限/UUID/version/墓碑 shape；Unicode/presence仍由严格领域验证。对象 ID/project/type/created_at 不可变、墓碑不得再更新。无跨模块 Project 私表 FK，当前所属和权限由正式 Authority 同 Tx 证明。合法最坏 escaping 必须实际编码验证，不只算术证明。

只用现 LockKey：mutation 统一 command EX→User EX→Project EX，一次归一化 AcquireAll；User EX 满足 Activity，Project EX 同时保护名称、容量、对象版本、generation，并与 Project archive/Owner 变化串行。读用 User SH→Project SH，Lookup 再加 command EX，但 User/Project 为 SH。Outbox plan 锁先 union 后一次获取，不在 Tx 内降序加锁/SH升级EX，不增 aggregate kind 或借 CredentialLock。

命令先同 Tx 当前 Read 授权并查 completed 原回执；命中不受当前目标删除/版本改变影响。未完成才做 Mutate gate和version/名称/容量。真实改变先持久 planned（稳定operation/event ID、原intent、前后像、冻结DB业务时刻），出 Tx 后 PrepareAppend，不在 SQL callback做外部准备。final Tx 重取完整锁集、当前Session/Owner/Project/command revision与前像，写对象+generation+history、typed Audit+Outbox+Activity、completed receipt，全部同物理 Tx，任一步失败全回滚。

计划间其他命令改目标，final VERSION_CONFLICT，不改原 expected_version或自动rebase；名称和归档也重核。原 planned 可经相同意图显式尝试，但不满足前像仍明确拒绝。`in_progress` 只表示观测到未完成意图，不保证后台worker在执行，本卡没有worker。

对象/history/event 用计划同一冻结DB业务时刻；command.created_at/committed_at记录各持久阶段。Audit created_at、Activity沿现端口自取时，Account原60s节流保持，不承诺同微秒或每次更新。no-op只完成命令，不虚构业务改变。

### 4.1 Outbox 与 Audit 来源证明

普通变量是持久配置，真实创建/改变/删除各产生一次安全 Audit 与配置事件以供 Owner 追溯。读/Lookup/no-op/replay不增加。Outbox producer=`projectvariable`、event=`project.variable_changed`、aggregate=`project.variable`、schema_version=1，Project scope，aggregate ID=VariableID、version=结果版本。payload恰 `variable_id,operation_id,change,changed_fields`；change=created/updated/deleted，创建字段 `[created]`、删除 `[deleted]`、更新为排序唯一的 name/description/value子集。无原值及可枚举值摘要。

Audit action=`project.variable.create|update|delete`、producer=`projectvariable`、resource_kind=`project_variable`；metadata恰 `variable_id,version,changed_fields`。仅当前Human、Project scope、Success、空associations、ordinal0，cause_ref为原CommandIdentity规范sha256摘要。00024精确扩现Audit action/producer/resource CHECK并保留旧闭集；contract NewEntry/DecodeMetadata与Project Audit HTTP/schema/现有客户端metadata decoder同时接入，不能让新增记录毒化已有分页。客户端仅扩本三种严格记录，不宽松接受未知action，也不预占client.ts、useSession或新变量界面。

事实 Authority 必须在 supplied Tx 验同Store、完整锁、原command/actor/revision、真实postimage/history/event与精确metadata。Project先核当前Owner/生命周期再分派确切producer。Outbox CurrentAccess/NewFact分别Read/Mutate，以issuer+purpose+summary绑定。仅同形UUID、producer字符串、伪cause或正确postimage但缺history均不能过关。任一真实Audit/Event/Activity端口缺失则DependencyUnbound，默认根不放成功替身。

## 5. 当前权限、原意图和 Unknown

| 当前事实 | Get/List/Lookup/完成同义重放 | 新写或未完成计划 |
| --- | --- | --- |
| 当前Human Owner、有效Session、initialized active | 允许且校验scope/目标 | 按版本和字段规则 |
| initialized archiving/archived | 允许读与历史receipt | PROJECT_NOT_ACTIVE |
| pending initialization或deleting | PROJECT_NOT_ACTIVE，无历史旁路 | 同左 |
| 其他User（包括系统管理员）、跨Project目标 | NOT_FOUND，不泄露对象或命令存在 | 同左 |
| 撤销/过期Session、当前User不存在 | 原认证错误与HTTP清Cookie规则 | 同左，final同Tx重核 |
| AgentRun/Service/System scope | 不提供本卡能力 | AgentRun沿Project原DEPENDENCY_UNBOUND，Service为FORBIDDEN；不扩大Actor许可 |

Account没有disabled User业务，本卡不造字段/假端口声称已验。预认证后、prepare后撤销Session/Owner变化/归档使用真实锁竞争验证，不能靠HTTP第一次认证缓存判最终权限。

摘要绑定command、Project、target、actor UserID、expected_version的presence与值、规范request的presence与内容；不绑定SessionID/CSRF/RequestID/JSON成员次序。create不含expected_version，update/delete必须含。key空间为Project+command，同空间改target/字段/version/User为IDEMPOTENCY_KEY_REUSED，先当前授权、不泄露旧内容；不同command是独立正式命令空间。同User新Session保存原材料可恢复，失权旧Owner不可读。

VariableMutation wire按command闭合：create/update恰 `{command,changed,variable,event_id,audit_id}`；delete恰 `{command,changed,deleted,event_id,audit_id}`，deleted恰 `{id,project_id,type,version,deleted_at}`。create/delete changed=true；update no-op changed=false且event_id/audit_id明确null；真实改变两个ID有效。无key/digest/plan/Session/SQL。Lookup恰 `{status,receipt}`，status为committed/in_progress/not_observed，后两者receipt=null。历史receipt按原intent验证，不查当前值代替或否定。

Store Unknown保留原Attempt/Cause、不重跑SQLcallback。允许服务最多3s独立只读确认ctx查询相同canonical command；该尾由call owner跟踪，Stop取消、Drain实际join，并继续核当前Session/Owner。只有合法completed同义receipt确认成功。not_observed/in_progress/确认超时/当前拒绝都不证明原未提交，返回COMMIT_UNKNOWN、commit_state=unknown、retry_hint=lookup并保留因果。明确NotCommitted才按其实际错误返回。HTTP不自动重放/换key/version；真实响应丢EOF仍从原intent恢复，当前GET相同不是commit证明。

## 6. HTTP、分页、错误与安全输出

基路径 `P=/api/v1/projects/{project_id}/variables`，只认canonical UUIDv7及精确rawpath，不redirect/path.Clean、不抢原Project/Work/Model/Usage/Audit路由。

| Method/path | 输入与结果 |
| --- | --- |
| GET/HEAD P | 仅limit/cursor；200摘要页 |
| GET/HEAD P/{variable_id} | 无query/body；200完整Variable |
| POST P | `{request:{variable_id,name,description,value}}`；200 create receipt |
| PATCH P/{variable_id} | `{expected_version:"1",request:{...}}`；200 update receipt |
| DELETE P/{variable_id} | `{expected_version:"1"}`，无request字段；200 delete receipt，不用204丢回执 |
| POST P/commands/lookup | create恰 `{command,request}`；update恰 `{command,target_id,expected_version,request}`；delete恰 `{command,target_id,expected_version}`；200 Lookup |

写和Lookup需真实Cookie Human、Host/Origin/Fetch-Metadata/CSRF及Idempotency-Key，Lookup用原key。只读无必需key。顺序沿Account CheckRequest/RequireHuman→解码→库同Tx重核，不提前Project Mutate检查破坏归档恢复。已有资源错method为405和精确Allow，未知路由404；HEAD无body但完成相同认证/I/O收尾。

JSON input≤1MiB，detail/mutation/Lookup output≤1MiB，list≤5MiB。拒绝未知/重复（含escaped同名）/大小写别名key、无效UTF-8/孤立surrogate、null混淆、尾随JSON、非application/json、非UTF-8 charset、Content-Encoding。version只canonical decimal string。GET/HEAD实际body最多探测1B拒绝，不无界drain；详情/写/Lookup禁RawQuery/ForceQuery。

list query原文≤32KiB，limit默认50/显式canonical 1..100，cursor≤8192B。未知/重复decoded key、空值、无等号、非法百分号/UTF-8/NUL/分号、裸`?`拒绝，没有名称搜索或隐藏filter。SQL按 `(name COLLATE C ASC,id ASC)` keyset与LIMIT limit+1，只取摘要列，不载全量value再切片。cursor绑定格式版本、kind=`project.variables`、UserID、ProjectID、type=variable、固定排序、最后真正返回name/id、同Tx query_generation；limit不绑定。同User新Session可续，任意真实变量改变（包括value）使旧cursor失效，no-op/replay不失效。错签名/绑定/position为CursorInvalid，generation变为CursorStale，不能退回首页。先当前授权再验cursor。空/末页不含next_cursor，有next_cursor须满页；wire `{items:[],next_cursor?:string}`，坏末项不得发布半页。

| 条件 | HTTP/code/安全字段 |
| --- | --- |
| 字段/保留名/JSON/坏cursor | 400 INVALID_ARGUMENT或CURSOR_INVALID；保留名 `/request/name:RESERVED_NAME` |
| 当前权限 | 原401/403/404 UNAUTHENTICATED/SESSION_REVOKED/FORBIDDEN/CSRF_FAILED/ORIGIN_DENIED/NOT_FOUND |
| version/归档/改义/旧页 | 409 VERSION_CONFLICT/PROJECT_NOT_ACTIVE/IDEMPOTENCY_KEY_REUSED/CURSOR_STALE |
| 存活name冲突 | 409 RESOURCE_BUSY，`/request/name:NAME_CONFLICT`，不回显占用记录 |
| 容量或版本/generation耗尽 | 409 RESOURCE_BUSY；PROJECT_VARIABLE_LIMIT或VERSION_EXHAUSTED固定字段码 |
| 长度/media | 413 PAYLOAD_TOO_LARGE / 415 UNSUPPORTED_MEDIA_TYPE |
| 缺依赖/存储/Unknown/关停 | 原503 DEPENDENCY_UNBOUND/DEPENDENCY_UNAVAILABLE/COMMIT_UNKNOWN/SHUTTING_DOWN，保留实际commit_state/hint |

复用公共Problem，不新增Foundation code。不把明确输入/DB失败都归Unknown，也不把未确认COMMIT写成NotCommitted。库已完成但receipt校验或输出失败时abort，不发布宣称未写入的Problem。正式Draft2020-12 schema验证真实encodeJSON正反样本及union、最大输入输出，不只测手写成功fixture。

日志仅route模板、request_id、安全Fault code/state、数值统计；禁key/cursor/rawquery/name/description/value/request/receipt/SQL/CSRF/cookie及值摘要。DTO顶层/enclosing/错误链/根真实出口都用canary验证。普通值只进当前授权detail/mutation/Lookup body，Audit/Outbox仅上述安全metadata。

## 7. 默认根与实际退出

装配顺序：Store→变量事实Authority→同一Project Authority AuditFacts→同Catalog VariableEvents→同Outbox Producers→变量Service（同Audit/Account Activity/cursor）→真实HTTPBoundary→精确路由。`app/project_usage.go`在既有构造入口内部先建立变量事实Authority，注入同一Project Authority并将确切对象随装配结果交回；保持现有入口签名，不另建ProjectAuthority绕Audit；`account.go`将同Service owner安装进现工作集合。部分构造失败、Stop/晚到install竞争必须退役新owner，不留不受根管理的入口。

read/Lookup总2s、mutation总30s，从认证前开始并继承更早deadline。私有I/O沿Work已验模式：实际Body Read/Close、完整UTF-8/JSON/编码、Write/Flush、取消回调join、清deadline；unwrap最多64层，循环/缺能力明确abort。取消/短写/Close/Flush/deadline失败/panic不遗弃goroutine；3s确认尾可实际继续但不延长HTTP发布期限，wrapper超时不能释放仍在跑的业务owner。

Service统一跟踪读/写/Lookup/确认。Stop禁止新admission并取消全部现存ctx与确认ctx，Drain仅每个调用实际返回后成功；根Force沿原总清理ctx，不另起后台无限等待或新预算。Joined必须实际owner与I/O回调结束，不能以当前进程不存在或另一个早过期布尔代替正在使用的DBctx事实。

## 8. 完整验收与资源

独审SPEC接受后实施；首片段可以持久服务闭合保存，但本卡须HTTP/defaultroot收敛才是完整结果。未参与实现者做独立验收。一次组织完整矩阵，不以逐轮补零散top代替整体设计。

| 层 | 关键判据 |
| --- | --- |
| 纯契约/schema | name大小写/保留前缀/UTF-8/NUL/escaping、presence/null、ID区别、version边界、clone/enclosing日志、receipt原intent；最大100项摘要/完整value实际编码及坏末项拒绝 |
| 迁移 | 空库、00023含旧Work数据升级、重复启动、故障回滚/journal；CHECK/partialunique/FK/墓碑不可变真实PG，旧Audit记录仍可读 |
| 持久闭环 | create→page/detail→rename/valueupdate→no-op→delete；精确version/generation/commands/history/Audit/Outbox/Activity；同名新UUID/旧ID不复活/空值/4096容量；每阶段SQL故障全回滚 |
| 竞争 | 两key同expected一赢家；同key同义/改义；同名create/rename；delete对update及同名重建；Session撤销/Owner变化/BeginArchive双提交顺序；真实PID/pg_locks/blocking_pids/barrier，不sleep冒竞争 |
| 分页 | 101+真实记录、C序/页边/跨Project/User/换limit/同User新Session；篡改/变更失效/no-op不失效；Rows取消无半页且writer真实获锁完成 |
| 恢复/Unknown | 三命令真实成功响应截断→新Session原Lookup/显式原replay；后续改/删仍旧receipt、无第二事实；实际完整COMMIT帧未转发/提交丢响应、内建确认成功/未观测/未完成/取消超时与attempt/cause保留 |
| HTTP权限 | 真Account登录cookie/CSRF、他人/他人admin、撤销/过期、错parent、pending/deleting、归档历史恢复vs新写、严格method/path/body/query；不手seedSession证明正例 |
| native/root | 真net/http读/写/Flush/Close/取消/join与自然2s/30s；真默认root六能力及HEAD、ProjectAudit消费新记录、原路由回归；进行中Read/mutation/COMMIT确认的Stop/Force/Joined、晚装配/部分失败 |
| 独立 | 未参与实现者自行验证权限/原intent/Unknown/真实根确认退出，自己读持久事实，不以作者success布尔或httptest-only代实际root |

PG复用 `tests/testsupport/postgres`，HTTP真实Account/Project准备参考 `tests/work/work_owner_http_fixture_test.go`，不得直接消费该包私有helper；完整帧代理参考 `tests/work/task_concurrency_test.go`。必要跨包窄helper另报写域，不复制监督器。

默认root复用现 `scripts/test-objects.sh`→outbound→postgres七资源链、显式预编binary/cwd/selector、已验root_chain_driver/pg_only_supervisor。扩精确selector闭集须取得工具唯一写权。库层复用原driver实际配置：Go `-test.timeout=6m`，但受driver整体105s context约束，另15s cleanup，supervisor123s+3s退役及75s TCP尾；不因Go标志放宽真实窗口。此前卡中90s test是未运行前的文义错误，已按实际原源码纠正。root沿原每包6m与有界外层清理，不静默加时、不编造ID或缺manifest的退休。每轮实际子进程Wait、确认/回调join、自有ID/nonce标签、runtime及hostTCP双尾分别记录，root统一排资源窗。SPEC阶段不运行网络测试。

作者pure/race/vet、integration race-c/精确发现、两入口build与真实PG/native/root分别记结果，编译不是动态PASS。原FAIL保留、修后只跑影响范围；Model/UI/WIP不进正式候选。本卡完成也仅是普通Variable Owner后端，Agent F1、Secret、完整D10仍未完成。

## 9. 当前状态与下一步

SPEC 已获独立有限审查及版本输入差异复核接受，无未决 mustfix。§2.1 复用现成 CommandMeta.ExpectedVersion；初审“端口无法实现”的表述已纠正为本卡须明确单一版本来源，无新增版本参数。

首个可构建片段为四契约、Audit 三 action 的契约/现有 HTTP/schema/客户端兼容及必要纯测试。Go 三包 pure、七个新 Go top 的 race、前端两文件99项及严格类型检查实际通过；Audit HTTP 使用正式 Draft2020-12 与本地 common 引用。首轮 Go 缺显式 schema Python 环境的 setup FAIL、旧前端 Project 关系测试误将新变量 action 纳入的1项 FAIL均保留，修正前置/测试分类后的限定复验通过。第二片段已实现00024、领域六能力/调用owner/事实Authority及Project精确分派，并完成窄pure/race/vet。私有completed缺receipt负控首轮实际FAIL，补状态闭合后复验通过。SQL仅静态核原4个Audit CHECK闭集全部保留及事务标记，不冒真实PG语法/约束/原子事实验收。00024及Project精确事实分派另获独立有限静态审查接受，不扩为整个服务或PG验收。第三片段已接入六能力HTTP、正式OpenAPI与默认根同Authority/Catalog装配、精确路由和实际调用join；正式schema纯控、限定HTTP/root race、vet及两个入口离线build实际通过。HTTP I/O首轮两项测试把合法摘要description字段误判为私密诊断的FAIL保留，缩小至诊断前缀后的复验通过。该片段完成时尚无真实PG/native/root证据；后续实证见下文，不算完整产品交付。

首批 PG 验收源码已具备 Persistence、PaginationAndLimits、Migration、Atomicity 四个精确 top，真实 Account/Project 前置，race-c 与精确发现实际通过；其后 Persistence 完整动态通过，三top组合的实际结果见下文。Runner 前序核查发现原00013的 Project Audit guard 保留了全部 `project.*` 名称，因此原00024虽保留四个旧闭集，却遗漏新变量动作能否通过该 guard。00024已精确保留旧完整谓词并 OR 三个合法 action/producer/resource tuple，独立变量事实 CHECK 继续约束全部字段；修复获独立有限差异/126布尔对照接受，不能等同 PostgreSQL 验收。迁移 top 新增正式变量三命令、旧 Project Update 正例及五类 CHECK 拒绝样本；首次 fixture 机械别名替换/不当解引用的 compile FAIL保留，修后编译通过。根已将正式 ca9f2d5d 的有界 PG supervisor 单文件导入本树，沿原预算；该工具准备阶段未启动真实资源。

首 Persistence 已完整真实通过（业务9.41s），验证实际 Account 登录、普通变量CRUD、no-op、历史Lookup/同key重放及精确Audit/Outbox事实。Go/driver实际Wait、两自有资源双观察、后代空及hostTCP双尾均完成，输入不变；并非后续Migration/Authority/Unknown/native/root已通过。HTTP/root冻结15源另获未参与实现者有限静审和7项独立纯控制接受，无mustfix；尚无真实HTTP权限、native I/O或默认root动态证据。


事务核心四源与四契约已获另一未参与者有限静审；发现 Create 使用另一 Project 已占全局 ID 时误回 ID_CONFLICT。两项作者纯负控先实际失败，修后 Create 仅查自域 Project marker，将跨 Project 判为 NOT_FOUND；同 Project（含墓碑）仍 ID_CONFLICT，独立 INSERT 兜底只处理该主键的23505，普通 UPDATE/其他存储错误分类不变。领域限定 race 通过，返修获独立窄复核接受；未把源层修复等同真实并发通过。新增 Authority/Concurrency/HTTP/Recovery 验收源码覆盖当前权限、双提交顺序、原意图、完整 COMMIT 帧与取消 join，已形成12个可发现 top；全部新增动态仍待。Migration 的旧数据输入补用正式 Work CreateMilestone，核旧对象/命令/Outbox 与原回执升级后保持，未用 SQL 手种成功事实。

存储三top组合首轮完整真实FAIL，所有实际Wait/自有资源双清/desc/runtime/TCP双尾及输入不变均完成。PaginationAndLimits完整PASS（14.52s）；Migration的升级/重复/旧Work事实及约束子例通过，但回滚后改用另一checksum的测试重试触发MIGRATION_HISTORY_DIVERGED。Atomicity的21个注错子例均先在Activity前置UPDATE失败，实际只采DATABASE_SQL_FAILED，未采SQLSTATE，不能称触达业务注错；正确postimage缺history的producer拒绝子例通过。

仅修上述测试前置：迁移用缺失测试函数注错，补该依赖后始终重试同一source/checksum，核pending/applied journal、Goose及schema；Activity同时合法回拨issued_at/last_activity_at，保持Account时间约束与原60s节流刺激。产品及00024不改，原整轮FAIL保留。修后race-c与精确2top发现通过，第05编译的string/Digest机械错误保留并已修。下一只复验Migration/Atomicity，不重复已有效Pagination或Persistence。原driver的三top闭集例外已有15作者/10独立控接受；根另授权仅增两top固定字符串，18作者/10独立实际源输入控制通过、其余全文不变；两测试前置修复及driver增量获独立有限接受，不扩大原期限。修后两top完整真实PASS：Migration 12.08s，Atomicity 8.97s，21个AFTER ROW故障实际触达并证明原子回滚；同源迁移恢复及两次journal/Goose/schema后验通过。Go/driver实际Wait、两自有资源双清、desc/runtime/TCP双尾与输入不变均完成，窗口已释放；保留首组合整体FAIL及该轮独立分页PASS，不将修后证据回填原失败。原Authority的CommitResult测试API误用、Recovery的sealed Rows测试前置编译失败均保留，已限修后编译闭合。


默认根验收两源已离线闭合：app top 保留正式根装配、真实 Account Actor 与四个实际 BEGIN/PID 的 Get/List/Delete/Lookup，在 graceful/Force 下核取消、原共享 DB context/deadline、未提前 Joined 及真实返回后 join；它不把 absent Project 上的退出屏障冒成成功业务事实。process top 消费实际 cmd、正式 Cookie/CSRF、六能力和双HEAD、三原意图历史恢复、ProjectAudit三安全动作及旧相邻路由；另三命令各在真实 TCP 收到成功响应头/首字节后关闭而未观察完整正文/EOF，再由新真实Session原Lookup/显式同key重放核持久事实。此传输刺激与库HTTP的 ResponseWriter失败控制分开。两源race-c/精确发现与process TestMain两入口build实际通过，尚无native/defaultroot动态结论。原root链adapter仅增两个闭合selector→已有cwd，固定MinIO缓存按原SHA复用；作者纯输入控制通过；root两精确映射及native三精确selector/env增量已获独立有限控制接受，未扩大原资源/预算/退出语义，实际native/root窗口仍待。


Authority首轮完整真实FAIL（9.58s），Go/driver实际Wait、两资源/desc/runtime/TCP双尾及输入不变均闭合。当前Owner/跨Project（含Create全局ID）、新Session与原version、Owner失权、归档读与历史重放、私有记录损坏拒绝共五子通过；pending/deleting子例停于缺少正式Delete永久确认的测试请求，撤销/过期子例中正式Logout拒绝已过，过期实际UNAUTHENTICATED与错误测试预期SESSION_REVOKED不符。依据Project DeleteProjectRequest.Validate和Account loadCurrentSession的既有契约，仅补Permanent=true及过期精确预期；保留真实撤销码、原权限断言及原整体FAIL，不改产品。修后race-c/精确发现通过，两处正式契约对照获独立有限接受；Authority修后动态待验，不外推剩余竞争、HTTP、Unknown或native/root。随后Concurrency首次启动在既有5GiB磁盘余量门禁setup失败，未创建PG资源或运行测试top，driver实际Wait及desc/runtime/TCP双尾已闭合；保留该setup FAIL，磁盘恢复前不降阈值或自动重试。


恢复磁盘余量后，Concurrency在相同冻结输入下实际运行，整体FAIL（14.39s）：八个同key/改义/同版本/同名/rename/delete竞争子项通过，跨Owner与Project竞争同全局ID在真实transactionid等待后输家返回INTERNAL_ERROR，未满足NOT_FOUND。实际Wait与两资源/desc/runtime/TCP双尾完整闭合，原失败未采SQLSTATE，不作回填。源码核出D03会保留SQL错误poison并优先作为事务结果，原库内事后23505映射不能覆盖。仅将Create INSERT改为精确ON CONFLICT ON CONSTRAINT variables_pkey DO NOTHING，0row立即NOT_FOUND、无后续generation/history或外层Audit/Event/Activity，其他SQL错误及同Project前检不变；删除失效特殊错误映射。新增零行无后续事实纯控先实际失败，修后领域race通过，三源获独立D03接缝窄复核接受。当前matrix08已race-c及精确发现通过；其后同一Concurrency九子完整真实PASS（9.94s），保留原transactionid等待及loser原planned/零新增业务事实断言，Go/driver实际Wait与全部资源/runtime/desc/TCP双尾及输入不变闭合。原43471业务FAIL与磁盘setup FAIL继续保留，不将修后证据回填原轮。受产品闭包影响的native/app/process均已用当前源重新race-c/发现，process TestMain两cmd build通过，动态尚待。下一只组合Authority与FinalAuthorityCompetition，根授权精确白名单增量、19项作者输入控与精确list2/build通过，单源增量已获独立窄审接受，待freshgrant；原期限不扩、不重跑已过Concurrency。
