# D09 Project chat 配置与安全目录恢复卡

修订 2，2026-10-05，仅同步验收状态；§1–8 的规格、API 与授权历史保持修订 1。状态：**本卡 21 源已独立验收并由主线程采纳，提交推送 `de00c610da62cb77cc03efe7c3cc842cf81f1ba5`。** 固定生产基线 `81fe7427ceb4672247b3d30a51c10a2e2808ba04`；其已验 Project+Secret 窄绑定证据已归位于 `0e79a39186dd09461f29160ff3d6a395588c78c1`。结果见[正式验收报告](../agent-team/d09-project-configuration-verification.md)，不代表完整 D09 或根装配完成。

## 1. 目标、依据与真实依赖

当前 Owner 可原子管理本 Project chat Provider/Model，读取本项目配置、查证原配置命令结果，并获得 enabled System/本 Project chat 的安全选择目录。Summary 初值/Settings、Project 创建输入、Resolver/Usage、Model lease/Provider 调用、HTTP/root/app 不在范围；不写默认值、不以 NULL 宣称必填 Summary 完成。

必读 [AGENTS](../../../AGENTS.md)、[团队流程](../agent-team/README.md)、[开发计划](../development-plan.md)、[设计技能](../../../.agents/skills/agenteam-design/SKILL.md)；实施读 [Go 技能](../../../.agents/skills/agenteam-go-development/SKILL.md)，独立验收读 [验收技能](../../../.agents/skills/agenteam-verification/SKILL.md)。业务与完整模块边界引用 [D09 主卡](d09-model-system-token-usage.md)、[D09 设计](d09-model-system-token-usage-design.md) §2–4/8/10/13、[B01-K](d09-b01-system-configuration.md)、[模型配置架构](../../architecture/platform-infrastructure/model-system/model-configuration.md)，不重定义其规则。

| 依赖 | 已验事实与本卡消费边界 |
| --- | --- |
| D09 C0 / B01-K | `e6e94c4` 纯契约、`26622bc` typed Model Audit、`543511c` System 原子配置与 00015；本卡扩真实 scope，保留已验 System 字节与行为 |
| Project / Secret | `81fe742` 的真实 Human Owner/gate、Project Secret mutation delegate 与 Secret Audit fact 路由；[正式报告](../agent-team/project-secret-audit-binding-verification.md) 保留原失败及组合验收边界；不把普通 Owner 扩成 Service grant |
| Audit / Outbox / PG | 沿实际已提交 Appender、typed Model verifier、ProducerAuthority 和事务锁；模型无新网络 I/O；Object S2/Artifact stop 不是本卡前置，不能消费其活动候选 |
| 本卡必须补齐的接缝 | 固定 `project/events.go:Discover/ValidateInTx` 只接受 ProjectProducer 或已绑定 lifecycle；Model Project Append 仍 unbound。本卡补 §4 的窄口后，才能验真实 Project Model Event |
| 后继未绑定 | D10 Agent（含 approval_model）及 Summary settings 的 canonical 引用登记/替换；D09 Resolver/Usage/Provider runtime；D07/后继 app 装配与 D26/27 UI。缺 adapter 不模拟成功 |

00015 已具备 Project Provider scope/project_id/chat 协议约束、Model 继承 Provider scope、Project commands 唯一键/原计划/receipt、references、Model Audit 闭集。**无需迁移，不占 00017，不改 00001–00016。** Project settings/运行表仍不在本结果中。Summary 产品问题未答不会阻塞本卡，但继续阻塞该初值与完整 D09 验收。

## 2. 正式 API 与构造

既有 `CreateProvider/UpdateProvider/DeleteProvider/CreateModel/UpdateModel/DeleteModel` 签名和 C0 请求不变，开启其 `CommandMeta.Scope=Project` 路径。Project 仅 chat；Provider scope/protocol、Model provider/type 不可变；CredentialRef 必须同 Project、Purpose=model。保留 B01-K 对未知非空 options/parameters/overwrite、型号 conformance 的拒绝规则，不借配置扩展承诺真实 Provider 调用。

在 `model/authority.go` 增加具名最窄依赖，方法完全由既有真实 Project Authority 满足，不扩公共 contract：

```go
type ProjectAuthority interface {
    AuthorizeProject(context.Context, foundation.Tx, identity.Actor,
        identity.ProjectID, identity.AccessIntent) (identity.AccessGrant, error)
}
// Authorizations 保留 Sessions/System，增加 Projects ProjectAuthority。
```

