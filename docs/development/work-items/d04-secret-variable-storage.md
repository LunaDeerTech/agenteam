# D04 Project Secret Variable 存储 producer

> 交付边界：D04 材料存储 producer 的有限验收已完成；已与 D05、Secret Owner 一起按 00028→00029→00030 连续迁移交付 main `fb84a892`。库矩阵沿固定来源复用。Secret HTTP、默认根绑定与 Agent F1 不在本库接受范围。

后继 [Secret Owner HTTP](d10-secret-variables-owner-http.md) 已完成 HTTP、既有 Project 默认根接入及限定独立风险验证；该后继不改变本卡的库验收范围，也不启用生产 Project initializer、完整生命周期或 F1。

D04 producer 与 00029 已实现：core3（17 节点）、maintenance 单 top、固定四 top/十格恢复矩阵（13 节点）均完整真实 PG PASS，原 Wait 与资源双退役尾齐。它通过 typed `secret/contract.ProjectVariableWrites` 向 Owner 提供材料写入和安全 receipt，不新增公开明文读取接口。本卡落实已接受的 [D10 rev2](d10-secret-variables-owner.md) §4、§6；真实 authority 与 Owner final-Tx 的后续有限验收见 [Owner 服务](d10-secret-variable-owner-service.md)。

## 1. 范围、所有者与构造顺序

D04 独占 `internal/central/secret/contract/project_variable.go` 及其纯测试、Secret 实现内的专用写入和 receipt、envelope、rotation、cleanup、private Audit witness 必要增量。既有 Account、Model、kind1/2 的行为与格式必须保持。不得仅扩大 `Purpose.Valid`：旧 `validateWrite` 会因此接受新用途，且旧请求也可能以旧 purpose 指向新用途的现存 Credential；两个入口均须显式隔离。

D10 Owner 后继工作拥有专用 authority 的真实 provider、canonical/history/completed receipt、外层锁并集及同一最终事务中的 D10 Audit/Outbox/Activity。独立 `SecretWriteAuthority(Store, current Account/Project gates)` 在 Project 构造后、Secret Service 构造前产生，作为 Secret 构造时的不可变端口；不得让现有 `projectvariable.NewAuthority(Store)` 事实提供者反持 Project/Secret Service。禁止 late setter、locator、生产 stub 或以 Model Lookup 替代。

Secret Audit 独立结果已由e94077eb正式交付严格合同与现有 Go/HTTP/schema/TS读端兼容，不包含D04 private witness或D10 Owner写事务。公开 signature 的确定增量须先与 D10 消费者核对并交 root 备案；签名兼容核对不是独立验收。root 拥有 app 最终装配、Git、迁移号与正式交付；00029已授本域，26–28由各自owner完成正式前缀，本域不改其源或代其验收。

## 2. 专用端口与不可变数据

新端口只接受 Human Actor 与 Project scope，所有 ID 使用 Foundation/Identity 的正式 typed ID。D04 contract 不 import D10 implementation 或 contract，VariableID 直接使用正式 `identity.ProjectVariableID`；D10 A 的 `VariableID` 已是该类型的 alias，不需退化为字符串或重新生成 ID。

| 对象 | 固定内容与限制 |
| --- | --- |
| `ProjectVariableIntent` | 原 Human Actor、ProjectID、VariableID、原 CommandIdentity、create/update/delete、外部 expected 的 presence/value、name/description/value 各自 presence 与原值；value 仅为 `SecretMaterial`。构造复制元数据并校验闭集，格式化/JSON/log 不暴露材料或材料摘要。 |
| `ProjectVariableWriteRequest` | 无材料的授权请求；包含 Actor（当前 Session）、Project/Variable、原 identity、operation、外部 expected。不得包含派生 CredentialRef、内部 Credential version 或新生成 receipt ID。 |
| `ProjectVariableWriteBasis` | 不可变 opaque authority 结果，绑定 issuer、同一 Store、请求、发现时的 Variable version、CredentialRef/version，或 create 的精确未发行 ID/不存在事实；锁集合规范化、复制。历史 receipt 路径也必须绑定其原映射。 |
| authority stages | `ReceiptRead` 先通过当前 Session/Project gate，核对 D10 原 completed receipt 或确实未观察到；`NewWrite` 才核当前 Mutate、类型、前像和 expected。新的 name syntax/reserved name/shared namespace 由持同一最终 Tx 完整锁的 D10 Owner 消费 Intent metadata 校验；该无 name 输入的 authority 不声称验证新名称。方法在真实 caller Tx 内确认同 Store、live Tx 与完整持锁，不开第二事务或补锁。 |
| prepared / observation | prepared 只能由真实 D04 producer 签发，绑定本 Service、原 intent 与 authority basis；公开 observation 只暴露供 D10 继续写 canonical 的安全 ref/version/effect，不暴露 digest、sealed bytes 或 private Audit witness。 |

