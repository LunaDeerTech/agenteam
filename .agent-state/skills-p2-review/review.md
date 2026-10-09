# P2-only descendant prereap 窄独审

固定输入：`/workspace/agenteam-skills-p2-independent` 的 `78c61d38`，增量基线 `4530b375`。只审监督器新三个 helper、唯一 P2 root 分支，以及作者两个 Python 控制；未参与实现，未改作者文件。结论为有限离线接受，无 remaining must-fix。

新分支只在 `^TestSkillIndependentP2ConfirmationAndPackage$` 且 root 模式、原 driver 实际 Wait 之后执行。原 `prctl(PR_SET_CHILD_SUBREAPER)` 成功门先行；一次 owned descendant 集合中，每个 PID 必须两次读取的 PID/start_ticks/PPID/state 相等、PPID 是当前 supervisor、state 为 Z，才调用该 PID 的 `waitpid(WNOHANG)`。只有返回同 PID 且原 status=0 可接受。可执行文件 basename 是闭集诊断，未知/无权为 null，不是权限或身份依据；不读 argv/env，不输出 comm。

未知、变化、live、非直接子进程、非零/信号状态、wait0、错误 PID、ECHILD/其他 wait 错误都保留失败。一个成功 Z 不遮另一未知/失败 PID；新出现的 PID 仍经过旧 survivors/kill 门。成功不能重置此前 driver 的失败。移除三个 helper 与唯一四行分支后，整个 supervisor 与基线逐字一致，因此原 selector、123/3、540/60、Go 6m、75s TCP、资源/private/runtime/input 观察与尾保持；未新增 sleep、重试或等待预算。

实际控制：

- 作者 `python3 .agent-state/skills-p2-independent/reaper-controls.py`，本人 `66751f` actual exit0，64 控。实际新分支及旧 survivors/kill 块在 proc/wait/kill 替身下执行，含原失败保持、新 PID 和各身份/Wait 负例。
- 作者 `python3 .agent-state/skills-p2-independent/harness-controls.py`，本人 `66b3ad` actual exit0，42 控。真实 main 的全部外部边界为替身；九格包括失败、资源遗留与旧入口，原 Wait、14 资源观察、双 private/runtime、TCP 和 input 尾仍执行。
- 本目录 `python3 .agent-state/skills-p2-review/controls.py`，本人 `3d6ede` actual exit0，12 项独立控制。直接提取原三个 helper，连贯执行实际 stat parser、两次身份、前置安全日志与精确非阻塞 wait；补 exe 无权/已知 basename 不救 PID reuse、错 PID、非法 UTF-8、截断、非直接 Z、Z→live、两个 Z 中非零状态、错 Wait PID、snapshot 错误和未实际 Wait 不扫描。并独立核整文件逆投影。所有 `/proc` 与 wait 均为合成输入；唯一外部命令是只读 `git show`。

限制：没有启动 supervisor main、子业务进程、PG、socket、浏览器或 Go 编译，也没有用合成 wait 证明内核实际子进程退休。一次有界 owned 集合不等于 proc 系统调用可硬抢占；本增量没有新的 sleep/延时或超时延长。原 P2-01 的四子业务 PASS、whole FAIL、历史 PID 身份缺失原样保留。修后真实 P2 整轮及其实际 Wait/资源/TCP/input 尾另待独占窗口，不由本次有限接受替代。
