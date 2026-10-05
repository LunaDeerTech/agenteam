# D09 恢复卡：System Model 管理读口

> rev1 规格已独立静审通过（STATIC PASS）并获主线程采纳；固定业务 `c54f73f3324caa11608d84e5d207141985eb6074`，被审原卡 SHA-256 `ddcb7225d809787f0987bf24ab13c5a3e6ea398d6f483c1f0de910c156da7d6b`。本文只交付两个完整管理读取能力，不交付前端或完整 D09。主线程已采纳提交推送 `bfae86b1b39d47c931d3be2071e76bd2b0e99a19`，并正式授权 `d08_recovery_design` 在固定 c54 上实施 §8 精确 13 路径，作者已实际启动；`recovery_verification` 独立验收。当前未获真实测试资源运行权，尚无本卡业务验收结论。

## 1. 结果与依据

管理员能够读取 System Model Credential 的当前安全 metadata，刷新后仍能按真实版本修改/删除；能够在删除 System Model 前读取精确、有限的引用数量/类型、替换要求与已知 adapter 阻断。两个 GET/HEAD 均经真实当前 Account Session/admin 授权，确认读取事务后才返回。

依据：[System HTTP](recovery-d09-system-model-http.md)、[默认生产根](recovery-d09-system-model-root.md)、[D09 工程规格](d09-model-system-token-usage-design.md)、[Model Configuration](../../architecture/platform-infrastructure/model-system/model-configuration.md)、[系统设置](../../frontend-design/layouts/system-settings.md)、[通用设置](../../frontend-design/layouts/settings-shell.md)。必读 [AGENTS](../../../AGENTS.md)、[团队流程](../agent-team/README.md)、[设计技能](../../../.agents/skills/agenteam-design/SKILL.md)；实施补读 [Go 技能](../../../.agents/skills/agenteam-go-development/SKILL.md)，独立验收使用 [验证技能](../../../.agents/skills/agenteam-verification/SKILL.md)。

只消费已验 HTTP `ac5b4c6`、默认根 `457b197` 及 c54 实际包含的 Model/Secret/Account/PG 能力。现有六个 root URL 前缀会转发本卡新增子路由；构造、初始化、所有权和后台任务均不变。当前仅 Model 的 platform_selector 引用替换已绑定，Agent/project_summary adapter 缺失。它们的登记计数可以读取，但不能宣称已取得外域当前事实或替换权限。

本卡无 SQL/迁移、新依赖、C0 签名、Project/App/Account 生产改动；不读取或消费活动 ledger/web/Artifact。后续 Model UI 仍等待公开入口已验接缝，并保持系统设置默认用户页。Summary 初值/Project Settings 待决、Provider 调用/Invocation 未绑定、ready503 和暂停的 Object 缺陷均不改变。

## 2. 已核接缝与必要窄增量

| 固定 c54 接缝 | 本卡使用/变化 |
| --- | --- |
| `model/http.go` 的 `systemHTTP.routes/dispatch`、Account `HTTPBoundary.RequireSystem` | 现 17 方法另有 5 HEAD；新增 2 GET/2 HEAD 后为 19 方法、7 HEAD。沿原 Host/Origin/Fetch-Metadata/Session/admin 投影；只给新读口加 §6 请求预算。 |
| Secret `HumanWriteCommands.Metadata`、`secret/write.go:458–467` | 正式端口已存在。固定实现先 `authorize(Tx{})`，后直接 Store 读 metadata；Account 授权事务的 User 锁未覆盖随后读取。新管理读口所需一致性通过 §4 的 Secret 自有只读事务补齐，签名不变。本项是静态锁边界核查及新接口保证，不是已动态复现的旧漏洞。 |
| `secret/storage.go:74–94` 的 `loadMetadata` | 按完整 ref scope/id 读取 purpose/version，并校验内部 current_payload_id；不读密文表、不解密。 |
| `model/query.go`、`model/authority.go` | 复用真实当前 Human/System Read 授权及错误规则；新方法拥有一个读取事务和完整锁 union，不在 HTTP 外层持锁嵌套 Secret 事务。 |
| `model/configuration.go:25–29,226–230` | 普通配置/selector 写持 references SH，删除持 EX，selector 写另持 selection EX。因此一致性影响读取必须一次拿 references EX，不能沿普通列表 SH 声称稳定。 |
| `model/references.go`、00015 的 reverse index | 数据归 Model；七种 kind/role 聚合有限，平台四引用可与 canonical selector 核对；不调用删除准备口或读取其他域私表。 |
| `model/http_test.go` | 原 OpenAPI 双向一致性断言明确 count=22；仅改为 26，保留全部原断言。 |