CommandIdentity 必须复用 D10 A：namespace `projectvariable`、ownerIDs 恰 `[ProjectID]`、完整 command `project.secret_variable.create/update/delete`、原 idempotency key。create 有 name/description/value 且无 expected；update 至少一个可改字段且有 expected；delete 只有 expected。SecretMaterial 的借用/销毁遵循原 contract；调用者持有材料的生命周期不能被公开摘要替代。D10 业务名称规则仍由其正式 contract/authority 校验，D04 不另造一套规范。

### 2.1 固定 producer / authority 签名

以下均定义在 D04 contract；`ProjectVariableWritePlan` 为 authority opaque plan，`ProjectVariableWriteObservation` 为下文安全结果。实现不得要求 D10 import Secret implementation。

```go
type ProjectVariableWriteAuthority interface {
    Discover(context.Context, ProjectVariableWriteRequest) (ProjectVariableWritePlan, error)
    CheckPlan(ProjectVariableWriteRequest, ProjectVariableWritePlan) error
    CheckInTx(context.Context, foundation.Tx, ProjectVariableWriteRequest, ProjectVariableWritePlan, ProjectVariableWriteStage) error
}
// Stage 闭集仅 ReceiptRead / NewWrite；零值及其他值拒绝。
type ProjectVariableWrites interface {
    PrepareProjectVariableWrite(context.Context, ProjectVariableIntent, ProjectVariableWritePlan) (PreparedProjectVariableWrite, error)
    MatchProjectVariableIntentInTx(context.Context, foundation.Tx, PreparedProjectVariableWrite) (ProjectVariableWriteObservation, error)
    ApplyProjectVariableWriteInTx(context.Context, foundation.Tx, PreparedProjectVariableWrite) (ProjectVariableWriteObservation, error)
    LookupProjectVariableWriteInTx(context.Context, foundation.Tx, ProjectVariableWriteRequest, ProjectVariableWritePlan) (ProjectVariableWriteObservation, error)
}
type PreparedProjectVariableWrite interface {
    Preparation() (ProjectVariablePreparation, error)
    RequiredLocks() ([]foundation.LockRequest, error)
    Destroy()
    fmt.Formatter
    slog.LogValuer
    json.Marshaler
}
```

`CheckPlan` 是绑定实际 authority 对**原 request + 原 plan**的纯验真：用其私有 issuer 与原 request binding 确认同实例、原候选及不变安全 basis/完整锁；无 IO、不开 Tx、不重发现、不重新分配或替换 candidate。D04 Prepare 在调用任何 seal/nonce 准备之前经此端口验证。它不重验当前 Session/Owner/Project 权限，不能代替 ReceiptRead/NewWrite 的实际当前检查；零 plan、其他 issuer/请求/Session 或伪造/漂移均拒绝。不导出 issuer getter、token、callback 或 late setter。不能以 caller 自己的 Validate 声称验真，也不能对 Discover 两次结果强求全等：合法 create 的 Discover 可产生不同随机候选 Ref，二次重发现会错误地永久拒绝合法 create，忽略该字段或静默换候选同样不允许。

`ProjectVariablePreparation` 是 opaque、不可反序列化的安全投影：原无材料 Request、`foundation.ID[ProjectVariableReceipt]` 类型的 ReceiptID 与同 Project CredentialRef。字段读取保留 copy 语义；历史路径给原 receipt/ref，新路径给本候选 receipt/ref，不能把二者混用。该投影与复制的完整锁供 D10 在最终事务之前构造原 Outbox 私有 discovery witness；最终 observation 不能替代这一准备阶段。它们本身均不是授权、已提交事实或 private Audit witness。固定安全 fmt/slog/JSON；不公开 key epoch、nonce、sealed data、semantic digest 或材料。

