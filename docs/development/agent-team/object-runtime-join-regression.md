# Object Runtime 未 join work 的退出回归

结论：**固定 `42e3f7d` 的 Object Runtime 存在已复现、待修复的退出屏障缺陷**。真实 preparation work 已登记，正常 PostgreSQL 事务仍持有其 mutex、join 尚未确认时，`Runtime.Drain` 返回成功，process claim 已写为 `stopped`，实际 claim flock 可以取得；正常放行 holder 并确认提交后，再次 Drain 也没有补写 join checkpoint。

本结论来自 `recovery_handoff` 独立执行的一个普通锁竞争 probe，主线程已核原日志。本次文档执行者只核固定证据并归档，没有修改产品、重跑测试或执行 Git 写操作。既有 [S2 验收报告](d05-s2-verification.md)和当时通过结果原样保留；本记录补充此前测试未覆盖的新已知缺陷。Artifact 最终共享 guard 采纳须等待上游修复 delta 与相应验证，不据既有 S2 通过推定此边界安全。

## 固定输入与实际执行

基线为 `42e3f7d59ceb65cb22cdd6b639608caa8830fae2`，仅加入 [project_work_lifecycle_probe_test.go](evidence/object-runtime-join-regression/snapshot/tests/objects/project_work_lifecycle_probe_test.go)，SHA-256 为 `edfcc3d2eecb6157ce15ce0909d29af0c838d7f7831ac9309f72f3895bb1afb5`。原 [input.json](evidence/object-runtime-join-regression/input.json) SHA-256 为 `ec82e4b9f8eb2d48b9b57defe3b67aab3d1042f400f6a3754c67157ea713a572`；运行初末 767 项全部匹配。本次归档另只读核对 766 个固定 Git blob 与归档 probe，均匹配；不消费 Artifact、OpenAI wire 或修复卡的活动候选。

| 检查 | 真实结果与来源 |
| --- | --- |
| 离线 integration race 编译 | Go 1.27.1，精确 argv/env 见原 input；[compile.json](evidence/object-runtime-join-regression/compile.json)记录 exit 0、34.686 秒。 |
| 普通 PG 锁竞争 probe | [实际调用](evidence/object-runtime-join-regression/normal-lock-01/invocation.json)：`sh scripts/test-objects.sh -run '^TestObjectProjectWorkPendingJoinKeepsRuntimeGuard$'`，`GOFLAGS=-mod=readonly -buildvcs=false -v`；driver 沿原 integration/race/count=1/timeout=6m。 |
| 实际失败 | [完整日志](evidence/object-runtime-join-regression/normal-lock-01/driver.log)及[结果](evidence/object-runtime-join-regression/normal-lock-01/result.json)：唯一目标顶层 FAIL、四条失败断言，Objects 包 3.502 秒；driver exit 1、67.839 秒。无 race 报警或 skip；另 18 包 no-tests-to-run 不计功能通过。 |

原 driver 日志 SHA-256 为 `19339033697ae87a32c23f7a72f7dbcad66256c792ef025e89096bc83add2726`。原件没有重跑、改断言或覆写失败结果。编译准备阶段 `fixture_authorized=false` 保留原值；后续真正取得运行窗口的事实记录在 invocation，不回改较早输入。

## 已复现行为

正常 Runtime 初始化后，公共 `PreparePayload` 已提交 preparation work，停在本地短 body 的 Read barrier。另一条主动拥有、15 秒截止的正常 Store 事务，通过正式 CommandLock 持该 work mutex。释放 body 后，公共调用因短 body 正常返回 `INVALID_ARGUMENT`；查询仍确认 `joined_at IS NULL`，holder 的真实 PG backend 125 处于 `idle in transaction`。

| 阶段 | 直接观测 | 违反的退出条件 |
| --- | --- | --- |
| holder 仍持锁，公共调用已经返回 | Drain 为 nil、claim 为 stopped、独立文件句柄可取得真实 flock；work 尚未 join | 未终局 work 必须继续由 Runtime 拥有，不能宣布进程已停止。 |
| 正常放行 holder，确认其事务 Committed | 第二次 Drain 仍成功，`joined_at` 仍 NULL | 退出前必须完成真实 join checkpoint，不能仅等待一次公共调用返回。 |

这里的 holder 是正常、受控、最终确认提交的活事务；没有代理、网络截断、丢弃客户端或保留异常 backend。日志与探针没有手造 CommitResult。

固定源码链为：`service.go:162` 的结束函数在 `finishOperationWork` 返回后移除 operation；`project_work.go:367` 保留 join 失败的 ended handle，但忽略该次错误。`joinProjectWork`（294）本身仍要求原 origin 失效、完整原锁 EX 以及 checkpoint 确认提交，才移除 handle。上层 `Service.Drain`（183）与 `ProcessGuard.finish`（`process.go:388`）的完成条件漏计该残留 work，`Runtime.Drain`（265）因此允许 claim 退休。缺口在上层拥有和退出条件，不能把现有单 work 的严格确认误写为“NotCommitted 直接删除 work”。

修复由主线程另立 `recovery-object-runtime-join.md`，该卡由 `recovery_handoff` 唯一维护。本报告不冻结其实现方案，也不授予业务写权；修后须验证持锁期间保留 guard、放行后同实例真正 join 并完成退出，以及预算和并发收尾边界。

## 资源、归档与未验证范围

[资源原始记录](evidence/object-runtime-join-regression/normal-lock-01/resource-handoff.json)保留两次 exact-ID inspect；[复核解释](evidence/object-runtime-join-regression/normal-lock-01/resource-handoff-reviewed.json)确认本轮 4 容器/3 网络均已不存在，原 2 容器/4 网络 ID/name/labels 不变，189 个采集到的子进程均已退出，所属进程为 0、runtime/gotmp 为空。原 parser 没有识别网络的 `network <exactID> not found`；其错误布尔摘要与原 exit/stderr 一并保留，复核没有重启 fixture。

[证据目录与重建说明](evidence/object-runtime-join-regression/README.md)保存原 17 项证据及其索引、单 probe 和可复原基线指纹，不保存缓存、完整 snapshot、凭据或运行 descriptor。[原报告](evidence/object-runtime-join-regression/normal-lock-01/report.md) SHA-256 为 `f8da8cf27a44cd2a6c5bde0d9c6aaf78f855d78e30b9a76f5dd3aa97043024d1`，原 17 项索引 SHA-256 为 `6dadd63726f8209fe8161632a02a621f1e719a4b1c5ecfb534555b190cf23dfe`。

先前网络 ROLLBACK 任务因自动安全筛查中断，仅留下[静态收束记录](evidence/object-runtime-join-regression/static-only/report.md)：没有 probe/snapshot、没有 compile、动态验证或资源。没有请求用户绕过筛查，也未恢复该执行任务。本次普通锁竞争只证明上述退出条件缺陷，**不证明原 ROLLBACK 网络场景可达**，也不替代 read-before-register 或完整 Object/Artifact/Project 生命周期验收。

当前缺陷仍待修复；历史 S2 验收和 Object Audit 结果不改写。完整 D05/D08/D28/E01 及 Artifact 最终共享 guard 集成仍未完成。本次仅执行文档、原件 SHA 和本地链接检查，无 Go、Docker、SQL 或网络操作。
