# 恢复：Object 的 Project Audit 真实事实校验

修订：rev1。状态：规格已通过独立静审并由主线程采纳；实施仍等待 S2 修复后完整独立验收及最终接缝 delta 核查，本卡尚不授权代码开写。

## 完整结果与固定输入

沿 [D08 §9.2](d08-project-owner-design.md#92-当前权限对象与-audit-分派)恢复独立 Object `ProjectFactAuthority`，把既有真实 upload/transfer/cleanup 调用点接入同 Tx 事实见证，并以真实 Object、Audit、PG、MinIO 验证成功、拒绝、原子回滚与 Unknown。普通 Object reader 没有本口新增动作；Downloads 的 issued/started/terminal Audit 仍由 [DownloadProvider](../../../internal/central/object/contract/download.go) 分派业务域 typed action，不冒领为 ObjectProducer。

固定基线 `49c6589c3919cad62a4bae2c993b5fd953d36b1e`。只读 S2 样本为 `/tmp/agenteam-d05-s2-author-ufzmxn_g/production-review-01/input.json`，SHA-256 `b81c98af9084172e952a4af5b02680738efc26c07e53d17d7e8761bab218aa45`；其 18 个生产源逐项校验后复制到作者独立快照。该样本不是已验收实现。S2 当前暴露 DiscardPrepared 在 PUT close 后、verify/finishWriter 尚未完成时提前释放 operation 的实际 join 缺陷，旧 SourceLease force 也失败；这些修复不属于本卡。必须等 S2 固定通过后逐接缝 delta 核查，尤其 upload/access/project_lifecycle，不能沿旧 hash 直接实施。

不需要额外 schema，不占 00017。已有 objects/uploads/upload_attempts/cleanup_operations/object_transfers/leases 及 S2 project_stops/work 足以表达持久事实；私有见证只补当前调用点、锁和临时验证结果，不成为恢复记录，不改变任何数据保留规则。Secret checker 已验不代表 Object 或 Project 映射已绑定。

必读 [AGENTS](../../../AGENTS.md)、[团队流程](../agent-team/README.md)、[设计技能](../../../.agents/skills/agenteam-design/SKILL.md)、[Go 技能](../../../.agents/skills/agenteam-go-development/SKILL.md)及 [S2 恢复卡](recovery-d05-object-stop.md)。真实资源按主线程唯一窗口交接，沿 [依赖恢复记录](../agent-team/dependency-recovery-2026-10-05.md)使用原 Go/PG/MinIO、race 和预算。

## API、构造和边界

API 位于 `internal/central/object`，复用 [Audit 既有接口](../../../internal/central/audit/contract/project_fact_authority.go)：

```go
type ProjectAuditAuthority struct { /* private Store */ }
func NewProjectAuditAuthority(Store) (*ProjectAuditAuthority, error)
func (*ProjectAuditAuthority) CheckProjectAuditInTx(
    context.Context, foundation.Tx, audit.Entry, audit.AppendKey,
) error
```

只依赖同一个 Object Store，不依赖 Object/Audit Service、Backend、Project 实现或运行时注册表；构造零 SQL。nil/typed-nil Store、零值 checker 返回 DEPENDENCY_UNBOUND；不可安全比较身份的 Store 实例返回 INVALID_ARGUMENT，必须按实例动态值判断，不能只检查类型是否 comparable。跨 Store/Tx、过期 Tx、缺实际见证或事实不匹配均拒绝。错误沿 Object 既有闭集与安全包装。

只受理 Project scope、ObjectProducer、ObjectService actor、对应 Object/ObjectTransfer resource 以及下表六种 action；全 Entry/AppendKey 精确绑定。当前 Session/Owner/业务父权限及 Project gate 仍由正式上游负责，checker 只读 Object 本域事实，不查 Project/Account/Runner 私表，不提供宽松 allow。

构造顺序为 Object Store → 独立 checker → Project 受限 AuditFacts 映射 → Audit Service → Object Service。生产 Project 映射后继增加 ObjectProducer，先核当前 gate/actor，再调用本 checker；缺 provider 拒绝。维护、停止、清理必须保留各自真实权限口与原 cause，不能由 ObjectService+UUID 或 Owner grant 替代。

## 固定 action、原 cause 与实际事实

下表的 Entry actor 均为原 ObjectService、CauseRef=原 AppendKey cause；Object metadata 的 initiator 取持久 upload/transfer 原始发起人，不能替换为当前重试 Session。普通重试的当前 actor 仍须通过当前权限，两者角色不同。

| 动作与固定 key | 必须核查的真实事实与见证来源 |
| --- | --- |
| `object.upload.complete`：upload ID，ordinal 0；Success/PublishedPhase | `PublishVerifiedInTx` 已 ValidateAccessPlan、当前 Resource/Project gate 与 native work 校验；当前 upload/owner/object 绑定、未 revoked/cleaning、current attempt 精确匹配且 verified、size/digest 匹配对象。Audit 仍在 objects available / attempt published / upload committed 更新前；见证证明实际 publish 调用点，不能要求尚未发生的后态。Audit 与全部 publication 更新同 Tx |
| `object.upload.failed` writer：attempt ID，ordinal 0；Unknown/FailedPhase | `finishWriter` 的 FinishWriterAccess 已授权；本 attempt 的 io_closed/phase、原 process writer lease 释放、upload 状态更新已真实执行。核原 attempt/upload/object/owner 与安全 reason，不能把历史另一 attempt 的失败归给 current attempt。不能统一要求 upload=unknown：原 SQL 保留 committed upload |
| `object.upload.failed` recovery：同 attempt ID，ordinal 1；Unknown/FailedPhase | 实际 backend verify 后的 RecoverAttemptAccess；`gateAttempt` 已把本 attempt 标记 cleanup/abandoned 并保留原 cleanup operation。核 attempt/upload/object 与 cleanup 行；安全 reason 来自本次真实验证结果，不能从任意 UUID/Entry JSON 倒推已做 I/O |
| `object.delete`：`sha256:hex(SHA256("object.delete.v1\x00" + earliestOperationID + "\x00" + objectID))`，ordinal 1；Success/DeletedPhase | `finalizeCleanup` 的 FinalizeCleanupAccess；对象 cleaning、零引用/active lease、全部 attempt cleaned；重读 `cleanup_operations ORDER BY created_at,id LIMIT 1` 的最早 operation_id，不能换成最后 claim/current Project operation。Audit 仍在 deleted UPDATE 前；真实清理终局与同 Tx 最终状态更新共同成立 |
| `object.transfer.issue`：transfer ID，ordinal 0；Success/IssuedPhase | `TransferService.within` 已核正式 Runner/Operation、当前 owner/gate、计划与本域绑定；新 transfer issued 行已 INSERT，核 owner/project/object/direction/manifest、runner generation/operation version/execution、GET 真 lease 或 PUT 真 staging/upload 绑定。签名 material 只能在实际提交确认后返回 |
| `object.transfer.complete`：transfer ID，ordinal 1；Success/SentPhase，SentBytes=manifest length | GET Capture 已核正式 completed evidence 并 UPDATE complete；PUT Publish 已核原证据、真实 private candidate verified/publication、再 UPDATE transfer complete。helper 收到的 r 可能仍是旧 phase；checker 必须重读实际行，核 evidence/digest、manifest/object/owner 与对应方向真实阶段，不能用旧 r 充当 canonical |
| `object.transfer.revoke`：transfer ID，ordinal 2；Success/RevokedPhase | 普通 Cancel、Project Cleanup、S2 Continue stop 三来源的真实授权和本域行。统一在 `revoked_at IS NOT NULL` 的实际后态 Append；不统一要求 phase=failed 或 cleanup_gate=true，合法 complete 和 S2 stop 都可保留这些字段。撤销事实不证明远端已停止、lease 已退休或 I/O 已 join |

upload failed 有两个 ordinal，仍是一个 action。所有 typed metadata 保持原字段与 MIME 规范化、outcome、phase、reason；不新增 Audit action 或 metadata。

### 已采纳的唯一顺序窄调

`transfer_recovery.go:gateProjectTransfers` 原来先 Audit 再批量 revoked UPDATE。主线程已采纳：在原 Project EX + 本批 Object 锁、同一个 Tx 内，固定原待撤销 ids/原 metadata，执行原 predicate/字段的 UPDATE，再按原 transfer ID/ordinal 2/metadata Append。失败回滚 UPDATE 与全部 Audit；Unknown 仍对应原 Tx，恢复 key 不变，不增加 I/O 或提交点。checker 不接受“尚未 revoked 但 Success”的专用宽松分支。其它 publication/delete 原顺序沿正式 D08 规则，不扩大改排。

## 私有 witness 与锁来源

Object Store 当前没有 RequireHeldLocks，不扩旧 Store interface。只能消费现有已经 AcquireAll、Validate 成功的计划/token，制作包内私有、当前 Tx 的安全阶段证明；checker 自身只用 Store.InTx 核活 Tx，再读本域行，不 Acquire、不嵌套 Tx、不调用 Backend/权限 provider。

- `withinAccess`：在既有 Acquire/Validate 与 accessWorkBefore 成功后，仅为 FinishWriter、RecoverAttempt、FinalizeCleanup、GateProject 这些相关闭集操作，给原 callback 传私有 context 证据。原回调的真实阶段/权限检查继续执行，不能用 context 代替。由此 recovery.go/cleanup.go 保持只读。
- `PublishVerifiedInTx`：支持外部业务 Tx 组合，不能假设经过 withinAccess；在本函数现有验证及真实 verified 前置完成后、Append 前制作自身 exact plan/LockedAccess 证据。不得把传入 token 的存在当作 Validate 成功。
- `TransferService.within`：正式 transfer authorize、extras Validate、既有 work 登记/复核成功后，为 Issue/Capture/Publish/Cancel callback 传同 Tx 私有证据；包括真实 authorization 的安全 evidence/generation/version 关联。GET/PUT 本域检查继续沿原路径。嵌套 Object publication 的局部 context 不覆盖外层 transfer 证明。
- S2 `project_lifecycle.go` 的 gated Continue 回调：在原 projectStopTransaction 已 Acquire 完整锁、Validate 当前授权并重核 facts，且真实本域 stop 行存在后，制作限定原 cause/本批对象的证据，再调 revokeProjectBatch。Read/Inspect-only 不产生写审计资格；不改 project_stop_store.go 或其授权矩阵。

证明只复制安全操作种类、原 actor/owner/ID/cause、规范锁集合、阶段/evidence 身份与必要 metadata。普通 Access 证明须核私有 issuer/LockedAccess 对本次 Tx/request 的精确绑定；stop 证明须核原 ProjectStopAuthorization.Matches(tx,request,dependencies)、Continue mode 和已经完成完整 Acquire/Validate/facts 复核的原 callback。不能把持有一个计划或授权值等同于已经 Acquire。证明不保存 PreparedPayload、AccessPlan 中可能携带的 payload、Backend key、source/reader、签名 URL 或服务指针。无导出 mint/WithWitness、全局表或跨事务缓存。

实际集中 Append 再封装一次绑定完整 Entry、完整 AppendKey、同 Store/Tx 与安全本域事实的最终 witness；不能仅用 SemanticDigest（它省略 Session/HTTPTrace）。所有九个 associations、ActorDetails、resource、metadata 内容均比较。相同活 Tx 的精确重复可检查；跨 Tx/Store/Entry/ordinal/producer 重用或根据历史 receipt/行直接调用 Audit.Append 均拒绝。安全 reason 可是短事务临时事实，但对象 bytes 与未清洗错误不进 witness/日志。

## 候选实施范围：13 个路径

| 类型 | 精确路径与窄限制 |
| --- | --- |
| 旧源 1 | `internal/central/object/upload.go`：集中 Append 接 witness；PublishVerifiedInTx 的显式阶段证明。其余 upload/PUT/finishWriter/join 顺序不改 |
| 旧源 2 | `internal/central/object/transfer.go`：集中 appendTransferAudit 接 witness、重读事实；不改普通签名/传输流程 |
| 旧源 3 | `internal/central/object/access.go`：仅 withinAccess 的上述私有 context 传递；既有 Acquire/Validate/work before/after 顺序保持 |
| 旧源 4 | `internal/central/object/transfer_access.go`：仅 within 在原完整授权后传私有 context；不增加锁或权限捷径 |
| 旧源 5 | `internal/central/object/project_lifecycle.go`：仅 gated Continue 回调为 revokeProjectBatch 建立上述证明；不改变 cancel/Unknown/真实 join/只读终态 |
| 旧源 6 | `internal/central/object/transfer_recovery.go`：仅 gateProjectTransfers 的同 Tx UPDATE→Audit 窄重排 |
| 新生产 2 | `internal/central/object/project_audit.go`、`internal/central/object/project_audit_witness.go` |
| 新单测 2 | `internal/central/object/project_audit_test.go`、`internal/central/object/project_audit_witness_test.go` |
| 新真实集成 3 | `tests/objects/project_audit_test.go`、`tests/objects/project_audit_transfer_test.go`、`tests/objects/project_audit_fixture_test.go` |

六个旧文件全部与未验 S2 重合：当前只有规格所有权，没有代码所有权。S2 验收后重新固定输入并逐接缝 delta 核查；若修复使闭合 context 方案不成立，先报告最小范围差异，不能沿候选强行旁路。recovery.go、cleanup.go、transfer_complete.go、project_stop_store.go、旧测试、Audit/Project/contract/迁移保持只读。

## 必须验收的完整结果

测试以 `TestObjectProjectAudit` 为共同前缀；使用真实 Object/Audit/PG/MinIO与真实 plans、leases、attempt、cleanup、transfer 行。测试专用 Project/Owner/Runner/Stop ports 只隔离尚未绑定的上游，必须核当前状态、映射、actor/cause/同 Tx/所需锁并真正调用生产 checker，不返回恒定 allow。停止与维护仍走各自正式接口，不以生产假 adapter 补缺。

- 构造 nil/typed nil/零值/动态不可比较 Store；全 Entry/Key 篡改、缺/错/跨 Store/过期 Tx witness、缺真实 Acquire/Validate、跨 action/阶段/原 cause/ordinal 均拒绝；checker 零 Acquire/写/外部 I/O。
- 真 PUT verified→publish（包括外部组合 Tx）、writer failed ordinal 0、真实 recovery verify failed ordinal 1、真实删除清理后最终 Audit；改 wrong owner/attempt/digest/phase/最早 cleanup operation 即失败。审计失败不提交 publication/deleted 状态，不多做一次已 verified PUT；旧 System/Avatar 不受 Project checker 缺失影响。
- 真 transfer GET/PUT issue/complete/revoke，检查实际 evidence、lease、staging/candidate 与当前行；覆盖原 r 仍为旧 phase。普通 Cancel、Project Cleanup、S2 archive/delete stop 三种 revoke 均需后态；Audit 失败回滚撤销/stop 本域写入且零提前 cancel；只读 terminal Inspect 不新写 Audit。
- 原子回滚、同 key 重放零新增 Audit；受控真实 COMMIT ACK 丢失命中实际 publication/transfer/revoke Audit Tx，验证 committed/rollback/仍未决 Unknown，不把 prepare/work admission 算作最终提交。等待原 writer/锁实际终局再判事实；不重复 PUT/GET/签名 material，不把缺行或 revoked 当作真实 join。
- 撤销当前 Session/Owner/Project/Runner/Stop 权限后拒绝，不以历史 receipt、旧 admission 或私有 context 绕过。普通 Downloads 仍走原业务 provider 审计，合法 archive 读、delete lease fencing、S2 actual join、cleanup/恢复关键旧组合回归保持。

实现自测需相关 Object/Audit 单元、race、vet/编译及新增真实 PG+MinIO组；按接缝补旧 publication/transfer/download/stop/cleanup 回归，保留原始红日志，不弱化断言或延长预算。冻结精确源清单后交未参与实现者独立验收。当前只完成候选规格静态检查，没有本块 Go/PG/MinIO 行为通过声明。

生产 Project ObjectProducer 映射及相应普通/维护当前 gate 分派由后继 Project 绑定块负责，不因 checker 可构造就宣称完整 D05/D08/D10/D12 已装配。D10 Skill 初始化专用授权与 D12 Knowledge owner/source/DownloadProvider 继续由各自真实领域负责。

## 规格采纳记录

2026-10-05，独立 verification_worker 对原候选卡（SHA-256 `85898b95c246a00386257fc9da0660bf1297d8f7aa1b7c98f596d305c35064a7`）完成静审，未发现确定硬阻断；9 个链接/fragment、13 个实施路径、6 个 S2 重叠源与 7 阶段/6 actions 均核对通过。主线程复核 D08 §9.2 对 publication/delete 写前审计的明确规则，以及同一事务回滚边界后采纳。原审查报告 `/tmp/agenteam-object-audit-spec-review-gmi9y43k/review.md` SHA-256 `d9d85cf25abd0fb56ea691b64ba79081543c38ba5a438549cd711a2d799283b9`，检查结果 `checks.json` SHA-256 `c916e49f9f7d6faab7eba1ac2807895a30d3cdeba4b636cc40b9b00fa0854242`。临时文件仅为本次过程证据，以上结论及实施门槛以本卡持久记录为准。

本次没有 Go、PG/MinIO 或产品行为通过声明。S2 两项实际缺陷已有作者修复和部分复验，尚未完成全部兼容回归及独立验收；不得用本次规格审查替代此前置。
