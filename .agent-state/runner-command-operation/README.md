# Runner command Operation

本片首次方法控制、基础检查、compile/list/prepare 与独立 native01 均已实际返回 0；native 原 1 top / 2 sub 和完整资源、TCP、输入尾门通过。正式边界与分工见[工作卡](../../docs/development/work-items/d15-runner-command-operation.md)。范围仅为一次显式 Runner Command Operation，具体原件见下文。

Runtime 显式构造 `NewRunCommandAuthority(store, guard, process, executionAuthority)`，再绑定实际 `mount.NewCommandResolver` / `runnerhttp.NewCommandDispatcher` 和 `NewRunCommandExecutor`。唯一执行输入是 Execution ID；`ExecuteRunCommand` 不接受裸 argv、RID 或 committed flag。`LookupRunCommand` 只观察同一 Operation，不开启新派发或写恢复。Outcome 默认 JSON/fmt/log 脱敏，`Result()` 显式返回原报告、复制的 stdout/stderr 和可用标记。

当前配置 admission 与历史读取分离：真实私有 witness 内的 `RequireRunCommandAdmissionInTx` 核 current allow + 实际 current empty env，至少最终 Claim 再核；完成后 Lookup 不重做 dispatch admission。报告 metadata 和两流分别绑定长度/摘要，schema 临时以 report/stdout/stderr 完整 envelope 校验，持久结果不挤入32KiB terminal_data。

独立 PG 方法的精确入口为 `TestRunCommandOperation`，两个 sub：

- `committed-attempt-command-and-lookup`：真实 Attempt COMMIT 原返回前零 wire；实际 exit23 与完整 stdout/stderr；最终实际 COMMIT 后受控丢回执，原 Lookup 恢复且不重发。
- `environment-added-before-claim-no-dispatch`：Attempt 返回后、Claim 前由普通 Owner 增有效环境 entry（空字符串也非空集合），原请求拒绝且零 wire。

基础控、实际依赖闭包、race-c/list、fresh native 请求及原 actual Wait/资源/TCP 门均沿 root 单独授窗执行。编译准备与实际 native 分别授权，原运行没有重试或跨失败续跑。此记录不表示已合 main、推送、迁移原件或清理活动树。

## 真实装配与判据

唯一新 PG 文件为 `tests/projectvariable/run_command_operation_test.go`。复用原真实 Account/普通 Project Owner、Agent、Registry、Scheduler Launch、Preparation 与 ProcessGuard；原 source fixture 的 ordinary Variable 先由正式 Owner 删除，再由正式 Agent Update 设 `ApprovalAllow`、空 Secret allowlist 和本次 Tool/Mount。历史未选中的 Secret 保持原状，Model credential 仍由真实 provider 管理，不注入 child。Preparation 独立读取必须得到 empty Variables/Secrets，唯一受控 Model JSONCaller 只产生一个被真实显式 Command driver 保护的 ToolCall。没有 raw SQL seed、假权限或 caller committed flag。

第一 sub 的透明 Store 只控制原返回边界：Attempt 已物理提交但原 `WithinTx` 尚未返回时观察 durable running / 原 RID 和零 wire；放行后实际单次 argv、完整 stdout/stderr 与 exit 23 成为 `failed` / SQL `error`，安全代码 `command_exit_nonzero`。原 final 事务同时存独立 bounded report、两流及各自 SHA/长度，并退休同一 Operation/Attempt；只有已存报告的真实 COMMIT 返回后才投影丢回执 Unknown。两个 Lookup 观察原身份、完整报告和两流，wire 始终 1，不再次 Execute 或 Model。

第二 sub 在 Attempt 原返回之后、原 Claim 事务和锁之前设屏障。普通 Owner 正式提交 `value=""` 的 ordinary Variable，独立连接确认 canonical row/history/原 receipt 已存在，Agent Allow、Tool/Mount 不变；放行后由真实 current environment gate 拒绝，原身份以 `not_dispatched` 退休，wire 0、无报告。此例验证空值条目不是空集合，不重复上游 Mount 撤销矩阵。唯一 `TestRunCommandOperationChild` 默认 Skip，仅由真实 argv Owner 启动；它不计作第三个 PG top。

