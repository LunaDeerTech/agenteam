# Runner 本地 Command owner

本片只提供 Linux trusted-host 的单次 argv 进程 owner 与受控 workspace cwd 桥。它接续 [D16 Runner Workspace](d16-runner-workspace.md) 与 [Command Runtime](../../architecture/runner/execution-runtime.md#9-command-runtime)，不是完整 run-command Tool、Managed Process 或默认生产入口。

## 实现边界

- `command.New` 复制可信目录 fd；一次 `Run` 直接执行绝对 Program 与 Args，不经 shell，不做 PATH 查找。只传 `PATH=/usr/bin:/bin`、`LANG=C`、`LC_ALL=C`，不继承 Runner 凭据或其它环境；未提供业务 timeout 字段，由原调用 context 决定期限。
- `Handle.RunCommand` → Root 原 active-call → Linux openat2 含 containment flags 的目录 fd → 原 Owner；临时 fd、owner、pipe 和真实返回全部完成后才退休原 Root/Manager 调用。无 fd、Owner 或 host path getter。
- stdout/stderr 分流并串行同步借用 Sink，每块最多 8KiB，每流最多交付 64KiB，之后继续 drain 并明确 truncated。默认 fmt/JSON/log 不输出参数或内容。结果保留 Started、实际 Wait、exit/signal、取消、输出与组观察事实，即使同时返回错误。
- Stop 关闭 admission 并取消原调用；Drain 等待实际 Run 和目录副本 Close，不用超时返回伪造 joined。原 child 以独立 process group 启动；取消先 TERM、清理 grace 后 KILL。Linux WNOWAIT 只观察自己的原 child，保持 leader 未 reap 以避免对重用 PGID 发信号，再由原 Cmd 真正 Wait；无全局 subreaper 或 Wait(-1)。
- 原 leader 正常退出后仍清理其 process group；无法 EOF 的遗留 pipe 在清理预算后关闭原读端，再等待实际 callback/read 返回。组不存在的观察只证明该组，不证明逃逸的整棵进程树；命令仍拥有 Runner OS user 的真实 host 权限，cwd containment 不是命令 sandbox。

## 状态与必要验证

固定技术/方法 `2664c9c6` 的首次 `basic01` 已完成原执行：Go850848 race实际0/15.257s，精确14个行为top、20个sub PASS，3个子进程helper仅默认Skip；三包vet851160实际0/2.167s。覆盖真实argv/cwd/env、输出边界、非零退出与启动失败、原child/group与移组取消，以及Run/Sink/Close未返回时不得提前joined；桥实际验证containment与Root/Manager admission保持。没有新增旧域矩阵。

原工具20601由作者亲收actual0（chunk2709b5），wrapper850742实际0/19.852s；四次实际race/plain依赖发现皆0，1,111项输入before/after-unit/after逐项相同。所有Popen原Wait与私有SID双空齐全、无不可见观察缺口，窗口已释放；这些SID事实不证明逃逸整树退出。唯一主审已最终有限接受，可交main，尚不提前记正式合入。

完整52个output文件（1,996,308B）及5目录已实际跨文件系统复制至main `output/ai/retired-20261012/runner-command-owner`；旁 `runner-command-owner-preservation` 保存源前后、目标逐字及metadata证明。dev34→27的新副本不保原inode，原output未删；main完整源码历史可达和树/分支清理由root后续核实，尚不计完成。精确恢复边界见[方法说明](../../../.agent-state/runner-command-owner/README.md)。

验收范围仅Linux本地可信argv/cwd桥，不含macOS、WSS、PG、迁移、Central Operation授权、Secret注入、shell、下一Model或Transcript消费；默认nil executor、完整initializer/ready与原tools HTTP A/B等STOP保持。本片不宣称完整run-command或逃逸子进程树保证。
