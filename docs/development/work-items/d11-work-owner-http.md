# D11 Owner Work Planning HTTP 与默认生产根

修订：rev2，2026-10-09，**SPEC已独立接受，分页及错误投影实施中；HTTP/root尚未完成或动态验收**。

## 1. 完整结果与真实前置

已有有效 Project 的当前 Human Owner 可通过正式认证 HTTP 创建、修改、排序 Milestone/Sprint，创建和整理未指派 backlog Task，并记录、分页查阅、解除两类 Blocker。响应丢失后可以保存的原意图查证原命令，再显式同义重放。默认 Central 根实际构造并路由这些服务，权限、持久事实与退出链均使用真实实现。

本卡消费 [Structure](d11-work-structure.md)、[Task Planning](d11-task-planning.md)及正式交付 `0b44e26a` 的 [B0-P](d11-task-blocker-service.md)。三张卡的业务、字段、版本、排序、历史与失败组合保持有效。本卡新增 HTTP 消费和必要的 Blocker 分页读取，不复制领域状态机，也不将既有库验收称作新 HTTP 验收。

| 真实前置 | 本卡接入 | 保留边界 |
| --- | --- | --- |
| Account HTTPBoundary、Session/CSRF、Activity、单层 RequestID 与安全 Problem | 实际认证、当前 Human、同事务 Activity 与安全 HTTP 输出 | 管理员不获得 Owner 旁路；不用 SQL 手种 Session 证明正式登录 |
| Project Authority、已验 Owner read/update 默认根 | 同 Store 的当前 Owner/Read/Mutate、Project 门禁 | 创建 HTTP、真实 Skills 初始化及完整 lifecycle/cleanup 仍未绑定 |
| Work 三套命令/Lookup、Structure/Task Reader、四组精确 producer gate | 正式库调用及历史结果，不直接改业务表 | 仅既有 Human 规划命令；Task 状态转换、指派、Executor/Scheduler 未接入 |
| Outbox Catalog/Appender、Account root 生命周期、cursor Keyring | 一个 Catalog、一个真实 Work Authority、实际 Stop/Drain/Join、有签名分页 | 不新增 worker、事件消费者、通用框架或迁移 |

Project 必须已存在且 initialized；从新账号创建 Project 的产品链仍缺真实 Skills/创建 HTTP，不能用本卡 fixture 填补。没有新 UI、完整 Timeline、Task 删除、Sprint start/complete/rollover、Agent 自动阻塞/恢复或生产部署。保留 `ready=false`/503、Object runtime join、OpenAI tools 独验、Central SPA 发布和 Jina/Image 来源停止项。

## 2. HTTP 资源与原命令意图

以下 `P` 恰为 `/api/v1/projects/{project_id}`。所有 path ID 使用 canonical 小写 UUIDv7；正文不再接受 ProjectID。既有资源 ID 取 path；create 的新 Milestone/Sprint/Task/Blocker ID 沿原契约由调用者在首次发送前生成并保存，重试不换 ID。

