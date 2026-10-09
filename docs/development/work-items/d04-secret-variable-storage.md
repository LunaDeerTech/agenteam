# D04 Project Secret Variable 存储 producer

状态（2026-10-09）：SPEC rev1 待独立审查；尚无产品实现、迁移或真实 PG 验收。工作树基线为正式 main `8cb0a95338dc417ff34be06c00086fbbc7159efa`。本卡落实已接受的 [D10 rev2](d10-secret-variables-owner.md) §4、§6，不改变 Secret Variable 的业务语义。D10 A 的五个 contract、三个测试及 schema 已正式交付，不等于本 producer 或 D10 Owner 已实现。

## 1. 范围、所有者与构造顺序

D04 独占 `internal/central/secret/contract/project_variable.go` 及其纯测试、Secret 实现内的专用写入和 receipt、envelope、rotation、cleanup、private Audit witness 必要增量。既有 Account、Model、kind1/2 的行为与格式必须保持。不得仅扩大 `Purpose.Valid`：旧 `validateWrite` 会因此接受新用途，且旧请求也可能以旧 purpose 指向新用途的现存 Credential；两个入口均须显式隔离。

D10 Owner 后继工作拥有专用 authority 的真实 provider、canonical/history/completed receipt、外层锁并集及同一最终事务中的 D10 Audit/Outbox/Activity。独立 `SecretWriteAuthority(Store, current Account/Project gates)` 在 Project 构造后、Secret Service 构造前产生，作为 Secret 构造时的不可变端口；不得让现有 `projectvariable.NewAuthority(Store)` 事实提供者反持 Project/Secret Service。禁止 late setter、locator、生产 stub 或以 Model Lookup 替代。

Knowledge 的独立 Audit 工作只做严格合同与现有 Go/HTTP/schema/TS 读端兼容，不拥有 D04 的 private witness 或 D10 Owner 写事务。公开 signature 的确定增量须先与 D10 消费者核对并交 root 备案；签名兼容核对不是独立验收。root 拥有 app 最终装配、Git、迁移号与正式交付。当前未授权任何迁移号，28 已预留给 Skills。

## 2. 专用端口与不可变数据

新端口只接受 Human Actor 与 Project scope，所有 ID 使用 Foundation/Identity 的正式 typed ID。D04 contract 不 import D10 implementation 或 contract，VariableID 直接使用正式 `identity.ProjectVariableID`；D10 A 的 `VariableID` 已是该类型的 alias，不需退化为字符串或重新生成 ID。

| 对象 | 固定内容与限制 |
| --- | --- |
| `ProjectVariableIntent` | 原 Human Actor、ProjectID、VariableID、原 CommandIdentity、create/update/delete、外部 expected 的 presence/value、name/description/value 各自 presence 与原值；value 仅为 `SecretMaterial`。构造复制元数据并校验闭集，格式化/JSON/log 不暴露材料或材料摘要。 |
| `ProjectVariableWriteRequest` | 无材料的授权请求；包含 Actor（当前 Session）、Project/Variable、原 identity、operation、外部 expected。不得包含派生 CredentialRef、内部 Credential version 或新生成 receipt ID。 |
| `ProjectVariableWriteBasis` | 不可变 opaque authority 结果，绑定 issuer、同一 Store、请求、发现时的 Variable version、CredentialRef/version，或 create 的精确未发行 ID/不存在事实；锁集合规范化、复制。历史 receipt 路径也必须绑定其原映射。 |
| authority stages | `ReceiptRead` 先通过当前 Session/Project gate，核对 D10 原 completed receipt 或确实未观察到；`NewWrite` 才核当前 Mutate、类型/名称共享空间、前像和 expected。方法在真实 caller Tx 内确认同 Store、live Tx 与完整持锁，不开第二事务或补锁。 |
| prepared / observation | prepared 只能由真实 D04 producer 签发，绑定本 Service、原 intent 与 authority basis；公开 observation 只暴露供 D10 继续写 canonical 的安全 ref/version/effect，不暴露 digest、sealed bytes 或 private Audit witness。 |