已核旧 Metadata 生产调用者仅 `model/http_credentials.go` 的写前边界；它随后仍由 ExecuteWrite 核完整材料摘要和原 expected_version。已有 System metadata 测试、Project Secret 事实拒绝后的 metadata 读取、Secret 跨 scope 反例都继续适用。固定 `project.SecretAuthority.AuthorizeProject` 支持同 Tx 委托，要求 User/Project SH；不需新 Project adapter。

## 3. HTTP 与正式 DTO

URI 均带 `/api/v1`：

| 方法/URI | 结果 |
| --- | --- |
| GET/HEAD `/api/v1/system/model-credentials/{id}` | 200 当前 metadata；不存在、已删除、非 System 或非 Model purpose 为 404。 |
| GET/HEAD `/api/v1/system/models/{id}/deletion-impact` | 200 精确登记聚合及已知规则；Project Model/不存在为 404；超过 §6 数量预算为 409，绝无部分成功。 |

路径 ID 使用既有 canonical UUIDv7 解析。沿原顺序先 HTTP 边界与当前管理员，再路径/输入与业务事实；两口不接受 query（包括空 `?`）、请求体、cursor/limit/替换对象。非零 Content-Length 或 chunked/Transfer-Encoding 请求体在授权后拒绝 InvalidArgument，不扫描任意 body。GET/HEAD 不要求 CSRF token/Idempotency-Key；安全 Cookie、Host/Origin/Fetch-Metadata 规则完整保留，不授予匿名读取。HEAD 执行同一权限、读取、错误检查，成功无 body；沿既有 WriteJSON/Problem 投影，不自行增加另一套 HEAD 错误行为。

Credential 响应恰三个字段：

```json
{"credential_id":"<UUIDv7>","purpose":"model","version":"7"}
```

HTTP 调用既有 `HumanWriteCommands.Metadata(ctx, actor, System CredentialRef)`；重新核返回 ref 完全相等、System scope、Model purpose、version 合法。ref/scope/purpose 不匹配不输出任何结果并返回 NotFound；非法 version/无效返回结构为 DependencyUnavailable。不能使用历史 receipt、Provider.version 或假定 version=1 填充；不存在时不返回 `deleted:true` 或空成功。不存在 GET SecretMaterial 或其他 purpose 的 HTTP 入口。

Model Go API/类型仅新增于 `internal/central/model/management_read.go`，不扩跨域 C0：

```go
type ModelReplacementRequirement string // "none", "optional", "required"
type ModelDeletionBlocker string        // "reference_adapter_unbound"
type ModelReferenceCount struct {
    OwnerKind string       `json:"owner_kind"`
    Role      string       `json:"role"`
    Count     f.Progress   `json:"count"`
}
type ModelDeletionImpact struct {
    ModelID                mc.ModelID                  `json:"model_id"`
    Version                f.Version                   `json:"version"`
    ReferenceCount         f.Progress                  `json:"reference_count"`
    ReferenceGroups        []ModelReferenceCount        `json:"reference_groups"`
    ReplacementRequirement ModelReplacementRequirement `json:"replacement_requirement"`
    DeleteBlocker          *ModelDeletionBlocker         `json:"delete_blocker"`
}
func (s *Service) GetModelDeletionImpact(
    ctx context.Context, actor id.Actor, model mc.ModelID,
) (ModelDeletionImpact, error)
```

