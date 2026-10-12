# Runner command Operation

当前仅源码实现，未运行本片基础检查、compile 或 native。正式边界与分工见[工作卡](../../docs/development/work-items/d15-runner-command-operation.md)。真实上游 Command/WSS 证据不代替本片验收。

Runtime 显式构造 `NewRunCommandAuthority(store, guard, process, executionAuthority)`，再绑定实际 `mount.NewCommandResolver` / `runnerhttp.NewCommandDispatcher` 和 `NewRunCommandExecutor`。唯一执行输入是 Execution ID；`ExecuteRunCommand` 不接受裸 argv、RID 或 committed flag。`LookupRunCommand` 只观察同一 Operation，不开启新派发或写恢复。Outcome 默认 JSON/fmt/log 脱敏，`Result()` 显式返回原报告、复制的 stdout/stderr 和可用标记。

当前配置 admission 与历史读取分离：真实私有 witness 内的 `RequireRunCommandAdmissionInTx` 核 current allow + 实际 current empty env，至少最终 Claim 再核；完成后 Lookup 不重做 dispatch admission。报告 metadata 和两流分别绑定长度/摘要，schema 临时以 report/stdout/stderr 完整 envelope 校验，持久结果不挤入32KiB terminal_data。

独立 PG 方法的精确入口为 `TestRunCommandOperation`，两个 sub：

- `committed-attempt-command-and-lookup`：真实 Attempt COMMIT 原返回前零 wire；实际 exit23 与完整 stdout/stderr；最终实际 COMMIT 后受控丢回执，原 Lookup 恢复且不重发。
- `environment-added-before-claim-no-dispatch`：Attempt 返回后、Claim 前由普通 Owner 增有效环境 entry（空字符串也非空集合），原请求拒绝且零 wire。

基础控、实际依赖闭包、race-c/list、fresh native 请求及原 actual Wait/资源/TCP 门由独立方法作者按 root 授窗执行；目前无本片通过结论。不得以已存在脚本或 list 代替业务/完整退出验收。现阶段不迁移原件、不清理活动树。

## 真实装配与判据

唯一新 PG 文件为 `tests/projectvariable/run_command_operation_test.go`。复用原真实 Account/普通 Project Owner、Agent、Registry、Scheduler Launch、Preparation 与 ProcessGuard；原 source fixture 的 ordinary Variable 先由正式 Owner 删除，再由正式 Agent Update 设 `ApprovalAllow`、空 Secret allowlist 和本次 Tool/Mount。历史未选中的 Secret 保持原状，Model credential 仍由真实 provider 管理，不注入 child。Preparation 独立读取必须得到 empty Variables/Secrets，唯一受控 Model JSONCaller 只产生一个被真实显式 Command driver 保护的 ToolCall。没有 raw SQL seed、假权限或 caller committed flag。

第一 sub 的透明 Store 只控制原返回边界：Attempt 已物理提交但原 `WithinTx` 尚未返回时观察 durable running / 原 RID 和零 wire；放行后实际单次 argv、完整 stdout/stderr 与 exit 23 成为 `failed` / SQL `error`，安全代码 `command_exit_nonzero`。原 final 事务同时存独立 bounded report、两流及各自 SHA/长度，并退休同一 Operation/Attempt；只有已存报告的真实 COMMIT 返回后才投影丢回执 Unknown。两个 Lookup 观察原身份、完整报告和两流，wire 始终 1，不再次 Execute 或 Model。

第二 sub 在 Attempt 原返回之后、原 Claim 事务和锁之前设屏障。普通 Owner 正式提交 `value=""` 的 ordinary Variable，独立连接确认 canonical row/history/原 receipt 已存在，Agent Allow、Tool/Mount 不变；放行后由真实 current environment gate 拒绝，原身份以 `not_dispatched` 退休，wire 0、无报告。此例验证空值条目不是空集合，不重复上游 Mount 撤销矩阵。唯一 `TestRunCommandOperationChild` 默认 Skip，仅由真实 argv Owner 启动；它不计作第三个 PG top。

## 方法与执行边界