| 方法与路径 | 消费能力 / 请求 |
| --- | --- |
| GET/HEAD `P/milestones` | `ListMilestones`，摘要页 |
| POST `P/milestones` | `CreateMilestone` |
| GET/HEAD `P/milestones/{milestone_id}` | `GetMilestone`，完整当前值 |
| PATCH `P/milestones/{milestone_id}` | `UpdateMilestone` |
| POST `P/milestones/{milestone_id}/reorder` | `ReorderMilestone` |
| GET/HEAD `P/sprints` | `ListSprints`，必须给 `milestone_id` query，摘要页 |
| POST `P/sprints` | `CreateSprint` |
| GET/HEAD `P/sprints/{sprint_id}` | `GetSprint`，完整当前值 |
| PATCH `P/sprints/{sprint_id}` | `UpdateSprint` |
| POST `P/sprints/{sprint_id}/reorder` | `ReorderSprint` |
| POST `P/structure-commands/lookup` | 六个 Structure 命令的原意图 Lookup |
| GET/HEAD `P/tasks` | `ListTasks`，严格 filter，摘要页 |
| POST `P/tasks` | `CreateTask` |
| GET/HEAD `P/tasks/{task_id}` | `GetTask`，完整当前值 |
| PATCH `P/tasks/{task_id}` | `UpdateTask` |
| POST `P/tasks/{task_id}/reorder` | `ReorderTask` |
| POST `P/task-commands/lookup` | 三个 Task Planning 命令的原意图 Lookup |
| GET/HEAD `P/tasks/{task_id}/blockers` | 新同事务分页 Reader，完整 Blocker 页 |
| POST `P/tasks/{task_id}/blockers` | `AddTaskBlocker` |
| POST `P/tasks/{task_id}/blockers/resolve` | `ResolveTaskBlocker`，原 request 中持有 blocker_id |
| POST `P/tasks/{task_id}/blocker-commands/lookup` | 两个 Blocker 命令的原意图 Lookup |

全部写操作与三个 Lookup 要求恰一个合法 `Idempotency-Key` header，1–128 字节闭集沿 Foundation。它是首次命令的原 key；Lookup 不创建自己的 key。不得 trim、拼接重复值或采纳 query/body 中的另一份 key。RequestID 只取原外层服务端 middleware；缺失则拒绝，不信任用户 trace header，不再套第二层 middleware。

### 2.1 mutation 封套

每个 create 正文恰 `{ "request": <原 Create DTO> }`，`expected_version` 不得出现。每个 update/reorder/add/resolve 正文恰 `{ "expected_version": "1", "request": <原 DTO> }`。Blocker add 不是 Task create，仍必须携带当前 Task 的 expected_version。version 使用 `1..9223372036854775807` 的 canonical 十进制字符串；显式 null、number、指数、前导零与溢出拒绝。不能在 HTTP 排除 MaxInt64：历史重放或合法 no-op 仍由库决定。

`request` 必须为非 null 对象，交原正式 DTO 的严格 UnmarshalJSON/Validate：保留 optional 字段的 presence、原默认展开、空串清空、before_id 省略表示组尾等语义。不得先 Get 当前对象补齐字段，不接受 caller rank、Actor、scope、额外 ProjectID 或隐含默认 type/priority。Task create 的 initial_state/assignee 输入沿原库明确否定分支，不能悄悄忽略后创建成功。

整份 HTTP 正文上限 1 MiB；内层 request 仍受原 DTO 自己的 cap 限制。该额外封套预算不会放宽解码后业务字段。外层和内层均拒绝未知/重复/大小写别名 key、非法 UTF-8/孤立 surrogate、尾随 JSON、非 JSON media/charset、Content-Encoding。不得仅依赖 `encoding/json` 大小写匹配或 `*string` 识别 null。完整最坏 escaping 的合法请求须可到达原服务。

### 2.2 Lookup 在断连前即可保存的输入

Structure/Task Lookup 的正文是原意图，不要求客户端自己实现 Go canonical digest：

- create：恰 `command,request`，request 是原 create DTO，自带原新 ID；
- update/reorder：恰 `command,target_id,expected_version,request`，target_id 是原 mutation path 中的同名类型 ID；
- Blocker Lookup：TaskID 已在 path，正文恰 `command,expected_version,request`，request 是原 add 或 resolve DTO。

`command` 使用原库的完整稳定命令名，仅允许本路径对应的六/三/两名。Lookup 与 mutation 共用原 DTO 解码和默认展开，构造当前 Human + 原 version/request/target 的 CommandMeta，调用原公开 `*Digest` 函数，再调用对应 Lookup 一次。RequestID 是本次 HTTP 的 ID，但本来就不参与业务 digest。新 Session 仍属同 User 时可复用原意图；不同 User 不能借原 key 获取 receipt。不得暴露任意 namespace/摘要查询或依赖首次成功响应才取得恢复材料。

