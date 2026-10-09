# 默认 Runner OS 信号最小可行性探针

状态：默认 Runner cmd 已独立离线构建，首轮探针实际 FAIL：eof 首格未在原5s内取得要求的初始阻塞读见证，后两格未执行。**尚未证明该环境的阻塞读假设；也没有发出业务停止信号，不能判断 OS 三格生命周期行为。** 正式 C 源码、候选及两工具映射未修改。C 已另获真实完整通过，本探针不与其混跑，也不能代替 DefaultProcesses 或独立验证。

## 固定入口与依据

`runner-os-signals.py` 仅接收另行构建并给定 SHA256 的真正 `cmd/agenteam-runner`，不是 Go test binary。Linux/amd64 专用，固定 Go1.27.1；探针不构建、不启动服务、不发请求，不传登记 token。新 output 必须不存在，0700；证据 JSONL 为0600/O_EXCL，失败目录保留，不自动重试。

源码依据：

- `cmd/agenteam-runner/main.go` 用真实 `signal.Notify(SIGINT,SIGTERM)`，向原 app 传 `os.Stdin`。
- `internal/runner/control/client.go` 的 Run 在读取 EnrollmentSource 前持原 identity lock；未取得完整有效 token，不创建 pending、不写身份、不构造或调用 device transport。
- `internal/runner/app/app.go` 的 readEnrollmentToken 在 StopContext 取消后 Close 原 input，仍等原 Read goroutine；原 app 使用第一次 Stop 的 deadline 和单独原1s Force，未 join 时以 SHUTDOWN_TIMEOUT/exit1 结束。
- Go1.27.1 `os.Stdin=NewFile(0,...)`；`os/file_unix.go` 对 blocking 的 kindNewFile 不启用 netpoll；`internal/poll/fd_unix.go` 的 isBlocking=1 Close 不等待持有 read 引用的真正 fd destroy。能否在本内核和受限 `/proc` 下观测，仍待实际探针。

## 三格、判据和边界

每格独占一个直接 Popen 子进程和自己的真实 stdin pipe，父进程不供字节或 EOF。发信号前必须核：原 PID/start_ticks、exe dev/inode、fd0 pipe inode 与 O_NONBLOCK 清除；同一 task 两次 syscall 快照相同，确为 Linux amd64 `read(fd0)` 且 `wchan=pipe_read`。最多128 task，任何权限失败或无法观察均 FAIL，无 sleep/flock-only 替代。信号后必须见公开 stopping 日志，再重复原阻塞读证明和同 inode 身份锁检查。轮询等待本身不作事实证据。

| 场景 | 原刺激 | 必须结果 |
| --- | --- | --- |
| eof | SIGTERM 后真实父 writer Close/EOF | 原进程 actual Wait0、drained、无 forced、锁释放、无 identity 文件 |
| second_signal | 默认10s drain，SIGTERM 后真实 SIGINT，writer 保持打开 | 原 Wait1、SHUTDOWN_TIMEOUT/forced、无 drained，观察到退出在首次信号5s内，早于10s drain；force阶段观察不少于0.9s |
| deadline | 正式配置3s，只有 SIGTERM，writer 保持打开 | 原 Wait1、SHUTDOWN_TIMEOUT/forced、无 drained，原3s+1s链在3.9..5s观察范围内完成 |

时间是父进程的实际观察边界，含调度和证据写入开销；不修改生产 deadline/force allowance，不宣称精确内核退出时刻。stopping 后的 syscall 证明取消已请求时 Read 仍在内核；不是对生产 Close 函数调用栈的窥探。强退两格明确 `read_joined=false`，只认真实进程退出后的 OS 回收和 actual Wait，不声称 Read/Client 正常返回。

每格正常收尾要求 stdout/stderr 实际 EOF、空 stdout、actual Wait、原 PID 消失、相同 lock inode 可独占取得、父 pipe 全关闭；随后只删除本格自己新建的空身份 lock 和目录。无后台 copier/thread。失败路径先对仍未 reap 的自有直接 child SIGKILL、3s 内 actual Wait，再关父 pipe；不碰任意 PID、共享资源或失败目录。清理超时保留 FAIL/缺失尾，不能以 kill 请求代替实际 Wait。任何一格失败后不启动下一格。

## 首轮结果与后继边界