真实 Secret producer 返回本实现包的私有 concrete pointer，私有 state 绑定同一 Service issuer、原 intent/plan 及 sealed 候选；不使用 `any`、公开 unwrap、callable callback 或可制造的授权 bool 传递状态。Match/Apply **先**检查精确私有 concrete 类型、typed-nil、私有 state 非 nil、同 Service issuer、共享状态未 Destroy；在这些检查之前不得调用接口对象自带的任何方法。外部 interface 实现、包装真实 handle、另一 Service 的真实 handle 都拒绝，不能用其 Preparation/RequiredLocks 自证权限。已通过检查后仍须验证同 Store 的活 Tx、全部持锁与原 authority stages，不把 Go 类型等同授权。

所有 prepared 副本共享受保护的退休状态。Destroy 幂等清除 D04 自有可控临时摘要/材料、退役 sealed 候选；后续 projection/locks/Match/Apply 拒绝。它不销毁调用者仍持有的原 Intent 或原 SecretMaterial。Preparation 和 RequiredLocks 不能返回内部可变切片/指针；未知或已销毁对象不返回似乎有效的空锁集合。

Observation 闭集为未观察／完整安全结果，后者含原 receipt ID、Project/Variable/稳定 User/原 command、外部 expected、CredentialRef、create/replace/delete/none effect、结果 Credential version、deleted。Match 先 ReceiptRead，再在 D04 内常量时间比较原 intent；未观察与匹配有明确类型，KeyReused 返回正式 fault。Apply 在同 Tx 再核 NewWrite；Lookup 仅 ReceiptRead 且不含原语义比较。返回的公开安全结果必须与 D10 的真正 completed/history/canonical 验证相合，不作为任意 caller 自签事实。

ReceiptRead 的当前 Session/Owner/Project gate 在读历史 receipt **之前**执行，并绑定 D10 原 writer/completed receipt 或确实未观察到。NewWrite 除相同当前权限外核当前 Mutate、external expected、类型、映射前像/create 精确未存在，不读取尚未产生的 D10 postimage。名称规则由 D10 Owner 在同一最终 Tx 单独验证，任一失败都回滚 D04/D10 同一笔业务事务；不能以 Prepare 时的名称检查取代最终持锁校验。

## 3. 原语义、幂等与事务

原语义使用版本化、有长度边界的确定编码，覆盖稳定 User/Project、command、Variable、外部 expected presence/value、各字段 presence 和原字节。不能加入当前 canonical、内部 Credential version、随机 Ref/receipt/event ID、Session/CSRF/RequestID；这些会使较晚重放失去原语义。当前授权仍每次检查，不从历史 receipt 继承。

Prepare 在事务外分配 nonce 并密封必要材料，不写 durable planned row。发现已有历史 receipt 时只准备安全比较，不密封新的业务值。Match 在 D04 内解密原 32-byte 语义摘要并作 constant-time 比较；不对外暴露裸 SHA，身份-only Lookup 不证明语义相同。相同身份不同原语义返回 KeyReused；合法历史重放不得因 canonical 后来改变而变成新写。

最终由 D10 开一个 Tx 并一次 AcquireAll 完整锁并集：原 command EX、User EX、Project EX、Secret write-key SH、涉及每个 CredentialRef aggregate EX 与既有协议要求的全部锁；D04 只 RequireHeldLocks，不临时补锁。顺序保持 D04 native 写值/专用 intent receipt/必要 Secret Audit → D10 canonical mapping/version/generation/history及同Tx私有证明 → D10 Audit实际返回AuditID → D10完整completed → Outbox/Activity；D10证明与顺序窄修见其§6.2。Apply 重验 issuer、当前权限、映射、purpose、version 和 reference/lease 约束。effect none 也写专用 receipt，但不伪造 Secret value 更新或 Secret Audit。

并发 create 的获胜映射若与事务外候选 Ref 不同，应退出事务、重发现并按 D10 原有限重准备规则处理；不在锁内加锁或无界重试。Unknown 保留原 attempt/cause/CommitResult，D04 不私自另开确认事务或重跑 callback；由 D10 按原一次、有界确认协议处理，不能把未提交/未知都变成成功。

## 4. 精确 SQL 与 index 需求（00029）

[00029 正式迁移源](../../../db/migrations/00029_project_variable_receipts.sql)已由真实 Migrator 与本域 SQL 矩阵覆盖。本批保留既有 00001..00027，先装配已接受的 D05 00028，再连续加入 00029 和 Owner 00030；本域测试不替代前缀各领域的业务与成本验收。00029 只包含下列 D04 存储对象，D10 Owner 的 canonical/history/completed 由独立 00030 负责。