Lookup 是当前 Read，但 unsafe POST 仍需 CSRF。不执行 command、不自动重试写、不重新生成 ID/key，不把当前 Get 的 version/rank/字段当原意图。`not_observed` 不保证另一个请求没有未知提交，也不授自动重执行；`in_progress` 不是成功 receipt。原 intent 不同仍沿领域幂等冲突，不能把 conflict 转成新命令。

## 3. 查询、投影与有界分页

所有 GET/HEAD 禁止实际 Request.Body 可见的实体正文，包括声明 Content-Length=0 但所给 Body 仍有字节的适配器；最多探测一个字节，不用无界 drain。native net/http 已将CL=0之后的线路字节按连接framing解释，handler不得读取底层连接来吞掉下一条pipelined请求。详情GET/HEAD、mutation及Lookup均禁止非空RawQuery与ForceQuery。列表 query 总原文上限 32 KiB，cursor 解码后 ≤8192 B；重复 key、重复 decoded key、未知 key、空值、非法百分号/UTF-8/NUL、`;`、无 `=` 和裸 `?` 拒绝。不 trim/合并查询值。

列表统一 `limit` 省略为50，显式必须 canonical 十进制1..200；`cursor` 省略为首屏。Milestone 只接受 limit/cursor；Sprint 额外恰一个必须的 milestone_id；Task 额外允许原 `state,priority,type,milestone_id,sprint_id,text,assignee_agent_id`。Task assignee 省略为不限，值 `null` 为未指派，否则为 canonical AgentID；不接受空串/其它 sentinel。text 沿原 ValidateTitle 和原查询语义，不写新的全文搜索。Blocker 额外允许 `status`，省略明确展开 unresolved，显式仅 unresolved/resolved/all；展开后的 status 参与 cursor 绑定。

详情返回已授权的完整 Milestone/Sprint/Task DTO。列表是明确的摘要表示，不是假完整对象：

| 列表项 | 恰有字段 |
| --- | --- |
| MilestoneSummary | id, project_id, title, manual_rank, version, created_at, updated_at |
| SprintSummary | id, project_id, milestone_id, title, state, manual_rank, version, created_at, updated_at |
| TaskSummary | id, project_id, milestone_id, sprint_id, title, type, priority, state, assignee_agent_id, manual_rank, version, created_at, updated_at |
| Blocker | 原 TaskBlocker 全部字段，包括明确 nullable 的 resolution/actor 字段与两类 metadata |

详情能读取完整 description/plan；摘要不输出空串冒充正文，不增加假 history/blocker/execution 数组。合法读取的非 backlog 或已指派 Task 保留原事实，不改写；这不开放对应 mutation。所有 list 输出恰 `{items:[...],next_cursor?:string}`，items 永非 null，无下一页时 next_cursor 缺席，不能空串/null。

单详情/mutation/Lookup 输出上限1 MiB，列表上限5 MiB。每个摘要保守上限16 KiB；200×16 KiB加cursor/封套小于5 MiB；Blocker单条既有16 KiB约束也满足同一页预算。必须实际编码/反解最大合法 title/description/plan/Blocker文本和满200项页，不能仅算术断言。原库 full Task/Structure 页只用于受授权的服务返回，本 HTTP 不序列化其大正文页，也不裁 items 后沿用错误 cursor。

投影先验证原 typed value/union，再验证本次 Project/target/parent、list filter、项数及有序唯一结果；错误依赖返回不发布任何成功候选。Mutation/Lookup 校验原命令、原 target、原 request 所能证明的字段和原 version关系，不额外查询当前对象以否定历史 receipt。Structure/Task no-op 允许原 expected version，真实改变是+1；create为1，Blocker始终+1且不能溢出。Lookup committed receipt也按保存的原意图验证。

