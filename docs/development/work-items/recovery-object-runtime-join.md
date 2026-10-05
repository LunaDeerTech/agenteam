# D05 恢复：Object Runtime 的真实 work join 屏障

修订：rev2，仅更新执行状态，rev1 已审行为正文不变。规格已审并提交 `8bce2d9319f0b649b7c144ba14d9ee52f7dd09da`；随后业务执行被自动安全筛查中断，当前原实施任务停止，不改派或重试，缺陷仍未修。文末原实施移交保留为历史，现状以新增中断记录为准；不代表 D05/D08 全模块验收完成。

## 输入、证据与完整结果

固定生产基线为 `9d648575d07ce791dd623eb45eb6604cc6c908b5`。它与红例基线 `42e3f7d59ceb65cb22cdd6b639608caa8830fae2` 在 `internal/central/object/`、`internal/central/postgres/`、`tests/objects/` 的已提交内容无差异；不读取或合入 Artifact、Project、Provider 等活动稿。既有规则沿用 [S2 Object 停止卡](recovery-d05-object-stop.md)、[D05 设计](d05-object-storage-design.md)及 [S2 独立验收](../agent-team/d05-s2-verification.md)，原 work 身份、权限、锁、取消、恢复和预算规则不变。

已冻结的普通锁竞争证据位于 `/workspace/agenteam-object-join-probe-e__5hndp/normal-lock-01/`：`report.md` SHA-256 `f8da8cf27a44cd2a6c5bde0d9c6aaf78f855d78e30b9a76f5dd3aa97043024d1`；17 项证据索引 `SHA256SUMS.json` SHA-256 `6dadd63726f8209fe8161632a02a621f1e719a4b1c5ecfb534555b190cf23dfe`。唯一原 probe 为其兄弟 `snapshot/tests/objects/project_work_lifecycle_probe_test.go`，SHA-256 `edfcc3d2eecb6157ce15ce0909d29af0c838d7f7831ac9309f72f3895bb1afb5`；原输入、日志和脚本已停写，不用修后证据覆盖。

该 probe 离线 race 编译通过；真实原 driver exit 1、67.839s，Objects 3.502s。已提交 preparation work 的 `joined_at` 仍 NULL、正常第二事务持 work mutex、公共调用已返回短 body 的 `INVALID_ARGUMENT` 时，`Runtime.Drain` 却返回 nil、process claim 写 stopped、flock 被释放；正常放行并确认 holder Committed 后仍未补齐 join。4 容器/3 网络两次 exact-ID absent、既有 2 容器/4 网络不变、所属进程/runtime 清零；其余 18 个 no-tests 包不算功能通过。

本卡完整结果：公共调用结束后仍未退休的 work、首次登记前已进入的相关 writer，以及 join 确认自身，均持续归同一 Service/Runtime 所有；Drain/Force 在原预算内推进实际确认，只有这些所有权全部真实收束后，才允许关闭服务资源、写 process stopped 和释放 flock。一次失败不能提前退出，也不能因为只增加计数而永久不再尝试 join。

首次必读 [AGENTS.md](../../../AGENTS.md)、[团队流程](../agent-team/README.md)、[设计技能](../../../.agents/skills/agenteam-design/SKILL.md)、[Go 开发技能](../../../.agents/skills/agenteam-go-development/SKILL.md)；独立验收另读[验证技能](../../../.agents/skills/agenteam-verification/SKILL.md)。精确 Go/PG/MinIO 沿[已验依赖记录](../agent-team/dependency-recovery-2026-10-05.md)，不改版本、镜像、依赖或 fixture driver。

## 已确定源链与 Read 首次登记窗口