`f/mc/id` 分别为既有 foundation/model contract/identity contract 别名。JSON 恰上列六字段，version 与全部 count 为 canonical decimal string；version 范围沿 foundation，count 依 §6。无引用时 groups 为 `[]`、count `"0"`、requirement `"none"`、blocker `null`。其他结果依下表，groups 仅列 count>0 的项，按 owner_kind/role 字典序稳定排序，total 等于所有 group count 之和。

| owner_kind / role | replacement_requirement 的贡献 | 当前 adapter |
| --- | --- | --- |
| platform_selector / embedding、memory | required | 已绑定 |
| platform_selector / reranker、image | optional | 已绑定 |
| agent / agent_model、approval_model | required | 未绑定 |
| project_summary / meeting_summary | required | 未绑定 |

任一 required 则总体 required，否则非空为 optional；任一外域项则 `delete_blocker="reference_adapter_unbound"`，仍以 200 返回真实计数。未知 kind/role、非法计数或 canonical 不一致为 DependencyUnavailable，不归入“其他”或忽略。`delete_blocker:null` 只表示没有这项已知 adapter 阻断，不表示“可删除”；本接口不评估某个替代模型，不生成 plan、grant、confirmation token、命令身份或写 nonce。合法替代仍沿现正式 DeleteModel 的同 type/enabled/provider/用途能力及所有权限规则。

无 Project/Agent/owner ID、名称、链接、owner_version、reasoning_effort、凭据 ref、模型参数或其他域内容。聚合计的是 Model 自有登记的引用边数：同一 owner 的两个 role 计两条，不宣称去重项目/Agent 数或外域当前活体事实。平台项另有 §5 的本域 canonical 证明。

## 4. Secret.Metadata 同事务读取与兼容

仅修改 `secret/write.go` 的 Metadata 方法及它必要的同文件私有 helper；Prepare/Execute/LookupWrite、摘要/nonce/写权限和其他既有函数不改。先校验原 ref；有效 Service 的校验顺序仍为 ref → Human/有效 scope → 当前 Session → 该 scope 权限 → 存在性。不得提前查 metadata 来判 purpose/存在，也不得把 System 管理员当 Project Owner。

nil/零 Service、typed-nil Store/所需授权口安全拒绝 DependencyUnbound，无 SQL/接口 panic；无效 ref 仍先 InvalidArgument。nil ctx 拒绝 InvalidArgument；有效 ctx 沿 §6。只检查当前 scope 所需授权：System 不依赖 Projects，Project 不新增 System 前置；有效 Session 的拒绝优先于其后 scope 权限结果。原有效 actor、ref、purpose 和跨 scope 错误语义保持。

用 `recoveryCause("secret-metadata")` 开一个 Secret Store 自有读取事务，一次 AcquireAll：`User(actor.UserID) SH`、`CredentialRefAggregate(ref.ID) SH`，Project scope 再加 `Project(ref.ProjectID) SH`。不得借命令锁、write-key/nonce gate、Human Mutate 或 Service 技术角色。`Store.InTx` 与 `RequireHeldLocks` 确认同 Store/实际 Tx 的完整 union，再 `authorize(ctx, tx, actor, ref.Scope, Read)`，最后由该 Tx executor `loadMetadata`。Account 权限在这个 User 锁内检查 actor 的完整 User/Session 与当前 role；Project 委托在同 User/Project 锁内沿原 Read gate。授权口不得另开事务、补锁或接受其他 Store 的 Tx。全链失败必须清空候选 metadata。