三个 Lookup 保留库的闭合 wire：Structure 为 `{state,result}`，Task/Blocker 为 `{status,receipt}`；committed时值必有且合法，in_progress/not_observed时值明确null。其余 key 禁止。Mutation保留原 StructureMutation/TaskMutation/TaskBlockerMutation 的安全业务字段与事件ID，不序列化 private plan、operation revision、key、semantic digest、Session、CauseID或SQL。三种结果不强行合成无消费者的新通用 receipt。

### 3.1 消费者需要的 Blocker 分页口

在 `work/contract/task_blocker_query.go` 新增唯一窄接口，不修改既有全量 `ListTaskBlockers`：

```go
type TaskBlockerPageReader interface {
    ListTaskBlockersPage(context.Context, identity.Actor, ProjectID, TaskID,
        TaskBlockerStatus, foundation.PageRequest) (foundation.Page[TaskBlocker], error)
}
```

`work.NewBlockerReader(store Store, authority *Authority, keys cursor.Keyring) (*BlockerReader,error)` 为纯构造，拒绝 nil/不同 Store/无效 keyring；同根已绑定的 Work Authority 必须原对象复用。只提供上面一个公开读取方法。使用原 Store、scanBlocker 与当前 Project 授权，不跨域改表、不新增迁移。

一次 Tx 内取得全锁 union：User SH、Project SH、Schedule SH、目标 Task SH；先当前 Session/Owner/Read 门禁，再读取本 Project 下 Task 当前 version及匹配 Blocker。order固定 `(created_at ASC,id ASC)`，SQL使用复合keyset和LIMIT limit+1，只扫描这页，不先读4096条后切片。保留历史总数≤4096的内部一致性检查可用 count，不加载所有正文。

cursor 复用现有签名 Keyring，scope=Project，query digest绑定 format/kind=work.task-blockers、owner UserID、ProjectID、TaskID、展开后的status；order固定，position仅最后返回条目的 Instant与ID，OrderGeneration=同Tx Task.version。续页先当前授权，再验签/绑定/position、比较当前Task.version；变化返回原 CursorStale，错签名/错scope/filter/类型返回 CursorInvalid。无效/失效均不得降成首屏。跨Owner转移导致权限失败；同User新Session可续页。limit不进入绑定，可以安全换页尺寸。

add/resolve及真实 Task字段/业务排序改变都会推进Task.version，因此保守使旧Blocker cursor失效；仅 sibling rank重整不改此Task业务version，且不改变Blocker集合/顺序，无需使其失效。空页/末页无next_cursor；有lookahead时以最后真正返回项签名，不能使用lookahead项。每页独立授权，当前已归档仍沿Read可见，uninitialized/deleting拒绝。所有Rows、Tx和临时结果在返回前真实关闭，错误时不返回部分页。

## 4. 身份、当前门禁与错误

顺序固定为原 Account CheckRequest/Host/Origin/Fetch-Metadata、安全头与实际RequireHuman，再解码合法输入并调用库；库继续在自己的事务里取得当前User/Project锁并核Owner。HTTP不直读Account/Project表、不注入Service/Agent actor、不用缓存授权替换事务事实。

| 当前事实 | 读取 / Lookup / 已完成同义replay | 新写或未完成planned |
| --- | --- | --- |
| 本人、有效Session、initialized active | 沿原领域能力允许 | 沿原version/状态/容量/图/parent规则 |
| initialized archiving/archived | Read可见，保留原历史receipt | ProjectNotActive，不能重新完成planned |
| pending initialization或deleting | ProjectNotActive，无历史旁路 | ProjectNotActive |
| 他人Project（包括管理员）、错误父子目标 | 原NotFound/授权错误，不泄露内容 | 同左 |
| 无效/撤销Session或停用User | 原认证错误、清Cookie规则 | 同左，事务重核不得漏掉竞态 |