[profile-controls.py](profile-controls.py) 固定基线 main `125a61896c1ead22885f20bf65f821654835cdca`。两原 harness 只新增本 selector 的元数据；剥去该唯一 key 后 AST 与有效映射须保持原基线，包括所有已验 Write/Projection profile。1 正 / 7 反必须验证真实闭集判定函数，不能只查字符串。当前只做过源码 AST 等价和 Python syntax 静态检查，尚未实际运行 profile controls。

[run-pg-checks.py](run-pg-checks.py) 分 `compile` 与 `prepare`：前者以原完整 driver inputs 覆盖同包全部 Go、所有 migration/embed（含实际 `00070_runner_command_operations.sql`）、module/工具链和必要方法，执行 offline / max1 / `-race -tags=integration -c -p=1`，随后唯一精确 `^TestRunCommandOperation$` list；每段保原 PID/Wait/前后输入。后者只消费成功编译结果，runtime key 集仅增加原实物 binary，生成 fresh namespace 和四身份绑定，不启动 native。建议首轮 `/tmp/rco01`，必须在准备时验证尚不存在；不复用别的任务 root。

实际 native 始终由原 `pg_only_supervisor.py --root-chain` 外层执行原 `root_chain_driver.py`，保七资源、540 + 60 s、6m test、原 actual Wait、desc/private/runtime 双尾与 75 s 自动 HOST_TCP 基线差额双空。默认 [record-native.py](record-native.py) 逐字复用原 recorder（SHA `56a32b0d45a633c3c85306d098730318327c02f4ee9c5267c8d712e9880d4f20`）；不能拆成直接 driver、替换返回判据、改预算或自动重试。方法不会因 helper 默认 Skip 把它计为业务通过。

[run-basic-checks.py](run-basic-checks.py) 的固定闭集为 **34 新 + 7 旧 = 41 top / 8 个 test 包 / 10 个 vet 包**，精确名字在脚本 `NEW_TESTS` / `COMPAT_TESTS`。新增来自 Tool contract/Registry 5、Mount/HTTP 11、PV/Execution/Loop 8、runtime 10。受影响旧控只保 contract 的 Builtin 金样、HTTP 原 terminal/correlation 与 generation/observer join 两控、Execution read/write borrow 两控、Loop 原 read/write profile 两控，已由 principal 确认；不另加 HTTP 旧四控，也不重跑上游 Write 44 top 或已验 Runner Command/WSS 矩阵。

基础方法保持原 offline/max1、真实 race-test/vet 依赖发现、migration/embed 与模块输入、Popen/Wait 和前后稳定检查。这里只保固定 `--scope full`，没有继承旧 Write 失败后专用 runtime-followup 分支，也不接受任意选择器。静态核已确认 41 个声明各唯一、必要源码与 migration 70 存在、四 Python 源语法有效、recorder 原字节相同；这些不是方法正反、Go、list 或 PG 的动态通过。首次运行前发现的 PG 同块变量重名和两个方法迁移编号字面量均已最窄修正，原断言/闭包未放宽，目前无 Go 动态 FAIL。

下列是尚未执行的首轮入口，必须按 root 实际窗口串行调用；任一失败保留原件并停止，不自动重试或启动下一段：

```sh
python3 -B .agent-state/runner-command-operation/profile-controls.py
python3 -B .agent-state/runner-command-operation/run-basic-checks.py --output output/ai/runner-command-operation/basic01
python3 -B .agent-state/runner-command-operation/run-pg-checks.py compile --output output/ai/runner-command-operation/compile01
python3 -B .agent-state/runner-command-operation/run-pg-checks.py prepare --compile output/ai/runner-command-operation/compile01 --output output/ai/runner-command-operation/native01 --namespace /tmp/rco01
```

每个实际工具外层还须亲收返回并保存原 invocation/PID/Wait；prepare 不授权 native。原 outer recorder 只在 root 后续单独授 native 窗口、核输入与资源条件后执行生成目录中的 `record-native.py`。

全部执行仅在 `/workspace/agenteam-runner-command-operation`。原热 GOCACHE 和只读 GOMODCACHE 沿脚本固定路径保护，不复制或改锁；全部 Git 操作由 root 完成。现在方法和源码不构成编译或真实 PG 通过。范围止于一次显式 Command Operation，受控 JSON Model、原 Project test Skills receipt 边界保持；没有新 Transcript 消费、下一 Model、默认生产绑定、Secret 环境执行、完整 Approval 自动流程或 tools HTTP 解禁。