root fresh grant 后沿原脚本、固定cmd和新 `output/ai/runner-control/os-signals-01` 执行。首同进程 UTC2026-10-09T23:32:41Z 可用6,105,567,232B；session29686→ecba67 actual exit1。持久 events.jsonl 只进入 eof 格，记录自有PID1323863、start_ticks6119304、pipe inode3042207及公共starting。原初始5s到期报 `initial_read_not_observed`；没有initial_blocked_read、signal_sent或后两格事实，不当作真实第二信号/截止时间测试失败。

原finally记录 killed=true、actual_waited=true、parent_pipe_ends_closed=true。5cf3ec紧接实际外层终态只读确认该精确PID的/proc路径不存在；失败输出只保留0600 events.jsonl和0700 eof目录内identity.json.lock，没有identity.json。失败目录按原约定保留；没有新cleanup、自动重跑或额外资源启动，窗口已释放。cleanup未保存原Wait数值状态，不能补造-9等原终态字段；其Wait已返回与外层actual1是分别记录的事实。

原观测器只在完全命中syscall/wchan条件时保存见证，没有保存未匹配的安全任务快照。因此原失败不能区分阻塞读未成立、内核观测名称/条件差异或其它未记录情况。下一步若增加诊断，只能补有限安全观测/控制并另获实际窗口，不能把后采样或源码猜测回填本轮。OS三格、正式测试最小接入仍未完成；DefaultProcesses及独立补集未由本轮执行。

## 固定 cmd 与命令

在明确独占窗口前，不执行此脚本的 main。先由原固定离线环境单独构建原 cmd 至新私有 output 文件，并记录真实 build Wait 与 SHA；不复用测试 binary 冒默认 cmd。沿已有本树 GOCACHE、只读 GOMODCACHE、Go1.27.1/local/off、保留原 PATH。构建不修改正式源码或 C 输入。

本轮 root 授权的离线构建已完成：同进程 UTC2026-10-09T23:21:48.358993Z 可用5,644,451,840B，执行固定Go `build -mod=readonly -p=1 -o output/ai/runner-control/agenteam-runner-os-probe-01 ./cmd/agenteam-runner`，原缓存/local/off、保留PATH，无tags/race/ldflags/hooks。49073→09d6d6 actual exit0；产物11,090,755B，SHA256 `7f1c32bb97b91365570ba440adffd69605cb4a29ab724ac7922b8647f3015a78`。d51328只读 `go version -m` 确认为真实cmd、Go1.27.1、linux/amd64、VCS4eb69445且modified=false；23:22:02全局可用5,633,101,824B。编译并未执行该程序或验证三格假设。

实际命令形状：

```text
python3 .agent-state/runner-control/runner-os-signals.py --binary <新固定cmd绝对路径> --sha256 <实际build产物SHA256> --output <未使用的本树output绝对目录>
```

脚本启动前同进程 statvfs 必须至少5368709120 B，否则exit78且不启动；每格启动/读见证≤5s、stopping≤1s、取消后读见证≤2s，退出沿首次信号5s观察上限；任何失败清理另有明确3s上限。不存在 PG、浏览器、TLS/WSS、listener、业务密钥、COMMIT 或租约修改。未取得实际终态前不称可行性通过，也不将原 controlled CLI 三格升级为 OS 验证。

## C 与本探针之后的有限依赖

Linux/amd64 identity/control、空 registry 子能力仍须：原 DefaultProcesses 默认双 cmd 正常完整链真实验收；Model 持有的独立 Management Concurrent/LogoutOrder/CommitUnknown 三 top 真实独验；新 main 装配保留已交付上游模块、连续迁移/Audit 和受影响启动回归的验证。C/本探针若暴露生产缺陷，才按实际失败修复；目前没有新增已证产品修复项。

完整 D15 仍有真实 macOS≥14/另一 CPU 的文件权限、fsync/rename/flock、网络取消和跨 UID 场景的环境及证据门，交叉编译不关闭它们。公开业务 Dispatch 的 verified authority/同 Store 最终 Tx、Mount/Execution/approval/Secret masking 和真实 operation/Data Channel/Tool Runtime 绑定属于后继已保留 D10/D16/D17/D18/D19 接入；当前没有公共 Dispatch 实现授权，空 registry 的 UNSUPPORTED_OPERATION 是现行产品行为。不能将这些未来绑定当成当前可执行成功，也不为本轮扩展普通 frame 排列。