不得在调用库前加Project Mutate预检查，破坏Read→原回执→新变更门禁顺序。两阶段计划之间的撤权/归档与同key旧revision竞态沿原行为：Blocker首次并发调用可能 FORBIDDEN，但可用当前权限下Lookup/原意图重放恢复，不能把所有首次调用强改成成功。

错误沿 Foundation Fault/Account Problem：400输入、401认证、403CSRF/Forbidden、404不可见目标/未知路由、405方法、409版本/状态/幂等/依赖环/光标失效、413原文超限、415media、503依赖/最终Unknown；不把所有取消/PG错误归到一个码。保留原 `/blocker_id:BLOCKER_HISTORY_LIMIT` 等字段信息及真实commit_state。底层已经返回成功而mutation结果校验失败时直接abort，不生成NotStarted/NotCommitted Problem；read/Lookup坏投影可返回DependencyUnavailable，不能伪造receipt或改成not_observed。

rev1独审发现 Foundation 的以下九码已 Known，但公共 `httpapi.problemKinds` 与 `common.json` 尚未登记，不能直接接入。本卡补最小映射，保持原领域Fault和安全投影规则：

| Code | HTTP / title | 固定默认detail |
| --- | --- | --- |
| TASK_NOT_FOUND | 404 / Task not found | The requested task was not found. |
| TASK_VERSION_CONFLICT | 409 / Task version conflict | The task changed. Read it again before retrying. |
| TASK_STATE_INVALID | 409 / Task state invalid | The task state does not permit this action. |
| TASK_ASSIGNEE_REQUIRED | 409 / Task assignee required | This action requires an assigned agent. |
| TASK_SPRINT_INVALID | 409 / Task sprint invalid | The task sprint does not permit this action. |
| TASK_TERMINAL_IMMUTABLE | 409 / Task terminal immutable | A terminal task cannot be changed. |
| BLOCKER_NOT_FOUND | 404 / Blocker not found | The requested blocker was not found. |
| BLOCKER_ALREADY_RESOLVED | 409 / Blocker already resolved | The blocker is already resolved. |
| TASK_DEPENDENCY_CYCLE | 409 / Task dependency cycle | The task dependency would create a cycle. |

TaskVersionConflict在没有合法显式hint时默认`reread`；其它八码不新增默认重试提示，沿现有合法hint透传。CommitUnknown仍强制`lookup`。沿原SafeMessage、合法FieldErrors、四种CommitState、RequestID/HEAD/无cause规则，不格式化err.Error。未知非Known code本来经Code.Safe映射INTERNAL_ERROR/500，本卡保持该回退；九码补齐后没有新增Known缺映射分支，不为未来假设增加通用registry或改旧异常语义。九码逐项投影与schema验证及原unknown-code回归是必要验收。

已知资源的不支持方法返回405与精确Allow；GET资源也支持HEAD、无正文。未知canonical路由404；非canonical path/RawPath沿Account既有400，不redirect、不做path.Clean后路由。dispatcher只认本卡明确子资源，不抢Owner/Model/Usage/Audit/Project update及原commands/lookup。

## 5. native I/O、取消与输出安全

沿已验 [Project Owner HTTP](d08-project-owner-update-http.md)的同一总预算原则：read/Lookup总2s，mutation总30s，从认证前开始并继承更早parent deadline。私有Work I/O adapter持有原body和native Read/WriteDeadline、Flush能力；解wrap最多64层，无能力或循环则abort，不另建通用HTTP框架。对应能力在任何认证/业务前解析。

预算覆盖认证、body读取、库调用、完整编码、body真实Close、全部写入/Flush、取消回调join和清deadline；只Close一次原body，不把MaxBytesReader当另一个资源。拒绝请求也不能以提前Problem绕过body/回调所有权。成功清deadline前必须已完成body Close、停用未启动回调并实际join已启动回调；clear-deadline失败同样abort。超时、取消、short write、Flush/Close失败或意外panic走 `http.ErrAbortHandler`，原外层Recover不得再写无界替代Problem。没有为逃脱阻塞而遗弃的goroutine。正式1MiB/5MiB响应完整编码并通过校验后才写200、`application/json`、`Cache-Control:no-store`和准确Content-Length；HEAD无body，仍完成安全头/Flush。

