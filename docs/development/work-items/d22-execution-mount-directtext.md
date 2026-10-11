# D22 非空 Mount 的文本执行与持有退休

本片承接[preparing capture](d22-execution-mount-capture.md)，让既有 `DirectTextDriver` / Loop / Model JSON 消费非空 Mount 元数据，并将真实运行持有与退休接入原 Snapshot/终态事务。正式依据为[执行编排](d01-contracts/execution-orchestration.md#snapshot执行与取消)、[Workspace §9、§14](../../architecture/runner/agent-workspace.md#9-execution-context-projection)。不提供 Runner 文件/命令操作、当前 Resolve/Dispatch ticket、物理目录或完整 MountContext。

## 数据与授权合同

- 原 00061 preparation material/ref 永久保持原字节和引用集合；运行持有使用新 `ExecutionMountUse{Mounts,InputDigest,SnapshotDigest}`，原 Snapshot digest 同时绑定 P/A/E、preparing attempt、start/process 与原 input。退休为 `ExecutionMountRetirement{Version,EventID}`，绑定真实终态版本与事件。
- Mount provider 的 `AcquireExecutionMountUseInTx`、`VerifyExecutionMountUseInTx`、`RetireExecutionMountUseInTx` 只用同 Store 原 Tx 和已取得的完整 Project/Agent/schedule/Execution 锁。没有新增 Runner/当前配置读取、嵌套 Tx 或后补锁。Authority 必须核 Execution 原 registry owner 与同步、短期、同 Tx 的私有 witness；公开 DTO/取消标志不构成许可。
- preparing 只验证原 material/ref；running 要求 exact live use；terminal 要求 exact use 和 Version/EventID 对应的 retirement。缺行、坏行、错 attempt/digest 或状态不符均失败，不用部分结果补齐。Acquire 同意图可重入但已退休不可复活。
- 原 Snapshot/running/Started 事务在真实 Snapshot 写入后才 Acquire；任何后继写失败由原事务整体回滚。原 Loop/Model 实际 join 后，原 terminal 事务同时写退休和生命周期事件。Stop、ctx 超时、函数返回、Unknown 均不单独退休。00062 新增两张 append-only 表，不改旧 00061、不删历史材料或目录。
- 普通首次读取、已运行/已完成 Start 重入，以及 start/terminal Unknown 观察都核上述历史状态，不重新 capture 当前配置。Unknown 只观察原 candidate，不重复调用 Model 或退休副作用；start 提交确认后仍由原 session 首次调用 Model。已确认 NotCommitted 的终态写沿原 closing 路径重新规划。
- 旧空集合不调用新增 provider、零新表写，原模型 `mounts:[]` 字节保持。非空模型投影仅包含 mount id/name/description、Runner id/name/description、原安全 workspace 名称作为逻辑路径；`capability_state:not_captured`、`headless:null`、`capabilities:null`，不从缺 hello 推导 false/已知空能力，不加 offline/incompatible 文本执行门。不将整个 input/material 或 WorkspaceID、attempt、credential/root path 注入模型。

## 实施与有界验收

Mount 子作者完成 `mount/contract/execution_use.go`、对应 provider/repository/控及完整 00062，随后完成唯一新 `tests/projectvariable/execution_mount_first_round_test.go` 和原 harness 的闭集 profile；负责人写 Execution/Loop/App 接线与控，并在子作者停写后承接 PG 断言修复。共同合同固定后按文件并行，Git 由 root 负责。

首链复用真实 first-round Model adapter/自有 HTTPS provider与此前正式 Runner/普通 Owner 配置：capture → Snapshot/Started/live use → 唯一模型调用与安全投影 → Transcript/Usage/terminal/retirement → 历史重入。必要失败控包括缺/错材料与 use、提前退休、原 held Model reader 未 join、事务末段回滚，以及 terminal 已 Committed 后受控丢回执的原身份恢复；不冒 PG 物理 Unknown，非空 start-Unknown 不在本次两子例的动态覆盖内。纯控、旧 empty 兼容与受影响真实链按风险限定，不重跑未变历史矩阵。

## 当前验证边界

`67f2250b` 的四个受影响包定向 race 为 16 top / 45 sub PASS；App 新 provider 装配仅 compile-only PASS，未执行 App 行为。原件在 `output/ai/execution-mount-directtext/checks/unit01-*`、`app01-*`，原工具 26378 实际退出 0，限定输入前后不变。

首轮真实 `native01` 保留 **wholeFAIL**：正常子例在历史重放前错误检查尚未 Stop 的 driver `Joined()`，与该方法的整个服务退出语义不符；第二子例的 held Model、实际 join、同 Tx 退休后回滚、原 closing 恢复及 terminal COMMIT 后丢回执观察 PASS。原 Go/driver/outer 均实际退出 1，七资源、descendants/private/runtime、TCP baseline 差额双尾及输入不变均闭合。原材料为 `output/ai/execution-mount-directtext/native01` 和 `/tmp/emd01/pg-3c18353e7fd14a998a859ba0d0a15f61.log`；不能将该轮记成整体通过。

`d3f64149` 仅删除三行错误的中途 `Joined()` 断言，保留真实 terminal/use 事实、同实例历史重放以及最终 Stop/Drain/Joined；生产和方法未改。`compile03/list03` 实际退出 0，原工具 8254，新 binary SHA256 `5d90570e5ac19d04186fd1322746e187c4ecae782af1d1d9b60e8f28ea5cb7b2`。旧静态修复前的 prepared 配方也标明未执行，不伪记 native 失败。

修后 `native02` 的 1 top / 2 sub 全 PASS（42.34s），验证正常非空 Snapshot/真实 Model 安全投影/退休、当前配置清空后的原 receipt 与历史材料重读，以及 held Model 的真实 join、终态 Tx 回滚和 Committed 后受控丢回执恢复。原 Go 533538 与 driver 531750 Wait 0，七资源、descendants/private/runtime 双尾和输入不变齐。但原 75s TCP baseline 差额门保留四行，外层工具 12409 与 supervisor 531728 实际退出 1，故该轮仍为 **wholeFAIL**，不能用业务通过代替完整验收。原件在 `output/ai/execution-mount-directtext/native02` 与 `/tmp/emd02/pg-03cd8dae339a429088d78ce99368da8d.log`。

后续一次被动 `/proc` 观测将两个仍在的 ESTABLISHED 非零 inode 正向关联到 `codex` PID 843 的 FD 13/14；两条 inode 0 TIME_WAIT 已消失，无法追溯。记录只证明当时的持有关系，存在权限不可读与历史转移边界，不能据端点或进程名豁免原门。原 `tcp-passive-observation.json`、`tcp-parent-chain.json` 保留；未杀进程、过滤连接、预热或改 baseline。

root 据该新观测授权一次有界原方案复验。`native03` 复用同一 `d3f64149` 技术、`compile03` binary 和原监督方法，仅使用新私有目录；1 top / 2 sub PASS（32.70s）。原 Go 550548/driver 548869 实际 Wait 0，七资源共 14 次 absence、descendants/private/runtime 双尾、原 75s 自动 baseline TCP 差额双空及输入不变齐；supervisor 548847 实际退出 0 / 124.812s，原外层工具 17648 的实际退出 0 由作者亲收。该轮 **wholePASS** 已获独立有限接受，原件在 `output/ai/execution-mount-directtext/native03` 和 `/tmp/emd03/pg-8f8a6169f415437fa09dbe600d12d4bb.log`。前两轮 wholeFAIL 不回写；此为分段证据组合，不称单次全矩阵通过。当前无本线进程、服务或缓存写者，窗口已释放，等待 root 集成。

本链使用领域服务、真实 Model HTTPS 与 Project 持久 test Skills receipt；不等于默认 App、HTTP 入口、物理目录或 Runner 运行闭环。ToolCalls/no-tools 其它 profile 门、Runner nil executor/空 Control registry、生产 initializer/ready/registry/Cleanup、网络故障 probe 与其它既有 STOP 保留。后续 native 仍由 root 排自有资源窗口，原实际 Wait、七资源和 TCP baseline 差额双尾不改。
