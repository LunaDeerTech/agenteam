# D15 Runner run-command WSS 有限适配

工作树 `/workspace/agenteam-runner-command-wss`，以已验本地 Command owner 的 `5920a949` 为组合起点。Handler/stream、显式 Control 绑定、codec 与正式专属语义已形成源码并获有限静态审查接受；新增控制仅源码冻结，尚未编译、基础验证或真实 WSS 验收。

本片接通显式 run-command / revision 1：可信 Workspace Manager → contained cwd 桥 → 原单次 argv Owner；回传受限 UTF-8 stdout/stderr 与真实 Start/Wait/Drain 后的完整调用报告。普通构造与生产 cmd 仍 nil executor；没有 Central 授予、Enrollment/Client.Run 验收、Secret 环境注入、ManagedProcess 或新的工具 HTTP 验证。

`recovery_lead` 唯一写 runcommand Handler/stream、Control 专属绑定与取消尾处理、专属卡/README；`skills_milestone_review` 唯一写协议 codec 和两正式规范的专属结果例外；真实 WSS fixture 与方法由独立作者负责，Git 与资源窗口由 root 管理。当前不改继承 current。

## 固定行为

- 请求只有 `workspace_name` 与 `arguments{mode:"argv",program,args,cwd}`。继承 request 的显式 deadline，不添默认业务超时。任意非 nil Environment（含空 map）与 IdempotencyKey 在 Workspace admission 前拒绝。固定最小子进程环境不继承 Runner 凭据；maskingReady 仅表达该显式 profile 的已知空注入集合，不是通用 Secret masker 或主机数据防泄漏保证。
- 已知 Started + Waited 且原 Owner/Handle Drain 实际返回，才可用专属 success payload 报告 exit/signal、取消/超时、输出完整性与 completion_code。非零退出不是调用报告丢失；success 不表示命令业务成功。未 Start 取消使用 cancelled；没有可信 Wait 使用 unknown。完整解释同步到正式 Control / execution-runtime 规范，generic response 的闭集不改变。
- stdout/stderr 独立跨 chunk UTF-8 carry（最多 3 字节），只发完整合法文本，合法 U+FFFD 保留。非法字节/非截断 EOF 残码使用固定内部输出错误，实际停止并 join；不替换字节、不把 base64 冒文本。沿本地每流 64 KiB / chunk 8 KiB 与协议 16 KiB/frame，已声明截断独立记录。
- 取消与发送的竞态只在原 active.mu 内的 command 专属策略处理：健康 wire、原 request 尚未 terminal/streamFailed 时，尚未入队的取消尾返回精确 sentinel，序号不变。Handler 标记丢弃并清 carry，Sink 返回 nil 继续排空，最终 output_complete=false。真实 emitter/transport 错误仍按原门失败并关闭 wire，不按 context 吞掉。
- 字节计数为原队列成功接纳的 UTF-8 Stream 字节，不声称网络已写完或 peer 已消费。既有 FIFO 保证收到 terminal 的 peer 先看到已接纳帧；原 session/wire/worker/Owner/Handle/Manager 均须实际返回才计退出。进程组诊断不扩大为逃逸整树或 sandbox 保证。

## 待执行验证

源码/控制冻结后由唯一 principal 增量静审。再由 root 排期执行新增 codec 4 top、Handler/stream 7 top、Control 3 top 的必要 race 与三包 vet；原 stream/cancel/runtime-error/deadline 四控及 Write/Directory hello 两控覆盖实际公共分支差额。本地 Command 已通过且未变的证据复用，不重跑旧全矩阵；上述入口当前均未执行。

真实 TLS/WSS 首链固定 `TestRunCommandWSS/exit-and-cancel`，子进程 helper `TestRunCommandWSSChild` 默认 Skip，不计行为通过。同一真实 Session 先执行非零 argv/双流，再取消原 child；仅绑定 Session 与 Manager，不冒 Central 权限或 enrollment。原实际 Wait、私有进程身份/退出与自动 HOST_TCP 基线差额门保持；60s test/70s owner 等待与 75s TCP 差额预算沿原方法，由方法作者生成具体配方。无 PG/Docker 就不编造七个 PG 资源。

方法与当前状态见[本片 README](../../../.agent-state/runner-command-wss/README.md)。完整初始化、ready、默认绑定、OpenAI tools A/B、Object join、SPA publication 与来源获取等既有 STOP 不变。