结果沿既有 Secret `commitError`：只有 Committed 且返回前原/派生 ctx 均未取消才返回 metadata；Unknown 返回 CommitUnknown/unknown，NotCommitted 保留原 fault；提交后 ctx 取消按现 LookupWriteCommand 的 DependencyUnavailable/committed 模式返回零 metadata。Unknown 不返回一个“安全当前版本”，不自动重读或将其解释为 NotCommitted。再次独立 Metadata/GET 只是新观察。

不调用 writable、Initialize、ReadCredentialForRequest/Usage、Resolve、Audit、lease 或任何 nonce 分配；本次 metadata 读取不会因写 key/rotation 状态新增 Mutate 门槛。内部 current_payload_id 只由既有 helper 做结构校验，不读取 secret_payloads/receipt 密文，不解密。已删除 receipt 仍能按旧写/lookup 契约重放；本 GET 当前缺失 404 与历史命中可以同时成立。

## 5. Model 聚合事实与竞态

GetModelDeletionImpact 安全拒绝 nil/零 Service、nil ctx、非法 ID 或非 Human；认证失败优先于受保护存在性。以 `readCause("management-impact")` 开一个本域读取事务，一次 AcquireAll 完整 union：`User(actor.UserID) SH`、`ModelConfigAggregate(model.ID) SH`、`SystemConfig("model-platform-selection") SH`、`SystemConfig("model-references") EX`。InTx/RequireHeldLocks 确认原 Store/Tx 后调用既有 `Authority.currentScope(...SystemScope(), Read)`。只读方法使用 EX 是为了与现有 SH 引用写互斥，不代表业务写许可；不先取 SH 后升级，不锁其他域 owner。

在同 Tx 内确认 System Model 存在/version，读取 singleton selection 及至多 5 条平台引用进行核对：合法 canonical 最多四条，必须与 `selectionReferences` 完全相等（包括 owner/id/version/role/model/Project 空/effort），`configured=false` 只能有空集合。缺行、重复/额外平台 owner、漏登记、错误 version 等拒绝 503，不把 canonical 还引用但索引空解释成可删。平台查询必须 SQL LIMIT 5，不能复用无界 loadOwnerReferences 后再截断。可以复用既有 loadSelection/selectionView/selectionReferences，无第二套 selector 生命周期。

目标 Model 的索引反向读取使用现有 model_reference_reverse；先对 `model_id` 做 SQL 有界输入（最多 10,001 行），再在 SQL 聚合 kind/role 或逐行常量空间累计，不先无界 COUNT/GROUP BY 后 LIMIT。不得将 10,001 行中的前 10,000 当准确结果。可以只读取 kind/role；平台 identity 核验走前段有限查询，外域 owner 信息不需要加载。合法数据仅七种聚合，不能为方便构造所有外域 owner 列表。返回前验证 groups、total、规则并在实际 Committed 后检查 ctx；Unknown/失败/取消一律零结果，不自动 retry。

读取结束不持锁、不返回可执行能力。selector/reference 在随后变化时，旧响应即成为旧观察；原 DeleteModel 必须重新 Discover/规划并在原写锁内核 Model 版本、全部引用和替代事实。Model.version 不随每条外域引用改变而必然变化，不能仅凭它断言引用未变。验收须实际出现“读取后新增/改变引用，原删除以最新事实执行或拒绝”，不得使用预览数据绕过准备口。未绑定 adapter 仍由原删除路径返回 DependencyUnbound；本读口不安装 allow 或改写其他域行。

## 6. 预算、HTTP 错误与材料边界

新两条 HTTP 分派在 RequireSystem 前派生最长 3 秒 ctx，包含本次认证、锁等待与读取；Secret.Metadata 和 GetModelDeletionImpact 的直接库调用也派生至多 3 秒，始终继承更早的调用方 deadline/cancel，嵌套预算不得延长。原 17 方法不新增 HTTP timeout。业务读 deadline 不替代 D03 原有实际事务清理，不启动脱离调用的 goroutine，不以超时为由提前声明底层终局。

