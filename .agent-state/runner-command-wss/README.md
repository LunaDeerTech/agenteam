# Runner run-command WSS

2026-10-12，组合 `6131333449527f84bd9de70e0f3c6d56daf442c4` 的基础、编译和首次真实 WSS 已获唯一 principal 有限 whole 接受。工作树为 `/workspace/agenteam-runner-command-wss`；本片 main 集成、最终推送及树/分支清理尚未发生。对应[专属卡](../../docs/development/work-items/d15-runner-command-wss.md)与正式[Control 协议](../../docs/architecture/runner/control-protocol.md)、[Execution Runtime](../../docs/architecture/runner/execution-runtime.md)。

本片只能显式构造 env-free run-command Handler/Control executor。输入 argv/cwd 与真实退出事实不能用来授予 Central 权限；default New/cmd 仍未绑定。已知调用报告 success 与命令 exit 0 分开，取消后实际丢弃的输出必须标不完整。字节计数只代表原 outbox 接纳，取消 sentinel 不掩盖真实发送失败。

## 实际验证

原条件序列工具 `4572` actual 0，外层 PID 869427。`methods01` 原进程 869429 Wait 0，1 正 / 7 反及原 TCP 门 AST 相同检查通过。`basic01` 的原 race 进程 869538 Wait 0：20 top / 43 sub PASS；原 vet 869778 Wait 0，三包均通过，1,506 项实际输入在 unit 前后和 vet 后相同。闭集为 codec 4 新 top、Handler/stream 7 新 top、Control 3 新 top，以及 stream/cancel/runtime-error/deadline 四个旧 top 和 Write/Directory hello 两个旧 top；未重跑已验本地 Command 全矩阵。

`compile01` 原 Go 870033 race-c Wait 0，精确 list 原 870083 Wait 0，仅列出 `TestRunCommandWSS`；1,492 项编译输入前后相同。实物 binary `run-command-wss.test` 为 18,411,987 B，SHA-256 `5f7868222267d4a403c2c8ef884041dc1172617586f17b8d37ef47570fe18c19`。runtime 1,493 项仅增加该 binary；原 prepare 870148 Wait 0，固定 fresh `/tmp/rcwss01`、60 s test / 70 s owner wait / 75 s TCP 尾与两次空观测。

首次且唯一 `native01`：原工具 session `36451` 亲收 actual 0（chunk `b9153b`），记录器 wrapper 872059；supervisor 872062 原 Wait 0 / 1.785 s，原 test 872063 Wait 0。`TestRunCommandWSS/exit-and-cancel` 为 1 top / 1 sub PASS（0.22 s）；真实 `RUN_COMMAND_WSS owned_resources_joined=true`、descendants/private 双空、HOST_TCP 自动基线差额双空、1,493 项输入不变齐全，`result.json` exit 0 / 1.59 s。没有失败后重试。

同一真实 TLS/WSS Session 首先通过原 Manager/contained cwd/argv Owner 得到 exit 23、完整 stdout/stderr 和同 RID terminal；随后看到同一连接第二个原 child 的双流 ready，再发原 Cancel，等待真实 Wait、pipe/sink 返回、Owner/Handle Drain 后的取消报告。peer 收到的 FIFO 帧逐字匹配两流和队列接纳计数。该取消例没有额外尾输出，因此 `output_complete=true`；不能外推为实际取消丢尾场景，丢尾与发送失败的区分由上述定向基础控制覆盖。`TestRunCommandWSSChild` 仅原 argv 子进程入口，默认 Skip，不算额外行为 top。

单一资源 marker 只在原 session/worker/wire、peer 首次物理 Close 与 handler/server connection、offline Client、Handle 和 Manager 全部实际退出后发出。没有 PG/Docker，不编造七个 PG 资源；可见 descendants/private/TCP 事实不扩大为逃逸整树或 sandbox 保证。没有 Central 授权、enrollment/完整 Client.Run、默认生产绑定、Secret 环境注入或下一工具调用；既有 tools HTTP 等 STOP 保持。

## 原件与恢复

完整原 output 已由工具 `62814` 亲收 actual 0（chunk `2e4596`，PID 875560 / 1.444 s），同文件系统 dev 27 `NOREPLACE` 迁至 `/workspace/agenteam/output/ai/retired-20261012/runner-command-wss/`。92 文件 / 23,492,506 B / 10 目录（含根）的 path、SHA、size、dev、inode、mode、uid、gid、nsmtime 前后完全相同，保留 `methods01`、`basic01`、`compile01`、`native01` 及各原 outer 目录和实物 binary。独立证明在旁目录 `runner-command-wss-preservation/`：`output-before.json`、`output-after.json`、`source-history.json`、`process-audit.json`、`external-copies.json`、`result.json`、`tool-terminal.json`、`post-preservation-check.json`。

15 份 input map 涉及的 75 个仓库历史 blob 已逐项匹配原组合 `61313334`，1,433 项外部或产物实物核对齐；此时只证明原分支历史恢复，main 完整历史祖先关系仍待 root 集成核实。原 Go 1.27.1 工具链、SkillHTTP 热 GOCACHE 和 SkillsMilestoneResume 只读 GOMODCACHE 仍按原 invocation 定位，未迁移或删除。18 个已知 PID 在保全时可见 `/proc` 范围缺席，该观察不代替各原 Wait，也不声称逃逸整树不存在。

`native01/terminal.json` 记录本次 supervisor 872062 的实际 Wait；`native01/result.json` 记录 test 872063 与资源/TCP/输入结论。`native01/outer-terminal.json` 是此前 **prepare** wrapper 870148 的原件，保留原值，不当作 native 外层回执。工具 session 36451 的亲收转录另存证明目录 `native-tool-observation.json`，明确为 assistant-observed，不伪称 provider 原始记录；原 output 未追加或改写。原 `/tmp/rcwss01/{invocation,process,tcp-baseline}.json`、`test.log`、`native.log` 共五文件 / 1,329 B 已排他逐字复制至证明目录 `external/rcwss01/`，原 `/tmp` 保留。

源码与配方分别见 [run-checks.py](run-checks.py)、[profile-controls.py](profile-controls.py)、[run-native-checks.py](run-native-checks.py) 与 [record-native.py](record-native.py)。原 record SHA `b38417777a70e0106a15420016e1375491fc5a3b66fb23b3f2fdd9051eefa8c9` 保持；原调用中的绝对路径是历史身份，保全不重写。后继如需新运行，重新生成新输出和 fresh namespace，不能直接把旧配方移址后当成同次运行。当前所有原进程已退出，源码/方法冻结；本次文档同步不重测。
