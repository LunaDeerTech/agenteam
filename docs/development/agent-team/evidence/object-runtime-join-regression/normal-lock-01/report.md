# Object pending work 的正常锁竞争验证

结论：固定 `42e3f7d59ceb65cb22cdd6b639608caa8830fae2` 的退出屏障确实漏计尚未确认 join 的本地 `projectWork`。单个真实 PG probe 失败；不涉及代理、网络截断或保留异常服务器连接。原受阻 ROLLBACK 网络探针保持停止，该场景的动态可达性仍未验证。

## 固定输入和实际运行

- 唯一新增源码：[单 probe](../snapshot/tests/objects/project_work_lifecycle_probe_test.go)，SHA-256 `edfcc3d2eecb6157ce15ce0909d29af0c838d7f7831ac9309f72f3895bb1afb5`。仓库及产品源码未修改。
- [固定输入](../input.json) SHA-256 `ec82e4b9f8eb2d48b9b57defe3b67aab3d1042f400f6a3754c67157ea713a572`；767 项源码/SQL/模块输入初、末全部匹配。精确 Go 1.27.1；私有离线缓存。
- [离线 race 编译](../compile.json) exit 0，34.686s。
- [实际 argv/env](invocation.json)：`sh scripts/test-objects.sh -run '^TestObjectProjectWorkPendingJoinKeepsRuntimeGuard$'`，`GOFLAGS=-mod=readonly -buildvcs=false -v`；未给 driver 不支持的 CLI `-v`。原 PG driver 使用 integration/race/count=1/timeout=6m。
- [完整首轮日志](driver.log)和[结果](result.json)：driver exit 1，67.839s；唯一目标 top 失败，Objects 包 3.502s。其余 18 个包为 no-tests-to-run，不计功能通过。

## 实际事实

正常 Runtime 初始化后，真实 `PreparePayload` 已提交 preparation work，停在纯本地短 body 的 Read barrier。第二个主动拥有、有 15s 截止时间的正常 Store 事务通过正式 CommandLock 持该 work mutex；随后释放 body，公共调用正常返回 `INVALID_ARGUMENT`。查询证实 work 的 `joined_at IS NULL`，holder 的 PG backend 125 仍处于其正常 `idle in transaction` 状态。该 holder 不是被丢弃客户端之后遗留的 writer。

| 阶段 | 实际结果 | 应保持的不变量 |
| --- | --- | --- |
| holder 仍持 mutex，公共调用已返回 | `Drain=nil`、claim=`stopped`、真实 flock 可取得；work 未 join | Drain 不完成，claim/flock 继续归 Runtime 所有 |
| 正常放行 holder，并确认其事务 Committed | 第二次 Drain 仍完成，但 `joined_at` 仍 NULL | 先补齐真实 join checkpoint，再完成退出 |

原失败未重跑、未改断言或产品。全部四条失败断言均保留在完整日志；没有 race 检测报警或 skip。

## 精确源链及最小修复结果建议

固定源码的 `internal/central/object/service.go:162` 在 `finishOperationWork` 返回后无条件移除 operation；`project_work.go:367` 的失败 join 保留 ended handle，但忽略该次返回错误。`joinProjectWork`（294–339）本身要求原 origin 失效、完整原锁 EX 屏障以及 checkpoint 确认提交，才移除 handle。

`service.go:183` 的 Drain 只计算 operations、cleanupRequests、maintenance。`runtime.go:265` 因而调用 `process.go:388` 的 guard.finish；后者在 398 行重复相同漏计谓词，最终写 stopped 并关闭 flock。Service/Runtime 没有公共 Joined 方法，本次未新增此类 API。

建议下一项完整修复仅围绕以下结果，候选生产路径为 `internal/central/object/service.go`、`project_work.go`、`process.go`；沿本 probe 及既有生命周期测试验收，不需 SQL/共享 contract/新公共 API：

1. Drain 和 ProcessGuard 使用一致的退出条件，未退休 projectWork 及正在执行的 join 确认必须仍被拥有；公共调用返回不能代替真实 work 终局。
2. Drain 在调用者原剩余预算内，对真正 ended 的 handle 有界重试现有 join 证明；不要只加 map 计数导致永久无进展，也不要启动脱离调用者的收尾。Force 不另开预算。
3. 同一 handle 的并发收尾须串行或显式计入 owner，不能让一次成功移除 map 掩盖另一个尚未结束的确认事务；错误、Unknown 或未证终局时保留 handle。成功退休唤醒等待者。
4. 原 work 身份、完整原锁 EX、真实 checkpoint 的确认规则不弱化。普通 holder 仍在时，本 probe 应保留 claim/flock；正常放行后，同一实例须实际 join 并完成 Drain。

这三条生产路径是精确候选范围，不是已授权实施或已通过修复。原 ROLLBACK/异常连接场景不能由本次正常锁竞争替代宣称覆盖。

## 资源交还与证据

[原始资源记录](resource-handoff.json)保存了两次真实 exact-ID inspect；[复核解释](resource-handoff-reviewed.json)确认本轮 4 容器/3 网络均不存在，nonce/labels 对应本次 driver。两次基线 2 容器/4 网络的 ID/name/labels 全部不变；189 个采集到的子进程均已退出，workspace 相关进程为 0，runtime/gotmp 为空。原摘要 parser 只识别 `No such object`，未识别网络的 `network <exactID> not found`，故其布尔摘要有误；实际 stderr/exit1 原封保留，复核未重启 fixture。

唯一 fixture 窗口已正式交回 root。Go/driver/fixture/monitor 命令全部结束；本报告仅归档本轮结论。
