# Runner Command owner 验证方法

本片是 trusted-host Linux 单次 argv owner 与真实 filesystem/workspace cwd 桥；没有 WSS、PG、Docker、Central grant、默认绑定或新的网络资源。方法不伪列 PG 七资源或 TCP 通过。cwd containment 不是命令 sandbox，进程组与私有 session 观察也不证明逃逸整棵进程树已退出。

## 精确范围

`run-checks.py basic` 仅选择下列 14 个新行为 top 与 3 个默认 Skip helper，再对这三个包 vet。旧生产接口、read/write/directory 实现没有修改，使用独立可选 command 能力，不重复旧矩阵。

| 包 | 行为 top | 独立 helper（必须默认 Skip） |
| --- | --- | --- |
| `internal/runner/command` | `TestCommandRequestAndDefaultRedaction`、`TestCommandOwnerWaitsForOriginalReturnAndClose`、`TestCommandOwnerStopBeforeStartAndCloseError`、`TestCommandDirectExecKeepsArgvCwdAndMinimalEnvironment`、`TestCommandOutputBoundsAndActualNonzeroExit`、`TestCommandStartFailureAndSinkErrorKeepSafeFacts`、`TestCommandCancellationWaitsForActualChildAndGroup`、`TestCommandHeldSinkKeepsOwnerUntilActualReturn` | `TestCommandProcessHelper` |
| `internal/runner/filesystem` | `TestFilesystemCommandUsesContainedPinnedCWD`、`TestFilesystemCommandRejectsInvalidAndEscapingCWD`、`TestFilesystemCommandRetainsAdmissionThroughOwnerAndLeaseClose`、`TestFilesystemCommandConstructorFailureJoinsBeforeLeaseClose` | `TestFilesystemCommandCWDChild` |
| `internal/runner/workspace` | `TestHandleCommandUsesActualPinnedWorkspace`、`TestHandleCommandStopJoinsOriginalReturnBeforeRetiring` | `TestWorkspaceCommandChild` |

Go JSON 事件必须对应本次原命令的精确 package/top 集合；14 个行为入口全部 PASS，三个 helper 全部且仅默认 Skip，不把 helper 算作通过入口。缺入口、额外 top、重复终态、任何行为或 subtest Skip/FAIL 都拒绝。helper 仅由行为测试使用实际 test binary、显式 marker/argv 启动；其 stdout/stderr 归原调用 Sink 或原测试断言，不以输出为空、测试 context 已取消或 helper 存在推断 joined。子进程归属仍需下面的原 Wait 与 session 尾。

## 实际输入与资源

复用已接受 Go 工具链与方法：离线、`-mod=readonly -p=1`、`GOMAXPROCS=1`，受保护 SkillHTTP 热 cache / SkillsMilestoneResume 只读 modules。实际 race/plain `go list -deps -test -json` 并集收录依赖 Go/C/asm/embed、三包同包测试、module 文件、固定 Go 和本方法脚本。生成的 test main 只单列排除，其真实源与测试不排除；不是手写最小文件表。测试后再发现集合和 SHA，新增、删除或修改相关输入都不能计通过。

单一 Python wrapper 拥有每条 Go 命令的原 `Popen`、PID/starttime、日志与实际 `wait()`；无需第二层监督。每阶段以新 session 启动，读取 `/proc/*/stat` 保存同 SID 成员及原身份（不读环境、cmdline 或文件内容），包括原 parent 退出后被 reparent 的成员。测试中的 `Setpgid` 和 `moved-group` 不改变 SID；当前 fixture 不调用 `setsid`。阶段只有在原 Go Wait 与两次独立同 SID 空观察都成立时才释放。轮询可能没有捕获极短生存个体；它不替代产品自身 Start/Wait/pipe/Close 的真实断言，不声称逐个后代都是本 Python 的 child 或已经由 Python Wait。

不可读 `/proc`、原身份缺失、未收到 Wait、尾部仍有成员（包括 zombie）均失败且标 `resource_window_released=false`，必须向 root 保持占用/收尾事实；外层工具结束不能替代资源释放。超时只对当次 SID 中、pidfd 打开后再次核对同一 starttime 的进程发 TERM，再必要 KILL，不按裸 PID、全局进程名或其它 session 杀进程。不动环境服务。超时及失败即使清理完也保持 FAIL；不重试。