本接口没有分页：输出是固定最多 7 组的一个精确观察。引用上限 10,000，10001 为哨兵；超限以 ResourceBusy/409 结束且零 DTO，不输出 approximate/has_more/next_cursor 或裁剪结果，不自动循环重读。平台 canonical 合法四项，LIMIT 5 命中额外项属于事实不一致而非正常分页。现索引/PK 足够，无迁移或索引新增；未来提升上限须有独立规格/预算依据。

沿既有安全 Problem：InvalidArgument 400；Unauthenticated/SessionRevoked 401；Forbidden/OriginDenied 403；NotFound 404；MethodNotAllowed 405；本卡数量超限 ResourceBusy 409；未绑正式依赖 DependencyUnbound 503；事实不一致 DependencyUnavailable 503；读事务 CommitUnknown 503 且 state=unknown。实际 D03 SQL/锁超时/取消 fault 原样传播：按现投影可能为 InternalError/500，不强行改成 ResourceBusy，不丢 postgres/SQLSTATE 链。所有不确定/失败不附候选 metadata/count。

HTTP 复用 `no-store`、`nosniff`、`no-referrer` 和 request_id；不加 ETag/304、跨用户缓存、CORS 或任意请求透传。公共 CommitUnknown 投影仍沿既有规则；这些 GET 没有命令身份，重新 GET 是新读取，不向 command lookup 填虚构 key，也不能承诺旧 writer 终局。新 API 描述须说明这一差别。

日志只沿现安全 projector，不能输出 request/response DTO、SQL args、Cookie/CSRF、Credential value/digest/payload、完整 owner/reference 记录或底层错误正文。本卡不读取 Secret material、不追加 Model/Secret 命令、Audit/Event、lease 或 nonce；既有 Account 请求认证维护和 D03 journal 不冒称“零 SQL”。

## 7. 验收结果矩阵

纯测试放 §8 的三个新文件，覆盖 typed DTO/decimal string/七组规则/10000 与 10001、无效/零值/typed-nil、验证顺序、完整锁 union、同 Tx/Store、授权失败前零 metadata SQL、Unknown/NotCommitted/提交后取消零输出。真实组不能由这些纯 Store 分支替代。

| 新真实顶层（确切名） | 必须证明的行为 |
| --- | --- |
| `TestSystemModelManagementMetadata` | 真实 Account/Secret 创建后 GET+HEAD；真实更新后的当前 version、刷新等价重读；历史 receipt 版本不替代当前值；delete 后 GET404 与旧 lookup/replay 并存；非 Model purpose/未知 ref 不泄漏；普通用户/匿名拒绝。safe metadata 不读 payload 密文、不分配 nonce/lease、不增业务记录。 |
| `TestSystemModelManagementDeletionImpact` | 无引用；真实平台 required/optional、两种角色数量；受限外域索引 fixture 的 agent/project_summary 数量/阻断；边数非 owner 数；七组闭合/JSON 大整数；canonical 缺失/错误 owner/version/额外行拒绝；10000 精确、10001 全拒，不无界聚合。外域 fixture 仅证明索引语义，不冒充真实外域 binding。 |
| `TestSystemModelManagementReadAuthorityAndAtomicity` | HTTP 预授权后、读事务前实际 Session 撤销必须拒绝；User SH 内读时另一普通事务的撤销/角色变更与读互斥；Credential EX/Model EX/references SH/selection EX 普通锁竞争证明完整读 union。读取后真实更新凭据/selector，再以旧 expected 或原删除请求执行，最终依现事实拒绝/收敛，预览不授权。 |
| `TestSystemModelManagementReadBudgetAndUncertainResult` | 使用正式 Store 装饰器在真实读取 callback/授权/SQL 已执行、底层实际 Committed 后才投影 Unknown；记录真实和投影结果，HTTP503/零数据、没有自动重试；NotCommitted、原 caller 更早取消、实际锁等待预算/提交后取消拒绝旧数据。区分只读事务 Unknown 与业务 dispatch，不构造网络/ROLLBACK 代理。 |
| `TestProjectSecretMetadataReadCompatibility` | 复用真实 PG/Account/Project/Secret bundle，System admin 的 System metadata 与 Project Owner 的 Project metadata 分别成功；System admin 不成为外域 Owner，非 Owner/撤销 Session 拒绝；Project Read gate 沿旧契约，System 不额外要求 Project 口/Project 不额外要求 System 口；完整 User/Project/Credential SH、错 Store/缺锁拒绝；metadata 不改变 canonical/version/Audit/nonce。 |