| 固定源码 | 事实及本卡处理 |
| --- | --- |
| [service.go](../../../internal/central/object/service.go) 的 `admit`/finish、`Drain` | finish 尝试 work join 后仍删除 operation；Drain 只计 operation、cleanupRequest、maintenance，漏计残留 work。改为共同退出屏障并有界推进确认 |
| [project_work.go](../../../internal/central/object/project_work.go) 的 `finishOperationWork`、`joinProjectWork` | 失败 join 保留 ended handle；现有原锁 EX 和确认提交后退休规则是需要保留的能力。补实际确认 owner、串行及通知，不把已有保守保留改成删 map |
| [runtime.go](../../../internal/central/object/runtime.go) 的 `Drain`/`Force`，及 [process.go](../../../internal/central/object/process.go) 的 `finish` | Runtime 先 Service 后 guard；guard 重复相同漏计谓词。仅修 guard 对共同屏障的消费，Runtime 公共签名和编排不变 |
| [read.go](../../../internal/central/object/read.go) 的 `openRead`，及 `withinProjectWork`/`admitProjectWork` | `openRead` 第 250/254 行已有私有 handle；helper 却先 Acquire/Validate/业务 callback，最后才 register。登记前错误可能不留下原 Tx/计划锁的本地记录。两个首次登记 helper 一并补私有 writer 所有权，不需改 read.go |
| [D03 transaction.go](../../../internal/central/postgres/transaction.go) 的 callback/rollback 分支 | `NotCommitted` 是业务不提交结果；该公开结果及本地 Tx token 失效本身不提供所有原 backend 锁已终结的独立证明。D03 实现及公开结果不变 |

Read/OpenUploadSource 的首次事务是同一 work 退出结果的必要部分，不能只在 durable register 之后开始跟踪。但本次普通 mutex 红例只覆盖已登记 work；登记前窗口目前只有静态证据，不宣称原 ROLLBACK 网络情形已复现。下述正常 canary 锁和正式端口错误只验证保守所有权规则。

## 行为、所有权与事务规则

### 共同退出条件

在 Service 的同一互斥保护下，退出必须同时满足：operations、cleanupRequests 均为空；maintenance 已实际停止；本地 projectWork 全部退休；本卡关联的首次 writer、进行中或待确认的 join owner 均已退休。`ProcessGuard.finish` 消费同一谓词，不能另写一个较弱的计数。`drained`、context 已取消、公共方法返回、Tx token 不再可用或 `joined_at` 单次可见，都不单独证明该条件。

`StopAdmission` 仍只停止原有准入；Force 仍按既定规则取消/关闭以打断实际 I/O，不能因此退休 work 或 guard。Service 资源关闭及 guard 最终停止沿既有顺序执行，其他并发 Drain 不能在确认/关闭尚未实际返回时抢先宣称成功。无 Runtime 装配的 Service 也必须满足同一 work 屏障。

### 首次 writer 的私有记录

两个首次 helper 在 Discover 成功、进入真实 WithinTx callback 后，**在第一次 AcquireAll 之前**保存该原 Tx 与完整单次计划 union：原 AccessPlan 的全部锁、原 work mutex 及原 extra；它只是私有 ownership，不是假定已持有全部锁。AcquireAll 部分失败也保留完整原 union，不能仅记成功取得的子集或最后一个锁。

该记录关联原 Service、operation、work handle 与不可变 work 身份；更新和退休须核同一私有 owner，不能以相同 UUID 替换另一个 handle。可用独立私有集合承载登记前 writer，避免把没有 durable work 的记录误当成可取消、可授权或可恢复 I/O 的事实。不改变正式 registry 的 work/epoch 意义，不创建 source、lease、权限 grant 或数据库行来填空。Discover 失败、WithinTx 未进入 callback 等确证尚无此 writer 的路径不得遗留无必要的待办。

登记成功后的 work 与 writer 交接不得出现无人拥有的间隙。业务 Tx 确认 Committed 后可以退休该 writer，但仍活跃或未 join 的 work 留在原 registry；公开 NotCommitted、Unknown 的错误及调用者语义保持原样。已进入 callback、可能持域锁的未确认 writer 保守留下原身份/union；不能把 NotCommitted 改报业务 Unknown，也不能直接删除 owner。

### 实际确认及确认者自身

原 `joinProjectWork` 的实际 ended、本地原 Tx 不再活动、原完整锁归一 EX、canonical work 身份及确认提交规则保留。锁必须一次完整 Acquire，不在持锁后升级；既有真实权限、admission、revocation、cleanup fence 与跨项目规则不变。

