# D15 Runner run-command WSS 有限适配

工作树 `/workspace/agenteam-runner-command-wss`，以已验本地 Command owner 的 `5920a949` 为组合起点，最终技术组合 `61313334` 已通过基础、编译与首次真实 WSS，并获唯一 principal 有限 whole 接受。最终 `ed78b1e4` 已随完整历史main `16f834fa` 合入并推送（25306 actual0/120ec8），原树及本地/远端分支已实际清理。

本片接通显式 run-command / revision 1：可信 Workspace Manager → contained cwd 桥 → 原单次 argv Owner；回传受限 UTF-8 stdout/stderr 与真实 Start/Wait/Drain 后的完整调用报告。普通构造与生产 cmd 仍 nil executor；没有 Central 授予、Enrollment/Client.Run 验收、Secret 环境注入、ManagedProcess 或新的工具 HTTP 验证。

`recovery_lead` 实现 runcommand Handler/stream、Control 专属绑定与取消尾处理；`skills_milestone_review` 实现协议 codec 和两正式规范的专属结果例外；`knowledge_delete_fixture` 实现真实 WSS fixture 与方法并同步最终三份文档，`runtime_recovery` 独立验收。Git、保全调度与资源窗口由 root 管理。

## 固定行为

- 请求只有 `workspace_name` 与 `arguments{mode:"argv",program,args,cwd}`。继承 request 的显式 deadline，不添默认业务超时。任意非 nil Environment（含空 map）与 IdempotencyKey 在 Workspace admission 前拒绝。固定最小子进程环境不继承 Runner 凭据；maskingReady 仅表达该显式 profile 的已知空注入集合，不是通用 Secret masker 或主机数据防泄漏保证。
- 已知 Started + Waited 且原 Owner/Handle Drain 实际返回，才可用专属 success payload 报告 exit/signal、取消/超时、输出完整性与 completion_code。非零退出不是调用报告丢失；success 不表示命令业务成功。未 Start 取消使用 cancelled；没有可信 Wait 使用 unknown。完整解释同步到正式 Control / execution-runtime 规范，generic response 的闭集不改变。
- stdout/stderr 独立跨 chunk UTF-8 carry（最多 3 字节），只发完整合法文本，合法 U+FFFD 保留。非法字节/非截断 EOF 残码使用固定内部输出错误，实际停止并 join；不替换字节、不把 base64 冒文本。沿本地每流 64 KiB / chunk 8 KiB 与协议 16 KiB/frame，已声明截断独立记录。
- 取消与发送的竞态只在原 active.mu 内的 command 专属策略处理：健康 wire、原 request 尚未 terminal/streamFailed 时，尚未入队的取消尾返回精确 sentinel，序号不变。Handler 标记丢弃并清 carry，Sink 返回 nil 继续排空，最终 output_complete=false。真实 emitter/transport 错误仍按原门失败并关闭 wire，不按 context 吞掉。
- 字节计数为原队列成功接纳的 UTF-8 Stream 字节，不声称网络已写完或 peer 已消费。既有 FIFO 保证收到 terminal 的 peer 先看到已接纳帧；原 session/wire/worker/Owner/Handle/Manager 均须实际返回才计退出。进程组诊断不扩大为逃逸整树或 sandbox 保证。

## 实际验证与边界

原工具 `4572` 条件序列 actual 0：`methods01` 1 正 / 7 反与原 TCP AST 门通过；`basic01` 为 20 top / 43 sub race PASS、三包 vet 实际 0，1,506 项输入三份相同。20 top 恰为新增 codec 4、Handler/stream 7、Control 3 与受影响旧 stream/cancel/runtime-error/deadline 四控及 Write/Directory hello 两控。本地 Command 已通过且未变的证据复用，没有重复旧全矩阵。

`compile01` race-c / 精确 list 均实际 0，1,492 项编译输入相同；runtime 1,493 项只增加原实物 binary。首次且唯一 `native01` 原工具 `36451` 亲收 actual 0（`b9153b`）：supervisor 872062 Wait 0 / 1.785 s，test 872063 Wait 0；`TestRunCommandWSS/exit-and-cancel` 1 top / 1 sub PASS（0.22 s）。同一真实 TLS/WSS Session 的 exit 23 双流与原 child Cancel，均经过实际 Manager、argv Owner、Wait、pipe/sink、Handle/Manager Drain 与同 RID terminal；原 peer 首 Close、handler/server connection、Client/session/wire/worker 全 join 后才发唯一 marker。descendants/private 双空、自动 HOST_TCP 基线差额双空与输入不变齐全；原 60 s / 70 s / TCP 75 s 门保持，没有重试或预算放宽。

`TestRunCommandWSSChild` 默认 Skip，仅由真实 Owner 的明确 argv 启动，不计额外行为 top。本次取消 child 发完双 ready 后无尾输出，故 `output_complete=true` 有实际依据；不冒称动态覆盖取消丢尾。真实发送失败与取消丢尾门的定向纯控分别保留。无 PG/Docker，不编造七个 PG 资源；也不把可见进程组/descendants 事实当作逃逸整树或 sandbox 保证。

`native01/outer-terminal.json` 是 prepare 原进程 870148 的回执；本次 native 原 Wait 在 `terminal.json`（872062）与 `result.json`（872063），两者不混用。工具 36451 的亲收转录另存独立证明，标明 assistant-observed。完整 output 已由原工具 62814 actual 0 同文件系统 `NOREPLACE` 保全至 main 工作树 `output/ai/retired-20261012/runner-command-wss/`：92 文件 / 23,492,506 B / 10 目录全 metadata/hash/inode 同；五份 tmp 副本 1,329 B 齐、原 tmp 保留。旁目录 `runner-command-wss-preservation/` 保存实际核验；15 maps / 75 个仓库历史 blob 已匹配原 `61313334`，main补证明0f81c1实际0确认75路径历史恢复，root普通树/本地删除及fresh tip核对18477实际0、远端exact-lease删除95005实际0、fresh空/prune/tracking空42368实际0均已闭合。恢复路径、外部依赖与可见进程检查边界见 README。

方法与当前状态见[本片 README](../../../.agent-state/runner-command-wss/README.md)。完整初始化、ready、默认绑定、OpenAI tools A/B、Object join、SPA publication 与来源获取等既有 STOP 不变。
