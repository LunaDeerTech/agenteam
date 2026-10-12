# Runner run-command WSS

2026-10-12，组合 `6131333449527f84bd9de70e0f3c6d56daf442c4` 的基础、编译和首次真实 WSS 已获唯一 principal 有限 whole 接受。最终 `ed78b1e4` 已随完整历史main `16f834fa` 合入并推送（25306 actual0/120ec8）；原 `/workspace/agenteam-runner-command-wss` 树及两端分支已实际清理。对应[专属卡](../../docs/development/work-items/d15-runner-command-wss.md)与正式[Control 协议](../../docs/architecture/runner/control-protocol.md)、[Execution Runtime](../../docs/architecture/runner/execution-runtime.md)。

本片只能显式构造 env-free run-command Handler/Control executor。输入 argv/cwd 与真实退出事实不能用来授予 Central 权限；default New/cmd 仍未绑定。已知调用报告 success 与命令 exit 0 分开，取消后实际丢弃的输出必须标不完整。字节计数只代表原 outbox 接纳，取消 sentinel 不掩盖真实发送失败。

## 实际验证

原条件序列工具 `4572` actual 0，外层 PID 869427。`methods01` 原进程 869429 Wait 0，1 正 / 7 反及原 TCP 门 AST 相同检查通过。`basic01` 的原 race 进程 869538 Wait 0：20 top / 43 sub PASS；原 vet 869778 Wait 0，三包均通过，1,506 项实际输入在 unit 前后和 vet 后相同。闭集为 codec 4 新 top、Handler/stream 7 新 top、Control 3 新 top，以及 stream/cancel/runtime-error/deadline 四个旧 top 和 Write/Directory hello 两个旧 top；未重跑已验本地 Command 全矩阵。

`compile01` 原 Go 870033 race-c Wait 0，精确 list 原 870083 Wait 0，仅列出 `TestRunCommandWSS`；1,492 项编译输入前后相同。实物 binary `run-command-wss.test` 为 18,411,987 B，SHA-256 `5f7868222267d4a403c2c8ef884041dc1172617586f17b8d37ef47570fe18c19`。runtime 1,493 项仅增加该 binary；原 prepare 870148 Wait 0，固定 fresh `/tmp/rcwss01`、60 s test / 70 s owner wait / 75 s TCP 尾与两次空观测。

首次且唯一 `native01`：原工具 session `36451` 亲收 actual 0（chunk `b9153b`），记录器 wrapper 872059；supervisor 872062 原 Wait 0 / 1.785 s，原 test 872063 Wait 0。`TestRunCommandWSS/exit-and-cancel` 为 1 top / 1 sub PASS（0.22 s）；真实 `RUN_COMMAND_WSS owned_resources_joined=true`、descendants/private 双空、HOST_TCP 自动基线差额双空、1,493 项输入不变齐全，`result.json` exit 0 / 1.59 s。没有失败后重试。

同一真实 TLS/WSS Session 首先通过原 Manager/contained cwd/argv Owner 得到 exit 23、完整 stdout/stderr 和同 RID terminal；随后看到同一连接第二个原 child 的双流 ready，再发原 Cancel，等待真实 Wait、pipe/sink 返回、Owner/Handle Drain 后的取消报告。peer 收到的 FIFO 帧逐字匹配两流和队列接纳计数。该取消例没有额外尾输出，因此 `output_complete=true`；不能外推为实际取消丢尾场景，丢尾与发送失败的区分由上述定向基础控制覆盖。`TestRunCommandWSSChild` 仅原 argv 子进程入口，默认 Skip，不算额外行为 top。

单一资源 marker 只在原 session/worker/wire、peer 首次物理 Close 与 handler/server connection、offline Client、Handle 和 Manager 全部实际退出后发出。没有 PG/Docker，不编造七个 PG 资源；可见 descendants/private/TCP 事实不扩大为逃逸整树或 sandbox 保证。没有 Central 授权、enrollment/完整 Client.Run、默认生产绑定、Secret 环境注入或下一工具调用；既有 tools HTTP 等 STOP 保持。

## 原件与恢复

完整原 output 已由工具 `62814` 亲收 actual 0（chunk `2e4596`，PID 875560 / 1.444 s），同文件系统 dev 27 `NOREPLACE` 迁至 `/workspace/agenteam/output/ai/retired-20261012/runner-command-wss/`。92 文件 / 23,492,506 B / 10 目录（含根）的 path、SHA、size、dev、inode、mode、uid、gid、nsmtime 前后完全相同，保留 `methods01`、`basic01`、`compile01`、`native01` 及各原 outer 目录和实物 binary。独立证明在旁目录 `runner-command-wss-preservation/`：`output-before.json`、`output-after.json`、`source-history.json`、`process-audit.json`、`external-copies.json`、`result.json`、`tool-terminal.json`、`post-preservation-check.json`。

15 份 input map 涉及的 75 个仓库历史 blob 已逐项匹配原组合 `61313334`，1,433 项外部或产物实物核对齐；随后main补核0f81c1实际0确认全部75路径历史版本可从正式main恢复，原树当时clean；独立证明为 `main-history-supplement.json` 和 `main-history-tool.json`，不改原source-history。原 Go 1.27.1 工具链、SkillHTTP 热 GOCACHE 和 SkillsMilestoneResume 只读 GOMODCACHE 仍按原 invocation 定位，未迁移或删除。18 个已知 PID 在保全时可见 `/proc` 范围缺席，该观察不代替各原 Wait，也不声称逃逸整树不存在。