对于尚未登记的 writer，同样先以其完整原 union EX 确认原 writer 屏障，再查询 canonical work。只有完整身份匹配的实际行才能按既定规则推进；不存在时只能在确认事务 **Committed** 后退休本地 owner，不插入补造行。登记前未获完整持久身份却发现意外同 ID 行时失败关闭，不据猜测 epoch 更新它。这里的 absence 不是读一次空表或重新发一个 work ID。

每次确认开始前就归入同一退出所有权；SQL callback 结束与本地 `InTx` 失效不等于确认事务自身终局。确认 Committed 且其调用实际返回后才退休相应 owner/handle。确认 Unknown，或已进入 callback 的 NotCommitted，保留本次完整 union 和原 work 身份，等待后续同锁确认；已证未进入 callback/无待收尾 writer 时才允许直接结束该次尝试。不能靠读取已写 `joined_at` 直接跳过这个屏障。

同一 handle 的确认只能有一个实际执行者；并发 finish、recovery、Drain 复用/等待该所有权，不能在一个确认尚未结束时由另一个删除 map。实现不得递归累积无限确认 owner：同一有界状态记录本轮待确认的原身份/完整 union及是否正在尝试，前一轮确定终局才替换或退休；确有多个原 writer 时逐一保留其必要证明，不用最后一个 Tx 覆盖未决前驱。不得持 Service registry mutex 跨 PG I/O。

### 原预算内推进及唤醒

Drain 在调用者原 context/截止时间内重试真正 ended 的本地 work 与待确认 writer；不重新启动业务，不发 GET/PUT/cleanup、不新发 cause/work，不等待人工调用另一个公开方法才有进展。每次尝试有有限子预算，不能延长调用者或 Force 的原预算；同一低位阻塞项不能使其它 ready 项一直得不到尝试。实现者在冻结输入中记录选取的内部尝试上界和轮次策略，不增加公共配置或新默认超时。

无进展时等待既有变更通知或有限重试时点，禁止忙循环和脱离调用者的后台重试。原 context 到期则及时返回既有错误，owner 仍保留；取消一个 Drain 不得取消另一个 Drain、偷退其正在确认的 owner，或重新开放准入。成功退休、登记前安全退出以及影响可推进性的状态变化在同一 mutex 下唤醒等待者，不能漏通知或重复关闭 channel。

正常释放争用锁后，同一实例后续 Drain 必须补齐实际 join 并完成退出；已有 joined checkpoint 幂等保留原时间/身份。重复和并发 Drain/Force 不得重复业务副作用、不因关闭先后次序提前释放 guard，也不能永久卡住。

## 精确候选文件与排除项

本卡审查通过后由主线程另授一位 backend 实现者以下 **5 个路径**；现在只有本规格写权。实施前确认三个旧生产文件已交还，并核基线 delta；与 Artifact 活动 16 源、Project 绑定/R4、Provider 各自文件隔离。

| 路径 | 允许变更 |
| --- | --- |
| `internal/central/object/service.go` | 私有 ownership 状态、共同屏障、Drain 原预算推进/并发收束与通知；构造只初始化所需私有状态 |
| `internal/central/object/project_work.go` | 首次 helper 的原 writer 记录、join 自身 owner/串行/退休、局部有界推进；既有身份和锁证明不弱化 |
| `internal/central/object/process.go` | guard 消费共同退出条件；原 claim/flock 和停止事务不换成推断 |
| `internal/central/object/runtime_join_test.go`（新增） | 无 Docker 的正式 Store 结果分支、ownership 竞争与通知测试；不增加产品测试 hook |
| `tests/objects/runtime_join_test.go`（新增） | 下节真实 PG/MinIO 集成、局部正式 Store/Authority 装饰器及普通同步 helper；原已冻结 probe 作为首个回归保留其核心断言 |

不修改 Runtime/Read/Access 的公共接口、D03、shared contract、迁移、driver、go.mod/go.sum、已有测试断言或其他卡/台账。不存在 schema 变更，00017 仍仅归 R4 completion_plan。其他 Object 入口的全局泛化审计、跨进程死亡、Artifact stop、Project 生命周期推进/Cleaner、HTTP/root、D28/E01 不在本卡交付范围；发现相关新增确定问题须交主线程评估，不无界扩大实现。完整模块及原旧 Outbox 时间红的关注项保持原状态。