领域自有提交确认可能在原ctx取消后继续其已定3s上限；同步请求仍须拥有并等其实际返回。该tail不延长HTTP发布期限。超时后不得把未知结果伪写成NotStarted/NotCommitted或200。库返回带Unknown的原Fault时保留Cause/Attempt内部链，wire只呈公共Problem；业务已经完成但输出无效/无法完整发布，客户端仍按原意图Lookup恢复，不能声称HTTP错误撤销了提交。

日志只含稳定route模板、服务端request_id和既有安全Fault元数据。禁止key、cursor、query、标题/正文/plan/Blocker文本、请求/receipt JSON以及DTO enclosing fmt/slog回退；沿Task卡已知enclosing泄露边界检查真实出口。使用专用canary验证所有成功/拒绝/Unknown/abort出口，不记录真实凭据。

## 6. 默认根与真实退出

默认根使用现同Store的 Project Authority 创建一个Work Authority，在同一Catalog注册 RegisterWorkEvents/RegisterTaskEvents/RegisterTaskBlockerEvents。构造Outbox之前绑定 `Producers[WorkProducer]` 到该确切Authority；沿Project现有精确四事件gate，不放大到任意work前缀。

构造一个Structure Reader，再构造Structure/Task/Blocker三个命令Service、Task Reader和新Blocker Reader；使用同一真实Account Authority作为Activity、同一个Outbox Appender及现CursorKeyring。HTTP公开构造为 `workhttp.NewHTTPHandler(bindings Bindings,boundary *account.HTTPBoundary) (http.Handler,error)`；Bindings恰六个具体指针字段 Structure、StructureReader、Tasks、TaskReader、Blockers、BlockerReader，分别对应上述六对象，任一nil拒绝。`HandlesPath(path string) bool`只分发本卡明确资源形状；Actor、原Request和URL不重建。私有测试接口可替身验证坏投影，但默认根不得安装fixture或空端口。构造只做绑定，不增加初始化/后台恢复，不接通缺失Skills、Lifecycle、Resolution或Invocation。

新增Work bundle纳入 `accountAssembly.works()`，排在Account core/Activity退休前。StopAdmission先对三个Service全部Stop；Drain先Stop全部，再在原剩余ctx内逐个等待真实Drain。只有三者实际完成且partial construction结束才Joined；首个错误不允许Force漏取消后两个。Work没有Project式Force，bundle Force也必须实际调用所有Stop并尝试全部Drain，不能虚构方法或另给期限。

构造部分成功、install遇stop/Force、之后构造失败的每个路径都由原assembly持有已建Service并停止/等待；不得在local变量里遗失已建实例。纯Reader由现根HTTP active计数与真实HTTP join保护，三个命令Drain不能代替读取Rows/body/取消回调退出。正常graceful路径在HTTP、Account/Work与Outbox真实join后才正常退休ObjectGuard/DB；Force路径沿原共享截止时间启动全部取消，期限耗尽仍必须走既有DB.ForceClose以打断底层调用。未join会阻止guard正常退休，但不能把DB强关永远卡在未join上，也不能把DB强关或进程退出写成Work已实际join。保留原退出失败/未join事实。本卡验证新增Work阻塞对既定顺序的影响，不改Object停止实现或宣称旧Object join缺陷修复。

## 7. 文件、实例与资源所有权

root已协调本树唯一写入 `internal/central/app/account.go`、`internal/central/httpapi/problem.go`、`api/openapi/common.json`、`docs/development/backend/README.md`、`api/openapi/work-planning.json`；不改通用router、依赖锁文件、Model生产服务或Model/D27 harness输入。公共common.json可能进入另一树动态输入闭包，写入须遵守root当轮freeze窗口；其已验旧schema行为保持。规格写者为service_delivery，实施实例blocker_implementation，独立SPEC及高风险动态验证为未参与实现的blocker_spec_review；具体实现文件可在本树串行交接，同一时刻只有一个写者。

