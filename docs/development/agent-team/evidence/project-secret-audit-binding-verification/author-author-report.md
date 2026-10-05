Project + Secret + Audit 八源候选已实现并冻结，但整体验收仍有一项旧回归阻断，未宣称通过或可提交。

固定输入为 `7d7c50df0dafcdeaaf700dc2662a6013245bbb6f` 加 `candidate-freeze/manifest.json` 的八源；manifest SHA-256 `019f871617e5c2f49df4af44bf6c9c9153043bde69708cb72d767a7fa79d5e6f`。仓库、执行 tree、候选副本逐项一致，go.mod/go.sum 与基线一致（`final-input-check.json`）。四个生产源自首轮真实运行前起未变，仍匹配先交独立 V 的 production-freeze。没有改 Secret/SQL/contracts/Object/其它 R3 源，没有 Git 写操作。

实现了受限不可变 producer map、独立 Secret delegate，以及实际 Secret 域 Audit 分派；Human 继续核当前 Session/Owner/Mutate，Service 只接受 SecretResolve 的严格角色/Project/cause 与当前 active gate，再委派真实 Secret checker。opaque checker 没有构造期 Store 身份 API，保证来自正式同 Store 装配和运行时 Tx/私有见证；未假称任意 wrapper 同一。Project lease Human subject 因没有 User SH 计划仍拒绝且零 material。Service resolve 组合使用严格持久 Usage fixture，未交付生产 Project Usage/AgentRun/root。R3 phase fixture 只构造合法 Authority 输入，未宣称生命周期推进已完成。

通过证据：

- Project/Secret/Audit 单测与 race 通过；相关 unit/integration vet、Project/security/Account compile-only 通过。日志为 unit2/race1/vet1/compile2。最后仅 Unknown fixture 窄修后的 Project compile/vet 为 confirmed-commit-compile/confirmed-commit-vet。
- 六个新增真实顶层已分别通过：real1 中 CRUD、CurrentAuthority；diagnostic1 中 Construction、FactRejection、ResolveGate（和旧版 Unknown）；confirmed-commit 中最终 Unknown 三子例。只复验修改影响场景，不能把失败的 real1 总组称通过。完整链为 new-tests-chain.json，各轮 input/command/log 原样保留。
- 最终 Unknown 的 committed 模式先在协议上确认真实最终原 command COMMIT 与 ReadyForQuery idle，再丢 ACK。日志明确 tag-COMMIT → commit-idle-ack-dropped → driver CancelRequest；Store 实际返回 Unknown，canonical/receipt/Audit 同时存在。pending/rollback 使用原 held-writer proxy，保留真实锁、原 writer join 与 receipt/原命令恢复。不是 nonce PrepareWrite 的提交。
- 所选旧回归 49 顶层通过、1 失败、1 child-only skip；security 10 顶层 47.324s、Account 2 顶层 6.471s，Project 37 顶层通过但包因下述一项失败（121.348s）。无测试包和 child-only skip 不计能力。原 race/count=1/timeout=6m、版本与断言未放宽。

原失败和修复边界：

1. unit1 的新 archived fake 缺 archived_at，被真实 Project Ref 校验拒绝。只补合法 fixture 时间，保存 unit1-input/log 和 unit1-fix.patch；之后 unit2/race 通过。
2. real1 多处新断言将 Secret 外层错误误当 Audit 内层错误。Secret 原有包装为 DependencyUnavailable；修后同时断言实际内层精确拒绝码和外层码。捕获的 witness context 随原 Store operation 终结被取消；新事务必须由新的 bounded root context 创建，再把 WithoutCancel(captured) 传给 CheckAppend，才能真正验证 witness 跨 Tx/Store 拒绝。保留 callback 错误与最终 poison rollback 的独立断言。生产源未改，修复差异为 real1-diagnostic.patch。
3. real1 Unknown/committed 等待原 proxy completed 5s 失败。原首红没有协议观测，确切 PG 响应无法追溯。新增诊断观察到 driver CancelRequest 与 held COMMIT 的竞争风险，但不能宣称唯一复原原失败。随后按主线程裁决用实际 COMMIT+idle 后丢 ACK 的确定性 committed 故障；pending/rollback 不变。confirmed-commit.patch、最终协议日志和输入均保留。

唯一未闭合的旧回归：`TestOutboxLifecycleInspectionAuthorityBoundary/completed` 六个子例得到 DEPENDENCY_UNAVAILABLE，预期为 INVALID_STATE 或 exact terminal success（regressions.log:473–486）。所有顶层/子例原错误保留在 regressions-summary.json；不因源码未改就断言与绑定无关。

该顶层属于 tests/project 的旧 38 项，不在 tests/outbox 的 21 项。此前 R3 作者冻结日志 `/tmp/agenteam-d08-r3-1VJDoN/evidence/r3-real-regression.log:533–573` 对同一 fixture/test SHA 曾全部通过；R3 独立 V 当时另外运行 `^TestProjectR3Independent` 三顶层，没有重跑本原顶层。当前相关 11 个旧输入与 7d7c50d 逐字相同；其中 R3 冻结源逐项对比也一致（regression-old-source.json 及保存副本）。

静态线索：r3Fixture.phase 的 completed 分别给 operation completed_at/updated_at、Project archived_at/updated_at 调用两次 clock_timestamp。00013 两表的物理列顺序都是 updated_at 在终态时间前，数据库未约束终态时间 <= updated_at；ProjectRef.Validate(types.go:92) 和 LifecycleOperation.Validate(lifecycle.go:552) 则会拒绝逆序，Instant 保留微秒。错误可沿 InspectStop → inspectLifecycleReceipt → ValidateInTx → lifecycleFact → loadProject/loadLifecycleOperation → Validate → unavailable，再由 Outbox portError/commitError 原样返回。原失败未采集四个原始时间，尚不能判定具体分支或唯一根因。

八源可能涉及的接缝仅 NewAuthority 增加 nil map 的私有复制与非 Project Audit 分支。该旧测试的 Lifecycle binding 不消费 Secret delegate/map；Project 自有 Audit action 仍走旧分支。此静态关系不替代基线/候选对照；主线程已安排独立 V 真实采样四时间与 Validate/Inspect，并比较同一 DB 时间绑定的 fixture 对照。当前作者无权改旧 fixture，未运行新诊断 Go/Docker。此阻断解除前，八源不能完成最终采纳。

资源：四轮均沿原 test-objects.sh、自有空 DOCKER_CONFIG、固定 PG17/16 digest 和精确 SHA MinIO。每轮 4 容器/3 网络的 exact ID 均确认不存在，原基线不变。13:14:41 UTC 记录所有 28 个 owned exact ID 清零，go/docker/*.test/minio runtime 列表为空（resource-handoff.json），作者命令结束并正式交回窗口。之后仅整理证据/只读诊断，八源持续停写；等待独立验收与 root 后续范围。
