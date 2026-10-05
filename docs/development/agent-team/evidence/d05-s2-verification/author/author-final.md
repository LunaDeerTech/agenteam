# D05 S2 作者交付（等待独立验收）

Object 项目停止的卡内 28 源已实现并停写：15 旧生产接缝、3 新生产源、3 新单元文件、7 新集成文件。最终固定基线为 `49c6589c3919cad62a4bae2c993b5fd953d36b1e`，覆盖 review03；只读完整副本为 `candidate-final/`，`input.json` SHA-256 为 `c3dc0800bb7b4c2c7368186b42954f2ba8ac23d2cefd2d2d0ff6990f6c6320bd`。28 项 SHA 已同时核对共享 workspace、执行 snapshot 和冻结副本。没有修改公共 contract、S1、旧 SQL、旧测试、module、脚本、其他领域或他人文档；没有 Git 写操作。正式 ProjectStopAuthority/root 组合留给后继集成，本次只交 Object 库结果，不宣告 D05/D08 整体完成。

## 实际行为

正常 prepare/upload/read/source/download/transfer 与独立 verification/cleanup 在真实 I/O 前登记持久 work，只有 confirmed admission 才开始 I/O。多 Project 关系分别登记，保留完整原 writer 锁、原 process/epoch、maintenance worker/fence；Unknown 不猜终局，不替换身份。Archive 保留已发布读取，Delete 等所有适用 I/O；Request/Inspect 用正式 Authority，同事务精确验证。取消、spool.Close、扫描 cursor 与本地没有 handle 都不当作实际 join。恢复只接受本地真实完成或精确外部进程死亡，并继续确认原 writer 终局和 native 事实；远端 Transfer 仍要正式退休事实。

完整 writer 引用覆盖 PUT、verify、FinishWriterAccess/DB 尾部。Discard 与 writer admission 同 mutex 串行；取消时活 writer 令 Discard 返回 Busy，writer 实返后自动继续 discard，防提前 Stopped 和永久 Pending。Source cleanup 的 native release/work join 共用原取消与时间预算。维护 child operation 只从同 Service、仍在 active registry、ctx 未取消的真实父 admission 继承初始化位，保留 shutdown/force/quota/普通 runtimeReady gate，child cancel 不取消跨项目父轮次。

## 作者检查

所有正式真实执行均使用固定 `snapshot/`（49c6589 + 当轮 28 源），由未改的 `sh scripts/test-objects.sh -run <正则>` 驱动，保持原 race/count1/每包 6m。各 `input-<运行名>.json` 记录实际 command/cwd/environment/28 SHA；PG/MinIO 版本、镜像与 nonce 在对应原日志。Go1.27.1；离线锁定 modcache；本机 Docker socket + 自有空 Docker config；未连接既有数据库或读取其凭据。

| 检查 | 输入 | 原始日志 | 结果 |
| --- | --- | --- | --- |
| object/... unit/count1 | review03 | `logs/unit-05.log` | exit 0；0.926s |
| object/... race/count1 | review03 | `logs/race-04.log` | exit 0；2.285s |
| object/... vet | review03 | `logs/vet-04.log` | exit 0 |
| integration Object 仅编译 | review03 | `logs/compile-runtime-fix-01.log` | exit 0；不计真实测试 |
| 两 cmd build -o 自有 tmp/bin | review03 | `logs/build-central-03.log`、`logs/build-runner-03.log` | exit 0 |
| 21 新 + 15 旧 core，36 顶层 | review02 | `logs/new-core-final-01.log` | exit 0；87.774s |
| 原 Runtime 11 + 新 verification/cross-project 2 | review03 | `logs/compat-runtime-02.log` | exit 0；46.548s |
| Transfer core 16 | review03 | `logs/compat-transfer-core-01.log` | exit 0；38.907s |
| Transfer recovery 11 | review03 | `logs/compat-transfer-recovery-01.log` | exit 0；29.170s |
| Download 12 | review03 | `logs/compat-download-01.log` | exit 0；22.958s |
| Object recovery 9 | review03 | `logs/compat-object-recovery-01.log` | exit 0；21.226s |
| 授权路径 diff --check | 最终共享源 | `logs/diff-check-final.log` | exit 0 |

`coverage-final.json` 按实际正则与固定源码声明列出全部选择项：21 新 + 74 旧，共 **95 个不同顶层**；review03 直接执行 61 个，其余 34 个沿未变行为复用 review02 的通过结果。此处没有把 95 项说成在同一最终输入重跑；两项新维护测试既在 review02 全组也在 review03 定向组内。review03 唯一 delta 为两个生产文件的 child 初始化准入与一个新增单测，精确前后 SHA/patch 位于 `production-review-03/`；原 runtime 失败完整两 top 已在 runtime11 原组重验，独立 V 将对最终源执行卡定与风险组。其他包 `[no tests to run]` 不计通过项。