## 方法与执行边界

[profile-controls.py](profile-controls.py) 固定基线 main `125a61896c1ead22885f20bf65f821654835cdca`。两原 harness 只新增本 selector 的元数据；剥去该唯一 key 后 AST 与有效映射保持原基线，包括所有已验 Write/Projection profile。首次实际 1 正 / 7 反通过，调用真实闭集判定函数；8 项方法输入前后相同。

[run-pg-checks.py](run-pg-checks.py) 分 `compile` 与 `prepare`：前者以原完整 driver inputs 覆盖同包全部 Go、所有 migration/embed（含实际 `00070_runner_command_operations.sql`）、module/工具链和必要方法，执行 offline / max1 / `-race -tags=integration -c -p=1`，随后唯一精确 `^TestRunCommandOperation$` list；每段保原 PID/Wait/前后输入。后者只消费成功编译结果，runtime key 集仅增加原实物 binary，生成 fresh namespace 和四身份绑定，不启动 native。首次使用 `/tmp/rco01`，准备和原 recorder 启动前均要求该 namespace 不存在，没有复用别的任务 root。

实际 native 始终由原 `pg_only_supervisor.py --root-chain` 外层执行原 `root_chain_driver.py`，保七资源、540 + 60 s、6m test、原 actual Wait、desc/private/runtime 双尾与 75 s 自动 HOST_TCP 基线差额双空。默认 [record-native.py](record-native.py) 逐字复用原 recorder（SHA `56a32b0d45a633c3c85306d098730318327c02f4ee9c5267c8d712e9880d4f20`）；不能拆成直接 driver、替换返回判据、改预算或自动重试。方法不会因 helper 默认 Skip 把它计为业务通过。

[run-basic-checks.py](run-basic-checks.py) 的固定闭集为 **34 新 + 7 旧 = 41 top / 8 个 test 包 / 10 个 vet 包**，精确名字在脚本 `NEW_TESTS` / `COMPAT_TESTS`。新增来自 Tool contract/Registry 5、Mount/HTTP 11、PV/Execution/Loop 8、runtime 10。受影响旧控只保 contract 的 Builtin 金样、HTTP 原 terminal/correlation 与 generation/observer join 两控、Execution read/write borrow 两控、Loop 原 read/write profile 两控，已由 principal 确认；不另加 HTTP 旧四控，也不重跑上游 Write 44 top 或已验 Runner Command/WSS 矩阵。

基础方法保持原 offline/max1、真实 race-test/vet 依赖发现、migration/embed 与模块输入、Popen/Wait 和前后稳定检查。这里只保固定 `--scope full`，没有继承旧 Write 失败后专用 runtime-followup 分支，也不接受任意选择器。首次运行前发现的 PG 同块变量重名和两个方法迁移编号字面量均已最窄修正，原断言/闭包未放宽；这些静态修正不记作动态 FAIL。首次实际基础 41 top / 207 sub 与 10 包 vet 全部通过，905 项实际闭包各阶段前后相同。

下列是本次实际使用的首轮入口。后继运行仍须 root 实际窗口；任一失败保留原件并停止，不自动重试或启动下一段：

```sh
python3 -B .agent-state/runner-command-operation/profile-controls.py
python3 -B .agent-state/runner-command-operation/run-basic-checks.py --output output/ai/runner-command-operation/basic01
python3 -B .agent-state/runner-command-operation/run-pg-checks.py compile --output output/ai/runner-command-operation/compile01
python3 -B .agent-state/runner-command-operation/run-pg-checks.py prepare --compile output/ai/runner-command-operation/compile01 --output output/ai/runner-command-operation/native01 --namespace /tmp/rco01
```