`NewAuthority` 仍要求原 Sessions/System。`Projects == nil` 保持旧 System-only 构造兼容，所有 Project 路径明确 `DEPENDENCY_UNBOUND`；显式 typed nil 拒绝构造。非 nil 捕获一次，调用不得切换 provider；零值或不匹配的 AccessGrant 拒绝。Model/Project 使用同一真实 Store/Tx，Model 自核所需锁，真实 delegate 在同 Tx 核当前 Session/Owner/初始化/lifecycle；不导入 Project 实现、不读其表、不从管理员推导 Owner。

构造无环：Account Authority、Secret ProjectAuditAuthority → Project Authority（Secret AuditFacts）→ Project SecretAuthority；再建 Model Authority（真实 Project delegate）→ Audit（Models=Model、Projects=Project）、SecretUsageRouter（Model+原 Account/MCP）、Secret（Project delegate）、Outbox（真实 Model producer+Project gate）→ Model Service。Model typed Audit 不注册进 Project AuditFacts；Audit 已先按 ModelAction 分派 Model 自己的 verifier。所有根装配仅在真实测试中组合，生产 app 留后继。

新增下列 Service 查询，不改变既有 System 查询签名/仅 System 结果；类型放 `model/project_query.go`。`ProjectQuery` 仅 `{Cursor string; Limit int}`，Limit 沿原 1–100；`ProviderPage/ModelPage` 复用现有类型。

```go
GetProjectProvider(ctx context.Context, actor identity.Actor, project identity.ProjectID,
    provider contract.ProviderID) (contract.ProviderView, error)
GetProjectModel(ctx context.Context, actor identity.Actor, project identity.ProjectID,
    model contract.ModelID) (contract.ModelView, error)
ListProjectProviders(ctx context.Context, actor identity.Actor, project identity.ProjectID,
    query ProjectQuery) (ProviderPage, error)
ListProjectModels(ctx context.Context, actor identity.Actor, project identity.ProjectID,
    query ProjectQuery) (ModelPage, error)
ListAvailableChatModels(ctx context.Context, actor identity.Actor, project identity.ProjectID,
    query ProjectQuery) (AvailableChatModelPage, error)
```

上列为 `(*Service)` 方法，`contract` 指 `model/contract`。ListProjectModels 覆盖本项目全部 Provider 的 chat Models；不增加过滤器。新增 `AvailableChatModel` 字段精确为 `ID contract.ModelID`、`ProviderID contract.ProviderID`、`Scope identity.Scope`、`Name string`、`ProviderName string`、`Version foundation.Version`、`Capabilities contract.Capabilities`；JSON 名分别为 `id/provider_id/scope/name/provider_name/version/capabilities`。`AvailableChatModelPage` 为 `Items []AvailableChatModel` 与 `NextCursor string`（`items/next_cursor,omitempty`）；返回切片和 Capabilities 深拷贝，不返回配置结构的嵌入字段。

现 `LookupCommand` 保持签名，经 `LookupCommandRequest.Meta.Scope` 增 Project 的六个配置命令查证。未知 command 仍 InvalidArgument；Project `model.selection.update` 仍 DependencyUnbound，不能据此交付 Settings。公开 Lookup 是当前有权后的历史查询，不能替代某一 Unknown 请求的私有语义确认。

## 3. Scope、原子事务与历史兼容