新卡 21 个入口逐项与冻结任务卡比对一致。覆盖包含真实 body/Close/回写阻塞、resolver 未返、完整 standalone writer 尾部、cleanup zero/remove/callback、真实外部 kill/flock、多页 native 事实、旧 cleanup worker 被 native 新 claim 覆写、多关系、跨 Project 维护隔离、远端延迟 PUT/退休，以及实际 PG COMMIT 的 admission/preflight/gate/join/stopped/maintenance Unknown。Unknown 维护测试确认同 Tx immutable work 与 native worker/fence/process，并检查原 writer 持锁时不能靠查询猜终局。

## 保留的失败与修复

1. 初期编译出现 fence Version/int64 与错误 Stat 替换两项报错，原始终端输出未另存；后续 `compile-02.log` 通过。`unit-01.log` 为私有 plan 单测缺依赖锁的 InvalidArgument，补真实规范化 User 依赖锁后通过。
2. `fixture-new-01.log` 的三个 reader 场景原红使用 4-byte body，正式 integrityReader 在 ReadObject 返回前同步 EOF/Close/release。原轮库已清理，不能补取当时 SQL 状态。修后新增小 body 同步回调/native released/work joined 对照；活 reader 场景改为 256KiB + 真实 GET hold，Request 前证明 active lease/unjoined work/exact process/body 未返。保留原 stop 断言和预算。`fixture-fix-03` 定向及随后全组通过。
3. `fixture-fix-02.log` 是给原 driver 多传不支持的 `-v`，启动前即拒绝、资源 0；删除多余参数，未修改 driver。
4. `compat-core-01.log` 的旧 Source unopened Force 红由 work join 在已取消 release 后另开活 cleanup budget、重争 held Object 锁导致。修复沿用原 ctx/剩余预算；取消后保留 ended + 待 checkpoint 身份，未改旧断言或 500ms。`join-fix-01` 定向及 36 项全组通过。
5. 独立静审指出 standalone Prepare→Reserve→UploadPrepared 在 spool.Close 后仍有活 verify/回写尾部。`writer-red-01.log` 以真实 300000-byte PUT+verify、进入 DB acquire 前的 FinishWriterAccess 阻塞复现 archive/delete 假 Stopped 和显式 Discard 误成功。原 18 生产副本在 `production-review-01/`，四生产修复 delta 在 `production-review-02/`，完整原红五源差量在 `writer-red-input-delta/`，可由 review02 重建并逐 SHA 对上原 input。修后要求 Pending 持续到实返，再自动 Stopped/Drain 清零，无需手动补 discard；定向与 36 项全组通过。
6. `compat-runtime-01.log` 的旧 RuntimeBusyDoesNotMaskLaterRecoveryFailure 与 RuntimeRecoveryCleanupKeepsStartupAndWorkerBudget/startup 原红：Runtime.Initialize 已真实 admit 初始化父，但新增 childOperation 清 parent 后按普通准入被 runtimeReady=false 拒绝。review03 继承真实父初始化位并验证 registry/scope/ctx，补无父/假父/foreign/done/canceled/普通未 ready/shutdown/forced/quota/独立 cancel 单测，未绕过停止 gate。原 runtime11 + 两维护定向当前全过。原红 input 精确等于 `candidate-review02/`。
7. `compile-writer-red-01.log` 曾因 cwd 误留共享 repo 而编译成功；此结果作废，不作隔离验证证据，没有 Docker 动作。`compile-writer-red-02.log` 在固定 snapshot 重做。所有真实 fixture 都来自固定 snapshot。

## 清理与移交

每轮 `cleanup-<运行名>.json` 留有 owned exact ID 的清理结果。最新窗口五轮共 20 个 owned 容器与 15 个网络在 `cleanup-final.json` 再次逐 ID 确认 absent；既有 2 容器/4 网络 ID/name/labels 不变，runtime 空、owned process 0。早期各轮同样有独立清理记录；第一轮原字符串比较 false 仅因 labels 顺序，规范化结果 true；误传 -v 轮没有资源。

全部 Go、Docker 和后台命令已退出，28 源、执行 snapshot、冻结副本与作者 cache 停写。唯一 fixture 窗口已交回主线程供独立 V；本报告仅写自有 tmp，不延迟独立验收。作者已完成适用自测，最终独立高风险验证与真实生产绑定组合尚待其负责者确认。
