# Project + Secret Audit 窄绑定独立验收

2026-10-05，`skill_verification` 独立验收通过，主线程采纳八源结果并已提交、推送 `81fe7427ceb4672247b3d30a51c10a2e2808ba04`。验收者逐项核该提交的八源与冻结输入一致；远端相同由主线程 `ls-remote` 确认，见 [提交核对](evidence/project-secret-audit-binding-verification/accepted-commit.json)。通过范围是实际 Account Session / Project Owner 与 gate、Secret checker、Audit、Secret Service 的窄组合；没有生产 root / HTTP 或完整 D08 模块完成声明。

**保留项：作者旧回归批次的 Outbox 顶层曾失败，原因仍未确定。** 独立基线与候选各 16 次有界诊断，以及原完整顶层一次只读观察均通过，仅证明本轮未重现；没有证实时间根因，没有修改旧 fixture 或产品，也没有把原旧批次表述为一次全绿。后续完整模块验证仍须关注此项。

## 固定输入与审查

规格为 [Project + Secret 绑定卡](../work-items/recovery-project-secret-audit-binding.md) rev2；已采纳 `dc45a6a` 中含采纳记录的完整文件 SHA-256 为 `ae16d0ab4773c8615bc495d33778dab7297f24bdbb915d1417ee758894160c80`。实现基线 `7d7c50df0dafcdeaaf700dc2662a6013245bbb6f` 加八源，作者 [冻结清单](evidence/project-secret-audit-binding-verification/author-candidate-manifest.json) SHA-256 `019f871617e5c2f49df4af44bf6c9c9153043bde69708cb72d767a7fa79d5e6f`。自有 snapshot 从固定 `git archive` 建立，只覆盖清单内八源，没有纳入活动 S2 或后续主树；go.mod/go.sum 未变。见 [输入指纹](evidence/project-secret-audit-binding-verification/fixed-inputs.json)。

四生产源从作者首轮真实测试前起保持不变。`NewAuthority` 复制受限 producer map；不能覆盖 Project producer，Secret 的 nil / typed-nil 和保留 Object 项明确拒绝，其余未知项不被静默忽略。构造无 SQL 或 Service / Registry 环。旧 Project Audit 事实、Owner、R3 lifecycle 分支未改；新增 Human Secret 分派使用当前 Session / Owner / Mutate gate，Service 仅允许 SecretResolve 的严格角色、Project、cause 和 active gate。

完整 ctx / Tx / Entry / AppendKey 原样交给真实 Secret checker；没有重建 witness、只比 SemanticDigest、临时补锁或嵌套事务。新 Secret delegate 保存 Authority 值，核 Project scope / 精确 ProjectID 后走当前 Owner Mutate，cleanup 仍 unbound。opaque checker 没有构造期 Store getter；同 Store 装配和运行期精确 Store / Tx witness 分别举证，不声称构造时能识别任意 wrapper。详见 [四生产源静审](evidence/project-secret-audit-binding-verification/production-static-review.md)。

## 实际结果与证据复用

| 检查 | 结果与边界 |
| --- | --- |
| 作者纯检查 | Project / Secret / Audit unit、race、相关 unit / integration vet 通过；Project / security / Account integration compile-only 通过。最终 Unknown fixture 窄改后 Project compile / vet 再过 |
| 作者六个新真实顶层 | real1 的 CRUD、CurrentAuthority 原体未变；diagnostic1 的 Construction、FactRejection、ResolveGate 修后通过；最终 Unknown 三子例由 confirmed-commit 通过。不是失败首轮整组通过 |
| 作者旧回归批次 | 49 顶层通过、1 失败、1 child-only skip。security 10 顶层 47.324s；Account 2 顶层 6.471s；Project 37 顶层通过，因原 Outbox 顶层失败而包失败（121.348s） |
| 独立纯增量 | caller map 变更 / 并发分派 race + count=1 通过；独立真实 probe 的 race compile-only、integration vet 通过；最后旧顶层观察 overlay race compile-only 通过 |
| 独立真实 binding 增量 | 1 顶层、3 子例通过；Project 4.307s，driver 18.972s，exit 0 |
| 独立旧红诊断 | 同一 probe 在基线 / 候选各固定 16 次，均通过，未出现时间逆序；随后候选原完整旧顶层一次通过，Project 2.939s，driver 17.209s |