- Provider/Model/command 私有记录、持久计划、SQL、视图、CredentialRef、Audit、Event 一致保存并复核真实 scope。Project Model 必须归属请求 Project 的 Provider；按裸资源 ID 不能把 System/另一 Project 配置带入。有效 ID 不属于授权 scope 返回 NotFound，不先泄露其存在或配置。
- System 保留 `model.system` namespace、原 owner 列表、`model-command-v1` semantic 字节、原 event/receipt；旧 committed `mutation_plan` 没有新 scope 字段时，只能在真实行 scope=system、原 System identity 已验证后按 legacy System 解码。不能把缺失 Project scope 当 System，或为升级改写旧 command。原 System selector/替换规则保持。
- 新 Project identity 固定 `model.project`，owners 顺序 `[ProjectID, stable Human UserID]`，原 command/key；semantic 使用独立 version marker 并包含 Project/stable user 及完整请求意义，排除 Session/trace。原创建 ID/EventID 首次计划固定。换有效 Session 同义可重放，同 key 异义冲突；同 user/key 不同 Project 不串 receipt。
- 初次收集 Command EX、User SH、全局 `SystemConfigLock("model-references")`（普通 SH；Model 删除/替换 EX）、Project SH、Provider/Model/Credential/record 与 Outbox registration/event 锁，一次完整 union Acquire；之后只 RequireHeldLocks，缺锁不能靠补 Acquire 通过。锁后重读所有 scope/Provider/ref/version/index 映射，变更整 Tx ResourceBusy/既定冲突，不能继续旧计划。
- 顺序为当前 Session+Owner Read 与不可逆 gate → canonical 同 key 已提交 receipt/摘要 → 首次 mutation 的 Mutate/version/依赖。未出现 receipt 才能新写；准备失败也在原 writer 锁、当前权限下复查并发同义 receipt。归档可查询/重放历史结果；deleting、未初始化、删后或失权不可借 receipt 绕 gate。Model 自己的 typed fact、Secret reference planner 与新 Event 仍须证明同 Tx 原 prepared command 和真实结果，不能仅凭 Owner 返回 Success。
- Provider/Model mutation、Secret Retain/ReleaseReferenceUsage、typed Model Audit（原 command cause、ordinal0、完整 Actor/Session/typed metadata）、Outbox Event 和 committed receipt 全在同一真实 Tx。Secret release 只释放引用，不删除材料/租约；Project Secret 创建/旋转仍走真实独立 Secret 命令，不在 Model 私存明文。Audit changed_fields/affected_count/replacement 必须来自实际变更，不能造引用改写数。
- Committed 才返成功；NotCommitted 沿原 key 重试。Unknown 保留原 cause/attempt/identity/expected semantic；确认须在原 Command EX 后证明实际 writer 终局、当前权限及 exact canonical receipt。异义冲突；无行/确认失败/仍 pending 保持原 Unknown，不把另一个请求或 Project 的 receipt 当本次结果，不换 ID/key，不再发副作用。当前失权的确认不得泄露 receipt；错误/格式化日志不含 key、配置秘密或材料。

## 4. Project Model Event 窄授权

只扩 `project/events.go` 的 `Discover/ValidateInTx` 两处分派，新 helper 独立于 Project 自己事件的 `appendBinding/eventFact`；原 Project producer、lifecycle、Delivery/Inspect/Requeue 语义不改。

本分支闭集：`Kind=AppendProject`、Human Actor、Producer=`model`、EventType=`model.configuration_changed`、SchemaVersion=1、AggregateType=`model.configuration`、合法 AggregateVersion 且无 AggregateSequence、Project scope 与请求 ProjectID 完全一致。其他 producer、embedding-selection-changed、未来版本/aggregate 或用途不走此分支，不对任意非 Project producer 授权。不跨域 import Model 实现或读取其表；闭合 event identity 沿 B01-K 已注册 schema。

`Discover` 仅接 CurrentAccess，校验完整合法请求并规划 User SH+Project SH；不会把发现时状态当授权，不写 SQL、不发 Event。private binding 精确包含 namespace/purpose、完整 `Actor.Details()`（含 Session）、完整 `event.Summary`（含 payload digest）、ProjectID 与固定允许阶段序列 `[CurrentAccess, NewFact]`。沿既有 Project private issuer，可用独立 purpose/opaque 域分离旧计划；返回计划不可变。**Outbox 对同一 deps 调用两个阶段**，不得只摘要本次 CurrentAccess 后拒绝全部 NewFact，也不得忽略用途让其他 action/stage 复用。

`ValidateInTx` 同 Store/Tx 验私有 issuer/binding/用途、完整 required locks，随后分别调用真实 `RequireOwnerInTx(Read)` / `RequireOwnerInTx(Mutate)`；不得开启独立 Tx/补锁。Stage 外值拒绝，changed Actor/Session、summary 任一字段或 Project、错 issuer/跨 Store/缺锁均拒绝；同计划不能将 owner 证明解释成 Model canonical fact。Model ProducerAuthority 仍由 Outbox 独立校验闭合 catalog、原 private prepared command、实际资源与原 Event，Project 不替代它。

