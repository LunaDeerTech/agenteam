# Runner 本地 Command owner

本片只提供 Linux trusted-host 的单次 argv 进程 owner 与受控 workspace cwd 桥。它接续 [D16 Runner Workspace](d16-runner-workspace.md) 与 [Command Runtime](../../architecture/runner/execution-runtime.md#9-command-runtime)，不是完整 run-command Tool、Managed Process 或默认生产入口。

## 实现边界

- `command.New` 复制可信目录 fd；一次 `Run` 直接执行绝对 Program 与 Args，不经 shell，不做 PATH 查找。只传 `PATH=/usr/bin:/bin`、`LANG=C`、`LC_ALL=C`，不继承 Runner 凭据或其它环境；未提供业务 timeout 字段，由原调用 context 决定期限。
- `Handle.RunCommand` → Root 原 active-call → Linux openat2 含 containment flags 的目录 fd → 原 Owner；临时 fd、owner、pipe 和真实返回全部完成后才退休原 Root/Manager 调用。无 fd、Owner 或 host path getter。
- stdout/stderr 分流并串行同步借用 Sink，每块最多 8KiB，每流最多交付 64KiB，之后继续 drain 并明确 truncated。默认 fmt/JSON/log 不输出参数或内容。结果保留 Started、实际 Wait、exit/signal、取消、输出与组观察事实，即使同时返回错误。
- Stop 关闭 admission 并取消原调用；Drain 等待实际 Run 和目录副本 Close，不用超时返回伪造 joined。原 child 以独立 process group 启动；取消先 TERM、清理 grace 后 KILL。Linux WNOWAIT 只观察自己的原 child，保持 leader 未 reap 以避免对重用 PGID 发信号，再由原 Cmd 真正 Wait；无全局 subreaper 或 Wait(-1)。
- 原 leader 正常退出后仍清理其 process group；无法 EOF 的遗留 pipe 在清理预算后关闭原读端，再等待实际 callback/read 返回。组不存在的观察只证明该组，不证明逃逸的整棵进程树；命令仍拥有 Runner OS user 的真实 host 权限，cwd containment 不是命令 sandbox。

## 状态与必要验证

源码实现中，尚未编译或执行。拟验证真实 argv/cwd/env、输出边界、非零退出与启动失败、实际取消和原 child/group，以及 Stop/Drain 遇原调用、回调和 Close 未返回时不得提前 joined。桥由独立作者负责真实 containment 与 Root/Manager admission 保持。只运行本片必要新控和受影响回归，命令由 root 排定共享资源窗口。

无 WSS、PG、迁移、Central Operation 授权、Secret 注入、shell、下一 Model 或 Transcript 消费；默认 nil executor、完整 initializer/ready 与原 tools HTTP A/B 等 STOP 保持。本片源码不声明真实验收通过。
