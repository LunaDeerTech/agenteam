# D08 Outbox stop adapter

新窄片基于 main `43590d1f`，依据 [D08 §8](d08-project-owner-design.md#8-生命周期编排)与[四个 stop adapter 合同](recovery-project-domain-bindings.md#四个-stop-adapter)。只实现 Outbox 一项；不恢复旧 22 路径整卡，不声明生产初始化或完整生命周期可用。

## 精确写域与行为

技术写域仅 `internal/central/project/outbox_stop_participant.go`、同名纯测试和 `tests/project/outbox_stop_participant_test.go`。复用实际 `LifecycleAuthority`、R3 decoder/Store/锁与 D06 服务；不修改已有生产接缝、迁移或公共接口。

`NewOutboxStopParticipant(*LifecycleAuthority, outbox.ProjectLifecycleParticipant)` 固定 Name=`outbox`，拒绝空依赖和错 Name。Request/Inspect 在同 Store 短 Tx 的 Project SH 下核原 actor、scope、cause、当前 operation、冻结 manifest 与 phase；只有真实 Committed 返回且原 context 有效才委托。Request 仅当前 stopping 且选中 stop 未 failed；Inspect 沿原 R3 停止/失败/清理/完成只读边界，最小 delete receipt 的原版本证据仍由真实 D06 检查。

同一个 caller-derived 最多 2s context 覆盖预检和原 D06 调用；更短 parent 保持，原调用同步实际返回，不开后台、不重试、不在下游调用期间持 Project SH。下游每个原阶段仍重验 R3，不把预检当缓存授权。cause 的 Project、operation、action、接受 version 全部原样转换；错误（包括 Unknown）不转为成功，预检 Unknown 保留原 attempt/cause 且不调用下游。报告只在真实下游返回、ctx 有效后发布：Stopped 必须 Pending=0，负计数/矛盾报告拒绝；否则 pending/work_pending，不从数量编造 PendingRef。

adapter 不更新 Project operation/participant checkpoint，不自行 claim/Retry，不拥有 ProcessGuard。Cleanup 始终 `DEPENDENCY_UNBOUND`；普通 Deliver/Inspect/Requeue、生产 registry、initializer、ready 与原其他 STOP 保持。下游自身技术停止写入不等于 Project 清理。

opaque Name 与预检 Tx 实际返回后的同步 stop 调用分别设安全 panic 边界，返回固定 `DEPENDENCY_UNAVAILABLE` 和零报告，不带原 panic 内容，也不推断回滚或资源 join。原预检 `WithinTx` 及其 callback 完全不在该 recover 边界内；原事务 unwind 保持。正常下游错误与 Unknown 原样传播。

## 必要验证与首条真实链

纯控制覆盖固定身份/拒绝、同 Store 与实际持锁、事务 Committed/NotCommitted/Unknown、原 context/实际返回、报告转换及 Cleanup 零委托。真实 PG 方法冻结后独审并排期：现有正式 Project 接受 → 原 stop driver 的 stopping → adapter → 真实 D06 非空队列停止/Inspect → 精确 cause/report 与持久事实。另核旧 cause/phase/manifest、预检 Unknown 零下游副作用及终态只读；必要 SQL 前置明确标为 fixture，不能称生产完成。

冻结 PG 方法为 `TestProjectOutboxStopParticipant` 的两个子例：

- `nonempty-queue-stop-and-inspect`：沿原 R3 受控 Account/Skills 前置与真实 authorities，用公开 handler 注册声明及实际 Project Create/BeginArchive 产生两条 pending delivery；不启动 dispatcher 或 callback。原 stop driver 进入 stopping 后，adapter 调用真实 D06，把两条记录写为 `dead_letter/project_stopped`，验证零 attempt/processed、精确停止 receipt、同 cause 的只读 Inspect，以及 Project operation/participant/command/event/Audit/activity 快照不变。下游入口先以正常独立 Tx 取得 Project EX，证明预检 SH 已实际释放。
- `current-facts-unknown-and-terminal`：错误 version/manifest 拒绝且零委托；正式 Store 的原只读 Tx 实际 Committed 后，仅装饰返回为 Unknown，核原 attempt/cause 与零委托。下游入口的正常第二 Tx 改变 phase 后，真实 D06 必须重验并拒绝；随后明确恢复 SQL 观察输入。终态由既有 R3 SQL fixture 构造，仅验原 receipt 只读与 Request/Cleanup 拒绝，不称已完成其他参与者或生产生命周期。

两子例保持原 ProcessGuard 到 driver/Project/D06 调用实际返回，最后 idle Object Runtime Drain 后验证真实 flock 可取得及原 stopped 行。最大化复用 D06 已有 actual callback join、取消和 Inspect continuation 的有效证据，不重复旧矩阵。本片不声称生产 callback 启动已接通。原实际 Wait、七资源、私有目录/后代/宿主 TCP 原门和输入保持要求不变。

## 本组控制原件

`output/ai/outbox-stop-adapter/checks/unit03-{invocation,result}.json` 与 `unit03.log` 记录限定五个 top / 二十个子例的 race PASS；Go PID 372313 实际退出 0，10.324s。同目录 `vet01-*` 记录本 Project 包 vet：PID 372458 实际退出 0，34.300s。两组源输入前后相同，原工具 session 19585 实际退出 0；这些不是 PG 或生产绑定结果。

原 `unit01-*` 和 `unit02-*` 编译 FAIL 保留：分别为控制夹具错误假定 Rows 形状/比较不可比较结构，以及 digest 输入类型错误；只修本纯测试，未改生产迎合断言。unit03 基于 `dd9230e7` 加限定的一行类型修复，精确 SHA 见 invocation，后续源码 checkpoint 不改变已验字节。

## 真实链与最终差额

固定 `0f49aced` 的 `native01` 已有限 wholePASS 并经非作者独验：唯一 top / 两 sub 7.93s，原 Go PID 384133 与 driver PID 382445 实际 Wait 0；七资源十四次 absence，descendants/private/runtime 及原自动 baseline 的宿主 TCP 增量均双次清空，1667 项输入保持。supervisor PID 382426 原 terminal 0 / 96.414s；外层原工具 session 17593 由作者亲见退出 0，实际 supervisor Wait 97.125s。原配方、输入、外层日志与终态保存在 `output/ai/outbox-stop-adapter/native01/`，完整原日志为 `/tmp/obs01/pg-f622e27eca24462e8b48140d3d0ddf51.log`。本线资源、后台及 Go writer 均已退出；运行未见容量错误，不以尾后余量冒充峰值测量。

收口复读正式合同发现 opaque panic 边界遗漏。原 `panic-repro01-*` 绑定未修改的 `0f49aced` 生产字节：构造 Name、当前 Request/Inspect Name、预检实际返回后的 Inspect 四项 FAIL，原事务 panic 继续 unwind 的反控 PASS；原工具 43013 实际退出 1，保留原件。后继 `ad78260a` 仅两个生产/纯测试文件补同步边界；`unit04-*` 六 top / 二十六 sub race 实际 0 / 10.394s，`vet02-*` 实际 0 / 0.815s，两次输入前后相同，原工具 22576 实际退出 0。Request 的新增纯控直接验证 opaque 调用边界，不伪称 receipt 纯夹具能授权 canonical stopping；正常 Request 全路径来自上述 PG。

后继 `ad78260a` 差额已由非作者独审接受，正常路径与普通错误/Unknown 转发不变，可复用上述 `0f49aced` 的正常真实链，按分段证据组合接受；不称 `ad78260a` 新跑了整套 native。unit04/vet02 invocation 的 source_checkpoint 保留原基底 `0f49aced`，其中精确源 SHA 已核对等于 `ad78260a`，不改原记录。普通 delivery、实际 callback 的完整旧矩阵、Artifact adapter、完整 registry、Cleanup、生产 initializer/ready 与旧网络 fault probe STOP 均未由本片解除。