| 当前事实 | Project CurrentAccess | Project NewFact / 首次配置写 |
| --- | --- | --- |
| active + initialized + 当前 Owner/Session | Read 通过 | Mutate 通过 |
| archiving/archived + initialized + 当前 Owner/Session | Read 通过 | ProjectNotActive |
| deleting/未初始化 | 沿正式 Project gate 拒绝 | 拒绝 |
| 删后/非 Owner（含他人 admin）/Session 失效 | 沿正式 Project 错误拒绝 | 拒绝 |
| 计划后 actor/summary/Project 被换、缺锁/错 issuer | 拒绝且零副作用 | 拒绝且零副作用 |

Model 公开历史命令重放在 receipt 分支完成，不重新调用 Audit/Event。现 Model producer 只接受原 Tx 的 prepared fact；本卡不新增公开历史 Event 任意 reappend 能力。归档命令重放与上述 Project gate 阶段矩阵分别验证，不能以其中一项代替另一项。

## 5. 安全目录与删除的可交付边界

自己的配置 Get/List 返回自己的完整安全配置视图（仅稳定 CredentialRef，无材料）；System 原 Get/List 仍只供当前管理员且仅 System。可用目录只包含 `Provider.enabled && Model.enabled && type=chat` 的 System/当前 Project 并集，所有投影字段在 §2 闭合，**不能返回 System endpoint、protocol options、CredentialRef、provider_model_id、parameters/request/header overwrite 或其他 Project 数据**。只查并投影所需字段，不靠完整配置序列化后删字段。目录表示当前配置可选，不表示网络/型号已实测。

新列表使用原 keyset/水位原则（created_at DESC、ID DESC），cursor 绑定 Project scope、稳定 user、查询种类及固定 chat/enabled 语义，limit 可变、跨查询/Project/user 不可复用。每页同 Tx 当前 Owner Read 与 Project gate，预持全局 model-references SH；用一条本域 JOIN 查询的 statement snapshot 取得 Provider/Model enabled 与安全字段的一致投影。SH 不是对普通配置 mutation 的排他锁，不能把多次无约束查询拼成一致快照。归档可读，不为目录启动 snapshot/lease/Provider 调用。

无引用 Model 可删除：先计划 exact references，EX 屏障后重扫完整规范索引与版本，空集合才是本次无引用事实。Project replacement 只能为 enabled 本 Project chat 或 enabled System chat，Provider 同样 enabled；其他 Project 不可见。没有引用时合法 replacement 可按既有请求/metadata 记录，affected_count=0，不宣称改写 owner。

当前 references 实现只会改 platform_selector；`agent/project_summary` 真实行没有绑定 canonical rewrite adapter。Project 删除遇这些行（或不合法的跨 scope/platform 行）必须整 Tx 拒绝，缺真实 owner adapter 用 DependencyUnbound；不得忽略索引、清空引用或仅更新 index。Provider 有任何 Model 则拒绝删除，空 Provider 删除只释放真实 Secret 引用。System 平台 required selector、合法替换与 Secret 删除竞争保持原规则。

后继 D10 负责 Agent/approval_model 的正式 ReferenceAuthority，Summary 决策后的 settings owner 负责 project_summary。它们须在同 Tx 修改 canonical + D09 完整反向索引，并遵守全局 SH 建引用 / EX 删除替换屏障；之后再独立接入真实跨域原子替换，本卡不提供生产 stub 或 fake rewrite。

## 6. 精确文件权限

本次规格阶段唯一仓库写入是本卡。静审采纳并正式续派后，建议下列 **13 个生产文件 + 8 个新测试文件** 由单一 backend 作者独占；名称和范围冻结，其他文件只读。若确需变动旧测试私有 helper/API，先报具体编译/行为证据申请窄范围，不删旧断言或加兼容 allow。

| 生产路径（internal/central/ 下） | 允许范围 |
| --- | --- |
| `model/authority.go` | 可选 ProjectAuthority、scope-aware 权限与精确 grant |
| `model/store.go` | 真实 scope 私有 record/load/view；旧 System 记录兼容 |
| `model/commands.go` | Project identity/semantic/receipt、初始锁、同 Tx/replay/Unknown |
| `model/configuration.go` | 六 CRUD 的 Project 准入、映射复核与 SQL |
| `model/configuration_policy.go` | exact scope CredentialRef、Project chat；原 conformance 不放宽 |
| `model/query.go` | LookupCommand Project scope、必要共用 read/page helper；旧 System 隔离 |
| `model/references.go` | Project replacement 可见性/EX 重扫/缺 owner 拒绝；System 兼容 |
| `model/secret_authority.go` | 原 Provider command/ref scope 的真实 retain/release 计划和事实 |
| `model/audit_authority.go` | 原 scope/Owner/command/resource/result/metadata 事实 |
| `model/events.go` | Project header/payload、scope 一致性与原 private producer facts |
| `project/events.go` | 仅 Discover/ValidateInTx 的上述 Model 分支分派及相应注释 |
| **新** `model/project_query.go` | §2 五查询、selection-safe DTO、Project cursor |
| **新** `project/external_event_authority.go` | §4 Model-only 正式 Project gate helper |