作者 [原首轮](evidence/project-secret-audit-binding-verification/author-real1.log)、[修后诊断](evidence/project-secret-audit-binding-verification/author-diagnostic1.log)、[最终 COMMIT](evidence/project-secret-audit-binding-verification/author-confirmed-commit.log)、[旧回归原红](evidence/project-secret-audit-binding-verification/author-regressions.log)及各自 command / result 均保留。用 [函数体匹配记录](evidence/project-secret-audit-binding-verification/author-test-evidence-reuse.json)核实上述复用场景与最终源相同；原源码快照仅保留必要两份新测试历史，归档后缀为 `.go.txt`。无测试包、compile-only、child-only skip 均不算业务通过。

作者实际组合验证真实 CRUD / purpose 变化 / delete 后 receipt 与 Audit tuple、重放不增 Audit；当前 Session / Owner / gate 先于旧 receipt；合法 typed Entry 的 Session、HTTPTrace association、metadata 等变化和真实跨 Store / Tx 拒绝。修后错误断言同时核 Secret 外层 DependencyUnavailable、实际 Project / checker 精确内层错误以及最终回滚，没有只检查 err 非 nil。

Human 一般 Project lease 缺 User SH 的实际 resolve 拒绝且无可读 material；Service 正例使用真实持久 lease 和严格 fixture Usage grant，撤 Usage / lease / gate 后拒绝。该 Usage fixture 不是生产 Project Usage provider。最终 mutation Unknown 的 committed 分支在原最终 Apply + receipt + checker + Audit Tx 中观察真实 `COMMIT` tag 和 `ReadyForQuery idle` 后丢 ACK；pending / rollback 仍使用原 held-writer 及原锁串行化。resolve 三态也保留零 material 边界。

独立增量补充了三个实际 PG 场景，见 [完整 probe](evidence/project-secret-audit-binding-verification/independent_binding_test.go.txt)、[命令](evidence/project-secret-audit-binding-verification/binding-independent-command.json)和[原日志](evidence/project-secret-audit-binding-verification/binding-independent.log)：

1. 最终 writer 先在同 Tx 查询精确 canonical / receipt / Audit / Session tuple，记录 backend PID 后才 arm。客户端 Unknown 时，实际 `pg_stat_activity` 为原 backend idle in transaction，`pg_locks` 证明精确 User SH；User EX 在原 200ms 预算内阻塞且无已提交事实。释放原 COMMIT、join 及原 RequiredLocks 串行化后只有一套事实。随后在 User EX 下撤原 Session，旧 prepared replay 与 Lookup 都先拒绝；续发当前 Session 可恢复原 receipt，Audit 不增。
2. 已有成功 receipt 后，在 Project EX 下改变当前 Owner；原 prepared Apply / Lookup 拒绝且事实不变。恢复 Owner 后重放原 ref，Audit 不增。
3. 真 delegate 缺 User SH 的错误被 callback 故意忽略：port 为 DependencyUnavailable，真实 Store poison 使最终 InternalError / NotCommitted，先写入的 marker 也回滚。完整 User / Project 锁下同 delegate 正常通过。

另以公开 Authority 接口做 [map race probe](evidence/project-secret-audit-binding-verification/independent_binding_map_test.go.txt)：构造完成后 caller 反复替换 / 删除 / 新增 map 项，与 256 次分派并发；始终选初始 provider，replacement 调用 0。此项使用单测 SQL fixture，未冒称真实 PG 行为。

## 原失败与未归因旧回归

作者 unit1 的新 archived fixture 缺 archived_at，修复仅补合法测试输入，原日志和 patch 保留。real1 中错误外层码断言及已取消 captured context 的反例前置被修正；新 Tx 由新 bounded root context 创建，再在 callback 内传保留 witness 的 WithoutCancel view，避免把取消或 nested-Tx 拒绝误算精确 witness 检查。

real1 的 mutation committed 等待 proxy.completed 5s 曾失败。原红没有协议观测；后来的诊断只证明 CancelRequest 与 held COMMIT 可能竞争，不能唯一复原原失败。最终 committed 故障改为真实 COMMIT + idle 后丢 ACK，原 pending / rollback、5s join 与六分钟包预算不放宽。原 [差异链](evidence/project-secret-audit-binding-verification/author-confirmed-commit.patch)和失败日志保留。

旧 `TestOutboxLifecycleInspectionAuthorityBoundary/completed` 的六子例原来均得到 DependencyUnavailable，预期是 InvalidState 或 exact terminal success。静态线索是旧 phase 对 archived_at / updated_at、completed_at / updated_at 分别调用 `clock_timestamp()`；ProjectRef.Validate 与 LifecycleOperation.Validate 会拒绝终态时间晚于 updated_at。原红未记录四个原值，因此不能确定它命中了哪个拒绝分支。