“普通事务竞争”须有实际持锁/等待或进入 callback 的确定信号，不靠 sleep 猜顺序。授权撤销必须持久生效并由真实 Account authority 确认，不能因为无效 SQL/请求构造先失败而记权限通过。重用现 systemHTTPStore 的正式结果装饰可以增私有测试 wrapper，但必须实际调用底层；不得按“第 N 次事务”盲目投影。保留原失败与确切原因，不降低断言或预算。

真实主组合复用 `tests/model/system_http_fixture_test.go`；Project 兼容复用 `tests/project/b03_secret_audit_fixture_test.go` 的已验 bundle/helper。可在本卡新测试文件内加同包私有 helper，不修改旧 fixture/伪造生产授权。合法测试索引/损坏数据在隔离测试库明确标 fixture；不直接改生产身份或把库 fixture 称完整 root/UI 业务。

## 8. 待授精确路径（13）

| # | 路径 | 范围 |
| --- | --- | --- |
| 1 | `internal/central/secret/write.go` | 旧；仅 Metadata 及其必要私有同文件 helper，§4。 |
| 2 | `internal/central/model/http.go` | 旧；两 route/new-read timeout dispatch 接缝，不重构旧 17 方法。 |
| 3 | `internal/central/model/management_read.go` | 新；正式 API/DTO、同 Tx 聚合与预算。 |
| 4 | `internal/central/model/http_management.go` | 新；两个 HTTP handler、安全 DTO/新读口预算识别 helper。 |
| 5 | `api/openapi/model-system.json` | 旧；两 GET/HEAD、闭合 schema/错误/预算；info 可准确补新能力并纠正旧 root 未绑历史描述，原路由语义不变。 |
| 6 | `internal/central/model/http_test.go` | 旧；唯一原断言改动 count22→26，双向 OpenAPI/HEAD/旧断言原样保留。 |
| 7 | `internal/central/secret/metadata_test.go` | 新；方法验证顺序、所有权、完整锁与结果分支。 |
| 8 | `internal/central/model/management_read_test.go` | 新；聚合契约、cap、canonical、权限/锁/结果分支。 |
| 9 | `internal/central/model/http_management_test.go` | 新；wire/闭合 DTO/HEAD/材料投影、新路由预算隔离。 |
| 10 | `tests/model/system_management_metadata_test.go` | 新；metadata 真实组及必要同包 helper。 |
| 11 | `tests/model/system_management_impact_test.go` | 新；影响聚合真实组。 |
| 12 | `tests/model/system_management_boundaries_test.go` | 新；当前权限/竞争/预算/正式 Store 不确定组。 |
| 13 | `tests/project/secret_metadata_read_test.go` | 新；真实 Project/Account 兼容。 |

其余路径只读。没有对 `model/query.go`、原 references/commands、Secret storage/service/contract、App、Project、Account、D03 或旧 SQL 的写权；现 helper 足够，不因测试便利扩大生产入口。必要范围外缺口先报精确证据，由主线程裁决。本文新卡当前唯一仓库写权不自动授权此表实施。

## 9. 检查、资源与独立验收门槛

