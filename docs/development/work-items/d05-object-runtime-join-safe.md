# D05：Object Runtime join 的隔离安全修复候选

2026-10-11，主线程在用户持续完成正式计划的授权内，明确建立本次新隔离候选。基线 main `f688738f`，树 `/workspace/agenteam-object-runtime-join-safe`，分支 `ai/object-runtime-join-safe-20261011`；runtime_recovery 实施，variables_review 独立审查，root 负责 Git 与资源调度。这不是重启旧中断执行任务，不改写旧自动筛查记录或原失败，也不开放生产 initializer、registry、Cleanup 或 ready。

## 契约与精确写域

沿用[原已审行为规格](recovery-object-runtime-join.md#行为所有权与事务规则)的共同屏障、原 writer、确认所有权和原预算规则。旧卡的实施暂停作为历史保持；其永久停止的网络探针仍禁止，新增测试不构造事务网络代理、网络截断、异常连接保留或网络 COMMIT/ROLLBACK 故障。新增真实刺激只有任务自有第二个普通 PostgreSQL 事务与本地 barrier；Unknown 只通过正式 Store 结果装饰，明确实际底层结果。下列指定七项既有回归沿原自有 storage I/O fixture，不修改其受控转发或断言。

以下五个实现/测试路径及本卡、本树 current 获写权：

| 路径 | 范围 |
| --- | --- |
| `internal/central/object/service.go` | 私有 ownership、共同退出条件、Drain 预算内推进与服务关闭的实际返回 |
| `internal/central/object/project_work.go` | 首次 helper 的完整 union/原 Tx、确认串行与退休、恢复构造 handle 所有权 |
| `internal/central/object/process.go` | 同一退出条件及 guard 最终停止 owner；并发等待者保各自 context |
| `internal/central/object/runtime_join_test.go` | 新增正式 Store 结果和本地竞争纯控，不加生产 test hook |
| `tests/objects/runtime_join_test.go` | 新增普通锁/canary/正式端口结果的真实 PG/MinIO 控制 |

当前三生产文件对旧 `9d648575` 有合法差额：`process.go` 的 CurrentProcess（+19），`project_work.go` 的 boundedCleanupBudget 与 stopped Skill Cleanup 同 Tx 授权（+64/-11），`service.go` 的 metadataTransactions（+2）。本片以当前主线为底保留这些差额，不按旧卡“与9d相同”的历史描述覆盖。无公共 API、D03、迁移、依赖、原测试断言或 Runtime/Read 编排变更。主线程后续另授两既有 harness 的精确 selector/input/group 字典增量；原 driver 函数、预算和资源尾保持。

## 定量推进与所有权

Service 和 guard 消费同一退出条件：原 operations/cleanup/maintenance、本地未退休 work、首次 writer 和确认 owner 全部实际退出。第一次 AcquireAll 前保存完整原 AccessPlan + work mutex + extra union；多个未决原 writer 分别保留，不由最后一个 Tx 覆盖。业务 Committed 实际返回可退休该 writer；其他原业务结果不改写，对进入 callback 的未确认 writer 保守保留。未进入 callback 的错误不增 owner。

确认需 ended、原完整锁一次归一 EX、真实 canonical 身份或严格 absence，且确认事务 Committed 和原 WithinTx 实际返回后才退休。首次登记缺完整身份却发现意外同 ID 行时失败关闭。确认本身在执行前进入同一私有 owner；同身份只一个确认者，其他调用者等待/跳过，不递归堆积确认 owner，不持 Service mutex 跨 I/O。恢复读取构造的 handle 也纳入此规则。

Drain 的每次实际确认最多取调用者剩余预算与 **50ms** 的较早者；稳定 work ID 顺序并记下一起点，每次尝试后前移，防止低位阻塞项长期遮蔽 ready 项。无进展等原变更通知或 **10ms** 重试时点，不忙循环、不派后台重试，不延长 Drain/Force 原 context。原 Store 调用必须实际返回；context 到期不是 join 或丢弃 owner 的证据。服务关闭与 guard 最终停止各自只有一个实际执行者，其他等待者按各自 context 等原返回；不能锁住 mutex 跨事务让短等待者失去预算。已有预算传递路径保留。

## 冻结的可判别验证方法

首个真实回归沿[已保存普通锁 probe](../agent-team/evidence/object-runtime-join-regression/snapshot/tests/objects/project_work_lifecycle_probe_test.go)，SHA256 `edfcc3d2eecb6157ce15ce0909d29af0c838d7f7831ac9309f72f3895bb1afb5`；原记录 `normal-lock-01` exit1/67.839s 保留。原四红断言不削弱：注册 work 已提交且公共短 body 错误已返回、第二个普通 Tx 持 work mutex 时，短 Drain 不成功、claim 仍 claimed、真实 flock 仍占；holder 正常 Committed 后，同实例后续 Drain 必须补齐 joined checkpoint 才 stopped/释放 flock。

固定纯控顶层：`TestObjectRuntimeJoinOwnerOutcomeBoundary`（callback 未进入、Acquire 部分失败完整 union、NotCommitted/Unknown 不改业务结果、确认返回屏障）与 `TestObjectRuntimeJoinConcurrentOwnership`（同 handle 确认串行、取消等待者、退休通知、重复退出、guard 最终原返回与短等待者）。使用正式 Store 的明确结果及 channel barrier，不以计时猜测竞争。

固定真实顶层沿原规格，子例在测试源码冻结时列明：

1. `TestObjectProjectWorkPendingJoinKeepsRuntimeGuard`：上述原四红；查询 holder PID/原 mutex、work 和 claim，直接核本任务 claim 文件 flock。
2. `TestObjectRuntimeJoinConcurrentDrainAndForceBudget`：两个原 context；将真实 join/guard 停止的原 Store 返回持在普通本地 barrier，另一等待者不能提前退休或超出其预算；放行后原调用全部 join。
3. `TestObjectRuntimeJoinFirstReadWriterBeforeRegistration`：ReadAccess/OpenSourceAccess 精确语义匹配，正式 Authority 在首次完整 Acquire 后、register 前返回错误；第二 Tx 持捕获 union 中实际锁，错误保持且零新增 work/lease/GET，释放后同锁确认 absence；另控未进 callback 不泄留 owner。
4. `TestObjectRuntimeJoinConfirmationFailureRetainsOwnership`：真实 callback 错误/正常回滚，和真实底层结果后装饰 Unknown；即使 joined 行可见，原调用未返回或结果未确认都不退休。正常重查同身份、保持原 checkpoint；不是网络故障复现。
5. `TestObjectRuntimeJoinReadyWorkMakesProgress`：两个真实 ended work；低位正常 mutex 被持有时另一项仍能在原有限预算完成 join，放行后全部退出，无新外部业务 I/O。

适用原回归仅取旧卡列明七项：Force 主/transfer 同预算、Recovery cleanup 预算、Busy 后项、Metadata ready gate、ProjectStop Delete/Cleanup 实际 I/O/回调和 Cancel 实际 source；保原断言。新失败才按证据扩范围，不机械扩旧矩阵。

## 输入、运行与退出门

源码先按当前基线+五路径限定 diff 冻结，实际编译记录原命令/退出；Go 固定 1.27.1，锁定 module/PG/MinIO 沿现有工具。未变通过证据复用；unit/race/vet、integration compile、两 cmd build 按影响执行。新纯控/首个真实链先回答对应风险，剩余适用关键门在正式交付前完成；只编译或 no-tests 不计功能通过。

任何 native 先取得 root 明确唯一窗口，使用自有隔离 fixture 和原 `scripts/test-objects.sh`/既有监督器，原 race/count1/6m、540+60 与七资源双尾不弱化。若需新增精确 selector 字典，另报 root 授窄写域，不改原驱动或资源框架。冻结实际同包源码、工具、脚本及二进制；原 Wait/exit、精确 nonce/IDs 两次 absence、desc/private/runtime/TCP 双尾、输入不变全齐才报 wholePASS。业务通过与尾失败分别记录，不用后验无进程补原终态。

## 实现与有限验收结果

方法 `c6f9d299`、三生产初片 `45d25313`、Force 关闭 owner 修复与纯控 `e299ba83`、首个 PG top/profile `e2b7090f` 已由主线程保存。独立审查发现 Force 的实际 Backend.Close 原先绕过服务关闭 owner；普通本地 Close barrier 的 `force-close-repro01` 实际 exit1，证明另一个 Drain 与 guard 可以提前完成。最小修复令 Force/Drain 共用关闭 owner，将真实主/transfer Close 全部返回纳入共同屏障；未改 storage 或网络实现。

`output/ai/object-runtime-join-safe/checks/` 保留全部原 invocation/log/result：`unit01` 的测试 CauseID 误期待 FAIL 保留，按原 Object COMMIT_UNKNOWN/Unknown 投影修断言；`unit02` 原工具56101实际0，race 5顶层17子例（新2顶层6子例）。`package-race01`、`vet01`、`build-central01`、`build-runner01` 均实际0，未启动两个业务二进制；对应输入前后未变。独立审查已接受 Force 竞争修复与上述有限证据。

`compile01` 的 race-c/list 原工具2178实际0，1706编译输入前后相同，精确命中首1顶层；`native01` 绑定1707项和最终二进制 `b4e18d82d0f95c3f9125977ee9f21156edd36cf9eea14eda6d6d152c188e43b5`。主线程明确授权独占窗口后，原普通锁测试主体 PASS 5.53s：holder backend127 正常持锁时原四红对应的 Drain/claim/flock 保护均成立，放行后同实例补齐 joined checkpoint、claim stopped，并实际重取 flock。Go247035/driver244759实际 Wait0；七资源14次 absence、desc/private/runtime/TCP 双尾、输入不变全部齐。原工具98235由作者亲见实际0；supervisor244711实际0/106.762s，原监督日志 `/tmp/ojs01/pg-f247863eb5fe4918b43366daa13bbe83.log` 与 `native01/{invocation,process,terminal}.json`、`outer.log` 保留。variables_review 已独立接受首1顶层有限 wholePASS。

后继只增两精确 profile 与 PG 测试，固定 `4807ac91` 保持三生产/纯控及首顶层正文不变。`compile02` 的原工具81089实际0，1706编译输入前后相同，两个 list 精确命中新4顶层和旧7顶层；`native02/03` 均绑定1707输入、70同包 Go 与二进制 `89000fc68c0d8297e97f834eb72babf15c3b7e6bd4ffccea6187ab9165da7c4a`。两组分别经方法/输入独审与主线程独占窗口授权，未重跑首组。

| 运行 | 精确场景 | 实际终态与原件 |
| --- | --- | --- |
| `native02` | 新4顶层10子例：join/guard 原返回屏障；Read/Source 登记前及未入 callback；正常回滚、Committed 后装饰 Unknown、恢复构造 owner；阻塞低位 work 与 ready 后项 | 原工具54934亲收0，supervisor258455实际0/146.032s；Go260275/driver258456实际Wait0。原日志 `/tmp/ojs02/pg-c3c46b9a47e54082a06c6c99a86cee83.log`，`native02` 的 invocation/terminal/outer 原件齐。已独立接受有限 wholePASS。 |
| `native03` | 原7顶层10子例：本卡指定的 Force 双 I/O 预算、Recovery startup/worker、Busy 后项、Metadata confirmed/cancelled、ProjectStop Delete/Cleanup 和实际 source Cancel | 原工具99665亲收0，supervisor263601实际0/193.866s；Go265599/driver263623实际Wait0。原日志 `/tmp/ojs03/pg-cf871026b0144fce8a16d4c31e3377eb.log`，`native03` 的 invocation/terminal/outer 原件齐。已独立接受有限 wholePASS。 |

三组均有七资源14次 absence、desc/private/runtime/TCP 双尾及输入不变 terminal0；variables_review 已接受三次分段证据和本卡限定修复，不称单次当前 HEAD 全绿，也没有用后验观察补写原退出。所有旧普通锁 FAIL、新 `unit01`/`force-close-repro01` FAIL 均原位保留。未知结果子例明确是实际底层 Committed 后的正式 Store 结果装饰；不证明网络 Commit Unknown。Read/Source 验证完整原 union 的 EX 确认、真实普通 canary 与零新增 canonical work/lease，nil reader 和固定失败路径证明未达 `backend.get`，不声称有独立 MinIO GET 计数。

本片的 Account/Project Authority 沿原受控 fixture；恢复构造 handle 子例仅对精确旧 process 提供受控 ProcessAuthority，两个真实 standalone Service 最后各自 Drain，不证明真实进程死亡或 foreign join。其余新增 Runtime 场景实际消费 claim/flock。最终候选只修本卡退出所有权缺陷，生产 initializer/registry、Cleanup、ready=false/503、完整 D05/D08、Artifact/Project 全组合及其他 STOP 不由此完成；旧中断记录和永久停止的网络探针不恢复。当前全部技术文件停写，自有调用与资源均已实际退出。
