# D08 R3 作者交付（非独立验收）

16 源/测试路径已冻结，停止写入；Git 写操作 0。固定树 `/tmp/agenteam-d08-r3-1VJDoN/tree` 来自 `49c6589c3919cad62a4bae2c993b5fd953d36b1e`，只同步本卡 16 路径，隔离并行 D05 S2 Object 源。`frozen-inputs.json` SHA-256 `d0e5fd1c03cf6b9a22c74ae299b324d983a537a90bab8dd329d79f572d8b25d9`。

实现真实 Project LifecycleAuthority（冻结声明、整份 manifest、current pointer/gate/版本/owner、删除最小 receipt），Object typed continue/read-only grant，以及 Outbox private issuer/精确依赖/resolver。Outbox Inspect 先确认只读 receipt；非终态追加 Stop 计划，在 preflight/gate/progress/report 同 Tx 逐次 Inspect→Stop 校验、一次完整 union。取消仍仅在 confirmed committed preflight 后匹配原 handle；Cleanup/capture/cancel/terminalProof 控制流未扩展。

通过：Project/Outbox 单测 race、vet；两真实测试包 compile-only 与 integration vet；原 `scripts/test-objects.sh` 的修复后 R3 + B02/R2 + Outbox lifecycle/stop/3 recovery 组，原 race/count=1/timeout=6m 未变。真实 Project 38 顶层通过（127.804s），真实 Outbox 21 顶层通过（60.848s），Outbox 包内 2 顶层重复通过。`TestProjectB02ClaimChild` 单独顶层因 child-only 条件 skip，不计为通过；无测试包不计入行为证据。完整命令/环境/hash 在 `r3-real-regression-command.json`，原日志在 `r3-real-regression.log`，纯检查见 `checks.json`。

首轮原始失败保留在 `unit-first.log`（LockRequest 不是 comparable）、`unit-second.log`（空 AccessDependencies 被契约提前拒绝），已修正确切比较/负例构造；`r3-real-first.log` 保留第一真实轮。该轮 Outbox 2 新真实顶层全部通过；Project 失败系新 fixture 未同步 archived_at/updated_at、尝试 schema 不允许的 archive/completed、以及未独立断言 callback unavailable 与 Store poison rollback InternalError。修复仅新测试，具体差异 `first-repair.patch`；真实 phase/pointer 规划竞态增加见 `phase-race-coverage.patch`。没有改旧 SQL 或放宽断言/预算。`local-check-diagnostic.json` 另记录证据脚本误判 git --no-index 的 diff exit=1，属于证据整理问题，非产品失败。

Unknown 由真实 PG proxy 按 `outbox.stop-observe`、`outbox.stop-authorize` cause，在事务回调完成后精确 arm 实际 COMMIT，覆盖原 writer commit/rollback/仍未决的零取消、错 cause 重试和 join。双计划序列按真实 Tx 记录，检查 preflight/gate/progress/report 都为 Inspect→Stop；额外依赖分别 RequireHeldLocks，wrapper 校验每物理 Tx 仅一次 AcquireAll。

证据分界：`tests/project` 使用真实 Project facts + Outbox Service 验权限/receipt/真实规划后 phase/pointer 变化。`tests/outbox` callback/Unknown/join 复用严格持久 fixture provider，属于 Outbox 协议反例，不能替代生产普通 DeliverProject 绑定。普通 delivery/requeue、Audit checker、生产 root、生命周期推进/Restore/Retry/cleanup/finalizer 均仍未由本卡实现；Object 测试仅证正式 typed port，不宣称 D05 Service 已停止。

资源：两轮各自 4 容器/3 网络均由 driver nonce/exact-ID 清理并二次 inspect absent；创建事件、前后基线、准确 ID 在两个 `*-result.json`/`*-events.jsonl`。基线 2 容器和网络集合未变。`process-cleanup.json`：本任务进程/后台命令 0。fixture 窗口交回主线程，可交 skill_verification 独立验收。
