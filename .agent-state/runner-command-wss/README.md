# Runner run-command WSS

当前是源码阶段，未执行 Go、服务或真实 WSS。以已验本地 Command owner/Workspace 桥为依赖；对应[专属卡](../../docs/development/work-items/d15-runner-command-wss.md)与正式[Control 协议](../../docs/architecture/runner/control-protocol.md)、[Execution Runtime](../../docs/architecture/runner/execution-runtime.md)。

本片只能显式构造 env-free run-command Handler/Control executor。输入 argv/cwd 与真实退出事实不能用来授予 Central 权限；default New/cmd 仍未绑定。已知调用报告 success 与命令 exit 0 分开，取消后实际丢弃的输出必须标不完整。字节计数只代表原 outbox 接纳，取消 sentinel 不掩盖真实发送失败。

当前 writer 范围：Handler/stream/Control 接线和本卡由 recovery_lead；codec/正式语义由 skills_milestone_review；真实 WSS fixture 与方法由 knowledge_delete_fixture 独占。新增控制源码闭集为 codec 4 top、Handler/stream 7 top、Control 3 top；必要旧回归限 stream/cancel/runtime-error/deadline 四控及 Write/Directory hello 两控，三包 vet。现有源码静态接受不是 Go 或真实 WSS 通过，不预先声称可编译或原资源已退出。

真实 selector 为 `TestRunCommandWSS/exit-and-cancel`，唯一 `TestRunCommandWSSChild` helper 默认 Skip；同一 TLS Session 的非零 argv/双流和原 child 取消须由实际调用证明。方法脚本由独立作者冻结，实际全输入、原进程 Wait 与结果随后按真实记录补入；不因源级 selector 成立就计通过。

后继实际验证须保原 child Wait、pipe/sink 返回、Owner/Handle/Manager Drain、session workers、wire 和 TLS peer/server 全部真实收尾。没有 PG 资源；现有 WSS 自动 HOST_TCP 差额和 Command 的本 SID 观测按实际依赖组合，不把可见 SID 空等同逃逸整树不存在。无新业务默认 timeout、不改旧 STOP。
