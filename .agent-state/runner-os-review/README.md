# Runner OS 三格探针只读窄审

输入：`/workspace/agenteam-runner-control` 已保存 `236461e3` 的 `.agent-state/runner-control/runner-os-signals.py` 与同名 `.md`，相邻真实 cmd/app/client/config 与本地 Go1.27.1 源码。结论：限定方法与执行边界接受，无阻断；没有运行该脚本/main、child、signal、socket、Go编译或新控制套件。

- 必须由后继真实固定 Go build 证明传入的是 `cmd/agenteam-runner`，保原 build Wait/SHA；脚本本身只核给定 SHA、文件形状和实际 child exe inode，不能独自认证来源。原 cmd 的 signal.Notify、os.Stdin、原10s或配置3s drain及1s Force 路径保持。
- 只产生直接 Popen child。PID/start_ticks/exe dev+inode 在发信号前重验；failure cleanup 仅 kill 尚未 reap 的原直接 child，不能选择其他 PID。新0700目录、0600/O_EXCL证据、不复用输出或自动重试；无 token/完整 EOF，真实 Client 在 EnrollmentSource 前持身份锁，在有效 token 前不写 pending身份或构造 device transport。
- 原 fd0 pipe inode、只读和 O_NONBLOCK 清除，同一个 task 两次相同 read(fd0) syscall 和 pipe_read wchan 均为必要实际证据，含 stopping 日志后再采。权限不足/无法观察即失败；sleep/flock-only 不替代。Go blocking NewFile/poll Close 源码支持需要实测的假设，但静读不证明当前内核可观测或行为已发生。
- 正常收尾要求原实际 Wait、stdout/stderr EOF、空stdout、完整日志、同一 lock inode 可重新EX取得、原PID消失、父pipe全部关闭及本格目录仅原lock后精确删除。异常3s强退仍须实际Wait；没有该尾只能保留FAIL，不能用信号发送或后验无资源补认。
- 两个 forced格保 `read_joined=false`，仅证明进程实际退出及OS回收；eof格还依赖实际app drained/Client返回链，不能互相扩大。父观察时间含调度/fsync开销，超界失败而非扩大生产预算；probe仍不是DefaultProcesses、Management独验、macOS或完整D15交付。

只读工具 `a0ee94`、`4ceb45`、`e8b0dd` 均正常返回；不是动态探针通过。结论已直接交Runner与root；Project CleanupPhase产品验证恢复优先。