新测试精确为 `internal/central/model/project_authority_test.go`、`internal/central/model/project_configuration_test.go`、`internal/central/model/project_query_test.go`、`internal/central/project/external_event_authority_test.go`；以及 `tests/model/project_configuration_fixture_test.go`、`tests/model/project_configuration_integration_test.go`、`tests/model/project_configuration_unknown_test.go`、`tests/model/project_available_models_test.go`。包内前四个测绑定/字节/错误/投影；后四个组合真实 PG 事实与故障，不修改既有 System/Project fixture。

所有公共 contract、Secret/Audit/Outbox 生产核心、`model/service.go`、`model/secret_router.go`、旧测试/脚本、迁移、共享设计/台账、root/app 均不授权修改；不读未验活动 S2/Artifact 候选作为依赖。自有临时目录可存固定输入与日志；Go/Docker/PG/MinIO 只在实施后由主线程另交窗口，规格阶段不运行。禁止 Git 写与再委派。

## 7. 验收门槛与交付

真实组用 Account Session、Project Authority、Secret（真实 Project write + Purpose=model reference）、Model、Audit、Outbox 与 PG；不能用 fake Project allow 或把 checker 成功当配置事实。Project 建立可沿已验 B02 fixture 的正式 Skills initializer 测试口，明确隔离未绑 Skills，不能添加 Summary 输入/默认。探针只能观测/拦截后委派真实操作或注入拒绝；实际引用方未绑定的反例可由 fixture 写合法 index 行，但不能声称该 fixture 是生产 adapter。

| 场景 | 必须观察到的结果 |
| --- | --- |
| 六 CRUD、Scope/版本 | 当前 Owner 正常完成；Project 只 chat、不可变字段/旧版本拒绝；其他 Project/System ID 不泄露；他人 Owner/admin/撤销 Session 拒绝；System 原行为不变 |
| Secret/Audit/Event 原子性 | 真实 Project Secret create 后 Provider 引用增/换/减；wrong purpose/scope 拒绝；任一 Secret/Audit/Outbox 故障整 Tx 零配置/command/引用/新 Audit/Event；删除 Provider 后 Secret 仍存 |
| 完整 fact binding | 改 Entry/Key/cause/ordinal/Actor.Session/typed metadata、错误 scope/resource/result version、伪造 producer summary 或缺 private command 都拒绝；跨 Store/Tx、缺锁、wrong issuer 拒绝且不补 Acquire |
| 双阶段及撤权 | 同一真实 Project deps active 两阶段均可；archiving/archived CurrentAccess 通过、NewFact 拒绝；计划后撤 Session/Owner/gate 零新 mutation/副作用；其他 producer/type/version/Service/action 不因新分支放开 |
| 幂等/旧兼容 | 同义换 Session、stale expected_version 原 receipt 优先；异义冲突；跨 Project 相同 user/key 不串结果；旧 System namespace/semantic 与缺新字段 legacy committed plan/receipt 仍可读重放；归档命令重放零新增 Audit/Event |
| Unknown 三态 | PG 协议实际 COMMIT 后丢 ACK、实际回滚、实际 writer 未终局；原 writer 锁/语义确认、撤 Session 不泄露、absent/失败仍 Unknown；rollback-unknown A 后同 key 异义 B 提交，A 不采纳 B receipt；用协议终局信号而非 sleep 猜提交 |
| 引用/删除竞争 | 空 index 正常删除；真实 agent/project_summary 行在 replacement/no-replacement 均零写拒绝；锁后集合变化整 Tx 回滚；SH 建引用/EX 删除互斥；Provider 非空拒绝、真实 Secret 删除与 release 串行，旧 System replacement 仍通过 |
| 目录/分页 | own+System enabled 两层 chat 精确集合、disabled/nonchat/其他 Project 零泄露；endpoint/ref/overwrite canary 不出现在返回/序列化；cursor 跨 user/project/query 拒绝，改 limit 合法，每页撤权立即拒绝；归档只读 |

