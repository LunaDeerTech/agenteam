# D11 Milestone / Sprint 结构库技术验收

## 后续全 18 路径接受补充

当前结论：root 已按明确版本组合接受 **全部 18 路径的 Milestone/Sprint 结构卡**，包括原技术17及已精确安装、独立文档核对通过的 [README #18](../backend/README.md)。这不完成整个 D11 或平台，Task/membership、生命周期删除正向、Execution/Dispatch 与生产 Work 装配仍未绑定。

全18接受原件 SHA `74c3fbbe2cd13777af20b6c9c6b7fa5a6f56bf04ba8d29fc829e926bb372ea77`；README 为 80200B / `b6b5b281`，安装记录 `544ba023`，独审 `bd2c4037`。小型 root 原件完整嵌入 [evidence JSON](d11-work-structure-verification-evidence.json) 的 `subsequent_full18_acceptance`，同时固定安装/独审引用。本补充没有新增技术运行；Git 交付仍由 root 另行处理。

下文保留技术17原封存时点全文；其中 README 待装等阶段语句是历史记录。原报告 `3dbcf7a3`、原 evidence `5d16b077` 的身份和字段保持，JSON 原 `state` 也仅表达该旧时点，后续状态以新增接受补充为准。原五新轮旧版本、U1后继组合、全部原FAIL与独立B外部工具terminal缺口均未改写；全18接受不补造该工具终态。

## 技术 17 原封存记录

结论：root 已接受 **17 条技术路径的固定版本组合**，接受原件 SHA `3cfd72ffca9e4eb37bf212a3081e4ef2ce715c35538c612b7c1b6247a85452fe`。本档封存时 README #18 仍待独立文档核对和安装；本结论不表示整张 18 路径卡、整个 D11 或平台已经完成。独立 B 的外部工具 terminal 缺口保留，不能把监督器内的成功 Wait 改称该工具终态。

范围依据 [正式结构卡](../work-items/d11-work-structure.md)（行政版 `812490b6`，语义版 `c07c7112`），沿用 [永久 SPEC 验证](d11-work-structure-spec-verification.md) 与其原 FAIL/D1 档。本次仅新增本报告及 [evidence JSON](d11-work-structure-verification-evidence.json)，没有执行或重跑技术检查。JSON 保存完整 source SHA、阶段与运行原件的 bytes/SHA/定位，以及三份小型 root 原件；大型 raw、源码树、Go 图、binary 和 cleanup 清单未复制，临时原位引用不冒称已永久归档。

## 接受的能力与源码

当前 Human Owner 的 Milestone/Sprint 创建、title/description 更新与手工排序共六命令；Get/List、当前授权的分页 cursor 与同调用者 Tx 的 placement；版本/幂等/Unknown、有界重规划、真实 Stop/Drain；两类 typed Work 事件、精确 Project gate、同 Tx canonical/receipt/Event/Activity 与迁移 `00021`。这里只消费已验 Human Authority、Project 和 Outbox 子能力，不将上游模块整体视为完成。

root 已逐 byte 核当前 17 源与 `accepted-sources`（`d08d1589`）相同。此归档复用该固定确认，没有重新扫描所有源码或声明新的 Git 提交。

| # | 路径 | bytes | SHA-256 前缀 |
| --- | --- | ---: | --- |
| 1 | `internal/central/work/contract/structure.go` | 30084 | `485ddab3e8f5` |
| 2 | `internal/central/work/contract/structure_test.go` | 17190 | `dc97433b8f8a` |
| 3 | `internal/central/work/contract/events.go` | 10590 | `3db5751b8fa3` |
| 4 | `internal/central/work/contract/events_test.go` | 5249 | `b0e28df310c1` |
| 5 | `internal/central/work/service.go` | 3559 | `244d368c4ece` |
| 6 | `internal/central/work/structure.go` | 33511 | `5e8202d24084` |
| 7 | `internal/central/work/rank.go` | 4259 | `dab61abaff9e` |
| 8 | `internal/central/work/reader.go` | 11714 | `e33e75cc10fd` |
| 9 | `internal/central/work/repository.go` | 24060 | `9a19e4842388` |
| 10 | `internal/central/work/events.go` | 10263 | `b98bb27b72ba` |
| 11 | `internal/central/work/structure_test.go` | 28264 | `a180c47782ce` |
| 12 | `internal/central/project/work_event_authority.go` | 3296 | `9317a994514d` |
| 13 | `internal/central/project/events.go` | 12568 | `dad67f637989` |
| 14 | `db/migrations/00021_work_structure.sql` | 5127 | `a7c541546af5` |
| 15 | `tests/work/fixture_test.go` | 37961 | `fa437258e76d` |
| 16 | `tests/work/structure_test.go` | 39046 | `86c051609e14` |
| 17 | `tests/work/structure_concurrency_test.go` | 32526 | `f7885efe1b90` |

## 版本、原失败与离线检查

contract C1/C2 原 STATIC FAIL 保留：Placement 的私有聚合 cap 改为 `2×MaxRequestBytes`，原命令和 receipt 仍为 256KiB；JSON 反例按实际 raw 责任重写。library L1/L2 原 STATIC FAIL 保留：在首个 Work SQL 前验 own issuer、完整已持锁和当前 Project Read，并保最终 exact binding；私有链保存原 CommitResult/Attempt/Cause。PG P1–P3 修正真实并发前提、actual caller Tx/key/mode/blocker 和全体 spectator/no-op 断言，不吞错或改动产品契约。首实际编译因含 func 的 LockKey 不可比较而 FAIL，后以一行 `CompareLockKeys` 修复；这也保留了此前 STATIC 未发现编译约束的局限。原列表顺序检查器失败及修正原件另在 evidence 引用，不当作业务失败或抹除。

driver D1 将 helper 诊断纳入父级取消和实际 Wait；独立 probe AB1 先验证两个原 DTO，再归一 clone 的合法 rank，真实 rank/order/spectator 断言保持。两项原 STATIC 失败链均保留。

原 `workunknown01` 为 **11 RUN / 8 PASS / 3 FAIL（含 top）**。精确原 Lookup waiter 握手已到达，但 `planned/commit=false` 与 `completed/commit=true` 在 caller 取消后返回 `INTERNAL_ERROR`，没有满足 `errors.Is(context.Canceled)` 和零结果，后续恢复断言未到达。inner helper/driver exit255、Go 在真实 top FAIL 后结束、watchdog complete=false 而线程已 join；outer actual exit1。它有完整退休，不是超时或无终局，原六件失败身份由 root disposition `1c498f2e` 保留。

U1 仅在 Lookup 的本次事务已明确 `NotCommitted` 且 caller `ctx.Err()` 非空时返回安全包装的取消错误与零结果；不传播任意 PG error，不改 Committed/Unknown 或原 writer confirmation。最终 Work `structure.go` 为 `5e8202d2`，pure test 为 `a180c477`；后者在 PG 运行图中只是 verification-only。前五个新 PG 轮保留旧 `3db32cb2`/`fd93a507` 身份。root 接受的是这个版本组合，不能称最终源码的一次全套运行。

离线原件包括 contract race/vet、library race/vet、PG3 race-c/vet 与精确六 Work＋三 Project＋两 Outbox discovery、fixture helper build/vet。U1 格式零差量，纯 race **24/24（11 top＋13 nested）** 与 vet 通过；新增 result-only Store 九状态格和 Stop 原 writer 代表仅证明纯控制流，真实 SQL 由后续 PG 证明。独立 probe 的格式/race-c/vet/精确两名 discovery 与私有 helper 绑定复用已接受记录；discovery 不冒业务通过。全部阶段保持原 actual Wait、固定输入和资源未启动边界。

## 真实 PG 结果组合

下表 RUN/PASS 包含 top 与 nested，作者新六轮合计 55/55、旧五轮 12/12、独立 A/B 4/4。原失败轮另列于上节，不混入成功数。

| 实际轮 | 精确 top | RUN/PASS | top | 源版本 |
| --- | --- | ---: | ---: | --- |
| `workmigration01` | `TestWorkStructureMigration` | 12/12 | 4.77s | 原版本 |
| `workpersistence01` | `TestWorkStructurePersistenceAndPaging` | 7/7 | 11.60s | 原版本 |
| `workauthority01` | `TestWorkStructureAuthorityAndReplay` | 7/7 | 2.48s | 原版本 |
| `workatomicity01` | `TestWorkStructureAtomicityAndProducer` | 11/11 | 1.63s | 原版本 |
| `workconcurrency01` | `TestWorkStructureConcurrencyAndRank` | 7/7 | 3.39s | 原版本 |
| `workunknown02` | `TestWorkStructureCommitUnknown` | 11/11 | 14.26s | U1 |
| `workoldlocks02` | `TestProjectB02OwnerPortRequiresCallerTransactionLocks` | 1/1 | 1.10s | U1 |
| `workoldatomic02` | `TestProjectB02AuditEventReceiptTouchAtomicityAndNoOp` | 3/3 | 1.18s | U1 |
| `workoldreplay02` | `TestProjectB02ReceiptRequiresCurrentSessionAndReplaysAcrossRenewal` | 1/1 | 1.12s | U1 |
| `workoldoutbox02` | `TestOutboxAtomicAppendCurrentAuthorityAndReplay` | 1/1 | 1.12s | U1 |
| `workoldoutlocks02` | `TestOutboxMissingLocksPoisonAndMappingChangeRollsBack` | 6/6 | 1.09s | U1 |
| `ind-work-a01` | `TestWorkStructureIndependentAuthorityOrdering` | 1/1 | 2.04s | U1＋私有 probe |
| `ind-work-b01` | `TestWorkStructureIndependentCommitRank` | 3/3 | 10.11s | U1＋私有 probe |

Migration 两组实际验证 fresh/populated 00020→00021、重复迁移、五表/约束与真实 DDL 失败回滚。Persistence 包括六命令、205 项分页、cursor 身份与 stale、真实同 Tx placement。Authority 包括当前 Session/Owner、跨 Actor/Project/命令隔离、归档历史与 planned 新写区分。Atomicity 包括 canonical/order/receipt/Event/Activity 同 Tx 回滚、精确 producer 与 poison/Rows/Drain。Concurrency 包括两 Service 真实 Command EX holder/waiter、User EX/version 竞争、所有 sibling/spectator/no-op facts、旧 cursor 与三轮重规划上限；不把 User 锁等待改写为 rank 锁等待。

U1 后 `workunknown02` 的四个真实 COMMIT hold 组合均到达取消、迟到结果及显式原命令恢复；确认 SQL 错误/撤权仍保原 writer Attempt→Cause，Stop 等待已登记 confirmation，独立 proxy writer 另行释放并等待。旧五轮仅接受各自 Project/Outbox 已有权限、原子性、历史与锁边界，不据此扩大 Work 或上游生产绑定。

独立 A 采用权限/归档 Tx 与真实 caller 锁等待的不同构造，区分当前撤权、合法新 Session 历史、archive 先赢的 planned 与 Work final 先赢的 completed，并核两 Service 隔离。独立 B 用 low/high 两种四对象 rank 几何、真实 COMMIT/rollback、原 Lookup 取消和另一写入竞争，验证原 Attempt、全部 spectator 业务字段/version/time、顺序、generation、cursor、no-op 与历史不触 Activity。证据是固定 probe 的实际断言到达，不声称 raw 含全部 SQL dump。

## 实际等待、退休与 B 的缺口

每轮原件分别记录 launcher→driver、driver→helper、helper→Go 的实际 Wait，watchdog 线程 join，两个精确新 network/container ID 双次 absent、owned/runtime 双空与固定输入前后一致。作者轮是 430 输入及既有 30 sets/104 外部组边界，独立轮为 432 输入及私有 probe/overlay；没有重建全图。原五轮、U1 后续与独立链的历史身份和失败 disposition 各自保留。

105s 执行＋15s fixture cleanup 计入 120s round；package 的 6m 上限与 round 不混用。启动前 fresh 可用磁盘≥5GiB，不是内存限制。cleanup 后 host TCP delta 的附加尾部两清观察≤75s，不是整段运行的暴露上限，也不证明全部短连接或 tuple 所有权。进程观察数不等于 Wait 数；各轮一个非 owned containerd shim 仅观察、未 wait，不宣称全宿主 ECHILD。原失败轮的非零终态也如实保留。

**独立 B 外部工具 session20487 的 terminal/exit 未取得。** 环境 starting→ready 后续 wait 返回 `Unknown process id`。原 outer 监督器记录 driver actual Wait exit0、77.351181s，结束于 `2026-10-08T22:38:33.671428+00:00`；helper/Go、top 10.11s、2 IDs/owned-runtime 与 TCP 尾部 58.452453s 退休原件齐全。这些记录不能替代外部工具 wait。

root 后续有限只读清零 `681d24f5` 用两次原 launcher/driver PID+start identity absent、两 exact IDs 的实际 inspect notfound、原 runtime 双空及四原件 hash 不变，恢复了共享窗口；没有新建/删除资源或业务重跑。这是新的有限当前观察，不回填原工具终态，不证明整宿主树或机器重启。root 按以上缺口明确保留的有限组合接受技术结果。

## 未绑定与后继

Task canonical/membership Source of Truth、Task placement writer、Execution/Dispatch occupancy 与 Work 生命周期清理尚未绑定，缺失端口必须 `DependencyUnbound`，不得假 empty。Sprint start/complete/rollover/delete 正向、Milestone delete、跨 Milestone move 与 Project current_sprint pointer 写入不在本卡。生产 Agent/Tool、Work HTTP/schema/UI/Audit 路由、App/root 装配与消费者未接通，没有新生产 handler，ready503 不变。

fixture 的持久 Account/Session 行经过真实 Authority，Project Create 和 test-only 持久 Skill receipt 提供库层前提；它们不是正式 Login、生产 Skills、Archive/Start/Complete 或 Object stopped 证明。README #18 与后续 root 文档/Git 交付另行接受。Object runtime join、OpenAI tools 独立验收、SPA 并发发布三项停止及 Image/Jina 来源阻塞保持。