## 精确验收

本卡为恢复/并发高风险修复，作者冻结五源后由未参与实现者独立验证。生产修复不得靠改短原断言、跳过失败、伪造 canonical work、直接删除 map/claim 或关闭数据库连接制造成功。

新增包内顶层固定为 `TestObjectRuntimeJoinOwnerOutcomeBoundary`、`TestObjectRuntimeJoinConcurrentOwnership`：使用正式 Store 接口的确定结果及同步 barrier，覆盖未进入 callback 的安全直退、进入 callback/Acquire 部分失败的原 union 保留、业务错误不改写、同一确认的互斥、取消等待者不退休执行者、退休通知和重复退出。结果 double 只证明状态协议，不冒充真实 PG 网络 Unknown。

新增集成文件提供以下 **5 个顶层**，子例在作者测试输入冻结时列明：

| 顶层 | 必须实际观察 |
| --- | --- |
| `TestObjectProjectWorkPendingJoinKeepsRuntimeGuard` | 沿原冻结 probe 的正常第二 Tx work mutex；公共 PreparePayload 已返回且 work 未 join 时，短 Drain 不完成、claim 仍 claimed、真实 flock 仍占用；holder 正常 Committed 后同实例 Drain 补齐 join，才 stopped/释放 flock |
| `TestObjectRuntimeJoinConcurrentDrainAndForceBudget` | 真实 pending work；两个不同 context 的 Drain/Force 竞争。将某次真实 join 的 Store 返回停在普通本地 barrier，另一个不能抢退 guard；短调用取消不偷退 owner，释放后全部有界结束，Force 不另开收尾预算 |
| `TestObjectRuntimeJoinFirstReadWriterBeforeRegistration` | ReadAccess 与 OpenSourceAccess：真实 Acquire 完整计划后，正式 Authority/Store 路径在 register 之前返回错误；正常第二 Tx canary 持原 union 中精确锁，公共错误保持且无新增 work/lease/GET。Drain 仍保留 guard；放行后以同锁确认 absence，零补造行且完成退出。另覆盖尚未进入 callback 的失败不会永久占 owner |
| `TestObjectRuntimeJoinConfirmationFailureRetainsOwnership` | 确认阶段的真实 callback 错误/正常回滚后公开 NotCommitted，及正式 Store 结果装饰器的 Unknown 分支；正常 canary/本地返回 barrier 期间即使 joined 行已可见仍不能退休 owner。解除故障后同身份实际确认，checkpoint 幂等且 guard 最后释放 |
| `TestObjectRuntimeJoinReadyWorkMakesProgress` | 至少两个真实 ended work，低位 work 的正常 mutex 持有不妨碍另一 ready work 在有限原预算内完成 checkpoint；首项释放后全部完成，无跨项目误取消、无新外部 I/O |

登记前 canary 必须是主动拥有、有限截止时间、正常提交/回滚的第二事务；从原正式计划/Acquire 参数记录精确锁，用语义条件命中 Read/Source 或 join，不能靠“第 N 次事务”猜测。原事务与 canary 的 PID/tuple、真实 work/lease 行、公开返回、claim/flock、context 和 barrier 均留证。它能证明保守资源跟踪，**不能证明原 writer 在 ROLLBACK 传输失败后仍持锁**。Unknown 结果装饰器须记录实际底层事务结果，明确是公开状态分支驱动，不写成真实网络故障复现。

原被自动安全筛查中断的网络探针永久保持停止；本卡不构造、不恢复代理、网络截断或异常服务器连接保留手段。验收使用上述普通锁竞争、正式端口错误及适用既有回归。已有 S2 Unknown 证据可作为原协议背景，不能代替本次变化后的状态/ownership 验证。