独立 [基线原日志](evidence/project-secret-audit-binding-verification/baseline-terminal-time.log)与[候选原日志](evidence/project-secret-audit-binding-verification/candidate-terminal-time.log)各固定 16 次接受 / 完成，逐次记录四时间、Authority / Inspect 结果并检查原六种 Outbox 本域状态。32 次均无逆序，未触发诊断 probe 中“先保存逆序、再用同一捕获时间校正”的条件分支；没有执行任何时间归一化。

随后只在 overlay 的原 phase completed 后增加一条只读观察调用，原 SQL / 原完整顶层断言不改，也不循环或延迟重试。[原完整顶层日志](evidence/project-secret-audit-binding-verification/original-outbox-top-observed.log)记录：archive 与 updated 均为 `2026-10-05T13:32:10.995264Z`，completed 与 updated 均为 `2026-10-05T13:32:10.994622Z`；从当前行构建的完整 ProjectRef / LifecycleOperation Validate 及实际 Authority 均为 nil，原完整顶层通过。见 [汇总](evidence/project-secret-audit-binding-verification/terminal-time-diagnostic-summary.json)及[观察源码](evidence/project-secret-audit-binding-verification/independent_terminal_observer_test.go.txt)。

基于受影响生产接缝静审、作者同源分段证据和独立真实权限 / 事务增量，未发现要求八源生产返修的明确阻断，主线程采纳窄绑定。**这不是旧红根因已经查清或完整旧批次全绿的结论。** 未归因原红保留为后续完整模块验证事项，不修旧 fixture，不删除或替换原失败证据。

## 复现、清零与停止范围

实际使用精确 Go 1.27.1、锁定离线依赖、自有 cache / 空 Docker config、本机 socket；独立四轮均调用原 PostgreSQL driver，保留 `-tags=integration -race -count=1 -timeout=6m`。业务测试运行 PG 17.8 / vector 0.8.1；helper 的 PG 16.12 不支持版本 fixture 不是第二份业务通过。独立轮未启动 MinIO。原命令、环境、工具指纹和结果均归档；[reproduce.py](evidence/project-secret-audit-binding-verification/reproduce.py)从固定基线与采纳提交重建输入及完整 probe，不依赖旧 tmp 存续。

```sh
python3 docs/development/agent-team/evidence/project-secret-audit-binding-verification/reproduce.py
python3 docs/development/agent-team/evidence/project-secret-audit-binding-verification/reproduce.py --pure
# 以下各模式仅在获得独占 fixture 窗口后执行，按需要选择，不是要求全部重跑。
python3 docs/development/agent-team/evidence/project-secret-audit-binding-verification/reproduce.py --fixture
python3 docs/development/agent-team/evidence/project-secret-audit-binding-verification/reproduce.py --mode original-top --fixture
python3 docs/development/agent-team/evidence/project-secret-audit-binding-verification/reproduce.py --mode baseline-diagnostic --fixture
python3 docs/development/agent-team/evidence/project-secret-audit-binding-verification/reproduce.py --mode candidate-diagnostic --fixture
```

重建脚本的 prepare-only 已实际核指纹和 overlay，未借归档再运行 Go / Docker。原执行脚本另以 `original-` 前缀保留，记录当时路径，不作为可移植入口。

独立四轮累计 owned 8 容器 / 4 网络由原 driver 清理并二次 exact-ID inspect absent；既有 2 容器 / 4 网络 ID 集合不变，任务进程 0、runtime 空。当前 baseline 的 ID / name / labels 只读保留，未读取容器环境或凭据；本轮没有记录此前 name / labels 快照，故一致性断言限于 ID 集合。末检自有 snapshot、作者冻结源、待提交主树八源全匹配，见 [资源和输入末检](evidence/project-secret-audit-binding-verification/final-input-resources.json)。Docker 窗口已交回主线程供 S2 继续，验收者不再使用 fixture。

生产 root / HTTP、生产 Project Usage provider、Human 一般 lease 正向 resolve、AgentRun / Execution、Object / Artifact facts checker、Secret cleanup、生命周期推进 / 终态完成均不在通过范围。没有修改产品源码、旧 fixture、旧报告、状态卡或 Git；只归档本报告及同名证据目录。[证据清单](evidence/project-secret-audit-binding-verification/SHA256SUMS.json)覆盖持久原日志、probe 与检查记录；本交付冻结后 all-stop。