| 范围 | 必要路径 |
| --- | --- |
| 正式规格与用户/开发入口 | 本卡；docs/development/backend/README.md；api/openapi/work-planning.json |
| 分页 | internal/central/work/blocker_page.go、blocker_page_test.go；internal/central/work/contract/task_blocker_query.go、task_blocker_query_test.go |
| HTTP | internal/central/work/http/handler.go、read.go、commands.go、wire.go、io.go 及对应_test.go；native边界独立 native_test.go |
| 最小公共错误接缝 | internal/central/httpapi/problem.go；新 work_problem_test.go；api/openapi/common.json仅补九个code |
| 根 | internal/central/app/account.go；新增 work_planning.go、work_planning_test.go、work_planning_process_test.go |
| 真实跨层测试 | tests/work/work_owner_http_test.go；必要新HTTPfixture独占 tests/work/work_owner_http_fixture_test.go；tests/process/work_owner_http_test.go |
| 可恢复独立输入 | .agent-state/work-owner-http/ 下必要probe/独立编译入口；复用现监督器，不复制产品镜像、旧日志或完整harness |

无新迁移；必要OpenAPI为正式手写schema，不生成前端资产。新增OpenAPI必须与真实输出按Draft2020-12/FormatChecker/本地refs验证，所有对象additionalProperties:false，版本/ID/nullable/union/presence闭合，不以正则文本搜索代schema验证。

PG/native HTTP/真实root资源必须等root分配独占窗口。Task Planning现有2ID PG-only driver用于分页/事务库PG，保持原105s与已编binary/精确selector；它不提供root/Object环境。真实默认root用 `tests/process/work_owner_http_test.go`，复用该包TestMain/launch/event/wait、databaseEnvironment和noCentralBackends；现 `scripts/test-objects.sh` 的既有Object→outbound→postgres资源链已包含tests/process，实际执行cmd/agenteam，无须因为tests/work不在硬编码清单而另造监督器。native测试复用既有Project native监听、EOF及actual-join范式。精确top/实际binary/argv/原预算与必要环境在编译冻结后、真实窗口前核对，零发现不能算通过。资源使用任务自有PG、现有真实Object启动依赖及确切ID，不连接dev infra；浏览器不是本卡必需。实际进程Wait、Rows/body/workerjoin、实际资源数量与TCP双尾须收齐，不能把后验clear补原终态；若现runner无法承载具体场景再协调最小适配，不预先扩大基础设施。

## 8. 一次对齐的验收矩阵