`native01/terminal.json` 记录本次 supervisor 872062 的实际 Wait；`native01/result.json` 记录 test 872063 与资源/TCP/输入结论。`native01/outer-terminal.json` 是此前 **prepare** wrapper 870148 的原件，保留原值，不当作 native 外层回执。工具 session 36451 的亲收转录另存证明目录 `native-tool-observation.json`，明确为 assistant-observed，不伪称 provider 原始记录；原 output 未追加或改写。原 `/tmp/rcwss01/{invocation,process,tcp-baseline}.json`、`test.log`、`native.log` 共五文件 / 1,329 B 已排他逐字复制至证明目录 `external/rcwss01/`，原 `/tmp` 保留。

源码与配方分别见 [run-checks.py](run-checks.py)、[profile-controls.py](profile-controls.py)、[run-native-checks.py](run-native-checks.py) 与 [record-native.py](record-native.py)。原 record SHA `b38417777a70e0106a15420016e1375491fc5a3b66fb23b3f2fdd9051eefa8c9` 保持；原调用中的绝对路径是历史身份，保全不重写。后继如需新运行，重新生成新输出和 fresh namespace，不能直接把旧配方移址后当成同次运行。当前所有原进程已退出，源码/方法冻结；本次文档同步不重测。


## 实际交付与清理

root已普通移除原worktree和本地 `ai/runner-command-wss-20261012`，并fresh核对精确tip `ed78b1e42b2521141046f8ffd86da4340dcf185b`（18477 actual0/de5f60）。远端exact-lease删除95005 actual0（299567）；后继fresh空、fetch/prune及tracking空42368 actual0（e1eabc）。已保全的main retired原件与证明不动，原tmp仍保留。本次增加1树/1本地/1远端，全局累计38/39/74、legacy140待核/keep1；其它历史FAIL、STOP及保护缓存保持。


## 历史编译二进制归档补充（archive03）

2026-10-12，本片下列裸二进制已无损收入共同 [archive03](../../output/ai/retired-compile-binaries/archive03/README.md)，随后经独立授权从原 retained 路径精确移除。完整字节现保存在 [history-binaries.tar.gz](../../output/ai/retired-compile-binaries/archive03/history-binaries.tar.gz)：298,117,374 B，SHA-256 `e72c0e3c62242e4ef0fa552bdfdfb634b97290d6c58de90026c7997a77b9922a`。表中成员名相对原 `/workspace/agenteam/output/ai`，并保留原 compile 路径关系。

| 归档成员 | 字节 | SHA-256 |
| --- | ---: | --- |
| `retired-20261012/runner-command-wss/compile01/run-command-wss.test` | 18,411,987 | `5f7868222267d4a403c2c8ef884041dc1172617586f17b8d37ef47570fe18c19` |

原 phase1 工具 28190 实际 0（`662de4`），outer 988070 / child 988073 实际 Wait 0 / 50.927s；当轮完整解流验证九成员并保留全部裸件。后续 phase2 工具 59243 实际 0（`e2e194`），outer 990023 / child 990026 实际 Wait 0 / 8.168s：先重新验证整包、九成员内容与元数据及原件身份，再只 unlink 这九个获授权 ELF，目录和其它文件删除数均为 0。实际清单见 [removal03](../../output/ai/retired-compile-binaries/removal03/README.md)、[result.json](../../output/ai/retired-compile-binaries/removal03/result.json) 和 [deleted.jsonl](../../output/ai/retired-compile-binaries/removal03/deleted.jsonl)。原 139 份关联记录、FAIL、输入 maps 和归档 full stat 均未改变；当前 Command 2,007 输入与四身份保持。整批相对裸件基线净减少 394,043,392 B 已分配空间（小证明文件另计），不据此宣称 ENOSPC 已解决或新运行必定成功。

恢复时先核整个归档 SHA 和 [manifest](../../output/ai/retired-compile-binaries/archive03/manifest.json)、[before.json](../../output/ai/retired-compile-binaries/archive03/before.json)、[specs.json](../../output/ai/retired-compile-binaries/archive03/specs.json) 中的精确成员关系，再仅向新的隔离目录排他提取所需成员。例如使用 `tar --extract --gzip --file <归档绝对路径> --directory <新隔离目录> --keep-old-files --same-permissions --same-owner --numeric-owner -- <上表成员名>`；不得覆盖现有文件或写入当前 source/cache。提取后逐件核 SHA/size、mode、uid/gid 和纳秒 mtime；权限不足以恢复元数据或任一核验不符即停止，不能记为恢复完成。完整边界见共同归档恢复说明。

前文的文件数量、裸件保留和迁移 inode 相同是原保全时的历史事实。重新提取会分配新 inode 并产生新的 ctime，dev 取决于恢复目标文件系统，旧身份只保留在原 manifest/PAX 记录中，不能称新文件重建了原 inode；原日志、结果和 maps 不改写。恢复不是重跑授权或新的 PASS，热 cache、只读 modules、工具链与当前二进制不在本次删除范围。