CommandIdentity 必须复用 D10 A：namespace `projectvariable`、ownerIDs 恰 `[ProjectID]`、完整 command `project.secret_variable.create/update/delete`、原 idempotency key。create 有 name/description/value 且无 expected；update 至少一个可改字段且有 expected；delete 只有 expected。SecretMaterial 的借用/销毁遵循原 contract；调用者持有材料的生命周期不能被公开摘要替代。D10 业务名称规则仍由其正式 contract/authority 校验，D04 不另造一套规范。

端口实现前尚需锁定一个工程接缝：现有 `PreparedWrite` 位于 Secret implementation，D10 消费者只能依赖 contract，因此专用 prepared capability 的 contract 载体和 producer 接口必须一起定型。不得为了先编译增加 `any`、可任意构造的授权布尔值、公开 unwrap 或 digest getter。该点不阻塞 Intent/request 校验、保密格式和 kind3 纯编码设计；正式 SQL/Service 接线在本 SPEC 与端口独审后展开。

## 3. 原语义、幂等与事务

原语义使用版本化、有长度边界的确定编码，覆盖稳定 User/Project、command、Variable、外部 expected presence/value、各字段 presence 和原字节。不能加入当前 canonical、内部 Credential version、随机 Ref/receipt/event ID、Session/CSRF/RequestID；这些会使较晚重放失去原语义。当前授权仍每次检查，不从历史 receipt 继承。

Prepare 在事务外分配 nonce 并密封必要材料，不写 durable planned row。发现已有历史 receipt 时只准备安全比较，不密封新的业务值。Match 在 D04 内解密原 32-byte 语义摘要并作 constant-time 比较；不对外暴露裸 SHA，身份-only Lookup 不证明语义相同。相同身份不同原语义返回 KeyReused；合法历史重放不得因 canonical 后来改变而变成新写。

最终由 D10 开一个 Tx 并一次 AcquireAll 完整锁并集：原 command、User、Project、涉及 Credential 与既有协议要求的全部锁；D04 只 RequireHeldLocks，不临时补锁。顺序保持 D04 native 写值/专用 intent receipt/必要 Secret Audit → D10 canonical mapping/version/generation/history/completed → D10 Audit/Outbox/Activity。Apply 重验 issuer、当前权限、映射、purpose、version 和 reference/lease 约束。effect none 也写专用 receipt，但不伪造 Secret value 更新或 Secret Audit。

并发 create 的获胜映射若与事务外候选 Ref 不同，应退出事务、重发现并按 D10 原有限重准备规则处理；不在锁内加锁或无界重试。Unknown 保留原 attempt/cause/CommitResult，D04 不私自另开确认事务或重跑 callback；由 D10 按原一次、有界确认协议处理，不能把未提交/未知都变成成功。

## 4. 精确 SQL 与 index 需求（未分配迁移号）

1. `secret_payloads.owner_kind` 扩为 1/2/3；kind3 仅 Project scope，摘要明文恰 32 bytes、密文恰 48 bytes。kind1/2 既有约束和编码保持。不能修改历史 migration00003，后续使用独立已授权迁移。
2. `secrets.purpose` 新增仅 Project scope 的 `project_variable`；旧 `secret_command_receipts.purpose`、`secret_references.consumer`、`secret_leases.consumer` 闭集不扩大，避免经旧 API 或执行消费者旁路。
3. 新 `secret_project_variable_receipts`：safe_id 主键、ProjectID、VariableID、稳定 UserID、原 CommandIdentity digest、完整三种 command kind、外部 expected nullable positive version、原 CredentialID、安全 effect、result Credential version/deleted、digest_payload_id。presence 由 expected NULL/非 NULL 精确表示；operation/effect/deleted/version 组合设闭集 CHECK。create/replace/delete/none 的真实表示与 Apply contract 同时冻结，不能依字符串默认值猜测。
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

当前仍缺真实 D10 authority provider 与 Owner final-Tx、迁移号和具体 prepared contract 载体；本卡不以 stub 越过。先保存本 SPEC/current 供根与独立审查，再落可独立验证的纯合同片段；该片段也不表示整个 producer 可交付。