实施固定 c54 加本卡 13 源，在私有轻量工作根准备必要 Go 闭包和 embed/OpenAPI 资产；不用活动源码证明依赖。Go 固定 1.27.1，`GOTOOLCHAIN=local`、`GOPROXY=off`、`GOSUMDB=off`、`GOFLAGS=-mod=readonly`，复用已核 modcache，私有 workspace GOCACHE/TMPDIR；先 unit/race/vet `./internal/central/model/... ./internal/central/secret/...`，integration 编译 `./tests/model ./tests/project ./tests/security`，两 cmd build。记录真实 argv/env/exit 与源指纹，不把空日志当完整命令记录。

真实仅在主线程明确交独占窗口后运行原 `scripts/test-security.sh -run '<固定 selector>'`，保持原 race/count1/6m；verbose 经 GOFLAGS 传递，不给脚本自造参数。主 fixture 为既有受支持 PG17.8+、MinIO；PG16 仅原不支持反例，不新加正例/迁移组。新增五顶层全部运行；必要旧回归为原十个 `TestModelSystemHTTP*`，加 `TestProjectSecretAuditBindingFactRejection`、`TestSecretBindingsAuthorizationAndReadAuditBoundary`；仅这些确定名称，避免命中旧网络不确定探针或全模块。纯 Go 的语义不变通过项可据固定输入复用。

真实环境只用任务私有 nonce/标签/资源；记录旧基线 exact ID/name/labels、存活自有资源身份和命令。结束二次 exactID absent、基线不变、所属进程 0/runtime 空后交回。禁止恢复暂停 Object/网络/ROLLBACK 探针，也不靠提高预算掩盖失败。当前设计阶段没有 Go/Docker/网络/SQL/浏览器权限。

独立验证者先核静态权限/EX 锁理由、scope 兼容与安全聚合，再按作者固定输入/风险覆盖选独立真实反例，至少覆盖当前身份与实际 Secret metadata 同 Tx、预览与后续删除事实变化；不机械重跑全部通过组。存在错误码/权限/锁/计数/预算或未执行真实门槛，均不能宣布本卡已交付。

## 10. 交付与后继责任

主线程已完整审阅本卡 rev1 与独立报告并采纳 13 路径范围。独立报告：`/workspace/agenteam-model-management-reads-spec-v-ok5jcolo/report.md`，SHA-256 `a65a76885b8735e5c6dbf952d58adc623afe37dc831ac01818417b6482ae5c38`；被审原卡 SHA-256 为页首所列 `ddcb7225d809787f0987bf24ab13c5a3e6ea398d6f483c1f0de910c156da7d6b`。本次仅归位行政状态，§1–9 技术、固定 c54 和 13 路径逐字不变。采纳不是业务或动态验收；主线程已在提交 `bfae86b1b39d47c931d3be2071e76bd2b0e99a19` 后授予 `d08_recovery_design` 精确 13 路径实施权并确认实际启动，`recovery_verification` 的独立计划已冻结，当前没有真实资源运行权。[规格持久记录](../agent-team/system-model-management-reads-spec-verification.md)保留原卡、独立报告及其 Unknown 措辞限定计划；Secret 原错误投影不公开 attempt/cause，物理信息仅由真实 Store 观测，Model 沿原 UnknownCommandError 合同。此限定不改本卡技术口或权限。

最终证据应精确绑定 13 源、原失败/修复 delta、原命令/日志、当前依赖与实际资源清理；代码通过后由主线程统一安排能力文档及状态归位。源码提交、迁移、生产 rollout 不属于当前设计授权。

本卡只增加已定 System 管理信息的读取能力。未来实际 Agent/project_summary reference adapter 由 Model 与相应领域正式绑定后，同时更新影响读口的“未绑”事实及跨域集成验收，不能只删一个 blocker 字符串。System Model UI 另卡消费这两个已验读口，并先落实已定用户默认入口和已验单 Cookie owner；本卡不先改 web 或替用户决定 Summary。
