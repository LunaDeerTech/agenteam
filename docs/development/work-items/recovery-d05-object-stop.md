# D05 恢复：Object 项目停止闭环

修订：rev4，仅同步阶段状态，rev2冻结行为与既有历史记录不变。S1两源`49c6589`及S2 Object库级停止28源均已独立验收；S2已提交推送`6658a6cb1f29299521773bc8dc86b2f607b8c809`，远端一致。已验迁移前缀至00016，正式结果及后继边界见文末。

## 输入、结果与依赖

固定输入为 `98262b49aa87c52cfb0c09586dd1b76f9aa1fd52`，沿用已提交 C0（`16595ad`）与 00014（`30f5c29`）。中断前未提交 D05 候选、旧 `/tmp` 文件及聊天中的完成声明不作为实现或验收依据。正式规则引用 [D08 设计 §9.1](d08-project-owner-design.md#91-d05-项目停止能力)、[§9.2](d08-project-owner-design.md#92-当前权限对象与-audit-分派)、[现有 stop 契约](../../../internal/central/object/contract/project_lifecycle.go)、[PrepareReadAccess](../../../internal/central/object/contract/access.go)和 [00014](../../../db/migrations/00014_object_artifact_project_stop.sql)。公共契约、旧迁移与四张技术表的既定职责不变；仅通过 00016 为 Object work 补充下文缺失的维护 I/O 身份。

本块的完整结果是 Object 库实际实现项目 `RequestProjectStop`、`InspectProjectStop`，在正常生产入口登记持久 work，按 archive/delete 收敛真实 I/O、原 writer 与恢复事实，并用真实 PG/MinIO 验证。仅添加类型、内部 helper 或通过空表返回 stopped 不满足交付。Object 报告只证明本域停止，不等于 Project 归档/删除完成。

首次必读 [AGENTS.md](../../../AGENTS.md)、[团队流程](../agent-team/README.md)、[设计技能](../../../.agents/skills/agenteam-design/SKILL.md)、[Go 开发技能](../../../.agents/skills/agenteam-go-development/SKILL.md)和 [D05 设计](d05-object-storage-design.md)；独立验收者另读[验证技能](../../../.agents/skills/agenteam-verification/SKILL.md)。运行依赖以[已提交恢复记录](../agent-team/dependency-recovery-2026-10-05.md)为准，不更改 Go、PG、MinIO 版本、SHA 或 go.mod/go.sum。

| 依赖 / 后继 | 本块消费与限制 |
| --- | --- |
| C0、00014、既有 Object/SourceReads/Downloads/Transfer/ProcessGuard | 消费已提交实现；00016 只扩 Object work 的 verification/cleanup 表达，不重建 schema、不改旧 SQL |
| 正式 `oc.ProjectStopAuthority` | 使用下文唯一注入口。已有 typed 口不代表生产实现已存在；没有真实绑定时明确 unbound |
| D08 B03-R2 | [R2 卡](recovery-d08-b03-acceptance.md)仅接受操作，不提供 stopping 推进或 ProjectStopAuthority；本块不依赖活动 Project 源码，不改其文件 |
| 后继 D08 当前 cause/phase Authority 与推进 | 负责正式 Authority、ProjectLifecycle actor、参与者装配及端到端集成；未完成前不解锁生产归档/删除 |
| 后继 Artifact 停止 | 负责自身 command/work、`source_project_id` 双向关系及调用 Object；不在本块伪造 Artifact stopped |
| 后继清理与 Audit checker | delete 清理、旧 archive receipt/work 退休、最小 delete receipt、§9.2 Object/Secret Audit fact checker 分别后续交付；本块不放宽现有 Audit |

实现可否与 R2 并行由主线程根据固定依赖另行下发。R2 当前独占 Project 源码及首轮真实 fixture；本块不得抢占 Docker、PG、MinIO、端口或测试库。真实验收必须在明确资源交接后串行执行。

## 子阶段与最小 schema

先独立交付 S1 更合理：现有 00014 无法表达新 verifier 与可被覆写的 cleanup claim；先用真实 PG 证明持久身份、旧行兼容和迁移原子性，再由 S2 消费固定 schema，可避免运行实现依赖未验的终局证据。S1 是完整数据库变更结果，不是 Object 停止能力完成；S2 仍必须交付本卡全部真实运行行为，不再拆成无行为 helper 卡。

| 子阶段 | 完整交付与依赖 | 唯一待下发写入范围 / 所有权 |
| --- | --- | --- |
| S1 维护 work schema | 只消费固定 00001–00015；验收下述约束、fresh/populated 升级及同字节失败重试 | 单一 schema 实现者仅新增 `db/migrations/00016_object_project_work_maintenance.sql`、`tests/objects/project_stop_work_migration_test.go`；00016 全局编号只保留给本卡，不改 embed、driver、旧 SQL 或旧测试 |
| S2 Object 停止运行闭环 | 消费 S1 已独立验收提交/固定指纹及已有真实事实端口；实现登记、Request/Inspect、join/恢复和兼容验收 | 单一运行实现者拥有下文 15 个旧文件、3 个新源码/同名单测及 7 个新集成文件；S1 两文件冻结，不与 schema 作者共写 |

S1/S2 的具体执行者、卡片写者和资源交接由主线程下发，相关范围同时只有一个写者；审查/运行需先固定相应输入。R2 可继续其已授权工作，S1/S2 真实 fixture 均在 R2 明确交还后安排，不能凭迁移文件独立就并行使用 Docker。

00016 是仅 Up 的 transactional 追加迁移，严格限定在 `agenteam_object.project_work`：

- 将现有 kind CHECK 的闭集扩为原六种加 `verification`、`cleanup`；不将维护工作伪装成 preparation/reader，不扩公共 enum，不改 Artifact kind。
- 新增可空 `cleanup_claim_fence bigint`，无默认值、无猜测性 backfill。新增 `project_work_cleanup_claim_check`：cleanup 必须显式非 NULL 且大于 0；所有其它 kind（包括 verification）必须为 NULL。CHECK 必须用显式 `IS NOT NULL` 防止 SQL 的 NULL 真值绕过。
- 新增 `project_work_maintenance_object_check`：verification/cleanup 的 `object_id` 必须非 NULL；旧六种 kind 原有可空规则保留。原 safe_id、PK、admission tuple、project/revocation FK、索引及 stop state/cursor 约束保持。
- 新增部分唯一索引 `object_project_work_cleanup_claim`：`(resource_id, cleanup_claim_fence) WHERE kind='cleanup'`。同一 cleanup operation/fence 只能有一个 work/worker；work 的原 PK 同时保证同一 worker UUID 不能另指一个 cleanup operation、fence 或 process。不要以 upsert 修改身份绕过冲突。

| kind | `id` 与 `resource_id` 的精确含义 | `process_id` / fence / 身份不变性 |
| --- | --- | --- |
| verification | id 为本次独立 verifier work UUID；resource_id 为被验证的原 private_candidate attempt ID，object_id 为其实际 ObjectID。一次新的实际 verify 是新的 work，不能复用旧上传 work | process_id 为本次 Service/spool process，不能复制旧 attempt.process_id；cleanup_claim_fence 必须 NULL。同一登记 Unknown 沿原 work 确认，不能新建身份逃避原 writer |
| cleanup | **id 精确等于本次原 cleanup worker UUID**（显式 Parse 同一 UUID，不另发 work UUID）；resource_id 是原 cleanup_operations.id，绝非 attempt ID、Project operation_id 或 worker ID；object_id 来自该 cleanup 的实际 Object | process_id 为本次执行 claim/zero/remove 的 Service/spool process；cleanup_claim_fence 等于该次 claim 的持久 fence。登记后这些字段和 kind/project/admission 身份不覆写；只允许按原身份推进 revoked/joined |

worker 一次对应一个 claim 的依据是既有 `cleanup.go` 的真实控制流：每次 `claimCleanup(object, attempt)` 调用先生成一个 worker UUID，只调用一次 `withinAccess`，在单个 attempt 的 cleanup 行上将 fence 加一并写该 worker；随后只把这一 claim 交给一次 zero/remove。重试调用会生成新的 worker。不能只凭 UUID 惯例认定一一对应；S2 须保持此控制流，并测试同身份重放完整核等，以及相同 worker 不同 resource/fence/process 被拒绝。新增 claim 插入新 work，旧 work 永不因 `cleanup_operations.worker_id/fence` 被覆写而覆盖或删除。身份冲突须失败，不接受 `ON CONFLICT DO UPDATE` 换 process/资源。只读 schema 的 PK/唯一性不替代运行时完整身份核验。

原 native cleanup 行被下一 claim 覆写后，每条旧 work 仍保有 exact worker/fence/process/object/resource；恢复分别取得原 Object/Command 锁以确认旧 claim/回写 writer 终局，再依据该条 work 的真实本地 join 或其 exact process 死亡判断。新 claim 完成、原 uploader.io_closed、当前 native worker 不再相等都不证明旧 worker 已结束。不增加对可退休 native 行的级联 FK，不清掉未决旧 work；没有旧 work 的历史 applying claim 保守 unknown，不能从 uploader 猜回填执行 process/fence。

S1 验收在新文件提供以下顶层测试；复用已有 owned fixture、迁移 Source/helper，测试只证明 schema，不造 Object 停止成功：

- `TestObjectProjectStopWorkMigrationFreshAndPopulatedFifteen`：空库顺序应用至 16；以及已有 15 的 populated 库升级，含原六种 work、joined/未 joined、stop receipt、Artifact source 关系和有效 native cleanup 行。逐字段比较旧事实；新列仅 NULL，不改 worker/时间/状态，不碰其它表、既有约束与索引。
- `TestObjectProjectStopWorkMigrationMaintenanceConstraints`：两种新 kind 正例；cleanup NULL/0/负 fence、其它 kind 非 NULL fence、维护 kind 空 object、未知 kind、同 worker 冲突、同 cleanup/fence 不同 worker 拒绝；不同新 fence/worker 可与旧未 joined 行共存，旧行完整保留。新字段不是权限或真实死亡证明。
- `TestObjectProjectStopWorkMigrationPreservesPriorConstraints`：原 safe_id、admission 二元组、stop state/cursor、跨 Project revoked_by FK、Artifact kind/source 与隔离约束的正反例保持。Object kind 的唯一预期变化仅为增加两个值。
- `TestObjectProjectStopWorkMigrationAtomicFailureAndRetry`：测试私有 Source 在 00016 DDL 后注入可修复依赖失败，确认新增列/索引/约束全部回滚、旧 kind CHECK 和旧数据完整，journal 不记 applied；修复测试依赖后以相同源码字节/checksum 重试只应用一次。实际 production SQL 的正常升级另外覆盖，不替换失败 Source 指纹冒充重试。

沿[迁移与修复规则](../backend/database.md#迁移与修复)不写 Down。未提交 DDL 失败由原事务回滚并按真实 journal 处理；COMMIT 不确定先确认真实 journal/版本，不删除 journal 重来。已经提交 00016 后采用向前修复，不承诺能缩回旧 kind 或删 fence 而保留未决 work。部署回退必须使用兼容 16 且理解新增 work 的构建，并先验证迁移 guard/准入/恢复；未验旧 binary 不作为回退方案。完整停服备份恢复是另外授权操作，本卡不提供破坏性降级脚本。

## 构造与正式端口

保留现有构造签名，只在 `object.Authorizations` 增加一个正式字段：

```go
type Authorizations struct {
	// 既有 Planner、Resources、Read、Gate、Cleanup、Leases、Processes 保留。
	ProjectStop oc.ProjectStopAuthority
}

func New(Store, *Backend, *Spool, ac.Appender, Authorizations) (*Service, error)

func (*Service) RequestProjectStop(context.Context, identity.Actor,
	oc.ProjectStopCause) (oc.ObjectStopReport, error)
func (*Service) InspectProjectStop(context.Context, identity.Actor,
	oc.ProjectStopCause) (oc.ObjectStopReport, error)

var _ oc.ObjectProjectStop = (*Service)(nil)
```

`NewSourceReads`、`NewDownloads`、`NewTransferService`、`NewRuntime` 与 `Store` 不扩签名。尤其不能给 Object Store 补不存在的 `RequireHeldLocks` 或增加公共 witness。新增字段构造时固定，不建可变全局授权注册库。ProjectStop 为 nil 保留既有普通能力和构造兼容；两个 stop 方法返回 `DEPENDENCY_UNBOUND`，不能据此不登记普通项目工作。已有 Processes 为 nil 也不新增普通能力前置，不妨碍本地真实 join；它只意味着不能推断外部 process 死亡。已有普通调用仍须消费当前 Resource/Read/ProjectGate/RunnerTransfer 等真实端口。

`ProjectStopAuthority.DiscoverProjectStop` 只给依赖计划；每个短 Tx 将其与本域映射一次完整 union，`ValidateProjectStopInTx` 必须返回同 Tx、完整 Request/依赖匹配的 authorization。Object 自己检查私有计划 issuer、资源关系与完整锁计划。正式 Project adapter 负责当前 Project/operation/冻结 manifest/接受版本与自己的 held-lock 检查；Object 不读写 Project 表，不从 Service actor 字符串、公开 Dependencies 构造器或空表产生许可。

测试可在新测试文件实现严格 SQL 事实型 Authority：有明确测试拥有的 cause/phase/manifest/版本前置，在同一真实 Tx 核完整请求与实际持锁；它仅隔离测试正式端口，不是生产 adapter，也不能是恒定 allow。测试前置不得冒充 Project 已运行 stopping、完成 archive 或完成 delete 的证据。缺正式生产绑定是明确的集成未完成项，不以本块通过抹去。

## 精确文件范围

S2 待主线程下发后，唯一实现者可改以下 **15 个既有生产文件**，均位于 `internal/central/object/`。每项仅用于本表接缝，不是目录授权；S1 不改这些文件。

| # | 旧文件 | 本块接缝 |
| --- | --- | --- |
| 1 | `service.go` | Authorizations 注入；operation 多关系 registry；PreparePayload 在 spool 前登记、prepared/Discard 的真实 join；维护 work 的 enclosing-operation finish 与原 writer 确认；保持全服务生命周期计数 |
| 2 | `access.go` | 已有 PrepareReadAccess 的 SH/Read 分派；私有完整计划与准入代次；ClaimCleanupAccess 同 Tx 新 claim 登记 hook 及 CheckpointCleanupAccess 技术回写跟踪，不改变既有公开 Access 规则 |
| 3 | `upload.go` | Reserve/UploadPrepared/Publish 的准入及撤销检查；真实 writer 与 work 对应；旧 attempt 在 Restore 后仍不可 publish |
| 4 | `references.go` | receipt consume/attach 与原 work/epoch 的一致性；旧撤销 upload/receipt 不复活，已发布 canonical reference 保留 |
| 5 | `read.go` | 正常 reader 项目关系，GET 前登记，实际 body/lease 结束后才进入 join checkpoint |
| 6 | `source_reads.go` | source 实际项目登记；AcquireSourceInTx 的原 Tx 与未 open handle；取消、打开和 release 的终局对应 |
| 7 | `download_service.go` | IssueDownload 在 ResolveDownload 前持久登记；OpenDownload 的原 grant/attempt 与项目关系 |
| 8 | `download_stream.go` | StreamHTTP/Close、reader、完成回写与清理 callback 全部结束才 join，不提前 finish |
| 9 | `transfer.go` | 本地 Issue 工作与持久远端 grant/lease 分离；保存真实 transfer 关联 |
| 10 | `transfer_access.go` | 原 transfer/owner/current gate 与 stop epoch 的完整计划和校验 |
| 11 | `transfer_upload.go` | staging/candidate 真实 PUT writer 登记及撤销保护 |
| 12 | `transfer_complete.go` | source GET/spool/candidate/publish 各实际关系和生命周期，不以中央调用返回代替远端退休 |
| 13 | `transfer_recovery.go` | 复用可信 Retirement/终局事实的窄恢复；不能将 cleanup 专用 gate 整体借作 stop 权限 |
| 14 | `recovery.go` | recoverAttempt 的独立 verifier 登记/实际 join；逐项 child handle；所有 work 的原 writer、真实 join/精确死亡确认与可重试 checkpoint |
| 15 | `runtime.go` | startup/recover 纳入只剩 work/stopping 行的情形，不能误入 recoverEmpty；保持共享 ProcessGuard 的释放顺序 |

允许新增的源码仅为：

- `internal/central/object/project_lifecycle.go`：两个公开方法、短事务与报告投影。
- `internal/central/object/project_work.go`：私有多关系 registry、持久准入、join 与恢复身份。
- `internal/central/object/project_stop_store.go`：本域扫描、native 映射、批次计划和 checkpoint。
- 上述三个文件各自同名 `_test.go`，用于私有绑定、计划、诊断限额和生命周期规则的有意义单元测试。

S2 允许新增的集成测试文件仅为 `tests/objects/project_stop_fixture_test.go`、`project_stop_authority_test.go`、`project_stop_io_test.go`、`project_stop_unknown_test.go`、`project_stop_recovery_test.go`、`project_stop_transfer_test.go`、`project_stop_compatibility_test.go`。S1 的 migration 测试文件不属 S2 写权。只读复用已有 fixture/helper，不改旧测试文件或 driver；新增文件不能重新实现宽松基础设施或授权框架。

`cleanup.go`、`download_cleanup.go`、`download_attempt.go`、`process.go`、`startup.go`、`reader.go`、`spool.go`、`storage.go` 只读复用。公共 contract、既有 SQL、Project/Artifact/Secret/Audit/Outbox 实现、app/HTTP、脚本、模块锁、已提交依赖恢复五文件均冻结；唯一新 SQL 为 S1 的 00016。若实际需要表外文件或接口调整，先说明具体接缝和现有范围为何不足，交主线程重新授权。

## 工作登记与正常入口

1. `project_work` 记录的是技术工作，不是业务权限。行中 Project 来自已验证 owner/source/transfer 映射，process 精确取已有 Service/spool process；不能取 actor 的目标项目、旧 uploader process 或仅信任 business_ref 字段。已绑定 Runtime/ProcessGuard 时必须核 process 一致并由 guard 覆盖完整生命周期；未绑定 Processes/Guard 的旧合法装配仍能普通调用和本地 join，绝不能据此宣称跨进程死亡。Avatar/System 无 Project 的工作不强造项目行，继续既有权限流程。
2. 每个实际工作关系有稳定 work ID、kind、resource ID、可空 object ID 和原 writer 身份。PreparePayload 在任何源 body/spool 消费前完成登记；尚无 ObjectID 时保留空值，不伪造对象。`admission_version/admission_operation` 记录在正确 Project 锁下看到的最近本域 stop 代次（无历史为 0/null），Restore 不将它清零；它只识别旧工作，当前权限仍由正式 Gate 决定。
3. 自有事务的前置登记必须 confirmed committed 才可 spool、外部 resolver、GET/PUT 或返回可开启 I/O 的能力。私有 work identity/必要序列锁必须 Tx 外预分配并纳入一次计划。已有 InTx 入口随调用者原事务登记，沿原 Object/Command 等完整锁计划串行确认原 writer；不得在调用者 AcquireAll 后新增锁。callback 成功、`Tx.Valid()` 或读不到行都不等于 committed。
4. 同一嵌套 `operation` 可同时包含目标 Project 的 preparation/write、另一 Project 的 source，以及后续 download/transfer 关系。使用不可覆盖的关系集合，不能增加一个可覆盖的 `operation.ProjectID` 标签。每个关系各有持久行与实际 release/join；一方撤销不能覆盖或提前遗忘另一方。全服务 operation/cleanup 计数在所有实际工作结束前保持。
5. IssueDownload 已有 begin 但 ResolveDownload 前尚无对象：在调用 provider 前，用现有 BusinessFileRef 对应的正式 Owner 映射，消费 `PrepareReadAccess` 的 Planner、当前 ResourceAuthority Read 与 ProjectGate Read，在短 Tx 登记并确认。ArtifactFile 对应实际 Artifact owner；UploadedObject 沿 receipt 的真实 owner；其它 owner 的正式 provider 未绑定则 unbound。不能仅凭 ProjectID 提前 allow；解析后仍重验 exact source/version/object 与 provider 权限。
6. SourceReads 以 `ResolvedSource.Owner` 与 `Meta.Scope` 的一致关系确定源项目，并消费原 source plan/resolver 校验。AcquireSourceInTx 即登记 source work 和未 open handle，不能等 OpenLeasedSource 才登记。delete 后未 open handle 不能开启 GET；取消未 open lease 的 cleanup writer 也必须真实完成或保留 pending。原 Acquire Tx Unknown 时沿原锁确认，不能猜测回滚后删除 handle。
7. `agenteam_artifact.project_work.source_project_id/source_joined_at` 的真实业务关系由后继 Artifact 块持久化和双向扫描；本块不碰该表。Object 已实际承担的源关系必须现在以本域 `kind=source, project_id=实际源项目` 持久化，不能因 Artifact 后继而漏记，更不能把目标项目冒作源项目。
8. read/download work 的 join 包含实际 body 的 Read/Close、lease release、download finish/Audit/DB 回写及 cleanup callback；发出 cancel、关闭包装器、HTTP 返回或 lease 状态变化单独都不够。继续复用既有 cleanupContext 和共享生命周期计数，禁止脱离 ProcessGuard 的后台 checkpoint。

## 维护 I/O 的登记与 join

`recoverAttempt` 当前在旧 private_candidate.io_closed 后直接 `backend.verify`。S2 在该入口为本次 verifier 建独立 `kind=verification` work：先沿原 attempt/object 的正式 RecoverAttemptAccess 与本域完整计划重读身份、核准入，再在短 Tx 登记当前 process/work，confirmed commit 后才调用真实 verify。原 uploader 已死或 io_closed 只证明原上传，不能证明本次 verifier。登记 Unknown 时零 verify；继续/确认用原 work，不能换 ID。真实 verify 的 body/close、后续 attempt/cleanup gate/Audit/DB 回写与调用者尾部工作结束且原 writer 串行终局后，才按该 work 写 joined。验证失败或 Audit 失败与 I/O 是否真实 join 分开记录；失败不伪装业务成功，也不凭完成 callback 猜测 COMMIT。既有 UploadPrepared 内的 verify 随同一次 writer 的完整生命周期计数，不可因抽出恢复登记而漏掉。

`cleanup.go` 仍只读，通过两个现有接缝完整跟踪：

1. `access.go.withinAccess` 对 ClaimCleanupAccess 在已完成一次完整 AcquireAll/Validate 后，记录该 attempt 原 cleanup 的 claim 身份；调用原 callback；仅当 callback 成功且同 Tx 重读确认**本次实际生成** applying worker/fence，才按 00016 规则插入 work。旧 applying 行未改变或 callback 因 lease/reference 拒绝 claim 时不得误登记。保留原 claim/attempt/object 与新 worker/fence 的精确关系，登记失败与 claim 一起回滚；不在锁后补锁，不启动嵌套事务。
2. 本地 relation 在提交结果未知时仍保留原身份。已有 `claimCleanup` 的 commitError 位于 zero/remove 之前：只有 claim 与 work 同 Tx confirmed committed 才可执行 I/O；Unknown/NotCommitted 均零 zero/remove。原 callback 返回 nil 不足以启动 I/O。重新 claim 是新 worker/fence/work，不能覆盖旧 work 或借旧已提交登记复用另一个 process。
3. 正常/失败回写均经原 CheckpointCleanupAccess，按 exact cleanup/worker/fence 跟踪本次 writer；回写 phase=completed 或 noteCleanupFailure 返回都只提供技术进度。`cleanupIOContext` 可能脱离 caller cancel，因此 cancel/超时不标 joined。最终 `service.go` enclosing operation finish 必须晚于 cleanObject、失败回写、finalize/Audit 及调用者尾部检查实际返回，再用独立有界事务按原身份和原锁证明 writer 终局、写 joined；若仍 Unknown 保留内存/持久事实供恢复。
4. recoverProgress 可跨 Project，不能为一个 work 取消其父 operation。`recovery.go` 给逐项 verifier/cleanObject 建精确 child handle；其它已有调用路径用对应 relation 观察或等待其实际 enclosing-operation finish。cleanup 取消不合作时保持 pending；不能扩大取消范围或提前释放 guard。nil Processes 时可依据这个 Service 真实拥有的 join，不能根据缺本地 handle 断言历史 process 已结束。

verification/cleanup 均是维护写链的工作，archive 与 delete 都必须纳入真实 join 判断，不能按 archive 保留的普通只读 reader 忽略。正常维护权限仍消费既有事实端口；新增 work 不授予零标记、删除、Audit 或新 publish 权限。stop gate 必须协调这些入口，使终局判定后旧代次不能自动再开维护 I/O；已开始的真实 claim 先 join，未开始的补偿义务与后继正式 Cleaner 的权限分别保留。停止本身不调用 Project Cleanup，也不物理清理已发布 canonical 内容。旧 revoked attempt 即使验证成功仍不得重新获得 publish/attach 资格。

## Request、Inspect 与真实终局

授权矩阵、typed request/report 与错误闭集沿 D08 §9.1。实现须满足以下接缝约束：

- 两方法固定 Object component 和各自 step。Request 仅在 `ContinueProjectStop` 下幂等创建同 cause gate；Inspect 不创建新 cause。`ReadProjectStop` 只读已有匹配技术事实，不取消、不推进游标、不补写回执；缺本域匹配 stopped receipt 不能以空表成功。Restore、新 operation 或错误版本拒绝且零取消。
- 取消前先完成 **只读** preflight：一次完整锁 union、当前 Authority 验证、本域重读及捕获确切 handle/process/attempt/fence，且该 Tx 确认 committed。创建/推进 gate 和持久撤销使用另一次独立重规划并授权的短 Tx；其 Unknown 也不启动新取消。随后锁外仅取消仍与捕获身份相等的原 handle，不按 Project 宽扫当下 map，不调用全局 StopAdmission/Drain/Force。
- 等待始终在 Tx 外且有预算。每次 checkpoint 重规划一次完整 union、重验当前 cause 和映射、确认原 writer Tx 已终局，然后要求本地真实 join，或确切旧 process 已死亡且无仍活着的原 writer。55P03、超时、失联、cancel 返回、读失败或任一提交 Unknown 均保留可恢复身份并报告 pending/unknown，不能写 joined/stopped。
- stop 完成更新、游标、撤销和 join 的 Unknown 都可从原身份串行确认，不产生新 cause/work 以逃避 Unknown。合法技术性 release/已获准 join 的恢复不创造业务权限；不能借恢复重新授权旧 cause 或触发新外部 I/O。
- 当前 `ProcessAuthority` 缺失或不能证明 exact 外部 process 死亡时，不猜测跨进程终局；当前 Service 的真实本地 join 仍可正常确认。绑定时复用 ProcessGuard 的部署/host/spool/process 与实际 flock 证明，不以 TTL、连接消失、中央进程退出推断远端 Runner lease 结束。guard 必须覆盖真实 body、verification、cleanup、回写以及最后 callback；`runtime.go` 不能将只有 project_work/project_stops 的库当作空实例绕过恢复。

| 动作 | 必须收束 | 必须保留 |
| --- | --- | --- |
| archive | 本 Project 未完成 preparation、upload/attempt、待 publish/consume/attach 资格与 transfer PUT 写链，以及独立 verification/cleanup 的实际 claim、I/O 和回写；持久保留撤销，使 Restore 只允许新 command/key | 已发布 canonical payload/reference/history；当前权限允许的普通 reader、source、download 和 transfer GET。不得以删除已发布内容证明停止 |
| delete | archive 的写链，加本 Project 实际 reader/source/download/transfer I/O、未 open lease/handle及其回写；外部 grant/lease按真实退休证据收敛 | 本轮只形成 stopped 技术事实；对象物理清理、下载事实清除和最终 receipt 退休由后继 Cleaner 完成 |

Target write 与 source read 分别按各自真实项目与类别处理；停止目标写入所需的实际源读可随该写 operation 取消，但独立合法读不能被 archive 全局关闭。transfer 本地 Issue/Complete operation 结束不等于远端授权终局。原 RunnerTransferAuthority Retirement、远端 lease/签名生存期及实际请求终结证据必须按既有协议消费；缺端口或证据保持 unbound/pending。不能调用 cleanup 专用 `gateProjectTransfers` 来跳过 stop 授权或现有 Audit。

## 有界扫描与旧事实

只扫描新 registry 不够。固定使用 00014 已有 `scan_kind=0..4`，各 lane 按稳定主 ID keyset 前进：

| scan_kind | 扫描的既有事实与身份 |
| --- | --- |
| 0 | 本 Project `project_work`，以 work ID；包括只有登记、尚无 ObjectID 的工作，以及每次独立 verifier、全部新旧 cleanup claim |
| 1 | 本域 objects 及对应 uploads/current、未决 attempts/reserved references、native cleanup_operations 与各 claim work，以 ObjectID；覆盖改造前没有 work 的写链和被新 worker 覆写的旧 claim |
| 2 | `object_leases` 及实际 object/owner/source/执行租约映射，以 LeaseID；不能漏未 open source 或异进程保护 |
| 3 | 下载 grants 与全部相关 attempts，以 GrantID；started attempt 不能因 reader lease 消失而自动结束 |
| 4 | `object_transfers` 与 grant/lease/retirement 关系，以 TransferID；包括本地调用已返回的远端使用 |

每轮最多处理 100 个主记录，关联子记录也有明确预算；单项 fanout 超预算或映射不完整保持 pending，不用截断后的空诊断证明完整。游标仅安排扫描，不是终局证据；遇到低 ID pending 仍让独立后项前进，完成一轮后重访未决项。`scan_kind=0, scan_after=NULL` 的初始值也不是扫完标记。

只有全部相关 native/work 类别已经检查、没有未确认 writer/Unknown/活跃关系、当前 stop gate 与 Project 权限继续阻止本动作的新准入，才可在独立重验事务原子写 stopped 并按 schema 归零游标。最终完整性检查必须消费全量事实的存在性/终局判定，包含所有未 joined verification 和 cleanup claim，不以 native 当前 worker/fence 或 completed phase 覆盖旧 claim；不能消费有界 refs 列表。映射改变则回滚重规划。archive 的合法普通只读工作不进入其停止待办，delete 则必须纳入；verification 不是这种普通只读工作。

没有 work 行的旧 native 数据保持原权限与可见性。已完成且可证明无 I/O 的旧对象无需补造活跃 work；仍 started 的旧 download、来源不明 lease、无本次执行 process 证据的旧 cleanup claim，或不能证明原 writer/进程终局的 transfer 必须保守 pending/unknown。重启、缺新登记、固定等待时间、没有当前本地 handle 都不是死亡证据。00016 不猜测 backfill 这些身份；本块不批量删除旧数据、不伪造完成标记、不迁移已发布对象。

## 验收集合与兼容门槛

以下为待实施的精确集成测试入口，必须驱动真实 Object 服务、owned PG/MinIO 和实际 I/O/事务阻塞；测试名字与其断言一并冻结后交独立验收，不以只构造 report 代替行为。

| 新测试文件（均在 `tests/objects/`） | 顶层测试名及必须观察的结果 |
| --- | --- |
| `project_stop_fixture_test.go` | 仅本卡严格 Authority/装配/同步 helper；进程死亡场景使用真实 ProcessGuard，普通兼容场景保留旧无 Processes/Guard 装配；使用既有 nonce/label/exact-ID fixture，不新增恒 allow |
| `project_stop_authority_test.go` | `TestObjectProjectStopAuthorityAndReadOnlyInspect`：全请求绑定、错 cause/phase/manifest/版本拒绝、Inspect 不能创 gate、只读零写；`TestObjectProjectStopRestoreKeepsRevocation`：新代次可正常工作，旧 prepared/receipt/attempt 仍拒绝 |
| `project_stop_io_test.go` | `TestObjectProjectStopArchivePreservesPublishedReads`：真实上传/下载 SHA 与 reference 保留；`TestObjectProjectStopDeleteWaitsForActualIO`：阻塞 body/回写在实际 join 前 pending；`TestObjectProjectStopRegistersBeforeResolver`：resolver 已进入但未返回仍可观测/取消/等待；`TestObjectProjectStopUnopenedSourceAndCrossProjectRelations`：未 open lease 禁止 GET、源/目标各自持久且不覆盖；`TestObjectProjectStopVerificationOwnsItsLifetime`：原上传已 io_closed 后阻塞真实 verify，单独 work/process 可观测，archive 仍等待其 verify/DB/Audit 尾部；`TestObjectProjectStopCleanupWaitsForActualIOAndCallbacks`：分别阻塞真实 zero/remove 和回写，脱离 caller cancel 仍 pending，原 uploader 已死不替代当前 cleanup worker join |
| `project_stop_unknown_test.go` | `TestObjectProjectStopUnknownAdmissionNeverStartsIO`：登记 COMMIT Unknown 零 resolver/GET/PUT/spool 消费；`TestObjectProjectStopUnknownPreflightNeverCancels`；`TestObjectProjectStopUnknownCheckpointWaitsForOriginalWriter`：gate/join/stopped 写的晚 COMMIT、ROLLBACK、仍持锁分别确认，身份不变；`TestObjectProjectStopUnknownMaintenanceNeverStartsIO`：独立 verification 登记和 cleanup claim+work 提交分别 Unknown，零 verify/zero/remove，确认晚 COMMIT/ROLLBACK 时保持原 worker/work/fence/process |
| `project_stop_recovery_test.go` | `TestObjectProjectStopRecoveryRequiresExactProcessAndJoin`：活进程/真实 kill+flock/cleanup 回写对照，原 writer 未终局仍 pending；`TestObjectProjectStopNativeFactsAndFairBatches`：无 work 的旧事实、多页/低 ID 阻塞不饥饿、另一项目隔离；`TestObjectProjectStopRuntimeRecoversOnlyTechnicalRows`：仅 work/stopping 行不能按空库启动；`TestObjectProjectStopCleanupClaimOverwriteKeepsOldWorker`：真实新 claim 覆写 native worker/fence 后，旧未 join work 原身份仍在，只有其自身原 writer+join/精确死亡可收敛；同 worker 换 resource/fence/process 拒绝；`TestObjectProjectStopRecoveryDoesNotCancelOtherProject`：逐项维护工作停止不误取消跨项目父轮次 |
| `project_stop_transfer_test.go` | `TestObjectProjectStopTransferRequiresRemoteRetirement`：中央返回/退出、URL 到期、cancel 均不能替代协议终局；`TestObjectProjectStopTransferLatePUTCannotPublish`：真实延迟 PUT/忽略取消、当前 Authority 与 Retirement、Restore 后旧资格不复活 |
| `project_stop_compatibility_test.go` | `TestObjectProjectStopOptionalBindingKeepsOrdinaryObjects`：直接复用旧 common fixture 的 OpenSpool+Initialize、nil ProjectStop/Processes、无 ProcessGuard，普通构造/上传/读取/重放和本地 join 仍工作；缺外部 process 证据必须 pending/unbound；`TestObjectProjectStopArchiveAndDeleteNormalObjectCompatibility`：已存在对象、source/download/transfer 的正常路径、未知业务 owner 拒绝、非项目 Avatar/System 不误登记或误停 |

兼容门槛不只跑新测试。至少保留已有 `TestObjectExistingUploadReadReplayAndCurrentPermission`、`TestObjectProspectiveReceiptConsumeAndCancel`、全部 `TestObjectSourceLease*`、`TestObjectStreamingRangeMissingAndIntegrity`、`TestObjectGracefulDrainKeepsAdmittedReaderAlive`、`TestObjectForce*`、`TestObjectRuntime*`、`TestObjectProcessGuardExactFlockKillAndHistoricalBoot`、`TestTransfer*` 及受影响 download 正常/Unknown/Close 测试。独立验收者按 15 个旧接缝冻结实际顶层集合，逐组穷尽并记录；其它包 no-tests 不算通过。既有上传、当前授权、receipt 重放、archive 合法读和全服务 shutdown 的语义不得因项目 stop 改写。

后续实际命令从仓库根执行，沿原每包 6m/race 预算按顶层测试分组，不放宽超时掩盖死锁。S1 在交接 fixture 后先独立执行迁移集合，不等待 S2；S2 的最终冻结集合排除输入未变且已验的 S1 迁移测试，复用其固定证据：

```sh
AGENTEAM_GO=/workspace/toolchains/go1.27.1/bin/go AGENTEAM_MINIO_BINARY=/workspace/agenteam-dependency-recovery-5drj88a2/bin/minio GOMODCACHE=/workspace/agenteam-dependency-cache/modcache sh scripts/test-objects.sh -run '^TestObjectProjectStopWorkMigration'
GOTOOLCHAIN=local /workspace/toolchains/go1.27.1/bin/go test -count=1 ./internal/central/object/...
GOTOOLCHAIN=local /workspace/toolchains/go1.27.1/bin/go test -race -count=1 ./internal/central/object/...
GOTOOLCHAIN=local /workspace/toolchains/go1.27.1/bin/go vet ./internal/central/object/...
AGENTEAM_GO=/workspace/toolchains/go1.27.1/bin/go AGENTEAM_MINIO_BINARY=/workspace/agenteam-dependency-recovery-5drj88a2/bin/minio GOMODCACHE=/workspace/agenteam-dependency-cache/modcache sh scripts/test-objects.sh -run '^TestObjectProjectStop(Authority|Restore|Archive|Delete|Registers|Unopened|Verification|Cleanup|Unknown|Recovery|Native|Runtime|Transfer|Optional)'
git diff --check
```

依赖缓存路径是复用入口，不是当前运行可用性证明；执行者按恢复记录核工具链、binary SHA 和镜像 digest，不读取旧凭据或连接既有库。高风险权限、事务、恢复场景必须由未参与实现者独立验证，固定源码/依赖/测试集合及原始失败、修复后日志，最后确认后台命令与本轮 owned 资源清零。

## 升级条件与当前交付状态

实现前须由主线程确认：正式 ProjectStopAuthority 集成的责任后继、库级严格事实测试的验收边界、15 个旧文件唯一写者，以及真实 fixture 的交接次序。若已提交接口不足以表达某个实际 Owner/原 writer/远端终局，不造私有 allow、不扩公共协议；暂停该接缝并给出具体事实和最小范围调整。局部私有数据结构与 SQL 写法由实施者在本卡约束内选择。

rev1 独立静审针对 SHA-256 `c09e0363e2a4c6665e04d3ec181f28e1128e6766d26a31d6727d157883918b8c`，结论为需修三点：独立 verifier 缺登记，cleanup claim 身份不能在 00014 持久保存，及 nil ProcessAuthority 普通兼容需消歧。rev2 按主线程裁决补充上述 S1/S2、独立维护 work 与实际 join，并保留旧装配兼容。

2026-10-05，未参与本卡编写的 `d08_recovery_design` 对冻结 rev2 SHA-256 `4a57e26d23696964d9a295a21fd125b12d4a412b942945859d6cbe886a642532` 完成 delta 静审，三点闭合，无新阻断。14 个链接/fragment、结构与 4 个 S1、21 个 S2 顶层测试入口检查通过。结论文件为 `/tmp/agenteam-d05-stop-spec-review-_ujt176s/review-rev2.md`，SHA-256 `5bb2ba41ff99b002a5be924be647c02a8409123b41dc52a611e35be463a86023`；此处持久记录其结论，不依赖临时文件存续。S1 可先独立实施和验收，PK/部分唯一索引仅证明插入约束，不能单独证明 UPDATE 身份不变或真实 join；完整核验仍归 S2。主线程采纳时只更新页首与本记录，行为正文保持。未执行产品测试。

本卡作者只做固定输入阅读与 Markdown 自查；没有实现上述能力，没有运行 Go/PG/MinIO/Docker，也未修改冻结文件。设计通过、S1 schema 验收、S2 Object 库停止验收、Artifact 集成、Project 正式 Authority/推进和最终 delete 清理分别记录，不将任一单块宣告为 D05/D08 整体完成。

## S1 独立验收与运行实施交接

S1迁移与测试两源已获独立验收并由主线程采纳、提交推送`49c6589`。[正式报告](../agent-team/d05-s1-verification.md)记录作者4新+1旧真实race通过（7.247s），独立4卡定+2探针、共6顶层16子例真实race通过（9.126s）；两源及39项固定输入在R3/S2开写前末检匹配，4容器、3网络及所属进程清零。00016只证明schema约束、升级和原子性，不证明S2停止、work登记或实际join。

主线程已将S2卡内28源交`d08_registry_backend`正式实施，无Docker权；已静审提交`ed7985a`的[R3 Authority卡](recovery-d08-lifecycle-authority.md)由`restore_test_dependencies`实施卡内16源并独占Docker。两者文件隔离，各自固定快照验收，不消费对方活动稿。真实运行、适配器组合及完整D05/D08仍须后续验收。

## S2 独立验收与后继交接

S2 28源已独立验收并由主线程采纳、提交推送`6658a6cb1f29299521773bc8dc86b2f607b8c809`，远端一致。[正式报告](../agent-team/d05-s2-verification.md)记录作者95个不同顶层（21新+74旧）：review03直接通过61项，review02未变证据复用34项；最终unit/race/vet/compile及两cmd构建通过。独立25顶层+43子例共68个命名结果全PASS、无skip（94.897s），包括SR1 writer尾部、SR2 SourceForce、review03 Runtime两旧红及活work gate Unknown三态零提前Close与原writer锁终局，原红和修复链均保留。

独立4容器3网络exact-ID absent，既有2容器4网络ID/name/labels不变、runtime空、所属进程0，28源末检匹配。本次通过Object库级停止；Artifact stop、Object Audit facts checker、正式Project推进/参与者装配及生产root仍属后继，不据此宣告完整D05/D08或D28/E01完成。此前绑定验收的原Outbox首红未复现、原因未知，继续保留为完整模块测试关注项。

当前fixture空闲；`d08_registry_backend`已按Artifact rev2 `fcb83fc`启动16源业务实施，设计负责人已按Object Audit rev2 `fcae355`启动13源业务实施、固定`6658a6c`，`restore_test_dependencies`已按独立静审通过的D09 Project配置`6e0bda1`启动21源业务实施；三线均无Docker权，`skill_verification`转Object Audit验收准备。后续验收与资源按主线程正式交接推进。