适用既有回归精确选择：`TestObjectRuntimeForceJoinsMainAndTransferIOInOneBudget`、`TestObjectRuntimeRecoveryCleanupKeepsStartupAndWorkerBudget`、`TestObjectRuntimeBusyDoesNotMaskLaterRecoveryFailure`、`TestObjectRuntimeMetadataGateUntilActualProbeAndRecovery`、`TestObjectProjectStopDeleteWaitsForActualIO`、`TestObjectProjectStopCleanupWaitsForActualIOAndCallbacks`、`TestObjectCancelProtectsActualSourceUntilJoined`。这些测试读固定基线并保留原断言；其他未变通过证据复用，新增失败再按真实影响扩大检查。

作者执行精确 Go 1.27.1 的 Object unit/race/vet、integration compile 与两个 cmd build；真实集成沿原 `scripts/test-objects.sh -run '<冻结精确集合>'`，由 `GOFLAGS` 传 verbose，原 driver 保持 race/count1/6m。Docker/PG/MinIO 必须由主线程交接唯一窗口后启动；独立验证可自建私有 probe，不越权改五源或原证据。报告真实顶层/子例、首红与修后对应输入，no-tests 不计功能通过；固定源码末检、nonce/labels/exact IDs 双次 absent、原基线资源不变、所属进程/runtime 清零后交回窗口。

## 交付与升级

先冻结规格交主线程和独立审查，再移交实施写权。作者交付五源差异、真实检查及原始日志、首红保持位置、未验证边界和 all-stop；独立验收结论分清实际 PG 结果与 Store 结果模拟。未获写权、不具备固定依赖、需要扩路径/API/schema/资源方法时，仅暂停受影响部分报主线程；本卡通过前不宣称退出遗漏已经修复。

## 规格采纳与实施移交

主线程已采纳独立静审报告 `/tmp/agenteam-object-runtime-join-static-22q1yt18/review.md`，SHA-256 `5e8c6a29242c4876a816d2a2007d3ad013331fcf18ea66ce1263be1bf2c6e03f`；审查输入为本卡候选 SHA-256 `ce689cfcad2ad211d05c7a77d552fe96c1053d924bfbf7f93a3b7ea2d5f6ceaa`。五路径范围与正文行为不变。普通锁原红及持久证据已提交 `696b523ae95b8bfcc1f6e5544888d9166d0bb45c`，不以规格通过声称产品修复完成。

唯一实施者移交为 `d08_recovery_design`，仅开放本卡三生产加两新增测试；固定 `9d648575` 加自己五源，其他活动候选不混入。实施须覆盖外部恢复构造 handle 的确认所有权，以及最终 guard 停止事务被另一 Drain 等待时的原 context 预算；这些均属于正文共同屏障和并发预算要求，不增加接口或路径。独立验收由未参与实现者进行，准备计划由 `recovery_handoff` 冻结。现在无 Docker 权，真实执行前须正式交接唯一窗口；既定停止的网络探针不恢复。

## 业务执行中断与当前状态

规格 `8bce2d9319f0b649b7c144ba14d9ee52f7dd09da` 已获独立静审通过并采纳。随后该次业务执行被自动安全筛查中断，工具原文为 `possible cybersecurity risk`；此处只记录原文，不推断触发原因。当前原实施任务停止，不改派、不重试；本次状态归位不恢复实施或测试。

中断时没有产品或测试源码改动，三个生产文件仍与固定 `9d648575` 一致，两个新增 `runtime_join_test.go` 未创建。未运行 Go/compile/test、Docker/SQL/网络，未启动 fixture 或产生自有运行资源，无未结束命令或关联后台进程。`/workspace/agenteam-object-runtime-join-prep-4t_us3q5/tree/` 保留 803 文件的固定私有准备快照；该目录的 `baseline/`、`spec-rev1.md`、`input.json`、`report.md` 原件保留，`bin/evidence/gocache/gotmp/runtime` 为空，没有运行日志或产品验证结果。

[普通锁竞争原失败及持久证据](../agent-team/object-runtime-join-regression.md)已提交 `696b523ae95b8bfcc1f6e5544888d9166d0bb45c`，原红与旧 S2 验收不改写。退出缺陷仍未修，Artifact 最终共享 guard 采纳与依赖它的 Project 领域绑定当前被阻塞；Artifact 本域普通修复和 OpenAI wire 实施继续，不能据此宣布受阻依赖完成。原已停止的网络任务继续停止，本次没有新增方案、探针或业务代码。