每个实际工具外层均亲收返回并保存原 invocation/PID/Wait；prepare 不授权 native。原 outer recorder 在 root 后续单独授 native 窗口、核输入与资源条件后才执行生成目录中的 `record-native.py`。

## 实际结果与原始证据

执行源固定为 `3ce4e7c4c095ebf8b7dc007913ce672e8ca16e27`。原件根为本树 `output/ai/runner-command-operation/`；以下 PID 均有原 Popen/Wait 记录，不以尾后进程查询代替原 Wait。

| 阶段 | 实际结果 | 原件 |
| --- | --- | --- |
| 方法控制 | PID 888406，Wait 0；1 正 / 7 反；8 项输入同 | `methods01/{invocation,process,result,inputs-before,inputs-after}.json`、`command.log` |
| 基础 | script 888411，Wait 0 / 230.948 s；unit 888560，Wait 0 / 150.686 s，41 top / 207 sub；vet 890704，Wait 0 / 77.779 s；905 项输入同 | `basic01-outer/`、`basic01/` 原依赖、JSON test log、精确 selection 与每段输入/Wait |
| 编译与 list | Go 891816，Wait 0 / 73.480 s；list 893041，Wait 0 / 1.070 s，输出仅 `TestRunCommandOperation`；1978 项三图同 | `compile01-outer/`、`compile01/` |
| prepare | script 893068，Wait 0 / 0.922 s；runtime 1979 项仅增加原 binary，86 个同包 Go 与全部 migrations 保留 | `prepare01-outer/`、`native01/{invocation,plan,input-hashes,recorder-identity}.json` |
| native01 | 工具 29984 亲收 0 / chunk `fe8b3d`；supervisor 894697 Wait 0 / 124.193 s；driver 894704 与 Go 896357 原 Wait 0；1 top / 2 sub PASS / 31.86 s | `native01/{process,terminal}.json`、`outer.log` 与下列原 `/tmp` 文件 |

编译 binary 为 `compile01/projectvariable.test`，89,808,164 B，SHA-256 `0c8f46ceea3fca8339787d0a1d7148f608ea9a7d1658af99d6e8e010f0384111`。原条件链工具 4800 / outer 888382 已亲收 0，chunk `0e0272`；`sequence-result.json` 保留全部子 Wait，`sequence-tool-observation.json` 明确是 assistant-observed 工具回执转录，不冒充 provider 原件。

真实 native 原日志为 `/tmp/rco01/pg-8da508b922384084a038a0f0e68b6d84.log`；同名无 `.log` 目录保留 `owned.json` 和 `request.json`。第一 sub 16.06 s、第二 sub 15.80 s，原安全结果行分别确认：真实 Attempt 原返回前零 wire、原返回后完整 exit23 报告及双流、真实 final COMMIT 后 Unknown 仅 Lookup 且 wire 1；正式新增 ordinary 空值条目后当前环境非空，旧身份 `not_dispatched` 退休且 wire 0。

七资源的原 ID/nonce 与 `owned.json` 相同，两轮共 14 次 absence；原 descendants、private、runtime 双尾空，原 HOST_TCP 基线差额双空，supervisor `inputs_unchanged=True terminal=0`。`native01/inputs-after.json` 与原 1979 项 runtime map 全等；`native-tool-observation.json` 保存工具 29984 的 assistant-observed 亲收转录。prepare 容量记录仅是准备时观测：3,709,665,280 B / 1,736,454 free inode，不代表运行峰值。

全部执行仅在 `/workspace/agenteam-runner-command-operation`。原热 GOCACHE 和只读 GOMODCACHE 沿脚本固定路径保护，不复制或改锁；全部 Git 操作由 root 完成。受控 JSON Model、原 Project test Skills receipt 边界保持；验收不涵盖新 Transcript 消费、下一 Model、默认生产绑定、Secret 环境执行、完整 Approval 自动流程或 tools HTTP 解禁。