纯检查：Go 1.27.1，相关 `internal/central/model/...` 与 `internal/central/project/...` 的原单测、race/count=1、vet，以及新/旧 integration compile 和两 cmd compile/build。真实入口沿 `AGENTEAM_GO=... AGENTEAM_MINIO_BINARY=... sh scripts/test-models.sh`（固定现脚本执行 `test-objects.sh -run '^TestModel'`），新真实顶层以 `TestModelProject` 开头；保持原 race/count=1/每包 6m，不靠降断言/预算换通过。受影响旧 Project 权限/事件、ProjectSecret 窄绑定、Secret/Account/Outbox 相邻组由验收者按实际 delta 选定并记录，不把 no-tests 当兼容。

验收者未参与实施，先核本卡，再对停止写入的精确 21 源指纹独立验证高风险权限/原子性/Unknown/目录隔离；可复用未变依赖证据，额外探针只存自有 tmp。交付固定基线+源清单、实际 argv/原始日志/测试数/保留首红、通过/未验/限制，实际命令终止与 owned 容器/网络 exact-ID absent、runtime/进程清零证据。单项通过不称完整 D09/Project lifecycle 或 ready 完成。只在静审采纳后由主线程另行派实施与资源；发现范围外契约/schema/产品问题，仅暂停相关部分并报告最小缺口。

## 8. 规格采纳与实施授权

独立verification_worker已对原候选SHA-256 `32a9ccc856bbb8f0a736b5f56c9fd0da3fb440de4afd032ba2f3d152bce7b608` 完成静审，未发现要求先修订的硬阻断；主线程复核API、真实双阶段授权、System旧字节兼容、引用缺adapter拒绝与安全目录后采纳。审查报告 `/tmp/agenteam-d09-project-config-static-cdxaosuw/review.md` SHA-256 `3eba6ec43ba94693199c68a90223401af0af4b57aae3a1a787ee887c91ece8d2`，固定输入清单 `inputs.json` SHA-256 `a9fde7f6d4b8bc7c6b169e95b81265998f558e17d0385ce2509f046fa56fc760`；本节持久记录结论，不依赖临时文件存续。静审没有Go/PG/行为通过声明。

唯一实施者为`restore_test_dependencies`，仅开放本卡13生产+8新测试共21路径。使用固定81fe742生产输入加自己21源自测，不消费Object Audit或Artifact活动稿；不改旧测试、公共contract、Secret/Audit/Outbox核心、迁移或app。全部Project receipt路径必须按当前Read→原receipt→首次Mutate执行，System legacy缺scope只允许在真实System行且已核原身份的加载支路兼容。源码冻结后交未参与实现者独立验收；真实fixture须主线程交接，当前不占00017。Summary初值与完整D09待定边界保持。

## 9. 窄块验收与提交接续

本卡的 Project chat Provider/Model CRUD、当前项目配置查询、命令查证与安全可用目录已验收。作者第二轮 43 顶层/117 子例、独立真实 2 顶层/5 子例通过；21 源以 `de00c610` 提交推送，正式报告及可重建证据以 `965da5dbbcabf4b2764a77789cbf8494e53fced8` 提交推送，均由主线程核实远端一致。[正式报告](../agent-team/d09-project-configuration-verification.md)保存静审、固定输入、覆盖表、实际命令与资源清零证据。

首轮整包 FAIL 保留；5 个未变新顶层复用该非 verbose 执行中的未失败结果，没有单列 PASS 行，不称首轮整包通过。早期作者纯检查保留 stdout/stderr 与当时执行声明，但 exact argv/exit 未单独持久化；空日志不作退出码证明。复用边界及后续完整元数据见报告的[实际检查](../agent-team/d09-project-configuration-verification.md#实际检查和复用证据)与[首红记录](../agent-team/d09-project-configuration-verification.md#首红修订与限制)。

Summary 创建初值/Settings 仍待用户决定；真实 Agent/project_summary 引用替换、Resolver/Usage、Provider 调用及 HTTP/app/root 留后继，完整 D09 未完成。本次只同步状态和验收入口，不改既定行为，也不新增迁移。
