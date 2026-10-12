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

各阶段原 invocation/process/identity/result、ownership JSONL、日志与 input maps 保存新输出目录，失败不覆盖。`outer-terminal.json` 只表示 wrapper 返回；执行者还须亲收外层工具 actual Wait，并另存 tool-terminal，区分退出码与资源释放。当前尚未运行 Go/race/vet/真实命令，生产与控源码静态接受不代实际通过。

方法源码已通过离线语法解析与 inert help。最终 `methods02` 原 Python PID 837453 实际 Wait0/0.100s，1 正 / 7 反与 stat parser 控通过，脚本和 Python 二项输入前后相等；原执行见 `output/ai/runner-command-owner/methods02-tool-terminal.json`。此前 `methods01` 的初版纯解析 0 原件仍保留；本次增加实际输入/异常未释放记录后使用新的目录，正式方法证据以 `methods02` 为准。它不证明 Go 编译、子进程、pidfd、实际 `/proc` 可见性或退出资源门通过。