测试的 `-timeout=60s` 与 wrapper 单阶段预算仅用于验证（deps 120s、包含编译的 race 300s、vet 180s）。异常信号后各等待最多 3s，正常原 Wait 后 session 尾最多 5s；没有给产品添加业务 timeout。Sink 不合作或真实 Close 未退时产品仍应持有 owner，不能以方法超时伪造 joined。任何未退出 PID 及观察缺口留在原 ownership/result 日志。

## 使用与当前状态

以下配方不代表运行授权；基础执行须由 root 排在其它共享工具链窗口之后：

```sh
python3 -B .agent-state/runner-command-owner/run-checks.py basic --output output/ai/runner-command-owner/basic01
```

只读语法与 `--help`、`self-check` 可以离线执行。self-check 只对内存事件做 1 正 / 7 反与 stat 字段解析，不启动 Go、进程或服务，也不验证信号、实际 Wait 或 `/proc` 可见性：

```sh
python3 -B .agent-state/runner-command-owner/run-checks.py self-check --output output/ai/runner-command-owner/methods01
```

各阶段原 invocation/process/identity/result、ownership JSONL、日志与 input maps 保存新输出目录，失败不覆盖。`outer-terminal.json` 只表示 wrapper 返回；执行者还须亲收外层工具 actual Wait，并另存 tool-terminal，区分退出码与资源释放。

方法源码已通过离线语法解析与 inert help。最终 `methods02` 原 Python PID 837453 实际 Wait0/0.100s，1 正 / 7 反与 stat parser 控通过，脚本和 Python 二项输入前后相等；原执行见 `output/ai/runner-command-owner/methods02-tool-terminal.json`。此前 `methods01` 的初版纯解析 0 原件仍保留；本次增加实际输入/异常未释放记录后使用新的目录，正式方法证据以 `methods02` 为准。它不证明 Go 编译、子进程、pidfd、实际 `/proc` 可见性或退出资源门通过。

首次 `basic01` 已按固定技术/方法 `2664c9c6` 实际完成：原工具20601亲收actual0（chunk2709b5），wrapper850742实际0/19.852s。race Go850848实际0/15.257s，精确14行为top/20sub PASS、3helper仅Skip；vet Go851160实际0/2.167s。四次deps各自原Wait0，1,111项实际输入三份逐项相同；所有阶段原Popen Wait、本SID双empty与无不可见缺口均已记录，窗口明确释放。唯一主审已最终有限接受；正式main合入与清理仍由root另行完成，不提前记已交付。

## 保全与恢复位置

完整副本已由指定作者一次实际复制核验（原chunk `ca7244` actual0/0.683s）至 `/workspace/agenteam/output/ai/retired-20261012/runner-command-owner`：52个普通文件、1,996,308B、5个目录，包含methods01/02、basic01全部原阶段、input maps及tool-terminal。证明另在同级 `runner-command-owner-preservation/{output-before,output-after,destination,source-recovery,process-audit,preservation-result,tool-terminal}.json`。

这是dev34→dev27的跨文件系统新副本，不是rename。源前后清单严格相同，目标相对path、SHA、size、mode、uid/gid、nsmtime逐项相等；新device/inode不同，不能声称恢复原inode。目录size/nlink等文件系统差异单独保留，不等同，atime/ctime不承诺。原 `/tmp/agenteam-runner-command-owner/output/ai/runner-command-owner` 仍完整保留，尚未删除或清理本树。

五份原input map中，basic三份均1,111项；44个仓库历史输入版本逐字对应tested `2664c9c6` 且任务分支可达，正式main完整history可达待root合入后核实。1,068个外部输入（含methods的Python）现有SHA相同，仍留原受保护工具链/modules路径，未复制或修改这些依赖。44个已记录PID当前均不在；这是有限当前观察，原Go/outer Wait与SID日志仍是实际退出依据，不扩大成全局可见性证明。

恢复时按上述新副本前缀读取历史记录，原日志里的绝对路径保持原样；另获验证授权时从正式保存源码及原环境重新准备fresh输出，不能直接在旧证据目录重跑。ignored output副本并不由Git自动携带，跨设备还需传输材料并按保全清单核SHA。范围仅Linux本地trusted-host argv/cwd桥；无WSS/Central/default/macOS/逃逸整树证明，其它主线事项、35/36/70清理累计、141待核与keep1及所有STOP不变，不把本Command提前计作已清理。