| 门槛 | 必须证明的外部/持久事实 |
| --- | --- |
| SPEC/公开接口 | 本卡独立接受后才实施；三组Lookup原意图可在首次发送前保存；所有route/enum/presence/nullable和预算闭合 |
| pure wire/schema | 全21能力、合法最大escaping、摘要满页和单详情、错误union/目标/filter、重复/未知/null/media/query/headers/path、Allow/HEAD、无数据日志；九码逐项及原unknown-code映射；真实body通过schema正反验证 |
| 真实分页 | 当前Owner且同Tx Task.version；相同created_at tie-break、首/中/尾/空页、不跳漏、换limit、status/scope/Owner签名隔离、add/resolve与Task update使cursor失效、同User换Session、撤权/归档/删除门禁、Rows取消真实退出 |
| 真实HTTP闭环 | Bootstrap/邀请/兑换/Login得到真实cookie/CSRF；已有Project经真实服务加test-only Skills receipt准备，不冒创建HTTP；21能力使用同根实际服务，读取同一持久Task/Blocker与历史/Outbox/receipt/Activity，无第二写入 |
| 权限/生命周期 | 未登录/撤销Session/停用User、异Owner管理员、跨Project父子；HTTP预认证后、prepare后安排撤权/归档真实竞争；archived Read/Lookup/完成replay通过，新写/未完成planned拒绝，pending/deleting拒绝 |
| 三域恢复 | 各一实际首次完整响应丢失后原body/key/target/version查证；当前内容已变化仍返回历史receipt，同User新Session可查；改义冲突、writer在途不假not_observed、COMMIT forwarded/unforwarded的Unknown与明确恢复；无自动重发 |
| native连接 | 自然2s读/Lookup和自然30s写代表，不以short-parent/PG1s lock_timeout替代；另验更早parent。slow read/write、body Close、Flush/短写、unsupported deadline/wrapper cycle、确认tail、clear失败；同连接在上次deadline已过去后下一次完整请求成功。成功实际底层EOF+完整Content-Length，失败为可判定abort/不完整响应；LimitReader恰读满不证明EOF，body/callback须真join，不能靠Recorder或response事件代替 |
| root 生命周期 | 默认根真实路由/cookie、唯一Catalog与Workproducer；命令/确认/纯Reader分别被hold时Stop先取消，graceful实际join后正常退Activity/guard/DB；Force沿原共享期限/DB.ForceClose且不冒已join，partial install/晚构造不遗失实例；原Account/Project/Model路由代表回归 |
| 独立验收 | 未参与实现者设计并亲自执行有判别力的权限/分页/恢复/退出动态场景；作者自测不能代替。输入变化只补受影响验证，保留所有初始失败 |

先覆盖完整矩阵再按资源分组，不在每轮结束后不断补造同类top。不重复B0-P内部全图/容量/14处SQL回滚等未改逻辑的全套动态验收；新增分页、HTTP、根接线及相关旧路由/退出链必须有自己的证据。必要静态检查为受影响包pure/race/vet、集成binary真实编译/精确top发现、两入口build与限定diff-check。完整结果的一次原子交付包含实现、测试、schema、卡和最小台账更新。

## 9. 当前可证状态

已按正式计划及实际构造/授权/根接缝确认依赖就绪，root授予上述写域。rev1独审暂不接受：遗漏九码Problem映射、混淆graceful/forced退出、详情query及framing边界不全、自然期限/EOF验收不够明确；rev2逐项修正并采用现process harness，已获独立差异审查接受，无剩余must-fix。分页四源及作者真实PG测试已闭合，限定pure/race/vet与integration race编译、精确一个top发现通过；独立分页产品静审无must-fix，三个独立动态场景的probe也已race编译并精确发现。作者与独立首编译因作者测试直接比较含func的LockKey失败，原失败保留；修复仅改为正式CompareLockKeys。九码Problem/schema已通过限定pure/race/vet及原错误/schema回归，独立差异静审接受。根生命周期库片段限定pure/race通过，尚未接入account/root路由。HTTP实施进行中；作者分页首轮整体FAIL（4子项中3PASS）：取消子项在Reader持Schedule SH时等待writer后阶段User EX，实际writer先等待discovery Schedule EX，因而5s内未到观测点；仅修测试锁观测及提前返回诊断，预算和产品不变。该轮Go实际退出1/9.52s、driver实际退出1/17.15s，外层实际退出1/92.472s且输入不变；两自有ID双退役/runtime双空齐，但hostTCP原尾仍1行delta，原尾FAIL保留。随后仅现场核到该轮端口/PID无存活，不将后验clear补原PASS。观测最小修复已独立差异接受；作者第二轮四子项完整PASS（Go4.54s、driver14.006s、外层73.761s，均实际退出0），两自有ID/runtime/hostTCP双清及输入不变齐。此结果不改首轮两个FAIL。独立分页尚未动态执行，没有本卡HTTP或默认根动态通过结论。全局进度与实际资源窗口由任务台账/分支记录维护，本卡不复制逐轮聊天、日志或全树哈希。