1. `secret_payloads.owner_kind` 扩为 1/2/3；kind3 仅 Project scope，摘要明文恰 32 bytes、密文恰 48 bytes。kind1/2 既有约束和编码保持。不能修改历史 migration00003，后续使用独立已授权迁移。
2. `secrets.purpose` 新增仅 Project scope 的 `project_variable`；旧 `secret_command_receipts.purpose`、`secret_references.consumer`、`secret_leases.consumer` 闭集不扩大，避免经旧 API 或执行消费者旁路。
3. 新 `agenteam_secret.project_variable_receipts`：safe_id 主键、ProjectID、VariableID、稳定 UserID、原 CommandIdentity digest、完整三种 command kind、外部 expected nullable positive version、原 CredentialID、安全 effect、result Credential version/deleted、digest_payload_id。presence 由 expected NULL/非 NULL 精确表示；operation/effect/deleted/version 组合设闭集 CHECK：create 命令只能 effect=create、expected NULL、result version=1、deleted=false；update 只能 replace/none、expected 非 NULL，replace 的 result version≥2，none 保持原正版本，deleted=false；delete 只能 effect=delete、expected 非 NULL、result version≥2、deleted=true。external expected 是 D10 Variable version，不能错误地与内部 Credential version做等值/加一约束；内部版本前后关系由真实 Apply 与 receipt 证明。
4. UNIQUE `(project_id, command_digest)` 与 UNIQUE `digest_payload_id`；payload FK 使用现有 deferrable 协议；`(project_id, id)` 支持 Project 批量清理。不得加入 D10 canonical 或当前 Credential 的 FK/CASCADE，否则历史 receipt 会随删除丢失。rotation 反查必须同时比 receipt id、digest_payload_id、Project/scope，并取原 CredentialID；不能只信 owner_id。
5. 现有 payload rotation/project indexes 可服务 kind3；不先增加全库扫描、材料索引或明文/摘要列。精确 SQL CHECK 和执行计划须在迁移号授权后，以正式 schema/真实 PG 定向证明；纯测试不称 SQL 已验。

## 5. kind3、rotation、cleanup 与旧入口隔离

kind3 的 AAD 沿既有 version/domain/scope/project/owner-kind/owner-ID/payload-ID 编码，其中 owner-ID 为专用 receipt ID；不改 kind1/2 任一字节。反例须覆盖 kind/scope/receipt/payload/Project 替换及 31/33-byte 明文，旧 kind1/2 golden 同时保持。

rotation 每批仍最多 100，last-ID/head rescan、canary 和 Retire 的完整性规则覆盖 kind3。reverse owner 必须来自新 receipt，历史 Credential 即使已删除也以原 ID 参与规定的锁。owner 不存在而 payload 仍存在为安全失败；在持锁重验时 payload 确已清理才可跳过，不能把失读当删除。

CleanupProject 保持真实 D08 deleting gate/操作与 cause，既有 reference/active lease 返回 pending。每轮在原事务中按最多 100 个新 receipt 删除对应精确 kind3 payload；保留旧 secrets 与 kind2 receipt 的原批量上限。Completed 仅在 Project 的 canonical Secret、旧 receipt、新 receipt、payload 均清空后发布；Unknown 不推断清理完成。

旧 Human Model、Account Service、reference/lease 接口都不签发新用途。除请求 purpose 之外，旧 Apply 对读取的现存 Credential purpose 也须拒绝新用途，阻止伪造旧 purpose 更新或删除；专用 D04 Apply 反向只操作该 Project 的新用途。F1 adapter 仍未绑定，不用假 provider/默认允许填补。

## 6. native Audit 与验收边界

只有实际 native Apply 完成后，D04 才产生私有 ProjectVariable Audit witness，绑定同 Store、当前 Tx、Actor、Entry/IdempotencyKey、原 command、kind3 receipt/payload、CredentialRef、前后 version 与真实 canonical 结果。旧 mutation、resolution、新 ProjectVariable variant 互斥；effect none 不产生值变更 witness。Audit authority 必须重新读到对应事实，不能靠公开 prepared/basis 自证，也不能把 D10 metadata Audit 当 D04 Secret value Audit。

纯测试先覆盖闭集 Intent/Request、opaque issuer 与 copy/mutation、保密输出及材料销毁、原语义稳定/区分、kind3/AAD 及旧 golden、stage/错误与 Unknown 分类。后继真实 PG 必须验证 receipt replay/KeyReused/并发 create 重发现/历史映射、所有 current gates、完整锁并集、原子回滚、rotation/canary/Retire、100/101 cleanup、deleted Credential 和 private Audit witness 负控。独立审查针对实际固定差异与可复跑控制；不把无 PG 的 pure 或编译结果计入这些矩阵。

SPEC的prepared/名称/表名三缺口及CheckPlan增量已获Runner有限复核；Intent、crypto/plan/prepared/read/Prepare、Apply/nativeAudit与rotation/Cleanup均已获Skills对应有限独审，00029获Runner限定DDL审查，SQL与recovery tests/入口亦已独审。旧入口Purpose.Valid保持闭集。独审边界与固定版本证据见[原 D04 恢复记录](https://github.com/LunaDeerTech/agenteam/blob/b724e397dce837f7bc9af7f83ad4d9d93ee311f9/.agent-state/current.md)，不以 pure 控制替代真实 PG。

作者完整 Secret implementation/contract 两包纯 race 已实际通过（5785→713106）。新的纯测试调用实际 Service/AEAD/native facts checker，Store/当前 authority/Append transport/commit 是明确受控端口，不能据此宣称真实 D10 权限、PostgreSQL SQL/原子回滚、100/101 cleanup 或 Unknown 数据库结果。具体入口、冻结输入及有限范围保存在[原 D04 恢复记录](https://github.com/LunaDeerTech/agenteam/blob/b724e397dce837f7bc9af7f83ad4d9d93ee311f9/.agent-state/current.md)。

本库固定 Unknown/并发矩阵已实际通过。D04 矩阵使用明示受控 authority；真实 D10 authority provider 与 Owner final-Tx 由后续 Owner 库矩阵和独验另行覆盖。整合按实际差异补验，不把 D04 fixture 当作真实 Owner，也不机械重跑未变矩阵。

## 7. 已验有限结果

core3首次真实PG在固定候选742/driver831上完成：原session48021→11fe77实际outer0，三top＋14sub共17节点PASS，Go/driver实际Wait0、精确两资源双退役、凭据退役、后代双空、TCP双空增量、输入未变全部齐备。该组证明本树连续Migrator含00029、D04四effect/历史幂等/current deny、真实目标回滚及九DDL负例；25–28依赖迁移成功不代表其业务或28执行计划验收。

maintenance复用同候选/driver，原session3222→0a46e7实际outer0、69.661s完整尾；唯一top PASS2.51s，真实101历史receipt/删Credential后轮换与Retire/仅key2重启重放/100+1清理通过，原Wait、两资源/private/后代/TCP/input尾全部齐。D10权限来源仍为明示受控fixture，真实Unknown/并发和正式Owner未验。两原日志路径、资源身份和终态见[固定 D04 恢复记录](https://github.com/LunaDeerTech/agenteam/blob/b724e397dce837f7bc9af7f83ad4d9d93ee311f9/.agent-state/current.md)；未重编或修改旧验收输入。

write/nonce恢复首窗使用独立a7a7候选/8f1driver，原session76000→cc555a本人实际outer0，完整76.654s；原COMMIT before/after/pending及nonce ACK-loss四格、5精确节点PASS，实际Go/driver Wait、代理Close/wg与Store退役、两资源双退役/private/后代/TCP双空/input未变齐全。此证据保留真实Unknown/原Cause、目标COMMIT与nonce持久事实的界线；第二组maintenance Unknown/并发六格尚未启动，不扩为十格全过。当前D10仍为受控provider，正式Owner/HTTP与main连续迁移交付不在本次结论内。

maintenance Unknown/并发末窗沿同冻结候选与driver，原session54310→497aa8本人实际outer0、78.814s完整尾；Concurrency5.13s与MaintenanceUnknown5.36s，共6目标格/8精确节点PASS，真实双方PID/精确锁等待、目标COMMIT、原flight/proxy/Store实际退休及原Wait/两资源/private/desc/TCP双空/input尾齐。两窗固定十格至此到齐，不新增默认恢复矩阵；仅D04 producer有限结果，D10正式Owner、F1与材料消费仍未验。
